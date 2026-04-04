package triage

import (
	"strings"

	"github.com/njayp/clio"
)

// operationalPatterns are substrings that indicate an infrastructure/operational issue
// rather than a code bug. A K8s event containing e.g. "OOMKilled" will pass the error
// filter in k8s/filter.go (it IS an error) then get triaged here as operational (not a
// code bug). This is correct — the filter detects errors, the triager classifies them.
var operationalPatterns []string

func init() {
	raw := []string{
		"OOMKilled",
		"ImagePullBackOff",
		"ErrImagePull",
		"FailedScheduling",
		"Insufficient cpu",
		"Insufficient memory",
		"dns lookup",
		"no such host",
		"certificate signed by unknown authority",
		"x509:",
		"connection refused",
		"connection timed out",
		"i/o timeout",
	}
	operationalPatterns = make([]string, len(raw))
	for i, p := range raw {
		operationalPatterns[i] = strings.ToLower(p)
	}
}

// Triager classifies errors using lightweight heuristics before invoking the agent.
type Triager struct{}

// NewTriager creates a new Triager.
func NewTriager() *Triager { return &Triager{} }

// IsOperational returns true if the error event is obviously an infrastructure/operational
// issue that does not require agent investigation.
func (t *Triager) IsOperational(event clio.ErrorEvent) bool {
	combined := strings.Join(event.LogLines, "\n")

	// Also check K8s events for operational patterns
	if event.K8sContext != nil {
		combined += "\n" + strings.Join(event.K8sContext.Events, "\n")
	}

	lower := strings.ToLower(combined)
	for _, p := range operationalPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
