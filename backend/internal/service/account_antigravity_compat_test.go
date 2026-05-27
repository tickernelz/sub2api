package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountGetModelMapping_AntigravityKeepsHiddenCompatibilityAliases(t *testing.T) {
	account := &Account{
		Platform:    PlatformAntigravity,
		Credentials: map[string]any{},
	}

	mapping := account.GetModelMapping()
	require.Equal(t, "claude-opus-4-6-thinking", mapping["claude-opus-4-6"])
	require.Equal(t, "gemini-pro-agent", mapping["gemini-3.1-pro-high"])
	require.Equal(t, "gemini-3.1-flash-image", mapping["gemini-3-pro-image"])
}
