package clio

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"
)

// BranchPrefix is the prefix for all clio-created branches.
const BranchPrefix = "clio/"

// ErrorEvent represents a batch of related error log lines from a pod.
type ErrorEvent struct {
	PodName    string
	Namespace  string
	Container  string
	Repo       string // "owner/repo" from config
	LogLines   []string
	Timestamp  time.Time
	K8sContext *K8sContext
}

// K8sContext contains Kubernetes context gathered for an error event.
type K8sContext struct {
	Events       []string // recent K8s events for the pod
	DeployName   string
	ImageTag     string
	PrevImageTag string // previous revision's image tag (empty if first deploy)
	RolledBack   bool   // true if current revision < max observed revision
	Replicas     int
	ConfigMaps   []string
}

// AgentResult is the JSON output the Claude Code agent writes to RESULT.json.
type AgentResult struct {
	IsCodeBug bool   `json:"is_code_bug"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Reasoning string `json:"reasoning"`
}

// Fix is a proposed code change to address an error.
type Fix struct {
	Branch    string
	Title     string
	Body      string
	DiffPatch string // unified diff from worktree
	RawLogs   string
	Reasoning string
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
	return fmt.Sprintf("%s%s-%s", BranchPrefix, slug, fp[:8])
}
