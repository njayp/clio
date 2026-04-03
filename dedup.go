package main

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Dedup suppresses duplicate error events within a cooldown window
// using a fingerprint based on pod container, repo, and log content.
type Dedup struct {
	cooldown time.Duration
	mu       sync.Mutex
	seen     map[string]time.Time
}

// NewDedup creates a Dedup with the given cooldown duration.
// It starts a background goroutine to periodically clean up expired entries.
func NewDedup(cooldown time.Duration) *Dedup {
	d := &Dedup{
		cooldown: cooldown,
		seen:     make(map[string]time.Time),
	}
	go func() {
		ticker := time.NewTicker(cooldown)
		defer ticker.Stop()
		for range ticker.C {
			d.Cleanup()
		}
	}()
	return d
}

// Fingerprint generates a stable hash for an error event.
// It normalizes by sorting log lines and hashing the container + content.
func Fingerprint(e ErrorEvent) string {
	lines := make([]string, len(e.LogLines))
	copy(lines, e.LogLines)
	sort.Strings(lines)

	h := sha256.New()
	fmt.Fprintf(h, "%s/%s\n", e.Repo, e.Container)
	for _, l := range lines {
		fmt.Fprintln(h, l)
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// IsDuplicate returns true if this fingerprint was seen within the cooldown window.
// If not a duplicate, it records the fingerprint.
func (d *Dedup) IsDuplicate(fp string) bool {
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	if lastSeen, ok := d.seen[fp]; ok {
		if now.Sub(lastSeen) < d.cooldown {
			return true
		}
	}
	d.seen[fp] = now
	return false
}

// Cleanup removes expired entries. Call periodically to prevent memory growth.
func (d *Dedup) Cleanup() {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()

	for fp, lastSeen := range d.seen {
		if now.Sub(lastSeen) >= d.cooldown {
			delete(d.seen, fp)
		}
	}
}

// BranchName generates a clio/ prefixed branch name for an error event.
func BranchName(fp string, summary string) string {
	slug := strings.ToLower(summary)
	slug = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, slug)
	// Trim leading/trailing dashes and collapse multiples
	parts := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' })
	slug = strings.Join(parts, "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return fmt.Sprintf("%s%s-%s", branchPrefix, slug, fp[:8])
}
