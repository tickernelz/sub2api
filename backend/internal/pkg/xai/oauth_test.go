//go:build unit

package xai

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAuthorizationInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		raw               string
		wantCode          string
		wantState         string
		wantRequiresState bool
	}{
		{
			name:              "full callback url",
			raw:               "http://127.0.0.1:56121/callback?code=abc123&state=state456",
			wantCode:          "abc123",
			wantState:         "state456",
			wantRequiresState: true,
		},
		{
			name:              "query string",
			raw:               "?code=abc123&state=state456",
			wantCode:          "abc123",
			wantState:         "state456",
			wantRequiresState: true,
		},
		{
			name:              "full callback url missing state",
			raw:               "http://127.0.0.1:56121/callback?code=abc123",
			wantCode:          "abc123",
			wantRequiresState: true,
		},
		{
			name:              "query string missing state",
			raw:               "code=abc123",
			wantCode:          "abc123",
			wantRequiresState: true,
		},
		{
			name:     "bare code",
			raw:      "abc123",
			wantCode: "abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseAuthorizationInput(tt.raw)
			require.Equal(t, tt.wantCode, got.Code)
			require.Equal(t, tt.wantState, got.State)
			require.Equal(t, tt.wantRequiresState, got.RequiresState)
		})
	}
}

func TestBuildAuthorizationURLIncludesHermesCompatibleParameters(t *testing.T) {
	t.Setenv(EnvAuthorizeURL, "https://auth.example.test/oauth2/authorize")
	t.Setenv(EnvClientID, "client-id")
	t.Setenv(EnvScope, "openid profile offline_access api:access")

	authURL := BuildAuthorizationURL("state", "challenge", "http://127.0.0.1:56121/callback", "nonce")
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)

	values := parsed.Query()
	require.Equal(t, "https", parsed.Scheme)
	require.Equal(t, "auth.example.test", parsed.Host)
	require.Equal(t, "/oauth2/authorize", parsed.Path)
	require.Equal(t, "code", values.Get("response_type"))
	require.Equal(t, "client-id", values.Get("client_id"))
	require.Equal(t, "http://127.0.0.1:56121/callback", values.Get("redirect_uri"))
	require.Equal(t, "openid profile offline_access api:access", values.Get("scope"))
	require.Equal(t, "state", values.Get("state"))
	require.Equal(t, "nonce", values.Get("nonce"))
	require.Equal(t, "challenge", values.Get("code_challenge"))
	require.Equal(t, "S256", values.Get("code_challenge_method"))
	require.Equal(t, "generic", values.Get("plan"))
	require.Equal(t, "sub2api", values.Get("referrer"))
}

func TestValidateXAIURLsAllowOfficialOAuthAndGatewayHosts(t *testing.T) {
	authorizeURL, err := ValidateOAuthEndpointURL(DefaultAuthorizeURL)
	require.NoError(t, err)
	require.Equal(t, DefaultAuthorizeURL, authorizeURL)

	tokenURL, err := ValidateOAuthEndpointURL(DefaultTokenURL)
	require.NoError(t, err)
	require.Equal(t, DefaultTokenURL, tokenURL)

	baseURL, err := ValidateBaseURL(DefaultBaseURL)
	require.NoError(t, err)
	require.Equal(t, DefaultBaseURL, baseURL)

	cliBaseURL, err := ValidateBaseURL(DefaultCLIBaseURL)
	require.NoError(t, err)
	require.Equal(t, DefaultCLIBaseURL, cliBaseURL)

	baseURLNoPath, err := ValidateBaseURL("https://api.x.ai")
	require.NoError(t, err)
	require.Equal(t, DefaultBaseURL, baseURLNoPath)

	chatURL, err := BuildChatCompletionsURL(DefaultCLIBaseURL + "/")
	require.NoError(t, err)
	require.Equal(t, DefaultCLIBaseURL+"/chat/completions", chatURL)
}

func TestValidateXAIURLsRejectArbitraryHostsByDefault(t *testing.T) {
	_, err := ValidateOAuthEndpointURL("https://auth.example.test/oauth2/token")
	require.Error(t, err)

	_, err = ValidateBaseURL("https://xai.test/v1")
	require.Error(t, err)

	_, err = ValidateBaseURL("http://127.0.0.1:8080/v1")
	require.Error(t, err)

	_, err = ValidateBaseURL("https://api.x.ai/custom")
	require.Error(t, err)
}

func TestValidateXAIURLsAllowUnsafeDevOverride(t *testing.T) {
	t.Setenv(EnvAllowUnsafeURLOverrides, "true")

	tokenURL, err := ValidateOAuthEndpointURL("http://127.0.0.1:8080/oauth2/token")
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:8080/oauth2/token", tokenURL)

	baseURL, err := ValidateBaseURL("http://127.0.0.1:8080/v1/")
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:8080/v1", baseURL)
}

func TestDefaultModelMappingIncludesGrokAliases(t *testing.T) {
	t.Parallel()

	mapping := DefaultModelMapping()
	require.Equal(t, "grok-4.3", mapping["grok"])
	require.Equal(t, "grok-4.3", mapping["grok-latest"])
	require.Equal(t, "grok-build-0.1", mapping["grok-build"])
	require.Equal(t, "grok-4.20-0309-reasoning", mapping["grok-4.20-reasoning"])
	require.Equal(t, "grok-4.20-0309-non-reasoning", mapping["grok-4.20-non-reasoning"])
	require.Equal(t, "grok-4.20-multi-agent-0309", mapping["grok-4.20-multi-agent-0309"])
}
