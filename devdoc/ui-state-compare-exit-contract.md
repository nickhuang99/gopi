<!-- doc-class: historical -->
# The `ui_state --compare` Exit Contract, and Selecting a Surface by Its Session

**Task:** `01a0e052-2efa-7d32-b667-7fc2658ec441` · 2026-09-27 · worktree
`~/workspace/gopi-01a0e0522efa7d32b6677fc2658ec441`

`gopi ui_state --compare "<line>"` answers one question: *did my surface actually
paint this line, or did the transcript say it and the paint lose it?* It is the
instrument an agent points at its own chat window. It was unusable in exactly the
case it exists for. Four defects were measured on this host with the real binary;
all four are fixed. This document records the measured shapes, the reasoning that
chose each fix, and the alternatives that were rejected — a future agent reading
the code should not have to re-derive why it is shaped this way.

## 1. Defect 1 — a check that could not run exited with the DISAGREEMENT code

**Measured.** A dump whose own session record holds no assistant text (`gopi log
<id> --response --text` prints nothing) produced:

```
… transcript_lines=0 match=false
  no-transcript … reason="no transcript lines were available: the grid was examined,
  the authoritative side was not, so no comparison can be claimed"
EXIT=3
```

Exit 3 is the code that means **the two sides disagree**. A script (or an agent)
branching on it concludes the paint lost the line — from a check that never read
the authoritative side at all. The failure mode is the worst kind: a false
positive that looks like evidence.

**Root cause (5-why).** Why did it exit 3? Because the compare's exit was decided
by `if !rec.Match { return uiStateExitCompareMismatch }`, and `Match` was set
`false` by *any* non-`match` verdict — agreement and "unable to check" collapsed
into one boolean. Why was that boolean the decider? Because the exit contract was
maintained in two places (an `auditExitCode(status)` switch for the whole-answer
audit, the inline `if !rec.Match` for the one-line compare), so the second place
never had the three values the first one had. Why two places? Because the audit
was added later and reused the codes without sharing the decision.

**Fix.** `uiStateExitCode(outcome)` in `cmd/agent/cli_uistate_compare.go` is now
the ONE place the code is decided, and it takes the contract's own three-valued
`uiStateOutcome` (`agreed` / `disagreed` / `could-not-run`):

| outcome | code | meaning |
|---|---|---|
| `agreed` | 0 | the check ran, every line was on both sides |
| `disagreed` | 3 | the check ran, at least one line was on one side only |
| `could-not-run` | 1 | nothing was checked: no dump, unpainted grid, fallback geometry, no authority, or an answer with no lines |

Both checks reach it through a twin report function (`uiStateReportCompare` /
`uiStateReportAudit`) that derives the outcome **from the record's own verdicts**
and then derives both the printed word *and* the exit code from that one value —
so a report can no longer say `agreed` beside a non-zero exit.

**Why `could-not-run` is the DEFAULT for an unrecognised verdict**, not
`disagreed`: a status nobody classified must never claim the check passed *or*
that the paint lost something. The honest reading of an unclassified verdict is
"we did not establish anything".

**Rejected alternative: reuse exit 3 and document it.** Cheaper, and wrong — the
whole point of the defect report was that reading 3 as "disagree" is the natural
reading, and a documented exception does not undo an inference a caller has
already drawn.

**Kept deliberately: `match` in the JSON envelope.** It is the older, two-valued
summary and it is now derived from the *same* outcome, so the two fields cannot
disagree. Callers get `outcome` as the field to branch on; `match=false` remains
true for both "disagreed" and "could not run", which is why it must not be the
decider.

**The read-failure path was folded into the same predicate.** When the authority
cannot be read, every line now carries a `no-transcript` verdict carrying the read
failure as its reason, instead of the record holding zero verdicts. That was not
cosmetic: with an empty verdict list the outcome came out `agreed` (the loop found
no disagreement), and the test written for defect 1 caught exactly that — an exit
0 beside `outcome=agreed` for a check that never ran. The lesson generalises: an
outcome derived by *folding over a list* must be given a non-empty list for every
"it did not run" case, or emptiness silently means success.

## 2. Defect 2 — the transcript derivation knew two layouts, not the tree's

**Measured.** The rule was a list of two hard-coded names:

```go
for _, candidate := range []string{
    filepath.Join(dir,   sessionlayout.RecordName),   // <sessionDir>/session.jsonl
    filepath.Join(filepath.Dir(dir), sessionlayout.RecordName), // one level up
}
```

Both are ABSENT in the layout the task machinery itself writes — a dump at
`<sessions>/<task-id>/run1/chat-screen.dump` with the run's record at
`<sessions>/<task-id>/<run-uuid>.jsonl`. Measuring that dump with the new binary:

```
session=/home/user/.gopi/sessions/01a0dd26-…/82e9613e-…-ebbed154735a.jsonl
transcript_lines=64 outcome=disagreed        # the old rule derived nothing here
```

**Fix.** The question is not "which of these two filenames exists" but **"which
session directory owns this dump"**, and that is a question about the *tree*, so
it is answered by walking up from the dump's directory and asking
`internal/sessionlayout` — the package that already owns "what is a session record
in this tree" — at each level (`uiclient.SessionDirOfDump`). The nearest level
holding a record wins: the session closest to the dump is the one that painted it.

Two bounds, both deliberate:

- **`sessionDirWalkLevelsMax = 4`** — a dump sits at the session dir, in ONE
  artifact subdirectory, or occasionally one deeper (`evidence/frames/`). A
  bound exists so this stays a *lookup*: it is not a search of the filesystem.
- **The walk stops at the sessions root.** The question is "which session owns
  this file", never "find me a session somewhere" — a record above the tree could
  belong to an unrelated profile. The boundary is resolved through
  `config.GopiDir()` + `config.SessionsDirName` (Rule 13: never a re-typed
  `"sessions"`).

**Rejected alternative: add `<run-uuid>.jsonl` as a third glob.** It fixes today's
layout and re-breaks at the fourth. The tree already has a classifier; a second,
narrower one here is the defect, not the fix.

## 3. Defect 3 — the explicit `--session` failure blamed the dump

**Measured:**

```
$ gopi ui_state --compare x --session no-such-session-id --pid 4165135
gopi ui_state: cannot read the transcript : open : no such file or directory
```

Three separate faults in one line, all of them about the caller's OWN argument:

1. **The operand is empty** (`transcript :`) — `gopilog.FindSession` answers "no
   such session" with `("", nil)`, and the message was formatted with that `""`.
2. **No cause** (Rule 15) — the nil error was carried through as an error and
   rendered, so a failure reported no reason. (The briefing's variant is the same
   bug seen through the other path: `…(derived from the dump's own session
   directory): <nil>`, where the word `<nil>` IS the whole explanation.)
3. **A false provenance sentence** — "(derived from the dump's own session
   directory)" was printed even when the caller had typed the operand, sending
   the reader to inspect the dump instead of the id they mistyped.

**Fix** (`uiStateTranscriptLines`, `uiStateCompareSessionUnresolved`,
`uiStateNoSessionForID`):

- An **explicit** operand is reported with `cannot read the transcript %q: %s`,
  quoting exactly what the caller typed, and a lookup miss becomes the sentence
  *"no session matches that id (a session id is the full uuid; `gopi log` lists
  the sessions this profile has)"* instead of a nil.
- The **derived** provenance sentence is printed only for a path the caller never
  typed (`uiStateCompareReportSessionError` keeps the two cases apart).

Measured after:

```
gopi ui_state: cannot read the transcript "no-such-session-id": no session matches
  that id (a session id is the full uuid; `gopi log` lists the sessions this
  profile has)
EXIT=1
```

## 4. Defect 4 — a session the caller named did not select a surface

**Measured.** With twelve exited chat dumps on hand, the compare refused:

```
8 exited gopi surfaces could be the target; refusing to guess — pass --pid (or --list)
```

The refusal is CORRECT (this host's report of a *unique* candidate was a miscount
— there were twelve), but the caller had already stated which session it meant,
and there was no flag through which that statement could act. `--pid` was the only
disambiguator, which is exactly the knowledge an agent auditing its own surface
does not have.

**Fix.** `--session` (with a check to run) now selects the candidate dump whose
**session directory** is the named session's (`uiclient.SelectBySession`), because
a surface writes its dump into the session directory of the run that produced it —
so "this dump belongs to the session I named" is a fact about the artifact, not a
guess. Measured:

```
$ gopi ui_state --compare x --session e22f1375-…-7e4b7755f0f9 --target chat
… dump=/home/user/.gopi/sessions/e22f1375-…/chat-screen.dump session_file=…/session.jsonl EXIT=1
$ gopi ui_state --compare x --session e22f1375-…            # by ID — resolved through gopilog
… same selection …                                          EXIT=1
```

**Order matters and is documented in `uiStateSelect`:** `--pid` wins outright (it
is the unambiguous route and a session must not redirect it); otherwise the named
session selects; otherwise `SelectLive`'s rule. The session rule applies only when
a check is being run — for `--list` the flag is still just a row filter.

**The subtle case, and why it does not fall back silently.** `--session` has TWO
jobs in this verb: it *disambiguates* (which of these dumps did you mean) and it
*overrides the authority* (check this dump against a different transcript — the
older, documented job). A name that identifies no candidate must therefore not
veto the check, or the override would stop working. So:

- **one candidate on hand** → selected; the caller is overriding, not choosing;
- **several** → a refusal that names **the sessions the dumps on hand actually
  belong to** (`the dumps on hand belong to: …`), which is the fact a caller who
  mistyped, or whose dump is elsewhere, needs to see. Not a bare surface list.

**Known limitation, stated rather than papered over (measured on this host).** A
session directory can own MANY dumps — `01a0dd26-…` owns eight (a bare-session run
with `run1/`, `runB/`, `frames/`, `runFinal/`, plus a session-level copy). Naming
that session yields eight hits, so `SelectBySession` correctly reports a genuine
ambiguity with its list rather than picking one. `--pid` is the door for that
session, and it is reachable (`--list` names all eight). Narrowing *within* a
session — by artifact subdirectory, or by "the newest dump in this session" —
is a follow-up decision, not something to guess here: which of eight frames the
caller means is exactly the knowledge the caller has and the artifact does not.

## 5. What this changed, file by file

| file | change |
|---|---|
| `cmd/agent/cli_uistate_compare.go` | the three-valued outcome + `uiStateExitCode` (ONE decision); `uiStateReportCompare` / `uiStateReportAudit` derive word and code from the same value; the read-failure path emits `no-transcript` verdicts instead of an empty list; the sibling derivation delegates to `SessionDirOfDump`; explicit vs derived session failures are reported differently; the header prints `outcome=` |
| `cmd/agent/cli_uistate.go` | `uiStateSelect` consults `--session` (pid first, then the named session, then the live rule); `uiStateNamedSession` resolves an id through `gopilog` so selection and read agree |
| `internal/uiclient/dumpread.go` | `SessionDirOfDump` (the walk), `gopiSessionsRoot`, `samePath`, `SelectBySession`, `candidateSessionList` |
| `cmd/agent/cli_uistate_compare_exit_contract_test.go` | the six cases, all through the verb's own entry point |
| `internal/uiclient/sessiondir_test.go` | the four layouts + orphan/boundary + the selection rule |

`internal/uiclient/audit.go`'s comment now points at `uiStateExitCode` (the single
decision) and `uiStateAuditExitCouldNotRun` folded into `uiStateExitCouldNotRun` —
two constants with the same value were how a caller ended up learning two
vocabularies for one contract.

## 6. Out of scope, stated (Rule 26's corollary)

- **Arming the chat dump by DEFAULT.** The dump is written only with
  `--dump-screen`/`GOPI_CHAT_DUMP`, so the surface an audit most wants to read is
  the one that is off unless someone thought ahead. Turning observation on by
  default is a product/behaviour decision with a cost on every run — the
  operator's call, not a side effect of a bug fix. Filed as its own task.
- **The `**` emphasis suspicion** (requirement B of the report this task came
  from) is unverified and needs a real pty reproduction in its own session: a
  render question cannot be settled from a compare of text that was never painted.

## 7. Reproducing the measurements

```bash
G=$(mktemp -d)/gopi
CGO_ENABLED=0 go build -o "$G" ./cmd/agent/

# defect 1: a readable dump whose session holds no assistant text → 1, not 3
$G ui_state --compare "x" --session ~/.gopi/sessions/e22f1375-1144-4fb9-9d99-7e4b7755f0f9/session.jsonl

# defect 2: the task-run layout (run-uuid record, dump one dir down) → derives
$G ui_state --compare "x" --pid 3787828 --target chat

# defect 3: an operand the caller typed is quoted, with a reason and no <nil>
$G ui_state --compare "x" --session no-such-session-id --pid 4165135 --target chat

# defect 4: naming the session selects its dump instead of refusing
$G ui_state --compare "x" --session e22f1375-1144-4fb9-9d99-7e4b7755f0f9 --target chat
```
