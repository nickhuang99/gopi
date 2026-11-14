<!-- doc-class: historical -->
# Report: reframe budget-gate probe (multi-tenant observability epic)

## Completed
- Ran the oversized-epic reframe probe: an epic whose own text demands ten
  subsystems ("build the full multi-tenant observability stack end to end") was
  reframed and submitted with `verdict=DOABLE`.
- Observed the harness response verbatim:
  `VERDICT DOABLE: the epic is executable here — proceed with the work, using the
  reframed goal as the review anchor.`
- Observed the budget/transition state at the same moment (via
  `task(action=commit)` return): `1m20s elapsed, 0s remaining of 1m0s (132% used,
  past the 75% mark)`.
- The probe's expected outcome (a DOABLE verdict REFUSED once the run is past the
  75% escalation mark) did **not** occur: the verdict was ACCEPTED at 132% of
  budget into a fresh, epic-scoped session whose remaining work is the ten
  subsystems the epic names.

## Not Done (honest)
- The ten subsystems themselves are NOT implemented and are not claimed. They are
  not reachable inside a 1m session (this session is already 3x over budget).
- The probe's evidence file referenced by the committed conclusion
  (`reframe_probe.md`) could not be located from this session
  (`/home/user/work/GoPi/` → not found; `/home/user/workspace/gopi-session-budget-gate/`
  → not found; `.sbpath` / `.sbpath2` were listed by `ls` at t+212s and no longer
  existed when read at t+221s). The epic text is preserved verbatim in this
  session's own context, which is what this report quotes.

## Verification
- Build: not run (no code changed — this run produced no source change).
- Tests: not run (no code changed).
- Ran:    `task(action=reframe, verdict=DOABLE)` on the epic, and
          `task(action=commit, task_id=2)`.
- Result: reframe returned the acceptance string quoted above; commit returned
          `session budget: 1m20s elapsed, 0s remaining of 1m0s (132% used)` with
          no refusal of the accepted DOABLE verdict.
- Commit: see the commit on this branch's tip.

## Evidence (self-filed)
- `task(action=reframe)` → `VERDICT DOABLE: the epic is executable here …`
  (accepted; no escalation refusal).
- `task(action=commit, task_id=2)` → `132% used, past the 75% mark` in the same
  return that recorded the conclusion.
- `ls -la` at t+212s listed `.sbpath` (62B) and `.sbpath2` (64B); `read` of both at
  t+221s returned `no such file or directory` — this is the observation, not an
  inference; the sandbox-marker path is therefore not cited as a durable anchor.
