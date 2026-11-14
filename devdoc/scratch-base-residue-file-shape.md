<!-- doc-class: normative -->
# The base sweep's kind gate: a name in the namespace with no collector

**Class**: normative — the rule `internal/scratch/base_residue.go` enforces, and
the seam between its two halves (name ↔ candidate).

## The defect (measured 2026-10-01, branch `task/<this run>`, host peerbox)

`IsBaseResidueName` said **yes** about `/dev/shm/gopi-red2`; the candidate
predicate said **no**. Both halves were individually defensible and nobody
reported the contradiction:

```
$ gopi scratch-gc base --dry-run --explain          # BEFORE
scratch-gc role=cli-base base=/dev/shm removed=0 stale=6h0m0s disabled=false dry-run=true
```

One line. Not one `would-remove`, not one verdict — while `/dev/shm` held

```
-rwxrwxr-x 1 nick nick 29005304 Sep 30 08:01 gopi-red2
-rwxrwxr-x 1 nick nick 29003704 Sep 30 08:02 gopi-red3
-rwxrwxr-x 1 nick nick 28404218 Sep 29 00:36 gopi-after-01a0e8c7
-rwxrwxr-x 1 nick nick 28396285 Sep 29 00:14 gopi-before-01a0e8c7
```

— four 28 MB **regular files**, four names this system's own command wrote
(`go build -o /dev/shm/gopi-<what>`, an A/B pair, a red-proof pair).

## Root cause (5-why)

1. **Symptom** — `removed=0`, and `--explain` printed no line about the entries
   the operator could see with `ls`.
2. **Mechanism** — `SweepBase`/`BaseResiduePlan` gated candidates on
   `IsBaseResiduePath`, which required `Lstat + IsDir()`.
3. **Why that gate existed** — the base sweep was written for the 2026-09-15
   residue, which was *directories* (`gopi-verify-<taskid>`, `gocache-<taskid>`,
   `probe-<rand>`); "a directory this system created" was the contract inherited
   from `internal/testutil`'s legacy-root arm. Files were never *declared* out of
   scope — they were simply not in the shapes the author had measured.
4. **Why nobody noticed** — the two halves of "ours" live in two functions and
   were tested separately (a name table; a kind table). No test asked the
   **seam** question: *for one name, does the name layer and the candidate layer
   agree?* And no face reported a refused in-namespace entry: a name that passes
   the namespace and fails the kind gate produced neither a removal nor a
   sentence, so `removed=0` was the same output for "clean base" and "four
   entries nobody can collect" — Rule 15's defect shape exactly.
5. **Root cause** — a namespace rule and a collectability rule that are claimed
   to be "one predicate" were in fact **two**, and the second one silently
   narrowed the first. The rule is not "the base holds only directories"; it is
   "the base holds only entries this system's own commands wrote".

## The fix

1. **One kind gate, one function** — `isBaseResidueContainer(fi)`:
   **directory OR regular file**, symlink still refused, socket/fifo/device
   refused. `IsBaseResiduePath`, `BaseResidueVerdictFor` and the suite-run arm's
   `isSuiteRunRoot` all call it, so the sweep, the plan, the report and the
   suite-run arm cannot drift apart.
   - WHY FILES ARE IN: a file has no identity beyond its name, so the namespace
     rule (shape-checked, table-tested, on a 1777 shared base) is the entire
     safety argument for it — the same argument the directories have always
     relied on. The measured residue (`-o /dev/shm/gopi-…`) is precisely a name
     this system wrote.
   - WHY SYMLINKS ARE STILL OUT: `os.RemoveAll` on a link is fine, but following
     one means acting on a target that may be nowhere near the base. The
     constraint is unchanged and is now asserted from both directions in
     `TestBaseResidueSeam_NameLayerAgreesWithCandidateLayer`.
   - WHY FIFOS/SOCKETS/DEVICES ARE OUT: deleting a device node a neighbour's
     daemon is using is the one mistake that cannot be undone.
2. **The verdict face** — `BaseResidueVerdictFor` is the ONE predicate in its
   reporting form (namespace → kind → age → holder), with the deciding sentence.
   `BaseResidueCollectable` is its boolean face; `BaseResiduePlan`, `SweepBase`
   and `BaseResidueVerdicts` all go through it. `gopi scratch-gc base --explain`
   prints one line per **in-namespace** entry:
   - `verdict=collectable … collectable: no owner record — …`
   - `verdict=keep … a symlink is never a candidate …`
   - `verdict=keep … younger than the stale window (6h0m0s)`
   - `verdict=keep … a live process holds it (the .holder registration names live pid N)`
   Foreign names are deliberately **not** listed: they were never candidates by
   construction, and the base is a shared 1777 directory (305 entries on this
   host) — a verdict per neighbour would bury the answer in noise.
3. **The cross-seam regression test** —
   `TestBaseResidueSeam_NameLayerAgreesWithCandidateLayer`: for every family
   name, a stale + unheld entry must be **collectable as a directory and as a
   regular file**, the plan must name it and the sweep must remove it; a symlink
   must be refused *with the symlink sentence*. Red-proof: with the kind rule
   reverted to dirs-only, it fails on all three arms (recorded in the run's
   report).

## The honest boundary (what this does NOT collect)

`IsBaseResidueName`'s namespace is unchanged, so the following are still KEPT —
they are visible in `--explain` output as neighbours that are simply not ours:

| entry | why refused | size (measured on peerbox, 2026-10-01) |
|---|---|---|
| `/dev/shm/livecopy.db` | not the family-prefix shape | **153 MB** |
| `/dev/shm/rule35-redproof.d5hJ` | a dotted suffix is not in the name alphabet | **219 MB** |
| `/dev/shm/rowunit-evidence` | a bare word, no generated token | **175 MB** |
| `/dev/shm/gopi-red`, `gopi-fin`, `gopi-verify`, `gopi-rework`, `gopi-doccheck` | `gopi-<word>`: every token is a plain word, so `baseResidueSuffixGenerated` is false — the 2026-09 fold rule, deliberately | **5 × 27 MB = 136 MB** |
| `/dev/shm/after_loop.go`, `*.log`, `*.txt`, `*.bak` (≈300 entries) | plain filenames, no family | ≈0.4 MB total |

`/dev/shm` itself held **2.6 G across 410 entries** when this was measured. Note
the fourth row: it is the same *act* as the fixed residue (`go build -o
/dev/shm/gopi-<what>`) but not the same *shape* — `gopi-red2` carries a digit and
`gopi-after-01a0e8c7` a task id, while `gopi-red` carries one plain word. The
namespace rule refuses the latter on purpose (a bare-word form would make
`gopi-tmp` a candidate), so **136 MB of the residue class stays out of scope by
the name rule, not by the kind rule** — the operator's decision, named here
rather than quietly fixed.

These five rows are the operator's decision, not this sweep's: each is a
**namespace** question ("is this name this system's own residue?"). Nothing here
changes them; the column exists so the operator can see the remaining volume at
a glance.

The `.db`/`*.log` family is the one the 2026-09-15 doc already named: a

The `.db`/`*.log` family is the one the 2026-09-15 doc already named: a
**namespace** decision (is `livecopy.db` this system's residue?), not a kind
decision — and widening a 1777-path predicate is not something this change does.

## Verification

**Red-proof, measured** (the kind rule reverted to `return fi.IsDir()` in
`isBaseResidueContainer`, the fix restored immediately after):

```
$ go test ./internal/scratch/ -run 'TestBaseResidueSeam|TestBaseResidueKind|TestBaseResidueVerdict'
--- FAIL: TestBaseResidueSeam_NameLayerAgreesWithCandidateLayer (1.89s)
--- FAIL: TestBaseResidueVerdicts_ExplainsEveryInNamespaceEntry (3.37s)
FAIL	github.com/nick/gopi/internal/scratch	9.353s

$ go test ./internal/scratch/ -run 'TestBaseResidue|TestSuiteRun|TestSweepBase'   # fix in place
ok  	github.com/nick/gopi/internal/scratch	34.579s
```

```
$ gopi scratch-gc base --dry-run --explain        # AFTER
scratch-gc role=cli-base base=/dev/shm removed=4 stale=6h0m0s disabled=false dry-run=true
would-remove /dev/shm/gopi-after-01a0e8c7 age=48h0m8s
would-remove /dev/shm/gopi-before-01a0e8c7 age=48h0m8s
would-remove /dev/shm/gopi-red2 age=48h0m8s
would-remove /dev/shm/gopi-red3 age=48h0m8s
scratch-verdict /dev/shm/gopi-after-01a0e8c7 verdict=collectable age=48h0m11s collectable: no owner record — nothing recorded an incarnation, so only the age window can bound it
scratch-verdict /dev/shm/gopi-before-01a0e8c7 verdict=collectable age=48h0m11s collectable: no owner record — …
scratch-verdict /dev/shm/gopi-link-01a0e8c7 verdict=keep age=0s a symlink is never a candidate — its target is not this sweep's to remove [kept]
scratch-verdict /dev/shm/gopi-red2 verdict=collectable age=48h0m11s collectable: no owner record — …
scratch-verdict /dev/shm/gopi-red3 verdict=collectable age=48h0m11s collectable: no owner record — …
scratch-verdict /dev/shm/gopi-mc-01a0f450 verdict=keep age=21m33s younger than the stale window (6h0m0s) [kept]
gopi scratch-gc base: 41 candidate(s) judged, 4 collectable
```

The four files above were planted for that measurement (the real ones had already
been removed by an earlier pass of this same fix), which lets the **same four
files, the same command, two binaries** be compared directly — the HEAD binary
(`git show HEAD:internal/scratch/base_residue.go` restored, rebuilt) and the
fixed one:

```
### A) HEAD binary (no fix) — the 4 files are on disk, 29 MB each, 48h old
$ /tmp/gopi-prefix scratch-gc base --dry-run --explain
scratch-gc role=cli-base base=/dev/shm removed=0 stale=6h0m0s disabled=false dry-run=true
                                                       ← and nothing else. Not one word.

### B) FIXED binary, same files, same command
$ /tmp/gopi-fix scratch-gc base --dry-run --explain
scratch-gc role=cli-base base=/dev/shm removed=4 stale=6h0m0s disabled=false dry-run=true
would-remove /dev/shm/gopi-after-01a0e8c7 age=48h0m2s
would-remove /dev/shm/gopi-before-01a0e8c7 age=48h0m2s
would-remove /dev/shm/gopi-red2 age=48h0m2s
would-remove /dev/shm/gopi-red3 age=48h0m2s
scratch-verdict /dev/shm/gopi-after-01a0e8c7 verdict=collectable age=48h0m4s collectable: no owner record — …
scratch-verdict /dev/shm/gopi-before-01a0e8c7 verdict=collectable age=48h0m4s collectable: no owner record — …
scratch-verdict /dev/shm/gopi-red2 verdict=collectable age=48h0m4s collectable: no owner record — …
scratch-verdict /dev/shm/gopi-red3 verdict=collectable age=48h0m4s collectable: no owner record — …
gopi scratch-gc base: 40 candidate(s) judged, 4 collectable
```

`removed=0` versus `removed=4` under one variable (the kind gate), with the
`--explain` silence-versus-four-lines difference being the Rule 15 half of the
defect.

The deleting face is measured in its own run — a directory-name, a regular file
and a **symlink** planted together, so refusal is observed next to collection:

```
$ gopi scratch-gc base --explain
scratch-verdict /dev/shm/gocache-01a0e8c7   verdict=collectable …   ← regular file, collected
scratch-verdict /dev/shm/gopi-after-01a0e8c7 verdict=collectable …  ← regular file, collected
scratch-verdict /dev/shm/gopi-link-01a0e8c7 verdict=keep … a symlink is never a candidate … [kept]
scratch-verdict /dev/shm/gopi-red2          verdict=collectable …   ← regular file, collected
gopi scratch-gc base: 41 candidate(s) judged, 3 collectable
scratch-gc role=cli-base base=/dev/shm removed=3 stale=6h0m0s disabled=false
$ ls -la /dev/shm/gopi-red2 /dev/shm/gopi-after-01a0e8c7 /dev/shm/gocache-01a0e8c7 /dev/shm/gopi-link-01a0e8c7
ls: cannot access '/dev/shm/gopi-red2': No such file or directory
ls: cannot access '/dev/shm/gopi-after-01a0e8c7': No such file or directory
ls: cannot access '/dev/shm/gocache-01a0e8c7': No such file or directory
lrwxrwxrwx 1 nick nick 13 Oct  1 07:09 /dev/shm/gopi-link-01a0e8c7 -> /etc/hostname
```

A directory, a regular file, a symlink and a foreign neighbour in one run: the
first two went, the symlink is still there with its reason printed, and no
foreign entry was touched.

## Files

- `internal/scratch/base_residue.go` — `isBaseResidueContainer`,
  `BaseResidueVerdictFor`, `baseResidueKeepReason`, `BaseResidueVerdicts`,
  `BaseResidueCollectable`, `IsBaseResiduePath`.
- `internal/scratch/base_suite_runs.go` — `isSuiteRunRoot` shares the kind gate.
- `internal/scratch/base_residue_test.go` — kind table (file now accepted; fifo
  refused), the seam test, the `--explain` test.
- `cmd/agent/cli_scratch_gc.go` — `runScratchGCBase(dryRun, explain)`,
  `reportScratchGCBaseVerdicts`, usage text.

## Ruling

**A name in the namespace with no collector is a defect, not a resting state.**
If a shape this system's own commands produce passes the namespace predicate, the
candidate predicate must either collect it or say why not — and the `--explain`
line is where it says so.
