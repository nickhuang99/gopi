<!-- doc-class: historical -->
# A text2img render's default output is a class tree a sweep already walks — never beside the source

Task `01a1b3cf-f2e4-7cb8-8ff8-2552b6d6f85e` (host hosta, 2026-11-07). The
sibling defect — the capture family's — is written up in
`devdoc/capture-default-out-is-a-declared-tree.md` (task `01a1b3b1`, commit
`9988901f`); this page records what is DIFFERENT here and does not repeat the
shared reasoning.

## The defect, measured

`internal/text2img/render.go`'s default branch was one line:

```go
if out == "" {
    out = strings.TrimSuffix(src, filepath.Ext(src)) + ".png"
}
```

That is `<src 去扩展名>.png` — **beside the source file**. A task run's cwd is its
own clone ground, and `src` is routinely a file inside it, so every `--out`-less
render left an untracked PNG in a git work tree: PROJECT.md Rule 30's defect,
recorded at close-out as `completed-unclean`, collected by no merge path. Both
non-slash faces reach that one line — `gopi tool text2img` and the model's
`text2img` tool — because both go through `tools.Text2ImgDetail` →
`text2img.RenderFile`.

Measured on the pre-fix binary, from a clean git fixture:

```
$ git status --porcelain          # (empty)
$ gopi tool text2img probe.txt
$ git status --porcelain
?? probe.png
```

## What is different from the capture fix: MULTI-PAGE NAMING

A capture writes ONE file, so the capture class could name it
`<prefix><stamp>.dump.png` and stop there. A text2img render writes **N** files,
named `<root>-<page><ext>` for page ≥ 2 — and the class suffix `.dump.png` is
what makes a name collectable at all. Those two facts collide:

```go
root := strings.TrimSuffix(out, filepath.Ext(out))   // "x.dump"
ext  := filepath.Ext(out)                            // ".png"
// page 2 → "x.dump-2.png"  ← no predicate accepts this, no sweep collects it
```

So the split had to become **suffix-aware**, and it is now ONE function,
`splitOutputPath` (`internal/text2img/render.go`), which splits on the class
suffix by REFERENCE (`scratch.Text2ImgDumpFileSuffix`) rather than re-spelling
`.dump.png` (Rules 13/14 — a second spelling here would let the writer mint a
name the collector refuses):

| render | page 1 | page 2 |
|---|---|---|
| no `out` (default) | `text2img-<stamp>.dump.png` | `text2img-<stamp>-2.dump.png` |
| explicit `--out x.png` | `x.png` | `x-2.png` (unchanged) |

Both faces then produce names `scratch.IsText2ImgDumpName` accepts, which is the
only property that matters: a destination no collector accepts is not a fix, it
is the same leak in a different directory.

### A second naming decision: same-second collisions advance the STAMP

Two renders inside one second must not overwrite each other. The capture class
solves that by appending `-<n>`, which is free there — a capture has no page
number. Here `-<N>` is already the PAGE discriminator, so
`text2img-<stamp>-2.dump.png` would mean both "the second render this second,
page 1" and "the first render, page 2". Both are collectable, so the ambiguity
costs no reclaim — it costs a READER, who cannot tell a collision from a page.
The resolver therefore advances the **stamp** by one second per collision, which
keeps one meaning per name shape.

## WHERE it lands, and WHO collects it

`<scratch home>/dumps/gopi-text2img/` — a declared dump class tree, appended to
`DeclaredTrees()` by `Text2ImgDumpTrees()` (`internal/scratch/scratch_text2img.go`),
collected by the sweep that ALREADY ticks (`gopi tool scratch-gc trees` and
`scratchGCTick`'s resident pass). No new sweep, script or crontab (Rule 35).

It is a SIXTH sibling of the dump and capture classes — same shape (one suffix,
one class directory, `ScratchDumpsStaleAge` = 14 days), same collect path
(`removeCollectableOfTree` through the generic walk). The namespace predicate is
`IsText2ImgDumpName`: `text2img-<digits>` with an OPTIONAL `-<page>` and nothing
else, so a `diagram.png` the operator drops in that directory is never a
candidate.

Verified live (`GOPI_TEST_SCRATCH_HOME` pinned to this run's session dir, cwd = a
real git fixture):

```
$ gopi tool text2img probe.txt
  output: .../dumps/gopi-text2img/text2img-1794019150.dump.png
$ git status --porcelain          # (empty)

$ gopi tool text2img long.txt --lines 1
  output: .../text2img-1794019155-1.dump.png
  output: .../text2img-1794019155-2.dump.png
  output: .../text2img-1794019155-3.dump.png
  output: .../text2img-1794019155-4.dump.png

$ gopi tool scratch-gc trees | grep text2img
tree-summary tree=scratch-text2img examined=5 not_ours=0 collectable=0 kept=5
```

`examined=5 not_ours=0` is the load-bearing reading: five artifacts, all five
recognised as this writer's, none foreign. `not_ours` is the number that would
grow if the writer and the predicate disagreed.

## What this deliberately did NOT change

**The `/text2img` slash command** (`cmd/agent/commands.go`) already filled its own
`out` from `scratch.DumpDir(RuntimeClassText2Img)` and never reached the default
branch. It was correct; it is untouched. The two directories stay distinct: the
slash command stages into the RUNTIME scratch class (`dumps/text2img`), the
default faces land in the DUMP class (`dumps/gopi-text2img`) — one is a staging
area for a command, the other is the destination of a render a caller asked for
by path.

**An explicit `--out`** behaves exactly as before (`x.png` → `x-1.png`, `x-2.png`
…), pinned by `TestExplicitText2ImgOutIsHonouredVerbatim`, which also asserts the
class tree stays EMPTY when an explicit path is given.

## Not done (honest)

- `cmd/agent/cli_text2img_verify.go` calls `text2img.RenderFile(src, out, …)` with
  its own non-empty `out`, so it never reaches the default branch — untouched and
  unexercised by this change.
- The class directory is not created until a first default render happens
  (`scratch-gc trees` reports `present=false` on a host that has never run one);
  that is the same lazy-create contract the other class roots honour.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- Tests: `make test-budget PKGS='./internal/scratch ./internal/text2img
  ./internal/tools ./internal/agent ./cmd/agent'` — see the task report for the
  per-package lines.
- Ran: the three CLI invocations above, verbatim, plus
  `gopi cli --once --tools "用 text2img 工具把当前目录下的 toolside.txt 渲染成 PNG，不要传 out 参数"`,
  whose transcript shows the model calling `text2img {"src":"toolside.txt"}` and
  reporting
  `/home/user/workspace/gopi-test-scratch/dumps/gopi-text2img/text2img-1794019235.dump.png`
  — the model-side face, no `out`, ground unchanged.
