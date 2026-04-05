package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/njayp/clio"
)

func TestAgentResult_Parse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantBug   bool
		wantTitle string
		wantPRURL string
		wantErr   bool
	}{
		{
			name:      "code bug with PR",
			input:     `{"is_code_bug": true, "pr_url": "https://github.com/owner/repo/pull/1", "title": "Fix nil pointer", "reasoning": "Missing nil check"}`,
			wantBug:   true,
			wantTitle: "Fix nil pointer",
			wantPRURL: "https://github.com/owner/repo/pull/1",
		},
		{
			name:    "operational issue with issue URL",
			input:   `{"is_code_bug": false, "issue_url": "https://github.com/owner/repo/issues/2", "title": "", "reasoning": "Misconfigured memory limits"}`,
			wantBug: false,
		},
		{
			name:    "operational issue",
			input:   `{"is_code_bug": false, "title": "", "reasoning": "OOMKilled due to memory limits"}`,
			wantBug: false,
		},
		{
			name:    "invalid JSON",
			input:   `not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result clio.AgentResult
			err := json.Unmarshal([]byte(tt.input), &result)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.IsCodeBug != tt.wantBug {
				t.Errorf("IsCodeBug = %v, want %v", result.IsCodeBug, tt.wantBug)
			}
			if result.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", result.Title, tt.wantTitle)
			}
			if result.PRURL != tt.wantPRURL {
				t.Errorf("PRURL = %q, want %q", result.PRURL, tt.wantPRURL)
			}
		})
	}
}

func TestBuildInvestigationPrompt(t *testing.T) {
	event := clio.ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"panic: nil pointer dereference"},
		K8sContext: &clio.K8sContext{
			DeployName: "myapp",
			ImageTag:   "v1.2.3",
			Events:     []string{"Started", "Unhealthy"},
			Replicas:   3,
		},
	}

	prompt := BuildInvestigationPrompt(event, "clio/fix-abc12345")

	mustContain := []string{
		"Pod: app-1",
		"Namespace: staging",
		"Container: web",
		"panic: nil pointer dereference",
		"Deployment: myapp",
		"Image Tag: v1.2.3",
		"Replicas: 3",
		"clio-plan.md",
		"Do NOT modify source code",
		"clio/fix-abc12345",
	}
	for _, check := range mustContain {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}

	mustNotContain := []string{"gh pr create", "git push origin", "git commit"}
	for _, check := range mustNotContain {
		if strings.Contains(prompt, check) {
			t.Errorf("prompt should not contain %q", check)
		}
	}
}

func TestInvestigationSystemPrompt(t *testing.T) {
	sp := InvestigationSystemPrompt()
	if !strings.Contains(sp, "Clio") {
		t.Error("system prompt should mention Clio")
	}
	if !strings.Contains(sp, "Investigation") {
		t.Error("system prompt should mention Investigation")
	}
	if strings.Contains(sp, "gh pr create") {
		t.Error("system prompt should not contain \"gh pr create\"")
	}
}

func TestWriteContextFile(t *testing.T) {
	dir := t.TempDir()
	event := clio.ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"panic: nil pointer dereference"},
		K8sContext: &clio.K8sContext{
			DeployName: "myapp",
			Replicas:   2,
		},
	}

	if err := writeContextFile(dir, event); err != nil {
		t.Fatalf("writeContextFile: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, contextFile))
	if err != nil {
		t.Fatalf("read context file: %v", err)
	}
	content := string(data)

	mustContain := []string{
		"Pod: app-1",
		"Namespace: staging",
		"panic: nil pointer dereference",
		"Deployment: myapp",
	}
	for _, check := range mustContain {
		if !strings.Contains(content, check) {
			t.Errorf("context file missing %q", check)
		}
	}
}

func TestBuildShipPrompt(t *testing.T) {
	prompt := BuildShipPrompt("clio/fix-abc12345")

	mustContain := []string{
		"clio/fix-abc12345",
		"git push",
		"gh pr create",
		"RESULT.json",
		"clio-context.md",
		"clio-plan.md",
		"git log HEAD --not origin/HEAD",
		"is_code_bug",
		"pr_url",
	}
	for _, check := range mustContain {
		if !strings.Contains(prompt, check) {
			t.Errorf("ship prompt missing %q", check)
		}
	}
}

func TestWriteClaudeConfig(t *testing.T) {
	dir := t.TempDir()
	if err := WriteClaudeConfig(dir, clio.ClaudeConfig); err != nil {
		t.Fatalf("WriteClaudeConfig: %v", err)
	}

	expected := []string{
		".claude/skills/go/SKILL.md",
		".claude/agents/plan-reviewer.md",
	}
	for _, path := range expected {
		full := filepath.Join(dir, path)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected file %s to exist: %v", path, err)
		}
	}
}

func TestReadResult(t *testing.T) {
	dir := t.TempDir()

	// Missing file should wrap os.ErrNotExist
	_, err := readResult(dir)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}

	// Valid file should parse
	os.WriteFile(filepath.Join(dir, "RESULT.json"), []byte(`{"is_code_bug":false,"reasoning":"OOM"}`), 0o644)
	result, err := readResult(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsCodeBug {
		t.Error("expected IsCodeBug=false")
	}
}

func TestAppendToGitignore(t *testing.T) {
	dir := t.TempDir()
	if err := appendToGitignore(dir, "clio-plan.md", "RESULT.json"); err != nil {
		t.Fatalf("appendToGitignore: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "clio-plan.md") {
		t.Error(".gitignore missing clio-plan.md")
	}
	if !strings.Contains(content, "RESULT.json") {
		t.Error(".gitignore missing RESULT.json")
	}
}
