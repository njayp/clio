package main

import (
	"sync"
	"time"
)

// Batcher groups consecutive error lines from the same pod/container into
// a single ErrorEvent. It flushes when the batch window expires or the
// source (pod+container) changes.
type Batcher struct {
	window time.Duration
	out    chan ErrorEvent
	mu     sync.Mutex
	timers map[string]*batchEntry
}

type batchEntry struct {
	event ErrorEvent
	timer *time.Timer
}

func batchKey(e ErrorEvent) string {
	return e.Namespace + "/" + e.PodName + "/" + e.Container
}

// NewBatcher creates a Batcher that flushes after the given window.
func NewBatcher(window time.Duration) *Batcher {
	return &Batcher{
		window: window,
		out:    make(chan ErrorEvent, 100),
		timers: make(map[string]*batchEntry),
	}
}

// Add appends a single-line ErrorEvent to the current batch for its pod/container.
// The batched event is flushed after the window expires with no new lines.
func (b *Batcher) Add(e ErrorEvent) {
	key := batchKey(e)
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, exists := b.timers[key]
	if exists {
		entry.timer.Stop()
		entry.event.LogLines = append(entry.event.LogLines, e.LogLines...)
	} else {
		entry = &batchEntry{event: e}
		b.timers[key] = entry
	}

	entry.timer = time.AfterFunc(b.window, func() {
		b.mu.Lock()
		flushed := entry.event
		delete(b.timers, key)
		b.mu.Unlock()
		b.out <- flushed
	})
}

// Events returns the channel of batched error events.
func (b *Batcher) Events() <-chan ErrorEvent {
	return b.out
}

// Flush forces all pending batches to be emitted immediately.
func (b *Batcher) Flush() {
	b.mu.Lock()
	entries := make([]*batchEntry, 0, len(b.timers))
	for k, e := range b.timers {
		e.timer.Stop()
		entries = append(entries, e)
		delete(b.timers, k)
	}
	b.mu.Unlock()

	for _, e := range entries {
		b.out <- e.event
	}
}
