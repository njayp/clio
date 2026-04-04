package github

import (
	"context"
	"fmt"
	"log/slog"
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

// defaultBranch resolves the repo's default branch via the API.
func (g *Client) defaultBranch(ctx context.Context, owner, name string) (string, error) {
	repo, _, err := g.client.Repositories.Get(ctx, owner, name)
	if err != nil {
		return "", fmt.Errorf("get repo: %w", err)
	}
	return repo.GetDefaultBranch(), nil
}

// CreatePRFromBranch creates a pull request for a branch that has already been pushed.
func (g *Client) CreatePRFromBranch(ctx context.Context, repo string, fix clio.Fix) (string, error) {
	owner, name := splitRepo(repo)
	defBranch, err := g.defaultBranch(ctx, owner, name)
	if err != nil {
		return "", err
	}

	pr, _, err := g.client.PullRequests.Create(ctx, owner, name, &gh.NewPullRequest{
		Title: gh.Ptr(fix.Title),
		Body:  gh.Ptr(fix.Body),
		Head:  gh.Ptr(fix.Branch),
		Base:  gh.Ptr(defBranch),
	})
	if err != nil {
		return "", fmt.Errorf("create PR: %w", err)
	}

	slog.Info("opened PR", "url", pr.GetHTMLURL(), "branch", fix.Branch)
	return pr.GetHTMLURL(), nil
}

// CommentOnPR posts a comment on the given PR.
func (g *Client) CommentOnPR(ctx context.Context, repo string, prURL string, body string) error {
	owner, name := splitRepo(repo)

	// Extract PR number from URL (format: .../pull/123)
	parts := strings.Split(prURL, "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid PR URL: %s", prURL)
	}
	var prNumber int
	fmt.Sscanf(parts[len(parts)-1], "%d", &prNumber)
	if prNumber == 0 {
		return fmt.Errorf("could not parse PR number from URL: %s", prURL)
	}

	_, _, err := g.client.Issues.CreateComment(ctx, owner, name, prNumber, &gh.IssueComment{
		Body: gh.Ptr(body),
	})
	if err != nil {
		return fmt.Errorf("comment on PR: %w", err)
	}
	return nil
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
