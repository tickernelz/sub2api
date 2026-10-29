package perch

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTokenServer(t *testing.T, tokenBody string) *rawServer {
	t.Helper()
	var addr string
	server := newRawServer(t, func(_ int, request string) string {
		body := tokenBody
		if strings.Contains(request, defaultAuthConfigPath) {
			body = `{"ok":true,"appUrl":"http://` + addr + `","supabaseUrl":"http://` + addr + `","supabaseAnonKey":"sb_publishable_test","providers":["google","github"],"redirectHost":"127.0.0.1"}`
		}
		return "HTTP/1.1 200 OK\r\ncontent-type: application/json\r\ncontent-length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
	})
	addr = server.addr()
	return server
}

func tokenRequestPart(t *testing.T, raw string, part string) string {
	t.Helper()
	idx := strings.Index(raw, CRLF+CRLF)
	if idx < 0 {
		t.Fatalf("request has no header terminator: %q", raw)
	}
	if part == "body" {
		return raw[idx+len(CRLF+CRLF):]
	}
	head := strings.Split(raw[:idx], CRLF)
	lines := make([]string, 0, len(head))
	for i, line := range head {
		if i < 2 || strings.HasPrefix(strings.ToLower(line), "content-length:") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, CRLF)
}

func assertTokenHeadMatchesCLI(t *testing.T, raw string) {
	t.Helper()
	got := tokenRequestPart(t, raw, "head")
	want := tokenRequestPart(t, readFixture(t, "auth-token.http"), "head")
	if got != want {
		t.Fatalf("token headers differ from the CLI recording\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRefreshTokenRequestMatchesCLITokenShape(t *testing.T) {
	server := newTokenServer(t, `{"access_token":"access-after-refresh","token_type":"bearer","expires_in":3600,"refresh_token":"refresh-after-rotate","user":{"id":"11111111-1111-4111-8111-111111111111","email":"sample@example.com"}}`)
	session := NewSession(
		Credentials{AppURL: "http://" + server.addr(), RefreshToken: "refresh-token"},
		WithHTTPClient(NewWireDoer(dialDirect, 5*time.Second, 5*time.Second)),
	)

	token, err := session.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if token != "access-after-refresh" {
		t.Fatalf("token = %q", token)
	}

	requests, _ := server.snapshot()
	captured := ""
	for _, request := range requests {
		if strings.HasPrefix(request, "POST /auth/v1/token") {
			captured = request
		}
	}
	if captured == "" {
		t.Fatalf("no token request on the wire: %q", requests)
	}
	if line := strings.SplitN(captured, CRLF, 2)[0]; line != "POST /auth/v1/token?grant_type=refresh_token HTTP/1.1" {
		t.Fatalf("request line = %q", line)
	}
	assertTokenHeadMatchesCLI(t, captured)
	if body := tokenRequestPart(t, captured, "body"); body != `{"refresh_token":"refresh-token"}` {
		t.Fatalf("body = %q", body)
	}
}

func TestPKCEExchangeKeepsTheCLITokenHeaders(t *testing.T) {
	server := newTokenServer(t, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`)
	request, err := NewExchangeRequest(context.Background(), LoginFlow{
		SupabaseURL:     "http://" + server.addr(),
		SupabaseAnonKey: "sb_publishable_test",
		CodeVerifier:    strings.Repeat("v", 112),
		RedirectURI:     "http://127.0.0.1:62173/perch/callback",
	}, "auth-code")
	if err != nil {
		t.Fatalf("exchange request: %v", err)
	}

	doer := NewWireDoer(dialDirect, 5*time.Second, 5*time.Second)
	response, err := doer.Do(request)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()

	requests, _ := server.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	assertTokenHeadMatchesCLI(t, requests[0])
	if !strings.HasPrefix(requests[0], "POST /auth/v1/token?grant_type=pkce HTTP/1.1") {
		t.Fatalf("request line = %q", strings.SplitN(requests[0], CRLF, 2)[0])
	}
}
