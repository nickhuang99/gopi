<!-- doc-class: normative -->
# The daemon spawn seam reaps the child it starts — the Z-state `gopi` zombie

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task row `01a132b0-497d-72c2-935e-db05d8d8356e`, branch
`task/01a132b0497d72c2935edb05d8d8356e`, worktree
`/home/user/workspace/gopi-01a132b0497d72c2935edb05d8d8356e`.

Follows `devdoc/cmd-agent-child-reaper.md` (which swept the children a suite
leaves behind and hit this one as `leaked child pid=… state=Z`) and the
judgement-side sibling (`01a132a7`, a process census that must not silently drop
a process whose `exe` is unreadable). This document is the SOURCE side: why such
a process exists at all.

## The symptom, and why it looked like it came from nowhere

`scripts/gopi-worker-up.sh --count` reported an extra "other gopi process": a pid
with `/proc/<pid>/comm = gopi`, but no `exe` link and no `cmdline`. Both readings
are correct, and together they name the state exactly:

| channel | Z-state reading | why |
|---|---|---|
| `/proc/<pid>/comm` | still `gopi` | comm is task-struct-resident; the kernel keeps it across exit |
| `/proc/<pid>/exe` | empty | the `mm` is released at exit |
| `/proc/<pid>/cmdline` | empty | argv is released at exit |
| `/proc/<pid>/stat` field 3 | `Z` | the only channel that says it outright |

So the zombie was never invisible — it was uncountable by `comm`, uncountable by
`exe`, and the two censuses disagreed in exactly one cell (`other`), which is why
the original report could say "the worker counts are identical" and "there is a
stranger process" at the same time.

## The measurement that located it

A passive `/proc` sampler (read-only: Rule 27 forbids manufacturing load to make
a number) polled every 50 ms for any process whose comm was `gopi`, recording its
state, ppid, the parent's comm and its cmdline — so the same process is captured
once ALIVE and once as a zombie:

```
ts=1791857860.485209792 pid=738103 comm=[gopi] state=Sl ppid=733871 ppid_comm=[agent.test]
    cmdline=[/dev/shm/suite-cmd-agent-run-…/gopi-suite-bin-…/gopi worker ]
ts=1791857804.320298074 pid=649678 comm=gopi  state=Z  ppid=645638 ppid_comm=agent.test
    cmdline=[]
```

That is the whole attribution: the zombie is a **`gopi worker` daemon** (alive
phase shows its argv), and its parent is the **test binary** (`agent.test`). It
was reproduced three times by one command:

```bash
go test ./cmd/agent/ -run TestInProcessDaemonStartCompletesWhenTheRealBinaryIsPublished -count=1
```

and the suite's own close-out sweep named it independently:

```
suite child sweep: leaked child pid=649678 state=Z
    cmd="(cmdline unreadable — the process exited between reads)"
```

## The root cause

`daemonSpawn` (`cmd/agent/cli_daemon_spawn.go`) forks the detached daemon with
`child.Start()` and owns a `Wait` on exactly ONE branch — the failure branch,
`daemonScopeAbandon(child, …)`, which SIGTERMs the child and waits for it. The
**success** branch returns the verdict and lets the `*exec.Cmd` go.

For a launcher that exits immediately this is invisible: the child is
re-parented to init, which reaps it. For a caller that **stays alive** it is
permanent, because zombie-ness is a property of the parent-child relation and
nobody else can collect that exit:

- a test binary (thousands of starts, one long-lived process) — the measured case;
- a launcher that starts two daemons;
- any REPL/session subcommand that starts a daemon and keeps running.

"Outlives its launcher" was being read as "nobody needs to reap it", which is a
statement about the process TREE being applied to one CALL. The two are not the
same claim.

## The fix, and why it is in the producer

The reap is **decoupled from the verdict**: a goroutine owns the child's `Wait`,
so the start's asynchronous contract is unchanged (the caller is never delayed,
and a daemon outliving its parent is exactly as before). It is started only on
the success path, so exactly one `Wait` ever reaches one `*exec.Cmd` — the
failure path keeps its own, and two `Wait`s on one `Cmd` is an error.

Every exit of that goroutine is accounted for (Rule 15b): a normal exit *including
a non-zero one* — is silent, since a daemon's own exit status is its business;
an abnormal `Wait` failure, or a child that outlives `daemonChildReapWait`, is
reported with the pid. The report is deliberately placed on the reap path rather
than removed to keep the output clean: a reap that quietly gave up would leave
the very zombie this seam exists to prevent, without a word.

The alternative — sweeping zombies out of the suite's close-out — was rejected:
it treats the symptom in the one place that happens to notice, leaves the
production seam leaking for every other long-lived launcher, and cannot help the
launcher that has no sweep.

## Verification

| what | how |
|---|---|
| the zombie is gone | the SAME passive sampler around the SAME command: pre-fix `state=Z ppid_comm=agent.test`, post-fix no `Z` row at all, and the suite's sweep reports `live child processes: 0` |
| the fix is load-bearing | red proof: `daemonReapStartedChild(child, …)` disabled → `TestStartedDaemonIsReapedSoItsParentLeavesNoZombie` FAILS naming `state="Z"`, and the suite's own sweep reports `leaked child pid=… state=Z`; restored, verified by blob identity (`git hash-object`) |
| no regression in the start | `TestInProcessDaemonStartCompletesWhenTheRealBinaryIsPublished` still passes, and its measured cwd/pid/state assertions are untouched |

## What this does NOT claim

- **It is not "no zombies in this suite".** Three children are *designed* to
  outlive their test's SIGKILL (`devdoc/cmd-agent-child-reaper.md`: the
  inherited-lock fixture and the pty lifecycle fixture), and `suiteSweepLeakLimit`
  is 3 for that reason. This fix removes the daemon-shaped one, which was a leak
  and not a design.
- **It does not change the count in `gopi-worker-up.sh`.** The zombie was
  counted as *other*, never as a worker; the census-side disagreement is the
  sibling task's subject, and this one only removes one cause of it. What WAS
  measured is the magnitude: on the script's own test seam (`GOPI_PROC_ROOT`,
  `scripts/gopi-worker-up.sh:112`) with a minimal three-entry fixture — a live
  worker, the zombie's SHAPE (`comm=gopi`, empty `cmdline`), and a one-shot run
  child — the real verb reports `other gopi processes: 2` with the zombie's
  entry present and `1` with it removed, the worker line unchanged at `1`
  (`artifacts/count-fixture.sh`, `artifacts/count-fixture.log`, task 01a132b0).
  That is a fixture ON the predicate, not a live reading, and it is what the
  earlier unobserved "+1" claim should have been.
- **It does not cover a parent killed between `Start` and the goroutine.** The
  window is a few instructions, and it cannot be closed from the parent at all
  (a dead parent has no code left to run) — the child is re-parented to init in
  that case, which reaps it.

## The guard's own flake: a TRANSIENT Z is not a leak (task 01a13cdb, 2026-10-15)

The guard above was itself intermittent, and this is the measured correction —
the production seam was never wrong for it.

**What was observed.** 2 FAIL in 118 archived multi-package streams that contain
the test (one of them predating the branch that added it); the same revision
green standalone; every failure a single line at ~0.10s elapsed with the FIRST
poll already reading `state=Z`.

**What was measured** (passive `/proc` sampling, never a probe or a load — Rule
27; the harness is a temporary in-process arm driving the SAME production entry
point `workerDaemonSpawnErr`, systemd-scope mode, deleted before commit):

```
iter=1 daemon=3845474 ALIVE state="S" ppid=3845350 ppid_comm=agent.test (test binary is 3845350)
iter=1 daemon=3845474 FIRST Z at +1.193ms (poll 2) ppid=3845350
iter=1 daemon=3845474 LEFT /proc at +2.86ms (4 polls)
     Z-WINDOW first=+1.193ms last=+2.36ms width=1.167ms
iter=2 ... FIRST Z at +1.198ms ... LEFT /proc at +2.146ms  width=0s
iter=3 ... FIRST Z at +1.156ms ... LEFT /proc at +2.294ms  width=0s
iter=4 ... FIRST Z at +1.156ms ... LEFT /proc at +2.048ms  width=0s
iter=5 ... FIRST Z at +1.214ms ... LEFT /proc at +2.323ms  width=0s
```

**The mechanism, stated as the deterministic property it is.** A child that
exits is `state=Z` from the instant the kernel releases it until its parent's
`Wait` collects it. That interval is what `wait(2)` semantics ARE — it is not
avoidable by any seam, however eagerly it reaps (here the reap's `Wait` lands
about 1 ms after the signal). The arm failed on the FIRST reading of `Z`, and
its first poll is at `t=0`, so its verdict was a race against that window:
whether poll #1 landed inside ~1 ms of Z. Nothing about the host was needed to
explain it beyond ordinary scheduler latency.

**The rate, measured both sides.**

```
go test -count=120 -run TestStartedDaemonIsReapedSoItsParentLeavesNoZombie ./cmd/agent/
  pre-fix : 2 FAIL / 120  (0.09s, 0.13s elapsed, "ZOMBIE (state=\"Z\")" on the first poll)  → exit 1
  post-fix: 0 FAIL / 120                                                                     → exit 0
```

The 118-stream archive under-sampled precisely this rate: in ONE process the
test runs 120 times in ~33s, which is why `-count=N` (the registry's own
prescribed instrument) reaches it while a multi-package batch does not.

**The correction, and the one thing that did NOT change.** A `Z` reading is a
failure only when it PERSISTS past `drzZombieCollectGrace` (2s — ~700–1700x the
largest measured transient), and the failure message now carries the parent READ
from `/proc/<pid>/stat`'s ppid field (`drzPidPPID`) instead of assuming
`os.Getpid()` — the pre-fix arm blamed a parent it had never measured, which
Rule 24 forbids. The seam, the SIGKILL-by-explicit-pid, the deadline and every
other assertion are untouched.

**Red-then-green, observed.** With `daemonReapStartedChild` disabled (the
pre-fix production behaviour) the corrected guard FAILS 3/3, each naming
`has been a ZOMBIE (state="Z") for 2.01s — well past any collect window`;
restored, the same command is 0/120. The restore was verified against a
REVISION, not the index: `git hash-object cmd/agent/cli_daemon_spawn.go` =
`git rev-parse HEAD:cmd/agent/cli_daemon_spawn.go` = `d690f40ca7a054f2390078b9f5e320857d9115b3`.
