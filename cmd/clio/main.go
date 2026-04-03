package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/njayp/clio"
	"github.com/njayp/clio/internal/claude"
	ghclient "github.com/njayp/clio/internal/github"
	"github.com/njayp/clio/internal/k8s"
	"github.com/njayp/clio/internal/pipeline"
	"github.com/njayp/clio/internal/server"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := clio.LoadConfig()
	if err != nil {
		slog.Error("config error", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer stop()

	// Kubernetes client
	k8sCfg, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("failed to get in-cluster config", "error", err)
		os.Exit(1)
	}
	k8sClient, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		slog.Error("failed to create k8s client", "error", err)
		os.Exit(1)
	}

	// Anthropic client
	anthropicClient := anthropic.NewClient(
		option.WithAPIKey(cfg.AnthropicKey),
	)

	// Wire dependencies
	watcher := k8s.NewWatcher(k8sClient, cfg)
	classifier := claude.NewClassifier(anthropicClient, cfg.Model)
	fixer := claude.NewFixer(anthropicClient, cfg.Model)
	gh := ghclient.NewClient(cfg.GitHubToken)

	p := pipeline.NewPipeline(watcher, classifier, fixer, gh, cfg)

	// HTTP server
	srv := server.NewServer(cfg.Port)

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			slog.Error("http server error", "error", err)
		}
	}()

	slog.Info("clio starting",
		"repo", cfg.Repo,
		"release", cfg.Release,
		"namespace", cfg.Namespace,
		"dry_run", cfg.DryRun,
	)

	srv.SetHealthy(true)

	if err := p.Run(ctx); err != nil {
		slog.Error("pipeline error", "error", err)
		os.Exit(1)
	}
}
