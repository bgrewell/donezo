package api

import (
	"context"
	"time"
)

// The retention sweep (#16). A trash that never empties is an archive with a
// worse name, so anything deleted longer ago than the window is purged for
// real. The window is generous by default and configurable, because the cost
// of purging too early is losing something and the cost of purging too late
// is disk.

// DefaultTrashRetention is how long a deleted item stays restorable.
const DefaultTrashRetention = 30 * 24 * time.Hour

// sweepInterval is how often the sweep runs while the server is up. Daily is
// plenty: the window is measured in weeks, so the only thing a tighter
// interval buys is purging a few hours earlier.
const sweepInterval = 24 * time.Hour

// RunTrashSweep purges expired trash now and then daily, returning when ctx
// is done. It blocks, so the caller owns the goroutine and can wait for it to
// finish before closing the stores — the same discipline auth.Sweeper needs,
// and for the same reason: an in-flight sweep must not race a store Close.
//
// It sweeps once at startup deliberately. An instance stopped and started
// every day would otherwise never reach the first tick, and expired items
// would sit restorable forever — the same bug as having no sweep at all, only
// harder to notice.
//
// Retention of zero or less disables it, which is the escape hatch for anyone
// who would rather nothing ever disappeared on a timer.
func (s *Server) RunTrashSweep(ctx context.Context) {
	if s.trashRetention <= 0 {
		s.logger.Printf("trash retention disabled; nothing is purged automatically")
		return
	}
	s.sweepTrash(ctx)
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepTrash(ctx)
		}
	}
}

// sweepTrash purges expired trash from every live space, one pass.
//
// A failure on one space is logged and the sweep continues: one unreadable
// space database should not stop every other space from being tidied, and
// there is nothing a caller could do with the error anyway.
func (s *Server) sweepTrash(ctx context.Context) {
	cutoff := s.clock().UTC().Add(-s.trashRetention).Format(time.RFC3339)
	spaces, err := s.core.ListSpaces(ctx)
	if err != nil {
		s.logger.Printf("trash sweep: list spaces: %v", err)
		return
	}
	var total int64
	for _, sp := range spaces {
		// Archived spaces are skipped: they are deliberately frozen, and
		// quietly destroying content inside one would be a surprise.
		if sp.ArchivedAt != nil {
			continue
		}
		n, err := s.spaces.PurgeExpired(ctx, sp.ID, cutoff)
		if err != nil {
			s.logger.Printf("trash sweep: space %s: %v", sp.ID, err)
			continue
		}
		total += n
	}
	if total > 0 {
		s.logger.Printf("trash sweep: purged %d item(s) deleted before %s", total, cutoff)
	}
}
