package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

var errChatStreamChunkInvalid = errors.New("invalid chat completions stream chunk")

var chatStreamConcatKeys = map[string]bool{
	"refusal":           true,
	"reasoning_content": true,
	"reasoning":         true,
	"arguments":         true,
	"data":              true,
	"transcript":        true,
}

// chatCompletionsStreamAccumulator rebuilds a non-streaming chat.completion from raw stream chunk payloads, keeping fields it does not model.
type chatCompletionsStreamAccumulator struct {
	top     map[string]any
	usage   any
	choices map[int]*chatChoiceAccumulator
	err     error
}

type chatChoiceAccumulator struct {
	extra         map[string]any
	message       map[string]any
	role          string
	content       strings.Builder
	sawContent    bool
	toolCalls     []*chatToolCallAccumulator
	toolCallSlots map[int64]int
	finishReason  string
}

type chatToolCallAccumulator struct {
	id        string
	callType  string
	name      string
	arguments strings.Builder
	extra     map[string]any
	function  map[string]any
}

func newChatCompletionsStreamAccumulator() *chatCompletionsStreamAccumulator {
	return &chatCompletionsStreamAccumulator{top: map[string]any{}, choices: map[int]*chatChoiceAccumulator{}}
}

// add folds one raw chunk payload into the reply; the first invalid payload fails assemble.
func (a *chatCompletionsStreamAccumulator) add(payload []byte) {
	if a.err != nil {
		return
	}
	if !gjson.ValidBytes(payload) {
		a.err = errChatStreamChunkInvalid
		return
	}
	gjson.ParseBytes(payload).ForEach(func(key, value gjson.Result) bool {
		if value.Type == gjson.Null {
			return true
		}
		switch key.Str {
		case "object":
		case "choices":
			value.ForEach(func(_, choice gjson.Result) bool {
				if choice.IsObject() {
					a.addChoice(choice)
				}
				return true
			})
		case "usage":
			a.usage = json.RawMessage(value.Raw)
		case "id", "created":
			if _, seen := a.top[key.Str]; !seen {
				a.top[key.Str] = json.RawMessage(value.Raw)
			}
		default:
			a.top[key.Str] = json.RawMessage(value.Raw)
		}
		return true
	})
}

func (a *chatCompletionsStreamAccumulator) addChoice(choice gjson.Result) {
	index := int(choice.Get("index").Int())
	acc := a.choices[index]
	if acc == nil {
		acc = &chatChoiceAccumulator{}
		a.choices[index] = acc
	}
	choice.ForEach(func(key, value gjson.Result) bool {
		if value.Type == gjson.Null {
			return true
		}
		switch key.Str {
		case "index":
		case "delta":
			acc.addDelta(value)
		case "finish_reason":
			if value.Str != "" {
				acc.finishReason = value.Str
			}
		default:
			if acc.extra == nil {
				acc.extra = map[string]any{}
			}
			acc.extra[key.Str] = mergeChatStreamValue(key.Str, acc.extra[key.Str], decodeChatStreamValue(value))
		}
		return true
	})
}

func (acc *chatChoiceAccumulator) addDelta(delta gjson.Result) {
	delta.ForEach(func(key, value gjson.Result) bool {
		if value.Type == gjson.Null {
			return true
		}
		switch key.Str {
		case "role":
			if acc.role == "" {
				acc.role = value.Str
			}
		case "content":
			if value.Type == gjson.String {
				acc.sawContent = true
				_, _ = acc.content.WriteString(value.Str)
				return true
			}
			if acc.message == nil {
				acc.message = map[string]any{}
			}
			acc.message[key.Str] = mergeChatStreamValue(key.Str, acc.message[key.Str], decodeChatStreamValue(value))
		case "tool_calls":
			value.ForEach(func(_, call gjson.Result) bool {
				if call.IsObject() {
					acc.addToolCall(call)
				}
				return true
			})
		default:
			if acc.message == nil {
				acc.message = map[string]any{}
			}
			acc.message[key.Str] = mergeChatStreamValue(key.Str, acc.message[key.Str], decodeChatStreamValue(value))
		}
		return true
	})
}

func (acc *chatChoiceAccumulator) addToolCall(call gjson.Result) {
	id := call.Get("id").Str
	index := call.Get("index")
	slot := -1
	switch {
	case index.Exists():
		if existing, ok := acc.toolCallSlots[index.Int()]; ok {
			slot = existing
		}
	case len(acc.toolCalls) > 0:
		if last := acc.toolCalls[len(acc.toolCalls)-1]; id == "" || id == last.id {
			slot = len(acc.toolCalls) - 1
		}
	}
	if slot < 0 {
		acc.toolCalls = append(acc.toolCalls, &chatToolCallAccumulator{})
		slot = len(acc.toolCalls) - 1
		if index.Exists() {
			if acc.toolCallSlots == nil {
				acc.toolCallSlots = map[int64]int{}
			}
			acc.toolCallSlots[index.Int()] = slot
		}
	}
	merged := acc.toolCalls[slot]
	call.ForEach(func(key, value gjson.Result) bool {
		if value.Type == gjson.Null {
			return true
		}
		switch key.Str {
		case "index":
		case "id":
			if merged.id == "" {
				merged.id = value.Str
			}
		case "type":
			if merged.callType == "" {
				merged.callType = value.Str
			}
		case "function":
			merged.addFunction(value)
		default:
			if merged.extra == nil {
				merged.extra = map[string]any{}
			}
			merged.extra[key.Str] = mergeChatStreamValue(key.Str, merged.extra[key.Str], decodeChatStreamValue(value))
		}
		return true
	})
}

func (call *chatToolCallAccumulator) addFunction(function gjson.Result) {
	function.ForEach(func(key, value gjson.Result) bool {
		if value.Type == gjson.Null {
			return true
		}
		switch key.Str {
		case "name":
			if call.name == "" {
				call.name = value.Str
			}
		case "arguments":
			_, _ = call.arguments.WriteString(value.Str)
		default:
			if call.function == nil {
				call.function = map[string]any{}
			}
			call.function[key.Str] = mergeChatStreamValue(key.Str, call.function[key.Str], decodeChatStreamValue(value))
		}
		return true
	})
}

func decodeChatStreamValue(value gjson.Result) any {
	decoder := json.NewDecoder(strings.NewReader(value.Raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return json.RawMessage(value.Raw)
	}
	return decoded
}

func mergeChatStreamValue(key string, current, next any) any {
	switch value := next.(type) {
	case string:
		if previous, ok := current.(string); ok && chatStreamConcatKeys[key] {
			return previous + value
		}
	case []any:
		if previous, ok := current.([]any); ok {
			return append(previous, value...)
		}
	case map[string]any:
		if previous, ok := current.(map[string]any); ok {
			for childKey, childValue := range value {
				if childValue == nil {
					continue
				}
				previous[childKey] = mergeChatStreamValue(childKey, previous[childKey], childValue)
			}
			return previous
		}
	}
	return next
}

// finishReason returns the first finish_reason any choice reported, or "" while the reply is unfinished.
func (a *chatCompletionsStreamAccumulator) finishReason() string {
	for _, index := range a.choiceIndexes() {
		if reason := a.choices[index].finishReason; reason != "" {
			return reason
		}
	}
	return ""
}

// assemble returns the chat.completion object built so far.
func (a *chatCompletionsStreamAccumulator) assemble() ([]byte, error) {
	if a.err != nil {
		return nil, a.err
	}
	out := make(map[string]any, len(a.top)+3)
	for key, value := range a.top {
		out[key] = value
	}
	out["object"] = "chat.completion"
	if _, ok := out["created"]; !ok {
		out["created"] = time.Now().Unix()
	}
	if a.usage != nil {
		out["usage"] = a.usage
	}
	choices := make([]any, 0, len(a.choices))
	for _, index := range a.choiceIndexes() {
		choices = append(choices, a.choices[index].output(index))
	}
	out["choices"] = choices

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(out); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func (acc *chatChoiceAccumulator) output(index int) map[string]any {
	message := make(map[string]any, len(acc.message)+3)
	for key, value := range acc.message {
		message[key] = value
	}
	message["role"] = acc.role
	if acc.role == "" {
		message["role"] = "assistant"
	}
	if acc.sawContent {
		message["content"] = acc.content.String()
	} else if _, ok := message["content"]; !ok {
		message["content"] = nil
	}
	if len(acc.toolCalls) > 0 {
		calls := make([]any, 0, len(acc.toolCalls))
		for _, call := range acc.toolCalls {
			calls = append(calls, call.output())
		}
		message["tool_calls"] = calls
	}
	choice := make(map[string]any, len(acc.extra)+3)
	for key, value := range acc.extra {
		choice[key] = value
	}
	choice["index"] = index
	choice["message"] = message
	choice["finish_reason"] = nil
	if acc.finishReason != "" {
		choice["finish_reason"] = acc.finishReason
	}
	return choice
}

func (call *chatToolCallAccumulator) output() map[string]any {
	function := make(map[string]any, len(call.function)+2)
	for key, value := range call.function {
		function[key] = value
	}
	function["name"] = call.name
	function["arguments"] = call.arguments.String()
	out := make(map[string]any, len(call.extra)+3)
	for key, value := range call.extra {
		out[key] = value
	}
	out["id"] = call.id
	out["type"] = call.callType
	if call.callType == "" {
		out["type"] = "function"
	}
	out["function"] = function
	return out
}

func (a *chatCompletionsStreamAccumulator) choiceIndexes() []int {
	indexes := make([]int, 0, len(a.choices))
	for index := range a.choices {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}
