<!-- doc-class: historical -->
# Merger ⇄ takeover: the operator's design vs the code (alignment audit)

Task: `gopi merger algorithm confirm` (session `01a0b1c5`), branch
`fix/merger-design-alignment`, worktree `~/workspace/gopi-merger-design-audit`,
base `master` `8b0011a3`.

This audit is **read-only against `master`**: it changes no production code, no
test and no constant. Every claim carries the `file:line` or the command that
produced it, so a reviewer can re-measure instead of trusting prose.

---

## 0. The verdict in one screen

| # | the operator's principle | verdict | decisive evidence |
|---|---|---|---|
| 1 | merger = self-scheduled long-resident process whose only duty is: wake on an interval, check whether a takeover runs, if not spawn one | **MINOR-DIFF** | `merger_loop.go:238-257` is exactly tick→sleep; the tick does `gate → spawn` (`merger_tick.go:208,216`). But the same tick ALSO does two task-plane sweeps (`merger_tick.go:188-189`) — the operator's own complaint in task `01a0b19d` |
| 2 | merger is a flock singleton; a second instance is refused | **ALIGNED** | `cli_tasks.go:4334` `LOCK_EX|LOCK_NB`; refusal text names the holder; pinned by `TestMergerSingleton_*` |
| 3 | takeover is a singleton: guaranteed by merger's singleton **plus a before-spawn takeover-status check performed BY the merger** | **ALIGNED (stronger than stated)** | `merger_takeover_gate.go:99-142` asks first and fails CLOSED; the takeover additionally HOLDS the inherited flock (`merger_takeover_lock.go:24-33`) |
| 4 | no system-cron dependency; a plain sleep-like interval loop is enough | **ALIGNED** | `merger_loop.go:200-257` (in-process `for` + `time.After`); no unit/crontab installed for it |
| 5 | takeover: ff-merge, minimum-effort resolve, rework task with the diff as attachment, delete **every** processed branch, success or failure | **ALIGNED IN PRINCIPLE, 2 exception classes** | prompt §3.3.1 (ff-first), §3.6 (rework+attachment), §3.6.1 (delete is the endpoint, cases ①–④); exceptions ⑤/⑥ are the prompt's own explicit design |
| 6 | merger reports takeover statistics in `--status` (incl. zombie monitoring), and the takeover reports per-merge progress so it is not killed by mistake | **ALIGNED** | `--status` renders identity triple + counters + the "may have stopped working" edge (`merger_takeover_live.go:474`, edge = 15m, `merger_takeover_progress.go:190`); takeover rows are `in_progress`-only in the zombie sweep → never auto-killed |

Three gaps are real and are dispatched (§4). One is a 4th: the audit's own doc
family contradicts the shipped flags (§3.5).

---

## 1. Principle 1 — the merger as a self-scheduled launcher

**Aligned part.** `runTaskMerger` (`merger_tick.go:78-108`) → `--every` →
`mergerResidentLoop` (`merger_loop.go:200`). The loop body is four statements:
reap backstop, `runTaskMergerWithLock(opts, lock)`, stop check, sleep
(`merger_loop.go:238-257`). The tick's own tail is one branch:
`gate(...)` at `merger_tick.go:208`, `mergerTakeoverRun(opts, lock, repos)` at
`:216`, then `return` — "no child handle, no wait, no child registry"
(`merger_takeover.go:302-312`, `merger_tick.go:38`).

**The takeover is the LLM-driven branch-merging worker with its own prompt.**
`cmd/agent/merger_takeover_prompt.md` (46650 B) is `go:embed`-ed
(`merger_takeover.go:288-291`) and injected ahead of stdin/positionals by
`mergerTakeoverInjectedBriefing()` (`:270-277`), keyed on `argv[0] ==
"gopi-merger-takeover"` (`:90`) — deliberately NOT an env marker, which a
grandchild would inherit. Its route differs from a common worker's: the
takeover is a **bare agent session with no task id** (`merger_takeover.go:433-436`),
so it cannot write back onto a peer's row.

**The drift the operator already filed** (task `01a0b19d`, human, priority 1,
pending): *"I feel above output of merger running exposes we add extra duty for
it to handle task sweeping work which should be another role … a general task
sweeping work should be handled by other role."* The code agrees with the
complaint, exactly:

```
merger_tick.go:188   scratchGCSweep(mergerRoleLabel)
merger_tick.go:189   runTaskLifecycleTick(mergerRoleLabel)
```

The second line is the task plane: stale-report + confirmed-zombie disposal +
finish cleanup, throttled by `taskLifecycleThrottleMin` on every merger tick
(`task_lifecycle_tick.go:197-210`). The code's own justification is
`merger_tick.go:171-189` ("LOCAL, BOUNDED, JUDGEMENT-FREE maintenance … the
host's scheduler is the only thing that runs when nobody is working"), and two
E2E tests pin it (`TestMergerTickE2E_DisposesZombieWithZeroWorkerProcesses`,
`TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound`). So the merger's
resident face is **not** "only spawn a takeover": it is "spawn a takeover, and
be the host's last-resort task janitor".

**Verdict: MINOR-DIFF, already owned by `01a0b19d`.** It is a small detail by
volume (two calls) but it is the operator's stated concern, and the decision is
theirs — the code has a defensible reason (no other resident component exists to
dispose a zombie when nobody is working). **Do not "fix" this blind**: task
`01a0b19d` is the decision record.

## 2. Principle 2 — flock singleton

`acquireServeLock` (`cli_tasks.go:4142`) → `lockAndVerifyIdentity`
(`cli_tasks.go:4329`) does `syscall.Flock(fd, LOCK_EX|LOCK_NB)` (`:4334`), and on
refusal builds a diagnosis from the descriptor, wrapping `errServeLockHeld`
(`:4338-4343`). A **stray-holder** check refuses even a *won* lock when the path
was unlinked underneath a live holder (`:4350-4353`, the 2026-09-12 incident).

Two nuances, both deliberate and both consistent with the design:

- the takeover's own faces (`gopi merger --once`, `gopi task merge`) are admitted
  WITHOUT taking the lock when a live **ancestor** holds it
  (`cli_tasks.go:4157-4168`, `mergerLockHeldByAncestor`) — read from the kernel,
  not from an env var. This is what stops the child from deadlocking against its
  own launcher.
- `ensureServeLockIdentity` re-proves the identity after acquisition and on the
  loop's entry (`cli_tasks.go:4359+`, `merger_loop.go:218`), so an unlinked lock
  file stops the merger instead of silently detaching the guard.

**Verdict: ALIGNED.**

## 3. Principle 3 — takeover singleton, the merger doing the checking

The operator's sentence is implemented literally and in two layers:

① **The merger asks first.** `mergerTakeoverGateForLauncher`
(`merger_takeover_gate.go:99-109`) resolves the log path, scans the process
table through `tools.MergedTakeoverEvidenceForLauncher()` (`:105`), and decides
with `takeoverGateDecision` (`:132-142`): first entry that is MINE and either
`!Measured || Running` ⇒ `(pid, true)` ⇒ the tick prints the skip and returns
(`merger_tick.go:208-215`). `maxTakeoversPerTick = 1` is a named constant
(`:83`). Fail-closed on: unreadable table, unresolvable log path, unmeasurable
state (`:100-108`) — "the cost of a wrong skip is one interval; the cost of a
wrong spawn is two merge rounds writing refs".

② **The takeover additionally HOLDS the lock**, so the property is not only a
pread check but a kernel guarantee: the launcher hands its flock descriptor to
the child (`merger_takeover_lock.go:24-33`), which adopts it
(`mergerTakeoverLockOwnerEnv` + inherited fd) and re-arms `FD_CLOEXEC` for
everything IT spawns so a grandchild can never keep the singleton alive
(`merger_takeover_lock.go:35-40`).

Two ordering details that matter for the letter of the design: the takeover
request was implemented with a **pre-spawn gate rather than flock-first** — the
merger's flock IS held while the takeover runs, but the merger's *decision* to
spawn is a status read, not a lock wait. And the gated tick runs the master-safety
sweep and both resident sweeps BEFORE the gate (`merger_tick.go:167-189`), so a
skipped round still repairs a predecessor's unfinished merge.

**Verdict: ALIGNED — the design's "merger checks takeover status before spawn" is
met, and the singleton is stronger than stated (pread + inherited flock + gate).**

## 4. Principle 4 — zero system dependency

The loop is `for { tick; sleep(D) }` in-process (`merger_loop.go:238-257`);
`mergerSleepOrStop` is `select { signal | time.After(interval) }`
(`merger_loop.go:275-289`) — no request file, no watchdog goroutine, no
supervisor. Default period `tools.MergerIntervalDefault` = 5m0s, with ONE reader
(`internal/tools/merger_schedule.go`, shared spelling `MergerIntervalFlag`).

**Measured on this host:** no `gopi merger` crontab line and no systemd unit.
`crontab -l` has exactly one gopi entry — `gopi-scratch-gc-watchdog.sh`
(30 min), an unrelated sweep; `~/.config/systemd/user/` has no merger unit.
`gopi merger --status` at audit time: `merger: not running`, `lock holder: pid
410497 (stale: the process is gone; the lock is FREE)`, and an independent
`flock -n ~/.gopi/serve.lock` probe answered `FREE` — i.e. the merger is not
running and nothing external is driving it.

**The remaining host dependency is the operator's launch, and it can stay
external:** the `--restart` face is documented as spawning the successor with
`setsid` (`merger_restart.go:~400`, "the operator's own `setsid nohup`" in
`main.go:662`). Under Rule 24 that child stays in the launcher's
`session-*.scope`; a reboot consequently ends the loop. That is a *launch*
dependency, not a cron dependency, and the operator's principle does not forbid
it (they said `setsid nohup` themselves). It is reported, not filed.

**Verdict: ALIGNED.** "No dependency, just a sleep-like while-loop" is literally
the implementation.

## 5. Principle 5 — the takeover's merge semantics

### 5.1 ff-first, minimum effort

`§3.3.1` (`prompt:259-310`) is a hard ORDER, not advice: triage → land every
fast-forwardable branch **in one batch** (`git merge --ff-only`, then
`branch -d`) → only then the conflict branches, easiest first. The conflict test
is the real probe (`git merge --no-commit --no-ff` + `--abort`), with `git
merge-tree` explicitly banned with the two measured reasons (prompt:236-249).
Budget: per branch ~20 min, whole round ~45 min, self-enforced
(`prompt:336,339`) — consistent with "minimum effort to merge".

### 5.2 rework as a task, diff as attachment

`§3.6` (prompt:399-431) + `§3.6.1` case ④ (prompt:471-522): anchor →
`git diff` → **file** the rework task with `--attachment` → READ the success
line (`Task queued: <id>`) → *then* delete. The payload tells the reworker to
re-implement from current master and explicitly **not** `git apply` the diff.
This is exactly the operator's "submit diff as attachment as task to ask worker
rework".

### 5.3 deletion sweep — and the two exceptions the prompt itself carves

§3.6.1 states the operator's principle almost verbatim ("Once that is settled —
one way or the other — the branch is a leftover, and leftovers are deleted.
**Delete is the endpoint of the lifecycle, not a suppression.**"), then defines
six cases: ① merged `-d`, ② ancestor `-d`, ③ empty shell `-d`, ④ rework filed
`-D`, **⑤ undecidable → touch nothing**, **⑥ live peer holds it → touch
nothing**.

So the letter of the operator's design ("no branch is useful after merging, also
useless if unable to merge — delete in both cases") IS the rule for ①–④, and
⑤/⑥ are the deliberate, reasoned exception ("宁可少做，不可误杀"; the branch stays
in the sweep set and is *re-reported* each tick so the loop's owner sees it).
The operator allowed exactly this kind of difference ("it can differ in small
detail and report"). I report it; I do not touch it.

**Verdict: ALIGNED IN PRINCIPLE.** The only *unfinished* part of the operator's
intent is the branch↔task linkage that makes ⑤/⑥ rarer — `--status` currently
reports 4 branches on this repo as `owner-record-only` ("a ledger row remembers
it and nothing holds it — registration repair"), and the writer half is already
dispatched as `01a0b1ab` ("stamp the branch↔worktree linkage at CLAIM time",
Rule 26 re-dispatch of the failed `01a0a9b1`). Nothing to duplicate here.

## 6. Principle 6 — statistics in `--status`, progress from the takeover

**Status side.** The takeover block carries the three-factor identity
(`pid`+`starttime`+`cmdline`, each factor named as UNREADABLE when it was not
read — `merger_takeover_live.go:226,335-365`), the measured process state
(`MergerTakeoverStateExited = "exited (zombie — terminated, not yet reaped by
its launcher)"`, `:108`), age, cgroup, session and log attribution, then the
round's own counters attached from ITS control directory
(`attachTakeoverProgress`, `:366`; record in
`internal/tools/merger_takeover_progress.go:272-320`). The liveness edge is
explicit:

```
merger_takeover_live.go:473-476
  (⚠ no record update for %s — it may have stopped working; judge it the way the
   take-over's own §3.5 requires and DO NOT kill it on this line alone)
```

**Takeover side.** `gopi merger takeover-progress init|stage|done|remaining|show`
(`merger_takeover_progress.go:448-527`), mandated by prompt `§3.10.1`
("after EVERY concluded branch"), with the operator-facing reason stated to the
agent in its own words: *"It is your liveness evidence … an un-updated record is
what makes that question unanswerable."* The record is only amendable while the
recorded process still runs, compared on the identity triple
(`TakeoverProgressRoundLive`, `internal/tools/merger_takeover_progress.go:440-462`).

**The "not killed by mistake" half is also mechanical, and stronger than the
operator asked for.** Every takeover gets an auto-registered ledger row
(`merger_takeover.go:433-436`, `submitted_by: harness auto-registration`), but
the zombie sweep only ever queries **`status='in_progress'`** rows
(`internal/tools/taskzombie_sweep.go:155-157`, `args = [taskStatusInProgress]`).
Measured over the ledger: **80 takeover rows, all `completed`, zero ever failed
or claimed** (`select run_cmd,status,count(*) … group by status`), and no
`in_progress` takeover row exists now. So the task-zombie machinery cannot
dispose a take-over at all; the only thing that can end one is the agent's own
§3.4 budget or an operator.

**Verdict: ALIGNED.**

---

## 7. Gaps found, and their disposition

Each row is a fact I measured, plus the disposition. `KEEP` = correct as is,
documented so nobody "fixes" it. `DISPATCH` = filed as its own task (§8).

| # | gap | evidence | disposition |
|---|---|---|---|
| G1 | **A wedged take-over blocks every later round, forever.** The gate's only question is "is one of mine still RUNNING"; a live-but-stuck round answers "running", so every tick skips and no new round starts. The round's own stale edge is rendered as a ⚠ sentence and nothing acts on it. `--stop`/`--restart` cannot conclude either while the child holds the inherited flock (already filed: `01a0b17c`). | `merger_takeover_gate.go:132-142` (`if !e.Measured || e.Running`); `merger_takeover_live.go:473-476`; `01a0b17c` pending | **DISPATCH** — the operator asked for exactly this duty: "monitor possible zombie of takeover" |
| G2 | **Under the resident loop face, the merger never runs `mergerPassOnce`.** `merger_tick.go:195` returns from the launch branch before `:244`, so `mergeDeadHolderReclaimLeg`, `mergerResumeDeadHolderLeg`, `serveAdoptionSweep`, `serveGitSyncSweep` and `mergerPassCloseLoop` (the declared-rebuild drain) ride ONLY on the `--once` face. The prompt tells the takeover to use `--once` for the REBUILD (`prompt:136-143`), never that it is also the sole carrier of those four legs. | call sites: `cli_tasks.go:2704,2716,2726,2727,2746` behind `merger_tick.go:195-216`; prompt §2 only | **DISPATCH** — either state it as intended (and say so in the prompt) or restore the legs on the loop face |
| G3 | **`--status` prints `merger: not running` when the lock is held by a stale pid.** Measured, twice: during a `--restart` probe ("pid … still holds … after 1m0s") the live loop read as not running; on this audit's host a gone pid 410497 is named as holder with `merger: not running`. Also `TestMergerStatusDoesNotCallAZombieRunning` is RED at base. | `--status` output quoted in §6/§7 below; test run in §9 | **DISPATCH** (small, but the operator's principle 6 makes `--status` the monitoring instrument, so it must not under-report) |
| G4 | **`devdoc/merger-self-driving.md` documents `--stop`/`--restart` as RETIRED; the code RESTORED them** two days later (epic `merger-loop-restart`, 2026-09-19). `main.go:396,438-443,494` and `cmd/agent/merger_restart.go:1-40` are the current truth. | doc table lines 18-21 vs `main.go:513-515` (only `--daemon --start --wait --force` retired) | **DISPATCH** — docs are the next agent's only window; the stale table is a trap |
| G5 | The merger's resident face is the host's last-resort *task* janitor (`scratchGCSweep` + `runTaskLifecycleTick`, `merger_tick.go:188-189`). | code + the operator's own filing `01a0b19d` | **KEEP as-is, decision with the operator** (`01a0b19d` already pending; do not pre-empt) |
| G6 | ⑤/⑥ (undecidable / live peer) keep branches in the sweep set, so the operator's "delete every processed branch" is not literal. | prompt:471-533 | **KEEP** — the prompt reasons it; the operator allowed small differences |
| G7 | The pre-spawn gate is a status read, not a lock wait; and the gated tick does master safety + both sweeps before asking. | `merger_tick.go:167-216` | **KEEP** — a skipped round still repairs a predecessor; documented in the gate header |
| G8 | A reboot ends a loop launched `setsid nohup` (same `session-*.scope`), so the operator must relaunch. | Rule 24; `main.go:662` | **KEEP / report** — a launch dependency, not a cron dependency |

---

## 8. Dispatched work

Filed from this audit via `task_submit` (ids in the run report; each briefing is
self-contained, per Rule 26):

- **G1** — `01a0b1cd-b8ad-7cb3-94ec-e54fdaf4b32e` (bug): take-over wedge watch —
  give the merger's `--status`/gate a bounded, evidence-gated answer for a round
  that is ALIVE but has stopped updating its record, without ever auto-killing a
  *working* round.
- **G2** — `01a0b1cd-bfc6-79f2-8734-a524eede0b62` (bug): the loop face's missing
  legs — establish intent for `mergerPassOnce`'s legs riding only on `--once`,
  then restore them or make the obligation explicit in the take-over prompt.
- **RESOLVED (2026-09-19, task `01a0b1cd-fa6a-7bc9-81c0-1ccc54150c92`).** G4 is
fixed: `devdoc/merger-self-driving.md`'s table now carries the `--stop`/`--restart`
restoration in the row itself plus a dated `CORRECTION` block — and the correction
is wider than the filing first saw, because the restoration it named was itself
overtaken the same day: the whole resident merger is retired (`f9ff1a89`,
`55c4d6f3`), so no row is a live face and the doc's cited test names are dead.
The other rows of this table are NOT re-verified here (they were measured at
`45c6ecc1`/`5cdae25e`; two of them — G1, G2 — name code that the retirement has
since deleted, so they are moot rather than open).

**G3** — `01a0b1cd-f713-7907-8839-6a8def46f37a` (bug): `merger: not running`
  under a live-loop / stale-holder read; plus the base-red
  `TestMergerStatusDoesNotCallAZombieRunning`.
- **G4** — `01a0b1cd-fa6a-7bc9-81c0-1ccc54150c92` (docs): refresh
  `devdoc/merger-self-driving.md`'s flag table to the shipped
  `--stop`/`--restart`.

Not duplicated (already pending, cited for the reviewer): `01a0b19d` (merger's
extra duties — the operator's own filing), `01a0b1ab` (branch↔worktree linkage
at claim time), `01a0b1b3` (merge-conflict route: adoption's resolve lane mints
86 rows for 70 branches vs the take-over's 4 rework rows), `01a0b17c`
(`--stop`/`--restart` cannot conclude while a spawned take-over holds the
inherited flock), `01a0b165` (base-failing restart E2E), `01a0b120` (foreign-peer
count attribution in `--status`).

---

## 9. Verification (what this audit observed)

```
$ go build ./...            # in the worktree — clean (see the run report)
$ git status --porcelain    # in the worktree — the doc commit only, nothing else
```

Two suite failures were observed and are **pre-existing at `master` `8b0011a3`**
(they are not caused by, and not fixed by, this audit):

```
--- FAIL: TestMergerRestartDoesNotTouchTheTakeoverInFlight (65.68s)   [cmd/agent]
    merger_restart_e2e_test.go:728: `--restart` exited 1:
      … pid 660234 still holds …/.gopi/serve.lock after 1m0s — the loop has not
      reached a tick boundary yet
--- FAIL: TestMergerStatusDoesNotCallAZombieRunning                    [internal/tools]
    merger_takeover_state_test.go:114: the terminated take-over is not named as
      "exited (zombie — terminated, not yet reaped by its launcher)"
```

Both are the SAME defect family as gap G3 (the status/stop path under a
holder that is present in the process table but not progressing), which is why
G3 carries their repro in its briefing instead of a fresh one.

`## Not Done (honest)`: this audit does **not** fix any of G1–G4 — it is an
alignment report plus the dispatches, exactly as the briefing asks. It also does
not re-open the operator's own pending decision in `01a0b19d`.

---

## 10. Post-verification against current `master` (the audit's base has moved)

This section is a **verification addendum**, written after re-measuring the
branch against `master` tip `45c6ecc1` (the audit's base was `8b0011a3`, five
merges earlier). It changes no verdict above; it records which of the two
base-failing tests this branch reported in §9 are still red at the new tip.

The intervening merges (`git log --oneline 8b0011a3..master`) include
`fix/merger-status-operator-text`, which reworked the exact surface §9 measured:

```
# measured 2026-09-18 in this audit's worktree, at master tip 45c6ecc1
go test ./cmd/agent/ -run 'TestMergerStatusDoesNotCallAZombieRunning|TestMergerStatus' -count=1
                                                                    -> ok  github.com/nick/gopi/cmd/agent  3.958s
git merge-base --is-ancestor d573e81a 8b0011a3                      -> exit 1 (NOT in the audit base)
git merge-base --is-ancestor d573e81a master                        -> exit 0
```

| §9 observation | state at `8b0011a3` (audit base) | state at `45c6ecc1` (current master) |
|---|---|---|
| `TestMergerStatusDoesNotCallAZombieRunning` red; pid 410497 named while the lock is free | RED (measured, §9) | **GREEN** — the asserting test moved to `cmd/agent` and passes (`84df4f02`, `d573e81a`) |
| `--status` prints `merger: not running` over a stale holder pid | reproduced on this host (§6) | **fixed for the stale case** — current output: `lock holder: pid 410497 (stale: the pid named here is gone; the lock is FREE)` |

**So G3's code half is already landed and does not need re-fixing**; the
dispatched row `01a0b1cd-f713-7907-8839-6a8def46f37a` should be re-read against
master tip before anyone acts on it. This is the "an incomplete fix is repaired
by dispatching again — never by waiting" path (Rule 26), applied in the
*withdraw* direction: the evidence says the fix landed, so the row's briefing —
not the audit — is what needs the update.

**Unchanged by the re-measurement:** the verdict table (§0) and gaps G1, G2, G4
all re-read identically at the new tip — G1 (`merger_takeover_gate.go`'s
`if !e.Measured || e.Running`), G2 (`merger_tick.go:195` returning before
`:244`) and G4 (the stale `devdoc/merger-self-driving.md` table) are all still
present in the tree at `45c6ecc1`.

## 11. P6 confirmed on the operator's flag, and one reader defect repaired

### 11.1 The operator's P6 flag, answered

The operator's sixth principle asked for two things, and §6 established both
exist. This session additionally confirmed the flag that proves it is the one
the operator's own command writes, not a look-alike:

| the operator asked for | where it is, measured | verdict |
|---|---|---|
| rich statistics under `--status`, to spot take-over zombies | `gopi merger --status` prints, per observed take-over, the identity triple (pid + starttime + cmdline), the measured process state — including `MergerTakeoverStateExited = "exited (zombie — terminated, not yet reaped by its launcher)"` — cgroup/session/log attribution, and the round's OWN counters attached from its control dir (`merger_status.go:496 attachTakeoverProgress`) | **ALIGNED** |
| the take-over reporting per-merge progress so it is not killed by mistake | `gopi merger takeover-progress init\|stage\|done\|remaining\|show`; the prompt mandates `init` as the round's FIRST call (§3.10.1) and `done` after EVERY concluded branch; `--status` renders the report with an explicit non-conclusion: `"(⚠ no record update for %s — it may have stopped working; judge it the way the take-over's own §3.5 requires and DO NOT kill it on this line alone)"` (`merger_takeover_live.go:474`), threshold `takeoverProgressStaleEdge = 15 * time.Minute` = half the prompt's 20-minute per-branch cap | **ALIGNED, mechanically stronger than asked** |

The "killed by mistake" route the operator feared does not exist in the code:
the zombie sweep queries only `taskStatusInProgress` (`taskzombie_sweep.go:155-157`)
while every take-over row in the ledger is already terminal — measured
2026-09-18, `sqlite3 /home/user/.gopi/memory.db "select status,count(*) from tasks where
run_cmd='gopi-merger-takeover'"` → 108 rows, **all `completed`, zero
`in_progress`, zero failed**. Host state at the same moment: `ps -eo pid,stat`
showed **zero `Z`-state processes**, and no merger/take-over was running (lock
free; the named holder pid was stale). No zombie, by either definition.

### 11.2 The one defect this session found and repaired: `show` answered with an errno

`takeover-progress show` was the **only** member of its family that did not speak
the family's vocabulary. With no record present — the ordinary first call of a
round, and the call the prompt recommends as "read your own record back" —
it printed the raw OS message and exited 1:

```
takeover-progress show: open /home/user/.gopi/merger/takeover-progress.txt: no such file or directory
```

Two things make that a defect rather than a style choice:

- Every sibling verb reports a missing/unwritable round through the shared
  honest-degradation line `takeover-progress: NOT RECORDED — <reason>`
  (`cmd/agent/merger_takeover_progress.go:101`), and `loadLocked` names the cause
  in words ("no takeover-progress.txt in <dir>", "the recorded take-over round is
  no longer running").
- The status surface reading **the same file** renders a curated sentence
  (`takeoverProgressAbsentNote`, `internal/tools/merger_takeover_progress.go:686`).
  Two readers of one record disagreed about the same errno.

Root cause: the classification of a failed read lived only inside
`ReadTakeoverProgress`, so the CLI re-derived it (badly) from `os.ReadFile`'s
error. Fix: ONE exported classifier,
`tools.TakeoverProgressReadFailureView(path, err)`, returning the state and the
note; `ReadTakeoverProgress` now delegates to it and `show` renders the note
through the family's warn prefix. Two readers, one vocabulary (Rule 14).

Evidence, and the red→green transition:

```
# RED — the verb's pre-fix body restored in isolation (git stash of the cli file):
--- FAIL: TestTakeoverProgressShowReportsTheCuratedCauseNotAnErrno
    show with no record must report through "takeover-progress: NOT RECORDED — " …;
    got "takeover-progress show: open …/.gopi/merger/takeover-progress.txt: no such file or directory"

# GREEN — with the fix:
go test ./cmd/agent/ -run 'TestTakeoverProgress' -count=1   -> ok  github.com/nick/gopi/cmd/agent  1.982s
```

The regression test (`TestTakeoverProgressShowReportsTheCuratedCauseNotAnErrno`)
asserts three things, so it cannot be satisfied by deleting the message: exit
non-zero, the family prefix present, **and** the errno string absent.

Live confirmation of the fixed verb (built binary, fixture profile):

```
GOPI_HOME=$(mktemp -d) gopi merger takeover-progress show
  -> stderr: takeover-progress: NOT RECORDED — no progress record yet — the
     take-over has not written one (a round started by a binary from before this
     record existed writes none at all)
  -> exit 1
GOPI_HOME=$(mktemp -d) gopi merger --status
  -> "merger:      not running"   (the same profile, the same absent file)
```

Note what this closes and what it deliberately does not: the fix makes the two
readers speak one vocabulary. It does **not** change the exit code (1 — a verb
that printed no record has not shown one) and it does not touch the round-liveness
guards, which §6 and §11.1 measured as correct.
