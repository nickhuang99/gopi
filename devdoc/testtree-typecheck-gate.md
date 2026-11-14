<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Every _test.go in the Tree MUST Compile — the gate is TestTestTreeTypecheckIsGreen

The standing claim, enforced by the gate it names: `TestTestTreeTypecheckIsGreen`
(`internal/standards/testtree_typecheck_gate_test.go`). Its devdoc row is
classified in `devdoc/DOC-INDEX.tsv` by the corpus's own writer
(`python3 tools/docclass/classify.py .`), never by hand.

## The defect it was written after

Task `01a14162`, master tip `adafe54b2` (2026-10-15): `internal/docclass`'s
`anchor_number_material_test.go` referenced `gitDirEnvName` /
`gitWorkTreeEnvName`. Those identifiers exist only as **package-local** names of
`cmd/agent/cli_doccheck_test.go` — a package `internal/docclass` does not import —
so the reference resolved nowhere:

```
$ go vet ./...
# github.com/nick/gopi/internal/docclass
# [github.com/nick/gopi/internal/docclass]
vet: internal/docclass/anchor_number_material_test.go:158:14: undefined: gitDirEnvName
→ rc=1
```

`go build ./...` was green throughout, and that is the interesting half: **`go
build` never compiles a `_test.go` file**, and `go test` reports the failure only
for the package you ask it about. So the integration branch carried a red test
build that no routine command in the suite named.

The fix is at the type's ONE definition site (`internal/testutil/gitenv.go`):
`gitWorkTreeEnvName` → `GitWorkTreeEnvName`, and the caller references
`testutil.GitDirEnvName` / `testutil.GitWorkTreeEnvName` instead of a name that
only resolved in the package it was copied from (Rules 1/13/14 — one definition,
never mirrored).

## The mechanism, and the flag that is load-bearing

`go list` already knows whether a package's test variant compiles, and `-test`
enumerates that variant:

```bash
go list -e -export -test -json ./...   # -e keeps failing packages in the listing
```

**`-export` is not an optimisation — it is the whole mechanism.** The listing
runs the compiler over each test variant in order to produce export data, and
that compile is what surfaces the failure. Measured on an `git archive` export of
the pre-fix tip with the broken names restored (2026-10-15, this host):

| invocation | packages with errors |
|---|---|
| `go list -e -test -json ./...` | **0** |
| `go list -e -export -test -json ./...` | **2** — carrying `internal/docclass/anchor_number_material_test.go:158:14: undefined: gitDirEnvName` |

A future "optimisation" that drops the flag leaves the gate green on a tree whose
test build is red; `TestTestTreeTypecheckRedProof`'s third arm pins exactly that
difference, and logs a NOTE (rather than failing) if a future toolchain stops
exhibiting it.

## Why a gate rather than "just run vet"

`go vet ./...` is the reproduction, not the gate. It is not on `make test`'s
path, it costs a whole-tree type-check, and — decisive — it fails on **any** vet
diagnostic (a printf verb, an unreachable branch), not only on a package that
does not compile. A suite that shelled it out would conflate "this tree does not
build" with "someone wrote a sloppy Printf". The class that must fail fast is
narrower and decidable: *does every package's test variant compile*. That is the
question `go list -export -test` answers.

## Cost, measured

- warm (the ordinary case: the suite has just built the same packages, so the
  export data is in the build cache): **~1.7 s** for `./...`;
- cold: **~45 s** — one whole-tree build, which every run of the suite pays
  anyway.

It is deliberately **not** classified heavy: a `heavytest.Require` would take it
off the smoke path, which is the one place this defect has to be caught.

## The proof is three-sided

`TestTestTreeTypecheckRedProof` runs tiny stdlib-only synthetic modules (so the
proof is ~0.3 s and cannot be skipped behind a module download):

- **GREEN** — a module whose test file resolves the names it uses is clean;
- **RED** — the same module with the test file referencing a **sibling
  package's** package-local constant, unqualified: reported, and the message must
  name the identifier;
- **FLAG** — the broken module **without** `-export` reports nothing.

The real-tree arm asserts two floors first (≥ 200 package entries, ≥ 50 test
variants), so an enumeration that looked at nothing cannot report a clean green.

## Evidence of the red proof on the real tree

A `git archive` export of the pre-fix tip, with the gate file copied in and the
broken names restored by an inert mutation, run for real:

```
$ go test ./internal/standards/ -run TestTestTreeTypecheckIsGreen -count=1
--- FAIL: TestTestTreeTypecheckIsGreen (51.58s)
    1 package(s) do not compile — a test build of this tree is RED. …
      github.com/nick/gopi/internal/docclass [github.com/nick/gopi/internal/docclass.test]
        internal/docclass/anchor_number_material_test.go:158:14: undefined: gitDirEnvName
        …
rc=1
```

## Rework note (task `01a141ff`, 2026-10-17) — this file and its gate were NOT re-authored

A rework row (`01a141925e307a55b2d11edad8166780`, tip `de99aad27`) replayed the
`a9949a497` landing of this gate onto `3124ec41f` and asserted, in its own copy of
this document, that *"the sibling row `01a14162` … never landed on master"*. **That
premise is false on this integration branch**, and the measurement is one command:

```
$ git merge-base --is-ancestor a9949a497 refs/remotes/gitPushcheck/master; echo rc=$?
rc=0
```

`a9949a497` — and `13993992c` after it — are ancestors of `942b3ac6a`. The
branch's gate file was byte-identical to `a9949a497`'s (`f1fb432e9`), i.e. to the
revision `13993992c` superseded, so merging it would have re-introduced the second
whole-tree listing that commit removed. The merge therefore took **this tree's**
side for both files and kept only the one half this tree still lacked: the removal
of the `gitDirEnvName` / `gitWorkTreeEnvName` **mirror** in
`cmd/agent/cli_doccheck_test.go`, which now references
`testutil.GitDirEnvName` / `testutil.GitWorkTreeEnvName` (Rule 14 — a mirror is not
a vocabulary; that copy is what made the pair look resolvable in another package).

Per-file disposition and the ref+sha each reading was taken on:
`devdoc/testtree-typecheck-gate-rework-20261017.md`.
