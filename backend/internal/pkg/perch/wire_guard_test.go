package perch

import (
	"bufio"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuildWireRequestRejectsInjectedHeaderValue(t *testing.T) {
	req := newModelCallRequest(t, "127.0.0.1:1")
	req.Header.Set("authorization", "Bearer token\r\nx-perch-turn-ticket: forged")

	payload, err := buildWireRequest(req, nil)
	if !errors.Is(err, errWireHeader) {
		t.Fatalf("err = %v, payload = %q", err, string(payload))
	}
	if payload != nil {
		t.Fatalf("payload = %q, want nil", string(payload))
	}
}

func TestBuildWireRequestRejectsInjectedHeaderName(t *testing.T) {
	req := newModelCallRequest(t, "127.0.0.1:1")
	req.Header["x-perch-locale\r\nx-perch-turn-ticket"] = []string{"en"}

	payload, err := buildWireRequest(req, nil)
	if !errors.Is(err, errWireHeader) {
		t.Fatalf("err = %v, payload = %q", err, string(payload))
	}
	if strings.Contains(string(payload), "forged") {
		t.Fatalf("payload carried the injected line: %q", string(payload))
	}
}

func TestBuildWireRequestRejectsBareNewlineValue(t *testing.T) {
	req := newModelCallRequest(t, "127.0.0.1:1")
	req.Header.Set("x-perch-turn-ticket", "ticket\nforged: 1")

	if _, err := buildWireRequest(req, nil); !errors.Is(err, errWireHeader) {
		t.Fatalf("err = %v, want %v", err, errWireHeader)
	}
}

func TestWireDoerRejectsInjectedHeaderBeforeWrite(t *testing.T) {
	server := newRawServer(t, func(int, string) string {
		return "HTTP/1.1 200 OK" + CRLF + "content-length: 2" + CRLF + CRLF + "ok"
	})
	req := newModelCallRequest(t, server.addr())
	req.Header.Set("x-perch-turn-ticket", "test.ticket\r\nx-perch-call-delta: 0")
	doer := NewWireDoer(dialDirect, 0, 5*time.Second)

	resp, err := doer.Do(req)
	if resp != nil {
		t.Fatalf("resp = %#v, want nil", resp)
	}
	if !errors.Is(err, errWireHeader) {
		t.Fatalf("err = %v, want %v", err, errWireHeader)
	}
	requests, _ := server.snapshot()
	if len(requests) != 0 {
		t.Fatalf("server saw %d requests, want 0: %q", len(requests), requests)
	}
}

func idleProbePair(t *testing.T, pending string) net.Conn {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	ready := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			close(ready)
			return
		}
		t.Cleanup(func() { _ = conn.Close() })
		if pending != "" {
			_, _ = conn.Write([]byte(pending))
		}
		close(ready)
	}()

	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	<-ready
	time.Sleep(50 * time.Millisecond)
	return client
}

func TestWireConnAliveRejectsConnectionWithPendingBytes(t *testing.T) {
	client := idleProbePair(t, "x")

	if wireConnAlive(client) {
		t.Fatalf("connection with an unread byte was reported alive")
	}
}

func TestWireConnAliveKeepsQuietConnection(t *testing.T) {
	client := idleProbePair(t, "")

	if !wireConnAlive(client) {
		t.Fatalf("quiet connection was reported dead")
	}
}

type countingConn struct {
	net.Conn

	mu     sync.Mutex
	closes int
}

func (c *countingConn) Close() error {
	c.mu.Lock()
	c.closes++
	c.mu.Unlock()
	return nil
}

func (c *countingConn) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closes
}

func TestWireBodySettlesOnceUnderConcurrentReadAndClose(t *testing.T) {
	conn := &countingConn{}
	doer := NewWireDoer(dialDirect, 0, time.Second)
	body := &wireBody{
		body:     io.NopCloser(strings.NewReader("")),
		reader:   bufio.NewReader(conn),
		doer:     doer,
		conn:     conn,
		addr:     "perch.test:443",
		reusable: true,
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = body.Close()
		}()
	}
	wg.Wait()

	if got := conn.closeCount(); got != 1 {
		t.Fatalf("conn closed %d times, want exactly 1", got)
	}
	doer.pool.mu.Lock()
	idle := doer.pool.idle["perch.test:443"]
	doer.pool.mu.Unlock()
	if idle != nil {
		t.Fatalf("idle connection = %#v, want none after a non-complete settle", idle)
	}
}
