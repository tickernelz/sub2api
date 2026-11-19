package apicompat

import (
	"encoding/json"
	"sort"
	"strings"
)

// ChatCompletionsStreamAccumulator rebuilds a non-streaming Chat Completions response from streamed chunks.
type ChatCompletionsStreamAccumulator struct {
	resp    ChatCompletionsResponse
	choices map[int]*chatChoiceAccumulator
}

type chatChoiceAccumulator struct {
	role             string
	content          strings.Builder
	sawContent       bool
	refusal          strings.Builder
	reasoningContent strings.Builder
	reasoning        strings.Builder
	toolCalls        []ChatToolCall
	toolCallSlots    map[int]int
	finishReason     string
}

func NewChatCompletionsStreamAccumulator() *ChatCompletionsStreamAccumulator {
	return &ChatCompletionsStreamAccumulator{choices: map[int]*chatChoiceAccumulator{}}
}

// Add folds one streamed chunk into the response.
func (a *ChatCompletionsStreamAccumulator) Add(chunk *ChatCompletionsChunk) {
	if chunk == nil {
		return
	}
	if a.resp.ID == "" {
		a.resp.ID = chunk.ID
	}
	if a.resp.Created == 0 {
		a.resp.Created = chunk.Created
	}
	if chunk.Model != "" {
		a.resp.Model = chunk.Model
	}
	if chunk.SystemFingerprint != "" {
		a.resp.SystemFingerprint = chunk.SystemFingerprint
	}
	if chunk.ServiceTier != "" {
		a.resp.ServiceTier = chunk.ServiceTier
	}
	if chunk.Usage != nil {
		usage := *chunk.Usage
		a.resp.Usage = &usage
	}
	for i := range chunk.Choices {
		a.addChoice(&chunk.Choices[i])
	}
}

func (a *ChatCompletionsStreamAccumulator) addChoice(choice *ChatChunkChoice) {
	acc := a.choices[choice.Index]
	if acc == nil {
		acc = &chatChoiceAccumulator{toolCallSlots: map[int]int{}}
		a.choices[choice.Index] = acc
	}
	delta := choice.Delta
	if delta.Role != "" {
		acc.role = delta.Role
	}
	if delta.Content != nil {
		acc.sawContent = true
		_, _ = acc.content.WriteString(*delta.Content)
	}
	if delta.Refusal != nil {
		_, _ = acc.refusal.WriteString(*delta.Refusal)
	}
	if delta.ReasoningContent != nil {
		_, _ = acc.reasoningContent.WriteString(*delta.ReasoningContent)
	}
	if delta.Reasoning != nil {
		_, _ = acc.reasoning.WriteString(*delta.Reasoning)
	}
	for _, call := range delta.ToolCalls {
		acc.addToolCall(call)
	}
	if choice.FinishReason != nil && *choice.FinishReason != "" {
		acc.finishReason = *choice.FinishReason
	}
}

func (acc *chatChoiceAccumulator) addToolCall(call ChatToolCall) {
	slot := -1
	switch {
	case call.Index != nil:
		if existing, ok := acc.toolCallSlots[*call.Index]; ok {
			slot = existing
		}
	case len(acc.toolCalls) > 0 && (call.ID == "" || call.ID == acc.toolCalls[len(acc.toolCalls)-1].ID):
		slot = len(acc.toolCalls) - 1
	}
	if slot < 0 {
		acc.toolCalls = append(acc.toolCalls, ChatToolCall{ID: call.ID, Type: call.Type, Function: call.Function})
		if call.Index != nil {
			acc.toolCallSlots[*call.Index] = len(acc.toolCalls) - 1
		}
		return
	}
	merged := &acc.toolCalls[slot]
	if merged.ID == "" {
		merged.ID = call.ID
	}
	if merged.Type == "" {
		merged.Type = call.Type
	}
	if merged.Function.Name == "" {
		merged.Function.Name = call.Function.Name
	}
	merged.Function.Arguments += call.Function.Arguments
}

// FinishReason returns the first finish_reason any choice reported, or "" while the reply is unfinished.
func (a *ChatCompletionsStreamAccumulator) FinishReason() string {
	for _, index := range a.choiceIndexes() {
		if reason := a.choices[index].finishReason; reason != "" {
			return reason
		}
	}
	return ""
}

// Response returns the assembled chat.completion object.
func (a *ChatCompletionsStreamAccumulator) Response() *ChatCompletionsResponse {
	resp := a.resp
	resp.Object = "chat.completion"
	resp.Choices = make([]ChatChoice, 0, len(a.choices))
	for _, index := range a.choiceIndexes() {
		acc := a.choices[index]
		message := ChatMessage{
			Role:             acc.role,
			Content:          json.RawMessage("null"),
			Refusal:          acc.refusal.String(),
			ReasoningContent: acc.reasoningContent.String(),
			Reasoning:        acc.reasoning.String(),
			ToolCalls:        acc.toolCalls,
		}
		if message.Role == "" {
			message.Role = "assistant"
		}
		if acc.sawContent {
			content, _ := json.Marshal(acc.content.String())
			message.Content = content
		}
		for i := range message.ToolCalls {
			if message.ToolCalls[i].Type == "" {
				message.ToolCalls[i].Type = "function"
			}
		}
		resp.Choices = append(resp.Choices, ChatChoice{Index: index, Message: message, FinishReason: acc.finishReason})
	}
	return &resp
}

func (a *ChatCompletionsStreamAccumulator) choiceIndexes() []int {
	indexes := make([]int, 0, len(a.choices))
	for index := range a.choices {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}
