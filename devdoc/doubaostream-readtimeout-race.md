<!-- doc-class: historical -->
# Task Report: `internal/doubaostream` — the read-deadline race

**Task** `01a11e1a1b43731b9c6881a709d0cbdc` · branch `task/01a11e1a1b43731b9c6881a709d0cbdc`
**Symptom** `go test ./internal/doubaostream/ -count=1 -race` reported `WARNING: DATA RACE`
at `ws.go`'s read-deadline write, on every session test — while `make test-budget`
(which runs **without** `-race`) stayed green, so the defect lived in the suite for
as long as the reader goroutine has existed.

## 5-why: the root cause

1. **What fails?** Two goroutines write the same `time.Duration` field (`wsConn.readTimeout`,
   `ws.go:265`) with no synchronisation: the frame reader (`Session.readLoop` →
   `Result.readOne` → `wsConn.readFrame`, `ws.go:297`) and the session's write side
   (`Session.Finish` → `wsConn.SetReadDeadline`, `session.go:277`).
2. **Why do they both write it?** Because the field is *the* per-connection record of
   "which read budget is in force", and the design has **two phase owners**: the reader
   re-arms per frame from it, and the write side flips it at the phase change
   (flow → drain) when `Finish` sends the end-of-stream flag.
3. **Why does one field have two owners?** Because the read budget *is* the read phase,
   and the phase is decided by a fact on each side: the reader knows whether a frame is
   in flight; `Finish` knows whether the end-of-stream flag is out. Neither can compute
   the other's half, so both wrote the shared cell.
4. **Why was that not caught when the reader goroutine was introduced?** The field was
   written when one goroutine owned both directions ("send a packet, read its response";
   the comment on the type said so). Adding the reader goroutine gave the READ side an
   owner of its own, and the type's own "not safe for concurrent use" note stopped being
   true for this field — while `make test-budget` runs without `-race`, so nothing said so.
5. **Why is it a defect and not a benign race?** Because the value is not decorative: it
   decides **whether a deadline is armed at all**. Which write wins decides whether a
   deadline can land *between two `io.ReadFull` calls* — the framing state half-consumed
   and unrecoverable, which is exactly the `errFrameDesync` / `"unknown WebSocket opcode
   0x3"` defect family (memory #8835, live dictation death). A torn read of this value is
   therefore a torn *stream*.

**Root cause (one sentence).** The read budget was modelled as a *shared cell* ("the
connection's current deadline") when it is really *two different facts* — "what budget
did the caller publish?" (write side) and "which budget is this frame read under, and
where may a deadline be applied?" (reader) — and the fix is to stop sharing the cell:
publish the value, and let the reader take it at the **frame boundary**, the one instant
where the framing state is whole.

## Completed

- **`internal/doubaostream/ws.go`** — the fix:
  - `wsConn` gains a `readMu sync.Mutex` guarding the read-side state (`readTimeout` and
    `frameBytesIn`). `readTimeout` keeps its name — it is no longer *recalled from* by the
    reader (`readFrame` never reads it), it is the value the caller **published**.
  - `SetReadDeadline(d)` is now a single funnel: it takes `readMu`, stores `readTimeout`
    and arms the socket via `armDeadline` — one critical section, so no reader can be
    reading a halfway-changed value.
  - `armDeadline` is the **one** place the socket's read deadline is touched, and its
    docs state the both-directions rule (positive arms, zero/negative clears).
  - `readFrame` takes its deadline at the **frame boundary** (`readDeadlineForFrame`,
    which copies `readTimeout` and arms under the same lock) and then reads the frame —
    `readMu` is **never held across an `io.ReadFull`**, so a read parked on the socket
    cannot hold the lock a `Close` needs.
  - `frameBytesIn` became private behind `setFrameBytesIn` / `addFrameBytesIn` /
    `frameBytesRead` — the same guard, and the reason: the race detector cannot see
    "this happens to be the reader's own goroutine" and must not have to.
  - The published value is still applied to the socket immediately **when it is safe**
    (the arming and the copy are one critical section); a mid-frame read then picks it up
    at its next frame boundary — which is exactly the existing reader-side behaviour of
    `Result.readOne`'s per-call `SetReadDeadline`, now made non-racy instead of
    which-write-wins.
- **`internal/doubaostream/client.go`** — two lines: `readOne` reads the frame's consumed
  byte count through `frameBytesRead()` instead of the now-private field.
- **`internal/doubaostream/ws_read_deadline_race_test.go`** (new) — the minimal regression
  test (Rule 10.5): two goroutines and **no clock**. The reader folds 32 real frames off a
  real socket while a second goroutine flips the deadline between the flow phase's `0`
  and the drain phase's budget. Red on the pre-fix tree, green on the fixed one.
- **`internal/doubaostream/session_test.go`** — a **pre-existing flake** the `-race` run
  exposed (see "Not Done" for the honest accounting): `TestSessionPushesPartialWhileAudioIsStillGoing`
  waited for the handshake's partial on the *server's receipt of a packet* — two goroutines,
  two facts. Measured red on **4 of 5** runs with the committed product files (and **2 of 3**
  with the read-deadline fix in place, so it is independent of this change) before the fix.
  Repaired by waiting on the session's own counter (`Session.awaitPartials`), which returns at
  once when the partial has arrived, so the "still speaking" window the test is about is
  unchanged. **Honest weakening:** the assertion no longer requires the partial to have beaten
  the server's receipt of the LAST packet; it requires it within the grace. That is what the
  client can actually observe deterministically; the original form asserted scheduler timing.

## Not Done

- **The `-race` flake is a second defect, and it was NOT introduced by this fix.** It is
  in the same package and was blocking the acceptance criterion (`-race` green), so it is
  repaired here; the root cause is written up above and in the test's comment. It is not
  the read-deadline race and should not be read as part of it.
- **`-race` was run on this package only** (the branch's scope, Rule 27/`internal/smokegate`).
  A whole-tree `-race` run is the off-peak heavy path and was not run.
- **The live `/voice` drill was not re-run** (`GOPI_LIVE_VOICE_DRILL=1` needs the vendor
  credential and bills audio); the package's live-shaped tests (`socketPair`, real sockets)
  cover the surface this change touches.
- **`writeTimeout` was left unguarded** — by design, and stated: it is written only where
  the write deadline is configured (`Open`, `Close`) on the ONE write side the session's
  calling contract names, and read by `writeFrame` from that same goroutine. The race
  detector agrees (the whole suite is green with `-race`).

## Verification

- **Build:** `go build ./internal/doubaostream/` → OK; `go vet ./internal/doubaostream/` → OK;
  `gofmt -l internal/doubaostream/` → empty.
- **Tests:** `go test ./internal/doubaostream/ -count=1 -race` → `ok` (0 `WARNING: DATA RACE`,
  31.3 s); repeated 3 more times → `ok`, 0 races each. **After the delivery-point
  merge onto master** the same command → `ok 41.952s`, then `ok 34.900s`, then
  `ok 34.893s` — 0 races in all three.
  `make test-budget PKGS='./internal/doubaostream'` → `exit=0 outcome=pass` (twice:
  before the merge, and on the merged tree — stamp `2026-10-09T09:34:43+08:00`, 58 s).
  `gopi tool doccheck` → "the devdoc corpus is consistent".
  On the standards side, `go test ./internal/standards/ ./internal/stylegate/ -count=1`
  fails **on files this branch does not touch** and fails identically on a clean
  master worktree (`/home/user/work/GoPi`, `1ab1d8ff0`): Rule 20 escape
  `internal/shot/x11.go:1064 getRootImage`, stylegate test-setenv-scope hits in
  `internal/lineedit/`, `internal/tools/` — both packages are byte-identical to
  master here (`git diff --stat master -- internal/shot/ internal/lineedit/` empty),
  so this is pre-existing master state, not a regression from this change.
- **Ran:**
  ```
  # PRE-FIX (product files at their committed state, new test file added):
  go test ./internal/doubaostream/ -count=1 -race -run 'TestReadDeadlineFlipIsNotARace'
  # POST-FIX:
  go test ./internal/doubaostream/ -count=1 -race
  ```
- **Result:** PRE-FIX the minimal test fails with
  `WARNING: DATA RACE` — `Write at …` `ws.go:265` (SetReadDeadline) vs
  `Previous read at …` `ws.go:297` (readFrame); the full pre-fix suite produced **7**
  distinct race reports (all rooted at the same field) and `exit=1`. POST-FIX: the minimal
  test is green in 2.8 s and the full suite is green with **0** races.
  Product-file sha256 (restored-tree proof):
  | file | pre-fix | post-fix |
  |---|---|---|
  | `ws.go` | `a5c904512d2cd931cb8da59a0ef1c1428cf0f784f250096f3a78b825e609c029` | `770e8f45a2d07991d7c194cf54bf5cc51e2a2b282f632b329037db528927542c` |
  | `client.go` | `d81e472689085baa9e8f4239ff684845d0a7c5435948b6120cb3909b858d394a` | `35740772fa95dcf7f5372b9dbb219bb0b3c341755482f173b7031dfc3a8fe24b` |
  | `session.go` | `db1e2eb9a5484e80a866e04b1cefd0a2887d8cd46651c6c4acefcce86ee5137f` | *(unchanged by this fix)* |

  **After the delivery-point merge** `session.go` reads
  `dc5e7cc69865af17c52c84ae9c366452ba6c81b906ce86d1b1ac2b3542c7dc79` — that is
  **master's own content** (task `01a11dfe`'s `releaseSocket`), which this branch
  never edited: `git diff --stat master -- internal/doubaostream/session.go` is
  empty at the merge commit. `ws.go` / `client.go` are the fix's own hashes above,
  unchanged by the merge (master did not touch them).
- **Commit:** `994e3d4d2` (the fix), `198ea4150` (the flake repair), `50b3f65e7`
  (this report), `a790d900a` (the delivery-point merge) — branch
  `task/01a11e1a1b43731b9c6881a709d0cbdc`.

## Delivery-point sync (the merge, measured)

The branch was cut from a master that then moved: **19 commits** landed while this
run was working, and two of them touched this very package — `01a11dfe` fixed a
second teardown defect (`Session.releaseSocket`, `Close` after `Finish` reporting
`use of network connection`, `devdoc/doubaostream-session-teardown.md` +
`internal/doubaostream/session_teardown_test.go`) and rewrote part of
`session_test.go`. So the alignment ran LAST (Rule 30a), at hand-over, not as a
milestone along the way.

- `git merge master` → **one conflict**, `devdoc/DOC-INDEX.tsv`: both sides
  appended a single row in the same alphabetical slot
  (`doubaostream-readtimeout-race.md` here, `doubaostream-session-teardown.md`
  upstream). **Both rows kept**, no row dropped.
- `internal/doubaostream/session.go`, `session_test.go`,
  `session_teardown_test.go` **auto-merged onto master's state** — the merged
  session.go carries master's `releaseSocket` / `closeRequested` and none of this
  branch's change (the fix is entirely in `ws.go` + `client.go`, which master did
  not touch). Verified after the merge, not assumed:
  `grep -c awaitPartials internal/doubaostream/session_test.go` = 4 (this branch's
  wait) and `grep -c 'teardown must report the socket as already released' …` = 1
  (master's assertion) — both facts coexist.
- **Re-measured on the merged tree**: `go test ./internal/doubaostream/ -count=1
  -race` → `ok 41.952s`, **0 races**; the merge commit `a790d900a` carries it.
  `make test-budget PKGS='./internal/doubaostream'` → `exit=0 outcome=pass`.

**The merge commit is where the deliverable is**: the fix's own commits
(`994e3d4d2`, `198ea4150`, `50b3f65e7`) sit under `a790d900a`, so the branch head
is a descendant of master (Rule 6's commit-path step 7) and carries both.

### Semantics preserved (no regression)

The change is a **synchronisation** change, not a behaviour change; the package's existing
semantic tests all pass (`-race` green):

- `TestReadDeadlineMidFrameIsAnErrorNotSilence` — a mid-frame expiry still surfaces as
  `*APIError{Op:"read"}` wrapping `os.ErrDeadlineExceeded` (the desync guard is intact).
- `TestClearingTheReadDeadlineMakesAMidFrameTimeoutImpossible` — `SetReadDeadline(0)`
  still clears (and the reader still arms nothing during the flow phase).
- `TestSilenceAtAFrameBoundaryIsStillSilence` — a boundary timeout is still `(nil, nil)`.
- `TestFinishDrainReportsAMidFrameInterruption` — the drain's deadline can still cut a
  frame and the interruption is still reported as `ErrFrameDesync`, not hidden.
- `TestFinishStillEndsOnTheServicesSilence`, `TestSessionCloseIsIdempotent`,
  `TestFinishFlushesTheTailAndFlagsIt`, `TestLiveReadLoopSurvivesASlowFrame` — the phase
  transitions, the drain wait, and Close-after-Finish are unchanged.

## Evidence (self-filed)

- `go test ./internal/doubaostream/ -count=1 -race` (pre-fix, product files restored from
  `HEAD`) → `exit=1`, `grep -c 'WARNING: DATA RACE'` = **7**; the first report's frames:
  `ws.go:265` (write, via `client.go:546` ← `session.go:367`) vs `ws.go:265` (previous
  write, via `session.go:277`).
- `go test ./internal/doubaostream/ -count=1 -race -run 'TestReadDeadlineFlipIsNotARace'`
  (pre-fix) → `exit=1`, one race: `ws.go:265` write vs `ws.go:297` read.
- `go test ./internal/doubaostream/ -count=1 -race -run 'TestReadDeadlineFlipIsNotARace'`
  (post-fix) → `exit=0`, `ok … 2.805s`.
- `go test ./internal/doubaostream/ -count=1 -race` (post-fix, full suite) → `exit=0`,
  `races=0`, `ok … 31.313s`; three further full runs → `ok`, `races=0`.
- `git status --porcelain` at close: empty (Rule 30).
- `make test-budget PKGS='./internal/doubaostream'` → `exit=0 outcome=pass` (stamp
  2026-10-09, host peerbox).
- **The pre-existing flake, measured on the unmodified tree**:
  `go test ./internal/doubaostream/ -count=1 -race -run 'TestSessionPushesPartialWhileAudioIsStillGoing'`
  against the committed `ws.go`/`client.go` → `FAIL` 3/3, then 4/5, with
  `OnPartial fired 4–5 times … want 6`. After the `awaitPartials` change → `ok`.
