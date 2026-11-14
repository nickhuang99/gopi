<!-- doc-class: historical -->
# Task Report: CONFIG.md §2c flag-column gate (doc ↔ catalog flag face)

## Completed

- **New gate** `internal/standards/config_doc_flag_gate_test.go` (same
  `standardgate` package as `config_doc_gate_test.go`), four assertions plus
  four red proofs:
  - `TestConfigDocFlagColumnMatchesCatalog` — the gate. Every §2c flag-table row
    that joins to a catalog row by ENV NAME has its flag column compared against
    the catalog's **own rendering** (`internal/config.EnvVarsFlagLabels`).
  - `TestConfigDocFlagColumnParsesTheWholeTable` — inventory pin + the run's own
    count line (`configDocFlagRowCount = 61`).
  - `TestConfigDocFlagColumnReportsUnjoinableRows` — a raw flag-table row with no
    env name to join on is REPORTED (named, with line + section), never skipped.
  - `TestConfigDocFlagColumnReportsRowsAbsentFromCatalog` — §2c rows whose env
    name has no catalog row at all are REPORTED, each with a recorded reason.
  - Red proofs: `…RedProofWrongFlagCell` (both directions + control),
    `…RedProofDecoratedCells` (a cell with prose around it),
    `…RedProofReadFloor` (a reshaped table scores nothing ⇒ the floor fires;
    a trailing empty cell is tolerated).
- **New exported primitive** `internal/config/envflaglabel.go`:
  `EnvVarsFlagLabels()` (+ `EnvVarsNoFlagMarker`, type `EnvFlagLabel`). It
  exports the catalog's flag column through `envFlagLabel` — the SAME function
  `gopi --help-env` draws its third column from. This is the load-bearing design
  decision: a *thin wrapper over the one definition* instead of a second copy of
  the catalog's formatting in the gate (Rule 13/14).
- **Run evidence** (Rule 31): `bin` rebuilt and `gopi --help-env` executed; its
  third column is byte-identical to what the gate compares against.

### What the gate actually covers (measured in this tree)

`§2c flag table: 61 row(s) parsed, 58 compared against the catalog's own
rendering, 2 present but absent from the catalog (reported, not compared),
1 unjoinable (no env name to join on); the catalog carries 88 rows`

- **58 of 61 rows are compared.** That includes every §2c sub-table whose header
  carries the flag column, and it covers **every settings key in the catalog's
  knob set**, not a subset. The remaining 3 are REPORTED by name with a reason
  (2 absent from the catalog — findings 1/2; 1 a whole settings section —
  finding 3), never silently skipped.
- **A row is scored on the flag TOKENS in its cell, not its wording** — so
  `*(none — there is no flag face)*` (the merge/memsync tick rows) scores as
  flagless and passes, and `--every` on `gopi memory sync` scores as the
  subcommand flag face it claims.
- Two rule arms, because a flag face has two shapes:
  1. **agent-level flag** — the catalog renders `--name`; the doc must carry
     exactly it (or no flag token, when the catalog renders the marker `—`).
  2. **subcommand flag** — the knob's flag lives on a subcommand
     (`--every`/`--timeout` on `gopi memory sync`, `--engine` on
     `gopi voice_input`). The authority is the **catalog's own help sentence**,
     which states the spelling ("also the --every flag, which wins", "CLI:
     --engine"). The check is **containment**: every flag the doc claims must be
     one the catalog names. This is not an allow-list — an invented subcommand
     flag reds (`--node-role` on `GOPI_NODE_ROLE` is the red proof), and
     containment (rather than equality) is required because the same sentence
     also mentions flags of *other* knobs (`--peer`, `--push`), which equality
     would false-red.

## Findings (reported, not papered over)

1. **`GOPI_SANDBOX_DIR` is a live knob missing from the catalog.**
   CONFIG.md §2c:264 documents it and its `--sandbox-dir`; `config.SandboxEnvName`
   exists (`internal/config/sandbox.go:36`), `config.ResolveSandboxRoot` resolves
   it, and `internal/config/config.go:444` parses the flag — that file's own
   comment calls it *"an ORDINARY knob"*. But **no row for it exists in
   `EnvVars`**, so `gopi --help-env` never lists it and the flag⇒env map cannot
   reach it. Verified: `grep -c "GOPI_SANDBOX" internal/config/settings.go` → `0`.
   **Fixing this is a catalog change (add the `EnvVars` row), not a gate change** —
   it is recorded in `configDocCatalogAbsentRows` with this reason. I did not make
   it: adding a catalog row changes help output and `--help-env` reachability, an
   operator-facing surface outside this epic's scope, and it deserves its own
   task with its own verification.
2. **`GOPI_SANDBOX_LEDGER_SEED`** (§2c:265) is likewise documented and read
   (`internal/sandbox/ledger.go:99`) but absent from the catalog — so its `—`
   flag cell has no far side. Recorded in the same list. Same remedy, same scope
   argument.
3. **The one unjoinable row is a SETTINGS SECTION, not a knob.**
   `CONFIG.md:326` (`repos`) carries `—` in both the env and flag cells; it is the
   §2d table's whole-section entry, and §2d’s per-key rows legitimately live in
   a 2-column table with no flag column. Recorded in `configDocUnjoinableRows`
   with that reason; the assertion compares the measured set against the record,
   so the row cannot silently leave the report.
4. **`CLIFlag` is stored bare (dashless) while the doc writes dashed.**
   `TmpDirFlagName = "tmpdir"`, `"db"`, `"home"`, `"api-key"` — the prefix comes
   from `flagPrefix` at render time. A gate comparing the doc cell against
   `CLIFlag` directly would red **50 of 61 rows**; comparing against
   `envFlagLabel` (the renderer) reds none. This is the reason the gate consumes
   the renderer and not the raw field, and it is recorded in `envflaglabel.go`.
5. **An unheld, untracked `CONFIG.md.bak`-style artifact risk avoided**: the
   mutation probe's backup was written **outside** the worktree
   (`/home/user/.gopi/sessions/01a0de83-…/CONFIG.md.bak`), so no litter entered
   the repo (Rule 30).

## Not Done

- **The catalog rows for `GOPI_SANDBOX_DIR` / `GOPI_SANDBOX_LEDGER_SEED` were not
  added** (finding 1/2). Until they are, those two rows are *reported* by the gate
  but not *compared*. That is the honest coverage line, not a claim of full
  coverage.
- **No baseline entries were needed** — `internal/standards/baseline.go` is
  untouched and the gate is green on the tree with zero permits. What the gate
  cannot express is recorded as an in-test REPORT with a reason, never as a
  permit.
- The gate does **not** check §2d/§2e/§2f (their tables have no flag column) nor
  the help↔parser direction (already owned by `internal/config/config_test.go` +
  `help_env_claim_test.go`; deliberately not duplicated).
- No new persistent dependency of any kind: Rule 35's four-question gate does not
  apply. The deliverable is Go source in the product's own tree (shipped in the
  binary, lifecycle = the repo's), plus one test. No script, cron line, state
  file, unit, or config key was created.

## Verification

- **Build**: `go build ./...` → OK (exit 0).
- **Tests**: `CGO_ENABLED=0 go test -count=1 ./internal/standards/ ./internal/config/`
  → `ok github.com/nick/gopi/internal/standards 46.342s`, `ok github.com/nick/gopi/internal/config 1.922s` (exit 0).
  Branch-scoped smoke of the two packages the diff touches; `gofmt -l` clean on both.
- **Ran**: the surface this change is *about* is the help's flag column, so the
  rebuilt binary was executed against it:
  - `CGO_ENABLED=0 go build -o …/gopi ./cmd/agent/` → `BUILD_OK` (17.7s, job-1)
  - `…/gopi --help-env` → its third column is the gate's far side, verbatim:
    `ANTHROPIC_AUTH_TOKEN  api_key  --api-key`;
    `GOPI_EFFORT_VALUES  effort_values  —`;
    `GOPI_MEMORY_DB  memory_db  --db` (bare `CLIFlag` rendered dashed — finding 4).
- **Result (the gate is RUN over the real doc, not trusted)**:
  - poison 1 — §2c `GOPI_TIMEOUT_SEC` flag cell `--timeout` → `—`:
    `--- FAIL: TestConfigDocFlagColumnMatchesCatalog` / `CONFIG.md:201 [Agent runtime] GOPI_TIMEOUT_SEC: flag cell "—" reads the flag(s) "", the catalog renders "--timeout"` (exit 1).
  - poison 2 (inverse) — `GOPI_EFFORT_VALUES` `—` → `--effort-values`:
    `CONFIG.md:180 [Thinking & reasoning] GOPI_EFFORT_VALUES: flag cell "`--effort-values`" claims "--effort-values", the catalog renders "—" and its own help names no such flag face (…)` (exit 1).
  - restored — `git diff --stat CONFIG.md` → empty; re-run → `ok github.com/nick/gopi/internal/standards 6.622s`.
  - row count — `-v -run TestConfigDocFlagColumnParsesTheWholeTable` prints the
    61/58/2/1 line quoted under *What the gate actually covers*.
- **Commit**: `c8a5f5d1` — `gate: machine-check CONFIG.md §2c's CLI-flag column against the catalog` ; that commit also carries the DOC-INDEX row the doc-classification gate generated for this report. THIS docs-only commit pins the hash (repo precedent: a report cannot name its own hash).

## Evidence (self-filed)

- `go build ./...` → printed `BUILD OK`, exit 0.
- `CGO_ENABLED=0 go test -count=1 ./internal/standards/ ./internal/config/` → the
  two `ok` lines quoted above, exit 0.
- Red-before/green-after, both directions, on the REAL CONFIG.md: the two FAIL
  blocks above (exit 1 each) and the restored `ok` (exit 0). Restoration proven
  by `git diff --stat CONFIG.md` printing nothing.
- Non-vacuity: the read-floor proof asserts a reshaped table yields **0** parsed
  rows and that 0 < `configDocMinFlagRows`; the inventory pin asserts the parse
  returns exactly 61; the red proofs drive the same `configDocFlagFindings` the
  gate calls and assert exactly one finding, with a clean control in the same
  test.
- Far side is the operator's own surface: `gopi --help-env` output (job-1,
  exit 0) shows `--api-key` / `—` / `--db` rendered by `envFlagLabel`, the
  function `EnvVarsFlagLabels` wraps.
- Coverage arithmetic is printed by the test itself, not asserted in prose.
