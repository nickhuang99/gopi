<!-- doc-class: historical -->
# Task Report: worker-ownership gate master-red — the peak-guard probe matrix

Task: fix the master-red of `TestWorkerOwnershipPointerPresent`
(`internal/testutil`), which was blocking unrelated verification work (it was
hit while verifying `internal/cgroupscope`'s scratch pin).

## Completed

- **Reproduced the red on the CURRENT master tip, not the briefed one.** The
  briefing names master `833cc2b6`; master had advanced to **`16f871c3`**.
  At `16f871c3` the gate fails with **exactly ONE** offender, and it is NOT
  the file the briefing names:

  ```
  worker_ownership_gate_test.go:346: files that teach a worker-launch / task-run command without the operator-only pointer:
        devdoc/peak-guard-token-free-verbs.md
  ```

- **The briefed file is already fixed on master — the briefing is stale.**
  `devdoc/worker-daemon-acceptance/closing-run-1806.txt` already carries the
  canonical pointer as its line 1 (the marker token `WORKER-OWNERSHIP:
  OPERATOR-ONLY` plus the canonical block's path, in the file's `#` comment
  form) and its `workerOwnershipDismissed` siblings landed in `66517e00`
  (merged). Nothing was changed in it here — re-fixing a fixed file would be
  the wrong edit.

- **Offender identified and measured.** `devdoc/peak-guard-token-free-verbs.md`
  (the peak-guard cost-guard epic's report, `62bbcfa4`) hits 4 of the gate's
  launch needles, ALL of them inside the report's own **live probe matrix**
  table — `| `gopi worker -h` | 0 | … |` (139), `| `gopi task run -h` | 0 | … |`
  (140), `| `gopi worker` | **4** | refusal |` (142),
  `| `gopi task run` | **4** | refusal |` (144). Zero other occurrences.

- **Disposition: dismissed with a reason — deliberately NOT annotated.**
  Each cell is *an observation of the command being DENIED*: the recorded
  outcome is the guard's own refusal, which is the measurement the matrix
  exists to publish. The one invocation the document actually teaches a reader
  to copy is `gopi version` (the token-free smoke test whose exemption is the
  epic's whole subject). Pasting the pointer there would instruct a reader to
  do the opposite of what the row records, and a pointer convention that fires
  on recorded refusals turns the marker into noise. Same ruling and same
  precedent as the transcript dismissals already in the table
  (`devdoc/worker-daemon-acceptance/*.txt`, `devdoc/worktree-cwd-zombie.md`).

- **No merge-conflict exposure.** The two live peak-guard branches
  (`fix/peak-guard-cost-aware`, `fix/peak-guard-cost-class`) do not touch this
  doc (`git diff --name-only master...<branch>` — empty for it), so annotating
  or dismissing it cannot collide with either.

## Files

- `internal/testutil/worker_ownership_gate_test.go` — one
  `workerOwnershipDismissed` entry for `devdoc/peak-guard-token-free-verbs.md`
  with a reasoned comment block (no rule logic changed; the gate's needles,
  scope and non-vacuity guards are untouched).
- `devdoc/worker-ownership-gate-peak-doc.md` — this report. It quotes the
  audit's needles, so it carries a matching dismissal entry too (the same
  self-referential disposition `devdoc/worker-ownership-pointer-master-red.md`
  took on `fix/worker-ownership-pointer-line`).

## Not Done

- **No pointer line pasted into `devdoc/peak-guard-token-free-verbs.md`.**
  Deliberate (see disposition above): a pointer says "operator-only — a worker
  is not yours to start", and the document's four mentions are the guard
  refusing exactly that; the honest artifact is the recorded refusal.
- **The other two master-red files are NOT re-fixed here.** The branch
  `fix/worker-ownership-pointer-line` (`8cd5ba18`) already fixes
  `devdoc/worker-daemon-acceptance/closing-run-1806.txt` (pointer added, file
  1 `# …` — master already carries it via `66517e00`) and dismisses
  `devdoc/worktree-cwd-zombie.md`. Those are that branch's edits; this branch
  carries only the offender that is actually red at `16f871c3`.

## Verification

- Gate (acceptance): `go test ./internal/testutil/ -run TestWorkerOwnershipPointerPresent -count=1`
- All gate tests (incl. the canonical-block / scans-the-tree non-vacuity checks):
  `go test ./internal/testutil/ -run TestWorkerOwnership -count=1`
- Package (acceptance): `go test ./internal/testutil/ -count=1`
