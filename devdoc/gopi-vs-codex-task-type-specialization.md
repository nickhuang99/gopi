<!-- doc-class: historical -->
# gopi vs codex: Task-Type Specialization (代码按任务类型分工的优势)

Date: 2026-09-07. Supersedes/extends the earlier
`devdoc/gopi-vs-codex-competitive-analysis.md` (which covered tools,
multi-agent, memory, prompts, safety — but NOT the task-class angle).

The trigger was `codex_help.txt`: codex "works in different styles for
different types of task". This report (a) proves that observation from
codex-rs source, (b) measures the advantage with a controlled A/B run of
codex vs gopi on identical tasks, (c) recommends what gopi should borrow.

---

## 1. What codex actually does (source-level proof)

Codex is not one generic agent with mood swings. The agent-loop config is
**parameterized by task class**, and the task class is chosen by the
subcommand or the session state machine:

| Task class | Chosen by | Different agent-loop config |
|---|---|---|
| Review | `codex review` | Sub-codex thread with rubric prompt, strict JSON output schema, P0–P3 priorities, "only flag bugs introduced in this commit", **no fix generation**, web_search disabled, approvals never, separate review_model |
| Exec (one-shot) | `codex exec` | `--sandbox read-only\|workspace-write\|danger-full-access`, `--approve-for-me`, `-C <root>` scoping, optional `--output-schema FILE`, no interactive collab tools |
| Regular session | `codex` (interactive) | Collaboration-mode templates: `plan.md` (non-mutating, decision-complete spec) vs `default.md` (execute with assumptions), switched only by explicit mode messages |
| Skills / packaged modes | `codex agents` / skills dir | imagegen, review-agent, skill-creator … = task-specialized instruction packs |

Key sources read in `~/work/codex` (codex-rs):
- `codex-rs/core/src/tasks/`: `SessionTask` trait; `ReviewTask` spawns a
  **sub-codex** via `run_codex_thread_one_shot` with a *different* config
  (base_instructions = `prompts/templates/review/rubric.md`).
- `codex-rs/prompts/templates/review/rubric.md`: full rubric — findings as
  strict JSON `{findings[], overall_correctness}`, priority P0–P3,
  "report, do not fix", "flag only what this commit introduced".
- `codex-rs/codex-rs-exec` (the `exec` subcommand flags above);
  `--output-schema` forces the final turn to conform to a JSON schema.
- `codex-rs/prompts/templates/{plan,default}.md`: collaboration mode =
  developer-instruction contract, **changed by explicit messages, not by
  user tone** — so a "please plan this" casual sentence cannot silently
  flip an executing agent into a planning one (and vice versa).

Design essence: **the agent style IS the task type.** Each task class
carries its own (prompt × tools × output-schema × safety × model)
package. claude code and gopi ship one generic agent; the task
"personality" must be re-derived ad hoc from the user's wording every run.

## 2. Controlled experiment (same task, both agents)

Two synthetic Go fixtures (planted bugs in git history / failing tests).
Identical prompt text given to codex (task-typed subcommand) and to gopi
(one generic mode). Both ran the same model family, logs under
`~/workspace/gopi-codex-taskcode/.exp/`.

### 2a. REVIEW task — find bugs introduced in HEAD vs HEAD~1 (planted: Average div-by-zero panic; MaxScore all-negative → returns 0 sentinel)

| | codex `review` / `exec` (read-only) | gopi (generic prompt) |
|---|---|---|
| Bugs found | both, first pass | both + wrote/runtime-verified a repro test |
| Output shape | rubric-structured: [P1]/[P2], file:line, root cause, doc contradiction, fix | prose + test evidence |
| Repo scoping | `-C <root>`/workdir bound; never confused | took ~10 rounds / 157s disambiguating "this codebase" (memory pointed at `~/work/GoPi`; real target = CWD fixture) |
| Safety | sandbox=read-only, approval=never (by design) | no root flag; agent self-anchored on CWD |

Finding quality was **equal** (both found all planted bugs). The
differences were (1) codex was *deterministically* scoped + shaped —
gopi had to burn rounds negotiating scope and output form; (2) codex's
rubric output was machine-consumable (JSON findings), gopi's was prose.

### 2b. FIX task — Product returns after first element + wrong empty identity; tests fail

| | codex `exec` fix | gopi generic fix |
|---|---|---|
| Result | fixed both defects, ran `go test && go vet` (clean) | fixed both defects, ran `go test` (clean) |
| Time / effort | 24s single pass | ~17s / 6 rounds |
| Diff quality | identical `total:=1; total*=x; return total` | identical |

Capability on a clean, well-scoped bug: **tie**. The divergence shows up
not in raw skill but in *shaping*: scope certainty, output schema,
safety policy, and wasted negotiation rounds.

## 3. Why task-type specialization is an advantage (mechanism)

1. **The prompt is the product's only lever on the LLM.** A rubric +
   output schema + "report, don't fix" converts a review from a
   free-form essay into a bounded search with a checkable shape. gopi's
   generic "find bugs" let the model wander (memory-search detour, repo
   ambiguity, prose output). Codex's review mode made the same model
   deterministic in one pass.
2. **Constraints shrink the search space.** read-only sandbox +
   disabled web_search + "no fixes" removed whole decision branches; the
   model spent its budget on the actual diff. This is the classic
   prompt-engineering result: bounded tools + bounded output ⇒ higher
   precision, fewer wasted turns.
3. **Safety policy rides the task, not the user's mood.** A review that
   can mutate the repo is a footgun; codex ties sandbox to the subcommand
   so the *task type* declares its blast radius. gopi's single mode leaves
   it to the model's judgement each time.
4. **Structured output is a contract, not a preference.** JSON findings
   with severities can be diffed, scored, fed to CI. Prose cannot. The
   review task's value to a team is the structured artifact.
5. **Determinism of mode.** codex switches collaboration mode only on an
   explicit developer instruction — so a plan-mode session cannot drift
   into executing (and an executor can't stall planning). A generic agent
   derives its mode from tone, which is unreliable.

The cost of codex's design is real but contained: a bigger prompt
surface, mode confusion when the user picks the wrong subcommand. The
benefit is that *each task class gets a tuned agent instead of a generic
one that must re-derive its own specialization every run* — and that
re-derivation is exactly the 157s / 10-round overhead gopi paid.

## 4. What gopi should borrow (recommendations)

gopi's gap is not capability — the A/B shows gopi fixes and reviews as
well as codex. The gap is **missing task-type parameterization**:

1. **Task-kind entrypoints** (highest value, lowest cost): a review-style
   invocation should bind (a) a review rubric prompt with severity
   levels, (b) **read-only** tool policy, (c) a **JSON output schema**
   for findings. This alone removes the repo-ambiguity + prose-output
   overhead measured in 2a.
2. **Per-task sandbox policy**: review/plan tasks default to read-only +
   no web; build/fix tasks get workspace-write. Model it as a task-class
   property, not a prompt suggestion.
3. **`--output-schema FILE`** support: the last turn must conform to a
   schema (findings[] for review, patch summary for fix). Turns prose
   into a machine artifact — the same reason the review sub-codex forces
   strict JSON.
4. **Explicit scope flag** (a `-C/-root` binding): gopi's generic mode
   had to negotiate which repo "this codebase" meant. A scoped-root flag
   makes review/exec deterministic and parallel-safe (aligns with gopi's
   worktree isolation rules).
5. **Mode is a contract, not tone**: borrowing codex's plan-vs-execute
   split would need an explicit mode signal — gopi's single generic agent
   should at least expose task-class as a first-class parameter rather
   than inferring everything from the user's sentence.

Minimum viable borrow = #1 + #3 for a review task: a `gopi review`
entrypoint whose prompt is a rubric, whose tools are read-only, and whose
output is schema-bound JSON findings. That directly converts the measured
weakness (scope negotiation + prose) into codex's strength (determinism
+ machine artifact) while leaving gopi's generic mode as the default.

## 5. Verification

- Fixtures: `~/workspace/gopi-codex-taskcode/.exp/{repo,fixrepo}` —
  review fixture = clean baseline `9aef651` → buggy `e8755b8`; fix
  fixture = `dc39e46` with failing tests.
- Logs: `codex-exec1.log`, `codex-review1.json`, `gopi-review1.log`,
  `gopi-fix1.log`, `codex-fix1.log` (same dir).
- codex source: `~/work/codex/codex-rs/{core/src/tasks,prompts/templates/codex-rs-exec}`.
- Experiment deltas measured: review scoping overhead (gopi ~10 rounds
  extra), output structuredness (codex rubric JSON vs gopi prose), fix
  quality (tie).

## 6. Not done / caveats

- Web evidence is thin: no authoritative benchmark comparing task-typed
  vs generic agent entrypoints surfaced in searches; the argument rests
  on source + measured run + established structured-output/constraint
  principles.
- Sample = one model family on two small synthetic fixtures. Realistic
  repo scale and mixed model quality (codex's separate review_model vs
  gopi's single model) were not exercised.
