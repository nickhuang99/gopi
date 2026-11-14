<!-- doc-class: historical -->
# Silent Degradation — Four Places Where the System Stayed Quiet

**Epic:** 01a12af7098a7193bdbd991afca3f9c7
**Branch:** `task/01a12af7098a7193bdbd991afca3f9c7`
**Date:** 2026-10-11

## What this epic is, in one sentence

Four places where a gopi run degrades — refuses, skips, or falls back — and
**says nothing where the operator is looking**. The task was to add the SIGNAL,
never to move a guard: every refusal, every ancestry check, every dirty-worktree
gate stays exactly as it was. What changed is what an operator is TOLD.

The unifying shape, which turned out to be real rather than rhetorical (see
the last section): **the fact was already known to the failing arm, and it was
lost between that arm and the surface the operator reads.**

---

## Case ① — A close-out refused whole, and the ledger still read `completed`

### Root cause

Not "the chain forgot to report". The chain reports **twice**: it writes the row
trail event `finish-cleanup-removal-refused` (`task_finish_cleanup.go:950`) and
it prints to stderr. Both are durable and correct.

The failure is a **delivery** failure with two halves:

1. **stderr belongs to a run that has ended.** An operator arriving hours later
   cannot read it — the process is gone.
2. **The trail was readable only one row at a time.** `task show <id>` already
   rendered the event as the row's verdict line (`taskEventLatestVerdict`,
   `tasks.go:510`). With 41 stranded rows, learning the state of the backlog
   cost 41 invocations — the arithmetic that let nine hours pass.

So the signal existed, was correct, and was **unreachable at the volume it
mattered at**. This is Rule 15's shape at the human seam: not a swallowed error,
but an error that reached nobody.

### Fix

A reader plus one rendered marker — **no gate touched**:

- `internal/tools/task_list_closeout_signal.go` (new): ONE query, issued BEFORE
  the row cursor opens (the ledger pins a single connection,
  `SetMaxOpenConns(1)` — a per-row query under an open cursor is a deadlock, the
  same constraint `failedReissueNote` obeys). It asks which event is each row's
  **LAST** (`MAX(id) GROUP BY task_id`), not whether the name appears anywhere.
- `internal/tools/tasks.go`: one header count line + a per-row
  `[closeout-refused]: <reason>` tail.
- An unmigrated ledger (no `task_events` table) answers "nothing to say", never an
  error: a list that failed on a missing optional table would trade a silent
  marker for a silent list.

**Why "newest, not ever" is the whole correctness of the query.** Measured on
this host's ledger, by a read-only generator archived with this epic's evidence
(`artifacts/census_refusals.go` in the session directory — the path is quoted in
the report; it opens the ledger `mode=ro` and prints one number per dimension):

```
$ cd <repo> && go run ~/.gopi/sessions/<id>/artifacts/census_refusals.go
event_rows=315            # task_events rows whose event is the refusal
distinct_task_ids_ever=32 # task ids that EVER recorded it
newest_is_refusal=32      # task ids whose NEWEST event is the refusal
```

The first reading of this figure, taken while the epic ran, was
`296 / 29 / 29`; it had grown to `315 / 32 / 32` by the end of the same session,
because every further refused close-out on this host adds a row. **That drift is
the argument for citing a generator rather than a number** (Rule 38): a census
figure a reader acts on must be re-derivable, and this one moves every time the
delivery chain refuses.

On **this** host the last two counts coincide (every task that ever recorded a
refusal still ends there), so the host's own ledger does not by itself
distinguish the two queries. The distinction is still load-bearing, because it is
what keeps the marker a **state rather than a scar**: the test pins a row refused
and then delivered on a later pass, and requires the marker to be **gone**. A
marker keyed on the bare event name would accuse every row that was ever refused,
including ones the chain later collected. (Rule 9: a claim about the code is not
evidence that the host's data has the shape you assumed — the earlier draft of
this document asserted 276/28 from memory and the measurement refuted it.)

### Red proof

`internal/tools/task_list_closeout_signal_test.go` — 5 tests. The load-bearing
one fails on the pre-change tree (measured: `the list does not mark a row whose
close-out was refused`; `tasks.go` reverted, new file kept). Two more pin the
poles the naive implementation gets wrong: **no marker** on a row refused then
delivered, and a **byte-identical** render (no stray blank line) when nothing is
stranded.

### Real-run observation

```
$ /tmp/gopi-before task list --status completed | grep -c closeout-refused
0
$ /tmp/gopi-after task list --status completed
843 task(s) with status=completed:
close-out refused on 29 row(s) — the delivery chain stopped before the push, so
the branch was NOT removed; each affected row is marked [closeout-refused] with
its reason:
#01a12b06-…  [completed]  …  [via worker] [closeout-refused]: removal skipped
(delivery already attempted): the recorded w…
$ /tmp/gopi-after task list --status completed | grep -c closeout-refused
29   (1 header + 28 rows)
```

`gopi task show 01a120a7-…` renders the same fact as before — that surface was
already honest and was not touched.

---

## Case ② — `chat --replay` refused, and named a MENU instead of a cause

### Root cause

The replay driver installs its playback controls **at the editor's key seam**
(`r.control.install(input.editor)`), so with no editor-backed layout there is no
key seam, `announceStart()` is never reached, and the key hint never appears.
That part is correct by design and was left alone.

The defect was the **sentence**. It read:

> this run has no interactive editor (piped input, `--no-editor`, or a terminal
> too small for the chat layout)

A menu of three, offered where the code had exactly **one** measured fact in
hand — the input layer's mode string, which cannot distinguish a pipe from a flag
from a short terminal. And the menu was **incomplete**: a terminal that opened but
could not be put into raw mode was a fourth cause and was in no list at all.

### Fix

The cause is now **decided where it is known and carried to where it is said**:

- `lineedit_wire.go`: `chatInput.noLayoutReason`, set in
  `newChatInputWithPalette` — the single place that decides — with four named
  constants (`chatNoEditorReasonNoEditor` / `…PipedStdin` / `…TooSmall` /
  `…TerminalRefused`), plus two totality constants for the nil layer and the
  unrecorded layer.
- `chatInputNoLayoutReason` is its ONE reader (nil-safe: the arm that reports a
  degradation must not be the thing that panics — Rule 19).
- `chat_replay.go`: the refusal takes the reason and **names what is missing** —
  that the controls are not installed and their hint will never appear.

### The bug the real run caught and the unit test could not

The first build reported `reason: the input layer records no cause` for
`--no-editor` — a correct sentence about a wrong fact. **Two ordering defects,
both only visible when executed:**

1. **The cooked return path never carried the value.** `noLayout` was computed
   and then dropped on the floor at the final `return &chatInput{…}`.
2. **The region-refusal arm wrote to the local AFTER the struct was built.**
   `ci := &chatInput{…}` captures `noLayoutReason` **by value**; assigning to
   `noLayout` afterwards left `ci` claiming a layout it did not have.

Fixed by taking the layout's verdict *before* the layer is built. This is exactly
Rule 31's case: the unit tests were green while the surface was wrong, because a
test calls the constructor the way its author imagined.

### Red proof

`cmd/agent/chat_replay_no_layout_reason_test.go` — 4 tests, the load-bearing one
being that the four reasons render **distinct** strings (a table collapsed to one
string would satisfy "the reason reaches the sentence" while telling the operator
nothing), plus totality for the nil and unrecorded layers.

### Real-run observation

```
$ /tmp/gopi-before chat --replay 5af45c3e-… --no-editor | grep "no interactive editor"
(nothing — the refusal read "no reserved layout is installed"; the cause was a menu)
$ /tmp/gopi-after chat --replay 5af45c3e-… --no-editor
[gopi replay] this run has no interactive editor, so the playback controls
(pause/step/help/quit) are NOT installed and their key hint will never appear —
reason: --no-editor was passed; a replay films the chat LAYOUT, so it is refused
here rather than producing a transcript that looks like one
$ printf '' | /tmp/gopi-after chat --replay 5af45c3e-… 
  … reason: stdin is not a terminal (piped or redirected input); …
```

Two of the four causes exercised for real; the third and fourth (`too small`,
`terminal refused`) need a terminal gopi does not have here — see **Not Done**.

---

## Case ③ — The pushback lane's remedy pointed at the wrong master

### Status: ALREADY LANDED — named and skipped, not redone.

Ticket **01a12a54-6ff3-73d1-91ed-39b5890f4dd6** ("pushback 车道 ff 闸越权…且补救
建议指向错误的 master ref 形成死循环") is `completed` on the ledger. The epic's
instruction was: audit it, name it, and skip if it is covered.

**Audit, done on the code rather than on the report** (Rule 9: 命中≠实现):

- `devdoc/pushback-alignment-fact-not-gate.md` is present in this tree and is that
  task's own deliverable.
- The gate this case complained about is **gone**: the sentence
  "not fast-forwardable onto master" no longer exists in
  `internal/tools/task_sync_git_pushback.go` (`grep -c` → 0), and the remaining
  mentions are comments that *record* the old defect and its remedy
  (`task_sync_git_pushback.go:441-444`: "fast-forwardable, whose remedy ('merge
  master into it') cannot be performed").
- The fix runs on the real surface: `go test ./internal/tools/ -run TestPushback`
  → **ok**, 5.3s.

**Conclusion: covered. Nothing to change here, and this epic did not re-touch it.**

---

## Case ④ — A branch deleted while a scratch ref was its only company

### Root cause

The close-out's orphan guard asks the right question — *would this tip survive in
some other ref?* — and it enumerated **every** ref
(`branchDeleteContainingRefs`). One family answers YES without keeping anything:

```
refs/remotes/gitPushcheck/master   the pushback lane's per-pass mirror,
                                   written by a `+` refspec fetch and DELETED by
                                   the same pass's own defer
                                   (gitPushbackPruneMirrors)
```

**Measured on this host:**

```
$ git for-each-ref --format='%(refname)' refs/remotes/gitPushcheck/
refs/remotes/gitPushcheck/master      (count: 1)
```

So a tip held only by that ref reads as "reachable from another ref" — true in
the same sense that a file in a directory about to be emptied is "on disk".

### Fix

A **reader at the report seam**, deliberately separated from the guard:

- `branchDeleteScratchOnlyHolders` / `branchDeleteDurableHolders` /
  `branchDeleteNonDurableRef` in `branch_delete_guard.go`. The non-durable
  namespace is named through `gitPushbackCheckRootRef` — the constant that owns the
  spelling (Rule 14) — never a second literal.
- The close-out's branch step captures the tip **before** the delete
  (`branchDeleteTipOf`) — the one fact a recovery command needs, and
  unrecoverable a moment later — and appends
  `finishCleanupBranchScratchOnlyNote` to the SUCCESS line.

**The guard's decision is untouched**, and that is asserted, not asserted-about:
`TestBranchDeleteGuardDecisionIsUnchangedByTheScratchArm` drives the real guard
and requires that a mirror-held tip is still NOT refused (a refusal built on
"are the holders durable?" would be the guard changing, which this epic does not
do). The recovery advice is `git branch <name>-recovered <mirror>`: `git branch`
reuses the objects already in the store — one ref write, no copy, no new artifact
(Rule 35: the fix is a READ plus a sentence, not a mechanism).

### Red proof

`internal/tools/branch_delete_scratch_mirror_test.go` — 4 tests: the mirror named
when it is the only holder; silence when a durable holder exists; silence when
NOTHING holds the tip (that case is the orphan guard's, and doubling it would make
this arm's sentence false); the note carries an actually-typable command.

### Real-run observation

`artifacts/case4-repro.sh` (archived with this epic's evidence in the session
directory) — a repo whose branch tip has `refs/remotes/gitPushcheck/master`
as its only other container:

```
refs containing the tip:
  refs/heads/feat/held-by-scratch
  refs/remotes/gitPushcheck/master
OLD report: every holder counted -> 'reachable from another ref' -> silence.
NEW report: its tip is held only by a scratch mirror (refs/remotes/gitPushcheck/master)
            — the pushback lane DELETES it when the pass ends. Keep it now if you may
            still want it: git branch feat/held-by-scratch-recovered refs/remotes/gitPushcheck/master
after the lane prunes its own mirror, refs containing the tip: (none)
=> that command is the difference between a branch and a lost commit.
```

---

## Can the four be unified into one mechanism?

**No — and the reason is structural, not a matter of effort.**

Three of the four are the same *shape* (a known fact lost between the deciding arm
and the surface a human reads), but they are lost at three **different seams**, and
each seam needs a different instrument:

| case | where the fact dies | what revives it |
|---|---|---|
| ① | stderr of a process that has ended | a **reader** over the persisted trail, at the surface the operator already opens (`task list`) |
| ② | between the deciding branch and the refusal string (never captured at all) | a **field carried on the layer**, from the one place that decides to the one place that speaks |
| ④ | inside a report-shaping function, then the subject is deleted | a **reader** over git refs, taken *before* the referent disappears |

What unifies them is not a mechanism but a **test**: in each case the question to
ask is *"which arm knows this, and what is the LAST thing it hands to anyone?"* —
and the answer in all four was either "nothing" or "a menu of possibilities".

A single shared mechanism would have to be a **degradation notice channel** — and
that is exactly what the repo already has, twice: the ledger's per-row event trail
(which ① and ④ both read, and which needed no new event, no new column, no new
config key) and `chatNoticef`/the diagnostic stream for the live run (which ②
uses). So the honest answer is the narrowing one: **the mechanism already existed
in both halves; what was missing was a reader that reached a human, and a cause
that was carried rather than re-guessed.** Adding a third channel would have been
a new dependency (Rule 35) solving a problem two existing channels already solved.

Case ③ is not part of the family: it was a **wrong remedy**, and the remedy was
replaced at its source by a different task. Nothing here should be read as having
re-audited it.

---

## Files changed

| file | change |
|---|---|
| `internal/tools/task_list_closeout_signal.go` | **new** — the ledger view's reader + marker + header sentence |
| `internal/tools/task_list_closeout_signal_test.go` | **new** — case ① red proof |
| `internal/tools/tasks.go` | read the signals before the cursor; render the header count and the per-row tail |
| `internal/tools/branch_delete_guard.go` | scratch-mirror reader, the note, the recovery hint, `branchDeleteTipOf` |
| `internal/tools/branch_delete_scratch_mirror_test.go` | **new** — case ④ red proof |
| `internal/tools/task_finish_cleanup.go` | capture the tip before the delete; append the note to the success line |
| `cmd/agent/lineedit_wire.go` | `noLayoutReason` + the four cause constants + `chatInputNoLayoutReason` |
| `cmd/agent/chat_replay.go` | the refusal takes the measured cause and names the missing controls |
| `cmd/agent/chat_replay_no_layout_reason_test.go` | **new** — case ② red proof |

**No config key was added, no guard was loosened, and no new event, column or file
on disk.** Every string literal that carries meaning is a named constant
referenced from its single definition (Rules 1b/13/14).

## Not Done (honest)

- **Two of case ②'s four causes were not exercised on a real terminal**: "the
  terminal is too small" and "the terminal could not be opened for raw mode" need
  a controlling terminal and a specific geometry, which this run does not have
  (it is itself running under a captured session). They are covered by unit tests
  on the constants and by code reading; the two causes that CAN be produced here
  (`--no-editor`, piped stdin) were both run for real, and it was that run which
  caught the two ordering bugs the unit tests missed.
- **Case ④ has no end-to-end run through `gopi task finish-cleanup`**: producing
  it for real needs a ledger row whose branch has a mirror-only holder, which means
  driving the pushback lane against a peer. The reader, the note and the guard's
  unchanged decision are exercised by tests against a real git repository (the
  fixture builds the exact ref shape the lane leaves), and the repro script above
  shows the ref geometry and both report strings.
- **`internal/tools` carries pre-existing failures unrelated to this change.**
  Measured on a **detached worktree at the parent commit** — `git worktree add
  --detach <path> d89a2ff73^` (= `4f7484122`), a tree that contains none of this
  epic's commits, so nothing here is being attributed to an edited copy:

  ```
  $ cd <base worktree at 4f7484122> && go test ./internal/tools/ -count=1 -run 'TestMergeBuildGate_'
  --- FAIL: TestMergeBuildGate_MasterContainsBranchAbstainsOnAncestry (0.03s)
  --- FAIL: TestMergeBuildGate_AncestryDirectionKeepsTheRefusal (0.03s)
  --- FAIL: TestMergeBuildGate_RefusesTheIncidentShape (0.02s)
  --- FAIL: TestMergeBuildGate_GoDefaultRefusesBrokenModule (0.02s)
  ```

  Four, not one — the earlier draft of this section named only the test whose
  verbatim message had been read, and an unattributed failure the report stays
  silent about is the same defect one row over. The mechanism is a single
  environmental one: the merger's BUILD gate **ABSTAINS** whenever the shared Go
  build cache resolves onto RAM-backed storage (`/dev/shm`), and an abstention
  leaves `Refused=false`, which is exactly what each of the four asserts against
  (the run's own log prints the 2026-09-15 incident and the fix). It reproduces
  with `-run` narrowed to these four tests alone, so it is not contention.

  The other environment reds are `rg` and `ctags` missing from `$PATH` on this
  host, which turns every `code_search` / `keyword_search` test red locally. None
  of these are in scope here (this epic is signal-only) and none were "fixed" to
  make a suite green.
