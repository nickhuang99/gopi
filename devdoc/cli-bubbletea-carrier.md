<!-- doc-class: historical -->
# Task Report: `gopi ui` — the Bubble Tea terminal chat surface (POC #2)

Companion to `devdoc/cli-links-carrier.md` (`gopi cli`, POC #1, branch
`remotes/peerbox/feat/cli-links`). Same task, second carrier, measured side by
side. Branch: `feat/gopi-cli-bubbletea` off `29d0fb75`.

## Completed

### A. The engine is NOT duplicated — `gopi ui` is a pure client of `gopi web`

`internal/uiclient/` (wire.go / stream.go / model.go / run.go / doc.go) is a
stdlib SSE reader plus a Bubble Tea model. It speaks to the EXISTING
`POST /api/chat` of `gopi web` over loopback:

- no second engine: model call, tool loop, slash table, session JSONL and the
  task-ledger row all stay server-side;
- no token needed on loopback (the server-held history means the client ships
  only the new message);
- the LAN token gate (`GOPI_WEB_TOKEN` / `webTokenGate`) is untouched — it was
  neither weakened nor bypassed.

Reused frame vocabulary (confirmed against the server's own emitters):
`queued` / `delta` / `tool` / `command` / `error` / `done`.

### B. `gopi ui` is a new PEER verb, shadowing nothing

`cmd/agent/cli_ui.go` (582L) + the registration surfaces:

- `CliVerbUI = "ui"` in `cmd/agent/cli_verbs.go`;
- `isUISubcommand` added to `main.go`'s pre-dispatch guard;
- `topLevelCommandNames` (completion), `commandIndex` (help, "Chat & web:"
  section), and the `GOPI_WEB_TOOLS` row in `internal/config/settings.go` now
  name `gopi ui`.

`cmd/agent/cli_ui_wire_guard_test.go` (196L) pins the client's event names,
request contract and route path to the SERVER's literals, and asserts the verb
dispatches exactly once and shadows no web flag.

### C. Runtime hygiene

`internal/scratch/scratch.go` gains `RuntimeClassUISession = "ui-session"`: the
in-process engine's console output is redirected to `<session>/ui-engine.log`
while the TUI owns the terminal — an age-bounded scratch class, off `/` by the
same mechanism as every other class (Rule 22), so a stray engine print cannot
paint over the TUI.

### D. The three sketch defects are closed structurally, not by patching

| # | Sketch defect | What this branch does instead |
|---|---|---|
| 1 | send-injection copy bug (model copied by value, so the echoed user line could be lost/duplicated) | every method takes `*model`; **no field is ever assigned through a copy** (documented at `model.go:110`) |
| 2 | missing `send` field in the request body | the body is built from `FieldMessage` (`stream.go:83`), a named constant — not a literal, not absent |
| 3 | `extractText` raw-frame fallback that silently accepted unparsed frames | **no such fallback exists**: `decodeFrame` decodes each event into a typed struct and returns `MalformedFrameError` / `UnknownFrameEventError` — an unparsable frame is REPORTED, never guessed |

Verified by grep: production code contains no `extractText`, and every
`json.Unmarshal` sits inside the six per-event decoders in `wire.go`.

## Not Done (honest)

- **No comparison verdict against POC #1.** This branch measures itself only;
  the side-by-side (Links vs Bubble Tea) belongs to the pairing task.
- **The clipboard/image/GUI half of a chat surface is absent**: no paste of
  rich content, no attachments, no mouse. Bubble Tea could carry them; this
  POC deliberately stops at text + tool lines + interrupt.
- **No multi-session switcher / resume picker.** Session continuity is the
  server's; the client has no session list.
- **`--addr`-style targeting is minimal** — the client defaults to
  `127.0.0.1:8090` and has no LAN binding of its own (by design: it is a
  client, never a server).
- Windows/`coninput` is an indirect dependency of Bubble Tea and untested here;
  everything measured is Linux/amd64 with a real X-less pty.

## Verification

- Build: `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/gopi ./cmd/agent/` → BUILD_OK
- Binary: `statically linked`, `stripped`, `ldd: not a dynamic executable`
- Vet: `go vet ./internal/uiclient/ ./cmd/agent/` → clean (no output)
- Tests (branch-scoped): all four packages green — observed output below
- Real pty drives: `scripts/ui/drive_ui.py` — 7/7 steps, both runs
- Commit: `921a6e09`

## Evidence (self-filed)

**Branch-scoped test gate — observed, all green** (56 new tests across the
first two):
```
$ go test ./internal/uiclient/ -count=1
ok  	github.com/nick/gopi/internal/uiclient	0.242s
$ go test ./cmd/agent/ -run 'UI|CliUI|Ui' -count=1
ok  	github.com/nick/gopi/cmd/agent	2.688s
$ go test ./cmd/agent/ -run 'Contract|Completion|Usage' -count=1
ok  	github.com/nick/gopi/cmd/agent	5.080s
$ go test ./internal/config/ ./internal/scratch/ -count=1
ok  	github.com/nick/gopi/internal/config	0.637s
ok  	github.com/nick/gopi/internal/scratch	16.677s
```

**Build / static-link.**
```
$ CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/gopi ./cmd/agent/ && echo BUILD_OK
BUILD_OK
$ file bin/gopi
bin/gopi: ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked,
          BuildID[sha1]=2b1e53a2a9704342a7b9c441fbee9b65ee9097f1, stripped
$ ldd bin/gopi
	not a dynamic executable
```

**The tool loop ran LIVE against the real engine** (verbatim from
`/home/user/workspace/gopi-cli-bubbletea-evidence/ui-session2.raw`; the prompt
is exactly `run the bash tool with exactly: head -3 go.mod`):
```
  ⚙ call bash head -3 go.mod
  ⚙ result bash exit=0
      module github.com/nick/gopi
      go 1.24.0
```

**Server-held history carried across turns** — turn 3 asks what the bash
command printed and answers from the transcript the CLIENT never sent:
```
      ck/gopi`, a blank line, and `go 1.24.0`.
idle · turns 3 · tool events 2
```

**Ctrl+X is not an error.** `interruptedNotice = "interrupted (Ctrl+X)"`; the
step-7 screen is `step07-turn-4-interrupted-(not-an-error).txt`, and the idle
footer reads `idle · turns 4 · tool events 2` — no `error` frame.
`endTurn` is the single idempotent conclusion path, gated by an `ended` bool,
so a double done/interrupt cannot double-count a turn.

**Per-step screens** (identical step names in `screens/` from run 1 and
`screens2/` from run 2 — two independent drives, 7/7 each):
```
step01-surface-ready.txt
step02-turn-1-done.txt
step03-turn-2-done:-the-bash-tool-ran-live.txt
step04-idle-interrupt-is-harmless.txt
step05-turn-3-done:-history-carried-across-turns.txt
step06-turn-4-is-streaming.txt
step07-turn-4-interrupted-(not-an-error).txt
```

**Deterministic framing count**, measured by counting the model's repaint
marker (`ESC[22A`, move-up-22 = one full frame):
```
ui-session.raw   6390 bytes  31 frames   (run 1)
ui-session2.raw  6920 bytes  35 frames   (run 2, the one quoted above)
```
The client repaints on state change, not per byte — which is the property
Bubble Tea was expected to buy and it holds: ~35 full frames carry four turns
(two of them streaming long answers) rather than one frame per SSE delta.
A "you> " split of the raw file is deliberately NOT quoted as a per-turn
figure: the echoed transcript repeats user lines, so that split over-counts
turns (15/16 instead of 4) and is not a measurement.

**Dependency delta** (`git diff master -- go.mod`) — 3 direct requires, 1 added
by this branch:
```
require (
	github.com/charmbracelet/bubbletea v1.3.10   <-- added (pinned)
	github.com/mattn/go-runewidth v0.0.16
	modernc.org/sqlite v1.44.0
)
```
Bubble Tea drags 15 indirect entries (lipgloss, x/ansi, x/cellbuf, x/term,
osc52, colorful, …). That is the whole external cost of POC #2 and it is the
honest headline for the comparison: **Links cost a vendored C browser + a build
script; Bubble Tea costs 16 module lines.** No dependency beyond the module
graph — the client itself is stdlib + these two.

**`gopi web` is unchanged in behaviour.** The diff to `cmd/agent/cli_web.go`
is three constant extractions only (`webToolsFlag` = `"--tools"`,
`webToolsEnv`/`webToolsEnabled()`), made because TWO surfaces now read the same
switch (Rule 1b/13). The parser, the usage text, the token gate and the routes
are byte-identical in effect; the existing suites stay green.

## Findings / design decisions

### Why a client, not a fork

The sketch implied a second engine. That was the expensive path and the wrong
one: `gopi web` already owns the model call, the tool loop, the approval gate,
the session JSONL and the ledger row. Making `gopi ui` a loopback SSE client
means the two carriers cannot DRIFT — every behavioural fix lands once. The
cost is that `gopi ui` needs a reachable `gopi web`; a future single-binary
mode would embed the same handler, not rewrite it.

### The named-constant refactor in `cli_web.go`

`"--tools"` and `"GOPI_WEB_TOOLS"` were inline literals read at two sites each.
Adding a second reader (the `ui` flag table) is exactly the moment Rule 1b/13
targets: extract now, spelling lives once. The refactor is behaviour-free and
guarded by the existing web suites.

### Wire guard rather than a doc

Client and server agree on six event names and one path (`/api/chat`). A
comment would rot; `cli_ui_wire_guard_test.go` compares the client's constants
to the server's literals, so a rename on either side fails the build.

## Honest cost list

- 3724 lines touched/added, of which **~2300 are production-adjacent** (client
  1238, verb + guard 1071, scripts 463) and 923 are tests.
- Module cost: +1 direct require, +15 indirect (see above).
- Terminal cost: raw mode means the TUI owns the tty while running; the engine
  log redirection (`RuntimeClassUISession`) exists precisely so a stray engine
  print cannot corrupt the frame — one extra file per session.
- Interrupt granularity is per-turn: Ctrl+X abandons the turn, not a single
  tool call inside it.
- The verb refuses to inherit a foreign task-report contract in its
  environment (visible in the transcript), which is deliberate but is one more
  surface a future carrier must keep.

## Recommendation

Land POC #2 as the **carried** option if the side-by-side values a maintained
widget toolkit (scrolling, resize, key routing already solved) over a vendored
C browser and a shell build script; land POC #1 if the priority is zero module
growth and a dependency-free build. The measurement this branch contributes to
that decision: 16 module lines vs a vendored binary, and a live tool loop plus
cross-turn history proven in a real pty with no error frames.
