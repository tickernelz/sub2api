package perch

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
}

type perchTestOptions struct {
	StreamBody       []byte
	ModelStatus      int
	ModelErrorBody   []byte
	AccountBody      []byte
	RefreshBody      []byte
	RefreshStatus    int
	FirstModelFails  bool
	FirstTicketFails bool
}

func newPerchTestServer(t *testing.T, opts perchTestOptions) *perchTestServer {
	t.Helper()
	ts := &perchTestServer{}
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
		ts.configHits++
		payload, err := json.Marshal(map[string]any{
			"ok":              true,
			"appUrl":          ts.URL,
			"supabaseUrl":     ts.URL,
			"supabaseAnonKey": "sb_publishable_test",
			"providers":       []string{"google", "github"},
			"redirectHost":    "127.0.0.1",
		})
		if err != nil {
			t.Fatalf("encode config: %v", err)
		}
		writeFixtureJSON(t, w, http.StatusOK, payload)
	})
	mux.HandleFunc("/auth/v1/token", func(w http.ResponseWriter, r *http.Request) {
		ts.refreshHits++
		if got := r.URL.Query().Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if r.Header.Get("apikey") == "" {
			t.Errorf("refresh request missing apikey header")
		}
		writeFixtureJSON(t, w, refreshStatus, freshExpiry(t, refreshBody))
	})
	mux.HandleFunc("/api/perch-terminal/turn-ticket", func(w http.ResponseWriter, r *http.Request) {
		ts.ticketHits++
		if opts.FirstTicketFails && ts.ticketHits == 1 {
			writeFixtureJSON(t, w, http.StatusUnauthorized, []byte(`{"ok":false,"error":"not_authenticated","errorCode":"not_authenticated"}`))
			return
		}
		body := map[string]string{}
		decodeBody(t, r, &body)
		ts.lastTicket = body
		ts.lastTicketHeaders = r.Header.Clone()
		payload, err := json.Marshal(map[string]any{
			"ok":        true,
			"ticket":    "ticket-value",
			"ticketId":  "44444444-4444-4444-8444-444444444444",
			"runId":     "tkt-cli-1793207619077-1li5qu4h",
			"surface":   "cli",
			"profile":   "standard",
			"enforced":  false,
			"expiresAt": time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
		})
		if err != nil {
			t.Fatalf("encode ticket: %v", err)
		}
		writeFixtureJSON(t, w, http.StatusOK, payload)
	})
	mux.HandleFunc("/api/perch-terminal/model-call", func(w http.ResponseWriter, r *http.Request) {
		ts.modelHits++
		decodeBody(t, r, &ts.lastModelReq)
		ts.lastHeaders = r.Header.Clone()
		if opts.FirstModelFails && ts.modelHits == 1 {
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
		if _, err := w.Write(streamBody); err != nil {
			t.Errorf("write stream: %v", err)
		}
	})
	mux.HandleFunc("/api/perchai/account", func(w http.ResponseWriter, r *http.Request) {
		ts.accountHits++
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
