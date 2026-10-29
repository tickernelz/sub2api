package perch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func tlsReuseServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-length", "2")
		_, _ = w.Write([]byte("ok"))
	}))
	var conns int32
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			atomic.AddInt32(&conns, 1)
		}
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, &conns
}

func tlsDialerFor(t *testing.T, server *httptest.Server) DialFunc {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	config := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialer := &tls.Dialer{Config: config}
		return dialer.DialContext(ctx, network, addr)
	}
}

func TestWireDoerReusesTLSConnectionAcrossRequests(t *testing.T) {
	server, conns := tlsReuseServer(t)
	doer := NewWireDoer(tlsDialerFor(t, server), 0, 5*time.Second)

	for i := 0; i < 5; i++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+modelCallPath, nil)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp, err := doer.Do(req)
		if err != nil {
			t.Fatalf("do %d: %v", i, err)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if string(body) != "ok" {
			t.Fatalf("body %d = %q", i, body)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("close %d: %v", i, err)
		}
	}

	if got := atomic.LoadInt32(conns); got != 1 {
		t.Fatalf("server connections = %d, want 1 across five requests", got)
	}
}
