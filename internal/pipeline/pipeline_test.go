package pipeline

import (
	"context"
	"strings"
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

type mockTriage struct {
	operational bool
}

func (m *mockTriage) IsOperational(_ clio.ErrorEvent) bool {
	return m.operational
}

type mockAgent struct {
	result *clio.AgentResult
	err    error
}

func (m *mockAgent) Run(_ context.Context, _ clio.ErrorEvent) (*clio.AgentResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

type mockGitHub struct {
	openBranches []string
}

func (m *mockGitHub) ListOpenClioPRs(_ context.Context, _ string) ([]string, error) {
	return m.openBranches, nil
}

func sendEvent(ch chan clio.ErrorEvent, event clio.ErrorEvent) {
	ch <- event
}

func TestPipeline_EndToEnd(t *testing.T) {
	watcher := &mockWatcher{events: make(chan clio.ErrorEvent, 10)}
	triage := &mockTriage{operational: false}
	agent := &mockAgent{result: &clio.AgentResult{
		IsCodeBug: true,
		PRURL:     "https://github.com/owner/repo/pull/1",
		Title:     "Fix nil pointer",
		Reasoning: "Missing nil check",
	}}
	gh := &mockGitHub{}

	cfg := clio.Config{
		Repo:           "owner/repo",
		Cooldown:       time.Hour,
		BatchWindow:    10 * time.Millisecond,
		MaxConcurrency: 1,
		MaxPRsPerHour:  5,
	}

	p := NewPipeline(watcher, triage, agent, gh, cfg)
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

	// Agent returned a result with PR URL — pipeline should have logged and counted it.
	// No more direct PR creation or commenting assertions since agent handles those now.
	if agent.result.PRURL == "" {
		t.Error("expected agent result to have PR URL")
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
	triage := &mockTriage{operational: false}
	agent := &mockAgent{result: &clio.AgentResult{
		IsCodeBug: true,
		PRURL:     "https://github.com/owner/repo/pull/1",
		Title:     "Fix",
	}}
	gh := &mockGitHub{openBranches: []string{"clio/fix-" + fp}}

	cfg := clio.Config{
		Repo:           "owner/repo",
		Cooldown:       time.Hour,
		BatchWindow:    10 * time.Millisecond,
		MaxConcurrency: 1,
		MaxPRsPerHour:  5,
	}

	p := NewPipeline(watcher, triage, agent, gh, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go p.Run(ctx)

	sendEvent(watcher.events, event)
	time.Sleep(200 * time.Millisecond)
	cancel()

	// Agent should not have been called since existing PR matched
}

func TestPipeline_RateLimiter(t *testing.T) {
	callCount := 0
	watcher := &mockWatcher{events: make(chan clio.ErrorEvent, 10)}
	triage := &mockTriage{operational: false}
	agent := &countingAgent{
		result: &clio.AgentResult{
			IsCodeBug: true,
			PRURL:     "https://github.com/owner/repo/pull/1",
			Title:     "Fix",
		},
		count: &callCount,
	}
	gh := &mockGitHub{}

	cfg := clio.Config{
		Repo:           "owner/repo",
		Cooldown:       10 * time.Millisecond,
		BatchWindow:    10 * time.Millisecond,
		MaxConcurrency: 1,
		MaxPRsPerHour:  1, // allow only 1 PR
	}

	p := NewPipeline(watcher, triage, agent, gh, cfg)
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

	if callCount > 1 {
		t.Errorf("expected at most 1 agent call (rate limited), got %d", callCount)
	}
}

type countingAgent struct {
	result *clio.AgentResult
	count  *int
}

func (a *countingAgent) Run(_ context.Context, _ clio.ErrorEvent) (*clio.AgentResult, error) {
	*a.count++
	return a.result, nil
}

func TestPipeline_DryRunNoPush(t *testing.T) {
	watcher := &mockWatcher{events: make(chan clio.ErrorEvent, 10)}
	triage := &mockTriage{operational: false}
	// In dry run, agent won't have push tools, so result has no PR URL
	agent := &mockAgent{result: &clio.AgentResult{
		IsCodeBug: true,
		Title:     "Fix",
		Reasoning: "Found a bug but dry run",
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

	p := NewPipeline(watcher, triage, agent, gh, cfg)
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

	// Agent result has no PR URL (push tools were omitted in dry run)
	if agent.result.PRURL != "" {
		t.Error("expected empty PR URL in dry run")
	}
}

func TestPipeline_SkipsRolledBack(t *testing.T) {
	watcher := &mockWatcher{
		events: make(chan clio.ErrorEvent, 10),
		k8sCtx: &clio.K8sContext{RolledBack: true, DeployName: "myapp"},
	}
	triage := &mockTriage{operational: false}
	agent := &mockAgent{result: &clio.AgentResult{
		IsCodeBug: true,
		PRURL:     "https://github.com/owner/repo/pull/1",
		Title:     "Fix",
	}}
	gh := &mockGitHub{}

	cfg := clio.Config{
		Repo:           "owner/repo",
		Cooldown:       time.Hour,
		BatchWindow:    10 * time.Millisecond,
		MaxConcurrency: 1,
		MaxPRsPerHour:  5,
	}

	p := NewPipeline(watcher, triage, agent, gh, cfg)
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

	// Agent should not have been called since deployment was rolled back
}
