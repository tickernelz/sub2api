package perch

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
)

func compressed(t *testing.T, encoding, payload string) []byte {
	t.Helper()
	switch encoding {
	case "":
		return []byte(payload)
	case "gzip":
		var buf bytes.Buffer
		writer := gzip.NewWriter(&buf)
		if _, err := writer.Write([]byte(payload)); err != nil {
			t.Fatalf("gzip write: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}
		return buf.Bytes()
	case "br":
		var buf bytes.Buffer
		writer := brotli.NewWriter(&buf)
		if _, err := writer.Write([]byte(payload)); err != nil {
			t.Fatalf("brotli write: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("brotli close: %v", err)
		}
		return buf.Bytes()
	}
	t.Fatalf("unknown encoding %q", encoding)
	return nil
}

func rawCompressed(encoding, contentType string, body []byte) string {
	headers := "HTTP/1.1 200 OK\r\ncontent-type: " + contentType + "\r\n"
	if encoding != "" {
		headers += "content-encoding: " + encoding + "\r\n"
	}
	return headers + fmt.Sprintf("content-length: %d\r\n", len(body)) + "\r\n" + string(body)
}

func wireClient(t *testing.T, server *rawServer) *Client {
	t.Helper()
	doer := NewWireDoer(dialDirect, 5*time.Second, 5*time.Second)
	session := NewSession(Credentials{
		AppURL:       "http://" + server.addr(),
		AccessToken:  "test-token",
		RefreshToken: "test-refresh",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}, WithHTTPClient(doer))
	return NewClient(session, WithClientHTTP(doer), WithClientHTTPStream(doer))
}

func TestWireDoerDecodesCompressedTicket(t *testing.T) {
	body := compressed(t, "gzip", `{"ticket":"test.ticket","runId":"tkt-cli-test","expiresAt":"2099-01-01T00:00:00Z"}`)
	server := newRawServer(t, func(_ int, request string) string {
		if strings.Contains(request, turnTicketPath) {
			return rawCompressed("gzip", "application/json", body)
		}
		return "HTTP/1.1 200 OK\r\ncontent-length: 2\r\n\r\nok"
	})
	client := wireClient(t, server)
	ticket, err := client.TurnTicket(context.Background(), ClientSurfaceCLI, DefaultProfile)
	if err != nil {
		t.Fatalf("turn ticket: %v", err)
	}
	if ticket.Ticket != "test.ticket" || ticket.RunID != "tkt-cli-test" {
		t.Fatalf("ticket = %+v", ticket)
	}
}

func TestCallModelReadsCompressedStream(t *testing.T) {
	stream := "data: {\"type\":\"answer_delta\",\"text\":\"pong\"}\n\n" +
		"data: {\"type\":\"done\",\"ok\":true,\"text\":\"pong\",\"model\":\"test/model\",\"usage\":{\"inputTokens\":1,\"outputTokens\":1,\"totalTokens\":2}}\n\n"
	for _, encoding := range []string{"", "gzip", "br"} {
		encoding := encoding
		t.Run("encoding="+encoding, func(t *testing.T) {
			payload := compressed(t, encoding, stream)
			server := newRawServer(t, func(_ int, request string) string {
				switch {
				case strings.Contains(request, turnTicketPath):
					return rawCompressed("", "application/json", []byte(`{"ticket":"test.ticket","runId":"tkt-cli-test","expiresAt":"2099-01-01T00:00:00Z"}`))
				case strings.Contains(request, modelCallPath):
					return rawCompressed(encoding, "text/event-stream", payload)
				default:
					return "HTTP/1.1 200 OK\r\ncontent-length: 2\r\n\r\nok"
				}
			})
			client := wireClient(t, server)
			request := testChatRequest()
			call, err := BuildModelCallRequest(request, CallOptions{})
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			outcome, err := client.CallModel(ctx, call)
			if err != nil {
				t.Fatalf("CallModel: %v", err)
			}
			if strings.TrimSpace(outcome.Text) != "pong" {
				t.Fatalf("text = %q", outcome.Text)
			}
		})
	}
}
