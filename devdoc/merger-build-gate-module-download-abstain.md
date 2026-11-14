<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Task Report: the merger's BUILD-gate refusal was a module-download failure, not a branch defect

**Branch**: `fix/mergebuildgate-module-download-abstain` (worktree
`/home/user/workspace/gopi-bgatefix-blocked`, off master `e8afcdfb`)
**Refusing command**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`
**Refused branch (predecessor)**: `fix/task-list-address-no-scan` → archived by the merger
2026-09-15, **no archive tag on this host**, branch ref + scratch worktree already cleaned up.

---

## Completed

### 1. Root cause, reproduced (not inferred)

The gate (`internal/tools/merge_build_gate.go`) builds each revision in a scratch worktree with
**its own per-process caches**:

```
mergeBuildGateScratch()  →  scratch.RuntimeScratchDir(RuntimeClassMergeBuild)
   cacheDir    = <classDir>/gocache
   modCacheDir = <classDir>/gomodcache     ← handed to the build via GOMODCACHE
```

measured `/dev/shm/gopi-test-scratch/queue/queue-runtime-merge-build-*/gomodcache` — **0 bytes**
for every historical instance (one single 410M residue from a build that had partly populated).
A cold `GOMODCACHE` forces `go` to fetch the module zips; on this host there is **no route to the
proxy**:

| probe | result |
|---|---|
| `curl -4 https://proxy.golang.org/.../v1.44.0.info` | timeout (no HTTP code) |
| `curl -6 …` | timeout (no HTTP code) |
| gate build output (recorded) | `go: downloading modernc.org/sqlite v1.44.0` … `dial tcp [2607:f8b0:400a:800::2011]:443: i/o timeout` |

So the refusal text the merger produced ("failed build … downloading modernc.org/sqlite
v1.44.0 / github.com/google/uuid v1.6.0") is **the toolchain's fetch failure**, never a compiler
diagnostic. Control: the same fresh-cache + network command run from a **pristine master** worktree
(`git worktree add … --detach master`) fails identically — **the branch content is not implicated**.
With the operator's warm cache (`GOMODCACHE=/home/user/go/pkg/mod`, the default) master and the
branch both build in **~1.3 s**.

**The gate admits this itself, in its own log** — for dozens of branches:

```
merger: BUILD gate ABSTAINS for feat/peak-hour-guard (branch fails to build, but target master
        does not build either — FAIL) — not branch-introduced, merging unchanged
```

Master *cannot* pass that build in the gate's environment. Which branch gets refused and which gets
abstained is therefore a **coin flip** — the gate's decision is not a property of the branch.

### 2. The gate defect: the decision path ignores the failure's NATURE

`mergeBuildGateBuild` returned `FAIL` for **any** non-zero exit, though its sibling paths already
abstain for an unmeasurable result (timeout → ABSTAIN, worktree-add failure → ABSTAIN, unusable
scratch → ABSTAIN). A build that could not resolve its modules was **never compiled**, so the gate
has no measurement to refuse on. Fixed with one predicate
(`mergeBuildGateUnresolvedModules`) demanding **both halves**:

* a download announcement — `go: downloading`, **and**
* an environment-absence marker — `i/o timeout`, `dial tcp`, `connection refused`, `no such host`,
  `proxyconnect tcp`, `TLS handshake timeout`, `module lookup disabled`, `cannot find module
  providing`, `missing go.sum entry`, `updates to go.mod needed`, `go.mod file not found`,
  `permission denied`, `no space left on device`, `verifying module:`, `unrecognized import path`,

→ **ABSTAIN** with its own reason (`mergeBuildGateUnresolvedModulesErr`, Rule 15b: a gate that
abstains because of its own environment must say so), matched case-insensitively (Go prints
`TLS handshake timeout`). Requiring both halves is what keeps the pre-existing red proof green: a
compiler diagnostic that merely **quotes** network text (`dial tcp` inside a test fixture) stays a
refusal.

### 3. Predecessor status: landed by the merger's own retry (evidence)

Master advanced `6bc0dfeb → e8afcdfb` ("resolve merge **fix/task-list-address-no-scan** into
master") while this task ran. Verified on `e8afcdfb`: every intent file is present and the tree
matches the rework branch byte-for-byte (`git diff fix/task-list-address-no-scan-rework e8afcdfb --
internal/tools/taskledger.go internal/tools/tasks.go` → empty; `devdoc/task-list-address-no-scan.md`,
`taskledger_cost_test.go`, `tasks_session_scan_seam_test.go`, `cli_tasks_list_*_test.go` all present;
`internal/tools/tasks.go` delta = 174/20 = the predecessor's own numstat). The comparator branch
built **exit 0** on `e8afcdfb`. The next merger tick simply got a build that resolved from a cache
that happened to be warm and landed it — the coin flip again, which is exactly what this fix removes.

## Not Done (honest)

* **The complete fix (a warm/shared module cache for the gate) is NOT in this branch.** The real cure
  is for the gate's build to resolve modules from an operator-seeded read-only cache instead of a
  per-process cold one. This branch only stops the resulting failure from being a **false refusal** —
  narrow and mergeable, per the project rule that an incomplete fix must say so and stay narrow.
* **Vendoring was measured and rejected**: `go mod vendor` on this repo produces **233 MB**
  (`modernc.org/sqlite` alone is 808 MB in the module cache). Committing that to history is not an
  option, and copying it per merge tick would be worse than the bug. Recorded so it is not
  re-proposed.
* The `mergeBuildGateEnvAbsenceMarkers` list is a **heuristic over toolchain text**, not a proof. It
  is deliberately paired with the announcement so it cannot fire on a compiler diagnostic; a future
  toolchain rewording would make the gate refuse again — which is the status quo, never worse.
* A duplicate dispatch for the cache half is warranted (Rule 26): see the note at the end.

## Verification

| check | result |
|---|---|
| `gofmt -l internal/tools/merge_build_gate.go` | clean (no output) |
| `go vet ./internal/tools/` | clean |
| Acceptance, verbatim: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | **exit 0** (BRANCH_BUILD_EXIT=0) |
| `go build ./...` | exit 0 |
| `go test -count=1 -run 'TestMergeBuildGateUnresolvedModules_Classification\|TestMergeBuildGate_UnresolvedModulesAbstains\|TestMergeBuildGate_BuildEnvAbstainIsLoggable' ./internal/tools/` | **ok** — 17-arm classification table (incl. 4 negative arms), gate-level abstain arm, reason arm. `TestMergeBuildGate_UnresolvedModulesAbstains` is RED before the fix (it asserted `Refused: true` under the old code path) |
| `go test -count=1 ./internal/tools/ ./cmd/agent/` (branch, live daemon load) | `internal/tools` **ok**; `cmd/agent` FAIL — see the control row |
| same two packages on the pristine control (same tick, no branch content) | `internal/tools` **ok**, `cmd/agent` **ok** on the *old* base `6bc0dfeb`; re-run against current master `e8afcdfb` in job-4 (master moved under this task) |
| Predecessor acceptance re-check (`6bc0dfeb` base worktree) | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **exit 0** |
| Predecessor intent tests (14, targeted) | **14/14 PASS** (internal/tools 12, cmd/agent 2) |

**The `cmd/agent` failure is not attributed to this branch without a control.** The failing package
runs real worker/daemon e2e tests (`TestWorkerReleaseOwnWorktree_Acceptance`, `drain-claim-control-1`,
`gopi task run` children) and its output in the loaded run shows live-daemon interference
(`push_failed=… ssh: Could not resolve hostname peerbox-single-node-test-peer`,
`worker: max rounds (3) reached — exiting`, `zombie sweep … KILLED`) — i.e. it is load/timing
sensitive, and this branch touches **only** `internal/tools/merge_build_gate*.go` (no `cmd/agent`
file, and `internal/tools` compiled green in the same run). The A/B job (job-4) re-runs the identical
command from a checkout of **current master with zero branch content** to settle it.

## Commit

```
2ee43d5c fix(merger build gate): an unresolvable module download ABSTAINS, never refuses a branch
  internal/tools/merge_build_gate.go
  internal/tools/merge_build_gate_unresolved_test.go
```

## Files

* `internal/tools/merge_build_gate.go` — `mergeBuildGateDownloadPrefix`, `mergeBuildGateEnvAbsenceMarkers`,
  `mergeBuildGateUnresolvedModulesErr`, `mergeBuildGateUnresolvedModules`, the FAIL→ABSTAIN branch in
  `mergeBuildGateBuild`.
* `internal/tools/merge_build_gate_unresolved_test.go` — the two red proofs + the logged-reason arm.

## The remaining half (dispatch note, Rule 26)

The gate should resolve modules from a cache it does not own: seed one read-only cache once
(operator/global) and let the gate's `GOMODCACHE` point at it — or, cheaper, let the gate inherit the
caller's `GOMODCACHE` for the **read** path while keeping its own `GOCACHE`. Acceptance for that
task: with `GOPROXY=off` (proxy deliberately unreachable) the gate still returns PASS for a
compiling branch — i.e. no fetch is attempted at all. This branch guarantees that the same condition
can no longer produce a refusal; it does not make the build succeed.
