<!-- doc-class: historical -->
> **HISTORY (added by the restore row).** `internal/tools/merge_config_anchor_gate.go` and its
> test file were deleted whole in commit `5a35f6104` (the `internal/docclass` teardown). Every
> occurrence below is a RECORD of that lane while it existed — **not** a command to run and not
> a check that runs today. That lane's live check **was** the census generator's `--check` leg
> plus the arms restored under the same documented name `TestMergeConfigAnchorGate`, which drove
> it rather than any merge-time lane. **BOTH WERE DELETED (2026-11-05)**: `78bdf05a`
> (operator ruling) removed the documentation-citation family — `config-anchor-census.go`, its
> `.tsv`, `citation-census.go`, `citation-selector-census.go` and the gates that drove them —
> and `b8cad761` removed `internal/tools/merge_config_anchor_gate_test.go` with the last census
> arm. Nothing judges CONFIG.md's anchors today: `go run` on the generator now prints
> `stat … no such file or directory`, and `-run TestMergeConfigAnchorGate` prints
> `[no tests to run]` and exits 0 (an EMPTY GREEN, not a pass). The anchor rules are prose
> authors obey; a reader must not run either command this note used to give.
# The bug-row LEDGER LOCUS — "filed" must name the file it was filed into

**Status:** shipped (branch `task/01a14438ceb7777ea49bad082f37aa03`)
**Trigger:** task `01a14438` — the observability half of bug row `01a142da`.

## 1. The defect, measured

Three merger lanes end a successful filing with one line:

```
[task-notify] merger: CONFIG-ANCHOR gate filed bug row: CONFIG-ANCHOR gate: the merge TARGET carries 1 rotted and 0 gone CONFIG.md anchor(s)
```

An operator read that, ran `gopi task list`, got **0 hits**, and had no way to
distinguish three worlds:

1. the write was lost (a real defect),
2. the write landed in a ledger this process resolved but the operator's CLI does
   not — the **ordinary** case inside a test, where
   `internal/testutil` pins `GOPI_MEMORY_DB` to a per-test sandbox (Rule 32),
3. the write landed in the operator's ledger and the reader filtered wrongly.

Task `01a142da` proved (via a throwaway probe test) that the answer was (2):
`config.LedgerPath()` inside the suite returned
`/dev/shm/suite-tools-run-<pid>/testdb-<rand>/memory.db`, not
`~/.gopi/memory.db`. The line was **honest** — `TaskSubmit` really succeeded — but
not **informative**, which is Rule 15's shape one level up: "found nothing" and
"never scanned" must not look alike; so must "filed" and "filed somewhere
invisible".

## 2. The fix — one resolver, one clause

`internal/tools/task_bug_row_ledger.go`:

```go
func bugRowLedgerLocus() string {
    path, source := config.ResolveLedger(config.LedgerNoFlag)
    ...
    return fmt.Sprintf("(ledger %s via %s)", path, source)
}
```

The three filing sites append it:

```go
mergeConfigAnchorLog("filed bug row: %s %s", summary, bugRowLedgerLocus())
mergeTargetDocGateLog("filed bug row: %s %s", summary, bugRowLedgerLocus())
mergesetGateLog("filed bug row: %s %s", summary, bugRowLedgerLocus())
```

Delivered line (real `-v` output, 2026-10-16):

```
[task-notify] merger: CONFIG-ANCHOR gate filed bug row: … anchor(s) (ledger /dev/shm/suite-tools-run-3567861388/testdb-3686823269/memory.db via GOPI_MEMORY_DB)
[task-notify] merger: TARGET-DEVDOC gate filed bug row: … (ledger /dev/shm/…/testdb-3686823269/memory.db via GOPI_MEMORY_DB)
[task-notify] merger: MERGESET gate filed bug row: … (ledger /dev/shm/…/testdb-3686823269/memory.db via GOPI_MEMORY_DB)
```

### Why `config.ResolveLedger` and not a second derivation (Rule 2)

The write path is `TaskSubmit → memoryDBPath() → config.LedgerPath() →
config.ResolveLedger(LedgerNoFlag)`. The locus calls the **same** function with the
same argument (no caller here parses `--db`, so `LedgerNoFlag` is the honest
value; rank 1 is stated by its owner, and the merger owns no flag). Consequences
worth keeping:

- A new channel added to the ranking is reflected here with **no edit** in
  `internal/tools` — the resolver is the only reader of the ranking.
- The channel NAME printed is the resolver's own source constant
  (`config.MemoryDBEnv`, `config.LedgerSettingsKey`, `config.LedgerSourceDefault`,
  `config.LedgerSourceFlag`), so no channel spelling lives a second time here
  (Rule 1/13/14).
- It **cannot** disagree with the file `TaskSubmit` opened, which a re-derived
  path (reading `GOPI_MEMORY_DB` by hand, or `os.Getenv("HOME") + "/.gopi"`)
  silently could — that is the exact class of defect that produced the
  `config show` mismatch this resolver exists to prevent.

### The last-resort branch

`config.ResolveLedger` returns `("", config.LedgerSourceDefault)` when no profile
home resolves; `memoryDBPath()` then falls back to its CWD-relative
`memoryDefaultDBFile`. The locus prints **that** file plus an explicit note
(`no profile home resolved … CWD-relative last resort`), because "the row went to
`./memory.db` wherever the merger was standing" is the worst possible place for a
write to be filed silently. This is reporting only: `memoryDBPath` itself is not
changed.

### What is deliberately NOT changed

- `internal/testutil/ledgerpin.go` / `dbfixture.go` — the pin is correct; the fix
  is to SAY where the row went, never to move it (Rule 32).
- The summary / payload / dedup identity of any lane — the locus is appended to
  the LOG line, not to the row. `TestMergesetGate_BugSummary*` still passes.
- The other diagnostic lines (`bug row NOT filed …`, `already a bug row …`). They
  already name a reason; their own visibility question is a separate change.

## 3. The regression guard

`internal/tools/task_bug_row_ledger_test.go`:

- `TestBugRowLedgerLocus_NamesTheExactPinnedLedger` — the helper names the pinned
  path exactly once AND the deciding channel (`config.MemoryDBEnv`). It first
  asserts `config.LedgerPath() == testutil.NewTestDBPath(t)`, so the fixture is
  proven to be the same file `TaskSubmit` opens.
- `TestBugRowLedgerLocus_AllThreeFilingSitesCarryIt` — drives the **three real
  filing functions** (`mergeConfigAnchorReport`,
  `mergeTargetDocGateFileBugRow`, `mergesetGateFileBugRow`) and asserts the
  **captured stderr** carries `(ledger <pinned path> via GOPI_MEMORY_DB)`. A
  helper that exists but is not called at a site is invisible; this arm is what
  makes a forgotten call site red.

### Red proof (measured, both directions)

1. **Locus removed** (`bugRowLedgerLocus` forced to return `""`):

```
--- FAIL: TestBugRowLedgerLocus_NamesTheExactPinnedLedger
    the locus must name the file TaskSubmit writes (/dev/shm/suite-tools-run-20725349/testdb-3096596063/memory.db), got ""
--- FAIL: TestBugRowLedgerLocus_AllThreeFilingSitesCarryIt/CONFIG-ANCHOR
    the CONFIG-ANCHOR site's `filed bug row` line must name the ledger it wrote ((ledger /dev/shm/…/testdb-1922398099/memory.db via GOPI_MEMORY_DB));
--- FAIL: …/TARGET-DOC   --- FAIL: …/MERGESET
```

2. **One call site reverted** (CONFIG-ANCHOR only, helper intact):

```
--- FAIL: TestBugRowLedgerLocus_AllThreeFilingSitesCarryIt/CONFIG-ANCHOR
    the CONFIG-ANCHOR site's `filed bug row` line must name the ledger it wrote ((ledger /dev/shm/…/memory.db via GOPI_MEMORY_DB));
```

Restored, and both arms green. The restore was verified by content identity of the
two touched files (backup copies kept OUTSIDE the worktree, in the session's
`artifacts/`) — see the task report's `## Evidence (self-filed)`.

## 4. Pre-existing red, not this change

`TestMergeConfigAnchorGate_RealTreeIsClean` and two siblings are **red on the base
tree itself** (8 ROT findings in `cmd/agent/*` measured before any edit of this
run; the tree is a linked worktree whose copies of `cmd/agent/main.go` etc. have
moved ahead of the census table). The ledger already carries this as
`01a144d7-e39e-7e5f-9cf8-40e2fae85e95`. This change touches neither the table nor
those files.

## 5. What a future reader should take away

A log line that reports an EFFECT of a write must report the write's DESTINATION,
derived from the same resolver the write used. "It succeeded" is not evidence a
reader can act on; "(ledger <path> via <channel>)" is — it turns a 0-hit
`gopi task list` into a diagnosis instead of an alarm.
