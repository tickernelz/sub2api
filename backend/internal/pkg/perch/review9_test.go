package perch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTurnTicketWaitIsCancellableByTheWaiter(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"ticket":"t1","ticketId":"id1","runId":"r1","profile":"standard","expiresAt":"2099-01-01T00:00:00Z"}`))
	}))
	t.Cleanup(server.Close)
	client := NewClient(NewSession(testCredentials(server.URL), WithHTTPClient(server.Client())))

	minted := make(chan error, 1)
	go func() {
		_, err := client.TurnTicket(context.Background(), "cli", "standard")
		minted <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for len(client.mintGate) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the first mint never took the gate")
		}
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := client.TurnTicket(ctx, "cli", "standard")
	elapsed := time.Since(started)

	close(release)
	<-minted

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the waiter's own deadline", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("waiter returned after %s, want under 500ms", elapsed)
	}
}
