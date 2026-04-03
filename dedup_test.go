package main

import (
	"strings"
	"testing"
	"time"
)

func TestDedup_SuppressesDuplicateWithinCooldown(t *testing.T) {
	d := NewDedup(time.Hour)
	event := ErrorEvent{
		PodName:   "app-1",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"ERROR something broke"},
	}
	fp := Fingerprint(event)

	if d.IsDuplicate(fp) {
		t.Error("first event should not be duplicate")
	}
	if !d.IsDuplicate(fp) {
		t.Error("second event should be duplicate")
	}
}

func TestDedup_AllowsAfterCooldown(t *testing.T) {
	d := NewDedup(10 * time.Millisecond)
	event := ErrorEvent{
		PodName:   "app-1",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"ERROR something broke"},
	}
	fp := Fingerprint(event)

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
	e1 := ErrorEvent{PodName: "app-1", Container: "web", Repo: "owner/repo", LogLines: []string{"ERROR a"}}
	e2 := ErrorEvent{PodName: "app-1", Container: "web", Repo: "owner/repo", LogLines: []string{"ERROR b"}}

	if d.IsDuplicate(Fingerprint(e1)) {
		t.Error("e1 should not be duplicate")
	}
	if d.IsDuplicate(Fingerprint(e2)) {
		t.Error("e2 should not be duplicate (different content)")
	}
}

func TestFingerprint_Deterministic(t *testing.T) {
	event := ErrorEvent{
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"ERROR a", "ERROR b"},
	}
	fp1 := Fingerprint(event)
	fp2 := Fingerprint(event)
	if fp1 != fp2 {
		t.Errorf("fingerprints differ: %s vs %s", fp1, fp2)
	}
}

func TestFingerprint_OrderIndependent(t *testing.T) {
	e1 := ErrorEvent{Container: "web", Repo: "owner/repo", LogLines: []string{"ERROR a", "ERROR b"}}
	e2 := ErrorEvent{Container: "web", Repo: "owner/repo", LogLines: []string{"ERROR b", "ERROR a"}}
	if Fingerprint(e1) != Fingerprint(e2) {
		t.Error("fingerprints should be order-independent")
	}
}

func TestBranchName_Format(t *testing.T) {
	event := ErrorEvent{Container: "web", Repo: "owner/repo", LogLines: []string{"ERROR"}}
	fp := Fingerprint(event)
	branch := BranchName(fp, "Fix null pointer in auth handler")

	if !strings.HasPrefix(branch, "clio/") {
		t.Errorf("branch %q should have clio/ prefix", branch)
	}
	if strings.Contains(branch, " ") {
		t.Errorf("branch %q should not contain spaces", branch)
	}
}

func TestCleanup_RemovesExpired(t *testing.T) {
	d := NewDedup(10 * time.Millisecond)
	event := ErrorEvent{Container: "web", Repo: "owner/repo", LogLines: []string{"ERROR"}}

	d.IsDuplicate(Fingerprint(event))
	time.Sleep(20 * time.Millisecond)
	d.Cleanup()

	d.mu.Lock()
	count := len(d.seen)
	d.mu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 entries after cleanup, got %d", count)
	}
}
