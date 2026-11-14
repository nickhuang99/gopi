<!-- doc-class: historical -->
# Task Report: one build cache for the gopi test binary in `cmd/agent`

Bug: the gopi-binary build cache was duplicated (found by the test-suite
rigor audit, `devdoc/test-code-discipline-audit.md`, and left open as the
sibling ticket of `devdoc/tmpbin-test-scratch-closeout.md`).

## Root cause

FOUR independent copies of "build the gopi binary once" lived in
`cmd/agent`, all writing the SAME package-level cache:

| site | destination |
| --- | --- |
| `cli_version_test.go` (runGopiForVersionHome) — **declared** `gopiBinOnce`/`gopiBinErr` | `<SuiteTempRoot>/gopi-version-bin-*/gopi` |
| `home_settings_test.go` (runHomeSettingsProbe) | `<SuiteTempRoot>/gopi-home-settings-bin-*/gopi` |
| `settings_nilpanic_test.go` (buildSettingsNilpanicBin) | `<SuiteTempRoot>/gopi-settings-nilpanic-bin-*/gopi-settings-test` |
| `cli_contracts_test.go` (buildCLIContractBin) | `<SuiteTempRoot>/gopi-contract-bin-*/gopi-contract-test` |

(The ticket named three; `cli_contracts_test.go` was a fourth copy — the
audit's count was off by one, the grep is the truth: 4 writers, 1 declaration.)

Each copy had the shape `if gopiBinOnce == "" && gopiBinErr == nil { … }`.
Whichever build function ran FIRST won the cache, so **which binary the other
tests execute was decided by test order** (Go runs tests in
declaration/file order), and the losers silently skipped their own build —
skipping the "fresh build of this package" behaviour they each documented.
A coverage-visible but semantic ambiguity: the loser's test still passed, so
nothing pointed at it.

## Fix (test-only, no production code touched)

One helper file, `cmd/agent/gopibin_test.go`, owning the whole concept:

- the constants (`gopiTestBinEnv = "GOPI_TEST_BIN"`, `gopiBuildDirPrefix`,
  `gopiBinFileName`) — no inline literals (Rules 1a/1b);
- the single declaration of `gopiBinOnce`/`gopiBinErr`;
- `gopiTestBinPath(t)` — the only public entry, cached, skipping cleanly on
  build failure (unchanged skip semantics);
- `resolveGopiTestBin()` — the only producer: `$GOPI_TEST_BIN` when set,
  else `go build -o <scratch>/gopi .` into a per-run `MkdirTemp` dir under
  `testutil.SuiteTempRoot()` (off `/`, removed with the suite's run root).

The four duplicated blocks are deleted; the four call sites now call
`gopiTestBinPath(t)` (`runGopiForVersionHome`, `runHomeSettingsProbe`,
`TestMalformedSettingsNoPanic`, the four `buildCLIContractBin` callers), and
the two `exec.Command(gopiBinOnce, …)` uses read the local `bin` instead.

Behaviour deliberately KEPT (all four copies agreed on it):
- `$GOPI_TEST_BIN` override — the operator can point the suite at a prebuilt;
- build once per test binary, reused by every case;
- a build that cannot be produced SKIPS the test, never fails it;
- `go build .` = a fresh build of this package (NOT the repo's gitignored
  `bin/gopi`, whose staleness is the trap recorded in memory #1258/#1326).

Deliberately NOT taken: switching to `internal/testutil.GopiBin` as the
briefing suggested. `GopiBin` returns the module-root `bin/gopi` (skip when
absent) — using it would have silently replaced "always fresh" with "whatever
prebuilt exists", which is precisely the stale-binary bug those tests were
written to avoid. The seam direction was followed (the env override, the
scratch-root destination), the always-fresh guarantee was kept.

## Not Done

- `merger_singleton_e2e_test.go` (`msingBin`) and `cli_tasks_worker_test.go`
  also read `$GOPI_TEST_BIN` / `testutil.GopiBin` — they build into their own
  per-test destinations and share no package cache, so they are NOT part of
  this duplication. Left untouched (out of scope, no ambiguity to fix).
- `internal/testutil` was not changed: its `run.go` is production-of-test-helper
  code shared by other packages, and no other package needs the always-fresh
  build.

## Verification

- **Acceptance grep** — `grep -rn "gopiBinOnce" --include=*_test.go cmd/agent`
  → 7 lines, all in `gopibin_test.go` — no other file of the package mentions
  the cache: the **declaration** (line 47), exactly **one writer** (line 60,
  the multi-assign of the cache pair), the one read (line 65) and the guard's
  own references. Plus an exhaustive AST guard
  (`TestGopiBinCacheHasSingleOwner`) that fails if ANY other `*.go` in the
  package declares or writes the cache, or if the owner holds more than one
  declaration/more than one write — the grep is a convention, the guard is the
  ratchet (Rule 13). Red-proved: injecting a second writer into
  `cli_version_test.go` made it FAIL naming that file.
- **Dead-code grep** — `buildGopiOnce|buildSettingsNilpanicBin|buildCLIContractBin|versionBuildDirPrefix|homeSettingsBuildDirPrefix`
  → 0 hits repo-wide, so `go vet` is not merely lucky.
- **Build** — `CGO_ENABLED=0 go build ./...`: OK (`BUILD_OK`).
- **Compile/vet** — `go vet ./cmd/agent/`: exit 0; `gofmt -l cmd/agent/`: clean
  (a stash-check proved the three initially-listed files were already
  unformatted before my edits).
- **Tests** — `go test -count=1 ./cmd/agent/`: `ok github.com/nick/gopi/cmd/agent
  59.816s`, `FINAL_AGENT_TEST_EXIT=0` (before/after measured inside the same
  job, so the diff is real and not concurrent noise).
- **/tmp** — the job measured `/tmp | grep -ci gopi` BEFORE the run (243) and
  AFTER it (243), and `ls /tmp/gopi-suite-bin-*` → 0: no new artifact, no
  fixed-path build. The build lands in the suite's pinned run root
  (`/dev/shm/gopi-test-scratch/suite-cmd-agent-run-*/`, /dev/shm, memory-backed)
  exactly like every other suite scratch dir — never on `/`.

## Commit

`fix/agent-test-bincache-single` (worktree `~/workspace/gopi-bincache`).
