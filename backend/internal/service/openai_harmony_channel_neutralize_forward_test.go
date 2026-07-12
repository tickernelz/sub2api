package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// readReqBody reads the fully-built upstream request body back for assertion.
func readReqBody(t *testing.T, req *http.Request) string {
	t.Helper()
	require.NotNil(t, req)
	require.NotNil(t, req.Body)
	b, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	return string(b)
}

func cfgWithHarmonyNeutralize(enabled bool) *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.NeutralizeHarmonyChannelToken = enabled
	return cfg
}

func TestBuildUpstreamRequestNeutralizesHarmonyChannelTokenWhenEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte("{\"model\":\"gpt-5\",\"input\":\"<|channel|>analysis\"}")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	svc := &OpenAIGatewayService{cfg: cfgWithHarmonyNeutralize(true)}
	account := &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://example.com/v1"}}

	req, err := svc.buildUpstreamRequest(c.Request.Context(), c, account, body, "token", true, "", false)
	require.NoError(t, err)
	got := readReqBody(t, req)
	require.Equal(t, "{\"model\":\"gpt-5\",\"input\":\"<\uff5cchannel\uff5c>analysis\"}", got)
	require.NotContains(t, got, "<|channel|>")
}

func TestBuildUpstreamRequestPreservesHarmonyChannelTokenWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte("{\"model\":\"gpt-5\",\"input\":\"<|channel|>analysis\"}")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	svc := &OpenAIGatewayService{cfg: cfgWithHarmonyNeutralize(false)}
	account := &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://example.com/v1"}}

	req, err := svc.buildUpstreamRequest(c.Request.Context(), c, account, body, "token", true, "", false)
	require.NoError(t, err)
	got := readReqBody(t, req)
	require.Equal(t, string(body), got)
	require.Contains(t, got, "<|channel|>")
}

func TestBuildUpstreamRequestPassthroughNeutralizesHarmonyChannelTokenWhenEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte("{\"model\":\"gpt-5\",\"input\":\"<|channel|>analysis\"}")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	svc := &OpenAIGatewayService{cfg: cfgWithHarmonyNeutralize(true)}
	account := &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://example.com/v1"}}

	req, err := svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, account, body, "token")
	require.NoError(t, err)
	got := readReqBody(t, req)
	require.Equal(t, "{\"model\":\"gpt-5\",\"input\":\"<\uff5cchannel\uff5c>analysis\"}", got)
	require.NotContains(t, got, "<|channel|>")
}

func TestBuildUpstreamRequestPassthroughPreservesHarmonyChannelTokenWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte("{\"model\":\"gpt-5\",\"input\":\"<|channel|>analysis\"}")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	svc := &OpenAIGatewayService{cfg: cfgWithHarmonyNeutralize(false)}
	account := &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://example.com/v1"}}

	req, err := svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, account, body, "token")
	require.NoError(t, err)
	got := readReqBody(t, req)
	require.Equal(t, string(body), got)
	require.Contains(t, got, "<|channel|>")
}

func TestBuildUpstreamRequestNeutralizesHarmonyChannelTokenWhenCfgNil(t *testing.T) {
	// Nil cfg must behave as neutralize-ON (matches the s.cfg == nil guard and
	// the viper default gateway.neutralize_harmony_channel_token=true).
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte("{\"model\":\"gpt-5\",\"input\":\"<|channel|>analysis\"}")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	svc := &OpenAIGatewayService{}
	account := &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://example.com/v1"}}

	req, err := svc.buildUpstreamRequest(c.Request.Context(), c, account, body, "token", true, "", false)
	require.NoError(t, err)
	require.NotContains(t, readReqBody(t, req), "<|channel|>")
}
