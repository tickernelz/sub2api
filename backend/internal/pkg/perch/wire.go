package perch

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

type wireHeaderName struct {
	key  string
	name string
}

type wireDefaultHeader struct {
	name  string
	value string
}

var modelCallWireHeaders = []wireHeaderName{
	{"content-type", "Content-Type"},
	{"x-perch-call-delta", "x-perch-call-delta"},
	{"accept", "Accept"},
	{"authorization", "Authorization"},
	{"user-agent", "User-Agent"},
	{"x-perch-locale", "x-perch-locale"},
	{"x-perch-turn-ticket", "x-perch-turn-ticket"},
}

var supabaseTokenWireHeaders = []wireHeaderName{
	{"content-type", "Content-Type"},
	{"authorization", "Authorization"},
	{"apikey", "apikey"},
	{"x-client-info", "X-Client-Info"},
	{"x-supabase-api-version", "X-Supabase-Api-Version"},
	{"accept", "accept"},
	{"accept-language", "accept-language"},
	{"sec-fetch-mode", "sec-fetch-mode"},
	{"user-agent", "user-agent"},
	{"accept-encoding", "accept-encoding"},
}

var jsonWireHeaders = []wireHeaderName{
	{"content-type", "Content-Type"},
	{"authorization", "Authorization"},
	{"user-agent", "User-Agent"},
	{"x-perch-locale", "x-perch-locale"},
}

var wireDefaultHeaders = []wireDefaultHeader{
	{"accept", "*/*"},
	{"accept-language", "*"},
	{"sec-fetch-mode", "cors"},
	{"accept-encoding", "br, gzip, deflate"},
}

// WireDoer speaks HTTP/1.1 with the header order the official client emits.
type WireDoer struct {
	dial    DialFunc
	plain   DialFunc
	control time.Duration
	header  time.Duration

	mu   sync.Mutex
	idle map[string]net.Conn
}

func NewWireDoer(dial DialFunc, controlTimeout, headerTimeout time.Duration) *WireDoer {
	if headerTimeout <= 0 {
		headerTimeout = responseHeaderTimeout
	}
	return &WireDoer{
		dial:    dial,
		plain:   (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		control: controlTimeout,
		header:  headerTimeout,
		idle:    make(map[string]net.Conn),
	}
}

func (w *WireDoer) Do(req *http.Request) (*http.Response, error) {
	body, err := drainRequestBody(req)
	if err != nil {
		return nil, err
	}
	addr := wireAuthority(req.URL)
	conn, err := w.connection(req.Context(), addr, w.dialFunc(req.URL))
	if err != nil {
		return nil, fmt.Errorf("perch: wire dial %s: %w", addr, err)
	}
	return w.exchange(req, conn, addr, body)
}

func drainRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	defer func() { _ = req.Body.Close() }()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("perch: wire read request body: %w", err)
	}
	return body, nil
}

func wireAuthority(u *url.URL) string {
	if u.Host == "" {
		return u.Path
	}
	if strings.Contains(u.Host, ":") {
		return u.Host
	}
	if u.Scheme == "http" {
		return u.Host + ":80"
	}
	return u.Host + ":443"
}

func (w *WireDoer) dialFunc(u *url.URL) DialFunc {
	if u.Scheme == "http" {
		return w.plain
	}
	return w.dial
}

func (w *WireDoer) connection(ctx context.Context, addr string, dial DialFunc) (net.Conn, error) {
	w.mu.Lock()
	idle := w.idle[addr]
	delete(w.idle, addr)
	w.mu.Unlock()

	if idle != nil {
		if wireConnAlive(idle) {
			return idle, nil
		}
		_ = idle.Close()
	}
	return dial(ctx, "tcp", addr)
}

func wireConnAlive(conn net.Conn) bool {
	return wireConnIdle(conn)
}

func (w *WireDoer) ControlTimeout() time.Duration { return w.control }

func (w *WireDoer) release(addr string, conn net.Conn) {
	w.mu.Lock()
	if existing, ok := w.idle[addr]; !ok || existing == nil {
		w.idle[addr] = conn
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()
	_ = conn.Close()
}

func (w *WireDoer) exchange(req *http.Request, conn net.Conn, addr string, body []byte) (*http.Response, error) {
	payload, err := buildWireRequest(req, body)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	deadline := time.Now().Add(w.header)
	if w.control > 0 {
		deadline = time.Now().Add(w.control)
	}
	if ctxDeadline, ok := req.Context().Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("perch: wire deadline: %w", err)
	}
	if _, err := conn.Write(payload); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("perch: wire write: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("perch: wire read response: %w", err)
	}
	decoded, decodeErr := decodeResponseBody(resp)
	if decodeErr != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("perch: wire decode response: %w", decodeErr)
	}
	if decoded != resp.Body {
		resp.Body = decoded
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1
		resp.Uncompressed = true
	}
	if w.control == 0 {
		if err := conn.SetDeadline(time.Time{}); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("perch: wire clear deadline: %w", err)
		}
	}
	resp.Body = &wireBody{body: resp.Body, reader: reader, doer: w, conn: conn, addr: addr, reusable: !resp.Close}
	return resp, nil
}

type wireBody struct {
	body     io.ReadCloser
	reader   *bufio.Reader
	doer     *WireDoer
	conn     net.Conn
	addr     string
	reusable bool
	settle   sync.Once
}

func (b *wireBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if err != nil {
		b.finish(err == io.EOF)
	}
	return n, err
}

func (b *wireBody) Close() error {
	b.finish(false)
	return nil
}

func (b *wireBody) finish(complete bool) {
	b.settle.Do(func() {
		if complete && b.reusable && b.reader.Buffered() == 0 {
			b.doer.release(b.addr, b.conn)
			return
		}
		_ = b.conn.Close()
	})
}

func buildWireRequest(req *http.Request, body []byte) ([]byte, error) {
	var buf bytes.Buffer
	target := req.URL.RequestURI()
	if target == "" {
		target = "/"
	}
	if err := checkWireRequestLine(req.Method, target, req.URL.Host); err != nil {
		return nil, err
	}
	_, _ = buf.WriteString(req.Method)
	_, _ = buf.WriteString(" ")
	_, _ = buf.WriteString(target)
	_, _ = buf.WriteString(" HTTP/1.1\r\n")
	_, _ = buf.WriteString("host: ")
	_, _ = buf.WriteString(req.URL.Host)
	_, _ = buf.WriteString("\r\n")
	_, _ = buf.WriteString("connection: keep-alive\r\n")

	written := make(map[string]bool, len(req.Header)+len(wireDefaultHeaders))
	for _, entry := range wireOrderFor(req) {
		values := req.Header.Values(entry.key)
		if len(values) == 0 {
			continue
		}
		written[entry.key] = true
		for _, value := range values {
			if err := checkWireHeader(entry.name, value); err != nil {
				return nil, err
			}
			_, _ = buf.WriteString(entry.name)
			_, _ = buf.WriteString(": ")
			_, _ = buf.WriteString(value)
			_, _ = buf.WriteString("\r\n")
		}
	}

	rest := make([]string, 0, len(req.Header))
	for key := range req.Header {
		if written[strings.ToLower(key)] {
			continue
		}
		rest = append(rest, key)
	}
	sort.Strings(rest)
	for _, key := range rest {
		lower := strings.ToLower(key)
		if written[lower] {
			continue
		}
		written[lower] = true
		for _, value := range req.Header.Values(key) {
			if err := checkWireHeader(key, value); err != nil {
				return nil, err
			}
			_, _ = buf.WriteString(key)
			_, _ = buf.WriteString(": ")
			_, _ = buf.WriteString(value)
			_, _ = buf.WriteString("\r\n")
		}
	}

	for _, entry := range wireDefaultHeaders {
		if written[entry.name] {
			continue
		}
		written[entry.name] = true
		_, _ = buf.WriteString(entry.name)
		_, _ = buf.WriteString(": ")
		_, _ = buf.WriteString(entry.value)
		_, _ = buf.WriteString("\r\n")
	}

	if len(body) > 0 {
		_, _ = buf.WriteString("content-length: ")
		_, _ = buf.WriteString(strconv.Itoa(len(body)))
		_, _ = buf.WriteString("\r\n")
	}
	_, _ = buf.WriteString("\r\n")
	_, _ = buf.Write(body)
	return buf.Bytes(), nil
}

var (
	errWireRequestLine = errors.New("perch: wire request line carries a forbidden byte")
	errWireHeader      = errors.New("perch: wire header carries a forbidden byte")
)

func checkWireRequestLine(method, target, host string) error {
	if method == "" || !wireToken(method) {
		return fmt.Errorf("%w: method", errWireRequestLine)
	}
	if wireForbidden(target) || strings.ContainsAny(target, " \t") {
		return fmt.Errorf("%w: target", errWireRequestLine)
	}
	if wireForbidden(host) || strings.ContainsAny(host, " \t") {
		return fmt.Errorf("%w: host", errWireRequestLine)
	}
	return nil
}

func checkWireHeader(name, value string) error {
	if name == "" || !wireToken(name) {
		return fmt.Errorf("%w: name %q", errWireHeader, name)
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '\t' {
			continue
		}
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("%w: value for %q", errWireHeader, name)
		}
	}
	return nil
}

func wireToken(s string) bool {
	for i := 0; i < len(s); i++ {
		if !wireTokenByte(s[i]) {
			return false
		}
	}
	return len(s) > 0
}

func wireTokenByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}

func wireForbidden(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c == 0x7f {
			return true
		}
	}
	return false
}

func wireOrderFor(req *http.Request) []wireHeaderName {
	path := strings.TrimSuffix(req.URL.Path, "/")
	if strings.HasSuffix(path, modelCallPath) {
		return modelCallWireHeaders
	}
	if strings.HasSuffix(path, supabaseTokenPath) {
		return supabaseTokenWireHeaders
	}
	return jsonWireHeaders
}
