package perch

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTicketBudgetHoldsUnderConcurrentModelCalls(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(time.Hour).Unix()
	client := NewClient(NewSession(creds, WithHTTPClient(server.Client())))

	const workers = 16
	const perWorker = 10
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for call := 0; call < perWorker; call++ {
				stream, err := client.OpenModelCall(context.Background(), ModelCallRequest{})
				if err != nil {
					errs[index] = err
					return
				}
				_ = stream.Close()
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}

	total := workers * perWorker
	if got := server.modelHits; got != total {
		t.Fatalf("model hits = %d, want %d", got, total)
	}
	if max := server.maxTicketCalls(); max > ticketCallLimit {
		t.Fatalf("one ticket carried %d model calls, limit is %d", max, ticketCallLimit)
	}
	idealMints := (total + ticketCallLimit - 1) / ticketCallLimit
	if mints := server.ticketMints(); mints > idealMints+2 {
		t.Fatalf("ticket mints = %d, want at most %d", mints, idealMints+2)
	}
}
