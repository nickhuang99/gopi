<!-- doc-class: historical -->
# Task Report: one production home per scratch env name (Rule 13/14 hardening)

## Summary

The reported RED — master's `internal/scratch` failing to compile because two
parallel landings auto-merged two independent attempts at the same Rule-14
constant homes — was **already repaired on master before this task began**. The
deliverable here is therefore the *hardening*: finishing the one-home-per-value
discipline for the value the repair left mirrored, correcting two comments that
misdescribed the relationship, and closing the class with a machine gate.

## Completed

### 1. `internal/testutil.MemoryBaseEnvName` is now a TRUE alias

Was an independent re-declaration of the literal in another package:

```go
// BEFORE (internal/testutil/legacy_residue.go:53) — a MIRROR
MemoryBaseEnvName = "GOPI_SCRATCH_MEMORY_BASE"

// AFTER — a reference to the ONE definition (Rule 14)
MemoryBaseEnvName = scratch.MemoryBaseEnvName
```

`internal/testutil` already imported `internal/scratch` (`scratchroot.go` aliases
`scratch.TmpfsMagic` / `RamfsMagic`), so this costs **no new dependency and no
import cycle** — confirmed by `go list -deps ./internal/scratch/` returning zero
`internal/standards`/`internal/testutil` entries (scratch is a leaf).

Runtime equivalence proven, not assumed:

```
testutil.MemoryBaseEnvName="GOPI_SCRATCH_MEMORY_BASE" scratch.MemoryBaseEnvName="GOPI_SCRATCH_MEMORY_BASE" equal=true
```

### 2. The test-local copy reconciled to the same source

`internal/testutil/scripts_gate_test.go:104` carried a second literal
(`scratchMemoryBaseEnvName`). It is reconciled to the package's own alias, which
now resolves to the ONE definition:

```go
scratchMemoryBaseEnvName = MemoryBaseEnvName
```

### 3. The two misdescribing comments corrected

- `internal/scratch/tmproot.go:80-86` asserted "internal/testutil.MemoryBaseEnvName
  is an ALIAS of this constant" while testutil held an independent literal. It now
  states the real, verified relationship: this constant is the **ONE definition**,
  and testutil's name is a true alias of it *by construction*.
- `internal/testutil/legacy_residue.go:49-54` claimed one-source while declaring a
  second literal. The comment now names the definition it aliases, and the
  package-level doc no longer implies a second source.

**No behavioural change**: the scratch diff is comment-only (verified below), so
master's landed `09ebf131` semantics are untouched, per scope discipline.

### 4. New machine gate (Rule 13 literal-gate precedent)

`internal/standards/scratch_env_home_gate.go` (+ `_test.go`), registered as
Gate 8 in the `standardgate` registry, mirroring the `TestUnknownNode_MirrorLiteralGate`
and `worktreeRootGate` precedents. It asserts the literal for each policed value
occurs in exactly ONE production `.go` file:

| value | the ONE holder |
|---|---|
| `GOPI_SCRATCH_MEMORY_BASE` | `internal/scratch/tmproot.go` |
| `GOPI_SCRATCH_BASE` | `internal/scratch/base_residue.go` |

Design notes worth keeping:

- **The gate READS the values from the constants** (`scratch.MemoryBaseEnvName`,
  `scratch.ResolveBaseEnvName`) instead of re-typing them. A gate that mirrored
  the literals it polices would be the very defect it reports — this was
  discovered concretely: the first draft hardcoded both and **failed on itself**
  (2 hits, its own lines 70–71). `internal/scratch` is a leaf package, so the
  reference is free.
- **AST string literals only**, so a doc comment that *mentions* the variable to
  explain the contract is prose, not a definition — the gate does not punish
  explanation (pinned by a probe).
- An in-package **re-declaration** is reported too (the shape that broke the
  build), while the declared holder may spell its own value.
- `_test.go` files are deliberately out of scope: a test that runs the setup
  script must be able to write the wire spelling into a child's environment.

## Not Done (honest)

- **The compile break itself was not "fixed" here — it was already fixed.** The
  reported RED is pre-existing-and-already-repaired; nothing in this branch
  resurrects it. Recovery commits: `b1ec7062` and `5b42b613` are **NOT** ancestors
  of master; the repair landed in master's **`09ebf131`** ("resolve merge
  feat/merger-always-rebuilds into master"), and pristine-extract + in-place
  builds both exit 0 at master tip `e791c9c7`.
- **`tmpfsMagic`/`ramfsMagic`** were part of the same collision but are already
  correctly aliased (`rootgocache.go` holds the canonical `TmpfsMagic`/`RamfsMagic`;
  `tmproot.go` and `testutil/scratchroot.go` both reference them). No change was
  needed, and none was made.
- **Two pre-existing test failures are NOT addressed** (both reproduce identically
  on pristine master, so they are out of this task's scope):
  - `TestEveryTempUsingSuiteIsPinned` — `internal/taskreport` lacks a `TestMain`
    scratch-root pin.
  - `TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy` — the finding names no
    root.
  - Additionally `internal/standards` **Gate 2 (Rule 17)** is red on pristine
    master (`internal/agent/journalreport_test.go:101,104`) and remains red here.
    Gate 8 passes.

## Verification

```
# Build (acceptance: exit 0)
$ CGO_ENABLED=0 go build ./...
build-exit=0

# The new gate, on the real tree — 300 files scanned, zero violations
$ CGO_ENABLED=0 go test ./internal/standards/ -run TestGate8 -v
--- PASS: TestGate8ScratchEnvHomeRedProof (0.00s)
--- PASS: TestGate8ScratchEnvHomeInjectionRedProof (0.00s)
    scratch_env_home_gate_test.go:169: Rule 13/14 (one production home per scratch env name):
      files=300 hits=0 baselined=0 escaped=0 NEW=0
--- PASS: TestGate8ScratchEnvHomeRealTreeIsClean (0.21s)
ok  github.com/nick/gopi/internal/standards  0.939s

# RED PROOF — the gate is NOT vacuous. Re-inlining the literal into a SECOND
# production file (internal/testutil/zz_redproof_probe.go) fails it:
$ CGO_ENABLED=0 go test ./internal/standards/ -run TestGate8ScratchEnvHomeRealTreeIsClean
--- FAIL: TestGate8ScratchEnvHomeRealTreeIsClean (0.20s)
    files=301 hits=1 baselined=0 escaped=0 NEW=1
    the production tree carries 1 re-inlined scratch env-name literal(s):
      [{rel:internal/testutil/zz_redproof_probe.go line:5
        desc:"GOPI_SCRATCH_MEMORY_BASE" re-inlines the ONE definition — reference
        scratch.MemoryBaseEnvName (defined in internal/scratch/tmproot.go) ...}]
FAIL  (exit 1)
# probe deleted; the gate is green again.

# Targeted suites (bounded -run filters; never `go test ./...` — known suite hang)
$ CGO_ENABLED=0 go test ./internal/scratch/
ok  github.com/nick/gopi/internal/scratch  7.682s

$ CGO_ENABLED=0 go test ./internal/testutil/ -run TestMemoryBaseNameMatchesTheSetupScript -v
--- PASS: TestMemoryBaseNameMatchesTheSetupScript (0.00s)   # the setup-script drift gate

# Acceptance grep: exactly ONE bare declaration site for each value, repo-wide
$ grep -rn '"GOPI_SCRATCH_MEMORY_BASE"' --include="*.go" .
./internal/scratch/tmproot.go:90:  MemoryBaseEnvName = "GOPI_SCRATCH_MEMORY_BASE"
$ grep -rn '"GOPI_SCRATCH_BASE"' --include="*.go" .
./internal/scratch/base_residue.go:70:  ResolveBaseEnvName = "GOPI_SCRATCH_BASE"

# CLI help still resolves
$ CGO_ENABLED=0 go run ./cmd/agent scratch-gc --help
gopi scratch-gc — sweep the suite scratch home ONCE and exit (watchdog face).
... the resolved memory base (GOPI_SCRATCH_BASE, default /dev/shm)

# Pre-existing failures reproduce on PRISTINE MASTER (not caused here)
$ (in ~/work/GoPi @ e791c9c7) CGO_ENABLED=0 go test ./internal/testutil/ -run TestEveryTempUsingSuiteIsPinned
--- FAIL: TestEveryTempUsingSuiteIsPinned   (identical message)
$ (in ~/work/GoPi @ e791c9c7) CGO_ENABLED=0 go test ./internal/tools/ -run TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy
--- FAIL: ...: the finding names no root
```

## Files

- `internal/scratch/tmproot.go` — comment corrected (comment-only)
- `internal/testutil/legacy_residue.go` — mirrored literal → true alias; comments corrected
- `internal/testutil/scripts_gate_test.go` — test-local copy → alias of the one source
- `internal/standards/scratch_env_home_gate.go` — NEW gate (predicate + registry entry)
- `internal/standards/scratch_env_home_gate_test.go` — NEW red proof + injection proof + real-tree check
- `internal/standards/gate_test.go` — Gate 8 registered
- `devdoc/scratch-env-single-home.md` — this report

## Commit

`feat(standards): one production home per scratch env name (Rule 13/14 gate)` on
branch `feat/scratch-single-home` (worktree `~/workspace/gopi-scratch-single-home`),
branched off master `e791c9c7`.

## Addendum — independent re-verification (second session, same branch)

Every acceptance claim above was re-run from scratch in the worktree rather than
trusted from the text. Nothing new was implemented; this section records the
reproduction (all commands in `~/workspace/gopi-scratch-single-home`).

**Green, re-measured:**

```
$ CGO_ENABLED=0 go build ./...                       # exit 0 (whole repo)
$ CGO_ENABLED=0 go test ./internal/scratch/          # ok  github.com/nick/gopi/internal/scratch  7.526s
$ CGO_ENABLED=0 go test ./internal/testutil/ -run 'TestMemoryBaseNameMatchesTheSetupScript' -v
--- PASS: TestMemoryBaseNameMatchesTheSetupScript (0.00s)   # the setup-script drift gate
$ CGO_ENABLED=0 go test ./internal/standards/ -run TestGate8 -v
--- PASS: TestGate8ScratchEnvHomeRedProof (0.00s)
--- PASS: TestGate8ScratchEnvHomeInjectionRedProof (0.00s)
--- PASS: TestGate8ScratchEnvHomeRealTreeIsClean (0.22s)   # files=300 hits=0 NEW=0
$ CGO_ENABLED=0 go run ./cmd/agent scratch-gc --help | grep GOPI_SCRATCH_BASE
  GOPI_SCRATCH_BASE  the memory base (default /dev/shm).   # CLI help still resolves
```

**Single home per value, re-measured** (bare literal, quoted, so a doc mention
cannot satisfy it):

```
$ grep -rn '"GOPI_SCRATCH_MEMORY_BASE"' --include='*.go' .
./internal/scratch/tmproot.go:90:  MemoryBaseEnvName = "GOPI_SCRATCH_MEMORY_BASE"
$ grep -rn '"GOPI_SCRATCH_BASE"' --include='*.go' .
./internal/scratch/base_residue.go:70:  ResolveBaseEnvName = "GOPI_SCRATCH_BASE"
```

The other sites are references: `internal/testutil/legacy_residue.go:60`
`MemoryBaseEnvName = scratch.MemoryBaseEnvName`,
`internal/testutil/scripts_gate_test.go:107` `scratchMemoryBaseEnvName = MemoryBaseEnvName`,
`internal/scratch/tmproot.go:78` `BaseEnvName = ResolveBaseEnvName`, and the
production reader `internal/tools/worktree_sweep_memory_base.go:150`
`os.Getenv(scratch.MemoryBaseEnvName)`.

**The red proof re-run IN THE TREE** (the earlier run used a probe file; this one
was re-created, observed, and removed):

```
$ printf 'package testutil\n\nconst zzEpicRedProofProbe = "GOPI_SCRATCH_MEMORY_BASE"\n' \
    > internal/testutil/zz_epic_redproof_probe.go
$ CGO_ENABLED=0 go test ./internal/standards/ -run TestGate8ScratchEnvHomeRealTreeIsClean
--- FAIL: TestGate8ScratchEnvHomeRealTreeIsClean (0.22s)   # exit 1
    Rule 13/14 (one production home per scratch env name): files=301 hits=1 NEW=1
    the production tree carries 1 re-inlined scratch env-name literal(s):
      [{rel:internal/testutil/zz_epic_redproof_probe.go line:3
        desc:"GOPI_SCRATCH_MEMORY_BASE" re-inlines the ONE definition — reference
        scratch.MemoryBaseEnvName (defined in internal/scratch/tmproot.go) ...}]
$ rm internal/testutil/zz_epic_redproof_probe.go
$ CGO_ENABLED=0 go test ./internal/standards/ -run TestGate8    # ok ... 0.608s
$ git status --short                                            # (empty — tree clean)
```

**The three failures named in "Not Done" are pre-existing, re-confirmed by a
pristine-master control in `~/work/GoPi` @ `e791c9c7`:**

| failure | on master | on this branch |
|---|---|---|
| `internal/standards` Gate 2 / Rule 17 (`internal/agent/journalreport_test.go:101,104`) | `NEW=2` | identical `NEW=2` |
| `TestMemoryBaseKeepArmsOverrideTheNewRoot` (dirty ⇒ kept) | FAIL — same message | FAIL — same message |
| `TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy` | FAIL — "the finding names no root" | FAIL — same message |
| `TestEveryTempUsingSuiteIsPinned` (`internal/taskreport` lacks a `TestMain` pin) | FAIL — same message | FAIL — same message |

So the branch introduces no new red: master's count and the branch's count agree
site for site.

**A warning for the next reader about the control checkout.** The pristine-master
control above was run in a purpose-made detached worktree
(`~/workspace/gopi-base-e791c9c7`, `git worktree add --detach <path> e791c9c7`).
It was NOT run in `~/workspace/gopi-master-check`: that directory looks like a
master reference but is **another agent's worktree**, parked at `9aec81eb` with
uncommitted local modifications (`cmd/agent/merger_resume_arm_wiring_test.go`) —
using it as a baseline yields a false "master is green" reading, because it
neither contains the commit under test nor the failure sites. Always pin a
control by explicit revision, never by a directory whose name suggests master.
