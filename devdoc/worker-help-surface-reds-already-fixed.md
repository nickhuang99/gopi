<!-- doc-class: historical -->
# Task Report: the five `./cmd/agent` worker-help-surface reds are ALREADY GREEN — no change was needed

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a133af-4991-7a35-8b0b-27c5ce9a8d28`, branch `task/01a133af49917a358b0b27c5ce9a8d28`,
issued 2026-10-13 13:14:48 and measured at **`153c0942e`** (master tip at the moment of this
run; this branch's HEAD is that commit — no commits were added to it, see `## Not Done`).

## Completed

**The epic's premise did not hold at this run's start, and that is the finding.** Every one
of the four named tests **PASSES on this branch as found**. All four derived artifacts the
epic asks to regenerate are already present, already regenerated, and already consistent with
the binary this branch builds. The fix landed on master **three minutes after** the discovery
run measured its red — the discovery is real, its reproduction is honest, and its reading of
the cause is correct; it was simply overtaken.

The four tests, run verbatim (raw output in
`artifacts/green-four-tests.txt`):

```
--- PASS: TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation (0.00s)
--- PASS: TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason (0.00s)
--- PASS: TestPublishedHelpPagesStayWithinTheirUsageBudget (2.66s)   [incl. .../worker]
--- PASS: TestManSourcesMatchTheirHelpPage
ok  	github.com/nick/gopi/cmd/agent	13.240s
```

The four artifacts, each verified present and consistent (`artifacts/`):

| # | artifact the epic names | state at `153c0942e` |
|---|---|---|
| 1 | G6 inventory table | `devdoc/gopi-tools-maintenance-help-surface-inventory.md` row 121/122 list `gopi worker --stop --branch B` / `gopi worker --stop --pid N`, both marked *"yes — typed by `TestToolsMaintenanceHelpSurfaceFormsAreTyped`"* |
| 2 | tested/blocked case list | `cmd/agent/cli_tools_maintenance_help_surface_cases_test.go:1641,1649` — both forms typed, each with its refusal assertion |
| 3 | `man/gopi-worker.md` | matches the `-h` page (the gate compares the SYNOPSIS block, every flag name, and the verb index) |
| 4 | usage-length budget | `workerUsageBudget = 136` with a dated rationale: the page measures **124** lines and `124 + usageBudgetNewFlagMargin(12) = 136` — at the page's own length plus the room exactly one new knob's paragraph needs, **no slack beyond it** |

## Not Done

- **No code, table, man source or constant was changed, so this branch carries no commits
  beyond its base.** Every deliverable the epic names was already satisfied; changing a
  passing artifact would be churn and changing `workerUsageBudget` (already correct at 136)
  would *widen* a reviewed cap for no reason. The honest deliverable is this record.
- **`TestTaskVerbSweepWorktreesRunsAsAdvertised` — root-caused, NOT fixed (it is not
  flaky, and it is not this epic's subject).** The discovery run reported it as
  "flaky or environment/load sensitive … an observation, not a claim". The observation is
  right and the mechanism is now measured: it is a **TIMEOUT under load**, not a logic
  failure. From the discovery run's own machine-readable stream
  (`…/01a13388-…/scratch/queue/test-budget/run-1791867058-1254491.json:11147`):

  ```
  cli_tasks_help_surface_basic_test.go:852: gopi task sweep-worktrees --db /dev/shm/…/g1-ledger.db:
      TIMEOUT after 25s — argv fell through to the agent loop, or parked on a child
  ```

  The probe's per-invocation ceiling is `g1TaskVerbTimeout = 25` seconds
  (`cli_tasks_help_surface_basic_test.go:65`), and the verb it bounds is **not** a ledger
  read: `runTaskSweepWorktrees` (`cmd/agent/cli_tasks.go:4275`) walks **every repo in the
  settings table**, and `internal/tools` runs it against `/home/user/work/GoPi` — the shared
  main checkout, which at this run's cut held **31 registered worktrees**. Measured cost of
  the same invocation on this host (`artifacts/verb-timing.txt`): **2.9–5.2 s** at load
  ~3–4; **4.6 s at load 12.6**. The 25 s ceiling is ~5–8× the measurement, so the margin is
  not absurd — but it is a **wall-clock bound on work proportional to the number of live
  worktrees and the host load**, both of which grow without bound, so the failure recurs
  whenever the sweep is slow (concurrent suite runs each holding worktrees is the observed
  shape). Reported, not repaired: the remedy is a budget/phasing decision on the probe
  (`cli_tasks_help_surface_basic_test.go`) and is a `cmd/agent` test-infrastructure change
  with its own acceptance, filed as a Rule 41 observation on the row rather than smuggled
  into a "regenerate a help table" change.
- **The fifth/second-order red the discovery also mentioned, `TestDocManifestCoversEveryDevdocFile`
  in `./internal/standards`, is NOT present here**: it PASSES, because this branch adds no
  untracked devdoc — this record was classified into `devdoc/DOC-INDEX.tsv` in the same
  commit that lands it (Rule 36).
- **A sibling row exists**: `01a133b9-6e13-7a11-bdbd-8253c7a17379` ("Two pre-existing master
  reds in ./cmd/agent: worker --stop help text not in the maintenance inventory, and gopi
  worker -h over its usage budget", filed 13:25:52 — **eleven minutes after this row and one
  minute before the fix merge at 13:25:31+**). Same subject as this epic. A reader of that
  row should read this record first: the two named reds are green at `153c0942e`.

## Why the premise failed — the timeline, measured (`artifacts/fix-provenance.txt`)

```
12:34:15  afbf9b572   ← the discovery run's pristine worktree ("master afbf9b572")
                      inventory row = 0 hits for `stop --branch`; workerUsageBudget = 114
12:37:21  b95f24ec1   fix(help): type the worker target-selector forms and re-derive the G6 inventory
                      — 4 files, 143+/87-: the cases list (+24), the budget (114→136),
                        devdoc/gopi-tools-maintenance-help-surface-inventory.md (172 lines
                        rewritten), man/gopi-worker.md (+16/-16)
13:14:48  this task row submitted (from a session that had measured afbf9b572 at 12:34)
13:25:31  00a0f7803   master
13:42:48  153c0942e   master tip = this branch's HEAD (b95f24ec1 is an ANCESTOR)
```

Per first-parent master commit, the inventory's `stop --branch` hits and the budget constant
flip together and never flip back (`inv=0 workerUsageBudget = 114` through `bb9095708`, then
`inv=1 … = 136` from `00a0f7803` onward). **The epic's own briefing anticipated this**:
*"the cap in this tree already reads workerUsageBudget = 136 with a dated rationale (master
may already carry a partial fix — verify before changing)"* — the instruction to verify was
the load-bearing one, and verification says the fix is complete, not partial.

## Red proof — the epic's four failures are REAL at the pre-fix commit, and the artifacts are what fixes them

Not asserted from the git log: a **detached probe worktree at `b95f24ec1^`** (the fix's
parent, 12:01:26) was built and driven (`artifacts/red-proof-prefix.txt`). It reproduces the
discovery run's output **exactly**, including which lines:

```
--- FAIL: TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation (0.00s)
    cli_tools_maintenance_help_surface_basic_test.go:426: `gopi worker --stop --branch B` is advertised by the help (worker) but the inventory does not list it — re-run scripts/g6-tools-maintenance-help-surface.sh ./bin/gopi and regenerate the table
    cli_tools_maintenance_help_surface_basic_test.go:426: `gopi worker --stop --pid N` … (same)
--- FAIL: TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason (0.00s)
    cli_tools_maintenance_help_surface_basic_test.go:945: "gopi worker --stop --branch B" (worker) is advertised and NEITHER tested NOR declared blocked …
--- FAIL: TestPublishedHelpPagesStayWithinTheirUsageBudget (11.38s)
    cli_usage_budget_test.go:170: `gopi worker -h` is 124 lines — over the 114-line budget by 10.
--- FAIL: TestManSourcesMatchTheirHelpPage (0.29s)
```

Measured at that commit, and non-zero only in the fixed tree (`fix-provenance.txt`):

```
at b95f24ec1^ : inventory `stop --pid` hits = 0, `stop --branch` hits = 0
                man/gopi-worker.md  hits = 0 / 0
                cases file          hits = 0 / 0
                workerUsageBudget = 114
at HEAD       : inventory 1, cases 1 each, man 1 each, workerUsageBudget = 136, page = 124 lines
```

**The derived artifacts are the fix** — no production code changed between the red and the
green, which is exactly why this epic is a regeneration task and why nothing was regenerated
here.

## Re-derivation — Rule 31: the script and the generator, run live, not read

The inventory gate the epic names is `TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation`.
Its own re-derivation command was executed against the binary this branch builds:

```
scripts/g6-tools-maintenance-help-surface.sh ./bin/gopi       → 177 rows (exit 0)
python3 scripts/g6-inventory-doc.py <that.tsv> cmd/agent <out>
      → "wrote …: 177 rows, 173 distinct forms, 142 typed, 31 blocked"
diff -u devdoc/gopi-tools-maintenance-help-surface-inventory.md <out>   → NO OUTPUT
```

The tracked table is **byte-identical** to what the derivation produces from this binary —
the strongest form of "the table is not stale". The two worker rows appear in the fresh
derivation:

```
worker	gopi worker --stop --branch B	branch,stop	2
worker	gopi worker --stop --pid N	pid,stop	2
```

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **exit 0**.
- **Tests — the four named by acceptance:**
  `GOPI_TEST_SCRATCH_HOME=/home/user/workspace/gopi-scratch-01a133af go test ./cmd/agent/
  -count=1 -run 'TestManSourcesMatchTheirHelpPage|TestPublishedHelpPagesStayWithinTheirUsageBudget|
  TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation|TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason'`
  → **ok, 13.240s**, all four PASS (the `/worker` subtest passes inside the budget test,
  printing `gopi worker -h: 124 lines (budget 136, margin 12)`).
- **Tests — the package acceptance:** `GOPI_TEST_SCRATCH_HOME=<off-/ dir> make test-budget
  PKGS='./cmd/agent'` — see `## Acceptance run` below.
- **Ran (Rule 31) — the SURFACE, not just the suite.** The epic's subject is a help surface,
  so the page itself was printed and the derivation script driven against the real binary —
  both above. The four artifacts are each a live observation, not a reading of a test name.
- **Result:** the four tests pass, the table re-derives byte-identically, the page is inside
  its reviewed cap, the man source matches, and `git status --porcelain` prints nothing.
- **Commit:** the record's own commit on this branch (see `## Evidence` §8 for the hash).

## Acceptance run

`GOPI_TEST_SCRATCH_HOME=/home/user/workspace/gopi-scratch-01a133af make test-budget
PKGS='./cmd/agent'` — the epic's stated acceptance. Started 13:47, still running at the
90-minute session cut (the package's own budget is 25m; the full `./cmd/agent` package takes
~8–13 min when the host is quiet and materially longer at the load this host carried all
afternoon — `cat /proc/loadavg` read **12.64 9.82 6.84** mid-run, with a *second* full
`./cmd/agent` run from a concurrent session in flight). Its per-package stream is at
`/home/user/workspace/gopi-scratch-01a133af/queue/test-budget/run-*.json`; the run is named
in the closing message to the operator rather than claimed here, because **a gate result the
run did not observe is not a reportable result**.

## Evidence (self-filed)

Raw material: `/home/user/.gopi/sessions/01a133af-4991-7a35-8b0b-27c5ce9a8d28/artifacts/`
(Rule 37 — outside the worktree).

1. **Green four tests** — `go test … -count=1 -run '<the four>'` → `ok … 13.240s`; the
   `-v` arm printed `--- PASS:` for each (quoted at the top).
2. **Artifact 1 re-derived** — `scripts/g6-tools-maintenance-help-surface.sh ./bin/gopi` →
   177 rows; `python3 scripts/g6-inventory-doc.py <tsv> cmd/agent <out>` → `177 rows, 173
   distinct forms, 142 typed, 31 blocked`; `diff -u` against the tracked table → **no output**.
3. **Artifacts 2/3/4 read back by line** — `grep -n 'stop --pid N\|stop --branch B'
   devdoc/gopi-tools-maintenance-help-surface-inventory.md` → rows 121, 122;
   `grep -n 'stop --pid\|stop --branch' cmd/agent/cli_tools_maintenance_help_surface_cases_test.go`
   → 1641, 1649; `./bin/gopi worker -h | wc -l` → **124**; `grep -n 'workerUsageBudget ='
   cmd/agent/cli_usage_budget_test.go` → `= 136`.
4. **Fix provenance** — `git merge-base --is-ancestor b95f24ec1 HEAD` → **holds** (the fix is
   an ancestor of this branch's HEAD); `git log -1 --format='%H %ci %s' b95f24ec1` →
   `2026-10-13 12:37:21 fix(help): type the worker target-selector forms and re-derive the G6
   inventory`; `afbf9b572` (12:34:15) is an ancestor too, and the per-first-parent scan flips
   the inventory/budget from 0/114 to 1/136 between `bb9095708` and `00a0f7803`.
5. **Executable red proof** — detached worktree at `b95f24ec1^`, `CGO_ENABLED=0 go build`
   OK, `go test … -count=1 -run '<the four>'` → the four `--- FAIL:` lines quoted above,
   matching the discovery run's output. Probe worktree removed after the measurement
   (`git worktree remove --force /home/user/workspace/gopi-prov-red-01a133af`). Rule 5's
   RESTORE criterion does not apply: **no production file was mutated** — the probe used a
   separate detached worktree, and this branch's tree was never written to.
6. **Flaky test root cause** — the discovery run's own stream, line 11147:
   `…TIMEOUT after 25s…` for `TestTaskVerbSweepWorktreesRunsAsAdvertised`;
   `grep -n 'g1TaskVerbTimeout'` → `cli_tasks_help_surface_basic_test.go:65: … = 25 // seconds`;
   direct timing of the bounded verb: 2.93 / 3.46 / 4.57 s at load ~9.8; 4.6 s at load 12.6;
   `git worktree list | wc -l` → **31**; isolated re-runs of the test **PASS** (three
   consecutive: 19.4 / 12.6 / 14.9 s wall for the package).
7. **Rule 30** — `git status --porcelain` in the worktree after the commit: prints nothing.
8. **Commit / branch** — the record commits to `task/01a133af49917a358b0b27c5ce9a8d28`; the
   hash is reported in the closing message (a report cannot carry its own commit's hash
   without a second commit, and a second commit to fix a hash is not worth the churn).
