package perch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	modelCallPath   = "/api/perch-terminal/model-call"
	turnTicketPath  = "/api/perch-terminal/turn-ticket"
	accountPath     = "/api/perchai/account"
	ticketRenewSkew = 30 * time.Second
	accountTTL      = 10 * time.Minute

	modelCallIdleTimeout  = 60 * time.Second
	responseHeaderTimeout = 60 * time.Second
	ticketCallLimit       = 40
	DefaultTicketTTL      = 5 * time.Minute
)

// Client speaks the authenticated Perch terminal API on behalf of one session.
type Client struct {
	session    *Session
	http       Doer
	streamHTTP Doer
	idle       time.Duration
	now        func() time.Time

	mu         sync.Mutex
	mintGate   chan struct{}
	rotateMu   sync.Mutex
	tickets    map[string]ticketEntry
	ticketUses map[string]ticketUse

	account   AccountInfo
	accountAt time.Time
	accountOK bool
}

type ticketEntry struct {
	ticket    Ticket
	fetchedAt time.Time
	expiresAt time.Time
}

type ticketUse struct {
	count     int
	expiresAt time.Time
}

type ClientOption func(*Client)

func WithClientHTTP(doer Doer) ClientOption {
	return func(c *Client) {
		if doer != nil {
			c.http = doer
			c.streamHTTP = streamingClient(doer)
		}
	}
}

func WithClientClock(now func() time.Time) ClientOption {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

func NewClient(session *Session, opts ...ClientOption) *Client {
	c := &Client{
		session:    session,
		http:       &http.Client{Timeout: 0},
		streamHTTP: &http.Client{Timeout: 0},
		idle:       modelCallIdleTimeout,
		now:        time.Now,
		tickets:    make(map[string]ticketEntry),
		ticketUses: make(map[string]ticketUse),
		mintGate:   make(chan struct{}, 1),
	}
	if session != nil {
		c.http = session.http
		c.streamHTTP = streamingClient(session.http)
		c.now = session.now
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func WithModelCallIdle(idle time.Duration) ClientOption {
	return func(c *Client) {
		if idle > 0 {
			c.idle = idle
		}
	}
}

func WithClientHTTPStream(doer Doer) ClientOption {
	return func(c *Client) {
		if doer != nil {
			c.streamHTTP = streamingClient(doer)
		}
	}
}

func streamingClient(doer Doer) Doer {
	client, ok := doer.(*http.Client)
	if !ok {
		return doer
	}
	clone := *client
	clone.Timeout = 0
	if transport, ok := client.Transport.(*http.Transport); ok {
		cloned := transport.Clone()
		if cloned.ResponseHeaderTimeout == 0 {
			cloned.ResponseHeaderTimeout = responseHeaderTimeout
		}
		clone.Transport = cloned
	}
	return &clone
}

// Session exposes the credential holder behind the client.
func (c *Client) Session() *Session {
	return c.session
}

// ModelCallStream is an open streaming model call.
type ModelCallStream struct {
	Response *http.Response
	Events   *SSEStream
	Request  ModelCallRequest
}

// Close releases the upstream body.
func (s *ModelCallStream) Close() error {
	if s == nil || s.Response == nil || s.Response.Body == nil {
		return nil
	}
	return s.Response.Body.Close()
}

// Next decodes the next event; the returned body is nil at end of stream.
func (s *ModelCallStream) Next() (Event, *eventBody, error) {
	for {
		payload, err := s.Events.Next()
		if err != nil {
			return Event{}, nil, err
		}
		event, body, err := decodeEvent(payload)
		if err != nil {
			continue
		}
		return event, body, nil
	}
}

// Account fetches the authenticated account projection with plan and entitlements.
func (c *Client) Account(ctx context.Context) (AccountInfo, error) {
	var payload struct {
		OK      bool `json:"ok"`
		Profile struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"profile"`
		Session struct {
			UserID       string        `json:"userId"`
			OrgID        string        `json:"orgId"`
			WorkspaceID  string        `json:"workspaceId"`
			PlanCode     string        `json:"planCode"`
			PlanName     string        `json:"planName"`
			Entitlements []Entitlement `json:"entitlements"`
		} `json:"session"`
	}
	if err := c.doJSON(ctx, http.MethodGet, accountPath, nil, &payload); err != nil {
		return AccountInfo{}, err
	}
	return AccountInfo{
		Email:        payload.Profile.Email,
		Name:         payload.Profile.Name,
		UserID:       payload.Session.UserID,
		WorkspaceID:  payload.Session.WorkspaceID,
		OrgID:        payload.Session.OrgID,
		PlanCode:     payload.Session.PlanCode,
		PlanName:     payload.Session.PlanName,
		Entitlements: payload.Session.Entitlements,
	}, nil
}

// TurnTicket returns a cached ticket for the profile, minting a new one when stale.
func (c *Client) lockMint(ctx context.Context) error {
	select {
	case c.mintGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) unlockMint() {
	<-c.mintGate
}

func (c *Client) TurnTicket(ctx context.Context, surface, profile string) (Ticket, error) {
	if strings.TrimSpace(profile) == "" {
		profile = DefaultProfile
	}
	key := surface + "|" + profile

	if err := c.lockMint(ctx); err != nil {
		return Ticket{}, err
	}
	defer c.unlockMint()

	c.mu.Lock()
	entry, ok := c.tickets[key]
	c.mu.Unlock()
	if ok && c.now().Add(ticketRenewSkew).Before(entry.expiresAt) {
		return entry.ticket, nil
	}

	body := map[string]string{"surface": surface, "profile": profile}
	var payload struct {
		Ticket    string `json:"ticket"`
		TicketID  string `json:"ticketId"`
		RunID     string `json:"runId"`
		Profile   string `json:"profile"`
		ExpiresAt string `json:"expiresAt"`
		Enforced  bool   `json:"enforced"`
	}
	if err := c.doJSON(ctx, http.MethodPost, turnTicketPath, body, &payload); err != nil {
		return Ticket{}, err
	}
	if strings.TrimSpace(payload.Ticket) == "" {
		return Ticket{}, &APIError{Status: http.StatusBadGateway, Code: CodeAPIError, Message: "turn ticket response carried no ticket"}
	}
	expires := c.now().Add(5 * time.Minute)
	if strings.TrimSpace(payload.ExpiresAt) != "" {
		if parsed, err := time.Parse(time.RFC3339, payload.ExpiresAt); err == nil {
			expires = parsed
		}
	}
	ticket := Ticket{
		Ticket:    payload.Ticket,
		TicketID:  payload.TicketID,
		RunID:     payload.RunID,
		Profile:   payload.Profile,
		Enforced:  payload.Enforced,
		ExpiresAt: expires.UTC().Format(time.RFC3339),
	}

	c.mu.Lock()
	c.tickets[key] = ticketEntry{ticket: ticket, fetchedAt: c.now(), expiresAt: expires}
	c.mu.Unlock()
	return ticket, nil
}

func (c *Client) reserveTicketUse(ticket Ticket) bool {
	if strings.TrimSpace(ticket.Ticket) == "" {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.pruneTicketUsesLocked(now)
	use := c.ticketUses[ticket.Ticket]
	if use.count >= ticketCallLimit {
		return false
	}
	use.count++
	use.expiresAt = ticketExpiry(ticket, now)
	c.ticketUses[ticket.Ticket] = use
	return true
}

func (c *Client) pruneTicketUsesLocked(now time.Time) {
	for token, use := range c.ticketUses {
		if !use.expiresAt.IsZero() && now.After(use.expiresAt) {
			delete(c.ticketUses, token)
		}
	}
}

func ticketExpiry(ticket Ticket, now time.Time) time.Time {
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(ticket.ExpiresAt)); err == nil {
		return parsed
	}
	return now.Add(DefaultTicketTTL)
}

func (c *Client) discardTicket(surface, profile string) {
	if strings.TrimSpace(profile) == "" {
		profile = DefaultProfile
	}
	c.mu.Lock()
	delete(c.tickets, surface+"|"+profile)
	c.mu.Unlock()
}

func (c *Client) nextTicket(ctx context.Context, exhausted Ticket) (Ticket, error) {
	c.rotateMu.Lock()
	defer c.rotateMu.Unlock()

	cached, err := c.TurnTicket(ctx, ClientSurfaceCLI, DefaultProfile)
	if err == nil && cached.Ticket != exhausted.Ticket && c.reserveTicketUse(cached) {
		return cached, nil
	}
	c.discardTicket(ClientSurfaceCLI, DefaultProfile)
	fresh, err := c.TurnTicket(ctx, ClientSurfaceCLI, DefaultProfile)
	if err != nil {
		return Ticket{}, err
	}
	if !c.reserveTicketUse(fresh) {
		return Ticket{}, &APIError{Status: http.StatusBadGateway, Code: CodeTicketUnavailable, Message: "perch: turn ticket budget exhausted"}
	}
	return fresh, nil
}

// OpenModelCall starts a streaming model call and returns the open SSE stream.
func (c *Client) OpenModelCall(ctx context.Context, req ModelCallRequest) (*ModelCallStream, error) {
	if strings.TrimSpace(req.Lane) == "" {
		req.Lane = DefaultLane
	}
	if strings.TrimSpace(req.ClientSurface) == "" {
		req.ClientSurface = ClientSurfaceCLI
	}
	if req.AvoidModelIDs == nil {
		req.AvoidModelIDs = []string{}
	}

	resp, err := c.postModelCall(ctx, &req)
	if err != nil {
		return nil, err
	}
	return &ModelCallStream{Response: resp, Events: NewSSEStream(resp.Body), Request: req}, nil
}

func (c *Client) postModelCall(ctx context.Context, req *ModelCallRequest) (*http.Response, error) {
	endpoint, err := joinURL(c.session.AppURL(), modelCallPath)
	if err != nil {
		return nil, err
	}

	attempt := 0
	for {
		attempt++
		ticket, err := c.TurnTicket(ctx, ClientSurfaceCLI, DefaultProfile)
		if err != nil {
			if !IsAuthError(err) || attempt >= 2 {
				return nil, err
			}
			if _, refreshErr := c.session.Refresh(ctx); refreshErr != nil {
				return nil, refreshErr
			}
			c.discardTicket(ClientSurfaceCLI, DefaultProfile)
			continue
		}
		if !c.reserveTicketUse(ticket) {
			ticket, err = c.nextTicket(ctx, ticket)
			if err != nil {
				return nil, err
			}
		}

		if strings.TrimSpace(req.RunID) == "" {
			req.RunID = firstNonEmpty(ticket.RunID, NewRunID(c.now()))
		}
		if req.Attribution == nil {
			req.Attribution = c.attribution(ctx, req.RunID, req.Lane)
		}
		if req.Attribution != nil && strings.TrimSpace(req.Attribution.RunID) == "" {
			req.Attribution.RunID = req.RunID
		}
		payload, err := json.Marshal(*req)
		if err != nil {
			return nil, fmt.Errorf("perch: encode model call: %w", err)
		}
		token, err := c.session.AccessToken(ctx)
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("perch: build model call: %w", err)
		}
		httpReq.Header.Set("content-type", "application/json")
		httpReq.Header.Set("accept", "text/event-stream")
		httpReq.Header.Set("authorization", "Bearer "+token)
		applyIdentityHeaders(httpReq, true)
		httpReq.Header.Set("x-perch-turn-ticket", ticket.Ticket)

		resp, err := c.streamHTTP.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("perch: model call: %w", err)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			decoded, err := decodeResponseBody(resp)
			if err != nil {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("perch: decode model call body: %w", err)
			}
			resp.Body = newIdleBody(decoded, c.idle)
			return resp, nil
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("perch: read model call error: %w", readErr)
		}
		apiErr := NewAPIError(resp.StatusCode, body)
		if attempt >= 2 || !retryableModelCall(apiErr) {
			return nil, apiErr
		}
		if IsAuthError(apiErr) {
			if _, err := c.session.Refresh(ctx); err != nil {
				return nil, err
			}
			c.discardTicket(ClientSurfaceCLI, DefaultProfile)
			continue
		}
		c.discardTicket(ClientSurfaceCLI, DefaultProfile)
	}
}

func retryableModelCall(err *APIError) bool {
	if err == nil {
		return false
	}
	switch err.Code {
	case CodeSurfaceRequired, CodeContextOverflow, CodeUsageLimitReached, CodeTurnRateLimited, CodeModelBlocked, CodePlanRequired:
		return false
	}
	if IsAuthError(err) {
		return true
	}
	return err.Code == CodeTicketUnavailable
}

// CallModel runs a model call to completion and returns the settled outcome.
func (c *Client) CallModel(ctx context.Context, req ModelCallRequest) (*Outcome, error) {
	stream, err := c.OpenModelCall(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	return CollectOutcome(stream)
}

// CollectOutcome drains a stream to its done event.
func CollectOutcome(stream *ModelCallStream) (*Outcome, error) {
	var text strings.Builder
	var reasoning strings.Builder
	toolCalls := make([]ToolCall, 0, 4)
	var outcome *Outcome
	for {
		event, body, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("perch: read stream: %w", err)
		}
		switch event.Type {
		case EventAnswerDelta:
			_, _ = text.WriteString(body.Text)
		case EventReasoningDelta:
			_, _ = reasoning.WriteString(body.Text)
		case EventToolCallDelta, EventToolUseEnd:
			toolCalls = mergeToolCalls(toolCalls, body.ToolCalls)
		case EventStreamRestart:
			text.Reset()
			reasoning.Reset()
			toolCalls = toolCalls[:0]
			outcome = nil
		case EventDone:
			outcome = outcomeFromDone(body)
		case EventError:
			code := strings.TrimSpace(body.ErrorCode)
			if code == "" {
				code = CodeAPIError
			}
			return nil, &APIError{Status: ErrorStatus(code), Code: code, Message: strings.TrimSpace(body.Message)}
		}
	}
	if outcome == nil {
		return nil, &APIError{Status: http.StatusBadGateway, Code: CodeStreamInterrupted, Message: "perch stream ended without a done event"}
	}
	if !outcome.OK {
		code := outcome.ErrorCode
		if code == "" {
			code = CodeAPIError
		}
		return outcome, &APIError{Status: ErrorStatus(code), Code: code, Message: outcome.Error, Limit: outcome.Limit}
	}
	if outcome.Text == "" && text.Len() > 0 {
		outcome.Text = text.String()
	}
	if len(outcome.ToolCalls) == 0 && len(toolCalls) > 0 {
		outcome.ToolCalls = toolCalls
	}
	if outcome.Reasoning == "" && reasoning.Len() > 0 {
		outcome.Reasoning = reasoning.String()
	}
	return outcome, nil
}

func outcomeFromDone(body *eventBody) *Outcome {
	ok := true
	if body.Ok != nil {
		ok = *body.Ok
	}
	return &Outcome{
		OK:           ok,
		Text:         body.Text,
		Content:      body.Content,
		ToolCalls:    mergeToolCalls(nil, body.ToolCalls),
		FinishReason: body.FinishReason,
		Provider:     body.Provider,
		Model:        body.Model,
		DurationMs:   body.DurationMs,
		ContextFill:  body.ContextFill,
		ContextInput: body.ContextInput,
		Usage:        body.Usage,
		Error:        body.Error,
		ErrorCode:    body.ErrorCode,
		Limit:        body.Limit,
	}
}

func mergeToolCalls(existing []ToolCall, incoming []ToolCall) []ToolCall {
	for _, call := range incoming {
		replaced := false
		for i := range existing {
			if existing[i].ID == call.ID && call.ID != "" {
				existing[i] = call
				replaced = true
				break
			}
		}
		if !replaced {
			existing = append(existing, call)
		}
	}
	return existing
}

func (c *Client) attribution(ctx context.Context, runID, lane string) *Attribution {
	info, ok := c.cachedAccount(ctx)
	if !ok {
		return nil
	}
	if strings.TrimSpace(info.UserID) == "" && strings.TrimSpace(info.WorkspaceID) == "" {
		return nil
	}
	return &Attribution{
		UserID:    info.UserID,
		Workspace: info.WorkspaceID,
		RunID:     runID,
		Lane:      lane,
		Source:    ClientSurfaceCLI,
	}
}

func (c *Client) cachedAccount(ctx context.Context) (AccountInfo, bool) {
	c.mu.Lock()
	info, fetchedAt := c.account, c.accountAt
	ok := c.accountOK
	c.mu.Unlock()
	if ok && c.now().Sub(fetchedAt) < accountTTL {
		return info, true
	}
	fresh, err := c.Account(ctx)
	if err != nil {
		return AccountInfo{}, false
	}
	c.mu.Lock()
	c.account, c.accountAt, c.accountOK = fresh, c.now(), true
	c.mu.Unlock()
	return fresh, true
}

// NewRunID mints a Perch-shaped run id.
func NewRunID(now time.Time) string {
	return fmt.Sprintf("tkt-cli-%d-%d", now.UnixMilli(), now.UnixNano()%1_000_000)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	endpoint, err := joinURL(c.session.AppURL(), path)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("perch: encode %s: %w", path, err)
		}
		reader = bytes.NewReader(payload)
	}
	token, err := c.session.AccessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("perch: build %s: %w", path, err)
	}
	req.Header.Set("accept", "application/json")
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	req.Header.Set("authorization", "Bearer "+token)
	applyIdentityHeaders(req, false)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("perch: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("perch: read %s: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return NewAPIError(resp.StatusCode, payload)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("perch: decode %s: %w", path, err)
	}
	return nil
}

// ErrorStatus maps a Perch error code to the client-facing status.
func ErrorStatus(code string) int {
	switch code {
	case CodeNotAuthenticated:
		return http.StatusUnauthorized
	case CodeTurnRateLimited, CodeUsageLimitReached:
		return http.StatusTooManyRequests
	case CodePlanRequired:
		return http.StatusPaymentRequired
	case CodeModelBlocked, CodeSurfaceRequired:
		return http.StatusForbidden
	case CodeContextOverflow, CodeProviderNotConfigured:
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}
