package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/njayp/clio"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func newTestAnthropicClient(t *testing.T, handler http.HandlerFunc) anthropic.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL),
	)
}

func anthropicResponse(t *testing.T, text string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "msg_test",
			"type":  "message",
			"role":  "assistant",
			"model": "claude-sonnet-4-20250514",
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 10, "output_tokens": 10},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func TestClassifier_CodeBug(t *testing.T) {
	respJSON := `{"is_code_bug": true, "summary": "Nil pointer in auth handler", "confidence": 0.9, "relevant_files": ["pkg/auth/handler.go"]}`

	client := newTestAnthropicClient(t, anthropicResponse(t, respJSON))
	classifier := NewClassifier(client, "claude-sonnet-4-20250514")

	event := clio.ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"panic: runtime error: nil pointer dereference", "\t/app/pkg/auth/handler.go:42"},
	}

	class, err := classifier.Classify(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !class.IsCodeBug {
		t.Error("expected IsCodeBug=true")
	}
	if class.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9", class.Confidence)
	}
	if len(class.RelevantFiles) != 1 || class.RelevantFiles[0] != "pkg/auth/handler.go" {
		t.Errorf("RelevantFiles = %v", class.RelevantFiles)
	}
}

func TestClassifier_OperationalIssue(t *testing.T) {
	respJSON := `{"is_code_bug": false, "summary": "Pod killed due to memory limits", "confidence": 0.95, "relevant_files": []}`

	client := newTestAnthropicClient(t, anthropicResponse(t, respJSON))
	classifier := NewClassifier(client, "claude-sonnet-4-20250514")

	event := clio.ErrorEvent{
		PodName:   "app-1",
		Namespace: "staging",
		Container: "web",
		Repo:      "owner/repo",
		LogLines:  []string{"OOMKilled"},
		K8sContext: &clio.K8sContext{
			Events: []string{"OOMKilled"},
		},
	}

	class, err := classifier.Classify(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if class.IsCodeBug {
		t.Error("expected IsCodeBug=false for OOM")
	}
}

func TestClassifier_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": {"type": "server_error", "message": "internal error"}}`))
	}))
	defer server.Close()

	client := anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(server.URL),
	)
	classifier := NewClassifier(client, "claude-sonnet-4-20250514")

	_, err := classifier.Classify(context.Background(), clio.ErrorEvent{LogLines: []string{"ERROR"}})
	if err == nil {
		t.Error("expected error from API failure")
	}
}
