<!-- doc-class: historical -->
# A capture's default `--out` is a class tree a sweep already walks — never the caller's cwd

**Date:** 2026-11-07 · **Task:** `01a1b3b1` (follow-up to `01a1b337`) · **Rule touched:** 30, 35, 13/14

## The defect, measured

Every capture face in the tree — `gopi tool capture`, `gopi tool screen_capture`,
`gopi tool shot`, and the model's own `screen_capture` tool — defaulted `--out`
to a **bare relative name**, and the write was handed the un-absolutised string
(`filepath.Abs` only fed the reported JSON):

```go
path := out
if path == "" {
    path = "screen-" + fmt.Sprintf("%d", time.Now().Unix()) + ".png"
}
abs, err := filepath.Abs(path)     // ← the JSON got `abs`, os.WriteFile got `path`
```

`os.WriteFile` therefore resolved the default against the **process's cwd** — and
a task run's cwd is **its own clone ground**. Every capture taken without `--out`
left an untracked PNG inside a git work tree, which is PROJECT.md Rule 30's exact
defect: the close-out records `completed-unclean`, and no merge path collects the
file.

Red proof (pre-fix binary at `780f39f6`, cwd = a fresh clone of the repo):

```console
$ git status --porcelain | wc -l
0
$ bin/gopi tool capture                     # no --out
{"path":"…/baseclone/screen-1794016936.png", …}
$ git status --porcelain
?? screen-1794016936.png
```

Green proof (same cwd, fixed binary):

```console
$ bin/gopi tool capture
{"path":"/home/user/workspace/gopi-test-scratch/dumps/gopi-captures/screen-1794016941.dump.png", …}
$ git status --porcelain | wc -l
0
```

## Why NOT the session directory — the obvious answer, refused

`<gopi dir>/sessions/<task-id>/` is off every git tree, so it looks like the fix.
It is not, because **the declared-tree sweep (`DeclaredTrees()`, which
`gopi tool scratch-gc trees` and `scratchGCTick` walk) has no row for a
`<stamp>.png` family there**: its session rows cover `*.raw.log`
(session-raw), the legacy residue shapes and `attachments/`. Moving the debris
into that directory would trade "pollutes a git tree" for "pollutes a directory
nothing collects" — strictly worse, because the second is invisible to every
check.

## Why not "the file never exists" (base64 in the return value)

The model-facing contract is that `screen_capture` answers a **path** and the very
next step is `vision_analyze <path>` (the tool's own `hint` says so, and
`vision_analyze` reads a local file). A capture that never hit the disk could not
be read by the tool it exists for.

## The shape that was built

A **third sibling of the two dump class trees** (`internal/scratch/scratch_captures.go`):

| piece | value |
|---|---|
| class directory | `<scratch home>/dumps/gopi-captures/` (via the existing `ScratchDumpsClassRoot`, never a spelled path) |
| file name | `<prefix><unix>.dump.png` — `screen-` / `shot-` |
| namespace predicate | `IsCaptureDumpName` — the family prefix plus a stamp, so a hand-placed `diagram.png` in that directory is **never** a candidate |
| age window | `CaptureStaleAge = ScratchDumpsStaleAge` (14 days) — the same rule, its own name |
| collector | the **existing** declared-tree sweep. No new sweep, no script, no crontab (Rule 35) |
| writer | `scratch.CaptureDumpFile(prefix)` — ONE resolver, reached by all three faces, returns an ABSOLUTE path |

`.dump.png` is the safety property, not cosmetics: the reclaim predicate must
decide "ours" from the NAME alone, and the directory sits on the operator's own
filesystem where a human may drop a PNG. The exact same reasoning the dump
classes carry with `.dump.json`.

## The four Rule-35 questions, answered

1. **What breaks without it** — every capture leaves an untracked PNG in the run's
   clone; `git status --porcelain` is non-empty at close-out (Rule 30). Measured
   above.
2. **Why it cannot be fixed in-process** — it IS fixed in-process: one default
   resolver, three call sites, zero new processes or files.
3. **Who collects it** — the declared-tree sweep that already ticks; the row is
   `scratch-captures`, visible as
   `tree=scratch-captures present=true entries=N` in `gopi tool scratch-gc trees --dry-run`.
4. **What the next person sees** — that `scratch-gc trees` line, plus the
   rewritten `--out` usage text on all three verbs and the model-facing tool
   description, each naming the new default and the reason.

## Honest limits

- **No count bound.** The only limit is the 14-day window. A "keep at most N
  captures" rule is a different decision and was deliberately not taken.
- **Existing residue is not retro-cleaned.** PNGs a run already dropped in a
  clone stay there; this change stops new ones.
- **A capture in a *pinned* scratch home is collected by that home's sweep**, so a
  test or a sandbox that pins `GOPI_TEST_SCRATCH_HOME` / `GOPI_HOME` keeps its own
  tree and its own window.

## Regression guards (all green, each red on the pre-fix tree)

- `internal/tools/screencapture_default_out_test.go` — no `--out`, `t.Chdir` into
  a real git fixture, the writer runs, then `git status --porcelain` must be empty
  and the path absolute inside `scratch.CaptureDumpRoot()` **and** accepted by
  `IsCaptureDumpName` (a destination no collector accepts is not a fix).
- `cmd/agent/cli_shot_default_out_test.go` — the same for `writeShotPNG`, plus
  "an explicit `--out` is still honoured verbatim".
- `internal/scratch/scratch_captures_test.go` — the namespace refuses
  `diagram.png` / `screen-<unix>.png` / `img.dump.png`; two captures in one second
  do not collide; the REAL declared row collects the aged file, keeps the fresh
  one, keeps the foreign one, and never removes the class directory.
- `internal/scratch/declared_trees_test.go` — the new row is wired into
  `DeclaredTrees()` and both writer call sites are anchored to it, so a face that
  grows its own destination spelling fails the wiring gate.
