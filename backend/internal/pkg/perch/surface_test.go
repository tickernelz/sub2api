package perch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

const surfacePromptFloor = 205

func surfacePromptSatisfied(text string) bool {
	return len(text) >= surfacePromptFloor
}

func TestSurfacePromptSatisfiedByProviderText(t *testing.T) {
	if !surfacePromptSatisfied(surfacePromptText) {
		t.Fatalf("surface prompt is %d chars, below the %d measured floor", len(surfacePromptText), surfacePromptFloor)
	}
	short := surfacePromptText
	if len(short) > surfacePromptFloor-1 {
		short = short[:surfacePromptFloor-1]
	} else {
		short = "short"
	}
	if surfacePromptSatisfied(short) {
		t.Fatalf("predicate accepts the %d char prompt %q", len(short), short)
	}
}

func TestBuildModelCallRequestLeadsWithSurfacePrompt(t *testing.T) {
	request := apicompat.ChatCompletionsRequest{
		Model: "wandb-kimi-k2-6",
		Messages: []apicompat.ChatMessage{
			{Role: "system", Content: json.RawMessage(`"You are terse."`)},
			{Role: "user", Content: json.RawMessage(`"Reply with the single word: pong"`)},
		},
	}
	call, err := BuildModelCallRequest(&request, CallOptions{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(call.Request.Messages) != 4 {
		t.Fatalf("messages = %d, want 4", len(call.Request.Messages))
	}
	var first struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(call.Request.Messages[0], &first); err != nil {
		t.Fatalf("decode first message: %v", err)
	}
	if first.Role != "system" || first.Content != surfacePromptText {
		t.Fatalf("first message = %s/%d chars, want the surface system prompt", first.Role, len(first.Content))
	}
	if !surfacePromptSatisfied(first.Content) {
		t.Fatalf("first message is %d chars, below the %d measured floor", len(first.Content), surfacePromptFloor)
	}
	var second struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(call.Request.Messages[1], &second); err != nil || second.Role != "system" {
		t.Fatalf("caller system message not preserved at index 1: %v %s", err, second.Role)
	}
	body, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("encode call: %v", err)
	}
	if !strings.HasPrefix(string(body), `{"request":{"messages":[{"role":"system","content":`) {
		t.Fatalf("body does not lead with the surface system message: %.120s", body)
	}
}
