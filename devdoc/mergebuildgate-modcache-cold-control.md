<!-- doc-class: historical -->
# Task Report: the modcache cold control observes the RAW toolchain text again

Branch: `fix/mergebuildgate-modcache-cold-control` (fresh worktree
`/home/user/workspace/gopi-modcache-cold-control`, off master `51edd8d9`).

## Completed

- **Diagnosis corrected against the live tree.** The briefing's premise was
  that `go test ./internal/tools/` is RED and that the cold-cache control
  "can never observe the download line". Measured on master `51edd8d9`:
  the control was ALREADY repaired by `358947c6` (an ancestor of master) —
  `TestMergeBuildGate_SharedModCacheResolvesWithProxyOff` PASSES (16.7s). What
  `358947c6` did was re-point the control at the ABSTAIN *reason* and drop the
  toolchain-text assertion, which left the remaining half of the epic standing:
  the observation path still hides the raw output.
- **Item 1 — both observables exposed.** The loss was never in production
  (`mergeBuildGateOutcome` carries `Output` AND `Err`; the abstain path at
  `internal/tools/merge_build_gate.go` `mergeBuildGateBuild` sets both). It was
  the test helper: `mbgmcBuildWithCache` did `return res.Err` on the abstain
  path, discarding `res.Output`. It now returns a named
  `mbgmcBuildObservation{Output, Status, Err}` projected through a named
  function `mbgmcObserve` (the collapsing shape becomes directly testable).
- **Item 2 — the control is non-vacuous again.** The cold-cache arm asserts
  the two observables SEPARATELY: the RAW text
  (`strings.Contains(run.Output, mbgmcDownloadPrefix)`) and then the
  classification (`Status == ABSTAIN` + `Err` carries
  `mergeBuildGateUnresolvedModulesErr`). The `GoCacheRootEnvName` cold-build
  pin from `358947c6` is preserved (without it the shared warm build cache
  answers the compilation before module resolution is reached — the control
  would silently stop exercising its own observable).
- **Item 3 — regression test.**
  `TestMergeBuildGate_ModCacheColdControlKeepsRawOutput` red-proves the
  projection: it feeds `mbgmcObserve` the exact shape `2ee43d5c` produces
  (ABSTAIN + `Err` = the gate's reason + `Output` = a `go: downloading …`
  line) and fails if the raw text is lost, if the classification is lost, or if
  the two observables collapse into one string.
- **Rule 1/13** — no new literals: the asserted text reuses
  `mbgmcDownloadPrefix` (test-local, per the file's merge-collision note) and
  `mergeBuildGateUnresolvedModulesErr`, and the new fixture text is derived
  from `mbgmcDependencyModule` / `mbgmcDependencyVersion` / `mbgmcProxyOff`.

## Not Done (honest)

- The briefing's claim "master is RED on this test" is **not reproducible on
  master `51edd8d9`** — it passes there; the red the epic describes belongs to
  the state before `358947c6` landed. Delivered instead: the remaining,
  genuinely-open half (items 1 + 3), and the control's raw-text assertion
  restored. `gofmt -l internal/tools/` lists one PRE-EXISTING file
  (`task_deliverable_worktree.go`, an extra blank line at ~line 352) which is
  unformatted on master's own tree and untouched by this branch — it is not
  fixed here to keep the diff surgical (Rule 4).
- The two spellings of the download prefix (`mbgmcDownloadPrefix` in the test,
  `mergeBuildGateDownloadPrefix` in production) remain duplicated; the file
  already carries the reason (a same-name collision with the sibling branch),
  so unification is deliberately left to the merge of both halves.

## Verification

- **Reachability of the asserted text** (makes the control non-vacuous, run
  outside the test harness with the fixture's exact shape —
  `module mbgmcreach`, requiring `github.com/google/uuid v1.6.0`, importing it,
  real hashes from this repo's `go.sum`, `GOPROXY=off GOSUMDB=off`):

```
=== (a) COLD GOMODCACHE + GOPROXY=off (expect: go: downloading …, then a resolution failure)
go: downloading github.com/google/uuid v1.6.0
main.go:6:2: module lookup disabled by GOPROXY=off
   [exit=1]
=== (b) WARM GOMODCACHE (operator's cache) + GOPROXY=off (expect: silence, exit 0)
[exit=0, NO download announcement]
```

- **Falsification (the control is not trivially true)** — a harness
  (`/home/user/tmp/mbgmc-falsify.py`) patches the file, runs the test, restores
  and re-checks the sha256. See `## Falsification output` below.
- **Targeted tests on the branch**:

```
=== RUN   TestMergeBuildGate_SharedModCacheResolvesWithProxyOff
[task-notify] merger: BUILD gate shared build cache …/gocache holds 0 B (bound 512 MiB)
--- PASS: TestMergeBuildGate_SharedModCacheResolvesWithProxyOff (10.64s)
=== RUN   TestMergeBuildGate_ModCacheColdControlKeepsRawOutput
--- PASS: TestMergeBuildGate_ModCacheColdControlKeepsRawOutput (0.00s)
PASS
ok  	github.com/nick/gopi/internal/tools	11.303s
```

- **Build + format (branch HEAD, `TMPDIR`/`GOTMPDIR` off `/`)**:

```
=== go build ./... ===
BUILD-OK
=== gofmt -l internal/tools (changed file must be absent) ===
GOFMT-CLEAN(changed file)
```

- **Full package**, `go test ./internal/tools/ -count=1` (background job,
  measured 158.8s — the epic's ~238s estimate was on a colder cache):

```
ok  	github.com/nick/gopi/internal/tools	158.783s
```

Exit code 0, no FAIL lines, no `--- FAIL`.

## Falsification output

- **Phase A** — restore the collapsing projection (`Output := res.Err` on
  ABSTAIN): both the guard test and the cold-cache control must FAIL.
- **Phase B** — seed the "cold" cache from the operator cache (the fetch path
  is never exercised): the control's raw-download assertion must FAIL.
- Harness `/home/user/tmp/mbgmc-falsify.py`; each phase restores the file and
  re-checks its sha256. **RESULT: phaseA_red=True phaseB_red=True
  fixtures_restored=True** (exit 0).

```
=== PHASE A: mbgmcObserve collapses Output to res.Err on ABSTAIN (the 2ee43d5c defect shape)
--- [guard test, broken projection] exit=1
    --- FAIL: TestMergeBuildGate_ModCacheColdControlKeepsRawOutput (0.00s)
        merge_build_gate_modcache_test.go:391: the observation LOST the raw toolchain text:
        Output="the build could not resolve its modules in the gate's own scratch GOMODCACHE
        (no reachable module proxy) — the revision was never compiled" (an abstention must not hide it)
--- [cold control, broken projection] exit=1
    --- FAIL: TestMergeBuildGate_SharedModCacheResolvesWithProxyOff (10.05s)
        merge_build_gate_modcache_test.go:352: the cold-cache build never announced a download
        ("go: downloading ") — this control no longer exercises the fetch path it exists to disprove:
            the build could not resolve its modules in the gate's own scratch GOMODCACHE
            (no reachable module proxy) — the revision was never compiled
PHASE A RED as expected: True
--- restored: sha256 OK
=== PHASE B: the cold arm is seeded warm (the fetch path is never exercised)
--- [cold control, warm cache] exit=1
    --- FAIL: TestMergeBuildGate_SharedModCacheResolvesWithProxyOff (19.27s)
        merge_build_gate_modcache_test.go:342: a COLD module cache built successfully with
        GOPROXY=off — this host has a module source the test assumed away:
PHASE B RED as expected: True
--- restored: sha256 OK
RESULT: phaseA_red=True phaseB_red=True fixtures_restored=True
```

Phase A is the red-proof of the fix (the guard test + the control both go red
when the projection collapses again); Phase B is the red-proof that the
control's raw-text assertion is not trivially true (a warm "cold" arm never
reaches the fetch path, so the assertion fires).
