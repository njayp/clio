package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/njayp/clio"
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

// Classifier classifies errors using the Anthropic API.
type Classifier struct {
	client anthropic.Client
	model  anthropic.Model
}

// NewClassifier creates a classifier with the given client and model.
func NewClassifier(client anthropic.Client, model string) *Classifier {
	return &Classifier{client: client, model: anthropic.Model(model)}
}

type classifyResponse struct {
	IsCodeBug     bool     `json:"is_code_bug"`
	Summary       string   `json:"summary"`
	Confidence    float64  `json:"confidence"`
	RelevantFiles []string `json:"relevant_files"`
}

// Classify sends the error event to Claude for classification.
func (c *Classifier) Classify(ctx context.Context, event clio.ErrorEvent) (clio.Classification, error) {
	var cr classifyResponse
	if err := callClaude(ctx, c.client, c.model, 1024, classifyPrompt(event), &cr); err != nil {
		return clio.Classification{}, fmt.Errorf("classify: %w", err)
	}

	slog.Info("classified error",
		"pod", event.PodName,
		"is_code_bug", cr.IsCodeBug,
		"confidence", cr.Confidence,
		"summary", cr.Summary,
	)

	return clio.Classification{
		IsCodeBug:     cr.IsCodeBug,
		Summary:       cr.Summary,
		Confidence:    cr.Confidence,
		RelevantFiles: cr.RelevantFiles,
	}, nil
}
