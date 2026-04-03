package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
)

// callClaude sends a prompt to Claude and JSON-unmarshals the response into dest.
func callClaude(ctx context.Context, client anthropic.Client, model anthropic.Model, maxTokens int64, prompt string, dest any) error {
	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return fmt.Errorf("anthropic API error: %w", err)
	}
	if len(resp.Content) == 0 {
		return fmt.Errorf("empty response from API")
	}
	text := resp.Content[0].Text
	if err := json.Unmarshal([]byte(text), dest); err != nil {
		return fmt.Errorf("failed to parse response: %w (raw: %s)", err, text)
	}
	return nil
}

// ClaudeClassifier classifies errors using the Anthropic API.
type ClaudeClassifier struct {
	client anthropic.Client
	model  anthropic.Model
}

// NewClaudeClassifier creates a classifier with the given client and model.
func NewClaudeClassifier(client anthropic.Client, model string) *ClaudeClassifier {
	return &ClaudeClassifier{client: client, model: anthropic.Model(model)}
}

type classifyResponse struct {
	IsCodeBug     bool     `json:"is_code_bug"`
	Summary       string   `json:"summary"`
	Confidence    float64  `json:"confidence"`
	RelevantFiles []string `json:"relevant_files"`
}

// Classify sends the error event to Claude for classification.
func (c *ClaudeClassifier) Classify(ctx context.Context, event ErrorEvent) (Classification, error) {
	var cr classifyResponse
	if err := callClaude(ctx, c.client, c.model, 1024, ClassifyPrompt(event), &cr); err != nil {
		return Classification{}, fmt.Errorf("classify: %w", err)
	}

	slog.Info("classified error",
		"pod", event.PodName,
		"is_code_bug", cr.IsCodeBug,
		"confidence", cr.Confidence,
		"summary", cr.Summary,
	)

	return Classification{
		IsCodeBug:     cr.IsCodeBug,
		Summary:       cr.Summary,
		Confidence:    cr.Confidence,
		RelevantFiles: cr.RelevantFiles,
	}, nil
}
