//go:build perchlive

package perch

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLivePerchGatewayModelCall(t *testing.T) {
	_, creds := liveCredentials(t)
	client := NewGatewayClient(creds, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	request := chatRequestForLiveTest()
	call, err := BuildModelCallRequest(&request, CallOptions{})
	if err != nil {
		t.Fatalf("BuildModelCallRequest: %v", err)
	}
	startedAt := time.Now()
	outcome, err := client.CallModel(ctx, call)
	if err != nil {
		t.Fatalf("CallModel: %v", err)
	}
	t.Logf("text=%q upstream_model=%s provider=%s duration_ms=%d wall_ms=%d",
		strings.TrimSpace(outcome.Text), outcome.Model, outcome.Provider,
		outcome.DurationMs, time.Since(startedAt).Milliseconds())
}
