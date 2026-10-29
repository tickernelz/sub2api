package perch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReserveTicketUseIsBounded(t *testing.T) {
	client := NewClient(NewSession(testCredentials("https://app.example.com")))
	for i := 0; i < ticketCallLimit; i++ {
		if !client.reserveTicketUse(Ticket{Ticket: "ticket-a"}) {
			t.Fatalf("reserve %d returned false before the limit", i)
		}
	}
	if client.reserveTicketUse(Ticket{Ticket: "ticket-a"}) {
		t.Fatalf("reserve past the limit must return false")
	}
	if !client.reserveTicketUse(Ticket{Ticket: "ticket-b"}) {
		t.Fatalf("a fresh ticket must start with a clean budget")
	}
}

func TestConcurrentModelCallsShareOneClient(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(time.Hour).Unix()
	client := NewClient(NewSession(creds, WithHTTPClient(server.Client())))

	const workers = 16
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			stream, err := client.OpenModelCall(context.Background(), ModelCallRequest{})
			if err != nil {
				errs[index] = err
				return
			}
			_ = stream.Close()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	if server.modelHits != workers {
		t.Fatalf("model hits = %d, want %d", server.modelHits, workers)
	}
	if server.refreshHits != 0 {
		t.Fatalf("refresh hits = %d, want 0", server.refreshHits)
	}
}

func TestConcurrentColdStartRefreshesOnce(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	store := &recordingStore{creds: testCredentials(server.URL), refresh: true}
	handler := NewHandler(store)

	const workers = 16
	var wg sync.WaitGroup
	codes := make([]int, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(chatRequestBody(t, false))))
			handler.ServeHTTP(recorder, request)
			codes[index] = recorder.Code
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("worker %d status = %d", i, code)
		}
	}
	if server.refreshHits != 1 {
		t.Fatalf("refresh hits = %d, want exactly 1 across concurrent cold start", server.refreshHits)
	}
}

func TestEvictionSkipsPinnedEntries(t *testing.T) {
	handler := NewHandler(nil)
	pinnedKey := "pinned"
	handler.mu.Lock()
	handler.seq++
	handler.clients[pinnedKey] = &clientEntry{order: handler.seq, pinned: 1}
	for i := 0; i < maxCachedClients+5; i++ {
		handler.seq++
		handler.clients["idle-"+string(rune('a'+i%26))+string(rune('a'+i/26))] = &clientEntry{order: handler.seq}
	}
	handler.evictLocked()
	size := len(handler.clients)
	_, kept := handler.clients[pinnedKey]
	handler.mu.Unlock()

	if !kept {
		t.Fatalf("pinned entry was evicted")
	}
	if size > maxCachedClients {
		t.Fatalf("cache size = %d, want at most %d", size, maxCachedClients)
	}
}
