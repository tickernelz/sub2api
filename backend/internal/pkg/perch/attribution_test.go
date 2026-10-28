package perch

import (
	"context"
	"testing"
)

func TestModelCallSendsAttributionFromAccount(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{})
	session := NewSession(testCredentials(server.URL), WithHTTPClient(server.Client()))
	client := NewClient(session)

	stream, err := client.OpenModelCall(context.Background(), ModelCallRequest{})
	if err != nil {
		t.Fatalf("OpenModelCall: %v", err)
	}
	defer stream.Close()

	attribution := server.lastModelReq.Attribution
	if attribution == nil {
		t.Fatalf("model call must carry attribution")
	}
	if attribution.UserID != "971cd79c-e9c6-4ceb-b775-923f4110e295" {
		t.Fatalf("attribution userId = %q", attribution.UserID)
	}
	if attribution.Workspace != "c47c7c5a-910e-4d32-b363-0f4bde58a5bc" {
		t.Fatalf("attribution workspaceId = %q", attribution.Workspace)
	}
	if attribution.Source != ClientSurfaceCLI || attribution.Lane != LaneChat {
		t.Fatalf("attribution surface = %#v", attribution)
	}
	if attribution.RunID != server.lastModelReq.RunID || attribution.RunID == "" {
		t.Fatalf("attribution runId = %q, request runId = %q", attribution.RunID, server.lastModelReq.RunID)
	}
	if server.accountHits != 1 {
		t.Fatalf("account lookups = %d, want 1", server.accountHits)
	}

	second, err := client.OpenModelCall(context.Background(), ModelCallRequest{})
	if err != nil {
		t.Fatalf("second OpenModelCall: %v", err)
	}
	defer second.Close()
	if server.accountHits != 1 {
		t.Fatalf("account lookups after second call = %d, want cached 1", server.accountHits)
	}
}

func TestModelCallSkipsAttributionWhenAccountFails(t *testing.T) {
	server := newPerchTestServer(t, perchTestOptions{AccountBody: []byte(`{"ok":false,"error":"boom"}`)})
	session := NewSession(testCredentials(server.URL), WithHTTPClient(server.Client()))
	client := NewClient(session)

	stream, err := client.OpenModelCall(context.Background(), ModelCallRequest{})
	if err != nil {
		t.Fatalf("OpenModelCall must still run without attribution: %v", err)
	}
	defer stream.Close()
	if server.lastModelReq.Attribution != nil {
		t.Fatalf("attribution must stay empty when the account lookup yields nothing: %#v", server.lastModelReq.Attribution)
	}
}
