package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/perch"
	"github.com/gin-gonic/gin"
)

// PerchAvailableModels resolves the model list for a Perch account: explicit mapping, then the account's Perch allowlist, then the shipped catalog.
func (s *AccountTestService) PerchAvailableModels(ctx context.Context, account *Account) ([]claude.Model, error) {
	if s == nil || account == nil || account.Platform != PlatformPerch {
		return nil, errors.New("perch account is required")
	}
	if mapping := account.GetModelMapping(); len(mapping) > 0 {
		ids := make([]string, 0, len(mapping))
		for requestedModel := range mapping {
			ids = append(ids, requestedModel)
		}
		sort.Strings(ids)
		return PerchModelEntries(ids), nil
	}
	models, err := s.FetchPerchAccountModels(ctx, account)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, errors.New("perch account advertises no models")
	}
	return models, nil
}

// FetchPerchAccountModels reports the model list Perch allows for this account.
func (s *AccountTestService) FetchPerchAccountModels(ctx context.Context, account *Account) ([]claude.Model, error) {
	if s == nil || account == nil || account.Platform != PlatformPerch {
		return nil, errors.New("perch account is required")
	}
	if s.openaiGatewayService == nil {
		return nil, errors.New("perch model discovery service is unavailable")
	}
	client, err := s.openaiGatewayService.perchClientFor(ctx, account)
	if err != nil {
		return nil, err
	}
	ids, err := client.ModelIDs(ctx)
	if err != nil {
		return nil, err
	}
	return PerchModelEntries(ids), nil
}

// PerchModelEntries shapes Perch model identifiers for surfaces that speak the Anthropic model list schema.
func PerchModelEntries(ids []string) []claude.Model {
	models := make([]claude.Model, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		models = append(models, claude.Model{ID: id, Type: "model", DisplayName: id})
	}
	return models
}

func (s *AccountTestService) testPerchAccountConnection(c *gin.Context, account *Account, modelID string, prompt string) error {
	testModelID := strings.TrimSpace(modelID)
	if testModelID == "" {
		defaults := perch.DefaultModelIDs()
		if len(defaults) == 0 {
			return s.sendErrorAndEnd(c, "Perch advertises no default model")
		}
		testModelID = defaults[0]
	}
	testModelID = account.GetMappedModel(testModelID)
	if s.openaiGatewayService == nil {
		return s.sendErrorAndEnd(c, "Perch gateway service is unavailable")
	}
	payload, err := json.Marshal(createOpenAIChatCompletionsTestPayload(testModelID, prompt))
	if err != nil {
		return s.sendErrorAndEnd(c, "Failed to build the Perch test request")
	}
	var req apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return s.sendErrorAndEnd(c, "Failed to build the Perch test request")
	}
	call, err := perch.BuildModelCallRequest(&req, perch.CallOptions{})
	if err != nil {
		return s.sendErrorAndEnd(c, "Perch rejected the test request: "+err.Error())
	}
	client, err := s.openaiGatewayService.perchClientFor(c.Request.Context(), account)
	if err != nil {
		return s.sendErrorAndEnd(c, "Perch credentials are unusable: "+err.Error())
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()
	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})

	stream, err := client.OpenModelCall(c.Request.Context(), call)
	if err != nil {
		return s.sendErrorAndEnd(c, perchTestError(err))
	}
	defer func() { _ = stream.Close() }()

	outcome, err := perch.CollectOutcome(stream)
	if err != nil {
		return s.sendErrorAndEnd(c, perchTestError(err))
	}
	if outcome == nil {
		return s.sendErrorAndEnd(c, "Perch closed the stream without a result")
	}
	if !outcome.OK {
		message := strings.TrimSpace(outcome.Error)
		if message == "" {
			message = "no answer returned"
		}
		if code := strings.TrimSpace(outcome.ErrorCode); code != "" {
			message = code + ": " + message
		}
		return s.sendErrorAndEnd(c, "Perch model call failed: "+message)
	}
	if text := strings.TrimSpace(outcome.Text); text != "" {
		s.sendEvent(c, TestEvent{Type: "content", Text: outcome.Text})
	}
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}

func perchTestError(err error) string {
	var apiErr *perch.APIError
	if errors.As(err, &apiErr) && strings.TrimSpace(apiErr.Message) != "" {
		return "Perch model call failed: " + strings.TrimSpace(apiErr.Message)
	}
	return "Perch model call failed: " + err.Error()
}
