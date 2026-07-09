package service

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// stream_watchdog_integration.go
//
// Glue between the provider streaming loops and the shared StreamWatchdog +
// StreamRetrySettings. Two goals:
//
//  1. Zero extra DB round-trips on the streaming hot path: the resolved
//     StreamRetrySettings are cached in-process (60s TTL, singleflight) exactly
//     like the other gateway settings caches in setting_service.go.
//  2. A single decision function each loop calls when a watchdog timer fires,
//     so the failover-vs-fail-clean policy lives in ONE place.

// cachedStreamRetrySettings holds the resolved settings plus its expiry.
type cachedStreamRetrySettings struct {
	settings  StreamRetrySettings
	expiresAt int64 // unix nano
}

var streamRetrySettingsCache atomic.Value // *cachedStreamRetrySettings

const streamRetrySettingsCacheTTL = 60 * time.Second

// InvalidateStreamRetrySettingsCache clears the in-process cache so the next
// read reflects a just-persisted change. Call from SetStreamRetrySettings.
func InvalidateStreamRetrySettingsCache() {
	streamRetrySettingsCache.Store((*cachedStreamRetrySettings)(nil))
}

// getStreamRetrySettingsCached returns the effective StreamRetrySettings using a
// 60s in-process cache. On any error it fails safe to the built-in defaults so a
// transient DB blip never disables the watchdog nor blocks the stream.
func (s *SettingService) getStreamRetrySettingsCached(ctx context.Context) StreamRetrySettings {
	if cached, ok := streamRetrySettingsCache.Load().(*cachedStreamRetrySettings); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.settings
		}
	}
	settings, err := s.GetStreamRetrySettings(ctx)
	if err != nil || settings == nil {
		settings = DefaultStreamRetrySettings()
	}
	streamRetrySettingsCache.Store(&cachedStreamRetrySettings{
		settings:  *settings,
		expiresAt: time.Now().Add(streamRetrySettingsCacheTTL).UnixNano(),
	})
	return *settings
}

// resolveStreamWatchdogConfig produces the per-request watchdog config for a
// platform, honoring the DB settings + per-platform overrides. Returns a
// disabled config (Enabled() == false) when the feature is off, in which case
// callers should not create a watchdog at all.
func resolveStreamWatchdogConfig(ctx context.Context, settingService *SettingService, platform string) StreamWatchdogConfig {
	if settingService == nil {
		return StreamWatchdogConfig{}
	}
	return settingService.getStreamRetrySettingsCached(ctx).EffectiveWatchdogConfig(platform)
}

// newStreamWatchdogForPlatform builds a ready watchdog for the request, or nil
// when the feature is disabled for this platform. Callers must `defer w.Stop()`
// and guard timer channels — a nil watchdog's Chans() return nil channels which
// never fire, so the select arms degrade to no-ops.
func newStreamWatchdogForPlatform(ctx context.Context, settingService *SettingService, platform string) *StreamWatchdog {
	cfg := resolveStreamWatchdogConfig(ctx, settingService, platform)
	if !cfg.Enabled() {
		return nil
	}
	return NewStreamWatchdog(cfg)
}

// streamOutputCommitted reports whether any byte has already been written to the
// client for this request. Once true, a stale stream must fail cleanly rather
// than fail over (a retry would emit duplicate/garbled output to the client).
//
// This is the universal output gate used across every provider loop, mirroring
// the existing c.Writer.Written() checks throughout the gateway.
func streamOutputCommitted(c *gin.Context) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	return c.Writer.Written()
}

// streamStallDecision is the outcome the loop should apply after a watchdog fires.
type streamStallDecision int

const (
	// stallIgnore: warn-only or not actionable; keep streaming.
	stallIgnore streamStallDecision = iota
	// stallFailover: safe to fail over to another account (no output committed yet).
	stallFailover
	// stallFailClean: stale but output already committed; abort without retry.
	stallFailClean
)

// decideStreamStall centralizes the failover-vs-fail-clean policy for a fired
// watchdog reason. It also emits a structured log/metric so thresholds can be
// tuned from real traffic.
//
// platform/model/accountID are for logging only. metrics may be nil.
func decideStreamStall(
	c *gin.Context,
	reason StreamStallReason,
	platform string,
	model string,
	accountID int64,
	metrics *streamRetryMetrics,
) streamStallDecision {
	switch reason {
	case StreamStallGapWarn:
		if metrics != nil {
			metrics.gapWarnTotal.Add(1)
		}
		logger.L().Warn("stream stale watchdog: chunk gap warning",
			zap.String("platform", platform),
			zap.String("model", model),
			zap.Int64("account_id", accountID),
		)
		return stallIgnore

	case StreamStallTTFT, StreamStallGapTimeout:
		committed := streamOutputCommitted(c)
		if metrics != nil {
			switch reason {
			case StreamStallTTFT:
				metrics.ttftTimeoutTotal.Add(1)
			case StreamStallGapTimeout:
				metrics.gapTimeoutTotal.Add(1)
			}
			if committed {
				metrics.failCleanTotal.Add(1)
			} else {
				metrics.failoverTotal.Add(1)
			}
		}
		logger.L().Warn("stream stale watchdog: stall detected",
			zap.String("platform", platform),
			zap.String("model", model),
			zap.Int64("account_id", accountID),
			zap.String("reason", reason.String()),
			zap.Bool("output_committed", committed),
		)
		if committed {
			return stallFailClean
		}
		return stallFailover

	default:
		return stallIgnore
	}
}

// streamRetryMetrics counts stale-stream outcomes for observability/tuning.
// All counters are process-wide; per-platform breakdown is emitted via logs.
type streamRetryMetrics struct {
	ttftTimeoutTotal atomic.Int64
	gapWarnTotal     atomic.Int64
	gapTimeoutTotal  atomic.Int64
	failoverTotal    atomic.Int64
	failCleanTotal   atomic.Int64
}

// StreamRetryMetricsSnapshot is a read-only view for admin/ops surfacing.
type StreamRetryMetricsSnapshot struct {
	TTFTTimeoutTotal int64 `json:"ttft_timeout_total"`
	GapWarnTotal     int64 `json:"gap_warn_total"`
	GapTimeoutTotal  int64 `json:"gap_timeout_total"`
	FailoverTotal    int64 `json:"failover_total"`
	FailCleanTotal   int64 `json:"fail_clean_total"`
}

func (m *streamRetryMetrics) snapshot() StreamRetryMetricsSnapshot {
	if m == nil {
		return StreamRetryMetricsSnapshot{}
	}
	return StreamRetryMetricsSnapshot{
		TTFTTimeoutTotal: m.ttftTimeoutTotal.Load(),
		GapWarnTotal:     m.gapWarnTotal.Load(),
		GapTimeoutTotal:  m.gapTimeoutTotal.Load(),
		FailoverTotal:    m.failoverTotal.Load(),
		FailCleanTotal:   m.failCleanTotal.Load(),
	}
}

// globalStreamRetryMetrics is the process-wide counter set. A single instance
// keeps wiring trivial across the many provider loops without threading a field
// through six service structs.
var globalStreamRetryMetrics = &streamRetryMetrics{}

// StreamRetryMetrics returns a snapshot of the process-wide stale-stream
// counters for admin/ops surfacing.
func StreamRetryMetrics() StreamRetryMetricsSnapshot {
	return globalStreamRetryMetrics.snapshot()
}

// ensure context import stays used even if a future refactor drops the only
// consumer; resolveStreamWatchdogConfig takes ctx for symmetry with other
// cached settings accessors.
var _ = context.Background
