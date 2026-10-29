package perch

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestHandlerFingerprintKeepsControlPlaneDeadline(t *testing.T) {
	store := &recordingStore{creds: testCredentials("https://app.perchai.app")}
	handler := NewHandler(store)

	client, release, err := handler.client(context.Background())
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer release()

	control, ok := client.http.(*http.Client)
	if !ok {
		t.Fatalf("control client = %T", client.http)
	}
	if control.Timeout != controlPlaneTimeout {
		t.Fatalf("control timeout = %v, want %v", control.Timeout, controlPlaneTimeout)
	}
	stream, ok := client.streamHTTP.(*http.Client)
	if !ok {
		t.Fatalf("stream client = %T", client.streamHTTP)
	}
	if stream.Timeout != 0 {
		t.Fatalf("stream timeout = %v, want 0", stream.Timeout)
	}
}

func TestDiscardTicketKeepsOtherBudgets(t *testing.T) {
	client := NewClient(NewSession(testCredentials("https://app.example.com")))
	if !client.reserveTicketUse("ticket-a") || !client.reserveTicketUse("ticket-b") {
		t.Fatalf("reserve failed")
	}
	client.discardTicket(ClientSurfaceCLI, DefaultProfile, "ticket-a")

	client.mu.Lock()
	_, hasA := client.ticketUses["ticket-a"]
	usesB := client.ticketUses["ticket-b"]
	client.mu.Unlock()

	if hasA {
		t.Fatalf("discarded ticket kept its budget entry")
	}
	if usesB != 1 {
		t.Fatalf("ticket-b uses = %d, want 1", usesB)
	}
}

func TestTicketRotationMintsOnceForConcurrentWorkers(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(time.Hour).Unix()
	client := NewClient(NewSession(creds, WithHTTPClient(server.Client())))

	first, err := client.TurnTicket(context.Background(), ClientSurfaceCLI, DefaultProfile)
	if err != nil {
		t.Fatalf("prime ticket: %v", err)
	}
	for client.reserveTicketUse(first.Ticket) {
	}

	const workers = 4
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := client.nextTicket(context.Background(), first.Ticket)
			errs[index] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	if server.ticketHits > 2 {
		t.Fatalf("ticket mints = %d, want at most 2 (prime + one rotation)", server.ticketHits)
	}
}
