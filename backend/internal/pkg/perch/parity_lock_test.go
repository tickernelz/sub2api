package perch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

type parityLockServer struct {
	listener net.Listener
	mu       sync.Mutex
	requests []recordedRequest
	active   int
	peak     int
	hold     time.Duration
	holdPath string
	barrier  chan struct{}
	waiting  chan struct{}
	respond  map[string]func(w io.Writer, path string)
}

type recordedRequest struct {
	method  string
	path    string
	body    []byte
	headers http.Header
	at      time.Time
}

func newParityLockServer(t *testing.T) *parityLockServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &parityLockServer{listener: listener, respond: map[string]func(w io.Writer, path string){}}
	go s.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return s
}

func (s *parityLockServer) url() string { return "http://" + s.listener.Addr().String() }

func (s *parityLockServer) json(path, body string) {
	s.respond[path] = func(w io.Writer, _ string) {
		_, _ = io.WriteString(w, "HTTP/1.1 200 OK\r\ncontent-type: application/json\r\ncontent-length: "+strconv.Itoa(len(body))+"\r\n\r\n"+body)
	}
}

func (s *parityLockServer) defaults(t *testing.T) {
	t.Helper()
	s.json("/api/perch-terminal/public-model-default", `{"ok":true,"view":"public","selection":{"chatModelId":"wandb-kimi-k2-6"},"source":"database"}`)
	s.json("/api/perchai/account", `{"ok":true,"profile":{"id":"11111111-1111-4111-8111-111111111111","email":"sample@example.com"},"session":{"userId":"11111111-1111-4111-8111-111111111111","workspaceId":"22222222-2222-4222-8222-222222222222","orgId":"33333333-3333-4333-8333-333333333333","planCode":"pilot"}}`)
	var mints int32
	s.respond["/api/perch-terminal/turn-ticket"] = func(w io.Writer, _ string) {
		n := atomic.AddInt32(&mints, 1)
		body := fmt.Sprintf(`{"ok":true,"ticket":"header.payload.signature-%d","ticketId":"44444444-4444-4444-8444-444444444444","runId":"tkt-cli-1793207619077-1li5qu4h","profile":"standard","expiresAt":"%s"}`, n, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		_, _ = io.WriteString(w, "HTTP/1.1 200 OK\r\ncontent-type: application/json\r\ncontent-length: "+strconv.Itoa(len(body))+"\r\n\r\n"+body)
	}
}

func (s *parityLockServer) stream(path, body string) {
	s.respond[path] = func(w io.Writer, _ string) {
		_, _ = io.WriteString(w, "HTTP/1.1 200 OK\r\ncontent-type: text/event-stream\r\ncontent-length: "+strconv.Itoa(len(body))+"\r\n\r\n"+body)
	}
}

func (s *parityLockServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *parityLockServer) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		body, _ := io.ReadAll(req.Body)
		_ = req.Body.Close()
		s.mu.Lock()
		s.requests = append(s.requests, recordedRequest{method: req.Method, path: req.URL.Path, body: body, headers: req.Header.Clone(), at: time.Now()})
		s.active++
		if s.active > s.peak {
			s.peak = s.active
		}
		hold := s.hold
		holdPath := s.holdPath
		barrier := s.barrier
		waiting := s.waiting
		handler := s.respond[req.URL.Path]
		s.mu.Unlock()
		if holdPath == "" || req.URL.Path == holdPath {
			if hold > 0 {
				time.Sleep(hold)
			}
			if barrier != nil {
				if waiting != nil {
					waiting <- struct{}{}
				}
				<-barrier
			}
		}
		if handler != nil {
			handler(conn, req.URL.Path)
		} else {
			_, _ = io.WriteString(conn, "HTTP/1.1 404 Not Found\r\ncontent-length: 0\r\n\r\n")
		}
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}
}

func (s *parityLockServer) paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.requests))
	for _, entry := range s.requests {
		out = append(out, entry.path)
	}
	return out
}

func (s *parityLockServer) bodyFor(path string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.requests {
		if entry.path == path {
			return entry.body
		}
	}
	return nil
}

func lockClient(server *parityLockServer, opts ...ClientOption) *Client {
	creds := Credentials{
		AppURL:       server.url(),
		AccessToken:  "eyJhbGciOiJFUzI1NiJ9.eyJzdWIiOiJzYW1wbGUifQ.signature",
		RefreshToken: "refresh-token",
		UserID:       "11111111-1111-4111-8111-111111111111",
	}
	session := NewSession(creds)
	base := []ClientOption{WithRetryPolicy(0, []time.Duration{0, 0})}
	return NewClient(session, append(base, opts...)...)
}

func lockCall(t *testing.T) ModelCallRequest {
	t.Helper()
	call, err := BuildModelCallRequest(&apicompat.ChatCompletionsRequest{
		Model:    "wandb-kimi-k2-6",
		Messages: []apicompat.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}, CallOptions{Lane: LaneChat})
	if err != nil {
		t.Fatalf("BuildModelCallRequest: %v", err)
	}
	return call
}

const parityDoneStream = "data: {\"type\":\"done\",\"ok\":true,\"text\":\"pong\",\"finishReason\":\"stop\"}\n\n"

func TestParityModelCallBodyCarriesCLIDefaults(t *testing.T) {
	call, err := BuildModelCallRequest(&apicompat.ChatCompletionsRequest{
		Model:    "wandb-kimi-k2-6",
		Messages: []apicompat.ChatMessage{{Role: "user", Content: json.RawMessage(`"hi"`)}},
	}, CallOptions{})
	if err != nil {
		t.Fatalf("BuildModelCallRequest: %v", err)
	}
	if call.Request.Temperature == nil || *call.Request.Temperature != 0.6 {
		t.Fatalf("temperature = %v, want 0.6 as the CLI sends", call.Request.Temperature)
	}
	if call.Request.MaxTokens == nil || *call.Request.MaxTokens != 32000 {
		t.Fatalf("maxTokens = %v, want 32000 as the CLI sends", call.Request.MaxTokens)
	}
	if string(call.Request.ToolChoice) != `"auto"` {
		t.Fatalf("toolChoice = %s, want auto", call.Request.ToolChoice)
	}
}

func TestParityTurnTicketBodyKeepsCLIKeyOrder(t *testing.T) {
	server := newParityLockServer(t)
	server.defaults(t)
	client := lockClient(server)

	if _, err := client.TurnTicket(context.Background(), ClientSurfaceCLI, DefaultProfile); err != nil {
		t.Fatalf("TurnTicket: %v", err)
	}
	body := server.bodyFor("/api/perch-terminal/turn-ticket")
	if string(body) != `{"surface":"cli","profile":"standard"}` {
		t.Fatalf("turn-ticket body = %s, want the CLI key order", body)
	}
}

func TestParityModelCallCarriesBillingMultiplierKey(t *testing.T) {
	server := newParityLockServer(t)
	server.defaults(t)
	server.stream("/api/perch-terminal/model-call", parityDoneStream)
	client := lockClient(server)

	if _, err := client.CallModel(context.Background(), lockCall(t)); err != nil {
		t.Fatalf("CallModel: %v", err)
	}
	body := server.bodyFor("/api/perch-terminal/model-call")
	if !bytes.Contains(body, []byte(`"billingMultiplier":null`)) {
		t.Fatalf("attribution lacks the CLI billingMultiplier key: %s", body)
	}
	if !bytes.Contains(body, []byte(`"toolChoice":"auto"`)) || !bytes.Contains(body, []byte(`"temperature":0.6`)) {
		t.Fatalf("body does not carry the CLI request defaults: %s", body)
	}
}

func TestParitySequenceMatchesCLIOrder(t *testing.T) {
	server := newParityLockServer(t)
	server.defaults(t)
	server.stream("/api/perch-terminal/model-call", parityDoneStream)
	client := lockClient(server)

	if _, err := client.CallModel(context.Background(), ModelCallRequest{Lane: LaneChat}); err != nil {
		t.Fatalf("CallModel: %v", err)
	}
	want := []string{"/api/perch-terminal/public-model-default", "/api/perchai/account", "/api/perch-terminal/turn-ticket", "/api/perch-terminal/model-call"}
	got := server.paths()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sequence = %v, want %v", got, want)
	}
}

func TestParityPreferredModelComesFromPublicDefault(t *testing.T) {
	server := newParityLockServer(t)
	server.defaults(t)
	server.stream("/api/perch-terminal/model-call", parityDoneStream)
	client := lockClient(server)

	if _, err := client.CallModel(context.Background(), ModelCallRequest{RoostModelChoice: RoostStandardMax}); err != nil {
		t.Fatalf("CallModel: %v", err)
	}
	var body struct {
		PreferredModelID *string `json:"preferredModelId"`
		RoostModelChoice string  `json:"roostModelChoice"`
	}
	if err := json.Unmarshal(server.bodyFor("/api/perch-terminal/model-call"), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.PreferredModelID == nil || *body.PreferredModelID != "wandb-kimi-k2-6" {
		t.Fatalf("preferredModelId = %v, want the public default", body.PreferredModelID)
	}
	if body.RoostModelChoice != "" {
		t.Fatalf("roostModelChoice = %q, want it dropped when the public default is known", body.RoostModelChoice)
	}
}

func TestParityRetryBudgetStaysOnOneAccount(t *testing.T) {
	server := newParityLockServer(t)
	server.defaults(t)
	server.respond["/api/perch-terminal/model-call"] = func(w io.Writer, _ string) {
		_, _ = io.WriteString(w, "HTTP/1.1 503 Service Unavailable\r\ncontent-type: application/json\r\ncontent-length: 46\r\n\r\n{\"ok\":false,\"error\":\"down\",\"errorCode\":\"upstream_unavailable\"}")
	}
	client := lockClient(server)

	if _, err := client.CallModel(context.Background(), ModelCallRequest{Lane: LaneChat}); err == nil {
		t.Fatalf("CallModel must fail when the upstream keeps failing")
	}
	calls := 0
	for _, path := range server.paths() {
		if path == "/api/perch-terminal/model-call" {
			calls++
		}
	}
	if calls != 3 {
		t.Fatalf("model calls = %d, want the CLI budget of 3 attempts", calls)
	}
}

func TestParityGateSpacesRequestStarts(t *testing.T) {
	release := make(chan struct{})
	close(release)
	probe := &gateProbeDoer{release: release}
	doer := newGatedDoer(probe, newAccountGate(40*time.Millisecond, 2))

	for i := 0; i < 4; i++ {
		resp, err := doer.Do(&http.Request{URL: &url.URL{Scheme: "http", Host: "probe"}})
		if err != nil {
			t.Fatalf("Do %d: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	probe.mu.Lock()
	starts := append([]time.Time(nil), probe.starts...)
	probe.mu.Unlock()
	if len(starts) != 4 {
		t.Fatalf("wire requests = %d, want 4", len(starts))
	}
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(starts[i-1]); gap < 35*time.Millisecond {
			t.Fatalf("gap %d = %v, want at least the configured 40ms spacing", i, gap)
		}
	}
}

func TestParityTicketUnavailableRetryRemints(t *testing.T) {
	server := newParityLockServer(t)
	server.defaults(t)
	var calls int32
	server.respond["/api/perch-terminal/model-call"] = func(w io.Writer, _ string) {
		if atomic.AddInt32(&calls, 1) == 1 {
			body := `{"ok":false,"error":"ticket gone","errorCode":"turn_ticket_unavailable"}`
			_, _ = io.WriteString(w, "HTTP/1.1 502 Bad Gateway\r\ncontent-type: application/json\r\ncontent-length: "+strconv.Itoa(len(body))+"\r\n\r\n"+body)
			return
		}
		_, _ = io.WriteString(w, "HTTP/1.1 200 OK\r\ncontent-type: text/event-stream\r\ncontent-length: "+strconv.Itoa(len(parityDoneStream))+"\r\n\r\n"+parityDoneStream)
	}
	client := lockClient(server)

	if _, err := client.CallModel(context.Background(), lockCall(t)); err != nil {
		t.Fatalf("CallModel: %v", err)
	}
	server.mu.Lock()
	tickets := make([]string, 0, 2)
	for _, entry := range server.requests {
		if entry.path == "/api/perch-terminal/model-call" {
			tickets = append(tickets, entry.headers.Get("x-perch-turn-ticket"))
		}
	}
	server.mu.Unlock()
	if len(tickets) != 2 {
		t.Fatalf("model calls = %d, want 2", len(tickets))
	}
	if tickets[0] == tickets[1] {
		t.Fatalf("retry reused ticket %q after ticket_unavailable", tickets[0])
	}
}

type gateProbeDoer struct {
	mu       sync.Mutex
	starts   []time.Time
	inflight int
	peak     int
	release  chan struct{}
}

func (d *gateProbeDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	d.starts = append(d.starts, time.Now())
	d.inflight++
	if d.inflight > d.peak {
		d.peak = d.inflight
	}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.inflight--
		d.mu.Unlock()
	}()
	select {
	case <-d.release:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: http.Header{}}, nil
}

func TestParityGateSpacesQueuedCallers(t *testing.T) {
	probe := &gateProbeDoer{release: make(chan struct{})}
	doer := newGatedDoer(probe, newAccountGate(40*time.Millisecond, 2))

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := doer.Do(&http.Request{URL: &url.URL{Scheme: "http", Host: "probe"}})
			if err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		probe.mu.Lock()
		started := len(probe.starts)
		probe.mu.Unlock()
		if started >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d requests reached the wire while the cap held them", started)
		}
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	close(probe.release)
	wg.Wait()

	probe.mu.Lock()
	starts := append([]time.Time(nil), probe.starts...)
	probe.mu.Unlock()
	if len(starts) != 4 {
		t.Fatalf("wire requests = %d, want 4", len(starts))
	}
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(starts[i-1]); gap < 35*time.Millisecond {
			t.Fatalf("gap %d = %v, want the 40ms floor even for callers queued behind the cap", i, gap)
		}
	}
}

func TestParityGateCapsConcurrentExchanges(t *testing.T) {
	probe := &gateProbeDoer{release: make(chan struct{})}
	doer := newGatedDoer(probe, newAccountGate(0, DefaultMaxConcurrentCalls))

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := doer.Do(&http.Request{URL: &url.URL{Scheme: "http", Host: "probe"}})
			if err != nil {
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}()
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		probe.mu.Lock()
		settled := probe.inflight
		probe.mu.Unlock()
		if settled >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d requests were in flight", settled)
		}
		time.Sleep(2 * time.Millisecond)
	}
	close(probe.release)
	wg.Wait()

	probe.mu.Lock()
	peak := probe.peak
	probe.mu.Unlock()
	if peak > 2 {
		t.Fatalf("peak concurrency = %d, want at most 2, the CLI race2 width", peak)
	}
}
