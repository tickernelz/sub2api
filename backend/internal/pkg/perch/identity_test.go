package perch

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDefaultHeadersCarryCLIClientIdentity(t *testing.T) {
	headers := DefaultHeaders()
	want := map[string]string{
		"user-agent":         "perchai-cli/" + CLIVersion(),
		"x-perch-locale":     "en",
		"x-perch-call-delta": "1",
	}
	for key, value := range want {
		if headers[key] != value {
			t.Fatalf("header %s = %q, want %q", key, headers[key], value)
		}
	}
}

func TestVersionOverrideValidation(t *testing.T) {
	if got := resolveCLIVersion("2.5.0"); got != "2.5.0" {
		t.Fatalf("override = %q, want 2.5.0", got)
	}
	for _, bad := range []string{"", "2.5", "v2.5.0", "2.5.0-rc1", "2.5.0+build", "unknown"} {
		if IsSupportedCLIVersion(bad) {
			t.Fatalf("version %q must be rejected", bad)
		}
		if got := resolveCLIVersion(bad); got != CLICurrentVersion {
			t.Fatalf("override %q resolved to %q, want baseline", bad, got)
		}
	}
}

func TestModelCallSendsCLIIdentityAndKeepsTicketRequestClean(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	session := NewSession(testCredentials(server.URL), WithHTTPClient(server.Client()))
	client := NewClient(session)

	stream, err := client.OpenModelCall(context.Background(), ModelCallRequest{})
	if err != nil {
		t.Fatalf("OpenModelCall: %v", err)
	}
	defer func() { _ = stream.Close() }()

	headers := server.lastHeaders
	for key, value := range map[string]string{
		"user-agent":         "perchai-cli/" + CLIVersion(),
		"x-perch-locale":     "en",
		"x-perch-call-delta": "1",
	} {
		if got := headers.Get(key); got != value {
			t.Fatalf("model call header %s = %q, want %q", key, got, value)
		}
	}
	if got := server.lastTicketHeaders.Get("x-perch-call-delta"); got != "" {
		t.Fatalf("turn ticket request must not carry delta mode, got %q", got)
	}
}

func TestDecodeResponseBodyHandlesCompression(t *testing.T) {
	plain := []byte("data: keepalive\n\n")

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(plain); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	for _, encoding := range []string{"gzip", "identity"} {
		body := plain
		if encoding == "gzip" {
			body = compressed.Bytes()
		}
		resp := &http.Response{
			Header: http.Header{"Content-Encoding": []string{encoding}},
			Body:   io.NopCloser(bytes.NewReader(body)),
		}
		decoded, err := decodeResponseBody(resp)
		if err != nil {
			t.Fatalf("%s decode: %v", encoding, err)
		}
		raw, err := io.ReadAll(decoded)
		if err != nil {
			t.Fatalf("%s read: %v", encoding, err)
		}
		if string(raw) != string(plain) {
			t.Fatalf("%s body = %q, want %q", encoding, raw, plain)
		}
		if err := decoded.Close(); err != nil {
			t.Fatalf("%s close: %v", encoding, err)
		}
	}
}

func TestTurnContextIsPrependedBeforeFirstUserMessage(t *testing.T) {
	req := testChatRequest()
	call, err := BuildModelCallRequest(req, CallOptions{})
	if err != nil {
		t.Fatalf("BuildModelCallRequest: %v", err)
	}
	if got := messageRoles(t, call.Request.Messages); strings.Join(got, ",") != "system,user,user" {
		t.Fatalf("roles = %v", got)
	}
	if text := messageText(t, call.Request.Messages[1]); !strings.HasPrefix(text, "<perch_turn_context>") {
		t.Fatalf("turn context = %q", text)
	}
	if text := messageText(t, call.Request.Messages[2]); text != "Reply with pong." {
		t.Fatalf("user message = %q", text)
	}
}
