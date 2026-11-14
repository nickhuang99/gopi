<!-- doc-class: historical -->
# The flake registry's SECOND direction, and the row that was swallowed

Task: `01a10a5708d7761cacc347bf07a791a3` (epic from the raw briefing archived
with the row). Branch `task/01a10a5708d7761cacc347bf07a791a3`.

## The defect this closes

`internal/standards/flake_registry_gate_test.go` read the registry in ONE
direction: every row must name a test that EXISTS. Two consequences, both
measured:

1. **A flaky test registered by nobody reads green.** The gate's only input was
   the table, so the table could under-report the class it exists to document and
   nothing noticed.
2. **A row appended onto a `#` comment line is SWALLOWED.** With no trailing
   newline on the file, an append concatenates onto the last line; when that line
   is a comment, the row inherits the `#` and becomes prose. Every reader of the
   file sees it, and the gate — reading "no row here" — stays green.
   The condition was **live in this repo at the start of this task**: `tail -c 1
   testdata/flakes.tsv | od -c` showed the file ending `… n p a t h ) .` with no
   `\n` (measured on task 01a10a2d; still true on this branch before the fix).

## The mechanism chosen, and why the other was rejected

The missing direction needs a second source of the flaky set that is
**INDEPENDENT OF THE TABLE**. Two candidates:

**(a) A second exemption table** (e.g. `testdata/flakes-declared.tsv`) —
**rejected.** It is the same artifact as the registry: hand-maintained, appended
by the same edit tool, subject to the same missing-newline swallow and the same
"somebody forgot" failure. Two tables that must be kept in step are one table
with a second place to rot, and a gate asserting them equal would still be
asserting a fact about TABLES — never about the code.

**(b) A declaration carried in the TEST itself** — **chosen.**
`flaketest.Declare(mechanism)` sits in the test body, so:

- it is written **at the moment a person is looking at the mechanism** (the same
  moment the registry row gets its prose);
- it cannot be forgotten by a tool appending to a *different* file;
- the AST scan reads the **CODE**, which is the only source that moves when a
  test is renamed, deleted or moved.

The division of labour is deliberate. The table keeps what a machine cannot
derive (prose mechanism, owning ledger row, date); the call site keeps the
machine-readable membership bit. **The two sources fail independently**, which is
what makes `registry ⊇ declared` worth asserting.

### Why `Declare` is a verdict-neutral marker (no panic, no skip)

- **No skip / no retry / no suppression** — a flake is a DEFECT, and a call that
  changed a verdict would HIDE exactly what the registry documents (the same
  reason the gate refuses `heavytest.Require` on a registered test).
- **No panic on a bad mechanism** — the first draft panicked, and the tree's own
  Rule 19 gate caught it (`TestGate4PanicRedProof`: *NEW violation at
  internal/flaketest/flaketest.go:226*). The gate is right for a second reason
  too: a panic fires **only where the test happens to run**, while the
  enforcement belongs where the evidence is. The mechanism argument is read as a
  **string literal from the AST** at the call site, so a declaration that names
  nothing is reported **by file:line whether or not the test is ever executed**.
  `flaketest.MechanismOK` is that check's one definition; the gate calls it.

### The structural guard: what exactly is a swallowed row

The scanner (`flakeScanRegistryText`, pure — the same function the red proofs
drive) reports three file-shaped defects, each **by line number and by name**:

| shape | finding |
|---|---|
| file does not end with `\n` | the CONDITION that makes the next append inherit the last line's shape |
| `#` line carrying a TAB | **SWALLOWED REGISTRY ROW** — a row concatenated onto a comment |
| row with leading whitespace | a row that does not begin at a line start |
| wrong column count | malformed |

**One exemption, and it is a convention rather than a free pass**: the
column-header line `# test\tpackage\t…` is spelled once, in the leaf package
(`flaketest.RegistryHeader`, Rule 14), and the comparison is **exact** — a
swallowed ROW can never be byte-equal to the header, because the header's first
column is the word `test` where a row's is a test name. This was found by the
gate's own first run against the real file (a false positive on line 45), which
is the argument for having the pure checker at all.

## Deliverables

1. `internal/flaketest` (leaf, no project import) — `Declare`, `MechanismOK`,
   `DeclareShape`, `DeclareSelfName`, `DeclareMinMechanismWords`.
2. The six registered tests carry a `Declare` call with their mechanism
   (internal/tools ×2, cmd/agent ×4).
3. `flake_registry_gate_test.go` — `TestFlakeRegistryCoversEveryDeclaredFlake`
   (registry ⊇ declared AND declared ⊆ registry, both by name + file:line), a
   non-vacuity floor (`flakeMinDeclaredTests`), and the structural guard.
4. Red proofs: `TestFlakeScanRejectsAMalformedRegistry` (pure scanner over
   synthetic text: swallowed row RED **by name**, leading whitespace RED,
   missing trailing newline RED, malformed columns RED, well-formed GREEN) and
   `TestFlakeDeclarationGateRedProof` (the real `flakeCoverageDiff`, both arms).
5. Real-file two-arm demonstration, recorded in the task report.

## Not Done (honest)

- The structural guard covers `testdata/flakes.tsv` only. The same swallow can
  happen in `testdata/heavy-tests.tsv` and `devdoc/DOC-INDEX.tsv` (the memory
  record from task 01a10a2d names all three); it was not in this epic's scope.
- The gate cannot decide that an UNdeclared test is flaky — it can only hold the
  two sources in step. A test that is flaky and carries no `Declare` call is
  still invisible until a human diagnoses it; the call site makes that a
  one-line addition at the moment of diagnosis, not a new kind of proof.
