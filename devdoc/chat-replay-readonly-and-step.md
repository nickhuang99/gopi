<!-- doc-class: historical -->
# Task Report: `gopi chat --replay` — the watcher's keyboard cannot edit the line it is watching, and a step is visible

Task `01a1236ebe917a72bbc36ad7d814f6b0` · branch `task/01a1236ebe917a72bbc36ad7d814f6b0`

## Completed

Both defects share one cause and one seam — `GateKey`, the replay's only key entry point.

**① An arrow key did not move the run's position, and any key resumed the replay.**
The arrows were already bound to `chatReplayControl.move()`, but `move()` **lifted the
pause** on any arrow while the driver's `waitWhilePaused()` releases as soon as
`paused==false`. Two consequences, both from the operator's report: an arrow pressed
while paused *resumed the recording* — so the watcher saw the run flow on and read it as
"the arrow did something other than what I asked"; and the OSD line printed the run's
beat only, never the **cursor** the arrow had just moved, so the key's real effect was
invisible. Fix (`chat_replay_control.go`): `move()` now **takes** the pause rather than
lifting it, and `chatControlMovedLine` names the cursor, the edge clause when it could
not travel, how the run came to be held (the step paused it / the pause already held
it), and **where the run really is** — the one number the watcher cannot see. `toggle()`
gained the same cursor clause. The old "— resumed" step suffix is gone: a step that
announced "resumed" beside a run it had just frozen would be the same lie in the other
direction.

**② A watching human could edit the line the recording is being typed into.**
`GateKey` returns "unclaimed" for every key the replay does not define, the editor then
applies it, and the replay driver is typing the recording into **that same buffer** — so
a watcher's Backspace deleted a character from the performance, irreversibly (the next
recorded chunk is appended to the polluted line). Fix: the editor gained its own
**read-only hold** (`internal/lineedit/inputlock.go`) — `SetInputLocked(true)` while the
replay owns the surface, released in `markClosing()`. The hold refuses only keys that are
( a) **typed by a human** (`ReadOriginTyped` — the recording arrives through the injector,
so the driver keeps typing) and (b) **would change the line** (`MutatesLine`: a rune, a
line break, a deletion, a kill, a yank, EOF, Tab; motion keys — arrows, Home/End,
PageUp/Down — and the inert keys are NOT mutations and stay available). The rule is
**one predicate** read by both arms, so the gate and the lock cannot disagree about the
same key.

Two invariants the fix had to respect, both enforced in code:

- **The editor is the right owner, not only the gate.** Asked at both places a decoded
  key reaches `handleKey` (the read loop *and* the flushed lone-Escape arm) — a hold the
  gate path obeyed and the flush path did not would be a promise with a hole in it.
- **The hold is a LOAN.** `markClosing()` gives the line back, and the *failure* arm of
  the driver (`chat_replay.go`) now calls it too: a driver that died on a refused key
  would otherwise leave the REPL holding a lock whose owner no longer exists.
- **No silent refusal** (Rule 15): a held key is counted (`InputLockDrops`) and reported
  to the editor's diagnostic stream naming the key — the defect family this pair of fixes
  closes is exactly "a key did nothing and said nothing".

## Files

- `internal/lineedit/inputlock.go` (new) — `MutatesLine`, `SetInputLocked` /
  `InputLocked` / `InputLockDrops`, `holdKey`, `keyDropped`, `warnInputLocked`.
- `internal/lineedit/editor.go` — two lock fields on `Editor`; both key paths route
  through the new `keyDropped` (= gate first, then hold).
- `cmd/agent/chat_replay_control.go` — `install()` takes the hold and keeps the editor
  handle; `releaseLine()`; `markClosing()` releases; `move()` takes the pause and the
  moved line names cursor + run + which clause holds it; `toggle()` cursor clause;
  `pausedNow()` reader.
- `cmd/agent/chat_replay.go` — `markClosing()` on the failure arm.
- `cmd/agent/chat_replay_readonly_test.go` (new) — the real-path case (pty → production
  chat input → editor → control layer).
- `cmd/agent/chat_replay_watcher_repro_test.go` (new) — the two minimal reproductions,
  written against pre-fix API **on purpose**, so `git show HEAD:<file>` reproduces RED.
- `cmd/agent/chat_replay_control_test.go` — updated to the corrected semantics (a step
  holds the pause; a jump that cannot travel still says so).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → `EXIT=0`; `go vet ./cmd/agent/` clean.
- Tests: `make test-budget PKGS='./cmd/agent ./internal/lineedit'` → **PASS** (both
  packages, whole package tree, not only the new cases).
- **Ran (Rule 31) — the surface itself, with real keys**: a live
  `bin/gopi chat --replay cf12cdab…` on a **real pty** (the operator's own session
  log), with the watcher's keys written into that pty — `X`, then `\x7f`
  (Backspace), then the pause, Left, resume and quit keys. Observations, from the
  pty transcript:

  - the **watcher's editing keys are refused and reported**:
    `[gopi] editor: the surface that owns this line is holding it — X was dropped;
    the line is not writable while the surface drives it (dropped so far: 1)`
    (`… Backspace was dropped; … dropped so far: 2` in the earlier attempt) — the
    recording itself kept being typed into the line, and no watcher character
    reached it;
  - **the recording itself kept typing through the refused keys** — the check that the
    hold refuses the WATCHER and not the performance. In the same pty transcript the
    line's own text is typed to completion (`[system] [t+17s] — first request of this
    session.`), and the drop notice lands *between* two of the recording's own chunks
    (`…this session` → drop #1 → `…this session.`): the watcher's `X` never appears in
    the line, and the driver appended its next chunk afterwards;
  - the run's own close, after the quit key: `[gopi replay] the replay ended at the
    watcher's request after beat 1 of 1`, `turns typed: 1/1`, exit code 0 — the
    quit key is claimed by the gate *while the hold is armed*, which is ② and ①'s
    ordering in one observation;
  - **the pause/step OSD sentences, captured live in a SECOND run** — the first
    live attempt used a **one-turn** recording, whose takeover is immediately
    followed by the typed turn, so a key sent mid-performance had well under a
    second of window (five attempts, 0.5 s–2 s after `playback:`); the fix was to
    pick a recording with room. `gopi chat --replay 42980518-…` (the same
    recording family, replayed turn by turn) on a real pty, keys typed only after
    the `playback:` line, `0.6 s` apart:

    ```
    [gopi replay] PAUSED at beat 0 of 1; cursor at beat 1 — space resumes, ←/→ step
    the cursor, q/Esc leaves the replay
    [gopi replay] step back: cursor at beat 0 of 1 — the pause HOLDS the run there —
    the run has typed 0; space resumes typing to the cursor, q/Esc leaves the replay
    [gopi replay] step forward: cursor at beat 1 of 1 — the pause HOLDS the run
    there — the run has typed 0; space resumes typing to the cursor, q/Esc leaves
    the replay
    [gopi replay] resumed at beat 0 of 1; cursor at beat 1
    [gopi replay] the replay ended at the watcher's request after beat 1 of 1 …
    [gopi replay] tool results served from the recording: 0 (unrecorded: 0), model
    requests: 0 (script-exhausted: 0), turns typed: 1/1
    ```

    Every sentence ①'s fix introduced is there: the pause names **both** beats
    (the run's, and the cursor's), each step names the cursor it moved to, the
    edge clause, **how** the run came to be held, and **where the run really is**
    (`the run has typed 0` — while the cursor sits one beat further on, the number
    the watcher cannot see otherwise). The keys did **not** resume the recording:
    both steps report `the pause HOLDS the run there`, and the only resume in the
    transcript is the deliberate space. Exit code 0, `turns typed: 1/1`.
    Transcript: `…/artifacts/osd-1.log` + `osd-excerpt.txt`.
- Evidence under `/home/user/.gopi/sessions/01a1236e-be91-7a72-bbc3-6ad7d814f6b0/artifacts/`:
  `live-pty.log` (the pty transcript), `live-replay-pty.py` (the harness: pty via
  the stdlib, keys typed only AFTER the takeover line), `live-held-excerpt.txt`.

- **Red → green, observed (Rule 5 / Rule 10.5)**: the two minimal reproductions were run
  against the PRE-FIX tree (`git show HEAD:<file>` restored for the four touched files,
  new files held aside) and then against the fixed tree:

```
--- FAIL: TestReproWatcherBackspaceDoesNotEditTheLine
    a watcher's Backspace edited the line the recording is being typed into:
         got  "the recording typed thi"
         want "the recording typed this"
--- FAIL: TestReproStepDoesNotResumeTheRecording
    a step RESUMED the recording: the arrow released the parked driver
ok  	github.com/nick/gopi/cmd/agent	2.045s   (pre-fix: FAIL)
--- PASS: TestChatReplayWatcherCannotEditTheLine
--- PASS: TestChatReplayStepKeepsThePauseAndNamesTheRun
--- PASS: TestChatReplayLineReturnsToTheReplWhenTheReplayEnds
--- PASS: TestReproWatcherBackspaceDoesNotEditTheLine
--- PASS: TestReproStepDoesNotResumeTheRecording
ok  	github.com/nick/gopi/cmd/agent	3.459s   (post-fix: PASS)
```

## Not Done (honest)

- The live-run evidence uses a **piped pty** (`script` / the stdlib `pty` module,
  keys written into the pty), not XTEST keys on an X display: hosta's recorded
  recipe (#7023/#7024) needs Xvfb + a real `xterm`, and this run's budget did not
  allow standing that up as well. The two arms agree on the mechanism under test
  (the key arrives at the editor as TYPED through the pty either way) and the
  test-level reproductions cover the semantic; a reviewer wanting the X-level arm
  can follow the memory recipe verbatim.
- **The live run's first arm did not show the pause/step OSD sentences** — see
  `Verification`: that recording is one turn long and its takeover is immediately
  followed by the typed turn, so a key sent mid-performance has almost no window.
  A second live run on a recording with room did capture them (quoted in
  `Verification`), so ①'s live arm is now covered; the piped-pty caveat below
  still stands for both.
- The hold does **not** cover in-process writers of the line (`InsertIntoLine`
  producers): they are not a watcher's keystrokes and `MutatesLine` does not claim
  to judge them. The file's header says so rather than implying a wider promise.
- `chatControlStepBackwardGlyph`/`ForwardGlyph` naming in the pause line is
  unchanged; the step *lines* no longer name the glyph the watcher pressed beyond
  the `how` word.


## Evidence (self-filed)

- `git status --porcelain` in the worktree at close-out: clean (only the intended edits
  are committed; nothing untracked left behind).
- Pre-fix RED run: `go test ./cmd/agent/ -run 'TestRepro...'` printed the two FAIL blocks
  quoted above (full logs: `/home/user/.gopi/sessions/01a1236e-be91-7a72-bbc3-6ad7d814f6b0/artifacts/red.txt`).
- Post-fix GREEN run: the same command with the fix in place printed `ok … 3.459s` plus
  the three real-path cases (log: `…/artifacts/green.txt`).
- Live run: `…/artifacts/live-run.log` + `live-exit.txt`; the second live arm (the
  pause/step OSD, real pty, multi-turn recording): `…/artifacts/osd-1.log`,
  `osd-excerpt.txt`, harness `live-osd-pty.py` — keys `pause, left, right,
  pause-toggle, quit`, exit code 0.
