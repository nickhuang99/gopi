<!-- doc-class: historical -->
# Task Report: internal/shot X11+XTEST layer — function-level coverage to 100% (Display, Focus, Click, TypeText)

Branch: `feat/cov-shot-x11` · Worktree: `~/workspace/gopi-cov-shot-x11` · Base: `114cf26`
Deliverable: `internal/shot/cov_shot_x11_test.go` (tests only — **no product changes**)

## Completed

- `python3 scripts/func_coverage.py --pkg internal/shot --uncovered` → **100.0%** (12/12 funcs, 0 uncovered).
- 19 new top-level tests (48 PASS / 0 FAIL / 0 SKIP in the package, identical across two runs).
- **Wire-layout byte assertions** for `GetInputFocus`, `GetKeyboardMapping` and `XTestFakeInput`
  (button + key), pinned against a **fake X server** that records raw request bytes — the part a
  real X server never lets you see.
- The four briefing-named error paths plus four more: no DISPLAY, unconnectable display,
  out-of-range coordinate, unknown key name, out-of-range mouse button, unmappable rune,
  keysym absent from the server mapping, short/malformed keyboard-mapping reply, `0` per-code reply.
- Live-display tests on a **private Xvfb started by the test itself** (never `$DISPLAY`), with
  observable-change verification: the injected button/key events are read back out of `xev`'s
  report file, and the focus id is cross-checked against an independent X client (`xdotool`).
- Two product bugs found and **filed, not fixed** (tests-only slice — see Filed bugs).

## Not Done (honest disclosure)

- **No product code was changed** (task constraint). Both bugs found remain in `master`'s code;
  they are filed as separate tasks with repro + acceptance criteria.
- The `omitempty`/silent-failure audit of the layer was **not** in scope and was not performed.
- The fake X server implements only the three requests the actuation path emits; a request outside
  that set is answered with an X11 error and any test relying on it would fail loudly rather than
  hang.
- `RootSize()` (an already-covered accessor) got no nil-guard test beyond what `Display()`'s test
  documents; the nil-receiver audit it points at is filed as a bug.
- No `xfwm4` window manager is started by the live tests: they set the input focus explicitly
  through `xdotool windowfocus` instead, so no WM dependency is added. The consequence is that
  focus-follows-pointer behaviour is not exercised.

## Source inventory (protocol vs convenience layer)

`internal/shot` is two layers. The **protocol layer** is the hand-rolled X11 wire client — it owns
every byte on the socket. The **convenience layer** is what the LLM/CLI actually calls; it composes
protocol requests, enforces ranges, and produces the machine-readable result.

| Layer | Symbol | File:line | What it owns |
|---|---|---|---|
| protocol | `x11Client.request/write` | `x11.go:307`, `x11.go:313` | sequence counter, write deadline, one atomic request |
| protocol | `x11Client.readReply` | `x11.go:321` | 32-byte reply header + payload, X-error → Go error |
| protocol | `x11Client.handshake` / `parseSetup` | `x11.go:365`, `x11.go:400` | connection setup; root geometry field map |
| protocol | `x11Client.queryExtension` | `x11.go:449` | extension major opcode lookup (XTEST) |
| protocol | `x11Client.getRootImage` / `getImageBand` | `x11.go:477`, `x11.go:494` | banded GetImage, BGRX→RGBA |
| protocol | `x11Client.inputFocus` | `xactuate.go:475` | **GetInputFocus** (opcode 43) → focused window id |
| protocol | `x11Client.queryPointer` | `xactuate.go:459` | QueryPointer (opcode 38) → root coords |
| protocol | `x11Client.sync` | `xactuate.go:451` | the act→verify **barrier** (delegates to `inputFocus`) |
| protocol | `XActuator.fakeInput` | `xactuate.go:200` | **XTestFakeInput** (36-byte request) |
| protocol | `XActuator.loadKeymapLocked` | `xactuate.go:343` | GetKeyboardMapping → keysym table |
| convenience | `XActuator.Display` | `xactuate.go:184` | echo the display the actuator was opened with |
| convenience | `XActuator.Focus` | `xactuate.go:174` | mutex + `inputFocus` |
| convenience | `XActuator.Click` | `xactuate.go:239` | button range guard + press/release pair + `sync` |
| convenience | `XActuator.TypeText` | `xactuate.go:260` | rune→keysym→keycode, Shift wrapping, abort on first bad rune |
| convenience | `XActuator.MovePointer` / `Key` / `Pointer` / `RootSize` | `xactuate.go:212/274/163/180` | sibling verbs (covered before this task) |
| convenience | pure translators | `keysymForRune:394`, `keysymForName:412`, `namedKeysyms:419` | text/key vocabulary |

## code_search evidence (definitions + callers)

`code_search(symbol="Display")` → 5 hits; the one definition in this package:

```
Display  internal/shot/xactuate.go:184  func  func (a *XActuator) Display() string {
```
(the other hits are unrelated `Display` methods/fields in `cli_act.go:191`, `x11.go:112`,
`internal/tools/screencapture.go:49`.)

`code_search(symbol="TypeText")` → definition `internal/shot/xactuate.go:260` (+ doc comment
`:255`); references outside the package are the unrelated `hippoBlockTypeText` constant.

Definitions of the four target functions (grep over the worktree, `internal/shot/*.go`):

```
internal/shot/xactuate.go:174:func (a *XActuator) Focus() (uint32, error)
internal/shot/xactuate.go:184:func (a *XActuator) Display() string
internal/shot/xactuate.go:239:func (a *XActuator) Click(button int) error
internal/shot/xactuate.go:260:func (a *XActuator) TypeText(text string) error
internal/shot/xactuate.go:451:func (c *x11Client) sync() error
internal/shot/xactuate.go:475:func (c *x11Client) inputFocus() (uint32, error)
```

Production callers — all four are reached through the single `gopi act` CLI verb
(`cmd/agent/cli_act.go`, one dispatch site each):

```
cmd/agent/cli_act.go:229:  res := actResult{Action: req.action, Display: a.Display(), DryRun: req.dryRun}
cmd/agent/cli_act.go:245:  if err := a.Click(req.button); err != nil {
cmd/agent/cli_act.go:250:  if err := a.TypeText(req.text); err != nil {
cmd/agent/cli_act.go:267:  if focus, err := a.Focus(); err == nil {   // echoed into every result
```

Test callers after this slice: 21 real call sites for the four target functions in the new file
(Display 2, Focus 6, Click 4, TypeText 9) — call sites, not mere mentions. The metric rewards
presence; these are invocations whose results are asserted on.

## Test inventory mapped to boundaries

File: `internal/shot/cov_shot_x11_test.go`.

| Test | Target | Boundary / wire fact pinned |
|---|---|---|
| `TestCovX11DisplayReportsConnectedName` | Display | echoes the opened display; zero-value actuator returns `""` |
| `TestCovX11FocusRequestBytes` | Focus | **GetInputFocus request = exactly 4 bytes `2b 00 01 00`**; reply window id read from header `[8:12]` |
| `TestCovX11ClickRequestBytes` (buttons 1..5) | Click | **two XTestFakeInput requests, 36 bytes each, length word 9**, `xtReqType=2`, types 4 then 5, detail=button, time 0, root/rootX/rootY 0, deviceid 0 |
| `TestCovX11ClickRejectsOutOfRangeButton` | Click | buttons `{-1,0,6,99}` refused, **0 bytes on the wire** |
| `TestCovX11ClickPropagatesWriteError` | Click | write error propagates (Rule 15), never swallowed |
| `TestCovX11TypeTextRequestBytes` | TypeText | **press/release pair per rune + Shift wrapped**, keycodes taken from the *server's* mapping (layout gives `n`/`N` different keycodes), key events carry no coordinates |
| `TestCovX11TypeTextStopsAtFirstBadRune` | TypeText | unmappable rune aborts; only the preceding `n` is typed |
| `TestCovX11TypeTextUnknownKeysym` | TypeText | keysym valid but absent from the server map → loud error, 0 events |
| `TestCovX11KeyboardMapRequestBytes` | TypeText (via keycode resolve) | **GetKeyboardMapping = 8 bytes, opcode 101, first-keycode 8, count 248**, emitted exactly once (cached) |
| `TestCovX11KeymapShortReply` | TypeText | truncated map body → loud error, no panic / no mis-index |
| `TestCovX11KeymapZeroPerCode` | TypeText | `0` keysyms-per-keycode → error, no divide-by-zero |
| `TestCovX11OpenActuatorNoDisplay` | (shared entry) | DISPLAY unset/blank → refusal naming `DISPLAY`, no dial |
| `TestCovX11OpenActuatorUnconnectableDisplay` | (shared entry) | syntactically valid, dead display → connect error |
| `TestCovX11ClickNoDisplay` | (shared entry) | same refusal from the Click-side path |
| `TestCovX11MovePointerRejectsOutOfRangeCoordinate` | MovePointer (sibling) | `-1`, `x==width`, `y==height`, far outside refused, 0 bytes; **last addressable pixel accepted** |
| `TestCovX11KeyRejectsUnknownName` | Key (sibling) | `f13`, `Return`, `enter`, `""`, `typo` refused; `return` still works |
| `TestLiveDisplayFocusClickTypeObservable` | all four | live: focus id `==` xdotool's, xev logs `button 3`, xev logs `keysym 0x66` + `KeyRelease` |
| `TestLiveDisplayFocusChangesWithFocus` | Focus | live: parsed id `==` independent client's id |
| `TestKnownBugTypeTextMappingNotifyDesync` | TypeText | live: pins the discovered defect's exact signature + stickiness + fresh-session recovery |

### The pinned byte facts (single place, for the next reader)

```
GetInputFocus      request : 2b 00 01 00                      (4 bytes; len word = 1)
GetKeyboardMapping request : 65 00 02 00 08 f8 00 00          (8 bytes; len word = 2)
                              ^opcode101 ^len=2 ^first=8 ^count=248
XTestFakeInput     request : 36 bytes, len word = 9            (sz_xXTestFakeInputReq)
   [0]=XTEST major  [1]=2 (X_XTestFakeInput)  [2:4]=9  [4]=type  [5]=detail
   [6:8]=pad  [8:12]=time(0)  [12:16]=root  [24:26]=rootX  [26:28]=rootY  [35]=deviceid
   type 2/3 = KeyPress/KeyRelease (detail = KEYCODE), 4/5 = ButtonPress/ButtonRelease
   key/button events carry NO coordinates (root + rootX/rootY stay 0)
Reply frame        : 32-byte header; [0]=1 Reply, [1]=extra data, [2:4]=sequence,
                     [4:8]=extra length in 4-byte units; an EVENT uses [0]=its event code
```

Authority: `/usr/include/X11/extensions/xtestproto.h` on this host —
`#define sz_xXTestFakeInputReq 36`, and the struct's field order
(`type, detail, pad0, time, root, pad1, pad2, rootX, rootY, pad3, pad4, pad5, deviceid`).

## Live vs skipped disclosure

- **Live tests ran on this host**: `TestLiveDisplayFocusClickTypeObservable`,
  `TestLiveDisplayFocusChangesWithFocus` and `TestKnownBugTypeTextMappingNotifyDesync` all RAN
  (PASS, 0.10–0.26 s each) against a private Xvfb the test starts on a swept display number
  (`:200`–`:219`) — **`$DISPLAY` was never used**, so nothing was injected into the displays other
  agents own on this box (`:99` hosts another agent's gedit + xfwm4; `:0` is a real Xorg).
- **Skip path verified visibly**: with a PATH containing no Xvfb/xev, the live tests report
  `SKIP: ... Xvfb is not installed — live actuation cannot be exercised (wire-layout tests still
  ran)`, and the in-test skip for a missing oracle names the reason too
  (`xev is not installed — the live observable-change oracle is unavailable`). Windows are located
  from xev's own `Outer window is 0x…` line, so no window manager is required.
- **Everything else is deterministic and hermetic**: the wire-layout tests bind a real unix socket
  and talk to the fake server — no network, no display, no sleeps beyond bounded readiness polls.

## Security note — where a guard/limit belongs

- The **button range** (`1..5`) and the **coordinate range** (inside the root window) are the
  layer's existing input guards: both refuse loudly *before* writing, which the tests pin by
  requiring **zero bytes on the wire** for a refused request.
- The **missing guard** this slice found is at the reply boundary: `readReply` (`x11.go:321`)
  classifies a frame as an error (type 0) or "unexpected", but has **no event-vs-reply
  discrimination and no sequence-number matching** — it will hand a caller the bytes of an
  unrelated event as if they were its answer. That is where a guard belongs: match the reply
  sequence number (`header[2:4]`) against the request sequence, drain (or queue) events, and bound
  the number of frames a single read may scan so a chatty server cannot make the loop unbounded.
  `Focus()`'s answer is the agent's "did my keystroke go to the right window" — a misattributed
  frame there is a silent correctness (not just stability) failure. Filed as task
  `01a08d67-a67b-7c4a-84ec-8595ebe8ff8c`.
- **Text is deliberately limited** to provable ASCII + named keys, and an unprovable rune is a
  refusal, so the layer cannot type mojibake it cannot see (Rule 15) — pinned by two tests.

## Filed bugs (tests-only slice: reported, not fixed)

| Task | Bug |
|---|---|
| `01a08d67-a67b-7c4a-84ec-8595ebe8ff8c` | **`readReply` consumes a MappingNotify event (type 34) as the `GetInputFocus` reply.** On a fresh display the first `gopi act type` fails with `GetInputFocus: unexpected reply type 34`; the keys DO reach the server (xev logs them), but the connection stays frame-shifted for every later round-trip. strace: `write(GetKeyboardMapping)` → keys → `write(2b 00 01 00)` → `read("\"…")` (0x22 = 34) with **no read in between**, because XTestFakeInput is a no-reply request. |
| `01a08d67-a7b3-7c72-90a5-2b4c3f3d16fc` | **`XActuator.Display()` panics on a nil receiver** — it guards `a.c == nil` but dereferences `a` first. (`RootSize()` has no nil guard at all.) |

Both bugs are pinned by committed tests (`TestKnownBugTypeTextMappingNotifyDesync` asserts the
signature plus both consequences; `TestCovX11DisplayReportsConnectedName` documents the nil case),
so the fix lands with a red→green signal.

## Verification

```
Build:  go build ./...                                    → OK (exit 0)
Tests:  go test -count=1 ./internal/shot/                 → ok  github.com/nick/gopi/internal/shot  39.390s
        go test -count=1 ./internal/shot/                 → ok  github.com/nick/gopi/internal/shot  39.412s
        (both runs: 48 PASS / 0 FAIL / 0 SKIP; verbose outcome lists diff-identical → non-flaky)
        go test -short -count=1 ./internal/shot/          → ok (8.824s)
        go vet ./internal/shot/                           → clean
Coverage: python3 scripts/func_coverage.py --pkg internal/shot --uncovered → 100.0% (12/12, 0 uncovered)
        python3 scripts/func_coverage.py --pkg internal/shot --json → {"total_funcs":12,"covered":12,"pct":100.0,"missing":[]}
```

### RAW output — `go test` run 1

```
$ cd ~/workspace/gopi-cov-shot-x11 && go test -count=1 ./internal/shot/
ok  	github.com/nick/gopi/internal/shot	39.390s
```

### RAW output — `go test` run 2

```
$ cd ~/workspace/gopi-cov-shot-x11 && go test -count=1 ./internal/shot/
ok  	github.com/nick/gopi/internal/shot	39.412s
```

### RAW output — new tests (verbose, run 1)

```
--- PASS: TestCovX11DisplayReportsConnectedName (0.00s)
--- PASS: TestCovX11FocusRequestBytes (0.00s)
--- PASS: TestCovX11ClickRequestBytes (0.06s)
    --- PASS: TestCovX11ClickRequestBytes/button1 (0.01s)
    --- PASS: TestCovX11ClickRequestBytes/button2 (0.01s)
    --- PASS: TestCovX11ClickRequestBytes/button3 (0.01s)
    --- PASS: TestCovX11ClickRequestBytes/button4 (0.01s)
    --- PASS: TestCovX11ClickRequestBytes/button5 (0.01s)
--- PASS: TestCovX11ClickRejectsOutOfRangeButton (0.00s)
    --- PASS: TestCovX11ClickRejectsOutOfRangeButton/button-1 (0.00s)
    --- PASS: TestCovX11ClickRejectsOutOfRangeButton/button0 (0.00s)
    --- PASS: TestCovX11ClickRejectsOutOfRangeButton/button6 (0.00s)
    --- PASS: TestCovX11ClickRejectsOutOfRangeButton/button99 (0.00s)
--- PASS: TestCovX11ClickPropagatesWriteError (0.00s)
--- PASS: TestCovX11TypeTextRequestBytes (0.03s)
--- PASS: TestCovX11TypeTextStopsAtFirstBadRune (0.01s)
--- PASS: TestCovX11TypeTextUnknownKeysym (0.00s)
--- PASS: TestCovX11KeyboardMapRequestBytes (0.01s)
--- PASS: TestCovX11KeymapShortReply (0.00s)
--- PASS: TestCovX11KeymapZeroPerCode (0.00s)
--- PASS: TestCovX11OpenActuatorNoDisplay (0.00s)
--- PASS: TestCovX11OpenActuatorUnconnectableDisplay (0.00s)
--- PASS: TestCovX11ClickNoDisplay (0.00s)
--- PASS: TestLiveDisplayFocusClickTypeObservable (0.15s)
--- PASS: TestLiveDisplayFocusChangesWithFocus (0.10s)
--- PASS: TestKnownBugTypeTextMappingNotifyDesync (0.10s)
--- PASS: TestCovX11MovePointerRejectsOutOfRangeCoordinate (0.00s)
    --- PASS: TestCovX11MovePointerRejectsOutOfRangeCoordinate/negative-x (0.00s)
    --- PASS: TestCovX11MovePointerRejectsOutOfRangeCoordinate/negative-y (0.00s)
    --- PASS: TestCovX11MovePointerRejectsOutOfRangeCoordinate/x-at-width (0.00s)
    --- PASS: TestCovX11MovePointerRejectsOutOfRangeCoordinate/y-at-height (0.00s)
    --- PASS: TestCovX11MovePointerRejectsOutOfRangeCoordinate/far-outside (0.00s)
--- PASS: TestCovX11KeyRejectsUnknownName (0.01s)
    --- PASS: TestCovX11KeyRejectsUnknownName/name_f13 (0.00s)
    --- PASS: TestCovX11KeyRejectsUnknownName/name_Return (0.00s)
    --- PASS: TestCovX11KeyRejectsUnknownName/name_enter (0.00s)
    --- PASS: TestCovX11KeyRejectsUnknownName/name_ (0.00s)
    --- PASS: TestCovX11KeyRejectsUnknownName/name_typo (0.00s)
ok  	github.com/nick/gopi/internal/shot	39.396s
```

### RAW output — visible skip (no Xvfb on PATH)

```
$ PATH=/usr/local/go/bin:/tmp/emptybin go test -count=1 -run 'TestLiveDisplay|TestKnownBug' -v ./internal/shot/
=== RUN   TestLiveDisplayFocusClickTypeObservable
    cov_shot_x11_test.go:1150: Xvfb is not installed — live actuation cannot be exercised (wire-layout tests still ran)
--- SKIP: TestLiveDisplayFocusClickTypeObservable (0.00s)
=== RUN   TestLiveDisplayFocusChangesWithFocus
    cov_shot_x11_test.go:1243: Xvfb is not installed — live actuation cannot be exercised (wire-layout tests still ran)
--- SKIP: TestLiveDisplayFocusChangesWithFocus (0.00s)
=== RUN   TestKnownBugTypeTextMappingNotifyDesync
    cov_shot_x11_test.go:1306: Xvfb is not installed — live actuation cannot be exercised (wire-layout tests still ran)
--- SKIP: TestKnownBugTypeTextMappingNotifyDesync (0.00s)
PASS
ok  	github.com/nick/gopi/internal/shot	0.004s
```

### RAW output — `func_coverage.py`

```
$ python3 scripts/func_coverage.py --pkg internal/shot --uncovered
package                                     funcs    cov    pct  tests
--------------------------------------------------------------------------
internal/shot                                  12     12 100.0%
--------------------------------------------------------------------------
TOTAL                                          12     12 100.0%

Uncovered exported funcs (by package):

NOTE: conservative metric — a func counts as covered only if its name
appears in a *_test.go file of the same package. Target: 100%.
```

```
$ python3 scripts/func_coverage.py --pkg internal/shot --json
{
  "total_funcs": 12,
  "covered": 12,
  "pct": 100.0,
  "packages": [
    {"package": "internal/shot", "funcs": 12, "covered": 12, "missing": [], "has_tests": true}
  ]
}
```

**Metric honesty check** — the metric is name-presence only, so here is the call-site evidence
(real invocations, not mentions):

```
$ grep -o 'a\.Display(' internal/shot/cov_shot_x11_test.go | wc -l   # 2
$ grep -o 'a\.Focus('   internal/shot/cov_shot_x11_test.go | wc -l   # 6
$ grep -o 'a\.Click('   internal/shot/cov_shot_x11_test.go | wc -l   # 4
$ grep -o 'a\.TypeText(' internal/shot/cov_shot_x11_test.go | wc -l  # 9   (siblings: MovePointer 3, Key 2)
   — every one a real call, mostly asserting on the result or on the bytes it produced.
```

### RAW strace evidence for filed bug #1 (frame desync)

```
3984885 write(5, "e\0\2\0\10\370\0\0", 8)                  = 8     GetKeyboardMapping
3984885 read(5, "\1\7\2\0\310\6\0\0…", 32)                 = 32    its reply
3984885 read(5, "…", 6944)                                  = 6944  its payload
3984885 write(5, "\204\2\t\0\2*\0\0…", 36)                  = 36    XTestFakeInput KeyPress
   …keys injected, NO read here (FakeInput is a no-reply request)…
3984887 write(5, "+\0\1\0", 4)                              = 4     GetInputFocus (0x2b)
3984887 read(5, "\"\0\3\0\1\10\370\0…", 32)                 = 32    type 0x22 = 34 = MappingNotify EVENT
gopi act: GetInputFocus: unexpected reply type 34
```

## memory_store entries

- **id 1747** (declarative) — *Fake X11 server over a real unix socket — deterministic wire-layout
  tests for a pure-Go X11/XTEST client.* Question: "How do I unit-test a hand-rolled pure-Go X11
  client (wire byte layouts, error paths, malformed replies) with no real X server — or pin
  request bytes a live server never exposes?" Contains the 9-step recipe (free-display bind probe,
  `.XN-lock` contents, setup-reply built by length, the self-describing request length loop, the
  **no-reply request rule**, malformed-reply injection) and the pinned 36-byte/`words=9` facts.
  Source: `GoPi branch feat/cov-shot-x11; internal/shot/cov_shot_x11_test.go; devdoc/cov-shot-x11-agent.md`.
  Keywords: 10 mixed zh/en breadcrumbs (`fake X server 假 X 服务器`, `no-reply request 无回复请求`,
  `X11 wire layout byte assertion 请求字节布局 断言`, …).
- Task-start retrieval was used and voted: memory **#1693 / #1687 / #1714** → `useful`
  (#1693 supplied the two known protocol traps — the setup field map and the 36-byte FakeInput
  length; #1687 supplied the coverage-slice workflow and the "file latent bugs, don't fix them in a
  tests-only slice" rule; #1714 supplied the seam/stub testing pattern).
- No `memory(action=update)` was needed: nothing retrieved contradicted the results. #1693's claim
  that XTEST's 36-byte length is `sz_xXTestFakeInputReq` was **re-verified** here against
  `/usr/include/X11/extensions/xtestproto.h` and by the fake server's recorded bytes.

## Workflow feedback

1. **The briefing's error-path list mostly lives one layer up.** "Out-of-range coordinate" and
   "unknown key name" are guards of `MovePointer`/`Key`, not of the four named functions —
   `Click`'s own guard is the **mouse button** range. I covered both sets rather than renaming the
   requirement away: the four *target* functions got their own guards pinned (button range,
   unmappable rune, keysym-not-in-mapping, `0`/short keymap reply), and the coordinate/key-name
   guards got explicit tests too. Recording this because a future coverage slice will hit the same
   mismatch between "the function you must cover" and "the error path it owns".
2. **`func_coverage.py` ranks nothing.** All four functions were equally "uncovered" although
   `Display()` is a one-line getter and `TypeText` is the whole keystroke pipeline. Time is best
   spent proportional to what the function decides, not evenly per name — the metric only demands
   name presence, so a green metric with thin assertions is entirely possible. The honesty section
   above is the counterweight.
3. **Effort estimate for this shape of task**: ~40% of the session went to *environment plumbing*
   (a private Xvfb that survives bash's process-group kill, wakeup apps dying with my shell,
   `pkill -f` matching my own `bash -c` and killing the session — three times). Two reusable rules
   came out of it: **never `pkill -f <pattern>` that can match your own command line** (use
   `pkill -x` on the exact process name, or kill by PID from `ps`), and **start long-lived helpers
   with the `job` tool** (detached session, no bash timeout reaping).
4. **The high-value move was the fake server.** It turned "call the function and check the error
   message" into byte-level assertions and made malformed replies testable at all — and building it
   is what produced both bug findings (the frame desync only becomes visible when you can see that
   a no-reply request had NOTHING to read before the next reply).
5. **Tests found both bugs, neither was guessable from reading.** The nil-receiver panic appeared
   only when I actually called `Display()` on a nil `*XActuator`; the MappingNotify desync appeared
   only on a *fresh* display — on `:99`, where a map was already fetched, everything passed. A
   coverage slice that ran only against the long-lived display would have reported 100% green and
   missed both. **Isolate the environment a live test runs in** (a private display, not the shared
   one) — it is both politeness and a bug-finding technique.
6. **The `read` tool refused this test file** as "binary" (it is valid UTF-8 with em-dashes),
   which forced snippet reads through `bash sed`. Worth fixing separately; noted here so the next
   agent does not think the file is corrupt.
7. **What I would do differently**: probe the environment (X sockets, helper binaries, whether a
   display number is bindable) *before* writing the first test — the first draft was built around
   `:0` and every single wire test skipped, which cost a full edit round.

## Deliverable declaration

- **Branch**: `feat/cov-shot-x11` (worktree `~/workspace/gopi-cov-shot-x11`, shared object store —
  the commit is already on the branch in the main repo `/home/user/work/GoPi`; no push needed).
- **Files**: `internal/shot/cov_shot_x11_test.go` (new), `devdoc/cov-shot-x11-agent.md` (this file).
- **Recovery**: `git show feat/cov-shot-x11:devdoc/cov-shot-x11-agent.md` ·
  `git show feat/cov-shot-x11:internal/shot/cov_shot_x11_test.go`
