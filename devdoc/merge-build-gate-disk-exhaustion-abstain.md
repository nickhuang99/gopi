<!-- doc-class: historical -->
> **Retired mechanism (merge pass verify/withdraw deletion, 2026-11-01).** The
> post-pass verification and its withdrawal (`internal/tools/merge_pass_verify.go`:
> `CaptureMergePassScope` / `VerifyMergePass` / `WithdrawMergePass`, plus the
> `merge-withdrawn` notify alarm) were DELETED on 2026-11-01 with the last slice
> of the retired top-level `gopi merge` verb — its last production caller,
> `cmd/agent/merge_run.go`, was already gone. **`gopi task merge` does not verify
> the merged tree and does not withdraw a landing.** Everything below that speaks
> of the post-pass in the present tense is the record of a capability that
> existed, measured on the day each page states; it is not a promise about the
> shipped engine. `man/gopi-merge.md` carries the corrected contract.
# The merge-time build gate refused branches for ENOSPC — an environment failure reported as "does not compile"

**Status:** fixed on branch `task/01a12ff2a6e07f33bc60722ee50ecfb8`; red proof measured in the
same run.
**Filed by:** task `01a12ff2-a6e0-7f33-bc60-722ee50ecfb8` (peerbox-bound — Rule 28: the merge
path is the system layer).
**Host where the class was measured:** `hostb-Laptop`, 2026-10-12 (the evidence run is
task `01a12fbe-1fee-7e6d-b5a6-58b9298c100c`, `devdoc/home-partition-exhaustion-hp.md`).

---

## 1. The defect, as filed

A build that died because the **filesystem refused the write** was classified by the
merge-time build gate as a **compile failure**, so the gate refused the branch as
"does not compile" and the branch was archived.

The measured failing line — one line, and nothing else in the build's output:

```
link: mapping output file failed: no space left on device
```

**Why the existing classifier missed it.** `mergeBuildGateUnresolvedModules` (the gate's
environment-absence classifier, added 2026-09-15) requires **both** halves of a two-part
signature:

1. the toolchain's download announcement (`go: downloading …`), **and**
2. one of `mergeBuildGateEnvAbsenceMarkers` — a list that already carried
   `"no space left on device"`.

A pure **link-stage** ENOSPC prints **no announcement**: nothing was being downloaded. The
marker was present, the announcement was not, so the predicate returned false, the outcome
fell through to `Status=FAIL`, and the gate's refusal arm ran
(`taskMergeRefuseUncompilable` → rework row + `archive/<branch>`).

That is the whole bug: **one class of environment failure was expressed as a conjunction,
and the disk-exhaustion instance of it does not satisfy the first half.**

## 2. The fix

A **separate class** with its own predicate, in the same file and the same abstention
contract as the timeout and the module-resolution abstentions:

| shape | predicate | verdict |
|---|---|---|
| timeout | the command outlived its declared budget | ABSTAIN (pre-existing) |
| module set unresolvable | announcement **AND** absence marker | ABSTAIN (pre-existing) |
| **write refused (ENOSPC / quota)** | **a disk marker AND no compiler diagnostic** | **ABSTAIN (this change)** |
| anything else | — | FAIL (unchanged) |

`mergeBuildGateDiskExhausted` returns the marker it matched, so the abstention can name the
cause instead of asserting one. The whole class needs **no announcement** — the sentence is
the environment's own report — but it needs the **opposite** guard: a repo whose declared
build **is a test suite** (this project's own fixtures included) can print
`no space left on device` inside a **failing test's message** while the compile failed for a
real reason. So the class is claimed only when the output carries **no `file:line:col:`
diagnostic at all** (`mergeBuildGateDiagnosticShape`). An output with neither a diagnostic
nor a marker is still a FAIL — that is the 2026-09-13 incident shape, and
`TestMergeBuildGate_StillRefusesARealCompileError` pins it.

**The reason an abstention must carry.** `mergeBuildGateDiskExhaustedErr` renders:

```
the build died of disk exhaustion ("no space left on device"): the filesystem refused the
write, so the revision was NEVER LINKED and nothing about the branch was measured —
disk state: <path> (device 0x…, 18.0 GiB free); / (device 0x…, 130.4 GiB free)
```

measured verbatim from the gate's own log on the fixture — see §5.

## 3. Both partitions, because one partition reports green while builds die

`mergeBuildGateDiskState` renders **two** figures — the filesystem that holds the path the
build ran in, **and `/`** — through the leaf package the build-cache placement already
measures with (`internal/scratch.FreeBytes` / `DeviceID`; `scratch.RootDeviceProbePath` for
`/`). Zero dependency, zero subprocess, read-only: **one** `statfs` per path (Rule 35 — no
`df` to parse, no external command to require at runtime), and an unmeasurable path is
**printed as a failure inside the string**, never replaced by an assumed figure (Rule 15b).

**Why both, measured.** On this host at the time of the incident:

| path | mount | device | free |
|---|---|---|---|
| the worktree holding every checkout, the Go build cache (119G) and the ambient `TMPDIR` | `/home` (`/dev/nvme0n1p7`) | 66309 | **2.1G, 100% used** |
| `/` (`/dev/nvme0n1p5`) | `/` | 66307 | **131G, 25% used** |

A build died of ENOSPC on the **second** row while the **first** row read "131G free". A
one-partition diagnostic therefore prints a figure that **contradicts** the failure and sends
the reader hunting a code defect. The device id rides the figure for the same reason Rule 22
is judged by device id and never by a spelling: on a layout like this the mount **names** are
the only thing separating the two filesystems.

**Not fixed here, deliberately:** where the build cache lives and how it is bounded is the
**operator's** decision (filed as `01a12ff2-a6a8-7af8-a664-c1c51dc7676c`). This change only
makes the machine *say what happened* when the disk is full.

## 4. The sibling gate

`mergeUnionGateJudgeFailure` carried the identical defect: its leg is
`go test -run '^$' -c ./...`, which **links test binaries**, so a full filesystem kills it at
the link stage with no diagnostic either. It now consults the **same predicate and the same
reason** (Rule 14 — one definition, never a second spelling), routed through the build
gate's `mergeBuildGateDiskExhaustedErr` rather than a union-local sentence.

## 5. Red proof and green control (Rule 31 — measured, not asserted)

The whole fixture is **ENOSPC-shaped OUTPUT injected into the existing gate fixtures** — a
real git repo, a real `worktree add --detach` checkout, a real `sh -c` build subprocess whose
declared contract prints the measured line and exits 1. **Nothing fills a disk, runs `dd`,
`stress` or any load generator (Rule 27).**

**RED — pre-change code, changed trees.** The pre-change `merge_build_gate.go` +
`merge_union_gate.go` are overlaid (`go test -overlay`) onto the changed tree:

```
=== RUN   TestMergeBuildGate_DiskExhaustionAbstains
    merge_build_gate_disk_test.go:117: a branch was REFUSED for a disk the FILESYSTEM could
    not write — the revision was never linked, so nothing about the branch was measured:
    {Refused:true Detail:branch does not compile — link: mapping output file failed: no space
    left on device (branch fix/disk-exhausted-build, target master built fine, master
    unchanged) …}
--- FAIL: TestMergeBuildGate_DiskExhaustionAbstains (0.08s)
FAIL	github.com/nick/gopi/internal/tools	7.664s
```

`Refused:true` on the pre-change code, with the ENOSPC line quoted as the branch's
"compiler diagnostics" — the archive-as-uncompilable path, reproduced.

**GREEN — changed code:** the same six cases pass, including the two controls
(`TestMergeBuildGate_StillRefusesARealCompileError`, whose fixture output carries the
incident's real diagnostics **beside** the ENOSPC line, and
`TestMergeBuildGateUnresolvedModules_Classification`, unchanged).

**Two test files, and the split is load-bearing.** `merge_build_gate_diskclass_test.go`
names the new symbols, so on a pre-change overlay it does not **compile** — that is the red
proof for the predicate's existence. `merge_build_gate_disk_test.go` names no new symbol, so
it runs on **both** revisions and fails as **behaviour** (`Refused:true`) rather than as a
build error. A single file could not do both.

## 6. The test fixture leaked a cache into a sibling's assertion (found at close-out, fixed)

The whole-package run that closed this out came back red on a **pre-existing** regression,
`TestMergeBuildGate_GoCacheLeavesNoScratchCache`, which asserts that no `gocache` / `gomodcache`
leaf exists under the per-run merge-build scratch **class**. The new fixture was the intruder,
and the isolation is worth recording because the reasoning is not obvious from the failure text.

- **Isolated by subtraction, not inferred.** With the two new disk test files moved **out** of
  the package, `go test ./internal/tools/ -run 'TestMergeBuildGate'` is **green**; with them in,
  it is **red** on that sibling. Running the pair in one process reproduces it; the disk test
  alone, or the pair in the other order, does not.
- **Root cause.** `mergeBuildGateScratchModCache()` (`merge_build_gate.go`) does not merely
  *read* a module-cache path — it **materialises** `<class>/gomodcache` via `os.MkdirAll`, and
  it is reached precisely for a repo that is **not** a Go module
  (`mergeBuildGateScratchModCache` is the fallback on that branch). The new fixture called it
  directly to obtain a cache for its declared build. The class directory is `…-<pid>`, i.e.
  shared by **every test in the binary**, so one test's materialisation becomes the next test's
  failure.
- **Why the sibling is right and the fixture was wrong.** The leaf it looks for is the tmpfs
  per-process build cache (726M–1.14G per build) that an earlier change removed; a test that
  writes into that class recreates exactly the thing the regression protects against.
- **Fix.** The fixture takes `t.TempDir()` for its module cache: its declared build is a shell
  script that runs no Go toolchain, so the module cache there is an environment *value*, not a
  measurement. The gate-level tests next door still drive `mergeBuildGate`, which resolves its
  own caches through the real path — the coverage that matters is unchanged.
- **The general lesson, for the next fixture.** A helper that *creates* what it is asked for and
  a helper that *reports where it would be* are different calls; a test that wants the second
  must not borrow the first. When a new test file reddens a sibling that has nothing to do with
  the change, subtract (move the file out) before theorising.

## 7. What this change does NOT do (honest)

- It does **not** free a byte. A build at zero free still fails — it now fails **correctly**:
  the branch is not refused, and the log says "disk exhaustion, the revision was never
  linked, here are both partitions".
- It does **not** bound or relocate the shared `GOCACHE` (119G, measured) — the operator's
  decision, filed as `01a12ff2-a6a8-7af8`.
- The **chained-status** half of the original finding (`A && B ; echo $?` collapsing "B ran
  and was red" and "A failed, B never ran" into the same `1`) is **this run's own operator
  discipline**, documented in `devdoc/home-partition-exhaustion-hp.md` §8 — a shell-caller
  problem, not a gate. What a *gate* can fix is exactly what this change fixes: never call an
  environment failure a branch failure.

## 8. The three remaining legs, and how this branch got the class back (task `01a13032`)

The change above fixed the build gate and the **union** gate. Three more merge-path legs run a
real build and read `err != nil` as a statement about the revision, and each was still
reporting a disk exhaustion as "does not compile":

| leg | file | the output value it reads |
|---|---|---|
| `vetGateExecute` | `internal/tools/rebuild_vet_gate.go` | `vetGateOutput(out.Stdout, out.Stderr)` |
| `mergesetGateExecute` | `internal/tools/rebuild_vet_mergeset.go` | `vetGateOutput(out.Stdout, out.Stderr)` |
| `mergePassRunBuild` | `internal/tools/merge_pass_verify.go` | `res.Output` |

**Each leg reads its OWN output value, and that is the point.** The vet and mergeset legs
capture two streams and combine them with `vetGateOutput`; `mergePassRunBuild` already carries a
combined `res.Output`. A leg abstaining on a *sibling's* text would be an abstention about
something that did not run, so the arms are written against the value each leg actually measured.

**One definition, five call sites (Rule 14).** Nothing was re-classified here: every arm calls
`mergeBuildGateDiskExhausted` and shapes its reason with `mergeBuildGateDiskExhaustedErr`, both
defined in `merge_build_gate.go`, and the marker list (`mergeBuildGateDiskExhaustionMarkers`)
stays the ONE table that already existed. A `grep -c mergeBuildGateDiskExhausted` above zero in
each changed file is the acceptance line, and a second marker table anywhere would be the defect.

### 8.1 The prerequisite had to be re-landed first (measured)

At this task's start the class was **absent from the branch's own ground**: `grep -c
mergeBuildGateDiskExhausted` was `0` in `merge_build_gate.go` and `merge_union_gate.go`, the
devdoc did not exist, and **no ref — local or on `peerbox` — contained `35a24be91`**. The five
commits of task `01a12ff2` survived only as **dangling objects** in the shared object store:
their branch ref had been deleted without a merge, so an observer following branches sees work
that never landed while the objects are still there to recover. They were recovered by a real
merge (`git merge 35a24be91`), which is a true merge because the range's base `8b73f9f4e` is an
ancestor of this branch's HEAD. **Two conflicts, both resolved by keeping both sides** — the
range was written against `8b73f9f4e`, while HEAD carried later work in the same two regions:

- `mergeBuildGateRunLeg`: HEAD had added the PATH pre-flight's `mergeBuildGateCommandWord`
  helper; the range had added the disk arm. Both are kept.
- `devdoc/DOC-INDEX.tsv`: both sides added a row (this devdoc, and `merge-build-gate-test-leg.md`);
  both rows are kept, in sort order.

### 8.2 `task_finish_premerge.go` — the decision, stated

`internal/tools/task_finish_premerge.go` is the **fourth** caller, and it is *not* shaped like the
other three: it reaches its build through `mergeCheckOutput`, which goes through
`mergeBuildGateRunCapped` and therefore has **no `*exec.ExitError`** — it gets an exit code and a
text. The decision is **wire it anyway, and here is why no separate predicate is needed**:
`mergeBuildGateDiskExhausted` takes the output TEXT alone and neither needs nor asks for an exit
error, which is exactly the shape this seam has. So the arm reads the shared predicate on the
check's own `output` and records the shared reason. **There is no second text predicate and no
"here it stays refuse" carve-out** — the seam that lacks an `ExitError` is the seam the shared
predicate already fits, so wiring it costs one line and removes an inconsistency rather than
introducing one. Its position in the ordering is deliberate: the `timedOut`, `exit == 0` and
unresolved-modules verdicts are tested **first**, so a pass is still a pass and the 2026-09-15
resolution class keeps its own reason.

### 8.3 Where the tests inject, and why `mergePassRunBuild` is different

Six tests in `internal/tools/merge_build_gate_disklegs_test.go` — a red arm and a green control
per leg — share one fixture: a shell script that `cat`s a marker file to stderr and exits 1
(**Rule 27: the ENOSPC red proof injects the OUTPUT; nothing fills a disk, runs `dd` or generates
load**). The marker text is the ONE measured spelling, `mbgDiskLinkLine`.

`mergePassRunBuild` cannot be driven through a spec argv — it **spells its own argv**
(`go build -o <scratch dir> ./...`) and ignores `spec.Argv`. The only seam is the toolchain WORD
it resolves through `PATH`, so the test plants a fake `go` that reproduces the failing build's
output (the same `t.Setenv`-of-`PATH` pattern the sibling merge-path fixtures use). That is a
legitimate injection point, not a weakening: the thing under test is the leg's *classification*
of an output, and the output is what the fixture supplies.

### 8.4 Red proof and green control (Rule 31 — measured, not asserted)

The red proof was taken by **removing the wiring** and re-running: with the four arms reverted to
HEAD, all three abstention tests fail with `an ENOSPC-shaped build failure must ABSTAIN, got no
abstention at all`, and the wiring was re-applied from the saved patch. Each green control keeps
the SAME ENOSPC line beside the incident's real compiler diagnostics and asserts the leg is still
red — an abstention there would hide every genuine compile error that happens to share a disk.

The assertion on the marker is deliberately on the kernel's own spelling
(`mergeBuildGateNoSpaceMarker`) rather than the whole stderr text: the reason QUOTES the phrase it
matched, and asserting the raw stream would pin a formatting choice instead of the classification.
