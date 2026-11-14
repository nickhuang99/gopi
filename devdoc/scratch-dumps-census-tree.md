<!-- doc-class: historical -->
# The scratch dumps class trees in the family census

## The defect (measured 2026-10-11, this host)

`CONFIG.md` rows 655/656 (`GOPI_ERROR_DUMP_DIR` / `GOPI_1214_DUMP_DIR`) advertised

```
gopi tool scratch-gc families --tree dumps
gopi tool scratch-gc families --tree dumps-1214
```

as the census recipe for both knobs. On a default configuration both exit 1:

```
gopi scratch-gc families: could not walk tree dumps at /home/user/.gopi/dumps:
  open /home/user/.gopi/dumps: no such file or directory
scratch-gc role=cli-families trees=0 shapes=0 unclassified=0 files=0 dirs=0 bytes=0
exit=1
```

The reason is a **root mismatch, not a missing directory**. The two knobs' DOCUMENTED
default is `scratch.DumpDir(<class>)` — `<scratch home>/dumps/gopi-dumps` and
`<scratch home>/dumps/gopi-1214-dumps` (`internal/scratch/scratch.go`, `HomeDir()` +
`DumpsDirName` + the class; `internal/deepseek/minimal_core.go`, `error_1214.go`). What
the census knew as `dumps` / `dumps-1214` was the **profile's** `dumps/` pair
(`sibling_dumps.go`), which exists only when an operator EXPORTS one of the knobs. So
the census name reached the tree that needs an export, while the tree that always exists
— the one the repo's own suite writes through — was reachable by **no name at all**.

Measured on this host at the time of the fix: 393 dumps in
`<scratch home>/dumps/gopi-dumps` (405 by the end of the session), and
`gopi tool scratch-gc families --tree scratch-dumps` →
`no censused tree is named "scratch-dumps"`.

## The fix — two rows in the registries that already exist, no new machinery

| what | where |
|---|---|
| the two declared-tree rows (`scratch-dumps`, `scratch-dumps-1214`) | `internal/scratch/scratch_dumps.go` (appended by `DeclaredTrees()`) |
| the two census trees (`--tree scratch-dumps`, `--list` line) | `internal/sessionlayout/scratch_dumps.go` |
| the name → root map wired once | `internal/config/scratch_trees.go` |
| both rows enumerated by the census CLI | `cmd/agent/cli_scratch_gc.go` (`censusTrees`) |

**Reused, not re-implemented.** The declared rows are built by the PROFILE dump rows'
own constructor shape (`siblingDumpTree`): `EntryKindFileSuffix` + the same suffix + the
same namespace predicate (`isSiblingDumpName`) + the same 14-day window. The census
trees carry the same family list (`dumpsFamilies`). The sweep therefore applies the
shared walk, the ONE `/proc` pass, the `.holder` registration, the "a declared root is
never a candidate" refusal and the removal re-check — none of it is a second answer
(Rule 35: no new process, no new file, no new dependency; the rows ride the sweep that
already ticks).

## Why the roots are wired through a map, not spelled in the census package

`internal/scratch` imports `internal/tools`, which imports `internal/sessionlayout`, so
`internal/sessionlayout` cannot import `internal/scratch` back. The existing seam is a
function value installed by `internal/config` (`RegisterSiblingDataRootResolver`). The
new trees need **two** roots that are not derived from the profile root, so the seam is
a map keyed by tree NAME, built in the package that owns both halves:

```go
sessionlayout.RegisterScratchDumpsRootResolvers(scratch.ScratchDumpClassRoots())
```

A name and its root are therefore paired where the names live, and a row cannot ask for
another tree's root. An unwired map resolves to a **named error**, never `""` (an empty
root would walk the process's own working directory — the failure the sibling registry's
own unwired resolver exists to refuse).

## What is deliberately NOT done

- **The profile pair is untouched.** `--tree dumps` still exits 1 on a default
  configuration, and that is now the *correct, documented* reading: the profile tree is
  the knob's EXPORTED target, and its absence is the honest report that nothing was
  pointed there. A name that resolved to "whichever of the two exists" would answer a
  question the caller did not ask.
- **No new census subject invented.** `SiblingTrees()` still enumerates the profile's own
  children (so `SiblingBoundTreeNames()` and its gate are unchanged); the scratch pair is
  enumerated by `ScratchDumpsTrees()` and appended to the CLI's tree table as a second
  group.
- **Nothing is created by resolving.** `ScratchDumpsClassRoot` joins the path and returns
  it; the walk reports a missing root as UNCONFIGURED. A sweep that materialised the tree
  it bounds would make "the writers never ran here" and "the tree is empty" render the
  same.

## Gates

| gate | what it pins |
|---|---|
| `internal/scratch/scratch_dumps_test.go` (whole file) | tree names as VALUES on both sides; class leaves against the WRITERS' source (`minimal_core.go`, `error_1214.go`, `scratch.go`); the two windows agree; each census tree has a declared row of the same name whose family is `bound` and whose root is the writer's own join; the plan collects an aged dump, keeps a fresh one, ignores a foreign file, and never takes the declared root |
| `cmd/agent/cli_scratch_gc_scratch_dumps_test.go` | E2E through the real binary: `--tree scratch-dumps` exits 0 and prints tree/root/family/decision; `--list` names both scratch trees with the WRITER's roots; `--tree dumps` still refuses and names the profile root |
| `internal/standards` `TestConfigDoc*` | the two CONFIG.md rows still resolve (env names exist, catalog covered, `file:line` anchors live) |
| `devdoc/gopi-tools-maintenance-help-surface-inventory.md` | unchanged: the two `--tree NAME` / `--list` forms were already typed |

## The cross-package literal pair, and why it is unavoidable

`ScratchDumpsTreeName` (internal/scratch) and `sessionlayout.ScratchDumpsTree` are the
same string in two packages that cannot import one another. The class directory names
are the same situation against `internal/deepseek`. Rules 13/14 ban a bare literal, so
each pair is held together by a test that reads BOTH sides — the value comparison where
both are Go values, and a source read where the far side is unexported in an importing
package.

## Source anchors

| what | file:line |
|---|---|
| the class trees' declared rows + constants | `internal/scratch/scratch_dumps.go:1` |
| the census trees + the name→root seam | `internal/sessionlayout/scratch_dumps.go:1` |
| the writers' documented default | `internal/deepseek/minimal_core.go:41`, `internal/deepseek/error_1214.go:55` |
| the class dir + scratch root join | `internal/scratch/scratch.go:322`, `internal/scratch/scratch_dumps.go:154` |
| the rows registered into the sweep | `internal/scratch/declared_trees.go:513` |
| the CLI's census tree table | `cmd/agent/cli_scratch_gc.go:955` |
| the docs' rows | `CONFIG.md:655`, `CONFIG.md:656` |

## Verification (this run)

See `## Verification` in the task report: `go build ./...`; `make test-budget` over
`./internal/scratch ./internal/sessionlayout ./internal/config ./cmd/agent
./internal/standards`; and the verbatim before/after transcript of the four census
invocations (kept at `…/sessions/01a12bb2-a3e3-7da9-8a96-20b08a05b05d/artifacts/census-before-after.txt`).
