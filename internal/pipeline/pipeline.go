package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/njayp/clio"
)

// Prometheus label constants
const (
	skipDedup      = "dedup"
	skipExistingPR = "existing_pr"
	skipRollback   = "rollback"
	skipRateLimit  = "rate_limit"

	classCodeBug     = "code_bug"
	classOperational = "operational"
)

// Triage performs lightweight heuristic classification before invoking the agent.
type Triage interface {
	IsOperational(event clio.ErrorEvent) bool
}

// Agent investigates and fixes errors using Claude Code.
type Agent interface {
	Run(ctx context.Context, event clio.ErrorEvent) (*clio.AgentResult, error)
}

// GitHubClient interacts with the GitHub API for dedup checks.
type GitHubClient interface {
	ListOpenClioPRs(ctx context.Context, repo string) ([]string, error)
}

// PodWatcher discovers pods and streams their logs.
type PodWatcher interface {
	Watch(ctx context.Context) (<-chan clio.ErrorEvent, error)
	GatherContext(ctx context.Context, event *clio.ErrorEvent) error
}

// Pipeline orchestrates the full error-to-PR flow.
type Pipeline struct {
	watcher PodWatcher
	triage  Triage
	agent   Agent
	gh      GitHubClient
	dedup   *Dedup
	batcher *Batcher
	cfg     clio.Config

	// Rate limiting
	mu      sync.Mutex
	prTimes []time.Time
}

// NewPipeline creates a pipeline with all dependencies wired.
func NewPipeline(watcher PodWatcher, triage Triage, agent Agent, gh GitHubClient, cfg clio.Config) *Pipeline {
	return &Pipeline{
		watcher: watcher,
		triage:  triage,
		agent:   agent,
		gh:      gh,
		dedup:   NewDedup(cfg.Cooldown),
		batcher: NewBatcher(cfg.BatchWindow),
		cfg:     cfg,
	}
}

// Run starts the pipeline. Blocks until context is cancelled.
func (p *Pipeline) Run(ctx context.Context) error {
	events, err := p.watcher.Watch(ctx)
	if err != nil {
		return fmt.Errorf("pipeline: watcher failed: %w", err)
	}

	sem := make(chan struct{}, p.cfg.MaxConcurrency)

	// Goroutine: feed error events to batcher (filtering done at source in watcher)
	go func() {
		for {
			select {
			case <-ctx.Done():
				p.batcher.Flush()
				return
			case event, ok := <-events:
				if !ok {
					p.batcher.Flush()
					return
				}
				errorsDetected.Inc()
				p.batcher.Add(event)
			}
		}
	}()

	// Process batched events
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-p.batcher.Events():
			if !ok {
				return nil
			}
			sem <- struct{}{}
			go func(ev clio.ErrorEvent) {
				defer func() { <-sem }()
				p.processEvent(ctx, ev)
			}(event)
		}
	}
}

func (p *Pipeline) processEvent(ctx context.Context, event clio.ErrorEvent) {
	fp := clio.Fingerprint(event)

	// Dedup check
	if p.dedup.IsDuplicate(fp) {
		prsSkipped.WithLabelValues(skipDedup).Inc()
		slog.Info("skipping duplicate error", "pod", event.PodName)
		return
	}

	// Check existing PRs
	fpShort := fp[:8]
	openBranches, err := p.gh.ListOpenClioPRs(ctx, event.Repo)
	if err != nil {
		slog.Error("failed to list open PRs", "error", err)
	} else {
		for _, branch := range openBranches {
			if strings.Contains(branch, fpShort) {
				prsSkipped.WithLabelValues(skipExistingPR).Inc()
				slog.Info("skipping error with existing PR", "pod", event.PodName, "branch", branch)
				return
			}
		}
	}

	// Gather K8s context
	if err := p.watcher.GatherContext(ctx, &event); err != nil {
		slog.Error("failed to gather K8s context", "pod", event.PodName, "error", err)
	}

	// Skip if rolled back
	if event.K8sContext != nil && event.K8sContext.RolledBack {
		prsSkipped.WithLabelValues(skipRollback).Inc()
		slog.Info("skipping error from rolled-back deployment",
			"pod", event.PodName, "deploy", event.K8sContext.DeployName)
		return
	}

	// Heuristic triage: skip obvious operational issues before spawning the agent
	if p.triage.IsOperational(event) {
		errorsClassified.WithLabelValues(classOperational).Inc()
		slog.Info("skipping operational issue (triage)", "pod", event.PodName)
		return
	}

	// Rate limit check (pre-check only; slot recorded after PR confirmed)
	if !p.canPR() {
		prsSkipped.WithLabelValues(skipRateLimit).Inc()
		slog.Warn("rate limit reached, skipping", "pod", event.PodName)
		return
	}

	// Spawn agent to investigate and fix
	result, err := p.agent.Run(ctx, event)
	if err != nil {
		slog.Error("agent failed", "pod", event.PodName, "error", err)
		return
	}

	// Classify result
	switch {
	case result.PRURL != "":
		p.recordPR()
		errorsClassified.WithLabelValues(classCodeBug).Inc()
		prsOpened.Inc()
		slog.Info("agent opened PR", "pod", event.PodName, "pr_url", result.PRURL)
	case result.IssueURL != "":
		errorsClassified.WithLabelValues(classOperational).Inc()
		issuesOpened.Inc()
		slog.Info("agent opened issue", "pod", event.PodName, "issue_url", result.IssueURL)
	case result.IsCodeBug:
		errorsClassified.WithLabelValues(classCodeBug).Inc()
		slog.Warn("agent found code bug but no PR created", "pod", event.PodName)
	default:
		errorsClassified.WithLabelValues(classOperational).Inc()
	}
}

// canPR checks whether the rate limit allows another PR (does not consume a slot).
func (p *Pipeline) canPR() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Hour)

	// Remove expired entries
	valid := p.prTimes[:0]
	for _, t := range p.prTimes {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	p.prTimes = valid

	return len(p.prTimes) < p.cfg.MaxPRsPerHour
}

// recordPR records a PR creation against the rate limit.
func (p *Pipeline) recordPR() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prTimes = append(p.prTimes, time.Now())
}
