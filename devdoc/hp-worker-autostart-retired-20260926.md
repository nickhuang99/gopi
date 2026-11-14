<!-- doc-class: normative -->
# The HP worker autostart is RETIRED (2026-09-26) — the net is gone, and the file says why

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## The ruling

Operator, 2026-09-26, verbatim and not up for re-litigation:

> 「这个 cronjob 必须终止，我们不能依赖这种系统来做，它掩盖了问题。」

Translated for the record: this cronjob must end; we cannot depend on this kind of
system; it covered the problem up.

## The measurement that produced it (recorded once, never re-derived)

- `crontab -l` on HP carried `*/10 * * * * /home/user/bin/hp-worker-autostart.sh`.
- Its log `~/.gopi/logs/worker-autostart.log`: **36,398 lines, 1,214 delegations.**
- The shape, from the log itself:

  ```
  [12:50:01] no worker alive, 3 pending — delegating to launcher 1
  started worker 1: pid 2709846
  live gopi workers: 0 (pids: none)
  [12:50:05] launcher accepted: 1 worker(s) …
  [13:00:01] no worker alive, 3 pending — delegating to launcher 1
  ```

- Those pids are gone. The zero count was **honest**: something was killing the
  workers and the cron line kept replacing them — 1,214 times. For an unknown
  period it silently converted "workers keep dying" into "there is always a
  worker alive", which is why the underlying failure was never diagnosed.
  掩盖了问题 is a measurement here, not a figure of speech.
- The cron line was already removed (backup
  `~/workspace/crontab.bak-20260926-214318.txt`). This task makes the retirement a
  fact of the REPOSITORY, so a rebuild, a re-install or a new HP cannot silently
  re-arm it.
- The defect class is Rule 35's (a persistent dependency added to make something
  reliable) — the scheduled line was added in 2026-09-10 to make two starters
  reliable and it masked a worker-death loop instead. `briefings/brief-rule35-dependency-gate.md`
  records that history; this retirement closes it.

## What changed

### 1. `scripts/hp/hp-worker-autostart.sh` — the retirement gate

A scheduler context REFUSES LOUDLY (Rule 15): non-zero exit, a message naming the
ruling and the reason, and **no launcher call**. The gate has exactly two arms:

| arm | what it reads | why a crontab cannot forge it |
|---|---|---|
| `scheduler-ancestry` | the kernel's process table (`/proc/<pid>/stat`) walked up from `$$` for `cron` / `atd` / `anacron` | a wrapper script's own parent is still cron, whatever the wrapper sets — the tell is WHAT THE CALLER IS, never what the environment says |
| `no-terminal` | `[ -t 0 ]` | cron supplies no terminal, and a crontab line cannot manufacture a tty (a kernel property, not a variable) |

The refusal status is **78** (`EX_CONFIG` in sysexits(3)), deliberately NOT the
agent-session guard's `3`: "this script is retired" and "the launch was refused by
the guard" are different facts and an operator reading `echo $?` must tell them
apart. Both scripts carry both arms; the nightsafe sister also keeps its night
window (23:00–09:00) for the operator's own hand-run.

The operator's watched path is preserved: with a terminal on stdin the script does
exactly what it did before (LOOK BEFORE ASKING → ONE delegated `$LAUNCHER "$N"`
call carrying the count → checked status). `--dry-run` remains but is **not** an
exemption from the gate (reading what would happen is still running it); `-h`/`--help`
still prints the header, because reading the ruling is not running the script and
`-h` precedes the gate.

**No escape hatch.** No `--force`, no `GOPI_ALLOW_AUTOSTART`, no alternate cadence,
no argument or environment exemption — an escape hatch is the ruling reversed. The
gate reads the process table and fd 0 only; it consults no variable, so
`GOPI_ALLOW_AUTOSTART=1` is inert (pinned by
`TestHpWorkerLaunchersRefusalSurvivesASchedulerEnvironment`). A test asserts the
hatches are NAMED in the refusal message (that naming is the point) and never
honoured.

### 2. `scripts/hp/hp-worker-nightsafe.sh` — audited, then changed

It carried the same hazard and for a sharper reason: its window is precisely when
nobody is watching. Same gate, same status, same two arms; its night window and its
delegation contract are untouched.

### 3. `internal/testutil/hp_worker_launchers_test.go` — the guard, updated

The old delegation/loudness/knob/pointer assertions are kept (they still apply).
Five new tests pin the retirement **behaviourally**:

- `TestHpWorkerLaunchersRefuseAnAutomaticCaller` — the SAME invocation, three
  callers: (1) no terminal (cron's shape) → exit 78, message carries the ruling and
  the reason, `REFUSED` in the log, and the stub launcher's call log is EMPTY;
  (2) a real pty on stdin (an operator) → exit 0, no `REFUSED`, the launcher is asked
  for liveness; (3) a `cron`-named wrapper that HOLDS a pty → exit 78, launcher call
  log empty. Case (3) is the bypass this work closes: a wrapper could otherwise
  claim a watched context.
- `TestHpWorkerLaunchersRefusalSurvivesASchedulerEnvironment` — cron's own
  environment (`SHELL=/bin/sh`, `PATH=/usr/bin:/bin`) plus `GOPI_ALLOW_AUTOSTART=1`
  and `GOPI_WORKER_FORCE=1`: still 78, still no launcher call.
- `TestHpWorkerLaunchersOfferNoEscapeHatch` — no hatch is honoured; `[ -t 0 ]`
  present; the scheduler markers and the retirement status are named constants.
- `TestHpWorkerLaunchersRecordTheRulingAndTheMeasurement` — the header carries the
  ruling, `1,214` and the mask.
- `TestHpWorkerLaunchersRefuseAnAutomaticContextStatically` — the seam
  (`refuse_retired`, `scheduler_ancestor`, `watched_terminal`) is code the script
  takes, not prose about a path.

The existing behavioural probes (`TestHpWorkerLaunchersRefusedLaunchIsLoudNonZeroAndSilentOnSuccess`,
`…DryRunStartsNothing`) now run through a real **pty** (`openHpPTY`, zero-dep:
`/dev/ptmx` + `unix.IoctlSetPointerInt(TIOCSPTLCK)`), because a watched run is by
definition one with a terminal on stdin — without the pty those probes would have
been measuring the refusal, not the delegation. `hpRunStarter`'s default stdin is
`/dev/null`, i.e. a scheduler's shape.

### 4. Docs — nothing reads as an install instruction

The repo documents that recorded the cron install now carry the retirement note
inline, so a future operator cannot read "install the cron line" without the ruling:
`devdoc/gopi-home-hygiene.md`, `devdoc/hp-inline-launchers-20260912.md`,
`devdoc/hp-inline-launchers-verification-2026-09-12.md`,
`devdoc/01a092db-closedout-20260915.md`, `devdoc/OVERNIGHT-SUMMARY-20260910.md`
and `briefings/brief-why-hp-branches-vanish.md`.

## Pitfalls found and fixed (for the next agent)

1. **An unquoted list in a `case` pattern is NOT split into alternatives.**
   `case $comm in $SCHEDULER_COMM_MARKERS)` matches the literal string
   `"cron atd anacron"` and nothing else, so the ancestry arm was silently vacuous
   while the script looked right. Found by driving the arm with a real
   `comm=cron` parent (a `prctl(PR_SET_NAME)` / `/proc/self/comm` write), not by
   reading. It now goes through a `comm_is_scheduler` helper that compares one
   marker at a time.
2. **`-h` must precede the gate.** The harness invokes `<script> -h` to read a
   helper's usage. With the gate first, `-h` refuses — which would have looked like
   a broken script. Help is exempt BY ORDER (and the gate is code with no
   options-to-skip list).
3. **`exec` resets `comm`.** A wrapper probe written as
   `printf cron > /proc/self/comm; exec bash "$TARGET"` fails: the `exec` replaces
   the process image and the name goes back to `bash`. Run the target as a CHILD.
4. **A scheduler-shaped probe must not use the default launcher path.** The first
   version of the retirement probe let the default `$HOME/bin/gopi-worker-up.sh`
   resolve to the *operator's real* launcher. It never reaches it (the gate refuses
   first), but a probe that could is a live-side-effect risk; the sandbox now sets
   the default inside itself as well.

## Honest gaps

- **Nothing is installed from here.** HP's `~/bin/` copies are the live artifacts;
  this repo's files are their source of truth, and this run is forbidden to touch
  anything live (not restart, not kill, not install). The retirement is a fact of
  the REPOSITORY: the next `install`-shaped act on HP must be a deliberate operator
  one, and the script it installs refuses to be scheduled. There is no install seam
  to change (the scripts were hand-installed), which is exactly why the refusal
  lives in the file.
- **The host this ran on is peerbox, not HP.** The observed transcripts come from a
  scheduler-shaped context reproduced on this host (a real `cron`-named ancestor
  plus no terminal), not from HP's crontab — HP's crontab is verifiable read-only
  from here only if a peer is reachable, and no live touch is licensed by this
  task. `crontab -l` on THIS host carries no autostart line.
- **The `atd`/`anacron` markers are untested by execution** (no such daemon here);
  the `cron` marker is, and the list is one constant.
- **A `setsid`-style caller that keeps a terminal AND is not descended from a
  scheduler** is outside the gate's reach by construction (it looks like an
  operator). Deliberately: the gate refuses the AUTOMATIC shapes, and an operator
  who detaches a run by hand owns it. Stated rather than papered over.
- `scripts/hp/probe-hp-loudness.sh` (the HP-side probe, run by hand) still probes
  the delegation arms and is NOT retirement-aware; a future run of it will now see
  refusals where it expected delegation. It was left untouched (it is a hand-run
  probe, out of this task's deliverable list) and is named here so the next reader
  is not surprised.

## Verification

See the run report at `$GOPI_TASK_REPORT_PATH` for the pasted transcripts
(`Ran:`/`Result:`), the RED/GREEN proof and the acceptance checklist.
