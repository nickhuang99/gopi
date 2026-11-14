<!-- doc-class: historical -->
# devdoc: screen_capture — closing the def↔dispatcher edge

**Branch**: `fix/screen-capture-dispatch` (worktree `~/workspace/gopi-screencapture-dispatch`)

## The defect (measured, at master 812f9a60)

`screen_capture` was advertised to the model — a `claudeToolDefs` entry
(`internal/agent/loop.go:1374`) and a line in GOPI.md Rule 3 — while
`executeToolDefault`'s `switch tc.Name` had no case for it. Every call fell to
the `default:` arm and returned:

```
Unknown tool: screen_capture with args {"out":"…"}
```

30 such strings across 9 session logs (the two in
`~/.gopi/sessions/01a0b445-1146-72e5-bd64-9be81756af9b/…jsonl` are from the
session that opened this epic). 16 dispatched arms vs 17 defs.

**Why nothing caught it**: the existing gate `TestGOPIRule3_MatchesClaudeToolDefs`
pins *GOPI.md ↔ claudeToolDefs*. The second edge — *defs ↔ dispatcher* — was
gated by nothing. A tool could be advertised and unusable indefinitely.

## The fix

### 1. The dispatch arm (`internal/agent/loop.go`)

```go
case toolnames.ToolScreenCapture:
    display, _ := args["display"].(string)
    out, _ := args["out"].(string)
    result, err := screenCaptureEngine(display, out)
    if err != nil {
        return fmt.Sprintf("%s: %v", screenCaptureErrorPrefix, err)
    }
    return result
```

- **The engine is the CLI's engine.** `screenCaptureEngine` is a seam *var* whose
  production binding is `tools.ScreenCapture` — the exact call
  `cmd/agent/cli_screencapture.go` makes. Two faces (`gopi screen_capture` and
  the model tool), one engine, so they cannot drift. The seam exists so the
  dispatcher's plumbing and error contract are testable without an X server;
  `TestScreenCaptureDispatch_EngineIsTheCLISharedOne` compares the binding's
  function pointer to `tools.ScreenCapture`, so a future re-point fails the
  build.
- **Rule 1b/13**: the tool name and the error prefix are constants
  (`toolnames.ToolScreenCapture`, `screenCaptureErrorPrefix`). The prefix is a
  constant because **two** sites must agree on it: the arm emits it, and
  `toolErrorMarkers` lists it (otherwise a failed capture would not be seen as a
  dead end — Rule 15).
- **No new tool** (Rule 3): this wires an *existing advertised* tool. The defs
  list is unchanged.

### 2. Side tables — what was deliberately NOT wired, and why

| table | decision | reason |
|---|---|---|
| `validateToolArgs` | **no arm** | both args are optional by schema (`display` → `$DISPLAY`, `out` → `screen-<unix>.png`). An arm demanding one would make the tool uncallable with its defaults. `TestScreenCaptureDispatch_BothArgsOptional` pins this. |
| `parallelSafeTools` | **not added** | a capture is not read-only of *our* state: with no `out`, two captures in one round write the same default filename in cwd. Serial keeps them honest. |
| `pollingTools` | **not added** | a capture is instantaneous; it has no external wait semantics. |
| `contentReturningTools` | **not added** | the result is a status report (path/dims/hash), not retrieved prose — the generic dead-end markers are the right signal, and the tool's own error prefix is registered. |
| `toolErrorMarkers` | **added** | see above (Rule 15: a failed capture must be visible as a dead end). |

### 3. The new gate (`internal/agent/tool_dispatch_coverage_test.go`)

`TestGate_ToolDispatchCoversEveryDef` scans `loop.go` with `go/parser` and
requires, in **both** directions:

- **defs → dispatcher**: every `claudeToolDefs[i].Name` has a case label. This
  is the edge the defect lived on. The failure message names the consequence
  ("every call returns \"Unknown tool: <name>\"") so the next reader does not
  have to re-derive why the edge matters.
- **dispatcher → defs**: every case label is an advertised name — minus the
  merged families' **action verbs** (`task`/`job`/`memory` dispatch on their
  `action` argument with nested `case toolnames.ToolTaskCommit:`-style labels
  that are case labels but not tool names). Those are subtracted from a set
  resolved out of `internal/toolnames`.
- **A bare-string case label is an error** (Rule 1b/13), regardless of coverage.

Four deliberate anti-vacuity anchors, because a gate that reads green-blind is
worse than no gate:

1. the parser must find a `switch tc.Name` at all (else: **fail** with "the
   dispatch switch moved or was renamed — update the anchor, do not delete the
   gate");
2. a **read-floor** (`dispatchCoverageReadFloor = 15`) on the number of
   dispatched names;
3. presence checks on three known arms (`read`/`bash`/`task`);
4. the action-verb subtraction set must be **non-empty**.

`TestGate_ToolDispatchScanIsNotVacuous` is the extractor's own self-test: a
synthetic switch carrying a nested action switch (bare-string label), a bare
string at the tool level, and a `default:` clause — the scan must see exactly
the three tool-level labels, with the right kinds, and must report `found=false`
for a file with no dispatch switch.

The switch scan is structural rather than textual on purpose: a grep for
`case toolnames.Tool` would also collect the nested action switches, and a
line-based scan breaks on multi-label cases (`case toolnames.ToolTaskView,
toolnames.ToolTaskCommit:`) and on arms spanning lines.

## Rule 11 evidence — the real turn

Built the branch binary and ran a live turn (Xvfb `:98`, 1024x768x24):

```
cd ~/workspace/_tmp/sc-dispatch-turn
DISPLAY=:98 ~/workspace/gopi-screencapture-dispatch/bin/gopi \
  "Use the screen_capture tool right now to capture the live X11 desktop to
   the file …/eye.png, and report back the path, the pixel dimensions and the
   sha256_16 from the tool result. Do not use bash for this."
```

Session `7371ec76-4af3-41ec-bd08-35210c274a74` (`~/.gopi/sessions/…/session.jsonl`):

- **line 7 — tool_use**: `{"name":"screen_capture","input":{"out":"…/eye.png"},
  "id":"call_01_t8BVxO1HNzlHXNHpKzI25393"}`
- **line 11 — tool_result** for that id:
  `{"path":"…/eye.png","display":":98","width":1024,"height":768,"depth":24,
    "bands":6,"bytes":3924,"sha256_16":"dc95dfdcd7f63eaf","hint":"…"}`

No `Unknown tool` string anywhere in the session. The PNG is 3924 bytes on
disk; the agent independently re-read IHDR bytes 16:24 with python3
(`struct`/`hashlib`) and got 1024×768 and the same sha256_16 — the tool result
is truthful, not stale (Rule 10.2's layered truth check).

A second turn after the final edit, session `4f1dcedf-1787-4ad6-8f0a-b849b9f6a4c9`
(prompt: "Call the screen_capture tool exactly once … No bash."):

- **line 17 — tool_use** `call_00_hiMgEyIwHVcSmlfBq1C34473`
  `{"display":":98","out":"…/eye2.png"}` → **line 19 — tool_result** with
  `width 1024, height 768, depth 24, bands 6, bytes 3924, sha256_16 dc95dfdcd7f63eaf`.
- The agent called it a second time unprompted (line 42/45, same ground truth).
- The only two `Unknown tool` strings in that file are (a) prose inside a
  retrieved **memory record** and (b) the model's own thinking quoting it —
  **zero** tool results carry the string. `grep -c "Unknown tool"` is therefore
  not a usable check on its own; the check is that no `tool_result` body
  contains it.

## A trap the fix walked into: loop.go line anchors are gated

The first placement put `screenCaptureErrorPrefix` / `screenCaptureEngine`
right below `toolErrorMarkers` (line ~410), which is where they read most
naturally. That turned `TestHippocampusDocAnchorsResolve` **red**:
`devdoc/hippocampus.md` and `devdoc/agent-five-organs-ontology.md` carry
`loop.go:NNN` anchors into this file, and 7 of them landed on blank lines once
the insertion shifted the file. (Blank-line failure only — 7 of the anchors,
not all 271.)

The declarations therefore live at the **tail** of `loop.go`, past the highest
anchored line, where an insertion cannot rot a doc — and
`TestScreenCaptureDeclarationsStayBelowDocAnchors` fails if a future agent
tidies them back up to the top. That guard re-derives the same anchor set the
doc gate does (explicit + bare-continuation form), with a read-floor.

## Pre-fix red proof

```
git stash push -- internal/agent/loop.go
go test ./internal/agent/ -run TestGate_ToolDispatchCoversEveryDef
--- FAIL: TestGate_ToolDispatchCoversEveryDef (0.02s)
    claudeToolDefs advertises "screen_capture" but executeToolDefault's `switch tc.Name`
    has no case for it — every call returns "Unknown tool: screen_capture"
git stash pop
```
