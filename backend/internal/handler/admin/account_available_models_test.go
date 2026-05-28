package admin

import "testing"

func TestBuildMappedAntigravityModels_FiltersRedundantAliasesAndChatPseudoModels(t *testing.T) {
	models := buildMappedAntigravityModels(map[string]string{
		"chat_20706":                     "chat_20706",
		"claude-opus-4-6":                "claude-opus-4-6-thinking",
		"claude-opus-4-6-thinking":       "claude-opus-4-6-thinking",
		"claude-sonnet-4-5":              "claude-sonnet-4-5",
		"claude-sonnet-4-6":              "claude-sonnet-4-6",
		"gemini-2.5-flash":               "gemini-2.5-flash",
		"gemini-2.5-pro":                 "gemini-2.5-pro",
		"gemini-3-flash":                 "gemini-3-flash",
		"gemini-3-pro-high":              "gemini-pro-agent",
		"gemini-3.1-pro-high":            "gemini-pro-agent",
		"gemini-pro-agent":               "gemini-pro-agent",
		"gemini-3.1-pro-low":             "gemini-3.1-pro-low",
		"gemini-3.1-flash-image":         "gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview": "gemini-3.1-flash-image",
		"tab_flash_lite_preview":         "tab_flash_lite_preview",
		"z-custom-model":                 "z-custom-upstream",
	})

	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}

	want := []string{
		"claude-opus-4-6-thinking",
		"claude-sonnet-4-6",
		"gemini-2.5-flash",
		"gemini-3-flash",
		"gemini-3.1-flash-image",
		"gemini-pro-agent",
		"gemini-3.1-pro-low",
		"tab_flash_lite_preview",
		"z-custom-model",
	}
	if len(ids) != len(want) {
		t.Fatalf("unexpected ids length: got %v want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("unexpected filtered Antigravity model ids: got %v want %v", ids, want)
		}
	}
}

func TestBuildMappedAntigravityModels_UsesPlatformModelListOrder(t *testing.T) {
	models := buildMappedAntigravityModels(map[string]string{
		"z-custom-model":           "z-custom-upstream",
		"gemini-3-flash":           "gemini-3-flash",
		"claude-sonnet-4-6":        "claude-sonnet-4-6",
		"claude-opus-4-6-thinking": "claude-opus-4-6-thinking",
	})

	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}

	want := []string{"claude-opus-4-6-thinking", "claude-sonnet-4-6", "gemini-3-flash", "z-custom-model"}
	if len(ids) != len(want) {
		t.Fatalf("unexpected ids length: got %v want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("unexpected order: got %v want %v", ids, want)
		}
	}
}
