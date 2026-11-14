<!-- doc-class: historical -->
# The `session-scratch-dir` bound: the container's lifecycle, not the directory's name

**Task**: `01a0f522-152b-7921-890e-25e03f0276e3`
**Branch**: `task/01a0f522152b7921890e25e03f0276e3`

This is the **design rationale** for the bound the census document
(`devdoc/sessions-tree-scope-census.md` §2c) records as a decision. The census
carries the measured shape and the family table; this carries the mechanism, the
two corrections the first version needed, and what an extender must not break.

## The problem

The sessions tree is `~/.gopi/sessions/<container>/<entry>`. The container is the
unit the ledger knows: `internal/tools` mints one per run, the session readers
resolve a task id *through* the `metadata.json` inside it, `gopi task show` prints
a container's artifacts through it, and a record may be RESUMED from it. Beside
that, a run leaves DIRECTORIES inside its own container under names it chose:
measured 2026-10-01, 277 directories in 165 distinct shapes (`evidence`, `evid`,
`ev`, `bin`, `artifacts`, `crops`, `figs`, `__pycache__`, `smoke-clone`,
`master-check`, `probeN`, `acceptN`, …).

The census recorded the honest verdict `keep`: "the name is the run's own choice;
no predicate tells a dead one from a live one". **That verdict was about the NAME
and it was right about it** — the declared-tree registry is nothing but namespace
predicates (`go-build*`, `gopi-raw-*`, `*.raw.log`), and no namespace predicate
can enumerate 165 run-chosen shapes.

## The way out: ask one level up

`containerHasNoLedger` (`internal/scratch/sessions_scratch_dir.go`) stops asking
"which name is this directory?" and asks **"has the ledger finished with this
container?"** — a question with an answer, because the ledger either describes
the container or it does not. It is read with ONE `os.ReadDir` of the container
and answered FALSE by anything short of proof.

## The contract this arm MUST keep

Each of these is a denial, not an omission, and each has a test that fails if it
is broken:

1. **A directory inside a container the ledger describes MUST never be touched,
   at any age.** A container holding a `metadata.json` or any `*.jsonl` is a
   container `gopi task show` may still resolve through and a record a reader may
   still resume. 399 of the measured 430 directories live in such containers:
   this is the clause that protects almost all of them.
2. **A directory another family claims MUST be routed to that family, never
   decided here** — asked through `sessionlayout.ClassifyFamily`, the same call
   the census makes, so a name list in this file cannot drift from the registry
   and a NEW container family is respected without this file changing.
   `attachments-archive` (KEEP: a deleted branch's only surviving copy is an
   attached bundle), `foreign-checkout` (legacy: a probe's whole `git clone`) and
   `sandbox-profile` (legacy: a materialised `.gopi`) all live inside containers.
3. **The window MUST be the arm's own** — `SessionScratchDirStaleAge`, applied by
   `scratchDirWindow` as a FLOOR under the caller's value. A caller may ask for a
   LONGER window (a sweep face may always be more conservative); a shorter one
   MUST NOT shorten this arm.
4. **A symlink MUST never be a candidate** — removing a link removes its target
   with it.
5. **Liveness MUST be the EXISTING verdicts** — the tree's one indexed `/proc`
   pass (`heldSet` / `heldIn`) plus the `.holder` registration
   (`unheldRegistered`), never a new one.
6. **Every unknown MUST resolve to KEEP** — an unreadable container, an
   unmeasurable mtime, an unclassifiable directory.

## The arm is the EXISTING one, extended

`internal/scratch/declared_trees.go`'s `session-raw` row already walks this tree
one level down and already refuses to remove a container that is not empty of its
own candidates; what it could not do was judge a NESTED DIRECTORY — it counted
one as `keptFiles++` and kept the container forever. The change adds one hook
where the arm already stands:

```go
if cand := containerScratchDirEntries(tree, childPath, path, now, age, held); len(cand) > 0 {
    out = append(out, cand...)
    continue
}
keptFiles++ // unowned or young: the container is not emptied by it
```

So there is one tree, one walk, one plan/sweep pair and one answer to "what is in
this container" — no second row over the same root, no second schedule, no second
window to keep in step, and **no new artifact** (Rule 35): no script, no cron
line, no state file, no config key.

## Two corrections the first version needed, both pinned by a test

### 1. The window was dead code — and the direction it was wrong in cost evidence

`containerEntries` hands its hook the same `age` it judges the row's OWN
candidates by, and for `session-raw` that is `StaleAge` = **14 days**, the
`*.raw.log` FILE arm's window. The first version used that value directly, which
made `SessionScratchDirStaleAge` (the 30-day constant the census doc, the
constant's own comment and the registry row all describe) **read by nothing** —
and made a 15-day-old evidence directory inside a forgotten container
collectable: a deletion the documented decision does not authorise, in the
direction that costs the evidence a report cites by name.

`scratchDirWindow` now floors the caller's value at the arm's own.

**Red proof**: `TestSessionsScratchDirWindowFloorIsTheArmsOwn` — a 15-day
directory (past the file arm's window, inside the floor) must not be in the plan
under the row's own window, and must be refused by the hook even when handed a
1-hour caller window.

### 2. The fixture could not see its own failure

Creating a child moves the PARENT's mtime — so a container backdated before its
directory was seeded read as **freshly touched**, which is exactly the state the
predicate protects ("a run in flight"). The fixture now builds everything first
and ages LAST, and that reason is a comment on `seedScratchDir` so the next
author does not re-bury it. The sibling bug: an observing wrapper must be handed
the previous hook as its `inner`, because calling the hook by name re-enters the
wrapper (measured — it overflowed the stack on the first run).

## What an extender must not do

- **Do not add a name list** of the families to route away: ask
  `ClassifyFamily`. The list would be a second registry.
- **Do not pass the row's `StaleAge` straight through**: it is a FILE arm's
  window.
- **Do not "simplify" the fixture's build-then-age order**, and do not seed a
  container's children after aging it.

## Constants

| constant | value | where |
|---|---|---|
| `SessionScratchDirStaleAge` | 30 d (a FLOOR, not a value) | `internal/scratch/declared_trees.go` |
| `TreeNameSessionRaw` / `TreeSessionRaw` | `"session-raw"` | `declared_trees.go` / `sessionlayout/families.go` |
| `FamilySessionScratchDir` | `"session-scratch-dir"` | `sessionlayout/families.go` |

## Evidence

```
$ go test ./internal/scratch/ -run TestSessionsScratchDir -count=1 -v
--- PASS: TestSessionsScratchDirSweepIsTwoSided (4.36s)
--- PASS: TestSessionsScratchDirWindowFloorIsTheArmsOwn (1.16s)
--- PASS: TestSessionsScratchDirSweepHookIsTheOneThatDecides (3.93s)
ok  github.com/nick/gopi/internal/scratch

$ ./bin/gopi scratch-gc families | grep -o 'decision=[a-z]*' | sort | uniq -c
    178 decision=bound        # was: keep — 165 session-scratch-dir shapes + the sibling
   1237 decision=keep        #              attachments family, which landed on master
     60 decision=legacy

$ ./bin/gopi scratch-gc trees          # the REAL sweep, real delete
scratch-gc role=cli-trees home= removed=106 stale=336h0m0s tree=session-raw bytes=1413268851
```

**The live sweep is a NO-OP for this family today, by design.** Measured after
the change: 430 non-attachment directories, oldest mtime 14.80d; the 25
no-record containers hold 31 directories, oldest 7.97d — all inside the 30-day
window. The 106 removals above are the `*.raw.log` FILE arm, whose behaviour this
change did not alter. (The sweep is idempotent, so REPLAYING the command after it
has run prints `removed=0 tree=session-raw bytes=0`: those 106 entries are gone.
The number above is this run's record, not a per-run constant.)

## Not done

- `attachments-archive` stays `keep` (its own task; the writer
  `internal/tools/attachments_archive.go` is not on this branch).
- The fixture does not synthesise a container whose `metadata.json` arrives after
  the window: that shape cannot occur (the window is a month), and a fixture for
  it would assert an impossibility rather than a behaviour.
