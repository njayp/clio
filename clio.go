package clio

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"sort"
	"time"
)

//go:embed all:.claude
var ClaudeConfig embed.FS

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
	PRURL     string `json:"pr_url,omitempty"`
	IssueURL  string `json:"issue_url,omitempty"`
	Title     string `json:"title"`
	Reasoning string `json:"reasoning"`
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
