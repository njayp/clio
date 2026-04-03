package claude

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/njayp/clio"
	"github.com/anthropics/anthropic-sdk-go"
)

// Fixer generates code fixes using the Anthropic API.
type Fixer struct {
	client anthropic.Client
	model  anthropic.Model
}

// NewFixer creates a fixer with the given client and model.
func NewFixer(client anthropic.Client, model string) *Fixer {
	return &Fixer{client: client, model: anthropic.Model(model)}
}

type fixResponse struct {
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	Reasoning   string            `json:"reasoning"`
	FileChanges map[string]string `json:"file_changes"`
}

// GenerateFix sends the error context and source files to Claude to generate a fix.
func (f *Fixer) GenerateFix(ctx context.Context, event clio.ErrorEvent, class clio.Classification, files map[string]string) (clio.Fix, error) {
	var fr fixResponse
	if err := callClaude(ctx, f.client, f.model, 4096, fixPrompt(event, class, files), &fr); err != nil {
		return clio.Fix{}, fmt.Errorf("fix: %w", err)
	}

	if len(fr.FileChanges) == 0 {
		slog.Warn("claude could not generate fix",
			"pod", event.PodName,
			"reasoning", fr.Reasoning,
		)
		return clio.Fix{}, fmt.Errorf("fix: no file changes proposed (reasoning: %s)", fr.Reasoning)
	}

	branch := clio.BranchName(clio.Fingerprint(event), class.Summary)
	rawLogs := strings.Join(event.LogLines, "\n")

	slog.Info("generated fix",
		"pod", event.PodName,
		"branch", branch,
		"files_changed", len(fr.FileChanges),
	)

	return clio.Fix{
		Branch:      branch,
		Title:       fr.Title,
		Body:        fr.Body,
		FileChanges: fr.FileChanges,
		RawLogs:     rawLogs,
		Reasoning:   fr.Reasoning,
	}, nil
}
