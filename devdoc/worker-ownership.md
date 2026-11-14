<!-- doc-class: normative -->
# Worker ownership — who starts a worker, and what an agent does instead

**Status: canonical. This is the ONE block every other document points at.**

Operator ruling 2026-09-12 (verbatim quote + full audit:
`devdoc/pa-never-runs-tasks-20260912.md` §1, §5, §7). Everything below is the
short form that the rest of the repo references instead of re-explaining.

---

## The rule

**hermes / PA — and every other agent — NEVER start, restart, or kill a
worker, and NEVER run a task directly.** The worker is **the operator's**
process. An agent that starts one takes a process it cannot own, cannot see
die, and cannot clean up; that is the failure the 2026-09-12 ruling names and
the guard now refuses.

An agent's verb is the submission. The **queue is the handoff**.

## The canonical operator-only block

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md**
>
> The commands on this page that launch a worker (`gopi worker`, any
> `~/bin/gopi-worker-up.sh N` form, `nohup gopi task run <id>`, or a script
> wrapping them) are **the operator's** — a human, in his own terminal.
> **hermes / PA / any agent must NEVER run them**: they never return, they
> stall the caller, and killing the caller's process group takes the workers
> with it (04:12 incident, `devdoc/salvage-20260912-0412-incident.md`).
>
> **What an agent does instead — always, with no exception:**
>
> ```bash
> gopi task submit --summary "<what>" --payload "<self-contained briefing>"
> ```
>
> The queue is the handoff; the operator's worker drains it.
>
> **Expect the guard to refuse you — that refusal is the contract working,
> not a bug.** The ruling ships a guard (`internal/config/agentguard.go`, on
> `feat/pa-never-runs-tasks`, merged into the tree that carries the ruling)
> that makes the two forbidden verbs fail with **exit 3** (`gopi worker`, any
> form; `gopi task run`, any form). Until that branch is merged the refusal
> arrives from the operator, not the binary — the rule binds an agent either
> way.
> A refused `gopi worker` is **the expected outcome**; do not work around it
> (no `setsid`, no `nohup`, no wrapper script, no `env -u`, no
> `GOPI_ALLOW_AGENT_SPAWN=1`) — report it, or submit the work instead.

## Who owns what

| Thing | Owner | An agent's relationship to it |
|---|---|---|
| Starting a worker (`gopi worker`, `gopi-worker-up.sh N`) | **the operator** | never run it; `exit 3` is the expected refusal |
| Running one task (`gopi worker <task-id>`) | **the operator** | never run it; submit the task instead |
| daemon (`gopi task serve` / `gopi task merger`) | **the operator** | never start/restart it |
| worker lifetime, restarts, kills | **the operator** | stop by explicit PID only, never `pkill -f gopi` |
| `gopi task submit` | **the agent** | this is the agent's ONE queue verb |
| `gopi task list` / `show` / `view` / `status` | anyone | read-only, always fine |

## The session environment — the launcher owns DISPLAY

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**A worker inherits the launcher's whole SESSION ENVIRONMENT and can never
re-acquire any part of it.** `setsid` + `nohup` give a worker its own session
and no controlling terminal; they change NOTHING about `DISPLAY`, `XAUTHORITY`
or `cwd`. So a worker started from a shell whose `DISPLAY` is an SSH forward
(`localhost:N`) carries a display address whose socket dies with that forward,
and every `screen_capture` / `act` it makes afterwards fails (exit 1) — silently,
until the first GUI tool call, hours later.

| Question | Answer (measurable) |
|---|---|
| Who owns `DISPLAY` for a worker? | **The launcher's shell.** It is inherited verbatim; the worker cannot fix it later. |
| What is the durable local display on this host? | **`Xvfb :99`** (`DISPLAY=:99`). `:99` → capture exit 0; `localhost:15.0` / unset → exit 1. |
| Does `XAUTHORITY` need to be managed too? | **No.** It resolves to the `~/.Xauthority` FILE, so setting `DISPLAY` correctly is sufficient. |
| What does the operator do about a stale/forwarded `DISPLAY`? | Launch from a shell whose `DISPLAY` is the durable local one (`:99`). |
| How is a wrong display discovered? | Every worker prints ONE `display preflight` line at loop start (reachable / UNREACHABLE + the remedy), on all three spawn doors. |

The full decision record — why the worker ANNOUNCES rather than REFUSES, why the
launcher-script door stays deliberately un-gated for the operator while the guard
judges the LAUNCHER's environment, the corrected "cannot launder this" claim and
the launcher's loud launch verdict — is `devdoc/worker-display-ownership.md`.

After a code update the worker is a stateless one-shot (restart it) and the
merger is a long-running daemon that keeps executing the OLD code until the
**operator** restarts it — `devdoc/pa-never-runs-tasks-20260912.md` §5.5, and
Rule 12 for the rebuild half.

## The pointer — how every other document refers here

Exactly one line, in whatever comment form the file's language allows:

```text
OPERATOR-ONLY — see devdoc/worker-ownership.md
```

- Markdown: `<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->`
- Shell:    `# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md`
- Go:       `// WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md`

The marker token is `WORKER-OWNERSHIP: OPERATOR-ONLY`; a one-line pointer
carries it plus this file's path. `internal/testutil/worker_ownership_gate_test.go`
enforces it: any document under `devdoc/` or `briefings/` that mentions one of
the three launch shapes must carry the pointer **or** be named in that test's
`workerOwnershipDismissed` table with a reason. A dismiss entry is a
reviewed, reasoned exception — never a way to un-gate a file that should be
annotated.

**History is kept.** The annotation is an insertion; no audit record is
deleted or rewritten. A document that *quotes* the forbidden command inside an
incident post-mortem keeps the quote — the pointer is what stops it reading as
an instruction.
