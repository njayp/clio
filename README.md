# Clio

Kubernetes-native agent that watches staging pods for errors, uses Claude Code to investigate and fix code bugs, and opens GitHub PRs for human review.

## How It Works

1. **Watch** — Tails logs from all pods in a Helm release via K8s API
2. **Filter & Batch** — Detects error patterns (ERROR/FATAL/panic/stack traces) and groups multi-line errors
3. **Dedup** — Fingerprints errors to avoid duplicate processing within a cooldown window
4. **Triage** — Lightweight heuristics skip obvious operational issues (OOM, image pull, DNS) before invoking the agent
5. **Agent** — Claude Code runs in a git worktree with the full repo and Go toolchain, investigating the error, reading code, making fixes, and verifying with `go build`/`go test`
6. **PR** — Opens a GitHub PR on a `clio/` prefixed branch with full context (logs, reasoning)

## Quick Start

### As a Helm Subchart

Add clio as a dependency in your app's `Chart.yaml`:

```yaml
dependencies:
  - name: clio
    version: "0.1.0"
    repository: "oci://ghcr.io/njayp/charts"
    condition: clio.enabled
```

Configure in your `values.yaml`:

```yaml
clio:
  enabled: true
  repo: "owner/repo"
  secretName: "myapp-clio"  # Secret with ANTHROPIC_API_KEY and GITHUB_TOKEN
  dryRun: true              # Start with dry run to observe behavior
```

Create the Secret:

```bash
kubectl create secret generic myapp-clio \
  --from-literal=ANTHROPIC_API_KEY=sk-ant-... \
  --from-literal=GITHUB_TOKEN=ghp_...
```

### Configuration

All configuration is via environment variables, set through Helm values:

| Value | Default | Description |
|---|---|---|
| `repo` | required | GitHub repo (`owner/repo`) |
| `target` | `""` | Narrow to specific `app.kubernetes.io/name` (empty = all pods in release) |
| `secretName` | required | Secret with `ANTHROPIC_API_KEY` and `GITHUB_TOKEN` |
| `cooldown` | `1h` | Dedup cooldown window |
| `maxConcurrency` | `3` | Max parallel fix pipelines |
| `maxPRsPerHour` | `5` | Rate limit for PR creation |
| `batchWindow` | `5s` | Window to group multi-line errors |
| `maxAgentTurns` | `25` | Max Claude Code agent turns per session |
| `dryRun` | `false` | Log actions without creating PRs |

### Dry Run

Start with `dryRun: true` to observe clio's behavior without creating PRs. Clio will classify errors and log what it would do:

```
{"level":"INFO","msg":"dry run: would open PR","pod":"myapp-abc-xyz","branch":"clio/fix-nil-pointer-a1b2c3d4","title":"Fix nil pointer in auth handler"}
```

### Deployment History

Clio reads deployment history (ReplicaSet revisions) to detect rollbacks. If the current deployment has been rolled back, clio skips PR creation since the error is already operationally addressed.

### Observability

- `/healthz` — Returns 200 when the watcher is active
- `/metrics` — Prometheus metrics:
  - `clio_errors_detected_total`
  - `clio_errors_classified_total{type=code_bug|operational}`
  - `clio_agent_duration_seconds`
  - `clio_agent_outcome_total{outcome=code_bug|operational|error}`
  - `clio_prs_opened_total`
  - `clio_prs_skipped_total{reason=dedup|rate_limit|existing_pr|dry_run|rollback}`
