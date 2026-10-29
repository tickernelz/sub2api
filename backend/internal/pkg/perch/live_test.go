//go:build perchlive

package perch

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func liveCredentials(t *testing.T) (string, Credentials) {
	t.Helper()
	path := strings.TrimSpace(os.Getenv("PERCH_SESSION_FILE"))
	if path == "" {
		path = DefaultSessionPath()
	}
	creds, err := LoadCredentialsFile(path)
	if err != nil {
		t.Skipf("no live Perch session: %v", err)
	}
	return path, creds
}

func TestLivePerchHealth(t *testing.T) {
	_, creds := liveCredentials(t)
	session := NewSession(creds)
	client := NewClient(session)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	health, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !health.OK {
		t.Skipf("account is not healthy: %+v", health)
	}
	t.Logf("plan=%s email=%s workspace=%s restricted=%v models=%v", health.Plan, health.Email, health.Workspace, health.Restricted, health.Models)
}

func TestLivePerchModelCall(t *testing.T) {
	path, creds := liveCredentials(t)
	session := NewSession(creds, WithCredentialsPersist(func(next Credentials) {
		if err := SaveCredentialsFile(path, next); err != nil {
			t.Logf("persist refreshed session: %v", err)
		}
	}))
	client := NewClient(session)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	account, accountErr := client.Account(ctx)
	t.Logf("account lookup: err=%v userId=%q workspaceId=%q", accountErr, account.UserID, account.WorkspaceID)
	if accountErr != nil {
		t.Fatalf("account lookup: %v", accountErr)
	}
	attribution := client.attribution(ctx, "run-live-probe", LaneChat)
	if attribution == nil {
		t.Fatalf("adapter would send no attribution")
	}
	t.Logf("attribution that the model call carries: userId=%q workspaceId=%q source=%q", attribution.UserID, attribution.Workspace, attribution.Source)

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
	if strings.TrimSpace(outcome.Text) == "" {
		t.Fatalf("perch returned empty text: %+v", outcome)
	}
	if outcome.Model == "" {
		t.Fatalf("perch did not report a model: %+v", outcome)
	}
	if outcome.Usage == nil || outcome.Usage.PromptTokens() <= 0 || outcome.Usage.CompletionTokens() <= 0 {
		t.Fatalf("perch did not report usage: %+v", outcome.Usage)
	}
	t.Logf("text=%q upstream_model=%s provider=%s duration_ms=%d wall_ms=%d usage=%+v",
		strings.TrimSpace(outcome.Text),
		outcome.Model,
		outcome.Provider,
		outcome.DurationMs,
		time.Since(startedAt).Milliseconds(),
		*outcome.Usage,
	)
}
