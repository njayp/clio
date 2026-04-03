UPDATE THIS FILE (CLAUDE.md) AND README.md AS NEEDED

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
