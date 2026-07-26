package auth

import (
	"context"
	"log"
	"time"
)

// defaultSweepInterval is how often the sweeper runs after the initial
// startup pass.
const defaultSweepInterval = time.Hour

// SessionPruner is the slice of the core store the sweeper needs.
type SessionPruner interface {
	// DeleteExpiredSessions removes sessions whose expiry has passed
	// and returns how many were removed.
	DeleteExpiredSessions(ctx context.Context) (int64, error)
}

// Sweeper periodically deletes expired sessions and prunes idle rate
// limiter entries: once at startup and then on every interval tick
// until its context is canceled.
type Sweeper struct {
	sessions SessionPruner
	limiter  *RateLimiter
	interval time.Duration
	logger   *log.Logger
}

// SweeperOption configures a Sweeper (functional options pattern).
type SweeperOption func(*Sweeper)

// WithSweepInterval sets the time between sweeps. Non-positive values
// are ignored, keeping the default of one hour.
func WithSweepInterval(d time.Duration) SweeperOption {
	return func(s *Sweeper) {
		if d > 0 {
			s.interval = d
		}
	}
}

// WithSweepLimiter also prunes the given rate limiter on every sweep.
func WithSweepLimiter(l *RateLimiter) SweeperOption {
	return func(s *Sweeper) { s.limiter = l }
}

// WithSweepLogger reports sweep results and failures to l. Without a
// logger the sweeper is silent.
func WithSweepLogger(l *log.Logger) SweeperOption {
	return func(s *Sweeper) { s.logger = l }
}

// NewSweeper builds a Sweeper over sessions, which must be non-nil.
func NewSweeper(sessions SessionPruner, opts ...SweeperOption) *Sweeper {
	s := &Sweeper{sessions: sessions, interval: defaultSweepInterval}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Run sweeps immediately, then once per interval, returning when ctx is
// canceled. It blocks; start it in its own goroutine.
func (s *Sweeper) Run(ctx context.Context) {
	s.sweep(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

// sweep performs one pass over sessions and, if configured, the rate
// limiter.
func (s *Sweeper) sweep(ctx context.Context) {
	n, err := s.sessions.DeleteExpiredSessions(ctx)
	switch {
	case err != nil:
		s.logf("sweep expired sessions: %v", err)
	case n > 0:
		s.logf("swept %d expired session(s)", n)
	}
	if s.limiter != nil {
		s.limiter.Sweep()
	}
}

// logf logs when a logger is configured.
func (s *Sweeper) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}
