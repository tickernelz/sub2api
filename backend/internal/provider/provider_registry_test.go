package provider

import (
	"reflect"
	"testing"
)

func TestRegistryCoversAllExistingPlatforms(t *testing.T) {
	expected := []string{
		PlatformAnthropic,
		PlatformOpenAI,
		PlatformGemini,
		PlatformAntigravity,
		PlatformKiro,
		PlatformOpenCode,
	}

	seen := map[string]bool{}
	for _, def := range All() {
		if def.Platform == "" {
			t.Fatalf("registered provider has empty platform: %+v", def)
		}
		if seen[def.Platform] {
			t.Fatalf("duplicate provider definition for platform %q", def.Platform)
		}
		seen[def.Platform] = true
	}

	for _, platform := range expected {
		if _, ok := Get(platform); !ok {
			t.Fatalf("expected provider registry to include platform %q", platform)
		}
	}
}

func TestProviderDefinitionsDeclarePolicies(t *testing.T) {
	for _, def := range All() {
		if def.DisplayName == "" {
			t.Fatalf("provider %q has empty display name", def.Platform)
		}
		if len(def.AccountTypes) == 0 {
			t.Fatalf("provider %q has no account type policy", def.Platform)
		}
		if len(def.Protocols) == 0 {
			t.Fatalf("provider %q has no protocol policy", def.Platform)
		}
		if def.DefaultGroupCount <= 0 {
			t.Fatalf("provider %q has invalid default group count %d", def.Platform, def.DefaultGroupCount)
		}
	}
}

func TestProviderAccountTypePolicies(t *testing.T) {
	cases := []struct {
		platform string
		allowed  []string
		denied   []string
	}{
		{platform: PlatformAnthropic, allowed: []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey, AccountTypeBedrock, AccountTypeServiceAccount}, denied: []string{AccountTypeUpstream}},
		{platform: PlatformOpenAI, allowed: []string{AccountTypeOAuth, AccountTypeAPIKey}, denied: []string{AccountTypeSetupToken, AccountTypeBedrock, AccountTypeServiceAccount}},
		{platform: PlatformGemini, allowed: []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeServiceAccount}, denied: []string{AccountTypeSetupToken, AccountTypeBedrock}},
		{platform: PlatformAntigravity, allowed: []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeUpstream}, denied: []string{AccountTypeSetupToken, AccountTypeBedrock, AccountTypeServiceAccount}},
		{platform: PlatformKiro, allowed: []string{AccountTypeOAuth, AccountTypeAPIKey}, denied: []string{AccountTypeSetupToken, AccountTypeBedrock, AccountTypeServiceAccount}},
		{platform: PlatformOpenCode, allowed: []string{AccountTypeAPIKey}, denied: []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeBedrock, AccountTypeServiceAccount}},
	}

	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			def, ok := Get(tc.platform)
			if !ok {
				t.Fatalf("missing provider definition for %q", tc.platform)
			}
			for _, accountType := range tc.allowed {
				if !def.SupportsAccountType(accountType) {
					t.Fatalf("platform %q should support account type %q", tc.platform, accountType)
				}
			}
			for _, accountType := range tc.denied {
				if def.SupportsAccountType(accountType) {
					t.Fatalf("platform %q should not support account type %q", tc.platform, accountType)
				}
			}
		})
	}
}

func TestProviderDefaultGroupAndQuotaPolicies(t *testing.T) {
	cases := []struct {
		platform              string
		defaultGroupCount     int
		platformQuotaEligible bool
	}{
		{platform: PlatformAnthropic, defaultGroupCount: 1, platformQuotaEligible: true},
		{platform: PlatformOpenAI, defaultGroupCount: 1, platformQuotaEligible: true},
		{platform: PlatformGemini, defaultGroupCount: 1, platformQuotaEligible: true},
		{platform: PlatformAntigravity, defaultGroupCount: 2, platformQuotaEligible: true},
		{platform: PlatformKiro, defaultGroupCount: 1, platformQuotaEligible: false},
		{platform: PlatformOpenCode, defaultGroupCount: 1, platformQuotaEligible: true},
	}

	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			def, ok := Get(tc.platform)
			if !ok {
				t.Fatalf("missing provider definition for %q", tc.platform)
			}
			if def.DefaultGroupCount != tc.defaultGroupCount {
				t.Fatalf("default group count = %d, want %d", def.DefaultGroupCount, tc.defaultGroupCount)
			}
			if def.Capabilities.SupportsPlatformQuota != tc.platformQuotaEligible {
				t.Fatalf("platform quota eligibility = %v, want %v", def.Capabilities.SupportsPlatformQuota, tc.platformQuotaEligible)
			}
		})
	}
}

func TestSimpleModeDefaultGroupRequirements(t *testing.T) {
	requirements := SimpleModeDefaultGroupRequirements()
	expected := map[string]int{
		PlatformAnthropic:   1,
		PlatformOpenAI:      1,
		PlatformGemini:      1,
		PlatformAntigravity: 2,
		PlatformKiro:        1,
		PlatformOpenCode:    1,
	}
	for platform, count := range expected {
		if requirements[platform] != count {
			t.Fatalf("requirement for %q = %d, want %d", platform, requirements[platform], count)
		}
	}
	for platform := range requirements {
		if _, ok := expected[platform]; !ok {
			t.Fatalf("unexpected default group requirement for %q", platform)
		}
	}
}

func TestDefaultModelIDsForPlatformAreRegistryDriven(t *testing.T) {
	cases := []struct {
		platform string
		wantIDs  []string
		denyIDs  []string
	}{
		{platform: PlatformAnthropic, wantIDs: []string{"claude-sonnet-4-6"}, denyIDs: []string{"gpt-5", "gemini-2.5-flash"}},
		{platform: PlatformOpenAI, wantIDs: []string{"gpt-5.5"}, denyIDs: []string{"claude-sonnet-4-6", "gemini-2.5-flash"}},
		{platform: PlatformGemini, wantIDs: []string{"gemini-2.5-flash"}, denyIDs: []string{"gpt-5", "claude-sonnet-4-6"}},
		{platform: PlatformAntigravity, wantIDs: []string{"gemini-3.5-flash-low"}, denyIDs: []string{"gpt-5"}},
		{platform: PlatformKiro, wantIDs: []string{"claude-opus-4-7"}, denyIDs: []string{"gpt-5"}},
		{platform: PlatformOpenCode, wantIDs: []string{"glm-5.1", "qwen3.7-max"}, denyIDs: []string{"claude-sonnet-4-5-20250929"}},
	}

	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			ids := DefaultModelIDsForPlatform(tc.platform)
			if len(ids) == 0 {
				t.Fatalf("expected default model IDs for %q", tc.platform)
			}
			seen := map[string]bool{}
			for _, id := range ids {
				if seen[id] {
					t.Fatalf("duplicate default model ID %q for platform %q", id, tc.platform)
				}
				seen[id] = true
			}
			for _, id := range tc.wantIDs {
				if !seen[id] {
					t.Fatalf("platform %q missing expected default model %q in %v", tc.platform, id, ids)
				}
			}
			for _, id := range tc.denyIDs {
				if seen[id] {
					t.Fatalf("platform %q leaked unrelated default model %q in %v", tc.platform, id, ids)
				}
			}
		})
	}

	if ids := DefaultModelIDsForPlatform("unknown-provider"); len(ids) != 0 {
		t.Fatalf("unknown provider default IDs = %v, want empty", ids)
	}
}

func TestPlatformQuotaPlatformsDerivedFromRegistryCapabilities(t *testing.T) {
	want := []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformOpenCode}
	if got := PlatformQuotaPlatforms(); !reflect.DeepEqual(got, want) {
		t.Fatalf("quota platforms = %v, want %v", got, want)
	}
}

func TestProviderRuntimeCapabilityPolicies(t *testing.T) {
	cases := []struct {
		platform              string
		protocols             []Protocol
		supportsAccountUsage  bool
		supportsModelSync     bool
		supportsOpenAICompact bool
	}{
		{platform: PlatformAnthropic, protocols: []Protocol{ProtocolAnthropic}, supportsAccountUsage: true, supportsModelSync: true},
		{platform: PlatformOpenAI, protocols: []Protocol{ProtocolOpenAICompatible}, supportsAccountUsage: true, supportsModelSync: true, supportsOpenAICompact: true},
		{platform: PlatformGemini, protocols: []Protocol{ProtocolGemini}, supportsAccountUsage: true, supportsModelSync: true},
		{platform: PlatformAntigravity, protocols: []Protocol{ProtocolAntigravity}, supportsAccountUsage: true, supportsModelSync: true},
		{platform: PlatformKiro, protocols: []Protocol{ProtocolKiro}, supportsAccountUsage: true, supportsModelSync: false},
		{platform: PlatformOpenCode, protocols: []Protocol{ProtocolOpenAICompatible, ProtocolOpenCode}, supportsAccountUsage: true, supportsModelSync: true},
	}

	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			def, ok := Get(tc.platform)
			if !ok {
				t.Fatalf("missing provider definition for %q", tc.platform)
			}
			for _, protocol := range tc.protocols {
				if !def.SupportsProtocol(protocol) {
					t.Fatalf("provider %q should support protocol %q", tc.platform, protocol)
				}
			}
			if def.Capabilities.SupportsUsage != tc.supportsAccountUsage {
				t.Fatalf("account usage capability = %v, want %v", def.Capabilities.SupportsUsage, tc.supportsAccountUsage)
			}
			if def.Capabilities.SupportsModelSync != tc.supportsModelSync {
				t.Fatalf("model sync capability = %v, want %v", def.Capabilities.SupportsModelSync, tc.supportsModelSync)
			}
			if def.Capabilities.SupportsOpenAICompact != tc.supportsOpenAICompact {
				t.Fatalf("OpenAI compact capability = %v, want %v", def.Capabilities.SupportsOpenAICompact, tc.supportsOpenAICompact)
			}
		})
	}
}

func TestProviderModelSyncAccountTypePolicies(t *testing.T) {
	cases := []struct {
		platform string
		allowed  []string
		denied   []string
	}{
		{platform: PlatformAnthropic, allowed: []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey}, denied: []string{AccountTypeBedrock, AccountTypeServiceAccount}},
		{platform: PlatformOpenAI, allowed: []string{AccountTypeAPIKey}, denied: []string{AccountTypeOAuth}},
		{platform: PlatformGemini, allowed: []string{AccountTypeOAuth, AccountTypeAPIKey}, denied: []string{AccountTypeServiceAccount}},
		{platform: PlatformAntigravity, allowed: []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeUpstream}, denied: []string{AccountTypeSetupToken}},
		{platform: PlatformKiro, denied: []string{AccountTypeOAuth, AccountTypeAPIKey}},
		{platform: PlatformOpenCode, allowed: []string{AccountTypeAPIKey}, denied: []string{AccountTypeOAuth}},
	}

	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			def, ok := Get(tc.platform)
			if !ok {
				t.Fatalf("missing provider definition for %q", tc.platform)
			}
			for _, accountType := range tc.allowed {
				if !def.SupportsModelSyncForAccountType(accountType) {
					t.Fatalf("platform %q should support model sync for account type %q", tc.platform, accountType)
				}
			}
			for _, accountType := range tc.denied {
				if def.SupportsModelSyncForAccountType(accountType) {
					t.Fatalf("platform %q should not support model sync for account type %q", tc.platform, accountType)
				}
			}
		})
	}
}

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
