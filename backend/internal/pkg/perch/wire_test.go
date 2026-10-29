package perch

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const CRLF = "\r\n"

type rawServer struct {
	listener net.Listener
	respond  func(index int, request string) string

	mu       sync.Mutex
	requests []string
	conns    int
}

func newRawServer(t *testing.T, respond func(index int, request string) string) *rawServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &rawServer{listener: listener, respond: respond}
	go server.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (s *rawServer) addr() string { return s.listener.Addr().String() }

func (s *rawServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns++
		s.mu.Unlock()
		go s.handle(conn)
	}
}

func (s *rawServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	for {
		request, err := readRawRequest(reader)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.requests = append(s.requests, request)
		index := len(s.requests) - 1
		s.mu.Unlock()
		response := s.respond(index, request)
		if _, err := conn.Write([]byte(response)); err != nil {
			return
		}
		if strings.Contains(response, "connection: close") {
			return
		}
	}
}

func (s *rawServer) snapshot() ([]string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...), s.conns
}

func readRawRequest(reader *bufio.Reader) (string, error) {
	var buf bytes.Buffer
	for {
		line, err := reader.ReadString('\n')
		_, _ = buf.WriteString(line)
		if err != nil {
			return "", err
		}
		if line == CRLF {
			break
		}
	}
	head := buf.String()
	length := 0
	for _, line := range strings.Split(head, CRLF) {
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			_, _ = fmt.Sscanf(strings.TrimSpace(line[len("content-length:"):]), "%d", &length)
		}
	}
	if length > 0 {
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			return "", err
		}
		_, _ = buf.Write(body)
	}
	return buf.String(), nil
}

func dialDirect(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, addr)
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	payload, err := os.ReadFile("testdata/wire/" + name)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return string(payload)
}

func newModelCallRequest(t *testing.T, addr string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+addr+modelCallPath, bytes.NewReader([]byte(`{"hello":"world"}`)))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-perch-call-delta", "1")
	req.Header.Set("accept", "text/event-stream")
	req.Header.Set("authorization", "Bearer test-token")
	req.Header.Set("user-agent", "perchai-cli/2.4.120")
	req.Header.Set("x-perch-locale", "en")
	req.Header.Set("x-perch-turn-ticket", "test.ticket")
	return req
}
func TestWireDoerMatchesRecordedModelCallBytes(t *testing.T) {
	server := newRawServer(t, func(int, string) string { return "HTTP/1.1 200 OK\r\ncontent-length: 2\r\n\r\nok" })
	doer := NewWireDoer(dialDirect, 0, 5*time.Second)

	resp, err := doer.Do(newModelCallRequest(t, server.addr()))
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	requests, _ := server.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	want := strings.ReplaceAll(readFixture(t, "model-call.http"), "<host>", server.addr())
	if requests[0] != want {
		t.Fatalf("wire mismatch\n--- got ---\n%s\n--- want ---\n%s", requests[0], want)
	}
}

func TestWireDoerMatchesRecordedTurnTicketBytes(t *testing.T) {
	server := newRawServer(t, func(int, string) string { return "HTTP/1.1 200 OK\r\ncontent-length: 2\r\n\r\nok" })
	doer := NewWireDoer(dialDirect, 5*time.Second, 5*time.Second)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+server.addr()+turnTicketPath, strings.NewReader(`{"surface":"cli","profile":"standard"}`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer test-token")
	req.Header.Set("user-agent", "perchai-cli/2.4.120")
	req.Header.Set("x-perch-locale", "en")

	resp, err := doer.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	requests, _ := server.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	want := strings.ReplaceAll(readFixture(t, "turn-ticket.http"), "<host>", server.addr())
	if requests[0] != want {
		t.Fatalf("wire mismatch\n--- got ---\n%s\n--- want ---\n%s", requests[0], want)
	}
}
func TestWireDoerReusesConnectionAcrossRequests(t *testing.T) {
	server := newRawServer(t, func(int, string) string { return "HTTP/1.1 200 OK\r\ncontent-length: 2\r\n\r\nok" })
	doer := NewWireDoer(dialDirect, 0, 5*time.Second)

	for i := 0; i < 2; i++ {
		resp, err := doer.Do(newModelCallRequest(t, server.addr()))
		if err != nil {
			t.Fatalf("do %d: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("close %d: %v", i, err)
		}
	}

	requests, conns := server.snapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if conns != 1 {
		t.Fatalf("connections = %d, want 1", conns)
	}
}

func TestWireDoerDropsConnectionWhenBodyClosedEarly(t *testing.T) {
	server := newRawServer(t, func(int, string) string {
		return "HTTP/1.1 200 OK" + CRLF + "content-type: text/event-stream" + CRLF + "transfer-encoding: chunked" + CRLF + CRLF + "7" + CRLF + "data: 1" + CRLF
	})
	doer := NewWireDoer(dialDirect, 0, 5*time.Second)

	first, err := doer.Do(newModelCallRequest(t, server.addr()))
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if err := first.Body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, err := doer.Do(newModelCallRequest(t, server.addr()))
	if err != nil {
		t.Fatalf("second do: %v", err)
	}
	_ = second.Body.Close()

	_, conns := server.snapshot()
	if conns != 2 {
		t.Fatalf("connections = %d, want 2", conns)
	}
}

func TestWireDoerControlTimeoutBoundsExchange(t *testing.T) {
	server := newRawServer(t, func(int, string) string { return "" })
	doer := NewWireDoer(dialDirect, 150*time.Millisecond, 5*time.Second)

	start := time.Now()
	if _, err := doer.Do(newModelCallRequest(t, server.addr())); err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("elapsed = %s, want bounded by control timeout", elapsed)
	}
}
