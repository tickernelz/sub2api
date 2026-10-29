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

func newPerchGatewayServer(t *testing.T, text string, modelStatus int, errorCode string) *perchFakeServer {
	t.Helper()
	fake := &perchFakeServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/perch-terminal/public-model-default":
			w.Header().Set("content-type", "application/json")
			fmt.Fprint(w, `{"ok":true,"view":"public","selection":{"chatModelId":"wandb-kimi-k2-6"},"source":"database"}`)
		case "/api/perch-terminal/cli-auth/config":
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"appUrl":%q,"supabaseUrl":%q,"supabaseAnonKey":"sb_publishable_test"}`, fake.URL, fake.URL)
		case "/auth/v1/token":
			w.Header().Set("content-type", "application/json")
			fmt.Fprint(w, `{"access_token":"access-after-refresh","refresh_token":"refresh-after","expires_in":3600}`)
		case "/api/perch-terminal/turn-ticket":
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"ticket":"tkt","ticketId":"tid","runId":"tkt-cli-test","profile":"standard","expiresAt":%q}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		case "/api/perch-terminal/model-call":
			fake.modelCalls++
			if modelStatus != 0 {
				w.Header().Set("content-type", "application/json")
				w.WriteHeader(modelStatus)
				fmt.Fprintf(w, `{"ok":false,"error":"perch refused","errorCode":%q}`, errorCode)
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
			perchAppURLKey:       server.URL,
			perchAccessTokenKey:  "access-token",
			perchRefreshTokenKey: "refresh-token",
			perchExpiresAtKey:    time.Now().Add(time.Hour).Unix(),
		},
	}
}

func withPerchTestFactory(t *testing.T, server *httptest.Server) {
	t.Helper()
	previous := perchClientFactory
	perchClientFactory = func(creds perch.Credentials, store perch.CredentialStore) *perch.Client {
		return perch.NewClient(
			perch.NewSession(creds, perch.WithHTTPClient(server.Client())),
			perch.WithRetryPolicy(perch.DefaultMaxAttempts, []time.Duration{0, 0}),
		)
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
	server := newPerchGatewayServer(t, "pong", 0, "")
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
	server := newPerchGatewayServer(t, "pong", 0, "")
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

func TestForwardPerchChatCompletionsKeepsSurfaceRefusalOnTheAccount(t *testing.T) {
	server := newPerchGatewayServer(t, "pong", http.StatusTooManyRequests, "perch_surface_required")
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
	if errors.As(err, &failover) {
		t.Fatalf("a surface refusal moved the request to another account: %+v", failover)
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "perch_surface_required") {
		t.Fatalf("body = %s, want the perch code", recorder.Body.String())
	}
	if server.modelCalls != 1 {
		t.Fatalf("model calls = %d, want exactly 1", server.modelCalls)
	}
}

func TestForwardPerchChatCompletionsKeepsRequestErrors(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			server := newPerchGatewayServer(t, "pong", http.StatusBadRequest, "context_overflow")
			withPerchTestFactory(t, server.Server)
			account := newPerchAccountForTest(server.Server)
			service := &OpenAIGatewayService{}

			body := `{"model":"perch-standard","messages":[{"role":"user","content":"ping"}]}`
			if stream {
				body = `{"model":"perch-standard","stream":true,"messages":[{"role":"user","content":"ping"}]}`
			}
			c, recorder := newPerchGinContext(t, body)
			result, err := service.forwardPerchChatCompletions(c.Request.Context(), c, account, []byte(body), "")
			if result != nil {
				t.Fatalf("result = %+v, want nil", result)
			}
			var failover *UpstreamFailoverError
			if errors.As(err, &failover) {
				t.Fatalf("request-scoped error became a failover: %+v", failover)
			}
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", recorder.Code)
			}
			if !strings.Contains(recorder.Body.String(), "context_overflow") {
				t.Fatalf("body = %s, want the perch code", recorder.Body.String())
			}
			if server.modelCalls != 1 {
				t.Fatalf("model calls = %d, want exactly 1", server.modelCalls)
			}
		})
	}
}

func TestForwardPerchChatCompletionsRetriesUpstreamOutageOnTheSameAccount(t *testing.T) {
	server := newPerchGatewayServer(t, "pong", http.StatusServiceUnavailable, "upstream_unavailable")
	withPerchTestFactory(t, server.Server)
	account := newPerchAccountForTest(server.Server)
	service := &OpenAIGatewayService{}

	body := `{"model":"perch-standard","stream":true,"messages":[{"role":"user","content":"ping"}]}`
	c, recorder := newPerchGinContext(t, body)
	_, err := service.forwardPerchChatCompletions(c.Request.Context(), c, account, []byte(body), "")
	var failover *UpstreamFailoverError
	if errors.As(err, &failover) {
		t.Fatalf("an upstream outage moved the request to another account: %+v", failover)
	}
	if server.modelCalls != perch.DefaultMaxAttempts {
		t.Fatalf("model calls = %d, want %d attempts on one account", server.modelCalls, perch.DefaultMaxAttempts)
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func newPerchSSEErrorServer(t *testing.T, code string, viaDone bool) *perchFakeServer {
	t.Helper()
	fake := &perchFakeServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/perch-terminal/public-model-default":
			w.Header().Set("content-type", "application/json")
			fmt.Fprint(w, `{"ok":true,"view":"public","selection":{"chatModelId":"wandb-kimi-k2-6"},"source":"database"}`)
		case "/api/perch-terminal/cli-auth/config":
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"appUrl":%q,"supabaseUrl":%q,"supabaseAnonKey":"sb_publishable_test"}`, fake.URL, fake.URL)
		case "/auth/v1/token":
			w.Header().Set("content-type", "application/json")
			fmt.Fprint(w, `{"access_token":"access-after-refresh","refresh_token":"refresh-after","expires_in":3600}`)
		case "/api/perch-terminal/turn-ticket":
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"ticket":"tkt","ticketId":"tid","runId":"tkt-cli-test","profile":"standard","expiresAt":%q}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		case "/api/perch-terminal/model-call":
			fake.modelCalls++
			w.Header().Set("content-type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if viaDone {
				fmt.Fprintf(w, "data: {\"type\":\"done\",\"ok\":false,\"error\":%s,\"errorCode\":%q}\n\n", quoteJSON(code), code)
			} else {
				fmt.Fprintf(w, "data: {\"type\":\"error\",\"message\":%s,\"errorCode\":%q}\n\n", quoteJSON(code), code)
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func TestForwardPerchChatCompletionsKeepsStreamedRequestErrors(t *testing.T) {
	for _, viaDone := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			name := fmt.Sprintf("done=%v/stream=%v", viaDone, stream)
			t.Run(name, func(t *testing.T) {
				server := newPerchSSEErrorServer(t, "context_overflow", viaDone)
				withPerchTestFactory(t, server.Server)
				account := newPerchAccountForTest(server.Server)
				service := &OpenAIGatewayService{}

				body := `{"model":"perch-standard","messages":[{"role":"user","content":"ping"}]}`
				if stream {
					body = `{"model":"perch-standard","stream":true,"messages":[{"role":"user","content":"ping"}]}`
				}
				c, recorder := newPerchGinContext(t, body)
				_, err := service.forwardPerchChatCompletions(c.Request.Context(), c, account, []byte(body), "")
				var failover *UpstreamFailoverError
				if errors.As(err, &failover) {
					t.Fatalf("request-scoped stream error became a failover: %+v", failover)
				}
				if recorder.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400", recorder.Code)
				}
				if server.modelCalls != 1 {
					t.Fatalf("model calls = %d, want exactly 1", server.modelCalls)
				}
			})
		}
	}
}

func TestPerchRejectsUnsupportedGatewayEntries(t *testing.T) {
	server := newPerchGatewayServer(t, "pong", 0, "")
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

func TestPerchFailoverPolicyPerCode(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		code         string
		wantFailover bool
		wantStatus   int
		wantCalls    int
	}{
		{"not_authenticated", http.StatusUnauthorized, "not_authenticated", true, 0, 2},
		{"invalid_grant", http.StatusUnauthorized, "invalid_grant", true, 0, 2},
		{"turn_rate_limited", http.StatusTooManyRequests, "turn_rate_limited", false, http.StatusTooManyRequests, 1},
		{"usage_limit_reached", http.StatusTooManyRequests, "usage_limit_reached", false, http.StatusTooManyRequests, 1},
		{"surface_required", http.StatusTooManyRequests, "perch_surface_required", false, http.StatusForbidden, 1},
		{"upstream_outage", http.StatusServiceUnavailable, "upstream_unavailable", false, http.StatusServiceUnavailable, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newPerchGatewayServer(t, "pong", tc.status, tc.code)
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
			moved := errors.As(err, &failover)
			if moved != tc.wantFailover {
				t.Fatalf("failover = %v, want %v (err %v)", moved, tc.wantFailover, err)
			}
			if tc.wantFailover {
				if recorder.Body.Len() != 0 {
					t.Fatalf("client received %q, a failover must stay silent", recorder.Body.String())
				}
			} else {
				if recorder.Code != tc.wantStatus {
					t.Fatalf("status = %d, want %d", recorder.Code, tc.wantStatus)
				}
				if !strings.Contains(recorder.Body.String(), tc.code) {
					t.Fatalf("body = %s, want the perch code %s", recorder.Body.String(), tc.code)
				}
				if tc.code != "context_overflow" && strings.Contains(recorder.Body.String(), "perch refused") {
					t.Fatalf("body = %s, want a generic message instead of upstream detail", recorder.Body.String())
				}
			}
			if server.modelCalls != tc.wantCalls {
				t.Fatalf("model calls = %d, want %d", server.modelCalls, tc.wantCalls)
			}
		})
	}
}
