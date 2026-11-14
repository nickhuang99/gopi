<!-- doc-class: historical -->
# Task Report: the peak-hour cost guard becomes cost-aware

> **RETIRED (2026-09-16).** The guard this report is about — and every `--ignore-peak-hours` /
> `GOPI_IGNORE_PEAK_HOURS` spelling used below — was removed outright by `fix/remove-peak-guard`
> (commit `bb0c4c1a`, devdoc/peak-guard-removal.md). The knob is read by nobody and no exemption
> is needed to run anything; the report is history, not a recipe.

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: **peak-guard-cost-aware** — branch `fix/peak-guard-cost-class`, off master
`c0e4c809`.

## The defect (reproduced, then measured)

The peak-hour cost guard (`cmd/agent/peak_guard.go`, called from `main()` before
the subcommand chain) refused to **start any `gopi` process** inside DeepSeek's
Beijing peak windows (09:00–12:00 / 14:00–18:00) at 2x token rates. Its
placement was deliberate — the file comment says "so no verb escapes it
(agent prompt, worker, merger, task run, chat, web, memory, every future verb)"
— and its only exemption was a bare usage query (`isUsageQuery`).

"Every mode" is too strong a reading of the money question. A start that issues
no LLM call protects no money by being refused; it only lets the **wall clock**
decide whether an ordinary command works. Reproduced on the built binary:

```
$ GOPI_PEAK_HOURS=00:00-24:00 ./gopi version
[gopi] peak-hour cost guard: refusing to start — `gopi version` would run inside
       DeepSeek's peak window 00:00-24:00
$ echo $?
4
```

Measured blast radius, with the guard ARMED for 24h and the suite's own pin
lifted (`GOPI_PEAK_HOURS=00:00-24:00 go test ./cmd/agent/ -count=1`):

| run | failing cases |
|---|---|
| master `c0e4c809`, pin lifted | **84** |
| this branch, pin lifted | see "Verification" |

The 84 fall into two groups:

1. **First order** — the guard refuses the process under test: every
   worker/merger lifecycle e2e rig, `gopi version` cases, the voice/web/memory
   rigs. `TestMergerRestart_NoAlarmWordingAndTheCgroup…` is red for four hours a
   day (documented in `testmain_test.go`'s pin comment, measured 2026-09-14
   14:00:07 CST).
2. **Second order** — the merger's verify child (`gopi task verify`, spawned through
   `GOPI_MERGE_VERIFY_BINARY`) is refused inside the rig, so
   `TestMergeVerifyGatePassKeepsMerge` reports "a PASSING verify must KEEP the
   merge": the gate that decides whether a merged branch is kept was itself
   gated by the clock.

And `gopi worker --daemon` — the operator's own service — could not be started
inside a window at all.

## What this branch changes (one idea)

**The guard asks the money question, not the clock question.**

```
before:  if !isUsageQuery(argv) { argv = applyPeakGuard(argv) }
after:   if spendingStart(argv)   { argv = applyPeakGuard(argv) }
```

and the guard itself is untouched: the same banner (`config.PeakRefusal`), the
same exit code (`config.PeakGuardExitCode = 4`), the same escape hatches
(`--ignore-peak-hours`, `GOPI_IGNORE_PEAK_HOURS=1`, settings.json), the same
in-round deferral for the resident worker/merger (`workerPeakWait`).

`spendingStart` lives in the new `cmd/agent/cost_class.go`:

- **Pure predicate over argv** — no clock, no env, no process state, no
  spawning. That is what makes it testable at any hour and what lets the
  determinism test *inject* the peak moment (`GOPI_PEAK_HOURS=00:00-24:00`)
  instead of patching `time.Now`.
- **Constants, never literals** (Rule 1b/13): every verb in the tables is the
  constant the dispatch chain compares against (`CliVerbXxx`, `taskverbs.*`,
  `toolnames.ToolXxx`) — a rename is a compile error, not a silent
  reclassification.
- **Fail closed for the money**: the default is SPENDING. Only an explicitly
  enumerated free SHAPE is exempt, so a verb nobody classified is refused
  exactly as before.

### The classification (and the judgement calls, stated so they can be revisited)

**Never refused (free):** `version`; every help spelling (`-h`, `--help`,
`help`, `help-env`, `--help-env`, and `-h`/`--help` after any verb — reading the
contract from inside a window is exactly when it is needed); `log`, `stats`,
`raw`, `proxy`, `scratch-gc`, `completion`; `memory` (incl. `memory sync`),
`memsync`, `memory_sync`; `code-search`; `shot`, `act`; the tools that drive
robots rather than a model (`web_search`, `web_fetch`, `screen_capture`,
`voice_input`) and the group forms `gopi web search|fetch`; `gopi chat
list|backfill` (the chat INDEX — a ledger query; every other chat argv starts
the ReAct loop).

**Still refused (spending):** the bare prompt path; `task run` (the executor);
`task verify` (**newly recognised as spending** — it spawns a real agent child
with a verification briefing, `internal/tools/taskverify.go`); `chat`; the `web`
server (LLM-backed); `brew`; `resume`; worker START (`--daemon`, `--once`,
foreground: a round spawns the executor child); merger START (a round can spawn
the conflict RESOLVE child and the merge VERIFY child, both agent sessions).

**Two decisions worth their own paragraph:**

1. **Worker/merger CONTROL verbs are free.** `--status`, `--stop`, `--restart`,
   `--start` act on a *resident* service from a separate process: no lock, no
   round, no model. This is the case that matters most operationally — inside a
   peak window, "the daemon is burning 2x money, stop it" must not itself be
   refused. Their starting forms stay refused, so the deferral contract
   (`workerPeakWait`) is unchanged.
2. **The merge-family `task` verbs are free** (`task merge`, `merge-execute`,
   `merged-cleanup`, `audit-merges`, `sync`, `harvest`, `adopt`, `tag-resumed`,
   `sweep-*`, `improvements`, `verdicts`, plus the read/mutate verbs). They do
   git and ledger work; the merge they land is judged by the merger's own verify
   gate when the *merger* lands a branch (which does require a worker/merger
   start, and those stay refused). The alternative — refusing `task merge` —
   would block the operator's recovery path (`gopi task merge` by hand) exactly
   when a resident merger is draining for the peak window. Deliberate, recorded
   here rather than discovered later.

Also documented in `cost_class.go`: `vision_analyze` / `text2img` are classified
with the other tool mirrors (one standalone model call, not a session) — the
one place the classifier is deliberately generous, because refusing them
re-introduces the clock-decides defect for the vision/OCR path an operator
drives from a cron tick.

## Files

| file | change |
|---|---|
| `cmd/agent/cost_class.go` | NEW — the `costClass` predicate, the constant-referencing tables, and the classification's reasoning |
| `cmd/agent/main.go` | the guard call site becomes `if spendingStart(args) && spendingStart(classifyOn)`; the comment block records the cost-class rule |
| `cmd/agent/cost_class_test.go` | NEW — both directions per verb, the constant round-trip, and the dispatcher-coverage drift guard |
| `cmd/agent/peak_guard_costaware_e2e_test.go` | NEW — the determinism test (one injected 24h window, both directions in the SAME run), the escape-hatch pin, the suite-pin anti-regression test, and `disarmedPeakEnv` |
| `internal/standards/baseline.go` | Rule 16 baseline re-keyed 1035 → 1048 (the main.go comment block grew; same site, same two `%v` verbs on a STRING constant) |
| `cmd/agent/testmain_test.go` | the suite pin's rationale rewritten (still set; see below) |
| `cmd/agent/merger_singleton_e2e_test.go`, `merger_graceful_restart_e2e_test.go`, `worker_daemon_flag_e2e_test.go`, `agent_spawn_guard_test.go` | their shared env builders call `disarmedPeakEnv` — the arm-dependent rigs state the operator's hatch explicitly |

## The suite pin (`GOPI_IGNORE_PEAK_HOURS`) — measured, then re-scoped

> **RETIRED — kept as history.** The guard and this pin were removed outright by
> `fix/remove-peak-guard` (commit `bb0c4c1a`, devdoc/peak-guard-removal.md). The variable is
> read by nobody today, no suite needs it, and no reader should treat any line below as an
> instruction to set it.

`cmd/agent`'s `TestMain` disarmed the gate for the whole suite
(`unpinPeakHourGate`) so the wall clock could not decide a test's verdict. The
epic's requirement was to resolve that pin **by measurement**, not by assumption:
run the suite with the pin lifted and the guard armed for 24 hours, and see what
is actually left.

**Run 1 — master's classification, pin lifted: 84 failing cases.** Every failure
was the guard refusing the process under test. Four findings, all of which the
first design had missed:

1. `gopi --version` / `gopi -v` — only the `version` WORD was in the free table;
   the version FLAGS (`cli_version.go`'s `case CliVerbVersion, "--version", "-v"`)
   were not. (`TestVersionSubcommand`.)
2. `gopi memory_store` / `memory_search` / … — the LEGACY FLAT-VERB aliases
   (`memoryLegacyAliases`, `cli_memory.go`) dispatch to the same free `memory`
   group, but classified as spending; they refused the memory-sync rig's own
   seeds (`cli_memory_sync_test.go:152`, exit 4).
3. `gopi -h --settings X` — the help token was looked for in the argv
   `BootstrapEnvFromArgs` had already stripped, so a usage query carrying a
   config token stopped being recognised as one.
4. `gopi worker -h` — the same shape: every dispatcher answers `-h`, but the
   classifier only recognised the bare-usage-query form (`isUsageQuery`).

Fixes: `outputOnlyVerbs` + the two version-flag constants and
`costVerbFree`/`memoryLegacyAliases` for (1) and (2); `hasHelpWord` over the
ORIGINAL `args` plus `classifyOn` for the reverse shape (`gopi --settings X log
--last`) for (3) and (4).

**Run 2 — after those fixes: 55 failures, of which 40 were a STALE BINARY.**
This is worth recording because it nearly caused a phantom bug hunt:
`testutil.GopiBin(t)` (internal/testutil/run.go:34) resolves `$GOPI_BIN` → the
repo's **`bin/gopi` FILE** → `t.Skip`; it never rebuilds. The file in the
worktree was from 18:04, the source from 18:38, so those 40 cases asserted the
OLD guard's behaviour and their failures looked like "the classifier did not
take effect". Proof: `GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi worker -h` exited 4
while the freshly built binary exited 0. Both resolutions were applied — rebuild
`bin/gopi` AND export `GOPI_TEST_BIN` to the fresh binary for the suite run
(the other half of the suite uses `gopiTestBinPath()`, which DOES rebuild).
Recorded to the knowledge base.

**Run 3 — fresh binary, hatch NOT yet stated per rig: 48 failures / 44
guard-caused.** Every guard-caused failure named a SPENDING verb — `gopi worker`,
`gopi task run`, `gopi merger`, `gopi web` — i.e. not a classifier defect but a
set of rigs whose SUBJECT is a spending start:

| rig | why it needs a spending start |
|---|---|
| `agent_spawn_guard_test.go` (`guardTestEnv`) | launches real `gopi worker --once` / `task run` to prove the LAUNCH rules |
| `worker_daemon_flag_e2e_test.go` (`wdEnv`) | starts real detached workers; asserts the foreground/daemon split |
| `merger_singleton_e2e_test.go` (`msingEnv`) / `merger_graceful_restart_e2e_test.go` (`mgrEnv`) | start real mergers that really merge (`gopi merger --once`, restart, stop) |
| web-server rig | starts the LLM-backed server |

With the gate armed, those rigs test the gate instead of their subject.

**The resolution — the hatch moves from ambient to explicit.** A new helper
`disarmedPeakEnv(env)` (peak_guard_costaware_e2e_test.go) sets the OPERATOR'S OWN
override (`config.IgnorePeakHoursEnv` — the documented `--ignore-peak-hours` /
`GOPI_IGNORE_PEAK_HOURS=1` escape a human uses to work inside a window knowing
they pay 2x) and it is called by exactly the three shared env builders those rigs
use (`guardTestEnv`, `wdEnv`, `msingEnv` → `mtlEnv`, `mgrEnv`). Each call site
carries a comment naming the spending start it covers, so the decision is
greppable and per-rig.

`TestMain`'s pin is KEPT, with a rewritten rationale (it is now a belt, not the
mechanism): a new rig that starts a spending process without saying so cannot go
red for four hours a day in a way nothing announces. The armed direction is not
lost, because it is pinned explicitly and hermetically — with an injected window,
so it holds at any hour:

- `TestPeakGuardIsCostAwareBothDirections` — free and spending in ONE run.
- `TestPeakGuardCostAwareKeepsTheEscapeHatch` — the override still wins.
- `TestPeakGuardStaysArmedWhenTheSuitePinIsSet` — the pin cannot be mistaken for
  the gate having been removed: the test clears the hatch itself and requires the
  refusal.

That is the acceptance bar's "documented + pinned at the suite entry point"
branch, with the difference that the pin now covers a *named, small* set rather
than "every verb" — the 84-failure blast radius is gone.

## Verification

**Build**

```
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     # BUILD_OK
go vet ./cmd/agent/ ./internal/standards/           # clean
gofmt -l cmd/agent cost_class.go …                  # clean
```

**The accepted defect, fixed (measured on the built binary, guard armed for 24h)**

```
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi version          → exit 0    (was exit 4 + banner)
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi --help           → exit 0
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi worker --status  → exit 0
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi task list        → exit 0
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi --version        → exit 0
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi memory_search q  → exit 0
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi say hello        → exit 4 + banner   (unchanged)
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi task run t-1     → exit 4 + banner   (unchanged)
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi chat             → exit 4   (the ReAct loop)
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi worker --daemon  → exit 4   (unchanged)
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi merger --once    → exit 4   (unchanged)
$ GOPI_PEAK_HOURS=00:00-24:00 ./bin/gopi --ignore-peak-hours "say hello" → NOT refused (hatch works)
```

**The suite, full run inside an injected 24h peak window**

```
$ GOPI_TEST_BIN=$PWD/bin/gopi GOPI_PEAK_HOURS=00:00-24:00 \
      go test ./cmd/agent/ -count=1
--- FAIL: TestWorkerDaemon_StatusReportsTheLiveDaemon (0.26s)
FAIL  (1 failure)
```

Before this branch, the same command (master's classification, suite pin lifted)
was **84 failures**. `TestWorkerDaemon_StatusReportsTheLiveDaemon` is
**pre-existing and unrelated** — it fails identically on master with the fresh
binary and NO window injected (verified in `/home/user/work/GoPi`), and its
failure text is about the worker STATE RECORD a foreground-launched daemon
cannot write, not about the guard:

```
worker_daemon_singleton_e2e_test.go:170: status must name the live daemon's
  state record for pid N, got: gopi worker pid N: idle — gopi-worker worker
  (no state file — identified by argv; it predates the state record …)
```

**The suite, normal developer run (no window injected)**

```
$ go test ./cmd/agent/ -count=1
same single test: TestWorkerDaemon_StatusReportsTheLiveDaemon
```

i.e. the armed and normal runs are now INDISTINGUISHABLE — the clock no longer
changes any test's verdict, which is the epic's acceptance bar.

**Touched-package tests**

```
$ go test ./internal/standards/ -count=1     # ok  (Rules 16-21, incl. my Rule 16 re-key)
$ go test ./internal/config/ ./internal/peakhours/   # ok, ok
$ go test ./cmd/agent/ -run TestCostClass    # ok
$ go test ./cmd/agent/ -run TestPeakGuard    # ok (both directions + hatch + pin anti-regression)
```

**Commit** — `fix/peak-guard-cost-class` (5 commits, off master `c0e4c809`):

```
e69fa3df docs(standards): re-key the Rule 16 baseline for the grown main() comment
56cbc461 test(peak-guard): the arm-dependent rigs state the hatch themselves
012699c0 test(peak-guard): the suite pin becomes a belt, not the mechanism
2afd1df3 feat(peak-guard): wire main() to the cost class
c294e407 feat(peak-guard): the start gate asks the COST question, not the clock question
```

## Not Done

- `TestWorkerDaemon_StatusReportsTheLiveDaemon` is red on this branch AND on
  master (reproduced there with the fresh binary and no window) — a separate,
  pre-existing worker-state-record defect. Not fixed here: it is a different
  subject and fixing it inside the guard's branch would hide it in this diff.
- The tool-mirror classification (`vision_analyze`, `text2img` free) is a
  judgement call, documented in `cost_class.go` rather than measured against
  spend data. If the operator wants them refused inside a window, the change is
  one table entry + one test row.
- `cmd/agent/cli_worker_control.go` is gofmt-unclean on master (pre-existing,
  untouched by this branch).
