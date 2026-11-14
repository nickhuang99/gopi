<!-- doc-class: historical -->
# The `-X` operand is checked against go.mod, and the absence message names the dropped-operand cause

Task `01a1346b-0a5d-7dad-a343-e77104f1b75f`, branch
`task/01a1346b0a5d7dada343e77104f1b75f`. Follow-up to the Rule 41 row filed by task
`01a1339f-cf36-7ebe-bc52-917adf3cf2b9` (`artifacts/brief-silent-ldflag-drop.md`).

> **Truth class: historical** — this is the record of one change. What is NORMATIVE is
> the test it leaves behind: `internal/version/module_path_agreement_test.go` is the
> standing claim (the operand must agree with `go.mod`), and it fails when that claim is
> violated. The measurements below are evidence for why the claim exists.

## Completed

- **`internal/version/module_path_agreement_test.go` (new).** Two tests pin
  `gopiversion.RevisionLdflagPath` to its own source of truth, each half checked
  separately so a failure names which half broke:
  - `TestRevisionLdflagPathAgreesWithItsSourceOfTruth` rebuilds the operand from three
    facts read at run time — the `module` line of the tree's own `go.mod`, this package's
    directory relative to that file (found by walking up from the test's CWD), and its own
    slash form — and requires the constant to equal it exactly.
  - `TestRevisionLdflagPathNamesAVariableInThisPackage` checks the `importpath.name` shape
    independently: the name after the last `.` is a declared package-level `var` in the
    non-test sources of this package, parsed with `go/parser` (a text scan misses spellings
    it did not anticipate).
  - Read floors everywhere: a missing `go.mod`, a missing `module` directive, a package
    that resolves to the repo root, a directory from which no non-test `.go` file parsed —
    each fails **loudly** rather than passing vacuously. A check that cannot find its
    source of truth is red, never silently green (Rule 15).
- **`cmd/agent/cli_version.go`.** A second named constant,
  `versionStampSkippedDroppedOperandCause`, joins `versionNoVCSStamp`: the sentence now
  names the linker's own silence as an additional cause, and spells the required operand
  from `gopiversion.RevisionLdflagPath` (never retyped — Rules 1b/13/14) rather than
  pointing the reader at the tree.
- **`cmd/agent/cli_version_stamp_test.go`.** The wording test
  (`TestVersionNoVCSStampMessageNamesNoCheckout`) now pins the new clause AND asserts the
  operand path appears in the message; the clause is referenced through its constant, so
  test and message cannot drift.

## Not Done (honest)

- **`internal/version` is not covered by the wording test's real-build arms.** The new
  clause is asserted on the CONSTANT and through the fixture binaries in `cmd/agent`; no
  new real-build fixture was added for the wrong-operand case itself, because
  `cmd/agent/cli_version_stamp_test.go` already builds real binaries from a real clone +
  worktree and the wrong-operand arm is exactly the report's own negative control
  (below), measured by hand. If a future change makes the fixture cheap to extend, the
  wrong-operand build belongs there as a third arm.
- **Nothing checks an operator's shell command.** The constant is now pinned to `go.mod`;
  a hand-typed `-X` operand in someone's terminal is not something a test in this tree can
  see. The message change is the mitigation (it names the correct spelling), not a gate.
- **`gopi version -h` was left as it was.** The usage head already prints the recipe with
  the operand taken from `gopiversion.RevisionLdflagPath`, so it is self-correcting, and
  growing it would grow a second surface for one sentence. `man/gopi-version.md` carries
  that same recipe in a `copied verbatim from gopi <verb> -h` fence and is gated by
  `make man-check` (`internal/standards/testdata/mancensus.go`), so neither copy can drift
  in silence.

## Why this is a defect and not cosmetics (the argument, so a later reader does not relitigate it)

The Go linker does **not** validate an `-X importpath.name=value` operand against the
symbol it names. An operand that names nothing is dropped in silence — the build exits 0
and stderr stays empty — so gopi's declared-revision recipe can degrade to stamping
nothing while every surface reports success. Measured on this ground, Go 1.24.0:

| build | `-X` operand | exit | stderr | artifact's `version` |
|---|---|---|---|---|
| documented recipe | `github.com/nick/gopi/internal/version.Revision` | 0 | empty | `revision: c3721966…` + `revision-source:` |
| same tree, wrong module half | `github.com/youngminz/gopi/internal/version.Revision` | 0 | **empty** | `revision: unavailable …` |

Both arms are indistinguishable from the command line; only reading the artifact back
separates them (that is the Rule 31 shape, and the reason this repo's recipe exists at
all — a linked worktree gets no toolchain `vcs.revision` either).

Against that backdrop, `RevisionLdflagPath` was a constant **nothing checked**:
`go.mod`'s module path and the constant are two spellings of one fact, and they are 0.9 s
away from disagreeing for the rest of the tree's life. Change the module path (a two-line
file anybody may edit) and the constant goes stale, the recipe keeps exiting 0, `gopi
version` keeps printing `unavailable`, and **no test fails anywhere** — the constant's
entire purpose (Rules 1b/13/14: one spelling, never retyped) is defeated the moment its
one spelling can rot in silence. The new test is the missing link between the two: the
constant is now held against `go.mod` instead of being trusted.

## Evidence (measured on this ground)

```
$ git rev-parse HEAD
c3721966694b4c95df9f5443aeea5a4df5d509e4
```

**Red proof, module half** (`RevisionLdflagPath`'s module path → `github.com/youngminz/gopi`):

```
--- FAIL: TestRevisionLdflagPathAgreesWithItsSourceOfTruth (0.00s)
    module_path_agreement_test.go:193: RevisionLdflagPath = "github.com/youngminz/gopi/internal/version.Revision"
          want "github.com/nick/gopi/internal/version.Revision"
        built from go.mod's module path ("github.com/nick/gopi"), …
FAIL	github.com/nick/gopi/internal/version	1.859s
```

**Red proof, symbol half** (`.Revision` → `.RevisionX`):

```
--- FAIL: TestRevisionLdflagPathNamesAVariableInThisPackage (0.00s)
    module_path_agreement_test.go:215: RevisionLdflagPath = "…internal/version.RevisionX": it names
        "RevisionX" in package "github.com/nick/gopi/internal/version", but this package is
        "internal/version" and declares "Revision"
```

Two mutations, two distinct failing tests — neither test is decoration for the other.

**Restore, by blob identity** (`git hash-object` vs `git rev-parse HEAD:<path>`, not an
index-relative diff):

```
blob before: 8365f6aaf122c9d8f4a7d97c0f04f299477ddc5f  (HEAD:internal/version/self.go = 8365f6aa…)
blob after:  8365f6aaf122c9d8f4a7d97c0f04f299477ddc5f
$ git diff HEAD -- internal/version/self.go | wc -l
0
```

**Rule 31 — the documented recipe, executed, artifact read back** (artifacts under
`~/.gopi/sessions/01a1346b-0a5d-7dad-a343-e77104f1b75f/artifacts/`):

```
$ CGO_ENABLED=0 go build -ldflags "-X github.com/nick/gopi/internal/version.Revision=$(git rev-parse HEAD)" -o artifacts/gopi-recipe ./cmd/agent/
exit=0
$ ./artifacts/gopi-recipe version
v0.1.0
go: go1.24.0
platform: linux/amd64
revision: c3721966694b4c95df9f5443aeea5a4df5d509e4
revision-source: self-declared by the build recipe (-ldflags github.com/nick/gopi/internal/version.Revision) — NOT attested by the toolchain, …
```

**Rule 31 — negative control (the defect, and the new sentence)**: the same tree, the
same HEAD, only the operand's module half wrong:

```
$ CGO_ENABLED=0 go build -ldflags "-X github.com/youngminz/gopi/internal/version.Revision=$(git rev-parse HEAD)" -o artifacts/gopi-wrongx ./cmd/agent/
exit=0        # and 0 bytes on stderr
$ ./artifacts/gopi-wrongx version
v0.1.0
go: go1.24.0
platform: linux/amd64
revision: unavailable — this build carries no VCS stamp: the Go toolchain embedded no VCS build
settings for it — a linked-worktree build gets none, and so does -buildvcs=false — and this
command reads only the artifact, so no revision can be recovered here. The recipe's own operand
can go missing the same way: the linker does not check that `-X importpath.name=value` names a
variable that exists, so an operand naming none is dropped in silence (build exit 0, nothing on
stderr) — if this build passed -ldflags -X, the operand has to spell
github.com/nick/gopi/internal/version.Revision
```

That last paragraph is the deliverable's reader-facing half: the failure mode is still
silent at link time (not ours to change), but the artifact now says which flag to check.
