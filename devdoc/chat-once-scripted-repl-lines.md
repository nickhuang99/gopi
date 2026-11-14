<!-- doc-class: historical -->
# Task Report: the non-interactive `gopi chat` face accepts REPL LINES (scripted sessions)

Task `01a0dc29f1077766a08cf775c9adcbef` · branch `task/01a0dc29f1077766a08cf775c9adcbef`
· worktree `~/workspace/gopi-01a0dc29f1077766a08cf775c9adcbef`. Base: master
`c90205bd`.

**This file is a REWORK.** The scripted-REPL face first landed on the deleted
branch `wip/master-local-only-20260926` (its own report was `01a0db61…`), whose
branch could not merge (master moved under it). This run re-implemented the face
from the archived diff's INTENT — never `git apply` — on a fresh branch off
current master, per GOPI.md Rules 6a + 23. What master ALREADY carries (the
6-column ctx/cost status-row geometry, the text2img ink fallback, the
chat-status devdocs) is deliberately NOT touched here: `statusTokensCol` stays
retired and no geometry was re-widened.

## Completed

- **`gopi chat --once` treats its input as REPL LINES when the first line is a
  slash command** (`cmd/agent/chat_once_seq.go`). Those lines are written into the
  REPL's OWN reader (`newChatInputCooked` → `runChatWithInput`), so every line is
  read by the interactive loop and reaches the arm it names — the command switch,
  the session commands, the model turns. **No second command table**: the only
  predicate involved is `isSlashCommand`, the same "/" trigger the REPL and the web
  carrier branch on, and the execution path is `execSlashCommand` / `chatHelpText`
  exactly as a human's `/help` is.
  - argv command lines are accepted too: every argv element that IS a command line
    joins the transcript (so `gopi chat --once /tools "review the diff"` is a
    command followed by a message).
  - the REPL's argv is the tail with those command words removed
    (`chatOnceSeqRest`) — they are transcript, not REPL flags.
- **The exit contract is honest** (Rule 15), and it is armed through ONE seam
  (`chatOnceFailureObserver`, `cmd/agent/chat_once.go`) that the REPL's own arms
  report into:
  - `/quit` (or `/exit`) with nothing failed → **0**. Before this, a scripted
    `/quit` exited 1 with a fabricated `the turn completed without producing any
    text` for a run that never asked the model anything.
  - a command line that cannot be honoured → **1**, named twice: the REPL's own
    `[gopi] unknown command "…"` and this face's
    `gopi chat: a scripted line was not honoured: …`. Before this it was exit 0 —
    a driver could not tell a scripted command that ran from one that was refused.
    The refused line is NEVER sent to the model (asserted on the wire, below).
  - a failed turn → **1** (`chatOnceFailureTurn`; the REPL keeps the session open
    by design — bug 01a0d513 — so the exit status is the only place this face can
    learn of it).
  - usage → **2** (unchanged).
- **`chatOnceSeqPick`:** a one-prompt run now takes stdin ONLY when no
  `-p/--prompt/positional` was given (the precedence this face always documented —
  it previously concatenated both).
- **Input that does NOT open with a command is untouched**: one prompt is still one
  message, and a multi-line prompt is still folded into ONE message
  (`chatOnceOneLine`). The `--once` face a caller already scripts did not move.
- **`-h` documents the contract** (the scripted-session paragraph and its exit
  statuses, in the same `chatOnceUsage` page).

## Not Done (honest)

- **Discovery scope, measured not assumed.** `wait`-type commands that must await
  a live process were not extended; this face reads a transcript and returns.
- The `[gopi] refused an inherited task-report contract …` line on sandbox stderr
  is ambient environment noise (an inherited `GOPI_TASK_REPORT_PATH` naming another
  session's file), not from this change; not investigated.
- The REPL still prints its `you>` prompt to stderr once per line on the cooked
  path. That is the historic byte stream of a piped `gopi chat` and was left
  byte-identical on purpose; a driver parses stdout, which carries no prompt.
- **Two pre-existing `internal/standards` × `devdoc` inconsistencies were found on
  master and only ONE is repaired here** — see "Pre-existing reds (A/B)" below.
- `make test-budget PKGS='./cmd/agent ./testsuite'` as ONE budgeted invocation was
  NOT run: the two packages were driven separately against the built binary (each
  green, timings below). The budget wrapper's own git-stream isolation is a
  different task's surface; this run reports the numbers it measured.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0, no output); `go vet ./cmd/agent/ ./testsuite/` → clean.
- Tests:
  - `GOPI_BIN=$PWD/bin/gopi go test ./testsuite/ -run 'TestChatOnce' -count=1` → `ok … 96.183s`
    (6 new `TestChatOnceSlash_*` cases + the 18 pre-existing `--once` cases).
  - `GOPI_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run 'TestChatOnce|TestG5' -count=1` → `ok … 21.136s`
    (the G5 help-surface census is UNCHANGED — no flag was added or removed, only
    the `--once` usage paragraph grew).
  - `go test ./internal/standards/ -run 'TestDocClass|TestDocIndex' -count=1` → `ok 0.954s`
    (RED before this run's banner repair — see below).
- Ran (Rule 31 — the changed surface, for real, through the built binary
  `bin/gopi`; the first invocation inside a `gopi sandbox` profile):

```
$BIN sandbox create --label chat-once --task-id 01a0dc29f1077766a08cf775c9adcbef
   → sandbox /home/user/workspace/gopi-sandbox/sandbox-chat-once-339ab79e created=true
$BIN chat --once /help                       # rc=0, stdout 12298 bytes
   → stdout carries: Slash commands (executed locally — never sent to the model)
   → stderr ends: gopi chat — Ctrl+D or /quit to exit. / you> you> bye.
printf '/help\n/quit\n' | $BIN chat --once    # rc=0, table present, session closed ("bye.")
printf '/memory list\n/quit\n' | $BIN chat --once  # rc=0, stdout: "20 most recent record(s):"
$BIN chat --once /no-such-verb               # rc=1, stdout 0 bytes, stderr:
   → [gopi] unknown command "no-such-verb" — available: code-search, log, memory, new, task, text2img
   → gopi chat: a scripted line was not honoured: unknown command "no-such-verb" — …
```

- Result: every claim above is an observed exit code and an observed byte count
  from this run; the ZERO-model-request claim is asserted by the mock in the
  testsuite (`llm.RequestCount() == 0`), not by reading stdout.

### RED proof (removing the dispatch)

The dispatch block in `runChatOnce` was replaced by a comment, `/tmp/gopi-red` was
built from that tree, and the new suite was run against it:

```
CGO_ENABLED=0 go build -o /tmp/gopi-red ./cmd/agent/
GOPI_BIN=/tmp/gopi-red go test ./testsuite/ -run 'TestChatOnceSlash' -count=1 -v
```

Result (measured this run): **5 of 6 FAIL** —
`ArgumentIsDispatchedNotSentToModel` (`exit = 2, want 0`),
`StdinSequenceIsDispatched` (`stdout carries no /help table`),
`CommandThenMessageInOneScript` (`exit = 1, want 0`),
`RefusedCommandIsLoudAndNonZero` (`stderr does not NAME the offending command`,
`does not say the scripted line was not honoured`, `reports a MODEL TURN failure
for a run with no turn`), `OnDiskSideEffect` (`exit = 2, want 0`). The 6th,
`PlainPromptIsStillOneMessage`, stayed GREEN and must — it guards the face this
change deliberately did NOT touch, so a red there would mean the wrong thing.
Restored from a pre-edit copy; `grep -c 'RED PROOF' cmd/agent/chat_once.go` → `0`.

## Evidence (self-filed)

- `llm.RequestCount() == 0` for a command-only script (both the argv and the stdin
  shape) — the mock served NOTHING, so the command was dispatched, never sent.
- `llm.RequestCount() == 1` for `"first line\nsecond line"` piped with no command —
  one folded message, the untouched face.
- The wire claim is asserted on the request's **user messages**, not as a body-wide
  substring: a body-wide search matches the SYSTEM prompt (which documents
  `/tools`), which is why `chatOnceWireUserTexts` exists.
- `/text2img`'s artifact read back from disk: non-empty, header `\x89PNG`.
- `printf '/memory stats\n/quit\n' | gopi chat --once` → **rc=1** with
  `unknown memory subcommand "stats" — want search|store|get|list`: the brief's
  example line is a refused verb on master, so it IS the live proof of the refusal
  path (the accepted forms are `list|search|store|get`).

## Pre-existing reds (A/B, not regressions)

1. `devdoc/tui-turn-status-live-hint.md` carried `<!-- doc-class: historical -->`
   in **master's own blob** while master's `DOC-INDEX.tsv` classes it `normative`
   (a resolving `sym` anchor on `testsuite/chat_pty_test.go
   TestChatPTY_LiveHintShowsTheTurnInFlight`) — `TestDocClass…` was RED on master
   with neither file touched by this diff (`git diff --name-only master` lists only
   `cmd/agent/chat_once.go` and `cmd/agent/cli_chat.go`). Master's classification is
   the authoritative one, so the STRAY BANNER LINE was removed (1 deletion, no
   manifest edit) rather than the class rewritten. This is the same repair the
   original run had to make — it is pre-existing, and it is reported here as
   repaired, not caused.
2. NOT repaired (left live, deliberately): the manifest's rows are generated against
   whatever master's corpus was, and a repo-wide `classify.py` pass re-anchors
   hundreds of unrelated rows — a 1-line bookkeeping row is not worth a
   corpus-wide rewrite and a fresh conflict surface, which this rework exists to
   avoid. A NEW doc does need its own row, and this run adds exactly that one
   (`--only devdoc/chat-once-scripted-repl-lines.md`).
