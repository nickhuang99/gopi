<!-- doc-class: historical -->
# Requirements 06/12 — Agent Loop, Tasklist & Working Area

**Module B6 of the reverse-requirements series.** Reverse analysis: the built system is the
evidence; the inferred user need follows from it; the value of this document is the gap (§6),
not the inference.

*On the two numbers in the title: this series carries two live id schemes (the umbrella states and
justifies that in `00-method-and-standards.md` §7.2). `06` is this document's **file ordinal within
the module documents**; `M5` is the **module id** in the boundary map, and the epic that dispatched
this work calls it **`[7/15]`** in the briefing index (`01-per-module-briefings.md`: `## [5/14] M5 —
Working a goal to a verified deliverable (the agent loop)`). All three name this one module; none of
them is stale. A reader arriving from the epic should read `06-agent-loop.md` = `M5` = `[5/14]`.*

| | |
|---|---|
| **Branch / worktree** | `feat/req-b6-agent-loop` @ `~/workspace/gopi-req-b6` (Rule 6a) |
| **Citation base** | every `file:line` below is read at master `17cec86b`, the revision the analysis was written at |
| **Verified at** | master `812f9a60`, branch `docs/req-06-agent-loop-verify`, worktree `~/workspace/gopi-req-06-verify` (2026-09-18) |
| **Deliverable** | this file, and nothing else (`git diff --name-only 812f9a60..HEAD`) |
| **Verification** | docs-only: no code, no test, no `make fmt` (Rule 5/7); the gate for a docs branch is the single-file diff, recorded at the end |
| **Measured** | analysis pass: live reads of `~/.gopi/**/*.jsonl`, `~/.gopi/**/*.dump.json`, `~/.gopi/memory.db`, `~/.gopi/settings.json`, 2026-09-16/17. **Verification pass (2026-09-18): every count and every citation re-measured** — resolution result in `## Verification`, re-measured table in §6.5 |
| **Overlap** | sibling `01a0ab12-3e7d-721c-84f2-83972d4a47a0` (same module, other series). At the analysis revision no `devdoc/requirements/agent-loop.md` or `06-*.md` existed in any branch (`git log --all --diff-filter=A -- 'devdoc/requirements/*'` → `00/01/02/03/05/07` + umbrella only), so nothing was duplicated. That sibling has since **merged this document into master**, so this pass verifies and repairs it instead of re-writing it (Rule 23: the prior branch is read, never committed on). |

## Boundary

**In scope**

- `internal/agent/loop.go` (5,722 lines) — the run loop, its caps, its nudges, its exit decision.
- The task-list contract: `internal/agent/tasklist.go` (354), `heartbeat.go`.
- The working-area discipline as it affects loop shape: `pendingTaskAreaConclusionGuard`
  (`loop.go:4412`), `injectTaskAreaConclusions` (`:5508`), `trimCommittedTaskArea` (`:5546`),
  `taskAreaCarryForward` (`:2572`).
- Run residue: `crashdump.go` (531), `crashdump_sweep_gate.go` (594), `session.go`'s orphan
  classification (`session.go:504`–`:536`, `:735`, `:775`).
- Run introspection and its cost: `internal/agencyscan`, `internal/pricing`, `internal/thinkmode`,
  `internal/agent/trajectory.go`.
- Liveness/interruption as the *loop's* property: the wall-clock cap, the two stuck detectors,
  the interruption contract. Context **economics** (compaction budgets, post-compaction
  survival) belongs to B5 and is cited there, not re-analysed here.

**Out of scope**

- The 17 tool implementations and the routing that persuades the model to use them — B7.
- What a *task row* means and how it is closed out — B1.
- The notification transport (webhook/command, event filters) — B10. This document judges only
  **which run-level facts reach that transport and whether the operator can act on them**.
- The CLI's rendering of any of this — B8.

## Boundary why — the loop is not a directory

`internal/agent/` holds the chat surface (B9, `chat_ops.go`, 376) and the CLI-adjacent task
registration (B1, `taskregister.go`, 573) beside the loop, so the package cannot be the boundary;
the umbrella's M1/M4-adjacent mismatch is restated here per §7 of the umbrella. The
user-visible responsibilities this document owns are four, and each is statable without the
package:

1. **A run has a shape I can watch** — a plan is declared, items advance, and the log shows it.
2. **A run cannot spin forever** — a loop with no progress stops itself, and says so.
3. **A run leaves a forensic trace when it dies** — an operator finds out *that* it died and *why*.
4. **A run is finite by declaration, not by luck** — the environment boundary is a policy the
   operator sets and the stopped run is reported at it.

Two further properties are *mechanism* inside the boundary, and are analysed as such rather than
promoted to needs: the working area's compaction survival (B5 owns the budget; here it matters
only as "the plan outlives the conversation"), and the harness's power to refuse a free-text
"done" (a contract between the harness and the model, whose user is a future maintainer).

---

## 1. Purpose (product-owner language)

A single automated work session — the thing that runs when I hand a machine a task — has to be
usable, and "usable" means four separate things:

1. **I can see the shape of the work while it happens.** Before spending anything, the session
   says what it intends to do, and then it advances through that list one item at a time, and each
   finished item leaves a short, machine-kept record of what was decided. I should be able to open
   the log afterwards and read the plan and the conclusions without re-reading the transcript.

2. **It cannot run forever by accident.** A session that is not making progress — repeating itself,
   or waiting for something that will never change — must stop on its own, well before it has cost
   me a night. No session may burn hours because nobody was watching.

3. **When it dies, I find out — and I find out why.** A session that crashes, is killed, or loses
   power must leave behind enough to answer "what was it doing". Silence is the failure I am
   guarding against: a session that simply disappears with no record is worse than one that fails
   loudly.

4. **Its length is a policy I set, not a surprise.** Even a healthy session must be bounded by a
   limit I can choose, and when that limit stops it I must be told that *that* is what happened —
   not left to infer it from a duration. And the stop must be orderly: the work done so far stands,
   the record is closed, and I can tell the difference between "it finished its job" and "the clock
   ran out".

5. **Its abilities are a closed, knowable set.** A session can do exactly what the product says it
   can do, that set is written down in one place, and it changes only when a person changes it. It
   never grows a capability at runtime: no plugin is loaded, no extension is discovered, and no
   verb works merely because the model emitted its name. This is what makes "what can the machine
   do" answerable without reading the source — a question the operator asks before every dispatch.

The **need** behind all five is a single sentence: *an automated session is only tolerable if I
can watch it, bound it, interrupt it, account for it after it is gone, and know the exact set of
things it can do.* A session that is fast but opaque, capable but unbounded, or whose surface can
grow without my knowing, is not worth having.

Note what the purpose is **not**: it is not "the model should plan like a person", and not "the
model should never fail". Nothing here requires the session to be clever or infallible. It
requires the session to be *legible and bounded* — and (§1.5) to be *knowable*, in the sense that
the set of things it can do is written down once and does not change without a person changing it.

## 2. Actors served

| Actor | What they get | Evidence |
|---|---|---|
| **The operator (nick)** | The only actor who is *harmed* by a runaway. He gets: a declared plan, progress he can watch, two independent stop conditions, a wall-clock ceiling he sets himself, and a forensic record per death. | The two 2026-09-01 incidents are operator-observed and operator-reported (`t-1788242328`, `t-1788259388`); the wall-clock knob is documented for him (`loop.go:305`–`:319`); the sweep is a startup report he reads (`cmd/agent/main.go:974` (`agent.BackfillOrphanCrashDumps`, def `internal/agent/crashdump.go:288`; was `:741` at `17cec86b`)). |
| **The agent (the model in the run)** | Binding instructions that make its compliance checkable: exactly one item in progress, completion only through a committed conclusion, a memory layout it can trust, and a stop it is allowed to take. It is the *executor* of the loop contract, and the *beneficiary* of the caps — the caps are the harness's assertion that the model's own judgement is not trusted to end a run. | `loop.go:645`–`:647` (the rule text it receives), `loop.go:340`–`:342` (the guard nudge), `tasklist.go:16`–`:21` (the three states). |
| **The serve/worker supervisor and the next session** | A bounded child: it can assume a session ends, the row is closed, and the residue is attributable. A resumed session inherits its predecessor's committed conclusions. | `finishSessionTask` (`taskregister.go:321`) is the end hook the supervisor keys on; carry-forward is wired at `loop.go:2572`. |
| **The future maintainer** | An explanation of why the loop's guards exist, from the incidents rather than from a philosophy. This is the actor the design comments already serve, at length, at each constant. | Every cap constant in `loop.go:80`–`:342` carries its incident in the comment; `crashdump_sweep_gate.go:1`–`:95` carries its measured cost table. |

**The honest asymmetry**: the *model* is the loop's executor, but it is not the actor whose need
the caps serve. The caps exist precisely because the operator does not delegate the decision to
stop. Any requirement in §4 that reads as a rule for the model is, from the operator's side, an
untrustworthiness assumption — and §8 labels which of these I can evidence and which I inferred.

**The command-and-control distinction this module must not blur** — it is the epic's acceptance
criterion for this document, so it is stated here and used as a test throughout §6:

| | **Harness command-and-control** | **Operator-visible product** |
|---|---|---|
| What it is | the mechanism by which the harness governs the model: the `task` verb set, the item states, `maxTaskAreaNudges`, the committed-conclusion store, the pending-set guard, carry-forward, the dispatch switch's `default:` branch | the legibility the operator consumes: *that the run is running*, *that it finished*, *that it died*, *what it decided*, *what it could do* — the task list as a **display**, the run's exit reason, the plan a reader can see, the tool inventory |
| Who its user is | the harness itself, plus the *next* round and the *next* session (a resumed run reads its predecessor's conclusions) | the operator (nick), and the future maintainer reading a report |
| How it is judged | by rule-adherence and by the negative test — does it *fail to stop* a non-progressing run, does it *mis-close* an item | by whether a person can answer their question **without reading the transcript or the source** |
| Evidence it is needed | the harness's own contract (W4's "框架有最终话语权"; the nudge cap `loop.go:332`) | the operator's verbatim question ("are these dead?", W6) and the two founding incidents (W1) |
| **What must NOT be claimed for it** | that the operator benefits from the *mechanism* — he benefits from the *legibility* the mechanism buys. A committed conclusion is not a feature he reads; an item state is not a thing he asked for | that legibility can be *required* to be produced by the machine-checked list — the evidence (§6.3.3) shows half the fleet reaches legibility by the other route |

Read this way, three parts of the module fall on different sides and are analysed accordingly: the
**liveness caps and the residue sweep** are product (the operator is the harmed party, W1/W3/W5); the
**working area** — pending set, conclusions, carry-forward — is command-and-control whose justifying
need is product (W4, and §6.4.1 states the residual doubt); the **closed tool surface (W9/§1.5)**
with its pinning test is *both*, which is exactly why §6.6's phantom matters — a surface defect is
invisible to the operator, who cannot see what the model was silently refused.

## 3. End-to-end workflow

**A. Entry**

1. A session starts (a prompt, a `task run` child, a chat turn, a resumed session). A start event
   is written first, and the run directory is chosen by session-dir layout
   (`session.go:146`; `loop.go:2143`–`:2149`).
2. The loop arms its checkpoints before the first model round: a memory-consult checkpoint, the
   task-list nudge state, and the deadline (`loop.go:2663`, `:2666`, `:2678`–`:2680`).

**B. The round cycle (the loop is ReAct-shaped: ask → act → repeat)**

3. **Ask.** One model round is issued under a context that carries the session deadline
   (`loop.go:2706`–`:2717`) and a ticking clock the model can see (`loop.go:645`).
4. **Act.** Tool calls are executed in one batch, sequentially or in parallel
   (`loop.go:3290`–`:3300`, `:3005`).
5. **Judge liveness — three independent detectors, checked every round**:
   - *no-tool repetition*: N consecutive rounds with no tool call and the same leading text
     (`maxStuckRounds = 5`, `loop.go:254`; detector `:1866`–`:1901`; tripped at `:3039`).
   - *identical tool repetition*: N consecutive rounds with the same tool and byte-identical
     parameters (`maxStuckToolfulRounds = 10`, `loop.go:294`; detector `:1958`; tripped at `:3057`).
     Polling tools are exempt by name (`pollingTools`, `:367`) and instead compare the *results* —
     a frozen polled target trips it (`:3280`–`:3296`).
   - *the session ceiling*: elapsed time against the deadline (`loop.go:2858`, `:3005`).
6. **Advance the plan.** A re-emitted task list is parsed and validated — no item may vanish, no
   duplicate ids, only the three states (`tasklist.go:18`–`:22`, `:268`–`:296`). A missing end
   marker is recovered by balanced-prefix parsing rather than dropping the list (`tasklist.go:119`–
   `:124`, `:186`, `:235`–`:290`).
7. **Keep the working area honest.** On a round that tries to leave, the guard asks whether every
   tracked item has a committed conclusion; if not it injects one nudge naming the missing ids and
   continues the run (`loop.go:4412`–`:4445`), at most `maxTaskAreaNudges = 3` times (`:332`).
   Committed conclusions are re-injected into the context and their analysis rounds are folded
   (`loop.go:3383`–`:3384`).

**C. Exit — five distinguishable ways out** (all verified as distinct code paths at `17cec86b`)

| # | Way out | Trigger | What the operator sees |
|---|---|---|---|
| 1 | **Declared completion** | `maxConsecutiveEndTurnRounds = 2` consecutive no-tool rounds (`loop.go:89`), or one such round when the list is all-completed with a substantive answer | `session_end` exit 0; the notification line says `task finished` |
| 2 | **Stuck, no tool calls** | 5 identical no-tool rounds | `session_end` **exit 1** + an error body naming the loop; notification event `loop-stuck` |
| 3 | **Stuck, identical calls** | 10 identical tool rounds (or identical poll results) | same, event `loop-stuck-toolful` |
| 4 | **Session ceiling** | elapsed ≥ the wall-clock cap | `session_end` **exit 0**, no error body; event `session-wallclock-exceeded`; a stderr line |
| 5 | **Interrupted / crashed** | the interruption contract (`loop.go:2464`), or a panic → the crash-dump re-panic (`crashdump.go:228`–`:251`) | exit 1 + a dump file + a failed close-out; or a dump written by the next start's sweep |

**D. After the run**

8. The end hook closes the ledger row and fires the notification (`taskregister.go:321`,
   `crashdump.go:216`).
9. **On the next start**, orphan sessions are classified and dumped, and the sweep is gated so it
   does not pay a full tree walk on every start (`crashdump_sweep_gate.go:1`–`:95`; caller
   `cmd/agent/main.go:974` (`agent.BackfillOrphanCrashDumps`, def `internal/agent/crashdump.go:288`; was `:741` at `17cec86b`)), with a report line to the operator.

**Entry condition**: a session is created and a deadline is armed. **Exit condition** (the one
that matters): the run reaches a terminal state, the ledger row is closed, and — for every cause
except a graceful finish — a *machine-readable* record of the cause exists where a later reader
will look.

---

## 4. Why (with citations)

Every claim below is cited to a ledger row, a `file:line` at `17cec86b`, a GOPI.md rule, or a live
measurement in this session. Anything else carries `[opinion]`.

**W1 — "It cannot run forever" was demanded as a bug report, twice, and the operator supplied the
diagnosis.** The two founding incidents:

> `t-1788242328` (verbatim): "背景（2026-09-01 两起 12h+ 循环事故：t-1788173679 4276 iter 每 20s
> 一轮'已完成：HEAD'纯文本循环 … agent loop 缺卡死兜底，Rule 7 的 stop 只靠 LLM 自觉）。"

And the second, after the first guard missed the same class of loop:

> `t-1788259388` (verbatim): "背景（nick 质疑揭穿：t-1788173679 的 43187s/4804 轮循环——每轮 bash
> git log -1 + 纯文本'已完成：HEAD xxx'——bug047 的 stuck-net 未触发。根因：stuck.observe(content,
> len(toolCalls)>0) 的语义=**tool 轮重置 streak**——监控循环每轮都有 tool_use（查 HEAD）→ streak
> 永远归零→永不 terminate。"

The requirement is therefore **not** "the model should be able to stop itself" — the loop had that
and it was not enough. It is "**the environment must stop a non-progressing session even when the
model keeps producing plausible output**". The third incident (`t-1788278732-2`, verbatim: a
`bash sleep 115; ssh …` monitor loop repeated 272 times, "总烧 10h15m") is the same shape again.

**W2 — The first guard must not kill honest waiting.** The very next report is a *counter*-requirement:

> `t-1788309830` (verbatim): "stuck-detection (toolStreak, bug055) false-positives on polling/waiting
> tools — job_output loops kill legitimate tasks. BLOCKS the single-node closed loop."
> Three real victims the same morning, all "legitimately WAITING on a background job".

The code answers this directly: polling tools are exempt by name and judged by their *results*
instead (`loop.go:357`–`:367`, `:3280`–`:3300`). Acceptance of W1 therefore has a bound: **stops
must cost zero honest waits** — and this is measurable (§6.5).

**W3 — A run's length is a policy, and the operator set it.** The operator's own framing is the
12h+ unattended run; the ceiling is his switch, not the model's:

> `loop.go:313` — `defaultSessionWallclock = 8 * time.Hour`, with the override documented as
> "GOPI_SESSION_MAX_WALLCLOCK=0 means UNLIMITED (the cap is disabled)". The incident is named in
> the same comment: "the 12h+ loops ran unattended overnight".

**W4 — The plan is a contract between the harness and the model, and the harness holds the last
word.** The design is the operator's, stated in the dispatch text of the T2 task:

> `t-1788555938` (verbatim): "设计意图：LLM 无权通过自由文本宣布整体完成——完成必须逐 item 经
> write_task_area 提交，框架有最终话语权。"

Two bounds come attached and are *part of the requirement*: a single-step session with no declared
list is never intercepted, and after 3 nudges a refusing model is allowed to exit (`loop.go:324`–
`:342`, `:4400`–`:4412`). A verification task exists to prove the mechanism reaches the wire —
`t-1788524560` (verbatim: "prove from the RAW LOG that the memory-aware design actually drives the
LLM — not from any implementer's self-report").

**W5 — A dead run must leave a trace, and the sweep must not cost a start.** The crash-dump epic's
own design note states the contract (`crashdump.go:19`–`:28`): one collection path feeding the
session-end error block, a dump file, and the ledger's failed summary. The gate on *when* the sweep
may run exists because the sweep was measured to dominate startup:

> `crashdump_sweep_gate.go:1`–`:25` (measured, 2026-09-12): "DetectOrphanSessions 1.835 s /
> 455.5 MB alloc churn … Before any gate this ran on EVERY agent start."

So the requirement is not "sweep on every start" — it is "**a newly dead session is detected on the
next start, and a start does not pay a full tree walk**". The gate's own doc names the rejected
alternative (`:27`–`:32`): a calendar-day skip is refused because it makes the acceptance
unreachable for up to 24 h.

**W6 — The task list is a legible instrument, and it was repaired after the operator asked whether
the rows were dead.** `briefings/brief-task-list-state-honesty.md` opens with the operator's
question — "症状（operator 亲问 \"are these dead?\"）" — and the finding: "**一个仪表盘不该让使用者
无法判断状态**". The list's three-state vocabulary is a 2026-09-07 review decision recorded at the
constant (`tasklist.go:16`–`:22`, matching Claude Code/Codex), and the honesty brief belongs to B8's
display surface but constrains the loop: **the loop must emit enough state for the display to tell
"running" from "finished but unrecorded" from "died"**, which is why the loop writes
`tasklist_issued_at`/`last_toolcall_at` heartbeats (`t-1788040730`, verbatim: "①新列
tasklist_issued_at … ②新列 last_toolcall_at（这就是 nick 说的 heartbeat 字段名）").

**W7 — Decided constraints on this module** (GOPI.md; each is a requirement even though written as
discipline): **Rule 19** — the ONE legal panic site is the run's own re-panic, and it is annotated
as such (`crashdump.go:226`, `:251`); **Rule 3** — the tool surface is fixed at 17 and the loop's
tool definitions are the source of truth, pinned by `TestGOPIRule3_MatchesClaudeToolDefs`
(`gopidoc_sync_test.go:54`, and GOPI.md:87); **Rule 11** — a harness mechanism that the model never
uses is worthless, which makes the task-list/working-area protocol's *adoption* part of its
acceptance (§6.5); **Rule 7**'s DONE definition is delivered to the model through this loop
(`loop.go:647`).

**W8 — Bounded cognitive budget is a requirement, mechanically enforced.** The assembled system
prompt must stay under 4,500 characters — "the density measured against dsh's 4.1K-character prompt
(competition #4)" (`prompt_test.go:66`–`:70`) — and every block must be present (":71"–":80"). So
any new loop instruction competes for a budget that is already spent; the loop's own instructions
live in the *user* turn instead, which the same test notes is deliberate (`:346`).

**W9 — The run's abilities are a closed set, and the closure is machine-checked (Rule 3).** Not
every requirement on this module is about *stopping*; one is about *scope*. GOPI.md's Rule 3 is a
hard constraint written as discipline — "Tools Are Minimal — Don't Add New Ones" — naming 17 tools
and declaring `claudeToolDefs` the source of truth (re-verified 2026-09-18: the heading is
`## Rule 3` at `GOPI.md:85`, the 17-entry inventory line at `:87`). Its *reason* is stated as an
enforcement failure, not a taste: the rule once claimed 18 tools while `claudeToolDefs` registered
23, so a file read at startup as a constraint asserted a false inventory and forbade tools the agent
already had — "a stale list is not a docs bug, it is a prompt bug"
(`gopidoc_sync_test.go:47`–`:53`). The requirement has two halves and both are load-bearing:

- **17 is a product statement, not an implementation detail.** Measured 2026-09-18 at `812f9a60`:
  `claudeToolDefs` (`internal/agent/loop.go:849`, `var claudeToolDefs = []deepseek.ClaudeToolDef{`)
  holds exactly **17** `Name: toolnames.*` entries, and its pinning test passes —
  `go test ./internal/agent/ -run TestGOPIRule3_MatchesClaudeToolDefs` → `ok … 3.211s`. The test is
  at `internal/agent/gopidoc_sync_test.go:54`.
- **The closure is why the tasklist protocol has the shape it has.** Because no capability can be
  added at runtime, every ability the harness wants the model to have is *folded into an existing
  tool's verb set*. The `task` tool is the clearest case: `todo` (**1,492** calls), `write_task_area`
  (**410**), `task_evaluate` (13) and `tasklist` (1) survive in the store as **historical tool
  names — 1,916 retired-name calls in total** — and all were consolidated into `task` + an `action`
  verb, whose whole vocabulary is four actions (measured 2026-09-18: `commit` 5,522, `view` 3,675,
  `review` 1,641, `reframe` 1,244 calls). The consolidation is *complete*, not in progress: the last
  emission of any retired name is **2026-09-10** and none has been emitted since.

The consequence for the operator is the interesting part, and it is the acceptance in §5.9: "what
can the machine do" is answerable by reading one list, and that list cannot grow behind his back.
Any requirement in §6 that would add a capability must therefore be expressed as a **verb of an
existing tool**, and a requirement that can only be met by a new tool is — by Rule 3 — not a gopi
requirement at all (the same bounding consequence the umbrella draws for C1).

---

## 5. Acceptance in user terms

From the outside, with no access to the code, a session is correct with respect to this module when:

1. **The plan is visible before the work.** Within the first few completed steps the log contains a
   declared list, and thereafter the list only ever grows in completion — an item never disappears
   from it.
2. **Progress is per-item, and each finished item leaves a conclusion.** A reader of the session
   record can list what was decided without reading the transcript, and the harness — not the
   model's prose — decides when the run may end.
3. **A non-progressing session stops on its own, and the stop is labelled.** Observable: the
   session record's end carries a non-zero exit *and* an error body that says the loop was stuck
   and which signal tripped; the operator's notification stream shows the corresponding event.
4. **Honest waiting is not mistaken for being stuck.** Observable: a session that polls the same
   live background job for many rounds is not terminated, while the same poll against a *frozen*
   target is.
5. **No session exceeds the ceiling I set.** Observable: with the ceiling configured, session
   durations cluster at or below it — and the runs that end *at* it are no longer the longest runs
   in the store.
6. **A ceiling-stopped run is reported as such, and its work stands.** Observable: the operator can
   distinguish "finished its job" from "the clock ran out" from the record alone, and the
   close-out is terminal rather than failed-by-default.
7. **A dead run is detectable on the next start, without a costly start.** Observable: killing a
   session mid-run yields a report on the next start naming it; ordinary starts are not
   measurably slower for it.
8. **A killed session's evidence does not rot into a false accusation.** Observable: a session that
   later ends cleanly is not still reported as a crash.
9. **The ability set is closed and countable.** Observable: the written list of what a session can
   do (17) and the code's list agree, a build fails if they drift apart, and no verb works merely
   because the model emitted its name — an unknown tool name is an error, not a new capability.
10. **A run-level alarm actually arrives at the channel I configured.** Observable: an alarm event
    the product says it fires (the loop stopping itself, the clock stopping a run) appears in the
    operator's configured stream when it happens — not only in a process's stderr, and not filtered
    out by a subscription list that never learned the event's name.

The user does **not** ask for: a smarter stopping heuristic, a per-round token ceiling as a
substitute for a wall clock, a resumable stuck run, an agent that judges its own completion, or a
way for a session to acquire a capability at runtime (Rule 3 answers the last one before it is
asked). Nothing in the evidence requests them; where one has been *assumed*, it is named in §6.

---

## 6. Gap analysis — code against need

### 6.1 Serves

- **The liveness need is served, with two independent detectors and a named exemption.** Both caps
  are constants with incidents in their comments (`loop.go:254`, `:294`), the exemption is a data
  table not a special case (`loop.go:357`–`:367`), and the third signal — a frozen polled target —
  compares results rather than fingerprints (`:3280`–`:3300`). Each has a red-proof test pair that
  asserts both directions (`loop_stuck_test.go`: terminates / tool-use resets / divergent text
  resets / param change does not trip / disabled).
- **The wall-clock ceiling is real and the cap band is empty in production.** Measured: 4 of 1,184
  completed sessions ended within 100 s of exactly 8 h (28,800 s / 28,801 s / 28,801 s /
  28,897 s), all with exit 0 and 1,287–3,232 iterations. **No run in the store exceeds 8 h by more
  than 100 s** — the only two longer sessions (12.0 h, 23.2 h) both ended 2026-09-01, i.e. before
  the cap existed. The cap is not a paper constant.
- **The residue detector works and is honestly bounded.** The sweep gate is change-sensitive with
  three independent layers and an explicit statement of what the probe *cannot* see
  (`crashdump_sweep_gate.go:51`–`:76`), and it fails open on every uncertainty — "a marker that
  suppressed the sweep when it could not be believed would be the silent no-sweep failure this
  codebase fears most" (`:78`–`:83`), pinned by
  `TestOrphanSweepDue_VerdictTableFailsOpenOnEveryUncertainty`.
- **The working area survives both compaction and resume.** Committed conclusions are re-injected
  before the analysis rounds they replace are folded (`loop.go:3383`–`:3384`, ordering documented at
  `:1430`–`:1440`), and resume carries them forward (`:2572`).
- **The completion guard is adopted by the model.** Measured across 1,597 session files: the guard
  nudge landed as a real user message in **89 runs**, and the per-run count never exceeded **3** —
  exactly the cap (`loop.go:332`). The mechanism is not a paper contract either.

### 6.2 Falls short

1. **The ceiling-stopped run is indistinguishable from a declared completion in the durable
   record.** The cap's own code comment promises the operator will be told *why* (`loop.go:303`–
   `:311`: "the operator-facing signal that the run stopped at the environment boundary, not by
   declared completion"), and it is delivered — to **stderr** and to the **notification stream**
   (`loop.go:2734`–`:2741`). It is **not** in the session record: `session_end` carries only
   `elapsed_sec / exit_code / total_iterations / total_tokens` (`session.go:370`; verified on the
   four cap-band sessions by parsing their end lines). Consequence, measured: those four runs closed
   terminal with exit 0 — `t-1788648849` "completed / task finished", `t-1788720718`
   "completed / task finished", `t-1788721024` **"failed / task failed"**, and one row with no task
   binding — and none of the three is distinguishable, in the ledger, from a run that finished its
   job. The cap's *reason* is absent from both durable records. [inferred — weakest link: I verified
   the absence of a reason field in `session_end` and the terminal status of the rows; a
   cap-stopped run could in principle have failed later for an unrelated reason, which is exactly
   the ambiguity the gap asserts.]

2. **The two run-level alarms are filtered out by the operator's own event subscription — the
   filter, not the delivery, is the root cause.** Re-verified and sharpened 2026-09-18 (this
   replaces the earlier reading, which said delivery "reach[es] no configured target on this host";
   that was measured on 2026-09-17 and by 2026-09-18 the target directory existed and the channel
   was live):

   - The channel **works**: `~/.gopi/settings.json` configures
     `notify.cmd = "cat >> /home/user/.gopi/notify/task-events.jsonl"`, `/home/user/.gopi/notify/`
     exists, and the file holds **95,283 delivered events** (49 MB): `completed` 42,313, `claimed`
     30,047, `failed` 21,429, `stale` 1,454, `build-failed` 40.
   - Both run-level alarms are nonetheless **absent from that file** — `loop-stuck` **0** hits and
     `session-wallclock-exceeded` **0** hits — while they *are* present in the session records
     themselves (**43** session files carry `session-wallclock-exceeded`).
   - The mechanism is a gate, not a delivery failure: `NotifySyntheticEvent` returns **before**
     delivery unless the event name is in the configured allowlist —
     `if !cfg.enabled() || !cfg.events[event] { return }`
     (`internal/tools/taskheartbeat.go:40`–`:44`, quoted line `:42`). The configured allowlist is
     `claimed,in_progress,completed,failed,stale,build-failed`. **`loop-stuck`,
     `loop-stuck-toolful` and `session-wallclock-exceeded` are not in it, so the gate drops them at
     the source** — the same class of defect as the rule that motivated `TestGOPIRule3`: a written
     list that did not learn a new name, silently disabling the feature that depends on it (Rule 15's
     no-silent-failure shape, at the operator's end).
   - Consequence: the two facts W1 and W3 exist to deliver — "the loop stopped itself" and "the
     clock stopped the run" — are recorded in the session JSONL and delivered to the operator
     **never**. The names are defined and commented as "the operator-facing signal"
     (`loop.go:265`–`:280`, `:301`–`:305`), so the product claims a channel that the configuration
     closes.
   - [inferred — weakest link: the subscription list is per-host configuration, so this is a
     **default/config** gap rather than a code defect: a host that adds the three names to
     `notify.events` would receive them, and I did not test that. What is *not* inferred: the gate
     is by event name, the names are absent from the list, and the events are in the session store
     and absent from the delivered stream — all three re-measured 2026-09-18.]
3. **A stuck termination's cause reaches the record only because the run *errored*.** Re-measured
   2026-09-18: **4** sessions carry a stuck termination in their own error record
   (`"error_body":"agent loop stuck…`), **all 4 with exit 1**, **all 4 naming `tool=job_output`**,
   all dated 2026-09-02 — i.e. exactly the false-positive class `t-1788309830` reported (the earlier
   pass said 5; the count is 4 in the store as it stands, and the string appears in a further 27
   files only as *discussion* of the mechanism, which is why a substring scan over-reports). So the
   cause is in the record — but accidentally, not by design: the graceful ceiling path (`err == nil`,
   `loop.go:2734`) proves the loop has no structured way to record a stop reason, only an error
   string. Fixing §6.2.1 by reusing the error body would mislabel a policy stop as a failure.
4. **The loop's own residue is unbounded and mostly un-retirable — C4 conflict.** Re-measured
   2026-09-18 by parsing every `*.dump.json` on the host. **Two dump families exist and the earlier
   pass conflated them by counting filenames:**
   - **Session crash dumps** — untyped-by-name, `type:"crash_dump"` **inside** the file: **1,578**
     files (211 in 2026-08, 1,367 in 2026-09), of which **1,522 carry `orphan:true`**. These are the
     module's own residue.
   - **API-error dumps** — `~/.gopi/dumps/`: **2,311** files whose keys are
     `http_status` / `api_error_body` / `original_request`. **They are a different family written by
     another subsystem** (`internal/deepseek`), they are **not** crash dumps, and they are **not**
     produced by the loop. The earlier "813 dump files" figure was a `find` that hit this directory,
     so it measured neither family correctly.

   For the crash dumps that *are* this module's: **378 of the 1,522 orphan dumps (24.8 %) point at a
   session record that no longer exists**, and the retirement path deliberately skips exactly that
   case ("Session unreachable from this scan (deleted log) — leave the dump: it is the only surviving
   evidence. Do not count it.", `crashdump.go:423`–`:425`, the skip at `:424`). So they accumulate by
   construction. Filed as a C4 conflict rather than a defect: the residue *is* evidence, and the
   evidence has no retention bound — and the bound is absent for a second reason now measured: the
   retirable set is enumerated from the session records themselves
   (`listOrphanDumpPaths` → `sessionlayout.Records`, `crashdump.go:451`–`:469`), so a dump whose JSONL
   is gone can never even be *seen* by the sweeper. It is out of every sweep's reach by construction,
   not merely skipped by a guard.

### 6.3 Over-builds

1. **A whole production function is dead, and the sweep gate's comment knows it.**
   `crashdump.go:403` (`retireStaleOrphanDumps`), `:411`–`:435`, has **no production caller** at this
   revision — the only mention outside its own definition is a comment
   (`crashdump_sweep_gate.go:93`, "has no production caller today"), the live path is the sibling
   `reconcileStaleOrphanDumps` (`crashdump.go:316`→`:332`), and no test calls it directly (the
   retirement tests exercise the reconcile path through `BackfillOrphanCrashDumps`). Two functions
   implement "retire a dump whose crash claim is no longer true"; one is live, one is not, and the
   dead one is the one whose name the gate's comment cites.
2. **Two nudges chase one behaviour, and their thresholds were never reconciled.** The completion
   guard (`maxTaskAreaNudges = 3`), the no-list nag (`defaultNudgeRounds = 5` /
   `defaultNudgeToolRounds = 3`, `loop.go:103`–`:104`) and the stale-list nag
   (`staleListBlockRounds = 4`, `:112`) all exist to make the model re-emit or advance the list, at
   three different round counts, with three separate counters. [inferred — weakest link: I read the
   constants and their call sites, but "two mechanisms pursue one behaviour and nobody chose"
   requires intent, which no comment states; it may be deliberate layering by cadence.]
3. **The task-list discipline's adoption INVERTED on 2026-09-10, and the earlier reading ("used by
   roughly one session in twenty-three") described a fleet that no longer exists.** This is the
   verification pass's most consequential correction, and it is a *reversal*, not a refinement.
   Measured 2026-09-18 over all **3,148** session files (`~/.gopi/sessions/**/*.jsonl`), split at
   2026-09-10:

   | Era | Session files | used the `task` tool | declared a free-text list |
   |---|---|---|---|
   | before 2026-09-10 | 1,254 | **0** (0.0 %) | 996 (79.4 %) |
   | from 2026-09-10 | 1,894 | **848** (44.8 %) | 914 (48.3 %) |

   Over the whole store the totals are `task`-using files **848** with **12,146** calls
   (`commit` 5,522, `view` 3,675, `review` 1,641, `reframe` 1,244) against **1,910** files carrying
   `[TASK_LIST_BEGIN]`. The inversion is not subtle: the machine-checked mechanism went from
   *structurally impossible* (0 files — the consolidated `task` tool postdates that era; the retired
   per-verb names `todo`/`write_task_area` were what existed then, and their last emission is
   2026-09-10) to the mechanism of **44.8 % of sessions**, while the prose mechanism fell by roughly
   a third. Two mechanisms, and the newer one is now *the younger half of the fleet's default*, with
   the two running near parity. **The honest residual is that the two also CO-EXIST**: the eras
   overlap, so 48.3 % of post-cut sessions still declare a free-text list *and* many of those also
   commit through `task`. §6.4.1's question ("is the machine-checked list the right implementation?")
   is therefore still open — but the earlier framing of it as "a mechanism nobody took up" was an
   artifact of measuring a heterogeneous store as one population, and is withdrawn.
4. **7 of those 1,126 calls carry a malformed action.** Re-measured 2026-09-18: of **12,152** recorded
   `task` calls, **26 carry no `action` at all** and a further **27 carry an `action` that is not one
   of the four verbs** — its value is a raw tool-call parameter blob running from 59 to 4,310
   characters (a `<｜｜DSML｜｜ parameter name="conclusion" …` string) rather than a verb. **The
   malformed shape is being produced right now, not historically**: at least one of them is in this
   session's own record (measured while writing this sentence). The run did not fail — the input was
   accepted and the conclusion landed — which means the tool's input validation admits a shape no
   model was ever asked to emit, and the harness cannot tell "the model sent garbage" from "the
   transport mangled a good call".
   [inferred — weakest link: I observe the stored input, not the model's intent; a transport
   encoding bug and a model error look identical in the record. What is *not* inferred: the counts,
   the value shapes and the fact that a malformed call still commits successfully.]

### 6.4 Never decided

1. **Is the task-list discipline a *user* requirement or a harness-internal mechanism?** This is the
   brief's own honest unknown, and the evidence does not settle it. The operator designed the
   contract's strongest form (W4: "框架有最终话语权"), yet the same operator's dispatch also
   prescribed that the free-text list remains legal for *planning*. The adoption numbers (§6.3.3,
   re-measured 2026-09-18) make the question sharp in a way the first pass got backwards: at the
   fleet level the machine-checked mechanism is now used by **44.8 %** of post-2026-09-10 sessions
   while the prose mechanism covers **48.3 %** of them — near parity, not a 4 % curiosity. So the
   *practice* is genuinely undecided, and it is undecided in an expensive way: two mechanisms are
   carried, and the harness pays for both while the record cannot say which one it relied on. My
   reading, offered as such: the *need* ("I can see the shape of the work", §1.1) is the operator's;
   the machine-checked list is one implementation of it, and the fact that half the fleet still
   reaches for the other one is the strongest available evidence that the machine-checked list has
   not yet displaced it. [inferred — weakest link: adoption counts measure use, not sufficiency; a
   session may be perfectly legible with either mechanism, and the 44.8 % / 48.3 % pair also counts
   sessions that use **both**, which no measurement so far separates. Separating them is the single
   cheapest decisive experiment this module could run: partition post-cut sessions into
   {task-only, list-only, both, neither} and see which partition correlates with a clean close-out.]
2. **The probe's own blindness was never turned into a decision.** The sweep's change probe cannot
   see writes inside an existing session directory — stated, measured, and left to the bounded
   cadence floor for cover (`crashdump_sweep_gate.go:51`–`:76`). The floor is a *mitigation*, not a
   chosen bound: nobody decided how stale a detection may be.
3. **How stale may a *liveness* signal be?** The heartbeat's two thresholds
   (`defaultHeartbeatInterval = 30 s`, `defaultStaleRounds = 3`, `heartbeat.go:11`–`:12`) and the
   orphan classification's quiet window (`orphanLiveWindow = 15 min`, `session.go:526`) are three
   different answers to "how long must silence last before I believe it" — none of them derived from
   the other, and none justified against the operator's tolerance. He asked for the *display* to
   distinguish running from dead (W6); he never said at what silence the machine may conclude it.
4. **What the operator may do with a finished-but-unrecorded run.** The honesty brief lists the
   remedy ("可操作：补关账") and the loop now emits the heartbeat it needs, but no requirement exists
   for *who* closes such a row. §6.2.1 shows the ambiguity has already produced one 8-hour run whose
   row says `failed` and two whose rows say `completed`, with no record distinguishing them.
5. **Interrupted is not a first-class outcome.** The interruption contract unwinds a run gracefully
   (`loop.go:2464`), yet no exit reason exists for "the operator stopped it" — it will read as
   either a completion or a failure. [inferred — weakest link: I did not find an interrupt-path
   session in the store to confirm which of the two it lands as.]

### 6.5 Quantitative claims, re-measured live

All figures from `~/.gopi/**` and `~/.gopi/memory.db`. Every line was re-measured in the
**verification pass on 2026-09-18** at master `812f9a60`; the "analysis pass" column carries what the
first measurement said where it differs, so a reader can see what changed rather than only what is.
Method: `grep -F` over the enumerated file list for exact strings, and a single streaming pass over
the JSONL for the structured counts (`tool_use` names and their `input.action`).

| Claim | Re-measured 2026-09-18 | Analysis pass (2026-09-16/17) | How |
|---|---|---|---|
| Session records | **3,148** | 1,597 | `.jsonl` under the data root (`find`) |
| Sessions with a terminal event | **1,811** (lines; **1,143** distinct files in the snapshot) | 1,184 | parse `session_end` |
| Runs terminated by a stuck detector | **4** — all exit 1, all 2026-09-02, **4 × `tool=job_output`** | 5 | `"error_body":"agent loop stuck` |
| Sessions ending within 100 s of the 8 h cap | **3** (28,801.1 / 28,801.4 / 28,897.1 s) | 4 | `elapsed_sec` ∈ [28,790, 28,940] |
| Longest session in the store | **83,632.9 s (23.2 h)** | 23.2 h | max `elapsed_sec` |
| Sessions carrying the wall-clock event string | **43 files** | 23 | `grep -F session-wallclock-exceeded` |
| Guard nudges delivered (user messages) | **471 runs; max 3 per run** | 89 runs; max 3 | count per file, capped by `maxTaskAreaNudges` |
| Sessions declaring a free-text task list | **1,910 files** | 1,238 | `[TASK_LIST_BEGIN]` present |
| Sessions using the `task` tool | **848 files (44.8 % of post-2026-09-10 sessions); 12,146 calls** | 69 files (4.3 %); 1,126 calls | JSONL `tool_use` scan |
| … task tool by action | `commit` **5,522** / `view` **3,675** / `review` **1,641** / `reframe` **1,244** | 503/361/167/88 | `input.action` tally |
| Retired tool names still in the store | **1,916 calls** — `todo` 1,492, `write_task_area` 410, `task_evaluate` 13, `tasklist` 1; **last emission 2026-09-10** | (not measured) | `"name":"<tool>"` per file |
| Session crash dumps (`type:"crash_dump"`) | **1,578** (211 in 2026-08, 1,367 in 2026-09); **1,522** carry `orphan:true` | 813 (mis-counted) | JSON parse of every `*.dump.json` |
| … orphan crash dumps whose session record is **gone** | **378 (24.8 %)** — invisible to `listOrphanDumpPaths` | 686 of 784 | sibling `.jsonl` existence check |
| API-error dumps (`~/.gopi/dumps/`, keys `http_status`/`api_error_body`) | **2,311** — a **different family**, written by `internal/deepseek`, not this module | (conflated with the above) | JSON parse |
| Notification events landing on the operator's channel | **95,283 delivered** — but `loop-stuck` **0** and `session-wallclock-exceeded` **0** | 0 (target dir absent) | count the configured target file + event names |
| Advertised tools that the dispatcher cannot serve | **1 of 17 — `screen_capture`** (§6.6) | (not measured) | dispatch switch vs `claudeToolDefs` |

**Honest reading of three lines.** (a) The stuck terminations are **4, all on 2026-09-02 and all
`job_output`** — i.e. precisely the false-positive class `t-1788309830` reported, and none since the
polling exemption landed: the detector's entire measured production effect has so far been on honest
waits. A substring scan finds **31** files containing "agent loop stuck" and would call them all
terminations; 27 of those are sessions *discussing* the mechanism, which is why the number is stated
from the structured error record and not from a text search. (b) The cap band is **3 runs**, not a
cluster: the ceiling's observable effect is "runs are no longer longer than 8 h", which the one
83,632 s (23.2 h) pre-cap session demonstrates and nothing contradicts. (c) The adoption inversion
in §6.3.3 is the correction that matters most: the store is a **heterogeneous population** —
mechanisms changed on 2026-09-10 — and any single blended adoption figure for it is meaningless,
which is what the earlier pass's 4.3 % was.

---

### 6.6 NEW — found by this pass: the closed tool surface has a phantom, and the gate that pins the surface cannot see it

**This is the finding the operator did not have.** Rule 3's requirement (W9) is that the run's
abilities are a closed, knowable set. Measured 2026-09-18 at `812f9a60`, the set is knowable but not
**true**: **one of the 17 advertised tools has no dispatch case, so every call to it fails, and no
test in the repository can detect that.**

The evidence is a three-way comparison, each leg re-run in this session:

| Leg | Measured | Command / site |
|---|---|---|
| What the product **documents** | 17 tools | `GOPI.md:87` — "GoPi has 17 tools: read, write, edit, bash, … vision_analyze, text2img, **screen_capture**" |
| What the harness **advertises to the model** | 17 `claudeToolDefs` entries, incl. `ToolScreenCapture` | `claudeToolDefs` at `internal/agent/loop.go:849`; its entry at `:1374` (`Name: toolnames.ToolScreenCapture`) |
| What the harness can actually **dispatch** | **16** of those 17 — `ToolScreenCapture` has no `case` | the per-tool dispatch switch `switch tc.Name` opens at `loop.go:4625` and its `default:` at `:5071` returns `"Unknown tool: %s with args %s"` (`:5073`). A `grep` for `screen`/`ScreenCapture` across the whole switch (`:4600`–`:5080`) returns **nothing** |

`ToolJob` and `ToolMemory` and `ToolTask` are dispatched below their merged `case`s, and the
`ToolJobAction*` / `ToolMemoryAction*` / `ToolTask*` constants in the switch are those tools'
**action verbs**, not additional tools — so the surface really is 17, and exactly one of them,
`screen_capture`, is unreachable.

**The consequence is measured, not hypothesised.** The model *does* call it — `screen_capture`
appears in **11** recorded `tool_use` blocks — and every time it receives
`"Unknown tool: screen_capture with args {…}"`. The error is on the wire: **30** occurrences of that
string in the session store, across **9** session files, in three clusters (**2026-09-11** ×5,
**2026-09-12** ×1, **2026-09-18** ×3). The 2026-09-18 cluster is **today** — i.e. this is live at the
revision being analysed, not a historical wart. Of the runs whose outcome is recorded, the failure
is absorbed (the model retries another way; **5 of the 9 files carry `exit_code:0`**), which is
exactly why it has gone unnoticed: the phantom costs a round-trip and a confused model, never a
failed run.

**Why the Rule 3 gate does not catch it — and why that is a defect in the gate, not only in the
def.** `TestGOPIRule3_MatchesClaudeToolDefs` (`internal/agent/gopidoc_sync_test.go:54`) asserts
`GOPI.md` ↔ `claudeToolDefs`. Both agree on 17. The third leg — `claudeToolDefs` ↔ the dispatch
switch — **has no test at all**, so a def whose wire name no `case` handles is invisible to every
gate. The failure it fails to catch is precisely Rule 3's own incident shape, one layer deeper:
there, a *stale document* claimed a tool the agent had; here, a *stale advertisement* claims a tool
the dispatcher does not implement. Both are "a list that disagrees with the code, read as
authority" — and the second is worse, because the false claim is made directly to the model, which
then spends a round acting on it, while the document is at least only read by a human.

**Bearing on the module's requirements.** §1.5 and §5.9 assert the ability set is closed and
knowable, and that this is what makes "what can the machine do" answerable without reading the
source. This finding says the answer is currently **wrong by one**, in the direction that matters
least visibly (the operator believes screen capture works; the model discovers it does not, every
time). The requirement is not weakened — it is *shown to be half-implemented*: closure is enforced
top-down (defs are fixed and pinned to the doc) and unenforced bottom-up (defs are not pinned to the
dispatcher).

**What is verified and what is inferred.** Verified by direct measurement: the 17/17/16 counts, the
absence of any `screen_capture` case in the dispatch switch, the 11 calls and 30 failure strings in
the store, the three date clusters including today, and that the pinning test passes (so the gate
cannot see this). [inferred — weakest link: I read the dispatch switch and the store, but I did not
execute an agent turn to watch the phantom in flight; and I cannot exclude that a *test hook*
substitutes a handler (`a.executeToolFn`, `loop.go:4604`–`:4609`, is a seam used by tests) —
production runs leave it nil, which is what makes the `default:` branch the live path, but I am
recording that as read, not observed.]

## 7. Implications for tests (naming existing tests; writing none)

This module is documents-only. What the analysis implies for tests that already exist — as a
judgement, delivered as prose:

**The liveness half is guarded in both directions, and the guards are the requirement's shape**
(file of each test re-verified 2026-09-18 — the first pass listed the last two under `loop_test.go`;
they live in `nudge_test.go`):

- `internal/agent/loop_stuck_test.go` — `TestAgent_StuckLoop_TerminatesAndNotifies`,
  `TestAgent_StuckLoop_ToolUseResets`, `TestAgent_StuckLoop_DivergentTextResets`,
  `TestAgent_StuckToolfulLoop_TerminatesAndNotifies`, `TestAgent_StuckToolfulLoop_ParamChangeNoTrip`,
  `TestAgent_StuckToolfulLoop_Disabled`, `TestAgent_PollingJobOutput_RunningJobNotKilled`,
  `TestAgent_PollingJobOutput_FrozenJobKilled`, `TestAgent_WallclockCap_GracefulFinish`,
  `TestAgent_WallclockCap_ZeroUnlimited`, `TestSessionWallclockCap`.
- `internal/agent/loop_test.go` — `TestAgent_Run_ExitsOnFirstCompletedEndTurn`,
  `TestAgent_Run_KeepsConfirmationForProgressingList`.
- `internal/agent/nudge_test.go` — `TestAgent_Run_NudgesStaleTaskList`,
  `TestAgent_Run_NoStaleNagWhenListKeptCurrent`.
- `internal/agent/crashdump_sweep_gate_test.go` —
  `TestOrphanSweepDue_VerdictTableFailsOpenOnEveryUncertainty` (cited in §6.1; the first pass
  omitted it from this list).
- `internal/agent/gopidoc_sync_test.go` — `TestGOPIRule3_MatchesClaudeToolDefs` (W9's enforcement;
  re-run 2026-09-18 → `ok`, and §6.6 shows why passing is not sufficient).

**Direction the gaps imply** — each a candidate, not a request made by this document:

1. §6.2.1 — the requirement changes the *verdict* of the wall-clock tests' subject matter: what is
   tested today is that the cap stops a run gracefully, and what the gap shows is that nothing
   asserts the **stop reason is durably recorded**. A test asserting that a cap-stopped session's
   record lets a reader distinguish it from a declared completion is the missing guard — and it must
   not be satisfied by hijacking the error body, which would mislabel a policy stop as a failure
   (§6.2.3).
2. §6.2.3 — a companion direction: the exit vocabulary should be an enumeration that the record
   carries, and a test should pin that a graceful cap-stop keeps exit 0/terminal semantics.
3. §6.2.2 — a delivery-failure test at the *operator's* end: the configured target failing to open
   is currently only a stderr line; the requirement ("I find out") implies an observable
   degradation.
4. §6.3.1 — either a test that calls `retireStaleOrphanDumps` (making it live) or its removal; a
   function with no caller and no test is what the sweep gate's own comment flags as unowned.
5. §6.3.4 — an input-shape test for the task tool's `action`, since a non-verb action reached
   persistence and the run proceeded — and re-measured 2026-09-18 the shape is **live** (53 of
   12,152 calls: 26 with no action, 27 with a parameter blob as the action, one of them in this
   session's own record).
6. §6.4.1 — the honest one: no test can decide whether the machine-checked list is the right
   implementation of §1.1. The measurement is adoption (§6.5), and he does not run it by reading
   tests. The decisive *experiment* is named there and is not a test: partition post-2026-09-10
   sessions into {task-only, list-only, both, neither}.
7. **§6.6 — the direction the requirement most clearly implies, and the one with a defect behind it:
   pin `claudeToolDefs` to the dispatcher.** `TestGOPIRule3_MatchesClaudeToolDefs` currently closes
   `GOPI.md` ↔ `claudeToolDefs`; nothing closes `claudeToolDefs` ↔ the `switch tc.Name` at
   `loop.go:4625`, and `screen_capture` is the proof that the third edge is unguarded. A test that
   asserts every `claudeToolDefs[i].Name` has a dispatch case (and, symmetrically, that the switch
   handles no name absent from the defs) is the missing guard — and it is a **test of an existing
   mechanism**, so Rule 3 forbids nothing here: no tool is added, and the surface is not changed,
   only *verified*. Rendered as the requirement it serves: §5.9's "the ability set is closed and
   countable" is not met while the count is right and one member is a phantom.

**Rule 27 note:** none of the directions above may be satisfied by generating load. Every quantity
involved is a count or a duration read from the store, never a throughput measurement.

---

## 8. Confidence and unknowns

| Claim | Confidence | Weakest link |
|---|---|---|
| "A non-progressing run must stop itself" is a **requirement**, evidenced by three operator-reported incidents and two design rounds. | high | none material — the rows are verbatim and the code matches their prescribed shape. |
| Honest waiting must not be killed — the requirement has a **bound**, not just a direction. | high | [inferred] the polling exemption is name-level (`loop.go:367`), so the bound is "tools the table lists", not "all legitimate waits"; a wait implemented through `bash` is still exposed, deliberately (`:365`). |
| The wall-clock ceiling is a **met requirement** with an empty cap band in production. | high (counts) / medium (interpretation) | [inferred] 3 runs in the band over 1,811 terminal records and a longer window; a longer window could show the cap firing nightly or never again. |
| A ceiling-stopped run is **not distinguishable in the durable record**. | high | [inferred] §6.2.1's one `failed` row among the cap-band sessions shows a second, unexplained path; I verified the absence of a reason field, not the cause of that row's failure. |
| The stuck detector's measured production effect has been **on honest waits only**. | medium-high | [inferred] 4 terminations is a small sample; the exemption postdates all of them (all 2026-09-02), and a session that loops *without* a tool fingerprint still relies on the 5-round no-tool counter, which has no production termination at all. |
| Run residue grows without a retention bound (C4 conflict), and the largest part of it is **invisible to the sweeper**. | high (counts) | [inferred] the design intent to keep evidence is read from the comment, not from a written retention decision (§6.4.2); and "invisible" follows from reading `listOrphanDumpPaths`, not from observing a sweep decline to remove one. |
| The two run-level alarms are **filtered out by the configured event allowlist**, not by delivery failure. | high | [inferred] the subscription list is per-host configuration, so a host that subscribes the three names would receive them; I verified the gate, the list, the session-record presence and the target-file absence, but did not test the positive case. |
| The ability set is **closed and countable** (§5.9). | high for closure, **contradicted for truth** | §6.6: closure is real (an unknown name hits `default:` at `loop.go:5071`) and the count is right (17/17), but **one member is a phantom** — `screen_capture` is advertised and undispatched, measured live. Closure holds; the *set* is wrong by one. |
| The machine-checked list is a **marginal** mechanism in practice (used by 4.3 % of sessions). | **WITHDRAWN** — re-measured as 44.8 % of post-2026-09-10 sessions | The first figure blended a heterogeneous store across a mechanism change; see §6.3.3. The residual unknown is now the {task-only, list-only, both, neither} partition, not the raw rate. |

**What is not inferred at all** (verified by reading the cited line, not by reasoning): every
`file:line` in §3–§7 was read back at `17cec86b` before this document was committed, and the
verification pass re-resolved all **51** distinct `file:line` citations and all **26** range tail
lines against master `812f9a60` (result: 50/51 byte-identical, 26/26 range tails identical, 1 line
shift re-anchored — see `## Verification`); the ledger quotations in §4 are verbatim payload text;
all counts in §6.5 were produced by commands in this session; the `screen_capture` finding in §6.6
is a live measurement, not a reading.

### What I could not determine

*(This list is the verification pass's, and it replaces the analysis pass's where they overlap; two
of its entries are resolved and said so, rather than quietly dropped.)*

1. **Whether the task-list discipline is a user requirement or harness bookkeeping — and the earlier
   framing of this as "nobody uses it" is withdrawn.** §6.4.1 states both readings; the numbers now
   put the two mechanisms at near parity (44.8 % vs 48.3 % of post-cut sessions). **What is genuinely
   a requirement, and what is not — stated as the distinction the epic asked for:** the *need* is the
   operator's ("I can see the shape of the work", §1.1, evidenced by W6's verbatim "are these dead?").
   The **in-session working area** (committed conclusions, the pending-set guard, carry-forward across
   resume) is **harness bookkeeping**: its user is the *next* round and the *next* session, not the
   operator — he never reads a committed conclusion as a product feature; he reads the report the run
   writes at the end. The **task list's user-visible half** is narrower than either: it is only the
   legibility the display needs to distinguish running / finished-unrecorded / died (W6, and B8 owns
   the display). Everything else — the item states, the nudge caps, the conclusion store — is
   machinery whose *justification* is the legibility requirement and whose *audience* is the harness.
   I could not determine which of the two implementations the operator would keep if forced to one.
2. **Why one cap-band session's row reads `failed` while its session record says exit 0.** Verified
   both facts (3 cap-band records in the current store), could not connect them: the close-out path
   is B1's surface and I did not trace the row's write history.
3. **Whether an operator-interrupted run is durably distinguishable** from a completion or a
   failure. No interrupt-path session was identifiable in the store, so the gap is recorded without a
   measurement.
4. **The intended cadence relationship between the three list-nudges** (`3` / `4` / `5` rounds —
   `maxTaskAreaNudges` `loop.go:332`, `staleListBlockRounds` `:112`, `defaultNudgeRounds` `:103`).
   Three constants, three counters, no comment that derives one from another; I could not tell
   layering from accumulation.
5. **Which of the two dump families' growth is a defect.** Measured: **1,578** session crash dumps
   (1,522 orphan, **378** structurally invisible to the sweeper) *and* **2,311** API-error dumps in
   `~/.gopi/dumps/` written by a different subsystem. The analysis pass conflated them; separating
   them raises a new question it did not ask — whether "the loop's residue" is even the right owner
   for the second family, which this module does not write.
6. **The real cost of the caps in tokens.** The store has no per-run cap counter and I did not
   aggregate request sizes; a quantitative claim here would be a fabrication, so it is absent rather
   than estimated.
7. **[RESOLVED, recorded because it was on this list]** The notify finding's root cause is now known
   and is not "delivery failed": the configured event allowlist omits the three run-level event names
   (§6.2.2). The delivery channel itself works — 95,283 events on disk.
8. **[RESOLVED, recorded because it was on this list]** The dump-population growth is now attributed
   to two families rather than one, with the crash-dump family's 378 invisible members counted
   separately (§6.2.4).

## C1–C6 check

| | Constraint | Verdict | Evidence |
|---|---|---|---|
| **C1** | Static binary, zero deps, no plugins | **PASS** | The whole loop, both detectors, the ceiling, the dumps and the sweep are stdlib + the one pure-Go SQLite driver; no subprocess, no plugin seam, no new dependency (**re-measured 2026-09-18**: `go.mod` has exactly one direct require, `modernc.org/sqlite v1.44.0`, all others `// indirect`). The only process spawns are the notify target's configured command (`internal/tools/tasknotify.go`) and the agent's own tools (B7). |
| **C2** | The operator observes and can reverse everything | **CONFLICT — filed in §6.2.2** (was "PARTIAL") | Observe *at the mechanism*: both caps are env-overridable with a documented unlimited value, each guard has a switch, the sweep has a three-valued knob, and the loop's rules are visible in the operator's own GOPI.md. But the two run-level alarms he is told will reach him are **dropped by his own event allowlist** — `loop-stuck` and `session-wallclock-exceeded` are not subscribed, so 95,283 delivered events do not include one of them (§6.2.2). "Can reverse everything" also fails one layer down: the run's advertised surface contains a tool that cannot execute (§6.6), which is not reversible by him — he would have to read the code to discover it. |
| **C3** | Daemon simplicity — every branch multiplies failure surface | **PASS, with one watched branch** | Nothing here is resident; the guards are per-run and stateless across runs, the sweep is start-triggered and gated rather than periodic. The watched branch is unchanged and now sharper: three liveness signals + the completion guard + three nudges + a multi-action `task` tool is a lot of behaviour on the run's critical path, and C3's discipline ("do less") applies to a run loop as much as to a daemon. §6.6 is the first measured cost of that complexity: a tool surface nobody diffs against its dispatcher. |
| **C4** | Cleanup with a named bound and a refusal behaviour | **CONFLICT — filed in §6.2.4, now with the count corrected** | The residue has a deletion path and an explicit refusal (`crashdump.go:423`–`:425`), but no bound: **1,578** session crash dumps, **1,522** of them orphaned, **378** structurally unreachable by the sweeper (its candidate set comes from the session records themselves, `listOrphanDumpPaths` `crashdump.go:451`–`:469`). The earlier "813" was a `find` that counted a *different* family (2,311 API-error dumps) and is withdrawn. |
| **C5** | One-person solutions; cost and cognitive budget are requirements | **PASS, with a measured tension** | The density gate is the mechanical half (system prompt < 4,500 chars, `prompt_test.go:70`) and the loop respects it by putting its instructions in user turns. The tension is cognitive, not token: four ways to stop a run, three nudges to advance a list, and a tool surface whose advertised count (17) exceeds what executes (16) — a reader must hold more distinctions than "what does this run do" needs (§6.4.1, §6.6). |
| **C6** | `why` over `what` | **PASS for this document's subject, with one binding consequence** | The loop is unusually well served here: nearly every cap constant carries its incident in its comment (`loop.go:254`, `:294`, `:313`, `:332`), which is why §4 could cite *why* each guard exists rather than infer it. The consequence is Rule 3's (W9): a requirement that could only be met by adding a tool is not a gopi requirement — and §6.6's repair is deliberately framed as *verifying an existing mechanism*, never as extending the surface. |

## Verification

**This section reports the verification pass of 2026-09-18** (branch
`docs/req-06-agent-loop-verify`, worktree `~/workspace/gopi-req-06-verify`, cut from master
`812f9a60`). The analysis pass's own verification notes are kept below it, because they explain two
corrections that are still visible in the text.

### Citation re-resolution (current master)

- **Base of record for this pass: `812f9a60`** (`git rev-parse HEAD` in the worktree). The analysis
  pass's base `17cec86b` is an ancestor of it (`git merge-base --is-ancestor` → yes), so every
  original citation can be checked against the revision it was written at *and* against current
  master.
- **All 51 distinct `file:line` citations were re-resolved**, each printed from both revisions side
  by side (`git show 17cec86b:<file>` vs the working tree). **Result: 50 of 51 byte-identical.**
- **The one that moved:** `cmd/agent/main.go:974` (`agent.BackfillOrphanCrashDumps`, def `internal/agent/crashdump.go:288`; was `:741` at `17cec86b`) — the `agent.BackfillOrphanCrashDumps(...)` call is
  now at **`cmd/agent/main.go:974`**; the old line number now holds an unrelated comment about
  SIGPIPE. This is a **line shift, not a wrong claim**: re-reading the call at `17cec86b` shows the
  same statement, so the citation was correct when written. It has been **re-anchored by symbol**
  (`BackfillOrphanCrashDumps`, definition `internal/agent/crashdump.go:288`) so the next shift cannot
  silently break it — which is the failure mode this repo has already measured once (a sibling
  requirements document lost 38 of 74 citations to line drift).
- **All 26 range tail lines were checked too.** The document cites ranges in the shorthand form
  `` `loop.go:80`–`:342` `` (the second half is a bare `:N`); a first-half-only check would have
  silently skipped 26 line numbers. Every tail line was compared between `17cec86b` and `812f9a60`:
  **26 of 26 identical**.
- **Load-bearing citations were read for content, not just position**: `loop.go:254/294/305/313/324/332/357/367/645/647/2464/2572/2734/3383/4412`,
  `tasklist.go:16`, `heartbeat.go:11`, `session.go:370/526`, `crashdump.go:421` — each printed and
  matched against the sentence the document attributes to it. No mismatch.

### Re-run

- **Rule 3's enforcement test re-run at current master:**
  `CGO_ENABLED=0 go test ./internal/agent/ -run TestGOPIRule3_MatchesClaudeToolDefs -count=1` →
  `ok  github.com/nick/gopi/internal/agent  3.211s`. Still pass-relevant: the test is
  `internal/agent/gopidoc_sync_test.go:54`, `GOPI.md`'s inventory line is `:87` under the
  `## Rule 3` heading at `:85`, and `claudeToolDefs` (`loop.go:849`) holds **17** entries. §6.6 is
  what it still does not catch.
- **Every named test in §7 was re-checked for existence at current master** — all 16 resolve to a
  file, and **two were corrected** (see §7).
- **Quantitative claims:** §6.5's table was re-produced end-to-end by commands run in this session
  (exact-string `grep -F` over an enumerated file list; one streaming JSONL pass for the structured
  counts). Every row now carries both the re-measured value and the analysis-pass value where they
  differ, so the corrections are visible rather than silent.
- **Rule 27 (never generate load): observed.** Every measurement in this pass is a read of files
  that already exist — counts, string matches, JSON parses, one `go test -run` of a single unit
  test. No device write, no concurrent writer, no synthetic pressure, no benchmark.

### What this pass changed (the repairs, and why)

1. **§6.6 added — a NEW, verified finding** (`screen_capture` is advertised to the model but has no
   dispatch case; 11 live calls, 30 failures in the store, including today). This is the epic's
   ">=1 finding new to the operator".
2. **§6.2.2 repaired** — the notify conclusion was inverted: the channel works (95,283 events) and
   the two run-level alarms are dropped by the configured event allowlist, not by delivery failure.
   Root cause found, previously unknown.
3. **§6.3.3 repaired and its conclusion REVERSED** — adoption is 44.8 % of post-2026-09-10 sessions,
   not 4.3 % of the whole store; the store is heterogeneous across a mechanism change, which no
   blended rate can express. §6.4.1 and §8 were updated to match.
4. **§6.2.4 repaired** — two dump families separated (`crash_dump` 1,578 with 378 unreachable vs
   API-error 2,311); the earlier 813 was a `find` that conflated them. `/tmp`-style scratch was
   avoided: every intermediate file of this pass lives under the session dir or `~/` (Rule 22/30).
5. **§1.5/W9/§5.9/§7 added** — Rule 3 stated as a *requirement* (the closed ability set), traced to
   `GOPI.md` Rule 3 **and** its enforcement test; `task` verb consolidation measured; the missing
   `claudeToolDefs ↔ dispatcher` edge named as the implication.
6. **§6.3.4 sharpened** — the malformed-action count is 53 of 12,152 calls, it is live, and one
   instance is in this session's own record.
7. **C2 upgraded PARTIAL → CONFLICT, C6 added to the table** — because the operator-vision check is
   only complete against all six constraints, and §6.6 gives C2 a second, unreversible observation.

### Analysis-pass verification (2026-09-16/17, kept)

- **Deliverable (analysis pass):** `devdoc/requirements/06-agent-loop.md` on branch
  `feat/req-b6-agent-loop`, worktree `~/workspace/gopi-req-b6` (created from master per Rule 6a;
  never developed in the main repo). That branch has since been merged to master by the sibling task;
  this pass verifies and repairs the merged document rather than committing on the prior branch
  (Rule 23).
- **Baseline:** the analysis pass's citations pin `17cec86b`; **every range was read back line by line
  and corrected before its final commit** — its first pass had six ranges spanning a line or two past
  their subject (the sweep gate's design block, the task-list status constants, the raise/panic pair,
  the cap-comment block, `retireStaleOrphanDumps`, the plan-parsing block), each replaced with the
  exact definition/documentation lines. Those corrections are still byte-identical today (checked in
  the 26/26 range pass above).
- **Its one self-corrected measurement:** counting guard nudges per *file* (up to 20) counted nudges
  echoed back inside tool results; the corrected count counts only those delivered as user messages.
  Re-measured this pass: **471 runs, max 3 each** — the shape of that correction was right, the
  magnitude moved with the store.

### Gates for THIS branch

- **Deliverable:** this file, and nothing else. `git diff --name-only 812f9a60..HEAD` →
  `devdoc/requirements/06-agent-loop.md`, exactly one file (Rule 5/7: docs-only; no code, no test, no
  `make fmt`).
- **Worktree clean (Rule 30):** `git status --porcelain` prints nothing. Every scratch and evidence
  file of this pass was written **outside** the worktree (under the session directory and `~/`), so
  the tree holds only the committed deliverable.
- **No test run against the tree for correctness**, deliberately: no code was touched, so no test can
  exercise a change. The one test *executed* (`TestGOPIRule3_MatchesClaudeToolDefs`) was run as
  evidence about the requirement, not as a gate on this diff.
- **Deliverable declaration (Rule 6d):** branch **`docs/req-06-agent-loop-verify`** in the shared repo
  `~/work/GoPi` — the commits are shared through the worktree's refs, so no push or fetch is needed.
  Recovery command:
  `git -C ~/work/GoPi show docs/req-06-agent-loop-verify:devdoc/requirements/06-agent-loop.md`.
  (The analysis-pass branch `feat/req-b6-agent-loop` is already merged into master, so the same file
  is also at `git -C ~/work/GoPi show master:devdoc/requirements/06-agent-loop.md` — without this
  pass's repairs.)
