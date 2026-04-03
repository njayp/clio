package pipeline

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/njayp/clio"
)

// Mock implementations for testing

type mockWatcher struct {
	events chan clio.ErrorEvent
	k8sCtx *clio.K8sContext
}

func (m *mockWatcher) Watch(ctx context.Context) (<-chan clio.ErrorEvent, error) {
	return m.events, nil
}

func (m *mockWatcher) GatherContext(_ context.Context, event *clio.ErrorEvent) error {
	if m.k8sCtx != nil {
		event.K8sContext = m.k8sCtx
	}
	return nil
}

type mockClassifier struct {
	result clio.Classification
	err    error
}

func (m *mockClassifier) Classify(_ context.Context, _ clio.ErrorEvent) (clio.Classification, error) {
	return m.result, m.err
}

type mockFixer struct {
	result clio.Fix
	err    error
}

func (m *mockFixer) GenerateFix(_ context.Context, event clio.ErrorEvent, class clio.Classification, _ map[string]string) (clio.Fix, error) {
	if m.err != nil {
		return clio.Fix{}, m.err
	}
	fix := m.result
	if fix.Branch == "" {
		fix.Branch = clio.BranchName(clio.Fingerprint(event), class.Summary)
	}
	return fix, nil
}

type mockGitHub struct {
	mu           sync.Mutex
	prsCreated   []clio.Fix
	comments     []string
	openBranches []string
	files        map[string]string
}

func (m *mockGitHub) FetchFiles(_ context.Context, _ string, _ []string) (map[string]string, error) {
	if m.files != nil {
		return m.files, nil
	}
	return map[string]string{"main.go": "package main"}, nil
}

func (m *mockGitHub) CreatePR(_ context.Context, _ string, fix clio.Fix) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prsCreated = append(m.prsCreated, fix)
	return "https://github.com/owner/repo/pull/1", nil
}

func (m *mockGitHub) CommentOnPR(_ context.Context, _ string, _ string, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comments = append(m.comments, body)
	return nil
}

func (m *mockGitHub) ListOpenClioPRs(_ context.Context, _ string) ([]string, error) {
	return m.openBranches, nil
}

func sendEvent(ch chan clio.ErrorEvent, event clio.ErrorEvent) {
	ch <- event
}

func TestPipeline_EndToEnd(t *testing.T) {
	watcher := &mockWatcher{events: make(chan clio.ErrorEvent, 10)}
	classifier := &mockClassifier{result: clio.Classification{
		IsCodeBug:     true,
		Summary:       "nil pointer in handler",
		Confidence:    0.9,
		RelevantFiles: []string{"main.go"},
	}}
	fixer := &mockFixer{result: clio.Fix{
		Title:       "Fix nil pointer",
		Body:        "Adds nil check",
		FileChanges: map[string]string{"main.go": "package main\n// fixed"},
		Reasoning:   "Missing nil check",
	}}
	gh := &mockGitHub{}

	cfg := clio.Config{
		Repo:           "owner/repo",
		Cooldown:       time.Hour,
		BatchWindow:    10 * time.Millisecond,
		MaxConcurrency: 1,
		MaxPRsPerHour:  5,
	}

	p := NewPipeline(watcher, classifier, fixer, gh, cfg)
	ctx, cancel := context.WithCancel(context.Background())

	go p.Run(ctx)

	sendEvent(watcher.events, clio.ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"panic: nil pointer dereference"},
		Timestamp: time.Now(),
	})

	// Wait for processing
	time.Sleep(200 * time.Millisecond)
	cancel()

	gh.mu.Lock()
	defer gh.mu.Unlock()
	if len(gh.prsCreated) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(gh.prsCreated))
	}
	if gh.prsCreated[0].Title != "Fix nil pointer" {
		t.Errorf("PR title = %q", gh.prsCreated[0].Title)
	}
	if len(gh.comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(gh.comments))
	}
	if !strings.Contains(gh.comments[0], "Confidence") {
		t.Error("comment should contain confidence")
	}
}

func TestPipeline_SkipsExistingPR(t *testing.T) {
	event := clio.ErrorEvent{
		PodName:   "app-1",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"ERROR something"},
	}
	fp := clio.Fingerprint(event)[:8]

	watcher := &mockWatcher{events: make(chan clio.ErrorEvent, 10)}
	classifier := &mockClassifier{result: clio.Classification{IsCodeBug: true, Summary: "bug"}}
	fixer := &mockFixer{result: clio.Fix{Title: "Fix", FileChanges: map[string]string{"a.go": "fixed"}}}
	gh := &mockGitHub{openBranches: []string{"clio/fix-" + fp}}

	cfg := clio.Config{
		Repo:           "owner/repo",
		Cooldown:       time.Hour,
		BatchWindow:    10 * time.Millisecond,
		MaxConcurrency: 1,
		MaxPRsPerHour:  5,
	}

	p := NewPipeline(watcher, classifier, fixer, gh, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)

	sendEvent(watcher.events, event)
	time.Sleep(200 * time.Millisecond)
	cancel()

	gh.mu.Lock()
	defer gh.mu.Unlock()
	if len(gh.prsCreated) != 0 {
		t.Errorf("expected 0 PRs (existing PR match), got %d", len(gh.prsCreated))
	}
}

func TestPipeline_RateLimiter(t *testing.T) {
	watcher := &mockWatcher{events: make(chan clio.ErrorEvent, 10)}
	classifier := &mockClassifier{result: clio.Classification{
		IsCodeBug: true, Summary: "bug", RelevantFiles: []string{"a.go"},
	}}
	fixer := &mockFixer{result: clio.Fix{
		Title: "Fix", FileChanges: map[string]string{"a.go": "fixed"},
	}}
	gh := &mockGitHub{}

	cfg := clio.Config{
		Repo:           "owner/repo",
		Cooldown:       10 * time.Millisecond, // short cooldown so dedup doesn't interfere
		BatchWindow:    10 * time.Millisecond,
		MaxConcurrency: 1,
		MaxPRsPerHour:  1, // allow only 1 PR
	}

	p := NewPipeline(watcher, classifier, fixer, gh, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)

	// Send two different events
	for i := 0; i < 2; i++ {
		time.Sleep(20 * time.Millisecond) // exceed cooldown
		sendEvent(watcher.events, clio.ErrorEvent{
			PodName:   "app-1",
			Container: "web",
			Repo:      "owner/repo",
			LogLines:  []string{strings.Repeat("ERROR ", i+1)},
			Timestamp: time.Now(),
		})
		time.Sleep(100 * time.Millisecond) // wait for processing
	}

	cancel()

	gh.mu.Lock()
	defer gh.mu.Unlock()
	if len(gh.prsCreated) > 1 {
		t.Errorf("expected at most 1 PR (rate limited), got %d", len(gh.prsCreated))
	}
}

func TestPipeline_DryRunSkipsPR(t *testing.T) {
	watcher := &mockWatcher{events: make(chan clio.ErrorEvent, 10)}
	classifier := &mockClassifier{result: clio.Classification{
		IsCodeBug: true, Summary: "bug", RelevantFiles: []string{"a.go"},
	}}
	fixer := &mockFixer{result: clio.Fix{
		Title: "Fix", FileChanges: map[string]string{"a.go": "fixed"},
	}}
	gh := &mockGitHub{}

	cfg := clio.Config{
		Repo:           "owner/repo",
		Cooldown:       time.Hour,
		BatchWindow:    10 * time.Millisecond,
		MaxConcurrency: 1,
		MaxPRsPerHour:  5,
		DryRun:         true,
	}

	p := NewPipeline(watcher, classifier, fixer, gh, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)

	sendEvent(watcher.events, clio.ErrorEvent{
		PodName:   "app-1",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"FATAL crash"},
		Timestamp: time.Now(),
	})

	time.Sleep(200 * time.Millisecond)
	cancel()

	gh.mu.Lock()
	defer gh.mu.Unlock()
	if len(gh.prsCreated) != 0 {
		t.Errorf("expected 0 PRs in dry run, got %d", len(gh.prsCreated))
	}
}

func TestPipeline_SkipsRolledBack(t *testing.T) {
	watcher := &mockWatcher{
		events: make(chan clio.ErrorEvent, 10),
		k8sCtx: &clio.K8sContext{RolledBack: true, DeployName: "myapp"},
	}
	classifier := &mockClassifier{result: clio.Classification{IsCodeBug: true, Summary: "bug"}}
	fixer := &mockFixer{result: clio.Fix{Title: "Fix", FileChanges: map[string]string{"a.go": "fixed"}}}
	gh := &mockGitHub{}

	cfg := clio.Config{
		Repo:           "owner/repo",
		Cooldown:       time.Hour,
		BatchWindow:    10 * time.Millisecond,
		MaxConcurrency: 1,
		MaxPRsPerHour:  5,
	}

	p := NewPipeline(watcher, classifier, fixer, gh, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)

	sendEvent(watcher.events, clio.ErrorEvent{
		PodName:   "app-1",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"ERROR from rolled back deploy"},
		Timestamp: time.Now(),
	})

	time.Sleep(200 * time.Millisecond)
	cancel()

	gh.mu.Lock()
	defer gh.mu.Unlock()
	if len(gh.prsCreated) != 0 {
		t.Errorf("expected 0 PRs (rolled back), got %d", len(gh.prsCreated))
	}
}
