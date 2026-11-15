<!-- doc-class: normative -->
# The declared-tree holder index was spelled — a symlinked root made the arm's only liveness veto a no-op

**Task**: `01a136d9-2b54-7166-83ec-101f92cdeca3` · **File**: `internal/scratch/declared_trees.go`
(`heldSet`/`heldIn`/`rootSpellings`/`relativeUnderAny`/`joinUnder`/`insertChain`) ·
**Test**: `internal/scratch/declared_trees_spelling_test.go`

## The defect

`heldSet` indexed a tree's `/proc` holder paths by LITERAL spelling: `filepath.Clean`
on the holder, then a lexical `strings.HasPrefix` test against the tree ROOT as the
walk spelled it. The `/proc` channel reports what the KERNEL resolved
(`/proc/<pid>/cwd` is the kernel's own path), and a declared tree's root can be
reached through a symlinked spelling — a scratch home linked elsewhere (the retired
`scripts/setup-scratch-home.sh` is the recorded case), a `GOPI_WORKTREE_ROOT` under
a linked parent. The two sides then never meet, and the consequences compound:

1. A holder spelled `…/live-root/gopi-build-cache/<key>` does not even START with
   the walk's root `…/linked-root/gopi-build-cache`, so the existing
   "outside the tree" skip discarded it (`continue`);
2. the index therefore held no key at all for the held directory, and `heldIn`
   answered **false** for the exact situation the veto exists for — a live process
   standing inside the candidate.

`declaredEntryCollectable` reads that false as "nobody holds it", so the arm's only
liveness veto became a no-op and the sweep fell through to age alone. This is the
fail-open direction in a package whose contract is **"every unknown is a KEEP"**:
the tree arm would have deleted a directory a live `go build` (or any process) was
standing in.

## Measured (this host, 2026-10-14)

Probe, root spelled through a link (`linked` → `real`), holder path as the kernel
reports it:

```
PRE-FIX   heldIn(literal run-a)=false  heldIn(resolved run-a)=false  index keys=0
POST-FIX  heldIn(literal run-a)=true   heldIn(resolved run-a)=true   sibling=false keys=6
```

Real CLI verb, `GOPI_WORKTREE_ROOT=<linked root>` (worktreeroots.canonicalRoot only
`Clean`s, so the row's `Resolve` returns the LINK spelling), a live process standing
in key `0123456789abcdef`, an equally stale UNHELD sibling `fedcba9876543210`:

```
pre-fix  binary: tree=build-cache removed=2  … would-remove …/linked-root/…/0123456789abcdef
                 (the held key IS handed to the sweep) and …/fedcba9876543210
post-fix binary: tree=build-cache removed=1  … would-remove …/linked-root/…/fedcba9876543210
                 (only the unheld control)
```

Evidence: the task's session directory, `artifacts/verb-evidence/{verb-prefix.txt,verb-fixed.txt}`.

## The fix, and why it is per ROOT and not per holder

`heldSet` now indexes every holder chain under **every spelling of the tree root**:

* `rootSpellings(root)` — the literal spelling plus `CachedCanonicalPath(root)`.
  Memoized, and a no-op on every tree reached without a symlink (most of them), so
  the cost is at most ONE resolution per tree.
* `relativeUnderAny(holder, roots)` — locate the holder under whichever root
  spelling it matches, LEXICALLY (the same rule the candidate side applies, so the
  two can only meet where the spellings already agree).
* `joinUnder` + `insertChain` — re-mount the same relative tail onto every root
  spelling, so one directory on disk has a key under both names and a candidate
  spelled either way finds it.

**The rejected alternative, and it is rejected on measurement**: resolving each
holder path. `HolderScan` on this host carries **55,818** paths per pass, and a full
resolve pass costs **387–995 ms** (70 of them change spelling). It would not have
worked either — a resolved holder does not lexically start with the literal root, so
the outside-the-tree skip discards it *before* any spelling index can help. The chain
must be **re-based onto the root's spellings**; indexing a holder under both of its
own spellings fixes nothing.

**No resolution is added per candidate** — that is the measured trap this index was
built to avoid (`CachedCanonicalPath`: per-candidate `EvalSymlinks` cost ~100 s on a
75k-entry tree). A candidate is still one map lookup on the single spelling the walk
handed it: `heldIn(path)` is unchanged.

## Verification recipe (reusable)

Fixture: `t.TempDir()` + `os.Symlink(real, link)`; a tree row whose `Resolve` returns
the LINK spelling; the holder is a REAL child process (`exec.Command("sleep", …)`)
with `Dir` set to the live candidate, waited on by polling `/proc/<pid>/cwd` (the
existing `legacyWaitForCwd` shape). **Assert the two spellings really differ**
(`readlink /proc/<pid>/cwd` must start with the resolved root and NOT with the
literal one) — without that assertion the test would pass for the wrong reason on a
host where nothing is linked. Assert on the PLAN face (`DeclaredTreePlan` →
`declaredEntryCollectable` → `heldIn`, the same predicate the sweep collects
through), and ALWAYS carry a positive control: an equally stale, unheld sibling that
must BE planned, or the test also passes for a plan that names nothing.

Red proof: copy the fixed file aside, `git checkout HEAD -- <file>`, re-run (must
FAIL, three messages), restore from the copy and verify with `sha256sum` — never an
index-relative diff (Rule 5).
