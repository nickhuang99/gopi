<!-- doc-class: normative -->
# The session fixture's views are partitioned ONCE — and a gate that keeps it so

**Class**: normative (a standing claim about this tree, with the check that
enforces it). **Rule 10.5** (a fix ships a minimal repro), **Rule 13/14** (one
predicate, one source; no re-partition by hand), **Rule 41** (measured vs
inferred, kept apart).

## The defect this closes, in one line

`cmd/agent/cli_notify_duplicate_test.go` asserted that the session record carried
exactly ONE user row with the text `gopi notify --session <id> --message <text>`
had delivered — and read the record through `testutil.Session.UserContents()`,
the accessor that returns the OPERATOR's turns only. The delivered sentence is
recorded on a row the HARNESS injected, so the search was false by construction:
the assertion was red on a record that was exactly right.

## The verdict, and the evidence that decides it

**READ side, not write side.** The write path never lost anything. Measured this
run by restoring the parent's bytes in the fixture and dumping the raw JSONL
beside the reading:

```
cmd/agent/cli_notify_duplicate_test.go:159: PROBE views: operator=[]
    harness=["[system] An external process sent this session a message: NOTIFY-DUP-9f31: one delivery, one message" "[system] [t+2s] — first request of this session."]
    all=[...same two rows...]
cmd/agent/cli_notify_duplicate_test.go:161: PROBE record file
    /dev/shm/suite-cmd-agent-run-1425400635/TestNotifyDeliverStatesTheMessageOnceInTheSession3839797656/001/.gopi/sessions/24dcf5e7-…/session.jsonl
    (1879 bytes, 1 raw occurrences of the delivered text):
    {"message":{"content":"[system] An external process sent this session a message: NOTIFY-DUP-9f31: one delivery, one message","role":"user"}, …}
    …
cmd/agent/cli_notify_duplicate_test.go:170: the session record carries 0 user message(s) with the delivered text, want exactly 1 …
--- FAIL: TestNotifyDeliverStatesTheMessageOnceInTheSession (32.09s)
```

One run, one file: the record (line 3 of the JSONL) holds the row; the operator
view is empty; the operator-only *union* (`all`) holds it. `0 user message(s)` is
the fixture's reading, never the record's content.

Raw transcript: `~/.gopi/sessions/01a12ca8-244c-7ba5-98a3-663b79831b94/artifacts/red-and-raw-dump.out`.

## The chain that produced it, named

1. **`ea1357381`** (`fix(testutil): the session fixture reports the OPERATOR's
   turns, not harness rows`) made `UserContents()` operator-only — it now skips
   every row `sessionevent.IsHarnessInjected` accepts — and added
   `HarnessUserContents()` / `AllUserContents()` as the two complements. The
   change is correct: a chat writes a clock stamp on EVERY request, so a "what
   did the operator type?" read that returned stamps counted two turns as four.
2. **Every fixture that searched `UserContents()` for a HARNESS row went
   vacuously red with it** — the delivered sentence is one of those rows
   (`internal/agent/chat_job_notice_turn.go` → `sessionMessageFact`, which puts
   `sessionevent.HarnessMessagePrefix` on it).
3. **As this record was written, the fixture-side repair was not on master —
   and the claim below is a claim about THAT master, `a3a31da42`.** Re-measured
   on 2026-10-13 (`git merge-base --is-ancestor <c> a3a31da42` → exit 1 for each
   of `9867ed461`, `6c06db641`, `4cec86abb`, and for `ea1357381`, the partition):
   the writing run's window was real. Against **today's** master the same
   command returns **exit 0 for all three** — the repairs landed on master
   afterwards, so this record's branch-window claim is
   a historical one and the commits it names are on master now. The commits, as
   named here: `9867ed461`
   (`cli_notify_deliver_idle_test.go:200`, `cli_notify_deliver_test.go:329`),
   `6c06db641`, `4cec86abb` (`cli_notify_duplicate_test.go:159` — the row this
   task is about). At writing time
   `master:cmd/agent/cli_notify_duplicate_test.go` was byte-identical to the
   parent's `0b743cbac` (`sha256 72eaa34a…`); today's master carries the repair
   (`sha256 bc17019c…`, 163 lines — the same bytes as HEAD).

### The control: clean master is GREEN, and the reason is the whole story

Measured this run in a detached worktree at `master` (`a3a31da42`, its own
`bin/gopi`; transcript `artifacts/control-master-run.out`):

```
--- PASS: TestNotifyDeliverStatesTheMessageOnceInTheSession (32.08s)
ok  github.com/nick/gopi/cmd/agent  35.908s      TEST_RC=0
```

so the briefing's "master: one green observation" is CONFIRMED — the red is NOT
inherited from master. It reproduces where the briefing says, the parent
`0b743cbac`, and the two runs' own log lines show why:

* **branch (HEAD)**: `carries side file(s) [memory-prefetch.jsonl] …`
* **master `a3a31da42`**: `carries side file(s) [memory-prefetch.jsonl (unknown
  .jsonl)] …`

At that master's tree the fixture is the OLD shape twice over: its
`UserContents()` returns **every
user row** (`internal/testutil/session.go` at `a3a31da42` consults no harness predicate
and has neither `IsHarnessUser` nor the two complementary accessors — re-measured
2026-10-13: today's master `60cf0a4d5` **does** carry
`IsHarnessUser`/`HarnessUserContents`/`AllUserContents`), and this
fixture is still the parent's bytes. On a pre-partition tree the operator-only
read IS the raw census, so the delivered row is counted and the assertion is
green — for a reason that stops holding the moment the partition lands.

**The window is therefore entirely on the branch, and it is closed on it**:
`ea1357381` (the partition) is an ancestor of HEAD; the fixture half
(`9867ed461`, `4cec86abb`) is an ancestor of HEAD and, **at the time of this
record**, of no master (`a3a31da42`). A tree
that carries the partition WITHOUT the fixture half is the red one (measured:
`0b743cbac` — 4/4 in the reporting run, 1/1 in this one); HEAD carries both and is
green (2/2 here).

**Re-measured 2026-10-13 — the window has since closed on master too.** On
today's master the fixture half is present, not absent: `git merge-base
--is-ancestor` returns **exit 0** for each of `9867ed461`, `6c06db641` and
`4cec86abb` (`master` = `60cf0a4d5`; the same three also return exit 0 against
HEAD). So the red tree the paragraph above describes is now a *historical*
configuration, reconstructible only by checking out the pre-landing master — the
defect it documents is closed, not open. **Who landed it on master is not
established by this measurement**: what the walk does establish (measured) is
that the first first-parent-master commit containing each one is `a69601a22`
(`Merge branch 'task/01a12c4a…'`) for `9867ed461` and `b7e5eddd6` (`pre-merge:
verify branch against peer master before pushing`) for `6c06db641` and
`4cec86abb`, each a merge whose second parent is the task branch. Whether that
is also the merge that landed the work on the peer's master was NOT measured —
so no landing merge is named here (
marked not located, not asserted
here).

Original measurement, this run's ground, verbatim:
`artifacts/ancestry-measurement.out` (`artifacts/ancestry-measurement-2.out`
and `artifacts/ancestry-measurement-3.out` for the first-first-parent walk).
4. **No other live ref carried this gate** — measured at the time of this
   record. A sweep of every branch
   (`git for-each-ref refs/heads` → `git ls-tree -r <branch> -- internal/standards`
   for a `session_view*` path) returned no hit, so the gate below was written here
   rather than assumed to be in flight elsewhere (Rule 26's overlap check, run as
   a measurement instead of a guess). **Re-measured 2026-10-13: the gate is now
   on master** (`internal/standards/session_view_gate_test.go`, blob
   `fcdea50e…`, the same bytes HEAD carries) and on three task branches; the
   writing run's own master `a3a31da42` did NOT carry it (measured: absent). So
   the paragraph above is the record of the window, and the gate has since been
   shipped.
   Measurement: `artifacts/branch-sweep-heads-only.out`.

## The rule the class needs, stated once

`internal/testutil.Session`'s three accessors **partition the record's user rows
exactly once**, by the production predicate `sessionevent.IsHarnessInjected`:

| view | what it returns |
|---|---|
| `UserContents()` | the OPERATOR's turns (harness rows dropped) |
| `HarnessUserContents()` | the rows the HARNESS injected |
| `AllUserContents()` | every user row (the union) |

A caller that wants both halves **splits the union with the predicate** — that is
the only legitimate second filter, and `cmd/agent/cli_lineedit_test.go` does
exactly that. Filtering a FILTERED view again partitions nothing.

## The gate

`internal/standards/session_view_gate_test.go` — two statically decidable
classes over `internal/`, `cmd/`, `testsuite/` (1654 test files scanned this run):

* **Class 1 — a filtered view is not a raw record.** A `for … range` over
  `UserContents()`/`HarnessUserContents()` whose body applies a harness predicate
  again.
* **Class 2 — the delivered sentence is a harness row.** A `for … range` over the
  operator-only view while searching (`strings.Contains`) for the text this file
  hands to `notifyMessageFlag` — the flag whose argument IS what the product
  records on a harness row. The delivered-text constant is derived per file
  (package-level identifiers used as arguments beside `notifyMessageFlag`), so a
  renamed constant is tracked, not exempted.

Properties that keep it from being ceremony:

* the view inventory is **derived from `internal/testutil/session.go`**
  (`go/parser`), so a fourth accessor is classified by the same polarity rule
  instead of being invisible to the gate;
* **read floors**: < 100 test files scanned, or < 2 views classified, is a
  FAILURE — a scan that found nothing is not a scan that passed (Rule 15);
* a reviewed exception is the inline `session-view-refilter-ok:` marker on or
  above the loop;
* the red proof drives the two classes AND the "correct read is not refused"
  half (union split, harness view, escaped hit) — a gate that also refuses the
  right read would be unusable.

**Red proof on the REAL tree** (not only on probes): restoring the parent's
`UserContents()` bytes at `cli_notify_duplicate_test.go:159` makes the gate name
that file and line and fail; restoring the fix makes it green. Transcript:
`artifacts/gate-bites-real-tree.out`.

## Honest boundaries — what this gate does NOT decide

"Did this fixture choose the view that matches the row's PROVENANCE?" is not
statically decidable in general: provenance is a fact about the writer. So the
notify family keeps its behavioural guard — those fixtures go red the moment the
read path regresses — and this gate covers the shapes that otherwise stay
green-or-plausibly-red with no behavioural signal. Named, not implied.

## Files

* `internal/standards/session_view_gate_test.go` (new — the gate; zero new
  dependencies, `go/parser` only, the same shape its sibling gates use)
* `devdoc/session-record-view-refilter-gate.md` (this document)
* `devdoc/DOC-INDEX.tsv` (this document's row, same commit)
