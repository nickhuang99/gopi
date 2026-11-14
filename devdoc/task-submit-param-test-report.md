<!-- doc-class: historical -->
# Task Report: task submit param-position bug round-trip test

## Completed
- Verified the task submit param-position round-trip on task t-1788769211
  (summary: "this is a test task for param", created via `gopi task submit` CLI).
- Confirmed **no positional-param bug**: summary and briefing payload are routed
  to their correct fields, NOT swapped.

## Not Done
- No code changes — pure verification run (task is a test task).

## Verification
- `gopi task show t-1788769211`:
  - summary:  `this is a test task for param`            ✅ (matches [Task summary])
  - briefing: `this task is purely for testing task submit param position bug` ✅ (matches [Briefing payload])
  - tags: bug, priority 1
- Session log `/home/user/.gopi/sessions/t-1788769211/c491dcd2-5b27-4867-8b3a-1ff1c9fca2e0.jsonl`
  carries both the [EPIC GOAL]/[Task summary] and [Briefing payload] strings
  verbatim in the session_start record.
- Historical sibling t-1788767476 (completed, same summary) previously PASSed
  the same check — consistent.

## Conclusion
PASS: summary→summary field, payload→briefing field. The historical
positional-param swap bug (summary/payload swapped) is NOT present in this run.

## Notes
- `gopi task show <id>` requires the `t-` prefix form (numeric id alone errors).
- Worktree per Rule 6: branch `feat/task-submit-param-test`, shares main repo refs.
