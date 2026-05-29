//go:build unit

package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAPIKeyGroupAssignments_DefaultOnlyBackfillsAssignedGroup(t *testing.T) {
	defaultGroupID := int64(20)

	groupID, groupIDs, err := normalizeAPIKeyGroupAssignments(defaultGroupIDPtr(defaultGroupID), nil)

	require.NoError(t, err)
	require.NotNil(t, groupID)
	require.Equal(t, defaultGroupID, *groupID)
	require.Equal(t, []int64{defaultGroupID}, groupIDs)
}

func TestNormalizeAPIKeyGroupAssignments_MultiGroupDedupesAndPrioritizesDefault(t *testing.T) {
	defaultGroupID := int64(20)

	groupID, groupIDs, err := normalizeAPIKeyGroupAssignments(defaultGroupIDPtr(defaultGroupID), []int64{30, 20, 30, 40})

	require.NoError(t, err)
	require.NotNil(t, groupID)
	require.Equal(t, defaultGroupID, *groupID)
	require.Equal(t, []int64{20, 30, 40}, groupIDs)
}

func TestNormalizeAPIKeyGroupAssignments_GroupIDsOnlyUsesFirstAsDefault(t *testing.T) {
	groupID, groupIDs, err := normalizeAPIKeyGroupAssignments(nil, []int64{30, 20})

	require.NoError(t, err)
	require.NotNil(t, groupID)
	require.Equal(t, int64(30), *groupID)
	require.Equal(t, []int64{30, 20}, groupIDs)
}

func TestNormalizeAPIKeyGroupAssignments_RejectsDefaultOutsideAssignedGroups(t *testing.T) {
	defaultGroupID := int64(20)

	_, _, err := normalizeAPIKeyGroupAssignments(defaultGroupIDPtr(defaultGroupID), []int64{30, 40})

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrAPIKeyDefaultGroupNotAssigned), "got %v", err)
}

func TestNormalizeAPIKeyGroupAssignments_RejectsTooManyGroups(t *testing.T) {
	groupIDs := make([]int64, maxAPIKeyAssignedGroups+1)
	for i := range groupIDs {
		groupIDs[i] = int64(i + 1)
	}

	_, _, err := normalizeAPIKeyGroupAssignments(nil, groupIDs)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrAPIKeyGroupLimitExceeded), "got %v", err)
}

func TestAPIKeyGroupIDsForUpdate_ScalarDefaultChangePreservesExistingAssignments(t *testing.T) {
	oldDefault := int64(10)
	newDefault := int64(20)
	apiKey := &APIKey{GroupID: &oldDefault, GroupIDs: []int64{10, 20, 30}}

	groupIDs := apiKeyGroupIDsForUpdate(apiKey, UpdateAPIKeyRequest{GroupID: &newDefault})

	require.Equal(t, []int64{10, 20, 30}, groupIDs)
}

func TestAPIKeyGroupIDsForUpdate_ScalarDefaultChangeOutsideAssignmentsKeepsLegacyReplacement(t *testing.T) {
	oldDefault := int64(10)
	newDefault := int64(40)
	apiKey := &APIKey{GroupID: &oldDefault, GroupIDs: []int64{10, 20, 30}}

	groupIDs := apiKeyGroupIDsForUpdate(apiKey, UpdateAPIKeyRequest{GroupID: &newDefault})

	require.Equal(t, []int64{40}, groupIDs)
}

func TestAPIKeyGroupIDsForUpdate_FallsBackToLegacyScalarWhenJoinAssignmentsMissing(t *testing.T) {
	oldDefault := int64(10)
	apiKey := &APIKey{GroupID: &oldDefault}

	groupIDs := apiKeyGroupIDsForUpdate(apiKey, UpdateAPIKeyRequest{})

	require.Equal(t, []int64{10}, groupIDs)
}

func defaultGroupIDPtr(id int64) *int64 {
	return &id
}
