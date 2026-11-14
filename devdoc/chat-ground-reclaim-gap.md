<!-- doc-class: normative -->
# Chat grounds: a create path with no collect path (diagnosis, 2026-09-28)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Question.** `gopi chat` leaves orphan `chat/<session-id>` branches and
`~/workspace/gopi-<session-id>` worktrees: 0 commits ahead, no live holder.
Why does nothing collect them?

**Answer in one sentence.** Creation is deliberate and per-run; **reclaim is a
policy gap, not an implementation gap**: every removal arm that exists is
keyed on something a chat never produces (a ledger row, or a process standing
in the tree), and the arms that DO examine these registrations are
**process-global all-or-nothing gates** that go quiet only when the whole host
is idle.

---

## 1. Creation path (pinned)

| step | site |
|---|---|
| call, once per RUN, before the first turn | `cmd/agent/cli_chat.go:983` (`if ground, gerr := chatEnterGround(ag); gerr != nil`) |
| chat face | `cmd/agent/chat_ground.go:37` `chatEnterGround` → `:46` `tools.ChatGroundFor(sessionID, startDir)` |
| resolver / creator | `internal/tools/chat_ground.go:120` `ChatGroundFor` |
| name stem | `internal/tools/chat_ground.go:204` `chatGroundIDStem` → `internal/tools/task_run_ground.go:357` `taskGroundIDStem` (drops every non-alphanumeric ⇒ a v4 UUID becomes 32 hex chars) |
| branch | `internal/tools/chat_ground.go:174` `branch := ChatGroundBranchPrefix + stem`; prefix `= "chat/"` at `:78` |
| directory | `internal/tools/task_run_ground.go` `taskGroundDirFor` under `config.ResolveRepoWorktreeRoot` (`internal/config/config.go:1419`, default root `<home>/workspace`) |
| materialisation | `taskGroundCreate` (`internal/tools/task_run_ground.go:583-600`) → `git worktree add <dir> -b <branch> HEAD` |
| announcement / exit notice | `internal/tools/chat_ground.go:251` → `chatGroundExitNoticeFmt` (`:277`): *"is LEFT IN PLACE — nothing was deleted"* |

**Per-run, not per-session.** The name is derived from the **session id**, and
the resolver reuses an existing ground (`chat_ground.go:178-190`), so a resume
of the SAME session reuses its ground; a *new* run mints a new session id and
therefore a NEW ground. The six observed registrations = one run each.

**Carriers.** Only `runChatWithInput` calls it: `gopi chat` (lineedit),
`gopi chat --once`, `--turns`. `gopi ui` does NOT (`cmd/agent/cli_ui.go:136`
`runUIChat` → `internal/term/term.go:360-362`) and `gopi web` does NOT
(`cmd/agent/cli_web.go:1314-1316`).

**Nothing is stamped in the ledger** — `internal/tools/chat_ground.go:31`:
*"A chat is not a row."* That one sentence is the origin of every arm failure
below.

**How the ground is meant to end** (same file's design comment):
`internal/tools/task_run_ground.go:430-441` — *"The ground is the SESSION's,
and it is released when the session is. Nothing in this file schedules,
collects or expires it: the removal machinery already owns that
(`finishCleanupWorktreeInUse`, `ReclaimDeadHolderWorktrees`, the worktree
sweep)."* The duty was **assigned to arms that are ledger-keyed**. A chat
never enters their key space.

---

## 2. Exits — what happens, and what cleans up

All terminal exits converge on one closeout:
`cmd/agent/cli_chat.go:1379` / `:1537` / `:1548` / `:1562` → `leaveChat :2137` →
`finishChat :2154` → `CloseChatSession` + `CloseChatRow` + `closeChatDiagnostics`
+ `chatLeaveGroundNote(sess.ground)` (`:2168`).

| exit | does code run? | path | does anything clean the ground? |
|---|---|---|---|
| `/quit`, `/exit` | yes | `leaveChat` (`cli_chat.go:1379`), exit 0 | NO — notice only |
| EOF (Ctrl+D) | yes | `lr.eof` (`:1560`) → `leaveChat` | NO |
| read error (not EOF) | yes | `finishChat` (`:1557`) | NO |
| **Ctrl-C, idle at prompt** | yes | watcher `cli_chat.go:1007` (`signal.Notify` os.Interrupt + SIGTERM) claims no turn (`:1056`) → `ag.Interrupt()` + `close(interruptCh)` → REPL select breaks → `leaveChat` | NO |
| **Ctrl-C, mid-turn** | yes, but *not* a closeout | `claimTurnInterrupt` TRUE → marker printed, turn abandoned, **session stays open**; the operator must press again | NO |
| SIGTERM (any time, incl. mid-turn) | yes | never claims a turn → same closeout as idle Ctrl-C | NO |
| SIGKILL / crash / panic / OOM | **no code runs** | — | NO (no `session_end`, no CHATS update, no note) |

**Every row ends the same way: a NOTICE.** `chatGroundExitNoticeFmt` names the
worktree and the branch and says it was left in place.

---

## 3. Why each existing removal arm misses it

### 3a. `finish-cleanup` — UNREACHABLE

`finishCleanupWorktreeInUse` and its chain are **ledger-keyed**: they need a
row carrying `deliverable.branch` / `deliverable.worktree`. `chat_ground.go:31`
("a chat is not a row") means **no chat ever produces that stamp**. Measured:
`select count(*) from tasks where deliverable like '%chat/%'` → **0** over
**2868** rows. This arm is not "weak" for chats — it cannot see them.

### 3b. `scratch-gc` / `ReclaimDeadHolderWorktrees` — ABSENT

`internal/tools/scratch_gc_tick.go:116-194` sweeps the scratch home,
base-suites and sandboxes only. `ReclaimDeadHolderWorktrees` additionally
requires a **dead-holder ROW** — the same missing key.

### 3c. the worktree sweep — REACHABLE, and it keeps all six

`serveWorktreeReapSweep` (`cmd/agent/cli_tasks.go:3546`) IS reachable, and IS
called by `taskLifecyclePass` (`internal/tools/task_lifecycle_tick.go:298`) —
the tick the worker loop and the merger both fire, i.e. a resident duty. It runs
**ungated** on the repo it is asked about.

The sweep judges every registration through ONE armoury,
`worktreeSweepJudge` (`internal/tools/worktree_sweep.go:874`). Its first two
gates are process-global:

* **① self-checkout** (`:1004`, rule `self-checkout`): *this process stands in
  this registration's own repository* → keep. Not even scoped to the
  registration's repo.
* **② live-session** (`:951` dead-registration form, `:1044` live form; rule
  `live-session`): fires when **any** row in `live-reserved`/`claimed` status
  still has a recorded runner that is alive (`worktreeSweepLiveSessionRow`,
  `:1300` → `worktreeSweepRowSessionLive`, `:1172`).

Both `return` on the FIRST matching row, so **one unrelated live run freezes
every residue on the host**. Measured, verbatim, on this host:

```
$ gopi task sweep-worktrees --dry-run --json | (findings naming chat/*)
{"path":"/home/user/workspace/gopi-0f9994c3cf0d4a81a1c006dc4adc7620",
 "branch":"chat/0f9994c3cf0d4a81a1c006dc4adc7620","disposition":"keep",
 "reason":"no live task's deliverable names this registration, but live task
  01a0e4fe-2dad-7306-bf4c-1951caefb268 still has a run standing (its recorded
  runner (pid 341456) is still running) — its own session's ground",
 "age_hours":0,"rule":"live-session"}
```

…the same sentence for all six. The keeper is **unrelated** to every one of
them: `ps -p 341456` → `gopi worker`, elapsed 11:26:05. The sweep's own words
concede the mismatch — *"no live task's deliverable names this registration"*.
**The six are clean-able by construction** (0 commits ahead, no holders); they
are kept by a gate that is answering a different question.

`age_hours: 0` is a second, independent co-cause: the sweep's age floor
(`worktreeRuleTooYoung`) is evaluated in the *same* pass, so even with ②
satisfied a fresh registration is kept.

### 3d. the branch: no arm at all

Reclaim/reap delete the **worktree** (`reclaimGitRemove`,
`internal/tools/task_reclaim.go:1995` = `git worktree remove`, never
`--force`). The **branch** is deleted only on the *dead-registration* arm
(`worktree_sweep.go:961`: *"registration is dead (its directory is gone) — the
branch goes with it if git agrees (`branch -d`)"*), pinned by
`dirty_closeout_binding_test.go:137` and by the ZERO `git branch` count in
`worktree_sweep.go`. Six chat branches are **0 ahead of HEAD** and therefore
block nothing and are offered to nobody.

---

## 4. What the six residues actually are

The exact provenance is **not** provable from the artifacts (they carry no
session dir and no CHATS row). What IS measured:

* six branches `chat/<32 hex>`; `git rev-list --count HEAD..<b>` = **0** for
  every one; six registrations in `/home/user/work/GoPi`'s worktree list;
* **no** process has a cwd inside any of them (measured `/proc/*/cwd` scan);
* **no** row in `chats` (checked in *both* spellings — dashed, and the
  dash-stripped stem that actually names the branch) for any of the six;
* `GOPI_WORKTREE_ROOT` is **not** set in this session's environment, and the
  variable is spelled in exactly one place (`internal/worktreeroots/roots.go:136`);
  `testutil.MockEnv` pins it (`internal/testutil/run.go:129` →
  `worktreeroots.WorktreeRootEnvName+"="+t.TempDir()`), and the epic's own
  acceptance test relies on that pin (`testsuite/chat_ground_test.go:104-108`:
  *"MockEnv pins GOPI_WORKTREE_ROOT to a per-test temp dir, so the ground is
  created under the fixture and never in the operator's real workspace"*).

⇒ A chat launched **outside** `testutil.MockEnv` (bare `./bin/gopi chat`, or a
`chat --once` harness that pins `GOPI_HOME` but not the worktree root), with
cwd = a MAIN checkout, mints a ground in the real `<home>/workspace`.
**Note the count: six since 13:46 — during the epic that built this feature.
It is the epic's own footprint, not a background trickle.** *Caveat: I did not
find the harness path that produced them; that is an inference from the
evidence above, not a measurement, and it is reported as an inference.*

---

## 5. Recommendation — classified

**(a) EXTEND AN EXISTING ARM**, conditional on Rule 37 landing.

Extend the sweep's **dead-registration branch arm** (`worktree_sweep.go:961`,
which already runs `git branch -d`, never `-D`/`--force`) to a second subject:
a registration that is

1. annotated `refs/heads/chat/*`,
2. **0 commits ahead** of the repo's integration branch (git itself authorises
   the delete — `branch -d` refuses otherwise),
3. clean (`worktreeSweepJudge` guard ④ already refuses DIRT),
4. whose session is **provably over** (absent from `chats` in both spellings,
   or `ended_at` set) — never "old enough",
5. with **no** live process inside it (the `/proc/*/cwd` scan the sweep
   already owns, `processInsideSessionDir`, `:1319`).

**Why (a) and not (c).** Every ingredient already exists in
`internal/tools/worktree_sweep.go` — the registration list, the /proc probe,
the `git branch -d` path, the dry-run-with-rule-name report. Adding a *new*
collector would be a second implementation of each (Rules 13/14) and a new
persistent surface (Rule 35).

**Why it is conditional on Rule 37 — and why "creation becomes dead code" is
NOT automatic.** On this base (`b6be46d8`) `GOPI.md`'s highest rule is **Rule
36** (`grep -c "Rule 37" GOPI.md` → **0**); there is no evaluable code path
for it. Its sibling row (`01a0e689-3724`, pending) lands it as **prose**. So
`ChatGroundFor` is never told anything: if Rule 37 is prose only, chat grounds
keep being minted exactly as now, and (a) becomes load-bearing. **Rule 37
landing as evaluable policy is what makes the create path dead code** — and if
it does, this item is a no-op, not a liability.

**Ordering discipline.** This is deliberately **not** "delete the six now":
they are the measured evidence that the create path and the collect path
disagree. Collect them by the arm that ships, not by hand.

**Explicitly NOT recommended:** a daemon/cron/timer; a new config key or env
var; weakening `finishCleanupWorktreeInUse`; widening ①② to "probably
scratch" (they exist because a live process standing in a tree is the strongest
evidence the tree is in use — they are right, they are just **process-global**,
and narrowing their scope is the real fix, tracked separately).

---

## 6. Evidence index

* `git branch --list 'chat/*'` → 6; `git rev-list --count HEAD..<b>` → 0 ×6
* `git worktree list` → 6 registrations
* `gopi task sweep-worktrees --dry-run --json` → 18 findings; the 6 chat ones
  all `disposition:"keep"`, `rule:"live-session"`, citing unrelated row
  `01a0e4fe-…` / pid 341456 (`gopi worker`, 11:26:05 elapsed)
* `/proc/*/cwd` scan over the six directories → no holders
* `select count(*) from tasks where deliverable like '%chat/%'` → 0 (2868 rows)
* `select count(*) from chats where session_id = <id>` → 0 (both spellings)
* `grep -rn GOPI_WORKTREE_ROOT internal/` → one spelling
  (`internal/worktreeroots/roots.go:136`), one test pin
  (`internal/testutil/run.go:129`)
* `go build ./...` exit 0; `go vet ./...` exit 0 at `b6be46d8` (no code changed)
