package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeOpus55_FamilyFallbackPricing(t *testing.T) {
	svc := NewBillingService(&config.Config{}, &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"claude-opus-5-5": {
				InputCostPerToken:           4e-6,
				OutputCostPerToken:          20e-6,
				CacheCreationInputTokenCost: 5e-6,
				CacheReadInputTokenCost:     0.2e-6,
			},
		},
	})

	pricing, err := svc.GetModelPricing("claude-opus-5-5")
	require.NoError(t, err)
	require.NotNil(t, pricing)
	assert.Equal(t, 4e-6, pricing.InputPricePerToken)
	assert.Equal(t, 20e-6, pricing.OutputPricePerToken)
}

func TestClaudeOpus55_BedrockMappingAndEffort(t *testing.T) {
	mapped, ok := domain.DefaultBedrockModelMapping["claude-opus-5-5"]
	require.True(t, ok, "claude-opus-5-5 missing from DefaultBedrockModelMapping")
	assert.Equal(t, "us.anthropic.claude-opus-5-5-v1", mapped)

	levels := claude.EffortLevelsForModel("claude-opus-5-5")
	require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, levels)

	assert.True(t, modelSupportsAnthropicFastMode("claude-opus-5-5"))
}
