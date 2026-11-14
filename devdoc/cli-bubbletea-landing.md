<!-- doc-class: historical -->
# Task Report: the Bubble Tea UI POC landed on current HP master

**Task**: `01a0bedc-6ba8-7302-9182-9be0bf90d036` — land the finished Bubble Tea
chat surface (`feat/gopi-cli-bubbletea`, 4047 lines / 24 files, 476 commits
behind) onto CURRENT master as a landable, RUNNABLE, **unmerged** branch.
Branch: **`feat/gopi-cli-bubbletea-landing`** (off HP master `f90db8d6`),
worktree **`~/workspace/gopi-cli-bubbletea-landing`** on **HP only**.

## Completed

### 1. The POC is ADOPTED, not rebased (Rule 23 / Rule 29)

The old branch was read strictly read-only (`git show <ref>:<path>`,
`git diff master..<ref>`). No worktree of it was touched and no commit was made
on it. A **fresh** worktree was cut from current HP master and the POC's files
were adopted into it:

- **Wholesale (11 new files)**: `cmd/agent/cli_ui.go`, `cli_ui_test.go`,
  `cli_ui_wire_guard_test.go`, `internal/uiclient/{wire,stream,model,run,doc}.go`,
  `client_test.go`, `scripts/ui/{drive_ui.py,render_transcript.py}`.
- **By hand (7 registration deltas)**: `CliVerbUI` in `cli_verbs.go`; the
  `isUISubcommand` guard in `main.go`; `CliVerbUI` in the completion candidate
  set; the `gopi ui` index row in `internal/config/help.go`; the
  `RuntimeClassUISession` scratch class; the contract/usage test wiring.

**What the port deliberately did NOT take** (the POC branch's stale state):
`internal/config/settings.go`'s revert of the `boolvocab` read (master's version
is newer and correct), `internal/scratch/scratch.go`'s retire of
`CachedCanonicalPath` and its `merger-takeover` class, the `cmd/agent/cli_web.go`
JS listener rewrite, the help.go rows for `archive`/`cli`/memory/voice/shot that
master has since changed, and `cli_verbs.go`'s retirement of `CliVerbArchive` /
`CliVerbCLI`. Those hunks are divergences, not intent — master is authoritative.

### 2. Divergence resolution (what conflicted, how it was resolved)

| # | Conflicted on | Resolution |
|---|---|---|
| 1 | `cmd/agent/cli_web.go`: the POC added a named `webToolsFlag = "--tools"` that current master does not define | **Kept the POC's constant** (its own wire-guard test needs it) and pointed master's parser at it — one literal, one definition (Rule 1b/13) |
| 2 | `internal/uiclient/cli_ui_wire_guard_test.go` asserted the route as a SOURCE-TEXT match on `"/api/chat"` | Master now spells the route `mux.HandleFunc(webChatPath, …)`. The literal match could never fire. **Pinned the VALUES instead** (`uiclient.ChatPath != webChatPath` → fail) plus a check that the route table is registered *through* the constant. A stronger assertion: it survives a rename on either side |
| 3 | `cli_completion_test.go`'s hardcoded first-level `want` list | Master's list still carries `archive`/`cli`; added `CliVerbUI` to it. The POC's own removal of those two entries is a divergence (master outranks the POC), so they stay |
| 4 | `internal/scratch/scratch.go` — `RuntimeClassUISession` did not exist | Added the constant in master's own style (Rule 22: a runtime class the scratch GC bounds, off `/`) |
| 5 | `internal/uiclient` needed the model's terminal geometry | See §3 — this was a **real defect**, not a port artifact |

### 3. Two real defects found by MEASUREMENT (not by inspection)

**(a) `render_transcript.py` mis-parsed the capture.** The aid decoded a pty
read as a whole string and DROPPED an escape sequence split across a read
boundary — which is the normal case, since a pty delivers a `Write` in several
reads. The dropped sequence was usually an erase-to-end-of-line, so the previous
frame's text stayed under the new line. **Measured**: whole-string feed → 10
screen lines; a byte-by-byte feed of the *same bytes* → 715 lines, the screen
turned into character soup. Fixed with an incremental UTF-8 decoder plus a
carried escape tail. **Proof**: a 1-byte, a 3-byte and a whole-string feed now
produce **byte-identical** output.

**(b) `gopi ui` laid out for 80×24 in EVERY terminal.** `uiRunTUI` passes its
counting writer (`ScreenWriter`) as the Bubble Tea output; Bubble Tea decides
whether to query the terminal by testing that writer for being a tty — an
io.Writer is not one — so **no `WindowSizeMsg` was ever emitted** and the model
kept its hardcoded default. **Measured on real ptys**: at 45 columns the model's
line ran 45 wide, but at **120 columns it still stopped at 80** while the kernel
wrapped the banner at 120. Fixed without new dependencies: `uiclient.Config`
gains `Width`/`Height` (0 = keep the default) and `cli_ui.go` reads
`TIOCGWINSZ` via `golang.org/x/sys/unix` — **already a direct dependency**
(`go.mod`: `golang.org/x/sys v0.37.0`). Regression guard:
`internal/uiclient/geometry_test.go`.

**(c) The pty driver let a tool-using turn write into the worktree.** The driven
turn is a REAL LLM turn with tools on, and the step that asks for the numbers
1..40 made the model write `numbers.txt` **into the worktree under test** — the
untracked litter Rule 30 forbids, produced by a test. Fixed: the driver now runs
the child in a scratch cwd it creates (`--cwd` to override), so anything the
turn writes belongs to the scratch tree.

**A third artifact, and why it is NOT a defect.** The live step snapshots showed
text such as `... tool events 2ents 2` and `exit=0, output:`. Both are the aid's
rendering of a **partial repaint**: the model patches only the tail of a line
(`\r` + the changed suffix, then `ESC[<n>A` back to the home position), and the
aid's screen model appends the suffix at the current cursor instead of
overlaying it — so the new tail lands after the old one. Proof that the surface
itself is fine: replaying **only the final repaint frame on a cleared screen**
yields the correct line (`idle · turns 2 · tool events 2`) with **zero**
artifacts, and the final screen of the whole capture is clean. The raw byte
capture is faithful; the aid's incremental overlay is not. Recorded here rather
than smoothed over — and the authoritative screen evidence is §4's Go dump,
produced by the code under assertion.

### 4. The verification the promise actually needed (see Verification)

Build/static-link, a real-LLM pty drive, an asserted screen-model dump for a
tool-call turn, per-turn redraw cost, non-TTY behaviour, and the untouched
plain-`gopi chat` path.

## Not Done (honest)

- **The `gopi ui` binary was NOT committed.** It is a build artifact
  (`bin/` is gitignored, `.gitignore:2`); the branch carries the SOURCE. The
  pasteable command therefore builds first (one line, `&&`-chained).
- **`drive_ui.py`'s per-step snapshots still render the transcript region
  imperfectly.** Root cause is now understood and partly fixed (see §3a, §3c and
  the driver's new `SNAPSHOT_SETTLE`): the aid's screen model mis-overlays a
  PARTIAL repaint — the model patches only the tail of a line, and the suffix is
  appended at the cursor rather than overlaid, so the live snapshot can read
  `exit=0, output:` where the model wrote `Ran it — output:` and
  `tool events 2ents 2` where the frame's own final state is
  `idle · turns 2 · tool events 2`. **It is an aid defect, not a surface
  defect** — proven by replaying only the final frame on a cleared screen, which
  is correct and artifact-free. The **raw capture is faithful**, and the
  authoritative screen evidence for this report is the Go screen-model dump,
  which is generated by the code under assertion. A future task should port the
  aid's screen model onto a proven terminal emulator rather than hand-rolling
  the CSI overlay; it is documented as such, not hidden.
- **The screen-model dump is capped at 20 transcript rows.** `model.View()`
  renders the visible viewport, not the scrollback, so a turn longer than the
  viewport shows its tail. The report's dump therefore prints the terminal view
  AND the same turn at full height (where the whole turn is visible). An agent
  that needs an off-screen row must page the transcript (`PgUp/PgDn`) — there is
  no "dump the whole scrollback" entry point. Worth adding; not added here.
- **The pre-fix transcript region shows duplicated answer text on the FIRST
  turn only** (`gopi> pty-okpty-ok`). That is the ENGINE emitting the answer
  twice, not the UI: the session JSONL for that turn holds **two** assistant
  records with the same text (the non-streaming fallback path plus the streamed
  deltas). Reproduced outside this branch's code; not fixed here — it belongs to
  `gopi web`'s engine, and fixing it would change product behaviour outside this
  task's scope. Reported so it is not re-discovered as a TUI bug.
- **No `--addr`-style LAN/remote pty run.** Everything measured is loopback on
  HP, Linux/amd64, no X, real pty.
- **A pre-existing flake in the plain-`chat` tests is reported, not fixed.**
  `TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights`
  (`lineedit_height_test.go`) fails intermittently — **measured 2 failures in 6
  runs on PURE master** (`f90db8d6`, a detached worktree with zero changes from
  this task: `row 2 = "you> h", want … "hello"`). So `-run Chat` is flaky on
  master today, independent of this branch. It is a real defect in the chat
  surface's layout accounting and deserves its own task; fixing it here would
  change product behaviour outside this port's scope.
- **The old branch is left untouched** (Rule 23/Rule 9): no archive tag, no
  deletion, no push.
- **`go test ./...` (whole tree) was NOT run** — Rule 27's merge/verify rule is
  smoke, branch-scoped. The scoped gate below is what was run.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./cmd/agent/` → BUILD_OK (offline:
  `GOPROXY=off GOFLAGS=-mod=mod`)
- **Static link**: `file` → statically linked; `ldd` → not a dynamic executable
- **Tests** (branch-scoped, the diff's packages): `internal/uiclient` ok,
  `cmd/agent` (UI|CLIContract|Completion|Usage|Help|Term|Geometry) ok,
  `cmd/agent -run Chat` ok, `internal/config` + `internal/scratch` ok
- **Real LLM pty drive**: `drive_ui.py` 7/7 steps, exit 0 (the status line
  reached `idle · turns 4`)
- **Screen model**: a Go test over a CAPTURED real tool-call turn — 6/6
  assertions pass
- **Branch**: `feat/gopi-cli-bubbletea-landing` in
  `~/workspace/gopi-cli-bubbletea-landing`, unmerged, worktree left in place
- **Master**: unchanged, `feat/gopi-cli-bubbletea` and `keep/hp-master-17`
  untouched

## Evidence (self-filed)

**Build + static binary** (`Evidence` dir = the session's
`.../01a0bedc-…/evidence/`):
```
$ CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $EV/gopi ./cmd/agent/ && echo BUILD_OK
BUILD_OK
$ file $EV/gopi
$EV/gopi: ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked, BuildID[sha1]=4ef3bec830fa18477f7e9d619d04e31a714419ae, stripped
$ ldd $EV/gopi
	not a dynamic executable
```

**The real-LLM pty drive** — 7/7 steps, and the counters the TUI itself
measured (one line, printed by the binary when `GOPI_UI_SELFTEST=1`):
```
COUNTERS render_frames=48 render_bytes=10626 turns=4 turn_frames=[6 5 7 12]
DRIVE_RC=0
steps completed: 7/7
```
`turn_frames` is the per-turn redraw cost: **6 / 5 / 7 / 12 flushed frames per
turn** (the 12 is the turn that was interrupted mid-stream, so it was repainted
until Ctrl+X landed). The four turn markers the driver waits on were each
observed, i.e. the status line reached `idle · turns 4`.

**The screen-model dump + its assertions** (real tool-call turn — the captured
HTTP response body of `POST /api/chat`, 44 delta frames + 2 tool frames +
1 done frame):
```
$ GOPI_UI_SCREENMODEL_DUMP=$EV/screenmodel-tool-call-turn.txt \
    go test -count=1 -v -run "Real|ScreenModelReader" ./internal/uiclient/
    ASSERT tool call row            "callbashhead-3go.mod"         pass=true
    ASSERT tool result row          "resultbashexit=0"             pass=true
    ASSERT tool output on screen    "github.com/nick/gopi"         pass=true
    ASSERT status line at rest      "idle·turns1·toolevents2"      pass=true
    ASSERT not still streaming      "streaming…"                   pass=true
    ASSERT no fault recorded        "  ⚠ "                         pass=true
--- PASS: TestRealToolCallTurnScreenModelIsAgentReadable
--- PASS: TestScreenModelReaderRefusesMalformedFrame
--- PASS: TestRealTurnCountersMatchTheModelState
```
The dump itself (what an agent reads — no ESC bytes, asserted):
```
# frames:  delta=44 tool=2 done=1
# === SAME TURN, FULL HEIGHT (the assertions' screen) ===
gopi ui → http://127.0.0.1:8090  (tools on)
you> run the bash tool with exactly: head -3 go.mod
gopi> I'll run that exact command.Done.
      ```
      module github.com/nick/gopi
      go 1.24.0
      ```
      Module `github.com/nick/gopi`, Go 1.24.0.
  ⚙ call bash head -3 go.mod
  ⚙ result bash exit=0
idle · turns 1 · tool events 2
```

**Non-TTY behaviour** (stdin `/dev/null`, stdout a regular file):
```
$ $EV/gopi ui --tools < /dev/null > $EV/nontty-ui.out 2>$EV/nontty-ui.err; echo rc=$?
rc=1
$ wc -c < $EV/nontty-ui.out
0
$ sha256sum $EV/nontty-ui.out $EV/nontty-ui2.out      # a second identical run
e3b0c442…b7852b855  nontty-ui.out
e3b0c442…b7852b855  nontty-ui2.out
$ tail -2 $EV/nontty-ui.err
gopi ui: ui: could not open a new TTY: open /dev/tty: no such device or address
gopi web: server stopped, chat session finalized.
```
So: non-TTY stdout is **0 bytes and byte-identical** across runs (nothing is
ever printed to it), and the refusal is on **stderr with exit 1** — honest, not
a silent success. The TUI itself never runs on a non-TTY (it re-opens
`/dev/tty`).

**The geometry defect, red then green** (pty captured at 120 columns, the
model's own line measured in the replay):
```
PRE-FIX  (pty cols=120): |  · gopi ui — type a message and press Enter. Ctrl+X interrupts a turn, Ctrl+C q|  (len 80)
POST-FIX (pty cols=120): |  · gopi ui — type a message and press Enter. Ctrl+X interrupts a turn, Ctrl+C quits.|  (len 85)
POST-FIX (pty cols=100): |  · gopi ui — type a message and press Enter. Ctrl+X interrupts a turn, Ctrl+C quits.|  (len 85)
```
The pre-fix line stops at visible column 76 — the model's 80-column default in a
120-column terminal. Guard: `internal/uiclient/geometry_test.go` (both tests
PASS; the zero-value case keeps the default instead of adopting width 0).

**The aid's parser fix, red then green** (same bytes, three feed granularities):
```
feed 1-byte  -> 10 lines
feed 3-byte  -> 10 lines
feed whole   -> 10 lines
$ diff (all three) -> IDENTICAL — boundary-correct
$ grep residue -> 0
```

**The plain-`gopi chat` path** (must survive as the fallback/debugger):
```
$ go test -count=1 -run "Chat" ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	4.234s      # observed in the final consolidated run
$ git status --porcelain | grep -E "cli_chat|chat_stream" || echo "no chat file in the diff"
no chat file in the diff — the fallback carrier is untouched
# NOTE: this set is FLAKY ON MASTER, not here — measured 2 failures in 6 runs on a
# detached worktree of pure master f90db8d6 (lineedit_height_test.go:146/174). Filed
# as its own bug task; see ## Not Done (honest).
```

**Master untouched / unmerged / worktree present** (measured in the HP main
repo):
```
$ git log --oneline -1 master
f90db8d6 delivery-point sync: merge master into the delivered branch before close-out
$ git log --oneline -1 feat/gopi-cli-bubbletea
145738c1 feat(ui): gopi ui — Bubble Tea terminal chat surface (POC #2) …
$ git tag | grep -c "^keep/"; git tag | grep keep/hp-master-17
5
keep/hp-master-17
$ git worktree list
/home/user/work/GoPi                             f90db8d6 [master]
/home/user/workspace/gopi-cli-bubbletea-landing  f90db8d6 [feat/gopi-cli-bubbletea-landing]
$ git merge-base --is-ancestor feat/gopi-cli-bubbletea-landing master && echo MERGED || echo "NOT merged"
NOT merged
```

## The one pasteable command (HP)

```bash
cd ~/workspace/gopi-cli-bubbletea-landing && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && ./bin/gopi ui --tools
```

It is `&&`-chained because the binary is a gitignored build artifact — the
branch carries source, not a checked-in ELF. `--tools` is the switch both
`gopi web` and `gopi ui` read; drop it for tool-free turns. It reads the same
model/API settings as any other gopi process (no new config), starts its own
loopback server (no token needed on loopback) and opens the TUI. `gopi ui -h`
documents `--addr / --server / --token` for attaching to an already-running
`gopi web`.
