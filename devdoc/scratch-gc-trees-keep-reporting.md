<!-- doc-class: historical -->
# `gopi tool scratch-gc trees --explain` — the keep half was never COMPUTED

**Date:** 2026-10-16 · **Surface:** `gopi tool scratch-gc trees` (+ `--dry-run --explain`)
**Packages:** `internal/scratch`, `cmd/agent`

## The defect (measured, filed by the briefing's run)

`gopi tool scratch-gc trees` reported only the entries a sweep WOULD REMOVE. The
keep half was not merely unprinted — it was never **computed**:

- `declaredTreeEntries` returns candidates and drops every refusal at its
  `continue` sites;
- `declaredEntryCollectable` answered with a bare `bool`, so **no reason string
  existed at the decision point**;
- `runScratchGCTrees` took `(dryRun, selector)` — the parsed `--explain` flag
  reached the parser and **nothing else**.

Consequence: *"this entry is protected, and here is why"* and *"this entry was
never a candidate in the first place"* printed the **identical thing — nothing**.
That is exactly the question `--explain` exists to answer, so the flag's only
readable output on this arm was the plan it already printed without it.

## The fix — ONE predicate, two faces

### 1. `internal/scratch/declared_trees_verdict.go` (new)

`declaredEntryKeepReason(tree, path, now, age, held) string` is the **one**
decision point. It answers with the sentence that decided it, or `""` when the
entry is collectable. `declaredEntryCollectable` **delegates** to it:

```go
func declaredEntryCollectable(...) bool {
    return declaredEntryKeepReason(...) == ""
}
```

The delete path is therefore bit-for-bit unchanged — the sweep still deletes iff
the predicate says so; it just says so in words now. A second predicate in the
report would have drifted from the sweep the first time either half changed, and
a report that drifts is worse than no report: it can be **wrong about a delete**.

Each keep names itself, and no two are rendered the same (Rule 15):

| clause | sentence |
|---|---|
| root | `EntryKeepDeclaredRoot` (the declared root survives by design) |
| unmeasurable | `EntryKeepUnmeasurable: <err>` |
| young | `EntryKeepFresh (<window>)` |
| unfinished /proc pass | `EntryKeepScanIncomplete` — **not** `EntryKeepLiveHolder`; an unfinished scan observed nothing |
| indexed /proc hit | `EntryKeepLiveHolder (the .holder scan saw a process inside it)` |
| registered owner | `EntryKeepLiveHolder (<OwnerVerdictFor reason>)` — the owner's own sentence, e.g. `the recorded owner is live` |
| container held | `EntryKeepContainerHeld` |
| container unreadable | `EntryKeepUnmeasurable: <err>` |
| container still pinned | `EntryKeepContainerNotDrained (<n>)` |
| container young (own window) | `EntryKeepFresh (<RootStaleAge>)` |
| a container child another family owns | `EntryKeepLegacyKind` |

### 2. `DeclaredTreeVerdicts` — the reporting twin of the same walk

Same root resolution, same `readTreeRoot`, same single `/proc` pass
(`ScanHolders` + `heldSet`), same order, same `TreeEntryScanLimit`, same
predicate. It answers `([]EntryVerdict, TreeKeepSummary)`:

- a verdict for the tree **ROOT** (`EntryKeepDeclaredRoot`), always;
- a verdict per judged direct entry — and for a container-kind tree, the
  container's **own** keep, plus a verdict for each child it looked at;
- a `TreeKeepSummary{Examined, NotOurs, Collectable, Kept, KeptReasons,
  Truncated}`.

An entry the tree's **naming rule** refuses (a foreign name in a shared tree) is
**not** a verdict line — reporting the neighbours would bury the answer, which is
the base arm's own measured decision — but it **is** counted (`not_ours`), so
nothing is silently skipped.

The container's `--explain` reading is the one the briefing could not get
anywhere: `verdict=keep … the container still holds entries that are not
collectable (2)`.

`declaredTreeVerdicts` (the queue arm's older twin) is **gone**: the queue's
`queueVerdicts` now calls the same `DeclaredTreeVerdicts`, so two arms judge one
tree one way instead of two.

### 3. `cmd/agent/cli_scratch_gc.go`

- `runScratchGCTrees(dryRun, **explain**, selector)` — the flag is read now.
- Under `--explain`, one `scratch-verdict`-shaped line per judged entry (the
  **shared** label/verdict/kept-mark constants the default arm uses), plus a
  `kept-by-reason:` distribution line.
- **Unconditionally** (flag or not, dry-run or live), one **summary** line per
  selected tree:

  ```
  tree-summary tree=session-raw examined=4 not_ours=0 collectable=1 kept=3 truncated=false
  ```

  This is the line that makes an empty plan impossible to misread as proof of
  protection: four states that all used to print nothing are separated by it —
  the arm never ran, the tree does not exist here, the tree was walked and every
  entry was protected, the tree was walked and had no entry.

The summary is a **separate, additive** line, not fields appended to
`scratchGCTreesLogFmt`: existing tests and an operator's log filters parse that
shape, so its fields must not move. Exit-status semantics are unchanged.

## Evidence (the acceptance, run for real — Rule 31)

Binary pair kept in this run's session dir (`…/01a142ef-…/artifacts/`):
`gopi-prefix` (pre-fix, built from the same worktree before the change) and
`gopi-fixed`. Probe: `artifacts/probe.sh`, output `artifacts/probe-noholder.txt`.

Fixture (sandbox `GOPI_HOME`, never the operator's trees): a sessions-tree
container holding an aged `<session>.raw.log` **and** a `session.jsonl` record.

```
--- PREFIX: scratch-gc trees --dry-run --explain --tree session-raw
scratch-gc role=cli-trees … tree=session-raw present=true entries=0 bytes=26 …
would-remove …/0f1e…/0f1e….raw.log tree=session-raw age=360h0m0s bytes=26
                          <-- and NOTHING about the container. Silence.

--- FIXED: the same command
…
scratch-verdict …/sessions/0f1e…/scratch verdict=keep … this tree's decision is per KIND, not per age [kept]
scratch-verdict …/sessions/0f1e…/session.jsonl verdict=keep … not this system's namespace [kept]
scratch-verdict …/sessions/0f1e… verdict=keep … the container still holds entries that are not collectable (2) [kept]
kept-by-reason: 1× not this system's namespace; 1× the container still holds entries that are not collectable (2); …
tree-summary tree=session-raw examined=4 not_ours=0 collectable=1 kept=3 truncated=false
```

The second probe — **with a live holder inside the container** — flips the same
container to `verdict=keep … a live process holds the container`, and its death
flips it to `would-remove`, proving the line tracks the predicate and not a
constant (`TestScratchGCCLITrees_HolderDeathFlipsTheKeepToWouldRemove`).

## Tests

- `internal/scratch/declared_trees_verdict_test.go` — **ONE fixture, four
  reasons**: aged (collectable) / young (`EntryKeepFresh`) / held
  (`EntryKeepLiveHolder` + `OwnerReasonLive`) / namespace miss (counted, not a
  verdict line); the container keep; the truncated-scan keep (driven by an
  expired deadline, never by load — Rule 27); and the anti-drift pin
  `TestDeclaredEntryCollectable_IsTheReasonFace`.
- `cmd/agent/cli_scratch_gc_trees_explain_test.go` — the REAL verb, sandboxed:
  the held container's keep line, the session record's keep line, the root's
  line, the summary line, the reason distribution, `--dry-run` deleting nothing,
  the holder-death flip, and the summary printing **without** `--explain`.

**Fixture pitfall measured while writing these:** `RegisterHolder` writes the
`.holder` record *inside* the entry, which refreshes the entry's own mtime — a
held fixture that is not re-aged afterwards is kept by the **clock** and the test
passes while pinning nothing. Both layers re-age after registering.

## Not done

Nothing deferred: the delete path is unchanged, and both layers are covered.
