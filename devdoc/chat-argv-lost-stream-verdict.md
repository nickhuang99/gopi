<!-- doc-class: normative -->
# The chat argv fixture's lost stream: one event, two verdicts (and the attribution sweep that did NOT name the closer)

**Task**: 01a12bb8-7fe8-799f-b56e-b4b66bf0fb9c · **Branch**: `task/01a12bb87fe8799fb56eb4b66bf0fb9c`
**Surface**: `cmd/agent/chat_argv_test.go` (`chatRunArgs` and its drain/release helpers)
**Base**: `50a09f7a05e5e12c1fc8271b9e3877b481c6f531`
**Supersedes the open half of**: `devdoc/chat-argv-fixture-release.md` (§"Not covered —
the suite-wide releaser is not named")

## The occurrence, verbatim

A full `cmd/agent` package run of `TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--help`
emitted BOTH of these about ONE event, four lines apart:

```
chat_argv_test.go:897: chatRunArgs: the stderr stream's read end (fd 7, identity pipe:[52641246])
    was released by a path OUTSIDE this fixture: … The case's streams and exit status were captured
    before the release and its assertions stand, so this is recorded and not fatal …
chat_argv_test.go:897: read the stderr stream (fd 7, which this fixture opened): read |0: bad file descriptor
--- FAIL: TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--help (0.00s)
```

The release half judged the event by **what it cost the case** ("recorded and not
fatal"); the drain half judged the **same** event by **which syscall raised it**
("fatal"). Raw evidence, unchanged:
`~/.gopi/sessions/01a12b79-24cb-7eee-995d-3125dd372de4/artifacts/chat-argv-fd-leak-raw.txt`.

## The measured attribution (what the record DOES establish)

1. **The releasing path is NOT the fixture, and not a stale wrapper of the
   fixture's own.** With `strace -f -k -e trace=close` over the same test scope,
   every EBADF close carried a stack that goes through the fixture's `os.File`
   wrapper (`internal/poll.(*FD).destroy` ← `os.(*file).close` ←
   `chatRunArgsReleaseStream`) — i.e. the wrapper is reporting a release it did not
   make. The 14 `*os.File` claims this package's tests make are all reachable
   across their call, so no finalizer owns any of them either
   (`runtime.runfinq` appears **0** times in that trace).
   Evidence: `…/artifacts/strace-k.log` (85 571 lines).
2. **The number the fixture loses has been in use since before its `os.Pipe`
   call** — so the loss is a recycled number, not a same-syscall EBADF. Two
   independent anchors, both from the second full-package run
   (`…/artifacts/fullrun-PREFIX.out`, lines 63–90, a `-test.v` binary run):
   - the WRITE-end guard reports `fd 7, identity pipe:[52796405]` for a pipe
     created microseconds earlier — the same number the occurrence used — although
     in that process the number had been recycled since;
   - one descriptor at that number answered `(deleted)` (a stale `*os.File`), while
     the fixture's own fresh wrapper answered `pipe:[…]` for the SAME number: two
     sightings of one number prove the number was released and re-handed
     (`/proc/self/fd` sweep from `chatRunArgsReuseCensus`, printed by the guards).
3. **The fixture defends against it deterministically.** Both guard tests
   (`TestChatRunArgsForeignReleaseOfAReadEnd…`, `…OfAWriteEnd…`) produce a real
   foreign release on a real number and pass in **every** context measured —
   isolated, 25×, and inside the full package run (lines 76, 90 of the same file).

## What it does NOT establish — the closer is NOT named

The sweep reproduced the shape but **never observed the closure itself**, so this
report names no actor (memory #7916: "do NOT name a closer from a green run"):

- `strace -f -q -e trace=close,dup,dup2,dup3` over the chat-argv scope produced
  EBADF closes only at low, per-process numbers (the fixture's own stale wrappers
  among them), and **none** under a pass naming the pid the fixture's own release
  ran in (`…/artifacts/strace-chatargv.log`, grep `close(7)`); `-k` showed no
  third-party path either.
- In a `-test.v` full-package run the same log shows fd **3** where the occurrence
  had fd **7** — the number moves with the process, which is why an absolute fd is
  not evidence of an actor (this is also why the sibling devdoc's `|0` misreading
  happened).
- The candidate **classes** were fingerprinted and are recorded for the next
  occurrence rather than promoted to a finding (Rule 41: a measured gap, not a
  claim — and this is filed as its own task row rather than guessed here):
  - `serve_lock_refusal_diagnosis_test.go`'s cross-process helper blocks on
    `io.Copy(io.Discard, os.Stdin)` and the parent closes that stdin as cleanup,
    across an `os/exec` boundary;
  - the PTY fixtures (`chat_dump_geometry_pty_test.go` and its 15 siblings) which
    open `/dev/ptmx`/`/dev/pts/N`, close the master mid-test and re-hand the
    number;
  - the several `os.Stdout/os.Stderr` swap fixtures in this package that close a
    write end under the suite's own standard streams.

## The repair

**One event, one verdict — decided by what the failure cost the CASE.** The read
and the release halves now share the predicate the release half already used:

```go
chatRunArgsReadVerdictOf(own, other chatRunArgsRead) chatRunArgsReadVerdict
// own.err == nil                          → chatRunArgsReadClean
// own.s != "" || other.s == ""            → chatRunArgsReadLostData   (FATAL)
// otherwise (lost before its first byte)  → chatRunArgsReadRecordedLoss (recorded)
```

- **Fatal, and it must stay fatal**: a stream that carried BYTES and then errored
  is a truncated claim (assertions over a prefix), and a case where NEITHER stream
  carried anything has no captured claim at all.
- **Recorded**: a stream lost BEFORE its first byte, with the sibling carrying the
  claim and the exit status — which is precisely the help walk (`--help` asserts
  the exit status and a non-empty stdout). Failing there destroyed a case whose
  every assertion had held; that is the defect.
- Both arms now name the stream, the number, the identity, the kernel's reading of
  the number and the census of who holds the pipe — so a foreign release can never
  again surface as a bare `read ... bad file descriptor` with no attribution.
- `chatRunArgs` now names **all four** ends it opened (`outRfd, outWfd, errRfd,
  errWfd`) to both release calls, so the census' survived-claim filter is complete.

## Red proof (Rule 10.5), taken this session

Temporarily restoring the pre-fix rule (any read error ⇒ fatal) in
`chatRunArgsReadVerdictOf`, then reverting:

```
$ go test -count=1 -run 'TestChatRunArgsLostStreamVerdictAgreesWithItsRelease' ./cmd/agent/
--- FAIL: TestChatRunArgsLostStreamVerdictAgreesWithItsRelease (0.00s)
    chat_argv_test.go:525: a stream lost BEFORE its first byte (sibling carrying the claim)
        classified as 1, want the recorded-loss verdict (2) — the whole-package occurrence's
        `read the stderr stream ... bad file descriptor` would again end a case whose every
        assertion had held
    chat_argv_test.go:552: the real lost stream (fd 3) classified as 1, want the recorded-loss verdict (2)
FAIL
# after reverting the patch:
ok  	github.com/nick/gopi/cmd/agent	3.911s
```

The guard drives BOTH halves off ONE real foreign release in one test, and pins the
fatal arms (truncated stream; both streams empty) beside it, so the non-fatal arm
cannot be bought by widening acceptance.

## Rule 27 note

No load was generated at any point. The flake was sought by **repetition** of the
existing scopes (`-count=25`, 10× separate runs) and by **one** full-package run per
tree, exactly as memory #7916 prescribes. The full-package acceptance runs were
background jobs, never foreground calls.

## Attributing the OTHER reds in a full run — the base worktree is the instrument

Both full-package runs of this epic came back `exit=1 outcome=fail`, and **neither
red was this branch's**: the diff touches only `cmd/agent/chat_argv_test.go` +
`devdoc/DOC-INDEX.tsv` + two devdocs (`git diff --stat master..HEAD`). The
measurement that turns that from a claim into a fact:

```bash
git worktree add --detach /home/user/workspace/gopi-verify-01a12bb8 HEAD~1   # the BASE
cd /home/user/workspace/gopi-verify-01a12bb8
go test -count=1 -run 'TestChatCookedFallback_NonTTY$|TestCodeSearchBackendNoSymbol$' ./cmd/agent/
go test -count=1 -run 'TestChatVoice_TheRunOfFailuresEndsTheMode$|TestRunPathPlainPromptIsVerbatim$' ./cmd/agent/
# ALL FOUR fail at the base: outputs artifacts/base-foreign-tests.out, artifacts/base-foreign2.out
```

`git diff --stat` is the weak signal (it names files); the base worktree is the
strong one (it names a verdict at a commit that cannot contain the change). The
four reds have **two distinct root causes**, both invisible from this branch's own
diff, and each is filed (`--tags bug`, Rule 41):

| test | class | filed |
|---|---|---|
| `TestChatCookedFallback_NonTTY` | deterministic: fixture asserts the pre-turn-stamp shape | `01a12be0-6d86-7b85-a38d-4b5dc30e72a7` |
| `TestCodeSearchBackendNoSymbol` | flake by concurrency on the shared `.gopi-tags` index (PASS in one run, FAIL in the next) | `01a12be1-ec32-7e90-bc74-561a9b38d2ff` |
| `TestRunPathPlainPromptIsVerbatim` (+ `TestChatVoice_TheRunOfFailuresEndsTheMode`) | same turn-stamp root cause / one inference | `01a12be3-12bc-78c7-8e0f-808ea3200dc1` |

## The attribution line is NOT automatically evidence of a suite leak

Three `released by a path OUTSIDE this fixture` lines appear in the post-fix
full-package stream — and **all three are produced by THIS fixture's own guard
arms**, which deliberately call `syscall.Close(fd)` to manufacture the shape
(`chat_argv_test.go:541` in `TestChatRunArgsLostStreamVerdictAgreesWithItsRelease`,
plus the two `…ForeignReleaseOf…` guards). Grep which TEST emitted each line before
treating it as a catch:

```bash
python3 -c "import json;[print(d.get('Test')) for d in map(json.loads,open('<stream>.json')) if 'released by a path OUTSIDE' in d.get('Output','')]"
```

memory #7916 point 5 warns about exactly this, and it is why this doc does not
report "a real foreign release was caught in the full run": the guard's own arms
are the source, and the vocabulary walk the epic is about emitted the line in
**neither** full run.

## Not covered (open, honestly)

- **The closing path is still unnamed** — see above; the instrument now makes the
  next occurrence self-attributing (identity + census + stack), and the candidate
  classes are enumerated. A `--tags bug` row is filed for it with the raw evidence.
- `testdata/flakes.tsv` deliberately carries **no** row for this family: a registry
  row exists for a test whose verdict MOVES across repetitions, and after this
  change the fixture's verdict does not move (a foreign release is recorded, not
  failed). Registering it would be a suppression entry for a test that no longer
  fails — the shape that gate's own header rejects.
