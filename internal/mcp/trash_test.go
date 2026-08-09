package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/bgrewell/donezo/internal/store"
)

// #16 over MCP: delete_item is no longer destructive, and an agent can see and
// undo what it removed.

func TestDeleteItemIsRecoverableOverMCP(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{
		ID: "tsk-x", Title: "deleted by mistake", Status: "open", CreatedAt: "2026-08-09",
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	if text, isErr := f.callTool(t, f.rw, "delete_item",
		`{"space_id":"sandbox","kind":"task","item_id":"tsk-x"}`); isErr {
		t.Fatalf("delete_item: %s", text)
	}
	if tasks, _ := f.spaces.ListTasks(ctx, "sandbox"); len(tasks) != 0 {
		t.Errorf("task still live after delete: %+v", tasks)
	}

	text, isErr := f.callTool(t, f.rw, "list_trash", `{"space_id":"sandbox"}`)
	if isErr {
		t.Fatalf("list_trash: %s", text)
	}
	if !strings.Contains(text, "tsk-x") || !strings.Contains(text, "deleted by mistake") {
		t.Errorf("trash listing should show the item and what it was: %s", text)
	}

	if text, isErr := f.callTool(t, f.rw, "restore_item",
		`{"space_id":"sandbox","kind":"task","item_id":"tsk-x"}`); isErr {
		t.Fatalf("restore_item: %s", text)
	}
	tasks, err := f.spaces.ListTasks(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "deleted by mistake" {
		t.Errorf("task not restored: %+v", tasks)
	}
}

// A project can be restored over MCP even though it cannot be deleted here:
// undoing a destructive action is not itself destructive.
func TestRestoreProjectOverMCPBringsBackItsBatch(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	loom := "loom"
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{
		ID: "tsk-owned", ProjectID: &loom, Title: "owned", Status: "open", CreatedAt: "2026-08-09",
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	// Deleted the way the web app does it, which is the only way today.
	if _, err := f.spaces.SoftDeleteProject(ctx, "sandbox", "loom"); err != nil {
		t.Fatalf("soft delete project: %v", err)
	}

	text, isErr := f.callTool(t, f.rw, "restore_item",
		`{"space_id":"sandbox","kind":"project","item_id":"loom"}`)
	if isErr {
		t.Fatalf("restore_item: %s", text)
	}
	if !strings.Contains(text, "deleted together with") {
		t.Errorf("a multi-row restore should say so: %s", text)
	}
	if _, err := f.spaces.GetProject(ctx, "sandbox", "loom"); err != nil {
		t.Errorf("project not restored: %v", err)
	}
	tasks, err := f.spaces.ListTasks(ctx, "sandbox")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("owned task not restored with the batch: %+v", tasks)
	}
}

// delete_item still refuses projects, and the refusal still explains itself.
func TestDeleteItemStillRefusesProjects(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	text, isErr := f.callTool(t, f.rw, "delete_item",
		`{"space_id":"sandbox","kind":"project","item_id":"loom"}`)
	if !isErr || !strings.Contains(text, "cascades") {
		t.Errorf("isErr=%v text=%s, want a refusal explaining the cascade", isErr, text)
	}
	if _, err := f.spaces.GetProject(context.Background(), "sandbox", "loom"); err != nil {
		t.Errorf("project should be untouched: %v", err)
	}
}

func TestRestoreItemRefusals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.spaces.CreateTask(ctx, "sandbox", store.TaskItem{
		ID: "tsk-live", Title: "live", Status: "open", CreatedAt: "2026-08-09",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, tc := range []struct{ name, args, want string }{
		{"never deleted", `{"space_id":"sandbox","kind":"task","item_id":"tsk-live"}`, "no trashed task"},
		{"unknown id", `{"space_id":"sandbox","kind":"task","item_id":"ghost"}`, "no trashed task"},
		{"missing id", `{"space_id":"sandbox","kind":"task"}`, "item_id is required"},
		{"unknown kind", `{"space_id":"sandbox","kind":"sandwich","item_id":"x"}`, "kind must be one of"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, isErr := f.callTool(t, f.rw, "restore_item", tc.args)
			if !isErr || !strings.Contains(text, tc.want) {
				t.Errorf("isErr=%v text=%q, want an error containing %q", isErr, text, tc.want)
			}
		})
	}
	// And the live task was not disturbed by any of that.
	if tasks, _ := f.spaces.ListTasks(ctx, "sandbox"); len(tasks) != 1 {
		t.Errorf("live tasks = %d, want 1", len(tasks))
	}
}
