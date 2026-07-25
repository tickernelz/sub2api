package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRecordOpenAIWSInvalidPrompt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 42, Name: "ws-account", Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	payload := []byte(`{"type":"response.failed","response":{"error":{"code":"invalid_prompt","message":"Request blocked."}}}`)

	recorded := svc.recordOpenAIWSInvalidPrompt(c, account, true, "req_ws_123", payload)

	require.True(t, recorded)
	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "invalid_prompt", events[0].Kind)
	require.Equal(t, "req_ws_123", events[0].UpstreamRequestID)
	require.Equal(t, int64(42), events[0].AccountID)
	require.True(t, events[0].Passthrough)
	require.Equal(t, "Request blocked.", events[0].Message)
}

func TestRecordOpenAIWSInvalidPromptErrorEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	payload := []byte(`{"type":"error","error":{"code":"invalid_prompt","message":"Request blocked."}}`)

	recorded := svc.recordOpenAIWSInvalidPrompt(c, account, false, "req_ws_error", payload)

	require.True(t, recorded)
	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "invalid_prompt", events[0].Kind)
}

func TestRecordOpenAIWSInvalidPromptIgnoresOtherFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	payload := []byte(`{"type":"response.failed","response":{"error":{"code":"server_error","message":"Internal error"}}}`)

	recorded := svc.recordOpenAIWSInvalidPrompt(c, account, false, "req_ws_456", payload)

	require.False(t, recorded)
	_, ok := c.Get(OpsUpstreamErrorsKey)
	require.False(t, ok)
}

func TestIsOpenAIWSInvalidPromptObservableEvent(t *testing.T) {
	require.True(t, isOpenAIWSInvalidPromptObservableEvent("response.failed"))
	require.True(t, isOpenAIWSInvalidPromptObservableEvent("error"))
	require.False(t, isOpenAIWSInvalidPromptObservableEvent("response.completed"))
}
