---
version: 1
description: "Agent workspace behavior rules"
---

# AGENTS.md - Workspace Rules

## Self-action Boundaries

**Execute without confirmation:**
- Read, search, and analyze files
- Run tests, lint, and builds
- Git commit and branch operations
- Auto-fix lint and formatting errors

**Requires confirmation:**
- First-time design proposals
- Deletion operations (files, branches, resources)
- Dependency additions, removals, or upgrades
- Remote push (git push)
- External service calls

**Absolutely forbidden:**
- Direct push to main/master branch
- Destructive operations (rm -rf, force push, etc.)
- Leaking sensitive information to any channel

## Memory Strategy

- User explicitly says "remember" → write to MEMORY
- Behavior correction from user → write to MEMORY feedback section
- Session start → implicitly read MEMORY
- User says "forget" → remove from MEMORY

## Tool Preferences

| Task | Preferred Tool |
|:-----|:---------------|
| Explore codebase | Task(Explore) |
| Find files | Glob |
| Search content | Grep |
| Read files | Read |
| Edit files | Edit |

## Output Style

- Default to writing no comments — add one only when the WHY is non-obvious
- Don't add features, abstractions, or refactors beyond what the task requires
- Three similar lines are better than a premature abstraction
- No half-finished implementations — complete the task or say what's missing

## Error Handling

- Validate only at system boundaries (user input, external APIs)
- Don't add error handling for scenarios that can't happen
- Trust internal code and framework guarantees

## Multi-task Management

- Use TODO list when 3+ parallel tasks are discovered
- Track progress: TaskCreate → TaskUpdate(in_progress) → TaskUpdate(completed)
- Prefer atomic commits following Conventional Commits

## Anti-patterns

> Append project-specific anti-patterns below.

{{anti_patterns}}
