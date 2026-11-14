<!-- doc-class: historical -->
# Which stream the chat layout is painted on — and why a filmer needs the answer

**Class**: design decision + measured defect. **Task**:
`01a0ff182b4b7c6d9bbc5761fe826c37`. **Rule**: 1, 14, 31.

## The defect, measured

`scripts/replay/replay.sh` films a real terminal: it launches the product inside a
private pty and photographs the window, one frame per drawn beat. Its launcher
redirects the child's **stderr** to a log —
`2>'$SCRATCH/replay-stderr.log'` — deliberately, so a diagnostic line can never be
drawn onto the screen the frames photograph.

The chat surface painted its **entire layout** on fd 2: the banner, the reserved
layout, the turn headers, the answers, the goodbye. So the pty carried only the
two startup lines that went to fd 1, and the layout sat in the log:

```
$ cat <scratch>/replay-stderr.log
gopi chat: screen dump → …
[?2004h[?u[1;24r[1;1H[2Jgopi chat — Ctrl+C interrupts…
╭── gopi v0.1.0 ──╮   … the whole Tips panel …
```

Measured on the same tree and the same session:

| | beats | distinct frames | GIF frames |
|---|---|---|---|
| pre-fix | 16 | **1** (15 dropped) | 1 (6.6 KiB) |
| after | 16 | **12** (4 dropped) | 12 (582 KiB) |

`verify.py` said it in its own words — `the film has 1 frames (floor 12): a slide
show, not an animation` and `the film has 0 saturated bright pixel(s) … this is a
grey-scale film of a colour product`.

## The decision

`cmd/agent/chat_stream_split.go` (the rule, one file):

- **fd 1 = the LAYOUT stream** — what a reader is meant to see, and what a filmer
  photographs.
- **fd 2 = the DIAGNOSTIC stream** — errors, refusals, degradations, the
  `[gopi] …` plumbing notices: what a reader must be TOLD.

The criterion is *not* "fd 1", it is: **a stream may function as the layout stream
only if every descriptor a terminal owns is the same destination.** That is what
makes the change safe as well as correct —

- where stdout and stderr are the same pty (a human's terminal, every pty harness
  in `testsuite/`), moving the layout's bytes changes **nothing observable**:
  same destination, same order;
- where they differ (a launcher's `2>log`), fd 1 is the only defensible answer,
  because it is what a caller photographs by default and because fd 2 is the
  descriptor a filmer must be able to file without losing the film.

The fallback is explicit and is **not a second policy**: fd 1 when it is a
terminal, else fd 2 — exactly where the layout went before, so `gopi chat >file`
and `gopi chat 2>/dev/null` keep the bytes they always produced. Piping stdout is
unaffected: the non-interactive flow path writes to fd 1 by construction.

## The trap this closed

The wrong choice was not obviously wrong, which is why it survived: **when stdout
and stderr are the same terminal — the ordinary human case — the fd number is
invisible.** It only becomes a decision when something points the two descriptors
at different destinations, and that is exactly the case a filmer creates. A rule
that cannot be seen to be violated from the common case needs its own statement,
its own resolver and its own test; that is what this file is.

## The regression test

`testsuite/chat_layout_stream_pty_test.go` runs the REAL binary on a REAL pty
whose stderr is a FILE — the launcher's shape, which is the only thing the defect
needed — and asserts both halves:

1. the layout marker **reaches the pty** (the descriptor a photographer reads);
2. the marker is **absent from the log** (the descriptor a launcher must be free
   to file) — the half that stops the convention being satisfied by writing to
   both streams.

## What this devdoc does NOT claim

`verify.py` still reports three FAILs on the acceptance re-run, none of them this
task's defect and all named in the task report: the `frames/` sample-rule clash
(leg 3), the redaction log not being armed (leg 4), and `src_line 0` in the beat
ledger (leg 1, pre-existing on `master`).
