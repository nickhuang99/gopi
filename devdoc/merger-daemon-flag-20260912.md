<!-- doc-class: historical -->
# Task Report: `gopi merger --daemon` — top-level merger + self-daemonization

> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** Standing claim
> made before the change it describes: the resident merger — detached start,
> loop, tick, take-over launcher, `--stop`/`--restart` control face and
> `--status` surface — was closed (`f9ff1a89`) and deleted (`55c4d6f3`). Every
> spelling of `gopi merger` now refuses with one recorded sentence
> (`mergerRetiredCause`), and the one-tick merge is the bare `gopi merge`
> (`cmd/agent/merge_run.go`); the merger WORK LOCK survives. See
> `devdoc/merger-retired.md` and the dated correction in
> `devdoc/merger-self-driving.md`.

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Epic: merger-daemon-flag (2026-09-12) — branch `feat/merger-daemon-flag`,
worktree `~/workspace/gopi-mergerdaemon`.

## The five design questions

### 1. Why top-level `gopi merger` and not `gopi task merger`?

Because the merger is a **SERVICE, not a ledger verb** — the same argument that
already put `gopi worker` at the top level. A merger is an independent resident
process a host starts from its own service unit; it merges every branch it finds
in the `settings.json` `"repos"` sweep list **whether or not any ledger row
tracks that branch** (`repo_sweep`, architecture-fix-v2). Grouping it under
`task` implied a dependency on the task ledger that does not exist.

History (verified with `git log --all -S`, not recalled): the daemon was born
`gopi task serve` and renamed `gopi task merger` by the serve/worker-split
(2026-09-03) — while `gopi worker` was made top-level **in the same split**. The
group placement survived the rename; that inconsistency (peer service top-level,
sibling not) is the defect this epic fixes. It is a **promotion, not a
regression** — the verb was never top-level before.

`gopi task merger` and the older `gopi task serve` remain **working deprecated
aliases** (dispatch unchanged, foreground semantics byte-identical), and the
family face now prints a one-line deprecation note on stderr naming the new home.

### 2. How is the flock owned across the fork/exec?

**The parent acquires; the child inherits.** `mergerDaemonSpawn` calls the
existing `acquireServeLock()` in the process the operator is waiting on, then
passes the open lock file (fd → `ExtraFiles[0]` → child fd 3) to the re-exec'd
child. The child re-takes the *same* flock directly on the inherited fd
(`mergerDaemonInheritLock`, no path re-resolution, no reopen) and stamps **its
own pid** into the file.

Why this shape, and not "child acquires after forking":

* the acquire is a **single** call, made once, by a process whose exit path is
  trivially correct — there is no window where two processes both pass the
  check, and no post-fork re-race;
* a **failed** acquire happens *before* anything is detached, so the refusal is
  reported to the caller synchronously with a non-zero exit (the acceptance
  requirement "a `--daemon` start while a merger already holds the lock reports
  failure to the operator");
* flock ownership follows the **open file description**, which `ExtraFiles`
  duplicates — so the fork/exec cannot open a hole. Closing the parent's copy
  (which the parent does right after `Start`) does **not** release the lock,
  because the child's duplicate still references the same description.

Proof it holds (three layers, all in the devdoc's evidence below):
the E2E asserts (a) the lock file names the **daemon's** pid while the spawning
parent is long gone, (b) a second `gopi merger --once` exits non-zero naming that
pid, and (c) a second `gopi merger --daemon` is refused **non-silently**.

### 3. Why a pipe handshake instead of "fork and hope"?

Because the brief's second requirement — *the invoking shell returns with a
success/failure signal* — is unsatisfiable after a plain fork: the caller cannot
distinguish "the daemon is sweeping" from "the daemon died on the flock three
milliseconds in". Both look identical (`the command returned`).

So the parent keeps one end of a pipe; the child writes **one byte** after it has
(a) re-taken the inherited flock, (b) opened the log, (c) restamped its pid —
i.e. once it is provably the running daemon. The parent's single `Read` is the
start proof. On failure the child writes `FAIL: <reason>` instead, and the parent
prints it and exits non-zero. A child that dies without a word is caught by EOF,
and a child that hangs is caught by the `mergerDaemonHandshakeWait` (30s) bound —
in both cases the parent reports and exits non-zero. **No path returns 0 without
positive proof.** A failed start also SIGTERMs + reaps the child
(`mergerDaemonAbandonChild`) so a refused start leaves no process behind.

### 4. Why stdin closed and where does stdio go?

* **stdin = `/dev/null`** (`cmd.Stdin = nil`): a daemon must never consume the
  caller's terminal input, and must not hold a pipe open. Combined with
  `SysProcAttr{Setsid: true}` the child becomes a session leader with no
  controlling terminal — it survives the shell's exit and every HUP, **without
  an external `setsid`** (which is the point of the flag).
* **stdout + stderr → `<gopi data root>/logs/merger.log`** — resolved through
  `config.GopiDir()`, so it is `GOPI_HOME`-aware exactly like every other
  gopi-data path (a sandboxed merger never writes into the real profile), and
  the location is **chosen by the daemon, not the caller** — the drift the brief
  called out. One file per **merger**, not per start: an operator has one place
  to read, and a restart keeps history. The parent's success line names the
  absolute path, so nobody has to know the convention.

The daemon is stopped by an **explicit pid** (SIGTERM); the success line prints
it. Nothing in this feature ever signals by pattern.

### 5. What stays exactly as it was?

* **No flag = foreground, blocking**: `runTaskMerger` keeps its own
  `acquireServeLock()` + `defer lock.Close()` and the identical loop. The
  detached path shares the same loop body (`runTaskMergerWithLock`), so
  `--once`/`--every` semantics cannot drift.
* **`--once` unchanged** — one pass, exit 0, no scratch sweep (the E2E seam).
* **The aliases keep their historical behaviour**, including rejecting
  `--daemon` (a hint points at the top-level verb rather than a bare
  `unknown flag`). Only the top-level face accepts it.
* **Diagnostics name the verb actually typed** (`opts.invokedAs` →
  `gopi merger:` vs `gopi task merger:`), and the shared refusal message was
  de-hardcoded to "another merge daemon is already running … refusing
  double-serve" so it is true for both spellings.

## Deliverables

| # | Deliverable | Where |
|---|---|---|
| 1 | Top-level verb + deprecated alias + completion + help | `cmd/agent/main.go` (`isMergerSubcommand`, `mergerUsageHead`), `cmd/agent/cli_verbs.go` (`CliVerbMerger`), `cmd/agent/cli_completion.go`, `internal/config/help.go` |
| 2 | Self-daemonization (setsid + stdio + pipe handshake) | `cmd/agent/cli_merger_daemon.go` (new) |
| 3 | Flock inherited across fork/exec, second merger refused | `mergerDaemonSpawn` / `mergerDaemonInheritLock` / `mergerDaemonRestampPID` |
| 4 | Tests | `cmd/agent/merger_daemon_flag_e2e_test.go` (new, real-binary E2E) |
| 5 | This devdoc | `devdoc/merger-daemon-flag-20260912.md` |

Constant discipline (Rule 1/13/14): every new verb, flag, env name, fd number,
path, mode, timing and byte is a named constant in the two owning files
(`CliVerbMerger` in the verb SSOT `cli_verbs.go`; the `mergerDaemon*` block in
`cli_merger_daemon.go`); the re-exec argv is built from the `mergerDaemonArgs`
list, so a new loop flag cannot be forgotten. No bare `"merger"`, `"--daemon"` or
fd literal appears at a call site.

## Verification

### Build / format

```
$ go build ./...                 # clean
$ gofmt -l ./cmd ./internal      # empty
$ go vet ./cmd/agent/            # clean
```

### Tests

```
$ go test -count=1 -timeout 900s -run 'TestMergerDaemon' ./cmd/agent/
ok  github.com/nick/gopi/cmd/agent  15.260s
```

Full touched-package run (`cmd/agent`, `internal/config`, `internal/standards`,
`internal/stylegate`): see the RESULTS section at the end of this document.

### Real-binary E2E (evidence)

Script: `~/e2e-mergerdaemon/smoke.sh` — a private `GOPI_HOME` with an **empty
`repos` table**, so nothing real is ever swept. Run twice (the first run caught
two genuine defects, listed under *Bugs found*).

```
### 1. gopi merger -h (zero side effects)
GoPi merger — the branch-merge DAEMON (a resident server-side service).
exit=0
lock exists after -h? NO

### 2. foreground --once (unchanged semantics)
[serve] [adopt] skipped: worktree dirty (adoption refuses a dirty tree)
[merger] [repos] no configured repos in settings repos table — nothing to sweep
[merger] --once: single pass done
once-exit=0

### 3. --daemon returns promptly
out: gopi merger: starting a detached merger daemon (--every 5m)
merger daemon started: pid 913135, log /home/user/e2e-mergerdaemon/.gopi/logs/merger.log
daemon-exit=0 wall=.163815639s
parsed pid=913135
ps:  913135       1  913135  913135 Ssl  gopi-merger merger --every 5m

### 4. second --once refused (foreground face)
gopi merger: another merge daemon is already running (lock .../serve.lock held by pid 913135)
             — refusing double-serve: two mergers (or a legacy `task serve`) never double-merge
second-once-exit=1

### 5. second --daemon refused, non-silent
gopi merger: daemon start failed: another merge daemon is already running (lock .../serve.lock
             held by pid 913135) — refusing double-serve: ...
second-daemon-exit=1

### 6. deprecated alias still works + says so
gopi task merger: deprecated alias — the merge daemon is now the top-level `gopi merger`
                  (see `gopi merger -h`)
gopi task merger: another merge daemon is already running (lock .../serve.lock held by pid 913135)
alias-once-exit=1
alias-daemon-exit=1   (with the pointed hint — see below)

### 7. daemon log
[merger] pass start 06:41:58
[merger] [repos] no configured repos in settings repos table — nothing to sweep
[merger] sleeping 5m0s

### 8. stop by EXPLICIT pid (never pkill)
alive after TERM? NO
```

Reading of the evidence:

* **`ps` line `913135 1 913135 913135 Ssl`** = ppid **1** (reparented to init —
  fully detached), pgid = pid (process-group leader), sid = pid (**session
  leader**, i.e. setsid took), state `Ssl` (sleeping, session leader,
  multi-threaded). The caller's shell was never in that session.
* **`wall=.16s`** — the invoking shell returned in 164 ms while the daemon is
  still alive, which is the whole point of the flag.
* **`held by pid 913135`** — the live daemon's pid, not the (dead) spawning
  parent's. This is `mergerDaemonRestampPID` working.

The daemon's own log shows the config-driven pass on its `--every` cadence, i.e.
the detached child really is running the merger loop (not just alive).

### Bugs found and fixed during verification

The real-binary E2E earned its keep — both of these were invisible to unit tests:

1. **Handshake fd number (first smoke run: `daemon did not report readiness
   within 30s`, while the daemon was in fact running).** The parent stamped the
   child's env with `readyW.Fd()` — the *parent's* fd number. `ExtraFiles`
   **renumbers**: the child always sees the first entry as fd 3. The child's log
   said `handshake write failed: … bad file descriptor`. Fix: encode the
   CHILD-side numbers (`mergerDaemonLockFD`=3, `mergerDaemonReadyChildFD`=4) as
   constants, since os/exec guarantees that numbering. Guarded by
   `TestMergerDaemon_DetachesAndSecondIsRefused` (an unresponsive child makes
   `--daemon` return non-zero *and* exceed the prompt budget).
2. **Stale pid in the lock file.** `acquireServeLock` stamps the *parent's* pid;
   the parent then exits, so every refusal would name a dead process. Fix:
   the child restamps its own pid once it owns the lock. Guarded by the E2E's
   "the lock file names the LIVE daemon" assertion.
3. (Found by the alias E2E, not the smoke script) `gopi task merger --daemon`
   printed a bare `unknown flag --daemon`. Fix: `mergerDaemonFlagHint` turns it
   into a pointed sentence.

### Honest boundaries

* **How to confirm it is running**: `gopi merger --daemon` prints `merger daemon
  started: pid N, log PATH`; or `ps -o pid,sid,args -C gopi-merger` (argv[0] is
  the stable constant `gopi-merger`, deliberately not the ephemeral build path);
  or check that the lock file contains a live pid.
* **How to read the log**: `tail -f <gopi data root>/logs/merger.log`
  (`~/.gopi/logs/merger.log` by default). The path is always named in the start
  line, so the convention never has to be remembered.
* **How to stop it**: `kill -TERM <pid>` with the pid from the start line or
  `ps`. There is no pidfile and no `--stop` flag — deliberate: the pid is
  printed, and the flock file also names it. **Never** a pattern kill: this host
  runs live merger/worker daemons.
* **Log rotation is not implemented** — `merger.log` grows unbounded. It is a
  single line per merge decision plus one per pass tick, but a long-lived daemon
  on a busy host will accumulate. Out of scope here; the honest position is
  "rotate it externally (logrotate) or restart the daemon".
* **The `--every` interval is not restarted/rescaled on SIGHUP** — no reload
  support; restart to change it.
* **The handshake bound is 30s** (`mergerDaemonHandshakeWait`). A child that is
  alive but wedged at startup makes the *caller* wait 30s and then reports
  failure (the child is then SIGTERMed and reaped). A healthy start is ~150ms.
* **`--daemon` is top-level only.** The aliases refuse it on purpose, to keep
  their semantics byte-identical for running deployments.
* **Not proven here**: the daemon running INSIDE a supervisor (systemd unit) —
  the flag exists so a supervisor is not needed. And `--daemon --once` is a legal
  but pointless combination (a detached one-shot); it is not refused.

## Files touched

| File | Change |
|---|---|
| `cmd/agent/cli_merger_daemon.go` | **new** — spawn/handshake/child half, all constants |
| `cmd/agent/main.go` | `CliVerbMerger` predicate + `mergerUsageHead` + dispatch wiring + flag hint |
| `cmd/agent/cli_verbs.go` | `CliVerbMerger` (top-level verb SSOT) |
| `cmd/agent/cli_tasks.go` | `runTaskMergerWithLock` split; `mergerProg*` consts; `invokedAs`; deprecation note; de-hardcoded refusal; `--daemon` hint |
| `cmd/agent/cli_completion.go` | `CliVerbMerger` in `topLevelCommandNames` |
| `internal/config/help.go` | top-level "Resident SERVICES" merger block (`--daemon`), `task merger` demoted to a documented alias |
| `cmd/agent/merger_daemon_flag_e2e_test.go` | **new** — 6 real-binary Hermetic E2E/focused tests |
| `cmd/agent/cli_completion_test.go`, `cmd/agent/usage_test.go`, `cmd/agent/cli_contracts_test.go` | drift guards updated for the new verb |
| `internal/standards/baseline.go` | re-pinned (line shifts in `internal/config/help.go`) |

## RESULTS

```
$ gofmt -l ./cmd ./internal           # 0 lines
$ go build ./...                      # BUILD-OK
$ go test -count=1 -timeout 1800s ./cmd/agent/ ./internal/config/ \
      ./internal/standards/ ./internal/stylegate/
--- FAIL: TestMergeVerifyGatePassKeepsMerge (10.84s)
FAIL  github.com/nick/gopi/cmd/agent  224.240s
ok    github.com/nick/gopi/internal/config    0.781s
ok    github.com/nick/gopi/internal/standards 27.427s
--- FAIL: TestStyleGate (16.36s)
FAIL  github.com/nick/gopi/internal/stylegate  18.696s

$ go test -count=1 -timeout 900s -run 'TestMergerDaemon' ./cmd/agent/
ok  github.com/nick/gopi/cmd/agent  15.260s        # this epic's own tests: PASS
```

**Both remaining failures were verified PRE-EXISTING on the shared repo's
master** (i.e. not caused by this branch — and I checked by RUNNING master, not
by assuming):

```
# in the MAIN repo, master @ bbe60f7f:
--- FAIL: TestMergeVerifyGatePassKeepsMerge (25.30s)
    merger_verify_e2e_test.go:152: a PASSING verify must KEEP the merge
--- FAIL: TestStyleGate (16.36s)   # internal/agent/cov_agent_cr_test.go:909
```

That matches master's own tip commit message:
`docs(salvage): round 10 — final acceptance gate on master is RED and
attributable to three post-salvage merges`. Note the verify E2E failure is in
`merger_verify_e2e_test.go`, a DIFFERENT file from this epic's
`merger_daemon_flag_e2e_test.go`, and this branch does not touch
`internal/agent/` at all (`git diff master...HEAD -- internal/agent/` is empty).

Both are therefore recorded as **out of scope, attributed to master**, not
papered over and not silently ignored.

## Live-ness check (never disturb the operator)

The host runs LIVE `gopi worker` daemons (`/home/user/work/GoPi/bin/gopi
worker`, ppid 1) and a foreign interactive `gopi merger` (pid 950367, child of
someone's bash). Nothing in this work signalled by pattern: the E2E always
stops its own daemon by the EXPLICIT pid from the start line, and a scan of
every process's environment for `GOPI_HOME=/home/user/e2e-mergerdaemon` found
ZERO leftovers at the end. The foreign pid 950367 was observed to confirm it
was not mine (parent = a bash, not a test), and left strictly alone.

### Edge cases (verified on the real binary)

```
### --daemon --every nope   (must fail fast, spawn NOTHING)
gopi merger: daemon start failed: invalid --every "nope" (want e.g. 5m, 30s, 1h)
exit=1 wall=.31s daemons=0

### foreground --once --every nope   (unchanged)
gopi merger: invalid --every "nope" (want e.g. 5m, 30s, 1h)
exit=1

### --daemon --once   (a legal detached one-shot)
merger daemon started: pid 979008, log …/logs/merger.log
exit=0
one-shot daemon gone after its pass? YES
```

The first case is the reason `mergerDaemonSpawn` validates the loop flags
BEFORE it forks: had it not, the parent would have reported "started" (exit 0)
and the child would have died a moment later on the bad duration — a silent
success for a daemon that does not exist (Rule 15). The validation and the
loop now share one resolver, `mergerInterval` (Rule 2: single source of truth
for `--every`).

### 4th bug found during verification

**`--every nope` under `--daemon` reported success.** The duration was parsed
only INSIDE the loop (after the handshake), so the child failed *after* the
parent had already answered "started". Fix: `mergerInterval` (one resolver)
called by both the pre-spawn validation and the loop. Guarded by the E2E's
prompt-budget + "no daemon left behind" assertions.

### Baseline re-pin (honest note)

`internal/standards/baseline.go` carries two `file:line` permits that moved
because this change inserted lines earlier in the same files. Both were
RE-VERIFIED as the same code, not new violations:

| Permit | master | after this branch | site |
|---|---|---|---|
| Rule 16 (%w) | `cmd/agent/main.go:515` | `cmd/agent/main.go:646` | the `readPayloadFile` `@<path>` wrap (mentionsError false positive — it already wraps with `%w`) |
| Rule 18 (else-after-return) | `cmd/agent/cli_tasks.go:1161` | `cmd/agent/cli_tasks.go:1167` | the attachment-briefing `else if len(replicaPaths) > 0` guard |

`internal/stylegate` still reports ONE hit,
`internal/agent/cov_agent_cr_test.go:909` — proven PRE-EXISTING and
attributable elsewhere: the file is byte-identical to master
(`git diff master -- internal/agent/cov_agent_cr_test.go` is empty) and the
shared repo's own master tip is
`bbe60f7f docs(salvage): round 10 — final acceptance gate on master is RED and
attributable to three post-salvage merges`. Master fails it too. Nothing in
this branch touches `internal/agent/`.

## ⚠ Coordination note for the PA / merger (READ BEFORE ACCEPTING)

There is a **second worktree working the same area**:
`/home/user/workspace/gopi-merger-top` on branch `fix/merger-top-level-verb`
(5 commits ahead, NOT an ancestor of master — I verified with
`git merge-base --is-ancestor`, not by eye). Its diff touches the same
surfaces — `cmd/agent/cli_verbs.go`, `cmd/agent/main.go`,
`cmd/agent/cli_tasks.go`, `cmd/agent/cli_completion.go`,
`internal/config/help.go`, plus `cmd/agent/merger_top_level_e2e_test.go` and
`devdoc/merger-top-level-verb.md` — i.e. the **same epic's top-level promotion
half**.

Consequences:

* Merging both will conflict in those files (same regions: the dispatch chain
  in `main()`, `CliVerb*` constants, the top-level help text and the completion
  list). Resolve by taking ONE implementation of the promotion, not by
  concatenating both.
* The two overlap cleanly in INTENT — that branch has the promotion, this
  branch has the promotion **plus `--daemon`**, plus the flock-ownership and
  start-proof work the epic is actually about. If both are accepted, this
  branch's `isMergerSubcommand` / `mergerUsageHead` / `CliVerbMerger` are the
  versions to keep (they are the superset), and the other branch's
  `merger_top_level_e2e_test.go` should be folded in rather than dropped —
  more coverage of the same surface is fine, duplicate implementations are not.
* I deliberately did NOT wait for or merge that branch: it is not mine to
  accept, and the epic was assigned to me as a standalone deliverable.

## Branch / recovery (Rule 6d deliverable declaration)

* **Branch**: `feat/merger-daemon-flag` @ `393916b7`, in the SHARED repo
  (`/home/user/work/GoPi`) — the worktree `/home/user/workspace/gopi-mergerdaemon`
  is linked, so **no push was needed and none was done**; the commit is already
  visible to master's repo.
* **Recovery**: `git show feat/merger-daemon-flag:devdoc/merger-daemon-flag-20260912.md`
  (and the same with `cmd/agent/cli_merger_daemon.go`).
* **master was never touched**: `bbe60f7f` before and after; the worktree's
  `git status` is clean and `git reflog show master` shows no entry from me.
* Not merged (that is the PA's call, per Rule 9 the safe-merge gate must run
  first).
