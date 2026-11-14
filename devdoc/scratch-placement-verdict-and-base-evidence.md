<!-- doc-class: historical -->
# Scratch placement — a red test must say WHICH thing is wrong

Task: `01a12f12-8a1d-7dd9-9b2c-50fbdab874f9` · branch `task/01a12f128a1d7dd99b2c50fbdab874f9`

## The defect this closes

Two ambient tests in `internal/testutil` failed whenever the host's memory base
was low, and the message they printed was:

```
placement "device-root": this suite's root is NOT memory-backed although /dev/shm is —
a fresh ledger built under the root pays the device's journal commits
```

That sentence is TRUE and MISLEADS. It reads as *the placement logic is broken*,
while the placement had done exactly what it is designed to do: **decline a
candidate with no room**. The two situations need different readers:

| situation | what it is | who fixes it |
|---|---|---|
| `placementOutcomeDefect` | the code declined (or chose wrongly) while the host HAD room | a code change |
| `placementOutcomeBaseSaturated` | the base genuinely has no room | room on the memory filesystem, or a human decision about what occupies it |

Measured live during this task: `/dev/shm` = 7804604416 bytes, free swinging
between **1.2 MiB above** the 1.0 GiB floor and **244 MiB below** it, within
minutes, because the base is a SHARED 1777 directory other processes write to.
A test whose verdict flips with that swing cannot be read.

## Design

**The decision is judged against the LIVE measurement, and only that decides.**
`classifyAmbientPlacement(p, liveFree, liveErr)` answers the three-valued
outcome from the placement's own answer plus the kernel's number for the base.
An unmeasurable base cannot prove room, so it is reported as the environment
condition with the failure named (never a silent yes, never a code regression).

**The two failure situations are never collapsed into one "FAIL"** — they are
different values of a named type with different text, and a test asserts the
values differ.

**The saturated verdict carries evidence, read-only.** `baseSaturationEvidence`
reports, and `Report()`/`Census()` print:

- the free space and the floor the decision was taken on;
- the top-level entries with each one's size, age and **the name predicate's
  verdict** (`scratch.IsBaseResidueName` — the ONE predicate the sweep itself
  decides on, so an explanation cannot drift from the decision);
- the **HUMAN DECISION LIST**: the largest names the predicate REFUSES, which is
  the registry a person needs to make a decision that is theirs, not the code's;
- the **RECLAIM FACES, measured** — both PLAN faces (`BaseResiduePlan`,
  `BaseSuiteRunPlan`), because "the reclaimer collects nothing" has to be
  measured before it is called a failure.

Nothing in the evidence writes, deletes or fills anything: `os.ReadDir`, `Info`,
`WalkDir` and the plan faces. The only fixture that reproduces SATURATION does it
on the `memoryFreeBytes` / `baseFreeBytes` seams — filling a real shared tmpfs
would consume the host's RAM and break every other process on the machine
(Rule 27).

## The one wait that was removed

The first version of the reader waited up to 30 s for a saturated base to
recover. It was both useless and wrong (measured: a 40 s verdict for 5 s of
work): the placement's own reclaim-before-decline step already RE-MEASURES after
it collects, so a second reading by the caller cannot change the answer (the
placement's inputs are filesystem kinds and the caller's channels —
deterministic state). The reader now reads once.

## Acceptance — run on the day the base was still saturated

`GOPI_SCRATCH_MEMORY_BASE=/run/lock` makes the OTHER tmpfs on this host the
memory base. It is 5.0 MiB, it really is a memory filesystem, and a saturated
base is therefore reproducible at any moment, **without writing a byte to it**:

```bash
GOPI_SCRATCH_MEMORY_BASE=/run/lock go test -count=1 -v \
  -run 'TestScratchPlacementIsMemoryBackedOnThisHost|TestScratchPlacementHonoursACallerRootAndReportsADevice' \
  ./internal/testutil/
```

- the ambient test FAILS, and its message says `BASE SATURATED — THE PLACEMENT
  LOGIC IS NOT THE FINDING HERE`, names the measured bytes and prints the
  evidence block;
- the decision-table test PASSES and states the same conclusion for its own
  cases;
- the same command on a HEALTHY base (`/dev/shm` above the floor) passes both —
  so the change is a verdict about the situation, not a muted assertion.

A BROKEN placement still turns BOTH red: the decision was temporarily forced to
the device arm while the host had 1.2 GiB free, and both tests reported
`PLACEMENT DEFECT (a code regression …)` naming the free space that proves it.

## Not done / honest limits

- The **2422 MiB** of name-refused residue measured on 2026-10-12 was taken at a
  different moment. Today the same measurement reports **1123.9 MiB across 188
  entries** (the base is live: `gopi-test-scratch` itself fell from 386 MiB to
  140 bytes in between). The numbers name their measurement time for that
  reason. Nothing was deleted, no predicate was widened, no floor was moved.
- Read-only-ness of the verdict fixtures is proven on a PRIVATE fixture base
  (`TestBaseSaturationEvidenceIsReadOnlyAndNamesItsNumbers`, by a
  size+mtime fingerprint). It is deliberately NOT asserted by re-counting the
  shared base: that count moved 408 → 402 between two readings seconds apart
  with this test doing nothing to it — such an assertion could only be red for
  someone else's work.
