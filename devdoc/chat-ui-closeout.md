<!-- doc-class: historical -->
# Task Report: chat REPL — the `/`-menu's Enter, one command two spellings, and the close-out screen

Task `01a0d508-2fea-7dbc-b223-8fc103f10350` (worktree
`/home/user/workspace/gopi-01a0d5082fea7dbcb2238fc103f10350`, branch
`fix/chat-menu-enter-and-closeout` — renamed from the runner-assigned
`task/01a0d5082fea7dbcb2238fc103f10350` per Rule 6a (the rename is the whole
cost of a descriptive name; no second worktree was created).

Three operator-reported defects in the interactive chat UI, all reproduced on a
real PTY first, then fixed and re-measured. The operator's framing — "loop the UI
into development" — is why every claim below is a CELL-GRID reading from a real
`gopi chat` run, not a callback assertion.

## Completed

### 1. `/quit` and `/exit` are ONE command, and the UI now says so

**The report** (attachment `tmp/ui_exit.txt`, verbatim):

```
you> /
  /log (Usage: /log <subcommand> [args])
  /new (Usage: /new)
  /task (Usage: /task <subcommand> [args])
  /quit (session)
> /exit (session)
deepseek-flash   ·  iter 8
```

Two rows, two labels, no information — the operator asked what the difference
was. There is none: the dispatcher accepts both and does the same thing (the
sibling `/clear` row already read `forget the conversation and start fresh (also
/new)`, so the *convention* existed and `/quit` simply did not follow it).

- `internal/lineedit/menu.go`: `Candidate` gains `Alias string` — the other
  spelling of the SAME behaviour. The COMPLETER owns that knowledge (the widget
  must not learn command names); the menu draws it as part of the row's note.
- `cmd/agent/lineedit_wire.go`: `chatSessionAliases` maps each spelling to its
  twin **in both directions** (the pair is symmetric — declaring one canonical
  would be a claim the dispatcher does not make), and `candidateFor` attaches
  `alias of /exit` to the `/quit` row. The menu now reads
  `/quit (alias of: /exit)` beside `/exit (alias of: /quit)`.
- `cmd/agent/cli_chat.go`: the `/help` row is
  `end the session and exit (also /exit — one command, two spellings)`, and
  `quitCommandUsage` (`/help quit`) states the same, plus that Ctrl+D is the
  third spelling of the same action.

Both spellings stay SELECTABLE (a word a menu stops offering is a command the
user can only reach by typing it in full) — the pair is presented, not collapsed.

### 2. Enter on the open menu now TAKES the selection (the menu was a photograph)

**The report**: navigation worked ("selecting a choice does not transfer the
chosen tool into the input slot, so tool choice is useless"). Reproduced on the
PTY (probe `menu.sh before`): typing `/`, then `Down`, then `Enter` submitted the
RAW PREFIX — the editing row came back as `you>` with the menu gone, and the
highlighted `/new` never reached the line (`menu-before-1-menu-open.txt` →
`…-2-menu-down.txt` show the highlight moving; `…-3-after-enter.txt` shows it
discarded).

**Root cause** (`internal/lineedit/editor.go`, the `KeyEnter` arm):
`Tab` was the acceptance key (`MenuAccept`), and `Enter` went straight to
`e.val.Enter(...)` → submit. So the keystroke that means "take this item" in
every other menu sent the raw prefix instead, and the highlighted candidate was
discarded. The `(session)` label above was therefore not merely unclear — the
menu it labelled was inert.

**Fix**: on an OPEN menu, Enter **is** the accept action, decided *before* the
paste-burst classification, and it breaks out of the key handler — no submit on
that keystroke. The second Enter submits what is now on the line.

The one subtlety, measured rather than assumed: "did this Enter take a choice"
is decided by comparing the buffer, not by asking whether a menu is still open.
The completer re-publishes candidates for a *completed* line (so `/task` leaves a
menu offering exactly `/task`), and a blind accept-then-consume would have made
`/task` unsendable — the menu would swallow every Enter for the rest of that
line. The buffer comparison is the honest test of "was a choice applied", so a
no-op accept falls through to the submit.

### 3. The close-out now erases the reserved layout (and parks before erasing)

**The report** (attachment `tmp/ui_cleanup.txt`): on quit/exit the workarea is
not cleaned up. Reproduced on the PTY (probe `cleanup.sh before`): after `/quit`
the grid still held the editing row and the status row, and the shell's next
command ran *on the `you>` row*.

**Root cause**: `DisableRegion` emits the scroll-region reset (`CSI r`) and
nothing else. That restores the terminal's *frame*, not the *screen*: the rows
the layout owned keep their last paint, and the cursor stays parked on the
editing row. Worse, `ED` erases from the cursor — so erasing without parking
first leaves rows above it painted, which is why the fix has a fixed order.

**Fix**: `region.Reset()` = disable the region → `CursorHomeSeq()` (`CSI H`,
added to `internal/sgr`) → `ED(2)`. `Editor.CloseOut()` wraps it and reports
whether the layout was ACTIVE (the historic path must claim nothing and write
nothing). `chatInput.CloseOut()` exposes it to the REPL.

`cmd/agent/cli_chat.go` closes out through **one** `leaveChat` helper that
`/quit`, `/exit`, Ctrl+D, a read error, and a signal all use — erase FIRST, then
print `bye.`, then finalize the session. The order matters and is not stylistic:
the input layer's cleanup is *deferred*, so erasing from there would blank the
goodbye the user just read. One answer to "what is the user left looking at"
instead of one per exit reason.

`cmd/menu` output is untouched: `ED(2)` erases the screen, never the scrollback,
so a user can still scroll back through the session.

## Not Done

- **The web carrier was not re-run.** `internal/lineedit` is the editor path
  (`chatInput.editor`); the web composer has its own Enter handling and no
  reserved layout, so the changed surfaces are not shared. I verified this by
  reading `cli_web.go` (its comment — "Enter never sends while the menu is open —
  the only way to send is to accept first" — is the CONVENTION the terminal path
  now follows) but did not drive the browser. `CloseOut()` returns `false` when
  there is no editor, and the no-op path is pinned by
  `TestEditorCloseOutWithoutARegionIsANoOp`.
- **A mid-session `SIGKILL`** cannot be cleaned up by anyone; the reserve is
  erased on every graceful path (break, Ctrl+D, error, SIGINT/SIGTERM), which is
  what the report is about.
- The menu does not wrap long alias notes onto a second row; on a very narrow
  terminal the note is clipped like any other hint. Not reported, not fixed.
- **Pre-existing failures in the scoped suite, NOT caused by this diff** (each
  reproduced on an unmodified `git archive HEAD` checkout at
  `/home/user/workspace/_tmp/baseline-check`, same commands): `TestG5InventoryMatchesTheHelp`,
  `TestG5SurfaceCensusIsDerivedFromConstants`,
  `TestCLINamingExceptionsDocumentedInTopLevelHelp`,
  `TestCLIUserVerbTokensAreAllFromCliVerbs` (all `cmd/agent`),
  `TestScreenAssertionDoesNotSyncOnProducerCounters` (cited rows are
  `internal/uiclient/dump_test.go`, which this diff never touches),
  `TestDocClassBannerMatchesManifest` (`devdoc/handoff-20260924-preflight.md`,
  `devdoc/worker-resume-ground.md` — other agents' files), and
  `TestTaskRunSeamWritesReportAndShowRendersIt` plus the `Test*E2E` task-lifecycle
  family (`cmd/agent`; that base run was alone on the host and still red).
  I did not fix any of them: they are outside this task's surface.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`; `go build ./...` clean.
- Tests:
  - `go test -count=1 -run 'TestEditorCloseOut|TestChatCompletion|TestChatHelpPanel|TestChatStream' ./internal/lineedit ./cmd/agent/`
    → `ok github.com/nick/gopi/internal/lineedit 1.816s` / `ok github.com/nick/gopi/cmd/agent 0.510s`
  - `go test -count=1 -run 'TestSlashMenu' ./cmd/agent/` → `ok` (4 tests: alias rows,
    coverage, the RENDERED alias note, and Enter-at-the-REPL-seam).
  - `make test-budget PKGS='./internal/lineedit ./internal/sgr ./cmd/agent ./internal/standards'`
    → budget NOT tripped (elapsed 782s of 25m), `exit=1`, and **every** failure in that
    run was re-checked against a pristine `git archive HEAD` checkout
    (`/home/user/workspace/_tmp/baseline-check`) — see `## Not Done` for the list.
    The one failure family the diff DID cause (a line-shifted baseline in
    `internal/standards/screen_assertion_gate_test.go`) was fixed by moving the new
    test above the baselined lines, not by touching the baseline.
- Ran (the surfaces I changed, verbatim):
  - `devdoc/evidence/sgr-grid/termgrid.py ./bin/gopi 100 30 <script>` driven by
    `devdoc/evidence/chat-ui-closeout/{menu,cleanup}.sh <label> ./bin/gopi` — a real
    `gopi chat` on a 100×30 PTY, keys scripted, screen read as a cell grid.
  - `git status --porcelain` in the worktree → empty.
- Result: see `## Evidence` (before/after grids quoted there).

## Evidence (self-filed)

Probes are `menu.sh` (type `/`, `Down`, `Enter`, Ctrl+D) and `cleanup.sh`
(`/quit`, then look at the screen), each run against the same binary path with
the same 100×30 geometry via `devdoc/evidence/sgr-grid/termgrid.py`. Raw grids:
`devdoc/evidence/chat-ui-closeout/`.

**Defect 2, before the fix** (`menu.sh before`, step 3 — after `/`, `Down`,
`Enter`; the grid is quoted verbatim from `menu-before-3-after-enter.txt`):

```
23|
24|
25|
26|
27|
28|you>
29|deepseek-flash   ·  iter 0  ·  tools ·                       ·                         ready
```

The selection was discarded: the highlighted `/new` is nowhere on the line, and
the raw prefix was submitted instead. (`menu-before-1-menu-open.txt` shows the
menu open with `> /log` highlighted, `…-2-menu-down.txt` the highlight moved to
`> /new` — so navigation worked and only the ACCEPT was broken.)

**After** (`menu.sh after`): the menu itself now answers the operator's question
about `quit`/`exit` — `menu-after-1-menu-open.txt`, verbatim:

```
23|you> /
24|> /log (Usage: /log <subcommand> [args])
25|  /new (alias of /clear)
26|  /task (Usage: /task <subcommand> [args])
27|  /quit (alias of /exit)      <- the operator's question, answered ON the row
28|  /exit (alias of /quit)
```

and the same three steps that discarded the choice before now carry it
(`menu-after-3-after-enter.txt`):

```
27|you> /new           <- Down selected /new and Enter took it into the input slot
28|> /new (alias of /clear)
```

**Defect 3, before the fix** (`cleanup.sh before`, `closeout-before-2-after-exit.txt`
— the grid at the moment the app process is gone; the emitted bytes are quoted
below it because this tool does not instrument the cursor):

```
27|
28|you> bye.                              <- the editing row survived, and the goodbye
29|deepseek-flash · iter 0 · tools · …     landed INSIDE it
```

The trailing bytes of that same capture say why: `...^[[29;1H^[[2Kyou>^M^[[5Cbye.\n^[[r^[[?2004l^[[?25h`
— the goodbye is written at column 6 of the row the editing prompt occupies,
and `^[[r` (scroll-region reset) is the ONLY cleanup, emitted *after* it.

**After** (`cleanup.sh after`, `closeout-after-2-after-exit.txt`):

```
 0|bye.          <- the goodbye is the ONLY thing on the screen
 1|              <- the layout's rows are gone
```

The same tail reading, red→green: `...^[[29;1H^[[2Kyou>^[[0m^M^[[5C^[[r^[[H^[[2Jbye.\n^[[?2004l^[[?25h`
— cursor parked (`^[[H`) and the screen erased (`^[[2J`) **before** the goodbye,
so `bye.` is painted onto a blank screen at (1,1) rather than into the editing row.

**Red → green, each fix reverted with its test kept:**

```
# fix 1 (Enter accepts the selection), editor.go reverted:
--- FAIL: TestChatCompletion_EnterTakesTheSelectedCandidate (2.01s)
FAIL	github.com/nick/gopi/internal/lineedit	3.053s

# fix 3 (close-out), Editor.CloseOut reverted to DisableRegion:
--- FAIL: TestEditorCloseOutClearsTheReservedLayoutAndParksTheCursor (0.00s)
    closeout_test.go:101: after CloseOut the screen still carries "committed-answer-line" at row 8 — the layout's rows were not erased
FAIL	github.com/nick/gopi/internal/lineedit	0.400s
```

Both test files, restored: `ok github.com/nick/gopi/internal/lineedit`.

**Rule 31 (the changed surface was RUN)**: the binary was rebuilt from this
branch and the two real-PTY probes re-run against it — `menu.sh after` and
`cleanup.sh after`, whose grids are the ones quoted above
(`menu-after-1-menu-open.txt`, `menu-after-2-menu-down.txt`,
`menu-after-3-after-enter.txt`; `closeout-after-1-just-after-quit.txt`,
`closeout-after-2-after-exit.txt`), plus the driver log `run-after.log`.

Commit: see the branch tip (`git log --oneline -1`).
