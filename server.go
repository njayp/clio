package main

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

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
)

func init() {
	prometheus.MustRegister(errorsDetected, errorsClassified, prsOpened, prsSkipped)
}

// Server provides health and metrics HTTP endpoints.
type Server struct {
	port    int
	healthy *atomic.Bool
}

// NewServer creates a new HTTP server for health and metrics.
func NewServer(port int) *Server {
	return &Server{
		port:    port,
		healthy: &atomic.Bool{},
	}
}

// SetHealthy marks the server as healthy (watcher is active).
func (s *Server) SetHealthy(v bool) {
	s.healthy.Store(v)
}

// ListenAndServe starts the HTTP server. Blocks until error or context done.
func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(fmt.Sprintf(":%d", s.port), mux)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if s.healthy.Load() {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready"))
	}
}
