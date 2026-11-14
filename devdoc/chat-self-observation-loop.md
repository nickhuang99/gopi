<!-- doc-class: historical -->
# The chat self-observation loop: armed, and finally RUN

Task `01a0dd28-c27b-7521-a6a9-4c6837500356` (epic: *"the tool exists, the loop was
never run"*). Every piece shipped before this task; what had never happened is a
run in which the dump was **armed before** the display state worth reading
existed.

## 1. The loop, run for real (117x26 pty, kernel-held size)

Armed runs with `stderr` ON the pty (a run whose stderr is redirected records
`terminal=0x0`), each answering through a local SSE stub so the answer line is
known and the run costs nothing:

```
record: #gopi-chat-dump pid=3809327 target=self:chat tools=off terminal=117x26 \
        model=117x26 grid=117x26 geometry=layout mode=ansi frames=112 dumps=6

$ gopi ui_state --compare "<the answer line>" --session <session.jsonl> --target chat
#gopi-ui-state-compare surface=chat state=live dump=... session=... transcript_lines=1 match=false
  mismatch grid_row=-1 in_grid=false in_transcript=true geometry=layout cols=117 rows=26 \
  frames=112 prefix=32 reason="the transcript has this line but the grid does not (longest
  verbatim prefix on the grid: 32 runes): the paint lost it, re-wrapped it, or it has
  scrolled out of the visible grid"
[exit=3]
```

**The loop RUNS**: the artifact is discovered with no `--pid` and no path, its
identity is proved (`state=live`), the geometry provenance is `layout` (the LAYOUT
measured it), and the verdict carries a DIRECTION
(`in_transcript=true in_grid=false`) instead of a bare "wrong".

## 2. The obstruction, named from the measurement

The frame that produced the verdict HELD the painted row, truncated at exactly the
verdict's own number — the 32-rune boundary:

```
023|▌ 自观测闭环：中文与 latin 混排的一行，dump 必须逐字符
```

34 runes = the `▌ ` gutter (2 cells) + **32 runes of text**, against a 35-rune line
(`自观测闭环：中文与 latin 混排的一行，dump 必须逐字符还原。`). `frames=112` counts
112 **render writes**; `dumps=6` counts 6 **file-refresh boundaries**
(`dumpMinInterval` = 100 ms, `internal/uiclient/dump.go`). The artifact is a sample
of a 100 ms-throttled stream.

**Where the measurement landed**: when the wait changed from a 12-rune needle to the
FULL line and the artifact was snapshotted the moment it appeared, the verdict for
the SAME input moved from `prefix=32` (delta in flight, row present) to `prefix=0`
(settled frame, no answer row in any of the 26 rows) — three runs agree
(`red`, `green`, `settled`: `frames=151/152/156`, all `prefix=0`).

**Alternatives ruled out, each with its instrument:**

| alternative | measurement that removes it |
|---|---|
| **not armed** | the artifact exists, is discovered with no `--pid`; its record names this pid/target (`state=live`); the check ANSWERED instead of exiting 1 |
| **wrong geometry (refused)** | `geometry=layout`, `terminal=117x26 model=117x26 grid=117x26` — the layout's own install rewrote the source. A fallback grid REFUSES (exit 1, `refused`), a different word and code |
| **sample garbage / unpainted** | `frames=112 > 0`; an unpainted grid refuses (`UnpaintedNotice`); 26 rows were read (prompt row, status row, `[gopi] chat table` row) |
| **discovery-path bug** | `gopi ui_state --target chat` on the same profile exits 0 and prints the screen; `--list` prints it; an unarmed profile exits 1, "no dump found in any scanned location" |
| **wrong line given to the check** | the searched line is read from `session.jsonl` itself (the authority), not typed: `transcript_lines=1`, `in_transcript=true` |

## 3. The cost, measured (why the dump is opt-in)

- artifact: **5 787 bytes** for a 117x26 grid (5 853 in the shape whose record
  carries `geometry=fallback`);
- writes: one atomic temp+rename **per 100 ms throttle boundary** on the render
  path — 6 boundaries over ~10 s, 11 over a 2-minute run;
- the no-dump path is **byte-identical by construction**: the editor's writer is
  returned UNCHANGED when the sink is nil.

So the flag stays OPT-IN, and the help text says so with these numbers.

## 4. The remedy (arming discoverability / help-text contract)

The obstruction behind "*the loop was never run*" is that nothing said **arm the
dump first** before the run whose screen no longer exists to be read.

**Where it went, and why (measured).** The top-level page is an INDEX BY CONTRACT:
`gopi -h` is capped at 160 lines just as I was adding this, and the baseline
renders **159 of those 160** — so the loop could not live there without breaking a
budget another epic established. The ladder is therefore:

| page | what it carries |
|---|---|
| `gopi -h` | that `gopi chat` and `gopi ui_state` EXIST, plus "each accepts -h for its full help" — the rung that makes the verb pages reachable |
| `gopi chat -h` | the ARMING flag, its cost, and the three-command loop below |
| `gopi ui_state -h` | the check, the verdict vocabulary, and THE EXIT CONTRACT (0 ran-and-agreed / 1 could-not-run / 3 ran-and-disagreed) |

The loop, verbatim from `gopi chat -h`:

```
gopi chat --dump-screen <session-id>      # 1. ARM this run
gopi ui_state --target chat               # 2. is a dump on hand?
gopi ui_state --compare "<a line>" --session <session-id> --target chat   # 3. the check
```

**Exit-code contract (deliberate):** `0` = the check RAN and both sides have the
line; `3` = it ran and they DISAGREE (direction in `in_grid`/`in_transcript`);
`1` = it **could not run** (no dump armed, nothing painted, fallback geometry). An
unchecked screen is not a verified one, so "could not run" is never rendered as
agreement, and it lives on a different code from both other outcomes.

## 5. The RED proof

`cmd/agent/cli_uistate_compare_arming_test.go` runs the loop's three steps against
ONE input and flips only the arming:

- GREEN: armed artifact discovered by the verb → exit 0, verdict `match`;
- RED: the artifact removed → the SAME call exits **1** and never claims `match`.

Three further cases pin the discoverability contract (`gopi -h` names the two
verbs and the "-h for its full help" pointer; `gopi chat -h` carries the arming flag;
`gopi ui_state -h` carries `--compare` AND names `--dump-screen`, since the check is
unreadable without a dump). Their RED direction is reverting the help text.
