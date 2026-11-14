<!-- doc-class: historical -->
# The merge `--every` loop's profile holds the removal-audit sink — contract vs sink location

**Status:** fixed on `task/01a14502d8eb7dbab317ed529c6a6ab4` (commit `c836f428c`); the branch was
archived orphaned and its intent was relayed onto master on 2026-10-17 by
`task/01a145800aa577bcb94900661760a736` — see "Relay" at the end.
**Measured:** 2026-10-16, host `hosta`, worktree `/home/user/workspace/gopi-01a14502d8eb7dbab317ed529c6a6ab4`.
**Symptom:** `go test ./cmd/agent/ -run TestMergeEveryLoopWritesNoStateFile -count=1` FAILED on a clean
checkout at every revision carrying it (parent `1c5b08698`, HEAD `14d94f6c0`), offender always
`<profile>/.gopi/logs/removal-audit.log`.

## The two candidate readings, and which one the measurement supports

The failure offered two readings (they exclude each other). **Reading (a) is the cause, and reading (b)
is refuted by measurement, not by argument.**

### (a) CONTRACT — the pass genuinely writes the sink into its own profile. MEASURED TRUE.

One variable, opposite outcomes. `TMPDIR` pinned under the session dir in BOTH arms:

```bash
go test ./cmd/agent/ -run 'TestMergeEveryLoopWritesNoStateFile' -count=1 -v          # FAIL (12.04s / 3.64s)
#   merge_every_test.go: the profile holds [.gopi/logs/removal-audit.log] …
GOPI_REMOVAL_AUDIT_LOG=/home/user/.gopi/sessions/01a14502-d8eb-7dba-b317-ed529c6a6ab4/audit/run1.log \
  go test ./cmd/agent/ -run 'TestMergeEveryLoopWritesNoStateFile' -count=1 -v        # PASS (3.98s)
```

With the override pinned outside the profile the scenario is GREEN, and the pinned file carries the very
record that was landing in the profile — so the content, the writer and the arm are named, not inferred:

```json
{"version":1,"ts":"2026-10-16T15:15:30.768509826Z","kind":"branch-delete","arm":"merge-run-spent-branch",
 "op":"branch:delete","pid":2107395,
 "repo":"/dev/shm/suite-cmd-agent-run-3850179620/TestMergeEveryLoopWritesNoStateFile279323469/001",
 "target":"feat/loop-TestMergeEveryLoopWritesNoStateFile",
 "branch":"feat/loop-TestMergeEveryLoopWritesNoStateFile",
 "evidence":"merge --ff-only landed the ref in master (a fast-forward cannot conflict, nothing is left to hand over)"}
```

Emit site: `removalaudit.BranchDelete(removalaudit.ArmMergeRunSpentBranch, …)` in `cmd/agent/merge_run.go`
(three sites: the already-spent prune, the fast-forwarded prune, and the cleanup of a spent branch after a
merge). `internal/removalaudit`'s package contract is explicit that the record is emitted BEFORE the git
mutation, and that its default sink is `<gopi data root>/logs/removal-audit.log` (`sinkPath`), whose
directory the emitter creates on demand for the DEFAULT sink only.

So the loop's no-state-file contract and the audit sink are two real contracts, and this profile — which
IS the loop's gopi data root — receives the audit trail of the work the loop actually did. The audit
sink is not the loop's state: same standing as the signal-provenance record the walk already tolerates
for a different writer (`internal/signalrec`).

### (b) PATH-RESOLUTION — the sink degenerates to `<profile>/.gopi/logs/` in a sandbox. REFUTED.

`removalaudit.sinkPath` (`internal/removalaudit/audit.go`) resolves in ONE place (Rule 2):

```go
if override := os.Getenv(EnvSink); override != "" { return override, "", "" }   // used verbatim
gd, err := config.GopiDir()                                                     // else the data root
dir := filepath.Join(gd, LogDirName); return filepath.Join(dir, FileName), "", dir
```

There is no sandbox-relative, `TMPDIR`-relative or cwd-relative leg for the default to degenerate into:
`config.GopiDir()` = `$GOPI_HOME/.gopi`, else `$HOME/.gopi`. The independent, decisive observation is the
A/B itself: with `EnvSink` **set**, the file lands at the exact path the override names and NOTHING is
written into `<profile>/.gopi/logs/` (the test passes). A resolver that degenerated for a sandbox profile
would have written both, or ignored the override. It honoured it — so the default path in this test is
literally `$GOPI_HOME/.gopi/logs/`, computed correctly, and the profile receiving the file is the
CORRECT location for it, not a degenerate one.

## The fix: the walk's exemption set was short by one artifact (contract side)

`mergeProfileOffenders` (`cmd/agent/merge_every_child_test.go`) had exactly two exemptions —
`serveLockFilename` and `mergeIsSignalRecord`. It gained a third, narrow one:

```go
func mergeIsAuditSinkPath(path, home string) bool {   // ONE path, spelled from the producer's constants
	rel, err := filepath.Rel(home, path)
	if err != nil { return false }
	want := filepath.Join(config.ProfileDirName, removalaudit.LogDirName, removalaudit.FileName)
	return rel == want
}
```

`removalaudit.LogDirName` / `removalaudit.FileName` / `config.ProfileDirName` are READ, never re-typed
(Rule 1/13/14). It is a FILE match, never a directory tolerance: `<home>/<gopi dir>/logs/<FileName>` is
the only admitted path, so a second file inside `logs/` (a rotation), the sink's file name at the profile
ROOT, and a file name that merely looks like the sink all still fail.

`loopResidueWriterReason` also named only ONE writer (signalrec) whose artifact the failure did not even
mention. It now names both writers, the arm and the mechanism — the text the failure was missing.

## The falsifier, and the two red proofs (Rule 5)

`TestMergeProfileWalkKeepsItsTeeth` grew the sink's own near-misses, so the new tolerance is pinned in
BOTH directions: the tolerated set (lock, recorder record, audit sink) must NOT appear as offenders, and
each of six look-alikes MUST — `merge-state.json` beside the lock, `signal-provenance.jsonl` at the
sessions root, `metadata.json` inside the process dir, a container keyed by the inherited ledger id, the
rotated sink `removal-audit.log.1` inside `logs/`, and `removal-audit.log` at the profile ROOT.

Two red proofs, each mutated and RESTORED with the restore verified against a revision
(`git hash-object <path>` == `git rev-parse HEAD:<path>`), never against the index:

| mutation | observed |
|---|---|
| exemption call disabled, override unset | FAIL `the profile holds [.gopi/logs/removal-audit.log]` (the honest WHY text, verbatim) |
| tolerance degenerated to a file-NAME match | FAIL `the walk did not report .gopi/removal-audit.log as residue (reported [… .gopi/logs/removal-audit.log.1 …])` |

Both restore checks printed `RESTORE_OK blob==HEAD:<path>` with an empty `git status --porcelain`.

## What was NOT done

The `GOPI_REMOVAL_AUDIT_LOG` override contract is untouched: the emitter still creates the DEFAULT
sink's directory and only that one, and `TestExplicitOverrideWithAMissingDirectoryIsRefusedNotInvented`
still passes. No production code changed in this task — the defect was in the guard's exemption set, and
the sink's location is correct as designed.

## Relay (2026-10-17): what master already held, and what this record adds

The branch was archived orphaned; its TIP (`d094928bb`) was still in the object store, so nothing was
re-applied from a diff. Measured on master `cbf5664dd` **before** this relay:

- the **test-side intent had already landed by a different route**: `mergeProfileOffenders` gains the
  third exemption through `mergeIsRemovalAuditSink` + `mergeRemovalAuditSinkRel` (the tolerated path,
  built from `config.ProfileDirName` / `removalaudit.LogDirName` / `removalaudit.FileName` in ONE place)
  in `cmd/agent/merge_every_child_test.go`, `loopResidueWriterReason` names both writers, the sink's
  CONTENT is checked against `mergeOwnRecordArms` (`ArmMergeRunSpentBranch`, `ArmMergeRunProbeGround`),
  and `TestMergeProfileWalkKeepsItsTeeth` refuses **seven** near-misses — the six in the table above plus
  `metadata.json` as a second file inside `logs/`. So the walk master ships is the NARROWER of the two:
  same tolerated path, more near-misses refused. The function NAME differs from the archived draft's
  (`mergeIsAuditSinkPath`); grep `mergeIsRemovalAuditSink` on master, not the older spelling.
- what master did **not** carry was **this document**. Its row was absent from `devdoc/DOC-INDEX.tsv`
  (`grep -c merge-every-profile-audit-sink` → 0 at `cbf5664`), so the root-cause reading — contract vs
  path-resolution, and the A/B that decides between them — was recorded nowhere a reader could find it.
  A sibling document, `devdoc/merge-loop-residue-walk-audit-sink-20261016.md`, records the same defect
  from a second run; this one carries the DECISION (a=contract, b=refuted) and the red-proof table.

The relay therefore shipped **the residue only**: this file, classified by the generator
(`tools/docclass/classify.py --only <path>`), and no test edit — re-landing the archived tests would have
replaced the robust walk with its earlier draft and dropped an arm.
