package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// ClaudeFixer generates code fixes using the Anthropic API.
type ClaudeFixer struct {
	client anthropic.Client
	model  anthropic.Model
}

// NewClaudeFixer creates a fixer with the given client and model.
func NewClaudeFixer(client anthropic.Client, model string) *ClaudeFixer {
	return &ClaudeFixer{client: client, model: anthropic.Model(model)}
}

type fixResponse struct {
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	Reasoning   string            `json:"reasoning"`
	FileChanges map[string]string `json:"file_changes"`
}

// GenerateFix sends the error context and source files to Claude to generate a fix.
func (f *ClaudeFixer) GenerateFix(ctx context.Context, event ErrorEvent, class Classification, files map[string]string) (Fix, error) {
	var fr fixResponse
	if err := callClaude(ctx, f.client, f.model, 4096, FixPrompt(event, class, files), &fr); err != nil {
		return Fix{}, fmt.Errorf("fix: %w", err)
	}

	if len(fr.FileChanges) == 0 {
		slog.Warn("claude could not generate fix",
			"pod", event.PodName,
			"reasoning", fr.Reasoning,
		)
		return Fix{}, fmt.Errorf("fix: no file changes proposed (reasoning: %s)", fr.Reasoning)
	}

	branch := BranchName(Fingerprint(event), class.Summary)
	rawLogs := strings.Join(event.LogLines, "\n")

	slog.Info("generated fix",
		"pod", event.PodName,
		"branch", branch,
		"files_changed", len(fr.FileChanges),
	)

	return Fix{
		Branch:      branch,
		Title:       fr.Title,
		Body:        fr.Body,
		FileChanges: fr.FileChanges,
		RawLogs:     rawLogs,
		Reasoning:   fr.Reasoning,
	}, nil
}
