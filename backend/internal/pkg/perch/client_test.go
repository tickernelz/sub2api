package perch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestTurnTicketIsReusedUntilExpiry(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	session := NewSession(testCredentials(server.URL), WithHTTPClient(server.Client()))
	client := NewClient(session)

	first, err := client.TurnTicket(context.Background(), ClientSurfaceCLI, DefaultProfile)
	if err != nil {
		t.Fatalf("TurnTicket: %v", err)
	}
	second, err := client.TurnTicket(context.Background(), ClientSurfaceCLI, DefaultProfile)
	if err != nil {
		t.Fatalf("TurnTicket second: %v", err)
	}
	if first.Ticket != second.Ticket {
		t.Fatalf("ticket changed between calls: %q vs %q", first.Ticket, second.Ticket)
	}
	if server.ticketHits != 1 {
		t.Fatalf("ticket hits = %d, want 1", server.ticketHits)
	}
	if server.lastTicket["surface"] != "cli" || server.lastTicket["profile"] != "standard" {
		t.Fatalf("ticket request = %#v, want surface=cli profile=standard", server.lastTicket)
	}
}

func TestTurnTicketRenewsWhenExpired(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	now := time.Now()
	clock := func() time.Time { return now }
	session := NewSession(testCredentials(server.URL), WithHTTPClient(server.Client()), WithClock(clock))
	client := NewClient(session, WithClientClock(clock))

	if _, err := client.TurnTicket(context.Background(), ClientSurfaceCLI, DefaultProfile); err != nil {
		t.Fatalf("first ticket: %v", err)
	}
	now = now.Add(10 * time.Minute)
	if _, err := client.TurnTicket(context.Background(), ClientSurfaceCLI, DefaultProfile); err != nil {
		t.Fatalf("renewed ticket: %v", err)
	}
	if server.ticketHits != 2 {
		t.Fatalf("ticket hits = %d, want 2", server.ticketHits)
	}
}

func TestOpenModelCallSendsProtocolHeaders(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	session := NewSession(testCredentials(server.URL), WithHTTPClient(server.Client()))
	client := NewClient(session)

	stream, err := client.OpenModelCall(context.Background(), ModelCallRequest{
		Request: ChatRequest{Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hi"}`)}},
	})
	if err != nil {
		t.Fatalf("OpenModelCall: %v", err)
	}
	defer stream.Close()

	if got := server.lastHeaders.Get("authorization"); got != "Bearer access-token" {
		t.Fatalf("authorization = %q", got)
	}
	if got := server.lastHeaders.Get("x-perch-turn-ticket"); got != "ticket-value" {
		t.Fatalf("turn ticket header = %q", got)
	}
	if got := server.lastHeaders.Get("accept"); got != "text/event-stream" {
		t.Fatalf("accept = %q", got)
	}
	if server.lastModelReq.ClientSurface != "cli" || server.lastModelReq.Lane != "chat" {
		t.Fatalf("model call = %#v, want clientSurface=cli lane=chat", server.lastModelReq)
	}
	if server.lastModelReq.AvoidModelIDs == nil {
		t.Fatalf("avoidModelIds must be an empty array, not null")
	}
	if server.lastModelReq.RunID == "" {
		t.Fatalf("runId must be generated when missing")
	}
}

func TestModelCallRefreshesOnUnauthorized(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{FirstModelFails: true})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(time.Hour).Unix()
	session := NewSession(creds, WithHTTPClient(server.Client()))
	client := NewClient(session)

	stream, err := client.OpenModelCall(context.Background(), ModelCallRequest{})
	if err != nil {
		t.Fatalf("OpenModelCall after refresh: %v", err)
	}
	defer stream.Close()

	if server.modelHits != 2 {
		t.Fatalf("model hits = %d, want 2", server.modelHits)
	}
	if server.refreshHits != 1 {
		t.Fatalf("refresh hits = %d, want 1", server.refreshHits)
	}
	if got := server.lastHeaders.Get("authorization"); got != "Bearer access-after-refresh" {
		t.Fatalf("retry authorization = %q", got)
	}
}

func TestModelCallErrorEnvelope(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{ModelStatus: http.StatusUnauthorized})
	session := NewSession(testCredentials(server.URL), WithHTTPClient(server.Client()))
	client := NewClient(session)

	_, err := client.OpenModelCall(context.Background(), ModelCallRequest{})
	if err == nil {
		t.Fatalf("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.Code != CodeNotAuthenticated {
		t.Fatalf("code = %q, want %q", apiErr.Code, CodeNotAuthenticated)
	}
	if apiErr.OpenAIType() != "authentication_error" {
		t.Fatalf("openai type = %q", apiErr.OpenAIType())
	}
}

func TestCollectOutcomeFromCapturedStream(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	session := NewSession(testCredentials(server.URL), WithHTTPClient(server.Client()))
	client := NewClient(session)

	outcome, err := client.CallModel(context.Background(), ModelCallRequest{})
	if err != nil {
		t.Fatalf("CallModel: %v", err)
	}
	if outcome.Text != "pong" {
		t.Fatalf("text = %q, want pong", outcome.Text)
	}
	if outcome.Model != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("model = %q", outcome.Model)
	}
	if outcome.Usage == nil || outcome.Usage.OutputTokens != 4 || outcome.Usage.CacheReadInputTokens != 26496 {
		t.Fatalf("usage = %#v", outcome.Usage)
	}
	if outcome.Usage.PromptTokens() != 26672 {
		t.Fatalf("prompt tokens = %d, want 26672", outcome.Usage.PromptTokens())
	}
}

func TestModelCallSurfaceGateIsNotRetried(t *testing.T) {
	flagReason := []byte(`{"ok":false,"error":"Your plan includes Perch-hosted models for use in Perch AI Web, Desktop, and CLI only.","errorCode":"perch_surface_required","limit":{"limitKind":"perch_surface_required","plan":"pilot","resetAt":null}}`)
	server := newPerchTestServer(t, perchTestOptions{ModelStatus: http.StatusForbidden, ModelErrorBody: flagReason})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(time.Hour).Unix()
	session := NewSession(creds, WithHTTPClient(server.Client()))
	client := NewClient(session)

	_, err := client.OpenModelCall(context.Background(), ModelCallRequest{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.Code != CodeSurfaceRequired {
		t.Fatalf("code = %q, want %q", apiErr.Code, CodeSurfaceRequired)
	}
	if got := apiErr.OpenAIType(); got != "permission_error" {
		t.Fatalf("openai type = %q", got)
	}
	if got := ErrorStatus(apiErr.Code); got != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", got)
	}
	if server.modelHits != 1 {
		t.Fatalf("model hits = %d, want 1", server.modelHits)
	}
	if server.refreshHits != 0 {
		t.Fatalf("refresh hits = %d, want 0", server.refreshHits)
	}
}

func TestModelCallRefreshesWhenTicketMintIsRejected(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{FirstTicketFails: true})
	creds := testCredentials(server.URL)
	creds.ExpiresAt = time.Now().Add(time.Hour).Unix()
	session := NewSession(creds, WithHTTPClient(server.Client()))
	client := NewClient(session)

	stream, err := client.OpenModelCall(context.Background(), ModelCallRequest{})
	if err != nil {
		t.Fatalf("OpenModelCall after ticket refresh: %v", err)
	}
	defer stream.Close()

	if server.ticketHits != 2 {
		t.Fatalf("ticket hits = %d, want 2", server.ticketHits)
	}
	if server.refreshHits != 1 {
		t.Fatalf("refresh hits = %d, want 1", server.refreshHits)
	}
	if server.modelHits != 1 {
		t.Fatalf("model hits = %d, want 1", server.modelHits)
	}
	if got := server.lastHeaders.Get("x-perch-turn-ticket"); got == "" {
		t.Fatalf("model call reached Perch without a turn ticket")
	}
}

func TestCollectOutcomeResetsAfterStreamRestart(t *testing.T) {
	streamBody := []byte("data: {\"type\":\"answer_delta\",\"text\":\"abc\"}\n\n" +
		"data: {\"type\":\"stream_restart\",\"reason\":\"same_model_resend\"}\n\n" +
		"data: {\"type\":\"answer_delta\",\"text\":\"x\"}\n\n" +
		"data: {\"type\":\"done\",\"ok\":true,\"text\":\"\",\"finishReason\":\"stop\",\"model\":\"deepseek/deepseek-v4.1-flash\",\"usage\":{\"inputTokens\":3,\"outputTokens\":1}}\n\n")
	server := newPerchTestServer(t, perchTestOptions{StreamBody: streamBody})
	session := NewSession(testCredentials(server.URL), WithHTTPClient(server.Client()))
	client := NewClient(session)

	outcome, err := client.CallModel(context.Background(), ModelCallRequest{})
	if err != nil {
		t.Fatalf("CallModel: %v", err)
	}
	if outcome.Text != "x" {
		t.Fatalf("text = %q, want x", outcome.Text)
	}
}
