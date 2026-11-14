<!-- doc-class: historical -->
# The FOREGROUND worker: the control face stops asserting what it never measured

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a131c8-0500-7de6-b3b7-650b8005b26e` (bug report
`01a131f4-7494-7b9a-9a63-4a225ea5a4fa` is the *finding* this run produced, and it
is a different defect — see "What this run found on the way" below).

## The defect, measured

`gopi worker --stop` printed, and exited 0:

```
gopi worker: no worker daemon is running (no live worker state under …);
a FOREGROUND `gopi worker` holds no lock and is unaffected
```

while the operator's **foreground worker was alive** (measured 2026-10-12,
pid 3642684, uptime 3h34m) and plainly visible to `ps`. Two defects share that
one sentence:

1. **A proxy metric was reported as the metric.** What was measured is "no
   daemon *state file* is live". What was stated is "no worker is running".
   Those are different facts, and a foreground worker has no state file *by
   design* — so the sentence was not merely imprecise, it was systematically
   wrong precisely where the operator needed it.
2. **The one command that claims to stop workers was useless on the most common
   shape.** `gopi worker` (no `--daemon`) is the normal way a person runs this;
   "is unaffected" left `kill` as the only exit, i.e. the graceful-stopping tool
   pushed the operator onto the non-graceful path.

The same blind spot ran through the read face: `--status` printed "no workers
running" with the pid sitting in the process table.

## The fix

`cmd/agent/cli_worker_foreground.go` (new) is **the foreground half** of the
worker control surface:

- `foregroundWorkerPIDs` enumerates worker-shaped processes that are **not**
  detached — the exact complement of the daemon-shaped `fallbackWorkerPIDs`,
  with the same profile attribution (a process's OWN environment, degrading to
  "count it as this profile's" when the environment cannot be read).
- The **caller is excluded by pid**. A `--stop`/`--status` invocation has the
  same argv shape as a foreground worker (`comm = gopi`, `argv[1] = worker`), so
  without the exclusion the command would find *itself* and SIGTERM its own stop.
- `--stop` reaches them through the **same primitive the daemon path uses** —
  `SIGTERM` is the worker's "finish the in-flight round, then exit"
  (`installWorkerStopSignals`), `SIGUSR1` is `--force`. A foreground stop is not
  a second semantics; it is the same one delivered to a process the daemon path
  cannot address (a foreground worker has no request file to deliver).
- **The signal is sent only on positive identity evidence**: `workerSameBinary`
  compares device+inode of `/proc/<pid>/exe` against `/proc/self/exe`. A worker
  whose binary cannot be proven to be this one is **named with the exact command
  that stops it** — never signalled (a worker whose signal contract we cannot
  verify may forward the stop into the in-flight run child and kill the round).
  This is the conservative direction of `workerForeignDrainBinary`, for the case
  where no file can carry the stop.
- `--status` prints each foreground pid with its command line and the one command
  that stops it; the empty case now needs **both** halves to be empty.
- `--restart` **reports** the foreground workers and deliberately does **not**
  signal them: replacing the daemon is not a request for them to leave, and a
  command that silently killed a worker it did not name would be the original
  defect wearing a new coat.

`workerNoDaemonNote` — the function that carried the false assertion — is
**deleted**, not reworded; its replacement names what was measured and then
states the process-table fact that was actually read.

## What this run found on the way (Rule 41)

The first real-run pass against a sandbox binary built as `/tmp/gopi-fix`
reported, with pid 105360 alive:

```
gopi worker: no FOREGROUND `gopi worker` process is in the process table either
```

The shape predicate keyed on **`comm == "gopi"`**, and a worker of a
differently-named build carries that name — the same class of false absence, one
level down. Fixed here by **reading the image** (`/proc/<pid>/exe`, falling back
to the process's own `argv[0]`) and requiring the gopi program prefix;
**filed** as bug `01a131fc-b833-7c1b-9e1f-f879920b59ae`, because the same
literal spelling is what `internal/procname.WorkerShape`,
`cmd/agent/cli_worker_status.go`'s `workerProcessShape` and
`scripts/gopi-worker-up.sh --count` all use — so a `gopi-fix worker` is invisible
to the launcher's count and to the daemon-duplication check too, which is a real
consequence (a worker that cannot be counted can be started twice).

## Named constants (Rule 1)

`workerForegroundNoteFmt`, `workerForegroundNoneFmt`, `workerForegroundFoundFmt`,
`workerForegroundMoreFmt`, `workerForegroundAskingFmt`,
`workerForegroundForceAskingFmt`, `workerForegroundForeignFmt`,
`workerForegroundExitedFmt`, `workerForegroundStillFmt`,
`workerForegroundStatusFmt`, `workerForegroundSample`, `workerForegroundSignalWord`,
`workerForegroundForceWord`, `workerProgramPrefix`, `workerDeletedMarker`,
`procExeName` — plus the existing `workerControlStop` / `workerControlForce` /
`workerControlWait` / `workerGracefulSignalName` / `workerStopForceSignalName`
verbatim.

## Verification (this branch)

- Red proof: reverting the three edited files to `HEAD` makes
  `TestWorkerStatusListsAForegroundWorkerAndStopEndsIt` fail at exactly the bug
  (`--status` claimed no workers while pid 59642 was alive); restore verified by
  blob hash against the pre-red snapshot.
- Real run: `~/.gopi/sessions/01a131c8-…/artifacts/run-foreground-stop-final.log`
  — sandbox → real foreground worker → `--status` lists it → `--stop` names it
  and exits 0 → `ps` proves the pid is gone.

## The repo-context pin the package's own gate demanded (found on this branch)

Landing the e2e test took **two** pins, and the second one was named by the
repository, not by me: `TestSpawnersPinTheRepoContext` scans every function in
`cmd/agent` that spawns a real worker/merger and fails the build when the spawn
site does not control the child's repo context (the hazard in
`repo_context_sandbox_test.go`: the child inherits this process's cwd — the live
checkout — and its own-worktree-release pass enumerates the developer's
worktrees). Measured on the committed branch, clean worktree at `35a687b0c`:

```
go test ./cmd/agent/ -run TestSpawnersPinTheRepoContext -count=1
--- FAIL: TestSpawnersPinTheRepoContext (0.64s)
    repo_context_sandbox_guard_test.go:510: 1 function(s) spawn a real gopi
    worker/merger WITHOUT the repo-context sandbox:
        worker_foreground_stop_e2e_test.go:181: wfsStartForeground()
```

The test called `SandboxRepoContext(t)` — which pins the **ledger**, the
**settings repos table** and **this process's cwd** — but the child it started
still inherited the launcher's directory. Two pins, and only one of them was
mine to remember: the caller's half (the pin call) and the **child's** half
(`cmd.Dir`, or a runner that sets it). The fix is one field:
`wfsStartForeground(t, bin, dir, env)` takes the sandbox directory from
`SandboxRepoContext(t).Dir` and sets `cmd.Dir = dir`. Green after
(`ok github.com/nick/gopi/cmd/agent 1.386s`), with both e2e halves still green.

**Why this belongs in the devdoc and not only in the test.** A gate caught it,
which is the good case; the generalisable lesson is the one the gate's own
message states — *"pinning the LEDGER is only HALF a sandbox"* — and it applies
to any future test in this package that starts a real worker: the pin has a
caller half and a child half, and a test that has only the first looks pinned and
is not.

### Commits on this branch

| commit | what |
|---|---|
| `b50f70157` | the fix: `--stop`/`--status` reach a FOREGROUND worker |
| `35a687b0c` | the anchor-number table, regenerated (this fix's own line shift healed a frozen permit) |
| `42e93c651` | the child-side repo-context pin the gate named |
