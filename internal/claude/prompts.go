package claude

import (
	"fmt"
	"strings"

	"github.com/njayp/clio"
)

// classifyPrompt builds the prompt for Claude to classify an error event.
func classifyPrompt(event clio.ErrorEvent) string {
	var sb strings.Builder
	sb.WriteString("You are a Kubernetes error classifier. Analyze the following error logs and determine if this is a code bug that can be fixed in the application source code, or an operational/infrastructure issue.\n\n")

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

	sb.WriteString(`Respond in JSON with exactly these fields:
{
  "is_code_bug": true/false,
  "summary": "brief description of the error",
  "confidence": 0.0-1.0,
  "relevant_files": ["path/to/file1.go", "path/to/file2.go"]
}

Rules:
- is_code_bug: true if this error can be fixed by changing application source code (null pointer, unhandled exception, logic error, missing error handling)
- is_code_bug: false if this is operational (OOMKilled, resource limits, missing config, network issues, infrastructure)
- relevant_files: best-guess file paths from stack traces or error messages. Empty array if unclear
- confidence: how confident you are in the classification (0.0 = guessing, 1.0 = certain)
- Respond ONLY with JSON, no explanation`)

	return sb.String()
}

// fixPrompt builds the prompt for Claude to generate a code fix.
func fixPrompt(event clio.ErrorEvent, class clio.Classification, files map[string]string) string {
	var sb strings.Builder
	sb.WriteString("You are a code fix generator. Given error logs and the relevant source files, produce a minimal fix.\n\n")

	sb.WriteString("## Error Summary\n")
	sb.WriteString(class.Summary + "\n\n")

	sb.WriteString("## Error Logs\n```\n")
	for _, line := range event.LogLines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("```\n\n")

	sb.WriteString("## Source Files\n")
	for path, content := range files {
		sb.WriteString(fmt.Sprintf("### %s\n```\n%s\n```\n\n", path, content))
	}

	sb.WriteString(`Respond in JSON with exactly these fields:
{
  "title": "Short PR title (imperative mood, under 72 chars)",
  "body": "PR description explaining the fix",
  "reasoning": "Your analysis of the root cause and why this fix is correct",
  "file_changes": {
    "path/to/file.go": "entire new file content with the fix applied"
  }
}

Rules:
- Make the MINIMAL change needed to fix the bug
- Do not refactor, clean up, or improve unrelated code
- file_changes must contain the COMPLETE new file content for each changed file
- If you cannot produce a confident fix, return empty file_changes and explain in reasoning
- Respond ONLY with JSON, no explanation`)

	return sb.String()
}
