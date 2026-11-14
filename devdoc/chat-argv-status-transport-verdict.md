<!-- doc-class: historical -->
# The chat argv fixture's status verdict rides on the transport, not on the fd

Task `01a12c9c-83af-7e52-925c-3cf9ece43fa5`.
The red this closes, as reported: `cmd/agent` **whole-package** runs failed
`TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/-h` with
`runChat([-h -h]) = 1, want 0`, while the case was green when run alone.

## 1. Verdict: not this merge, and the same family a fifth time

**Not merge-introduced — measured, not inferred.** At the state the red is
reported against — the tree BEFORE this run's fix — the file under test was
byte-identical in this branch, in its merge-base with master, and on master:

```
$ git rev-parse f0c7b57bb^:cmd/agent/chat_argv_test.go    # the pre-fix tree
01ce8f3a22f381ca35c6fb50ad68eef7c14a9bbc
$ git rev-parse 0b743cbac:cmd/agent/chat_argv_test.go     # branch merge-base with master
01ce8f3a22f381ca35c6fb50ad68eef7c14a9bbc
$ git rev-parse master:cmd/agent/chat_argv_test.go
01ce8f3a22f381ca35c6fb50ad68eef7c14a9bbc
$ git diff f0c7b57bb^ -- cmd/agent/chat_argv_test.go      # no output
```

**Read that block against `f0c7b57bb^`, never against the tip** (scope note added
after the fact, 2026-10-13): the fix commit `f0c7b57bb` IS this file, so the same
`git diff master -- cmd/agent/chat_argv_test.go` on the tip prints 652 changed
lines, and a reader who ran it there would find the doc contradicting its own
tree. The identity claim is about the pre-fix revision, which is the revision the
epic's constraint names; the tip divergence is this run's own work
(`9a10d34bafd6c88fc5f28ce8f837a183dc386e60` vs `01ce8f3a22f381ca35c6fb50ad68eef7c14a9bbc`).

One blob, three revisions, empty diff. At that revision the branch touched 34
other paths and not this one, so the failure cannot have been introduced by the
merge.

**What it is instead.** The same fixture family as
`devdoc/chat-argv-fixture-release.md` (commit `6ecc0244`),
`chat-argv-write-end-release-attribution.md`,
`chat-argv-write-end-release-landing-check.md`,
`chat-argv-foreign-release-attribution.md`,
`chat-argv-foreign-release-identity.md` and `chat-argv-lost-stream-verdict.md`:
a whole-package-only EBADF on a pipe end this fixture installed as
`os.Stdout`/`os.Stderr`. Whole-package runs are a MOVING TARGET (memory #7047:
one frozen tree, three runs → pass/pass/fail-on-a-different-case), and per
`Rule 27` no load may be manufactured to force the reproduction.

## 2. Where the release comes from, and the one remaining gap in the fixture

The chain, with code references:

1. `chatRunArgs` (`cmd/agent/chat_argv_test.go`) opens two `os.Pipe` pairs and
   installs the **write ends** as `os.Stdout`/`os.Stderr` for the duration of the
   call under test. The call's output therefore travels through descriptors this
   fixture owns but does not exclusively control.
2. `-h` answers by writing the page and returning `chatExitOK`
   (`cmd/agent/cli_chat.go:1787` — and the positional-beside-query arm at
   `:1849`):

   ```go
   if _, werr := os.Stdout.WriteString(chatUsage); werr != nil {
       fmt.Fprintf(os.Stderr, "%v\n", werr)
       return chatExitFailed
   }
   ```
3. When a NUMBER under that write end has been released by a path outside the
   fixture, that write fails, `runChat` returns `chatExitFailed` **faithfully**,
   and the case's `code != chatExitOK` fires. The fixture's report names the
   event (`close |1: bad file descriptor`) but the case's verdict was still read
   off the exit status alone.

The fixture had already closed that gap for *releases* (`chatRunArgsReleaseShapeOf`)
and for *streams* (`chatRunArgsReadVerdictOf`) — but **the exit status was still
read bare**, which is exactly the path the reported failure takes.

The real release path was not observed in this session (see §4), and that is
stated as a limit rather than papered over. What IS pinned is the causal link:
the `TestChatRunArgsForeignReleaseOfTheStdoutItCarried` guard reproduces the
mechanism deterministically by releasing the write end through a raw
`syscall.Close` and measuring `runChat([-h]) == chatExitFailed`.

## 3. The fix: one verdict, taken on the route the status flows through

`chatRunArgsStatusVerdictOf(code, want, ev)` — three answers, so the arm that
excuses a failure can never be reached by accident:

| answer | when | effect on a case |
|---|---|---|
| `chatRunArgsStatusOnTarget` | the status agrees | nothing to decide (a release may not turn a pass into a failure) |
| `chatRunArgsStatusTransportLost` | it disagrees AND the fixture's own record shows a **foreign** release (EBADF) on one of the four ends | **recorded, not fatal** |
| `chatRunArgsStatusDisagrees` | it disagrees with no such release — including an **UNOBSERVED** record | fatal, unchanged |

The record is `chatRunArgsStreamEvidence`, filled by the new
`chatRunArgsWitnessed` (the old `chatRunArgs` is now a thin sampling face that
discards it). It carries each end's fd number, its kernel identity
(`pipe:[inode]`), its release verdict and errno, plus both drained streams and
their verdicts.

Two supporting changes keep the fatal arms intact while moving them off the
fixture's floor and onto the caller:

- `chatRunArgsStreamDrained` now **returns** its verdict instead of calling
  `t.Fatalf`; `chatRunArgsAssertStreamsSound` carries the pre-existing fatal rule
  for the cases that read the streams (the sampling callers), so the arm is kept
  — it is simply no longer a rule of the fixture that answers for claims it never
  examined.
- The argv walk (`TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses`) takes
  the verdict from the record, and — whichever arm it lands on — asserts the
  product evidence its claim actually rests on. That claim is its own
  pure-decision function, `chatRunArgsHelpAnswerVerdictOf`, with five arms pinned
  by the guard: the page is present (clean), the read end was lost before it
  carried a byte (**unobservable** — recorded, the status above is the evidence),
  the stream was **truncated** (fatal: a prefix is not the page), the drain was
  clean but carries no page (fatal), and the record was **never filled** (fatal,
  and named — an unrun observation is not a pass). The naive spellings are why it
  is a classifier: `lost || page missing` reds the walk for a loss the fixture
  already recorded as non-fatal, and `!lost && page missing` lets a truncated page
  through as a pass — a silent hole in the very case the repair is about.
  A lost transport therefore does not buy the walk a free pass.

Nothing was deleted from the existing vocabulary: `chatRunArgsReleaseShapeOf`
keeps all four arms (nil / `os.ErrClosed` / EBADF / unknown), every existing guard
still asserts them, and `EBADF` stays distinguishable from `os.ErrClosed` —
`TestChatRunArgsReleaseClassifierPinsBothSidesOfTheVerdict` is unchanged.

### 3a. A third provenance found while wiring the record (measured)

The provenance instrument (`chatRunArgsForeignReleaseLabel`) separates a release
the CASE met from one this file's own guards MANUFACTURED — by a **naming
convention**: `chatRunArgsGuardNamePrefix = "TestChatRunArgsForeignRelease"`.
`TestChatRunArgsLostStreamVerdictAgreesWithItsRelease` manufactures the shape at
`chat_argv_test.go:949` (raw `syscall.Close` on a number it then hands to
`chatRunArgsReleaseStream`) and its name is **not** under that prefix — so its
record carried the *occurrence* phrase. A reader grepping a whole-package run for
"a release the case met" counted this file's own guard. Same defect class as the
one the two labels were introduced to remove, one level up.

Closed with a closed set (`chatRunArgsManufacturingGuards`) that the walk reads
*before* the prefix test, plus a guard arm that asks the walk about all three
manufacturing tests — including the one named outside the convention. The
convention is kept, because it is what a new guard should follow.

**Caught live in a whole-package run (measured, 2026-10-13).** The claim above
was not argued from the source. In a 600s whole-package run of this branch's
base the occurrence phrase appeared **exactly once** in 8634 lines — and that one
line was the misattribution:

```
$ grep -c "released by a path OUTSIDE this fixture" wpkg-run1.log
1
```

`chat_argv_test.go:748` reported `… was released by a path OUTSIDE this fixture
ON THE CASE UNDER TEST`, and the stack printed under it named
`TestChatRunArgsLostStreamVerdictAgreesWithItsRelease` — a test in this file that
manufactures the shape with its own `syscall.Close` (`:949`). So the instrument
labelled this file's own guard as a release the case under test met, in a real
run, with no symptom. After this change the same guard's record reads
`[foreign-release] MANUFACTURED by a guard in this file`.

## 4. Honest limits

- **The occurrence was not observed in this session, and this document does not
  claim its absence.** One whole-package run was taken to completion
  (`go test ./cmd/agent/ -count=1 -v`, log `wpkg-run1.log`): it ended
  `FAIL github.com/nick/gopi/cmd/agent 600.446s` with
  `panic: test timed out after 10m0s` (what was running at the panic:
  `TestRunPathRefusesVerbWithAmbientPromptInEnv`), and
  `TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses` — the case the epic
  reports — **passed** in it. Two measured consequences: **the instrument was
  wrong** — a bare `go test` bypasses the repo's declared per-package budget
  (`test_budget="25m"` in `scripts/test-budget.sh`, reached via
  `make test-budget PKGS=./cmd/agent/`, a trip reported as a TIMEOUT at exit 3,
  "never as a bare FAIL"; `devdoc/test-budget-single-source.md`) — and the
  occurrence did not reproduce. Per memory #7047 a whole-package run is a
  moving target, so a green run proves nothing about the defect's absence.
- **The instrument was over-reporting**, which the same run showed (§3a): the
  only occurrence-phrased line in 8634 was this file's own guard. That is the
  strongest single reason to take the fixture's *attribution* seriously before
  hunting a closer — a record that names a guard as a leak sends the next reader
  after the wrong actor.
- **No closer is named.** The fixture's *own* guards release raw numbers with
  `syscall.Close` (`chat_argv_test.go:949/1223/1286/1373/1381`), and so do
  `cmd/agent/borrowed_fd_sites_test.go` and
  `cmd/agent/daemon_death_pipe_shape_test.go`; the in-process `fd := int(r.Fd())`
  + `syscall.Close` pattern elsewhere in the package is the candidate class. That
  is a candidate CLASS, not an attribution — the run that closes a foreign number
  has not been caught in the act.
- **A released transport and a genuine product failure can coincide.** The record
  cannot separate them; it carries the release, not a counterfactual run without
  it. That is why the walk keeps asserting the page even on the recorded arm
  (`chatRunArgsHelpAnswerVerdictOf`).
