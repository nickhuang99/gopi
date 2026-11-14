<!-- doc-class: historical -->
# Task Report: session time budget in the system prompt (P1/P2/P3)

Task: `01a0c343-f57e-7bf2-a4da-3384d9c290df` · Branch `feat/session-budget-prompt`
Worktree `/home/user/workspace/gopi-session-budget`

## Completed

- **P1 — `clockRule()` (the tick-clock line).** The session budget rides the
  clock rule, because the tick is what makes the ceiling legible: the model
  already reads `[t+NNNs]` every round, and the rule now tells it what it is
  counting toward. Rendered live from the budget source, never a literal.
- **P2 — task rule 5 in `taskManagementRules`.** The same iron rule as a
  numbered rule in the tasklist protocol: a task that cannot finish inside the
  budget is TOO BIG **by definition**; running out is a **planning failure, not
  a graceful ending**; past the mark, split via
  `task(action=reframe, verdict=TOO_BIG)` + roadmap.
- **P3 — the commit return.** `commitTaskItem` appends the elapsed/budget line
  to **every** `task(action=commit)` return: `session budget: <elapsed> elapsed
  of <budget> (N% used).` Below the mark that is the whole line; at or past it
  the line adds the one concrete move left (split via
  `task(action=reframe, verdict=TOO_BIG)`). Budget `0` (unlimited) and a nil
  clock both return `""` — the commit return is then byte-identical to before.
- **One named source (Rule 1b/2).** `sessionBudgetMinutes = 60`,
  `sessionBudgetWarnPercent = 75` and `sessionBudgetEnv = "GOPI_SESSION_BUDGET"`
  are declared together; **exactly one reader**, `sessionBudget()` (unset →
  default, positive N → N **minutes**, `0` → unlimited, invalid/negative → warn
  + default). Both prompt faces and the commit line render through
  `sessionBudgetMinutesResolved()` / `sessionBudgetRule()` — no inline number
  anywhere.
- **`PromptSection.Render`** — the mechanism that makes the prompts live. A
  package-level `Text` is evaluated once at process init, so a
  `GOPI_SESSION_BUDGET` set after init decorated no prompt (found by the
  override test going red). Sections whose text renders a runtime value now
  carry a `Render func() string`, resolved at **assembly** time; `Text` stays
  the ordinary form and the test-visible `promptSections` var is preserved.
- **Verification of the pre-existing cap fix (Fact 4, not rebuilt).**
  `internal/runend` + the `exitCode != 0 || endReason.Truncated()` status rule +
  the `gopi task show` TRUNCATED marker are already on master; proved by a
  forced cap hit (see below), not re-implemented.

## Not Done (honest)

1. **The first pair of short-budget runs produced a NEGATIVE (kept here as the
   record of the earlier attempt).** Two early runs, budget 1m and 3m, both
   answered `DOABLE` at the reframe beat even after seeing 93% / 111% on the
   commit return: gopi issues its **first** epic-reframe call *before any
   work*, when the budget is still young (28s of 3m, 15%), so a `TOO_BIG`
   verdict there would have been a lie about an uninvestigated epic; the
   *second* beat, where the budget was genuinely blown, arrived after the
   deliverable already existed. **That negative is superseded**: with an epic
   too big to finish (and therefore still uninvestigated when the mark passed),
   the live run DOES release `TOO_BIG` — see Verification (3b) and Evidence.
   The two attempts together are the useful record: the instruction fires when
   the scenario is genuinely too big, and correctly does not fire when the work
   is already done.
2. **`GOPI_SESSION_BUDGET` is not yet in `CONFIG.md`.** The knob is documented
   in its const block and in this report; the repo's config-doc gate
   (`internal/standards/config_doc_gate_test.go`) governs **`settings.json`
   keys**, not env vars, so the gate is green — but a reader looking in
   `CONFIG.md` alone will not find it.
3. **`sessionBudgetWarnPercent`/`sessionBudgetMinutes` are not surfaced in
   `gopi --help`.** Same reasoning: they are prompt-teaching knobs, not CLI
   flags.
4. **My own diff introduced six doc-anchor regressions, and the fix is in this
   branch (a second commit).** Inserting ~160 lines into `internal/agent/loop.go`
   moved every doc anchor below the seam: five landed on BLANK lines, which
   `TestDocAnchorsResolveInNormativeDocs` reports as drift, plus one prose case
   in `cov-agent-cr.md` whose retelling of an old failure had quoted the anchors
   as bare `:NNN` and so became a live anchor itself. Repaired in commit
   `826443de` — 157 anchors across 38 normative docs re-keyed with pure-move
   verification (old line text == new line text), 9 references inside fenced
   code blocks deliberately left alone. This was **not** in the epic's
   deliverables and I did not set out to do it; the gate found it and the gate
   is the reason it is reported. **The remaining `11 new` doc-anchor drifts are
   NOT mine** — they load identically on unmodified master `3710dc8e` with none
   of my files present (same 11 sites, none a `internal/agent/loop.go` anchor) and are outside
   this branch's scope.

## Finding (reported, not acted on — as instructed)

**The 60-minute prompt budget and the 8-hour engine cap are two different
numbers with two different jobs, and the branch keeps them that way.** The 8h
value is unchanged (`defaultSessionWallclock = 8 * time.Hour`); the budget is
its own constant and its own env (`GOPI_SESSION_BUDGET` ≠
`GOPI_SESSION_MAX_WALLCLOCK`). `TestSessionBudgetIsNotTheWallclockCap` pins
both inequalities so a future edit cannot silently fold them. The argument
*for* tying them: the prompt budget is a meant-to-be-true statement about the
run, and a run can legitimately last 8h — a 60m promise is then not a promise.
The argument *against* tying them (the reason I did not): the 8h cap is a
boundary the engine **enforces**, the 60m is a knob the prompt **teaches**, and
8h as a *planning* budget would teach nothing at all — a planning ceiling is
useful only when it is much shorter than the enforcement boundary. The
decision is the operator's.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`
- Tests: `go test -count=1 ./internal/agent/` on the branch tip → the ONLY
        failure is `TestDocAnchorsResolveInNormativeDocs`, whose 11 new entries
        are byte-for-byte the same 11 that fail on unmodified master `3710dc8e`
        (measured in the main repo with none of my files present) — i.e. the
        package is as green as master, and green on everything my diff touches
        except that pre-existing gate. Every other test in the package passes.
        `gofmt -l .` → the same 9 pre-existing master files, none of them mine.
        Density gate: `TestSystemPromptDensity` PASS — assembled prompt **4488**
        chars vs the 4500 gate.
- **Ran: the real short-budget runs (re-measured on the current branch tip).**
  Two launch commands, both with the budget override and no engine cap:

  ```
  cd <evid>/run1 && timeout 900 env GOPI_SESSION_BUDGET=1 GOPI_HOME=<evid>/home1 \
    GOPI_TASK_WRAPPER_ID= GOPI_SESSION_MAX_WALLCLOCK=0 <bin>/gopi \
    "Audit all 17 tools of this repo: for each one find its definition, its call
     sites and its tests, then write a comprehensive devdoc/report.md with a
     per-tool section. Also rewrite each tool description to be 30% shorter while
     keeping every routing cue. Then verify the prompt density gate still passes.
     Do not stop early; report only when the whole audit and the rewrite are
     complete."

  cd <evid>/run2 && timeout 900 env GOPI_SESSION_BUDGET=2 GOPI_HOME=<evid>/home2 \
    GOPI_TASK_WRAPPER_ID= GOPI_SESSION_MAX_WALLCLOCK=0 <bin>/gopi \
    "Create the file facts.txt containing exactly three lines, each one fact
     about the Go programming language. Then report that you are done."
  ```

  sessions: run1 `c95dcd02-63b0-4ea6-812e-129e801abd04` (177 JSONL records),
  run2 `5b4c0fe1-d571-46d0-b526-fa505a51ae72`.
- Result:
  - **(a) the commit return carries the elapsed/budget line — POSITIVE, both
    bands.** Verbatim from run2's JSONL (budget 2m, normal band):
    ```
    session budget: 12s elapsed of 2m0s (9% used).
    ```
    Verbatim from run1's JSONL (budget 1m, escalated band — the full sentence
    the model received):
    ```
    session budget: 4m35s elapsed of 1m0s (457% used) — PAST the 75% mark: a task
    that cannot finish inside the budget is TOO BIG by definition, and running out
    is a planning failure, not a graceful ending. Split it NOW:
    task(action=reframe, verdict=TOO_BIG) with a roadmap.
    ```
    (also seen at `4m57s elapsed of 1m0s (495% used)` in the same session.)
  - **(b) the model called `task(action=reframe, verdict=TOO_BIG)` with a
    roadmap — POSITIVE (this is the result the earlier pair failed to produce).**
    Verbatim tool_use input, run1 JSONL record 112 (5 roadmap items; 4 shown):
    ```
    {"acceptance": "Each audit section: all 17 tools covered across the 3 files,
       every file:line re-verifiable, byte counts present, `go build ./...` green. ...",
     "action": "reframe", "constraints": "Bounded deliverable: devdoc/report.md must
       stay <=32KB. ...", "verdict": "TOO_BIG",
     "roadmap": [
       {"goal": "Audit tools 1-5 (read, git_blame, write, edit, bash): for each,
          locate the claudeToolDefs schema entry in internal/agent/loop.go, ..."},
       {"goal": "Audit tools 6-11 (code_search, keyword_search, web_search,
          web_fetch, memory, context_trim): same per-tool dossier, ..."},
       {"goal": "Audit tools 12-17 (task_submit, job, task, vision_analyze,
          text2img, screen_capture): same per-tool dossier, ..."},
       {"goal": "Rewrite all 17 Description literals in claudeToolDefs
          (internal/agent/loop.go) so each is <=70% of its original wire byte
          length, ..."}, ...]}
    ```
    A **second** `TOO_BIG` reframe with a 4-item roadmap follows at JSONL record
    157. The model's own reasoning names the trigger: *"session budget is 1min
    nominal but the epic explicitly demands a full audit — the right move is a
    TOO_BIG reframe with a roadmap."* The harness's user-visible reply to the
    FORCE_ERROR is absent because the call is accepted and the run ends there —
    which is the designed behaviour.
  - **The earlier NEGATIVE, kept for contrast (same branch, an epic that was NOT
    too big):** with budget 1m / 3m and a *small* task, the model answered
    `DOABLE` at both reframe beats (`{"action":"reframe","verdict":"DOABLE", ...}`),
    reasoning *"a reframe would have been a no-op restatement after the fact"*.
    Both outcomes together are the honest picture: the instruction fires when the
    scenario warrants it and does not fire when the work is already done.
- **Ran: the forced cap hit on a scratch row.**
  `timeout 600 env GOPI_SESSION_MAX_WALLCLOCK=20 GOPI_HOME=<evid>/cap/home
  GOPI_TASK_WRAPPER_ID= <bin>/gopi "Count from 1 to 40, one number per line,
  each time using the bash tool to append the next number to numbers.txt. Do
  not stop until you reach 40."` then
  `GOPI_HOME=<evid>/cap/home <bin>/gopi task show 01a0c589-91cc-7664-90d3-d7100003395a`
- Result: the row is **not** an ordinary `completed`:
  ```
  status:    failed
  reason:    TRUNCATED run (wallclock-cap) — the engine stopped the loop, not
             the model; the task was NOT reported finished
  result:    — elapsed=20.0s iter=7 tokens=68903 | TRUNCATED run
             (wallclock-cap) | FAILED — recoverable: `gopi resume
             01a0c589-91cc-7664-90d3-d7100003395a` ...
  truncated: TRUNCATED run (wallclock-cap) — the engine stopped the loop before
             the task was reported finished — this row is NOT a completed result
  ```
  `gopi task list` shows `[failed]` for the same row, and stdout carried
  `[gopi] session wallclock cap reached (20s) — finished gracefully before the
  model produced a final answer`. **Fact 4 verified against a live run — not
  rebuilt.**
- **Ran: the prompt dump resumed on the current branch tip (the `Render` seam
  that makes the budget live).** Re-verified from a fresh clone of the branch
  tip, then the shim was deleted:
  - default: **4488 bytes**, rule 5 reads `BUDGET: session budget 60min — ...`
    and the tick line `Session budget 60min — ...`.
  - **`GOPI_SESSION_BUDGET=15`**: **4488 bytes**, both faces render `15min`, and
    `60min` occurs **0 times** — the number is rendered from the one source, not
    typed in.
  - **`GOPI_SESSION_BUDGET=0`**: **4123 bytes**, `0min` occurs **0 times** and
    `60min` 0 times; rule 5 reads `BUDGET: unlimited — no planning ceiling; ...`.
    **0 = unlimited keeps meaning unlimited, in the prompt and in the commit
    return alike.**
- Commit: branch tip of `feat/session-budget-prompt` —
  `git log -1 --format=%H feat/session-budget-prompt`. (This line does not carry
  a literal hash on purpose: the report is amended into its own commit, so a
  hash written here would be stale the moment it lands.)

## Evidence (self-filed)

- `CGO_ENABLED=0 go build -o ./cmd/agent/` from the branch tip → `BUILD_OK` (exit 0).
- `gofmt -l .` on the branch tip → the same 9 files as unmodified master
  (`cmd/agent/cli_config.go`, `cmd/agent/cli_tasks.go`,
  `cmd/agent/serve_lock_replacement_test.go`, `devdoc/voice-zerodep-poc/*.go`,
  `internal/shot/x11route_fallback_test.go`, `internal/tools/*_test.go`); my
  three files (`internal/agent/loop.go`,
  `internal/agent/sessionbudget_test.go`, `internal/agent/wfcontract_test.go`)
  are NOT among them — `gofmt -l internal/agent/loop.go
  internal/agent/sessionbudget_test.go internal/agent/wfcontract_test.go`
  prints nothing. The 9 are pre-existing on master and untouched by this branch.
- Focused suite from the branch tip:
  `go test -count=1 -run 'SystemPrompt|SessionBudget|Unlimited|ClockRule|CommitTaskItemReturn|EmissionContract|Prompt' ./internal/agent/`
  → `ok github.com/nick/gopi/internal/agent 1.161s`.
- Density: `TestSystemPromptDensity` PASS with the assembled prompt at **4488**
  chars vs the gate's `>= 4500` failure condition (re-measured this session on
  all three budget modes).
- Doc-anchor gate on the branch tip: `2138 anchors verified … 11 new, 4 stale
  permits` — **identical counts to unmodified master** (`3710dc8e` in
  `/home/user/work/GoPi`, none of my files present: `2140 … 11 new, 4 stale
  permits`; the same 11 sites appear in both runs and **not one is a
  `internal/agent/loop.go` anchor**). So the branch introduces zero new drift. Before the
  anchor-remap commit the branch read one MORE new than master; that one delta
  was mine and is fixed by commit `826443de`.
- Rule-31 run 1: process `timeout 900 env GOPI_SESSION_BUDGET=1 ...` exited 0;
  the model's three `task_submit` calls registered ledger rows (seen in run1's
  stderr) and its JSONL contains two accepted `reframe`/`TOO_BIG` calls with
  5- and 4-item roadmaps.
- Rule-31 run 2: the model's deliverable observed afterwards — `facts.txt`
  present at 203 bytes with `wc -l` → `3`, quoted by the model itself.
- Cap run: `[gopi] session wallclock cap reached (20s) — finished gracefully
  before the model produced a final answer` on stdout, `EXIT=0`, and the row
  read back `status: failed` / `reason: TRUNCATED run (wallclock-cap)` /
  `truncated: ... this row is NOT a completed result` from the ledger.
- Unlimited check: `grep -c '0min' prompt/unlimited.txt` → `0`; `60min` → `0`;
  `grep -c '15min' prompt/fifteen.txt` → `2` and `60min` → `0`.
- Main repo (`/home/user/work/GoPi`, master): `git status --porcelain | wc -l`
  → `0` — no run of this session wrote into the main checkout.

## Files

- `internal/agent/loop.go` — budget constants + `sessionBudget()`,
  `sessionBudgetMinutesResolved()`, `sessionBudgetRule()`, `clockRule()` /
  `clockRuleBase` / `clockRuleUnlimited`, `(*Agent).sessionBudgetLine()`,
  `commitTaskItem` return, `PromptSection.Render` + assembly, task rule 5,
  and the prompt-density compressions that pay for the two new sentences.
- `internal/agent/sessionbudget_test.go` — new (7 tests).
- `internal/agent/wfcontract_test.go` — `taskManagementRules` is a function now.

## Appendix — the rendered system prompt (deliverable 5)

`assembleSystemPrompt()` with `GOPI_SESSION_BUDGET` unset, verbatim (4488 chars,
density gate < 4500 — this is the exact text sent to the model every round).
The two budget faces are rule 5 of `[TASK MANAGEMENT RULES]` and the final
tick-clock line.

```
You are GoPi. Use tool_use blocks; each tool's own description states when to use it.


[TOOL ROUTING — right tool, not a shell workaround]
- MEMORY (if-then) — 记忆=跨任务竞争力:
  a. start each task with memory(action=search) FIRST.
  b. at completion call memory(action=store) once.
  c. mid-task notable find/pitfall → memory(action=store) NOW.
  d. vote memory(action=feedback, rating=useful/useless/unused) IMMEDIATELY after each memory(action=search); contradicts → memory(action=feedback, rating=useless)+memory(action=update).
- read: see a file. Use read — not shell cat/head/sed; sed/head/tail = byte-precise extraction INSIDE a compound command.
- edit: replace one unique string (or all) — not write.
- bash: build/test/run/git — not for reading files or searching code.
- code_search: symbol DEFINITIONS — ALWAYS use it. Call code_search FIRST with the bare identifier, not shell grep; returns every definition in ONE structured call. usages → keyword_search.
- context_trim: dead-end/errored/no-value result → trim NOW + leave one-line conclusion; copy header tool_use id verbatim.
- git_blame: git file line-history — "谁写的这行 / 哪次 commit 引入 bug" — git_blame(file[,start,end]), not bash git blame.
- task(action=reframe): judge AFTER investigating — DOABLE: do it now; TOO_BIG: breakdown+roadmap (harness auto-submits), never dodge; IMPOSSIBLE: provably unattainable only, with reason. task_submit: an independent work item.



[EFFICIENCY RULES — minimize round-trips]
1. PARALLEL: all independent tool calls in ONE response — sequence on dependency.
2. ACT WHEN READY: no re-deriving.
3. BE CONCISE: lead with the result.
4. Report outcomes faithfully: no claim without verification output.



[TASK MANAGEMENT RULES]
1. SCOPE: >=3 real steps gets a list; a one-shot lookup skips it. Initial list = PLAN (not receipt): [TASK_LIST_BEGIN] by your second tool round.
2. STATUS: pending -> progressing (exactly ONE) -> completed; a parent waits on its children. task(action=view) reads it; never narrate progress in thinking.
3. Re-emit on a status change; idle no more than every 3 tool rounds.
4. Done item: task(action=commit, status=completed, conclusion) then task(action=review, verdict=A..E).
5. BUDGET: session budget 60min — a task that cannot finish inside it is TOO BIG by definition; running out is a planning failure, not a graceful ending. Commit returns report elapsed/budget; past 75% stop and task(action=reframe, verdict=TOO_BIG)+roadmap.
6. DONE = all ids completed AND verification green AND, with GOPI_TASK_REPORT_PATH set, the report written there (## Completed/## Files/## Verification/## Not Done, <=32KB). Then memory(action=store).



[BASH EFFICIENCY RULES]
1. Chain related steps into ONE command that counts (the project's own build+test gate) — parallel batches are separate and fine.
2. Extract MANY facts at once; cap output.
3. On error: stderr, ADJUST — never re-run.
4. Early: git log --all --oneline -10 / git branch -a / status.
5. Verify cheap deterministic tests.



[JOB RULES — long work runs detached]
1. Outlives one bash call? job(action=start) — NOT bash w/ big timeout.
2. No busy-poll: job(action=output) ONCE when notified.
3. job(action=kill) when done.



[ENGINEERING RULES — habits]
1. 约束反应: blocked? weigh bypass vs accept — CREATE within the constraint FIRST.
2. 渲染真值: render verification = PIXEL-level truth, not OCR.
3. 根因优先: 5-why root cause first; A/B params ONLY after it.
4. 最小样本: verify the SMALLEST sample, never "looks right".
5. 回归测试: every bug fix ships a minimal repro test.
6. 配方入记忆: proven recipes → memory(action=store) (复用不重做).
7. 依赖预算: preflight deps — ZERO-DEP default.



[THINKING RULES — brief]
1. Think briefly: 1-2 short sentences (under ~250 characters).
2. No recaps; lead with the tool call or deliverable.


[WORKING MEMORY LAYOUT]
Read-only anchors: SYSTEM + the goal briefing. Working area: analysis/tool rounds, COMPACT.
Item converges → task(action=commit) its CONCLUSION; never re-derive.

A ticking clock runs from session start; each tool result carries [t+NNNs]. Session budget 60min — a task that cannot finish inside it is TOO BIG by definition, and running out is a planning failure, not a graceful ending; past 75% split it (task(action=reframe, verdict=TOO_BIG)). One call reaching t+300s = wrong tool: job(action=start) or cut scope.
```

## Deliverable location

- Branch **`feat/session-budget-prompt`**, worktree
  `/home/user/workspace/gopi-session-budget`. Commit is in the shared repo's
  refs (no push needed).
- Recovery: `git show feat/session-budget-prompt:devdoc/session-budget-prompt.md`
- No merge to master (as instructed).
