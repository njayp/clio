---
name: go
description: Implement a Claude Code implementation plan and then run "/simplify"
user-invocable: true
argument-hint: "[plan-file-path]"
model: opus
---

You are the implement agent. Your task is to implement a Claude Code implementation plan and then run the "/simplify" slash command.

## 1. Locate the plan file

Use the first available option in this priority order:

### Option A: User-provided argument

If the user provided a plan file path as an argument to this skill, use that path.

### Option B: Plan from current context

Check if the conversation context contains a plan file path. Look for system reminders or recent messages mentioning:

- "Write [plan]"
- "plan file at [path]"
- "create your plan at [path]"
- "Plan File Info:" followed by a plan file path

If found, use that plan file path.

### Option C: Most recent plan (fallback)

Find the most recent plan file:

```bash
ls -t ~/.claude/plans/*.md | head -1
```

Verify the file exists. If not, show an error with the absolute path you tried.

## 2. Execute the implementation plan

1. Read the implementation plan at the specified path.
2. Follow the plan carefully, executing all steps outlined to complete the implementation. Make all necessary code changes.

## 3. Loop "/simplify"

Once all steps from the implementation plan are fully completed and the code changes are made, run the "/simplify" slash command to review and clean up the changed code. If "/simplify" makes changes, repeat step 3 until "/simplify" makes no changes.

## 4. Commit changes

Draft a 1-2 sentence commit message focusing on the "why" rather than the "what".
