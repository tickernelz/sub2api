package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tickernelz/sub2api/internal/pkg/cursorproto"
	providerregistry "github.com/tickernelz/sub2api/internal/provider"
	"github.com/tidwall/gjson"
)

const (
	cursorUserAgent              = "connect-es/1.6.1"
	cursorDisconnectDrainTimeout = 1200 * time.Millisecond
)

func (s *GatewayService) forwardCursorMessages(ctx context.Context, c *gin.Context, account *Account, parsed *ParsedRequest, startTime time.Time) (*ForwardResult, error) {
	if account == nil || parsed == nil {
		return nil, fmt.Errorf("cursor forward: missing account or request")
	}
	if account.Type != AccountTypeOAuth {
		return nil, fmt.Errorf("cursor requires oauth token")
	}
	if s == nil || s.httpUpstream == nil {
		return nil, fmt.Errorf("cursor forward: http upstream is not configured")
	}
	token := account.GetCursorAccessToken()
	if token == "" {
		return nil, fmt.Errorf("cursor forward: access token is required")
	}

	originalModel := parsed.Model
	mappedModel := originalModel
	if next := account.GetMappedModel(originalModel); next != "" {
		mappedModel = next
	}
	requestedModel := cursorproto.ResolveRequestedModel(mappedModel)
	if requestedModel.ModelID == "" {
		return nil, fmt.Errorf("cursor forward: model is required")
	}

	userText := flattenCursorMessages(parsed.Body, parsed.System)
	blobStore := make(map[string][]byte)
	requestBody, err := cursorproto.BuildAgentRequestBody(cursorproto.AgentRunInput{
		ModelID:   mappedModel,
		UserText:  userText,
		BlobStore: blobStore,
	})
	if err != nil {
		return nil, err
	}

	requestID := uuid.NewString()
	pr, pw := io.Pipe()
	upstreamCtx, releaseUpstreamCtx := detachStreamUpstreamContext(ctx, parsed.Stream)
	req, err := http.NewRequestWithContext(WithHTTPUpstreamProfile(upstreamCtx, HTTPUpstreamProfileCursor), http.MethodPost, providerregistry.CursorRunURL, pr)
	releaseUpstreamCtx()
	if err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return nil, err
	}
	applyCursorRunHeaders(req.Header, token, requestID)

	respCh := make(chan cursorHTTPResult, 1)
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	go func() {
		resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
		respCh <- cursorHTTPResult{resp: resp, err: err}
	}()

	writeErrCh := make(chan error, 1)
	go func() {
		_, err := pw.Write(requestBody)
		if err != nil {
			writeErrCh <- err
		}
	}()

	var resp *http.Response
	select {
	case result := <-respCh:
		if result.err != nil {
			_ = pw.Close()
			return nil, result.err
		}
		resp = result.resp
	case err := <-writeErrCh:
		_ = pw.Close()
		return nil, err
	case <-ctx.Done():
		_ = pw.CloseWithError(ctx.Err())
		return nil, ctx.Err()
	}
	defer func() { _ = resp.Body.Close() }()
	defer func() { _ = pw.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		safeErr := sanitizeUpstreamErrorMessage(string(bytes.TrimSpace(body)))
		if safeErr == "" {
			safeErr = http.StatusText(resp.StatusCode)
		}
		c.JSON(http.StatusBadGateway, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "upstream_error",
				"message": "Cursor upstream request failed",
			},
		})
		return nil, fmt.Errorf("cursor upstream error %d: %s", resp.StatusCode, safeErr)
	}
	if parsed.OnUpstreamAccepted != nil {
		parsed.OnUpstreamAccepted()
	}

	usage := ClaudeUsage{InputTokens: estimateCursorTokens(userText)}
	if parsed.Stream {
		streamResult, err := s.streamCursorResponse(ctx, c, resp.Body, pw, blobStore, originalModel, &usage)
		if err != nil {
			return nil, err
		}
		return &ForwardResult{
			RequestID:        requestID,
			Usage:            usage,
			Model:            originalModel,
			UpstreamModel:    requestedModel.ModelID,
			Stream:           true,
			Duration:         time.Since(startTime),
			FirstTokenMs:     streamResult.firstTokenMs,
			ClientDisconnect: streamResult.clientDisconnect,
		}, nil
	}

	text, err := processCursorResponse(ctx, resp.Body, pw, blobStore, nil, &usage)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "upstream_error",
				"message": "Failed to parse Cursor upstream response",
			},
		})
		return nil, err
	}
	if usage.OutputTokens == 0 {
		usage.OutputTokens = estimateCursorTokens(text)
	}

	responseBody, err := json.Marshal(map[string]any{
		"id":            "msg_" + uuid.NewString(),
		"type":          "message",
		"role":          "assistant",
		"model":         originalModel,
		"content":       []any{map[string]any{"type": "text", "text": text}},
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage": map[string]int{
			"input_tokens":  usage.InputTokens,
			"output_tokens": usage.OutputTokens,
		},
	})
	if err != nil {
		return nil, err
	}
	c.Data(http.StatusOK, "application/json", responseBody)

	return &ForwardResult{
		RequestID:     requestID,
		Usage:         usage,
		Model:         originalModel,
		UpstreamModel: requestedModel.ModelID,
		Stream:        false,
		Duration:      time.Since(startTime),
	}, nil
}

type cursorHTTPResult struct {
	resp *http.Response
	err  error
}

type cursorStreamResult struct {
	firstTokenMs     *int
	clientDisconnect bool
}

func applyCursorRunHeaders(header http.Header, token, requestID string) {
	traceParent := newCursorTraceParent()
	header.Set("Authorization", "Bearer "+token)
	header.Set("Backend-Traceparent", traceParent)
	header.Set("Connect-Accept-Encoding", "gzip")
	header.Set("Connect-Protocol-Version", "1")
	header.Set("Content-Type", "application/connect+proto")
	header.Set("Traceparent", traceParent)
	header.Set("User-Agent", cursorUserAgent)
	header.Set("X-Cursor-Client-Type", "cli")
	header.Set("X-Cursor-Client-Version", "cli-0.0.0")
	header.Set("X-Ghost-Mode", "true")
	header.Set("X-Original-Request-Id", requestID)
	header.Set("X-Request-Id", requestID)
}

func newCursorTraceParent() string {
	traceID := make([]byte, 16)
	spanID := make([]byte, 8)
	if _, err := rand.Read(traceID); err != nil {
		return "00-00000000000000000000000000000000-0000000000000000-01"
	}
	if _, err := rand.Read(spanID); err != nil {
		return "00-" + hex.EncodeToString(traceID) + "-0000000000000000-01"
	}
	return "00-" + hex.EncodeToString(traceID) + "-" + hex.EncodeToString(spanID) + "-01"
}

func processCursorResponse(ctx context.Context, body io.Reader, writer io.Writer, blobStore map[string][]byte, onText func(string) error, usage *ClaudeUsage) (string, error) {
	var text strings.Builder
	for {
		select {
		case <-ctx.Done():
			return text.String(), ctx.Err()
		default:
		}

		frame, err := cursorproto.ReadConnectFrame(body)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return text.String(), nil
			}
			return text.String(), err
		}
		if err := respondToCursorControlFrame(frame.Payload, writer, blobStore); err != nil {
			return text.String(), err
		}
		events, err := cursorproto.DecodeAgentServerMessage(frame.Payload)
		if err != nil {
			return text.String(), err
		}
		for _, event := range events {
			switch event.Kind {
			case cursorproto.ServerEventText:
				if event.Text == "" {
					continue
				}
				text.WriteString(event.Text)
				if onText != nil {
					if err := onText(event.Text); err != nil {
						return text.String(), err
					}
				}
			case cursorproto.ServerEventThinking:
				continue
			case cursorproto.ServerEventTokenDelta:
				usage.OutputTokens += event.Tokens
			case cursorproto.ServerEventTurnEnded:
				return text.String(), nil
			case cursorproto.ServerEventKVServerMessage:
				continue
			}
		}
	}
}

func respondToCursorControlFrame(payload []byte, writer io.Writer, blobStore map[string][]byte) error {
	if event, err := cursorproto.DecodeKvServerEvent(payload); err != nil {
		return err
	} else if event != nil {
		switch event.Kind {
		case cursorproto.KvEventGetBlob:
			_, err := writer.Write(cursorproto.EncodeKvGetBlobResult(event.ID, blobStore[hex.EncodeToString(event.BlobID)], event.RequestMetadata))
			return err
		case cursorproto.KvEventSetBlob:
			if blobStore != nil && len(event.BlobID) > 0 {
				blobStore[hex.EncodeToString(event.BlobID)] = append([]byte(nil), event.BlobData...)
			}
			_, err := writer.Write(cursorproto.EncodeKvSetBlobResult(event.ID, event.RequestMetadata))
			return err
		}
	}

	event, err := cursorproto.DecodeExecServerEvent(payload)
	if err != nil {
		return err
	}
	if event != nil {
		switch event.Kind {
		case cursorproto.ExecEventRequestContext:
			_, err := writer.Write(cursorproto.EncodeRequestContextResponse(event.ID, event.ExecID))
			return err
		case cursorproto.ExecEventUnsupported:
			_, err := writer.Write(cursorproto.EncodeUnsupportedExecResult(*event, "Cursor built-in tools are not supported by Sub2API Cursor runtime"))
			return err
		}
	}
	return nil
}

func (s *GatewayService) streamCursorResponse(ctx context.Context, c *gin.Context, body io.Reader, writer io.Writer, blobStore map[string][]byte, model string, usage *ClaudeUsage) (cursorStreamResult, error) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	messageID := "msg_" + uuid.NewString()
	start := time.Now()
	if err := flushSSEJSON(c.Writer, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            messageID,
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]int{
				"input_tokens":  usage.InputTokens,
				"output_tokens": 0,
			},
		},
	}); err != nil {
		return drainCursorResponseAfterDisconnect(ctx, body, writer, blobStore, usage, nil)
	}
	if err := flushSSEJSON(c.Writer, "content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         0,
		"content_block": map[string]any{"type": "text", "text": ""},
	}); err != nil {
		return drainCursorResponseAfterDisconnect(ctx, body, writer, blobStore, usage, nil)
	}

	var firstTokenMs *int
	text, err := processCursorResponse(ctx, body, writer, blobStore, func(delta string) error {
		if firstTokenMs == nil {
			ms := int(time.Since(start).Milliseconds())
			firstTokenMs = &ms
		}
		if err := flushSSEJSON(c.Writer, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]string{"type": "text_delta", "text": delta},
		}); err != nil {
			return fmt.Errorf("%w: %w", errCursorClientDisconnect, err)
		}
		return nil
	}, usage)
	if err != nil {
		if errors.Is(err, errCursorClientDisconnect) {
			return drainCursorResponseAfterDisconnect(ctx, body, writer, blobStore, usage, firstTokenMs)
		}
		return cursorStreamResult{firstTokenMs: firstTokenMs}, err
	}
	if usage.OutputTokens == 0 {
		usage.OutputTokens = estimateCursorTokens(text)
	}
	if err := flushSSEJSON(c.Writer, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}); err != nil {
		return cursorStreamResult{firstTokenMs: firstTokenMs, clientDisconnect: true}, nil
	}
	if err := writeSSEMessageEnd(c.Writer, usage.OutputTokens); err != nil {
		return cursorStreamResult{firstTokenMs: firstTokenMs, clientDisconnect: true}, nil
	}
	return cursorStreamResult{firstTokenMs: firstTokenMs}, nil
}

var errCursorClientDisconnect = errors.New("cursor client disconnected")

func drainCursorResponseAfterDisconnect(ctx context.Context, body io.Reader, writer io.Writer, blobStore map[string][]byte, usage *ClaudeUsage, firstTokenMs *int) (cursorStreamResult, error) {
	drainCtx := context.WithoutCancel(ctx)
	if cursorDisconnectDrainTimeout > 0 {
		var cancel context.CancelFunc
		drainCtx, cancel = context.WithTimeout(drainCtx, cursorDisconnectDrainTimeout)
		defer cancel()
	}
	text, err := processCursorResponse(drainCtx, body, writer, blobStore, nil, usage)
	if err != nil {
		return cursorStreamResult{firstTokenMs: firstTokenMs, clientDisconnect: true}, err
	}
	if usage.OutputTokens == 0 {
		usage.OutputTokens = estimateCursorTokens(text)
	}
	return cursorStreamResult{firstTokenMs: firstTokenMs, clientDisconnect: true}, nil
}

func flattenCursorMessages(body []byte, system any) string {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return cursorSystemText(system)
	}

	systemTexts := make([]string, 0, 1)
	if text := cursorSystemText(system); text != "" {
		systemTexts = append(systemTexts, text)
	}
	turns := make([]gjson.Result, 0)
	for _, msg := range messages.Array() {
		if msg.Get("role").String() == "system" {
			if text := cursorContentText(msg.Get("content")); text != "" {
				systemTexts = append(systemTexts, text)
			}
			continue
		}
		turns = append(turns, msg)
	}
	if len(turns) == 1 && turns[0].Get("role").String() == "user" && !turns[0].Get("tool_calls").Exists() {
		userText := cursorContentText(turns[0].Get("content"))
		return joinNonEmpty("\n\n", append(systemTexts, userText)...)
	}

	lines := make([]string, 0, len(turns))
	for _, msg := range turns {
		role := msg.Get("role").String()
		text := cursorContentText(msg.Get("content"))
		switch role {
		case "user":
			if text != "" {
				lines = append(lines, "User: "+text)
			}
		case "assistant":
			if text != "" {
				lines = append(lines, "Assistant: "+text)
			}
		case "tool":
			callID := msg.Get("tool_call_id").String()
			if callID == "" {
				callID = "(unknown)"
			}
			lines = append(lines, "Tool result ("+callID+"): "+text)
		default:
			if text != "" {
				lines = append(lines, role+": "+text)
			}
		}
	}
	return joinNonEmpty("\n\n", append(systemTexts, strings.Join(lines, "\n\n"))...)
}

func cursorSystemText(system any) string {
	switch v := system.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if text, ok := m["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, strings.TrimSpace(text))
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func cursorContentText(content gjson.Result) string {
	if content.Type == gjson.String {
		return strings.TrimSpace(content.String())
	}
	if !content.IsArray() {
		return ""
	}
	parts := make([]string, 0)
	for _, part := range content.Array() {
		if text := strings.TrimSpace(part.Get("text").String()); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func joinNonEmpty(sep string, values ...string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, sep)
}

func estimateCursorTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	runes := len([]rune(text))
	if runes <= 4 {
		return 1
	}
	return (runes + 3) / 4
}
