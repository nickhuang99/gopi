<!-- doc-class: historical -->
# Task Report: a fixture's git child never inherits a foreign repository

`GIT_DIR` (once set) beats `cmd.Dir`. A fixture of the class
`exec.Command("git") + cmd.Dir = root + cmd.Env = append(os.Environ(), …)`
therefore talks to whatever repository the SUITE's process was pointed at, not
the one it built. Measured 2026-10-13 (task `01a134c6`, bug row
`01a13513-b8d0`): a shell that had exported
`GIT_DIR=/home/user/work/GoPi/.git` made `internal/docclass`'s fixture
re-initialise the REAL repository and commit 12 synthetic commits onto real
`master` — tree 7678 files → 2, 507,046 deletions — and a sibling worktree was
cut from the wreckage.

## Completed

### 1. One hermetic environment helper, in the already-test-only package

`internal/testutil/gitenv.go`:

- `testutil.GitEnv(extra …)` — `os.Environ()` with the repository-routing names
  **REMOVED, never blanked** (`GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`,
  `GIT_OBJECT_DIRECTORY`, plus `GIT_COMMON_DIR` — the worktree-layout twin of
  the same redirect). The caller's own pins are appended AFTER the strip, so
  pinning the fixture identity cannot lose the strip.
  `GIT_DIR=` is NOT "unset" to git — it is an empty path, and git answers
  `fatal: The empty string is not a valid path` — hence removal, not blanking.
- `testutil.WithoutGitRoutingEnv(env)` — the same strip for a caller that has
  already built a slice (the shape the production seams use). Whole-name match,
  so `GIT_DIRECTIVE=…` survives; over-removing is its own silent defect.
- `testutil.PinSuiteGitEnv(suite)` — the PROCESS-level half, wired into
  `PinSuiteForTestMain` (`internal/testutil/scratchroot.go`), the ONE entry
  point every suite already shares, so **no suite repeats the loop and a new
  TestMain gets it for free**. An operator who exported a routing variable is
  TOLD on stderr that the suite dropped it (Rule 15 — never silent).
- `testutil.AssertGitDirInsideRoot(t, root)` — the premise, asserted:
  `git rev-parse --git-dir` (asked with the routing stripped, so the probe
  cannot rescue a fixture that still inherits one) must resolve **inside** root,
  `Fatal` otherwise, naming root, the resolved directory and the reason.

### 2. The incident's own fixture is hermetic AND asserts its premise

`internal/docclass/anchor_number_provenance_test.go` — the fixture that wrote
the 12 commits. Its git child now gets `testutil.GitEnv(...)` instead of
`append(os.Environ(), …)`, and `testutil.AssertGitDirInsideRoot(t, root)` runs
immediately after `git init`, before a single commit is written.

### 3. A machine gate, its red proof, and a ratchet-down baseline

`internal/standards/git_env_gate.go` + `_test.go`, registered in `gates()`:

- the predicate reports, per outermost function, the line of an inherited
  environment read that a git child in the same function would receive — the
  hit lands ON the `os.Environ()` line and names the function;
- `TestGate9GitEnvRedProof` is the red proof: the incident's own shape is
  reported (one hit, on the right line), while the helper's environment, the
  documented `// git-env-ok:` escape, and a NON-git child are all green;
- `TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly` (renamed 2026-10-16 from
  `TestGate9GitEnvRealTreeScansTestFiles`, a name that read like a real-tree
  red proof) is the COVERAGE arm — it asserts exactly two things: a floor on the
  files read (a scan that walked nothing is not a green gate) and that no
  baseline entry is STALE. It is **not** the red proof, and a NEW unbaselined
  hit does not fail it; for that run the gate record's probe,
  `TestStandardGates/TestGate9GitEnvRedProof` (see `devdoc/standards-gate.md`
  §8);
- the baseline is a **measurement**: **71 pre-existing sites** in the tree at
  this commit, every entry a fixture git call of exactly this class. The ratchet
  is downward (repair a site → delete its entry; a stale entry fails the gate).

## Not Done (honest)

- **The other ~70 baselined sites are NOT fixed in this task.** They are
  enumerated, keyed and ratcheted, not repaired: the deliverable's scope is the
  surface (helper + process pin + gate), and the incident's own fixture. Each
  remaining entry is a one-line repair (`cmd.Env = testutil.GitEnv(...)`), which
  is why the baseline is the honest artefact rather than a hidden one.
- **The gate's predicate keys on the git binary being the string literal
  `"git"`.** A fixture that execs a git CONSTANT (`gitBin`, `docCheckGitExe`,
  …) is not seen by the gate — recorded in the gate's own "what it deliberately
  does not check" block, because "is this identifier a git binary" is not
  decidable by a syntactic gate. Those fixtures are still fixed by the helper,
  and the run-time arm (`AssertGitDirInsideRoot`) is what catches them.
- **Production git seams are out of the gate's scope** (it scans the test
  topology only): a production `exec.Command("git")` with the inherited
  environment is a deliberate routing decision belonging to its own code, not to
  the fixture class this epic is about.
- The `GIT_*` strip does **not** cover every name git reads (e.g.
  `GIT_ALTERNATE_OBJECT_DIRECTORIES`, `GIT_CONFIG_GLOBAL`): the five chosen are
  the ones that ROUTE to another repository or object store, which is the
  hazard class. A fixture that wants the config pins to `/dev/null` itself.

## Verification

- **Build:** `go build ./...` — clean (no output).
- **Tests:** `make test-budget PKGS='./internal/testutil ./internal/docclass
  ./internal/docindex ./internal/standards'` — the repo's declared per-package
  entry point, branch-scoped to the packages this diff touched (its output is in
  `<session>/artifacts/test-budget-touched.txt`).
- **Ran:** the acceptance invocation of this session's worktree (real verb, real
  arguments, real state), and the gate's own tests:
  ```
  cd /home/user/workspace/gopi-01a13513b8d07e9581a87ec1137f6307 \
    && GIT_DIR=/home/user/work/GoPi/.git go test ./internal/docclass/ -count=1
  go test ./internal/standards/ -run 'TestGate9GitEnv' -count=1
  go test ./internal/testutil/ -run 'TestWithoutGitRoutingEnv|TestGitEnv|TestPinSuiteGitEnv|TestCheckGitDirInsideRoot' -count=1
  ```
- **Result:** the acceptance run printed `ok github.com/nick/gopi/internal/docclass
  16.685s`, exit 0, with `GIT_DIR` exported — and the real repository was
  **unchanged**: `git -C /home/user/work/GoPi rev-parse HEAD`
  `795f10044b28b8d29a168cb8c2bdca3319047068` and 7695 tracked files **before**,
  the same object name and the same 7695 files **after** (the full transcript is
  in this run's session dir, `artifacts/acceptance-gitdir.txt`). The fixture's
  commits landed in its own `t.TempDir()` — now asserted in-test by
  `AssertGitDirInsideRoot`, and provable from the run itself: with the assertion
  in place a fixture that reached the real repository would have failed the
  suite instead of reporting `ok`.
- **Red proof of the gate (the Rule 5 HEAD-relative criterion):** the fixture's
  `cmd.Env = testutil.GitEnv(…)` line was restored to the pre-fix
  `append(os.Environ(), …)`, and the enforcing test was run — the actual
  command and its observed output:
  ```
  $ go test ./internal/standards/ -run 'TestStandardGates/TestGate9GitEnvRedProof' -count=1
  --- FAIL: TestStandardGates (1.07s)
      --- FAIL: TestStandardGates/TestGate9GitEnvRedProof (1.07s)
          gate_test.go:116: Rule 13/15 (...): NEW violation (checklist 1b/git-env) at
              internal/docclass/anchor_number_provenance_test.go:67  [anchorNumberProvenanceRepo]
  FAIL
  ```
  The mutation was then restored **from a copy of the fixed file kept outside
  the worktree** (`/tmp/keepfix.go`, not the index), verified byte-identical
  (`cmp /tmp/keepfix.go <path>` → no output), and re-run GREEN
  (`ok github.com/nick/gopi/internal/standards 4.548s`). The transcript is
  `<session>/artifacts/redproof-gate9.txt`. The index-free criterion holds on the
  committed tree as well: `test "$(git hash-object
  internal/docclass/anchor_number_provenance_test.go)" = "$(git rev-parse
  HEAD:internal/docclass/anchor_number_provenance_test.go)"` — the restored file
  IS the committed blob.
- **Commit:** see the branch tip of `task/01a13513-b8d0-7e95-81a8-7ec1137f6307`.

## Evidence (self-filed)

- Acceptance (before == after):
  `git -C /home/user/work/GoPi rev-parse HEAD` → `795f10044b28b8d29a168cb8c2bdca3319047068`
  and `git -C /home/user/work/GoPi ls-tree -r --name-only master | wc -l` → `7695`,
  recorded before AND after the acceptance run; both readings identical.
  Transcript: `<session>/artifacts/acceptance-gitdir.txt`.
- The process pin, observed engaging under the acceptance command
  (`GIT_DIR=/home/user/work/GoPi/.git go test ./internal/docclass/ -run
  TestAnchorNumberProvenanceParserReadsTheClaim -count=1 -v`), verbatim from
  stderr:
  ```
  testutil: suite "docclass": dropped GIT_DIR=/home/user/work/GoPi/.git from this
    process — an inherited routing variable makes a fixture's git child answer about
    SOMEONE ELSE'S repository, which cmd.Dir cannot override (2026-10-13: 12 fixture
    commits landed on real master)
  --- PASS: TestAnchorNumberProvenanceParserReadsTheClaim (0.00s)
  ok  github.com/nick/gopi/internal/docclass  0.244s
  ```
- **`./internal/standards` carries FOUR PRE-EXISTING red tests** that are not
  this branch's and not touched by it:
  `TestCommandsDocGeneratedBlockMatchesSource`,
  `TestConfigDocNameCarryingRefsDeclareTheirSymbol`,
  `TestConfigDocRefsLandOnContent`, `TestConfigDocRefClassCensus` (a stale
  `CONFIG.md` anchor at `cmd/agent/chat_dump.go:170`, and a name-carrying
  reference at `chat_session_slots.go:87`). Proven pre-existing by running the
  same four tests in a DETACHED worktree at this branch's BASE commit
  `5604465ef` — they fail there identically, with this branch's change absent
  entirely (`<session>/artifacts/configdoc-gate-at-base.txt`, exit=1). This
  branch changes none of the surfaces involved (`git diff --stat HEAD~1 HEAD --
  CONFIG.md cmd/agent/cli_ui.go cmd/agent/chat_dump.go
  cmd/agent/chat_session_slots.go cmd/agent/cli_verbs.go` → empty). Filed as a
  bug row at the moment of discovery (Rule 41), with the evidence above:
  **task `01a1355f-13eb-7cbb-9c21-baa6bae0a6b8`** (`--tags bug`). The gate this
  task DELIVERED (`TestGate9GitEnvRedProof`, `TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly`)
  is green on the committed tree, and the file counts measured before and after
  the acceptance run are the same.
- The gate's own counts, from the test's own log line:
  `TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly` logged `126 test files scanned,
  72 hit(s), 71 baseline entries` (one hit — `internal/docclass/…:55` — was
  repaired, hence hits = entries + 1).
- `TestGate9GitEnv` (red proof + coverage arm) — `ok
  github.com/nick/gopi/internal/standards 12.334s`.
- `go test ./internal/testutil/ -run 'TestWithoutGitRoutingEnv|TestGitEnv|TestPinSuiteGitEnv|TestCheckGitDirInsideRoot'`
  — `ok github.com/nick/gopi/internal/testutil 9.420s`.

## MERGE NOTE — read this BEFORE the report above (added 2026-10-14, task/01a13587dffe7bdead8445f5db009313)

The report above is the ORIGINAL run's (branch `task/01a13513-b8d0-7e95-81a8-7ec1137f6307`),
and it is preserved as the record of that run. What changed when the branch was
landed onto today's master, and what a later reader must not be surprised by:

1. **The fixture call site is master's, the mechanism is this branch's.** Both sides
   fixed the same 2026-10-13 incident. Master fixed it inline in
   `internal/docclass` (`anchorNumberGitChildEnv` / `anchorNumberGitCleanEnv` /
   `anchorNumberAssertRepoIsOwnRoot`, plus the two-arm witness
   `anchor_number_fixture_isolation_test.go`); this branch extracted the mechanism
   into `internal/testutil/gitenv.go` and added the tree-wide gate. The merge keeps
   **one** implementation: the inline names are now thin DELEGATES
   (`testutil.GitEnv`, `testutil.WithoutGitRoutingEnv`, `testutil.AssertGitDirInsideRoot`,
   `testutil.GitRoutingEnvNames()`, `testutil.GitDirEnvName`), so the fixture, the
   witness and every future caller resolve to the same strip and the same
   containment assertion. Master's witness test is unchanged in shape and still
   passes — its two arms are the reason the strip cannot silently rot.
2. **The gate baseline above (71 entries) was re-measured to 74.** The 71 was
   measured at this branch's own merge base, 369 commits behind master, so it was
   stale by construction on the merged tree. The reading and its arithmetic are in
   `internal/standards/baseline.go`'s own comment above `gitEnvBaseline` — the
   delta is three master-side fixture sites that arrived with the merged history,
   and one hit (the witness's red arm) that is an inline escape, not a permit.
3. **What is still NOT done** (unchanged from the report above, restated because
   it is the part a reader most often assumes away): the ~74 baselined sites are
   enumerated and ratcheted, not repaired; the gate does not see a git child whose
   binary is a constant rather than the literal `"git"`; production git seams are
   out of its scope.

## Design notes worth carrying forward

- **The fix is at the SURFACE, and it has three parts because the incident had
  three failures**: the child's environment (helper), the process's own
  environment (pin), and the premise nobody checked (assertion). Any one alone
  would have blocked the incident; all three land together so a future fixture
  gets them from one call each.
- **No new persistent dependency (Rule 35)**: no script, no cron line, no git
  shim on `PATH`, no new env knob — the helper rides a package that already
  exists as a test-only dependency, and the process pin rides a `TestMain` entry
  point every suite already calls.
