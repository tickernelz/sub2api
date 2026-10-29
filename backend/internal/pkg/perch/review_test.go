package perch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingStore struct {
	mu      sync.Mutex
	creds   Credentials
	stored  []Credentials
	refresh bool
}

func (s *recordingStore) PerchCredentials(ctx context.Context) (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refresh {
		expired := s.creds
		expired.ExpiresAt = time.Now().Add(10 * time.Second).Unix()
		return expired, nil
	}
	return s.creds, nil
}

func (s *recordingStore) StorePerchCredentials(ctx context.Context, creds Credentials) error {
	s.mu.Lock()
	s.stored = append(s.stored, creds)
	s.mu.Unlock()
	return nil
}

func (s *recordingStore) storedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.stored)
}

func TestHandlerRefreshesOnceAcrossRequests(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	store := &recordingStore{creds: testCredentials(server.URL), refresh: true}
	handler := NewHandler(store)

	for i := 0; i < 3; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(chatRequestBody(t, false))))
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d status = %d body = %s", i, recorder.Code, recorder.Body.String())
		}
	}

	if server.refreshHits != 1 {
		t.Fatalf("refresh hits = %d, want 1 across three requests", server.refreshHits)
	}
	if got := server.lastHeaders.Get("authorization"); got != "Bearer access-after-refresh" {
		t.Fatalf("last request authorization = %q, want the rotated token", got)
	}
	if store.storedCount() != 1 {
		t.Fatalf("persisted credentials = %d, want 1", store.storedCount())
	}
}

func TestModelCallClientDropsTotalTimeout(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(time.Hour).Unix()
	slow := server.Client()
	slow.Timeout = time.Minute

	session := NewSession(creds, WithHTTPClient(slow))
	client := NewClient(session)

	streaming, ok := client.streamHTTP.(*http.Client)
	if !ok {
		t.Fatalf("stream client = %T, want *http.Client", client.streamHTTP)
	}
	if streaming.Timeout != 0 {
		t.Fatalf("stream client timeout = %v, want 0", streaming.Timeout)
	}
}

func TestModelCallStreamFailsAfterIdle(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{StallAfterDelta: true})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(time.Hour).Unix()
	client := NewClient(NewSession(creds, WithHTTPClient(server.Client())), WithModelCallIdle(80*time.Millisecond))

	done := make(chan error, 1)
	go func() {
		_, err := client.CallModel(context.Background(), ModelCallRequest{})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("stalled stream must fail")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("stalled stream was not interrupted by the idle watchdog")
	}
	close(server.stall)
}

func TestHandlerStreamsErrorWhenUpstreamAborts(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{AbortAfterDelta: true})
	handler := newTestHandler(server)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(chatRequestBody(t, true))))
	handler.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if !strings.Contains(body, "stream_interrupted") {
		t.Fatalf("aborted stream must report stream_interrupted: %s", body)
	}
	if strings.Contains(body, "[DONE]") {
		t.Fatalf("aborted stream must not close with [DONE]: %s", body)
	}
}

func TestStreamResponseIDUsesRunID(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	handler := newTestHandler(server)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(chatRequestBody(t, true))))
	handler.ServeHTTP(recorder, request)

	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: {") {
			continue
		}
		var chunk struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err != nil {
			continue
		}
		if chunk.ID == "chatcmpl-" || !strings.HasPrefix(chunk.ID, "chatcmpl-") {
			t.Fatalf("chunk id = %q, want the run id suffix", chunk.ID)
		}
		return
	}
	t.Fatalf("stream carried no chunk: %s", recorder.Body.String())
}

func TestHandlerEndsStreamWhenServerRestartsAfterOutput(t *testing.T) {
	streamBody := []byte("data: {\"type\":\"answer_delta\",\"text\":\"partial\"}\n\n" +
		"data: {\"type\":\"stream_restart\",\"reason\":\"same_model_resend\"}\n\n" +
		"data: {\"type\":\"answer_delta\",\"text\":\"partial again\"}\n\n")
	server := newPerchTestServer(t, perchTestOptions{StreamBody: streamBody})
	handler := newTestHandler(server)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(chatRequestBody(t, true))))
	handler.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if !strings.Contains(body, "stream_interrupted") {
		t.Fatalf("restart after output must end as stream_interrupted: %s", body)
	}
	if strings.Count(body, "partial again") != 0 {
		t.Fatalf("resent content must not reach the client: %s", body)
	}
}

func TestSaveCredentialsFileReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	creds := testCredentials("https://app.example.com")
	creds.AccessToken = "access-first"
	if err := SaveCredentialsFile(path, creds); err != nil {
		t.Fatalf("SaveCredentialsFile: %v", err)
	}
	creds.AccessToken = "access-second"
	if err := SaveCredentialsFile(path, creds); err != nil {
		t.Fatalf("SaveCredentialsFile second: %v", err)
	}

	loaded, err := LoadCredentialsFile(path)
	if err != nil {
		t.Fatalf("LoadCredentialsFile: %v", err)
	}
	if loaded.AccessToken != "access-second" {
		t.Fatalf("access token = %q, want the second write", loaded.AccessToken)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat session file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("session file mode = %o, want 600", perm)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".perch-session-*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

func TestTicketRejectionRefreshesOnce(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{TicketAlwaysFails: true})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(time.Hour).Unix()
	client := NewClient(NewSession(creds, WithHTTPClient(server.Client())))

	if _, err := client.OpenModelCall(context.Background(), ModelCallRequest{}); err == nil {
		t.Fatalf("expected the ticket rejection to surface")
	}
	if server.refreshHits != 1 {
		t.Fatalf("refresh hits = %d, want 1", server.refreshHits)
	}
	if server.modelHits != 0 {
		t.Fatalf("model hits = %d, want 0", server.modelHits)
	}
}

func TestRefreshErrorCodeIsAuthError(t *testing.T) {
	body := []byte(`{"error_code":"refresh_token_already_used","msg":"already used"}`)
	server := newPerchTestServer(t, perchTestOptions{RefreshStatus: http.StatusBadRequest, RefreshBody: body})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(5 * time.Second).Unix()
	session := NewSession(creds, WithHTTPClient(server.Client()))

	_, err := session.AccessToken(context.Background())
	if err == nil {
		t.Fatalf("expected the refresh rejection to surface")
	}
	if !IsAuthError(err) {
		t.Fatalf("refresh error = %v, want an auth error", err)
	}
}
