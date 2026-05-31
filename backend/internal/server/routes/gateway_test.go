package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tickernelz/sub2api/internal/config"
	"github.com/tickernelz/sub2api/internal/handler"
	servermiddleware "github.com/tickernelz/sub2api/internal/server/middleware"
	"github.com/tickernelz/sub2api/internal/service"
)

func newGatewayRoutesTestRouter() *gin.Engine {
	return newGatewayRoutesTestRouterForPlatform(service.PlatformOpenAI)
}

func newGatewayRoutesTestRouterForPlatform(platform string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	RegisterGatewayRoutes(
		router,
		&handler.Handlers{
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupID := int64(1)
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				GroupID: &groupID,
				Group:   &service.Group{Platform: platform},
			})
			c.Next()
		}),
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
	)

	return router
}

func TestGatewayRoutesOpenAIResponsesCompactPathIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/responses/compact",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI responses handler", path)
	}
}

func TestGatewayRoutesRejectsOpenCodeResponsesCompact(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformOpenCode)

	for _, path := range []string{
		"/v1/responses/compact",
		"/responses/compact",
		"/backend-api/codex/responses/compact",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"big-pickle"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should reject unsupported OpenCode compact", path)
		require.Contains(t, w.Body.String(), "/responses/compact is not supported for OpenCode")
	}
}

func TestOpenCodeUsesOpenAICompatibleGatewayRouting(t *testing.T) {
	require.True(t, isOpenAICompatibleGatewayPlatform(service.PlatformOpenAI))
	require.True(t, isOpenAICompatibleGatewayPlatform(service.PlatformOpenCode))
	require.False(t, isOpenAICompatibleGatewayPlatform(service.PlatformCursor))
	require.False(t, isOpenAICompatibleGatewayPlatform(service.PlatformAnthropic))
	require.False(t, isOpenAICompatibleGatewayPlatform(service.PlatformGemini))
}

func TestGatewayRoutesRejectCursorRuntimeUntilNativeAdapterExists(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformCursor)

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/v1/messages", body: `{"model":"composer-2.5","messages":[]}`},
		{method: http.MethodPost, path: "/v1/messages/count_tokens", body: `{"model":"composer-2.5","messages":[]}`},
		{method: http.MethodPost, path: "/v1/chat/completions", body: `{"model":"composer-2.5","messages":[]}`},
		{method: http.MethodPost, path: "/v1/responses", body: `{"model":"composer-2.5","input":"hi"}`},
		{method: http.MethodPost, path: "/responses", body: `{"model":"composer-2.5","input":"hi"}`},
		{method: http.MethodPost, path: "/backend-api/codex/responses", body: `{"model":"composer-2.5","input":"hi"}`},
		{method: http.MethodGet, path: "/v1/responses"},
		{method: http.MethodGet, path: "/responses"},
		{method: http.MethodGet, path: "/backend-api/codex/responses"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should reject unsupported Cursor runtime", tc.path)
		require.Contains(t, w.Body.String(), "Cursor runtime is not implemented yet")
	}
}

func TestGatewayRoutesOpenAIImagesPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-image-2","prompt":"draw a cat"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI images handler", path)
	}
}
