<!-- doc-class: historical -->
# Task Report: the /voice leaving note's second half must be the REASON's own

**Task:** 01a11f8b-c6f6-7dc5-aad9-3f8c572798f3 · **Branch:** `task/01a11f8bc6f67dc5aad93f8c572798f3`
**Feature doc:** `devdoc/voice-left-note-causal-fidelity.md` (this file) ·
**Code:** `cmd/agent/chat_voice.go` · **Test:** `cmd/agent/chat_voice_left_note_test.go`

## Completed

- **H1/H2 were separated by measurement, and H1 is the answer: the exit is
  SELECTED CORRECTLY; the SENTENCE was wrong.** On a real pty, with the real binary
  and a FINITE audio source (`/voice` reading a 14 s WAV), the capture ends under a
  still-ON mode, the sentinel `errVoiceStreamClosed` comes back from
  `voiceChatRecorderStaged.Round`, and the loop does exactly what it documents —
  `if !m.stale(entry) { m.leave(chatVoiceLeftByCaptureEnded) }`. The reason field on
  the grid is right (`the capture ended`); what was wrong is the note's fixed second
  half, which stated the causality backwards.
- **The note now composes `reason × tail`, one pairing, one reader.**
  `chatVoiceLeftTails` (`cmd/agent/chat_voice.go`) pairs each of the five reasons
  with its second half and `chatVoiceLeftTail` is its only reader, so the reason
  field is exactly as attributable as before while the tail can no longer contradict
  it. An unlisted reason falls back to the stopping sentence (the unmarked case), so
  adding a reason is never a silent blank.
- **A regression case drives all FIVE reasons on a real pty**, judged on the painted
  grid (Rule 10.2): `TestChatVoice_LeftNoteSecondHalfMatchesItsReason`. Per reason it
  asserts the composed sentence IS on the grid AND the other direction's sentence is
  NOT — the second assertion is the one that is red on the pre-fix code.
- **The red proof is a mutation, not a claim**: with
  `chatVoiceLeftTails[chatVoiceLeftByCaptureEnded]` set back to the stopping tail
  (the pre-fix composition in one line), the case fails on the capture-ended
  sub-case only — the four mode-driven reasons stay green, which is the shape the
  defect had. Reverted immediately; the green run is the reverted tree.

## Not Done (honest)

- **Two of the three paths into `the capture ended` were not driven on a real pty**:
  a live microphone dying mid-mode (arecord killed), and the audio file ending
  *while an engine round is in flight* (here the source ran out between windows, at
  the same instant the mode was starting a round — the exit landed, so the arm is
  exercised, but the in-flight variant is not separately shown). All three reach the
  identical arm (`voiceChatRecorderStaged.Round` → the sentinel → the ONE
  `m.leave(chatVoiceLeftByCaptureEnded)` → the same pairing), which the regression
  case pins through the recorder seam, but the pty evidence is one path, not three.
- **The tail's prose is not machine-verified against the event beyond the pairing**:
  the case asserts which tail a reason owns and that the two directions differ, not
  that the sentence is true of the world in every scenario. A future third direction
  would need a new entry; nothing fails if someone adds a reason and lets it fall
  back (that fallback is stated in code, and is the honest unmarked case).
- **The three exits that write no leaving note at all are untouched** (a generation
  mismatch, a stale round, the session's own teardown) — the teardown is
  deliberately the SILENT half, which is pre-existing design, not a gap this task
  opened.

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` → exit 0, no output.
- **Tests (green, final tree) — RE-RUN THIS SESSION on the shipped test:**
  ```
  CGO_ENABLED=0 go test -count=1 -v -run 'TestChatVoice_LeftNoteSecondHalfMatchesItsReason' ./cmd/agent/
  --- PASS: TestChatVoice_LeftNoteSecondHalfMatchesItsReason (91.12s)
  PASS
  ok  	github.com/nick/gopi/cmd/agent	91.552s
  ```
  (five sub-cases, `-v` output: the_toggle / the_stop_word / the_operator's_Escape /
  a_failed_round / the_capture_ended_under_a_live_mode — all PASS). The non-verbose
  form the acceptance names:
  ```
  CGO_ENABLED=0 go test -count=1 -run 'TestChatVoice_LeftNoteSecondHalfMatchesItsReason' ./cmd/agent/
  ok  	github.com/nick/gopi/cmd/agent	91.875s
  ```
- **Tests (red, pre-fix composition restored by mutation):** the same command with
  `chatVoiceLeftByCaptureEnded: chatVoiceLeftStoppingTail` in `chatVoiceLeftTails`
  (the pre-fix sentence, one line). **RE-MEASURED on the shipped test this session**
  — the quote below is the grid the run printed, and the run's full verbose output
  is kept at `$GOPI_TASK_REPORT_PATH`'s directory as `red-run-verbose.txt`
  (not committed, Rule 37):
  ```
      chat_voice_left_note_test.go:181: timed out waiting for the leaving note for "the capture ended" ([gopi] voice mode OFF (the capture ended) — nothing is left to record; the text already on the line is kept. No further transcript will be inserted.
          )
          --- grid ---
  …
          [gopi] voice mode OFF (the capture ended) — the recorder is stopping; the text already on the line is kept. No further transcript will be inserted.
  --- FAIL: TestChatVoice_LeftNoteSecondHalfMatchesItsReason (94.14s)
      --- PASS: TestChatVoice_LeftNoteSecondHalfMatchesItsReason/the_toggle (30.24s)
      --- PASS: TestChatVoice_LeftNoteSecondHalfMatchesItsReason/the_stop_word (30.22s)
      --- PASS: TestChatVoice_LeftNoteSecondHalfMatchesItsReason/the_operator's_Escape (30.24s)
      --- PASS: TestChatVoice_LeftNoteSecondHalfMatchesItsReason/a_failed_round (0.22s)
      --- FAIL: TestChatVoice_LeftNoteSecondHalfMatchesItsReason/the_capture_ended_under_a_live_mode (3.21s)
  ```
  The failure is the case's FIRST assertion (the paired sentence is absent from the
  grid — the row instead carries `the recorder is stopping`, quoted above by the
  grid dump itself); the "other direction" assertion is never reached, which is why
  the red shape is a timeout naming the wanted sentence AND printing the row that
  was actually there. The four mode-driven reasons stay green — the defect's exact
  shape: one template, right for four exits, backwards for the fifth. The mutation
  was reverted before the green run; `sha256sum cmd/agent/chat_voice.go` =
  `4390ddb3e0850bb63cfb84968165f1f37cc2d2ef0f0d0f948feaf0069208fb56` for the green
  one, with `git diff --stat` empty at that hash.
- **Ran (Rule 31 — the real surface, the real binary, a real pty, a real run log):**
  ```
  SB=~/.gopi/sessions/01a11f8b-c6f6-7dc5-aad9-3f8c572798f3/pty-lednote
  CGO_ENABLED=0 go build -o "$SB/home/gopi-post" ./cmd/agent/
  env "GOPI_HOME=$SB/home" GOPI_VOICE_ENGINE=local \
      GOPI_CHAT_VOICE_AUDIO="$SB/fixture.wav" \
      LANG=C.utf8 LC_ALL=C.utf8 TERM=xterm-256color GOPI_HUMAN_TERMINAL=1 \
      python3 devdoc/evidence/sgr-grid/termgrid.py "$SB/home/gopi-post" 100 30 "$SB/script2.txt"
  ```
  `fixture.wav` is a 14 s 16 kHz mono PCM16 finite clip; `script2.txt` types `/voice`,
  waits for the sentence under test, and shoots the printed grid.
- **Result (PRE-fix binary, `artifacts-pre/3-left-note.grid.txt`, copy at
  `~/.gopi/sessions/<task>/pre-left-note.grid.txt`) — the defect, verbatim:**
  ```
  [gopi] voice mode OFF (the capture ended) — the recorder is stopping; the text already on the line i
  s kept. No further transcript will be inserted.
  ```
- **Result (POST-fix binary, `artifacts-post/3-left-note.grid.txt`, copy at
  `post-left-note.grid.txt`) — the fix, verbatim, same instrument, same fixture:**
  ```
  [gopi] voice mode OFF (the capture ended) — nothing is left to record; the text already on the line
  is kept. No further transcript will be inserted.
  ```
  and the instrument's own run log for that run:
  ```
  waitfor 'gopi chat' OK (t=2.8s)
  waitfor 'VOICE MODE ON' OK (t=4.1s)
  waitfor 'nothing is left to record' OK (t=26.3s)     ← the anchor IS the sentence under test
  shot 3-left-note (t=26.9s)
  TERMGRID_EXIT=0
  ```
- **Commit:** `872c05a7f` `fix(voice): the leaving note's second half follows its reason`
  (docs follow-ups on the same branch: `2249b1d0c`, `bbf7be658`, `790e02a73`, and
  `bbf4b9bf4` — the re-measured red/green quotes). **Branch:**
  `task/01a11f8bc6f67dc5aad93f8c572798f3`.

## Files

- `cmd/agent/chat_voice.go` — `chatVoiceLeftNote` composes `reason × tail`; the
  pairing table `chatVoiceLeftTails` and its only reader `chatVoiceLeftTail`.
- `cmd/agent/chat_voice_left_note_test.go` — the five-reason regression case (new, 212 lines).
- `devdoc/voice-left-note-causal-fidelity.md` — this report + its `devdoc/DOC-INDEX.tsv` row.
- `devdoc/evidence/sgr-grid/termgrid.py` — the pty instrument (pre-existing, unchanged).
- Evidence, NOT committed (Rule 37 — session dir): `$GOPI_TASK_REPORT_PATH`,
  `pre-left-note.grid.txt`, `post-left-note.grid.txt`, `pty-script-{pre,post}.txt`,
  `red-run-verbose.txt`, and the whole pty fixture tree `pty-lednote/`
  (`fixture.wav`, both binaries, `artifacts-{pre,post}/`, the scripts).

## Deliverable declaration (Rule 6d)

- **Branch:** `task/01a11f8bc6f67dc5aad93f8c572798f3` (this task's own, in the
  worktree `/home/user/workspace/gopi-01a11f8bc6f67dc5aad93f8c572798f3`).
- **Recovery:** `git show task/01a11f8bc6f67dc5aad93f8c572798f3:devdoc/voice-left-note-causal-fidelity.md`
  (the code: `git show task/01a11f8bc6f67dc5aad93f8c572798f3:cmd/agent/chat_voice.go`).
- A linked worktree shares the main repo's refs, so the commits above ARE on that
  branch in the main repo — no push is involved.

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./...` → `BUILD_ALL_OK` (exit 0), on the same tree the green
  test ran on (both in one command, `sha256sum` of the changed file printed above them).
- Red run (RE-MEASURED this session, mutation in place): `--- FAIL:
  TestChatVoice_LeftNoteSecondHalfMatchesItsReason (94.14s)` with the capture-ended
  sub-case FAIL and the four mode-driven sub-cases PASS; the failing case's own grid
  dump carries `[gopi] voice mode OFF (the capture ended) — the recorder is stopping; …`
  (full verbose output saved at
  `~/.gopi/sessions/01a11f8b-c6f6-7dc5-aad9-3f8c572798f3/red-run-verbose.txt`).
- Green run (RE-MEASURED this session, mutation reverted, `sha256sum
  cmd/agent/chat_voice.go` = `4390ddb3e0850bb63cfb84968165f1f37cc2d2ef0f0d0f948feaf0069208fb56`,
  `git diff --stat` empty): `--- PASS: TestChatVoice_LeftNoteSecondHalfMatchesItsReason
  (91.12s)` / `ok github.com/nick/gopi/cmd/agent 91.552s`.
- The two pty binaries are PROVABLY different trees, measured by their own bytes:
  `grep -aF` on each finds the old sentence in `home/gopi` (built 16:14, pre-fix) and
  `nothing is left to record` ONLY in `home/gopi-post` (built 16:28, post-fix); and
  `gopi-post` rebuilds BIT-IDENTICAL from the committed tree
  (`sha256 11795059f8b699ab14e1eeb848e5d2adc5f035f76aefb20f86b682065afbca4a`, equal to
  the binary the pty run used) — so the post-fix grid is the committed code's own output.
- The two grid dumps differ in EXACTLY the sentence (30 rows each, `diff` = 2 lines),
  so the run's other rows are held constant between pre and post — the change under
  test is the only difference in what the operator saw.
- Real pty, pre-fix: instrument log `waitfor 'voice mode OFF' OK (t=50.2s)`,
  `shot 3-left-note (t=50.6s)`, `TERMGRID_EXIT=0`, and the grid row quoted above
  (observed).
- Real pty, post-fix: instrument log `waitfor 'nothing is left to record' OK (t=26.3s)`,
  `TERMGRID_EXIT=0`, grid row quoted above (observed).
- `git status --porcelain` in the worktree → empty after the commits (Rule 30),
  with `sha256sum cmd/agent/chat_voice.go` unchanged at the green hash.
