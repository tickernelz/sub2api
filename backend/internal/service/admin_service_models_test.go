package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsListCandidateIDs_OpenCodeUsesProviderDefaults(t *testing.T) {
	ids := defaultModelsListCandidateIDs(PlatformOpenCode)
	require.Contains(t, ids, "glm-5.1")
	require.Contains(t, ids, "qwen3.7-max")
	require.NotContains(t, ids, "claude-sonnet-4-5-20250929")
}
