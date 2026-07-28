package auth

import (
	"sync"
	"testing"
	"time"
)

// limiterClock is a manually advanced clock for deterministic limiter
// tests.
type limiterClock struct {
	mu sync.Mutex
	t  time.Time
}

func newLimiterClock() *limiterClock {
	return &limiterClock{t: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)}
}

func (c *limiterClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *limiterClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestRateLimiterAllow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  func(t *testing.T, l *RateLimiter, clock *limiterClock)
	}{
		{
			name: "allows the limit, blocks the next",
			run: func(t *testing.T, l *RateLimiter, _ *limiterClock) {
				t.Helper()
				for i := 0; i < 3; i++ {
					if ok, _ := l.Allow("ip"); !ok {
						t.Fatalf("attempt %d denied, want allowed", i+1)
					}
				}
				ok, retry := l.Allow("ip")
				if ok {
					t.Fatal("attempt 4 allowed, want denied")
				}
				if retry != time.Minute {
					t.Errorf("retryAfter = %s, want %s (full window, no time passed)", retry, time.Minute)
				}
			},
		},
		{
			name: "window slides: oldest attempt ages out",
			run: func(t *testing.T, l *RateLimiter, clock *limiterClock) {
				t.Helper()
				l.Allow("ip")
				clock.Advance(30 * time.Second)
				l.Allow("ip")
				l.Allow("ip")
				if ok, retry := l.Allow("ip"); ok {
					t.Fatal("over-limit attempt allowed")
				} else if retry != 30*time.Second {
					t.Errorf("retryAfter = %s, want 30s until the first attempt ages out", retry)
				}
				clock.Advance(31 * time.Second) // first attempt now outside the window
				if ok, _ := l.Allow("ip"); !ok {
					t.Error("attempt after window slide denied, want allowed")
				}
			},
		},
		{
			name: "boundary: attempt exactly window old no longer counts",
			run: func(t *testing.T, l *RateLimiter, clock *limiterClock) {
				t.Helper()
				l.Allow("ip")
				l.Allow("ip")
				l.Allow("ip")
				clock.Advance(time.Minute)
				if ok, _ := l.Allow("ip"); !ok {
					t.Error("attempt at exact window edge denied, want allowed")
				}
			},
		},
		{
			name: "denied attempts do not extend the block",
			run: func(t *testing.T, l *RateLimiter, clock *limiterClock) {
				t.Helper()
				l.Allow("ip")
				l.Allow("ip")
				l.Allow("ip")
				for i := 0; i < 5; i++ {
					clock.Advance(10 * time.Second)
					if ok, _ := l.Allow("ip"); ok {
						t.Fatalf("attempt during block allowed at +%ds", (i+1)*10)
					}
				}
				clock.Advance(11 * time.Second) // 61s after the first attempt
				if ok, _ := l.Allow("ip"); !ok {
					t.Error("attempt after original window denied; denied attempts must not extend the block")
				}
			},
		},
		{
			name: "keys are isolated",
			run: func(t *testing.T, l *RateLimiter, _ *limiterClock) {
				t.Helper()
				for i := 0; i < 3; i++ {
					l.Allow("10.0.0.1")
				}
				if ok, _ := l.Allow("10.0.0.1"); ok {
					t.Fatal("blocked key allowed")
				}
				if ok, _ := l.Allow("10.0.0.2"); !ok {
					t.Error("fresh key denied; limiter must isolate per key")
				}
			},
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clock := newLimiterClock()
			l := NewRateLimiter(WithLimit(3), WithWindow(time.Minute), WithLimiterClock(clock.Now))
			tt.run(t, l, clock)
		})
	}
}

func TestRateLimiterSweep(t *testing.T) {
	t.Parallel()
	clock := newLimiterClock()
	l := NewRateLimiter(WithLimit(3), WithWindow(time.Minute), WithLimiterClock(clock.Now))
	l.Allow("stale")
	clock.Advance(30 * time.Second)
	l.Allow("fresh")
	clock.Advance(45 * time.Second) // "stale" is now 75s old, "fresh" 45s

	l.Sweep()

	l.mu.Lock()
	_, staleKept := l.attempts["stale"]
	fresh, freshKept := l.attempts["fresh"]
	l.mu.Unlock()
	if staleKept {
		t.Error("fully aged-out key survived Sweep")
	}
	if !freshKept || len(fresh) != 1 {
		t.Errorf("in-window key pruned by Sweep (kept=%v, len=%d)", freshKept, len(fresh))
	}
}

func TestRateLimiterOptionValidation(t *testing.T) {
	t.Parallel()
	// Non-positive option values are documented as ignored.
	l := NewRateLimiter(WithLimit(0), WithWindow(-time.Second), WithLimiterClock(nil))
	if l.limit != defaultLimit {
		t.Errorf("limit = %d, want default %d", l.limit, defaultLimit)
	}
	if l.window != defaultWindow {
		t.Errorf("window = %s, want default %s", l.window, defaultWindow)
	}
	if l.clock == nil {
		t.Error("clock = nil, want default retained")
	}
}
