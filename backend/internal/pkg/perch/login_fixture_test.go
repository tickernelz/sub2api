package perch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loginFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "login", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return strings.TrimRight(string(raw), "\n")
}

func TestAuthorizeURLMatchesCLIFixture(t *testing.T) {
	cases := []struct {
		provider string
		fixture  string
	}{
		{ProviderGoogle, "authorize-google.txt"},
		{ProviderGitHub, "authorize-github.txt"},
	}
	for _, tc := range cases {
		got, err := BuildAuthorizeURL("https://fixture.supabase.co", tc.provider, "http://127.0.0.1:45999/callback", "FIXTURE_CHALLENGE")
		if err != nil {
			t.Fatalf("%s: %v", tc.provider, err)
		}
		want := loginFixture(t, tc.fixture)
		if got != want {
			t.Fatalf("%s authorize url byte mismatch\n got %q\nwant %q", tc.provider, got, want)
		}
	}
}

func TestExchangeRequestMatchesCLIFixture(t *testing.T) {
	flow := LoginFlow{
		SupabaseURL:     "https://fixture.supabase.co",
		SupabaseAnonKey: "FIXTURE_ANON_KEY",
		CodeVerifier:    "FIXTURE_VERIFIER",
		RedirectURI:     "http://127.0.0.1:45999/callback",
		Provider:        ProviderGoogle,
		CreatedAt:       time.Unix(1793300000, 0),
	}
	req, err := NewExchangeRequest(context.Background(), flow, "FIXTURE_CODE")
	if err != nil {
		t.Fatalf("build exchange request: %v", err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	got := strings.Join([]string{
		req.Method + " " + req.URL.Path + "?" + req.URL.RawQuery,
		"content-type: " + req.Header.Get("content-type"),
		"apikey: " + req.Header.Get("apikey"),
		"authorization: " + req.Header.Get("authorization"),
		"",
		string(body),
	}, "\n")
	want := loginFixture(t, "exchange-pkce.txt")
	if got != want {
		t.Fatalf("exchange request byte mismatch\n got %q\nwant %q", got, want)
	}
	if req.Method != http.MethodPost {
		t.Fatalf("method: %s", req.Method)
	}
}

func TestExchangeRejectsUsedAndExpiredCode(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantReason string
	}{
		{"used", http.StatusNotFound, `{"error":"invalid flow state","error_description":"invalid flow state, no valid flow state found"}`, "invalid flow state"},
		{"expired", http.StatusBadRequest, `{"error":"invalid_grant","error_description":"invalid flow state, flow state has expired"}`, "invalid_grant"},
	}
	for _, tc := range cases {
		server := newSupabaseTokenServer(t, tc.status, tc.body)
		client := NewLoginClient()
		_, err := client.Exchange(context.Background(), LoginFlow{SupabaseURL: server.URL, SupabaseAnonKey: "anon", CodeVerifier: "VER"}, "CODE")
		server.Close()
		var apiErr *APIError
		if err == nil || !errors.As(err, &apiErr) {
			t.Fatalf("%s: want APIError, got %v", tc.name, err)
		}
		if apiErr.Code != tc.wantReason {
			t.Fatalf("%s: code %q want %q", tc.name, apiErr.Code, tc.wantReason)
		}
		if strings.Contains(apiErr.Message, "access_token") {
			t.Fatalf("%s: error leaked a token: %q", tc.name, apiErr.Message)
		}
	}
}

func newSupabaseTokenServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/token" || r.URL.Query().Get("grant_type") != "pkce" {
			t.Errorf("unexpected exchange path %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}
