package perch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

func newTestHandler(server *perchTestServer) *Handler {
	return NewHandler(CredentialSourceFunc(func(ctx context.Context) (Credentials, error) {
		return testCredentials(server.URL), nil
	}))
}

func chatRequestBody(t *testing.T, stream bool) []byte {
	t.Helper()
	req := map[string]any{
		"model":    "wandb-kimi-k2-6",
		"stream":   stream,
		"messages": []map[string]any{{"role": "user", "content": "Reply with the single word: pong"}},
	}
	if stream {
		req["stream_options"] = map[string]any{"include_usage": true}
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	return raw
}

func TestHandlerStreamsOpenAIChunks(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	handler := newTestHandler(server)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(chatRequestBody(t, true))))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if ct := recorder.Header().Get("content-type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}

	var text strings.Builder
	var usage *apicompat.ChatUsage
	var sawDone bool
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			sawDone = true
			continue
		}
		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("decode chunk %s: %v", payload, err)
		}
		if chunk.Model != "wandb-kimi-k2-6" {
			t.Fatalf("chunk model = %q, want requested model", chunk.Model)
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != nil {
				_, _ = text.WriteString(*choice.Delta.Content)
			}
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	if text.String() != "pong" {
		t.Fatalf("streamed text = %q, want pong", text.String())
	}
	if usage == nil || usage.TotalTokens != 26676 || usage.PromptTokens != 26672 {
		t.Fatalf("usage chunk = %#v", usage)
	}
	if !sawDone {
		t.Fatalf("stream did not terminate with [DONE]")
	}
	if server.modelHits != 1 || server.ticketHits != 1 {
		t.Fatalf("upstream calls model=%d ticket=%d", server.modelHits, server.ticketHits)
	}
}

func TestHandlerNonStreamResponseCarriesUsage(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	handler := newTestHandler(server)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(chatRequestBody(t, false))))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var response apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v (%s)", err, recorder.Body.String())
	}
	if response.Object != "chat.completion" {
		t.Fatalf("object = %q", response.Object)
	}
	if response.Model != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("model = %q", response.Model)
	}
	if len(response.Choices) != 1 || string(response.Choices[0].Message.Content) != `"pong"` {
		t.Fatalf("choices = %#v", response.Choices)
	}
	if response.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish reason = %q", response.Choices[0].FinishReason)
	}
	if response.Usage == nil || response.Usage.CompletionTokens != 4 {
		t.Fatalf("usage = %#v", response.Usage)
	}
	if got := recorder.Header().Get("x-perch-upstream-model"); got != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("upstream model header = %q", got)
	}
}

func TestHandlerMapsUpstreamAuthFailure(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{
		ModelStatus:    http.StatusUnauthorized,
		ModelErrorBody: []byte(`{"ok":false,"error":"Unauthorized","errorCode":"not_authenticated"}`),
	})
	handler := newTestHandler(server)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(chatRequestBody(t, false))))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Error struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, recorder.Body.String())
	}
	if envelope.Error.Type != "authentication_error" || envelope.Error.Code != CodeNotAuthenticated {
		t.Fatalf("envelope = %#v", envelope.Error)
	}
}

func TestHandlerModelsHonorsPlanRestriction(t *testing.T) {
	restricted := []byte(`{"ok":true,"profile":{"email":"sample@example.com"},"session":{"userId":"u1","workspaceId":"w1","planCode":"pilot","planName":"Pilot","entitlements":[{"key":"limits.allowed_models","value_json":{"models":["wandb-kimi-k2-6"]}}]}}`)
	server := newPerchTestServer(t, perchTestOptions{AccountBody: restricted})
	handler := newTestHandler(server)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].ID != "wandb-kimi-k2-6" {
		t.Fatalf("models = %#v", payload.Data)
	}
}

func TestHandlerRejectsNonPostChat(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	handler := newTestHandler(server)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestHandlerStreamsErrorWhenDoneNeverArrives(t *testing.T) {
	streamBody := []byte("data: {\"type\":\"answer_delta\",\"text\":\"partial\"}\n\n")
	server := newPerchTestServer(t, perchTestOptions{StreamBody: streamBody})
	handler := newTestHandler(server)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(chatRequestBody(t, true))))
	handler.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if !strings.Contains(body, "stream_interrupted") {
		t.Fatalf("stream body = %s", body)
	}
	if strings.Contains(body, "[DONE]") {
		t.Fatalf("interrupted stream must not close with [DONE]: %s", body)
	}
}

func TestHandlerSetsRetryAfterFromLimitReset(t *testing.T) {
	resetAt := time.Now().Add(90 * time.Second).UTC().Format(time.RFC3339)
	errorBody := []byte(`{"ok":false,"error":"usage limit reached","errorCode":"usage_limit_reached","limit":{"limitKind":"usage_limit","plan":"pilot","resetAt":"` + resetAt + `"}}`)
	server := newPerchTestServer(t, perchTestOptions{ModelStatus: http.StatusTooManyRequests, ModelErrorBody: errorBody})
	handler := newTestHandler(server)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(chatRequestBody(t, false))))
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	raw := recorder.Header().Get("retry-after")
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("retry-after = %q", raw)
	}
	if seconds < 60 || seconds > 91 {
		t.Fatalf("retry-after = %d, want about 90", seconds)
	}
	if server.modelHits != 1 {
		t.Fatalf("model hits = %d, want 1", server.modelHits)
	}
}
