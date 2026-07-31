package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSelectsProvider(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		cfg          Config
		wantProvider string
		wantModel    string
		wantErr      string
	}{
		{
			name:         "empty provider is disabled, not an error",
			cfg:          Config{},
			wantProvider: "none",
		},
		{
			name:         "anthropic defaults its model",
			cfg:          Config{Provider: ProviderAnthropic, APIKey: "sk-test"},
			wantProvider: ProviderAnthropic,
			wantModel:    DefaultAnthropicModel,
		},
		{
			name:         "anthropic honors an explicit model",
			cfg:          Config{Provider: ProviderAnthropic, APIKey: "sk-test", Model: "claude-haiku-4-5"},
			wantProvider: ProviderAnthropic,
			wantModel:    "claude-haiku-4-5",
		},
		{
			name:    "anthropic without a key is refused at construction",
			cfg:     Config{Provider: ProviderAnthropic},
			wantErr: "API key",
		},
		{
			name:         "openai-compatible needs no key",
			cfg:          Config{Provider: ProviderOpenAICompatible, BaseURL: "http://localhost:11434/v1", Model: "llama3"},
			wantProvider: ProviderOpenAICompatible,
			wantModel:    "llama3",
		},
		{
			name:    "openai-compatible without a base URL is refused",
			cfg:     Config{Provider: ProviderOpenAICompatible, Model: "llama3"},
			wantErr: "base URL",
		},
		{
			name:    "openai-compatible without a model is refused",
			cfg:     Config{Provider: ProviderOpenAICompatible, BaseURL: "http://localhost:11434/v1"},
			wantErr: "model name",
		},
		{
			name:    "unknown provider is refused",
			cfg:     Config{Provider: "hal9000"},
			wantErr: "unknown provider",
		},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := New(tt.cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got.Provider() != tt.wantProvider {
				t.Errorf("provider = %q, want %q", got.Provider(), tt.wantProvider)
			}
			if got.Model() != tt.wantModel {
				t.Errorf("model = %q, want %q", got.Model(), tt.wantModel)
			}
		})
	}
}

func TestDisabledReportsNotConfigured(t *testing.T) {
	t.Parallel()
	if _, err := (Disabled{}).Complete(context.Background(), "s", "u"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

func TestPromptByID(t *testing.T) {
	t.Parallel()
	if _, ok := PromptByID("nope"); ok {
		t.Error("unknown prompt id should not resolve")
	}
	got, ok := PromptByID(PromptPolishCapture.ID)
	if !ok {
		t.Fatal("built-in prompt should resolve by id")
	}
	if got.System == "" {
		t.Error("resolved prompt has no system text")
	}
	// The capture prompt is the one place a note's own text is fed to a
	// model, so it must tell the model not to act on what it reads.
	if !strings.Contains(got.System, "not a request addressed to you") {
		t.Error("capture prompt should refuse to follow instructions in the captured text")
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	if got := Truncate("short"); got != "short" {
		t.Errorf("short input changed: %q", got)
	}
	// Multi-byte runes must not be split: cutting mid-rune would send
	// invalid UTF-8 upstream.
	long := strings.Repeat("é", maxInputRunes+50)
	got := Truncate(long)
	if len([]rune(got)) != maxInputRunes {
		t.Errorf("truncated to %d runes, want %d", len([]rune(got)), maxInputRunes)
	}
	if !strings.HasPrefix(long, got) {
		t.Error("truncation should be a prefix of the input")
	}
}

// ─── OpenAI-compatible transport ─────────────────────────────────────────

// newFakeEndpoint serves one canned /v1/chat/completions response.
func newFakeEndpoint(t *testing.T, status int, body string, capture *http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			*capture = *r.Clone(r.Context())
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenAICompatibleComplete(t *testing.T) {
	t.Parallel()
	var got http.Request
	srv := newFakeEndpoint(t, http.StatusOK,
		`{"choices":[{"message":{"role":"assistant","content":"  Cleaned up.  "}}]}`, &got)

	client, err := New(Config{
		Provider: ProviderOpenAICompatible,
		BaseURL:  srv.URL + "/v1",
		Model:    "llama3",
		APIKey:   "local-key",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	reply, err := client.Complete(context.Background(), "be tidy", "raw text")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if reply != "Cleaned up." {
		t.Errorf("reply = %q, want the trimmed text", reply)
	}
	if got.URL.Path != "/v1/chat/completions" {
		t.Errorf("path = %q", got.URL.Path)
	}
	if auth := got.Header.Get("Authorization"); auth != "Bearer local-key" {
		t.Errorf("Authorization = %q", auth)
	}
}

// A local runtime usually has no key; sending an empty bearer header would
// make some servers reject an otherwise valid request.
func TestOpenAICompatibleOmitsEmptyAuth(t *testing.T) {
	t.Parallel()
	var got http.Request
	srv := newFakeEndpoint(t, http.StatusOK,
		`{"choices":[{"message":{"content":"ok"}}]}`, &got)
	client, err := New(Config{
		Provider: ProviderOpenAICompatible, BaseURL: srv.URL + "/v1", Model: "llama3",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Complete(context.Background(), "s", "u"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, present := got.Header["Authorization"]; present {
		t.Error("no key configured, so no Authorization header should be sent")
	}
}

func TestOpenAICompatibleErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"structured upstream error", http.StatusBadRequest,
			`{"error":{"message":"model not found"}}`, "model not found"},
		{"bare non-2xx", http.StatusInternalServerError, `not json`, "500"},
		{"no choices", http.StatusOK, `{"choices":[]}`, "no choices"},
		{"empty content", http.StatusOK, `{"choices":[{"message":{"content":"   "}}]}`, "no text"},
		{"unparseable success", http.StatusOK, `{`, "decode response"},
	}
	for _, tt := range tests {
		tt := tt // capture for parallel subtests (golangci-lint predates Go 1.22 loopvar)
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newFakeEndpoint(t, tt.status, tt.body, nil)
			client, err := New(Config{
				Provider: ProviderOpenAICompatible, BaseURL: srv.URL + "/v1", Model: "llama3",
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = client.Complete(context.Background(), "s", "u")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

// A model that never answers must not hang the caller: the configured
// timeout has to actually bound the round trip.
func TestOpenAICompatibleTimeout(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	client, err := New(Config{
		Provider: ProviderOpenAICompatible,
		BaseURL:  srv.URL + "/v1",
		Model:    "llama3",
		Timeout:  50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	start := time.Now()
	if _, err := client.Complete(context.Background(), "s", "u"); err == nil {
		t.Fatal("a hung endpoint should fail, not succeed")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %s; the timeout did not bound the call", elapsed)
	}
}

// The request body must be the shape a local runtime expects, including
// stream:false — a streaming reply would not parse.
func TestOpenAICompatibleRequestShape(t *testing.T) {
	t.Parallel()
	bodies := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		bodies <- buf
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	client, err := New(Config{
		Provider: ProviderOpenAICompatible, BaseURL: srv.URL + "/v1", Model: "llama3",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Complete(context.Background(), "SYSTEM", "USER"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var sent chatRequest
	if err := json.Unmarshal(<-bodies, &sent); err != nil {
		t.Fatalf("parse sent body: %v", err)
	}
	if sent.Model != "llama3" || sent.Stream {
		t.Errorf("sent = %+v, want model llama3 and stream false", sent)
	}
	if len(sent.Messages) != 2 ||
		sent.Messages[0].Role != "system" || sent.Messages[0].Content != "SYSTEM" ||
		sent.Messages[1].Role != "user" || sent.Messages[1].Content != "USER" {
		t.Errorf("messages = %+v", sent.Messages)
	}
}
