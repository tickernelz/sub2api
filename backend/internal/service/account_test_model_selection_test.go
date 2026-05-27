package service

import "testing"

func TestSelectAccountTestModel_UsesExplicitModelID(t *testing.T) {
	account := &Account{Platform: PlatformGemini}

	got := selectAccountTestModel(account, "explicit-model", []string{"list-model"})

	if got != "explicit-model" {
		t.Fatalf("expected explicit model to win, got %q", got)
	}
}

func TestSelectAccountTestModel_UsesAccountMappingKeyBeforeFallbackList(t *testing.T) {
	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"custom-public-model": "upstream-model",
			},
		},
	}

	got := selectAccountTestModel(account, "", []string{"fallback-list-model"})

	if got != "custom-public-model" {
		t.Fatalf("expected account mapping key to win over fallback list, got %q", got)
	}
}

func TestSelectAccountTestModel_PrefersMappedModelListOrder(t *testing.T) {
	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"z-custom-model":       "upstream-z",
				"gemini-2.5-flash":     "gemini-2.5-flash",
				"gemini-3-pro-preview": "gemini-3-pro-preview",
			},
		},
	}

	got := selectAccountTestModel(account, "", []string{"gemini-3-pro-preview", "gemini-2.5-flash"})

	if got != "gemini-3-pro-preview" {
		t.Fatalf("expected first model-list entry supported by mapping, got %q", got)
	}
}

func TestSelectAccountTestModel_UsesPublicMappingKeyForMappedModelListEntry(t *testing.T) {
	account := &Account{
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gemini-3.1-pro-high": "gemini-pro-agent",
			},
		},
	}

	got := selectAccountTestModel(account, "", []string{"gemini-pro-agent"})

	if got != "gemini-3.1-pro-high" {
		t.Fatalf("expected public mapping key for upstream list entry, got %q", got)
	}
}

func TestSelectAccountTestModel_SkipsWildcardOnlyMappingForFallbackList(t *testing.T) {
	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gemini-*": "gemini-2.5-flash",
			},
		},
	}

	got := selectAccountTestModel(account, "", []string{"gemini-2.5-flash"})

	if got != "gemini-2.5-flash" {
		t.Fatalf("expected wildcard-only mapping to fall back to supported concrete model list entry, got %q", got)
	}
}

func TestSelectAccountTestModel_FallsBackToModelListWhenNoMapping(t *testing.T) {
	account := &Account{Platform: PlatformGemini, Type: AccountTypeOAuth}

	got := selectAccountTestModel(account, "", []string{"list-model-a", "list-model-b"})

	if got != "list-model-a" {
		t.Fatalf("expected first model list entry, got %q", got)
	}
}
