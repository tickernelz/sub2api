//go:build perchparity

package perch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

type parityRequest struct {
	Index   int     `json:"index"`
	Method  string  `json:"method"`
	Path    string  `json:"path"`
	Raw     string  `json:"raw_base64"`
	StartMs float64 `json:"start_ms"`
}

type parityCapture struct {
	StartedAt   string          `json:"started_at"`
	Requests    []parityRequest `json:"requests"`
	Connections int             `json:"connections"`
	JA3         string          `json:"ja3"`
	JA3Raw      string          `json:"ja3_raw"`
}

type recordingConn struct {
	net.Conn
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *recordingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.mu.Lock()
		_, _ = c.buf.Write(p[:n])
		c.mu.Unlock()
	}
	return n, err
}

func (c *recordingConn) take() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]byte(nil), c.buf.Bytes()...)
	c.buf.Reset()
	return out
}

type parityServer struct {
	listener    net.Listener
	capture     parityCapture
	mu          sync.Mutex
	start       time.Time
	accountBody []byte
	defaultBody []byte
	ticketBody  []byte
	streamBody  []byte
}

func newParityServer(t *testing.T) *parityServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &parityServer{listener: listener, start: time.Now()}
	s.capture.StartedAt = s.start.UTC().Format(time.RFC3339Nano)
	s.accountBody = parityJSON(map[string]any{
		"ok":      true,
		"profile": map[string]any{"id": "11111111-1111-4111-8111-111111111111", "email": "sample@example.com", "name": "Sample"},
		"session": map[string]any{
			"userId": "11111111-1111-4111-8111-111111111111", "workspaceId": "22222222-2222-4222-8222-222222222222",
			"orgId": "33333333-3333-4333-8333-333333333333", "planCode": "pilot", "planName": "Pilot", "entitlements": []any{},
		},
	})
	s.defaultBody = parityJSON(map[string]any{
		"ok": true, "view": "public", "source": "database",
		"selection": map[string]any{
			"thinkingMode": false, "chatModelId": "wandb-kimi-k2-6", "sandboxPlannerModelId": "wandb-kimi-k2-6",
			"reportModelId": "wandb-kimi-k2-6", "thinkingModelId": "wandb-kimi-k2-6", "supervisorModelId": "wandb-kimi-k2-6",
			"fastWorkerModelId": "wandb-kimi-k2-6", "codeWorkerModelId": "wandb-kimi-k2-6", "dataWorkerModelId": "wandb-kimi-k2-6",
			"writerModelId": "wandb-kimi-k2-6", "visionModelId": "wandb-kimi-k2-6", "verifierModelId": "wandb-kimi-k2-6",
			"visionFallbackModelIds": nil, "modelBackendPreset": "wandb_kimi",
		},
	})
	s.ticketBody = parityJSON(map[string]any{
		"ok": true, "ticket": "header.payload.signature", "ticketId": "44444444-4444-4444-8444-444444444444",
		"runId": "tkt-cli-1793207619077-1li5qu4h", "surface": "cli", "profile": "standard", "enforced": false,
		"expiresAt": time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
	})
	s.streamBody = []byte("data: {\"type\":\"answer_delta\",\"text\":\"pong\"}\n\n" +
		"data: {\"type\":\"done\",\"ok\":true,\"text\":\"pong\",\"finishReason\":\"stop\",\"model\":\"wandb-kimi-k2-6\"," +
		"\"usage\":{\"inputTokens\":10,\"outputTokens\":1,\"cacheCreationInputTokens\":0,\"cacheReadInputTokens\":0,\"totalTokens\":11}}\n\n")
	go s.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return s
}

func parityJSON(payload map[string]any) []byte {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return raw
}

func (s *parityServer) addr() string { return s.listener.Addr().String() }

func (s *parityServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.capture.Connections++
		s.mu.Unlock()
		go s.handle(&recordingConn{Conn: conn})
	}
}

func (s *parityServer) handle(conn *recordingConn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			return
		}
		_ = req.Body.Close()
		raw := conn.take()
		s.mu.Lock()
		s.capture.Requests = append(s.capture.Requests, parityRequest{
			Index:   len(s.capture.Requests),
			Method:  req.Method,
			Path:    req.URL.Path,
			Raw:     base64.StdEncoding.EncodeToString(raw),
			StartMs: float64(time.Since(s.start).Microseconds()) / 1000,
		})
		s.mu.Unlock()
		if !s.respond(conn, req.URL.Path) {
			return
		}
	}
}

func (s *parityServer) respond(conn net.Conn, path string) bool {
	status := http.StatusOK
	body := []byte("{}")
	contentType := "application/json"
	switch path {
	case "/api/perch-terminal/public-model-default":
		body = s.defaultBody
	case "/api/perchai/account":
		body = s.accountBody
	case "/api/perch-terminal/turn-ticket":
		body = s.ticketBody
	case "/api/perch-terminal/model-call":
		body = s.streamBody
		contentType = "text/event-stream"
	case "/api/perch-terminal/cli-auth/config":
		body = parityJSON(map[string]any{"ok": true, "supabaseUrl": "http://" + s.addr(), "supabaseAnonKey": "sb_publishable_test"})
	default:
		status = http.StatusNotFound
	}
	resp := &http.Response{
		StatusCode:    status,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{contentType}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	if err := resp.Write(conn); err != nil {
		return false
	}
	return true
}

func (s *parityServer) write(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	encoded, err := json.MarshalIndent(s.capture, "", "  ")
	if err != nil {
		t.Fatalf("encode capture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "capture.json"), append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
}

func TestParityCapture(t *testing.T) {
	out := strings.TrimSpace(os.Getenv("PERCH_PARITY_OUT"))
	if out == "" {
		out = filepath.Join(t.TempDir(), "capture")
	}
	server := newParityServer(t)
	addr := server.addr()

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	control := NewWireDoer(dialer.DialContext, 30*time.Second, 30*time.Second)
	stream := control.Share(0, 30*time.Second)
	creds := Credentials{
		AppURL:       "http://" + addr,
		AccessToken:  "eyJhbGciOiJFUzI1NiJ9.eyJzdWIiOiJzYW1wbGUifQ.signature",
		RefreshToken: "refresh-token",
		UserID:       "11111111-1111-4111-8111-111111111111",
		Email:        "sample@example.com",
	}
	session := NewSession(creds, WithHTTPClient(control))
	client := NewClient(session, WithClientHTTPStream(stream), WithMinRequestInterval(DefaultMinRequestInterval))

	req := &apicompat.ChatCompletionsRequest{
		Model: "standard",
		Messages: []apicompat.ChatMessage{
			{Role: "user", Content: json.RawMessage("\"Reply with exactly: PARITY-CLI-1\"")},
		},
	}
	call, err := BuildModelCallRequest(req, CallOptions{Now: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("BuildModelCallRequest: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	outcome, err := client.CallModel(ctx, call)
	if err != nil {
		t.Fatalf("CallModel: %v", err)
	}
	if !outcome.OK {
		t.Fatalf("outcome not ok: %#v", outcome)
	}

	hello := captureClientHello(t)
	server.mu.Lock()
	server.capture.JA3 = hello.hash
	server.capture.JA3Raw = hello.raw
	server.mu.Unlock()
	server.write(t, out)
	t.Logf("captured %d requests on %d connections -> %s", len(server.capture.Requests), server.capture.Connections, out)
}

type helloCapture struct {
	hash string
	raw  string
}

func captureClientHello(t *testing.T) helloCapture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()
	result := make(chan helloCapture, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			result <- helloCapture{}
			return
		}
		defer func() { _ = conn.Close() }()
		ja3, err := readJA3(conn)
		if err != nil {
			result <- helloCapture{}
			return
		}
		result <- helloCapture{hash: ja3, raw: lastJA3Raw}
	}()
	dialer := FingerprintDialer(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _ := dialer.DialTLSContext(ctx, "tcp", listener.Addr().String())
	if conn != nil {
		_ = conn.Close()
	}
	select {
	case out := <-result:
		return out
	case <-time.After(10 * time.Second):
		t.Fatalf("no client hello captured")
		return helloCapture{}
	}
}
