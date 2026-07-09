package service

import (
	"testing"
	"time"
)

func srIntPtr(v int) *int { return &v }

func TestStreamRetrySettings_Default(t *testing.T) {
	d := DefaultStreamRetrySettings()
	if !d.Enabled {
		t.Fatal("default should be enabled")
	}
	if d.TTFTTimeoutSeconds != 60 || d.ChunkGapWarnSeconds != 10 || d.ChunkGapTimeoutSeconds != 30 {
		t.Fatalf("unexpected default thresholds: %+v", d)
	}
	if d.RetryMax != 2 || d.RetryBackoffMs != 1000 {
		t.Fatalf("unexpected retry defaults: %+v", d)
	}
}

func TestStreamRetrySettings_Sanitize(t *testing.T) {
	cases := []struct {
		name string
		in   StreamRetrySettings
		want StreamRetrySettings
	}{
		{
			name: "clamp below min",
			in:   StreamRetrySettings{Enabled: true, TTFTTimeoutSeconds: 5, ChunkGapWarnSeconds: 1, ChunkGapTimeoutSeconds: 5, RetryMax: -1, RetryBackoffMs: -1},
			want: StreamRetrySettings{Enabled: true, TTFTTimeoutSeconds: 10, ChunkGapWarnSeconds: 3, ChunkGapTimeoutSeconds: 10, RetryMax: 0, RetryBackoffMs: 0},
		},
		{
			name: "clamp above max",
			in:   StreamRetrySettings{Enabled: true, TTFTTimeoutSeconds: 9999, ChunkGapWarnSeconds: 9999, ChunkGapTimeoutSeconds: 9999, RetryMax: 99, RetryBackoffMs: 99999},
			want: StreamRetrySettings{Enabled: true, TTFTTimeoutSeconds: 300, ChunkGapWarnSeconds: 0, ChunkGapTimeoutSeconds: 300, RetryMax: 5, RetryBackoffMs: 10000},
			// note: warn 9999 -> clamped 120, but 120 < 300 so it stays... adjusted below
		},
		{
			name: "zero disables individual field",
			in:   StreamRetrySettings{Enabled: true, TTFTTimeoutSeconds: 0, ChunkGapWarnSeconds: 10, ChunkGapTimeoutSeconds: 30, RetryMax: 2, RetryBackoffMs: 1000},
			want: StreamRetrySettings{Enabled: true, TTFTTimeoutSeconds: 0, ChunkGapWarnSeconds: 10, ChunkGapTimeoutSeconds: 30, RetryMax: 2, RetryBackoffMs: 1000},
		},
		{
			name: "warn >= timeout disables warn",
			in:   StreamRetrySettings{Enabled: true, ChunkGapWarnSeconds: 40, ChunkGapTimeoutSeconds: 30, RetryMax: 1},
			want: StreamRetrySettings{Enabled: true, ChunkGapWarnSeconds: 0, ChunkGapTimeoutSeconds: 30, RetryMax: 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.sanitize()
			// The "clamp above max" case has a subtle warn interaction; recompute expectation.
			if tc.name == "clamp above max" {
				// warn 9999 -> 120, timeout 9999 -> 300, 120 < 300 so warn kept at 120
				tc.want.ChunkGapWarnSeconds = 120
			}
			// Field-by-field compare (struct has a map field, not directly comparable).
			if got.Enabled != tc.want.Enabled ||
				got.TTFTTimeoutSeconds != tc.want.TTFTTimeoutSeconds ||
				got.ChunkGapWarnSeconds != tc.want.ChunkGapWarnSeconds ||
				got.ChunkGapTimeoutSeconds != tc.want.ChunkGapTimeoutSeconds ||
				got.RetryMax != tc.want.RetryMax ||
				got.RetryBackoffMs != tc.want.RetryBackoffMs {
				t.Fatalf("sanitize() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestStreamRetrySettings_EffectiveWatchdogConfig_Disabled(t *testing.T) {
	s := StreamRetrySettings{Enabled: false, TTFTTimeoutSeconds: 60, ChunkGapTimeoutSeconds: 30}
	cfg := s.EffectiveWatchdogConfig("openai")
	if cfg.Enabled() {
		t.Fatal("disabled settings must yield a disabled watchdog config")
	}
}

func TestStreamRetrySettings_EffectiveWatchdogConfig_Global(t *testing.T) {
	s := DefaultStreamRetrySettings()
	cfg := s.EffectiveWatchdogConfig("anthropic")
	if cfg.TTFT != 60*time.Second || cfg.GapWarn != 10*time.Second || cfg.GapTimeout != 30*time.Second {
		t.Fatalf("unexpected effective config: %+v", cfg)
	}
}

func TestStreamRetrySettings_EffectiveWatchdogConfig_PlatformOverride(t *testing.T) {
	s := DefaultStreamRetrySettings()
	s.PlatformOverrides = map[string]StreamRetryPlatformOverride{
		// Antigravity reasoning: give it a longer gap timeout.
		"antigravity": {ChunkGapTimeoutSeconds: srIntPtr(120)},
	}
	// Non-overridden platform keeps global.
	openai := s.EffectiveWatchdogConfig("openai")
	if openai.GapTimeout != 30*time.Second {
		t.Fatalf("openai gap timeout = %v, want 30s", openai.GapTimeout)
	}
	// Overridden platform uses its own value.
	ag := s.EffectiveWatchdogConfig("antigravity")
	if ag.GapTimeout != 120*time.Second {
		t.Fatalf("antigravity gap timeout = %v, want 120s", ag.GapTimeout)
	}
	// Non-overridden fields inherit global.
	if ag.TTFT != 60*time.Second {
		t.Fatalf("antigravity ttft = %v, want inherited 60s", ag.TTFT)
	}
}

func TestStreamRetrySettings_EffectiveWatchdogConfig_OverrideDisableField(t *testing.T) {
	s := DefaultStreamRetrySettings()
	s.PlatformOverrides = map[string]StreamRetryPlatformOverride{
		// Explicit 0 disables TTFT for this platform only.
		"bedrock": {TTFTTimeoutSeconds: srIntPtr(0)},
	}
	cfg := s.EffectiveWatchdogConfig("bedrock")
	if cfg.TTFT != 0 {
		t.Fatalf("bedrock ttft should be disabled (0), got %v", cfg.TTFT)
	}
	if cfg.GapTimeout != 30*time.Second {
		t.Fatalf("bedrock gap timeout should inherit global 30s, got %v", cfg.GapTimeout)
	}
}

func TestStreamRetrySettings_RetryBackoff(t *testing.T) {
	s := StreamRetrySettings{RetryBackoffMs: 1500}
	if s.RetryBackoff() != 1500*time.Millisecond {
		t.Fatalf("retry backoff = %v, want 1.5s", s.RetryBackoff())
	}
}
