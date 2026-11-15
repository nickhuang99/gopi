<!-- doc-class: normative -->
# Rule-16 sites in `internal/docindex/anchor_drift.go`: the inner error was off the chain

Base commit: **cf21ceb4e** (`Merge branch 'task/01a134f3af347e4aa25c5035f1d03b01'`), branch
`task/01a1354c5f4a746bac53d0b6571eeab9`, task `01a1354c`. Every figure below is a command and
its output, not a reading.

## 1. The defect, measured

`5fc3504fb` ("fix(docclass): the close-out asks the anchor-number freeze table's question …")
added `internal/docindex/anchor_drift.go` with four `fmt.Errorf` calls that rendered the INNER
error with `%v`:

```bash
grep -n '%v' internal/docindex/anchor_drift.go          # on cf21ceb4e
→ 148: fmt.Errorf("%w: stat %s: %v", errAnchorDriftUnreadable, …, err)
→ 153: fmt.Errorf("%w: %v", errAnchorDriftUnreadable, err)
→ 164: fmt.Errorf("%w: %v", errAnchorDriftUnreadable, err)
→ 168: fmt.Errorf("%w: %v", errAnchorDriftUnreadable, err)
```

The outer sentinel was already `%w`, so the refusal stayed attributable to the arm
(`errors.Is(err, errAnchorDriftUnreadable)` worked) while the filesystem error the refusal is
ABOUT — the one the message prints — left the chain. The gate that exists for exactly this:

```bash
go test -count=1 -run 'TestStandardGates/TestGate1ErrorWrapRedProof' ./internal/standards/
→ Rule 16 (%w error wrapping): 7 total hits, 2 baselined, 1 escaped, 4 new (unannotated), 2 baseline entries
→ NEW violation … internal/docindex/anchor_drift.go:148 (and :153, :164, :168)
exit 1
```

## 2. The fix

Both verbs are `%w` — `"%w: %w"` and `"%w: stat %s: %w"`. A second `%w` verb needs **Go 1.20+**
(this module declares `go 1.24.0`, `go.mod`), and the site pattern is not new: the same shape is
already shipped in `internal/tools/memory_sync_task.go:922`,
`internal/scratch/tmproot.go:633`, `internal/cgroupscope/cgroupscope.go:407/411/415/422/487/491`.
The rejection alternative — a `// no-wrap:` inline escape — was considered and refused: the
escaped error is a `*fs.PathError` a caller can legitimately ask about (that is what the
regression test does), and the gate's own escape is reserved for the deliberate KEEP class
(a value that is `nil` exactly where it must stay out of the chain, `baseline.go`).

## 3. The regression test, and what it can and cannot reach

`TestAnchorDriftArmRefusalKeepsTheOSErrorInTheChain` (`internal/docindex/anchor_drift_test.go`)
asserts, for both error branches of `anchorDriftOf`, that
`errors.Is(err, errAnchorDriftUnreadable)` **and** `errors.As(err, &*fs.PathError)` hold, and that
the `PathError.Path` is the very table the refusal names. The probe is the arm's OWN return —
`SyncBranch` folds the same error into a refusal SENTENCE (`scope_git.go`), where there is no
error left to probe.

Two measured surprises, both folded into the fixture:

- **The stat branch cannot be exercised by a MISSING table.** `os.Stat` returning `IsNotExist` is
  the arm's OTHER, recorded state — `anchorDriftArm{Absent: anchorDriftAbsentTable}, nil` — not an
  error. A first cut removed the table and the test failed with "a table the arm cannot read is an
  ERROR": the fixture was asserting the wrong branch. The stat branch needs a stat failure that is
  NOT `IsNotExist`, so the fixture puts a REGULAR FILE where the table's directory belongs (the
  stat answers `ENOTDIR`).
- **The read branch is reachable with a DIRECTORY in the table's place**: the pre-flight stat
  succeeds and the first read fails `EISDIR`.

Red proof, run this run (mutation → red → restore), evidence in
`~/.gopi/sessions/01a1354c-5f4a-746b-ac53-d0b6571eeab9/artifacts/`:

```bash
# restored the 4 sites to %v, then:
go test -count=1 -run 'TestAnchorDriftArmRefusalKeepsTheOSErrorInTheChain' ./internal/docindex/
→ --- FAIL: …/the_table's_directory_is_not_a_directory_(the_stat_branch)
→ --- FAIL: …/the_table_cannot_be_read_(the_read_branch)
→ "the OS error is no longer in the chain — errors.Is/As cannot reach it"
exit 1
# restore verified by CONTENT, not by an index-relative diff (devdoc/redproof-restore-criterion):
git hash-object internal/docindex/anchor_drift.go → fcbacdd49638c502ded8a228a46f7237fb42e94e (before == after)
```

## 4. The hole this run did NOT close, and should be named

After the fix the Rule-16 row reads **3 total hits, 2 baselined, 1 escaped, 0 new** — the
briefing's expected "7 total, 6 baselined, 1 escaped" does not match this tree, because the four
permits it counts are the count from a run in which `errorWrapBaseline` had absorbed these sites as
line-keyed entries. On cf21ceb4e the list holds **2** entries
(`internal/tools/memory_procedural.go:241`, `internal/tools/memory_sync_procedural.go:245`) — the
total is lower precisely because this run FIXED four real hits instead of permitting them.

That is evidence for a defect of its own, and this run does not fix it (Rule 41: report it, do not
widen the diff):

- The Rule-16 rows carry only two line-keyed permits, BOTH in `internal/tools/*.go`. The 6-vs-2
  discrepancy cannot be produced by this tree.
- The merge note in `internal/standards/baseline.go` ("merge-note: HEAD's line-key (260) and
  master's (277) named the SAME %v site at different line numbers; the merged task_merge.go has
  exactly one such site, so a single re-keyed entry carries both intents") explains ONE entry from
  two, not six from two.
- The named mechanism in the briefing — the landing's close-out runs the anchor-number census
  generator, whose scope is the FREEZE table, not `errorWrapBaseline`.

So the most consistent explanation is a different mechanism this file's history demonstrates
repeatedly: a merge dropped or re-keyed entries. It is NOT chaseable to a commit here — the
integration tip changes during this run (`795f10044` is an ancestor of this branch's HEAD
`cf21ceb4e`, and `master` moved to `508af3b67` while it ran), and it is filed as its own task
rather than guessed at.

## 5. Note on the second half of the briefing

"另有 Rule 17 违规" is a misreading, and the tree measures it directly
(`job-1` of this run, `go test -run TestStandardGates ./internal/standards/`): **Rule 17: 111
total hits, 62 baselined, 49 escaped, 0 new** — every hit is either a permit or an inline
`// env-leak-ok:` exception reviewed on its own line; `new` is the class the gate fails on, and it
is 0.
