<!-- doc-class: normative -->
# Writer-owned scratch lifetime — the record the creating process leaves behind


**Class note:** this document makes a STANDING claim about master: it names the
writer-side registration, the verdict that reads it, and the CLI face that reports
it. Every file:line below resolves on the branch named in the report.

## The defect it closes

A per-run scratch root was collectable only when it was **BOTH** unheld **AND**
older than the stale window (`scratch.DefaultStaleAge` = 6h). A run that is
SIGKILLed leaves a root with no live process anywhere near it and an mtime of the
moment it died — so the residue is invisible to the sweep for the whole window, and
on a host whose runs fill a scratch filesystem in hours the sweep reports
`removed=0` while the tree grows. Measured on 2026-09-18: 137 entries in the home,
92 of them older than an hour, and only 3 of 175 candidates judged collectable —
the POLICY talking, not the mechanism.

The age window was standing in for a fact nobody had recorded: **who created this
root, and is that process still the process at that pid?**

## The mechanism

Every producer of a per-run root REGISTERS an owner record at creation time and the
sweep answers the exact question through the repository's ONE identity ladder.

```
scratch.RuntimeScratchDir / scratch.MkdirTempIn / scratch.CreateTemp
        │  (the writer, at creation time)
        ▼
   <root>/.holder      v=2   pid=<n>   start=<kernel start ticks>   cmd=<argv>
        │
        ▼  scratch.OwnerVerdictFor   (internal/scratch/owner_record.go)
   procname.RunnerIdentity(procname.DefaultProcRoot, procname.Liveness{Pid, StartTicks, CmdlineText})
        │  (internal/procname/procname.go — the ONE ladder, four outcomes)
        ▼
   gone | recycled        → the owner is PROVEN dead → collect, at ANY age
   live                   → keep, at any age
   unmeasurable           → keep (an unmeasured fact is never a death)
   (no record at all)     → fall through to the age window, as before
```

### The liveness predicate, quoted

The verdict is not a second implementation. `scratch.OwnerVerdictFor`
(`internal/scratch/owner_record.go:227`) makes exactly this call:

```go
outcome, why := procname.RunnerIdentity(ProcRootDir, procname.Liveness{
	Pid:         strconv.Itoa(rec.Pid),
	// The incarnation is the strong factor; the command line is consulted
	// only when the record states no incarnation (the ladder's own rule).
	StartTicks:  int64(rec.Start),
	CmdlineText: rec.Cmd,
})
```

and `procname.RunnerIdentity` (`internal/procname/procname.go:483`) is the ladder
the tool layer already routes its ledger rows through — the same one
`task_finish_cleanup.go`'s `finishCleanupProcessHolderLive` reaches via
`procname.RunnerLive`.

### Why pid + kernel start ticks (and not a clock, not a mtime, not a pid alone)

- **A pid alone is a number the kernel recycles.** Reading it as live reports a
  stranger as the recorded process; reading it as gone fabricates a death. The
  ladder refuses a record that states no factor at all and answers
  `OutcomeUnmeasurable`/`UnmeasurableNoRecord`.
- **The kernel start ticks (`/proc/<pid>/stat` field 22, ticks since boot) are the
  incarnation.** They answer "is this the SAME process?" across a pid reuse:
  `start` differs ⇒ `OutcomeRecycled` ⇒ the owner is gone, whatever the pid is
  doing now. That is the fact `mtime` cannot express and a monotonic clock cannot
  guess.
- **The command line is the third factor** and is deliberately weak: the ladder
  compares it only when no incarnation was recorded (`statesAnyFactor` /
  `identityCore`). It exists so a record that lost its `start` field still has
  something to compare, never so it can override the incarnation.
- **Direction is not symmetric, so the branches are not either.** A root that
  survives one more sweep costs disk; a root deleted from under a live run
  destroys a run's state. An unreadable record
  (`OwnerReasonUnreadable`) and an unmeasurable probe (`no-procfs`,
  `stat-unparsable`, `no-cmdline`) therefore BOTH land on keep.

### The verdict order in the sweep — the owner arm is FIRST

`scratch.entryVerdictInNamespace` (`internal/scratch/gc.go`), reached by both the
sweep and the report:

| # | arm | effect |
|---|---|---|
| ① | the writer's owner record (`OwnerVerdictFor`) | gone/recycled ⇒ **collectable now**; live/unreadable/unverifiable ⇒ **keep** |
| ② | the `/proc` holder veto (`DirHolder` → cwd/fd/exe/maps; batched as `LiveHolders`/`HeldByProcesses` for the queue) | a live process standing in it ⇒ keep; a probe that **errored** ⇒ keep |
| ③ | the age window (`ScratchEntryStale`) | applied **only** to a candidate that registered nothing |

Both pre-existing delete guards are untouched and still run:
`unheldRegistered` (`gc.go`) now asks `!OwnerKeeps(path)`, and
`scratch.declaredTrees.removeCollectableOfTree` (`internal/scratch/declared_trees.go:832`)
now refuses with `OwnerVerdictFor(path).Reason`. `HolderAlive`
(`internal/scratch/holder.go:419`) is preserved for its existing callers and
delegates to the same verdict; `ParseHolder` is now the incarnation-only face of
`ParseOwnerRecord`.

## The producers: who registers, and who does not

The list below is ENUMERATED, not sampled: the rule is "the entry IS the
directory the sweep judges", so a producer that hands the sweep a directory it
created itself must register, and a producer that creates a directory *inside* a
registered one is covered by its parent (the sweep judges entries directly under
the scratch home and under `queue/` — a grandchild is never a candidate).

| producer (file:line) | class | registers? |
|---|---|---|
| `internal/scratch/scratch.go:247` `RuntimeScratchDir` | every runtime class dir — the entry the sweep judges (`queue/queue-runtime-<class>-<pid>`) | **YES** — `WriteOwnerRecord(dir, os.Getpid())` at `scratch.go:270` |
| `internal/scratch/scratch.go:280` `MkdirTemp` | per-call dir **inside** a runtime class | covered by its parent: it calls `RuntimeScratchDir` (`scratch.go:281`), which registers |
| `internal/scratch/scratch.go:297` `MkdirTempIn` (new) | a dir inside a **caller-resolved** class dir | **YES** — `WriteOwnerRecord` at `scratch.go:302`; this is the constructor for a class dir the caller resolved itself, so such a caller cannot forget |
| `internal/scratch/scratch.go:310` `CreateTemp` | a **file** inside a runtime class | covered by its parent: `RuntimeScratchDir` at `scratch.go:311` (files are never sweep entries) |
| `internal/testutil/scratchroot.go:230` `PinSuiteTempRoot` | the suite run root (created at `:272` with `os.MkdirTemp(home, …)`) | **YES** — `registerScratchHolder(root)` at `:284` → `scratch.RegisterHolder` → `WriteOwnerRecord` |
| `internal/testutil/scratchroot.go:710` `ScratchDBPath` | a dir under the pinned run root (`os.MkdirTemp(SuiteTempRoot(), …)` at `:711`) | covered by its parent (the run root is registered) |
| `internal/testutil/dbfixture.go:122` `NewTestDBPath` | the writer-scaffold DB dir (`os.MkdirTemp(root, …)` at `:130`) | covered by its parent (`SuiteTempRoot()`) |
| `internal/tools/merge_build_gate.go:429`/`:444` `mergeBuildGateScratch` | merge-time build worktrees, inside `scratch.RuntimeScratchDir(RuntimeClassMergeBuild)` | **YES** — `scratch.MkdirTempIn(classDir, …)` at `:448` |
| `internal/tools/merge_union_gate.go:470` `mergeUnionGateScratch` | merge-time union worktrees, same class dir | **YES** — `scratch.MkdirTempIn(classDir, …)` at `:474` |
| ~~`internal/tools/merge_pass_verify.go:378` `mergePassOutputDir`~~ | **RETIRED 2026-11-01** — the merge-pass output dir inside its class dir, covered by its parent (`RuntimeScratchDir(mergePassScratchClass)`) | **gone**: the file was deleted with the retired top-level `gopi merge` verb's post-pass verify/withdraw lane, so this class no longer has a producer. Kept in the table as the record of a registered producer that existed; nothing writes `mergePassScratchClass` today |
| `internal/tools/tasks.go:6899` `taskVerifyGround` | verifier ground | via `scratch.MkdirTemp` ⇒ class dir registered |
| `internal/tools/job.go:116,120` | job stdout/stderr files | via `scratch.CreateTemp` ⇒ class dir registered |
| `internal/shot/shot.go:137`, `internal/tools/websearch.go:343`, `cmd/agent/cli_ui.go:174`/`:498`, `cmd/agent/commands.go:504` | the remaining runtime classes (shot, web-search, UI session, text2img) | via `scratch.MkdirTemp` / `scratch.CreateTemp` ⇒ class dir registered |
| `internal/tools/merge_union_gate.go:530` (`scratch.MkdirTempIn`), `merge_build_gate.go:639`, `merge_anchor_gate.go:1040` | merge-resolve scratch | via `scratch.MkdirTempIn` ⇒ class dir registered |

*(This row named three `cmd/agent` files when it was written; measured 2026-11-01,
`cmd/agent/merge_run.go`, `cmd/agent/merge_preland_gate.go` and
`cmd/agent/merge_union.go` are ALL absent — they went with the top-level merge verb.
The surviving merge-lane scratch producers are the three `internal/tools` gates named
above; each still registers its class dir before materialising.)*
| `internal/scratch/sandbox_gc.go:307` `SweepSandboxes` + `internal/sandbox/owner.go:101` | the agent **sandbox root** | **NO — and deliberately**: a sandbox is not a scratch entry. It carries its own marker (`.sandbox-owner.json`, task id + pid + start ticks) written by its own initializer, and its own reclaimer; naming `SandboxOwnerFileName` a second `.holder` producer would give one directory two owners |

`gopi scratch-gc` counting: a producer that registers and then dies without
cleanup leaves a root the NEXT sweep collects at any age — the property the
acceptance run below measures.

## The CLI face: `gopi scratch-gc --explain`

One line per candidate, on stdout, after the plan:

```
scratch-verdict <path> verdict=collectable|keep age=<d> outcome=<...> owner=pid:<n>/start:<ticks> <reason>
```

`outcome` is the owner judgement the line rests on — `gone`, `recycled`, `live`,
`unmeasurable`, or `unregistered` (a distinct word, because "nobody recorded an
owner" is the gap this work exists to close and must not render as if a writer had
been judged dead). `--explain` is additive: with `--dry-run` it is the vetting face
(the plan **and** the verdicts, nothing deleted); alone it is an ordinary sweep that
also prints the evidence, plus a per-removal line on stderr
(`scratch: collected <path> outcome=… owner=… (<reason>)`).

## Measured before/after (same fixture, both binaries)

Fixture: the leaking writer path (`scratch.RuntimeScratchDir(RuntimeClassJob)`, the
call every runtime producer goes through), started under a private scratch home,
then **SIGKILLed** — mtime of the leaked root: 3 seconds old, window 6h.

| step | master binary | this branch's binary |
|---|---|---|
| `gopi scratch-gc --explain` | **exit 1** — `unexpected argument "--explain" (want base, trees, sandboxes, --dry-run, --scratch-gc-stale-min or -h)` | exit 0, `verdict=collectable age=6s outcome=gone owner=pid:232676/start:607296592` |
| `gopi scratch-gc --dry-run` | `removed=0`; the leak stays | `removed=1`, `would-remove … age=6s` |
| `gopi scratch-gc` (real) | leak survives the whole 6h window | `removed=1` at age 6s; stderr `scratch: collected … outcome=gone owner=pid:232676/start:607296592` |
| second `gopi scratch-gc` | — | `removed=0` (idempotent) |

No periodic trigger was added: the one-shot verb and the existing resident tick are
the only faces, and the age window is still the operator's knob
(`GOPI_TEST_SCRATCH_STALE_MIN` / settings `scratch_gc_stale_min`, read once by
`scratch.ResolveStaleAge`).

## Tests that hold the contract

- `internal/scratch/owner_verdict_test.go` — real sweeps against real homes, both
  fixtures aged ~0 on purpose (if the verdict still needed the clock they are red):
  gone owner collected while fresh; live-elsewhere owner kept at fresh **and**
  stale; recycled pid collected; one sweep with both; an unreadable record kept; an
  unregistered root still bounded by the clock.
- `cmd/agent/cli_scratch_gc_explain_test.go` — the same directions through the REAL
  verb (`--explain`, `--explain --dry-run`, first run `removed=1`, second run
  `removed=0`, `outcome=gone|recycled|live` on the lines).
- `internal/testutil/scratchroot_test.go` `TestSuiteRootRegistersItsWriter` — the
  writer obligation asserted on the real pinned suite root: all three factors
  present, and the sweep's verdict on it is `live`/keep.
