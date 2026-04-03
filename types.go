package main

import (
	"context"
	"time"
)

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

// Classification is Claude's assessment of an error event.
type Classification struct {
	IsCodeBug     bool
	Summary       string
	Confidence    float64 // 0-1
	RelevantFiles []string
}

// Fix is a proposed code change to address an error.
type Fix struct {
	Branch      string
	Title       string
	Body        string
	FileChanges map[string]string // filepath → new content
	RawLogs     string
	Reasoning   string
}

// Classifier classifies error events using Claude.
type Classifier interface {
	Classify(ctx context.Context, event ErrorEvent) (Classification, error)
}

// Fixer generates code fixes using Claude.
type Fixer interface {
	GenerateFix(ctx context.Context, event ErrorEvent, class Classification, files map[string]string) (Fix, error)
}

// GitHubClient interacts with the GitHub API for file fetching and PR management.
type GitHubClient interface {
	FetchFiles(ctx context.Context, repo string, paths []string) (map[string]string, error)
	CreatePR(ctx context.Context, repo string, fix Fix) (prURL string, err error)
	CommentOnPR(ctx context.Context, repo string, prURL string, body string) error
	ListOpenClioPRs(ctx context.Context, repo string) ([]string, error) // returns open PR branch names with clio/ prefix
}

// PodWatcher discovers pods and streams their logs.
type PodWatcher interface {
	Watch(ctx context.Context) (<-chan ErrorEvent, error)
	GatherContext(ctx context.Context, event *ErrorEvent) error
}
