package service

import "testing"

func TestCursorAccountHelpersStripWrappedTokenAndStayNativeOnly(t *testing.T) {
	account := &Account{
		Platform: PlatformCursor,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "user-123::cursor-secret-token",
		},
	}

	if !account.IsCursor() {
		t.Fatalf("expected Cursor account helper to identify platform")
	}
	if account.IsOpenAICompatibleRuntime() {
		t.Fatalf("Cursor must not be treated as OpenAI-compatible runtime")
	}
	if account.IsOpenAICompatibleAPIKey() {
		t.Fatalf("Cursor must not be treated as an OpenAI-compatible API key account")
	}
	if got := account.GetOpenAICompatibleAPIKey(); got != "" {
		t.Fatalf("OpenAI-compatible API key = %q, want empty", got)
	}
	if got := account.GetOpenAICompatibleBaseURL(); got != "" {
		t.Fatalf("OpenAI-compatible base URL = %q, want empty", got)
	}
	if got := account.GetCursorAccessToken(); got != "cursor-secret-token" {
		t.Fatalf("Cursor access token = %q", got)
	}
	if got := account.GetCursorUserID(); got != "user-123" {
		t.Fatalf("Cursor user ID = %q", got)
	}
}

func TestCursorAccountHelpersPreferExplicitUserID(t *testing.T) {
	account := &Account{
		Platform: PlatformCursor,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "wrapped-user::cursor-secret-token",
			"user_id":      "explicit-user",
		},
	}

	if got := account.GetCursorUserID(); got != "explicit-user" {
		t.Fatalf("Cursor user ID = %q", got)
	}
}
