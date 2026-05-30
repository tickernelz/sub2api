package provider

import "strings"

const (
	OpenCodeVariantOpenCode = "opencode"
	OpenCodeVariantZen      = "opencode-zen"
	OpenCodeVariantGo       = "opencode-go"
)

const (
	OpenCodeDefaultBaseURL   = "https://opencode.ai/zen/v1"
	OpenCodeGoBaseURL        = "https://opencode.ai/zen/go/v1"
	OpenCodeDefaultModelsURL = "https://opencode.ai/zen/v1/models"
	OpenCodeDefaultQuotaURL  = "https://opencode.ai/zen/go/v1/quota"
)

var openCodeDefinition = Definition{
	Platform:         PlatformOpenCode,
	DisplayName:      "OpenCode",
	AccountTypes:     []string{AccountTypeAPIKey},
	DefaultVariantID: OpenCodeVariantZen,
	Variants: []Variant{
		{ID: OpenCodeVariantOpenCode, DisplayName: "OpenCode", BaseURL: OpenCodeDefaultBaseURL, ModelsURL: OpenCodeDefaultModelsURL},
		{ID: OpenCodeVariantZen, DisplayName: "OpenCode Zen", BaseURL: OpenCodeDefaultBaseURL, ModelsURL: OpenCodeDefaultModelsURL},
		{ID: OpenCodeVariantGo, DisplayName: "OpenCode Go", BaseURL: OpenCodeGoBaseURL, ModelsURL: OpenCodeGoBaseURL + "/models", TestKeyBaseURL: OpenCodeDefaultBaseURL},
	},
	DefaultModels: openCodeModels,
	Capabilities: Capabilities{
		SupportsOpenAIChatCompletions: true,
		SupportsAnthropicMessages:     true,
		SupportsOpenAIResponses:       true,
		SupportsGeminiGenerateContent: true,
		SupportsModelSync:             true,
		SupportsUsage:                 true,
		MaxTools:                      128,
	},
}

var openCodeModels = []ModelMetadata{
	// OpenCode Zen/base models.
	{ID: "big-pickle", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5-nano", TargetFormat: TargetFormatOpenAIChat, ContextLength: 400000, SupportsVision: true},
	{ID: "gpt-5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5-codex", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.1", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.1-codex", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.1-codex-max", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.1-codex-mini", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.2", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.2-codex", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.3-codex", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.3-codex-spark", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.4", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.4-mini", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.4-nano", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.4-pro", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gpt-5.5-pro", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "claude-haiku-4-5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "claude-sonnet-4", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "claude-sonnet-4-5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "claude-sonnet-4-6", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "claude-opus-4-1", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "claude-opus-4-5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "claude-opus-4-6", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "claude-opus-4-7", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gemini-3-flash", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gemini-3.1-pro", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "gemini-3.5-flash", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "grok-build-0.1", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "glm-5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "glm-5.1", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "minimax-m2.5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "minimax-m2.7", TargetFormat: TargetFormatAnthropicMessages, ContextLength: 200000, SupportsVision: true},
	{ID: "kimi-k2.5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "kimi-k2.6", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "qwen3.5-plus", TargetFormat: TargetFormatAnthropicMessages, ContextLength: 200000, SupportsVision: false},
	{ID: "qwen3.6-plus", TargetFormat: TargetFormatAnthropicMessages, ContextLength: 200000, SupportsVision: false},
	{ID: "deepseek-v4-flash-free", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true, SupportsReasoning: true},
	{ID: "minimax-m2.5-free", TargetFormat: TargetFormatOpenAIChat, ContextLength: 204800, SupportsVision: true},
	{ID: "nemotron-3-super-free", TargetFormat: TargetFormatOpenAIChat, ContextLength: 1000000, SupportsVision: true},
	{ID: "qwen3.6-plus-free", TargetFormat: TargetFormatAnthropicMessages, ContextLength: 200000, SupportsVision: false},

	// OpenCode Go models.
	{ID: "kimi-k2.5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "mimo-v2.5-pro", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "mimo-v2.5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "mimo-v2-pro", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "mimo-v2-omni", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true},
	{ID: "qwen3.7-max", TargetFormat: TargetFormatAnthropicMessages, ContextLength: 200000, SupportsVision: false},
	{ID: "qwen3.6-plus", TargetFormat: TargetFormatAnthropicMessages, ContextLength: 200000, SupportsVision: false},
	{ID: "deepseek-v4-pro", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true, SupportsReasoning: true},
	{ID: "deepseek-v4-flash", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true, SupportsReasoning: true},
}

var openCodeModelByID = func() map[string]ModelMetadata {
	m := make(map[string]ModelMetadata, len(openCodeModels))
	for _, model := range openCodeModels {
		if model.DisplayName == "" {
			model.DisplayName = model.ID
		}
		if _, exists := m[model.ID]; !exists {
			m[model.ID] = model
		}
	}
	return m
}()

func OpenCodeModelMetadata(model string) (ModelMetadata, bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return ModelMetadata{}, false
	}
	meta, ok := openCodeModelByID[model]
	if ok {
		return meta, true
	}
	return ModelMetadata{ID: model, DisplayName: model, TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true}, false
}

func ResolveOpenCodeVariant(credentials map[string]any) Variant {
	variantID := OpenCodeVariantZen
	if raw, ok := credentials["provider_variant"]; ok {
		if value := strings.TrimSpace(asString(raw)); value != "" {
			variantID = value
		}
	}
	if raw, ok := credentials["opencode_variant"]; ok {
		if value := strings.TrimSpace(asString(raw)); value != "" {
			variantID = value
		}
	}
	if raw, ok := credentials["base_url"]; ok {
		if baseURL := strings.TrimRight(strings.TrimSpace(asString(raw)), "/"); baseURL != "" {
			return Variant{ID: variantID, DisplayName: "Custom OpenCode", BaseURL: baseURL, ModelsURL: buildOpenCodeModelsURL(baseURL)}
		}
	}
	variant, ok := openCodeDefinition.Variant(variantID)
	if ok {
		return variant
	}
	variant, _ = openCodeDefinition.Variant(openCodeDefinition.DefaultVariantID)
	return variant
}

func OpenCodeDefaultModelIDs() []string {
	seen := make(map[string]struct{}, len(openCodeModels))
	ids := make([]string, 0, len(openCodeModels))
	for _, model := range openCodeModels {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func buildOpenCodeModelsURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/models") {
		return baseURL
	}
	return baseURL + "/models"
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
