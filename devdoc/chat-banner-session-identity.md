<!-- doc-class: historical -->
# Task Report: the chat startup screen now states THIS session's identity

**Task:** `01a0fd00-cf48-77a3-b1c9-c5620b7a90d0` — "启动屏看不到当前会话完整 UUID——修正版"
**Branch:** `task/01a0fd00cf4877a3b1c9c5620b7a90d0` (worktree
`/home/user/workspace/gopi-01a0fd00cf4877a3b1c9c5620b7a90d0`)
**Base:** master at branch creation.

The superseded "最短唯一短号" design is **not** in this branch: no prefix logic was
added, and a short id is never printed as if it were an identity (the ban is asserted
by `TestChatBannerIdentityIsNeverAShortPrefix`).

## Completed

- **`session` is now the panel's FIRST fact.** `chatBannerFacts` (`cmd/agent/chat_banner.go`)
  gained a leading `{label: "session", value: chatBannerIdentityValue(...)}` row and its
  comment now carries the ORDER ARGUMENT: every other fact is obtainable elsewhere on the
  same screen (model → status row's first column and the answer band's byline, version →
  the title bar one row above, tools → the status row and the historic one-liner, node →
  the startup `[gopi] node:` notice, dir → the ground notice); the session's own id is the
  only fact a reader cannot get from this screen by any other means, and it is the operand
  they actually need (`gopi log <id>`, `make replay SESSION=<id>`, `gopi chat --resume <id>`).
  It is also the only UNGUESSABLE value on the panel — every other value is a name a reader
  can recognise.
- **The value is the FULL 36-cell uuid whenever the panel's value column can hold it, and a
  visibly ELIDED uuid when it cannot** — `chatBannerIdentityValue`, one function, two shapes:
  the whole uuid (36 cells, the operand), or the uuid cut in the MIDDLE with
  `chatBannerCutMarker` between the kept head and tail (eyeball-only, matchable against
  `/log`'s own listing; deliberately NOT a shorter operand). Below `chatBannerIdentityKeepMin`
  the value degrades to the marker alone rather than to a stub that looks like a value.
- **The panel's own geometry was fixed to hold it.** Measured cause: the panel's width is
  derived from its CONTENT (the widest tip) and a tip's width is terminal-dependent — the
  silent-terminal newline line is 71 cells, the kitty-capable one 29 — so on a
  kitty-capable terminal a tips-only panel came out **45 cells**, i.e. a **35-cell value
  column for a 36-cell uuid**, and the panel's own leading fact would have elided on a
  terminal with room for it. The panel's width demand now includes
  `chatBannerFactCells+1+fullid.UUIDLen` (derived from `fullid.UUIDLen`, Rule 1) when the run
  has a session, i.e. 46 cells — under `chatBannerPanelMaxCols` (71), so it never enlarges
  the panel past the tip-sized maximum. `chatBannerFactValueCells` (24) and
  `chatBannerPanelMaxCols` (71) were **not** widened; `chatBannerFactValueCells`' comment now
  records why raising it would buy the identity nothing.
- **A real in-UI path to the whole id, proven to be answered.** One new rotating tip
  (`/log sessions lists every session id in full`) names the REPL's own subcommand,
  spelled through `logCommandName` (new const) + `chatBannerLogListSub` (new const); the
  registry entry in `commands.go` is now keyed and `Name`d by that same constant. A REAL
  `gopi chat` run was driven with that line typed into it and answered with the session
  table (full 36-cell uuids), including the very session whose id the panel had shown
  (`evidence/slash-log-sessions.txt`, line 21) — so the fallback path is not a claim about
  a switch's source, it is a command that ran.
- **Evidence**, under this run's session dir
  (`/home/user/.gopi/sessions/01a0fd00-cf48-77a3-b1c9-c5620b7a90d0/evidence/`):
  `pty.raw` (the live run's byte stream), `startup-screen-panel.txt` (the panel's rows,
  ANSI-stripped), `banner-snap.dump` / `live-banner.dump` (the run's own
  `--dump-screen` artifact), `ptybanner.py` + `watchbanner.py` (the pty driver).

## Not Done

- **The startup panel is not in `--dump-screen`'s `grid` section, and this is not this
  task's doing.** The banner is committed to the terminal and then the run opens an
  INTERIOR scroll region (`\x1b[1;25r`), so the panel's lines scroll off the region's top
  margin and are discarded before any dump is written. The artifact says so itself, in its
  own `blind_spots` block: `interior_scroll_loss|a line pushed off an INTERIOR
  scroll-region margin is discarded before it reaches the scrollback`. So the dump's
  `lines`/`grid` sections carry the settled screen (status row, `you>`), and the panel's
  evidence is the pty byte stream + the rendered text, not the dump's grid. Verified on
  master's own binary as well (`testsuite` startup test: the box glyphs are in neither
  section there either) — this is a pre-existing property of the dump, not a regression.
  FIXING IT IS A DIFFERENT TASK (the dump is `ui_state`'s evidence channel and the region
  is closed before the first sample; that fight belongs to the dump's own feature).
- **The elided shape was not observed on a real terminal in this run.** On this host the
  panel is drawn at 71 cells (a ≥100-column terminal), so the value column always holds 36
  cells and the code takes the whole-uuid arm. The elided arm is covered by unit tests
  (`TestChatBannerIdentityElisionKeepsBothEnds`, `TestChatBannerIdentityIsNeverAShortPrefix`)
  and by the geometric case (`TestChatBannerIdentitySizesThePanelItsOwnRoom`), not by a
  photographed narrow terminal. A reader wanting that photograph needs a terminal below
  ~100 columns, where the box is declined entirely — i.e. the elided arm is reachable only
  by a panel drawn at a width no tip on this host produces.
- `chatBannerSessionFactCount` went 5 → 6 (the capacity constant and the fact list are one
  decision); `chatBannerRows`' row budget is unchanged, so on a very short terminal the
  TAIL of the fact list is still what gives way — which is the point of the new order.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **OK** (run twice, before and
  after the geometry fix).
- Tests: `go test ./cmd/agent/ -run 'ChatBanner|Banner' -count=1` → **ok**
  (5.4s, all banner cases incl. the 6 new ones).
  `make test-budget PKGS='./cmd/agent ./testsuite'` → **exit=1**: 15 cases failed
  (`TestChatPTY_StartupScreenShowsTheLogoAndItsSidePanel` among them) under that run's load
  (`load_start=9.77 7.32 4.39`, 4 cores; the budget did NOT trip). Re-run in isolation the two
  `cmd/agent` ones **PASS**, and `TestChatLayerFormat_OnRealPTY` in `testsuite` **FAILS on
  master's own tree** (separate `git archive master` checkout) — the real-PTY family on this
  host under load, not this diff. Every `ChatBanner` case is green in every run.
- Ran:
  1. `python3 evidence/ptybanner.py <bin>/gopi <dump> evidence/banner-snap.dump 117 28 evidence/pty.raw`
     — a REAL `gopi chat --dump-screen=…` on a real pty, 117×28, snapshotting the dump
     **while the run is alive** (the surface wipes the dump at close-out).
  2. `printf '/log sessions\n/quit\n' | ./bin/gopi chat` — the **panel's own advice**,
     typed into a real REPL.
  3. `make replay SESSION=2b9abecc-3df7-4e03-af97-23f9154c5646 OUT=… EMU=xterm` — the id
     **read off the startup screen**.
  4. `bin/gopi log 2b9abecc-3df7-4e03-af97-23f9154c5646`.
- Result: the rendered panel, stripped from the pty's own byte stream
  (`evidence/startup-screen-panel.txt`):
  ```
  │                                           │ This session                                                          │
  │    interactive chat, in this terminal     │ session  2b9abecc-3df7-4e03-af97-23f9154c5646                         │
  │                                           │ model    deepseek-flash                                               │
  │              deepseek-flash               │ dir      …gopi/sessions/01a0fd00-cf48-77a3-b1c9-c5620b7a90d0/evidence │
  │              peerbox (server)               │ node     peerbox (server)                                               │
  │ …d00-cf48-77a3-b1c9-c5620b7a90d0/evidence │ version  v0.1.0                                                       │
  │                                           │ tools    tools off                                                    │
  ```
  `gopi log 2b9abecc-3df7-4e03-af97-23f9154c5646` (the value COPIED OFF THAT SCREEN)
  answered with that session's own block — `Session: 2b9abecc-…`, `Started: 2026-10-02
  22:53:07`, `Workdir: …/evidence` — i.e. the on-screen value IS the operand; and
  `/log sessions` (the panel's own advice, typed into a live REPL) printed the session
  table with that same full id — `2b9abecc-3df7-4e03-af97-23f9154c5646 deepseek-flash
  4.8s 0 iter 0 tok EXIT=1` (`evidence/slash-log-sessions.txt`, line 21), i.e. the in-UI
  path from an elided identity to the whole one is a command, not a claim.
- **`make replay SESSION=<the on-screen value>` could not complete on this host, and the
  reason is environmental, not the operand.** `replay_resolve_session` RESOLVED the operand
  correctly — `[replay] session=/home/user/.gopi/sessions/2b9abecc-… (from operand
  '2b9abecc-…')`, `out-tag=2b9abecc-20261002` — and then the terminal module could not
  realise a window on its private Xvfb display: `FATAL: 私有 display :63 上等不到
  xterm 的真尺寸窗口（等了 30×15.0s）`, and the same for `xfce4-terminal` in the auto
  run, with the module's own diagnosis (a GTK terminal needs a WM on a bare Xvfb;
  `metacity`/`mutter`/`openbox` are **missing** on this host). So the replay arm's proof is
  the RESOLUTION line, not a finished recording; the operand itself was proven by `gopi
  log` (which parsed the id and read that session's records).
- Commit: `git log --oneline -1` in this worktree (see the row's own delivery block).

## Evidence (self-filed)

- `grep -c` over the live run's byte stream: `'\xe2\x95\xad'` (box top-left) = **1**,
  `'This session'` = **1** — the panel was painted once, on this run.
- The uuid printed on the screen is the SAME directory the log resolved:
  `/home/user/.gopi/sessions/2b9abecc-3df7-4e03-af97-23f9154c5646` exists and
  `gopi log` printed `Session: 2b9abecc-3df7-4e03-af97-23f9154c5646`.
- `banner-snap.dump`'s own header: `#gopi-chat-dump pid=1147628 target=self:chat
  terminal=117x28 …` — the geometry the panel was sized for.
- The dump's `blind_spots` block, verbatim, carries
  `interior_scroll_loss|a line pushed off an INTERIOR scroll-region margin is discarded
  before it reaches the scrollback` — the recorded reason the banner is not in the grid.
- `printf '/log sessions\n/quit\n' | ./bin/gopi chat` → `grep -n 2b9abecc-…` over the
  captured output → line 21: `2b9abecc-3df7-4e03-af97-23f9154c5646 deepseek-flash 4.8s 0 iter
  0 tok EXIT=1` — the panel's advice, executed, listing the panel's own session in full.
- `make replay SESSION=2b9abecc-…` printed `[replay] session=/home/user/.gopi/sessions/2b9abecc-3df7-4e03-af97-23f9154c5646 (from operand '2b9abecc-3df7-4e03-af97-23f9154c5646')`
  and then `FATAL: 私有 display :63 上等不到 …真尺寸窗口` — resolution succeeded,
  the terminal module's window acquisition failed (metacity/mutter/openbox reported missing
  by the module's own dependency check in the same output).
- `go test ./testsuite/ -run TestChatPTY_StartupScreenShowsTheLogoAndItsSidePanel -count=1`
  fails **on master's own tree** as well (`git archive master` → separate dir → build →
  run: FAIL, same shape) — i.e. that pre-existing failure is not caused by this diff.
- `./bin/gopi doccheck` → `doccheck: the devdoc corpus is consistent — every file under
  devdoc/ has a well-formed row` (the `write` of this report classified itself in
  `devdoc/DOC-INDEX.tsv`, one row added, no hand edit).
