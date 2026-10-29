package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/perch"
)

const (
	EnvPerchAppURL = "PERCH_APP_URL"
)

type PerchOAuthService struct {
	store  *perch.LoginStore
	login  *perch.LoginClient
	now    func() time.Time
	appURL string
}

func NewPerchOAuthService() *PerchOAuthService {
	appURL := strings.TrimSpace(os.Getenv(EnvPerchAppURL))
	if appURL == "" {
		appURL = perch.DefaultAppURL
	}
	return &PerchOAuthService{
		store:  perch.NewLoginStore(),
		login:  perch.NewLoginClient(),
		now:    time.Now,
		appURL: perch.NormalizeAppURL(appURL),
	}
}

func (s *PerchOAuthService) WithLoginStore(store *perch.LoginStore) *PerchOAuthService {
	if s != nil && store != nil {
		s.store = store
	}
	return s
}

type PerchAuthURLInput struct {
	AdminID  int64
	Provider string
}

type PerchAuthURLResult struct {
	AuthURL     string   `json:"auth_url"`
	SessionID   string   `json:"session_id"`
	Provider    string   `json:"provider"`
	Providers   []string `json:"providers"`
	AppURL      string   `json:"app_url"`
	RedirectURI string   `json:"redirect_uri"`
	ExpiresIn   int      `json:"expires_in"`
}

type PerchExchangeInput struct {
	AdminID     int64
	SessionID   string
	CallbackURL string
	Code        string
}

type PerchExchangeResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AppURL       string `json:"app_url"`
	ExpiresAt    int64  `json:"expires_at"`
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
}

func (s *PerchOAuthService) GenerateAuthURL(ctx context.Context, input PerchAuthURLInput) (*PerchAuthURLResult, error) {
	flow, providers, err := s.login.Begin(ctx, s.appURL, input.Provider, "")
	if err != nil {
		return nil, perchOAuthError(err, "PERCH_OAUTH_START_FAILED")
	}
	authURL, err := s.login.AuthorizeURL(flow)
	if err != nil {
		return nil, perchOAuthError(err, "PERCH_OAUTH_START_FAILED")
	}
	sessionID, err := newPerchLoginSessionID()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "PERCH_OAUTH_SESSION_FAILED", "failed to allocate login session: %v", err)
	}
	if err := s.store.Put(ctx, sessionID, perch.LoginSession{AdminID: input.AdminID, LoginFlow: flow}); err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "PERCH_OAUTH_SESSION_FAILED", "failed to persist login session: %v", err)
	}
	return &PerchAuthURLResult{
		AuthURL:     authURL,
		SessionID:   sessionID,
		Provider:    flow.Provider,
		Providers:   providers,
		AppURL:      flow.AppURL,
		RedirectURI: flow.RedirectURI,
		ExpiresIn:   int(perch.LoginSessionTTL / time.Second),
	}, nil
}

func (s *PerchOAuthService) ExchangeCode(ctx context.Context, input PerchExchangeInput) (*PerchExchangeResult, error) {
	session, err := s.store.Take(ctx, input.SessionID, input.AdminID)
	if err != nil {
		switch {
		case errors.Is(err, perch.ErrLoginSessionNotFound):
			return nil, infraerrors.New(http.StatusNotFound, "PERCH_OAUTH_SESSION_NOT_FOUND", "login session is unknown or already used; start the login again")
		case errors.Is(err, perch.ErrLoginSessionAdmin):
			return nil, infraerrors.New(http.StatusForbidden, "PERCH_OAUTH_SESSION_FORBIDDEN", "login session belongs to another admin; start your own login")
		}
		return nil, infraerrors.Newf(http.StatusInternalServerError, "PERCH_OAUTH_SESSION_FAILED", "failed to load login session: %v", err)
	}
	code := strings.TrimSpace(input.Code)
	if code == "" && strings.TrimSpace(input.CallbackURL) != "" {
		code, err = perch.ParseCallback(input.CallbackURL)
		if err != nil {
			return nil, perchOAuthError(err, "PERCH_OAUTH_CALLBACK_INVALID")
		}
	}
	creds, err := s.login.Exchange(ctx, session.LoginFlow, code)
	if err != nil {
		return nil, perchOAuthError(err, "PERCH_OAUTH_EXCHANGE_FAILED")
	}
	return &PerchExchangeResult{
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		AppURL:       creds.AppURL,
		ExpiresAt:    creds.ExpiresAt,
		UserID:       creds.UserID,
		Email:        creds.Email,
	}, nil
}

func perchOAuthError(err error, fallbackReason string) error {
	var apiErr *perch.APIError
	if errors.As(err, &apiErr) {
		status := apiErr.Status
		if status == http.StatusUnauthorized || status == http.StatusForbidden || status >= 500 {
			status = http.StatusBadGateway
		}
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		reason := normalizePerchReason(apiErr.Code)
		if reason == "" {
			reason = fallbackReason
		}
		return infraerrors.New(status, "PERCH_OAUTH_"+reason, apiErr.Message)
	}
	return infraerrors.Newf(http.StatusBadGateway, fallbackReason, "%v", err)
}

func normalizePerchReason(code string) string {
	upper := strings.ToUpper(strings.TrimSpace(code))
	var out strings.Builder
	lastUnderscore := false
	for _, r := range upper {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			_, _ = out.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if out.Len() > 0 && !lastUnderscore {
			_ = out.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(out.String(), "_")
}

func newPerchLoginSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
