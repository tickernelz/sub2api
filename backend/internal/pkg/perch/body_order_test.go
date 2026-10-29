package perch

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func orderedKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		t.Fatalf("expected object, got %v", token)
	}
	keys := make([]string, 0, 12)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			t.Fatalf("read key: %v", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			t.Fatalf("object key is not a string: %v", keyToken)
		}
		keys = append(keys, key)
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			t.Fatalf("skip value: %v", err)
		}
	}
	return keys
}

func TestModelCallBodyKeyOrderMatchesRecordedCLI(t *testing.T) {
	call, err := BuildModelCallRequest(testChatRequest(), CallOptions{RunID: "tkt-cli-capture"})
	if err != nil {
		t.Fatalf("BuildModelCallRequest: %v", err)
	}
	raw, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	wantTop := []string{"request", "runId", "lane", "strictManual", "preferredModelId", "avoidModelIds", "attribution", "clientSurface", "resendAfterOutput"}
	gotTop := orderedKeys(t, raw)
	if len(gotTop) < len(wantTop) {
		t.Fatalf("top-level keys = %v, want at least %v", gotTop, wantTop)
	}
	if strings.Join(gotTop[:len(wantTop)], ",") != strings.Join(wantTop, ",") {
		t.Fatalf("top-level key order = %v, want %v", gotTop, wantTop)
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	wantRequest := []string{"messages", "tools", "temperature", "maxTokens", "toolChoice"}
	if got := orderedKeys(t, envelope["request"]); strings.Join(got, ",") != strings.Join(wantRequest, ",") {
		t.Fatalf("request key order = %v, want %v", got, wantRequest)
	}
}
