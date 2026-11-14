package tools

import (
	"strconv"
	"strings"
)

// LedgerDSN returns the database/sql DSN a NON-production opener (a test
// fixture, a diagnostic tool) must use to open a gopi ledger with the SAME
// connection contract production applies to its own ledger.
//
// Production opens the ledger through openDB (see its doc comment) and puts it
// in WAL journal mode through enableWAL. The contract has three parts, and a
// bare `sql.Open("sqlite", path)` DSN carries NONE of them:
//
//  1. a busy TIMEOUT — so an opener QUEUES behind a live writer (a resident
//     merger/worker daemon, or a sibling fixture process) instead of failing
//     instantly with `database is locked (5) (SQLITE_BUSY)`;
//  2. WAL journal mode — the mode gopi itself puts its ledger in, so a reader
//     gets a consistent snapshot while the resident daemon writes;
//  3. (openDB only, not expressible in a DSN) a single pooled connection.
//
// It exists because the missing timeout is not a "flake": it is a defect whose
// hit rate is load-dependent. Measured twice, from two directions:
//
//   - an ad-hoc READER racing the retired merger's writes got an immediate
//     `database is locked (5) (SQLITE_BUSY)` SELECT in 2 of 3 runs of
//     TestKilledWorkerDaemon_MergerTickRecoversTheStalledRound against a
//     control clone at the same commit — so the failure is neither the test's
//     logic nor load-specific to one tree;
//   - cmd/agent's zombie-dispose fixture (the row it seeds is written by
//     openRawTasksDB, a bare-DSN handle) lost the race against the very daemon
//     the test starts — `backdate t-zd-confirmed: database is locked (5)
//     (SQLITE_BUSY)` — reported 7 runs out of 8.
//
// Rule 14: ONE definition, referenced by every caller — the literal DSN must
// never be re-typed at a call site. The timeout VALUE is never re-typed either:
// it is dbBusyTimeoutMSec, the same constant openDB uses, so retuning
// production retunes every test fixture with it (Rule 1a/13).
func LedgerDSN(path string) string {
	return "file:" + path + "?" + ledgerPragmas()
}

// LedgerMemoryDSN returns the DSN for the ONE class of ledger opener this
// contract does NOT describe: an IN-MEMORY database (`:memory:`), which is not a
// ledger file at all.
//
// WHY THE CLASS IS EXEMPT, exactly — two of the contract's three parts are
// physically inexpressible on an in-memory database, not merely unnecessary:
//
//  1. a busy TIMEOUT protects a caller from a live writer of the SAME FILE. An
//     in-memory database is a private, single-connection region of this process
//     (measured: `PRAGMA journal_mode=WAL` on `:memory:` returns `memory`, i.e.
//     SQLite REFUSES the mode — an in-memory database cannot hold the WAL — so a
//     DSN demanding WAL cannot exist for it);
//  2. WAL journal mode, same refusal, same measurement.
//
// So no DSN spelling can carry the contract here, and an in-memory opener can
// never race the resident daemon the contract exists for: it shares no path with
// any other process. Fabricating a `file:…?mode=memory`+WAL spelling to satisfy a
// checker would be a DSN that lies about what it opens — the opposite of what
// the contract is for.
//
// WHY A NAMED HELPER RATHER THAN A LITERAL THE CHECKER TOLERATES (Rule 13/14).
// `:memory:` is a DSN VALUE, and a DSN value re-typed at each call site is
// exactly the shape Rule 14 forbids: the next editor of a second in-memory
// fixture either re-types the literal (drift) or, worse, spells a HALF-contract —
// `":memory:?_pragma=busy_timeout(...)"`, which costs parsing and buys nothing
// on a private handle. Routing every in-memory opener through this ONE function
// keeps the gate's predicate a SINGLE question ("is the DSN argument built by a
// ledger DSN helper?") instead of a question plus a list of literals it must
// recognise by value — a value-based exemption is a predicate widened by
// content, and it cannot tell a deliberate in-memory fixture from a ledger
// opener whose path variable happens to hold the same string.
//
// The consumer that proves the class is real: internal/tools's fts5 boundary
// invariant (memory_split_invariant_test.go) asks a bare `unicode61` tokenizer a
// question on a throwaway table — no ledger file, no daemon, no schema (the
// measured refusal of WAL above is from that very rig).
func LedgerMemoryDSN() string {
	return ledgerMemoryDSN
}

// ledgerMemoryDSN is the ONE rendering of the in-memory DSN (Rule 2/14): the
// value is stated here and nowhere else, so the single definition is also the
// single place that would have to change if SQLite's in-memory spelling ever
// did. It is deliberately NOT `file:…?mode=memory` — that spelling opens a
// NAMED in-memory database shared between connections of the same process,
// which is a different object with different visibility semantics.
const ledgerMemoryDSN = ":memory:"

// LedgerReadOnlyDSN returns the DSN for a NON-production opener that must only
// READ a ledger (a measurement probe, an artifact verification): production's
// own busy timeout, plus the read-only guards (mode=ro and query_only) that make
// it impossible for the probe to write the ledger it reads.
//
// It exists separately from LedgerDSN because a scratch home and a live ledger
// need different open modes, and re-rendering the DSN at the probe's call site
// is exactly the re-typing Rule 13/14 forbid.
//
// ONLY the timeout is shared from production's pragma list. journal_mode(WAL)
// and synchronous(NORMAL) are WRITE-side pragmas: on a mode=ro connection SQLite
// refuses them, and because go-sqlite delivers pragmas with the first statement,
// the refusal surfaces on the CALLER's query — measured as
// `select count(*) from tasks: attempt to write a readonly database (8)` in
// LedgerBackupVerify (six tests red) when this helper briefly carried them. A
// reader does not set the journal mode; it reads whatever the writer chose.
func LedgerReadOnlyDSN(path string) string {
	return ledgerReadOnlyDSN(path, dbBusyTimeoutMSec)
}

// LedgerReadOnlyDSNIn returns the SAME read-only shape as LedgerReadOnlyDSN
// with an explicit busy timeout, for the ONE caller whose timeout is not
// production's: the typing-time memory prefetch (cmd/agent/chat_prefetch.go),
// whose lookup must GIVE UP in well under a second. Production's 15s is
// deliberately far too long for a keystroke path — it is the right contract for
// a foreground writer, which should queue rather than fail.
//
// It is a parameterisation of the ONE read-only DSN rather than a second DSN
// spelling, so `mode=ro` and `query_only(true)` cannot hold in one shape and be
// lost in the other; the timeout value itself is never re-typed at a call site
// (Rule 1a/13). A timeoutMS <= 0 is refused by the caller's own constant
// instead of being silently replaced — the value IS the give-up contract.
func LedgerReadOnlyDSNIn(path string, timeoutMS int) string {
	return ledgerReadOnlyDSN(path, timeoutMS)
}

// ledgerReadOnlyDSN is the ONE renderer of the read-only DSN (Rule 2).
func ledgerReadOnlyDSN(path string, timeoutMS int) string {
	return "file:" + path + "?mode=ro&_pragma=busy_timeout(" + strconv.Itoa(timeoutMS) + ")" +
		"&_pragma=query_only(true)"
}

// LedgerBusyErr reports whether err is SQLite DECLINING because the ledger is
// held by another connection — SQLITE_BUSY / SQLITE_LOCKED / "database table is
// locked" — as opposed to a structural failure.
//
// It is the ONE classifier for that question (Rule 14), because two consumers
// now act on the answer and their actions must not disagree:
//
//   - a probe that RETRIES: the read-only keystroke path
//     (cmd/agent/chat_prefetch.go) retries a locked lookup a bounded number of
//     times inside its own deadline;
//   - a probe that STOPS: the AND-subset recall ladder
//     (internal/tools/memory_ladder.go) abandons its walk, because a lock is a
//     fact about the LEDGER and not evidence about the subset it was counting.
//
// MEASURED reason the second one matters (peer1 2026-10-07): with the walk
// treating every probe error as "zero matches", a lock made the ladder ask up
// to memsearchMaxCombos (1023) more times at a 250ms busy timeout each, so the
// prefetch reported `prefetch gave up after 1500ms (query): context deadline
// exceeded` where the honest answer was the SQLITE_BUSY refusal it had already
// received in 250ms — an outcome the keystroke path handles, and a diagnosis it
// lost. The driver renders the code as TEXT (modernc.org/sqlite), so the
// spellings are matched as text; the strings are FTS5/SQLite engine vocabulary,
// not gopi keywords, which is why they are not constants (Rule 1b governs names
// gopi chooses).
func LedgerBusyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "SQLITE_LOCKED")
}

// ledgerNoWaitBusyTimeoutMSec is the busy timeout a NO-WAIT probe disables the
// busy handler to: zero, i.e. an acquisition that cannot be granted is REFUSED
// immediately instead of queueing. It is a named constant (Rule 1b) because it IS
// a timeout value, and it is deliberately the OPPOSITE of dbBusyTimeoutMSec —
// the one case where production's timeout is the wrong answer.
const ledgerNoWaitBusyTimeoutMSec = 0

// ledgerNoWaitPragma returns the HANDLE-level statement that disables the busy
// handler on a connection opened with LedgerDSN, for a probe whose question is
// "is this ledger's write lock free RIGHT NOW?".
//
// WHY IT EXISTS — a probe that inherits production's timeout CANNOT answer that
// question. Measured while the memsync batching probe was being designed
// (internal/tools/memory_sync_assets_batch_test.go): with production's
// busy_timeout the first refusal did not return until the whole hold was over —
// first_ok_at = 7.1s of a 1.5s apply — a number with no relation to the lock, so
// "when does a concurrent writer first get in?" became unmeasurable. Zero is the
// only value at which the probe's result is the LOCK's state instead of the
// timeout's length; the caller reads the refusal off SQLITE_BUSY itself.
//
// WHY IT IS A PRAGMA AND NOT A DSN. The DSN stays the production one, because the
// gate that owns this contract (cmd/agent/ledger_dsn_contract_test.go) recognizes
// exactly the DSN helpers that carry production's contract, and a probe inventing
// a third DSN spelling would be a second, unreviewed answer to "what does a
// ledger opener look like". Deviating on the HANDLE is the channel production
// itself uses for its own timeout (openDB executes `PRAGMA busy_timeout = <ms>`
// rather than expressing it in a DSN), so the deviation is one named statement
// instead of one extra DSN shape — and the value lives here, never re-typed at
// the call site (Rule 13/14).
func ledgerNoWaitPragma() string {
	return "PRAGMA busy_timeout = " + strconv.Itoa(ledgerNoWaitBusyTimeoutMSec)
}

// ledgerReadQueryOnlyPragma is the HANDLE-level statement that makes a ledger
// writer's handle refuse to write, and it is the statement a read-only opener
// re-asserts on its own connection.
//
// It is a named constant rather than a literal at the open site because TWO
// callers must agree on it and their disagreement is silent: the DSN renders
// query_only(true) into the connection string (ledgerReadOnlyDSN) while
// openTasksDBReadOnly re-asserts it on the handle after opening, exactly as
// openDB asserts query_only for its own read-only mode. Retuning one spelling
// and not the other would leave a handle whose read-only guard is a DSN
// convention rather than a measured property — the defect this constant exists
// to prevent (Rule 1b: a value two sites share has one definition).
const ledgerReadQueryOnlyPragma = "PRAGMA query_only = ON"

// ledgerRollbackJournalMode is the journal_mode a rig sets on its OWN holder
// handle when it needs a concurrent writer to be REFUSED rather than served.
// Named (Rule 1b) because it is a value two call sites must agree on, and
// because it is deliberately the OPPOSITE of the mode production chooses.
const ledgerRollbackJournalMode = "DELETE"

// ledgerWALJournalMode is the mode ledgerPragmas asks for and the mode gopi's
// own ledger carries (enableWAL, memory.go). Named (Rule 1b/13) because it is
// compared against a readback in a test and rendered into the DSN, and a value
// re-typed at either site is exactly the drift the const exists to prevent.
const ledgerWALJournalMode = "WAL"

// LedgerRollbackJournalPragma returns the HANDLE-level statement that puts a
// ledger back in SQLite's rollback journal, for a rig whose subject is what a
// reader does while a writer holds the ledger.
//
// WHY IT EXISTS — under WAL, a READER IS NEVER BLOCKED, so the question such a
// rig asks is unanswerable. Verified live by TestLedgerRollbackJournalPragmaIsWhatMakesAHolderRefuse
// (internal/tools): with journal_mode=WAL a `BEGIN EXCLUSIVE` holder leaves a
// concurrent read-only SELECT returning its rows in ~0ms (the reader takes a
// consistent snapshot from the WAL), while the SAME holder under DELETE makes
// that SELECT wait its full busy timeout and then return `database is locked (5)
// (SQLITE_BUSY)`. A rig that wants to observe the refusal must therefore ask for
// the rollback journal explicitly.
//
// WHY IT IS A PRAGMA AND NOT A DSN — the same reason as ledgerNoWaitPragma
// above, and it is the reason this file has one DSN spelling and not three:
// cmd/agent's contract gate recognises exactly the DSN helpers carrying
// production's contract, so a rig inventing its own DSN would be a second,
// unreviewed answer to "what does a ledger opener look like" — and, measured
// 2026-10-12, such a spelling is also invisible to that gate. Deviating on the
// HANDLE is the channel production itself uses for its own timeout (openDB
// executes `PRAGMA busy_timeout = <ms>` rather than expressing it in a DSN), so
// the deviation is one named statement whose value lives here and is never
// re-typed at a call site (Rule 13/14).
func LedgerRollbackJournalPragma() string {
	return "PRAGMA journal_mode = " + ledgerRollbackJournalMode
}

// LedgerRollbackJournalMode returns the mode LedgerRollbackJournalPragma asks
// for and LedgerRollbackJournalDSN carries, so a rig can assert it READ THE
// MODE BACK without re-typing the value at the assertion (Rule 1b/13).
func LedgerRollbackJournalMode() string {
	return ledgerRollbackJournalMode
}

// LedgerRollbackJournalDSN returns the DSN a rig uses when its SUBJECT is what a
// read-only reader does while a writer holds the ledger — production's contract
// (busy timeout, synchronous) with the rollback journal asked for by the FIRST
// connection instead of WAL.
//
// WHY THE RIG NEEDS ITS OWN DSN RATHER THAN A FLIP. The rollback journal can be
// asked for after the fact — `PRAGMA journal_mode = DELETE` on a holder handle —
// but that flip needs EXCLUSIVE access to the file, and it is refused with
// SQLITE_BUSY while ANY other connection to that file is open, even an idle,
// pooled one that is doing nothing. Measured (peer1, 2026-10-15, task
// 01a13db4-b931-7230-9da3-83566f5b06dc), on the exact rig sequence another
// process's connection pool leaves behind:
//
//   - 300/300 flips succeeded when the fixture held the file alone;
//   - the same flip was refused in 444µs with ONE sibling `sql.DB` merely OPEN
//     on the same file, and in 15µs with that sibling mid-read.
//
// So a post-hoc flip is a race against connections the fixture does not own,
// which is why the failure appears in a full-package run and never under
// `-run X -count=3`. A file whose FIRST connection asks for the rollback journal
// is CREATED in that mode (measured: `PRAGMA journal_mode` reads back "delete"
// on the fresh file), so no flip — and no exclusive moment — is ever needed:
// the same rig scenario then takes its `BEGIN EXCLUSIVE` in 258µs with the
// sibling still attached.
//
// WHY IT IS A DSN HELPER AND NOT A SECOND SPELLING AT THE CALL SITE. The class
// is a ledger opener, so it goes through the same reviewed channel as every
// other one: cmd/agent's contract gate (ledger_dsn_contract_test.go) admits the
// DSN helpers by NAME, and a rig that re-typed `file:…?journal_mode(DELETE)` at
// its own call site would be exactly the unreviewed second answer that gate
// exists to prevent (Rule 13/14 — one definition, referenced).
//
// WHICH RIGS MAY USE IT. Only one whose subject is the refusal itself. The mode
// is a deliberate deviation from production's WAL, so it must not reach a fixture
// that measures production's reader behaviour (e.g. the warm handle's
// journal_mode readback): there, WAL is the fact under test.
func LedgerRollbackJournalDSN(path string) string {
	return "file:" + path + "?" + ledgerPragmasIn(ledgerRollbackJournalMode)
}

// ledgerPragmas renders the pragma list every ledger DSN shares — the ONE place
// the timeout value is rendered, so it is never re-typed at a call site (Rule
// 1a/13/14). The value is dbBusyTimeoutMSec, the same constant openDB applies to
// production's own handle.
//
// The three pragmas are production's own connection contract, in the order the
// production path applies them (openDB → enableWAL):
//
//  1. busy_timeout — queue behind a live writer instead of failing with
//     SQLITE_BUSY;
//  2. journal_mode(WAL) — the mode gopi puts its ledger in;
//  3. synchronous(NORMAL) — what enableWAL applies next, and the pragma that
//     matters on this host: under the default FULL every COMMIT fsyncs, and a
//     single fsync measured 0.7-1.3 s/op here at /proc/pressure/io full > 90%
//     (2026-09-16). A fixture that seeds a handful of rows then closes was
//     measured parked in `tx.Commit() → xfsync` (memory.go:595) for minutes.
//     NORMAL under WAL keeps the checkpoint-time sync and drops the per-commit
//     one — the same tradeoff production's enableWAL doc comment already accepts.
func ledgerPragmas() string {
	return ledgerPragmasIn(ledgerWALJournalMode)
}

// ledgerPragmasIn renders the SAME pragma list with the journal mode as a
// parameter, so the one rig-shaped deviation (LedgerRollbackJournalDSN) shares
// the timeout and the synchronous mode with production's contract instead of
// re-deriving them (Rule 2: the timeout value stays rendered in exactly one
// place). Everything but the journal mode is production's own connection
// contract; see ledgerPragmas for the account of each.
func ledgerPragmasIn(journalMode string) string {
	return "_pragma=busy_timeout(" + strconv.Itoa(dbBusyTimeoutMSec) + ")" +
		"&_pragma=journal_mode(" + journalMode + ")" +
		"&_pragma=synchronous(" + ledgerSyncMode + ")"
}

// ledgerSyncMode is the synchronous= value every ledger DSN states — the SAME
// mode enableWAL applies to production's handle (memory.go). Named (Rule 1b): the
// two paths must not drift, and a re-typed string at a call site is the defect.
const ledgerSyncMode = "NORMAL"

// A note for the next reader (measured, 2026-09-26 — this is why the driver
// argument stays a "sqlite" LITERAL at every open site, and why that is not the
// Rule 13 violation it looks like). Replacing it with a package const
// (ledgerDriverName) turned the contract gate in cmd/agent BLIND: the gate
// decides whether an opener is its business by comparing the driver argument to
// the sqlite driver name, and ledgerIsSQLiteDriverArg matches a *string literal*
// only — so `sql.Open(ledgerDriverName, dbPath)` and
// `sql.Open(ledgerDriverName, LedgerDSN(p))` become the same shape to it, and a
// re-introduced bare DSN at such a site passes unnoticed. Measured: with the
// const in place, restoring a bare DSN on one site left
// TestLedgerOpenersUseProductionDSNContract GREEN — the non-vacuity proof the
// whole rule rests on. The literal is therefore load-bearing INSTRUMENTATION: it
// is the observable the gate reads, not a value free to be centralised.

// ledgerSyncModeWorkingFS is the synchronous= value the FIRST-OPEN schema build
// applies to its OWN connection when the ledger is provably working data on a
// memory filesystem (firstOpenSyncMode, memory.go).
//
// It is not a durability setting, because there is no device to be durable to:
// the schema is built from nothing into a fresh file on tmpfs/ramfs, so the
// commits it makes cannot survive a reboot whether or not they are synced, and a
// sync there is pure cost (measured: 0.5-2.2s per schema phase on ext4's shared
// journal queue vs 9-10ms for the WHOLE first open on tmpfs —
// internal/tools/firstopen_probe_test.go). Every other ledger — the operator's
// own, any file on a real device, any path whose filesystem cannot be measured —
// keeps ledgerSyncMode (NORMAL), i.e. production durability is untouched.
const ledgerSyncModeWorkingFS = "OFF"

// ledgerSyncModeRegressed is the synchronous= level the first-open regression
// reverted TO — SQLite's default, which is what the durable path used before the
// t-1788361069 fix. It is named (Rule 1b) because the guard's red proof has to
// reproduce that state from a shared source: a re-typed "FULL" in the regression
// arm could drift from the level the guard reads back and quietly disarm the
// proof. (The guard reaches that arm by replaying the production build sequence
// on its own handle — firstOpenBuildLedger, firstopen_pragma_guard_test.go —
// because synchronous is per-connection and the public entry point closes its
// handle before returning.)
const ledgerSyncModeRegressed = "FULL"
