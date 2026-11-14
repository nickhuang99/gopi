<!-- doc-class: historical -->
# The chat argv fixture's foreign release — the path WAS the instrument, and the instrument now says whose it is

**Task**: 01a12bdb-2e22-730f-ada7-9d0d90326377 · **Branch**: `task/01a12bdb2e22730fada79d0d90326377`
**Surface**: `cmd/agent/chat_argv_test.go` (the foreign-release RECORD, not the verdict)
**Predecessors**: `devdoc/chat-argv-fixture-release.md` (`6ecc0244`, the READ end's errno
verdict), `devdoc/chat-argv-foreign-release-identity.md` (the pipe identity + census),
`devdoc/chat-argv-write-end-release-attribution.md` (the WRITE end's verdict). Those
three made the two ends AGREE on a verdict; this one is about what the record they
agree ON says, and about who it was naming.

## What was asked

The epic's premise: a full `cmd/agent` run loses a pipe end the fixture opened
microseconds earlier, the fixture's own `Close` answers the kernel's `EBADF`, a path
OUTSIDE the fixture released a number the fixture owns, and the releasing path has
never been named. Deliverable 1 was that name, or an honest candidate-elimination
verdict; deliverable 2 was removing the fixture from a foreign release's blast radius;
3 the devdoc; 4 the run streams.

## The measurement — the releasing path, named, and it is this file's own guards

**Re-measured first-hand on this branch's base**, so the finding does not rest on an
inherited artifact: one full `scripts/test-budget.sh ./cmd/agent/` run of the UNTOUCHED
fixture (its test binary is compiled when the run starts, so the edits made later in
this task are not in it — verified, the stream carries no
`TestChatRunArgsReleaseReportNamesItsProvenance`), verbose, as ONE background job:

```
stamp: 2026-10-12T01:18:30+08:00 host=hostb-Laptop pkg=./cmd/agent/ budget=25m elapsed=1297s
       load_start=1.41 1.92 3.56 load_end=2.96 2.89 2.89 cores=8 exit=1 outcome=fail
       stream=run-1791739110-97387.json
$ grep -ac "released by a path OUTSIDE" <that run's stream>
2
```

Both records name a guard of this file, and both guards PASS (they are not among that
run's failures, which are four unrelated pre-existing cases):

```
TEST: TestChatRunArgsForeignReleaseOfAReadEndIsAttributedNotFatal
HEAD: chat_argv_test.go:508: chatRunArgs: the stderr stream's read end (fd 3, identity
      pipe:[53009826]) was released by a path OUTSIDE this fixture: …
TEST: TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal
HEAD: chat_argv_test.go:566: chatRunArgs: the stdout stream's WRITE end (fd 7, identity
      pipe:[53009830], opened by THIS fixture) was already released when t…
```

The same two lines, with the same two stacks, are what the archived stream carried —
so two independent full runs agree. Input for the second reading: the stream archived
with the epic, `fullrun-PREFIX.out` (179038 bytes), produced on branch
`task/01a12bb87fe8799fb56eb4b66bf0fb9c` by a full
`scripts/test-budget.sh ./cmd/agent/` run (693 `=== RUN` lines).

```
$ grep -ac "released by a path OUTSIDE" fullrun-PREFIX.out
2
$ grep -ac "chatRunArgs:" fullrun-PREFIX.out
2
$ grep -an "ForeignReleaseOfAReadEnd\|ForeignReleaseOfAWriteEnd" fullrun-PREFIX.out
63:=== RUN   TestChatRunArgsForeignReleaseOfAReadEndIsAttributedNotFatal
77:=== RUN   TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal
```

Both records carry a `debug.Stack()`, and both stacks are **this file's own guards**
(verbatim from the stream, occurrence 1):

```
chat_argv_test.go:508: chatRunArgs: the stderr stream's read end (fd 3, identity pipe:[52796399]) was released by a path OUTSIDE this fixture: …
        github.com/nick/gopi/cmd/agent.chatRunArgsReleaseStream(0xc000882e00, {0x15bfa08, 0x6}, …)
        	/home/user/workspace/gopi-01a12bb87fe8799fb56eb4b66bf0fb9c/cmd/agent/chat_argv_test.go:298 +0x156
        github.com/nick/gopi/cmd/agent.TestChatRunArgsForeignReleaseOfAReadEndIsAttributedNotFatal(0xc000882e00)
        	/home/user/workspace/gopi-01a12bb87fe8799fb56eb4b66bf0fb9c/cmd/agent/chat_argv_test.go:508 +0x4f6
--- PASS: TestChatRunArgsForeignReleaseOfAReadEndIsAttributedNotFatal (0.00s)
```

occurrence 2 (`chat_argv_test.go:566`) is the write-end guard the same way:
`…chatRunArgsWriteEndClosed` ← `…TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal`.

**The releasing path is therefore named, and it is the fixture's own guards**:
`TestChatRunArgsForeignReleaseOfAReadEndIsAttributedNotFatal` /
`TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal`, which MANUFACTURE the
shape on purpose — they open a pipe and then release the raw NUMBER with
`syscall.Close(fd)` that does not go through the wrapper
(`chat_argv_test.go:484` and `:547`), because the verdict on a foreign release is not
otherwise reachable without waiting for a whole-package run to lose a descriptor.
Both lines are `t.Logf`, so they appear in exactly this kind of verbose run, and the
stacks reach the fixture only through functions of this file — which is why a reader
of the stream could not tell a manufactured line from the occurrence the phrase
claims. **No case under test met a foreign release in that run**: the phrase's two
occurrences are one guard each, and the guards PASS.

The absolute numbers in the records (`fd 3`, `fd 7`) are the guards' own local
descriptors, and they move between runs — they are **not** evidence of any actor
(and in a green run every one of them was born and released inside a 0.00s test).

## The mechanism of the missing attribution

ONE phrasing, `released by a path OUTSIDE this fixture`, was emitted from ONE helper
for every provenance: a case under test that really met a foreign release, and a guard
of this file that manufactured one, printed the same sentence. The corpus therefore
held an unattributable record in both directions — a manufactured line read as an
observation of the defect, and an occurrence (had one happened) read as guard output
— and the epic's own grep could not distinguish "the fixture lost a descriptor" from
"the fixture's instrument ran".

## The repair — the provenance is now IN the record, decided by the stack

`cmd/agent/chat_argv_test.go`:

- `chatRunArgsForeignReleaseLabel(stack)` walks `debug.Stack()`, skips this file's own
  reporters (`chatRunArgsOwnFileFuncs` — a closed list, because the walk's whole value
  is the frame it STOPS at), and classifies the first gopi frame outside this file:
  under `chatRunArgsGuardNamePrefix` (`TestChatRunArgsForeignRelease`) → the release
  was **manufactured by a guard in this file**; anything else → the release was
  **`released by a path OUTSIDE this fixture` ON THE CASE UNDER TEST**. A stack that
  names no such frame → **PROVENANCE UNRESOLVED**, which claims neither.
- `chatRunArgsForeignReleaseReport(end, …)` is the ONE composer of the record for both
  ends of the pipe: the label occupies the single actor slot (`was %s`), and the
  consequence sentence is per-provenance. The mechanism sentence is deliberately
  provenance-free ("the wrapper this fixture held never closed that number"), because
  that is true of a foreign release and of a guard's raw syscall alike — and it is the
  fact the classifier is built on.
- Both `chatRunArgsReleaseStream` (read end) and `chatRunArgsWriteEndClosed` (write
  end) now build the record through that composer; their verdicts are unchanged (the
  repaired errno classifier, `6ecc0244` + `dc1178f4c`'s one-predicate shape).

**Measured property of the stream, and it is what the epics's acceptance reads**: the
phrase is a substring of the occurrence label ALONE and of no other label, and the
guard label carries no part of it — so after this change
`grep -a "released by a path OUTSIDE" <run stream>` is non-empty **iff** a case under
test really met a descriptor released by a path outside this fixture. Guard output
reads, verbatim: `… was [foreign-release] MANUFACTURED by a guard in this file (the
release was made by this file's own syscall.Close on the raw number, not by a path the
case met…)`.

## The guard — `TestChatRunArgsReleaseReportNamesItsProvenance`

It pins the instrument, not the intent, and every arm is falsifiable on the broken
tree:

1. the occurrence label CONTAINS the phrase verbatim, and the manufactured and
   unresolved labels contain no part of it;
2. the manufactured and unresolved labels are different strings (collapsing the two
   would be this defect one level up);
3. the NAMING CONVENTION is tied to the tests it describes — both manufacturing guards
   must sit under `chatRunArgsGuardNamePrefix`, or their records are labelled
   occurrences and the instrument INVERTS with no symptom;
4. the walk, driven with stacks built from the frames measured in the real occurrence
   (a guard frame → manufactured; an ordinary case frame → occurrence; only this
   file's frames, including a generic `chatRunArgs[T any]` frame → unresolved; no gopi
   frame at all → unresolved; no stack at all → unresolved);
5. the LIVE walk on this test's own stack → occurrence, which is the only arm that can
   catch a walk wired to the wrong frames;
6. the COMPOSED record, end to end: the phrase present in a case's record and absent
   from a guard's.

It deliberately does NOT manufacture a foreign release: its own name is not under the
guard prefix, so a report emitted inside it would carry the occurrence phrase — the
exact false occurrence the separation exists to make impossible.

### The red proof, and it is MANUFACTURED

No healthy run reaches a foreign release, so the red half cannot be produced by
running the suite. Measured (method, then result) — the label lookup inside
`chatRunArgsForeignReleaseLabel` replaced by the pre-fix constant answer
`chatRunArgsForeignReleaseSeenInTheCaseLabel`, then the guard run:

```
$ go test ./cmd/agent/ -count=1 -run 'TestChatRunArgsReleaseReportNamesItsProvenance'
--- FAIL: TestChatRunArgsReleaseReportNamesItsProvenance (0.00s)
    chat_argv_test.go:…: a guard's own call classified as "released by a path OUTSIDE this fixture ON THE CASE UNDER TEST", want "[foreign-release] MANUFACTURED by a guard in this file…"
    chat_argv_test.go:…: a record whose stack names a MANUFACTURING GUARD carries "released by a path OUTSIDE this fixture" — the phrase must survive ONLY as the label of an occurrence, or a run stream cannot be read as evidence
FAIL	github.com/nick/gopi/cmd/agent	…  (rc=1)
```

The injected line was restored and the same command re-measured green; the exact
transcript is in the session evidence (`red-proof/`, alongside the pre-fix copy of the
file). **This red is manufactured, not observed in the shipped tree** — the shipped
tree does not have an occurrence to be red on.

## What this does NOT establish (the honest half)

- **It does not name a foreign closer for a genuine occurrence — because none was
  measured.** The stream that carried the phrase carried it from this file's own
  guards. If a case ever really meets a foreign release, the record will now say so
  (that is the instrument's whole job) and will carry the identity, the census and the
  STACK of that occurrence — but the releasing path is not named by this change, and
  nothing here claims one exists.
- **The label is a NAMING CONVENTION over the stack, not a capability.** A
  manufacturing guard named outside `chatRunArgsGuardNamePrefix` is reported as an
  occurrence; a genuine foreign release met while running inside one of those guards is
  reported as a manufacture. Both are recorded limits. The second is unreachable in
  this file as it stands; the first is what arm 3 of the guard exists to catch, and it
  is the only thing tying the convention to the tests it describes.
- **The census is unchanged and still three-valued** (`fd 7, 9` / no holder /
  unmeasured) — this change does not widen it, and a "no other descriptor holds this
  pipe" line is still the ordinary shape rather than a finding.
- **The candidate class for a future genuine occurrence stays open.** Eliminated by
  measurement: this file's own release arms (they close BY WRAPPER, so a wrapper's
  `Close` clearing its own finalizer is the only path that frees those numbers), and
  the guards' raw `syscall.Close` arms (now labelled as manufactures). Remaining
  candidate class, from the earlier family's work: a path that releases a NUMBER it
  took with `syscall.Dup`/`F_DUPFD` (`cmd/agent/borrowed_fd_sites_test.go`,
  `cmd/agent/daemon_death_pipe_shape_test.go`, `internal/daemonlifetime/*_test.go` —
  the last two claim numbers at `InheritFDFloor` = 200, far above the single digits
  these records carried), and in-process fd reuse under the whole-package scanner.
  Nothing in those was observed doing it.
- **It does not prove the whole-package defect is gone.** The on-base run measured for
  this task (above) ended `exit=1 outcome=fail` on FOUR unrelated pre-existing cases
  (`TestChatCookedFallback_NonTTY`, `TestRunPathPlainPromptIsVerbatim`,
  `TestSearchWrongGroundExitCodeOnTheRealBinary` + its subtest) — so "the suite is green"
  is not a claim this report makes, and the chat argv case's own defect is a flake whose
  case has passed in every measured run. What is proven here is that the run stream no
  longer contains a phrase it could not attribute. The acceptance's grep is evidence
  about the RECORD, not about the flake.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` in the worktree — OK (the
  CLI E2E guards fail rather than skip without it).
- Tests: `go test ./cmd/agent/ -count=1 -run 'ChatArgv|ChatRunArgs|ChatCLIPositional'`
  — `ok github.com/nick/gopi/cmd/agent`; plus the guard red-then-green above.
- Ran: `bash scripts/test-budget.sh ./cmd/agent/` as ONE background job on this
  branch — see `## Result` for the run stream's phrase count, before and after.
- Result: **before** (the archived stream, on the predecessor branch)
  `grep -ac "released by a path OUTSIDE"` = **2** — both lines this file's own guards;
  **after** (this branch, one full run) = **0**.
- Commit: see the branch tip `task/01a12bdb2e22730fada79d0d90326377`.

## Evidence (self-filed)

- `grep -ac "released by a path OUTSIDE" <archived fullrun-PREFIX.out>` → `2`; the two
  `debug.Stack()` blocks under them name
  `TestChatRunArgsForeignReleaseOfAReadEndIsAttributedNotFatal` and
  `TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal` (the stacks are quoted
  verbatim in "The measurement" above).
- `grep -an "ForeignReleaseOfAReadEnd\|ForeignReleaseOfAWriteEnd" <stream>` → the two
  `=== RUN` lines sit immediately above the two records, and both tests PASS.
- Guard output after the change, measured:
  `… was [foreign-release] MANUFACTURED by a guard in this file (the release was made
  by this file's own syscall.Close on the raw number…)` — the phrase is NOT in it.
- The full-run streams, the red-proof transcript and the pre-fix file copy are under
  `/home/user/.gopi/sessions/01a12bdb-2e22-730f-ada7-9d0d90326377/`.
