//go:build perchlive

package perch

import (
	"context"
	"testing"
	"time"
)

func TestLiveGatewayModelCall(t *testing.T) {
	_, creds := liveCredentials(t)
	client := NewGatewayClient(creds, nil)
	call, err := BuildModelCallRequest(testChatRequest(), CallOptions{DisableTurnContext: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stream, err := client.OpenModelCall(ctx, call)
	if err != nil {
		t.Fatalf("OpenModelCall: %v", err)
	}
	defer func() { _ = stream.Close() }()
	outcome, err := CollectOutcome(stream)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	t.Logf("ok=%v model=%s textLen=%d", outcome.OK, outcome.Model, len(outcome.Text))
}
