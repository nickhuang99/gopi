<!-- doc-class: historical -->
# Game-Agent Testbed — Design + Gap Analysis

**Date**: 2026-09-11
**Author**: gopi analyst session (design-only; NO implementation)
**Branch**: `feat/game-agent-testbed-design`
**Scope constraint (from nick)**: 「我们不是一个公司，资源是很有限的」 — any proposal that grows into
a platform/company is REJECTED. Everything below is sized for one operator with one machine.

**Evidence base** (all cited inline; nothing from memory):

| Ref | Artifact |
|---|---|
| **M1R** | `devdoc/evidence/homm3-m1-llm-full-game-20260907.md` (verbatim copy of `/home/user/Downloads/games/Maps/devdoc/homm3-m1-llm-full-game-20260907.md`) |
| **M1P** | `devdoc/evidence/homm3-m1-progress/progress.md` (verbatim copy of `.../Maps/devdoc/evidence/homm3-m1/progress.md`) |
| **M1S** | session log `/home/user/.gopi/sessions/t-1788720718/a454aba5-455f-400a-ba87-eaeb5ea5972d.jsonl` (5.3 MB, 9414 records, 2026-09-07T04:14:51→12:16:09 +08:00 = 8.02 h) |
| **PRICE** | `devdoc/deepseek-v4.1-flash-model-naming-and-kv-architecture-20260910.md` lines 44–58 |
| **SRC** | gopi source at commit `6a316b7` (main repo master), file:line as shown |
| **MEM** | distilled records #1677 / #1678 (webcam closed-loop evaluation) and #223 / #224 (VCMI LLM playability) — used as *prior measurement*, labelled as such, never as the primary evidence |

---

## A. The M1 "hero-localization blocker" is an OPEN-LOOP failure, not a vision failure

### A.1 What M1 actually shows

M1R reports the second game as: 「深陷"英雄视觉定位"：PIL 图像处理/minimap 分析/红蓝 sprite 签名
反复尝试定位英雄——vision 无法可靠从 1280×800 全图上识别小英雄标记」 and concludes
「真瓶颈=视觉定位非游戏逻辑」, with the next-step candidates listed as ① template matching
② local zoom then vision ③ programmatic sprite signature — i.e. M1R **already proposed the
closed-loop remedies** but framed the finding as "vision can't see the hero".

The session log says something sharper. Over 8.02 h M1S contains:

| Quantity | Count | How measured |
|---|---|---|
| rounds (`request` records) | **2342** | top-level `type` tally — matches M1R's 2342 轮 |
| tool calls (`tool_result` / `tool_use`) | **2370** | 2370 `tool_result` records; `tool_use` blocks sum to exactly 2370 |
| — of which `bash` | 1851 | tool_name tally || — of which `vision_analyze` | 464 | tool_name tally |
| — of which `job_start`/`job_output` | 11 / 4 | tool_name tally |
| assistant output records (streaming fragments) | 4667 | `type=assistant` tally |
| distinct `vision_analyze` prompt prefixes | **443** (of 464 prompt args) | prompt-arg tally |
| **bash commands containing `convert`** | **1357 of 1851 (73 %)** | per-command tally over the 1851 `bash` tool_use inputs |
| bash commands containing `import -window` | 567 | same |
| bash commands containing `xdotool` | 507 | same |
| bash commands containing `DISPLAY=` | 709 | same |
| bash commands containing `sleep` | 613 | same |
| distinct bash commands | 1851 (100 %) | every bash call was hand-written |

Reproduce (all of the above, one pass):
```bash
python3 - <<'EOF'
import json,collections,re
p="/home/user/.gopi/sessions/t-1788720718/a454aba5-455f-400a-ba87-eaeb5ea5972d.jsonl"
types=collections.Counter(); tools=collections.Counter(); cmds=[]; pr=collections.Counter()
for line in open(p,encoding='utf-8',errors='ignore'):
    try: o=json.loads(line)
    except Exception: continue
    types[o.get('type')]+=1
    m=o.get('message',{})
    if m.get('role')=='assistant':
        for c in m.get('content',[]) or []:
            if c.get('type')=='tool_use':
                tools[c.get('name')]+=1
                if c.get('name')=='bash': cmds.append(((c.get('input') or {}).get('command','')).strip())
    for mm in re.finditer(r'"prompt":\s*"((?:[^"\\]|\\.){0,400})"', line): pr[mm.group(1)[:80]]+=1
print(dict(types)); print(dict(tools)); print("distinct cmds",len(set(cmds)))
for pat in ['convert','import -window','xdotool','DISPLAY=','sleep']:
    print(pat, sum(1 for c in cmds if pat in c))
print("distinct vision prompts", len(pr), "total", sum(pr.values()))
EOF
# → {'session_start':1,'user':33,'assistant':4667,'request':2342,'tool_result':2370,'session_end':1}
# → {'bash':1851,'vision_analyze':464,'job_start':11,'job_output':4,'read':3,'write':4,
#    'edit':3,'keyword_search':1,'memory_search':6,'memory_store':2,'memory_feedback':3,
#    'context_trim':1,'write_task_area':15,'todo':2}   (sum 2370)
# → distinct cmds 1851 | convert 1357, import -window 567, xdotool 507, DISPLAY= 709, sleep 613
# → distinct vision prompts 443 total 464
```

**443 distinct vision prompts out of 464 calls, and 73 % of all bash calls contained
ImageMagick `convert`.** The model was not repeating one question —
it was asking 443 *different* questions ("读出底部信息条文字。", "这是什么图标按钮？",
"描述这张屏幕截图：…是否已到主菜单？", …) against a 1280×800 frame, and then acting on the
answer with no way to tell whether the answer was right. **Every one of the 1851 bash commands
was unique** — nothing was reused; the loop re-derived its own capture/actuation plumbing
thousands of times. This is the signature of an **open-loop** loop: perceive → guess absolute
target → act → assume success → perceive again from scratch.

M1R's own technical-lesson line is a paraphrase of the same thing: 「vision 定位大场景小目标 =
核心难点（英雄 vs 全图）」. The hero occupies a few dozen pixels in a 1280×800 frame
(≈1.02 M px); a 24×24 hero sprite is 0.06 % of the frame. **Nothing in the pipeline ever
narrowed the search region, and nothing ever verified an action's outcome.**

### A.2 Why this is not a capability wall — the same failure was measured and retracted next door

`devdoc/webcam-capture-rpi5-agent-evaluation.md` (§1, commit `587624f`) had already run the
controlled version of this experiment (distilled as MEM #1677/#1678):

- **Open-loop absolute-coordinate aiming is structurally dead.** One-shot "1 HID count = 1 px"
  clicking: **0/4 hits**, true error 84–770 screen px, while the agent *believed* it was 5–8 px
  off. It does not know it is wrong — so no improvement in camera quality, resolution or model
  choice repairs it.
- **Vision as coordinate source, measured**: median **154.8** screen px error on a full-view
  640×360 photo (max 2668.6, 2/5 answers pointing at unrelated image areas). A 3× zoomed ROI
  drops it to ~16 px — better, still not aimable.
- **Closed loop, measured**: deterministic quad-lock + homography + re-apply through a fresh
  H each iteration → **0.0–2.2 screen px** target error; deterministic ROI grey-diff arrival
  detector, self-calibrating null from two same-state frames → **7.9× separation** on a real
  GUI state change; command transfer measured *during* the loop because the pointer is
  non-linear (24 counts→120.5 px = 5.0 px/count vs 2 counts→2 px = 1.0 px/count, a **5×**
  non-linearity).
- **The LLM's proper role**: pick *which region* to look at, and judge *did the expected change
  happen* — never emit a coordinate.

So the M1 blocker decomposes as: **capability is present and sufficient; the LOOP is missing.**
The vision model is a legitimate weak localiser (~155 px open-loop, ~16 px zoomed) and a fine
*verifier*. M1 asked it to be a 1-px-precision locator, 464 times, with no feedback channel —
23 % of all tool calls spent on a question the architecture could never answer.

### A.3 The gopi-side corroborating fact: the loop has no repetition guard that can see this

`internal/agent/loop.go` has exactly two "you are stuck" detectors:

- `maxStuckRounds = 5` (`internal/agent/loop.go:217`) — counts consecutive **no-tool** rounds
  whose first `stuckTextPrefixChars = 50` runes are near-identical.
- `maxStuckToolfulRounds = 10` (`internal/agent/loop.go:257`) — counts consecutive rounds whose
  **tool name + params hash is byte-identical** (the 272-round/10 h incident class).

Neither fires on 443 *distinct* vision prompts describing the same scene. And
`MaxIterations: 0` (`internal/agent/loop.go:2309`, "0 = unlimited — model decides when to
stop") means the only real terminator is `defaultSessionWallclock = 8 * time.Hour`
(`internal/agent/loop.go:276`, graceful-finish path at `loop.go:2589–2597`). **M1 ran the
full 8.02 h cap and was stopped by the wall clock, not by the task.** That is the quantified
cost of an open loop with no progress signal: it cannot distinguish converging from thrashing,
so it burns the budget.

### A.4 The closed-loop algorithm to replace it (no code; the design)

Per iteration `k`, with a *task-scoped* state machine (not a fresh prompt):

```
STATE: goal, anchor (screen-relative ratio of the target/ROI), plan, last_action, expected_delta, budget
LOOP k = 1..K_max:
  1. OBSERVE   capture frame_k (deterministic, same window/geometry as frame_{k-1})
               ROI := window around the CURRENT anchor  (start wide: full screen)
  2. DIFF      d_k := mean|grey(frame_k − frame_{k-1})| over ROI
               null_floor := 2 same-state frames' diff (self-calibrating, no absolute threshold)
               VERDICT: d_k > 2·null_floor ⇒ "something real happened"
  3. DECIDE    IF goal-visible-in-ROI (text/icon read at ≥6 photo px cap-height per MEM #1678)
                   → emit RELATIVE action (arrow keys / nearest-neighbour cursor move), never a coordinate
               ELIF ROI too coarse (target not resolvable)
                   → shrink ROI by fixed factor about the anchor (coarse-to-fine), re-issue step 1
               ELSE (target not in ROI at all)
                   → llm picks among ≤3 WIDE REGIONS (LLM = region selector, not pointer)
  4. ACT       relative actuation only; measure the counts→px transfer IN-LOOP and invert it
               (MEM #1678: 5× non-linearity ⇒ never assume a scalar gain)
  5. VERIFY    require 2 consecutive frames agreeing on the expected delta (deadband) else step 1
  6. TERMINATE if any of:
       (a) arrival: expected-delta observed 2× consecutively
       (b) goal_reached: task predicate from the game state (e.g. Hero Found = anchor locked
           with matching sprite/template score above threshold for 2 frames)
       (c) no_progress: anchor unchanged AND d_k ≤ 2·null_floor for N=8 consecutive iterations
           → machine-detected stuck, surfaced as a distinct event (this is the guard gopi lacks)
       (d) detection_failed: ROI shrunk to the resolution floor and still no lock (≥6 photo px rule)
       (e) budget: K_max iterations or wall-clock slice exceeded
```

**Iteration estimate.** MEM #1678's measured servo reached arrival in **9–14 iterations**
(4/4 arrivals, final error 10.6–27.7 px) for a single target under a fixed camera. Applying
that to "locate the hero on the adventure map and click it": a coarse-to-fine ROI ladder of
~3 levels × 3–5 servo iterations ≈ **10–20 iterations per localization**, each costing one
capture (160 ms on an open stream per MEM #1677 §9) + one deterministic diff (<10 ms on x86) +
occasionally one vision call. Compare M1's second game: hours of 464 vision calls with
**no termination predicate at all** — the win is not "better vision", it is **a stopping
condition and a progress signal**.

**Honest caveat**: MEM #1677/#1678 were measured on a webcam-looking-at-a-monitor optical
path. The HoMM3 case has a *digital* capture (`import -window`), so the homography/keystone
leg is unnecessary and errors should be *smaller*; but the hero sprite is smaller and
lower-contrast than the tested 20×20 widget, and the game *animates* (idle animations change
pixels even when the hero does not move), which raises `null_floor`. **The 9–14-iteration
figure is a prior, not a measurement for this game.** The first increment below is designed to
*produce* that measurement cheaply.

---

## B. Capability gap table — "agent plays a game unattended"

Legend: **HAVE** = primitive exists and is adequate · **PARTIAL** = exists but wrong shape ·
**MISSING** = no primitive. Every row cites gopi source `file:line` at commit `6a316b7`.

| # | Capability | Status | Evidence (file:line) | Smallest fix |
|---|---|---|---|---|
| B1 | Read a screen | **PARTIAL** | `vision_analyze` takes an image **path** the model must produce (`internal/tools/vision.go:116`, schema `internal/agent/loop.go:1267–1281`: only `image_path` + `prompt`), so *capture* is the model's job via bash `import`/`xwd` (M1S: 567 bash commands contained `import -window`). `internal/shot/shot.go:1–12` is a **Chromium/CDP** capture engine (`Page.captureScreenshot`, `shot.go:228`) — it cannot see an X11 game window at all. | One tool that captures a named window/screen region and returns a **path** (no CDP, no LLM-authored shell). |
| B2 | Act on a screen | **MISSING** | grep for `xdotool\|Xvfb\|XTEST\|X11` across `internal/` + `cmd/`: **zero hits** (verified). M1S hand-rolled 507 bash commands containing `xdotool`. | Relative-only actuation primitive (key/click/drag with an explicit *count*), no absolute coordinates exposed to the LLM. |
| B3 | Coordinate grounding | **MISSING (structurally)** | No primitive returns or accepts a location. `vision_analyze` returns prose (`internal/tools/vision.go:110–123`). MEM #1678: vision absolute localisation median 154.8 px ⇒ 0/4 open-loop hits. | Do not build it. Ground *relatively*: ROI window + deterministic anchor ratio (MEM #1678 tracker, 0.0–6.9 px). |
| B4 | Know whether my action worked | **MISSING** | No diff/verify primitive anywhere; M1's verification was "take another screenshot and ask the LLM what it sees" (464×). No arrival detector, no deadband. | Deterministic ROI grey-diff with a self-calibrating null (MEM #1677 §2 recipe; 7.9× separation measured). |
| B5 | Detect "I am thrashing" | **PARTIAL** | Only `maxStuckRounds = 5` (`loop.go:217`, no-tool near-identical text) and `maxStuckToolfulRounds = 10` (`loop.go:257`, byte-identical tool+params). 443 distinct vision prompts defeat both. `loop.go:2309` `MaxIterations: 0` = unlimited. | A task-level *progress* signal (anchor stable AND ROI diff below floor for N iterations) — the (c) branch of A.4. |
| B6 | Terminate a game episode | **PARTIAL** | Only `defaultSessionWallclock = 8h` (`loop.go:276`, applied `loop.go:2589–2597`) and the two stuck counters. **M1 was stopped by the wall clock** (M1S: 8.02 h) — no in-game notion of "won/lost/stuck". | Episode-level predicate (defeat/victory screen template, or "N turns with no state change"). |
| B7 | Long-run stability | **HAVE (improved)** | M1R: 「本 run 8h cap 无 400 崩溃——较 M0 进步」. Context plumbing: `DefaultCompactTargetRatio = 0.35` (`internal/agent/compact.go:39`), `DefaultCompactCooldown = 600s` (`compact.go:45`). | None. Do not touch. |
| B8 | Cost accounting | **PARTIAL** | Two halves: (i) **per-request `{cache_hit, cache_miss, prompt_tokens, completion_tokens, iteration, timestamp}` IS persisted** into the session JSONL (`type=request`, 2342 records in M1S — this is what makes §F.3 measurable); (ii) but `CostTracker.Summary()` (`internal/deepseek/types.go:127–150`) prints raw token counts only — **no 元 conversion and zero price constants anywhere in the repo** (grep for price constants: zero hits), and there is no in-session cost *signal* the agent can act on. | Offline arithmetic (this doc, §F.3) is enough for now; if a cost *guard* is ever wanted, a per-model price constant block (Rule 1) + a summary line — not before increment 1's numbers exist. |
| B9 | Run the game at all | **HAVE** | M1R/M1P: HoMM3 under wine 9.0 on WineHQ stable, Xvfb + xdotool + ImageMagick `import`, driven through bash. | Keep — but wrap in B1/B2 so it is not re-derived per round. |
| B10 | Know the rules from the screen | **HAVE (adequate)** | M1R: turn loop, resource piles, Witch Hut visit all executed (`M1P` 19:46:55Z…20:14:19Z). MEM #223 confirms for VCMI/HoMM3 that the *cognition* ceiling is real but is not the perception gap. | Nothing in increment 1. |
| B11 | Self-verify without a human | **MISSING** | Every M1 check was a vision read interpreted by the same model that acted. No ground-truth predicate. | The whole point of A.4 step 5 + B5. |
| B12 | Reproducible game episode | **PARTIAL** | 2442 frames under `~/workspace/homm3-m1/` but no seed/scenario pinning, no per-episode manifest; M1's two games differ (Black Finn / Yog) so they are not comparable. | A fixed seed + one scenario + a run manifest (episode id, seed, budget, outcome). |

**Three-row summary**: *seeing* is PARTIAL-but-sufficient (B1), *acting* is MISSING (B2),
*closing the loop* is MISSING (B4/B5/B6/B11). The gap list is dominated by control, not by
perception. This is the same shape as MEM #224 (CivRealm: the LLM-4X bottleneck is cognition,
not the modality) — but M1 adds a second, cheaper bottleneck: **the harness has no feedback
channel at all**.

---

## C. Scoped minimum deliverable — ranked candidates

The constraint is 「我们不是一个公司，资源是很有限的」. A deliverable is only admissible if one
operator can build, run and *verify* it on one machine with no downloads (see §D).

| Rank | Candidate | What it is | Effort | Verifiable by | Verdict |
|---|---|---|---|---|---|
| **C1** | **`sgt-*` closed-loop pilot** | Drive one already-installed puzzle binary (`/usr/games/sgt-*`, 40 present) on Xvfb; ROI diff + relative keys; episode = solve or K_max. | **~1–2 focused days** | Win/lose is machine-checkable; N episodes → a solve-rate number; token cost measured, not estimated | **RECOMMENDED** |
| C2 | HoMM3/VCMI hero-lock micro-task | Same loop, but the target is the M1 hero sprite on the adventure map | ~3–5 days | Requires B1+B2+B3+B4 *and* the sprite template; the animation null-floor is unmeasured | Second — do it *after* C1 has produced the loop |
| C3 | VCMI text-bridge AI (no pixels) | Plug the LLM into `CCallback`/`CGameInterface` per MEM #223 (report `~/work/claude/nick/vcmi_LLM_playability_report.md`) | ~1.5–2.5 months part-time (MEM #223) | Game-result + score vs Nullkiller2 | Out of scope now (violates the resource constraint), but it is the *correct* long-term HoMM3 answer |
| C4 | Generic "screen-agent" tool (`gopi click/screenshot`) | A general X11 grounding tool | weeks + ongoing maintenance | — | **Reject** — that is a platform (also Rule 3: no new top-level tools without LLM-adoption proof) |
| C5 | RL / imitation-learning game agent | Train a policy | weeks–months + GPU | — | **Reject** — MEM #223/#1678 both show the cheap deterministic primitives reach the bar first |

### C.1 The recommendation, and its smallest scope

**C1, and its smallest scope is deliberately smaller than "solve a puzzle":**

> **Increment 1 = one scripted probe, not an agent feature.**
> Pick ONE `sgt-*` binary. Launch it on Xvfb. Capture two frames; measure the ROI `null_floor`.
> Issue ONE relative keypress; capture again; measure the diff. Then run **K = 10 servo
> iterations toward ONE fixed on-screen target** (an arrow button, a grid cell), with the
> A.4 termination rules (a)–(e) wired in, and have the LLM do exactly two things: pick the ROI
> region and judge "did the expected change happen".
> **Deliverable = a numbers table**: iterations-to-arrival, final px error, null_floor,
> vision calls per episode, tokens per episode, and whether termination (c)/(d) fired.
> Nothing is committed to `internal/` in increment 1.

That is one file, one game, one target, one measured table — and it is the *smallest thing that
can falsify* the design hypothesis of §A. If iterations-to-arrival lands at 30+ with frequent
(d) firings, the closed-loop premise is wrong for this game and C2/C3 should be re-scoped
before any tool is built. If it lands at ~10, increment 2 (B5's stuck signal) is justified.

**Why not start with HoMM3 (the actual goal)?** Because C2's success depends on B1–B4 all
existing at once, and its null-floor is polluted by idle animation. C1 isolates the loop with a
static target and a machine-checkable outcome; it is the control experiment M1 never ran.

---

## D. Game selection

### D.1 What is actually installed (inventory, this machine, 2026-09-11)

Verified by `ls /usr/games/`, `dpkg -l`, `command -v`:

| Game / family | Presence | Notes |
|---|---|---|
| **`sgt-*` puzzle suite** | **40 binaries** (`/usr/games/sgt-*`: mines, solo, tents, pattern, bridges, lightup, galaxies, map, towers, …) + `sgt-launcher` | Simon Tatham's Portable Puzzle Collection. All share ONE engine: static grid, keyboard/mouse, `--generate <n>` / `--load` / `--save` CLI flags (verified by `strings`). **Win state is machine-checkable.** |
| gnome-mines, gnome-sudoku | present (`/usr/games/`) | GTK; heavier, less scriptable than `sgt-*` |
| **VCMI 1.6.0** | `~/workspace/vcmi-install/bin/{vcmiclient,vcmserver,vcmeditor,vcmlauncher,vcmibuilder}`; source at `~/work/vcmi`; user data `~/.local/share/vcmi` | The HoMM3 open-source engine. MEM #223: fully symbol-abstracted; the LLM plugs in at `CCallback`/`CGameInterface` with **zero pixels**. Also the M1 target when run through wine. |
| **HoMM3 Complete (GOG)** | via `wine 9.0` (WineHQ stable, `wine-stable 9.0.0.0~jammy-1`) | This is what M0/M1 drove |
| **Battle for Wesnoth** | source at `~/work/wesnoth` | Open source, TBS, has an AI side |
| X11 tooling | `Xvfb`, `xdotool`, `import`, `convert`, `ffmpeg`, `xwd`, `python3` all present | full headless capture+actuation stack, **no download needed** |
| Browser capture | `internal/shot/shot.go` (CDP) | exists in gopi; irrelevant to native games |

**Nothing needs to be downloaded.** There is enough installed material for the whole plan.

### D.2 Recommendation

- **Increment 1 target: one `sgt-*` puzzle** (proposed: `sgt-mines` or `sgt-pattern` — the
  latter has a small grid, deterministic rule checking, and a tiny state space). Rationale:
  (i) static frame ⇒ a clean `null_floor` that isolates the *loop* from the *animation* noise;
  (ii) `--generate/--load/--save` gives a **reproducible episode** (B12) for free;
  (iii) win/lose is checkable by the game itself, so the acceptance number is not a vision
  judgement — it is a fact.
- **Increment 2 target: HoMM3 / VCMI hero localization**, reusing the exact loop, now with the
  animated-frame null-floor measured.
- **Do not** add a new game download. If a game must be added later, prefer a package already in
  the distro archive over an asset download (Rule 7: dependency preflight; ZERO-DEP default).

---

## E. 陪人玩 / hot-seat companion framing — and why it lowers the bar

The original framing question was whether the agent must *beat* the game or merely *play
alongside* a human. That distinction is load-bearing for the resource constraint.

| | **Solo unattended agent** | **陪人玩 (hot-seat companion)** |
|---|---|---|
| Success bar | Play a full game "well" — win, or at least not lose to the built-in AI | Take a *legal, sensible* turn on request; hand control back |
| Iteration budget | 2400+ rounds (M1S) before any verdict | One turn = a bounded number of rounds; a human is the termination signal |
| Failure mode | Unattended thrash, wall-clock burn, no verdict | Human says "wrong" immediately — **the human IS the progress signal** (B5/B11 for free) |
| Cost model | Pays for every idle round (§F.3) | Pays only for turns actually taken |
| Blocker sensitivity | Needs B1–B6, B11, B12 all | Needs B1, B2, B10 — **not** B11/B12 |

**The lowered bar is real and it is the strongest argument for this framing**: `陪人玩` removes
the exact capability that M1 proved missing. In hot-seat play, a human observes the agent's move
and corrects it — the closed loop is *closed by the human*, at zero token cost. M1's 443-prompt
thrash exists only because nobody was there to say "no, the hero is there".

**Honest counter-point (a deliverable, not a caveat buried in prose)**: hot-seat `陪人玩` also
**weakens the "unattended" claim that motivated the whole exercise** — an agent whose loop is
closed by a human is a *tool*, not an unattended agent, and it is then competing with "just play
with a friend". §G addresses whether that is a product or a hobby.

---

## F. Candid difficulty ranking + cost per game-hour

### F.1 Ranked difficulty (hardest → easiest), with the reason

| Rank | Item | Why it is ranked here |
|---|---|---|
| 1 | **Playing *well*** (strategy, long-horizon planning) | MEM #224 (CivRealm, arXiv:2401.10568): GPT-4 with a *perfect text interface* lost decisively to a heuristic 4X AI; failure modes = long-term planning, economy, spatial reasoning. M1 confirms: defeat on Day 3 (`M1P` 20:18:01Z) with no battle ever triggered. This is a *ceiling*, not a blocker to be removed. |
| 2 | **Unattended "did I succeed?" verification** | B11/B5/B6 all MISSING; open-loop failure is invisible to the agent (§A.2) — M1 cannot tell converging from thrashing, so it spends its entire budget. |
| 3 | **Pixel grounding of small moving objects** | B3; measured open-loop error 154.8 px vs a ~20 px target (MEM #1678). Solvable *only* as a loop, never as a better model. |
| 4 | **The game's own determinism/animation** | The `null_floor` for an animated frame is unmeasured; this is the main technical unknown in C1→C2. |
| 5 | **Wiring capture+actuation inside gopi** | B1/B2: today the LLM hand-rolls `import`/`xdotool` through bash (567 + 507 of 1851 bash commands in M1S). Tedious and context-costly, not conceptually hard. |
| 6 | **Long-run stability** | Already solved (B7): 8 h without a 400 crash, `internal/agent/compact.go:39/45`. |
| 7 | **Getting the game to run headless** | Already solved in M0/M1 (wine + Xvfb + xdotool). |

### F.2 The one-line difficulty verdict

**Difficulty is not "can the model see" — it is "does anything tell the model it is wrong",
and after that, "does the model play well".** Items 1 and 2 are the real costs; items 3–7 are
engineering that already has measured recipes.

### F.3 Cost per game-hour, computed from the real price table

**Prices** (PRICE, `devdoc/deepseek-v4.1-flash-model-naming-and-kv-architecture-20260910.md`
lines 46–51, 元 per 1 M tokens, `deepseek-flash` = DeepSeek-V4.1-Flash):

| | idle (空闲) | peak (高峰) |
|---|---|---|
| input, cache **hit** | 0.02 | 0.04 |
| input, cache **miss** | 1.0 | 2.0 |
| output | 4.0 | 8.0 |

Peak = 工作日 09:00–12:00 and 14:00–18:00 Beijing (line 57); everything else idle (line 58);
idle = peak/2. **These are the only numbers used below — no invented figures.**

**Step 1 — M1's measured invoice.** M1S **does** persist per-request usage: every `request`
record carries `{cache_hit, cache_miss, completion_tokens, prompt_tokens, iteration, timestamp}`
(`internal/deepseek/types.go:117–124` is the matching `Usage` struct). 2342 request records,
04:16:10→12:16:05 +08:00 (7.998 h; the enclosing session window 04:14:51→12:16:09 = 8.02 h).

```bash
python3 - <<'EOF'
import json
p="/home/user/.gopi/sessions/t-1788720718/a454aba5-455f-400a-ba87-eaeb5ea5972d.jsonl"
hit=miss=out=0; byidle=bypeak=0.0; n=0
for line in open(p,encoding='utf-8',errors='ignore'):
    try: o=json.loads(line)
    except Exception: continue
    if o.get('type')!='request': continue
    n+=1
    H,M,C = o['cache_hit'] or 0, o['cache_miss'] or 0, o['completion_tokens'] or 0
    hit+=H; miss+=M; out+=C
    peak = int(o['timestamp'][11:13]) in (9,10,11,14,15,16,17)   # PRICE line 57
    ph,pm,po = (0.04,2.0,8.0) if peak else (0.02,1.0,4.0)        # 元 per 1M
    v = H/1e6*ph + M/1e6*pm + C/1e6*po
    if peak: bypeak+=v
    else:    byidle+=v
print(n, hit, miss, out)
print(f"idle-hours={byidle:.3f}  peak-hours={bypeak:.3f}  TOTAL={byidle+bypeak:.3f} 元")
EOF
# → 2342 131796480 1994598 796901
# → idle-hours=4.154  peak-hours=7.329  TOTAL=11.482 元
```

| M1 measured totals | Value |
|---|---|
| prompt tokens (sum over 2342 rounds) | **133,791,078** (= 133.8 M) |
| — cache **hit** | 131,796,480 (**98.5 %**) |
| — cache **miss** | 1,994,598 (1.5 %) |
| completion (output) tokens | **796,901** |
| mean prompt tokens **per round** | **57,127** |
| requests inside peak windows (09–11, 14–17) | 892 of 2342 (38 %) |

**Step 2 — the arithmetic, line by line** (元 per 1 M: hit 0.02/0.04, miss 1.0/2.0, out 4.0/8.0):

| Component | Tokens | Rate (idle → peak) | 元 (idle) | 元 (peak) |
|---|---|---|---|---|
| input, cache hit | 131.80 M | 0.02 → 0.04 | 2.636 | 5.272 |
| input, cache miss | 1.995 M | 1.0 → 2.0 | 1.995 | 3.989 |
| output | 0.797 M | 4.0 → 8.0 | 3.188 | 6.375 |
| **all-idle total** | | | **7.818** | |
| **all-peak total** | | | | **15.636** |
| **actual (hour-of-day weighted: 892 peak / 1450 idle rounds)** | | | **11.482 元** | |

**Step 3 — per game-hour** (8.02 h session):

- measured actual: 11.482 / 8.02 = **1.43 元 / game-hour**
- counterfactual all-idle (run it 23:00–08:00, as M0 did): 7.818 / 8.02 = **0.98 元 / game-hour**
- counterfactual all-peak (a working-hours run): 15.636 / 8.02 = **1.95 元 / game-hour**
- per round: 11.482 / 2342 = **4.9 毫 / round**

**~$0.14–0.27 per game-hour** at ≈7.1 元/USD (the FX rate is the only non-source number here;
every other figure comes from the PRICE table and M1S).

**Step 4 — what actually drives the cost (the counter-intuitive finding).** The naive model
says "the vision agent pays for looking". The invoice says otherwise:

- **98.5 % cache-hit rate** — the 1 M context is re-sent every round but almost all of it is
  cached, so the *entire* 131.8 M-token hit traffic costs **2.64 元**, while the **1.5 %**
  that misses costs **2.0 元** and the **0.8 M output tokens cost 3.19 元**.
- Output is 0.6 % of the tokens but **41 %** of the bill (3.19 / 7.82 idle). Hit input is
  98.5 % of the tokens but **34 %** of the bill.
- The real cost generator is **57,127 prompt tokens per round** — the conversation grows to
  ~133 M tokens of cumulative re-sent context. **The cheapest lever is not a better model and
  not fewer vision calls; it is fewer, shorter rounds and a smaller live context.** Every
  round costs ~4.9 毫 before the model does anything.
- Sanity check on the time axis: the peak-window share (892/2342 = 38 % of rounds) carried
  64 % of the cost (7.329/11.482) — the idle/peak ×2 switch is worth more than any prompt
  tuning at this scale.

**Step 5 — honest caveats.** (a) The 8.02 h / 2342-round figures describe M1's *first* game and
the 8 h cap; the second game (the hero-localization thrash) is inside the same window, so this
invoice is the whole run, not the thrash alone. (b) M1S's own `model` field says
`deepseek-v4-flash` (`metadata.json`), but M1R states the run used the GLM night-free window
(23:00–09:00) and M1S shows the agent *probing* `open.bigmodel.cn` / `.glm_settings.json` with
an `api_key` and `model: glm-5.3-flash` (18 occurrences, inside tool_results and model text —
**not** a request-line host, which is why the host count is 0). **So this invoice is the
DeepSeek-price counterfactual for M1's token volumes, not a receipt that was paid** — which is
exactly what §F.3 needs (what a paid run of that shape would cost) and is labelled as such.
(c) Thinking tokens are not in `completion_tokens` if the provider splits them; PRICE line 53
lists max output 384 K, and 0.797 M total over 2342 rounds = 340 output tokens/round, which is
consistent with a split-thinking wire. (d) **Increment 1 must record tokens per episode** —
which is cheap, because M1S proves the numbers are already on disk; the gap was my reading of
them, not gopi's instrumentation.

**Cross-check against an independent estimate**: MEM #223 (VCMI/HoMM3 via a *text* bridge,
object-graph snapshot 1–5 k tokens/turn, 300–2000 decisions/game, cheap models) → **$0.10–2.00
per whole game**. A HoMM3 game is many hours; the two agree in order of magnitude. But M1's
measured invoice exposes the real shape of the pixel route: **57 k prompt tokens per round**,
i.e. the pixel agent re-sends ~1000× the text bridge's per-turn snapshot because screenshots
and screenshots-of-screenshots accumulate in the transcript. **That is the quantified argument
for C3's text bridge being the right long-term engine for this specific game, and for the pixel
loop being a *generic* capability rather than a HoMM3 feature** — and it is a cost argument,
not merely an architectural preference.

---

## G. Monetisation analysis

Framed by the constraint: 我们不是一个公司 — so G1–G6 are tests of whether *any* of this is
worth money, each one allowed to come back negative.

### G1. Consumer vs enterprise (the structural table)

| | **Consumer** (a player buys this) | **Enterprise** (a studio buys this) |
|---|---|---|
| What is sold | An AI that plays with/against you | AI playtesting / coverage / balance data |
| Buyer | An individual, 10–100 元 one-off or a subscription | A studio/publisher with a QA line item |
| Distribution | Steam / itch / mobile store | Direct sales, integration into CI |
| Who already exists | Every game's built-in AI, plus the companion-app category (Character.AI-scale engagement per web evidence) | modl.ai (see G2) and the traditional QA outsourcers |
| Our realistic position | An individual's weekend artefact with **1.0–1.95 元/game-hour** marginal cost (§F.3, measured) | A vendor that must pass studio IP/on-prem review — **out of scope by constraint** |
| Verdict | Only via a *turn-based* niche (G4) and only with BYOK (G3) | **Rejected** — enterprise sales is a company, not a solo operator |

### G2. Evidence: the enterprise space is occupied (real sources, or explicit none-found)

- **modl.ai** — Copenhagen-based; AI playtesting agents ("modl:test bots") that send simulated
  inputs and explore a game's state space, plus player modelling and telemetry; positioned as
  **"Agents as a Service" for enterprise**, with on-prem/private-cloud options and CI/CD
  integration for Unity/Unreal/custom engines. Source: web search (deepseek web method) on
  "modl.ai AI agents playtesting games studio enterprise", 2026-09-11; the returned summary
  describes the company as a **B2B QA vendor, not a consumer or indie-facing product**, and
  names its competitors as traditional QA outsourcers (Keywords, PTW, Testronic) plus emerging
  AI-QA startups. **Not an official page fetch** — flagged as second-hand web synthesis.
- **Market-size numbers**: the same search returned *estimates* — game services/external dev
  ~$4–6 B, QA+loc testing ~15–25 % of that (~$700 M–$1.5 B), playtesting specifically in the
  low hundreds of millions; Keywords Studios FY2023 group revenue ~€780 M with functional
  testing historically ~€100 M+/yr. **These are third-party estimates, not audited figures**,
  and Keywords went private (EQT, 2024) so FY2025 segment reporting may not be public.
- **Explicit none-found**: I found **no** official, vendor-published price list for AI
  playtesting (no public per-hour / per-run price, no self-serve tier). A web search for
  `"AI playtesting" game QA automation market 2025 companies modl Ludo.ai` returned **zero
  usable results** (bing: empty). So the enterprise price point is **unknown and must be
  treated as unknown** — no number is invented here.
- **What this means for a solo operator**: the enterprise route requires sales, IP/security
  review, on-prem delivery and per-title agent tuning (modl's own positioning). That is
  precisely the "becomes a company" outcome the constraint forbids. **Verdict: reject G2 as a
  business, retain as a *capability demonstration*.**

### G3. Marginal cost per game-hour — the number and the verdict

From §F.3 (measured): **1.43 元/game-hour** actual, **0.98** all-idle, **1.95** all-peak. Compare against the only real consumer price anchor we have:
a companion-style subscription is ~$10–20/month (web evidence, G5) and, with an active player at
2 h/day for 30 days → 60 game-hours × **1.43 元** (the measured actual rate, §F.3) = **≈86 元
≈ $12/month in marginal inference cost** — i.e. **the same order as the entire subscription
price**, and that is the *good* case (the run was 38 % peak-priced; a daytime-only player pays
1.95 元/h → ≈117 元 ≈ $16/month, which *exceeds* the price). At idle-only it is ≈59 元 ≈
$8/month. **Every plausible configuration has marginal cost within ±1× of the retail price.**
That is the whole business case, and it does not close on its own.

**BYOK verdict**: **BYOK is required, not optional.** A consumer product that absorbs the
inference bill carries a cost of the same order as the price it can charge — one thrashing
8-hour session (M1's measured 11.5 元, and M1 *was* a thrashing 8-hour session) eats a month of
subscription at the low end. Letting the user supply their own key makes cost **the user's
marginal decision**, keeps the operator's cost at zero, and is the only shape compatible with
我们不是一个公司. The corollary is uncomfortable and is stated plainly: with BYOK, "the product"
is a **saved prompt + a small local tool**, competing for free.

**Note the asymmetry this creates for 陪人玩 (§E)**: the human-closed loop is *also* the cost
control — a human takes ~one turn per minute, not 2342 rounds per 8 h, so the same capability
delivered as a hot-seat companion is roughly **one to two orders of magnitude cheaper per hour
of entertainment** than the unattended agent measured in §F.3.

### G4. Which genres can carry it at all — and the companionship argument

**Turn-based only.** The reason is arithmetic, not taste: cost is per *decision*, and a
turn-based game gives a decision a wall-clock budget of seconds-to-minutes, which a
network round-trip fits inside. A real-time game needs a decision every 16–100 ms; a
1.5–3.3 s vision round trip (MEM #1677 §6 measurement) is 15–200× over budget, before cost.
MEM #1678 also measured the *capture* leg alone at ~2.5 s round-trip vs 11.5 ms of actuation —
**the network vision loop is 15× the entire capture+actuation leg**, and that ratio does not
improve with a better camera.

| Genre set | Fits turn-based? | Fit for this design |
|---|---|---|
| Turn-based 4X / TBS (Civ, HoMM3, Wesnoth, Endless Legend) | yes | Best — a turn is a bounded decision |
| Board/abstract puzzles (the `sgt-*` suite, chess) | yes | Best for *verification* (machine-checkable win) |
| Roguelike / dungeon crawler (turn-based) | yes | Good — small action space, long episodes |
| Card / auto-battler | yes (decision-timed) | Good |
| RTS / MOBA / FPS / action | **no** | Rejected on latency arithmetic |

**Pain-point-vs-companionship argument.** The web evidence (deepseek web synthesis, 2026-09-11)
on what players actually ask for is consistent across r/civ, r/totalwar, r/paradoxplaza,
r/aoe2, r/Stellaris and r/truegaming: **(i) AI that uses the same rules (no cheating bonuses);
(ii) AI that adapts to the player's strategy instead of running a fixed script; (iii) better
diplomacy — remembering past actions; (iv) difficulty via intelligence, not stat inflation.**
That is a **pain point**, and it is where an LLM is genuinely good (flavour-diplomacy,
adaptation, memory). It is *not* where the M1 effort went (pixel localization). Meanwhile the
*companionship* axis is a different product (emotional attachment, per the companion-app
evidence: Character.AI ~2 h/day engagement, $9.99/mo tier) with a different incumbent set and a
different risk profile.

**Argument**: the defensible framing for a solo operator is **"a smarter opponent/partner for
turn-based games I already own"** (pain point: the AI cheats or is brain-dead), *not* "an AI
companion" (crowded, emotionally-loaded, monetised by companies with model budgets). The
uncomfortable follow-up: if the answer is "a smarter AI opponent", MEM #223 shows the correct
implementation for the flagship title is a **text bridge into the engine (no pixels)** — which
is cheaper, more reliable, and *less* general than the pixel loop. The pixel loop's value is
therefore **generality across installed games**, not superiority on HoMM3.

### G5. Demand signals (with sources — or an explicit evidence gap)

| # | Signal | Source | Strength |
|---|---|---|---|
| S1 | Players consistently demand non-cheating, adaptive AI opponents; the most-subscribed mods for Civ VI ("AI+"), Total War ("Better AI", "Campaign AI Overhaul"), Stellaris ("Starnet AI"), AoE2 ("Barbarian"/"Extreme AI"), HoI4 ("Expert AI") are all AI-behaviour mods; Firaxis hired Civ AI modders | web evidence, deepseek web synthesis over r/civ, r/totalwar, r/paradoxplaza, r/aoe2, r/Stellaris, r/truegaming (2026-09-11) | **Strong on the pain point**, weak on willingness-to-pay |
| S2 | Generative-AI *disclosure* is a measured negative: games disclosing generative-AI use receive **lower recommendation rates and more negative sentiment** than procedurally-generated-content baselines; analysis of **508,192** English Steam reviews + thematic analysis of 600 | arXiv:2608.11539, "Player Perceptions of Generative AI in Games: A Steam Review Analysis" (Bazzaz & Cooper, submitted 2026-08-12), fetched 2026-09-11 | **Strong and adversarial** — argues against a visible "AI plays this" label |
| S3 | Companion AI has real engagement/revenue: Character.AI ~20 M+ MAU at peak, ~2 h/day, ~$9.99/mo tier, ~$32 M revenue 2024 (estimated); companion apps among the fastest-growing consumer-AI categories by in-app revenue; D30 retention 20–40 %, ARPU $5–15/mo, paid conversion 3–10 % | web evidence, deepseek web synthesis (Sensor Tower / Appfigures / a16z-class estimates, company-reported engagement), 2026-09-11 — **estimates, not audited** | Medium — signals a market for *companionship*, not for *game AI* |
| S4 | 陪玩 (paid play-companion) market: ~200–300 亿元 GMV-scale estimates for 2024 (China), core paying users ~15–25 M, and 「AI 陪玩开始侵蚀低端市场」 — AI voice/teammate products already displacing low-end paid companionship | web evidence, deepseek web synthesis of 艾瑞/易观-class estimates, 2026-09-11 — **explicitly flagged as having large error bars and 60–70 % "real" discount on self-reported GMV** | Medium — and the AI-erosion note is a *negative* for a new entrant |

**Explicit evidence gaps (required deliverables, not omissions):**

1. **No price data** for AI playtesting (see G2: search returned nothing usable).
2. **No willingness-to-pay measurement** for "an AI that plays turn-based games with me" — S1
   proves the pain point, not the cheque. Every mod in S1 is **free**; that is the strongest
   counter-evidence in this document.
3. **No numbers on how many consumers can BYOK** — G3's whole viability rests on it and no
   source was found.
4. **FX and price stability**: §F.3's 元→$ conversion assumes ~7.1 元/USD and the 2026-09-10
   price table; a price change moves every number in G3.

### G6. Counter-arguments, ranked strongest-first

1. **The free-mod counter-example (strongest).** The demand in S1 is satisfied today by *free*
   community AI mods with large subscriptions counts. Charging for "better AI" means beating a
   free alternative that is already integrated into the game's engine and rules. → Defensible
   answer: **generality** (works on games whose engine you cannot mod) — narrow, and unproven.
2. **Generative-AI disclosure is a measured negative (S2).** 508 k Steam reviews show disclosed
   generative-AI use correlates with lower recommendation rates and "low developer investment"
   readings. → A visible AI-opponent feature is a marketing liability on the platform where a
   consumer product would ship. This directly attacks the G1-consumer row.
3. **The flagship best-case is a text bridge, not the pixel loop.** MEM #223 (source-verified
   against VCMI master: `AdventureMapInterface.cpp:502,609,920` → `cb->endTurn/moveHero/castSpell`,
   ~50 `CCallback` methods, 45 `IGameInfoCallback` getters) shows HoMM3 needs **zero pixels**.
   The pixel testbed's value is only its generality — and the loop itself
   (increment 1, C1) may fail on the animated-frame null-floor. → Honest reading: **increment 1
   is a research probe whose business value is contingent and unproven.**
4. **Marginal cost is the same order as the price (G3).** 1.43 元/game-hour (measured) × a
   player at 2 h/day ≈ 86 元 ≈ $12/month vs a $10–20/month companion price — no headroom. → Only BYOK fixes it, and BYOK commoditises
   the product to "a saved prompt".
5. **Enterprise is the only profitable-looking route — and it is explicitly out of scope.** modl
   already occupies it with on-prem delivery, CI integration and per-title tuning (G2). → Doing
   it would mean becoming a company: rejected by the constraint.
6. **Playing *well* is unsolved and may stay unsolved.** MEM #224: GPT-4 lost decisively to a
   heuristic Civ VI AI *with perfect information*. A product that plays badly is worse than no
   product. → Caps the ambition at "companion/partner", which G4 argues is a different market.

**G-verdict**: every monetisation route either requires company-scale execution (enterprise),
competes with a free incumbent (consumer AI-mod), or carries a cost of the same order as the price it can charge (consumer,
non-BYOK: 1.43 元/game-hour measured vs a ~$10-20/month anchor).
**The honest conclusion is that this work is a capability project, not a business** — which is consistent with the constraint, and is why §H recommends
an increment that produces *knowledge*, not revenue.

---

## H. Revised recommendation — the first increment

**H1 — what it is (one concrete, small, user-testable thing):**

> **`episode-1`: a reproducible closed-loop probe on ONE installed `sgt-*` puzzle.**
>
> 1. Fixed seed + fixed binary (`sgt-pattern --generate <N>`, flags verified present via
>    `strings /usr/games/sgt-mines`), launched on Xvfb — a **reproducible episode** (closes B12).
> 2. Two same-state frames → `null_floor` over the game grid ROI (the B4 primitive measured,
>    not assumed).
> 3. One relative keypress → measure the diff → confirm the arrival detector separates a real
>    change from the null at the measured ratio (MEM #1677 target: ≥2×; measured elsewhere 7.9×).
> 4. Run **K = 10** servo iterations toward ONE fixed on-screen target with the full A.4
>    termination set (a)–(e), including the (c) no-progress guard that gopi lacks (B5).
> 5. LLM does exactly two jobs: pick the ROI among ≤3 wide regions, and judge
>    "did the expected change happen". It emits **no coordinate**.
> 6. Record: iterations-to-arrival, final error in px, null_floor, diff ratios, vision calls,
>    **prompt/output tokens** (closes B8), which termination branch fired.
>
> **Deliverable = the numbers table + a run manifest.** No change to `internal/` in this
> increment. Nothing is committed as a "feature" until the numbers say the loop converges.

**H2 — why this satisfies both "milestone" and "user-testable":**

- *Milestone*: it is the first gopi-driven game episode whose outcome is decided by a
  **machine-checkable predicate** and whose termination is a **design decision** rather than an
  8-hour wall clock (M1S: 8.02 h under `loop.go:276`). It is also the control experiment M1
  never ran: static frames isolate the loop from animation noise.
- *User-testable*: a human can watch the 10 iterations on Xvfb and see the ROI shrink and the
  target lock — the same demo that a 陪人玩 (§E) session would show, at 1/100 the budget.

**H3 — the go/no-go gate (falsifiable, decided before building anything):**

| Outcome | Reading | Next |
|---|---|---|
| iterations-to-arrival ≤ 15, termination (a)/(b) dominant, tokens/pass recorded | closed-loop premise holds | Increment 2: the B5 no-progress guard + B2 relative-actuation primitive (with LLM-adoption proof per Rule 11) |
| iterations 15–30 with occasional (d) | holds but the resolution floor bites | Increment 2 becomes *resolution first* (crop/zoom contract on `vision_analyze`) |
| iterations > 30 or (c)/(d) dominant | the loop does not converge even on a static, machine-checkable target | **Stop.** Re-scope to C3 (VCMI text bridge, MEM #223) — the capability project's honest answer for HoMM3 |

**H4 — cost of the increment.** One operator, ~1–2 days, 0 downloads, 0 new tools committed,
and a token bill far below §F.3's measured 1.43 元/game-hour because K = 10 iterations per pass
is a *bounded* budget — the opposite of M1's 2342 rounds / 8.02 h wall-clock run.

---

## What we would NOT build

Explicit scope discipline. Each item is rejected with the reason, not merely omitted.

1. **Not a new top-level gopi tool.** GoPi has 16 tools and `internal/agent/loop.go`
   `claudeToolDefs` is the source of truth (Rule 3). B1/B2 gaps are *composed from* existing
   primitives (bash + job + vision_analyze) until increments prove an LLM actually adopts them —
   Rule 11 requires real-task adoption evidence before any tool commit.
2. **Not a generic screen-automation product / "computer-use platform".** That is a platform
   company (see the constraint). C4 rejected.
3. **Not an RL / imitation-learning game agent.** Weeks-to-months plus GPU, and MEM #1678's
   deterministic primitives reach the measurable bar first. Dependency-budget violation.
4. **Not an enterprise playtesting service.** modl.ai-class deliverable needs on-prem, CI
   integration, per-title tuning and sales — the explicit "becomes a company" failure mode (G2).
5. **Not a companion/roleplay product.** Different market, different incumbents
   (Character.AI-scale engagement per S3), different regulatory risk, and it does not exercise
   the capability this project is about.
6. **Not a real-time-game agent.** Rejected on arithmetic: 1.5–3.3 s vision round trip vs a
   16–100 ms decision budget (MEM #1677 §6/§9). No amount of engineering changes this.
7. **Not a new game download.** §D.1 shows 40 `sgt-*` binaries, VCMI 1.6.0 + source, Wesnoth
   source, HoMM3/wine and the full X11 capture stack are already installed. Adding a download
   must be justified against *that*, and none of the plan needs one.
8. **Not a fix to `MaxIterations: 0` / the wall-clock cap** (`loop.go:2309/276`) as part of this
   work. The gap is a *progress* signal (B5), not a shorter clock; changing the global cap to
   chase this would affect every other task's run.
9. **Not a pixel-grounding feature that hands coordinates to the LLM.** MEM #1678's measured
   154.8 px median open-loop error is the standing reason; the design keeps the LLM as region
   selector + verifier only.
10. **Not a "make HoMM3 win" goal.** Defeat on Day 3 (`M1P` 20:18:01Z) and MEM #224's CivRealm
    result put *playing well* at the top of §F.1's difficulty ranking. Scoping it into increment
    1 would guarantee failure.

---

## Open questions carried forward (honest unknowns)

- Is the animated-frame `null_floor` small enough for the ROI diff to work on HoMM3? **Unmeasured.**
- Does the LLM actually adopt the region-selector role, or does it revert to asking for
  coordinates? **Untested** — and per Rule 11, this must be tested with real tasks before any
  prompt/tool commit.
- M1's token usage **is** persisted per request (`type=request` records) — the cost figures in
  §F.3/G3 are arithmetic on those real counts at the published rates. The genuinely open
  question is different: M1S's records are DeepSeek-shaped usage from a run M1R describes as
  GLM-routed, so §F.3 is a **price counterfactual on real token volumes**, not a receipt.
  Increment 1 should log the provider actually billed.
- 443 distinct vision prompts / 464 calls is the headline M1 number, but the JSONL truncates
  prompts at 400 escaped chars in the extraction; a full-text re-count could move the
  distinct-prefix number slightly (the *distinct* conclusion would not change: 464 calls, 2342
  rounds, 8.02 h, no convergence guard fired).
