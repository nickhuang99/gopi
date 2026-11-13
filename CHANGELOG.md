# GoPi Changelog

## Unreleased — one turn count: the bare word is the session's, and the person's subset is named

### Changed — the surfaces, and one WIRE key an external script may read
- **`turns` is ONE number again, and it is `chats.turn_count`** — the turns the
  session ran. An external notify is an input, a self-driven turn is a turn, and
  a line the person typed is an input, so all three open the same thing
  (the operator's ruling, 2026-10-27). The word is not qualified anywhere and is
  not printed beside a second turn total.
- **The person-only subset is kept, under its own word**: `human turns` — the
  rows the operator typed, the same number `session log <id> --turns` tabulates.
  It prints UNDER the count on `session show` (and in `session stats`' box), it
  never prints on a one-line index row, and it can be unmeasured without
  affecting the count above it, which needs no log at all.
- **`gopi session list --mode chat`** prints ONE turn clause: `turns 200` (was
  `turns 66 (repl turns 200)`).
- **`gopi session show <id>`** prints `turns       200` then
  `human turns 66` (was `turns 66` / `repl turns 200` — the pair, with the
  subset's number first).
- **`gopi session log <id>` / `session stats`** print the subset under
  `human turns` and point at `session show` for the turn count — the block cannot
  read the ledger (internal/tools imports internal/gopilog, so the import would
  be a cycle), and promoting the subset into the total's place is the collision
  this change removes.
- **JSON**: `turns` carries `chats.turn_count`; `human_turns` carries the subset
  (`human_turns_known`, `human_turns_note`). `repl_turns` survives until
  **2026-12-31** as a NUMBER nested inside `deprecated_keys`
  (`{repl_turns, replaced_by, remove_after, note}`), which states its own
  replacement and deadline on every row — deliberately not published beside
  `turns` at the top level. `operator_turns` is not republished: the subset now
  has one name.
- **Reverted from the previous unreleased entry**: that entry moved the count to
  `repl_turns` to keep it apart from a rival reading of `turns`. The rival is
  what the ruling abolished, so the qualifier went with it. Its "the human faces
  do not change" claim was true of that step and is superseded here.
- Pinned by `cmd/agent/session_json_wire_test.go` (the tag against the CONSTANT,
  since a struct tag cannot hold one; no second top-level turn number),
  `cmd/agent/session_list_turns_test.go` and
  `cmd/agent/session_show_turns_test.go` (one count, the subset under its own
  word, a word-boundary check that "human turns 11" does not read as a bare
  `turns 11`).

## v0.2.0 — the clone / workspace GROUND channel

### Changed
- **The compiled-in version is now `v0.2.0`** (`internal/version.DefaultVersion`),
  a MINOR bump for the framework change that earned it: a cloned repository can
  be a task row's **ground** — the row records the workspace itself rather than
  a derived worktree name — and that row is driven through a `dev_repo` switch
  plus a pull/build/push loop. Bumped by hand, in the commit that earns it; the
  merge path still mints no version tag and no build step rewrites the string
  (devdoc/version-identity.md § the bump log).
- **The `gopi version` REPORT SHAPE is unchanged** — version top line, then
  the build metadata including the `revision:` line the post-merge verifier
  (`internal/tools/rebuild.go`) reads via `version.RevisionOfDisplay`.
- **Version fixtures read the constant instead of spelling a literal**: the
  chat banner fixture (`internal/lineedit`), the output-language surface
  marker and `chat_banner_test.go` in `cmd/agent` now take the value from
  `internal/version`, so the next bump moves code and fixtures together.

## Unreleased — CLI help de-duplication (one function, one help text)

### Changed
- **`gopi task serve -h` prints ONE pointer line**, not the whole `gopi task`
  family usage: the pre-split spelling still dispatches (scripts and running
  deployments keep working), and the operator learns the canonical home
  (`gopi merger`) from a sentence instead of diffing two pages.
- **`gopi task serve` / `gopi task merger` vs `gopi merger`** now follow the
  `web_search` → `web search` deprecation contract: dispatch kept, one pointer,
  canonical verb in the index, contract-suite pin. New contract cases:
  `task serve` (one-line pointer, single-line assertion) and `task dedup-scan`.
- **`gopi memsync` and `gopi memory sync` cross-reference each other by
  purpose** (patch-FILE channel vs live ssh session — siblings, neither
  deprecated), and the top-level index says which is which. The
  `gopi memsync --dedup-scan` audit is **re-homed to `gopi task dedup-scan`**
  (a ledger maintenance read does not belong under a row-transferring verb):
  the old spelling still dispatches and is refused with a pointer to the new
  home.
- **The CLI naming rule is stated** (tool-named commands keep the tool's
  underscore spelling; gopi's own verbs are dash-cased) in
  `devdoc/cli-naming-convention.md` and in the top-level help's tools section;
  `vision_analyze` / `screen_capture` / `voice_input` are documented deliberate
  exceptions, re-checked by `cli_completion_exceptions_test.go`.

### Fixed
- The top-level help's task index no longer hides `serve`'s relationship to
  `merger` (found by the 2026-09-15 CLI surface audit, Case 4).

## Unreleased — voice input: measured path to ZERO dependency (study)

### Added
- **`devdoc/voice-input-zero-dependency.md`** — measured feasibility of shipping
  the speech-to-text engine **and its weights inside the gopi binary** instead of
  depending on an external `llama-funasr-cli` (6.2 MB) plus ~910 MB of model
  files. Verdict: **feasible**, and the 500 MB budget is comfortable.
  - **Budget target: whisper.cpp `small` quantized to `q5_0` → a 171.3 MiB
    embedded binary** (static: 173.8 MiB) that transcribes Chinese perfectly,
    including the discriminating word 语音识别 (speech recognition) that tiny/base
    models mis-hear. Measured alternatives: q8_0 252.2 MiB, f16 469.2 MiB (no room
    left for gopi), gzip+f16 429.7 MiB but +14.1 s startup and 2.75 GB RSS, today's
    Fun-ASR-Nano 909.4 MiB (1.8× over budget).
  - **Compression finding**: quantization beats every general codec on the same
    weights — f16 465 MiB → q5_0 167 MiB (ratio 0.359) versus zstd-9 0.911 and
    gzip-9 0.915 — and it costs nothing at startup while *lowering* RSS. zstd is
    uniformly better and 8× faster than gzip; zstd level 3 (10 s) is the only sane
    codec point (level 19: 518 s for 3% more).
  - **Rejected**: whisper `small` q4_0 (145 MB) — one token degrades
    (更好地 → 更好的 — homophone drift, "better" → "better") on an 11 s clip;
    quality floor is q5_0.
  - `//go:embed` framing overhead is **10 KB** — the mechanism is free.

### Added
- **`devdoc/voice-zerodep-poc/`** — the runnable proof (nested Go modules, so the
  root `CGO_ENABLED=0 go build ./...` is unaffected):
  - `main.go` + `model_file.go` / `model_embed.go` / `model_embedgz.go` /
    `model_embedq5.go` — four build shapes (`-tags embed|embedgz|embedq5`) linked
    against static whisper.cpp.
  - `memfd/main.go` — proves a **path-based** GGUF loader (llama.cpp family)
    accepts an anonymous memfd: whisper logs `loading model from
    '/proc/self/fd/6'` → rc=0.
  - `memfdelf/main.go` — proves a whole **engine ELF can be exec'd out of a
    memfd** with the model in a second memfd, `child exit=0`. This is the shape
    that keeps gopi `CGO_ENABLED=0` (Rule 12's declared build untouched) while
    both engine and model ship embedded.

### Verified
- strace over the embedded binary in an isolated dir: **0** opens of any
  `.bin`/`.gguf` — the only file touched is the input WAV. Re-proved on a clean
  checkout with the engine's own log line as corroboration:
  `whisper_init_from_buffer_with_params_no_state: loading model from buffer`.
- Functional: every shape transcribes `/tmp/tts/zh_long.16k.wav` to the exact
  ground-truth sentence including 语音识别 (speech recognition) — at q5_0, q8_0,
  f16 and gzip embeds.

### Fixed
- **The PoC's recommended shape did not compile.** `model_file.go`'s build
  constraint was `!embed && !embedgz` — missing `!embedq5` — so `-tags embedq5`
  died with `initEngine redeclared in this block` / `transcribe redeclared`. The
  other three shapes built, which is why the reported matrix looked green. Now
  `!embed && !embedgz && !embedq5`; `-tags embedq5` builds in 43 s to
  179 566 384 B (171.2 MiB) and transcribes correctly with zero model-file opens.
  Devdoc §7 also cited a nonexistent `memfd_main.go`; the real paths are
  `memfd/main.go` / `memfdelf/main.go`. Lesson recorded: a build matrix is only as
  trustworthy as its gate covering the row the conclusion rests on.

## Unreleased — every dispatched subcommand is discoverable from `gopi -h`

### Added
- **`gopi voice_input` is documented in `gopi -h`** (`internal/config/help.go`)
  - The speech-to-text feature existed and was dispatched
    (`cmd/agent/cli_voice.go` ↔ `main.go`), but the top-level help never named
    it, so the feature could not be found — or tested — from the CLI. It is now
    listed in all three real shapes (`gopi voice_input <audio-file> [--chunk N]`,
    `--server --listen :7788`, `--server --stdio`), with the engine
    (Fun-ASR-Nano), the `--models`/`--runtime` overrides, the HTTP contract
    (`POST /transcribe` → `{"text":…}`), the `--chunk` measurement that makes it
    non-optional for long audio, and the reason it is a **CLI verb only, not an
    agent tool** (transcription happens before the model sees anything; the user
    reviews the text first). A `gopi voice_input talk.wav` example was added too.
- **Seven more undiscoverable verbs documented** — generalizing the guard (below)
  showed they were equally invisible: `chat`, `brew`, `memsync`, `proxy`,
  `screen_capture`, `act`, `scratch-gc`. Each entry mirrors that subcommand's own
  `-h` text.
- **`voice_input` joins the tab-completion candidate set**
  (`cmd/agent/cli_completion.go`) — `gopi <TAB>` now offers the verb.

### Fixed
- **A subcommand could ship invisible** — the per-feature help guards are written
  one feature at a time, so anything added without its own guard reached users
  with no help entry. `TestTopLevelHelpCoversEveryDispatchedSubcommand`
  (`cmd/agent/usage_test.go`) now derives the predicate list from `main.go`'s
  dispatch chain and requires a `gopi <verb>` entry in `gopi -h` for EVERY one
  of them (and the reverse: every completable verb must be explained) — an
  unmapped predicate fails the build, forcing a deliberate documentation
  decision rather than closing the class by hand a third time.

## Unreleased — raw log is session-scoped (fix: same-second filename collision)

### Fixed
- **Two agents starting in the same wall-clock second shared ONE raw log**
  (`internal/agent/rawlog.go` `createRawLogFile`, `internal/gopilog/rawlog.go`)
  - The raw log name is `gopi-raw-YYYYMMDD-HHMMSS.log` (second-granularity wall
    clock) and was opened `O_CREATE|O_APPEND`. Two gopi processes started inside
    one second — parallel `task run`/worker children, an epic fan-out — resolved
    the same name and both appended, so one file held two sessions' HTTP traffic
    with no boundary between them.
  - Consequence the user sees: `gopi raw <id> 1 request` returned whichever
    session sent first — **another session's first request**, with the reading
    session's own prompt/epic goal nowhere in that round. Observed on real logs:
    sessions `77d67d9b` and `044938be` both recorded
    `gopi-raw-20260827-231907.log` and each read the other's round 1 (2 of 487
    session/raw pairs on this machine).
  - Fix: the file is now created `O_EXCL`. The winner keeps the bare name; the
    loser retries `…-1.log`, `…-2.log`, … then the collision-proof
    `…-u<pid>-<n>.log` form, so creation never has to trade "share a file"
    against "lose the log". One raw log file now always belongs to one session,
    which is what makes "round N of my raw log" a meaningful statement.
  - `gopilog` accepts the suffixed names everywhere it parses them
    (`rawLogNameRe`, timestamp pairing, `listRawLogFiles` ordering); two logs
    sharing one embedded second are reported as an ambiguous timestamp match
    instead of silently guessed at.

### Added
- **`gopi raw` says so when a raw log is shared by several sessions**
  (`cmd/agent/cli_raw.go` `warnSharedRawLog`, `gopilog.SessionsRecordingRawLog`)
  - Logs written before the exclusive-create fix cannot be un-interleaved. When
    a session's raw log is recorded by more than one `session_start`, the raw
    view now prints one stderr line naming the other session(s) and stating the
    consequence (round numbers are NOT session-scoped). stdout stays byte-pure.
    The scan reads one bounded line per session file (~16 ms over 1202 sessions).
- **`gopi raw <id> --userprompt`** — the user's own input as sent
  (`internal/gopilog/rawlog.go` `FirstUserMessage` / `FormatRequestUserPrompt`)
  - Symmetric with `--sysprompt`: prints the first request's `role="user"`
    message text, no header, pipe-friendly. It answers "how do I get my prompt
    back out of a 50 KiB request line" without leaving the byte view.
  - Definition note in `gopi raw -h`: a request is the HTTP request message —
    the full JSON body — and the user's prompt is not a separate protocol
    artifact but `messages[0]` of it. `--userprompt` therefore prints the block
    as sent, with any directive the agent appended to it included (the wire does
    not separate them from what was typed).

## Unreleased — CLI subcommand groups (Epic: gopi memory <sub>, gopi web search|fetch)

### Added
- **CLI subcommand groups replace flat tool verbs** (`cmd/agent/cli_memory.go`,
  `cmd/agent/cli_web.go`, `cmd/agent/cli_websearch.go`,
  `cmd/agent/cli_webfetch.go`, `internal/config/help.go`)
  - The CLI surface now groups tool families under one first-level verb —
    `gopi memory search|store|get|update|delete|feedback|sync` and
    `gopi web search|fetch` — matching the `gopi task <sub>` convention.
    Tab-completion friendly: `gopi <TAB>` pops first-level verbs, then
    `gopi memory <TAB>` pops the second level (memorySubcommandNames).
  - The legacy flat verbs (`gopi memory_search`, `gopi web_fetch`, …) keep
    working as thin aliases that print a one-line deprecation note on
    stderr pointing at the grouped form — no script breakage (Rule 8
    lesson: alias, never remove).
  - **Dead-help bug fixed**: `gopi memory_feedback_list` (advertised in
    `gopi -h` but never dispatched — it fell through to the agent loop)
    now routes to the audit subcommand.
  - **Agent-loop fallthrough fixed**: a bare `gopi memory` (no subcommand)
    or `gopi web <unknown>` now prints usage + exit 1 instead of starting
    an LLM session.
  - `gopi web` (bare or server flags) still starts the web chat server;
    only `web search` / `web fetch` route to the tools.
  - Top-level `gopi -h` advertises the grouped forms; usage_test now
    asserts the flat verbs are NOT advertised.

### Added
- **`gopi log --git` filter for `git_blame` toolcalls**
  (`cmd/agent/cli_log.go`, `internal/gopilog/formatter.go`)
  - The log reader can now filter the new `git_blame` agent tool (git
    history mimic of gitlens/gitblame): `gopi log <id> --git` shows only
    git_blame calls. The tool appears in `toolFamilies`, `familyOrder`,
    help (`gopi log -h`), and the no-match hint — so its LLM usage is
    studyable/debuggable.
  - Full-session and filtered views render git_blame with a readable
    summary (`[N] git_blame: <file> (lines A-B)` + first result line)
    instead of a raw `%v` Args dump.
- **`gopi log` tool-family shortcuts + `--grep` content search**
  (`cmd/agent/cli_log.go`, `internal/gopilog/formatter.go`)
  - The log reader now mirrors the agent's full tool inventory
    (`internal/agent/loop.go`): new shortcut flags `--search`
    (code_search, keyword_search), `--web` (web_search, web_fetch),
    `--job` (job_start, job_output, job_kill) and `--context`
    (context_trim), joining the existing `--memory` family. A single
    `toolFamilies` registry drives expansion, so a toolcall added to the
    agent loop becomes log-filterable with a one-line addition.
  - `--grep TEXT` adds content search over a session (case-insensitive) —
    the log-side analogue of the agent's keyword_search: it renders every
    tool call / text response / thinking block containing the text, and
    combines with the family flags (`gopi log <id> --grep x --job`) to
    scope the match to a tool family. Works with `--last N` too.
  - Chat view now renders search-family tools with their query/URL and a
    compact result summary instead of a raw `%v` dump.
  - Help (`gopi log -h`) documents every family and the filterable tools;
    the no-match hint lists the new shortcuts.
- **Value-signal ranking for `memory_search`** (`internal/tools/memory.go`)
  - Candidates are now rescored instead of ordered by raw BM25: primary key
    is query-token coverage (`tokens=N` — how many distinct meaningful query
    terms appear in topic+question+answer), then tier (hot +2 / cold −6),
    the `possibly_stale` penalty, then confidence; BM25 `fts.rank` is the
    tiebreak. The `tokens=` signal is rendered in the result meta so a user
    can spot low-quality rows.
  - Fixes the low-quality result pattern of `memory_search "deepseek v4 flash
    price adjustment"`: a row that merely repeats rare tokens no longer
    outranks one that answers more of the query.
- **Cold tier is now live** — `tier` was display-only; now `cold` rows are
  excluded from tier-1 results and only surface when no warm/hot row matches
  (last-resort bucket). Demoting a low-quality row (`memory_update --tier
  cold`) makes it stop being 1st tier to return without deleting the
  knowledge.
- **Maintenance CLI subcommands** (`cmd/agent/cli_memory.go`):
  - `gopi memory_get <id>` — inspect one record in full.
  - `gopi memory_update <id> [--tier hot|warm|cold] [--topic/--question/
    --answer/--source/--confidence/--keywords]` — edit or demote; `--keywords`
    rewrites both the `search_kw` column and the `keywords:` answer tail
    (tail is stripped, never duplicated).
  - `gopi memory_delete <id>` — remove a record and its FTS index entries
    (explicit cleanup for foreign DBs without keep-in-sync triggers).
  - All three share the engine functions (`tools.MemoryGet/Update/Delete`),
    accept `--id N` or a positional id, and are advertised in `gopi -h`.
- **`memory_update` LLM tool** (`internal/agent/loop.go`) — the memory tool
  family is now complete: `memory_search` (read) / `memory_store` (write) /
  `memory_update` (edit). The tool dispatches to the same engine function as
  the CLI (`tools.MemoryUpdate`) — tool and CLI behavior identical by
  construction. Trust boundary: only records the task retrieved may be
  edited; `validateToolArgs` enforces `id` required + at least one editable
  field. Guidance in the description steers the conservative ladder (demote
  to cold > fix keywords > rewrite answer) and forbids deletion (stays a
  manual CLI decision).
- **Devdoc**: `devdoc/memory-lowquality-retrieval-evaluation.md` — root-cause
  analysis of the low-quality retrieval example (inert tier, BM25-only
  ranking, AND→OR intent drop, garbage imported keywords) + the resolution.

## Unreleased — CLI help & unified configuration

### Added
- **Full `gopi -h` and `gopi --help-env`** (`internal/config`, `cmd/agent/main.go`)
  - The top-level help now covers every usage form, a grouped table of all
    ~35 env vars each paired with its `settings.json` key and CLI flag, all
    subcommands (including the integrated `gopi log` reader), and use-case
    examples. `gopi --help-env` prints the complete grouped table alone.
  - The help is rendered from a single `config.EnvVars` catalog, so the docs
    can never drift from the knobs the code actually honours.
- **Three interchangeable ways to configure every knob** — precedence
  `CLI flag > env var > settings.json > default`:
  - `settings.json` now accepts snake_case keys mirroring every env var
    (`timeout`, `thinking`, `max_tokens`, `compact_keep_recent`, `memory_db`,
    `raw_log`, ...), not just `api_key`/`base_url`/`model`.
  - Global CLI flags for every knob: `--timeout`, `--thinking`, `--compact`,
    `--max-tokens`, `--reasoning-effort`, `--tool-timeout`, `--raw-log`,
    `--db`, ... Settings only fill env vars that are unset; flags always win.

## 2026-08-20

### Added
- **Parallel execution of read-only tool calls** (feat/parallel-claude, devdoc/parallel-tool-execution.md)
  - A round with multiple tool calls whose names are ALL whitelisted (`read`, `code_search`, `keyword_search`) now executes them concurrently (one goroutine per call + `sync.WaitGroup`) instead of serially. Wall-clock latency for the round drops from the sum of call latencies to the max.
  - Results are collected into an index-keyed slice, so they are sent back to the model in tool-call order regardless of completion order.
  - The whole parallel batch is bounded by one timeout (`tools.FileToolTimeout()`, default 30s / `GOPI_TOOL_TIMEOUT_SEC`): a timed-out call gets an error result while the others still return normally.
  - Mixed rounds (any `write`/`edit`/`bash` present) and single-call rounds keep the existing serial path — side-effect tools never overlap.
  - Batch observability: per-call duration and batch total are logged via the agent logger.
  - Entry point is `executeToolBatch()` in `internal/agent/loop.go`, used by both `Run()` and `runNonStreaming()`; `executeTool` gained a test-only hook (`executeToolFn`) so concurrency/timeout behavior is testable deterministically.
  - Tests: single-call serial regression, concurrent execution with order preservation, mixed-round serial fallback, batch timeout with other calls returning, plus a full-loop two-read end-to-end test (`internal/agent/loop_test.go`).

### Changed
- **`code_search` split into `code_search` + `keyword_search`** (feat/search-split, devdoc/search-tool-split.md)
  - The single tool with a `mode` param (symbol/refs/text) is gone. Routing now lives in the planning layer: `code_search` = ctags symbol/definition lookup, `keyword_search` = ripgrep full-text over any file (code, docs, config, scripts).
  - Both tools return structured results, not bare strings: `CodeSearch(scope, symbolName) ([]CodeSymbolHit, error)` and `KeywordSearch(scope, query) ([]TextHit, error)` with a shared `SearchScope` (root, include/exclude globs, result cap). Contract types live in `internal/tools/search.go`.
  - `CodeSearch` falls back to ripgrep on a clean ctags miss (hits marked kind `reference`), so an existing symbol never comes back empty; total miss returns an empty result with no error.
  - ctags index now generated with `--fields=+n` so hits carry the definition line number; `formatTagEntry` returns a structured `CodeSymbolHit` (name, kind, file:line, signature from the ctags pattern field).
  - Agent layer: two ToolDefs (`code_search` requires `symbol`, `keyword_search` requires `query`), matching `executeTool`/`validateToolArgs` cases, system prompt tool list updated.
  - Tests: structured hit shape, absolute root, fallback path, globs, regex, `MaxResult` cap, empty-query guards, formatters (`internal/tools/search_test.go`); agent-layer validation + execution tests (`internal/agent/loop_test.go`).

## 2026-08-05

### Fixed
- **Deadlock on daemon-spawning bash commands: process-group isolation + group kill** (bug011/bug012, `internal/tools/bash.go`)
  - Production hang: gopi hung 2h34m on `nohup ./bin/scheduler daemon ... &` — the background GRANDCHILD inherited the stdout/stderr pipe write-ends, so after bash exited the read goroutines' `stdoutPipe.Read()` never returned EOF. The idle/absolute timeouts called `cmd.Process.Kill()`, which kills only bash — the daemon kept the pipes open and `<-readDone` blocked forever (the 60s absolute timeout was useless).
  - Fix: `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` isolates the command in its own process group; BOTH kill paths now signal the whole group with `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)`:
    - idle-output monitor (was `cmd.Process.Kill()`)
    - absolute timeout — a new ctx-watcher goroutine group-kills when `ctx.Done()` fires with `context.DeadlineExceeded` (exec.CommandContext's internal kill only targets bash; the watcher complements it and exits via an `execDone` channel on normal return — no goroutine outlives bash())
  - Result: the daemon dies with the group, pipes close, read goroutines get EOF, `bash()` returns promptly with the timeout error.
  - New regression tests (`internal/tools/bash_daemon_test.go`): daemon-spawn returns within window (idle-first AND absolute-first), no process leak (pid-file `kill(pid,0)` + /proc scan), foreground sanity. Verified the tests hang on the pre-fix code (reproduce the deadlock in the stdout read goroutine) and pass with the fix.
- **Premature-exit loophole: exit decision now uses heartbeat task state, not the current round's parse** (bug010, 4b9b1a3)
  - The ReAct loop previously trusted the CURRENT round's parsed task list — which is `nil` whenever the LLM omits `[TASK_LIST_BEGIN]...[TASK_LIST_END]` from an intermediate response (DeepSeek's learned token-saving behavior). GoPi could exit with unfinished tasks simply because the model skipped printing the list that round.
  - Both exit gates now consult `hb.CurrentTasks()` (the authoritative accumulated state):
    - All-completed early exit: `len(stateTasks) > 0 && AllCompleted(stateTasks) && len(toolCalls) == 0`
    - No-tool-calls path: `len(stateTasks) > 0 && !AllCompleted(stateTasks)` → inject the existing incomplete-tasks reminder rendered with the FULL accumulated list (`FormatTaskList(stateTasks)`) and continue
  - `len(stateTasks) == 0` (no list ever established — legal for reasoning-only tasks) or all-completed → final answer. First-round pure-reasoning replies still exit cleanly.
  - The model may omit the list; the framework no longer cares (does NOT resurrect the reverted 5f45973 per-round enforcement).
- **Task-list parse/validation errors no longer swallowed** (bug010, 4b9b1a3)
  - Malformed task-list JSON (markers present, bad JSON): logged to stderr, one-line corrective user message injected, loop continues
  - Validation violations (e.g. task deletion): logged; deletion specifically injects a restore-all-task_ids corrective message and continues; the violating list is never written to the heartbeat
  - Repeated violator: `maxTaskListViolations` (3) consecutive rounds abort the run with an error
  - Legitimate list omission (`nil,nil`) is never corrected

### Added
- **Live thinking display** (bug008, 40ff546)
  - DeepSeek `reasoning_content` now streams to stderr in real time instead of being swallowed
  - Thinking is visually separated from the final answer: ANSI dim `[thinking] ` on TTYs, plain `[thinking] ` prefix when piped/redirected
  - `GOPI_HIDE_THINKING=1` suppresses the live display (thinking mode + session-log capture stay on)
  - Session-log `LogAssistantThinking` write preserved (streaming + fallback paths)

### Changed
- **Thinking mode enabled by default** (bug008, 40ff546)
  - All request paths now send `thinking: {"type":"enabled"}` when `GOPI_THINKING` is unset (was nil = model default = OFF)
  - `GOPI_THINKING=disabled` still opts out; invalid values warn and fall back to enabled
  - Applies uniformly to Run(), runNonStreaming() and the Chat() fallback (single default in NewAgent)

## 2026-08-03

### Added
- **Reasoning capture in session logger** (24c9e60, b039594)
  - `LogAssistantThinking()` captures DeepSeek `reasoning_content` as thinking events
  - Thinking blocks logged before text/tool_use in assistant messages
  - Streaming and non-streaming paths both supported
  - Format: `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"..."}]}}`
  - Aligned with Claude JSONL session format

### Fixed
- **All four timeout sources removed** (c94d766, 5b12eaf, 93444ba)
  - `NewAgent()` default 300s → 0 (unlimited)
  - `Run()` hardcoded 5min → GOPI_TIMEOUT_SEC
  - `runNonStreaming()` 5min → unlimited
  - HTTP client 120s → 0 (no HTTP-level timeout)
  - Single deadline source now: agent context (GOPI_TIMEOUT_SEC, default 0)

### Added
- **Kernel system prompt hardcoded** (4872856)
  - `"You are GoPi. Tools: read, write, edit, bash."` — immutable
  - No env override, no user configuration
  - Model decides strategy, prompt only lists tools

### Added
- **Leveled logging** (b7d3bf5)
  - INFO/WARN/ERROR with full command display
  - Default always-on, GOPI_LOG_LEVEL=OFF to disable

### Added
- **Session system with JSONL logging** (f4b0681)
  - UUID v4 session IDs
  - ~/.gopi/sessions/<id>.jsonl with 6 event types
  - Thread-safe, fsync after every write

### Added
- **Settings file support** (29e848a)
  - Read API key from ~/.gopi/settings.json
  - Resolution: env > settings.json > default

### Added
- **GOPI.md coding rules** (2fe0e0f, 36b349a)
  - 6 rules including: no magic numbers, minimal tools, git clone isolation
