package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewPromptSet(t *testing.T) {
	t.Parallel()
	a := Prompt{ID: "a", Description: "first", System: "sys-a"}
	b := Prompt{ID: "b", Description: "second", System: "sys-b"}
	aPrime := Prompt{ID: "a", Description: "replaced", System: "sys-a2"}

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
			if found && got.System != tt.wantSys {
				t.Errorf("System = %q, want %q", got.System, tt.wantSys)
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
	got[0].System = "clobbered"
	if again := set.All(); again[0].System == "clobbered" {
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
				if got.System != PromptPolishCapture.System {
					t.Errorf("System = %q, want the built-in text", got.System)
				}
			default:
				if got.System != tt.wantSystem {
					t.Errorf("System = %q, want %q", got.System, tt.wantSystem)
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
			if strings.TrimSpace(string(ref)) != PromptPolishCapture.System {
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
	if strings.TrimSpace(string(got)) != PromptPolishCapture.System {
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
	if !ok || got.System != PromptPolishCapture.System {
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
	if !ok || got.System != PromptPolishCapture.System {
		t.Error("built-in prompts should still be served")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
