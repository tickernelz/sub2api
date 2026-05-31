package cursorproto

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectFrameRoundTrip(t *testing.T) {
	frame := WrapConnectFrame([]byte("hello"))
	require.Equal(t, byte(0), frame[0])
	require.Equal(t, []byte{0, 0, 0, 5}, frame[1:5])

	got, err := ReadConnectFrame(bytes.NewReader(frame))
	require.NoError(t, err)
	require.Equal(t, byte(0), got.Flags)
	require.Equal(t, []byte("hello"), got.Payload)

	_, err = ReadConnectFrame(bytes.NewReader(nil))
	require.ErrorIs(t, err, io.EOF)
}

func TestEncodeAgentRunRequestIncludesCursorRequiredPlaceholders(t *testing.T) {
	payload, err := EncodeAgentRunRequest(AgentRunInput{
		ModelID:        "composer-2-fast",
		UserText:       "hello cursor",
		ConversationID: "conv-1",
		MessageID:      "msg-1",
	})
	require.NoError(t, err)

	top := requireField(t, decodeFieldsForTest(t, payload), 1, wireTypeLen)
	run := decodeFieldsForTest(t, top.Bytes)

	requireField(t, run, 1, wireTypeLen) // conversation_state placeholder
	require.Equal(t, "conv-1", fieldString(t, requireField(t, run, 5, wireTypeLen)))
	require.Equal(t, uint64(0), fieldVarint(t, requireField(t, run, 12, wireTypeVarint)))
	require.Equal(t, "conv-1", fieldString(t, requireField(t, run, 16, wireTypeLen)))

	mcpTools := requireField(t, run, 4, wireTypeLen)
	require.Empty(t, mcpTools.Bytes, "Cursor expects the mcp_tools envelope even when no tools are declared")

	action := requireField(t, run, 2, wireTypeLen)
	conversationAction := decodeFieldsForTest(t, action.Bytes)
	userMessageAction := requireField(t, conversationAction, 1, wireTypeLen)
	uma := decodeFieldsForTest(t, userMessageAction.Bytes)
	userMessage := requireField(t, uma, 1, wireTypeLen)
	um := decodeFieldsForTest(t, userMessage.Bytes)
	require.Equal(t, "hello cursor", fieldString(t, requireField(t, um, 1, wireTypeLen)))
	require.Equal(t, "msg-1", fieldString(t, requireField(t, um, 2, wireTypeLen)))
	requireField(t, um, 3, wireTypeLen) // selected_context placeholder
	require.Equal(t, uint64(1), fieldVarint(t, requireField(t, um, 4, wireTypeVarint)))

	requestedModel := requireField(t, run, 9, wireTypeLen)
	rm := decodeFieldsForTest(t, requestedModel.Bytes)
	require.Equal(t, "composer-2", fieldString(t, requireField(t, rm, 1, wireTypeLen)))
	param := requireField(t, rm, 3, wireTypeLen)
	pf := decodeFieldsForTest(t, param.Bytes)
	require.Equal(t, "fast", fieldString(t, requireField(t, pf, 1, wireTypeLen)))
	require.Equal(t, "true", fieldString(t, requireField(t, pf, 2, wireTypeLen)))
}

func TestDecodeServerTextAndTurnEnd(t *testing.T) {
	payload := encodeMessage(1, encodeMessage(1, encodeString(1, "hi")))
	payload = append(payload, encodeMessage(1, encodeMessage(14))...)

	events, err := DecodeAgentServerMessage(payload)
	require.NoError(t, err)
	require.Equal(t, []ServerEvent{
		{Kind: ServerEventText, Text: "hi"},
		{Kind: ServerEventTurnEnded},
	}, events)
}

func TestEncodeRequestContextResponseFrame(t *testing.T) {
	execServer := append(encodeVarintField(1, 7), encodeMessage(10)...)
	execServer = append(execServer, encodeString(15, "exec-1")...)
	serverPayload := encodeMessage(2, execServer)
	event, err := DecodeExecServerEvent(serverPayload)
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, ExecEventRequestContext, event.Kind)
	require.Equal(t, uint64(7), event.ID)
	require.Equal(t, "exec-1", event.ExecID)

	frame := EncodeRequestContextResponse(7, "exec-1")
	connectFrame, err := ReadConnectFrame(bytes.NewReader(frame))
	require.NoError(t, err)

	top := requireField(t, decodeFieldsForTest(t, connectFrame.Payload), 2, wireTypeLen)
	execMessage := decodeFieldsForTest(t, top.Bytes)
	require.Equal(t, uint64(7), fieldVarint(t, requireField(t, execMessage, 1, wireTypeVarint)))
	require.Equal(t, "exec-1", fieldString(t, requireField(t, execMessage, 15, wireTypeLen)))
	result := requireField(t, execMessage, 10, wireTypeLen)
	resultFields := decodeFieldsForTest(t, result.Bytes)
	requireField(t, resultFields, 1, wireTypeLen)
}

func TestDecodeUnsupportedExecReadAndEncodeRejectedResult(t *testing.T) {
	readArgs := encodeString(1, "secret.txt")
	execServer := append(encodeVarintField(1, 8), encodeMessage(7, readArgs)...)
	execServer = append(execServer, encodeString(15, "exec-read")...)
	serverPayload := encodeMessage(2, execServer)

	event, err := DecodeExecServerEvent(serverPayload)
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, ExecEventUnsupported, event.Kind)
	require.Equal(t, uint64(8), event.ID)
	require.Equal(t, "exec-read", event.ExecID)
	require.Equal(t, 7, event.ResultFieldNumber)

	frame := EncodeUnsupportedExecResult(*event, "Cursor built-in tools are not supported")
	connectFrame, err := ReadConnectFrame(bytes.NewReader(frame))
	require.NoError(t, err)
	top := requireField(t, decodeFieldsForTest(t, connectFrame.Payload), 2, wireTypeLen)
	execClient := decodeFieldsForTest(t, top.Bytes)
	require.Equal(t, uint64(8), fieldVarint(t, requireField(t, execClient, 1, wireTypeVarint)))
	require.Equal(t, "exec-read", fieldString(t, requireField(t, execClient, 15, wireTypeLen)))
	readResult := requireField(t, execClient, 7, wireTypeLen)
	rejected := requireField(t, decodeFieldsForTest(t, readResult.Bytes), 2, wireTypeLen)
	rejection := decodeFieldsForTest(t, rejected.Bytes)
	require.Equal(t, "secret.txt", fieldString(t, requireField(t, rejection, 1, wireTypeLen)))
	require.Contains(t, fieldString(t, requireField(t, rejection, 2, wireTypeLen)), "not supported")
}

func TestDecodeKvSetBlobAndEncodeAckEchoesMetadata(t *testing.T) {
	metadata := []byte{1, 2, 3}
	setArgs := append(encodeBytes(1, []byte("blob-id")), encodeBytes(2, []byte("blob-data"))...)
	kv := append(encodeVarintField(1, 42), encodeMessage(3, setArgs)...)
	kv = append(kv, encodeBytes(4, metadata)...)
	payload := encodeMessage(4, kv)

	event, err := DecodeKvServerEvent(payload)
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, KvEventSetBlob, event.Kind)
	require.Equal(t, uint64(42), event.ID)
	require.Equal(t, []byte("blob-id"), event.BlobID)
	require.Equal(t, metadata, event.RequestMetadata)

	ack := EncodeKvSetBlobResult(event.ID, event.RequestMetadata)
	frame, err := ReadConnectFrame(bytes.NewReader(ack))
	require.NoError(t, err)
	top := requireField(t, decodeFieldsForTest(t, frame.Payload), 3, wireTypeLen)
	client := decodeFieldsForTest(t, top.Bytes)
	require.Equal(t, uint64(42), fieldVarint(t, requireField(t, client, 1, wireTypeVarint)))
	requireField(t, client, 3, wireTypeLen)
	require.Equal(t, metadata, requireField(t, client, 4, wireTypeLen).Bytes)
}

func decodeFieldsForTest(t *testing.T, payload []byte) []field {
	t.Helper()
	fields, err := decodeFields(payload)
	require.NoError(t, err)
	return fields
}

func requireField(t *testing.T, fields []field, fieldNumber int, wt wireType) field {
	t.Helper()
	for _, f := range fields {
		if f.Number == fieldNumber && f.WireType == wt {
			return f
		}
	}
	t.Fatalf("field %d/%d not found in %#v", fieldNumber, wt, fields)
	return field{}
}

func fieldString(t *testing.T, f field) string {
	t.Helper()
	require.Equal(t, wireTypeLen, f.WireType)
	return string(f.Bytes)
}

func fieldVarint(t *testing.T, f field) uint64 {
	t.Helper()
	require.Equal(t, wireTypeVarint, f.WireType)
	return f.Varint
}
