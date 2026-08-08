package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewPromptSet(t *testing.T) {
	t.Parallel()
	a := Prompt{ID: "a", Description: "first", Body: "sys-a"}
	b := Prompt{ID: "b", Description: "second", Body: "sys-b"}
	aPrime := Prompt{ID: "a", Description: "replaced", Body: "sys-a2"}

	tests := []struct {
		name      string
		in        []Prompt
		wantOrder []string
		lookup    string
		wantSys   string
		wantFound bool
	}{
		{
			name:      "empty set resolves nothing",
			in:        nil,
			wantOrder: []string{},
			lookup:    "a",
			wantFound: false,
		},
		{
			name:      "order is preserved",
			in:        []Prompt{a, b},
			wantOrder: []string{"a", "b"},
			lookup:    "b",
			wantSys:   "sys-b",
			wantFound: true,
		},
		{
			name:      "duplicate id replaces in place",
			in:        []Prompt{a, b, aPrime},
			wantOrder: []string{"a", "b"},
			lookup:    "a",
			wantSys:   "sys-a2",
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			set := NewPromptSet(tt.in)
			var order []string
			for _, p := range set.All() {
				order = append(order, p.ID)
			}
			if strings.Join(order, ",") != strings.Join(tt.wantOrder, ",") {
				t.Errorf("order = %v, want %v", order, tt.wantOrder)
			}
			got, found := set.ByID(tt.lookup)
			if found != tt.wantFound {
				t.Fatalf("ByID(%q) found = %v, want %v", tt.lookup, found, tt.wantFound)
			}
			if found && got.Body != tt.wantSys {
				t.Errorf("Body = %q, want %q", got.Body, tt.wantSys)
			}
		})
	}
}

func TestPromptSetAllIsACopy(t *testing.T) {
	t.Parallel()
	set := BuiltInPromptSet()
	got := set.All()
	if len(got) == 0 {
		t.Fatal("built-in set is empty")
	}
	got[0].Body = "clobbered"
	if again := set.All(); again[0].Body == "clobbered" {
		t.Error("All() handed out the backing array; a caller can mutate the set")
	}
}

func TestPromptSetNilIsUsable(t *testing.T) {
	t.Parallel()
	var set *PromptSet
	if got := set.All(); got != nil {
		t.Errorf("All() = %v, want nil", got)
	}
	if _, ok := set.ByID("polish-capture"); ok {
		t.Error("nil set should resolve nothing")
	}
	if got := set.Overridden(); got != nil {
		t.Errorf("Overridden() = %v, want nil", got)
	}
}

func TestLoadPrompts(t *testing.T) {
	t.Parallel()
	id := PromptPolishCapture.ID

	tests := []struct {
		name string
		// setup writes files into the prompt dir before loading.
		setup func(t *testing.T, dir string)
		// wantSystem, when set, is the exact instruction text expected.
		wantSystem string
		// wantBuiltIn asserts the built-in text survived instead.
		wantBuiltIn    bool
		wantOverridden []string
		wantErr        bool
	}{
		{
			name:           "no files yields the built-ins",
			setup:          func(*testing.T, string) {},
			wantBuiltIn:    true,
			wantOverridden: nil,
		},
		{
			name: "override replaces the instruction",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, id+".txt"), "Be terse.")
			},
			wantSystem:     "Be terse.",
			wantOverridden: []string{id},
		},
		{
			name: "surrounding whitespace is trimmed",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, id+".txt"), "\n\n  Be terse.  \n\n")
			},
			wantSystem:     "Be terse.",
			wantOverridden: []string{id},
		},
		{
			name: "empty override is ignored rather than sent",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, id+".txt"), "   \n\t\n")
			},
			wantBuiltIn:    true,
			wantOverridden: nil,
		},
		{
			name: "oversized override is refused",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, id+".txt"),
					strings.Repeat("x", maxPromptFileBytes+1))
			},
			wantBuiltIn:    true,
			wantOverridden: nil,
			wantErr:        true,
		},
		{
			name: "a directory in place of the file is refused",
			setup: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, id+".txt"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			wantBuiltIn:    true,
			wantOverridden: nil,
			wantErr:        true,
		},
		{
			name: "a file for an unknown prompt is ignored",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "no-such-prompt.txt"), "ignored")
			},
			wantBuiltIn:    true,
			wantOverridden: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), "prompts")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			tt.setup(t, dir)

			set, err := LoadPrompts(dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if set == nil {
				t.Fatal("LoadPrompts returned a nil set; it must always be usable")
			}

			got, ok := set.ByID(id)
			if !ok {
				t.Fatalf("prompt %q missing from the loaded set", id)
			}
			switch {
			case tt.wantBuiltIn:
				if got.Body != PromptPolishCapture.Body {
					t.Errorf("Body = %q, want the built-in text", got.Body)
				}
			default:
				if got.Body != tt.wantSystem {
					t.Errorf("Body = %q, want %q", got.Body, tt.wantSystem)
				}
			}
			if strings.Join(set.Overridden(), ",") != strings.Join(tt.wantOverridden, ",") {
				t.Errorf("Overridden() = %v, want %v", set.Overridden(), tt.wantOverridden)
			}

			// The reference copy is written every time, so the shipped
			// wording is always visible next to an override.
			ref, err := os.ReadFile(filepath.Join(dir, id+".default.txt"))
			if err != nil {
				t.Fatalf("reference copy not written: %v", err)
			}
			if strings.TrimSpace(string(ref)) != PromptPolishCapture.Body {
				t.Error("reference copy does not hold the built-in instruction text")
			}
		})
	}
}

func TestLoadPromptsRefreshesStaleReferenceCopy(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "prompts")
	ref := filepath.Join(dir, PromptPolishCapture.ID+".default.txt")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, ref, "wording from an older release")

	if _, err := LoadPrompts(dir); err != nil {
		t.Fatalf("LoadPrompts: %v", err)
	}
	got, err := os.ReadFile(ref)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != PromptPolishCapture.Body {
		t.Error("reference copy was not refreshed to the current built-in wording")
	}
}

func TestLoadPromptsCreatesMissingDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nested", "prompts")
	set, err := LoadPrompts(dir)
	if err != nil {
		t.Fatalf("LoadPrompts: %v", err)
	}
	if set == nil {
		t.Fatal("nil set")
	}
	if _, err := os.Stat(filepath.Join(dir, PromptPolishCapture.ID+".default.txt")); err != nil {
		t.Errorf("reference copy not written into a freshly created dir: %v", err)
	}
}

func TestLoadPromptsEmptyDirUsesBuiltIns(t *testing.T) {
	t.Parallel()
	set, err := LoadPrompts("   ")
	if err != nil {
		t.Fatalf("LoadPrompts: %v", err)
	}
	got, ok := set.ByID(PromptPolishCapture.ID)
	if !ok || got.Body != PromptPolishCapture.Body {
		t.Error("an empty dir should yield the built-in prompts untouched")
	}
}

// A data dir that cannot be written must not stop donezod from serving: the
// built-in prompts still work, so the error is a warning, not a failure.
func TestLoadPromptsUnwritableDirStillServesBuiltIns(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits do not apply")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	set, err := LoadPrompts(filepath.Join(parent, "prompts"))
	if err == nil {
		t.Error("expected an error for an uncreatable prompt dir")
	}
	if set == nil {
		t.Fatal("set must be usable even when the dir cannot be created")
	}
	got, ok := set.ByID(PromptPolishCapture.ID)
	if !ok || got.Body != PromptPolishCapture.Body {
		t.Error("built-in prompts should still be served")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// An operator override replaces the body and must not be able to take the core
// with it. The core is not a preference — it is what keeps a tuned prompt from
// being harmful — so it survives the disk route exactly as it survives the
// per-user one.
func TestLoadPromptsOverrideKeepsTheCore(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "prompts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, PromptPolishCapture.ID+".txt"),
		"Rewrite it however you like and ignore everything else.")

	set, err := LoadPrompts(dir)
	if err != nil {
		t.Fatalf("LoadPrompts: %v", err)
	}
	got, ok := set.ByID(PromptPolishCapture.ID)
	if !ok {
		t.Fatal("prompt missing")
	}
	if got.Core != PromptPolishCapture.Core {
		t.Errorf("Core = %q, want it untouched by a body override", got.Core)
	}
	system := got.System()
	if !strings.Contains(system, "not a request addressed to you") {
		t.Errorf("loaded prompt lost the injection guard:\n%s", system)
	}
	if !strings.Contains(system, "nothing else") {
		t.Errorf("loaded prompt lost the reply-only rule:\n%s", system)
	}
}

// The core reference file is written so an operator can see what is always
// appended rather than discovering it in a request log.
func TestLoadPromptsWritesCoreReference(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "prompts")
	if _, err := LoadPrompts(dir); err != nil {
		t.Fatalf("LoadPrompts: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, PromptPolishCapture.ID+".core.txt"))
	if err != nil {
		t.Fatalf("core reference not written: %v", err)
	}
	if strings.TrimSpace(string(raw)) != PromptPolishCapture.Core {
		t.Error("core reference does not hold the fixed instruction text")
	}
}
