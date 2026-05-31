package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateProviderAccountTypeRejectsOpenCodeOAuth(t *testing.T) {
	err := validateProviderAccountType(PlatformOpenCode, AccountTypeOAuth)

	require.Error(t, err)
	require.Contains(t, err.Error(), "platform opencode does not support account type oauth")
}

func TestValidateProviderAccountTypeAllowsOpenCodeAPIKey(t *testing.T) {
	err := validateProviderAccountType(PlatformOpenCode, AccountTypeAPIKey)

	require.NoError(t, err)
}

func TestValidateProviderAccountTypeAllowsAntigravityUpstream(t *testing.T) {
	err := validateProviderAccountType(PlatformAntigravity, AccountTypeUpstream)

	require.NoError(t, err)
}
