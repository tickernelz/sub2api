package perch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// CredentialSource resolves the Perch session the handler should use for a request.
type CredentialSource interface {
	PerchCredentials(ctx context.Context) (Credentials, error)
}

type CredentialStore interface {
	StorePerchCredentials(ctx context.Context, creds Credentials) error
}

// CredentialSourceFunc adapts a function to CredentialSource.
type CredentialSourceFunc func(ctx context.Context) (Credentials, error)

func (f CredentialSourceFunc) PerchCredentials(ctx context.Context) (Credentials, error) {
	return f(ctx)
}

// Handler serves OpenAI-compatible chat completions and model lists on top of Perch.
type Handler struct {
	source      CredentialSource
	http        Doer
	now         func() time.Time
	turnContext bool

	mu      sync.Mutex
	clients map[string]*clientEntry
	seq     int64
}

type clientEntry struct {
	client *Client
	order  int64
	pinned int
}

const (
	maxCachedClients    = 64
	controlPlaneTimeout = 60 * time.Second
)

type HandlerOption func(*Handler)

func WithHandlerHTTP(doer Doer) HandlerOption {
	return func(h *Handler) {
		if doer != nil {
			h.http = doer
		}
	}
}

func WithTurnContext(enabled bool) HandlerOption {
	return func(h *Handler) {
		h.turnContext = enabled
	}
}

func WithHandlerClock(now func() time.Time) HandlerOption {
	return func(h *Handler) {
		if now != nil {
			h.now = now
		}
	}
}

func NewHandler(source CredentialSource, opts ...HandlerOption) *Handler {
	h := &Handler{
		source:      source,
		now:         time.Now,
		turnContext: true,
		clients:     make(map[string]*clientEntry),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/v1/models" && r.Method == http.MethodGet:
		h.serveModels(w, r)
	case path == "/v1/chat/completions" && r.Method == http.MethodPost:
		h.serveChatCompletions(w, r)
	default:
		writeError(w, &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "not found"})
	}
}

func (h *Handler) serveModels(w http.ResponseWriter, r *http.Request) {
	client, release, err := h.client(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	defer release()
	models, err := client.ModelIDs(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	created := h.now().Unix()
	list := make([]map[string]any, 0, len(models))
	for _, model := range models {
		list = append(list, map[string]any{
			"id":       model,
			"object":   "model",
			"created":  created,
			"owned_by": "perch",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": list})
}

func (h *Handler) serveChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeError(w, &APIError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "cannot read request body"})
		return
	}
	var req apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, &APIError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "request body is not valid JSON"})
		return
	}

	client, release, err := h.client(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	defer release()

	call, err := BuildModelCallRequest(&req, CallOptions{DisableTurnContext: !h.turnContext})
	if err != nil {
		writeError(w, &APIError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()})
		return
	}

	stream, err := client.OpenModelCall(r.Context(), call)
	if err != nil {
		writeError(w, err)
		return
	}
	defer func() { _ = stream.Close() }()

	responseID := "chatcmpl-" + strings.TrimPrefix(stream.Request.RunID, "tkt-cli-")
	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage

	if req.Stream {
		h.streamResponse(w, stream, req.Model, responseID, includeUsage)
		return
	}

	outcome, err := CollectOutcome(stream)
	if err != nil {
		writeError(w, err)
		return
	}
	model := req.Model
	if outcome.Model != "" {
		model = outcome.Model
	}
	if model != req.Model {
		w.Header().Set("x-perch-upstream-model", model)
	}
	writeJSON(w, http.StatusOK, ToChatResponse(outcome, model, responseID, h.now().Unix()))
}

func (h *Handler) streamResponse(w http.ResponseWriter, stream *ModelCallStream, model, responseID string, includeUsage bool) {
	flusher, _ := w.(http.Flusher)
	header := w.Header()
	header.Set("content-type", "text/event-stream")
	header.Set("cache-control", "no-cache")
	header.Set("connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	_, err := StreamChat(stream, model, responseID, h.now().Unix(), includeUsage, func(chunk apicompat.ChatCompletionsChunk) error {
		payload, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			apiErr = &APIError{Status: http.StatusBadGateway, Code: CodeAPIError, Message: err.Error()}
		}
		writeStreamError(w, apiErr)
		return
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}
func (h *Handler) client(ctx context.Context) (*Client, func(), error) {
	creds, err := h.source.PerchCredentials(ctx)
	if err != nil {
		return nil, nil, err
	}
	creds = creds.normalized()
	if err := creds.Validate(); err != nil {
		return nil, nil, err
	}

	key := firstNonEmpty(creds.AppURL, DefaultAppURL) + "|" + clientIdentityKey(creds)
	doer := h.http
	now := h.now

	h.mu.Lock()
	if entry, ok := h.clients[key]; ok {
		entry.pinned++
		current := entry.client.session.Credentials()
		if current.AccessToken == "" || creds.ExpiresAt > current.ExpiresAt {
			entry.client.session.SetCredentials(creds)
		}
		client := entry.client
		h.mu.Unlock()
		return client, func() { h.unpin(key) }, nil
	}

	opts := []SessionOption{WithClock(now)}
	if store, ok := h.source.(CredentialStore); ok {
		opts = append(opts, WithCredentialsPersist(func(next Credentials) {
			persistCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if storeErr := store.StorePerchCredentials(persistCtx, next); storeErr != nil {
				logger.LegacyPrintf("perch.handler", "persist refreshed credentials failed: type=%T", storeErr)
			}
		}))
	}
	controlHTTP, streamHTTP := doer, Doer(nil)
	if doer != nil {
		streamHTTP = streamingClient(doer)
	} else {
		dialer := FingerprintDialer(nil)
		controlHTTP = NewWireDoer(dialer.DialTLSContext, controlPlaneTimeout, responseHeaderTimeout)
		streamHTTP = NewWireDoer(dialer.DialTLSContext, 0, responseHeaderTimeout)
	}
	if controlHTTP != nil {
		opts = append(opts, WithHTTPClient(controlHTTP))
	}
	session := NewSession(creds, opts...)
	client := NewClient(session, WithClientClock(now))
	if controlHTTP != nil {
		client.http = controlHTTP
	}
	if streamHTTP != nil {
		client.streamHTTP = streamHTTP
	}

	h.seq++
	h.clients[key] = &clientEntry{client: client, order: h.seq, pinned: 1}
	h.evictLocked()
	h.mu.Unlock()
	return client, func() { h.unpin(key) }, nil
}

func (h *Handler) unpin(key string) {
	h.mu.Lock()
	if entry, ok := h.clients[key]; ok && entry.pinned > 0 {
		entry.pinned--
	}
	h.mu.Unlock()
}

func (h *Handler) evictLocked() {
	for len(h.clients) > maxCachedClients {
		var oldestKey string
		var oldestOrder int64
		for key, entry := range h.clients {
			if entry.pinned > 0 {
				continue
			}
			if oldestKey == "" || entry.order < oldestOrder {
				oldestKey = key
				oldestOrder = entry.order
			}
		}
		if oldestKey == "" {
			return
		}
		delete(h.clients, oldestKey)
	}
}

func clientIdentityKey(creds Credentials) string {
	if id := firstNonEmpty(creds.UserID, creds.Email); id != "" {
		return id
	}
	sum := sha256.Sum256([]byte(creds.AccessToken))
	return hex.EncodeToString(sum[:8])
}

func writeStreamError(w http.ResponseWriter, apiErr *APIError) {
	payload, err := json.Marshal(map[string]any{"error": errorEnvelope(apiErr)})
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", payload)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, err error) {
	apiErr := asAPIError(err)
	status := apiErr.HTTPStatus()
	if status == http.StatusTooManyRequests {
		if seconds, ok := retryAfterSeconds(apiErr, time.Now()); ok {
			w.Header().Set("retry-after", strconv.Itoa(seconds))
		}
	}
	writeJSON(w, status, map[string]any{"error": errorEnvelope(apiErr)})
}

func retryAfterSeconds(err *APIError, now time.Time) (int, bool) {
	if err.Limit.Empty() {
		return 0, false
	}
	var limit struct {
		ResetAt string `json:"resetAt"`
	}
	if json.Unmarshal(err.Limit.Raw, &limit) != nil || strings.TrimSpace(limit.ResetAt) == "" {
		return 0, false
	}
	reset, parseErr := time.Parse(time.RFC3339, limit.ResetAt)
	if parseErr != nil {
		return 0, false
	}
	seconds := int(reset.Sub(now).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return seconds, true
}

func asAPIError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return &APIError{Status: http.StatusBadGateway, Code: CodeAPIError, Message: err.Error()}
}

func errorEnvelope(err *APIError) map[string]any {
	envelope := map[string]any{
		"message": err.Message,
		"type":    err.OpenAIType(),
		"code":    err.Code,
	}
	if !err.Limit.Empty() {
		envelope["limit"] = err.Limit
	}
	return envelope
}
