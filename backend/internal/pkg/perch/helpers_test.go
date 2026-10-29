package perch

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func freshExpiry(t *testing.T, raw []byte) []byte {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw
	}
	if _, ok := payload["expires_at"]; !ok {
		return raw
	}
	payload["expires_at"] = time.Now().Add(time.Hour).Unix()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return encoded
}

func writeFixtureJSON(t *testing.T, w http.ResponseWriter, status int, raw []byte) {
	t.Helper()
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(raw); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func decodeBody(t *testing.T, r *http.Request, out any) {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode body: %v (%s)", err, string(raw))
	}
}

type perchTestServer struct {
	*httptest.Server
	configHits        int
	ticketHits        int
	modelHits         int
	refreshHits       int
	accountHits       int
	lastModelReq      ModelCallRequest
	lastTicket        map[string]string
	lastTicketHeaders http.Header
	lastHeaders       http.Header
	stall             chan struct{}
	ticketCalls       map[string]int
	mu                sync.Mutex
}

func (s *perchTestServer) maxTicketCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	max := 0
	for _, calls := range s.ticketCalls {
		if calls > max {
			max = calls
		}
	}
	return max
}

func (s *perchTestServer) ticketMints() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ticketHits
}

type perchTestOptions struct {
	StreamBody        []byte
	ModelStatus       int
	ModelErrorBody    []byte
	AccountBody       []byte
	RefreshBody       []byte
	RefreshStatus     int
	FirstModelFails   bool
	FirstTicketFails  bool
	TicketAlwaysFails bool
	AbortAfterDelta   bool
	StallAfterDelta   bool
}

func newPerchTestServer(t *testing.T, opts perchTestOptions) *perchTestServer {
	t.Helper()
	ts := &perchTestServer{stall: make(chan struct{})}
	mux := http.NewServeMux()
	streamBody := opts.StreamBody
	if streamBody == nil {
		streamBody = fixture(t, "model_call_stream.sse")
	}
	refreshBody := opts.RefreshBody
	if refreshBody == nil {
		refreshBody = fixture(t, "supabase_refresh.json")
	}
	refreshStatus := opts.RefreshStatus
	if refreshStatus == 0 {
		refreshStatus = http.StatusOK
	}
	accountBody := opts.AccountBody
	if accountBody == nil {
		accountBody = fixture(t, "account.json")
	}

	mux.HandleFunc("/api/perch-terminal/cli-auth/config", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.configHits++
		ts.mu.Unlock()
		payload, err := json.Marshal(map[string]any{
			"ok":              true,
			"appUrl":          ts.URL,
			"supabaseUrl":     ts.URL,
			"supabaseAnonKey": "sb_publishable_test",
			"providers":       []string{"google", "github"},
			"redirectHost":    "127.0.0.1",
		})
		if err != nil {
			t.Errorf("encode config: %v", err)
		}
		writeFixtureJSON(t, w, http.StatusOK, payload)
	})
	mux.HandleFunc("/auth/v1/token", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.refreshHits++
		ts.mu.Unlock()
		if got := r.URL.Query().Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if r.Header.Get("apikey") == "" {
			t.Errorf("refresh request missing apikey header")
		}
		writeFixtureJSON(t, w, refreshStatus, freshExpiry(t, refreshBody))
	})
	mux.HandleFunc("/api/perch-terminal/turn-ticket", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.ticketHits++
		ticketHits := ts.ticketHits
		ts.mu.Unlock()
		if opts.TicketAlwaysFails || (opts.FirstTicketFails && ticketHits == 1) {
			writeFixtureJSON(t, w, http.StatusUnauthorized, []byte(`{"ok":false,"error":"not_authenticated","errorCode":"not_authenticated"}`))
			return
		}
		body := map[string]string{}
		decodeBody(t, r, &body)
		ts.mu.Lock()
		ts.lastTicket = body
		ts.lastTicketHeaders = r.Header.Clone()
		ts.mu.Unlock()
		payload, err := json.Marshal(map[string]any{
			"ok":        true,
			"ticket":    fmt.Sprintf("ticket-%d", ticketHits),
			"ticketId":  "44444444-4444-4444-8444-444444444444",
			"runId":     "tkt-cli-1793207619077-1li5qu4h",
			"surface":   "cli",
			"profile":   "standard",
			"enforced":  false,
			"expiresAt": time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
		})
		if err != nil {
			t.Errorf("encode ticket: %v", err)
		}
		writeFixtureJSON(t, w, http.StatusOK, payload)
	})
	mux.HandleFunc("/api/perch-terminal/model-call", func(w http.ResponseWriter, r *http.Request) {
		var modelReq ModelCallRequest
		decodeBody(t, r, &modelReq)
		ts.mu.Lock()
		ts.modelHits++
		modelHits := ts.modelHits
		ts.lastModelReq = modelReq
		ts.lastHeaders = r.Header.Clone()
		if ts.ticketCalls == nil {
			ts.ticketCalls = make(map[string]int)
		}
		ts.ticketCalls[r.Header.Get("x-perch-turn-ticket")]++
		ts.mu.Unlock()
		if opts.FirstModelFails && modelHits == 1 {
			writeFixtureJSON(t, w, http.StatusUnauthorized, []byte(`{"ok":false,"error":"Unauthorized","errorCode":"not_authenticated"}`))
			return
		}
		if opts.ModelStatus != 0 {
			body := opts.ModelErrorBody
			if body == nil {
				body = fixture(t, "model_call_error.json")
			}
			writeFixtureJSON(t, w, opts.ModelStatus, body)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if !opts.AbortAfterDelta && !opts.StallAfterDelta {
			if _, err := w.Write(streamBody); err != nil {
				t.Errorf("write stream: %v", err)
			}
			return
		}
		delta := []byte("data: {\"type\":\"answer_delta\",\"text\":\"partial\"}\n\n")
		if _, err := w.Write(delta); err != nil {
			t.Errorf("write delta: %v", err)
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		if opts.AbortAfterDelta {
			panic(http.ErrAbortHandler)
		}
		select {
		case <-ts.stall:
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("/api/perchai/account", func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.accountHits++
		ts.mu.Unlock()
		writeFixtureJSON(t, w, http.StatusOK, accountBody)
	})

	ts.Server = httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func testCredentials(appURL string) Credentials {
	return Credentials{
		AppURL:       appURL,
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    0,
		UserID:       "11111111-1111-4111-8111-111111111111",
		Email:        "sample@example.com",
	}
}
