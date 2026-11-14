<!-- doc-class: historical -->
# Task Report: fix the `cmd/agent` test that leaked `out.txt` into the repo

Branch: `fix/test-outtxt-leak` (worktree `/home/user/workspace/gopi-outtxt-leak`)
Base: master `86467b49`

## Completed

- **Root cause measured, not assumed.** `TestTaskRunNormalExitSingleRowE2E`
  drove the `write` tool through a mocked LLM tool call with the *relative*
  path `"out.txt"`. The tool executes in the process that runs the agent loop,
  and `go test ./cmd/agent/` gives the test binary the **package directory** as
  its cwd — so the artifact landed at `cmd/agent/out.txt` in every checkout
  that ran the package. Measured with a probe run: worktree root `out.txt`
  absent, package-dir `cmd/agent/out.txt` present (4 bytes, "done"), and no
  copy anywhere under `~/.gopi` — the write never reached the child's own
  working directory. Master-preexisting: introduced by `0492fdb8` /
  `0f9ec348`, untouched since; the file is **not** gitignored
  (`git check-ignore cmd/agent/out.txt` → exit 1), so it polluted `git status`
  and could be swept into a commit by any `git add` of the package directory.
- **Fix (surgical, 1 file).** `outPath := filepath.Join(work, writeToolOutName)`
  — an absolute path inside the test's own `t.TempDir()` scratch tree — is now
  what the mocked call feeds the tool. The mock is created *after* the temp
  dirs exist, and the test now **asserts the write actually happened**
  (content `"done"` at `outPath`), which nothing did before; a second runtime
  assertion fails if `out.txt` appears in this package's directory at all.
  The tool name comes from `toolnames.ToolWrite` and the argument names from
  named constants (Rule 1/13).
- **Regression guard (new file
  `cmd/agent/cli_tasks_writeleak_guard_test.go`).** Two halves:
  - `TestWriteToolPathsInPackageTestsAreAbsolute` — an exhaustive static scan
    of **every** `*_test.go` in the package (no self-exemption) that fails on
    any mocked tool-call argument map naming the write tool's path argument
    with a relative string literal. It fails loudly if it scanned zero files
    (a gate that reads green blind is not a gate).
  - `TestRelativeWriteToolPathPredicateRedProof` — red-proofs that predicate on
    assembled sources: the historical leak spelling MUST be flagged, and four
    corrected forms (absolute one-liner, absolute multi-line, identifier value,
    unrelated map key) must NOT be. The probe source is built with
    `fmt.Sprintf` rather than typed literally, so the guard's own file stays
    inside the exhaustive scan it enforces.
  - Red-proof on the live tree: planting `{"path": "out.txt"}` in a throwaway
    `zz_redproof_writeleak_test.go` made the gate FAIL naming that file; after
    removing it the gate passed. (Logged, not committed.)

## Not Done

- **No `.gitignore` entry for `out.txt`.** Adding one would *hide* the leak
  rather than remove it, and the artifact is not a legitimate build product —
  the guard makes the defect unrepeatable instead.
- **The wider class was not swept.** The static gate is scoped to `cmd/agent`
  mocked write calls, where the defect was measured. Two other relative-path
  tool-call literals exist in *unit* tests (`internal/testutil/mockllm_test.go:24`
  `"plan.txt"`, `internal/gopilog/formatter_test.go` `"go.mod"`/`"f.go"`), but
  those never execute a real tool against the filesystem (they assert on
  serialization), so they cannot leak. Extending the gate repo-wide would
  require proving that per-site, which is a separate task.
- No e2e/behaviour change: the fix is test-only, no production code touched.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- `gofmt -l cmd/agent/` → empty; `go vet ./cmd/agent/` → clean
- Guard tests: `go test ./cmd/agent/ -run 'TestWriteToolPathsInPackageTestsAreAbsolute|TestRelativeWriteToolPathPredicateRedProof' -count=1` → PASS
- Red-proof: planted leak file → gate FAIL naming the path; removed → PASS
- Reproduced before the fix (worktree, `GOPI_BIN` set):
  `go test ./cmd/agent/ -run TestTaskRunNormalExitSingleRowE2E` → PASS and
  `git status --short` → `?? cmd/agent/out.txt`
- After the fix (same command, `GOPI_BIN` set):
  `go test ./cmd/agent/ -run TestTaskRunNormalExitSingleRowE2E -count=1` →
  PASS, artifact present under the test's scratch dir, `git status --short` → empty
- Package-wide: `go test ./cmd/agent/ -count=1` (21.5s) → `git status --short` shows
  **no `cmd/agent/out.txt`** (before the fix this one command is what produced it).
  The package as a whole is NOT green, for an unrelated pre-existing reason:
  `TestAgentSpawnGuardAllowsOperatorStyleLaunch` fails with
  `inherited worker lock fd 3 is not a usable file: stat worker.lock: bad file
  descriptor: handshake write failed`. Reproduced identically on the untouched
  master worktree `/home/user/workspace/gopi-peak-pristine` (same commit
  `86467b49`), so it is **not** caused by this change and is out of scope here.
- Commit: see the branch tip `fix/test-outtxt-leak`.

## Notes for the reviewer

- The leak was **master-preexisting** — reproduced on a fresh worktree at
  `86467b49` before any edit.
- Two probe scripts (`leakprobe*.sh`, `verify.sh`) and their logs were used for
  measurement and are **not** part of the deliverable; they are removed before
  the final commit.
