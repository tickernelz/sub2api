package antigravity

import "testing"

func TestDefaultModels_ExposeOnlyCuratedSmokePassingAntigravityModels(t *testing.T) {
	t.Parallel()

	models := DefaultModels()
	got := make([]string, 0, len(models))
	for _, m := range models {
		got = append(got, m.ID)
	}

	want := []string{
		"claude-opus-4-6-thinking",
		"claude-sonnet-4-6",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
		"gemini-2.5-flash-thinking",
		"gemini-3-flash",
		"gemini-3-flash-agent",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-lite",
		"gemini-3.5-flash-extra-low",
		"gemini-3.5-flash-low",
		"gpt-oss-120b-medium",
		"tab_flash_lite_preview",
		"tab_jump_flash_lite_preview",
	}

	if len(got) != len(want) {
		t.Fatalf("unexpected curated Antigravity model count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected curated Antigravity model order: got %v want %v", got, want)
		}
	}
}

func TestDefaultModels_DoesNotExposeRedundantAntigravityAliases(t *testing.T) {
	t.Parallel()

	models := DefaultModels()
	byID := make(map[string]ClaudeModel, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	for _, id := range []string{
		"gemini-pro-agent",
		"gemini-3.1-pro-high",
		"gemini-3.1-pro-low",
		"gemini-3-pro-high",
		"gemini-3-pro-low",
		"gemini-3-pro-preview",
		"gemini-3.1-pro-preview",
		"gemini-3-pro-image",
		"gemini-3-pro-image-preview",
		"gemini-3.1-flash-image-preview",
		"gemini-2.5-flash-image-preview",
		"claude-opus-4-7",
		"claude-opus-4-6",
		"claude-opus-4-5-thinking",
		"claude-sonnet-4-5",
		"claude-sonnet-4-5-thinking",
	} {
		if _, ok := byID[id]; ok {
			t.Fatalf("did not expect redundant or non-smoke-passing model %q in DefaultModels", id)
		}
	}
}
