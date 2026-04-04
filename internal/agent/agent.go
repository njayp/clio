package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/njayp/clio"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	agentDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "clio_agent_duration_seconds",
		Help:    "Duration of Claude Code agent sessions",
		Buckets: prometheus.ExponentialBuckets(10, 2, 8), // 10s to ~21min
	})
	agentOutcome = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "clio_agent_outcome_total",
		Help: "Outcomes of agent sessions",
	}, []string{"outcome"})
)

func init() {
	prometheus.MustRegister(agentDuration, agentOutcome)
}

// Agent spawns Claude Code subprocesses to investigate and fix errors.
type Agent struct {
	ws  *Workspace
	cfg clio.Config
}

// NewAgent creates an Agent with the given workspace and config.
func NewAgent(ws *Workspace, cfg clio.Config) *Agent {
	return &Agent{ws: ws, cfg: cfg}
}

// Run investigates an error event using Claude Code in a git worktree.
// Returns the agent's result, or nil if the error is not actionable.
func (a *Agent) Run(ctx context.Context, event clio.ErrorEvent) (*clio.AgentResult, error) {
	start := time.Now()

	if err := a.ws.EnsureClone(ctx); err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("ensure clone: %w", err)
	}

	fp := clio.Fingerprint(event)
	branch := fmt.Sprintf("%sfix-%s", clio.BranchPrefix, fp[:8])

	// Clean up stale branch from previous incomplete runs
	a.ws.DeleteBranch(ctx, branch)

	wtDir, err := a.ws.CreateWorktree(ctx, branch)
	if err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("create worktree: %w", err)
	}
	defer a.ws.RemoveWorktree(ctx, wtDir)

	prompt := BuildPrompt(event, branch)

	// Build allowed tools list
	allowedTools := []string{
		"Read", "Write", "Edit", "Glob", "Grep",
		"Bash(go build ./...)", "Bash(go test ./...)", "Bash(go vet ./...)",
		"Bash(git diff)", "Bash(git status)", "Bash(git add)", "Bash(git commit)", "Bash(git reset)",
	}
	if !a.cfg.DryRun {
		allowedTools = append(allowedTools,
			"Bash(git push)", "Bash(gh pr create)", "Bash(gh issue create)",
		)
	}

	args := []string{
		"-p", prompt,
		"--output-format", "json",
		"--allowedTools", strings.Join(allowedTools, ","),
		"--max-turns", strconv.Itoa(a.cfg.MaxAgentTurns),
		"--max-cost", a.cfg.MaxAgentBudget,
		"--append-system-prompt", SystemPrompt(),
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = wtDir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Clio",
		"GIT_AUTHOR_EMAIL=clio@noreply.github.com",
		"GIT_COMMITTER_NAME=Clio",
		"GIT_COMMITTER_EMAIL=clio@noreply.github.com",
		"GITHUB_TOKEN="+a.cfg.GitHubToken,
	)

	slog.Info("spawning agent", "pod", event.PodName, "branch", branch, "worktree", wtDir, "dry_run", a.cfg.DryRun)
	output, err := cmd.CombinedOutput()
	duration := time.Since(start).Seconds()
	agentDuration.Observe(duration)

	if err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("claude agent exited with error: %w\noutput: %s", err, output)
	}

	// Read RESULT.json
	resultPath := filepath.Join(wtDir, "RESULT.json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("read RESULT.json: %w", err)
	}

	var result clio.AgentResult
	if err := json.Unmarshal(data, &result); err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("parse RESULT.json: %w (raw: %s)", err, data)
	}

	switch {
	case result.PRURL != "":
		agentOutcome.WithLabelValues("pr_created").Inc()
		slog.Info("agent created PR", "pod", event.PodName, "pr_url", result.PRURL, "duration_s", duration)
	case result.IssueURL != "":
		agentOutcome.WithLabelValues("issue_created").Inc()
		slog.Info("agent created issue", "pod", event.PodName, "issue_url", result.IssueURL, "duration_s", duration)
	case result.IsCodeBug:
		agentOutcome.WithLabelValues("code_bug_no_pr").Inc()
		slog.Warn("agent found code bug but did not create PR", "pod", event.PodName, "reasoning", result.Reasoning)
	default:
		agentOutcome.WithLabelValues("operational").Inc()
		slog.Info("agent determined operational issue", "pod", event.PodName, "reasoning", result.Reasoning)
	}

	return &result, nil
}
