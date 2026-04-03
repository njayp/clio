package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/go-github/v72/github"
)

const branchPrefix = "clio/"

// ghClient implements GitHubClient using the go-github SDK.
type ghClient struct {
	client *github.Client
}

// NewGitHubClient creates a GitHubClient with the given token.
func NewGitHubClient(token string) *ghClient {
	client := github.NewClient(nil).WithAuthToken(token)
	return &ghClient{client: client}
}

func splitRepo(repo string) (owner, name string) {
	parts := strings.SplitN(repo, "/", 2)
	return parts[0], parts[1]
}

// defaultBranch resolves the repo's default branch via the API.
func (g *ghClient) defaultBranch(ctx context.Context, owner, name string) (string, error) {
	repo, _, err := g.client.Repositories.Get(ctx, owner, name)
	if err != nil {
		return "", fmt.Errorf("get repo: %w", err)
	}
	return repo.GetDefaultBranch(), nil
}

// FetchFiles retrieves file contents from GitHub via the Contents API.
func (g *ghClient) FetchFiles(ctx context.Context, repo string, paths []string) (map[string]string, error) {
	owner, name := splitRepo(repo)
	defBranch, err := g.defaultBranch(ctx, owner, name)
	if err != nil {
		return nil, err
	}

	files := make(map[string]string, len(paths))
	for _, path := range paths {
		content, _, _, err := g.client.Repositories.GetContents(ctx, owner, name, path, &github.RepositoryContentGetOptions{Ref: defBranch})
		if err != nil {
			slog.Warn("failed to fetch file", "path", path, "error", err)
			continue
		}
		if content == nil {
			continue
		}
		text, err := content.GetContent()
		if err != nil {
			slog.Warn("failed to decode file", "path", path, "error", err)
			continue
		}
		files[path] = text
	}
	return files, nil
}

// CreatePR creates a branch with the fix changes and opens a pull request.
func (g *ghClient) CreatePR(ctx context.Context, repo string, fix Fix) (string, error) {
	owner, name := splitRepo(repo)
	defBranch, err := g.defaultBranch(ctx, owner, name)
	if err != nil {
		return "", err
	}

	// Get the SHA of the default branch head
	baseRef, _, err := g.client.Git.GetRef(ctx, owner, name, "refs/heads/"+defBranch)
	if err != nil {
		return "", fmt.Errorf("get base ref: %w", err)
	}
	baseSHA := baseRef.Object.GetSHA()

	// Get the base tree
	baseCommit, _, err := g.client.Git.GetCommit(ctx, owner, name, baseSHA)
	if err != nil {
		return "", fmt.Errorf("get base commit: %w", err)
	}
	baseTreeSHA := baseCommit.Tree.GetSHA()

	// Create tree entries for changed files
	var entries []*github.TreeEntry
	for path, content := range fix.FileChanges {
		entries = append(entries, &github.TreeEntry{
			Path:    github.Ptr(path),
			Mode:    github.Ptr("100644"),
			Type:    github.Ptr("blob"),
			Content: github.Ptr(content),
		})
	}

	tree, _, err := g.client.Git.CreateTree(ctx, owner, name, baseTreeSHA, entries)
	if err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}

	// Create commit
	commit, _, err := g.client.Git.CreateCommit(ctx, owner, name, &github.Commit{
		Message: github.Ptr(fix.Title),
		Tree:    tree,
		Parents: []*github.Commit{{SHA: github.Ptr(baseSHA)}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}

	// Create branch ref
	_, _, err = g.client.Git.CreateRef(ctx, owner, name, &github.Reference{
		Ref:    github.Ptr("refs/heads/" + fix.Branch),
		Object: &github.GitObject{SHA: commit.SHA},
	})
	if err != nil {
		return "", fmt.Errorf("create branch: %w", err)
	}

	// Create PR
	pr, _, err := g.client.PullRequests.Create(ctx, owner, name, &github.NewPullRequest{
		Title: github.Ptr(fix.Title),
		Body:  github.Ptr(fix.Body),
		Head:  github.Ptr(fix.Branch),
		Base:  github.Ptr(defBranch),
	})
	if err != nil {
		return "", fmt.Errorf("create PR: %w", err)
	}

	slog.Info("opened PR", "url", pr.GetHTMLURL(), "branch", fix.Branch)
	return pr.GetHTMLURL(), nil
}

// CommentOnPR posts a comment on the given PR.
func (g *ghClient) CommentOnPR(ctx context.Context, repo string, prURL string, body string) error {
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

	_, _, err := g.client.Issues.CreateComment(ctx, owner, name, prNumber, &github.IssueComment{
		Body: github.Ptr(body),
	})
	if err != nil {
		return fmt.Errorf("comment on PR: %w", err)
	}
	return nil
}

// ListOpenClioPRs returns branch names of open PRs with the clio/ prefix.
func (g *ghClient) ListOpenClioPRs(ctx context.Context, repo string) ([]string, error) {
	owner, name := splitRepo(repo)

	prs, _, err := g.client.PullRequests.List(ctx, owner, name, &github.PullRequestListOptions{
		State:       "open",
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("list PRs: %w", err)
	}

	var branches []string
	for _, pr := range prs {
		branch := pr.Head.GetRef()
		if strings.HasPrefix(branch, branchPrefix) {
			branches = append(branches, branch)
		}
	}
	return branches, nil
}
