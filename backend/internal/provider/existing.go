package provider

import (
	"strings"

	"github.com/tickernelz/sub2api/internal/pkg/antigravity"
	"github.com/tickernelz/sub2api/internal/pkg/claude"
	"github.com/tickernelz/sub2api/internal/pkg/geminicli"
	kiropkg "github.com/tickernelz/sub2api/internal/pkg/kiro"
	"github.com/tickernelz/sub2api/internal/pkg/openai"
)

var anthropicDefinition = Definition{
	Platform:              PlatformAnthropic,
	DisplayName:           "Anthropic",
	AccountTypes:          []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey, AccountTypeBedrock, AccountTypeServiceAccount},
	Protocols:             []Protocol{ProtocolAnthropic},
	ModelSyncAccountTypes: []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey},
	DefaultModels:         claudeModelMetadata(),
	DefaultGroupCount:     1,
	Capabilities: Capabilities{
		SupportsOpenAIChatCompletions: true,
		SupportsAnthropicMessages:     true,
		SupportsOpenAIResponses:       true,
		SupportsModelSync:             true,
		SupportsUsage:                 true,
		SupportsPlatformQuota:         true,
		MaxTools:                      128,
	},
}

var openAIDefinition = Definition{
	Platform:              PlatformOpenAI,
	DisplayName:           "OpenAI",
	AccountTypes:          []string{AccountTypeOAuth, AccountTypeAPIKey},
	Protocols:             []Protocol{ProtocolOpenAICompatible},
	ModelSyncAccountTypes: []string{AccountTypeAPIKey},
	DefaultModels:         openAIModelMetadata(),
	DefaultGroupCount:     1,
	Capabilities: Capabilities{
		SupportsOpenAIChatCompletions: true,
		SupportsOpenAIResponses:       true,
		SupportsEmbeddings:            true,
		SupportsImages:                true,
		SupportsOpenAICompact:         true,
		SupportsModelSync:             true,
		SupportsUsage:                 true,
		SupportsPlatformQuota:         true,
		MaxTools:                      128,
	},
}

var geminiDefinition = Definition{
	Platform:              PlatformGemini,
	DisplayName:           "Gemini",
	AccountTypes:          []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeServiceAccount},
	Protocols:             []Protocol{ProtocolGemini},
	ModelSyncAccountTypes: []string{AccountTypeOAuth, AccountTypeAPIKey},
	DefaultModels:         geminiModelMetadata(),
	DefaultGroupCount:     1,
	Capabilities: Capabilities{
		SupportsOpenAIChatCompletions: true,
		SupportsGeminiGenerateContent: true,
		SupportsModelSync:             true,
		SupportsUsage:                 true,
		SupportsPlatformQuota:         true,
		MaxTools:                      128,
	},
}

var antigravityDefinition = Definition{
	Platform:              PlatformAntigravity,
	DisplayName:           "Antigravity",
	AccountTypes:          []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeUpstream},
	Protocols:             []Protocol{ProtocolAntigravity},
	ModelSyncAccountTypes: []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeUpstream},
	DefaultModels:         antigravityModelMetadata(),
	DefaultGroupCount:     2,
	Capabilities: Capabilities{
		SupportsOpenAIChatCompletions: true,
		SupportsAnthropicMessages:     true,
		SupportsGeminiGenerateContent: true,
		SupportsModelSync:             true,
		SupportsUsage:                 true,
		SupportsPlatformQuota:         true,
		MaxTools:                      128,
	},
}

var kiroDefinition = Definition{
	Platform:          PlatformKiro,
	DisplayName:       "Kiro",
	AccountTypes:      []string{AccountTypeOAuth, AccountTypeAPIKey},
	Protocols:         []Protocol{ProtocolKiro},
	DefaultModels:     kiroModelMetadata(),
	DefaultGroupCount: 1,
	Capabilities: Capabilities{
		SupportsOpenAIChatCompletions: true,
		SupportsAnthropicMessages:     true,
		SupportsUsage:                 true,
		MaxTools:                      128,
	},
}

func claudeModelMetadata() []ModelMetadata {
	models := make([]ModelMetadata, 0, len(claude.DefaultModels))
	for _, model := range claude.DefaultModels {
		models = append(models, ModelMetadata{
			ID:             model.ID,
			DisplayName:    model.DisplayName,
			TargetFormat:   TargetFormatAnthropicMessages,
			ContextLength:  200000,
			SupportsVision: true,
		})
	}
	return models
}

func openAIModelMetadata() []ModelMetadata {
	models := make([]ModelMetadata, 0, len(openai.DefaultModels))
	for _, model := range openai.DefaultModels {
		targetFormat := TargetFormatOpenAIChat
		if strings.HasPrefix(model.ID, "gpt-image-") {
			targetFormat = TargetFormatOpenAIResponses
		}
		models = append(models, ModelMetadata{
			ID:             model.ID,
			DisplayName:    model.DisplayName,
			TargetFormat:   targetFormat,
			ContextLength:  400000,
			SupportsVision: true,
		})
	}
	return models
}

func geminiModelMetadata() []ModelMetadata {
	models := make([]ModelMetadata, 0, len(geminicli.DefaultModels))
	for _, model := range geminicli.DefaultModels {
		models = append(models, ModelMetadata{
			ID:             model.ID,
			DisplayName:    model.DisplayName,
			TargetFormat:   TargetFormatGeminiGenerateContent,
			ContextLength:  1000000,
			SupportsVision: true,
		})
	}
	return models
}

func antigravityModelMetadata() []ModelMetadata {
	models := antigravity.DefaultModels()
	metadata := make([]ModelMetadata, 0, len(models))
	for _, model := range models {
		targetFormat := TargetFormatAnthropicMessages
		if strings.HasPrefix(model.ID, "gemini-") || strings.HasPrefix(model.ID, "tab_") || strings.HasPrefix(model.ID, "gpt-oss-") {
			targetFormat = TargetFormatGeminiGenerateContent
		}
		metadata = append(metadata, ModelMetadata{
			ID:             model.ID,
			DisplayName:    model.DisplayName,
			TargetFormat:   targetFormat,
			ContextLength:  200000,
			SupportsVision: true,
		})
	}
	return metadata
}

func kiroModelMetadata() []ModelMetadata {
	models := make([]ModelMetadata, 0, len(kiropkg.DefaultModels))
	for _, model := range kiropkg.DefaultModels {
		models = append(models, ModelMetadata{
			ID:             model.ID,
			DisplayName:    model.DisplayName,
			TargetFormat:   TargetFormatAnthropicMessages,
			ContextLength:  200000,
			SupportsVision: true,
		})
	}
	return models
}
