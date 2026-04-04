package agent

import (
	"encoding/json"
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

func TestBuildPrompt(t *testing.T) {
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

	prompt := BuildPrompt(event, "clio/fix-abc12345")

	checks := []string{
		"Pod: app-1",
		"Namespace: staging",
		"Container: web",
		"panic: nil pointer dereference",
		"Deployment: myapp",
		"Image Tag: v1.2.3",
		"Replicas: 3",
		"RESULT.json",
		"go build",
		"go test",
		"clio/fix-abc12345",
		"gh pr create",
		"gh issue create",
		"git push origin",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing %q", check)
		}
	}
}

func TestSystemPrompt(t *testing.T) {
	sp := SystemPrompt()
	if !strings.Contains(sp, "Clio") {
		t.Error("system prompt should mention Clio")
	}
	if !strings.Contains(sp, "RESULT.json") {
		t.Error("system prompt should mention RESULT.json")
	}
	if !strings.Contains(sp, "gh pr create") {
		t.Error("system prompt should mention gh pr create")
	}
}
