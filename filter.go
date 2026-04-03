package main

import "strings"

// errorPatterns are substrings that indicate an error log line.
var errorPatterns = []string{
	"ERROR",
	"FATAL",
	"panic:",
	"runtime error:",
	"Traceback (most recent call last)",
	"Exception:",
	"SIGABRT",
	"SIGSEGV",
	"exit status",
	"OOMKilled",
	"CrashLoopBackOff",
}

// stackTracePatterns indicate a line is part of a stack trace continuation.
var stackTracePatterns = []string{
	"\tat ",        // Java stack trace
	"goroutine ",   // Go goroutine header
	"\t/",          // Go stack trace file paths
	"  File \"",    // Python traceback
	"    at ",      // Node.js/JS stack trace
	"    raise ",   // Python raise
	"    return ",  // Python traceback context
}

// IsErrorLine returns true if a log line looks like an error or part of a stack trace.
func IsErrorLine(line string) bool {
	for _, p := range errorPatterns {
		if strings.Contains(line, p) {
			return true
		}
	}
	for _, p := range stackTracePatterns {
		if strings.Contains(line, p) {
			return true
		}
	}
	return false
}
