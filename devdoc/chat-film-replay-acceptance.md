<!-- doc-class: normative -->
# `gopi chat --replay <session> --film <outdir>` — end-to-end acceptance

**Task** `01a1003a-7e74-7dee-aec2-51aeb71bff18` (rework of `01a0ffbb-a7eb-7828-b0d7-a80a5b8bbc71`,
epic item 5/5), branch `task/01a1003a7e747deeaec251aeb71bff18`, base **master
`429ac774`**. **No production code changed in this run** — it is acceptance
evidence plus this document. Everything below was re-measured on the real binary
built from that base (`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`), and
every artifact lives in the run's session directory,
`~/.gopi/sessions/01a1003a-7e74-7dee-aec2-51aeb71bff18/evidence/` (Rule 30/37).

> **How this document relates to the deleted branch.** Its first edition lived on
> `task/01a0ffbba7eb7828b0d7a80a5b8bbc71` (tip `dd95e929`), whose ref is **gone**;
> a one-shot merge judgement of 2026-10-03 ruled that branch unmergeable in
> principle (its unique work is this doc plus a replayed DOC-INDEX line, and it
> read master's film/replay takeover the wrong way round). This edition is
> **re-authored on master**, not replayed: every number below was re-taken here,
> and §10 names what the old edition asserted and this one corrects. The old
> diff survives only as the run's attachment (`sha256
> 7b6201a8e139285cc490096319b83ceaa53ee87d951ada18d50d5e90855b56a6`, 27144
> bytes; a task-id-named copy of the 469-line document was extracted from it as a
> reference blob and is **not** an input to the shipped file).

## 1. The claim under test

One command, no shell driver, no python3, no `xdotool`/`xwininfo`/ImageMagick:

```bash
gopi chat --replay <session-id> --film <outdir>
```

The producer is the *same binary* re-entered (`os.Executable()` →
`gopi chat --replay <id> --scratch <dir> --cols 117 --rows 26`), the driver owns
the private X display and the private terminal, and the beat protocol
(`beats.tsv` / `ack` / `done.txt`) is the one master's `cmd/agent/chat_replay_beats.go`
emits and `cmd/agent/chat_film.go` consumes.

## 2. Verb list — a flag on `chat`, and one verb REMOVED

The shape claim is falsifiable from two sources; both were read on this base.

**The `gopi --help` command index** (`internal/config/help.go`, the one array the
advertised list is rendered from). Normalised to one `gopi <verb>` word per line:

```bash
git show 971400a7^:internal/config/help.go | grep -o '{"gopi [a-z_-]*' | sed 's/{"gopi //' | sort -u > before.txt
git show HEAD:internal/config/help.go      | grep -o '{"gopi [a-z_-]*' | sed 's/{"gopi //' | sort -u > after.txt
diff before.txt after.txt
```

```
28d27
< replay
before=45 after=44
```

(`971400a7` = `refactor(replay)!: abolish the gopi replay verb and its self-drawn
renderer`; the pre-image commit `971400a7^` is the same commit as the epic's
`c8e71550`, verified by rev-parse.) The **built binary agrees**: `./bin/gopi
--help` prints **57** `  gopi <verb>` index rows and, of the words `replay` and
`film`, mentions neither as a command — its single hit is the `--keep-thinking`
line:

```
56:  --keep-thinking ON|OFF           replay reasoning in assistant history
```

The index count itself is re-derived by a **gate** on this run, not asserted by
this prose: `go test ./cmd/agent/ -run 'TestVerbCensus' -count=1` →
`--- PASS: TestVerbCensusMatchesTheDispatch (23.97s)`.

**The flags live on `chat`** — `gopi chat --help` is the only place they are
documented (usage line, verbatim; the beat protocol's `--scratch`/`--cols`/
`--rows` are documented in the page's own beat section, below the synopsis):

```
Usage: gopi chat [--tools] [--no-editor] [--no-color] [-v|--verbose] [--dump-screen[=PATH]|--no-dump-screen] [--resume SESSION] [--replay SESSION] [--film DIR [--out FILE.gif]] [chat-ideal-geometry COLSxROWS] [raw-log-path|session-id]
```

The flag set and the group total are re-derived, never counted by hand:
`bash scripts/g5-chat-web-help-surface.sh ./bin/gopi` →

| surface | flags | n |
|---|---|---|
| `chat` | chat-ideal-geometry, film, film-terminal, max-seconds, no-color, no-dump-screen, no-editor, once, out, policy, prompt, replay, resume, stop-on, tools, turns, verbose, voice-audio, wechat, ack-timeout-ms, cols, max-beats, rows, scratch | 24 |

with the whole derived group totalling **47** rows of the script's own output
(re-derived 2026-10-19 on the binary built from this commit — Rule 38: the two
figures above are what `scripts/g5-chat-web-help-surface.sh ./bin/gopi` printed,
never a hand-increment. The `chat` row had drifted to 21 while the script
measured 23: `--policy` and `--voice-audio` had reached the page without this
row being re-derived, and `--wechat` joined them when the replay/film family
gained its own help page — `gopi chat --replay -h` — so the row is now 24 and
matches. The family's words still appear on `gopi chat -h` as an INDEX that
names the scoped page.)
(The script prints each row's flags in ITS OWN alphabetical order; the row above
keeps the order this document's earlier editions used — the beat protocol's five
words appended — so the anchor `scripts/replay-check.sh` reads the count from
still resolves.)
(Both figures are *this base's*; the deleted branch's edition quoted 13/39 from
`44303fbb`, and the drifting number is exactly why the script exists.)

## 3. The run — verbatim

```bash
cd /home/user/workspace/gopi-01a1003a7e747deeaec251aeb71bff18
E=~/.gopi/sessions/01a1003a-7e74-7dee-aec2-51aeb71bff18/evidence
./bin/gopi chat --replay 758507fd-4e12-4df6-b67a-c75aaa33aa42 --film $E/film
```

stderr, verbatim (paths shortened at `…`):

```
[gopi film] display :63 (server pid 667384), terminal xterm at 117x26 cells on it
[gopi film] producer: …/bin/gopi chat --replay 758507fd-… --scratch …/chatfilm-309770636 --cols 117 --rows 26 (its output is appended to …/evidence/film/producer.log)
[gopi film] terminal window 2097164 ("gopi film film") area 706x342+0+0
[gopi film] film written to …/evidence/film: 6 frame(s) from 6 ledger row(s), 6 frame(s) reached the settled criterion, 0 did not
[gopi film] beats.tsv: …/evidence/film/beats.tsv
```

exit `0`, wall time **6.5 s** (`time` → `real 0m6.529s user 0m0.249s sys 0m0.038s`).
The session replayed is a real recorded `gopi chat` session (one turn "Say the
single word: pong").

**Production metadata** (all measured, not quoted):

| fact | value |
|---|---|
| frames | 6 × `frame-0001..0006.png` |
| distinct md5 | **6 of 6** (no two frames identical) |
| frame geometry | every IHDR `0x2c2 × 0x156` = **706×342 px** (117×26 cells) |
| `frames.tsv` | 6 rows, `grabs=2`, `stale=0`, `settled=yes`, bytes 9897/10011/10125/10569/9305/8618 |
| `beats.tsv` | 6 rows: 4 × `request`, 1 × `answer`, 1 × `chrome` |
| `producer.log` | 16431 bytes |
| `terminal.log` | 0 bytes (xterm session log, empty = the terminal itself wrote nothing) |

**A visual read of frame 1** (`vision_analyze`, qualitative only) shows the
wordmark, `Welcome back!`, the `Tips for getting started` column, the
`This session` block and the status row — i.e. the frames are the real chat
layout, not a blank terminal. It is *not* evidence about render quality (Rule
10.2); the structural assertions above are.

### 3a. The beat protocol's own handshake, retained on disk

The protocol markers live in the driver's scratch, which is swept when the run
ends — so a second run named it from the caller (`--scratch`, the flag the driver
passes the producer) to keep `ack` / `done.txt` / the per-beat dumps as evidence
rather than as a quote:

```bash
./bin/gopi chat --replay 758507fd-… --scratch $E/film-kept/scratch --film $E/film-kept/film
[gopi film] terminal window 4194316 ("gopi film film") area 706x342+0+0
[gopi film] film written to …/film-kept/film: 6 frame(s) from 6 ledger row(s), 6 frame(s) reached the settled criterion, 0 did not
exit 0
```

```
$E/film-kept/scratch/  ack=2B ("6")  done.txt=2B ("6")  beats.tsv=316B
                       redactions.tsv=135B  dumps/=6  xvfb-63.log=0B
$E/film-kept/film/     6 × frame-000N.png  frames.tsv (6 rows, grabs=2, stale=0, settled=yes)  beats.tsv  producer.log
```

The six frames are **not byte-identical** to §3's (independent grab of a live
terminal), but the beats are: `cut -f1,2,3,5,6` of the two `beats.tsv` files
diffs empty — `IDENTICAL` — only the wall-clock `at_ms` column moves. That is the
honest shape of this evidence: the **content** of a replay is reproducible, the
**pixels** of a screenshot are not, so no claim here rests on frame hashes being
stable across runs.

`redactions.tsv` (135 B) is worth one line: it records the masking the replay
route performs upstream of the surface — 3 rows, 36/32/32 runes, matching a
session id and **this task id** (`01a1003a7e747deeaec251aeb71bff18`) twice. So
the filmed surface never carries the run's own identity.

## 4. A second, harder session — a tool call in the film

The one-turn session above never dispatches a tool, so the film had no tool beat.
The same command against a recorded session that *does* call a tool
(`5f223312-…`, one `bash` call + result):

```bash
./bin/gopi chat --replay 5f223312-55c3-43ed-a00f-2c3418f395cc --film $E/film-tools
[gopi film] film written to …/evidence/film-tools: 17 frame(s) from 17 ledger row(s), 17 frame(s) reached the settled criterion, 0 did not
```

17 frames, **17 distinct md5**, every one `settled=yes`, and `beats.tsv` carries
both kinds this epic cares about (`cut -f1,2,3,5`):

```
12  tool_call  ⚙ call bash echo "4"
13  answer     4  [TASK_LIST_BEGIN] [{
16  chrome     turn end
17  request    /quit
```

So a tool beat, an answer beat and the closing chrome all reach the film.

## 5. Same frequency — a geometry flag reaches the producer

The driver's own geometry flags must be the frequency the producer draws at, not
a re-picked default:

```bash
./bin/gopi chat --replay 758507fd-… --cols 100 --rows 30 --film $E/film-samefreq
[gopi film] terminal window 2097164 ("gopi film film-samefreq") area 604x394+0+0
[gopi film] film written to …/evidence/film-samefreq: 6 frame(s) … 0 did not
```

The requested geometry appears in the terminal, in the producer's argv **and** in
the frames: IHDR `0x25c × 0x18a` = **604×394 px** (vs 706×342 at 117×26). A
producer refusing a terminal that disagrees with the layout would have failed the
run; it did not.

Two flag combinations are refused **by name**, both measured on this base:

```bash
./bin/gopi chat --replay 758507fd-… --resume 758507fd-…     → rc=2
  gopi chat: --replay re-runs a recorded session ("758507fd-…") and --resume
  continues one ("758507fd-…") — a replay continues nothing
./bin/gopi chat --film $E/film-noreplay                     → rc=2, no directory created
  gopi chat: --film films a REPLAYED session and no --replay was given — write
  --replay SESSION --film DIR
```

The refusals are the feature that keeps "one route" honest.

## 6. The dependency surface — a PATH shim, with a control

**Method.** A PATH containing *only* a shim directory. Real `Xvfb` and `xterm`
are symlinked into it (they are the two the epic permits); every name that used to
carry the pipeline — `python3`, `python`, `xdotool`, `xwininfo`, `xprop`, `xwd`,
`convert`, `magick`, `import`, `scrot`, `ffmpeg`, `wmctrl`, `xdpyinfo`,
`gnome-screenshot` — is a script that appends its own name to a witness file and
`exit 127`.

**Temporal order matters, so it was fixed: the real run FIRST, the controls
after.**

```bash
PATH=$P/bin ./bin/gopi chat --replay 758507fd-… --film $E/film-shim2
  → rc=0, "film written to …/film-shim2: 6 frame(s) from 6 ledger row(s), 6 … 0 did not"
  → witness files=0  bytes=0            # ← the zero, measured AFTER the real run
PATH=$P/bin python3 -c 'print(1)'   → rc=127
PATH=$P/bin xdotool --version       → rc=127
PATH=$P/bin xwininfo -root          → rc=127
PATH=$P/bin convert -version        → rc=127
PATH=$P/bin import -window root /dev/null → rc=127
  → witness files=1  bytes=40   ("python3 xdotool xwininfo convert import", one name per line)
```

Same shim directory, same PATH value, two observations in one time order: a real
film run that calls none of them, and hand-made calls that all land and are
recorded. `python3`/`xdotool`/`xwininfo`/`xprop`/ImageMagick were **never
executed** by the film path — not merely "not needed this time".

The two external programs the run *does* still need are named, not hidden:
`internal/shot/displayalloc.go` (`displayServerDefault = "Xvfb"`, probed with a
real invocation before use) and `internal/shot/terminalterm.go`
(`FilmTerminalBinary = "xterm"`). The X11 client itself is gopi's own pure-Go
`GetImage` (the same client `gopi capture` uses).

### 6b. What the removed pipeline was, and what replaced it

Measured on this base, with the epic's own range:

| range | shortstat |
|---|---|
| `c8e71550` → `02c1e523` (the DRIVER commit, master's own) | 6 files changed, +2003 / −2 |
| `c8e71550` → `b191a55d` (the driver merged) | 51 files changed, +2866 / −8749 |
| `c8e71550` → `master` | 85 files changed, +6691 / −8779 |

Deleted by `971400a7`, lines counted from the pre-image:
`internal/replay/replay.go` **1176**, `cmd/agent/cli_replay.go` **51**,
`tools/replay/verify.py` **1609**, `tools/termemu/termemu.py` **1395**,
`scripts/replay/replay.sh` **713** (plus their siblings; 37 files, +188 / −8684 in
that one commit). `go.mod`/`go.sum` are **not** in the diff — the whole port is
stdlib plus in-repo packages (Rule 35).

**Who owns what now** (this is the correction §10 has to make). Master's shape is:

| surface | carried by |
|---|---|
| the replay route | `cmd/agent/chat_replay.go` — re-runs the REAL chat surface |
| the model, taken out of the re-run's hands | `internal/replaydrive` (a SCRIPT + an **in-process** Claude-SSE stub on `127.0.0.1:0`, loopback only) |
| the beat ledger | `cmd/agent/chat_replay_beats.go` |
| the film driver | `cmd/agent/chat_film.go` (578 lines) |
| display + terminal | `internal/shot/displayalloc.go`, `internal/shot/terminalterm.go` |
| what is left of the old package | `internal/replay/{ledger,redact,session_ground}.go` + tests — vocabulary the live route imports, **not** a renderer |

## 7. Blocked criteria (honest)

### B1 — the film package the epic promised is not what `--film` writes

Epic item 3 requires the output directory to be a self-contained film: frames +
GIF + metadata, with `verify.py`'s five criteria ported to Go. **`gopi chat --film`
does none of that on this base.** Measured here:

```bash
grep -c 'gifgen\|film.gif' cmd/agent/chat_film.go                  → 0
git grep -n 'gopi/internal/film' master                            → (no hits)
git grep -l 'CheckTraceability' master -- '*.go'                   → (no hits; likewise the other four)
```

`internal/film` (item 3's package) does not exist on master and **nothing imports
it**, so the GIF composition and the metadata cannot appear in any output this run
produced. **This run does not claim them.**

**How that tip is named, and why the instrument matters.** The branch
`task/01a0ffbba7457cb7a1b43db95617ea8e` **no longer exists** — `git rev-parse
--verify --quiet refs/heads/task/01a0ffbba7457cb7a1b43db95617ea8e` → exit **1**
— while the commit **is** reachable by tag:
`refs/tags/rework-backup/task-01a0ffbba7457cb7a1b43db95617ea8e` → `0b516f5f`
(exit 0). So for a deleted branch the correct instrument is its `rework-backup/*`
tag: an ancestor check against a non-existent ref exits **128** ("not a valid
object name"), which is not the same statement as "not merged".

### B2 — no route from a `--film` outdir to the five criteria

Even once `internal/film` lands, there is no verb or flag that runs
`CheckTraceability` / `CheckLegibility` / `CheckMotion` / `CheckWithholding` /
`CheckColour` against an outdir — those symbols have **zero** hits in master's Go
tree today (§7/B1), and `gopi --help` advertises nothing film-shaped.

Consequence for §3: the assertions this run *can* make about the frames are
pixel-level **structural** ones — count, distinctness, geometry, settled
criterion — plus a qualitative vision read. A **content** verdict (traceability /
legibility / motion / colour) is exactly the class of judgement those ports exist
to make, and this run does not make it.

### B3 — a caller cannot arm `--dump-screen` during a film run

The driver builds the producer's argv itself (`chat_film.go`: `--replay`,
`--scratch`, `--cols`, `--rows`), so a caller's `--dump-screen` is not forwarded.
Measured:

```bash
./bin/gopi chat --replay 758507fd-… --dump-screen=$E/screen-b3.dump --film $E/film-dump
→ exit 0, film written (6 frames), but $E/screen-b3.dump does not exist
```

The producer's dump is nevertheless written — **default ON**, into the
*producer's own* session directory, which the run's own `producer.log` names:

```
gopi chat: screen dump → /home/user/.gopi/sessions/faec40b1-730b-4eb1-875b-801f083c40d0/chat-screen.dump (plain text; this surface's own interpreted state — no pixels)
#gopi-chat-dump-report path=…/chat-screen.dump bytes=11605 pid=667390 grid=117x26 geometry=layout dumps=25 frames=144 blind_spots=8
```

So the observation exists; what is missing is the caller's ability to **choose**
its destination while filming.

### B4 — the 01a0ff97 residual stays open, and the film cannot close it

`01a0ff97-0caa-7c9e-982e-b5a7015718a3` (dump lost the startup screen; DCS parsed
as text) is **completed**, and its fix is visible in this run's artifact: the
producer's own dump carries 11605 bytes over 25 dumps / 144 frames, and frame 1 of
§3 shows the banner rows. What 01a0ff97 explicitly declined to decide is quoted
from its own report (`~/.gopi/sessions/01a0ff97-0caa-7c9e-982e-b5a7015718a3/task-report.md`,
line 54 onward — re-read this run):

> - **The moment the startup screen enters the scrollback is still decided by the
>   band's row budget** (the live row sits at the bottom of the region). Changing
>   the layout is outside this task's boundary (no new drawing layer) and was not
>   done. The startup screen is now *present in the artifact* on both paths;
>   *which segment* carries it depends on the frame …

**Blocked, with that reason**: any claim of the form "the film shows the startup
screen *in history*" depends on that open layout question. Neither a dump nor a
frame can recover a screen that was overwritten rather than scrolled into history.
This run asserts only that the startup screen is **captured** (dump) and **shown**
(frame 1, painted by the signed layout) — not that it entered scrollback.

### B5 — the settle predicate, measured rather than assumed

The honest shape of this evidence is not "always two grabs". Across the seven
`--film` output directories this run produced, `frames.tsv` reads:

```
2/0 × 52 frames        3/1 × 1 frame (film-tools, frame 16)
```

so the re-grab really does happen sometimes — a frame that leaves `stale=1` and is
still declared settled is the predicate doing its job (it looked again, found the
screen changed, looked once more). It is why the driver's own summary says
"reached the settled criterion" rather than "was identical twice". The display
number is *derived*, never spelled (`display :63 … server pid`, from what
`/tmp/.X11-unix` holds).

### B6 — the branch-scoped suite, and the one red the previous edition saw

The deleted branch's edition reported `cmd/agent` FAIL on `TestServeLockReplacementMechanism`
in-suite and green alone, on a two-file `devdoc/` diff. Re-measured here:

```bash
go test ./cmd/agent/ -run 'TestServeLockReplacementMechanism' -count=1
→ ok  github.com/nick/gopi/cmd/agent  1.818s
```

It is **not** in `testdata/flakes.tsv` (grep: no rows). This run therefore does not
claim to have reproduced a red, and does not claim the test is a flake: one
observation per state, on this base.

### B7 — what this document does NOT re-derive

- The **verb-census gate** was run (§2, PASS) — the previous edition left it out;
  this one closes that boundary. The **help-surface group count** (§2) comes from
  the script, not a gate reading, which is the script's own contract.
- Items 1–3 of the epic are `completed` on their own rows, but the film *package*
  is not in master (B1/B2), so the epic is **not** closed by this document;
  `devdoc/chat-film-replay-driver.md` and `devdoc/replay-abolished-verb-single-route.md`
  are master's own normative/historical pair on the same ground and are the
  authority this document defers to.

## 8. Design notes and pitfalls worth keeping (Rule 7)

- **Why a flag and not a verb.** The producer is the same binary, and the surface
  that replays is `chat`; a `gopi film` verb would be a second answer to "how do
  I replay" and would have grown the verb list the epic requires to shrink. §2's
  45 → 44 is the measurable consequence.
- **Why `Xvfb`/`xterm` survive and nothing else does.** A film is a photograph of
  a real terminal on a real X display; Xvfb is the display and xterm is the
  terminal, but *window* and *pixel* handling are gopi's own code (GetImage,
  `_NET_WM_PID` lookup, the settle predicate). §6 shows the difference between
  "the script had to be there" and "these two binaries are the hardware".
- **Pitfall: the replay operand is a session id, not a task id.** Re-measured on
  this base: `./bin/gopi chat --replay 01a1003a-7e74-7dee-aec2-51aeb71bff18`
  (this run's own id, SessionID-shaped) exits **0 after 0 beats** —
  `the filmed terminal is gone (exited with status 0) and no completion marker was
  written — the film ends here with 0 beat(s)` … `no frame was taken`. A
  misidentification wearing the right syntax; the operand must come from the
  session's own name.
- **Pitfall: `--film` outdir must be fresh-ish.** `CheckFilmScratch` runs before a
  display is started (an unwritable output fails without launching Xvfb), and the
  driver's scratch is allocated under the pinned suite scratch root, class `shot`
  — off `/`, never a bare `/tmp` (Rule 22).
- **The driver re-executes the running binary** (`os.Executable()`), so the binary
  under test is the binary that produced the frames — the acceptance is genuinely
  end-to-end and not a library call.
- **Exit status is the driver's**: `0` on a complete film, `1` on a preflight or
  protocol failure, `2` on a usage error (both refusals in §5 returned 2).
- **Precision on what "the replay pipeline was deleted" means.** The *renderer
  file* and the *script directories* are gone — `internal/replay/replay.go`,
  `cmd/agent/cli_replay.go`, `scripts/replay/`, `tools/replay/`, `tools/termemu/`
  — while the **package `internal/replay` still exists** (three live files, §6b)
  because the beat ledger, its redaction and the session ground are what the route
  imports. The wording "the directory was deleted" would be false; the wording
  "its renderer and the film scripts were deleted" is the measured one.
- **The producer's own log still speaks `[gopi replay]`** (`[gopi replay] re-running
  session …`, `[gopi replay] turn 1/1: typing the recorded prompt`). That is the
  flag's spelling surviving inside the surface's messages, not a second verb.

## 9. What this document is not

It is not a claim that the epic's five sub-items are all merged. Items 1 and 2 are
in this branch's base (`02c1e523` — the driver — is an **ancestor** of `HEAD`,
verified with `git merge-base --is-ancestor`); item 3's package is not, and it is
reachable only by its `rework-backup/*` tag (§7/B1). B1–B3 are exactly the items
that gap explains and are the ones to dispatch next; B7 is this run's own
boundary.

## 10. Reconciliation with master's takeover (the re-authored part)

The deleted branch's edition described the takeover in the **opposite ownership
direction** from what master landed. Corrected here, claim by claim:

| the old edition asserted | the measured state on master `429ac774` |
|---|---|
| the deleted `internal/replay/replay.go` was "the self-drawn renderer", replaced by `internal/shot/*` **containing the replay route** | the renderer is indeed gone, but the route comes from **`internal/replaydrive`** (script + in-process Claude-SSE stub) + `cmd/agent/chat_replay.go`; `internal/shot` owns only the **display and the terminal**; the beat protocol lives in `cmd/agent/chat_replay_beats.go` |
| the driver = `internal/shot/displayalloc.go` (+529) and `terminalterm.go` (+445) "replace the parts that were genuinely needed" | those two files are real, but the driver commit is `02c1e523` — **6 files, +2003 / −2** — and `internal/replay` survives as three live files, not as a directory to be removed |
| the epic's diff was "51 files, +2866 / −8749" | that is `c8e71550 → b191a55d` (driver merged); the wording that commit removed is the driver commit's own `+2003/−2`, and the pure ablation is `971400a7`'s 37 files, +188 / −8684 |
| chat help carried 13 flags / group 39 (from `44303fbb`), and 14/40 at the base of this edition | this base derives **21** flags for `chat` and **47** rows for the group (`scripts/g5-chat-web-help-surface.sh`), because the beat protocol's `--scratch`/`--cols`/`--rows` were accepted by the parser and named nowhere on the page until task `01a103e4` documented them |
| the verb-census gate was left unrun (B7 of the old edition) | it was **run** here: `TestVerbCensusMatchesTheDispatch` PASS (§2) |
| the branch's base was `b191a55d` | this edition is authored on **`429ac774`**, and every number in §§3–7 was re-taken there |

What did **not** change, and is why this document is worth keeping rather than
deleting: the acceptance itself. The same command, the same beat protocol, the
same six/seven-frame production metadata, the same zero-dependency witness, the
same refusals, and the same honest block set (a film is frames, not a GIF; the
five criteria have no route from an outdir; a caller cannot choose the dump
destination; the startup-screen-in-scrollback question is open).
