<!-- doc-class: historical -->
# The variable tty read boundary, measured on the real chat sink

Task `01a0e851-bbf0-7b56-9922-1313da2e677e`. The predecessor task `01a0e825`
reconciled session 884eca28's real pty turn at cell level and reported
`lost=0 extra=0` — but its reader fed the sink in uniform 7-byte reads. This task
asks whether that model hid a boundary effect, on the SAME real frame (scrollback
row 0826, `produced=52 reading=16 matched=14 LOST=38 EXTRA=2`).

**Verdict, in one line:** the read boundary does not damage cells by being
variable, and `e06a2aed` does not change the whole-turn outcome (all legs
`lost=0 extra=0` on both trees); but a boundary CAN be made to damage cells by
LONGER MECHANISM — a held sequence's introducer dropped while the interpreter
waits for its parameters — and `e06a2aed` does eliminate exactly that, measured
red→green on the real sink. Whether the live frame's 38 cells went that way is
**not decidable from the frame**; what is now decidable is the class of mechanism
it can and cannot be.

## 1. The real read call and its buffer (deliverable 1)

The chat path's input reader is a **poll + read(2) into a fixed 4096-byte buffer**,
not 7 bytes:

| what | where |
|---|---|
| `readBufBytes = 4096` — "readBufBytes bounds one read(2)" | `internal/lineedit/raw.go:75` |
| the single read call: `got, err := t.in.Read(buf)` | `internal/lineedit/raw.go:459` |
| the caller's buffer: `buf := make([]byte, editReadBuf)`, `editReadBuf = readBufBytes` | `internal/lineedit/editor.go:1221`, `internal/lineedit/editor.go:27` |

So the predecessor's `reconReadStep = 7` was a **model**, and a conservative one:
the real read is bounded by 4096 bytes, and the kernel returns whatever is
available at that moment (VMIN=1, `internal/lineedit/raw.go`'s `rawVMIN`), which is
what makes the read size variable in the first place.

**On the OUTPUT side — which is the side this epic is about — the read size is not
the editor's choice at all.** The sink is an `io.Writer` riding the paint stream
(`chatDumpWriter`, `cmd/agent/chat_dump.go:313`), and
`internal/uiclient/dump.go:447-455` feeds `Screen.Feed(p)` **once per Write it
receives**. The sink therefore interprets the stream in the chunks the WRITER
produced, and the artifact's `frames` counter is literally the sink's READ COUNT.
That is the fact that makes the boundary drivable in a test at all.

**Measured write census** (real turn, `cmd/agent`, 117x26, 88 writes / 1795 bytes;
full log `varread-paint-writes.txt` in this run's session directory):

| size (bytes) | writes |
|---|---|
| 11 | 33 |
| 8 | 20 |
| 10 | 14 |
| 16 | 12 |
| 6 | 2 |
| 75, 62, 37, 358, 34, 333, 24, 12, 7 | 1 each |

`11` is `ESC[?25lESC[25;1H`-shaped chrome; `8` is the freeze write
`ESC[2K ESC[1;1H` (erase line + home) — a complete paint FRAME in one write. The
seven write sizes the predecessor's model could never reach are the ones that
bracket those frames.

## 2. The instrument (deliverable 2)

`cmd/agent/chat_turn_dump_variable_read_pty_test.go` drives one real 117x26 pty,
the real armed `--dump-screen` sink and the real chat layout, and reads the
artifact back as a RENDERED CELL GRID (never a byte log, never OCR). Four legs of
one turn, differing ONLY in the read list handed to the sink:

- `whole` — one read per paint write (the production write sizes; frame boundary
  INSIDE the read);
- `uniform7` — the predecessor's flat 7-byte step;
- `production` — the recorded write sizes REPLAYED as reads;
- `whole2` — `whole` again, to separate a boundary effect from run-to-run noise.

For each leg the reconciliation reports `lost` (produced cells the grid lacks) and
`extra` (grid cells the answer never had) over the artifact's rows, plus a
`## Cross-check` that the boundary the leg delivered IS the boundary the sink saw
(`delivered_reads == sink_frames`), and a render-fidelity comparison of the full
render truth — scrollback content + grid + attribute runs.

Measured (master, and identical on the e06a2aed tree):

```
VARREAD whole      reads=88  bytes=1795 frames=88  dumps=2 produced=171 matched=171 lost=0 extra=0
VARREAD uniform7   reads=257 bytes=1795 frames=257 dumps=3 produced=171 matched=171 lost=0 extra=0
VARREAD production reads=88  bytes=1795 frames=88  dumps=2 produced=171 matched=171 lost=0 extra=0
VARREAD whole2     reads=88  bytes=1795 frames=88  dumps=2 produced=171 matched=171 lost=0 extra=0
VARREAD CROSS-CHECK render: content identical across whole(88) / uniform7(257) / production(88) / whole2(88)
VARREAD VERDICT lost: whole=0 uniform7=0 production=0 whole2=0 | extra: 0/0/0/0
```

**Self-check (the counter is not blind)** — reported per leg, as a DELTA against
the artifact's own baseline so it is valid on a damaged artifact too:
`6 cells removed -> lost=+6`; `16 cells added -> extra=+16` (exact, in both cases).

An earlier draft asserted `extra` absolutely and failed on its own injury
(`want 2, got 16`) — the fix was to make the expectation exact
(`len(fragment)`) and then to make the comparison a baseline delta; both mistakes
were the instrument's, not the product's, and the second would have been a silent
false FAIL on any tree carrying damage.

## 3. A HELD-INTRODUCER boundary defect, and e06a2aed's effect (deliverable 3, part 1)

The whole-turn legs cannot lose a cell from ANY read size, so the boundary's
reachable damage had to be searched for where the interpreter keeps STATE across
reads. `Screen.Feed` holds a partial sequence in `Screen.pending`
(`internal/lineedit/screen.go:162-221`), and one event on this path resets that
state: the geometry re-measure reaches `Screen.Resize`
(`internal/uiclient/dump.go`'s `SetGeometry` → `Screen.Resize`), which on the
pre-fix tree clears `pending` (`screen.go:591-593`, master).

So the boundary CAN damage cells, by this sequence: the sink is handed a bare
introducer `ESC[` as one read, a re-measure arrives while it is held, and the
sequence's parameters come in the next read. Deterministic probe
(`TestChatDumpHeldIntroducerAcrossRemeasure_OnRealSink`):

| tree | row 6 of the artifact | matched / lost / extra |
|---|---|---|
| **master** | `6;1Habcdefghijklmnopqrst` | 40 / 0 / **+4** — the parameters painted as CELLS |
| **e06a2aed applied** | `abcdefghijklmnopqrst` | 40 / 0 / **0** — clean |

That is the live frame's own signature: row 011 of session 884eca28 carries
`;61H24;73H5;230m` — placed sequences with every introducer gone — and the
probe's damaged row carries `6;1H` where cells belong. **This is the one cell
loss this task could produce from a read boundary, and `e06a2aed` eliminates it.**

Honest limits of that probe: the introducer-alone read is CONSTRUCTED (this
renderer emits each placement whole, so a real run does not split one), and the
re-measure is called directly rather than from a reflow. It is a mechanism proof
at the real sink, not a reproduction of a live run.

The assertion is gated by `GOPI_HELD_INTRODUCER_RED=1` (default: SKIP + the
measured numbers), so the defect stays visible without leaving a red test on
master's path (the same landed form as the line-break cadence repro).

## 4. Verdict on the live 38-cell loss (deliverable 3, part 2)

- **Is a variable read size BY ITSELF the cause? No — measured zero.** Four
  boundaries, including a flat 7-byte step and the production write sizes replayed
  as reads, painted identical content with `lost=0 extra=0`. A boundary that
  varies is not by itself a cell loss.
- **Does `e06a2aed` change that outcome? No, and it is not supposed to** — the
  whole-turn numbers are identical on both trees. It changes the outcome of the
  HELD-INTRODUCER boundary only (probe: extra 4 → 0).
- **Is the live 38-cell loss attributable to the read boundary? Recorded as NOT
  DECIDABLE from this task's evidence**, and for a reason that is itself a
  measurement: the frame's GRID shows `;61H24;73H5;230m` (the introducer LOST and
  the parameters painted), while its TEXT plane shows an INTENDED source line
  (`主场景`, `先不立`) painted as different CJK characters (`下文`, `1m`) — the
  glyphs are not the sequence's parameter bytes, so the cells appearing where they
  do cannot be cells the same bytes produced. The mechanism (a) held-introducer
  drop, (b) chunk-boundary misparse, and (c) the two-writer (turn goroutine vs
  editor read loop) sequencing seam are still all standing, and this evidence
  separates them no further.
- **What is newly excluded**: "any variable read size damages the answer" — the
  four-leg matrix, with its counter proven live, measures it as zero. That was the
  specific option the predecessor's uniform-7 model could not close, and it is now
  closed.

## 5. What the live frame's mechanism therefore is not

Not a plain variable-read-size loss. The remaining candidates all need an EVENT
interleaved with a partially-held sequence (a re-measure, a resize, a second
writer's frame), which is why the delivered fix's class is the only one this task
could make red — and the only one it could make green.

## Files

- `cmd/agent/chat_turn_dump_variable_read_pty_test.go` (new) — the four-leg
  variable-read pty reconciliation, its cross-checks and self-check, and the
  held-introducer probe.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0.
- `go vet ./cmd/agent/` → exit 0.
- `go test ./cmd/agent/ -run TestChatTurnDumpVariableReadBoundary_OnRealPTY -count=1 -v`
  → PASS, the leg lines and verdict quoted in §2; identical with `e06a2aed`
  checked into `internal/lineedit/screen.go` + `internal/uiclient/dump.go`
  (`git checkout task/01a0e81bdc177756b9e3ad7d4701260f -- <paths>`, restored with
  `git checkout HEAD -- <paths>`).
- `GOPI_HELD_INTRODUCER_RED=1 go test ./cmd/agent/ -run TestChatDumpHeldIntroducerAcrossRemeasure_OnRealSink -count=1 -v`
  → RED on master (`row 6 = "6;1Habcdefghijklmnopqrst"`, `extra=4`), PASS with
  `e06a2aed` applied (`row 6 = "abcdefghijklmnopqrst"`, `extra=0`).
- Evidence (this run's session directory): `varread-paint-writes.txt`,
  `varread-digest-<leg>.txt`, `held-introducer-probe.txt`,
  `varread-ab-matrix.txt`.
