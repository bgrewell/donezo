package auth

import (
	"bytes"
	"context"
	"errors"
	"log"
	"sync"
	"testing"
	"time"
)

// fakePruner signals each DeleteExpiredSessions call on a channel.
type fakePruner struct {
	calls chan struct{}
	n     int64
	err   error
}

func (p *fakePruner) DeleteExpiredSessions(context.Context) (int64, error) {
	p.calls <- struct{}{}
	return p.n, p.err
}

// waitForCall fails the test if the pruner is not invoked within a
// generous deadline (the sweeper runs on a real ticker).
func waitForCall(t *testing.T, p *fakePruner, what string) {
	t.Helper()
	select {
	case <-p.calls:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestSweeperRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pruner  *fakePruner
		wantLog string
	}{
		{
			name:    "logs swept sessions",
			pruner:  &fakePruner{calls: make(chan struct{}, 8), n: 3},
			wantLog: "swept 3 expired session(s)",
		},
		{
			name:    "prune failure is logged, not fatal",
			pruner:  &fakePruner{calls: make(chan struct{}, 8), err: errors.New("disk on fire")},
			wantLog: "disk on fire",
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var buf bytes.Buffer
			logger := log.New(lockedWriter{mu: &mu, buf: &buf}, "", 0)

			clock := newLimiterClock()
			limiter := NewRateLimiter(WithWindow(time.Minute), WithLimiterClock(clock.Now))
			limiter.Allow("stale-key")
			clock.Advance(2 * time.Minute)

			s := NewSweeper(tt.pruner,
				WithSweepInterval(10*time.Millisecond),
				WithSweepLimiter(limiter),
				WithSweepLogger(logger),
			)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				s.Run(ctx)
				close(done)
			}()

			waitForCall(t, tt.pruner, "startup sweep")
			waitForCall(t, tt.pruner, "ticker sweep")
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Run did not return after context cancel")
			}

			limiter.mu.Lock()
			_, staleKept := limiter.attempts["stale-key"]
			limiter.mu.Unlock()
			if staleKept {
				t.Error("sweep did not prune the rate limiter")
			}
			mu.Lock()
			logged := buf.String()
			mu.Unlock()
			if !bytes.Contains([]byte(logged), []byte(tt.wantLog)) {
				t.Errorf("log = %q, want it to contain %q", logged, tt.wantLog)
			}
		})
	}
}

func TestSweeperOptionValidation(t *testing.T) {
	t.Parallel()
	s := NewSweeper(&fakePruner{calls: make(chan struct{}, 1)}, WithSweepInterval(0))
	if s.interval != defaultSweepInterval {
		t.Errorf("interval = %s, want default %s", s.interval, defaultSweepInterval)
	}
}

// lockedWriter serializes writes so the sweeper goroutine and the test
// can share one buffer.
type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}
