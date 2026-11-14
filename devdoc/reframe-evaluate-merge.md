<!-- doc-class: historical -->
# Task Report: reframe + evaluate merged into ONE structured toolcall — `task(action=reframe)`

**Branch**: `feat/reframe-verdict` (worktree `/home/user/workspace/gopi-reframe-verdict`)
**Spec**: `briefings/SPEC-reframe-evaluate-merge.md` (nick 2026-09-10, 四档→三档 + POC)
**Task**: `01a088ef-c0d2-785e-8b63-d2357ca55c6a`

---

## Completed

### 1. Schema — `task(action=reframe)` (`internal/agent/loop.go`, `internal/toolnames`)

The `task` tool gained a fourth action beside view/commit/review:

```
task(action=reframe,
     epic_summary, constraints, deliverables, acceptance,   ← the reframe output
     verdict: DOABLE | TOO_BIG | IMPOSSIBLE,                 ← exactly 3 (no NEEDS_RESEARCH)
     roadmap: [{goal, acceptance}…],                        ← REQUIRED when TOO_BIG
     reason: "…"                                            ← REQUIRED when IMPOSSIBLE
)
```

- Action name/wire value from `toolnames.ToolTaskReframe`; the verdict
  vocabulary lives in the new leaf package **`internal/taskverdict`**
  (`Doable`/`TooBig`/`Impossible`, `All()`, `Valid()`) — one exported
  definition, zero imports, consumer-side constants only (Rules 1b/13/14).
- `task_evaluate` is **RETIRED**: `toolnames.ToolTaskEvaluate` is deleted and
  the tool is gone from `claudeToolDefs` → **16 tools** (Rule 3), with
  `GOPI.md`'s count line and the `task_submit`/`task` routing text updated.
- The IMPOSSIBLE criterion is stated strictly in the directive/schema:
  *"PROVABLY unattainable in principle (hard/risky/unknown is TOO_BIG, with the
  investigation as a roadmap item)"*.
- NO `NEEDS_RESEARCH` anywhere — research is a roadmap item, not an exit.

### 2. Harness hard validation (`validateReframe`, `parseReframeInput`)

Not prompt-only — the gate rejects the bureaucracy's zero-cost answers:

| payload | result |
|---|---|
| `TOO_BIG` with no/empty/incomplete roadmap (each item needs goal **and** acceptance) | **REJECTED**, error names `roadmap` |
| `IMPOSSIBLE` with no reason | **REJECTED**, error names `reason` |
| empty / whitespace / unknown verdict (incl. `NEEDS_RESEARCH`) | **REJECTED** |
| missing epic_summary / constraints / deliverables / acceptance | **REJECTED** |

Applied both pre-dispatch (`validateToolArgs`) and in the handler; a rejected
call has **zero side effects** (no swap, no verdict latch, no enqueue —
asserted). Verdict normalization is shape-only: `"  doable "` validates, a
wrong-shaped roadmap parses empty and is then rejected.

### 3. Dispatch semantics (`handleTaskReframe` → `dispatchReframe*`)

- **DOABLE** → the harness tells the model to proceed; verdict latched;
  telemetry recorded; the reframed epic becomes the review anchor.
- **TOO_BIG** → the roadmap is **auto-submitted as real child tasks**
  (parent-linked, `source=auto`, tag `reframe-roadmap`, payload = goal +
  acceptance so the child stands alone as a briefing) and the run **exits
  honorably** — it must NOT start the children itself. Value = the due
  diligence + the decomposition.
- **IMPOSSIBLE** → the ledger row is marked **failed** with the model's reason
  (`TaskUpdateReason`, event reason `reframe verdict=IMPOSSIBLE`) and
  `reframeHonestExit` latches so the run's own end hook cannot overwrite it
  with `completed` (`taskregister.go`).
- A **repeat** reframe in one run is recorded as telemetry but never
  re-dispatched — the FIRST verdict stands (duplicate children would be a
  real defect). This is the loop's own signature, made observable.

### 4. Free-text flow retired (`internal/agent/reframe.go`)

`[REF RAMED EPIC]` block + `extractReframedEpic` + `maybeApplyReframe` are
gone. The reframe directive is now appended to the `[EPIC GOAL]` framing as a
toolcall instruction; `applyReframe` swaps the raw briefing for the compact
reframed epic via the tool result (with an archive pointer). The nudge
backstop survives, re-pointed at the toolcall, and refuses below
`reframeMinRawLength` (derived from the directive's own cost) so a small
briefing is never destroyed by a swap.

### 5. Telemetry (`internal/tools/reframe.go`, CLI `gopi task verdicts`)

Every reframe lands one append-only `task_events` row (`event=reframe`,
detail = verdict + item count). `TaskReframeStats()` renders the verdict
distribution with the total roadmap item count per verdict **plus per-task
reframe counts** — the two observable facts: a distribution that is not 100%
DOABLE proves honest exits happen, and a count > 1 is the bureaucracy loop's
visible symptom. New CLI verb `gopi task verdicts` (+ completion + arg
parsing). `gopi task show` now renders a row's roadmap `children:` block
(`TaskChildren`, roadmap order).

### 6. System prompt (zero-loophole, density gate held)

```
- task(action=reframe): judge AFTER investigating — DOABLE: do it now;
  TOO_BIG: break down + roadmap (harness auto-submits), never dodge;
  IMPOSSIBLE: provably unattainable only, with reason. task_submit: an
  independent work item.
```

Assembled prompt = **4478 chars** (< the 4500 density gate); trimming hit
redundancy only (no pinned assertion string was weakened).

## Not Done

- **Telemetry rows count a repeat reframe as its own record** by design
  (append-only; the per-task count is the signal) — no dedup/normalization
  layer.
- `IMPOSSIBLE` does **not** abort the run loop; the model finishes its own
  summary. The ledger already says failed and the end hook is latched, so a
  mid-flight abort was judged unnecessary risk to the writeback path.
- The E2E scenarios were run in/against a **sandbox** (`GOPI_HOME=/tmp/...`)
  and the sandbox repos are throwaway; the two runs that did real work created
  worktrees in the main repo as a side effect — both were inspected and
  **removed** (see Verification).
- POC §6's honest limitation (naked-prompt verdict instability) is only
  partially re-tested here: 3 scenarios, not a distribution study.

## Verification

**Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK

**Unit/integration** (final gate, `go test ./...` with the build):
`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **BUILD OK**;
`go test ./... | grep -E "^(FAIL|--- FAIL)"` → **no output** (zero failures
across every package, including `cmd/agent`, `internal/agent`,
`internal/tools`, `testsuite`).

New tests:
- `internal/agent/reframe_test.go` — directive wiring into the EPIC framing;
  free-text protocol gone; **all validation gates** (incl. TOO_BIG-without-
  roadmap and IMPOSSIBLE-without-reason); DOABLE continues; TOO_BIG enqueues
  real children (pending, parent-linked, tagged, standalone payload) + closes
  the run; IMPOSSIBLE marks the row failed + records the reason; invalid call
  leaves no state; repeat is recorded not re-dispatched; real `Run()` briefing
  swap (request 2 loses the giant, carries the reframed goal); small briefing
  never swapped; nudge fires once / never for small / never when reframed /
  never re-injected on resume; swap-by-prefix after a memory block.
- `internal/agent/reframe_gate_test.go` — Rule 13 grep gates: the retired wire
  name `"task_evaluate"` and the identifier `ToolTaskEvaluate` appear in no
  production source (single archival comment site allowed); `NEEDS_RESEARCH`
  appears nowhere in agent/toolnames production sources; the schema carries
  the reframe contract (all 3 verdicts documented, all 6 parameters present,
  roadmap items carry both goal and acceptance).
- `internal/tools/reframe_test.go` — telemetry record/stats (distribution,
  item sums, most-reframed-first ordering), garbage rejection (unknown verdict
  / empty task id), malformed detail bucketed as UNKNOWN; `TaskChildren`
  order + claimability.

**Rule 11 — real-task E2E (LLM actually calls the tool)**

Real `gopi` runs against a sandboxed `GOPI_HOME`, DeepSeek live, session JSONL
tool_use evidence:

| # | scenario | observed `task` call | harness consequence |
|---|---|---|---|
| 1 | *"zero hallucination / 100% factual accuracy for ANY prompt"* (5.3 KB briefing) | round 42: `action=reframe`, `verdict=IMPOSSIBLE`, **no roadmap**, reason = clause-by-clause mechanism (unverifiable universal / undecidable truth / future-contingent) | verdict + reason returned verbatim in the tool result; no children |
| 2 | *"rewrite the entire gopi Go→Rust, full parity"* (5.3 KB) | `action=reframe`, `verdict=TOO_BIG`, **roadmap = 10 items** | **10 real child tasks auto-enqueued**, all `pending`/claimable, parent-linked, `tags=reframe-roadmap`; the run closed honorably |
| 3 | same epic launched through the **real ledger** (`gopi task run <id>`, row `in_progress`) | `action=reframe`, `verdict=TOO_BIG`, 13-item roadmap | ledger went **1 → 14 rows**: 13 children `pending` under parent `01a088f9-2ab8-…` |

Telemetry read back from run 2's ledger:

```
$ gopi task verdicts
1 reframe record(s) across 1 task(s)
VERDICT       COUNT  ITEMS
TOO_BIG           1     10
per task:
  01a088f8-f1cb-… ×1 (TOO_BIG)
```

Evidence paths (session JSONL, tool_use + tool_result pairs):
`/tmp/e2e-reframe/w1/home/.gopi/sessions/42b98d59-…/session.jsonl` (run 1),
`/tmp/e2e-reframe/w2/home/.gopi/sessions/*/session.jsonl` (run 2),
`/tmp/e2e-reframe/w3/home/.gopi/sessions/*/session.jsonl` (run 3).

**E2E side-effect cleanup (no main-repo contamination)**: run 1 created
worktree `~/workspace/gopi-zero-halluc` (branch `docs/zero-hallucination-impossible`,
commit `d6544bd`) and run 3 created `~/workspace/gopi-rust-port-roadmap`
(`docs/rust-port-roadmap`, `5c36646`) — both were the agents doing real work,
not harness artifacts. Both worktrees were removed and both branches deleted;
`git worktree list` in the main repo now shows only `master` +
`feat/reframe-verdict`, and the main repo's only untracked file is the
pre-existing `tasks/legacy-t-rows-export.tsv`.

## Notes / incidents

- **Cancelled ≠ killed**: a *cancelled* task's gopi process was still alive and
  writing into this worktree (same path, different session). It was identified
  via `/proc/<pid>/cwd` + `GOPI_TASK_WRAPPER_ID`, killed, and its partial work
  reviewed — it built and tested clean and was kept as the implementation base.
  Lesson stored in memory (id 1577).
- The density gate is real: adding the reframe routing line pushed the prompt
  to 4572 chars, so trims were made in redundant prose only, keeping every
  substring the prompt tests pin.

## Independent verification (second pass, 2026-09-10)

Re-verification from a fresh worktree on a clean checkout of `2db78eb`
(no working-tree changes carried over — `git status` empty):

- **Schema/constants** — `internal/taskverdict/verdict.go` holds the three
  verdicts (`Doable`/`TooBig`/`Impossible`) + `All()`/`Valid()`; the wire
  vocabulary has ONE definition site (Rules 1b/13/14).
- **Validation** — direct call of the gates (`TestValidateReframe_AllGates`,
  `TestValidateToolArgsReframeGates`) plus the E2E: a `TOO_BIG` with no
  roadmap, an `IMPOSSIBLE` with no reason, and an empty verdict are all
  rejected with no state written.
- **NO fourth verdict** — `grep -rn NEEDS_RESEARCH internal/ cmd/` in
  production sources returns only the explanatory comment at the
  `taskverdict` definition; no comparison, no schema entry.
- **Retirement** — `grep -o '"task_evaluate"'` over production `.go` finds
  only the archival comment in `internal/toolnames/names.go`; the literal is
  otherwise confined to the gate test that forbids it.
- **Density** — `TestSystemPromptDensity` passes; the prompt is compacted
  below the 4500 gate (intermediate 4572 → final 4478).
- **Rule 11 E2E re-read from raw JSONL** (not from this report):
  `/tmp/e2e-reframe/w1/.../42b98d59-.../session.jsonl` → `task` tool_use with
  `action=reframe`, `verdict=IMPOSSIBLE`, `reason` 1844 chars, **no roadmap
  key**; `w2/.../1fc6a287-.../session.jsonl` → `verdict=TOO_BIG`, roadmap 10
  items, no reason; `w3` (real ledger run) → `verdict=TOO_BIG`, 13 items.
- **Enqueue consequence, counted on the ledger**: `GOPI_HOME=…/w2 gopi task
  list` shows 11 rows — the parent `01a088f8-f1cb-…` completed plus **10
  pending children** `[1/10]…[10/10]`, parent-linked, `source=llm`, in roadmap
  order; `gopi task verdicts` reports `TOO_BIG 1 / ITEMS 10` (w2) and
  `TOO_BIG 1 / ITEMS 13` (w3).

## Rule-11 E2E on the MERGED master (`docs/reframe-verify-master` pass)

The three original E2E runs used a binary built from the working tree
**before** `2db78eb` was committed, so the evidence was re-established
against a binary built from the merged master tip `3ce88c2`
(`git worktree add --detach … 3ce88c2`; `CGO_ENABLED=0 go build -o bin/gopi
./cmd/agent/` → OK).

Run: fresh `GOPI_HOME` + empty sandbox git repo, the same TOO_BIG scenario
(Rust port epic) piped on stdin. Session JSONL
`/tmp/e2e-mv/w4/home/.gopi/sessions/16c389a5-47c3-4646-935f-804098c94b14/session.jsonl`
contains two `task` tool_use blocks with `action=reframe` — one of each
harness behaviour in a single real run:

1. **Validation gate fires** — `call_00_xdH79hz3lLswe3IqDEDU4589` omitted
   `constraints`; the tool result is
   `task action=reframe: missing required parameter: constraints`, and no
   ledger row was created (the model then re-issued the call with the field).
2. **Valid `TOO_BIG` dispatches** — `call_00_U8bGxhZCpf1QQLj7RfRd8869`:
   `verdict=TOO_BIG`, `roadmap` = 14 items, **no `reason` key**; the tool
   result reads *"VERDICT TOO_BIG: the harness enqueued 14 child task(s) from
   your roadmap — this epic is CLOSED here (do not start the children
   yourself; they run in fresh sessions…)"*.

Ledger consequence, counted directly: `gopi task list` → **15 rows** — the
parent `01a08914-2121-78fa-a29a-e4c8c576f208` plus 14 pending children
`[1/14]…[14/14]`, `parent_task_id` = the parent,
`tags: reframe-roadmap`, `source: llm`, all claimable.
`gopi task verdicts` → `TOO_BIG 1 / ITEMS 14`.

Test suite on the merged master (`go test ./...`): **every reframe/verdict/
gate/prompt/tool test passes**; the run's only FAIL is
`TestWorkerBusyRoundResetsIdleStreak` (`cmd/agent`), and one package-level
**timeout** in `internal/tools` from a different test.

Both were chased to ground and both are **pre-existing flakiness, not
reframe regressions** — verified by A/B against the pre-merge tip `b684656`
in a second worktree (`~/workspace/gopi-baseline-b684656`):

- `TestWorkerBusyRoundResetsIdleStreak` is **flaky on BOTH trees at the same
  rate**: 10 reps on `b684656` → reps 1,3,4,5 PASS / rep 2 FAIL;
  10 reps on master `3ce88c2` → reps 1,2,3,4 PASS / rep 5 FAIL. The failure
  is a wall-clock assertion, not a logic break — the busy round resets the
  streak correctly but the goroutine's 1.5 s submit can land after round 1's
  drained verdict, so `(1/3 idle rounds)` prints once instead of twice
  (`worker: claimed t-busy-reset-1` appears BEFORE the `queue drained (1/3)`
  line). A timing-sensitive test predating this epic (`ba3ce54`).
- `internal/tools` FAIL in the aggregate run is a **10 m package timeout**
  from `TestReproServeClaimsEmptyPayloadRow` (`panic: test timed out after
  10m0s` → real `t.TempDir` SQLite work), *not* this epic — `internal/tools/
  -run TestReproServeClaimsEmptyPayloadRow` alone passes in ~1 s **on both**
  `b684656` and master, and the reframe telemetry tests in that package pass
  in every run. The aggregate run's 400 s+ wall time comes from the harness's
  task-session/subprocess tests sharing one process under load.

Both pre-date the reframe epic (`git diff b684656 3ce88c2` touches
`internal/tools/tasks.go` only by adding `TaskTags` + the `children:` render
block — neither is on the claim/run path).

## Final closure on master `2516e09` (docs reconciliation)

`feat/reframe-verdict` was merged as `3ce88c2`; master has since advanced
(`2516e09`, the hippocampus epic reframe report — *not* this epic). This
branch was re-based onto it by an ordinary merge commit `d8e20f8` so the two
reports (this one and `devdoc/hippocampus-epic-reframe.md`, byte-identical in
both trees) no longer collide; no conflicts, no code changes.

**What this pass re-confirms (unchanged from the merge-commit pass):**

- **Build on master**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- **Reframe code on master is byte-identical to the verified branch** — the
  merge was conflict-free, and every reframe/verdict/gate/prompt/tool test
  passes on the merged tree.
- **Rule-11 E2E on a binary built from the merged tip `3ce88c2`** (this was
  the one real audit gap of the first pass: those runs used a pre-commit
  working-tree binary) → same binary rebuilt on `2516e09`+`d8e20f8`; evidence
  unchanged in shape: one `task(action=reframe)` rejected by validation
  (missing `constraints` → no ledger row) and one `TOO_BIG` with a 14-item
  roadmap → 14 pending children on the ledger, `gopi task verdicts` =
  `TOO_BIG 1 / ITEMS 14`.
- **The two non-reframe failures are pre-existing flakiness**, proven by A/B
  against `b684656` (see above): the worker idle-streak test flakes on both
  trees, and the `internal/tools` aggregate FAIL is a 10-minute package
  timeout from `TestReproServeClaimsEmptyPayloadRow` that passes standalone
  (~1 s) on both trees.

**Honest gaps (not done, do not read as complete):**

- The two flaky tests are **not fixed** here — out of this epic's scope, and
  they need their own task (a wall-clock race in the worker streak test; a
  shared-process timeout in `internal/tools`).
- No distribution study of naked-prompt verdict stability (3 scenarios, not N).
- The `IMPOSSIBLE` path still lets the model write its own closing summary
  instead of aborting the loop (deliberate — see Not Done).

## Commit

Deliverable lives on **`feat/reframe-verdict`**, merged into master as
`3ce88c2`, with the documentation reconciled by
**`docs/reframe-verify-master`** (`3324ab2` → `26b86a1` → merge `d8e20f8`).
The implementation commit message is
`feat(reframe): merge reframe+evaluate into task(action=reframe) — 3 verdicts, retire task_evaluate`.

Recovery commands (the deliverable is in the MAIN repo — a worktree shares
its refs, so nothing was lost when the worktrees were removed):

```bash
git show 2db78eb:devdoc/reframe-evaluate-merge.md          # this report (implementation commit)
git show docs/reframe-verify-master:devdoc/reframe-evaluate-merge.md  # closure pass
git show docs/reframe-verify-master --stat                 # the two docs commits
```
