package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tickernelz/sub2api/internal/pkg/pagination"
	providerregistry "github.com/tickernelz/sub2api/internal/provider"
)

type opencodeProxyRepoStub struct {
	getByIDFunc func(ctx context.Context, id int64) (*Proxy, error)
}

func (s *opencodeProxyRepoStub) Create(ctx context.Context, proxy *Proxy) error { return nil }
func (s *opencodeProxyRepoStub) GetByID(ctx context.Context, id int64) (*Proxy, error) {
	if s.getByIDFunc != nil {
		return s.getByIDFunc(ctx, id)
	}
	return nil, fmt.Errorf("proxy not found")
}
func (s *opencodeProxyRepoStub) ListByIDs(ctx context.Context, ids []int64) ([]Proxy, error) {
	return nil, nil
}
func (s *opencodeProxyRepoStub) Update(ctx context.Context, proxy *Proxy) error { return nil }
func (s *opencodeProxyRepoStub) Delete(ctx context.Context, id int64) error     { return nil }
func (s *opencodeProxyRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *opencodeProxyRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]Proxy, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *opencodeProxyRepoStub) ListWithFiltersAndAccountCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]ProxyWithAccountCount, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *opencodeProxyRepoStub) ListActive(ctx context.Context) ([]Proxy, error) {
	return nil, nil
}
func (s *opencodeProxyRepoStub) ListActiveWithAccountCount(ctx context.Context) ([]ProxyWithAccountCount, error) {
	return nil, nil
}
func (s *opencodeProxyRepoStub) ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error) {
	return false, nil
}
func (s *opencodeProxyRepoStub) CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error) {
	return 0, nil
}
func (s *opencodeProxyRepoStub) ListAccountSummariesByProxyID(ctx context.Context, proxyID int64) ([]ProxyAccountSummary, error) {
	return nil, nil
}

func TestOpenCodeQuotaFetcher_FetchQuotaParsesTripleWindows(t *testing.T) {
	reset5h := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	resetWeekly := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quota" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer opencode-key" {
			t.Fatalf("Authorization = %q, want Bearer opencode-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"quota": {
				"window_5h": {"used": 3, "limit": 12, "reset_at": ` + unixJSON(reset5h) + `},
				"weekly": {"used_amount": "15", "limit_amount": "30", "resetAt": "` + resetWeekly.Format(time.RFC3339) + `"},
				"month": {"used": 60, "limit": 60, "reset_after_seconds": 900}
			}
		}`))
	}))
	defer server.Close()

	account := &Account{
		ID:       123,
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "opencode-key",
			"base_url": server.URL,
		},
	}

	result, err := NewOpenCodeQuotaFetcher(nil).FetchQuota(context.Background(), account, "")
	if err != nil {
		t.Fatalf("FetchQuota() error = %v", err)
	}
	if result == nil || result.UsageInfo == nil {
		t.Fatal("expected usage info")
	}
	usage := result.UsageInfo
	if usage.OpenCodeFiveHour == nil || usage.OpenCodeFiveHour.Utilization != 25 {
		t.Fatalf("5h utilization = %#v, want 25%%", usage.OpenCodeFiveHour)
	}
	if usage.OpenCodeWeekly == nil || usage.OpenCodeWeekly.Utilization != 50 {
		t.Fatalf("weekly utilization = %#v, want 50%%", usage.OpenCodeWeekly)
	}
	if usage.OpenCodeMonthly == nil || usage.OpenCodeMonthly.Utilization != 100 {
		t.Fatalf("monthly utilization = %#v, want 100%%", usage.OpenCodeMonthly)
	}
	if usage.FiveHour == nil || usage.FiveHour.Utilization != 25 {
		t.Fatalf("compat five_hour = %#v, want 25%%", usage.FiveHour)
	}
	if usage.SevenDay == nil || usage.SevenDay.Utilization != 50 {
		t.Fatalf("compat seven_day = %#v, want 50%%", usage.SevenDay)
	}
	if usage.OpenCodeFiveHour.ResetsAt == nil || !usage.OpenCodeFiveHour.ResetsAt.Equal(reset5h) {
		t.Fatalf("5h reset = %v, want %v", usage.OpenCodeFiveHour.ResetsAt, reset5h)
	}
	if usage.OpenCodeWeekly.ResetsAt == nil || !usage.OpenCodeWeekly.ResetsAt.Equal(resetWeekly) {
		t.Fatalf("weekly reset = %v, want %v", usage.OpenCodeWeekly.ResetsAt, resetWeekly)
	}
	if usage.OpenCodeMonthly.ResetsAt == nil || time.Until(*usage.OpenCodeMonthly.ResetsAt) <= 0 {
		t.Fatalf("monthly reset from reset_after_seconds not set in future: %#v", usage.OpenCodeMonthly)
	}
	if result.Raw["quota"] == nil {
		t.Fatalf("expected raw quota payload")
	}
}

func TestOpenCodeQuotaFetcher_FailOpenOnNonOKAndMalformedPayload(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "nope", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"quota": {"window_5h": {}}}`))
	}))
	defer server.Close()

	account := &Account{
		ID:       124,
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "opencode-key",
			"base_url": server.URL,
		},
	}

	fetcher := NewOpenCodeQuotaFetcher(nil)
	result, err := fetcher.FetchQuota(context.Background(), account, "")
	if err != nil {
		t.Fatalf("FetchQuota() non-OK error = %v", err)
	}
	if result != nil {
		t.Fatalf("non-OK should fail open with nil result, got %#v", result)
	}

	result, err = fetcher.FetchQuota(context.Background(), account, "")
	if err != nil {
		t.Fatalf("FetchQuota() malformed error = %v", err)
	}
	if result != nil {
		t.Fatalf("malformed quota should fail open with nil result, got %#v", result)
	}
}

func TestOpenCodeQuotaFetcher_CanFetchRequiresOpenCodeAPIKey(t *testing.T) {
	fetcher := NewOpenCodeQuotaFetcher(nil)
	if !fetcher.CanFetch(&Account{Platform: PlatformOpenCode, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key"}}) {
		t.Fatal("expected OpenCode API-key account to be fetchable")
	}
	if fetcher.CanFetch(&Account{Platform: PlatformOpenCode, Type: AccountTypeAPIKey}) {
		t.Fatal("expected missing api_key to be unfetchable")
	}
	if fetcher.CanFetch(&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key"}}) {
		t.Fatal("expected non-OpenCode account to be unfetchable")
	}
}

func TestOpenCodeQuotaURLDefaultsAndOverrides(t *testing.T) {
	cases := []struct {
		name        string
		credentials map[string]any
		want        string
	}{
		{
			name:        "explicit quota URL wins",
			credentials: map[string]any{"quota_url": "https://quota.example.com/"},
			want:        "https://quota.example.com",
		},
		{
			name:        "default base URL uses documented quota endpoint",
			credentials: map[string]any{"base_url": providerregistry.OpenCodeDefaultBaseURL},
			want:        providerregistry.OpenCodeDefaultQuotaURL,
		},
		{
			name:        "custom base URL derives quota path",
			credentials: map[string]any{"base_url": "https://proxy.example.com/opencode/v1/"},
			want:        "https://proxy.example.com/opencode/v1/quota",
		},
		{
			name:        "go variant uses go quota path",
			credentials: map[string]any{"provider_variant": providerregistry.OpenCodeVariantGo},
			want:        providerregistry.OpenCodeGoBaseURL + "/quota",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := openCodeQuotaURL(&Account{Platform: PlatformOpenCode, Type: AccountTypeAPIKey, Credentials: tc.credentials})
			if got != tc.want {
				t.Fatalf("openCodeQuotaURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOpenCodeQuotaFetcher_GetProxyURLUsesRepository(t *testing.T) {
	proxyID := int64(7)
	fetcher := NewOpenCodeQuotaFetcher(&opencodeProxyRepoStub{getByIDFunc: func(ctx context.Context, id int64) (*Proxy, error) {
		if id != proxyID {
			t.Fatalf("proxy id = %d, want %d", id, proxyID)
		}
		return &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 8888}, nil
	}})

	got := fetcher.GetProxyURL(context.Background(), &Account{ProxyID: &proxyID})
	if got != "http://127.0.0.1:8888" {
		t.Fatalf("proxy URL = %q, want http://127.0.0.1:8888", got)
	}
}

func TestProvideAccountUsageServiceUsesInjectedOpenCodeQuotaFetcher(t *testing.T) {
	fetcher := NewOpenCodeQuotaFetcher(&opencodeProxyRepoStub{})
	svc := ProvideAccountUsageService(nil, nil, nil, nil, nil, fetcher, NewUsageCache(), nil, nil)
	if svc.opencodeQuotaFetcher != fetcher {
		t.Fatal("expected provided OpenCode quota fetcher to be used")
	}
}

func TestAccountUsageService_GetUsageFetchesOpenCodeQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quota" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"quota": {
				"window_5h": {"used": 6, "limit": 12},
				"window_weekly": {"used": 9, "limit": 30},
				"window_monthly": {"used": 12, "limit": 60}
			}
		}`))
	}))
	defer server.Close()

	account := Account{
		ID:       321,
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "opencode-key",
			"base_url": server.URL,
		},
	}
	svc := NewAccountUsageService(&stubOpenAIAccountRepo{accounts: []Account{account}}, nil, nil, nil, nil, NewUsageCache(), nil, nil)

	usage, err := svc.GetUsage(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("GetUsage() error = %v", err)
	}
	if usage == nil || usage.OpenCodeFiveHour == nil || usage.OpenCodeFiveHour.Utilization != 50 {
		t.Fatalf("OpenCodeFiveHour = %#v, want 50%%", usage)
	}
	if usage.OpenCodeWeekly == nil || usage.OpenCodeWeekly.Utilization != 30 {
		t.Fatalf("OpenCodeWeekly = %#v, want 30%%", usage.OpenCodeWeekly)
	}
	if usage.OpenCodeMonthly == nil || usage.OpenCodeMonthly.Utilization != 20 {
		t.Fatalf("OpenCodeMonthly = %#v, want 20%%", usage.OpenCodeMonthly)
	}
}

func unixJSON(t time.Time) string {
	return `"` + t.Format(time.RFC3339) + `"`
}
