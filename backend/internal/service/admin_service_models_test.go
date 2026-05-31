package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	providerregistry "github.com/tickernelz/sub2api/internal/provider"
)

func TestDefaultModelsListCandidateIDs_FollowsProviderRegistryForKnownPlatforms(t *testing.T) {
	platforms := []string{
		PlatformAnthropic,
		PlatformOpenAI,
		PlatformGemini,
		PlatformAntigravity,
		PlatformKiro,
		PlatformOpenCode,
		PlatformCursor,
	}

	for _, platform := range platforms {
		t.Run(platform, func(t *testing.T) {
			require.Equal(t, providerregistry.DefaultModelIDsForPlatform(platform), defaultModelsListCandidateIDs(platform))
		})
	}
}

func TestDefaultModelsListCandidateIDs_OpenCodeUsesProviderDefaults(t *testing.T) {
	ids := defaultModelsListCandidateIDs(PlatformOpenCode)
	require.Contains(t, ids, "glm-5.1")
	require.Contains(t, ids, "qwen3.7-max")
	require.NotContains(t, ids, "claude-sonnet-4-5-20250929")
}
