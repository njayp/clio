package main

import (
	"os"
	"testing"
	"time"
)

func setEnv(t *testing.T, envs map[string]string) {
	t.Helper()
	for k, v := range envs {
		t.Setenv(k, v)
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"CLIO_REPO":        "owner/repo",
		"CLIO_RELEASE":     "myapp",
		"ANTHROPIC_API_KEY": "sk-test",
		"GITHUB_TOKEN":     "ghp-test",
		"CLIO_NAMESPACE":   "staging",
	}
}

func TestLoadConfig_Valid(t *testing.T) {
	setEnv(t, validEnv())

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want %q", cfg.Repo, "owner/repo")
	}
	if cfg.Release != "myapp" {
		t.Errorf("Release = %q, want %q", cfg.Release, "myapp")
	}
	if cfg.Target != "" {
		t.Errorf("Target = %q, want empty", cfg.Target)
	}
	if cfg.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want default", cfg.Model)
	}
	if cfg.Cooldown != time.Hour {
		t.Errorf("Cooldown = %v, want 1h", cfg.Cooldown)
	}
	if cfg.MaxConcurrency != 3 {
		t.Errorf("MaxConcurrency = %d, want 3", cfg.MaxConcurrency)
	}
	if cfg.TailLines != 100 {
		t.Errorf("TailLines = %d, want 100", cfg.TailLines)
	}
	if cfg.MaxPRsPerHour != 5 {
		t.Errorf("MaxPRsPerHour = %d, want 5", cfg.MaxPRsPerHour)
	}
	if cfg.BatchWindow != 5*time.Second {
		t.Errorf("BatchWindow = %v, want 5s", cfg.BatchWindow)
	}
	if cfg.DryRun {
		t.Error("DryRun should default to false")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
}

func TestLoadConfig_MissingRequired(t *testing.T) {
	tests := []struct {
		name   string
		remove string
		errMsg string
	}{
		{"missing repo", "CLIO_REPO", "CLIO_REPO is required"},
		{"missing release", "CLIO_RELEASE", "CLIO_RELEASE is required"},
		{"missing anthropic key", "ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY is required"},
		{"missing github token", "GITHUB_TOKEN", "GITHUB_TOKEN is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := validEnv()
			delete(env, tt.remove)
			setEnv(t, env)
			os.Unsetenv(tt.remove)

			_, err := LoadConfig()
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != tt.errMsg {
				t.Errorf("error = %q, want %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestLoadConfig_CustomValues(t *testing.T) {
	env := validEnv()
	env["CLIO_TARGET"] = "myapp-web"
	env["CLIO_MODEL"] = "claude-opus-4-20250514"
	env["CLIO_COOLDOWN"] = "30m"
	env["CLIO_MAX_CONCURRENCY"] = "10"
	env["CLIO_TAIL_LINES"] = "500"
	env["CLIO_MAX_PRS_PER_HOUR"] = "2"
	env["CLIO_BATCH_WINDOW"] = "10s"
	env["CLIO_DRY_RUN"] = "true"
	env["CLIO_PORT"] = "9090"
	setEnv(t, env)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Target != "myapp-web" {
		t.Errorf("Target = %q, want %q", cfg.Target, "myapp-web")
	}
	if cfg.Model != "claude-opus-4-20250514" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if cfg.Cooldown != 30*time.Minute {
		t.Errorf("Cooldown = %v", cfg.Cooldown)
	}
	if cfg.MaxConcurrency != 10 {
		t.Errorf("MaxConcurrency = %d", cfg.MaxConcurrency)
	}
	if cfg.TailLines != 500 {
		t.Errorf("TailLines = %d", cfg.TailLines)
	}
	if cfg.MaxPRsPerHour != 2 {
		t.Errorf("MaxPRsPerHour = %d", cfg.MaxPRsPerHour)
	}
	if cfg.BatchWindow != 10*time.Second {
		t.Errorf("BatchWindow = %v", cfg.BatchWindow)
	}
	if !cfg.DryRun {
		t.Error("DryRun should be true")
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d", cfg.Port)
	}
}
