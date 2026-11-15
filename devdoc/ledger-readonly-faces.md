<!-- doc-class: normative -->
# Pure-read ledger faces on the read-only opener (task 01a13490)

Scope: (1) wire the remaining PURE-READ ledger faces to the read-only opener
(`openTasksDBReadOnly` + `LedgerReadOnlyDSN`) the previous run landed for
`task show`; (2) attribute the report that a ledger's `-wal`/`-shm` disappear
after a pure-read verb runs.

Both questions turned out to have the SAME root cause, which is why they are one
report: **opening the ledger through `openTasksDB` is what a reader must not do**
— it runs the tasks DDL batch on every open over a `_txlock=immediate` DSN, i.e.
it takes the ledger's WRITE lock (and, as measured below, destroys the sidecars
it finds).

## Completed

### 1. Eight pure-read faces wired to the read-only opener

Each conversion carries an in-code rationale ("this face is a PURE READ: its
SELECTs are the whole of what it does, and it calls no writer") above the changed
line. The whole change is one word per site (`openTasksDB` → `openTasksDBReadOnly`);
no new mechanism, constant, DSN spelling or timeout was introduced (Rule 1/2/13/14).

| # | Face | File:line | Why it is a pure read |
|---|------|-----------|-----------------------|
| 1 | `taskRowExists` | internal/tools/tasks.go:189 | one `taskByID` SELECT; returns a bool |
| 2 | `taskEventHasDetail` | internal/tools/tasks.go:618 | one `SELECT 1 FROM task_events …` |
| 3 | `TaskSessionID` | internal/tools/tasks.go:3565 | one row SELECT + the refusal helpers, themselves SELECT-only |
| 4 | `ResolveTaskBranch` | internal/tools/tasks.go:3454 | one row SELECT, parsed in memory |
| 5 | `TaskChildren` | internal/tools/reframe.go:227 | one `taskChildrenWithDB` SELECT |
| 6 | `TaskReframeStats` | internal/tools/reframe.go:274 | one `SELECT` over `task_events` + rendering |
| 7 | `TaskRowStatusAt` | internal/tools/task_run_retry.go:83 | one row SELECT, returns its status |
| 8 | `taskTagsByID` | internal/tools/memory_sync_delta.go:583 | one `SELECT COALESCE(tags,'')` |

Write paths were not touched. Every face whose own function writes
(`TaskNew*`, `TaskSetPayload`, `TaskVerify`, `SweepZombies`, the reclaim/merge
recorders, `taskUpdate*`, `TaskTransferHandover`, …) keeps `openTasksDB`, and
that is deliberate rather than incidental: a WRITE face needs the write opener
not only for the lock but because that opener is what heals a legacy ledger's
missing tasks tables (see Not Done 2).

### 2. `-wal` / `-shm` attribution

**Reproduced.** A copied ledger (`memory.db` + `memory.db-wal` + `memory.db-shm`,
which is the state the reporter described) read ONCE by a pure-read gopi verb
comes back as `memory.db` alone. Both sidecars are removed by the gopi process's
own `unlink` syscalls, with `rc=0`:

```
$ ls -la /home/user/workspace/_tmp/lb-show   # before
memory.db  memory.db-shm  memory.db-wal
$ striace -f -e trace=file gopi task show <row> --db …/memory.db
  unlink("…/lb-show/memory.db-shm") = 0
  unlink("…/lb-show/memory.db-wal") = 0
$ ls -la /home/user/workspace/_tmp/lb-show   # after
memory.db
```
(evidence: `evidence/loadbearing-show.strace`, `loadbearing-list.strace`)

**It is not "just a copied WAL set behaving oddly".** The same copied ledger read
by an INDEPENDENT SQLite (python3's `sqlite3`, `file:…?mode=ro`) keeps both
sidecars and returns identical row counts (1355 rows, `PRAGMA journal_mode` =
`wal`). So the deletion is a property of THIS opener, not of reading a copied WAL
set (evidence: `evidence/attrib.sh` transcript).

**Which statement triggers it.** Not a statement of its own: the sequence is the
ledger OPEN/CLOSE itself, and it is identical for the read-only opener and the
write opener. Captured on the read-only path (fd 7 = the `-wal`):

```
fcntl(7, F_GETLK, {F_UNLCK, l_start=128, l_len=1}) = 0
fcntl(7, F_SETLK, {F_WRLCK,  l_start=128, l_len=1}) = 0     # recovery/write attempt on the -wal
ftruncate(7, 3) = 0                                          # truncate the WAL header
unlink("…/memory.db-shm") = 0
unlink("…/memory.db-wal") = 0
```
`PRAGMA journal_mode` read back ON the read-only handle: `wal` — i.e. the reader
did not ask SQLite to leave WAL; it read a WAL database and the close removed the
sidecars. A benign reading of this is "a last connection closing a WAL it may
consolidate"; the destructive reading is the one that had to be tested.

**Is the WAL load-bearing when this happens?** Measured with a purpose-built case
(a writer that COMMITS and is then SIGKILLed, so the commit exists only in the
`-wal`: marker absent from the main db, present in the 90 KB `-wal`): after the
verb, the sidecars are gone AND the marker bytes are found in the main db — the
commit was consolidated before the WAL was removed (evidence:
`evidence/loadbearing-*.out`, `decisive.sh`). On the one construction tested,
**no committed row was lost**. That is the whole of what the evidence supports:

> Deleting the sidecars is real, reproduced and gopi-caused, and it is destructive
> in principle (a `-wal` is the only copy of a commit until it is checkpointed);
> on every construction measured here the commit survived the consolidation. The
> mechanism that decides whether consolidation runs — and whether an abrupt
> termination between `ftruncate` and `unlink` can lose a frame — was NOT pinned.
> Conclusion deliberately limited to the实测.

**A/B on the operator's ledger copy** (one writer process holding
`BEGIN IMMEDIATE`, same row, back to back; old = the installed binary built
10:09, new = this branch; evidence: `evidence/ab-old-vs-new.log`, `ab-*.out`):

| verb | binary | rc | elapsed | ledger after |
|------|--------|----|---------|--------------|
| `task show <row>` | old | 1 | **43.58 s** | `memory.db` only (sidecars DELETED) |
| `task show <row>` | new | 1 | **0.02 s** | `memory.db -shm -wal` (INTACT) |
| `task verdicts` | old | 0 | **43.65 s** | `memory.db` only (DELETED) |
| `task verdicts` | new | 0 | **0.02 s** | `memory.db -shm -wal` (INTACT) |
| `task list` | old / new | 0 | ~43 s | DELETED in both (control: not wired) |

Both verbs printed byte-identical output in both arms (`615 reframe record(s)
across 429 task(s)`; the `task show` arms both refused the same absent row), so
the faster answer is also the SAME answer — the fix changes the access path, not
the reading. The two properties are one: taking the write lock on every open is
what costs the 43 s, and the same open is what removes the sidecars.

**A stale-binary trap worth recording.** The first A/B used the installed
`bin/gopi` as "new" and therefore showed no difference for `task verdicts`:
`strings bin/gopi | grep -c "read-only ledger open"` = 0 — the installed binary
was built 10:09, i.e. BEFORE the read-only opener landed (17:59, commit
`81fc8fdba`). Any earlier A/B on this defect that compared the installed binary
against itself would have measured nothing. The measurement above uses a binary
built from this branch.

### 3. Tests

`internal/tools/ledger_readonly_faces_test.go` (new):
- `TestPureReadFacesServeTheSameReadingUnderAWriterHold` — per converted face, a
  separate connection holds the ledger's write lock (proved REAL every run: a
  no-wait concurrent writer must be refused), and each face must (a) finish under
  a 5 s budget — vs the 15 s busy timeout and the 43 s observed stall — and
  (b) answer the SAME reading the write-handle oracle produced. Measured: all 8
  faces in 0.5–1.1 ms.
- `TestPureReadFacesRefuseWrites` — the handle these faces read through rejects an
  `UPDATE`.

`internal/tools/ledger_readonly_walsidecar_test.go` (new):
- `TestReadOnlyOpenPreservesTheSnapshotSidecars` — a ledger copy that carries
  sidecars must still carry them after a read-only open/close, and must answer the
  same row set. Device-backed fixture (a `/dev/shm` ledger carries no `-wal`, so
  the fixture puts the ledger in WAL explicitly — measured: without that line the
  test skipped at "1 files copied").

## Verification

```
$ go build ./cmd/agent/                         # OK (CGO_ENABLED=0)
$ go test ./internal/tools/ -run 'TestPureReadFaces' -count=1
ok  	github.com/the operator/gopi/internal/tools	2.943s   # 8 faces, 0.5–1.1ms each under the hold
$ go test ./internal/tools/ -run 'TestReadOnlyOpenPreservesTheSnapshotSidecars' -count=1
ok  	github.com/the operator/gopi/internal/tools	1.928s
$ make test-budget PKGS='./internal/tools/'     # see the session record / task report
```
Pre-existing guards re-run green: `TestTaskShowDoesNotWaitOnAWriterHold`,
`TestTaskShowReadsThroughAReadOnlyHandle` (the previous run's regressions).

## Evidence

Session dir `…/.gopi/sessions/01a13490-3987-7a4a-a892-bc916f683bc6/evidence/`:

| file | what it is |
|------|-----------|
| `ab-old-vs-new.log`, `ab-{old,new}-*.out`, `ab-*.holder` | the A/B: rc, elapsed, ledger listing before/after, for each verb and binary |
| `loadbearing-{show,list}.strace` | the unlink/`ftruncate`/`fcntl` syscalls, load-bearing-WAL case |
| `loadbearing-{show,list}.out` | the verbs' own output (rc recorded alongside) |
| `attrib.sh`, `walrep2.sh`, `loadbearing.sh`, `decisive.sh` | the probe scripts as run |
| `remaining-pure-read-faces.txt` | the mechanical sweep's classified output |

## Not Done

1. **~80 further pure-read sites are NOT wired.** A mechanical sweep (a Go
   function-span scan: "does this function's own body contain a write statement or
   a writer call?") classifies **80 call sites as pure reads and 49 as writers**;
   the full list, with the enclosing function for each, is
   `evidence/remaining-pure-read-faces.txt`. Only the 8 unimpeachable ones were
   converted in this session. The reason is not the edit (it is a word per site)
   but the JUDGEMENT: a regex cannot see that a helper three calls deep writes
   (e.g. `importTaskOpenDB`, `OpenTasksDBForSweep` return handles their callers
   use to write), so each remaining face needs a human/LLM read before its opener
   is changed. That is one face per few minutes — a follow-up run's work, not this
   one's.
2. **Legacy (pre-tasks) ledger semantics for read faces are unverified.**
   `openTasksDB` heals a default-path ledger that predates the tasks tables
   (`migrateDefaultTriggers` → `ensureTasksSchema`); `openTasksDBReadOnly` cannot,
   because a `mode=ro` connection cannot create a table. For a read verb this is
   acceptable BY DESIGN and is the previous run's accepted behaviour for
   `task show` (that arm was measured there: a fresh profile still rolls up
   because the missing-file arm falls back to the write opener). But no test here
   asserts it, so a read verb pointed at a legacy DEFAULT-path ledger with data is
   a behaviour this report does not claim to have verified.
3. **The `-wal` deletion itself is NOT fixed, only attributed.** The remaining
   question — which SQLite operation decides whether the close consolidates and
   removes the sidecars, and whether an abrupt termination inside that window can
   lose a committed frame — is open. It is filed as a bug row (Rule 41) rather
   than patched here, because "make the sidecars survive" is not obviously the
   desired behaviour: SQLite's own WAL lifecycle is legitimate, and a change here
   must not defeat `enableWAL`'s checkpointing.
