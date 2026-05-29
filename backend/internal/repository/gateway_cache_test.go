package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tickernelz/sub2api/internal/service"
)

func TestGatewayCacheSessionAccountIDIsolatedByAPIKey(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })

	cache := NewGatewayCache(rdb)
	groupID := int64(7)
	sessionHash := "same-session"
	ctxKeyA := service.WithAPIKeyID(context.Background(), 101, false)
	ctxKeyB := service.WithAPIKeyID(context.Background(), 202, false)

	require.NoError(t, cache.SetSessionAccountID(ctxKeyA, groupID, sessionHash, 1001, time.Minute))
	require.NoError(t, cache.SetSessionAccountID(ctxKeyB, groupID, sessionHash, 2002, time.Minute))

	gotA, err := cache.GetSessionAccountID(ctxKeyA, groupID, sessionHash)
	require.NoError(t, err)
	require.Equal(t, int64(1001), gotA)

	gotB, err := cache.GetSessionAccountID(ctxKeyB, groupID, sessionHash)
	require.NoError(t, err)
	require.Equal(t, int64(2002), gotB)

	_, err = cache.GetSessionAccountID(context.Background(), groupID, sessionHash)
	require.True(t, errors.Is(err, redis.Nil), "API-key scoped writes must not populate the legacy group-only key")
}
