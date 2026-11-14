<!-- doc-class: historical -->
# `gopi capture --list-windows` — the read-only listing (salvage re-implementation)

## Why this document exists

A development branch `feat/capture-list-windows` (tip `1034f0ff`, merge-base with
master `247e9c6c`) carried one feature master does not have: a read-only listing
of the display's top-level windows. A master brief
(`briefings/brief-capture-list-windows-from-master.md`, commit `247e9c6c`)
forbids merging that branch and asks for the genuine delta to be **re-implemented
on master**. This document records what the branch's delta was, which units were
already present on master, which were genuinely absent, and how the absent one
was re-implemented (never replayed).

## The salvage verdict, per delta unit

Instruments, run live (never a diff's prose, never an audit TSV row):

```bash
git diff --raw --no-abbrev 247e9c6c 1034f0ff      # the 11-file delta + blob hashes
git rev-parse master:<path>                       # master's blob at tip a9d67ccb
git grep -n -i 'list-windows' master              # the behaviour test, whole tree
```

| file | branch post-image | master at `a9d67ccb` | verdict |
|---|---|---|---|
| `cmd/agent/cli_capture.go` | `af6bf33a…` | `3fe90be7…` (= base) | **GAP** — the `--list-windows` mode dispatch |
| `cmd/agent/cli_capture_test.go` | `7c36b7c6…` | `b37597a4…` (= base) | **GAP** — its tests |
| `devdoc/DOC-INDEX.tsv` | `6da372a3…` | `6c992237…` | append-only ledger — not a unit |
| `devdoc/screen-capture-window-list.md` | `675dcd30…` | ABSENT | **GAP** (documentation) |
| `internal/shot/cov_shot_x11_test.go` | `05dd7a88…` | `02ebc1eb…` (= base) | **GAP** — fake-server fixture for the listing |
| `internal/shot/pidwindow.go` | `776cc3df…` | `acef8523…` (drifted) | PARTIAL — see below |
| `internal/shot/x11list.go` | `865bb2c4…` | ABSENT | **GAP** — the engine |
| `internal/shot/x11list_test.go` | `981c9479…` | ABSENT | **GAP** — its tests |
| `internal/shot/x11rect.go` | `2fb4edc6…` | `d254523c…` (= base) | PARTIAL — shared id-spelling + geometry accessor |
| `internal/tools/screencapture.go` | `89f2b8ab…` | `5f0a73a3…` (= base) | **GAP** — the tools-layer payload |
| `internal/tools/screencapture_seam_test.go` | `52df5476…` | `f77dfcad…` (= base) | **GAP** — its seam test |

**The decisive observation** is that master's blob for six of the eleven files is
*byte-identical to the merge-base pre-image* (`3fe90be7`, `b37597a4`,
`02ebc1eb`, `d254523c`, `5f0a73a3`, `f77dfcad`). Master therefore did **not**
absorb those files' branch versions under another name — the branch's delta is
mechanically absent from them. The name test was run anyway, as a content test:

```bash
git grep -n -i 'list-windows' master     # 0 hits outside the brief itself
git grep -c 'listWindows\|ownWindow' master -- cmd internal   # no files
git ls-tree -r --name-only master internal/shot/ | grep x11list   # nothing
```

The two files that DO differ from the base — `internal/shot/pidwindow.go`
(`acef8523`) and `internal/shot/x11list.go` (absent) — carry the listing's
supporting primitives. Per symbol, at master tip:

| symbol the branch's listing needs | at master `a9d67ccb` |
|---|---|
| `queryTreeChildren`, `queryTree`, `translateToRoot`, `windowProperty`, `internAtom`, `inputFocus`, `topLevelWindow`, `pointerWindow`, `windowRect`, `windowSize`, `netWMNameName`, `netActiveWindowName`, `x11RouteNetActiveWindow` / `…InputFocus` / `…Pointer` | present |
| `windowGeometry`, `windowIDString`, `windowTitle`, `windowText`, `windowPIDForAtom`, `netWMNamePropertyName`, `netWMCheckName`, `x11WindowIDFormat`, `x11FormatBits8` | **absent** |

That table is the whole gap: the *identity* vocabulary (`windowIDString` /
`x11WindowIDFormat`), the *title* reader, the atom-once PID read, the geometry
accessor named for its parent-relative origin, and the listing itself.

## What was re-implemented on master (never replayed)

Written fresh against current master, taking the branch's design as the spec and
its comments as the record of what was measured. Four files changed:

- **`internal/shot/x11list.go`** (new) — `ListWindows`, the `ListWindowsProvider`
  seam, `XWindowInfo` / `XWindowList`, `activeWindowBestEffort`, `ewmhManager`,
  `windowTitle` / `windowText`, `windowScreenRect`, `internAtomBestEffort`,
  `sortWindowInfos`.
- **`internal/shot/x11rect.go`** — `x11WindowIDFormat` + `windowIDString` (ONE
  spelling of a window id), `x11FormatBits8`, `windowGeometry` (+ `x11WindowGeometry`
  naming the parent-relative origin), with `windowSize` delegating to it.
- **`internal/shot/pidwindow.go`** — `windowPIDForAtom` (atom resolved once per
  listing), and the two id-spelling call sites now go through `windowIDString`.
- **`internal/tools/screencapture.go`** — `ScreenCaptureListWindows` + the
  `screenCaptureWindowList` payload + its hint.
- **`cmd/agent/cli_capture.go`** — the `--list-windows` flag, its exclusivity
  with every capturing mode, the `--out` / `--decorations` refusals, and the
  dispatch *before* the capture engine so the read-only mode cannot inherit a
  write path.

## The deviation from the branch, and why (a live run found it)

The branch's `windowScreenRect` measured the window's **content** origin
(TranslateCoordinates of the window itself) while reporting the **outer** corner
of a bordered window is what `xwininfo` prints. Measured 2026-10-01 on a bare
Xvfb with an xterm (border 1) at `-geometry +37+53`:

```
# first cut (windowRect, the capture path's own accessor)
0x20000c  38 54 484x316      ← one pixel off in both axes
# after the correction (parent origin + parent-relative GetGeometry)
0x20000c  37 53 484x316      ← matches
# xwininfo -root -children (ground truth)
0x20000c ("xterm" "XTerm")  484x316+37+53  +37+53
```

So `windowScreenRect` builds the rectangle from the **parent's** root origin plus
GetGeometry's parent-relative x/y — the same correction `windowRect`'s own
comment records for the capture path, applied to the opposite convention. The
listing's rectangle is what a caller pastes into `--area`, so a one-pixel error
there is a wrong capture, not a cosmetic one. The unit fixture models this
explicitly (border 1, parent-relative GetGeometry, content-origin translates), so
the convention is pinned rather than assumed.

## The read-only property

Asserted from the server side, not promised in a comment:
`TestListWindowsIssuesOnlyReadRequests` classifies **every** opcode in a whole
listing's recorded request stream against the seven read verbs
(InternAtom / QueryTree / GetGeometry / TranslateCoordinates / GetProperty /
GetInputFocus / QueryPointer) and fails on any other — and requires the
enumeration opcodes to actually appear, so a listing that sent nothing cannot
pass for the wrong reason. `TestListWindowsDialsOnce` pins one connection (the
windows and the focus are read from the same moment), which no single request's
bytes can show.

## Skipped honestly

- Nothing was skipped: `go build ./...`, `go vet`, and the branch-scoped tests
  all ran, and the surface was executed against a real Xvfb display.
- `act move --own` / keysym typing from the same source branch is a different
  feature and was **not** ported (`xactuate.go` untouched), as the brief directs.
- No change was made to `--screen`, `--window`, `--self`, `--area` or
  `--decorations`; their meanings and their existing conflict error are intact.
