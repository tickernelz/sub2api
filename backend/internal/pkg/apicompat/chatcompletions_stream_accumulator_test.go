package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatCompletionsStreamAccumulator_MergesToolCallsWithoutIndex(t *testing.T) {
	acc := NewChatCompletionsStreamAccumulator()
	for _, payload := range []string{
		`{"id":"c1","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_a","type":"function","function":{"name":"first","arguments":"{\"a\":"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"1}"}}]}}]}`,
		`{"id":"c1","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_b","function":{"name":"second","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
	} {
		var chunk ChatCompletionsChunk
		require.NoError(t, json.Unmarshal([]byte(payload), &chunk))
		acc.Add(&chunk)
	}

	resp := acc.Response()
	require.Equal(t, "tool_calls", acc.FinishReason())
	require.Len(t, resp.Choices, 1)
	message := resp.Choices[0].Message
	require.Equal(t, "assistant", message.Role)
	require.JSONEq(t, "null", string(message.Content))
	require.Len(t, message.ToolCalls, 2)
	require.Equal(t, "call_a", message.ToolCalls[0].ID)
	require.Equal(t, "first", message.ToolCalls[0].Function.Name)
	require.Equal(t, `{"a":1}`, message.ToolCalls[0].Function.Arguments)
	require.Equal(t, "call_b", message.ToolCalls[1].ID)
	require.Equal(t, "function", message.ToolCalls[1].Type)
	require.Nil(t, message.ToolCalls[1].Index)
}
