package perch

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/redissession"
	"github.com/redis/go-redis/v9"
)

const (
	ProviderGoogle = "google"
	ProviderGitHub = "github"

	authorizePath  = "/auth/v1/authorize"
	tokenPath      = "/auth/v1/token"
	loginStoreName = "oauth:session:perch"

	supabaseClientInfo = "supabase-js-node/2.106.0"
	supabaseAPIVersion = "2024-01-01"
	supabaseUserAgent  = "node"

	LoginSessionTTL          = 10 * time.Minute
	DefaultLoginRedirectHost = "127.0.0.1"
	DefaultLoginRedirectPort = 45999
	loginExchangeTimeout     = 30 * time.Second
	loginVerifierBytes       = 32
)

var (
	ErrLoginSessionNotFound = errors.New("perch: login session not found")
	ErrLoginSessionAdmin    = errors.New("perch: login session belongs to another admin")
)

type LoginFlow struct {
	Provider        string    `json:"provider"`
	AppURL          string    `json:"app_url"`
	SupabaseURL     string    `json:"supabase_url"`
	SupabaseAnonKey string    `json:"supabase_anon_key"`
	CodeVerifier    string    `json:"code_verifier"`
	CodeChallenge   string    `json:"code_challenge"`
	RedirectURI     string    `json:"redirect_uri"`
	CreatedAt       time.Time `json:"created_at"`
}

type LoginSession struct {
	AdminID int64 `json:"admin_id"`
	LoginFlow
}

func GenerateCodeVerifier() (string, error) {
	buf := make([]byte, loginVerifierBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("perch: generate code verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func CodeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func DefaultRedirectURI(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = DefaultLoginRedirectHost
	}
	if port <= 0 {
		port = DefaultLoginRedirectPort
	}
	return fmt.Sprintf("http://%s:%d/callback", host, port)
}

func BuildAuthorizeURL(supabaseURL, provider, redirectTo, codeChallenge string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(supabaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("perch: supabase url is empty")
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != ProviderGoogle && provider != ProviderGitHub {
		return "", &APIError{Status: http.StatusBadRequest, Code: "oauth_provider_unavailable", Message: "unsupported provider " + provider}
	}
	if strings.TrimSpace(redirectTo) == "" {
		return "", fmt.Errorf("perch: redirect uri is empty")
	}
	if strings.TrimSpace(codeChallenge) == "" {
		return "", fmt.Errorf("perch: code challenge is empty")
	}
	params := []string{
		"provider=" + url.QueryEscape(provider),
		"redirect_to=" + url.QueryEscape(redirectTo),
		"code_challenge=" + url.QueryEscape(codeChallenge),
		"code_challenge_method=s256",
	}
	if provider == ProviderGoogle {
		params = append(params, "prompt=select_account")
	}
	return base + authorizePath + "?" + strings.Join(params, "&"), nil
}

func ParseCallback(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", &APIError{Status: http.StatusBadRequest, Code: "oauth_callback_missing_code", Message: "callback url is empty"}
	}
	if !strings.Contains(text, "://") && !strings.Contains(text, "?") {
		return text, nil
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return "", &APIError{Status: http.StatusBadRequest, Code: "oauth_callback_invalid", Message: "callback url is not parseable"}
	}
	query := parsed.Query()
	if upstreamError := strings.TrimSpace(query.Get("error")); upstreamError != "" {
		description := strings.TrimSpace(query.Get("error_description"))
		if description == "" {
			description = upstreamError
		}
		return "", &APIError{Status: http.StatusBadRequest, Code: "oauth_callback_error", Message: description}
	}
	code := strings.TrimSpace(query.Get("code"))
	if code == "" {
		return "", &APIError{Status: http.StatusBadRequest, Code: "oauth_callback_missing_code", Message: "callback url has no code"}
	}
	return code, nil
}

type LoginClient struct {
	http Doer
	now  func() time.Time
}

func NewLoginClient(opts ...SessionOption) *LoginClient {
	dialer := FingerprintDialer(nil)
	session := &Session{
		http: NewWireDoer(dialer.DialTLSContext, controlPlaneTimeout, responseHeaderTimeout),
		now:  time.Now,
	}
	for _, opt := range opts {
		opt(session)
	}
	return &LoginClient{http: session.http, now: session.now}
}

func (c *LoginClient) Begin(ctx context.Context, appURL, provider, redirectURI string) (LoginFlow, []string, error) {
	session := &Session{http: c.http, now: c.now}
	auth, err := session.authConfigFor(ctx, appURL)
	if err != nil {
		return LoginFlow{}, nil, err
	}
	if !auth.OK || auth.SupabaseURL == "" || auth.SupabaseAnonKey == "" {
		return LoginFlow{}, nil, &APIError{Status: http.StatusBadGateway, Code: "auth_config_unavailable", Message: "perch auth config is unavailable"}
	}
	providers := make([]string, 0, len(auth.Providers))
	for _, candidate := range auth.Providers {
		trimmed := strings.ToLower(strings.TrimSpace(candidate))
		if trimmed == "" {
			continue
		}
		providers = append(providers, trimmed)
	}
	chosen := strings.ToLower(strings.TrimSpace(provider))
	if chosen == "" && len(providers) > 0 {
		chosen = providers[0]
	}
	if chosen == "" {
		return LoginFlow{}, nil, &APIError{Status: http.StatusBadGateway, Code: "oauth_provider_unavailable", Message: "perch advertises no login provider"}
	}
	supported := false
	for _, candidate := range providers {
		if candidate == chosen {
			supported = true
			break
		}
	}
	if !supported {
		return LoginFlow{}, nil, &APIError{Status: http.StatusBadRequest, Code: "oauth_provider_unavailable", Message: "perch does not offer the " + chosen + " provider"}
	}
	redirect := strings.TrimSpace(redirectURI)
	if redirect == "" {
		redirect = DefaultRedirectURI(auth.RedirectHost, DefaultLoginRedirectPort)
	}
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		return LoginFlow{}, nil, err
	}
	challenge := CodeChallengeS256(verifier)
	if _, err := BuildAuthorizeURL(auth.SupabaseURL, chosen, redirect, challenge); err != nil {
		return LoginFlow{}, nil, err
	}
	return LoginFlow{
		Provider:        chosen,
		AppURL:          NormalizeAppURL(auth.AppURL),
		SupabaseURL:     strings.TrimRight(auth.SupabaseURL, "/"),
		SupabaseAnonKey: auth.SupabaseAnonKey,
		CodeVerifier:    verifier,
		CodeChallenge:   challenge,
		RedirectURI:     redirect,
		CreatedAt:       c.now(),
	}, providers, nil
}

func (c *LoginClient) AuthorizeURL(flow LoginFlow) (string, error) {
	return BuildAuthorizeURL(flow.SupabaseURL, flow.Provider, flow.RedirectURI, flow.CodeChallenge)
}

func NewExchangeRequest(ctx context.Context, flow LoginFlow, code string) (*http.Request, error) {
	endpoint := strings.TrimRight(flow.SupabaseURL, "/") + tokenPath + "?grant_type=pkce"
	body, err := json.Marshal(struct {
		AuthCode     string `json:"auth_code"`
		CodeVerifier string `json:"code_verifier"`
	}{AuthCode: code, CodeVerifier: flow.CodeVerifier})
	if err != nil {
		return nil, fmt.Errorf("perch: encode pkce exchange: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("perch: build pkce exchange: %w", err)
	}
	setSupabaseTokenHeaders(req, flow.SupabaseAnonKey)
	return req, nil
}

func setSupabaseTokenHeaders(req *http.Request, anonKey string) {
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+anonKey)
	req.Header.Set("apikey", anonKey)
	req.Header.Set("X-Client-Info", supabaseClientInfo)
	req.Header.Set("X-Supabase-Api-Version", supabaseAPIVersion)
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "*")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("user-agent", supabaseUserAgent)
	req.Header.Set("accept-encoding", "br, gzip, deflate")
}

func (c *LoginClient) Exchange(ctx context.Context, flow LoginFlow, code string) (Credentials, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Credentials{}, &APIError{Status: http.StatusBadRequest, Code: "oauth_callback_missing_code", Message: "authorization code is empty"}
	}
	exchangeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loginExchangeTimeout)
	defer cancel()
	req, err := NewExchangeRequest(exchangeCtx, flow, code)
	if err != nil {
		return Credentials{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("perch: pkce exchange: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Credentials{}, fmt.Errorf("perch: read pkce exchange: %w", err)
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
		return Credentials{}, &APIError{Status: resp.StatusCode, Code: StatusToCode(resp.StatusCode), Message: "unexpected token response"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || parsed.AccessToken == "" {
		code := firstNonEmpty(parsed.ErrorCode, parsed.Error, StatusToCode(resp.StatusCode))
		message := firstNonEmpty(parsed.ErrorDescription, parsed.Msg, parsed.Error)
		if message == "" {
			message = "unexpected token response"
		}
		return Credentials{}, &APIError{Status: resp.StatusCode, Code: code, Message: message}
	}
	creds := Credentials{
		AppURL:       flow.AppURL,
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		ExpiresAt:    parsed.ExpiresAt,
		UserID:       parsed.User.ID,
		Email:        parsed.User.Email,
	}
	if creds.ExpiresAt <= 0 && parsed.ExpiresIn > 0 {
		creds.ExpiresAt = c.now().Add(time.Duration(parsed.ExpiresIn) * time.Second).Unix()
	}
	return creds.normalized(), nil
}

type LoginStore struct {
	mu        sync.Mutex
	items     map[string]LoginSession
	localOnly map[string]struct{}
	remote    *redissession.Store
	now       func() time.Time
}

func NewLoginStore() *LoginStore {
	return &LoginStore{items: make(map[string]LoginSession), localOnly: make(map[string]struct{}), now: time.Now}
}

func NewRedisLoginStore(rdb *redis.Client) *LoginStore {
	store := NewLoginStore()
	if rdb != nil {
		store.remote = redissession.New(rdb, loginStoreName, LoginSessionTTL)
	}
	return store
}

func (s *LoginStore) Put(ctx context.Context, id string, session LoginSession) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("perch: login session id is required")
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = s.now()
	}
	remoteOK := false
	if s.remote != nil {
		remoteOK = s.remote.Set(ctx, id, session) == nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, existing := range s.items {
		if s.now().Sub(existing.CreatedAt) > LoginSessionTTL {
			delete(s.items, key)
			delete(s.localOnly, key)
		}
	}
	s.items[id] = session
	if s.remote != nil && !remoteOK {
		s.localOnly[id] = struct{}{}
	}
	return nil
}

func (s *LoginStore) Take(ctx context.Context, id string, adminID int64) (LoginSession, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return LoginSession{}, ErrLoginSessionNotFound
	}
	if s.remote != nil && !s.isLocalOnly(id) {
		return s.takeRemote(ctx, id, adminID)
	}
	session, found, err := s.takeLocal(id, adminID)
	if err != nil {
		return LoginSession{}, err
	}
	if found {
		return session, nil
	}
	if s.remote == nil {
		return LoginSession{}, ErrLoginSessionNotFound
	}
	var remote LoginSession
	exists, err := s.remote.Get(ctx, id, &remote)
	if err != nil {
		return LoginSession{}, fmt.Errorf("perch: read login session: %w", err)
	}
	if !exists {
		return LoginSession{}, ErrLoginSessionNotFound
	}
	if remote.AdminID != adminID {
		return LoginSession{}, ErrLoginSessionAdmin
	}
	consumed, err := s.remote.TryConsume(ctx, id)
	if err != nil {
		return LoginSession{}, fmt.Errorf("perch: consume login session: %w", err)
	}
	if !consumed {
		return LoginSession{}, ErrLoginSessionNotFound
	}
	_ = s.remote.Delete(ctx, id)
	return remote, nil
}

func (s *LoginStore) isLocalOnly(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.localOnly[id]
	return ok
}

func (s *LoginStore) takeRemote(ctx context.Context, id string, adminID int64) (LoginSession, error) {
	var remote LoginSession
	exists, err := s.remote.Get(ctx, id, &remote)
	if err != nil {
		return LoginSession{}, fmt.Errorf("perch: read login session: %w", err)
	}
	if !exists {
		return LoginSession{}, ErrLoginSessionNotFound
	}
	if remote.AdminID != adminID {
		return LoginSession{}, ErrLoginSessionAdmin
	}
	consumed, err := s.remote.TryConsume(ctx, id)
	if err != nil {
		return LoginSession{}, fmt.Errorf("perch: consume login session: %w", err)
	}
	if !consumed {
		return LoginSession{}, ErrLoginSessionNotFound
	}
	_ = s.remote.Delete(ctx, id)
	s.mu.Lock()
	delete(s.items, id)
	delete(s.localOnly, id)
	s.mu.Unlock()
	return remote, nil
}

func (s *LoginStore) takeLocal(id string, adminID int64) (LoginSession, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.items[id]
	if !ok {
		return LoginSession{}, false, nil
	}
	if s.now().Sub(session.CreatedAt) > LoginSessionTTL {
		delete(s.items, id)
		return LoginSession{}, false, nil
	}
	if session.AdminID != adminID {
		return LoginSession{}, false, ErrLoginSessionAdmin
	}
	delete(s.items, id)
	return session, true, nil
}
