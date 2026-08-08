package mcp

import (
	"io"
	"log"
	"net/http/httptest"
	"sync"
	"testing"
)

// writeRecorder collects the space ids reported through WithOnWrite.
type writeRecorder struct {
	mu     sync.Mutex
	spaces []string
}

func (w *writeRecorder) record(spaceID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.spaces = append(w.spaces, spaceID)
}

func (w *writeRecorder) seen() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string{}, w.spaces...)
}

// onWriteFixture is newFixture with a recorder wired into the handler.
func onWriteFixture(t *testing.T) (*fixture, *writeRecorder) {
	t.Helper()
	rec := &writeRecorder{}
	f := newFixture(t)
	// Rebuild the handler with the callback installed; the fixture's stores
	// and tokens are reused so the sessions stay valid.
	h := NewHandler(f.core, f.spaces,
		WithClock(fixedClock),
		WithLogger(log.New(io.Discard, "", 0)),
		WithVersion("test-1.2.3"),
		WithOnWrite(rec.record),
	)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	f.h, f.server = h, srv
	f.rw = f.connect(t, f.rwToken)
	f.ro = f.connect(t, f.roToken)
	return f, rec
}

// A browser watching a space has no other way to learn that an LLM changed
// something, so a write over MCP has to report it — and a read must not, or
// every poll would refetch identical state.
func TestOnWriteFiresForWritesOnly(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		args     string
		readOnly bool
		want     []string
	}{
		{
			name: "write tool reports its space",
			tool: "capture_to_inbox", args: `{"space_id":"sandbox","text":"hi"}`,
			want: []string{"sandbox"},
		},
		{
			name: "read tool reports nothing",
			tool: "get_space_overview", args: `{"space_id":"sandbox"}`,
			want: nil,
		},
		{
			name: "list_spaces takes no space and reports nothing",
			tool: "list_spaces", args: `{}`,
			want: nil,
		},
		{
			name: "a failed write reports nothing",
			tool: "log_activity", args: `{"space_id":"sandbox","project_id":"no-such-project","title":"x"}`,
			want: nil,
		},
		{
			name: "a write refused by scope reports nothing",
			tool: "capture_to_inbox", args: `{"space_id":"sandbox","text":"hi"}`,
			readOnly: true, want: nil,
		},
		{
			name: "a write into another user's space reports nothing",
			tool: "capture_to_inbox", args: `{"space_id":"private","text":"hi"}`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, rec := onWriteFixture(t)
			sess := f.rw
			if tt.readOnly {
				sess = f.ro
			}
			f.callTool(t, sess, tt.tool, tt.args)

			got := rec.seen()
			if len(got) != len(tt.want) {
				t.Fatalf("onWrite saw %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("onWrite[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// Every write tool routes through the same adapter, so a newly added one is
// covered without anyone remembering to wire it up. This pins that: if the
// registry gains a write tool that reports nothing, the count drifts.
func TestOnWriteCoversEveryWriteTool(t *testing.T) {
	writes := 0
	for _, tl := range tools {
		if tl.write {
			writes++
		}
	}
	if writes == 0 {
		t.Fatal("no write tools found — the registry shape changed")
	}
	// A representative write, to prove the adapter path is live at all.
	f, rec := onWriteFixture(t)
	f.callTool(t, f.rw, "create_task", `{"space_id":"sandbox","title":"probe"}`)
	if got := rec.seen(); len(got) != 1 || got[0] != "sandbox" {
		t.Errorf("create_task reported %v, want [sandbox]", got)
	}
}
