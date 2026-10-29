package perch

import (
	"bufio"
	"io"
	"strings"
)

// SSEStream decodes Perch server-sent events from a response body.
type SSEStream struct {
	scanner *bufio.Scanner
	done    bool
}

func NewSSEStream(body io.Reader) *SSEStream {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return &SSEStream{scanner: scanner}
}

// Next returns the next data payload; io.EOF ends the stream.
func (s *SSEStream) Next() ([]byte, error) {
	if s.done {
		return nil, io.EOF
	}
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		return []byte(payload), nil
	}
	s.done = true
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}
