<!-- doc-class: historical -->
# devdoc: the merger as a PURE cron launcher

> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** The follow-up note
> below has itself been overtaken: `gopi merger --every D` is **gone**, not
> reduced — the resident merger's entry points were closed (`f9ff1a89`) and its
> implementation deleted (`55c4d6f3`). The one-tick pass this document argues for
> survives as the bare **`gopi merge`** (`cmd/agent/merge_run.go`); the merger's
> single-instance WORK LOCK survives (`cmd/agent/merger_work_lock.go`, pinned by
> `merger_work_lock_e2e_test.go`). **Every test name cited below is dead** — of
> the four launcher/tick cases this doc leans on, none exists any more; the
> surviving lock/liveness invariants are `TestMergerWorkLock_MergeFacesRefuseWhileAHolderRuns`
> and `TestMergerWorkLock_RefusesInProcess`. The retirement record is
> `devdoc/merger-retired.md`; the flag-classification table with the same dated
> correction is `devdoc/merger-self-driving.md`. What still reads as current
> truth here is the ARGUMENT (a one-tick merge driven by the operator/scheduler,
> with judgement in a prompt instead of a liveness heuristic) — which is exactly
> what the surviving `gopi merge` face is built on.

> **FOLLOW-UP (2026-09-18, epic `merger-self-driving`).** Everything here stands
> about ONE tick. What came back is the REPETITION of that tick: `gopi merger
> --every D` runs it in-process, in the foreground, under a flock taken once and
> held for the loop's whole life, ended by a signal. It is a sleep parameter, not
> a control face, so no machinery this document removed returns with it —
> `devdoc/merger-self-driving.md` has the classification table, the measured lock
> window and the defects the loop surfaced.

Epic: **merger-pure-cronjob** (2026-09-17) · branch `feat/merger-pure-cronjob`
(worktree `~/workspace/gopi-merger-cronjob2`) · briefing
`briefings/brief-merger-as-cronjob-gopi-driven.md`

## 1. The change in one paragraph

`gopi merger` used to be a process that **decided**. It swept every configured
repo, triaged each branch, judged whether a peer holding a worktree was alive or
stuck, and chose what to do when nothing could land. It now **launches**: it
takes the single-instance flock, runs the master-safety repair pass, starts ONE
short-lived `gopi` carrying the take-over prompt on its stdin, and exits —
holding no child handle, waiting on nothing, deciding nothing about branches.
All branch judgement moved out of Go and into a shipped text artifact.

The Go change is deliberately thin (a launcher + three gates). **The prompt is
the deliverable.**

## 2. Why — the three measured incidents

A Go "is that process alive or stuck?" heuristic is the wrong instrument on this
host, measured three times:

1. **verify-gate futex deadlock** — the merge verify child deadlocked in futex;
   the parent's liveness read said "alive" and waited for ever.
2. **4/4 workers stuck in futex after SIGTERM** — every worker looked alive by
   pid and by `/proc/<pid>` while making no progress at all.
3. **pid-exists-but-dead ambiguity** — a pid that exists with a stale
   `/proc/<pid>/stat` reading is indistinguishable, from Go, from a slow one.

Each incident is recorded in `cmd/agent/merger_takeover.go`'s header, next to
the code that replaced the heuristic. The replacement is not a better heuristic:
it is a **model that reads the evidence itself** and is explicitly allowed to
answer *undecidable*, which no boolean ever was.

## 3. The prompt (`cmd/agent/merger_takeover_prompt.md`, 20624 bytes)

Section-anchored with invisible HTML comments so a coverage gate can hold it
honest while a reader sees clean markdown:

```
<!-- takeover-section: §3.5 peer-dead -->
## §3.5. ★ Is a peer DEAD? (the hardest judgement — read this twice)
```

Coverage (gate-enforced, `merger_takeover_prompt_gate_test.go`):

| section | source |
|---|---|
| §0 mission, §1 orient | added — the briefing assumes orientation but never states it |
| §3.1 peer-alive | briefing §3.1 — five machine facts, ordered by usefulness |
| §3.2 worktree | briefing §3.2 — occupied / dirty / empty-shell / orphan / detached |
| §3.3 triage | briefing §3.3 — ancestry, `rev-list --count`, the `merge-tree` ban, the real `--no-commit --no-ff` probe |
| §3.4 time-cap | briefing §3.4 — self-imposed cap, no archive tag, irreconcilable conflict |
| §3.5 peer-dead | briefing §3.5 — the dead / alive / **undecidable** ladder |
| §3.6 rework-queue | briefing §3.6 — merge-base anchor → diff → attachment → `gopi task submit` |
| §3.7 resume | briefing §3.7 — name the session id, never resume a peer's |
| §3.8 flow | briefing §3.8 — serial flow, `go vet` not just `go build`, revert a bad merge |
| §3.9 serial-lock | added — how to take over asynchronously *and* still serialise git writes |
| §3.10 envelope | added — no resident processes, off-`/` scratch, never kill a peer |
| §3.11 report | added — the tick's only durable output and its shape |

The gate also pins 22 verbatim **behaviours** (`--is-ancestor`, `merge-tree`,
`--no-commit --no-ff`, `rev-list --count`, `archive/`, `20 minutes`, `futex`,
`/proc/<pid>`, `undecidable`, `merge-base`, `gopi task submit`, `git apply`,
`gopi resume`, `go vet ./...`, `go build`, `revert -m 1`, `gopi merger --once`,
`gopi merger --status`, `~/workspace/`, `MERGER TAKE-OVER REPORT`,
`gopi task for-branch`, `orphan`): a section that survives while its instruction
disappears is still a lost rule.

## 4. The launcher (`cmd/agent/merger_takeover.go`)

```
flock → lifecycle record → master safety (repair repos a dead predecessor left mid-merge)
      → start ONE gopi → write the start line → return
```

Design decisions worth keeping:

- **The child is identified by its `argv[0]`**, not an env var:
  `mergerTakeoverArgv0 = "gopi-merger-takeover"`. An env marker would be
  inherited by every gopi the take-over itself starts, and each would be briefed
  as a take-over. `argv[0]` is set at exec and cannot leak.
- **The take-over briefing is the injection seam**: `readInput()` asks
  `mergerTakeoverInjectedBriefing()` FIRST, so a take-over is briefed from the
  shipped prompt before stdin or any positional. Frame (repo list + measured
  detach) is generated in code; the judgement is the artifact.
- **The detach outcome is MEASURED and told to the child** (`session-scope` /
  `scope`), never promised. When the cgroup escape is refused the degradation is
  announced verbatim, per Rule 24/25 — no invented remedy, no privileged fix.
- **The repo list crosses the boundary as an env var the child validates**
  (`GOPI_MERGER_TAKEOVER_REPOS`, newline-separated): an entry that is not an
  absolute path is dropped **and named on stderr**. The list is resolved once, in
  `mergerMasterSafetySweep`, and reused (Rule 2).
- **The child is started with `cmd.Start()`, never `Run()`/`Wait()`**, and the
  env has the inherited task-report contract stripped
  (`reportContractStripAndLog`) so a spawned gopi can never write onto a peer's
  row.

## 5. Reliability analysis of the peer-dead criteria

The prompt's ladder is ordered by **what a wrong answer costs**, which is the
property that matters when the criteria are imperfect:

| reading | verdict | cost if wrong |
|---|---|---|
| `/proc/<pid>` absent | DEAD | wrong only if pid reuse; the row is re-checked before any write |
| `/proc` present + **futex** in `wchan` | DEAD (the measured incident) | the incident's own signature — this is what Go could not see |
| `/proc` present + HEAD moved | ALIVE | safe direction: leave it |
| `/proc` present + no movement, no futex | **UNDECIDABLE → skip** | safe direction: a skipped branch is a deferred merge |

Every uncertain case fails toward *skip*, and §3.5 states outright that
**skipping is a successful tick**. A take-over can therefore never be wrong in
the direction that destroys work; its worst outcome is a branch merged one tick
later than it could have been.

## 6. What the live runs showed (three scenarios, real model calls)

Fixtures: throwaway repos under `~/workspace/_mtev{,2,3}` (never the live repo —
a take-over's own-worktree-release pass would enumerate the developer's
worktrees). Each tick driven through the real `gopi merger` verb.

**S1 — a live peer (the "don't touch it" case).**
Repo had `feat/ff-clean` (fast-forwardable, no holder) and `feat/peer-work`
checked out in a worktree **with an uncommitted `wip.txt`**.
Tick output (2.1s, exit 0):

```
[merger] master safety [.../repo]: tip aeade39925e2, no unfinished merge, tree clean
gopi merger: take-over detach degraded: cgroup scope escape unavailable: … permission denied
  — the child stays in this login session scope, keep the tick short-lived
gopi merger: take-over started pid=2560858 detach=session-scope log=…/merger-takeover.log
  — this process holds no child and will not wait
```

The child (pid 2560858, **ppid=1** once the tick died) landed `feat/ff-clean`
(`master` → `700d1f2 feat: ff-clean`), **left `feat/peer-work` and its worktree
untouched, and never touched `wip.txt`** (still `uncommitted peer edit`).
Tool trail (session `3515f4b0…`): `bash` 14, `memory` 6, `task` 3, `task_submit`
1, `context_trim` 1.

**S2 — a peer whose holder is dead (the "take it over" case).**
`feat/dead-holder` in `wt-dead`, clean, no process anywhere in that worktree, no
ledger row. The take-over's own reasoning, from its log:

> "The only 'peer' is my own take-over task row … — that's this session's row,
> not a peer. … no process has cwd in _mtev2/wt-dead. … the worktree is clean …
> **worktree-held branches: the merger proper skips them. But my take-over
> instructions say: judge each with real evidence.** The branch IS FF-able."

Tool trail (session `fdd07a27…`): `bash` 37, `memory` 5, `task` 3,
`context_trim` 2, `read` 1. `master` stayed at `base` — the take-over decided
the holder's claim on the branch outranked a clean fast-forward, which is the
conservative reading and the one §3.5 authorises ("skipping is a successful
tick"). It reported the branch and the worktree as untouched.

**S3 — an unattributable branch (the case that found a prompt gap).**
`feat/unknown-holder` in `wt-unknown`, no ledger row, no live holder. The
take-over **landed it**: `master` → `f0f977c merge: feat/unknown-holder —
unattributable work (merger take-over tick)`. Tool trail (session `588ac750…`):
`bash` 33, `task` 8, `memory` 5, `context_trim` 1.

Its log then shows it re-read `master`, **found its own merge commit, and
attributed it to somebody else** — *"a PEER ran a merger tick and landed the
merge while I was orienting!"* — before debating whether to revert. That is the
real finding of this epic's live evidence: **the tick's write was correct, but
the REPORT did not make its own authorship of the write legible**, even to the
agent that made it. §1 was rewritten because of this run:

- the original §1 wording I drafted banned landing **orphan** branches outright —
  which would have been wrong, because S1's *success* requires landing exactly
  such a branch (a merged orphan is how a branch whose row was deleted still
  reaches master);
- the shipped §1 therefore keeps the landing but makes the **report line
  mandatory**: name the branch as an orphan, its author, its last commit, and
  the merge commit — "a merge is reversible, but a merge nobody was told about is
  not reviewable";
- it also added the recency guard (a branch born minutes ago is live work) and
  the ownership commands (`gopi task for-branch`, `git worktree list`,
  `git log -1 --format=…`).

**Honest gap:** S3 shows the take-over will land a clean orphan branch in a
fixture repo whose whole content is a test merge. Nothing in the prompt *forbids*
that, and the operator's standing rule "never merge without a ledger row" is
weaker than it reads: the merger has always swept orphan branches, and the
briefing's §7 explicitly *retires* the old park-without-a-row arm. If the
operator wants orphan branches left for a human, that is a one-line policy
change in §1 — it is a policy decision, not a defect I should have silently
guessed at. Proposed default (shipped): land it, name it loudly.

## 7. The gates, and their falsification

| gate | what it pins | falsified |
|---|---|---|
| `merger_tick_no_child_gate_test.go` | **spawns but NEVER waits** in `merger_tick.go` + `merger_takeover.go` (+ the narrower git-only list for `repo_sweep.go`), **with a mandatory presence control** so the absence half cannot pass by deleting the feature | ① injecting `c.Wait()` → red naming `merger_takeover.go`; ② deleting the `cmd.Start()` arm → red "the SPAWN is gone"; both green after restore |
| `merger_takeover_prompt_gate_test.go` | every required §-anchor + 22 verbatim behaviours + artifact ≡ embedded bytes | deleting the `§3.5 peer-dead` anchor → red "no longer carries required section"; green after restore |
| `merger_takeover_launcher_test.go` | acceptance (b) measured in `/proc`: the tick returns, the merger pid is gone, the child's `argv[0]` is the take-over marker and its image IS the published stub, nothing live owns it | the assertion fired for real on the first run (a shebang stub's `$0` is not `argv[0]`) — see §8 |
| `TestMergerLockFDIsNotInheritedByTheTakeover` | the launcher's header claim that the lock fd is O_CLOEXEC — read directly with `F_GETFD`, with a usability precondition so the flag reading cannot be vacuous | added because the header cited a test that **did not exist** (see §8); the instrument itself is proven to discriminate — `F_GETFD` reports `FD_CLOEXEC=1` on a Go-opened fd (PEP 446) and `0` after an `F_SETFD 0`, so the assertion can go red |

The no-child gate was **reconciled, not relaxed**: the old version banned all
spawning, which the launcher must do. The new one states the property that
actually has to hold, and the presence control makes "delete the feature" a
failure rather than a pass.

## 8. Traps found while building (so the next agent does not re-find them)

- **A shebang script does not receive `argv[0]`.** The first launcher test used
  a `#!/bin/sh` stub; the kernel execs `/bin/sh ./stub`, so `$0` is the script's
  own path and the marker check failed. The stub is now a compiled Go program.
  (This also meant the test caught a *real* property rather than a rig artifact.)
- **`git checkout -- <path>` cannot restore an untracked file.** During gate
  falsification the injections went into untracked files, so the restore silently
  did nothing and a later run failed for the wrong reason. Backup/restore by
  copy, or commit first, then falsify.
- **A bare tick still owns the two resident sweeps.** The scratch GC and the
  task-lifecycle sweep must ride BOTH faces: `TestMergerTickE2E_DisposesZombie…`
  and `TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound` drive a **bare**
  tick and require a confirmed zombie to be disposed and a stalled round
  recovered. They are local, bounded, judgement-free recovery — routing them
  through an agent would make recovering a dead round depend on a model call.
- **Old bare-tick sweep tests had to move to `--once`.**
  `TestMergerSingleton_TwoConcurrentInvocations` asserts a bare tick *lands a
  branch*; that is now the `--once` face. The test's real subject is the
  single-instance flock, so it now drives `--once` — the face that takes the
  same lock and does the sweep — rather than being deleted.
- **`perl -0pi -e` with a `§` in the pattern** needs the byte-exact text; the
  anchor is `<!-- takeover-section: §3.5 peer-dead -->` (spaces matter).
- **A hand-spelled hold-fd env name silently disabled the stub.** The stub read
  `GOPI_DAEMON_LIFETIME_HOLD_FD`; the real constant is
  `daemonlifetime.HoldEnvName` = `GOPI_DAEMON_DEATHSIG_HOLD_FD`. The stub found
  it empty, returned at once — and every `/proc/<pid>` read afterwards raced a
  dead process. The stub source is now a format string and the name is injected
  from that single definition (Rule 13 exactly: a constant bypassed by a literal
  fails silently and the failure looks like a race).
- **`/proc/<pid>/exe` vanishes the moment the child exits**, so using it to prove
  "which image did the launcher start" is a race. The child now reports its own
  `os.Executable()` in its self-report file — the same fact, readable after it
  is gone.
- **NEVER edit the repo while `go test ./cmd/agent/` is running.** The suite
  compiles the package once and then executes for minutes; an edit mid-run (to
  the prompt artifact, whose embedded copy is frozen at compile time while the
  coverage gate reads the file at run time) produces an *impossible* green — and
  a crop of unrelated failures that look real. Measured: one mid-run edit made
  12 tests fail, including three whose only "defect" was my own concurrent
  write. Run the suite on a frozen tree, and commit before it.
- **A stale `bin/gopi` makes the suite look broken, and the failure looks like
  your change.** Ten tests failed on the first full-suite run; one root cause was
  that the published binary at `bin/gopi` predated the branch, so every test that
  drives the real verb exercised the OLD launcher. Rebuild before believing a
  suite failure that names behaviour you did not touch. (Rule 12 in reverse: the
  merge path rebuilds; a worktree run does not.)
- **A test that reads an artifact from a relative path breaks the moment a
  sibling test `os.Chdir`s.** The prompt gate opened
  `cmd/agent/merger_takeover_prompt.md` relative to the cwd; another test in the
  same package chdirs, so the gate passed or failed by luck of ordering. Fixed by
  resolving the path from `runtime.Caller(0)` — a test's own file location, not
  the process cwd. Ship the minimal repro when fixing this (a `t.Chdir` + read).
- **A comment that cites a test which does not exist is worse than no comment.**
  My own launcher header asserted "merger_takeover_launcher_test.go proves the
  O_CLOEXEC bit mechanically" — and that test was never written. Found by
  grepping my own citation. The test now exists (`F_GETFD` on the lock fd, with
  an flock-usability precondition so the flag read cannot pass on a dead
  descriptor). **When a comment says a test proves something, grep for that
  test before you trust the sentence.**

## 9. Disposal recommendation (not executed — an operator decision)

Measured on the main repo (`~/work/GoPi`) at `ee1d0fd9`:

- **157 `archive/*` tags**, many doubly prefixed (`archive/archive/…`) — the
  namespace has been applied twice to some branches. They are history and are
  never a skip predicate (the prompt says so explicitly), so they can be kept
  without harm; if the tag list is to be pruned, prune by *date*, not by name
  pattern, and never delete a tag whose branch is not provably merged.
- **6 branches not merged into master**: `feat/merger-pure-cronjob` (this work),
  `feat/worker-positional-taskid`, `feat/worker-term-exit`,
  `fix/heavy-tests-classification-rework`, `fix/sweep-memory-base-label`,
  `merge/conflict-sweep-01a0adc9`. Each needs the §3.3 treatment (ancestry →
  real probe → land / rework row), which is exactly what the take-over is for —
  **the recommendation is to let the take-over process them** rather than prune
  them by hand here.
- **Orphan worktrees** under `~/work/_verify/` and `~/workspace/` (detached HEADs
  at `f9fb7546`, `ee1d0fd9`, `ba53da3d`) are not held by a live process and are
  candidates for `git worktree prune` after the branches above are settled.

## 10. Files

| file | change |
|---|---|
| `cmd/agent/merger_takeover_prompt.md` | **new** — the shipped take-over prompt (the epic's deliverable) |
| `cmd/agent/merger_takeover.go` | **new** — the launcher (spawn, log, detach, briefing injection) |
| `cmd/agent/merger_takeover_prompt_gate_test.go` | **new** — prompt coverage + artifact≡binary |
| `cmd/agent/merger_takeover_launcher_test.go` | **new** — acceptance (b), measured in `/proc` |
| `cmd/agent/merger_tick.go` | cron face = safety + launch + return; `--once` keeps the synchronous pass; both faces keep the resident sweeps |
| `cmd/agent/main.go` | take-over briefing injection in `readInput`; `mergerUsageHead` states launch-and-leave |
| `cmd/agent/merger_tick_no_child_gate_test.go` | reconciled to "spawns but never waits" + presence control |
| `cmd/agent/merger_singleton_e2e_test.go` | drives `--once` (the face that sweeps) for its flock assertions |
