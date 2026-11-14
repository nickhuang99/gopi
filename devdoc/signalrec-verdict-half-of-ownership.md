<!-- doc-class: historical -->
# Half a predicate is a disagreement: the session-log verdict read handler ownership from argv alone

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Date: 2026-09-25. Epic: `0x01a0d65f` (bare-prompt SIGTERM → default death, no
`session_end`), reworked under row `01a0d7a3`.
Status: landed on branch `task/01a0d7a3525c7939b4c0c927c0e48fb1`.

## What this rework found before it changed anything

The row asked to re-land the archived branch `task/01a0d687…` (476-line diff) on
current master. **Master had already landed it.** Commit `2a13830c`
("fix(signal): read handler ownership at SIGNAL time — the re-raiser no longer
kills a bare-prompt gopi mid-unwind", 2026-09-25 15:53) carries, in master's own
voice, every substantive item of the archived diff:

| archived diff | master |
|---|---|
| `signalrec.ClaimSignalHandlers()` + an atomic ownership flag | `handlersClaimed` (signalrec.go:286) |
| the predicate evaluated **at signal time** | `signalForwarding()` (signalrec.go:1391) |
| the Install handler consumes the predicate | signalrec.go:1068 |
| the cross-process declaration channel | `envOwnsHandlers` (signalrec.go:1412) |
| the call sites | `cmd/agent/main.go:682`, `cmd/agent/cli_term.go:244` |
| the regression guard | `TestSignalRecClaimedHandlerIsNotReRaised` (signalrec_test.go:1215) |
| the end-to-end guard | `TestTask_BarePrompt_SIGTERM_WritesSessionEnd` (testsuite/task_test.go:219) |
| the devdoc + its index row | `devdoc/signalrec-claim-vs-reraise.md` |

So the delta is not the archived diff. `go build ./...` and `go vet ./...` were
already green at the branch tip, which is master's tip plus nothing.

**The one item of the archived diff that was NOT in master is the interesting
one**, and it is the defect below.

## The defect: two halves of one question, answered two ways

`2a13830c` cured the **re-raise** half — a process that declares it owns its
signal handlers is no longer re-raised to death mid-unwind. It left the
**session-log verdict** half reading ownership from argv alone:

```go
// before
return !forwardsDefault(roleFromArgv(os.Args))
```

Both halves answer the SAME question — *does this process own its terminating
signal handlers?* — and an argv-derived answer cannot answer it: `argv[1]` of a
bare-prompt run **is the prompt text**, and a DECLARED claim is invisible to argv
by construction (it is a fact about the live process). So for a claiming run the
two halves disagreed about the same process:

- the re-raise half left it to unwind gracefully (correct, and the epic's fix),
- the verdict half classified it as a short-lived command whose line *"the
  console already carries"*, so it appended nothing.

The run that lost the most work wrote the least evidence into the one file an
operator reads afterwards. That is the archived branch's own wording of why it
changed this line (`return !signalForwarding()`), and it is the piece that did
not survive.

## Fix

One predicate, two consumers (Rule 13):

```go
func sessionLogVerdictOn() bool {
	/* explicit knob wins: SessionLogVerdictEnv true/false */
	return !signalForwarding()   // was: !forwardsDefault(roleFromArgv(os.Args))
}
```

`signalForwarding()` is the argv hint AND NOT the claim AND NOT the suppress
knobs — the same expression the re-raise consults, so the two halves can no
longer disagree.

## Guard

`TestSignalRecOwnershipDeclarationGovernsTheSessionLogVerdict`
(`internal/signalrec/signalrec_unsourced_test.go`). It pins both arms **in one
process**, because both the claim and the policy are process-local:

- **arm A (no declaration)**: an argv the table cannot classify stays OFF — the
  console line already carries the verdict and a duplicate in the session log is
  noise. This is the arm that keeps the fix from becoming "always write".
- **arm B (declared)**: the declaration must move this half too — the line must
  appear, carrying the shared `VerdictUnsourced` word.
- the explicit knob still wins over the ownership default, in both directions.

The declaration is stated through `envOwnsHandlers`, the cross-process channel
that feeds the **same** predicate — never a second policy (its own comment says
so; the in-process `ClaimSignalHandlers()` is its sibling, and its goroutine
safety argument was not worth a data race in a test).

RED → GREEN measured on one tree:
`signalrec_unsourced_test.go:324: sessionLogVerdictLine is empty for a process
that DECLARED it owns its signal handlers …` → `ok  github.com/nick/gopi/internal/signalrec`.

## Not fixed here (honest, and the trap to avoid)

**The verdict line has no production writer, and it is masked by a test that
asserts the opposite of the truth.** `Install`'s `sessionLog` argument is the
only path to that line, and its ONLY caller in the tree is
`cmd/agent/main.go:455` — `signalrec.Install("")`, empty since the package's
first commit (`2fd022ca`). The run's real session log is learned later, in-process
(`SetSessionDir`, main.go:979), which resolves the *directory* for the record but
never for this line. So `appendSessionLogVerdict` returns immediately on every
real gopi run, and the doc comment above it ("Default: ON for a mode that owns
its signal handlers") describes a write that cannot happen.

I did NOT repair that here, deliberately:

- **It is the original epic's declared scope to give it a path, and that change
  is bigger than a rework** — it is a file-identity question (which `.jsonl` in
  the dir is *the* session log) plus new symlink/append exposure, and its
  regression surface spans `internal/gopilog` and the sessions-tree gates.
- **The guard above is written so it cannot be satisfied by the falsehood.**
  Every assertion goes through `sessionLogVerdictLine(record)`, which returns ""
  for `Sourced == nil` regardless of the policy: the in-process unclassified case
  the recorder actually handles is NOT the armed one. A writer repairing the path
  will have to confront the real gap rather than inherit a green light.

**Dispatchable as its own task**: give `Install` (or a reader beside it) the run's
real session-log path so the verdict line lands in the session JSONL — and, while
there, settle whether a prose line belongs in a file whose other readers parse it
as JSON (`internal/gopilog` unmarshals per line).

## Evidence

```
$ CGO_ENABLED=0 go build ./...                                                    # OK
$ go vet ./internal/signalrec/ ./cmd/agent/                                       # OK
$ go test -count=1 -run TestSignalRecOwnershipDeclarationGovernsTheSessionLogVerdict \
      ./internal/signalrec/       # RED before the fix (line above); ok 0.759s after
$ go test -count=1 ./internal/signalrec/                                          # ok 3.186s
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/                                 # 26814594 bytes
$ GOPI_BIN=$PWD/bin/gopi go test -count=1 -timeout 5m ./testsuite/ \
      -run TestTask_BarePrompt_SIGTERM_WritesSessionEnd
--- PASS: TestTask_BarePrompt_SIGTERM_WritesSessionEnd (1.22s)   # ok testsuite 1.695s
```
