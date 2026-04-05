package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/njayp/clio"
)

// writeErrorContext appends pod metadata and K8s context to a builder.
func writeErrorContext(sb *strings.Builder, event clio.ErrorEvent) {
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
}

// BuildInvestigationPrompt constructs the prompt for the investigation step.
// The agent investigates the error and writes clio-plan.md (code bug) or RESULT.json (operational).
func BuildInvestigationPrompt(event clio.ErrorEvent, branch string) string {
	var sb strings.Builder

	sb.WriteString("Investigate the following Kubernetes error and determine if it is a code bug that can be fixed in this repository.\n\n")
	writeErrorContext(&sb, event)

	sb.WriteString(fmt.Sprintf(`## Instructions

Your working branch is: %s

Do NOT modify source code. Your job is investigation only.

1. Read the error logs and Kubernetes context above.
2. Search the codebase for relevant files (use stack traces, error messages, and package names as clues).
3. Determine if this is a code bug fixable in this repository, or an operational/infrastructure issue.
4. If it IS a code bug:
   a. Write a file named clio-plan.md in the repository root with:
      - Root cause analysis
      - Step-by-step fix instructions (specific files, line numbers, what to change)
      - Critical files involved
   b. Do NOT make any code changes — only write the plan.
5. If it is NOT a code bug but needs human attention (e.g. misconfigured resource limits, bad config map values):
   a. Create a GitHub issue:
      - `+"`gh issue create --title \"<issue title>\" --body \"<description with K8s context>\"`"+`
   b. Write RESULT.json (see below). Do NOT write clio-plan.md.
   c. Do NOT create an issue for purely operational problems (OOM, DNS resolution, image pull errors) — just write RESULT.json.
6. If it is NOT a code bug (no issue needed), write RESULT.json:

`, branch))

	sb.WriteString("```json\n")
	sb.WriteString(`{
  "is_code_bug": false,
  "issue_url": "",
  "title": "",
  "reasoning": "Explanation of why this is not a code bug"
}
`)
	sb.WriteString("```\n\n")

	sb.WriteString("You MUST write either clio-plan.md (code bug) or RESULT.json (not a code bug) before finishing.")

	return sb.String()
}

// InvestigationSystemPrompt returns the system prompt for the investigation step.
func InvestigationSystemPrompt() string {
	return `You are Clio, an automated error-investigating agent for Kubernetes applications. Investigation mode only.

Rules:
- Do NOT modify source code — investigation only.
- If it's a code bug, write clio-plan.md with root cause and fix steps. Do NOT write RESULT.json.
- If it's operational, write RESULT.json. Do NOT write clio-plan.md.
- Do NOT create GitHub issues for purely operational problems (OOM kills, DNS failures, image pull errors).
- Use "gh issue create" for GitHub operations — the CLI is already authenticated.`
}

// writeContextFile writes K8s error context to clio-context.md in the worktree.
func writeContextFile(wtDir string, event clio.ErrorEvent) error {
	var sb strings.Builder
	writeErrorContext(&sb, event)
	return os.WriteFile(filepath.Join(wtDir, contextFile), []byte(sb.String()), 0o644)
}

// BuildShipPrompt constructs the prompt for the ship step (push + PR + RESULT.json).
func BuildShipPrompt(branch string) string {
	return fmt.Sprintf(`You are Clio, an automated error-fixing agent. Your job is to push the branch and create a GitHub PR.

## Instructions

1. Check for new commits:
   Run: git log HEAD --not origin/HEAD --oneline
   If there are NO new commits, write RESULT.json (see failure format below) and stop.

2. Push the branch:
   Run: git push origin %s

3. Create a GitHub PR:
   - Read clio-context.md and clio-plan.md from the repository root.
   - Build a PR body with the Kubernetes context and plan.
   - Get the commit subject: git log -1 --format=%%s
   - Run: gh pr create --head %s --title "<commit subject>" --body "<PR body>"

4. Write RESULT.json as the VERY LAST step:

`+"```json"+`
{
  "is_code_bug": true,
  "pr_url": "<URL from gh pr create output>",
  "title": "<commit subject>",
  "reasoning": "<brief summary of what was fixed>"
}
`+"```"+`

## On failure

If any step fails (no commits, push error, PR creation error), write RESULT.json with:

`+"```json"+`
{
  "is_code_bug": true,
  "pr_url": "",
  "title": "",
  "reasoning": "<explanation of what failed>"
}
`+"```"+`

You MUST write RESULT.json before finishing, regardless of success or failure.
`, branch, branch)
}
