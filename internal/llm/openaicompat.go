package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// This file implements the OpenAI-compatible provider: any endpoint that
// serves POST /v1/chat/completions. That covers the local runtimes people
// actually run — Ollama, LM Studio, llama.cpp's server, vLLM — plus most
// hosted gateways, which is what makes "point donezo at my own model" work
// without a provider-specific implementation per runtime.
//
// This is hand-rolled rather than SDK-backed because the target is the
// wire format, not one vendor: a local runtime implements the shape, not a
// particular company's client library.

// maxResponseBytes bounds what is read from the endpoint. The configured
// endpoint is trusted, but a wedged or misconfigured server should not be
// able to grow donezod's memory without limit.
const maxResponseBytes = 1 << 20

// openAICompatClient calls a /v1/chat/completions endpoint.
type openAICompatClient struct {
	http    *http.Client
	baseURL string
	model   string
	apiKey  string
}

// newOpenAICompatible builds the OpenAI-compatible provider. A base URL is
// required — there is no sensible default, since the whole point is that
// the endpoint is wherever the operator is running a model. A key is not:
// local runtimes are typically unauthenticated.
func newOpenAICompatible(cfg Config) (Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("llm: the openai-compatible provider needs a base URL " +
			"(for example http://localhost:11434/v1)")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("llm: the openai-compatible provider needs a model name")
	}
	return &openAICompatClient{
		http:    &http.Client{Timeout: cfg.Timeout},
		baseURL: base,
		model:   cfg.Model,
		apiKey:  cfg.APIKey,
	}, nil
}

// chatRequest is the request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// chatMessage is one message in the conversation.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the subset of the reply donezo reads.
type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends one non-streaming chat completion and returns the reply.
func (c *openAICompatClient) Complete(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("llm: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("llm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm: request to %s: %w", c.baseURL, err)
	}
	defer func() {
		// Drain before closing so the connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
	}()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("llm: read response: %w", err)
	}
	var parsed chatResponse
	// A non-2xx may or may not carry a JSON error body; decode first so a
	// structured message can be surfaced, and fall back to the status.
	decodeErr := json.Unmarshal(raw, &parsed)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if decodeErr == nil && parsed.Error != nil && parsed.Error.Message != "" {
			return "", fmt.Errorf("llm: model endpoint refused the request: %s", parsed.Error.Message)
		}
		return "", fmt.Errorf("llm: model endpoint returned %s", resp.Status)
	}
	if decodeErr != nil {
		return "", fmt.Errorf("llm: decode response: %w", decodeErr)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("llm: the model returned no choices")
	}
	reply := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if reply == "" {
		return "", errors.New("llm: the model returned no text")
	}
	return reply, nil
}

// Provider names the provider.
func (c *openAICompatClient) Provider() string { return ProviderOpenAICompatible }

// Model names the configured model.
func (c *openAICompatClient) Model() string { return c.model }
