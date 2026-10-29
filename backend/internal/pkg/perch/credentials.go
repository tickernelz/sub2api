package perch

import (
	"fmt"
	"strings"
	"time"
)

const (
	DefaultAppURL         = "https://app.perchai.app"
	defaultAuthConfigPath = "/api/perch-terminal/cli-auth/config"
)

type Credentials struct {
	AppURL       string `json:"appUrl"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"`
	UserID       string `json:"userId,omitempty"`
	Email        string `json:"email,omitempty"`
}

func (c Credentials) normalized() Credentials {
	out := c
	out.AppURL = NormalizeAppURL(out.AppURL)
	out.AccessToken = strings.TrimSpace(out.AccessToken)
	out.RefreshToken = strings.TrimSpace(out.RefreshToken)
	out.UserID = strings.TrimSpace(out.UserID)
	out.Email = strings.TrimSpace(out.Email)
	return out
}

func (c Credentials) Validate() error {
	if c.normalized().AccessToken == "" {
		return fmt.Errorf("perch: access token is required")
	}
	return nil
}

func (c Credentials) ExpiresAtTime() time.Time {
	if c.ExpiresAt <= 0 {
		return time.Time{}
	}
	return time.Unix(c.ExpiresAt, 0)
}

func (c Credentials) Redacted() map[string]any {
	return map[string]any{
		"app_url":     NormalizeAppURL(c.AppURL),
		"user_id":     strings.TrimSpace(c.UserID),
		"email":       strings.TrimSpace(c.Email),
		"expires_at":  c.ExpiresAt,
		"has_access":  strings.TrimSpace(c.AccessToken) != "",
		"has_refresh": strings.TrimSpace(c.RefreshToken) != "",
	}
}

func NormalizeAppURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	return strings.TrimRight(trimmed, "/")
}
