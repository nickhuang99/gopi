<!-- doc-class: normative -->
# A read-only `gopi memory` verb must not create a knowledge base

**Status:** delivered (2026-11-06, epic `01a1ac96`).
**Mechanism:** `cmd/agent/cli_memory_ledger_guard.go` (guard) +
`cmd/agent/cli_memory_ledger_guard_test.go` (regression table).
**Pages:** `gopi memory -h` (contract paragraph), `man/gopi-memory.md`.

## The defect, measured

Every read-only memory subcommand resolved an explicitly named `--db` through
the ordinary ledger opener, which CREATES and initialises the file when it is
absent. So a typo'd path was indistinguishable from an empty knowledge base,
and the read left a 128 KiB persistent artifact behind that no reclaim path
owns (Rule 35).

Before the fix — `bin/` at the parent commit, probe script
`~/.gopi/sessions/01a1ac96-ff6a-7586-86f9-89265db00a3b/probe-pre.sh`,
raw output in `pre-fix-behaviour.txt` beside it:

| invocation | exit | what it said | the file afterwards |
|---|---|---|---|
| `memory search probe --db <missing>` | 0 | `No distilled memory found for this query.` | 131072 bytes, created |
| `memory get 5 --db <missing>` | 1 | `memory_get: sql: no rows in result set` | 131072 bytes, created |
| `memory list --limit 2 --db <missing>` | 0 | `knowledge base is empty` | 131072 bytes, created |
| `memory provenance --db <missing>` | 0 | `total rows: 0` | 131072 bytes, created |
| `memory feedback --list --db <missing>` | 0 | `No feedback recorded yet.` | 131072 bytes, created |
| `memory feedback --suggest-cold --db <missing>` | 0 | (no suggestions) | 131072 bytes, created |
| `memory feedback --recompute --db <missing>` | 0 | (nothing recomputed) | 131072 bytes, created |
| `memory converge-keywords --dry-run --db <missing>` | 0 | (nothing to converge) | 131072 bytes, created |
| `memory backfill-tokens --dry-run --db <missing>` | 2 | `flag provided but not defined: -db` | not created — the documented flag did not exist |

Two different wrong answers for one root cause (`search` said "nothing
distilled on this topic yet — store what you learned", which is an instruction
to write into a file the user never meant to name), and the mirror-image hazard
on the writing side: a writer that silently fell back to the DEFAULT ledger
would put the row somewhere the caller never named.

## The contract now enforced

**A `--db PATH` that does not exist is NOT an empty knowledge base.**

- **Read-only verbs refuse it.** `search`, `get`, `list`, the reporting modes of
  `provenance` (`shape` / `counts` / `latent`), `feedback --list` /
  `--suggest-cold` / `--recompute`, `converge-keywords --dry-run`,
  `backfill-tokens --dry-run`. Exit non-zero; the message names the path the
  user wrote, the path the verb would have used silently, and the ledger that is
  ACTIVE (with whether that one exists); no file is created.
- **Writing verbs keep the ability to create, and SAY they did.** `store`,
  `update`, `delete`, `feedback <id>` with a rating, `provenance --backfill` /
  `--reclassify`, `converge-keywords` and `backfill-tokens` without `--dry-run`.
  After a successful write they print
  `gopi <verb>: created a new knowledge base at <path> (it did not exist before this run)`
  to stderr — stdout stays parseable — so "empty" and "brand new" are never the
  same sentence (Rule 15). The notice is computed BEFORE the open (the only
  moment the absence is observable) and printed only AFTER it succeeded, so a
  run that failed cannot claim it created anything.
- **`capture-stats` opens no ledger at all** and refuses `--db` by name.
- **`backfill-tokens --db` now EXISTS.** The group page had advertised
  `gopi memory backfill-tokens [--dry-run] [--db PATH]` while the parser refused
  the flag (`flag provided but not defined: -db`, exit 2). The flag is admitted
  and threaded through the same resolution the siblings use.

`empty != new` is the whole point: a verb that CREATES a ledger is fine, a verb
that creates one while reporting "there is nothing here" is not.

## What was deliberately NOT changed

The resolution ranking (`tools.MemoryDBPath`, `config.ResolveLedger`,
`tools.OpenMemoryLedger`) is untouched: `CONFIG.md` registers it, other callers
legitimately create a ledger, and changing the ranking to fix a CLI message
would trade a visible defect for an invisible one. The guard decides only what
the CLI does when the caller EXPLICITLY named a path and that file is absent.

No new persistent dependency (Rule 35): no sweep, no hook, no config key. The
absent-path question is asked of the filesystem at the seam, with `os.Stat` and
`fs.ErrNotExist` distinguished from every other failure — a path that could not
be INSPECTED is refused with its own sentence, never folded into "absent" or
"fine".

## The test, and why each arm is needed

`./cmd/agent/cli_memory_ledger_guard_test.go`:

| arm | asserts | a weaker fix it catches |
|---|---|---|
| `TestMemoryReadOnlyRefusesAbsentExplicitDB` | per read-only verb: exit≠0, message names the given path AND the active ledger, `os.Stat` shows no file | a guard that exits 0 with a warning; one that prints a generic `no such file` answering neither question; one that refuses but leaves the created file |
| `TestMemoryWriterCreatesAndSaysSo` | a writer creates the file AND says so; the notice does not repeat on the second write to the same ledger | a "fix" that refuses every `--db`; a notice that is a constant rather than a statement about THIS run |
| `TestMemoryReadOnlyAcceptsExistingExplicitDB` | every read-only verb still answers normally against a real ledger | the fix that refuses all `--db` and looks green on the two arms above |
| `TestMemoryCaptureStatsRefusesDBAndCreatesNothing` | the one read-only verb outside the shared table refuses `--db` and creates nothing | a refactor routing `capture-stats` through the ledger opener |
| `TestMemoryLedgerGuardUsesTheGroupPageVocabulary` | the page carries the same three sentences | a guard and a page drifting into two descriptions of one mechanism (Rule 13) |

**Red proof.** The three behavioural arms run against the PRE-fix binary
(`git archive HEAD | tar -x` of the parent commit, built to
`~/.gopi/sessions/01a1ac96-ff6a-7586-86f9-89265db00a3b/gopi-prefix`, driven via
`GOPI_BIN=`) fail 10 of 10 read-only subtests; against the fixed binary the
whole selection is green. The measurement is recorded in the run's report.

## Verification (this change)

```
Build:  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/        → exit 0; go vet ./cmd/agent/ clean
Tests:  make test-budget PKGS='./cmd/agent'                     → see the run's report for the stream
Ran:    ./bin/gopi memory list --limit 2 --db /tmp/nonexistent-gopi-xyz/x.db
Result: exit 1; stderr begins
        "gopi memory list: --db /tmp/nonexistent-gopi-xyz/x.db does not exist — this verb only
         READS the knowledge base, so it will not create that file. …"
        and `ls /tmp/nonexistent-gopi-xyz` → "No such file or directory" (nothing created).
Ran:    probe-pre.sh (all ten read-only verbs, each with a missing --db)
Result: every read-only verb exit 1, "ls: cannot access …" for all ten files;
        the writer control exits 0, creates 131072 bytes, and prints
        "gopi memory store: created a new knowledge base at … (it did not exist before this run)".
        Raw: ~/.gopi/sessions/01a1ac96-ff6a-7586-86f9-89265db00a3b/post-fix-behaviour.txt
```

## Reusing this pattern

The shape generalises to any CLI seam that opens a SQLite file: **ask
`os.Stat` at the seam, refuse `fs.ErrNotExist` on read paths, announce creation
on write paths, and print the resolved path of the DEFAULT target in every
refusal** — a refusal that names only what the user typed leaves the actual
target invisible, which is the mirror-image bug this guard exists to prevent.
