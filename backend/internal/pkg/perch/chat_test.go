package perch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

func testChatRequest() *apicompat.ChatCompletionsRequest {
	temperature := 0.6
	maxTokens := 32000
	return &apicompat.ChatCompletionsRequest{
		Model:       "wandb-kimi-k2-6",
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
		Messages: []apicompat.ChatMessage{
			{Role: "system", Content: json.RawMessage(`"You are terse."`)},
			{Role: "user", Content: json.RawMessage(`"Reply with pong."`)},
		},
		Tools: []apicompat.ChatTool{{
			Type:     "function",
			Function: &apicompat.ChatFunction{Name: "noop", Description: "No-op."},
		}},
		ToolChoice: json.RawMessage(`"auto"`),
	}
}

func TestBuildModelCallRequestCarriesConversationAndOptions(t *testing.T) {
	call, err := BuildModelCallRequest(testChatRequest(), CallOptions{
		RunID:       "run-1",
		Attribution: &Attribution{UserID: "user-1", Source: "cli"},
	})
	if err != nil {
		t.Fatalf("BuildModelCallRequest: %v", err)
	}
	if call.RunID != "run-1" || call.Lane != LaneChat || call.ClientSurface != ClientSurfaceCLI {
		t.Fatalf("call envelope = %#v", call)
	}
	if call.ManualModelOptionID != "wandb-kimi-k2-6" || !call.StrictManual {
		t.Fatalf("manualModelOptionId = %q strictManual = %v", call.ManualModelOptionID, call.StrictManual)
	}
	if call.RoostModelChoice != "" || call.PreferredModelID != nil {
		t.Fatalf("tier = %q preferred = %v", call.RoostModelChoice, call.PreferredModelID)
	}
	if call.Attribution == nil || call.Attribution.UserID != "user-1" {
		t.Fatalf("attribution = %#v", call.Attribution)
	}
	if call.AvoidModelIDs == nil {
		t.Fatalf("avoidModelIds must serialize as []")
	}
	if len(call.Request.Messages) != 4 || len(call.Request.Tools) != 1 {
		t.Fatalf("messages=%d tools=%d", len(call.Request.Messages), len(call.Request.Tools))
	}
	if got := messageRoles(t, call.Request.Messages); got[0] != "system" || got[1] != "system" || got[2] != "user" || got[3] != "user" {
		t.Fatalf("roles = %v, want system,system,user,user", got)
	}
	context := messageText(t, call.Request.Messages[2])
	if !strings.Contains(context, "<perch_turn_context>") || !strings.Contains(context, time.Now().Format("2006-01-02")) {
		t.Fatalf("turn context = %q", context)
	}
	if call.Request.MaxTokens == nil || *call.Request.MaxTokens != 32000 {
		t.Fatalf("maxTokens = %v", call.Request.MaxTokens)
	}
	if string(call.Request.ToolChoice) != `"auto"` {
		t.Fatalf("toolChoice = %s", call.Request.ToolChoice)
	}

	encoded, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("encode call: %v", err)
	}
	for _, want := range []string{`"request"`, `"runId":"run-1"`, `"lane":"chat"`, `"clientSurface":"cli"`, `"avoidModelIds":[]`, `"maxTokens":32000`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("encoded call missing %s: %s", want, encoded)
		}
	}
	if strings.Contains(string(encoded), "max_tokens") {
		t.Fatalf("call must use Perch camelCase maxTokens: %s", encoded)
	}
}

func messageRoles(t *testing.T, messages []json.RawMessage) []string {
	t.Helper()
	roles := make([]string, 0, len(messages))
	for _, raw := range messages {
		var probe struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			t.Fatalf("decode message role: %v", err)
		}
		roles = append(roles, probe.Role)
	}
	return roles
}

func messageText(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var probe struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("decode message content: %v", err)
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(probe.Content, &parts); err == nil && len(parts) > 0 {
		var builder strings.Builder
		for _, part := range parts {
			_, _ = builder.WriteString(part.Text)
		}
		return builder.String()
	}
	var plain string
	if err := json.Unmarshal(probe.Content, &plain); err != nil {
		t.Fatalf("decode message text: %v", err)
	}
	return plain
}

func TestBuildModelCallRequestMapsTierAliases(t *testing.T) {
	cases := map[string]string{
		"standard":     RoostStandard,
		"standard_max": RoostStandardMax,
		"Standard 1M":  RoostStandardMax,
		"pro":          RoostPro,
		"PRO_MAX":      RoostProMax,
	}
	for model, want := range cases {
		req := testChatRequest()
		req.Model = model
		call, err := BuildModelCallRequest(req, CallOptions{})
		if err != nil {
			t.Fatalf("BuildModelCallRequest(%q): %v", model, err)
		}
		if call.RoostModelChoice != want {
			t.Fatalf("model %q tier = %q, want %q", model, call.RoostModelChoice, want)
		}
		if call.ManualModelOptionID != "" || call.StrictManual {
			t.Fatalf("model %q pinned manually: %q strict=%v", model, call.ManualModelOptionID, call.StrictManual)
		}
	}
}

func TestBuildModelCallRequestPinsUnknownModelManually(t *testing.T) {
	req := testChatRequest()
	req.Model = "deepseek/deepseek-v4.1-flash"
	call, err := BuildModelCallRequest(req, CallOptions{})
	if err != nil {
		t.Fatalf("BuildModelCallRequest: %v", err)
	}
	if call.ManualModelOptionID != "deepseek/deepseek-v4.1-flash" || !call.StrictManual {
		t.Fatalf("manualModelOptionId = %q strictManual = %v", call.ManualModelOptionID, call.StrictManual)
	}
	if call.RoostModelChoice != "" {
		t.Fatalf("tier = %q, want empty", call.RoostModelChoice)
	}
	encoded, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("encode call: %v", err)
	}
	if !strings.Contains(string(encoded), `"manualModelOptionId":"deepseek/deepseek-v4.1-flash"`) || !strings.Contains(string(encoded), `"strictManual":true`) {
		t.Fatalf("encoded call = %s", encoded)
	}
}

func TestBuildModelCallRequestDefaultsToStandardMax(t *testing.T) {
	req := testChatRequest()
	req.Model = ""
	call, err := BuildModelCallRequest(req, CallOptions{})
	if err != nil {
		t.Fatalf("BuildModelCallRequest: %v", err)
	}
	if call.RoostModelChoice != DefaultRoostTier {
		t.Fatalf("tier = %q, want %q", call.RoostModelChoice, DefaultRoostTier)
	}
}

func TestBuildModelCallRequestFallsBackToMaxCompletionTokens(t *testing.T) {
	req := testChatRequest()
	req.MaxTokens = nil
	completion := 4096
	req.MaxCompletionTokens = &completion

	call, err := BuildModelCallRequest(req, CallOptions{})
	if err != nil {
		t.Fatalf("BuildModelCallRequest: %v", err)
	}
	if call.Request.MaxTokens == nil || *call.Request.MaxTokens != 4096 {
		t.Fatalf("maxTokens = %v, want 4096", call.Request.MaxTokens)
	}
}

func TestBuildModelCallRequestRejectsEmptyConversation(t *testing.T) {
	if _, err := BuildModelCallRequest(&apicompat.ChatCompletionsRequest{}, CallOptions{}); err == nil {
		t.Fatalf("expected error for request without messages")
	}
}

func TestUsageToChatTreatsCacheTokensAsPrompt(t *testing.T) {
	usage := UsageToChat(&Usage{InputTokens: 176, OutputTokens: 4, CacheReadInputTokens: 26496, TotalTokens: 26676})
	if usage.PromptTokens != 26672 {
		t.Fatalf("prompt tokens = %d, want 26672", usage.PromptTokens)
	}
	if usage.CompletionTokens != 4 || usage.TotalTokens != 26676 {
		t.Fatalf("usage = %#v", usage)
	}
	if usage.PromptTokensDetails == nil || usage.PromptTokensDetails.CachedTokens != 26496 {
		t.Fatalf("cached tokens detail = %#v", usage.PromptTokensDetails)
	}
}

func TestFinishReasonMapping(t *testing.T) {
	cases := []struct {
		name    string
		outcome *Outcome
		want    string
	}{
		{"stop", &Outcome{FinishReason: "stop"}, "stop"},
		{"length", &Outcome{FinishReason: "max_tokens"}, "length"},
		{"context", &Outcome{FinishReason: "model_context_window_exceeded"}, "length"},
		{"tool calls by reason", &Outcome{FinishReason: "tool_use"}, "tool_calls"},
		{"tool calls by payload", &Outcome{ToolCalls: []ToolCall{{ID: "call_1"}}}, "tool_calls"},
		{"empty", &Outcome{}, "stop"},
	}
	for _, tc := range cases {
		if got := FinishReason(tc.outcome); got != tc.want {
			t.Fatalf("%s: finish reason = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestChunkEmitterEmitsRoleOnceAndUsageLast(t *testing.T) {
	stream := NewSSEStream(strings.NewReader(string(fixture(t, "model_call_stream.sse"))))
	emitter := NewChunkEmitter("wandb-kimi-k2-6", "chatcmpl-1", 1700000000)

	var chunks []apicompat.ChatCompletionsChunk
	var settled *Outcome
	for {
		payload, err := stream.Next()
		if err != nil {
			break
		}
		event, body, err := decodeEvent(payload)
		if err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if event.Type == EventDone {
			settled = outcomeFromDone(body)
			continue
		}
		chunks = append(chunks, emitter.Chunks(event, body)...)
	}
	if settled == nil {
		t.Fatalf("stream carried no done event")
	}
	chunks = append(chunks, emitter.Final(settled, true)...)

	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3 (content, finish, usage)", len(chunks))
	}
	if chunks[0].Choices[0].Delta.Role != "assistant" {
		t.Fatalf("first chunk role = %q", chunks[0].Choices[0].Delta.Role)
	}
	if chunks[1].Choices[0].Delta.Role != "" {
		t.Fatalf("role must only be sent once")
	}
	if got := chunks[1].Choices[0].FinishReason; got == nil || *got != "stop" {
		t.Fatalf("finish reason = %v", got)
	}
	if chunks[2].Usage == nil || chunks[2].Usage.TotalTokens != 26676 {
		t.Fatalf("usage chunk = %#v", chunks[2].Usage)
	}
}

func TestChunkEmitterEmitsSealedToolArguments(t *testing.T) {
	stream := NewSSEStream(strings.NewReader(string(fixture(t, "model_call_tool_sealed.sse"))))
	emitter := NewChunkEmitter("wandb-kimi-k2-6", "chatcmpl-3", 1700000000)

	var calls []apicompat.ChatToolCall
	var settled *Outcome
	for {
		payload, err := stream.Next()
		if err != nil {
			break
		}
		event, body, err := decodeEvent(payload)
		if err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if event.Type == EventDone {
			settled = outcomeFromDone(body)
			continue
		}
		for _, chunk := range emitter.Chunks(event, body) {
			calls = append(calls, chunk.Choices[0].Delta.ToolCalls...)
		}
	}
	if settled == nil {
		t.Fatalf("stream carried no done event")
	}
	for _, chunk := range emitter.Final(settled, false) {
		calls = append(calls, chunk.Choices[0].Delta.ToolCalls...)
	}
	if len(calls) != 1 {
		t.Fatalf("tool call deltas = %d, want 1 sealed call", len(calls))
	}
	if calls[0].Function.Arguments != `{"path":"a.txt","body":"x"}` {
		t.Fatalf("arguments = %s, want the sealed snapshot", calls[0].Function.Arguments)
	}
}

func TestChunkEmitterDeduplicatesToolCalls(t *testing.T) {
	stream := NewSSEStream(strings.NewReader(string(fixture(t, "model_call_tool_stream.sse"))))
	emitter := NewChunkEmitter("wandb-kimi-k2-6", "chatcmpl-2", 1700000000)

	var calls []apicompat.ChatToolCall
	var settled *Outcome
	for {
		payload, err := stream.Next()
		if err != nil {
			break
		}
		event, body, err := decodeEvent(payload)
		if err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if event.Type == EventDone {
			settled = outcomeFromDone(body)
			continue
		}
		for _, chunk := range emitter.Chunks(event, body) {
			calls = append(calls, chunk.Choices[0].Delta.ToolCalls...)
		}
	}
	if settled == nil {
		t.Fatalf("stream carried no done event")
	}
	for _, chunk := range emitter.Final(settled, false) {
		calls = append(calls, chunk.Choices[0].Delta.ToolCalls...)
	}
	if len(calls) != 1 {
		t.Fatalf("tool call deltas = %d, want 1 (done event repeats the same call)", len(calls))
	}
	if calls[0].Function.Name != "noop" || calls[0].Function.Arguments != `{"a":1}` {
		t.Fatalf("tool call = %#v", calls[0])
	}
	if settled == nil || FinishReason(settled) != "tool_calls" {
		t.Fatalf("settled = %#v", settled)
	}
}
