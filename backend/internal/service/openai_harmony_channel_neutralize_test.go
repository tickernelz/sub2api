package service

import (
	"bytes"
	"testing"
)

func TestNeutralizeOpenAIHarmonyChannelToken(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantChanged bool
		want        string
	}{
		{
			name:        "glued analysis is neutralized",
			in:          `{"input":"<|channel|>analysis"}`,
			wantChanged: true,
			want:        "{\"input\":\"<\uff5cchannel\uff5c>analysis\"}",
		},
		{
			name:        "spaced analysis is neutralized",
			in:          `{"input":"<|channel|> analysis"}`,
			wantChanged: true,
			want:        "{\"input\":\"<\uff5cchannel\uff5c> analysis\"}",
		},
		{
			name:        "multiple occurrences all neutralized",
			in:          "<|channel|>analysis and again <|channel|>analysis",
			wantChanged: true,
			want:        "<\uff5cchannel\uff5c>analysis and again <\uff5cchannel\uff5c>analysis",
		},
		{
			name:        "token without analysis is still neutralized (idempotent safety)",
			in:          "prefix <|channel|> suffix",
			wantChanged: true,
			want:        "prefix <\uff5cchannel\uff5c> suffix",
		},
		{
			name:        "no token is a no-op",
			in:          `{"input":"just a normal review of channel analysis"}`,
			wantChanged: false,
			want:        `{"input":"just a normal review of channel analysis"}`,
		},
		{
			name:        "other harmony tokens untouched",
			in:          "<|start|>assistant<|message|>hi<|end|>",
			wantChanged: false,
			want:        "<|start|>assistant<|message|>hi<|end|>",
		},
		{
			name:        "empty body is a no-op",
			in:          "",
			wantChanged: false,
			want:        "",
		},
		{
			name:        "already-neutralized body is a no-op (idempotent)",
			in:          "<\uff5cchannel\uff5c>analysis",
			wantChanged: false,
			want:        "<\uff5cchannel\uff5c>analysis",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, changed := neutralizeOpenAIHarmonyChannelToken([]byte(tc.in))
			if changed != tc.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, tc.wantChanged)
			}
			if string(out) != tc.want {
				t.Fatalf("out = %q, want %q", string(out), tc.want)
			}
		})
	}
}

func TestNeutralizeOpenAIHarmonyChannelTokenNoAliasingOnNoOp(t *testing.T) {
	in := []byte("no token here")
	out, changed := neutralizeOpenAIHarmonyChannelToken(in)
	if changed {
		t.Fatalf("expected no change")
	}
	// On no-op the same backing array is returned (zero-allocation contract).
	if &out[0] != &in[0] {
		t.Fatalf("expected no-op to return the same backing slice")
	}
}

func TestNeutralizeOpenAIHarmonyChannelTokenDoesNotMutateInput(t *testing.T) {
	in := []byte("<|channel|>analysis")
	original := append([]byte(nil), in...)
	out, changed := neutralizeOpenAIHarmonyChannelToken(in)
	if !changed {
		t.Fatalf("expected change")
	}
	if !bytes.Equal(in, original) {
		t.Fatalf("input slice was mutated: got %q want %q", string(in), string(original))
	}
	if bytes.Equal(out, in) {
		t.Fatalf("output should differ from input")
	}
}

func TestDetectOpenAIInvalidPrompt(t *testing.T) {
	cases := []struct {
		name        string
		payload     string
		wantHit     bool
		wantMessage string
	}{
		{
			name:        "flat error.code invalid_prompt",
			payload:     `{"error":{"code":"invalid_prompt","type":"invalid_request_error","message":"Request blocked."}}`,
			wantHit:     true,
			wantMessage: "Request blocked.",
		},
		{
			name:        "nested response.error.code invalid_prompt (stream response.failed shape)",
			payload:     `{"type":"response.failed","response":{"id":"resp_x","status":"failed","error":{"code":"invalid_prompt","message":"Request blocked."}}}`,
			wantHit:     true,
			wantMessage: "Request blocked.",
		},
		{
			name:        "case-insensitive code",
			payload:     `{"error":{"code":"INVALID_PROMPT","message":"nope"}}`,
			wantHit:     true,
			wantMessage: "nope",
		},
		{
			name:    "cyber_policy is not invalid_prompt",
			payload: `{"error":{"code":"cyber_policy","message":"blocked"}}`,
			wantHit: false,
		},
		{
			name:    "upstream_error is not invalid_prompt",
			payload: `{"type":"response.failed","response":{"error":{"code":"upstream_error","message":"input exceeds the context window"}}}`,
			wantHit: false,
		},
		{
			name:    "no error code",
			payload: `{"type":"response.completed","response":{"status":"completed"}}`,
			wantHit: false,
		},
		{
			name:    "empty payload",
			payload: "",
			wantHit: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hit, code, msg := detectOpenAIInvalidPrompt([]byte(tc.payload))
			if hit != tc.wantHit {
				t.Fatalf("hit = %v, want %v", hit, tc.wantHit)
			}
			if !tc.wantHit {
				return
			}
			if code != "invalid_prompt" {
				t.Fatalf("code = %q, want invalid_prompt", code)
			}
			if msg != tc.wantMessage {
				t.Fatalf("message = %q, want %q", msg, tc.wantMessage)
			}
		})
	}
}
