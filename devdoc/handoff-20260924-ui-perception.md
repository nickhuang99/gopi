<!-- doc-class: normative -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
# Handoff — `gopi ui` perception: what to verify when the new binary runs

**Read this immediately after starting a session on the new `gopi` binary.**
It is written by the session that built this (2026-09-24) **for its own next
self**, which will not remember any of it. It is a *verification* checklist, not
a summary: every "should work" below was measured by the author, and every
command is copy-pasteable.

The goal of this whole line of work: **`gopi ui` must be perceptible to an agent
as TEXT — no pixels, no vision model, no cross-process screen grabbing.** Once
that holds, the agent can iterate on its own interface without the operator in
the loop. Everything below exists to answer one question: *is that true yet?*

---

## 0. The 60-second smoke test (do this FIRST)

```bash
cd /home/user/work/GoPi
CGO_ENABLED=0 go build -o /tmp/gopi-check ./cmd/agent/     # expect: silent, exit 0
ls -la internal/lineedit/screendump.go internal/uiclient/dump.go
```

Then run the real surface under a pty (this is the whole point — a pipe is not a
terminal, and bubbletea behaves differently without a TTY):

```bash
mkdir -p /tmp/ui-check && cd /tmp/ui-check
cat > drive.py <<'PY'
import os, pty, select, time, struct, fcntl, termios
BIN="/tmp/gopi-check"; DUMP="/tmp/ui-check/live.dump"
pid, fd = pty.fork()
if pid == 0:
    os.execv(BIN, [BIN, "ui", "--addr", "127.0.0.1:0", "--dump-screen="+DUMP])
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
buf=b""; t0=time.time()
while time.time()-t0 < 6:
    r,_,_ = select.select([fd],[],[],0.5)
    if r:
        try: buf += os.read(fd, 65536)
        except OSError: break
print("pty bytes:", len(buf)); print("dump:", os.path.getsize(DUMP) if os.path.exists(DUMP) else "MISSING")
os.write(fd, b"q"); time.sleep(1.3)
try: os.kill(pid, 9)
except Exception: pass
PY
timeout 30 python3 drive.py
head -8 live.dump
```

**Measured twice on master (at/after merge `5d677e54`), byte-for-byte:**
`pty bytes: 917` then `905` (the pty byte count varies run to run — do NOT use it
as an assertion), **`dump: 5499` both times**. `dump: 5499` is the stable
number for an idle 100x30 surface at initial paint; treat a *different* size as
investigate-worthy, not as automatic failure.

If `dump:` says MISSING, or the head is not the `#gopi-ui-dump` provenance line,
**stop and report that** — do not proceed to interpret a stale file.

---

## 1. What is actually on master now (verified, not assumed)

Merged and confirmed present:

| Path | What it is |
|---|---|
| `internal/lineedit/screendump.go` | `Screen.Dump() ScreenDump` — the representation |
| `internal/uiclient/dump.go` | `ScreenDumper` — an `io.Writer` **beside** bubbletea's paint stream |
| `cmd/agent/cli_ui.go` | `--dump-screen[=PATH]`, `--dump-mode ansi\|plain`, `GOPI_UI_DUMP`, `GOPI_UI_DUMP_MODE` |
| `internal/uiclient/model.go` | intra-line emphasis (spans over the existing `LineStyle` decision) |
| `devdoc/ui-screen-self-observation.md` | the full design + evidence matrix |
| `devdoc/ui-entropy-audit.md` | **1/133 the bytes** vs the pixel path, +6 assertable facts |
| `devdoc/evidence/ui-screen-dump/` | `capture.sh` + three measured dumps (`.live`, `.abrupt-kill`, `.before-first-frame`) |

Merged via `5d677e54 merge: ui-screen-self-observation`. **Check the merge is
still in your history** (`git log --oneline | grep -i self-observation`); a
rebase or a bad merge-back could silently drop it, and then everything below
fails for a reason that has nothing to do with the feature.

### ~~The one thing that is NOT done~~ — CORRECTED 2026-09-25: `ui_state` HAS landed

**This subsection originally asserted that `ui_state` — the agent-facing tool —
was NOT built, with task `01a0d337-3e73-79ec-bdcb-1a60ca1bbe9d` pending. That is
no longer true, and the claim is retracted.** The task landed
(`devdoc/ui-state-reader.md`, `cmd/agent/cli_uistate.go`); a reader who trusted
the old text stopped at a door that is open.

The text is corrected rather than silently deleted, because the misreading is
the lesson: **a handoff doc's "not done" is a snapshot of its own writing
moment, never a fact about the tree you are standing in** — check the binary
(`gopi ui_state -h`) before you believe a checklist.

Measured 2026-09-25 on the branch that fixed this doc
(`CGO_ENABLED=0 go build -o /tmp/gopi-docdrift ./cmd/agent/`):

```bash
$ /tmp/gopi-docdrift ui_state -h ; echo $?
gopi ui_state — read a live `gopi ui` screen state as an interpreted cell grid
...
0
$ GOPI_HOME=/tmp/gopi-docdrift-home /tmp/gopi-docdrift ui_state ; echo $?
gopi ui_state: no ui-screen.dump dump found in any scanned location
1
```

(`/tmp/gopi-docdrift-home` was an empty directory, so "no dump" is the honest
answer there — the exit code 1 is the refusal, not a failure.)

So: content is perceptible, screen-state is perceptible, **and screen-state is
reachable by a TOOL CALL** (one call, no path). §4 is the verification path for
it.

---

## 2. Four things I verified today, and how to re-verify each

### 2.1 The dump describes the interface AS RENDERED
`ScreenDumper` is an `io.Writer` on the same stream bubbletea paints to. So the
dump is not a replay guess — it is what the surface actually put on the screen.

**Re-verify:** the `#gopi-ui-dump` provenance line must carry `frames=` > 0.
Measured: `frames=4`. `frames=0` means nothing was painted — the sink is
attached but the surface never drew (wrong `--addr`, or it exited early).

### 2.2 Content is already reachable over HTTP
`gopi ui` starts the `gopi web` engine **in-process**. Measured: the `ui` process
listens on its own port (`ps` showed pid 22202 owning `127.0.0.1:8090`), and

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:<port>/api/transcript   # measured: 200
```

**Re-verify:** with `gopi ui --addr 127.0.0.1:8090` running, that curl returns
200. This is the *content* channel ("what was said"); it needs no new code. Note
it is an **internal** port with a token gate — read `webGatedHandler`/`webTokenGate`
in `cmd/agent/cli_web.go` before assuming a bare curl works from elsewhere.

### 2.3 Per-cell attributes are exported
The dump carries an `attrs_begin` … `attrs_end` section, one run per row,
covering the **full width**. Measured, an untouched screen:
`000|0:100=default` (the whole 100-cell row is default style).

**Re-verify:** after the intra-line emphasis feature, type a message with
`**bold**` and check that the answering row's attr run *changes* from
`default` — i.e. some span is no longer default. **If every row still reads
`=default` in every state, the emphasis path is NOT reaching the renderer**,
even if the plain `**` may look right in the text.

### 2.4 Blind spots ship with every dump
`blind_spots_begin count=8` … `blind_spots_end`, each `id|explanation`.
Re-verify the count is 8 and that `frame_timing` and `interior_scroll_loss` are
present — they are the two that most often bite when you misread a dump.

**These are not decoration.** They are the list of things this instrument
*cannot* see. Read them before concluding anything is broken:
- `frame_timing` — a dump is one instant; a flicker or a transient wrong layout
  is invisible. **Do not conclude "no flicker" from a single dump.**
- `raw_bytes` — the dump is the *effect* of the byte stream, not the stream.
  Sequence order and timing are gone.
- `blank_vs_unpainted` — a space cell and a never-painted cell are identical.
  "This row is padded" is assertable; "this row was WRITTEN as blanks" is not.
- `interior_scroll_loss` — a line pushed off an *interior* scroll-region margin
  is discarded by the terminal, not saved. History beyond the grid plus
  `Scrollback()` is unrecoverable.

---

## 3. THE TRAP — "clean quit = blank screen" (measured twice)

This is the single most likely way to fool yourself into thinking perception is
broken.

- **Clean quit** (press `q`) → bubbletea runs its exit wipe (`CSI J`) → the last
  write is the **erased** screen.
- **Abrupt kill** (SIGTERM/SIGKILL, e.g. `timeout`) → the exit sequence never
  runs → the file holds the **last painted frame**.

**Measured today:** after `os.write(fd, b"q")` the file *grew* (5499 → 5685
bytes) and still contained the painted rows; an earlier session's report claimed
"blank after exit" and that was **explicitly corrected as not observed**. So the
observed behavior is version- and timing-dependent — **which is exactly why you
must not rely on a post-exit dump.**

> **RULE: read the dump WHILE the surface is running.** That is what
> `devdoc/evidence/ui-screen-dump/capture.sh` does. A dump read after the
> process exits is evidence about the *exit*, not about the interface.

If you see an empty or near-empty dump, **suspect the observer's timing before
suspecting the surface.**
Incident refs: `devdoc/dump-name-collision.md`, `internal/deepseek/dump_file.go`
(existing name-collision machinery — a dump path can collide with the HTTP dump
files, so a stale file may not be yours).

---

## 4. Verify the `ui_state` tool like this (IT HAS LANDED — this is the live path)

1. **It must discover, not require a path.** Start one `gopi ui`, call the tool
   with no path argument, and confirm it finds the live surface.
2. **It must refuse ambiguity.** Start **two** `gopi ui` processes and call it.
   It must **refuse and list both candidates** — never silently pick one. This
   is a hard requirement: this host has repeatedly had two gopi surfaces at once
   (the operator's `gopi ui --tools` and the agent's own).
3. **It must diagnose the exited target.** Point it at a dead surface's dump and
   confirm it says so in words — measured, the exact sentence is *"the target
   has exited cleanly: the dump is the LAST frame that surface painted, not the
   current screen"* (`uiclient.ExitedNotice`) — rather than returning a blank
   screen as fact. (This line read "appears to have exited" until 2026-09-25; the
   real diagnosis is a fixed constant, and a checklist that paraphrases it asks
   the reader to confirm a sentence nothing prints.)
4. **Its blind-spot list must be visible in its output**, and its description's
   first sentence must say what it is ("the interpreted cell grid — not the byte
   stream, not pixels").

**How to drive the surface for 1–3 (measured 2026-09-25, both ways):** use the
`script -q -c "stty rows 30 cols 100; …"` route §0/capture.sh uses. A surface
driven through `pty.fork()` + `TIOCSWINSZ` wrote its dump file at startup and
then **never painted a frame** (header stuck at `frames=0 dumps=0`, all-blank
grid, `state=live`) for the whole budget, while the same binary under `script`
painted in ~15s. The dump file exists either way — **wait on the frame counter
in its first line, never on the file's existence.** All four requirements were
measured green with a pty-driven pair of surfaces (evidence:
`devdoc/handoff-20260924-ui-perception-drift-fix-report.md`). One cosmetic
defect is live and unfixed: with **two dead** surfaces the refusal's first line
reads "2 **live** gopi surfaces could be the target" while its own table marks
both `exited` (`internal/uiclient/dumpread.go` — `AmbiguityError.Error()`
renders one text for both cases).

---

## 5. What "done" looks like (the acceptance the operator actually cares about)

The operator's framing, verbatim in intent: *display is secondary and always
fixable; the key is whether you can perceive — that is the loop; once the channel
is open you can self-iterate without me.*

So do not report "it builds" or "the dump looks right". Report **this**:

> An agent, using only a tool call, obtained the current `gopi ui` screen state
> as text and made **N independent layout assertions** about it — and the pixel
> path cannot make at least one of them.

Measured anchors to aim at (from the merged work): the dump's row 0 is
`gopi ui → <target>  (tools off)` starting at cell 0 with no surviving stray
cells (that is the regression for the two real defects found — see below), and
`cursor row=29 col=0` on an idle 100x30 surface.

**The two defects the E2E run already caught** (both fixed, both with repro
tests) — keep them in mind as the class of bug this instrument finds:
1. an **OSC sequence was painted as text** (bubbletea's background query
   `ESC ] 11 ; ? ESC \` left `"1;?"` in cells 0–2, so the header row read
   `" opi ui → …"`);
2. **`CSI J` was only implemented for parameter 2** — directional erases were
   no-ops, so cells the terminal had already wiped survived in the grid.

Both are invisible to a human eye and to a vision model. They are visible to a
reader of *this* dump. That is the argument for the whole approach — **and if
you find a third one, that is a success, not a regression.**

---

## 6. Open items / honest limits

- **`ui_state` tool: LANDED** (see the correction in §1 — this item read "not
  built" until 2026-09-25). `gopi ui_state` discovers the dump, attributes it,
  and prints the cell grid in one call; §4 is its verification recipe — and all
  four of §4's requirements were measured green on 2026-09-25 with two real
  pty-driven surfaces (`devdoc/handoff-20260924-ui-perception-drift-fix-report.md`).
- **Known cosmetic defect in the refusal text (found 2026-09-25, NOT fixed).**
  With **two dead** surfaces the refusal is right (exit 2, both candidates
  listed, both marked `exited` in its own table) but its first line reads
  *"2 **live** gopi surfaces could be the target"* — `AmbiguityError.Error()`
  (`internal/uiclient/dumpread.go`) renders one text for the live-pair and the
  dead-pair cases. Anyone quoting the first line alone would call two exited
  surfaces running.
- **The intra-line emphasis feature needs a visual confirmation** (§2.3) — the
  tests are text/cell-level; nobody has yet confirmed on the real surface that a
  `**bold**` span shows as a non-default attr run.
- **Fonts, images (sixel/kitty), IME composition, private modes, and frame
  timing are permanently invisible to this instrument.** Do not try to fix that
  here — see `dumpBlindSpots` in `internal/lineedit/screendump.go`. Font/glyph
  quality legitimately needs pixels; nothing else on that list does.
- **`gopi chat` (the plain line-editor REPL) is the fallback and must stay
  untouched.** `gopi ui` is the main surface; `internal/lineedit` is shared, so
  a change there reaches both — check both before claiming a change is safe.
- The whole-tree suite has **pre-existing reds** that are not yours:
  `internal/stylegate` (`internal/tools/merge_build_gate_gocache_test.go:110`)
  and `internal/smoke` (`TestStagedScriptTreeCheckFailsWhenTheSourceClosureIsLost`,
  a red-proof fixture). Both have an empty diff against master.

---

## 7. Where to read more (in this order)

1. `devdoc/ui-screen-self-observation.md` — the design, the evidence matrix, the
   three post-run states, the honest "Not Done" list.
2. `internal/lineedit/screendump.go` — `ScreenDump`, `dumpBlindSpots` (read the
   blind-spot constants; they are the instrument's contract).
3. `devdoc/evidence/ui-screen-dump/capture.sh` — the working recipe for driving
   the surface under a pty and reading the dump live.
4. `devdoc/ui-entropy-audit.md` — the cost argument (1/133 bytes) and the rule
   for when pixels are still legitimate.
5. `internal/uiclient/model.go` — `answerSpan` / `DisplaySpans` (the emphasis
   layer), and the `TestViewLayoutIsStable`-style layout assertions.
