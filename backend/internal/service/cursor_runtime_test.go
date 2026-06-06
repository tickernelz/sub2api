package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tickernelz/sub2api/internal/pkg/cursorproto"
	"github.com/tickernelz/sub2api/internal/pkg/tlsfingerprint"
)

func TestGatewayServiceForwardCursorMessagesUsesNativeRunProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := newCursorRuntimeTestUpstream(
		cursorTestFrame(cursorTestExecRequestContextPayload(7, "exec-1")),
		cursorTestFrame(cursorTestExecReadPayload(8, "exec-read", "secret.txt")),
		cursorTestFrame(cursorTestTextPayload("pong")),
		cursorTestFrame(cursorTestTurnEndedPayload()),
	)
	svc := &GatewayService{httpUpstream: upstream}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"composer-2-fast","messages":[{"role":"user","content":"ping"}],"stream":false}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	accepted := false
	parsed.OnUpstreamAccepted = func() { accepted = true }
	account := &Account{
		ID:          101,
		Name:        "cursor-test",
		Platform:    PlatformCursor,
		Type:        AccountTypeOAuth,
		Concurrency: 3,
		Credentials: map[string]any{
			"access_token": "user-1::cursor-token",
		},
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"type":"message"`)
	require.Contains(t, rec.Body.String(), `"model":"composer-2-fast"`)
	require.Contains(t, rec.Body.String(), `"text":"pong"`)
	require.Contains(t, rec.Body.String(), `"stop_reason":"end_turn"`)
	require.Equal(t, "composer-2-fast", result.Model)
	require.True(t, accepted)
	require.Equal(t, "composer-2", result.UpstreamModel)
	require.Equal(t, ClaudeUsage{InputTokens: 1, OutputTokens: 1}, result.Usage)

	require.NotNil(t, upstream.request)
	require.Equal(t, "https://agentn.global.api5.cursor.sh/agent.v1.AgentService/Run", upstream.request.URL.String())
	require.Equal(t, "Bearer cursor-token", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "application/connect+proto", upstream.request.Header.Get("Content-Type"))
	require.Equal(t, "1", upstream.request.Header.Get("Connect-Protocol-Version"))
	require.Equal(t, "gzip", upstream.request.Header.Get("Connect-Accept-Encoding"))
	require.Equal(t, "cli", upstream.request.Header.Get("X-Cursor-Client-Type"))
	require.NotEmpty(t, upstream.request.Header.Get("X-Request-Id"))
	require.Empty(t, upstream.request.Header.Get("X-Cursor-Checksum"))
	require.Equal(t, HTTPUpstreamProfileCursor, HTTPUpstreamProfileFromContext(upstream.request.Context()))

	frames := upstream.waitFrames(t)
	require.GreaterOrEqual(t, len(frames), 3)
	expectedAck, err := cursorproto.ReadConnectFrame(bytes.NewReader(cursorproto.EncodeRequestContextResponse(7, "exec-1")))
	require.NoError(t, err)
	require.Equal(t, expectedAck.Payload, frames[1].Payload)
	expectedRejection, err := cursorproto.ReadConnectFrame(bytes.NewReader(cursorproto.EncodeUnsupportedExecResult(cursorproto.ExecServerEvent{Kind: cursorproto.ExecEventUnsupported, ID: 8, ExecID: "exec-read", ResultFieldNumber: 7, Path: "secret.txt"}, "Cursor built-in tools are not supported by Sub2API Cursor runtime")))
	require.NoError(t, err)
	require.Equal(t, expectedRejection.Payload, frames[2].Payload)
}

func TestProcessCursorResponseRejectsTruncatedFrame(t *testing.T) {
	_, err := processCursorResponse(context.Background(), bytes.NewReader([]byte{0, 0, 0, 0, 8, 1, 2}), io.Discard, nil, nil, &ClaudeUsage{})
	require.Error(t, err)
}

func TestProcessCursorResponseDoesNotExposeThinkingAsText(t *testing.T) {
	body := bytes.Join([][]byte{
		cursorTestFrame(cursorTestThinkingPayload("hidden chain of thought")),
		cursorTestFrame(cursorTestTextPayload("answer")),
		cursorTestFrame(cursorTestTurnEndedPayload()),
	}, nil)
	var deltas []string
	text, err := processCursorResponse(context.Background(), bytes.NewReader(body), io.Discard, nil, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	}, &ClaudeUsage{})
	require.NoError(t, err)
	require.Equal(t, "answer", text)
	require.Equal(t, []string{"answer"}, deltas)
}

func TestProcessCursorResponseContinuesAfterKVControlFrame(t *testing.T) {
	body := bytes.Join([][]byte{
		cursorTestFrame(cursorTestTextPayload("first ")),
		cursorTestFrame(cursorTestKVSetBlobPayload(9, "blob-id", "blob-data", []byte{1, 2, 3})),
		cursorTestFrame(cursorTestTextPayload("second")),
		cursorTestFrame(cursorTestTurnEndedPayload()),
	}, nil)
	var control bytes.Buffer
	text, err := processCursorResponse(context.Background(), bytes.NewReader(body), &control, nil, nil, &ClaudeUsage{})
	require.NoError(t, err)
	require.Equal(t, "first second", text)
	require.NotEmpty(t, control.Bytes(), "KV control frame should still be acknowledged")
}

func TestGatewayServiceForwardCursorMessagesClientDisconnectDrainsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := newCursorRuntimeTestUpstream(
		cursorTestFrame(cursorTestTextPayload("lost")),
		cursorTestFrame(cursorTestTokenDeltaPayload(5)),
		cursorTestFrame(cursorTestTurnEndedPayload()),
	)
	svc := &GatewayService{httpUpstream: upstream}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Writer = &cursorFailingWriter{ResponseWriter: c.Writer, failAfter: 0, cancelOnFail: cancel}
	body := []byte(`{"model":"composer-2.5","messages":[{"role":"user","content":"ping"}],"stream":true}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	account := &Account{ID: 103, Name: "cursor-disconnect-test", Platform: PlatformCursor, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{"access_token": "cursor-token"}}

	result, err := svc.Forward(ctx, c, account, parsed)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 5, result.Usage.OutputTokens)
}

func TestGatewayServiceForwardCursorMessagesStreamsAnthropicSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := newCursorRuntimeTestUpstream(
		cursorTestFrame(cursorTestExecRequestContextPayload(7, "exec-1")),
		cursorTestFrame(cursorTestTextPayload("pon")),
		cursorTestFrame(cursorTestTextPayload("g")),
		cursorTestFrame(cursorTestTurnEndedPayload()),
	)
	svc := &GatewayService{httpUpstream: upstream}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"composer-2.5","messages":[{"role":"user","content":"ping"}],"stream":true}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	account := &Account{
		ID:          102,
		Name:        "cursor-stream-test",
		Platform:    PlatformCursor,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "cursor-token"},
	}

	result, err := svc.Forward(context.Background(), c, account, parsed)

	require.NoError(t, err)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	bodyText := rec.Body.String()
	require.Contains(t, bodyText, "event: message_start")
	require.Contains(t, bodyText, "event: content_block_delta")
	require.Contains(t, bodyText, `"text":"pon"`)
	require.Contains(t, bodyText, `"text":"g"`)
	require.Contains(t, bodyText, "event: message_stop")
	frames := upstream.waitFrames(t)
	require.GreaterOrEqual(t, len(frames), 2)
}

type cursorFailingWriter struct {
	gin.ResponseWriter
	writes       int
	failAfter    int
	cancelOnFail func()
}

func (w *cursorFailingWriter) Write(p []byte) (int, error) {
	if w.writes >= w.failAfter {
		if w.cancelOnFail != nil {
			w.cancelOnFail()
		}
		return 0, io.ErrClosedPipe
	}
	w.writes++
	return w.ResponseWriter.Write(p)
}

type cursorRuntimeTestUpstream struct {
	response []byte
	request  *http.Request

	mu     sync.Mutex
	frames []cursorproto.ConnectFrame
	done   chan struct{}
}

func newCursorRuntimeTestUpstream(responseParts ...[]byte) *cursorRuntimeTestUpstream {
	return &cursorRuntimeTestUpstream{
		response: bytes.Join(responseParts, nil),
		done:     make(chan struct{}),
	}
}

func (u *cursorRuntimeTestUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.request = req
	go func() {
		defer close(u.done)
		for {
			frame, err := cursorproto.ReadConnectFrame(req.Body)
			if err != nil {
				return
			}
			u.mu.Lock()
			u.frames = append(u.frames, frame)
			u.mu.Unlock()
		}
	}()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(u.response)),
	}, nil
}

func (u *cursorRuntimeTestUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *cursorRuntimeTestUpstream) waitFrames(t *testing.T) []cursorproto.ConnectFrame {
	t.Helper()
	select {
	case <-u.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cursor request body to close")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]cursorproto.ConnectFrame(nil), u.frames...)
}

func cursorTestFrame(payload []byte) []byte {
	return cursorproto.WrapConnectFrame(payload)
}

func cursorTestExecRequestContextPayload(id uint64, execID string) []byte {
	execServer := append(cursorTestVarintField(1, id), cursorTestMessage(10)...)
	execServer = append(execServer, cursorTestString(15, execID)...)
	return cursorTestMessage(2, execServer)
}

func cursorTestExecReadPayload(id uint64, execID, path string) []byte {
	execServer := append(cursorTestVarintField(1, id), cursorTestMessage(7, cursorTestString(1, path))...)
	execServer = append(execServer, cursorTestString(15, execID)...)
	return cursorTestMessage(2, execServer)
}

func cursorTestTextPayload(text string) []byte {
	return cursorTestMessage(1, cursorTestMessage(1, cursorTestString(1, text)))
}

func cursorTestThinkingPayload(text string) []byte {
	return cursorTestMessage(1, cursorTestMessage(4, cursorTestString(1, text)))
}

func cursorTestTokenDeltaPayload(tokens uint64) []byte {
	return cursorTestMessage(1, cursorTestMessage(8, cursorTestVarintField(1, tokens)))
}

func cursorTestKVSetBlobPayload(id uint64, blobID, blobData string, metadata []byte) []byte {
	setArgs := bytes.Join([][]byte{cursorTestBytes(1, []byte(blobID)), cursorTestBytes(2, []byte(blobData))}, nil)
	kv := bytes.Join([][]byte{cursorTestVarintField(1, id), cursorTestMessage(3, setArgs), cursorTestBytes(4, metadata)}, nil)
	return cursorTestMessage(4, kv)
}

func cursorTestTurnEndedPayload() []byte {
	return cursorTestMessage(1, cursorTestMessage(14))
}

func cursorTestString(number int, value string) []byte {
	return cursorTestBytes(number, []byte(value))
}

func cursorTestMessage(number int, parts ...[]byte) []byte {
	return cursorTestBytes(number, bytes.Join(parts, nil))
}

func cursorTestBytes(number int, value []byte) []byte {
	out := cursorTestVarint(uint64(number<<3) | 2)
	out = append(out, cursorTestVarint(uint64(len(value)))...)
	out = append(out, value...)
	return out
}

func cursorTestVarintField(number int, value uint64) []byte {
	out := cursorTestVarint(uint64(number << 3))
	out = append(out, cursorTestVarint(value)...)
	return out
}

func cursorTestVarint(value uint64) []byte {
	if value == 0 {
		return []byte{0}
	}
	out := make([]byte, 0, 10)
	for value > 0x7f {
		out = append(out, byte(value&0x7f)|0x80)
		value >>= 7
	}
	out = append(out, byte(value))
	return out
}
