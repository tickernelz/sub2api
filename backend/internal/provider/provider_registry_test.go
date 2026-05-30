package provider

import "testing"

func TestOpenCodeProviderDefinition(t *testing.T) {
	def, ok := Get(PlatformOpenCode)
	if !ok {
		t.Fatalf("expected OpenCode provider to be registered")
	}
	if def.Platform != PlatformOpenCode {
		t.Fatalf("platform = %q, want %q", def.Platform, PlatformOpenCode)
	}
	if !def.SupportsAccountType(AccountTypeAPIKey) {
		t.Fatalf("OpenCode should support API-key accounts")
	}
	if def.SupportsAccountType(AccountTypeOAuth) {
		t.Fatalf("OpenCode should not claim OAuth support")
	}
	if def.DefaultVariantID != OpenCodeVariantZen {
		t.Fatalf("default variant = %q, want %q", def.DefaultVariantID, OpenCodeVariantZen)
	}
	variant, ok := def.Variant(OpenCodeVariantGo)
	if !ok {
		t.Fatalf("expected OpenCode Go variant")
	}
	if variant.BaseURL != "https://opencode.ai/zen/go/v1" {
		t.Fatalf("go base URL = %q", variant.BaseURL)
	}
	if !def.Capabilities.SupportsOpenAIChatCompletions || !def.Capabilities.SupportsAnthropicMessages || !def.Capabilities.SupportsOpenAIResponses || !def.Capabilities.SupportsGeminiGenerateContent {
		t.Fatalf("OpenCode should advertise all target format capabilities: %+v", def.Capabilities)
	}
}

func TestOpenCodeModelMetadata(t *testing.T) {
	cases := []struct {
		model      string
		wantFormat TargetFormat
		wantVision bool
	}{
		{model: "glm-5.1", wantFormat: TargetFormatOpenAIChat, wantVision: true},
		{model: "qwen3.7-max", wantFormat: TargetFormatAnthropicMessages, wantVision: false},
		{model: "minimax-m2.7", wantFormat: TargetFormatAnthropicMessages, wantVision: true},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			meta, ok := OpenCodeModelMetadata(tc.model)
			if !ok {
				t.Fatalf("missing model metadata for %s", tc.model)
			}
			if meta.TargetFormat != tc.wantFormat {
				t.Fatalf("target format = %q, want %q", meta.TargetFormat, tc.wantFormat)
			}
			if meta.SupportsVision != tc.wantVision {
				t.Fatalf("supports vision = %v, want %v", meta.SupportsVision, tc.wantVision)
			}
		})
	}
}

func TestOpenCodeVariantFromCredentials(t *testing.T) {
	variant := ResolveOpenCodeVariant(map[string]any{"provider_variant": "opencode-go"})
	if variant.ID != OpenCodeVariantGo {
		t.Fatalf("variant = %q, want %q", variant.ID, OpenCodeVariantGo)
	}
	if variant.ModelsURL != "https://opencode.ai/zen/go/v1/models" {
		t.Fatalf("go models URL = %q", variant.ModelsURL)
	}

	custom := ResolveOpenCodeVariant(map[string]any{
		"provider_variant": "opencode-go",
		"base_url":         "https://proxy.example.com/opencode/v1/",
	})
	if custom.ID != OpenCodeVariantGo {
		t.Fatalf("custom variant = %q, want %q", custom.ID, OpenCodeVariantGo)
	}
	if custom.BaseURL != "https://proxy.example.com/opencode/v1" {
		t.Fatalf("custom base URL = %q", custom.BaseURL)
	}
	if custom.ModelsURL != "https://proxy.example.com/opencode/v1/models" {
		t.Fatalf("custom models URL = %q", custom.ModelsURL)
	}

	fallback := ResolveOpenCodeVariant(map[string]any{"provider_variant": "unknown"})
	if fallback.ID != OpenCodeVariantZen {
		t.Fatalf("fallback variant = %q, want %q", fallback.ID, OpenCodeVariantZen)
	}
}

func TestOpenCodeDefaultModelIDsAreDeduped(t *testing.T) {
	ids := OpenCodeDefaultModelIDs()
	if len(ids) == 0 {
		t.Fatalf("expected OpenCode default model IDs")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate model ID %q in %v", id, ids)
		}
		seen[id] = true
	}
	for _, id := range []string{"glm-5.1", "qwen3.7-max", "minimax-m2.7"} {
		if !seen[id] {
			t.Fatalf("missing expected model %q in %v", id, ids)
		}
	}
}
