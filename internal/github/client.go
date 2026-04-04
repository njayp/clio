package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/njayp/clio"
	gh "github.com/google/go-github/v72/github"
)

// Client implements the pipeline.GitHubClient interface using the go-github SDK.
type Client struct {
	client *gh.Client
}

// NewClient creates a Client with the given token.
func NewClient(token string) *Client {
	client := gh.NewClient(nil).WithAuthToken(token)
	return &Client{client: client}
}

func splitRepo(repo string) (owner, name string) {
	parts := strings.SplitN(repo, "/", 2)
	return parts[0], parts[1]
}

// ListOpenClioPRs returns branch names of open PRs with the clio/ prefix.
func (g *Client) ListOpenClioPRs(ctx context.Context, repo string) ([]string, error) {
	owner, name := splitRepo(repo)

	prs, _, err := g.client.PullRequests.List(ctx, owner, name, &gh.PullRequestListOptions{
		State:       "open",
		ListOptions: gh.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("list PRs: %w", err)
	}

	var branches []string
	for _, pr := range prs {
		branch := pr.Head.GetRef()
		if strings.HasPrefix(branch, clio.BranchPrefix) {
			branches = append(branches, branch)
		}
	}
	return branches, nil
}
