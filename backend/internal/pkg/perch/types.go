package perch

import (
	"encoding/json"
	"strings"
)

const (
	LaneChat         = "chat"
	ClientSurfaceCLI = "cli"
	DefaultProfile   = "standard"
	DefaultLane      = LaneChat
)

const (
	EventAnswerDelta      = "answer_delta"
	EventReasoningDelta   = "reasoning_delta"
	EventToolCallDelta    = "tool_call_delta"
	EventToolUseEnd       = "tool_use_end"
	EventRetrying         = "retrying"
	EventStreamRestart    = "stream_restart"
	EventContinuationSeam = "continuation_seam"
	EventModelCallFailed  = "model_call_failed"
	EventDone             = "done"
	EventError            = "error"
)

// ModelCallRequest is the wire body for POST /api/perch-terminal/model-call.
type ModelCallRequest struct {
	Request             ChatRequest  `json:"request"`
	RunID               string       `json:"runId"`
	Lane                string       `json:"lane"`
	StrictManual        bool         `json:"strictManual"`
	PreferredModelID    string       `json:"preferredModelId,omitempty"`
	RoostModelChoice    string       `json:"roostModelChoice,omitempty"`
	ManualModelOptionID string       `json:"manualModelOptionId,omitempty"`
	AvoidModelIDs       []string     `json:"avoidModelIds"`
	Attribution         *Attribution `json:"attribution,omitempty"`
	ClientSurface       string       `json:"clientSurface,omitempty"`
}

// ChatRequest carries the conversation the Perch router should answer.
type ChatRequest struct {
	Messages    []json.RawMessage `json:"messages"`
	Tools       []json.RawMessage `json:"tools,omitempty"`
	Temperature *float64          `json:"temperature,omitempty"`
	MaxTokens   *int              `json:"maxTokens,omitempty"`
	ToolChoice  json.RawMessage   `json:"toolChoice,omitempty"`
}

// Attribution identifies the Perch user, workspace and run behind a model call.
type Attribution struct {
	UserID    string `json:"userId,omitempty"`
	Workspace string `json:"workspaceId,omitempty"`
	RunID     string `json:"runId,omitempty"`
	Lane      string `json:"lane,omitempty"`
	Source    string `json:"source,omitempty"`
}

// Usage is the token accounting Perch returns on the done event.
type Usage struct {
	InputTokens              int `json:"inputTokens"`
	OutputTokens             int `json:"outputTokens"`
	CacheCreationInputTokens int `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int `json:"cacheReadInputTokens"`
	TotalTokens              int `json:"totalTokens"`
}

// PromptTokens is the full input side including cache reads and cache writes.
func (u *Usage) PromptTokens() int {
	if u == nil {
		return 0
	}
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// CompletionTokens is the generated token count.
func (u *Usage) CompletionTokens() int {
	if u == nil {
		return 0
	}
	return u.OutputTokens
}

// Event is one decoded SSE payload from the Perch model proxy.
type Event struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

type eventBody struct {
	Type         string         `json:"type"`
	Text         string         `json:"text"`
	Message      string         `json:"message"`
	Ok           *bool          `json:"ok"`
	Content      []ContentBlock `json:"content"`
	ToolCalls    []ToolCall     `json:"toolCalls"`
	FinishReason string         `json:"finishReason"`
	Provider     string         `json:"provider"`
	Model        string         `json:"model"`
	DurationMs   int64          `json:"durationMs"`
	ContextFill  int            `json:"contextFillTokens"`
	ContextInput int            `json:"contextInputTokens"`
	Usage        *Usage         `json:"usage"`
	Error        string         `json:"error"`
	ErrorCode    string         `json:"errorCode"`
	Limit        *Limit         `json:"limit"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Outcome is the settled result of one model call, whether streamed or not.
type Outcome struct {
	OK           bool
	Text         string
	Reasoning    string
	Content      []ContentBlock
	ToolCalls    []ToolCall
	FinishReason string
	Provider     string
	Model        string
	DurationMs   int64
	ContextFill  int
	ContextInput int
	Usage        *Usage
	Error        string
	ErrorCode    string
	Limit        *Limit
}

func decodeEvent(raw json.RawMessage) (Event, *eventBody, error) {
	var body eventBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return Event{}, nil, err
	}
	return Event{Type: strings.TrimSpace(body.Type), Raw: raw}, &body, nil
}

// IsTerminal reports whether the event settles the call.
func (e Event) IsTerminal() bool {
	return e.Type == EventDone || e.Type == EventError
}

// Ticket is a short-lived authorisation for one Perch run.
type Ticket struct {
	Ticket    string `json:"ticket"`
	TicketID  string `json:"ticketId"`
	RunID     string `json:"runId"`
	Profile   string `json:"profile"`
	ExpiresAt string `json:"expiresAt"`
	Enforced  bool   `json:"enforced"`
}

// AccountInfo is the authenticated Perch account projection.
type AccountInfo struct {
	Email        string        `json:"email"`
	Name         string        `json:"name"`
	UserID       string        `json:"userId"`
	WorkspaceID  string        `json:"workspaceId"`
	OrgID        string        `json:"orgId"`
	PlanCode     string        `json:"planCode"`
	PlanName     string        `json:"planName"`
	Entitlements []Entitlement `json:"entitlements"`
}

// Entitlement is one feature flag Perch returns for the signed-in account.
type Entitlement struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value_json"`
}

type Health struct {
	OK         bool
	Plan       string
	Email      string
	Workspace  string
	Models     []string
	Restricted bool
}
