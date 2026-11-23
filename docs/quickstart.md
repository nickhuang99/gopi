<!-- doc-class: normative -->

# The four frames at the top of the README — and how to re-render them

`docs/quickstart.gif` is not a mock-up. Each of its four frames is a **real command's
real output**, rendered to a PNG by `gopi tool text2img` and assembled into one animated
GIF by `gopi tool gif`. Both of those verbs are this binary, and the whole chain uses the
Go standard library: **no ffmpeg, no ImageMagick, no gifsicle, no new module.**

That is the point of showing it this way: the tool that makes the picture is the tool the
picture is about.

## What each frame says

| Frame | The claim it shows |
|---|---|
| 1 | `gopi --version` — one binary, built with `CGO_ENABLED=0`, reporting its own Go version and revision |
| 2 | `gopi task list` / `gopi task show` — the ledger, append-only, with the row a dispatcher takes next marked `[next]` |
| 3 | `gopi task submit` — work becomes a ROW with an id and a state machine, not a stretch of conversation |
| 4 | `gopi worker <id>` — a worker takes the row into its own clone, builds, and pushes `master` |

## Re-rendering it

The recipe is deliberately boring, and it is the one used for the committed file. From the
repository root:

```bash
# 1. write each frame's TEXT (the command, and its output, verbatim)
mkdir -p /tmp/gifframes
cat > /tmp/gifframes/f1.txt <<'EOF'
$ ./bin/gopi --version
  v0.5.0
  go: go1.24.0
  platform: linux/amd64
  revision: 75d262c2bc75

$ ./bin/gopi task submit "fix the parser"
t-01a1db5b  pending  priority 5
EOF

# ... f2.txt, f3.txt, f4.txt the same way

# 2. pad every frame's text to the SAME line count and column count, so all
#    four PNGs come out the same size — `gopi tool gif` refuses mismatched
#    frames rather than cropping them
python3 - <<'PY'
mx = 0
data = {}
for n in ["f1", "f2", "f3", "f4"]:
    ls = open(n + ".txt", encoding="utf-8").read().rstrip("\n").split("\n")
    data[n] = ls
    mx = max([mx] + [len(l) for l in ls])
for n, ls in data.items():
    ls = [l.ljust(mx) for l in ls]
    while len(ls) < 18:
        ls.append(" " * mx)
    open(n + ".pad.txt", "w", encoding="utf-8").write("\n".join(ls[:18]) + "\n")
PY

# 3. text -> PNG, one per frame. --font a MONOSPACE face is not optional:
#    the default is PROPORTIONAL, so equal character count does NOT mean equal
#    pixel width, and step 2's padding would produce four different widths.
for f in f1 f2 f3 f4; do
  ./bin/gopi tool text2img /tmp/gifframes/$f.pad.txt /tmp/gifframes/$f.png \
      --size 14 --cols 200 --lh 22 \
      --font /usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf
done

# 4. PNGs -> one animated GIF. The caption band is drawn BELOW each frame,
#    never over its pixels, so the caption cannot cover what it explains.
./bin/gopi tool gif \
  --frames /tmp/gifframes/f1.png,/tmp/gifframes/f2.png,/tmp/gifframes/f3.png,/tmp/gifframes/f4.png \
  --out docs/quickstart.gif --delay 250 \
  --caption "/tmp/gifframes/f1.png=1/4  build it, run it" \
  --caption "/tmp/gifframes/f2.png=2/4  a task ledger, not a chat log" \
  --caption "/tmp/gifframes/f3.png=3/4  file the work" \
  --caption "/tmp/gifframes/f4.png=4/4  a worker does it in its own clone"
```

## Two things that will bite you

**The frames must be the same size.** A GIF has one logical screen; `gopi tool gif`
refuses a mismatched frame instead of cropping it. That is why step 2 pads the *text* to a
uniform shape before rendering — it is cheaper than padding pixels afterwards.

**Padding the text is only half of it — the FONT decides the pixels.** `text2img`'s
default face is **proportional**, so two frames padded to the same character count
render at *different* pixel widths and `gopi tool gif` then refuses them. Measured on
ten characters, same `--size 13`:

```
default (proportional):   iiiiiiiiii ->  56 px    MMMMMMMMMM -> 131 px
--font DejaVuSansMono:    iiiiiiiiii -> 103 px    MMMMMMMMMM -> 103 px
```

That is why step 3 passes `--font` with a monospace face: with it, equal column count
*is* equal pixel width. (A monospace face with no CJK glyphs still renders Chinese —
`text2img` falls back through a font chain and reports it, e.g.
`DejaVuSansMono.ttf → wqy-zenhei.ttc → …` — and the fallback face keeps the same
advance width, so mixed 中英 frames still line up. Verify with the `od` check below
rather than trusting it.)

**Width has a ceiling for chat apps.** A GIF wider than **500 px** will not play in a phone
conversation view. `gopi tool gif` still writes the file and **warns** with the measured
width rather than silently resizing your picture — check it yourself with
`od -j6 -N4 docs/quickstart.gif` (the logical screen descriptor is little-endian, 2 bytes
of width then 2 of height). The committed file is 464 px wide, inside that ceiling.

## Verify a GIF you did not make

```bash
$ od -j6 -N4 docs/quickstart.gif
0000006 000720 000754
```

`000720` and `000754` are octal for 464 and 492 — the width and height a reader's player
will use. A file whose header disagrees with what a tool reports is a file assembled by
something that did not understand the format.
