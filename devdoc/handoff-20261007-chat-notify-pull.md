<!-- doc-class: historical -->
# Handoff — chat-to-chat notify, peerbox-vs-HP division of labour, and two pending rows

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Read this immediately after starting a session on this host.**
Written 2026-10-07 by the chat session `c1687f35-f4c9-4abf-b203-9704ecbae992`
(on `hostb-Laptop`, repo `/home/user/work/GoPi`, master tip `715d9ffc5`),
**for its own next self**, which will not remember any of it.

This is a *state + action* document. It records what was measured (not believed),
what was submitted, and what the next session must NOT re-litigate.

---

## 0. THE ONE THING TO KNOW FIRST: two rows are pending and the operator will run them

Both are on **HP's own ledger** (not forwarded — see §2). `gopi task list --status pending`:

```
#01a11548-e636-7e81-b122-64f12d938eb6  [pending]  bug: compute_nodes NO CONSUMER 报告每次调用都打印 …  [next]
#01a11533-80fc-7648-85ad-190532917ccc  [pending]  feature: gopi task pull --peer <alias> --task-id T …（push-only 的镜像腿）
```

| id | tag | what it is | briefing source |
|---|---|---|---|
| `01a11548-e636-7e81-b122-64f12d938eb6` | `bug` | every gopi invocation prints a 400-char `compute_nodes … NO CONSUMER` line on stderr | `/tmp/compute-nodes-noise-briefing.md` (inlined into the row) |
| `01a11533-80fc-7648-85ad-190532917ccc` | `feature,cli` | new `gopi task pull` verb — fetch ONE row peer→local on demand | `/tmp/hp-pull-feature-briefing.md` (inlined into the row) |

`[next]` marks the bug row: the queue is **priority-first (bug → 1)**, so `gopi worker`
takes the bug row before the feature row. That is intended, and it is also the safer order —
see §4 for why the bug row is a prerequisite for comfortable work on the feature row.

**Read each briefing with `gopi task show <id>` before doing anything.** The briefings are
the authoritative contract; this document only adds what they do not carry.

---

## 1. The notify channel — chat↔chat across hosts, measured WHICH WAY

This session's only real *deliverable* was a working demonstration, and the four legs were
all observed. Do not re-derive this; do not "improve" the recipe in §1.3.

### 1.1 The four legs, with the evidence each produced

| leg | command | observed evidence |
|---|---|---|
| hp → peerbox | `ssh peerbox 'gopi notify --session d3dbb2ed-1384-49df-8ee4-172375b0a570 --message "…"'` | `queued: message f4191d19146eb6c4 …` |
| peerbox → hp (reply) | peer ran `ssh hp` + `gopi notify` | my inbox `messages/inbox.jsonl`: `{"id":"5393c58d014457cf", … "source":"notify"}` |
| hp → peerbox | `gopi notify --peer peerbox --session d3dbb2ed-… --message "…"` | `queued on peerbox: message b5e0b6eb01deb702 …` |
| peerbox → hp (reply) | peer claimed to use `--peer hp` | arrived at a turn boundary as a `[system]` line |

### 1.2 `--peer` is the right form — `ssh` was never load-bearing

The operator's original recipe spelled the hop as `ssh peerbox` + `gopi notify`. That works,
but `gopi notify --peer <alias>` is the cross-machine form and needs no shell: the peer
alias resolves through gopi's own declared topology (`sync_peers` in `settings.json`).
**Prefer `--peer`.** The peer session was told this and accepted the correction in its reply:
*"I reached for ssh when `gopi notify --peer <alias>` is already the cross-machine form."*

### 1.3 The recipe, and the two traps

```bash
# send (from this host, to a chat session on another host)
gopi notify --peer peerbox --session <peer-session-id> --message "…"
```

- **Delivery is at a ROUND BOUNDARY, not an interruption.** An IDLE console wakes for it and
  opens a turn by itself; a turn already in flight is never interrupted. Measured latency
  end-to-end: ~45 s (one wake-up + one model turn). Do not conclude "it failed" at 10 s.
- **The RECEIVER's session directory `messages/` is created lazily, on first receipt.**
  Before any message arrives, `~/.gopi/sessions/<id>/messages/` **does not exist** — a missing
  directory is *"nothing ever received"*, **not** *"delivery failed"*. Delivery success is the
  SENDER's `queued:` line; receipt success is the receiver's `inbox.jsonl`.
- **`source` is `notify` on BOTH transports**, so the inbox cannot tell you whether the far
  side used `--peer` or shelled out `ssh`. If that distinction matters, ask the peer to state
  the command it ran — the transcript will not show it.

---

## 2. The topology, and the direction question I got WRONG

**The operator corrected this session on a reversed argument. Do not repeat the error.**

### 2.1 What is declared, measured from `settings.json` on both hosts

| | HP (`hostb-Laptop`) | peerbox |
|---|---|---|
| `role` | `client` | `server` |
| `sync_peers` | `["peerbox"]` | `["hp"]` |
| `sync_to` | `peerbox` | — |
| `memory_peers` | `["peerbox"]` | `["hp"]` |
| `compute_nodes` | `[{name: peerbox, …}]` | absent |
| `task_sync` | **`false`** | (absent) |

HP's startup banner says it plainly:
`[gopi] task-sync: off (declared in task_sync: false — automatic task-row exchange disabled)`

### 2.2 The operator's working model (this is the DESIGN, not a guess)

> peerbox 的 workload 太重了，我要在 hp 上提交本地执行，自动会把 branch push 回 peerbox，
> 这个已经是有了。

- **HP submits AND EXECUTES locally.** A row filed on HP is claimed and run by HP's own worker,
  against HP's own ledger (`~/.gopi/memory.db`, 3288 rows and counting).
- **The finished branch is pushed back to peerbox at close-out.** This already works.
- **peerbox = git upstream / integrator.** It does not carry the feature workload.

### 2.3 The correction I had to make (recorded so it is not re-argued)

I first argued that `task_sync` was the relevant knob and that HP's `task_sync=false` meant
something was missing. **That was backwards.** The authoritative statement is in
`internal/config/task_sync_switch.go`:

> **Neither delivery face consults this value**: the finish-cleanup branch push-back chain
> does not read it (a client with task sync OFF still HANDS ITS BRANCH BACK)

The branch push-back is chosen at `internal/tools/task_finish_cleanup.go:1232`:

```go
if IsServerNode() || finishCleanupServerTarget() == "" {
    return finishCleanupServerRecycle(...)   // server: recycle the ground
}
return taskFinishCleanupChain(...)           // client: the DELIVERY chain (merge, build, report, PUSH branch)
```

HP declares `role=client` + `sync_peers=["peerbox"]` ⇒ `finishCleanupServerTarget()` resolves
`peerbox:/home/user/work/GoPi` ⇒ **HP takes the client delivery chain. Branch push-back is ON
right now, independent of `task_sync=false`.**

And the submit side is consistent with it: `taskSubmitNeedsForward()`
(`internal/tools/task_submit_forward.go`) returns **false** when `config.TaskSyncEnabled()` is
false, so `gopi task submit` on HP **inserts locally**. Confirmed live:

```
Task queued: 01a11533-80fc-7648-85ad-190532917ccc (status=pending, source=agent, tags=feature,cli)
```

**Therefore: do NOT turn `task_sync` on for HP.** It would forward HP's rows *away from the
node that is supposed to run them* — the exact opposite of the operator's model. The
`task_submit_forward.go` engine (client → `sync_to`, confirm via the peer's own `task show`,
then echo into the local mirror) is written, tested, and **deliberately OFF here**.

### 2.4 The gap the feature row fills

The DELIVERY leg (HP → peerbox branch) exists. The **RECEIVE** leg does not: there is no way to
say *"fetch me these specific rows from peerbox and let me run them here."* The existing verbs
are the wrong shapes:

- `gopi task sync --peer peerbox` — a BIDIRECTIONAL anchor-driven reconciliation SESSION, whole-window.
- `gopi task harvest --peer peerbox` — the SyncML pull half, also window-driven, and it pushes
  every local branch back as a side effect.
- `gopi task sync --push-only --task-id T` — the one-row manual face, but in the **PUSH** direction.

That mirror image is task `01a11533-80fc-7648-85ad-190532917ccc`.

---

## 3. The `compute_nodes` noise — why it is a bug row and not a nit

`cmd/agent/compute_nodes_unwired.go` was added by commit `2fcf9a317` (**task `01a0ed9f`**) to
make an inert knob visible (Rule 21 / Rule 15). Its stated intent:

> "IT REPORTS … ONLY WHEN SOMETHING IS DECLARED … so 'my whitelist was read' and 'nobody
> declared anything' are distinguishable"

The `len(nodes) == 0` early return honours that half. **The defect is the other half: the
trigger is "the knob is declared", but for a REPORT the correct trigger is "the knob is
declared AND THIS COMMAND CONCERNS WHAT IT AFFECTS."** The call site is
`cmd/agent/tmpdir_inject.go:111` — the pre-chain bootstrap block **every verb passes through** —
so on this host (which legitimately declares `compute_nodes`) the line fires on **every command
forever**, including `gopi notify`, which has nothing to do with dispatch.

Observed verbatim in this session on: `gopi notify --peer …`, `gopi task sync -h`,
`gopi task sync --status`, `gopi task send -h`, `gopi task submit …`, `gopi task show …`,
and even `echo hi | gopi`.

**The four consequences are in the row's briefing** (noise floor; not actionable per
invocation; it trains the reader to ignore stderr — i.e. it makes the knob LESS visible, not
more; and it fires on verbs that cannot read the knob). The briefing prefers **Option A**
(narrow the trigger to the worker/serve/merger start path, where a dispatcher would have run,
leaving `gopi --help-env`'s `NO CONSUMER` group heading as the standing statement) and forbids
weakening the Rule 21 detection itself: the finding is TRUE and both
`compute_nodes_unwired_test.go` gates must keep passing.

**Every reading in this section was done from source; the REPRODUCTION is free** — just run any
command and look at stderr.

---

## 4. What the next session should do, in order

1. **Re-read both rows**: `gopi task show 01a11548-e636-7e81-b122-64f12d938eb6` and
   `gopi task show 01a11533-80fc-7648-85ad-190532917ccc`. The briefings are the contract.
2. **Do not re-litigate §2.** The direction (HP executes locally; branch pushes back to peerbox;
   `task_sync` stays OFF) is the operator's decision and is measured in the code, not inferred.
3. **Expect the bug row first** (`[next]`, priority 1). It makes every subsequent command's
   stderr readable, so the feature row is easier to work afterwards.
4. **If the operator asks for a hosted chat again**, use §1.3 verbatim — `--peer`, not `ssh`,
   and remember the round-boundary latency and the lazily-created `messages/` directory.
5. **If a worker was already running these rows**, check for a branch before starting anything:
   `git branch -a | grep 01a115` and `gopi task for-branch <branch>`.

### What is NOT done (honest)

- **No code was written or committed by this session.** It is a chat: per Rule 37 a chat owns no
  branch and no worktree, so it dispatched rows instead. The chat boundary permits writing and
  committing `devdoc/` only — which is what this file is (see §5).
- **The HP→peerbox branch push-back is verified from SOURCE, not from a live round trip.** I read
  the branch at `task_finish_cleanup.go:1232`, the target resolver
  `finishCleanupServerTarget()`, and `gitPushbackPlan` — but I did **not** observe a real
  `submit → local claim → work → branch lands on peerbox` cycle in this session. If that matters,
  the cheap proof is: `ssh peerbox 'git -C /home/user/work/GoPi rev-parse <branch>'` after a task
  run completes, not the push's exit code.
- **The `--peer` reply leg is the peer's CLAIM, not my observation.** I saw the message arrive;
  I did not see which channel carried it (`source: notify` on both, per §1.3).

---

## 5. Provenance

- Session: `c1687f35-f4c9-4abf-b203-9704ecbae992`
  (`~/.gopi/sessions/c1687f35-f4c9-4abf-b203-9704ecbae992/`).
- Host/repo/binary: `hostb-Laptop` · `/home/user/work/GoPi` · master `715d9ffc5`,
  binary built `2026-10-07T07:33:32Z`.
- Files read (source of every claim above): `internal/config/task_sync_switch.go`,
  `internal/config/node_role.go`, `internal/tools/task_submit_forward.go`,
  `internal/tools/task_finish_cleanup.go` (l.1232, l.2333),
  `internal/tools/task_sync_git_pushback.go`, `internal/tools/memory_sync_delta.go`
  (`IsServerNode`), `cmd/agent/compute_nodes_unwired.go`, `cmd/agent/tmpdir_inject.go` (l.111),
  `cmd/agent/compute_nodes_unwired_test.go`.
- Commands run (all output quoted above): `gopi notify` (both forms), `gopi task sync -h`,
  `gopi task sync --status`, `gopi task submit -h`, `gopi task submit …` (×2),
  `gopi task show …`, `gopi task list --status pending`, `gopi config show`,
  `echo hi | gopi`.
- Briefing texts submitted with the two rows: written to `/tmp/hp-pull-feature-briefing.md` and
  `/tmp/compute-nodes-noise-briefing.md`, then **inlined into the rows themselves** (they are the
  durable copy — `/tmp` is not).
