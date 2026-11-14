<!-- doc-class: historical -->
# Requirements (reverse analysis) — 01: the task ledger — what a task row is FOR

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Series**: `[2/11]` of the reverse requirement analysis. Method, `why` standard,
output shape and the operator's binding constraints come from
`briefings/brief-requirements-reverse-analysis.md`. Module brief:
ledger row `01a0ab3d-1e94-7590-bb98-e6fe771f5403` (B1/12 of the sibling series
`01a0ab12…`, which names this deliverable's path `devdoc/requirements/01-task-ledger.md`).

**Front matter — the boundary I chose**

- **Boundary**: *what a task row is FOR and what the operator reads back out of
  it.* Lifecycle and statuses; who may write which field; what is immutable;
  how a row is created (submit, auto-register, reissue); how a row's terminal
  state is judged; the operator's verbs over rows; the row's event stream; the
  cross-node convergence of rows.
- **In scope (evidence)**: `internal/tools/tasks.go` (store, statuses,
  transitions, CAS claim, immutability, archive), `taskstate.go`, `taskzombie.go`,
  `taskproc.go`, `taskrunguard.go`, `tasksummarytext.go`,
  `task_cancel*` paths, `memory_sync_*.go` (the rank ladder and its rulings),
  `tasknotify.go` (the event stream), `internal/agent/taskregister.go`
  (auto-registration), `cmd/agent/cli_tasks*.go` (the operator's face),
  `internal/tasktags`, `internal/taskverbs`, `internal/taskverdict`,
  `internal/runcmd`.
- **Out of scope**: the *execution* of a task (`runTaskRun`, the worker loop,
  the child process, the session JSONL) — that is the execution module; the
  merge execution (module 02); the CLI's whole verb surface (module 08); the
  tool implementations the agent calls (module 07). `task_finish_cleanup.go`
  (push + worktree recycle) is cited where it decides a row's *delivery*
  meaning, but its mechanism belongs to modules 03/04.
- **Boundary why**: the user-visible responsibility is *"I hand work to the
  machine, and later I can see what happened to it."* That responsibility is
  not a directory. `internal/tools` holds both the ledger and the agent's tool
  implementations — two different actors in one package (the umbrella map's
  mismatch **M1**; `internal/agent/taskregister.go` writes rows from the agent
  side and `cmd/agent/cli_tasks.go` from the operator side, while
  `internal/tools/tasks.go:8` declares the ledger "an AUDIT/TRAIL layer, not
  runtime control" — even as `:45-48` of the same file calls it a WORK QUEUE).
  I bound by responsibility and accept, as a finding (§6-G9), that the *writer*
  boundary is crossed by five packages with no single enforcement point.
- **Overlap**: sibling `01a0ab3d-…` (B1/12) overlaps this module by design; the
  umbrella series `01a0ab12-2d2d-7c54-acb4-7c77e3024b69` is pending and had no
  document at the time of writing. **Superseded by the reconciliation below**: a
  document *does* exist on a sibling branch (`docs/requirements-task-ledger` @
  `5961f171`), which this pass judged, completed and supersedes; the "nothing to
  complete" sentence was true only of the sibling's own predecessor chain, not of
  the deliverable.

**Reconciliation — the two candidate sources for this deliverable** (added by
the B1/12 completion pass, 2026-09-17, working tree `~/workspace/gopi-req-b1`,
branch `feat/req-b1-task-ledger` off master `17cec86b`):

| source | what it is | verdict |
|---|---|---|
| `docs/requirements-task-ledger` @ `5961f171` (824 lines, the version this branch starts from) | the sibling task's document. Its own front matter claimed *"no `devdoc/requirements/` content exists"* on any branch — **that claim was already false when it was written**: `archive/docs/requirements-task-ledger` exists as an archive tag **and** the branch was still live at `5961f171`. The prior author checked `devdoc/requirements/` from a branch and missed its own predecessor on the same tip. | **conforms to the 8-section standard** — front matter with boundary + M1, §1 with no code identifiers, §2–§5, §6 with serves/falls-short/over-builds + C1–C5 + F1–F3, §7 naming 9 existing tests, §8 with per-requirement confidence, the five incidents, the unknown list, and an evidence appendix. **Judged and completed rather than redone.** |
| `feat/req-b1-task-ledger` (this branch) | **this** document — the judged-and-completed version: the same 8 sections, with every quantitative claim **re-measured** (the prior version's numbers were ~3 % stale: the ledger moved 2389 → 2421 rows during the workspace's normal operation), three citations corrected by reading the cited lines back, the §6 four-surface split made **explicit**, and one new finding (**F4**) added. | supersedes the prior branch's copy as the deliverable; both carry the same `file:line` evidence. |

**Third pass — the verification pass (2026-09-18T10:5xZ, worktree
`~/workspace/gopi-req-ledger-m3`, branch `feat/req-task-ledger-module3`, base
`d6999ddf` = master `783186b0` + the umbrella-verify merge).** The document above
was **not** rewritten: it was judged, its every claim re-checked against the tree
and the live ledger, and the check's results are recorded here and in the marked
sections. Three classes of result, in order of how much they cost the reader:

1. **38 of the document's 74 `file:line` citations no longer resolve.** They were
   never wrong — `git show 17cec86b:<file>` shows each pointing at the sentence it
   claims (three spot-checks recorded in the appendix) — they were **rotten by
   later edits**: 74 commits touched the ledger files between the prior pass's
   base `17cec86b` and master. `tasks.go:3408` (R2's immutability proof) is now
   `:3486`; `tasks.go:5709` (the queue's one ORDER BY) is now `:5840`;
   `task_reclaim.go:1393` (the residue refusal) is now `:1672`. Every citation
   the argument *rests* on has been re-anchored by **symbol name**, so the next
   reader can find the sentence even after the next 74 commits.
   **The general lesson, and it is the module's own subject matter**: a
   requirement document whose evidence is line numbers is a document with a
   half-life. Citable-by-symbol survives; citable-by-offset does not.
2. **The `R11` verdict is not merely a gap — it is a requirement that was
   fixed and then swept out.** See **F5**.
3. **R10 ("nothing is ever deleted") is false as measured — 756 row ids are
   missing from the live ledger, and two of this document's own fixture rows are
   among them.** See **N6/G14**.

Reconciliation of the two is deliberate and recorded here because the epic
briefing names the prior branch `docs/requirements-task-ledger` @ `5961f171`
explicitly; that branch is a **worktree-shared tip** (`git worktree list` shows
`/home/user/workspace/gopi-req-ledger`), so nothing was overwritten — this
branch reads it out with `git show` and completes it.

---

## 0. The operator's stated vision (the constraint every requirement must respect)

Nothing below is my inference. These are the operator's own constraints, as
delivered by the epic briefing (umbrella row `01a0ab12-2d2d-7c54-acb4-7c77e3024b69`,
quoted as constraints) and carried by his own ledger rows elsewhere in this
document. They are not goals of this module; they are the frame it must fit in,
and §6 checks each requirement against them.

- **C1 — one static binary, zero dependencies, no plugins.** Nothing may be
  added that needs an external service, a daemon of its own, or a runtime the
  operator must install. (Consistent with `GOPI.md` Rule 10.7 / Rule 22's
  zero-dependency posture.)
- **C2 — the operator is in control of one integrated thing.** One binary, one
  ledger file he owns, one place he looks. Not a federation of small services he
  has to keep in his head.
- **C3 — the daemon must stay simple.** Long-running machinery gets its logic
  from small, bounded, well-named pieces; complexity in a resident process is
  the failure mode that grows silently.
- **C4 — the agent cleans up after itself.** A finished run leaves no branch,
  worktree, process or half-open record behind. Hygiene is the system's job, not
  a chore for the operator.
- **C5 — small team, limited resources.** Cheap in attention and cheap in
  compute: the answer to a question the operator actually asks is worth an
  index, not a subsystem.

Read against this module: **C1/C5 are the reason the ledger lives in the same
SQLite file as everything else and has no service of its own; C2 is the reason
every question in §4a is a question for `gopi`, not for `sqlite3`; C3 is the
reason the status vocabulary is a data table rather than branches inside a
daemon; C4 is the reason a completion can be *refused* when the run's own
worktree still carries residue (`task_reclaim.go:1672`).**

---

## 1. What this module is for

*(product-owner language; no code identifiers by construction)*

When you have more ideas and more small jobs than attention, you need a place to
put them and a way to see what became of them. This module is that place: **a
single durable list of every piece of work you have handed to the machine, and
of every piece it did on its own initiative, with enough on each entry to answer
"what was I thinking when I filed this?", "is anyone doing it?", "did it
happen?", and "why not?" — without anyone having to remember or reconstruct it.**

It is deliberately two things at once, and this is the tension the rest of this
document is about:

1. **A queue** — things you hand over go in at one end, workers take them out,
   and the same item is never taken twice (including across your two machines).
2. **A memory** — the list is *your* record, not the machine's log. It is
   append-only: entries are never deleted, an overdue item is never silently
   dropped, and a wrong entry is corrected by filing a new one, so the trail of
   what you asked for survives every pass of the machinery.

## 2. Who is served

| actor | what it needs from the ledger | evidence |
|---|---|---|
| **The operator (nick)** | a to-do list he can trust: file an idea without ceremony, see what is in flight, see what nobody started, see what failed, and get back to an idea weeks later | `01a0ab3d-…` (§"What is a task row FOR?"); ledger rows below |
| **The worker (a gopi process)** | a *work order*: which single row it now owns, and a brief sufficient to do the work | `devdoc/task-dispatcher-p2.md` (cited in `internal/tools/tasks.go:45-48`, "the ledger was upgraded from a passive audit layer to a WORK QUEUE — pending is the enqueue state (task_submit / gopi task submit), claimed is the atomic-pickup CAS state (task next)"; the same file's `:1-8` header still calls the ledger "an AUDIT/TRAIL layer, not runtime control" — the two comments are the M1 mismatch in miniature), and the payload-required rule `bug028` (`t-1788053164`, *"task serve 的 refuse 路径 os.Exit(1) 杀掉整个守护进程：无 payload 行被领取→refuse→serve 死亡"*) |
| **The sync layer (two nodes)** | a *dedup key* and a total order: task id, status rank, anchor — so two machines running the same work is structurally impossible | `taskStatusRank`/`statusRankOf`, `internal/tools/tasks.go:2884-2920`; `t-1788036628`, `t-1788147782` |
| **The auditor (operator, or a later agent)** | a record: what failed, why, what evidence was left, what was already disposed and can be skipped | `t-1788036628` ("failed 保留供审计"); `01a09214-d6f4-74bc-b0f6-a68d7a117c4d`; `GOPI.md` Rule 5's report requirement |
| **The agent (Bond) as *submitter*** | a way to file a bug or a sub-task it cannot fix now, so it can continue its own work | `task_submit` tool; `GOPI.md` Rule 26 |

**Which user is primary?** The operator-as-memory-holder. Direct evidence that
the *queue* reading is primary exists (`internal/tools/tasks.go:8` calls the
ledger an "AUDIT/TRAIL layer, not runtime control"), but the queue's own
requirement — never run the same item twice — is a *safety property*, not the
purpose; the purpose is that the operator can find and account for his own
ideas. Five of the eight recorded design decisions (D3, D4, D6, D7, D8 below)
are about the operator's *record*, one (D1) is about cross-node safety, one (D2)
about what the reader sees, one (D5) about the worker's self-sufficiency.

## 3. The workflow it sits in

*(the operator's words; entry and exit conditions named)*

```
the operator has an idea or a complaint
        │
        ├── "gopi task submit \"<one line>\" --payload @brief.md"     ← ENTRY
        │        (row minted: task_id = the session id, created up front;
        │         summary + payload frozen here for ever)
        │
   ┌────┴─────────────────────────────────────────────┐
   ▼                                                  ▼
 a worker claims it                        it is never claimed
 (pending → claimed → in_progress)         (still pending, visible in the
   │                                        "nobody started this" view)
   │  the worker writes status + a reason + a result digest;
   │  the row's *content* is never rewritten
   ▼
 terminal: completed | failed | cancelled
   │
   ├── verified (an independent run judged the delivered artifact)
   ├── archived (the operator says "I have dealt with this failure")
   └── a follow-up row (reissue) when the answer was wrong
        │
        ▼                                                    ← EXIT
 the operator asks the ledger (list / show / search) and gets one of the
 four answers: in flight, not started, failed-and-needs-me, or "this is what
 I asked for and this is what came back"
```

Where the module sits: it is the *spine*. Every other module in this series
either writes a row (merger conflicts → module 02; workers → execution; the
agent → module 06) or reads one (the operator's cockpit → module 08). The
entry condition of a row is *a human or an agent deciding that a unit of work
exists*; its exit condition is *the row is terminal and the operator has
stopped needing to look at it* — which is a judgement, never a deletion.

## 4. Why it exists — the requirement per need, with evidence

Every claim cites a ledger task id, `file:line`, a commit SHA, or a GOPI.md
rule. `[opinion]` marks anything uncited. `[inferred]` marks a reconstruction
with its weakest link named in the same sentence.

### R1 — An idea can be filed in one line and must never need ceremony

**Need**: I am mid-thought; filing must cost one line. If filing is
expensive I will not file, and the idea is lost.

**Evidence**: `t-1788037892` — the operator's final ruling removes the
creation verb from the shell entirely: *"task new 彻底移除（nick 终审：shell 层
不接受此接口；唯一合法调用者=gopi 进程内部 taskregister）"*, i.e. creation happens
through the one queue-entry verb. The submit path mirrors the summary into the
payload when none is given — `cmd/agent/cli_tasks.go:567` (`payloadMirrored`:
*"an empty payload would be invisible to every dispatcher"*).
**Observable**: one line of input is enough to make a row that a worker can
claim.

### R2 — The brief must be self-sufficient, and frozen the moment it is filed

**Need**: the worker that picks this up may be a different process on a
different machine a week later. It must be able to do the work from the row
alone — and it must do the work I asked for, not the work the *machinery*
started describing.

**Evidence (self-sufficiency)**: `bug028` (`t-1788053164`) — a row with no
payload was claimed, the claim was refused, and the refusal killed the whole
serve daemon: a payload-less row is never dispatched (also the origin of the
queue-position suffix that marks an unclaimable row, `internal/tools/tasks.go:1792`).
**Evidence (immutability)**: `t-1788039140` — *"task update 收窄为 status-only
（nick 终审：payload/summary 全不可变——a job is just a job）"*. Implemented as a
hard refusal: `internal/tools/tasks.go:2932`, `:2960`, `:2991` (and `:3448`) (*"task content is
immutable — cancel and re-submit"*), the removal of every summary SET clause
(`internal/tools/tasks.go:3486`-`3505`, comment "bug056"), and the CLI help text at
`cmd/agent/cli_tasks.go:285`.
**Observable**: `gopi task update <id> --summary X` is refused (the flag no
longer exists); the row's summary still reads what I typed; a result digest
appears *next to it*, never over it.

### R3 — Status means "how far along", never "what happened to me"

**Need**: I must be able to tell at a glance whether something is waiting,
running, or finished — and across my two machines the same item must be in the
same place, without clocks having to agree.

**Evidence**: `t-1788039618` — *"claimed 降级为瞬时态（nick 终审：claim-and-run
原子化，无停留中间态）"*. The ladder is explicit and ranked:
`taskStatusRank` (`internal/tools/tasks.go:2889-2897`) —
`pending(0) → claimed(1) → in_progress(2) → terminal-tier(3) → verified(4)`,
with `failed`/`cancelled` *peers of `completed`* on rank 3 (*"后到覆盖"*), and
convergence by rank, never by timestamp (`syncDeltaApplyNewer`,
`internal/tools/memory_sync_delta.go:539` (`syncDeltaApplyNewer`), `:584`-`600`; the SQL re-derives both ranks
inside the UPDATE so a race cannot clobber).
**Observable**: a row never moves backwards; two nodes that disagree converge
to the further-along state; `gopi task list --status <s>` shows the rung I asked
for.

### R4 — "Why it ended" is a first-class, operator-visible field

**Need**: "done" is not an answer. I need the *reason* — and I need it to be
there even when the writer was lazy, because a blank reason is where an audit
trail dies.

**Evidence**: `t-1788470578` — *"task should have a reason field"* (the operator
filed this himself). Implemented with a per-status default so a lazy transition
still reads as intent: `taskDefaultReason` (`internal/tools/tasks.go:3382`, both comments)
"task finished" / "verification passed"), `TaskUpdateReason`
(`internal/tools/tasks.go:2987`), a same-status no-op never clobbering an existing
reason (`:3420`).
**Observable**: `gopi task show <id>` prints a reason line for a finished row;
`gopi task list --status failed` rows are not silently blank.

### R5 — The operator must be able to ask the ledger his own questions

**Need**: I think in questions, not in flags. The ledger has to answer them, or
I will go back to keeping a private list.

**Evidence**: the historical record *is* a sequence of these questions, filed by
the operator as ledger rows:

| his question (verbatim, from the row) | row |
|---|---|
| "in_progress task should show session id and model name" | `t-1788254165` |
| "task list should show in_progress-task starting excution time" | `t-1788044507` |
| "task show missing run_cmd which makes reproduce result difficult" | `01a095d9-1006-7142-88e4-95123fc84b46` |
| "task has no connection with branch" | `01a0a964-1139-7848-a4b5-37ec6b05d6b8` |
| "how to get taskid from branch/worktree" | `01a0a9f6-9bb5-7044-a1c8-ff3f1c752a36` |
| "in_progress tasks are all dead?" | `01a09fa7-5c37-7767-b691-138f4bacc4da` |
| "task resumes and status still failed?" | `01a09d29-4938-7b66-8cf5-1acf9fa2c264` |
| "why are so many merge task failed?" | `01a09c2d-bef3-7db3-9041-a9ad4b8a6a9a` |
| "task list return speed" | `01a09d2d-06e6-7c5c-976c-7c526e87e75d` |
| "how to prevent one task run concurrently" | `01a09c46-4d32-7cc4-aec6-b6767d46451f` |
| "when gopi finish task, it doesn't remove git worktree" | `01a091f9-4e2b-73e0-8b66-f5c96754796c` |
| "why three workers created when I only run once" | `01a09d0d-27ec-7191-80d2-237075f86a38` |

**Observable**: §4a below — each question checked against the real binary.

### R6 — One execution is one row; the row is born when the work is filed

**Need**: if one unit of work can appear as two entries, then every count I read
is wrong, and so is every "is anyone doing this?" answer.

**Evidence**: the operator's EPIC GOAL is row `t-1788562577` (verbatim:
*"消除 task run 双行冗余 — auto 镜像行只保留给'无工单执行'（裸 prompt/chat），
task run 执行信息全部写回 human 行"*), whose own problem statement measures the
cost: *"双份维护（关账×2/显示×2/同步×2/stale 检测×2）"*. Decision 7 (id shape):
`t-1788905574` — *"DB cleanup Phase 1: task_id generation → uuid + session-dir-created-at-submit"*.
Implemented: `internal/agent/taskregister.go:245-260` (the row is created through
the same extended INSERT face as a submit) and the comment at
`internal/tools/tasks.go:1758` (*"the [dup-of <wrapper>] marker is GONE"*).
**Observable**: running one filed item yields one in-flight row, not two.

### R7 — Cross-node: the ledger is ONE thing, and it converges forward

**Need**: I work on two machines. Something I finished on the laptop must show
as done on the server, and something the server already delivered must not be
overwritten by a stale echo from the laptop.

**Evidence**: `t-1788036628` (the operator states the problem himself:
*"HP 跑的任务状态只回写 HP 本地 memory.db，peerbox 台账永远 pending"*) and
`t-1788147782` (*"claim 即广播：TaskStart 成功后异步 db-push 到对端（准原子所有权——双节点平等竞争）"*).
`t-1788093118` records the protocol choice: *"[M4.13] task sync：timestamp-delta
双向同步（SyncML 核心简化版——nick 提议并确认实现）"*.
**Observable**: a task finished on the client appears terminal on the server
without a human copying anything.

### R8 — Two nodes may never run the same row twice

**Need**: duplicated execution is the one failure that destroys work (two
worktrees, two branches, two conflicting halves of one deliverable). It must be
impossible, not unlikely.

**Evidence**: `GOPI.md` Rule 28: *"Without the cancel both hosts claim it and
the work runs twice"*; the rank ladder (R3) plus the CAS claim
(`internal/tools/tasks.go:4588`/`:5446` (`SET claimed_by`); `memory_sync_claim.go:11` (the CAS phase comment)) is the
mechanism; the operator's rule after the `01a0aac4` → `01a0ab11` migration is
the *practice* — the cancel row carries the reason *"migrated to HP: … do NOT run
here (double-run guard). HP has the synced repo + branch."* (row
`01a0aac4-861f-74fa-880f-619330b0db25`, verified by `gopi task show`).
**Observable**: `gopi task run next` never returns an already-claimed row; the
worker loop and a manual run cannot both own it.

### R9 — A failed or abandoned row is evidence, not garbage

**Need**: the ideas that did not work out are exactly the ones I need to find
again. Losing one is worse than losing a success.

**Evidence**: `t-1788036628` in the operator's own words in the title:
*"失败墓碑归档语义（nick 同意：failed 保留供审计，archive/resolved 标记让 --status failed
只显示未处置失败）"*; and the sync rule implemented at
`internal/tools/memory_sync_task.go:1181`/`:1200` (the tag writes), where a cancel that meets
finished work loses the status but keeps the *intent* as a tag
(`syncTaskCancelIntentTag = "cancel-was-intent"`, declared `:1252`) under the
comment *"work is never dropped (the nick hard rule)"*.
**Observable**: an archived failure still answers `task show` / `task search`;
the default failed view shows only failures I have not yet dealt with.

### R10 — Nothing in this module is deleted, ever

**Need**: a record I can edit away is not a record.

**Evidence**: `internal/tools/tasks.go:5-6` — *"Nick's iron rule: the ledger is
append-only — records may be created, summaries edited and status changed, but
NEVER deleted. There is deliberately no delete path anywhere in this file."*
Pinned by a test that reads the source and fails if a delete verb appears:
`internal/tools/tasks_fts_test.go:136` `TestTaskNoDeleteFuncRegression` — whose
own docstring scopes it honestly: *"the append-only guard at the API-surface
level"*, i.e. no entry point, not no missing row.
**Measured 2026-09-18T11:01Z: 756 row ids are absent from the live table**
(`max(id) − count(*)`), and 93 `task_events` rows point at task ids that no
longer exist. The rule holds for the code and not for the record — see **F6/N6**,
which is the one place in this document where a requirement the operator stated
verbatim is contradicted by the ledger itself rather than by a code path.
Reissue replaces reopen: *"rework is REISSUED as a NEW pending row, append-only"*
(`internal/tools/tasks.go:2845`), delivered to the operator as
`gopi task reopen <id> --reason "why it failed"` (`cmd/agent/cli_tasks.go:441`)
— and `taskStatusReopened` participates in no transition precisely so that the
*old* row cannot be made to look unfinished again.
**Observable**: there is no verb that removes a row; rework produces a new row
and leaves the old one readable.

### R11 — "Finished" and "delivered" must not be confusable

**Need**: if the ledger says done while the work never left my laptop, the
ledger is lying and I will stop trusting it. I need the row, the branch and the
peer's copy to agree.

**Evidence**: the incident chain — `01a0aab1-587a-7ca3-ba98-4b66b11b8b90`
("client push must pre-merge server master and verify buildable BEFORE pushing
the branch") and `01a0aab8-ea14-7ad6-88ed-e0012f2a5555` (its absorbed follower),
against `briefings/brief-hp-push-diagnosis.md` (three completed rows
`01a0a97e`/`01a0aa74`/`01a0aa0e`, branches 19/3/5 commits ahead of the peer,
never pushed). The design decision is recorded in `GOPI.md` Rule 12's close-loop
(master moves → binary is rebuilt) and in the `sync-resolved` semantics below.
**Observable**: after a run ends, the peer has the branch; if the push failed,
the row does not read as a clean success.
**Corrected this pass (F5)**: the code DID serve it — `6e22d80b` moved the push
ahead of the worktree-dirty guard and added the delivery-observability file — and
a later sweep (`085811f8`) deleted that file and reverted the ordering. The
swept code is still on master's own ancestry (both commits are ancestors of
master) but no longer in master's **tree** — it survives as history only. R11's status is
therefore **implemented-then-destroyed**, not "never served", and the correct next
action is recovery, not design.

### R12 — The ledger must be able to tell me the truth about liveness, not just about state

**Need**: "in progress" is a claim by a process that may be dead. I need to know
whether *anyone is really there*, in a way I can act on.

**Evidence**: `01a09fa7-5c37-7767-b691-138f4bacc4da` ("in_progress tasks are all
dead?"), `01a09435-9ffe-7998-a6c0-da485f7571dd` ("task list 显示层诚实性：
'在跑'/'干完未关账'/'死了'三种状态不可区分（都显示 [running Xh]）"),
`01a09431-fca8-7806-9894-ff2010b75821` ("worker 正常退出却写不回台账（7 行僵尸：干完了
但停在 in_progress，成果已进 master）"). Mechanism: the display layer reinterprets
existing signals into one honest label (`internal/tools/taskstate.go:3`), and
the process-identity triple (pid + start ticks + cmdline) answers by measurement
(`taskproc.go`; `gopi task status <id>` prints running/dead/recycled/foreign/
no-holder/recycled, `cmd/agent/cli_tasks.go:249`-`256` (the help text; `task status <id>` at `:256`)).
**Observable**: `gopi task list --status in_progress` distinguishes a live run
from a finished-but-unrecorded one from a dead one — and `gopi task status <id>`
gives a verdict with an exit code.

### R13 — The ledger's own vocabulary must be able to grow without lying about the past

**Need**: when the model changes (a status is retired, an id shape changes), old
rows must keep reading correctly. The record outlives the code.

**Evidence**: `taskStatusReopened` is kept in the CHECK set but participates in
no transition (`internal/tools/tasks.go:66`-`81`, `:97`); legacy `t-<unix>` ids keep their
rendering while new rows mint uuid v7 (`tasks.go:1648` (`taskHashDisplay`), `t-1788905574`); and
the migration is idempotent and self-healing rather than scripted
(`tasksModelColumn` `:157`, `tasksInProgressAtColumnName`; `tasks.go:143`-`215`).
**Observable**: an old row still opens, lists and syncs; a rename of a status
value is a one-line change plus a legacy-reader clause.

### 4a. The operator's questions, each checked against the current binary

Checked by running `/home/user/work/GoPi/bin/gopi` against the live ledger
`~/.gopi/memory.db`.

**RE-MEASURED 2026-09-18T11:01Z** — master `783186b0` (this branch `d6999ddf`),
binary `/home/user/work/GoPi/bin/gopi` (built 2026-09-18 18:51 local), ledger
`~/.gopi/memory.db` **110,526,464 bytes, 1907 rows / 3394 events**. Every count
in this section, in §6 and in F1–F6 is from this reading; the prior pass's
numbers (2421 rows / 360 events) are quoted only where the *difference* is the
point. **The ledger did not grow into these numbers — it shrank into them**: 756
row ids are absent from a table whose iron rule says nothing is ever deleted,
two of them this document's own fixture rows. That is **F6/N6** below, and it
invalidates the prior pass's reading of R10, corrected there.

| his question, in his words | can the CLI answer? | with what |
|---|---|---|
| **"what is running right now?"** | **yes** | `gopi task list --status in_progress` — **5 rows** (re-measured 2026-09-18T11:01Z), each labelled honestly: `[running <dur> since <t>]` for a verified-live holder, `[unknown: no claim stamp, …]` for a row born in_progress |
| **"is that one actually alive, or dead?"** | **yes** | `gopi task status <id>` → verdict + exit code (0 resumable / 1 running / 3 unverifiable); measured: `01a0ab1c…` → `verdict: no-holder`, `resumable: yes` |
| **"what did I ask for that nobody did?"** | **yes** | `gopi task list --status pending` — **31 rows** (re-measured 2026-09-18T11:01Z), the CLI prints its rendered count; the claimable one is marked `[next]`, a row no dispatcher can claim is marked `[queue-skip]` |
| **"what failed and needs me?"** | **yes** | `gopi task list --status failed` → **90 of 155** rows (re-measured 2026-09-18T11:01Z: 65 of the 155 carry the `resolved` token and are hidden; `--all` shows all 155) |
| **"did the thing I filed actually get verified?"** | **yes** | `gopi task list --status verified` (68) and `gopi task verify <id>` to *produce* a verdict |
| **"what is this row?"** | **yes** | `gopi task show <id>` — summary, status, session log path, raw log path, model, payload, tags, claim trail, timestamps, latest result digest |
| **"which row owns this branch?"** | **yes** | `gopi task for-branch <branch> [--repo R] [--json]`, with a distinct exit code 4 for "no row names this branch" (never a blank success) |
| **"what did I ask for last Tuesday?"** | **NO** | no time filter: `gopi task list --since …` → `unknown flag --since`; `--source` likewise. Workaround: `gopi task search <keyword>` (FTS over summary/payload/tags) or `task show` by id. This is a real gap — §6-G3 |
| **"what did *I* decide / what did the operator say?"** | **partly, and by accident** | there is no `--source human` filter, but `gopi task show` prints `submitted_by: human (gopi task submit CLI)`; searching the operator's own words is a `search` on prose, not a field query |
| **"why did all of these fail?"** | **NO (as a query)** | `list --status failed` shows summaries only; the reasons are one `task show` per row |
| **"what happened to this row, step by step?"** | **NO** | the `task_events` trail (`task_events`, **3394 rows** measured 2026-09-18T11:01Z) is written but has no operator verb — `gopi task events <id>` → `unknown subcommand "events"` (measured); only `task show` renders the single latest event as `result:` |
| **"what did I ask for that was never started and then cancelled?"** | **no direct answer** | `--status cancelled` shows **271 rows** (re-measured 2026-09-18T11:01Z) with no reason column in the list view; the reason exists in the row but `list` does not render it |
| **"the answer was wrong — put it back in the queue with the reason"** | **yes** | `gopi task reopen <id> --reason "…"` — the rework is re-enqueued as a NEW pending row; the judged row keeps its verdict (`cli_tasks.go:4223-4227`, `:4307`) |
| **"what does my ledger look like as a whole — what patterns is it telling me?"** | **yes, and it is the module's most under-marketed verb** | `gopi task improvements` — an agency scan: measured today *"4181 row(s) examined, 9 candidate pattern(s)"* with a task-family size histogram (`1:960 2:68 3:23 …`) and, per pattern, the rows that would be enqueued; `gopi task dedup-scan [--live]` answers the duplicate question directly |
| **"how big is this ledger getting / how slow will it get?"** | **partly** | `gopi task list` prints the post-filter header (`1838 task(s):` measured 2026-09-18T11:01Z; `--all` → `1908` — 1 fewer than the table's 1907 because a row is created by the read itself) with no hint of how many were hidden, and no size/duration report; the operator filed `01a09d2d-…` ("task list return speed") |

**The single most-used question the CLI cannot answer is a time-bounded one.**
The ledger is the operator's memory, and memory is queried by *when*.

## 5. Acceptance in user terms

The operator considers this module working when, without opening the database
and without asking anyone:

1. **Filing costs one line.** I type one line; a row exists; a worker can do the
   job from that row alone. (R1, R2)
2. **What I typed is what the row still says.** Nothing — not a writeback, not a
   sync, not an upgrade — has rewritten my words. Anything added sits beside
   them. (R2, R10)
3. **"Nobody has started this" is a list I can act on.** Every row in it is
   either claimable right now or explicitly marked as one no dispatcher can
   take, with the reason. (R5, §4a)
4. **"In flight" is honest.** For each such row I can tell apart (a) a live run,
   (b) work that finished and only the record was left open, (c) a run that
   died — and I get a verdict, not a hint. (R12)
5. **A finished row tells me why, in one line, without me reading logs.** And if
   I did not write the reason, the machine still wrote a true coarse one rather
   than leaving it blank. (R4)
6. **"Done" is not a claim I have to double-check on the peer.** When a row says
   the machine's job is over, the artifact has left the machine that made it (or
   the row says it did not). (R11)
7. **A failure I have dealt with leaves my working view but stays retrievable.**
   I mark it dealt-with; the default failed list shrinks; nothing is gone. (R9)
8. **The same unit of work is one row, on both machines, at one place in the
   order** — and when both machines disagree, they converge to the further-along
   state without me arbitrating clocks. (R3, R6, R7, R8)
9. **An old row still reads correctly after the model changes.** (R13)

**A test author reading only the above should be able to separate bug from
allowed behaviour on at least these:** "a row's summary changed" = bug (2);
"a dirty worktree made the whole close-out skip, so the push never happened" =
bug (6) — the *harmless* step must run and only the *destructive* step defers;
"a pending row vanished" = bug (3); "an in_progress row whose process is gone
still prints `[running 3h]`" = bug (4).

## 6. Gap analysis

### Serves (evidence, not assertion)

- **Append-only in the CODE, and only there.** No delete path exists and a
  source-reading test forbids one (`tasks.go:5-6`; `tasks_fts_test.go:136`, whose
  docstring says "at the API-surface level"). **Corrected this pass: R10 is a
  policy, not an enforced property** — 756 row ids have left the live table (F6),
  by a route no code implements and no test observes. The guard does exactly what
  it says it does; it does not do what the R10 requirement needs.
- **Immutability, structurally.** There is no summary SET clause on the update
  path (`tasks.go:3486`-`3493` — bug056's comment: *"summary is IMMUTABLE — there is
  NO summary SET clause here anymore"*); the result digest goes to the event
  trail (`tasks.go:2683`, `TaskUpdateWithResult` `:2953`). R2 holds at the writer.
- **Forward-only convergence.** The rank ladder is data, and the guard is
  re-derived inside the UPDATE so a race cannot clobber
  (`memory_sync_delta.go:812`-`940`). R3/R7 hold even under concurrency.
- **The `failed` view the operator asked for works**, and is measurable:
  `--status failed` → 90 rows (measured 2026-09-18T11:01Z); the 65 rows carrying
  the exact `resolved` token are hidden, and `--all` shows all 126. Across all
  statuses the default list hides **70** of 1907 rows (1907 → `1838 task(s)`),
  i.e. the `resolved` token, not the status, is what the filter keys on.)
  (`tasks_archive_test.go:72` `TestFailedViewHidesArchived`.) R9 holds.
- **The delicate case is handled well, not by accident.** A cancel that meets
  finished work *loses the status but keeps the intent* as provenance
  (`memory_sync_task.go:1181`-`1252`), which is precisely R9's "an idea is never
  silently lost".

### Falls short (with evidence)

**G1 — `completed` means "the run stopped", and the operator reads it as "the
work landed". Three incidents prove the reader's expectation is not the code's.**

Evidence: the decision is on record and *deliberate*:
*"completed 语义本来就是简化设计（status 纯序只服务跨节点 sync 防双跑；真实结果
cancelled/failed/crash 由 reason 承载；不想 reopen——重发新任务=简单解 append-only）"*
(quoted in module brief `01a0ab3d-1e94-…`, decision 4; the same text is the body
of this epic's own row `01a0ab11-69c4-7392-806d-678a7bbad2b4`). Measured
2026-09-18T11:01Z: of **1780** terminal rows, **529 `completed` rows carry an
empty reason and 782 carry exactly the default `"task finished"`** — i.e. for
1311 of the 1376 `completed` rows the row itself contains *no* information about
whether anything landed.
`internal/tools/tasks.go:2854-2857` renders the terminal tier as one rank
(*"completed/failed/cancelled are peers (后到覆盖 — the later write wins a tie)"*,
`taskStatusRank` at `:2889-2897`).
**Verdict**: not a bug in the code — a requirement gap. Nobody decided whether
the row is a *queue ticket* (stop-ordered) or a *delivery receipt* (landed). The
three incidents below all read it as a receipt.

**G2 — There is no operator verb over the row's history.** `task_events` holds
every transition and every digest (360 rows measured; written at
`tasks.go:308`-`316` (`taskEventWrite`); read back only as "latest" at `tasks.go:319`-`321` (`taskEventLatest`), or as an
existence test at `:331-343`). The CLI surfaces one line of it in `task show`
(`result:`) and nothing else; `gopi task events <id>` → `unknown subcommand
"events"` (measured); `task verdicts` reads a *different* trail (the reframe
verdicts) and only counts them. R4's "why" is
therefore *latest-state*, not *history*: after two transitions the operator can
no longer see the first reason. "What happened to this row, step by step?" is
unanswerable from the CLI (§4a).

**G3 — The memory has no time axis.** `gopi task list --since X` and
`--source human` both fail with `unknown flag` (measured). The operator's own
questions are time-bounded ("what did I ask for last Tuesday?"), and the answers
exist in the table (`created_at`, `updated_at`, `ended_at`) with no verb to ask
them. R5 is met for *state* questions and unmet for *time* questions.

**G4 — The queue order is fixed at submit time and cannot be corrected
afterwards; the row that specified the order is itself `cancelled`.** Priority is
delivered (`taskClaimOrderBy`, `internal/tools/tasks.go:5840` — *the single
ORDER BY that decides the queue*: `COALESCE(priority, 5) ASC, created_at ASC,
id ASC`, with `taskTagPriority` deriving bug=1 / docs=8 / else 5 at
`tasks.go:5907` (`taskTagPriority`), overridable by the `--priority` flag on submit). But
`task update` is status-only (R2's immutability ruling), so a row's weight can
never be corrected after filing — the only lever is a tag chosen at submit time.
The row that specified the queue order, `t-1788087991` (*"队列顺序锁死+优先级：
TaskNextFiltered 显式 FIFO（ORDER BY ASC）+ priority 列（submitter 可发优先级）"*),
is **`cancelled`** — its `fifo` tag survives, its explicit-FIFO intent did not
(the shipped order is priority-first). With 31 pending rows (re-measured
2026-09-18T11:01Z), "the bug I filed
five minutes ago waits behind everything older that is also a bug" is the
ordinary outcome, and no requirement covers how the operator re-prioritises
without filing a new row. **Never decided** — not a served need and not a
violation.

**G5 — A row can be born `in_progress` with no claim stamp, and the ledger has no
opinion about it.** On the prior pass 5 of 8 `in_progress` rows had
`claimed_by` empty (`01a0aaab-189f-…`, `01a0aaab-370a-…`, `01a0ab04-e646-…`,
`01a0ab18-c511-…`, `01a0ab1c-e5c1-…`); the honest verdict verb calls this
`no-holder` ("a fake in_progress") and each is `resumable: yes`. A resident sweep
does exist — `gopi task sweep-zombies [--kill]` (`cli_tasks.go:344`), and the
query-side sweep fires at `task list` time under a throttle (`cli_tasks.go:167`-`183`, `taskListSweepThrottleMin`) — but its
disposal leg acts **only on rows it can confirm dead** (`zombieVerdict`,
`taskzombie.go:235` (`zombieVerdict`): *"heartbeat stop + claimed pid dead = confirmed"*), and
a row with no claim stamp at all lands in the `default:` arm — *"suspected"* —
which `--kill` deliberately refuses (*"Suspected-only rows are never killed by
an automated sweep"*, `taskzombie_sweep.go:259`). So the shape with the
*least* evidence is the one the machinery can never clean, and the operator must
notice. The merge-conflict rows (`01a0aaab-…`, born `in_progress` by design so
the worker pool structurally cannot dispatch them, `repo_resolve_row.go:71`-`117`)
are the *reason* the shape exists — right for the merger's tracker row, wrong for
a row that claims to be a worker run. **Falls short**: R12's honesty is delivered
to the *reader* but not to the *record*.

**G6 — `verified` is a rank above the terminal tier, so a verifier can overwrite
the outcome the operator recorded.** `taskStatusVerified = 4` outranks
completed/failed/cancelled (rank 3) and `failed → verified` is a legal local
transition (`tasks.go:2856`-`2885`, `taskAllowedTransitions`). R3's "terminal states are peers" and this are
in tension: the model has *two* kinds of finality and the higher one is not the
operator's. The operator-visible consequence: a `failed` row that a verifier
accepts stops being a failure without any human saying so — the same *shape* as
incident 2, in the local path rather than the sync path. **Never decided**
explicitly; the recorded rationale is only about rework economics
(`devdoc/task-verify-loop.md`, cited at `tasks.go:60-67`).

**G7 — R2's live violation is the chat backfill's summary append.** The sync
harvest path appends `" — chat: backfilled"` to a row's **summary**
(`internal/tools/chat.go:425` `chatBackfillMark`, written at `:501` as
`UPDATE tasks SET summary = summary || ?`) — a direct breach of "content is
immutable", pinned as correct behaviour by `TestChatBackfillMigratesLegacyAutoRows`
(`chat_test.go:147`). The *other* mechanism built for the same need is
**dead code that would have breached R2 the same way**: the merge staleness guard
defines a *summary* marker (`MergeVerifyStaleNoOpMarker =
"[merge-verify staleness guard]"`, `task_staleness_guard.go:326`) and
`MergeVerifyStaleNoOpMarkedSummary(originalSummary)` which prepends it to the
summary (`:986`) — that function has **no caller anywhere in the tree** (grep
over every `.go` file returns only its definition and its doc comment). Its
*live* sibling is the rework sweep (`MergeVerifyStaleReworkNoOpSummary`, `:1182`,
called at `tasks.go:4515`), which closes rows as `cancelled` with the verdict in
the *reason* + event — the R2-correct shape. So the module has one R2-violating
writer that runs and one that was orphaned before it could. **Over-build + drift.**

**G8 — The ledger cannot represent "a person is waiting on this".** Nothing in
the status set, the tags, or the verbs records *operator attention* as a first
class fact (no `blocked`, no `needs-me`, no `waiting`). Today 90 rows in the
default failed view and 31 pending (2026-09-18T11:01Z) are addressed to a human's attention and are
distinguished only by status. The operator's own question *"is this dead?"*
(`01a09fa7-…`) is the symptom of a missing state, not a missing display.

**G9 — Five packages write rows with no single writer boundary**; read
independently, the ledger's only documented invariant about *who may write what*
is prose in a comment (`tasks.go:5`-`8`) plus per-path refusals. R2 and R10 are
enforced at three sites (`taskUpdate`'s refusal, the FTS-trigger migration, the
no-delete test) and violated at others (G7's summary marker; `chat.go:501`'s
`summary = summary || ?` backfill mark; `memory_sync_task.go:1875`-`2036`'s
`harvestInsert` and `:728`-`1148`'s `importTaskFresh`, both of which carry a remote
row's `payload` through an INSERT/`TaskSubmit` with no immutability check — the
sync layers treat `payload` as a *transport field*, while R2 treats it as
*frozen content*). **Never decided**: the module has no writer contract.

**G10 — Test scaffolding lives in the operator's production ledger and, under
R10, can never leave.** Six acceptance-fixture rows were written into
`~/.gopi/memory.db`: `t-live-accept` ("live acceptance row", `failed` — it is one
of the 90 rows in the operator's default failed view), `t-live-decoy` ("decoy
row", `pending`), `t-recon-guard-demo` / `t-recon-guard-demo2` ("anchor guard
live demo row"), `t-zd-confirmed` ("zombie dispose t-zd-confirmed") and
`t-report-legacy` ("legacy row probe"). **Corrected 2026-09-18T11:01Z (F6):
four of the six survive, two are GONE** — `t-recon-guard-demo` and
`t-recon-guard-demo2` are absent from the table, and neither the module's iron
rule nor `TestTaskNoDeleteFuncRegression` noticed. Of the survivors
`t-live-accept` is `failed` (one of the 90 rows in the operator's default failed
view), `t-live-decoy` and `t-zd-confirmed` now read `completed`, and
`t-report-legacy` is `cancelled` with the `resolved` tag. R10 says they cannot be
removed, only archived — **the record says two of them were removed.** The
module's own iron rule conflicts with its test practice *and* with its own
record.

**G11 — The listed `#` handle is ambiguous and not resolvable.** `list` renders
the first 8 characters of a uuid row as the `#` handle
(`taskHashDisplay`, `tasks.go:1648`) while `task show` requires the full id
(`gopi task show 01a0ab11` → `no task with task_id="01a0ab11"`). Measured on
this ledger: **147 uuid rows share an 8-char prefix with at least one other row**
(re-measured 2026-09-18T11:01Z; the prior pass read 317 on the pre-purge table)
(worst: `01a0ab12` → 16 rows, `01a0ab3e` → 13 — the two requirement series). The
handle a human would copy is exactly the one the tool refuses. **Falls short of
R5.**

### Never decided (the requirement itself is unclear or absent)

These are gaps that are neither a violation nor an over-build: the code does
something reasonable and **nobody ever wrote down what it should be**. They are
the findings most likely to be resolved by a decision rather than a patch.

- **N1 (=G1) — whether a row is a queue ticket or a delivery receipt.** The
  terminal vocabulary cannot say "the run stopped" and "the work landed"
  separately (1311 of 1376 `completed` rows carry no evidence either way,
  re-measured 2026-09-18T11:01Z). Not
  decided; three incidents read it as a receipt.
- **N2 (=G4) — how the operator re-prioritises a filed row.** The queue order is
  fixed at submit time (`internal/tools/tasks.go:5840`) and `task update` is
  status-only, so the only lever is a tag chosen before filing. With 31 pending
  rows and no verb to reorder, the intent for a queue of this size is unrecorded;
  the row that specified the order (`t-1788087991`) is itself `cancelled`.
- **N3 (=G6) — whether a verifier may outrank the operator's recorded outcome.**
  `verified` sits at rank 4, above the operator's terminal tier at rank 3, and
  `failed → verified` is a legal local move (`tasks.go:2856`-`2885`). Two kinds of
  finality exist; nobody decided that the machine's is the higher one.
- **N4 (=G8) — whether "a person is waiting on this" is a state the ledger owes
  the operator.** No status, tag or verb carries operator attention. 61 + 82 rows
  are addressed to human attention and are distinguished only by status.
- **N6 (=F6) — whether a row may ever leave the table, and if so under whose
  authority.** R10 states "never". The record says 756 rows left, by a route no
  code implements and no test observes; the one documented instance
  (`devdoc/legacy-t-purge-20260910.md`, 751 rows) was a hand-run purge with an
  operator review and a backup. Nobody has decided whether that is the *sanctioned
  exception* to the iron rule or a violation of it — and the answer changes
  whether `TestTaskNoDeleteFuncRegression` should be widened to the data, and
  whether `task_events` orphans are acceptable. **This is the module's most
  consequential undecided question after G1.**
- **N5 (=G9) — who may write which field, and what "frozen content" means to a
  transport layer.** Five packages write rows; the only stated invariant is prose
  (`tasks.go:5`-`8`) plus per-path refusals, and the sync paths treat `payload` as a
  transport field while R2 treats it as frozen content.

### Over-builds

- **G12 — `reopened` is dead vocabulary kept alive for reading.** It stays in the
  status set that renders the SQL CHECK constraint (`tasks.go:66`-`81`) and in the
  CLI's rendered status filter list (`cmd/agent/cli_tasks.go:441` help head, `:210`, *"plus the
  retired \"reopened\" for legacy rows"*) while participating in no transition
  (`tasks.go:2871`, `:3476`: *"reopened is retired — no transition targets it"*).
  Justified by R13, but it costs the operator a status name he can select and
  never see. Small, deliberate, and worth naming as such.
- **G13 — two status ladders exist for the same set of values**: the local
  transition table (`tasks.go:2856`-`2885`) and the sync rank map
  (`tasks.go:2889`-`2897`). They encode the same intent in two shapes and must be
  kept consistent by hand; `reopened` already differs between them (in the table
  it has no key at all; in the rank map it is `0`).

### The C1–C5 check (the operator's vision, as a constraint on each requirement)

| constraint | verdict for this module |
|---|---|
| **C1** one static binary, zero deps, no plugins | **held.** The ledger is one SQLite file in the same `memory.db` as everything else; the only direct dependency is `modernc.org/sqlite` (pure Go). No requirement above needs a plugin. |
| **C2** the operator controls the whole thing | **held overall, weakened by G2/G3.** Everything is one file he owns and can open by hand; but the two questions he actually asks (history, time) are only answerable by leaving the CLI. |
| **C3** a daemon must be simple | **held.** The ledger's logic lives in the *writer paths*, not in a daemon; the sweeps are bounded and switchable. However G13's two ladders are exactly "two branches of logic" multiplied across the local and sync faces. |
| **C4** the agent must clean up after itself | **held, with a new edge.** `reclaimRunResidueRefusal` (`task_reclaim.go:1672`) lets a completion be *refused* when the row's own worktree still carries residue (`tasks.go:3425`) — the record now enforces the hygiene instead of tolerating it. Not raised before; recorded as served. |
| **C5** small team, limited resources | **held.** 1907 rows in a 110 MB file, `list` returns instantly; the ledger costs no resident process. G2/G3 are the cheap half of the answer (an index and a flag), not a new subsystem. |

### The finding the operator does not already know

**F1 — His own iron rule ("no delete, ever") is what makes his ledger
uncleanable, and his hygiene verb is fighting a losing battle against it.**

Measured 2026-09-18T11:01Z: `--status failed` hides 65 rows because they carry
`resolved`; 155 − 65 = 90 remain. But the *same* append-only rule that protects
his record (`tasks.go:5`-`6`, pinned by `TestTaskNoDeleteFuncRegression`) also
guarantees the ledger only ever grows: **1907 rows, of which 273 are `cancelled`
and 159 of those are `source=auto`** — see F6: the premise is no longer true — machine-registered rows that no human ever
decided anything about. `gopi task list` has already had to grow a filter
(`FilterArchivedRows`, `cmd/agent/cli_tasks.go:4817`) and a marker
(`[resolved]`) purely to keep the operator's own view readable, and a dedicated
hygiene task (`01a09214-d6f4-74bc-b0f6-a68d7a117c4d`) had to be filed to do by
hand what the machinery cannot do at all — **and, per F6, was later done by
hand with a raw `DELETE`, which is how 756 row ids left the table.**

The consequence he has not stated: **because nothing may be deleted, every future
ledger question has to be answered by a *filter*, and every filter must be
remembered by every future reader.** The 608 `source=human` rows (536 after the
72 `gopi run` telemetry rows, re-measured 2026-09-18T11:01Z) are his record; the
other 1299 rows — 852 `auto` plus 447 `llm` — are the machinery's. He has one list serving two populations,
with different lifetimes and no boundary between them. The cheap requirement
this implies — *"the operator's record and the machine's telemetry must be
separable by default"* — is not written anywhere, and today the only separator
is the undocumented convention that auto rows carry `[auto]` in their summary
text (**624** rows so prefixed, re-measured 2026-09-18T11:01Z).

**F2 (smaller, mechanical, immediate, and it changes the mechanism the ledger
already trusts) — the row handle the CLI prints is the row handle the CLI cannot
resolve.** Measured 2026-09-18T11:01Z: the worst shared prefix is `t-178830`
(32 rows), and among uuid rows **147 share their printed 8-char handle with at
least one other row** (the prior pass read 317 on the pre-purge ledger);
`task show`/`task status` accept only the full uuid (G11). Re-measured:
`gopi task show 01a0ab12` → `no task with task_id="01a0ab12"`, while the
requirements series' own prefix (`01a0ab12`) is shared by 16 rows. Any operator or agent that copies the visible handle gets a
confident `no task with task_id` for a row that is plainly on screen.

**F4 (new in the prior pass, and it strengthens F1's cheap requirement) — the
`source` column is already a complete, reliable separator; the convention the
code actually uses is a *retired* text prefix.** Measured 2026-09-18T11:01Z: all
1907 rows carry a non-empty `source` (`human` 608 / `auto` 852 / `llm` 447 —
zero NULL, zero empty), so *"the operator's record and the machine's telemetry
must be separable by default"* needs **no schema change and no new column** — one
`WHERE source='human'` predicate, which is exactly the flag §4a measures as
missing (`--source` → `unknown flag`). Meanwhile the convention the ledger
actually relies on for the same job is a **summary-text prefix the code declares
RETIRED**: `internal/tools/chat.go:16` states *"The [auto]-shaped chat rows in
tasks are RETIRED"*, and the prefix itself is pinned as *legacy* at
`chat.go:427`-`430`/`:449` (`chatLegacyAutoSummaryPrefix`). So the separator
that exists (a data column, populated for every row) is unused, and the one in
use (substring matching on prose) is a retired legacy marker — 624 rows happen to
carry it (re-measured 2026-09-18T11:01Z). The operator has not been told that the answer to "which of these are
mine" is a column away.

**F5 (new in this pass, and it reverses a verdict above) — the R11 fix was
landed, proved, and then SWEPT OUT; the deliverable-before-dirty-gate property is
back to absent, on the diff that reintroduced the original incident's hazard.**
The prior pass's R11 row reads "the code did not serve it". That is not the whole
story, and the correction matters because it changes what the next agent should
do:

- `6e22d80b` (*"fix(closeout): deliver the branch before the dirty gate; make a
  stranded delivery observable"*, 2026-09-17 00:16) is the operator's own
  complaint `01a0aab8-ea14-7ad6-88ed-e0012f2a5555` ("client push must not be
  gated by the worktree dirt") implemented. Its message states the defect
  exactly: *"the client close-out chain applied the worktree-dirty guard to the
  WHOLE sequence, so one untracked .evid/ scratch file skipped the push too and
  stranded a finished branch on the client host with the ledger already saying
  completed."* It added the reordering **and** a new observability file
  (`internal/tools/task_finish_delivery.go`, 330 lines, plus
  `task_finish_delivery_test.go` / `_observe_test.go`, 430 lines of tests) and
  its own notify event.
- `085811f8` (*"sweep: back out feat/client-closeout-delivery (its conflicted
  shared files were resolved master-side, half-applying it: 6 merge-induced
  reds)"*) **deleted that file** and reverted the reordering. It was itself
  reverted once (`b627a081 Revert "Merge branch 'fix/merge-backlog-terminal-guard'"`
  is the neighbouring episode), then the sweep landed for good: measured now,
  `internal/tools/task_finish_delivery.go` **does not exist at HEAD**, no caller
  of `TaskFinishDelivery` exists anywhere (grep), and the dirty gate is again
  applied to the **whole** chain — `internal/tools/task_finish_cleanup.go:1145`
  (the client arm `taskFinishCleanupChain`, `:1101`) and `:2164` (the injected
  target seam `runFinishCleanupChainTarget`, `:2100`) both `return` BEFORE the
  push at `:2232`. I re-read both call sites at their current lines to confirm
  the order, and the operator's row `01a0aab8` reads `completed` with reason
  `"task finished"` — the fix's own ticket shows the same "completed, no evidence"
  shape the fix existed to remove.
- The requirement therefore has a **known-good implementation, with tests, in
  master's own history** (`6e22d80b` is an ancestor of master; the *tree* entry
  was removed by `085811f8`, so the fix is recoverable with
  `git show 6e22d80b:internal/tools/task_finish_delivery.go` and its two test
  files — no lost branch required), and no row states that the capability was
  deliberately retired. Either the reorder returns (with the delivery tests) or
  the operator's `01a0aab8` must be recorded as *withdrawn* — today it is
  recorded as `completed`.

**Weakest link**: I could not find a ledger row that says the reorder was
*intended* to be dropped. The sweep message says why the merge was backed out
(its shared files had been half-applied master-side, 6 reds); "back out the
half-application" and "retire the capability" are different decisions and only
the first is on record. `[inferred: the retirement was tactical, not a decision]`.

**F6 (new in this pass — the single largest deviation from the stated model) —
756 row ids are missing from a ledger whose iron rule is "NEVER deleted", 93
`task_events` rows are therefore orphans pointing at tasks that no longer exist,
and the document above has been reading the ledger as if that were not so.**

Measured 2026-09-18T11:01Z on `/home/user/.gopi/memory.db` (110,526,464 bytes): `SELECT
max(id) - count(*) FROM tasks` = **756**; `task_events` holds **3394** rows
covering **1290 distinct task ids**, of which **93 ids have no row in `tasks` at
all** (71.7 % of the ledger's own audit trail belongs to rows that are gone).

Three independent measurements say this is a **purge, not history**, and they
agree on the count:

1. **The gap set is inherited, not created.** Comparing rowid gaps against the
   in-tree backups: the innermost snapshot carries 1003 rows with **754 gaps**,
   the live ledger has 756, and **all but 2 of the live gaps are already present
   in that older file** — i.e. the rows were removed *before* 2026-09-11 and the
   deletion predates the backups that survive. (The live-only 2 are the two
   fixture rows below.)
2. **The pre-purge snapshot differs by exactly the count.** Two earlier backups —
   `memory.db.bak-pre-legacy-purge-20260910-095008` (1614 rows) and
   `memory-glm-audit-20260909-184030.db` (1602 + 3 gaps) — have **essentially the
   documented 751-row purge** (below) as their difference to the live table.
3. **No surviving tooling can do this.** Grepping the whole tree for
   `DELETE FROM tasks` / `DROP TABLE tasks` returns **only test files**
   (`internal/tools/tasks_repo_guard_test.go:42`,
   `internal/agent/cov_agent_cr_test.go:124`,
   `internal/agent/taskregister_test.go:229` — which asserts the *absence* of the
   statement) plus the migration's own `DROP TABLE tasks_old`
   (`tasks.go:788`). There is **no production delete path** — which is exactly
   what R10 claims and what `TestTaskNoDeleteFuncRegression` enforces. The
   deletion was done **outside the code**: by hand, against the live DB.

The one documented purge explains the method and is the precedent:
`devdoc/legacy-t-purge-20260910.md` records a GLM night-window audit, a
per-batch decision table, an operator review (20/20 sample) and an application
to the production DB on 2026-09-10 — **751 rows deleted in one transaction**,
with a backup taken first (`memory.db.bak-pre-legacy-purge-20260910-094506`).
Its own framing is the finding: *"清理掉的是机制产出的垃圾，不是人的工作意图"*
("what was cleaned was garbage the mechanism produced, not a human's work
intent"), deletion profile `cancelled 687 (91.5%)`, `sync-yielded 558`,
`merge-backlog ~51`, `auto 667 + human 66 + llm 18`.

**What this costs the operator, and it is not the 756 rows:**

- **R10 is a policy, not a property.** The document's strongest structural claim
  — "Append-only, structurally. No delete path exists and a source-reading test
  forbids one" — is true of the *code* and false of the *ledger*. The guard's
  docstring is explicit that it is only an API-surface guard:
  `tasks_fts_test.go:136` *"the engine must expose no delete-capable task entry
  point"*. Nothing in the tree detects a row that left the table by any other
  route. My F6 measurement is, as far as I can tell, the **first time this
  ledger has been checked for it**.
- **Two of the document's own fixture rows are among the deleted, and the
  document still asserts they are permanent.** G10 lists `t-recon-guard-demo` /
  `t-recon-guard-demo2` as immutable-in-perpetuity under R10. Measured now:
  **both are gone.** `t-live-accept` (`failed`), `t-live-decoy` (now
  `completed`), `t-zd-confirmed` (now `completed`) and `t-report-legacy` (now
  `cancelled`) survive. So the row population this document measured was
  *already* post-purge, and G10's "all six exist today" was a quarter wrong when
  written — the deletion is **still running**, not a closed historical event.
- **The audit trail is the thing that broke, not the to-do list.** 93 orphaned
  `task_events` rows are the ledger's answer to "what happened to this row"; for
  those ids there is no longer a row to ask about. `task_events` was never
  named in R10's scope, and the purge did not sweep it — so the trail survived
  the very deletion the rows did not, which is how the discrepancy became
  measurable at all.
- **The next purge is unguarded too.** Every argument for a purge (F1's
  "uncleanable ledger", the 90-row default failed view) is still live, no tool
  implements one, and the only recorded method is hand-written SQL with a
  backup. A second purge is therefore likely, and is not covered by any test.

**Weakest link**: I could not determine **which** deletion removed the 756 rows
in the live table (the documented purge is 751 and the arithmetic points at it,
but the live-only 2 rows and the missing snapshot between 2026-09-11 and today
leave the correspondence at ~99 %, not 100 %). `[inferred: the gap set descends
from the 2026-09-10 purge plus one later hand-deletion of 2 fixture rows; the
direct evidence — a row in the ledger — does not exist, which is itself the
point of the finding.]`

**F3 (structural, and it contradicts a premise in this epic's own briefing) —
the auto-registration hook *does* populate the payload column.** The epic and
`taskregister.go:245-260` both state the committed contract as *"auto rows keep
payload EMPTY"*. But `01a0aaab-370a-776f-a8ac-e80cb7c009b0` (created by the
merger's resolve lane through `TaskNewOID` + `TaskSubmit`) reads
`source: human` with a full briefing payload, and the merger's *tracker* row type
(R2 of module 02) exists precisely to record the conflict briefing. So the
"payload is empty for auto rows" invariant is true for bare-prompt runs and false
for merger-owned rows — a boundary the module brief does not draw.

## 7. What this implies for tests

*(No tests written. These are the existing tests whose verdict the requirements
change.)*

| test | what it asserts today | the requirement says | direction |
|---|---|---|---|
| `internal/tools/task_finish_cleanup_test.go:191` (`TestFinishCleanupChainDirtyWorktree`) and `:912` (`TestServerRecycleDirtyWorktreeSkipped`) | *"no steps may run on a dirty worktree"* — the whole chain skips; the assertion is `len(rep.Steps) != 0 && !onlyUntrackedResidueNotes(rep.Steps)` (`:201`) | **R11**: push is idempotent and harmless; only the destructive steps (worktree remove, branch delete) may defer. The branch **must** reach the peer | **verdict changes, and it is the sharpest one in this table.** The test was written to allow a `no-op`/residue NOTE step only; a reordering that runs the push would leave a real `push` step in `rep.Steps` and **fail this assertion** — so this test is not merely silent about R11, it is a *blocker* to implementing it. F5 shows the fix that had to touch it. It should assert: push ran, worktree+branch survived. (`Residue`/`onlyUntrackedResidueNotes` at `:174`-`184` is the positive control: the file already knows how to permit a step inside a dirty skip.) |
| `internal/tools/tasks_test.go:672` (`TestTaskUpdateLifecycle`) | the status ladder moves forward and terminal moves stamp `ended_at` | **G1/F1**: it also proves `completed` is reachable with no evidence of delivery. The requirement does not say the test is wrong — it says the *model* is undecided | **green test, wrong requirement** — keep it, but a new requirement-level test is needed for "completed implies delivered-or-said-so" |
| `internal/tools/tasks_archive_test.go:72` (`TestFailedViewHidesArchived`) | 65 archived rows leave the default failed view | **R9** — correct as written; but it is also the proof of **F1** (the mechanism is a filter, not a boundary) | **keep; extend** with a source-separation assertion if the operator accepts F1 |
| `internal/tools/tasks_fts_test.go:136` (`TestTaskNoDeleteFuncRegression`) | no delete verb may be declared in `tasks.go` | **R10** — correct, and stronger than R10 needs: it inspects source text, so it also fails a *rename* | **keep; widen scope** to the other row-writing packages (G9), where the same literal scan would catch a delete path added outside `tasks.go` |
| `internal/tools/memory_sync_delta_test.go:1175` (`TestSyncDeltaPendingHumanTicketVsCancelledMirror`) | a real ticket is never blind-flattened (bug053 fix) | **R9/R7** — correct. This is the fix for incident 2 | **keep — it is the requirement made testable**; note it is the *only* incident turned into a named requirement test |
| `internal/tools/task_run_closeout_test.go:53` (`TestRunCloseoutClosesOpenRow`) | an open row whose child exited normally is closed `completed` | **G1**: the test institutionalises "the wrapper stopped ⇒ completed", with no delivery check | **verdict changes** — this is the local twin of incident 1; the requirement says close-out must consult delivery (or write a reason that says it did not) |
| `internal/tools/taskstate_test.go:165` (`TestTaskInProgressStateHonestyBoundaries`) | the display label is honest for live/finished/died | **R12** — correct and load-bearing; it is the one place the operator's "is it dead?" question was made machine-checkable | **keep** |
| `internal/tools/chat_test.go:147` (`TestChatBackfillMigratesLegacyAutoRows`) | the backfill appends ` — chat: backfilled` to a row's **summary** | **R2**: the summary is immutable — and `chat.go:501` does exactly what bug051/bug056 forbade | **verdict changes**: this test pins a violation of R2 that the bug051/bug056 rulings only fixed on the writeback path |
| `cmd/agent/cli_tasks_list_sweep_readpath_test.go:120` (`TestTaskListReadPathSweepsNothing`) | a plain `list` writes nothing and fires no event | **R10/R5**: correct, and it is the requirement "reading my record must never change it" | **keep**; it is the model for how to test the ledger (a counter, never a clock) |
| `internal/tools/task_finish_delivery_test.go` / `_observe_test.go` (**not in the tree at HEAD**) | *"the branch is delivered before the worktree-dirty guard; a stranded delivery is observable"* — 430 lines added by `6e22d80b` | **R11 exactly** | **the tests for this requirement were written, passed, and were deleted by the `085811f8` sweep along with `task_finish_delivery.go`** (F5). A requirement whose only tests live in the history of a swept file is the reason R11 keeps re-opening; recovering them is a concrete, bounded piece of work — not a new test to write |
| `internal/tools/tasks_fts_test.go:136` (`TestTaskNoDeleteFuncRegression`) and **no counterpart over the data** | no delete verb may be *declared in `tasks.go`* (its docstring says "at the API-surface level") | **F6**: the property R10 actually rests on is "no row leaves the table", and 756 rows left | **keep, do not weaken** — and the *gap* is that nothing checks the property. The measurement is currently a manual query (`max(id) − count(*)` vs `task_events` orphans); if the operator accepts N6, the natural home is a ledger-integrity probe next to the existing ledger-hygiene verbs, not a new gate on the merge path |

## 8. Confidence and limits

### Confidence per requirement

| req | confidence | basis |
|---|---|---|
| R1 filing is one line | **high** | operator's verbatim ruling `t-1788037892` + the implemented `payloadMirrored` path |
| R2 brief frozen / self-sufficient | **high** | operator's verbatim ruling `t-1788039140` + three hard refusals + a test |
| R3 status is ordered, forward-only | **high** | operator's verbatim ruling `t-1788039618`; the ladder is data with a pinned test (`memory_sync_rank_test.go:13`) |
| R4 reason is first-class | **high** | the operator filed it himself (`t-1788470578`) |
| R5 the operator can ask his questions | **high as a need, measured as a gap** | 12 of his own rows are the questions; §4a is a live measurement |
| R6 one execution, one row | **high** | operator's EPIC GOAL `t-1788562577`, with the cost measured in his own problem statement |
| R7 the ledger is one thing across nodes | **high** | `t-1788036628` + `t-1788147782` + `t-1788093118` |
| R8 never run twice | **high** | `GOPI.md` Rule 28 states the consequence; the CAS + rank ladder is the mechanism |
| R9 failures are evidence | **high** | operator's verbatim title in `t-1788036628` ("nick 同意") |
| R10 append-only | **corrected: POLICY high, PROPERTY FALSE** | the intent comment (`tasks.go:5`-`6`) and `TestTaskNoDeleteFuncRegression` (`tasks_fts_test.go:136`) are both real and both about the **code**; measured against the **ledger**, 756 row ids are missing and the guard's own docstring says it only covers the API surface. See F6/N6. **Weakest link**: no tool, test or row in the tree observes a row leaving the table, so the property is unverifiable by anything except the measurement I ran. |
| R11 done ≠ delivered | **medium-high** | the *incident* is documented (`brief-hp-push-diagnosis.md`); the *requirement* is `[inferred]` from his reaction, not stated by him — **weakest link**: no operator row says "completed must mean delivered"; he has only ever complained that the branches did not arrive |
| R12 liveness honesty | **medium-high** | strong evidence that he *asked* (`01a09fa7-…`, `01a09435-…`); his *acceptance* of the current labels is `[inferred]` from the absence of a follow-up complaint — **weakest link**: no row closes the loop |
| R13 vocabulary can grow | **medium** | `[inferred]` from the legacy-reader clauses and self-healing migrations; **weakest link**: no operator statement about backward compatibility exists — the practice may simply be an engineering habit |

### The five incidents, classified

**Incident 1 — rows said `completed` for work that never left the machine.**
`briefings/brief-hp-push-diagnosis.md` (measured 2026-09-16: three branches 19/3/5
commits ahead of peerbox, tasks `01a0a97e`/`01a0aa74`/`01a0aa0e` all `completed`,
nothing notified); today's rows `01a0aab1-587a-…` and `01a0aab8-ea14-…`, both
`completed` with reason `"task finished"`.
→ **REQUIREMENT VIOLATED — R11.** The ledger claimed a state the world did not
have, and the mechanism that should have made it true (push) was short-circuited
by an unrelated guard. The requirement is real; the code did not serve it.
**Corrected in the third pass (F5): "the code did not serve it" is the wrong
verdict.** `6e22d80b` *did* serve it — the push was moved ahead of the dirty
guard, with `task_finish_delivery.go` and 430 lines of tests — and `085811f8`
later backed that file out along with the ordering (it does not exist at HEAD;
the guard again short-circuits the push at `task_finish_cleanup.go:1145`/`:2164`,
before the push at `:2232`). The requirement is real, the code served it, and the
serving was destroyed. Incident 1 is therefore **not closed** and its ticket
`01a0aab8` reads `completed`.

**Incident 2 — a sync conflict flattened three real tickets to `completed`.**
Rows `t-1788259388` (bug050), `t-1788259882` (bug051), `t-1788260677` (bug052) —
all `completed`, all tagged `sync-resolved`, all with **no implementation on any
branch**; then `t-1788278743`, `t-1788278743-1`, `t-1788279646-2` are the
*reissues* that carry the same work, and `e260bb63-9d9e-4ae3-84f6-c744ff66dace`
is the operator's own audit row: *"[bug053] 三张 bug 单虚假关账（bug050/051/052
全部 completed 但实现不存在）——M4.16 sync-conflict 裁决的系统性副作用"*. The mechanism
is named in the fixed code (`memory_sync_delta.go:584-600`).
→ **REQUIREMENT VIOLATED — R9 (an idea may never be silently lost) and R8's
spirit.** The false close was not a status bug: the *ruling* granted a verdict
("the work merits done") that only real work could merit. Fixed in M4.16's
semantic correction, which now refuses to flatten a real ticket and alarms
instead (`tasknotify.go:86`, `taskNotifyEventLedgerConflictHumanTask`).

**Incident 3 — zombie rows: a killed run left `in_progress` for ever.**
`01a09435-9ffe-…` (the operator's own filing of the display-honesty defect),
`01a09431-fca8-…` and `01a09435-…` ("7 行僵尸：干完了但停在 in_progress，成果已进
master"); mechanism evidence `internal/tools/taskstate.go:3`.
→ **REQUIREMENT VIOLATED at the time — R12 — and SERVED since.** The state was
recoverable-but-invisible; the fix (honest labels 2026-09-12, then the process
identity triple) satisfies R12 at the reader. **Residual**: G5 — 5 rows carry the
same shape today and no sweep closes them.

**Incident 4 — `--status failed` had 57 rows nobody was triaging.** The
historical measurement is quoted verbatim inside the fixed briefing
(`briefings/brief-task-ledger-hygiene.md`: *"`gopi task list --status failed`
currently shows **57 rows**. Only 6 carry the `resolved` tag."*) and the task is
`01a09214-d6f4-74bc-b0f6-a68d7a117c4d`.
→ **NEVER DECIDED — then decided by practice.** The *requirement* ("the failed
view must be a signal, not a pile") was never stated as a need; the operator's
rows show him *acting* on the symptom (filing hygiene tasks), and the archive
verb is the convention that emerged. Today's numbers (61 visible / 126 total)
show the same residue pattern recurring, which is the evidence that the
underlying requirement — *the ledger must separate my record from the machine's
telemetry* (F1) — is still undecided.

**Incident 5 — a row's summary was overwritten by the writeback path.**
Rows `t-1788259882` (bug051: *"writeback 覆盖 summary——违反 content 不可变（任务原摘要
被'gopi run completed'覆写，审计线索丢失）"*), `t-1788278743` and
`cbe57cd6-…` (bug056, the recurrence — proof the first fix was incomplete).
Mechanism and fix are both named at `internal/tools/tasks.go:3408-3416`
(*"the old `if summary != ""` arm wrote the writeback's result digest over the
submit-time summary (bug051's false-close symptom 3128f17f)"*).
→ **REQUIREMENT VIOLATED — R2, unambiguously.** The operator had stated the rule
verbatim (`t-1788039140`) three days *before* the writeback path broke it. This
is the cleanest case in the set: a stated requirement, a stated mechanism, a
violation, a fix, and an enforcing test.

### What I could not determine

1. **Whether "completed" is allowed to mean "the process stopped".** The decision
   is recorded in this epic's own briefing (decision 4) as the operator's words —
   but no *ledger row* carries it, and the premise is quoted inside a briefing
   whose own author notes three incidents reading it the other way. I could not
   find the original row or session where he said it; the quote is second-hand
   from my own briefing. **If that quote is an LLM's paraphrase rather than his
   words, G1 changes shape entirely** (from "undecided" to "decided, and the
   code serves it"). This is the single weakest link in this document.
2. **Whether the five incidents in this epic's briefing are "the" five.** The
   original raw epic briefing is not in `briefings/` (only
   `briefings/brief-req-2-task-ledger.md` exists, and it lists the same five), so
   I cannot tell whether they were drawn from a house list or authored for this
   task. I classified the five as given.
3. **Why the auto-registration hook was allowed to write `source=human`
   payload-bearing rows** (F3) — whether that was a decision or drift. I found
   the mechanism (the merger resolve lane creating its tracker row through the
   human-submit face) and no statement about it.
4. **What the intended queue ordering is.** `t-1788087991` (explicit FIFO +
   priority) is `cancelled`; the shipped order is priority-then-age with
   tag-derived priority. I could not find the row that supersedes it — the intent
   for a 31-row queue (re-measured 2026-09-18T11:01Z; 82 at the prior pass) is
   unrecorded (G4).
5. **Whether the operator ever accepted the honest liveness labels** (R12) —
   there is no follow-up row after `01a09435-…` either way.
6. **How much of the ledger the operator considers *his*.** F1's
   human/auto separation is my inference from counts (608 human rows — 536 after
   the 72 `gopi run` telemetry rows — against 1299 `auto`+`llm`, all re-measured
   2026-09-18T11:01Z); no row states
   the boundary he wants (F4 supplies the *mechanism* for a boundary, not the
   operator's decision to draw one). Note the `llm` population (412) is *not*
   obviously telemetry: it is largely agent-filed bug reports, which are his
   process's output rather than a run's log.
7. **Whether `task_events` is meant to be operator-visible at all** (G2). It is
   written as an audit trail and surfaced once, as `result:`; both readings are
   consistent with the code.
8. **The exact provenance of the `no-op` summary marker** (G7): the helper exists,
   is documented as the marker mechanism, and has no caller — so I cannot tell
   whether a *future* path intended it or whether a refactor orphaned it.
9. **Whether the prior version of this document was itself the sibling's
   deliverable or an earlier draft of the same task** — its front matter claims
   "nothing to complete, nothing to duplicate" while its own branch
   (`docs/requirements-task-ledger`) had already been archived as
   `archive/docs/requirements-task-ledger`. I treated it as conforming and
   completed it (see the front-matter reconciliation); I could not determine the
   intent behind the archive tag.
10. **Which deletion produced the 756 missing row ids, and by whose authority.**
   F6's arithmetic points at the documented 2026-09-10 purge (751 rows), but ~2
   live-only gaps and a missing snapshot between 2026-09-11 and today leave the
   correspondence at ~99 %. There is no ledger row, commit or devdoc that
   records a second deletion — **the deletion that removed two of this document's
   own fixture rows left no trace anywhere except the absence of the rows.**
   Whether it was another night-window audit (the purge's method) or an ad-hoc
   hand-run is undeterminable from the tree.
11. **Whether the R11 reorder (F5) was deliberately retired or accidentally
   swept.** The sweep's commit message explains backing out a half-applied merge,
   not retiring a capability; the swept branch's own ticket `01a0aab8` reads
   `completed` with reason `"task finished"`. Both readings fit the record.

---

### Appendix — the load-bearing evidence files

- Module brief (this deliverable's authority, quoted in full where it quotes the
  operator): ledger `01a0ab3d-1e94-7590-bb98-e6fe771f5403`, payload rendered by
  `gopi task show <id>` — **re-resolved this session (it is live, `completed`,
  `source=llm`, tagged `from-hostb-Laptop,requirements,docs,module-b1`).** The
  prior pass kept a working copy at `.evid/b1-brief.txt`; that file is not in the
  repo and was read from the sibling worktree, which no longer exists — the row
  is the authority, and it still resolves.
- Governing operator rows, quoted verbatim in §4, **all re-resolved against the
  live ledger 2026-09-18T11:01Z** (status is stated, because the status is part
  of the evidence): `t-1788039140` (completed — R2 immutability),
  `t-1788037892` (cancelled — R1, creation verb removed), `t-1788039618`
  (completed — R3 instantaneous claim), `t-1788036628` (completed — R7/R9),
  `t-1788470578` (completed — R4, "task should have a reason field", filed by the
  operator himself), `t-1788562577` (completed — R6), `t-1788053164` (completed —
  bug028, R2 self-sufficiency), `01a0aab1-587a-…` / `01a0aab8-ea14-…` (both
  **completed** — the R11/f5 pair, and per F5 that is itself the defect:
  `01a0aab8` is the ticket whose fix was swept out), plus the module brief and
  epic rows `01a0ab3d-…`, `01a0ab11-69c4-…`, `01a0aac4-861f-…` (cancelled, the
  migration guard) and `01a0af50-5029-…` (completed — the Rule 30 tracked-work
  incident).
- Live measurements (**2026-09-18T11:01Z**, `bin/gopi` built 2026-09-18 18:51
  local, master `783186b0`, ledger `/home/user/.gopi/memory.db` **110,526,464 bytes,
  1907 rows / 3394 events**): every count in §4a, §6, F1–F6 was produced by a
  command in this session, not recalled. **The prior pass's numbers (2421 rows /
  360 events at 2026-09-17T20:00Z) are not a stale version of these — the table
  SHRANK by 514 rows and the event trail grew ~9× between the two readings**,
  which is F6. Three readings of the same query taken minutes apart during this
  session moved by 1–2 rows and 1–3 events (`task list --all` → 1908 while the
  table held 1907: the read itself mints a row) — the drift is the module's own
  behaviour, so every count here is stamped with the moment it was read.
- **The prior pass's git base is no longer a tip, but nothing here is
  unreachable**: `17cec86b` is an **ancestor of master** (verified
  `git merge-base --is-ancestor 17cec86b master` → exit 0); it is simply no
  longer a branch tip, which is why `git show 17cec86b:<file>` (not a checkout)
  is how its citations were checked. The same holds for F5's two halves:
  `6e22d80b` and `085811f8` are **both ancestors of master** (exit 0 each) while
  `internal/tools/task_finish_delivery.go` is **absent from master's tree**
  (present at `6e22d80b`, removed by `085811f8`) — the code was destroyed, the
  commits were not orphaned. An earlier draft of this record said "unreachable
  from master"; that was wrong and is corrected here, because the distinction is
  load-bearing for F5: a *destroyed-but-ancestral* fix is recoverable with
  `git show 6e22d80b:<path>`, whereas an unreachable one would need the branch's
  tip to survive. **Every claim in this document that depends on a code path was
  re-verified against `d6999ddf`/master `783186b0`, not against the base the
  prior pass cited.**

**Care-pass record (this revision, the third pass).** Three passes of care are
now on record, each with its own correction class:

- *Pass 2 (2026-09-17T20:00Z)* re-read every citation and corrected three line
  numbers, and re-measured all ten quantitative claims (`2389/2319/…` →
  `2421/2351/…`).
- *Pass 3 (this one, 2026-09-18T11:01Z)* **re-verified all 74 `file:line`
  citations against the current tree**: 36 resolved unchanged, **38 did not**.
  The 38 were checked against the prior pass's own base (`git show
  17cec86b:<file>`) and each pointed at the sentence it claimed — *they were
  correct when written and were invalidated by the 74 commits that touched the
  ledger files afterwards* (`tasks.go` alone moved by ~1300 lines). Every one
  the argument rests on is re-anchored above, and the load-bearing ones are now
  cited by **symbol** (`taskClaimOrderBy`, `finishCleanupWorktreeDirtyReason`,
  `reclaimRunResidueRefusal`, `taskEventWrite`/`taskEventLatest`,
  `TaskUpdateWithResult`) so a line shift cannot silently break them again.
  Spot-checks of the "correct then, rotted now" claim, recorded as evidence:
  @`17cec86b` `tasks.go:2854` = *"The terminal tier is deliberately ONE rank"*
  (now `:2885`); `:3408` = *"bug056: summary is IMMUTABLE"* (now `:3486`);
  `:5709` = *"var taskClaimOrderBy"* (now `:5840`); `cmd/agent/cli_tasks.go:505`
  = *"payloadMirrored records"* (now `:567`);
  `task_reclaim.go:1393` = *"func reclaimRunResidueRefusal"* (now `:1672`).
  **This pass also caught its own error of that class**: the appendix's
  reachability record had claimed `17cec86b`/`6e22d80b`/`085811f8` were
  "unreachable from master". All three are **ancestors of master** (exit 0 for
  `git merge-base --is-ancestor` each); what was destroyed is a *tree entry*
  (`task_finish_delivery.go`), not a commit — this is exactly the
  "correct when written, wrong later / wrong when written" class the pass exists
  to catch, and it is corrected in the appendix with its verification command.
  All ten quantitative claims re-measured again (the numbers above), **and two
  verdicts changed**: R10 (append-only) from "high, enforced" to "policy holds,
  property false", and R11 from "the code did not serve it" to "the code served
  it and the fix was destroyed" (G10, §7's first row, R10's confidence row, N6
  and unknowns 10–11 carry the corrections).
