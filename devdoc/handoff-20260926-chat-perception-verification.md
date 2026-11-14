<!-- doc-class: historical -->
# Handoff — the chat perception loop: what to VERIFY after the queue drains and a new binary is built

Date: 2026-09-26 (night, +08:00)
Repo: `/home/user/work/GoPi` @ master `56870d4c`
Author: a gopi chat session (`a5e1c3e9-fcfb-450c-a0a8-abcea5688f25`)
Purpose: **a verification checklist for my FUTURE SELF**, to be run once the tasks below
complete and a fresh `bin/gopi` is built. This doc is not a report of work done — it is a
set of falsifiable checks, each with the exact command and the expected observation, so the
next session can tell whether the perception loop actually closed.

> **Read this first:** §0 says WHY this matters and what "verify" means here. Then run §2 in
> order. §1 lists the tasks whose completion arms each check. §3 records what is ALREADY
> true and must not be rebuilt. §4 is the honest list of what I could NOT establish.

---

## 0. THE ONE IDEA

**The dump is not my EYE, it is my MEMORY.** (Operator's framing, verbatim: *"it is not just
for display(eye), it is accuracy(brain)"*.) A display error can be re-looked at; **a memory
error cannot be noticed** — every later inference rests on a false premise. So the fidelity of
the self-observation channel is a **correctness property**, not a UX preference.

Corollary: **I currently cannot verify ANY of my own output.** Four independent links in my
self-observation chain are broken or absent (measured this session, §4). That is why the
deliverable of a fix is never "the code changed" — it is **"the agent can now notice"**.

**What "verify" means in this doc:** every check below is an *observation* — a command whose
output either matches the promise or does not. Never "the test is green"; always "the binary,
run this way, showed this".

---

## 1. THE TASKS WHOSE COMPLETION ARMS EACH CHECK

Run `gopi task show <id>` for each before starting — the payload is the real spec, and a task
may have been superseded (I watched one of mine get cancelled mid-session; check status).

| task_id | what it must deliver | arms check |
|---|---|---|
| `01a0ddb3-f47a-7803-99b1-31df5c66ad51` | the eye reads a PAINTED screen: dump no longer stops at `frames=3 dumps=1`; an unpainted/fallback grid is LOUD, not reported as `state=live` blank | §2.1 |
| `01a0ddac-61f6-7970-8fba-c2352bcf6ca8` | independent re-verification of the three-channel verdict (display faithful vs copy lossy) | §2.2 |
| `01a0ddcd-2b52-771f-8fea-2c3dc42b5b56` | **my own bug** — `gopi log --response --text` loses text (4727 bytes vs 3831 authoritative chars) | §2.3 |
| `01a0ddab-af90-799a-85b2-2d2bf3dc9771` | the UNEVEN row damage: a table row lost its head while its neighbour was whole | §2.4 |
| `01a0ddaa-c70d-7caa-bb84-bca5ef0208d1` | unclosed fence → `StyleFor` flips `inFence` permanently → `**bold**` prints literally | §2.5 |
| `01a0ddbd-fb9f-7b53-aa88-194054d5ec10` | `renderSpanLine`'s plain-text fallback on a bookkeeping mismatch is OBSERVABLE (Rule 15) | §2.6 |
| `01a0ddbd-9e2b-78fc-bddf-dcf19af35e67` | the agent's self-knowledge NAMES the console eye and says what it is evidence FOR | §2.7 |
| `01a0dd6f-76dc-74ce-9de1-defc8ea2da5c` | self-observation loop ARMING (rework of an archived diff) | §2.1 prerequisite |
| language option (env/config/CLI, three-in-one) | output-language only, as a small optional head section — **NOT a SOUL.md-style persona file** (my original proposal was cancelled by operator ruling) | §2.8 |

**Order matters.** §2.1 is first because every other check uses the eye. If §2.1 fails, the
rest are unprovable from inside — stop and fix the eye.

---

## 2. THE CHECKS

### 2.1 — Can I see a screen that has been PAINTED? (the foundational check)

```bash
# Drive a REAL pty at a KNOWN size, arm the dump, send one prompt, read it back.
# (Use a python pty.fork harness at a fixed 117x26 — the size MUST be known, not assumed.)
gopi chat --tools --dump-screen --cols 117 --rows 26
# after the answer has painted:
gopi ui_state --target chat --pid <pid>
```

**PASS (all three must hold):**
1. The header's `grid=` equals the pty's `stty size` (117x26) — **not** `80x24`, **not** `0x0`.
2. `frames=` is large (the sibling measured `687`, a good chat) — **not** `3`. `dumps=1` is a
   FAIL even if the grid is non-blank.
3. The grid contains the answer's text (not 26 blank rows, `cursor row=0 col=0`).

**FAIL signature to name explicitly:** a grid that is all-blank but reported `state=live` with
**no** flag saying "this was never painted" or "this is a fallback geometry". §0's memory-error
failure mode. `598aa37a` already handles `frames=0`; the gap is `frames=3` + all-blank.

**Also check the arming gap** (handoff §2c, still open at time of writing): a surface started
WITHOUT `--dump-screen` produces no dump at all, and `ui_state` then cannot see it. Decide
whether the fix makes the flag unnecessary, or makes the absence loud.

### 2.2 — Is the "display is faithful, only the copy is lossy" verdict TRUE?

**This is the check I most want answered, because I produced evidence that may falsify it.**
The sibling (`01a0dd26`) claims A=B≠C: transcript = painted screen ≠ mouse copy. That puts the
fault in the terminal's selection.

```bash
# Channel A — the authoritative transcript
gopi log <id> --response --text
# Channel D — MY replay path (NEW; the sibling never tested it)
gopi log <id> --response --text | wc -c
python3 -  # sum assistant text-block chars in ~/.gopi/sessions/<id>/session.jsonl
```

**PASS:** A == D (the replay reproduces the transcript exactly, any wrapper accounted for).
**FAIL (observed 2026-09-26):** A = 44 lines / 4727 bytes, D-source = 3 blocks / 3831 chars,
and the replay **truncates a Chinese paragraph mid-clause**. If that still holds, the
"only the mouse copy loses text" story is INCOMPLETE — there is a loss layer inside gopi.

**Deliverable of this check:** a written verdict, with the line-by-line diff of the first N
lines, stating the SHAPE of the loss (head / mid-line / tail).

### 2.3 — Does `--json` round-trip when `--text` does not?

A cheap discriminator for §2.2's layer question:
```bash
gopi log <id> --json | wc -c     # raw JSONL — expect byte-exact vs session.jsonl
gopi log <id> --response --text | wc -c
```
If `--json` is exact and `--text` is short, the loss is in the **TEXT renderer**, not the store.
Also re-run `--text` at two explicit widths — if the byte count moves with width, the loss is
wrap-related (the `chat_flow_wrap` / band machinery is shared).

### 2.4 — Is the UNEVEN damage reproducible, and on which row class?

The operator's second sighting (task `01a0ddab-af90`): in ONE block at ONE width, a markdown
table row lost its head while the source line beside it was whole. **A uniform width overflow
cannot explain that.**

```bash
# Feed a known answer containing a table + a neighbouring prose line through a real pty
# at a width where the table row is over-budget, then compare grid vs transcript.
gopi ui_state --compare "<the table row>" --session <id>
gopi ui_state --compare "<the neighbour line>" --session <id>
```

**PASS:** both lines `match`, or both reported identically.
**FAIL:** the compare returns `mismatch` for one and `match` for the other. Then name **what
distinguishes the damaged line** — its leading `▌ ▌` gutter, a re-cut applied per row class, or
the band's shrink carry (memory #6003: a shrinking band gives rows back from below and the
editing line draws on the first row it gave up, erasing a committed row). **Do not conclude
"the terminal wrapped it" until you have shown why the neighbour was spared.**

### 2.5 — Does `**bold**` survive an unclosed fence?

Root cause (measured, task `01a0ddaa`): `internal/lineedit/compose.go:86`
`StyleFor` does `if isFence(trimmed) { c.inFence = !c.inFence }` — **unconditional**, and
`isFence` is just `HasPrefix(trimmed, "```")`. ONE unpaired fence flips the state for the rest
of the answer → every following prose line is `LineCode` → `AnswerRenderer.rowFor`
(`answer.go:1267`) gates on `ls != LinePlain` and drops the emphasis → literal `**`.

```bash
# In a real chat, send a message whose answer contains an UNPAIRED ``` then a bold line.
# Read the grid back.
gopi chat --tools --dump-screen
```

**PASS:** the bold line renders as emphasis (markers absent) AND the grid confirms it.
**MUST NOT REGRESS:** inside a REAL fence, `**` stays literal; `palette_test.go:385`'s
classification table still passes. **Do NOT touch `emphasis.go`** — the emphasis layer is
correct (`TestStyledLineMarksExactlyTheBoldCells` PASS); the bug is one level above, the CALL
CONDITION. Verify on BOTH `gopi chat` and `gopi ui` (`internal/lineedit` is shared;
the ui path is `uiclient/model.go:860`).

### 2.6 — Is the formatting fallback OBSERVABLE?

Task `01a0ddbd-fb9f`: `renderSpanLine` falls back to plain text on a bookkeeping mismatch and
**nothing reports it**. That is a silent fidelity drop in the very channel §0 calls my memory.

**PASS:** force the mismatch and observe a reported degradation (stderr/artifact marker/exit
code) — not a silently-plain line. The check is: **can I tell that formatting was dropped?**
If the answer is "only by looking closely", the check FAILS.

### 2.7 — Do I KNOW I can see (self-knowledge)?

Task `01a0ddbd-9e2b`: the machinery already exposes per-cell-range style
(`AttrRun style=fg=rgb:fff8dc bold`) but the self-knowledge section names only `gopi log`.

**PASS:** ask the fresh binary a question about its own screen and see if it reaches for
`ui_state`/`--dump-screen`/`--compare` **unprompted**. The observable is a real transcript
where the agent used the eye, not a claim in the prompt that it could.

### 2.8 — Does the output language follow the setting, not the input?

Operator wants Chinese replies even from English input (he types English because it is faster).
**Ruling that supersedes my first design:** NO new persona-file mechanism and the framework is
not touched. The accepted shape is a **three-in-one (env var / config file / CLI flag)
OUTPUT-LANGUAGE option**, carried as a small optional section at the head of the system prompt,
affecting **response language only** — internal prompt and reasoning untouched.

```bash
# all-English prompt, language set to Chinese:
gopi chat -p "what tools do you have?"          # + the language flag/env/config
gopi chat --tools                                # the OTHER path — separately wired!
```

**PASS:** Chinese reply on BOTH `gopi chat` and `gopi chat --tools` (they are separately wired:
`loop.go:4266` vs `chat_ops.go:502` — one fixed and the other missed is a FAIL).
**NEGATIVE CONTROL:** with the setting absent/empty, the same English prompt answers in English
— proving the setting is the cause, not luck.
**MUST NOT:** hardcode Chinese as gopi's default for all users; an English-only operator must
keep working.

---

## 3. ALREADY TRUE — DO NOT REBUILD

- **Read my own output from `gopi log <id> --response` / `session.jsonl`, never a screenshot.**
  The operator's mouse copy (`/tmp/dump_text.txt`, 4945 B, 89 lines) is **corrupted** — it has
  NO `#gopi-*-dump` header (grepped, 0 hits), so it is a SCREEN COPY, not a dump artifact.
  Treating it as one wastes time.
- **The instruments exist**: `gopi chat --dump-screen[=PATH]`, `gopi ui_state
  [--pid|--target|--json|--list]`, `gopi ui_state --compare "<line>" --session <id>` (exit 3 =
  the two sides disagree; **this is the verb that separates a wrap artifact from a real loss** —
  "in the log, not on the grid" = re-wrap, "in neither" = a loss).
- **Use `--compare`, not a hand-rolled diff.** It exists precisely for this, and it prints its
  blind spots with every answer.
- **`--dump-screen` on `gopi ui_state`/`--compare` vs the transcript**: a dump re-interpreted at
  the wrong width **fabricates** corruption (§3c of the earlier handoff: `terminal=0x0`,
  `grid=80x24` while the pty was 117x26). Check the header's geometry BEFORE believing a
  mismatch.
- **The emphasis layer is correct**; `internal/sgr` already supports the 256-colour tier
  (`ColorModePalette=5`, `Palette256`) and `internal/lineedit` never referenced it (measured).
- **The palette is three-tiered, not binary**: truecolor (`COLORTERM=truecolor`) → 24-bit;
  `TERM=*256color*` → `38;5;n`; else 16-colour. The middle tier is the one whose absence makes
  the default look like a stock console.

---

## 4. WHAT I COULD **NOT** ESTABLISH (be suspicious of anyone who claims otherwise)

Measured this session, in order of how much they hurt:

1. **I cannot perceive my own dropped characters — and I proved it, not just suspected it.**
   My session's replay (`--response --text`) disagrees with the authoritative JSONL (§2.2). So
   the channel I would use to check my own output is itself lossy. **I am inside an illusion I
   cannot detect**; there is no internal signal that tells me the text I am reading is
   truncated.
2. **My own surface has NO dump at all.** `gopi ui_state --list` showed 12 chat surfaces, all
   belonging to earlier PA/worker runs — **none is mine**, because my process was started
   without `--dump-screen`. So even the blank-grid failure (`frames=3 dumps=1`) is more
   generous than my situation.
3. **The one verb that could settle a loss refused to run for me.** `ui_state --compare`
   demanded `--pid` because 12 exited surfaces matched, and my own had no dump to point at:
   `12 exited gopi surfaces could be the target; refusing to guess`. The refusal is correct
   (never guess a screen) — but the consequence is that on this box **the self-check is
   unreachable by default**.
4. **Which layer loses the text is UNKNOWN.** The sibling says the copy; I found a second
   candidate in gopi's own replay; §2.4's uneven damage suggests a third in the band's re-cut.
   **Nobody has yet proven it.** Do not assert a cause before running §2.2–§2.4.
5. **Zombie surfaces confuse the instruments.** 12 exited chat dumps make every `--pid`-less
   call ambiguous. Whether the fix should prune/age dumps is an open design question — I did
   not settle it.

---

## 5. IF YOU RESUME HERE

1. `git log -1 --format='%h %ci %s'` — confirm the HEAD you are verifying. This doc was
   written at `56870d4c`.
2. `gopi task show <id>` for every id in §1 — **check status first**; a task may be cancelled
   or superseded (mine was, by an operator ruling that changed the design).
3. Build a fresh binary and **rebuild duty**: compare `bin/gopi`'s embedded revision against
   `git rev-parse --short master` (equal ⇒ drained). Note: **`bin/gopi revision` BLOCKED for
   >2 min** in a bash chain this session — do not chain it; run it alone or use the binary's
   own `--version`.
4. Run §2.1 FIRST. If the eye still cannot see a painted screen, every later check is
   unprovable — fix the eye and re-run, do not "work around" it.
5. Write down every check's outcome in the shape `Reproduced: <cmd> → <output>`, including the
   FAILURES. A failed check with a quoted observation is worth more than a passing claim: this
   whole campaign exists because someone believed a screen that was lying.
