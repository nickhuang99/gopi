<!-- doc-class: historical -->
# The merge-loop residue walk and the audit sink it writes itself

**Task** `01a143696d247e66abe1dda74a3fc9fd`, branch `task/01a143696d247e66abe1dda74a3fc9fd`,
2026-10-16, host `hosta`. Base revision of the run `c59b729523efb88fbed816dc9c53b07bfa97ba24`; the fix is
`c466c05c13d89c24bfe19417142f886c8ad2357e`.

## The symptom

```
$ go test ./cmd/agent/ -count=1 -run 'TestMergeEveryLoopWritesNoStateFile'
--- FAIL: TestMergeEveryLoopWritesNoStateFile (2.32s)
    merge_every_test.go:577: the profile holds [.gopi/logs/removal-audit.log] — the command's contract is
        no pid file, no progress record, and no state record left behind by an ended loop.
        WHY THIS CAN HAPPEN: the ONLY writer that can create a directory here is the signal-provenance
        recorder, … (internal/signalrec, sessionDest step 3) … check signalrec's
        modesThatOwnTheirSignalHandlers for a `merge`/merger alias before suspecting the merge path itself
```

The instrument was right and the explanation was wrong: the offender is not
`signalrec`'s record (the walk already tolerates `sessions/process-<pid>/signal-provenance.jsonl`)
but the **removal-audit sink**, which the assertion had never been told about.

## The writer, named from its own record

Pointing the sink's documented override (`removalaudit.EnvSink`, `GOPI_REMOVAL_AUDIT_LOG`)
outside the profile turns the scenario green and leaves the record to read:

```
$ GOPI_REMOVAL_AUDIT_LOG=<sink> go test ./cmd/agent/ -count=1 -run 'TestMergeEveryLoopWritesNoStateFile'
ok  	github.com/nick/gopi/cmd/agent	2.336s

$ cat <sink>
{"version":1,"ts":"2026-10-16T14:44:19.999488688Z","kind":"branch-delete","arm":"merge-run-spent-branch",
 "op":"branch:delete","pid":752998,"repo":"…/TestMergeEveryLoopWritesNoStateFile1903545983/001",
 "target":"feat/loop-TestMergeEveryLoopWritesNoStateFile","branch":"feat/loop-TestMergeEveryLoopWritesNoStateFile",
 "evidence":"merge --ff-only landed the ref in master (a fast-forward cannot conflict, nothing is left to hand over)"}
```

`arm=merge-run-spent-branch` is `removalaudit.ArmMergeRunSpentBranch`, emitted at
`cmd/agent/merge_run.go:2452` (① `merge-base --is-ancestor`), `:2469` (② `merge --ff-only`) and
`:2573` (③ the union lane), each immediately before the ref is pruned. `removalaudit.sinkPath`
resolves `<config.GopiDir()>/logs/removal-audit.log`, and `emit` creates that directory and the
file (`O_APPEND|O_CREATE|O_WRONLY`) — a fresh profile therefore gains the file the first time any
arm records anything, which for `gopi merge --every` is the first spent ref it prunes.

## Which side was defective

**The test's walking rule — not the sink's resolution.** The sink does exactly what its own
contract says (one path for the whole package, created at append time, loud refusal rather than a
guessed path); no arm resolved a path the profile did not justify. The walk's rule was written
before this writer existed: it admitted the lock and the recorder's record and called everything
else residue, so "the loop leaves no state" and "the loop never pruned a ref" had silently become
one assertion. An audit record of an operation the command performed is not state.

## The fix

`cmd/agent/merge_every_child_test.go` + `cmd/agent/merge_every_test.go` only — no production file
was touched:

1. A second tolerated artifact, matched by COMPONENTS against the **emitter's own constants**
   (`removalaudit.LogDirName` + `removalaudit.FileName`, Rule 13/14): the tolerated path is exactly
   `<gopi dir>/logs/removal-audit.log`.
2. `loopResidueWriterReason` rewritten to name BOTH writers in the order a reader should check
   them, and to say what remains a real defect.
3. `TestMergeProfileWalkKeepsItsTeeth` grew two look-alikes, so the new tolerance is a narrowing
   rather than a licence: **a second file inside `logs/`** (`metadata.json`) and **the audit file
   name at the profile root** (`<profile>/.gopi/removal-audit.log` — the right name at the wrong
   level).

The walk still tolerates nothing else: the `--run` suite profile (`suite-gopi-home`) is not
tolerated, because the live scenario pins `GOPI_HOME` to a fixture directory of its own
(`mergeEveryFixture` → `mergeLockTestHome`) and the sink follows that pin.

## Red proofs (both against explicit revisions)

| proof | mutation | observed |
|---|---|---|
| RED-B | the tolerance call removed (the pre-fix walk shape) | `--- FAIL: TestMergeEveryLoopWritesNoStateFile`, `the profile holds [.gopi/logs/removal-audit.log]` |
| RED-A | the narrow component match replaced by `if d.Name() == removalaudit.FileName` (a bare file-name licence) | `--- FAIL: TestMergeProfileWalkKeepsItsTeeth`, `the walk did not report .gopi/removal-audit.log as residue`, and the extra-file check reported `want exactly [… .gopi/removal-audit.log]` vs `got [… .gopi/logs/metadata.json]` |

Each restore was measured with an explicit revision — the file's blob compared against
`git rev-parse HEAD:cmd/agent/merge_every_child_test.go` (equal ⇒ restored), and `git diff HEAD`
empty.

## What this run also found (and where it is filed)

- The end-to-end probe of `gopi merge --every` that pinned `GOPI_HOME` but **not `GOPI_SETTINGS`**
  drove the operator's live `repos` table and pruned a ref named `tmp-mine` in the main checkout.
  Full record: the run's session dir, `evidence/root-cause-sink-writer.md` §4; the isolation recipe
  (pin `GOPI_SETTINGS`, or give the fixture an empty `repos` table) is stored in memory (#11321).
- The predicate that arm records before mutating means the ref's work was already contained in the
  integration branch at that moment — so this was not a loss of reachable work, but it was an
  operator-visible mutation by a probe, which is worth the paragraph.

## Not done

- No production change was made and none was required: `removalaudit`'s resolution is the contract,
  and the run deliberately left `internal/signalrec` and the emitter untouched.
- The suite profile under `/dev/shm/suite-*/suite-gopi-home` still accumulates `logs/removal-audit.log`
  from other packages' fixtures; whether that shared pin should be sandboxed per package is a
  separate question this run did not open.
