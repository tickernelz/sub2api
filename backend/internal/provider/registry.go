package provider

import (
	"sort"
	"strings"

	"github.com/tickernelz/sub2api/internal/domain"
)

const (
	PlatformAnthropic   = domain.PlatformAnthropic
	PlatformOpenAI      = domain.PlatformOpenAI
	PlatformGemini      = domain.PlatformGemini
	PlatformAntigravity = domain.PlatformAntigravity
	PlatformKiro        = domain.PlatformKiro
	PlatformOpenCode    = domain.PlatformOpenCode
)

const (
	AccountTypeOAuth          = domain.AccountTypeOAuth
	AccountTypeSetupToken     = domain.AccountTypeSetupToken
	AccountTypeAPIKey         = domain.AccountTypeAPIKey
	AccountTypeUpstream       = domain.AccountTypeUpstream
	AccountTypeBedrock        = domain.AccountTypeBedrock
	AccountTypeServiceAccount = domain.AccountTypeServiceAccount
)

type Protocol string

const (
	ProtocolAnthropic        Protocol = "anthropic"
	ProtocolOpenAICompatible Protocol = "openai-compatible"
	ProtocolGemini           Protocol = "gemini"
	ProtocolAntigravity      Protocol = "antigravity"
	ProtocolKiro             Protocol = "kiro"
	ProtocolOpenCode         Protocol = "opencode"
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
	SupportsEmbeddings            bool
	SupportsImages                bool
	SupportsOpenAICompact         bool
	SupportsModelSync             bool
	SupportsUsage                 bool
	SupportsPlatformQuota         bool
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
	Platform              string
	DisplayName           string
	AccountTypes          []string
	Protocols             []Protocol
	ModelSyncAccountTypes []string
	DefaultVariantID      string
	Variants              []Variant
	DefaultModels         []ModelMetadata
	Capabilities          Capabilities
	DefaultGroupCount     int
}

func (d Definition) SupportsAccountType(accountType string) bool {
	for _, t := range d.AccountTypes {
		if t == accountType {
			return true
		}
	}
	return false
}

func (d Definition) SupportsProtocol(protocol Protocol) bool {
	for _, p := range d.Protocols {
		if p == protocol {
			return true
		}
	}
	return false
}

func (d Definition) SupportsModelSyncForAccountType(accountType string) bool {
	if !d.Capabilities.SupportsModelSync {
		return false
	}
	for _, t := range d.ModelSyncAccountTypes {
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
	PlatformAnthropic:   anthropicDefinition,
	PlatformOpenAI:      openAIDefinition,
	PlatformGemini:      geminiDefinition,
	PlatformAntigravity: antigravityDefinition,
	PlatformKiro:        kiroDefinition,
	PlatformOpenCode:    openCodeDefinition,
}

var registryPlatformOrder = []string{
	PlatformAnthropic,
	PlatformOpenAI,
	PlatformGemini,
	PlatformAntigravity,
	PlatformKiro,
	PlatformOpenCode,
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
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Platform < defs[j].Platform
	})
	return defs
}

func SimpleModeDefaultGroupRequirements() map[string]int {
	requirements := make(map[string]int, len(registry))
	for _, def := range registry {
		if def.DefaultGroupCount > 0 {
			requirements[def.Platform] = def.DefaultGroupCount
		}
	}
	return requirements
}

func DefaultModelIDsForPlatform(platform string) []string {
	def, ok := Get(platform)
	if !ok {
		return nil
	}
	seen := make(map[string]struct{}, len(def.DefaultModels))
	ids := make([]string, 0, len(def.DefaultModels))
	for _, model := range def.DefaultModels {
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

func PlatformQuotaPlatforms() []string {
	platforms := make([]string, 0, len(registry))
	for _, platform := range registryPlatformOrder {
		def, ok := Get(platform)
		if ok && def.Capabilities.SupportsPlatformQuota {
			platforms = append(platforms, platform)
		}
	}
	return platforms
}
