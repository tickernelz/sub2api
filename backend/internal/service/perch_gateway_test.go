package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/perch"
	"github.com/gin-gonic/gin"
)

func newPerchGatewayServer(t *testing.T, text string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/perch-terminal/turn-ticket":
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"ticket":"tkt","ticketId":"tid","runId":"tkt-cli-test","profile":"standard","expiresAt":%q}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		case "/api/perch-terminal/model-call":
			w.Header().Set("content-type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			fmt.Fprintf(w, "data: {\"type\":\"answer_delta\",\"text\":%q}\n\n", text)
			fmt.Fprint(w, "data: {\"type\":\"done\",\"ok\":true,\"text\":"+quoteJSON(text)+",\"model\":\"deepseek/deepseek-v4.1-flash\",\"finishReason\":\"stop\",\"usage\":{\"inputTokens\":120,\"outputTokens\":7,\"cacheReadInputTokens\":900,\"totalTokens\":1027}}\n\n")
			flusher.Flush()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func quoteJSON(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func newPerchAccountForTest(server *httptest.Server) *Account {
	return &Account{
		ID:       4242,
		Platform: PlatformPerch,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			perchAppURLKey:      server.URL,
			perchAccessTokenKey: "access-token",
			perchExpiresAtKey:   time.Now().Add(time.Hour).Unix(),
		},
	}
}

func withPerchTestFactory(t *testing.T, server *httptest.Server) {
	t.Helper()
	previous := perchClientFactory
	perchClientFactory = func(creds perch.Credentials, store perch.CredentialStore) *perch.Client {
		return perch.NewClient(perch.NewSession(creds, perch.WithHTTPClient(server.Client())))
	}
	t.Cleanup(func() { perchClientFactory = previous })
}

func newPerchGinContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("content-type", "application/json")
	return c, recorder
}

func TestForwardPerchChatCompletionsNonStream(t *testing.T) {
	server := newPerchGatewayServer(t, "pong")
	withPerchTestFactory(t, server)
	account := newPerchAccountForTest(server)
	service := &OpenAIGatewayService{}

	body := `{"model":"perch-standard","messages":[{"role":"user","content":"ping"}]}`
	c, recorder := newPerchGinContext(t, body)
	result, err := service.forwardPerchChatCompletions(c.Request.Context(), c, account, []byte(body), "")
	if err != nil {
		t.Fatalf("forward: %v (%s)", err, recorder.Body.String())
	}
	if result == nil || result.Usage.InputTokens != 1020 || result.Usage.OutputTokens != 7 {
		t.Fatalf("usage = %+v", result)
	}
	if !strings.Contains(recorder.Body.String(), "pong") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestForwardPerchChatCompletionsStream(t *testing.T) {
	server := newPerchGatewayServer(t, "pong")
	withPerchTestFactory(t, server)
	account := newPerchAccountForTest(server)
	service := &OpenAIGatewayService{}

	body := `{"model":"perch-standard","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"ping"}]}`
	c, recorder := newPerchGinContext(t, body)
	result, err := service.forwardPerchChatCompletions(c.Request.Context(), c, account, []byte(body), "")
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	payload := recorder.Body.String()
	if !strings.Contains(payload, "data: [DONE]") {
		t.Fatalf("stream ended without done marker: %s", payload)
	}
	if !strings.Contains(payload, "chat.completion.chunk") {
		t.Fatalf("stream carried no chunk: %s", payload)
	}
	if result == nil || result.Usage.OutputTokens != 7 {
		t.Fatalf("usage = %+v", result)
	}
}
