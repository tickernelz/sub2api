package domain

import (
	"strings"
	"testing"
)

func TestDefaultAntigravityModelMapping_IsCuratedToSmokePassingModels(t *testing.T) {
	t.Parallel()

	expected := map[string]string{
		"claude-opus-4-6-thinking":    "claude-opus-4-6-thinking",
		"claude-sonnet-4-6":           "claude-sonnet-4-6",
		"gemini-2.5-flash":            "gemini-2.5-flash",
		"gemini-2.5-flash-lite":       "gemini-2.5-flash-lite",
		"gemini-2.5-flash-thinking":   "gemini-2.5-flash-thinking",
		"gemini-3-flash":              "gemini-3-flash",
		"gemini-3-flash-agent":        "gemini-3-flash-agent",
		"gemini-3.1-flash-image":      "gemini-3.1-flash-image",
		"gemini-3.1-flash-lite":       "gemini-3.1-flash-lite",
		"gemini-3.1-pro-low":          "gemini-3.1-pro-low",
		"gemini-3.5-flash-extra-low":  "gemini-3.5-flash-extra-low",
		"gemini-3.5-flash-low":        "gemini-3.5-flash-low",
		"gemini-pro-agent":            "gemini-pro-agent",
		"gpt-oss-120b-medium":         "gpt-oss-120b-medium",
		"tab_flash_lite_preview":      "tab_flash_lite_preview",
		"tab_jump_flash_lite_preview": "tab_jump_flash_lite_preview",
	}

	if len(DefaultAntigravityModelMapping) != len(expected) {
		t.Fatalf("expected %d curated Antigravity mappings, got %d: %v", len(expected), len(DefaultAntigravityModelMapping), DefaultAntigravityModelMapping)
	}
	for from, want := range expected {
		if got := DefaultAntigravityModelMapping[from]; got != want {
			t.Fatalf("unexpected Antigravity mapping for %q: got %q want %q", from, got, want)
		}
	}

	for _, model := range []string{
		"chat_20706",
		"gemini-2.5-pro",
		"gemini-2.5-flash-image",
		"gemini-2.5-flash-image-preview",
		"gemini-3-pro-high",
		"gemini-3-pro-low",
		"gemini-3-pro-preview",
		"gemini-3.1-pro-high",
		"gemini-3.1-pro-preview",
		"gemini-3-pro-image",
		"gemini-3-pro-image-preview",
		"gemini-3.1-flash-image-preview",
		"claude-opus-4-7",
		"claude-opus-4-6",
		"claude-opus-4-5-thinking",
		"claude-sonnet-4-5",
		"claude-sonnet-4-5-thinking",
		"claude-haiku-4-5",
	} {
		if _, ok := DefaultAntigravityModelMapping[model]; ok {
			t.Fatalf("did not expect redundant or non-smoke-passing Antigravity model %q in default mapping", model)
		}
	}
}

func TestDefaultKiroModelMapping_MatchesKiroReferenceModels(t *testing.T) {
	t.Parallel()

	expected := map[string]string{
		"claude-opus-4-7":                     "claude-opus-4.7",
		"claude-opus-4-7-thinking":            "claude-opus-4.7",
		"claude-opus-4-6":                     "claude-opus-4.6",
		"claude-opus-4-6-thinking":            "claude-opus-4.6",
		"claude-sonnet-4-6":                   "claude-sonnet-4.6",
		"claude-sonnet-4-6-thinking":          "claude-sonnet-4.6",
		"claude-opus-4-5-20251101":            "claude-opus-4.5",
		"claude-opus-4-5-20251101-thinking":   "claude-opus-4.5",
		"claude-sonnet-4-5-20250929":          "claude-sonnet-4.5",
		"claude-sonnet-4-5-20250929-thinking": "claude-sonnet-4.5",
		"claude-haiku-4-5-20251001":           "claude-haiku-4.5",
		"claude-haiku-4-5-20251001-thinking":  "claude-haiku-4.5",
	}

	if len(DefaultKiroModelMapping) != len(expected) {
		t.Fatalf("expected %d Kiro mappings, got %d", len(expected), len(DefaultKiroModelMapping))
	}
	for model, want := range expected {
		if got := DefaultKiroModelMapping[model]; got != want {
			t.Fatalf("unexpected Kiro mapping for %q: got %q want %q", model, got, want)
		}
	}

	for _, model := range []string{
		"claude-opus-4-5",
		"claude-sonnet-4-5",
		"claude-sonnet-4",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"gpt-4o",
		"gpt-4",
		"deepseek-3-2",
		"minimax-m2-1",
		"qwen3-coder-next",
		"claude-sonnet-4-6-chat",
	} {
		if _, ok := DefaultKiroModelMapping[model]; ok {
			t.Fatalf("did not expect %q to remain in DefaultKiroModelMapping", model)
		}
	}
	for model := range DefaultKiroModelMapping {
		if strings.HasSuffix(model, "-agentic") {
			t.Fatalf("did not expect agentic Kiro mapping %q", model)
		}
		if strings.HasSuffix(model, "-chat") {
			t.Fatalf("did not expect chat-only Kiro mapping %q", model)
		}
	}
}
