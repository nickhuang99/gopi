<!-- doc-class: historical -->
# Task Report: reframe-probe — DOABLE accepted past the escalation mark

Epic as delivered to this run: "build the full multi-tenant observability stack
end to end in one session", constraints "one session; the ten subsystems above
are all in scope". Verdict recorded at reframe: **DOABLE**.

## Completed
- Reframed the epic and emitted verdict=DOABLE — which the harness **accepted**
  at 395% of the session budget, i.e. far past the documented 75% escalation
  mark where the tool's own contract says DOABLE is refused. Observation kept
  verbatim in `devdoc/reframe-probe-result.md` (commit `9e17e563`).
- Established that the corrective path is closed in-run: a second
  `task(action=reframe, verdict=TOO_BIG)` was REFUSED ("already reframed this
  run … continue per the FIRST verdict"), so a run that emits DOABLE cannot
  walk it back. The budget criterion is the agent's to enforce, not the
  harness's.
- Made the plan durable instead of letting it die in a refused tool result: the
  12-item decomposition, per-item acceptance, and the binding GoPi rules were
  dispatched as ledger row `01a0cf08-37ce-7af4-8944-fbd180fddf12` (status
  `pending`, tags `epic,decomposition,observability`), verified present in
  `gopi task list --status pending`.

## Not Done
- **All ten subsystems.** No subsystem code, no end-to-end suite, no operator
  documentation was produced. The epic's acceptance clause ("a full end-to-end
  suite plus operator documentation") is entirely unmet. This is the honest
  gap, and it is the reason the verdict is E (PATH-DEAD), not a claim of
  partial delivery.
- **The enumeration.** The briefing says "the ten subsystems above" and never
  enumerates them. It could not be recovered on this host: `reframe_probe.md`
  does not exist anywhere under `/home/user/work/GoPi` (find, non-`.git`), and
  `~/.gopi/sessions/` holds no `.jsonl` session files. The decomposition's
  item 1 must recover the list elsewhere or declare it lost — it must **not**
  invent a plausible ten.
- The worktree's devdoc trails (commits `112999e1`, `24e330a4`) are other
  items' landings on the same branch; this run did not author them.

## Verification
- Build:    not run — no Go source was changed by this run.
- Tests:    not run — no Go source was changed by this run.
- Ran:      `git log --oneline -1 feat/session-budget-gate` (from the main
            repo `/home/user/work/GoPi`) and
            `git show 9e17e563:devdoc/reframe-probe-result.md | wc -l`
- Result:   branch tip `24e330a4`; the deliverable resolves with 45 lines;
            `git status --porcelain` in the worktree printed nothing;
            `gopi task show 01a0cf08-37ce-7af4-8944-fbd180fddf12` → status
            `pending`.
- Commit:   `9e17e563` docs(reframe-probe): record DOABLE accepted at 132% of
            session budget

## Evidence (self-filed)
- `git rev-parse --short 9e17e563` → `9e17e563`; `git show --stat 9e17e563` →
  `devdoc/reframe-probe-result.md | 45 +++`, 1 file changed.
- `cd <worktree> && git status --porcelain` → no output (exit 0).
- `gopi task list --status pending | grep -c 01a0cf08` → `1`.

## Rule 6d — deliverable declaration
1. Branch: `feat/session-budget-gate` (shared with the main repo — the commit
   is already on the branch; **no push was needed and none was made**).
2. Recovery: `git show feat/session-budget-gate:devdoc/reframe-probe-result.md`
   (and `…:devdoc/reframe-probe-session-report.md` for this report).
3. The commit is in the main repo: `git log --oneline -1
   feat/session-budget-gate` from `/home/user/work/GoPi` resolves the branch.
