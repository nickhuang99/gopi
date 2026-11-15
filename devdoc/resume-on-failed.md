<!-- doc-class: normative -->
# `gopi resume` on a FAILED task — what actually happened to 01a097f8, and what the ledger can/cannot tell you

**Question under investigation** (operator, 2026-09-13): `gopi task show 01a097f8…` printed
`status: failed` with `resumed: ×1 [via resume]`. Does that mean *the task was resumed and still
failed*? Does a resume of a failed task deterministically fail for the same reason — should we allow
it at all?

**Answer in one line**: the row says `failed` because the *last status write on it* was a
**post-merge verify verdict that happened 4 hours BEFORE the resume**; the resume itself **ran and
succeeded** (exit_code 0, 125 s, 684 399 tokens) — it just had **no way to say so**, because the
resume path writes lineage (`resumed:<session>` tag, `run_method=resume`) and never an outcome, and
its one status-bearing write is pinned `WHERE status='in_progress'`, i.e. a silent ZERO-row no-op on
a terminal row. So this is the "unchanged stale status" case, **not** a re-failure.

Evidence chain (all commands are reproducible; raw transcripts in
`/home/user/.gopi/sessions/01a097f8-…/` and the ledger `~/.gopi/memory.db`):

| time (+08:00) | actor | fact |
|---|---|---|
| 07:33:58 | human | row submitted (`bug`, priority 1) |
| 07:34:21 | worker `peer1:1842018` | `in_progress` event (session `4616b463…`) |
| 07:55:30 | that session | `d7474fb4 feat(task): persist a task's process identity …` |
| 07:58:47 | wrapper | `completed` event (24m25s, task-report 5372 B) |
| 10:13:56 | merger | merge `5f8f7d21 Merge branch 'feat/task-process-identity'` |
| 10:21:34 | **merge-verify child** (session `0f6b34d0…`, `gopi task update … --status failed`) | **`failed` event** — "the merge is RED on master: internal/tools fails ONLY on its own new test `TestTaskStatusRunningForOwnRow` … the assertion is wrong" |
| 10:22:10 | merger | rework row `01a09892-…` filed (`merge-verify,branch-rework`) |
| 10:22:11 | merger | `merge-verify` event: master `reset --hard dc3c09bb2418c39ea6d52a87fe9214b83f4d5bdd` |
| 10:27:13 | merger | merge `6b32a060` — **re-merged and KEPT** (`d7474fb4` is an ancestor of master today: `git merge-base --is-ancestor d7474fb4 master` → yes) |
| 14:16:09 / 14:16:38 | verifier (row `01a09957`) | deliverable independently verified **PASS** (`ab9131ba` is master tip at that moment) |
| 14:18:29 | **operator** | `gopi resume …4616b463….raw.log --task 01a097f8…` → **new session `0a4a1f96…`, pid 2744622**; row tag `resumed:0a4a1f96-…` written 14:18:30 |
| 14:20:36 | that session | `session_end` **exit_code 0**, 23 iterations, 125.9 s, 684 399 tokens; committed conclusion `session_conclusions(0a4a1f96…)`: **"RECOVERY AUDIT — deliverable SAFE, no loss"** |
| 14:20:50 | worker | rework row `01a09892` closed `completed` (44m37s) |

The resumed session's own committed work-area (8 items) is the receipts: item 1 = survey, item 8 =
"RECOVERY AUDIT — deliverable SAFE, no loss", items 2–7 = **"skipped: superseded"** — i.e. the
resume's actual content was an audit, not a rework, because the cause (`TestTaskStatusRunningForOwnRow`)
had already been fixed by rework `01a09892` / merges `6b32a060`+`ab9131ba`.

## Why the ledger could not answer the question (mechanism, file:line)

```
gopi resume <task-id>
  └─ decideResumeByTaskID(taskID)            cmd/agent/cli_resume.go:100-118
        │  switch st.Verdict:                (tools/taskproc.go, TaskProcessStatusFor)
        │    running                 → refuse (double run)
        │    foreign | unknown       → refuse (unverifiable)
        │    dead | recycled | no-holder | n/a → PROCEED
        │
        └─ every TERMINAL row has NO holder: the identity + claim stamp is CLEARED on the
           terminal transition ⇒ verdict n/a, note "row is failed — no holder to verify",
           resumable yes            (live proof below)
  └─ tools.TaskStampSelfClaim(taskID)        internal/tools/tasks.go:4667
        │  UPDATE … WHERE task_id = ? AND status = 'in_progress'      ← ZERO rows on a failed row
        │  (no RowsAffected check ⇒ a no-op reports success — Rule 15 shape)
  └─ tools.TaskTagResumed / TaskStampRun     internal/tools/tasks.go:4101 / runcmd stamp
        └─ adds ONLY `resumed:<session>` + run_method/run_cmd — lineage, never an outcome
  └─ ag.Resume(input, history)               internal/agent/loop.go:2491 → runEngine(resume=true)
        └─ start hook skipped on resume (`if !resume { maybeRegisterSessionTask }`, loop.go:2590)
           and the end hook's close-out is inert (autoTaskRegistered latched false, wrapper env
           absent) — so no status write-back path exists for a resumed run at all.
```

Live proof of the gate's read on the incident row (read-only, today):

```
$ gopi task status 01a097f8-79bf-73d6-ad4c-1dc14abe0086
status:      failed
verdict:     n/a
evidence:    row is failed — no holder to verify
resumable:   yes
holder:      (none)
recorded:    (none) (row predates the identity columns)
```

So **the gate is deliberately status-blind** (its own usage text: "GATED on the row's process
identity, never on its status string"), which is correct for its purpose (double-run prevention) —
but it means a terminal row is *always* resumable, and nothing downstream records what the resume did.

> **SUPERSEDED IN PART (2026-10-29).** Two of the facts recorded above no longer hold, and a
> reader must not act on them. (1) The gate is **no longer status-blind**: `failed` was split
> from *finished*, and the resume of a failed row became **LEVEL 1** of the failure policy — the
> default handling of a failed run, which continues the SAME row (same task id, same session
> id, same directory, checkpoint appended). A COPY (`gopi task clone` → `<id>-c1`) is LEVEL 2,
> the remedy after a resume has failed again. The statement of the policy is the header of
> `internal/tools/task_run_retry.go`; the gate now refuses only FINISHED statuses
> (completed/verified/superseded), admits `cancelled` with a printed caveat, and additionally
> refuses a holder whose claim stamp names ANOTHER HOST (verdict `unknown` — this machine cannot
> judge that pid). (2) "Nothing downstream records what the resume did" is **fixed**: the
> ownership handoff (`TaskStampSelfClaim`) now moves a failed row back to `in_progress` in the
> same statement that records the holder, so the ledger shows the resumed run — and the stamp
> was moved to AFTER every refusal point, so a resume that is refused leaves the row exactly as
> it was. What still stands, unchanged and worth the reader's attention: the
> `TaskResumeTarget` session fallback described in the next paragraph.

Second, smaller leg found while reconstructing: `TaskResumeTarget` falls back to the **row's own
session** when the resumed session has no raw log of record (`internal/tools/taskproc.go:607-619`).
The live resumed session `0a4a1f96` has **no `.raw.log`** (its dir holds only `metadata.json` +
`session.jsonl`), so a repeat of the operator's command today resolves to `4616b463…` — the session
that *delivered* the branch — i.e. the rerun is re-armed with a dead end it cannot detect.

**Empirical confirmation of the blind spot** (added 2026-09-14, re-verified against the live ledger):
`SELECT event,created_at FROM task_events WHERE task_id LIKE '01a097f8%'` returns FIVE rows and the
last one is the `failed` at 10:21:34 — **no event was written by the 14:18 resume at all**. So a
reader of `task_events` alone sees a row that was failed and never touched again; the ONLY trace of
the resume is the `resumed:<session>` tag (rendered `[resumed×1] [via resume]`), which is
deliberately lineage-only. The resumed session's own committed conclusions — 8 work-area items
ending in "RECOVERY AUDIT — deliverable SAFE, no loss" — are stored in `session_conclusions`, which
`gopi task show` does not render. **That is precisely why the operator could not tell a successful
resume from a re-failure: the evidence existed, but no operator-facing surface joined it to the row.**

## Do we allow such a resume? Does it "definitely fail for the same reason"?

- **Not deterministic, and it did not re-fail**: the resume ran, exited 0, and its own conclusion was
  "deliverable SAFE". It also **never re-ran the failing gate** — the gate that failed (post-merge
  verify of `5f8f7d21`) is a merger step, not part of the resume pipeline; it had already passed
  twice more by the time of the resume (`6b32a060` kept, `01a09957` PASS).
- **Refusing it would buy no safety**: the failure mode the gate guards (double-running a live run)
  cannot occur on a holderless row. Refusing terminal-row resumes would instead break the legitimate
  recovery path (`failed` events also appear on rows later moved `verified`, e.g. `01a09732-…`
  failed 04:19:41 → verified 15:27:04 after a resume).
- **The real defect is silent sameness**: resume writes no outcome, so "resume succeeded" and
  "resume re-failed" are indistinguishable in the ledger. That is the question the operator asked,
  and the ledger cannot answer it by construction.

**Recommendation (ranked; each is falsifiable against the ledger):**

1. **Record the resume attempt as an append-only ledger event carrying the PRIOR cause** (smallest
   fix, one line beside the existing `TaskStampRun` in `cli_resume.go`). Adopted ⇒
   `gopi task show` on this row would gain a line such as
   `resumed: run 0a4a1f96 started 14:18 (prior outcome: failed at 10:21 — post-merge verify …)` and
   the operator's question answers itself. It does not change any status semantics (append-only).
2. **Carry the terminal cause into the resumed context**: the row's terminal `task_events` detail
   (here: the merge-verify verdict) should be injected with the continuation. Today the resumed model
   gets the checkpoint + the ORIGINAL session's committed conclusions only, so it must rediscover
   *why* the task is failed (the incident's audit did exactly that, at 684k tokens).
3. **Warn on the stale-session fallback** (shape 2): when the resolved checkpoint is the row's own
   already-terminal session and no recorded resumed session exists, print
   `no resumed session on record — this resumes the DELIVERED session <id>`. Cheap, and it removes
   the one genuinely misleading case.
4. **Do NOT** refuse `resume` on terminal rows as such: no double-run exposure, and it breaks
   recovery/rework re-entry. If a status-based guard is ever wanted, the only honest one is a
   *warning naming the resolution* (item 3), not a refusal.

## Executable statement of the above (regression guard)

`cmd/agent/cli_resume_failed_row_test.go` (new, this branch) pins the four facts the recommendation
rests on — so a future change to the gate or to the ownership handoff cannot silently invalidate the
decision, and the "do we allow it" posture is one reviewable diff away from changing:

- `TestResumeGateProceedsOnTerminalRow` — a `failed`, holderless row: `resumable true`, gate =
  `resumeGateProceed`.
- `TestResumeOnTerminalRowLeavesTheStatusTerminal` — `TaskStampSelfClaim` on a `failed` row leaves
  the status `failed` (the pin is `status='in_progress'`); if this ever flips, the change must be
  deliberate and reported.
- `TestResumeProvenanceMarksTheRerunButNotTheOutcome` — the resume's only signals are
  `resumed:<session>` (`TaskResumeCount == 1`) and `run_method=resume`; the outcome face is untouched.
- `TestResumeTargetOnTerminalRowStillResolvesTheRowSession` — the target resolution returns the
  row's own session id (`4616b463…` shape) even on a terminal row.

## Verification

- Build: `go build ./...` → OK (see commit message).
- Tests: `GOPI_MEMORY_DB= go test ./cmd/agent/ -run 'TestResume…' -count=1 -v` → 4/4 PASS
  (output in the commit's test run); full `go test ./cmd/agent/ ./internal/tools/ -count=1` green.
- Evidence sources: `~/.gopi/memory.db` (`tasks`, `task_events`, `session_conclusions`),
  `~/.gopi/sessions/01a097f8-…/{4616b463…,metadata.json}`,
  `~/.gopi/sessions/0a4a1f96-…/{session.jsonl,metadata.json}`,
  `~/.gopi/sessions/0f6b34d0-…/session.jsonl` (the merge-verify child that wrote `failed`), git:
  `5f8f7d21`, `6b32a060`, `d7474fb4`, `dc3c09bb`, `ab9131ba`.

## Not Done

- No product change made (investigation + executable characterization only) — items 1–3 above are
  recommendations for a follow-up task; only item 3 is a one-line UX fix.
- The `TaskStampSelfClaim` zero-rows-affected silent no-op is reported here but not fixed (Rule 15
  shape; fixing it touches the claim ladder and deserves its own task).
- `internal/stylegate` / `internal/testutil` pre-existing failures untouched (out of scope, red on
  pristine master).
