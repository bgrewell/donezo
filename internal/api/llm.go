package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/bgrewell/donezo/internal/llm"
)

// This file serves the optional language-model features. Everything here
// degrades to "switched off": with no model configured the status endpoint
// reports disabled and the rewrite endpoint answers 503, so the web UI can
// simply not offer the affordance rather than showing a broken one.
//
// Model calls are rate-limited per user. Unlike the rest of the API they
// cost real money and real seconds upstream, so an accidental loop in a
// client should hit a ceiling rather than a bill.

// llmStatus is the GET /api/llm body: what the UI needs to decide whether
// to offer model-backed affordances at all.
type llmStatus struct {
	// Enabled reports whether a model is configured.
	Enabled bool `json:"enabled"`
	// Provider names the configured provider, empty when disabled.
	Provider string `json:"provider,omitempty"`
	// Model names the configured model, empty when disabled.
	Model string `json:"model,omitempty"`
	// Prompts are the built-in prompts available to callers.
	Prompts []llmPromptView `json:"prompts"`
}

// llmPromptView describes one prompt without exposing its instruction
// text, which is donezo's to tune rather than a client's to depend on.
type llmPromptView struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// llmRewriteRequest is the POST /api/llm/rewrite body.
type llmRewriteRequest struct {
	// PromptID selects a built-in prompt.
	PromptID string `json:"promptId"`
	// Text is the content to run the prompt over.
	Text string `json:"text"`
}

// llmRewriteResponse carries the model's reply.
type llmRewriteResponse struct {
	Text string `json:"text"`
}

// handleLLMStatus reports whether model features are available.
func (s *Server) handleLLMStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := userFrom(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	available := s.promptSet().All()
	prompts := make([]llmPromptView, 0, len(available))
	for _, p := range available {
		prompts = append(prompts, llmPromptView{ID: p.ID, Description: p.Description})
	}
	client := s.llmClient()
	_, disabled := client.(llm.Disabled)
	status := llmStatus{Enabled: !disabled, Prompts: prompts}
	if !disabled {
		status.Provider = client.Provider()
		status.Model = client.Model()
	}
	writeJSON(w, http.StatusOK, status)
}

// handleLLMRewrite runs a built-in prompt over the supplied text.
func (s *Server) handleLLMRewrite(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req llmRewriteRequest
	if !s.decodeBody(w, r, &req) {
		return
	}
	prompt, found := s.promptSet().ByID(req.PromptID)
	if !found {
		writeError(w, http.StatusBadRequest, "unknown promptId")
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if err := llm.CheckInput(text); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"text is too long to rewrite (limit %d characters)", llm.MaxInputRunes))
		return
	}

	client := s.llmClient()
	if _, disabled := client.(llm.Disabled); disabled {
		writeError(w, http.StatusServiceUnavailable, "no language model is configured")
		return
	}
	if allowed, retryAfter := s.llmLimiter.Allow(strconv.FormatInt(user.ID, 10)); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "too many model requests; try again shortly")
		return
	}

	// Bound the call independently of the client's own timeout so a
	// provider that ignores it still cannot outlive the server's write
	// deadline and turn a failure into an apparent hang.
	ctx, cancel := context.WithTimeout(r.Context(), llm.DefaultTimeout)
	defer cancel()

	reply, err := client.Complete(ctx, prompt.System, text)
	switch {
	case errors.Is(err, llm.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "no language model is configured")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "the model took too long to respond")
	case errors.Is(err, llm.ErrReplyTruncated):
		// Returning the partial reply would let the caller overwrite the
		// user's text with half of it.
		writeError(w, http.StatusBadGateway, "the model's reply was cut off; nothing was changed")
	case err != nil:
		// The upstream message can carry endpoint details; log it and give
		// the caller something calm and actionable instead.
		s.logger.Printf("llm rewrite: %v", err)
		writeError(w, http.StatusBadGateway, "the language model could not be reached")
	default:
		writeJSON(w, http.StatusOK, llmRewriteResponse{Text: reply})
	}
}

// llmClient returns the configured client, defaulting to disabled so
// handlers never have to nil-check.
func (s *Server) llmClient() llm.Client {
	if s.llm == nil {
		return llm.Disabled{}
	}
	return s.llm
}

// promptSet returns the prompts this instance serves, defaulting to the
// built-ins for the same reason llmClient defaults to disabled.
func (s *Server) promptSet() *llm.PromptSet {
	if s.prompts == nil {
		return llm.BuiltInPromptSet()
	}
	return s.prompts
}
