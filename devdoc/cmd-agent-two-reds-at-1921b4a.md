<!-- doc-class: historical -->
# Two pre-existing cmd/agent reds at the 1921b4a cut — the audit sink per the walk, and the repo-context marker account

**Task** `01a143a79f64744fb6768ebde0d814e9`, branch `task/01a143a79f64744fb6768ebde0d814e9`,
2026-10-17, host `hosta`. Ground cut at `1921b4a67`; the briefing named `1e6de82`
(`1e6de828806f3be274008c561dd95efaedf7f9a2`, 2026-10-16 14:59:34) and that revision IS an
ancestor of this ground. This document is this run's record.

## Red 1 — `TestMergeEveryLoopWritesNoStateFile`: ALREADY FIXED before this ground

The briefing's red was real at `1e6de82` and is **not** present at `1921b4a`:

```
$ git worktree add --detach /home/user/workspace/gopi-ctl-1e6de82 1e6de82
$ cd /home/user/workspace/gopi-ctl-1e6de82
$ go test ./cmd/agent/ -run 'TestMergeEveryLoopWritesNoStateFile' -count=1
--- FAIL: TestMergeEveryLoopWritesNoStateFile (2.58s)
    merge_every_test.go:573: the profile holds [.gopi/logs/removal-audit.log] — the command's contract is
        no state file, no pid file, no progress record.
        WHY THIS CAN HAPPEN: the ONLY writer that can create a directory here is the signal-provenance
        recorder, … (internal/signalrec, sessionDest step 3) …
FAIL
```

(`/home/user/.gopi/sessions/01a143a7-9f64-744f-b676-8ebde0d814e9/evidence/gopi-prefix-1e6de82.log`)

The fix landed on master one revision earlier and this ground descends from it:

```
$ git log -1 --format='%h %ci %s' c466c05c1
c466c05c1 2026-10-16 22:49:13 +0800 fix(cmd/agent): the merge-loop residue walk tolerates the audit sink it writes itself
$ cd /home/user/workspace/gopi-01a143a79f64744fb6768ebde0d814e9 && git merge-base --is-ancestor c466c05c1 HEAD && echo yes
yes
$ go test ./cmd/agent/ -run 'TestMergeEveryLoopWritesNoStateFile' -count=10
ok  	github.com/nick/gopi/cmd/agent	2.698s      (10/10 PASS)
```

### Root cause, verified against the writer's own source (not read off the fix)

**Writer: `cmd/agent`'s own merge pass, through the removal-audit emitter.**

- `cmd/agent/merge_run.go:2487`, `:2504`, `:2608` — `mergeDecideBranch` calls
  `removalaudit.BranchDelete(removalaudit.ArmMergeRunSpentBranch, …)` in the three lanes the
  loop's `--every` pass really takes: ① `merge-base --is-ancestor` proved the ref spent,
  ② `merge --ff-only` landed it, ③ the union lane landed it. A `gopi merge --every D` loop
  with one fast-forwardable fixture branch therefore takes lane ② **on its first pass**,
  which is exactly what the scenario's fixture provides (`mergeEveryFixture` →
  `mergeBranch(t, repo, mergeTestBranch(t, "loop"), "loop.txt", "loop\n")`).
- `internal/removalaudit/audit.go` — `sinkPath()` (`:598`) resolves the default sink as
  `config.GopiDir()/logs/removal-audit.log` (the `LogDirName` + `FileName` constants), and
  `emit` (`:520`) creates that directory itself (`os.MkdirAll(dir, sinkDirMode)`, the
  "THE SINK'S OWN DIRECTORY IS THE SINK'S OWN OBLIGATION" paragraph, measured 2026-10-15).
  Because the loop runs with `GOPI_HOME`/`GOPI_SETTINGS`/`GOPI_MEMORY_DB` pinned to the
  fixture profile, `config.GopiDir()` IS that profile, and the first prune creates
  `<profile>/.gopi/logs/removal-audit.log`.

**Why it was attributed to the wrong subsystem first**: the walk's own failure text named
`internal/signalrec` (the provenance recorder) while the walk reported the SINK. The
instrument was right, the explanation was wrong — and that mismatch is what the first
report of this red recorded. `c466c05c1` fixed both halves: it taught `mergeProfileOffenders`
to tolerate the sink and rewrote `loopResidueWriterReason` to name both writers in the order
a reader should check them.

**Where the correct fix belongs — writer, profile isolation, or the test's contract?**

The test's contract (`loopResidueWriterReason`, and the walk's comment): *"what this contract
refuses is STATE the command leaves behind, and an audit record of an operation the command
performed is not state."* That is the right call, and the reason it is the right call is
structural: the sink is **append-only, shared and host-wide by design** (the
2026-09-17/18 post-mortems needed one grep-able list of removals), so moving it — or
redirecting it per-invocation — would trade a real operational property (a record an
operator can read after an incident, in the directory the post-mortem went to first) for a
test contract that was never about it. The fix therefore belongs in **the walk's tolerated
set**, matched by components against the emitter's own constants, and it is a NARROWING, not
a licence: `mergeIsRemovalAuditSink` accepts exactly
`<profile>/.gopi/logs/removal-audit.log` and nothing else — the file name at the profile
root, a rotated sibling, a second file inside `logs/` all stay offenders, and
`TestMergeProfileWalkKeepsItsTeeth` (the walk's falsifier) asserts each look-alike in one
comparison (`go test ./cmd/agent/ -run TestMergeProfileWalkKeepsItsTeeth` → PASS, measured
this run).

**This run's delta here is ZERO** — and that must be read as "nothing to do", not as "the
work was done": the correction is `c466c05c1`, one commit below this ground, authored by the
sibling task `01a143696d247e66abe1dda74a3fc9fd` and recorded in
`devdoc/merge-loop-residue-walk-audit-sink-20261016.md`.

## Red 2 — `TestSpawnersPinTheRepoContext`: real at this ground, fixed here (`14b503278`)

```
--- FAIL: TestSpawnersPinTheRepoContext (0.34s)
    repo_context_sandbox_guard_test.go:524: the tree carries 4 "repo-context-ok:" marker(s)
    but this scan COUNTED only 2 escaped spawn site(s)
```

### The two accounts, read out of the gate

`cmd/agent/repo_context_sandbox_guard_test.go`:

- `markers` (`:471`) is a **TEXT** count: `strings.Count(src, rcsEscapeMarker)` with
  `rcsEscapeMarker = "repo-context-ok:"` (`:115`), over every line of every `*_test.go` in
  the package;
- `escaped` (`:498`) is an **AST** fact: `rcsFunctionIsEscaped` (`:1186`) returns true only
  for a FUNCTION that is a *counted spawn site* and carries the token inside its own body.

### Both orphans came from `d1f88b5e5`

```
$ git show d1f88b5e5 -- cmd/agent/worker_control_sigint_e2e_test.go
+	// repo-context-ok: the victim is a plain sleeper; the env vector goes to the REAL binary under test, …
 	victim := exec.Command("sleep", "600")
…
+	// repo-context-ok: the child's environment is the fixture's own sandbox vector (GOPI_HOME/GOPI_SETTINGS/GOPI_MEMORY_DB pinned to t.TempDir())
 	cmd := exec.Command(f.bin, CliVerbWorker, workerControlStop, workerControlPIDFlag, f.pidArg(), workerControlWait, wcSigintBound)
```

- the first site spawns `sleep`, not the real gopi binary → never admitted by
  `rcsIsSpawnCall` / `rcsCallSpawnsLifecycleVerb`, so no escape can ever match it;
- the second IS admitted but **PINNED** (`cmd.Env = f.env`, the fixture's sandbox vector) →
  it takes the `pinned++` arm at `:491`, and the escape arm at `:496` is unreachable for it.

### Measured, with a temporary counting probe (restored in the same round)

Dropping `"workerControlStop"` from `rcsReadOnlyWorkerControls` makes the gate name its real
offenders — and the counted one is the site inside `runStopWithCtrlC`, i.e. the second
marker's function:

```
DEBUG OFFENDER worker_control_sigint_e2e_test.go: TestWorkerControlStop_NoWaitIsUnaffected
  argv=[f.bin,f.env,CliVerbWorker,workerControlStop,workerControlPIDFlag,<unresolved>,workerControlWait,0] daemonOnly=false
DEBUG OFFENDER worker_idle_drained_stop_e2e_test.go: TestWorkerDaemon_IdleDrainedStopLandsOnStopCommand
  argv=[bin,env,CliVerbWorker,workerControlStop,workerControlWait,wisStopCommandWait] daemonOnly=false
```

A second probe (`DEBUG MARKER-ORPHAN`, at the `if !spawns { continue }` arm) attributed the
**text** account per file, and named exactly the two markers:

```
DEBUG MARKER-ORPHAN: worker_control_sigint_e2e_test.go: newWorkerCtrlSigintFixture()
DEBUG MARKER-ORPHAN: worker_control_sigint_e2e_test.go: runStopWithCtrlC()
```

(probe transcript: `…/01a143a7-…/evidence/repo-context-orphan-marker-bug.md`)

### Is `worker --stop` a hazard at all?

No, and the gate already says so: `rcsReadOnlyWorkerControls` (`:207`) excludes
`workerControlStop` — the control face answers a question or signals a pid and resolves no
repo set (`cli_worker_control.go` reaches `tools.ControlDir`, `tools.PidAlive` and the
request file; never `GitSyncRepoRoot` nor a reclaim pass). So **neither** of the two sites is
an unpinned hazard, and neither should have carried an escape.

### The fix, and why it is not a weakened ratchet

`14b503278` deletes the two markers and replaces them with prose that states *why no escape
belongs there*. The escape token is deliberately spelled **without its trailing colon**
everywhere in that file — the gate counts the colon-terminated token as text, so prose naming
it in full would itself be counted as a marker.

Rule 13 is untouched: `git diff 1921b4a..HEAD -- cmd/agent/repo_context_sandbox_guard_test.go`
is **empty** (0 lines). No entry-point was removed, no recognition set widened, no floor
lowered, no marker account relaxed. The remaining two markers are exactly the two escaped
sites the scan counts:

```
repo-context gate: 108 lifecycle spawn site(s) scanned, 87 pinned, 2 escaped,
                   19 daemon-start (own-dir) site(s), 2 escape marker(s)
```

Both escapes are real and stay: `merge_lock_e2e_test.go:198: mergeE2EStartRun()` and
`worker_daemon_singleton_e2e_test.go:81: wdForeground()` — each reports its own reason in the
gate's log line, per the gate's design.

### The gate mechanism is filed, not repaired — bug `01a14577-c040-74de-8108-7ed0401fd781`

The root of this failure is a property of the gate, not of the two comments: the marker
account is keyed on **TEXT** and the escape account on the **AST**, and the token carries its
own trailing colon as part of its identity — so *quoting the escape in prose is
indistinguishable from using it*, and any writer who documents a pin can make the equality
impossible. This run did not touch the gate (Rule 41: the finder does not repair); the
mechanism, the probe transcript and the suggested directions (count markers only inside
counted spawn functions, or give prose a distinct spelling) are in the bug row and in
`…/evidence/repo-context-orphan-marker-bug.md`.

## What the reader should take away

1. A red in a briefing can be a red of the **briefing's revision**, not of the ground: check
   `git merge-base --is-ancestor <named-rev> HEAD` and run the test in a control worktree at
   the named revision before doing anything. Here red 1 was already fixed one commit below
   the ground, and the run's job was to show that with a measurement rather than assert it.
2. When a gate keeps two accounts of one fact, the two must be the same **kind** of account.
   A text count vs an AST count is a defect generator, and the failure it produces lands on
   whoever writes a comment last.
3. A marker whose site is pinned or excluded is an orphan, and the gate is right to fail on
   it — but the fix is to delete the marker, never to move the site into the escape arm.
