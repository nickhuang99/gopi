package tools

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ledger_read_guard.go — the rule a PURE-READ ledger lookup must obey, in one
// place.
//
// THE RULE: a lookup that answers "which ledger rows name this branch?" must
// never CREATE its subject. An absent ledger has no rows, so it names no
// owner — that is the empty answer, not a read failure.
//
// WHY (measured 2026-09-26 — the root cause of cmd/agent's 10-minute package
// budget, and of the merge pass's per-branch cost):
//
// TaskForBranch's own header promises "a PURE READ of the ledger: the SQL is a
// single SELECT, the whole scan runs inside one read transaction, and no
// statement in this file writes". The promise was broken one call down, by its
// own opener: openTasksDB initializes a MISSING file (ensureKnowledgeBase →
// initKnowledgeBase → ensureSchema/ensureTasksSchema). So answering "nobody
// owns this branch" cost a FULL KNOWLEDGE-BASE FIRST OPEN — the schema build,
// ~15 DDL statements whose commits each pay the filesystem's durability cost.
//
// Measured for the identical work on this host, with
// `gopi task list --db <fresh path>` (whose open path is the same one):
//
//	550 ms   fresh ledger on ext4 (device 2052, the operator's /home)
//	 88 ms   fresh ledger in /dev/shm
//	  9 ms   that same ext4 ledger, opened again once built
//
// and 550 ms is the IDLE figure. The first-open cost is a SHARED device
// journal commit (internal/tools/firstOpenSyncMode's own measurement: the sync
// parks in jbd2_log_wait_commit, a queue every writer on the device shares), so
// under a live merger + worker it stretches by orders of magnitude. A merge
// pass reaches this once per branch per pass, which is what turns a merge-sweep
// test into a 10-minute trip.
//
// The cost was never the point, though: a read that creates the file it reads
// has written the one artifact tests are forbidden to touch (Rule 32 — the
// ledger is APPEND-ONLY, so a row written there is permanent), and on the
// operator's own ledger it takes the WRITE lock to publish a schema nobody
// asked for, contending with the resident merger for the very file the lookup
// came to read.
//
// WHY THE ANSWER IS THE EMPTY ONE, NOT A REFUSAL: `Readable: false` means "I
// could not look" (Rule 15) and it PARKS the branch — mergeLiveOwnerParks
// treats an unreadable ledger as a reason not to merge. Rendering an absent
// ledger that way would freeze the merge queue on any host whose ledger simply
// has not been built yet, and it would misreport the fact: there is no ledger,
// therefore no row names the branch, and that IS the answer. Refusals stay
// reserved for the case they describe — a file that exists and cannot be read
// as a ledger.
//
// The rule is a FUNCTION, not a caller-supplied flag: a bool argument
// defaulting to false at one call site is a bypass waiting to be written, and
// this file is the ONE place the decision can change.
//
// It deliberately does not touch the WRITE openers. A ledger that is absent is
// still created by the paths whose contract is creation — `gopi task submit`,
// the worker's writeback, a fixture seeding rows, a peer import — because
// creating a ledger is exactly what those are for.

// ledgerMissingForRead reports whether a PURE-READ lookup must answer from an
// absent ledger instead of opening (and thereby building) it.
//
// An empty path is the "no ledger named" case and is treated as absent: a
// caller that named no ledger must not cause one to appear.
//
// For a real path the decision is EXISTENCE plus ONE more question: could this
// path have been read at all? A ledger whose parent directory exists is simply
// not built yet — the empty answer is the truth, and the reader must not build
// it. A path that CANNOT name a ledger (its parent is a regular file, or a
// directory that does not exist) is a different fact entirely: "I could not
// look" (Rule 15), and the caller must fall through to the ordinary opener so
// the real error surfaces. Measured 2026-09-26 — an early cut of this guard
// answered the empty result for BOTH, which turned an unopenable ledger into
// "no owner" and was caught by the then-live for-branch CLI test (the verb is
// retired as of 2026-10-30, task 01a18799; the same arm is asserted today by
// TestTaskVerbShowRunsAsAdvertised through `show --branch`: exit 4 where the
// contract wants 1, "never the no-owner answer").
//
// The decision is made on the FILESYSTEM, never on a spelling: a relative path,
// a symlink or a sandboxed profile all resolve to the same answer (the same
// "measure the property, not the name" discipline Rule 22 applies to temp
// roots). An ABSOLUTE path is not a special case — spelling it absolutely is
// not evidence that it can be read, which is exactly how the first cut of this
// guard got the blocker case wrong.
func ledgerMissingForRead(path string) bool {
	if path == "" {
		return true
	}
	if fileExists(path) {
		return false
	}
	return ledgerParentIsNotYetADirectory(path)
}

// ledgerParentIsNotYetADirectory decides the ONLY ambiguous case: the ledger
// itself does not exist, so what about where it would live?
//
//	parent is an existing directory → "not built yet": the empty answer, and
//	  the reader must not build it (the rule this file exists for).
//	parent does not exist YET       → also "not built yet". Answering the empty
//	  result is both truthful and the SAFER direction here: falling through
//	  would visit an opener whose job is to create, so a read path could mint a
//	  directory TREE on its way to a ledger nobody asked for.
//	parent is a regular FILE        → the path can name no ledger at all:
//	  "I could not look" (Rule 15), so the caller must fall through to the
//	  ordinary opener and let its error surface. Measured 2026-09-26 — the
//	  first cut answered the empty result here, turning an unopenable ledger
//	  into "no owner"; the then-live for-branch CLI test caught it (that verb
//	  is retired 2026-10-30, task 01a18799; TestTaskVerbStatusRunsAsAdvertised
//	  asserts the same arm through `status --branch`) — exit 4 where the
//	  contract wants 1, "never the no-owner answer".
//	parent exists but cannot be stat'd for another reason (permissions, ELOOP)
//	  → NOT the empty answer either: "I could not look" is exactly what that
//	  is, and the opener's error is the honest report.
//
// Only fs.ErrNotExist earns the empty answer; every other stat failure is a
// refusal, never a silent "nobody owns this branch" (Rule 15).
func ledgerParentIsNotYetADirectory(path string) bool {
	dir := filepath.Dir(path)
	if dir == "" {
		return true
	}
	info, err := os.Stat(dir)
	if err != nil {
		return errors.Is(err, fs.ErrNotExist)
	}
	return info.IsDir()
}

// TaskForBranchReadOnlyLookupBudget is the ceiling a lookup with NOTHING to read
// is allowed — the value every caller's guard measures against, in ONE place
// (Rule 14), because internal/tools' own test and cmd/agent's merge-pass test
// both assert it and two copies would drift apart silently.
//
// It is not a tuned threshold: a knowledge-base first open is measured at 839 ms
// MEAN / 2211 ms MAX on this host's ext4 while a live merger and worker run
// (18 ms / 22 ms for the same work in /dev/shm) — so a schema build cannot fit
// inside this budget even on an unloaded device, while the file-existence check
// that replaces it costs microseconds. That gap is what makes the guard a
// red-proof on a loaded machine rather than a timing race.
//
// The number is deliberately far below the cheapest observed build rather than
// just below it: a budget tuned to the mean would pass on a lucky run and turn
// the regression test into a coin flip.
const TaskForBranchReadOnlyLookupBudget = 150 * time.Millisecond
