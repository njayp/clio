package pipeline

import "github.com/prometheus/client_golang/prometheus"

// Prometheus metrics for observability.
var (
	errorsDetected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "clio_errors_detected_total",
		Help: "Total error events detected from logs",
	})
	errorsClassified = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "clio_errors_classified_total",
		Help: "Total errors classified by type",
	}, []string{"type"})
	prsOpened = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "clio_prs_opened_total",
		Help: "Total PRs opened",
	})
	prsSkipped = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "clio_prs_skipped_total",
		Help: "Total PRs skipped by reason",
	}, []string{"reason"})
	issuesOpened = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "clio_issues_opened_total",
		Help: "Total GitHub issues opened",
	})
)

func init() {
	prometheus.MustRegister(errorsDetected, errorsClassified, prsOpened, prsSkipped, issuesOpened)
}
