package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	openAIRefreshTokenStatusExtraKey             = "openai_refresh_token_status"
	openAIRefreshTokenStatusReused               = "reused"
	openAIRequiresReauthExtraKey                 = "openai_requires_reauth"
	openAIRefreshTokenReusedAtExtraKey           = "openai_refresh_token_reused_at"
	openAIRefreshTokenReusedTokenVersionExtraKey = "openai_refresh_token_reused_token_version"
	openAIRefreshTokenLastErrorExtraKey          = "openai_refresh_token_last_error"
	openAIRefreshTokenStatusOK                   = "ok"
	openAIRefreshTokenRecoveredAtExtraKey        = "openai_refresh_token_recovered_at"
	openAIRefreshTokenLastErrorMaxLength         = 512
)

func IsOpenAIRefreshTokenReusedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "refresh_token_reused")
}

func shouldSoftHandleOpenAIRefreshTokenReused(account *Account, err error) bool {
	return account != nil &&
		account.Platform == PlatformOpenAI &&
		account.Type == AccountTypeOAuth &&
		strings.TrimSpace(account.GetOpenAIAccessToken()) != "" &&
		IsOpenAIRefreshTokenReusedError(err)
}

func markOpenAIRefreshTokenReused(ctx context.Context, repo AccountRepository, account *Account, err error) {
	if repo == nil || account == nil {
		return
	}
	updates := OpenAIRefreshTokenReusedExtra(account, err, time.Now().UTC())
	if updateErr := repo.UpdateExtra(ctx, account.ID, updates); updateErr != nil {
		slog.Warn("openai_refresh_token_reused_marker_failed",
			"account_id", account.ID,
			"error", updateErr,
		)
	}
	mergeAccountExtra(account, updates)
}

func OpenAIRefreshTokenReusedExtra(account *Account, err error, now time.Time) map[string]any {
	updates := map[string]any{
		openAIRefreshTokenStatusExtraKey:             openAIRefreshTokenStatusReused,
		openAIRequiresReauthExtraKey:                 true,
		openAIRefreshTokenReusedAtExtraKey:           now.UTC().Format(time.RFC3339),
		openAIRefreshTokenReusedTokenVersionExtraKey: accountTokenVersion(account),
	}
	if err != nil {
		updates[openAIRefreshTokenLastErrorExtraKey] = truncateOpenAIRefreshTokenError(err.Error())
	}
	return updates
}

func OpenAIRefreshTokenRecoveredExtra(now time.Time) map[string]any {
	return map[string]any{
		openAIRefreshTokenStatusExtraKey:             openAIRefreshTokenStatusOK,
		openAIRequiresReauthExtraKey:                 false,
		openAIRefreshTokenReusedTokenVersionExtraKey: nil,
		openAIRefreshTokenLastErrorExtraKey:          nil,
		openAIRefreshTokenRecoveredAtExtraKey:        now.UTC().Format(time.RFC3339),
	}
}

func clearOpenAIRefreshTokenReusedMarker(ctx context.Context, repo AccountRepository, account *Account) {
	if repo == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return
	}
	if strings.TrimSpace(account.GetExtraString(openAIRefreshTokenStatusExtraKey)) != openAIRefreshTokenStatusReused {
		return
	}
	updates := OpenAIRefreshTokenRecoveredExtra(time.Now().UTC())
	if updateErr := repo.UpdateExtra(ctx, account.ID, updates); updateErr != nil {
		slog.Warn("openai_refresh_token_reused_marker_clear_failed",
			"account_id", account.ID,
			"error", updateErr,
		)
	}
	mergeAccountExtra(account, updates)
}

func applyOpenAIRefreshTokenRecoveredExtra(account *Account) {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return
	}
	updates := OpenAIRefreshTokenRecoveredExtra(time.Now().UTC())
	mergeAccountExtra(account, updates)
}

func shouldSuppressOpenAIRefreshForReusedToken(account *Account) bool {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return false
	}
	if strings.TrimSpace(account.GetOpenAIAccessToken()) == "" || strings.TrimSpace(account.GetOpenAIRefreshToken()) == "" {
		return false
	}
	if strings.TrimSpace(account.GetExtraString(openAIRefreshTokenStatusExtraKey)) != openAIRefreshTokenStatusReused {
		return false
	}
	markedVersion := getExtraInt64(account, openAIRefreshTokenReusedTokenVersionExtraKey)
	currentVersion := accountTokenVersion(account)
	if markedVersion == 0 || currentVersion == 0 {
		return markedVersion == currentVersion
	}
	return markedVersion == currentVersion
}

func openAICredentialsPayloadHasToken(credentials map[string]any) bool {
	for _, key := range []string{"access_token", "refresh_token", "id_token"} {
		if value, ok := credentials[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return true
		}
	}
	return false
}

func accountTokenVersion(account *Account) int64 {
	if account == nil {
		return 0
	}
	return account.GetCredentialAsInt64("_token_version")
}

func getExtraInt64(account *Account, key string) int64 {
	if account == nil || account.Extra == nil {
		return 0
	}
	val, ok := account.Extra[key]
	if !ok || val == nil {
		return 0
	}
	switch v := val.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		var out int64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &out); err == nil {
			return out
		}
	}
	return 0
}

func truncateOpenAIRefreshTokenError(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) <= openAIRefreshTokenLastErrorMaxLength {
		return msg
	}
	return msg[:openAIRefreshTokenLastErrorMaxLength]
}
