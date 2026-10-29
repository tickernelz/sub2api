package perch

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClientModelCallEmitsRecordedHeaderOrder(t *testing.T) {
	server := newRawServer(t, func(_ int, request string) string {
		switch {
		case strings.Contains(request, turnTicketPath):
			return "HTTP/1.1 200 OK\r\ncontent-type: application/json\r\ncontent-length: 82\r\n\r\n" + `{"ticket":"test.ticket","runId":"tkt-cli-test","expiresAt":"2099-01-01T00:00:00Z"}`
		case strings.Contains(request, modelCallPath):
			return "HTTP/1.1 200 OK" + CRLF + "content-type: text/event-stream" + CRLF + "transfer-encoding: chunked" + CRLF + "connection: close" + CRLF + "" + CRLF + "0" + CRLF + "" + CRLF + ""
		default:
			return "HTTP/1.1 200 OK" + CRLF + "content-length: 2" + CRLF + "" + CRLF + "ok"
		}
	})

	doer := NewWireDoer(dialDirect, 5*time.Second, 5*time.Second)
	session := NewSession(Credentials{
		AppURL:       "http://" + server.addr(),
		AccessToken:  "test-token",
		RefreshToken: "test-refresh",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}, WithHTTPClient(doer))
	client := NewClient(session, WithClientHTTP(doer), WithClientHTTPStream(doer))

	stream, err := client.OpenModelCall(context.Background(), ModelCallRequest{Request: ChatRequest{Messages: []json.RawMessage{json.RawMessage(`{"role":"user"}`)}}})
	if err != nil {
		t.Fatalf("open model call: %v", err)
	}
	_ = stream.Close()

	requests, _ := server.snapshot()
	var modelCall string
	for _, request := range requests {
		if strings.HasPrefix(request, "POST /api/perch-terminal/model-call") {
			modelCall = request
		}
	}
	if modelCall == "" {
		t.Fatalf("no model call captured")
	}
	if got, want := headerNames(modelCall), headerNames(readFixture(t, "model-call.http")); !equalSlices(got, want) {
		t.Fatalf("header order = %v, want %v", got, want)
	}
}

func headerNames(request string) []string {
	block := request
	if index := strings.Index(block, "\r\n\r\n"); index >= 0 {
		block = block[:index]
	}
	lines := strings.Split(block, "\r\n")
	names := make([]string, 0, len(lines))
	for _, line := range lines[1:] {
		if index := strings.Index(line, ":"); index > 0 {
			names = append(names, line[:index])
		}
	}
	return names
}

func equalSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
