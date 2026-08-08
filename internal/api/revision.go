package api

import "sync"

// This file tracks "has anything in this space changed?" so a client can ask
// cheaply and often, and refetch only when the answer moves.
//
// The marker is an in-memory counter rather than anything derived from the
// database. Two things ruled the derived options out: the space databases run
// with SetMaxOpenConns(1) (see internal/store.openDB), so a connection
// dedicated to reading a change marker would hold the only connection and
// starve every write — and on a single shared connection SQLite's
// PRAGMA data_version never reflects that connection's own commits anyway.
// Meanwhile only projects and activities carry updated_at, so there is no
// timestamp to take a maximum over.
//
// Being in memory has one visible consequence: a donezod restart resets every
// counter, so each connected client sees one changed revision and refetches
// once. That is the harmless direction to fail in — a spurious refetch costs a
// request, whereas a missed one leaves the screen quietly wrong.

// revisions counts committed changes per space.
//
// Safe for concurrent use: every mutating HTTP request and every write over
// MCP bumps it, and any number of pollers read it.
type revisions struct {
	mu sync.RWMutex
	n  map[string]uint64
}

func newRevisions() *revisions {
	return &revisions{n: make(map[string]uint64)}
}

// Bump records that a space changed. A space is not tracked until something
// changes in it, so an untouched space reads as revision 0 without needing an
// entry.
func (r *revisions) Bump(spaceID string) {
	if spaceID == "" {
		return
	}
	r.mu.Lock()
	r.n[spaceID]++
	r.mu.Unlock()
}

// Current returns a space's revision.
func (r *revisions) Current(spaceID string) uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.n[spaceID]
}
