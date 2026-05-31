package cursorproto

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/google/uuid"
)

const (
	connectFlagNone byte = 0x00
	connectFlagGzip byte = 0x01
)

type wireType int

const (
	wireTypeVarint  wireType = 0
	wireTypeFixed64 wireType = 1
	wireTypeLen     wireType = 2
	wireTypeFixed32 wireType = 5
)

type field struct {
	Number   int
	WireType wireType
	Varint   uint64
	Bytes    []byte
}

type ConnectFrame struct {
	Flags   byte
	Payload []byte
}

func WrapConnectFrame(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	frame[0] = connectFlagNone
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}

func ReadConnectFrame(r io.Reader) (ConnectFrame, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ConnectFrame{}, err
		}
		return ConnectFrame{}, err
	}
	length := binary.BigEndian.Uint32(header[1:5])
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(r, payload); err != nil {
		return ConnectFrame{}, err
	}
	flags := header[0]
	if flags&connectFlagGzip != 0 {
		zr, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return ConnectFrame{}, err
		}
		decompressed, err := io.ReadAll(zr)
		closeErr := zr.Close()
		if err != nil {
			return ConnectFrame{}, err
		}
		if closeErr != nil {
			return ConnectFrame{}, closeErr
		}
		payload = decompressed
	}
	return ConnectFrame{Flags: flags, Payload: payload}, nil
}

type AgentRunInput struct {
	ModelID        string
	UserText       string
	ConversationID string
	MessageID      string
	SystemPrompt   string
	BlobStore      map[string][]byte
}

type RequestedModel struct {
	ModelID    string
	Parameters []ModelParameter
}

type ModelParameter struct {
	ID    string
	Value string
}

func ResolveRequestedModel(modelID string) RequestedModel {
	modelID = strings.TrimSpace(modelID)
	if modelID == "auto" {
		return RequestedModel{ModelID: "default"}
	}
	if strings.HasPrefix(modelID, "composer-") && strings.HasSuffix(modelID, "-fast") {
		return RequestedModel{
			ModelID: strings.TrimSuffix(modelID, "-fast"),
			Parameters: []ModelParameter{{
				ID:    "fast",
				Value: "true",
			}},
		}
	}
	return RequestedModel{ModelID: modelID}
}

func EncodeAgentRunRequest(input AgentRunInput) ([]byte, error) {
	conversationID := strings.TrimSpace(input.ConversationID)
	if conversationID == "" {
		conversationID = uuid.NewString()
	}
	messageID := strings.TrimSpace(input.MessageID)
	if messageID == "" {
		messageID = uuid.NewString()
	}
	requested := ResolveRequestedModel(input.ModelID)
	if requested.ModelID == "" {
		return nil, fmt.Errorf("cursor run request: model is required")
	}

	userMessage := encodeMessage(1,
		encodeString(1, input.UserText),
		encodeString(2, messageID),
		encodeMessage(3),
		encodeVarintField(4, 1),
	)
	action := encodeMessage(2,
		encodeMessage(1, userMessage),
	)

	var conversationStateParts [][]byte
	if input.SystemPrompt != "" && input.BlobStore != nil {
		blob := []byte(fmt.Sprintf(`{"role":"system","content":%q}`, input.SystemPrompt))
		sum := sha256.Sum256(blob)
		input.BlobStore[fmt.Sprintf("%x", sum[:])] = blob
		conversationStateParts = append(conversationStateParts, encodeBytes(1, sum[:]))
	}
	conversationState := encodeMessage(1, conversationStateParts...)

	modelParts := [][]byte{encodeString(1, requested.ModelID)}
	for _, param := range requested.Parameters {
		modelParts = append(modelParts, encodeMessage(3,
			encodeString(1, param.ID),
			encodeString(2, param.Value),
		))
	}
	requestedModel := encodeMessage(9, modelParts...)

	runRequest := [][]byte{
		conversationState,
		action,
		encodeMessage(4),
		encodeString(5, conversationID),
		requestedModel,
		encodeVarintField(12, 0),
		encodeString(16, conversationID),
	}
	return encodeMessage(1, runRequest...), nil
}

func BuildAgentRequestBody(input AgentRunInput) ([]byte, error) {
	payload, err := EncodeAgentRunRequest(input)
	if err != nil {
		return nil, err
	}
	return WrapConnectFrame(payload), nil
}

type ServerEventKind string

const (
	ServerEventText             ServerEventKind = "text"
	ServerEventThinking         ServerEventKind = "thinking"
	ServerEventThinkingComplete ServerEventKind = "thinking_complete"
	ServerEventTokenDelta       ServerEventKind = "token_delta"
	ServerEventTurnEnded        ServerEventKind = "turn_ended"
	ServerEventHeartbeat        ServerEventKind = "heartbeat"
	ServerEventToolCallStarted  ServerEventKind = "tool_call_started"
	ServerEventToolCallComplete ServerEventKind = "tool_call_completed"
	ServerEventKVServerMessage  ServerEventKind = "kv_server_message"
	ServerEventUnknown          ServerEventKind = "unknown"
)

type ServerEvent struct {
	Kind   ServerEventKind
	Text   string
	Tokens int
	Field  int
}

type ExecEventKind string

const (
	ExecEventRequestContext ExecEventKind = "exec_request_context"
	ExecEventUnsupported    ExecEventKind = "exec_unsupported"
)

type ExecServerEvent struct {
	Kind              ExecEventKind
	ID                uint64
	ExecID            string
	ResultFieldNumber int
	Path              string
	Command           string
	WorkingDir        string
	URL               string
}

func DecodeExecServerEvent(payload []byte) (*ExecServerEvent, error) {
	fields, err := decodeFields(payload)
	if err != nil {
		return nil, err
	}
	for _, top := range fields {
		if top.Number != 2 || top.WireType != wireTypeLen {
			continue
		}
		execFields, err := decodeFields(top.Bytes)
		if err != nil {
			return nil, err
		}
		var event ExecServerEvent
		var hasRequestContext bool
		for _, f := range execFields {
			switch {
			case f.Number == 1 && f.WireType == wireTypeVarint:
				event.ID = f.Varint
			case f.Number == 15 && f.WireType == wireTypeLen:
				event.ExecID = string(f.Bytes)
			case f.Number == 10 && f.WireType == wireTypeLen:
				hasRequestContext = true
			case isUnsupportedExecArgsField(f.Number) && f.WireType == wireTypeLen:
				event.Kind = ExecEventUnsupported
				event.ResultFieldNumber = f.Number
				populateUnsupportedExecDetails(&event, f.Bytes)
			}
		}
		if hasRequestContext {
			event.Kind = ExecEventRequestContext
			return &event, nil
		}
		if event.Kind == ExecEventUnsupported {
			return &event, nil
		}
	}
	return nil, nil
}

func isUnsupportedExecArgsField(number int) bool {
	switch number {
	case 2, 3, 4, 5, 7, 8, 9, 11, 14, 16, 20, 23:
		return true
	default:
		return false
	}
}

func populateUnsupportedExecDetails(event *ExecServerEvent, payload []byte) {
	fields, err := decodeFields(payload)
	if err != nil {
		return
	}
	for _, f := range fields {
		if f.WireType != wireTypeLen {
			continue
		}
		switch event.ResultFieldNumber {
		case 2, 14, 16:
			switch f.Number {
			case 1:
				event.Command = string(f.Bytes)
			case 2:
				event.WorkingDir = string(f.Bytes)
			}
		case 20:
			if f.Number == 1 {
				event.URL = string(f.Bytes)
			}
		default:
			if f.Number == 1 {
				event.Path = string(f.Bytes)
			}
		}
	}
}

func EncodeUnsupportedExecResult(event ExecServerEvent, reason string) []byte {
	if reason == "" {
		reason = "Cursor built-in tools are not supported"
	}
	resultFieldNumber := event.ResultFieldNumber
	if resultFieldNumber == 0 {
		resultFieldNumber = 11
	}

	var resultPayload []byte
	switch resultFieldNumber {
	case 2, 14, 16:
		resultPayload = encodeMessage(2,
			encodeString(1, event.Command),
			encodeString(2, event.WorkingDir),
			encodeString(3, reason),
		)
	case 3, 4, 7, 8:
		resultPayload = encodeMessage(2,
			encodeString(1, event.Path),
			encodeString(2, reason),
		)
	case 9:
		resultPayload = nil
	case 11:
		resultPayload = encodeMessage(2, encodeString(1, reason))
	case 20:
		resultPayload = encodeMessage(2,
			encodeString(1, event.URL),
			encodeString(2, reason),
		)
	default:
		resultPayload = encodeMessage(2, encodeString(1, reason))
	}

	execMessage := encodeMessage(2,
		encodeVarintField(1, event.ID),
		encodeString(15, event.ExecID),
		encodeMessage(resultFieldNumber, resultPayload),
	)
	return WrapConnectFrame(execMessage)
}

func DecodeAgentServerMessage(payload []byte) ([]ServerEvent, error) {
	fields, err := decodeFields(payload)
	if err != nil {
		return nil, err
	}
	out := make([]ServerEvent, 0, len(fields))
	for _, top := range fields {
		if top.Number == 4 && top.WireType == wireTypeLen {
			out = append(out, ServerEvent{Kind: ServerEventKVServerMessage})
			continue
		}
		if top.Number != 1 || top.WireType != wireTypeLen {
			continue
		}
		updates, err := decodeFields(top.Bytes)
		if err != nil {
			return nil, err
		}
		for _, update := range updates {
			switch update.Number {
			case 1:
				if update.WireType == wireTypeLen {
					text, err := decodeNestedString(update.Bytes, 1)
					if err != nil {
						return nil, err
					}
					out = append(out, ServerEvent{Kind: ServerEventText, Text: text})
				}
			case 4:
				if update.WireType == wireTypeLen {
					text, err := decodeNestedString(update.Bytes, 1)
					if err != nil {
						return nil, err
					}
					out = append(out, ServerEvent{Kind: ServerEventThinking, Text: text})
				}
			case 5:
				out = append(out, ServerEvent{Kind: ServerEventThinkingComplete})
			case 2:
				out = append(out, ServerEvent{Kind: ServerEventToolCallStarted})
			case 3:
				out = append(out, ServerEvent{Kind: ServerEventToolCallComplete})
			case 8:
				if update.WireType == wireTypeLen {
					tokens, err := decodeNestedVarint(update.Bytes, 1)
					if err != nil {
						return nil, err
					}
					out = append(out, ServerEvent{Kind: ServerEventTokenDelta, Tokens: int(tokens)})
				}
			case 13:
				out = append(out, ServerEvent{Kind: ServerEventHeartbeat})
			case 14:
				out = append(out, ServerEvent{Kind: ServerEventTurnEnded})
			default:
				out = append(out, ServerEvent{Kind: ServerEventUnknown, Field: update.Number})
			}
		}
	}
	return out, nil
}

type KvEventKind string

const (
	KvEventGetBlob KvEventKind = "kv_get_blob"
	KvEventSetBlob KvEventKind = "kv_set_blob"
)

type KvServerEvent struct {
	Kind            KvEventKind
	ID              uint64
	BlobID          []byte
	BlobData        []byte
	RequestMetadata []byte
}

func DecodeKvServerEvent(payload []byte) (*KvServerEvent, error) {
	fields, err := decodeFields(payload)
	if err != nil {
		return nil, err
	}
	for _, top := range fields {
		if top.Number != 4 || top.WireType != wireTypeLen {
			continue
		}
		kvFields, err := decodeFields(top.Bytes)
		if err != nil {
			return nil, err
		}
		var event KvServerEvent
		var getArgs []byte
		var setArgs []byte
		for _, f := range kvFields {
			switch {
			case f.Number == 1 && f.WireType == wireTypeVarint:
				event.ID = f.Varint
			case f.Number == 2 && f.WireType == wireTypeLen:
				getArgs = f.Bytes
			case f.Number == 3 && f.WireType == wireTypeLen:
				setArgs = f.Bytes
			case f.Number == 4 && f.WireType == wireTypeLen:
				event.RequestMetadata = append([]byte(nil), f.Bytes...)
			}
		}
		if getArgs != nil {
			args, err := decodeFields(getArgs)
			if err != nil {
				return nil, err
			}
			event.Kind = KvEventGetBlob
			for _, f := range args {
				if f.Number == 1 && f.WireType == wireTypeLen {
					event.BlobID = append([]byte(nil), f.Bytes...)
				}
			}
			return &event, nil
		}
		if setArgs != nil {
			args, err := decodeFields(setArgs)
			if err != nil {
				return nil, err
			}
			event.Kind = KvEventSetBlob
			for _, f := range args {
				switch {
				case f.Number == 1 && f.WireType == wireTypeLen:
					event.BlobID = append([]byte(nil), f.Bytes...)
				case f.Number == 2 && f.WireType == wireTypeLen:
					event.BlobData = append([]byte(nil), f.Bytes...)
				}
			}
			return &event, nil
		}
	}
	return nil, nil
}

func EncodeKvGetBlobResult(id uint64, blobData, requestMetadata []byte) []byte {
	parts := [][]byte{}
	if id != 0 {
		parts = append(parts, encodeVarintField(1, id))
	}
	parts = append(parts, encodeMessage(2, encodeBytes(1, blobData)))
	if len(requestMetadata) > 0 {
		parts = append(parts, encodeBytes(4, requestMetadata))
	}
	return WrapConnectFrame(encodeMessage(3, parts...))
}

func EncodeKvSetBlobResult(id uint64, requestMetadata []byte) []byte {
	parts := [][]byte{}
	if id != 0 {
		parts = append(parts, encodeVarintField(1, id))
	}
	parts = append(parts, encodeMessage(3))
	if len(requestMetadata) > 0 {
		parts = append(parts, encodeBytes(4, requestMetadata))
	}
	return WrapConnectFrame(encodeMessage(3, parts...))
}

func EncodeRequestContextResponse(id uint64, execID string) []byte {
	requestContext := encodeMessage(1)
	success := encodeMessage(1, requestContext)
	execMessage := encodeMessage(2,
		encodeVarintField(1, id),
		encodeString(15, execID),
		encodeMessage(10, success),
	)
	return WrapConnectFrame(execMessage)
}

func decodeNestedString(payload []byte, number int) (string, error) {
	fields, err := decodeFields(payload)
	if err != nil {
		return "", err
	}
	for _, f := range fields {
		if f.Number == number && f.WireType == wireTypeLen {
			return string(f.Bytes), nil
		}
	}
	return "", nil
}

func decodeNestedVarint(payload []byte, number int) (uint64, error) {
	fields, err := decodeFields(payload)
	if err != nil {
		return 0, err
	}
	for _, f := range fields {
		if f.Number == number && f.WireType == wireTypeVarint {
			return f.Varint, nil
		}
	}
	return 0, nil
}

func encodeTag(number int, wt wireType) []byte {
	return encodeVarint(uint64(number<<3) | uint64(wt))
}

func encodeVarint(value uint64) []byte {
	if value == 0 {
		return []byte{0}
	}
	out := make([]byte, 0, 10)
	for value > 0x7f {
		out = append(out, byte(value&0x7f)|0x80)
		value >>= 7
	}
	out = append(out, byte(value))
	return out
}

func encodeVarintField(number int, value uint64) []byte {
	out := encodeTag(number, wireTypeVarint)
	out = append(out, encodeVarint(value)...)
	return out
}

func encodeBytes(number int, value []byte) []byte {
	out := encodeTag(number, wireTypeLen)
	out = append(out, encodeVarint(uint64(len(value)))...)
	out = append(out, value...)
	return out
}

func encodeString(number int, value string) []byte {
	return encodeBytes(number, []byte(value))
}

func encodeMessage(number int, parts ...[]byte) []byte {
	size := 0
	for _, part := range parts {
		size += len(part)
	}
	body := make([]byte, 0, size)
	for _, part := range parts {
		body = append(body, part...)
	}
	return encodeBytes(number, body)
}

func decodeFields(payload []byte) ([]field, error) {
	fields := make([]field, 0)
	for pos := 0; pos < len(payload); {
		tag, next, err := decodeVarint(payload, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		fieldNumber := int(tag >> 3)
		wt := wireType(tag & 0x7)
		if fieldNumber <= 0 {
			return nil, fmt.Errorf("invalid protobuf field number %d", fieldNumber)
		}
		f := field{Number: fieldNumber, WireType: wt}
		switch wt {
		case wireTypeVarint:
			v, next, err := decodeVarint(payload, pos)
			if err != nil {
				return nil, err
			}
			f.Varint = v
			pos = next
		case wireTypeLen:
			length, next, err := decodeVarint(payload, pos)
			if err != nil {
				return nil, err
			}
			pos = next
			if length > uint64(len(payload)-pos) {
				return nil, io.ErrUnexpectedEOF
			}
			f.Bytes = append([]byte(nil), payload[pos:pos+int(length)]...)
			pos += int(length)
		case wireTypeFixed64:
			if len(payload)-pos < 8 {
				return nil, io.ErrUnexpectedEOF
			}
			f.Bytes = append([]byte(nil), payload[pos:pos+8]...)
			pos += 8
		case wireTypeFixed32:
			if len(payload)-pos < 4 {
				return nil, io.ErrUnexpectedEOF
			}
			f.Bytes = append([]byte(nil), payload[pos:pos+4]...)
			pos += 4
		default:
			return nil, fmt.Errorf("unsupported protobuf wire type %d", wt)
		}
		fields = append(fields, f)
	}
	return fields, nil
}

func decodeVarint(payload []byte, offset int) (uint64, int, error) {
	var out uint64
	for shift := 0; shift < 64; shift += 7 {
		if offset >= len(payload) {
			return 0, offset, io.ErrUnexpectedEOF
		}
		b := payload[offset]
		offset++
		out |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return out, offset, nil
		}
	}
	return 0, offset, fmt.Errorf("varint overflow: max=%d", uint64(math.MaxUint64))
}
