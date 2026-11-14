<!-- doc-class: normative -->
# Task Report: test-isolation env leak — newTestAgentEnv must pin the knowledge-base path

Task: **01a08975** (branch `fix/test-env-isolation`, worktree
`~/workspace/gopi-envleak`, forked from master `ca27da4`).

Epic: fix the test-isolation env leak where `newTestAgentEnv`
(`internal/agent/loop_test.go`) left `GOPI_MEMORY_DB` / `GOPI_HIPPO_DB` to the
developer's shell, so an ambient export (an e2e leftover pointing at a real
populated DB) was read by `MemoryHotResident()` inside the agent under test and
made `TestHippocampus_DefaultOffLeavesBehaviourUnchanged` fail spuriously.
(`devdoc/hippocampus-repeat-memory-env-leak.md`, memory #1618, branch
`feat/hippocampus-adoption`.)

## Completed

1. **Root cause closed at the helper (the fix).**
   `newTestAgentEnv` now calls a new `isolateMemoryEnv(t, dbPath)`
   (`internal/agent/loop_test.go`):

   ```go
   const (
       memoryDBEnv       = "GOPI_MEMORY_DB"
       memoryDBEnvLegacy = "GOPI_HIPPO_DB"
   )

   func isolateMemoryEnv(t *testing.T, dbPath string) {
       t.Setenv(memoryDBEnv, dbPath)        // pinned INSIDE the temp home
       t.Setenv(memoryDBEnvLegacy, "")      // legacy alias made unreachable
       t.Setenv(hippocampusEnvEnabled, "")  // the gate starts off
   }
   ```

   The helper's contract is now documented as **denylist-clearing, not a
   whitelist**: any env var that steers behaviour (data roots, gates,
   overrides) must be named there, because anything unnamed stays visible to
   the agent. `GOPI_HOME` alone is *not* isolation — `memoryDBPath()` is
   `GOPI_MEMORY_DB > GOPI_HIPPO_DB > GopiDir()/memory.db`, so the overrides
   outrank the isolated home.

2. **The second leak variable is closed by the same call — and the triage that
   found it is worth recording.** Re-reading memory #1618 (the incident's own
   write-up) after the first verification round showed that the ambient shell
   of the *reporting* session had leaked **two** variables:
   `GOPI_HIPPOCAMPUS=1` (the gate) as well as `GOPI_MEMORY_DB`. With the gate
   leaked ON, *both* sides of the equivalence test run with the organ enabled,
   so `on == baseline` and the stripped comparison fail together — a
   *different* failure mode from the hot-digest injection, and `env -u
   GOPI_MEMORY_DB` alone did not clear it. `isolateMemoryEnv` sets
   `hippocampusEnvEnabled` to `""` in the same call, so this epic closes both
   variables at once (the variable-diff recipe — "the key the helper does not
   name is the leak source" — is recorded in memory #1618).

3. **Three regression tests** (`internal/agent/testenv_isolation_test.go`),
   each planting a **POPULATED hot-tier** SQLite fixture (a nonexistent
   ambient path is self-isolating; a populated one is what makes a leak
   observable — exactly the e2e leftover the incident hit):
   - `TestTestEnvIsolation_AmbientMemoryDBDoesNotLeak` — runs a **real
     scripted session** through `runOneScriptedSession` with
     `GOPI_MEMORY_DB=<populated db>` set, and requires the normalized session
     log to carry neither `hotResidentMessagePrefix` nor the fixture's topic.
   - `TestTestEnvIsolation_LegacyDBEnvDoesNotLeak` — same for the legacy
     `GOPI_HIPPO_DB` spelling (the second link of the resolution chain).
   - `TestTestEnvIsolation_ResolvedPathStaysInSandbox` — the **write** side:
     after isolation a real `tools.MemoryStore` lands under the temp home and
     the ambient database keeps its exact row count.

   Each test first proves the fixture **injectable**
   (`tools.MemoryHotResident()` returns the fixture topic with the ambient path
   in force), so the absence assertion can never pass vacuously. That probe is
   also a *behavioural* rename guard: this package's `memoryDBEnv` constant is
   asserted to be the very name `internal/tools` reads at runtime.

4. **The sibling helper is closed too — same contract, other package.**
   `cmd/agent/main_test.go:testAgentEnv` carried the *identical* defect (HOME
   + GOPI_HOME + settings + API key, no DB overrides), so an ambient
   `GOPI_MEMORY_DB` reached `cmd/agent`'s agent-construction tests by the same
   route. It now pins `GOPI_MEMORY_DB` into its temp home and clears
   `GOPI_HIPPO_DB` + `GOPI_HIPPOCAMPUS` through the same three named
   constants, with the denylist contract written into its doc comment. (Its
   only caller is the vision-model recording test — a prompt-independent
   assertion — which is *why* the leak was never noticed there; the fix is
   hygiene against the next caller, not a currently-failing test.)

5. **Integration verification (ambient exports in force) found and fixed two
   more instances of the same class — in the guards themselves.**
   Running the suites with a *real* ambient export
   (`GOPI_MEMORY_DB=/home/user/.gopi/memory.db`, 1572 rows) surfaced:

   - **The new regression file was not hermetic.** A test that plants only
     `GOPI_HIPPO_DB` was hijacked by an ambient `GOPI_MEMORY_DB`, which
     `memoryDBPath()` prefers — the injectability probe then read the *ambient*
     database and the test asserted against the wrong fixture. Every isolation
     test now calls `clearKnowledgeEnv(t)` (clears all three names) **before**
     planting exactly one. A regression test for an env leak must itself be
     immune to the leak it guards.
   - **`cmd/agent/cli_memsync_test.go:TestIsMemsyncSubcommandExport`'s comment
     "hermetic: empty home, empty DB" was false.** `GOPI_MEMORY_DB` outranks
     `GOPI_HOME`, so with the ambient export the export was non-empty (failing
     its `"rows": []` assertion) and with a *foreign* fixture the open failed
     on `no such column: search_kw`. It now pins the DB path inside its temp
     home.

## Not Done (honest)

- **No change to non-test code.** The defect is test infrastructure; production
  reads of `memoryDBPath()` are the intended feature. Nothing was "fixed" in
  `internal/tools`.
- **No package-wide `TestMain` env scrub.** A scrub of *every* `GOPI_*` var was
  considered and rejected: 40 tests in the package deliberately `t.Setenv`
  behaviour knobs, and a blind scrub would silently change what they exercise.
  Targeted pinning of the values that outrank `GOPI_HOME` is the discipline
  that closes this class, and the denylist contract is now written down.
- **Pre-existing `-race` data race left untriaged — reported here, not
  silently dropped (Rule 15b).** `go test ./internal/agent/ -race -run
  TestHippocampus_DefaultOffLeavesBehaviourUnchanged` reports
  `WARNING: DATA RACE` between `hippocampus.go:518` (an appender goroutine,
  reached from `internal/agent/loop.go:2966`) and `hippocampus.go:613/636`. It reproduces on
  the **untouched baseline** (detached worktree at `ca27da4`, `--- FAIL:
  TestHippocampus_DefaultOffLeavesBehaviourUnchanged (13.70s)`, race detected),
  so it is not caused by this change. Filed as bug task
  `01a0898e-8fcf-79f1-a9fd-d20f40e72e45` (`gopi task show`) with the repro and
  the two conflicting stacks; unrelated to the env leak.
  **FIXED** on branch `fix/hippocampus-race2` (tip of that branch): the host's
  in-flight hand-off now goes through locked accessors only — see
  `devdoc/hippocampus-host-data-race.md` for the root cause and the repro test.
- **The regression tests are hermetic themselves**: they plant their own
  ambient fixture, so they are green in a *clean* shell as well
  (`go test ./internal/agent/ -run TestTestEnvIsolation -count=1` → `ok`, 3/3
  PASS) — they do not depend on the developer's env to be interesting.

## Verification

- **Build**: `go build ./...` → OK (`BUILD_OK`).
- **New tests, fixed helper**: `go test ./internal/agent/ -run
  TestTestEnvIsolation -count=1 -v` → **3/3 PASS** (1.28s / 1.81s / 2.31s).
- **New tests, fix REVERTED** (the `isolateMemoryEnv` line commented out) →
  **3/3 FAIL**, with the log line `🧠 hot-resident digest injected (104 bytes)`
  — i.e. the tests are real guards, not decoration:
  `--- FAIL: TestTestEnvIsolation_AmbientMemoryDBDoesNotLeak`,
  `--- FAIL: TestTestEnvIsolation_LegacyDBEnvDoesNotLeak`,
  `--- FAIL: TestTestEnvIsolation_ResolvedPathStaysInSandbox`.
- **The incident's own test, root-cause A/B/C** (fixture =
  `/tmp/ambient-e2e-memory.db`, 3 hot-tier rows):
  - A) `GOPI_MEMORY_DB=<fixture> go test ./internal/agent/ -run
    TestHippocampus_DefaultOffLeavesBehaviourUnchanged` with the fix →
    `ok github.com/nick/gopi/internal/agent 5.200s`.
  - B) same command, fix reverted → `--- FAIL: TestHippocampus_
    DefaultOffLeavesBehaviourUnchanged (2.89s)` with
    `hippocampus_test.go:785: gate-on session differs from gate-off beyond the
    hippocampus artifacts` — **the incident reproduced on demand**.
  - C) `GOPI_HIPPO_DB=<fixture>` (legacy spelling), fix in place →
    `ok github.com/nick/gopi/internal/agent 4.554s`.
- **Regression surface, ambient export present**: the two packages that read
  the memory env were exercised together with
  `GOPI_MEMORY_DB=/tmp/ambient-e2e-memory.db` (result recorded in the commit
  message and re-run below); the ambient value no longer reaches any test in
  `internal/agent`.
- **Integration gate — the whole affected surface WITH the ambient exports the
  incident described** (real `GOPI_MEMORY_DB` = the live 1572-row DB, plus
  `GOPI_HIPPO_DB` and `GOPI_HIPPOCAMPUS=1`):
  - `GOPI_MEMORY_DB=… GOPI_HIPPOCAMPUS=1 go test ./cmd/agent/` → `ok 163.2s`.
  - `GOPI_MEMORY_DB=… GOPI_HIPPO_DB=… GOPI_HIPPOCAMPUS=1 go test
    ./internal/agent/` → `ok 106.7s`.
  - Same shape, the isolation tests alone → PASS; with `isolateMemoryEnv`'s
    scrub reverted → the tests FAIL, so they remain real guards after the
    hermeticity change.
- **Commit**: `2a01bec` (helper + the three tests), `a0551bd` (report +
  sandbox test), `63bd671` (second leak variable), `4de5555` (cmd/agent helper
  + report), `757b332` (make the guards themselves hermetic), on
  `fix/test-env-isolation` (files added one by one, Rule 8).

## Deliverable declaration (Rule 6d)

- **Branch**: `fix/test-env-isolation` (worktree
  `~/workspace/gopi-envleak`, forked from `ca27da4`). The worktree shares the
  main repo's refs — the commit is already on the branch.
- **Recovery**: `git show fix/test-env-isolation:devdoc/test-env-isolation.md`
  and `git log --oneline fix/test-env-isolation`.
- The worktree was **not** deleted; merge and cleanup belong to the PA after
  acceptance (Rule 6e).
