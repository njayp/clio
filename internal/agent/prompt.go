package agent

import (
	"fmt"
	"strings"

	"github.com/njayp/clio"
)

// BuildPrompt constructs the prompt passed to `claude -p` with error context.
func BuildPrompt(event clio.ErrorEvent, branch string) string {
	var sb strings.Builder

	sb.WriteString("Investigate the following Kubernetes error and determine if it is a code bug that can be fixed in this repository.\n\n")

	sb.WriteString("## Error Logs\n")
	sb.WriteString(fmt.Sprintf("Pod: %s\nNamespace: %s\nContainer: %s\nRepo: %s\n\n", event.PodName, event.Namespace, event.Container, event.Repo))
	sb.WriteString("```\n")
	for _, line := range event.LogLines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("```\n\n")

	if event.K8sContext != nil {
		sb.WriteString("## Kubernetes Context\n")
		if len(event.K8sContext.Events) > 0 {
			sb.WriteString("Recent Events: " + strings.Join(event.K8sContext.Events, "; ") + "\n")
		}
		if event.K8sContext.DeployName != "" {
			sb.WriteString(fmt.Sprintf("Deployment: %s\n", event.K8sContext.DeployName))
		}
		if event.K8sContext.ImageTag != "" {
			sb.WriteString(fmt.Sprintf("Image Tag: %s\n", event.K8sContext.ImageTag))
		}
		if event.K8sContext.PrevImageTag != "" {
			sb.WriteString(fmt.Sprintf("Previous Image Tag: %s\n", event.K8sContext.PrevImageTag))
		}
		if event.K8sContext.RolledBack {
			sb.WriteString("Note: This deployment has been rolled back.\n")
		}
		sb.WriteString(fmt.Sprintf("Replicas: %d\n", event.K8sContext.Replicas))
		if len(event.K8sContext.ConfigMaps) > 0 {
			sb.WriteString("ConfigMaps: " + strings.Join(event.K8sContext.ConfigMaps, ", ") + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf(`## Instructions

Your working branch is: %s

1. Read the error logs and Kubernetes context above.
2. Search the codebase for relevant files (use stack traces, error messages, and package names as clues).
3. Determine if this is a code bug fixable in this repository, or an operational/infrastructure issue.
4. If it IS a code bug:
   a. Make the minimal fix needed — do not refactor or improve unrelated code.
   b. Run `+"`go build ./...`"+` to verify compilation.
   c. Run `+"`go test ./...`"+` to verify tests pass.
   d. Iterate until both pass.
   e. Stage and commit your changes:
      - `+"`git add -A && git reset HEAD RESULT.json`"+`
      - `+"`git commit -m \"<short imperative description of fix>\"`"+`
   f. Push your branch:
      - `+"`git push origin %s`"+`
   g. Create a pull request:
      - `+"`gh pr create --title \"<PR title>\" --body \"<PR body with K8s context and reasoning>\"`"+`
   h. Record the PR URL in RESULT.json (see below).
5. If it is NOT a code bug but needs human attention (e.g. misconfigured resource limits, bad config map values):
   a. Create a GitHub issue:
      - `+"`gh issue create --title \"<issue title>\" --body \"<description with K8s context>\"`"+`
   b. Record the issue URL in RESULT.json (see below).
   c. Do NOT create an issue for purely operational problems (OOM, DNS resolution, image pull errors) — just write RESULT.json.
6. Write a file named RESULT.json in the repository root with this exact format:

`, branch, branch))

	sb.WriteString("```json\n")
	sb.WriteString(`{
  "is_code_bug": true,
  "pr_url": "https://github.com/owner/repo/pull/123",
  "title": "Short PR title (imperative mood, under 72 chars)",
  "reasoning": "Your analysis of the root cause and why this fix is correct"
}
`)
	sb.WriteString("```\n\n")

	sb.WriteString(`If this is NOT a code bug but you created an issue:

`)
	sb.WriteString("```json\n")
	sb.WriteString(`{
  "is_code_bug": false,
  "issue_url": "https://github.com/owner/repo/issues/456",
  "title": "",
  "reasoning": "Explanation of why this is not a code bug and what action is needed"
}
`)
	sb.WriteString("```\n\n")

	sb.WriteString(`If this is a purely operational issue (no issue needed):

`)
	sb.WriteString("```json\n")
	sb.WriteString(`{
  "is_code_bug": false,
  "title": "",
  "reasoning": "Explanation of why this is an operational issue"
}
`)
	sb.WriteString("```\n\n")

	sb.WriteString("You MUST always write RESULT.json before finishing.")

	return sb.String()
}

// SystemPrompt returns the --append-system-prompt content for the Claude Code agent.
func SystemPrompt() string {
	return `You are Clio, an automated error-fixing agent for Kubernetes applications.

Rules:
- Make minimal changes — fix only the bug, do not refactor or improve unrelated code.
- Always verify your fix compiles with "go build ./..." before finishing.
- Always run "go test ./..." and ensure tests pass.
- If tests fail after your fix, iterate until they pass.
- Always write RESULT.json in the repository root before finishing.
- If you cannot determine the root cause or produce a confident fix, set is_code_bug to false and explain in reasoning.
- When creating PRs, include relevant Kubernetes context (pod name, namespace, error logs) in the PR body.
- When creating issues, include enough context for a human to diagnose and resolve the problem.
- Do NOT create GitHub issues for purely operational problems (OOM kills, DNS failures, image pull errors).
- Use "git push origin <branch>" to push — the remote is already authenticated.
- Use "gh pr create" and "gh issue create" for GitHub operations — the CLI is already authenticated.`
}
