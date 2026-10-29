package perch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	expirySkew         = 90 * time.Second
	authConfigTTL      = 10 * time.Minute
	defaultHTTPTimeout = 60 * time.Second
)

type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

type AuthConfig struct {
	OK              bool     `json:"ok"`
	AppURL          string   `json:"appUrl"`
	SupabaseURL     string   `json:"supabaseUrl"`
	SupabaseAnonKey string   `json:"supabaseAnonKey"`
	Providers       []string `json:"providers"`
}

type SessionOption func(*Session)

func WithHTTPClient(doer Doer) SessionOption {
	return func(s *Session) {
		if doer != nil {
			s.http = doer
		}
	}
}

func WithClock(now func() time.Time) SessionOption {
	return func(s *Session) {
		if now != nil {
			s.now = now
		}
	}
}

func WithCredentialsPersist(fn func(Credentials)) SessionOption {
	return func(s *Session) {
		s.onUpdate = fn
	}
}

type Session struct {
	mu         sync.Mutex
	creds      Credentials
	http       Doer
	now        func() time.Time
	onUpdate   func(Credentials)
	authConfig AuthConfig
	configAt   time.Time
	refreshing *refreshFlight
}

type refreshFlight struct {
	done  chan struct{}
	creds Credentials
	err   error
}

func NewSession(creds Credentials, opts ...SessionOption) *Session {
	s := &Session{
		creds: creds.normalized(),
		http:  &http.Client{Timeout: defaultHTTPTimeout},
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Session) Credentials() Credentials {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creds
}

func (s *Session) SetCredentials(creds Credentials) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds = creds.normalized()
}

func (s *Session) AppURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if app := s.creds.AppURL; app != "" {
		return app
	}
	return DefaultAppURL
}

const refreshExchangeTimeout = 30 * time.Second

func (s *Session) AccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	creds := s.creds
	s.mu.Unlock()

	if creds.AccessToken == "" {
		return "", fmt.Errorf("perch: session has no access token")
	}
	if creds.ExpiresAt <= 0 || creds.RefreshToken == "" {
		return creds.AccessToken, nil
	}
	if time.Unix(creds.ExpiresAt, 0).Sub(s.now()) > expirySkew {
		return creds.AccessToken, nil
	}
	return s.refresh(ctx)
}

func (s *Session) Refresh(ctx context.Context) (string, error) {
	return s.refresh(context.WithoutCancel(ctx))
}

func (s *Session) refresh(ctx context.Context) (string, error) {
	s.mu.Lock()
	if flight := s.refreshing; flight != nil {
		s.mu.Unlock()
		select {
		case <-flight.done:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if flight.err != nil {
			return "", flight.err
		}
		return flight.creds.AccessToken, nil
	}
	if s.creds.RefreshToken == "" {
		s.mu.Unlock()
		return "", fmt.Errorf("perch: session has no refresh token")
	}
	flight := &refreshFlight{done: make(chan struct{})}
	s.refreshing = flight
	creds := s.creds
	s.mu.Unlock()

	exchangeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshExchangeTimeout)
	defer cancel()
	updated, err := s.exchangeRefreshToken(exchangeCtx, creds)

	s.mu.Lock()
	flight.creds = updated
	flight.err = err
	s.refreshing = nil
	if err == nil {
		s.creds = updated
	}
	persist := s.onUpdate
	s.mu.Unlock()
	close(flight.done)

	if err != nil {
		return "", err
	}
	if persist != nil {
		persist(updated)
	}
	return updated.AccessToken, nil
}

func (s *Session) exchangeRefreshToken(ctx context.Context, creds Credentials) (Credentials, error) {
	auth, err := s.authConfigFor(ctx, creds.AppURL)
	if err != nil {
		return Credentials{}, err
	}
	if !auth.OK || auth.SupabaseURL == "" || auth.SupabaseAnonKey == "" {
		return Credentials{}, &APIError{Status: http.StatusBadGateway, Code: "auth_config_unavailable", Message: "perch auth config is unavailable"}
	}

	endpoint := strings.TrimRight(auth.SupabaseURL, "/") + "/auth/v1/token?grant_type=refresh_token"
	body, err := json.Marshal(map[string]string{"refresh_token": creds.RefreshToken})
	if err != nil {
		return Credentials{}, fmt.Errorf("perch: encode refresh request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Credentials{}, fmt.Errorf("perch: build refresh request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("apikey", auth.SupabaseAnonKey)
	req.Header.Set("authorization", "Bearer "+auth.SupabaseAnonKey)

	resp, err := s.http.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("perch: refresh session: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Credentials{}, fmt.Errorf("perch: read refresh response: %w", err)
	}

	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    int64  `json:"expires_at"`
		ExpiresIn    int64  `json:"expires_in"`
		User         struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
		ErrorCode        string `json:"error_code"`
		Msg              string `json:"msg"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return Credentials{}, &APIError{Status: resp.StatusCode, Code: StatusToCode(resp.StatusCode), Message: truncateBody(payload)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || parsed.AccessToken == "" {
		code := firstNonEmpty(parsed.ErrorCode, parsed.Error, StatusToCode(resp.StatusCode))
		message := firstNonEmpty(parsed.ErrorDescription, parsed.Msg, parsed.Error, truncateBody(payload))
		return Credentials{}, &APIError{Status: resp.StatusCode, Code: code, Message: message}
	}

	next := creds
	next.AccessToken = strings.TrimSpace(parsed.AccessToken)
	if token := strings.TrimSpace(parsed.RefreshToken); token != "" {
		next.RefreshToken = token
	}
	switch {
	case parsed.ExpiresAt > 0:
		next.ExpiresAt = parsed.ExpiresAt
	case parsed.ExpiresIn > 0:
		next.ExpiresAt = s.now().Add(time.Duration(parsed.ExpiresIn) * time.Second).Unix()
	}
	if parsed.User.ID != "" {
		next.UserID = parsed.User.ID
	}
	if parsed.User.Email != "" {
		next.Email = parsed.User.Email
	}
	return next.normalized(), nil
}

func (s *Session) authConfigFor(ctx context.Context, appURL string) (AuthConfig, error) {
	appURL = NormalizeAppURL(appURL)
	if appURL == "" {
		appURL = DefaultAppURL
	}
	s.mu.Lock()
	cached := s.authConfig
	cachedAt := s.configAt
	s.mu.Unlock()
	if cached.OK && cached.AppURL == appURL && s.now().Sub(cachedAt) < authConfigTTL {
		return cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appURL+defaultAuthConfigPath, nil)
	if err != nil {
		return AuthConfig{}, fmt.Errorf("perch: build auth config request: %w", err)
	}
	req.Header.Set("accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return AuthConfig{}, fmt.Errorf("perch: fetch auth config: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return AuthConfig{}, fmt.Errorf("perch: read auth config: %w", err)
	}
	var cfg AuthConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return AuthConfig{}, &APIError{Status: resp.StatusCode, Code: "auth_config_invalid", Message: truncateBody(payload)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return cfg, &APIError{Status: resp.StatusCode, Code: "auth_config_unavailable", Message: truncateBody(payload)}
	}
	if cfg.AppURL == "" {
		cfg.AppURL = appURL
	}

	s.mu.Lock()
	s.authConfig = cfg
	s.configAt = s.now()
	s.mu.Unlock()
	return cfg, nil
}

func truncateBody(payload []byte) string {
	text := strings.TrimSpace(string(payload))
	if len(text) > 240 {
		return text[:240]
	}
	return text
}

func joinURL(base, path string) (string, error) {
	base = NormalizeAppURL(base)
	if base == "" {
		return "", fmt.Errorf("perch: app url is empty")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("perch: invalid app url: %w", err)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
