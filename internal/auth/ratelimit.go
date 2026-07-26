package auth

import (
	"sync"
	"time"
)

// Default rate limiter policy for credential endpoints: 10 attempts per
// rolling 5 minutes per key.
const (
	defaultLimit  = 10
	defaultWindow = 5 * time.Minute
)

// RateLimiter is a sliding-window attempt limiter keyed by an opaque
// string (donezod keys by client IP). State is in memory only and
// resets on restart, which is acceptable for its job of slowing online
// password guessing. Call Sweep periodically (the Sweeper does) to drop
// idle keys.
type RateLimiter struct {
	limit  int
	window time.Duration
	clock  func() time.Time

	mu       sync.Mutex
	attempts map[string][]time.Time
}

// RateLimiterOption configures a RateLimiter (functional options
// pattern).
type RateLimiterOption func(*RateLimiter)

// WithLimit sets how many attempts each key may make per window.
// Non-positive values are ignored, keeping the default of 10.
func WithLimit(n int) RateLimiterOption {
	return func(l *RateLimiter) {
		if n > 0 {
			l.limit = n
		}
	}
}

// WithWindow sets the rolling window length. Non-positive values are
// ignored, keeping the default of 5 minutes.
func WithWindow(d time.Duration) RateLimiterOption {
	return func(l *RateLimiter) {
		if d > 0 {
			l.window = d
		}
	}
}

// WithLimiterClock overrides the time source. Defaults to time.Now;
// deterministic tests inject a fake clock.
func WithLimiterClock(clock func() time.Time) RateLimiterOption {
	return func(l *RateLimiter) {
		if clock != nil {
			l.clock = clock
		}
	}
}

// NewRateLimiter builds a limiter allowing 10 attempts per 5 minutes
// per key, adjusted by opts.
func NewRateLimiter(opts ...RateLimiterOption) *RateLimiter {
	l := &RateLimiter{
		limit:    defaultLimit,
		window:   defaultWindow,
		clock:    time.Now,
		attempts: make(map[string][]time.Time),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Allow records an attempt for key if it is under the limit and reports
// whether it may proceed. When denied it also returns how long the
// caller should wait before retrying; denied attempts are not recorded,
// so being blocked never extends the block.
func (l *RateLimiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	now := l.clock()
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := pruneBefore(l.attempts[key], now.Add(-l.window))
	if len(kept) >= l.limit {
		l.attempts[key] = kept
		return false, kept[0].Add(l.window).Sub(now)
	}
	l.attempts[key] = append(kept, now)
	return true, 0
}

// Sweep drops keys whose recorded attempts have all aged out of the
// window, bounding memory between sweeps.
func (l *RateLimiter) Sweep() {
	cutoff := l.clock().Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, times := range l.attempts {
		kept := pruneBefore(times, cutoff)
		if len(kept) == 0 {
			delete(l.attempts, key)
			continue
		}
		l.attempts[key] = kept
	}
}

// pruneBefore returns the suffix of times newer than cutoff. Attempts
// are appended in clock order, so the expired prefix is contiguous.
func pruneBefore(times []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(times) && !times[i].After(cutoff) {
		i++
	}
	return times[i:]
}
