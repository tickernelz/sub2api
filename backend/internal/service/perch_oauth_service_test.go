package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/perch"
)

func newPerchTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	mux.HandleFunc("/api/perch-terminal/cli-auth/config", func(w http.ResponseWriter, r *http.Request) {
		payload, _ := json.Marshal(map[string]any{
			"ok":              true,
			"appUrl":          server.URL,
			"supabaseUrl":     server.URL,
			"supabaseAnonKey": "anon-key",
			"providers":       []string{"google", "github"},
			"redirectHost":    "127.0.0.1",
		})
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/auth/v1/token", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("grant_type") != "pkce" {
			t.Errorf("grant_type: %q", r.URL.Query().Get("grant_type"))
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"AT","refresh_token":"RT","expires_at":1793266236,"user":{"id":"U1","email":"ops@example.com"}}`))
	})
	return server
}

func TestPerchOAuthServiceAuthURLAndExchange(t *testing.T) {
	server := newPerchTestServer(t)
	defer server.Close()

	svc := NewPerchOAuthService()
	started, err := svc.GenerateAuthURL(context.Background(), PerchAuthURLInput{AdminID: 11, Provider: "google", AppURL: server.URL})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.SessionID == "" || len(started.SessionID) != 32 {
		t.Fatalf("session id shape: %q", started.SessionID)
	}
	wantPrefix := server.URL + "/auth/v1/authorize?provider=google&redirect_to=http%3A%2F%2F127.0.0.1%3A45999%2Fcallback&code_challenge="
	if !strings.HasPrefix(started.AuthURL, wantPrefix) {
		t.Fatalf("auth url: %s", started.AuthURL)
	}
	if !strings.HasSuffix(started.AuthURL, "&code_challenge_method=s256&prompt=select_account") {
		t.Fatalf("auth url tail: %s", started.AuthURL)
	}
	if started.RedirectURI != "http://127.0.0.1:45999/callback" || started.Provider != "google" {
		t.Fatalf("start payload: %+v", started)
	}
	if started.ExpiresIn != int(perch.LoginSessionTTL.Seconds()) {
		t.Fatalf("expires_in: %d", started.ExpiresIn)
	}

	_, err = svc.ExchangeCode(context.Background(), PerchExchangeInput{AdminID: 12, SessionID: started.SessionID, CallbackURL: "http://127.0.0.1:45999/callback?code=C1"})
	if err == nil || infraerrors.Code(err) != http.StatusForbidden {
		t.Fatalf("foreign admin must be forbidden, got %v", err)
	}
	if reason := infraerrors.Reason(err); reason != "PERCH_OAUTH_SESSION_FORBIDDEN" {
		t.Fatalf("reason: %q", reason)
	}

	result, err := svc.ExchangeCode(context.Background(), PerchExchangeInput{AdminID: 11, SessionID: started.SessionID, CallbackURL: "http://127.0.0.1:45999/callback?code=C1"})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if result.AccessToken != "AT" || result.RefreshToken != "RT" || result.Email != "ops@example.com" || result.ExpiresAt != 1793266236 {
		t.Fatalf("exchange payload: %+v", result)
	}
	if result.AppURL != server.URL {
		t.Fatalf("app url: %q", result.AppURL)
	}

	if _, err := svc.ExchangeCode(context.Background(), PerchExchangeInput{AdminID: 11, SessionID: started.SessionID, CallbackURL: "http://127.0.0.1:45999/callback?code=C1"}); err == nil || infraerrors.Code(err) != http.StatusNotFound {
		t.Fatalf("reused session must be rejected, got %v", err)
	}
}

func TestPerchOAuthServiceRejectsUnknownSessionAndProvider(t *testing.T) {
	server := newPerchTestServer(t)
	defer server.Close()
	svc := NewPerchOAuthService()

	if _, err := svc.ExchangeCode(context.Background(), PerchExchangeInput{AdminID: 1, SessionID: "nope", Code: "c"}); err == nil || infraerrors.Code(err) != http.StatusNotFound {
		t.Fatalf("unknown session: %v", err)
	}
	if _, err := svc.GenerateAuthURL(context.Background(), PerchAuthURLInput{AdminID: 1, Provider: "email", AppURL: server.URL}); err == nil || infraerrors.Code(err) != http.StatusBadRequest {
		t.Fatalf("unsupported provider: %v", err)
	}
	if _, err := svc.GenerateAuthURL(context.Background(), PerchAuthURLInput{AdminID: 1, Provider: "google", AppURL: "http://127.0.0.1:1"}); err == nil {
		t.Fatal("unreachable perch app must fail")
	}
}
