package triage

import (
	"testing"

	"github.com/njayp/clio"
)

func TestTriager_IsOperational(t *testing.T) {
	triager := NewTriager()

	tests := []struct {
		name       string
		event      clio.ErrorEvent
		wantOperat bool
	}{
		{
			name:       "OOMKilled in logs",
			event:      clio.ErrorEvent{LogLines: []string{"container killed: OOMKilled"}},
			wantOperat: true,
		},
		{
			name:       "ImagePullBackOff in K8s events",
			event:      clio.ErrorEvent{LogLines: []string{"error starting"}, K8sContext: &clio.K8sContext{Events: []string{"ImagePullBackOff"}}},
			wantOperat: true,
		},
		{
			name:       "ErrImagePull",
			event:      clio.ErrorEvent{LogLines: []string{"ErrImagePull: registry.example.com/app:v1"}},
			wantOperat: true,
		},
		{
			name:       "FailedScheduling",
			event:      clio.ErrorEvent{LogLines: []string{"FailedScheduling: insufficient resources"}},
			wantOperat: true,
		},
		{
			name:       "Insufficient cpu",
			event:      clio.ErrorEvent{LogLines: []string{"Insufficient cpu to schedule pod"}},
			wantOperat: true,
		},
		{
			name:       "DNS failure",
			event:      clio.ErrorEvent{LogLines: []string{"dns lookup db.svc.cluster.local: no such host"}},
			wantOperat: true,
		},
		{
			name:       "TLS certificate error",
			event:      clio.ErrorEvent{LogLines: []string{"x509: certificate signed by unknown authority"}},
			wantOperat: true,
		},
		{
			name:       "connection refused",
			event:      clio.ErrorEvent{LogLines: []string{"dial tcp 10.0.0.1:5432: connection refused"}},
			wantOperat: true,
		},
		{
			name:       "i/o timeout",
			event:      clio.ErrorEvent{LogLines: []string{"dial tcp 10.0.0.1:443: i/o timeout"}},
			wantOperat: true,
		},
		{
			name:       "nil pointer dereference is a code bug",
			event:      clio.ErrorEvent{LogLines: []string{"panic: runtime error: invalid memory address or nil pointer dereference"}},
			wantOperat: false,
		},
		{
			name:       "unhandled exception is a code bug",
			event:      clio.ErrorEvent{LogLines: []string{"ERROR: unhandled exception in request handler"}},
			wantOperat: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := triager.IsOperational(tt.event)
			if got != tt.wantOperat {
				t.Errorf("IsOperational() = %v, want %v", got, tt.wantOperat)
			}
		})
	}
}
