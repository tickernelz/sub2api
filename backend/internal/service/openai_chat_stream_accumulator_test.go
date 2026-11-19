//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatCompletionsStreamAccumulator_MergesToolCallsWithoutIndex(t *testing.T) {
	acc := newChatCompletionsStreamAccumulator()
	for _, payload := range []string{
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"id":"call_a","type":"function","function":{"name":"first","arguments":"{\"a\":"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"function":{"name":"first","arguments":"1}"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_b","function":{"name":"second","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
	} {
		acc.add([]byte(payload))
	}

	body, err := acc.assemble()
	require.NoError(t, err)
	require.Equal(t, "tool_calls", acc.finishReason())
	require.Equal(t, "assistant", gjson.GetBytes(body, "choices.0.message.role").String())
	require.Equal(t, gjson.Null, gjson.GetBytes(body, "choices.0.message.content").Type)
	require.Equal(t, int64(2), gjson.GetBytes(body, "choices.0.message.tool_calls.#").Int())
	require.Equal(t, "call_a", gjson.GetBytes(body, "choices.0.message.tool_calls.0.id").String())
	require.Equal(t, "first", gjson.GetBytes(body, "choices.0.message.tool_calls.0.function.name").String())
	require.Equal(t, `{"a":1}`, gjson.GetBytes(body, "choices.0.message.tool_calls.0.function.arguments").String())
	require.Equal(t, "call_b", gjson.GetBytes(body, "choices.0.message.tool_calls.1.id").String())
	require.Equal(t, "function", gjson.GetBytes(body, "choices.0.message.tool_calls.1.type").String())
	require.False(t, gjson.GetBytes(body, "choices.0.message.tool_calls.1.index").Exists())
	require.NotZero(t, gjson.GetBytes(body, "created").Int())
}

func TestChatCompletionsStreamAccumulator_KeepsFieldsItDoesNotModel(t *testing.T) {
	acc := newChatCompletionsStreamAccumulator()
	for _, payload := range []string{
		`{"id":"c2","created":1763550000,"model":"m","citations":["https://a.example"],"choices":[{"index":0,"delta":{"role":"assistant","content":"hel"},"logprobs":{"content":[{"token":"hel","logprob":-0.1}]}}]}`,
		`{"id":"c2","created":1763550001,"model":"m","citations":["https://a.example"],"choices":[{"index":0,"delta":{"content":"lo","annotations":[{"type":"url_citation"}],"function_call":{"name":"legacy","arguments":"{\"x\":"}},"logprobs":{"content":[{"token":"lo","logprob":-0.2}]}}]}`,
		`{"id":"c2","model":"m","choices":[{"index":0,"delta":{"function_call":{"arguments":"1}"}},"logprobs":null,"finish_reason":"stop"}]}`,
		`{"id":"c2","model":"m","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5,"prompt_cache_hit_tokens":1,"cost":0.00012}}`,
	} {
		acc.add([]byte(payload))
	}

	body, err := acc.assemble()
	require.NoError(t, err)
	require.Equal(t, int64(1763550000), gjson.GetBytes(body, "created").Int())
	require.JSONEq(t, `["https://a.example"]`, gjson.GetBytes(body, "citations").Raw)
	require.Equal(t, "hello", gjson.GetBytes(body, "choices.0.message.content").String())
	require.JSONEq(t, `[{"token":"hel","logprob":-0.1},{"token":"lo","logprob":-0.2}]`, gjson.GetBytes(body, "choices.0.logprobs.content").Raw)
	require.JSONEq(t, `[{"type":"url_citation"}]`, gjson.GetBytes(body, "choices.0.message.annotations").Raw)
	require.Equal(t, "legacy", gjson.GetBytes(body, "choices.0.message.function_call.name").String())
	require.Equal(t, `{"x":1}`, gjson.GetBytes(body, "choices.0.message.function_call.arguments").String())
	require.JSONEq(t, `{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5,"prompt_cache_hit_tokens":1,"cost":0.00012}`, gjson.GetBytes(body, "usage").Raw)
}
