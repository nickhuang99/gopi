<!-- doc-class: historical -->
# Requirements — sync (client/server) & node identity: why my two machines are one system, and how each knows which one it is

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Module:** B4 of 12 (canonical id `M3` in `devdoc/requirements/00-module-map.md:138`) · **Series:** `01a0ab12` umbrella, module row `01a0ab3d-d541-7b98-98b6-b715334250ca`
**Base of record:** master `812f9a60` ("Merge branch 'feat/cwd-lifetime-recovery'"), read 2026-09-18 on host **peerbox**.
**Every `file:line` in this document is checked against `812f9a60`**; where a line number is quoted from another revision it says so.
**Sibling document audited:** `devdoc/requirements/04-sync-and-nodes.md` (commit `92399148`, pinned to `17cec86b`) — **not overwritten**; Appendix A is my audit of it.

**The evidence canon used here** (the series' evidence classes, `00-method-and-standards.md`
§6.2/§6.3): **E1** operator rows in the ledger (`source='human'`) · **E2** commit SHA ·
**E3** `file:line` · **E4** a `GOPI.md` rule · **E6** devdoc. **Full-row ids** are used
for operator quotes and **8-character prefixes** elsewhere, exactly as the ledger stores
them; a prefix is ambiguous by construction, and the resolved rows are: `t-1788548312`,
`t-1788125937`, `t-1788036628`, `t-1788093118`, `t-1788091028`, `t-1788307190`,
`t-1788362947`, `t-1788243017`, `t-1788065158`, `t-1788332252` (all `source=human`;
last one held by the client, absent from the centre),
`b7b1735e-4900-4bb2-b0fb-f00d12a4295d` / `8a37eef4-a671-4d7c-b062-454ecf60261f`
(`source=auto` rows recording the operator's observation of the agent's identity guess),
`01a0aab1-587a-7ca3-ba98-4b66b11b8b90` (completed), and the series rows
`01a0ab12-2537-7b89-9fe2-43d7c3933a2a` (umbrella),
`01a0ab12-35e7-71c2-a03a-ec98256a3c39` (this module's row, `in_progress`),
`01a0ab3d-d541-7b98-98b6-b715334250ca` (the sibling document's row, `completed`).

---

## Front matter — the boundary I chose

```
Boundary:      two machines must behave as one system: work and knowledge move
               between them, each machine can say which one it is, and the
               operator can tell whether the two are still in agreement.
In scope:      the task-ledger channel (delta session, anchor + its guard,
               harvest/pull, the claim broadcast, full reconciliation), the
               knowledge-asset channel, the git passenger channel (push-back,
               fetch/adoption, the close-out delivery leg), node identity and
               role resolution, the peer whitelists, and the operator-facing
               surfaces that expose them (`task sync`/`harvest`/`push`/`diff`,
               `memory sync`, the startup identity line, the per-child line).
               Files read: internal/tools/memory_sync_{delta,task,session,
               assets,reconcile,claim}.go, internal/tools/task_sync_git*.go,
               internal/tools/task_finish_cleanup.go,
               internal/tools/task_finish_{premerge,deliverysync}.go,
               internal/config/{config,node_role,settings}.go,
               cmd/agent/cli_tasks_sync{,_git}.go, cmd/agent/cli_memory_sync.go,
               cmd/agent/{cli_tasks.go,merger_tick.go,cli_tasks_sync_git.go}.
Out of scope:  merging a delivered branch and everything after it (B2,
               `02-merge-and-branches.md`); the row's own lifecycle semantics
               (B1, `01-task-ledger.md`); worktree creation/release (B3) except
               the ONE delivery gate inside it; notifications as a subsystem
               (B10) except whether the sync alarm is reachable.
Boundary why:  it is a need a user can state ("is my other machine up to date?")
               and it can be re-implemented without changing another module's
               need. It is NOT a directory: `internal/sync` does not exist
               (`ls -d internal/*/` → 43 packages, no sync), and the
               responsibility is spread over the files named above.
```

**Directory-vs-responsibility mismatches that a reader of this module must know**
(the umbrella's `X<n>`, §7.1 of `00-method-and-standards.md`): **X1** — the whole
channel lives inside `internal/tools`, the package that also holds the tool
implementations, the ledger, dispatch, merge and worktree lifecycle (106 production
files, 61,646 lines); **X4** — `cmd/agent/cli_*.go` is a *surface* of this module,
not a module.

---

## 1. What this is for

I work on two machines. One decides things and holds the shared history; the other is
where most of the writing happens. Both hold my work, and I expect them to behave as
**one system** rather than two copies that drift apart.

Three things must hold, and they are what this module is about.

**First, what I did on one machine shows up on the other**, without me carrying
anything by hand: a piece of work filed, started, finished or failed on either side
becomes visible on the other, and the two machines must not end up with *different
answers* about the same piece of work.

**Second, the finished article travels, not only the note about it.** A record that
says "done" while the actual work sits on the machine that produced it is worse than
no record — it is a record that lies.

**Third, each machine knows which one it is, and behaves accordingly.** They are not
interchangeable: one is the centre, the other reports to it. If the wrong one
believes it is the centre it will start settling questions that are not its to
settle, and it will do so silently, because from the inside the two machines look
identical.

**My own caveat, in my terms.** I do not need this to be instant and I do not need
the two machines to agree at every moment. What I need is that work which has moved
ends up in the same condition on both sides, that I can tell which side's version is
current, and that **nothing is lost quietly while I am not looking**. This module
exists to make "one system" *true* rather than true by my assumption.

---

## 2. Who is served

### 2.1 The operator (nick) — "I should not be the transfer mechanism"

Work must cross machines while I am asleep. What I will never accept in place of that:
being told "you need to run this on the other one too", or discovering days later that
a finished piece of work only ever existed on the machine that produced it. The need
is stated verbatim in my own row: **"only DB-sync means nothing"**
(`t-1788125937`, bug038 — quoted in `internal/tools/task_finish_cleanup.go:1184`
region and in the sibling document §R4) [E1].

But the sharpest statement of it is not about sync at all — it is about **whose
version of the record is the current one**. On 2026-09-04 I filed
`t-1788548312` ("current gopi repo cannot pull from peerbox", `source=human`,
`status=failed`) whose payload is the shell transcript of a machine whose trunk had
drifted from the centre's, in my own language: `git pull` announcing new branches it
had never seen. **That row exists in HP's ledger today and does not exist in peerbox's**
(measured this session: `t-1788548312` present on the client, absent from the centre)
[E1 + measurement]. The requirement it encodes is the one this module exists to meet,
and the machine it was filed *from* is no longer the machine that holds it.

### 2.2 The agent (Bond) — the actor that must not guess where it is

An agent session runs on one machine and must reason about the topology. In
2026-08-31 the operator observed a transcript in which the agent asked *"Wait, which
node am I?"* and answered itself by inspecting which remotes happened to be
configured — it guessed right, by luck (`b7b1735e-4900-4bb2-b0fb-f00d12a4295d`,
`8a37eef4-a671-4d7c-b062-454ecf60261f`) [E1]. The code records the diagnosis at the
site: *"Bond guessed its node identity from the remotes list because neither side's
settings.json carried a role field; the guess was right, but only by luck"*
(`internal/config/node_role.go:12-17`) [E3]. The need, distinct from the operator's:
an agent must be **told** which machine it is on, in its own first message.

### 2.3 The two machines, in two opposed roles

The only asymmetric actor pair in the system. The centre owns the system layer and may
rule on a contradiction; the reporting machine does the feature work and must never
rule (`GOPI.md:1035` Rule 28; the role table follows at `:1037-1040`) [E4].

### 2.4 The future maintainer — the actor the size constraint is about

That person must be able to answer "what must be identical on both machines, and what
must never cross?" without reading five `memory_sync_*.go` files. Today the answer is
spread over two whitelist keys, an environment override that names a machine for one
purpose but not another, a startup banner, a per-child briefing line, and a rule in
the project's rule file. §6 is where that becomes a finding rather than an invitation.

---

## 3. The workflow it sits in

**Entry condition.** One machine holds information the other does not: a row was
filed, claimed, moved or finished; a lesson was distilled; a branch was completed and
must reach the centre. `[inferred: this statement is my reading of the code — no
operator row states the entry condition in these words; weakest link: §8·1]`

**The three channels, and why they are separate.** Their units differ, and that is a
requirement rather than an accident (R6 below):

1. **the ledger channel** — task rows, moved as a timestamp-delta patch over a direct
   secure-shell call, one session per peer
   (`internal/tools/memory_sync_session.go`; the export predicate is
   `updated_at > anchor`, `internal/tools/memory_sync_delta.go:186`; the import is one
   transactional apply, `:429`);
2. **the knowledge channel** — distilled rows, moved "unique-only", with no conflict
   resolution at all (`internal/tools/memory_sync_assets.go:111` ff.; the operator's
   own framing `t-1788307190`: *"UNIQUE-only … NO conflict resolution — conflicts are
   totally complicated and deliberately OUT OF SCOPE"*) [E1];
3. **the git channel** — branches and the trunk, moved by ordinary version-control
   operations, which deliberately ride *before* the ledger so that a row's promised
   branch already exists when the row arrives
   (`internal/tools/task_sync_git.go`; the ordering comment is
   `cmd/agent/cli_tasks_sync_git.go:83-92`) [E3]. `[inferred: the ordering rationale
   is the code's own claim; I found no operator row demanding the order]`

**Steps, end to end, as they exist today on the centre (this host).**

- a run finishes → the close-out chain resolves **its own arm** from the declared
  topology, not from a role: `if IsServerNode() || finishCleanupServerTarget() == ""`
  → the server/single-node arm, whose entire step set is *worktree removal*
  (`internal/tools/task_finish_cleanup.go:877-878`, arm at `:897-905`; its own doc
  comment, `:902`: *"Step set: worktree remove only (no push — nothing to push to)"*);
- on the **client** the other arm runs: pre-merge the peer's trunk into the branch,
  rebuild through the repo's declared contract, then push, then remove the worktree,
  then delete the branch — in that order, because the order *is* the safety argument
  (`internal/tools/task_finish_cleanup.go:1-32`, the push at `:2232`, the pre-merge
  gate `internal/tools/task_finish_premerge.go:218-229`);
- the centre's merger then sees an ordinary branch and merges or parks it (B2);
- the ledger session runs — **but only if a peer is declared**: the pass short-circuits
  before role resolution when the whitelist is empty
  (`cmd/agent/cli_tasks_sync_git.go:58-60`) and the delta tick returns immediately when
  the peer list is empty (`cmd/agent/cli_tasks_sync.go:325-336`); the receiving side
  applies the patch in one transaction and only then advances the anchor
  (`internal/tools/memory_sync_delta.go:1089`).

**Exit condition.** The receiving machine's ledger contains the sender's rows; the
sender's anchor for that peer has advanced; the branch exists on the centre; and for
the knowledge channel the receiver holds the sender's unique rows. **The half of the
exit condition the protocol does not have**: nothing states what happens when a row
*ceases to exist* on one side (§6 G3–G4).

**What the operator can do by hand today.** `task sync --status` (a local read of the
anchor table), `task diff --peer` (a row-set comparison that exits non-zero when the
anchor premise is broken), `task sync --full` (the repair the tool itself prescribes),
`task push`, `memory sync --peer` (measured, this session, on the centre).

---

## 4. Why it exists — the requirement per need, traced

Each requirement carries its citation. `[opinion]` is my judgement, `[inferred]` is a
requirement reconstructed from code shape, with its weakest link named in §8.

### R1 — The two machines are one ledger, and a row's history is one story
**Need:** I file work on one machine and see it on the other; a row must not be able to
mean two different things on the two machines.
**Evidence:** `t-1788036628` (operator: client-side runs only ever wrote back to the
client's own database; remote SQLite is the wrong answer by design); `t-1788093118`
(the timestamp-delta design and the operator's own confirming wording in the same row —
verbatim: *"nick 确认 willing to implement"*, in a payload that opens
"背景（nick：simple script=sync timestamp-after-records，SyncML 协议核心简化"*); and
`t-1788091028` quotes him on the two-sided obligation — **"双端冲突都要处理 … 这是独立
并行开发要付的价（nick 原话：this is price we have to pay）"** [E1];
`internal/tools/memory_sync_delta.go:186` (export) and `:429` (one transactional
apply) [E3].

### R2 — Where the two machines disagree, one of them is allowed to decide — and it is not the one doing the work
**Need:** independent parallel work is the point; it will produce contradictions, and a
contradiction nobody may settle becomes a stuck row.
**Evidence:** the ruling is recorded inside the code as **nick 冲突语义定稿**
(`internal/tools/memory_sync_delta.go:563-570`, verbatim: the status "is a mere enum —
the real deliverable lives in the git branch … the SERVER side simply writes completed
… the CLIENT side keeps the legacy keep-row + 'conflict' tag … client never rules")
[E3]; the asymmetry is `GOPI.md:1035` Rule 28 [E4].
**Measured today (centre, 2026-09-18): neither side rules, and the centre cannot.**
`IsServerNode` (`internal/tools/memory_sync_delta.go:786-809`) has two arms: the
declared role, then a legacy hostname fallback — and **the fallback cannot return
true**: an unreadable hostname returns `false` (`:796-799`), an unreadable peer list
returns `false` (`:800-803`), a peer list containing the machine's own name returns
`false` (`:804-808`), and the final line is `return false` (`:809`). The centre's
settings declare no `role` at all (measured: `"role" in settings → False`,
`GOPI_SYNC_PEERS` empty in its daemon environment) [measurement], so on this host the
server arm of both the ledger and the git channel is unreachable.

### R3 — A ruling must never be able to close real work
**Need:** "the machine decided" must not be a way for a ticket a human wrote to
disappear.
**Evidence:** the operator's alarm is recorded in the code as the semantic fix
bug053/M4.16 (`internal/tools/memory_sync_delta.go:585-600`, `:694-705`): rows
`bug050/051/052` carried `sync-resolved` while the implementation never existed — the
ruling "FAKE-CLOSED real work" [E3]. The line is drawn at the row's *origin*: an
auto-registered mirror row may be flattened, a real ticket (source human/llm with a
non-empty payload) may not.

### R4 — What has been delivered must actually travel, and the record must not be able to say otherwise
**Need:** the note is worthless without the article. Operator verbatim: **"only DB-sync
means nothing"** (`t-1788125937`, bug038) [E1]; and when the machine went quiet on
this, he filed the diagnosis himself — *"why did HP never push its finished branches to
peerbox? client leg silently skipped"* (`01a0aab1`).
**Evidence for the mechanism:** the close-out chain's ordering
(`internal/tools/task_finish_cleanup.go:1-32`, the push at `:2232`), the **pre-merge
gate** that precedes the push (`internal/tools/task_finish_premerge.go:1-20`, `:218-229`:
fetch the peer's trunk, merge into the branch, rebuild through the repo's declared
build, push only if both succeed, ABSTAIN and say so when nothing is measurable), and
the close-out *server* arm's own delivery sync
(`internal/tools/task_finish_deliverysync.go:1-30`, created by `3c396080`
"feat(close-out): the delivery point syncs the branch with master before releasing it")
[E3/E2].
**Acceptance, as the operator would test it:** after a finished client run the centre
has the branch and the laptop has neither worktree nor branch; after a run whose
delivery failed, both are still there.

### R5 — One machine must be able to say which one it is, without looking around
**Need:** the agent's own first message must carry the answer.
**Evidence:** the bug041 diagnosis is quoted at the identity's definition
(`internal/config/node_role.go:10-17`) [E3]; the role is read from one place
(`RoleEnv = "GOPI_NODE_ROLE"`, `:33`) and shipped to every child
(`NodeIdentityBriefing`, `:98-108`, wired at `cmd/agent/cli_tasks.go:1573`) and to the
operator at startup (`cmd/agent/main.go:770`); the child line's contract is explicit:
*"Empty (no line at all) when the role is undeclared"* (`internal/config/node_role.go:94-96`).

### R6 — The three channels must be separate, because their units are different
**Need:** task rows are queue state (they must reconcile; a stale one is a worker
theft); knowledge rows are additive (a reconciliation rule over lessons would be
nonsense); branches are code (their unit is a commit graph, and the answer is version
control's, not mine). Conflating them gives one setting that must be right for three
topologies.
**Evidence:** `t-1788307190` (operator's own framing, quoted in R1's row);
`t-1788362947` (the asset channel needs its own whitelist) with the reason recorded at
the site — single-node task mode clears the task peer list and asset sync must survive
that (`internal/config/config.go:596-603`) [E1/E3].
**Measured today, live, in both directions.** On the **centre**: `memory_peers = ["hp"]`
while `sync_peers = []`, so `memory sync --peer hp` is declared and task sync has no
peer at all [measurement]. On the **client**: `sync_peers` names the centre and
`memory_peers` names itself (the G-family defect, §6 G5).

### R7 — Correctness of the whole channel is the operator's to check, on demand
**Need:** "are my two machines actually one?" must be a question I can ask.
**Evidence:** `t-1788243017`, the operator's framing of the anchor premise as **common
sense**, verbatim: **"common sense——锚点是同步的起始时间线，锚点之前两侧的行必须
identical"**, with the demanded deliverable being the reconciliation command *and* the
automatic guard [E1]; the guard refuses rather than corrupts the comparison
(`internal/tools/memory_sync_reconcile.go:505`) [E3].

### R8 — The topology is declared, never discovered — and a machine may not treat itself as its own peer
**Need:** a machine must not silently sync into itself, or into a machine nobody
declared.
**Evidence:** the doctrine is written into the refusal text — *"declared topology,
never guessed"* (`internal/config/node_role.go:13-14`), the whitelist is consulted on
every session path (`internal/tools/memory_sync_assets.go:185-197`, `memory_sync_session.go`,
`internal/tools/memory_sync_reconcile.go`), and `ResolveSyncPeers`' own comment fixes the precedence
and the failure posture (`internal/config/config.go:549-583`: CLI > env > settings,
malformed values are errors, empty means unconfigured and manual `--peer` names are
then refused) [E3].

### R9 — A degraded or unknown machine must fail toward the conservative side
**Need:** a machine that cannot prove it is the centre must not behave like one.
**Evidence:** the incident is recorded in the commit itself — `dd118f5d`
*"fix(sync): IsServerNode — config role wins (bug044) + fail-safe client default (N1):
unknown host no longer claims server rights (live incident: HP merged peerbox branches as
self-declared server)"* [E2]; the trade is stated at the site: **"Client overreach is
cheap."** (`internal/tools/memory_sync_delta.go:791-795`).

### R10 — The channel must be bounded by what one person can hold
**Need (the size constraint, C5):** one person must be able to name what has to be kept
in sync.
**Evidence:** the knobs are enumerated in one place (`internal/config/settings.go:196-209`:
role, target, task whitelist, asset whitelist, provenance-name override, auto-sync
pacing, git cadence, full-replay cadence) — eight, and each has exactly one reader
(Rule 2) [E3]. The refusal to add a ninth machinery is itself recorded: the anchor
premise was made *checkable* rather than given a second anchor (bug048).

### R11 — Nothing may be lost while I am not looking
**Need:** the failure the whole protocol is built to prevent is the *silent* one.
**Evidence:** the operator's own bug reports are all shaped that way —
`t-1788243017` (single-sided rows "永久漏同步"), `01a0aab1` ("client leg silently
skipped"), `t-1788065158` (a submitted bug row falling into a hole on a remote
machine) [E1]. The design answer is the anchor guard (refuse, do not advance:
`internal/tools/memory_sync_reconcile.go:505`) plus alarms that must be reachable
(`internal/tools/tasknotify.go:86`, `:155-159`).

### R12 — The centre's own remedy for divergence is "pull the centre's version", and that is a requirement about the mechanism, not only about the data
**Need (new in this document):** when the two machines have diverged, the operator needs
one action that makes the *machine he is sitting at* hold the centre's truth.
**Evidence:** the operator's own row, filed 2026-09-04 from the client, title in his
words — **"current gopi repo cannot pull from peerbox"** (`t-1788548312`,
`source=human`, `status=failed`, payload = the failing `git pull` transcript) [E1/E3].
It exists **on the client and not on the centre** (measured this session) — the row
that asks for the centre's truth is itself held only by the machine that lacks it.
**The requirement reads:** a machine that cannot reach the centre's state must be able
to say so, and the divergence that made it unable must be visible *before* the operator
sits down at it. Corruption of the remedy is that today the record of the complaint is
a row in the divergent set.

---

## 5. Acceptance in user terms

Each statement is observable with a command, and written so a test author can tell a
bug from allowed behaviour. The **measured status** column is this session
(2026-09-18, centre = peerbox) — a failing clause is stated as the requirement and the
measurement is why §6 is not a formality.

| # | Requirement | The user's test | Measured status today |
|---|---|---|---|
| **A1** | R1 one ledger | `gopi task diff --peer hp` exits 0 with no differences after one session. *Not a bug:* a row differing while one side's session has not yet run — the protocol is one-directional per session and never claimed simultaneity. | **FAILING — 952 differences, exit 1** (`local=1908 peer=2468`) |
| **A2** | R2/R3 one side is authoritative and it does not close real work | the centre's ledger gains no `sync-resolved` on a row with a non-empty human payload while the client holds it pending; a contradiction surfaces to the operator | **Not met** — the client's keep-and-tag is by design, the centre cannot take its own branch (R2), and the operator-facing alarm has no subscriber (§6 G2) |
| **A3** | R4 the article travels | after a clean client finish, `git branch --list <branch>` on the centre lists it and the client's worktree and branch are gone; after a failed delivery both remain | **Not met on the centre** — with no peer declared the close-out takes the server arm whose whole step set is worktree removal (`internal/tools/task_finish_cleanup.go:877-878`, `:897-905`); the client arm exists in trunk and runs on the client |
| **A4** | R5 identity is declared | on a machine whose settings declare a role, the startup line names it and every dispatched child's first message carries it | **Half met** — the client declares and prints; the centre declares nothing and therefore prints **nothing** (the line is deliberately empty when undeclared, `internal/config/node_role.go:94-96`), so the only symptom of a machine outside the declared topology is an absence |
| **A5** | R8 declared topology on every path | a peer not in the whitelist is refused naming the whitelist key; a machine is never accepted as its own sync peer | **First half holds** (both channels enforce membership, `internal/tools/memory_sync_assets.go:185-197`, `internal/config/config.go:549-583`); **second half absent** — no path resolves what a peer name *is* (§6 G4) |
| **A6** | R7/R11 the premise is checkable and its violation is loud | after a refused session `sync --status` shows the old anchor and the operator was told | **Instrument right, delivery absent** — `task diff --peer` answers `952 difference(s) — the anchor premise is BROKEN` and exits **1**; its only caller is the CLI dispatch (§6 G3) |
| **A7** | R6 knowledge travels without a ruling | unique asset rows appear on the other machine; a row present on both is left alone | **Mechanism met, cadence not** — both stores carry the other's rows, and `memory sync` has exactly one caller (the CLI: `cmd/agent/cli_memory_sync.go:157`), so knowledge moves only when typed |
| **A8** | R6 channels are independently declared | clearing the task whitelist does not disable knowledge sync | **Met** — two keys, two readers (`internal/config/config.go:549-583` for task, `internal/config/config.go:596-636` for assets) |
| **A9** | R12 the remedy is available where the divergence is felt | a machine that cannot reach the centre's state can say so before the operator sits down at it | **Not met** — the row that reported this (`t-1788548312`) is itself held only by the divergent machine |

---

## 6. Gap analysis

### 6.1 Where the code SERVES the requirement

| # | Requirement | Evidence it is served | Verdict |
|---|---|---|---|
| S1 | R1 one ledger | `internal/tools/memory_sync_delta.go:186`/`:429`; one session per peer; anchors per peer | **Served as a mechanism** — the instrument works and says the data has diverged (A1) |
| S2 | R2 the ruling exists and is unreachable only by configuration | the ruling is written, documented and tested (`TestSyncDeltaServerResolvesConflict`, `TestSyncDeltaServerRulingBlocksHumanTicket`) | **Built, no live caller on the centre** |
| S3 | R3 no fake-closing of real work | the mirror-vs-real-ticket distinction (`internal/tools/memory_sync_delta.go:585-600`, `:694-705`) + the alarm path | **Partly served — the escape hatch has no subscriber (G2)** |
| S4 | R4 the article travels | the client arm's ordered pre-merge → push → remove → delete (`internal/tools/task_finish_cleanup.go:1-32`, `:2232`), `internal/tools/task_finish_premerge.go:218-229`, and the server arm's delivery sync (`task_finish_deliverysync.go`) | **Served in the code on both arms; reached only on the client (G1)** |
| S5 | R5 identity is declared, not inferred | `internal/config/node_role.go:10-17`, one env reader (`:33`), child briefing (`:98-108`), startup line (`cmd/agent/main.go:770`) | **Served where declared; the undeclared case is silent (A4)** |
| S6 | R6 the three channels are separate | two whitelist keys with independent readers; measured live on both machines | **Served** |
| S7 | R7 the premise is executable | `task diff --peer` + the anchor guard (`internal/tools/memory_sync_reconcile.go:505`) | **Mechanism served; the requirement is not met** — it reports BROKEN and exits 1 |
| S8 | R8 declared topology for the wire | refusal text names the key; every session path validates membership | **Served for the wire** |
| S9 | R9 conservative failure | the fail-safe is a real incident fix with the trade stated at the site (`dd118f5d`, `internal/tools/memory_sync_delta.go:791-795`) | **Served** |
| S10 | R10 bounded by one person | eight knobs, one reader each (`internal/config/settings.go:196-209`) | **Served at the upper edge of the size constraint (§6.5)** |

### 6.2 Where it FALLS SHORT

**G1 — the machine that is supposed to be authoritative does not know it is, and the
branch that would let it receive the client's work is unreachable there.**
`GOPI.md:1035` Rule 28 declares peerbox the centre. The centre's settings declare no
`role` at all [measurement], and `IsServerNode`'s fallback is dead code that cannot
return true on any path (`internal/tools/memory_sync_delta.go:786-809`, four returns, all `false`),
so the effective predicate is `role == server` and nothing else. Both the git channel
and the close-out pick their arm from the declared topology
(`cmd/agent/cli_tasks_sync_git.go:58-60`, `internal/tools/task_finish_cleanup.go:877-878`), so on the
centre the client arm runs and then has nothing to push to, and the **server arm —
the one that acquires the client's delivered branches — never runs.** Root: the role
was added as a migration (bug041) and only the client's half was persisted.
**Requirement violated:** R2, R5, R9, and R4's receiving half.

**G2 — the alarm R3 depends on has no subscriber.** `ledger-conflict-human-task` is
the operator-facing half of the bug053 fix (`internal/tools/memory_sync_delta.go:585-600`, event
constant `internal/tools/tasknotify.go:86`). It rides the event set `all` only
(`internal/tools/tasknotify.go:155-159`); the ordinary default is
`claimed,in_progress,completed,failed` (`:63`), and the **live** subscription on the
centre is `claimed,in_progress,completed,failed,stale,build-failed` [measurement] —
which does not contain it. The fallback is a stderr line a daemon's log never reaches
a human through. **Requirement violated:** R3's escape hatch and R11.

**G3 — the protocol has no delete, and the divergence is now 952 differences with a
composition I measured.** The export predicate is `updated_at > anchor`
(`internal/tools/memory_sync_delta.go:186`) with no tombstone and no delete propagation. Measured
this session from the centre, two numbers that differ because they count different
things: the shipped instrument reports **952 differences** (`local=1908 peer=2468`;
189 missing-on-peer, **749 missing-on-local**, 14 status-mismatch), while a direct
comparison of the 749 ids against the client's ledger gives their composition —
**auto 667, human 66, llm 16**. So the dominant class is registration noise the centre
never received, and the tail is real human work. **Requirement violated:** R11.

**G4 — the channel never asks whether a peer name is a machine, and on this host the
same name means two different things.** The asset channel's validation is membership
of a list of strings (`internal/tools/memory_sync_assets.go:185-197`) — no handshake, no identity
exchange; the session's remote command is a bare `ssh <peer> gopi …`. Measured live:
the client accepts `memory sync --peer <its own whitelisted alias>`, and the alias
`hp` resolves to a third address from the client while resolving to the client from
the centre. **Requirement violated:** R8's second half, and R5's premise that a name
denotes a machine.

**G5 — one machine is recorded under two identities, in the table that means "this is
who I sync with".** The centre's anchor table holds **both** `hp` and `hostb-Laptop`
as separate peers with separate anchors (measured; the rows' `updated_at` values are
**10,510 seconds apart** — 2.9 h — while both were written within the same hour on
2026-09-16/17). The mechanism is at the code: the provenance/target identity resolves
as *environment override, else hostname* (`internal/tools/memory_sync_delta.go:95-103`, `:241-245`;
`GOPI_SYNC_NODE` at `:99`), and the override was installed mid-life (`53a965b8`).
**Requirement violated:** R1's "one story" and R5's identity.

**G6 — the answer to "are my machines one?" is per-machine, manual and unread — and
today the answer is that they are not.** `task sync --status` is a local read of the
anchor table. The one instrument that compares the two machines
(`task diff --peer`, exit 1, "the anchor premise is BROKEN") has **no scheduled
caller**: the auto-sync pass exists but is off unless its pacing variable is set
(`cmd/agent/cli_tasks_sync.go:264-276` — 0/absent = off), and it returns before doing
any work when the peer list is empty (`:325-336`) — on the centre it returns at the
first check, on every tick, forever. **Requirement violated:** R7, R11. `[opinion]`
this is the cheapest and most valuable fix in the document: a scheduled call to a
command that already exists and already exits non-zero.

**G7 — the channels' *cadences* are asymmetric for a reason nobody decided.** The task
channel has a pass gated on a pacing variable; the knowledge channel has **exactly one
caller, the CLI** (`cmd/agent/cli_memory_sync.go:157`)
[measured by exhaustive search over non-test files]. The measured effect: the centre
holds 9 rows about the client while the client holds **404** rows from the centre — an
asymmetry of 45×, in the direction that means the *centre's* knowledge reached the
client while the client's did not come back. **Requirement weakened:** R6's separation
was meant to keep the channels independent, not to leave one of them unscheduled.

**G8 — the peer's repository path is one hard-coded assumption on a platform Rule 12
made multi-repository.** The delivery target is a fixed path in the mechanisms that
need it, while the live settings declare **two** repositories (`/home/user/work/GoPi`
with a build command, and a second entry with none — a valid pure merge target under
Rule 12) [measurement]. **Requirement weakened:** R4 is true only for the repository
the constant was written for. `[inferred: that per-repo delivery is required; weakest
link — no operator row asks for it, so this may be speculative generality]`

**G9 — delivery has no notion of "the branch this task produced".** The client's git
pass publishes every non-master local branch (`GitSyncPushLocalBranches`, the harvest
push-back), with a *filter* (already-merged is skipped) rather than a *selection*. The
incident is the operator's own framing — "stale branches flood back to peerbox,
resurrecting deleted branches" (`t-1788332252`, measured: that row is on the client and
absent from the centre). **Requirement weakened:** R4 as stated says the finished
work travels; the implementation says everything the client happens to hold travels
unless the centre can prove it already has it.

**G10 — the centre's empty peer list switches off the cross-machine claim lock.** The
claim broadcast is gated on the declared peers; with none, a run claims locally only
and spawns no remote leg. On the centre that is the live state [measurement], so the
mechanism that prevents both machines from executing one task is off on the machine
whose whitelist is empty. **Requirement violated:** R1's "one row, one story".
`[inferred: that the protection being off will cost a duplicated run — the two hosts
work different layers by Rule 28, so I have not observed one]`

### 6.3 Where it OVER-BUILDS

**O1 — the server half of the ruling is written, documented, tested and unreachable in
the live topology.** This is deliberately *not* called "over-built code": deleting it
would be wrong and configuring it is a one-line change. `[opinion]` the honest label
is "built code with no live caller" — the same shape the sibling recorded, still true
after four more days of master.

**O2 — three ways to configure one fact, per channel, for two channels.** A peer can be
named by settings key, environment variable and command-line flag
(`internal/config/config.go:549-583` merges all three for the task channel, `internal/config/config.go:596-636` for assets, and
`internal/config/help.go:49-55` is the env-name group that lists them —
`GOPI_NODE_ROLE`, `GOPI_SYNC_TO`, `GOPI_SYNC_PEERS`, `GOPI_SYNC_NODE`,
`GOPI_SYNC_INTERVAL`, `GOPI_SYNC_FULL_EVERY`, `GOPI_SYNC_GIT_INTERVAL`). Each is individually justified
(Rule 2's single reader, the test seam, the one-off session); together they are three
chances to be wrong about one fact.

**O3 — the anchor table accumulates names that no longer exist and is never pruned.**
Measured on the centre: five peer rows, including a single-letter peer with anchor 0, a
test harness's name that has no producer anywhere in the tree, and **both** identities
of the same machine (G5). Nothing asks for pruning — except the operator's ability to
read the table as the truth about the topology (§2.4).

### 6.4 Where the requirement was NEVER DECIDED

**N1 — what a machine's *name* is: the alias or the host name?** G4/G5/G7 are three
symptoms of one undecided question. The code uses the host name (with an override) for
provenance and the alias for addressing, and nothing declares that these denote the
same machine.

**N2 — who may remove a row, and what the other side should think when it happens?**
G3 measures the result. I found no decision record saying "removal is out of scope, and
here is what the other side will do" — no operator row, no briefing, no comment at any
removal site. `[opinion]` the honest reading is that removal was never considered
rather than decided against.

**N3 — is knowledge sync supposed to be automatic?** N4's question restated against the
code as it is today: the task channel has a pass and its pacing knob; the asset channel
has a verb and no pass. Nobody decided that. `[inferred: the weakest link is that the
operator may hold exactly this opinion deliberately — "low-hanging fruit only" — in
which case the gap is that nothing says so where the next reader will look]`

**N4 — how many peers is the topology allowed to have?** The whitelist is a list and
the code loops over it, while Rule 28 describes exactly two hosts. **No source states
whether more than one peer is supported, undefined or forbidden**, and the operator has
a third machine in the picture already. `[opinion]` this is the likeliest place for a
silent wrong answer if a third machine is ever added by editing one settings file.

### 6.5 The operator's stated vision as CONSTRAINTS, checked

The series carries six binding constraints (`00-method-and-standards.md` §5, C1–C6);
this module is checked against them, and conflicts are filed rather than softened.

| Constraint | Verdict for this module |
|---|---|
| **C1** one static binary, zero deps, no plugins | **Holds.** The channels are the standard library plus the version-control binary the project already requires; the transport is a direct secure-shell call; the patch encoding is the project's own struct, not a protocol library. No finding. |
| **C2** the operator must control and be able to observe everything | **Conflict → G1, G2, G6, G7, and the failing A6.** An alarm with no subscriber, a correct check that no schedule runs while the premise it checks is broken, a channel that only moves when typed, and a centre that does not know it is the centre are each something he cannot observe or reverse. |
| **C3** a daemon must be simple | **Tension → O2 and R10's count.** The channel adds cross-node legs to a daemon whose behaviour depends on eight knobs, two whitelists and two identity values, and whose *order* (git before ledger) is load-bearing. `[opinion]` the order is justified by the code's own comment; the knob count is what I would cut. |
| **C4** the agent cleans up after itself; dirty is a violation | **Holds with a caveat.** The client's delivery/cleanup chain is real and exercised, and the close-out now refuses rather than merely records a dirty finish (Rule 30's own gate). The caveat is that the same chain's *server* arm is a single step (worktree removal), so on this host "cleanup" and "delivery" are the same act and the delivery half cannot happen. |
| **C5** a small team; solutions one person can run | **Tension → O2 and N4**, plus a measured cost outside the code: the workspace temp root holds **1.7 GB and 46 top-level directories**, at least one of which is a full **clone** (`.git` present), i.e. the exhaust of this very module's work is parked in the default scratch area. `[inferred: that these are this module's residue; weakest link — I did not trace each directory to a producing task.]` |
| **C6** `why` over `what` | **This document is the answer for this module; the code's own `why`s are strong at the sites cited and absent at the two sites that matter most (N2's removal path, N3's cadence).** |

### 6.6 What is genuinely new to the operator

Numbered; each names the sentence that carries it. The sibling document
(`devdoc/requirements/04-sync-and-nodes.md`) already told him the centre declares no
role, that the divergence exists and that nothing runs the check — **these five are
not restatements of those** (Appendix A records the full audit).

**Finding 1 (new) — the divergence is not one-directional, and I can prove the
mechanism from the centre's own tables.** Of the 749 rows the centre lacks, **667 are
registration noise (`source=auto`) and 83 are real work (66 human + 16 llm)** — so the
"missing 751 rows" is mostly the centre never receiving, not the client losing. But the
sharp case is the other direction: the operator's own defect row
**"current gopi repo cannot pull from peerbox" (`t-1788548312`, filed 2026-09-04) is
absent from the centre's ledger while the centre's `task_events` still records it being
started and completed on 2026-09-08.** A row that was demonstrably *in* the centre's
ledger is not there now, and the client — which still holds it — has no way to learn
that, because there is no tombstone and no delete propagation. **This turns the
sibling's weakest link (G3's cause, marked "medium: deleted vs archived out of the main
table") into a measured fact on the centre's own tables**, and it also turns the
sibling's §6.6 Finding 2 from "one side never received" into "the protocol cannot say
either way".

**Finding 2 (new) — the same machine has two identities in the anchor table, 2.9 hours
and two peer rows apart.** The centre holds `hp` (anchor 1789605617, updated
2026-09-17 00:40:21) and `hostb-Laptop` (anchor 1789595107, updated 2026-09-16
21:45:13). Two peer rows for one machine mean a delta session against one of them has
no visibility into what the other already sent — the divergence is partly *structural*,
not merely a missing trigger. The sibling's G5 counted the two names in the provenance
data; **what is new here is that the duplication is persisted in the table that defines
the topology**, which is the one table that must have one row per machine.

**Finding 3 (new) — knowledge flows 45× more in one direction than the other, and the
cause is a missing schedule.** The centre holds **9** rows about the client
(`node:hostb-Laptop` 8 + `node:hp` 1); the client holds **404** rows from the centre
(`node:peerbox`). The asset session has exactly **one caller — the CLI verb** — so the
client's knowledge reaches the centre only when a human types it, while the task
channel has a daemon pass. The sibling noticed the manual/automatic asymmetry (its N4)
but did not measure its size or its direction; the direction is the one that matters,
because the client is the machine that *produces* knowledge and the centre is the one
that is supposed to hold it.

**Finding 4 (new) — the trunk now contains what the sibling said was only in the
running binary, and the delivery gap is now purely a configuration gap.** The sibling's
§7.5 told a test author not to read this module's delivery behaviour off trunk because
the close-out fix commits sat on remote-tracking branches. Measured at `812f9a60`:
`bf9fbef8` (the pre-merge gate), `5c45a279` (deliver a finished branch to the sync peer)
and `3c396080` (the delivery-point sync) are **all ancestors of master**, and the
`feat/client-closeout-delivery` feature was separately **backed out** for merge-induced
reds (`085811f8`, −2,590 lines across 19 files, two of them entire implementations
deleted). So the honest statement today is: the mechanism is in trunk; what stops it
running on the centre is one undeclared settings key.

**Finding 5 (new) — the operator can watch the divergence close, and the repair he
would want is a deletion he cannot see.** The centre's own remedy path is
`gopi task sync --full hp` (printed by the failing tool itself). Because the protocol
is forward-only and the client holds 749 rows the centre lacks, a full reconciliation
credits the client's rows to the centre — including the 667 registration rows — and
**that is the correct behaviour today and will be the wrong behaviour after any
cleanup**, because a row the centre legitimately removed comes back on the next pull.
Nothing in the system distinguishes "the centre removed it" from "the centre never had
it". `[opinion]` this is the single most consequential consequence of N2 being
undecided, and it is cheap to observe: run the prescribed repair, then count.

---

## 7. What this implies for tests

Existing tests only, named as they are, with the verdict this requirement changes — or
an explicit statement that none exists. **No tests are written by this document**
(series rule). Test names were resolved in this session against `812f9a60`.

### 7.1 Tests whose verdict R2/R3 changes — the ruling must never close real work

| Test | File | Verdict under this requirement |
|---|---|---|
| `TestSyncDeltaServerResolvesConflict` | `internal/tools/memory_sync_delta_test.go` | STILL CORRECT as a server test, **but it must now run with the role declared**: G1 shows an undeclared machine is never the server, so the test currently proves a branch that is unreachable in the live topology. A companion asserting the guard (not the ruling) for the undeclared case does not exist. |
| `TestSyncDeltaServerRulingBlocksHumanTicket` | same file | CORRECT and load-bearing, **and its verdict is incomplete**: it proves the event reaches the pipe the test installed, not that any live configuration subscribes to it. It cannot fail when the operator's event list omits the name (G2). |
| `TestSyncDeltaConflictEqualAnchor`, `TestSyncDeltaEqualAnchorEcho` | same file | Unchanged in intent; **their role fixture is the thing to align** — these were retro-fitted when the role became primary (`a26c4b8a`, `6f7aae38`), i.e. the code drifted and the tests followed. Under R5 the fixture should be the *declared* role, so an undeclared machine can never pass a server assertion. |
| `TestSyncDeltaTerminalNeverRegresses` | same file | Unchanged, and the right guard for R1. |
| `TestBidir2LiveConflictNeverClobbered` | `internal/tools/memory_sync_bidir2_e2e_test.go` | CORRECT; R2's verdict strengthens it — the clobber must also be impossible *without* a declared role on either side. |

### 7.2 Tests whose verdict R4/R12 changes — the article must travel

| Test | File | Verdict |
|---|---|---|
| `TestFinishCleanupChainPushFailKeepsBranch`, `TestFinishCleanupIdempotentPush` | `internal/tools/task_finish_cleanup_test.go` | CORRECT — the acceptance test for A3's second half (the article survives a failed push). |
| `TestFinishCleanupChainDirtyWorktree` (`:191`), `TestServerRecycleDirtyWorktreeSkipped` (`:912`) | same file | CORRECT under C4 — the dirty-worktree guard is load-bearing. **The assertion at `:201`** ("`no steps may run on a dirty worktree`") is the series' worked instance of a correct-assertion/wrong-requirement failure: the push is harmless and must RUN, the removal is destructive and must DEFER. `[opinion]` the requirement does not change this test's verdict, but it *does* require the companion assertion that the push ran. |
| `TestPreMergeConflictBlocksThePush`, `TestPreMergeCleanMergeRebuildsThenPushes`, `TestPreMergeAbstainsWithoutADeclaredBuild`, `TestPreMergeUpToDateIsAZeroCostNoOp`, `TestPreMergeIsOffByTheKillSwitch`, `TestPreMergeGroundMustBeTheDeliveredBranch` | `internal/tools/task_finish_premerge_test.go` (8 tests) | ALL CORRECT and current on trunk — **their verdict is unaffected; what is new is that they now test a mechanism the centre reaches only after its settings change.** |
| `TestDeliveryPointSyncConflictBlocksTheRelease`, `…CleanMergeBuildsThenReleases`, `…UpToDateIsZeroCost`, `…NeverMergesInTheMainCheckout`, `…IsOffByTheKillSwitch` | `internal/tools/task_finish_deliverysync_test.go` (11 tests) | CORRECT and the counterpart of the client gate for this host's arm. **The missing test is not here — it is the one that asserts the arm CHOSEN is the one declared** (G1). |
| `TestPushbackSkippedMergedStaleBranchNotResurrected`, `TestPushbackNoBranchPushesNothing`, `TestPushbackCollisionRenamesAndSucceeds` | `internal/tools/task_sync_git_pushback_test.go` | CORRECT as filter tests. **Under G9/R4 they pin the wrong shape**: they assert the flood is survivable, and no test asserts the selection ("the branch this task produced") because no such notion exists in the code. |
| `TestServeGitSyncSweepNoPeerIsPureNoOp`, `TestServeGitSyncSweepWithPeerAdvancesClock`, `TestServeGitSyncSweepThrottles` | `cmd/agent/singlenode_serve_test.go`, `cmd/agent/cli_tasks_sync_git_test.go` | CORRECT as single-machine contracts, **and the first is the test that hides G1** on a two-node install whose centre declares no peer. `WithPeerAdvancesClock` advances a clock; it does not prove a fetch happened. |

### 7.3 Tests whose verdict R7/R8 changes — the premise and the topology

| Test | File | Verdict |
|---|---|---|
| `TestReconCompareBothDirections`, `TestAnchorGuardRefusesPendingMismatch`, `TestSyncAnchorGuardRefusesOnMismatchWithRepairHint`, `TestSyncAnchorGuardEqualPendingSetPasses`, `TestSyncAnchorGuardFailClosedOnUncomparablePeer`, `TestTaskDiffPeerComparesCatalogBothDirections` | `internal/tools/memory_sync_reconcile_test.go`, `_cov_test.go` | CORRECT — the guard is exactly R7's "refuse, do not advance", and these are tests of a mechanism that **works**. Their verdict changes in an unusual way: **they are all green while the live premise is broken**, because each tests one call under a fixture and none asks whether the call is ever made. The missing test is not another guard case — it is an assertion that the check has a scheduled caller. |
| `TestSyncStateForwardOnlyAnchors` | `internal/tools/memory_sync_delta_test.go` | **Changed by G3 + Finding 1.** It asserts the forward-only behaviour as correct; under R11 that is correct for a row that still exists and incomplete for a row that was removed. The missing case: "the peer removed a row we already hold — what do we report?" |
| `TestMemoryAssetsPeerWhitelistDecoupled`, `TestMemoryAssetsStalePeerHint` | `internal/tools/memory_sync_assets_test.go` | CORRECT for R6/R8's first half. **Incomplete for R8's second half (G4):** no test asserts the machine a peer name resolves to is a different machine, and I verified live that the channel accepts a whitelisted name without asking. |
| `TestRunClaimLockProtocolNoPeers`, `TestStartClaimLockBroadcastEmptyPeers` | `internal/tools/memory_sync_claim_test.go` | CORRECT as single-machine contracts and **the tests that bless G10**: they assert an empty peer list means local-claim-only, which is right for a genuinely single-node install and is exactly what the live two-node topology does on the centre. |

### 7.4 Tests for identity itself (R5)

`internal/config/node_role_test.go` pins eight behaviours — `TestConfigRoleSyncToParsing`,
`TestResolveNodeRoleSettings`, `TestResolveNodeRoleEnvOverridesSettings`,
`TestResolveNodeRoleUnknownSpellingUndeclared`, `TestNodeIdentityLineServer`,
`TestNodeIdentityLineClientArrow`, `TestNodeIdentityUndeclaredEmpty`,
`TestNodeIdentityBriefingFormat` (8 `func Test`). `TestNodeIdentityUndeclaredEmpty` is
the one that **blesses the defect**: it asserts an undeclared role produces an empty
line — correct as a refusal to guess, and the exact reason A4 has no symptom on the
centre. `[opinion]` the requirement does not change this test's verdict, but it requires
a companion: an operator who declared the topology in a rule must be able to find out
that a machine did not declare it. **That test does not exist.**

### 7.5 An explicit statement about what does NOT exist

- **No test asserts that the centre can receive.** Every git-channel test exercises the
  client pass or the single-node no-op; none runs the server pass on a machine whose
  role is declared. This is the largest hole and it is on the requirement's critical path.
- **No test asserts a two-node declaration that is half present.** Both live defects
  (G1, G10) are "one machine declared, the other only inferred", and both are blessed by
  tests that treat "no peer" as a legitimate single-node shape.
- **No test bounds the flood** (G9) or asserts selection, because the notion does not
  exist in the code.
- The sibling's §7.1 names `TestSyncPushDeliveryOrderPushThenCleanup`; **that test does
  not exist at either revision** (searched by name, this session, and in the sibling's
  own base). The nearest real names are `TestFinishCleanupChainPushFailKeepsBranch` and
  `TestFinishCleanupIdempotentPush` (`internal/tools/task_finish_cleanup_test.go:211`, `:272`).

---

## 8. Confidence and weakest links

### 8.1 Per-claim confidence

| Claim | Confidence | Basis |
|---|---|---|
| the boundary; `internal/sync` does not exist | **high** | directory census + file inventory |
| R1–R11 as stated with those citations | **high** | operator rows read and quoted, code lines read |
| R12 (the remedy must exist where the divergence is felt) | **medium** | one operator row, read in full — but it is one row, and it is filed as a *bug* rather than a need statement |
| **A1's measurement (952 differences, exit 1, 189/749/14)** | **high** | the shipped instrument, run in this session against the live peer; exit code observed |
| **the composition of the 749 (667 auto / 66 human / 16 llm)** | **high** | direct id-set comparison between the two live ledgers |
| **Finding 1 (t-1788548312 present on the client, absent from the centre, events surviving on the centre)** | **high** for the observation; **medium** for "removed after ingestion" | three independent readings agree (client row, centre absence, centre `task_events` rows for 2026-09-08). The *mechanism* of removal is my inference — see 8.2 |
| **Finding 2 (two anchor rows for one machine, 10,510 s apart)** | **high** | the centre's `sync_state` table read directly; the arithmetic is exact |
| **Finding 3 (9 vs 404 rows, one caller)** | **high** | counted on both ledgers over ssh + exhaustive search for the session's callers |
| **Finding 4 (the close-out commits are ancestors of master)** | **high** | `git merge-base --is-ancestor` per SHA |
| G1 (centre declares no role; server arms unreachable) | **high** | settings read on the centre, `GOPI_SYNC_PEERS` empty in a live daemon's environment, four `return false` paths read, both arm selections read |
| G2 (alarm has no subscriber) | **high** for "not subscribed in the live configuration" | live event list read on the centre + the const at `internal/tools/tasknotify.go:63`/`:155-159`. **medium** for "never fired anywhere" — I did not enumerate every log on every host |
| G4/G5 (one name, two machines; one machine, two names) | **high** for the observations | read on both machines this session; the *consequence* is §8.2 |
| G7 (asset cadence) | **high** | one caller found by exhaustive search; the asymmetry counted |
| G8 (hard-coded peer repo path) | **high** for the fact; **low** that it is a requirement gap | constants read; `[opinion]` as filed |
| N1–N4 ("never decided") | **medium** | absence of evidence — I searched operator rows, `GOPI.md` and the devdoc tree; a row could exist that I did not match |

### 8.2 Weakest links, in order

1. **What removed the centre's rows.** I proved a row that was worked on the centre is
   no longer in the centre's ledger while its event rows survive, so "removed after
   ingestion" is strongly supported — but *what* removed it (a purge, a lifecycle
   transition, a migration) I did not find, and it is not this module's code with a
   removal path in it. If it is B1's lifecycle doing something legitimate and
   documented, Finding 1's framing changes from "the protocol's blind spot" to "the
   operator's cleanup is invisible", which is still a finding but a different one.
2. **Whether the client's asset whitelist naming itself has ever caused a wrong
   session.** I observed the acceptance live and the address disagreement measured; the
   anchor table shows a recent write, so *something* answered at some point. I claim
   only that the name denotes no single machine and nothing checks — not that data was
   written to the wrong host.
3. **R12's strength.** It rests on one operator row filed as a bug. A reasonable reader
   could call it a bug report rather than a requirement; I have stated it as a
   requirement because the need it names is the module's own ("I can tell which side's
   version is current") and it recurs.
4. **The C5 scratch measurement (1.7 GB, 46 directories, one clone).** The size is
   measured; the attribution to this module's work is not.
5. **N4's reality.** More than one peer is a requirement gap only if a third machine is
   contemplated. Rule 28 mentions migrating a task, which implies more than two *rows*,
   not more than two *machines*.
6. **The one-week drift.** Master moved 17cec86b → 812f9a60 with 106 files changed under
   `internal/tools` + `internal/config` in between, including two merge rounds on
   close-out code. Every line number here is checked against `812f9a60`; a reader on a
   later master must re-resolve them (§8.3).

### 8.3 Marked inferences, collected

§3's entry condition and the ordering rationale; §4 R4's "per-repo delivery is
required"; G4's consequence; G7's cause (a missing schedule rather than a decision);
G10's "will cost a duplicated run"; G8's status as a gap; N1–N4's existence as
undecided questions; §6.5's attribution of the scratch tree; Finding 1's removal
mechanism; R12's strength. Everything else is a quoted row, a read line, a resolved
SHA, or a command whose output is stated — and every measurement was taken on
2026-09-18 with both machines live, so the counts are a snapshot of a moving system.

---

## Appendix A — audit of the sibling document (`devdoc/requirements/04-sync-and-nodes.md`, commit `92399148`, pin `17cec86b`)

The series' method (umbrella §7.4) says **the module document governs its own module's
content** and a later pass repairs only what fails acceptance, leaving weak resolutions
visibly marked. That is what this appendix is: `04-sync-and-nodes.md` is **not edited**.

**Coverage.** It satisfies the 8-section shape (all eight present, in order:
§1 `:87`, §2 `:123`, §3 `:170`, §4 `:218`, §5 `:394`, §6 `:467`, §7 `:923`, §8 `:986`)
plus front matter `:11`, the C1–C5 chapter `:59`, and an explicit
**"What I could not determine"** list `:1050` — 1,079 lines, ≥ 3 findings, every
inference marked with a weakest link. Its §6 is load-bearing (12 G-rows, 3 O-rows,
4 N-rows) and its §7 names ~30 real tests. **On its own terms it is a conforming,
strong module document.**

**What a later revision changes about it** (all measured this session at `812f9a60`):

| # | Issue | Evidence | Disposition |
|---|---|---|---|
| A-1 | **Stale pin.** It pins `17cec86b`; master has moved to `812f9a60` with **106 files changed** under `internal/tools` + `internal/config`, including two merge rounds on the close-out code. | `git diff --name-only 17cec86b..812f9a60 -- internal/tools internal/config \| wc -l` → 106 | Its own §6.3/§8 rule ("pin the revision") is obeyed; the document is simply a week old by construction. **No repair needed** — the series' rule is that a module document governs its own revision. |
| A-2 | **§7.5 is refuted on current master.** It says the trunk is at `17cec86b` and the close-out fix commits exist only on remote-tracking branches; a test author is told not to read delivery behaviour off trunk. | `git merge-base --is-ancestor` → `bf9fbef8` YES, `5c45a279` YES, `3c396080` YES (all ancestors of `812f9a60`), and `085811f8` backed the client-closeout feature out (−2,590 lines / 19 files) | **Superseded, not wrong** — true of its own base. Recorded here as Finding 4 so a reader does not follow the week-old instruction. |
| A-3 | **One test name does not resolve.** §7.2 names `TestSyncPushDeliveryOrderPushThenCleanup`; no such function exists at either revision. | name search over `*_test.go`, this session and in `17cec86b` | **Defect (citation that does not resolve).** The nearest real names are `TestFinishCleanupChainPushFailKeepsBranch` (`:211`) and `TestFinishCleanupIdempotentPush` (`:272`). Recorded; the sibling document is left as written per the series rule. |
| A-4 | **`GOPI.md:1002` for Rule 28 resolves only at its own base.** Rule 28 is at `:1002` in `17cec86b` and at `:1035` in `812f9a60`. | `git show 17cec86b:GOPI.md \| grep -n '^## Rule 28'` → 1002; current → 1035 | **Not a defect** — the citation is revision-correct. Every Rule 28 citation in *this* document uses `:1035`. |
| A-5 | **Its G3 cause is its own declared weakest link** ("deleted vs archived out of the main table; medium confidence"). | its §8 "Weakest links, in order" item 1 | **Strengthened here** into Finding 1 (a row demonstrably worked on the centre is gone from the centre's ledger, events surviving). Its cautious framing is vindicated, not corrected. |
| A-6 | **Its finding 2 says "nothing runs the check"; the mechanism is now named.** It states the check has one call site and no scheduled caller. | `cmd/agent/cli_tasks_sync.go:264-276` (0/absent = off) and `:325-336` (empty peer list returns immediately, on every tick) | **Extended, not contradicted:** the scheduled caller exists but is *off by configuration*, on the machine whose peer list is empty. |
| A-7 | **Coverage of the evidence sources.** It mines operator rows and GOPI.md rules well; it does **not** cite `t-1788548312` ("current gopi repo cannot pull from peerbox") — the operator's own divergence complaint, filed from the client and absent from the centre. | ledger query, this session | **Its gap, now closed here** (R12, Finding 1). The row is exactly the kind of evidence its §4 wanted and did not find. |
| A-8 | **Its line handles resolve.** Spot-checked 7 of its `file:line` citations in its own base plus `:281`/`:289`/`:977` in the merged copy — all resolve; `internal/tools/task_finish_premerge.go` (which it cites as new) exists and is created by `bf9fbef8`, an ancestor of its base. | spot-check, this session | **Conforms to the citation standard.** |

**Net:** the sibling document is a correct and conforming module document for the
revision it read; the series' honest measurement of its coverage is "8/8 sections, 3+
findings, 1 non-resolving test name, and one week of drift", with three of its findings
(A-5, A-6, A-7) now measurable rather than inferred. This document is a companion that
audits and extends it, not a replacement.

---

## What I could not determine

1. **What removed the centre's rows.** I proved the removal is real on the centre's own
   tables (Finding 1) but found no removal path in the sync family and did not find the
   purge/lifecycle/archival mechanism that did it. This is B1's territory and I flag it
   rather than guess.
2. **Whether the two anchor rows for one machine have ever caused a *lost* delta.**
   The duplication is measured; the loss is my model, not an observation. Reproducing it
   would need a session driven deliberately with each identity in turn.
3. **Whether `ledger-conflict-human-task` ever fired under any configuration on any
   host.** It is not in the centre's live event list; I did not enumerate the notify logs
   of every host or every retired home directory.
4. **Whether the client's asset whitelist naming itself has actually moved data to the
   wrong host.** The address disagreement is measured on both machines; the failure is
   not.
5. **What the second declared repository in the live settings is for** (a valid pure
   merge target under Rule 12). Whether G8 is a real gap or speculative generality is
   therefore not decided.
6. **The intended peer cardinality** (N4). Rule 28 is written for two hosts and the code
   for N; nothing decides which wins.
7. **Whether the 46 directories and 1.7 GB in the workspace temp root are this module's
   residue.** The size is measured; the attribution is not, and at least one entry is a
   full clone whose owner I did not identify.
8. **What the operator considers the *good* outcome for the current divergence**: a full
   reconciliation that credits 749 client rows to the centre (including 667 registration
   rows), or a classified merge that keeps the centre's removals. The tool prescribes the
   first; the requirement R11 plausibly wants the second; the decision is his.

