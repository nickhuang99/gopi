<!-- doc-class: normative -->
# Cell-level reconciliation of the chat paint stream — independent verification of `e06a2aed`

Task `01a0e825-8fbf-7684-a48a-aa674c274252`. Branch
`task/01a0e8258fbf7684a48aaa674c274252`, based on `master` `d1209fe0`.

## What was verified, and what was found

The delivered fix `e06a2aed` (branch `task/01a0e81bdc177756b9e3ad7d4701260f`,
**not merged**, not an ancestor of `master`) changes `internal/lineedit/screen.go`
in two places: `holdPending` no longer truncates an over-long held tail to its last
`maxPendingEscape` bytes, and `Screen.Resize` re-interprets a held tail against the
new grid instead of discarding it.

This task adds the **other half of the criterion** the fix's own tests do not
assert. The fix's regression tests assert one direction — *"no CSI parameter run
reaches a cell"*. The epic this task answers observed, on a live armed dump, that
the same row also shows the **opposite** direction: the answer's own characters
are gone (`主场景`, `先不立` never painted) and a sequence's tail (`1m`) stands
where they should have been.

So the guard added here reconciles **both directions at once, per cell column**:

- **LOST** — cells the producer meant to draw that the grid does not hold;
- **EXTRA** — cells the grid holds that the producer never drew.

### 1. The real pty + armed `--dump-screen` leg (the epic's acceptance item)

`cmd/agent/chat_turn_dump_cell_reconcile_pty_test.go`

A real 117×26 pty, the real chat layout, the real armed `--dump-screen` sink, one
whole turn driven with no keyboard input. The read boundary is **modelled, not
assumed**: a `reconSplitWriter` sits between the editor and the sink and hands
every paint frame to the grid in **7-byte reads** — exactly what a tty does — so
escape sequences are split at real boundaries rather than convenient ones.

The answer carries CJK prose, an emphasis (`**…**`) span, inline code, numbered
tokens, and a tail sentence; the reading is the artifact's scrollback + grid; the
oracle is the answer's own text (never the grid).

```
RECONCILE pty=117x26 read_step=7 frames=306 dumps=2 produced_cells=165 answer_rows=24..39 matched=165 lost=0 extra=0
SELF-CHECK (loss): 6 cells removed -> lost=6 (the counter sees it)
SELF-CHECK (addition): 16 cells added -> extra=16 (the counter sees it)
```

**The counter is proven before its zero is believed.** The self-check injures a
copy of the answer's own rows and requires the counter to move: an instrument that
cannot see a deliberate 6-cell removal cannot certify an intact screen. (The first
version of this self-check **failed** — it cut raw runes out of a space-padded band
row and reported `lost=0`. That was an instrument bug, and it is recorded here
because it is the exact shape of a green that means nothing.)

**PRE-FIX on the same harness passes too** (`lost=0 extra=0`, frames=306). That is
an honest negative result, not a gap: see §4.

### 2. The in-package guard, and its RED proof

`internal/lineedit/screen_cell_reconcile_test.go` — four fixtures (placed text, an
emphasis span, the observed `;61H23H…;230m` three-sequence shape, wide CJK runes),
each fed at **every 2-way split offset** and a **3-way grid**, plus two
carry-policy legs:

- `TestScreenReconcilesAfterAnOverlongHold` — an unterminated OSC (`ESC]11;?`, a
  real bubbletea background query) whose payload exceeds `maxPendingEscape`;
- `TestScreenReconcilesAcrossAReMeasure` — a `Resize` landing between a placement's
  introducer and its parameters.

**RED on the pre-fix tree** (`master` `d1209fe0` `screen.go`), measured:

```
--- FAIL: TestScreenReconcilesAfterAnOverlongHold
      lost runes: 3    lost styles: 0    extra cells: 57
      got: ";9S"  cells past the produced text: "EMI;9SEMI;9SEMI;9…"   <- the held payload, introducer gone
--- FAIL: TestScreenReconcilesAcrossAReMeasure
      read split at byte 1 of "\x1b[1;5H"
      lost runes: 7    extra cells: 1
      row:  "[1;5Hab"    want: "    abc"
```

**GREEN with the delivered fix applied** (`git show <branch>:internal/lineedit/screen.go`
— the branch is *read*, never entered, Rule 29): all four fixtures green at all
boundaries, both carry-policy legs green.

Both files, verbatim: `evidence/reconcile-RED-prefix.txt`,
`evidence/reconcile-GREEN-postfix.txt`.

### 3. Verdict on the existing live evidence `garbled-grid-011.txt`

Recomputed from `~/.gopi/sessions/884eca28-…/chat-screen.dump.json` (the JSON half
of the same artifact), scrollback row 0826, reconciled against that turn's own text
(from `884eca28-….raw.log`), markup folded (air, the rule glyph, `▌`, and the
`**`/backtick the renderer consumes):

```
INTENDED : :path]当主场景来定义它的显示形态。另外第三步（终端上下文回显）我建议先不立，等你确认那是你想要的。
ARTIFACT : path]当下文回显）我建议1m
produced=52  reading=16  matched=14  LOST=38  EXTRA=2  (the `1m` SGR tail)
```

**Which tree was it drawn by? The PRE-FIX tree — established, not guessed:**

- the fix commit `e06a2aed` is dated **2026-09-28T21:27:46+08:00**;
- the session that painted it (`pid 363788`, `gopi chat --tools --dump-screen`,
  still alive at the time of measurement) **started 20:08:28**, 79 minutes earlier;
- `git merge-base --is-ancestor e06a2aed master` → **NO**; the commit exists on
  `task/01a0e81bdc177756b9e3ad7d4701260f` alone.

**Is it a cell-level LOSS? Yes, at the cell layer:** 38 of that row's 52 produced
cells are not on it, and 2 cells (`1m`) have no counterpart in the answer.

**Is the mechanism the fix addresses? NOT established, and said so here.** The
missing span *is* in the artifact — at scrollback row 0850, in the agent's own
later quote of that same line — but **not on any row the band ever committed**.
Two mechanisms can produce that, and this evidence cannot separate them:

1. the delivered fix's class — an escape split at a read boundary, the introducer
   lost, a placement/erase applied somewhere other than intended;
2. a mis-sized read — a reader handing the sink fewer/more bytes than the tty
   produced, so a placement lands with the bytes of a different write.

What the evidence *does* settle is the criterion the fix's own tests were missing:
on that frame the grid was wrong in **both** directions, and "no fragment in a
cell" would not have caught the 38 lost cells.

### 4. Fix completeness — what is covered, what is not

**Covered by `e06a2aed` (measured, red→green):**

- a held sequence's introducer is never dropped while its parameters survive
  (over-long hold: `lost=3 extra=57` → `0/0`; re-measure: `lost=7 extra=1` → `0/0`);
- a re-measure re-interprets a held tail instead of discarding it.

**NOT covered — honest gap, dispatched as a new task:**

- **the real read boundary that produced the live evidence is not reproduced.**
  The pty harness splits at a *uniform* 7 bytes. A tty that returns MORE bytes
  than a frame — or the producer's buffer changing size mid-frame — is a different
  boundary, and no test in the tree drives it. `e06a2aed` neither fixes nor
  regresses it; the live frame's mechanism is still unexplained (see §3's option 2).

Landed form of the gap: `testsuite`-level evidence that the fix cannot reach is
**not** added here; it is dispatched, because a fix for it is a different change
with its own RED proof — task `01a0e851-bbf0-7b56-9922-1313da2e677e`
(`read-boundary: variable tty read size …`, tags `bug,lineedit,chat-render,verification`),
carrying the live numbers, the PRE-FIX attribution proof and the accepted uniform
7-byte model as the thing to falsify.

## Files

- `internal/lineedit/screen_cell_reconcile_test.go` (new) — the in-package cell
  reconciliation guard, both directions, all chunk boundaries, two carry legs.
- `cmd/agent/chat_turn_dump_cell_reconcile_pty_test.go` (new) — the real-pty,
  armed-dump reconciliation, with the read boundary modelled and an instrument
  self-check.
- `devdoc/escape-introducer-cell-reconcile.md` (this file).
- `devdoc/DOC-INDEX.tsv` — the row for this doc (Rule 36).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0, no output.
- Tests:
  - `go test ./internal/lineedit/ -run TestScreenReconcile -count=1`
    → **RED** on the pre-fix `screen.go` (`lost=3 extra=57`; `lost=7 extra=1`),
    **PASS** with the delivered fix (`ok github.com/the operator/gopi/internal/lineedit`).
  - `go test ./cmd/agent/ -run TestChatTurnDumpReconcilesCells_OnRealPTY -count=1 -v`
    → **PASS** (4.37s), `lost=0 extra=0 matched=165/165`, self-check green.
  - `go test ./internal/lineedit/ -count=1` (branch-scoped package run) → see
    `## Completed` note in the task report.
- Ran: `go test ./cmd/agent/ -run TestChatTurnDumpReconcilesCells_OnRealPTY -count=1 -v`
  on the fixed tree, with the pty at 117×26 and `reconReadStep = 7`.
- Result: the log line quoted in §1, from artifact
  `/dev/shm/suite-cmd-agent-run-3456116451/TestChatTurnDumpReconcilesCells_OnRealPTY760066969/001/chat-screen.dump`;
  the only observable difference between the pre-fix and post-fix runs of that case
  is the source applied to `internal/lineedit/screen.go` + `internal/uiclient/dump.go`.
- Commit: see the task report's `## Verification`.
