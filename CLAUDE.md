UPDATE THIS FILE (CLAUDE.md) AND README.md AS NEEDED

## Architecture

Clio watches K8s pods for errors, triages them with heuristics, then runs a 3-step Claude Code workflow in a git worktree:

1. **Investigate** (`claude -p` with `--allowedTools`) — read-only investigation; writes `clio-plan.md` (code bug) or `RESULT.json` (operational)
2. **Review** (`claude -p "/plan"` with `--dangerously-skip-permissions`) — refines the plan via the plan-reviewer agent (non-fatal if it fails)
3. **Implement** (`claude -p "/go"` with `--dangerously-skip-permissions`) — implements the plan, runs /simplify, commits

After step 3, Go code pushes the branch and creates a GitHub PR via `gh pr create`. The `.claude/` directory (skills, agents) is embedded via `go:embed` and written into each worktree.

**Key packages:**
- `internal/k8s/` — Pod watching, log filtering, K8s context gathering
- `internal/pipeline/` — Orchestration (dedup, batching, rate limiting, PR flow)
- `internal/triage/` — Lightweight heuristic triage (OOM, DNS, image pull → operational)
- `internal/agent/` — Claude Code subprocess management, git worktrees, prompt construction
- `internal/github/` — GitHub API client (PRs, comments)
- `internal/server/` — Health/metrics HTTP server

## Claude-Code Plan Guidelines

**Context:** Explain why this change is needed — the problem, what prompted it, and the intended outcome.
**Reuse:** Search for existing functions, utilities, and patterns before proposing new code. List any reused code with file paths.
**Simplicity:** Follow existing patterns, conventions, and tech stack. Avoid unnecessary abstractions — don't add new helpers, layers, or files when existing ones suffice.
**Completeness:** Include absolute file paths with line numbers, a "Critical Files" section, and a testing strategy where applicable.
**Verification:** Include concrete steps to verify changes end-to-end using available tools (e.g. `go test`, `grep`, build commands, browser automation) — not manual inspection alone.

## Coding Guidelines

- **small files** — keep files small (under 400 lines) and focused on a single responsibility
- **DRY** — don't repeat yourself
- **YAGNI** — you ain't gonna need it
- **KISS** — keep it simple, stupid
- **less is more** — prefer simplicity and elegance. remove unnecessary code
- **test behavior** — prefer testing behavior over implementation details
- **log levels** — errors: something failed that shouldn't have; warnings: system works but is degraded or misconfigured; info: normal operations worth noting
