<!-- doc-class: normative -->
# Task Report: attachment-index empty `replica_rel_path` contract (two read faces)

Branch: `fix/attachment-index-empty-rel` (worktree `~/workspace/gopi-attach-index-contract`).

## The divergence (root cause)

Two read faces of the SAME manifest used opposite policies for an entry whose
`replica_rel_path` is empty:

| face | reader | pre-fix behaviour on an empty rel |
|---|---|---|
| run-time EPIC index (`[attachmentN:]`) | `TaskDeliverableAttachments` → `taskDeliverableAttachmentIndex` | **dropped** the entry (`continue`) → index shorter than the manifest |
| `task show` | `taskShowWithDB` | paired `atts[i]` with `idx[i]` **positionally**, and for the tail rendered an empty string |

With a 3-entry manifest whose entry 2 has `replica_rel_path ""`, the index had
2 elements: `task show` printed `attachment2: <entry3-replica>` and
`attachment3:` (empty) — i.e. **every later replica was shifted one slot and
mislabelled**, and the briefing's numbering (`1..N` over the shortened list)
disagreed with `task show` for the same row. The tail rendered an empty string
rather than failing: a Rule 15b silent failure.

Root cause (5-why): manifest writers always stamp a non-empty rel
(`TaskCopyAttachments` validates `filepath.IsAbs` BEFORE copying, and
`taskAttachmentRelPath` returns `""` only for a non-absolute source) → an empty
rel can only come from a corrupt/cross-version blob → the index reader treated
"unresolvable" as "absent" and shortened the list → positional pairing in the
second face then silently mislabelled.

## Completed — the ONE contract

**Contract (attachment-index positional contract): the attachment index keeps
one slot per manifest entry, in manifest order; `len(idx) == len(manifest)`
ALWAYS. An entry with an empty/whitespace `replica_rel_path` is NOT dropped —
it occupies its slot and renders the explicit sentinel
`TaskAttachmentReplicaMissingLabel`
(`internal/tools/task_session.go`: `"(no replica: empty replica_rel_path in manifest)"`).**

Why keep-and-label beats drop (the decision, documented at the constant):

- dropping is an *order-preserving* operation on a list that a **second face
  consumes positionally** — the shortening itself is the mislabelling bug;
- the briefing numbers `[attachmentN:]` from this list, so a shortened list
  makes `task show` and the briefing contradict each other for the same row;
- an empty rel is not "no attachment" (the slot exists, with a source path and
  a digest) — rendering `""` loses that information silently;
- the sentinel is visible to the LLM reading the briefing (it can tell the
  replica is missing instead of reading a neighbour's file — the guidance in
  `internal/config/help.go` tells the child to read the `[attachmentN:]` path).

### Code changes (Rule 4 — surgical)

1. `internal/tools/task_session.go`
   - new **exported** const `TaskAttachmentReplicaMissingLabel` (Rule 14: the
     value is cross-package — `cmd/agent`'s briefing renderer must be able to
     recognise the sentinel without mirroring the string) + the contract
     rationale;
   - `taskDeliverableAttachmentIndex`: the `rel == ""` branch now APPENDS a
     position-preserving placeholder instead of `continue`ing, and its doc
     states the length contract;
   - `TaskDeliverableAttachments`: doc states `len(replicaPaths) == len(atts)`;
   - `TaskAttachment.ReplicaRelPath` field doc points at the contract.
2. `internal/tools/tasks.go` — `taskShowWithDB`'s attachment render:
   - the positional pairing is now contract-backed (`aligned := len(idx) ==
     len(atts)`) and the per-line default is the sentinel, so a *future*
     shortened index renders the explicit missing label instead of silently
     pairing a neighbour's replica (or an empty string) with this entry.

`cmd/agent/cli_tasks.go` (the `[attachmentN:]` renderer, `runTaskBriefing`)
needed **no production change**: it already numbers `i+1` over
`replicaPaths` — the point of the fix is that this list now has one element
per manifest entry, so its numbering matches `task show` by construction. A
new `cmd/agent` regression test drives that renderer end-to-end.

Rule 1b/13: no new bare `"attachments"` string literal was introduced — the
sentinel is a named const, `taskAttachmentsDirName` is untouched, and the
existing `config.SessionsDirName` / consts are referenced as before.

## Not Done

- No new tool, no CLI flag, no schema change: the contract is a read-face
  policy, so the manifest shape (`replica_rel_path`) is unchanged.
- The face pair is NOT unified into one function (the two faces deliberately
  read different sources — the blob alone for `task show`, the ledger row for
  the briefing). The contract is asserted by test rather than by a shared
  code path; a refactor to a single resolver is left as a possible follow-up
  (it would be a larger, non-minimal change than this bug fix warrants).
- The briefing's rendered `[attachmentN:]` block IS covered directly —
  `cmd/agent/cli_tasks_attach_index_test.go` calls the real
  `taskRunBriefing` and asserts the exact `[attachmentN: <path>]` lines, then
  cross-checks them against `tools.TaskShow` for the same row.

## Verification

- **Red → green (the regression test is real).** Temporarily restoring the
  pre-fix index branch (removing the placeholder append, leaving `continue`)
  makes the new test fail exactly as described:

  ```
  --- RED RUN (pre-fix index behaviour restored) ---
  --- FAIL: TestAttIdxCtrEmptyMiddleReplicaRel (0.08s)
      attach_index_contract_test.go:151: replicaPaths = 2, want 3 (one per
      manifest entry — an empty replica_rel_path must not shorten the index)
  FAIL	github.com/nick/gopi/internal/tools	0.088s
  ```

  With the fix in place both new tests pass:

  ```
  === NEW internal/tools test ===
  --- PASS: TestAttIdxCtrEmptyMiddleReplicaRel (0.05s)
  ok  	github.com/nick/gopi/internal/tools	0.055s
  === NEW cmd/agent test ===
  --- PASS: TestAttIdxBriefFacesAgreeOnEmptyMiddleEntry (2.52s)
  ok  	github.com/nick/gopi/cmd/agent	2.527s
  ```

- `go build ./...` → `BUILD_OK`
- `go test ./internal/tools/...` → `ok github.com/nick/gopi/internal/tools
  190.238s` (run 1, job-2) / `ok … 201.055s` (run 2, job-1).
  A third full run (job-5) reported `ok` for `./cmd/agent/...` (250.263s) and
  failed `./internal/tools/...` on ONE test only —
  `TestFreshDBOpenCloseIsFast` (12 fresh DB opens in 342.9ms vs a 300ms
  budget, message blames a stalled fsync on the `/dev/shm` scratch root).
  That test is a load/timing budget, unrelated to attachments: it PASSES in
  isolation on this branch (0.17s) and on the untouched base
  (`/tmp/master-base`, 0.239s). Not caused by this change.
- `go test ./cmd/agent/...` → `ok github.com/nick/gopi/cmd/agent 250.263s`
  (job-5; also `ok … 279.242s` in job-1).
- **Final confirmation run (branch tip 212703c, `-count=1`)**: `go build ./...`
  → `BUILD_OK`; `go test ./internal/tools/...` → `ok
  github.com/nick/gopi/internal/tools 193.455s`; `go test ./cmd/agent/...` →
  `ok github.com/nick/gopi/cmd/agent 262.945s` (no failures — this run's
  `TestFreshDBOpenCloseIsFast` passed, confirming the earlier single failure
  was the fsync-timing flake, not this change).
- **Red→green re-confirmed on the committed tree** (the documented recipe is
  reproducible by a later agent): replacing ONLY the placeholder append
  (`idx = append(idx, TaskAttachmentIndex{Path: TaskAttachmentReplicaMissingLabel})`)
  with a no-op (`_ = TaskAttachmentReplicaMissingLabel`) and running
  `go test ./internal/tools/ -run 'TestAttIdxCtr|TestCovTaskDeliverableAttachments'`
  fails with

  ```
  --- FAIL: TestAttIdxCtrEmptyMiddleReplicaRel (0.05s)
      attach_index_contract_test.go:151: replicaPaths = 2, want 3 (one per
      manifest entry — an empty replica_rel_path must not shorten the index)
  ```

  and `git checkout -- internal/tools/task_session.go` restores green. The
  test genuinely depends on the contract line.
- Compatibility run: master's `TestCovTaskDeliverableAttachmentsRoundTrip` /
  `TestCovTaskCopyAttachmentsRoundTrip` (`internal/tools/cov_task_session_test.go`,
  which does not exist on this branch's base `040933b` and lands via a sibling
  branch) were checked against this change: the non-empty path is
  byte-identical (same `filepath.Join(sd, filepath.FromSlash(rel))`, same
  manifest order), so the round-trip — asserted over a REAL 1-entry manifest —
  is unaffected; the matching `len(gotAtts) != 1 || len(replicaPaths) != 1`
  assertion still holds.
- `gofmt -l internal/tools cmd/agent` → clean (empty).
- `go vet ./internal/tools/... ./cmd/agent/...` → clean.

## Regression tests (two faces)

1. `internal/tools/attach_index_contract_test.go` — helpers are
   `attIdxCtr*`-prefixed (task-unique per the merge-hazard constraint; sibling
   coverage slices on `internal/tools` use `covLife*`):
   - builds a REAL 3-entry manifest by copying two files through
     `TaskCopyAttachments` and hand-assembling
     `[real, {replica_rel_path: ""}, real]`, then seeds a terminal ledger row;
   - face 1: `TaskDeliverableAttachments` must return 3 entries and 3 replica
     paths — element 1 the explicit label, elements 0/2 the real ONE-join
     replica paths (and element 1 must be neither `""` nor the bare session dir);
   - face 2: `task show` must render `attachment1..3` with **identical**
     ordering/values to face 1 (no shift, no mislabel), each entry's `from:`
     line present, attachment2 must not carry a neighbour's replica;
   - the two real replicas must still be readable files.
2. `cmd/agent/cli_tasks_attach_index_test.go` — fixture id `t-attidx-brief`
   (sibling slices use `t-attach-brief`): the same 3-entry blob seeded into a
   real submitted row, then the REAL `taskRunBriefing` (the `[attachmentN:]`
   renderer the child receives) is compared against `tools.TaskShow` — same
   count (3) and same per-number path, middle entry = the contract label.

## Merge-hazard check (constraint: task-unique test helper names)

A sibling coverage slice adds `internal/tools/cov_task_session_test.go`
(`TestCovTaskDeliverableAttachmentsRoundTrip` etc.), so this branch's helpers
were prefix-isolated. Verified by an actual merge rehearsal:

- `git worktree add --detach ~/workspace/gopi-merge-sim master` +
  `git merge --no-ff fix/attachment-index-empty-rel` → **merged cleanly**
  (5 files, no conflict) — the two test files coexist with
  `cov_task_session_test.go`.
- helper-name scan across `internal/tools/*_test.go` / `cmd/agent/*_test.go`:
  `attIdxCtr*` / `attIdxBrief*` are defined ONLY by this branch
  (`git for-each-ref` + `git grep -l` over every local ref → only
  `fix/attachment-index-empty-rel`); fixture ids `t-attidx-contract` /
  `t-attidx-brief` collide with no sibling (`t-attach-brief`, `t-attach-live`,
  `t-attach-msg`).
- merged-tree runs: `TestAttIdxCtrEmptyMiddleReplicaRel` PASS,
  `TestCovTaskDeliverableAttachmentsRoundTrip` PASS,
  `TestCovTaskDeliverableAttachmentsBoundaries` PASS,
  `TestCovTaskCopyAttachmentsRoundTrip` PASS (0.142s);
  `TestAttIdxBriefFacesAgreeOnEmptyMiddleEntry` PASS (4.339s);
  `go vet ./internal/tools/... ./cmd/agent/...` clean on the merged tree.

One naming caveat for review: this branch's `cmd/agent` test defines a
package-level `mustJSON(t, v)`. No other `cmd/agent` test file defines that
name (only `internal/tools/gitblame.go` has an unrelated
`mustJSON(v interface{}) string` in a different package), so it neither
conflicts today nor shadows anything.

## Commit

```
212703c docs(attachment-index-contract): the briefing [attachmentN:] face IS covered directly
8cd10d8 fix(tools): one contract for an empty replica_rel_path — keep the attachment-index slot
```
