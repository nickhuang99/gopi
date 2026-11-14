<!-- doc-class: historical -->
# Rule 17 on master: the journalreport_test.go absence case

## Completed

- Fixed the 2 NEW Rule 17 sites at `internal/agent/journalreport_test.go:101/:104`:
  one reviewed `// env-leak-ok:` escape with the honest reason, on the offending
  statement's contiguous comment block (the gate's documented position).
- Verified the escape is *read*, not decorative: `TestGate2TestEnvRedProof` turns
  green and the DataReport Rule 17 line reads `hits=114 baselined=91 escaped=23
  NEW=0`.
- Falsified (behavioural probe, before committing) the first draft's premise that
  `t.Setenv`'s cleanup re-SETs a removed name to `""`; the redundant
  `SnapshotEnv` pair the draft added was therefore dropped — the final diff is
  16 insertions / 8 deletions in one file (`git diff --numstat master...HEAD`).
- `internal/standards/baseline.go` left byte-identical to master; no permit added,
  moved or removed.
- Devdoc written (this file) with the gate arithmetic, the red proof and the
  branch/recovery declaration.

## Not Done

- **The package-level `go test ./internal/standards/ -count=1` still exits 1** on
  this branch — for the pre-existing, unrelated Rule 16 + Rule 18 line-shift in
  `internal/tools/task_merge.go` described below, which is *also* red on master's
  untouched tip. Fixing it means re-keying two baseline entries, exactly the
  ratchet edit this task forbade; it is dispatched as its own task
  (`task_submit`, Rule 26) rather than folded in.
- The briefing's acceptance command `go test ./internal/agent/ -run TestJournalReport`
  names a test that does not exist in the tree; the covering tests were run
  instead (see Verification).

## The defect

`internal/agent/journalreport_test.go` (`TestSanitizeReportContract`) carries one
table case — *"no contract at all is a no-op"* — whose **subject is the
variables' ABSENCE**: `SanitizeReportContract` decides on *presence*
(`os.LookupEnv`), so the case must leave `taskreport.PathEnv` and
`taskreport.OwnEnv` genuinely ABSENT for the duration of the sub-test. The test
did this with two `os.Unsetenv` calls, which Rule 17 (test env isolation,
`testEnvFind` → `os.Setenv`/`os.Unsetenv` in test files) reports as 2 NEW,
unannotated violations.

## What the code actually requires (root cause, read — not assumed)

- **The removal is structurally required.** The guard under test reads presence,
  so the absent case cannot be expressed by *setting* a value; it must be a real
  removal. `t.Setenv` only SETS (after it, `os.LookupEnv` reports the name
  present), so it cannot express this case. The `os.Unsetenv` is the case's
  SUBJECT, not a leaked write.
- **The restore is already correct, and the escape reason had to say so
  honestly.** The first draft of this fix added `testutil.SnapshotEnv` defers on
  the theory that the `t.Setenv` cleanup would re-SET the removed names to `""`.
  A behavioural probe run before committing **falsified** that: the stdlib
  `func (c *common) Setenv` (`$(go env GOROOT)/src/testing/testing.go:1326`)
  snapshots with `os.LookupEnv` and registers `os.Setenv(prevValue)` /
  `os.Unsetenv(key)` accordingly — so the pre-test state (absence included) is
  restored either way, and the SnapshotEnv pair was redundant. The probe:
  sub-test A reproduces master's shape (unset a name that was unset, `t.Setenv`
  it, `os.Unsetenv` it again) and a sibling test B, running after A, reads
  `LookupEnv` → **absent** (PASS). Redundant code was dropped rather than kept
  to decorate the diff.
- **The reason kept is the one the tree already accepts.** The same shape and
  the same reason are in `internal/testutil/envsnapshot_test.go:21-22`
  ("the ABSENT state is this test's subject; t.Setenv cannot unset") — the
  escape documents *why the removal is the subject*, which is exactly this site.
  `SnapshotEnv` remains the only legitimate restore for a *hand-rolled*
  save/restore (`TestGate2EnvRestoreShapeRedProof`); no hand-rolled pair exists
  here.

## The fix (minimal)

`internal/agent/journalreport_test.go`, one test, **16 insertions / 8 deletions** (measured: `git diff --numstat master...HEAD -- internal/agent/journalreport_test.go`):

- the two `os.Unsetenv` statements collapse into one loop over the two names,
  each iteration carrying the gate's documented reviewed inline escape with the
  honest reason (`// env-leak-ok: absence is the SUBJECT of this case — the
  guard decides on PRESENCE (os.LookupEnv) … t.Setenv cannot express that …`);
- the table's `absent` doc comment now states *why* the restore is correct
  (the `t.Setenv` cleanup snapshots with `os.LookupEnv`).

The escape sits in the **contiguous comment block directly above** the flagged
statement (`classify` → `linesAbove` stops at the first non-comment line), which
is the documented escape position — `TestGate2TestEnvRedProof` goes green on the
next run, proving the marker is read. **No baseline entry was added or touched**:
`internal/standards/baseline.go` is byte-identical to master
(`git diff master -- internal/standards/baseline.go` → empty).

## Gate arithmetic (Rule 17)

| | master tip `0dac2ae7` (pristine) | branch `fix/rule17-journalreport-env-escape` |
|---|---|---|
| hits | 115 | 114 |
| baselined | 91 | 91 |
| escaped | 22 | 23 |
| **NEW** | **2** (`journalreport_test.go:101`, `:104`) | **0** |
| STALE | 0 | 0 |

114 = 91 + 23 + 0 — the DataReport identity holds. Two statements became one
loop-iteration site (+1 escaped), and the two NEW hits are gone.

## Red proof (the same command, unmodified parent commit)

Rendered in a detached worktree at master tip `0dac2ae7` with **no local
changes** (`git status --porcelain internal/agent/journalreport_test.go` empty):

```
$ CGO_ENABLED=0 go test ./internal/standards/ -count=1        # exit 1
  gate_test.go:99:  Rule 17 (test env isolation): 115 total hits, 91 baselined, 22 escaped, 2 new (unannotated), 86 baseline entries
  gate_test.go:111: Rule 17 (test env isolation): NEW violation (checklist 5.2) at internal/agent/journalreport_test.go:101
  gate_test.go:111: Rule 17 (test env isolation): NEW violation (checklist 5.2) at internal/agent/journalreport_test.go:104
  gate_test.go:124: Rule 17 (test env isolation) (checklist 5.2): hits=115 baselined=91 escaped=22 NEW=2
  gate_test.go:130: Rule 17 (test env isolation): 2 hit(s) are neither baselined nor escaped — TestStandardGates fails them, so this report would be lying about a green gate
```

Artifact: `/dev/shm/rule17-redproof.txt` (raw gate output). The probe worktree was
`/dev/shm/gopi-rule17-redproof` (created detached at `0dac2ae7` with no local changes for
this proof, and removed afterwards so no worktree residue is left behind); it measured
device `28` (via `stat -c %d`) while `/` measures `2051`,
i.e. **off the `/` partition** — the Rule 22 criterion, checked not spelled. (An earlier
probe at `~/workspace/...` was dropped: that path is device `2052` and lives on a block
filesystem; `/dev/shm` is memory-backed.)

## Pre-existing reds NOT in this task's scope (honest — Rule 26 corollary)

`CGO_ENABLED=0 go test ./internal/standards/ -count=1` **still exits 1 on this
branch**, for reasons that are identical on master's tip and untouched by this
fix:

- `TestStandardGates/TestGate1ErrorWrapRedProof` — Rule 16: NEW at
  `internal/tools/task_merge.go:366` with STALE baseline `:362`;
- `TestStandardGates/TestGate3ElseAfterReturnRedProof` — Rule 18: NEW at
  `internal/tools/task_merge.go:336` with STALE baseline `:332`;
- `TestStandardGateDataReport` — same two, reported by the identity check.

That is a pure **line shift** of two already-baselined sites: master's tip
commit `1a5e8ea7` ("feat(merger): refuse a branch whose MERGE RESULT does not
compile") inserted code above them, so master's own baseline line keys went
stale. It is a different gate pair and a different file from this epic, and the
baseline ratchet is exactly the thing this task must not touch — so it is
dispatched as its own task instead of being folded in.

**Acceptance reading for THIS epic**: `TestStandardGates/TestGate2TestEnvRedProof`
(Rule 17) **PASS** and `TestStandardGateDataReport`'s Rule 17 line reads
`hits=114 baselined=91 escaped=23 NEW=0`. The Rule-17 red that the epic reports
is closed; the remaining package-level failure is the pre-existing Rule 16/18
pair above.

## Files

- `internal/agent/journalreport_test.go` — the fix.
- `internal/standards/baseline.go` — **unchanged** (byte-identical to master).
- this devdoc.

## Verification (branch `fix/rule17-journalreport-env-escape`; tip = the single commit
on it, `git rev-parse fix/rule17-journalreport-env-escape` — recorded by subject, not by
hash, because a hash written into the file it describes cannot be kept true through an amend)

| Check | Command | Result |
|---|---|---|
| Build | `CGO_ENABLED=0 go build ./...` | OK |
| Vet | `CGO_ENABLED=0 go vet ./internal/agent/ ./internal/standards/` | clean |
| Rule 17 gate | `CGO_ENABLED=0 go test ./internal/standards/ -count=1 -run 'TestStandardGates/TestGate2'` | `ok` (PASS) |
| Rule 17 arithmetic | `go test ./internal/standards/ -count=1 -run TestStandardGateDataReport -v` | `hits=114 baselined=91 escaped=23 NEW=0` |
| Failed-test behaviour | `go test ./internal/agent/ -count=1 -run 'TestSanitizeReportContract\|TestReportContractOwnershipResolvesEquivalentPaths' -v` | both PASS (**incl. the `no contract at all is a no-op` case**) |
| Baseline untouched | `git diff master -- internal/standards/baseline.go` | empty (0 bytes) |
| Red proof | pristine worktree @ `0dac2ae7`, same command | exit 1, 2 NEW hits at `:101`/`:104` |

**Naming note (honest):** the briefing's acceptance names
`go test ./internal/agent/ -run TestJournalReport`. No test by that name exists
in the tree (`grep -rn 'func TestJournalReport'` → no match); the tests that
cover this file are `TestSanitizeReportContract` (the table with the absent
case) and `TestReportContractOwnershipResolvesEquivalentPaths`, run above and
both PASS. The acceptance intent — *the edited test's own package still passes*
— is met by them.

## Deliverable location (Rule 6d)

- **Branch**: `fix/rule17-journalreport-env-escape` (fresh off master `0dac2ae7`),
  one commit: `fix(agent): give the report-contract absence case its reviewed
  Rule 17 escape` (`git log -1 --oneline fix/rule17-journalreport-env-escape`).
  The commit lives in the MAIN repo's object store (shared with the linked
  worktree `~/workspace/gopi-rule17-journalreport`, where the work was done) — no
  push needed, nothing to fetch: `git worktree list` shows the branch.
- **Recovery command**: `git show fix/rule17-journalreport-env-escape:devdoc/rule17-journalreport-env-escape.md`
  (the fix itself: `git show fix/rule17-journalreport-env-escape -- internal/agent/journalreport_test.go`).
- The worktree was NOT removed (PA merges and cleans up per Rule 6a/6e).



---

## Merge resolution note (`fix/rule17-journalreport-escape` ← `master`)

This file arrived on **both** sides of the merge (add/add), so the two devdocs
describe the *same* fix written twice. Resolution:

- **The body below is master's report and is the authoritative text.** Its
  consolidated loop (`f9311942`, one `os.Unsetenv` in a loop over the two names,
  one reviewed `// env-leak-ok:` escape) is the code the merge actually contains,
  so its gate arithmetic (`hits=114 baselined=91 escaped=23 NEW=0`) describes the
  merged tree. The branch's own text described an intermediate state of the same
  fix (two separate `os.Unsetenv` statements, one escape each → `hits=115
  escaped=24`); reporting those numbers against the merged code would be a lie.
- **The branch's unique content is preserved here** (neither side's intent is
  dropped):
  - Branch history: HEAD was `c7123178` (docs back-reference) on top of
    `49f5e49a` (the code+escape commit), forked from master tip `0dac2ae7`.
    Recovery: `git show 49f5e49a -- internal/agent/journalreport_test.go`.
  - The unset statements are `os.Unsetenv(taskreport.PathEnv)` /
    `os.Unsetenv(taskreport.OwnEnv)` — the absent case must take the pair out of
    existence **together**, and `os.Unsetenv` is used only to do that; the
    `t.Setenv` calls above restore both names on cleanup.
  - The gate's `escaped=N` count is **line-keyed** and shifts whenever the
    surrounding comments change (this merge shifts it again). A key-shaped
    escape (rule id + env name) would remove that fragility — a gate change, not
    this task's scope; worth its own dispatch if it bites again.
  - The Rule 17 predicate's scan
    (`internal/standards/gate.go`: `testRoots` = `prodRoots` + `testsuite`, i.e.
    `internal/` + `cmd/` + `testsuite/`) is exhaustive, so `NEW=0` means no
    unannotated env site remains anywhere in the scanned trees — not merely none
    in `internal/agent`.
