<!-- doc-class: historical -->
# The chat surface's ideal geometry: one knob, one resolver, two consumers

**Task**: `01a0ffbb-a79a-7eec-9068-58648941ffde` — *Give the chat surface a
declared ideal geometry as a first-class `internal/config` knob.*

## The defect this closes

The chat surface's chrome is drawn for one size:

- the startup screen's boxed mark with its side panel of tips (`chat_banner.go`),
- the reserved editing line plus its status row (`internal/lineedit/region.go`),
- the info/status bar's aligned column list (`statusColumnsAtCols`,
  `chat_stream.go`).

Every live measurement in this repo was taken at **117×26** — the operator's own
terminal, the pty every dump test allocates, the film's `--cols`/`--rows`. But
that pair was **not declared anywhere**: it existed as two independent bare
literal pairs —

```go
// the old driver (internal/replay/replay.go, since replaced by
// cmd/agent/chat_film.go on master) — before
defaultCols = 117
defaultRows = 26

// cmd/agent/chat_replay_beats.go (before)
chatBeatDefaultCols = 117
chatBeatDefaultRows = 26

// cmd/agent/chat_film.go (master's new film driver) read the SAME pair
if cols <= 0 { cols = chatBeatDefaultCols }
```

— with nothing but convention holding them equal to each other, and **nothing at
all** tying any of them to the size the LIVE `gopi chat` surface draws at. Two
consequences, both real:

1. **The live surface had no way to declare its own size.** On a terminal wider
   than 117 columns the layout simply stretched: the box was drawn at whatever
   width the terminal had, and the info bar's pinned columns could not use the
   extra room anyway. The screen a 200-column operator saw was not the screen the
   design describes.
2. **The film could drift from the thing it films.** The replay/beat defaults were
   a second, independent idea of the designed size — correct only for as long as
   nobody changed one of them.

## What was built

**The knob** — `internal/config/chat_geometry.go` (new), following the
`memory_prefetch` / `output_language` precedent exactly:

| channel | spelling | rank |
|---|---|---|
| CLI flag | `--chat-ideal-geometry COLSxROWS` | 1 |
| env | `GOPI_CHAT_IDEAL_GEOMETRY=COLSxROWS` | 2 |
| settings | `"chat_ideal_geometry": "COLSxROWS"` | 3 |
| built-in | `chatIdealGeometryDefault = "117x26"` | 4 |

ONE resolver — `config.ChatIdealGeometry()` — returning
`ChatIdealState{Cols, Rows, Source}`. The **source label rides the answer**,
because "the screen is 117 wide because you asked for it" and "because nobody
asked" are two different facts about one number, and one of them has a remedy the
other does not (declare a smaller ideal).

ONE validator — `parseChatIdealGeometry` — used by every channel *including the
built-in default*, so a typo in the constant cannot produce a geometry no channel
could express.

**Rule 15 discipline, deliberately soft.** A value that is not a `COLSxROWS` pair
is **reported on stderr and the next channel decides** — never silently read as
unset (the operator would never learn their declaration was ignored) and never
fatal for the settings channel (a typo must not make `gopi chat` unusable; this is
the same soft policy the settings channel of `output_language` uses).

**The flag is hoisted, and also accepted post-verb.** `BootstrapEnvFromArgs` runs
before the subcommand chain, which is what makes `gopi --chat-ideal-geometry
100x30 chat` reach chat. The chat verb ALSO accepts the post-verb spelling
(`gopi chat --chat-ideal-geometry 100x30`, and `=100x30`), published through the
**same writer** (`config.PublishChatIdealGeometryFlag`) — so there is one
representation and one precedence rule (the flag writes the variable
unconditionally, so a flag always beats an operator's export), and the verb's
closed vocabulary does not refuse a flag its own help page advertises.

**What the surface does with the answer** (`cmd/agent/chat_geometry.go`, new):

- **Canvas**: `lineedit.Editor.SetCanvasCols(min(terminal, ideal))`. The layout
  installs at that width; the remainder to the right is left alone. A terminal
  *wider* than the ideal now draws the chrome at the designed size.
- **Degradation**: a terminal *narrower* than the ideal is **told what it lost**
  (Rule 15), in the words the help page's effect claim uses, naming both sizes,
  the source label, and which piece of chrome yields: the box (`cols <
  chatBannerMinCols()`), else the info bar (`cols < statusColumnsAtCols`), else
  only the answer band is short of rows.

**Rows are never imposed — and that is a decision, not an omission.** The status
row is anchored to the screen's LAST row (`region.go`), so a canvas claiming
fewer rows than the terminal has would park the status line in the middle of the
screen — the "composer is not at the bottom" defect the layout's own comments
record. The ideal's *row* half therefore appears only in the degradation sentence
and in the film's default.

**The canvas mechanism is lineedit's, not a second geometry**: `SetCanvasCols`
feeds the ONE `canvasWidth()`, and that one function is what `EnableRegion`, the
`EventWinch` resize path and the answer renderer's `SetCols` all read. Zero (the
default) means "no canvas — the terminal's own width", so **every existing caller
is byte-identical**; at the built-in 117×26 the `min()` is an *identity*, which is
why the existing pty suite sees exactly the byte stream it always saw (asserted:
`TestChatGeometryCanvasIsIdentityAtTheDefaultGeometry`).

**The copies are gone**: the beat protocol's defaults and the film driver's
`--cols`/`--rows` defaults now call the same resolver
(`chatBeatIdealCols`/`chatBeatIdealRows`), and the constant pairs were deleted
outright. The third copy this branch also deleted lived in the old
`internal/replay` driver — **master has since replaced that whole driver with
`cmd/agent/chat_film.go`** (a sibling task landed `gopi chat --film DIR` while
this branch was in flight), and the merge routed the new file's defaults through
the same accessors, so the source of truth survived the replacement: driver,
beat protocol and live surface state ONE geometry.

## Files

| file | change |
|---|---|
| `internal/config/chat_geometry.go` | **new** — the knob, the resolver, the validator, the row summary |
| `internal/config/chat_geometry_test.go` | **new** — priority chain, fall-through-and-report, validator, one-source-of-truth |
| `internal/config/config.go` | `ChatIdealGeometry` settings field |
| `internal/config/env.go` | settings channel published into the knob's ONE env representation |
| `internal/config/flags.go` | the flag parsed as a plain string (validation stays at the ONE validator) |
| `internal/config/settings.go` | hoist entry (`PublishChatIdealGeometryFlag`) + the env catalog row |
| `internal/config/help.go` | the top-level discoverability row |
| `cmd/agent/chat_geometry.go` | **new** — the notice, the canvas width, the apply/install call |
| `cmd/agent/chat_geometry_test.go` | **new** — the decision, the flag's vocabulary contract |
| `cmd/agent/chat_geometry_pty_test.go` | **new** — the install on a real pty (200-col cap, 80×24 degradation) |
| `cmd/agent/lineedit_wire.go` | resolve+apply, in the ONE window where the editor exists and the layout has not installed |
| `cmd/agent/cli_chat.go` | the post-verb flag, `chatREPLFlags()`, the usage line |
| `cmd/agent/chat_replay_beats.go` | beat defaults read the resolver; the local pair deleted |
| `cmd/agent/chat_film.go` | (master's file, edited at merge) the film driver's geometry defaults read the same accessors |
| `internal/lineedit/editor.go` | `SetCanvasCols`, `canvasWidth()`, install + resize use it |
| `internal/lineedit/winsize.go` | `TerminalRows` exported (a second `TIOCGWINSZ` at the call site would be a second copy) |
| `internal/lineedit/canvas_test.go` | **new** — the cap, never-wider-than-terminal, zero = terminal, survives resize both ways |
| `CONFIG.md` | the knob's §2c row + the "where each half lives" note |
| `cmd/agent/cli_chat_web_help_surface_basic_test.go` | the G5 census pin, re-derived at 40 (14 for `gopi chat`) |
| `internal/standards/config_doc_flag_gate_test.go` | the table's audit floor 59 → 60, with the reason |

## Not Done (honest)

- **The FILM path is not exercised end to end here.** Master replaced the old
  `internal/replay` driver with `cmd/agent/chat_film.go` while this branch was in
  flight; the merge routed that file's geometry defaults through the same
  resolver, so the driver, the beat protocol and the live surface state ONE
  geometry by construction — but no `gopi chat --film DIR` run in this branch
  renders frames at a declared non-default geometry (a film needs the X display
  server and is the sibling task's surface). The four CHANNELS were each driven
  for real on a live chat instead (below).
- **`statusColumnsAtCols` is a definition of record for the pinned-row width, not
  a catalog entry.** The degradation notice reuses the existing constant; no new
  bridge was added between the two halves of the chrome (the row-count half of the
  notice names the answer band, which is measured by the layout's own budget, not
  by a named constant here).
- **The pty resize half lives in `internal/lineedit/canvas_test.go`**, not in the
  chat pty test: the chat pty fixture's winch step would assert the same
  `canvasWidth()` call twice. The chat side asserts what it *declares*, the
  lineedit side asserts the *mechanism* — the division is deliberate and stated in
  both files.

## Verification

- Build: `make build` → `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Tests: `make test-budget PKGS='./internal/config ./internal/lineedit
  ./internal/standards ./cmd/agent'` — graded by package:
  `internal/config`, `internal/lineedit`,
  `./cmd/agent`'s geometry/G5/G6/sync-peers families all green; the
  `internal/standards` reds are **pre-existing on master** and named in
  `## Not Done` above (they are in files this branch does not touch —
  `git diff master --name-only -- internal/tools/` prints nothing).
- Ran: `gopi chat` was executed for real on real ptys, one run per channel —
  `script -qec "stty cols W rows H; <binary> <argv>"` with `/quit` on stdin —
  and the installed canvas was read back off the painted box (the widest
  `│`-bearing row of the run's own output):

  | declaration | pty | canvas measured | notice |
  |---|---|---|---|
  | `gopi --chat-ideal-geometry 101x31` (pre-verb) | 200×40 | **101** | — |
  | `gopi chat --chat-ideal-geometry=103x33` (post-verb) | 200×40 | **103** | — |
  | settings `{"chat_ideal_geometry":"104x28"}` | 200×40 | **104** | — |
  | `GOPI_CHAT_IDEAL_GEOMETRY=100x30` | 200×40 | **100** | — |
  | (none — the built-in default) | 200×40 | **117** | — |
  | `GOPI_CHAT_IDEAL_GEOMETRY=wide` (unusable) | 200×40 | **117** | `config: GOPI_CHAT_IDEAL_GEOMETRY="wide" is not a COLSxROWS pair of positive integers — the next channel decides; the built-in default (117x26) decides` |
  | (none) | **80×24** | **80** | `this terminal is 80x24, smaller than the chat surface's ideal 117x26 (default): the info bar is drawn in its reduced form (80 of 100 pinned columns)` |

- Result: every channel installed the canvas it declared; a 200-column terminal
  never stretched past the ideal; the unusable value was reported and the run
  continued at the next channel; the 80×24 terminal kept its own width and was
  told which piece of chrome yielded. `gopi chat --help` lists the flag
  (`grep -c chat-ideal-geometry` = 4). `./bin/gopi doccheck` reports the corpus
  consistent. `git status --porcelain` empty.
- Commit: `f8034464` 

## Evidence (self-filed)

- `go build ./...` → exit 0.
- `go test ./internal/config/ -run 'ChatIdeal' -count=1` → `ok` (0.625s).
- `go test ./internal/lineedit/ -run 'Canvas' -count=1` → `ok` (0.246s).
- `go test ./cmd/agent/ -run 'ChatGeometry|ChatIdeal' -count=1` → `ok` (0.789s).
- `go test ./cmd/agent/ -run 'TestG5|TestToolsMaintenanceHelpSurface' -count=1`
  → `ok` (25.4s).
- `go test ./cmd/agent/ -run
  'TestVersionReportDoesNotBlameACheckoutTheBuildStoodIn|TestVersionReportStampingIsSkippedInAWorktreeEvenWhenBuildvcsIsTrue'`
  → `ok` (49.6s). This pair FAILED before the commit and ONLY because the new
  files were untracked: the case git-archives a tree and builds it, so an
  untracked `internal/config/chat_geometry.go` was invisible to that sub-build
  (`undefined: ChatIdealGeometryFlagName`). Committed, both pass — which is the
  finding worth keeping: an untracked new file makes a sub-build of the tree
  disagree with the tree.
- `bash scripts/g5-chat-web-help-surface.sh` → `chat  chat-ideal-geometry,…  13`;
  group total 39.
- `bash scripts/g6-tools-maintenance-help-surface.sh ./bin/gopi | grep
  chat-ideal` → `(top-level flags)  gopi --chat-ideal-geometry  …`.
- Pre-existing reds, measured against master (NOT introduced here):
  `git diff master --name-only -- internal/tools/` → empty;
  `git show master:internal/tools/memory_sync_claim_test.go | sed -n '470p'` →
  `os.Unsetenv(configSyncPeersEnv)`;
  `git show master:cmd/agent/cli_chat.go | grep -n '} else if beatDriver'` →
  line 1898 (this branch's copy is at 1915 — the same code, shifted by this
  branch's own hunks, which is why the Rule 18 gate reports it as "new").
