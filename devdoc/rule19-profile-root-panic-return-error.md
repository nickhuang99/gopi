<!-- doc-class: historical -->
# Task Report: Rule 19 panic in internal/scratch — return the error instead

Task: `01a0f7e3b7967118999f851ba69ca399` · branch `task/01a0f7e3b7967118999f851ba69ca399`

## Completed

- **Root cause (one line)**: `RegisterProfileRootChildren`
  (`internal/scratch/profile_root.go`) is not named `init`/`main`/`Must*`, so its
  `panic(...)` read as a Rule 19 violation. The gate's exemption
  (`panicAllowedFunc`, `internal/standards/gate.go`) is **name-based on the
  ENCLOSING function**; the fact that the only caller sits in an `init` is
  invisible to it. The comment above the panic ("called once, from internal/config's
  own init") is exactly the false comfort the gate refuses.
- **Both FAILs are one root cause, MEASURED not inferred**: the Rule 19 NEW hit makes
  `TestStandardGates/TestGate4PanicRedProof` red, and the same hit makes
  `TestStandardGateDataReport` red at `gate_test.go:132` — that report refuses to
  print a green gate while one hit is neither baselined nor escaped ("this report
  would be lying about a green gate"). The pre-fix run contains exactly these two
  FAILs and nothing else.
- **Fix — option A, the direction the gate itself prints** ("return the error
  instead"): the panic became a returned **named error**
  (`errProfileRootChildUnnamed`), mirroring the sibling seam already in the same file
  (`errProfileRootResolverUnwired`). **No baseline entry was added** — `baseline.go`
  is byte-identical to master.
- **Refusal semantics preserved exactly**: validation runs over the WHOLE batch
  *before* the append, so a refused registration appends nothing and leaves the
  registry exactly as it was — the panic's guarantee, now a returnable value.
- **Callers updated, in a shape Rule 19 exempts**: `internal/config/scratch_trees.go`
  (inside `func init()`) now checks the error and panics **from `init` itself** —
  exempt, because the enclosing function is *named* `init`. A refused registration
  there would leave the `sessions` tree undeclared for the whole process,
  indistinguishable from a tree nobody owns (Rule 15 — loud, never a silent
  unregistered row).
- **Regression test (Rule 10.5)**: `TestRegisterProfileRootChildrenRefusesAnUnnamedRow`
  (`internal/scratch/profile_root_test.go`) pins three properties — the call returns
  an error (it must NOT panic), the error is the named one (`errors.Is`), and the
  registry row count is unchanged after a refused batch.
- **Criterion not widened**: the gate's own probe `TestGate4PanicRedProof` — the test
  that was red — still reports an in-band panic at the enclosing FUNC line and still
  accepts init/main/Must*. Nothing in `internal/standards/` was edited at all.

## Not Done

No deferred item in the fix itself. Two honest limits, stated rather than implied:

- The exemption stays **name-based**, so a helper *named* `MustRegister…` would also
  be exempt while being callable from arbitrary code. That is the gate's existing
  design (its `MustDiv(a, b int)` case pins it); changing it would be a criterion
  change — outside this task's boundary, deliberately not done.
- `internal/standards/baseline.go` was not touched, not even re-keyed: the Rule 19
  list is `[]string{}` by construction, and the constraint is a downward-only ratchet.
- No other Rule 19 site exists: the pre-fix report reads
  `hits=2 baselined=0 escaped=1 NEW=1` — one escaped
  (`internal/agent/crashdump.go:228`, the reviewed re-panic) plus the one fixed here.

## Verification

- Build: `go build ./...` → OK (exit 0, no output).
- Build (declared, Rule 12): `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (`BUILD_OK`).
- Tests (packages the diff touched): `go test ./internal/scratch/ ./internal/config/ -count=1`
  → `ok internal/scratch 109.794s` · `ok internal/config 2.052s` · `EXIT=0`.
- Tests (epic acceptance): `go test ./internal/standards/ -count=1` → green, `EXIT=0`
  (before/after verbatim below).

### BEFORE — `go test ./internal/standards/ -count=1`, pre-fix tree at `ffa81580` (raw, verbatim)

```text
### command: go test ./internal/standards/ -count=1   → captured at /home/user/.gopi/sessions/01a0f7e3b7967118999f851ba69ca399/pre.txt; the working tree was PRE-FIX (the three files reverted, base commit ffa81580)
testutil: suite "standards": scratch root /dev/shm is tmpfs (memory filesystem) (placement memory-base, 6.9 GiB free) — fresh ledgers under it are built on a memory filesystem, so their first opens pay no block device's journal commits
daemon lifetime: armed — read end fd 200 is inherited by every gopi process this one starts; the write end is held by pid 2363142 alone, so any daemon started under it dies with this process (SIGKILL included)
--- FAIL: TestStandardGates (12.41s)
    --- FAIL: TestStandardGates/TestGate4PanicRedProof (0.75s)
        gate_test.go:101: Rule 19 (no panic outside init/main/Must*): 2 total hits, 0 baselined, 1 escaped, 1 new (unannotated), 0 baseline entries
        gate_test.go:113: Rule 19 (no panic outside init/main/Must*): NEW violation (checklist 2.7) at internal/scratch/profile_root.go:206  [panic outside init/main/Must* in RegisterProfileRootChildren — return the error instead (checklist 2.7)]
              fix it, or — if it is a known legacy site — add it to the TestGate4PanicRedProof baseline
              (baseline.go, one-line reason) and keep the ratchet downward.
              run: go test -run TestGate4PanicRedProof ./internal/standards/
--- FAIL: TestStandardGateDataReport (12.23s)
    gate_test.go:126: Rule 16 (%w error wrapping) (checklist 2.2): hits=4 baselined=3 escaped=1 NEW=0
    gate_test.go:129:   escape: internal/tools/worktree_sweep.go:2431  [fmt.Errorf renders the wrapped error with %v — errors.Is/As lose the chain; use %w]
    gate_test.go:126: Rule 17 (test env isolation) (checklist 5.2): hits=122 baselined=85 escaped=37 NEW=0
    gate_test.go:129:   escape: cmd/agent/cli_checkpoint_root_channel_test.go:234  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: cmd/agent/cli_checkpoint_root_channel_test.go:248  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: cmd/agent/cli_tasks_list_order_test.go:236  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: cmd/agent/cli_voice_review_test.go:1625  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: cmd/agent/dispatch_test.go:260  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: cmd/agent/dispatch_test.go:274  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: cmd/agent/merge_lock_test.go:207  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: cmd/agent/task_ledger_agreement_redproof_test.go:219  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: cmd/agent/task_run_release_coverage_test.go:136  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: cmd/agent/testmain_test.go:166  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: cmd/agent/tmpdir_settings_value_test.go:146  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: cmd/agent/worktree_release_accept_test.go:234  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/agent/journalreport_test.go:127  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/agent/testmain_isolation_test.go:86  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/agent/testmain_isolation_test.go:87  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/agent/testmain_isolation_test.go:88  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/agent/testmain_isolation_test.go:89  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/lineedit/palette_test.go:106  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/lineedit/palette_test.go:114  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/shot/launch_test.go:101  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/testutil/daemonlifetime_test.go:101  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/testutil/envsnapshot_test.go:21  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/testutil/envsnapshot_test.go:22  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/testutil/tmproot_assert_test.go:152  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/testutil/tmproot_assert_test.go:178  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/testutil/tmproot_assert_test.go:353  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/testutil/tmproot_assert_test.go:357  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/tools/memory_sync_assets_test.go:106  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/tools/memory_sync_assets_test.go:107  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/tools/memory_sync_bidir2_e2e_test.go:352  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/tools/run_ground_anchor_test.go:142  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/tools/task_submit_forward_test.go:120  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/tools/task_submit_forward_test.go:379  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/tools/task_submit_forward_test.go:427  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/tools/task_submit_forward_test.go:94  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/tools/tasks_test.go:1182  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:129:   escape: internal/tools/testmain_test.go:66  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:126: Rule 18 (no else after terminating if) (checklist 4.3): hits=2 baselined=0 escaped=2 NEW=0
    gate_test.go:129:   escape: internal/lineedit/editor.go:1550  [else after a terminating if-body — drop the else and de-indent (early return)]
    gate_test.go:129:   escape: internal/tools/memory.go:3392  [else after a terminating if-body — drop the else and de-indent (early return)]
    gate_test.go:126: Rule 19 (no panic outside init/main/Must*) (checklist 2.7): hits=2 baselined=0 escaped=1 NEW=1
    gate_test.go:129:   escape: internal/agent/crashdump.go:228  [panic outside init/main/Must* in recoverRunPanic — return the error instead (checklist 2.7)]
    gate_test.go:132: Rule 19 (no panic outside init/main/Must*): 1 hit(s) are neither baselined nor escaped — TestStandardGates fails them, so this report would be lying about a green gate
    gate_test.go:126: Rule 21 (env-name constant must have a real reader) (checklist 1b/env): hits=0 baselined=0 escaped=0 NEW=0
    gate_test.go:126: Rule 20 (no get/is/handle prefix on an accessor) (checklist 1.4): hits=1 baselined=0 escaped=1 NEW=0
    gate_test.go:129:   escape: internal/shot/x11.go:950  [getRootImage — drop the get prefix (accessor) or take an argument (command)]
    gate_test.go:126: Rule 13 (no hardcoded worktree root literal) (checklist 1b/worktree-root): hits=1 baselined=0 escaped=1 NEW=0
    gate_test.go:129:   escape: internal/shot/x11.go:193  ["/tmp/.X11-unix": hardcodes a worktree/fixture path under the ambient temp root — reference worktreeroots.AmbientTempRoot / worktreeroots.FixtureBannedRoot (or build the path from the scratch tree) instead: a temp workspace must never be chosen by a bare literal (Rule 22)]
    gate_test.go:126: Rule 13/14 (one production home per scratch env name) (checklist 1b/scratch-env): hits=0 baselined=0 escaped=0 NEW=0
    gate_test.go:126: Rule 16-21 family (no unresolved conflict marker in a text file) (checklist gate-family/conflict-marker): hits=0 baselined=0 escaped=0 NEW=0
    gate_test.go:126: branch-creation force forms (no destructive spelling in a shipped instruction) (checklist branch-form/destructive): hits=0 baselined=0 escaped=0 NEW=0
FAIL
FAIL	github.com/nick/gopi/internal/standards	59.208s
FAIL
EXIT=1
```

The criterion arithmetic and the shared-cause line of that same run, re-quoted
(verbatim, for the reader scanning for Rule 19):

```text
4:--- FAIL: TestStandardGates (12.41s)
7:        gate_test.go:113: Rule 19 (no panic outside init/main/Must*): NEW violation (checklist 2.7) at internal/scratch/profile_root.go:206  [panic outside init/main/Must* in RegisterProfileRootChildren — return the error instead (checklist 2.7)]
11:--- FAIL: TestStandardGateDataReport (12.23s)
55:    gate_test.go:126: Rule 19 (no panic outside init/main/Must*) (checklist 2.7): hits=2 baselined=0 escaped=1 NEW=1
57:    gate_test.go:132: Rule 19 (no panic outside init/main/Must*): 1 hit(s) are neither baselined nor escaped — TestStandardGates fails them, so this report would be lying about a green gate
66:FAIL
67:FAIL	github.com/nick/gopi/internal/standards	59.208s
68:FAIL
69:EXIT=1
```

### AFTER — `go test ./internal/standards/ -count=1`, this tree (raw, verbatim, complete)

```text
### command: go test ./internal/standards/ -count=1   (workspace POST-FIX)
ok  	github.com/nick/gopi/internal/standards	35.062s
EXIT=0
```

### AFTER (again) — the same command on the COMMITTED tree, tip `efc5baba`, `git status --porcelain` empty (raw, verbatim)

The devdoc and its `DOC-INDEX.tsv` row landed after the after-run above, so the acceptance command was re-run on exactly what the branch ships:

```text
### command: go test ./internal/standards/ -count=1   (committed tree, tip efc5baba, worktree clean)
ok  	github.com/nick/gopi/internal/standards	33.186s
EXIT=0
```

### AFTER (final) — the same command on the FROZEN tree (branch tip `e3904750`, `git status --porcelain` empty)

This is the tree the branch ships, measured after the last commit that touches any Go
file: the devdoc + its `DOC-INDEX.tsv` row landed after the after-run above, so the
acceptance command was re-run against exactly what is committed.

```text
### go test ./internal/standards/ -count=1   (branch tip e3904750, git status --porcelain empty — this is the exact tree the branch ships)
ok  	github.com/nick/gopi/internal/standards	42.962s
EXIT=0```

### One transient red during close-out (recorded, then fixed)

An intermediate run of that SAME command failed on
`TestDocClassBannerMatchesManifest`: the first `gopi doccheck --fix` wrote the class
banner for `devdoc/rule19-profile-root-panic-return-error.md`, and a later amend
re-copied the session copy of the report over the classified file, dropping it (class
`historical` requires `<!-- doc-class: historical -->` as the file's first line).
Restored, and the run quoted above is the measurement AFTER that fix. Recorded because
a run that has only ever printed green has not been shown to catch anything — and
because it separates two faces honestly: `gopi doccheck` said "the corpus is
consistent" both times (it reads the row/manifest), while the BANNER is enforced by
the standards gate, not by the verb.

### Criteria this branch does NOT touch

`git diff master --stat -- internal/standards/` prints nothing — no gate file, no
probe, no `baseline.go`. The Rule 19 criterion is unchanged; it is satisfied by the
code now, which is the direction the criterion itself prescribed.

### The surfaces actually executed (Rule 31)

```
$ go test ./internal/standards/ -count=1 -run TestGate4PanicRedProof -v
--- PASS: TestGate4PanicRedProof (0.00s)
ok  	github.com/nick/gopi/internal/standards	0.582s          EXIT=0

$ go test ./internal/scratch/ -count=1 -run 'TestRegisterProfileRootChildrenRefusesAnUnnamedRow|TestProfileRootChildrenCoverRepoWriters' -v
--- PASS: TestRegisterProfileRootChildrenRefusesAnUnnamedRow (0.00s)
--- PASS: TestProfileRootChildrenCoverRepoWriters (0.03s)
ok  	github.com/nick/gopi/internal/scratch	0.639s          EXIT=0

$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && ./bin/gopi scratch-gc profile
BUILD_OK
...
scratch-gc role=cli-profile home=/home/user/.gopi removed=0 stale=6h0m0s disabled=false children=90 declared=25 undeclared=65 dry-run=true

$ ./bin/gopi doccheck
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
```

Result of the CLI run: the changed registration path is exercised **for real** — the
`sessions` child, whose row is the one that crosses the package boundary through
`RegisterProfileRootChildren` from `internal/config`'s `init`, is **declared**:

```text
$ ./bin/gopi scratch-gc profile --explain | grep 'name=sessions'
profile-child class=state name=sessions removable=false reason=the session tree (the ONE official home of a run's evidence): the JSONL history is not bounded here, its `*.raw.log` files are (SessionRawLogStaleAge); never a candidate
```

Had the refactor broken the wiring (e.g. the row silently dropped instead of reaching
the registry), `sessions` would have fallen into `undeclared` and
`TestProfileRootSessionsRowIsWiredFromConfig` + this run would both say so. The exit
status of the CLI is 0 and it reports `declared=25 undeclared=65 removable=0` — the
pre-existing state of this host's profile root, unrelated to this change
(Rule 30: no sweep was asked for; `dry-run=true`, `removed=0`).

- Commit: `e3904750` (`fix(standards): Rule 19 — return the error from
  RegisterProfileRootChildren instead of panicking`) + `02269138` (the doc commit);
  the branch is `task/01a0f7e3b7967118999f851ba69ca399`.

### Acceptance re-run AFTER the delivery-point sync (the tree that will be merged)

The frozen-tip run above (HEAD `02269138`) was made before the branch was aligned
with master, so it is evidence about a tree that no longer exists. Re-run at HEAD
`a77b490e` (master `3458a3b8` merged in — sync exit 0, no conflict, no file of this
change touched):

```text
$ git merge --no-edit master          # 9 files changed, 881 insertions(+), 83 deletions(-)
$ echo $?                             # 0
$ git rev-parse --short HEAD          # a77b490e
$ go test ./internal/standards/ -count=1
ok  	github.com/nick/gopi/internal/standards	35.238s     EXIT=0
$ git status --porcelain              # (prints nothing)
```

`internal/standards/` is still untouched on the synced tip: `git diff master --
internal/standards/` prints nothing, and `git show master:internal/scratch/profile_root.go`
still carries the old `panic("scratch: RegisterProfileRootChildren called with an
empty Name")` at line 211 while this branch does not — the fix is the diff.

## Evidence (self-filed)

- `go build ./...` → exit 0, no output (observed in the same shell as `go vet`, which
  also printed nothing for `./internal/scratch/ ./internal/config/`).
- `go test ./internal/standards/ -count=1` BEFORE → `FAIL github.com/nick/gopi/internal/standards 59.208s`, `EXIT=1`.
- `go test ./internal/standards/ -count=1` AFTER → `ok github.com/nick/gopi/internal/standards 35.062s`, `EXIT=0`.
- Rule 19 line, BEFORE (verbatim, from the run): `Rule 19 (no panic outside init/main/Must*) (checklist 2.7): hits=2 baselined=0 escaped=1 NEW=1` and `NEW violation ... at internal/scratch/profile_root.go:206`.
- Rule 19 line, AFTER: absent from the report entirely — the run is `ok`, three lines total.
- `git status --porcelain` at hand-over prints nothing (Rule 30) — checked in the
  closing command of this task, not asserted from memory.
- `git diff --stat` (the exact scope, 3 files, all inside the allowed boundary):
  `internal/config/scratch_trees.go | 11 ++++++++--`,
  `internal/scratch/profile_root.go | 21 +++++++++++++----`,
  `internal/scratch/profile_root_test.go | 39 ++++++++++++++++++++++++++++++-----`.
- `git diff master -- internal/standards/` → empty: the criterion files are untouched.
