<!-- doc-class: historical -->
# Task Report: the lineedit input-ordering seam — a package-level guard and a direct pty test of `Terminal.InputPending`

Task: `01a0c2cf` (bug, dispatched from the task `01a0c28b` line of work).
Branch: `fix/lineedit-inputpending-coverage` (worktree `~/workspace/gopi-lineedit-gap`, off master `e70119f5`).

## Why this exists

Commit `65d7e278` ("fix(chat): a draw never overtakes queued input", merged
`c74ca08f`) closed the cmd/agent small-height flake by adding three things:

- `inputBacklog`, the OPTIONAL source seam (`internal/lineedit/raw.go:151`);
- the read loop's ordering gate `if e.inputPending() { continue }`
  (`internal/lineedit/editor.go`, the `EventWake` arm);
- the only PRODUCTION implementation, `(*Terminal).InputPending()`
  (`internal/lineedit/raw.go:372`) — a non-blocking `poll(2)` on stdin's
  descriptor, captured once at `Open`.

Before this branch, **no test anywhere used `Terminal.InputPending` as a
subject**, and the package's own scripted sources did not implement the seam at
all — `inputBacklog` was, inside `internal/lineedit`, a documented interface with
zero exercisers. The shipped cmd/agent guard (`TestChatPublishDoesNotOvertakeQueuedInput`)
rides on cmd/agent's own `scriptSource`, so its red-proof was a hand-made
`if false` on the production call site: it proved the DECISION is consulted, not
that the ANSWER the probe returns is right.

This branch closes both halves, and reports one correction to the brief it was
dispatched with (see "Not Done / Disagreed" below).

## Completed

### 1. A package-level deterministic ordering guard — `internal/lineedit/input_ordering_test.go`

`TestReadLoop_QueuedInputIsReadBeforeAPublishIsDrawn` states the shipped
invariant as an assertion inside the package that owns it:

- the read loop is PARKED on the source's own gate before its first read;
- keystrokes are queued and a publish is raised WHILE it is parked;
- the gate is released ONCE, so the loop's next read reports the ambiguous
  single wake (a publish and queued input in one event);
- the ORDER is read off the **write stream**: the index of the chunk that first
  carries the status text, against the index at which the loop took its first
  queued keystroke. A keystroke delivered at write index N says the loop chose to
  READ before it had emitted the N-th chunk.

No wall-clock threshold, no sleep-as-proof, no host load (Rule 27) — the barrier
is a channel gate and the measurement is an index into a byte stream.

Companions in the same file:

- `TestReadLoop_OrderingGuardIsDeterministic` — the same sequence ×5 inside one
  process (the instrument the flake class prescribes).
- `TestReadLoop_NoOrderingGateLetsTheDrawOvertake` — the NEGATIVE CONTROL: the
  identical sequence over a source whose seam answers `false` (the regressed
  answer) produces exactly the interleaving the guard forbids, so the guard's
  verdict is load-bearing on the seam's answer and not on its own bookkeeping.
  This is the package-level twin of the red-proof below; it runs in the ordinary
  suite, so the guard can never silently degrade into "always green".
- `var _ inputBacklog = (*guardedOrderingSource)(nil)` — a compile-time proof
  that the guard's source stands on the same interface production implements,
  i.e. the editor reaches it through the identical type assertion it uses for
  `*Terminal`.

The park-gate hook (`internal/lineedit/parkgate.go`) is *not* used by this guard:
that hook exists to construct the menu's acknowledge window, and this guard needs
no window — it owns the source's gate. The guard therefore runs without a hook
installed, which keeps it independent of `menu_publish_window_test.go`'s
`setParkGateHook`/`defer setParkGateHook(nil)` pair (both tests in the same
package). `ParkGateHookEnabled()` remains the seam's own precondition test, as
before.

### 2. A direct test of the shipped `Terminal.InputPending()` over a real pty — `internal/lineedit/inputpending_pty_test.go`

`TestTerminalInputPending` opens a real pty (`/dev/ptmx` + `TIOCGPTN` +
`TIOCSPTLCK` + `/dev/pts/N`, the recipe the package's `pty_linux.go` exposes) and
drives the REAL object through the REAL probe. Both halves are pinned as used:
`lineedit.Open(slave, …)` then `NewEditor(term, …)`, with an assertion that the
editor actually installed a backlog (`ed.backlog != nil`) — so the test measures
the surface production consults, not a lookalike.

The three honest shapes, plus the hangup arm:

| shape | probe answer |
|---|---|
| nothing written to the master | `false` (an idle terminal must never make the loop defer a publish) |
| a byte written, unread | `true` (the answer the ordering gate acts on) |
| that byte read | `false` (nothing pending; the gate must stop deferring) |
| peer hangs up (master closed) | `true` (POLLHUP reads immediately — the EOF the loop must read does not wait for another wake) |

`TestTerminalInputPending_UnknownDescriptorIsFalse` pins the degraded arm the
probe itself carries: a terminal whose input descriptor was never captured
answers `false` rather than guessing (the same arm a source WITHOUT the seam
takes).

Both tests `t.Skipf` on a host with no `/dev/ptmx`: the descriptor IS the
subject, so a substitute stream would assert nothing.

### 3. `cmd/agent`'s `scriptSource` already implements `inputBacklog` — verified, not duplicated

The brief's third deliverable ("make `scriptSource` implement `inputBacklog`")
is **stale**: it landed inside the fix commit itself. `git log -1 --format=%s`
on the blame of `cmd/agent/lineedit_wire_test.go:165` is `65d7e278 fix(chat): a
draw never overtakes queued input`, and `git show master:cmd/agent/lineedit_wire_test.go`
carries `func (s *scriptSource) InputPending() bool` at line 165. No change was
made here (and none was needed) — see "Ran" for the re-run the deliverable asked
for anyway.

## Not Done

### The acceptance command cannot be satisfied in the form written

The acceptance names:

```
go test -count=5 -run 'TestChatPublishDoesNotOvertakeQueuedInput|TestTerminalInputPending' ./cmd/agent/ ./internal/lineedit/
```

With ONE scope applying to BOTH packages, `go test` reports **"no tests to run"**
for the package whose pattern matches nothing: `TestChatPublishDoesNotOvertakeQueuedInput`
lives in `cmd/agent`, `TestTerminalInputPending` lives in `internal/lineedit`.
The set was therefore split by owner — every test the acceptance names is run, at
`-count=5`, in the package that owns it:

```
go test -count=5 -run 'TestTerminalInputPending' ./internal/lineedit/
go test -count=5 -run 'TestChatPublishDoesNotOvertakeQueuedInput' ./cmd/agent/
```

I did not satisfy it by ADOPTING the cross-package hole (adding a
`TestChatPublishDoesNotOvertakeQueuedInput` to `internal/lineedit`): that would
assert a cmd/agent harness behaviour from a package that cannot see the harness.
Both halves of the honest split are green and `-count=5` is preserved.

### The whole-package `cmd/agent` suite: red on master too, and by the same tests

`go test -count=1 ./cmd/agent/` is ~250-370s and runs detached. Its honest A/B
against a PRISTINE master worktree, same host, back to back:

| | branch `fix/lineedit-inputpending-coverage` (344.8s) | master (374.2s) |
|---|---|---|
| `TestTopLevelHelpCoversEveryDispatchedSubcommand` | FAIL | FAIL |
| `TestTmpDirSettingsValueReachesTheSpawnedChild` | FAIL | FAIL |
| `TestMergeWithoutFastStillHandsTheDivergedBranchOver` | FAIL | FAIL |
| `TestMergeSmallConflictStaysUnderTheCeiling` | FAIL | FAIL |
| `TestMergeHandOverFailureIsStampedAndCounted` | FAIL | FAIL |
| `TestMergeHandOverFailureCounterSurvivesRuns` | FAIL | FAIL |
| `TestMergeHandOverEscalationHaltsTheIdenticalRedecision` | FAIL | FAIL |
| `TestMergeCeilingIsIdempotentAcrossRuns` | FAIL | FAIL |
| `TestMergeCeilingFiresAndFilesReworkTask` | FAIL | FAIL |
| `TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness` | FAIL (whole-suite only) | not reported |

The nine are IDENTICAL on both — this branch adds no failure, and it changes no
production line at all. The tenth (`TestServeLockRefusalDiagnosis…`) passes in
isolation on BOTH (branch `ok 3.673s`, master `ok 3.225s`): a whole-suite
parallelism artifact, not a verdict, and the same artifact class the suite is
already red for.

One earlier whole-package run DID fail on this branch's own scratch:
`TestWorkerForegroundPrintsTheTaskOnItsConsole` refused to drive a `bin/gopi`
whose mtime was older than `internal/lineedit/editor.go` — because the red-proof
edits (editor.go → `if false`, then `git checkout`) moved that file's mtime past
the binary's. Rebuilt (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`), that
test is green (`ok 3.069s`). The stale-binary guard was working as designed; the
scratch was mine.

### `testdata/flakes.tsv` — no row added or refreshed, and here is the re-examination

The brief's constraint says the registry gate is bidirectional and that "any
touched test whose verdict can move needs its row added/refreshed". The check, in
the registry's own terms:

```
$ grep -n "Overtake\|lineedit" testdata/flakes.tsv
(no output — neither TestChatPublishDoesNotOvertakeQueuedInput nor any
 internal/lineedit test is registered)
```

The registry is not a list of tests that *could* fail; by its own header it is a
list of tests whose verdict is **not** a function of its inputs —
`deterministic: <mechanism>` names what makes the same code on the same host pass
and fail across repetitions. Every test touched by this branch is gated to the
opposite: the guards park the consumer until the tree is in a known state and
then read the verdict off the write stream — that is the property that makes them
guards, and the one reachable red is quoted in "Evidence" below. Registering a
*fixed* flake would also need an open ledger row to own the row, and `01a0c28b`
is completed.

I did NOT change the gate's semantics to make room for a row. If the reviewer
disagrees, the row belongs to the *gate* — a change to `internal/standards`
(flake registry) rather than to a test in this branch — and should be its own task.

### Not exercised by this branch

`cmd/agent`'s `scriptSource.InputPending` is inherited from `65d7e278` unmodified;
its behaviour is covered by the red-proof in "Evidence".

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0 (`BUILD OK`; `bin/gopi` written).
- Tests (acceptance set, split by owner as explained above; Rule 27 note: the
  instrument is `-count=N` over the tests themselves, never host load):

```
$ go test -count=5 -run 'TestTerminalInputPending' ./internal/lineedit/
ok  	github.com/nick/gopi/internal/lineedit	0.689s

$ go test -count=5 -run 'TestChatPublishDoesNotOvertakeQueuedInput' ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	1.490s
```

- Ran (Rule 31 — the surfaces this branch changed, executed for real):

```
$ go test -count=5 -run 'TestReadLoop_' ./internal/lineedit/     # new guard + control + determinism
ok  	github.com/nick/gopi/internal/lineedit	1.220s

$ go test -count=5 -race -run 'TestReadLoop_' ./internal/lineedit/
ok  	github.com/nick/gopi/internal/lineedit	3.248s

$ go test -count=5 -race -run 'TestTerminalInputPending' ./internal/lineedit/
ok  	github.com/nick/gopi/internal/lineedit	3.155s

$ go test -count=5 -run 'TestChatPublishDoesNotOvertakeQueuedInput' ./cmd/agent/   # seam already present from 65d7e278; re-run per the brief
ok  	github.com/nick/gopi/cmd/agent	1.625s

$ go test -count=1 ./internal/lineedit/      # whole package
ok  	github.com/nick/gopi/internal/lineedit	15.226s

$ go test -count=1 -run 'TestWorkerForegroundPrintsTheTaskOnItsConsole' ./cmd/agent/   # after the rebuild
ok  	github.com/nick/gopi/cmd/agent	3.069s
```

  One-process repetitions (the flake class's own instrument — Rule 27: the TEST is
  repeated, the host is never loaded):

```
$ go test -count=200 -run 'TestReadLoop_QueuedInputIsReadBeforeAPublishIsDrawn$' ./internal/lineedit/
ok  	github.com/nick/gopi/internal/lineedit	1.318s

$ go test -count=100 -run 'TestChatPublishDoesNotOvertakeQueuedInput$' ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	1.547s
```

- Result: the ordering guard is green, and it is green for the right reason — the
  guard's own log line on a green run:

```
input_ordering_test.go:395: ordering: the first keystroke was read at write index 0,
  the status draw landed at index 2 (1 chunk(s) carry it) — the read came first
```

  and the negative control observing the forbidden interleaving (it passes by
  MEASURING it, which is what keeps the guard above non-vacuous):

```
input_ordering_test.go:461: negative control: the draw landed at write index 2 with
  the first keystroke read only at write index 3 — the seam's answer is what orders them
```

- Commit: `3280fad7` (tests + report + DOC-INDEX row), `7a066214`, `5bd3b64a`,
  `5f75d154`, plus this A/B follow-up.

## Evidence (self-filed)

Red-proof, in both shapes the ticket asks about. Each was produced by editing the
tree, running the guard, and restoring immediately (`git checkout
internal/lineedit/editor.go`; `cmd/agent/lineedit_wire_test.go` restored from a
copy). After the reverts `git status --porcelain` listed only the two new
untracked test files; after the commits it prints nothing.

**(a) The production gate NEUTRALIZED** — `internal/lineedit/editor.go`, the
`EventWake` arm, `if e.inputPending() {` → `if false {`:

```
$ git diff --stat
 internal/lineedit/editor.go | 2 +-
 1 file changed, 1 insertion(+), 1 deletion(-)

$ go test -count=1 -run 'TestReadLoop_QueuedInputIsReadBeforeAPublishIsDrawn' ./internal/lineedit/
--- FAIL: TestReadLoop_QueuedInputIsReadBeforeAPublishIsDrawn (0.00s)
    input_ordering_test.go:385: the status draw overtook queued input — the draw landed at
    write-stream index 2, and the read loop only read its first keystroke after 3 chunk(s)
    had been written
FAIL	github.com/nick/gopi/internal/lineedit	2.035s

$ go test -count=1 -run 'TestChatPublishDoesNotOvertakeQueuedInput' ./cmd/agent/
--- FAIL: TestChatPublishDoesNotOvertakeQueuedInput (0.00s)
    lineedit_height_test.go:494: rows=4: the status draw overtook queued input — the 2-th
    recorded chunk drew the status, and the read loop only read its first keystroke after
    3 chunk(s) had been written
FAIL	github.com/nick/gopi/cmd/agent	1.501s
```

**(b) The SEAM's answer regressed** — cmd/agent's `scriptSource.InputPending`
returning `false` (the harness red-proof the fix commit's own devdoc measured), no
production file touched:

```
$ go test -count=1 -run 'TestChatPublishDoesNotOvertakeQueuedInput' ./cmd/agent/
--- FAIL: TestChatPublishDoesNotOvertakeQueuedInput (0.00s)
    lineedit_height_test.go:494: rows=4: the status draw overtook queued input — the 2-th
    recorded chunk drew the status, and the read loop only read its first keystroke after
    3 chunk(s) had been written
FAIL	github.com/nick/gopi/cmd/agent	1.639s
```

**(c) RESTORED** — both guards green at `-count=5`:

```
$ git checkout internal/lineedit/editor.go     # "Updated 1 path from the index"
$ go test -count=5 -run 'TestReadLoop_QueuedInputIsReadBeforeAPublishIsDrawn' ./internal/lineedit/
ok  	github.com/nick/gopi/internal/lineedit	1.220s
$ go test -count=5 -run 'TestChatPublishDoesNotOvertakeQueuedInput' ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	1.625s
```

So the brief's third question — "does the neutralized-gate red-proof still fire
once `scriptSource` implements `inputBacklog`?" — is **yes, twice over**: with the
production gate neutralized, and (without touching production) with the harness's
seam answering the regressed value. The second shape is the strictly stronger one
(it exercises the *answer*, not the *decision*), and
`TestReadLoop_NoOrderingGateLetsTheDrawOvertake` now reproduces it inside
`internal/lineedit` on every ordinary run.

**House gates, measured as an A/B, not asserted.** `internal/stylegate` and
`internal/standards` are ALREADY RED on pristine master; a branch that adds hits
would show a higher count, so the counts were extracted and diffed:

```
stylegate counts — master vs branch: diff EMPTY (identical)
  ignored-error 12 production/ceiling 0 · 230 test/ceiling 163
  unchecked-close 4 test/ceiling 0 · test-sleep 10 test/ceiling 0 · test-skip 3 test/ceiling 0

TestFlakeRegistryMatchesTheTestsItNames: ok on BOTH (branch 2.224s, master 2.707s)
TestDocManifestCoversEveryDevdocFile: RED on BOTH, same numbers
  (master 2117 rows/2166 files; branch 2118/2167 — exactly the one row this report adds)
```

## Files

| file | change |
|---|---|
| `internal/lineedit/input_ordering_test.go` | NEW — the package-level ordering guard, its determinism repeat and its negative control |
| `internal/lineedit/inputpending_pty_test.go` | NEW — the direct pty test of `(*Terminal).InputPending` + the degraded arm |
| `devdoc/lineedit-inputpending-coverage.md` | NEW — this report |
| `devdoc/DOC-INDEX.tsv` | one `historical` row for the report (the docclass gate requires every devdoc file classified, and the class banner on line 1) |

No production file was modified: the gap was coverage, not behaviour.

## Commit

On `fix/lineedit-inputpending-coverage` (worktree `/home/user/workspace/gopi-lineedit-gap`,
off master `e70119f5`):

- `3280fad7` `fix(lineedit): guard the input-ordering seam and exercise Terminal.InputPending over a pty` — the two tests, this report, the DOC-INDEX row (960 insertions, no production file touched).
- `7a066214` `docs(lineedit): record the whole-package cmd/agent run and its stale-binary guard hit`.
- `5bd3b64a` `test(lineedit): drop the unused label from the pty probe's wait helper`.
- `5f75d154` `docs(lineedit): name the branch's commits in the report`.
- `de8427d3` `docs(lineedit): the whole-package cmd/agent A/B — the same nine tests fail on pristine master`.
