package perch

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// DefaultMinRequestInterval is the floor between two request starts.
const DefaultMinRequestInterval = 300 * time.Millisecond

// DefaultMaxConcurrentCalls is the per-account cap on exchanges in flight.
const DefaultMaxConcurrentCalls = 2

// DefaultMaxAttempts is the per-call attempt budget.
const DefaultMaxAttempts = 3

// DefaultRetryDelays are the waits between attempts.
var DefaultRetryDelays = []time.Duration{time.Second, 3 * time.Second}

// accountGate serialises and spaces the requests one account sends.
type accountGate struct {
	starts  chan struct{}
	streams chan struct{}
	now     func() time.Time

	mu       sync.Mutex
	interval time.Duration
	last     time.Time
}

func newAccountGate(interval time.Duration, maxConcurrent int) *accountGate {
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrentCalls
	}
	return &accountGate{
		starts:   make(chan struct{}, 1),
		streams:  make(chan struct{}, maxConcurrent),
		now:      time.Now,
		interval: interval,
	}
}

func (g *accountGate) begin(ctx context.Context) (func(), error) {
	if g == nil {
		return func() {}, nil
	}
	select {
	case g.starts <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-g.starts }
	if g.interval <= 0 {
		g.mark()
		return release, nil
	}
	g.mu.Lock()
	last := g.last
	g.mu.Unlock()
	if !last.IsZero() {
		if delay := g.interval - g.now().Sub(last); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				release()
				return nil, ctx.Err()
			}
		}
	}
	g.mark()
	return release, nil
}

func (g *accountGate) acquire(ctx context.Context) (func(), error) {
	select {
	case g.streams <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-g.streams }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (g *accountGate) mark() {
	g.mu.Lock()
	g.last = g.now()
	g.mu.Unlock()
}

type gatedDoer struct {
	inner Doer
	gate  *accountGate
}

func newGatedDoer(inner Doer, gate *accountGate) Doer {
	if inner == nil || gate == nil {
		return inner
	}
	return &gatedDoer{inner: inner, gate: gate}
}

// unwrapDoer returns the doer a gate wraps.
func unwrapDoer(doer Doer) Doer {
	if gated, ok := doer.(*gatedDoer); ok {
		return gated.inner
	}
	return doer
}

func (d *gatedDoer) Do(req *http.Request) (*http.Response, error) {
	releaseStart, err := d.gate.begin(req.Context())
	if err != nil {
		return nil, err
	}
	releaseStart()
	releaseSlot, err := d.gate.acquire(req.Context())
	if err != nil {
		return nil, err
	}
	resp, err := d.inner.Do(req)
	if err != nil {
		releaseSlot()
		return nil, err
	}
	if resp.Body == nil {
		releaseSlot()
		return resp, nil
	}
	resp.Body = &gatedBody{ReadCloser: resp.Body, release: releaseSlot}
	return resp, nil
}

type gatedBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *gatedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.finish()
	}
	return n, err
}

func (b *gatedBody) Close() error {
	b.finish()
	return b.ReadCloser.Close()
}

func (b *gatedBody) finish() {
	b.once.Do(b.release)
}
