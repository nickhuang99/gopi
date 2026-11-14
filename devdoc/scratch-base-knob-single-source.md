<!-- doc-class: historical -->
# The memory-base env knob: one definition, two names, one reader

Task: the redeclaration that made `internal/scratch` unbuildable
(`BaseEnvName` / `tmpfsMagic` / `ramfsMagic` declared twice) — verified resolved
on master, plus the residual single-source-of-truth gap this change closes.

## What was actually broken, and where it stands now

The reported break was reproduced from the master tip named in the briefing
(`b712cacb`). That commit is **not in master's history** (`git merge-base
--is-ancestor b712cacb master` → NO): the three colliding blocks landed through
the `fix/worktree-sweep-memory-base-policy` journey, and master re-landed the
package when its merge was re-resolved (`09ebf131`, `566cf91c`, both
2026-09-16). On master today the colliding blocks are **gone**:

| value | ONE definition site (master) | declared elsewhere as |
|---|---|---|
| `"GOPI_SCRATCH_BASE"` | `base_residue.go` `ResolveBaseEnvName` — read by `ResolveBase` (base_residue.go:151) | `tmproot.go` `BaseEnvName = ResolveBaseEnvName` (alias) |
| `"GOPI_SCRATCH_MEMORY_BASE"` (host knob) | `tmproot.go` `MemoryBaseEnvName` | `internal/testutil/legacy_residue.go` (was a mirror → now an alias) |
| `0x01021994` / `0x858458f6` | `rootgocache.go` `TmpfsMagic` / `RamfsMagic` | `tmpfsMagic`/`ramfsMagic` (scratch), `scratch.TmpfsMagic`/`RamfsMagic` (testutil) |

So the semantics conflict is **decided, in code and in comments**:
`GOPI_SCRATCH_BASE` is the fixture-overridable knob (`ResolveBase`, one reader —
the wording "an explicitly configured base (GOPI_SCRATCH_BASE) is swept" in
`base_residue_test.go` confirms the intent), and `GOPI_SCRATCH_MEMORY_BASE` is
host configuration — the variable `scripts/setup-scratch-home.sh` reads and the
worktree sweep attributes a finding to (`worktree_sweep_memory_base.go:150`
reads `scratch.MemoryBaseEnvName`). Neither name was silently re-pointed: each
kept its own value, which is exactly why the alias (not a winner-takes-all
delete) was the correct resolution.

Fresh-extract evidence (`git archive master | tar -x` into a scratch dir):
`CGO_ENABLED=0 go build ./internal/scratch/` exit 0, `go build ./...` exit 0,
`go test ./internal/scratch/ -count=1` → ok 12.188s.

## The residual gap this change closes

`tmproot.go`'s comment claimed *"internal/testutil.MemoryBaseEnvName is an ALIAS
of this constant"*. It was **not**: testutil typed the bare literal a second
time, and a third copy lived in `scripts_gate_test.go`. Two consequences, both
of them the Rule 13/14 failure mode:

1. **A re-spelling would have gone unnoticed by the gate that claims to catch
   it.** `TestMemoryBaseNameMatchesTheSetupScript` compared
   `MemoryBaseEnvName` (testutil's own copy) against the setup script — a
   self-comparison that stays green if testutil's copy drifts from
   `scratch.MemoryBaseEnvName`, while the sweep and the production reader would
   silently disagree about which variable names the host's base.
2. Three spellings of one variable is the drift surface Rule 14 exists to close.

Fixed: `testutil.MemoryBaseEnvName = scratch.MemoryBaseEnvName`,
`scratchMemoryBaseEnvName = MemoryBaseEnvName`, and the drift gate now first
asserts `MemoryBaseEnvName == scratch.MemoryBaseEnvName` (alias identity), then
that the script mentions it.

Two mechanical guards (Rule 13's exhaustive-test pattern):

- `TestMemoryBaseEnvNameHasOneDefinition` — the literal `"GOPI_SCRATCH_MEMORY_BASE"`
  may appear in CODE in exactly one file (`internal/scratch/tmproot.go`); any
  other `.go` file under `internal/` or `cmd/` carrying it fails. Comments are
  stripped, so prose that names the variable stays legal (mentions are
  documentation, not definitions). The scan has a read-floor (>100 files), so a
  moved tree fails loudly instead of passing vacuously.
- Red-proof: injecting a one-line `const probe = "GOPI_SCRATCH_MEMORY_BASE"`
  into `internal/testutil` fails the gate with the file:line of the re-inline;
  the probe is removed and the gate goes green again.

## Not done (honest)

- The full `go test ./...` suite was **not** run (it exceeds the package budget
  on this host); verification is the package-scoped set below. The epic's
  acceptance is about the build, which is verified exhaustively.
- `go build` was verified on a `git archive` extract and in the worktree, not
  from the main repo's working directory (Rule 6: the main repo is shared).
- The pre-existing mirror is left in place **only** where it is a comment or a
  test-local name; no production `.go` file mirrors the value any more.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` on a fresh master extract → exit 0;
  same command in the worktree after this change → exit 0.
- Vet: `go vet ./internal/testutil/ ./internal/scratch/` → exit 0.
- Tests: `go test ./internal/scratch/ -count=1` → ok; `go test ./internal/testutil/
  -run 'TestMemoryBaseEnvNameHasOneDefinition|TestMemoryBaseNameMatchesTheSetupScript'
  -count=1 -v` → both PASS; the mirror gate FAILS on an injected re-inline
  (red-proof).
- gofmt: `gofmt -l internal/testutil/ internal/scratch/` → empty.
