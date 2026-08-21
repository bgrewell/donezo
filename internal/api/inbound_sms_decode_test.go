package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/store"
)

// stubLLM returns a fixed decode reply, so a test drives the create paths
// deterministically without a real model.
type stubLLM struct {
	reply string
	err   error
}

func (s stubLLM) Complete(context.Context, string, string) (string, error) {
	return s.reply, s.err
}
func (stubLLM) Provider() string { return "stub" }
func (stubLLM) Model() string    { return "stub" }

func TestInboundSMSDecode(t *testing.T) {
	t.Parallel()
	const number = "+15551234567"

	setup := func(t *testing.T, reply string) (http.Handler, *store.SpaceStore, context.Context) {
		t.Helper()
		srv := newTestServer(t,
			WithTwilioAuthToken(testTwilioToken),
			WithPublicURL("https://donezo.site"),
			WithLLM(stubLLM{reply: reply}),
		)
		ctx := context.Background()
		ben, err := srv.core.GetUserByUsername(ctx, "ben")
		if err != nil {
			t.Fatalf("get ben: %v", err)
		}
		verifySMSContact(t, srv.core, ben.ID, number)
		return srv.Handler(), srv.spaces, ctx
	}
	send := func(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
		t.Helper()
		sig := signInbound("https://donezo.site/sms", url.Values{"From": {number}, "Body": {body}})
		return postInbound(t, h, number, body, sig)
	}

	t.Run("reminder on a named project", func(t *testing.T) {
		h, spaces, ctx := setup(t, `{"action":"reminder","title":"look into XYZ","project":"Loom","remind_at":"2026-08-21T17:00"}`)
		rec := send(t, h, "remind me at 5 to look into XYZ for Loom")
		if !strings.Contains(rec.Body.String(), "Reminder set") || !strings.Contains(rec.Body.String(), "Loom") {
			t.Fatalf("reply = %s", rec.Body)
		}
		rems, err := spaces.ListReminders(ctx, "sandbox")
		if err != nil {
			t.Fatalf("list reminders: %v", err)
		}
		if len(rems) != 1 || rems[0].Text != "look into XYZ" || rems[0].RemindAt != "2026-08-21T17:00:00" {
			t.Fatalf("reminder = %+v", rems)
		}
		if rems[0].ProjectID == nil || *rems[0].ProjectID != "loom" {
			t.Errorf("reminder project = %v, want loom", rems[0].ProjectID)
		}
	})

	t.Run("task with a due date", func(t *testing.T) {
		h, spaces, ctx := setup(t, `{"action":"task","title":"do XYZ","project":"Loom","due":"2026-09-01"}`)
		rec := send(t, h, "create a task in Loom to do XYZ by sep 1")
		if !strings.Contains(rec.Body.String(), "Task added") {
			t.Fatalf("reply = %s", rec.Body)
		}
		tasks, err := spaces.ListTasks(ctx, "sandbox")
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		if len(tasks) != 1 || tasks[0].Title != "do XYZ" || tasks[0].Due == nil || *tasks[0].Due != "2026-09-01" {
			t.Errorf("task = %+v", tasks)
		}
	})

	t.Run("undecodable falls back to inbox", func(t *testing.T) {
		h, spaces, ctx := setup(t, `{"action":"none","title":"","project":""}`)
		rec := send(t, h, "hmm not sure what this is")
		if !strings.Contains(rec.Body.String(), "Saved to your donezo inbox") {
			t.Fatalf("reply = %s", rec.Body)
		}
		rems, _ := spaces.ListReminders(ctx, "sandbox")
		items, _ := spaces.ListInboxItems(ctx, "sandbox")
		if len(rems) != 0 || len(items) != 1 {
			t.Errorf("reminders=%d inbox=%d, want 0 and 1", len(rems), len(items))
		}
	})

	t.Run("unknown project falls back rather than guessing", func(t *testing.T) {
		h, spaces, ctx := setup(t, `{"action":"reminder","title":"x","project":"Ghost","remind_at":"2026-08-21T17:00"}`)
		rec := send(t, h, "remind me about x for Ghost")
		if !strings.Contains(rec.Body.String(), "Saved to your donezo inbox") {
			t.Fatalf("reply = %s", rec.Body)
		}
		if rems, _ := spaces.ListReminders(ctx, "sandbox"); len(rems) != 0 {
			t.Errorf("created a reminder for a non-existent project: %+v", rems)
		}
	})
}
