package perch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultAppURL          = "https://app.perchai.app"
	defaultAuthConfigPath  = "/api/perch-terminal/cli-auth/config"
	defaultSessionFileName = "cli-auth-session.json"
	defaultPerchDir        = ".perch"
)

type Credentials struct {
	AppURL       string `json:"appUrl"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"`
	UserID       string `json:"userId,omitempty"`
	Email        string `json:"email,omitempty"`
}

type sessionFile struct {
	Version int `json:"version"`
	Credentials
	UpdatedAt string `json:"updatedAt,omitempty"`
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

func DefaultSessionPath() string {
	if dir := strings.TrimSpace(os.Getenv("PERCH_CLI_AUTH_DIR")); dir != "" {
		return filepath.Join(dir, defaultSessionFileName)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, defaultPerchDir, defaultSessionFileName)
}

func LoadCredentialsFile(path string) (Credentials, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("perch: read session file: %w", err)
	}
	var file sessionFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return Credentials{}, fmt.Errorf("perch: parse session file: %w", err)
	}
	creds := file.normalized()
	if err := creds.Validate(); err != nil {
		return Credentials{}, err
	}
	return creds, nil
}

func LoadDefaultCredentials() (Credentials, error) {
	path := DefaultSessionPath()
	if path == "" {
		return Credentials{}, fmt.Errorf("perch: cannot resolve home directory for session file")
	}
	return LoadCredentialsFile(path)
}

func SaveCredentialsFile(path string, creds Credentials) error {
	creds = creds.normalized()
	if err := creds.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(sessionFile{Version: 1, Credentials: creds, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return fmt.Errorf("perch: encode session file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("perch: create session dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".perch-session-*")
	if err != nil {
		return fmt.Errorf("perch: create session temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if _, err := tmp.Write(payload); err != nil {
		cleanup()
		return fmt.Errorf("perch: write session file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("perch: chmod session file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("perch: close session file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("perch: replace session file: %w", err)
	}
	return nil
}
