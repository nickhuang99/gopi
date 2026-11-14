<!-- doc-class: normative -->
# Task provenance: only a person may claim `human`

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: *task source fake human* (2026-09-23). Operator's ruling, verbatim:

> only true human can claim human, for other cases, either 'merger', worker,
> server, or simply agent are all acceptable sources

## The defect (measured on the operator's ledger)

`gopi task submit` — the CLI face every agent also uses — defaulted the row's
source to the literal `human`:

```go
// cmd/agent/cli_tasks.go, runTaskSubmit (before)
source := opts.source
if source == "" {
    source = "human"
}
```

There was **no evidence check anywhere**: the CLI stamped a human claim whatever
process typed it. Census of `memory.db` on 2026-09-23:

| source | rows | of which |
|---|---|---|
| `human` | 779 | **457 carry a session id**, 382 carry a model |
| `auto`  | 993 | the merger's enqueue AND the harness's startup registration, conflated |
| `llm`   | 633 | the `task_submit` tool and the reframe decomposition |

The epic row itself (`01a0cdfc`) read `source=human` while carrying
`session=d81d5441`, `model=deepseek-flash` and `run_method=worker` — three marks
of an agent, one claim of a person. A provenance column that says `human` for
an agent's row is not a label with a wrong value; it is a column that cannot be
used for the thing it exists for.

## What lands here

### 1. One vocabulary, in a leaf package

`internal/tasksource` — constants only (Rule 14), no imports of project packages,
so every writer can reference it without a cycle.

| value | meaning |
|---|---|
| `human` | **the only claim of a person's authorship** |
| `agent` | an LLM agent session filed it (tool call, reframe fan-out, CLI from an agent's shell) |
| `worker` | the task executor's own follow-up row |
| `merger` | the merge path's machine enqueue (conflict / hand-over / ceiling rework) |
| `server` | the resident machinery: startup self-registration, sync-replicated rows |

Retired spellings stay **accepted on input** and are stored canonically:
`llm` → `agent`, `auto` → `merger`. They are aliases, not a second vocabulary —
without them every historical row and every peer ledger that still sends them
would be rejected, and `auto`'s two meanings (merger enqueue vs harness
registration) could not be told apart, which is exactly why the spelling was
retired.

### 2. The evidence, reused rather than re-invented

`config.AgentSpawnSignatureNow()` is the signature reader the worker/`task run`
spawn guard already maintains (researched on the live host 2026-09-12):
`GOPI_EXECUTOR_SESSION_ID`, `HERMES_SESSION_ID`, the pid-anchored
`GOPI_AGENT_SESSION`, the operator's negative signature `GOPI_HUMAN_TERMINAL`,
plus the procfs parent check (`parentIsWorkerGopi`). Provenance reads those same
signals, so there is one definition of "inside an agent session" in the tree.

### 3. The derivation ladder (`tasksource.Derive`)

1. nothing requested → **derive** from the signature (never a literal default);
2. a **human** claim → kept **only** with evidence, else the derived role;
3. the retired `auto` → re-derived (a writer that did not say which role it is
   has no claim to record);
4. any other known value → kept (a role that speaks for itself needs no evidence);
5. an unknown value → re-derived at the library boundary; the **CLI refuses it
   loudly** before that (see below).

`DefaultFor`: worker (procfs parent) → agent (agent chain above) → human.

#### 3a. A TYPED human claim is REFUSED, not rewritten (the second seam, 2026-09-23)

Step 2 is right for a LIBRARY caller — a writer that passes no source, or passes
one as an internal default, must still get an honest row. It is **wrong for a
typed flag**: `gopi task submit … --source human` from an agent session exited
**0**, printed `Task queued: … source=agent`, and stored `agent` — the caller's
own assertion silently replaced by the process's derived role, with the rule
living only in a source comment, while `-h` advertised `human` as one of five
selectable values. A refusal the caller cannot see is a silent failure (Rule 15).

The fix is a surface, not a new gate: `tasksource.HumanClaimDeniedNow()` exposes
the SAME predicate `Derive` already uses, and `runTaskSubmit` REFUSES on it
(exit 1, no row):

```
$ gopi task submit "sp" --source human --task-id t-a      # inside an agent session
gopi task submit error: refusing --source human: only a process with no agent
chain above it may record human (this process is inside an agent session, so it
may not claim a person filed the row — the row would be stored as agent). File
it with the role you actually are (agent/worker/server), or, if you are the
operator, re-run with GOPI_HUMAN_TERMINAL=1        # rc=1, ledger untouched
```

Both sentences are named constants (`taskSubmitInvalidSourceFormat`,
`taskSubmitHumanClaimRefusedFormat`) and the `--source` help paragraph states
the refusal, so the rule is discoverable from `-h` rather than from the source.
`GOPI_HUMAN_TERMINAL=1 … --source human` still yields a `human` row (the gate is
a gate, not a ban), and the four machine roles keep round-tripping.

#### 3b. The trap: a help paragraph in this file is LINE-ANCHORED land

The `--source` text lives in `cmd/agent/cli_tasks.go` **above ~2500 lines that
normative docs cite by line number** (`internal/agent`'s docanchor gate
re-verifies every `file:NNN` in every `normative` devdoc). Inserting four help
lines moved every one of those anchors and turned that gate's output into a
*different* set than base's pre-existing drift — six new problems, one of which
dropped a cross-doc stale-permit entry.

The edit that works is the one that keeps the file's line count: the paragraph
was re-flowed to state the refusal in the SAME eight lines, so every anchor
below resolves exactly where it did at base and the gate's output on this branch
is byte-identical to a run on a detached `HEAD` worktree. Two corollaries:

1. **Do not `gofmt -w` this file to "clean it up".** `gofmt -l
   cmd/agent/cli_tasks.go` names it **at base too** (one alignment site,
   pre-existing); `gofmt -d` renders the same body before and after. Running it
   would reflow long regions and move ~139 anchored lines — far more drift than
   the site it fixes.
2. **Compare the gate's problem SET, not its exit code.** The gate is already
   red on master, so "did I make it worse?" is answered by diffing the two runs'
   sorted problem lines (`<doc>: anchor …`), which is also how the task report's
   `## Verification` records it.

### 4. Enforcement at the funnels, not at 14 call sites

* `internal/tools/tasks.go` `insertTaskRow` — the ONE all-column INSERT every new
  row passes through — derives an empty source, holds a `human` claim to the same
  evidence, and canonicalizes an alias. A library caller that bypasses `TaskSubmit`
  therefore cannot record a claim it has no evidence for.
* `TaskSubmit` validates the **caller's own spelling first** (a typo must fail
  loudly), then applies the ladder, then asserts the result is a ledger value.
* Every production writer now names its role: `verify_gate.go` ×2,
  `task_merge_backlog.go`, `task_adoption.go`, `task_merge.go`,
  `rebuild_vet_mergeset.go`,
  `dirty_holder_salvage.go` (via `taskMergeSourceAuto = tasksource.Merger`),
  `memory_sync_task.go` (→ `server`), `internal/agent/taskregister.go` (→ `server`),
  `cmd/agent/cli_tasks.go` branch-pushed hint (→ `merger`),
  `internal/agent/loop.go`'s `taskSourceLLM = tasksource.Agent`.
  (`merge_ceiling_rework.go` and `merge_handover_counter.go` were two more until
  2026-11-01, when the merge-ceiling and hand-over-counter lanes went with the
  top-level `gopi merge` verb — the list above names the writers that exist.)

## Two bugs found by the tests and the live run (worth remembering)

1. **`HumanEvidence` initially trusted `!sig.AgentRun()` alone.** The spawn guard
   deliberately reports `AgentRun() == false` for a worker's own child (it EXEMPTS
   that chain), so the executor's follow-up rows would have been entitled to claim
   a person. Caught by the package's own table (`worker child asks for human →
   worker`); the fix is an explicit `ParentIsWorker → false`.
2. **`Normalize`'s second return was read as "recognized" when it meant "already
   current".** `ok == false` for exactly the aliases the canonicalization branch
   existed for, so `llm`/`auto` were stored unchanged. One question, one answer:
   `ok` now means recognized, and "was it current?" is `got != input`.

A third, found only by the live run: **the CLI derived before `TaskSubmit` saw the
caller's spelling**, so `--source hman` was silently rewritten into `agent`
instead of refused. The CLI now validates the caller's own spelling first.

A fourth, found by an audit of the surface itself: validation covered a
MISSPELLING but not the one value the gate actually denies — `--source human`
from an agent session was accepted, rewritten to `agent`, and stored, rc=0, with
nothing on stderr (Rule 15's exact shape). The CLI now refuses a denied human
claim by name; the end-to-end guard is
`cmd/agent/cli_tasks_source_gate_e2e_test.go`, driven with
`go test ./cmd/agent/ -run TestTaskSubmitSource`.

## Honest limits (Rule 15 — named, not hidden)

* **The signature travels in the ENVIRONMENT.** Derivation can prove "no agent
  chain sits above this process"; it cannot prove the presence of a person. An
  unattended script with a clean environment still reads `human`. This is the same
  evidence the spawn guard already trusts, and it is strictly better than a
  default that stamped `human` unconditionally.
* **A deliberate forgery defeats it** (`env -u GOPI_EXECUTOR_SESSION_ID gopi task
  submit …`), exactly as it defeats the spawn guard. Detection is the worker count
  and the log, not a refusal.
* **Historical rows are NOT rewritten.** The ledger is append-only (the iron rule
  at the top of `internal/tools/tasks.go`), so the 457 agent-marked `human` rows
  keep their value: this change is the epoch boundary, not a backfill. A reader
  that needs to know whether a *`human`* row of unknown age was really a person
  must date it against this change.
* **`run_method` is a different axis.** The CLI's `worker`/`serve`/`daemon`
  launcher identities say HOW a row was run, not WHO filed it; they are left
  alone. The operator's vocabulary was about provenance, and conflating the two
  axes would repeat the mistake `auto` made.

## Verification recipe (reusable)

A live matrix against a scratch ledger — never the operator's `memory.db`:

```bash
E2E=$(mktemp -d <scratch>/e2e-source-XXXX)
export GOPI_MEMORY_DB="$E2E/memory.db"
B=./bin/gopi   # CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/

# A agent session (the defect's exact shape)  → agent
env GOPI_EXECUTOR_SESSION_ID=<id> GOPI_AGENT_SESSION=999999 $B task submit "…" --payload p --task-id t-a
# B clean shell (a person)                    → human
env -u GOPI_EXECUTOR_SESSION_ID -u GOPI_AGENT_SESSION $B task submit "…" --payload p --task-id t-b
# C agent asks for --source human (a claim, not a forgery)
#   → REFUSED, exit 1, no row (post-fix; pre-fix it exited 0 and stored agent)
# D --source merger                           → merger
# E --source auto / F --source llm            → agent (re-derived)
# G --source hman                             → REFUSED, exit 1
# H under a real comm=gopi argv[1]=worker parent → worker
```

**Case H needs the parent's `argv[1]` to be the bare verb**, not a path: the probe
is a tiny `gopi`-named binary that execs `sh worker` *with the scratch dir on
`PATH`*, so the shell's `argv[0]` is `worker` — `"gopi worker"` is what
`parentIsWorkerGopi` matches. It must also run with a cwd inside a git work tree
(the ledger verifies the submit-time repo).
