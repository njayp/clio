package main

import (
	"testing"
	"time"
)

func TestBatcher_GroupsConsecutiveLines(t *testing.T) {
	b := NewBatcher(50 * time.Millisecond)

	b.Add(ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		LogLines:  []string{"panic: nil pointer dereference"},
	})
	b.Add(ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		LogLines:  []string{"goroutine 1 [running]:"},
	})
	b.Add(ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		LogLines:  []string{"\t/app/main.go:42 +0x1a2"},
	})

	select {
	case event := <-b.Events():
		if len(event.LogLines) != 3 {
			t.Fatalf("got %d log lines, want 3", len(event.LogLines))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for batched event")
	}
}

func TestBatcher_SeparatesPodContainers(t *testing.T) {
	b := NewBatcher(50 * time.Millisecond)

	b.Add(ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		LogLines:  []string{"ERROR in web"},
	})
	b.Add(ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "worker",
		LogLines:  []string{"ERROR in worker"},
	})

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case event := <-b.Events():
			got[event.Container] = true
			if len(event.LogLines) != 1 {
				t.Errorf("container %s: got %d lines, want 1", event.Container, len(event.LogLines))
			}
		case <-time.After(time.Second):
			t.Fatal("timed out")
		}
	}
	if !got["web"] || !got["worker"] {
		t.Errorf("expected events from both containers, got %v", got)
	}
}

func TestBatcher_FlushEmitsPending(t *testing.T) {
	b := NewBatcher(time.Hour) // very long window

	b.Add(ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		LogLines:  []string{"ERROR something"},
	})

	b.Flush()

	select {
	case event := <-b.Events():
		if len(event.LogLines) != 1 {
			t.Fatalf("got %d lines, want 1", len(event.LogLines))
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("flush did not emit event")
	}
}

func TestBatcher_SingleLinePassesThrough(t *testing.T) {
	b := NewBatcher(50 * time.Millisecond)

	b.Add(ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		LogLines:  []string{"FATAL shutdown"},
	})

	select {
	case event := <-b.Events():
		if len(event.LogLines) != 1 {
			t.Fatalf("got %d lines, want 1", len(event.LogLines))
		}
		if event.LogLines[0] != "FATAL shutdown" {
			t.Errorf("got %q", event.LogLines[0])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}
