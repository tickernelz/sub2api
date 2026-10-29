package perch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestModelCallRunsCLIPreCallSequence(t *testing.T) {
	var (
		mu    sync.Mutex
		order []string
	)
	record := func(entry string) {
		mu.Lock()
		order = append(order, entry)
		mu.Unlock()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/perch-terminal/public-model-default":
			record("public-model-default")
			w.Header().Set("content-type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/perch-terminal/cli-context":
			body := make([]byte, 512)
			n, _ := r.Body.Read(body)
			payload := string(body[:n])
			switch {
			case strings.Contains(payload, `"includeIndex":true`):
				record("cli-context:index")
			case strings.Contains(payload, `"query":"Reply with pong."`):
				record("cli-context:prompt")
			default:
				record("cli-context:empty")
			}
			w.Header().Set("content-type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		case "/api/perch-terminal/turn-ticket":
			record("turn-ticket")
			w.Header().Set("content-type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"ticket":"tkt","ticketId":"tid","runId":"tkt-cli-test","profile":"standard","expiresAt":%q}`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		case "/api/perch-terminal/model-call":
			record("model-call")
			w.Header().Set("content-type", "text/event-stream")
			fmt.Fprint(w, "data: {\"type\":\"done\",\"ok\":true,\"text\":\"pong\"}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(NewSession(testCredentials(server.URL), WithHTTPClient(server.Client())))
	call, err := BuildModelCallRequest(testChatRequest(), CallOptions{DisableTurnContext: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	stream, err := client.OpenModelCall(context.Background(), call)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = CollectOutcome(stream)
	_ = stream.Close()

	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()

	want := []string{"public-model-default", "cli-context:empty", "turn-ticket", "cli-context:prompt", "cli-context:index", "model-call"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
