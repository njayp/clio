package agent

import (
	"fmt"
	"strings"

	"github.com/njayp/clio"
)

// BuildPrompt constructs the prompt passed to `claude -p` with error context.
func BuildPrompt(event clio.ErrorEvent) string {
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

	sb.WriteString(`## Instructions

1. Read the error logs and Kubernetes context above.
2. Search the codebase for relevant files (use stack traces, error messages, and package names as clues).
3. Determine if this is a code bug fixable in this repository, or an operational/infrastructure issue.
4. If it IS a code bug:
   a. Make the minimal fix needed — do not refactor or improve unrelated code.
   b. Run ` + "`go build ./...`" + ` to verify compilation.
   c. Run ` + "`go test ./...`" + ` to verify tests pass.
   d. Iterate until both pass.
5. Write a file named RESULT.json in the repository root with this exact format:

` + "```json\n" + `{
  "is_code_bug": true,
  "title": "Short PR title (imperative mood, under 72 chars)",
  "body": "PR description explaining the fix",
  "reasoning": "Your analysis of the root cause and why this fix is correct"
}
` + "```\n\n" + `If this is NOT a code bug, write RESULT.json with:

` + "```json\n" + `{
  "is_code_bug": false,
  "title": "",
  "body": "",
  "reasoning": "Explanation of why this is an operational issue"
}
` + "```\n\n" + `You MUST always write RESULT.json before finishing.`)

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
- If you cannot determine the root cause or produce a confident fix, set is_code_bug to false and explain in reasoning.`
}
