package geminicli

import "testing"

func TestDefaultModels_ContainsImageModels(t *testing.T) {
	t.Parallel()

	byID := make(map[string]Model, len(DefaultModels))
	for _, model := range DefaultModels {
		byID[model.ID] = model
	}

	required := []string{
		"gemini-2.5-flash-image",
		"gemini-3.1-flash-image",
	}

	for _, id := range required {
		if _, ok := byID[id]; !ok {
			t.Fatalf("expected curated Gemini model %q to exist", id)
		}
	}
}

func TestDefaultModels_ExcludeAntigravityHighLowAliases(t *testing.T) {
	t.Parallel()

	byID := make(map[string]Model, len(DefaultModels))
	for _, model := range DefaultModels {
		byID[model.ID] = model
	}

	for _, id := range []string{"gemini-3.1-pro-high", "gemini-3.1-pro-low"} {
		if _, ok := byID[id]; ok {
			t.Fatalf("did not expect Gemini-native model list to expose Antigravity alias %q", id)
		}
	}
}
