package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/perch"
	"github.com/gin-gonic/gin"
)

type perchFakeServer struct {
	*httptest.Server
	modelCalls int
}

func newPerchGatewayServer(t *testing.T, text string, modelStatus int) *perchFakeServer {
	t.Helper()
	fake := &perchFakeServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/perch-terminal/turn-ticket":
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"ticket":"tkt","ticketId":"tid","runId":"tkt-cli-test","profile":"standard","expiresAt":%q}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		case "/api/perch-terminal/model-call":
			fake.modelCalls++
			if modelStatus != 0 {
				w.Header().Set("content-type", "application/json")
				w.WriteHeader(modelStatus)
				fmt.Fprint(w, `{"ok":false,"error":"perch refused","errorCode":"perch_surface_required"}`)
				return
			}
			w.Header().Set("content-type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			fmt.Fprintf(w, "data: {\"type\":\"answer_delta\",\"text\":%s}\n\n", quoteJSON(text))
			fmt.Fprint(w, "data: {\"type\":\"done\",\"ok\":true,\"text\":"+quoteJSON(text)+",\"model\":\"deepseek/deepseek-v4.1-flash\",\"finishReason\":\"stop\",\"usage\":{\"inputTokens\":120,\"outputTokens\":7,\"cacheReadInputTokens\":900,\"totalTokens\":1027}}\n\n")
			flusher.Flush()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
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
	server := newPerchGatewayServer(t, "pong", 0)
	withPerchTestFactory(t, server.Server)
	account := newPerchAccountForTest(server.Server)
	service := &OpenAIGatewayService{}

	body := `{"model":"perch-standard","messages":[{"role":"user","content":"ping"}]}`
	c, recorder := newPerchGinContext(t, body)
	result, err := service.forwardPerchChatCompletions(c.Request.Context(), c, account, []byte(body), "")
	if err != nil {
		t.Fatalf("forward: %v (%s)", err, recorder.Body.String())
	}
	if result == nil {
		t.Fatalf("result is nil")
	}
	inputTokens, cacheReadTokens, cacheCreationTokens := openAIUsageBilledBuckets(result.Usage)
	if inputTokens != 120 || cacheReadTokens != 900 || cacheCreationTokens != 0 || result.Usage.OutputTokens != 7 {
		t.Fatalf("buckets = %d/%d/%d/%d, want 120/900/0/7", inputTokens, cacheReadTokens, cacheCreationTokens, result.Usage.OutputTokens)
	}
	if !strings.Contains(recorder.Body.String(), "pong") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestForwardPerchChatCompletionsStream(t *testing.T) {
	server := newPerchGatewayServer(t, "pong", 0)
	withPerchTestFactory(t, server.Server)
	account := newPerchAccountForTest(server.Server)
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
	if result == nil {
		t.Fatalf("result is nil")
	}
	inputTokens, cacheReadTokens, _ := openAIUsageBilledBuckets(result.Usage)
	if inputTokens != 120 || cacheReadTokens != 900 {
		t.Fatalf("buckets = %d/%d, want 120/900", inputTokens, cacheReadTokens)
	}
}

func TestForwardPerchChatCompletionsFailsOverOnUpstreamRefusal(t *testing.T) {
	server := newPerchGatewayServer(t, "pong", http.StatusTooManyRequests)
	withPerchTestFactory(t, server.Server)
	account := newPerchAccountForTest(server.Server)
	service := &OpenAIGatewayService{}

	body := `{"model":"perch-standard","messages":[{"role":"user","content":"ping"}]}`
	c, recorder := newPerchGinContext(t, body)
	result, err := service.forwardPerchChatCompletions(c.Request.Context(), c, account, []byte(body), "")
	if result != nil {
		t.Fatalf("result = %+v, want nil", result)
	}
	var failover *UpstreamFailoverError
	if !errors.As(err, &failover) {
		t.Fatalf("err = %v (%T), want *UpstreamFailoverError", err, err)
	}
	if failover.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("failover status = %d, want 429", failover.StatusCode)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("client already received %q, failover must stay silent", recorder.Body.String())
	}
}

func TestForwardPerchChatCompletionsStreamFailsOverBeforeOutput(t *testing.T) {
	server := newPerchGatewayServer(t, "pong", http.StatusServiceUnavailable)
	withPerchTestFactory(t, server.Server)
	account := newPerchAccountForTest(server.Server)
	service := &OpenAIGatewayService{}

	body := `{"model":"perch-standard","stream":true,"messages":[{"role":"user","content":"ping"}]}`
	c, recorder := newPerchGinContext(t, body)
	_, err := service.forwardPerchChatCompletions(c.Request.Context(), c, account, []byte(body), "")
	var failover *UpstreamFailoverError
	if !errors.As(err, &failover) {
		t.Fatalf("err = %v (%T), want *UpstreamFailoverError", err, err)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("client already received %q before failover", recorder.Body.String())
	}
}

func TestPerchRejectsUnsupportedGatewayEntries(t *testing.T) {
	server := newPerchGatewayServer(t, "pong", 0)
	withPerchTestFactory(t, server.Server)
	account := newPerchAccountForTest(server.Server)
	service := &OpenAIGatewayService{}

	c, recorder := newPerchGinContext(t, `{"model":"perch-standard","input":"hi"}`)
	if _, err := service.Forward(c.Request.Context(), c, account, []byte(`{"model":"perch-standard","input":"hi"}`)); err == nil {
		t.Fatalf("Forward accepted a perch account")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("responses status = %d, want 400", recorder.Code)
	}
	if server.modelCalls != 0 {
		t.Fatalf("perch upstream was called %d times", server.modelCalls)
	}

	c2, recorder2 := newPerchGinContext(t, `{"model":"perch-standard","input":"hi"}`)
	if _, err := service.ForwardEmbeddings(c2.Request.Context(), c2, account, []byte(`{"model":"perch-standard","input":"hi"}`), ""); err == nil {
		t.Fatalf("ForwardEmbeddings accepted a perch account")
	}
	if recorder2.Code != http.StatusBadRequest {
		t.Fatalf("embeddings status = %d, want 400", recorder2.Code)
	}
}
