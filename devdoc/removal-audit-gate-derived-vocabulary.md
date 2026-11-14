<!-- doc-class: historical -->
# Task Report: the removal-audit gate DERIVES its emitter vocabulary instead of naming one literal

epic `01a12bacc4e578c298a4d7dea1a2dc89` · branch `task/01a12bacc4e578c298a4d7dea1a2dc89`

## Completed

- `internal/standards/removal_audit_gate_test.go` no longer hard-codes an
  emitter NAME. `removalAuditEmitBranch = "BranchDelete"` (and the sibling
  `removalAuditEmitWorktree = "WorktreeRemove"`) are gone; the vocabulary is
  derived at run time from `internal/removalaudit/audit.go` itself by
  `removalAuditDeriveVocabulary`, which classifies **every exported entry point
  by the `Record.Kind` it writes**:
  - `Record{Kind: KindX}` composite literals are read from the emitter's AST and
    resolved through its own `Kind`-typed constants (`removalAuditKindConstants`);
  - a delegation hop is followed to a fixpoint, so `BranchDelete` — which writes
    no record of its own and calls `BranchDeleteAt` — derives as a branch-delete
    emitter **alongside** its same-kind sibling;
  - an exported entry point that writes no `Record` is recorded as `opaque`
    (today: `DefaultPath`, `Records`, `SetEmitDisabledForTest`) and NEVER counts
    as an emitter, so the vocabulary is not "any exported name".
- A site of a kind is satisfied by **any** entry point the emitter's
  implementation writes that kind from. Both former `UNRECORDED` sites
  (`internal/tools/task_finish_cleanup.go:4310` / `:4339`) are recognised with no
  production edit: those sites genuinely write the record, through the sibling
  the old gate could not name.
- The gate's three existing halves are intact and unweakened:
  non-vacuity floor (`removalAuditSitesFloor` 15, measured 24),
  every declared arm referenced by a production emit (`removalAuditArmsFloor` 15,
  measured 0 unused), and the delete-a-producer probe against the REAL
  `internal/tools/task_merged_cleanup.go`. No floor was lowered and no site was
  released with `removal-audit-ok:`.
- New proofs in `TestRemovalAuditGateDerivesTheEmitterVocabulary`:
  1. the derived vocabulary names both kinds, with `BranchDelete` and
     `BranchDeleteAt` listed together;
  2. `removalaudit.Records` is NOT accepted as a worktree- or branch-delete
     emitter;
  3. **rename probe**: renaming `BranchDeleteAt` in the emitter's own source AND
     at every production call site leaves the gate green, while the same renamed
     vocabulary against un-renamed callers goes RED (the RED control reproduces
     the exact two `file:line` sites the pre-fix gate was reporting);
  4. an emitter source that fills no `Record.Kind` yields an **error** — an empty
     vocabulary is a hard failure, never a silent skip (Rule 15);
  5. a removal site whose only `removalaudit` call is the record READER is still
     `UNRECORDED`.

## Not Done

- The gate's own two SITE classes still meet the emitter's values through two
  literals (`removalAuditKindValueWorktree` / `removalAuditKindValueBranch`): the
  gate must state which Record.Kind each of its classes corresponds to, and the
  derivation is the thing that must then find an entry point for it. `removalAuditKindOfClass`
  **falls the test loudly** when the emitter declares no entry point for a value,
  so a value renamed in the emitter fails by name here instead of silently
  reporting every site unrecorded. Deriving the values too would mean the gate
  could not fail on that drift at all — stated here as a deliberate boundary, not
  an oversight.
- No production call site was touched (`internal/tools/task_finish_cleanup.go`
  unchanged), and no producer was exempted or released.
- The rename probe mutates real source **in memory** (the emitter's file, and the
  production files as text); it does not write to the worktree. A true on-disk
  rename probe would leave the tree dirty mid-test (Rule 30) and is unnecessary:
  the derivation is a pure function of source bytes.

## Verification

- Build: `go build ./...` → exit 0, no output.
- Tests: `go test ./internal/standards/ -run 'TestRemovalAuditGate'` →
  5/5 PASS (the four pre-existing halves incl. the new derivation proof);
  `go test ./internal/removalaudit/` → `ok github.com/nick/gopi/internal/removalaudit`;
  `make test-budget PKGS='./internal/standards ./internal/removalaudit'` → `packages: 2 finished, 0 with a failure` … `exit=0 outcome=pass`, 28s wall
  (the branch-scoped smoke set, Rule 27: never the whole tree — raw at
  `.../artifacts/05-smoke-budget.txt`).
- Ran (verbatim): `go test -count=1 ./internal/standards/ -run 'TestRemovalAuditGate' -v`
  at worktree HEAD → 5/5 `--- PASS` (`EveryRemovalSiteEmits`, `DataReport`,
  `RedProof`, `SeesTheRealTree`, `DerivesTheEmitterVocabulary`), `ok … 4.547s`,
  exit 0 — full output `.../artifacts/04-verify-at-head.txt`.
- Ran: **before** — `git checkout HEAD -- internal/standards/removal_audit_gate_test.go`
  (the shipped gate, unchanged tree), then
  `go test ./internal/standards/ -run 'TestRemovalAuditGate'`:
  ```
  --- FAIL: TestRemovalAuditGateEveryRemovalSiteEmits (1.09s)
      removal_audit_gate_test.go:428: internal/tools/task_finish_cleanup.go:4310 is a branch-delete site (branch -d) whose enclosing function holds no removalaudit.BranchDelete BEFORE it ...
      removal_audit_gate_test.go:428: internal/tools/task_finish_cleanup.go:4339 is a branch-delete site (branch -D) whose enclosing function holds no removalaudit.BranchDelete BEFORE it ...
  FAIL github.com/nick/gopi/internal/standards 3.557s
  ```
  **after** — the refactored gate, same command, `-v`:
  ```
  --- PASS: TestRemovalAuditGateEveryRemovalSiteEmits (1.19s)
  --- PASS: TestRemovalAuditGateDataReport (1.19s)
      removal sites: total=24 map[emitting:23 escaped:1] (floor 15) kinds=map[branch-delete:14 worktree-removal:10]
      declared arms with no production emit: [] (floor 15 arms referenced)
  --- PASS: TestRemovalAuditGateRedProof (0.00s)
  --- PASS: TestRemovalAuditGateSeesTheRealTree (0.77s)
      RED on the real tree (emit removed): [internal/tools/task_merged_cleanup.go:606]
  --- PASS: TestRemovalAuditGateDerivesTheEmitterVocabulary (2.60s)
      derived vocabulary: worktree-removal [WorktreeRemove] | branch-delete [BranchDelete BranchDeleteAt] | no kind written by [DefaultPath Records SetEmitDisabledForTest]
      RED control (emitter renamed, callers not): [internal/tools/task_finish_cleanup.go:4310 internal/tools/task_finish_cleanup.go:4339]
      empty vocabulary refused: the emitter source declares no Record.Kind filled by an exported entry point ...
  PASS ok github.com/nick/gopi/internal/standards 6.167s
  ```
- Result: the two long-standing `UNRECORDED` reports are gone and nothing was
  released to get there; the derived vocabulary prints its four names, the RED
  control still reproduces the defect shape at the two exact sites, and the
  rename probe is green on both sides.
- Commit: the tip of branch `task/01a12bacc4e578c298a4d7dea1a2dc89`, which carries
  this report in the same commit as the gate —
  `git log -1 --oneline task/01a12bacc4e578c298a4d7dea1a2dc89`.

## Evidence (self-filed)

- raw BEFORE output: `~/.gopi/sessions/01a12bacc4e578c298a4d7dea1a2dc89/artifacts/01-before-fail.txt`
  (contains the two `UNRECORDED` lines verbatim, exit 1).
- raw AFTER output: `.../artifacts/02-after-pass.txt` (all five gate tests PASS,
  the derived vocabulary line, the RED control and the empty-vocabulary refusal).
- the 24-site census and the vocabulary table are the two `TestRemovalAuditGateDataReport`
  / derivation-test log lines reproduced above verbatim from that run.
- `git status --porcelain` in the run's worktree: empty at hand-over (Rule 30).
