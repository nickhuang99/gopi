<!-- doc-class: historical -->
# A Replay Run's Session Lands In Its Own Scratch, Not In The Operator's Index

**Task** `01a12662-202a-769b-b504-bae19624cfb5` · branch `task/01a12662202a769bb504bae19624cfb5`

> **RETIRED (task `01a15981`).** The relocation this document describes no longer
> exists: `cmd/agent/chat_sessions_root.go`, `replaySessionRootIn` and the call
> site in `cli_chat.go` are deleted, and a replay now mints no session at all —
> see `devdoc/chat-replay-mints-no-session.md`. This page is kept as the record of
> why that tree was introduced (the scratch root was a symptom cure: it moved the
> write instead of stopping it) and of what the relocation had to get right.

## The defect

`gopi chat --replay <session>` runs the REAL chat surface over a recording, and
the surface mints a real session. That session landed in the operator's own
sessions root — `<gopi dir>/sessions/<uuid>/` — every single time. `gopi chat
--film` runs one `--replay` per shoot, so a few dozen film runs left a few dozen
look-alike rows in `gopi session list --mode chat`, each with its own `.raw.log`
(measured on the live profile: 30–54 MB per directory). The index that exists to
answer "which conversations did I have here?" filled up with copies of sessions
that were never a conversation.

## The fix, and why it is shaped this way

**A relocation, not a second storage layer.** The run's session root becomes the
run's OWN scratch tree; nothing about the session format, the JSONL, the logger
or the storage layer changes.

1. **ONE resolution point.** `config.SessionsRoot()` (`internal/config/sessions_root.go`)
   answers "where does a session run's directory live". Precedence:

   ```
   $GOPI_SESSIONS_ROOT            (the process's own relocation)
   <gopi dir>/<SessionsDirName>   (the documented default)
   ```

   Every in-process writer and reader of that root now goes through it:
   `internal/agent` (`sessionDirForID` — the ONE function that mints a run
   directory, so the JSONL, its co-located `.raw.log`, the inbox, the self-record
   and the orphan/dump sweeps all follow), `internal/gopilog.ResolveSessionDir`
   (the session resolver every verb reads), `internal/rundir.Root`,
   `internal/uiclient.gopiSessionsRoot` (the dump walk), `internal/tools.chatSessionsDir`
   (the `LocalOnly` presence probe), `internal/signalrec` (the crash record's
   destination), `cmd/agent`'s session-dump hint. The relocated value is
   honoured **verbatim** — a caller that exports a root means that root is the
   root — and the empty value is treated as absent (`os.Getenv` renders "unset"
   and "empty" alike, and a root of `""` addresses the process's own working
   directory).

   **Why one env var is the right channel.** It is the same kind of channel
   `GOPI_HOME` already is: a value that lives only inside the process tree that
   set it, consumed by the process's own resolution, installing nothing and
   outliving nothing (Rule 35 — no file, no daemon, no config key, no new
   dependency; `CONFIG.md` is unchanged because no catalogued knob moved). A
   process that exports nothing reads the documented default, so an ordinary
   `gopi chat` is untouched by construction rather than by a branch.

   **Why not `GOPI_HOME`.** `GOPI_HOME` relocates the WHOLE profile —
   `settings.json`, the ledger, `plugins/`, the logs. A replay must keep reading
   the operator's real configuration and must not touch the real ledger. One
   profile, one relocated tree.

2. **The replay's root is the run's OWN scratch** (`cmd/agent/chat_sessions_root.go`),
   taken from the EXISTING `--scratch` value when the caller named one — the same
   directory the beat protocol already writes `beats.tsv`/`ack`/`done.txt` into,
   so a film passes ONE tree to both halves of its work — and otherwise allocated
   through `internal/scratch` (`scratch.MkdirTemp`, class `RuntimeClassUISession`
   = the surface's own class, pattern `chatreplay-sessions-`). Off `/`, owner
   recorded, swept by the machinery that already ticks (Rule 22).

3. **The root is a CHILD named `sessions`**, not the scratch directory itself.
   The sessions tree's contract is *"its children are run directories"*
   (`internal/checkpoint.isSessionsRoot`, `internal/sessionlayout`), and a film's
   scratch already holds the protocol's own files. One name — `config.SessionsDirName`,
   never a literal (Rule 1/13) — keeps the tree recognisable to the readers that
   key on it, beside files that are not sessions.

4. **The embedding compiler's note — the ordering is the contract.** The
   publication sits at ONE call site (`cli_chat.go`, inside `if replayTarget != ""`),
   after `startReplay` and before `buildAgent`. Both ends of that window matter:
   `--replay <session-id>` resolves the id through `gopilog.FindSession`, which
   reads the sessions root, and **the recording lives in the operator's root** —
   publishing any earlier would make the resolver look in a scratch root that
   holds no recordings and refuse an id that exists; the agent (and therefore the
   session logger) is built immediately after, so publishing any later would miss
   the very write the relocation is for.

## The SIGKILL / residue answer

**A killed run leaves its session under a scratch tree that is reclaimed
automatically — and the operator's index is untouched either way.** Three
mechanisms, in the order they apply:

1. **The owner record.** `scratch.MkdirTemp` → `RuntimeScratchDir` writes an
   owner record (pid + kernel start ticks + command line) into the runtime
   class directory at the moment it is created. A `SIGKILL` leaves no cleanup
   hook behind, so the record — not the process — is what proves the staleness:
   the next sweep sees a dead owner and collects the tree immediately rather
   than waiting out the age window.
2. **The existing sweep.** The runtime scratch classes are collected by
   `gopi scratch-gc` / the resident tick's `scratchGCTick`; the entry name
   (`queue-runtime-<class>-<pid>`) is the one the sweep already recognises. No
   new sweep, no new hook, no new dependency (Rule 35).
3. **The relocation is process-local, so the blast radius is one process.**
   `GOPI_SESSIONS_ROOT` is published into this process (and, for a film, into the
   producer it spawns, which inherits the environment) and restored on exit.
   There is no on-disk state to clean up, no lock to leak, and no second copy of
   the tree to reconcile.

**What is NOT claimed.** A compiled film run that is `SIGKILL`ed leaves the
session JSONL and whatever `.raw.log` had been flushed inside the scratch tree
until the sweep runs — the same residue every scratch class has, bounded by the
same machinery, and deliberately not deleted by a `t.Cleanup`-style hook (which
does not run on `SIGKILL`, which is exactly when residue appears).

## What deliberately did NOT change

- The **normal chat write path**: `gopi chat` publishes nothing, so `SessionsRoot()`
  answers the default and the session lands in the profile exactly as before.
- `--film`'s external behaviour: frames, `frames.tsv`/`beats.tsv`, the GIF, the
  wechat check, exit codes — untouched. The relocation changes where a session is
  written, never what the film draws.
- `session list`: it is a reader of the LEDGER; the relocation adds no row and
  removes none.

## Files

| file | change |
|---|---|
| `internal/config/sessions_root.go` | NEW — `SessionsRootEnv`, `SessionsRoot()`, `SessionsRootIn()` |
| `cmd/agent/chat_sessions_root.go` | NEW — `publishReplaySessionsRoot`, `applyChatReplaySessionsRoot` |
| `cmd/agent/cli_chat.go` | the ONE call site, inside the `--replay` branch |
| `internal/agent/session.go` | `sessionDirForID`, `DetectOrphanSessions` → `config.SessionsRoot()` |
| `internal/agent/crashdump.go`, `crashdump_sweep_gate.go` | the dump sweeps → the same resolver |
| `internal/gopilog/reader.go` | `ResolveSessionDir` → the resolver |
| `internal/rundir/rundir.go` | `Root()` → the resolver |
| `internal/uiclient/dumpread.go` | the dump walk boundary → the resolver |
| `internal/signalrec/signalrec.go` | the record's destination → the resolver |
| `internal/tools/chat.go`, `memory_brew_run.go` | `chatSessionsDir`, `brewSessionsDir` → the resolver |
| `cmd/agent/main.go` | `sessionDumpHint` → the resolver |
| `internal/config/sessions_root_test.go`, `cmd/agent/chat_replay_sessions_root_test.go` | NEW — the regression guard |

## Regression guard

`cmd/agent/chat_replay_sessions_root_test.go` drives the REAL seam —
`applyChatReplaySessionsRoot` publishes, then a session is minted through the
production constructor `agent.NewSessionLoggerForRun` — and asserts (a) the
session landed under the scratch, (b) the profile sessions root holds **zero**
entries afterwards, (c) the restore returns the process to the profile root.
Remove the relocation and it fails on the profile root; that is the flood,
measured rather than described.

`internal/config/sessions_root_test.go` pins the resolution itself: relocated
value honoured verbatim, default unchanged, and `SessionsRootIn(GopiDir())`
equal to `SessionsRoot()` (one join, two spellings — Rule 14).
