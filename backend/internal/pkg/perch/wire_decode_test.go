package perch

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"
	"time"
)

func TestFingerprintProfileMatchesCLIHelloShape(t *testing.T) {
	profile := FingerprintProfile()

	sni := false
	for _, ext := range profile.Extensions {
		if ext == 0 {
			sni = true
		}
	}
	if !sni {
		t.Fatalf("profile omits the server_name extension: %v", profile.Extensions)
	}
	if len(profile.ALPNProtocols) != 1 || profile.ALPNProtocols[0] != "http/1.1" {
		t.Fatalf("alpn = %v, want only http/1.1", profile.ALPNProtocols)
	}
	if len(profile.CipherSuites) != 52 {
		t.Fatalf("ciphers = %d, want the CLI's 52", len(profile.CipherSuites))
	}
	if len(profile.SignatureAlgorithms) != 26 || profile.SignatureAlgorithms[0] != 2309 {
		t.Fatalf("sigalgs = %v, want the CLI's 26 starting at 2309", profile.SignatureAlgorithms)
	}
}

func TestWireDoerDecodesCompressedResponse(t *testing.T) {
	var payload bytes.Buffer
	writer := gzip.NewWriter(&payload)
	if _, err := writer.Write([]byte("pong")); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	body := payload.Bytes()

	server := newRawServer(t, func(int, string) string {
		return "HTTP/1.1 200 OK" + CRLF + "content-encoding: gzip" + CRLF +
			"content-length: " + itoa(len(body)) + CRLF + CRLF + string(body)
	})
	doer := NewWireDoer(dialDirect, 0, 5*time.Second)

	resp, err := doer.Do(newModelCallRequest(t, server.addr()))
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	decoded, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(decoded) != "pong" {
		t.Fatalf("decoded = %q, want pong", decoded)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
