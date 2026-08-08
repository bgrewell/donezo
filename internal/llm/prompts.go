package llm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PromptDirName is the directory, relative to the data directory, that holds
// prompt files.
const PromptDirName = "prompts"

// maxPromptFileBytes bounds an override file. Prompts are a paragraph or
// two; a file dramatically larger is a mistake — a pasted transcript, a
// wrong path — and loading it would spend tokens on every call.
const maxPromptFileBytes = 64 << 10

// defaultSuffix names the reference copy written for every built-in prompt.
// It is rewritten on every start and is never read back, so the shipped
// wording stays visible next to an override and keeps up with upgrades.
const defaultSuffix = ".default.txt"

// overrideSuffix names the file an operator edits to replace a prompt body.
const overrideSuffix = ".txt"

// coreSuffix names the reference copy of the part that is always appended and
// never replaceable. Written every start, like the default; reading it back
// would defeat the point of it being fixed.
const coreSuffix = ".core.txt"

// PromptSet is the set of prompts one donezod serves.
//
// It is built once at startup and read-only afterwards, so it is safe for
// concurrent use. Handlers take it by injection rather than reaching for a
// package global, which is what lets a test run against a known set.
type PromptSet struct {
	prompts    []Prompt
	byID       map[string]Prompt
	overridden []string
}

// NewPromptSet builds a set from prompts, in the order given. Later entries
// with a duplicate id replace earlier ones but keep the original position,
// so the order callers see stays stable.
func NewPromptSet(prompts []Prompt) *PromptSet {
	s := &PromptSet{byID: make(map[string]Prompt, len(prompts))}
	for _, p := range prompts {
		if _, seen := s.byID[p.ID]; seen {
			for i := range s.prompts {
				if s.prompts[i].ID == p.ID {
					s.prompts[i] = p
					break
				}
			}
		} else {
			s.prompts = append(s.prompts, p)
		}
		s.byID[p.ID] = p
	}
	return s
}

// BuiltInPromptSet returns the prompts donezo ships, with no overrides.
func BuiltInPromptSet() *PromptSet { return NewPromptSet(BuiltInPrompts) }

// All returns the prompts in a stable order. The result is a copy: callers
// cannot reach in and mutate the set.
func (s *PromptSet) All() []Prompt {
	if s == nil {
		return nil
	}
	out := make([]Prompt, len(s.prompts))
	copy(out, s.prompts)
	return out
}

// ByID returns a prompt by id.
func (s *PromptSet) ByID(id string) (Prompt, bool) {
	if s == nil {
		return Prompt{}, false
	}
	p, ok := s.byID[id]
	return p, ok
}

// Overridden lists, sorted, the ids whose instruction text came from disk
// rather than from the built-in. Startup logs it so an operator running a
// tweaked prompt can see that from the service output.
func (s *PromptSet) Overridden() []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s.overridden))
	copy(out, s.overridden)
	return out
}

// LoadPrompts returns donezo's prompts with any on-disk overrides applied.
//
// For each built-in prompt it writes "<id>.default.txt" (the shipped body) and
// "<id>.core.txt" (the fixed part appended to every prompt) into dir, both
// refreshed on every start: the shipped wording stays visible next to an
// override and keeps up with upgrades. If "<id>.txt" exists and holds more
// than whitespace, its contents replace that prompt's BODY. The core is not
// replaceable — see Prompt.Core for why.
//
// The returned set is always usable, including when err is non-nil — a data
// directory that cannot be written is a reason to run on the built-in
// prompts and say so, not a reason to refuse to serve. Callers should log
// the error and carry on.
func LoadPrompts(dir string) (*PromptSet, error) {
	if strings.TrimSpace(dir) == "" {
		return BuiltInPromptSet(), nil
	}
	// 0o700 matches the rest of the data directory, which is private to the
	// donezo user; an operator edits these as that user or as root.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return BuiltInPromptSet(), fmt.Errorf("llm: create prompt dir %s: %w", dir, err)
	}

	prompts := make([]Prompt, 0, len(BuiltInPrompts))
	var overridden []string
	var problems []error

	for _, p := range BuiltInPrompts {
		refPath := filepath.Join(dir, p.ID+defaultSuffix)
		if err := os.WriteFile(refPath, []byte(p.Body+"\n"), 0o600); err != nil {
			problems = append(problems, fmt.Errorf("write %s: %w", refPath, err))
		}
		// The core is written too, read-only in practice, so what is always
		// appended is visible rather than a surprise in the request log.
		corePath := filepath.Join(dir, p.ID+coreSuffix)
		if err := os.WriteFile(corePath, []byte(p.Core+"\n"), 0o600); err != nil {
			problems = append(problems, fmt.Errorf("write %s: %w", corePath, err))
		}

		body, ok, err := readOverride(filepath.Join(dir, p.ID+overrideSuffix))
		if err != nil {
			problems = append(problems, err)
		}
		if ok {
			// Replaces the body only. The core is not the operator's to drop
			// any more than it is the user's — it is what keeps a rewrite from
			// being harmful rather than merely not to taste.
			p.Body = body
			overridden = append(overridden, p.ID)
		}
		prompts = append(prompts, p)
	}

	sort.Strings(overridden)
	set := NewPromptSet(prompts)
	set.overridden = overridden
	return set, errors.Join(problems...)
}

// readOverride reads one override file. A missing file, or one holding only
// whitespace, reports ok=false: an empty override would otherwise send an
// empty instruction upstream, which is worse than the built-in it replaced.
func readOverride(path string) (string, bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("%s is a directory, not a prompt file", path)
	}
	if info.Size() > maxPromptFileBytes {
		return "", false, fmt.Errorf("%s is %d bytes, over the %d-byte limit; ignoring it",
			path, info.Size(), maxPromptFileBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "", false, nil
	}
	return text, true, nil
}
