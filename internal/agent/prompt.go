package agent

import (
	"fmt"
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

// BuildPRBody constructs a PR body with K8s context and plan reasoning.
func BuildPRBody(event clio.ErrorEvent, planContent string) string {
	var sb strings.Builder

	sb.WriteString("## Kubernetes Context\n\n")
	writeErrorContext(&sb, event)

	sb.WriteString("## Plan\n\n")
	sb.WriteString(planContent)
	sb.WriteString("\n\n---\n*Automated fix by Clio*\n")

	return sb.String()
}
