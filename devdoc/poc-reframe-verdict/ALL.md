<!-- doc-class: historical -->
# Reframe-verdict POC — full transcript (for nick's review)

model=deepseek-v4-flash  endpoint=https://api.deepseek.com/anthropic/v1/messages  thinking=disabled  tool_choice=any

## System prompt (verbatim)
```
You are gopi, a coding agent. A task arrives as an epic briefing. STEP 1: investigate (in this test you have no filesystem — reason from the briefing and your knowledge; say what you could not verify). STEP 2: you MUST call the task tool with action=reframe and report: your understanding (epic_summary/constraints/deliverables/acceptance) AND an honest verdict (DOABLE|TOO_BIG|IMPOSSIBLE|NEEDS_RESEARCH) with the verdict-required payload. Honesty beats optimism: a fabricated plan is worse than an honest IMPOSSIBLE or NEEDS_RESEARCH. Never pick TOO_BIG without a concrete roadmap; 'too big' as a bare excuse is forbidden.
```

## Tool schema (verbatim)
```json
{
  "name": "task",
  "description": "Manage the session task system. action=reframe: FIRST investigate the epic (read files, search memory, gather evidence) and then report your understanding PLUS an honest feasibility verdict in ONE call. action=view: read the task list. action=commit: commit a subtask conclusion. action=review: post-commit checkpoint.",
  "input_schema": {
    "type": "object",
    "properties": {
      "action": {
        "type": "string",
        "enum": [
          "reframe",
          "view",
          "commit",
          "review"
        ],
        "description": "reframe now."
      },
      "epic_summary": {
        "type": "string",
        "description": "One-paragraph restatement of what this epic really asks for (action=reframe)."
      },
      "constraints": {
        "type": "string",
        "description": "Hard constraints / boundaries (action=reframe)."
      },
      "deliverables": {
        "type": "string",
        "description": "What must exist when done (action=reframe)."
      },
      "acceptance": {
        "type": "string",
        "description": "Machine-checkable acceptance criteria (action=reframe)."
      },
      "verdict": {
        "type": "string",
        "enum": [
          "DOABLE",
          "TOO_BIG",
          "IMPOSSIBLE",
          "NEEDS_RESEARCH"
        ],
        "description": "Honest feasibility judgment made AFTER investigation. DOABLE = fits one session. TOO_BIG = needs decomposition (roadmap REQUIRED). IMPOSSIBLE = cannot be done (reason REQUIRED). NEEDS_RESEARCH = you don't know how yet (research_questions REQUIRED)."
      },
      "roadmap": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "goal": {
              "type": "string"
            },
            "acceptance": {
              "type": "string"
            }
          },
          "required": [
            "goal",
            "acceptance"
          ]
        },
        "description": "REQUIRED when verdict=TOO_BIG: the decomposed subtasks."
      },
      "reason": {
        "type": "string",
        "description": "REQUIRED when verdict=IMPOSSIBLE."
      },
      "research_questions": {
        "type": "array",
        "items": {
          "type": "string"
        },
        "description": "REQUIRED when verdict=NEEDS_RESEARCH."
      }
    },
    "required": [
      "action",
      "verdict"
    ]
  }
}
```


---

# CASE A-big

## Input briefing (verbatim)
```
Epic: redesign gopi's task lifecycle so that reframe(epic understanding) and evaluate(feasibility) happen in ONE structured tool call with four honest exits (DOABLE/TOO_BIG/IMPOSSIBLE/NEEDS_RESEARCH); eliminate the evaluate->submit->dispatch->re-evaluate bureaucracy loop; narrow task_submit to a one-shot 'I found an independent work item' exit; and keep the whole thing token-efficient. Touches: internal/agent/loop.go (reframe directive + task tool dispatch), internal/tools/tasks.go (ledger), cmd/agent CLI mirrors, system prompt, tests. Existing pieces: reframe today is a free-text [REF RAMED EPIC] block produced at run start; task(action=review) already returns a structured A-E verdict; write_task_area proved LLMs accept structured commit tools (9/9 vs 0/3).
```

## Model prose (verbatim)
```
I'll investigate what I can from the briefing and my knowledge, then report an honest verdict.

**What I can reason about:**

- The epic describes an internal refactor of *my own* task tooling. The briefing is unusually self-referential — it's asking me to redesign the harness I'm currently running inside.
- Key given facts: `reframe` today emits a free-text `[REF RAMED EPIC]` block; `task(action=review)` returns a structured A–E verdict; `write_task_area` showed LLMs accept structured commit tools well.
- Named touchpoints: `internal/agent/loop.go`, `internal/tools/tasks.go`, `cmd/agent` CLI mirrors, system prompt, tests.

**What I genuinely cannot verify (no filesystem):**
- Actual current schema of `task` tool actions and their params.
- Whether `evaluate` is a real separate tool/action today or a conceptual loop; the epic implies a `evaluate -> submit -> dispatch -> re-evaluate` cycle exists.
- Exact shape of the A–E verdict, and how `[REF RAMED EPIC]` is consumed downstream.
- Whether CLI mirrors and system prompt are generated from one source or hand-maintained in parallel.

**Feasibility reasoning:** This is a design + implementation task on a codebase I can't read here. It's coherent and scoped (one lifecycle, four exits), not impossible. But it requires schema-level knowledge I don't have in this environment, and getting the exact verdict semantics + backward-compat of the loop wrong would be a fabricated plan. The honest call is **NEEDS_RESEARCH** — I need to read the actual files before committing a plan, since the epic's own premise (eliminate an existing evaluate loop) can't be confirmed from the briefing alone.
```

## tool_use input (verbatim JSON)
```json
{
  "action": "reframe",
  "verdict": "NEEDS_RESEARCH",
  "epic_summary": "Redesign gopi's task lifecycle so that epic understanding (reframe) and feasibility evaluation (evaluate) collapse into a single structured tool call exposing four honest exits — DOABLE, TOO_BIG, IMPOSSIBLE, NEEDS_RESEARCH — each with its required payload (roadmap for TOO_BIG, reason for IMPOSSIBLE, research_questions for NEEDS_RESEARCH). The goal is to kill the current evaluate->submit->dispatch->re-evaluate bureaucracy loop, reduce task_submit to a single one-shot exit meaning \"I found an independent work item\", and keep the whole protocol token-efficient. It touches internal/agent/loop.go (reframe directive + task tool dispatch), internal/tools/tasks.go (the ledger), cmd/agent CLI mirrors, the system prompt, and tests. Prior art shows LLMs reliably adopt structured commit tools (write_task_area: 9/9 vs 0/3), which is the main feasibility signal for the new structured shape.",
  "constraints": "- Must preserve four honest exits with exact semantics: DOABLE (fits one session), TOO_BIG (roadmap REQUIRED), IMPOSSIBLE (reason REQUIRED), NEEDS_RESEARCH (research_questions REQUIRED); bare TOO_BIG without roadmap is forbidden.\n- Must eliminate the evaluate->submit->dispatch->re-evaluate loop, not just rename it.\n- task_submit narrows to a one-shot 'found an independent work item' exit only.\n- Token-efficient: fewer round-trips, no redundant re-derivation of the verdict.\n- Touches internal/agent/loop.go, internal/tools/tasks.go, cmd/agent CLI mirrors, system prompt, and tests — these must stay consistent (no divergent hand-maintained copies).\n- Backward compatibility of the ledger/CLI surface is a concern; verdict semantics must remain machine-checkable (A-E style structured output).",
  "deliverables": "- A single structured `reframe` tool action that returns understanding (epic_summary/constraints/deliverables/acceptance) AND a verdict with its required payload in one call.\n- loop.go updated: old free-text [REF RAMED EPIC] block removed/replaced; task tool dispatch routes the unified reframe+evaluate call.\n- tasks.go ledger updated to store/represent the four-exit verdict and its payload.\n- cmd/agent CLI mirrors updated to match the new schema.\n- System prompt updated to instruct the one-call reframe+evaluate protocol and the four honest exits.\n- old evaluate/submit/dispatch/re-evaluate path removed or reduced to nothing.\n- Tests covering each exit and the payload requirements (esp. TOO_BIG-without-roadmap rejection).",
  "acceptance": "- Machine-checkable: invoking the unified reframe call with verdict=TOO_BIG but no roadmap fails validation; verdict=IMPOSSIBLE without reason fails; verdict=NEEDS_RESEARCH without research_questions fails; DOABLE requires no extra payload.\n- reframe emits structured output (not the free-text [REF RAMED EPIC] block).\n- A single tool call yields both understanding and verdict (no separate evaluate round-trip).\n- task_submit exposes only the one-shot independent-work-item exit (no submit/dispatch/re-evaluate chain).\n- Existing tests updated/passing; new tests for all four exits.\n- CLI and system prompt surface the same schema as loop.go/tasks.go (single source of truth).",
  "research_questions": [
    "What is the exact current schema of the `task` tool actions/params in internal/tools/tasks.go (actions, fields, and the A-E review verdict shape)?",
    "Is there an actual separate `evaluate` tool/action today, or is the 'evaluate->submit->dispatch->re-evaluate' loop an emergent flow across task_submit + dispatch + a second task pass? Where exactly does re-evaluation happen in loop.go?",
    "How is the free-text `[REF RAMED EPIC]` block currently produced and consumed downstream (parsing, storage in ledger, CLI display)?",
    "How are the cmd/agent CLI mirrors generated relative to internal/tools/tasks.go and the system prompt — one source of truth, or hand-maintained parallel copies?",
    "What is the task_submit signature today and which call sites rely on submit/dispatch, so narrowing it to a one-shot exit is non-breaking?",
    "What token-cost baseline does the current multi-step flow incur, so 'token-efficient' can be measured against it?",
    "What's the existing test layout for tasks/loop (fixtures, golden outputs) so new exit tests match conventions?"
  ]
}
```

## usage: in=147 out=1466 stop=tool_use


---

# CASE B-impossible

## Input briefing (verbatim)
```
Epic: make gopi's LLM guarantee zero hallucination on arbitrary unseen questions — the model must never state anything false, for any prompt, including questions about the future, unverified claims, and adversarial inputs. Acceptance: 100% factual accuracy across all possible inputs.
```

## Model prose (verbatim)
```
I'll investigate this before making any judgment.
```

## tool_use input (verbatim JSON)
```json
{
  "action": "reframe",
  "epic_summary": "The epic asks for an LLM (gopi) that achieves provably zero hallucination on arbitrary, unseen prompts — including questions about the future, unverified claims, and adversarial inputs — with acceptance defined as \"100% factual accuracy across all possible inputs.\" In effect this demands a guarantee of soundness over an infinite, unbounded input distribution where ground truth is sometimes unavailable (future events), sometimes contested (unverified claims), and sometimes actively manipulated (adversarial inputs).",
  "constraints": "No filesystem in this test — I could not inspect gopi's actual code, current guardrails, retrieval stack, or evaluation harness, so claims about \"current behavior\" are unverified. The stated acceptance bar (100% across ALL possible inputs) is the binding hard constraint and is what makes the request pathological. Also: an LLM's outputs are probabilistic and its knowledge is frozen at training time, so ground truth for future/unknown facts does not exist at inference.",
  "deliverables": "Cannot be delivered as literally stated. A faithful-degraded substitute would be: (a) an abstention/refusal layer that emits \"I don't know\" whenever support is insufficient, (b) a retrieval + citation mechanism tying every assertion to verifiable sources, (c) a self-consistency/verification pass, and (d) a calibrated evaluation harness measuring factual accuracy on a bounded test set. None of these yield 100% on all inputs.",
  "acceptance": "As written (\"100% factual accuracy across all possible inputs\") the criterion is unsatisfiable and therefore un-checkable in any finite test. A checkable reformulation: on a fixed, held-out benchmark of N prompts, factual accuracy ≥ X% with abstention counted as non-error, adversarially tested.",
  "verdict": "IMPOSSIBLE",
  "reason": "The acceptance criterion is unsatisfiable for fundamental, not merely engineering, reasons. (1) Ground truth does not exist for a large slice of the required inputs — questions about the future, private/unpublished facts, and genuinely unverified claims have no correct answer at inference time, so \"100% factual accuracy\" is undefined, not just hard. (2) An LLM is a probabilistic next-token predictor over an open input space; there is no finite training set or guardrail that can constrain behavior on all possible inputs, and adversarial inputs are defined by their ability to find residual failures. (3) Perfect factual accuracy over an unbounded distribution is equivalent to solving the general halting-like problem of \"knowing everything\" — it is impossible in principle, not just costly in practice. (4) Even a fully sound \"abstain on anything uncertain\" system achieves zero hallucination only by drastically reducing coverage, which the epic explicitly rejects (it demands answers on arbitrary unseen questions, including the future). You can have soundness OR universal coverage, not both, over an open domain. Ergo IMPOSSIBLE as stated."
}
```

## usage: in=143 out=720 stop=tool_use


---

# CASE C-underspecified

## Input briefing (verbatim)
```
Epic: optimize the performance of the pipeline. It is too slow. Make it faster and report the improvement. (No repo, no metrics, no baseline, no definition of the pipeline.)
```

## Model prose (verbatim)
```
I'll investigate before judging. In this test I have no filesystem access, so my investigation is reasoning from the briefing plus general knowledge, and I'll be explicit about what I cannot verify.
```

## tool_use input (verbatim JSON)
```json
{
  "action": "reframe",
  "epic_summary": "The request is to make \"the pipeline\" faster and report the improvement. Stripped of vagueness, an actionable version of this epic is: establish a reproducible performance baseline for a specific pipeline, identify the dominant bottleneck(s), apply targeted optimizations, and demonstrate a quantitative speedup against that same baseline under identical conditions. As briefed, however, none of the required anchors exist: no repository or artifacts, no identification of which pipeline, no workload definition, no metrics, no baseline measurement, and no environment in which any pipeline is accessible.",
  "constraints": "- No repository, files, or artifacts are available in this session; there is nothing to read, modify, or benchmark.\n- No definition of \"the pipeline\" (which system, which stage, what it does) is provided.\n- No metrics, profiling data, or baseline numbers exist, so no \"improvement\" can be quantified or even defined.\n- No target (speedup factor, latency/throughput SLO, cost budget) is given.\n- No environment/runner/toolchain is specified, so any measurement would be non-reproducible.\n- Any reported \"improvement\" produced without a baseline would be fabricated; honesty requires refusing that.\n- I cannot verify: workload characteristics, bottleneck location, correctness invariants, or acceptable tradeoffs (memory, accuracy, cost, maintainability).",
  "deliverables": "For the epic to be deliverable at all, the following must eventually exist: (1) an identified pipeline, repository, and runnable entrypoint; (2) a defined workload and reproducible benchmark harness with fixed inputs and environment; (3) a recorded baseline measurement (latency/throughput/resource) with variance; (4) a profiler-derived bottleneck analysis; (5) one or more applied optimizations with correctness tests still passing; (6) a re-measured result and a quantitative before/after report. Right now none of these can be produced because the prerequisites (repo, definition, metrics, baseline) are absent.",
  "acceptance": "Cannot be stated as machine-checkable today because the target pipeline and workload are undefined. If the missing inputs were supplied, acceptance would be: a benchmark command reproduces the baseline within measured variance; the optimized run is measurably faster (e.g., >= agreed speedup threshold on the same workload/environment); all correctness/regression tests pass unchanged; and the improvement report cites raw before/after numbers reproducible by a third party. None of these are satisfiable in the current state.",
  "verdict": "IMPOSSIBLE",
  "reason": "I cannot execute this as briefed because every object the task refers to is missing or undefined: there is no repository or filesystem to read, no statement of which pipeline or what it does, no workload, no metrics, no baseline, and no target. Performance work is fundamentally a measurement activity — \"faster\" is undefined without a baseline and a fixed, reproducible workload, and \"report the improvement\" is a numeric claim I could only produce by fabricating it. Fabricating a speedup number or inventing a plausible \"optimization\" for an unspecified pipeline would be worse than declaring the task blocked. This is not IMPOSSIBLE in principle (performance optimization is a well-understood discipline), but it is impossible to honestly perform given the absence of a concrete artifact and any measurable target. Unblocking requires, at minimum: the repository/entrypoint, the definition/scope of the pipeline, the workload and benchmark harness, a captured baseline, the execution environment, and an agreed target/threshold. Given those, this becomes DOABLE or, if the pipeline is large, TOO_BIG with an evidence-driven roadmap."
}
```

## usage: in=254 out=904 stop=tool_use


---

# CASE D-toobig

## Input briefing (verbatim)
```
Epic: rewrite the entire gopi codebase from Go to Rust, including the agent loop, all 17 tools, the SQLite memory engine with FTS5+trigram search, the task ledger with merge/verify lifecycle, the CLI surface (~40 subcommands), the chat/web servers, and the full test suite. Ship it as a single static binary with feature parity.
```

## tool_use input (verbatim JSON)
```json
{
  "action": "reframe",
  "verdict": "NEEDS_RESEARCH",
  "epic_summary": "The epic asks for a full-language port of the gopi agent codebase from Go to Rust with strict feature parity: the agent loop, all 17 tools, the SQLite-backed memory engine (FTS5 plus trigram search), the task ledger with its merge/verify lifecycle, a CLI surface of roughly 40 subcommands, the chat and web servers, and the complete test suite, all delivered as one static binary. This is not a single bug fix or feature; it is a rewrite-the-world request whose scope touches every module in the repository. The real ask underneath is \"produce a Rust implementation that behaves identically to the Go original across all of these surfaces,\" which is a multi-week, multi-team-scale effort. My job in this step is not to promise a plan I cannot honor but to state what is genuinely needed before any Rust code can be written responsibly.",
  "constraints": "No filesystem available in this session: I cannot read the actual Go source, so the tool count (17), subcommand count (~40), schema details (FTS5+trigram), or server behavior cannot be verified and must be treated as the briefing's claims, not facts. Feature parity is a hard boundary: any behavioral divergence in the agent loop, ledger lifecycle, or search ranking is a failure, so the Rust port must be defined against concrete, executable parity oracles (the existing Go tests and captured I/O), not against my memory of how such systems usually work. Static single-binary delivery constrains dependencies (no runtime Go interop, no cgo-style escapes; SQLite must be bundled, e.g. via a vendored/static build with FTS5 enabled). The task ledger's merge/verify lifecycle and the chat/web server concurrency semantics are the highest-risk areas for subtle drift. One session cannot produce this; pretending otherwise would fabricate a plan.",
  "deliverables": "Before coding: an inventory of the real Go codebase (module graph, exact tool list, exact subcommand list, database schema, ledger state machine, server routes) produced by reading the repo, not the briefing. Then: a parity test harness that can run the same inputs against Go and Rust and diff outputs. Then: a Rust workspace scaffolding the crates (agent loop, tools, memory/SQLite+FTS5+trigram, ledger, CLI, servers, tests) and a static release build recipe. All of that is prerequisite evidence for a real port; none of it is claimed as done here.",
  "acceptance": "If the port were done, acceptance would be machine-checkable: the Rust binary builds as a single static artifact (e.g. ldd shows no dynamic deps) and passes the full ported test suite; every one of the enumerated tools has a behavioral parity test against the Go implementation; the CLI exposes and correctly implements every enumerated subcommand with matching exit codes and output schemas; the memory engine returns identical FTS5 and trigram search results for a fixed corpus; the ledger's merge/verify lifecycle passes a state-machine conformance test; and the chat/web servers satisfy the same request/response contract tests. These criteria are not satisfiable until the itemized Go inventory and parity harness exist.",
  "research_questions": [
    "What is the actual structure of the Go codebase: module/package graph, LOC per package, and which packages have the highest coupling to be ported first?",
    "What is the exact, enumerated list of the 17 tools and their input/output contracts, and the ~40 CLI subcommands with flags, exit codes, and output formats?",
    "What is the precise SQLite schema for the memory engine, and how are FTS5 and trigram indexes combined for ranking — is parity defined by query result sets, ordering, or scores?",
    "What is the ledger merge/verify lifecycle as an explicit state machine (states, transitions, persisted fields, concurrency rules)?",
    "What are the chat and web server protocols, routes, and concurrency expectations, and what does 'feature parity' mean operationally for streaming/SSE if present?",
    "What is the concrete parity oracle: does the Go test suite exist and fully cover these surfaces, and can a differential harness run both binaries over shared fixtures?",
    "What Rust crates are acceptable for a static single binary (rusqlite with bundled SQLite+FTS5, axum/hyper, clap) and what is the exact build/target recipe for full static linking including libc?",
    "What is the realistic effort budget per subsystem, and is there any existing partial Rust port or design doc that would change the plan?"
  ]
}
```

## usage: in=165 out=1065 stop=tool_use
