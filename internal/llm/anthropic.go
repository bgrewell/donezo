package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// This file implements the Anthropic provider on the official Go SDK.
// Calls go through the SDK rather than hand-rolled HTTP so request shapes,
// retries, and error typing stay correct as the API moves.

// DefaultAnthropicModel is used when no model is configured.
const DefaultAnthropicModel = "claude-opus-5"

// anthropicMaxTokens bounds the reply. donezo's prompts return a tidied
// version of their input, so the ceiling only needs to cover growth on the
// longest input this package accepts — not a long-form answer.
const anthropicMaxTokens = 2048

// anthropicClient calls the Claude API.
type anthropicClient struct {
	client anthropic.Client
	model  string
}

// newAnthropic builds the Anthropic provider. An API key is required: the
// hosted API has no anonymous mode, and failing here beats failing on every
// later request.
func newAnthropic(cfg Config) (Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("llm: the anthropic provider needs an API key")
	}
	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithRequestTimeout(cfg.Timeout),
	}
	// A base URL override points at a gateway or proxy that speaks the
	// Anthropic API; without one the SDK's own default is used.
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	model := cfg.Model
	if model == "" {
		model = DefaultAnthropicModel
	}
	return &anthropicClient{client: anthropic.NewClient(opts...), model: model}, nil
}

// Complete sends one non-streaming message and returns the reply text.
func (c *anthropicClient) Complete(ctx context.Context, system, user string) (string, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: anthropicMaxTokens,
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("llm: anthropic request: %w", err)
	}
	// A safety classifier can decline a request: that arrives as a normal
	// response with no content, not an error, so it has to be checked
	// before reading the blocks.
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "", errors.New("llm: the model declined this request")
	}
	var out strings.Builder
	for _, block := range resp.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			out.WriteString(text.Text)
		}
	}
	reply := strings.TrimSpace(out.String())
	if reply == "" {
		return "", errors.New("llm: the model returned no text")
	}
	return reply, nil
}

// Provider names the provider.
func (c *anthropicClient) Provider() string { return ProviderAnthropic }

// Model names the configured model.
func (c *anthropicClient) Model() string { return c.model }
