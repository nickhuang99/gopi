<!-- doc-class: historical -->
# Task Report: rework `fix/heavytest-scratch-pin` — the add/add conflict was a duplicate of landed `15671c6f`

Branch `fix/heavytest-scratch-pin-rework` (worktree `~/workspace/gopi-heavytest-pin-rework`),
off master `9e6d7844` (the rework briefing names master tip `52090dc0`; `9e6d7844` is a
descendant of it, so the branch was cut from the newer tip — Rule 23, fresh branch from master).

## Completed

- **Root cause of the add/add conflict (found, not merged around).** The pruned branch
  `fix/heavytest-scratch-pin` (tip `b9653811`, per the attached diff) added
  `internal/heavytest/testmain_scratchroot_test.go` + `devdoc/heavytest-scratch-pin.md`.
  Master had ALREADY landed the same fix independently as `15671c6f`
  ("fix(heavytest): pin the suite scratch root — route (a)"), adding the same
  `internal/heavytest/testmain_scratchroot_test.go` + `devdoc/heavytest-scratch-pin-fresh.md`.
  The two commits are **3 minutes apart by the same author**
  (`f646aace` 01:50:53, `15671c6f` 02:10:53) — one task generated twice (the branch ran, and a
  fresh-suffix twin landed via `fix/heavytest-pin-fresh`). Hence the add/add: no divergent
  content, a **duplicate deliverable**, so nothing on the pruned branch had to be re-applied.
- **Master's copy is the superset.** The `TestMain` is byte-identical in intent
  (`testutil.RunSuiteMain(m, "heavytest")`); master's comment block is the later, stronger
  revision (Rule 22 wording + a `go list -deps` leaf-import proof).
  `git merge-base --is-ancestor 15671c6f master` → exit 0, and `git diff
  master:internal/heavytest/testmain_scratchroot_test.go f646aace:…` shows a comment-prose diff
  only, no functional delta.
- **The one real delta on the branch (devdoc evidence) is already preserved upstream.** The
  branch's unique content was its second commit, `b9653811` (32 lines reworked, +20/-12): the
  `Rule 27` / `go test ./internal/testutil/` **10-minute package-timeout** observation, its root
  cause (`TestRule27NoDeviceLoadGenerators` alone 107s under four concurrent agent suites) and
  the branch-scoped `-timeout 10m -run '…'` re-runs. Master's `devdoc/heavytest-scratch-pin-fresh.md`
  already carries the same conclusion — its own coverage read (`packages: 41 scanned |
  scratch-using: 32 | pinned: 31` → `pinned: 32` after the pin) and the explicit statement that
  the whole-package run is "the off-peak heavy run's job". Dropping it therefore loses no evidence.
- **Consequence for the gate (why this rework is not just a no-op).** At master tip
  `9e6d7844` the Rule-22 pin gate is **RED again** — `TestEveryTempUsingSuiteIsPinned` fails on
  FOUR packages that gained scratch-using tests after `15671c6f`:
  `internal/borrowedfd`, `internal/daemonlifetime`, `internal/docclass`, `internal/removalaudit`
  (stable list: two consecutive runs, no other package). Those carry the real remaining red on
  master, so the rework lands the pins that close it (route (a) — the route the gate's own message
  names, which is this epic's subject):
  - `internal/borrowedfd/borrowedfd_pin_test.go` — `package borrowedfd_test`
  - `internal/daemonlifetime/daemonlifetime_pin_test.go` — `package daemonlifetime_test`
  - `internal/docclass/testmain_scratchroot_test.go` — `package docclass`
  - `internal/removalaudit/testmain_scratchroot_test.go` — `package removalaudit`
  Each is one additive `TestMain` calling `testutil.RunSuiteMain(m, "<pkg path>")`.
- **Why two pins sit in the EXTERNAL test package (measured, not stylistic).** The obvious
  in-package placement for `borrowedfd`/`daemonlifetime` **does not compile**:
  `internal/testutil` imports both of them in production
  (`go list -deps ./internal/testutil` → `internal/borrowedfd`, `internal/daemonlifetime`), so an
  in-package `_test.go` importing `testutil` closes a cycle —
  `import cycle not allowed in test: package …/borrowedfd (test) imports …/testutil imports
  …/borrowedfd from daemonlifetime.go`. The external `_test` package breaks that edge while pinning
  the same test binary (a directory has ONE test binary, so `daemonlifetime`'s pin also covers the
  in-package tests in `validate_hold_fd_internal_test.go`). Two of the four packages have no such
  edge and take the in-package placement.

## Not Done

- Nothing from the briefing was deferred. Explicitly out of scope and **left red on master**
  (Rule 26 — a partial fix is named, not overstated):
  `internal/testutil` `TestEveryTestRunPackageIsImportedBySomeHook` (7 unclassified test packages:
  `chate2e`, `crashdumpe2e`, `docclass`, `removalaudit`, `scratch`, `suiteenv`, `visibility`),
  `TestSuiteMainCallsMatchTheInventory` (the `suite-main.tsv` inventory needs the 4 new rows) and
  `TestSuitePinsMatchTheScratchRootTable` (its expected `pinned` count is a constant,
  `packages: 47 scanned … pinned: 36` now vs the table's stale `32`). These three are the
  inventory/table halves of the same testutil cleanup — one follow-up task, not this epic's scope.
- No cache was used to shorten the runtime of that budget check — per brief it may run long, but I
  ran it for real without flushing; the full result is reported either way.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → exit 0.
- Vet: `CGO_ENABLED=0 go vet ./internal/borrowedfd/ ./internal/daemonlifetime/ ./internal/docclass/ ./internal/removalaudit/` → exit 0.
- Pin gate: `CGO_ENABLED=0 go test -count=1 -run 'TestEveryTempUsingSuiteIsPinned|TestPinCoverageTable|TestEveryTestMainSuiteIsNotifyHermetic' ./internal/testutil/` → `ok … 2.212s`, `packages: 47 scanned | scratch-using: 36 … | pinned: 36` (pinned == scratch-using).
- Touched packages: `go test -count=1 ./internal/borrowedfd/ ./internal/daemonlifetime/ ./internal/docclass/ ./internal/removalaudit/` → all `ok`.
- Commit: see `git log -1` on `fix/heavytest-scratch-pin-rework`.

## Evidence (self-filed)

- **RED before the pins** (fresh worktree at master `9e6d7844`, unmodified):
  `go test -count=1 -run TestEveryTempUsingSuiteIsPinned ./internal/testutil/` → exit 1,
  `FAIL`, with exactly 4 `no TestMain pins the scratch root` messages naming
  `internal/borrowedfd`, `internal/daemonlifetime`, `internal/docclass`, `internal/removalaudit`
  (same 4 on a repeat run — the list is stable, so the red is master's, not flake).
- **NOT-WEAKENED CONTROL** (the green must come from the pins, never a relaxed predicate): the
  four pin files renamed aside (`mv <pin> <pin>.pinbak`) and the gate re-run → the IDENTICAL
  `FAIL` with the same 4 messages; restored → `ok … 3.345s`. The green is produced by the pins.
- **The import cycle is observed, not argued**: `go vet`/`go test` with the pin in `package
  borrowedfd` printed `import cycle not allowed in test` / `FAIL … [setup failed]` for both
  `internal/borrowedfd` and `internal/daemonlifetime`; after moving the files to the external
  `_test` packages, `go vet` exits 0 and both packages test `ok` (`borrowedfd 3.908s`,
  `daemonlifetime 2.015s`).
- **Master already carries the pruned branch's deliverable**:
  `git merge-base --is-ancestor 15671c6f master` → exit 0; `git show --stat 15671c6f` lists
  `devdoc/heavytest-scratch-pin-fresh.md` + `internal/heavytest/testmain_scratchroot_test.go`;
  `grep -c RunSuiteMain internal/heavytest/testmain_scratchroot_test.go` → `1`.
