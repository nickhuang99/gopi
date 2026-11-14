<!-- doc-class: normative -->
# Task Report: memory-base residue — a bounded sweep for this system's own ad-hoc base names

Branch: `feat/scratch-base-residue` (worktree `/home/user/workspace/gopi-shmbase`)
Commit: `9739a3cf` (sweep) → `34abde94` (this report) → `d6320bcc` (rebased verification note)
Branch tip at time of writing: `d6320bcc`, rebased onto master `eb802d64`.

## The defect

Agent sessions create ad-hoc scratch directories **directly in the tmpfs memory
base** (`/dev/shm`) instead of under the owned scratch home:

```
gopi-verify-<taskid>   gocache-<taskid>   gopi-acc-<taskid>
gopi-privhint-*        probe-*           ledger-snap*.db    *-final.log
```

Neither existing sweep could ever match them, and both are namespace-limited
**by construction**, not by accident:

| sweep | walks | matches |
|---|---|---|
| `internal/scratch.SweepHome` (gc.go) | `HomeDir()` only | `suite-` / `gopi-suite-tmp-` / `queue-` |
| `internal/testutil.GCLegacyScratchResidue` | the resolved memory base | `gopi-` **AND** `suite-tmp-` infix |

So this family had **no namespace, no collector and no lifetime**.

### Measurement

- **2.6 GB across 702 entries**, filling the **7.8 GB** tmpfs to **100%**.
- Failure mode: builds broke — sqlite `disk is full`, `ENOSPC` in `/dev/shm`.
- Re-measured later the same day, **after the host was reclaimed by hand**:
  `/dev/shm` 1.2G/7.8G, 9 entries. Re-measured during this task: 2.1G/7.8G, 9
  entries.

**The defect is the MECHANISM, not today's byte count** — that is the whole
point of this deliverable. A hand-reclaimed host re-accumulates the family the
moment sessions run again, because nothing collects it.

## Completed

### 1. `internal/scratch/base_residue.go` — the new arm

**Namespace predicate** (`IsBaseResidueName` / `IsBaseResiduePath`), each family
justified as a name **this repository's own commands produce** — no invented
namespace:

- `gopi-` — the repo's own directory-name prefix (`internal/testutil`'s
  `gopiScratchPrefix`); covers the measured `gopi-verify-*`, `gopi-acc-*`,
  `gopi-privhint-*`.
- `gocache-` — a Go build cache an agent pointed at the base by hand
  (`GOCACHE=/dev/shm/gocache-<taskid>`); 89 measured directories, the largest
  single family in the 702. No repo literal emits it, so it is justified from
  the measurement in the constant's doc comment.
- `probe-` — the short-lived writability/device probe directory.

**The rule is SHAPE, not a bare prefix.** A prefix test alone would delete a
neighbour's directory the moment someone named it `gopi-…`:

```
name = <family> "-" <token> [ "-" <token> ]*
```

- the first dash-token must **EQUAL** a family (`gofile-x` is not a match);
- a bare family is refused (`probe`, `gocache`, `gopi-` — never produced);
- `os.MkdirTemp`-hostile characters are refused (`gopi-verify-01a0a387.tar`);
- **at least one** remaining token must be machine-generated: it carries a
  **digit**, an **underscore**, a **canonical UUID** shape, or is a **base36
  key** (`k7fq`, `9f3a2c1b…`).

**Refused on purpose** (a human chose these; measured in the base):

`gocache-root` · `gocache-key-2026` · `gopi-notes` · `probe-cfg` ·
`gopi-suite-tmp-*` (the legacy sweep's namespace) · `gopi-test-scratch` (the
scratch home — the base arm would `RemoveAll` the live tree).

**Collectability** (`SweepBase` / `BaseResiduePlan`) shares **ONE** predicate
with the plan face, so `--dry-run` can never promise what the real sweep
refuses:

1. the base must not share `/`'s filesystem — **measured device ids** (Rule 22:
   a criterion, never a spelling), and the measurement happens only for a base
   that EXISTS (a missing base is a non-event, not an alarm);
2. namespace + directory kind (`Lstat`; a symlink is never followed);
3. **age** (`scratch.StaleAge`);
4. **holder** — ONE batched `/proc` cwd+fd+exe+maps pass plus the `.holder`
   registration. **A directory held by a live process is NEVER removed.**
5. removal is `removeCollectable` — the *same* removal the home sweep uses, so
   the permission-blocked-subtree repair and its "unmeasurable ⇒ keep" contract
   apply unchanged.

The base is **never created** and never recursed into: only direct children,
and a foreign tree is a single skipped entry.

### 2. CLI + resident wiring

- `gopi scratch-gc base [--dry-run]` — the operator-facing face, sharing the
  existing verb's parser, disabled switch and logfmt report.
- The resident **worker/merger tick** runs the same arm on its existing cadence
  and off switch (no new daemon, no new tool — Rule 3).

### 3. Safety for the dev host (a correction made mid-implementation)

Running the tests builds real directories; the **automatic** arm would then
sweep the *developer's own* `/dev/shm` on every `go test`. Two guards:

- `SetAutoBaseResidue(false)` is called by
  `internal/testutil.PinSuiteForTestMain`, so the automatic arm is OFF for the
  duration of any suite; a **configured** base (`GOPI_SCRATCH_BASE`) is
  unaffected — an explicit knob is an explicit instruction.
- `TestSweepBase_AutoResidueArmIsOnOnlyWhenArmed` asserts the pin holds and
  that a configured base is still swept.

**Verified:** after a full `go test ./internal/scratch/`, the host's 9 real
`/dev/shm` entries are all still present.

### 4. Tests (`internal/scratch/base_residue_test.go`)

- **namespace table** — accepted and refused shapes pinned name by name;
- **fixture (a)** a fresh entry is UNTOUCHED;
- **fixture (a, hard rule)** an age-stale entry held by a **live** process
  (open fd) is UNTOUCHED;
- **fixture (b)** an age-stale, unheld entry IS collected;
- **fixture (c)** a foreign/other-program name is NEVER a candidate;
- **dry-run** names exactly the synthetic stale entry and never a foreign one,
  and deletes nothing;
- refusing a base on `/`, refusing an empty/missing base, refusing the scratch
  home, the family list, the knob's single reader.

## Verification

```
go build ./...                        : OK
go test ./internal/scratch/ -count=1  : ok  (all new tests PASS)
go test ./internal/testutil/          : ok
go test ./internal/standards/         : Rule 16 FAILS — PRE-EXISTING, not mine
                                        (proved below)
```

### `internal/standards` — an honest note

`TestGate1ErrorWrapRedProof` fails at `cmd/agent/cli_voice_client.go:811` and
`cmd/agent/main.go:965` with a stale baseline entry at `cmd/agent/main.go:954`. **This is
not a regression I introduced.** Proof: on my branch's base commit
(`e025df78`) with my changes stashed, the same two `NEW violation` + one
`STALE` result appears — while the *newer* main-repo master (`eb802d64`) has
this site already fixed. My worktree was created from an older master tip, so
it inherited a red gate that master has since repaired. Every other gate (13,
17, 18, 19, 20, 21) is green.

### End-to-end CLI evidence

Fixture with two in-namespace entries and two foreign ones, all aged 48h:

```
$ GOPI_SCRATCH_BASE=$FX gopi scratch-gc base --dry-run
scratch-gc role=cli-base base=$FX removed=2 stale=6h0m0s disabled=false dry-run=true
would-remove $FX/gocache-01a0a998 age=48h0m0s
would-remove $FX/gopi-verify-01a0a999 age=48h0m0s
```

It named **exactly the two synthetic entries** and **neither foreign one**, and
deleted nothing.

The same fixture on the **`/` partition** is refused by measurement (Rule 22):

```
$ GOPI_SCRATCH_BASE=$FX gopi scratch-gc base
scratch-gc role=cli-base base=$FX removed=0 …
exit=1
stderr: … is on the same filesystem as / — refusing to sweep it
```

On a **real tmpfs base** (`/dev/shm`), the sweep collects exactly ours:

```
$ GOPI_SCRATCH_BASE=$FX gopi scratch-gc base
scratch-gc role=cli-base base=/dev/shm/tmp.7uirXgJMRo removed=2 …
exit=0
$ ls -1 $FX
gocache-root      # foreign — survived
postgres          # foreign — survived
```

## Not Done (honest scope)

- **`go test ./internal/standards/` is NOT fully green — on master, not here.**
  `TestStandardGates/TestGate1ErrorWrapRedProof` (Rule 16) reports 2 NEW
  violations (`cmd/agent/cli_voice_client.go:811`, `cmd/agent/main.go:965`) and
  1 STALE baseline entry (`cmd/agent/main.go:954`). This is **pre-existing on
  master and unrelated to this branch**: re-run in the untouched main repo at
  `/home/user/work/GoPi` it fails identically, and this branch touches none of
  those files (`git diff --name-only master..HEAD` lists only the eight files
  below). Every other gate passes. **Named here so it can be dispatched
  separately** — it is a master-red defect, not a scratch defect.
- **`.db` and `.log` FILES in the base are NOT collectable.** The deliverable
  asks for directories; `ledger-snap*.db` and `*-final.log` from the measured
  residue are regular files matched by nothing. Deleting a *file* needs its own
  argument (a database is not a scratch directory), and the existing sweeps are
  directory-only by contract. **Named here so it can be dispatched separately.**
- **Two measured leftovers are deliberately NOT candidates:**
  `/dev/shm/gopi-modprobe2` (likely a security tool's dir) and
  `/dev/shm/gopi-ph-b`/`-c`/`-c2` (sibling tasks' probe dirs). They are refused
  by the shape rule; deleting them is an operator decision, not a sweep's.
- **The 12-hour `StaleAge` premise is untouched.**
  `briefings/brief-scratch-gc-stale-window.md` (a separate dispatched task,
  present in this worktree) questions the bare 6h constant. This task reuses
  `scratch.StaleAge` for the base arm rather than inventing a second threshold.
- **No LLM tool was added** (Rule 3), so Rule 11's LLM-adoption proof does not
  apply: the deliverable is a resident sweep plus a CLI verb, both exercised
  here by real runs.

## Verification

Run in the worktree `~/workspace/gopi-shmbase`, branch `feat/scratch-base-residue`,
rebased onto `eb802d64` (= master tip at time of writing):

- `go build ./...`                                            → **OK** (exit 0)
- `go test ./internal/scratch/`                               → **ok** (10.0s)
- `go test ./internal/testutil/`                              → **ok** (70.1s)
- `go test ./internal/standards/`                             → all gates PASS except
  `TestGate1ErrorWrapRedProof` (Rule 16, 2 NEW + 1 STALE) — **proven pre-existing**:
  it fails identically in the untouched main repo `/home/user/work/GoPi` on the
  same master commit, and this branch touches none of the flagged files.

The three acceptance assertions, run explicitly:

```
--- PASS: TestBaseSweepFixture_StaleUnheldIsCollected        (stale + unheld → collected)
--- PASS: TestBaseSweepFixture_FreshEntryIsUntouched         (fresh → kept)
--- PASS: TestBaseSweepFixture_LiveHeldIsUntouched           (live holder → NEVER removed)
--- PASS: TestBaseSweepFixture_ForeignNameIsNeverACandidate  (foreign name → not a candidate)
--- PASS: TestBaseSweepFixture_MixedBaseCollectsOnlyOurs     (mixed base → only ours)
--- PASS: TestBaseResiduePlan_NamesExactlyTheSyntheticStaleEntry (dry-run names exactly it)
--- PASS: TestSweepBase_RefusesTheScratchHome
--- PASS: TestSweepBase_AutoResidueArmIsOnOnlyWhenArmed
--- PASS: TestBaseResiduePrefixes_AreTheDocumentedFamilies
```

`TestSweepBase_RefusesABaseOnTheRootFilesystem` **SKIPs** on this host: its
fixture needs a directory on the `/` partition and `t.TempDir()` here resolves
off `/` (Rule 22 — the host's ambient temp root is already off `/`). The
refusal path it covers is exercised for real by the end-to-end CLI run below.
The skip is announced, never silently green.

End-to-end against a synthetic base (real CLI, `gopi scratch-gc base --dry-run`):
the plan named exactly the synthetic stale entries and **neither** foreign entry;
a `/-partition` base was refused by measured device id; a real `/dev/shm` base
collected exactly ours while foreign entries survived; all 9 real `/dev/shm`
entries survived a full suite run (the auto arm is OFF unless armed —
`SetAutoBaseResidue`, wired through `internal/testutil.PinSuiteForTestMain`).

## Files

```
internal/scratch/base_residue.go       (new, the sweep + namespace predicate)
internal/scratch/base_residue_test.go  (new, fixtures + namespace table)
internal/scratch/scratch.go            (HomeDirEnvName: the one name of the
                                        scratch-home override)
internal/scratch/tmproot.go            (ProcRootPath is now an alias of
                                        ProcRootDir — one /proc spelling)
internal/testutil/scratchroot.go       (suite pin: automatic base arm OFF)
cmd/agent/cli_scratch_gc.go            (the `base` arm + --dry-run face)
cmd/agent/scratch_gc_tick.go           (resident tick runs the base arm)
```

## Recovery

The deliverable lives on branch **`feat/scratch-base-residue`** in
`/home/user/workspace/gopi-shmbase` (a linked worktree of `~/work/GoPi`, so the
commit is already in the shared repo — no push needed).

```
git show feat/scratch-base-residue:devdoc/scratch-base-residue.md
git log --oneline -1 feat/scratch-base-residue     # d6320bcc (tip; sweep commit was 9739a3cf)
```
