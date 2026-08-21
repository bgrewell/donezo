package api

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bgrewell/donezo/internal/store"
)

// clarifyTTL is how long a "which project?" question stays open for its answer.
const clarifyTTL = 15 * time.Minute

// pendingClarify is a decoded action held back on one question — which project
// it belongs to — waiting for the sender's next reply to resolve.
type pendingClarify struct {
	userID    int64
	action    decodedSMS
	options   []projectRef
	createdAt time.Time
}

// clarifyStore holds, per sending number, the one open clarification. It is
// deliberately in-memory and short-lived: a restart drops pending questions,
// which only means the sender re-texts — the right trade for a 15-minute
// conversational nudge, versus persisting a table of half-formed intents.
type clarifyStore struct {
	mu      sync.Mutex
	pending map[string]pendingClarify
	ttl     time.Duration
}

func newClarifyStore(ttl time.Duration) *clarifyStore {
	return &clarifyStore{pending: make(map[string]pendingClarify), ttl: ttl}
}

// put stores a pending clarification, opportunistically sweeping expired ones so
// abandoned questions cannot accumulate.
func (c *clarifyStore) put(key string, p pendingClarify, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.pending {
		if now.Sub(v.createdAt) > c.ttl {
			delete(c.pending, k)
		}
	}
	c.pending[key] = p
}

// take removes and returns the pending clarification for key, or ok=false when
// there is none or it has expired.
func (c *clarifyStore) take(key string, now time.Time) (pendingClarify, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pending[key]
	if !ok {
		return pendingClarify{}, false
	}
	delete(c.pending, key)
	if now.Sub(p.createdAt) > c.ttl {
		return pendingClarify{}, false
	}
	return p, true
}

// resolveClarify treats an inbound message as the answer to a pending "which
// project?" question, if one is open for this number. It returns a confirmation
// and true when it completed the held action; false means "no open question, or
// the reply was not a usable answer — decode it as a fresh message instead".
func (s *Server) resolveClarify(ctx context.Context, user store.User, from, body string) (string, bool) {
	pending, ok := s.clarify.take(from, s.clock())
	if !ok {
		return "", false
	}
	// A number reassigned between the question and the answer must not resolve
	// into the previous owner's held action.
	if pending.userID != user.ID {
		return "", false
	}
	answer := strings.TrimSpace(body)
	var proj *projectRef
	if !strings.EqualFold(answer, "none") && !strings.EqualFold(answer, "no project") {
		proj = matchRef(answer, pending.options)
		if proj == nil {
			return "", false // not a usable answer — fall through to a fresh decode
		}
	}
	loc := s.userLocation(ctx, user.ID)
	return s.completeAction(ctx, user, loc, proj, pending.action)
}

// matchRef resolves a texted answer to a project by name (case-insensitive) or
// by 1-based position in the offered list ("2").
func matchRef(answer string, options []projectRef) *projectRef {
	answer = strings.TrimSpace(answer)
	for i := range options {
		if strings.EqualFold(options[i].name, answer) {
			return &options[i]
		}
	}
	if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(options) {
		return &options[n-1]
	}
	return nil
}

// clarifyQuestion asks which project a decoded action belongs to.
func clarifyQuestion(title string, options []projectRef) string {
	names := make([]string, 0, len(options))
	for _, o := range options {
		names = append(names, o.name)
	}
	return "Which project for \"" + title + "\"? Reply with one — " + strings.Join(names, ", ") + " — or \"none\"."
}
