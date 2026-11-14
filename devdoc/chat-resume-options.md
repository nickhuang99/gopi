<!-- doc-class: historical -->
# Chat Resume Restores the Options, Not Just the Conversation

**Task**: `01a0f536287475a2aaf4cc9e95a83178` · **Branch**: `task/01a0f536287475a2aaf4cc9e95a83178`

## The complaint, and what it actually was

The operator asked for `gopi chat --resume TASKID`, and asked whether gopi could
recover the original options *itself* rather than making them be replayed. The
prior run (task `01a0eb0e…`, commit `08a2b683`, `devdoc/chat-crash-resume.md`)
had already made the positional seed resume a crashed chat, and closed with an
honest `## Not Done` naming exactly this gap:

> argv is NOT recorded so flags must be replayed by hand.

So the request was not for a feature that never existed — it was the other half
of one that had shipped. This work implements that half.

## Why the conversation alone is not enough (measured)

A checkpoint restores a session's `messages` **and** the `tools` array it was
sent with — but a run started without `--tools` *has* no tools, and the two facts
collide loudly. Resumed with the seed alone, a SIGKILLed `gopi chat --tools`
session inherited a history full of `tool_use`/`tool_result` turns with nothing
behind them: the model kept reasoning in tool terms and replied with a
*description* of the tool call it could not make instead of the tool's result.
`devdoc/chat-crash-resume.md` recorded that. The previous fix made the mismatch
**loud** ("re-run with `--tools`") — which still leaves the operator
reconstructing a command line from memory.

**Capability is not derivable.** The one party that ever knew the command line is
the run itself, at start-up. So it is recorded.

## What was built

Three pieces, each with exactly one job:

| Piece | Where | Job |
|---|---|---|
| The record | `internal/agent.SessionLogger.LogResumeOptions` | Write this run's options into its own session JSONL at start-up, then **Sync** — the run this exists for is the one that dies mid-turn |
| The event | `sessionevent.EventTypeResumeOptions` | The wire shape, tolerant like `signal_verdict`/`task_row_closed`: a plain JSON object with a `type`, no `message` key, so every reader that switches on event type without a default skips it structurally |
| The reader | `gopilog.ResumeOptionsFromFile` | Read it back; a torn or foreign line is **skipped, never fatal** |

On top of that: `gopi chat --resume SESSION`, and autodetect on the existing
positional seed.

### The three settled designs

**1. The record lives in the existing session JSONL — no new file.**
Rule 35's question ("what does this add to the machine?") is answered by adding
nothing: the session file already exists, already sits beside the session id the
resume addresses, and is already written by the run at start-up. A sidecar file
would be a second thing to find, own, and clean up.

**2. The wire keys live in a LEAF package, not in either half.**
This one was forced by an import cycle and is the reason it is documented:
`gopilog/formatter.go` imports `internal/agent`, so `agent` cannot import
`gopilog` — the writer and the reader **cannot share a definition** if that
definition lives in either one. `internal/sessionevent` is importable by both, so
`sessionevent.ResumeOptionsFieldFlags` / `…SessionID` / `…Workdir` are declared
there once and referenced from both sides. A key typed twice is a key that
drifts.

**3. `--resume` and the positional seed are ONE path, reached two ways.**
Both name a session and both resolve through the same resolver; the target
collection above the parser merges them, so everything below has one case to
handle instead of two that can disagree. Two *different* targets is a usage
error — nothing silently wins. They differ in exactly one way, and it is the
documented contract:

- **`--resume` (the switch)** — the operator *asked* for the recorded options, so
  they are applied unconditionally, and a session that recorded none is told
  about.
- **the seed (positional)** — the operator asked to continue a conversation.
  Options are restored only when the command line stated **none of its own**
  (total silence is the condition). An operator who spells a flag is being
  deliberate and is left alone.

**Precedence**: the command line and the record are **combined**, never
substituted. There is no negative spelling to conflict with, except the one
option with two candidate *values* — `--dump-screen`'s destination — and there
the explicit command line wins, with the recorded one named as superseded rather
than silently ignored.

### The chain (why a resumed run re-records what it inherited)

A resumed run mints its **own** session (fresh uuid), so the record a *later*
resume reads is the one written by *this* run. Recording only the explicitly
typed words would break resume at the second crash: `--tools` → killed → resume
(restored, but nothing recorded) → killed again → resume restores nothing, and
the capability is silently gone one hop later. So a run records **what it had**
(explicit ∪ inherited, de-duplicated).

The **seed is never recorded**, in either spelling: it names *which* session, and
the record already lives inside that session's own file. A session id in the
flags would make a later resume re-seed itself; a raw-log path would pin a
filesystem fact of one run onto another.

## The defect the live run caught (no unit test did)

The first end-to-end sandbox run printed two consecutive lines that contradicted
each other:

```
[gopi] chat: ⚠ the seeded session was TOOL-ENABLED (18 tool definition(s) …) but this run has tools OFF …
[gopi] chat: restored 2 option(s) this session was started with: --tools, --no-editor
```

Both lines were individually correct; **the order was wrong**. The capability
warning was decided at the seed — *before* the recorded options were applied — so
it reported a loss that the very next line repaired, and it sent the operator off
to replay a flag already restored. The fix moves the warning after the apply; the
remedy sentence now appears only when the loss is real. Pinned by
`TestChatResumeDoesNotWarnAboutCapabilityItRestored`, which fails if the warning
moves back — or if the restore stops working, since the warning's *absence* is
what proves the restore took effect before the decision.

## Verification (the run itself)

A real binary, a real sandbox profile, a real SIGKILL, a real resume — driven
against the live API, not a mock. Evidence under
`~/.gopi/sessions/01a0f536-2874-75a2-aaf4-cc9e95a83178/`.

**The crash was genuine.** The FIRST harness attempt was not, and the way it
failed is worth recording: it wrapped the binary in `timeout 300`, so `$!` was
`timeout`'s pid and the SIGKILL never reached `chat` — the "crashed" run answered
normally and wrote its own `session_end`. A kill target is the process you mean,
not its wrapper. The recorded harness (`crash-resume-phase1.sh`) kills the chat
pid directly, and triggers on the tool's OWN process with `pgrep -x sleep` — an
identity, never `pgrep -f "sleep 90"`, whose pattern this script's own text would
self-match (Rule 24).

```
PHASE1 killed=1 pid=3948322 session=aa5fd9ae-0296-4d73-8ce4-891136e60420
PHASE1 tool_result=0 session_end=0 resume_options=1
```

`tool_result=0` **and** `session_end=0` is the crash this feature exists for: the
`bash` call was in flight when the SIGKILL landed.

**The resume, with the id alone and no `--tools`** (`crash-phase2.log`):

```
[gopi] chat: seeded 1 history message(s) from …/aa5fd9ae-….raw.log
[gopi] chat: restored 2 option(s) this session was started with: --tools, --no-editor
gopi chat — Ctrl+C interrupts the running turn, Ctrl+D or /quit to exit.  (tools ON)
```

and the tool **actually ran**, which is the claim that matters. The resumed
session's own JSONL line 7 is the tool's result — the marker is inside it:

```json
{"content":"[t+93s] [tool_use id: call_00_5YvraEec3IJWttdzYbA16879]\nexit=0\nRESUME-CRASH-MARKER-2\n", …}
```

and the resumed run's wire carried the definitions:

```
tools on the resumed run's WIRE: 18      first tool: read
```

**No contradictory warning** — `grep -c TOOL-ENABLED crash-phase2.log` = 0, i.e.
the ordering defect (fixed above) stays fixed under a real crash-resume pair.

**The second pair — `--dump-screen` restored ARMED** (`dump-resume-phase{1,2}.sh`).
The first pair proved `--tools`; the other recorded flag is only useful if the sink
it restores is actually armed, so it was driven too:

```
PHASE1 killed=1 session=f3a5c502-…  tool_result=0 session_end=0 resume_options=1
PHASE1 recorded={'"flags":["--tools","--no-editor","--dump-screen"]'}  dump-on-disk=1
PHASE2 restored 3 option(s) this session was started with: --tools, --no-editor, --dump-screen
PHASE2 NOT-armed-lines=0   no-observation=0
```

and the resumed run's own tool result read
`chat_surface action=see armed=true found=true path=…/chat-screen.dump` — the
sentence an unarmed run prints is `this run is NOT armed, so see/compare report
"no observation"`, and it appears zero times. One honest limit, a property of the
HARNESS: a resume driven from piped stdin (or through `script`) installs no
reserved layout, so `see` qualifies its grid as an uninterpreted 80x24 blank
(`frames=0`) — reaching a painted grid needs a real terminal. The ARMING, which is
what the restore decides, is what the pair above proves.

## The final full suite (the run's own numbers, and the six FAIL lines classified)

`make test-budget PKGS='./cmd/agent ./internal/agent ./internal/gopilog ./internal/sessionevent ./internal/sessionlayout ./internal/standards'`
→ `6 finished, 2 with a failure`, 559s wall, load `6.21 6.12 5.22 → 6.58 8.65 7.44`
on 4 cores. **The machine was carrying three concurrent `test-budget` chains**
(`ps -eo pid,etime,args | grep '[g]o test'` → three `-count=1 -timeout 25m -json`
runs, one of them a neighbouring `./internal/scratch ./internal/sessionlayout ./cmd/agent`),
and that is the classification key for the six failures:

- **Reproduced on the base commit** (unpacked with `git archive` — Rule 29, never a
  second checkout): `TestCompact_PinnedWindowBacksOffInsteadOfLooping` (same
  message: `trim emptied the history … messages:null (HTTP 422)`),
  `TestSlashMenuOneCandidateStatesWhyTheArrowCannotMove`,
  `TestDocAnchorsResolveInNormativeDocs`, `TestPrefetchGoroutineSettleGapDetectsALiveLeak`.
- **Load-shaped, not diff-shaped** — `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator`
  (`timed out waiting for the turn that follows the refusal to answer` under load
  8.7) and `TestCodeSearchBackendFindsDefinition` (shells out to the ctags index).
  Both were re-run in isolation on the base tree AND on this branch:
  `--- PASS … (1.34s)` / `--- PASS … (1.57s)` / `ok … 5.811s` on the base, and
  `--- PASS … (0.88s)` / `--- PASS … (0.10s)` / `ok … 3.753s` here. A failure that
  disappears when the machine is quiet is the machine's.

`GOPI_OUTPUT_LANGUAGE=zh-CN` (this operator environment) must be unset for the
suite: the chat-surface tests read the SYSTEM PROMPT it injects, so they fail under
it and pass with `env -u GOPI_OUTPUT_LANGUAGE`.

## Tests

`go test ./cmd/agent/ -run 'TestChatResume|TestChatCLIVocabulary|TestChatCLI|TestRunChat|TestChatHelp|TestSeed'`
→ `ok github.com/nick/gopi/cmd/agent`

`go test ./internal/gopilog/ -run ResumeOptions` → `ok`

New coverage: round-trip through the real event name and field constants; a
session with no record is `found=false` and **not** an error; torn lines are
skipped and the record beside them still read; last record wins; `--resume`
restores `--tools`; the seed autodetects **and** stands down when an option is
stated; explicit-over-recorded for the dump destination; an unknown recorded word
is named while the known word beside it is still applied; `--resume` with no
target and with two targets is refused by name; a session with no record still
resumes its conversation and says the options were not restored; the help page
documents the path and no longer tells the operator to replay flags by hand; and
the regression guard above.

Two existing tests changed because the truth changed — they are transitions, not
relaxations:

- `chat_argv_test.go`'s closed-vocabulary walk now pairs a **valued** flag with a
  value in its own slot. Walking `--resume` bare asserted *"the flag is unknown"*
  — i.e. it would have required the flag to be broken to pass. `--resume` was
  added to `chatREPLFlags()` because that walk is exactly the anti-drift guard
  that should have noticed it.
- `chat_resume_tools_test.go`'s help assertions: `"REPLAY THE FLAGS"` is no
  longer true for any session carrying a record, and a help page that
  contradicts the code is worse than a silent one.

## Not done (honest)

- **The `--dump-screen` restore IS verified end-to-end (added 2026-10-01, after the
  live run below).** A `gopi chat --tools --no-editor --dump-screen` run was
  SIGKILLed mid-tool-call; its record named all three flags; resuming it with the
  id ALONE printed `restored 3 option(s) … --tools, --no-editor, --dump-screen`,
  armed the sink, and the resumed run's own `chat_surface action=see` answered
  `armed=true found=true path=…/af5d7a67-…/chat-screen.dump` — a REAL observation
  from the RESTORED artifact, not the `no observation` an unarmed run reports.
  What remains a property of the harness rather than of the restore: with a piped
  or `script`-wrapped stdin there is no reserved layout to install, so the dump's
  GRID stays an uninterpreted 80x24 blank (`frames=0`) and the tool itself
  qualifies it as such. Reaching a painted grid needs a real terminal on the
  resuming run; the arming — which is what the restore decides — is proven above.
- **A chat killed before its first reply still has no `chats` ledger row**, so
  `gopi chat list` cannot show it (`gopi log --sessions` can). That is the second
  named gap in `devdoc/chat-crash-resume.md` and is **untouched** here — a
  different fix, in the ledger, not in the grammar.
- **Rule 11 (LLM adoption proof) is not claimed for this change**: it adds no
  tool and no prompt-routing guidance, so the adoption standard it would have to
  meet does not apply.
