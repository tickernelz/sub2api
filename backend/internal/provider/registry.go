package provider

import "github.com/tickernelz/sub2api/internal/domain"

const (
	PlatformAnthropic   = domain.PlatformAnthropic
	PlatformOpenAI      = domain.PlatformOpenAI
	PlatformGemini      = domain.PlatformGemini
	PlatformAntigravity = domain.PlatformAntigravity
	PlatformKiro        = domain.PlatformKiro
	PlatformOpenCode    = "opencode"
)

const (
	AccountTypeOAuth          = domain.AccountTypeOAuth
	AccountTypeSetupToken     = domain.AccountTypeSetupToken
	AccountTypeAPIKey         = domain.AccountTypeAPIKey
	AccountTypeUpstream       = domain.AccountTypeUpstream
	AccountTypeBedrock        = domain.AccountTypeBedrock
	AccountTypeServiceAccount = domain.AccountTypeServiceAccount
)

type TargetFormat string

const (
	TargetFormatOpenAIChat            TargetFormat = "openai"
	TargetFormatAnthropicMessages     TargetFormat = "claude"
	TargetFormatOpenAIResponses       TargetFormat = "openai-responses"
	TargetFormatGeminiGenerateContent TargetFormat = "gemini"
)

type Capabilities struct {
	SupportsOpenAIChatCompletions bool
	SupportsAnthropicMessages     bool
	SupportsOpenAIResponses       bool
	SupportsGeminiGenerateContent bool
	SupportsModelSync             bool
	SupportsUsage                 bool
	MaxTools                      int
}

type Variant struct {
	ID             string
	DisplayName    string
	BaseURL        string
	ModelsURL      string
	TestKeyBaseURL string
}

type ModelMetadata struct {
	ID                string
	DisplayName       string
	TargetFormat      TargetFormat
	ContextLength     int
	SupportsVision    bool
	SupportsReasoning bool
}

type Definition struct {
	Platform         string
	DisplayName      string
	AccountTypes     []string
	DefaultVariantID string
	Variants         []Variant
	DefaultModels    []ModelMetadata
	Capabilities     Capabilities
}

func (d Definition) SupportsAccountType(accountType string) bool {
	for _, t := range d.AccountTypes {
		if t == accountType {
			return true
		}
	}
	return false
}

func (d Definition) Variant(id string) (Variant, bool) {
	for _, v := range d.Variants {
		if v.ID == id {
			return v, true
		}
	}
	return Variant{}, false
}

var registry = map[string]Definition{
	PlatformOpenCode: openCodeDefinition,
}

func Get(platform string) (Definition, bool) {
	def, ok := registry[platform]
	return def, ok
}

func All() []Definition {
	defs := make([]Definition, 0, len(registry))
	for _, def := range registry {
		defs = append(defs, def)
	}
	return defs
}
