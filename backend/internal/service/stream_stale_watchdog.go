package service

import (
	"context"
	"errors"
	"strings"
	"time"
)

// stream_stale_watchdog.go
//
// Shared, provider-agnostic watchdog for detecting *stale* SSE streams and
// deciding whether the gateway may safely fail over to another upstream account.
//
// Design goals (see AGENTS.md / stream-retry feature):
//   - ONE implementation, wired into every provider streaming loop (Anthropic,
//     OpenAI chat/messages, Antigravity, Bedrock, ...) instead of copy-pasted timers.
//   - Detect three distinct stall shapes:
//       * TTFT     — upstream connects but never sends the first byte.
//       * gap-warn — inter-event gap crosses a soft threshold (observability only).
//       * gap-fail — inter-event gap crosses a hard threshold (trigger failover).
//   - Reset the gap timers on *any* upstream event (keepalive ping, reasoning
//     delta, content token, ...), so legitimately "thinking" reasoning models are
//     not mis-classified as stale. Only true upstream silence (live TCP, zero
//     bytes) trips the watchdog.
//   - Be deterministically testable: the clock and timer factory are injectable,
//     so unit tests drive it with a fake clock and never sleep.
//   - Never retry once semantic output has been committed to the client. That
//     safety gate lives in the caller (it owns clientOutputStarted); the watchdog
//     only reports *why* it tripped and lets the caller decide.

// StreamStallReason enumerates why the watchdog fired.
type StreamStallReason int

const (
	// StreamStallNone is the zero value (no stall).
	StreamStallNone StreamStallReason = iota
	// StreamStallTTFT: connected but no first upstream event within the TTFT budget.
	StreamStallTTFT
	// StreamStallGapWarn: inter-event gap crossed the soft warning threshold.
	StreamStallGapWarn
	// StreamStallGapTimeout: inter-event gap crossed the hard failover threshold.
	StreamStallGapTimeout
)

func (r StreamStallReason) String() string {
	switch r {
	case StreamStallTTFT:
		return "ttft_timeout"
	case StreamStallGapWarn:
		return "chunk_gap_warn"
	case StreamStallGapTimeout:
		return "chunk_gap_timeout"
	default:
		return "none"
	}
}

// IsFailover reports whether this reason is one that (subject to the caller's
// output gate) should trigger a failover, as opposed to a warn-only signal.
func (r StreamStallReason) IsFailover() bool {
	return r == StreamStallTTFT || r == StreamStallGapTimeout
}

// StreamWatchdogConfig is the resolved, ready-to-use timing configuration for a
// single streaming request. All durations of zero disable that particular timer,
// mirroring the existing gateway convention (0 = disabled).
type StreamWatchdogConfig struct {
	// TTFT is the maximum time to wait for the first upstream event.
	TTFT time.Duration
	// GapWarn is the soft inter-event gap threshold (log only).
	GapWarn time.Duration
	// GapTimeout is the hard inter-event gap threshold (failover candidate).
	GapTimeout time.Duration
}

// Enabled reports whether any timer is active.
func (c StreamWatchdogConfig) Enabled() bool {
	return c.TTFT > 0 || c.GapWarn > 0 || c.GapTimeout > 0
}

// normalize enforces the invariant GapWarn < GapTimeout when both are set, and
// clamps negatives to zero (disabled). It returns a copy; the receiver is unchanged.
func (c StreamWatchdogConfig) normalize() StreamWatchdogConfig {
	out := c
	if out.TTFT < 0 {
		out.TTFT = 0
	}
	if out.GapWarn < 0 {
		out.GapWarn = 0
	}
	if out.GapTimeout < 0 {
		out.GapTimeout = 0
	}
	// A warn threshold at or above the hard timeout is meaningless: it would fire
	// simultaneously with (or after) the failover. Disable it in that case.
	if out.GapWarn > 0 && out.GapTimeout > 0 && out.GapWarn >= out.GapTimeout {
		out.GapWarn = 0
	}
	return out
}

// stoppableTimer is the minimal timer surface the watchdog needs. *time.Timer
// satisfies it; tests provide a fake implementation driven by a manual clock.
type stoppableTimer interface {
	// Chan returns the timer's fire channel.
	Chan() <-chan time.Time
	// Reset reschedules the timer to fire after d, draining any pending tick.
	Reset(d time.Duration)
	// Stop halts the timer, draining any pending tick.
	Stop()
}

// realTimer adapts *time.Timer to stoppableTimer.
type realTimer struct{ t *time.Timer }

func (rt *realTimer) Chan() <-chan time.Time { return rt.t.C }

func (rt *realTimer) Reset(d time.Duration) {
	if !rt.t.Stop() {
		select {
		case <-rt.t.C:
		default:
		}
	}
	rt.t.Reset(d)
}

func (rt *realTimer) Stop() {
	if !rt.t.Stop() {
		select {
		case <-rt.t.C:
		default:
		}
	}
}

// newRealTimer creates a real timer that fires after d. If d <= 0 the timer is
// created stopped (its channel never fires) so callers can treat it uniformly.
func newRealTimer(d time.Duration) stoppableTimer {
	t := time.NewTimer(time.Hour)
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	if d > 0 {
		t.Reset(d)
	}
	return &realTimer{t: t}
}

// StreamWatchdog tracks TTFT and inter-event gap for a single streaming request.
//
// Lifecycle:
//
//	w := NewStreamWatchdog(cfg)
//	defer w.Stop()
//	ttftCh, warnCh, gapCh := w.Chans()
//	for {
//	    select {
//	    case ev := <-events:
//	        w.OnUpstreamEvent()   // any upstream byte keeps the stream alive
//	        ...
//	    case <-ttftCh:
//	        // handle w.Reason() == StreamStallTTFT
//	    case <-warnCh:
//	        // observability only
//	    case <-gapCh:
//	        // handle w.Reason() == StreamStallGapTimeout
//	    }
//	}
//
// A StreamWatchdog is NOT safe for concurrent use; it is owned by the single
// goroutine running the streaming select loop.
type StreamWatchdog struct {
	cfg StreamWatchdogConfig

	ttft    stoppableTimer
	gapWarn stoppableTimer
	gapFail stoppableTimer

	firstEventSeen bool
	lastReason     StreamStallReason

	newTimer func(d time.Duration) stoppableTimer
}

// NewStreamWatchdog builds a watchdog with real timers.
func NewStreamWatchdog(cfg StreamWatchdogConfig) *StreamWatchdog {
	return newStreamWatchdogWithTimers(cfg, newRealTimer)
}

// newStreamWatchdogWithTimers is the testable constructor: it accepts a timer
// factory so unit tests can inject a fake, manually-advanced clock.
func newStreamWatchdogWithTimers(cfg StreamWatchdogConfig, newTimer func(d time.Duration) stoppableTimer) *StreamWatchdog {
	cfg = cfg.normalize()
	w := &StreamWatchdog{
		cfg:      cfg,
		newTimer: newTimer,
	}
	// TTFT starts immediately; the gap timers only start after the first event.
	w.ttft = newTimer(cfg.TTFT)
	w.gapWarn = newTimer(0)
	w.gapFail = newTimer(0)
	return w
}

// Chans exposes the three fire channels for use in a select statement. Any of
// them may be nil-equivalent (never fires) when its threshold is disabled.
func (w *StreamWatchdog) Chans() (ttft, warn, gap <-chan time.Time) {
	if w == nil {
		return nil, nil, nil
	}
	return w.ttft.Chan(), w.gapWarn.Chan(), w.gapFail.Chan()
}

// OnUpstreamEvent must be called for EVERY upstream event (content, reasoning
// delta, keepalive/ping, comment line, ...). It stops the TTFT timer on the
// first event and (re)arms the gap timers, proving the stream is still alive.
func (w *StreamWatchdog) OnUpstreamEvent() {
	if w == nil {
		return
	}
	if !w.firstEventSeen {
		w.firstEventSeen = true
		w.ttft.Stop()
	}
	if w.cfg.GapWarn > 0 {
		w.gapWarn.Reset(w.cfg.GapWarn)
	}
	if w.cfg.GapTimeout > 0 {
		w.gapFail.Reset(w.cfg.GapTimeout)
	}
}

// Reason returns the last stall reason recorded by a Tripped* call. Useful for
// logging/metrics after a timer channel fires.
func (w *StreamWatchdog) Reason() StreamStallReason {
	if w == nil {
		return StreamStallNone
	}
	return w.lastReason
}

// TrippedTTFT records and returns the TTFT stall reason. Call after ttftCh fires.
func (w *StreamWatchdog) TrippedTTFT() StreamStallReason {
	if w == nil {
		return StreamStallNone
	}
	w.lastReason = StreamStallTTFT
	return w.lastReason
}

// TrippedGapWarn records the warn reason. Call after warnCh fires. The warn timer
// is one-shot; it re-arms on the next OnUpstreamEvent.
func (w *StreamWatchdog) TrippedGapWarn() StreamStallReason {
	if w == nil {
		return StreamStallNone
	}
	w.lastReason = StreamStallGapWarn
	return w.lastReason
}

// TrippedGapTimeout records and returns the hard gap stall reason. Call after
// gapCh fires.
func (w *StreamWatchdog) TrippedGapTimeout() StreamStallReason {
	if w == nil {
		return StreamStallNone
	}
	w.lastReason = StreamStallGapTimeout
	return w.lastReason
}

// Stop halts all timers. Safe to call multiple times and on a nil receiver.
func (w *StreamWatchdog) Stop() {
	if w == nil {
		return
	}
	w.ttft.Stop()
	w.gapWarn.Stop()
	w.gapFail.Stop()
}

// isRetryableStreamNetworkError classifies a transport/scan error encountered
// mid-stream as retryable (safe to fail over) or not. Client cancellation and
// deliberate limits are never retryable; transient network faults are.
//
// This mirrors the classification the old fork shipped, hardened against the
// upstream error strings observed in production (http2 GOAWAY, i/o timeout, ...).
func isRetryableStreamNetworkError(err error) bool {
	if err == nil {
		return false
	}
	// Explicit client-side cancellation / deadline: never retry.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := err.Error()
	for _, s := range nonRetryableStreamErrorSubstrings {
		if strings.Contains(msg, s) {
			return false
		}
	}
	for _, s := range retryableStreamErrorSubstrings {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

var nonRetryableStreamErrorSubstrings = []string{
	"context canceled",
	"context deadline exceeded",
	"request body too large",
	"client disconnected",
	"http: request body too large",
}

var retryableStreamErrorSubstrings = []string{
	"timeout awaiting response headers",
	"connection refused",
	"connection reset by peer",
	"connection timed out",
	"dial tcp",
	"i/o timeout",
	"unexpected EOF",
	"EOF",
	"broken pipe",
	"no such host",
	"network is unreachable",
	"server sent GOAWAY",
	"received Server's graceful shutdown",
	"use of closed network connection",
}
