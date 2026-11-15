<!-- doc-class: normative -->
# The look-without-fixing bound (agent loop, detector ③)

Task: `01a0c2b1-7fb3-7161-bc35-762b71e14490` · branch `feat/look-without-fixing-bound` ·
base master `ccdd5f1e` · spec: §6.3 of `devdoc/cli-as-eyes-and-hands-feasibility.md`.

## 1. What was missing

§6.3 names the operator's risk: an **observation loop** — the model repeatedly looks
at live state (`read` / `bash` / `code_search`…), learns something each round, and
never acts. The spec states both existing nets are structurally blind to it, and this
task was dispatched to verify that claim and build the bound.

**Verified (severity: the claim holds, for the reason the spec states).**

| net | constant | why it cannot see this shape |
|---|---|---|
| detector ① `stuckState` | `maxStuckRounds = 5` | counts **no-tool** rounds; an observing round always carries a tool_use → the counter is reset by the very act of looking |
| detector ② `toolStreakState` | `maxStuckToolfulRounds = 10` | needs a **byte-identical** fingerprint (name + normalized-param hash + 50-rune text prefix) for 10 consecutive rounds; its polling exemption **resets on a changing RESULT** — a live screen/file changes every round, which the exemption reads as "alive" |
| `MaxIterations` | `0` = unlimited | in production nothing else caps round count |

### Anchor drift (spec §6.3 → master `ccdd5f1e`)

The spec's line numbers were written before the `feat/gopi-cli-bubbletea-landing`
merge; the symbols are all still where the spec says, the numbers moved:

| spec anchor | master `ccdd5f1e` | drift |
|---|---|---|
| `maxStuckRounds` at `internal/agent/loop.go:269` | `internal/agent/loop.go:269` | +2 |
| `stuckTextPrefixChars` at `internal/agent/loop.go:263` | `internal/agent/loop.go:265` | +2 |
| `maxStuckToolfulRounds` at `internal/agent/loop.go:316` | `internal/agent/loop.go:316` | +2 |
| `stuckState.tripped()` at `internal/agent/loop.go:1906` | `internal/agent/loop.go:2609` | +166 |
| `toolStreakState` at `internal/agent/loop.go:1969` | `internal/agent/loop.go:2129` | +160 |
| its polling exemption at `internal/agent/loop.go:2527` | `internal/agent/loop.go:2509-2532` (fields) / `observeResults` `internal/agent/loop.go:2637-2642` | moved, same logic |
| `notifyLoopStuck` at `internal/agent/loop.go:1920` | `internal/agent/loop.go:2086` | +166 |
| `MaxIterations: 0` at `internal/agent/loop.go:2424` | `internal/agent/loop.go:2565` | +141 |
| `sessionWallclockCap()` at `internal/agent/loop.go:1851`, called `:2683` | `internal/agent/loop.go:1956`, called `internal/agent/loop.go:2868` | +105 / +185 |
| `Screen.Contents()` `screen.go:427` | `internal/lineedit/screen.go:427` | **exact** (this file did not move) |
| bash 120 s absolute timeout `internal/tools/bash.go:38` | `internal/tools/timeout.go:30` (`DefaultBashTimeout`), idle `:37` | **drift is real and the spec is wrong about the file**: the timeouts were consolidated into `internal/tools/timeout.go`; the values (120 s / 120 s) are what the spec claims |
| 352-… no. | | |

Nothing in §6.3's argument depends on a line number; the drift is recorded so the next
reader does not "fix" a moved reference by moving the code back.

## 2. Mechanism chosen, and why this one

**The bound is on the missing quantity: consecutive rounds that only LOOKED, with no
mutating tool call between them.** Not on sameness (that is detector ②) and not on
tool-lessness (that is detector ①).

- `internal/agent/toolclass.go` (new) — the two classes as a single definition
  (Rules 1b/13/14), keyed on `internal/toolnames` constants:
  **observers** = read, bash, code_search, keyword_search, web_search, web_fetch,
  git_blame, vision_analyze, screen_capture; **mutators** = write, edit, task,
  task_submit, memory, text2img.
- `internal/agent/loop.go` — `observationState` + `observationRounds()` +
  `notifyLoopStuckObservation()`, fed at the **same single feed point** as detectors
  ①② (immediately after response extraction, before the `finish_reason` switch), so no
  `continue` path — truncated round, end-turn nudge, completion checkpoint — bypasses it.
- Notify wire name `loop-stuck-observation`, **declared** in `internal/tools/tasknotify.go`
  (`taskNotifyEventLoopStuckObservation` + exported `NotifyEventStuckObservation`, and
  added to the `taskNotifyAlarmEvents` registry so `GOPI_TASK_NOTIFY_EVENTS=all` sees it)
  and **aliased** in `internal/agent` — one spelling, no mirror literal.
- `internal/runend/runend.go` — `StuckObservation` reason (`stuck-observation`), in
  `engineReasons`, so the row is marked TRUNCATED like its two siblings (Rule 30/Rule 5
  honesty path already built for ①②).

### Reset semantics (each one is a regression guard)

| round | effect | why |
|---|---|---|
| ≥1 mutator tool | streak 0 **and** re-arm `2N` exempt look rounds | the action class the epic names; the exemption keeps the verification burst after real work from being billed as a loop |
| `job` (polling) | treated as ACT (never a look) | detector ② owns polling; bug055's changing-result exemption is load-bearing — this bound must not double-bill a legitimate wait |
| no tool_use | **RESETS** the streak | measured decision (§3.2): a round spent talking is not a round spent looking; the passthrough variant bills long productive research sessions |
| unknown tool name | treated as ACT | the bound never widens itself over a tool added later (`TestToolClassesCoverTheToolSurface` fails on a class-less new tool) |

The exemption is measured in **look rounds**, so a run survives **3N** looks after a
mutation (N exempt + 2N to trip) — linear, not quadratic.

### Threshold — and the measurement that MOVED it

`maxObservationRounds = 40`, overridable by `GOPI_STUCK_OBSERVE_ROUNDS`
(unset → 40, `"0"` → disabled, positive → that many, invalid → default + stderr
warning; `observationRounds()` is the ONLY reader, Rule 2).

The threshold started at 24 and the corpus measurement **overruled it** (§3.2): 24
would have terminated 6 real sessions, one of them the longest streak in the whole
corpus (32 consecutive diagnostic `bash` rounds in session `42cfe7ab`, after which
the run acted). 40 sits above the corpus maximum, so at the shipped default the
bound fires on **zero** of the 2317 measured sessions. That is a deliberate
placement, and the honest reading of it is in §3.3.

## 3. Measurement — real session corpus (`/home/user/.gopi/sessions`)

Scanner: `/home/user/.gopi/sessions/01a0c2b1-…/obs_scan.py` (distribution) and `obs_sim.py`
/ `obs_sim2.py` (full simulation of the detector's state machine over every
session); outputs `obs_summary.json`, `obs_sim.json`, `obs_sim2.json` in the same
directory. The round grammar and the two tool classes were **fixed before the run**
(they are in the scripts' docstrings): an *observation round* is a round whose every
tool_use is in the observer class; an *act round* is anything else; `job` is not an
observer tool.

### 3.1 Scope

**3700** session `.jsonl` files, **2317** of which contain at least one parseable
round. (`obs_scan.py` walked 3716 files; the difference is files with zero rounds.)

### 3.2 The numbers, and what they forced

Streak = the longest **unbroken** run of observation-only rounds in a session, with
`no-tool` rounds resetting (the shipped semantics) — full detector simulation,
including the `2N` post-act exemption:

| statistic | value |
|---|---|
| p50 / p90 | 0 / 2 |
| p99 | 6 |
| p99.9 | 24 |
| **MAX** | **32** — session `42cfe7ab`, 32 consecutive `bash` diagnostic reads, which then acted (a genuinely productive deep investigation; its per-round commands are in `obs_sim2.json`) |
| sessions that would trip at N=8 / 12 / 16 / 20 | 75 / 30 / 13 / 7 |
| sessions that would trip at **N=24** | **6** |
| sessions that would trip at **N=32** | **1** (that same session) |
| sessions that would trip at **N=40** | **0** |

The variant that does NOT reset on a no-tool round (streak survives narrative
rounds) was measured too, and is the reason the shipped semantics reset:

| variant | p99 | p99.9 | max | trips @ N=24 |
|---|---|---|---|---|
| **reset on no-tool round** (shipped) | 6 | 24 | 32 | 6 |
| passthrough on no-tool round | 43 | 96 | **139** | **150** |

The passthrough variant glues two separate research windows into one streak; 150 of
2317 real sessions would be truncated at N=24, which is not a bound on a defect but a
bound on long sessions. So: **reset**, and the threshold placed above the corpus
maximum.

### 3.3 Honest reading — the loop does not occur in this corpus

At the shipped default (40) the bound fires on **zero** measured sessions. Read
literally, the answer to the epic's question is **a measured negative: no
observation-without-act streak longer than 32 rounds occurs in the real corpus, so
today this bound is prospective, not corrective.** Five things qualify that, and none
of them rescues it into a "yes":

1. **The corpus predates the surfaces §6.3 is about.** The observation entries the
   spec names (`screen_capture`, a live TUI read, `vision_analyze`) are rare-to-absent
   in it (the spec's own adoption audit: `vision_analyze` 13 tool_use blocks over 400
   sessions, `screen_capture` 7, and zero `bash` calls mentioning tmux/`drive_ui`). A
   loop over surfaces the model had not yet been given cannot appear in a corpus
   gathered before it had them.
2. **A streak is a coincidence of class, not proof of intent.** 20 consecutive `read`
   rounds is a research pass; the same count of *identical-looking* looks repeatedly
   re-reading a changing pane is the loop. Tool names cannot separate them — which is
   exactly why the threshold sits above the whole distribution rather than at its
   knee, and why the resets are ACT-based.
3. **The corpus is successful runs.** A session that was abandoned mid-loop still
   writes its jsonl (the 12h loops of incidents t-1788173679 / Bond 718319 are in the
   family), but a session killed by an operator or a wall-clock cap writes its tail
   only if the close-out ran — so the corpus is biased toward runs that ended.
4. **The one shape the corpus DOES contain is detector ②'s, not ③'s.** The longest
   byte-identical-fingerprint runs in the corpus are `bash` polls with fixed
   arguments, i.e. the 718319 shape `toolStreakState` already bounds. Detector ③'s
   distinct shape (changing looks, no act) is what the corpus lacks.
5. **§7's could-not-determine list names what is still open**: whether *any* real
   model has ever entered this shape. Answer as of this measurement: **not provably,
   in this corpus — 0 confirmed instances.** The streak list is dumped with file
   paths so the next reader can read the 6 sessions that would trip at 24 and judge
   them by transcript rather than by count.

So the deliverable is honest in both directions: the bound is real, tested, wired to
the existing alarm channel, and it fires on the defect shape when fed one; and the
measurement says the defect has not been observed yet, with 40 chosen so that it
would not have truncated anything the corpus shows.

## 3.4 Rule 31 — the surface was RUN end to end (not only tested)

The changed surface is a production CLI run, so it was executed for real against a
mock Anthropic-streaming endpoint (the mock is committed as evidence, not into the
tree: `/home/user/.gopi/sessions/01a0c2b1-…/e2e/mock_llm.py`) — real binary, real loop, real
detectors, real notify pipeline:

```
gopi --home <evid>/home2 --settings <evid>/settings.json \
     --base-url http://127.0.0.1:8731 --api-key test-key --model mock-model \
     "watch the deployment until it settles"
   with GOPI_STUCK_OBSERVE_ROUNDS=4 GOPI_TASK_NOTIFY_EVENTS=loop-stuck-observation
        GOPI_TASK_NOTIFY_CMD='cat >> <evid>/notify2.jsonl'

→ ⏱ 0.9s | 4 iterations | FAIL
→ the capture received:
  {"event":"loop-stuck-observation", "...", "status":"loop-stuck-observation",
   "summary":"agent loop stuck (observation without act): 4 consecutive
   observation-only rounds with no act (streak=4 looks=4 threshold=4) — run terminated"}
```

The mock answers every request with a **different** look (`read /var/log/step-N.log`,
then `bash tail …`, `code_search`, `keyword_search`, `web_fetch`), i.e. the exact
shape the spec names: new information each round, no act. `4 iterations` and the
delivered event prove the bound is reachable through the production entry point, the
env override reaches it, and the alarm travels the real notify channel.

**Two failed attempts are recorded because they are the kind of mistake the next
runner will make**: (a) the first run used `DEEPSEEK_BASE_URL` while `ANTHROPIC_BASE_URL`
was also exported — the Anthropic spelling wins, so the run silently went to the real
API (2 minutes, real tokens); (b) `--base-url`/`--api-key` **flags** beat the env
vars, which is why the invocation above passes them explicitly. The lesson: on this
host, pin the endpoint with flags and unset the Anthropic family first, or your
"mock" run is a production run.

## 4. What a real model was observed doing (the honest section)

**No live model was run in this task.** Every green/red proof below drives a mock LLM
through the real loop with the real feed point, real detectors and the real notify
pipeline (`GOPI_TASK_NOTIFY_CMD` capture), which is the same instrument the two
existing detectors were validated with (bug055 / incident t-1788173679). The bound's
*adoption* by a real model — i.e. whether the model ever enters this shape in the
first place — is measured from the corpus (§3), not by running a session whose purpose
is to be caught.

Methods that could NOT be used, and why:

- **A live session aimed at the loop** — would require a task whose only honest
  completion is to look forever; manufacturing one is a synthetic test, not an
  observation (Rule 27's prohibition, applied to model behaviour rather than devices).
- **Read back the intent of the top streaks** — took too long inside the 60 min budget
  once the detector work was green; the streaks are listed with their files, so the
  next reader can do it cheaply.

## 5. Files changed

- `internal/agent/toolclass.go` (new) — the two tool classes + `observationToolClass`.
- `internal/agent/loop.go` — constants, `observationRounds()`, `observationState`,
  `notifyLoopStuckObservation()`, `toolNames()`, construction + the feed point.
- `internal/agent/loop_observation_test.go` (new) — 8 tests (§6).
- `internal/tools/tasknotify.go` — the wire-name declaration, the exported alias, the
  registry entry.
- `internal/runend/runend.go` — `StuckObservation` reason + `engineReasons` membership.

Not touched: `internal/uiclient`, `feat/gopi-cli-bubbletea-landing`, detector ①② logic,
`toolStreakState`'s polling exemption, `go.mod`.

## 6. Tests (all committed, all green)

`internal/agent/loop_observation_test.go`:

1. `TestObservationBound_EnvOverride` — unset (=empty) →40, `"0"`→disabled, `"3"`→3,
   `"bogus"`→default (single reader).
2. `TestObservationBound_StateUnit` — accumulate; ACT resets + arms the 2N exemption
   (3N total); polling is an ACT; a no-tool round RESETS; unknown name is an ACT; a
   mixed round with one mutator is an ACT.
3. `TestToolClassesCoverTheToolSurface` — every `claudeToolDefs` name has a class, and
   the tables carry no stale name (a new tool without a class fails).
4. `TestAgent_ObservationLoop_RedProofUnboundedWithoutBound` — **the red proof**:
   bound disabled ⇒ 40 look rounds run to the iteration budget, no loop-stuck-family
   alarm at all.
5. `TestAgent_ObservationLoop_BoundTripsAtDefaultAndNotifies` — **green**: trips at
   exactly `maxObservationRounds` rounds, error names the event, notify delivered with
   the family payload.
6. `TestAgent_ObservationLoop_ThresholdOverrideTrips` — threshold 3 trips at 3 (the
   threshold is honoured, not hardcoded).
7. `TestAgent_ObservationLoop_LegitimateJobPollSurvives` — a running-job poll at
   threshold **1** still runs its whole budget, no observation alarm (bug055 guard).
8. `TestAgent_ObservationLoop_PostWorkVerificationSurvives` — the edit/verify cadence
   survives 28 rounds at threshold 3.
9. `TestObservationEventNameIsDistinct` — the three wire names are distinct, the new
   one is in the `loop-stuck` family, and the agent alias equals the tools declaration.

## 7. Not done / could not determine (honest)

- **No live model run** (§4) — the bound's stopping power is proven against the real
  loop with a mock model, not against a real session that was actually stuck. A
  session that would trip ③ is by construction a broken one, and no existing corpus
  session contains a confirmed instance (see the measurement).
- **Intent of the corpus streaks not read** — the top streaks are listed with files for
  the next reader; classifying each as "research pass" vs "look-without-fixing" needs
  transcript reading.
- **The bound is prospective at the shipped threshold** (§3.3) — at N=40 it fires on
  zero of 2317 measured sessions. Setting `GOPI_STUCK_OBSERVE_ROUNDS=16` would bound
  the 356-session tail of the current distribution (measured), at the cost of
  truncating long-but-productive research runs; that trade is the operator's, and the
  default is deliberately the conservative side of it.
- **`context_trim` classified as ACT by default** — a round carrying only
  `context_trim` is neither a look at live state nor a state change; it is treated as
  an ACT (the unknown-name bucket) so the bound never bills a context operation as a
  look. This is a judgement call, stated here rather than hidden: a pathological run of
  *only* `context_trim` rounds is not bounded by ③.
- **No `text2img`/`screen_capture`-specific corpus evidence** — those are in the
  observer class but appear too rarely in the corpus to move any number.

## Evidence (self-filed)

- Red proof (bound disabled), command + result: the pass output of test 4 with
  `GOPI_STUCK_OBSERVE_ROUNDS=0` — the run reaches all 40 look rounds and the error is
  `max iterations`, with no `"event":"loop-stuck*"` in the notify capture.
- Green proof, test 5: run terminates at exactly `maxObservationRounds` (40) rounds
  with `... (see GOPI_TASK_NOTIFY loop-stuck-observation)` and the capture carries
  `"event":"loop-stuck-observation"`.
- Regression, test 7: 20 rounds of `job action=output` on a running job at
  `GOPI_STUCK_OBSERVE_ROUNDS=1` → `max iterations`, no observation alarm.
- `gofmt -l` on the touched packages: no output. `go vet ./internal/agent ./internal/tools
  ./internal/runend`: exit 0 (see the report for the whole-tree run).
- Anchor table §1: the two `grep -n` invocations in the report's `## Verification`.
