package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiResponseToChatCompletionsMapsThoughtPartsToReasoningContent(t *testing.T) {
	raw := []byte(`{"candidates":[{"content":{"parts":[{"text":"hidden reasoning","thought":true},{"text":"visible answer"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3,"thoughtsTokenCount":7,"totalTokenCount":20}}`)
	var geminiResp map[string]any
	require.NoError(t, json.Unmarshal(raw, &geminiResp))

	chatResp, usage, err := geminiResponseToChatCompletions(geminiResp, "gemini-2.5-flash", raw, nil)
	require.NoError(t, err)
	require.NotNil(t, chatResp)
	require.NotNil(t, usage)
	require.Len(t, chatResp.Choices, 1)

	message := chatResp.Choices[0].Message
	require.Equal(t, "hidden reasoning", message.ReasoningContent)

	var content string
	require.NoError(t, json.Unmarshal(message.Content, &content))
	require.Equal(t, "visible answer", content)

	require.Equal(t, 10, chatResp.Usage.CompletionTokens)
	require.NotNil(t, chatResp.Usage.CompletionTokensDetails)
	require.Equal(t, 7, chatResp.Usage.CompletionTokensDetails.ReasoningTokens)
	require.Equal(t, 7, usage.ReasoningTokens)
}

func TestGeminiChatCompletionsStreamMapsThoughtPartsToReasoningDelta(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamBody := strings.Join([]string{
		`data: {"responseId":"gemini-stream-test","candidates":[{"content":{"parts":[{"text":"hidden ","thought":true}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":0,"thoughtsTokenCount":5,"totalTokenCount":15}}`,
		`data: {"responseId":"gemini-stream-test","candidates":[{"content":{"parts":[{"text":"final"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"thoughtsTokenCount":5,"totalTokenCount":17}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	svc := &GeminiMessagesCompatService{}

	_, err := svc.handleChatCompletionsStreamingResponseFromGemini(c, resp, time.Now(), "gemini-2.5-flash", false, true)
	require.NoError(t, err)

	reasoning, content, usageReasoningTokens := collectChatCompletionsStreamReasoningFields(t, recorder.Body.String())
	require.Equal(t, "hidden ", reasoning)
	require.Equal(t, "final", content)
	require.Equal(t, 5, usageReasoningTokens)
}

func TestConvertClaudeMessagesToGeminiGenerateContentMapsThinkingConfig(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-flash","max_tokens":512,"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":4096}}`)

	geminiReq, err := convertClaudeMessagesToGeminiGenerateContent(body)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(geminiReq, "generationConfig.thinkingConfig.includeThoughts").Bool())
	require.Equal(t, int64(4096), gjson.GetBytes(geminiReq, "generationConfig.thinkingConfig.thinkingBudget").Int())
}

func TestAntigravityChatCompletionsBufferedPreservesReasoningUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"id":"msg_ag_reasoning","type":"message","role":"assistant","model":"gemini-3-pro-high","content":[{"type":"thinking","thinking":"hidden ag"},{"type":"text","text":"visible ag"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":12,"reasoning_tokens":7}}`)

	require.NoError(t, writeAntigravityChatBufferedFromAnthropic(c, body, "gemini-3-pro-high"))

	payload := gjson.Parse(recorder.Body.String())
	require.Equal(t, "hidden ag", payload.Get("choices.0.message.reasoning_content").String())
	require.Equal(t, "visible ag", payload.Get("choices.0.message.content").String())
	require.Equal(t, int64(7), payload.Get("usage.completion_tokens_details.reasoning_tokens").Int())
}

func collectChatCompletionsStreamReasoningFields(t *testing.T, body string) (string, string, int) {
	t.Helper()

	var reasoning strings.Builder
	var content strings.Builder
	usageReasoningTokens := 0

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		parsed := gjson.Parse(payload)
		if rc := parsed.Get("choices.0.delta.reasoning_content"); rc.Exists() {
			_, _ = reasoning.WriteString(rc.String())
		}
		if text := parsed.Get("choices.0.delta.content"); text.Exists() {
			_, _ = content.WriteString(text.String())
		}
		if rt := parsed.Get("usage.completion_tokens_details.reasoning_tokens"); rt.Exists() {
			usageReasoningTokens = int(rt.Int())
		}
	}

	return reasoning.String(), content.String(), usageReasoningTokens
}
