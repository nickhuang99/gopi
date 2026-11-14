<!-- doc-class: historical -->
# Task Report: round-level `[IMPORTANT]` note index (`chat_notes`) + `gopi session notes`

Task: `01a1235b-41ef-7a77-86bb-32de613d4111` (branch `task/01a1235b-41ef-7a77-86bb-32de613d4111`).

## Completed

- **`internal/tools/chat_notes.go` — the index.** New table `chat_notes`
  (`session_id`, `round_index`, `note`, `source_host`, `source_path`,
  `created_at`), the FTS5 pair `chat_notes_fts` (unicode61, over the note text)
  and `chat_notes_tri` (trigram), both **external-content** (`content='chat_notes'`,
  `content_rowid='id'`), and the three keep-in-sync triggers `chat_notes_ai` /
  `chat_notes_ad` / `chat_notes_au` — the `chats_fts`/`chats_tri` pattern copied,
  not re-invented. Plus `ChatNoteRecord` (the writer), `ChatNoteSearch` (a
  three-tier recall ladder: fts → trigram → like, first hit-bearing tier wins,
  mirroring `TaskSearch`), and the renderer. `ensureChatNotesSchema` opens through
  the existing `chatDBPath()` opener (the same database file the chat index uses —
  one store, two grains) and calls `ensureChatSchema` first, so a fresh database
  materialises both schemas from one entry point.
- **`internal/agent/selfrecord_noteindex.go` + `recordSelfNotes` (ONE write
  point).** The same call that appends `self-record.md` now also inserts the row:
  the file is the **evidence** layer and keeps its exact semantics (verified: one
  note, one line, unchanged), the table is the **retrieval** layer over the same
  facts. The index write is best-effort — it is logged with its reason and the
  round continues (Rule 15b: never silent, never fatal).
- **Provenance, from existing sources only (Rule 14).** `source_host` =
  `tools.OriginHostName()` (the node override then the hostname; the *same single
  definition* the memory provenance column uses, so a note and a distilled row
  from one run cannot disagree). `source_path` = the **full local path** of the
  record file. `session_id` = the run's own session log id. Nothing is asked of
  the model.
- **The round number is the session's own `request` count.**
  `SessionLogger.LogRequest` now counts the events it writes and exposes
  `RequestsSoFar()`; `recordSelfNotes` records at `RequestsSoFar()+1` — i.e. the
  ordinal of this session's `request` events, which is the numbering
  `sessionevent.RoundNumberOf` defines (the iteration's 0-based index + 1) and the
  numbering `gopi session log <id> <round>` takes. **This replaced the first
  design (read `a.hippocampusRound`), because the live probe contradicted it**: on
  the chat surface the round cursor is stamped only when a hippocampus host is
  enabled, so the first real run wrote `round ?` (RoundIndexUnknown). Measured in
  this task's own `Ran:` section below.
- **`gopi session notes <query> [--limit N]`** — the retrieval face, a sub-verb of
  the existing `session` family (Rule 3: no new top-level verb; it reaches
  `gopi session <TAB>` by construction, since the completion set *is*
  `sessionSubcommandNames`). It parses and delegates to `ChatNoteSearch` — no
  second search implementation. Each hit prints host, round, time, session, then
  the full path and the note text.
- **`internal/tools/chat.go`** — the sibling note the epic asked for: the chat
  index comment now says what the new pair indexes and why it is still not a
  messages-level index.
- **Tests.** `internal/tools/chat_notes_test.go` (red→green, CJK inside a longer
  run via trigram, 2-char CJK via the LIKE floor, the two refusals, round-absence
  rendered as absence, limit disclosed), `internal/agent/selfrecord_noteindex_test.go`
  (the wiring: file unchanged + row searchable + provenance asserted), and
  `cmd/agent/cli_session_notes_test.go` (the real binary: dispatch, both tiers,
  full path and host, the usage refusal, and the two help contracts). The
  `session notes -h` row was added to `cmd/agent/cli_contracts_test.go`, and the
  word to `sessionSubcommandNames`, so the family page, tab completion and the
  help-hierarchy guard all see it.

## Not Done

- **No vector search (by design, phase 1).** Only FTS5/BM25. See the phase-2 note
  below for what a zero-dependency path would require.
- **No backfill of notes recorded before this change.** Existing `self-record.md`
  files are not scanned into the table: that would be a second writer on a path
  nobody dispatches, and the epics's first phase is about the live write. A reader
  who wants the history re-indexed needs an explicit command, which is not built.
- **`gopi session notes` has no provenance filter** (`--host`, `--session`) yet.
  The columns are indexed for it (`idx_chat_notes_host`, `idx_chat_notes_session`),
  so the query is a WHERE clause away, but the flags are not there.
- **The task-loop path was not exercised end to end with a live `task run`
  child** (only the chat path was, plus the agent-layer unit test which drives the
  real `recordSelfNotes`). The same call site is shared, so the wiring is covered;
  the surface is not.

## Verification

- **Build:** `go build ./...` → no output (OK). `CGO_ENABLED=0 go build -o bin/gopi
  ./cmd/agent/` → OK.
- **Tests:** `go test ./internal/agent/ -run TestRecordSelfNotesAlsoIndexesThem
  -count=1` → `ok github.com/nick/gopi/internal/agent 0.912s`;
  `go test ./internal/tools/ -run TestChatNote -count=1` →
  `ok github.com/nick/gopi/internal/tools 0.862s`;
  `go test ./cmd/agent/ -run TestSessionNotes -count=1` →
  `ok github.com/nick/gopi/cmd/agent 2.016s`;
  `go test ./cmd/agent/ -run 'TestHelpPagesAdvertise|TestSession|TestUsage|TestHelp' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 13.152s`.
  `go test ./internal/standards/ -count=1` → the FIRST run was **RED** on
  `TestCommandsDocGeneratedBlockMatchesSource` (the generated verb index in
  `COMMANDS.md` carries two line references that my edits moved:
  `cli_memory.go:204→205`, `cli_session.go:213→223`), so I regenerated it with the
  gate's own generator — `go run internal/standards/testdata/commands-census.go .
  COMMANDS.md` → `commands-census: wrote COMMANDS.md (44 verbs)` — and re-ran:
  `ok github.com/nick/gopi/internal/standards 23.534s`. `gopi tool doccheck` →
  `the devdoc corpus is consistent` (the DOC-INDEX row for this report was written
  by the write hook, and the regenerated `COMMANDS.md` is part of this commit).
  Branch-scoped budget run: `make test-budget PKGS='./internal/tools ./internal/agent ./cmd/agent'`
  → exit 1, elapsed 897s, budget NOT tripped; **all eight tests this change adds PASS in
  that run** (read from the run stream's `-json` events: the five `TestChatNote*` cases,
  `TestRecordSelfNotesAlsoIndexesThem`, and both `TestSessionNotes*` cases — 0 failures).
  The 15 failures are pre-existing and environmental, each reproduced at the parent commit
  `2d1b48d1d` in a detached worktree
  (`git worktree add /home/user/workspace/gopi-base-01a1235b 2d1b48d1d --detach`):
  `ctags`/`rg` are absent from PATH on this host (`code_search: ctags: exec: "ctags":
  executable file not found in $PATH`), the merge-build-gate family refuses the suite's
  tmpfs scratch root as a Go build cache (the 2026-09-15 incident is its stated reason),
  and `TestDocAnchorsResolveInNormativeDocs` fails at both revs on 13 stale baseline
  permits — my new devdoc adds ZERO anchor drift (`grep -c chat-notes-round-index` over the
  failure output → `0`).
- **Ran:**
  - `GOPI_HOME=/home/user/workspace/live-01a1235b ./bin/gopi chat --once -p "Reply
    with exactly: note-index-live-ok. Do not call any tool. End your response with
    the [IMPORTANT] block required by your instructions, whose summary mentions
    轮级索引 and the token liveprobe4417."`
  - `GOPI_HOME=/home/user/workspace/live-01a1235b ./bin/gopi session notes 轮级索引`
  - `GOPI_HOME=/home/user/workspace/live-01a1235b ./bin/gopi session notes liveprobe4417`
  - (after the round-number fix) `... chat --once -p "... mentioning 第二轮
    liveprobe8823"` then `./bin/gopi session notes liveprobe8823` and
    `./bin/gopi session notes 第二轮`
- **Result:** the first live run produced a note and both searches hit it, but the
  provenance showed `round ?` — the round cursor was never stamped on that path
  (the hippocampus host is off by default), which is the defect the
  request-counting design fixed. The second live run (after the fix) shows the
  fix working end to end:

  ```
  $ GOPI_HOME=/home/user/workspace/live-01a1235b ./bin/gopi session notes liveprobe8823
  1 note(s) matching "liveprobe8823" via fts:
    hosta  round 1  2026-10-10 11:43  0f4ecfab-f195-43a6-881d-105ad465dbff
        /home/user/workspace/live-01a1235b/.gopi/sessions/0f4ecfab-.../self-record.md
        第二轮 liveprobe8823：按要求原样回复 second-live-probe-ok，未调用任何工具，未做任何仓库改动。 | 无

  $ GOPI_HOME=... ./bin/gopi session notes 第二轮
  1 note(s) matching "第二轮" via fts:
    ... 自-record.md path + note ...

  $ GOPI_HOME=... ./bin/gopi session notes 轮级索引      # first run, CJK inside a longer run
  1 note(s) matching "轮级索引" via trigram:
    hosta  round ?  ...  (the pre-fix round; the note and path are correct)
  ```

  The machine column (`hosta`) and the FULL path are both present, and the
  two CJK queries demonstrate the tier split (a 4-character fragment inside a
  longer run → trigram; a query that is a whole stored token → fts).
- **Commit:** see the branch tip (the report is committed with the change).
- `git status --porcelain` in the worktree: empty at close-out.

## Evidence (self-filed)

- `go test ./internal/tools/ -run TestChatNote -count=1` → `ok ... 0.862s` (the
  red→green pair, the CJK tiers and the refusals in one run).
- `go test ./internal/agent/ -run TestRecordSelfNotesAlsoIndexesThem -count=1`
  → `ok ... 0.912s` (the file-unchanged + row-written assertion).
- `go test ./cmd/agent/ -run TestSessionNotes -count=1` → `ok ... 2.016s`
  (the real binary: `via fts`, `via trigram`, `round 7`, `nick-…`, the full path,
  and the usage refusal).
- The two live runs above: their own search output is quoted verbatim, including
  the `round ?` line that contradicted the first design.
- The standards suite went red on the generated block and green after
  regeneration (`TestCommandsDocGeneratedBlockMatchesSource`: red on the
  `cli_session.go:213→223` / `cli_memory.go:204→205` line moves, then
  `commands-census: wrote COMMANDS.md (44 verbs)` → `ok ... 23.534s`).

## Phase 2 (vector search) — preconditions, without breaking zero dependency

Phase 1 is deliberately FTS5/BM25 only: `chat_notes` has no embedding column, no
`sqlite-vec`/`vss` extension, no model call. Three facts make a later phase cheap,
and one makes it dangerous:

1. **The rows are already addressable.** `chat_notes.id` is the join key any
   vector side-table would use, and the writer is a single function
   (`ChatNoteRecord`) — a phase 2 adds a column or a sibling table at ONE site.
2. **The recall ladder is a tier list, not an if-chain.** `ChatNoteSearch` tries
   tiers in order and the first hit-bearing one wins; a semantic tier is an entry
   in that slice, which is how the existing `fts → trigram → like` ladder was
   built.
3. **What must NOT happen:** pulling `sqlite-vec` (a C/C++ loadable extension) or
   any embedding runtime into the binary. The repository is CGO-free and
   extension-free by contract, so a `vss`-style index would end both. The
   zero-dependency shapes, in rising cost: (a) store no vectors at all and make
   phase 2 a *re-ranker over the existing BM25 candidates* using a pure-Go
   trigram/character-n-gram similarity — no new dependency, bounded work per
   query; (b) an out-of-process embedder behind an env-configured endpoint (the
   pattern `internal/deepseek` already uses), which keeps the binary pure Go but
   makes the feature depend on a running service; (c) a pure-Go embedding model,
   which is a real dependency decision and needs its own Rule-35 argument. My
   recommendation, if phase 2 is commissioned: (a) first, and only then (b).
