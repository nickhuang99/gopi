<!-- doc-class: normative -->
# `gopi chat` session ground: the lifecycle diagnosis (who cleans up — measured: nobody)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Base commit: **d1f885a8** (`brief: who cleans up after a gopi chat session?`, 2026-09-28 13:44:56 +0800).
Diagnosis only: **no code was changed by this task**. `go build ./...` exit 0, `go vet ./...` exit 0 (pasted in §7).

The question this answers is *not* "does the residue exist" (it does — §1) but **where in the
lifecycle a collector could hang, and why none hangs there today**. Every claim carries a
`file:line` or pasted command output.

---

## 1. The residue, measured live in this run (not the brief's table)

The brief (committed as `briefings/brief-chat-session-lifecycle-who-cleans-up.md`, the only file
of `d1f885a8`) recorded six `chat/*` branches, each `?? testdata/term_page.golden` dirty, zero
holders. **Those six are already gone** (`git tag --list 'archive/chat/*'` → 6 archive tags, and
`git branch --list 'chat/*'` returns a *different* six). The family on disk now:

```console
$ git branch --list 'chat/*' | cat
+ chat/0f9994c3cf0d4a81a1c006dc4adc7620
+ chat/1cf820b1c40d4ccc85bfbf33e56da4bd
+ chat/3dcb81cc00804eb1aa640d599ae48269
+ chat/57a2aef7a9894e7fbf307e8539165b48
+ chat/a2880e6c418e4eb8a2aaeb80a47cf053
+ chat/de37c85b2e5e4fc494cbc7eb7731848a
$ for b in $(git branch --list 'chat/*' | sed 's/^[*+ ]*//'); do echo "$b ahead=$(git rev-list --count master..$b) tip=$(git rev-parse --short $b)"; done
chat/0f9994c3cf0d4a81a1c006dc4adc7620 ahead=0 tip=d1f885a8
chat/1cf820b1c40d4ccc85bfbf33e56da4bd ahead=0 tip=d1f885a8
chat/3dcb81cc00804eb1aa640d599ae48269 ahead=0 tip=d1f885a8
chat/57a2aef7a9894e7fbf307e8539165b48 ahead=0 tip=d1f885a8
chat/a2880e6c418e4eb8a2aaeb80a47cf053 ahead=0 tip=d1f885a8
chat/de37c85b2e5e4fc494cbc7eb7731848a ahead=0 tip=d1f885a8
$ git rev-parse --short master
4a19b30d          # d1f885a8 is an ancestor of master → every chat branch is 0 ahead
$ git worktree list | grep -i chat
/home/user/workspace/gopi-0f9994c3cf0d4a81a1c006dc4adc7620  d1f885a8 [chat/0f9994c3cf0d4a81a1c006dc4adc7620]
… (six registrations, all at d1f885a8)
$ for i in <the six ids>; do p=/home/user/workspace/gopi-$i; n=$(ls -l /proc/[0-9]*/cwd 2>/dev/null | grep -c "$p"); echo "$i holders=$n"; done
0f9994c3… holders=0  1cf820b1… holders=0  3dcb81cc… holders=0
57a2aef7… holders=0  a2880e6c… holders=0  de37c85b… holders=0
```

**Dirt: there is none now.** The brief's `?? testdata/term_page.golden` does not reproduce:

```console
$ git -C /home/user/workspace/gopi-0f9994c3… status --porcelain      # (all six identical)
$ git -C /home/user/workspace/gopi-0f9994c3… status --porcelain --ignored | head
$ git -C /home/user/workspace/gopi-0f9994c3… ls-files .baseline .evid .evid2 | head -3
.baseline/gates.txt
.evid/addrow.py
.evid/after.txt
$ git log --oneline -1 -- .baseline
ee5e0a07 wip(feat/web-m2-rework): rescue uncommitted work before stopping the workers (PA)
```

The `?? testdata/term_page.golden` the brief saw was **the brief's own authoring session** — that
path (`devdoc/chat-screen-dump-drive.py`'s neighbours, `.baseline/`, `.evid/`, `.evid2/`) belongs
to the `verify`/audit workflow of a task session, not to a chat turn. The six grounds stand
**clean**, because their content is `d1f885a8`'s own tree (`.baseline/.evid/.evid2` are *tracked*
in it, 5417 tracked files) and nothing was added on top.

Provenance, exact (so the reader can tell what produced them):

```console
$ cat /home/user/work/GoPi/.git/logs/refs/heads/chat/0f9994c3…  # all six the same shape
0000000000000000000000000000000000000000 d1f885a8a2a0ba694553ff027c1830b9715f257d gopi <gopi@localhost> 1790574390 +0800	branch: Created from HEAD
$ date -d @1790574390 '+%F %T %z'
2026-09-28 13:46:30 +0800        # the six span 13:46:04 → 13:46:30; the brief commit is 13:44:56
$ cat /home/user/work/GoPi/.git/worktrees/gopi-0f9994c3…/logs/HEAD
…000 d1f885a8… gopi <gopi@localhost> 1790574390 +0800	
d1f885a8… d1f885a8… gopi <gopi@localhost> 1790574392 +0800	reset: moving to HEAD
```

* six `git worktree add <dir> -b chat/<id> HEAD` invocations, ~4–12 s apart, each followed
  2 s later by a `reset: moving to HEAD` (`git worktree add`'s own checkout step);
* **no session directory exists for any of the six ids** —
  `for s in <ids>; do find /home/user/.gopi/sessions -maxdepth 1 -name "$s*"; done` → nothing;
* **no CHATS row** — `select count(*) from chats` = 174, `… where git_branch like 'chat/%'` = **0**,
  and each of the six ids: `NO ROW`.

So: six grounds minted in the operator's real worktree root by a process whose session left no
trace — a **batch of automated `gopi chat` launches `cwd`-ed at a real checkout**, launched by a
script/driver that did not pin `GOPI_WORKTREE_ROOT` (contrast `internal/testutil/run.go:129`,
which pins it: `worktreeroots.WorktreeRootEnvName+"="+t.TempDir()`, so the Go test suite does
**not** leak). The same batch also reset a branch (`branch: Reset`-class reflog lines are absent,
but `reset: moving to HEAD` is present) — whether it is a driver or a hand-run is **not pinned by
this run's evidence**; what *is* pinned is the code path (§2) and that the residue is a ground,
not a deliverable.

---

## 2. Answer ①: who creates the branch and the workspace? — `gopi chat`, once per RUN, and only for the lineedit carrier

**The path** (one call, before the first turn):

| step | file:line |
|---|---|
| REPL/once/non-tty entry | `cmd/agent/cli_chat.go:676` `runChat` → `runChatWithInput`; `cmd/agent/chat_once.go:629`; `cmd/agent/chat_once_seq.go:134` |
| resolve + announce + chdir | `cmd/agent/cli_chat.go:983` `if ground, gerr := chatEnterGround(ag); gerr != nil {` → `cmd/agent/chat_ground.go:37` `func chatEnterGround` |
| the producer (pure function) | `internal/tools/chat_ground.go:120` `func ChatGroundFor(sessionID, dir string) ChatGroundResolution` |
| branch name | `internal/tools/chat_ground.go:174` `branch := ChatGroundBranchPrefix + stem` (`ChatGroundBranchPrefix = "chat/"`) |
| directory | `internal/tools/task_run_ground.go` — `taskGroundDirFor` → `TaskGroundDirPrefix` + stem under `config.ResolveRepoWorktreeRoot` (default `<home>/workspace`, `internal/config/config.go:514`) |
| materialise | `internal/tools/task_run_ground.go:583` `taskGroundCreate` → `:594-600` `git worktree add <dir> -b <branch> HEAD` |

**Trigger**: the first statement of the session, after the agent (and therefore the session id)
exists and **before any turn** (`cmd/agent/cli_chat.go:970-986`, comment: "resolved HERE, after
the session id exists … and BEFORE any turn runs"). Not the first tool call, not the first turn.

**Which carriers**: only `runChatWithInput` calls `chatEnterGround`, so `gopi chat` (lineedit),
`gopi chat --once` and the `--turns` sequence carrier all create one. **`gopi ui` does not**:
`cmd/agent/cli_ui.go:136` `runUIChat` → its turn runner at `cmd/agent/term.go:360-362` `ag.ChatOps`
never resolves a ground. **Web chat does not** either: `cmd/agent/cli_web.go:1314-1316`. So the
three surfaces of one feature differ: the lineedit carrier grounds, the two others do not
(they inherit whatever cwd they were started in).

**Per-session or per-run?** The **name** is per-session (the stem is the whole session id,
`internal/tools/chat_ground.go:172` `chatGroundIDStem` → `taskGroundIDStem`), and the resolution is
*reuse-if-present* (`internal/tools/chat_ground.go:178-190`: an existing registered worktree at the
derived path is reused, not re-created). But the **call** is per-run: the ground resolved on every
`runChatWithInput`. So:

* same session id, second run → **the same ground is reused** (`ground reused:` notice);
* resume/continue (`gopi chat <session-id>`) → per `devdoc/chat-session-ground.md` §"Not done",
  the seed names a raw log, not a worktree, so a resumed session mints a **new** session id and
  therefore a **new** ground; the old one is not re-entered. Each of the six observed grounds is
  one such run.

**Nothing is recorded**: `internal/tools/chat_ground.go:31` — "NOTHING IS STAMPED IN THE LEDGER.
A chat is not a row." That sentence is the origin of every arm failure in §4.

---

## 3. Answer ②③: what could clean it up, and why each did not fire

Three candidate arms were named in the brief; a fourth exists and is the one that *runs*.

### 3.1 `finish-cleanup` — UNREACHABLE (ledger-keyed, and a chat writes no ledger row)

`ReclaimDeadHolderWorktrees` (`internal/tools/task_reclaim.go:434`) is the repair leg that frees a
worktree whose holder is dead. Its candidate scan is a **ledger query**:

```go
// internal/tools/task_reclaim.go:965-976
`SELECT task_id, status, … FROM tasks
   WHERE deliverable LIKE ?            -- "%worktree%"
     AND status IN (?, ?, ?, ?)`       -- completed, failed, cancelled, verified
```

A chat ground has no `deliverable` at all (`internal/tools/chat_ground.go:31`), and the chat's own
CHATS row (`internal/agent/chatregister.go:56-66`) is never read by this arm. **Reason it did not
fire: it was never a candidate** — measured: the six ids have no `chats` row
(`where session_id=… ` → `NO ROW`) and no `tasks` row. This arm is *unreachable* for a chat, not
merely unlucky.

### 3.2 `gopi scratch-gc` / the scratch tick — ABSENT (its candour never covered this family)

`cmd/agent/scratch_gc_tick.go:100` `scratchGCTick` does exactly three things, and every one of them
names its own root:

| line | what it sweeps |
|---|---|
| `:116` | `scratch.SweepHome(home, …)` — the scratch HOME |
| `:142`, `:154` | `scratch.SweepBaseSuiteRuns` / `scratch.SweepBase(baseRoot, …)` — base-suite runs |
| `:184` | `scratch.SweepSandboxes(sandboxRoot, scratch.SandboxStaleAge)` — sandbox roots |

`<home>/workspace/gopi-<session-id>` is none of those. **Reason it did not fire: the family is
outside the reclaimer's scope** — the arm has no rule for a branch, no rule for a worktree under
the worktree root, and no candour that reaches one. (`ReclaimDeadHolderWorktrees` also wants a
dead-holder *row*, so it is not this arm's substitute either.)

### 3.3 the worktree reap (`finish-cleanup`'s registration sweeper) — REACHABLE, and it RUNS — then keeps all six

This is the finding that replaces the brief's "no mechanism exists". The sweep **is** wired to a
resident tick: `cmd/agent/task_lifecycle_tick.go:298` calls `serveWorktreeReapSweep`
(`cmd/agent/cli_tasks.go:3546`), which is the worker loop's **and** the merger tick's pass. It ran
against these six. Pasted, from this run — the identical entry point, dry run, two different `cwd`s:

**(a) process cwd outside the repo** (run from `/home/user`):

```console
$ bin/gopi-diag task sweep-worktrees --json
{"path": "/home/user/workspace/gopi-0f9994c3…", "branch": "chat/0f9994c3…", "disposition": "keep", "rule": "self-checkout",
 "reason": "this process stands in this registration's own repository — freeing it would delete the ground under this process; the
 registration belongs to the process that created it (the producer-side release), which proves ownership from the ledger's claim
 stamp rather than from the directory's looks"}
… all six identical
```

**(b) process cwd inside the repo** (the same dry run from this worktree):

```console
$ bin/gopi-diag task sweep-worktrees --json
{"path": "/home/user/workspace/gopi-0f9994c3…", "branch": "chat/0f9994c3…", "disposition": "keep", "rule": "live-session",
 "reason": "no live task's deliverable names this registration, but live task 01a0e4fe-2dad-7306-bf4c-1951caefb268 still has a run
 standing (its recorded runner (pid 341456) is still running) — its own session's ground"}
… all six identical          # pid 341456 = `gopi worker`, up 11:10:20
```

Both are **process-global, all-or-nothing gates that return on the first matching row**:

* `self-checkout` — `internal/tools/worktree_sweep.go:1003` `if ho.SelfRepoGuestCheckout { return keep(…) }`,
  resolved once per repo at `worktree_sweep.go:1986` from `worktreeSweepSelfGuestCheckout`
  (`worktree_sweep.go:717`). It asks about **the asker's own position** and is **not scoped to the
  registration's repo** — so one process standing in a linked worktree of *any* repo freezes every
  residue of that repo.
* `live-session` — `internal/tools/worktree_sweep.go:1045`, via `worktreeSweepLiveSessionRow`
  (`worktree_sweep.go:1300`), which returns the **first** live row with a standing run
  (`worktreeSweepSessionLiveEvidence`, `worktree_sweep.go:1254`). It never asks whether that row's
  branch, worktree or repo has anything to do with *this* registration.

Behind both gates, for the object itself, the ladder would *still* keep them right now:
the live-holder scan `worktree_sweep.go:1011` passes (holders=0, measured §1) — and then the age
floor `internal/tools/worktree_sweep.go:90` `DefaultWorktreeSweepMinAge = 24 * time.Hour`
(`:1108` `if age < opts.MinAge && !ended { return keep(worktreeRuleTooYoung, …) }`) keeps an
8-hour-old registration. **That floor is correct and is not a defect**: 24 h is exactly the
"a task just created this" shield.

**And the branch is never even offered.** The ref is deleted on exactly one rung —
`internal/tools/worktree_sweep.go:961` `return reapBranch("registration is dead (its directory is
gone) — the branch goes with it if git agrees (`branch -d`)")` — reached only when
`reg.Prunable || ho.DirGone` (`worktree_sweep.go:922`). A **live** directory (our six) never enters
that arm, and the ordinary reclaim rungs (`worktreeRuleOwnerEnded` `:1116`, `worktreeRuleUnclaimed`
`:1128`) call `reclaim(...)`, which removes the **registration** only. The branch axis is
*by design*: `v1` records in `/home/user/.gopi/logs/removal-audit.log` show `worktree:remove`
(`arm=task-self-own-sweep`) and `branch:delete` (`arm=merge-run-spent-branch`) as two different
operations by two different arms.

### 3.4 the producer pass (`ReclaimSelfOwnRegisteredWorktrees` / `ReclaimOwnWorktreesAtCloseout`) — UNREACHABLE (no chat close-out calls it)

`internal/tools/task_reclaim_self.go:479` is the ownership-independent pass that *does* free an
unowned registration of the run's own repo — and the audit log proves it works on chat grounds:
`logs/removal-audit.log` has 9 `worktree:remove` records with `arm=task-self-own-sweep` naming
`branch=chat/<id>`, e.g. `…/gopi-117c1c0e660547a68fd8817521396812`, `…/gopi-af7b87fa…`,
`…/gopi-ac391053…`, all between 01:23 and 02:00 today (before the archive of the brief's six).
Its callers are **task runs only**: `cmd/agent/cli_tasks.go:2581` (`gopi task run`'s close-out) and
`cmd/agent/cli_tasks_worker.go:184` (`workerRoundOwnWorktrees`, one per worker round). A chat's
close-out is `finishChat` (`cmd/agent/cli_chat.go:2154`) — it writes the CHATS row
(`:2159` `ag.CloseChatRow`) and prints the ground note (`:2168`), and calls **neither**.
**Reason it did not fire for the six: no chat process has ever called the pass; it only runs when a
task/worker round ends.** (And the worker rounds that *did* run it — `logs/worker.log:165584-165594`
— name only `gopi-firstopen`, `gopi-master-baseline`, `gopi-master-check`, `gopi-sync-help-row`,
`merge-run-judgement/land`: task worktrees, no `chat/` line anywhere in the log.)

### 3.5 one more arm, for completeness: the merge face's spent-branch prune — CANNOT SEE THEM

> *(The arm below was `gopi merge`'s; that top-level verb — and `cmd/agent/merge_run.go`,
> which the citation names — was removed on 2026-11-01. The arm is kept because the
> PRUNE is live: it moved into `internal/tools` with the rest of the engine, which
> `gopi task merge` runs today. The measurement below was taken against the old path.)*

`cmd/agent/merge_run.go:2055-2070` deletes a ref proved spent (`merge-base --is-ancestor`) — which
our `0 ahead` chat branches would be. But a candidate must first survive
`mergeCandidateBranches` (`cmd/agent/merge_run.go:2423`), which **skips every branch checked out in
a worktree** (`mergeWorktreeBranches`, `cmd/agent/merge_run.go:3012`, `held[b]` at `:2434`). A chat
ground is registered → all six are skipped before any lineage test runs.

---

## 4. Answer ④: what ends a chat session, and does any code run afterwards?

All exits, from `cmd/agent/cli_chat.go`:

| exit | what runs | source |
|---|---|---|
| `/quit`, `/exit`, Ctrl-D, EOF | `leaveChat` → `finishChat`: `CloseChatSession` (child stream flush), `CloseChatRow` (`:2159`), `chatLeaveGroundNote` (`:2168`) | `:2137`, `:2154` |
| **Ctrl-C at the prompt** (no turn in flight) | `signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)` (`:1007`) → watcher: `sig == os.Interrupt && claimTurnInterrupt(...)` is **false** → `ag.Interrupt()` + `close(interruptCh)` (`:1056-1057`) → REPL breaks → same `finishChat` | `:1007-1057`, `:1035-1053` |
| **Ctrl-C mid-turn** | the signal **claims the turn**: the turn is abandoned, the session stays open, the marker is printed (`:1050-1052`). No close-out. | `:1049-1053` |
| **SIGTERM** (any time) | same watcher, the `os.Interrupt` branch is skipped, `ag.Interrupt()` + `close(interruptCh)` (`:1053-1057`) → `finishChat` | `:1053-1057` |
| **SIGKILL / OOM-kill / crash / power loss** | **no code runs.** No handler can run; the CHATS row keeps `ended_at` NULL | — |

`chatLeaveGroundNote` (`cmd/agent/chat_ground.go:68`) is a **NOTICE, never an action** — its text
is `"[gopi] chat: this session's worktree %s (branch %s) is LEFT IN PLACE — nothing was deleted;
commit it there, or remove it yourself, if you want it gone.\n"` (`internal/tools/chat_ground.go`
`chatGroundExitNoticeFmt`). So the answer to "does anything clean" is **no, on every exit that runs
code at all, and on SIGKILL not even the notice**.

**The `t.Cleanup`-does-not-run-under-SIGKILL family does NOT apply here, and the difference
matters**: in that family a *dirty* worktree is left behind and the in-use/dirty guards then
protect it forever. A chat ground here is **clean and 0-ahead**, so the guards are not what keeps
it — the *absence of an owner* is. The one shape where the two families meet is a chat that wrote
files: those are then the session's deliverable, and the existing dirt guards keeping it is
correct behaviour, not a leak. Nothing needs to change for that half.

---

## 5. Answer ⑤: what is NOT the bug, and does the creation path become dead code under Rule 37?

**Not the bug: creation.** The ground is deliberate (`internal/tools/chat_ground.go` file header;
`devdoc/chat-session-ground.md`), it closes a real defect (a chat's writes landing uncommitted in
the main checkout), it reuses the task path's primitives and adds nothing new (Rule 35 was
answered when it landed: no daemon, no key, no state file), and it degrades instead of refusing.
**The missing piece is the RECLAIM of an inert (clean, 0-ahead, ownerless) ground** — i.e. this is
a *policy* gap, not an *implementation* gap: the removal verbs, the audit records, the age floor
and the `branch -d` rung all exist. What is absent is a rule that names this shape (no ledger row +
0 ahead + session over) and an arm that is allowed to reach it.

**Rule 37, on this base: it does not exist yet, and nothing can evaluate it.** `GOPI.md`'s highest
rule at d1f885a8 is Rule 36 (`GOPI.md:1041`); `grep -rn "Rule 37"` finds only two briefings
(`briefings/brief-chat-session-lifecycle-who-cleans-up.md`, `briefings/brief-chat-reads-gopi-md-and-rule-37.md`).
Its sibling row `01a0e689-3724-76dc-b037-da846cf469c5` is `status: pending`, `runs: none`, and
`cmd/agent/chat_ops.go` — the file the brief names — **does not exist** in this tree. So on this
base:

* Rule 37 lands as **prose plus (at most) a read-injection**, not as an evaluable code path; a
  "Rule 37 exists → stop creating grounds" fix has nothing to call.
* Therefore **`ChatGroundFor` does NOT become dead code by Rule 37 landing**, unless the sibling
  task also changes the creation path — and the brief forbids that overlap on purpose.
* The owner's own direction (quoted in `briefings/brief-chat-reads-gopi-md-and-rule-37.md`: a chat
  must not commit, needs no branch/worktree, only dispatches tasks) would make the ground
  *unnecessary*. That is a **second task** and it is not this one. Until it lands: grounds keep
  being created.

**Consequence for the recommendation**: the two candidate fixes ("reclaim it" / "stop creating it")
are different tasks, and both are needed — one is immediate (grounds already on disk and grounds
created before Rule 37's enforcement), one is the sibling's. Do not merge them.

---

## 6. The named recommendation

### (a) — an EXISTING arm extended, with the (b) policy sentence it needs. (c) is not justified.

**The arm**: the worktree reap already wired to the resident tick
(`cmd/agent/task_lifecycle_tick.go:298` → `cmd/agent/cli_tasks.go:3546`) — it already reaches these
six, already keeps them for a *named* reason, and needs no new daemon, no new key, no new file
(Rule 35's four questions: nothing new is created at all). Three concrete changes, in order of
what they unblock:

1. **(a1) un-freeze the two process-global gates** — the reason four of the five arms are silent
   *today*. `worktreeSweepSelfGuestCheckout` (`internal/tools/worktree_sweep.go:717`) is resolved
   once per repo at `:1986` and must be **scoped to the registration's repo** (today one unrelated
   guest checkout freezes a whole repo); `worktreeSweepLiveSessionRow`
   (`internal/tools/worktree_sweep.go:1300`) must not answer for a live row whose deliverable,
   branch and repo are all unrelated to the registration under judgement (it currently returns on
   the first live row, `:1300-1308`). Neither change weakens a guard's *predicate*, which is why
   this is not the forbidden "weaken `finishCleanupWorktreeInUse`".
2. **(a2) give the branch a home on the reclaim rung** — the ref is deleted only on the
   *directory-gone* arm (`worktree_sweep.go:961`). Extend the existing reclaim verdicts
   (`worktreeRuleOwnerEnded` `:1116`, `worktreeRuleUnclaimed` `:1128` — both already mean "no row
   owns this, it is clean, it is old") so that, after the removal, a branch which is
   **0 ahead of the integration branch** (`git merge-base --is-ancestor`, the exact measurement
   `ArmMergeRunSpentBranch` already makes at `cmd/agent/merge_run.go:2055`) is offered to
   **`git branch -d`** — never `-D`, never `--force`: git's own refusal is the decision, exactly as
   the rung at `:961` already argues.
3. **(b, the policy sentence the rule can already evaluate)** — the shape to name is: *a
   registration that no ledger row owns, whose tree is clean, whose branch is 0 ahead of the
   integration branch, and whose session is provably over, is not a deliverable; it may be
   released.* That sentence is (b) in the brief's taxonomy — it is not new machinery, it is the
   missing **criterion** that currently exists only for `chat/*` by accident of naming. Recording
   it as a rule is what keeps the next surface (a `gopi ui`/web ground, if those carriers ever
   ground) from re-inventing this diagnosis.

**(c) is not warranted**: nothing here needs a new collector, a new store, a new scheduler or a new
key; the collector exists, ticks on the resident hot path, and the only reason it does nothing is
that two guards answer a *different question* than the one the registration raises. Rule 35's
answer is therefore "no new dependency" — and the recommendation is exactly the arm-extended shape
Rule 35 prefers.

**Not recommended, deliberately**: deleting `chat/*` refs by pattern (a name is not a criterion —
Rule 22's own lesson: check the property, not the spelling), and any "age-only" arm that would drop
the ledger rung, since the ledger rung is what protects a live task's ground.

---

## 7. Verification (no code changed)

```console
$ go build ./... ; echo "build_exit=$?"
build_exit=0
$ go vet ./... ; echo "vet_exit=$?"
vet_exit=0
$ git status --porcelain        # in this worktree, after the report is committed: empty
```

Both run at base **d1f885a8** by this task's own worktree
(`/home/user/workspace/gopi-01a0e68b7e5d72c9983808d0e9c2bf91`); **no `.go` file was modified** —
the only additions are this document and its `devdoc/DOC-INDEX.tsv` row (Rule 36).

The surface this task *did* execute for real (Rule 31), verbatim:

```console
$ bin/gopi-diag task sweep-worktrees --json              # the arm under diagnosis, dry run, read-only
$ bin/gopi-diag task sweep-worktrees --json --min-age-h 0
gopi task sweep-worktrees: invalid --min-age-h "0" (want a positive integer, hours)
```

(the second confirms the floor is not the thing being dodged — it cannot even be set to 0.)

## Evidence (self-filed)

| claim | instrument |
|---|---|
| 6 `chat/*` branches, all 0 ahead, all tipped at d1f885a8 | `git branch --list 'chat/*'` + `git rev-list --count master..<b>` (pasted §1) |
| 6 registrations, 0 holders, 0 dirt | `git worktree list`; `/proc/*/cwd` scan per id; `git status --porcelain` (pasted §1) |
| the six were minted 13:46:04–13:46:30 by `gopi` | `cat .git/logs/refs/heads/chat/*`, `date -d @1790574390` (pasted §1) |
| the six left no ledger row and no session dir | `select … from chats` → `NO ROW` ×6 (174 rows total, 0 with `chat/%`); `find /home/user/.gopi/sessions -name "<id>*"` → empty |
| creation path | `cmd/agent/cli_chat.go:983`, `cmd/agent/chat_ground.go:37`, `internal/tools/chat_ground.go:120,174`, `internal/tools/task_run_ground.go:583-600` |
| not stamped | `internal/tools/chat_ground.go:31` |
| finish-cleanup is ledger-keyed | `internal/tools/task_reclaim.go:965-976` (`WHERE deliverable LIKE '%worktree%' AND status IN (…)`) |
| scratch-gc's three roots | `cmd/agent/scratch_gc_tick.go:116,142,154,184` |
| reap IS reachable | `cmd/agent/task_lifecycle_tick.go:298` → `cmd/agent/cli_tasks.go:3546` |
| both keeps, pasted | `task sweep-worktrees --json` from two cwds (§3.3) |
| self-checkout is process-global | `internal/tools/worktree_sweep.go:1003`, `:1986`, `:717` |
| live-session returns the first live row | `internal/tools/worktree_sweep.go:1045`, `:1300`, `:1254` |
| age floor 24 h | `internal/tools/worktree_sweep.go:90`, `:1108` |
| branch deleted only when the directory is GONE | `internal/tools/worktree_sweep.go:922`, `:961` |
| producer pass works on chat grounds but has no chat caller | `logs/removal-audit.log` `arm=task-self-own-sweep … branch=chat/…` ×9; `cmd/agent/cli_tasks.go:2581`, `cmd/agent/cli_tasks_worker.go:184` |
| worker rounds name only task worktrees | `logs/worker.log:165584-165594` |
| merge skips worktree-held branches | `cmd/agent/merge_run.go:2423-2441`, `:3012-3025` |
| exit table | `cmd/agent/cli_chat.go:1007,1035-1057,2137,2154,2159,2168` |
| Rule 37 absent on this base | `grep -n "^## Rule 3[5-9]" GOPI.md` → 35,36 only (`GOPI.md:1041`); sibling row `pending` |
| build/vet | §7, pasted |

## Not Done (honest)

* **What produced the six grounds is not pinned.** The code path is (§2) and the batch shape is
  (§1: six runs inside 26 s, no CHATS row, no session dir, `cwd` = a real checkout). The
  *launcher* — a driver script, a hand-run loop, or a suite path that escapes `MockEnv` — was not
  identified, and the sweep records I could correlate (`logs/worker.log`, `logs/removal-audit.log`)
  carry no line for 13:46. This is a **separate defect** (a launch that bypasses the
  `GOPI_WORKTREE_ROOT` pin), worth its own task; it does not change §3's conclusions, because the
  residue was produced by a process that has now exited.
* **Why the six are clean is inferred from content, not observed live.** No process was watched
  writing in them; the reading is "their tree is `d1f885a8`'s tree, `.baseline/.evid/.evid2` are
  tracked there (5417 files), `git status --porcelain` is empty".
* **No fix was implemented** (out of scope by the brief, and §6 is the deliverable instead).
* The `--min-age-h 0` probe was rejected by the flag parser, so the "aged out" hypothesis is
  argued from `worktree_sweep.go:90`'s 24 h constant rather than from a forced run — forcing it
  would have meant an APPLY sweep against live registrations, which the brief forbids.
