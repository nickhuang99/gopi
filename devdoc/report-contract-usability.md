<!-- doc-class: normative -->
# The report contract's second question: is the destination USABLE?

`agent.SanitizeReportContract` is the startup read face of the task-report
contract (`internal/agent/journalreport.go`, called once from
`cmd/agent/printLogPathsQuiet` in `cmd/agent/main.go`). Until 2026-10-28 it asked
ONE question:

> does this destination live in MY session's directory? (OWNERSHIP)

That question is necessary and was never sufficient. It answers "whose report is
this"; it says nothing about "can a file be created there". The measured failure
(session `01a18310-f082-702e-8d06-7d40bf6444da`): an inherited
`GOPI_TASK_REPORT_PATH` pointed at
`~/.gopi/sessions/<32-hex dashless>/task-report.md` whose **parent directory did
not exist**. Ownership passed — the container was not another live session's
directory — so the path entered the run's context as a FACT, and the contract's
own write (`cat > "$GOPI_TASK_REPORT_PATH"`) died on `ENOENT` at the point
furthest from the cause.

## The rule now

A destination must satisfy BOTH, or it is dropped and named:

1. **OWNERSHIP** — `taskreport.ProcessOwnsContract(sessionDir)`: the destination
   lives in this session's own directory (resolved, symlink-aware), and no owner
   marker names a different session.
2. **USABILITY** — `reportDestinationUsable(path)`: the destination's PARENT
   directory exists, is a directory, and `access(2)` says this process may
   create a file in it.

The refusal line carries the path, the offending directory and every criterion
that failed — BOTH are evaluated even when the first already refuses, because an
operator who fixes one and meets the other would be back where they started.

## The deliberate non-remedy: no `MkdirAll`

The tempting "fix" is to create the missing parent. That would MINT the
misspelling: a path built from a bad id becomes a real second container under
`<gopi>/.gopi/sessions/`, the run's only report lands in it, and nothing — no
`task show`, no close-out seal, no operator glob — looks there. So the read face
never creates a directory; a refusal is a refusal.

The export side (`tools.TaskReportDestination`) legitimately DOES create its
parent, and that is not a contradiction: there the path comes from a resolution
this tree owns, so the directory it creates is the one the writer will use. The
read face is handed a path by somebody else and can only judge it.

`access(2)` rather than the write probe the exporter uses, for the same reason in
a different direction: every startup runs this guard (`gopi chat`, a nested
probe, `gopi --help`), and a probe file would leave litter in other people's
session containers and create files in directories this process may not touch.

## The interface change

```go
func SanitizeReportContract(sessionDir string, refuse func(reason string)) (removed []string)
```

The `refuse` sink exists because a refusal that is only a return value is a
silent failure in the caller (Rule 15): `cmd/agent/main.go` prints the line to
stderr, and the second line names the variables that left the environment. A nil
sink is not permission to stay silent — the guard then writes the same line to
stderr itself.

## Verification

- `go build ./...` — clean.
- `make test-budget PKGS='./internal/agent ./cmd/agent'` — green.
- Red proof (PRE-FIX behaviour: ownership only, carrying the post-fix signature
  so the test fails on BEHAVIOUR, not on a symbol): three arms red, each with the
  exact silence a run met — the pure-usability arm keeps
  `GOPI_TASK_REPORT_PATH` set after the guard ("must not survive as a fact"),
  and the two unusable-parent arms produce ZERO refusal lines.
- End-to-end (`cmd/agent`): a real binary built from this tree, launched with an
  inherited destination whose parent is missing, prints the refusal naming the
  path and the directory, the model's own bash tool observes the variable as
  `<unset>`, and the missing container is NOT created.
- Counterweight: a usable inherited destination is KEPT (the reported path is
  what the child's shell still sees), so the fix cannot break the ordinary
  hand-off.
