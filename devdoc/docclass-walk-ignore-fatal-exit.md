<!-- doc-class: historical -->
# The docclass walk's ignore lookup: exit 1 is an answer, 128 is not

Epic `01a14222-386a-77fe-ab08-97e9e1b30da4`. Ground:
`/home/user/workspace/gopi-01a14222386a77feab0897e9e1b30da4`, branch
`task/01a14222386a77feab0897e9e1b30da4`, cut `18ed997`.

## What was asked, and what was measured

The briefing: `go test ./internal/docclass/ ./internal/docindex/ -count=1` failed
once in nine runs on this host, the output having been piped through `tail -3`, so
the package line survived (`FAIL … docclass 32.998s`) and the failing arm's name
did not.

**Not reproduced.** Twelve further runs with the full `-v` stream retained —
`/home/user/.gopi/sessions/01a14222-386a-77fe-ab08-97e9e1b30da4/logs/vrun_1..12.log`
— are green 12/12 (plus the eight green runs the anomaly row itself recorded: 20
runs). No `--- FAIL` line appears in any of them. The failing arm's name is not
recoverable from any retained artifact.

## The wall-clock figure does not separate red from green (this is the load-bearing measurement)

The briefing treated 32.998s as a signature of the bad run. On this host the
**green** package wall is `23.885s … 32.203s` — my round 12 was green at
`32.203s`, i.e. within 0.8s of the failing figure:

```
vrun_2.log   ok  github.com/nick/gopi/internal/docclass  23.885s
vrun_7.log   ok  github.com/nick/gopi/internal/docclass  30.207s
vrun_12.log  ok  github.com/nick/gopi/internal/docclass  32.203s     ← GREEN, 0.8s from 32.998s
prior session (grep-verified):  ok … 23.174s … 26.541s (six green runs)
```

Per-arm spread is ordinary (every `go run`-driven arm moves 0.3–1.7s; the slowest,
`TestTheDocclassPythonSuiteRunsOnEveryGate`, 4.21–5.94s). A host at 94% disk
capacity with a concurrent `cmd/agent` suite for company produces this. So: **no
time budget in the package was crossed, and the wall clock carries no information
about which arm failed.** Any search keyed on "the run that took longer" starts
from a premise the measurement refutes.

## The only deadline in the package is not it either

`internal/docclass/docclass.go` holds exactly one wall-clock bound,
`defaultWalkIgnoreTimeout` (30s), added when a *2s* bound was crossed on
2026-10-12. Its subject is one `git check-ignore --stdin -z` over the corpus:

```
$ git ls-files devdoc | wc -l
2389
$ git check-ignore --stdin -z < those paths    # three runs
0.57s  →  30s bound = 53x margin
```

No test body in `internal/docclass` calls `time.Now`/`time.Since`/`time.After` at
all, and the package has no `t.Parallel()`, so nothing in it waits on a clock.

## Root cause found elsewhere: the exit status of the one external verdict

`Walk` decides corpus membership by asking git, and `dropIgnored` reads that
answer from `cmd.Output()`'s **stdout**, not from the exit status — deliberately,
because git can exit 0 with empty output. But the `err` half is where the hole is:
the code reads an `*exec.ExitError` as git's "nothing is ignored" answer unless the
context deadline expired.

**Measured exit contract (git 2.34.1, this host, 2026-10-16):**

| invocation | exit |
|---|---|
| a path list with no ignored member | **1** (the answer) |
| `GIT_DIR=/nonexistent` | **128** (`fatal: not a git repository`) |
| corrupt `GIT_INDEX_FILE` | **128** (`fatal: … index file smaller than expected`) |
| cwd that is not a repository | **128** |

So every fatal fell through the `err != nil` branch — `ctx.Err() == nil`, no
`return` — into the output parse, and with no output it returned the path list
UNCHANGED: **the whole corpus, ignored litter included**, on a tree whose
membership was never decided. That is exactly the defect the 2s→30s change fixed
for the *deadline* door, arriving through the other one. It is live on `master`
(HEAD `18ed997`) and it is the shape that reds a coverage gate
(`2073 rows cover 2074 files`) intermittently — and that, being a gate on the
corpus, reds *whichever* arm reads the walk.

**Red-proved on a real fixture** (production file reverted to HEAD, new arm run,
production file restored — restore verified by content identity):

```
--- FAIL: TestWalkRefusesWhenGitExitsWithoutAnswering (0.01s)
    walk_ignored_test.go:304: Walk returned no error when git exited without
    answering, and returned [devdoc/__pycache__/stray.pyc devdoc/real-doc.md]
```

Note the returned vector: the ignored `.pyc` is IN the corpus.

## The fix

`internal/docclass/docclass.go`, 24 lines, no new dependency, no new file:

- `gitCheckIgnoreNothingIgnoredExit = 1` — the ONE non-zero status that is git's
  own answer, as a named constant (Rule 1), with the measured table beside it.
- `dropIgnored` returns a loud error for any other non-zero status, naming the
  status and what it means. The exit-status branch is thereby unreachable for
  fatals, and a walk that never got a verdict refuses instead of guessing
  (Rule 15).

Regression guard: `TestWalkRefusesWhenGitExitsWithoutAnswering`
(`internal/docclass/walk_ignored_test.go`), the second door into the same defect
as the existing `TestWalkRefusesWhenTheIgnoreLookupIsKilled`.

## What is NOT claimed

- **The 32.998s failure is not explained.** No mechanism was found that maps a
  >=33s package wall onto a named failing arm, and — because the wall clock does
  not separate the runs — the search premise itself is what this task refutes.
  The one red-proofed mechanism in this package is reported for what it is: a
  real, live, intermittent-gate defect on the only external verdict the package
  takes, not a proven cause of that one run.
- **The failing arm remains unnamed.** It is not in any retained log.
- Nothing here changes the 12 green runs into a reproduction.
