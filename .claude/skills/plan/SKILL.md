---
name: plan
description: Review and refine Claude Code implementation plans in-place.
user-invocable: true
argument-hint: "[plan-file-path]"
model: opus
---

You are helping the user iteratively review and refine an EXISTING Claude Code implementation plan until it converges.

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

## 2. Loop plan-reviewer

Launch the plan-reviewer agent, passing it the plan file path. Tell it to review and edit that specific file in-place. Do NOT pass the plan content — the agent will read the file itself. If the plan-reviewer makes changes, repeat step 2 until the plan-reviewer makes no changes.

## 3. Return directive to calling agent

After the review loop completes, output this exact message (substituting the actual path):

> The plan file at [path] has been reviewed and updated in-place. **Do NOT rewrite, copy, or recreate the plan.** If you are in plan mode, proceed directly to ExitPlanMode.
