<!-- doc-class: normative -->
# The resume path's dirty-ground contract, split in two (and repaired)

Task: `01a115ed-21da-72cd-b637-3eb1b8fce72b`
Branch: `task/01a115ed21da72cdb6373eb1b8fce72b`
Worktree: `/home/user/workspace/gopi-01a115ed21da72cdb6373eb1b8fce72b`

## The report's question, answered with evidence

The briefing asked: *which call removes the dirty worktree, and why did
`reclaimSkipDirtyReason` not stop it?*

**No product defect exists on this branch.** The removal is
`git worktree remove <wt> --force` at `internal/tools/task_finish_cleanup.go:4170`
(the `finishCleanupServerRecycle` arm, reached from the resume close-out), and it
ran **because it was allowed to**: the rescue arm
(`internal/tools/task_finish_cleanup_salvage.go`, epic `01a11548`, commit
`8d3b4b19e` — an ancestor of both this branch's base `93848040e` and master)
RECLAIMED the residue deliberately. The refused-and-stranded contract the test
asserted was replaced on 2026-10-07 by *rescue, then reclaim*, and that epic
narrowed its six affected tests in `./internal/tools/` — leaving
`cmd/agent/cli_resume_cleanup_e2e_test.go` asserting the pre-rescue contract for
a class the rescue arm releases on purpose.

### The mechanism, measured (not inferred)

`GIT_TRACE2_EVENT=<file>` around the failing test, grepped for the argv:

```
"argv":["git","commit-tree","…","-m","salvage: residue of run t-resume-cleanup-e2e (delivering feat/resume-e2e)"]
"argv":["git","update-ref","refs/gopi/residue/t-resume-cleanup-e2e-c4b188f6fcac","…"]
"argv":["git","worktree","remove","…/wt","--force"]
```

(the first two are the rescue, the third is the removal that follows it.)

### The two shapes, run through the REAL binary (Rule 31)

A throwaway probe test (`cmd/agent/zz_probe_dirty_contract_test.go`, deleted
before close-out) seeded both dirty states through
`gopi session resume <rawLog> --task <id>`:

| seeded dirt | porcelain | what the chain did | correct? |
|---|---|---|---|
| **tracked** edit of `b.txt` | `M b.txt` | refused; `[task-notify] run … closed out with UNCOMMITTED TRACKED WORK … entries: M b.txt`; the `completed` writeback was **REFUSED** (`gopi task ledger: close failed … the row stays open`); the directory survived | **yes** — an uncommitted deliverable keeps its tree |
| **untracked** `uncommitted.txt` | `?? uncommitted.txt` | `salvage` step OK, ref `refs/gopi/residue/t-resume-cleanup-e2e-1bb369636692` carrying `a.txt \| b.txt \| uncommitted.txt`; then the ground was removed | **yes** — the bytes are retrievable (`cat-file -p <ref>:uncommitted.txt` → `"wip"`), nothing was deleted |

So `reclaimSkipDirtyReason` was never bypassed: it is the **tracked** arm's
wording. The untracked arm is `reclaimUntrackedResidueMarker`, and
`finishCleanupDirtyReasonIsResidue` deliberately matches only that one (the
asymmetry is argued at length in the salvage file — a tracked change refuses the
`completed` writeback, so the row stays OPEN and the ground belongs to an
unfinished run).

## The repair

1. **`cmd/agent/cli_resume_cleanup_e2e_test.go` — `TestResumeKeepsADirtyWorktree`
   now seeds its own subject.** It wrote `wt/uncommitted.txt`, an *untracked*
   file, while its prose said "uncommitted work: the deliverable the branch
   needs" — the two claims point at different dirty states. It now edits the
   **tracked** `b.txt` (`M b.txt`), the state whose survival the test's name and
   prose actually describe, and its header states the split. The test name, its
   assertions and the product's `reclaimSkipDirtyReason` are untouched.
2. **A lost contract is now pinned, not just loosened** (Rule 5/10.5): the
   untracked half had no `cmd/agent` test once the assertion moved off it, so
   the new `TestResumeRescuesUntrackedResidueBeforeRemoval` asserts the *positive*
   contract — the residue's bytes are read back **out of the rescue ref with git**
   and compared, then the ground is asserted released. A removal whose copy is
   missing fails it.
3. **`internal/tools` exports `ResidueRefPrefix()`** — the rescue namespace
   `refs/gopi/residue/` now has a second consumer outside its package (the new
   test, and any operator retrieval), and Rule 14 forbids re-typing the literal.
   It is a niladic accessor named for its value (Rule 20), a one-line alias of
   the single `finishCleanupSalvageRefPrefix` definition.

Nothing in the product's behaviour changed: the `--force` removal, the rescue,
the tracked refusal and the ledger writebacks are exactly as `8d3b4b19e` left
them.

## Why the fix is this and not "make the test stop removing the worktree"

- **The removed content was recoverable, and the test could not tell.** The
  contract the test encoded was genuinely traded away for a better one two days
  earlier; re-asserting it would strand finished runs' grounds for ever, which is
  the exact failure epic `01a11548` was filed to end.
- **Weakening the test was not needed and did not happen.** The names, the
  directory-survival assertion and the `dirty`-reason assertion are all still
  asserted — against the dirty state they were written about.
- **The `cmd/agent` scope was the blind spot.** The rescue epic's own report
  lists its narrowed tests, all in `./internal/tools/`; nothing re-ran the
  `cmd/agent` half. Two contracts in one feature is how this survived a green
  epic.

## Not Done (honest)

- **The briefing's framing ("data loss on the resume path") is corrected, not
  confirmed.** No product code was changed, because none was wrong. If the
  operator's intent was *"the resume path must never reclaim any dirty ground,
  including the residue class"*, that is a **reversal of epic `01a11548`'s
  ruling** and belongs in its own task — it cannot be reached by this repair
  without re-stranding the grounds that epic freed.
- **The rescue refs have no reaper** (carried over from `8d3b4b19e`'s own
  "Not Done"): unchanged here, and deliberately not invented (Rule 35).
- **The broader `cmd/agent` red family** reported alongside this one by task
  `01a115b6…` was not triaged here.
- **The removed probe test's evidence is quoted, not shipped**: the probe file
  was deleted before close-out (it was this run's scratch, Rule 30); its measured
  output is transcribed in this document and in the report's `## Evidence`.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK
- Vet: `go vet ./cmd/agent/ ./internal/tools/` → exit 0
- Tests (branch-scoped, Rule 27): `make test-budget PKGS='./cmd/agent
  ./internal/tools'` → see the task report's `## Verification` for the run's own
  transcript.
- Ran: `CGO_ENABLED=0 go test ./cmd/agent/ -count=1 -run 'TestResumeReclaimsTheWorktreeItFinishedIn|TestResumeKeepsADirtyWorktree|TestResumeRescuesUntrackedResidueBeforeRemoval|TestResumeTailDeclaresOwnershipBeforeTheSessionStarts' -v` <!-- selector-ok: RECORD of the run that produced this report, quoted verbatim. All four selectors died with the finish-cleanup mechanism (`58b5618b refactor(task): retire the finish-cleanup mechanism (slice 3/4)`), so they cannot match again; this page is the record of that delivery, not a recipe. -->
  → `--- PASS` ×4, `ok github.com/the operator/gopi/cmd/agent 7.664s`
- Red→green (each arm proved to fail without its own fix):
  - rescue call disabled (`if false && …finishCleanupDirtyReasonIsResidue…`) →
    `--- FAIL: TestResumeRescuesUntrackedResidueBeforeRemoval`
  - the untracked marker treated as tracked (`…Prefix || true`, i.e. the OLD
    "any dirt survives" contract) → `--- FAIL: TestResumeKeepsADirtyWorktree`
    (`a DIRTY worktree must survive the resume cleanup: … no such file or directory`)
  - both restored → PASS
