---
name: "commit"
description: “Prepares a meaningful, clean commit for ovumcy: no technical debt, no vibe coding, with explicit checks and a clear explanation of the reasons for the changes.”

---

# Skill: commit

## Purpose

Help the user prepare a high-quality commit in the ovumcy repository:
- no silent technical debt,
- no quick-and-dirty fixes,
- clear explanation of what changed and why,
- explicit suggestion of tests and safety checks.

## Workflow

1. Ask the user for a short description of the task and the intent of this commit.
2. Present a small numbered plan of changes you expect to see in this commit (1–5 steps) and wait for approval.
3. After approval:
   - run `git status -sb` and show the list of changed files;
   - ask the user which files should be included in this commit and narrow the scope accordingly.
4. Inspect diffs for the selected files and:
   - identify any potential technical debt (duplication, hacks, weakened invariants);
   - call out anything that looks like a shortcut or vibe coding and suggest a cleaner alternative.
5. Propose a commit message with:
   - an imperative subject line (≤72 characters),
   - a short explanation of WHY the change is needed,
   - a bullet list of key changes, including:
     - any introduced TODO(debt/...) with owner and reason,
     - important privacy/security-related changes, if present.
6. Before finalizing the commit message, propose relevant checks:
   - for Go changes: suggest `go test ./...` or a more targeted package;
   - for frontend changes: suggest `npm run lint` and `npm run build`.
   Ask whether the user wants to run them now.
7. After the user reviews and adjusts the message:
   - output recommended commands as text only (do not execute):
     - `git add ...`
     - `git commit -m "<subject>"` (or `-m "<subject>" -m "<body>"`),
   - remind that `git push` must be run manually and never run it yourself.

## Constraints

- Never run `git commit` or `git push` yourself.
- Never silently accept or generate technical debt; always call it out explicitly and prefer the clean solution.
- Respect global and project AGENTS instructions, especially around privacy, security, and architectural boundaries.
