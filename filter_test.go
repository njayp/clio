package main

import "testing"

func TestIsErrorLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		// Error patterns
		{"2024-01-15 ERROR failed to connect to database", true},
		{"FATAL: could not open configuration file", true},
		{"panic: runtime error: index out of range", true},
		{"runtime error: invalid memory address", true},
		{"Traceback (most recent call last)", true},
		{"Exception: connection refused", true},
		{"exit status 1", true},
		{"OOMKilled", true},
		{"CrashLoopBackOff", true},
		{"received SIGABRT", true},
		{"received SIGSEGV", true},

		// Stack trace continuations
		{"\tat com.example.Main.run(Main.java:42)", true},
		{"goroutine 1 [running]:", true},
		{"\t/app/main.go:42 +0x1a2", true},
		{"  File \"/app/main.py\", line 42, in <module>", true},
		{"    at Object.<anonymous> (/app/index.js:42:5)", true},

		// Non-error lines
		{"2024-01-15 INFO server started on :8080", false},
		{"2024-01-15 DEBUG processing request id=abc", false},
		{"WARN: deprecated config key", false},
		{"healthy", false},
		{"", false},
		{"GET /api/users 200 OK", false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got := IsErrorLine(tt.line)
			if got != tt.want {
				t.Errorf("IsErrorLine(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}
