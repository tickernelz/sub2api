package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestFilterCodexInput_StripsUnsupportedStatusFromInputItems(t *testing.T) {
	message := map[string]any{
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": map[string]any{
			"text":   "previous response",
			"status": "nested-status",
		},
	}
	reasoning := map[string]any{
		"type":              "reasoning",
		"status":            "completed",
		"summary":           []any{},
		"encrypted_content": "opaque reasoning",
	}
	output := map[string]any{
		"type":    "function_call_output",
		"call_id": "fc_123",
		"status":  "completed",
		"output":  "done",
	}
	webSearch := map[string]any{
		"type":   "web_search_call",
		"id":     "ws_123",
		"status": "completed",
	}

	filtered := filterCodexInputWithOptions([]any{message, reasoning, output, webSearch}, codexInputFilterOptions{
		PreserveReferences: true,
	})

	require.Len(t, filtered, 4)
	for i, rawItem := range filtered {
		item, ok := rawItem.(map[string]any)
		require.True(t, ok, "filtered item %d must remain an object", i)
		_, hasStatus := item["status"]
		require.False(t, hasStatus, "status must not be sent on OAuth Codex input item %d", i)
	}

	filteredMessage, ok := filtered[0].(map[string]any)
	require.True(t, ok)
	filteredContent, ok := filteredMessage["content"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "nested-status", filteredContent["status"], "nested content metadata must be preserved")

	// Filtering must remain copy-on-write; callers may reuse their request maps.
	require.Equal(t, "completed", message["status"])
	require.Equal(t, "completed", reasoning["status"])
	require.Equal(t, "completed", output["status"])
	require.Equal(t, "completed", webSearch["status"])

	filteredOutput, ok := filtered[2].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "fc_123", filteredOutput["call_id"])
	require.Equal(t, "done", filteredOutput["output"])
}

func TestOpenAIGatewayService_OAuthResponsesStripsStatusFromInputItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	body := []byte(`{"model":"gpt-5.6-luna","stream":false,"input":[{"type":"message","role":"user","status":"completed","content":{"text":"hello","status":"nested-status"}},{"type":"function_call_output","call_id":"call_123","status":"completed","output":"done"},{"type":"web_search_call","id":"ws_123","status":"completed"}]}`)
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":{"type":"invalid_request_error","message":"capture"}}`))),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := &Account{
		ID: 89, Name: "oauth", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc"},
		Status:      StatusActive, Schedulable: true,
	}

	_, _ = svc.Forward(context.Background(), c, account, body)
	require.NotNil(t, upstream.lastReq)
	require.NotEmpty(t, upstream.lastBody)
	for i := 0; i < 3; i++ {
		require.False(t, gjson.GetBytes(upstream.lastBody, "input."+string(rune('0'+i))+".status").Exists(), "input item status must be stripped before OAuth upstream")
	}
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "input.0.content.text").String())
	require.Equal(t, "nested-status", gjson.GetBytes(upstream.lastBody, "input.0.content.status").String())
	require.Equal(t, "done", gjson.GetBytes(upstream.lastBody, "input.1.output").String())
}
