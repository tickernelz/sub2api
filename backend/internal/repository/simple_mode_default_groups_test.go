package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tickernelz/sub2api/internal/service"
)

func TestSimpleModeDefaultGroupRequirementsIncludeOpenCode(t *testing.T) {
	requirements := simpleModeDefaultGroupRequirements()

	require.Equal(t, 1, requirements[service.PlatformOpenCode])
	require.Equal(t, 1, requirements[service.PlatformKiro])
	require.Equal(t, 2, requirements[service.PlatformAntigravity])
}
