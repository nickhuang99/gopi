<!-- doc-class: normative -->
# The test-DB scaffold contract (frozen 2026-09-21)

This is the **interface** for the test-database scaffold. A migration ticket
(`01a0c2e7-db61-7dc8-bcb5-c24e6f1304c5`) is developed against this file in
parallel with the implementation, so it is written to be read by someone who
cannot see the code and cannot ask questions. PROJECT.md Rule 32 is the rule; this
file is the detail it points at.

**Freeze statement:** the signatures below are the contract. If one must change,
this file changes in the same commit as the code, and the change is announced in
the report.

## 1. Exact signatures (all in `internal/testutil/dbfixture.go`)

```go
// WRITER shape: a fresh, empty, per-test database under the pinned scratch root.
// Sets GOPI_MEMORY_DB to the returned path (and masks GOPI_HIPPO_DB to ""), so a
// tool call resolved through the environment lands on THIS file.
func NewTestDBPath(t *testing.T) string

// READER shape: the shared, read-only, once-per-run sample. The returned handle
// is opened with mode=ro + query_only, so a write through it fails.
func Sample(t *testing.T) *sql.DB

// The same sample's PATH, for a caller that hands it to a subprocess or an
// external tool. The file is mode 0444, so opening it with a plain DSN is still
// write-refusing (except for uid 0).
func SampleDBPath(t *testing.T) string

// The sample's DOCUMENTED content, in one place. Assert on these identifiers,
// never on an ordinal.
func SampleSeed() []SampleRow
func SampleSeedEvents() []SampleEventRow
func SampleDocs() []SampleDocRow

// PRIVATE, WRITABLE copy of the same sample definition — for a fixture whose
// SUBJECT is an ambient database path (it must be possible for the file to be
// written, or "nothing wrote it" proves nothing). Per call, never sealed.
func SampleReportAmbientFixtures(t *testing.T) string

// A device-backed scratch directory in the shared scratch home (Rule 22), for a
// fixture whose subject needs a real block device. Skips when the scratch home is
// memory-backed (there is no device to measure).
func TestDeviceScratchDir(t *testing.T, sub string) string
```

Seed types (whose fields the migration may read):

```go
type SampleRow struct {
    TaskID, SessionID, Summary, Status, Tags, Source string
    Priority int
}
type SampleEventRow struct {
    TaskID, Event string
    CreatedAt     int64
}
type SampleDocRow struct {
    Topic, Question, Answer, Tier string
    Confidence                    float64
}
```

## 2. Usage contract per shape

### Writer (`NewTestDBPath`)

- **Caller guarantees**: nothing. One call is the whole setup.
- **Scaffold guarantees**: (1) the path is under the pinned scratch root and
  provably off `/` — it FAILS the test rather than falling back to `os.TempDir()`
  when the suite never pinned a root; (2) the file does not exist yet; (3) each
  call returns a NEW path; (4) `GOPI_MEMORY_DB` = the path and `GOPI_HIPPO_DB` = `""`
  for the rest of the test (restored by `t.Setenv`, absence included — Rule 17);
  (5) the directory is refused if it would land in the operator's live profile.
- **Caller must NOT**: open the returned path writable-and-shared across tests, or
  assume a schema exists. **The scaffold does not apply the schema** — that DDL
  belongs to `internal/tools` (`openTasksDB`), which this package must not import
  (import cycle). A test that needs rows opens the path through the tools layer,
  which is what every existing fixture already does.
- **On misuse**: a `t.Fatalf` naming the missing pin, or (from
  `RefuseOperatorProfileWrite`) a refusal naming the profile.

### Reader (`Sample` / `SampleDBPath`)

- **Caller guarantees**: it will not try to write through the sample.
- **Scaffold guarantees**: the sample is built once per run, from `SampleSeed` +
  `SampleSeedEvents` + `SampleDocs`; `Sample`'s handle is read-only twice over
  (open mode `mode=ro` + `query_only(true)`, and file mode `0444`); a write
  attempt fails with "attempt to write a readonly database" (or a Unix `EACCES`
  for a plain-DSN caller).
- **Caller must NOT**: mutate the sample, or rely on rows beyond the documented
  ones.
- **On misuse**: a failed write. **Not** a per-test copy — the sample is shared by
  design, and the immutability is enforced at the file and at the connection, not
  by copying (copying per test is what the fixture exists to avoid).

### Ambient-path fixture (`SampleReportAmbientFixtures`)

- **Caller guarantees**: it may write into the returned path (that is the point).
- **Scaffold guarantees**: a fresh per-call copy of the shared definition, in the
  pinned scratch root, unsealed.
- **Why it exists**: a test whose subject is an ambient database path must be able
  to observe that nothing wrote through it; a sealed sample would make the write
  impossible for the wrong reason.

## 3. Seed contract (the rows a test may assert on)

**Stability promise: ADDING rows is allowed; changing or removing a documented row
is a CONTRACT CHANGE** (same commit as this file).

`tasks` (from `SampleSeed`):

| TaskID | Status | Tags | Source | Priority |
|---|---|---|---|---|
| `sample-completed-1` | `completed` | `sample` | `auto` | 5 |
| `sample-pending-1` | `pending` | `sample,reader` | `human` | 2 |
| `sample-claimed-1` | `claimed` | `` | `auto` | 5 |

`task_events` (from `SampleSeedEvents`): `sample-completed-1` → `created`
(1700000000), `completed` (1700000100); `sample-pending-1` → `created`
(1700000200); `sample-claimed-1` → `created` (1700000300), `claimed` (1700000400).
Timestamps are FIXED, never `now()`.

`distilled_qa` (from `SampleDocs`): `sample-hot-1` (tier `hot`, confidence 0.99)
and `sample-warm-1` (tier `warm`, confidence 0.5). **The first documented row is
the HOT one** — a hot-resident reader's fixture depends on that order.

The three `tasks` statuses are duplicated as literals here because this package
cannot import `internal/tools`. The duplication is CHECKED, not trusted:
`internal/standards` `TestSampleSeedStatusesExistInTheToolsConstants` fails if a
seeded status is not a value of a `taskStatus*` constant in `internal/tools`.

## 4. Naming and location convention

- One file: `internal/testutil/dbfixture.go` (production) + `dbfixture_test.go`.
- `New…` for a fixture that CREATES something; `…DBPath` for a path;
  the shared sample is spelled `Sample…` so a reader is obvious in a diff.
- A migrating file keeps its local helper NAME and delegates the body, so a
  reviewer sees the call sites unchanged (`setMemoryDB` is the worked example).

## 5. Worked before/after (the migration reference)

BEFORE — `internal/tools/memory_test.go` (as it stood):

```go
func setMemoryDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")   // resolves a path …
	t.Setenv(memoryDBEnv, path)                       // … and this line is deletable
	return path
}
```

AFTER — the shape the 47 call sites now inherit:

```go
func setMemoryDB(t *testing.T) string {
	t.Helper()
	return testutil.NewTestDBPath(t)
}
```

READER — `internal/agent/testenv_isolation_test.go`:

```go
func seedHotDBFixture(t *testing.T) string {
	t.Helper()
	return testutil.SampleReportAmbientFixtures(t)
}
```
(was: an inline `CREATE TABLE distilled_qa` + `INSERT` of a hand-written hot row,
inside a helper that also took `t.TempDir()`'s path and never pinned the env.)

## 6. Counting recipe (regenerate the inventory with this)

A `*_test.go` file counts as a **DB-path site** when it simultaneously carries a
`"*.db"` string literal, a path-resolver call (`filepath.Join` / `path.Join` /
`t.TempDir()`), and a DB opener (`sql.Open("sqlite"` / `openTasksDB(` / `openDB(`).
A **per-file DB helper** is a top-level `func …DB(t *testing.T)` in a `_test.go`
file. Measured 2026-09-21, in `devdoc/testdb-scaffold-migration-inventory.tsv`.

## 7. CURRENT STATUS (honest — do not build on an unimplemented signature)

| Item | Status |
|---|---|
| `NewTestDBPath` | **IMPLEMENTED** |
| `Sample`, `SampleDBPath` | **IMPLEMENTED** |
| `SampleSeed`, `SampleSeedEvents`, `SampleDocs` | **IMPLEMENTED** |
| `SampleReportAmbientFixtures` | **IMPLEMENTED** |
| `TestDeviceScratchDir` | **IMPLEMENTED** |
| Rule 32 gate (`db_scaffold_gate_test.go`) | **IMPLEMENTED, PARTIAL** (see §8) |
| PROJECT.md Rule 32 | **IMPLEMENTED** |
| Writer consumer migrated | **DONE** (`internal/tools/memory_test.go`, 47 call sites) |
| Reader consumer migrated | **DONE** (`internal/agent/testenv_isolation_test.go`, 3 tests) |
| Corpus migration | **NOT DONE** — sibling ticket `01a0c2e7-…` |
| Anything not in this table | **not designed; do not assume it exists** |

## 8. The gate is partial — what it does and does not catch

`internal/standards/db_scaffold_gate_test.go`:
`go test -run TestTestDBScaffoldIsUsedOrTheLedgerEnvIsPinned ./internal/standards/`

- **Catches (arm 1)**: a test function/helper that RESOLVES a DB path itself
  (`t.TempDir()`, `os.MkdirTemp`, `filepath.Join`, or a pre-scaffold path builder)
  and opens sqlite without setting a `*DB*` environment variable. A resolver call
  whose arguments are all the function's own parameters does NOT count (the caller
  owns the pin). Foreign databases opened for a refusal test are exempt via the
  inline escape `db-scaffold-ok: <reason>`.
- **Catches (arm 2)**: `os.MkdirTemp(".", …)` / `CreateTemp(".", …)` — a scratch
  directory created inside the source tree.
- **Does NOT catch**: a path resolved in one helper and opened in another; an
  opener reached through a variable or interface; a test that pins the env and then
  ignores the pinned path; a helper whose path comes from a parameter but whose
  caller never pinned anything.
- **Scan roots**: `internal/tools`, `internal/agent`, `internal/testutil`,
  `cmd/agent`, `testsuite`, `internal/standards`, `archive` — each with a read
  floor, so a broken walk fails instead of passing vacuously.
- **Permits**: four exact `file:line` permits, each with its reason (all four are
  false positives of the fingerprint, not debt — see the gate's
  `dbScaffoldBaseline`). A permit that no longer matches FAILS the gate.

## Acceptance — observed invocations (Rule 31)

All six run from the task worktree `~/workspace/gopi-testdb-scaffold`.

1. **Writer shape (real test, invocation, pass)**
   `go test ./internal/tools/ -run TestMemorySearchEmptyFreshDB -count=1 -v`
   → `--- PASS: TestMemorySearchEmptyFreshDB (0.66s)` / `ok github.com/the operator/gopi/internal/tools 1.531s`.
   The test's helper is `setMemoryDB` → `testutil.NewTestDBPath(t)` (§5).
2. **Reader shape + measured cost**
   `go test ./internal/testutil/ -run TestSampleIsBuiltOnceAndTheSharedCostIsMeasured -count=1 -v`
   → `sample setup cost: build-once 1.060900093s (what EVERY test pays without the
   fixture; 50 tests would pay 53.04500465s) vs shared lookup 6.256µs per call
   (169581x cheaper)` / PASS. (Also measured 492.4ms vs 18.0µs and 491.3ms vs
   6.3µs on two earlier runs — the absolute build cost is device-dependent.)
   Reader consumer: `go test ./internal/agent/ -run TestTestEnvIsolation -count=1 -v`
   → 3/3 PASS, with `fixture not injectable — the assertion would be vacuous
   (digest="#1 sample-hot-1 | conf=0.99 | …")` now satisfied by the shared sample.
3. **Immutability**
   `go test ./internal/testutil/ -run TestSampleRefusesEveryWriteShape -count=1 -v`
   → PASS. Mechanism observed: **a FAILED WRITE** (not a fresh copy) — the
   fixture's own handle rejects `INSERT` and `CREATE TABLE` with a "readonly"
   error (DSN `mode=ro` + `query_only`), and a plain-DSN handle against the same
   path is refused by the 0444 file mode.
4. **Gate red + green**
   `go test ./internal/standards/ -run 'TestDBScaffoldPredicateReddensOnSyntheticSources|TestTestDBScaffoldIsUsedOrTheLedgerEnvIsPinned' -count=1 -v`
   → both PASS: 8 subtests, of which **2 planted violations fire** (the
   shared-hand-rolled helper; the in-tree scratch directory) and **6 compliant
   shapes stay silent**; the tree-wide arm passes with the four permits live.
5. **The leak**
   `grep -rn 'MkdirTemp("\."' --include=*_test.go .` → 5 hits: 4 are COMMENTS
   (the retired shape's explanation in `firstopen_sync_test.go:503`, three in the
   gate's own docs, one in `dbfixture_test.go:229`) and 1 is the gate's synthetic
   probe (`db_scaffold_gate_test.go:661`) — **0 live call sites**.
   `find internal -maxdepth 3 -name '*device-*' -type d | wc -l` → `0`;
   `find internal -name '*.db' | wc -l` → `0`.
6. **GOPI.md rule + its claims**
   `git diff --stat master...HEAD -- GOPI.md` → `1 file changed, 69 insertions(+)`
   (Rule 32 itself is 68 lines: `git show HEAD:GOPI.md | awk 'NR>=1295' | grep -c ''`).
   (An earlier iteration of this step was written as "+70 lines" from a draft
   count; the measured number after the final commit is +69.)
   Claim checks: `testutil.NewTestDBPath`, `Sample`, `SampleDBPath`,
   `SampleReportAmbientFixtures`, `TestDeviceScratchDir`,
   `RefuseOperatorProfileWrite` all exist under exactly those names
   (`grep -n '^func <name>' internal/testutil/*.go` — lines 122/184/206/582/461 and
   `profileguard.go:87`); the cited gate
   `TestTestDBScaffoldIsUsedOrTheLedgerEnvIsPinned` exists
   (`internal/standards/db_scaffold_gate_test.go:466`) and is the command quoted in
   the rule; the cited files `devdoc/testdb-scaffold-contract.md` (this file) and
   `devdoc/testdb-scaffold-migration-inventory.tsv` exist.
