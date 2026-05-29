package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	dbent "github.com/tickernelz/sub2api/ent"
	"github.com/tickernelz/sub2api/ent/enttest"
	"github.com/tickernelz/sub2api/internal/pkg/pagination"
	"github.com/tickernelz/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newAPIKeyEntRepo(t *testing.T) (*apiKeyRepository, *dbent.Client) {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(10)

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	return newAPIKeyRepositoryWithSQL(client, db), client
}

func TestAPIKeyRepositoryMultiGroupCreateAndFetch(t *testing.T) {
	repo, client := newAPIKeyEntRepo(t)
	ctx := context.Background()
	user := mustCreateAPIKeyTestUser(t, ctx, client, "apikey-multi-create@example.com")
	defaultGroup := mustCreateAPIKeyTestGroup(t, ctx, client, "apikey-multi-default")
	secondGroup := mustCreateAPIKeyTestGroup(t, ctx, client, "apikey-multi-second")

	key := &service.APIKey{
		UserID:   user.ID,
		Key:      "sk-api...eate",
		Name:     "multi",
		GroupID:  &defaultGroup.ID,
		GroupIDs: []int64{defaultGroup.ID, secondGroup.ID},
		Status:   service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, defaultGroup.ID, *got.GroupID)
	require.Equal(t, []int64{defaultGroup.ID, secondGroup.ID}, got.GroupIDs)
	require.Len(t, got.Groups, 2)
	require.Equal(t, defaultGroup.ID, got.Groups[0].ID)
	require.Equal(t, secondGroup.ID, got.Groups[1].ID)
}

func TestAPIKeyRepositoryMultiGroupPreservesAssignmentOrder(t *testing.T) {
	repo, client := newAPIKeyEntRepo(t)
	ctx := context.Background()
	user := mustCreateAPIKeyTestUser(t, ctx, client, "apikey-multi-order@example.com")
	lowerIDGroup := mustCreateAPIKeyTestGroup(t, ctx, client, "apikey-multi-order-lower")
	defaultGroup := mustCreateAPIKeyTestGroup(t, ctx, client, "apikey-multi-order-default")

	key := &service.APIKey{
		UserID:   user.ID,
		Key:      "sk-api...order",
		Name:     "multi order",
		GroupID:  &defaultGroup.ID,
		GroupIDs: []int64{defaultGroup.ID, lowerIDGroup.ID},
		Status:   service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{defaultGroup.ID, lowerIDGroup.ID}, got.GroupIDs)
}

func TestAPIKeyRepositoryListByGroupIDMatchesAssignedGroups(t *testing.T) {
	repo, client := newAPIKeyEntRepo(t)
	ctx := context.Background()
	user := mustCreateAPIKeyTestUser(t, ctx, client, "apikey-multi-list@example.com")
	defaultGroup := mustCreateAPIKeyTestGroup(t, ctx, client, "apikey-multi-list-default")
	secondGroup := mustCreateAPIKeyTestGroup(t, ctx, client, "apikey-multi-list-second")

	key := &service.APIKey{
		UserID:   user.ID,
		Key:      "sk-api-key-multi-list",
		Name:     "multi list",
		GroupID:  &defaultGroup.ID,
		GroupIDs: []int64{defaultGroup.ID, secondGroup.ID},
		Status:   service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	keys, page, err := repo.ListByGroupID(ctx, secondGroup.ID, pagination.PaginationParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, keys, 1)
	require.Equal(t, key.ID, keys[0].ID)
	require.Equal(t, []int64{defaultGroup.ID, secondGroup.ID}, keys[0].GroupIDs)
}

func TestAPIKeyRepositoryGroupQueriesIncludeLegacyScalarGroupID(t *testing.T) {
	repo, client := newAPIKeyEntRepo(t)
	ctx := context.Background()
	user := mustCreateAPIKeyTestUser(t, ctx, client, "apikey-legacy-group@example.com")
	legacyGroup := mustCreateAPIKeyTestGroup(t, ctx, client, "apikey-legacy-group")

	legacyKey, err := client.APIKey.Create().
		SetUserID(user.ID).
		SetKey("sk-api...legacy").
		SetName("legacy scalar group").
		SetStatus(service.StatusActive).
		SetGroupID(legacyGroup.ID).
		Save(ctx)
	require.NoError(t, err)

	keys, page, err := repo.ListByGroupID(ctx, legacyGroup.ID, pagination.PaginationParams{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, keys, 1)
	require.Equal(t, legacyKey.ID, keys[0].ID)
	require.Equal(t, []int64{legacyGroup.ID}, keys[0].GroupIDs)
	require.Len(t, keys[0].Groups, 1)
	require.Equal(t, legacyGroup.ID, keys[0].Groups[0].ID)

	count, err := repo.CountByGroupID(ctx, legacyGroup.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	keyStrings, err := repo.ListKeysByGroupID(ctx, legacyGroup.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"sk-api...legacy"}, keyStrings)

	filtered, filteredPage, err := repo.ListByUserID(ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{GroupID: &legacyGroup.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), filteredPage.Total)
	require.Len(t, filtered, 1)
	require.Equal(t, legacyKey.ID, filtered[0].ID)
}

func mustCreateAPIKeyTestUser(t *testing.T, ctx context.Context, client *dbent.Client, email string) *dbent.User {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(email).
		SetUsername(email).
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	return user
}

func mustCreateAPIKeyTestGroup(t *testing.T, ctx context.Context, client *dbent.Client, name string) *dbent.Group {
	t.Helper()
	group, err := client.Group.Create().
		SetName(name).
		SetPlatform(service.PlatformAnthropic).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		Save(ctx)
	require.NoError(t, err)
	return group
}
