//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyService_SnapshotRoundTrip_PreservesAssignedGroups(t *testing.T) {
	svc := NewAPIKeyService(nil, nil, nil, nil, nil, nil, &config.Config{})
	defaultGroupID := int64(20)
	assignedGroups := []Group{
		{ID: 20, Name: "default", Platform: PlatformAnthropic, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 1},
		{ID: 30, Name: "openai", Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 1.2, AllowMessagesDispatch: true, DefaultMappedModel: "gpt-5.4"},
	}
	apiKey := &APIKey{
		ID:       1,
		UserID:   2,
		Key:      "sk-test",
		Name:     "multi",
		GroupID:  &defaultGroupID,
		GroupIDs: []int64{20, 30},
		Groups:   assignedGroups,
		Group:    &assignedGroups[0],
		Status:   StatusActive,
		User: &User{
			ID:          2,
			Status:      StatusActive,
			Role:        RoleUser,
			Balance:     10,
			Concurrency: 1,
		},
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.Equal(t, []int64{20, 30}, snapshot.GroupIDs)
	require.Len(t, snapshot.Groups, 2)
	require.Equal(t, int64(30), snapshot.Groups[1].ID)
	require.Equal(t, PlatformOpenAI, snapshot.Groups[1].Platform)
	require.True(t, snapshot.Groups[1].AllowMessagesDispatch)

	cached := svc.snapshotToAPIKey(apiKey.Key, snapshot)
	require.NotNil(t, cached)
	require.Equal(t, []int64{20, 30}, cached.GroupIDs)
	require.Len(t, cached.Groups, 2)
	require.Equal(t, int64(20), cached.Groups[0].ID)
	require.Equal(t, int64(30), cached.Groups[1].ID)
	require.NotNil(t, cached.Group)
	require.Equal(t, defaultGroupID, cached.Group.ID)
}
