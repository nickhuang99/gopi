<!-- doc-class: historical -->
# The /dev/shm role split: recognise-and-clean survives, create is retired

Branch `fix/scratch-shm-role-split` (worktree `/home/user/workspace/gopi-shm-role`),
based on master `45c6ecc1` (which contains the setup half, `524156af`).

Operator ruling this epic implements (2026-09-18, verbatim):

> "just for a few ms performance? no, no no! don't use tmpfs. change settings to
> workspace. now"

The setup half made the scratch home of record a REAL DIRECTORY on the block
device (`/home/user/workspace/gopi-test-scratch`, device **2052** = /dev/sda4,
`/` is **2051**). This is the code half.

---

## 1. Every code site that treated /dev/shm as the memory base

Verified against master before each edit. "Recognise" = the site must keep
working on a legacy artifact; "create" = a site that would place new data there.

| # | site | what it was | decision / change |
|---|---|---|---|
| 1 | `internal/worktreeroots/roots.go:57-63` (`ShmDir`) | "a tmpfs … legal ONLY as the SCRATCH role" | **role split, kept**. The doc now states the single surviving role: RECOGNISE AND CLEAN a legacy artifact; CREATE is retired and no refusal may prescribe it. Consumers: `internal/scratch.DefaultBasePath`, `internal/testutil.ShmDir`, `internal/testutil.MemoryBaseDefault` — all readers/deleters, none creates. Value unchanged (`/dev/shm`), so the legacy sweep still works. |
| 2 | `internal/worktreeroots/roots.go` `RoleScratch`, `RoleMemoryBase` | RoleScratch doc: "the ONLY role /dev/shm may carry"; RoleMemoryBase doc assumed the home still resolves onto a tmpfs | **doc corrected, behaviour unchanged** (no code branches on either doc). RoleMemoryBase now also says it is EMPTY on a block-backed host, which is the intended state. |
| 3 | `internal/standards/worktree_root_gate.go` (was `^/dev/shm/.`) | forbids a hardcoded `/dev/shm/<name>` literal outside `internal/worktreeroots/` | **KEEP, not loosened** (see §3). Merged with the footprint half: the anchor is now `^/dev/shm(/|$)` (the bare root counts too) and the gate also scans `_test.go` — a STRICTLY WIDER net than the one this branch started from, so "the gate is not a no-op" holds a fortiori. The refusal text carries the new meaning via a new constant `worktreeRootRetiredClause`, and the red-proof asserts that clause (a gate whose contract changed meaning must say so where it fires). |
| 4 | `internal/scratch/tmproot.go` `scratchHomeMountHint` | printed `mkdir -p X && mount -t tmpfs -o size=4G,mode=0755 tmpfs X` as the FIX in the temp-root refusal | **renamed + rewritten**: `scratchHomeMakeHint()` returns `mkdir -p <home>` only. The retired route is named in the doc as retired, so the history stays and the ADVICE is gone. |
| 5 | `internal/testutil/scratchroot.go:571` `assertScratchPathSafe` | its FIX sentence also printed the `mount -t tmpfs` command | **rewritten** to `mkdir -p <home>` (through `ScratchHomeMountHint`, Rule 14: one spelling of the fix). |
| 6 | `internal/testutil/scratchroot.go` contract block B | "BACKING — the scratch home is a tmpfs … that is what makes the WAL-close checkpoint fsync free" | **rewritten**: BACKING is now the block-device directory, with the measured price (601s vs 237s `internal/tools`; 411ms vs 16.8ms per fresh ledger DB) recorded as the cost the operator accepted. What stays enforced is off-`/`. |
| 7 | `internal/testutil/scratchroot.go:83` `ShmDir`, `legacy_residue.go:69` `MemoryBaseDefault` | aliases of `worktreeroots.ShmDir` | **kept** (Rule 14 aliases). Their docs now say they are the legacy sweep's target, not a scratch target. |
| 8 | `internal/scratch/base_residue.go:76` `DefaultBasePath` | "the base when nothing overrides it: … a tmpfs, so nothing is fsynced" | **kept, doc corrected**: it is the location RECOGNISED as the legacy memory-base residue namespace. Nothing in this package creates it (`BaseResiduePlan` reports an absent base as EMPTY; `SweepBase` is a no-op when it is missing). |
| 9 | `internal/scratch/base_residue.go` package doc | "the memory base is a tmpfs **by definition**" | **corrected**: a base on `/` is refused by measured device id; since the ruling the check is the device one, "and never a requirement that the base still be RAM". |
| 10 | `internal/scratch/rootgocache.go:59,113-124` | `goCacheOnRootReasonFmt` ("the tmpfs alternative is the 2026-09-15 one") and `AssertGoCacheOffShm` doc ("the suite's own scratch root happens to BE under /dev/shm on this host, which is legitimate") | **incident KEPT, false premise corrected**. The 726M-1.14G / 100% measurement stays verbatim; the doc no longer asserts the suite's scratch root is legitimately under /dev/shm. The refusal itself is UNCHANGED (it refuses ANY memory filesystem — the correct direction, so it must not be relaxed). |
| 11 | `cmd/agent/cli_tasks.go:310` help | "Stale-worktree-registration sweep (`/dev/shm assessment`, …)" | **rewritten** to "the 2026-09-13 worktree-residue assessment": the sweep is not about the retired location, and a help string that frames it by one is advice by implication. |
| 12 | `internal/tools/testsuite_temproot_test.go` | `TestSuiteScratchRootIsMemoryBacked` FAILED any scratch root that is not memory-backed; `freshDBStallAttribution` told the reader to "Pin the suite scratch root to /dev/shm"; the ratio arm's failure text named the same remedy | **repaired, see §4.** |
| 13 | `internal/agent/testmain_isolation_test.go` | `TestSuiteTempRootIsPinned` FAILED when the pinned root was not memory-backed; `TestMain`'s comment said the pin exists to reach a memory filesystem | **repaired, see §4.** |
| 14 | 55 comment lines naming `/dev/shm` (gc.go, holder.go, limits.go, scratch_gc_tick.go, worktree_sweep*.go, path_policy.go, task_deliverable_worktree.go, config.go, daemoncwd.go, daemonlifetime.go, taskverbs, orphansweep, merge_build_gate_gocache.go …) | history of the measured incidents | **UNCHANGED** (deliberate; the brief forbids churn for its own sake). Each records an incident that is still true. |

Nothing in the tree CREATES under `/dev/shm` any more: the scratch home
(`internal/scratch.HomeDir`) is `<home>/workspace/gopi-test-scratch`, and the
suite pin (`testutil.SuiteTempRootDir`) is the same directory. `HomeDirEnvName`
(`GOPI_TEST_SCRATCH_HOME`) is the only override, and it is a home, not a base.

## 2. The recognise-vs-create distinction, as code

- **CREATE is retired.** No production path builds a path under `ShmDir`.
  The one refusal that used to prescribe a tmpfs mount (site 4) no longer does,
  and the two suite assertions that required memory backing (sites 12-13) no
  longer do.
- **RECOGNISE AND CLEAN is still required, and is why `ShmDir` survives.**
  `internal/testutil.MemoryBase()` → `internal/scratch.SweepBase` /
  `GCLegacyScratchResidue` collect this system's own namespace there
  (`gopi-suite-tmp-*`, `gopi-<family>-<token>`, `gocache-*`, `probe-*`), and
  `internal/tools`' memory-base root still derives a root for a legacy
  registration found in that base. Deleting the constant would orphan the
  measured 2.6 GB / 702 entries.
- The distinction is asserted, not just documented: §5 shows a planted legacy
  artifact being collected on BOTH the base arm and the suite-pin arm.

## 3. The standards gate decision

`internal/standards/worktree_root_gate.go` pattern `^/dev/shm/.` **FORBIDS** a
hardcoded path below `/dev/shm` outside `internal/worktreeroots/`. It is not a
no-op and it was not loosened:

- It already forbade the shape as a WORKTREE root. Since the ruling the same
  shape is also out of policy as a SCRATCH location, so the gate now guards a
  RETIRED location rather than a discouraged one — strictly more correct than
  before, and a regression that re-introduces `/dev/shm/…` scratch is exactly
  what it catches.
- The only change inside the predicate is the **reason string**: a new constant
  `worktreeRootRetiredClause` (Rule 14, one definition) states that the location
  is RETIRED for creation while recognition/cleanup stays required. The
  red-proof now fails if that clause disappears from the reported reason, so the
  gate cannot be silently re-pointed while keeping its id.
- Baseline is still EMPTY by construction, and the real-tree test still passes
  (`hits=2 baselined=0 escaped=1 NEW=0` — the escaped hit is `internal/shot`'s
  X11 socket directory, a namespace, not a data root).
- **Not done**: the gate is still scoped to WORKTREE/data-root literals, so a
  hardcoded `/dev/shm/...` inside a *test* file is not reported (the gate's
  `wantTest=false` is deliberate — a test may legitimately name a fixture). That
  is the pre-existing scope, not a loosening introduced here.

## 4. The suite half of the ruling: assertions the ruling made wrong

The ruling changed the intended host state, so three assertions became claims
that are FALSE on a correctly configured host. Silence would have been the worst
outcome (a permanently red suite trains readers to ignore it), so each was
repaired to assert what is still true — never deleted, never skipped to green:

- `internal/tools`: `TestSuiteScratchRootIsMemoryBacked` →
  `TestSuiteScratchRootBackingIsKnown`: the backing must be MEASURED, named and
  off the `/` device (`AssertSuiteScratchRootSafe`), instead of requiring RAM.
- `internal/tools` `freshDBStallAttribution` + `TestScratchRootFilesystemClassification`:
  the block-backed arm no longer prescribes the retired base; the test now
  asserts BOTH directions on whatever root the host pinned (memory-backed ⇒ no
  fsync blame; block-backed ⇒ the text names no retired location), and names the
  cost as the operator's accepted trade.
- `internal/agent` `TestSuiteTempRootIsPinned`: same repair. The guarantee that
  actually protects the suite is that the root is pinned *and off `/`* — which
  is what it now asserts.
- `freshDBStallCeiling` 2s → **30s** (named constant, one place). 2s was
  calibrated against tmpfs (66ms at load 19); on the intended block-backed root
  a single fresh-DB cycle measured **2.73s vs a 31ms in-memory control** under
  this host's ambient load. The ceiling is the HANG detector, and a genuine hang
  is minutes — the new value is one order of magnitude above the measured case.
  `freshDBAttributionRatioMax` (12.0) is unchanged and still detects
  orders-of-magnitude divergence.
- `TestFreshDBOpenCloseIsFast` is now declared heavy
  (`heavytest.Require(t, heavytest.CriterionDiskFixture)`) and inventoried in
  `testdata/heavy-tests.tsv`: it is 12 real device round trips per run, i.e.
  minutes of real device work on the intended filesystem — Rule 27's
  classification, not a delete. It no longer decides a merge in a per-branch
  window whose verdict is host I/O.

**Honest limitation**: the block device's cost is real. The ruling accepted it;
this branch does not make it go away, it makes the tree honest about it (the
ledger notice, the stall attribution and this devdoc all name it as a cost
rather than as a defect).

## 5. Evidence

Merged state (`6deca64c` + master `994c8ec1`), all commands below run in
`~/workspace/gopi-shm-role` on branch `fix/scratch-shm-role-split`.

Device ids — the criterion is MEASURED, never a spelling:

```
$ stat -c '%n dev=%d' / /dev/shm /home/user/workspace
/                          dev=2051
/dev/shm                   dev=28
/home/user/workspace       dev=2052
$ git worktree add ~/workspace/gopi-shm-evidence -b chore/shm-evidence-probe
$ stat -c '%n dev=%d' ~/workspace/gopi-shm-evidence
/home/user/workspace/gopi-shm-evidence dev=2052      # NOT /dev/shm (dev 28)
$ df --output=source,fstype,target ~/workspace/gopi-shm-evidence | tail -1
/dev/sda4      ext4 /home
$ mkdir -p /home/user/workspace/gopi-test-scratch/suite-probe-run-761645
$ stat -c '%n dev=%d' …/suite-probe-run-761645
…/suite-probe-run-761645 dev=2052                   # fresh scratch, block device
(probe worktree/branch/scratch dir removed after the measurement)
```

Legacy artifact recognised and cleaned — **base arm** (built from this branch):

```
$ mkdir -p /dev/shm/gopi-verify-01a0b1b2; touch -d '10 hours ago' …
$ ./bin/gopi-role scratch-gc base --dry-run
scratch-gc role=cli-base base=/dev/shm removed=3 stale=6h0m0s disabled=false dry-run=true
would-remove /dev/shm/gopi-ph-c2 age=97h5m55s
would-remove /dev/shm/gopi-privhint-scope3 age=97h6m16s
would-remove /dev/shm/gopi-verify-01a0b1b2 age=10h0m0s
$ ./bin/gopi-role scratch-gc base
scratch: base sweep could not remove /dev/shm/gopi-ph-c2: unlinkat …/.gopi/logs/worker.log: permission denied
scratch: base sweep could not remove /dev/shm/gopi-privhint-scope3: unlinkat …: permission denied
scratch-gc role=cli-base base=/dev/shm removed=1 stale=6h0m0s disabled=false
$ ls -d /dev/shm/gopi-verify-01a0b1b2   → No such file or directory
$ ls -d /dev/shm/gc-bin /dev/shm/agent-branch.log /dev/shm/gopi-ph-c2
/dev/shm/gc-bin  /dev/shm/agent-branch.log  /dev/shm/gopi-ph-c2
```

Two things that matter in that output: the sweep RECOGNISES the legacy base and
removes this system's own namespace there; and the two entries it could NOT
remove were **reported, not silently lost** (Rule 15) — while a foreign name
(`gc-bin`) and a foreign file (`agent-branch.log`) were never touched.

Legacy artifact recognised and cleaned — **suite-pin arm** (the namespace rule):

```
$ mkdir -p /dev/shm/gopi-suite-tmp-probe762549; touch -d '10 hours ago' …
$ go test ./internal/tools/ -run 'TestSuiteScratchRootIsPinned|TestSuiteScratchRootBackingIsKnown' -count=1
ok  github.com/nick/gopi/internal/tools  0.535s
$ ls -d /dev/shm/gopi-suite-tmp-probe762549   → No such file or directory
```

Gate red-proof (planted bad shape, then removed — the tree is clean again):

```
$ printf 'package scratch\n\nconst zzPlantedRootProbe = "/dev/shm/gopi-planted"\n' > internal/scratch/zz_planted_probe.go
$ go test ./internal/standards/ -run TestGate7WorktreeRootLiteralRealTreeIsClean -count=1
--- FAIL: TestGate7WorktreeRootLiteralRealTreeIsClean (0.68s)
    Rule 13 (no hardcoded worktree root literal): hits=2 baselined=0 escaped=1 NEW=1
    … {rel:internal/scratch/zz_planted_probe.go line:3 desc:"/dev/shm/gopi-planted": hardcodes the standard
      shared-memory filesystem — RETIRED 2026-09-18 as a CREATE location (the scratch home is a real directory
      on the block device); the value survives only to RECOGNISE and CLEAN legacy artifacts — reference
      worktreeroots.ShmDir (the ONLY definition) instead}
$ rm internal/scratch/zz_planted_probe.go
$ go test ./internal/standards/ -run TestGate7WorktreeRootLiteralRealTreeIsClean -count=1
ok  github.com/nick/gopi/internal/standards  1.365s
$ git status --porcelain    → (empty)
```

Note which literal is `escaped=1` in the red output: `internal/shot/x11.go`'s
`/tmp/.X11-unix`, i.e. the one reviewed `// worktree-root-ok:` subject literal —
the gate still counts it and still keeps it out of the failure.

Build / vet / suites (merged state):

```
$ go build ./...            → BUILD_OK
$ go vet ./...              → VET_OK          (go vet is the only cheap compile of _test.go)
$ go test ./internal/standards/ ./internal/worktreeroots/ ./internal/scratch/
ok  …/internal/standards 9.756s   ok …/internal/worktreeroots   ok …/internal/scratch
$ go test ./internal/tools/ ./internal/shot/ ./internal/agent/ ./cmd/agent/
    → ./internal/tools: FAIL TestFirstOpenSyncModeDecisionTable — reproduced on
      base 45c6ecc1 too (0.394s); the sibling worker filed it as 01a0b1bf
    → ./internal/shot, ./internal/agent, ./cmd/agent: ok
```

Operator-facing text:

```
$ grep -rn 'mount -t tmpfs' --include=*.go internal cmd | grep -v _test
internal/scratch/tmproot.go:767:// It used to print a tmpfs mount (`mkdir -p … && mount -t tmpfs …`), and   ← doc, saying it is retired
$ grep -rn '/dev/shm' --include=*.go internal cmd | grep -v _test | grep '"'
internal/scratch/rootgocache.go:59   "tmpfs alternative is the 2026-09-15 one (/dev/shm at 100%%). Neither is acceptable."   ← incident, kept
internal/scratch/rootgocache.go:156  "… wrote 726M-1.14G per build into /dev/shm and filled it (100%% …) …"               ← incident, kept
internal/standards/worktree_root_gate.go  the gate's own reason (the RETIRED clause)                                    ← guard, kept
$ grep -rn '/dev/shm' scripts/ | grep -v 'shm-gc.sh\|setup-scratch-home.sh'
scripts/repro-live-holder-worktree-veto.sh:21:# off `/` by MEASUREMENT, not by the retired /dev/shm default).   ← names it as retired
```

`internal/shot/x11.go`'s `/tmp/.X11-unix` is the one remaining production
location literal, and it is the reviewed subject-literal: the X11 protocol
mandates that path, it is a namespace and not a data root, and deriving it would
break headless capture. It carries `// worktree-root-ok:` inline, so it shows in
the data report instead of hiding in a baseline.

## 6. §4 read-through of `internal/scratch/` + sibling sweep

**Decision that ASSUMED memory backing (found, and why it is not a bug):**

| file:line | decision | verdict |
|---|---|---|
| `internal/tools/worktree_sweep_memory_base.go:68` `memoryBaseRoot()` | derives an implicit root ONLY while the scratch home resolves onto a NON-`/` device that is a memory filesystem; on a block-backed home it returns `ok=false` | **correct in the recognise direction** — "cannot tell" / "not a memory fs" ⇒ no root, i.e. it never authorises a removal on a filesystem nobody chose. It is the one decision shaped entirely around "memory base"; its own test (`TestMemoryBaseRootSkippedOnDiskBackedScratchHome`) pins the block-backed branch. Kept; a sibling task is working this area (see §7). |
| `internal/scratch/rootgocache.go:113` `MemoryFilesystem` / `AssertGoCacheOffShm` | branches on memory-backed | **refuses**, i.e. the correct polarity regardless of what the scratch home is. Unchanged. |
| `internal/testutil/ledgerpin.go:170` `LedgerSandboxBackingNotice` | branches on memory-backed to decide whether to print a notice | notice-only, and its remediation sentence was already removed by the 2026-09-18 ruling — it now states the device cost and stops. Correct; untouched. |
| `internal/testutil/scratchroot.go:454` / `ledgerpin.go:177` | `scratch.MemoryFilesystem` classification | used for attribution and for the inconsistency check in `assertScratchPathSafe` (same device as `/` **and** memory-backed = contradictory host geometry) — direction-agnostic. Kept. |
| `internal/scratch/gc.go`, `holder.go`, `limits.go` | GC/residency rules | **no memory assumption in a decision**: age (`StaleAge=6h`), namespace (`IsBaseResidueName`/`Collectable`), liveness (`/proc` cwd+fd+exe+maps + `.holder`) and the permission-blocked-subtree repair are filesystem-independent. The `/dev/shm` mentions are incident history. |
| `internal/scratch/tmproot.go` `DefaultTmpDir` / `ensureUsableTempRoot` / `AssertTempRootOffRootFS` | temp-root policy | criterion is "off the `/` **device**", never "on tmpfs" — untouched by the ruling and unimprovable by it. |
| `internal/scratch/base_residue.go` `ResolveBase`/`SweepBase`/`BaseResiduePlan` | defaults to `/dev/shm` | legacy-recognition default; never creates the base (`os.ReadDir` → `IsNotExist` ⇒ 0/nil), and refuses a base on `/`. Kept. |

**Sibling sweep — `GOPI_SCRATCH_MEMORY_BASE` readers:** exactly one definition
(`internal/scratch.MemoryBaseEnvName`, tmproot.go) and exactly one reader
(`internal/testutil/legacy_residue.go MemoryBase()`, plus its `cmd/agent`
sweep caller). The retired setup script reads it no more — and
`TestMemoryBaseNameMatchesTheSetupScript` asserts the script does NOT mention it,
so "looks like a reader but is not one" cannot come back. The knob is still
meaningful: it points the LEGACY sweep at a base other than `/dev/shm` (a host
whose ad-hoc residue lives elsewhere), it is not a creation knob.

**Sibling sweep — `MemoryFilesystem()` callers that branch:** the three above
(`rootgocache.go` refuses, `ledgerpin.go` notices, `scratchroot.go` classifies
for attribution/consistency). None of them creates, and none of them assumes
memory-backed is the normal case any more.

## 7. Not done (honest)

- **The block-device cost is not mitigated.** After this branch the suite pays
  it on every fresh fixture, by the operator's decision. Measured on this host
  under its ambient load: `TestFreshDBInitIsFast` reds intermittently
  (2.68s and 4.72s observed; 2/2 and 1/2 in small A/B samples) **on the base
  commit too** — that is the cost, not a regression from this branch.
- **Two pre-existing reds reproduce on the base commit** (`gopi-shm-base`,
  detached at `45c6ecc1`): `TestScriptsAreBoundedAndOffTmp`
  (`internal/testutil`, `devdoc/evidence/google-x11-ab/*` carry `/tmp` literals
  and an unbounded loop) and the `TestFreshDBInitIsFast` timing above.
- **Sibling branch `feat/shm-gate-shapes` (`6f988037`) — MERGED, not left for
  the merger.** It was the same epic's other half (the gate's FOOTPRINT: bare
  `/dev/shm` anchors, the fixture topology), developed against the same base
  `45c6ecc1`, and it overlapped this branch in exactly one file. Two complete
  halves of one decision must land as one shape, so it was merged here
  (`6deca64c`) with the conflicts resolved by keeping BOTH intents:
  the gate's extended footprint (bare-root anchor, fixture topology,
  `escapeTest`/`scanTests`, boundary note) plus this branch's
  `worktreeRootRetiredClause` inside the reason string, with the red-proof still
  asserting the clause. `devdoc/shm-gate-shapes.md` (from that branch) is the
  companion record of the footprint half. Its §3 site table and this §1 table
  describe the same 22 sites from the two halves' points of view; where they
  differ is only the class names, not the decisions.
- **`scripts/*` rigs re-wired here are comment/harness edits only** — no shipped
  binary behaviour changed on this branch (the role split is docs + one hint
  function + the gate's reason string).
- The 55 historical `/dev/shm` comment lines are untouched on purpose.
