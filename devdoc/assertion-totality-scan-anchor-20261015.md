<!-- doc-class: historical -->
# Task Report: the assertion-totality gate scanned a SIBLING run's tree — anchored to the source file, and a vanished file stops being a red

Task `01a13bca-03cc-7b97-a1d7-95c197caedd4` · branch
`task/01a13bca03cc7b97a1d795c197caedd4`

## Completed

- **The scan is anchored to THIS file's own source location, not to the cwd.**
  `auditRepoRoot` now asks `runtime.Caller(auditSourceAnchorFrame)` for the
  compile-time path of `assertion_totality_audit_test.go`, walks up from its
  directory to the first `go.mod` (`auditModuleRootAbove`), and returns that as
  the module root. The old cwd walk survives only as an ANNOUNCED fallback
  (`t.Logf`, Rule 15) for a build whose recorded path does not resolve on disk.
- **A recorded path that names nothing is REFUSED as an anchor**
  (`auditAnchorRootFrom`): `-trimpath` (which the release build passes) records a
  module-relative path such as `github.com/nick/gopi/internal/tools/…`, which
  resolves to no file; a directory is refused too. Both refusals are measured by
  the regression test, so a later "just trust the path" edit cannot silently scan
  a tree nobody can `stat`.
- **A file that vanished mid-walk is COUNTED and REPORTED, never a red.**
  `auditWalkRoot` tolerates `os.IsNotExist` in BOTH places the race can appear —
  the `WalkDir` callback's own `err` (the directory entry disappeared) and the
  `os.ReadFile` (the file disappeared between the listing and the open) — logging
  `%s vanished between the directory read and the open (%v)` with the path and
  the error, and recording it in the scan's own result (`auditScan.vanished`).
  Every OTHER error stays loud: a permission error, an I/O error, a broken
  mount.
- **The walk's ROOT is deliberately NOT covered by that tolerance**
  (`os.IsNotExist(err) && path != base`): "the scan root does not exist" must
  remain an error, or a broken anchor would walk nothing and report an empty tree
  as a clean one. Pinned by its own test.
- **Three regression tests**, none of which needs a sleep, a second process or any
  load (Rule 27):
  - `TestAssertionTotalityAuditScanToleratesAVanishingFile` — the red proof for
    the vanish half, driven on a fixture whose vanish shape is a DANGLING SYMLINK
    (listed by the directory read, impossible to open: the race's observation with
    the race removed). It asserts no error, `read==1`, the live file still
    produced hits, and exactly the dangling path was recorded — then, in the same
    fixture, that an unreadable-by-permission directory (mode `0000`) still
    FAILS (skipped as root, where `0000` denies nothing).
  - `TestAssertionTotalityAuditScanRootComesFromTheSourceTreeNotTheCwd` — the red
    proof for the anchor half: it `t.Chdir`s into a decoy module root named like a
    sibling task worktree, PROVES the trap is live (the cwd contract
    `auditModuleRootAbove` does select the decoy, and the decoy yields a hit, so
    the assertion is not vacuous), and then asserts the production resolver still
    returns this tree.
  - `TestAssertionTotalityAuditScanFailsOnAMissingRoot` — the inverse guard on the
    walk root.

## Not Done

- **The gate is not promoted to `internal/standards/`.** The file's own header
  states the reason and this run kept it (Rule 28): `internal/standards` is the
  system layer (peerbox's), and this task ran on a feature host. The promotion stays
  a follow-up, exactly as the file already records.
- **No new environment knob and no new production code were added.** The tests
  drive the production helpers directly, so the file stays test-only and nothing
  in the repo gains a configurable surface.
- **`go test ./...` was not run whole-tree** — the branch-scoped budget run
  (`./internal/tools`, the only package the diff touches) is the smoke/verify
  path's own shape (Rules 27).

## Verification

- Build: `go build ./...` — OK (exit 0, no output).
- Tests:
  - `make test-budget PKGS='./internal/tools'` — `exit=0 outcome=pass`,
    `executed: 3603 test(s) ran in this run`, `packages: 1 finished, 0 with a
    failure`, `elapsed=666s`, stream
    `…/queue-runtime-job-2524706/run-1792015799-2567335.json`.
  - `go test -count=1 -run 'AssertionTotality' ./internal/tools/` — `ok
    … 7.116s` (and 10.658s / 11.217s on the verbose and earlier runs).
  - `go vet ./internal/tools/` — clean; `gofmt -l` — silent.
- Ran: the surface is a TEST, so the run is the test command itself, before and
  after the change, verbatim:

  ```
  # BEFORE — HEAD's file, plus one probe that cannot be opened
  $ git show HEAD:internal/tools/assertion_totality_audit_test.go > internal/tools/assertion_totality_audit_test.go
  $ ln -s zz_never_created.go internal/tools/testdata/zz_vanished_probe_test.go
  $ go test -count=1 -run 'TestAssertionTotalityAuditGate$' ./internal/tools/
  --- FAIL: TestAssertionTotalityAuditGate (0.85s)
      assertion_totality_audit_test.go:129: scan /home/user/workspace/gopi-01a13bca03cc7b97a1d795c197caedd4/internal:
      open …/internal/tools/testdata/zz_vanished_probe_test.go: no such file or directory
  FAIL    github.com/nick/gopi/internal/tools   5.273s

  # AFTER — the same probe, the same command
  $ go test -count=1 -run 'TestAssertionTotalityAuditGate$' -v ./internal/tools/
      assertion_totality_audit_test.go:640: scan skip: …/testdata/zz_vanished_probe_test.go vanished
      between the directory read and the open (… no such file or directory) — counted as unreadable, not a red
      assertion_totality_audit_test.go:220: scan /home/user/workspace/gopi-01a13bca03cc7b97a1d795c197caedd4:
      1 file(s) vanished mid-walk and were counted as unreadable
  --- PASS: TestAssertionTotalityAuditGate (1.91s)
  ok      github.com/nick/gopi/internal/tools   9.799s
  ```

  (The probe was removed afterwards; `git status --porcelain` is quoted below.)
- Result: the field failure — `os.ReadFile` ENOENT on a file the directory read
  had just listed — is reproduced verbatim with no concurrency at all, and the
  same condition is now a logged skip with the tree still green.

## Evidence (self-filed)

- Red proof, vanish half — mutation: both `if os.IsNotExist(...) { out.skip(...);
  return nil }` arms in `auditWalkRoot` replaced by `return err` (the pre-fix
  shape). `go test -count=1 -run
  'TestAssertionTotalityAuditScanToleratesAVanishingFile' ./internal/tools/` →
  `--- FAIL … a file that VANISHED mid-walk must not fail the scan, got open
  …/probe/zz_dangling_test.go: no such file or directory`, `FAIL … 7.198s`.
- Red proof, anchor half — mutation: `if root, ok := auditRootFromSourceAnchor();
  ok { return root }` replaced by `if false { return "" }` (the cwd behaviour).
  `go test -count=1 -run
  'TestAssertionTotalityAuditScanRootComesFromTheSourceTreeNotTheCwd'
  ./internal/tools/` → `--- FAIL … the scan root came out as
  "/dev/shm/…/gopi-0000000000000000000000000000fake" — want the source tree
  "/home/user/workspace/gopi-01a13bca03cc7b97a1d795c197caedd4"`, `FAIL … 7.508s`.
- Restore criterion — the deliverable blob was restored from a snapshot and
  checked against it, and its relation to HEAD measured explicitly:
  `sha256sum -c …/artifacts/fixed.sha256` → `internal/tools/assertion_totality_audit_test.go:
  OK` (i.e. identical to the pre-mutation snapshot,
  `d0afa456b9b1b6403a20f3b8cedc890b96fa4adfd9861120fd30cea062dbd80a`), plus
  `git diff HEAD --stat -- <path>` → `1 file changed, 485 insertions(+), 27
  deletions(-)` and `git hash-object <path>` ≠ `git rev-parse HEAD:<path>` (the
  fix is present, not a revert to HEAD). Re-green after the restore: `ok …
  10.054s`.
- Worktree cleanliness (Rule 30): `git status --porcelain` after removing the
  probe printed exactly one line — ` M
  internal/tools/assertion_totality_audit_test.go` — the deliverable itself,
  which the commit below lands; the probe (`internal/tools/testdata/`) was deleted
  and is untracked-clean afterwards. NOTE a side effect seen and reverted:
  `make test-budget` appended seven `/go.mod` lines to `go.sum` (the build cache
  sandbox), removed with `git checkout -- go.sum`.
- `-count=1` was used throughout, so no verdict in this report is a cached one.

## Root cause (5-why, for the next reader)

1. Why did the gate go red only when a concurrent run existed? Because it read a
   file that another process was deleting at that instant.
2. Why was it reading another process's file? Because its scan root was another
   CHECKOUT — a sibling task worktree.
3. Why that checkout? Because the root was found by walking up from the cwd: `go
   test ./internal/tools/` runs with cwd `…/internal/tools`, whose parent
   `internal/` has no `go.mod`, so the walk climbed to the REPOSITORY root — which
   in a worktree is a different checkout than the one the binary was built from.
4. Why did a vanished file abort the whole gate? Because the `WalkDir` callback
   returned every error it was handed, and `os.ReadFile`'s ENOENT was one of them.
5. Why was an unreadable file treated as a failed gate? Because "a file I could
   not read" and "an assertion I could not classify" were the same outcome. The
   fix separates them: absence is evidence about the FILESYSTEM and is logged and
   counted; any other read error still fails, and the walk's own root is never
   covered by the tolerance.

## Files

Changed:
- `internal/tools/assertion_totality_audit_test.go` — the anchor
  (`auditRepoRoot` / `auditRootFromSourceAnchor` / `auditAnchorRootFrom` /
  `auditModuleRootAbove`), the vanished-file contract and its evidence struct
  (`auditWalkRoot` / `auditScan` / `auditScan.skip` / `auditScan.logSummary`),
  and the three regression tests with their named fixture constants.

New:
- `devdoc/assertion-totality-scan-anchor-20261015.md` — this report.
