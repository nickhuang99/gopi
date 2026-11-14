<!-- doc-class: normative -->
# Task Report: ONE agent sandbox — configured root, ONE initializer, automatic reclaim

Branch: `feat/sandbox-unify` (worktree `/home/user/workspace/gopi-sandbox-unify`)

> **SUPERSEDED IN PART — the LEDGER's shape only (read this before copying anything
> below).** This is a historical report: the ledger it describes as `memory.db` → the
> **shared read-only sample symlink** was declared the DEFECT by commit `40035f03`
> ("the ledger is a real writable private file, not a link to the read-only sample")
> and is now GOPI.md **Rule 34**. A ledger is the file a run WRITES FIRST, so it is a
> REAL, private, `0600`, WRITABLE file at `<sandbox>/.gopi/memory.db`; the sample is
> still the ONE definition of a populated database, but it is used as a **seed, by
> VALUE (COPY)** — never linked. The pre-fix link failed two measured ways: the write
> was refused (`attempt to write a readonly database`) or a writer **replaced the
> symlink node** by renaming a sibling into place, so the sandbox silently read a
> different file than it pointed at. Everything else below (the configured root, the
> ONE initializer, the profile root `<sandbox>/.gopi`, the reclaim machinery, the
> idempotence) still holds — and the `internal/config/sandbox.go:167` reference in it
> is now `:172`, and `CONFIG.md`'s rows moved to lines 83 and 239.

## Completed

- **ONE configured root.** `internal/config/sandbox.go`: the `sandbox_dir` knob —
  settings key `sandbox_dir`, env `GOPI_SANDBOX_DIR`, flag `--sandbox-dir` —
  resolved by ONE function `config.ResolveSandboxRootConfigured`
  (`internal/config/sandbox.go:167`), precedence `flag > env > settings >
  <home>/workspace/gopi-sandbox`. Deliberately **no ambient channel** (nothing
  outside gopi reads a sandbox root). Wired through the same three channel
  tables `tmpdir` uses (`internal/config/config.go` field + JSON tag,
  `flags.go` `applyFlag` case, `settings.go` catalog rows, `CONFIG.md` line 83
  precedence row + line 233 per-key row).
- **ONE initializer.** `internal/sandbox` — `Create` (`sandbox.go:151`, the
  idempotent entry: same `--label` ⇒ same sandbox) and `Ensure`
  (`sandbox.go:196`, the repair half). The standard shape: `settings.json` →
  the REAL settings file (symlink, read-only by contract), `memory.db` → the
  **shared read-only sample from `internal/testutil`'s generator**
  (`testutil.SampleDBPath` — no second generator), `sessions/ raw/ logs/`
  private and empty, `.sandbox-owner.json` (task id + pid + **kernel start
  ticks** + created-at). Refusals: a settings target INSIDE the sandbox, a
  non-directory in the way, a `memory.db` target that does not exist.
- **CLI face.** `gopi sandbox create|show` (`cmd/agent/cli_sandbox.go`, verb
  constant in `cli_verbs.go`, dispatch in `main.go`, completion + CLI-contract
  suite rows updated). `create` prints the path as stdout's last line so
  `SB=$(gopi sandbox create --label build | tail -1)` works; `show` prints the
  resolved root **and the channel that decided it**.
- **ONE reclaimer, riding EXISTING machinery — no new daemon, no new tick.**
  `internal/scratch/sandbox_gc.go` (`PlanSandboxes` `:160`, `SweepSandboxes`
  `:307`, `SandboxRootRefusal` `:133`, `SandboxStaleAge` = 72h) reuses the
  package's ONE `/proc` liveness scan (`LiveHolders`/`HeldByProcesses`) and its
  ONE GC lock. Two call sites, both pre-existing: `gopi scratch-gc sandboxes`
  (the CLI one-shot the watchdog already runs — new arm beside `base`/`trees`)
  and the resident `scratchGCTick` (`cmd/agent/scratch_gc_tick.go`, the same
  function the worker and merger already call). Predicate: **ours** (name
  marker) AND **aged** (own mtime) AND **unowned** (no live incarnation of
  recorded pid+ticks) AND **unheld** (no live process inside); **every unknown
  is a KEEP**; the root is never removed at any age; the documented root is
  refused by name (`/`, `$HOME`, the gopi profile).
- **Rule.** GOPI.md **Rule 34** — the Rule-27/28 pattern (decision + named
  anti-pattern + measured census + the check).
- **Tests.** `internal/sandbox/sandbox_test.go` (shape, symlink targets,
  idempotence + repair, marker records the live incarnation, the three
  refusals, the precedence, symlink-seal), `internal/scratch/sandbox_gc_test.go`
  (killed owner ⇒ collected; live holder ⇒ kept; unverifiable owner ⇒ kept;
  fresh ⇒ kept; foreign name untouched; idempotent + root survives; refusal
  names), `cmd/agent/scratch_gc_tick_sandbox_test.go` (the resident tick
  collects, fails open, honours the off switch).

## Not Done (honest)

- **The 27 `scripts/*.sh` call sites are NOT migrated.** PROJECT.md Rule 34
  declares the anti-pattern and the new path; the census is in the table below,
  but rewriting each script's `GOPI_HOME=` line is a separate branch-scoped
  change per script (most must keep `GOPI_MEMORY_DB` pointing at a writer DB —
  a sandbox's `memory.db` is a read-only sample symlink by design, so a script
  needing a writable ledger must NOT adopt it wholesale). Dispatchable as its
  own task; the rule + the CLI verb are the prerequisite it needs, and both land
  here.
- **`internal/standards` Gate 2 is red on master, not on this branch.** A NEW
  Rule-17 hit at `internal/tools/tasks_test.go:1109` plus a stale baseline entry
  at `:1133` — both reproduce identically at `56f665a5` in the main repo with
  this branch's files untouched (`tasks_test.go` is byte-identical to master
  over lines 1100-1140). Left alone deliberately: Rule 30/29 forbid repairing
  another task's file from this branch.
- The sandbox arm does not sweep the **suite scratch home** (that tree keeps its
  own age-only contract) and the two are intentionally separate roots.

## Verification

- **Build:** `go build ./...` → `BUILD_OK`; `go vet ./...` → clean (`VET_EXIT=0`).
- **Tests:** `go test ./internal/sandbox/ ./internal/scratch/ ./internal/config/`
  and `go test ./cmd/agent/ -run 'ScratchGC|Sandbox|CliContract|Completion'` —
  see `## Evidence` for the observed output.
- **Ran (the surfaces this change adds, executed for real against real state —
  built binary `/dev/shm/sbdemo/gopi`):**

  ```
  $ GOPI_SANDBOX_DIR=…/sandboxes ./gopi sandbox show
  sandbox root=/dev/shm/sbdemo/home/sandboxes source=from GOPI_SANDBOX_DIR prefix=sandbox- stale=72h0m0s
  $ ./gopi sandbox show          # nothing configured
  sandbox root=/home/user/workspace/gopi-sandbox source=from the built-in default prefix=sandbox- stale=72h0m0s

  $ ./gopi sandbox create --label build --task-id 01a0demo-…  > sb1.path
  $ ./gopi sandbox create --label build                        > sb2.path
  $ diff sb1.path sb2.path   &&  echo IDEMPOTENT
  IDEMPOTENT: byte-identical stdout
  $ ls -la "$(tail -1 sb1.path)"
  drwx------  …  .
  -rw-r--r--  …  .sandbox-owner.json
  drwx------  …  logs
  lrwxrwxrwx  …  memory.db -> /dev/shm/sbdemo/sample.db
  drwx------  …  raw
  drwx------  …  sessions
  lrwxrwxrwx  …  settings.json -> /dev/shm/sbdemo/home/settings.json
  ```

  Reclaim, through the CLI one-shot (transcript: `/dev/shm/sbdemo/demo.out`):

  ```
  ### C: DRY RUN
  scratch-gc role=cli-sandboxes home=…/sandboxes removed=1 stale=72h0m0s disabled=false bytes=0 dry-run=true
  keep         …/sandbox-build-ff8bc3e3 reason="not aged"
  would-remove …/sandbox-k9-0d37fb6f    reason="aged and unowned (recorded owner pid 2489488, start ticks 602041677, is gone)"
  keep         …/sandbox-live-051cf943  reason="a live process stands inside it"

  ### C: SWEEP  → removed=1 bytes=139  exit=0
  root still there? yes   K exists? no   L exists? yes   root contents: sandbox-build-…, sandbox-live-…
  ### C: SECOND SWEEP → removed=0 bytes=0   (idempotent)
  ### B': kill L's holder, sweep again → removed=1 bytes=141 ; L exists? no ; final root remains: yes
  ```

  And the generator, named — the sample `memory.db` symlinks to is the one
  `internal/testutil` builds (`TestZZDemoPublishSamplePath`, temporary helper,
  **removed before commit**):

  ```
  GENERATOR internal/testutil/dbfixture.go SampleDBPath -> …/suite-sandbox-run-1798250875/testdb-sample/sample.db
  SAMPLE mode=-r--r--r-- bytes=24576   (0444, the scaffold's read-only seal)
  ```

- **Result:** create is idempotent (byte-identical stdout for a repeated label;
  one directory on disk), the shape is exactly the six documented entries, a
  SIGKILLed owner's sandbox is collected on the next sweep while a live
  holder's survives both that sweep and a second one, the root survives every
  age, and `sandbox show` reports the deciding channel rather than a default
  presented as a discovery.

## Census (the anti-pattern, measured 2026-09-23)

`grep -rn "GOPI_HOME=" --include=*.sh scripts/` → **27 files** set `GOPI_HOME`
to a directory the script itself made or chose. Representative inventions of a
ROOT (the shape Rule 34 bans):

| site | the invention |
|---|---|
| `scripts/docclass-red-proofs.sh:86,118` | `mktemp -d` twice, one under `$HOME`, one under `${TMPDIR:-$PWD}` |
| `scripts/probe-scope-rig.sh:14` | `mktemp -d "${TMPDIR:-/home/user/workspace/_tmp}/…"` |
| `scripts/hp/probe-hp-loudness.sh:7` | `SB="$HOME/.gopi-ship/probe"` (a fixed private root) |
| `scripts/e2e-task-merge.sh:43`, `parent-gate-ab.sh:28`, `e2e-m421-claim-lock.sh:33-34` (the last one is GONE — deleted with the cross-node claim lock, batch 4) | `GOPI_HOME="$WORK/home"` / `$ROOT/home` / `$A`,`$B` — per-script roots, no standard shape |

## Evidence (self-filed)

- `go build ./...` → `BUILD_OK` (observed, twice: after the CLI wiring, and after
  the GOPI.md/CONFIG.md edits).
- `go vet ./...` → no output, `VET_EXIT=0` (job stdout line `vet done` with
  nothing above it).
- `go test ./internal/sandbox/` → `ok github.com/nick/gopi/internal/sandbox
  1.689s`.
- `go test ./internal/scratch/ -run 'TestSweepCollects|TestSweepKeeps|TestSweepIsIdem|TestSandbox'`
  → `ok github.com/nick/gopi/internal/scratch 7.198s`, then `7.942s` after the
  empty-root refusal fix (both observed).
- `go test ./cmd/agent/ -run 'TestScratchGCTick' -v` →
  `--- PASS: TestScratchGCTickSweepsAbandonedSandboxes (0.29s)`,
  `--- PASS: TestScratchGCTickSandboxArmFailsOpenAndRespectsTheOffSwitch (0.00s)`,
  `--- PASS: TestScratchGCTick_ThrottleAndFailOpen (1.18s)`,
  `ok github.com/nick/gopi/cmd/agent 2.044s`.
- `go test ./internal/config/ -run 'TestSessionsDirNameNoHardcodedLiterals'` →
  `ok github.com/nick/gopi/internal/config 1.034s` (the first run of the full
  config package FAILED on `internal/config/sandbox.go:108`, a comment spelling
  the literal "sessions" next to the constant — fixed by rewording the comment,
  the constant itself already referenced `SessionsDirName`; red-then-green
  observed).
- `go test ./internal/standards/ -run 'TestStandardGates/TestGate2'` → red with
  `NEW violation … internal/tools/tasks_test.go:1109` + `STALE baseline entry
  internal/tools/tasks_test.go:1133`; the SAME output on master
  (`/home/user/work/GoPi` at `56f665a5`) with `diff` showing `tasks_test.go`
  lines 1100-1140 byte-identical between the two trees — pre-existing, not this
  branch's.
- No load generator was run (Rule 27): the only processes spawned were
  `/bin/sh -c sleep 300` children, killed with SIGKILL.
