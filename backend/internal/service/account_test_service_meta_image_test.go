//go:build unit

package service

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func metaImageAccountTest(t *testing.T, status int, body string, mapping map[string]any) (*httpUpstreamRecorder, *httptest.ResponseRecorder, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}}
	svc := &AccountTestService{
		httpUpstream:         upstream,
		cfg:                  &config.Config{},
		openaiGatewayService: &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream},
	}
	credentials := map[string]any{"api_key": "sk-muse", "base_url": "https://relay.example/v1"}
	if mapping != nil {
		credentials["model_mapping"] = mapping
	}
	account := metaTestAccount(credentials, AccountTypeOAuth)
	err := svc.testModelRoutedAccountConnection(c, account, "muse-image", "")
	return upstream, rec, err
}

func TestAccountTestMetaImageGeneratesThroughMuseImagesAPI(t *testing.T) {
	webp := base64.StdEncoding.EncodeToString([]byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00"))
	upstream, rec, err := metaImageAccountTest(t, http.StatusOK, `{"data":[{"b64_json":"`+webp+`"}],"usage":{"input_tokens":9,"output_tokens":7}}`, map[string]any{"muse-image": "muse-image-1.0"})

	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://relay.example/v1/images/generations", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer sk-muse", upstream.lastReq.Header.Get("Authorization"))
	require.True(t, strings.HasPrefix(upstream.lastReq.Header.Get("User-Agent"), "muse-build/"))
	require.Equal(t, "muse-image-1.0", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, defaultOpenAIImageTestPrompt, gjson.GetBytes(upstream.lastBody, "prompt").String())
	require.Equal(t, int64(1), gjson.GetBytes(upstream.lastBody, "n").Int())
	require.False(t, gjson.GetBytes(upstream.lastBody, "aspect_ratio").Exists())
	require.Contains(t, rec.Body.String(), "data:image/webp;base64,"+webp)
	require.Contains(t, rec.Body.String(), `"success":true`)
}

func TestAccountTestMetaImageReportsUpstreamError(t *testing.T) {
	_, rec, _ := metaImageAccountTest(t, http.StatusBadRequest, `{"error":{"message":"bad size"}}`, map[string]any{"muse-image": "muse-image-1.0"})

	require.Contains(t, rec.Body.String(), "API returned 400")
	require.Contains(t, rec.Body.String(), "bad size")
	require.NotContains(t, rec.Body.String(), `"success":true`)
}

func TestAccountTestMetaTextModelSkipsImagesAPI(t *testing.T) {
	upstream, _, _ := metaImageAccountTest(t, http.StatusOK, `{}`, map[string]any{"muse-image": "muse-spark-1.3"})

	require.NotNil(t, upstream.lastReq)
	require.NotContains(t, upstream.lastReq.URL.Path, "/images/")
}
