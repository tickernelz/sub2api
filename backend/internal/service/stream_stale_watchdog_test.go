package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeTimer is a manually-fired stoppableTimer for deterministic tests.
type fakeTimer struct {
	ch      chan time.Time
	armedAt time.Duration // last Reset duration; 0 = stopped
	stopped bool
	fired   int
}

func newFakeTimer(d time.Duration) *fakeTimer {
	ft := &fakeTimer{ch: make(chan time.Time, 1)}
	if d > 0 {
		ft.armedAt = d
		ft.stopped = false
	} else {
		ft.stopped = true
	}
	return ft
}

func (ft *fakeTimer) Chan() <-chan time.Time { return ft.ch }

func (ft *fakeTimer) Reset(d time.Duration) {
	ft.drain()
	ft.armedAt = d
	ft.stopped = d <= 0
}

func (ft *fakeTimer) Stop() {
	ft.drain()
	ft.stopped = true
	ft.armedAt = 0
}

func (ft *fakeTimer) drain() {
	select {
	case <-ft.ch:
	default:
	}
}

// fire simulates the timer elapsing (only if currently armed).
func (ft *fakeTimer) fire(t *testing.T) {
	t.Helper()
	if ft.stopped {
		t.Fatalf("attempted to fire a stopped timer")
	}
	ft.fired++
	ft.ch <- time.Now()
}

// newFakeFactory returns a timer factory plus a slice capturing every timer it
// created, in creation order: [ttft, gapWarn, gapFail].
func newFakeFactory() (func(time.Duration) stoppableTimer, *[]*fakeTimer) {
	created := &[]*fakeTimer{}
	factory := func(d time.Duration) stoppableTimer {
		ft := newFakeTimer(d)
		*created = append(*created, ft)
		return ft
	}
	return factory, created
}

func TestStreamWatchdog_Config_Normalize(t *testing.T) {
	cases := []struct {
		name string
		in   StreamWatchdogConfig
		want StreamWatchdogConfig
	}{
		{
			name: "warn above timeout is disabled",
			in:   StreamWatchdogConfig{TTFT: 60 * time.Second, GapWarn: 40 * time.Second, GapTimeout: 30 * time.Second},
			want: StreamWatchdogConfig{TTFT: 60 * time.Second, GapWarn: 0, GapTimeout: 30 * time.Second},
		},
		{
			name: "warn equal to timeout is disabled",
			in:   StreamWatchdogConfig{GapWarn: 30 * time.Second, GapTimeout: 30 * time.Second},
			want: StreamWatchdogConfig{GapWarn: 0, GapTimeout: 30 * time.Second},
		},
		{
			name: "negatives clamped to zero",
			in:   StreamWatchdogConfig{TTFT: -5, GapWarn: -1, GapTimeout: -1},
			want: StreamWatchdogConfig{},
		},
		{
			name: "valid ordering preserved",
			in:   StreamWatchdogConfig{TTFT: 60 * time.Second, GapWarn: 10 * time.Second, GapTimeout: 30 * time.Second},
			want: StreamWatchdogConfig{TTFT: 60 * time.Second, GapWarn: 10 * time.Second, GapTimeout: 30 * time.Second},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.normalize()
			if got != tc.want {
				t.Fatalf("normalize() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestStreamWatchdog_Config_Enabled(t *testing.T) {
	if (StreamWatchdogConfig{}).Enabled() {
		t.Fatal("empty config should be disabled")
	}
	if !(StreamWatchdogConfig{GapTimeout: time.Second}).Enabled() {
		t.Fatal("config with gap timeout should be enabled")
	}
}

func TestStreamWatchdog_TTFT_FiresWhenNoFirstEvent(t *testing.T) {
	factory, created := newFakeFactory()
	cfg := StreamWatchdogConfig{TTFT: 60 * time.Second, GapWarn: 10 * time.Second, GapTimeout: 30 * time.Second}
	w := newStreamWatchdogWithTimers(cfg, factory)
	defer w.Stop()

	timers := *created
	if len(timers) != 3 {
		t.Fatalf("expected 3 timers created, got %d", len(timers))
	}
	ttft := timers[0]
	// TTFT should be armed at construction; gap timers should start stopped.
	if ttft.stopped {
		t.Fatal("TTFT timer should be armed at construction")
	}
	if !timers[1].stopped || !timers[2].stopped {
		t.Fatal("gap timers should be stopped until first event")
	}

	ttft.fire(t)
	select {
	case <-w.ttft.Chan():
		if got := w.TrippedTTFT(); got != StreamStallTTFT {
			t.Fatalf("reason = %v, want ttft", got)
		}
	default:
		t.Fatal("expected TTFT channel to have a value")
	}
}

func TestStreamWatchdog_FirstEvent_StopsTTFT_ArmsGap(t *testing.T) {
	factory, created := newFakeFactory()
	cfg := StreamWatchdogConfig{TTFT: 60 * time.Second, GapWarn: 10 * time.Second, GapTimeout: 30 * time.Second}
	w := newStreamWatchdogWithTimers(cfg, factory)
	defer w.Stop()

	timers := *created
	ttft, gapWarn, gapFail := timers[0], timers[1], timers[2]

	w.OnUpstreamEvent()

	if !ttft.stopped {
		t.Fatal("TTFT timer should be stopped after first event")
	}
	if gapWarn.stopped || gapWarn.armedAt != cfg.GapWarn {
		t.Fatalf("gap warn should be armed at %v, got armed=%v stopped=%v", cfg.GapWarn, gapWarn.armedAt, gapWarn.stopped)
	}
	if gapFail.stopped || gapFail.armedAt != cfg.GapTimeout {
		t.Fatalf("gap fail should be armed at %v, got armed=%v stopped=%v", cfg.GapTimeout, gapFail.armedAt, gapFail.stopped)
	}
}

func TestStreamWatchdog_GapTimeout_FiresAfterSilence(t *testing.T) {
	factory, created := newFakeFactory()
	cfg := StreamWatchdogConfig{TTFT: 60 * time.Second, GapWarn: 10 * time.Second, GapTimeout: 30 * time.Second}
	w := newStreamWatchdogWithTimers(cfg, factory)
	defer w.Stop()

	gapFail := (*created)[2]
	w.OnUpstreamEvent() // arm gap timers
	gapFail.fire(t)

	select {
	case <-w.gapFail.Chan():
		if got := w.TrippedGapTimeout(); got != StreamStallGapTimeout {
			t.Fatalf("reason = %v, want gap timeout", got)
		}
		if !StreamStallGapTimeout.IsFailover() {
			t.Fatal("gap timeout should be a failover reason")
		}
	default:
		t.Fatal("expected gap-fail channel to have a value")
	}
}

// Reasoning-model safety: repeated upstream events (e.g. keepalive/reasoning
// deltas) keep re-arming the gap timer, so it never trips even across a long
// "thinking" period, as long as SOMETHING arrives within each gap window.
func TestStreamWatchdog_ReasoningKeepalive_DoesNotTrip(t *testing.T) {
	factory, created := newFakeFactory()
	cfg := StreamWatchdogConfig{TTFT: 60 * time.Second, GapWarn: 10 * time.Second, GapTimeout: 30 * time.Second}
	w := newStreamWatchdogWithTimers(cfg, factory)
	defer w.Stop()

	gapFail := (*created)[2]
	// Simulate 20 keepalive/reasoning events; each resets the gap timer.
	for i := 0; i < 20; i++ {
		w.OnUpstreamEvent()
		if gapFail.stopped {
			t.Fatalf("gap fail timer unexpectedly stopped at event %d", i)
		}
	}
	// The gap timer was reset on every event and never fired.
	if gapFail.fired != 0 {
		t.Fatalf("gap fail should not have fired during keepalive, fired=%d", gapFail.fired)
	}
}

func TestStreamWatchdog_GapWarn_ReArmsAndIsNotFailover(t *testing.T) {
	factory, created := newFakeFactory()
	cfg := StreamWatchdogConfig{TTFT: 60 * time.Second, GapWarn: 10 * time.Second, GapTimeout: 30 * time.Second}
	w := newStreamWatchdogWithTimers(cfg, factory)
	defer w.Stop()

	gapWarn := (*created)[1]
	w.OnUpstreamEvent()
	gapWarn.fire(t)
	<-w.gapWarn.Chan()
	if got := w.TrippedGapWarn(); got != StreamStallGapWarn {
		t.Fatalf("reason = %v, want gap warn", got)
	}
	if StreamStallGapWarn.IsFailover() {
		t.Fatal("gap warn must NOT be a failover reason")
	}
	// A subsequent event re-arms the warn timer.
	w.OnUpstreamEvent()
	if gapWarn.stopped {
		t.Fatal("gap warn should be re-armed after another event")
	}
}

func TestStreamWatchdog_Nil_SafeNoop(t *testing.T) {
	var w *StreamWatchdog
	w.OnUpstreamEvent()
	w.Stop()
	if w.Reason() != StreamStallNone {
		t.Fatal("nil watchdog Reason should be none")
	}
	ttft, warn, gap := w.Chans()
	if ttft != nil || warn != nil || gap != nil {
		t.Fatal("nil watchdog Chans should be nil")
	}
}

func TestStreamWatchdog_DisabledConfig_NeverFires(t *testing.T) {
	factory, created := newFakeFactory()
	w := newStreamWatchdogWithTimers(StreamWatchdogConfig{}, factory)
	defer w.Stop()
	for _, ft := range *created {
		if !ft.stopped {
			t.Fatal("all timers should be stopped for empty config")
		}
	}
	w.OnUpstreamEvent()
	for _, ft := range *created {
		if !ft.stopped {
			t.Fatal("timers should remain stopped for disabled config after event")
		}
	}
}

func TestIsRetryableStreamNetworkError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context canceled sentinel", context.Canceled, false},
		{"context deadline sentinel", context.DeadlineExceeded, false},
		{"client disconnected", errors.New("client disconnected mid stream"), false},
		{"body too large", errors.New("http: request body too large"), false},
		{"http2 goaway", errors.New("http2: server sent GOAWAY and closed the connection"), true},
		{"i/o timeout", errors.New("read tcp 1.2.3.4:443: i/o timeout"), true},
		{"connection reset", errors.New("read: connection reset by peer"), true},
		{"unexpected eof", errors.New("unexpected EOF"), true},
		{"dial tcp", errors.New("dial tcp 1.2.3.4:443: connect: connection refused"), true},
		{"unrelated", errors.New("something totally different"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableStreamNetworkError(tc.err); got != tc.want {
				t.Fatalf("isRetryableStreamNetworkError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
