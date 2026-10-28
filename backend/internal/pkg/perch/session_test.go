package perch

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAccessTokenSkipsRefreshWhenExpiryIsFar(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(2 * time.Hour).Unix()

	session := NewSession(creds, WithHTTPClient(server.Client()))

	token, err := session.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if token != "access-token" {
		t.Fatalf("token = %q, want access-token", token)
	}
	if server.refreshHits != 0 || server.configHits != 0 {
		t.Fatalf("refresh should not run: refresh=%d config=%d", server.refreshHits, server.configHits)
	}
}

func TestAccessTokenRefreshesInsideSkewAndPersists(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(10 * time.Second).Unix()

	var persisted atomic.Pointer[Credentials]
	session := NewSession(creds,
		WithHTTPClient(server.Client()),
		WithCredentialsPersist(func(next Credentials) { persisted.Store(&next) }),
	)

	token, err := session.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if token != "access-after-refresh" {
		t.Fatalf("token = %q, want access-after-refresh", token)
	}
	if server.refreshHits != 1 {
		t.Fatalf("refresh hits = %d, want 1", server.refreshHits)
	}
	got := session.Credentials()
	if got.RefreshToken != "refresh-after-rotate" {
		t.Fatalf("refresh token = %q, want rotated value", got.RefreshToken)
	}
	if got.ExpiresAt != 1793212800 {
		t.Fatalf("expires_at = %d, want 1793212800", got.ExpiresAt)
	}
	if persisted.Load() == nil || persisted.Load().AccessToken != "access-after-refresh" {
		t.Fatalf("persist hook did not receive refreshed credentials")
	}
}

func TestAccessTokenSingleflightRefresh(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(5 * time.Second).Unix()

	session := NewSession(creds, WithHTTPClient(server.Client()))

	var wg sync.WaitGroup
	tokens := make([]string, 8)
	errs := make([]error, 8)
	for i := range tokens {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tokens[idx], errs[idx] = session.AccessToken(context.Background())
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
		if tokens[i] != "access-after-refresh" {
			t.Fatalf("goroutine %d token = %q", i, tokens[i])
		}
	}
	if server.refreshHits != 1 {
		t.Fatalf("refresh hits = %d, want 1", server.refreshHits)
	}
}

func TestRefreshRejectsRotatedReuseError(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{
		RefreshBody:   []byte(`{"error":"invalid_grant","error_description":"Invalid Refresh Token"}`),
		RefreshStatus: http.StatusBadRequest,
	})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(time.Second).Unix()

	session := NewSession(creds, WithHTTPClient(server.Client()))

	_, err := session.AccessToken(context.Background())
	if err == nil {
		t.Fatalf("expected refresh failure")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.Code != "invalid_grant" {
		t.Fatalf("code = %q, want invalid_grant", apiErr.Code)
	}
	if apiErr.HTTPStatus() != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", apiErr.HTTPStatus())
	}
	if apiErr.OpenAIType() != "authentication_error" {
		t.Fatalf("openai type = %q, want authentication_error", apiErr.OpenAIType())
	}
}
