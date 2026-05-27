package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAntigravityGatewayService_ForwardAsChatCompletionsUsesAntigravityV1Internal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamBody := strings.Join([]string{
		`data: {"response":{"candidates":[{"content":{"parts":[{"text":"OK"}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}},"responseId":"ag-test-response"}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid-ag-chat"}},
		Body:       ioNopCloser(upstreamBody),
	}}

	svc := &AntigravityGatewayService{
		tokenProvider:  NewAntigravityTokenProvider(nil, nil, nil),
		httpUpstream:   upstream,
		settingService: NewSettingService(&antigravityChatCompatSettingRepo{}, &config.Config{}),
	}
	account := &Account{
		ID:       12,
		Name:     "ag-test",
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "test-token",
			"project_id":   "test-project",
			"model_mapping": map[string]any{
				"gemini-pro-agent": "gemini-pro-agent",
			},
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gemini-pro-agent","messages":[{"role":"user","content":"say OK"}],"max_tokens":16,"stream":false}`))
	c.Request.Header.Set("Content-Type", "application/json")

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gemini-pro-agent","messages":[{"role":"user","content":"say OK"}],"max_tokens":16,"stream":false}`), false)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, upstream.lastReq)
	require.Contains(t, upstream.lastReq.URL.Path, "/v1internal:streamGenerateContent")
	require.NotContains(t, upstream.lastReq.URL.Host, "anthropic")
	require.Equal(t, "Bearer test-token", upstream.lastReq.Header.Get("Authorization"))

	require.Equal(t, http.StatusOK, w.Code)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Equal(t, "chat.completion", payload["object"])
	choices, ok := payload["choices"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, choices)
	choice, ok := choices[0].(map[string]any)
	require.True(t, ok)
	message, ok := choice["message"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "OK", message["content"])
}

func ioNopCloser(s string) *readCloser {
	return &readCloser{Reader: strings.NewReader(s)}
}

type readCloser struct {
	*strings.Reader
}

func (r *readCloser) Close() error { return nil }

type antigravityChatCompatSettingRepo struct{}

func (r *antigravityChatCompatSettingRepo) Get(ctx context.Context, key string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (r *antigravityChatCompatSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	return "", ErrSettingNotFound
}

func (r *antigravityChatCompatSettingRepo) Set(ctx context.Context, key, value string) error {
	return nil
}

func (r *antigravityChatCompatSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *antigravityChatCompatSettingRepo) SetMultiple(ctx context.Context, settings map[string]string) error {
	return nil
}

func (r *antigravityChatCompatSettingRepo) GetAll(ctx context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *antigravityChatCompatSettingRepo) Delete(ctx context.Context, key string) error {
	return nil
}
