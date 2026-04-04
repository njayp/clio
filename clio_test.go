package clio

import (
	"testing"
)

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
