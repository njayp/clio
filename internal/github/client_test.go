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

func TestGitHubClient_CreatePRFromBranch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"default_branch": "main"})
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
		Branch: "clio/fix-test",
		Title:  "Fix test bug",
		Body:   "Fixes a test bug",
	}

	url, err := g.CreatePRFromBranch(context.Background(), "owner/repo", fix)
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
