package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

// ForwardAsChatCompletions routes OpenAI Chat Completions requests through the
// Antigravity Claude-compatible path, then converts the Anthropic response back
// to Chat Completions format.
func (s *AntigravityGatewayService) ForwardAsChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	isStickySession bool,
) (*ForwardResult, error) {
	var ccReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &ccReq); err != nil {
		writeGatewayCCError(c, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return nil, fmt.Errorf("parse chat completions request: %w", err)
	}
	if strings.TrimSpace(ccReq.Model) == "" {
		writeGatewayCCError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("model is required")
	}

	originalModel := ccReq.Model
	clientStream := ccReq.Stream
	includeUsage := ccReq.StreamOptions != nil && ccReq.StreamOptions.IncludeUsage

	responsesReq, err := apicompat.ChatCompletionsToResponses(&ccReq)
	if err != nil {
		writeGatewayCCError(c, http.StatusBadRequest, "invalid_request_error", "Invalid request")
		return nil, fmt.Errorf("convert chat completions to responses: %w", err)
	}
	anthropicReq, err := apicompat.ResponsesToAnthropicRequest(responsesReq)
	if err != nil {
		writeGatewayCCError(c, http.StatusBadRequest, "invalid_request_error", "Invalid request")
		return nil, fmt.Errorf("convert responses to anthropic: %w", err)
	}
	anthropicReq.Stream = clientStream

	anthropicBody, err := json.Marshal(anthropicReq)
	if err != nil {
		writeGatewayCCError(c, http.StatusBadRequest, "invalid_request_error", "Invalid request")
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	recorder := httptest.NewRecorder()
	captureCtx, _ := gin.CreateTestContext(recorder)
	originalWriter := c.Writer
	c.Writer = captureCtx.Writer
	result, forwardErr := s.Forward(ctx, c, account, anthropicBody, isStickySession)
	c.Writer = originalWriter
	if forwardErr != nil {
		return result, forwardErr
	}

	status := recorder.Code
	if status == 0 {
		status = http.StatusOK
	}
	if status >= 400 {
		writeAntigravityChatCompatError(c, status, recorder.Body.Bytes())
		return result, fmt.Errorf("antigravity upstream returned status %d", status)
	}

	if requestID := recorder.Header().Get("x-request-id"); requestID != "" {
		c.Header("x-request-id", requestID)
	}

	if clientStream {
		if err := writeAntigravityChatStreamFromAnthropic(c, recorder.Body.Bytes(), originalModel, includeUsage); err != nil {
			return result, err
		}
	} else {
		if err := writeAntigravityChatBufferedFromAnthropic(c, recorder.Body.Bytes(), originalModel); err != nil {
			return result, err
		}
	}

	if result != nil {
		result.Model = originalModel
		result.Stream = clientStream
	}
	return result, nil
}

func writeAntigravityChatBufferedFromAnthropic(c *gin.Context, body []byte, originalModel string) error {
	var anthropicResp apicompat.AnthropicResponse
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		writeGatewayCCError(c, http.StatusBadGateway, "server_error", "Failed to parse upstream response")
		return fmt.Errorf("parse anthropic response: %w", err)
	}

	responsesResp := apicompat.AnthropicToResponsesResponse(&anthropicResp)
	ccResp := apicompat.ResponsesToChatCompletions(responsesResp, originalModel)
	respBytes, err := json.Marshal(ccResp)
	if err != nil {
		writeGatewayCCError(c, http.StatusBadGateway, "server_error", "Failed to encode response")
		return fmt.Errorf("marshal chat completions response: %w", err)
	}
	respBytes = reverseToolNamesIfPresent(c, respBytes)
	c.Data(http.StatusOK, "application/json; charset=utf-8", respBytes)
	return nil
}

func writeAntigravityChatStreamFromAnthropic(c *gin.Context, body []byte, originalModel string, includeUsage bool) error {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	anthState := apicompat.NewAnthropicEventToResponsesState()
	anthState.Model = originalModel
	ccState := apicompat.NewResponsesEventToChatState()
	ccState.Model = originalModel
	ccState.IncludeUsage = includeUsage

	writeChunk := func(chunk apicompat.ChatCompletionsChunk) error {
		sse, err := apicompat.ChatChunkToSSE(chunk)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(c.Writer, string(reverseToolNamesIfPresent(c, []byte(sse))))
		return err
	}
	processEvent := func(event *apicompat.AnthropicStreamEvent) error {
		responsesEvents := apicompat.AnthropicEventToResponsesEvents(event, anthState)
		for _, resEvt := range responsesEvents {
			ccChunks := apicompat.ResponsesEventToChatChunks(&resEvt, ccState)
			for _, chunk := range ccChunks {
				if err := writeChunk(chunk); err != nil {
					return err
				}
			}
		}
		c.Writer.Flush()
		return nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}
		if err := processEvent(&event); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	for _, resEvt := range apicompat.FinalizeAnthropicResponsesStream(anthState) {
		for _, chunk := range apicompat.ResponsesEventToChatChunks(&resEvt, ccState) {
			if err := writeChunk(chunk); err != nil {
				return err
			}
		}
	}
	for _, chunk := range apicompat.FinalizeResponsesChatStream(ccState) {
		if err := writeChunk(chunk); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
	return nil
}

func writeAntigravityChatCompatError(c *gin.Context, status int, body []byte) {
	errType := "server_error"
	message := "Upstream request failed"
	var payload struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil {
		if payload.Error.Type != "" {
			errType = payload.Error.Type
		}
		if payload.Error.Message != "" {
			message = payload.Error.Message
		}
	}
	writeGatewayCCError(c, status, errType, message)
}
