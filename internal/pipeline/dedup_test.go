package pipeline

import (
	"testing"
	"time"

	"github.com/njayp/clio"
)

func TestDedup_SuppressesDuplicateWithinCooldown(t *testing.T) {
	d := NewDedup(time.Hour)
	event := clio.ErrorEvent{
		PodName:   "app-1",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"ERROR something broke"},
	}
	fp := clio.Fingerprint(event)

	if d.IsDuplicate(fp) {
		t.Error("first event should not be duplicate")
	}
	if !d.IsDuplicate(fp) {
		t.Error("second event should be duplicate")
	}
}

func TestDedup_AllowsAfterCooldown(t *testing.T) {
	d := NewDedup(10 * time.Millisecond)
	event := clio.ErrorEvent{
		PodName:   "app-1",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"ERROR something broke"},
	}
	fp := clio.Fingerprint(event)

	if d.IsDuplicate(fp) {
		t.Error("first event should not be duplicate")
	}

	time.Sleep(20 * time.Millisecond)

	if d.IsDuplicate(fp) {
		t.Error("event after cooldown should not be duplicate")
	}
}

func TestDedup_DifferentEventsNotDuplicate(t *testing.T) {
	d := NewDedup(time.Hour)
	e1 := clio.ErrorEvent{PodName: "app-1", Container: "web", Repo: "owner/repo", LogLines: []string{"ERROR a"}}
	e2 := clio.ErrorEvent{PodName: "app-1", Container: "web", Repo: "owner/repo", LogLines: []string{"ERROR b"}}

	if d.IsDuplicate(clio.Fingerprint(e1)) {
		t.Error("e1 should not be duplicate")
	}
	if d.IsDuplicate(clio.Fingerprint(e2)) {
		t.Error("e2 should not be duplicate (different content)")
	}
}

func TestCleanup_RemovesExpired(t *testing.T) {
	d := NewDedup(10 * time.Millisecond)
	event := clio.ErrorEvent{Container: "web", Repo: "owner/repo", LogLines: []string{"ERROR"}}

	d.IsDuplicate(clio.Fingerprint(event))
	time.Sleep(20 * time.Millisecond)
	d.Cleanup()

	d.mu.Lock()
	count := len(d.seen)
	d.mu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 entries after cleanup, got %d", count)
	}
}
