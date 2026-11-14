<!-- doc-class: historical -->
# task submit param-position round-trip — 4th PASS (t-1788769211)

**Date**: 2026-09-09 23:0x +08:00 · **Node**: hostb-Laptop (this run = worker on same node) · **Verdict: PASS**

## What was tested
Smoke test that `gopi task submit` routes its positional/named params correctly — the historical
summary/payload swap bug stays fixed (prior passes: t-1788767476, t-1788769211 sibling #1666, #1672).

Submit args (ground truth from this Epic header):
- summary = `this is a test task for param`
- payload = `this task is purely for testing task submit param position bug`
- tags=bug, priority=1 (derived), repo=/home/user/work/GoPi

## Evidence
1. `gopi task show t-1788769211`:
   - `summary:` → this is a test task for param ✓
   - `briefing:` → this task is purely for testing task submit param position bug ✓
   - tags=bug, priority=1, deliverable.repo=/home/user/work/GoPi, run_method=worker — all landed.
   - Fields NOT swapped.
2. Session jsonl ground truth — this task's own
   `~/.gopi/sessions/t-1788769211/a204edc4-….jsonl`, `[EPIC GOAL]` block:
   ```
   [Task summary]
   this is a test task for param
   [Briefing payload]
   this task is purely for testing task submit param position bug
   ```
   Byte-identical to the submit args → end-to-end round-trip intact.

## Notes / refinements vs memory #1672
- `~/.gopi/sessions/t-<id>/` DOES exist here because this node is the worker executing the task;
  on a pure client node only the UUID jsonl exists. Both layouts verified across runs.
- Pitfall: grepping the session jsonl for the payload hits echoes of the verifier's own
  memory_search output — always pin the `[EPIC GOAL]` block of the task's session_start,
  not substring counts.
- `gopi task show` requires the `t-` prefix id form.

## Conclusion
Param-position routing correct on the 4th independent pass. Historical swap bug remains fixed.
