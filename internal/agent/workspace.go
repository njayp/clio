package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Workspace manages a cached bare clone and git worktrees for concurrent fix attempts.
type Workspace struct {
	repo    string // "owner/repo"
	token   string
	baseDir string // e.g. "/tmp/clio-workspaces"
	bareDir string // baseDir/repo.git

	mu sync.Mutex // protects EnsureClone against concurrent calls
}

// NewWorkspace configures a workspace manager.
func NewWorkspace(repo, token, baseDir string) *Workspace {
	safe := strings.ReplaceAll(repo, "/", "-")
	return &Workspace{
		repo:    repo,
		token:   token,
		baseDir: baseDir,
		bareDir: filepath.Join(baseDir, safe+".git"),
	}
}

func (w *Workspace) repoURL() string {
	return fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", w.token, w.repo)
}

// EnsureClone creates a bare clone on first call, fetches updates on subsequent calls.
func (w *Workspace) EnsureClone(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := os.MkdirAll(w.baseDir, 0o755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}

	if _, err := os.Stat(filepath.Join(w.bareDir, "HEAD")); os.IsNotExist(err) {
		slog.Info("cloning repo", "repo", w.repo)
		cmd := exec.CommandContext(ctx, "git", "clone", "--bare", w.repoURL(), w.bareDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git clone --bare: %w\n%s", err, out)
		}
		// Warm the module cache once after initial clone
		w.downloadModules(ctx)
	} else {
		cmd := exec.CommandContext(ctx, "git", "fetch", "--all")
		cmd.Dir = w.bareDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git fetch: %w\n%s", err, out)
		}
	}
	return nil
}

// downloadModules warms the Go module cache from the bare clone.
func (w *Workspace) downloadModules(ctx context.Context) {
	// Create a temporary worktree just for module download
	tmpDir := filepath.Join(w.baseDir, "mod-cache-init")
	add := exec.CommandContext(ctx, "git", "worktree", "add", tmpDir, "HEAD")
	add.Dir = w.bareDir
	if out, err := add.CombinedOutput(); err != nil {
		slog.Warn("failed to create temp worktree for mod download", "error", err, "output", string(out))
		return
	}
	defer func() {
		rm := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", tmpDir)
		rm.Dir = w.bareDir
		_ = rm.Run()
	}()

	dl := exec.CommandContext(ctx, "go", "mod", "download")
	dl.Dir = tmpDir
	if out, err := dl.CombinedOutput(); err != nil {
		slog.Warn("go mod download failed", "error", err, "output", string(out))
	}
}

// CreateWorktree creates an isolated worktree checked out from HEAD.
func (w *Workspace) CreateWorktree(ctx context.Context, branch string) (string, error) {
	wtDir := filepath.Join(w.baseDir, "wt-"+branch)
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branch, wtDir, "HEAD")
	cmd.Dir = w.bareDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %w\n%s", err, out)
	}
	return wtDir, nil
}

// StageAndDiff stages all changes (excluding RESULT.json) and returns the staged diff.
func (w *Workspace) StageAndDiff(ctx context.Context, wtDir string) (string, error) {
	// Stage all changes
	add := exec.CommandContext(ctx, "git", "add", "-A")
	add.Dir = wtDir
	if out, err := add.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add: %w\n%s", err, out)
	}

	// Unstage RESULT.json so it's not committed or in the diff
	reset := exec.CommandContext(ctx, "git", "reset", "HEAD", "RESULT.json")
	reset.Dir = wtDir
	_ = reset.Run() // ignore error if RESULT.json wasn't staged

	// Get the staged diff (clean, excludes RESULT.json)
	cmd := exec.CommandContext(ctx, "git", "diff", "--staged")
	cmd.Dir = wtDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff --staged: %w", err)
	}
	return string(out), nil
}

// CommitAndPush commits already-staged changes and pushes.
func (w *Workspace) CommitAndPush(ctx context.Context, wtDir, branch, message string) error {
	// Commit
	commit := exec.CommandContext(ctx, "git",
		"-c", "user.name=Clio",
		"-c", "user.email=clio@noreply.github.com",
		"commit", "-m", message)
	commit.Dir = wtDir
	if out, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, out)
	}

	// Push
	push := exec.CommandContext(ctx, "git", "push", w.repoURL(), branch)
	push.Dir = wtDir
	if out, err := push.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %w\n%s", err, out)
	}
	return nil
}

// RemoveWorktree cleans up a worktree directory.
func (w *Workspace) RemoveWorktree(ctx context.Context, wtDir string) {
	rm := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", wtDir)
	rm.Dir = w.bareDir
	if err := rm.Run(); err != nil {
		slog.Warn("failed to remove worktree", "dir", wtDir, "error", err)
	}
}
