---
title: GOPI-UI_STATE
section: 1
header: GoPi Manual
---

# NAME

gopi-ui_state - read a live screen state as an interpreted cell grid

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
  gopi ui_state [--pid N] [--target ui|chat] [--json] [--list]
  gopi ui_state --compare "<line>" [--session <id|path>] [--target ui|chat] [--json]
  gopi ui_state --all [--session <id|path>] [--target ui|chat] [--last N] [--json]
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi ui_state** reads a live surface's own interpreted screen state — text plus
a `.json` twin — one call, zero pixels, zero vision. The verb **finds** the dump,
**proves whose it is**, and prints the state, or refuses.

What it is: an interpreted **cell grid**, not bytes and not pixels. The escape
sequences were already interpreted by the terminal emulator, so this describes
the CELLS of the screen. What it is not: a pixel grab, a byte stream (sequence
order and timing are gone), and not a font rendering — the blind spots are
printed with every answer.

The screen is an **evidence channel**, which is the reason the verb exists: it
can DISAGREE with the transcript, and the interesting question is which side lost
it. `--compare` answers exactly that for one line; `--all` answers it for the
whole answer.

# OPTIONS

`--pid N`
: read the dump written by that process. The unambiguous route: a caller that
knows which surface it means gets exactly that screen.

`--target S`
: which self-observing surface to read — `ui` (the BubbleTea TUI, whose dump is
`ui-screen.dump`) or `chat` (the line editor, `chat-screen.dump`). Both surfaces
write into the same session tree, so without it a chat running beside a ui is a
FALSE ambiguity.

`--json`
: print the machine-checkable rendering: the dump JSON, the identity verdict,
and the blind-spot list.

`--list`
: print every dump found, with each one's identity verdict. Never refuses — this
is the surface that resolves an ambiguity.

`--all`
: audit the whole answer, one line of shell. The session flag is optional here:
the authority is DERIVED from the dump itself, so the command alone runs. Every
line of the session's text is checked against the armed dump's grid as one unit,
and the ledger says how many lines matched and how many disagreed.

`--last N`
: narrow the whole-answer audit to the answer's last N lines. The window is
stated on the ledger: an audit of the last N lines never claims to have covered
the whole answer.

`--compare TEXT`
: the one-call check — is this line of the model's output on the painted grid,
AND in the authoritative transcript? The authority is derived from the dump
itself, so the flag plus the line alone runs. Several lines may be separated by
newlines.

`--session ID|PATH`
: optional override of the authority the check reads — a session id, or a path
to a `session.jsonl`. Omitted, the transcript is derived from the dump. It is a
separate flag rather than an argument of `--compare` so the default case needs
no ceremony.

# VERDICTS

`live`
: the dump's writer is still that very process (pid plus start ticks).

`exited`
: the writer is gone — the dump is its LAST frame, and that is said out loud.

`foreign`
: the pid is alive but is NOT the writer (a recycled pid).

`unreadable`
: the file is not a dump this build can parse.

The damage it names when a line disagrees: `paint_loss` (in the log, fits the
grid, not on it — the band never drew it), `wrap_artifact_or_loss` (in the log,
WIDER than the grid — a correct wrap explains it as well as a loss),
`cjk_wrap` (wider in CELLS than in RUNES — the layout's double-width arithmetic
is the candidate explanation), and `emphasis_unpainted` (the text arrived with
emphasis marked while the grid's cells carry the row's plain style).

# EXIT STATUS

`0`
: the read succeeded — and, for `--compare` / `--all`, the check RAN and the two
sides AGREED.

`1`
: no dump, a bad flag, or a check that COULD NOT RUN (no dump armed, nothing
painted, a blank grid, a fallback geometry) — or that had no authoritative side
to check against.

`2`
: several live surfaces — the refusal lists them, and `--pid` disambiguates.

`3`
: for `--compare` / `--all`, at least one line disagreed — a line the two sides
contradict each other about.

`4`
: for `--compare` / `--all`, at least one line was **outside the observed
window**: the dump's own saved history (`screen.scrollback`) holds it and the
visible grid does not. The surface painted it and the terminal has since pushed
it off the top of the window this artifact records, so nothing was lost — but
nothing about the *current* frame was verified either. It is **undecidable for
the visible grid, not a disagreement**, which is why it is a code of its own
rather than exit 3.

**Every finding names the side it is attributed to as the FIRST WORD of its
reason**, so a caller reads the attribution without parsing prose:

`transcript-side`
: the authoritative text has the line and the grid does not — a paint loss or a
wrap the reassembly could not explain.

`grid-side`
: the grid carries the line and the transcript does not — the artifact is ahead
of its log, so the two were captured at different moments rather than damaged.

`neither-side`
: neither the transcript nor any layer of the grid (visible window or saved
history) has it.

`off-grid`
: the saved history has it and the visible grid does not — undecidable, exit 4.

**An unchecked screen is never reported as a verified one.** Exit 0 means the
check ran and agreed; exit 1 means it could not run. Branch on 0 versus
non-zero, which is exactly why the two live on different codes. Exit 4 is
likewise never read as agreement, and never as a loss.

**An all-blank grid is qualified, not answered.** `unpainted` is `frames=0` (no
frame was ever interpreted); `blank` is `frames>0` with every cell blank, which
is the chat surface's ordinary shape while idle. A `--compare` against such a
grid is REFUSED, never answered "not in the grid" — the grid has no content to
disagree with.

# SEE ALSO

`gopi ui_state -h`, `gopi -h`, `man gopi`, `man gopi-chat` (the surface whose
dump this verb reads).
