<!-- doc-class: normative -->
# Requirements (reverse analysis) — 04: sync & node identity — why my two machines are one system, and how each knows which one it is

> **PARTLY REVERSED 2026-11-06 — the command this page names is BACK, minus one flag.**
> `gopi memory sync` was restored the day after its deletion, as a MANUAL door:
> one invocation = one session, exit. `--peer` / `--push` / `--pull` / `--dry-run` /
> `--serve-assets` / `--timeout` all run again; the runnable shape and its measured
> two-way session are in `devdoc/memory-sync-manual-door-restored.md`. **What is
> still gone is the `--every <dur>` cadence flag**, whose reader and loop went in
> batch 5 — so a line below that carries `--every` is a RECORD, not a command to
> paste (it exits 1 with `unknown flag`). The one-way copy `gopi memory backup`
> is the other manual cross-host door and its guarantees differ: it REPLACES the
> peer ledger instead of merging it.

> **CORRECTED 2026-11-05 — the pacing knob is GONE.** This page's §R3 (the ledger
> session's paragraph naming `GOPI_SYNC_INTERVAL` / `syncIntervalEnv` /
> `syncIntervalFromEnv` / `runTaskSyncTick` in `cmd/agent/cli_tasks_sync.go`) and
> its §N4 (the "and a pacing knob" sentence) describe a **deleted** file and a
> **removed** knob: `GOPI_SYNC_INTERVAL` had zero production readers, the whole
> `cmd/agent/cli_tasks_sync*.go` family went with the 2026-11-05 cross-node
> retirement, and `runTaskSyncTick` is gone with it. The same is true of
> `GOPI_SYNC_FULL_EVERY` / `GOPI_SYNC_GIT_INTERVAL` wherever this page or its
> siblings name them. Read those paragraphs as the measurement of the code AS IT
> WAS on the date they carry; for what exists today read `CONFIG.md` and
> `internal/config/sync_knob_removal_gate_test.go` (the anti-re-advertisement
> gate). Every anchor below into `cmd/agent/cli_tasks_sync*.go` is now dangling —
> the file, not the line number, is what went.

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Re-measured again:** 2026-10-13 (task `01a13219-058f-7bcdb14d-af1d6616835b`) — three further drifted citations were re-keyed by SYMBOL, on the same method as the 2026-10-03 pass: both occurrences of the startup-banner anchor in §R5 (`config.NodeIdentityLine`'s call site in `cmd/agent/main.go`) and §R6's finish-chain constant anchor (`finishCleanupPeerRepo` in `internal/tools/task_finish_cleanup.go`). Each new line was read by CONTENT (`sed -n`), never by arithmetic shift.

**Module:** B4 of 12 · **Host of record:** `hostb-Laptop` (the client) · **Measured:** 2026-09-17
**Re-measured:** 2026-10-03 on `peer1` at master `fd126c45` — every `.go:NNN` citation this document carries was read by CONTENT, not existence: **§O2** (the peer-whitelist channels) and **every other drifted anchor** across §R1/R3/R6/R8/R10/R11, §6.1, §6.3/§6.4, §7.1–§7.3 and the C1–C3 constraint rows. Each drift is re-keyed to its true SYMBOL plus current line — never re-baselined, never re-keyed by arithmetic, and where the symbol lives in a different FILE than the one cited the row now names the file that carries it. The dated 2026-09-17 record above is kept; every re-keyed anchor is marked `re-measured 2026-10-03` where the drift changed the sentence's MEANING (and §6.1 carries its own note for its table), while `devdoc/o2-peer-channel-remeasurement-20261003.md` §1 carries the full table — each cited line's actual content, and the symbol that replaced it.
**Method, standards, 8-section shape, C1–C5:** carried inline in this task's payload (umbrella `8e2fa780`/`a2d1af6d`, `01a0ab12` series).
**Sibling module in the other series:** `01a0ab12-35e7-71c2-a03a-ec98256a3c39` (status `pending`, no session, no branch, no document — checked `git log --all -- devdoc/requirements/` on 2026-09-17: only `00-method-and-standards`, `00-module-map`, `01-per-module-briefings`, `01-task-ledger`, `02-merge-and-branches`, `README`, `merge-branch-lifecycle` exist; nothing to judge or redo).

---

## Front matter — the boundary I chose

```
Boundary:      the two machines are one system: work and knowledge move between
               them, and each machine can say which one it is — and act on that.
In scope:      the task-ledger sync channel (delta session, harvest/pull, claim
               broadcast, full reconciliation, the anchor and its guard), the
               memory-asset channel, the git passenger channel (push-back /
               fetch / the finish-chain's delivery step), node identity and role
               resolution (the declared role, the provenance name, the two
               whitelists, the self-check), and the operator-facing surfaces that
               expose them (task sync / task harvest / task push / task diff /
               memory sync, the startup identity line, the per-child identity
               line).
               Evidence: internal/tools/memory_sync*.go, internal/tools/task_sync_git.go,
               internal/tools/task_finish_cleanup.go (the delivery gate),
               internal/config/{node_role,role,config,settings}.go,
               cmd/agent/cli_tasks_sync.go, cli_tasks_sync_git.go, cli_memory_sync.go.
Out of scope:  the MERGE of a delivered branch and everything that happens to it
               afterwards (judged in B2, devdoc/requirements/02-merge-and-branches.md);
               the ledger row's own lifecycle semantics (B1); the worktree's
               creation/release machinery (B3, though this document judges the ONE
               delivery gate that sits inside it); notifications as a subsystem
               (B10 — this document judges only whether the sync alarm is reachable).
Boundary why:  there is no sync package, and there is no sync directory. The
               directory census is unambiguous: `internal/sync` does not exist,
               and the responsibility is spread over five files that mutate the
               ledger plus three in the config package. It is a boundary because
               the NEED is user-visible and separable ("is my other machine up to
               date?"), not because a path names it. Conversely `internal/tools`
               is not the boundary (umbrella mismatch M1: it holds the tool
               implementations, the ledger, dispatch, merge and worktree
               lifecycle too), and `cmd/agent/cli_tasks_sync*.go` is a SURFACE of
               this module, not a module (mismatch M4).
```

**Directory-vs-responsibility mismatches to restate here (umbrella §7.1):** **M1** —
the whole channel lives inside the package that also holds the agent's tools and
the task ledger, so "one module = one directory" is false here in the strongest
form; **M2** — the asset channel writes the knowledge store while the store itself
(`internal/hippocampus`) is a different organ, and the unit of transfer is the
asset row, so the boundary must be drawn by *what moves*, not by *who stores it*;
**M4** — the CLI verbs (`task sync`, `task sync --serve-remote`, `task push`,
`memory sync`, `task harvest`, `task diff`) are this module's cockpit, and their
argument shapes are the module's public contract.

---

## 0. The operator's binding constraints (C1–C5), as they bear on this module

The five constraints are stated in the umbrella; this module must be checkable
against them, so they are restated in one line each with the tension this module
creates. Conflicts are filed in §6.5, not softened here.

- **C1 — one static binary, zero dependencies, no plugin architecture**
  (`devdoc/roadmap.md:5` "静态单二进制", `:64` "CGO_ENABLED=0:
  始终编译为静态二进制文件"; `internal/agent/loop.go:895` "7. 依赖预算: ZERO-DEP default.", pinned by `internal/agent/prompt_test.go:166`).
- **C2 — the operator must control the whole thing; anything he cannot observe or
  reverse is a defect** (`t-1788383728` family; the paraphrase "one place he
  understands" is an agent's wording and must never be quoted as verbatim).
- **C3 — a daemon must be simple; every branch of logic multiplies its failure
  surface** (`01a0a995-8742-77fc-a164-53054ea652c5` verbatim; `限制优于清理`,
  `01a08da4-1584-7c8f-9beb-cecdfa0d79a7`).
- **C4 — the agent must clean up after itself; dirty is a violation, not a
  condition to tolerate** (`01a0975a-3e3f-7ae5-a848-a98cd24d6dc3`). The
  blast-radius guard is CLOSED (`rejectOutsideDeclaredRoots`
  `internal/tools/path_policy.go:215`) and is not
  re-raised here; what this module owes C4 is the *delivery* half — a cleanup that
  destroys the only copy of a deliverable violates the constraint no matter how
  tidy it is.
- **C5 — a small team, solutions one person can run** (`01a08b9a-e5de-741d-b5a4-f934d7f50adf`,
  `01a08b92-7681-7dac-a26f-d9c38e79883c`). For this module C5 is quantitative:
  how many places must one person keep in sync to stay correct?

---

## 1. What this module is for

I work on two machines. One of them is the one that decides things and holds the
shared history; the other one is where I do most of the actual writing. Both of
them hold my work, and I expect them to behave as **one system**, not as two
copies that drift apart.

Three things must be true for that to hold, and all three are the subject of this
module.

First, **what I did on one machine must show up on the other**, and it must do so
without me carrying anything between them by hand. When I file an idea on one
machine, when a piece of work starts, when it finishes or fails, the other machine
must learn it — and the two of them must not be able to end up with *different
answers* about the same piece of work.

Second, **the finished article itself must travel, not just the note about it**.
A note that says "this is done" while the actual work sits on the machine that
wrote it is worse than no note: it is a record that lies. The rule I stated to my
agent was blunt — only moving the note is worth nothing.

Third, and this is the part that is easy to skip: **each machine has to know which
one it is, and behave accordingly.** They are not interchangeable. One of them is
the centre; the other one reports to it. If the wrong one of them believes it is
the centre, it will start settling questions that are not its to settle, and it
will do it silently, because from the inside both machines look identical.

**The honest caveat, in my own terms.** I do not need this to be instant. I do not
need the two machines to agree at every moment. What I need is that every piece of
work that has moved ends up in the same condition on both sides, that I can tell
which side's version is the current one, and that nothing is lost quietly while I
am not looking. This module exists to make "one system" *true*, rather than true by
my assumption that it is.

---

## 2. Who is served

### 2.1 The operator (the operator) — "I should not be the transfer mechanism"

The need is that work crosses machines while I am asleep. Concretely: I file a task
on peer1 in the evening; the laptop picks it up, does it, and the result and the
record of it are back on peer1 without me running anything. **What I will never
accept in place of that:** being told "you need to run this on the other one too",
or discovering days later that a finished piece of work only ever existed on the
machine that produced it. The verbatim statement of this need is a paste of my own
shell session with a worktree still parked and the note in my words —
"gopi client should push git branch to server and clean up" (`t-1788252501`,
rows `t-1788252501`, re-issued as `t-1788255198`).

### 2.2 The agent (Bond) — the actor that must not guess where it is

An agent session runs on one machine and has to reason about the topology. In
2026-08-31 an operator-observed transcript shows the agent asking *"Wait, which
node am I?"* and answering itself by inspecting which remote repositories happened
to be configured — **it guessed right, by luck** (`b7b1735e-4900-4bb2-b0fb-f00d12a4295d`,
`8a37eef4-a671-4d7c-b062-454ecf60261f`, both recording the operator's observation
and the diagnosed root cause: neither side's settings file carried a role field).
The need, distinct from the operator's: an agent must be *told* which machine it is
on, in its own first message, and must be able to tell which of two records about
the same work is authoritative.

### 2.3 The two machines, in their two opposed roles

This is the only actor pair in the whole system that is *asymmetric*. The centre
("server", peer1) owns the system layer and may rule on a contradiction; the
reporting machine ("client", HP) does the feature work and must never rule
(`GOPI.md:1002` Rule 28; the role table at `:1002-1004`). The operator's rationale
is recorded in the rule itself: every 2026-09-16 incident came from both machines
working the same layer at once, and the division exists so that a feature branch
comes back *mergeable*, "not so the hosts have different labels".

### 2.4 The future maintainer — the actor C5 is about

That person must be able to answer "what must be identical on both machines, and
what must never cross?" without reading 54k lines of `internal/tools`. Today the
answer is spread over: two whitelist settings, an environment variable that
overrides the machine name for one purpose but not another, a startup banner, a
per-child briefing line, and a rule in the project's rule file. §6 is where that
becomes a finding rather than an invitation.

---

## 3. The workflow it sits in

**Entry condition.** One machine holds new information and the other does not:
a row was filed, claimed, progressed, finished; an asset was distilled; a branch
was completed and needs to reach the centre `[inferred: the entry condition is my
reading of the code — no operator sentence states it in these words]`.

**The three channels that carry it.** They are deliberately separate and their
units differ (this separation is a requirement, not an accident — see §4 R6):

1. **the ledger channel** — task rows, moved as a timestamp-delta patch over a
   direct secure-shell call, one session per peer
   (`SyncPeerSession` `internal/tools/memory_sync_session.go:57`, the transport it reaches
the peer through `syncSSHExchange` `:173`);
2. **the asset channel** — knowledge-store rows, moved "unique-only" with no
   conflict resolution at all (`t-1788307190`; `MemoryAssetsImportPatch` `internal/tools/memory_sync_assets.go:755`, whose
insert path is `syncAssetsImportRows` `:898`);
3. **the git channel** — branches and the trunk, moved by ordinary version-control
   operations, which ride *before* the ledger so that a row's promised branch
   already exists when the row arrives
   (`internal/tools/task_sync_git.go:213` "deliverable{branch} lands BEFORE any
   db row migrates") `[inferred: the ordering rationale is the code comment's own
   claim, and I did not find an operator row demanding the order]`.

**Steps, end to end** (client machine):

- a run finishes → the finish chain resolves the machine's role → on the client it
  pushes the branch, removes the worktree, deletes the branch (in that order:
  `internal/tools/task_finish_cleanup.go:3-30` — the chain's own doc comment — and the
  client/server branch it describes at `:1173-1180`) → the branch is on the
  centre;
- the centre's merger then sees an ordinary branch and merges or parks it (B2);
- the ledger session runs (manual verb, or the daemon's tick when the pacing
  variable `GOPI_SYNC_INTERVAL` is set: `syncIntervalEnv`
  `cmd/agent/cli_tasks_sync.go:99`, read by `syncIntervalFromEnv` `:667`; the tick
  itself is `runTaskSyncTick` `:697`) → the receiving side
  applies the patch in one transaction and only then advances the anchor
  (epic ⑥: the anchor is written by `syncStateCommit`
  `internal/tools/memory_sync_delta.go:1118`, i.e. AFTER the apply — never before it).

**Exit condition.** The receiving machine's ledger contains the sender's rows; the
sender's receive-anchor for that peer has advanced; the branch exists on the
centre; and, for the asset channel, the receiver holds the sender's unique rows
`[inferred]`. **The other half of the exit condition is the part the code does not
have**: nothing in the protocol states what happens when a row is *deleted* on one
side (§6.2 G3).

**What the operator does by hand, today.** He can ask one machine what it thinks
of the other (`task sync --status` → HP answers `peer peer1: anchor=… last_synced=…`
— measured), compare the two row-sets (`task diff --peer`), force a full replay
(`task sync --full`), or move knowledge rows (`memory sync --peer`).

---

## 4. Why it exists — the requirement per need, traced

Every requirement below carries its citation. `[opinion]` marks my own judgement,
`[inferred]` marks a requirement I reconstructed from code shape with the weakest
link named (§8 repeats them as a list).

### R1 — The two machines are one ledger, and a row's history is one story
**Need:** I file work on one machine and see it on the other; a row must not be
able to mean two different things on the two machines.
**Evidence:** `t-1788036628` (operator: HP-side runs only ever wrote back to HP's
own database, "peer1 台账永远 pending" — remote SQLite is the wrong answer by
design); `t-1788093118` `[M4.13]` (the timestamp-delta design, "SyncML 协议核心
简化"), with the operator's own confirming wording in the same row — **the operator 确认
willing to implement**; `SyncTaskExportDelta` `internal/tools/memory_sync_delta.go:187`
(export: rows newer than the anchor), `SyncTaskImportDelta`
`internal/tools/memory_sync_delta.go:431` (import: one transactional apply).
*(re-measured 2026-10-03: the 2026-09-17 citations `memory_sync_delta.go:186`/`:429`
were both comment lines — `:186` `// (one SELECT, no policy).`, `:429`
`// SyncTaskImportDelta applies a v2 patch through the merge.` — one and two lines
above the functions they meant. Re-keyed to the symbols.)*
**Note on the shape:** the *unit of truth* is the row's `updated_at`, and the
protocol is one-directional per session with an anchor per peer
(`syncStateGet` `internal/tools/tasks.go:1068`, `syncStatePut`
`internal/tools/tasks.go:1082` — the only reader and the only writer).
*(re-measured 2026-10-03: `tasks.go:866`/`:878` are inside an unrelated
table-rebuild predicate; the sync_state accessors are the pair named above.)*

### R2 — Where the two machines disagree, one of them is allowed to decide — and it is not the one doing the work
**Need:** independent parallel work is the point; it will produce contradictions,
and a contradiction that nobody is allowed to settle becomes a stuck row.
**Evidence:** `t-1788091028` `[M4.9 增补]`, operator verbatim in his own words:
**"this is price we have to pay"** (the price of independent parallel development);
the ruling itself is recorded inside the code as **the operator 冲突语义定稿**
(`internal/tools/memory_sync_delta.go:563-570`, verbatim: the task status "is a mere
enum — the real deliverable lives in the git branch … So the SERVER side simply
writes completed … the CLIENT side keeps the legacy keep-row + 'conflict' tag …
client never rules"); the role asymmetry is `GOPI.md:1002` Rule 28.
**Measured today — the assignment is correct and the *ruling half* is unreachable
on BOTH machines.** The client's behaviour matches its design exactly: it keeps the
row and tags it `conflict` (157 such rows on the client, 16 outside the merge
channel's own tag, against 48 on the centre). The centre is where it breaks: it
never rules either, because its `IsServerNode()` is false — its settings declare no
role **and** its peer list is empty, and the hostname arm retires to
`return false` unconditionally for a machine with no peers
(`IsServerNode` `internal/tools/memory_sync_delta.go:815`, its fail-safe `return false`
at `:838` — and see §G1: every path of that
fallback returns `false`, so the effective predicate is `role == server` alone).
`sync-resolved` appears 36× on the
client and 22× on the centre, and **both sides' most recent occurrence is the same
morning** (client 2026-09-01 11:04:37, centre 11:25:30) — nothing has been ruled
since. `[inferred: the dates correlate with the role fail-safe landing that same
day (`dd118f5d`, 2026-09-01 03:45); I did not bisect what the centre's role
configuration was on 08-31, when it last wrote the tag.]`

### R3 — A ruling must never be able to close real work
**Need:** "the machine decided" must not be a way for a ticket that a human wrote
to disappear.
**Evidence:** the operator's own alarm is recorded as the semantic fix
**bug053 / M4.16** in the code (`syncDeltaMergeRow` `internal/tools/memory_sync_delta.go:540`,
the blind-ruling block at `:584-604`; `notifyLegacyConflict`
`internal/tools/memory_sync_delta.go:688`): tasks `bug050/051/052` all carried
`sync-resolved` while their
implementation never existed — the ruling "FAKE-CLOSED real work". The fix draws a
line at the row's *origin*: an auto-registered mirror row may be flattened, a real
ticket (human or model, non-empty payload) may not, and may never be
auto-completed (`notifyLedgerConflictHumanTask` `internal/tools/memory_sync_delta.go:763`,
firing `taskNotifyEventLedgerConflictHumanTask` `internal/tools/tasknotify.go:103`).
*(re-measured 2026-10-03: `memory_sync_delta.go:586-600` was a comment block inside
that function rather than its definition, `:697` a payload field of the alarm, and
`tasknotify.go:81-86` the `notify.events` default/synonym comments — none of them the
alarm. Re-keyed to the symbols.)*
**The requirement reads:** a machine may settle *noise*; it may never settle a
person's work. The escape hatch is an alarm for a human to adjudicate.

### R4 — What has been delivered must actually travel, and the record must not be able to say otherwise
**Need:** the note is worthless without the article. Operator verbatim:
**"only DB-sync means nothing"** (`t-1788125937`, bug038); and when the machine
went quiet on this, he filed the diagnosis himself — *"why did HP never push its
finished branches to peer1? client leg silently skipped"* (`01a0aab1`,
briefed in full at `briefings/brief-peer-push-diagnosis.md`), then the repair
`t-1788252501` ("gopi client should push git branch to server and clean up").
**Evidence for the mechanism:** the client chain's own ordering
(`internal/tools/task_finish_cleanup.go:3-30` — push, then remove, then delete, the
client arm's branch documented at `:1173-1180`; the ordering *is* the safety
argument, and a **pre-merge gate** now (`finishCleanupPreMergeVerify`
`internal/tools/task_finish_premerge.go:227`)
precedes the push: fetch the peer's trunk, merge into the branch locally, rebuild
through the repo's declared contract, push only if both succeed, and ABSTAIN (push
and say so) when the peer is unreachable or nothing is measurable —
`task_finish_premerge.go`), the role gate, and the one branch-keeping exemption for
the trunk.
**Re-measured 2026-10-03 — both trailing line references were wrong, and one named
the wrong FILE.** The role gate is not in `task_finish_premerge.go` at all: it is
`internal/tools/task_finish_cleanup.go:1232` (`IsServerNode() || finishCleanupServerTarget() == ""`
choosing the server's recycle arm over the client chain); the old line-820
reference pointed into a 637-line file, which is why it read as past-EOF.
The trunk exemption is
`internal/tools/task_finish_cleanup.go:583` (`finishCleanupExemptBranchesResolved`,
the trunk set itself at `:584`,
whose built-in set is `{"master", "main"}`; reached through
`finishCleanupBranchExempt`, `internal/tools/task_finish_cleanup.go:625`).
**Acceptance, as the operator would test it:** after a finished client run, the
centre has the branch and the laptop has neither worktree nor branch; after a run
whose delivery failed, both are still there.

### R5 — One machine must be able to say which one it is, without looking around
**Need:** the agent's own first message must carry the answer.
**Evidence:** `t-bug041` is quoted inside the code as the reason the identity
exists as *data* rather than an inference — **"Bond guessed its node identity from
the remotes list because neither side's settings.json carried a role field; the
guess was right, but only by luck"** (`internal/config/node_role.go:12-17`); the
operator's observation row `b7b1735e-4900-4bb2-b0fb-f00d12a4295d` / `8a37eef4-a671-4d7c-b062-454ecf60261f`.
**The requirement reads:** identity is DECLARED, never discovered — a value in the
settings file (with an environment override, `RoleEnv` `internal/config/node_role.go:33`),
resolved once (`ResolveNodeRole` `:52`), and shipped to every child
(`NodeIdentityBriefing` `:100`, wired at `cmd/agent/cli_tasks.go:2303`) and to the
operator at startup (`NodeIdentityLine` `internal/config/node_role.go:80`, printed at
`cmd/agent/main.go:1243`).

### R6 — The three channels must be separate, because their units are different
**Need:** task rows are *queue state* (they must reconcile, and a stale one is a
worker theft); asset rows are *knowledge* (they are additive, and a reconciliation
rule over lessons would be nonsense); branches are *code* (their unit is a commit
graph and the answer is version control's, not mine). Conflating them gives one
setting that must be right for three different topologies.
**Evidence:** `t-1788307190` (the operator's own framing: asset rows are
"UNIQUE-only, low-hanging-fruit … NO conflict resolution — conflicts are totally
complicated and deliberately OUT OF SCOPE"); the decoupling was then hardened in
the field — `t-1788362947` (the asset channel needs its own whitelist) with the
reason recorded at the site: single-machine task mode clears the task peer list,
and asset sync must survive that (`internal/tools/memory_sync_assets.go:180-184`);
the gate is its own key (the asset channel's enforcement is
`syncAssetsPeerWhitelisted`, `internal/tools/memory_sync_assets.go:524`, over
`config.MemoryPeersSettingsKey` — `internal/config/config.go:807`; the task channel's
is `RequireDeclaredPeer` over `config.SyncPeersSettingsKey` —
`internal/config/config.go:750`. The re-measurement on 2026-10-03 corrected all
three citations here: `memory_sync_assets.go:188` is a `PushedRows` struct field and
`internal/config/config.go:362`/`internal/config/config.go:404` are the
`SyncPeers`/`MemoryPeers` field comments, not the per-channel enforcement).
**Measured today, live — and the measurement corrected my own reading of the
declaration.** The two whitelists are separately enforced: on the client,
`task sync --peer peer1` is accepted and `task sync --peer hp` is refused
(`hp` is not in `sync_peers`), while `memory sync --peer hp` is accepted and
`memory sync --peer peer1` is refused. On the centre the mirror image holds:
`task sync --peer hp` refused (its `sync_peers` is **empty**), `memory sync --peer hp`
accepted. So R6's separation is real and holds in both directions. What the same
measurement exposed is a **different** defect, filed as G10: the client's asset
whitelist names a peer whose address, resolved *from the client*, is a third machine
that is not the centre and not itself, whereas the same alias resolved *from the
centre* correctly names the client. The declaration is not symmetric, and nothing
validates it.

### R7 — Correctness of the whole channel is the operator's to check, on demand
**Need:** "are my two machines actually one?" must be a question I can ask.
**Evidence:** `t-1788243017` (the operator's own framing of the anchor premise as
**common sense**, and PA's failure to foresee it, in his words
**"common sense——锚点是同步的起始时间线，锚点之前两侧的行必须 identical"**; the
deliverable he demanded was the reconciliation command *and* the automatic
guard); `t-1788330598` (the operator pasting the guard's own refusal back at us as
"this seems to be a bug" — the alarm working in the field); the commands exist at
`syncErrorPrefix`'s own comment `cmd/agent/cli_tasks_sync.go:80-92` — the
double-prefix defect and its ONE-OWNER fix).
**The requirement reads:** the premise the whole protocol rests on must be
executable, and a violation must refuse the advance rather than corrupt the
comparison (`syncAnchorGuard` `internal/tools/memory_sync_reconcile.go:361`;
the commit wrapper is `guardSyncStateCommit` `internal/tools/memory_sync_reconcile.go:542`).

> **Re-measured 2026-10-03:** the 2026-09-17 text cited
> `memory_sync_reconcile.go:505` for this — that line is
> `// session: returns true when the NEXT session must be full (count+1 >= N,`, a
> comment inside `syncTickShouldRunFull`, which decides whether the NEXT session is
> full and has nothing to do with refusing an advance. The guard is the pair named
> above; the bare continuation in R11 and the row in §6.1 were re-keyed with it.

### R8 — The topology is declared, never discovered — and a machine may not treat itself as its own peer
**Need:** a machine must not silently start syncing into itself or into a machine
nobody declared.
**Evidence:** the refusal message is the operator's own doctrine, written into the
error text — *"sync topology is declared, never discovered"*
(`internal/tools/memory_sync_session.go:88-90`, `memory_sync_assets.go:195-196`,
`internal/config/node_role.go:12`). The whitelist is consulted on every session path
(`syncPeerSession` `internal/tools/memory_sync_session.go:72`, via `syncPeerDeclared`
`internal/tools/memory_sync_reconcile.go:332`) and on the two
server-side paths that derive a target from it (`GitSyncRepoRoot`
`internal/tools/task_sync_git.go:253`, `GitSyncPushLocalBranches`
`internal/tools/task_sync_git.go:275`,
`finishCleanupServerTarget` `internal/tools/task_finish_cleanup.go:2333`).
*(re-measured 2026-10-03: `memory_sync_session.go:88-90` and
`memory_sync_assets.go:195-196` are struct-field comments, `node_role.go:18-19` the
`const (` + `NodeRoleServer` opening of the role-block — the doctrine sentence lives in
the type's doc comment at `:12-15`; `memory_sync_session.go:75` is a comment inside
`syncPeerSession` and `memory_sync_reconcile.go:302` a `staged.Close()` line, and
`task_sync_git.go:257`/`:292` are a `}` and a `return`, with
`task_finish_cleanup.go:1046` a marker inside `FinishCleanupSkipIsReportable`. All
re-keyed to the symbols above.)*
**Measured today:** the self-check exists in exactly **three** places
(`IsServerNode`'s peer loop `internal/tools/memory_sync_delta.go:831-836`,
`GitSyncServerTarget`'s own-name skip `internal/tools/task_sync_git.go:455-458`, and —
by construction — the role resolution's second arm), and **not** in the asset channel or its CLI
(§6.2 G4).

### R9 — A degraded or unknown machine must fail toward the conservative side
**Need:** a machine that cannot prove it is the centre must not behave like one.
**Evidence:** the live incident is recorded in the commit itself —
`dd118f5d` *"fix(sync): IsServerNode — config role wins (bug044) + fail-safe client
default (N1): unknown host no longer claims server rights (live incident: HP
merged peer1 branches as self-declared server)"*; the comment at
the N1 comment inside `IsServerNode` (`internal/tools/memory_sync_delta.go:815`;
the trade is stated at `:824`) states the trade in the operator's
terms — **"Client overreach is cheap."**
**The requirement reads:** where the machine cannot know, it must do less.

### R10 — The channel must be bounded by what one person can hold
**Need (C5):** one person must be able to name what has to be kept in sync.
**Evidence:** the knobs are enumerated in one place and nowhere else
(`internal/config/settings.go:201`, the `EnvVars` catalog — the topology/pacing rows
at `:351-359`: role, target, task whitelist, asset
whitelist, provenance-name override, auto-sync pacing, full-replay cadence, git
cadence) — eight. The refusal to add a ninth machinery is itself recorded: bug048
was fixed by making the anchor premise checkable, not by adding a second anchor.
**Measured:** the channel is **three** transports × (manual verb + daemon pass)
and **two** identity values. `[opinion]` this is at the upper edge of C5, and §6.4
N1 is where I say so.

### R11 — Nothing may be lost while I am not looking
**Need:** the failure the whole protocol is built to prevent is the *silent* one.
**Evidence:** the operator's own bug reports are all shaped that way —
`t-1788330598`, `01a0aab1` ("client leg silently skipped"), `t-1788243017`
(single-sided rows "永久漏同步"), `t-1788065158` (a submitted bug row falling into
a hole on a remote machine). The design answer is the anchor guard (refuse, do not
advance: `guardSyncStateCommit` `internal/tools/memory_sync_reconcile.go:542`) plus
alarms that must be reachable
(`taskNotifyAlarmEvents` `internal/tools/tasknotify.go:161`, the alarm constants at
`:87`/`:93`/`:103`).

> **Re-measured 2026-10-03:** the 2026-09-17 bare `memory_sync_reconcile.go:505` and
> `tasknotify.go:76`/`:86` were re-keyed (the first is `syncTickShouldRunFull`'s
> comment; `tasknotify.go:76`/`:86` are the `notify.events` default and the
> terminal-push-failed constant's comment, one line off each).

---

## 5. Acceptance in user terms

Each statement is observable by the operator with a command, and each is written
so that a test author can tell a bug from allowed behaviour.

**A1 — the two ledgers converge.** After a finished piece of work on the client,
one sync session (or one daemon tick, if enabled) leaves the centre holding that
row in the same terminal condition, and the client holding every row the centre
sent. *Observable:* `gopi task diff --peer peer1` exits 0 with no differences.
*Not a bug:* a row whose condition differs while one side's session has not yet run
— the protocol is one-directional per session and never claimed simultaneity.
**Measured status of this acceptance clause today: FAILING** — 753 differences,
exit 1 (§6.6, finding 2). The clause is stated here as the requirement; the
measurement is the reason §6 is not a formality.

**A2 — one side is authoritative, and it is the declared centre.** A contradiction
in a person's work must surface to the operator and must NOT close the work.
*Observable:* the centre's ledger does not gain a `sync-resolved` tag on a row whose
payload is non-empty while the client holds it pending. *Not a bug:* a mirror/
auto-registered row being flattened with `sync-resolved` — that is the designed
behaviour for registration noise. **Measured status today: neither side rules** —
the client's keep-and-tag behaviour is by design, and the centre cannot take its own
branch (§G1), so the "surfaces to the operator" half rests entirely on an alarm that
has never been delivered (§G2).

**A3 — the article travels with the note.** After a client run finishes cleanly,
`git branch --list <the branch>` on the centre lists it and the client's worktree
directory and local branch are gone. *Not a bug:* a run whose worktree was dirty
keeping everything; *that* is the live-writer guard, and it is load-bearing.
*Measured status today: the client half runs and reports* (`push: 18 pushed
(idempotent)`), *but it delivers every branch it has rather than the one the task
produced* (§G9), *and the centre's receiving half is unreachable* (§G1).

**A4 — identity is declared, not inferred.** On a machine whose settings declare a
role, the startup line names it, and every dispatched child's first message carries
it. *Observable:* the line is absent when the role is undeclared — correct as a
refusal to guess, **but the requirement needs its companion**, which does not exist
today: the operator must be able to learn that a machine has declared nothing
(§G1). An absent line is the right behaviour for the machine and the wrong signal
for the person, and the code currently only serves the machine. **Measured status
today: the client declares `role: client` and prints it; the centre declares no role
and prints nothing** — the clause is satisfied on one machine and silently
unsatisfied on the other, and the only symptom is a line that is not there.

**A5 — declared topology is enforced on every path.** A peer not in the whitelist
is refused with a message naming the whitelist key, and a machine is never accepted
as its own sync peer on any path. *Not a bug:* the refusal itself, however
inconvenient — the message is the contract. **Measured status today: the first half
holds on every path, the second half does not exist** (§G4) — and the second half is
not even well-posed while one alias denotes two addresses (§G10), so the requirement
as stated needs the stronger form: *a peer name must denote one machine, and that
machine must be able to confirm its own name.*

**A6 — the premise is checkable and its violation is loud.** If the two machines'
pending sets differ, the anchor does not advance and the operator gets a message
naming both counts and the two commands that repair it. *Observable:* `sync
--status` still shows the old anchor after a refused session, and `task diff --peer`
exits non-zero with a repair line. **Measured status of this acceptance clause
today: FAILING.** The instrument is right (753 differences, exit 1, repair named)
and its verdict reaches nobody — see §6.6. `[opinion]` A6 as written is therefore
not merely unmet by the code; it is unmet *by delivery*, which is a different and
cheaper defect than a missing mechanism.

**A7 — knowledge travels without a ruling.** Unique asset rows appear on the other
machine; a row present on both is left alone and nothing is overwritten.
*Not a bug:* a conflict in an asset row leaving both copies untouched
(no resolution exists by design).

**A8 — the channels are independently declared.** Clearing the task peer list (the
single-machine shape) does not silently disable knowledge sync.

---

## 6. Gap analysis

### 6.1 Where the code SERVES the requirement

*(re-measured 2026-10-03 on `peer1` at master `fd126c45`: the S1/S3/S5/S7 rows'
anchors were read by CONTENT and corrected — `memory_sync_delta.go:186`/`:429`,
`memory_sync_reconcile.go:505`, `:796-809` and `tasknotify.go:81-86` were comment
or field lines, not the symbols the rows assert; the S6 row's the two
`internal/config/config.go:362`/`internal/config/config.go:404` readings
were the struct-field comments, not the keys. All four rows now name the symbol plus
its current line. S2/S4/S8/S9 carry no `.go` anchor of their own beyond the ones
re-keyed above.)*

| # | Requirement | Evidence it is served | Verdict |
|---|---|---|---|
| S1 | R1 one ledger | `SyncTaskExportDelta` `internal/tools/memory_sync_delta.go:187`/`SyncTaskImportDelta` `:431`; live: 2026 rows on the client carry `from-peer1`, and `task sync --status` answers with a real anchor and timestamp | **Served** |
| S2 | R4 the article travels | the client chain's ordered push→remove→delete plus the pre-merge gate (`internal/tools/task_finish_cleanup.go:3-30`) | **Served on the client (measured: `push: 18 pushed (idempotent)`); the centre's receiving half is unreachable — G1** |
| S3 | R7 the premise is executable | `task diff --peer` + the anchor guard (`syncAnchorGuard` `internal/tools/memory_sync_reconcile.go:361`, commit wrapper `guardSyncStateCommit` `:542`), and it fired in the field (`t-1788330598`) | **Mechanism served; the requirement is NOT met** — the instrument reports "the anchor premise is BROKEN" (753 differences, exit 1) and nothing runs it (§G3/G6) |
| S4 | R8 declared topology | refusal message on every session path; measured on both machines, both directions | **Served** |
| S5 | R9 conservative failure | the fail-safe default is a real incident fix with the trade-off stated at the site (`dd118f5d`, `IsServerNode` `internal/tools/memory_sync_delta.go:815`, the fail-safe `return` at `:838`) | **Served** |
| S6 | R6 channel separation | two whitelist keys, two enforcements (`config.SyncPeersSettingsKey`, `internal/config/config.go:750` / `config.MemoryPeersSettingsKey`, `internal/config/config.go:807`; enforced at `internal/tools/peer_topology.go:91` / `:107`), measured live | **Served** |
| S7 | R3 no fake-closing of real work | the mirror-noise distinction + the alarm (`syncRowIsMirrorNoise` `internal/tools/memory_sync_delta.go:728`, the blind-ruling block `:584-604`, `notifyLegacyConflict` `:688`, `notifyLedgerConflictHumanTask` `:763`; alarm registry `internal/tools/tasknotify.go:161`) | **Partly served — see G2** |
| S8 | R11 nothing lost silently, in the *ledger* sense | the anchor guard is correct and the alarm path exists | **NOT met today — G3/G6** |
| S9 | the claim broadcast (one task, one runner) | `RunClaimLockProtocol` + the no-peer gate | **Served on the client; off on the centre — G11** |

**Two rows that make R2/R3 concrete and measurable** (they are the reason the
server-side ruling is now legacy-only): the client holds 157 rows tagged
`conflict` (16 of them outside the merge-channel's own `merge-conflict`), the
centre 48; the `sync-resolved` tag appears 36× on the client and 22× on the centre,
and **both sides' most recent occurrence is the same morning** — client 2026-09-01
11:04:37, centre 2026-09-01 11:25:30. Nothing has been ruled since. `[inferred]`
the cause is that the role fail-safe landed that same day (`dd118f5d`, 2026-09-01
03:45) and the centre has been unable to satisfy the gate ever since (§G1); the
weakest link is that I correlated dates rather than bisecting the state of the
centre's `role` configuration on 08-31 (§8).

### 6.2 Where it FALLS SHORT

**G1 — the machine that is *supposed* to be authoritative does not know it is.**
This is the finding I would put in front of the operator first.
`GOPI.md:1002` Rule 28 states the division as a rule, and the client obeys it end
to end: the client's settings declare `role: client` and `sync_to: peer1`, and
every role-gated behaviour follows. **Measured on the centre (2026-09-17): its
settings file has no `role` key at all** (`python3 -c` over the live file: `"role"
in d → False`, `GOPI_NODE_ROLE` unset in the shell, absent from the login profile,
and absent from every daemon's environment — I read `/proc/<pid>/environ` of the
running `gopi merger` process: no `GOPI_NODE_ROLE`).

The consequence is not cosmetic, and the code is more broken than "the centre is
misconfigured". Both the task channel **and** the git channel resolve their server
behaviour through the same first arm (`IsServerNode`
`internal/tools/memory_sync_delta.go:815`, the declared-role arm at `:817-821`:
declared role wins). The legacy fallback below it is **dead code that can never
return true** — every path in it returns `false`: an unreadable hostname returns
`false` (`:796-799`), an unreadable peer list returns `false` (`:800-803`), a peer
list containing the machine's own name returns `false` ("my own name is in my peer
list → I am the client", `:804-808`), **the loop then falls through** to the last
line, and that last line is `return false` (`:809`, the N1 fail-safe). So the
effective predicate is `role == "server"` and nothing else: **on this codebase the
server role exists only as a declared string, and any machine that has not declared
it is a client, permanently.**

The live centre has declared nothing, so the server branch of the git pass never
runs there: I executed `gopi task push` on the centre and it answered
`git-client: skipped: no sync peer (no server to push to)` — **the centre takes the
client path and then skips it**, so the "fetch the peers' branches" half of the
channel exists in code and is unreachable in the live configuration.

Worse, this is *self-concealing*: the role line is omitted precisely when the role
is undeclared (`internal/config/node_role.go:100-104`, the doc comment: "Empty (no
line at all) when the role is undeclared"), so the machine that has drifted prints
**no** identity banner where the client prints one — the missing line is the only
symptom, and nothing counts it.
**Requirement violated:** R5 (identity must be declared, not inferred) and R9 (the
undeclared case must at least be visible). **Root:** `role` was added as a
migration (bug041: "briefing 说 check role=client 但无数据可读") and only the
client's side of the migration was persisted. **A reader of Rule 28 would assume
both machines are in the topology; one of them is only in it by inference.**

**G2 — the alarm that R3 depends on has no reachable subscriber.**
`ledger-conflict-human-task` is the operator-facing half of the bug053 fix: when a
real ticket hits a contradiction, the ruling is blocked and **this** is supposed to
bring it to a human (`notifyLedgerConflictHumanTask`
`internal/tools/memory_sync_delta.go:763`). It is
delivered only if the notify configuration subscribes to that exact string; the
default event set is `claimed,in_progress,completed,failed`
(`taskNotifyEventsDefault` `internal/tools/tasknotify.go:77`), and the longer list this
sentence used to attribute to a constant in the same file has **no constant behind
it anywhere in the tree**: `internal/tools/tasknotify.go:86` is a comment
("Operators subscribe via events (like `stale`).") — the six-event list is the
*subscribed* value in the operator's own configuration, read on `peer1` on
2026-10-03 as `claimed,in_progress,completed,failed,stale,build-failed`
(`~/.gopi/settings.json`, key `notify.events`). Neither includes it.
*(re-measured 2026-10-03: this paragraph's older form attributed the six-event list to
a constant at `tasknotify.go:86` — no such constant exists (the line is a comment), and
the list is the operator's subscribed value, which is what the sentence now says; the
alarm's own symbol is `notifyLedgerConflictHumanTask`.)*
The delivery side confirms the shape of the defect: the event rides `all` only
(`internal/tools/tasknotify.go:155-158`, the 2026-09-19 measurement that `EVENTS=all`
delivered none of the agent-side alarms), so an operator who subscribed to *everything* would get
it and an operator who took the ordinary list does not. The fallback is a stderr
line (`memsyncWarnf` `internal/tools/tasknotify.go:606`, fired for this alarm at
`internal/tools/memory_sync_delta.go:764`) that a daemon's log never reaches a human
through.
**Measured: the alarm has never been delivered** — the centre's notify log
(`/home/user/.gopi/notify/task-events.jsonl`, 49 MB, still growing) contains **0**
occurrences of the event name. **Requirement violated:** R3's escape hatch and R11
(the failure must not be silent). **This is the same class as the 2026-09-02
incident already recorded in the same file** (`internal/tools/tasknotify.go:261-262`:
a restart
dropped the command and "stale events silently stopped landing") — the fix made
that one durable and left this one unsubscribed.

**G3 — the protocol has no delete, and the two ledgers have already diverged by ~750 rows.**
Every path in the channel moves rows by `updated_at` comparison; there is no
tombstone, no delete propagation, and I found no `DELETE FROM tasks` in any sync
file (searched the whole sync family). Rows that the centre removes for any reason
therefore cannot reach the client. **Measured (2026-09-17), two independent
instruments agreeing:** I pulled the centre's full id set (1682 rows at first
measurement) and compared it against the client's 2427 — **717 client rows carry
`from-peer1` (i.e. they were once accepted as the centre's) and are absent from the
centre today**, 676 of them with no trace anywhere in the centre's own event
history, which is what distinguishes a delete from a row the centre never had; and
the shipped instrument (`gopi task diff --peer peer1`) reports **748 rows
missing-on-peer** against a client total of 2428. The two numbers differ because
they count different things — mine asks "is this id in the centre's table", the
shipped one asks "does the centre's catalog contain this row" — and their agreement
within ~4% is the strongest evidence here. The affected rows are real work records,
not litter — a sample span:
`t-1788124035` "config schema 文档单", `t-1788145430` "[测试] priority tags 推导验证单",
`t-1788252501` (the operator's own push/cleanup task), `t-1788207158` family
(auto-registered merge-backlog rows), spread across 2026-08 (575) and 2026-09 (173).
**Requirement violated:** R11, and it is the exact failure mode the operator wrote
bug048 about, in the mirror direction (his framing: the anchor premise is that
everything before it is identical on both sides).
**Honest weak link:** the protocol was *designed* forward-only, and one reading is
therefore "a deleted row is out of scope by construction" — but the second half of
that reading fails, because the deletion is invisible to the operator on the side
that still holds the row: there is no marker, no count, and no command that reports
it. `gopi task diff --peer` compares sets and would report these as one-sided rows —
**if** anyone ran it; nothing runs it on a schedule (the verb has exactly one call
site, the CLI dispatch of `tools.TaskDiffPeer` at `cmd/agent/cli_tasks_sync.go:547`; no
daemon pass invokes it).

**And the module's own instrument says so, live, today.** Executed on the client
against the real centre while writing this document:

```
diff peer1: 753 difference(s) — the anchor premise is BROKEN (local=2428 peer=1683 rows)
  missing-on-peer  t-1788053263-1
  … 748 such lines
repair: gopi task sync --full peer1
exit=1
```

That is not a hypothetical: the requirement R7's own acceptance (A6) is failing
right now on the operator's two machines, it has been failing for two weeks
(the missing rows' dates), the tool that detects it **exits 1**, and the only reason
he has not seen it is that the check is manual and the daily path never runs it.
Under C2 this is the strongest form of the finding: the failure is *detectable,
detected, and unread*. `gopi task sync --full peer1` is the repair the tool itself
prescribes, and it is one command.

**G4 — "declared, never discovered" is enforced for the wire but not for the target.**
The self-check ("my own name is not a peer") exists in three places
(`IsServerNode`'s peer loop `internal/tools/memory_sync_delta.go:831-836`,
`GitSyncServerTarget`'s own-name skip `internal/tools/task_sync_git.go:455-458`, and the
role arm) and **not** in the asset channel (its per-session struct,
`MemoryAssetSyncStats`, is DECLARED at `internal/tools/memory_sync_assets.go:181` —
built at `:366`, and the older alias `MemoryAssetImportStats` at `:256` — and carries
the session's counters and no identity check; `syncAssetsPeerWhitelisted`
`:524` compares the peer against the whitelist only) nor in its CLI. **Measured, live, on the client:**
`gopi memory sync --peer hp` — the alias its own asset whitelist declares — is
**accepted** and answers `dry-run hostb-Laptop→hp: push would carry 1447 row(s)`.
Whether that session is a self-session depends on what `hp` resolves to, and G10
shows that answer is not the same on the two machines; the point stands either way,
because **no code path asks the question at all**. The channel accepts a peer
because a list contains it, and never because it is a different machine.
**Requirement violated:** R8 in its second half. **Class:** the same "it works,
therefore it is correct" reasoning the whole identity mechanism was built to remove.

**G5 — old *and new* rows carry two different provenance names for one machine.**
`from-hp` and `from-hostb-Laptop` are both live on the centre today: 73 rows
under the alias and 217 under the hostname, with **zero** rows carrying both. The
mechanism is that the provenance name resolves as "environment override, else host
name" (`memory_sync_delta.go:99-103`, `:241-245`), so it changed the moment the
override was introduced (`53a965b8`, 2026-08-31 — "ssh argv shapes + GOPI_SYNC_NODE
identity") and installed (`/home/user/.profile:30`, `/home/user/.config/environment.d/gopi-node.conf`,
2026-09-01 03:54). The split is visible in the data: the alias name dominates
August (16+7 rows) and the hostname name dominates September (42+130 rows).
**No historical row is wrong** — a provenance token is a record of the channel at
the time — **but no rule says so**, and any question of the form "what did the
laptop send me" now has two answers. **Requirement weakened:** R5/R1's "one story";
a requirement that is only true by my not asking the question. The same split
exists in the *other* channel's provenance: the knowledge store on the centre holds
both `node:hp` and `node:hostb-Laptop` sources, while the client holds only
`node:peer1` (404 rows).

**G6 — the answer to "are my machines one?" is per-machine, manual, and unread — and today the answer is NO.**
`task sync --status` is a local read of the anchor table. Measured: the client
answers with its anchor for the centre; the centre answers with **nothing** (empty
output — it has no peer row for the client under the name it would sync with). So
the operator can ask the laptop about the centre and cannot ask the centre about
the laptop, and there is no verb that answers "what does the *other* machine think
it has of mine". The stronger half is G3's live measurement: the one instrument that
*does* compare the two machines (`task diff --peer`, exit 1, "the anchor premise is
**BROKEN**", 753 differences) has no scheduled caller and no surface that would put
its verdict in front of the operator — one call site, the CLI dispatch
(`cmd/agent/cli_tasks_sync.go:547` into `TaskDiffPeer`
`internal/tools/memory_sync_reconcile.go:92`). **Requirement violated:** R7 and R11 — the check
exists, is correct, is cheap, and is not run. `[opinion]` of everything in §6 this is
the cheapest to fix and the most valuable: a periodic call to a command that already
exists and already exits non-zero.

**G7 — the peer's repository path is one hard-coded assumption for a multi-repository platform.**
The delivered branch is pushed to a fixed path on the peer — `/home/user/work/GoPi` — in
three separate constants for the three mechanisms that need it
(`harvestPeerRepo` `internal/tools/memory_sync_task.go:1692` harvest, `gitSyncPeerRepo`
`internal/tools/task_sync_git.go:48` the git channel, `finishCleanupPeerRepo`
`internal/tools/task_finish_cleanup.go:79` the finish chain), with
the coupling acknowledged in a comment ("harvestPeerRepo / finishCleanupPeerRepo
contract — one path"). Meanwhile the platform's own configuration declares
**two** repositories (`repos` in the live settings: the gopi checkout with a build
command, and a second path with none — Rule 12's merge sweep made multi-repo a
first-class shape). **Requirement weakened:** R4 — "the article travels" is true
only for the one repository the constants were written for; for any other
declared repository the delivery has no target and no code path can notice.
`[inferred: the inference is that a per-repo target is required; the weakest link
is that no operator row asks for per-repo delivery, so this may be a
speculative generality rather than a gap — I flag it as `[opinion]` that it is a
gap at all.]`

**G8 — a machine's "who am I" answer and its "what is my address" answer come from two unrelated systems.**
Role resolution reads the settings file for `role`; the *target* it syncs toward
comes from the peer list, not from the `sync_to` key that was designed to hold it.
Measured: `sync_to` is declared on the client and read by exactly one consumer —
the startup banner (`NodeIdentityLine` `internal/config/node_role.go:80`, printed at
`cmd/agent/main.go:1243`);
no sync code path reads it (the session resolves its target from the whitelist:
`memory_sync_session.go:75`). So the operator has two ways to state the same fact
and one of them is decorative. **Class:** C2 — a value the operator can set that
does not change behaviour is worse than a missing feature, because it is a control
that is not connected.

**G9 — the delivery channel has no notion of "the branch this task produced", so delivery is a flood with a filter.**
The client's git pass publishes **every non-master local branch** to the peer
(`harvestPeerRepo` `internal/tools/memory_sync_task.go:1692` the harvest push-back's
target, and
`internal/tools/task_sync_git.go:275` `GitSyncPushLocalBranches`, which deliberately
excludes only the trunk). **Measured live today on the client: 18 branches pushed.
The centre's argument for the list is a filter, not a selection: a branch already
merged on the centre is skipped so it is not resurrected
(`TestPushbackSkippedMergedStaleBranchNotResurrected`), and the push is
idempotent.** The requirement gap is the shape of the *statement*, not the
mechanism: R4 says "what the task finished must travel", the implementation says
"everything I happen to have must travel, unless the centre can prove it already
has it". That inversion is exactly the 2026-09-02 incident recorded as
`t-1788332252` in the operator's framing — *"HP stale branches flood back to
peer1, resurrecting deleted branches"* (30+ branches re-appeared on the centre at
14:39 that day; filed and fixed through `t-1788357248`/`t-1788358709`). The fix
made the flood survivable; nothing made it a selection. **Consequence for the
centre:** its branch list is a function of the client's local checkout rather than
of the client's finished work, which is why the centre's own purge/merge sweeps
(B2) must carry the weight. `[inferred: that the operator would prefer a selection
is my reading of the bug report's framing; the fix he accepted was a filter, so
this may be exactly what he wanted.]`

**G10 — one peer declaration, two different addresses, and no validation.**
This is a finding produced by trying to test G4 and getting a surprise.
`memory_peers = ["hp"]` on the client, and the alias `hp` in
`~/.ssh/config` on the client points at **192.0.2.12** — a third address that is
neither the centre nor the client (the client is 192.0.2.5, the centre
192.0.2.6), and which from the centre is **unreachable** (`ping -c1 -W2
192.0.2.12` → 100% packet loss, whereas 192.0.2.5 answers in 1.1 ms). On the
**centre**, the same alias `hp` points at 192.0.2.5 — i.e. the client. So the
string `hp` means "the client" on one machine and "some other host" on the other,
and the asset channel's only check is membership of a list of strings
(`syncAssetsPeerWhitelisted` `internal/tools/memory_sync_assets.go:524` over
`RequireDeclaredAssetPeer` `internal/tools/peer_topology.go:107`). Nothing in the system
can detect this: the channel
does not verify that the name it is about to sync with is the machine that thinks
it has the same name (there is no handshake identity exchange — the session's
remote command is `ssh <peer> gopi task sync --serve-remote`,
`memory_sync_session.go:55`). **Requirement violated:** R8 (declared topology) and
R5 (identity must be declared). `[inferred]` the practical effect today is small —
the asset channel's last recorded session with `hp` is in the anchor table with a
recent timestamp, so *something* answered — but the requirement that a peer NAME
denotes a specific MACHINE is nowhere enforced, and on this host the two machines
disagree about what the name means. This is the same defect family as G5 (one
machine, two names) seen from the other side (one name, two machines).

**G11 — the centre's empty peer list silently switches off the cross-machine claim lock, so the two machines can each run the same task.**
The claim-lock protocol is the mechanism that stops two machines from executing one
task: before a run, the machine CAS-claims the row and broadcasts the claim to its
peers (`RunClaimLockProtocol` `internal/tools/memory_sync_claim.go:855`, the broadcast at
`StartClaimLockBroadcast` `:220`, the remote CAS at `syncClaimRemoteAcquire` `:604`).
It is gated on the same empty
peer list that G1 turns on: with no declared peer, `gopi task run` prints
`[claim-lock] single-node: no sync peers declared — local claim only (no remote
leg)` and skips the remote protocol entirely — zero ssh spawn, zero round-trip
(`cmd/agent/cli_tasks.go:1551-1552`; the predicate is `config.HasSyncPeers()`,
`internal/config/config.go:787`). That gate sits inside `runTaskRunOnce`
(`:1378`) — the **shared** run funnel for the plain `gopi task run` wrapper, the
serve daemon and the worker loop (the function's own doc comment names all three
callers), so on the centre the cross-machine lock is off for every execution path,
not just the interactive one. **The centre declares `sync_peers = []`,** so on
the centre every claim is local-only. Its consequence is exactly the failure the
protocol exists to prevent, and the row the operator would care about most is the
human/PA-dispatched one: a task filed on the centre and claimed there can also be
claimed on the client, and neither machine learns of the other's claim before it
starts writing. **Requirement violated:** R1's "one row, one story" and the whole
point of a cross-node ledger. **Why it has not bitten visibly:** the two machines
work different layers by Rule 28's division, so the overlap is rare — but the
protection is off by configuration, not by design, and it is off on the machine
whose peer list is empty rather than on the machine that opted out. `[inferred]`
the "single-node settings shape" gate is a legitimate contract for a genuinely
single-node install (its comment says so and cites the no-op acceptance path); the
finding is that a *two-node* topology which declares its peers asymmetrically
silently lands in that shape.

**G12 — the client's own close-out chain was never complete, and the evidence is the branch you are reading this on.**
bug041 is the row that REQUIRED the client chain ("client 侧标准清理链：push branch→delete
worktree→delete branch", `t-1788141480`, filed and later cancelled), and the
operator had to file the diagnosis *himself* when the machine went quiet on it
(`01a0aab1-587a-7ca3-ba98-4b66b11b8b90`, "diagnose: why did HP never push its finished
branches to peer1?"). The epic that was then raised to verify and complete the chain
is `37fe9c7e-fa30-45ff-ab20-6aa996e85628` ("EPIC: 验证并完善 gopi client 的
push-back+cleanup 链（bug038+bug041 域）"). **Measured at the time of writing: this
machine carries 25 local branches and 12 live worktrees** — including the close-out
repairs themselves (`feat/client-closeout-delivery`,
`fix/finish-cleanup-single-node-wording-050556`, `fix/merger-park-worktree`,
`fix/cmd-agent-suite-red`) and, notably, **`feat/req-b4-sync-nodes` with
`~/workspace/gopi-req-b4`** — this document's own branch and worktree. So the chain
described in R4/A3 as "push, then remove, then delete" is not what is happening on
the machine that holds the requirement: the push half runs (18 branches delivered),
the cleanup half leaves residue behind rather than removing it, and part of that
residue is the branches whose jobs were to fix the cleanup. **Requirement violated:**
R4 and C4. **Two honest qualifications:** the count is a snapshot of a machine that
also legitimately hosts parked work (Rule 29's "work in progress" and the
merge-backlog's deliberately parked branches are *not* this defect), and this
document's own worktree is one of the legitimate ones — I created it. So the finding
is not "12 worktrees is wrong"; it is that **nothing classifies the 12**, and the
only machine-side guard is a dirty-worktree check that fires per run.
`[inferred]` the cause is the per-run rather than bulk shape of the cleanup; the
classification of those 12 (finished vs parked vs actively in use) is the first step
of any fix and I did not do it.

### 6.3 Where it OVER-BUILDS

**O1 — the client's conflict path is deliberate, so this is NOT an over-build; it is
the correct branch that lost its counterpart.** Recorded here because it is the
tempting wrong reading of G1. The operator's ruling
(`memory_sync_delta.go:563-570`) keeps the contradiction visible on the client — the
`conflict` tag on 157 rows is the design working, not a fallback. The over-build is
elsewhere and in the opposite direction: the *server* half of the same ruling is
written, documented, tested (`TestSyncDeltaServerResolvesConflict`,
`TestSyncDeltaServerRulingBlocksHumanTicket`) and **unreachable in the live
topology**, because the one machine that should take that branch does not know it is
the server. `[opinion]` the honest label is therefore not "over-built code" but
"built code with no live caller" — a distinction that matters, because deleting it
would be wrong and configuring it is a one-line fix.

**O2 — three ways to configure one fact.** *(re-measured 2026-10-03 on `peer1` at
master `fd126c45`; the 2026-09-17 citations `internal/config/config.go:311`/`internal/config/config.go:612` were both wrong —
`:311` is an `ignore_peak_hours` comment and `:612` a `GOPI_TASK_NOTIFY_EVENTS`
comment — neither names a peer source. The finding stands; its anchors are replaced
by symbols below.)* A peer can be named by: the settings key, the corresponding
environment variable, and an explicit command-line flag, and the merge of all three
is **ONE function per channel** — `config.ResolveSyncPeers`
(`internal/config/config.go:763`) for the task-ledger channel, with its
knowledge-asset twin `config.ResolveMemoryPeers`
(`internal/config/config.go:870`) for the asset channel.

Each channel is individually justified, and the justification is in the precedence
order the resolver encodes (`ResolveSyncPeers`, `internal/config/config.go:763-777`):

1. **The CLI flag** — `cliPeers []string`, the resolver's FIRST arm
   (`internal/config/config.go:764-766`): a one-off session's own topology, for
   `task sync`/`task harvest` against a peer this node has not declared. It is the
   only arm that can *widen* the topology for one run without editing a file. The
   CLI passes its own list through `syncResolvePeers`
   (`cmd/agent/cli_tasks_sync.go:641`, called with the flag's split value at `:650`
   — the bare `--peers` spelling reaches the same call as `""` and resolves to the
   declared list on purpose).
2. **The environment variable** — `GOPI_SYNC_PEERS`, the second arm
   (`internal/config/config.go:767-770`): the test seam and the per-shell override.
   It is the arm that lets a test drive the real resolver and the arm an operator
   uses to re-point one invocation's topology without touching the file the daemon
   reads. Read here and nowhere else (Rule 2's single reader; the constant is
   `SyncPeersEnv`, `internal/config/config.go:755`).
3. **The settings key** — `"sync_peers"`, the third and last arm
   (`internal/config/config.go:771-776`, `SyncPeersSettingsKey`,
   `internal/config/config.go:750`): the DECLARED topology — the arm every
   non-CLI caller reads, and the only one that persists for the daemon, the worker
   and the merger. This is the arm Rule 2's "declared, never discovered" doctrine
   is about.

**Two measurement notes on this finding (2026-10-03, this run's own readings, not
carried from 2026-09-17).** (i) Every arm's candidates pass through the SAME
normalizer — `normalizeSyncPeers` (`internal/config/config.go:794`) for the task
channel, `normalizePeerList` (`:895`) for the asset channel — and every arm
propagates that normalizer's error, so which source supplied a bad value is visible
only in the label the asset channel passes (`"memory peer"`); the task channel's
error names `SyncPeersSettingsKey`. (ii) `cliPeers` widens the topology only for the
callers that pass it: the finish chain (`finishCleanupServerTarget`
`internal/tools/task_finish_cleanup.go:2333`) and the git channel
(`GitSyncServerTarget` `internal/tools/task_sync_git.go:440`) both resolve with
`nil` — the declared sources only — so the CLI arm reaches the *session* verbs and
not the two server-side paths that derive a target from the same list.

Together they are three chances to be wrong about one fact, per channel, for *two*
channels — and the asymmetry between the two channels is the sharper half of the
finding: **`ResolveMemoryPeers` (`internal/config/config.go:870`) has NO CLI arm at
all** (its signature takes no `cliPeers`), so the asset channel exposes two of the
three sources while the merge code that names the operator's mistake — the refusal
in `RequireDeclaredPeer`/`RequireDeclaredAssetPeer`
(`internal/tools/peer_topology.go:91`, `:107`) — is shared, vocabulary and all.

**Behaviour note (2026-10-03, this run's own finding — a note, NOT a defect claim):** the
env arm of both resolvers binds with `if raw := strings.TrimSpace(os.Getenv(...)); raw != ""`
(`internal/config/config.go:767` for the task channel, `:871` for the asset channel —
byte-identical guards), so the value tested for emptiness is the TRIMMED one: a
whitespace-only `GOPI_SYNC_PEERS`/`GOPI_MEMORY_PEERS` is skipped and the settings arm
decides. That is coherent; what is worth recording is that it is a *presence* test on a
normalized value, so the guard and the value it guards are not the same string — a
reader who assumes the env var "was set, therefore it wins" reaches the wrong answer for
a whitespace-only value. Named here as an observation; the code is not changed (this task
is a re-measurement).

**O3 — the anchor table accumulates names that no longer exist.**
`sync_state` is never pruned and never canonicalised. Measured: the client holds
three peer rows — the real peer, `workerpar-test` (last touched 2026-08-27, a test
harness's name that has **no producer anywhere in the tree**, verified by search),
and `probe`; the centre holds five, including a single-letter peer `x` with
anchor 0, `workerpar-test`, and **both** `hp` and `hostb-Laptop` — the G5 split
persisted a second time, in the table that means "this is who I sync with".
**Requirement weakened:** nothing asks for pruning, and nothing needs it *except*
the operator's ability to read the table as the truth about the topology (§2.4).

### 6.4 Where the requirement was NEVER DECIDED

**N1 — how many peers is the topology allowed to have?**
The whitelist is a comma-separated list and the code loops over it
(`normalizeSyncPeers` `internal/config/config.go:794` splits the comma list;
`MemoryAssetsPeerSessionMode` `internal/tools/memory_sync_assets.go:328` accepts any of
them; the `task diff` help at `cmd/agent/cli_tasks_sync.go:175-186` requires a `--peer`
to be DECLARED), so the design admits N machines; the finish chain takes `peers[0]` and
calls it the server (`finishCleanupServerTarget` `internal/tools/task_finish_cleanup.go:2333`,
`peers[0]` validated at `:2340`), and the git channel's client pass takes the first
declared peer that is not this node (`GitSyncServerTarget` `internal/tools/task_sync_git.go:440`).
Rule 28 describes exactly two. **No source
states whether the >1 case is supported, undefined, or forbidden** — and the
operator has a third machine in the picture already (the platform treats "two
hosts" as the rule *and* the migration procedure in Rule 28 talks about "the other
host", singular). `[opinion]` this is the single most likely place for a silent
wrong answer if a third machine is ever added by editing one settings file.

**N2 — what is a machine's *name*: the alias or the host name?**
G4/G5/G10 are three symptoms of one undecided question. The code uses the host name
(with an override) for provenance and the alias for addressing, and nothing declares
that these are the same machine — worse, G10 measures a case where one alias denotes
two different addresses on the two machines. Measured consequences: one machine
appears under two names in both provenance and the anchor table, and one name
denotes two machines. **The requirement was never stated**; the code made a choice
per call site, and the ssh configuration made a different one.

**N3 — who is allowed to delete a row, and what happens downstream?**
G3 measures the result. The forward-only design is deliberate (the tombstone
machinery was never written), but I found **no** decision record saying "deletion is
out of scope, and here is what the other side will think" — no operator row, no
briefing, no comment at the delete sites. `[opinion]` the honest reading is that
deletion was never considered rather than decided against, and the data now shows
the cost (676 rows).

**N4 — is knowledge sync supposed to be automatic?**
The task channel has a daemon pass and a pacing knob (`GOPI_SYNC_INTERVAL`,
`cli_tasks_sync.go:315`); the asset channel has a verb and **no pass** — I searched
the daemon's passes and found no asset sync (`cmd/agent/cli_tasks.go`,
`cli_serve*`). **Nobody decided that knowledge sync is manual while work sync is
automatic**; it is a consequence of the order the two features were built
(`t-1788307190` came after M4.13). `[inferred: the weakest link is that the operator
may hold exactly this opinion deliberately — "low-hanging fruit only" — in which
case the gap is that nothing says so where the next reader will look.]`

### 6.5 The C1–C5 check, with the conflicts filed as findings

| Constraint | Verdict for this module |
|---|---|
| **C1** one binary, zero deps, no plugins | **Holds.** The channels are built from the standard library plus the version-control binary the project already requires; the transport is a direct secure-shell call (`syncSSHExchange` `internal/tools/memory_sync_session.go:173`,
the `ssh` exec at `:183-195`), and the delta encoding is the project's own patch
struct, not a protocol library. The single direct dependency (`modernizr`-style pure-Go SQLite) is pre-existing and shared with the ledger. No finding. |
| **C2** the operator controls and can observe everything | **Conflict → G2, G6, G8, G11, and the live A6 failure.** An alarm with no subscriber (G2), a check that is one-sided and unscheduled *while the premise it checks is broken* (G6 + the 753-difference measurement), a setting that is decorative (G8), and a locking mechanism switched off by an empty list the operator does not consider a topology declaration (G11) are each "something he cannot observe or reverse". Filed, not softened. |
| **C3** a daemon must be simple | **Conflict → O1, O2, and the count of knobs in R10.** The channel adds a pass to the daemon whose behaviour depends on eight knobs across two channels and two identity values. A single daemon tick now has to get the *order* right (git before ledger — `cli_tasks_sync_git.go:90-98`) as well as the timing. `[opinion]` the order is justified by the code's own comment and is the one part I would keep; the knob count is the part I would cut. |
| **C4** the agent cleans up after itself | **Holds, with the delivery half as the load-bearing part.** Measured live and this morning: the client's push/pull pass runs and reports `push: 16 pushed (idempotent)` then a fast-forward of the trunk, i.e. the cleanup path is real and exercising. The known 2026-09-16 defect (a dirty-worktree guard blocking the delivery) is **fixed in the live binary** and its fix is not in the trunk yet (see §7). |
| **C5** a small team, one person can run it | **Tension → O2 and N1.** Two channels × three configuration sources × two identity names is the module's C5 cost. `[opinion]` it is still within reach — one person can hold "two machines, two whitelists, one name override" — but only because the second machine is the only peer; N1 is where that stops being true. |

### 6.6 The findings the operator does not already know

**Finding 1 — the centre declares none of the topology, and every consequence of
that declaration is silently off — including the lock that prevents the two machines
from doing the same work twice.** Stated as one line: `GOPI.md:1002` Rule 28 declares peer1 the server, the
client's settings declare the matching client role and obey it end to end, and the
centre's settings declare **neither `role` nor any peer** — the role's absence is
caught by a fail-safe that returns "not the server" on every path
(`IsServerNode` `internal/tools/memory_sync_delta.go:815`, fail-safe `return false` at `:838`),
so the centre executes the *client* git pass and skips
it ("no sync peer"), while the server pass — the arm that would fetch the client's
delivered branches — is unreachable on the only machine that is supposed to run it.

**Why these are new to the operator, together:** he stated the division as a rule
and the client side is visibly correct, so every test of the topology he could run
from the laptop passes. The defect is only visible from the centre, in a line that
the code deliberately does not print when the role is undeclared
(`NodeIdentityLine` `internal/config/node_role.go:80` returns no line at all when the
role is undeclared; `NodeIdentityBriefing` `:100`), and its second-order effect is a
*silently different* git
behaviour rather than an error. It is the same class of defect as bug041 (identity
by inference) with the inference moved into the machine the operator never inspects,
and it directly undercuts R4/R11. The same absent declaration switches off the
cross-machine claim lock (G11) — the mechanism that stops both machines from
*executing* one task — which is the requirement's own first line ("one ledger, one
story") failing at the point where it costs a duplicated run rather than a
duplicated row.
Finding 2 is the same class one level up: the
*summary* of the two-machine relationship is computed correctly by a shipped command
and delivered to no one, so the operator's mental model ("one system") is maintained
by his not asking — and the moment he asks, the tool answers with its own word:
BROKEN.

**Finding 2 (the bigger one) — the two machines are, right now, not one ledger, and
the check that says so has no reader.** The module's own reconciliation instrument,
run on the client against the real centre while writing this document, answers
`diff peer1: 753 difference(s) — the anchor premise is BROKEN (local=2428 peer=1683
rows)` and exits **1**, listing 748 rows the client holds that the centre does not
(§G3). The check is correct, it names its own repair (`gopi task sync --full
peer1`), and nothing runs it: its only call site is the CLI dispatch. So the
acceptance clause A6 in this very document — the premise is checkable and its
violation is loud — is **false in the live system**, not because the instrument is
missing but because detection without delivery is not loud. The operator's own
framing of this premise was that it is *common sense*
(`t-1788243017`), and it has been broken, undetected and unread, for two weeks.

**Finding 3 — the channel's names are not anchored to machines, in either direction.**
One machine already appears under two names in the data (`from-hp` 73 rows vs
`from-hostb-Laptop` 217 rows on the centre, zero overlap; both names persist in
the centre's anchor table — G5), **and one name denotes two machines**: the alias
`hp` points at 192.0.2.12 from the client (a third address, unreachable from the
centre) and at 192.0.2.5 — the client — from the centre (G10). The asset channel's
entire validation of that declaration is membership of a list of strings
(`syncAssetsPeerWhitelisted` `internal/tools/memory_sync_assets.go:524`), and it accepts
the machine's own whitelisted name
without asking what it resolves to. **Why this is the requirement-level finding and
not a config typo:** R8 as written in the code says "declared, never discovered", and
a declaration that means different things on the two machines is *discovery in
disguise* — the property the module claims to hold is the one it cannot check. The
repair is not a corrected `~/.ssh/config`; it is that a peer declaration should name
a machine that can confirm its own name.

---

## 7. What this implies for tests

Existing tests only, named as they are, with the verdict the requirement changes —
or an explicit statement that none exists.

### 7.1 Tests whose verdict R2/R3 changes (the ruling must never close real work)

| Test | File | What it pins today | Verdict under this requirement |
|---|---|---|---|
| `TestSyncDeltaServerResolvesConflict` | `internal/tools/memory_sync_delta_test.go` | the server writes `completed` + `sync-resolved` and the client obeys | STILL CORRECT **as a server test** — but it must now run with the role *declared*, because G1 shows an undeclared machine is never the server. A variant with no role declared belongs alongside it, asserting the guard, not the ruling. |
| `TestSyncDeltaServerRulingBlocksHumanTicket` | same file (`:1100`) | on a real human ticket the ruling is blocked and `ledger-conflict-human-task` reaches the notify pipe (`:1101`) | CORRECT and load-bearing — but it proves the event reaches *the pipe the test installed*, not that any live configuration subscribes. Under G2 the verdict is **incomplete**: the test cannot fail when the operator's real event list omits the name. |
| `TestSyncDeltaEqualAnchorEcho` | same file (`:431`; `TestSyncDeltaConflictEqualAnchor` is its client-side companion, described at `:243`) | the tie-break at equal anchors, and that the echo is not re-exported | Unchanged in intent; **their role fixture is the thing to align** — these are the tests that were retro-fitted when the role became primary (`a26c4b8a`, `6f7aae38`), i.e. the code drifted and the tests followed. Under R5 the fixture should be the *declared* role in every case, so an undeclared machine can never pass a server assertion. |
| `TestSyncDeltaTerminalNeverRegresses` | same file (`:540`) | a terminal condition is not regressed by a stale patch | Unchanged, and it is the right guard for R1. |
| `TestBidir2LiveConflictNeverClobbered` | `internal/tools/memory_sync_bidir2_e2e_test.go:160` | a live conflict is never clobbered end to end | CORRECT; the doc's R2 verdict strengthens it (the clobber must also be impossible *without* a declared role on either side). |

### 7.2 Tests whose verdict R4 changes (the article must travel)

| Test | File | Verdict |
|---|---|---|
| `TestSyncPushDeliveryOrderPushThenCleanup` | `internal/tools/task_finish_cleanup_test.go` | CORRECT and it is the acceptance test for A3 — the ordering **is** the requirement. |
| the dirty-worktree guard tests | same file, assertions at `:201` and `:917` ("no steps may run on a dirty worktree") | CORRECT under C4 — but see §7.5: the live binary's ordering fix is **not** in the trunk, so trunk and binary disagree about this test's subject. |
| `TestSyncGitPushBackBranchesOnly` / the client-leg tests | `internal/tools/task_sync_git_test.go`, `cmd/agent/cli_tasks_sync_git_test.go` | Under G1 these pin the **client** half only. The requirement (R4) is about the *centre* accepting, and no test asserts the server pass runs on a machine whose role is declared — `TestServeGitSyncSweepWithPeerAdvancesClock` (`TestServeGitSyncSweepWithPeerAdvancesClock` `cmd/agent/singlenode_serve_test.go:55`)
advances a clock, it does not prove a fetch happened. **The missing test is a server-role-declared variant of the git pass.** |

### 7.3 Tests whose verdict R7/R8 change (the premise and the topology)

| Test | File | Verdict |
|---|---|---|
| the anchor-guard / reconciliation tests | `internal/tools/memory_sync_reconcile_test.go`, `_cov_test.go`: `TestReconCompareBothDirections`, `TestAnchorGuardRefusesPendingMismatch`, `TestSyncAnchorGuardRefusesOnMismatchWithRepairHint`, `TestSyncAnchorGuardEqualPendingSetPasses`, `TestSyncAnchorGuardFailClosedOnUncomparablePeer`, `TestTaskDiffPeerComparesCatalogBothDirections` | CORRECT — `guardSyncStateCommit` (`internal/tools/memory_sync_reconcile.go:542`) is exactly R7's "refuse, do not advance", and these are the tests of a mechanism that **works**. Their verdict changes in a different way than usual: they are all green *while the live premise is broken*, because every one of them tests one call under a fixture, and none of them asks whether the call is ever made. Under R7/R11 the missing test is not another guard case — it is an assertion that the check has a scheduled caller. |
| `TestSyncStateForwardOnlyAnchors` | `internal/tools/memory_sync_delta_test.go:591` | **Changed by G3.** It asserts the forward-only behaviour as correct; under R11 it is correct *for a row that still exists* and incomplete for a deleted row. The missing case is "the peer deleted a row we already hold — what do we report?" |
| `TestMemoryAssetsPeerWhitelistDecoupled` | `internal/tools/memory_sync_assets_test.go:409` | CORRECT for R6/R8's first half. **Incomplete for R8's second half (G4/G10):** there is no test asserting that the machine a peer name resolves to is a different machine — and I verified live that the channel accepts a whitelisted name without asking. |
| `TestPeerFlagsRefuseOptionShapedAlias` / `TestDispatchRefusesOptionShapedNode` | `TestPeerFlagsRefuseOptionShapedAlias` `cmd/agent/peer_alias_guard_test.go:24`,
`TestDispatchRefusesOptionShapedNode` `:99` | CORRECT (SSH argument hygiene) and unrelated to identity. |
| `TestServeGitSyncSweepNoPeerIsPureNoOp` | `cmd/agent/singlenode_serve_test.go:27` | CORRECT as a single-machine contract — **and it is the test that hides G1**: on the centre, "no peer" is the state that makes the server pass skip, so a pure no-op is asserted as desirable where the requirement wants a fetch. |
| `TestRunClaimLockProtocolNoPeers` (`internal/tools/memory_sync_claim_test.go`), `TestStartClaimLockBroadcastEmptyPeers` | same file | CORRECT as single-machine contracts and **the tests that bless G11**: they assert that an empty peer list means local-claim-only, which is right for a genuinely single-node install and is also exactly what the live two-node topology now does on the centre. Under R1 the missing test is the same companion as everywhere else in this document — *a two-node declaration that is only half present must be reported, not silently reduced to the single-node shape.* |

### 7.4 Tests for identity itself (R5) — verdict: correct but scoped to the wrong machine

`internal/config/node_role_test.go` pins all eight behaviours:
`TestConfigRoleSyncToParsing:27`, `TestResolveNodeRoleSettings:41`,
`TestResolveNodeRoleEnvOverridesSettings:49`,
`TestResolveNodeRoleUnknownSpellingUndeclared:59`, `TestNodeIdentityLineServer:67`,
`TestNodeIdentityLineClientArrow:79`, `TestNodeIdentityUndeclaredEmpty:87`,
`TestNodeIdentityBriefingFormat:97`.
`TestNodeIdentityUndeclaredEmpty:87` is the one that **blesses the defect**: it
asserts that an undeclared role produces an empty line — the correct behaviour for
"do not guess", and the exact reason G1 has no symptom. `[opinion]` the requirement
does not change this test's verdict, but it *does* require a companion: an operator
who declared the topology in a rule must be able to find out that a machine did not
declare it. **That test does not exist.**

### 7.5 The live binary is ahead of the trunk on the delivery path

Measured this session: the running client binary (built 2026-09-17 00:28) performs a
push/pull git pass that pushes **16 branches (idempotent)** and fast-forwards the
trunk, and its client-chain fix (push before the dirty gate) is present in the
binary. The trunk however is at `17cec86b`, and the closeout fix commits
(`5c45a279`, `6e22d80b`, `7fa56d13`) exist only on remote-tracking branches — I
verified with `git branch -a --contains`. **Verdict for a test author:** do not
read this module's delivery behaviour off trunk. Until the closeout fix lands,
trunk's tests and the binary's behaviour disagree, and the disagreement is exactly
on R4's acceptance (A3).

---

## 8. Confidence and weakest links

### Per-claim confidence

| Claim | Confidence | Basis |
|---|---|---|
| the boundary, and that `internal/sync` does not exist | **high** | directory census + file inventory |
| R1/R2/R4/R5/R7/R8/R9 exist as stated, with those citations | **high** | operator rows quoted verbatim, code lines read, one commit message read in full |
| S1–S6 ("served") | **high** | live measurement on both machines, not reading |
| **G1 (the centre declares no role; server pass unreachable there)** | **high** | settings file read on the centre, `GOPI_NODE_ROLE` absent from shell + profile + the running merger's `/proc/<pid>/environ`, `IsServerNode` fall-through read, and `task push` executed on the centre printing the client-skip line |
| **G2 (the alarm has no subscriber)** | **high** for "no subscriber in the live configuration" | live `notify.events` read on both machines + 0 occurrences in a 49 MB log that is still growing. **medium** for "it never fired anywhere else" — a different machine or an earlier configuration could have delivered it, and I did not search every log on every host |
| **G3 (676 rows absent from the centre)** | **high** for the count and the direction; **very high** that the premise is broken, because the module's own tool says so (`task diff --peer`, exit 1, 753 differences); **medium** for the *cause* | the set comparison is mechanical (1682 centre ids at first measurement vs 2427 client ids; 748 rows reported missing-on-peer by the shipped instrument; 717 tagged `from-peer1` and absent) — but "deleted" vs "archived out of the main table by a migration" is my inference; I did not read the centre's archive path. **This is the weakest link in the document**, and it is now the weakest link in the *system* too, because the difference is growing between my two measurements (1682 → 1683 centre rows in the minutes I spent writing) |
| **G11 (claim lock off on the centre)** | **high** | settings read on both machines + the gate read at `cmd/agent/cli_tasks.go:1551-1552` (the
`task diff --peer` flag's refusal) + `HasSyncPeers` read + the shared-funnel doc comment
naming all three callers. **`[inferred]` for the frequency**: the two hosts work different layers by Rule 28, so I have not observed a duplicated run — the claim is that the protection is off, not that it has already failed |
| G4 (asset channel accepts a self/ambiguous peer) | **high** | executed live, accepted, with the row count printed |
| **G10 (alias `hp` = 192.0.2.12 here, 192.0.2.5 there)** | **high** | `ssh -G hp` on both machines + `ping` from the centre (100% loss to .12, 1.1 ms to .5) + `hostname -I` on both. **`[inferred]` for the consequence only**: I did not run a full asset session to see it fail, and the anchor table shows a recent `hp` entry, so something did answer at some point |
| G5 (two provenance names for one machine) | **high** | counted on both machines, zero overlap, plus the override's install date |
| G7 (one hard-coded peer repo path) | **high** for the fact; **low** that it is a requirement gap | three constants read; `[opinion]` filed as such |
| G8 (`sync_to` is decorative) | **high** | single consumer found by exhaustive search |
| N1/N2/N3/N4 ("never decided") | **medium** | absence of evidence: I searched operator rows, GOPI.md and the devdoc tree; an operator row could exist that I did not match. N3 is the one I am most confident about, because the delete sites carry no comment |
| O1/O2/O3 ("over-build") | **medium–high** for the facts, **opinion** for their cost | counted knobs, counted `sync_state` rows, read the gate |

### Weakest links, in order

1. **G3's cause.** The 753-row divergence is measured *by the program itself*;
   whether it is delete-propagation failure or an archive path I did not find
   decides whether the finding is "the protocol has no delete" or "the protocol has
   no delete **and** the operator cannot see the ones that happened". The second is
   the one that matters and I have not proven it.
2. **G1's second-order effect.** I proved the centre takes the client git path and
   skips it; I did **not** prove that the server pass would have fetched anything
   useful on a day the client delivered a branch. That is a one-command experiment
   (`task sync --git` with the role declared on the centre) and it belongs in the
   follow-up, not in a claim here.
3. **G10's consequence.** The address disagreement is measured on both machines; I
   did not run a full asset session to observe a failure, and the anchor table's
   recent `hp` entry is evidence that something answered at some point. So "this
   breaks asset sync" is **not** what I am claiming — I claim only that the name
   does not denote one machine and nothing checks.
4. **The role history.** I read the fail-safe's stated rationale and the incident
   commit; I did not bisect when the *centre's* settings lost or never had a
   `role` key. So "the migration was only half-persisted" is inference.
5. **Whether the two provenance names harm anything today.** They are consistent
   per-row and no query depends on them; the finding is about the requirement being
   true only by accident, and an operator could reasonably call that pedantry.
6. **N1's reality.** The >1-peer question is a requirement gap only if a third
   machine is contemplated. Rule 28 mentions "cancelling the original row" when
   migrating work, which implies more than two rows, not more than two machines —
   so `[opinion]` this is a latent gap, not a live one.

### Marked inferences, collected

R2's date correlation (that the ruling stopped the day the role fail-safe landed);
R3's reading that "noise vs work" is a requirement rather than a bug fix; §3's
entry condition, ordering rationale and exit condition; G3's cause; G7's
"per-repo delivery is required"; N4's "the operator may hold the manual-asset
opinion deliberately"; G1's "the migration was only half-persisted"; and §6.3's
judgements of cost. Everything else in this document is a quoted row, a read line,
or an executed command — and every measurement in §6 was taken on 2026-09-17 between
04:35 and 05:00 local, **while the two machines were still live**, so the counts are
a snapshot of a moving system (the centre's row count advanced by one during
writing).

---

## What I could not determine

1. **Whether the centre ever had a `role` in its settings and lost it, or never had
   one.** No settings-file history is kept in the repository, and I did not search
   the ledger for the moment the client's role was added to see what the centre's
   was at that time.
2. **What actually removed the 717 rows the centre no longer has.** The centre's
   own `task_events` shows nothing for 676 of them, which means either they were
   never registered there or the removal took the event rows too. I did not find a
   delete path in the sync family, so the deletion happened somewhere in the
   ledger's own lifecycle — B1's territory, not mine, and I have flagged it rather
   than guessed.
3. **Whether `ledger-conflict-human-task` ever fired on any machine under any
   configuration.** It has not in the centre's 49 MB log; I did not enumerate the
   notify logs of every host or every retired home directory.
4. **Whether the server git pass, once the centre declares its role, actually
   fetches the client's delivered branches.** One command, not run, because running
   it would edit the centre's live settings — which is an operator decision, not
   mine.
5. **What the second declared repository (`repos` in the live settings, no build
   command) is for**, and therefore whether G7 is a real gap or a speculative
   generality. Rule 12 says an empty build is a valid pure merge target — so the
   entry is legal *and* undelivered for sync purposes; which of those matters is
   not stated anywhere I found.
6. **The intended peer cardinality** (N1). Rule 28 is written for two hosts; the
   code is written for N. Nothing decides which wins.
7. **Whether the `unknown-node` sentinel has ever actually been written.** 0 rows
   carry it on either machine today, and the gate test exists to keep the literal
   in one place — but "never used" and "used and since migrated away" are
   indistinguishable from where I stand.
