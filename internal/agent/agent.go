package agent

import (
	"context"
	"encoding/json"
	"errors"
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

const (
	planFile   = "clio-plan.md"
	resultFile = "RESULT.json"

	// Budget allocation percentages per step.
	investigationBudgetPct = 25
	reviewBudgetPct        = 15
	implementationBudgetPct = 50

	// Turn allocation percentages per step.
	investigationTurnsPct  = 40
	reviewTurnsPct         = 30
	implementationTurnsPct = 60
)

// Agent spawns Claude Code subprocesses to investigate and fix errors.
type Agent struct {
	ws  *Workspace
	cfg clio.Config
}

// NewAgent creates an Agent with the given workspace and config.
func NewAgent(ws *Workspace, cfg clio.Config) *Agent {
	return &Agent{ws: ws, cfg: cfg}
}

// claudeEnv returns the environment variables for Claude Code subprocesses.
func (a *Agent) claudeEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=Clio",
		"GIT_AUTHOR_EMAIL=clio@noreply.github.com",
		"GIT_COMMITTER_NAME=Clio",
		"GIT_COMMITTER_EMAIL=clio@noreply.github.com",
		"GITHUB_TOKEN="+a.cfg.GitHubToken,
	)
}

// runClaude executes a Claude Code subprocess and returns its output.
func (a *Agent) runClaude(ctx context.Context, wtDir string, args []string) ([]byte, error) {
	return a.runCmd(ctx, wtDir, "claude", args...)
}

// runCmd executes a command in the worktree with the agent's environment.
func (a *Agent) runCmd(ctx context.Context, wtDir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = wtDir
	cmd.Env = a.claudeEnv()
	return cmd.CombinedOutput()
}

// Run investigates an error event using a 3-step Claude Code workflow in a git worktree.
func (a *Agent) Run(ctx context.Context, event clio.ErrorEvent) (*clio.AgentResult, error) {
	start := time.Now()

	if err := a.ws.EnsureClone(ctx); err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("ensure clone: %w", err)
	}

	fp := clio.Fingerprint(event)
	branch := fmt.Sprintf("%sfix-%s", clio.BranchPrefix, fp[:8])

	a.ws.DeleteBranch(ctx, branch)

	wtDir, err := a.ws.CreateWorktree(ctx, branch)
	if err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("create worktree: %w", err)
	}
	defer a.ws.RemoveWorktree(ctx, wtDir)

	if err := WriteClaudeConfig(wtDir, clio.ClaudeConfig); err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("write claude config: %w", err)
	}

	slog.Info("spawning agent", "pod", event.PodName, "branch", branch, "worktree", wtDir, "dry_run", a.cfg.DryRun)

	// Step 1: Investigate
	if err := a.runInvestigation(ctx, wtDir, event, branch); err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("investigation: %w", err)
	}

	// If no plan was written, the agent determined this is operational — read RESULT.json
	result, err := readResult(wtDir)
	if err == nil {
		a.logResult(result, event, time.Since(start).Seconds())
		return result, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, err
	}

	// Plan exists — append plan and result files to .gitignore so /go's commit doesn't include them
	if err := appendToGitignore(wtDir, planFile, resultFile); err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("update .gitignore: %w", err)
	}

	// Step 2: Review plan (non-fatal)
	if err := a.runPlanReview(ctx, wtDir); err != nil {
		slog.Warn("plan review failed, proceeding with unreviewed plan", "error", err)
	}

	// Step 3: Implement
	if err := a.runImplementation(ctx, wtDir); err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("implementation: %w", err)
	}

	if a.cfg.DryRun {
		duration := time.Since(start).Seconds()
		agentDuration.Observe(duration)
		agentOutcome.WithLabelValues("pr_created").Inc()
		slog.Info("dry run — skipping push and PR", "pod", event.PodName, "duration_s", duration)
		return &clio.AgentResult{IsCodeBug: true, Title: "dry run", Reasoning: "dry run"}, nil
	}

	// Step 4: Push and create PR
	result, err = a.pushAndCreatePR(ctx, wtDir, event, branch)
	if err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("push and create PR: %w", err)
	}

	duration := time.Since(start).Seconds()
	agentDuration.Observe(duration)
	a.logResult(result, event, duration)
	return result, nil
}

// runInvestigation executes the investigation step (step 1).
func (a *Agent) runInvestigation(ctx context.Context, wtDir string, event clio.ErrorEvent, branch string) error {
	prompt := BuildInvestigationPrompt(event, branch)

	allowedTools := []string{
		"Read", "Glob", "Grep",
		"Bash(go test ./...)", "Bash(go build ./...)", "Bash(go vet ./...)",
		"Bash(git status)", "Bash(git diff)",
		"Bash(gh issue create)",
		"Write",
	}

	budget := scaleBudget(a.cfg.MaxAgentBudget, investigationBudgetPct)
	turns := a.cfg.MaxAgentTurns * investigationTurnsPct / 100

	args := []string{
		"-p", prompt,
		"--output-format", "json",
		"--allowedTools", strings.Join(allowedTools, ","),
		"--max-turns", strconv.Itoa(turns),
		"--max-cost", budget,
		"--append-system-prompt", InvestigationSystemPrompt(),
	}

	output, err := a.runClaude(ctx, wtDir, args)
	if err != nil {
		return fmt.Errorf("claude agent exited with error: %w\noutput: %s", err, output)
	}
	return nil
}

// runPlanReview executes the plan review step (step 2).
func (a *Agent) runPlanReview(ctx context.Context, wtDir string) error {
	budget := scaleBudget(a.cfg.MaxAgentBudget, reviewBudgetPct)
	turns := a.cfg.MaxAgentTurns * reviewTurnsPct / 100

	args := []string{
		"-p", "/plan " + planFile,
		"--output-format", "json",
		"--dangerously-skip-permissions",
		"--max-turns", strconv.Itoa(turns),
		"--max-cost", budget,
	}

	output, err := a.runClaude(ctx, wtDir, args)
	if err != nil {
		return fmt.Errorf("plan review failed: %w\noutput: %s", err, output)
	}
	return nil
}

// runImplementation executes the implementation step (step 3).
func (a *Agent) runImplementation(ctx context.Context, wtDir string) error {
	budget := scaleBudget(a.cfg.MaxAgentBudget, implementationBudgetPct)
	turns := a.cfg.MaxAgentTurns * implementationTurnsPct / 100

	args := []string{
		"-p", "/go " + planFile,
		"--output-format", "json",
		"--dangerously-skip-permissions",
		"--max-turns", strconv.Itoa(turns),
		"--max-cost", budget,
	}

	output, err := a.runClaude(ctx, wtDir, args)
	if err != nil {
		return fmt.Errorf("implementation failed: %w\noutput: %s", err, output)
	}
	return nil
}

// pushAndCreatePR pushes the branch and creates a GitHub PR.
func (a *Agent) pushAndCreatePR(ctx context.Context, wtDir string, event clio.ErrorEvent, branch string) (*clio.AgentResult, error) {
	// Check for new commits
	commitOutput, err := a.runCmd(ctx, wtDir, "git", "log", "HEAD", "--not", "origin/HEAD", "--oneline")
	if err != nil || len(strings.TrimSpace(string(commitOutput))) == 0 {
		return nil, fmt.Errorf("no new commits found on branch %s", branch)
	}

	// Push
	if out, err := a.runCmd(ctx, wtDir, "git", "push", "origin", branch); err != nil {
		return nil, fmt.Errorf("git push: %w\n%s", err, out)
	}

	// Read plan for PR body
	planContent, err := os.ReadFile(filepath.Join(wtDir, planFile))
	if err != nil {
		return nil, fmt.Errorf("read plan for PR body: %w", err)
	}

	// Get commit subject for PR title
	titleOutput, err := a.runCmd(ctx, wtDir, "git", "log", "-1", "--format=%s")
	if err != nil {
		return nil, fmt.Errorf("get commit subject: %w", err)
	}
	title := strings.TrimSpace(string(titleOutput))

	// Create PR
	body := BuildPRBody(event, string(planContent))
	prOutput, err := a.runCmd(ctx, wtDir, "gh", "pr", "create",
		"--head", branch,
		"--title", title,
		"--body", body,
	)
	if err != nil {
		return nil, fmt.Errorf("gh pr create: %w\n%s", err, prOutput)
	}

	prURL := strings.TrimSpace(string(prOutput))

	return &clio.AgentResult{
		IsCodeBug: true,
		PRURL:     prURL,
		Title:     title,
		Reasoning: string(planContent),
	}, nil
}

// readResult parses RESULT.json from the worktree.
// Returns os.ErrNotExist (wrapped) if the file doesn't exist.
func readResult(wtDir string) (*clio.AgentResult, error) {
	data, err := os.ReadFile(filepath.Join(wtDir, resultFile))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", resultFile, err)
	}
	var result clio.AgentResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse %s: %w (raw: %s)", resultFile, err, data)
	}
	return &result, nil
}

// appendToGitignore appends entries to the worktree's .gitignore.
func appendToGitignore(wtDir string, entries ...string) error {
	f, err := os.OpenFile(filepath.Join(wtDir, ".gitignore"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, e := range entries {
		if _, err := fmt.Fprintln(f, e); err != nil {
			return err
		}
	}
	return nil
}

// logResult logs the agent outcome with appropriate level and metrics.
func (a *Agent) logResult(result *clio.AgentResult, event clio.ErrorEvent, duration float64) {
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
}

func scaleBudget(total string, pct int) string {
	f, err := strconv.ParseFloat(total, 64)
	if err != nil {
		return total
	}
	return fmt.Sprintf("%.2f", f*float64(pct)/100)
}
