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
// Returns nil if the agent determines the error is not a code bug.
func (a *Agent) Run(ctx context.Context, event clio.ErrorEvent) (*clio.Fix, error) {
	start := time.Now()

	if err := a.ws.EnsureClone(ctx); err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("ensure clone: %w", err)
	}

	fp := clio.Fingerprint(event)
	tmpBranch := fmt.Sprintf("clio-tmp-%s-%d", fp[:8], time.Now().Unix())
	wtDir, err := a.ws.CreateWorktree(ctx, tmpBranch)
	if err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("create worktree: %w", err)
	}
	defer a.ws.RemoveWorktree(ctx, wtDir)

	prompt := BuildPrompt(event)

	// Build claude command
	args := []string{
		"-p", prompt,
		"--output-format", "json",
		"--allowedTools", "Read,Write,Edit,Glob,Grep,Bash(go build ./...),Bash(go test ./...),Bash(go vet ./...),Bash(git diff),Bash(git status)",
		"--max-turns", strconv.Itoa(a.cfg.MaxAgentTurns),
		"--max-cost", a.cfg.MaxAgentBudget,
		"--append-system-prompt", SystemPrompt(),
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = wtDir

	slog.Info("spawning agent", "pod", event.PodName, "worktree", wtDir)
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

	if !result.IsCodeBug {
		agentOutcome.WithLabelValues("operational").Inc()
		slog.Info("agent determined operational issue",
			"pod", event.PodName,
			"reasoning", result.Reasoning,
		)
		return nil, nil
	}

	// Stage changes and get clean diff (excludes RESULT.json)
	diff, err := a.ws.StageAndDiff(ctx, wtDir)
	if err != nil {
		slog.Warn("failed to stage and diff", "error", err)
	}

	// Generate branch name and commit+push (changes already staged)
	branch := clio.BranchName(fp, result.Title)
	if err := a.ws.CommitAndPush(ctx, wtDir, branch, result.Title); err != nil {
		agentOutcome.WithLabelValues("error").Inc()
		return nil, fmt.Errorf("commit and push: %w", err)
	}

	agentOutcome.WithLabelValues("code_bug").Inc()
	rawLogs := strings.Join(event.LogLines, "\n")

	slog.Info("agent generated fix",
		"pod", event.PodName,
		"branch", branch,
		"duration_s", duration,
	)

	return &clio.Fix{
		Branch:    branch,
		Title:     result.Title,
		Body:      result.Body,
		DiffPatch: diff,
		RawLogs:   rawLogs,
		Reasoning: result.Reasoning,
	}, nil
}
