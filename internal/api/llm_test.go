package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bgrewell/donezo/internal/auth"
	"github.com/bgrewell/donezo/internal/llm"
)

// fakeLLM is a countable stand-in for a configured model, following the
// PasswordHasher precedent: the seam is the interface, not the network.
type fakeLLM struct {
	reply  string
	err    error
	calls  int
	system string
	user   string
}

func (f *fakeLLM) Complete(_ context.Context, system, user string) (string, error) {
	f.calls++
	f.system, f.user = system, user
	return f.reply, f.err
}

func (f *fakeLLM) Provider() string { return "fake" }
func (f *fakeLLM) Model() string    { return "fake-1" }

func TestLLMStatusDisabledByDefault(t *testing.T) {
	t.Parallel()
	rec := doJSON(t, newTestServer(t).Handler(), http.MethodGet, "/api/llm", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body)
	}
	var got struct {
		Enabled  bool   `json:"enabled"`
		Provider string `json:"provider"`
		Prompts  []struct {
			ID string `json:"id"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Enabled {
		t.Error("no model configured, so enabled should be false")
	}
	if got.Provider != "" {
		t.Errorf("disabled status should not name a provider, got %q", got.Provider)
	}
	// Prompts are listed either way: they describe what donezo can do, not
	// what this instance happens to have switched on.
	if len(got.Prompts) == 0 {
		t.Error("built-in prompts should be listed even when disabled")
	}
}

func TestLLMStatusEnabled(t *testing.T) {
	t.Parallel()
	h := newTestServer(t, WithLLM(&fakeLLM{reply: "ok"})).Handler()
	rec := doJSON(t, h, http.MethodGet, "/api/llm", "")
	var got struct {
		Enabled  bool   `json:"enabled"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !got.Enabled || got.Provider != "fake" || got.Model != "fake-1" {
		t.Errorf("status = %+v", got)
	}
}

func TestLLMRewrite(t *testing.T) {
	t.Parallel()
	fake := &fakeLLM{reply: "Rotate the runner PATs before Friday."}
	h := newTestServer(t, WithLLM(fake)).Handler()

	rec := doJSON(t, h, http.MethodPost, "/api/llm/rewrite",
		`{"promptId":"polish-capture","text":"  rotate teh runner pats b4 fri  "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rewrite = %d (body %s)", rec.Code, rec.Body)
	}
	var got struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Text != fake.reply {
		t.Errorf("text = %q, want the model's reply", got.Text)
	}
	if fake.calls != 1 {
		t.Errorf("calls = %d, want 1", fake.calls)
	}
	// The prompt's system text is sent, and the user text is trimmed.
	if !strings.Contains(fake.system, "clean up") {
		t.Errorf("system prompt not passed through: %q", fake.system)
	}
	if fake.user != "rotate teh runner pats b4 fri" {
		t.Errorf("user text = %q, want it trimmed", fake.user)
	}
}

func TestLLMRewriteRejectsBadInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want int
	}{
		{"unknown prompt", `{"promptId":"nope","text":"x"}`, http.StatusBadRequest},
		{"missing prompt", `{"text":"x"}`, http.StatusBadRequest},
		{"empty text", `{"promptId":"polish-capture","text":"   "}`, http.StatusBadRequest},
		{"unknown field", `{"promptId":"polish-capture","text":"x","temp":1}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTestServer(t, WithLLM(&fakeLLM{reply: "ok"})).Handler()
			rec := doJSON(t, h, http.MethodPost, "/api/llm/rewrite", tt.body)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

// With no model configured the endpoint must say so plainly, so a client
// can hide the affordance rather than surface a confusing failure.
func TestLLMRewriteDisabled(t *testing.T) {
	t.Parallel()
	h := newTestServer(t).Handler()
	rec := doJSON(t, h, http.MethodPost, "/api/llm/rewrite",
		`{"promptId":"polish-capture","text":"x"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (body %s)", rec.Code, rec.Body)
	}
}

func TestLLMRewriteUpstreamFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"unreachable", errors.New("dial tcp: connection refused"), http.StatusBadGateway},
		{"timed out", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"not configured", llm.ErrNotConfigured, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newTestServer(t, WithLLM(&fakeLLM{err: tt.err})).Handler()
			rec := doJSON(t, h, http.MethodPost, "/api/llm/rewrite",
				`{"promptId":"polish-capture","text":"x"}`)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
			// The upstream message can name the endpoint; it must not be
			// echoed to the caller.
			if strings.Contains(rec.Body.String(), "connection refused") {
				t.Errorf("upstream detail leaked to the caller: %s", rec.Body)
			}
		})
	}
}

// Model calls cost money and seconds upstream, so a client stuck in a loop
// must hit a ceiling rather than run indefinitely.
func TestLLMRewriteRateLimited(t *testing.T) {
	t.Parallel()
	fake := &fakeLLM{reply: "ok"}
	limiter := auth.NewRateLimiter(
		auth.WithLimit(2),
		auth.WithWindow(time.Minute),
		auth.WithLimiterClock(fixedClock),
	)
	h := newTestServer(t, WithLLM(fake), WithLLMRateLimiter(limiter)).Handler()
	body := `{"promptId":"polish-capture","text":"x"}`

	for i := 0; i < 2; i++ {
		if rec := doJSON(t, h, http.MethodPost, "/api/llm/rewrite", body); rec.Code != http.StatusOK {
			t.Fatalf("call %d = %d (body %s)", i+1, rec.Code, rec.Body)
		}
	}
	rec := doJSON(t, h, http.MethodPost, "/api/llm/rewrite", body)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third call = %d, want 429 (body %s)", rec.Code, rec.Body)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 should carry Retry-After")
	}
	// The limit is enforced before the model is called, so a throttled
	// request costs nothing upstream.
	if fake.calls != 2 {
		t.Errorf("model called %d times, want 2 — the limiter should gate before the call", fake.calls)
	}
}

// Text past the prompt's limit is refused rather than quietly cut. Every
// caller replaces the user's own words with the reply, so sending a prefix
// would let the rewrite destroy the tail it was meant to tidy.
func TestLLMRewriteRefusesOverlongText(t *testing.T) {
	t.Parallel()
	fake := &fakeLLM{reply: "ok"}
	h := newTestServer(t, WithLLM(fake)).Handler()

	body, err := json.Marshal(llmRewriteRequest{
		PromptID: "polish-capture",
		Text:     strings.Repeat("a", llm.MaxInputRunes+1),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := doJSON(t, h, http.MethodPost, "/api/llm/rewrite", string(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "too long") {
		t.Errorf("body = %s, want it to say the text is too long", rec.Body)
	}
	if fake.calls != 0 {
		t.Errorf("model called %d times; overlong text should be refused before the call", fake.calls)
	}

	// Exactly at the limit still goes through.
	body, err = json.Marshal(llmRewriteRequest{
		PromptID: "polish-capture",
		Text:     strings.Repeat("a", llm.MaxInputRunes),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/llm/rewrite", string(body)); rec.Code != http.StatusOK {
		t.Errorf("text at the limit = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
}

// A reply the model cut off at its output ceiling is a partial rewrite.
// Handing it back would let the caller overwrite the capture with half of
// it, so it is reported as a failure and nothing is changed.
func TestLLMRewriteTruncatedReply(t *testing.T) {
	t.Parallel()
	h := newTestServer(t, WithLLM(&fakeLLM{err: llm.ErrReplyTruncated})).Handler()
	rec := doJSON(t, h, http.MethodPost, "/api/llm/rewrite",
		`{"promptId":"polish-capture","text":"x"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "cut off") {
		t.Errorf("body = %s, want it to say the reply was cut off", rec.Body)
	}
}

// An operator's on-disk override is only worth anything if it is what
// actually reaches the model, so assert against the system text the client
// was handed rather than the handler's response.
func TestLLMRewriteUsesOverriddenPrompt(t *testing.T) {
	t.Parallel()
	const override = "Custom wording from disk."
	set := llm.NewPromptSet([]llm.Prompt{{
		ID:          llm.PromptPolishCapture.ID,
		Description: llm.PromptPolishCapture.Description,
		System:      override,
	}})
	fake := &fakeLLM{reply: "tidied"}
	h := newTestServer(t, WithLLM(fake), WithPrompts(set)).Handler()

	rec := doJSON(t, h, http.MethodPost, "/api/llm/rewrite",
		`{"promptId":"polish-capture","text":"rotate teh pats"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rewrite = %d (body %s)", rec.Code, rec.Body)
	}
	if fake.system != override {
		t.Errorf("system = %q, want the override %q", fake.system, override)
	}
	if fake.system == llm.PromptPolishCapture.System {
		t.Error("handler used the built-in prompt despite an injected override")
	}
}

func TestLLMStatusListsInjectedPrompts(t *testing.T) {
	t.Parallel()
	set := llm.NewPromptSet([]llm.Prompt{
		{ID: "only-this", Description: "the injected one", System: "sys"},
	})
	h := newTestServer(t, WithPrompts(set)).Handler()
	rec := doJSON(t, h, http.MethodGet, "/api/llm", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body)
	}
	var got struct {
		Prompts []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got.Prompts) != 1 || got.Prompts[0].ID != "only-this" {
		t.Fatalf("prompts = %+v, want only the injected one", got.Prompts)
	}
	if got.Prompts[0].Description != "the injected one" {
		t.Errorf("description = %q, want the injected one", got.Prompts[0].Description)
	}
}

// The injected set is the whole world: a built-in id that is not in it must
// not resolve, or an override that renames a prompt would silently fall back.
func TestLLMRewriteRejectsIDOutsideInjectedSet(t *testing.T) {
	t.Parallel()
	set := llm.NewPromptSet([]llm.Prompt{
		{ID: "only-this", Description: "the injected one", System: "sys"},
	})
	fake := &fakeLLM{reply: "tidied"}
	h := newTestServer(t, WithLLM(fake), WithPrompts(set)).Handler()
	rec := doJSON(t, h, http.MethodPost, "/api/llm/rewrite",
		`{"promptId":"polish-capture","text":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body)
	}
	if fake.calls != 0 {
		t.Errorf("model called %d times for an unknown prompt id", fake.calls)
	}
}
