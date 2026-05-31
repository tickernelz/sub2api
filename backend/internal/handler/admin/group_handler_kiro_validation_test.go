package admin

import (
	"testing"

	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
)

func TestGroupRequestValidationAcceptsKiroPlatform(t *testing.T) {
	createReq := CreateGroupRequest{Name: "kiro-default", Platform: "kiro"}
	require.NoError(t, binding.Validator.ValidateStruct(createReq))

	updateReq := UpdateGroupRequest{Platform: "kiro"}
	require.NoError(t, binding.Validator.ValidateStruct(updateReq))
}

func TestGroupRequestValidationAcceptsOpenCodePlatform(t *testing.T) {
	createReq := CreateGroupRequest{Name: "opencode-default", Platform: "opencode"}
	require.NoError(t, binding.Validator.ValidateStruct(createReq))

	updateReq := UpdateGroupRequest{Platform: "opencode"}
	require.NoError(t, binding.Validator.ValidateStruct(updateReq))
}

func TestGroupRequestValidationAcceptsCursorPlatform(t *testing.T) {
	createReq := CreateGroupRequest{Name: "cursor-default", Platform: "cursor"}
	require.NoError(t, binding.Validator.ValidateStruct(createReq))

	updateReq := UpdateGroupRequest{Platform: "cursor"}
	require.NoError(t, binding.Validator.ValidateStruct(updateReq))
}
