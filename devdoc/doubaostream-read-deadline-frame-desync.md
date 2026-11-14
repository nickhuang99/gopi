<!-- doc-class: historical -->
# Task Report: the live streaming reader no longer desyncs on a mid-frame read deadline

Task `01a11dc1-7476-76bc-bc21-a326c42e481a` · branch `task/01a11dc1747676bcbc21a326c42e481a`

## The failure this fixes

A live `/voice` dictation session on the doubao-stream engine died mid-utterance:

```
[gopi] voice mode FAILED — doubao-stream engine: streaming asr: read: unknown WebSocket opcode 0x3
(logid 20261009065552AAB621D34D9FCDD0E5C6) (reqid e61a5799-9eaa-4c97-ac2a-e52dccd771b2)
```

(evidence: `~/.gopi/sessions/01a11d7370a3730c84dabcc80db2985a/artifacts/live-drill-pane.txt`)

### Root cause (5-why)

1. **Symptom**: a payload byte (`0x03`) arrived as a WebSocket opcode, i.e. the
   reader was parsing in the middle of a frame.
2. **Mechanism**: `wsConn.readFrame` reads a frame with `io.ReadFull`, so a read
   deadline that expires **after the first header byte but before the second**
   leaves the framing state *half consumed*.
3. **Why it was not noticed**: `Result.readOne` armed a deadline per read
   (`sessionReadBudget = 200ms`) and mapped **every** timeout to `(nil, nil)` —
   "silence". A half-eaten frame was therefore reported as a resumable state.
4. **Why that is a conflation**: a timeout at a frame **boundary** (nothing
   consumed) *is* resumable — the next read starts where the last one did, and a
   quiet asynchronous service is exactly what that looks like. A timeout **inside**
   a frame is not resumable at all: the rest of that frame is still in the socket
   and the next parse reads it as a header.
5. **Root**: the two facts were the same code path, so `readLoop` read straight
   past the interruption. Nothing about it was timing-specific — any live session
   held open across a quiet stretch could hit it; the file arm could not, because
   `Transcribe` writes all its audio before draining.

## Completed

- **`internal/doubaostream/ws.go`**
  - `wsConn.frameBytesIn` counts the bytes of the CURRENT frame that `readFrame`
    has consumed, and the count is updated from the byte counts `io.ReadFull`
    returns **between** its two calls (header, payload). The result is exact, not
    a heuristic: `0` at an error means the read stopped at a frame boundary.
  - A frame is read in **at most two** `io.ReadFull` calls: the whole header
    (fixed two bytes + length escape + mask key) into one `wsMaxHeaderBytes`
    buffer, then the payload. The new constant `wsMaxHeaderBytes` (Rule 1) exists
    so a header cannot be half-read across two calls for a reason of our own
    making.
  - `SetReadDeadline(0)` now means **no deadline** and CLEARS the socket's
    deadline (`SetReadDeadline(time.Time{})`) instead of arming
    `defaultReadTimeout` — a deadline left standing from an earlier phase fires
    mid-frame exactly as a per-frame one would. The per-read arming inside
    `readFrame` is gone: the deadline is armed once per phase.
- **`internal/doubaostream/client.go`**
  - `ErrFrameDesync` (exported sentinel, named per Rule 1/13) +
    `APIError.Is`, so `errors.Is(err, ErrFrameDesync)` works on the error a caller
    actually receives.
  - `readOne`'s three outcomes are now distinct: a timeout at a frame **boundary**
    stays `(nil, nil)` (silence); a failure with `frameBytesIn > 0` becomes
    `*APIError{Op:"read"}` whose cause is `fmt.Errorf("%w: %w", ErrFrameDesync,
    err)` (the sentinel AND the transport error); anything else is the transport
    failure it is. Rule 15: a state that is not resumable is never reported as one
    that is.
- **`internal/doubaostream/session.go`**
  - `sessionReadBudget` → `sessionFlowReadTimeout = 0`: **the audio-flow phase
    arms no deadline at all**, which makes the mid-frame desync *impossible by
    construction* while the operator is speaking rather than merely detected.
    `readLoop` counts its quiet frames only once the end-of-stream flag is out
    (before that, being quiet is not a fact about anything) and shows a desync to
    the terminal verdict via `recordErr`.
  - `sessionDrainReadTimeout = defaultFinalReadTimeout`: the drain deadline,
    armed **deliberately in `Finish`, only if the reader is still reading** (a
    reader that already returned is not waited for). `sessionFinishWait` is
    re-derived from that same drain timeout, so Finish's bound is unchanged in
    shape and still reported through `FinishTimedOut` (unchanged, verified below).
- **Tests** (all on a **real loopback socket** — `socketPair`, never an in-memory
  buffer):
  - `ws_read_deadline_test.go` — the mid-frame timeout is a failure, `(0)` means a
    cleared/no deadline, silence at a boundary is still silence, and the live
    `readLoop` survives a frame that arrives in two pieces across a 450 ms pause.
  - `session_finish_drain_test.go` — the drain reports a mid-frame interruption as
    `Op:"read"` + `ErrFrameDesync` with an EMPTY transcript, and a merely silent
    service still ends the session through `sessionQuietFrames` without a desync
    and without `FinishTimedOut`.
  - `live_voice_drill_test.go` — the opt-in live drill (`GOPI_LIVE_VOICE_DRILL=1`)
    against the real vendor endpoint.

## Not Done

- **The full live `/voice` UI drill with a real microphone** (the tmux pane run
  that measured the original failure) was not re-run: it needs an interactive
  terminal and a human speaking into `gopilivesrc`. The drill that WAS run is the
  same live code path (real vendor endpoint, real `Open` → `Say` → quiet flow
  phase → `Finish`) with 3 s of idle flow phase, which is the condition the fix is
  about, and it is quoted below. A human-driven UI confirmation remains open.
- The `cmd/agent` voice arm (`chat_voice_live.go`) is untouched: it consumes
  `Finish`/`FinishTimedOut` exactly as before, which is why the bounded-wait
  constraint holds.
- `readMessage`'s documented per-frame deadline re-arm (the anti-starvation
  guarantee for a message assembled from many frames) was initially dropped when
  the per-read arming was removed; it is restored in `readFrame`, now conditioned
  on a deadline being configured at all (`readTimeout > 0`, i.e. the drain phase).
  So the starve protection is intact where a deadline exists, and absent — by
  design — in the flow phase, which is the point of the fix.
- `drain` (the file arm) keeps `defaultFinalReadTimeout` per read and now also
  benefits from the desync guard — that is the intended reachable case, not a gap.

## Verification

- Build: `go build ./...` → OK; `go vet ./internal/doubaostream/` → clean;
  `gofmt -l` → empty.
- Tests: `make test-budget PKGS='./internal/doubaostream ./cmd/agent'` — quoted
  verbatim in `## Evidence`, where the run, its stamp and its ONE failure (a
  `cmd/agent` time-boundary flake unrelated to this diff) are laid out with the
  attribution measurement that rules this diff out.
- `go vet ./internal/doubaostream/` → clean; `gofmt -l internal/doubaostream/` → empty.
- **Red before / green after** — the new tests, verbatim, run twice. First on a
  detached worktree of the PRE-FIX commit `823305a17` (the commit the live
  failure was measured at) with only the new test file copied in:

```
$ cd /home/user/workspace/gopi-01a11dc-pre-fix && go test ./internal/doubaostream/ \
    -run 'MidFrame|ClearingTheReadDeadline|SilenceAtAFrameBoundary|LiveReadLoop' -v
=== RUN   TestReadDeadlineMidFrameIsAnErrorNotSilence
    ws_read_deadline_test.go:71: PRE-FIX: the live session reported exactly this = "streaming asr: read: unknown WebSocket opcode 0x3"
    ws_read_deadline_test.go:82: a read deadline that expired MID-FRAME was reported as silence (msg=<nil>, err=<nil>), but the framing state is half-consumed and is NOT resumable; the read that followed got read tcp 127.0.0.1:38096->127.0.0.1:44239: i/o timeout
--- FAIL: TestReadDeadlineMidFrameIsAnErrorNotSilence (0.30s)
=== RUN   TestClearingTheReadDeadlineMakesAMidFrameTimeoutImpossible
    ws_read_deadline_test.go:115: SetReadDeadline(0) armed 5s — a deadline during the audio flow can land inside a frame, which is the desync this phase exists to prevent
--- FAIL: TestClearingTheReadDeadlineMakesAMidFrameTimeoutImpossible (0.00s)
=== RUN   TestSilenceAtAFrameBoundaryIsStillSilence
--- PASS: TestSilenceAtAFrameBoundaryIsStillSilence (0.15s)
=== RUN   TestLiveReadLoopSurvivesASlowFrame
    ws_read_deadline_test.go:219: the live reader never folded a frame that arrived in two pieces across a 450ms pause (the flow phase must arm NO read deadline)
--- FAIL: TestLiveReadLoopSurvivesASlowFrame (3.45s)
FAIL	github.com/nick/gopi/internal/doubaostream	4.929s
```

  Then on this branch (post-fix):

```
$ go test ./internal/doubaostream/ -run 'FinishDrain|FinishStillEnds|LiveReadLoop|MidFrame|Clearing|Silence' -v -count=1
--- PASS: TestFinishDrainReportsAMidFrameInterruption (1.50s)
    drain reported the desync after 1.500677573s: streaming asr: read: the framing state cannot be resumed (1 bytes of the current frame were consumed) (logid logid-drain) (reqid reqid-drain): read interrupted inside a WebSocket frame: read tcp 127.0.0.1:49040->127.0.0.1:45617: i/o timeout
--- PASS: TestFinishStillEndsOnTheServicesSilence (3.00s)
--- PASS: TestReadDeadlineMidFrameIsAnErrorNotSilence (0.15s)
--- PASS: TestClearingTheReadDeadlineMakesAMidFrameTimeoutImpossible (0.30s)
--- PASS: TestSilenceAtAFrameBoundaryIsStillSilence (0.15s)
--- PASS: TestLiveReadLoopSurvivesASlowFrame (0.45s)
```

- **Ran (live drill, the real vendor endpoint)** — `GOPI_LIVE_VOICE_DRILL=1
  go test ./internal/doubaostream/ -run TestLiveVoiceDrill -v -count=1`:

```
=== RUN   TestLiveVoiceDrill
    drill: 64868 bytes of PCM16 (2027 ms of audio), credential /home/user/.doubao/v2_voice.env
    session open: cluster=volcengine_streaming_common logid=20261009072026D3E5D9BA2992A03A77F4
    PARTIAL 1 "无" (definite=false)
    PARTIAL 2 "无人" … PARTIAL 6 "无人度是多少" (definite=false)
    sent 11 packets (2027 ms of audio)
    flow phase: quiet for 3s (the pre-fix reader armed a 0 deadline per read)
    PARTIAL 7 "温度是多少？" (definite=true)
    RESULT text="温度是多少？" audio_ms=2027 partials=7 elapsed=6.254176004s timed_out=false
--- PASS: TestLiveVoiceDrill (6.45s)
```

  The stretch matters: the pre-fix reader crossed that 3 s with a 200 ms deadline
  re-armed per read (~15 chances to land mid-frame), and `elapsed` shows the
  session stayed alive across it and still produced the transcript. **Result**:
  the transcript that the failure above destroyed came back, and `FinishTimedOut`
  is `false` — the bounded wait is intact.

- Commit: **the branch tip of `task/01a11dc1747676bcbc21a326c42e481a`**, whose
  subject is "fix(voice): a read deadline that lands inside a WebSocket frame is a
  reported desync, never silence" and which is the single commit above `823305a17`
  (`git rev-list --count 823305a17..HEAD` = 1); `git status --porcelain` prints
  nothing. ONE HONEST NOTE, corrected after review: an earlier draft of this line
  quoted `433f9662c`, the commit as it stood BEFORE this report was amended into
  it. That object is now an ORPHAN with no ref containing it
  (`git for-each-ref --contains 433f9662c` → empty) and it differs from the branch
  tip by these 7 added lines alone (`git diff --stat 433f9662c <tip>` → this file,
  7 insertions, 1 deletion). The branch tip hash is therefore deliberately NOT
  spelled here: writing it would amend the commit again and re-roll it a second
  time. Read it off the branch.

## Evidence (self-filed)

- `make test-budget PKGS='./internal/doubaostream ./cmd/agent'` — run TWICE in this
  review, both times on this branch, and the two runs disagree about the second
  package, which is exactly why the attribution below is spelled out. Run A is
  INVALID and its own artifact says so: it was killed by a SIGQUIT this review
  sent to the test binary while diagnosing a stall (SIGQUIT dumps and then
  EXITS). Its stream carries the cause verbatim —
  `run-1791502198-3502256.json`, `TestBoundDaemonStaysAliveWhileItsSpawnerLives`,
  `SIGQUIT: quit` at `07:42:37`. Run B (`run-1791502974-3618315.json`, started
  after the kill, `stamp: 2026-10-09T07:42:54+08:00 host=peerbox`) is the honest one:

```
      5.9s  pass  github.com/nick/gopi/internal/doubaostream
    876.8s  fail  github.com/nick/gopi/cmd/agent

--- failures (assertion/build; the budget did NOT trip) ---
  --- FAIL: TestTaskStatusSelectorAndRowIDPrintTheSameVerdict (0.09s)
stamp: 2026-10-09T07:42:54+08:00 host=peerbox pkg=./internal/doubaostream ./cmd/agent
       budget=25m elapsed=895s load_start=3.72 4.71 5.31 load_end=2.64 4.39 4.91
       cores=4 exit=1 outcome=fail stream=run-1791502974-3618315.json
```

  **The scope this change owns is green in Run B: `internal/doubaostream` — 40
  verdicts, zero failures** (`pass ... 5.9s`, the only row for that package). The
  `cmd/agent` failure is NOT this diff's, and the attribution is three
  measurements, not a claim: (1) `git diff master...HEAD --stat -- cmd/agent/`
  prints NOTHING — this branch's `cmd/agent` is byte-identical to `823305a17`;
  (2) the failing test's own file has a zero-line diff against master
  (`git diff master...HEAD -- cmd/agent/cli_tasks_status_selector_test.go | wc -l`
  → `0`); (3) run alone it passes — `go test ./cmd/agent/ -run
  TestTaskStatusSelectorAndRowIDPrintTheSameVerdict -count=1` → `ok 3.210s`, and
  `-count=8` → `ok 3.081s` (8/8). Its message shows why it is a time-boundary
  flake: the assertion compares the row-id render against the branch render BYTE
  FOR BYTE, and the render carries a clock-derived field — the two runs differ in
  exactly one character, `recorded: ... start=2026-10-09T07:43:12+08:00` vs
  `...13+08:00`. Filed as bug task `01a11df4-153d-787d-aa19-cfb97d256e1f`
  (Rule 26: repaired by dispatching, never by waiting); this branch does not
  touch it.
- Pre-fix red proof: `git log --oneline -1` in the detached worktree printed
  `823305a17 feat(voice): streaming dictation — …`, and the run above is that
  tree's output (3 FAIL / 1 PASS). The worktree was removed afterwards
  (`git worktree remove --force`) and this branch's tree is the only one written.
- `FinishTimedOut` reachability: `cmd/agent/chat_voice_live.go:286`
  (`report.TimedOut = sess.FinishTimedOut()`) is unchanged, and
  `TestFinishStillEndsOnTheServicesSilence` drives the silence path to
  completion with `FinishTimedOut() == false`.
- The socket in every new test is a real loopback TCP pair (`socketPair`,
  `net.Listen`/`net.Dial`) — no test calls `readFrame` on a buffer.
