package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// stream_retry_settings.go
//
// StreamRetrySettings is the DB-backed configuration for *stale stream
// detection and failover*. It is deliberately separate from the pre-existing
// StreamTimeoutSettings, which only governs what to do AFTER a timeout
// (temp-unschedule / error / none). This one governs WHEN a stream is
// considered stale (TTFT + inter-chunk gap) and whether the gateway should
// fail over to another upstream account before any output reaches the client.
//
// Resolution order for the effective per-request config (see EffectiveWatchdogConfig):
//
//	per-platform override  ->  global StreamRetrySettings (DB)  ->  built-in defaults
//
// A missing DB row yields the built-in defaults (feature ON with safe values).
// An explicitly-disabled row yields a disabled config (no timers, no failover),
// distinct from "row missing" — this avoids the classic fallback pitfall where
// disabling a toggle silently reactivates config defaults.

// StreamRetrySettings is the canonical settings shape persisted as JSON under
// SettingKeyStreamRetrySettings.
type StreamRetrySettings struct {
	// Enabled is the master switch. When false, stale detection/failover is off
	// entirely regardless of the numeric fields.
	Enabled bool `json:"enabled"`

	// TTFTTimeoutSeconds: connected but no first upstream event within N seconds
	// => failover. 0 disables.
	TTFTTimeoutSeconds int `json:"ttft_timeout_seconds"`
	// ChunkGapWarnSeconds: inter-event gap soft threshold; logs a warning only.
	// 0 disables.
	ChunkGapWarnSeconds int `json:"chunk_gap_warn_seconds"`
	// ChunkGapTimeoutSeconds: inter-event gap hard threshold; triggers failover
	// (subject to the output-not-yet-started guard). 0 disables.
	ChunkGapTimeoutSeconds int `json:"chunk_gap_timeout_seconds"`

	// RetryMax caps how many failover attempts the stale path may drive.
	RetryMax int `json:"retry_max"`
	// RetryBackoffMs is the delay between failover attempts.
	RetryBackoffMs int `json:"retry_backoff_ms"`

	// PlatformOverrides optionally overrides the numeric thresholds per platform
	// (keys: "anthropic","openai","gemini","antigravity","grok","bedrock",...).
	// Only the fields present in each override are applied; the master Enabled
	// switch is NOT overridable per platform (a global kill-switch stays global).
	PlatformOverrides map[string]StreamRetryPlatformOverride `json:"platform_overrides,omitempty"`
}

// StreamRetryPlatformOverride carries optional per-platform threshold overrides.
// Pointer fields distinguish "unset" (inherit global) from an explicit 0 (disable).
type StreamRetryPlatformOverride struct {
	TTFTTimeoutSeconds     *int `json:"ttft_timeout_seconds,omitempty"`
	ChunkGapWarnSeconds    *int `json:"chunk_gap_warn_seconds,omitempty"`
	ChunkGapTimeoutSeconds *int `json:"chunk_gap_timeout_seconds,omitempty"`
}

// Bounds for validation/clamping. Chosen from the live-test finding that healthy
// streams keep inter-event gaps under ~2s (even reasoning models emit keepalive/
// deltas), while stale streams hang for minutes.
const (
	streamRetryTTFTMin = 10
	streamRetryTTFTMax = 300

	streamRetryGapWarnMin = 3
	streamRetryGapWarnMax = 120

	streamRetryGapTimeoutMin = 10
	streamRetryGapTimeoutMax = 300

	streamRetryRetryMaxMin = 0
	streamRetryRetryMaxMax = 5

	streamRetryBackoffMin = 0
	streamRetryBackoffMax = 10000
)

// DefaultStreamRetrySettings returns the built-in defaults. Feature is ON with
// conservative thresholds: healthy streams never trip, only genuine multi-second
// upstream silence does.
func DefaultStreamRetrySettings() *StreamRetrySettings {
	return &StreamRetrySettings{
		Enabled:                true,
		TTFTTimeoutSeconds:     60,
		ChunkGapWarnSeconds:    10,
		ChunkGapTimeoutSeconds: 30,
		RetryMax:               2,
		RetryBackoffMs:         1000,
	}
}

// clampInt clamps v into [min,max] but treats 0 as a valid "disabled" sentinel
// that bypasses the min bound.
func clampStreamRetryInt(v, min, max int) int {
	if v == 0 {
		return 0
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// sanitize clamps all numeric fields into valid bounds and enforces the
// warn < timeout invariant. Returns a copy; receiver unchanged.
func (s StreamRetrySettings) sanitize() StreamRetrySettings {
	out := s
	out.TTFTTimeoutSeconds = clampStreamRetryInt(out.TTFTTimeoutSeconds, streamRetryTTFTMin, streamRetryTTFTMax)
	out.ChunkGapWarnSeconds = clampStreamRetryInt(out.ChunkGapWarnSeconds, streamRetryGapWarnMin, streamRetryGapWarnMax)
	out.ChunkGapTimeoutSeconds = clampStreamRetryInt(out.ChunkGapTimeoutSeconds, streamRetryGapTimeoutMin, streamRetryGapTimeoutMax)
	// warn must be strictly below timeout; otherwise it is meaningless -> disable.
	if out.ChunkGapWarnSeconds > 0 && out.ChunkGapTimeoutSeconds > 0 &&
		out.ChunkGapWarnSeconds >= out.ChunkGapTimeoutSeconds {
		out.ChunkGapWarnSeconds = 0
	}
	if out.RetryMax < streamRetryRetryMaxMin {
		out.RetryMax = streamRetryRetryMaxMin
	}
	if out.RetryMax > streamRetryRetryMaxMax {
		out.RetryMax = streamRetryRetryMaxMax
	}
	if out.RetryBackoffMs < streamRetryBackoffMin {
		out.RetryBackoffMs = streamRetryBackoffMin
	}
	if out.RetryBackoffMs > streamRetryBackoffMax {
		out.RetryBackoffMs = streamRetryBackoffMax
	}
	return out
}

// EffectiveWatchdogConfig resolves the timing config for a given platform,
// applying per-platform overrides on top of the global thresholds. When the
// feature is disabled, it returns a zero (disabled) config.
func (s StreamRetrySettings) EffectiveWatchdogConfig(platform string) StreamWatchdogConfig {
	if !s.Enabled {
		return StreamWatchdogConfig{}
	}
	base := s.sanitize()
	ttft := base.TTFTTimeoutSeconds
	warn := base.ChunkGapWarnSeconds
	gap := base.ChunkGapTimeoutSeconds

	if ov, ok := base.PlatformOverrides[platform]; ok {
		if ov.TTFTTimeoutSeconds != nil {
			ttft = clampStreamRetryInt(*ov.TTFTTimeoutSeconds, streamRetryTTFTMin, streamRetryTTFTMax)
		}
		if ov.ChunkGapWarnSeconds != nil {
			warn = clampStreamRetryInt(*ov.ChunkGapWarnSeconds, streamRetryGapWarnMin, streamRetryGapWarnMax)
		}
		if ov.ChunkGapTimeoutSeconds != nil {
			gap = clampStreamRetryInt(*ov.ChunkGapTimeoutSeconds, streamRetryGapTimeoutMin, streamRetryGapTimeoutMax)
		}
	}

	cfg := StreamWatchdogConfig{
		TTFT:       time.Duration(ttft) * time.Second,
		GapWarn:    time.Duration(warn) * time.Second,
		GapTimeout: time.Duration(gap) * time.Second,
	}
	// normalize() re-checks warn < timeout after overrides.
	return cfg.normalize()
}

// RetryBackoff returns the configured backoff as a Duration.
func (s StreamRetrySettings) RetryBackoff() time.Duration {
	return time.Duration(s.RetryBackoffMs) * time.Millisecond
}

// GetStreamRetrySettings loads the DB-backed settings, falling back to defaults
// when the row is missing or unparseable. A row that exists with enabled=false
// is honored as an explicit disable (NOT replaced by defaults).
func (s *SettingService) GetStreamRetrySettings(ctx context.Context) (*StreamRetrySettings, error) {
	if s == nil || s.settingRepo == nil {
		return DefaultStreamRetrySettings(), nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyStreamRetrySettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultStreamRetrySettings(), nil
		}
		return nil, fmt.Errorf("get stream retry settings: %w", err)
	}
	if value == "" {
		return DefaultStreamRetrySettings(), nil
	}
	var settings StreamRetrySettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		// Corrupt row: fail safe to defaults rather than erroring the request path.
		return DefaultStreamRetrySettings(), nil
	}
	sanitized := settings.sanitize()
	return &sanitized, nil
}

// SetStreamRetrySettings validates and persists the settings as JSON.
func (s *SettingService) SetStreamRetrySettings(ctx context.Context, settings *StreamRetrySettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service not initialized")
	}
	if settings == nil {
		settings = DefaultStreamRetrySettings()
	}
	sanitized := settings.sanitize()
	data, err := json.Marshal(sanitized)
	if err != nil {
		return fmt.Errorf("marshal stream retry settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyStreamRetrySettings, string(data)); err != nil {
		return err
	}
	// Bust the in-process cache so the change takes effect immediately.
	InvalidateStreamRetrySettingsCache()
	return nil
}
