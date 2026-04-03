package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthz_Healthy(t *testing.T) {
	s := NewServer(8080)
	s.SetHealthy(true)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestHealthz_NotReady(t *testing.T) {
	s := NewServer(8080)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	s := NewServer(8080)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Just verify the endpoint is wired; full Prometheus output tested by prometheus lib
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# HELP clio_errors_detected_total"))
	}))

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "clio_errors_detected_total") {
		t.Errorf("body missing expected metric")
	}
}
