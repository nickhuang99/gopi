<!-- doc-class: normative -->
# The order/determinism flake class: a root-cause registry, a repetition catch, and a gate

Task row: the epic "flake class (order/determinism, host-independent) has no
criterion and no catch". Precedents: `devdoc/heavy-tests-rule27-gate.md`,
`devdoc/heavy-tests-rederivation.md` (the heavy class), and PROJECT.md Rule 27.

## The class, and why it is NOT the heavy class

The heavy class closed its "long" half from a real `go test -json` stream (a 5s
floor, `testdata/heavy-tests.tsv`) and its "ambient/load" half by criterion
(`heavytest.CriterionHostCondition`). The third class is different in kind:

| | heavy | flake (this class) |
|---|---|---|
| verdict belongs to | the HOST (load, wall clock, sudo) | the CODE + host (map order, shared fixture root, pid slot, wall-clock seed) |
| on the smoke path | skip — correct, there is nothing to fix | **never skip** — a skip reads as a passing green and HIDES a defect |
| instrument | duration from the run's stream | REPETITION of the test itself (`go test -count=N`) |
| disposition | `heavytest.Require` / `Criterion*` | fix the flake, and record its DETERMINISTIC cause |

So a flaky test may not be routed through `heavytest.Require`: a skip would make
the registry's own rows invisible exactly when someone looks for them. The gate
enforces that boundary mechanically (a registered test that also calls
`heavytest.Require` fails).

Rule 27 is untouched and absolute: nothing in this deliverable generates load,
pressure or contention. The whole instrument is `go test -count=N` over the test
itself — repetition, never the host.

## The three pieces

1. **`testdata/flakes.tsv`** — the registry. Five TAB-separated columns:
   `test`, `package`, `root_cause` (`deterministic: <mechanism>`), `owner`
   (the ledger row that fixed or owes the fix), `date`. Four rows, all real, all
   quoted from the repo's own diagnosis where one exists
   (`devdoc/sigpipe-flake-fix.md`, `devdoc/tools-flake-fix.md`,
   `devdoc/tools-flake-diagnosis.md`).
   **It is not a suppression list**, and the gate is what makes that true: a row
   without `deterministic: `, without an owning row id, without a valid date, or
   naming a test that does not exist, fails the build.

2. **`scripts/flake-catch.sh`** — the catch. Runs `go test -count=N -json` over a
   scope and NAMES every test whose verdict moved (`FLAKY … pass=P fail=M`, exit
   1), separating it from a test that fails every repetition (exit 3, a plain
   defect) and from a scope that measured nothing (exit 4 — never read as a clean
   green). Off the smoke path; reached through `make flake-catch`
   (`PKGS='-reps 20 ./cmd/agent/'`). Exit 0 means "no verdict moved at THIS N",
   and the report says so rather than implying stability.

3. **`internal/standards/flake_registry_gate_test.go`** — the gate, three halves:
   registry↔code in both directions, the class boundary (no `heavytest.Require`,
   no gate-package rows), and the mechanism itself: the smoke/merge path must not
   run it, the shared names must agree across Go and shell, and the mechanism must
   actually NAME a planted flake (see the red proof).

## Shared names (Rules 1b/13/14)

`internal/flaketest` is a leaf package (no project imports — the same reason
`internal/heavytest` exists) holding the one definition of the knob
(`GOPI_FLAKE_REPS`), the registry path, the catch/probe script paths, the
repetition band (2..100) and the date layout. `scripts/lib/flake-threshold.sh`
holds the shell side. The gate asserts the two spell the same values, that the
Go knob literal appears exactly once (at its definition), and that the script
reads the knob through the shared name rather than a bare literal.

The repetition band's edges are load-bearing: below `MinReps` a run cannot tell a
flake from a pass (this class's whole signature), and above `MaxReps` the
mechanism stops being a mechanism and becomes a load generator — which Rule 27
forbids. So an out-of-band value is REFUSED loudly by the script and never
silently clamped (a caller must never believe it ran a count the band forbids).

## The red proof, and how it is kept honest

A mechanism that is only grepped is a mechanism nobody has seen fire, so the gate
runs it: `scripts/flake-gate-probe.sh` builds a throwaway module whose only test
is a PLANTED map-iteration-order flake (it takes "the first key" of a Go map and
asserts the one key the contract names), points the REAL mechanism at it, and the
gate requires that the plant is named (exit 1, `FLAKY`, and a pointer at the
registry). It then runs the same fixture with the order dependence removed and
requires that nobody is named — without that control, "the mechanism named it"
could just mean the mechanism names everything.

The plant is a genuine coin flip (one accepted key of three), so the probe uses 6
repetitions: a false "nothing moved" is `(1/3)^6 ≈ 1.4e-3`. Measured on this host:
`pass=3 fail=3 rate=50%` — the plant is named, the control is silent, and the
whole probe costs a couple of seconds (it is the gate packages' own rule that
they may never be classified heavy).

Three NEGATIVE red proofs were run against the gate itself (plant a defect, watch
it go red, restore):

- a row whose cause column is an excuse (`flaky sometimes: …`) → FAIL, "a row
  without a DETERMINISTIC mechanism is a suppression entry";
- a row naming a deleted test → FAIL, "no such test function exists";
- the mechanism moved onto the `test:` target → FAIL, "the default
  (merge/verify) path MUST be smoke-only".

## Honest limits

- **The registry is a floor, not an inventory of the class.** It holds the four
  flakes that have been ROOT-CAUSED. A flake nobody has diagnosed yet is not in
  it; the catch mechanism is how such a flake gets NAMED, and a registry row is
  how the naming becomes durable. The loop is: `make flake-catch PKGS=<suspect>`
  → a named `FLAKY` test → pin its mechanism → add the row.
- **Repetition is a detector with a rate, not a proof of determinism.** The
  mechanism prints the N it used and never claims more than that N supports.
- **The owner column is checked against the task ledger, and the check is skipped
  LOUDLY where the ledger is unreadable** (the sandboxed suite points
  `GOPI_MEMORY_DB` at a temp DB): the gate prints
  `FLAKE-LEDGER-UNVERIFIED` and still enforces the owner's SHAPE. On a machine
  with the real ledger (measured here over `~/.gopi/memory.db`) all four owner ids
  resolve to real rows. The id parser itself is unit-tested on a synthetic
  listing, so the deep check is not its first exercise.
- **The mechanism is not wired onto the merge path, by design.** It reports;
  nothing consumes its exit code automatically. That is the Rule 27 boundary the
  epic asked for, and it is asserted rather than assumed.

## Postscript: the probe's first version was itself a flake, and the replay cases that fixed the reading

The gate above is behavioural — it runs the real mechanism over a planted map-order
flake. That design is what exposed the next three defects, in this order, all
before the work was reported done.

**1. The probe was a gate that fired on its own coin flip.** The first plant made
ONE map-order assertion on a 3-key map, at 6 repetitions. Measured, it failed the
gate in 2 of 8 runs — because a 3-key map's first key is the "right" one 0.775 of
the time (MEASURED over 40 runs: 2 keys 0.875, 3 keys 0.775, 4 keys 0.55, 6 keys
0.325 — a small map is NOT uniform, since Go picks a random start slot in the
bucket and the first-declared key holds slot 0). So the plant passed all 6
repetitions with probability 0.875^6 ≈ 0.45, and the gate reported a false green.

**2. The over-correction failed in the OPPOSITE direction.** Widening the plant to
ten assertions drove the per-repetition pass probability to ~0.05, so all 10
repetitions failed and the mechanism reported the fixture as a PLAIN BUG (exit 3,
"fails every repetition") — 10 of 12 runs. Both of these are the same mistake
wearing different clothes: a repetition-based red proof has TWO ways to lie, and
the fixture's per-repetition pass probability must sit near 1/2.

**3. The fix is a design point, not a number.** Three 3-key assertions give
p ≈ 0.775³ ≈ 0.47 per repetition, and 20 repetitions put BOTH errors at ≤ 1e-6
(false "no flake" = 0.47²⁰ ≈ 1e-7; false "plain bug" = 0.53²⁰ ≈ 2e-6). Measured
after the retune: 15 of 15 gate runs green (was 2 of 12). The numbers live in
`flake_probe_reps`'s comment so the next agent does not re-tune them blindly.

**4. Two REAL reading defects in the mechanism, found by the replay cases.** A
probabilistic probe cannot check what the report DOES with a given stream, so the
runner grew `-replay <case>`: it emits a synthetic verdict stream (running no
test) and the gate checks the exit code and the report text against expectations
declared in the shared lib (`flake_replay_expect`). The first replay run exposed:

- **a moving SUBTEST was NAMED** (`FLAKY … TestX/Subtest`). A subtest's re-run
  scope is its PARENT (`-run Parent`) and a registry row names a top-level test
  function, so naming the subtest names the wrong owner. Subtests are now left out
  of the tally and the report SAYS how many it ignored.
- **an all-SKIPPED scope exited 0** ("no verdict moved"). That is the exact reading
  error this class exists to end: `-count=10` over a scope whose tests are all
  skipped by a classification switch measured NOTHING, and reporting it as a clean
  green is how a mechanism becomes decorative. Now `measured` requires at least one
  pass or fail, and a scope with none exits 4 with its skips listed.

Both were demonstrated by replay, fixed, and pinned by the gate. Neither was
visible from the plant: a real flake probe can show that the mechanism SEES a
flake, never that it reads every other stream correctly.

**5. The gate's own two failure modes are red-proved too.** Making the mechanism
always exit 0 (blind) and always exit 1 (fires on everything, including the
order-pinned control) each turn the gate red — so the gate is not merely "the
script exited 1", it is "the plant was named AND the control was not".
