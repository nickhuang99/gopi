<!-- doc-class: normative -->
# Task Report: `/voice` — continuous local voice input for `gopi chat`

## Completed

- **`/voice` slash command** in the interactive chat REPL (`cmd/agent/chat_voice.go`):
  bare `/voice` toggles the mode, `/voice status` reports it, `/voice stop` leaves it.
  While the mode is ON the run captures the microphone and transcribes **locally**
  in bounded windows, and every transcript is **APPENDED at the caret** of the
  editing line. The operator keeps editing; Enter sends the whole line as **one
  ordinary turn**; Esc leaves the mode and **keeps the line**.
- **The insert road** (`internal/lineedit/lineinsert.go`) — the cross-goroutine
  text-insert facility the feature needed and the repo did not have: a producer
  that is not the read loop QUEUES the text and raises a LEVEL, the loop applies
  it at the **top of `repaint`** (before the plan is resolved and before the rows
  are derived from the buffer) and lowers the level. Public API:
  `Editor.InsertIntoLine` (at the caret), `Editor.InsertIntoLineAtEnd` (append),
  `Editor.InsertLineText(text, place)`, plus `Editor.KeyGate()` (read the owner
  currently at the key seam, so a layer can CHAIN instead of replacing it).
  The append **placement** is decided inside the queue entry (`LineInsertAtEnd`)
  because the caret lives in the model only the read loop may write.
- **Wiring** — `cmd/agent/lineedit_wire.go` (`chatInput.VoiceInsertLine` /
  `VoiceNotice` / `VoiceEditor`), a switch arm in `cmd/agent/cli_chat.go` (the
  word is answered by the REPL, never dispatched — the state it mutates is the
  REPL's), the shared tables (`chatSessionWords`, `chatNativeUsage`,
  `chatReplSlashVocabulary`, the `/help` banner), and a **deferred session-end
  teardown** (`defer voiceMode.shutdown()`) so no exit path can leave the
  recorder running.
- **Reuse, not re-implementation**: device enumeration/selection
  (`voiceListDevices` / `voiceResolveSelection`, the four-tier policy), recording
  (`voiceRecord`), container + the single engine seam (`voicePCM16ToWAV` /
  `voiceStageCaptureAudio` / `transcribeFile`), dependency check (`voiceDepsReady`),
  settings (`parseVoiceArgs` + `voiceResolveProfilePaths`). **No new tool**
  (Rule 3: the agent tool list is unchanged), **no new dependency** (Rule 35 —
  nothing persistent is created; `arecord`/`parecord`/`pactl` were already the
  ROUTE) and **no device load** (Rule 27 — every case uses a scripted recorder).
- **A REAL DEFECT found and fixed** while verifying on a live terminal (see
  `## Verification`, `Ran:`): a **lone Escape never reached the key gate**.
  `internal/lineedit/editor.go` resolved a timed-out ESC in the `EventNone` arm
  and handed it straight to `handleKey`, bypassing `dispatchKeyGate` — so a gate
  owner claiming Escape (exactly the `/voice` exit) never saw the operator's key,
  while every unit case passed because `InjectKey` delivers a *complete* sequence
  through the ordinary `applyRead` road. Fixed by giving the flushed key the same
  road (stamp `Origin`, `dispatchKeyGate`, then `keyRelease` + `handleKey`) and
  pinned by `TestKeyGate_SeesTheEscapeTheTimeoutFlushes`.

## Not Done

- **The status row does not carry a voice field.** The mode is visible through its
  own console rows (entered / transcript / silence / off) and through `/voice
  status`, not through the reserved status line. The status line's column
  arithmetic is separately pinned by its own geometry tests, and adding a column
  there is a change of the surface every other reader of that row depends on —
  it is deliberately out of this task's scope, not an oversight.
- **The recorder is stopped by the clock, not by a signal**, so leaving the mode
  may let one already-started window finish; its transcript is **discarded** (the
  leaving note says so). A signal-based stop needs a handle on a process captured
  behind `voiceRecord`'s own context, i.e. a second capture path — rejected on
  purpose (one capture implementation is why a device that cannot be opened is
  reported identically from every door).
- **Entering costs up to one window's latency** before the first transcript
  (6s + the engine's own transcription time). Streaming/partial results are not
  implemented.
- **No `--device` flag on `/voice`**: the mode uses the four-tier default policy
  the bare verb uses. A session that needs a different device should set it the
  way the verb accepts it; a per-mode flag was not added.
- **`/voice` in a cooked carrier** (piped stdin, `--no-editor`) is refused by name
  and does nothing — correct, but it means a scripted session cannot turn the
  microphone into its input, only type.

## Verification

- **Build**: `go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK;
  `go vet ./cmd/agent/ ./internal/lineedit/` → clean.
- **Tests**: `make test-budget PKGS='./internal/lineedit ./cmd/agent'` — branch-scoped
  (smoke set), never a whole-tree run (Rule 27). `./internal/lineedit` → **ok 52.4s**
  (`go test ./internal/lineedit/ -count=1` → `ok github.com/the operator/gopi/internal/lineedit 52.442s`).
  `./cmd/agent` → the budget run finished **958s wall, exit 1**, reporting **12
  top-level reds and 9 subtests**; `./internal/lineedit` was green in the same run.
  - **The one red this diff caused** was `TestChatUsageStaysWithinItsLineBudget`:
    `gopi chat -h: 565 lines (budget 560)` — the five lines `/voice` adds to the
    slash-command table. `chatUsageMaxLines` was raised 560 → 565 in `8c014619e`,
    with the reason recorded at the constant (the paragraph IS the command's
    contract and a slash command has no `gopi voice -h` page to move it to).
    Re-measured after the raise: `go test ./cmd/agent/ -run 'TestChatUsageStaysWithinItsLineBudget'` → green (in the targeted run quoted in `## Evidence`). <!-- selector-ok: a RECORD of a 2026-10 run, quoted verbatim; `chatUsageMaxLines` was retired with the whole chat-length-cap mechanism (`cmd/agent/cli_usage_budget_test.go` records that the cap replaces 577), so this selector cannot match again. The live help-page guard is `TestChatScopedHelpPageStaysWithinItsUsageBudget`. -->
  - **The other eleven reds are PRE-EXISTING and NOT this diff's — measured, not
    argued.** All eleven (`TestCLINamingExceptionsAreStillDispatchable`,
    `TestLedgerOpenersUseProductionDSNContract`,
    `TestMergePromptCoversEveryRequiredSection`,
    `TestMergeUnionRowCheckOnTheRealManifest`, `TestResumeKeepsADirtyWorktree`,
    `TestSpawnersPinTheRepoContext`, `TestSubmitSpawnersGiveTheChildAResolvableRepo`,
    `TestSubmitSpawnersGiveTheChildAResolvableRepo_ExactMatchRule`,
    `TestSyncTaskIDRefusedOnEverySessionFace`,
    `TestTaskMergeCanonicalFlagRefusalOnTheRealBinary`,
    `TestWorkerResumeKeepsTheGuardsIntact`) were re-run **on the branch's base
    `bb6ee97f9` in a separate checkout** (`/dev/shm/base-01a11601`, Rule 29 — never a
    borrow of someone's worktree) and **failed identically there** (`EXIT=1`, 11
    `--- FAIL` lines, log `/home/user/workspace/gopi-test-scratch/base-12-01a115e0.log`).
    Their shapes are environmental: several read a git repo through
    `/dev/shm/...` and hit `fatal: 不是 git 仓库 … 停止在文件系统边界（未设置
    GIT_DISCOVERY_ACROSS_FILESYSTEM）`. This diff touches neither `cli_completion_*`,
    `cli_sync*`, `cli_resume*`, `cli_worker*` nor the contract inventory.
  - **New cases** (by name, green in this diff's own runs):
    `./internal/lineedit` → `ok 52.442s` covering
    `TestLineInsert_AppendGoesAfterWhatIsTyped`,
    `TestLineInsert_EmptyAndFullBacklogAreRefusedByName`,
    `TestLineInsert_DrainAppliesInOrderAndLowersTheLevel`,
    `TestLineInsert_KeyGateReaderReturnsTheInstalledOwner`,
    `TestKeyGate_SeesTheEscapeTheTimeoutFlushes`; and `./cmd/agent` (targeted run,
    `ok github.com/the operator/gopi/cmd/agent 23.012s`) covering `chat_voice_pty_test.go` —
    a real pty + the real chat input layer: enter → append after a typed draft → the
    origin test on Escape → every refusal leg leaves the mode off with zero rounds
    recorded → a failed round reports and leaves → silence inserts nothing and keeps
    listening → usage/vocabulary are the same reading.
- **Ran** (verbatim, Rule 31 — a live `gopi chat` on a real pty, driven by a pty
  driver, judged on the `chat-screen.dump.json` CELL grid, never on OCR):
  `/home/user/workspace/gopi-01a115e03b397a9baf8b3b7408bb47b5/bin/gopi chat` under a
  100×30 pty, with `/voice`, a typed draft, Enter, `/voice status`, a **typed Esc**,
  `/voice status` again, `/quit`.
- **Result** (the observed grid, verbatim from the session dumps):
  - entry: `[gopi] VOICE MODE ON — recording 6s from pulse:@DEFAULT_SOURCE@
    (PulseAudio default source (alsa_input.pci-0000_05_00.6.analog-stereo))
    [first enumerated]; transcribing locally every 6s; each transcript is
    APPENDED at the caret (`status` or Esc leaves, the line is kept).`
  - the append, on a live microphone, after a draft the driver had typed:
    `you> typed-draft 人民推出一批思想精深、艺术精湛、制作精良的优秀作品。` — the
    draft intact, a real transcript appended after it, announced by
    `[voice] 人民推出一批思想精深、艺术精湛、制作精良的优秀作品。`
  - `Enter`: the WHOLE line went as one ordinary turn (the reply's own
    thinking text quotes it as the prompt).
  - `Esc` (typed): `[gopi] voice mode OFF (Esc) — the recorder is stopping; the
    text already on the line is kept. No further transcript will be inserted.`
  - `/voice status` after that: `[gopi] voice mode is OFF — `/voice` starts it.`
  - the engineered pipeline behind the transcript, run on the same machine:
    `./bin/gopi tool voice_input --seconds 3 --device "pulse:@DEFAULT_SOURCE@"` →
    `captured 96000 bytes (3.0s of 16000Hz mono PCM16)`, `transcribed locally in
    8.2s`, text `嗯，就变成只有一个人能干了。`
- **Commit**: branch `task/01a115e03b397a9baf8b3b7408bb47b5` — the deliverable is
  `d8b02c4e2` (`feat(lineedit): cross-goroutine line insert + the flushed-Escape
  gate fix`) + `579634d25` (`feat(chat): /voice — continuous local voice input on
  the editing line`); the budget constant this doc's `## Verification` records was
  raised in `8c014619e`. Read them with
  `git show <hash>` in the shared repo — the branch is a linked worktree, so no
  push is involved (Rule 6c).
- **Rule 30**: `git status --porcelain` in the worktree prints nothing (see
  `## Evidence`).

## Evidence (self-filed)

- `make test-budget PKGS='./internal/lineedit ./cmd/agent'` — output captured by
  the run (job stream): the per-package result lines are quoted in the run's
  final message; the two packages this diff touched are the only ones scoped.
  - `./internal/lineedit` → `ok github.com/the operator/gopi/internal/lineedit 58.058s`
    (observed in this run's stream, `run-1791377740-2554349.json`) — GREEN, and the
    five insert/gate cases this diff added passed by name in it (quoted in the
    targeted re-run below).
  - `./cmd/agent` → in the SAME budget run: `FAIL ... 949.798s`, with **12
    top-level reds** (census taken from that run's own JSON stream,
    `run-1791377740-2554349.json`, `Action=="fail"` ∧ `Test` non-empty): the one
    this diff caused (`TestChatUsageStaysWithinItsLineBudget`) plus eleven measured
    on the base (list and base log in `## Verification`).
  - the targeted re-run, captured whole to
    `/home/user/workspace/gopi-test-scratch/targeted-01a115e0.log`:
    `go test ./internal/lineedit/ -run 'TestLineInsert|TestForwarding|TestKeyGate' -count=1 -v`
    → `ok github.com/the operator/gopi/internal/lineedit 5.643s` with
    `--- PASS: TestLineInsert_AppendGoesAfterWhatIsTyped` … `TestKeyGate_SeesTheEscapeTheTimeoutFlushes` (6 PASS lines, 0 FAIL);
    `go test ./cmd/agent/ -run 'TestChatVoice_|TestChatUsageStaysWithinItsLineBudget' -count=1 -v`
    → `ok github.com/the operator/gopi/cmd/agent 9.652s` with
    `--- PASS: TestChatVoice_EnterAppendsAndEscapeLeaves` (0.70s),
    `TestChatVoice_EscapeIsClaimedOnlyFromTheTerminal` (0.21s),
    `TestChatVoice_RefusalsLeaveTheModeOffWithoutRecording` (0.62s),
    `TestChatVoice_ARoundFailsLeavesTheModeOffAndKeepsTheLine` (0.21s),
    `TestChatVoice_SilenceInsertsNothingAndSaysSo` (1.21s),
    `TestChatVoice_UsageAndVocabularyAreTheSameReading` (0.00s),
    `TestChatUsageStaysWithinItsLineBudget` (1.48s) — 7 PASS, 0 FAIL.
  - the eleven pre-existing legs, measured on the BASE, not argued:
    `cd /dev/shm/base-01a11601 && go test ./cmd/agent/ -run '<the same eleven>' -count=1`
    → `EXIT=1`, the same eleven `--- FAIL` lines (log
    `/home/user/workspace/gopi-test-scratch/base-12-01a115e0.log`), at base
    `bb6ee97f9`. The shapes are environmental, e.g.
    `cli_completion_exceptions_test.go:92: "web_search" has no contract-inventory entry`,
    and several reading a repo through `/dev/shm`:
    `git for-each-ref …: exit status 128: fatal: 不是 git 仓库 … 停止在文件系统边界
    （未设置 GIT_DISCOVERY_ACROSS_FILESYSTEM）`.
- `go build ./...` → empty output, exit 0 (observed).
- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → empty output, exit 0
  (observed, immediately before the live run below).
- The live pty run's evidence is the session dump the run itself wrote:
  `/home/user/.gopi/sessions/8647d8ba-d160-477c-b076-2e9c66bce43a/chat-screen.dump.json`
  (frames=113 at the 40s capture mark, frames=1482 after Enter).
- The regression's RED leg was observed BEFORE the fix on a live terminal: with
  the pre-fix binary, a typed Esc produced **no** `voice mode OFF` row (the grid
  still read `voice mode is ON`) — that is the defect above; the same sequence
  against the rebuilt binary produced the OFF row quoted in `## Verification`.
- `git status --porcelain` in the worktree — **observed empty** at the close of the
  run, immediately before hand-over: `git status --porcelain && echo CLEAN_END` printed
  the marker with nothing before it, at HEAD `19c77cc8f`.
- THIS DOC reached the disk through the door Rule 36 names: the `write` call
  **timed out after 30s** with the bytes already on disk (139 lines / 9243 bytes,
  observed with `wc`), so the classification hook never ran. It was classified
  **in the same change** with the repo's own tool —
  `gopi tool doccheck` reported `file-with-no-row devdoc/chat-voice-slash-command.md`
  (exit 1), `gopi tool doccheck --fix` reported `repaired 1 path(s)`, and the
  re-run exited 0 with the row present in `devdoc/DOC-INDEX.tsv`.

## Files

- `internal/lineedit/lineinsert.go` (new) — the insert road.
- `internal/lineedit/lineinsert_test.go` (new) — its contract + the Esc regression.
- `internal/lineedit/editor.go` — the `lines` queue field, four drain predicates,
  and the flushed-Escape fix.
- `cmd/agent/chat_voice.go` (new) — the mode, its constants, its gate, its loop.
- `cmd/agent/chat_voice_pty_test.go` (new) — the acceptance cases on a real pty.
- `cmd/agent/lineedit_wire.go` — the three `chatInput` methods.
- `cmd/agent/cli_chat.go` — the switch arm, the mode's construction + teardown,
  the `/help` banner line.
- `cmd/agent/commands.go` — `/help voice`.
