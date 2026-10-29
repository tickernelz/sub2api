package perch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedisClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func TestRedisLoginStoreConsumeIsAuthoritativeAcrossInstances(t *testing.T) {
	_, client := newTestRedisClient(t)
	issuer := NewRedisLoginStore(client)
	consumer := NewRedisLoginStore(client)
	ctx := context.Background()
	now := time.Unix(1793300000, 0)
	issuer.now = func() time.Time { return now }
	consumer.now = func() time.Time { return now }

	session := LoginSession{AdminID: 4, LoginFlow: LoginFlow{Provider: ProviderGoogle, CodeVerifier: "ver", CreatedAt: now}}
	if err := issuer.Put(ctx, "sess-redis", session); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := consumer.Take(ctx, "sess-redis", 4)
	if err != nil {
		t.Fatalf("consumer take: %v", err)
	}
	if got.CodeVerifier != "ver" {
		t.Fatalf("session payload lost: %+v", got)
	}
	if _, err := issuer.Take(ctx, "sess-redis", 4); !errors.Is(err, ErrLoginSessionNotFound) {
		t.Fatalf("issuer must see the session consumed elsewhere, got %v", err)
	}
}

func TestRedisLoginStoreFallsBackToLocalWhenSetFails(t *testing.T) {
	server, client := newTestRedisClient(t)
	store := NewRedisLoginStore(client)
	ctx := context.Background()
	now := time.Unix(1793300000, 0)
	store.now = func() time.Time { return now }

	server.Close()
	session := LoginSession{AdminID: 5, LoginFlow: LoginFlow{Provider: ProviderGitHub, CodeVerifier: "local", CreatedAt: now}}
	if err := store.Put(ctx, "sess-local", session); err != nil {
		t.Fatalf("put with a dead redis must still keep the local copy: %v", err)
	}
	got, err := store.Take(ctx, "sess-local", 5)
	if err != nil {
		t.Fatalf("local fallback take: %v", err)
	}
	if got.CodeVerifier != "local" {
		t.Fatalf("local fallback payload: %+v", got)
	}
}

func TestLoginStoreSweepsExpiredEntriesOnPut(t *testing.T) {
	store := NewLoginStore()
	now := time.Unix(1793300000, 0)
	store.now = func() time.Time { return now }
	ctx := context.Background()

	if err := store.Put(ctx, "old", LoginSession{AdminID: 1, LoginFlow: LoginFlow{CreatedAt: now}}); err != nil {
		t.Fatalf("put old: %v", err)
	}
	now = now.Add(LoginSessionTTL + time.Minute)
	if err := store.Put(ctx, "fresh", LoginSession{AdminID: 1, LoginFlow: LoginFlow{CreatedAt: now}}); err != nil {
		t.Fatalf("put fresh: %v", err)
	}
	store.mu.Lock()
	_, stale := store.items["old"]
	_, fresh := store.items["fresh"]
	store.mu.Unlock()
	if stale {
		t.Fatal("expired entry survived the put sweep")
	}
	if !fresh {
		t.Fatal("fresh entry missing after the sweep")
	}
}
