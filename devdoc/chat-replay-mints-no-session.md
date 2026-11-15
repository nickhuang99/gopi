<!-- doc-class: normative -->
# A replay mints no session — the scratch session root is gone

Task: `01a15981-845f-77cc-824a-94ec3a556077` (epic `chat-replay-is-really-read-only`).
Parent row: `01a1597d-dbdb-7496-b845-203f7ab0d05c` (the memory.db read/write guard).

## What this row removed, and why the thing it removed was never the fix

The parent row stopped a replay from **touching the operator's ledger** (prefetch
queries, auto-memory writes). This row removes the other half of the same blind
spot: the replay's own **session record**, and the scratch tree that used to
hide it.

For two years of this epic's history the shape was:

```
gopi chat --replay <id>
  → startReplay resolves the recording through the operator's sessions root
  → applyChatReplaySessionsRoot() points GOPI_SESSIONS_ROOT at <scratch>/sessions
  → NewAgent mints a session there: <uuid>/session.jsonl, .raw.log, metadata.json
  → the run ends; the tree stays until the scratch sweep collects it
```

`cmd/agent/chat_sessions_root.go`'s own header stated the reason the tree
existed: *"a few dozen shoots left a few dozen look-alike rows in `gopi session
list --mode chat`"* — i.e. the relocation was introduced to keep the operator's
console index clean. That is a real symptom, and the relocation did cure it.
What it also did is make **"the replay wrote into scratch" and "the replay wrote
nothing" indistinguishable** to anyone looking at the operator's profile: the
sessions tree stayed pristine because the writes were moved, not stopped. The
parent row is what proved that distinction mattered (its prefetch defect was live
for exactly as long as the relocation made the profile look untouched).

A replay's whole premise is that it reads a recording. Once the writes it used to
perform are actually refused — slot registry, editor history, chats table,
prefetch, auto-memory — the record it would mint has no reader, no owner and no
content: the stub answers every request in-process, so a `.raw.log` of a replay
records nothing at all, and the `session.jsonl` is a copy of a session that
already exists a few directories away.

## What is in force now

| artifact | before this row | after |
|---|---|---|
| `sessions/<uuid>/` (new directory) | created in scratch | **never created** |
| `session.jsonl` | minted, discarded | **never minted** |
| `metadata.json` | minted, discarded | **never minted** |
| `<uuid>.raw.log` | minted, discarded | **never minted** |
| `GOPI_SESSIONS_ROOT` | exported for the run | **not touched** (the relocation function is deleted) |

The mechanism is the flag the epic already owns — `replaymode.Active()` — asked
at the two functions that are the mint:

- `internal/agent/rawlog.go: NewRawLoggerForRun` — the check is the **first**
  statement, before `GOPI_RAW_LOG_DIR` and before any `os.MkdirAll`;
- `internal/agent/session.go: NewSessionLoggerForRun` — the check sits after the
  task-id parse (the announcement names the layout it did not create) and before
  `sessionDirForID`, i.e. before anything is created.

Both return `(nil, nil)`: the caller's contract is already "best-effort record",
`NewAgent` prints a non-nil error and continues, and every reader below that
point already handles a nil `SessionLog` (52 guard sites in `internal/agent`).
A missing logger is therefore a **stated** absence, not a new failure mode — and
it is stated three times, because three different readers need it:

```
[gopi replay] refused to open this run's raw HTTP log (<uuid>.raw.log): … a replay writes nothing at all: …
[gopi replay] refused to mint this run's session record (sessions/<session-uuid>/session.jsonl, plus its metadata.json): …
[gopi replay] no session record is kept for this run: the recording at <path> is read in place, and this run mints no session directory, no session JSONL, no metadata.json and no raw log
```

The third one is the operator-facing consequence (`cli_chat.go`, printed where
both facts are in hand): without it a console that prints no session path is
indistinguishable from one whose session failed to open.

The refusal sentence itself moved into `replaymode.Reason(src)`, shared by
`Refuse` and by the two minters. **The old wording had to go with the tree**: it
said a replay "writes nothing outside its own session tree", which described the
period when a session tree existed. With the tree deleted, that sentence would
point a reader at an artifact that no longer exists.

## Why not keep the relocation as a belt-and-braces

Rule 35's whole point, and the operator's own reasoning on this row: a scratch
tree is a **persistent artifact** — one more thing to reclaim, one more thing to
ask "whose is this?", and, in this case, one more thing that can make a live
defect look handled. The guarantee now holds by construction (nothing calls the
minter), so the relocation would be machinery guarding against a write that
cannot happen — which is precisely the class of dependency the rule refuses.

`--scratch` keeps its other job unchanged: the beat protocol's tree for a
`--film` run (`cmd/agent/chat_film.go`). It is deliberately no longer consulted
by the sessions path: a flag that could move a session tree would be the escape
hatch Rule 35 bans, and there is no tree left to move.

## Evidence

A real replay of a real recording, on the operator's own profile (`--replay` of
`7db79655-bb87-4553-abb9-3ad95914fc92`, driven through a pty so the surface
actually starts):

```
$ cd ~/.gopi/sessions/7db79655-… && find . -type f | sort | xargs sha256sum   # before
083d8e8f…  ./7db79655-….raw.log
5dd3cce1…  ./metadata.json
e53dd93d…  ./self-record.md
0e4f9247…  ./session.jsonl
$ ls ~/.gopi/sessions | wc -l ; sha256sum ~/.gopi/memory.db
2270
ec9beb60…  /home/user/.gopi/memory.db
$ script -qec "/tmp/gopi-replaycheck chat --replay 7db79655-…" /dev/null       # the run
$ …the same two measurements again                                             # after
```

Both sides are identical, file for file and byte for byte, including
`memory.db`'s sha256 (which is the stronger statement about the ledger: identical
bytes ⇒ identical row counts) and the count of directories in the sessions root.
No `chatreplay-sessions-*` tree was created; nothing new appeared under the
sandbox/scratch roots.

Where the run actually landed matters, so it is stated rather than implied: the
binary was run with **no `GOPI_HOME` override**, i.e. against the operator's real
profile (`/home/user/.gopi`), which is exactly the tree the acceptance is about. A sandbox
profile was created earlier in the run while staging the acceptance and was NOT
used — it is an ordinary `sandbox-replay-accept-*` tree under the configured
sandbox root, owned and aged by the reclaimer that already ticks (Rule 34/35); it
is not part of the deliverable.

## Tests

- `cmd/agent/chat_replay_mints_no_session_test.go`
  `TestReplayMintsNoSessionRecord` — a REAL sessions root holding a REAL
  recording (JSONL + `.raw.log` + `metadata.json`), the tree's file list and
  per-file sha256 taken before and after the two calls `NewAgent` makes
  (`NewRawLoggerForRun`, `NewSessionLoggerForRun`) under `replaymode.Arm`; any
  added file, removed file or changed byte fails the case. The recording's own
  bytes are re-read afterwards.
- the same file's `TestSessionMintStillWorksOutsideAReplay` — the **control**, so
  a minter that never minted would not pass for the right reason: with the flag
  disarmed the same two calls must produce a real session directory, a real
  JSONL, a real `metadata.json` and a real raw log, in the layout
  `sessionlayout`/`config.SessionsRoot` choose.
