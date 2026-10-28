package perch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

// CredentialSource resolves the Perch session the handler should use for a request.
type CredentialSource interface {
	PerchCredentials(ctx context.Context) (Credentials, error)
}

// CredentialSourceFunc adapts a function to CredentialSource.
type CredentialSourceFunc func(ctx context.Context) (Credentials, error)

func (f CredentialSourceFunc) PerchCredentials(ctx context.Context) (Credentials, error) {
	return f(ctx)
}

// Handler serves OpenAI-compatible chat completions and model lists on top of Perch.
type Handler struct {
	source CredentialSource
	http   Doer
	now    func() time.Time

	mu      sync.Mutex
	clients map[string]*Client
}

type HandlerOption func(*Handler)

func WithHandlerHTTP(doer Doer) HandlerOption {
	return func(h *Handler) {
		if doer != nil {
			h.http = doer
		}
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
		source:  source,
		now:     time.Now,
		clients: make(map[string]*Client),
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
	client, err := h.client(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
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

	client, err := h.client(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	call, err := BuildModelCallRequest(&req, CallOptions{})
	if err != nil {
		writeError(w, &APIError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()})
		return
	}

	stream, err := client.OpenModelCall(r.Context(), call)
	if err != nil {
		writeError(w, err)
		return
	}
	defer stream.Close()

	responseID := "chatcmpl-" + strings.TrimPrefix(call.RunID, "tkt-cli-")
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
	if outcome.Model != "" && outcome.Model != req.Model {
		w.Header().Set("x-perch-upstream-model", outcome.Model)
	}
	writeJSON(w, http.StatusOK, ToChatResponse(outcome, req.Model, responseID, h.now().Unix()))
}

func (h *Handler) streamResponse(w http.ResponseWriter, stream *ModelCallStream, model, responseID string, includeUsage bool) {
	flusher, _ := w.(http.Flusher)
	header := w.Header()
	header.Set("content-type", "text/event-stream")
	header.Set("cache-control", "no-cache")
	header.Set("connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	emitter := NewChunkEmitter(model, responseID, h.now().Unix())
	writeChunk := func(chunk apicompat.ChatCompletionsChunk) {
		payload, err := json.Marshal(chunk)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		}
	}

	var settled *Outcome
	for {
		event, body, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return
		}
		if event.Type == EventDone {
			settled = outcomeFromDone(body)
			continue
		}
		if event.Type == EventError {
			apiErr := &APIError{Status: http.StatusBadGateway, Code: firstNonEmpty(body.ErrorCode, CodeAPIError), Message: firstNonEmpty(body.Message, "perch stream error")}
			if payload, err := json.Marshal(map[string]any{"error": errorEnvelope(apiErr)}); err == nil {
				fmt.Fprintf(w, "data: %s\n\n", payload)
			}
			return
		}
		for _, chunk := range emitter.Chunks(event, body) {
			writeChunk(chunk)
		}
	}

	if settled == nil {
		return
	}
	if !settled.OK {
		apiErr := &APIError{Status: ErrorStatus(firstNonEmpty(settled.ErrorCode, CodeAPIError)), Code: firstNonEmpty(settled.ErrorCode, CodeAPIError), Message: settled.Error, Limit: settled.Limit}
		if payload, err := json.Marshal(map[string]any{"error": errorEnvelope(apiErr)}); err == nil {
			fmt.Fprintf(w, "data: %s\n\n", payload)
		}
		return
	}
	for _, chunk := range emitter.Final(settled, includeUsage) {
		writeChunk(chunk)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func (h *Handler) client(ctx context.Context) (*Client, error) {
	creds, err := h.source.PerchCredentials(ctx)
	if err != nil {
		return nil, err
	}
	creds = creds.normalized()
	if err := creds.Validate(); err != nil {
		return nil, err
	}

	key := firstNonEmpty(creds.AppURL, DefaultAppURL) + "|" + firstNonEmpty(creds.UserID, creds.Email, creds.AccessToken)
	var doer Doer = h.http
	now := h.now

	h.mu.Lock()
	cached, ok := h.clients[key]
	h.mu.Unlock()
	if ok {
		cached.session.SetCredentials(creds)
		return cached, nil
	}

	opts := []SessionOption{WithClock(now)}
	if doer != nil {
		opts = append(opts, WithHTTPClient(doer))
	}
	session := NewSession(creds, opts...)
	client := NewClient(session, WithClientClock(now))
	if doer != nil {
		client.http = doer
	}

	h.mu.Lock()
	h.clients[key] = client
	h.mu.Unlock()
	return client, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, err error) {
	apiErr := asAPIError(err)
	writeJSON(w, apiErr.HTTPStatus(), map[string]any{"error": errorEnvelope(apiErr)})
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
