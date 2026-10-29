//go:build perchlive

package perch

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func liveCredentials(t *testing.T) (string, Credentials) {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("PERCH_SESSION_JSON"))
	if raw == "" {
		t.Skip("no live Perch session: set PERCH_SESSION_JSON")
	}
	var creds Credentials
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		t.Skipf("PERCH_SESSION_JSON is not a perch session: %v", err)
	}
	if err := creds.Validate(); err != nil {
		t.Skipf("PERCH_SESSION_JSON is incomplete: %v", err)
	}
	return strings.TrimSpace(os.Getenv("PERCH_SESSION_PATH")), creds
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
	_, creds := liveCredentials(t)
	session := NewSession(creds, WithCredentialsPersist(func(next Credentials) {
		t.Logf("session rotated in memory: has_access=%v has_refresh=%v expires_at=%d", next.AccessToken != "", next.RefreshToken != "", next.ExpiresAt)
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
