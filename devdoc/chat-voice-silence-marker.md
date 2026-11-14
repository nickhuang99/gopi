<!-- doc-class: historical -->
# `/voice` must not type the engine's silence marker onto the operator's line

Task `01a116ad-b671-735b-bcf5-b0ad22a04ed4` · host `peerbox` · worktree
`/home/user/workspace/gopi-01a116adb671735bbcf5b0ad22a04ed4` · branch
`task/01a116adb671735bbcf5b0ad22a04ed4`.

## The defect, and why it survived review

`gopi chat`'s `/voice` mode records one bounded window, transcribes it locally and
APPENDS the transcript to the editing line. A window in which nobody spoke makes
the local Fun-ASR engine answer **its own silence marker** — `/sil` — instead of an
empty string (`/home/user/work/runtime/llama.cpp/fun-asr-nano/funasr-cli/funasr-cli.cpp:267`
drops exactly that token in SRT mode; the streaming arm prints it). The marker is
non-empty, so:

- `voiceChatRecorderLocal.Round` returned it as a transcript (it only `TrimSpace`d);
- the loop's `if text == ""` branch — the ONE silence signal, which writes
  `chatVoiceSilentNote` and re-arms the window — could never fire for a speechless
  round;
- each marker was appended to the operator's line through the normal insert road.

Measured on a real pty against the fixed tree's parent (grid in
`## Verification`, `Ran:` below): `you> draft-/sil/sil`.

Nothing in the tree handled `/sil` — the marker is the ENGINE's vocabulary and
gopi had never read it.

## The choice: the MODE side, not the engine seam

Two places could decide "the engine's marker means no text": the mode's round
boundary, or the engine seam `transcribeFile` (and its two arms,
`transcribeFileLocal` / `transcribeFileDoubao`). **This fix is (a), the mode side**,
at the ONE boundary in `cmd/agent/chat_voice.go` — `chatVoiceTranscriptText`,
applied by `voiceChatMode.loop` to the answer it gets from `voiceChatRecorder`.
The reasons, in the order they decide it:

1. **The two code paths answer different questions.** The seam's question is *what
   did the engine say?*, and its answer is consumed VERBATIM by three other
   surfaces: the bare `gopi tool voice_input`
   (`cmd/agent/cli_voice.go` → `voiceReportLocalTranscript`), both server shapes
   (`cmd/agent/cli_voice_server.go:227` and `:384`), and every eventual caller. For
   those, `/sil` is the engine's honest verdict about a FILE — a human inspecting a
   recording wants to read the marker, and the mode's own sibling
   `voiceReportLocalTranscript` already reports "no text" as a distinct outcome
   rather than pretending the engine said nothing. The seam is the wrong owner of a
   REPL policy.
2. **A seam-side rewrite would be a silent edit of an engine's output** (Rule 15):
   it would have to delete four characters the engine produced, for every caller,
   to serve one caller's UX. The mode-side judgement does not rewrite anything — it
   is a decision at the boundary where the text is ABOUT to become an edit to the
   user's line.
3. **It is per engine, and the seam is not.** `/sil` is Fun-ASR's marker; the cloud
   arm (`transcribeFileDoubao`) never emits it. Putting the rule in the seam means
   putting it in the engine-blind dispatch and making the cloud arm carry a local
   engine's marker (`/sil` has no meaning to Volcengine). Even if it were confined
   to `transcribeFileLocal`, that arm's contract — documented in
   `cmd/agent/cli_voice_engine.go` as "the single point where the engine is
   CHOOSEN" — is to be byte-for-byte the local engine's behaviour.
4. **The mode side is where the reachability lives.** The silence signal the marker
   must reach is the loop's `text == ""` arm. Judging one line above it means the
   branch already exists, already reports and already re-arms the window — the fix
   adds no state, no new branch and no new user-visible text.

**The honest cost of (a)**, stated so a later reader does not mistake it for
coverage: the OTHER surfaces that print an engine transcript still print `/sil` for
a speechless CLIP — by design, per reason 1. The bare verb and the server shapes are
not defective; they are not appending anything to anyone's line. If a future change
makes any of them APPEND text somewhere, this judgement is the piece that must move
with it.

## What changed

- **`cmd/agent/chat_voice.go`**
  - `chatVoiceSilenceMarker = "/sil"` — the engine's marker as a NAMED CONSTANT in
    the mode's own const block, next to `chatVoiceSilentNote` (Rule 1/13: one
    definition, referenced by the judgement and by the tests).
  - `chatVoiceTranscriptText(text string) string` — THE ONE judgement of "did this
    window carry speech". A window is silent **iff it is the marker REPEATED and
    nothing else** (the engine streams one marker per chunk, so one window's buffer
    can hold `/sil/sil`); it returns `""` for that and the input **untouched**
    otherwise. Speech that merely begins with the same characters (`/silence`) is
    not silence, and a real word followed by the marker stays speech — the marker is
    never silently deleted from a transcript that also carries words.
  - `voiceChatMode.loop` applies it once, right after the round returns and before
    the `text == ""` branch, with the reason co-located.
- **No change** to `voiceChatRecorderLocal.Round`, to `transcribeFile` and its arms,
  to the usage text, to `internal/lineedit`, or to the Esc/KeyGate path (already
  green — out of scope by the briefing).

**Why the judgement is applied by the LOOP and not inside
`voiceChatRecorderLocal.Round`.** The recorder is an INTERFACE
(`voiceChatRecorder`) precisely so a round can come from somewhere else, and the
`text == ""` arm is the silence signal for ANY recorder. Judging inside the one
concrete recorder would make the fix hold only for the implementation that happened
to be wired — which is the shape of the defect being repaired — and it would leave
the silence arm unreachable for every other recorder.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./...` → exit 0, empty output.
- **Tests**: `go test ./cmd/agent/ -run 'TestChatVoice_SilenceMarkerInsertsNothingAndSaysSo|TestChatVoiceTranscriptTextJudgesTheEngineMarker|TestChatVoice_EnterAppendsAndEscapeLeaves' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 1.942s` (`green-proof.log`); the whole
  `/voice` + `/voice`-adjacent set
  (`go test ./cmd/agent/ -run 'TestChatVoice|TestVoice' -count=1`) → `ok … 73.017s`.
  The package budget gate (`make test-budget PKGS='./cmd/agent'`) came back AFTER
  the fix with 12 failures — and none of them is this tree's: that gate snapshots
  the package into its own tested binary at run start (00:57:48), and its stream's
  own guard says so (`bare_session_one_dir_e2e_test.go:78: …-tested-bin/gopi (mtime
  00:57:48) is OLDER than …/cmd/agent/chat_voice.go (mtime 00:58:36)`). The
  instant (0.00–0.01 s) worker/worktree/session-dir failures are that guard, and its
  `TestChatVoice_SilenceMarkerInsertsNothingAndSaysSo` red is the deliberate red this
  run measured at 00:52 — its grid prints `you> d/sil/sil` while ALSO printing the
  silence row, i.e. the new case running against a tree without the judgement.
  Branch-scoped smoke against the COMMITTED tree, over exactly those red test names,
  is green: `go test ./cmd/agent/ -run 'TestBareRunKeepsAllItsArtifactsInOneSessionDir|
  TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers|
  TestWorkerChildDoesNotInheritTheDaemonsSessionHandle|
  TestEditToolAnnotatesTeachingDocsInAuditedTrees|TestRawPromptRunReleasesItsWorktree|
  TestChatVoice_SilenceMarkerInsertsNothingAndSaysSo|
  TestChatVoiceTranscriptTextJudgesTheEngineMarker' -count=1` → `ok … 12.065s`. A clean
  FULL-PACKAGE budget verdict for the committed tree was not observed.
- **Red → green**: the regression case was driven RED by unwiring ONLY the
  judgement (`text = chatVoiceTranscriptText(text)` commented out in the same tree,
  no other edit) and GREEN with it restored; both logs are on disk (paths below) and
  the red run's own grid row is quoted in `## Evidence`.
- **Ran** (verbatim — the real surface, on a real pty, with the repo's committed
  instrument and the REAL local engine):

```
# the fixture was chosen by measurement, not by guess (see the report's
# "why this fixture" note): the production recorder argv, pointed at the sink
bash $SB/repro/fixture-pick2.sh
#   -> remap-tone-v60  -> engine: /sil
#   -> remap-noise-v80 -> engine: 而是不是觉得我这个视频有点太短了？ (NOT silence)

# the reproduction rig: null sink + remap source + a bounded 40 x 30 s tone feed
bash $SB/repro/sil-pty.sh $SB/bin/gopi-prefix prefix          # FIXED TREE's parent
bash $SB/repro/sil-pty.sh $SB/bin/gopi-fix    fixed
bash $SB/repro/sil-pty.sh $SB/bin/gopi-fix    fixed-speech     # FIXTURE=zeros-30s.wav
# each: python3 devdoc/evidence/sgr-grid/termgrid.py "$BIN" 100 30 "$WORK/script.txt"
```

- **Result** — the observed cell grids, verbatim (rows as `termgrid.py` wrote them,
  nothing trimmed; `$SB` = `/home/user/.gopi/sessions/01a116ad-b671-735b-bcf5-b0ad22a04ed4`).

  **Before the fix** (`$SB/evidence/prefix-sil-2-esc.grid.txt`) — the defect, live:

```
23|[voice] /sil
24|[voice] /sil
25|[gopi] voice mode OFF (Esc) — the recorder is stopping; the text already on the line is kept. No fur
26|ther transcript will be inserted.
27|
28|you> draft-/sil/sil
```

  Note the two things that TOGETHER are the defect: the silence row
  (`no text in the last 6s window …`) is **absent** — the mode believed those two
  rounds were transcripts — and the marker is **on the operator's line**.

  **After the fix** (`$SB/evidence/fixed-sil-1-round.grid.txt`, shot while the mode
  was still running, and `$SB/evidence/fixed-sil-2-esc.grid.txt`, after Esc):

```
22|[gopi] VOICE MODE ON — recording 6s from pulse:@DEFAULT_SOURCE@ (PulseAudio default source (gopisile
23|ncemic)) [first enumerated]; transcribing locally every 6s; each transcript is APPENDED at the caret
24| (`status` or Esc leaves, the line is kept).
25|
26|[voice] no text in the last 6s window (silence, or the speech fell outside it) — still listening.
27|
28|you> draft-
```

  **The non-regression half, same rig, a fixture whose engine reply is NOT the
  marker** (`$SB/evidence/fixed-speech-sil-1-round.grid.txt`; 30 s of digital zeros,
  which the engine reads as Mandarin — measured, and the reason this fixture was
  chosen for the append leg):

```
27|[voice] 我想要一个完整的家。
29|you> draft-我想要一个完整的家。
```

  So: a real transcript still lands at the caret after the draft (append contract
  intact), the marker never does, and the silence row appears for the marker windows.

- **Commit**: `6cc6b9fda` — `fix(chat): /voice ignores the engine's own silence marker (/sil)`.

## Evidence (self-filed)

- Red run: `go test ./cmd/agent/ -run 'TestChatVoice_SilenceMarkerInsertsNothingAndSaysSo|TestChatVoiceTranscriptTextJudgesTheEngineMarker|TestChatVoice_EnterAppendsAndEscapeLeaves' -count=1`
  with the judgement unwired in the working tree (`$SB/repro/chat_voice.go.fixed`
  preserved, the unwired copy built) → **exit 1**,
  `--- FAIL: TestChatVoice_SilenceMarkerInsertsNothingAndSaysSo (0.22s)`,
  `chat_voice_pty_test.go:497: the engine's silence marker reached the screen:`,
  and the dumped grid row `you> d/sil/sil` — log
  `$SB/evidence/red-proof.log`. Green run, same three cases, judgement restored →
  `ok github.com/nick/gopi/cmd/agent 1.942s` — `$SB/evidence/green-proof.log`.
- The fix itself is exercised by a case that FAILS without it and passes with it —
  the same case, observed in both directions in this run (the two logs above).
- PTY rig, before: `bash $SB/repro/sil-pty.sh $SB/bin/gopi-prefix prefix` → `grid rc=1`
  (the tail step could not quit: `/quit` typed onto a line that still held the
  draft, so it was submitted as prose — the script now clears the line first), two
  shots written anyway; grids `$SB/evidence/prefix-sil-1-round.grid.txt`,
  `prefix-sil-2-esc.grid.txt` (+ `.grid.attr.txt`, `.raw.txt`), driver log
  `$SB/evidence/prefix-grid.log`, `waitfor 'gopi chat' OK (t=3.3s)`,
  `shot sil-1-round (t=51.8s)`, `shot sil-2-esc (t=55.2s)`.
- PTY rig, after: same command with `$SB/bin/gopi-fix` → `grid rc=0`,
  `exit OK (t=56.4s)`; grids `$SB/evidence/fixed-sil-1-round.grid.txt` /
  `fixed-sil-2-esc.grid.txt`, log `$SB/evidence/fixed-run.log`.
- Non-regression PTY run: `FIXTURE=$SB/evidence/zeros-30s.wav bash $SB/repro/sil-pty.sh $SB/bin/gopi-fix fixed-speech`
  → `grid rc=0`; grid `$SB/evidence/fixed-speech-sil-1-round.grid.txt` carrying
  `[voice] 我想要一个完整的家。` and `you> draft-我想要一个完整的家。`; log
  `$SB/evidence/fixed-speech-run.log`.
- Fixture selection, measured through the PRODUCTION recorder argv
  (`parecord --device=@DEFAULT_SOURCE@ --raw --rate=16000 --channels=1 --format=s16le`,
  `cmd/agent/cli_voice_client.go:424`), not guessed:
  `bash $SB/repro/fixture-pick2.sh` → `remap-noise-v80 → engine: 而是不是觉得我这个视频有点太短了？`,
  `remap-noise-v60 → /sil`, `remap-tone-v60 → /sil`. `bash $SB/repro/sil-probe.sh`
  (the engine on the file directly) → `noise400-6s → /sil` ✓ while
  `noise50-6s`/`zeros-6s` hallucinated Mandarin — i.e. the marker is NOT simply
  "digital silence", which is why the fixture is a fixed tone. `slice0/1 → /sil`,
  `slice2 → (empty)` for the same noise file, which is the non-stationarity that
  ruled the file out as the pty fixture (`$SB/evidence/silence-probe/`).
- Raw monitor vs remap source, measured: a RAW monitor starves `parecord`
  (`bytes=68238 (2.1s) peak=968` in a 6 s window) while the `module-remap-source`
  over the same monitor delivered contiguously (`bytes=195162 (6.1s) peak=1026`) —
  `$SB/evidence/fixture-pick/`. The rig therefore uses the remap source, the same
  shape the /voice re-land run measured (`devdoc/chat-voice-reland.md:213`).
- The rig's script had to be corrected by measurement, not by reasoning: the first
  shape typed the draft BEFORE `/voice`, and the grid then showed a real agent turn
  (`iter1 · ⚙ bash which gopi`) with an empty editing line — a slash word is only a
  command when the whole line is the command. Only the corrected order (enter the
  mode, then type the draft) reproduces the task's `draft-/sil` state.
- Audio host state restored: `pactl get-default-source` →
  `alsa_input.pci-0000_00_1b.0.analog-stereo`, and both modules this run loaded are
  unloaded (`pactl list short modules | grep gopi` → nothing) — `cleanup()` in
  `repro/sil-pty.sh` / `fixture-pick2.sh`, run on every exit path via `trap`; no
  host-level configuration was written (Rule 22/35).
- The engine is the real one, not a stub: every fixture measurement above is
  `/home/user/work/runtime/llama.cpp/build/bin/llama-funasr-cli` over
  `/home/user/work/funasr-models`, the same paths `gopi` resolves (the pty grid
  prints them in `[voice] engine local: models …`).
- `bash $SB/repro/sil-pty.sh …` writes nothing inside the worktree — the rig's
  working directory, the throwaway `GOPI_HOME` and every shot live under
  `$SB/repro/run-<label>/` and `$SB/evidence/` (Rule 37); the worktree's
  `git status --porcelain` was empty after the commit below.
- One observed flakiness, recorded rather than hidden: with the four-case scope, the
  run failed on `suite goroutine sweep: parked repository goroutines: 1` twice in
  13 attempts, the parked frame being
  `internal/lineedit.(*Editor).waitBandWrite` under
  `cmd/agent.(*chatInput).WriteNotice` — i.e. this suite's OWN band-writer
  instrumentation still inside its bounded wait
  (`internal/lineedit/bandwait.go:84`) when the close-out sweep samples. The
  pre-existing `/voice` cases alone (six runs) and each new case alone (six runs)
  never reproduced it, so it is not the new case's assertion; it is a sampling race
  between the sweep and a writer that the same suite spawns (log:
  `$SB/evidence/set-3.log`). It is NOT fixed here — see `## Not Done`.

## Not Done

- **No clean FULL-PACKAGE budget verdict for the committed tree.** `make test-budget
  PKGS='./cmd/agent'` (launched 00:57, returned 01:13 after 927 s) snapshots the
  package into its own tested binary at run start, and this run edited
  `cmd/agent/chat_voice.go` at 00:58:36 — inside that window — so its 12 failures are
  a stale-binary verdict, self-announced by the gate's own mtime guard
  (`bare_session_one_dir_e2e_test.go:78: …-tested-bin/gopi (mtime 00:57:48) is OLDER
  than …/cmd/agent/chat_voice.go (mtime 00:58:36) — this test drives the BUILT
  binary, so it would verify code that is not in that binary`). What WAS observed:
  branch-scoped smoke against the committed tree over exactly those test names
  (`ok … 12.065s`, `$SB/evidence/smoke-refutes-snapshot.log`), the three cases under
  test (`green-proof.log`), the existing `/voice` set six times with no failures, and
  the whole `TestChatVoice|TestVoice` selection (`ok … 73.017s`). Re-running the
  package budget is a fresh ~16-minute run and was not started inside this session's
  budget.
- **The `waitBandWrite` sweep flake** above is recorded, diagnosed to a
  frame/sampling description, and left alone: it is in `internal/lineedit`'s
  instrumentation plus the close-out sweep — outside this task's scope by the
  briefing (which forbids widening into `internal/lineedit`), and it needs its own
  minimal repro before anyone "fixes" it.
- **Engine-seam surfaces are intentionally unchanged**: `gopi tool voice_input`,
  the HTTP server and the stdio server still PRINT `/sil` for a speechless clip
  (reason 1 of the choice). If those surfaces ever gain an INSERT road, this
  judgement is what must move with them.
- **No test covers the pty rig in CI**: the reproduction needs a PulseAudio sink,
  the real engine and ~60 s per run, so it stays a recorded run (`repro/sil-pty.sh`
  in this run's session directory), not a suite case. The suite's equivalent is the
  scripted-recorder case, which is what the regression guard is.
