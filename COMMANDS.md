# GoPi Commands — the unified manual for the `gopi` CLI

One page for **every** command the `gopi` binary dispatches: what each verb is
for, which one answers a given wish, and where in the source each fact comes
from.

The page is derived from the source, not from memory: every row and every claim
below names the file and line that decides it. If a row here disagrees with the
code, the code wins — and the disagreement is a bug in this file (see
[Keeping this page honest](#6-keeping-this-page-honest)).

The three properties this page shares with its sibling [CONFIG.md](CONFIG.md):

- **The fact comes from the source**, never from a remembered list.
- **Every claim carries a `file.go:<symbol>` anchor** that must resolve at the
  commit it lands in — a line number where the reference really is about a line
  (see [Keeping this page honest](#6-keeping-this-page-honest)).
- **A machine-checked generator refuses block drift**
  (`internal/standards/testdata/commands-census.go`): the generated block below
  must match the source byte for byte, and `--check` exits 1 on any delta. The
  prose half — no invented verb, no dropped verb, every anchor resolving — has
  no gate behind it today; §6 says what was retired and when.

- Sibling document of record: [CONFIG.md](CONFIG.md) — every knob (env var,
  settings key, flag, precedence).
- Operator-facing twin: `gopi -h` (`internal/config/help.go:531`) — the index
  a user reads first; this page is the manual behind it.
- Generated twin: the block in [§2](#2-the-verb-index-generated) is written by
  `go run internal/standards/testdata/commands-census.go . COMMANDS.md`
  (`internal/standards/testdata/commands-census.go:48`).

---

## 0. What a command is here

Every verb answers to the same three-word contract: **`gopi <verb> -h`** prints
that verb's own page and exits 0. The top-level help is an index, not the
manual (`internal/config/help.go:531`); the detail lives on the verb's page.
One run of `gopi` that is not a verb is a *task*: the prompt path that starts an
agent session (`gopi [options] "<prompt>"`).

**Where the vocabulary lives.** The top-level verb words are constants in one
block — `cmd/agent/cli_verbs.go:46` — and each constant's comment records *why*
the verb exists (the incident that created it, or the surface it replaced).
That block is the single source of truth (`cmd/agent/cli_verbs.go:46`); the
dispatch predicates, the ssh-passthrough scan and the contract suite's token map
all reference those constants rather than re-typing the word. Two verbs keep
their spelling in a leaf package because a second package must name them
(Rule 14): `gopi memory` is `tools.CliVerbMemoryGroup`
(`internal/tools/ledger_backup.go:88`) and `gopi version` is
`cliverbs.Version` (`internal/cliverbs/cliverbs.go:Version`). One
verb — the `act` tool command, reached as
`gopi tool act` — declares its constant beside itself
(`cmd/agent/cli_act.go:38`).

**Which verbs exist is checked twice.** `dispatcherOfToken`
(`cmd/agent/cli_contracts_test.go:230`) maps each dispatched first token to the
`isXxxSubcommand` predicate that routes it, and
`TestCLIUserVerbTokensAreAllFromCliVerbs` (`cmd/agent/cli_contracts_test.go:361`)
fails when a constant has no contract entry or an entry names no constant. The
generated index in [§2](#2-the-verb-index-generated) joins exactly those two
sources (`internal/cliindex/cliindex.go:167`), so the document cannot carry a list
nobody checked.

**Spelling rule.** A command that mirrors a *tool* keeps the tool's underscore
spelling (`gopi tool screen_capture`, `gopi tool vision_analyze`,
`gopi tool text2img`) while a verb of the CLI's own is hyphenated
(`gopi tool code-search`, `gopi tool scratch-gc`, `gopi tool ledger-window`,
`gopi tool text2img-verify`). The exception `gopi ui_state` is
underscored on purpose: it is the tool-shaped spelling of a surface the agent
reaches through `bash` (`cmd/agent/cli_verbs.go:CliVerbUIState`). A tool name that is *not*
a command word is refused by name, not silently sent to the model
(`cmd/agent/cli_undispatched_tool_words.go:undispatchedToolWordRefusalReason`).

---

## 1. The machine truth behind this page

| what | where the machine says it |
|---|---|
| the verb words | `cmd/agent/cli_verbs.go:46` (the `CliVerbXxx` block; `cmd/agent/cli_act.go:38` for `act`) |
| the routing | `cmd/agent/cli_contracts_test.go:230` (`dispatcherOfToken`), predicates in the `isXxxSubcommand` functions |
| the `-h` pages | `cmd/agent/cli_contracts_test.go:68` (`cliContractInventory`, one fingerprint per page) |
| the top-level index | `internal/config/help.go:531` (`FormatUsage`) |
| the dispatch chain | `cmd/agent/main.go:135` and the `isXxxSubcommand` sites in `cmd/agent/cli_*.go` |
| the reader the gate and the generator share | `internal/cliindex/cliindex.go:167` |
| the `argv[0]?` level of a word — the address a refused flat spelling is answered with | `internal/config/cli_verb_guard.go:aliasAddresses` (`aliasAddresses`, exposed as `AliasAddresses()`, `internal/config/cli_verb_guard.go:AliasAddresses`) |

---

## 2. The verb index (generated)

The table below is **generated from the source** — do not hand-edit it. Its
`argv[0]?` column states **where the word is reachable**: `argv[0]` when the
word is its own top-level spelling, or `` routed only `gopi <address>` `` when
the binary REFUSES the flat spelling at argv[0] and answers it with that
address. The level is not a second opinion — it is read from the table the
refusal itself prints its address from (`config.AliasAddresses()`,
[§1](#1-the-machine-truth-behind-this-page)), so the document and the refusal
cannot disagree about where a word lives. Its "routed by" column is the
predicate and line that dispatch the word, and its "spelling owned by" column is
the constant and line that define it.

Two rows marked `argv[0]` still print a retirement when run rather than a live
command: the flat `gopi web_search` / `gopi web_fetch` (§3.5). Their own
spelling dispatches — the
`argv[0]` column answers *is this word routed*, not *does its page do work*.

<!-- BEGIN GENERATED: verb-index -->
| verb | argv[0]? | routed by | spelling owned by |
|---|---|---|---|
| `gopi act` | routed only `gopi tool act` | `cmd/agent/cli_act.go:isActSubcommand` | `CliVerbAct` (`cmd/agent/cli_act.go:CliVerbAct`) |
| `gopi annotate` | routed only `gopi tool annotate` | `cmd/agent/cli_annotate.go:isAnnotateSubcommand` | `CliVerbAnnotate` (`cmd/agent/cli_verbs.go:CliVerbAnnotate`) |
| `gopi archive` | routed only `gopi task archive` | `cmd/agent/cli_archive.go:isArchiveSubcommand` | `CliVerbArchive` (`cmd/agent/cli_verbs.go:CliVerbArchive`) |
| `gopi brew` | routed only `gopi memory brew` | `cmd/agent/cli_brew.go:isBrewSubcommand` | `CliVerbBrew` (`cmd/agent/cli_verbs.go:CliVerbBrew`) |
| `gopi capture` | routed only `gopi tool capture` | `cmd/agent/cli_capture.go:isCaptureSubcommand` | `CliVerbCapture` (`cmd/agent/cli_verbs.go:CliVerbCapture`) |
| `gopi chat` | argv[0] | `cmd/agent/cli_chat.go:isChatSubcommand` | `CliVerbChat` (`cmd/agent/cli_verbs.go:CliVerbChat`) |
| `gopi checkpoint` | routed only `gopi session checkpoint` | `cmd/agent/cli_checkpoint.go:isCheckpointSubcommand` | `CliVerbCheckpoint` (`cmd/agent/cli_verbs.go:CliVerbCheckpoint`) |
| `gopi cli` | argv[0] | `cmd/agent/cli_term.go:isTermSubcommand` | `CliVerbCLI` (`cmd/agent/cli_verbs.go:CliVerbCLI`) |
| `gopi code-search` | routed only `gopi tool code-search` | `cmd/agent/cli_code_search.go:isCodeSearchSubcommand` | `CliVerbCodeSearch` (`cmd/agent/cli_verbs.go:CliVerbCodeSearch`) |
| `gopi completion` | routed only `gopi config completion` | `cmd/agent/cli_completion.go:isCompletionSubcommand` | `CliVerbCompletion` (`cmd/agent/cli_verbs.go:CliVerbCompletion`) |
| `gopi config` | argv[0] | `cmd/agent/cli_config.go:isConfigSubcommand` | `CliVerbConfig` (`cmd/agent/cli_verbs.go:CliVerbConfig`) |
| `gopi gif` | routed only `gopi tool gif` | `cmd/agent/cli_gif.go:isGifSubcommand` | `CliVerbGif` (`cmd/agent/cli_verbs.go:CliVerbGif`) |
| `gopi keyword_search` | routed only `gopi tool keyword_search` | `cmd/agent/cli_keyword_search.go:isKeywordSearchSubcommand` | `ToolKeywordSearch` (`internal/toolnames/names.go:ToolKeywordSearch`) |
| `gopi ledger-window` | routed only `gopi tool ledger-window` | `cmd/agent/cli_ledger_window.go:isLedgerWindowSubcommand` | `CliVerbLedgerWindow` (`internal/cliverbs/cliverbs.go:LedgerWindow`) |
| `gopi log` | routed only `gopi session log` | `cmd/agent/cli_log.go:isLogSubcommand` | `CliVerbLog` (`cmd/agent/cli_verbs.go:CliVerbLog`) |
| `gopi memory` | argv[0] | `cmd/agent/cli_memory.go:isMemorySubcommand` | `CliVerbMemory` (`internal/tools/ledger_backup.go:CliVerbMemoryGroup`) |
| `gopi memsync` | routed only `gopi task dedup-scan` | — | `CliVerbMemsync` (`cmd/agent/cli_verbs.go:CliVerbMemsync`) |
| `gopi merger` | argv[0] | `cmd/agent/daemon_retired.go:isMergerSubcommand` | `CliVerbMerger` (`cmd/agent/cli_verbs.go:CliVerbMerger`) |
| `gopi notify` | argv[0] | `cmd/agent/cli_notify.go:isNotifySubcommand` | `CliVerbNotify` (`cmd/agent/cli_verbs.go:CliVerbNotify`) |
| `gopi proxy` | routed only `gopi tool proxy` | `cmd/agent/cli_proxy.go:isProxySubcommand` | `CliVerbProxy` (`cmd/agent/cli_verbs.go:CliVerbProxy`) |
| `gopi raw` | routed only `gopi session raw` | `cmd/agent/cli_raw.go:isRawSubcommand` | `CliVerbRaw` (`cmd/agent/cli_verbs.go:CliVerbRaw`) |
| `gopi resume` | routed only `gopi session resume` | `cmd/agent/cli_resume.go:isResumeSubcommand` | `CliVerbResume` (`internal/cliverbs/cliverbs.go:ResumeLeaf`) |
| `gopi sandbox` | routed only `gopi tool sandbox` | `cmd/agent/cli_sandbox.go:isSandboxSubcommand` | `CliVerbSandbox` (`cmd/agent/cli_verbs.go:CliVerbSandbox`) |
| `gopi scratch-gc` | routed only `gopi tool scratch-gc` | `cmd/agent/cli_scratch_gc.go:isScratchGCSubcommand` | `CliVerbScratchGC` (`internal/cliverbs/cliverbs.go:ScratchGC`) |
| `gopi screen_capture` | routed only `gopi tool screen_capture` | `cmd/agent/cli_screencapture.go:isScreenCaptureSubcommand` | `CliVerbScreenCapture` (`cmd/agent/cli_verbs.go:CliVerbScreenCapture`) |
| `gopi session` | argv[0] | `cmd/agent/cli_session.go:isSessionSubcommand` | `CliVerbSession` (`internal/cliverbs/cliverbs.go:Session`) |
| `gopi setup` | routed only `gopi config setup` | `cmd/agent/cli_setup.go:isSetupSubcommand` | `CliVerbSetup` (`cmd/agent/cli_verbs.go:CliVerbSetup`) |
| `gopi shot` | routed only `gopi tool shot` | `cmd/agent/cli_shot.go:isShotSubcommand` | `CliVerbShot` (`internal/cliverbs/cliverbs.go:Shot`) |
| `gopi stats` | routed only `gopi session stats` | `cmd/agent/cli_stats.go:isStatsSubcommand` | `CliVerbStats` (`cmd/agent/cli_verbs.go:CliVerbStats`) |
| `gopi task` | argv[0] | `cmd/agent/cli_tasks.go:isTasksSubcommand` | `CliVerbTask` (`cmd/agent/cli_verbs.go:CliVerbTask`) |
| `gopi text2img` | routed only `gopi tool text2img` | `cmd/agent/cli_text2img.go:isText2imgSubcommand` | `ToolText2img` (`internal/toolnames/names.go:ToolText2img`) |
| `gopi text2img-verify` | routed only `gopi tool text2img-verify` | `cmd/agent/cli_text2img_verify.go:isText2imgVerifySubcommand` | `CliVerbText2imgVerify` (`internal/cliverbs/cliverbs.go:Text2imgVerify`) |
| `gopi tokens` | routed only `gopi tool tokens` | — | `CliVerbTokens` (`cmd/agent/cli_verbs.go:CliVerbTokens`) |
| `gopi tool` | argv[0] | `cmd/agent/cli_tool.go:isToolSubcommand` | `CliVerbTool` (`cmd/agent/cli_verbs.go:CliVerbTool`) |
| `gopi ui` | argv[0] | `cmd/agent/cli_ui.go:isUISubcommand` | `CliVerbUI` (`cmd/agent/cli_verbs.go:CliVerbUI`) |
| `gopi ui_state` | argv[0] | `cmd/agent/cli_uistate.go:isUIStateSubcommand` | `CliVerbUIState` (`cmd/agent/cli_verbs.go:CliVerbUIState`) |
| `gopi version` | argv[0] | `cmd/agent/cli_version.go:isVersionSubcommand` | `CliVerbVersion` (`internal/cliverbs/cliverbs.go:Version`) |
| `gopi vision_analyze` | routed only `gopi tool vision_analyze` | `cmd/agent/cli_vision.go:isVisionSubcommand` | `ToolVisionAnalyze` (`internal/toolnames/names.go:ToolVisionAnalyze`) |
| `gopi voice_input` | routed only `gopi tool voice_input` | `cmd/agent/cli_voice.go:isVoiceSubcommand` | `ToolVoiceInput` (`internal/toolnames/names.go:ToolVoiceInput`) |
| `gopi web` | argv[0] | `cmd/agent/cli_web.go:isWebSubcommand` | `CliVerbWeb` (`cmd/agent/cli_verbs.go:CliVerbWeb`) |
| `gopi web_fetch` | argv[0] | `cmd/agent/cli_webfetch.go:isWebFetchSubcommand` | `ToolWebFetch` (`internal/toolnames/names.go:ToolWebFetch`) |
| `gopi web_search` | argv[0] | `cmd/agent/cli_websearch.go:isWebSearchSubcommand` | `ToolWebSearch` (`internal/toolnames/names.go:ToolWebSearch`) |
| `gopi worker` | argv[0] | `cmd/agent/main.go:isWorkerSubcommand` | `CliVerbWorker` (`internal/cliverbs/cliverbs.go:Worker`) |
<!-- END GENERATED: verb-index -->

Regenerate with:

```bash
go run internal/standards/testdata/commands-census.go . COMMANDS.md
go run internal/standards/testdata/commands-census.go . COMMANDS.md --check   # report only, exit 1 on drift
```

---

## 3. The verbs, by purpose

Each entry names the verb, what it is for, and the line that decides it; the
first line quoted in a description is the verb's own `-h` title, which
`gopi <verb> -h` prints. A verb whose page says it is deprecated still routes
(it exists for scripts that spell it), and the entry says so.

**A word whose §2 row reads `routed only` is reached through the family address
printed there** — `gopi session log`, `gopi config setup`, `gopi tool capture`,
`gopi task archive`. The flat spelling it retired (`gopi log`, `gopi setup`,
`gopi capture`) is refused at argv[0] with a `` `X` is RETIRED — use `gopi …` ``
line, so the entries below name the address that runs.

### 3.1 Looking at what happened — sessions, logs, ledger

- **`gopi session log`** — "GoPi JSONL session log reader"
  (`cmd/agent/cli_log.go:616 runLogSubcommand`).
  The session record: what a run did, round by round. `--last` reads the
  most recent one. `--rounds` tabulates the session's **rounds** — one JSONL
  `request` row = one HTTP request/response = one iteration, the SAME number
  `gopi session replay <id> --rounds` prints as `rounds=`, `gopi session stats`
  reports and the log's own iteration count names — with the raw-log ordinal
  and byte span each round pairs with; `--turns` tabulates the neighbouring
  quantity (one OPERATOR row to the next, with the rounds that turn took).
- **`gopi session stats`** — "GoPi session statistics (numbers, not content)"
  (`cmd/agent/cli_stats.go:218`). Rounds, tokens, cache hit, cost.
- **`gopi session raw`** — "GoPi raw HTTP log, byte for byte (verbatim)"
  (`cmd/agent/cli_raw.go:384`). The wire traffic behind those rounds.
- **`gopi session resume`** — resume an interrupted session
  (`cmd/agent/cli_resume.go:93`).
- **`gopi session checkpoint`** — "GoPi checkpoint & resume subcommands" — continue from
  a raw-log checkpoint, every `→ REQUEST` line being a byte-exact context
  snapshot (`cmd/agent/cli_checkpoint.go:99`).
- **`gopi session`** — "GoPi session subcommands — inspect and continue sessions"
  (`cmd/agent/cli_session.go:290`): a GROUP verb (list/show) whose log/stats/raw/
  resume arms are thin aliases over the four verbs above, so the family page
  promises the flags are that verb's own. `list` renders each chat's **source**
  — who held the console when the row was written — and `--source WHO` filters
  by it (`human`, `agent`, …); a row written before the column existed reads
  `(unrecorded)`, never as the operator's own.
- **`gopi task`** — "GoPi task subcommands — the task-as-memory ledger"
  (`cmd/agent/cli_tasks.go:isTasksSubcommand`). The cross-session ledger: list, show, submit,
  update. The ledger is an audit layer, not runtime control.
- **`gopi notify`** — "subscribe to the task-event notify stream"
  (`cmd/agent/cli_notify.go:notifySubVerbUsage`): `events` / `failed` / `sink`, with a caller-
  owned cursor. It adds no resident process — `--follow` is a foreground poll the
  caller ends (`cmd/agent/cli_notify.go:106`).

### 3.2 Deciding what is configured or installed

- **`gopi config`** — "inspect the configuration actually in force"
  (`cmd/agent/cli_config.go:94`): the effective value **with its provenance**.
  Its sibling is [CONFIG.md](CONFIG.md).
- **`gopi config setup`** — "bring this host to a run-ready state in ONE run"
  (`cmd/agent/cli_setup.go:77`): profile root, settings, sessions/, ledger — the
  zero-dependency replacement for a deployment shell script.
- **`gopi tool sandbox`** — the ONE standard agent sandbox, configured/private/
  reclaimable (`cmd/agent/cli_sandbox.go:230`): `create`, `show`, and `run`
  which materialises *and* executes inside it.
- **`gopi version`** — the build metadata reader
  (`internal/cliverbs/cliverbs.go:Version`).
- **`gopi config completion`** — "generate shell tab-completion scripts for the gopi
  CLI" (`cmd/agent/cli_completion.go:273`).

### 3.3 Running the model — chat surfaces, agents, prompts

- **`gopi chat`** — the interactive REPL carrier
  (`cmd/agent/cli_chat.go:runChatWithInput`); `--once` runs one turn with no terminal, and
  `--replay` / `--film` drive a recorded session.
- **`gopi cli`** — "chat in a TEXT-mode browser"
  (`cmd/agent/cli_term.go:107`), a second view over the same engine as
  `gopi web`.
- **`gopi ui`** — the terminal chat surface: scrolling transcript, live tool
  lines, input box (`cmd/agent/cli_ui.go:126`).
- **`gopi web`** — serve the local browser chat UI (`cmd/agent/cli_web.go:115`);
  `web search` / `web fetch` are the tool mirrors on the same group verb.
- **`gopi ui_state`** — read a live `gopi ui` screen as a cell grid, no pixels
  and no vision model (`cmd/agent/cli_uistate.go:391`). The reader of the dump
  the `gopi ui --dump-screen` sink writes.
- **`gopi memory brew`** — "distill reusable lessons from session logs into the
  knowledge base" (`cmd/agent/cli_brew.go:brewUsage`).
- **`gopi memory`** — the memory tool family as one subcommand group
  (`internal/tools/ledger_backup.go:88`): search / store / get / list / update /
  delete / feedback, plus the `backup` arm (the `sync` arm was deleted with the
  cross-node sync family, 2026-11-05 — see the retirement note on the family).
- **`gopi worker`** — "run the queued tasks, then exit. No daemon to babysit"
  (`cmd/agent/main.go:135`).

### 3.4 Seeing the machine — screen, pictures, input

- **`gopi tool capture`** — capture the LIVE X11 desktop to a PNG
  (`cmd/agent/cli_capture.go:133`): whole screen, current window, or a rectangle.
- **`gopi tool screen_capture`** — the tool-mirror spelling of the same engine
  (`cmd/agent/cli_screencapture.go:50`): the whole-screen form forwarding a mode
  flag, so the tool's own name stays a working command.
- **`gopi tool shot`** — screenshot a URL or gopi's own web chat UI
  (`cmd/agent/cli_shot.go:121`) — headless, so it never touches the real screen.
- **`gopi tool annotate`** — draw marks (rectangle, point, arrow, text) onto an
  EXISTING PNG, writing a NEW file (`cmd/agent/cli_annotate.go:170`).
- **`gopi tool gif`** — turn a PNG frame sequence into ONE animated GIF with words on
  each frame (`cmd/agent/cli_gif.go:124`) — the time dimension of the picture
  surface.
- **`gopi tool text2img`** — render a text file to PNG images
  (`cmd/agent/cli_text2img.go:123`), pure-Go TrueType.
- **`gopi tool text2img-verify`** — render the `知` fixture and print the
  font/size/pages used, writing the PNG for pixel-level (IoU) verification
  (`cmd/agent/cli_text2img_verify.go:54`).
- **`gopi tool vision_analyze`** — the vision tool as a command
  (`cmd/agent/cli_vision.go:43`): image → description/OCR.
- **`gopi tool act`** — drive the LIVE X11 desktop: move the pointer, click, type
  (`cmd/agent/cli_act.go:38`). The counterpart of `capture`: capture records,
  act changes.
- **`gopi tool voice_input`** — speech-to-text via Fun-ASR-Nano
  (`cmd/agent/cli_voice.go:21`); a CLI verb only, because transcription happens
  before the model sees anything (`internal/toolnames/names.go:147`).

### 3.5 Searching the code and the web

- **`gopi tool code-search`** — the `code_search` tool as a CLI command
  (`cmd/agent/cli_code_search.go:92`).
- **`gopi tool keyword_search`** — the `keyword_search` tool as a command
  (`cmd/agent/cli_keyword_search.go:106`).
- **`gopi web_search`** / **`gopi web_fetch`** — the tool mirrors
  (`cmd/agent/cli_websearch.go:105`, `cmd/agent/cli_webfetch.go:75`). Both flat
  spellings are RETIRED and refused by name (`gopi web_search: retired — use
  \`gopi tool web search\``); they do not route, and `-h` answers with the live
  page rather than running anything. Their dash twins `gopi web-search` /
  `gopi web-fetch` are refused the same way (a row of the spelling table in
  `cmd/agent/cli_undispatched_tool_words.go:undispatchedToolWordSpellings`), so no spelling of these two
  tools other than `gopi tool web search|fetch` is spent as a prompt. The record
  is `devdoc/web-tool-dash-twin-refusal.md`.

### 3.6 Housekeeping and plumbing

- **`gopi tool scratch-gc`** — "sweep the suite scratch home ONCE and exit (watchdog
  face)" (`cmd/agent/cli_scratch_gc.go:555`); the reclaimer rides ticks that
  already exist, and a `--dry-run` says what *would* go and why.
- **`gopi task archive`** — "archive a branch WITH a recorded reason (the
  decided-exclusion lane)" (`cmd/agent/cli_archive.go:133`). It exists because
  the same act done with a bare `git tag` buried nine branches with no reason
  and no ledger row (`cmd/agent/cli_verbs.go:52`).
- **`gopi tool ledger-window`** — two wall-clock bounds in, epoch seconds out
  (`cmd/agent/cli_ledger_window.go:77`); a separate program under `cmd/` until
  2026-10-02 (`cmd/agent/cli_verbs.go:150`).
- **`gopi tool proxy`** — the HTTP proxy upstream: `--upstream openai|anthropic`
  (`cmd/agent/cli_proxy.go:104`).

---

## 4. "I want to …" — which verb?

| I want to | use | why this one |
|---|---|---|
| see what a run actually did | `gopi session log --last` | the JSONL session record (`cmd/agent/cli_log.go:616`) |
| see what a run cost | `gopi session stats --last` | rounds/tokens/cost, not content (`cmd/agent/cli_stats.go:218`) |
| read the requests a run sent | `gopi session raw --last` | the verbatim HTTP log (`cmd/agent/cli_raw.go:384`) |
| continue an interrupted run | `gopi session resume <session-id>` | the resume surface (`cmd/agent/cli_resume.go:93`) |
| browse my past conversations | `gopi session list` | the family view over the chat index (`cmd/agent/cli_session.go:290`) |
| see the task ledger | `gopi task list` | the cross-session ledger (`cmd/agent/cli_tasks.go:isTasksSubcommand`) |
| find out what value a setting has right now | `gopi config show` | effective value with provenance (`cmd/agent/cli_config.go:94`) |
| know which knob controls X | [CONFIG.md](CONFIG.md), then `gopi --help-env` | the catalogue is the machine source |
| make a fresh machine run-ready | `gopi config setup` | provisions profile + ledger in one run (`cmd/agent/cli_setup.go:77`) |
| run a command in a throwaway private profile | `gopi tool sandbox run -- CMD` | materialises AND executes inside it (`cmd/agent/cli_sandbox.go:230`) |
| see which gopi build I have | `gopi version` | the build-metadata reader (`internal/cliverbs/cliverbs.go:Version`) |
| chat with the model in this terminal | `gopi chat` | the REPL carrier (`cmd/agent/cli_chat.go:runChatWithInput`) |
| chat with the model in a browser | `gopi web` | the local browser UI (`cmd/agent/cli_web.go:115`) |
| chat in a text-mode browser | `gopi cli` | the second view over that engine (`cmd/agent/cli_term.go:107`) |
| read a live terminal-UI screen without a vision model | `gopi ui_state` | the cell-grid reader of the dump (`cmd/agent/cli_uistate.go:391`) |
| run the queued tasks once | `gopi worker` | the queue runner, then exit (`cmd/agent/main.go:135`) |
| land the branches that fast-forward | `gopi task merge <branch>...` | the git atom over the same engine (`cmd/agent/cli_tasks_merge.go:20`) |
| clean up the scratch home | `gopi tool scratch-gc --dry-run` | the one-shot sweep, reporting first (`cmd/agent/cli_scratch_gc.go:594`) |
| put a branch aside with a reason | `gopi task archive <branch> --reason "…"` | the recorded-reason lane (`cmd/agent/cli_archive.go:133`) |
| see the screen | `gopi tool capture --screen` | live X11 capture (`cmd/agent/cli_capture.go:133`) |
| click/type on the screen | `gopi tool act …` | synthetic pointer/keyboard input (`cmd/agent/cli_act.go:38`) |
| screenshot a web page without touching the screen | `gopi tool shot --url …` | headless render (`cmd/agent/cli_shot.go:121`) |
| turn a text file into a PNG | `gopi tool text2img <file>` | the pure-Go renderer (`cmd/agent/cli_text2img.go:123`) |
| make a GIF from PNG frames | `gopi tool gif …` | the time dimension of the picture surface (`cmd/agent/cli_gif.go:124`) |
| read text out of an image | `gopi tool vision_analyze <png>` | the vision tool as a command (`cmd/agent/cli_vision.go:43`) |
| search the code by symbol | `gopi tool code-search <symbol>` | the `code_search` mirror (`cmd/agent/cli_code_search.go:92`) |
| search code/docs by text | `gopi tool keyword_search "<regex>"` | the `keyword_search` mirror (`cmd/agent/cli_keyword_search.go:106`) |
| search the web | `gopi web search "<query>"` | the grouped form of the tool (`cmd/agent/cli_web.go:115`) |
| save a lesson for later runs | `gopi memory store …` | the knowledge base family (`internal/tools/ledger_backup.go:88`) |
| subscribe to task events | `gopi notify events` | the notify stream (`cmd/agent/cli_notify.go:notifySubVerbUsage`) |
| set up shell tab-completion | `gopi config completion bash` | the generator (`cmd/agent/cli_completion.go:273`) |

---

## 5. Spellings that are deliberately odd

- **Deprecated but routed.** `gopi web_search` / `gopi web_fetch` open their
  page with a pointer to the grouped form; `gopi task serve` prints one pointer
  line saying merging is no longer on the command line (`cmd/agent/cli_tasks.go:taskServeAliasPointerFmt`).
  Keeping the spellings working is the point: running deployments and scripts
  must not break — they get an explanation rather than "unknown command".
- **Underscore vs hyphen** is a naming rule, not an accident — see §0
  (`cmd/agent/cli_verbs.go:CliVerbUIState`).
- **A tool name that is not a command** (the underscore `gopi code_search`)
  is refused by name with the corrected spelling rather than sent to the model
  (`cmd/agent/cli_undispatched_tool_words.go:undispatchedToolWordRefusalReason`).
- **A failed/absent verb's siblings**: the retired `gopi replay` is not a verb —
  it is `gopi chat --replay` now (`internal/config/cli_verb_guard.go:1068`, the
  guard row that answers the old word with that address).

---

## 6. Keeping this page honest

What stands behind this document — and, stated plainly, what does not.

- **The document gate this section used to name is GONE, and this page says so
  rather than claiming a check it no longer has.**
  `internal/standards/commands_doc_gate_test.go` existed and asserted five things
  — (a) every verb the source defines has an entry in this document; (b) this
  document invents no verb; (c) the generated block is byte-identical to a fresh
  render, with each row's level the one the refusal table answers; (d) every
  `file.go:<symbol>` / `file.go:<line>` anchor in this file resolves, under a
  read-floor; (e) §4 teaches no refused spelling. It was deleted WHOLE in
  `29882b3b` ("standards: drop the document-pipeline gates — doc-pipeline removal
  slice 1/5"), together with the shared `internal/standards/anchor_semantics.go`
  that implemented the `file.go:<NNN>` criterion for the two document gates.
  **Nothing replaced it**, so of that list exactly one property survives, and not
  as a gate (see the generator below): (c)'s byte-identity, which is the
  generator's `--check` and which no test executes on its own. **There is NO
  machine check today that a `file.go:<NNN>` anchor resolves, that this page
  invents no verb, or that §4 teaches no refused spelling** — those are an
  editor's responsibility, verified by hand, and this paragraph exists so a
  reader does not trust a check that is not there.
- **`](#…)` in-document heading links have never had a gate.** No test in this
  tree reads one: `grep -rln '](#' --include='*_test.go' internal/ cmd/
  testsuite/` returns nothing. The page's own convention is the rule — an
  in-document link carries its heading's NUMBER (`#1-the-machine-truth-behind-this-page`,
  `#2-the-verb-index-generated`), because GitHub's slug for
  `## 6. Keeping this page honest` is `#6-keeping-this-page-honest`: the heading's
  digits survive the slug rule (`.` is dropped, the number is not), so a link that
  omits the number points at no heading at all. Two links in this file omitted it
  from birth and were dead until this section was written; they are why the rule
  is stated. **A heading rename silently kills every link to it**, and only a
  reader who clicks one finds out.
- **`cmd/agent/cli_contracts_test.go`** — the routing half: each dispatched token
  maps to a predicate, each predicate has a live `-h` fingerprint, and the
  constant list and the token map must agree
  (`cmd/agent/cli_contracts_test.go:361`).
- **The generator** — `internal/standards/testdata/commands-census.go:48`
  re-renders §2 from the source; `--check` exits 1 on any delta. It is a program
  a reader runs, not a gate that runs itself: no test executes it.

**Rule for editors.** A hand-written claim in §3–§5 carries an anchor; when the
line moves, move the anchor in the same commit as the code. That anchor
discipline is exactly what CONFIG.md applies to its own rows — a reference that
does not resolve is the rot this page exists to prevent.

Re-derive the whole page's machine half with:

```bash
go run internal/standards/testdata/commands-census.go . COMMANDS.md --check
```
