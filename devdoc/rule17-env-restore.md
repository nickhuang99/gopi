<!-- doc-class: historical -->
# Rule 17 residual — the os.Setenv leak, closed and made self-enforcing

Branch: `fix/rule17-env-restore` (worktree `~/workspace/gopi-rule17-residual`)
Origin: the test-suite rigor audit (`devdoc/test-code-discipline-audit.md` §2.1,
ticket `01a08dcd-0401-7cd9-9c69-969876a43276`) — "os.Setenv in tests leaks
process state (49 sites)".

## 1. The briefing's literal acceptance is unsatisfiable, and the honest reading of it is narrower

The ticket asks for `grep -rn "os.Setenv(" --include=*_test.go internal cmd` to
return **zero**. That number was measured on a **pre-sweep tree**, and it cannot
be driven to zero without a regression — the tree already carries the evidence:

- `a73e063` ("style(tests): t.Setenv where possible, a reason everywhere else")
  converted the trivial lone set/restore pairs to `t.Setenv`, and moved the rest
  under `//stylegate:ignore` with a per-site reason.
- **The sweep's own conversion was wrong for three sites** and a follow-up had
  to revert them: `d7a05f3` ("the sweep's own t.Setenv conversion broke per-call
  env flips; add test-setenv-scope rule"). Affected:
  `cmd/agent/dispatch_test.go` and `internal/tools/memory_sync_bidir2_e2e_test.go`.

Why: **`t.Setenv`'s undo scope is the TEST.** A closure that flips an env var
*sper call* — the dispatch sandbox answers each peer, the bidir hook re-points
`GOPI_MEMORY_DB` per peer within one tick — must have the flip undone **when the
call returns**. `t.Setenv` inside such a closure leaves the *last* call's value
installed for the rest of the test. That is not a style question, it is a
correctness one, and the tree now enforces the direction with a gate:
`internal/stylegate` `RuleTestSetenvScope` (`checkSetenvScope`), whose own test
is `TestSetenvScopeIsPerCall`.

Consequence for this task: **the surviving `os.Setenv` sites are not
backlog.** They are the reviewed per-call form. Converting them would re-break
what `d7a05f3` fixed. The honest numbers on the frozen tree: the briefing's grep
(pattern `os.Setenv(`) matches **46 lines** — 13 of them inside the gate/probe
packages, where the calls live inside Go *string literals* the probes feed to
the AST scanner, so they are not real environment writes; the AST count of real
`os.Setenv`/`os.Unsetenv` calls is **107**, of which **7** are inline-escaped and
**100** are permitted by **95** line-keyed allow-list entries (five lines carry
two calls each). The epic's real subject is the residue underneath the
grep:

> the hand-rolled save/restore cannot restore *absence*.

## 2. The defect that was still live

```go
prev := os.Getenv("GOPI_MEMORY_DB")     // "" — also when the var was UNSET
os.Setenv("GOPI_MEMORY_DB", dbPath)
os.Setenv("GOPI_MEMORY_DB", prev)       // leaves it SET TO "" (or to a dead path)
```

`os.Getenv` renders *unset* and *set to ""* identically, so the restore writes
the empty string back and the variable stays **present**. Worse, the value being
undone is typically a `t.TempDir()` path that the framework has already removed
by the time a sibling test reads it — the environment is polluted, not restored.
`os.LookupEnv` can tell the two apart; that asymmetry is the whole bug.

Three sites had this shape **after** the sweep (plus one that was worse: it
omitted a restore entirely):

| site | shape |
| --- | --- |
| `internal/tools/memory_sync_assets_test.go` (sync hook) | `prevDB`/`prevNode` via `os.Getenv`, restored in a `defer` — and `GOPI_SYNC_NODE` was **never restored on the early-return path** (`return nil, fmt.Errorf("no simulated node %q", peer)` returned straight past the `defer`). Both vars point at a `t.TempDir()`. |
| `internal/tools/memory_sync_bidir2_e2e_test.go` (per-peer hook) | `prev := os.Getenv(...)` + immediate write-back `os.Setenv("GOPI_MEMORY_DB", prev)`. |
| `cmd/agent/dispatch_test.go` (two closures) | same `prev := os.Getenv` write-back, in the push leg and the confirm leg. |
| `internal/tools/memory_sync_task_test.go:505` | a bare `_ = os.Setenv("GOPI_MEMORY_DB", dbPath)` whose reason claimed a **child-process handoff** — there is no child in that test, and `taskSyncTestDB` already pinned `GOPI_MEMORY_DB`. Misfiled reason, so it was not even a reviewed exception; deleted. |

## 3. Fix — one reviewed implementation, not a per-site dance

New leaf package `internal/testutil` (`envsnapshot.go`):

```go
defer testutil.SnapshotEnv(t, "GOPI_MEMORY_DB")()   // exact prior state, absence included
os.Setenv("GOPI_MEMORY_DB", nodeLedger)
out, err := somethingThatReadsTheEnv()
```

- `SnapshotEnv` captures with **`os.LookupEnv`** and returns a restore func that
  `os.Unsetenv`s when the variable was absent and `os.Setenv`s the exact prior
  value otherwise. Registered with `defer` — deliberately **not** `t.Cleanup`,
  because the undo must run when the enclosing call returns.
- A restore error is reported via `t.Errorf`, never swallowed (Rule 15b): a
  failed restore silently leaves every sibling test in the binary wrong.
- `envsnapshot_test.go` pins the behaviour at the boundary that matters: the
  probe starts **unset**, flips, restores, and asserts the variable is
  **absent** — the exact case `os.Getenv` cannot express. (`t.Setenv` cannot
  unset, so the two `os.Unsetenv` lines there carry an explicit reason; they are
  the test's *subject*, not a leak.)

The three lossy sites now use it; their `//stylegate:ignore` reasons were
rewritten to say *why* the per-call form is correct here, with
`// env-leak-ok: SnapshotEnv restores on return, absence included` as the
reviewed escape the standards gate reads.

## 4. The machine check — so the shape cannot come back

Gate 2 (Rule 17 / checklist 5.2) previously only saw the *call* `os.Setenv`. It
was blind to the shape: a site could carry a reviewed `// env-leak-ok:` and
still be lossy. Folded into gate 2 (no seventh gate — the `gates()` table and
`TestGOPIRule3_MatchesClaudeToolDefs` stay untouched) is
**`testEnvRestoreShapeFind`**: within one function, a key captured as
`v := os.Getenv("LITERAL")` and later written back with
`os.Setenv("LITERAL", v)` is a hit, escaped by the *existing* `// env-leak-ok:`.

Precision is the deliverable (a noisy gate gets switched off), so the predicate
is deliberately narrow, and each narrowing has a negative probe in
`TestGate2EnvRestoreShapeRedProof`:

1. **literal keys only** — a computed key cannot be matched across call sites
   without dataflow analysis, and a guess would be a false positive;
2. **same-function capture only** — a value captured elsewhere is a parameter, not
   a leaked capture;
3. **non-literal write value only** — a constant per call has nothing to restore
   (and `t.Setenv` is a different, already-correct tool with its own rule);
4. **skipped entirely if `os.LookupEnv` is consulted for that key** — the
   hand-rolled-but-presence-preserving form is not the defect.

The red proof asserts the reported line is the **write-back** (line 8), not the
flip; removing the predicate makes it fail.

**Non-vacuity.** The gate is not green-by-blindness: the three sites in §2 all
existed on the pre-fix revision, and the predicate reports that shape — which is
why they now pass through `testutil.SnapshotEnv` instead of being silently
permitted. `TestGate2EnvReaderFindsTheRealTree`-style emptiness is impossible
here for the same reason: the shape predicate scans the real tree every run.

## 5. Baseline ratchet (down only)

Five now-obsolete entries removed from `testEnvBaseline` (`memory_sync_assets_test.go:98,99,101,102`,
`memory_sync_task_test.go:505`) — 100 → **95**. A stale permit is a silent
permission, so the allow-list may only shrink; a baseline entry that no longer
matches is itself an error, which is how the removal is verified.

## 6. Not done / deliberately rejected

- **Chasing `grep == 0` by converting the 29 reasoned sites.** Rejected with
  evidence: it is the `d7a05f3` regression, and `RuleTestSetenvScope` exists to
  stop exactly that. The report argues this rather than papering over the
  acceptance — the honest reading of the ticket is §2's defect, which is closed.
  Measured on the frozen tree: the grep returns **46**, not 0.
- **`os.Unsetenv` sweep.** ~70 sites exist, overwhelmingly `t.Cleanup(func(){ os.Unsetenv(k) })`
  for deliberately-absent state (that is the *correct* form: `t.Setenv` cannot
  unset, and a cleanup unset is exactly right). Out of this task's scope; the
  gate already covers `os.Unsetenv` as a *call*, and no lossy save/restore shape
  was found among them.
- **`os.Chdir` unchecked restores** (8 sites) — a separate ticket from the same
  audit (ticket `01a08dd0-8b9a-71dd-bc3c-74e22ab26fdf`), untouched here.
- **Applying `t.Parallel`** — no file with a real env site uses it, and
  `t.Setenv`/`t.Parallel` are mutually exclusive by design.

## 7. Verification

Verified on the frozen tree at the landing commit (raw output in §8):

- `CGO_ENABLED=0 go build ./...` → **build: OK**; `gofmt -l internal cmd` → empty
- `go test -count=1 ./internal/agent/ ./internal/tools/ ./cmd/agent/ ./internal/testutil/ ./internal/standards/ ./internal/stylegate/` → **all six ok** (agent 92.7s, tools 276.3s, cmd/agent 55.2s, testutil 2.3s, standards 6.7s, stylegate 5.1s), exit 0
- `go test -run TestStandardGates ./internal/standards/` → **PASS** (all six rules)
- gate 2 data report: `hits=107 baselined=100 escaped=7 NEW=0`, with the 5 removed entries confirmed (`100 → 95` allow-list) and **0 shape-predicate hits on the real tree**
- `go test -run TestGate2 ./internal/standards/` → both `TestGate2TestEnvRedProof` and `TestGate2EnvRestoreShapeRedProof` **PASS**
- `go test -run TestSnapshotEnvRestoresAbsence ./internal/testutil/` → **PASS**
- `grep -rn "os.Setenv(" --include=*_test.go internal cmd | wc -l` → **46** (see §1: the acceptance's zero is unattainable without the `d7a05f3` regression)

## 7b. One defect this task introduced and fixed (a false-positive gate)

The new comment prose in `internal/standards` describing the lossy shape spelled
the literal `t.TempDir()` — and `internal/testutil`'s static scratch-pin gate
(`TestEveryTempUsingSuiteIsPinned`, `TestPinCoverageTable`) matches scratch
markers by **raw text over the test sources**, so `internal/standards` was
reported as a temp-using package with no pin (`NO-PIN`).

Proven, not guessed: a detached worktree at the pre-change tip (`8447c58`) ran
the same test **green**, so the failure was this branch's, and the marker was
found in the comment (`grep -n 't\.TempDir()' internal/standards/*_test.go`
→ one comment line, zero real calls). Fixed by rewording the comment to
"per-test scratch-directory path" — not by adding a spurious `TestMain`, which
would have papered over a false positive. Both gates are green again (§8), and
the incident is recorded in memory: a marker-scanning gate reads comments too.

## 8. Raw verification log

```
### CGO_ENABLED=0 go build ./...
build: OK
### gofmt -l internal cmd
(empty above = clean)
### go test ./internal/agent/ ./internal/tools/ ./cmd/agent/ ./internal/testutil/ ./internal/standards/ ./internal/stylegate/
ok  	github.com/nick/gopi/internal/agent	92.744s
ok  	github.com/nick/gopi/internal/tools	276.325s
ok  	github.com/nick/gopi/cmd/agent	55.193s
ok  	github.com/nick/gopi/internal/testutil	2.296s
ok  	github.com/nick/gopi/internal/standards	6.718s
ok  	github.com/nick/gopi/internal/stylegate	5.075s
PKG_EXIT=0
### all six standards gates (verbose verdicts)
--- PASS: TestStandardGates (2.25s)
PASS
ok  	github.com/nick/gopi/internal/standards	2.253s
GATES_EXIT=0
### gate2 proofs
=== RUN   TestGate2TestEnvRedProof
--- PASS: TestGate2TestEnvRedProof (0.00s)
=== RUN   TestGate2EnvRestoreShapeRedProof
--- PASS: TestGate2EnvRestoreShapeRedProof (0.00s)
ok  	github.com/nick/gopi/internal/standards	0.002s
### SnapshotEnv absence probe
=== RUN   TestSnapshotEnvRestoresAbsence
--- PASS: TestSnapshotEnvRestoresAbsence (0.00s)
ok  	github.com/nick/gopi/internal/testutil	0.017s
### grep os.Setenv in tests
46
### gate data report (Rule 17 accounting)
    gate_test.go:102: Rule 17 (test env isolation) (checklist 5.2): hits=107 baselined=100 escaped=7 NEW=0
    gate_test.go:105:   escape: cmd/agent/dispatch_test.go:246  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:105:   escape: cmd/agent/dispatch_test.go:260  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:105:   escape: internal/testutil/envsnapshot_test.go:21  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:105:   escape: internal/testutil/envsnapshot_test.go:22  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:105:   escape: internal/tools/memory_sync_assets_test.go:106  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:105:   escape: internal/tools/memory_sync_assets_test.go:107  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:105:   escape: internal/tools/memory_sync_bidir2_e2e_test.go:348  [os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv]
    gate_test.go:102: Rule 21 (env-name constant must have a real reader) (checklist 1b/env): hits=0 baselined=0 escaped=0 NEW=0
### scratch-pin gate after the comment fix
go test -run 'TestEveryTempUsingSuiteIsPinned|TestScriptsAreBoundedAndOffTmp|TestPinCoverageTable' -count=1 ./internal/testutil/
ok  	github.com/nick/gopi/internal/testutil	0.417s
### pre-change control (baseline ratchet + false-positive proof)
$ git worktree add --detach /tmp/r17-precheck 8447c58 && cd /tmp/r17-precheck
$ go test -run 'TestEveryTempUsingSuiteIsPinned|TestPinCoverageTable' ./internal/testutil/
ok  	github.com/nick/gopi/internal/testutil	0.386s     # green pre-change → the NO-PIN failure was this branch's
$ (python check of the pre-change allow-list)
pre entries: 100 cur entries: 95
removed: memory_sync_assets_test.go:98,99,101,102  memory_sync_task_test.go:505
added: []
```
