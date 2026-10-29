package perch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildAuthorizeURLMatchesCLI(t *testing.T) {
	google, err := BuildAuthorizeURL("https://proj.supabase.co/", "google", "http://127.0.0.1:45999/callback", "CHAL")
	if err != nil {
		t.Fatalf("google url: %v", err)
	}
	wantGoogle := "https://proj.supabase.co/auth/v1/authorize?provider=google&redirect_to=http%3A%2F%2F127.0.0.1%3A45999%2Fcallback&code_challenge=CHAL&code_challenge_method=s256&prompt=select_account"
	if google != wantGoogle {
		t.Fatalf("google authorize url\n got %s\nwant %s", google, wantGoogle)
	}

	github, err := BuildAuthorizeURL("https://proj.supabase.co", "GITHUB", "http://127.0.0.1:45999/callback", "CHAL")
	if err != nil {
		t.Fatalf("github url: %v", err)
	}
	wantGitHub := "https://proj.supabase.co/auth/v1/authorize?provider=github&redirect_to=http%3A%2F%2F127.0.0.1%3A45999%2Fcallback&code_challenge=CHAL&code_challenge_method=s256"
	if github != wantGitHub {
		t.Fatalf("github authorize url\n got %s\nwant %s", github, wantGitHub)
	}
	if strings.Contains(github, "prompt=") {
		t.Fatalf("github url must not carry prompt: %s", github)
	}

	if _, err := BuildAuthorizeURL("https://proj.supabase.co", "email", "http://127.0.0.1:45999/callback", "CHAL"); err == nil {
		t.Fatal("unsupported provider must be rejected")
	}
	if _, err := BuildAuthorizeURL("", "google", "http://127.0.0.1:45999/callback", "CHAL"); err == nil {
		t.Fatal("empty supabase url must be rejected")
	}
}

func TestParseCallback(t *testing.T) {
	code, err := ParseCallback("http://127.0.0.1:45999/callback?code=abc-123")
	if err != nil || code != "abc-123" {
		t.Fatalf("full url parse: code=%q err=%v", code, err)
	}
	code, err = ParseCallback("  abc-123  ")
	if err != nil || code != "abc-123" {
		t.Fatalf("bare code parse: code=%q err=%v", code, err)
	}
	_, err = ParseCallback("http://127.0.0.1:45999/callback?error=access_denied&error_description=User%20denied")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "oauth_callback_error" {
		t.Fatalf("error callback must surface oauth_callback_error, got %v", err)
	}
	if !strings.Contains(apiErr.Message, "User denied") {
		t.Fatalf("error description must be carried: %q", apiErr.Message)
	}
	if _, err = ParseCallback("http://127.0.0.1:45999/callback"); err == nil {
		t.Fatal("callback without code must be rejected")
	}
	if _, err = ParseCallback(""); err == nil {
		t.Fatal("empty callback must be rejected")
	}
}

func TestLoginStoreSingleUseAndAdminBinding(t *testing.T) {
	store := NewLoginStore()
	now := time.Unix(1793300000, 0)
	store.now = func() time.Time { return now }
	ctx := context.Background()

	flow := LoginFlow{Provider: ProviderGoogle, CodeVerifier: "ver", RedirectURI: "http://127.0.0.1:45999/callback", CreatedAt: now}
	if err := store.Put(ctx, "sess-1", LoginSession{AdminID: 7, LoginFlow: flow}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := store.Take(ctx, "sess-1", 9); !errors.Is(err, ErrLoginSessionAdmin) {
		t.Fatalf("foreign admin must be rejected, got %v", err)
	}
	got, err := store.Take(ctx, "sess-1", 7)
	if err != nil {
		t.Fatalf("owner take: %v", err)
	}
	if got.CodeVerifier != "ver" || got.Provider != ProviderGoogle {
		t.Fatalf("session payload lost: %+v", got)
	}
	if _, err := store.Take(ctx, "sess-1", 7); !errors.Is(err, ErrLoginSessionNotFound) {
		t.Fatalf("second take must fail with not found, got %v", err)
	}

	if err := store.Put(ctx, "sess-2", LoginSession{AdminID: 7, LoginFlow: flow}); err != nil {
		t.Fatalf("put expired: %v", err)
	}
	now = now.Add(LoginSessionTTL + time.Second)
	if _, err := store.Take(ctx, "sess-2", 7); !errors.Is(err, ErrLoginSessionNotFound) {
		t.Fatalf("expired session must be rejected, got %v", err)
	}
	if _, err := store.Take(ctx, "missing", 7); !errors.Is(err, ErrLoginSessionNotFound) {
		t.Fatalf("unknown session must be rejected, got %v", err)
	}
}

func TestLoginExchangeSendsCLIShape(t *testing.T) {
	var rawBody string
	var method, path, grant string
	var apikey, authorization, contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		grant = r.URL.Query().Get("grant_type")
		apikey = r.Header.Get("apikey")
		authorization = r.Header.Get("authorization")
		contentType = r.Header.Get("content-type")
		body, _ := io.ReadAll(r.Body)
		rawBody = string(body)
		writeFixtureJSON(t, w, http.StatusOK, []byte(`{"access_token":"AT","refresh_token":"RT","expires_at":1793266236,"token_type":"bearer","user":{"id":"U1","email":"ops@example.com"}}`))
	}))
	defer server.Close()

	client := NewLoginClient()
	flow := LoginFlow{
		Provider:        ProviderGoogle,
		AppURL:          "https://app.perchai.app",
		SupabaseURL:     server.URL,
		SupabaseAnonKey: "anon-key",
		CodeVerifier:    "VER",
		CodeChallenge:   CodeChallengeS256("VER"),
		RedirectURI:     "http://127.0.0.1:45999/callback",
		CreatedAt:       time.Now(),
	}
	creds, err := client.Exchange(context.Background(), flow, "CODE-1")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if method != http.MethodPost || path != "/auth/v1/token" || grant != "pkce" {
		t.Fatalf("exchange request shape: %s %s grant=%q", method, path, grant)
	}
	if want := `{"auth_code":"CODE-1","code_verifier":"VER"}`; rawBody != want {
		t.Fatalf("exchange body\n got %s\nwant %s", rawBody, want)
	}
	if apikey != "anon-key" || authorization != "Bearer anon-key" || contentType != "application/json" {
		t.Fatalf("exchange headers apikey=%q authorization=%q content-type=%q", apikey, authorization, contentType)
	}
	if creds.AccessToken != "AT" || creds.RefreshToken != "RT" || creds.Email != "ops@example.com" || creds.ExpiresAt != 1793266236 {
		t.Fatalf("credentials not mapped: %+v", creds)
	}
	if creds.AppURL != "https://app.perchai.app" {
		t.Fatalf("app url not carried: %q", creds.AppURL)
	}
}

func TestLoginExchangeSurfacesUpstreamErrorWithoutTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFixtureJSON(t, w, http.StatusNotFound, []byte(`{"error":"invalid flow state","error_description":"invalid flow state, no valid flow state found"}`))
	}))
	defer server.Close()

	client := NewLoginClient()
	flow := LoginFlow{SupabaseURL: server.URL, SupabaseAnonKey: "anon-key", CodeVerifier: "VER"}
	_, err := client.Exchange(context.Background(), flow, "CODE-1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want APIError, got %v", err)
	}
	if apiErr.Status != http.StatusNotFound || apiErr.Code != "invalid flow state" {
		t.Fatalf("unexpected mapping: status=%d code=%q", apiErr.Status, apiErr.Code)
	}
	if strings.Contains(apiErr.Message, "access_token") {
		t.Fatalf("error message must not carry tokens: %q", apiErr.Message)
	}
	if _, err := client.Exchange(context.Background(), flow, "   "); err == nil {
		t.Fatal("empty code must be rejected before the network call")
	}
}

func TestLoginBeginUsesConfig(t *testing.T) {
	var hits int32
	var appURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/perch-terminal/cli-auth/config", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Header.Get("accept") != "application/json" {
			t.Errorf("config accept header: %q", r.Header.Get("accept"))
		}
		payload, _ := json.Marshal(map[string]any{
			"ok":              true,
			"appUrl":          "https://" + r.Host,
			"supabaseUrl":     "https://proj.supabase.co",
			"supabaseAnonKey": "anon-key",
			"providers":       []string{"github", "google"},
			"redirectHost":    "127.0.0.1",
		})
		writeFixtureJSON(t, w, http.StatusOK, payload)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	appURL = server.URL

	client := NewLoginClient()
	flow, providers, err := client.Begin(context.Background(), appURL, "", "")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("config endpoint hits: %d", hits)
	}
	if len(providers) != 2 || providers[0] != "github" || providers[1] != "google" {
		t.Fatalf("providers: %v", providers)
	}
	if flow.Provider != ProviderGitHub {
		t.Fatalf("default provider must be the first advertised one, got %q", flow.Provider)
	}
	if flow.RedirectURI != "http://127.0.0.1:45999/callback" {
		t.Fatalf("redirect uri: %q", flow.RedirectURI)
	}
	if flow.CodeChallenge != CodeChallengeS256(flow.CodeVerifier) {
		t.Fatalf("challenge does not derive from verifier")
	}
	authURL, err := client.AuthorizeURL(flow)
	if err != nil {
		t.Fatalf("authorize url: %v", err)
	}
	if !strings.HasPrefix(authURL, "https://proj.supabase.co/auth/v1/authorize?provider=github&redirect_to=") {
		t.Fatalf("authorize url: %s", authURL)
	}
	if _, _, err := client.Begin(context.Background(), appURL, "email", ""); err == nil {
		t.Fatal("provider outside the advertised list must be rejected")
	}
	if _, _, err := client.Begin(context.Background(), "http://127.0.0.1:1", "google", ""); err == nil {
		t.Fatal("unreachable app url must fail")
	}
}
