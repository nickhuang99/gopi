<!-- doc-class: historical -->
# Task Report: the task_submit chain no longer parks its own head, and "independent" is spellable

**Task**: `01a12e34-049f-7215-9424-39615cb0eede` (epic, Rule 28 system-layer scope on peerbox's terms; run on hosta)
**Branch**: `task/01a12e34049f7215942439615cb0eede`

## The defect (measured, task `01a12e06`'s triage run, 2026-10-12)

Three INDEPENDENT bug rows were submitted from one session. All three went out
`[queue-skip]` — not one was claimable by any dispatcher:

```
#01a12e30-fc36 … [queue-skip] parent 01a12e30-f209-7efd-80ec-0185febaca9c not released (status=pending; …)
#01a12e30-f209 … [queue-skip] parent 01a12e30-e32e-7d61-b96e-cbee5766a504 not released (status=pending; …)
#01a12e30-e32e … [queue-skip] parent 01a12e06-d42a-7a63-9128-d7478d73aca8 not released (status=in_progress; …)
```

Two mechanisms, and the repair had to delete both:

1. **The head of every session's chain was parked on the submitter itself.**
   The chain had no first link of its own: with `lastSubmitRowID` empty, the
   first auto-linked submit fell back to `a.autoTaskRowID` — the row the
   submitter is, by construction, still `in_progress` on. Item *i* hanging off
   item *i-1 then propagated that park to the whole fan.
2. **"These items are INDEPENDENT" had no spelling.** The only opt-out was an
   explicit `parent_task_id`, which a caller with nothing to point at cannot
   supply — and the alternative repair, one `gopi task update <id>
   --clear-parent` per row, is a manual write per row.

## Completed

- **`internal/agent/task_submit_chain.go`** — the chain rule now has ONE
  definition, `taskSubmitResolveChain(asked, lastSubmitRowID)`, returning a
  `taskSubmitChainDecision{chainLink, independent, parentID}`:
  - the **independence sentinel** (`taskSubmitIndependentSentinel = "independent"`,
    matched case-insensitively because it is a WORD, not an id) → **parentless**;
  - an explicit id → honoured **verbatim**;
  - omitted + a previous submission → chained to it;
  - omitted + **no** previous submission → **parentless** (the repair: nothing
    can reach `autoTaskRowID` any more).
- **Every note and every model-facing sentence names the release set through
  `tools.TaskParentReleaseSetLabel()`** — the same reader the gate itself renders
  with (Rule 14). The old wording said "completed/verified" while the gate also
  releases on `superseded`; a test now fails if the hand-spelled form returns.
- **`internal/tools/tasks.go`** — `tools.TaskParentReleaseSetLabel()` added, the
  exported face of the existing unexported label (one definition, Rule 14).
- **`internal/agent/loop.go`** — the `task_submit` handler calls
  `taskSubmitResolveChain` (its inline fallback to `a.autoTaskRowID` deleted);
  the `parent_task_id` parameter description and the tool description teach both
  new behaviours, rendered from their constants (Rule 13).

**Rule 33 is untouched.** The release set, the predicate `taskRowClaimable`/
`taskParentGateDenied`, the dangling-parent refusal and the absence of any
override/staleness release are the same code, unchanged. What changed is *which
row a submission points at*, never *when a row is released*.

## Not Done (honest)

- **No CLI flag was added** (deliberate). The epic's deliverable is a spelling
  the MODEL can use on the LLM tool path; `gopi task submit --parent-task-id
  independent` would reach `TaskSubmit` directly and be refused as a dangling
  parent. A CLI-side opt-out would need its own arm in `cmd/agent/cli_tasks.go`;
  that is a separate surface, and inventing it here would have put the same word
  in two places (Rule 14). If the operator wants it, it is a one-line parse arm.
- **`reframe`'s roadmap chain still hangs item 1 off the epic row**
  (`dispatchReframeRoadmap`). That is NOT the self-link this epic is about: the
  reframe row is being CLOSED by that same call (its verdict is written as
  TOO_BIG and the run ends), so children parked on it release as soon as the
  run's status write lands — the semantics differ from a live triage session,
  where the submitter row is genuinely still running. Left unchanged on purpose;
  flagged here so the difference is a decision and not an oversight.
- The session-budget gate's own budget was consumed by the red-proof detour
  above; the touched-package budget run is reported below as far as it got.

## Verification

- **Build**: `go build ./...` → OK (exit 0, no output). Binary for the real run:
  `CGO_ENABLED=0 go build -o /home/user/workspace/gopi-scratch-01a12e34/gopi ./cmd/agent/` → OK.
- **Tests**: `make test-budget PKGS='./internal/agent ./internal/tools'` →
  **4370 executed, 12 FAIL — NONE in this diff** (full stream
  `/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791780195-702292.json`).
  Each is reproduced at this branch's OWN BASE `8f4e445d2` with no diff at all
  (probe worktree, `…/evidence/preexisting-reds.txt`), so both families are
  pre-existing on master:

  | failing test | at my base | verdict |
  |---|---|---|
  | `TestSharedIntegrationRefGuard_*` (10 cases) | FAIL, same 2 root assertions | pre-existing; environment (`git` refuses `fetch` into a branch another worktree holds — the message names `/dev/shm/suite-tools-run-*/…/trunk-checkout`) |
  | `TestDocAnchorsResolveInNormativeDocs` | FAIL (271 permitted, **9 new**, 6 stale) | pre-existing — see below |
  | `TestAnchor`, `TestPublishedAnchorResolves` | FAIL | pre-existing (the docanchor corpus) |

  Verified by diffing the two runs' emitted entries: **no drift entry in my run
  names any file I touched** (`grep -c 'task_submit_chain\|task-submit-chain'` = 0).

- **Honest partial — the docanchor corpus.** `TestDocAnchorsResolveInNormativeDocs`
  is red **before and after** my change, but my `loop.go` edit (+7 net lines) does
  move it: base `9 new`, mine `24 new` / `23 stale permits`. Cause, measured from
  the run's own line-addressed diff: the drift **baseline
  `internal/agent/testdata/docanchor-baseline.tsv` records absolute `loop.go:NNNN`
  line numbers** for landed records, so ANY insertion above those sites re-flags
  them as stale/missing. This is not a defect my change introduced — it is the
  same mechanism that already made the gate red on pristine master — but the
  ratchet is charged to whoever touches `loop.go`, i.e. to me. Repairing it means
  regenerating the baseline (the census generator the test itself names) or
  re-inserting my handlers BELOW line ~1969, and neither is inside this epic; the
  alternative, hand-editing a generated artifact, is exactly what Rule 38 forbids.
  **Filed as a bug row instead of silenced** (Rule 41) — evidence
  `…/evidence/docanchor-{base,full}.txt`.
- **Ran** (Rule 31 — the changed surface, for real, on a sandbox ledger):

  ```
  GOPI_HOME=/home/user/workspace/gopi-sandbox/sandbox-01a12e34-3bb90690 \
    gopi tool sandbox create --label 01a12e34 --task-id 01a12e34-049f-…
  ```

  plus a REAL LLM run against a sandbox profile (`GOPI_HOME=<sandbox>`), driving
  the actual `task_submit` tool — evidence file
  `~/.gopi/sessions/01a12e34-049f-7215-9424-39615cb0eede/evidence/llm-run.txt`.

- **Result**: see `## Evidence (self-filed)`.

- **Rule 11 acceptance (a REAL LLM run used the new spelling).** The fixed binary
  was run against a sandbox profile (`GOPI_HOME=<sandbox>`), given a triage task
  that mixes an independent pair with a genuine dependency — and the model chose
  the spelling ITSELF, without the words "independent" or "parent_task_id" in the
  prompt. Its three calls, verbatim from the sandbox session jsonl
  (`…/evidence/llm-run-toolcalls.txt`):

  ```
  call_00_PcTZSP  summary="bug: gopi help 输出存在一行超出 88 列宽度预算"        parent_task_id=<OMITTED>
  call_01_rUbrjq  summary="bug: notify.sh 循环变量改名…测试恒红"                parent_task_id='independent'
  call_02_uUwKMu  summary="bug: worktree 释放 e2e 在 master 恒红（…复测）"      parent_task_id='01a12e6c-39c3-77d8-8ba9-a8c57cb71918'
  ```

  and the sandbox ledger's pending view afterwards
  (`…/evidence/sandbox-queue.txt`):

  ```
  #01a12e6c-6c00… [pending] bug: worktree 释放 e2e … [queue-skip] parent 01a12e6c-39c3-… not released (status=pending; releases on completed|superseded|verified)
  #01a12e6c-6beb… [pending] bug: notify.sh …            ← no marker: claimable now
  #01a12e6c-39c3… [pending] bug: gopi help … [next]     ← no marker: claimable now
  ```

  The first row is `[next]` and claimable, the sentinel row is claimable, the
  dependency row is parked naming its parent and the DERIVED release set. That is
  the epic's acceptance criterion observed end to end on a real ledger, by a real
  model, through the real tool.

## Evidence (self-filed)

- **RED**, `~/.gopi/sessions/01a12e34-049f-7215-9424-39615cb0eede/evidence/red-proof.txt`
  — at HEAD `8f4e445d2`, `go test ./internal/agent/ -run 'TestTaskSubmitChainRedProof'`:

  ```
  --- FAIL: TestTaskSubmitChainRedProof_FirstSubmitIsNeverParentedOnTheRunItsOwnRow
      the FIRST submission of a run has parent_task_id = "t-red-proof-own-row", want NONE …
  --- FAIL: TestTaskSubmitChainRedProof_IndependentSentinelYieldsAParentlessRow
      independent submit failed: task_submit error: task_submit: parent_task_id "independent" does not exist
  ```

  The red test (`internal/agent/task_submit_chain_red_test.go`) is self-contained
  on purpose: it references no helper the fix introduced, so it compiles and runs
  against BOTH trees — that is what makes the transition a witness.

- **GREEN**, `…/evidence/green-proof.txt` — after restoring the fix,
  `go test ./internal/agent/ -run 'TestTaskSubmitChain' -v`: 6/6 PASS
  (`ok github.com/nick/gopi/internal/agent 5.993s`).

- **Rule 30**: `git status --porcelain` in the worktree — reported in the final
  message; the four changed/added files are the deliverable.

## Files

| file | change |
|---|---|
| `internal/agent/task_submit_chain.go` | the rule, the sentinel, every note, the tool-description sentences |
| `internal/agent/loop.go` | handler uses the rule; `parent_task_id` + tool descriptions render from the constants |
| `internal/tools/tasks.go` | `TaskParentReleaseSetLabel()` (exported, one definition) |
| `internal/agent/task_submit_chain_test.go` | 4 regression tests (ledger-level, via the real tool handler) |
| `internal/agent/task_submit_chain_red_test.go` | the self-contained red-then-green witness |
