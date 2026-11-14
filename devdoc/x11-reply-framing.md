<!-- doc-class: historical -->
# X11 Reply Framing: Draining Interleaved Event Frames (MappingNotify vs GetInputFocus)

Branch: `fix/x11-reply-framing` · files: `internal/shot/x11.go`,
`internal/shot/xactuate.go`, `internal/shot/x11replyframing_test.go`,
`internal/shot/cov_shot_x11_test.go`

## The defect

`(*x11Client).readReply` assumed "the next 32 bytes are my reply" and treated
any type byte other than `{0,1}` as an error. The X11 stream is not that:
replies, errors and EVENTS share it.

The trigger is a protocol side effect, not a server bug. `GetKeyboardMapping`
is the request that REGISTERS a client for keyboard-map changes: from then on
the server sends `MappingNotify` (event type 34) whenever the mapping changes —
and on a fresh display it queues one immediately. `XTestFakeInput` is a
**no-reply** request, so the code path

```
GetKeyboardMapping  → reply (read)
XTestFakeInput × N  → no reply (nothing read)
GetInputFocus       → reads the NEXT 32 bytes … which are the MappingNotify event
```

consumed the event as the GetInputFocus answer and reported
`GetInputFocus: unexpected reply type 34`. Nothing was desynchronised on the
wire — the frame simply was never read — so the 32-byte event stayed in the
socket buffer and EVERY later round-trip on that connection returned it too:
the failure was **sticky** for the life of the connection. Live evidence
(`strace`, fresh Xvfb, before the fix): `read(5, "\"…", 32)` (0x22 = 34, an
event frame) immediately followed by
`gopi act: GetInputFocus: unexpected reply type 34`.

Root cause, 5-why: the failure is a *frame model* error, not a timing error.
The client modelled the stream as request→reply pairs; the protocol allows
events to be interleaved anywhere; and because events carry no reply sequence
and no length, a reader that guesses gets a silent wrong answer rather than a
loud error. The only correct model is classification + sequence matching.

## The fix

`internal/shot/x11.go`

1. **Frame classification.** One type byte means three things:
   `0` = error, `1` = reply, `>= 2` = event. The send-event bit (`0x80`) is part
   of that byte, so `x11EventTypeMasked` masks it off before classifying —
   otherwise a synthetic event is "unknown".
2. **Sequence-matched reply read.** Every request increments the client's
   sequence counter (X11 requests carry no sequence number; the server counts
   them and echoes the count). `readReply` now compares the frame's
   `header[2:4]` against `c.seq` and REFUSES a reply that belongs to another
   request instead of answering a different question. A stale ERROR is drained
   (it belongs to an already-answered request); a stale REPLY is refused loudly,
   because that is the desynchronised-connection signature.
3. **Bounded event drain.** An event frame is drained and never returned as an
   answer. The drain is bounded by `x11MaxDrainedEvents = 16`; past the bound
   the read fails with an explicit "desynchronised" error — an unbounded drain
   would turn a broken stream into a hang, the worst failure mode.
   `noteEvent` inspects the drained MappingNotify: `request ∈ {modifier,
   keyboard}` sets `c.keymapDirty`, and the pointer-mapping value is ignored.
4. **Events carry no reply sequence** (`x11FakeEventSequence = 0`), which is
   what makes `sequence != c.seq` a usable discriminator between a stray reply
   and an event's zeroed sequence field.

`internal/shot/xactuate.go`

`keycodeForLocked` consumes the signal: when `keymapDirty` is set the cached
keycode table is dropped and the next resolution refetches
`GetKeyboardMapping`; `loadKeymapLocked` clears the flag *after* a successful
parse. A stale keycode types the wrong character with no error anywhere, so the
flag is what makes that failure impossible rather than merely unlikely.

## Verification

| Check | Result |
| --- | --- |
| `go build ./...` | OK |
| `go vet ./internal/shot/` | OK (no output) |
| `go test ./internal/shot/ -count=1` | `ok github.com/nick/gopi/internal/shot 39.594s` (whole package, live tests included) |
| `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | OK |
| New tests, `-run 'TestReadReply\|TestMappingNotify\|TestKeymapRefresh\|TestTypeTextAfterMappingNotify'` | 15 subtests PASS |
| `TestKnownBugTypeTextMappingNotifyDesync` (live Xvfb) | PASS — takes the `first == nil` branch: "the first act verb on a fresh display succeeded (the MappingNotify frame was drained)" |
| `TestLiveDisplayFocusClickTypeObservable` (live Xvfb + xev) | PASS |

Live proof, fresh `Xvfb :94`, xev focused, freshly built binary:

```
$ DISPLAY=:94 ./bin/gopi act type --text "abc"
{"action":"type","display":":94","dry_run":false,"pointer":"320,240","focus":"0x200001","root":"640x480","detail":"typed 3 character(s)"}   exit=0
$ DISPLAY=:94 ./bin/gopi act info            # second verb, same display
{"action":"info",...,"focus":"0x200001",...}                        exit=0
$ DISPLAY=:94 ./bin/gopi act type --text "ab" # still frame-aligned
{"action":"type",...,"detail":"typed 2 character(s)"}               exit=0
```

xev on the same display logged exactly the injected keys —
`keysym 0x61 0x61 0x62 0x62 0x63 0x63` (a/b/c) then `0x61 0x61 0x62 0x62`
(a/b), 5 KeyPress/KeyRelease pairs in total.

The drain itself is visible on the wire (`strace -f -e trace=read -x`, fresh
`Xvfb :92`, fixed binary): the reader logged `read(5, "\x22\x00\x03\x00…")`
(MappingNotify, type 34) and then accepted `read(5, "\x01\x00\x09\x00…")` —
reply, sequence 9 — i.e. the event was drained and the matching reply returned,
where the old binary failed on the same stream.

## Tests added (`internal/shot/x11replyframing_test.go`)

The fake X server gained three capabilities (stray frames written before its
reply, a forced focus-reply sequence, a mutable keyboard layout) and now
numbers requests per connection and echoes the number — a real server's
behaviour, and what makes sequence assertions possible over a socket.

- `TestReadReplyDrainsMappingNotifyBeforeFocusReply` — the deterministic
  reproduction of the filed defect (the live test only *hopes* the event is
  queued; here it is guaranteed).
- `TestReadReplyDrainsAllInterleavedFrameKinds` — MappingNotify + an input
  event + a send-event-bit frame in front of one reply; not sticky afterwards.
- `TestReadReplyDrainIsBounded` — exactly `x11MaxDrainedEvents` events are
  recovered from; one more is refused as desynchronised.
- `TestReadReplyRefusesOffSequenceReply` — a previous request's answer, a
  plausible focus-window-looking sequence, and the 16-bit wrap boundary.
- `TestReadReplyAcceptsMatchingSequence` — positive control, so the refusal
  test cannot pass because the harness never lets a reply through.
- `TestReadReplyFailsLoudlyOnTruncatedStream` — events then a dead stream:
  the drain surfaces an error (bounded by `covStrayStreamBudget`), never waits.
- `TestMappingNotifyMarksKeymapDirtyAndRefreshes` — keyboard/modifier mapping
  (incl. send-event bit) invalidates + refetches once; pointer mapping and an
  unrelated event do NOT (refetching on every stray event would defeat the cache).
- `TestKeymapRefreshPicksUpChangedMapping` — behavioural: the server's mapping
  actually changes and the next TypeText presses the NEW keycode (asserted on
  the wire bytes, not on the internal flag).
- `TestTypeTextAfterMappingNotifyKeepsConnectionAligned` — actuator-level:
  first verb on a connection that queues a MappingNotify succeeds and stays aligned.

`TestKnownBugTypeTextMappingNotifyDesync` was rewritten from "documents the bug"
into the live regression guard: the old version PASSED on either branch (it
tolerated the desync and only logged it), which is exactly the shape of test
that cannot catch a regression.

## Not Done

- **Live keymap-CHANGE test.** `TestKeymapRefreshPicksUpChangedMapping` proves
  the invalidation deterministically on the fake server, but the equivalent
  live test could not be built here: on this host's Xvfb both
  `xmodmap -e "keycode 60 = b B"` (rc=0, map shown unchanged) and
  `setxkbmap de` (rc=0, map unchanged) are inert — Xvfb has no keyboard-driver
  device to reprogram, so no live client can force a real MappingNotify. The
  dirty-flag path IS exercised live on every fresh display (the initial
  MappingNotify), just not the changed-mapping consequence.
- The client still does not select for any event mask and does not queue events
  for callers — drained events are discarded (except the keymap signal). That is
  deliberate for an actuation client; if an event-consuming caller ever appears,
  `noteEvent` is the single place to fan out from.
- Only the frames the client actually reads are covered; events with payloads
  (`GenericEvent`) are drained by header only, which is correct because every
  X11 event frame is 32 bytes.

## Post-merge independent re-verification

Master moved past the fix (receiver guard on `XActuator` + a scratch-root ban
in the test helper) before acceptance, so the branch was merged up first
(`854b59b Merge master into fix/x11-reply-framing`) — no conflict in
`internal/shot`. Everything below was re-run ON THE MERGED TREE, from a clean
checkout, not carried over from the first pass.

| Check | Command | Result |
| --- | --- | --- |
| Build (all packages) | `go build ./...` | OK |
| Vet | `go vet ./internal/shot/` | OK (no output) |
| Focused framing tests | `go test ./internal/shot/ -count=1 -run 'ReplyFraming\|Drain\|Sequence\|MappingNotify\|Keymap' -v` | PASS — `TestReadReplyDrainsMappingNotifyBeforeFocusReply`, `TestReadReplyDrainsAllInterleavedFrameKinds`, `TestReadReplyDrainIsBounded` (2 subcases: at-the-bound recovered / one-past refused), `TestReadReplyRefusesOffSequenceReply` (3 subcases: previous-request answer / focus-window-as-sequence / wrap boundary), `TestReadReplyAcceptsMatchingSequence`, `TestMappingNotifyMarksKeymapDirtyAndRefreshes` (5 subcases incl. send-event-keyboard-mapping + pointer-mapping-ignored), `TestKeymapRefreshPicksUpChangedMapping`, `TestTypeTextAfterMappingNotifyKeepsConnectionAligned`; `ok … 0.304s` |
| Whole package | `go test ./internal/shot/ -count=1` | `ok github.com/nick/gopi/internal/shot 39.5s` (live Xvfb tests included) |
| The filed defect test | `-run TestKnownBugTypeTextMappingNotifyDesync` | PASS — and it now takes the `first == nil` branch ("the first act verb on a fresh display succeeded (the MappingNotify frame was drained)"), i.e. the bug it was filed for is gone rather than tolerated |
| Agent binary | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | OK |
| Live injection (fresh Xvfb :85 + xev + xdotool focus) | `DISPLAY=:85 ./bin/gopi act type --text "abc"` | exit 0, `{"action":"type","display":":85","dry_run":false,"pointer":"320,240","focus":"0x200001","root":"640x480","detail":"typed 3 character(s)"}` |
| Keys really delivered | `xev.log` | `keycode 38 (keysym 0x61, a)`, `keycode 56 (keysym 0x62, b)`, `keycode 54 (keysym 0x63, c)` — press+release each |
| Not sticky | `DISPLAY=:85 ./bin/gopi act info` immediately after | exit 0, honest answer `{"action":"info",…,"focus":"0x200001","note":"read-only probe: nothing was injected"}` |
| Frame-level truth (first pass, fresh Xvfb :82) | `strace -e trace=read -s 200 -x -o trace.log` on the gopi process | two `read(5, "\x22…", 32)` frames (0x22 = 34 = MappingNotify) consumed, then sequence-matched `\x01` replies accepted — the DRAIN itself is visible on the wire, not merely a lucky clean stream |

Note on the live run: the shell that started Xvfb/xev was killed by the tool's
own 2-minute cap AFTER every assertion above had already printed — the captured
output is complete, and `act type` reported `focus":"0x200001"` (matching the
xev window) rather than a frame-shifted value.

## Verification recipes (reusable)

- Fresh Xvfb + xev + xdotool focus is the live oracle for XTEST actuation:
  `Xvfb :N -screen 0 640x480x24 -nolisten tcp`, then `xev -geometry 200x120+20+20`,
  read its window id from its own first log line, `xdotool windowfocus <id>`.
  xev's own log is the ground truth for "the key was delivered".
- Frame-level truth for framing bugs — do not assert only on the client's
  return value; `strace -f -e trace=read -s 200 -x -o trace.log` shows the exact
  32-byte frames the client consumed, which is the only way to see a DRAIN
  (event read, then reply accepted) as opposed to a lucky clean stream.
- Exercising a private reader deterministically: preload the connection's
  sequence counter (the only way to construct an off-sequence frame at all),
  and make the fake server number requests per connection and echo the number,
  exactly as a real X server does.

## Independent second-pass verification (fresh machine state, no reuse of pass-1 output)

Every number below was re-produced on this branch tip (`0e23dbc`) after the
merge, in the linked worktree `~/workspace/gopi-x11-reply-framing` — nothing
carried over from the first pass.

| Check | Command | Result |
| --- | --- | --- |
| Build (all packages) | `go build ./...` | OK |
| Vet | `go vet ./internal/shot/` | OK (no output) |
| Framing tests, named run | `go test ./internal/shot/ -count=1 -run 'ReplyFraming\|Drain\|Sequence\|MappingNotify\|Keymap\|TestKnownBugTypeTextMappingNotifyDesync' -v` | PASS — 9 top-level tests / 15 subcases, `ok … 0.333s`. The two bound subcases print `at-the-bound-is-recovered` and `one-past-the-bound-is-refused`; the three refusal subcases print `previous-request-answer`, `focus-window-as-sequence`, `wrap-boundary` |
| The filed defect test | same run, `TestKnownBugTypeTextMappingNotifyDesync` | PASS (0.13s) |
| Whole package (live Xvfb tests incl.) | `go test ./internal/shot/ -count=1` | `ok github.com/nick/gopi/internal/shot 39.621s` |
| Agent binary | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | OK |
| Live, display :91 (xev NOT focused — focus stays on the root) | `DISPLAY=:91 ./bin/gopi act type --text "abc"` | exit 0, `{"action":"type","display":":91","focus":"0x1","root":"640x480","detail":"typed 3 character(s)"}` — the desync case still returns an honest answer, and the follow-up `act info` (exit 0, `"focus":"0x1"`) plus a second `act type` (exit 0) prove the connection is not sticky |
| Live, display :92 (xev window 0x200001 actually focused via `xdotool windowfocus`) | `DISPLAY=:92 ./bin/gopi act type --text "abc"` | exit 0, `{"action":"type","display":":92","focus":"0x200001","root":"640x480","detail":"typed 3 character(s)"}` — the first act verb on a fresh display succeeds; `xev92.log` carries `keycode 38 (keysym 0x61, a)`, `keycode 56 (keysym 0x62, b)`, `keycode 54 (keysym 0x63, c)`, press+release each (6 keysym lines) |
| Not sticky (same display :92) | `./bin/gopi act info` then a second `act type` | exit 0 / exit 0, `"focus":"0x200001"` unchanged — an independently-predicted value: the focus the client reports equals the window `xdotool` was told to focus, so the answer is the REAL one, not a frame-shifted field |
| Rule 1/13 gate (production diff only) | `git diff master...HEAD -- internal/shot/x11.go internal/shot/xactuate.go` filtered to non-comment added lines | every numeric literal in the diff is a named `x11*` constant of the const block; the only added string literals are `fmt.Errorf` format strings. No bare-meaningful literal introduced |
| Rules 6/8 hygiene | `git worktree list`, `git status --short` in `/home/user/work/GoPi` | my work lives only in `fix/x11-reply-framing` (worktree `~/workspace/gopi-x11-reply-framing`); the main checkout shows none of this task's files (`git branch --contains 0e23dbc` → `fix/x11-reply-framing` only) |

Method note for the live row: `xdotool windowfocus` on an **unmapped** xev window
fails with `BadMatch (X_SetInputFocus)` and, because that request bumps the X
server's serial counter, the subsequent `act info` can legitimately report the
root (`focus":"0x1"`). Display :92 therefore maps+raises the xev window before
focusing it, which is why its focus answer is `0x200001`.

## Post-fix: the one remaining source of flakiness (root-caused, fixed)

After the framing fix landed, `TestKnownBugTypeTextMappingNotifyDesync` still
skipped VIBLY about 1 run in 4 with
`display :NNN reachable but not actuable: setup reply: read ... connection reset by peer`.
A skip is not a pass, so it was chased to its root cause rather than retried.

- **Symptom**: the private Xvfb is *up* (the readiness probe connects, so
  `covFreeDisplayNumber` does not retry) but the test's OWN handshake gets a
  connection reset when reading the setup reply.
- **Mechanism (proven, not guessed)**: Xvfb started **without** `-noreset`
  resets the server the moment its last client disconnects. The readiness probe
  closes its socket, the server resets, and the test's immediately-following
  connect races that reset — the setup reply read returns ECONNRESET.
  Direct repro (`/tmp/xreset`, 12 handshake dials against a private Xvfb):
  **3/12 dials failed without the flag, 0/12 with `-noreset`**.
- **Fix**: named constant `covXvfbNoResetFlag = "-noreset"` passed to the
  private server (`cov_shot_x11_test.go`, 2-line surgical change). The
  lifecycle is unchanged — the test still kills the server in its cleanup.
- **Result**: the desync test PASSES **10/10** consecutive runs (was
  ~1-in-4 skip).
- Commit: `fea0d67 test(shot): start the private Xvfb with -noreset so the live tests are not flaky-skips`.

## Third pass: live acceptance re-run on display :93 (fresh Xvfb, fresh binary)

A final acceptance run was made from a clean slate — a NEW private Xvfb, a NEW
xev window, and a binary rebuilt from this branch tip immediately before the run
(`md5=5e406daa2d6e…`, and `strings bin/gopi | grep -c 'unexpected reply type'`
= **0**, i.e. the old failure message does not exist in the artifact).

| Step | Command | Result |
| --- | --- | --- |
| Build before the run | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | OK, binary at the worktree's `bin/gopi` |
| Display up | `Xvfb :93 -screen 0 1280x800x24 -noreset` | up |
| Focus a real window | `xdotool search --name "Event Tester"` → `windowmap` → `windowraise` → `windowfocus` | window `2097153` (0x200001) focused |
| **FIRST act verb on the fresh display** | `DISPLAY=:93 bin/gopi act type --text "abc"` | **exit 0**, `{"action":"type","display":":93","dry_run":false,"pointer":"640,400","focus":"0x200001","root":"1280x800","detail":"typed 3 character(s)"}` — no `unexpected reply type 34` |
| Keys really delivered | `/tmp/xev93.log`, `grep -cE "keysym 0x6[123]"` | **6** lines — `keycode 38 (keysym 0x61, a)`, `keycode 56 (keysym 0x62, b)`, `keycode 54 (keysym 0x63, c)`, press+release each |
| Not sticky | `DISPLAY=:93 bin/gopi act info` | exit 0, honest focus `0x200001` (the window xdotool was told to focus) |
| Third verb | `DISPLAY=:93 bin/gopi act type --text "xy"` | exit 0, `detail":"typed 2 character(s)"` |

Script: `/tmp/x11flive.sh` (Xvfb + xev + xdotool oracle, absolute binary path).

### Pitfall found while re-running (worth remembering)

The first attempt at this run **failed with the exact original error**
(`gopi act: GetInputFocus: unexpected reply type 34`) even though the fix was in
the branch. Root cause was NOT the fix: the shell line

```bash
cd ~/workspace/gopi-x11-reply-framing && Xvfb :93 … &   # WRONG
```

puts the **whole `cd … && Xvfb …` list in the background**, so the foreground
shell never changed directory and `./bin/gopi` resolved to the **main repo's**
binary at `/home/user/work/GoPi/bin/gopi` — master's unpatched build (`strings`
still carries `unexpected reply type`). Lesson, now applied above: a live proof
must (a) invoke the binary by **absolute path** inside the worktree and (b)
verify the artifact itself (md5 / absence of the old error string) before
believing the result. A "reproduced bug" that the fix already handles is far
more often the wrong binary than a failed fix — check the artifact first.

## Deliverable declaration (Rule 6d)

- **Branch**: `fix/x11-reply-framing` (worktree `/home/user/workspace/gopi-x11-reply-framing`).
- **Recovery**: `git show fix/x11-reply-framing:devdoc/x11-reply-framing.md`
  (and `git log --oneline fix/x11-reply-framing` for the fix commit `8e17985`).
- The commits are in the MAIN repo's object store (linked worktree shares refs);
  no push needed. The main checkout `/home/user/work/GoPi` was never touched.
