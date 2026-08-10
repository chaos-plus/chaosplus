# CLAUDE.md

---

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

## 5. Persistent Memory & Shared Specs

**Single `MEMORY.md` at repo root = cross-device, cross-session memory. Update it after every task.**

- On a new session or device, read the root `MEMORY.md` first to restore context.
- Append/update the relevant entry: what was done, why, and any non-obvious context needed to resume.
- Keep it concise — record what helps recover work later, not what the code, git history, or `.rules/` already capture. Rule bodies live in `.rules/`; memory keeps only pointers.

**Shared specs live in `.rules/` at the repo root** (moved out of `docs/.rules/`), shared by `apps/` and `.claude/skills/`. Reference them as repo-root-relative `.rules/<file>.md`.

**When in doubt, follow `.rules/`.** If a task, PRD, skill, or even this file says something different from `.rules/<file>.md`, use `.rules/` as the source of truth. Record the discrepancy in `MEMORY.md` so it can be resolved explicitly.

**Skills auto-sync with `.rules/`.** Every skill under `.claude/skills/` MUST reference the relevant `.rules/` file(s) as its canonical spec, not duplicate rules inline. When you implement a feature or fix, check whether the outcome changes a `.rules/` constraint — if it does, update the `.rules/` file first, then note the change in `MEMORY.md`. Skills that load their rules from `.rules/` at runtime stay current automatically; skills with hardcoded copies rot. Prefer the reference pattern: `See .rules/3.ARCH.md §"分布式执行器调度"` over copying those rules into the skill body.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.
