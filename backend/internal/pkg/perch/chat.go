package perch

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

// CallOptions are the per-request fields Perch needs beyond the conversation.
type CallOptions struct {
	RunID            string
	Lane             string
	ClientSurface    string
	Attribution      *Attribution
	AvoidModelIDs    []string
	PreferredModelID string
}

// BuildModelCallRequest converts an OpenAI Chat Completions request into a Perch model call.
func BuildModelCallRequest(req *apicompat.ChatCompletionsRequest, opts CallOptions) (ModelCallRequest, error) {
	if req == nil {
		return ModelCallRequest{}, fmt.Errorf("perch: chat request is required")
	}
	if len(req.Messages) == 0 {
		return ModelCallRequest{}, fmt.Errorf("perch: chat request carries no messages")
	}

	messages := make([]json.RawMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		raw, err := json.Marshal(message)
		if err != nil {
			return ModelCallRequest{}, fmt.Errorf("perch: encode message: %w", err)
		}
		messages = append(messages, raw)
	}

	tools := make([]json.RawMessage, 0, len(req.Tools))
	for _, tool := range req.Tools {
		raw, err := json.Marshal(tool)
		if err != nil {
			return ModelCallRequest{}, fmt.Errorf("perch: encode tool: %w", err)
		}
		tools = append(tools, raw)
	}

	maxTokens := req.MaxTokens
	if maxTokens == nil {
		maxTokens = req.MaxCompletionTokens
	}

	modelID := strings.TrimSpace(opts.PreferredModelID)
	if modelID == "" {
		modelID = strings.TrimSpace(req.Model)
	}

	call := ModelCallRequest{
		Request: ChatRequest{
			Messages:    messages,
			Tools:       tools,
			Temperature: req.Temperature,
			MaxTokens:   maxTokens,
			ToolChoice:  req.ToolChoice,
		},
		RunID:            strings.TrimSpace(opts.RunID),
		Lane:             firstNonEmpty(strings.TrimSpace(opts.Lane), DefaultLane),
		PreferredModelID: modelID,
		AvoidModelIDs:    opts.AvoidModelIDs,
		Attribution:      opts.Attribution,
		ClientSurface:    firstNonEmpty(strings.TrimSpace(opts.ClientSurface), ClientSurfaceCLI),
	}
	if call.AvoidModelIDs == nil {
		call.AvoidModelIDs = []string{}
	}
	return call, nil
}

// UsageToChat converts Perch token accounting into OpenAI usage.
func UsageToChat(usage *Usage) *apicompat.ChatUsage {
	if usage == nil {
		return nil
	}
	prompt := usage.PromptTokens()
	completion := usage.CompletionTokens()
	total := usage.TotalTokens
	if total <= 0 {
		total = prompt + completion
	}
	out := &apicompat.ChatUsage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
	}
	if usage.CacheReadInputTokens > 0 || usage.CacheCreationInputTokens > 0 {
		details := &apicompat.ChatTokenDetails{CachedTokens: usage.CacheReadInputTokens}
		if usage.CacheCreationInputTokens > 0 {
			details.CacheCreationTokens = usage.CacheCreationInputTokens
		}
		out.PromptTokensDetails = details
	}
	return out
}

// FinishReason maps a Perch finish reason onto the OpenAI vocabulary.
func FinishReason(outcome *Outcome) string {
	reason := strings.TrimSpace(outcome.FinishReason)
	switch reason {
	case "tool_calls", "tool_use":
		return "tool_calls"
	case "length", "max_tokens", "max_output_tokens", "model_context_window_exceeded":
		return "length"
	case "content_filter":
		return "content_filter"
	case "stop", "":
		if len(outcome.ToolCalls) > 0 {
			return "tool_calls"
		}
		return "stop"
	}
	if len(outcome.ToolCalls) > 0 {
		return "tool_calls"
	}
	return reason
}

// ToChatResponse renders a settled outcome as a non-streaming Chat Completions response.
func ToChatResponse(outcome *Outcome, model, responseID string, created int64) *apicompat.ChatCompletionsResponse {
	if created == 0 {
		created = time.Now().Unix()
	}
	message := apicompat.ChatMessage{
		Role:             "assistant",
		ReasoningContent: outcome.Reasoning,
	}
	if len(outcome.ToolCalls) > 0 {
		calls := make([]apicompat.ChatToolCall, 0, len(outcome.ToolCalls))
		for _, call := range outcome.ToolCalls {
			calls = append(calls, apicompat.ChatToolCall{
				ID:   call.ID,
				Type: "function",
				Function: apicompat.ChatFunctionCall{
					Name:      call.Name,
					Arguments: argumentsText(call.Arguments),
				},
			})
		}
		message.ToolCalls = calls
		if strings.TrimSpace(outcome.Text) != "" {
			message.Content = mustJSON(outcome.Text)
		}
	} else {
		message.Content = mustJSON(outcome.Text)
	}
	return &apicompat.ChatCompletionsResponse{
		ID:      responseID,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []apicompat.ChatChoice{{
			Index:        0,
			Message:      message,
			FinishReason: FinishReason(outcome),
		}},
		Usage: UsageToChat(outcome.Usage),
	}
}

// ChunkEmitter turns Perch stream events into OpenAI chat completion chunks.
type ChunkEmitter struct {
	ID      string
	Model   string
	Created int64

	roleSent bool
	emitted  map[string]struct{}
}

func NewChunkEmitter(model, responseID string, created int64) *ChunkEmitter {
	if created == 0 {
		created = time.Now().Unix()
	}
	return &ChunkEmitter{
		ID:      responseID,
		Model:   model,
		Created: created,
		emitted: make(map[string]struct{}),
	}
}

// Chunks converts one upstream event into zero or more client chunks.
func (e *ChunkEmitter) Chunks(event Event, body *eventBody) []apicompat.ChatCompletionsChunk {
	switch event.Type {
	case EventAnswerDelta:
		if body.Text == "" {
			return nil
		}
		text := body.Text
		return e.wrap(apicompat.ChatDelta{Content: &text}, nil)
	case EventReasoningDelta:
		if body.Text == "" {
			return nil
		}
		text := body.Text
		return e.wrap(apicompat.ChatDelta{ReasoningContent: &text}, nil)
	case EventToolCallDelta, EventToolUseEnd:
		if len(body.ToolCalls) == 0 {
			return nil
		}
		calls := make([]apicompat.ChatToolCall, 0, len(body.ToolCalls))
		for _, call := range body.ToolCalls {
			key := call.ID
			if key == "" {
				key = call.Name
			}
			if _, seen := e.emitted[key]; seen {
				continue
			}
			e.emitted[key] = struct{}{}
			index := len(e.emitted) - 1
			calls = append(calls, apicompat.ChatToolCall{
				Index: &index,
				ID:    call.ID,
				Type:  "function",
				Function: apicompat.ChatFunctionCall{
					Name:      call.Name,
					Arguments: argumentsText(call.Arguments),
				},
			})
		}
		if len(calls) == 0 {
			return nil
		}
		return e.wrap(apicompat.ChatDelta{ToolCalls: calls}, nil)
	}
	return nil
}

// Final emits the finish chunk plus the usage chunk when requested.
func (e *ChunkEmitter) Final(outcome *Outcome, includeUsage bool) []apicompat.ChatCompletionsChunk {
	reason := FinishReason(outcome)
	chunks := e.wrap(apicompat.ChatDelta{}, &reason)
	if includeUsage {
		usage := UsageToChat(outcome.Usage)
		if usage != nil {
			chunks = append(chunks, apicompat.ChatCompletionsChunk{
				ID:      e.ID,
				Object:  "chat.completion.chunk",
				Created: e.Created,
				Model:   e.Model,
				Choices: []apicompat.ChatChunkChoice{},
				Usage:   usage,
			})
		}
	}
	return chunks
}

func (e *ChunkEmitter) wrap(delta apicompat.ChatDelta, finish *string) []apicompat.ChatCompletionsChunk {
	if !e.roleSent {
		e.roleSent = true
		delta.Role = "assistant"
	}
	return []apicompat.ChatCompletionsChunk{{
		ID:      e.ID,
		Object:  "chat.completion.chunk",
		Created: e.Created,
		Model:   e.Model,
		Choices: []apicompat.ChatChunkChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: finish,
		}},
	}}
}

func argumentsText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "{}"
	}
	if strings.HasPrefix(trimmed, "\"") {
		var unquoted string
		if json.Unmarshal(raw, &unquoted) == nil {
			return unquoted
		}
	}
	return trimmed
}

func mustJSON(value string) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("\"\"")
	}
	return raw
}
