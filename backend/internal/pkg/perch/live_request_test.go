//go:build perchlive

package perch

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

func chatRequestForLiveTest() apicompat.ChatCompletionsRequest {
	temperature := 0.6
	maxTokens := 512
	return apicompat.ChatCompletionsRequest{
		Model:       "wandb-kimi-k2-6",
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
		Messages: []apicompat.ChatMessage{
			{Role: "user", Content: json.RawMessage("\"Reply with the single word: pong\"")},
		},
	}
}
