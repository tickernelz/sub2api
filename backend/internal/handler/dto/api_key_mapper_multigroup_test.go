//go:build unit

package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFromService_MapsAssignedGroups(t *testing.T) {
	defaultGroupID := int64(10)
	src := &service.APIKey{
		ID:       1,
		UserID:   2,
		Key:      "sk-test",
		Name:     "multi",
		GroupID:  &defaultGroupID,
		GroupIDs: []int64{10, 20},
		Groups: []service.Group{
			{ID: 10, Name: "anthropic", Platform: service.PlatformAnthropic, Status: service.StatusActive},
			{ID: 20, Name: "openai", Platform: service.PlatformOpenAI, Status: service.StatusActive},
		},
		Status: service.StatusActive,
	}

	out := APIKeyFromService(src)

	require.NotNil(t, out)
	require.Equal(t, []int64{10, 20}, out.GroupIDs)
	require.Len(t, out.Groups, 2)
	require.Equal(t, "anthropic", out.Groups[0].Name)
	require.Equal(t, service.PlatformOpenAI, out.Groups[1].Platform)
}
