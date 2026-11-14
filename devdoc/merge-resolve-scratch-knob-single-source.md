<!-- doc-class: normative -->
# Merge Resolution Report: fix/scratch-knob-single-source ← master

## Completed
- Reproduced the merge in the worktree: `git merge master --no-commit --no-ff`
  (HEAD `e9c398db` + master `0693a1f3`). Two conflicted files:
  `internal/testutil/legacy_residue.go`, `internal/testutil/scripts_gate_test.go`.
- Both conflicts were **comment/doc text only** — the executable code line was
  byte-identical on ours and theirs, so neither side's code could be dropped.
- Resolution = union of both intents:
  - `legacy_residue.go`: kept master's TRUE-ALIAS / same-script / drift-gate
    framing for the scratch env-name single source, plus our Rule 13/14
    (constants are referenced, never bypassed) wording.
  - `scripts_gate_test.go`: kept master's drift-gate/same-script doc and our
    `TestMemoryBaseNameMatchesTheSetupScript` specifics.
- Every conflict marker removed; `git add` of the two files; **committed first**
  as `828811ac` "resolve merge fix/scratch-knob-single-source into master" with
  parents `e9c398db` (ours) + `0693a1f3` (master).

## Verification
- Markers: `grep -rn '^<<<<<<< \|^>>>>>>> '` over the tree → 0 hits.
- `git status --porcelain` → empty (clean, merge concluded).
- Both resolved files differ from BOTH parents (`git diff --stat 828811ac^1 828811ac`)
  → proves contributions from each side survived, nothing was wholesale-taken.
- Build: `go build ./...` → exit 0.
- Touched-package tests: `go test ./internal/testutil/ -run TestMemoryBaseNameMatchesTheSetupScript -v`
  → PASS (`ok github.com/the operator/gopi/internal/testutil 4.872s`).
- `gofmt` clean, `git diff --check` clean.

## Not Done (honest)
- `go test ./internal/standards/` FAILS on this repo independently of the merge:
  Rule 17 reports 2 unbaselined `os.Unsetenv` hits at
  `internal/agent/journalreport_test.go:101,104`. **Proven pre-existing**: the
  file and those exact lines are byte-identical at master tip `0693a1f3`
  (`git show 0693a1f3:internal/agent/journalreport_test.go`), and that file is
  NOT part of this resolution (`git diff --stat 828811ac^1 828811ac` lists only
  the two testutil files). Master-only, must be fixed on master — not by this
  resolver, and NOT papered over with a baseline entry added by the merge.
- `go test ./...` deliberately not run (known pre-existing full-suite hang; a
  bounded check is the contract here).

## Files
- `internal/testutil/legacy_residue.go`
- `internal/testutil/scripts_gate_test.go`
