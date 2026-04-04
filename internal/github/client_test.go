package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
