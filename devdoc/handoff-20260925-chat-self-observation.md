<!-- doc-class: normative -->
# Handoff — `gopi chat` self-observation: what to verify, and the ONE merge you must do first

**Read this immediately after starting a session on the new `gopi` binary.**
Written 2026-09-25 by the session that filed the task, **for its own next self**,
which will not remember any of it. It is a *verification* checklist plus one
required action — not a summary of feelings.

---

## 0. THE ONE THING TO DO FIRST: merge the unmerged deliverable

The work you are about to verify **is NOT on master.** It sits on a task branch
that was left unmerged by design ("not merged, peer not pushed" — the task
report's own close-out line).

```bash
cd /home/user/work/GoPi
BR=task/01a0d5ca22507cb9ba577dc6000739cd
git log --oneline -1 $BR          # expect: 1cf98e3b feat(chat): --dump-screen — the chat surface observes its own answer band
git branch --merged master | grep -c 01a0d5ca    # expect: 0  (i.e. NOT merged)
grep -c 'dump-screen' cmd/agent/cli_chat.go      # expect: 0  (master lacks the flag)
```

**Measured on master `6be7aeff` (2026-09-25 20:35, clean worktree):**
- branch exists, tip `1cf98e3b`, **1 commit** ahead of its merge-base;
- **not** an ancestor of master → **not merged**;
- `cmd/agent/cli_chat.go` on master has **zero** `dump-screen` hits.

**So a "new binary" built from master alone does NOT have the chat dump.** If the
new binary you were handed was built from master, the capability is absent and every
check in §3 below will fail with "the flag does not exist" — that is the merge being
missing, **not** the feature being broken. Merge first:

```bash
git checkout master
git merge --no-ff $BR        # expect: 12 files, +1556/-38 (see §1), no conflicts on master 6be7aeff
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   # expect: silent, exit 0
./bin/gopi chat -h 2>&1 | grep -A3 -- '--dump-screen'   # the flag must now be documented
```

> **Why this is called out so loudly:** the diff `master..$BR` looks alarming
> (150 files, −13 482 lines). **That is an artifact of master having advanced
> past the branch's merge-base** and is NOT what the branch changes. The branch's
> own commit `1cf98e3b` touches **12 files**: `cmd/agent/chat_dump.go` (new, 297),
> `cmd/agent/chat_dump_test.go` (new, 626), `cmd/agent/cli_chat.go` (+63),
> `cmd/agent/lineedit_wire.go` (+51), `cmd/agent/cli_uistate.go` (+57),
> `cmd/agent/chat_stream_test.go` (±34), `internal/uiclient/dump.go` (+159),
> `internal/uiclient/dumpread.go` (+137), `internal/uiclient/dumpread_test.go`,
> `CONFIG.md` (+1), `devdoc/chat-screen-dump.md` (new, 121),
> `devdoc/chat-screen-dump-drive.py` (new, 46). **Verify with
> `git show --stat 1cf98e3b`, never with `git diff master..$BR`.**

---

## 1. What this work is, in one paragraph

`gopi chat --tools` — **the surface the agent IS**, not a neighbour — now observes
itself. It arms the existing `uiclient.ScreenDumper` and tees its **real paint
stream** (the editor's own writer, the layout's only writer) into
`chat-screen.dump` (+ `.json` twin) with a `Surface` discriminator (`ui` | `chat`,
carried in the provenance prefix `#gopi-chat-dump` and cross-checked against the
file NAME by `ReadDump`). `gopi ui_state --target ui|chat` reads it back as an
**interpreted cell grid with per-cell attributes** — no pixels, no vision model,
no cross-process screen access. A nil sink returns the caller's writer **unchanged**,
so a run without the flag is byte-identical **by construction, not by review**.

This closes the gap this whole line of work existed for. Before it: the eye was
built, wired, honest about its 8 blind spots — and **mounted on a different
process** (`gopi ui`, the BubbleTea surface). The agent could see the neighbour
and not its own face.

---

## 2. What is on the branch (verified by reading the committed report + commit)

| Path | What it is |
|---|---|
| `cmd/agent/chat_dump.go` (new) | flag/env parse, path resolution, `chatArmDump`, the notice, `chatDumpFinalize` |
| `cmd/agent/cli_chat.go` | `--dump-screen[=PATH]` + `GOPI_CHAT_DUMP`; arms **before the first frame**; freeze before teardown |
| `cmd/agent/lineedit_wire.go` | `newChatInputWithDumper`; the tee sits at the editor's writer |
| `cmd/agent/cli_uistate.go` | `--target ui\|chat` + the surface column in `--list` |
| `internal/uiclient/dump.go` | `Surface` discriminator on the dump |
| `internal/uiclient/dumpread.go` | surface-vs-filename cross-check; never reports a chat dump as a ui dump |
| `cmd/agent/chat_dump_test.go` (new) | 8 per-cell bold assertions through the REAL render path |
| `devdoc/chat-screen-dump.md` | the task report (read it — it quotes measured evidence) |
| `devdoc/chat-screen-dump-drive.py` | the live-PTY recipe (real `/dev/ptmx`, 40x100) |

**A real defect the live run found and fixed** (worth knowing, it is the kind of
thing that silently fakes a pass): the sink was frozen by the REPL's **deferred**
finalize, which runs *after* `leaveChat` → `input.CloseOut()` had already taken the
layout down (region reset, scroll-region reset, cursor show). The frozen artifact
therefore described the **teardown**, not the screen — a real run wrote **24 empty
rows with `frames=16 dumps=5`**, so every "the dump exists" check passed while the
artifact said nothing. The freeze now happens in `chatInput.CloseOut`, **before**
the terminal is handed back. Regression test + red proof are in the branch.

---

## 3. The 60-second smoke test (after §0's merge)

```bash
cd /home/user/work/GoPi
CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
ls -la cmd/agent/chat_dump.go internal/uiclient/dump.go   # both must exist
```

Then the tests **the branch itself names as its gate**:

```bash
go test ./cmd/agent/ -run 'TestChatDump_' -count=1        # expect: ok, ~27s, 7 tests
go test ./internal/lineedit/ ./internal/uiclient/ -count=1
go test ./internal/standards/ -run 'TestConfigDoc' -count=1
```

**Measured by the author on the branch:** `TestChatDump_` → `ok … 26.583s`.
If that suite is green and the flag is documented, the mechanism is present.

---

## 4. The live surface (the real proof — Rule 31)

A pipe is not a terminal; the dump of a surface that never painted is an honest
all-blank grid. Use the branch's own recipe:

```bash
# a real PTY + a stub SSE model, per devdoc/chat-screen-dump-drive.py
python3 devdoc/chat-screen-dump-drive.py     # drives bin/gopi chat --dump-screen=$SINK over /dev/ptmx
```

**Measured on the branch's live run** (quoted from the report, verbatim):

```
#gopi-chat-dump-report path=…/chat-screen.dump json=…/chat-screen.dump.json
  bytes=8664 pid=1280805 grid=100x40 dumps=6 frames=146 blind_spots=8
```

```
018|──── turn 1 · say the thing ───────…
023|▌ here is the answer:
025|▌ alpha and beta plain
027|▌ ```
028|▌ code **literal** line
029|▌ ```
031|▌ done
038|you>
039|deepseek-flash   ·  iter 1  ·  …  17 tokens  ready
```

```
attrs_begin
025|0:2=default 2:5=bold 7:5=default 12:4=bold 16:84=default
027|0:2=default 2:3=fg=cyan 5:95=default
028|0:2=default 2:21=fg=cyan 23:77=default
038|0:4=fg=green bold 4:96=default
```

Read that carefully — **it answers the exact question that started this**:
`alpha` and `beta` are bold (columns 2..17; the gutter 0..1 is not), the fenced
`code **literal** line` is **cyan code with NO bold cell** (the fence gate working
as designed), and the prompt is green+bold. Then:

```bash
gopi ui_state --target chat     # expect: #gopi-ui-state surface=chat pid=… state=exited + the grid, exit 0
gopi ui_state --list            # expect: the row listed with surf=chat
```

**The three mechanisms the epic named, and how to tell them apart** — this is the
part that makes the artifact worth having, so do not skip it:

1. **half-row** (`rowForPartial`, `internal/lineedit/answer.go:2714`) — an
   unfinished streamed tail is painted **with its markers intact**, deliberately
   (removing a marker is an irrevocable decision). If you see a literal `**` in a
   *live* frame, check whether the row's line is still open. **By design, not a bug.**
2. **fence/diff gate** (`rowFor`, `answer.go:1158` — `if ls != LinePlain || !hasBoldMarker(line)`)
   <!-- docanchor drift permitted 2026-09-27 (task 01a0e2ea91f37e9185e4f99317484247):
        dated handoff measured on master 6be7aeff (2026-09-25); the guard now lives
        deeper in the same file and re-pointing this citation would present that
        2026-09-25 measurement as today's. Repair = re-key by symbol from the row. -->
   — on a non-plain line the whole span path is skipped and `**` is drawn literally.
   Deliberate (a fence's `**` IS literal) — but a mis-placed fence silently
   de-bolds the prose after it. **Row 027/028 above is this, measured.**
3. **the other carrier** — `gopi ui` wraps via `uiclient.wrapEntry`; if a symptom
   shows on `gopi ui` but not on `chat`, it is the wrap path, not `lineedit`.

---

## 5. PITFALLS — measured this session, each one cost real time

1. **`SetPalette` is mandatory in any render probe.** `NewAnswerRenderer(reg, nil)`
   without `SetPalette` leaves the **zero Palette**, so `EmphasisBold` maps to an
   **empty style** and *every* bold cell silently renders plain. My first probe did
   exactly this and produced a **false "bold is broken"**. Use the existing helper
   `renderEmphasis` (`internal/lineedit/emphasis_test.go:48`) — it does
   `newPaletteCapture` + `newRegion` + `SetPalette` + `Tick` + `Finish` correctly.
   **This is the single easiest way to reach a wrong conclusion about emphasis.**
2. **`go test ./internal/lineedit/` prints enormous scratch-gc noise.** Filter:
   `grep -E '^row|^  mask|^BURSTS|^(ok|FAIL)'`.
3. **Do NOT `git diff master..$BR`** to see what the branch changed (§0). Use
   `git show --stat 1cf98e3b`.
4. **Pixel screenshots + vision are the black-box fallback this project ruled out**
   (`devdoc/ui-screen-self-observation.md`): ≈310 KB/screen vs ≈8.7 KB for this
   dump, and vision *misread* the markers it was pointed at (it read
   `marker=ALPHA-7441` as `marker=0,0`). Use the dump.
5. **A background process dies with its bash call.** `xdotool windowactivate --sync`
   hung → the bash timeout killed the whole process group, including the xterm probe
   I had just opened. For anything long-lived use `setsid` + `job`, and never a
   blocking `--sync`.
6. **`--dump-screen=<explicit path>` is invisible to the scanner** if the path is
   outside the session dir — a known, documented boundary (`Not Done` #1 on the
   branch). Do not report the scanner's silence as "the eye is broken"; use the
   bare flag form, or read the file directly.

---

## 6. What is honestly NOT done (do not claim it)

From the branch's own report — quoted, not softened:

- the chat dump is **not** yet discovered by `ui_state`'s default roots when it
  lives at an explicit path outside a session dir (only the session-dir placement,
  which is the resolver's default, is verified);
- the **fenced-line** test asserts "no cell bold" + the literal text, **not** the
  cyan `LineCode` colour per cell (the colour IS visible in the dump quoted in §4,
  but no test pins it);
- `GOPI_CHAT_DUMP` has **no test of its own shape** (truthy vs path) — covered
  indirectly;
- **no test pins the `frames=0` announcement text** on the cooked path.

---

## 7. Where the record lives (if the merge is refused or you must re-derive)

```bash
BR=task/01a0d5ca22507cb9ba577dc6000739cd
git show $BR:devdoc/chat-screen-dump.md          # the full task report
git show $BR:cmd/agent/chat_dump.go              # the implementation
git log --oneline -1 $BR

# ledger row + the run's own audit trail:
gopi task show 01a0d5ca-2250-7cb9-ba57-7dc6000739cd
#   session_log / raw_log paths are printed by that command; the run was
#   366 turns, exit=0, 3362s, worker on hostb-Laptop.
```

**Ledger status: `completed`** (the worker's own close-out said "not merged, peer
not pushed" — so *completed* here means *delivered to a branch*, not *on master*).

---

## 8. The one-paragraph version, if you read nothing else

Merge `task/01a0d5ca22507cb9ba577dc6000739cd` (tip `1cf98e3b`) into master, rebuild,
then run `go test ./cmd/agent/ -run 'TestChatDump_'` and the live PTY recipe — after
which `gopi chat --dump-screen` writes `chat-screen.dump` and
`gopi ui_state --target chat` reads **your own** screen back as a cell grid with
per-cell attributes. That is the difference between seeing the neighbour and seeing
your own face. The bold question that started this is answered, per cell, in §4:
`alpha`/`beta` bold, the fenced `**literal**` correctly literal and cyan.
