package provider

const (
	CursorDefaultBaseURL = "https://agentn.global.api5.cursor.sh"
	CursorRunPath        = "/agent.v1.AgentService/Run"
	CursorRunURL         = CursorDefaultBaseURL + CursorRunPath
)

var cursorDefinition = Definition{
	Platform:          PlatformCursor,
	DisplayName:       "Cursor",
	AccountTypes:      []string{AccountTypeOAuth},
	Protocols:         []Protocol{ProtocolCursor},
	DefaultGroupCount: 0,
	Variants: []Variant{
		{ID: PlatformCursor, DisplayName: "Cursor", BaseURL: CursorDefaultBaseURL},
	},
	DefaultModels: cursorModels,
	Capabilities: Capabilities{
		MaxTools: 0,
	},
}

var cursorModels = []ModelMetadata{
	{ID: "composer-2.5", DisplayName: "Composer 2.5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true, SupportsReasoning: true},
	{ID: "claude-4-6-sonnet", DisplayName: "Claude 4.6 Sonnet", TargetFormat: TargetFormatAnthropicMessages, ContextLength: 200000, SupportsVision: true, SupportsReasoning: true},
	{ID: "claude-opus-4-8", DisplayName: "Claude Opus 4.8", TargetFormat: TargetFormatAnthropicMessages, ContextLength: 200000, SupportsVision: true, SupportsReasoning: true},
	{ID: "gemini-3.1-pro", DisplayName: "Gemini 3.1 Pro", TargetFormat: TargetFormatGeminiGenerateContent, ContextLength: 1000000, SupportsVision: true, SupportsReasoning: true},
	{ID: "gemini-3.5-flash", DisplayName: "Gemini 3.5 Flash", TargetFormat: TargetFormatGeminiGenerateContent, ContextLength: 1000000, SupportsVision: true, SupportsReasoning: true},
	{ID: "gpt-5.5", DisplayName: "GPT-5.5", TargetFormat: TargetFormatOpenAIChat, ContextLength: 272000, SupportsVision: true, SupportsReasoning: true},
	{ID: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex", TargetFormat: TargetFormatOpenAIChat, ContextLength: 272000, SupportsVision: true, SupportsReasoning: true},
	{ID: "grok-build-0.1", DisplayName: "Grok Build 0.1", TargetFormat: TargetFormatOpenAIChat, ContextLength: 200000, SupportsVision: true, SupportsReasoning: true},
}
