<!-- doc-class: historical -->
# Task Report: re-green `internal/tools` on master (6 reds, two root causes)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch: `task/01a0d685a2737c55921f39191ad07cc6` (worktree
`/home/user/workspace/gopi-01a0d685a2737c55921f39191ad07cc6`), based on master tip
`0d08f306` (delivery-point sync of `e007b2d4`).

The branch's own diff touched **zero lines under `internal/tools`** — every red
below reproduced on the merged master, so all six are pre-existing, and all six
were landed by commits from **today** (2026-09-25).

## Completed

### The measured red (before any change)

```
go test -count=1 -timeout 600s -v -run \
 'TestDeclaredTopologyWordingHasOneDefinition|TestFinishCleanupWorktreeHeldByCallerIsRecycled|\
TestSyncImportTwoProcessConcurrent|TestParentGateListingAgreesWithFunnel|\
TestTaskShowBriefingFromPayload|TestReconcileMirrorCloseout' ./internal/tools/
```
→ `11 RUN`, `6 FAIL` (a `tail -80` of the same command hides them; the verbose run
is the instrument). load average at the measurement: **4.56 3.77 4.03**.

| # | test(s) | classification | root cause | fix |
|---|---|---|---|---|
| 1 | `TestDeclaredTopologyWordingHasOneDefinition` | defect (duplicate source of truth) | `memory_sync_claim.go` spelled its own copy of the "not in the sync_peers whitelist" sentence **and** checked `len(peers)==0` first, so an undeclared `--peers` on a peerless node was reported as "no sync_peers declared" — the wrong diagnosis, and a second sentence for a fact `peer_topology.go` owns (Rule 14). | the block now **delegates** to `RequireDeclaredPeer` and asks it *before* the empty-list refusal |
| 2 | `TestFinishCleanupWorktreeHeldByCallerIsRecycled` | defect (re-entered veto) | `reclaimGroundChecklistVerdict`'s rung ① (`guestRowOwnCheckoutRefusal`) refuses when the **asker's own cwd** is inside the candidate ground. The finish-cleanup chain calls that rung on the run's **own** close-out, where its own cwd is by construction inside the ground it must free. | added `reclaimGroundChecklistVerdictForCloseout` (one body, one flag): the chain's two call sites ask the close-out form; the arms that free **somebody else's** ground (sweep, ledger-keyed reclaim) keep the unmodified rung |
| 3 | `TestSyncImportTwoProcessConcurrent` | **stale fixture** (the product gate is working) | `8ba90a29` ("merge re-worded twin lessons") lowered the lesson twin-merge gate to Jaccard > `lessonJaccardMerge` + a shared-trigram floor. The fixture built its 20 rows from ONE template differing only in an index → the store merged 19 of them (measured: `stored=1 deduped=19`, row count 2 of 21). The test's subject is the whole-patch **transaction** boundary; it was measuring the dedup gate instead. | each fixture row carries a distinct topic **and** a distinct retrieval surface |
| 4 | `TestParentGateListingAgreesWithFunnel` (and `TestPendingListRendersClaimOrder`) | defect in the **test's locator** | `renderedRowLine` located a row by *any occurrence of the id*, but a parked **child**'s tail carries its **parent**'s id (`[queue-skip] parent <id> not released`) — so asking for the parent returned the child's line, and the assertion read the child's own marker as the parent's. The ledger layer was right. | the locator matches the **ID COLUMN** (`fields[0]` of `#<id>  [status] …`), with `TestParentGateRowLocatorNamesTheIdColumn` as its regression |
| 5 | `TestTaskShowBriefingFromPayload` / `…FromSession` / `…MissingSession` | defect (renderer) + **stale fixtures** | `be17a9bd` derives a row's provenance from the process signature; `taskSubmittedBy` was `switch (t.Source)` over the **retired** spellings `llm`/`human`/`auto` with no default, so a `server`/`merger`/`agent` row rendered `—`. The fixtures further asserted a `human` provenance they never asked for — a claim that is derived, and an agent-session environment (`GOPI_AGENT_SESSION` in the runner) downgrades it. | the renderer switches on `internal/tasksource` constants (Rule 14; `Normalize` resolves both historical aliases); the fixtures **state** the role they mean; `internal/testutil.PinHumanTerminal` pins the one signal that makes a human claim survive (same class as `PinSuiteGopiProfile`) |
| 6 | `TestReconcileMirrorCloseout_{Exit0ClosesCompleted,NonZeroExit,SIGKILLClosesFailed}` | defect (dead predicate) | the mirror lookup added `and source = 'auto'` — a **second key** for a fact the `wrapper:<id>` **tag** already carries. Once writers stopped spelling the retired conflation, the arm matched nothing and the mirror row stayed `in_progress` while the wording read as if it had run. | the source predicate is gone: a historical row is selected exactly as it is identified |

Also landed: `worktree_self_position_test.go` now drives the **other direction**
(returning into the ground must re-measure, not answer from a released base); the
recycle E2E prints the child's captured stdout/stderr on failure (the only witness
of *why* a release did not happen — before this it cost a 4-minute re-run to learn
what the child had already printed).

## Not Done (honest)

`cmd/agent` **`TestManualRunRecyclesItsWorktree` is still RED** — reproduced and
diagnosed, not fixed here. The real `gopi worker` binary, run with its CWD inside
the worktree it must recycle, is refused twice (verbatim, from the child's own
output):

```
task t-recycle-e2e: own-worktree-release: own registered worktree kept: …/001/wt
  — younger than the 5m0s floor (12s) — a concurrent round may be creating it
worker: own-worktree-release: own worktree release refused: task/trecyclee2e
  (t-recycle-e2e) — guest-row-reconcile refusal (arm guestRowOwnReleaseRefusal):
  this process stands in the linked worktree …/001/wt, not in the main checkout
  …/001/repo of that repository — it does not release the worktree of its own row
```

and the wrapper's close-out still reports
`finish-cleanup: … done=[align-before-delivery,pre-merge,worktree]`.

Two distinct gates hold ground that is demonstrably free, and the path that
worked before is now a **series** of them. It is **not fixed here on purpose**:
the repair means changing the reclaim/close-out primitives master is actively
reworking today (`TaskReconcileMirrorCloseout`, `ReleaseOwnWorktrees`, the
checklist rung) — a rebase conflict and a second owner for the same refusal
(Rule 14). It is **dispatchable and named** for its own task: the acceptance is
this test, the file is `internal/tools/guest_row_guard.go`
(`guestRowOwnReleaseRefusal`) plus the 5-minute floor on the own-registered
release pass, and the child's captured output (now printed by the test) is the
reproduction.

Also not done: `cmd/agent` and `internal/agent` were **not** run as whole packages
(budget — Rule 27's smoke rule), only the one branch-scoped E2E above.

## Verification

- Build: `go build ./...` → OK (`BUILD_OK`); `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (`BIN_OK`)
- Tests: `go test -count=1 -timeout 600s -run '<the six>' ./internal/tools/` → **`ok github.com/nick/gopi/internal/tools 27.041s`**, 0 FAIL (all six PASS, plus `TestTaskShowBriefing{FromSession,Truncated,MissingSession}`, `TestSubmittedByProvenance`, `TestTaskShowModelFromSession`)
- Ran: the acceptance command, verbatim —
  `go test -count=1 -timeout 600s -run 'TestDeclaredTopologyWordingHasOneDefinition|TestFinishCleanupWorktreeHeldByCallerIsRecycled|TestSyncImportTwoProcessConcurrent|TestParentGateListingAgreesWithFunnel|TestTaskShowBriefingFromPayload|TestReconcileMirrorCloseout|TestTaskShowBriefing|TestSubmittedByProvenance' ./internal/tools/`
- Result: `ok github.com/nick/gopi/internal/tools 17.831s`, 0 FAIL (re-run at report
  time: `11.802s`, 0 FAIL; the `-v` run of the same selection is **21 RUN, 21 PASS**).
- **The red side, re-measured on master's own tree** (`git worktree add --detach
  <tmp> $(git rev-parse master)` → `3b191dbf`, the same command, temp worktree
  removed immediately): **10 FAIL lines** —
  `TestSyncImportTwoProcessConcurrent`, `TestDeclaredTopologyWordingHasOneDefinition`,
  `TestFinishCleanupWorktreeHeldByCallerIsRecycled`, `TestParentGateListingAgreesWithFunnel`,
  `TestTaskShowBriefing{FromPayload,FromSession,MissingSession}`,
  `TestReconcileMirrorCloseout_{Exit0ClosesCompleted,NonZeroExit,SIGKILLClosesFailed}`
  → `FAIL github.com/nick/gopi/internal/tools 9.990s`. Red→green on one command.
- **Correction of a figure this report carried earlier**: "6 FAIL / 11 RUN" was an
  earlier *partial* selection whose selector list was never recorded; the number of
  failing **tests** under the acceptance selection is **10** (the "six" counts the
  six root-cause entries in the table above, three of which are multi-case). Measured
  figures replace the unverified one.
  The surface itself, end to end, was also driven for real: the built binary
  (`bin/gopi`) ran `worker <task>` inside a real worktree (`bin/gopi task submit …
  --repo <repo>` then `bin/gopi worker t-recycle-e2e` with the CWD in `wt`), and
  this deployment ran the full `internal/tools` package as the regression guard.
- Branch-scoped regression guard (the surfaces this diff touched), run after the
  acceptance run:
  `go test -count=1 -timeout 900s -run 'TestReconcileMirrorCloseout|TestTaskShow|TestSubmittedByProvenance|TestDeclaredTopology|TestPeerTopology|TestFinishCleanup|TestParentGate|TestPendingList|TestSyncImport|TestSelfPosition|TestWorktreeSelf|TestReclaim' ./internal/tools/`
  → `GUARD_RC=0`, `ok github.com/nick/gopi/internal/tools 79.367s`, 0 FAIL
- `go test -count=1 -timeout 300s ./internal/tasksource/` → **ok** (0.004s) — the
  only other package this diff touches (the exported-constant revert left it
  byte-identical to master, confirmed by `git diff`).
- Load average at the measurements: **2.56 2.94 3.66** (five-minute figures ranged
  2.56–4.96 across the session; Rule 27 — an existing fleet load, nothing generated
  by this run). NOTE (honest, Rule 27): TWO concurrent `go test ./internal/tools`
  runs by other sessions were live during this session (pids 1371609, 1387480,
  `-timeout 25m`); the full-package run was therefore killed rather than reported,
  and the branch-scoped guard above is the measurement that stands.
- Commit: `4f2255ac` (the fix), `4659c8cb` + `9335a056` (this report)

## Evidence (self-filed)

- `go test … -run '<the eight selectors>' ./internal/tools/` (verbose) → **21 RUN / 21 PASS** on this branch; the SAME command on master's own detached worktree (`3b191dbf`) → **`--- FAIL:` ×10**, `FAIL github.com/nick/gopi/internal/tools 9.990s` (load 5.60 3.90 3.88)
- `git diff master...HEAD --stat` → **empty** (the branch touched no code before this commit; the reds are master's)
- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && go test -run TestManualRunRecyclesItsWorktree ./cmd/agent/` → `--- FAIL: TestManualRunRecyclesItsWorktree`, message `manual run left its worktree behind (…/001/wt exists)`, with the two refusal lines quoted in **Not Done** — the observation, not an inference
- `go test -count=1 -run 'TestSyncImportTwoProcessConcurrent' ./internal/tools/` child output → `stored=1 deduped=19` and `row count after two concurrent imports: got 2, want 21` — the dedup gate, named by its own numbers
