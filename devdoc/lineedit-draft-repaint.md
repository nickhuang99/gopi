<!-- doc-class: historical -->
# The draft's frame: the four repaint conditions that did not know about the draft segment

**Task** 01a121fc-eea7-71f2-a58e-db02637ffcca (deliverable E) · **Branch**
`task/01a121fceea771f2a58edb02637ffcca` · **Base** `926a71342` · host `hosta`.

## Completed

- **Real-device proof on the shipped surface (see `## Verification` for the
  transcript).** A real `gopi chat --voice-audio <file>` runs over a real pty
  (100×30, gnome-terminal's own probe responder), `/voice` is typed and Enter
  paced, and the REAL streaming engine (volcengine `streaming_common`) speaks
  partials while nobody touches the keyboard. Read back through the run's own
  screen dump (`--dump-screen`):
  - **fixed binary**: `you> 开放时间早上九点` — the partial is on the editing line
    *while the session is still speaking*, with no key pressed;
  - **pre-fix binary** (same probe, `HEAD~1`'s `editor.go` rebuilt into
    `bin/gopi-prefix`): `you>` is **empty** at the same instants, and the sentence
    appears only once the capture ends (`[gopi] voice mode OFF …`) — the
    operator's *"I see nothing until I press Esc"*, reproduced at the real
    surface.
  - **RED/GREEN at the REAL surface, one probe, two binaries.** Same script
    (`--voice-audio` + `--dump-screen` over a real pty, 100×30):
    - fixed build: `you> 开放时间早上九点` at **t+2s, no key pressed**; then
      `you> 开放时间早上9点至下午5点。` after the settle;
    - pre-fix build (`bin/gopi-prefix`, built from `HEAD~1`'s `editor.go`):
      `you>` is **empty** at those same instants — the text arrives only when the
      capture ends. The probe script and both dumps live in the task's session
      evidence directory (`…/01a121fc-eea7-71f2-a58e-db02637ffcca/artifacts/`:
      `voice-live-probe.py`, `probe-run2.txt`, `probe-run-prefix.txt`,
      `live-screen.dump`, `prefix-screen.dump`).

- `internal/lineedit/editor.go`: the draft segment's level (`draftPending()`) is
  now part of the four places the read loop asks *"is there anything new to put on
  the screen?"*, each merged per its own branch's semantics:
  - **1500** (the `EventWake` seam) — added to the draw predicate, so the wake a
    streaming producer raises for its partial is enough to earn a frame. The
    branch's `inputPending()` yield above it covers the draft the same way it
    covers every other pending thing: input already on the wire is read first.
  - **1565** (the completion-wake / bounded-poll arm of `EventNone`) — same
    predicate, the arm the operator's defect landed in: it is the read the loop
    PARKED through when the draft was stated.
  - **1595** (the idle-wait arm, which already carried `geometryChanged()`) —
    added beside it; a bounded wait is the read outcome a draft raised in the park
    window meets.
  - **1804** (the post-key seam, the "changed the buffer?" conclusion of one read)
    — added, because a draft stated while the read was being decoded leaves the
    buffer UNCHANGED (the draft is not in `Value`) and the unchanged-buffer test
    would otherwise hide it.
- `internal/lineedit/draft_repaint_visibility_test.go` (new): three cases over the
  **real read loop** (`Run` on its own goroutine, the package's scripted source,
  the reserved chat layout), asserting the **rendered editing row** — never a
  producer counter as the assertion's subject:
  - `TestDraftReachesTheScreenWithoutAKeystroke` — one `SetDraft`, no key
    injected, the row must carry it; plus the level is consumed by the draw.
  - `TestDraftDoesNotSpinTheReadLoop` — the no-spin half: after the drain, three
    idle reads arrive and the redraw count must not move across a quiet window.
  - `TestDraftIsDrawnWhenTheStatusRowHasNoNews` — the same property with the one
    flag that can stand in for it removed (see *What masked the defect* below).
- `devdoc/lineedit-draft-repaint.md` (this file).

## What masked the defect (measured, not reasoned about)

The end-to-end pty acceptance case for this path
(`cmd/agent` `TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey`)
**passes on the pre-fix read loop as well.** Measured by swapping in
`git show HEAD:internal/lineedit/editor.go` and re-running it: `ok … 2.180s`.

The mechanism: `/voice` **publishes its status when it enters**
(`voiceChatMode.markLive` → `publishStatus` → `chatInput.VoicePublishStatus` →
`PublishStatus`), which raises `statusPending()` **and** wakes the loop. A draft
stated just after the mode opened therefore finds a flag already outstanding and
rides along in the frame the STATUS earned. `sink.Draft` is called with no
publish of its own (`chat_voice_live.go:276`), so a draft stated at any moment
when the status row has no news — a second utterance, a mode already painted, the
publish already consumed — has nothing to ride along in and waits for the next
keystroke, which is the operator's *"nothing appears until I press Esc"*.

The third case exists to pin exactly that: it publishes the status, waits for the
loop to **consume** the flag (`statusPending()` false — the draw clears it), and
only then states the draft. It is red on the pre-fix read loop, so it is a
deterministic regression guard for the observed behaviour rather than an
accident of timing.

## Files

- `internal/lineedit/editor.go` — four conditions (+ their reasoning comments).
- `internal/lineedit/draft_repaint_visibility_test.go` — new; three cases.
- `devdoc/lineedit-draft-repaint.md` — this report.

## Not Done

- **The run was made with `--voice-audio` (a FILE the live arm reads), not with
  the microphone.** The reason is honest and mechanical, not a missing device:
  `hosta` HAS a capture device (`card 1: PCH … ALC256 Analog`, capture 1) and it
  delivers real signal — measured, `parecord … --raw` for 6 s gave
  `bytes=191404 samples=95702 peak=3323` (artifacts: `mic-probe.raw`) — but a
  microphone reproduces what the ROOM says, so a run over it is not reproducible
  and cannot be scripted. The two paths that would script it are both out of
  bounds here: a virtual sink (a new resident PulseAudio module = a persistent
  dependency, Rule 35), and speaking into the mic on the operator's behalf (Rule
  27 — do not generate load to satisfy a test). The live arm's audio source is
  chosen by ONE function (`voiceAudioSourceFor` / `captureOptions`), so a FILE
  source exercises the same stream, the same engine and the same draft road; what
  the file run therefore does NOT cover is the capture device's own liveness and
  packet pacing. That parameter remains unverified by this task.
- **What was NOT verified here**: a HUMAN speaking Chinese into a real terminal
  (gnome-terminal/xfce4-terminal — xterm's default bitmap font has no CJK glyphs
  and shows `□` regardless of this fix). Reproduction steps for whoever has the
  room for it:
  1. `gopi chat` in gnome-terminal or xfce4-terminal;
  2. `/voice`, then **speak without touching the keyboard**;
  3. the partial transcript must appear in the editing line *as you speak*; on the
     pre-fix build nothing appears until a key (or Escape) is pressed;
  4. press Ctrl+G (the finish key), then Escape: the settled sentence must remain
     once on the line, and the mode must leave only on Escape.
- The remaining four deliverables of this epic (A–D) are out of this task's scope,
  as the briefing states.

## Verification

- **Build**: `go build ./...` → OK. `go vet ./internal/lineedit/` → clean.
- **Tests**: `go test ./internal/lineedit/ -count=1` → `ok … 31.142s` (whole
  package, including every pre-existing draft/answer/band/menu case).
- **RED/GREEN pair** (one shot each, same test selector, only `editor.go`
  swapped):
  - GREEN, fixed editor: `--- PASS: TestDraftReachesTheScreenWithoutAKeystroke`,
    `--- PASS: TestDraftDoesNotSpinTheReadLoop`,
    `--- PASS: TestDraftIsDrawnWhenTheStatusRowHasNoNews` → `ok 0.632s`.
  - RED, `git show HEAD:internal/lineedit/editor.go` in place (0 `draftPending()`
    sites in the file): all three `--- FAIL` —
    *"the editing row never carried the draft … never reached a repaint"*,
    *"the pending draft was never applied by the read loop"*,
    *"… with the status row unchanged"*.
- **Gates**: `go test ./internal/standards/ -run 'ScreenAssertion|NamedLimitsRule1|Mirror' -count=1`
  → `ok 1.636s` (the screen-assertion gate reports a test that synchronizes on a
  producer counter; the two `Repaints()` reads in the new file are marked
  `//screen-ok:` as direct MEASUREMENTS of a quiet window).
- **Ran** (Rule 31): three runs, at three levels — the package case, the pty
  acceptance case, and the shipped binary driven over a real pty:

  ```
  go test ./internal/lineedit/ -run 'TestDraftReachesTheScreenWithoutAKeystroke|TestDraftDoesNotSpinTheReadLoop|TestDraftIsDrawnWhenTheStatusRowHasNoNews' -count=1 -v
  go test ./cmd/agent/ -run 'TestChatVoiceLive' -count=1 -v
  bin/gopi chat --voice-audio <session>/artifacts/voice-16k-s16le.pcm --dump-screen   # under a pty
  ```

  1. the pty acceptance case for this exact path (`cmd/agent`, real pty, the
     production input layer and read loop):
     `go test ./cmd/agent/ -run 'TestChatVoiceLive' -count=1 -v` →
     `--- PASS: TestChatVoiceLive_DraftsGrowInTheLineThenSettleOnTheFinishKey (0.49s)`,
     `--- PASS: TestChatVoiceLive_ReportsTheUtterancesItRan (0.33s)`,
     `ok github.com/nick/gopi/cmd/agent 1.892s`. And the SAME case on the pre-fix
     editor → `ok … 2.180s`, which is the masking measurement quoted above.
  2. **the shipped binary itself**, over a real pty, with the REAL streaming
     engine, no keystroke during the utterance — the transcript and the two
     binaries are in `## Completed` and in the session evidence directory.
- **Commit**: see the branch's own history (`git log --oneline -1`).

- **Not this task, and NOT caused by it** (measured, then filed rather than
  fixed — one task, one defect): `cmd/agent`'s
  `TestVoiceEnginePrecedenceFlagBeatsEnvBeatsSettings` is RED in ANY environment
  that already exports `GOPI_AUDIO_ENGINE` — which includes every gopi task/chat
  process, because `internal/config/env.go:190` publishes the settings value into
  the environment. Its FIRST assertion (`cli_voice_engine_test.go:134-140`,
  "settings alone → doubao") is the one assertion in that function with no
  `t.Setenv(config.AudioEngineEnv, …)`; the other three have one. Measured here:
  `go test ./cmd/agent/ -run '^TestVoiceEnginePrecedenceFlagBeatsEnvBeatsSettings$'`
  → `--- FAIL … settings rung: engine = "streaming", want doubao`, and
  `env -u GOPI_AUDIO_ENGINE …` (same command, same tree) → `ok`. Excluded as mine
  twice over: `git diff --stat 926a71342..HEAD -- cmd/agent internal/config` is
  empty, and the same failure reproduces with `926a71342`'s `editor.go` in place.
  Filed on peerbox's ledger: task `01a1220e-e06f-7da9-b57a-b46ec0f5ca52`
  (briefing `artifacts/bug-briefing-voice-engine-env-rung.md`, attachments
  `cmdagent-voice-run.txt`, `red-run.txt`).

## Evidence (self-filed)

- Red run (unfixed): `grep -c 'draftPending()' internal/lineedit/editor.go` → `0`,
  then the three new cases → 3 × `--- FAIL`, exit 1, quoted above.
- Green run (fixed): same selector → 3 × `--- PASS`, `ok`, quoted above.
- Package suite: `ok github.com/nick/gopi/internal/lineedit 31.142s`.
- Standards gate: `ok github.com/nick/gopi/internal/standards 1.636s`.
- pty acceptance: `ok github.com/nick/gopi/cmd/agent 1.892s`.
- No-spin claim: the case itself (redraw count unchanged across a 40 ms quiet
  window on an idle line, with the draft still on the row afterwards). It is a
  measurement of redraw DECISIONS, not a wait for a screen.
