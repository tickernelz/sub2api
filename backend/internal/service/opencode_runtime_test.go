package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tickernelz/sub2api/internal/config"
	"github.com/tickernelz/sub2api/internal/pkg/ctxkey"
	"github.com/tickernelz/sub2api/internal/pkg/tlsfingerprint"
)

func TestOpenCodeAccountEligibleForOpenAICompatibleChatRuntime(t *testing.T) {
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenCode,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key": "opencode-key",
		},
	}

	require.True(t, isOpenAIAccountEligibleForRequest(
		context.Background(),
		account,
		"glm-5.1",
		false,
		OpenAIEndpointCapabilityChatCompletions,
	))
	require.False(t, isOpenAIAccountEligibleForRequest(
		context.Background(),
		account,
		"glm-5.1",
		false,
		OpenAIEndpointCapabilityEmbeddings,
	))
}

func TestOpenAIGatewayServiceGetAccessTokenSupportsOpenCodeAPIKey(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "opencode-key",
		},
	}

	token, tokenType, err := svc.GetAccessToken(context.Background(), account)

	require.NoError(t, err)
	require.Equal(t, "opencode-key", token)
	require.Equal(t, "apikey", tokenType)
}

func TestBuildOpenAIUpstreamRequestUsesOpenCodeBaseURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	svc := &OpenAIGatewayService{}
	account := &Account{
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "opencode-key",
			"base_url": "https://proxy.example.com/opencode/v1",
		},
	}

	req, err := svc.buildUpstreamRequest(context.Background(), c, account, []byte(`{"model":"glm-5.1","input":"hi"}`), "opencode-key", false, "", false)

	require.NoError(t, err)
	require.Equal(t, "https://proxy.example.com/opencode/v1/responses", req.URL.String())
	require.Equal(t, "Bearer opencode-key", req.Header.Get("authorization"))
}

func TestOpenAIGatewayServiceSelectsOpenCodeAccountsForOpenCodeGroup(t *testing.T) {
	groupID := int64(77)
	ctx := context.WithValue(context.Background(), ctxkey.Group, &Group{ID: groupID, Platform: PlatformOpenCode})
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{
			ID:          91,
			Platform:    PlatformOpenCode,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Credentials: map[string]any{"api_key": "opencode-key"},
		}}},
	}

	account, err := svc.selectAccountForModelWithExclusions(
		ctx,
		&groupID,
		"",
		"glm-5.1",
		nil,
		false,
		0,
		OpenAIEndpointCapabilityChatCompletions,
	)

	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, int64(91), account.ID)
}

func TestDefaultOpenAISchedulerSelectsOpenCodeAccountsForOpenCodeGroup(t *testing.T) {
	groupID := int64(78)
	ctx := context.WithValue(context.Background(), ctxkey.Group, &Group{ID: groupID, Platform: PlatformOpenCode})
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{{
			ID:          92,
			Platform:    PlatformOpenCode,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Credentials: map[string]any{"api_key": "opencode-key"},
		}}},
	}
	scheduler := newDefaultOpenAIAccountScheduler(svc, newOpenAIAccountRuntimeStats())

	selection, _, err := scheduler.Select(ctx, OpenAIAccountScheduleRequest{
		GroupID:            &groupID,
		RequestedModel:     "glm-5.1",
		RequiredCapability: OpenAIEndpointCapabilityChatCompletions,
	})

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(92), selection.Account.ID)
}

type opencodeTestHTTPUpstream struct {
	request *http.Request
	body    string
}

func (u *opencodeTestHTTPUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return nil, errors.New("unexpected Do call")
}

func (u *opencodeTestHTTPUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.request = req
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		u.body = string(body)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`data: {"type":"response.output_text.delta","delta":"pong"}

data: {"type":"response.completed"}

`)),
	}, nil
}

func TestAccountTestServiceRoutesOpenCodeThroughOpenAICompatibleProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/93/test", nil)
	account := Account{
		ID:          93,
		Platform:    PlatformOpenCode,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "opencode-key"},
	}
	upstream := &opencodeTestHTTPUpstream{}
	svc := &AccountTestService{
		accountRepo:  schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}

	err := svc.TestAccountConnection(c, account.ID, "", "", "")

	require.NoError(t, err)
	require.NotNil(t, upstream.request)
	require.Equal(t, "https://opencode.ai/zen/v1/responses", upstream.request.URL.String())
	require.Equal(t, "Bearer opencode-key", upstream.request.Header.Get("Authorization"))
	require.Contains(t, upstream.body, `"model":"big-pickle"`)
}

func TestAccountTestServiceRejectsOpenCodeCompactProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/94/test", nil)
	account := Account{
		ID:          94,
		Platform:    PlatformOpenCode,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "opencode-key"},
	}
	upstream := &opencodeTestHTTPUpstream{}
	svc := &AccountTestService{
		accountRepo:  schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}

	err := svc.TestAccountConnection(c, account.ID, "", "", AccountTestModeCompact)

	require.Error(t, err)
	require.Contains(t, err.Error(), "OpenCode does not support compact account tests")
	require.Nil(t, upstream.request)
	require.Contains(t, rec.Body.String(), "OpenCode does not support compact account tests")
}
