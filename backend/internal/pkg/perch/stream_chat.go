package perch

import (
	"errors"
	"io"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

func StreamChat(stream *ModelCallStream, model, responseID string, created int64, includeUsage bool, emit func(apicompat.ChatCompletionsChunk) error) (*Outcome, error) {
	emitter := NewChunkEmitter(model, responseID, created)
	wroteOutput := false
	send := func(chunk apicompat.ChatCompletionsChunk) error {
		wroteOutput = true
		return emit(chunk)
	}

	var settled *Outcome
	for {
		event, body, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, &APIError{Status: 502, Code: CodeStreamInterrupted, Message: "perch stream read failed"}
		}
		switch event.Type {
		case EventDone:
			settled = outcomeFromDone(body)
			continue
		case EventStreamRestart:
			if wroteOutput {
				return nil, &APIError{Status: 502, Code: CodeStreamInterrupted, Message: "perch restarted the stream after output was sent"}
			}
			emitter.ResetToolCalls()
			continue
		case EventError:
			code := firstNonEmpty(body.ErrorCode, CodeAPIError)
			return nil, &APIError{Status: 502, Code: code, Message: firstNonEmpty(body.Message, "perch stream error"), Limit: body.Limit}
		}
		for _, chunk := range emitter.Chunks(event, body) {
			if err := send(chunk); err != nil {
				return nil, err
			}
		}
	}

	if settled == nil {
		return nil, &APIError{Status: 502, Code: CodeStreamInterrupted, Message: "perch stream ended without a done event"}
	}
	if !settled.OK {
		code := firstNonEmpty(settled.ErrorCode, CodeAPIError)
		return nil, &APIError{Status: 502, Code: code, Message: settled.Error, Limit: settled.Limit}
	}
	for _, chunk := range emitter.Final(settled, includeUsage) {
		if err := send(chunk); err != nil {
			return nil, err
		}
	}
	return settled, nil
}
