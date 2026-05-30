package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	httppool "github.com/tickernelz/sub2api/internal/pkg/httpclient"
	providerregistry "github.com/tickernelz/sub2api/internal/provider"
)

const opencodeQuotaRequestTimeout = 8 * time.Second

type OpenCodeQuotaFetcher struct {
	proxyRepo ProxyRepository
}

func NewOpenCodeQuotaFetcher(proxyRepo ProxyRepository) *OpenCodeQuotaFetcher {
	return &OpenCodeQuotaFetcher{proxyRepo: proxyRepo}
}

func (f *OpenCodeQuotaFetcher) CanFetch(account *Account) bool {
	if account == nil || account.Platform != PlatformOpenCode || account.Type != AccountTypeAPIKey {
		return false
	}
	return strings.TrimSpace(account.GetCredential("api_key")) != ""
}

func (f *OpenCodeQuotaFetcher) FetchQuota(ctx context.Context, account *Account, proxyURL string) (*QuotaResult, error) {
	if !f.CanFetch(account) {
		return nil, nil
	}

	quotaURL := openCodeQuotaURL(account)
	if quotaURL == "" {
		return nil, nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, opencodeQuotaRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, quotaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create opencode quota request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(account.GetCredential("api_key")))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	client, err := httppool.GetClient(httppool.Options{
		ProxyURL:              proxyURL,
		Timeout:               opencodeQuotaRequestTimeout,
		ResponseHeaderTimeout: opencodeQuotaRequestTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("build opencode quota client: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, nil
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil
	}

	usage := parseOpenCodeQuotaUsage(raw, time.Now())
	if usage == nil {
		return nil, nil
	}

	return &QuotaResult{UsageInfo: usage, Raw: raw}, nil
}

func (f *OpenCodeQuotaFetcher) GetProxyURL(ctx context.Context, account *Account) string {
	if f == nil || account == nil || account.ProxyID == nil || f.proxyRepo == nil {
		return ""
	}
	proxy, err := f.proxyRepo.GetByID(ctx, *account.ProxyID)
	if err != nil || proxy == nil {
		return ""
	}
	return proxy.URL()
}

func openCodeQuotaURL(account *Account) string {
	if account == nil {
		return ""
	}
	if quotaURL := strings.TrimSpace(account.GetCredential("quota_url")); quotaURL != "" {
		return strings.TrimRight(quotaURL, "/")
	}
	if baseURL := strings.TrimRight(strings.TrimSpace(account.GetCredential("base_url")), "/"); baseURL != "" {
		if baseURL == providerregistry.OpenCodeDefaultBaseURL {
			return providerregistry.OpenCodeDefaultQuotaURL
		}
		return baseURL + "/quota"
	}
	variant := providerregistry.ResolveOpenCodeVariant(account.Credentials)
	if variant.ID == providerregistry.OpenCodeVariantGo {
		return strings.TrimRight(variant.BaseURL, "/") + "/quota"
	}
	return providerregistry.OpenCodeDefaultQuotaURL
}

func parseOpenCodeQuotaUsage(raw map[string]any, now time.Time) *UsageInfo {
	quota := toAnyMap(firstNonNil(raw["quota"], raw["data"], raw["usage"]))
	if len(quota) == 0 {
		return nil
	}

	fiveHour, hasFiveHour := openCodeUsageProgress(quota, now, "window_5h", "5h", "hourly", "short")
	weekly, hasWeekly := openCodeUsageProgress(quota, now, "window_weekly", "weekly", "week", "wk")
	monthly, hasMonthly := openCodeUsageProgress(quota, now, "window_monthly", "monthly", "month", "mo")
	if !hasFiveHour && !hasWeekly && !hasMonthly {
		return nil
	}

	updatedAt := now
	usage := &UsageInfo{
		UpdatedAt:         &updatedAt,
		OpenCodeFiveHour:  fiveHour,
		OpenCodeWeekly:    weekly,
		OpenCodeMonthly:   monthly,
		OpenCodeRateLimit: boolFromAny(firstNonNil(raw["limit_reached"], quota["limit_reached"])),
	}
	if fiveHour != nil {
		usage.FiveHour = cloneUsageProgress(fiveHour)
	}
	if weekly != nil {
		usage.SevenDay = cloneUsageProgress(weekly)
	}
	if monthly != nil {
		usage.OpenCodeRateLimit = usage.OpenCodeRateLimit || monthly.Utilization >= 100
	}
	if fiveHour != nil {
		usage.OpenCodeRateLimit = usage.OpenCodeRateLimit || fiveHour.Utilization >= 100
	}
	if weekly != nil {
		usage.OpenCodeRateLimit = usage.OpenCodeRateLimit || weekly.Utilization >= 100
	}
	return usage
}

func openCodeUsageProgress(quota map[string]any, now time.Time, keys ...string) (*UsageProgress, bool) {
	var window map[string]any
	for _, key := range keys {
		window = toAnyMap(quota[key])
		if len(window) > 0 {
			break
		}
	}
	if len(window) == 0 {
		return nil, false
	}

	used, usedOK := numberFromAny(firstNonNil(window["used"], window["used_amount"], window["usedAmount"]))
	limit, limitOK := numberFromAny(firstNonNil(window["limit"], window["limit_amount"], window["limitAmount"]))
	if !usedOK || !limitOK || limit <= 0 {
		return nil, false
	}

	resetAt := openCodeResetAt(window, now)
	progress := &UsageProgress{
		Utilization:   clampPercent((used / limit) * 100),
		UsedRequests:  int64(used),
		LimitRequests: int64(limit),
	}
	if resetAt != nil {
		progress.ResetsAt = resetAt
		remaining := int(time.Until(*resetAt).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		progress.RemainingSeconds = remaining
	}
	return progress, true
}

func openCodeResetAt(window map[string]any, now time.Time) *time.Time {
	if t, ok := timeFromAny(firstNonNil(window["reset_at"], window["resetAt"])); ok {
		return &t
	}
	if seconds, ok := numberFromAny(firstNonNil(window["reset_after_seconds"], window["resetAfterSeconds"])); ok && seconds > 0 {
		t := now.Add(time.Duration(seconds) * time.Second)
		return &t
	}
	return nil
}

func cloneUsageProgress(in *UsageProgress) *UsageProgress {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func toAnyMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	m, _ := value.(map[string]any)
	return m
}

func numberFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed, err == nil
	}
	return 0, false
}

func timeFromAny(value any) (time.Time, bool) {
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return time.Time{}, false
		}
		if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
			return parsed, true
		}
		if numeric, ok := numberFromAny(trimmed); ok {
			return unixSecondsOrMilliseconds(numeric), true
		}
	case float64:
		return unixSecondsOrMilliseconds(v), true
	case int:
		return unixSecondsOrMilliseconds(float64(v)), true
	case int64:
		return unixSecondsOrMilliseconds(float64(v)), true
	}
	return time.Time{}, false
}

func unixSecondsOrMilliseconds(value float64) time.Time {
	if value >= 1e12 {
		return time.UnixMilli(int64(value)).UTC()
	}
	return time.Unix(int64(value), 0).UTC()
}

func boolFromAny(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}
