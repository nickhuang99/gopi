<!-- doc-class: historical -->
# The operator's applications died during a suite run (2026-10-24) — evidence, ruled-out mechanisms, and what Rule 42 made of it

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**What this page is.** The evidence file PROJECT.md Rule 42 and
`internal/tools/kill_group_guard.go` both point at. It records ONE incident, what
was measured about it, what was ruled out, what the repository changed as a
result, and — plainly — what is still unknown. It is `historical` because it is a
record of a run, not a standing claim; the standing claim is Rule 42 itself, and
the machine check behind it is
`go test ./internal/testutil/ -run Rule42`.

## The event, as it was reported

During a test-suite run the operator's **browser and editor** (8 firefox
processes, a desktop editor) all stopped. The process records that were read at
the time say the firefox processes exited inside the **same second**, with the
X/snap scope consuming them and the message `Exiting due to channel error` — and
with **no OOM record and no SIGKILL record anywhere**. A `make test-budget` run
over `internal/tools` and `internal/agent` was live at that second, and the pid
ranges overlapped: firefox `1047211..1219592` against the suite's children
`1084347..1223658`.

The "no SIGKILL record" detail is the informative one, not a curiosity: a
SIGKILL cannot be caught, so no victim can write a death record. An absence of
records is therefore consistent with a group-addressed SIGKILL, and is NOT
evidence that no signal was sent.

**Provenance — read this before quoting the paragraph above.** The event is
**inherited testimony**, not a measurement I took: it is recorded in this
repository's own commit message `d5cb6bd` (`fix(tools): re-verify a process group
id before signalling it — a recycled id reached the operator's desktop`,
2026-10-24 09:35:27 +0800) and in the text of PROJECT.md Rule 42 (landed
`f739709`, same day, 07:11:12 +0800). I did not observe the killing, and no
process table survives it. Everything in the section below IS re-measurable, and
is stated with the command that produces it.

## What was ruled out — four mechanisms, four measurements

Each of these was a plausible mechanism for a suite run reaching the operator's
desktop, and each was eliminated by a reading rather than by argument. Re-run
them at any time; they are cheap and they do not touch the system's state.

| mechanism | measurement | reading |
|---|---|---|
| a `kill(-1, …)` in production Go | `git grep -nE 'Kill\(\s*(-1\|0)\s*,' -- '*.go'` | **0 files** — no such address exists in the tree |
| a gopi systemd unit signalling the session | `systemctl --user list-units --all --no-legend \| grep -ci gopi` and the system-scope form | **0** and **0** — no gopi unit on this host, user or system |
| the suite sharing the caller's process group | `grep -n 'test_budget_launcher=(setsid)' scripts/test-budget.sh` | `2322:    test_budget_launcher=(setsid)` — the suite leads its OWN session, so a group signal it sends stays inside it |
| a broad `pkill` from the suite | the Rule 42 gate's script arm (`scripts/`, comments stripped) | one `pkill -f` under `scripts/`, in `voice-capture-failmatrix.sh`, matching the unique per-run output path of a recorder it spawned itself — nothing else on the host can match — plus one `pgrep -f` in `scripts/materials/fourway-drive.sh`, which only READS the table |

**And the fifth mechanism, which is the one that survived:** a group id that was
somebody's child when the decision was taken and **somebody else's** when the
signal was delivered.

## Why a group id can reach a stranger at all

A process-group id **is** its leader's pid. `syscall.Kill(-pgid, sig)` is correct
at the instant `cmd.Start()` returns and only then.

`pid_max` on this host, measured:

```
$ cat /proc/sys/kernel/pid_max
4194304
```

The kernel recycles those numbers. A zombie held by a parent that never calls
`wait` pins its number out of circulation for as long as it lives and releases it
the instant the parent dies. So "the group I created at T1" and "the group that
id names at T4" are two different sets of processes, with **nothing between them**
— the window cannot be closed by holding a lock, because the kernel recycles the
number without asking. That is not a race to tighten; it is a design error, and
the only cure is to stop addressing a group you do not hold.

## What the repository changed because of it

1. **`d5cb6bd` — the guard.** `killChildTree` measured "the child leads a group of
   its own" at T1 and delivered `syscall.Kill(-pgid, SIGKILL)` at T4.
   `killChildTreeGroupStillTheChilds` (`internal/tools/bash.go`) now re-reads the
   live table **immediately before** delivery: the child must still lead the group
   being addressed and the pid must still answer. When it moved, the caller
   degrades to the per-pid path, which reads `/proc` live and therefore cannot
   reach a stranger. Its red proof — the guard removed, the test fails; the guard
   in place, the whole `KillChildTree` set is green — is
   `TestKillChildTreeRefusesAGroupSignalWhenTheGroupIdMoved`
   (`internal/tools/bash_kill_group_recycle_test.go`).
2. **`killGroupStillTheChilds` / `signalProcessGroup`**
   (`internal/tools/kill_group_guard.go`) — the same predicate as the shared
   production form the other thirteen call sites use, rather than a per-site
   copy.
3. **`f739709` — Rule 42.** The rule states the ban, the reason, the two measured
   incidents (this one and the earlier `signal.Reset` defect, fixed in `c9c0534`),
   and the alternatives.
4. **The suite leads its own session** (`scripts/test-budget.sh`, the `setsid`
   launcher cited above): a group-addressed signal a test does send reaches the
   suite and nothing else. Containment, not a licence.

## What is still unknown — stated as unknown

**The incident is not explained.** All four candidate mechanisms above were
eliminated by measurement. What remains is the class Rule 42 names: a signal
addressed by a *number*, delivered to whatever that number meant at delivery time.
Nothing in this repository can prove that a specific call did it after the fact —
a SIGKILL leaves no trace in the victim, and the sending process is long gone.

**INFERENCE, marked as one (Rule 41):** `killChildTreeGroupStillTheChilds` did not
exist when the kill happened. The evidence for that is a commit TIME, not an
observation of the running code: the guard landed at `d5cb6bd` (2026-10-24
09:35:27 +0800) and Rule 42 at `f739709` (same day, 07:11:12 +0800), both after
the incident, and `internal/tools/kill_group_guard.go` cites the incident as its
reason. What that proves is "the guard was added afterwards"; it does NOT prove
which code path delivered the signal, and no reader should take it as proof of
anything but ordering.

That is exactly why the remedy is a rule and a check rather than a bug fix, and
why the check's own coverage is stated as a floor (below).

**Next time, capture before concluding.** The evidence that would settle it is
all cheap and none of it was taken at the time:

```bash
# the suite's OWN group and session, at the moment it is running
ps -o pid=,ppid=,pgid=,sid=,comm= -p <suite-pid>
# who shares that group (this is the list that would name the victims)
ps -o pid=,pgid=,comm= -g <suite-pgid>
```

A group kill leaves its victims inside one `pgid`, so the survivor list of a
group (`ps -g`) is the witness, and it must be read while the run is live.

## What the machine check covers, and what it cannot

`go test ./internal/testutil/ -run Rule42` is the gate Rule 42 names
(`internal/testutil/rule42_destructive_signal_gate_test.go`). Its honest coverage:

- **CAUGHT** — `Kill(-1, …)` and `Kill(0, …)` anywhere in Go on disk, untracked
  files included. (The first version of this gate read `git ls-files` and
  therefore never scanned ITSELF while this file was still untracked — the green
  it printed covered 2955 of 2956 Go files, and the four fixture literals below
  only appeared once the file was committed. A scan that skips exactly the file
  being written is not a scan, so the Go arm walks the tree.); a Go process **executing** `pkill`/`killall`/`pgrep` as a control path;
  `signal.Reset(sig)` in a file that re-raises to itself **without** dropping its
  own subscription first (`signal.Stop`), which is the measured `c9c0534` defect;
  a script under `scripts/` running `pkill -f`, `killall`, `kill 0`, `kill -1` or
  `kill -- -PGID`.
- **REPORTED, never failed** — the ~40 legitimate `syscall.Kill(-pgid, …)` sites:
  the caller created that group on its own `*exec.Cmd` (`Setpgid: true`) or kept
  the handle. **Ownership is a fact about a handle, and a text gate cannot read
  one.** The census is printed on every run, so "not flagged" is a number, not a
  reassurance. `pgrep -f` reads the table and signals nothing; `signal.Reset`
  paired with a `signal.Stop` is handing the default disposition back, which is
  the spelling Rule 42 endorses. Both are reported for the same reason.
- **NOT CAUGHT** — a signal whose target arrives through a variable (a pgid that
  was read, stored or computed); a broad signal inside a non-script file (a
  `.py`, a Makefile recipe, a recorded capture under `devdoc/evidence/`); and any
  intent behind a call whose addressing literals look safe. **The gate is a
  floor, not the rule.**

The gate's own non-vacuity is checked, not asserted: it refuses to pass when the
Go walk or the script walk reaches fewer files than its floors, it prints
`judged / exempted / reported / rejected` on every run, and
`TestRule42PlantedViolationsTurnItRed` plants one banned form per kind into a
scratch tree — driving the same predicates the real tree uses — with CLEAN
CONTROLS (a `Setpgid` group signal, `kill -0` liveness probes, `kill -TERM PID`,
a Stop-then-Reset re-raise) that must not be rejected.

**History of this check, because the same failure is the reason it exists:** Rule
42 shipped at `f739709` with a `**Machine check.**` paragraph naming
`internal/testutil/rule42_destructive_signal_gate_test.go`, and that file did not
exist — `go test -run Rule42` printed `no tests to run` and exited 0. A named
check that matches nothing is worse than no check: its green is indistinguishable
from enforcement (the class Rule 36 and Rule 38 each record after their own month
of vacuum). The file now exists, the paragraph was corrected to say what it can
and cannot cover, and the Check line is runnable in the sense Rule 38 requires —
`=== RUN`, never an empty match.

## The `signal.Reset` wheel is still bolted on — measured, and NOT fixed here

Rule 42's banned table lists "`signal.Reset(sig)` used to re-raise", and its prose
records that the `c9c0534` fix left the same wheel on
`internal/tools/testmain_test.go`'s `toolsReraise`. That is **still true at
`0336db7`**, and the sites are:

| site | drops its own subscription? | why the blanket Reset is there |
|---|---|---|
| `internal/tools/testmain_test.go:286` (`toolsReraise`) | yes (`signal.Stop(ch)` at :267) | the run must die with the signal's own status instead of surviving a TERM it already cleaned up for |
| `internal/signalrec/signalrec.go:2404` (`reRaise`) | yes (one `signal.Stop` in the file) | restore the default disposition after `defaultForwardGrace` so the provenance record outlives the process |
| `internal/lineedit/raw.go:802` | yes (two `signal.Stop` calls in the file) | the terminal's fatal path: attributes restored, then the default handed back; a `Close()` that returned would leave the process in raw mode |
| `cmd/agent/merge_run.go:1714` | yes (three in the file) | the second-Ctrl-C arm — and it carries its OWN measurement: **Stop + self-kill → the process is STILL ALIVE** (signalrec holds its own registration for the process's life), Stop + Reset + self-kill → exit 130 |

So the naive fix is not available: deleting the `Reset` in `merge_run.go`
reintroduces an **unkillable** merge, which Rule 42's own prose calls worse than
dying mid-pass. Whether each site is a deliberate exception or still a defect is a
decision about four call sites and their measured behaviour, not something the
task that repaired this rule's citations could settle — so it is **bug row
`01a16d37`**, filed with this evidence.

The gate's treatment matches that honest state: each of the four is **flagged**
and then discharged by a **named per-file permit** carrying its reason, rather
than folded into a "reported, never failed" class — a tolerant class would have
made the gate blind to a NEW such arm as well. The permission table is checked in
both directions, so the moment a site is genuinely fixed its row must be deleted
in the same change or the gate fails on the stale permit.

## 2026-10-27/28: the same class, with a NAMED sender and a LOCATED call site

**This is a SECOND incident, not a new explanation of the one above.** The
2026-10-24 event remains unexplained (its section still says so). What happened
three days later is the same *class* — a signal addressed by a number — and this
time the provenance channel and the code both named it.

### What was measured

`~/.gopi/sessions/*/signal-provenance.jsonl`, filtered to
`"sender_comm":"agent.test"`, holds **5 deliveries to 4 distinct pids** between
2026-10-27T23:29 and 2026-10-28T00:12. (An earlier draft of the fix's commit
message said "five live processes"; the record is 4 pids, one of them signalled
twice — the sequence number is not the victim count, and the evidence is the
authority.) Every victim was a *running* gopi in a session directory this run did
not own:

| time | victim pid | argv | victim's cwd | sender |
|---|---|---|---|---|
| 2026-10-27T23:29:05 | 1192990 | `/home/user/work/gopi/bin/gopi` | `~/workspace/01a17dee-eb54-7b85-ac22-bb018eeb5f0d` | pid 1327296 `agent.test`, SI_USER |
| 2026-10-27T23:29:07 | 1192990 | same (repeat) | same | pid 1329837 `agent.test`, SI_USER |
| 2026-10-27T23:38:19 | 1568257 | `/home/user/work/gopi/bin/gopi` | `~/workspace/01a17dfa-cfcb-7939-a8e6-a79d64c6c645` | pid 1589472 `agent.test`, SI_USER |
| 2026-10-28T00:02:22 | 1669629 | `gopi worker` | `~/.gopi/sessions/01a17dac-6255-7140-a16b-c5a9e0cc8268` | pid 2000835 `agent.test`, SI_USER |
| 2026-10-28T00:12:28 | 1670464 | `gopi worker` | `~/.gopi/sessions/01a17db3-7966-72b2-b1f7-dc9e3d8ccfc9` | pid 2544045 `agent.test`, SI_USER |

`sourced:false` and `si_code:0 (SI_USER)` in each row say the delivery came from
userspace, not from the kernel — i.e. a `kill(2)` by a test binary.

### Where the calls were

Both shapes send SIGINT, and neither had an ownership check:

1. **`groundChildPid`** (`cmd/agent/task_ground_e2e_test.go`) scanned **every pid
   on the host** and accepted any process whose environment carried the row's
   `GOPI_TASK_WRAPPER_ID`. That variable is **inherited**, so a `gopi worker` in
   an unrelated session directory that re-executes the same row carries it too.
   The only rejections were `== wrapperPid` and `== os.Getpid()` — no ancestry,
   no cwd, no `GOPI_HOME`. `groundStopRun` → `groundKill` then signalled what it
   had found.
2. **Two `t.Cleanup` teardowns** — `image_selfcheck_e2e_test.go` and
   `worker_foreground_stop_e2e_test.go` — sent `cmd.Process.Signal(os.Interrupt)`.
   The handle establishes ownership, but it names the **number**: by teardown time
   the fixture is usually gone, and a signal to a reaped pid reaches whoever owns
   that number now.

### What changed (`2d756bc`)

- **`testutil.ProcDescendsFrom(pid, ancestor)`** walks the kernel's parent column
  (`/proc/<pid>/stat` field 4) and **refuses with a reason** instead of guessing
  when a level is unreadable, a cycle appears, or the chain exceeds its cap.
- **`groundChildPid`** treats the environment match as a **candidate filter** and
  requires descendancy from this process or its wrapper before returning a pid; a
  skipped candidate is logged with its reason. A walk that then times out is the
  honest verdict for a child that cannot be told apart from a stranger.
- **Both teardowns** deliver through `testutil.KillIfIdentical` against the
  recorded incarnation (pid + starttime + cmdline), the same guard `groundKill`
  and `merge_stop` already use.

**Regression guard**:
`TestProcDescendsFromIsTheOwnershipTestAnEnvMatchIsNot`
(`internal/testutil/procident_test.go`) measures both directions against real
`/proc` entries — a child this test spawned **is** a descendant (the true
positive a refuse-everything guard would lose), while its own parent, pid 1, and
`p == p` are not; and a target that is not a process id comes back with a reason
rather than a silent "not mine".

### The limit of this fix, stated

It closes the two shapes that were found. It does **not** make the tree
signal-safe in general: the other `syscall.Kill(<pid>, …)` call sites in
`cmd/agent/*_test.go` were not audited in this pass, and Rule 42's own reasoning
applies to every one of them — a number is not a process. `go test ./internal/testutil/ -run Rule42`
still covers the banned shapes it names and nothing more.

