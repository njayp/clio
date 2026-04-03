package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Prometheus label constants
const (
	skipDedup      = "dedup"
	skipExistingPR = "existing_pr"
	skipRollback   = "rollback"
	skipRateLimit  = "rate_limit"
	skipDryRun     = "dry_run"

	classCodeBug     = "code_bug"
	classOperational = "operational"
)

// Pipeline orchestrates the full error-to-PR flow.
type Pipeline struct {
	watcher    PodWatcher
	classifier Classifier
	fixer      Fixer
	gh         GitHubClient
	dedup      *Dedup
	batcher    *Batcher
	cfg        Config

	// Rate limiting
	mu         sync.Mutex
	prTimes    []time.Time
}

// NewPipeline creates a pipeline with all dependencies wired.
func NewPipeline(watcher PodWatcher, classifier Classifier, fixer Fixer, gh GitHubClient, cfg Config) *Pipeline {
	return &Pipeline{
		watcher:    watcher,
		classifier: classifier,
		fixer:      fixer,
		gh:         gh,
		dedup:      NewDedup(cfg.Cooldown),
		batcher:    NewBatcher(cfg.BatchWindow),
		cfg:        cfg,
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
			go func(ev ErrorEvent) {
				defer func() { <-sem }()
				p.processEvent(ctx, ev)
			}(event)
		}
	}
}

func (p *Pipeline) processEvent(ctx context.Context, event ErrorEvent) {
	fp := Fingerprint(event)

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

	// Classify
	class, err := p.classifier.Classify(ctx, event)
	if err != nil {
		slog.Error("classification failed", "pod", event.PodName, "error", err)
		return
	}

	if class.IsCodeBug {
		errorsClassified.WithLabelValues(classCodeBug).Inc()
	} else {
		errorsClassified.WithLabelValues(classOperational).Inc()
		slog.Info("skipping operational issue", "pod", event.PodName, "summary", class.Summary)
		return
	}

	// Rate limit check
	if !p.allowPR() {
		prsSkipped.WithLabelValues(skipRateLimit).Inc()
		slog.Warn("rate limit reached, skipping PR", "pod", event.PodName)
		return
	}

	// Fetch relevant files
	files, err := p.gh.FetchFiles(ctx, event.Repo, class.RelevantFiles)
	if err != nil {
		slog.Error("failed to fetch files", "error", err)
		return
	}

	// Generate fix
	fix, err := p.fixer.GenerateFix(ctx, event, class, files)
	if err != nil {
		slog.Error("fix generation failed", "pod", event.PodName, "error", err)
		return
	}

	// Dry run mode
	if p.cfg.DryRun {
		prsSkipped.WithLabelValues(skipDryRun).Inc()
		slog.Info("dry run: would open PR",
			"pod", event.PodName,
			"branch", fix.Branch,
			"title", fix.Title,
			"files_changed", len(fix.FileChanges),
		)
		return
	}

	// Create PR
	prURL, err := p.gh.CreatePR(ctx, event.Repo, fix)
	if err != nil {
		slog.Error("failed to create PR", "pod", event.PodName, "error", err)
		return
	}
	prsOpened.Inc()

	// Comment on PR with context
	comment := formatPRComment(event, class, fix)
	if err := p.gh.CommentOnPR(ctx, event.Repo, prURL, comment); err != nil {
		slog.Error("failed to comment on PR", "url", prURL, "error", err)
	}

	slog.Info("PR opened successfully", "url", prURL, "pod", event.PodName)
}

// allowPR checks and records a PR against the rate limit.
func (p *Pipeline) allowPR() bool {
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

	if len(p.prTimes) >= p.cfg.MaxPRsPerHour {
		return false
	}
	p.prTimes = append(p.prTimes, now)
	return true
}

func formatPRComment(event ErrorEvent, class Classification, fix Fix) string {
	var sb strings.Builder
	sb.WriteString("## Clio Auto-Fix Context\n\n")

	sb.WriteString(fmt.Sprintf("**Confidence:** %.0f%%\n\n", class.Confidence*100))

	sb.WriteString("### Classification Reasoning\n")
	sb.WriteString(fix.Reasoning)
	sb.WriteString("\n\n")

	sb.WriteString("### Raw Logs\n```\n")
	sb.WriteString(fix.RawLogs)
	sb.WriteString("\n```\n")

	if event.K8sContext != nil {
		sb.WriteString("\n### Kubernetes Context\n")
		if event.K8sContext.DeployName != "" {
			sb.WriteString(fmt.Sprintf("- **Deployment:** %s\n", event.K8sContext.DeployName))
		}
		if event.K8sContext.ImageTag != "" {
			sb.WriteString(fmt.Sprintf("- **Image:** %s\n", event.K8sContext.ImageTag))
		}
		if event.K8sContext.PrevImageTag != "" {
			sb.WriteString(fmt.Sprintf("- **Previous Image:** %s\n", event.K8sContext.PrevImageTag))
		}
	}

	return sb.String()
}
