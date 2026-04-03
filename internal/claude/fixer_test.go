package claude

import (
	"context"
	"testing"

	"github.com/njayp/clio"
)

func TestFixer_GeneratesFix(t *testing.T) {
	respJSON := `{
		"title": "Fix nil pointer in auth handler",
		"body": "Add nil check before accessing user field",
		"reasoning": "The error occurs because user can be nil when auth token is expired",
		"file_changes": {
			"pkg/auth/handler.go": "package auth\n\nfunc Handler() {\n\tif user == nil {\n\t\treturn\n\t}\n}"
		}
	}`

	client := newTestAnthropicClient(t, anthropicResponse(t, respJSON))
	fixer := NewFixer(client, "claude-sonnet-4-20250514")

	event := clio.ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"panic: nil pointer", "\t/app/pkg/auth/handler.go:42"},
	}
	class := clio.Classification{
		IsCodeBug:     true,
		Summary:       "Nil pointer in auth handler",
		RelevantFiles: []string{"pkg/auth/handler.go"},
	}
	files := map[string]string{
		"pkg/auth/handler.go": "package auth\n\nfunc Handler() {\n\t_ = user.Name\n}",
	}

	fix, err := fixer.GenerateFix(context.Background(), event, class, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fix.Title != "Fix nil pointer in auth handler" {
		t.Errorf("Title = %q", fix.Title)
	}
	if len(fix.FileChanges) != 1 {
		t.Errorf("FileChanges has %d entries, want 1", len(fix.FileChanges))
	}
	if fix.Branch == "" {
		t.Error("Branch should not be empty")
	}
	if fix.Reasoning == "" {
		t.Error("Reasoning should not be empty")
	}
}

func TestFixer_NoFileChanges(t *testing.T) {
	respJSON := `{
		"title": "Cannot fix",
		"body": "",
		"reasoning": "This requires infrastructure changes",
		"file_changes": {}
	}`

	client := newTestAnthropicClient(t, anthropicResponse(t, respJSON))
	fixer := NewFixer(client, "claude-sonnet-4-20250514")

	event := clio.ErrorEvent{PodName: "app-1", Container: "web", Repo: "owner/repo", LogLines: []string{"ERROR"}}
	class := clio.Classification{IsCodeBug: true, Summary: "Some error"}

	_, err := fixer.GenerateFix(context.Background(), event, class, nil)
	if err == nil {
		t.Error("expected error when no file changes")
	}
}
