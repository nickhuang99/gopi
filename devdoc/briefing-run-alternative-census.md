<!-- doc-class: normative -->
# A `go test -run` alternative that reaches no test is named at dispatch

`internal/tools/task_briefing_run_alternatives.go` (row `01a18f76-9654`, branch
`task/01a18f76965470059cd795779da45060`).

## The defect this exists for

An acceptance line read, verbatim:

```
go test ./internal/tools/ -run 'TestWorktreeRemovalGroundGate|TestTaskReclaimSelfDelivery|TestEveryWorktreeRemovalArmQuestionsTheGround|TestSelfDelivery'
```

Two of those four names exist nowhere in the repository. `go test` prints
`testing: warning: no tests to run` for a pattern that matches nothing and
**exits 0**, so those two families would never have run while the acceptance line
read green and the report cited it. Restated as PROJECT.md Rule 36's own sentence:
a named check that does not exist is worse than no check, because its green
cannot be told apart from a clean tree. The remedy is not to re-type the
four names — the next briefing with a stale selector fails the same way.

## What it does

The sixth measurement on the dispatch seam in `cmd/agent/cli_tasks.go`. For each
`go test <pkg> -run '<pattern>'` line in a briefing it: resolves the package
pattern to a directory in the run's own ground; reads that directory's own
`*_test.go` files; collects the top-level `func Test…` names; splits the pattern
on `|`; counts matches per alternative. A ZERO is stamped into the child's
briefing by name; every alternative, dead or alive, leaves a count line in the
run record so a zero is visible against its neighbours.

## Decisions, and why

- **A SOURCE SCAN, not a `go test` run.** "Does a top-level test function match
  this pattern" is answerable exactly by reading the files, costs ~0.02 s for
  `./internal/tools` (588 files, 2984 names), needs no subprocess on the dispatch
  path and no network. What it cannot see is a test GENERATED at build time or a
  subtest (`TestX/Sub`): the subtest half is STATED in the record rather than
  guessed, and the `./...` pattern is refused as unmeasured instead of expanded.
- **REPORT, never REFUSE.** Every sibling clause on this seam reports, and a
  refusing clause blocks a claim over a briefing's prose — the wrong blast radius
  for a shape a `-run` can legitimately express (a name outside the named
  package, a generated test). The handle for a future refusal is
  `BriefingWithClause`'s append decision, one call.
- **A PARTIAL CENSUS IS A LIE, so truncation is STATED.** The first draft capped
  the scan at 400 test files; `./internal/tools` carries 588, so the scan
  truncated and reported the two LIVE families as ZERO REACH — the exact defect
  this clause repairs, one level up. The cap is now above the largest package,
  and hitting it (or an unreadable file) returns an UNMEASURED reason instead of
  a partial name list. Any future author tempted to "just cap it" should read
  this paragraph first.
- **`TestMain` is not a test.** A `-run` pattern cannot select it, so counting it
  would make a dead alternative look alive. The census reads 2984 where
  `grep -c '^func Test'` reads 2986: the difference is `TestMain` hooks.

## What a green here does NOT mean

The clause says one alternative cannot be satisfied by ANY test in this tree. It
does not say the acceptance criterion was met, and it stamps nothing when every
alternative reaches a test (the positive control, and the ordinary briefing).

## Checks

```
go test ./internal/tools/ -run 'TestBriefingRunAlt' -count=1 -v
```

Seven fixture legs (dead named / positive control / non-test literals / vacuum
floor / subtest / no-op / idempotence) plus
`TestBriefingRunAlt_OverTheEpicsAcceptanceLine`, which judges the incident's
verbatim acceptance line against this repository's real corpus.
