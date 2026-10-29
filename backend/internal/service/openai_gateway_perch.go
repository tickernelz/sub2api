package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/perch"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var perchClientFactory = func(creds perch.Credentials, store perch.CredentialStore) *perch.Client {
	return perch.NewGatewayClient(creds, store)
}

const (
	perchAppURLKey       = "app_url"
	perchAccessTokenKey  = "access_token"
	perchRefreshTokenKey = "refresh_token"
	perchExpiresAtKey    = "expires_at"
	perchUserIDKey       = "user_id"
	perchEmailKey        = "email"

	perchCredentialMissingCode = "perch_credentials_missing"
)

type perchAccountCredentialStore struct {
	service *OpenAIGatewayService
	account *Account
}

func (s perchAccountCredentialStore) StorePerchCredentials(ctx context.Context, creds perch.Credentials) error {
	next := shallowCopyMap(s.account.Credentials)
	next[perchAppURLKey] = creds.AppURL
	next[perchAccessTokenKey] = creds.AccessToken
	if creds.RefreshToken != "" {
		next[perchRefreshTokenKey] = creds.RefreshToken
	}
	if creds.ExpiresAt > 0 {
		next[perchExpiresAtKey] = creds.ExpiresAt
	}
	if creds.UserID != "" {
		next[perchUserIDKey] = creds.UserID
	}
	if creds.Email != "" {
		next[perchEmailKey] = creds.Email
	}
	return persistAccountCredentials(ctx, s.service.accountRepo, s.account, next)
}

func perchUnsupported(c *gin.Context, endpoint string) (*OpenAIForwardResult, error) {
	if c != nil && c.Writer != nil && !c.Writer.Written() {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"type":    "unsupported_protocol",
			"code":    "unsupported_protocol",
			"message": "perch accounts serve /v1/chat/completions only, not " + endpoint,
		}})
	}
	return nil, fmt.Errorf("perch accounts do not implement %s", endpoint)
}

func perchUnsupportedError(c *gin.Context, endpoint string) error {
	_, err := perchUnsupported(c, endpoint)
	return err
}

func perchCredentialValue(account *Account, keys ...string) string {
	if account == nil {
		return ""
	}
	for _, key := range keys {
		value, ok := account.Credentials[key]
		if !ok || value == nil {
			continue
		}
		if text, ok := value.(string); ok {
			if strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
			continue
		}
		if text := fmt.Sprint(value); strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func perchCredentialUnix(account *Account, keys ...string) int64 {
	if account == nil {
		return 0
	}
	for _, key := range keys {
		value, ok := account.Credentials[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case int64:
			return typed
		case int:
			return int64(typed)
		case float64:
			return int64(typed)
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
				return parsed
			}
			if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(typed)); err == nil {
				return parsed.Unix()
			}
		}
	}
	return 0
}

func perchCredentialsFromAccount(account *Account) (perch.Credentials, error) {
	if account == nil {
		return perch.Credentials{}, fmt.Errorf("perch: account is required")
	}
	appURL := perchCredentialValue(account, perchAppURLKey, "appUrl")
	if appURL == "" {
		appURL = perch.DefaultAppURL
	}
	creds := perch.Credentials{
		AppURL:       appURL,
		AccessToken:  perchCredentialValue(account, perchAccessTokenKey, "accessToken"),
		RefreshToken: perchCredentialValue(account, perchRefreshTokenKey, "refreshToken"),
		UserID:       perchCredentialValue(account, perchUserIDKey, "userId"),
		Email:        perchCredentialValue(account, perchEmailKey),
		ExpiresAt:    perchCredentialUnix(account, perchExpiresAtKey, "expiresAt"),
	}
	if err := creds.Validate(); err != nil {
		return perch.Credentials{}, err
	}
	return creds, nil
}

func (s *OpenAIGatewayService) perchClientFor(ctx context.Context, account *Account) (*perch.Client, error) {
	creds, err := perchCredentialsFromAccount(account)
	if err != nil {
		return nil, err
	}
	if value, ok := s.perchClients.Load(account.ID); ok {
		client, _ := value.(*perch.Client)
		if client != nil {
			current := client.Session().Credentials()
			if current.AccessToken == "" || creds.ExpiresAt > current.ExpiresAt {
				client.Session().SetCredentials(creds)
			}
			return client, nil
		}
	}
	created := perchClientFactory(creds, perchAccountCredentialStore{service: s, account: account})
	actual, _ := s.perchClients.LoadOrStore(account.ID, created)
	client, _ := actual.(*perch.Client)
	if client == nil {
		return created, nil
	}
	return client, nil
}

func perchUsage(u *perch.Usage) OpenAIUsage {
	if u == nil {
		return OpenAIUsage{}
	}
	return OpenAIUsage{
		InputTokens:              u.PromptTokens(),
		OutputTokens:             u.CompletionTokens(),
		CacheCreationInputTokens: u.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens,
	}
}

func perchFailoverError(err error) error {
	status := http.StatusBadGateway
	code := "upstream_error"
	message := err.Error()
	var apiErr *perch.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Status >= 400 && apiErr.Status <= 599 {
			status = apiErr.Status
		}
		if apiErr.Code != "" {
			code = apiErr.Code
		}
		if apiErr.Message != "" {
			message = apiErr.Message
		}
	}
	body, marshalErr := json.Marshal(map[string]any{"error": map[string]any{"type": code, "code": code, "message": message}})
	if marshalErr != nil {
		body = nil
	}
	return &UpstreamFailoverError{StatusCode: status, ResponseBody: body}
}

func (s *OpenAIGatewayService) forwardPerchChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	originalModel := gjson.GetBytes(body, "model").String()
	if originalModel == "" {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}
	billingModel := resolveOpenAIForwardModel(account, originalModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	SetOpsUpstreamModel(c, upstreamModel)

	requestBody := body
	if upstreamModel != originalModel {
		requestBody = ReplaceModelInBody(body, upstreamModel)
	}

	var req apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(requestBody, &req); err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "request body is not valid JSON")
		return nil, fmt.Errorf("decode perch chat completion request: %w", err)
	}

	call, err := perch.BuildModelCallRequest(&req, perch.CallOptions{})
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}

	client, err := s.perchClientFor(ctx, account)
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, perchCredentialMissingCode, err.Error())
		return nil, err
	}

	stream, err := client.OpenModelCall(ctx, call)
	if err != nil {
		return nil, perchFailoverError(err)
	}
	defer func() { _ = stream.Close() }()

	responseID := "chatcmpl-" + strings.TrimPrefix(stream.Request.RunID, "tkt-cli-")
	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	model := req.Model
	if model == "" {
		model = originalModel
	}

	if req.Stream {
		return s.streamPerchChatCompletions(c, stream, model, responseID, originalModel, billingModel, upstreamModel, includeUsage)
	}

	outcome, err := perch.CollectOutcome(stream)
	if err != nil {
		return nil, perchFailoverError(err)
	}
	settledModel := model
	if outcome.Model != "" {
		settledModel = outcome.Model
	}
	c.JSON(http.StatusOK, perch.ToChatResponse(outcome, settledModel, responseID, time.Now().Unix()))
	return &OpenAIForwardResult{
		RequestID:             responseID,
		ResponseID:            responseID,
		Model:                 originalModel,
		BillingModel:          billingModel,
		UpstreamModel:         upstreamModel,
		UpstreamResponseModel: outcome.Model,
		Usage:                 perchUsage(outcome.Usage),
	}, nil
}

func (s *OpenAIGatewayService) streamPerchChatCompletions(
	c *gin.Context,
	stream *perch.ModelCallStream,
	model, responseID, originalModel, billingModel, upstreamModel string,
	includeUsage bool,
) (*OpenAIForwardResult, error) {
	writer := c.Writer
	wroteOutput := false
	startStream := func() {
		if wroteOutput {
			return
		}
		header := writer.Header()
		header.Set("content-type", "text/event-stream")
		header.Set("cache-control", "no-cache")
		header.Set("connection", "keep-alive")
		writer.WriteHeader(http.StatusOK)
		wroteOutput = true
	}

	outcome, err := perch.StreamChat(stream, model, responseID, time.Now().Unix(), includeUsage, func(chunk apicompat.ChatCompletionsChunk) error {
		payload, marshalErr := json.Marshal(chunk)
		if marshalErr != nil {
			return marshalErr
		}
		startStream()
		if _, writeErr := fmt.Fprintf(writer, "data: %s\n\n", payload); writeErr != nil {
			return writeErr
		}
		writer.Flush()
		return nil
	})
	if err != nil {
		if !wroteOutput {
			return nil, perchFailoverError(err)
		}
		code := "upstream_error"
		message := err.Error()
		var apiErr *perch.APIError
		if errors.As(err, &apiErr) {
			if apiErr.Code != "" {
				code = apiErr.Code
			}
			if apiErr.Message != "" {
				message = apiErr.Message
			}
		}
		payload, marshalErr := json.Marshal(map[string]any{"error": map[string]any{"message": message, "type": code, "code": code}})
		if marshalErr == nil {
			_, _ = fmt.Fprintf(writer, "data: %s\n\n", payload)
			writer.Flush()
		}
		return nil, err
	}
	_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	writer.Flush()

	settledModel := model
	if outcome.Model != "" {
		settledModel = outcome.Model
	}
	return &OpenAIForwardResult{
		RequestID:             responseID,
		ResponseID:            responseID,
		Model:                 originalModel,
		BillingModel:          billingModel,
		UpstreamModel:         upstreamModel,
		UpstreamResponseModel: settledModel,
		Usage:                 perchUsage(outcome.Usage),
	}, nil
}
