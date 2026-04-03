package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/njayp/clio"
	gh "github.com/google/go-github/v72/github"
)

func newTestGitHubClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := gh.NewClient(nil).WithAuthToken("test-token")
	client.BaseURL, _ = client.BaseURL.Parse(server.URL + "/")
	return &Client{client: client}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func TestGitHubClient_FetchFiles(t *testing.T) {
	mux := http.NewServeMux()

	// Mock repo endpoint for default branch
	mux.HandleFunc("/repos/owner/repo", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"default_branch": "main"})
	})

	// Mock contents endpoint
	mux.HandleFunc("/repos/owner/repo/contents/pkg/handler.go", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"type":     "file",
			"encoding": "base64",
			"content":  "cGFja2FnZSBwa2c=", // "package pkg"
		})
	})

	g := newTestGitHubClient(t, mux)
	files, err := g.FetchFiles(context.Background(), "owner/repo", []string{"pkg/handler.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files["pkg/handler.go"] != "package pkg" {
		t.Errorf("content = %q", files["pkg/handler.go"])
	}
}

func TestGitHubClient_FetchFiles_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"default_branch": "main"})
	})
	mux.HandleFunc("/repos/owner/repo/contents/missing.go", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		writeJSON(w, map[string]any{"message": "Not Found"})
	})

	g := newTestGitHubClient(t, mux)
	files, err := g.FetchFiles(context.Background(), "owner/repo", []string{"missing.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestGitHubClient_CreatePR(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"default_branch": "main"})
	})
	mux.HandleFunc("/repos/owner/repo/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"ref":    "refs/heads/main",
			"object": map[string]string{"sha": "abc123", "type": "commit"},
		})
	})
	mux.HandleFunc("/repos/owner/repo/git/commits/abc123", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"sha":  "abc123",
			"tree": map[string]string{"sha": "tree123"},
		})
	})
	mux.HandleFunc("/repos/owner/repo/git/trees", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"sha": "newtree123"})
	})
	mux.HandleFunc("/repos/owner/repo/git/commits", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"sha": "newcommit123"})
	})
	mux.HandleFunc("/repos/owner/repo/git/refs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"ref":    "refs/heads/clio/fix",
			"object": map[string]string{"sha": "newcommit123"},
		})
	})
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			writeJSON(w, map[string]any{
				"html_url": "https://github.com/owner/repo/pull/42",
				"number":   42,
			})
			return
		}
		writeJSON(w, []any{})
	})

	g := newTestGitHubClient(t, mux)
	fix := clio.Fix{
		Branch:      "clio/fix-test",
		Title:       "Fix test bug",
		Body:        "Fixes a test bug",
		FileChanges: map[string]string{"main.go": "package main"},
	}

	url, err := g.CreatePR(context.Background(), "owner/repo", fix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://github.com/owner/repo/pull/42" {
		t.Errorf("url = %q", url)
	}
}

func TestGitHubClient_ListOpenClioPRs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{"head": map[string]string{"ref": "clio/fix-auth-abc123"}, "number": 1},
			{"head": map[string]string{"ref": "feature/unrelated"}, "number": 2},
			{"head": map[string]string{"ref": "clio/fix-db-def456"}, "number": 3},
		})
	})

	g := newTestGitHubClient(t, mux)
	branches, err := g.ListOpenClioPRs(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("expected 2 clio branches, got %d: %v", len(branches), branches)
	}
}

func TestGitHubClient_CommentOnPR(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"id": 1, "body": "test comment"})
	})

	g := newTestGitHubClient(t, mux)
	err := g.CommentOnPR(context.Background(), "owner/repo", "https://github.com/owner/repo/pull/42", "test comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
