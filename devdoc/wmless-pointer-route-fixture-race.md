<!-- doc-class: historical -->
# Task Report: the WM-less pointer-route fixture waits on observable conditions, not on a settled rectangle

Task `01a0fff2-2bd5-780a-9476-bf654a8ddf1c`, branch
`task/01a0fff22bd5780a9476bf654a8ddf1c`.

`internal/shot`'s `TestCaptureWindowWithoutWindowManagerUsesPointerFallback`
failed intermittently on a WM-less private Xvfb. It does not any more: the
fixture now waits on the two conditions the failure depended on — the client
window's **map state** and the **stability of its captured content** — instead
of on a settled rectangle alone.

## Completed

### 1. Qualitative result (the failure rate and what it correlates with)

- **Failure is pre-existing on master and is a race, not load-dependence.** It
  reproduced on the run's own clean worktree (`374b951e`, master-merged, no
  changes) at load average 2.7/5.4/6.7 on this 4-core host, and it stopped
  reproducing the moment the machine quieted down. Measured sequence:
  1 fail on the first single run (`-count=1`), then **15/15 green** for the same
  command, then **4 of 150** inside one instrumented batch, then 150/150 green
  for a later batch of the unmodified fixture. Same code, same host, same
  command: the outcome is decided by WHEN the fixture looks, not by load.
- **The 44-pixel failure signature is always the same.** Every observed pixel
  mismatch was `44 of 24656 pixels differ`, all of them inside
  `x∈[3,6], y∈[3,13]` — the fixture client's cursor cell — with
  `screen=000000 / window=ffffff`.
- **Two distinct mechanisms, both measured, both timing races:**

  | # | mechanism | measured rate | evidence |
  |---|---|---|---|
  | A | pointer aimed at a client that is not MAPPED yet → pointer resolves the ROOT → engine correctly refuses | 17/200 aborted + 4/200 resolved-the-wrong-thing | `STAT resolved=false mapstate=IsUnMapped 17`, `STAT resolved=true mapstate=IsUnMapped 4` (a window idle §1 instrument) |
  | B | the window REPAINTS its cursor between the two captures (the pointer entering it is what focuses it on a WM-less server) | 1/150 | `d(window1,crop1)=44 d(window2,crop2)=0 d(win1,win2)=44` — the window's own content changed; a second capture immediately after was clean |

  Mechanism B is frozen deterministically: parking the pointer on the root and
  then moving it into the window changes exactly 44 pixels, hollow→filled
  cursor, landing **3.9–4.5ms** after the move (5/5 iterations).

### 2. Five whys

1. *Why did the test fail?* The window capture and the screen crop of the same
   rectangle disagreed in 44 pixels.
2. *Why did they disagree?* They were captured at two different instants, and
   between them the client repainted its cursor; on a WM-less server nothing
   takes the focus on its own, so **moving the pointer into the client is the
   focus event** and xterm redraws its cursor hollow→filled as a result.
3. *Why was the fixture not immune?* Its only settle criterion was
   `xrectSettled` — "a rectangle at least 20px on a side and fully inside the
   root". That predicate reasons about GEOMETRY, and the repaint is about
   CONTENT; geometry says nothing about whether the content is still moving.
4. *Why did the same predicate also fail the other way?* A window is reported
   with its full final geometry the moment it is created and only later mapped.
   Measured: a client answering `Absolute upper-left 40,30 / Width 184 / Height
   134` (i.e. `xrectSettled` true) simultaneously answers `Map State:
   IsUnMapped`. The pointer aimed at that rectangle is over the ROOT, so
   `CaptureX11Window` fails with "no active window" — the engine is RIGHT and
   the fixture is wrong. `xrectAwaitIndependentClient` accepted that window
   because it never asked the server whether the window was on screen.
5. *Why did nobody notice?* Both conditions are invisible to a single sample:
   the map state flips within ~30ms of window creation, and the cursor repaint
   lands ~4ms after the pointer move. A sampled-at-one-instant assertion can
   only catch whichever side of the flip it happens to land on — hence 14/15
   green vs 1/15 red with no code difference.

**Root cause (one sentence):** the fixture's readiness criterion described the
rectangle but not the server state that makes the rectangle meaningful — the
window's map state, and whether its content has stopped changing — so it raced
both transitions instead of waiting for them.

### 3. The fix — fixture only, no product code

`internal/shot/x11rect_test.go` (`+153/-3`), no change outside `internal/shot`
and none to product code:

- `xrectWindowMapped` — asks the INDEPENDENT oracle (`xwininfo`, through
  `xrectXwininfoBinary`) for the window's `Map State`, comparing against the
  new constants `xrectMapStateLabel` / `xrectMapStateMapped`. A missing oracle
  degrades to the previous criterion rather than failing.
- `xrectAwaitMappedClient` — bounded poll until that condition holds; a fatal
  with the measured state when it never does (Rule 15: never a silent pass).
- `xrectAwaitPointerRoute` — bounded poll until the engine's pointer route
  resolves the window the oracle named. Before that, the engine's refusal is
  CORRECT behaviour, not a failure.
- `xrectAwaitStablePixels` — bounded poll until `xrectPixelSettleFrames`
  consecutive pixel-identical captures of the resolved window; this is the
  content-side settle criterion the fix adds.
- `xrectAwaitIndependentClient` now requires the map state before accepting a
  window, so the geometry-based settle check can no longer outrun the server.
- All waits are bounded by the existing `xrectReadyBudget`/`xrectPollStep` and
  labelled with the project's reviewed `stylegate:allow-sleep` comment; no new
  fixed sleep was introduced anywhere.

### 4. Minimal regression test (new file)

`internal/shot/x11rect_mapping_test.go` —
`TestXrectWindowMappedIsNotASettledRectangle` reproduces the state with
`xdotool windowunmap` (no load, ~0.2s) and asserts the two facts the fix rests
on: after the unmap, the mapping oracle says "not on screen" **while** the
rectangle still passes the old settle criterion. Red proof: stubbing
`xrectWindowMapped` to `true` turns it red with
`window 0x20000c still reports IsViewable after xdotool windowunmap`; restored,
it is green.

## Not Done

- **Mechanism B is not separately pinned by a deterministic regression test.**
  It is *frozen* deterministically (the park→enter measurement, 5/5) and it is
  covered end-to-end by the 15-run acceptance, but the red-proof test reproduces
  mechanism A only. Producing B on demand would need the client to repaint on
  command, which xterm does not offer; I did not fake it, because a test that
  injects an artificial repaint would not be the same mechanism.
- **The `-count=1` acceptance is statistically weak on its own.** 15 single runs
  all pass, but the pre-fix control also passed 150 consecutive runs of the same
  command — so the acceptance is anchored by the paired instrumented batch
  (4/150 pre-fix) and by the mechanism measurement, not by the 15 alone.
- Mechanism A's 4/200 "resolved a window while unmapped" reading is reported as
  measured (the engine resolved something the oracle called unmapped) and is not
  separately explained; the fixture now waits that case out either way.

## Verification

- Build: `go build ./...` → exit 0 (no output).
- Tests: `make test-budget PKGS='./internal/shot'` → `1 finished, 0 with a
  failure`; `exit=0 outcome=pass stream=run-1791005444-660517.json`.
- Ran: `go test ./internal/shot/ -run TestCaptureWindowWithoutWindowManagerUsesPointerFallback -count=1` × 15 (loop, `echo "$out" | grep -q "^ok"` per run).
- Result: **pass=15 fail=0 of 15** (`/proc/loadavg` 2.22 3.03 4.48 at the end of
  the loop), plus `-count=150` twice → `ok` in 32.9s and 32.1s.
- Ran: `go test ./internal/shot/ -run TestXrectWindowMappedIsNotASettledRectangle -count=1 -v`
- Result: `--- PASS: TestXrectWindowMappedIsNotASettledRectangle (0.23s)`; with
  `xrectWindowMapped` stubbed to `true`, `--- FAIL: ... still reports IsViewable
  after xdotool windowunmap`.
- Commit: see the branch tip (the report is committed with the fix).

## Evidence (self-filed)

- Pre-fix failure, verbatim from the run's own clean worktree:
  `x11rect_test.go:495: WM-less window capture vs the screen crop of
  184x134+41+31: 44 of 24656 pixels differ — first: (3,3) want=000000
  got=ffffff, (4,3) ...` → `--- FAIL (3.24s)`.
- Unmapped-window state, measured (the predicate that decides mechanism A):
  one window answering `Absolute upper-left X: 40 / Y: 30 / Width: 184 /
  Height: 134` — which `xrectSettledGeometry` accepts — answering
  `Map State: IsUnMapped` at the same instant (`xwininfo -id`, reproduced on
  demand with `xdotool windowunmap`).
- Mechanism B, measured: `UNFOCUSED->FOCUSED repaint landed after 4.453628ms;
  changed pixels: [(3,3)ffffff->000000 … (6,13)ffffff->000000]` — 44 pixels, the
  cursor cell, 5/5 iterations (3.9–4.5ms).
- Batch control: instrumented fixture, 150 iterations, same command shape →
  `iterations=150 failing=1` with
  `FAIL d(window1,crop1)=44 d(window2,crop2)=0 d(crop1,win2)=0 d(win1,win2)=44`
  (`d(window2,crop2)=0` is the second capture being clean — the content moved,
  the rectangle never did).
- Classification of the run-to-run variance: `STAT resolved=true
  mapstate=IsViewable 179`, `STAT resolved=false mapstate=IsUnMapped 17`,
  `STAT resolved=true mapstate=IsUnMapped 4` over 200 iterations.
- Acceptance loop: 15 logs at
  `~/.gopi/sessions/01a0fff2-2bd5-780a-9476-bf654a8ddf1c/evidence/accept-1..15.log`,
  each ending `ok github.com/nick/gopi/internal/shot`.
- Raw logs, instrumented sources and the failing PNG sets are in that same
  evidence directory (NOT in the worktree — Rule 30/37): `hunt-round-1.log`,
  `loop-*.log`, `stale-*.log`, `preflight-control-count150.log`, `hunt/`
  (win1/win2/crop1/root1 PNGs of the failing frames),
  `instrument-*-test.go.txt`.
