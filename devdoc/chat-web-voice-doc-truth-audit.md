<!-- doc-class: historical -->
# devdoc Truth Audit — C4: chat / web / voice specs

Status: **audit report** (this branch adds this file plus its own rows: the `historical` banner and
the `devdoc/DOC-INDEX.tsv` classification row at `:541`; no production code changed).
Method: `devdoc/doc-truth-audit-plan.md` §3 (extract standing claims → test against code with
`file:line` evidence → classify `aligned` / `drifted` / `planned-absent` / `superseded` /
`informational` → bug-worthiness estimate → report staleness signals).
Base: `feat/chat-web-voice-doc-truth-audit` @ `52090dc0`. Nothing was submitted (epic instruction).

---

## Summary

18 documents audited: the 8 named in the briefing plus the full `devdoc/web-*.md` set (10 files).

| verdict | count | docs |
|---|---|---|
| **aligned** | 11 | `chat-table-design`, `wf-contract-webfetch`, `chat-m2-streaming`, all 7 web report docs except as noted, `voice-input-design-space` §5 |
| **drifted** | 3 | `gui-chat-roadmap`, `SPEC-voice-asr-engine-selection`, `SPEC-voice-input-phase23` |
| **superseded** | 2 | `SPEC-voice-input-whisper`, `voice-input-design-space` |
| **planned-absent** | 2 claims | whisper SPEC §5 (input-box interface); phase23 §4 (ssh transport) / §3 (cgo binding) |
| **informational** | — | borrow surveys, effort tables, measurement tables in every doc above |

**The one finding that matters.** The voice SPEC chain (whisper → asr-engine-selection →
phase23) reads as one continuous design, but **two of the three mechanisms phase23 §3–§4
declares as "already verified / decided" are not what shipped**:

| phase23 says | master does | evidence |
|---|---|---|
| §3 "cgo 绑定" — `#cgo LDFLAGS: … -lllama -lggml …`, `import "C"` | **subprocess**, no cgo at all | `cmd/agent/cli_voice.go:641` `exec.Command(opts.runtime, "--enc", enc, "-m", llm, "-a", audioPath, "--chunk", …)`; `grep -rn 'import "C"' cmd/ internal/` → **0 hits**; declared build is `CGO_ENABLED=0` (`Makefile:9`) |
| §4 "传输：ssh（已配免密/零新端口）" | **HTTP multipart** to `/transcribe` | `cmd/agent/cli_voice_client.go:13-14` (`mime/multipart`, `net/http`), `:131 voiceTranscribePath = "/transcribe"` |
| §6 config `voice.funasr.encoder/llm/chunk_sec`, `voice.engine: funasr\|whisper` | flat `voice_engine: "local"\|"doubao"` + flags | `internal/config/settings.go:45,50,57-58`; `cmd/agent/cli_voice_engine.go:72` `voiceEngineValues = []string{config.VoiceEngineLocal, config.VoiceEngineDoubao}` |

The *decision* phase23 §1 made (Fun-ASR-Nano, CER 1.58% vs whisper-small 5.14%) is **confirmed
current** — it is quoted verbatim in the shipped source (`cli_voice.go:34-38`, `:93-96`). The
*mechanism* the same document describes in several H2 sections is a design that was never built
that way. A future agent reading phase23 today and following §3 would introduce cgo into
`cmd/agent`, breaking the project's declared build.

### Which voice engine / design is current, and which is superseded (the acceptance asks explicitly)

- **CURRENT (default, since 2026-10-07):** **豆包 / Volcengine cloud ASR** (录音文件识别,
  `volc.seedasr.auc`) — the terminal default is `config.VoiceEngineDefault`
  (= `config.VoiceEngineDoubao`, `internal/config/settings.go`). It needs **0 B** of weights and
  **0 B** of runtime; the audio is uploaded, and every run that resolves to it prints the privacy
  notice on stderr (`cmd/agent/cli_voice_engine.go`, `voiceDoubaoPrivacyNotice`) naming the
  endpoint and the three channels back to `local`.
  (`internal/doubaoasr/doubaoasr.go`, `cmd/agent/cli_voice_engine.go`). The 流式 SAUC resource was
  asked for first and is **not granted** (HTTP 403, measured) — `doubaoasr.go:27-30` states this.
- **CURRENT (opt-in, for on-machine transcription):** local **Fun-ASR-Nano** through the official
  FunASR llama.cpp runtime, spawned as a **subprocess** — `--engine local`
  (`config.VoiceEngineLocal`; also `GOPI_VOICE_ENGINE=local` or settings `"voice_engine": "local"`).
  Weights `funasr-encoder-f16.gguf` + `qwen3-0.6b-q4km.gguf` (`cli_voice.go`), `--chunk 15`
  (`voiceDefaultChunkSec = 15`). Reached by name now, not by default — the reversal's argument is
  `devdoc/doubao-asr-integration.md` §3.
- **SUPERSEDED:** **whisper.cpp** as the local engine (`SPEC-voice-input-whisper.md` — carries
  `<!-- doc-class: historical -->`); **cgo/static-library binding** of the ASR runtime
  (`phase23 §3`); **ssh** as the client→server transport (`phase23 §4`, `voice-input-design-space §1`
  T1); **whisper-centric "fixed architecture"** with device-side `ffmpeg|curl`
  (`voice-input-design-space.md` — banner present).
- **NEVER IMPLEMENTED:** `--engine whisper` — documented as a保留切换能力 in
  `SPEC-voice-asr-engine-selection.md §4` / `phase23 §6`, but `voiceValidateEngine`
  (`cli_voice_engine.go:104-115`) accepts exactly two values and rejects anything else loudly.

---

## Doc-by-doc

### Voice SPECs

| doc | base / last touch | class | verdict | evidence |
|---|---|---|---|---|
| `SPEC-voice-input-whisper.md` (297 L) | 2026-09-11, bannered `5faf7daa` | SPEC, banner `historical` | **superseded** | §3.1 role/config design survives as *shape* (client/server split shipped: `cli_voice_client.go` vs `cli_voice_server.go`), but the engine (`whisper_full`), the cgo static build (§2.5) and `capture_device` config key are all absent from master. No `superseded-by` pointer. |
| `SPEC-voice-asr-engine-selection.md` (200 L) | 2026-09-11 `2573465d` | SPEC, **no banner** | **drifted** (engine choice aligned, §4 config drifted) | Engine conclusion Fun-ASR-Nano = shipped default ✅. §4's jsonc block (`"engine": "funasr"\|"whisper"`, `voice.server_addr`, `voice.model_dir`) does not exist in `internal/config` — the real knob is `voice_engine` (`settings.go:50`) with values `local`/`doubao` (`:57-58`). §6 待办 lists "FunASR 模型下载 🔄" and "Phase 2/3 ⏳" — both long done (`cli_voice.go:641`, `cli_voice_server.go:114`). |
| `SPEC-voice-input-phase23.md` (196 L) | 2026-09-12 `4a490669` | SPEC, **no banner** | **drifted** (decision aligned; §3 mechanism, §4 transport, §6 config drifted) | Aligned: §1 decision table (quoted in `cli_voice.go:34-38`); §3's HTTP contract — `POST /transcribe`, multipart field `audio`, reply `{text, engine, chunk, elapsed_ms}` — is **byte-for-byte** what ships (`cli_voice_server.go:114,158-163`). Drifted: §3 cgo binding (see table above), §4 ssh transport, §6 nested `voice` settings object. §5 "文本填入用户输入框" — no such interface exists; `--client` prints the text (`cli_voice_client.go:31` comment: "record N seconds → POST multipart to /transcribe → text"). |
| `voice-input-design-space.md` (349 L) | 2026-09-11, bannered `5faf7daa` | SPEC/DESIGN, banner `historical` | **superseded** (except §5, which is still true) | Overturned: §0 "fixed architecture" picks local whisper.cpp on peerbox as the v1 backend — master's local engine is Fun-ASR-Nano and the cloud opt-in is doubao, not Volcengine-as-escape-hatch-only. §1.1's device contract (`ffmpeg -f alsa … \| curl -T -`) is not the shipped client: `cli_voice_client.go:48-49` names `arecord`/`parecord` and the client is gopi itself. **Still aligned:** §5's injection-point analysis — `cmd/agent/cli_web.go:716 mux.HandleFunc("/api/chat", s.handleChat)`, `s.history` server-owned (`:271`, `:1172`), the `/`-prefix hazard (`handleSlash`). §9's "no cgo in gopi" was honoured: `import "C"` → 0 hits. |

### Chat

| doc | base / last touch | class | verdict | evidence |
|---|---|---|---|---|
| `gui-chat-roadmap.md` (523 L) | base master `0dac2ae7`, last `d145c847` 2026-09-16 | SPEC/roadmap, **no banner** | **drifted** (TL;DR + §1 status table stale; borrow survey valid) | **Confirms §2.2(A).** TL;DR line 25: "cooked-mode `bufio.ReadString('\n')` (cli_chat.go:424/440). No history, no cursor editing, no multiline" — false: `cmd/agent/cli_chat.go:408` is `line, ok, err := input.ReadLine()`; `grep -n 'ReadString\|bufio' cmd/agent/cli_chat.go` → **no hits**; `internal/lineedit/` holds 26 `.go` files (editor, history, keys, menu, raw, render, region, answer, value, winsize …). The same stale claim is repeated in the §1 status table rows `cli_chat.go:424` / `:437` / `:440` / `:469` (lines 217-220) — the file is now 651 lines, so those citations still resolve to *different* code. **Extends §2.2(A):** the roadmap's §4 M1.4/M1.5 are marked SHIPPED, so the doc knows how to update status — the TL;DR and §1 were simply never revisited; and M2/M3 status is absent entirely although M2 landed (`internal/lineedit/region.go`, `cmd/agent/chat_stream.go`). |
| `chat-table-design.md` (147 L) | 2026-08-30 `c7bbf0f4` | SPEC/design, no banner | **aligned** | `chatsTable()` (`internal/tools/chat.go:77-99`) has all 18 columns the doc lists, incl. the reserved `brewed_at REAL` and the M4.6 additions `title`/`parent_session_id`/`git_branch`/`git_repo_root`/`end_reason` with the `CHECK (end_reason IN ('','normal','interrupt','crash'))` the doc specifies. `internal/agent/chatregister.go` exports `RegisterChatSession` (`:54`), `RecordChatTurn` (`:82`), `CloseChatRow` (`:98`). `chats_fts` + `chats_tri` triggers exist (`chat.go:107-126`) matching §④ "检索留待后续". `gopi chat list\|backfill` dispatch constants `chatSubcommandList`/`chatSubcommandBackfill` (`cli_chat.go:114-115`) + `TestChatIndexArgs` (`cli_chat_test.go:146`). `TestChatSearchIsolationFromTasks` (`internal/tools/chat_test.go:227`) — **ran, PASS**. |
| `chat-m2-streaming.md` (741 L) | 2026-09-16 `6cc98c61` | REPORT (Task Report), **no banner** | **aligned but stale disclaimers** | Claims hold: `internal/lineedit/region.go:78 editorTopRow = 0` is exactly the round-6 inversion the doc's round-6/7 sections describe; `answer.go`, `cmd/agent/chat_stream.go`, `lineedit_wire.go:245 NewSlashCompleter` all present. `go test ./internal/lineedit/ -run TestA5B -v` → all PASS, including `TestA5B_CommittedLineSurvivesInScrollback` and the negative control `TestA5B_InteriorRegionTopDiscardsLines`. **Stale:** the doc's headline disclaimer "the two-goroutine terminal WRITE … is DISPATCHED rather than half-fixed" (`01a0aa9a-84de-7555-b14a-c1f087d82515`) — that task is **`status: completed`** (`gopi task show`), and its fix landed as `devdoc/chat-m2-status-wake.md` (bannered `historical` on master, `5faf7daa`). The doc carries no banner while its own sibling reports all do — the `5faf7daa` classification pass missed it. |

### Web

All ten `devdoc/web-*.md` are REPORT-class (`# Task Report:` / devdoc-for-a-branch / brief). Standing
claims were tested anyway, since several are cited as the roadmap's acceptance evidence.

| doc | date | banner | verdict | evidence |
|---|---|---|---|---|
| `web-align-p0-brief.md` (27 L) | 08-29 | — | informational | Change manifest for `feat/web-align-p0`; names `internal/agent/loop.go` `OnToolEvents` + `chat_ops.go` ToolEvent. Both exist (`cli_web.go:597-621` consumes them). Historical, no standing claim. |
| `web-align-p0.md` (136 L) | 08-29 | — | **aligned** | R1 server-owned history: `cli_web.go:271` "history is the SERVER-OWNED authoritative conversation transcript", `:1172 s.history = append([]deepseek.Message{}, ag.Ctx.Messages...)`. R2 tool events → SSE. bug003 shutdown → `webShutdownGrace` (`:421,427`). |
| `web-align-p1.md` (164 L) | 08-29 | — | **aligned** | R4 shared command table: `cmd/agent/commands.go` (`slashCommandNames` `:148`); R5 image turns → vision model: `validateWebImages` (`cli_web.go:998`), `srv.visionModel` (`:298,359`). |
| `web-chat-single-session.md` (103 L) | 08-29 | — | **aligned** | "ONE session id + ONE session log across all turns": `cli_web.go:343` "and reused across every /api/chat turn, so the web conversation is one" + `:245`. |
| `web-fetch-cli.md` (51 L) | 08-29 | `historical` | **aligned** | `isWebFetchSubcommand` (`cli_webfetch.go:53`), `parseWebFetchArgs` (`:75`), `runWebFetchSubcommand` (`:132`), dispatched from `cmd/agent/main.go:859`. |
| `web-m1-b1b2b3.md` (207 L) | 09-16 | — | **aligned** | B1 reset: `webChatResetPath` (`cli_web.go:1305`) registered `:715`, `handleChatReset` (`:1556`). B2/B3 in the same file. |
| `web-m2-rework.md` (155 L) | 09-17 | — | **aligned** | B5 `/api/transcript` (`:718`); B8 `markdownNodes` (`:2104`); B9 `detectSlashMenu` (`:1401`); B12 `webTurnTimeout` + `webTurnTimeoutEnv` (`:733`). Doc self-tags `WORKER-OWNERSHIP: OPERATOR-ONLY` (SPEC-ish signal) yet has no doc-class banner. |
| `web-menu-keyboard-fix.md` (130 L) | 09-19 | — | **aligned** | `menuRefresh(resetSel)` (`:2261`) with the identity rule; wiring split exactly as documented: `:2705 input → menuRefresh(true)`, `:2706 click → menuRefresh()`, `:2712 keyup`. `TestWebMenu_KeydownKeyupPairKeepsTheHighlight` (`cli_web_menu_keyboard_test.go:78`). |
| `web-signal-session-accounting.md` (86 L) | — | `historical` | **aligned** | Root cause = `testutil.LoadSession` counting every `*.jsonl`; `internal/signalrec/signalrec.go:44` documents `sessions/<id>/signal-provenance.jsonl` beside `session.jsonl` — the doc's exact claim. |
| `web-signal-session-end.md` (183 L) | — | `historical` | informational | Root cause (sandbox env not reaching the child). Historical; bannered. |

### `wf-contract-webfetch.md` (64 L, bannered `historical`)

| doc | base / last touch | class | verdict | evidence |
|---|---|---|---|---|
| `wf-contract-webfetch.md` (64 L) | bannered `historical` | CONTRACT, banner `historical` | **aligned** | No standing claim has drifted: the four contract tests the doc names exist and pass (`testsuite/webfetch_contract_test.go:74,118,255,271` → `go test ./testsuite/ -run TestWebFetchContract` **ok 1.076s**); the script side is as described (`plugins/websearch/scripts/web_fetch.py` `DIRECT_DOMAINS` `:57`, `_direct_fetch` `:180`, `_pick_route` `:198`, `--cov-mark-route` `:265`); the deployed-script drift guard holds (`diff -q` vs `~/.gopi/plugins/websearch/scripts/web_fetch.py` → **identical**). |

The four contract tests the doc claims all exist and pass:
`TestWebFetchContract_GeoRoute` (`testsuite/webfetch_contract_test.go:74`),
`_FetchStatusContract` (`:118`), `_SingleLineBodyRegression` (`:255`),
`_DeployedScriptSync` (`:271`) — `go test ./testsuite/ -run TestWebFetchContract` → **ok 1.076s**.
The script side is as described: `plugins/websearch/scripts/web_fetch.py` `DIRECT_DOMAINS` (`:57`),
`_direct_fetch` (`:180`), `_pick_route` (`:198`), the `--cov-mark-route` test hook (`:265`).
The drift guard still holds today: `diff -q plugins/websearch/scripts/web_fetch.py
~/.gopi/plugins/websearch/scripts/web_fetch.py` → **identical**.

---

## Findings ranked

| # | finding | doc(s) | suggested fix | bug-worthiness |
|---|---|---|---|---|
| **F1** | **phase23's §3/§4 mechanisms are not the shipped ones** (cgo static binding; ssh transport). A reader following §3 introduces `import "C"` into `cmd/agent`, which the declared build (`Makefile:9 CGO_ENABLED=0`) cannot compile. | `SPEC-voice-input-phase23.md` | Add a status header naming the two superseded mechanisms and pointing at `cmd/agent/cli_voice.go:631-663` (subprocess) and `cli_voice_client.go:13-14,131` (HTTP multipart). Do **not** rewrite §1 (its decision is current). | **not-a-bug** (doc-only), but **highest directional risk** in this slice |
| **F2** | `SPEC-voice-asr-engine-selection.md` has **no doc-class banner** although it is 90% a Phase-0/1 measurement report, and its §4 config snippet + §6 待办 read as live work. | `SPEC-voice-asr-engine-selection.md` | Banner + a "superseded: see phase23 §1 / cli_voice_engine.go" pointer; the §4 jsonc block should be marked `planned, not implemented`. | **not-a-bug** |
| **F3** | `gui-chat-roadmap.md`'s TL;DR and §1 status table still assert the cooked-mode input path that master replaced (a month of landed work mis-described as "actually broken today"); the file:line citations resolve to unrelated code. | `gui-chat-roadmap.md` | Status block at the top (the plan's §2.2(A) fix); fix the four `cli_chat.go:4xx` rows to `cli_chat.go:408` + `internal/lineedit/`. §1.1 borrow survey stays untouched. | **not-a-bug** |
| **F4** | No voice doc carries a `superseded-by` edge; "which design is current" must be assembled from four docs and the code. | all four voice SPECs | One-line `Status: current\|planned\|superseded (by …)` header per doc (the plan's §5 standing recommendation). | **needs-operator-decision** (whether to adopt the header convention) |
| **F5** | `chat-m2-streaming.md` escaped the `5faf7daa` banner pass (every sibling report has one), and its headline disclaimer is stale: its dispatched follow-up `01a0aa9a-…` is **completed** and superseded by `chat-m2-status-wake.md`. | `chat-m2-streaming.md` | Banner + "superseded (status-refresh half) by `devdoc/chat-m2-status-wake.md`". | **not-a-bug** |
| **F6** | 5 of 10 `devdoc/web-*.md` carry no banner, though all 10 are REPORT-class. The classification pass reached only 5 in this family. | `web-align-p0-brief`, `web-align-p0`, `web-align-p1`, `web-chat-single-session`, `web-m1-b1b2b3`, `web-m2-rework`, `web-menu-keyboard-fix` | Extend the classification pass to the family; no content changes needed (all audited claims are true). | **not-a-bug** |
| **F7** | whisper SPEC §5's "transcript fills the input box, user edits, then sends" interface does not exist (the client prints to stdout). | `SPEC-voice-input-whisper.md` §5, `phase23` §5 | Mark `planned`; note that the operator's intent is satisfied by stdout + the existing `/api/chat` path (`cli_web.go:716`), so nothing is blocked. | **not-a-bug** (planned-absent is the normal state of a design doc — no current artifact asserts the box exists) |

| **F8** | **`TestDocManifestCoversEveryDevdocFile` was already RED at this task's base**: 29 files under `devdoc/` have no row in `devdoc/DOC-INDEX.tsv` (measured at `52090dc0` by `comm -23 <tree> <manifest>`), and `... and 4 more unclassified files` is truncated at 25 by the gate itself. The manifest's own policy says every file must be classified; four merged tasks landed docs the classification pass never reached. | `devdoc/DOC-INDEX.tsv` | Add the 29 missing rows (class + reason) — mechanical, no content change. My own doc was the 30th and is classified in this branch (row 541, `historical`, banner added) — the 29 are pre-existing and out of this audit's brief. | **bug** (a red gate nobody sees, per the same family as the classification report's F0) |

**Nothing here is a code bug** *in the chat/web/voice docs*. Every drifted/planned-absent claim is a *doc* claim; no rule text,
help output, test or caller in the tree asserts a feature that is missing. The actionable fixes are
doc-status edits, and F4 is a policy question, not a fix to apply.

---

## Coverage and limits

- **Audited (18):** the 8 named docs + all 10 `devdoc/web-*.md`.
- **Admitted by content, not name, and then excluded from the row set:** `devdoc/chat-m2-status-wake.md`
  (bannered `historical`, `5faf7daa`) — read as the *evidence* that F5's disclaimer is stale; not
  audited as its own row.
- **Not audited — family neighbours outside the brief, named honestly rather than implied covered:**
  `devdoc/chat-*.md` beyond the two named (`chat-lineedit`, `chat-mode-report`, `chat-ops-report`,
  `chat-simplified-path`, `chat-single-session`, `chat-help-command-arg`, `gopi-chat-mode-effort`);
  `devdoc/voice-*.md` beyond the four named (`voice-input-client`, `voice-input-out-file`,
  `voice-input-review`, `voice-listen-*`, `voice-hw-matrix`, `voice-e2e-hp`, `voice-e2e-hp`,
  `voice-node-coverage`, `voice-input-zero-dependency`, `voice-capture-failure-matrix`,
  `voice-mic-probe-*`); `devdoc/doubao-asr-*.md` and `devdoc/doubao-asr-eval/`;
  `devdoc/gopi-web-*.md`, `devdoc/websearch-*.md`, `devdoc/vps-websearch-setup.md`,
  `devdoc/review-cli-chat-web.md`, `devdoc/w3m-chat-integration-status.md`,
  `devdoc/merge-resolve-{chat-m2-status-wake,chat-m2-streaming,voice-input-client,web-signal-session-end}.md`.
  **The family is therefore not closed** — several of those are REPORT-class merge-resolution notes,
  but `voice-input-zero-dependency` and `voice-input-client` plausibly carry standing claims about the
  shipped voice path and belong in a follow-up sweep.
- **Not measured:** whether the `5faf7daa` classification pass bannered every doc it *should* have
  tree-wide (I measured only this family: 5 of 18). A tree-wide banner-coverage count is out of scope
  here and is reported as a gap, not an estimate.
- **Measurement limit:** every verdict is a **read** of the cited line, not an executed behaviour
  test, except where a test run is quoted in `## Verification`. No live ASR transcription was run
  (no model/runtime on this host is guaranteed) — the engine verdict rests on the CLI seam code
  (`voiceResolveEngine` → `transcribeFile` → `transcribeFileLocal`/`transcribeFileDoubao`), which is
  the dispatch that decides the engine, plus the doc-class of each design.
- Candidate docs, not read: everything under `devdoc/archive/`, `devdoc/evidence/`, `devdoc/assets/`
  (non-doc or archived by construction).

---

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` → exit 0, no output.
- **Tests (branch-scoped to the docs' claims, not the whole tree):**
  - `go test ./internal/tools/ -run TestChatSearchIsolationFromTasks -count=1` → **ok 3.051s**
    (backs the `chat-table-design` §④ claim).
  - `go test ./testsuite/ -run TestWebFetchContract -count=1` → **ok 1.076s**
    (backs all four `wf-contract-webfetch` claims).
  - `go test ./internal/lineedit/ -run TestA5B -count=1 -v` → **13/13 PASS**, including
    `TestA5B_CommittedLineSurvivesInScrollback` and the negative control
    `TestA5B_InteriorRegionTopDiscardsLines` (backs `chat-m2-streaming`'s A5 half).
- **Static evidence:** `grep -rn 'import "C"' cmd/ internal/` → 0 hits; `Makefile:9` declares
  `CGO_ENABLED=0`; `diff -q` repo vs deployed `web_fetch.py` → identical;
  `gopi task show 01a0aa9a-84de-7555-b14a-c1f087d82515` → `status: completed`.
- **This report's own class row (F8 evidence):** at base `52090dc0`,
  `comm -23 <(git ls-tree -r --name-only 52090dc0 devdoc/|sort) <(git show 52090dc0:devdoc/DOC-INDEX.tsv|cut -f1|sort) | wc -l`
  → **29** unclassified files; the same difference against this branch's own tree → **30**, the extra
  entry being `devdoc/chat-web-voice-doc-truth-audit.md`. This branch adds its own row
  (`devdoc/DOC-INDEX.tsv:541`, class `historical`) and the `<!-- doc-class: historical -->` banner, so
  `go test -run TestDocManifestCoversEveryDevdocFile ./internal/standards/` no longer names this
  report; it still fails on the 29 pre-existing files (F8). `go test -run TestDocClassBannerMatchesManifest
  ./internal/standards/` → **ok 2.391s** (banner and manifest agree, including this doc).
- **Anchor spot-check on this report's own claims:** `sed -n` of each cited line re-read at commit
  time — `cmd/agent/cli_chat.go:408` `line, ok, err := input.ReadLine()`;
  `cli_voice.go:641` `cmd := exec.Command(opts.runtime,`;
  `cli_voice_client.go:131` `voiceTranscribePath = "/transcribe"`;
  `cli_voice_engine.go:72` `var voiceEngineValues = []string{config.VoiceEngineLocal, config.VoiceEngineDoubao}`;
  `settings.go:50` `const VoiceEngineSettingsKey = "voice_engine"`; `internal/tools/chat.go:77`
  `func chatsTable() string {`.
- **No submission:** nothing was sent to the task queue (epic instruction); the only cross-session
  read was `gopi task show`, read-only.

## Not Done (honest)

- No production code, prompt, or config was changed — by design.
- No `superseded-by`/`Status:` headers were added to any doc: the epic says report first; F1–F7 are
  proposals, and F4 is an operator decision.
- The family sweep is partial (see `## Coverage and limits`); the unbannered-doc count is a
  family-scoped measurement, not a tree-wide one.
- **The 29 pre-existing unclassified `devdoc/` files (F8) were NOT given rows** — out of this audit's
  brief, and a mechanical task of its own. This branch classifies only its own file, so the docclass
  coverage gate stays red exactly as it was at base for those 29 (it is not made worse).
- **Banners were not added to the 7 unbannered `devdoc/web-*.md` docs (F6)** — the audit's instruction
  is report-first; the fix is a classification-pass extension, not a content edit.
- No `superseded-by`/`Status:` headers were added to any audited doc (F1–F7 are proposals; F4 is an
  operator decision).
