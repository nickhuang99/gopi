package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/nickhuang99/gopi/internal/tasksource"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/agencyscan"
	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/pushfence"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/runend"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/signalrec"
	"github.com/nickhuang99/gopi/internal/taskreport"
	"github.com/nickhuang99/gopi/internal/tasktags"
	"github.com/nickhuang99/gopi/internal/taskverbs"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
	"github.com/nickhuang99/gopi/internal/worktreerelease"
)

// Named constants for the task subcommand family (Rule 1 — no magic
// numbers at call sites).
const (
	// mergerOnceDoneLine is what a `--once` merger prints when its single tick
	// completes — the E2E suite's proof that a pass RAN and returned. It is
	// exported-by-constant from production (Rule 1b/14) because the tests that
	// wait for it were spelling it inline, and a rename then leaves them
	// waiting for a line that can never appear: measured 2026-09-17, the
	// singleton E2E still waited for the retired "single pass done" while the
	// binary printed "single tick done", so a perfectly healthy merger was
	// reported as failing to complete its pass.
	mergerOnceDoneLine = "[merger] --once: single tick done"

	// mergerLogBaseName is the file a merger's lifecycle lines are declared in:
	// <gopi data root>/logs/merger.log. It used to be defined by the retired
	// `--daemon` face (the spawn seam redirected the child's stdio there); with
	// the cron form there is ONE merger record per host, so the constant moves
	// here and keeps its exact value — an operator greps for the same path.
	mergerLogBaseName = "merger.log"

	// serveLockFilename is the single-instance lock file (flock
	// LOCK_EX|LOCK_NB) under ~/.gopi/ — a second `gopi task serve` on the
	// same machine must refuse, never double-dispatch.
	serveLockFilename = "serve.lock"

	// taskSubmitRefusesUngroundableID is the ARM SWITCH of the submit-time
	// ground-name refusal (epic 01a12b5c / 01a12b86): the value this CLI face
	// hands to TaskSubmitOptions.RefuseUngroundableID, so
	// `gopi task submit --task-id t-foo` is refused where it stands instead of
	// being accepted and dying at dispatch.
	//
	// WHY IT IS A NAMED CONSTANT (Rule 1) and not a bare `true` at the call
	// site: the arm is a DECISION with a measured precondition (the CLI-driven
	// fixture ids are full uuids — task/01a12b86ebbe7e7ea94481923d97c20d), and
	// TestTaskSubmitRefusesANonUUIDRowIDAtTheCommandLine
	// (cli_tasks_help_surface_basic_test.go) asserts this constant's value
	// through the REAL built binary. The engine keeps defaulting to false for
	// library callers: only this face declares itself, and the dispatch gate
	// keeps its unchanged behaviour as the backstop.
	taskSubmitRefusesUngroundableID = true

	// serveLockFileMode / serveLockDirMode are the lock file/directory
	// permission bits.
	serveLockFileMode = 0o644
	serveLockDirMode  = 0o755

	// serveLockIdentityRetries is how many times acquireServeLock re-opens the
	// lock path after winning the flock and re-checks that the file it locked
	// is still the file the NAME resolves to. flock is attached to an INODE,
	// so a concurrent unlink-then-recreate (2026-09-12: an `rm serve.lock`
	// while a merger ran) leaves the winner holding a lock nobody can see —
	// the next acquirer would win the fresh inode and double-merge. Re-check,
	// retry, and refuse after this many consecutive mismatches.
	serveLockIdentityRetries = 3

	// serveLockIdentityRefusal is the refusal text for that state, named once
	// (Rule 1b: it is compared in tests and rendered to the operator).
	serveLockIdentityRefusal = "the single-instance lock file was replaced while acquiring it (another process unlinked %s) — refusing to start: the flock is bound to an unlinked inode, so a second daemon could take the fresh file and double-merge; remove the stray holder and retry"

	// serveLockStrayHolderRefusal is the refusal for the OTHER half of the
	// same hazard (2026-09-12): the lock PATH is free because a running
	// merge process's flock sits on an UNLINKED copy of it (it keeps
	// merging, unguarded). Winning the fresh file here would double-merge, so
	// the acquisition refuses until that holder restores the lock (its next
	// pass) or is killed. Named once for the same reason.
	//
	// WORDING (epic daemon-machinery-removal, 2026-09-17): the holder is a
	// merge PROCESS, not necessarily a daemon. Under the cron form the holder
	// is a tick that has not exited yet; the lock's job — one merger at a time —
	// is exactly what did not change.
	serveLockStrayHolderRefusal = "another merge process (pid %d) still holds an UNLINKED copy of the single-instance lock %s — its guard is detached, so a second merger must not start (double-merge); that process restores the lock on its next pass, or kill pid %d to clear it"

	// serveLockHeldRefusal is the refusal for the ordinary case: a live merge
	// process holds the lock — under the cron form, a tick that is still running.
	// It is a NAMED value (Rule 1b: every caller matches on it, so a reworded
	// message must not silently turn "one at a time" into "start anyway").
	serveLockHeldRefusal = "another merge process is already running (lock %s held by %s) — refusing double-serve: two merger passes (or a legacy `task serve`) never double-merge"

	// --- the refusal DIAGNOSIS (epic serve-lock-diagnosis-liveness) ---------
	//
	// A pid ALONE is not evidence: a flock lives on the INODE, so the refusal
	// can be raised while the recorded pid names a DEAD process (a duplicated
	// descriptor keeps the flock alive past its owner's death) or while the
	// kernel's table carries no record at all for the file the refusal hit
	// (the lock path was replaced underneath a live holder). The lines below
	// answer the two questions an operator actually has — "is the holder
	// ALIVE?" and "what does the KERNEL say about the inode I was refused
	// on?" — and every one of them is a named constant (Rules 1b/13/14).
	// serveLockStrayHolderPrefix / serveLockKernelRecordsPrefix are the line
	// LABELS — the part an operator greps for and a test matches on — defined
	// once and composed into the rendering formats below, so the label cannot
	// be spelled differently at a second site (Rules 1b/13).
	serveLockStrayHolderPrefix   = "lock-stray-holder: "
	serveLockKernelRecordsPrefix = "lock-kernel-records: "
	// serveLockHolderPrefix labels the line that renders the pid STAMPED into
	// the lock file PAIRED WITH ITS LIVENESS (the rework union's half): the
	// stamp alone is what sent operators hunting a process that was already
	// gone, so the pair is reported as one labelled line of its own.
	serveLockHolderPrefix = "lock-holder: "
	// serveLockLineEnd terminates every refusal-diagnosis line.
	serveLockLineEnd = "\n"

	// serveLockNoStrayHolder is the answer "no process holds an unlinked copy
	// of this path" — reported, never left blank, so "no stray holder" is
	// distinguishable from "the probe could not run".
	serveLockNoStrayHolder = "none (no process holds an unlinked copy of %s)"
	// serveLockStrayHolderFormat names the stray holder AND its LIVENESS. Both
	// halves are one string because the pair is the point: printing the pid
	// without "dead" is the defect this diagnostic exists to remove.
	serveLockStrayHolderFormat = "pid %d on an unlinked copy of %s (%s)"
	// serveLockHolderLive / serveLockHolderDead are the liveness vocabulary,
	// spelled once (Rule 1b).
	serveLockHolderLive = "ALIVE"
	serveLockHolderDead = "DEAD"
	// serveLockKernelRecordsFormat renders the matching /proc/locks records,
	// with the inode they were matched on — the identity half of the report.
	serveLockKernelRecordsFormat = "%d record(s) on the refused inode %d: %s"
	// serveLockKernelNoRecords is the answer "the kernel's table is readable
	// and carries nothing for the refused inode".
	serveLockKernelNoRecords = "none"
	// serveLockKernelUnreadableFormat is the Rule 15 half: a table that cannot
	// be READ is announced as such and never rendered as "no records" (which
	// would read as evidence of a free lock).
	serveLockKernelUnreadableFormat = "UNREADABLE (%v) — no record evidence either way"
	// serveLockKernelNoInodeFormat is the other unprovable half: the acquired
	// descriptor could not be stat'ed, so there is no inode to match on.
	serveLockKernelNoInodeFormat = "UNKNOWN inode (%v) — the refused inode could not be resolved"

	// --- the descriptor- and stamp-based evidence (rework union, 2026-09-23) --
	//
	// The block above is the kernel-table half landed by epic
	// serve-lock-diagnosis-liveness. The values below came from the predecessor
	// branch fix/serve-lock-refusal-diagnosis, which had solved the same epic
	// from the other end: it reads the STAMP and pairs it with its liveness, and
	// joins the kernel table on the descriptor's DEVICE+INODE exactly as the
	// kernel spells it. Both halves answer different questions ("is the holder I
	// named alive?" / "who does the kernel say keeps this flock alive?") and a
	// refusal carries both.
	//
	// serveLockHolderDeadNote is appended to the lock stamp when NO process
	// carries that pid any more — the stamp is a diagnostic, not a holder.
	//
	// Why the refusal must say this (measured twice: c50e7f2a's E2E said "held
	// by pid 1630650" while that pid was gone; the operator's session said the
	// same of pid 1653050): a flock lives on the OPEN FILE DESCRIPTION, so a
	// descriptor copy inherited by a child (os/exec ExtraFiles, a fork) keeps
	// the lock alive after the process that STAMPED the file is dead, and a
	// flock is not invalidated by unlink/replace either. The bare stamp then
	// names a corpse and the operator hunts a process that never existed.
	serveLockHolderDeadNote = " (no such process: the flock outlived its owner — an inherited descriptor copy, or a replaced path)"

	// serveLockProcRoot / serveLockProcLocksPath are the procfs mount and the
	// kernel's POSIX/flock lock table under it. Named, never typed inline
	// (Rules 1b/13): the CONTENTION arm reads exactly this file, and a second
	// spelling somewhere else is a second source of truth.
	serveLockProcRoot      = "/proc"
	serveLockProcLocksPath = serveLockProcRoot + "/locks"

	// serveLockProcFdDirName is the per-process descriptor directory under
	// serveLockProcRoot/<pid>/ — the second join (/proc/<pid>/fd/<n>) that
	// names the LIVE pids holding the refused inode, which is who actually
	// keeps a flock alive.
	serveLockProcFdDirName = "fd"

	// serveLockFlockTypeColumn is the lock-table column that marks a BSD flock
	// (as opposed to a POSIX lock on the same inode, which is a different
	// mechanism).
	serveLockFlockTypeColumn = "FLOCK"

	// serveLockKernelNoMatch is the readout when the table is readable and
	// carries NO flock record for the refused descriptor's device+inode — and
	// the one case that tells the two causes of this epic apart: a record naming
	// a dead owner is "a corpse still holds it", while NO record at all means
	// the refusal was against an inode this path no longer names (a replaced
	// lock file) — never a live holder.
	serveLockKernelNoMatch = "no FLOCK record for inode %s — the refusal is against an inode this path no longer names (a replaced lock file), not a live holder"

	// serveLockKernelUnreadable is the readout when /proc/locks cannot be read
	// at all: "I cannot say", never "nobody holds it" (Rule 15 — the two are
	// different answers and only one of them may be reported as one).
	serveLockKernelUnreadable = "unreadable — cannot say who holds inode %s (this is NOT 'nobody holds it')"

	// serveLockKernelFdHoldersNote renders the fd-holder half of the readout:
	// the live pids holding an open descriptor on the inode, which is the ONLY
	// authoritative statement about who keeps a flock alive (a flock lives on
	// the open file description; it is released when the last fd copy closes,
	// whichever process holds it).
	serveLockKernelFdHoldersNote = "live pids holding an fd on that inode: %s"
	// serveLockKernelFdHoldersNoteIncomplete is the SAME label for a scan that
	// could not judge every process, and it is a DIFFERENT sentence on purpose
	// (epic 01a14845): the pre-fix rendering put "(scan incomplete)" after a
	// note that still asserted "LIVE pids holding an fd on that inode", i.e. it
	// turned "I could not judge" into "these are the holders". An unknown is not
	// a present (Rule 15), so the label itself changes: what the scan proved,
	// and what it could not.
	serveLockKernelFdHoldersNoteIncomplete = "pids PROVEN to hold an fd on that inode: %s (a lower bound — the scan could not judge every process, so other holders may exist and this list is NOT the whole answer)"
	// serveLockKernelHoldersNone / Unknown / IncompleteSuffix are the three
	// answers to the fd-holder scan: none found on a complete scan, "I cannot
	// say" (an unreadable /proc — NOT the same as none, Rule 15), and the
	// marker appended when a scan found holders but could not judge some
	// processes.
	serveLockKernelHoldersNone    = "none"
	serveLockKernelHoldersUnknown = "unknown (could not read every fd table — this is NOT 'none', and it is NOT a list of holders)"
	// serveLockKernelHoldersIncompleteSuffix rides the INCOMPLETE note, so it
	// never appears on the complete one (that shape is what made an incomplete
	// scan read as a holder report).
	serveLockKernelHoldersIncompleteSuffix = " (scan incomplete: unknown, not a clean answer)"
	// serveLockPIDPrefix is the holder rendering's pid prefix — ONE definition
	// shared by the refusal's holder and the merger's stray-holder note (Rule
	// 13: a constant that exists must be REFERENCED, not bypassed by a second
	// copy of its literal).
	serveLockPIDPrefix = "pid "
	// serveLockPidListJoin separates the pids in the fd-holder list, and
	// serveLockKernelEvidenceJoin separates the evidence fields on the kernel
	// line (the record list from the fd-holder list) from the refused inode.
	serveLockPidListJoin = ", "

	// serveLockKernelEvidenceJoin separates the evidence fields, and
	// serveLockKernelNoInode is the readout when the refused descriptor cannot
	// be stat'd at all — nothing can be joined, and no record is invented.
	serveLockKernelEvidenceJoin = " | "
	serveLockKernelNoInode      = serveLockKernelRecordsPrefix + "cannot stat the refused descriptor — its inode is unknown"
	// serveLockHolderUnknown is the holder rendering when the lock file carries
	// no pid at all (a holder predating pid stamping) — kept as one value so
	// the refusal and the worker face cannot drift apart.
	serveLockHolderUnknown = "an unknown holder"
	// serveLockDeviceFieldFormat spells a file as the kernel's lock table does:
	// "<maj>:<min>:<ino>", with major/minor in HEX (zero-padded to two digits,
	// as the kernel prints them — measured: st_dev 0x1b decodes to major 0 /
	// minor 27 and /proc/locks prints "00:1b") and the inode in decimal.
	serveLockDeviceFieldFormat = "%02x:%02x:%d"
	// serveLockDevMajorMask / serveLockDevMajorShift decode st_dev's major
	// number and serveLockDevMinorMask / serveLockDevMinorShiftHi /
	// serveLockDevMinorMaskHi its minor — the Linux kdev_t encoding, named
	// rather than inlined so the decode has exactly one definition.
	serveLockDevMajorMask    = 0xfff00
	serveLockDevMajorShift   = 8
	serveLockDevMinorMask    = 0xff
	serveLockDevMinorShiftHi = 12
	serveLockDevMinorMaskHi  = 0xfff00
	// serveLockFlockDeviceInodeColumn is the lock-table column carrying
	// "<maj>:<min>:<ino>" — the join key back to the refused descriptor.
	serveLockFlockDeviceInodeColumn = 5

	// taskChildFailTestEnv is the hermetic test escape for the serve-loop
	// survival path: when set to "1", runTaskRunOnce treats the child run
	// as FAILED without spawning it (no model call, no API key) — the
	// regression test drives the real writeback+continue path the daemon
	// exercises on a genuine child failure. Mirrors the GOPI_SKIP_CLAIM_
	// BROADCAST single-node escape style. Only ever set by tests.
	taskChildFailTestEnv = "GOPI_TASK_CHILD_FAIL_TEST"

	// taskAbnormalChildMarker tags the result digest of the WRAPPER's own
	// post-Wait record of an abnormal child exit, so an operator reading the
	// row can tell "the wrapper reported a crash" from "the child reported a
	// judged failure" at a glance. Spelled ONCE here (Rule 1b): the digest is
	// replicated into the legacy mirror rows too.
	taskAbnormalChildMarker = " — task run failed (abnormal child exit)"

	// taskRunRetryHandoverFmt is the operator-facing handover line the wrapper
	// prints when the retry ladder is exhausted: WHERE the ladder ended, WHY
	// (the named reason recorded on the row), and the two ways forward.
	taskRunRetryHandoverFmt = "task %s: retry ladder exhausted — %s\n"

	// taskRunRetryRefusedFmt is the line for a retry that could NOT be started
	// (no resumable target, or the double-run gate would refuse the resume):
	// the run stays failed and the reason is printed — never a silent skip
	// (Rule 15).
	taskRunRetryRefusedFmt = "task %s: abnormal child exit, no automatic retry started — %s\n"

	// taskRunRetryResumedFmt is the line for a retry that WAS started: the
	// attempt ordinal, the cap, the child's pid and where its own output went.
	taskRunRetryResumedFmt = "task %s: abnormal child exit (attempt %d of %d) — resuming the same session: %s (pid %d, log: %s)\n"

	// taskRunRetryNoteFmt is the ATTRIBUTION line for a resume whose target had
	// to be resolved AWAY from the ledger column: the row's `session_id` named a
	// session that never ran it (the submitter's), so the resume continues the
	// row's own run instead. Printed rather than dropped — an operator reading
	// the retry log must be able to tell which session was continued and why it
	// is not the one the ledger shows (Rule 15).
	taskRunRetryNoteFmt = "task %s: resume target note — %s\n"

	// taskRunRetryRoundEndFmt is the ATTRIBUTION line printed when the ladder
	// hands a resume to a process that outlives this run. It exists because
	// the detached resume is the one descendant a finished run does NOT wait
	// for — see taskRunRetryOrphanNote (measured: an 8m27s survivor that the
	// round had reported as finished).
	taskRunRetryRoundEndFmt = "task %s: the round ENDS here — resumed pid %d is detached and outlives it (cgroup %s)\n"
)

// tasksUsage is the help text for the gopi task subcommand family. It is
// printed when a subcommand is missing required arguments or the user passes
// -h/--help. The ledger is append-only — no delete subcommand exists (nick's
// iron rule: LLM/CLI may create tasks, edit summaries, change status — never
// delete; records are permanent for cross-session tracing/audit).
const tasksUsage = `GoPi task subcommands — the task-as-memory ledger (跨会话追溯/审计).

Usage:
  gopi task <sub> [args]

The ledger lives in the SAME memory.db as distilled_qa / memory_feedback, so a
task record survives its session. It is append-only: NO delete, the summary is
immutable, and a briefing revision is itself an audit record.

  Read the ledger — read-only: no verb here writes a row.

  gopi task list [--status S] [--all] [--db PATH]
      List rows, newest first; the row a dispatcher takes next is marked [next],
      an unclaimable one [queue-skip] — claim order is priority-first, NOT the
      render order — and a [hidden] line counts the archived rows --all shows.
      A LIVE row ends with its GROUND: "[ground <branch> @ <dir>]" (man gopi-task).

  gopi task show [<task_id>] [--branch B | --worktree PATH] [--db PATH]
      One row in full, named by task_id OR by its DELIVERABLE (--branch /
      --worktree — what an operator standing in the ground holds); a selector no
      row records fails by name and is followed by GIT PROVENANCE.

  gopi task status [<task_id>] [--branch B | --worktree PATH] [--json] [--db PATH]
      Is the holder ACTUALLY running? — answered against /proc (pid + start
      time): a REUSED pid reads recycled, never live; a GROUND with a live
      process reads IN USE; a foreign-host holder reads unknown. FINISHED work is
      refused (gopi task clone); FAILED is resumable; CANCELLED carries a caveat.
      Exit: 0 resumable, 1 running (do NOT resume),
      2 the row's recorded ground is in use, 3 unverifiable,
      4 no ledger row names the selector,
      5 the row's status forbids a resume (it is finished — gopi task clone).

  gopi task search <keyword> [--db PATH]
      Full-text search over summary / task_id / session_id / tags / payload /
      source (FTS5 + trigram, with a LIKE fallback for short CJK fragments).

  gopi task dedup-scan [--live] [--db PATH]
      Twin-ROW audit: report rows that look like one another (payload hash /
      canonical hash / trigram-similar); --live narrows to claimable rows.
      Report-only; exits 2 when twins are found.

  gopi task improvements
      Agency pass, REPORT-ONLY: mine this ledger for recurring patterns past the
      improvement thresholds and print each candidate with its evidence. Writes
      nothing — the proposal write path is the task-end hook, armed by
      GOPI_AGENCY=1 and fenced by the "quiet" tag so nothing is auto-claimed.

  Write the ledger — a row's status, briefing, repo, ground or verdict.

  gopi task submit "<summary>" [--payload TEXT|@FILE] [--tags a,b] [--parent-task-id ID]
                   [--source agent|human|worker|merger|server] [--task-id X]
                   [--session-id ID] [--db PATH]
                   [--repo REPO_PATH] [--model NAME] [--priority N]
                   [--fixture]
                   [--attachment ABS_PATH ...]
      Enqueue a PENDING task (the human/software entry point; the LLM uses the
      task_submit tool). --payload is the briefing the executing instance runs
      with; omitted, the summary becomes it (a payload-less row is never
      claimed). --payload TEXT|@FILE: a leading '@' followed by an existing
      file path is READ — the row stores the file's CONTENT, never the spelling,
      and an unreadable reference is REFUSED by name ('@@text' stores a
      literal '@'). The task_submit TOOL reads the same reference through the
      same engine, so the two faces cannot disagree.
      tags=["bug"] is the bug-flow entry. --source names the row's
      ROLE, derived from this process's own evidence when omitted (an explicit
      --source human the GOPI_HUMAN_TERMINAL gate denies is refused, never
      stored as something else). --repo must be a git work tree, verified
      BEFORE the row is written; --attachment (repeatable) copies files into
      the task's session dir. --fixture marks the row QUEUE DECORATION: its
      reserved "fixture" tag is refused by the shared claimability predicate, so
      the row shows as [queue-skip] and no dispatcher spends a run on it.
      --parent-task-id is REPEATABLE: the row depends on EVERY parent named
      (Rule 33's gate becomes an AND over the set), and the typed order is kept
      — the FIRST parent is stored in parent_task_id, the rest in
      parent_task_ids, so a reader that knows only the old column still sees a
      dependency. A repeated flag is no longer silently collapsed to its last
      occurrence.
      The literal "independent" is the task_submit TOOL's sentinel ("do not
      chain me to my previous submission"). This face has the SAME value with
      the SAME outcome: the row goes out with NO parent, and a note on stderr
      says so. The CLI never auto-chains, so omitting --parent-task-id already
      submits a parentless row — the spelling is accepted rather than refused
      as "parent_task_id does not exist", which pointed at a mistyped id rather
      than at the other face. Combining it with a real id is refused.

  gopi task update <task_id> [--status X] [--payload TEXT|@FILE] [--reason WHY]
                   [--result DIGEST] [--repo PATH] [--parent-task-id ID]
                   [--clear-parent] [--force] [--db PATH]
      Move a status, revise the briefing, re-point the repo or the dependency
      edge (at least one arm; they run in a fixed order). --parent-task-id is
      REPEATABLE and REPLACES the row's whole parent set in the typed order;
      --clear-parent removes EVERY parent (the list, not only the first). --force is the
      operator's DELIVERY-PROOF escape (the CLI face of
      GOPI_TASK_DELIVERY_PROOF_ALLOW): it does NOT skip the measurement — when
      the deliverable is provably not where the row says, the write still
      proceeds, it is announced on stderr and recorded on the row's trail as
      delivery-proof-unverified, so a waived close can never be read as a
      proven one. It waives nothing else. --repair-session runs the
      SESSION-PROVENANCE pass over the rows already on the ledger: a row whose
      session_id names a session provably NOT its own (that session has a
      container or a flat record of its own, or the chats index names it) is
      rewritten to the id of the record inside its OWN run directory
      (sessions/<task_id>/). DRY RUN unless --apply is typed; a value that
      cannot be proven foreign is never touched; every change is recorded on
      the row's trail as session-repaired. With no <task_id> the pass sweeps
      every row carrying a value, with one it narrows to that row. --repo
      naming a repo
      re-points deliverable.repo; naming a LIVE REGISTERED LINKED work tree of
      the row's own repo instead records the ground (deliverable.worktree +
      branch) — the door the ground refusal sends you to. --payload requires
      --reason, records payload_revised (old/new sha256s) and refuses blanking
      by name; --reason omitted takes a status-specific default.

  gopi task archive <task_id> [--db PATH]
      Archive a TERMINAL row as resolved ("resolved" tag): still searchable and
      showable, out of every list view unless --all. A branch operand retires
      that branch instead — the top-level spelling, whose page carries its
      flags: gopi task archive -h.

  gopi task clone <task_id> [--reason WHY|@FILE] [--repo PATH] [--confirm-cancelled]
                   [--dry-run] [--db PATH]
      CLEAN-ROOM copy: a new pending row under a FRESH uuid (parent=<id>, with
      the lineage in its OWN tags clone + rework-of:<id> — never in the id)
      that carries the
      briefing VERBATIM + tags + repo + the attachment manifest, and NO worktree/
      branch/grounds, so its ground derives from its OWN id. GATE: completed/
      failed/verified yes; in_progress/claimed and pending NO; cancelled needs
      --confirm-cancelled; a LIVE clone is refused by name. --dry-run writes
      nothing. Second rung of the resume ladder; man gopi-task.

  gopi task sweep-worktrees [--dry-run] [--apply] [--backfill-anchors]
                            [--min-age-h N] [--max N] [--json] [--db PATH]
      Reclaim STALE worktree registrations: only the provably abandoned ones
      (declared root, no live holder process, no non-terminal owner row, CLEAN,
      past the age threshold). A dirty worktree is REPORTED, never removed.
      --backfill-anchors runs the run-ground anchor axis' BACKFILL leg under
      the SAME plan/apply gate: it ADOPTS the clone grounds no producer ever
      anchored (none live in it, tree clean, every commit delivered) by writing
      a MEASUREMENT — never a taker identity — so the collect leg above can
      name them. It removes nothing. See devdoc/run-ground-anchor-backfill.md.

RETIRED: MOVING A ROW TO ANOTHER MACHINE (2026-11-05). A family of eight
cross-node spellings used to live here — sync, sync-pass, harvest, diff, send,
receive, pull, push — and every one of them reached ANOTHER
node's ledger over ssh. No verb in this binary does that any more: work crosses
hosts through the GIT ref a row's deliverable publishes to (the clone channel's
delivery leg), and a row that must be WORKED ON another host is filed there —
'gopi task submit "<briefing>"' on that host, which stamps that host's own repo
and ledger (Rule 28 / Rule 40). The eight spellings still DISPATCH, so an old
script, task row or shell history hears this instead of "unknown subcommand";
each prints one sentence naming where its capability went and exits 1. The one
capability of that family that was never a peer protocol — the twin-row audit —
is a ledger verb now: 'gopi task dedup-scan'.

The git-infrastructure atoms (merge / merge-execute / merged-cleanup / adopt /
audit-merges) and the merge machinery are documented by 'man/gopi-merge.md';
the task-executing service by 'gopi worker -h'. The RESIDENT MERGER DAEMON is
RETIRED (2026-09-19, devdoc/merger-retired.md): 'gopi merger' and its
deprecated alias 'gopi task merger' / 'gopi task serve' refuse and exit 1.

MERGING IS NOT GONE — THE TOP-LEVEL 'gopi merge' FACE IS (epic merge-removal),
and this note is where a reader who typed that word lands. The atoms below still
run the same engine under the family spelling the post-receive hook types, so a
merge pass is 'gopi task merge' (measured 2026-10-31: it landed a probe branch
and deleted it). What was removed is the standalone verb; the word still ROUTES,
so an old script gets this explanation instead of "unknown command".
man/gopi-merge.md documents the machinery.

Read by more than one verb, so they stay here:
  --db PATH   override the memory db path (default: ~/.gopi/memory.db)
  Status values: pending → claimed → in_progress → terminal (completed /
  failed / cancelled / superseded), plus verified. 'superseded' means the work
  is done ELSEWHERE; it releases a child through the dependency gate.
  Contracts: devdoc/gopi-task-ledger-architecture.md.
`

// isTasksSubcommand reports whether argv begins with the task subcommand
// family. When it does it runs the subcommand and returns true (the caller
// must not treat the remaining args as an input file). Checked before the
// top-level -h scan so `gopi task -h` prints the family usage.
func isTasksSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] != CliVerbTask {
		// Peer-side PROLOGUE: ssh runs the remote command under a shell, so
		// the peer's gopi receives the peer's command line — program word
		// first. It is recognized by POSITION (argv[0]), never by searching
		// for the word, and nothing dispatches unless the next token really
		// is the task VERB (which is consumed here, so what reaches the
		// dispatcher is the same slice the ordinary `gopi task …` path hands
		// it). This is a shape the passthrough exists for; it costs one
		// branch and it is the opposite of a scan.
		if args[0] == gopiProgramWord {
			if len(args) > 1 && args[1] == CliVerbTask {
				runTasksSubcommand(args[2:])
				return true
			}
			return false
		}
		// A gopi command line is not an ssh argv: every guess below is about a
		// REMOTE argv (`web` is not a host; "task" inside a query is not a
		// verb), so the walk refuses such an argv outright.
		if remote := argvAfterSSHPrefix(args); remote != nil {
			// The remote command arrives either word-split by the shim
			// (strings.Split) or as one login-shell-quoted element
			// (`ssh -o X host "gopi task sync --serve-remote"` → a single
			// positional). Both are COMMAND LINES — the walk above is what
			// guarantees that — and both are answered by the SAME retry: find
			// the task verb, dispatch what follows it.
			words := strings.Fields(strings.Join(splitQuotedArgs(remote), " "))
			for i, w := range words {
				if w == CliVerbTask && i+1 < len(words) {
					runTasksSubcommand(words[i+1:])
					return true
				}
			}
		}
		return false
	}
	runTasksSubcommand(args[1:])
	return true
}

// tasksOptions is the parsed result of one gopi task sub-verb invocation.
type tasksOptions struct {
	db      string
	status  string
	summary string
	result  string // update --result: result digest recorded to task_events (verify-loop evidence)
	// repoRepairOnly marks a `task update --repo PATH` with NO --status: a pure
	// deliverable.repo repair (epic 01a0d55b), so the command returns without a
	// status transition instead of erroring on "nothing to update".
	repoRepairOnly bool
	// payloadSet records that `task update --payload` was PRESENT (never
	// whether its value was empty — see the parse arm), so the dispatch arm
	// knows an edit was asked for even when the engine will refuse the value.
	// statusOmitted records that NO --status flag was given: the invocation is
	// a pure EDIT (--repo repair and/or --payload revision) and no transition
	// is attempted. It is the flag's presence, not its value: `--status ""`
	// stays a malformed transition the engine refuses, exactly as before.
	payloadSet    bool
	statusOmitted bool
	// parentSet / clearParent are the TREE-EDGE arm's presence pair (the same
	// presence-not-value rule as payloadSet): whichever was typed decides
	// whether the dispatch arm calls tools.TaskReparent at all, and with which
	// target. --parent-task-id carries the target id in parentTaskID (shared
	// with submit's flag of the same spelling); --clear-parent carries None.
	parentSet   bool
	clearParent bool
	// parentTaskIDs is EVERY `--parent-task-id` occurrence, in typed order
	// (parent 1 first). It is the list `submit` and the reparent arm actually
	// consume — parentTaskID above is kept as its FIRST element for the code
	// paths that still speak of "the" parent, so the two can never disagree.
	parentTaskIDs []string
	// force is `task update --force` (PRESENCE, not value — the same rule as
	// payloadSet): the operator's DELIVERY-PROOF escape, handed verbatim to
	// tools.TaskUpdateReasonForced. It is not an arm of its own; on its own it
	// asks for nothing.
	force bool
	// repairSession / repairApply are `task update --repair-session` and its
	// --apply opt-in (epic 01a17df9 ③): the session-provenance repair pass,
	// DRY by default. Presence-not-value, the same rule as force.
	repairSession bool
	repairApply   bool
	taskID        string // show/update/archive target; submit's --task-id
	all           bool   // list --all: include archived (resolved) rows
	query         string // search keyword
	// rowArchive / branchArchive are the TWO meanings of `task archive <operand>`
	// (epic verb-restructure, 2026-10-05). Exactly one is set, and the parser
	// decides which by resolving the operand against the ledger: a row id
	// retires the ROW (tags += resolved, the long-standing meaning), anything
	// else retires the BRANCH through the re-homed `gopi task archive` engine. They
	// are explicit flags rather than an inferred comparison at the dispatch arm
	// because the arm must not re-ask the question the parser already answered
	// (one decision, one place).
	rowArchive    bool
	branchArchive bool
	// archiveArgs is the RAW argv this parser was handed, kept only for the
	// branch arm: it is re-parsed by the branch archive's own parser
	// (parseArchiveArgs), which owns a different flag set and a different
	// refusal contract. Handing it the ORIGINAL argv — rather than
	// reconstructing one from the parsed fields — is what keeps the two
	// parsers from having to agree about flag spelling, `=`-attached values
	// and positionals: there is exactly one reading of the command line, and
	// it is performed by the verb that consumes it.
	archiveArgs []string
	// M4.12 harvest / M4.13 sync fields (both verbs address a peer).
	peer        string // --peer: the ssh alias (harvest pull-from / sync session)
	peersRaw    string // sync --peers: comma list overriding the whitelist
	peersAll    bool   // sync --peers with NO value: every DECLARED peer (the flag's name made true)
	since       string // harvest --since: unix ts override for this run
	remoteDB    string // harvest --remote-db: peer-side ledger path override
	noPush      bool   // harvest --no-push: skip the git push-back phase (db-only)
	serveRemote bool   // sync --serve-remote: the PEER side (stdin patch → stdout envelope)
	statusOnly  bool   // sync --status: print per-peer anchors, no session
	syncTags    string // sync --tags: partition filter — export only rows carrying this tag (epic ③ 模块分区选段)
	claimRemote bool   // sync --claim-remote: the PEER side of a claim broadcast (stdin claim patch → accepted/refused)
	pushOnly    bool   // sync --push-only: push our rows to the peers WITHOUT a pull session (manual claim broadcast)
	syncFull    bool   // sync --full: full-reconciliation session (ignore anchors, re-push everything — M4.24)
	diffOnly    bool   // diff --peer: reconciliation set-compare against a peer's ledger (M4.24)
	rowRemote   bool   // sync --row-remote: the PEER side of a row send (stdin one-row patch → receipt)
	remoteHome  string // send --remote-home: GOPI_HOME exported on the PEER-side argv (isolated profile)
	// psendOnly/receiveOnly are the two ROW legs (`task send` / `task
	// receive`): the single-row push and pull the family was missing. They
	// are the SAME shape as the other peer flags above and share the parse
	// case's validation; naming them separately keeps "which leg is this"
	// readable at the dispatch site.
	sendOnly    bool
	receiveOnly bool
	// pullOnly is the THIRD row-leg face (`task pull`), the operator's own
	// word for the pull half. It is its own field rather than a reuse of
	// receiveOnly because the two faces differ in exactly one place the
	// dispatcher must know about: pull carries a LIST of ids and reports per
	// row. Sharing the flag would leave the runner unable to tell which
	// selector it was handed.
	pullOnly bool
	// cancelSource is `task send --cancel-source` (row legs only): the row is
	// cancelled on THIS ledger once a peer's ledger reports it PERSISTED the
	// row. Presence, not value. It is REQUIRED for a send — a send without it is
	// refused before any peer contact (tools.SyncRowSendEx's move-only gate),
	// because a push that leaves the source pending is a COPY of one task on two
	// ledgers.
	cancelSource bool
	// (the `chain` field lived here — `task send --chain`'s batch selector. It
	// was RETIRED 2026-11-05 with the chain-transfer mechanism and the
	// cross-node sync family that reached it: `task send` is in
	// taskverbs.RemovedVerbs, so the field, its `--chain` spelling constant and
	// its boolean registration are gone with the leg that read them. Rule 43:
	// nothing outside the deleted mechanism read them, so they go with it. The
	// PEER's handler keeps its arity bound — tools.syncRowPatchMaxRows — because
	// that is a wire defence against a bad sender, not this flag's business.)
	// takeOver is `task receive --takeover`: the row is not READ off the peer
	// but HANDED OVER — the peer signs it off its queue (pending → cancelled,
	// a CAS), the content crosses on the same row leg, and this ledger claims
	// the copy that landed. The flag takes no value: it is the difference
	// between "read this row" and "take this row".
	takeOver bool
	// transferNext / transferHandover / transferSkip are the PEER-SIDE faces of
	// that same leg, dispatched on `task sync` beside --row-remote: the
	// read-only selector (`--next-eligible [ID]`) and the signature
	// (`--handover ID`). The spellings are the TOOLS constants — the same
	// values this node puts on the wire (Rules 1b/13: one definition,
	// referenced).
	transferNext     string
	transferNextBare bool
	transferHandover string
	// P2 dispatcher fields (submit/serve).
	as             string // --as: claim holder override (default hostname:pid)
	excludeSession string // --exclude-session: anti-self-reference filter
	payload        string // --payload: execution briefing text
	// payloadMirrored records that the submit parser mirrored the positional
	// summary into the payload column because no --payload was supplied
	// (an empty payload would be invisible to every dispatcher — see the
	// `case "submit"` invariant in parseTasksArgs). runTaskSubmit prints the
	// stderr note off this flag instead of re-deriving the condition.
	payloadMirrored bool
	tags            string // --tags: comma-separated
	fixture         bool   // submit --fixture: mark the row queue DECORATION (reserved fixture tag, unclaimable by construction)
	parentTaskID    string // --parent-task-id: split-tree linkage
	// independentParent records that --parent-task-id carried the
	// task_submit tool's INDEPENDENT sentinel (toolnames.TaskSubmitIndependentSentinel)
	// on the CLI face. The CLI never auto-chains, so the sentinel's meaning
	// here is "the caller said, in the tool's own vocabulary, that this row has
	// no parent" — same outcome as omitting the flag, announced on stderr so
	// the two faces are visibly ONE behaviour rather than two silent ones.
	independentParent bool
	source            string // --source: agent|human|worker|merger|server (submit; DERIVED when omitted)
	sessionID         string // --session-id: submitting session override
	model             string // --model: submit's intended-executor model stamp (empty = backfill at writeback)
	every             string // serve --every Nm (accepts s/m/h suffixes)
	once              bool   // serve --once: single pass then exit
	skipTags          string // serve --skip-tag: comma list; skip tasks tagged with any
	reason            string // update/clone --reason: the operator-visible WHY of the status move ('' = status-specific default), or the note a clone carries into its briefing
	successorRepo     string // clone --repo: the OVERRIDE of the repo the new row would otherwise inherit from its source row ('' = inherit)
	// cloneDryRun / confirmCancelled are `task clone`'s two switches. They are
	// their own fields rather than reusing sweepApply/dryRun: those belong to the
	// worktree sweep, whose default is ALSO a dry run for a different reason, and
	// one bool serving two verbs is how a meaning drifts.
	cloneDryRun      bool // clone --dry-run: report the plan, write nothing
	confirmCancelled bool // clone --confirm-cancelled: admit a cancelled source row
	priority         int  // submit --priority: M4.11 explicit override (0 = derive from tags)
	jsonOut          bool // status --json: machine-readable verdict for scripts/CI
	takeover         bool // run --takeover: override the pre-spawn singleton guard (deliberate steal)
	// Phase-1 attachments: submit --attachment <abs_path> (repeatable) —
	// files archived into the task session dir at submit (epic-task-reframe).
	attachments []string
	// sweep-worktrees fields (the stale-registration sweep).
	sweepApply   bool   // sweep-worktrees --apply: the explicit destructive opt-in (default = dry run)
	sweepMinAgeH string // sweep-worktrees --min-age-h N: age threshold override (hours)
	sweepMax     string // sweep-worktrees --max N: cap on one run's reclamations
	// sweepBackfillAnchors is `sweep-worktrees --backfill-anchors`: run the
	// run-ground anchor axis' BACKFILL leg (tools/run_ground_adopt.go) — adopt
	// the clone grounds no producer ever anchored. It is a flag on this verb
	// rather than a twenty-first verb because it belongs to the SAME axis and
	// must be governed by the SAME plan/apply gate: it is a WRITE (one audit
	// record per ground) and a bare run must stay a plan.
	sweepBackfillAnchors bool
	// finishCleanupTask is the `--task T` narrowing, shared by retry-delivery
	// (its own verb) and finish-cleanup — the row to repair instead of the scan
	// set.
	finishCleanupTask string
	// finishCleanupApply is `task finish-cleanup --apply`: the explicit
	// destructive opt-in for the close-out pass (default = dry run, the same
	// posture sweep-worktrees records).
	finishCleanupApply bool
	// selBranch / selWorktree are the two DELIVERABLE selectors shared by
	// `gopi task show` and `gopi task status`: address the row by the branch
	// its deliverable records (`git branch --show-current`) or by its worktree
	// directory (`pwd`), instead of by a task id the operator standing in a
	// worktree does not have. They are a distinct namespace from for-branch's
	// positional, which asks the PLURAL question "who owns this branch", so the
	// two never share a field.
	selBranch   string
	selWorktree string
	// dedup-scan (re-homed memsync --dedup-scan) needs the raw flag set: the
	// audit's two flags (--live, --db) are consumed by runTaskDedupScan, which
	// is shared with the retired memsync spelling — threading the set keeps
	// ONE implementation of the audit in the tree.
	flags  *memoryFlagSet
	dryRun bool // sweep-worktrees --dry-run: report only (explicit spelling)
	// Adoption chain (branch-adoption-chain epic) fields.
	master      string // adopt --master: integration branch override (default master)
	submit      bool   // adopt --submit: enable ③b conflict dispatch (report-only without)
	submitAudit bool   // audit-merges --submit: enqueue (default report-only)

	// TaskMerge fields (gopi task merge — the hook's Go face).
	mergeBranches []string // merge: branch positionals ("" with --all = sweep)
	mergeAll      bool     // merge --all: sweep every local branch except master

	// (the `--no-branch-scan` switch lived here; it was RETIRED with the
	// unmerged-branch inventory it narrowed — F4 of
	// devdoc/merge-resolve-effort-audit.md. Rule 15: an option is either
	// accepted and obeyed or refused by name, and after that retirement the
	// flag was accepted, advertised on two help pages, and read by NOBODY —
	// the "documented knob that does nothing" defect one level up from the
	// dead pipeline it was a sibling of. The merger's live discovery half is
	// MergerRepoSweep, which has no branch-inventory pass to skip.)
	// stop / restart are the RESIDENT LOOP's control face (epic
	// merger-loop-restart): --stop ends the live loop by the signal mechanism it
	// already documents, --restart stops it and starts a fresh loop from the
	// current on-disk binary. They are CONTROL flags, not detach flags — nothing
	// is detached, and the loop they act on is one the operator can already see
	// (the class decision and its argument lived in merger_restart.go, deleted
	// with the retired daemon — epic merger-delete-dead-impl, 2026-09-19).
	// Their VALIDATION is not here but at the face that acts on them
	// (mergerControlRequest), so a bad period is refused by the one reader that
	// would otherwise stop a loop for nothing.
	stop     bool
	restart  bool
	reportTo string // run --report-to: ssh alias the REMOTE reports back through
	repo     string // run --repo: git push-back target for the finished branch
	// submit --repo: the git repo the task's work happens in (M4.8)
	// invokedAs is the program name the merger runner prefixes its
	// diagnostics with: mergerProgTopLevel when main.go's top-level
	// `gopi merger` predicate dispatched us, "" (→ mergerProgFamily) through
	// the deprecated `gopi task merger` face. One field, resolved by
	// mergerProg() so no call site hardcodes a spelling (Rule 1).
	invokedAs string
}

// taskFlagSpellings are the flag spellings the ledger group needs BY NAME in
// more than one place (the admission table taskFlagMember plus a parse case):
// Rule 1b/13 — the value lives once here and every site references it, so a
// typo can no longer admit a flag one spelling and read it under another. That
// gap is exactly how `--task-id` came to be admitted on the sync faces and read
// by nobody (twice: the assignment was missing at b09afc6f, and the session
// faces never had a reader at all).
const (
	// taskIDFlagName is `--task-id`: the row selector (submit's id, the manual
	// push's single-row choice, and the row legs' row). An ALIAS of the leaf
	// package's word (Rule 13/14): internal/tools RENDERS the same selector
	// into the manual push's receipt (the deposit command it recommends), and
	// a second literal would be the mirror Rule 13 bans on the one string the
	// operator then types.
	taskIDFlagName = cliverbs.TaskIDWord
	// syncRemoteHomeFlag is `--remote-home DIR`: the ISOLATION channel of the
	// send leg. It rides the REMOTE argv only (GOPI_HOME=<dir> gopi task sync
	// --row-remote …), never this process's environment — a local export
	// against a peer's profile would move this node's ledger, which is the
	// opposite of what the flag is for (Rule 34: a probe runs in a private
	// profile).
	syncRemoteHomeFlag = tools.SyncRowRemoteHomeFlagName
	// syncRemoteDBFlag is `--remote-db PATH`: the PEER-side ledger a pull
	// reads (harvest's flag, reused by the row leg's receive half rather than
	// re-spelled — Rule 14: one owner per vocabulary).
	syncRemoteDBFlag = "remote-db"
	// taskPullClaimFlag is `--claim`: the flag `gopi task pull` REFUSES BY NAME
	// (epic task-pull, deliverable (d)). It is spelled here — rather than left
	// as an unknown flag — so the operator asking for it hears WHICH shape the
	// family has instead of "unknown flag": an option that is not obeyed must
	// still be answered (Rule 15), and the refusal names the atomic claim round
	// and the send-side hand-off that do exist (see the Pull parse case).
	taskPullClaimFlag = "claim"
	// taskCancelSourceFlag is `--cancel-source`: the push leg's half of a MOVE,
	// and it is REQUIRED (epic task-send-is-a-move). The source row is
	// cancelled HERE, with the family's one pending→cancelled CAS, after a
	// peer's ledger PERSISTED it. It used to be opt-in, and that made `send`
	// without it a COPY: the row stayed dispatchable here while a copy was
	// dispatchable on the peer, so one task could be claimed and run twice.
	// "One row globally" is a property of the system now, not of the caller's
	// spelling — a send without this flag is refused before any peer contact
	// (see runTaskSend and tools.SyncRowSendEx's move-only gate). The two legal
	// shapes are named in that refusal: this flag (push) and `receive
	// --takeover` (pull).
	taskCancelSourceFlag = tools.CancelSourceFlagSpelling
	// (the `--chain` send-leg selector lived here; it was RETIRED with the
	// chain-transfer mechanism and the cross-node sync family that reached it —
	// `task send` is in taskverbs.RemovedVerbs. Rule 43: its spelling constant
	// came from the deleted tools constant, so the whole wiring goes with the
	// mechanism rather than surviving as a flag nothing reads.)
	// taskTakeOverFlag is `receive --takeover`: the RECEIVING half of a
	// cross-ledger row transfer (epic task-pull). It is deliberately a flag on
	// the EXISTING pull verb rather than a new verb (Rule 3: the tool/verb
	// surface is fixed), and its name says what distinguishes it from the
	// plain read: `receive` READS a row off a peer; `receive --takeover` TAKES
	// one — the peer signs it off its own queue first (pending → cancelled,
	// the SAME CAS `send --cancel-source` performs), so the row travels
	// exactly once instead of being readable by both ledgers at once.
	taskTakeOverFlag = tools.TakeOverFlagSpelling
	// taskBranchFlagName / taskWorktreeFlagName are the two DELIVERABLE
	// selectors of `gopi task show`: address the row by the branch its
	// deliverable records (`git branch --show-current`) or by its worktree
	// directory (`pwd`), instead of by a task id the operator standing in a
	// worktree does not have. They live in ONE place because the help page, the
	// admission table and the parse case must agree on the spelling (Rule 1b/13)
	// — the advertised-but-unadmitted defect the sweep-worktrees case records.
	taskBranchFlagName   = "branch"
	taskWorktreeFlagName = "worktree"
	// mergeAtomAllFlagName / mergeAtomRepoFlagName / mergeAtomSubmitFlagName /
	// mergeAtomDBFlagName are the FOUR spellings `gopi task merge` admits —
	// named here because three sites must agree on them and each used to type
	// its own literal: the family parser's case for that verb, the atom's `-h`
	// page, and the refusal that points a canonical-face flag at the spelling
	// which reads it (Rules 1b/13 — the defect this task closes was exactly two
	// sites disagreeing about one flag set).
	//
	// They are the ATOM's spellings of flags the canonical face also has
	// (--all/--repo/--submit/--db all exist on `gopi merge` or the family), so
	// they are deliberately not shared with those faces' own names: what this
	// constant says is "this verb admits this word", and the gate pins it
	// against taskFlagMember in both directions.
	mergeAtomAllFlagName    = "all"
	mergeAtomRepoFlagName   = "repo"
	mergeAtomSubmitFlagName = "submit"
	mergeAtomDBFlagName     = "db"

	// taskDedupScanLiveFlag is `task dedup-scan --live`: the ONE scope flag of
	// the twin-row audit (narrow the pairwise ladder to claimable rows). Named
	// once because three sites must agree — the admission table, the parser's
	// bare-scope refusal (task 01a1ac9e: a bare spelling has NO scope, and its
	// measured cost on a shared ledger is unbounded) and the help line.
	taskDedupScanLiveFlag = "live"

	// successorRepoFlagName is `task clone --repo PATH`: the OVERRIDE of the repo
	// the new row inherits from its source row (epic 01a15657). Named once and
	// referenced by BOTH sites that must agree — the admission table
	// (taskFlagMember) and the parse arm — because a spelling admitted in one
	// place and read in another is the defect class Rule 1b/13 exists for.
	successorRepoFlagName = "repo"

	// taskParentTaskIDFlagName / taskClearParentFlagName are the two spellings
	// of `gopi task update`'s TREE-EDGE arm — the pair that moves a row's
	// parent_task_id after creation (the write path TaskReparent owns; before
	// it, an edge could be set only at INSERT). They live in ONE place because
	// four sites must agree on them (Rule 1b/13): the admission table
	// taskFlagMember, the parse arm, the dispatch arm and the help page — the
	// same four-site rule taskPayloadFlagName above states, and the defect
	// class TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted exists to close.
	//
	// The second spelling is the CLEARING half and is a separate flag rather
	// than an empty value on the first: `--parent-task-id ""` reads as a typo
	// on a command whose whole subject is which id to link to, while
	// `--clear-parent` says the removal is the instruction. The parser refuses
	// the two together (they contradict each other) — a mutually-exclusive
	// pair is not a conflict to resolve silently.
	taskParentTaskIDFlagName = "parent-task-id"
	taskClearParentFlagName  = "clear-parent"

	// taskPayloadFlagName is `--payload`: the row's briefing TEXT. On `submit`
	// it is the raw request the executing instance runs with (mirrored from
	// the positional summary when omitted); on `update` it is the REVISION
	// face — epic 01a0fea1 item [2/5], which retired the "cancel and
	// re-submit" monopoly on fixing a wrong briefing. It is a NAMED constant
	// because four sites in this file must agree on the spelling: the
	// admission table taskFlagMember, the parse arm, the dispatch arm and the
	// help page — the same rule the branch/worktree/bulk trio above states,
	// and the defect class TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted
	// exists to close (an advertised flag the table refuses, or a table entry
	// no parser reads, are the two shapes of the same disagreement).
	taskPayloadFlagName = "payload"

	// taskForceFlagName is `task update --force`: the CLI face of the operator's
	// DELIVERY-PROOF escape. It is an ALIAS of cliverbs.TaskUpdateForceWord
	// (Rule 13/14's one-definition shape, the same one taskTagsFlagName uses)
	// because internal/tools must spell the same flag in the notice it prints
	// and in the trail record, and that package cannot import package main.
	// FOUR sites must agree (Rule 1b/13): the admission table taskFlagMember,
	// the valueless-flag registration, the parse arm and the help page.
	taskForceFlagName = cliverbs.TaskUpdateForceWord

	// taskRepairSessionFlagName is `task update --repair-session`: run the
	// session-provenance pass over the ledger (epic 01a17df9 ③). It is
	// VALUELESS — an instruction, not a value, the same rule taskForceFlagName
	// states — and it is an arm of its own (unlike --force): on its own it asks
	// for exactly one thing, so it satisfies the nothing-asked-for refusal.
	// THREE sites must agree (Rule 13): the admission table taskFlagMember, the
	// parser, and the help page.
	taskRepairSessionFlagName = "repair-session"
	// taskConfirmCancelledFlagName is `task clone --confirm-cancelled`: the
	// operator's explicit "yes, I mean the row we ABANDONED". `cancelled` is
	// refused by default because minting work from an abandoned row silently
	// would resurrect a decision somebody already made — the flag is the one
	// deliberate reversal of it. THREE sites must agree (Rule 13): the admission
	// table taskFlagMember, the parser, and the help page.
	taskConfirmCancelledFlagName = "confirm-cancelled"

	// taskApplyFlagName / taskDryRunFlagName are the repair pass's two spellings
	// of the same decision. The DEFAULT (neither typed) is the dry run: a pass
	// that rewrites a column from a heuristic must never be one keystroke away
	// from doing it (the sweep-worktrees precedent, --apply as the explicit
	// destructive opt-in).
	taskApplyFlagName  = "apply"
	taskDryRunFlagName = "dry-run"

	// taskTagsFlagName is `--tags`: the row's CLASSIFIER on `submit` (and the
	// segment selector on `task sync`). It is an ALIAS of cliverbs.TaskTagsWord
	// (Rule 13/14's one-definition shape, the same one rawFlagFile uses): the
	// chat PA-duty paragraph renders the dashed spelling to tell the console how
	// to file a defect (`gopi task submit "<finding>" --tags bug`), and a prompt
	// that spelled the word itself would be the mirror defect this alias exists
	// to make impossible.
	taskTagsFlagName = cliverbs.TaskTagsWord

	// taskFixtureFlagName is `submit --fixture`: the ONE opt-in that marks a
	// seeded row as queue DECORATION (a rig's proof ledger, a demo, a
	// screenshot). The row is written through the ordinary submit path and
	// carries the reserved fixture tag, which the shared claimability
	// predicate refuses by construction — so no dispatcher will ever spend an
	// LLM round on it, and a rig that seeds it cannot be surprised by a claim
	// it cannot recall (the 2026-10-13 incident: two seeded rows were claimed
	// and run for real, one for 22 turns / 998k tokens).
	//
	// It is a separate FLAG rather than "just pass --tags fixture" because the
	// tag is a protocol value: a rig that re-typed the spelling would drift
	// from the constant the predicate reads (Rule 1b/13/14), and the flag is
	// what keeps ONE definition of it.
	taskFixtureFlagName = "fixture"
)

// taskRowAddressRefusal is the ONE refusal for "no way to name the row was
// given" — shared by `show` and `status` because the two verbs admit the same
// three addresses (the task_id positional, --branch, --worktree) and must give
// the same instruction when none of them is present. Two copies of the sentence
// would drift exactly the way the two admission tables would (Rule 14).
//
// It keeps the historical "missing required parameter:" lead because that is
// the shape every other usage refusal in this family prints, and callers
// (scripts, tests) match on it.
func taskRowAddressRefusal() error {
	return fmt.Errorf("missing required parameter: task_id (positional), --%s <branch> or --%s <path>",
		taskBranchFlagName, taskWorktreeFlagName)
}

// taskBareScopeRefusal is the ONE refusal for the two verbs whose BARE spelling
// names no SCOPE at all — `dedup-scan` and `merge` (task 01a1ac9e, 2026-11-06).
//
// MEASURED before the refusal existed (self-built HEAD binary 6c48f7ed1):
//
//	$ timeout 5 gopi task dedup-scan </dev/null ; echo $?   → 124   (zero output)
//	$ timeout 5 gopi task merge </dev/null     ; echo $?   → 124   (after taking
//	    the merge lock and starting a 300 s build gate on the operator's LIVE
//	    /home/user/work/gopi checkout)
//
// The cost of those two arms is decided entirely by their SCOPE, and scope was
// the one thing the bare spelling never asked for: `dedup-scan --live` answers
// the same audit in 0.46 s (19 rows) because it narrows the ladder, and `merge`
// names its work only once a branch, --all or a declared --repo says what to
// decide. So the CLI face demands the scope, in the SAME refusal shape the rest
// of the family prints — never a timeout, never a progress bar: the root cause
// is a missing parameter, and a watchdog would only make the wrong command die
// later (Rule 15: an option is obeyed or refused BY NAME).
//
// `list` and `improvements` are deliberately NOT in this class: their bare
// spelling IS a complete instruction ("list everything", "mine the ledger"), so
// refusing them would be the same defect mirrored.
func taskBareScopeRefusal(verb string) error {
	switch verb {
	case taskverbs.DedupScan:
		return fmt.Errorf("missing required parameter: --%s (narrow to claimable rows) or --%s <PATH> (name the ledger)",
			taskDedupScanLiveFlag, mergeAtomDBFlagName)
	case taskverbs.Merge:
		return fmt.Errorf("missing required parameter: <branch>... (the branches to decide), --%s (every local branch) or --%s <PATH> (one configured repo)",
			mergeAtomAllFlagName, mergeAtomRepoFlagName)
	}
	// A verb outside the class has no bare-scope refusal; returning an error
	// here would invent a refusal the caller asked nothing about, so the ONE
	// switch above is exhaustive over its own callers (both are named at the
	// two parse arms that call it).
	return fmt.Errorf("missing required parameter: %s takes no bare spelling", verb)
}

// parseTasksArgs parses args for the given sub-verb (list/show/new/update)
// without touching the process (no os.Exit, no env mutation) — the pure,
// unit-testable front-end. Flag syntax follows the memoryFlagSet convention:
// `--name value` and `--name=value` both work; cross-verb flags are rejected.
func parseTasksArgs(verb string, args []string) (*tasksOptions, error) {
	fs := newMemoryFlagSet()
	// Repeatable flags (Phase-1: submit --attachment <path> may appear
	// multiple times — each occurrence archives one attachment).
	fs.repeat("attachment")
	// `--parent-task-id ID` may be given MORE THAN ONCE (EPIC multi-parent):
	// a row can depend on several producers, so `submit` and `update` accept
	// the flag repeated, in the typed order (parent 1 first). WITHOUT this
	// registration the parser keeps only the LAST occurrence — the first is
	// silently dropped and the row is born with one parent while the caller
	// typed two, which is the measured defect this epic exists to fix
	// (`--parent-task-id A --parent-task-id B` stored only B, exit 0).
	fs.repeat(taskParentTaskIDFlagName)
	// `submit --fixture` is a VALUELESS flag (an instruction, not a value):
	// without this registration the scanner's "a bare flag takes the next
	// token as its value" rule would eat the SUMMARY positional and the row
	// would be born with the wrong headline.
	fs.boolean(taskFixtureFlagName)
	// `task update --force` is VALUELESS for the same reason: without this the
	// scanner's "a bare flag takes the next token as its value" rule would EAT
	// the token after it (a status word, an id), so the escape would silently
	// corrupt the command line it was typed on.
	fs.boolean(taskForceFlagName)
	// `task update --repair-session` / `--apply` are VALUELESS for the same
	// reason (the next token is a status word or an id, never their value).
	fs.boolean(taskRepairSessionFlagName)
	fs.boolean(taskApplyFlagName)
	// `sweep-worktrees --backfill-anchors` is VALUELESS: without this the
	// scanner's "a bare flag takes the next token as its value" rule would eat
	// the token after it.
	fs.boolean(sweepBackfillAnchorsFlagName)
	// NO optional-VALUE registration survives here. The only flags of that
	// shape were the cross-node sync family's (`--peers [a,b]`, and
	// `--next-eligible [ID]`), which are deleted with the sync verbs — see
	// taskverbs.RemovedVerbs and task_sync_verb_removal.go's sentence. The mechanism
	// itself (`memoryFlagSet.optionalValue`) stays: the memory family still
	// uses it, and the scanner has to keep deciding "bare flag claims the next
	// token" the same way for every verb that declares one.
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}
	for _, fname := range fs.order {
		if !taskFlagMember(verb, fname) {
			return nil, fmt.Errorf("unknown flag --%s", fname)
		}
	}
	opts := &tasksOptions{
		db:      fs.get("db"),
		status:  fs.get("status"),
		all:     fs.has("all"),
		jsonOut: fs.has("json"),
		flags:   fs,
	}
	switch verb {
	case taskverbs.List:
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
	case taskverbs.ForBranch, taskverbs.BranchRefs, taskverbs.Orphans, taskverbs.Stranded:
		// RETIRED (2026-10-30, task 01a18799). The four read-only audits of the
		// worktree+branch delivery semantics keep DISPATCHING (taskverbs.
		// RemovedVerbs) so a script, a task row, a briefing or a shell history
		// that types one gets a pointed removal message — never the generic
		// usage dump, which would send its reader hunting a typo. Parsing
		// therefore only VALIDATES the shape: no field is read and nothing is
		// executed (the refusal sits in runTasksSubcommand's switch, which this
		// arm lets the verb reach). The historical flags are admitted in
		// taskFlagMember for the same reason `run` keeps its whitelist.
		if len(positionals) > 1 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[1])
		}
	case taskverbs.Show, taskverbs.Status:
		// THREE ways to name the row, and exactly one of them: the task_id
		// positional (the canonical address), --branch or --worktree (the
		// DELIVERABLE selectors — an operator standing in a git worktree holds
		// `git branch --show-current` or `pwd`, not the row id). Parse refuses
		// only the shapes that cannot name a row; the resolution (which needs
		// the ledger) happens in the verb's own runner — runTaskShow /
		// runTaskStatus through the ONE seam both call,
		// resolveDeliverableSelector — and the mutual exclusion of the two
		// selectors is refused by the resolver's ONE definition of it.
		//
		// THE TWO VERBS SHARE THIS ARM because they share the ADDRESSING and
		// nothing else: `task show` renders the RECORD, `task status` renders
		// the live PROCESS VERDICT, and the selector is exactly as useful to a
		// zombie hunt as to a read — the operator standing in a worktree asks
		// "is the row that owns this ground still alive?" with the same two
		// facts in hand. Keeping one arm is what keeps the admission from
		// drifting between them (Rule 14).
		if len(positionals) > 1 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[1])
		}
		if len(positionals) == 1 {
			opts.taskID = positionals[0]
		}
		opts.selBranch = fs.get(taskBranchFlagName)
		opts.selWorktree = fs.get(taskWorktreeFlagName)
		if opts.taskID == "" && strings.TrimSpace(opts.selBranch) == "" && strings.TrimSpace(opts.selWorktree) == "" {
			return nil, taskRowAddressRefusal()
		}
	case taskverbs.Search, taskverbs.Update, taskverbs.Archive:
		if len(positionals) > 1 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[1])
		}
		if len(positionals) == 0 {
			if verb == taskverbs.Search {
				return nil, fmt.Errorf("missing required parameter: <keyword> (positional)")
			}
			if verb == taskverbs.Update && fs.has(taskRepairSessionFlagName) {
				// `task update --repair-session` names NO row when the pass is
				// meant to sweep the whole ledger — that absence is the
				// command, not a dropped argument (epic 01a17df9 ③: the
				// polluted population predates any repair door, so the door
				// must be able to address all of it). A row id beside the flag
				// still narrows it to that one row.
				//
				// The arm's FLAGS are recorded HERE and not only in the
				// positional arm below: this early return IS the sweep shape,
				// and a flag set only the other branch filled would make
				// `task update --repair-session` (no operand) a silent no-op —
				// the "accepted and ignored" shape Rule 15 forbids (measured:
				// the first real invocation fell through to the pristine-engine
				// refusal instead of running the pass).
				opts.repairSession = true
				opts.repairApply = fs.has(taskApplyFlagName)
				// NO STATUS WAS GIVEN: this is a pure edit, so no transition is
				// attempted (the same presence rule the positional arm records
				// below — without it the pass fell through into the
				// status-less refusal AFTER printing its report).
				opts.statusOmitted = true
				return opts, nil
			}
			if verb == taskverbs.Archive {
				// NO OPERAND, BUT POSSIBLY A MODE (epic verb-restructure,
				// 2026-10-05). --check, --reclaim and --verify-commit are the
				// branch archive's operand-LESS modes: they sweep or measure
				// and name no branch, so requiring a positional here would make
				// three of the re-homed verb's documented invocations
				// unreachable — the page would advertise `gopi task archive --check`
				// while `gopi task archive --check` answered "missing
				// task_id", which is exactly the advertising/dispatch
				// disagreement this family's gates exist to catch.
				//
				// ONE READING OF THE COMMAND LINE: the branch parser owns that
				// decision (which flags exist, which of them take no operand,
				// how an unknown flag is refused), so it is ASKED rather than
				// second-guessed. It is a pure parser that returns an error
				// instead of exiting, so asking it here is safe. It answers OK
				// only for a mode that needs no operand; any other zero-operand
				// shape — including `task archive` with no flags at all — stays
				// the historical task_id refusal below.
				if _, aerr := parseArchiveArgs(args); aerr == nil {
					opts.branchArchive = true
					opts.archiveArgs = args
					return opts, nil
				}
			}
			return nil, fmt.Errorf("missing required parameter: task_id (positional)")
		}
		opts.query = positionals[0]
		opts.taskID = positionals[0]
		// TWO ARCHIVES, TOLD APART BY THE POSITIONAL (epic verb-restructure,
		// 2026-10-05). This verb has always had one meaning — retire a ROW
		// (tags += resolved). The top-level `gopi task archive` invented later
		// retires a BRANCH (a refs/tags/archive/<branch> tag plus a recorded
		// reason), and the restructure re-homes it here, because "retire a
		// thing from the active set" is this family's job. The two are
		// distinguished by what the operand RESOLVES TO: an id present in the
		// ledger is the row archive (unchanged below); anything else is the
		// branch archive, whose own flag set and refusal contract
		// (parseArchiveArgs) then apply. The resolution is deferred to the
		// dispatch arm because it reads the database, and a parser that
		// exited on it could not be unit-tested.
		if verb == taskverbs.Archive {
			if tools.TaskRowExists(opts.taskID) {
				// A ROW retires as a row: the long-standing meaning, unchanged
				// below in the dispatch arm.
				opts.rowArchive = true
			} else {
				// NOT a row ⇒ a BRANCH. The operand is the whole decision, and
				// the branch arm keeps the ORIGINAL argv (its own parser owns
				// the flag set).
				opts.branchArchive = true
				opts.archiveArgs = args
			}
		}
		if verb == taskverbs.Update {
			// --result DIGEST: the status move MAY carry a result digest
			// (the verify-loop evidence). The digest rides task_events via
			// TaskUpdateWithResult — the summary column stays immutable
			// (bug056). Requiring --status always (nothing else is mutable).
			opts.result = fs.get("result")
			// --reason WHY: the operator-visible reason for the status move
			// (task-update-reason epic). '@path' file reference (same rule
			// as clone/submit) for long reasons. Empty = the caller is
			// lazy: the engine stamps a status-specific default — the
			// transition still explains itself.
			reason, err := ExpandFileRef(fs.get("reason"))
			if err != nil {
				return nil, err
			}
			opts.reason = reason
			// --repo PATH: RE-POINT the row's deliverable.repo at a live git
			// work tree (epic 01a0d55b — the remedy the ground refusal names).
			// It is the one deliverable mutation a status move carries, and it
			// is routed to the SAME blob writer every other stamp uses
			// (tools.TaskRepointDeliverableRepo), never to a raw UPDATE. Two
			// shapes reach the refusal and both are repaired here: a repo that
			// died (renamed/moved/deleted) and the TOO_BIG reframe's children
			// born with their parent run's deleted ground as their "repo".
			opts.repo = fs.get("repo")
			// --payload TEXT|@FILE (epic 01a0fea1 item [2/5]): REVISE the
			// row's briefing. The value goes through the SAME '@path' file
			// reference the submit face uses, and one function owns the
			// expansion in both places (config.ExpandFileRef via the package
			// alias), so a briefing shipped from a file stays byte-exact and
			// the two faces cannot drift on what '@' means.
			//
			// The PRESENCE of the flag — never the emptiness of its value —
			// is what records the edit: `--payload ""` is an EDIT whose
			// value the engine refuses by name (it would blank the row's
			// only briefing), which is a different outcome from "no edit was
			// asked for" and must not collapse into it.
			opts.payloadSet = fs.has(taskPayloadFlagName)
			if opts.payloadSet {
				body, perr := ExpandFileRef(fs.get(taskPayloadFlagName))
				if perr != nil {
					return nil, perr
				}
				opts.payload = body
			}
			// --parent-task-id ID / --clear-parent: the TREE-EDGE arm (Rule 33
			// — the gate's EDGE becomes movable, its release set and its
			// no-override/no-cascade/no-timeout rules untouched). The two
			// spellings are MUTUALLY EXCLUSIVE and the parser refuses the pair
			// by name: they are contradictory instructions about one column,
			// not a combination to resolve silently. A bare `--parent-task-id`
			// with an empty value is refused too — the clearing instruction has
			// its own spelling (--clear-parent), so an empty one reads as a
			// dropped argument, and guessing which half the caller meant is the
			// silent-failure shape Rule 15 forbids.
			//
			// --reason is REQUIRED with either (the engine refuses, one
			// definition, one message): the trail records WHY the dependency
			// edge moved.
			opts.parentSet = fs.has(taskParentTaskIDFlagName)
			opts.clearParent = fs.has(taskClearParentFlagName)
			if opts.parentSet && opts.clearParent {
				return nil, fmt.Errorf("task update: --%s and --%s are mutually exclusive — pass the parent to set, or --%s to remove the edge, never both",
					taskParentTaskIDFlagName, taskClearParentFlagName, taskClearParentFlagName)
			}
			if opts.parentSet {
				// EVERY typed occurrence, in order — the repeated flag replaces
				// the row's whole parent set (the same "what you typed is what
				// the column says" contract TaskReparent always had, now over a
				// LIST). One empty value anywhere is refused by name: a dropped
				// argument on a command whose subject is which ids to link to
				// must not be guessed at (Rule 15).
				opts.parentTaskIDs = fs.list(taskParentTaskIDFlagName)
				for _, raw := range opts.parentTaskIDs {
					if strings.TrimSpace(raw) == "" {
						return nil, fmt.Errorf("task update: --%s needs a task id; use --%s to remove the edge",
							taskParentTaskIDFlagName, taskClearParentFlagName)
					}
				}
				opts.parentTaskID = strings.TrimSpace(opts.parentTaskIDs[0])
			}
			// --force: the operator's DELIVERY-PROOF escape, as a VALUELESS
			// switch (an instruction, not a value). It is the CLI face of
			// GOPI_TASK_DELIVERY_PROOF_ALLOW and it reaches exactly ONE gate:
			// the delivery-proof arm's allowance. It is NOT a master switch —
			// the dirty-close-out, residue and tip gates are untouched — and it
			// is deliberately not an arm of its own: on its own it asks for
			// nothing, so it does not count as "something to update".
			opts.force = fs.has(taskForceFlagName)
			// --repair-session [--apply]: the SESSION-PROVENANCE pass (epic
			// 01a17df9 ③). It exists because the fill-only-while-empty stamp
			// made a polluted value permanent: rows already carrying another
			// session's id have no door that would ever correct them, so the
			// polluted population needs a one-shot pass of its own. It runs the
			// SAME predicate the write-time self-heal uses
			// (tools.TaskRepairSessionIDs → taskSessionRepairValue), never a
			// second definition of "does this belong to the row".
			// --dry-run is accepted as the explicit spelling of the default.
			opts.repairSession = fs.has(taskRepairSessionFlagName)
			opts.repairApply = fs.has(taskApplyFlagName)
			if opts.repairApply && !opts.repairSession {
				return nil, fmt.Errorf("task update: --%s belongs to --%s (it is that pass's opt-in)", taskApplyFlagName, taskRepairSessionFlagName)
			}
			// Nothing-asked-for refusal: the ONE place that lists what this
			// verb can change. It names every arm, so a caller who typed a
			// flag the table refuses reads the table's contents here rather
			// than guessing (Rule 15).
			if !fs.has("status") && opts.repo == "" && !opts.payloadSet && !opts.parentSet && !opts.clearParent && !opts.repairSession {
				return nil, fmt.Errorf("task update: nothing to update (set --status, --payload, --repo, --parent-task-id, --clear-parent or --repair-session)")
			}
			if !fs.has("status") {
				// No status was given: this is a PURE EDIT (--repo repair
				// and/or --payload revision), so the row keeps the status it
				// had and no transition is attempted. Recorded as the
				// PRESENCE of the flag (see tasksOptions.statusOmitted): a
				// present-but-empty `--status` is NOT this case — it is a
				// malformed transition and the engine still refuses it.
				opts.statusOmitted = true
			}
		}
	case taskverbs.Submit:
		if len(positionals) > 1 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[1])
		}
		if len(positionals) == 0 {
			return nil, fmt.Errorf("missing required parameter: <summary> (positional)")
		}
		opts.summary = positionals[0]
		// '@path' file reference: the payload may carry special characters /
		// formatting essential to task execution — keep it byte-exact by
		// reading from a file instead of an argv string.
		//
		// THE EXPANSION DOES NOT HAPPEN HERE ANY MORE (task_payload_fileref.go).
		// It happens in the ENGINE (tools.TaskSubmitReturningID), which every
		// submit face passes through, because the `task_submit` TOOL has no
		// argument parser of its own and handed the reference's SPELLING to the
		// engine — three bug rows were stored at len("@"+path) bytes with their
		// evidence never arriving, each reporting success. One reader
		// (config.ExpandFileRef) at one chokepoint, and exactly ONE call per
		// value: expanding here as well would read a payload whose CONTENT
		// begins with '@…' a second time and store a different file's bytes.
		payload := fs.get("payload")
		// Positional-submit invariant: a row whose payload is EMPTY is
		// invisible to every dispatcher — TaskNextFiltered skips payload-less
		// rows on purpose (bug028: those are ledger/registration entries, not
		// dispatchable work, and serving one killed the daemon). Before this
		// line the natural `gopi task submit "<text>"` (no --payload) left the
		// payload column empty, so the freshly queued row could never be
		// claimed: `worker` reported "queue drained" while `task list --status
		// pending` still showed the row (only an explicit `task run <id>` —
		// which skips the filter — could run it). Mirror the positional text
		// into the payload so the summary IS the briefing, matching the
		// runTaskBriefing contract and the --help wording. The engine keeps
		// its own semantics untouched: internal auto-submit paths (auto
		// registration rows, the bridge/mirror inserts) call tools.TaskSubmit
		// directly and still create payload-less ledger rows where that is
		// intended.
		if strings.TrimSpace(payload) == "" {
			opts.payload = opts.summary
			opts.payloadMirrored = true
		} else {
			opts.payload = payload
		}
		opts.tags = fs.get(taskTagsFlagName)
		// --fixture: the row is DECORATION, not work. The tag is appended
		// through the ONE shared value (tasktags.FixtureTag), never retyped
		// here — a rig that spelled the tag itself would be a second
		// definition of the persisted protocol value (Rule 1b/13/14).
		opts.fixture = fs.has(taskFixtureFlagName)
		if opts.fixture {
			opts.tags = tools.AppendTaskTag(opts.tags, tasktags.FixtureTag)
		}
		// --parent-task-id is REPEATABLE on submit too (EPIC multi-parent): a
		// child can depend on several producers. The empty-value refusal is the
		// update arm's (one sentence, one place) — here the list is simply the
		// typed occurrences, and TaskSubmit folds them into the two columns.
		//
		// THE INDEPENDENT SENTINEL ON THIS FACE (2026-11-07, task 01a1b448).
		// The SAME parameter name carries a sentinel on the task_submit TOOL:
		// `parent_task_id "independent"` means "do not chain me to my previous
		// submission" (toolnames.TaskSubmitIndependentSentinel). This face does
		// NOT chain — a CLI submit is parentless unless the caller names a
		// parent — so the sentinel has no chain here to suppress. Feeding it to
		// the engine as if it were an id produced
		// `parent_task_id "independent" does not exist`, an error that points
		// its reader at "did I mistype an id?" and never at "this spelling
		// belongs to the other face". So the value is now RECOGNISED here and
		// resolved to the outcome it names on both faces: a PARENTLESS row. The
		// value is read from its ONE definition (Rule 1/13/14) — this package
		// never re-types the literal — and matching is case-insensitive for the
		// same reason the tool's own resolver is: it is a word, not an id.
		//
		// MIXING IT WITH A REAL ID IS REFUSED rather than resolved silently:
		// "no parent" and "these parents" are contradictory instructions about
		// one column (Rule 15 — the pair is named, never guessed at).
		typedParents := fs.list(taskParentTaskIDFlagName)
		opts.parentTaskIDs = make([]string, 0, len(typedParents))
		for _, raw := range typedParents {
			parent := strings.TrimSpace(raw)
			if parent == "" {
				return nil, fmt.Errorf("task submit: --%s needs a task id (an empty value would leave the row with no dependency edge)",
					taskParentTaskIDFlagName)
			}
			if strings.EqualFold(parent, toolnames.TaskSubmitIndependentSentinel) {
				opts.independentParent = true
				continue
			}
			opts.parentTaskIDs = append(opts.parentTaskIDs, parent)
		}
		if opts.independentParent && len(opts.parentTaskIDs) > 0 {
			return nil, fmt.Errorf("task submit: --%s %q means \"no parent\" and cannot be combined with the real task id(s) %v — pass either a parent or the %q spelling (a row either waits for something or it does not)",
				taskParentTaskIDFlagName, toolnames.TaskSubmitIndependentSentinel, opts.parentTaskIDs, toolnames.TaskSubmitIndependentSentinel)
		}
		if len(opts.parentTaskIDs) > 0 {
			opts.parentTaskID = strings.TrimSpace(opts.parentTaskIDs[0])
		}
		opts.source = fs.get("source")
		opts.taskID = fs.get("task-id")
		opts.sessionID = fs.get("session-id")
		// --model: which model the submitter intends to execute the task.
		// Empty is legal (leave the column empty — the writeback backfills
		// the actual executor's model from the child session); a value is
		// written verbatim (same ledger face as taskregister's a.Model).
		opts.model = fs.get("model")
		// M4.11: explicit --priority ALWAYS overrides the tags derivation
		// (rule ④). 0 / absent = engine derives from tags.
		if v := fs.get("priority"); v != "" {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("task submit: --priority must be a positive integer, got %q", v)
			}
			opts.priority = n
		}
		// M4.8: --repo is the git repo the work happens in; absent → the
		// submitter's cwd (server mode = the workspace cwd). Verification
		// (git rev-parse) happens in the engine AFTER default resolution —
		// a non-repo dir aborts the whole submit (t-1788089584). This CLI
		// default is the STRICT face (a human submitter's non-repo cwd is
		// an error); TaskSubmit itself ALSO defaults an empty repo to cwd,
		// but leniently (a non-repo cwd warns + submits deliverable-less)
		// so the internal auto-submit paths are never blocked (epic
		// task-submit-repo-default, t-1788441090).
		opts.repo = fs.get("repo")
		if opts.repo == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("task submit: --repo default (cwd): %w", err)
			}
			// The default is the DURABLE repo, not the cwd itself: a submit
			// issued from inside a run's ground (a linked worktree) must name
			// the main checkout that outlives the run, or the row is born
			// naming a directory its own run deletes (epic 01a0d55b — the
			// TOO_BIG reframe's children). DurableRepoForSubmit is the ONE
			// resolution (Rule 13/14), shared with the engine's lenient
			// default and the task_submit tool.
			durable, derr := tools.DurableRepoForSubmit(cwd)
			if derr != nil {
				return nil, fmt.Errorf("task submit: --repo default: %w", derr)
			}
			opts.repo = durable
		}
		// Phase-1 attachments: every --attachment occurrence, in argument
		// order. Each path must be ABSOLUTE (the archiver's requirement —
		// relative paths cannot map back to a source location) and is
		// validated + copied inside TaskSubmit (a bad path aborts the
		// whole submit).
		opts.attachments = fs.list("attachment")
	case taskverbs.Run:
		// RETIRED (epic worker-run-entry, 2026-09-16). The spelling still
		// DISPATCHES (taskverbs.RemovedVerbs) so a script that types it gets a
		// pointed removal message instead of the generic usage dump — and so a
		// recorded command from before the rework still reaches a sentence
		// naming where the function went. Parsing therefore only VALIDATES the
		// shape; nothing is executed (the refusal sits in runTasksSubcommand,
		// before this point). The flag arm in taskFlagMember is kept for the
		// same reason: `gopi task run <id> --takeover …` (the exact command the
		// dispatch path records) must render as a REMOVAL, never as an
		// unknown-flag error, which would send its reader hunting a typo.
		if len(positionals) > 1 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[1])
		}
	case taskverbs.SweepZombies:
		// RETIRED (2026-10-26, devdoc/zombie-sweep-removed.md). The spelling
		// still DISPATCHES (taskverbs.RemovedVerbs) so a script that types it
		// gets a pointed removal message instead of the generic usage dump —
		// the same contract as the retired `run` above. Parsing therefore only
		// VALIDATES the shape: no field is read, and nothing is executed (the
		// refusal sits in runTasksSubcommand's switch, which this arm lets the
		// verb reach). The historical flags are admitted for the same reason
		// `run` keeps its whitelist: `gopi task sweep-zombies --dry-run` is a
		// command recorded in old task rows and shell history, and it must
		// render as a REMOVAL — not as "unknown flag --dry-run", which would
		// send its reader hunting a typo that does not exist. An
		// accepted-and-ignored flag is not a hazard here precisely because the
		// verb cannot run at all.
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
	case taskverbs.Serve, taskverbs.Merger:
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
		opts.statusOnly = fs.has("status")
		opts.as = fs.get("as")
		opts.excludeSession = fs.get("exclude-session")
		// The sleep parameter (epic merger-self-driving, 2026-09-18): the
		// operand has "=" inside it ("5m0s"), so the scanner's `--` prefix test
		// never mistakes it for a flag and it is always consumed as the value.
		// Its VALIDATION is not here but at the face that acts on it
		// (mergerEveryInterval in runTaskMerger), so a bad duration is refused
		// by the one reader that would otherwise sleep on it.
		opts.every = fs.get("every")
		opts.once = fs.has("once")
		opts.skipTags = fs.get("skip-tag")
		// The control face (epic merger-loop-restart): both spellings are
		// VALUE-LESS switches whose validation belongs to the face that acts on
		// them, exactly like --every's.
		opts.stop = fs.has("stop")
		opts.restart = fs.has("restart")
	case taskverbs.MergeExecute:
		// Merge-backlog executor's CLI face (epic tasks-merge-executor-live):
		// run the guarded merge for ONE pending "merge <branch> into master"
		// row. Direct infrastructure — no LLM child, like audit-merges/adopt.
		if len(positionals) != 1 {
			return nil, fmt.Errorf("merge-execute requires exactly one task id")
		}
		opts.taskID = positionals[0]
		opts.db = fs.get("db")
	case taskverbs.Verify, taskverbs.Reconcile, taskverbs.RetryDelivery:
		// RETIRED (2026-10-30, task 01a18799, slice 2/4). The three
		// implementation-shaped verbs keep DISPATCHING (taskverbs.RemovedVerbs)
		// so a script, a task row, a briefing or a shell history that types one
		// gets a pointed removal message — never the generic usage dump, which
		// would send its reader hunting a typo. Parsing therefore only VALIDATES
		// the shape: no field is read and nothing is executed (the refusal sits
		// in runTasksSubcommand's switch, which this arm lets the verb reach).
		// The historical flags are admitted in taskFlagMember for the same
		// reason `run` keeps its whitelist.
		if len(positionals) > 1 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[1])
		}
	case taskverbs.MergedCleanup:
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
	case taskverbs.FinishCleanup:
		// RETIRED (2026-10-30, task 01a18799, slice 3/4). The spelling still
		// DISPATCHES (taskverbs.RemovedVerbs) so a script that types it gets a
		// pointed removal message instead of the generic usage dump — the same
		// contract as the retired `run` and `sweep-zombies`. Parsing therefore
		// only VALIDATES the shape: nothing is executed (the refusal sits in
		// runTasksSubcommand's switch, which this arm lets the verb reach). The
		// historical flags are admitted for the same reason `run` keeps its
		// whitelist: `gopi task finish-cleanup --apply` is a command recorded in
		// old task rows and shell history, and it must render as a REMOVAL —
		// not as "unknown flag --apply", which would send its reader hunting a
		// typo that does not exist.
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q (finish-cleanup took --task)", positionals[0])
		}
	case taskverbs.SweepWorktrees:
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
		// The report-only posture is the DEFAULT and --dry-run is its explicit
		// spelling; --apply is the only way to make a run destructive.
		opts.dryRun = fs.has("dry-run")
		opts.sweepApply = fs.has("apply")
		opts.sweepMinAgeH = fs.get("min-age-h")
		opts.sweepBackfillAnchors = fs.has(sweepBackfillAnchorsFlagName)
		opts.sweepMax = fs.get("max")
		if spec := opts.sweepMinAgeH; spec != "" {
			if n, err := strconv.Atoi(spec); err != nil || n <= 0 {
				return nil, fmt.Errorf("task sweep-worktrees: invalid --min-age-h %q (want a positive integer, hours)", spec)
			}
		} else if fs.has("min-age-h") {
			return nil, fmt.Errorf("task sweep-worktrees: --min-age-h needs a value in hours (e.g. --min-age-h 24)")
		}
		if spec := opts.sweepMax; spec != "" {
			if n, err := strconv.Atoi(spec); err != nil || n <= 0 {
				return nil, fmt.Errorf("task sweep-worktrees: invalid --max %q (want a positive integer)", spec)
			}
		} else if fs.has("max") {
			return nil, fmt.Errorf("task sweep-worktrees: --max needs a value (e.g. --max 20)")
		}
		// A contradictory argv is refused HERE, exactly like the sibling verb
		// finish-cleanup: the safe reading (--dry-run wins) is what the gate
		// below enforces, but the operator is TOLD rather than quietly handed a
		// plan they did not ask for (Rule 15).
		if err := opts.sweepWorktreeRunConflicts(); err != nil {
			return nil, err
		}
	case taskverbs.Clone:
		// The clean-room copy: one source row, and every refusal decided BEFORE
		// the first write (the engine owns the order).
		if len(positionals) != 1 {
			return nil, fmt.Errorf("missing required parameter: <task_id> (positional)")
		}
		opts.taskID = positionals[0]
		// '@path' file reference, the same convention as submit's: a long
		// "why is the room bad" note reads better from a file.
		reason, err := ExpandFileRef(fs.get("reason"))
		if err != nil {
			return nil, err
		}
		opts.reason = reason
		opts.successorRepo = fs.get(successorRepoFlagName)
		opts.confirmCancelled = fs.has(taskConfirmCancelledFlagName)
		opts.cloneDryRun = fs.has(taskDryRunFlagName)
	case taskverbs.Adopt:
		// Branch adoption chain CLI face: report-only by default,
		// --submit enables the ③b conflict dispatch (the serve face
		// always submits).
		opts.repo = fs.get("repo")
		opts.master = fs.get("master")
		opts.submit = fs.has("submit")
	case taskverbs.Push:
		// Git sync channel's manual face (M4.18): the same atom the serve
		// tick runs, on demand. No flags — topology is declared, not guessed.
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
	case taskverbs.Improvements:
		// Agency miner's report-only face (epic agency-proposals): mine the
		// ledger for pattern-threshold candidates and PRINT them. No flags
		// (the thresholds are named constants; GOPI_AGENCY_MIN_FAMILY tunes
		// the one policy knob) and no writes — proposing is the end hook's job.
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
	case taskverbs.AuditMerges:
		// Merge-backlog audit's CLI face (nick's post-receive hook design):
		// discover unmerged client branches → enqueue merge-backlog tasks.
		// The hook calls this detached right after a push lands — the
		// event-driven stage-1 (no serve daemon needed for discovery).
		// --repo overrides the audited repo (default cwd); --submit flips
		// report-only → enqueue. (Case restored by afe50fd after cfbc959's
		// union-keep dropped it while keeping the verb in the usage line;
		// b6c96bf adds the archive-tag reachability filter upstream.)
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
		// fix(cli-task-surface-restructure, 2026-09-15): the flags were
		// advertised but NEVER PARSED — taskFlagMember had no AuditMerges case
		// and nothing assigned opts.repo/opts.submitAudit, so
		// `gopi task audit-merges --repo X --submit` died on "unknown flag"
		// while the help promised it (Rule 15: a flag accepted-and-ignored is
		// silent failure's quieter sibling — here it was refused, which at
		// least failed loudly). The synopsis moved into the merger usage page in
		// the same change (now RETIRED — devdoc/merger-retired.md), never a lie.
		opts.repo = fs.get("repo")
		opts.db = fs.get("db")
		opts.submitAudit = fs.has("submit")
	case taskverbs.DedupScan:
		// Re-homed from `gopi memory memsync --dedup-scan`: a report-only audit of
		// twin task rows. No positionals; the flag whitelist lives in
		// taskFlagMember.
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
		// THE BARE SPELLING HAS NO SCOPE (task 01a1ac9e): its cost is decided by
		// how much of the ledger the pairwise ladder walks, and the two flags that
		// bound it are the only things that say which ledger and which rows. The
		// measured bare run produced zero output and did not return inside five
		// seconds against this host's 775 MB shared ledger, while `--live` on the
		// same ledger answers in 0.46 s. Refused BY NAME in the family's shape
		// (taskBareScopeRefusal) — never by a timeout.
		if !fs.has(taskDedupScanLiveFlag) && !fs.has(mergeAtomDBFlagName) {
			return nil, taskBareScopeRefusal(verb)
		}
	case taskverbs.Merge:
		// TaskMerge's CLI face (epic 2026-09-01: the hook's inline bash
		// loops became ONE token-free Go command — git is the arbiter).
		// Branch positionals are the merge list; --all sweeps every local
		// branch except master (hp-unique-* archive-exempt). --submit
		// routes conflicts into the ledger (the hook face). NO positional and
		// no --all is the repos-table sweep: the ENGINE resolves the
		// settings.json "repos" table and sweeps each configured repo
		// (tools.TaskMerge's own arm — the CLI stores nothing extra, so the
		// spelling this comment describes is exactly the one the engine runs).
		opts.mergeBranches = positionals
		opts.mergeAll = fs.has("all")

		opts.repo = fs.get("repo")
		opts.submit = fs.has("submit")
		// THE BARE SPELLING IS THE UNBOUNDED ONE (task 01a1ac9e, 2026-11-06) and
		// is refused here rather than deep inside the engine, because what it
		// costs is decided at THIS seam: with no branch, no --all and no --repo
		// the engine resolves the settings.json "repos" table and sweeps every
		// configured repo — here, the operator's LIVE /home/user/work/gopi, with
		// the merge lock taken and a 300 s build gate started. Measured on this
		// host: the bare spelling had produced no decision line and no exit
		// inside five seconds, twice.
		//
		// That arm is still REACHABLE and unchanged — `--repo <PATH>` runs exactly
		// it against one declared repo (the help page documents both spellings,
		// and taskBareScopeRefusal names them). What is refused is the spelling
		// that names NO scope, which is the one an operator types by accident.
		if len(positionals) == 0 && !opts.mergeAll && !fs.has(mergeAtomRepoFlagName) {
			return nil, taskBareScopeRefusal(verb)
		}
	default:
		return nil, fmt.Errorf("unknown subcommand %q (want %s)", verb, strings.Join(taskverbs.DispatchVerbs, "|"))

	}
	return opts, nil
}

// multiParentsRest is parents 2..N of a typed parent list — the tail TaskSubmit
// receives in ParentTaskIDs (the head goes to ParentTaskID). It returns nil for
// an empty list: a bare `list[1:]` PANICS on a zero-length slice (measured —
// every plain `gopi task submit` crashed at the dispatch arm the first time
// this line ran with no flag), and "no parents" is the ordinary case, not an
// edge case.
func multiParentsRest(parents []string) []string {
	if len(parents) < 2 {
		return nil
	}
	return parents[1:]
}

// taskFlagMember reports whether a flag name is valid for the given sub-verb
// (mirrors memberOf's strict per-subcommand whitelist).
func taskFlagMember(verb, flag string) bool {
	switch verb {
	case taskverbs.List:
		switch flag {
		case "db", "status", "all":
			return true
		}
	case taskverbs.Search:
		switch flag {
		case "db":
			return true
		}
	case taskverbs.Show:
		// --db points at a fixture ledger; --branch/--worktree are the
		// DELIVERABLE selectors (they name the row through what it records at
		// submit/claim time, so an operator holding a worktree needs no task
		// id) — see runTaskShow.
		switch flag {
		case "db", taskBranchFlagName, taskWorktreeFlagName:
			return true
		}
	case taskverbs.Status:
		// --branch/--worktree are admitted here for the SAME reason they are
		// admitted on show: the row an operator standing in a worktree holds is
		// addressed by what its deliverable records, and "is that row still
		// alive?" is asked with exactly those two facts in hand (see runTaskStatus).
		switch flag {
		case "db", "json", taskBranchFlagName, taskWorktreeFlagName:
			return true
		}
	case taskverbs.Submit:
		switch flag {
		case "db", "payload", taskTagsFlagName, taskParentTaskIDFlagName, "source", taskIDFlagName, "session-id", "repo", "model", "priority", "attachment", taskFixtureFlagName:
			return true
		}
	case taskverbs.Run:
		switch flag {
		case "db", "as", "exclude-session", "report-to", "repo", "nodes", "takeover":
			return true
		}
	case taskverbs.Archive:
		// The flag table of BOTH archives (epic verb-restructure, 2026-10-05).
		// The row arm's one flag is --db; the branch arm is the whole
		// `gopi task archive` surface, whose membership map IS archiveFlagMembers
		// (cli_archive.go) — referenced, never mirrored (Rule 13/14), so a
		// flag added to the branch archive reaches this spelling by
		// construction. Without this half, `task archive <branch> --reason x`
		// would be refused as "unknown flag --reason" before the branch arm
		// ever ran.
		if archiveFlagMembers[flag] {
			return true
		}
		switch flag {
		case "db":
			return true
		}
	case taskverbs.Verify, taskverbs.Reconcile, taskverbs.RetryDelivery:
		// The RETIRED implementation-shaped verbs' historical flag sets, kept
		// for the same reason the read-only audits' are: a command recorded in
		// an old task row, a briefing or a shell history renders as a REMOVAL
		// rather than as "unknown flag --timeout", which would send its reader
		// hunting a typo that does not exist. Nothing consumes them: the
		// dispatcher exits non-zero before any of them could be read, which is
		// what makes an accepted-and-ignored flag harmless here.
		switch flag {
		case "db", "timeout", "json", "task":
			return true
		}
	case taskverbs.MergedCleanup:
		switch flag {
		case "db":
			return true
		}
	case taskverbs.FinishCleanup:
		// The RETIRED verb's historical flag set, kept so a recorded command
		// renders as a removal rather than an unknown-flag error (the `run`
		// precedent above). Nothing consumes them: the dispatcher exits 1
		// before any of them could be read.
		switch flag {
		case "db", "task", "apply", "json":
			return true
		}
	case taskverbs.SweepWorktrees:
		// --json is admitted because the SYNOPSIS documents it and the runner
		// reads it (runTaskSweepWorktrees → flags.jsonOut → WorktreeSweepJSON).
		// It was missing from this table while both of those were true, so every
		// `gopi task sweep-worktrees --json` — the help's own spelling — was
		// refused with "unknown flag --json": the same advertised-but-not-
		// admitted defect the audit-merges case above records, one verb over.
		// Rule 15: an advertised flag is either obeyed or refused by NAME; the
		// gate that keeps the whole class closed is
		// TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted.
		switch flag {
		case "db", "dry-run", "apply", "min-age-h", "max", "json", sweepBackfillAnchorsFlagName:
			return true
		}
	case taskverbs.ForBranch, taskverbs.BranchRefs, taskverbs.Orphans, taskverbs.Stranded:
		// The RETIRED read-only audits' historical flag sets, kept so a command
		// recorded in an old task row, a briefing or a shell history renders as
		// a REMOVAL rather than as "unknown flag --peer", which would send its
		// reader hunting a typo that does not exist (the `run` precedent, and
		// the same contract `sweep-zombies` records). Nothing consumes them:
		// the dispatcher exits non-zero before any of them could be read, which
		// is what makes an accepted-and-ignored flag harmless here.
		switch flag {
		case "db", "repo", "json", "peer", "only", "delivery":
			return true
		}
	case taskverbs.DedupScan:
		switch flag {
		case mergeAtomDBFlagName, taskDedupScanLiveFlag:
			return true
		}
	case taskverbs.SweepZombies:
		// The RETIRED verb's historical flag set, kept so a recorded command
		// renders as a removal rather than an unknown-flag error (the `run`
		// precedent above). Nothing consumes them: the dispatcher exits 1
		// before any of them could be read.
		switch flag {
		case "db", "stall-sec", "max-run", "dry-run", "kill":
			return true
		}
	case taskverbs.Clone:
		// The clone's four flags + the family's --db. All of them are READ by the
		// parse arm below (Rule 15: an option is either accepted and obeyed or
		// refused by name, never accepted and ignored).
		switch flag {
		case "db", "reason", successorRepoFlagName, taskConfirmCancelledFlagName, taskDryRunFlagName:
			return true
		}
	case taskverbs.Serve, taskverbs.Merger:
		// fix: taskFlagMember never listed "serve"/"merger" — the usage line
		// advertised --every/--once/--nodes/... but every serve/merger
		// invocation was rejected with "unknown flag" (found by the adoption
		// CLI E2E).
		switch flag {
		// "every" IS BACK (epic merger-self-driving, 2026-09-18). It was
		// dropped with the poll loop on the reasoning that "a one-tick process
		// has no poll period" — but the flag was never a control face: it is a
		// SLEEP PARAMETER, and the tick it repeats is the same tick. Re-admitting
		// it is what stops a claiming loop from being unreachable, and the
		// CONTROL/DETACH flags it was retired alongside (--daemon/--stop/
		// --restart/--start/--force/--wait) are still refused: a self-driving
		// loop came back, not a service (Rule 15 — an option is either accepted
		// and obeyed or refused by name, never accepted and ignored).
		//
		// The MERGER half of this table is now only history: both merger verbs are
		// retired at the alias dispatch before this parser is reached (epic
		// merger-delete-dead-impl, 2026-09-19). The entries stay because they are
		// the same table the WORKER's `--every` is parsed from, and because a
		// re-developed resident face will need exactly these spellings again.
		//
		case "every", "once", "as", "exclude-session", "skip-tag",
			"db", "status", "stop", "restart":
			return true
		}
	case taskverbs.Merge:
		// The four spellings the atom admits, read from the constants the atom's
		// page and its refusal pointer also read (Rules 1b/13).
		switch flag {
		case mergeAtomDBFlagName, mergeAtomRepoFlagName, mergeAtomSubmitFlagName, mergeAtomAllFlagName:
			return true
		}
	case taskverbs.AuditMerges:
		// fix(cli-task-surface-restructure, 2026-09-15): the AuditMerges case
		// was MISSING while the synopsis advertised --repo/--submit/--db, so
		// every `gopi task audit-merges --repo X` was refused as an unknown
		// flag. The flags the merger page documents are now actually admitted.
		switch flag {
		case "db", "repo", "submit":
			return true
		}
	case taskverbs.MergeExecute:
		switch flag {
		case "db":
			return true
		}
	case taskverbs.Harvest:
		switch flag {
		case "peer", "since", "db", syncRemoteDBFlag, "no-push":
			return true
		}
	// THE CROSS-NODE FAMILY HAS NO FLAG ARM, by design. `sync`, `send`,
	// `receive`, `pull`, `diff`, `harvest`, `push` and `sync-pass` are DELETED
	// (they are in taskverbs.RemovedVerbs), and their dispatch cases now print
	// ONE removal sentence before any flag is examined — so admitting a
	// historical flag here would only decide which refusal a reader sees. The
	// generic `--flag is not in this verb's set` would send them hunting a typo
	// that does not exist; reaching the removal sentence is the whole point
	// (Rule 15: a removal explains itself).
	case taskverbs.Adopt:
		switch flag {
		case "repo", "master", "submit", "db":
			return true
		}
	case taskverbs.Push:
		switch flag {
		case "db":
			return true
		}
	case taskverbs.Update:
		switch flag {
		case "db", "status", "result", "reason", "repo", taskPayloadFlagName,
			taskParentTaskIDFlagName, taskClearParentFlagName, taskForceFlagName,
			taskRepairSessionFlagName, taskApplyFlagName, taskDryRunFlagName:
			return true
		}
	}
	return false
}

// peerFlagError validates a peer alias given on argv (--peer, --report-to).
// The alias becomes an ssh argv ELEMENT, where a leading "-" is parsed by
// ssh as an OPTION (option injection) — the check lives in the shared
// tools.ValidateSSHPeerAlias and runs at PARSE time so the operator learns
// on argv, not mid-run (Rule 14: one validator, every face).
func peerFlagError(flag, alias string) error {
	if _, err := tools.ValidateSSHPeerAlias(alias); err != nil {
		return fmt.Errorf("%s: %w", flag, err)
	}
	return nil
}

// taskBriefingHeaderSummary / taskBriefingHeaderPayload are the section
// markers that separate the two briefing sources in the composed form (Rule
// 1: named constants). The child gopi receives the composed briefing as its
// [EPIC GOAL] first user message — distinct, explicit headers stop the LLM
// from confusing the one-line intent (summary) with the detailed request
// (payload), which is exactly the confusion the old single-source briefing
// caused (a rich summary was silently dropped when a payload existed).
const (
	taskBriefingHeaderSummary     = "[Task summary]"
	taskBriefingHeaderPayload     = "[Briefing payload]"
	taskBriefingHeaderAttachments = "[Attachments]"
	taskBriefingAttachPrefix      = "[attachment"
)

// taskRunBriefing resolves a task's execution briefing for `task run`: the
// child gopi's whole instruction. BOTH the summary (the one-line intent —
// often the essential line a submitter wrote FIRST) and the payload (the
// detailed request) feed the briefing — a non-empty payload no longer
// suppresses the summary, so a submitter who put the intent in the summary
// and the detail in the payload loses neither (nick, 2026-09-02).
//
// Composition (stable format):
//
//	[Task summary]
//	<summary>          (when non-empty)
//	[Briefing payload]
//	<payload>          (when non-empty)
//	[Attachments]      (when the row carries attachments — Phase-1 session-dir)
//	[attachment1: <abs replica path>]
//	[attachment2: <abs replica path>]
//
// The attachment index names REPLICA paths under the task's session dir
// (~/.gopi/sessions/<task_id>/attachments/...) — the LLM reads them
// with the existing file-read tool; no attachment-specific tool. Empty
// payload + non-empty summary = summary only (the summary IS the
// briefing — TaskSubmit's contract when --payload is absent). BOTH empty =
// refuse (unchanged from the e3ca35ca defense): a row with neither field
// carries zero task semantics — the dispatched child would burn a full
// context budget reverse-engineering what the task even is.
func taskRunBriefing(taskID string) (string, error) {
	summary, payload, err := tools.TaskBriefing(taskID)
	if err != nil {
		return "", err
	}
	summary = strings.TrimSpace(summary)
	payload = strings.TrimSpace(payload)
	if summary == "" && payload == "" {
		return "", fmt.Errorf("task %s has neither a summary nor a payload briefing — refusing to dispatch (set the row's summary or payload first)", taskID)
	}
	var parts []string
	if summary != "" {
		parts = append(parts, taskBriefingHeaderSummary, summary)
	}
	// A payload IDENTICAL to the summary is the positional-submit mirror
	// (parseTasksArgs mirrors the positional text into the payload so the row
	// stays dispatchable). Emitting it twice would feed the child the same
	// one-line text under two headers — noise in the first user message, not
	// information. Distinct texts still produce both sections.
	if payload != "" && payload != summary {
		parts = append(parts, taskBriefingHeaderPayload, payload)
	}
	// Phase-1 attachment index: when the row's deliverable carries an
	// attachments manifest, append the EPIC block naming each replica's
	// ABSOLUTE path under the task session dir. The child LLM reads these
	// paths with its existing file-read tool. Replica paths come from the
	// manifest's replica_rel_path (relative) joined onto the session root —
	// the same derivation as taskDeliverableAttachmentIndex, so task show
	// and the briefing always agree.
	atts, replicaPaths, err := tools.TaskDeliverableAttachments(taskID)
	if err != nil {
		// A broken manifest must be loud: the child would otherwise never
		// know its attachments exist. Refuse dispatch on a hard manifest
		// parse failure (task-not-found / no-manifest return nil, nil, nil
		// — zero change for attachment-less rows).
		return "", fmt.Errorf("task %s: resolve attachment index: %w", taskID, err)
	}
	_ = atts // manifest entries themselves are not injected — paths suffice
	if len(replicaPaths) > 0 {
		parts = append(parts, taskBriefingHeaderAttachments)
		for i, p := range replicaPaths {
			parts = append(parts, fmt.Sprintf("%s%d: %s]", taskBriefingAttachPrefix, i+1, p))
		}
	}
	return strings.Join(parts, "\n"), nil
}

// taskProgName is the program name the run path prints in its diagnostics.
// Since epic worker-run-entry (2026-09-16) every one of those lines comes from
// a run started by `gopi worker` (the loop, `--once`, or a named row), so
// the name says where the reader actually is — and ONE constant feeds them all
// (Rule 1b: the 27 sites that typed the string inline read this instead).
const taskProgName = "gopi worker"

// taskRedispatchWitnessLineFmt is the ONE spelling of the door's announcement
// that THIS run is a re-dispatch and which session it continues. It goes into
// the run's own facts (execRunFacts: the task's output.log AND the spawning
// stream), so the claim is readable afterwards from the same file the rest of
// the run's account is in — a witness the child writes and nothing announces is
// a fact nobody can connect to the door that declared it (Rule 15).
const taskRedispatchWitnessLineFmt = taskProgName +
	": %s is being RE-DISPATCHED — the run continues session %s, and its session_start carries that as the resume witness\n"

// taskProgArchive is the family spelling of the branch archive, passed to the
// re-homed engine so the lines it prints name the command the operator typed
// (Rule 1b: one spelling, declared here, referenced by the dispatch arm).
const taskProgArchive = "gopi " + CliVerbTask + " " + taskverbs.Archive

// THE RUN'S STAGE BOUNDARIES — the seam that answers "how long did it live?".
//
// The measured defect (row 01a1a60e-b419, 2026-11-04): the row was claimed and
// marked in_progress at 20:17:02, its run log's last line is 20:17:11, and the
// process was gone — while the row then read in_progress for 8h38m. Nothing in
// the system could say WHERE it died: before cmd.Start(), after it, or after
// cmd.Wait(). The ledger was no witness either, and the reason is worth stating
// exactly (measured, not assumed — see the two writers): last_toolcall_at is a
// LIVENESS stamp on a 2-minute ticker, written by BOTH halves — the wrapper's
// StartTaskRowKeepalive (internal/tools/taskcloseoutkeepalive.go) and the child
// engine itself (internal/agent/taskheartbeat.go + loop.go). It is pushed while
// anything lives and FREEZES the instant the whole tree is gone, so a row that
// died 8 seconds after claiming and one that worked 8 hours can read the same,
// and neither form says WHICH segment it died in.
//
// So the spawn seam writes its own stage boundaries, each stamped. They join
// the run's EXISTING record (execRunFacts → the task's output.log AND the
// spawning stream), so no new file, no new process, no new ticker (Rule 35) —
// and the LAST stage line present is a fact about the process itself, not about
// a supervisor that may have died with it.
//
// WHAT THIS DOES NOT DO: it does not change Start/Wait semantics (the failure
// chain is already correct: Start failing returns immediately, Wait failing
// reaches writebackFailure(failed)), and it does not judge liveness from a pid
// number.
const (
	// taskRunStageTimeLayout stamps every stage boundary. RFC3339Nano is the
	// shape a reader can diff against a ledger row, an output.log and a shell
	// history without conversion.
	taskRunStageTimeLayout = time.RFC3339Nano
	// taskRunStageSpawningFmt is written IMMEDIATELY before cmd.Start(): the
	// child does not exist yet, so this line is the seam's own last word before
	// exec. A record that ends here died in the seam, not in the child.
	taskRunStageSpawningFmt = taskProgName + ": run stage: spawning child (task %s at %s)\n"
	// taskRunStageSpawnedFmt is written the instant Start returns a pid — the
	// boundary between "the seam is preparing" and "a child exists".
	taskRunStageSpawnedFmt = taskProgName + ": run stage: child spawned pid=%d (task %s at %s)\n"
	// taskRunStageSpawnFailedFmt is the OTHER exit from Start: named separately
	// so an absent taskRunStageSpawnedFmt is never mistaken for a truncation
	// (Rule 15 — the failure states itself instead of leaving a gap to infer).
	taskRunStageSpawnFailedFmt = taskProgName +
		": run stage: child spawn FAILED err=%v (task %s, after %s at %s) — no child process ever existed\n"
	// taskRunStageReapedFmt is written the instant cmd.Wait() returns, with the
	// child's own error and the time it actually lived. This is the line that
	// makes "8 seconds" and "8 hours" different facts.
	taskRunStageReapedFmt = taskProgName +
		": run stage: child reaped err=%v (task %s, lived %s, at %s)\n"
)

// taskRunSpawnFailure turns a failed exec into the ROW'S TERMINAL VERDICT.
//
// THE DEFECT THIS CLOSES (row 01a1b5e2-ca5e, measured 2026-11-07): the spawn
// seam recorded `child spawn FAILED … no child process ever existed` in the run
// log, `runTaskRunOnce` then returned the error, and the process vanished — so
// the row stayed `claimed` for NINE HOURS until an operator looked. Because no
// status transition happened, no `failed` event was ever emitted either, so the
// notify chain (notify.events subscribes to `failed`) could not fire on the one
// failure it exists to report. The worker KNEW the answer; it just never wrote
// it on the channel anyone reads.
//
// WHY `failed` AND NOT A NEW STATUS: `claimed → failed` is already a legal edge
// of taskAllowedTransitions (the exec-never-happened edge, added by epic
// 01a1a87b so a run failing before exec can reach a verdict without asserting a
// process that does not exist). `in_progress` is NOT written and must never be
// here: it means "a child process exists", and by this line none does.
//
// WHY NOTHING NEW: writebackFailure is the seam every other failure arm in
// runTaskRunOnce already uses (refused ground, hermetic child failure, abnormal
// child exit), and the taskUpdate underneath it fires the transition notify.
// One seam, one notify path, zero new machinery (Rule 35). The `start-failed`
// trail event the spawn seam already wrote stays as the audit detail; this is
// the lifecycle verdict, and it is what makes `task status` and `task list`
// read ONE answer for the same row.
//
// It returns the caller's error unchanged, wrapped with the seam's own words,
// so the failure still reaches the worker loop / the process exit code exactly
// as before — the writeback adds a record, it never changes the run's outcome.
func taskRunSpawnFailure(taskID string, spawnErr error, reportTo, repo string) error {
	writebackFailure(taskID, tools.TaskStatusFailed, taskRunSpawnFailureReason(spawnErr), reportTo, repo)
	return fmt.Errorf(taskProgName+" error: start child: %w", spawnErr)
}

// taskRunSpawnFailureReasonPrefix is the ONE spelling of the reason a row
// carries when the run's child could not be exec'd. The value is the operator-
// facing sentence `task show` renders, and it is built HERE rather than at the
// call site so the trail event and the row's reason column cannot drift
// (Rules 1b/13).
const taskRunSpawnFailureReasonPrefix = "start child: "

// taskRunSpawnFailureReason renders the cause of a failed exec for BOTH records
// the seam writes — the `start-failed` trail event and the row's terminal
// reason. One function, so a reader of either face reads the same sentence.
func taskRunSpawnFailureReason(err error) string {
	if err == nil {
		return taskRunSpawnFailureReasonPrefix + "the child could not be started (no error reported)"
	}
	return taskRunSpawnFailureReasonPrefix + err.Error()
}

// taskRunSpawnChild starts the run's child and records the stage boundary on
// BOTH sides of cmd.Start(). It is the seam's one spawn door: a test drives it
// with a real exec.Cmd whose argv cannot be started, so the failure arm is
// exercised by the kernel's own answer rather than by a stub.
func taskRunSpawnChild(cmd *exec.Cmd, facts io.Writer, taskID, by string, started time.Time) error {
	fmt.Fprintf(facts, taskRunStageSpawningFmt, taskID, time.Now().Format(taskRunStageTimeLayout))
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(facts, taskRunStageSpawnFailedFmt, err, taskID,
			time.Since(started).Round(time.Millisecond), time.Now().Format(taskRunStageTimeLayout))
		// THE TRAIL RECORD: the exec failed, so NO child exists. The cause is
		// written on the row's append-only trail (`start-failed`) so the next
		// reader is not left guessing why a row never moved. The old code had
		// already written `in_progress` by this line, which is the shape epic
		// 01a1a87b removes: an in_progress row whose holder never had a
		// process.
		//
		// THE EVENT IS NOT THE VERDICT — deliberately. A same-status UPDATE
		// would bump updated_at and make the row look freshly re-armed (see
		// taskUpdatePreservingUpdatedAt's argument), and this seam does not
		// own a terminal verdict anyway: it reports a FACT about the spawn,
		// and the FAILING FUNNEL (runTaskRunOnce) turns it into the row's
		// `failed` writeback through the ordinary writeback seam. Splitting
		// them keeps one writer per fact (Rule 2): the seam says what the
		// kernel said, the funnel says what that means for the row.
		//
		// A record that cannot be written is PRINTED rather than swallowed
		// (Rule 15): the run is already failing, and a silent second failure
		// is how "claimed with no reason" happens.
		if recErr := tools.TaskStampStartFailure(taskID, taskRunSpawnFailureReason(err)); recErr != nil {
			fmt.Fprintf(os.Stderr, taskProgName+": %v\n", recErr)
		}
		// THE RETURN IS LOAD-BEARING and it is the ONLY exit from this arm.
		// Without it control falls through to the line below, where
		// cmd.Process is NIL after a failed Start — a nil-pointer panic in the
		// spawn seam itself, i.e. the failure that was supposed to be reported
		// crashes the reporter. Measured on the production binary 2026-11-08
		// (SIGSEGV pc=0xf4e2bc at cli_tasks.go:2206) after an edit dropped this
		// line while it was rewriting the comment above: no test in the suite
		// noticed, and only a real exec failure reached it (Rule 31).
		return err
	}
	fmt.Fprintf(facts, taskRunStageSpawnedFmt, cmd.Process.Pid, taskID, time.Now().Format(taskRunStageTimeLayout))
	// ═══ THE TRANSITION, AT THE MOMENT IT BECOMES TRUE (epic 01a1a87b) ═══
	//
	// `claimed → in_progress` is written HERE — after `cmd.Start()` returned
	// nil, i.e. after a child process exists — and nowhere earlier. This is the
	// whole epic: the status now states a process-lifecycle fact instead of a
	// lock fact, and the pid it records is the CHILD's, so `/proc/<pid>/stat`'s
	// start ticks can verify the very process the row claims (Rule: never a pid
	// number alone). The wrapper stays in `claimed_by` (who took the row is
	// unchanged); only the process-identity triple moves to the child.
	//
	// A refusal here is NOT swallowed: the child is running, so the run
	// continues (killing it would destroy work over a bookkeeping failure) but
	// the operator learns immediately, on stderr, that the row's status and the
	// world disagree.
	//
	// --takeover is deliberately NOT threaded through: takeover relaxes the
	// CLAIM, and by this line the claim is settled — passing it would let this
	// write overwrite a different runner's in_progress, which is the double-run
	// shape the CAS exists to prevent.
	if _, sErr := tools.TaskStartChildAdmitted(taskID, by, cmd.Process.Pid, false); sErr != nil {
		fmt.Fprintf(os.Stderr, taskProgName+": %s: the row could not be advanced to %s after the child exec'd "+
			"(pid %d) — the run CONTINUES, but the ledger and the world disagree: %v\n",
			taskID, tools.TaskStatusInProgress, cmd.Process.Pid, sErr)
	}
	return nil
}

// taskRunRecordReaped writes the stage boundary at cmd.Wait()'s return: the
// child's error and the duration it actually lived. Written on BOTH outcomes —
// a successful child is exactly as much a stage boundary as a failing one, and
// only having both makes the NEXT missing line meaningful.
func taskRunRecordReaped(facts io.Writer, taskID string, started time.Time, runErr error) {
	fmt.Fprintf(facts, taskRunStageReapedFmt, runErr, taskID,
		time.Since(started).Round(time.Millisecond), time.Now().Format(taskRunStageTimeLayout))
}

// runTaskRunOnce implements ONE run pass (the worker round's body): claim (or
// take the explicit already-claimed task), start it, execute the payload as a
// normal gopi run (session registration, GOPI.md, full tools — no second
// engine), then write the outcome back. The child's exit code decides
// completed/failed. Returns an error on ANY failure — it never exits the
// process (the serve daemon's loop depends on this: a failed run logs and
// moves to the next tick instead of killing the dispatcher). bug054: the
// child's session id is stamped into the task row as soon as its session
// JSONL appears (TaskBackfillSession — the early stamp, so `task show`
// mid-run already carries session_id + model), not deferred to the
// writeback; the writeback's backfills remain as the best-effort fallback
// when the early stamp misses. Callers: the worker round (workerRoundRun) — the
// loop logs a failed round and continues, a named row exits non-zero on it
// (runTaskWorkerExplicit); the serve daemon stamps and continues.
func runTaskRunOnce(opts *tasksOptions) error {
	// broken-pipe guard (t-1788305178 incident, 2026-09-02) — hoisted to
	// the TOP of the run funnel, BEFORE any claim/start/print: when `gopi
	// task run` is spawned by an external launcher (hermes terminal tool,
	// CI harness), stdout/stderr are a pipe whose read-end the launcher
	// holds. If the launcher closes the pipe mid-run, the wrapper's NEXT
	// stdout/stderr write gets EPIPE → Go raises SIGPIPE on fd 1/2 →
	// default disposition kills the wrapper (exit 141). TaskStart flips the
	// row to in_progress BEFORE the claim-lock lines / "running gopi with
	// task ... as briefing..." print below — a launcher (or the regression
	// test) that watches for in_progress and then closes the pipe leaves a
	// race window between that print and the old post-briefing
	// signal.Ignore. Ignoring here closes the window: every write this
	// function makes after this point is EPIPE-harmless. `gopi | head`
	// semantics are unaffected — this path runs only for the task-run
	// wrapper/serve/worker funnels. The CHILD does not inherit the ignore
	// (Go resets SIGPIPE at exec); its own ignore is gated on
	// GOPI_TASK_WRAPPER_ID in main().
	signal.Ignore(syscall.SIGPIPE)
	// SANDBOX PRECONDITION on the hermetic test escape (incident 2026-09-16, the
	// 5 burned rows): the funnel is where the escape actually fails a row, so an
	// armed escape with NO sandbox root refuses BEFORE the claim below — the row
	// keeps its status (Rule 15: loud, and no side effect). Inside a sandbox the
	// escape behaves exactly as before (the hermetic tests depend on it).
	if err := refuseIfUnsafeTaskChildFailEscape(taskChildFailVerbRun); err != nil {
		return err
	}
	target := opts.taskID
	// RUN-SCOPE LOCK (epic run-lock-per-run-dir): this process is about to
	// EXECUTE a run directory, so the FIRST thing it does is take that
	// directory's kernel lock (internal/runlock). An explicit target knows its
	// directory already, so it locks HERE — before the singleton guard, before
	// the claim CAS, before the session workdir move — which is what makes a
	// refusal leave the ledger completely untouched. The two arms inside the
	// block below learn their id from a claim, so they lock at the first
	// statement that has it (see run_lock_seam.go for why that limit exists and
	// what it still guarantees).
	runLockRelease := func() {}
	runLockHeld := false
	defer func() { runLockRelease() }()
	if target != "" && target != taskverbs.Next {
		rel, lerr := taskRunTakeLock(target)
		if lerr != nil {
			return lerr
		}
		runLockRelease = rel
		runLockHeld = true
	}
	if target == "" || target == taskverbs.Next {
		// WORKER round: the row is ALREADY claimed (workerRoundClaiming's CAS did
		// the claim) — claiming again here would steal the next pending
		// row and execute it WITHOUT the worker writeback contract. Pass
		// the claimed id through: TaskResumeClaimed re-stamps the claim
		// trail for this process, then the flow continues as a normal run.
		if workerWriteback != "" {
			target = workerWriteback
			if err := tools.TaskResumeClaimed(target, opts.as); err == nil {
				fmt.Printf("worker: resumed claimed task %s\n", target)
			} else if verdict, refusal, gErr := tools.TaskRunGuardFor(target); gErr == nil && verdict == tools.TaskRunHolderLive {
				// The resumption was REFUSED by the singleton guard: a live
				// OTHER process holds the row this round claimed. Executing
				// anyway would double-run it — the epic's P0. The refusal text
				// is the guard's (it names the holder), prefixed so the
				// worker's log attributes it to this round.
				return fmt.Errorf(taskProgName+": %s", refusal)
			}
		} else {
			res, err := tools.TaskNextFiltered(opts.as, opts.excludeSession, opts.skipTags)
			if err != nil {
				return fmt.Errorf(taskProgName+" error: %w", err)
			}
			if res.Empty {
				fmt.Println("no pending task")
				return nil
			}
			target = res.Task.TaskID
		}
		fmt.Printf("claimed: %s\n", target)
		// M4.12 repo guardrail: a claimed row with no deliverable.repo whose
		// text mentions a foreign project (vcmi-class) is warned at pickup —
		// advisory only, never a refusal (the runner decides with eyes open).
		for _, w := range tools.TaskRepoGuardWarn(target) {
			fmt.Printf("WARN %s\n", w)
		}
	} else {
		// SINGLETON GUARD (epic task-run-singleton-guard, 2026-09-15): an
		// explicit target may already be held by a LIVE process — a repeated
		// `gopi task run <id>`, or a duplicate of the row a worker round is
		// executing right now. The claim below does NOT arbitrate that (its
		// crash-recovery branch is a conditional UPDATE on status = claimed,
		// which two racers both win honestly), so the guard runs BEFORE any
		// write and refuses on a verified-live OTHER holder. Placement is
		// deliberate: AFTER the worker's own-claim pass-through above (a
		// worker re-entering ITS row is never refused) and BEFORE the session
		// workdir move below (entering the task's session directory is itself
		// a side effect a refused run must not leave behind).
		if opts.taskID != "" && !opts.takeover {
			// The pre-spawn door: TaskRunGuardFor judges the row BEFORE any
			// write (a refused run leaves the ledger and the session dir
			// untouched).
			verdict, refusal, gErr := tools.TaskRunGuardFor(opts.taskID)
			if gErr != nil {
				return fmt.Errorf(taskProgName+": singleton guard: %w", gErr)
			}
			if !tools.TaskRunGuardAdmits(verdict) {
				return fmt.Errorf(taskProgName+": %s", refusal)
			}
			switch verdict {
			case tools.TaskRunHolderSelf:
				// The row is already OURS: carry on to the run (the claim call
				// below re-stamps our own identity — no steal happens).
				fmt.Printf("[singleton-guard] %s is already claimed by this process — proceeding\n", opts.taskID)
			case tools.TaskRunHolderUnknown:
				// The holder cannot be judged here (a stamp naming no readable
				// process: another host, a previous run, a claim that could
				// not be read). Not evidence of a live run, so this run
				// proceeds AND records its own identity — which is what makes
				// the next runner's answer definite.
				fmt.Printf("[singleton-guard] %s: no judgeable live holder — proceeding and recording this run's process identity\n", opts.taskID)
			}
		} else if opts.taskID != "" && opts.takeover {
			fmt.Fprintf(os.Stderr, taskProgName+": --takeover: overriding the singleton guard for %s\n", opts.taskID)
		}
		// Explicit target: a pending row flows through the Phase-1 CAS
		// claim (TaskNextFiltered's UPDATE ... WHERE status =
		// tools.TaskStatusPending exactly-one-winner shape) — the M4.21
		// two-phase lock requires every claim to hold the local lock before
		// the remote leg. A CLAIMED row is the crash-recovery case (⑥): a
		// previous holder died between claim and start — reclaim it for
		// this run instead of refusing.
		if err := tools.TaskClaimExplicitForce(target, opts.as, opts.takeover); err != nil {
			return fmt.Errorf(taskProgName+" error: claim %s: %w", target, err)
		}
	}
	// RUN-SCOPE LOCK, the claim-first arms (see run_lock_seam.go): `target` is
	// final here — a worker round's already-claimed row, or the row a CAS just
	// handed this process — and it was NOT knowable any earlier, because the
	// claim is the call that produced it. The lock is taken before anything
	// downstream acts on the row: the dead-repo repair below can rewrite the
	// ledger, the session workdir move enters the directory, and the spawn
	// executes in it. A second process that reaches the same row is refused
	// HERE and writes nothing.
	if !runLockHeld {
		rel, lerr := taskRunTakeLock(target)
		if lerr != nil {
			return lerr
		}
		runLockRelease = rel
		runLockHeld = true
	}
	// DEAD deliverable.repo REPAIR, AT THE CLAIM/LAUNCH SEAM (epic 01a17980).
	// The row is about to be dispatched, and the FIRST thing dispatch does with
	// its repo is hand it to a channel that refuses a path that is not there
	// (clone channel: "is not usable: the directory no longer exists on disk").
	// Nineteen rows reached `failed` that way — killed at dispatch, before any
	// run, carrying a dead workspace path that every later attempt met again.
	//
	// So the row's repo is repaired IN PLACE here, before anything acts on it:
	// tools.TaskRepairDeadRepoInPlace resolves a reclaimed clone-channel
	// workspace back to the repository its own ledger row recorded
	// (RepairDurableRepoForSubmit) and writes it through the ONE deliverable
	// blob writer. A row naming a LIVE work tree — every normal row — is
	// identity and silent, and spawns no git process, which is what lets this
	// run on every dispatch.
	//
	// A repair that cannot be RECORDED is stated and the run continues: the
	// channel's own refusal is the loud, correct answer for a path that could
	// not be recovered, and aborting here would trade one opaque failure for
	// another (Rule 15).
	if line, rerr := tools.TaskRepairDeadRepoInPlace(target); rerr != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", taskProgName, rerr)
	} else if line != "" {
		fmt.Fprintf(os.Stderr, "%s: %s\n", taskProgName, line)
	}
	// SESSION WORKDIR (epic task-session-cwd, operator ruling 2026-09-12):
	// the row is claimed and WILL be executed by this process, so this
	// process now STANDS in the task's own session directory. Everything the
	// wrapper prints from here on (the claim-lock verdict, the child's
	// startup, the writeback) lands beside the task's JSONL, raw log, report
	// and attachments — one task, one directory, cleanable as a unit. The
	// start workdir was captured first and is handed to the child below
	// (taskExecutorWorkdir), so the EXECUTOR keeps the operator's working
	// context: the agent loop resolves the project's GOPI.md from cwd (it
	// walks up from there to the enclosing project's boundary), the
	// search tools index cwd, task_submit stamps deliverable.repo from cwd,
	// and bash's dangerous-command whitelist resolves relative targets
	// against cwd — none of which may silently become a log directory.
	//
	// Rule 15: a container that cannot be entered is a REFUSAL (this wrapper
	// exits non-zero through runTaskRun), never a silent fallback to running
	// with the task's output outside its own directory.
	line, wdErr := taskWorkdirEnter(target)
	if wdErr != nil {
		return fmt.Errorf("%s", taskWorkdirChdirRefuse(target, wdErr))
	}
	fmt.Println(line)
	// The move is undone on EVERY return path — not just the happy one. The
	// wrapper is expected to end the process (os.Exit after the terminal
	// writeback), but the funnel has many early error returns (TaskStart,
	// claim-lock yield, spawn failure) and any of them can leave a
	// LONG-LIVED caller standing in a task's session directory: the worker
	// loop would then run its next round — and print everything before the
	// claim — from round 1's container, and an in-process caller (the worker
	// tests driving runTaskRunOnce directly) would carry the move into every
	// later operation of that process. taskWorkdirRestore is idempotent and
	// a no-op for a process that never entered, so the deferred call costs
	// nothing on the exit path.
	defer taskWorkdirRestore()
	// The start is a CAS pinned to `claimed` for a target the guard admitted:
	// of two runners that both reached this line, exactly ONE advances the row
	// and the other is REFUSED (never a silent retry — that was the double-run
	// path). --takeover relaxes it, because the operator has asserted the
	// recorded holder is not to be trusted.
	// THE LAUNCH-SEAM ASSERTION (epic 01a1a87b, operator ruling 2026-11-05).
	//
	// This line USED to be `TaskStartForce(...)`: a claimed→in_progress write
	// that fired with NO child process in existence, and therefore made
	// `in_progress` mean "both claim locks are acquired" — a LOCK fact — while
	// `claimed` came to mean "the local lock is only half-acquired". Two
	// consequences, both measured: an `in_progress` row whose holder had no
	// child at all (the question "who is running it, and is that process still
	// there?" had no answer), and `claimed`/`in_progress` becoming equivalent at
	// every consumer that had to ask "is this row live?".
	//
	// The states now mean what a process-lifecycle status should mean:
	//
	//	claimed      ownership is decided; whether a child exists is NOT claimed
	//	in_progress  a child was exec'd successfully and is being waited on; its
	//	             pid is in the row's process-identity columns
	//
	// The transition moved DOWN to immediately after `cmd.Start()` succeeds
	// (see the marked block there). What must NOT be lost is the gate this
	// write used to double as — "of two racing runners exactly ONE proceeds" —
	// so it stays here as an ASSERTION on the claim it replaced: the row must
	// still be `claimed`, i.e. the claim this runner won is the one in force.
	// --takeover relaxes it exactly as it relaxed the CAS.
	if _, aErr := tools.TaskRequireClaimed(target, opts.takeover); aErr != nil {
		return fmt.Errorf(taskProgName+" error: start %s: %w", target, aErr)
	}
	// NO CLAIM-LOCK ROUND HERE — and this comment is the record of why.
	//
	// This used to be the runner's driver for the two-phase DISTRIBUTED lock
	// (M4.21): TaskStampLockPhase(local) → RunClaimLockProtocol (Phase-2 remote
	// CAS on every declared peer, 双锁齐) → stamp `lock_phase=both` on proceed,
	// revert to pending on yield. Three config gates stood in front of it
	// (`task_sync` off / no `sync_peers` / GOPI_SKIP_CLAIM_BROADCAST), so on a
	// one-host ledger it printed `[claim-lock] single-node: … local claim only
	// (no remote leg)` and proceeded — which is the whole protocol, restated as
	// a no-op with three ways to reach it.
	//
	// The protocol is GONE (batch 4 of the cross-node refactor): with no peer
	// declared there is nothing to CAS against, and a lock nobody can contest
	// is not a lock. What stands in its place is the claim that was ALREADY
	// won a few lines above — TaskRequireClaimed on the local pending→claimed
	// CAS — and there is nothing else to do before exec.
	//
	// So the single-node path prints no [claim-lock] line and is gated by
	// nothing but that CAS. `lock_phase` itself is untouched: it is genuinely
	// used by the merge executor, the staleness guard and the transfer leg
	// (TaskStampLockPhase, TaskLockPhaseBoth), and claimed→in_progress is still
	// written by TaskStartChildAdmitted after cmd.Start() succeeds — never
	// here, and never on a lock field.
	// THE WRAPPER KEEPS THE ROW'S HEARTBEAT ALIVE FROM HERE TO THE WRITEBACK.
	//
	// The claim above put the row in_progress and made THIS process its holder;
	// from this line until writebackOutcome lands, this process is the only
	// thing working on the row — and every heartbeat writer in the codebase
	// belongs to the ENGINE it is about to launch, not to this wrapper. So the
	// window between the child's exit and the writeback (the clone channel's
	// delivery leg: clean → pull --rebase → build → push, plus finish-cleanup's
	// branch check) had NO writer at all. Measured on row
	// 01a17a1b-496f-7179-9567-d544fa5922f9: last_toolcall_at 06:15:26 (the
	// engine's last stamp), ended_at 07:01:10 (this writeback) — 45.7 minutes of
	// live, in_progress work during which the liveness predicate saw a corpse.
	//
	// The stamp is a CLAIM ("this row is being executed right now"), and it is
	// true for exactly as long as this function is running, so the keepalive's
	// lifetime IS this function's lifetime. Placed here rather than beside
	// cmd.Wait() because the row is already in_progress and already claimed one
	// line above: the promise starts when the claim starts.
	stopKeepalive := tools.StartTaskRowKeepalive(context.Background(), target)
	defer stopKeepalive()

	// The briefing composes summary + payload (see taskRunBriefing) — the
	// child gets the FULL task text. A row with NEITHER field is a HARD
	// refusal: the e3ca35ca incident's bare-code-name dispatch burned a
	// context budget discovering what the task was.
	briefing, err := taskRunBriefing(target)
	if err != nil {
		return fmt.Errorf(taskProgName+" error: %w", err)
	}
	briefing = strings.Join([]string{briefing, config.NodeIdentityBriefing()}, "\n\n")
	// Run provenance stamp (devdoc/run-method-provenance.md): record HOW
	// this execution was launched + the exact re-runnable command line,
	// right after the run's start transition (TaskStart above + the
	// claim-lock protocol). manual = plain `gopi task run`; worker = the
	// worker loop round (identified by the workerWriteback latch — the
	// merger (a cronjob since daemon-machinery-removal 2026-09-17, with an
	// in-process repeat since merger-self-driving 2026-09-18) executes no
	// tasks since the
	// serve-worker-split, so it cannot appear here; dispatch and resume
	// stamp at their own sites). Best-effort: a stamp failure logs and
	// continues — never fails the run.
	if method, cmdline := runMethodFor(target, opts); method != "" {
		if err := tools.TaskStampRun(target, method, cmdline); err != nil {
			fmt.Fprintf(os.Stderr, taskProgName+": stamp run provenance for %s: %v (continuing)\n", target, err)
		}
	}
	fmt.Printf("running gopi with task %s as briefing...\n", target)

	// Execute as a subprocess gopi run: the child gets the payload on stdin
	// and inherits the environment (GOPI_API_KEY etc.). Its exit code is the
	// task's outcome; its session JSONL is the audit trail (cross-linked by
	// the writeback below). NO dispatcher flags leak into the child — the
	// sub-verb's own args belong to this process only.
	//
	// broken-pipe guard (t-1788305178 incident, 2026-09-02): the child's
	// stdout/stderr ARE the wrapper's stdout/stderr — which, when `gopi task
	// run` is spawned by an external launcher (hermes terminal tool, a CI
	// harness), is a PIPE whose read-end the launcher holds. If the launcher
	// exits / closes the pipe mid-run (turn end, SIGTERM, timeout kill), the
	// next write gets EPIPE → Go's runtime raises SIGPIPE on fd 1/2 →
	// default disposition kills the writer (observed: exit 141 / "signal:
	// broken pipe" after 18s, session JSONL truncated mid-run, the writeback
	// then records a spurious FAILED). THIS wrapper-side ignore protects the
	// wrapper's OWN post-spawn writes (claim lines, writeback) from the same
	// fate. (The wrapper-side SIGPIPE ignore is hoisted to the top of
	// runTaskRunOnce — see there. The child does NOT inherit it: Go resets
	// SIGPIPE to default at exec, so the child's own ignore lives in
	// main(), gated on GOPI_TASK_WRAPPER_ID.)
	started := time.Now()
	// Hermetic test escape (taskChildFailTestEnv=1): the serve-loop survival
	// regression test drives the REAL failed-child writeback + continue
	// path without spawning a child (no model call, no API key) — the child
	// is treated as failed with a synthetic error, exactly as a genuine
	// crash would surface (writebackOutcome failed + the error return that
	// keeps the serve daemon alive). Never set outside tests.
	if taskChildFailEscapeArmed() {
		runErr := fmt.Errorf("synthetic child failure (test escape)")
		elapsed := time.Since(started).Round(time.Second)
		writebackFailure(target, tools.TaskStatusFailed, "gopi run failed: "+runErr.Error()+taskAbnormalChildMarker+" — run: elapsed="+elapsed.String(), opts.reportTo, opts.repo)
		// The RELEASE TAIL runs on this return too (worktree-residue epic,
		// 2026-09-15): a FAILED run is the one whose ground is most likely to
		// be left behind, and it used to be the one path with NO close-out —
		// the reclaim + cwd release sat only on the success return below, so a
		// failed wrapper walked past both while still holding the row's
		// directory as its cwd. One implementation, shared with the success
		// path (Rule 14).
		taskRunReclaimOwnDirectories(target)
		// %w (not %v): this is the SAME message the real failed-child path
		// below emits, on purpose — the recursive path (child == wrapper
		// binary) must be probed identically from a test and from
		// production, and a synthetic error carries no formatting
		// requirement of its own. The two returns must stay identical.
		return fmt.Errorf(taskProgName+": task %s FAILED (child: %w)", target, runErr)
	}
	cmd := exec.Command(gopiExecutable())
	// THE RUN CHILD LEADS ITS OWN PROCESS GROUP (2026-09-25). Setpgid makes the
	// child a group leader (pgid == pid), which is what gives the run's stop paths
	// a process-group SELECTOR for its own descendants — without it a backgrounded
	// `bash -c … &` left behind by the child's tools carries the WRAPPER's group
	// and can only be recognised through the session census, which the wrapper
	// shares with everything else its launcher spawned (internal/tools/procsignal.go
	// carries the measurement and the ownership rules).
	//
	// THE MECHANISM THAT ORIGINALLY WANTED THIS IS DELETED (2026-11-05, 386731b5:
	// the run close-out reap). The shape STAYS because the stop paths still use it
	// — signalRunChild in cli_tasks_worker_stop.go measures Getpgid and only uses a
	// group signal when pgid == pid.
	//
	// NOTHING SIGNALS THIS GROUP IN PRODUCTION — the stop paths signal the child
	// DIRECTLY and its descendants individually (signalRunChild, which measures
	// Getpgid and only uses a group signal when pgid == pid), so a group leader
	// is exactly the shape that lets an exact group signal exist without
	// changing what the child's own children see. Re-parenting, session
	// membership and cwd are all untouched: the child stays in this process's
	// session, so the close-out still speaks to it by session (Rule 24).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// THE CHILD'S BRIEFING IS BOUND LATER (see the ground resolution below):
	// cmd.Stdin is a reader built from the briefing STRING at the instant it is
	// assigned, and the ground announcement is appended to that string at the
	// ground resolution — so the reader must be built AFTER it. The order is
	// load-bearing, not stylistic: setting Stdin here and binding the clause
	// below produced a run whose child stood in the announced ground while its
	// first model request carried NO announcement at all (measured 2026-09-25 —
	// the clause was written into a string nobody read).
	// THE CHILD'S STREAM DESTINATION (epic child-stream-task-output): the
	// child's stdout/stderr go to the ROW's own session directory, resolved
	// from the task id at this ONE seam — so every spawn path (manual
	// `task run`, a worker round, a serve tick) writes the task's text to the
	// SAME file, <session-dir>/output.log, instead of inheriting whatever
	// stream the spawner happened to hold. See cli_tasks_exec_output.go.
	//
	// Rule 15: an unwritable destination is REPORTED and the child keeps the
	// spawning stream — a run whose output nobody can find must not be
	// silently indistinguishable from an unattributed one, and refusing the
	// run outright here would strand a claimed row on a full disk. The path
	// is recorded for the writeback stamp in either case ("" when the log
	// could not be opened, so the stamp is simply omitted).
	childOut, outErr := taskExecutorOutputLog(target)
	if outErr != nil {
		fmt.Fprintf(os.Stderr, taskProgName+": %s: %v — the child's output stays on this stream\n", target, outErr)
	}
	if childOut != nil {
		defer childOut.Close()
		cmd.Stdout, cmd.Stderr = taskChildStreamWriters(childOut)
		// Every line this seam prints ABOUT the run joins the run's own
		// record AND stays on the spawning stream (see execRunFacts): the
		// operator running `task run` still sees the resolution as it
		// happens, and a later agent handed only the task id finds the same
		// account in the run's own file.
		execRunFacts = io.MultiWriter(childOut, os.Stderr)
		// The task's own stream record: printed on the SPAWNER's stream, so
		// whoever launched this run learns where its output went (Rule 15 —
		// the destination is announced, never guessed at).
		fmt.Fprintf(os.Stderr, taskOutputLogAnnounceFmt, target, taskExecutorOutputLogPath)
		// The SECOND half of the destination, announced too (epic
		// worker-foreground-console): whether the child's stream is ALSO
		// copied to this process's console, or suppressed because this
		// spawner is a detached daemon — the run log's own copy of this line
		// is the one an evidence reader finds later.
		consoleNote := taskOutputConsoleFmt
		if taskChildConsoleWriter() == nil {
			consoleNote = taskOutputNoConsoleFmt
		}
		fmt.Fprintf(execRunFacts, consoleNote, target)
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		// No run log: the seam's own lines stay on the spawning stream (the
		// latch must not point at a CLOSED file from a previous run).
		execRunFacts = os.Stderr
	}
	// THE CHILD'S DYING WORDS ARE KEPT (epic failed-row-reason-facts). The
	// wrapper redirects the child's stderr into the run log above, so at the
	// instant cmd.Wait() returns it has already seen — and discarded — whatever
	// the child printed while failing. The ring is attached HERE, once, and
	// covers BOTH destination branches above and every spawn path through this
	// seam (manual `task run`, a worker round, a serve tick): one latch, one
	// reader (the failure arm below), the same single-writer shape as
	// taskExecutorOutputLogPath. Writes to it never error, so the child's
	// stream cannot be broken by the diagnostic (Rule 15: the record is
	// additive, the stream is primary).
	taskExecutorStderrTail = newTaskChildStderrTail(taskChildStderrTailMaxBytes)
	cmd.Stderr = io.MultiWriter(cmd.Stderr, taskExecutorStderrTail)
	// Execution-context resolution (daemon-context-isolation epic,
	// 2026-09-13) — TWO decisions, both taken here because this is the ONE
	// spawn seam for every executor child (worker round, manual `task run`,
	// serve tick all funnel through runTaskRunOnce; Rule 2):
	//
	//  ① CWD = THE TASK'S PROJECT, not the spawner's launch directory. A
	//     resident service was launched from some directory of its own; a
	//     child that inherits it loads THAT project's GOPI.md, indexes ITS
	//     tree for code_search and resolves relative guard targets against
	//     it — i.e. task A runs under project B's rules. The resolution is
	//     tools.TaskRepoFor(task): the row's deliverable.worktree when the
	//     executor already created one, else its deliverable.repo. A row
	//     with NO repo keeps today's behaviour (the launch workdir) and the
	//     fallback is REPORTED, never silent (Rule 15).
	//  ② ENV = the launch environment MINUS the spawner's own execution
	//     context (tools.DaemonContextEnvVars: the agent-session handle and
	//     its inherited anchor). A service launched from an agent's shell
	//     carries that pair; without the strip every task it executes
	//     inherits a foreign session handle and reads as a descendant of an
	//     agent session rather than as the fresh session it is.
	launchWorkdir := taskExecutorWorkdir()
	execWorkdir, cwdSource := taskExecutorCwd(target, launchWorkdir)
	taskRunRepoDir = execWorkdir
	// PRODUCER-SIDE CWD ESCAPE (epic daemon-cwd-escape, 2026-09-16): the
	// resolution above gives the child the TASK's project — but when the row
	// carries no deliverable repo that answer is the LAUNCH workdir, and a
	// resident service's launch workdir is wherever it was started. If
	// that is a task's linked worktree (the merger spawned from an agent
	// session's own task worktree — the live incident), the child stands in a
	// FOREIGN worktree for the whole executor run: a foreign holder inside
	// guard ③'s window, blocking that worktree's recycle for nothing. The
	// child's cwd is therefore only kept when it is neutral or the task's own
	// ground; otherwise it escapes to the MEASURED process root, and the
	// escape is reported by name (Rule 15).
	//
	// NOTE the shape: EscapeFromWorktree's FIRST return is meaningful ONLY
	// together with ok=true ("keep what you passed in" is spelled "" — the
	// package's contract). Assigning it unconditionally is the exact mistake
	// that dropped the task's own project from the child's cwd (measured: five
	// cmd/agent e2e walks put every executor child in the session dir), so the
	// resolved value is the DEFAULT here and the escape overwrites it.
	childDir := executorChildDir(execWorkdir, target, launchWorkdir)
	// THE CHILD'S GROUND (epic worker-creates-child-ground, 2026-09-22):
	// resolve-or-CREATE the directory a repo-aware row must be executed in —
	// a linked worktree of its repo, on its branch — BEFORE the child is
	// spawned. Until this seam existed, the ground was minted mid-run by the
	// LLM (PROJECT.md Rule 6a), so at spawn time there was nothing to resolve to
	// and childDir fell back to the row's deliverable.repo — the MAIN
	// checkout. The spawn was then correctly configured to start in the wrong
	// place: legal, wrong, and invisible, so the reclaim guard that scans
	// /proc/*/cwd was honest and blind (measured 2026-09-22: three `task run`
	// children all started in the main checkout, and a live child stood in the
	// main checkout while its own worktree was freed out from under it).
	//
	// A refusal here is LOUD (Rule 15 / the briefing's item 3): the ground is
	// a precondition of the run, not a best-effort side channel, so an
	// unresolvable ground RECORDS the reason on the row and returns without
	// spawning. Never a silent fallback to the main checkout — that is the one
	// shape that made this defect invisible for a day.
	//
	// THE CLONE CHANNEL (settings dev_repo, 2026-10-18) is resolved BEFORE the
	// worktree ground and wins when it is ON: the same `gopi worker` call, the
	// same arguments — the workspace is a clone of the declared dev_repo rather
	// than a linked worktree. When it is OFF (dev_repo empty/absent) this is a
	// nil that changes nothing below, which is the whole compatibility
	// contract. The loop that follows the child (pull --rebase → build → push,
	// its brake, the reclaim) is run in the close-out half of this function.
	//
	cloneRun, cloneGround, cloneErr := taskRunCloneChannelFor(target, execRunFacts)
	if cloneErr != nil {
		refusal := tools.TaskRunGroundRefusal(target, cloneErr)
		fmt.Fprintf(execRunFacts, "%s\n", refusal)
		fmt.Fprintln(os.Stderr, refusal)
		if _, rerr := tools.TaskUpdateReason(target, tools.TaskStatusFailed, refusal, ""); rerr != nil {
			fmt.Fprintf(os.Stderr, "%s: recording the refusal on task %s failed: %v\n", taskProgName, target, rerr)
		}
		writebackFailure(target, tools.TaskStatusFailed, refusal, opts.reportTo, opts.repo)
		return fmt.Errorf("%s", refusal)
	}
	// THE WORKTREE CHANNEL IS CONSULTED ONLY WHEN THE CLONE CHANNEL IS OFF.
	//
	// It used to be called UNCONDITIONALLY and its answer then overwritten when
	// the clone channel was on — so every clone-channel run still ran this
	// resolver, whose creation arm executed a `git worktree add` whose directory
	// was discarded one line later (measured on this host: the run log carries
	// `CREATED the ground /home/user/workspace/gopi-01a13207597b…`, and that
	// directory is gone). The creation arm STAYS — it is the fallback ground on
	// a host that declares no dev_repo, and deleting it turned 41 e2e walks red
	// (2026-10-27; the 41 is the SET DIFFERENCE r1-r2 between the two runs'
	// raw streams — r2-r1 = 0, r1∩r2 = 15 pre-existing — not one run's count,
	// see devdoc/worktree-removal-census.md §1.0.1) — but it is not this
	// host's path, so it is no longer run
	// here. Two channels, one of them per host; nothing pays for both.
	var ground tools.TaskGroundResolution
	var groundErr error
	if cloneRun == nil {
		ground, groundErr = tools.TaskRunGroundFor(target)
	}
	if cloneRun != nil {
		// The clone channel's ground REPLACES the worktree resolution: the row
		// works in its own clone, so no linked worktree is minted and nothing
		// in the worktree channel's rules (main-checkout refusal, peer
		// baseline, gopi-<stem> naming) applies.
		ground, groundErr = cloneGround, error(nil)
	}
	if groundErr != nil {
		refusal := tools.TaskRunGroundRefusal(target, groundErr)
		fmt.Fprintf(execRunFacts, "%s\n", refusal)
		fmt.Fprintln(os.Stderr, refusal)
		// The row carries WHY (Rule 15): a refusal that only reaches stderr is
		// invisible to the next reader of the ledger, and the row would sit
		// claimed-but-unrunnable with no recorded reason.
		if _, rerr := tools.TaskUpdateReason(target, tools.TaskStatusFailed, refusal, ""); rerr != nil {
			fmt.Fprintf(os.Stderr, "%s: recording the refusal on task %s failed: %v\n", taskProgName, target, rerr)
		}
		writebackFailure(target, tools.TaskStatusFailed, refusal, opts.reportTo, opts.repo)
		return fmt.Errorf("%s", refusal)
	}
	if ground.Dir != "" {
		childDir = ground.Dir
		// The push leg's anchor follows the ground: `git -C <ground>` reads the
		// branch the executor actually works on (the row's branch, checked out
		// by the ground), which is what the report-back push must deliver. It
		// replaces the pre-epic value (the row's bare repo — the MAIN checkout,
		// whose branch is whoever is standing there).
		taskRunRepoDir = ground.Dir
		fmt.Fprintf(execRunFacts, taskGroundLineFmt, ground.Dir, target, ground.Detail)
		// THE GROUND ANNOUNCEMENT (epic ground-announcement, 2026-09-25): the
		// worker created the ground and set cmd.Dir into it, but NOTHING told
		// the child so — while PROJECT.md Rule 6a tells every run to create "a
		// worktree for YOUR BRANCH" (~/workspace/gopi-<descriptive>), and the
		// ground's branch is task/<uuid>, which matches nothing in that literal
		// test. So every run obeyed Rule 6a and left an ORPHANED SPARE worktree:
		// created, never entered, never held, named by no row, collected by no
		// sweep (measured live: one task → two worktrees, both refs carrying the
		// same commits; the child worked in the ground the whole time).
		//
		// TWO CHANNELS, one decision, and both are needed:
		//  ① the BRIEFING clause — the ONLY channel that reaches the MODEL, and
		//     the model is who reads Rule 6a. It is bound into the briefing
		//     BEFORE the spawn (below), alongside the node-identity block.
		//  ② the ENV pair — the same fact in the child's environment, for the
		//     executor's own code and for any descendant gopi it spawns.
		// Neither is a fallback for the other: a fact the model cannot see is a
		// fact it cannot act on, and a fact only the model sees is one the
		// process tree cannot read back (Rule 15 — the announcement is not
		// silent, and the record below names it).
		if clause := tools.TaskGroundAnnouncementClause(ground.Dir, ground.Branch); clause != "" {
			briefing = strings.Join([]string{briefing, clause}, "\n\n")
			// THE READER NEEDS NO REBINDING HERE (epic cmd-agent-e2e-seam
			// -stability, 2026-09-25): cmd.Stdin is bound ONCE, after every
			// mutation of `briefing` — see the binding below the ground
			// resolution. This spot used to be the rebinding seam, and that
			// is exactly what left the repo-less row shape (no ground) with a
			// NIL stdin: a child with no prompt prints the help and exits 1.
			fmt.Fprintf(execRunFacts, taskGroundAnnounceLineFmt, target, ground.Dir, tools.GroundBranchLabel(ground.Branch))
		}
		// THE PUBLISH CLAUSE (measured 2026-10-21 on the server: four refs the
		// clone channel never creates, each one `push` in its reflog, each
		// matching the run's own logged `git push origin HEAD:task/<id>` — see
		// internal/tools/git_publish_target_guard.go). The machine check
		// (bash.go step 2e) refuses that push at the moment it is attempted;
		// this is the half a check cannot be, because the run that knows the
		// route never forms the decision. It is rendered for the CLONE CHANNEL
		// ONLY: a linked worktree's commit is already on its branch in the
		// shared repo (Rule 6c's first half), and that path's briefing is
		// unchanged. Briefing scope and enforcement scope are the same
		// condition (cloneRun != nil ⇔ the guard can fire), so no run is ever
		// told about a rule that cannot apply to it.
		if cloneRun != nil {
			if clause := tools.TaskBriefingPublishClause(ground.Branch); clause != "" {
				briefing = strings.Join([]string{briefing, clause}, "\n\n")
			}
		}
		// THE AUTHORITY CLAUSE (epic 01a0db25, measured on live row
		// 01a0dabe-3fa1-7cc8-b601-66f1456b8ffd): the announcement above says
		// WHERE the ground is; it does not say which tree has AUTHORITY over
		// the run's writes — and a payload naming the repo's main checkout
		// takes that authority away in one sentence ("Repo: /home/user/work
		// /GoPi (branch master at …). Re-verify every line number before
		// relying on it."). That run wrote its evidence artifacts and a new
		// test file into the SHARED checkout. The clause names the ground as
		// the only tree the run writes and resolves the collision explicitly,
		// BEFORE the model reads the payload — appended AFTER the announcement
		// so the last word on the subject is the authority, and bound into the
		// string that cmd.Stdin below hands the child (one binding, no
		// rebinding seam).
		if clause := tools.TaskBriefingAuthorityClause(ground.Dir, ground.MainCheckout); clause != "" {
			briefing = strings.Join([]string{briefing, clause}, "\n\n")
		}
		// THE EVIDENCE DESTINATION (2026-09-26, row 01a0dbe4 on the HP main
		// checkout): the clause above is a FENCE with no door — it says which
		// tree the run may write and says nothing about where the artifacts a
		// run must write (pixel captures, dumps, logs) are legal. That run
		// obeyed the fence for every tracked file and INVENTED `<repo>/scratch/`
		// for its evidence: ~161 untracked files in the operator's checkout,
		// because Rule 22 forbids the `/` partition, .gitignore entries and
		// reapers were ruled out, and no directory in its text was legal.
		//
		// The destination is not invented here either: it is the run's OWN
		// session-artifacts directory — the one `taskreport.PathEnv` (exported
		// a few lines below) already points into, resolved through the same
		// tools.TaskReportPath the report contract uses, so the two handles on
		// that directory cannot drift. LAST, so the run's own payload text is
		// earlier and this is the final word on where things go.
		if clause := tools.TaskGroundEvidenceClause(target, ground.Dir); clause != "" {
			briefing = strings.Join([]string{briefing, clause}, "\n\n")
		}
		// THE BRIEFING FRESHNESS REFRESH (epic briefing-premise-freshness,
		// measured on row 01a12c7b, 2026-10-12): a briefing's sentence "row X's
		// fix never landed on master" is a MEASURABLE statement about the object
		// graph — but it is measured when the ROW IS QUEUED, and by the time the
		// run receives it the commit may be an ancestor of the integration
		// branch. Measured: the row was handed a snapshot taken at 03:41:45;
		// the fix it claimed was missing (9867ed461) had reached origin/master
		// at 03:44:05 — so the run spent its whole budget re-implementing landed
		// work and then had to deduplicate against a branch nobody had told it
		// about. Nothing in the pipeline disagreed with the sentence.
		//
		// So the premise is RE-MEASURED here — at the ONE spawn seam, against
		// THIS RUN's own ground (ground.Dir), before the briefing is bound to
		// the child's stdin below — and a clause is appended ONLY for a claim
		// the measurement contradicts. The judgement is not re-implemented:
		// tools.MasterClaimEvidenceAt owns "is <rev> an ancestor of the
		// integration branch" and its exit status IS the verdict (Rules 8/13 —
		// reuse, never a second git prober).
		//
		// ADDITIVE BY CONTRACT, exactly like the announcement, the authority
		// clause and the evidence destination above: best-effort, never blocks a
		// claim, and never returns an error. Every claim examined leaves a line
		// in the record — INCLUDING the arms that measured nothing, which are
		// STATED rather than read as a pass (Rule 15). APPENDED LAST on purpose:
		// the clause contradicts the briefing's own text, so it is the final
		// word on the subject before the model reads the payload.
		premiseClause, premiseRecord := tools.TaskBriefingPremiseRefresh(ground.Dir, briefing)
		for _, line := range premiseRecord {
			fmt.Fprint(execRunFacts, line)
		}
		// The append decision and the appended bytes come from ONE call
		// (tools.BriefingWithClause): a caller re-deriving "did it stamp?"
		// from the text can print a marker about a briefing the child
		// received unchanged.
		var premiseAppended bool
		briefing, premiseAppended = tools.BriefingWithClause(briefing, premiseClause)
		if premiseAppended {
			fmt.Fprintf(execRunFacts, taskBriefingPremiseLineFmt, target, ground.Dir, tools.IntegrationBranchName)
		}
		// THE BASE CLAUSE (epic 01a136a7, measured on this row's OWN briefing,
		// 2026-10-14): the sibling above re-measures a sentence that ASSERTS the
		// integration branch lacks something; this one re-measures the revision
		// a briefing declares its own measurements were taken at — the claim
		// shape with no assertion in it, and therefore the one the sibling is
		// structurally blind to. The motivating briefing said "on master, `gopi
		// tool doccheck` exits 1 (row #1212, base 2f697e5)" and demanded a
		// permit-table repair 20 minutes AFTER master's tip had already become
		// the repair, whose parent is that very base: the base is a claim about
		// the object graph, and commit DISTANCE is what decides it.
		//
		// WHAT IT MAY CONCLUDE is the whole point, and it is narrower than the
		// sibling's: a base behind the tip means the briefing's measurement is
		// OLDER THAN THE BRANCH — never that the defect is gone. So the clause
		// says RE-MEASURE BEFORE REPAIRING, and says in its own words that it is
		// NOT evidence the work is done. ADDITIVE by the same contract as
		// everything above it: best-effort, never blocks a claim, never errors,
		// and every base examined leaves a line in the record — the positive
		// control (a base that IS the tip) and the unmeasured arms included
		// (Rule 15).
		baseClause, baseRecord := tools.TaskBriefingPremiseBaseRefresh(ground.Dir, briefing)
		for _, line := range baseRecord {
			fmt.Fprint(execRunFacts, line)
		}
		// The append decision and the appended bytes come from ONE call
		// (tools.BriefingWithClause): a caller re-deriving "did it stamp?"
		// from the text can print a marker about a briefing the child
		// received unchanged.
		var baseAppended bool
		briefing, baseAppended = tools.BriefingWithClause(briefing, baseClause)
		if baseAppended {
			fmt.Fprintf(execRunFacts, taskBriefingBaseLineFmt, target, ground.Dir, tools.IntegrationBranchName)
		}
		// THE GATE-ASSERTION CLAUSE (epic 01a14242, measured on row
		// 01a136be-e3b2-71b9-9219-0b4a2258feff, 2026-10-16). The base clause
		// above re-anchors the revision the briefing NAMED; this one covers the
		// assertion shape that names NO revision at all — "`<command>` is RED on
		// master". The motivating briefing was written 2026-10-14 03:30:42 (master
		// tip 82605ae57, the defect real) and DISPATCHED two days later, after the
		// repair had landed (f468c3101, +22 min; on master by 05:35:57) — measured
		// on that row's own text: the base half returned claims=0 and the sibling
		// unlanded half fired on nothing, so NOTHING on the row said its premise
		// had died, and the dispatched run paid to re-derive that from scratch.
		//
		// The moment the assertion was made is the moment its TEXT was WRITTEN —
		// the one anchor such a sentence carries. That is NOT always the row's own
		// created_at: a rework/resume successor (`<id>-rN`, tasktags.ReworkLineageTag)
		// inherits its parent's payload VERBATIM, so its clock says when the ROW was
		// minted, not when the briefing was written. Measured 2026-10-27 on this
		// ledger (rows 01a159e5-… and its −r1 successor, repo /home/user/work/gopi):
		// anchoring on the successor's created_at (epoch 1793040588) resolved the
		// assertion to 0d313323 — 42 commits behind the tip — while the text's own
		// moment (epoch 1792509551) resolves to bf401850, 398 commits behind. (The
		// same two clocks read 0d313323 at distance 8 vs bf40185 at 381 when the
		// row was filed, six days earlier in the branch's life.) Either way the
		// stamp is precise and points at a revision the text was never written at,
		// which is worse than no stamp: two runs went to reproduce a red that had
		// never existed there.
		//
		// So the anchor is resolved by the ledger accessor that WALKS the copy
		// (tools.TaskBriefingAuthoringAt: lineage edge + a proven verbatim prefix +
		// the recorded payload-revision trail), and it is passed IN — this seam
		// already holds the row, and the clause function reads no ledger of its own.
		// A moment that cannot be established is STATED and not stamped: an anchor
		// invented by the seam would be a false positive on an honest briefing
		// (Rule 15), and a GUESSED one is exactly the defect this replaces.
		//
		// WHAT IT MAY CONCLUDE is the same narrow reading as its base sibling: a
		// branch that moved past the authoring revision is a fact about the
		// briefing's AGE, never about the defect — so the clause says RE-MEASURE
		// THE SURFACE BEFORE REPAIRING and denies the completion reading in its
		// own words. ADDITIVE by the same contract as everything above it:
		// best-effort, never blocks a claim, never errors, and every assertion
		// examined leaves a line in the record — the positive control (a row
		// authored AT the tip) and the unmeasured arms included (Rule 15).
		authoredAt, hasAuthored, authoredRecord, authoredErr := tools.TaskBriefingAuthoringAt(target)
		for _, line := range authoredRecord {
			fmt.Fprint(execRunFacts, line)
		}
		if authoredErr != nil {
			fmt.Fprintf(execRunFacts, taskBriefingAuthoredUnreadLineFmt, target, authoredErr)
		}
		if !hasAuthored {
			authoredAt = 0
		}
		gateClause, gateRecord := tools.TaskBriefingGateAssertRefresh(ground.Dir, authoredAt, briefing)
		for _, line := range gateRecord {
			fmt.Fprint(execRunFacts, line)
		}
		// The append decision and the appended bytes come from ONE call
		// (tools.BriefingWithClause): a caller re-deriving "did it stamp?"
		// from the text can print a marker about a briefing the child
		// received unchanged.
		var gateAppended bool
		briefing, gateAppended = tools.BriefingWithClause(briefing, gateClause)
		if gateAppended {
			fmt.Fprintf(execRunFacts, taskBriefingGateAssertLineFmt, target, ground.Dir, tools.IntegrationBranchName)
		}
		// THE FIGURE CLAUSE (epic 01a137cb, measured from row 01a13781's incident,
		// 2026-10-14). The base clause above fires on the DISTANCE between the
		// declared base and the integration tip, and its own positive control
		// deliberately stamps NOTHING when that distance is zero — so a briefing
		// whose quoted figure has gone stale while its base declaration is CURRENT
		// (or which declares no base at all) reached the child with no re-measure
		// instruction at all, and the child paid to re-derive a measurement that
		// was already green. This clause covers that shape: it re-measures each
		// population figure the briefing quotes AGAINST THIS GROUND's own tree.
		// ADDITIVE by the same contract as everything above it: best-effort, never
		// blocks a claim, never errors, silent on the ordinary briefing that quotes
		// no figure, and every figure examined leaves a line in the record — the
		// positive control (a figure this ground agrees with) and the unmeasured
		// arms included (Rule 15).
		countClause, countRecord := tools.TaskBriefingCountRefresh(ground.Dir, briefing)
		for _, line := range countRecord {
			fmt.Fprint(execRunFacts, line)
		}
		// The append decision and the appended bytes come from ONE call
		// (tools.BriefingWithClause): a caller re-deriving "did it stamp?"
		// from the text can print a marker about a briefing the child
		// received unchanged.
		var countAppended bool
		briefing, countAppended = tools.BriefingWithClause(briefing, countClause)
		if countAppended {
			fmt.Fprintf(execRunFacts, taskBriefingCountLineFmt, target, ground.Dir)
		}
		// THE PINNED PATH-SET CLAUSE (epic 01a1447c's incident, 2026-10-16). The
		// figure clause above re-measures a figure this ground can re-derive; this
		// one covers the shape it cannot: a briefing that PINS A SET OF PATHS
		// together with its count and writes an acceptance criterion against it.
		// Measured on the incident's own briefing text: the pinned twelve paths
		// were a snapshot the previous row took at 19:11, a sibling commit
		// (cc9c1fee8 — authored 18:16, merged 19:34) had already re-derived every
		// one of them, and the tree's real population was a DIFFERENT twelve, two
		// of which were class moves no cell re-derivation can clear — so the
		// criterion was unreachable as written and the child had to discover that
		// itself. The figure clause returned 0 on that text (it counts in CHINESE:
		// "12 行"), the gate-assertion scanner returned 0 (no RED verdict word), and
		// the base clause, which DID stamp, says only "run that surface again" —
		// nothing about a SET being the moving part.
		//
		// It does NOT re-derive the set: that costs a full classifier pass, paid by
		// EVERY dispatch to serve a rare shape, so the clause states the instruction
		// and the command shape and says in its own words that no measurement was
		// taken here (Rule 15). The one thing it DOES measure is cheap and exact:
		// which pinned paths this ground can answer for at all.
		// ADDITIVE by the same contract as everything above it.
		pathSetClause, pathSetRecord := tools.TaskBriefingPathSetRefresh(ground.Dir, briefing)
		for _, line := range pathSetRecord {
			fmt.Fprint(execRunFacts, line)
		}
		if pathSetClause != "" {
			briefing = strings.Join([]string{briefing, pathSetClause}, "\n\n")
			fmt.Fprintf(execRunFacts, taskBriefingPathSetLineFmt, target, ground.Dir)
		}
		// THE DEAD -run ALTERNATIVE CLAUSE (bug row 01a18f76, measured
		// 2026-10-31 on an epic's own acceptance line). That line read
		// `go test ./internal/tools/ -run 'TestWorktreeRemovalGroundGate|TestTaskReclaimSelfDelivery|TestEveryWorktreeRemovalArmQuestionsTheGround|TestSelfDelivery'`
		// — and TWO of those four names exist nowhere in the repo. `go test`
		// prints `testing: warning: no tests to run` for a pattern that matches
		// nothing and exits 0, so those families would never have run while the
		// acceptance line read green. Every sibling clause above re-measures
		// what the briefing ASSERTS about the tree; this one re-measures what
		// the briefing SELECTS in the tree — the one population a source scan
		// answers exactly, with no subprocess and no run.
		// ADDITIVE by the same contract as everything above it: best-effort,
		// never blocks a claim, never errors, silent on the ordinary briefing
		// that names no -run pattern, and every alternative examined leaves a
		// COUNT in the record so a zero is visible against its neighbours.
		runAltClause, runAltRecord := tools.TaskBriefingRunAlternativesRefresh(ground.Dir, briefing)
		for _, line := range runAltRecord {
			fmt.Fprint(execRunFacts, line)
		}
		// The append decision and the appended bytes come from ONE call
		// (tools.BriefingWithClause): a caller re-deriving "did it stamp?"
		// from the text can print a marker about a briefing the child
		// received unchanged.
		var runAltAppended bool
		briefing, runAltAppended = tools.BriefingWithClause(briefing, runAltClause)
		if runAltAppended {
			fmt.Fprintf(execRunFacts, taskBriefingRunAltLineFmt, target, ground.Dir)
		}
		// THE PROSE-NAMED TEST CLAUSE (bug row 01a18f86, measured 2026-10-31 on
		// the SAME defect the block above repairs, one shape over). That epic
		// named its tests in a SENTENCE — 「三条 delivery-retry 测试 … 必须转绿」 —
		// and two of those names exist nowhere on that ground; `go test -run
		// '<them>'` printed `no tests to run` and exited 0, so the run cited a
		// green that measured nothing. The block above reads `-run` TOKENS and is
		// silent on a sentence by its own contract (evidence/red-green-before.txt
		// in this row's session directory), which is why this one exists: it
		// censuses every `Test…` identifier the briefing names in prose against
		// this ground's whole test corpus. It is a WARNING, never a refusal — a
		// legitimate epic may name a test the child is being asked to WRITE.
		proseTestClause, proseTestRecord := tools.TaskBriefingProseTestNamesRefresh(ground.Dir, briefing)
		for _, line := range proseTestRecord {
			fmt.Fprint(execRunFacts, line)
		}
		// Same ONE decision at the append seam as every sibling above
		// (tools.BriefingWithClause): the bytes and the flag cannot disagree.
		var proseTestAppended bool
		briefing, proseTestAppended = tools.BriefingWithClause(briefing, proseTestClause)
		if proseTestAppended {
			fmt.Fprintf(execRunFacts, taskBriefingProseTestLineFmt, target, ground.Dir)
		}
		// THE PARTIAL-MATCH CLAUSE (the EIGHTH half, and this epic's own case).
		// The sixth clause above is silent by construction on a selector that
		// reaches SOME test: `go test -run 'Rule13|Rule16|NamedLimits|ErrorWrap'
		// ./internal/standards/` reached exactly ONE of that package's 222 test
		// functions (the `ErrorWrap` fragment), really ran it, really PASSed and
		// really exited 0 — so no `no tests to run` marker appears, no exit-5
		// emptiness verdict fires, and the Rule 13 red it was written to confirm
		// was never touched while the console read green. This clause censuses the
		// SAME alternatives through the SAME predicate and reports the PARTIAL
		// shape by name (which alternatives are dead, which are live, how many
		// tests each reaches), with a pasteable replacement.
		// ADDITIVE by the same contract as every sibling above it: best-effort,
		// never blocks a claim, never errors, silent on a wholly live selector.
		runAltPartialClause, runAltPartialRecord := tools.TaskBriefingRunAltPartialRefresh(ground.Dir, briefing)
		for _, line := range runAltPartialRecord {
			fmt.Fprint(execRunFacts, line)
		}
		// Same ONE decision at the append seam (tools.BriefingWithClause): the
		// bytes and the flag cannot disagree.
		var runAltPartialAppended bool
		briefing, runAltPartialAppended = tools.BriefingWithClause(briefing, runAltPartialClause)
		if runAltPartialAppended {
			fmt.Fprintf(execRunFacts, taskBriefingRunAltPartialLineFmt, target, ground.Dir)
		}
		// THE BASELINE CLAUSE (epic 01a12d66, measured on rows 01a12d24 and
		// 01a12d66 itself, 2026-10-12): a briefing's acceptance criterion is
		// often phrased against the INTEGRATION BRANCH — «`git diff --stat
		// master...HEAD -- internal/` 为空» — but on a client host this ground
		// was cut from the PEER's master tip while the LOCAL refs/heads/master
		// lags it (measured: 55 commits on 01a12d24, 83 here), so that
		// three-dot diff reports the whole INHERITED population and is
		// non-empty for a run that changed nothing. The criterion is the
		// submitter's own prose and stays untouched; what is added is the
		// baseline it must be re-based on — the commit the branch was cut at,
		// read off the branch's own reflog and rendered as a runnable command.
		// Appended AFTER the freshness clause so the last word is the one
		// about measurement. Additive by contract: best-effort, never blocks a
		// claim, and an unreadable origin is STATED, never silently replaced
		// by master (Rule 15).
		cutClause, cutRecord := tools.TaskGroundCutPointClause(ground.Dir, ground.Branch)
		if cutRecord != "" {
			fmt.Fprint(execRunFacts, cutRecord)
		}
		// The append decision and the appended bytes come from ONE call
		// (tools.BriefingWithClause): a caller re-deriving "did it stamp?"
		// from the text can print a marker about a briefing the child
		// received unchanged.
		var cutAppended bool
		briefing, cutAppended = tools.BriefingWithClause(briefing, cutClause)
		if cutAppended {
			fmt.Fprintf(execRunFacts, taskGroundCutPointLineFmt, target, ground.Dir, tools.GroundBranchLabel(ground.Branch))
		}
		// THE MASTER-READING CLAUSE (epic 01a13af7, measured on row 01a13524's own
		// close-out, 2026-10-14). The sibling above re-bases a criterion that
		// quantifies over the integration branch; this one answers the question a
		// step EARLIER — which ref the criterion's WORD "master" denotes at all.
		// Measured: one tree carried three different `devdoc/DOC-INDEX.tsv` blobs
		// each answering to "master" (refs/heads/master 578ada5ab/2502 lines,
		// the peer mirror 4bc6b73d6/2533 lines, the ground's cut commit
		// 735712e52/2540 lines), with the local ref 183 commits behind — so a
		// criterion «use master's version» was green or red depending on the ref
		// the REVIEWER held, while the seam itself rendered the integration
		// branch as a bare NAME and never as the ref+sha that name resolves to.
		// The payload's prose is the submitter's and stays untouched; what is
		// added is the naming. ADDITIVE by the same contract as everything above
		// it: best-effort, never blocks a claim, never errors, and an unreadable
		// pair is STATED and explicitly denies the fallback (Rule 15).
		masterClause, masterRecord := tools.TaskBriefingMasterReadingClause(ground.Dir)
		if masterRecord != "" {
			fmt.Fprint(execRunFacts, masterRecord)
		}
		// The append decision and the appended bytes come from ONE call
		// (tools.BriefingWithClause): a caller re-deriving "did it stamp?"
		// from the text can print a marker about a briefing the child
		// received unchanged.
		var masterAppended bool
		briefing, masterAppended = tools.BriefingWithClause(briefing, masterClause)
		if masterAppended {
			fmt.Fprintf(execRunFacts, taskBriefingMasterReadingLineFmt, target, ground.Dir,
				tools.IntegrationBranchName, tools.MasterReadingRefs(), tools.IntegrationBranchName)
		}
		// THE CENSUS REVISION ANCHOR (epic 01a1abcd, 2026-11-05). Every clause
		// above quotes MEASUREMENTS it took by reading THIS GROUND'S TREE — "this
		// ground counts 3114 top-level test function(s) there", "7 of these
		// path(s) do not resolve inside this ground" — and a tree is not a
		// constant: the child commits into it, a sibling landing moves it, a
		// re-dispatch reads a different one. A count written into a child's
		// briefing with no revision beside it is an observation dressed as a
		// standing fact, which is the defect the base / gate-assertion siblings
		// were built to correct one level up. This anchor is the date: one
		// `git rev-parse --short HEAD` per DISPATCH (not per clause), appended
		// ONLY when at least one census was actually stamped (an anchor over no
		// measurement is noise a reader learns to skip), and ADDITIVE by the
		// same contract as everything above it — best-effort, never blocks a
		// claim, and an UNREAD revision is STATED with the command's own exit
		// and stderr rather than omitted (Rule 15).
		if pathSetClause != "" || runAltAppended || proseTestAppended {
			revClause, revRecord := tools.TaskBriefingCensusRevisionClause(ground.Dir)
			fmt.Fprint(execRunFacts, revRecord)
			var revAppended bool
			briefing, revAppended = tools.BriefingWithClause(briefing, revClause)
			if revAppended {
				fmt.Fprintf(execRunFacts, tools.TaskBriefingCensusRevisionLineFmt, target)
			}
		}
		if refusal, bad := ground.ChildInMainCheckout(childDir); bad {
			fmt.Fprintf(execRunFacts, "%s\n", refusal)
			fmt.Fprintln(os.Stderr, refusal)
			if _, rerr := tools.TaskUpdateReason(target, tools.TaskStatusFailed, refusal, ""); rerr != nil {
				fmt.Fprintf(os.Stderr, "%s: recording the refusal on task %s failed: %v\n", taskProgName, target, rerr)
			}
			writebackFailure(target, tools.TaskStatusFailed, refusal, opts.reportTo, opts.repo)
			return fmt.Errorf("%s", refusal)
		}
	}
	// CLAIM-TIME GROUND STAMP (epic branch↔task linkage, item [2/5],
	// 2026-09-16): record the branch + linked worktree this run is about to
	// execute in, NOW — the row is in_progress from the start transition
	// above, and until this stamp the only writers of the pair were the
	// executor's own CREATION-time detection seam (a bash round that created
	// or moved into a worktree — task_deliverable_worktree.go) and the
	// COMPLETION-only backfill, so an in-flight row whose bash never created
	// a worktree named no ground for the whole run.
	//
	// The directory stamped is childDir — the directory the executor child
	// ACTUALLY starts in, after the foreign-worktree escape above. Timed
	// deliberately AFTER that escape: stamping execWorkdir would bind this row
	// to a foreign task's linked worktree (the same hijack the creation seam's
	// never-rebind rule exists to prevent; measured live on row 01a09250).
	//
	// The row's OWN deliverable.repo is the ground reference, never the
	// caller's cwd: the wrapper already chdir'd into the task's session
	// directory (taskWorkdirEnter above), so an unanchored resolution would
	// compare against the profile's enclosing tree. The stamp is ADDITIVE and
	// shares the ONE blob writer with every other deliverable path
	// (taskBackfillDeliverableWorktree → taskDeliverableParse/Rebuild, CAS on
	// the raw blob): a row that already names a worktree keeps it, and the
	// completion-time backfill still records/confirms the branch from the
	// CHILD's own cwd — no second source of truth (Rules 13/14).
	// THE CLONE CHANNEL GROUND IS ALREADY WRITTEN, AND IT IS THE TRUE ONE:
	// taskRunCloneChannelFor recorded workspace_root/<task-id> through
	// TaskRunGroundForClone. Re-deriving a linked-worktree name here would
	// overwrite that with a `gopi-<stem>` path this run never creates — a row
	// naming a directory that does not exist (measured live in the clone lab,
	// 2026-10-18). Skipped, not re-derived (Rule 15: no false evidence).
	if cloneRun == nil {
		taskClaimGroundStamp(target, childDir)
	}
	childEnv, leaked := taskChildExecEnv()
	// THE CHECKOUT-HOME STRIP IS ANNOUNCED HERE, not folded into the daemon-
	// context line: it is a different fact (a PROFILE the child must not
	// resolve, not an execution handle it must not carry) and the operator has
	// to be able to read which one happened (Rule 15). Renders nothing when
	// GOPI_HOME was kept — inheritance is the normal case.
	fmt.Fprint(execRunFacts, taskCheckoutHomeStripLine(leaked, os.Environ()))
	// THE RECORD FOLLOWS THE DECISION (epic worker-creates-child-ground): the
	// ground resolution above is the LAST word on the child's directory, so the
	// record names childDir and the source says which step decided it. Before
	// the ground existed this line named execWorkdir — true then, a lie now,
	// because the resolution had not happened yet when it was written.
	fmt.Fprintf(execRunFacts, taskCwdResolvedFmt, childDir, target, taskCwdSourceFor(ground, cwdSource))
	fmt.Fprint(execRunFacts, taskEnvStripLine(leaked, target))
	if childDir != "" {
		cmd.Dir = childDir
	}
	// THE CHILD'S BRIEFING IS BOUND HERE — ONCE, AFTER THE LAST MUTATION, AND
	// ON EVERY PATH (epic cmd-agent-e2e-seam-stability, measured 2026-09-25).
	// This binding used to live inside the `ground.Dir != ""` block above, as a
	// REBINDING: correct for a row WITH a ground, and silently absent for a row
	// without one. Nothing else ever set cmd.Stdin, so `gopi task run` /
	// `gopi worker` on a REPO-LESS row spawned a child with a nil reader — and
	// a child with no prompt prints the top-level help and exits 1. The wrapper
	// then recorded the executor's failure honestly ("abnormal child exit"), so
	// the row read as a model/run failure while the real cause was an unbound
	// reader: exactly the shape the repo-less resume E2E hit
	// (TestTaskRunExplicitResumeClaimedStillWorks: the recovery run's child
	// printed 120 lines of usage, exit status 1).
	//
	// Ordering is the requirement, not style: `briefing` keeps being extended
	// (the node-identity block at the top, the ground-announcement clause in
	// the resolution), and a reader holds the string it was HANDED — so the
	// binding must sit after the resolution. Binding here covers BOTH shapes
	// with one line, which is what removes the "did this path bind it?" class
	// rather than the one instance of it (Rules 13/14).
	cmd.Stdin = strings.NewReader(sessionevent.BriefingOnStdin(briefing))
	// task-single-row-exec seam: export the wrapper row's task_id to the
	// child BEFORE spawn (cmd.Env, not os.Setenv — a post-spawn setenv is too
	// late: the child reads it at auto-registration, milliseconds into its
	// run). The child's auto-register hook treats the env as its WRAPPER-CHILD
	// GATE: a task-run child already HAS a human ledger row (this one — the
	// single execution row), so it must NOT mint an auto mirror row. The env
	// is also the child's SIGPIPE-ignore latch (main.go) and the executor's
	// deliverable-worktree backfill key. Rule 2: one writer (here), readers
	// in main.go / taskregister.go / task_deliverable_worktree.go. The
	// pre-existing GOPI_TASK_CHILD_SESSION (post-exit writeback seam) is
	// unrelated and left untouched.
	cmd.Env = append(childEnv, tools.TaskWorkdirEnv+"="+execWorkdir)
	cmd.Env = append(cmd.Env, tools.TaskWrapperEnv+"="+target)
	// THE RESUME WITNESS, DECLARED BY THE DOOR (epic: land the resume witness on
	// the RE-DISPATCH door). This wrapper is the ONE process that can answer
	// "does this row already have a run?" without measuring anything — and the
	// process that writes session_start is its CHILD, so the answer travels in
	// the child's environment and nowhere else (tools.TaskRunResumeFrom is the
	// single reader, Rule 21).
	//
	// WHY THE DOOR AND NOT THE WRITER: the record-side witness must not be
	// reverse-inferred from the raw log. The raw side decides "this is a
	// continuation" by measuring its own file size and then INVENTS the id it
	// records, and `gopi session gc --trim-raw-log` deletes exactly that
	// header — so a writer that inferred from the raw log would lose the witness
	// the moment the log was trimmed (measured: three headers, two witnessed, no
	// resume header left in the raw log at all).
	//
	// STAMPED ONLY WHEN THERE IS SOMETHING TO SAY: a row on its first run
	// announces nothing, so an absent variable keeps its single meaning and the
	// child writes no witness rather than a guessed one (Rule 15a).
	// REBASE NOTE (delivery round 2/3): two increments, unioned. master's
	// 5ac76106 added the SECOND frame of the same fact (this wrapper process
	// must carry the declaration it hands the child, so its own close-out
	// notify can read it); this branch made the declaration a VALUE
	// (tools.TaskRunResumeDeclare) instead of a spelling each seam builds by
	// hand, because a third seam — the clone channel's delivery round — came
	// to exist by having nothing to call. Neither replaces the other: the
	// value is read once into `decl` and master's self-set block keeps its
	// exact semantics, only taking the value from decl instead of a local.
	if decl, ok := tools.TaskRunResumeDeclare(target); ok {
		cmd.Env = append(cmd.Env, decl.Env())
		// THE SAME DECLARATION, ON THIS PROCESS TOO (epic: notify payload
		// carries resumed_from). The executor child writes its own
		// session_start, but the TERMINAL writeback that fires the notify
		// event frequently runs HERE — in the wrapper, after cmd.Wait — and
		// this process does not carry the variable it just handed the child.
		// Setting it on ourselves is what makes the fact readable by the
		// notify layer of BOTH processes through the ONE reader
		// (tools.taskRunResumeWitness, Rule 21), instead of teaching the
		// payload a second, row-derived way of guessing it — a guess that is
		// a FALSE POSITIVE by construction, because by close-out the row's
		// run directory already holds this very run's record.
		//
		// Set only when we are the first to say it: a value inherited from a
		// parent door that already declared a resume is the older, still true
		// fact and is left alone rather than rewritten.
		if os.Getenv(tools.TaskRunResumeFromEnv) == "" {
			_ = os.Setenv(tools.TaskRunResumeFromEnv, decl.ResumedFrom) //stylegate:ignore this process's own copy of the door's declaration, so the wrapper's close-out notify reads the same fact the child was given
		}
		fmt.Fprintf(execRunFacts, taskRedispatchWitnessLineFmt, target, decl.ResumedFrom)
	}
	// THE GROUND ANNOUNCEMENT, ENV HALF (epic ground-announcement): the same
	// fact the briefing clause carries, in the child's own environment, so the
	// executor's code and any gopi it spawns can read WHICH ground is theirs
	// (tools.TaskGroundAnnounced is the reader — Rule 21). Stamped only when a
	// ground actually resolved: a repo-less row announces nothing rather than an
	// empty claim, and the pair is written HERE — one writer, the ONE spawn seam
	// every executor face funnels through (Rule 2). The briefing clause is bound
	// at the ground resolution above, before this; together they are the two
	// faces of one decision.
	if ground.Dir != "" {
		// THE BRANCH HALF IS CROSS-CHECKED BEFORE IT IS PUBLISHED (epic
		// ground-branch): `task/<dashless stem>` has two producers — the
		// DERIVATION (tools.TaskGroundBranchName, what the ledger records) and
		// this ANNOUNCEMENT — and a reader given the wrong one does not fail,
		// it SKIPS (the publish-target guard falls back to a measured HEAD),
		// which reads exactly like a pass. So the comparison happens here, at
		// the ONE spawn seam, and a divergence is a line in the run's facts
		// rather than a silent skip later.
		publishedBranch, branchDivergence := tools.TaskGroundBranchPublish(target, ground.Branch)
		if branchDivergence != "" {
			fmt.Fprintf(execRunFacts, "%s\n", branchDivergence)
			fmt.Fprintln(os.Stderr, branchDivergence)
		}
		cmd.Env = append(cmd.Env, tools.TaskGroundDirEnv+"="+ground.Dir)
		if publishedBranch == "" {
			// TaskGroundBranchPublish answers "" only when there is nothing to
			// publish (no derivable ledger address AND no recorded branch).
			// Publishing the empty string would be a CLAIM that the ground has
			// no branch, which no measurement established — so the variable is
			// left unset and the fact is stated (Rule 15: an absent
			// announcement is a truthful "nothing was announced", an empty one
			// is a false one).
			msg := fmt.Sprintf(taskGroundBranchUnpublishedFmt, ground.Dir, target, tools.TaskGroundBranchEnv)
			fmt.Fprintf(execRunFacts, "%s\n", msg)
			fmt.Fprintln(os.Stderr, msg)
		} else {
			cmd.Env = append(cmd.Env, tools.TaskGroundBranchEnv+"="+publishedBranch)
		}
	}
	// Run-ledger LINEAGE seam (worker-writeback epic, 2026-09-12; comment
	// rewritten by task 01a16f7c when the CHILD-side close-out was deleted).
	// The vars published here let a later scan recognise the processes this run
	// produced: the child inherits them into everything it spawns, and the
	// kernel keeps them after reparenting, so a /proc/<pid>/environ read answers
	// "was this process spawned by this run's tree?" — the question the
	// worker-side run close-out reap asks before it kills a leftover, and the
	// one the finish-cleanup holder-orphan classifier asks before it decides a
	// worktree-pinning process belongs to a run rather than to a third party
	// (see internal/tools/procsignal.go for the incarnation-guarded signal).
	//
	// They are NOT a liveness verdict: nothing in this binary asks "is the
	// wrapper pid still alive?" to decide who closes the row. That question —
	// the deleted child-side close-out — had its answer before it was asked,
	// because the wrapper is the process that WAITs for the child. Rule 2: one
	// writer (here), and the readers are the lineage scans above.
	if envTaskID, envOutcome := tools.RunCloseoutPair(target, tools.TaskStatusCompleted); envTaskID != "" {
		cmd.Env = append(cmd.Env,
			tools.TaskRunIDEnv+"="+envTaskID,
			tools.TaskRunOutcomeEnv+"="+envOutcome,
			tools.TaskRunWrapperPIDEnv+"="+strconv.Itoa(os.Getpid()),
		)
	}
	// task-report seam (epic task-report): export the report's ABSOLUTE path
	// BEFORE spawn, alongside the wrapper identity. The path is a HARNESS
	// fact — the session dir is relocatable (GOPI_HOME), migratable from the
	// legacy task-sessions root and prunable — so the executor is HANDED it
	// rather than deriving it (a model that guesses writes where nobody
	// reads). Rule 2: one writer (here), readers are the executor's prompt
	// contract and the seal step in writebackOutcome. Best-effort: a path
	// resolution failure logs and the run proceeds with no report path
	// exported (the seal still writes the skeleton at the resolved location).
	//
	// THE DESTINATION IS A PRECONDITION, NOT A STRING (epic
	// report-export-destination, measured 2026-10-28): the child's contract is
	// `cat > "$GOPI_TASK_REPORT_PATH"`, so an exported path whose PARENT does
	// not exist is a destination the run can never use — it gets ENOENT, its
	// only report goes nowhere, and nothing says so. tools.TaskReportDestination
	// resolves the SAME path (Rule 14: it delegates to TaskReportPath) and makes
	// the directory exist and accept a write BEFORE the pair enters the child's
	// environment; a failure lands in the else arm below, so an unusable
	// destination is REFUSED out loud instead of handed over (Rule 15).
	if reportPath, rpErr := tools.TaskReportDestination(target); rpErr == nil {
		// The OWNER marker travels WITH the path (epic
		// nested-report-path-clobber, 2026-09-15): an inherited path was
		// indistinguishable from "yours", so a nested gopi read the PARENT's
		// report path as its own destination and overwrote the parent's
		// deliverable (session 01a0a4ee). With the owner named, a descendant
		// shell loses the pair (tools' env sanitizer) and the child gopi it
		// spawns can neither read nor write it (journalreport.go's ownership
		// guard, StripInheritedReportContract at ITS own spawn). Both names
		// are written here and only here — Rule 2's single writer.
		cmd.Env = append(cmd.Env,
			taskreport.PathEnv+"="+reportPath,
			taskreport.OwnEnv+"="+target,
		)
	} else {
		fmt.Fprintf(os.Stderr, taskProgName+": report path for %s unresolved (continuing without %s): %v\n",
			target, taskreport.PathEnv, rpErr)
	}
	// SIGNAL PROVENANCE DESTINATION (epic signalrec-session-attribution,
	// 2026-09-14): hand the child the session dir its provenance records belong
	// in. The child MINTS its own run uuid at startup and the ledger row is
	// keyed by that uuid, so the child cannot derive the directory its own
	// artifacts land in — this launcher knows it (tools.TaskSessionDir(target)
	// IS the directory waitChildSessionJSONL finds the child's JSONL in), so it
	// is HANDED over in the child's environment before spawn, the same Rule-2
	// shape as the report path and the wrapper identity above. The ledger id is
	// handed with it so the record's task_run field stays populated. Best-
	// effort with a REPORT, never a silent miss: the child's own
	// GOPI_TASK_RUN_ID resolution is the fallback, and a child that can resolve
	// neither REFUSES the record loudly (signalrec's contract) — it never
	// writes it to the retired public logs dir.
	if sessDir, sdErr := tools.TaskSessionDir(target); sdErr == nil {
		cmd.Env = append(cmd.Env,
			signalrec.SessionDirEnv+"="+sessDir,
			signalrec.TaskIDEnv+"="+target,
		)
	} else {
		// A REFUSED target is a red flag, not a best-effort gap: the row id the
		// launcher was asked to run is not a spelling this tree turns into a
		// path element (sessionid's shape rule — a uuid's bytes with the wrong
		// dashes or one digit too many). Nothing is exported, so the child
		// resolves its own destination and the run's artifacts land SOMEWHERE;
		// the harness, meanwhile, resolves the report contract from the same
		// target (taskreport.PathEnv, above) and would read a different
		// directory. Saying so on stderr is what keeps the two halves from
		// disagreeing silently (Rule 15) — the launcher does not invent a
		// corrected id, it names the one it was given.
		fmt.Fprintf(os.Stderr, taskProgName+": signal provenance session dir for %s unresolved (the child falls back to its own ledger id): %v\n", target, sdErr)
	}
	// Process-GROUP record for the run's stop paths (2026-09-25; the environment
	// that originally wanted it — the run close-out reap — was DELETED 2026-11-05
	// in 386731b5, so this comment names only what still reads it). The record
	// holds the group this run's child LEADS together with that leader's
	// incarnation ticks, so a stop path can tell the run's own descendants from a
	// stranger's (a process-group id alone is a recyclable pid number —
	// internal/tools/procsignal.go's ownership rules). It must run AFTER Start,
	// never before: the values are read from the CHILD, so the pid has to exist.
	// The reap scope's exclusion set (`accountRunChild`) is gone with the reap —
	// this is the writer, and signalRunChild is the reader.
	if err := taskRunSpawnChild(cmd, execRunFacts, target, opts.as, started); err != nil {
		return taskRunSpawnFailure(target, err, opts.reportTo, opts.repo)
	}
	// CHILD REGISTRY (signal-handling epic, sandbox rig /tmp/self-release-*):
	// a stop signal that arrives while the run's child is still executing has
	// to reach the CHILD, not just this wrapper — otherwise the wrapper's
	// close-out (terminal writeback + the producer's own-worktree release,
	// below) waits on a child that never learns the run is over, and a
	// supervisor's SIGKILL takes down the whole close-out with it. The
	// registry is the merger's (one list of in-flight children, Rule 14):
	// the worker's / wrapper's signal arrival asks it for the live child and
	// forwards the signal, while the graceful path itself keeps WAITING —
	// the child exits on its own terms and this wrapper's defer chain (the
	// release) still runs. Deregistration is deferred so no exit path —
	// panic, early return, kill — can leak the entry.
	deregisterChild := registerPassChild(passChild{
		Kind:  passChildKindRun,
		Label: "task-run child " + target,
		PID:   cmd.Process.Pid,
		Group: cmd.Process.Pid,
	})
	defer deregisterChild()
	// bug054 (session_id stamp): stamp the child session into the task row
	// as soon as it exists — NOT at writeback, which is the whole-run later
	// that left in_progress rows with session_id empty and model (unknown),
	// exactly when tailing the child's logs is most needed. The child
	// writes+syncs its session_start immediately at startup (NewSessionLogger),
	// so a short poll for the newest session JSONL in the run window finds
	// it right after spawn. Best-effort: a miss (child died before the
	// session flushed / no sessions dir) just leaves the writeback's
	// deferred backfill as the fallback — the stamp NEVER blocks the run.
	childSID := ""
	if sid := waitChildSessionJSONL(started, target); sid != "" {
		childSID = sid
		if err := tools.TaskBackfillSession(target, sid); err != nil {
			taskNotifyF("session stamp for %s: %v", target, err)
		}
	}
	runErr := cmd.Wait()
	// THE STAGE BOUNDARY AT WAIT'S RETURN (see taskRunStageReapedFmt): written
	// on BOTH outcomes, and stamped, so the child's real lifetime is a recorded
	// fact — the line last_toolcall_at could never provide.
	taskRunRecordReaped(execRunFacts, target, started, runErr)
	elapsed := time.Since(started).Round(time.Second)
	// task-submit-model epic ②: export the child's session id so the
	// writeback's model backfill reads it (taskExecutorSessionModel). Best-
	// effort: a missing session file just skips the backfill.
	if childSID != "" {
		_ = os.Setenv(tools.TaskChildSessionEnv, childSID) //stylegate:ignore child-process env handoff; the child treats an empty value as 'unset'
	} else if sid := newestSessionJSONLAfter(started, target); sid != "" {
		_ = os.Setenv(tools.TaskChildSessionEnv, sid) //stylegate:ignore child-process env handoff; the child treats an empty value as 'unset'
	}
	stats := fmt.Sprintf(" — run: elapsed=%s", elapsed)
	if runErr != nil {
		// ABNORMAL vs VERDICT-BEARING (epic wrapped-executor-child 4/5): the
		// ROW'S STATUS IS READ FIRST, before the wrapper's own writeback lands
		// — a child that judged its task closes the row itself, so "already
		// terminal when I looked" is the OPPOSITE fact from "I am about to
		// close it because my child crashed". Reading it afterwards would make
		// every exit look like this wrapper's own report.
		closedByChild, rowStatus, statusErr := tools.TaskRowTerminalAt(target)
		// THE CHILD'S EXIT CODE, ELAPSED TIME AND ITS OWN LAST WORDS, one digest
		// (epic failed-row-reason-facts). Order is load-bearing: the reason
		// column keeps only the digest's HEAD (tools.taskFailedReason), so the
		// stderr excerpt rides directly behind the exit status — a topic
		// appended after the run stats would be cut off by the 300-rune cap and
		// the operator would be back to "exit status 1" with nothing to read.
		// taskChildStderrDigestClause returns "" for a child that printed
		// nothing, and an empty clause contributes no separator at all: the
		// digest never grows a placeholder for a stream that said nothing.
		writebackFailure(target, tools.TaskStatusFailed,
			"gopi run failed: "+runErr.Error()+taskChildStderrDigestClause(taskExecutorStderrTail)+taskAbnormalChildMarker+stats, opts.reportTo, opts.repo)
		reconcileChildMirror(target, tools.TaskStatusFailed, "child-exit: "+runErr.Error())
		// Same release tail as the hermetic failure above: a child that exited
		// non-zero must not leave this wrapper's cwd — or the run's worktree —
		// behind (worktree-residue epic). It runs BEFORE the retry because the
		// retry hands the row's ground to a NEW process: this one must already
		// be out of it (the reclaim would otherwise refuse its own caller's
		// cwd, and the resumed run would be spawned from inside a directory it
		// is about to free).
		taskRunReclaimOwnDirectories(target)
		// The bounded retry. It never runs the child again IN THIS PROCESS
		// (the epic's "the wrapper never babysits a child"): it starts the
		// existing recovery entry point as a DETACHED child and returns, with
		// the pre-writeback judgement above handed in as its input.
		taskRunAbnormalChildRetry(target, closedByChild, rowStatus, statusErr)
		return fmt.Errorf("gopi task run: task %s FAILED (child: %w)", target, runErr)
	}
	// THE CLONE CHANNEL'S DELIVERY LEG RUNS BEFORE THE CLAIM — the order IS the
	// fix (same defect class, same answer as 99ad8a2 on the bare-prompt face).
	//
	// THE DEFECT THIS CLOSES (measured on the live ledger, row 01a16f9d, and in
	// that run's own output.log): this block used to sit AFTER the `completed`
	// writeback and after the landed check, i.e. the push sat behind a gate that
	// only the push could open. tools.deliveryProofRefusal measures the tip the
	// run produced against refs/remotes/origin/<branch> — the ref the push
	// itself moves — so the claim was judged against a destination the delivery
	// had not reached yet, was REFUSED, taskRunCloseoutLanded saw a non-terminal
	// row and returned, and the delivery leg below it never ran at all:
	//
	//	$ grep -c 'delivery attempt' <run>/output.log
	//	0                        # the legs that push never executed
	//	$ gopi task show <row> | grep '^result:'
	//	result: … the tip … is in NEITHER the ancestry NOR the patches of
	//	        refs/remotes/origin/master … so the work exists only in the
	//	        run's own ground. Deliver it … and write back again, or leave
	//	        the row OPEN
	//
	// A gate that names its own remedy ("deliver it … and write back again")
	// while the code puts delivering AFTER it can never be satisfied — the row
	// is left open, its commits exist only in a ground nobody revisits, and the
	// ledger reads like a run still in flight. Delivering FIRST and claiming
	// SECOND is the only order in which each duty measures a true state: the
	// push moves refs/remotes/origin/<branch>, so the proof that follows sees
	// the tip AT its destination and the row lands completed; a push that was
	// BLOCKED leaves that ref where it was, so no `completed` claim is written
	// at all — the row keeps the leg's own sentence as its reason (Rule 15: the
	// run failed to deliver and does not say "completed").
	//
	// It still reuses the ONE delivery implementation (devworkspace.AttemptDelivery
	// through cloneRun.finish): at most devworkspace.MaxDeliveryRounds attempts
	// of clean → pull --rebase → build → push, each blocked attempt handed back
	// to ONE agent round. Nothing about the attempts, their brake or their
	// declarations changes here — only WHERE in the close-out they run.
	// cloneDeliveryReason is the clone channel's own sentence for a run that had
	// NOTHING to deliver (row 01a18244). It is distinct from the delivered case:
	// `nil` here means "the ordinary path", and a non-nil value REPLACES the
	// generic summary on the ONE writeback below, so the ledger row's reason is
	// the exit's composed string verbatim (Rule 14 — one composition, one place).
	var cloneDeliveryReason string
	if cloneRun != nil {
		cres, cerr := cloneRun.finish(os.Stdout)
		// THE THIRD OUTCOME (row 01a18244): the ground held nothing past the
		// integration point, so NOTHING WAS REFUSED and nothing was lost. The run
		// COMPLETES with the exit's own sentence as its reason — the ledger must
		// not read `failed` for a run that was asked to produce no change and
		// produced none (rows 01a17e38/01a17e7c/01a17e04 closed that way). The
		// reason is cerr's text VERBATIM (Rule 14): the same composed string the
		// exit returned, and the ONE summary this close-out writes back.
		nothingToDeliver := cerr != nil && errors.Is(cerr, errNothingToDeliver)
		// THE FOURTH OUTCOME (row 01a18392): the ledger row was RETIRED while the
		// delivery loop was running, so the loop stopped at its own round
		// boundary (no further attempt, no agent round) and returned this
		// sentinel. The row is already terminal — an operator's `cancelled` —
		// and writing `failed` over it would rewrite that decision (Rule 15 in
		// its destructive direction: the write is worse than the silence). So the
		// reason lands as a RECORD, not as a status change: print, keep the row,
		// and let the close-out report the run as stopped.
		rowRetired := cerr != nil && errors.Is(cerr, errRowRetired)
		if rowRetired {
			reason := cerr.Error()
			fmt.Fprintf(execRunFacts, "%s: %s\n", taskProgName, reason)
			fmt.Printf("task %s: %s\n", target, reason)
			return fmt.Errorf("%s", reason)
		}
		if nothingToDeliver {
			reason := cerr.Error()
			fmt.Fprintf(execRunFacts, "%s: %s\n", taskProgName, reason)
			fmt.Printf("task %s: %s\n", target, reason)
			cloneDeliveryReason = reason
			// THE CONCLUSION, handed to the completion-witness arm: this run
			// completed on a MEASUREMENT (its ground held nothing past the
			// integration point), and the ledger must be able to tell that
			// apart from a `completed` claim with nothing behind it
			// (epic 01a182bf). The latch is read by the ONE write-back seam.
			taskRunConclusion = reason
			cerr = nil
		}
		// THE FIFTH OUTCOME (row 01a182df): the same `count == 0` and the
		// OPPOSITE fact — this run's OWN push already moved the integration base
		// onto its tip, so the number is 0 because the delivery SUCCEEDED. It
		// rides the SAME two latches as the arm above (one write-back seam, Rule
		// 14) and is kept a separate sentinel only so the two sentences stay
		// different: `failed` here would report work that is on the integration
		// branch as work that never arrived — the exact sentence this row was
		// filed against.
		if cerr != nil && errors.Is(cerr, errAlreadyDelivered) {
			reason := cerr.Error()
			fmt.Fprintf(execRunFacts, "%s: %s\n", taskProgName, reason)
			fmt.Printf("task %s: %s\n", target, reason)
			cloneDeliveryReason = reason
			taskRunConclusion = reason
			cerr = nil
		}
		// THE SIXTH OUTCOME (epic: inherited delivery): the THIRD meaning of the
		// same `count == 0` — HEAD is already an ancestor of the integration
		// branch, so the content this ground was handed is on the base and this
		// run pushed nothing. It rides the SAME two latches as the arms above
		// (one write-back seam, Rule 14) and keeps its own sentinel only so the
		// three sentences stay different: `failed` here would report content that
		// IS on the integration branch as work that never arrived.
		if cerr != nil && errors.Is(cerr, errInheritedDelivery) {
			reason := cerr.Error()
			fmt.Fprintf(execRunFacts, "%s: %s\n", taskProgName, reason)
			fmt.Printf("task %s: %s\n", target, reason)
			cloneDeliveryReason = reason
			taskRunConclusion = reason
			cerr = nil
		}
		if cerr != nil {
			// finish's errors already name the stage, the round count and the
			// row's id, so the refusal is the error VERBATIM: a second prefix
			// would read "clone channel: clone channel: …" (measured in the
			// clone lab). The reason reaches the LEDGER here — the one write —
			// and the loop has already spent its closing round on the report.
			refusal := cerr.Error()
			// The refusal is the SECOND half of the same account: this line says
			// WHERE the delivery stopped, the close-out line above says the row
			// stayed open. Both belong in the run's own record for the same
			// measured reason (row 01a16f7c: its log carried neither).
			fmt.Fprintf(execRunFacts, "%s: %s\n", taskProgName, refusal)
			if _, rerr := tools.TaskUpdateReason(target, tools.TaskStatusFailed, refusal, ""); rerr != nil {
				fmt.Fprintf(execRunFacts, "%s: recording the failure on task %s failed: %v\n", taskProgName, target, rerr)
			}
			return fmt.Errorf("%s", refusal)
		}
		reclaimNote := ""
		if cres.Reclaimed {
			reclaimNote = ", workspace reclaimed"
		}
		if cloneDeliveryReason == "" {
			fmt.Printf("task %s: clone channel delivered on delivery attempt %d/%d%s\n",
				target, cloneRun.rounds, devworkspace.MaxDeliveryRounds, reclaimNote)
		}
	}
	// THE ONE CLOSE-OUT SUMMARY. A nothing-to-deliver run carries the exit's own
	// sentence (composed once, Rule 14) so the ledger row and the terminal line
	// cannot tell an operator two different stories; everything else keeps the
	// generic completed digest.
	summary := "gopi run completed" + stats
	closeStatus := tools.TaskStatusCompleted
	if cloneDeliveryReason != "" {
		// WHICH CLAIM IS TRUE, once the measurement is in (row 01a186b9, measured
		// 2026-10-29): the channel's empty-ground exit is a legitimate completion
		// for a run that was ASKED to produce nothing — but it is the WRONG claim
		// when the run's own report opens `Verdict: BLOCKED` and states it did not
		// do the work. That row closed `completed` with the report saying the
		// opposite, and nothing read the report. The decision lives in the tools
		// layer (NothingToDeliverOutcome), so the vocabulary of "the author
		// declared this unfinished" has ONE definition.
		closeStatus, summary = tools.NothingToDeliverOutcome(target, cloneDeliveryReason)
	}
	wbErr := writebackOutcome(target, closeStatus, summary, opts.reportTo, opts.repo)
	// serve-waitpid close-out (epic t-1788384486), legacy-mirror seam: the
	// child exited 0 but its OWN writeback may not have run (a child that
	// completed its work yet died before/without the END hook — the old auto
	// mirror-row world). Reconcile any in_progress LEGACY auto mirror row to
	// completed so a live child never leaves a zombie auto-row behind. Under
	// task-single-row-exec the child mints NO mirror row, so this is a no-op
	// for new runs (acceptance d — no double-write: the guard skips terminal
	// mirrors); it stays as the historical-cleanup + defensive seam.
	reconcileChildMirror(target, tools.TaskStatusCompleted, "child-exit: 0 (parent-side close-out)")
	// THE DELIVERY-TIME WORKTREE RECYCLE IS RETIRED (2026-10-30, task
	// 01a18799, slice 3/4). This arm used to run the close-out chain over the
	// WRAPPER row at delivery time — reclaim → align → gate → push → worktree
	// remove → branch -d — because the child's own close-out ran on an AUTO row
	// with no deliverable. Nobody hands a worktree or a branch over any more: a
	// clone-ground run publishes itself at its own END hook (Rule 6c), and the
	// grounds that remain are collected by the sweep that owns them
	// (`gopi task sweep-worktrees`). The producer-side release below stays — it
	// is this process's own obligation and depends on no chain.
	// PRODUCER-SIDE RELEASE (row 01a095b2): the WRAPPER row is the one holding
	// the claim — and therefore the worktree — for this run's duration, and the
	// sweep's dead-holder arm cannot see it while this process is alive. The
	// pass frees the directories of the rows THIS process finished, through the
	// same guarded removal the sweep uses (clean + no live process inside +
	// still on the row's branch), and NAMES every refusal.
	//
	// THE PASS NEEDS A USABLE CWD, so the release below runs after it — and the
	// release is what the OTHER producer paths (worker round, merger pass,
	// executor child, resume) reach through the same ONE implementation
	// (internal/worktreerelease, PROJECT.md Rules 13/14; this file's own private
	// chdir was the duplication that left them uncovered).
	taskRunReclaimOwnDirectories(target)
	// THE LAST CHECK ON THE SUCCESS ARM (epic worktree-dirty-residue, 2026-09-27):
	// everything above assumes the writeback above LANDED. When Rule 30's
	// close-out gate refused it (the run's own worktree still holds uncommitted
	// tracked work, or untracked residue with the enforcement switch armed), the
	// row is NOT terminal — and until this check existed the wrapper printed
	// `task <id> completed` and returned nil anyway: exit code, console and
	// ledger disagreed, the worktree was kept (correctly) and nothing ever came
	// back for it. The read is the SAME one the failure arm already makes
	// (tools.TaskRowTerminalAt), taken AFTER the write instead of before it
	// because the two arms ask opposite questions: the failure arm asks "had the
	// child already judged this row?" (a read taken after its own writeback
	// would call every exit its own report), this arm asks "did MY close-out
	// land?" — which is only answerable once the write has been attempted.
	// Nothing is committed, cleaned or deleted here (Rule 30's line); the
	// refusal text is the gate's own, and the escalation REPLACES the false
	// success line and gives the process a matching exit code.
	if landed, rowStatus, statusErr := taskRunCloseoutLanded(target, wbErr); !landed {
		return taskRunCloseoutEscalate(target, wbErr, rowStatus, statusErr)
	}
	// THE CLONE CHANNEL'S DELIVERY LEG IS ABOVE, BEFORE THE CLAIM — it is not
	// repeated here. Its line ("clone channel delivered on delivery attempt
	// N/M") has already printed, and a second call site would be a second
	// attempt: cloneRun.finish is not idempotent by itself, and the ONE attempt
	// per run is a property of the CALL SITES, so there is exactly one.
	fmt.Printf("task %s completed (elapsed %s)\n", target, elapsed)
	return nil
}

// taskRunReclaimOwnDirectories is the `gopi task run` wrapper's close-out tail:
// free the linked worktrees of the rows THIS process finished, then hand the
// process's cwd back out of whatever worktree it was standing in.
//
// WHY ONE FUNCTION AND NOT THREE INLINE BLOCKS (worktree-residue epic): the
// obligation is "no wrapper return path leaves the row's ground occupied" —
// and the wrapper has TWO ways out (the child's failure and its success). The
// failure return used to walk past both halves, so a failed run kept its
// worktree registered AND kept this process's cwd inside it: `git worktree
// remove` then refuses (the directory is held), and the survivor is exactly
// the residue the attachment reports. Sharing the tail makes the coverage a
// property of the WRAPPER rather than of each return site (Rules 13/14).
//
// ORDER MATTERS, and it is the order of the two effects: the reclaim needs a
// usable cwd, so it runs first; only then is moving the cwd the release of a
// directory that may already be gone. Both halves are best-effort and
// REPORTED (Rule 15) — neither ever changes the run's outcome or exit code,
// which is why the failure returns can call it sitting between the writeback
// and their error return.
//
// THE THREE-STEP TAIL IS NOW TWO STEPS (2026-11-05, 386731b5). This doc block
// used to head a second function, `taskRunReleaseOwnWorktrees`, which ran
// `taskRunReapOwnChildren` and then this one; BOTH are deleted with the run
// close-out reap and the function below is the whole tail. That is the reason
// for the name change, not a refactor: the reap judged ownership from an
// INHERITED env record and killed a nested run's children (measured on
// 01a1a876: five close-outs, five non-empty batches, 2/5/3/4/3).
//
// IT TOUCHES NO PROCESS. Not one signal is addressed from here, which is the
// property the split exists to make expressible — and it is now a property of
// the WHOLE tail, since the signalling step no longer exists to hold it back.
func taskRunReclaimOwnDirectories(target string) {
	// THE ORDER HERE IS THE PASSES' OWN (see workerRoundOwnWorktrees for the
	// reasoning): the reclaim runs first, and the cwd release runs last, right
	// after the directory it may have removed. That is sound ONLY because the
	// removal's own question — "is this ground mine to release, and is anybody
	// standing in it who is not my own run?" — is answered by the close-out mark
	// (reclaim_own_ground.go) and by the live-holder scan, never by where this
	// process happens to stand: measured on master f4451b9d, the wrapper's cwd at
	// this point is its task's SESSION directory (`gopi task: workdir → …/002/.gopi/
	// sessions/01a11500-0000-7000-8000-000000000011 (started in …/001/wt)`), so moving it earlier would
	// buy nothing while costing the pass the cwd it uses to enrol the repo it was
	// started in.
	// ONE call, because the two ownership-independent halves must run under ONE
	// close-out mark: the ledger-keyed half disarms its own on return, so a
	// caller that ran them as two calls left the second — the one that frees the
	// ground of a row whose deliverable names another directory, i.e. the
	// measured defect — running unmarked (tools.ReclaimOwnWorktreesAtCloseout
	// carries the reasoning).
	for _, line := range tools.ReclaimOwnWorktreesAtCloseoutWithin() {
		taskRunPrintOwnReleaseLine(os.Stdout, target, line)
	}
	worktreerelease.ReleaseReporting(os.Stderr, taskProgName)
}

// taskRunOwnReleaseFmt / taskRunOwnReleaseUnownedFmt are the close-out release
// pass's two report shapes, and the DIFFERENCE BETWEEN THEM IS THE FIX (the
// 2026-10-27 defect): `task <id>:` is a CLAIM that the line describes that row's
// state, and the ownership-independent half also reports directories NO row
// created — a chat's hand-made verification checkout, a leftover from another
// task — which used to be printed under whatever row happened to be closing out
// (measured: `task 01a17dcd-…: own-worktree-release: own registered worktree
// kept: /home/user/workspace/cptest-verify — worktree is DIRTY`, about seven
// files belonging to a DIFFERENT task). The observation stays (Rule 15: a dirty
// directory is a human's decision); the false subject goes — an unowned line is
// printed with no row id at all.
const (
	taskRunOwnReleaseFmt        = "task %s: own-worktree-release: %s\n"
	taskRunOwnReleaseUnownedFmt = "own-worktree-release: %s\n"
)

// taskRunPrintOwnReleaseLine prints ONE line of the close-out release pass with
// the subject it earned. The classification is the pass's own
// (tools.ReclaimSelfOwnIsOwnLine reads the marker the pass wrote): this seam must
// never re-derive ownership — a second derivation is exactly how the two sides
// would drift apart (Rules 13/14).
func taskRunPrintOwnReleaseLine(w io.Writer, target, line string) {
	if tools.ReclaimSelfOwnIsOwnLine(line) {
		fmt.Fprintf(w, taskRunOwnReleaseFmt, target, line)
		return
	}
	fmt.Fprintf(w, taskRunOwnReleaseUnownedFmt, line)
}

// taskRunAbnormalChildRetry is the wrapper's bounded automatic retry of an
// ABNORMALLY-exited executor child (epic wrapped-executor-child, 4/5).
//
// WHAT "ABNORMAL" MEANS, AND WHY THE DISCRIMINATOR IS EXPLICIT. One non-zero
// exit code carries two different facts, and the row's status AT THE EXIT
// INSTANT tells them apart — which is why this runs BEFORE the wrapper's own
// writeback:
//
//   - THE CHILD CLOSED ITS OWN ROW. The child that JUDGED its task fails the
//     row itself on its way out (its own close-out). A non-zero exit on an
//     already-terminal row is therefore a VERDICT: the task really failed, its
//     session is complete, its report is written. Restarting that is not
//     recovery, it is repeating a judgement — so it is NOT retried (a real
//     task failure is not a crash).
//   - THE ROW IS STILL OPEN. The child died ABNORMALLY — killed mid-run, or it
//     exited before its close-out could run — so the row would read failed
//     only because THIS wrapper is about to write it. Nothing has judged the
//     work, and the work in flight (a half-written fix, a worktree full of
//     commits, a session checkpoint holding the whole context) would be
//     abandoned by default. That is exactly what this arm resumes.
//
// No new marker is needed for this: it is the difference between "the row was
// already closed when I looked" and "I am the one closing it".
//
// BOUNDING. The attempt count comes from the row's APPEND-ONLY ordinal tags
// (tools.TaskRunRetryCounts), so a wrapper killed between two spawns cannot
// outrun its own cap by forgetting: the ledger remembers. At the cap the arm
// STOPS, records the NAMED handover reason on the row, and prints where the
// ladder ended — never an unbounded restart loop (a loop that always re-arms
// itself is a silent failure dressed as progress, Rule 15).
//
// Every refusal is REPORTED with its reason: a retry that cannot proceed must
// not look like a retry that was never needed.
func taskRunAbnormalChildRetry(target string, closedByChild bool, rowStatus string, statusErr error) {
	// (1) the discriminator, read BEFORE the wrapper's writeback by the caller
	// (the row's status at the child's exit INSTANT — see the call site).
	if statusErr == nil && closedByChild {
		fmt.Fprintf(os.Stderr, taskRunRetryRefusedFmt, target,
			"the child closed its own row (status "+rowStatus+") — a judged failure is a result, not a crash")
		return
	}
	if statusErr != nil {
		// The ledger could not answer. Rule 15: an unreadable fact is never
		// promoted into a retry — report it and let the operator look.
		fmt.Fprintf(os.Stderr, taskRunRetryRefusedFmt, target, "the row's status could not be read: "+statusErr.Error())
		return
	}
	// (2) the ladder has room?
	spent, err := tools.TaskRunRetryCounts(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, taskRunRetryRefusedFmt, target, "the attempt counter could not be read: "+err.Error())
		return
	}
	if spent >= tools.TaskRunRetryCap {
		reason := tools.TaskRunRetryCapReason()
		if _, uerr := tools.TaskUpdateReason(target, tools.TaskStatusFailed, reason, ""); uerr != nil {
			// A handover that could not be recorded is NOT a silent skip: the
			// operator would otherwise read the row as an ordinary failure with
			// no cap note (Rule 15b).
			fmt.Fprintf(os.Stderr, taskRunRetryRefusedFmt, target,
				"the cap was reached but the handover reason could not be recorded: "+uerr.Error())
			return
		}
		fmt.Fprintf(os.Stderr, taskRunRetryHandoverFmt, target, reason)
		return
	}
	// (3) a resumable target: the SAME task session, through the existing
	// recovery entry point. WHICH session that is comes from the ONE resolver
	// (tools.TaskResumeSessionFor) — the row's own RUN DIRECTORY first, the row
	// id second. Never the `session_id` column: for an agent-submitted row that
	// column names the session that SUBMITTED the row, and resuming it would
	// replay another conversation into this one (measured: 47 rows here name one
	// chat session they never ran under). The disagreement is reported below,
	// not silently dropped (Rule 15).
	rs := tools.TaskResumeSessionFor(target)
	targetArg := strings.TrimSpace(rs.SessionID)
	if rs.Differs() {
		fmt.Fprintf(os.Stderr, taskRunRetryNoteFmt, target, rs.Note())
	}
	// (4) the double-run gate, asked BEFORE the spawn (the pre-flight shape the
	// dead-holder arm uses): a verified-LIVE holder means a run exists and a
	// resume would race it, and a FOREIGN verdict means the row may be live on
	// another host. Either way: refuse, with the verdict named.
	if ok, verdict := tools.TaskRunChildResumable(target, os.Getpid()); !ok {
		fmt.Fprintf(os.Stderr, taskRunRetryRefusedFmt, target,
			"the row's holder verdict ("+verdict+") does not admit a resume — it would risk a double-run")
		return
	}
	action, logPath, pid, spawnErr := taskRunResumeSpawn(target, targetArg)
	if pid <= 0 {
		// NOTHING started: an exec failure is not the run's fault and must NOT
		// consume an attempt (the same pid==0 contract the dead-holder arm
		// reads).
		fmt.Fprintf(os.Stderr, taskRunRetryRefusedFmt, target,
			"the resume run could not be started: "+errText(spawnErr))
		return
	}
	// The child EXISTS: this is an attempt, recorded BEFORE its outcome is
	// known — a wrapper killed a millisecond later must not be able to restart
	// the same attempt forever.
	if noteErr := tools.TaskRunRetryNote(target); noteErr != nil {
		fmt.Fprintf(os.Stderr, "task %s: retry attempt %d started but could not be recorded (%v) — the cap may not hold\n",
			target, spent+1, noteErr)
	}
	// THE HANDED-OFF RUN IS PUBLISHED TO THE ROUND (task 01a15bd1): the resume
	// is Setsid'd and this wrapper does NOT wait for it, so without this the
	// round returns while a process it started is still running — measured as
	// an 8m27s survivor holding the round's worktree. workerRound's tail waits
	// on this pid (worker_round_retry_orphan.go carries the measurement).
	workerRoundNoteRetryResume(pid)
	if action == "" {
		action = runcmd.Command(runcmd.ResumeArgv(targetArg, target))
	}
	if spawnErr != nil {
		fmt.Fprintf(os.Stderr, taskRunRetryResumedFmt+taskRunRetryImmediateExitNote,
			target, spent+1, tools.TaskRunRetryCap, action, pid, logPath, errText(spawnErr))
		return
	}
	fmt.Fprintf(os.Stderr, taskRunRetryResumedFmt, target, spent+1, tools.TaskRunRetryCap, action, pid, logPath)
}

// taskRunResumeSpawn is the ONE spawn seam of the retry ladder: production
// resolves it to tools.SpawnTaskResume (which launches `gopi session resume <target>
// --task <task-id>` DETACHED, the same implementation the merger's dead-holder
// arm uses — Rule 14: one spawn mechanism, never a second one). A test that
// drives the ladder in-process swaps it for a stub, so the CAP and the
// handover are exercised without starting a real run.
var taskRunResumeSpawn = tools.SpawnTaskResume

// taskRunRetryImmediateExitNote is appended to the retry line when the resumed
// run started and died at once (it could not get past its own startup): the
// attempt WAS spent, and the operator must see both facts.
const taskRunRetryImmediateExitNote = " — note: that run exited immediately: %s\n"

// errText renders an error for a one-line operator report ("" for nil).
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// reconcileChildMirror is the parent-side close-out seam (epic
// t-1788384486): after the wrapper's OWN writeback lands, reconcile any
// LEGACY in_progress mirror row (the row tagged wrapper:<id> — the TAG is the
// mirror's identity, its provenance is not part of the lookup; task-single-row-exec
// no longer mints mirrors, but pre-fix runs may still carry an in_progress one)
// to the same terminal state. The wrapper row was
// already closed by writebackOutcome above. A child that died abnormally
// never ran its END hook — the parent closes the mirror itself, by its own
// action, with the exit evidence as the reason. Best-effort: failures are
// logged inside TaskReconcileMirrorCloseout, never fatal to the run (the
// wrapper row's terminal write is the authoritative outcome). No-op when no
// in_progress mirror exists.
func reconcileChildMirror(wrapperTaskID, status, reason string) {
	if err := tools.TaskReconcileMirrorCloseout(wrapperTaskID, status, reason); err != nil {
		fmt.Fprintf(os.Stderr, taskProgName+": mirror close-out for %s failed (continuing): %v\n", wrapperTaskID, err)
	}
}

// runMethodFor resolves the run-provenance stamp for a target right before
// execution: the method (worker when the worker latch holds the target — the
// merger executes no tasks since the serve-worker-split, so the remaining
// runTaskRunOnce callers are manual + worker) and the
// VERBATIM re-runnable command line. run_cmd on a crashed row doubles as the
// resume recipe, so it must be a command an operator can actually paste —
// never a human-readable description of the launch (the defect nick reported:
// `gopi worker (claim→run round, target <id>)` is a parenthesis, not a
// command).
//
// The command is reconstructed from argv the way THIS process was launched:
// the binary it is (os.Executable(), reduced to a basename by the renderer),
// the settings context it inherited (`--settings X` — the settings file IS
// the provider/api_key/model identity, so a bare `gopi task
// run T` would replay under the WRONG provider), the `task run` verb, and
// the target the run ACTUALLY executes (a `next`/empty launch claim now
// resolves to the claimed id, so the recorded command re-runs that task
// rather than claiming a fresh one), plus the caller's --report-to/--repo
// return path (a dispatched node's own row must carry its full recipe).
func runMethodFor(target string, opts *tasksOptions) (method, cmdline string) {
	if workerWriteback == target {
		// A worker round runs INSIDE the worker process (runTaskWorker claims
		// and calls this in-process), so the process's OWN argv IS the verbatim
		// launcher command — including the --worker-log-root/--once the
		// launcher chose. Re-runnable as written, so it is recorded as written
		// rather than reconstructed.
		return runcmd.MethodWorker, runcmd.Command(os.Args)
	}
	// The launcher's own command word: this process's executable when the OS
	// reports it, else the launcher identity a wrapper published (env), else
	// the conventional name — runcmd owns that resolution.
	binary := runcmd.Command([]string{runcmd.Launcher()})
	// The settings token-pair is a process-global flag that precedes the
	// subcommand, so it slots in between the binary and the verb:
	// `gopi --settings X worker T` (a `--settings X gopi worker T` prefix
	// would not be re-runnable — the shell would try to exec `--settings`).
	// config.LaunchSettingsArg already shell-quotes the path with the same
	// rules runcmd.Command uses, so its output is carried verbatim (quoting
	// it twice would break a spaced path).
	settings := config.LaunchSettingsArg()
	// The recorded tail is the CURRENT runner spelling (epic worker-run-entry,
	// positional since worker-positional-taskid): `gopi worker <target>` runs
	// exactly this row, which is what the retired `gopi task run <target>` did.
	// Recording the retired spelling would make the stored recipe a command that
	// only prints a removal message — and run_cmd is the RESUME recipe for a
	// crashed row, so it must re-run (Rule 15: a stored command that cannot run
	// is a silent failure).
	tail := []string{runcmd.VerbWorker, target}
	if opts != nil && opts.reportTo != "" {
		tail = append(tail, runcmd.FlagReportTo, opts.reportTo)
	}
	if opts != nil && opts.repo != "" {
		tail = append(tail, runcmd.FlagRepo, opts.repo)
	}
	line := binary
	if settings != "" {
		line += " " + settings
	}
	return runcmd.MethodManual, line + " " + runcmd.Command(tail)
}

// remoteReportTimeout bounds each remote report-back ssh call (two calls
// per remote writeback: task update + git push).
const remoteReportTimeout = 30 * time.Second

// taskSessionExt is the session JSONL filename suffix, spelled once in the
// shared classification (internal/sessionlayout.JSONLSuffix) rather than
// mirrored from internal/tools' unexported taskSessionJSONLExt (Rule 14:
// one value, one home — the leaf package is importable from anywhere).
const taskSessionExt = sessionlayout.JSONLSuffix

// newestSessionJSONL resolves the child session id for the model backfill
// seam: the newest session JSONL written at/after started in the task-run
// child's directory. The child (a `task run` executor) writes its run file
// as sessions/<taskID>/<uuid>.jsonl (session-dir-unify S1).
//
// THERE IS NO FALLBACK TO THE SESSIONS ROOT, AND THAT IS THE FIX (epic
// 01a17df9). The pre-fix version scanned the profile's sessions ROOT next — the
// LEGACY FLAT layout — when the task dir held nothing yet. A chat session is
// written CONTINUOUSLY, so its flat record is newer than the run's own log
// inside any run window, and the poll handed back the console's session id; the
// row then stamped it permanently (fill-only-while-empty). Two worker-claimed
// rows shared one chat session id that way while their own
// sessions/<task-id>/ directories held the truth. Only the ROW'S OWN directory
// answers this question now; "" means "nothing yet", and the writeback's
// deferred stamp fills the column from the same directory later.
//
// The returned id is additionally gated at the STAMP (tools.TaskBackfillSession
// asks taskSessionStampRefusal), so a future caller that reintroduces a wider
// scan is refused by name rather than silently poisoning the column.
func newestSessionJSONLAfter(started time.Time, taskID string) string {
	if taskID == "" {
		// No row, no run directory: nothing to name. (The old flat scan had no
		// business answering this either.)
		return ""
	}
	dir, err := config.GopiDir()
	if err != nil {
		return ""
	}
	return newestSessionJSONLIn(filepath.Join(dir, config.SessionsDirName, taskID), false, "", started)
}

// taskSessionPollInterval bounds the bug054 early-stamp poll: how long to
// wait between checks for the child's session JSONL. The child writes and
// syncs session_start immediately at startup, so one or two polls normally
// suffice; the interval is deliberately short (Rule 1: named constant, one
// place to tune).
const taskSessionPollInterval = 100 * time.Millisecond

// taskSessionPollMax is the bug054 early-stamp poll's ceiling: how long to
// keep looking for the child's session before giving up and letting the
// writeback's deferred backfill take over. The child could legitimately be
// slow to spawn (cold Go binary, slow FS); a miss here never fails the run.
const taskSessionPollMax = 5 * time.Second

// waitChildSessionJSONL polls for the child's session JSONL in the run
// window (started..started+taskSessionPollMax): the newest .jsonl under the
// sessions root — the task-run child's sessions/<taskID>/ directory first —
// whose modtime is at/after started. Returns the session id (run-uuid), or
// "" when nothing appears in time — best-effort, a miss just skips the
// early stamp (writeback backfills later). The child writes+syncs its
// session_start event immediately at startup (NewSessionLogger), so the file
// exists right after spawn.
func waitChildSessionJSONL(started time.Time, taskID string) string {
	deadline := started.Add(taskSessionPollMax)
	for {
		if sid := newestSessionJSONLAfter(started, taskID); sid != "" {
			return sid
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(taskSessionPollInterval)
	}
}

// taskNotifyF writes an early-stamp failure to stderr (mirrors the
// internal/tools taskNotifyLog face — package main can't import the
// unexported helper, and a stamp failure is a side-channel note, never a
// run blocker).
func taskNotifyF(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[task-notify] "+format+"\n", args...)
}

// newestSessionJSONLIn scans dir (best-effort, "" on any error) for the
// newest session RECORD file whose modtime is at/after started (zero time =
// any) and whose name differs from excludeName. Returns the filename without
// the extension (the session id).
//
// The record test is the SHARED classification (internal/sessionlayout), not
// a "*.jsonl" suffix match: dir is either the sessions ROOT (parentIsRoot —
// the legacy flat layout, where every bare *.jsonl is a record) or a task-run
// dir (each *.jsonl is a run log). A bare session dir's side files are not
// records, so this function can never return a session id read off a
// provenance record.
func newestSessionJSONLIn(dir string, parentIsRoot bool, excludeName string, started time.Time) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	newest := ""
	var newestMod time.Time
	for _, e := range entries {
		name := e.Name()
		// The classification decides whether this FILE is a session record
		// (dir is the sessions root — legacy flat records — or a task-run
		// dir: both hold records, and sessionlayout is the one place that
		// says so). A DIRECTORY entry is skipped explicitly: a session dir
		// classifies as a record too, but its name is a directory, not a
		// session id, so it must never be stamped. Reaching here for a BARE
		// session dir (a future caller) is safe as well — its side files
		// classify as non-records, so a session id can never be read off a
		// provenance record.
		ent := sessionlayout.Classify(dir, parentIsRoot, e)
		if ent.IsDir || ent.Kind != sessionlayout.KindRecord {
			continue
		}
		if excludeName != "" && name == excludeName {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !started.IsZero() && info.ModTime().Before(started) {
			continue
		}
		if newest == "" || info.ModTime().After(newestMod) {
			newest = name
			newestMod = info.ModTime()
		}
	}
	return strings.TrimSuffix(newest, taskSessionExt)
}

// writebackOutcome closes out a task run's outcome (the shared
// completed/failed path). Without --report-to this is the plain local
// writeback (tools.TaskRunWriteback). WITH --report-to (the M4.7 remote
// face, t-1788046891): the remote is STATELESS — the dispatcher passed the
// return path on argv — so the outcome is reported BACK to the center over
// ssh (`ssh <report-to> gopi task update <id> --status X`) and the executed
// branch is pushed back (`git push <repo> HEAD`). A local mirror writeback
// still runs first (the remote keeps its own audit trail); report failures
// are logged, never panic — the task ran, the report is best-effort.
//
// THE LOCAL WRITE'S ERROR IS RETURNED (epic worktree-dirty-residue,
// 2026-09-27): it used to be printed and dropped, and that single dropped
// error is what let the success arm announce a completion the ledger had
// REFUSED (Rule 30's close-out gate). The report-back and push legs below stay
// best-effort and are NOT part of the returned value — they describe a
// STATELESS remote, while this error is the local ledger's own verdict on the
// row. A caller that ignores it is discarding the ledger's answer.
func writebackOutcome(taskID, status, resultSummary, reportTo, repo string) error {
	// Engine-truncation rule (runend): the wrapper writes the HUMAN row's
	// terminal status AFTER cmd.Wait — but a child exit code of 0 also covers a
	// run the ENGINE stopped (wallclock cap / stuck net / iteration budget).
	// The child's end hook records WHY on the row; read it back and refuse the
	// `completed` claim here too, so the human row cannot close with a false
	// success that the auto row (and the merger) would then build on. Reading
	// the ledger rather than re-deriving from the exit code is what keeps the
	// two writers in agreement.
	if runEnd := tools.RunEndReasonFor(taskID); runEnd.Truncated() && status == tools.TaskStatusCompleted {
		status = tools.TaskStatusFailed
		resultSummary = strings.TrimRight(resultSummary, " ") + " | " + runEnd.Detail() + runend.RecoveryHint(taskID)
	}
	// task-report seam (epic task-report): SEAL the bounded report FIRST — at
	// the run's terminal moment, by the harness, deterministically. The seal
	// bounds a model-authored report to the cap (or writes an honest skeleton
	// when the model wrote nothing) and returns the ledger line naming the
	// file + its size + any missing required section. Appending that line to
	// the result digest makes the report's path part of the append-only
	// task_events detail — the index survives the session directory's pruning.
	// Best-effort by contract: a seal failure is LOGGED and the digest is
	// recorded without the report line; it never fails the terminal write
	// (the outcome is the authoritative fact, the report is its narrative).
	if line, sealErr := tools.SealTaskReportOutcome(taskID, status); sealErr != nil {
		fmt.Fprintf(os.Stderr, taskProgName+": report seal for %s failed (continuing): %v\n", taskID, sealErr)
	} else if line != "" {
		resultSummary = strings.TrimRight(resultSummary, " ") + " — " + line
	}
	// 1. Local writeback always (the remote's own ledger stays truthful).
	// The WORKER loop's rounds route through TaskRunWritebackTagged so the
	// row carries the worker provenance tag (M4.20 ①); a plain `task run`
	// keeps the tag-free writeback.
	var wbErr error
	if workerWriteback == taskID {
		// Output destination stamp (epic child-stream-task-output): the path
		// the spawn seam pointed the EXECUTOR CHILD's stdout/stderr at rides
		// the terminal result digest, so `task show` carries the exact file
		// even for a row whose session dir is later pruned. The path is the
		// seam's latch — empty when the log could not be opened (the streams
		// then stayed on the spawner's own).
		if p := taskExecutorOutputLogPath; p != "" {
			resultSummary = strings.TrimRight(resultSummary, " ") + " — " +
				tools.WorkerOutputLogSummaryLine + p
		}
		_, wbErr = tools.TaskRunWritebackTagged(taskID, status, resultSummary)
	} else {
		_, wbErr = tools.TaskRunWriteback(taskID, status, resultSummary)
	}
	if wbErr != nil {
		fmt.Fprintf(os.Stderr, taskProgName+": writeback failed (continuing): %v\n", wbErr)
	}
	// THE COMPLETION WITNESS (epic 01a182bf) — AFTER the terminal write, never
	// before: this arm ANNOTATES a landed claim, it does not gate one (see
	// task_completion_witness_seam.go for the criterion and why `completed` is
	// never turned back into `failed`). A row whose claim has no artefact behind
	// it gets ONE attributed trail entry; a delivered row and a row completing on
	// its own measured conclusion are reported and left silent on the trail.
	recordCompletionWitness(taskID, status)
	// THE ONE EARLY RETURN gives the caller the local verdict before the
	// stateless-remote legs (they add nothing to it — see the doc comment).
	if reportTo == "" {
		return wbErr
	}
	// The alias is an ssh argv element: an option-shaped one would be parsed
	// by ssh as an OPTION (-oProxyCommand=…) — refuse the hop (best-effort
	// path) and keep the local writeback above as the outcome record.
	if _, vErr := tools.ValidateSSHPeerAlias(reportTo); vErr != nil {
		fmt.Fprintf(os.Stderr, taskProgName+": report-back refused: %v\n", vErr)
	} else {
		// 2. Report the status back through the ssh alias.
		cmdCtx, cancel := context.WithTimeout(context.Background(), remoteReportTimeout)
		defer cancel()
		centerCmd := fmt.Sprintf("gopi task update %s --status %s", taskID, status)
		out, err := exec.CommandContext(cmdCtx, "ssh", reportTo, centerCmd).CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, taskProgName+": report-back to %s failed (continuing): %v (output: %s)\n",
				reportTo, err, strings.TrimSpace(string(out)))
		} else {
			fmt.Printf("reported %s → %s: %s\n", status, reportTo, strings.TrimSpace(string(out)))
		}
	}
	// 3. Push the executed branch back (repo target from the dispatcher).
	if repo != "" {
		// ssh-shaped target ("<alias>:<path>"): the alias half reaches ssh
		// as an argv element inside git — refuse an option-shaped one.
		if strings.Contains(repo, ":") {
			if _, vErr := tools.ValidateSSHGitTarget(repo); vErr != nil {
				fmt.Fprintf(os.Stderr, taskProgName+": report-back push refused: %v\n", vErr)
				return wbErr
			}
		}
		// The push leg runs `git` against the run's RESOLVED REPO (the task's
		// deliverable repo, latched at spawn by taskExecutorCwd), never
		// against this process's cwd: the wrapper has moved into the task's
		// session directory (epic task-session-cwd), and the session
		// directory is not inside the executor's repo — an unanchored `git
		// rev-parse` there resolves whatever repo encloses the profile (or
		// none at all), so the branch name and the push would describe the
		// wrong tree. The resolution falls back to the launch workdir for a
		// caller that never resolved one. `-C` names the repo explicitly (the
		// same convention every internal/tools git helper uses).
		gitArgs := []string{}
		// The push leg's anchor is the TASK's resolved repo (taskExecutorCwd's
		// answer, latched at spawn), falling back to the launch workdir for a
		// caller that never resolved one.
		if wd := taskRunRepoDir; wd != "" {
			gitArgs = append(gitArgs, "-C", wd)
		} else if wd := taskExecutorWorkdir(); wd != "" {
			gitArgs = append(gitArgs, "-C", wd)
		}
		branch, err := exec.Command("git", append(append([]string{}, gitArgs...), "rev-parse", "--abbrev-ref", "HEAD")...).Output()
		branchName := strings.TrimSpace(string(branch))
		if err != nil || branchName == "" {
			fmt.Fprintf(os.Stderr, taskProgName+": report-back push skipped (no branch: %v)\n", err)
			return wbErr
		}
		// THE TEST FENCE (internal/pushfence — ONE predicate, every push
		// boundary). This boundary had none: it renders its target from the
		// caller's --repo/--report-to and hands it straight to git, so a test
		// that drove this leg pointed real bytes at whatever the flag named.
		// Measured residue (2026-10-26, peerbox): refs/heads/feat/exists, an
		// orphan root of a `t <t@t>` fixture, reflog `push`.
		if why := pushfence.Refusal(repo); why != "" {
			fmt.Fprintf(os.Stderr, taskProgName+": report-back push refused before it ran: %s\n", why)
			return wbErr
		}
		pushCtx, pushCancel := context.WithTimeout(context.Background(), remoteReportTimeout)
		defer pushCancel()
		pushOut, err := exec.CommandContext(pushCtx, "git", append(append([]string{}, gitArgs...), "push", repo, "HEAD:"+branchName)...).CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, taskProgName+": git push to %s failed (continuing): %v (output: %s)\n",
				repo, err, strings.TrimSpace(string(pushOut)))
		} else {
			fmt.Printf("pushed %s → %s\n", branchName, repo)
			notifyBranchPushed(branchName, repo)
		}
	}
	return wbErr
}

// Rule 1 constants for the branch-backlog feature (one place, no literals
// at call sites).
const (
	// branchPushedTag marks the ledger hint rows the push-back creates.
	branchPushedTag = "branch-pushed"
	// NOTE (F4, devdoc/merge-resolve-effort-audit.md): the six sibling constants
	// that used to live here (gitBranchListTimeout, serveBranchScanThrottleMin,
	// mergeBacklogTagPriority, dedupeRecentTaskLimit, archiveTagScanTimeout and
	// the backlog template path) timed the serve pass's unmerged-branch
	// inventory — retired with it. The push-back hint above needs none of them:
	// it names the branch the push just delivered, not an inventory of branches.
)

// notifyBranchPushed records a 'branch-pushed' hint task in the LOCAL ledger
// after a successful push-back (writebackOutcome step 3, db-sync HP face).
// The center's serve tick inventory may not see the branch until the next
// sync — this row makes the pending-merge state visible in the 台账
// immediately. Best-effort: a failed submit is logged, never fatal (the
// push itself already succeeded).
func notifyBranchPushed(branchName, repo string) {
	summary := fmt.Sprintf("branch-pushed: %s awaits merge decision (pushed to %s)", branchName, repo)
	payload := fmt.Sprintf(
		"Branch %s was pushed back to %s by a remote task run (db-sync HP face). "+
			"The merge decision belongs to the gopi verify/merge loop — not this row. "+
			"This is a VISIBILITY hint: merge it, archive it (`gopi task archive <name> --reason <code>`), "+
			"or close this row with the decision recorded. Archiving goes through the ONE writer: a hand-minted "+
			"archive tag carries no reason record beside it, so the decision cannot be reviewed or reversed. "+
			"Left unmerged, the conflict surface grows "+
			"with every master generation (P2.5: 3 behind = 8 conflicts).",
		branchName, repo)
	id, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: summary,
		Payload: payload,
		Tags:    branchPushedTag,
		Source:  tasksource.Merger,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, taskProgName+": branch-pushed hint not recorded (continuing): %v\n", err)
		return
	}
	fmt.Printf("[branch-pushed] hint task %s recorded (merge backlog visibility)\n", id)
}

// printTaskServeAliasPointer prints the alias's ONE pointer line: the `-h` face
// of `gopi task serve` / `gopi task merger`.
//
// EXIT 0, DELIBERATELY — AND THE RUN PATH REFUSES (epic merger-delete-dead-impl,
// 2026-09-19). The `-h` scan calls this INSTEAD of retireMergerFace for exactly
// this reason: `-h` is a usage query and the CLI contract answers usage queries
// (the same P0-1 rule the retired `task run -h` case states). What the operator
// reads must not send them in a circle — and the face it used to send them to,
// mergeProgTopLevel (`gopi merge`), IS ITSELF GONE (epic merge-removal, item
// 2): the pointer that named it sent a reader from one refusal into another,
// the dead end this file's own neighbouring comment calls out. So the line says
// what is true instead of naming a command. Every word still comes from a
// constant (Rule 1b) — the format is the constant, and it no longer interpolates
// a program name.
func printTaskServeAliasPointer(verb string) {
	fmt.Printf(taskServeAliasPointerFmt, verb)
}

// taskServeAliasPointerFmt is the ONE spelling of the alias's help line — the
// format the `-h` face of BOTH deprecated aliases prints (epic
// merger-delete-dead-impl, 2026-09-19). It survives the daemon's deletion
// because the contract it serves is an OPERATOR contract, not a daemon feature:
// a help query is a SUCCESS (exit 0, one line), while a RUN of the same spelling
// is the refusal. Collapsing the two would refuse a usage query, which is the
// P0-1 rule the retired-verb `-h` cases already state — see
// printTaskServeAliasPointer, whose only reader is the help scan.
const taskServeAliasPointerFmt = "gopi task %s is a deprecated alias — the merger daemon is RETIRED; a merge pass is 'gopi task merge' (man/gopi-merge.md documents the machinery)\n"

// THE RETIRED MERGER TICK. `gopi merger` and its deprecated `gopi task
// merger`/`serve` aliases all REFUSE now (2026-09-19, devdoc/merger-retired.md —
// there is no live merger `-h` page to point at, and the page's text is
// preserved there).
//
// mergerPassOnce BELOW HAS NO PRODUCTION CALLER, and the paragraphs that follow
// describe what it WOULD do rather than what production runs. MEASURED 2026-11-01
// (task 01a19466, tree 52657bb6): renaming the mergerPassOnce DECLARATION leaves
// `go build ./...` GREEN (exit 0), so nothing in the compiled package names it;
// its runner chain `runTaskMerger` → `runTaskMergerWithLock` → `mergerPassOnce`
// went with the daemon (epic merger-delete-dead-impl, 2026-09-19) and neither
// runner has a definition in this tree. Everything under the pass —
// mergerSweepRepo, tools.MergerRepoSweep, and the serial-face ladder
// (mergerSerialFaceLegs) — is unreachable for the same reason: the compiler
// names exactly one reference to each, and every one of those sits inside the
// pass. THE FUNCTION IS KEPT FOR RE-DEVELOPMENT, WHOLE, not left live by
// oversight; the reachable merge path today is `gopi task merge` →
// tools.TaskMerge (devdoc/merge-landing-path-ownership.md, and
// `gopi task merge -h`).
//
// What the pass was built as (epic serve/worker-split 2026-09-03, nick's final
// design — REFINED by architecture-fix-v2 2026-09-03; its PROCESS FORM became
// one-tick-and-exit in epic daemon-machinery-removal, 2026-09-17):
// A merger is a
// PURE branch-merge process — it owns NO task execution: claim/run/writeback
// is the worker's job (runTaskWorker), which runs on BOTH client and server.
//
// Architecture-fix-v2 (nick's SIMPLER design): the merger's real work is a
// CONFIG-DRIVEN PURE GIT SWEEP — NOT task-oriented, NOT DB-driven. Each pass
// iterates the settings.json "repos" table keys (the sweep list) and, per
// repo, runs MergerRepoSweep: cd to the repo, merge every branch whose
// worktree-protection has been removed (clean merges land immediately;
// conflicts dispatch onto the normal task queue for a worker/LLM to resolve
// — the merger only merges clean), tidy merged worktrees, then rebuild the
// repo's declared build if the turn landed work. Whether a task row tracks
// a branch is irrelevant — merging is fully decoupled from the task ledger.
// The cross-node git-sync + adoption channels (peer push-back merges) run
// once per pass and are no-ops when no sync peer is declared. The
// merge-backlog row pipeline (branch-scan/audit/executor) is retired — the
// per-repo local sweep replaces it.
//
// A FAILED pass never kills the run: each step is fail-soft and the loop
// continues to the next tick. The worker's lifecycle passes do NOT run here —
// this is server-side merge-only. (CORRECTED 2026-10-30, task 01a18ef7: this
// sentence used to name "the stale sweep + finish-cleanup sweep"; the worker
// has neither any more, and the task-lifecycle pass was deleted —
// task_lifecycle_tick.go. The retired tick ran this pass once and exited;
// nothing here starts, stops or watches a process.)
//
// M4.5 守护化: an flock (LOCK_EX|LOCK_NB) on ~/.gopi/serve.lock makes the
// pass single-instance — a second writer on the same machine refuses with
// exit 1 (the lock file also carries the holder's pid for the error report).
// The lock FILE name stays serve.lock for backward compat with running
// deployments (two writers must never double-merge).
// NO --repo flag, no cwd assumption: the pass's own cwd is irrelevant —
// it cd's per configured repo.

// mergerPassOnce runs ONE full merge-round pass over the CONFIGURED repos
// (epic architecture-fix-v2, config-driven pure git sweep). The merger is
// NO LONGER task-oriented or DB-driven: settings.json "repos" is the sweep
// list, and each pass cd's to every configured repo and merges all
// branches whose worktree protection has been removed (nick's exact model:
// "in the config file we specify the repo.path and the merger just
// periodically cd's to repo.path and merges all branches"). Whether a task
// row tracks a branch is irrelevant to the merge decision.
//
// Per configured repo, the pass:
//
//	① runs the per-repo pure-git sweep (MergerRepoSweep): merge clean
//	  unprotected branches, dispatch conflicts onto the normal task queue,
//	  tidy merged worktrees;
//	② rebuilds the repo after a turn that landed work (per build contract).
//
// The cross-node git-sync + adoption channels (client push-back merges
// across sync peers) run ONCE per pass — they are the peer-delivery face,
// self-gating no-ops when no sync peer is declared, distinct from the
// local-branch config sweep. The merge-backlog row pipeline (branch-scan /
// audit / executor) is retired: it existed to discover + land branches on
// ONE cwd-derived repo; MergerRepoSweep's local branch sweep replaces it
// per configured repo, so no task rows mediate a clean local merge.
//
// It used to be shared by the merger's pre-loop drain and its per-tick loop, and
// that is why the body is kept as ONE unit: both callers are gone with the
// daemon (see the block above), so the pass is preserved WHOLE for
// re-development rather than by halves — a re-development that revived only the
// sweep loop would silently leave the serial-face legs where they are now.
// A FAILED pass never kills the caller: each step is fail-soft.

// NOTE (epic version-identity, 2026-09-24): gatedRevert stood here. It built
// the gate's revert handle carrying the gated branch as the round's landed
// branch — and its ONLY consumer was the version-tag mint's message
// ("what does this version entail"). The mint is deleted, the field it fed
// (FailedMergeRevert.MergedBranches) is deleted with it, and this helper had
// no other caller, so it went too rather than sitting as dead code that
// documents a coupling the epic removed. The gate's own revert handle was
// built by CaptureMergePassScope, in merge_pass_verify.go, which never needed
// the branch list — and that file, together with the retired top-level merge
// verb's whole post-pass verify/withdraw lane, was DELETED on 2026-11-01 (its
// last production caller, cmd/agent/merge_run.go, had already gone). The gate
// builds its revert handle from CaptureFailedMergeRevert (merge_revert.go)
// directly today, so the branch list is still unnecessary to it.

// --- the stale report's carrier: `staleSweep`, NOT a pass-local clock ---
//
// serveStaleSweep (+ its serveStaleThrottleMin clock) stood here until
// 2026-10-30 (task 01a18ef7). It was a throttled wrapper whose ONLY production
// caller was taskLifecyclePassOnce — the pass that had no production caller at
// all — so it swept nothing in production while its docstring named "serve
// passes" as the trigger. It is DELETED with the pass.
//
// THE STALE REPORT ITSELF IS NOW DELETED TOO (2026-11-05). `staleSweep` →
// `agentStaleSweep` → `agent.DetectStaleTasks`, its three carriers
// (`gopi task list --sweep`, and the `gopi task run <id>` / `gopi session
// resume <id>` prologues), the persisted throttle stamp in
// cmd/agent/tasklistsweep_gate.go and the re-arm UPDATE that kept a reported
// row from being reported twice are all gone. The operator's ruling: no process
// watches another process for it. What a stale row gets instead is a READING —
// `gopi task list` / `gopi task show <id>` resolve the holder's identity per
// row (taskproc.go's ladder) and say so, and the repair is the ledger's own
// verbs. Nothing about a row changes because some other process looked at it.

// finishCleanupDeadHolderReclaimLines is the dead-holder repair leg (epic
// 2026-09-12, defect 2) — a shared definition whose MERGER face is the only one
// LEFT DECLARED, and which no production process currently reaches:
//
//   - the MERGER's pass (mergerSerialFaceLegs ②③, via mergeDeadHolderReclaimLeg).
//     MEASURED 2026-11-01 (task 01a19466, tree 52657bb6): that pass,
//     mergerPassOnce, has NO production caller — renaming its declaration leaves
//     `go build ./...` green. So the reclaim arm is declared, tested, and
//     unreached by every shipped command. Recorded here so a reader does not
//     infer a running repair from this paragraph (Rule 15: a gap is named, never
//     covered by prose). Re-homing it is a duty of the re-development.
//
// THE WORKER-TICK CALL SITE IS RETIRED, NOT THE ARM. The paragraph that stood
// in the const block here named a second caller — the worker's lifecycle pass,
// `serveFinishCleanupSweep` — and that whole mechanism (its scans, its ledger
// writes, its worktree/branch teardown) was retired with
// internal/tools/task_finish_cleanup*.go. What survived the retirement is this
// leg and the tools.ReclaimDeadHolderWorktrees it calls: it reclaims a DIRECTORY
// left behind by a provably dead holder — a resource leak and a gate-[c]
// blocker, not a ledger verdict — so nothing about a stuck row's history is
// rewritten by it. A reader looking for the worker-tick wiring will not find it
// in task_lifecycle_tick.go: its retirement note there is the pointer.
//
// WHY IT IS THE MERGER'S (2026-09-13, still the reason): the reclaim used to be
// reachable only from a WORKER pass behind a 10-minute throttle. A host running
// `gopi merger` and no worker therefore never ran the arm at all, while the
// merger's own protected line kept advertising the branches as "reclaimable".
// Measured live: 20 protected branches, 7 named dead-holder, the merger ticking
// every 5m for 1h20m, and ZERO reclaim passes in its log — the advertisement
// outlived the arm that was supposed to honour it, and the branch was withheld
// from merging the whole time.
//
// It is NOT throttled: this is a BOUNDED ledger scan with three guards (dead
// claim pid, clean worktree, no live process inside), it frees the DIRECTORY
// only — never the branch — and the merger pass is itself interval-bounded, so
// a throttle's only effect here would be to delay a deliverable's release. It
// returns one line per freed (or named) branch; refusals go to the operator sink
// inside the arm.
func finishCleanupDeadHolderReclaimLines() []string {
	return tools.ReclaimDeadHolderWorktrees(true)
}

// sweepStatusLinePrefix is how a scan helper reports a soft failure through its
// []string result (the resident pass prints these verbatim as log lines). The
// one-shot verb turns them back into errors, so the prefix is spelled once
// (Rule 1b) rather than matched by a bare literal on both sides.
const sweepStatusLinePrefix = "sweep: "

// --- sweep ownership handover: RETIRED (epic sweep-report-not-act, 2026-09-19) ---
//
// setFinishCleanupSweepOwnerRemoverWired used to hand a row-owned worktree to
// that row's delivery chain so the sweep never ran its own blind branch -d. The
// handover had exactly one consumer — the reaper's mutating arm — and the
// reaper mutates nothing now: the pair is REPORTED with its owner evidence, and
// whoever owns it (the row's chain, or a human reading the report) resolves it.
// The seam itself (tools.SetFinishCleanupSweepOwnerRemover) stays declared so
// the retired disposal path and its tests remain honest about what they do —
// but no production face wires it, so nothing in a resident tick can be talked
// into removing a directory. Its production clear (task_lifecycle_tick.go) is
// kept: a nil seam is the invariant a resident tick establishes.

// --- merged-branch cleanup sweep (serve pass step) ---

// worktreeReapLegLabel names the reap's log leg on the carrier that ACTUALLY
// runs it (`gopi task merged-cleanup`, mergedCleanupPassOnce below). It mirrors
// the finish-cleanup leg's own label spelling (`[finish-cleanup]`), so one grep
// of an operator's transcript finds every registration sweep. It was declared in
// task_lifecycle_tick.go until 2026-10-30, beside a pass that never ran it;
// it lives here now, beside the carrier.
const worktreeReapLegLabel = "worktree-reap"

// worktreeReapDryRun is the reap's dry-run switch as its PRODUCTION carrier runs
// it (`gopi task merged-cleanup` → serveWorktreeReapSweep). Production leaves it
// false: the sweep is destructive by design (collecting a registration nothing
// else on the host will ever collect is the whole point). It is a package-level
// seam only because the reachability proof must be able to drive the carrier
// without mutating the host a test runs on.
var worktreeReapDryRun = false

const (
	// mergedCleanupIntervalEnv gates the serve-tick cleanup pass: seconds
	// (Rule 2 — read in exactly one place below). Absent/0 = the default;
	// negative = DISABLED; parse-fail = error (a typo is surfaced, not
	// silently ignored — the adoption interval's contract).
	mergedCleanupIntervalEnv = "GOPI_CLEANUP_INTERVAL"

	// mergedCleanupIntervalDefault matches the stale/adoption cadence
	// (15min): merged branches tolerate a 15-minute cleanup lag.
	mergedCleanupIntervalDefault = 15 * time.Minute

	// mergedCleanupScanMax caps branches cleaned per pass (a runaway
	// branch factory cannot turn one tick into a fork bomb).
	mergedCleanupScanMax = 10
)

// serveMergedCleanupLastTick is the sweep's process-wide throttle clock.
var serveMergedCleanupLastTick time.Time

// serveMergedCleanupSweep runs tools.MergedCleanupSweep over the gopi repo
// at most once per mergedCleanupIntervalDefault across serve passes. The
// FIRST pass always sweeps; later passes are throttled. Returns one log
// line per handled branch (removed/skipped-with-reason/failed). Fail-soft
// per branch.
func serveMergedCleanupSweep() []string {
	interval, disabled, err := mergedCleanupIntervalFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[serve] merged-cleanup: %v\n", err)
		return nil
	}
	if disabled {
		return nil
	}
	now := time.Now()
	if !serveMergedCleanupLastTick.IsZero() && now.Sub(serveMergedCleanupLastTick) < interval {
		return nil
	}
	serveMergedCleanupLastTick = now
	repo := tools.GitSyncRepoRoot()
	if repo == "" {
		return []string{sweepStatusLinePrefix + "not inside a git checkout"}
	}
	rep := tools.MergedCleanupSweep(tools.MergedCleanupOptions{Repo: repo, ScanMax: mergedCleanupScanMax, ReportOnly: true})
	lines := mergedCleanupReportLines(rep)
	// NOT the dead-registration reap's carrier any more — that reap lives on
	// `gopi task merged-cleanup` (see serveWorktreeReapSweep; corrected
	// 2026-10-30, task 01a18ef7: it used to name the task-lifecycle tick). It was appended
	// HERE on 2026-09-14 (35faf715) on the premise that this function rides a
	// resident tick; the premise stopped holding on 2026-09-04 (d11af9a6,
	// "merger pass is config-driven per-repo sweep"), which deleted this
	// function's ONLY production caller. From then on the reap was reachable
	// from its own tests and from nothing else — a dead registration sat
	// uncollected on a host that runs the merger. One carrier, and it is the
	// tick both resident components already fire.
	return lines
}

// serveWorktreeReapSweep runs tools.WorktreeSweepRun in APPLY mode with the
// sweep's own age default, once per resident tick, and renders its outcome as
// log lines. Only the dispositions the run actually TOOK are logged in detail; a
// run that found nothing to reap logs one summary line (Rule 15: an
// operator can tell "nothing to do" from "everything was kept").
//
// REACHABILITY IS THE POINT OF THIS FUNCTION'S POSITION (epic
// worktree-reap-reachable, 2026-09-16). It was appended inside
// serveMergedCleanupSweep (35faf715) whose only production caller had been
// deleted three months' worth of commits earlier, so on the operational host
// the ownership-independent reap — the ONLY instrument that can see a
// registration no ledger row remembers — ran from nothing but its own tests.
//
// ITS CARRIER IS `gopi task merged-cleanup` (runTaskMergedCleanup), MEASURED
// 2026-10-30. Between 2026-09-19 and that day its only production caller was
// taskLifecyclePass — and after 187d05c removed the worker round's tick call,
// taskLifecyclePass itself had NO production caller, so the reap was again
// reachable from its own tests and from nothing else (the same shape as the
// defect this comment was originally written about, one level up: not a symbol
// left behind by a deletion, but a DUTY). `gopi task merged-cleanup` is a live
// verb on every host that merges, it already takes the merger's work lock (the
// lock the reap needs: it mutates git registrations), and it is the verb whose
// whole subject is "reap merged branches and the worktrees they occupy". So the
// reap rides it — and every other reachability claim about this function must
// name a call site an operator's own command reaches, never a tick.
//
// It is deliberately NOT gated by a clock: this is a cheap, bounded pass (one
// `git worktree list` per repo + a few stats) whose whole purpose is latency —
// an hour of a dead registration is harmless, a day of a live one is a leak.
// The operator typed the command, so the answer is due now. Failure is reported
// and never fails the verb.
//
// It runs with reapDryRun=false (the verb's own "APPLY or nothing" posture); the
// parameter exists so a test can prove the leg is reachable WITHOUT mutating the
// host it runs on.
func serveWorktreeReapSweep(dryRun bool) []string {
	opts := tools.WorktreeSweepOptionsFromFlags(tools.WorktreeSweepFlags{Apply: !dryRun})
	res := tools.WorktreeSweepRun(context.Background(), opts)
	var lines []string
	for _, f := range res.Findings {
		switch f.Disposition {
		case string(tools.WorktreeReapDisposition), string(tools.WorktreeReapBranchDisposition):
			lines = append(lines, "reaped registration="+f.Path+" ("+f.Reason+")")
		case string(tools.WorktreeOrphanEscalateDisposition):
			// The one verdict that asks a HUMAN to act: a dirty registration no
			// ledger row owns. Reported here (the operator reads this log) and
			// alarmed on the notify channel by the run itself (Rule 15).
			lines = append(lines, "escalated orphan="+f.Path+" ("+f.Reason+")")
		}
	}
	for _, p := range res.Pruned {
		lines = append(lines, "pruned: "+p)
	}
	for _, e := range res.Errors {
		lines = append(lines, "reap error: "+e)
	}
	if res.Reaped > 0 || res.BranchesReaped > 0 || res.Escalated > 0 || res.Reclaimed > 0 || len(lines) > 0 {
		lines = append(lines, fmt.Sprintf(
			"worktree-reap: reaped=%d branches-reaped=%d reclaimed=%d escalated=%d repos=%d",
			res.Reaped, res.BranchesReaped, res.Reclaimed, res.Escalated, len(res.Repos)))
	}
	return lines
}

// mergedCleanupReportLines renders one cleanup report as log lines — the ONE
// spelling shared by the serve tick and the `gopi task merged-cleanup` verb
// (Rule 1/13: a second rendering would drift from this one).
func mergedCleanupReportLines(rep *tools.MergedCleanupReport) []string {
	// Report what the pass had to work with, ALWAYS: a sweep that examines
	// rows and changes nothing otherwise logs nothing, and "nothing to do"
	// becomes indistinguishable from "everything was skipped" (Rule 15).
	out := []string{"scan: " + tools.MergedCleanupScanStatus(rep.Repo)}
	for _, s := range rep.Removed {
		out = append(out, fmt.Sprintf("removed branch=%s worktree=%s", s.Branch, s.Worktree))
	}
	// The reported half (epic sweep-report-not-act, 2026-09-19): the pairs the
	// retired disposal would have reaped. The reaper mutates no ref now, so the
	// verdict the operator needs lives HERE — with its owner evidence.
	for _, s := range rep.Reported {
		out = append(out, fmt.Sprintf("would-reap branch=%s worktree=%s: %s", s.Branch, s.Worktree, s.Detail))
	}
	for _, s := range rep.Skipped {
		out = append(out, fmt.Sprintf("skipped branch=%s: %s", s.Branch, s.Detail))
	}
	for _, s := range rep.Notes {
		out = append(out, fmt.Sprintf("%sbranch=%s: %s", tools.MergedCleanupNotePrefix, s.Branch, s.Detail))
	}
	for _, s := range rep.Failed {
		if s.Branch == "" {
			out = append(out, "failed: "+s.Detail)
		} else {
			out = append(out, fmt.Sprintf("failed branch=%s: %s", s.Branch, s.Detail))
		}
	}
	return out
}

// runTaskMergedCleanup implements `gopi task merged-cleanup`: ONE cleanup
// pass over the repo the process stands in (GitSyncRepoRoot), printing the
// full report — every branch reaped, every branch spared with its reason.
// Guard-chain details live in tools.MergedCleanupSweep. Exit is non-zero
// only when no git checkout could be resolved (a usage error, not a sweep
// outcome: a pass that spared everything is a success).
// runTaskSweepWorktrees implements `gopi task sweep-worktrees`: reclaim the git
// REGISTRATIONS of stale task worktrees (the ranked guard the /dev/shm
// assessment found missing). Guard-ladder details and the "one list, one prune
// per repo" rule live in tools.worktreeSweepRun.
//
// DRY RUN IS THE DEFAULT: an operator reads the plan before an unattended run
// takes anything away. --apply is the explicit opt-in.
func runTaskSweepWorktrees(flags *tasksOptions) {
	// Single-instance guard — only when this run can MUTATE git: an APPLY sweep
	// reclaims worktree registrations and reaps branches, the same repo surgery
	// the merger performs (epic merger-always-flock). A DRY RUN only reads and
	// prints its plan, so it stays lock-free: an operator inspecting a busy
	// host must not be refused by the sweep they are diagnosing.
	if !flags.dryRunGate() {
		defer mergerWorkLock(sweepWorktreesFaceProg)()
	}
	opts := tools.WorktreeSweepOptionsFromFlags(tools.WorktreeSweepFlags{
		DryRun: flags.dryRunGate(),
		// The SAME gate, on the destructive field: the engine's posture comes
		// from Apply alone (DryRun: !flags.Apply inside the resolver), so
		// passing the raw --apply here is what made `--apply --dry-run` run as
		// an APPLY. `!dryRunGate()` cannot drift from the lock decision above
		// (one gate, one command).
		Apply:   !flags.dryRunGate(),
		MinAgeH: flags.sweepMinAgeH,
		Max:     flags.sweepMax,
	})
	res := tools.WorktreeSweepRun(context.Background(), opts)
	if flags.jsonOut {
		fmt.Println(tools.WorktreeSweepJSON(res))
		return
	}
	// The BRANCH axis, printed before the registration plan: the sweep's own
	// findings answer "what happens to these REGISTRATIONS", which is silent
	// about a branch an unfinished ledger row still owns (the measured
	// 01a0a996 shape: in_progress, claim pid already gone, worktree already
	// removed — invisible to every registration arm because there is no
	// registration left to judge). One read-only line per attributed branch so
	// the operator sees WHO to resume or clone before deciding on the plan
	// below. It decides nothing and removes nothing: the classifier reports,
	// the arms act.
	for _, repo := range res.Repos {
		for _, l := range tools.BranchAttributionSummaryLines(repo) {
			if len(res.Repos) > 1 {
				fmt.Printf("[%s] %s\n", repo, l)
				continue
			}
			fmt.Println(l)
		}
	}
	mode := "dry-run"
	if !res.DryRun {
		mode = "APPLY"
	}
	fmt.Printf("sweep-worktrees (%s): repos=%d reclaimed=%d would-reclaim=%d reaped=%d branches-reaped=%d escalated=%d\n",
		mode, len(res.Repos), res.Reclaimed, res.WouldReclai, res.Reaped, res.BranchesReaped, res.Escalated)
	for _, f := range res.Findings {
		// The rule and the root are printed BESIDE the disposition: a plan that
		// says "report" without naming the arm that refused it (and which root
		// would bring it into policy) is not actionable, which is exactly the
		// dry run's whole deliverable.
		fmt.Printf("  %-11s [%s] age=%dh %s (%s)\n", f.Disposition, f.Rule, f.AgeHours, f.Path, f.Reason)
		if f.Root != "" {
			fmt.Printf("              judged against root: %s\n", f.Root)
		}
	}
	// The prune is repo-global, so git may have collected a registration this
	// run never judged: its own -v lines are printed verbatim (Rule 15: the
	// operator sees every bookkeeping drop, attributed or not).
	for _, p := range res.Pruned {
		fmt.Printf("  pruned: %s\n", p)
	}
	for _, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "sweep-worktrees: %v\n", e)
	}
	// THE BACKFILL LEG RUNS FIRST, and that order is the point: adopting an
	// unanchored ground is what makes the run-ground axis below able to NAME it,
	// so one --apply does the whole job and the read-out that follows is the
	// measurement of what it did (Rule 31). Under a plan nothing is written and
	// the adoption candidates are printed as a plan instead.
	if flags.sweepBackfillAnchors {
		runTaskSweepBackfillAnchors(flags)
	}
	runTaskSweepRunGrounds(flags)
}

// sweepBackfillAnchorsFlagName is the flag's ONE spelling (Rule 1b): the
// parser's declaration, the admitted-flag table and the runner all reference it.
const sweepBackfillAnchorsFlagName = "backfill-anchors"

// runTaskSweepBackfillAnchors is the run-ground anchor axis' BACKFILL face
// (tools/run_ground_adopt.go): report the clone grounds the axis cannot see but
// can PROVE unowned, and — under the SAME --apply gate as the rest of this verb —
// write one anchor per ground so the collect leg can name them.
//
// It removes nothing, ever. Adoption and removal are two acts with two reasons,
// and a command that turned "no record" straight into "deleted" would have no
// moment at which an operator could read what it decided.
func runTaskSweepBackfillAnchors(flags *tasksOptions) {
	minAge := tools.RunGroundAdoptMinAge(flags.sweepMinAgeH)
	planOnly := flags.dryRunGate()
	adoptable, refused, err := tools.RunGroundAdoptableGrounds(time.Now(), minAge)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sweep-worktrees: run-ground backfill: %v\n", err)
		return
	}
	if len(adoptable) == 0 && len(refused) == 0 {
		return
	}
	mode := "APPLY"
	if planOnly {
		mode = "dry-run"
	}
	fmt.Printf("run-ground backfill (%s): adoptable=%d kept=%d\n", mode, len(adoptable), len(refused))
	for _, a := range adoptable {
		fmt.Printf("  %-11s %s  %s\n", tools.RunGroundAdoptAdoptableLabel, a.Dir, a.Reason)
	}
	for _, a := range refused {
		fmt.Printf("  %-11s %s  %s\n", tools.RunGroundAdoptKeptLabel, a.Dir, a.Reason)
	}
	if planOnly {
		return
	}
	adopted, skipped, aerr := tools.RunGroundAdoptGrounds(adoptable)
	if aerr != nil {
		fmt.Fprintf(os.Stderr, "sweep-worktrees: run-ground backfill: %v\n", aerr)
		return
	}
	fmt.Printf("run-ground backfill applied: adopted=%d skipped=%d\n", len(adopted), len(skipped))
	for _, dir := range skipped {
		fmt.Printf("  kept: %s\n", dir)
	}
}

// runTaskSweepRunGrounds is the RUN-GROUND ANCHOR axis of `gopi task
// sweep-worktrees` (epic worktree-reclaim-two-gaps, Gap B) — a read-out by
// default, and a removal only under the SAME --apply that governs the rest of
// the verb (one gate for one command, Rule 1b).
//
// Why it is a second axis and not another arm of the registration scan: that
// scan judges registrations the LEDGER can be paired against, and its arms all
// key on a row. A ground taken by a run with NO row is exactly what the anchor
// exists to make attributable — so the record, not the absence, decides, and the
// output names both directions with the evidence that reached them.
func runTaskSweepRunGrounds(flags *tasksOptions) {
	opts := tools.WorktreeSweepOptionsFromFlags(tools.WorktreeSweepFlags{
		DryRun: flags.dryRunGate(),
		// Same gate as runTaskSweepWorktrees: this axis is the SECOND removal
		// face of one verb, so it must not be reachable by an argv the verb
		// itself classifies as a plan.
		Apply:   !flags.dryRunGate(),
		MinAgeH: flags.sweepMinAgeH,
		Max:     flags.sweepMax,
	})
	collectable, kept, err := tools.RunGroundCollectableGrounds(time.Now(), opts.MinAge)
	if err != nil {
		// No anchors at all is the ordinary state of a host that has never
		// grounded a run — reported quietly, never as a failure.
		fmt.Fprintf(os.Stderr, "sweep-worktrees: run-ground anchors unreadable (%v) — the registration plan above is unaffected\n", err)
		return
	}
	if len(collectable) == 0 && len(kept) == 0 {
		return
	}
	fmt.Println(tools.RunGroundSweepHeader(collectable, kept))
	for _, line := range tools.RunGroundSweepReport(collectable, kept) {
		fmt.Printf("  %s\n", line)
	}
	if opts.DryRun {
		return
	}
	removed, skipped, cerr := tools.RunGroundCollect(collectable)
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "sweep-worktrees: run-ground collection: %v\n", cerr)
		return
	}
	fmt.Printf("run-ground anchors applied: removed=%d skipped=%d\n", len(removed), len(skipped))
	for _, dir := range skipped {
		fmt.Printf("  kept: %s\n", dir)
	}
}

// dryRunGate reports whether the sweep should plan only. A bare run IS a dry
// run (the operator must opt in to removal with --apply); --dry-run is the
// explicit spelling of the same posture, and it wins over --apply.
//
// It is the verb's ONE decision and every destructive arm reads it: the merger
// work lock is taken only when it reports false, and the engine's Apply field
// is fed `!dryRunGate()`. The second half is load-bearing — the engine derives
// its own posture from Apply ALONE (tools.WorktreeSweepOptionsFromFlags), so a
// gate wired only to the lock left `--apply --dry-run` a silent APPLY that
// removed registrations, disagreeing with `task finish-cleanup` on the same
// argv.
func (o *tasksOptions) dryRunGate() bool { return !o.sweepApply || o.dryRun }

// sweepWorktreeRunConflicts reports flags that contradict each other on argv, so
// the operator learns on the command line instead of from an unexpected
// destructive run.
func (o *tasksOptions) sweepWorktreeRunConflicts() error {
	if o.sweepApply && o.dryRun {
		return fmt.Errorf("task sweep-worktrees: --apply and --dry-run contradict each other; --dry-run wins (pass one)")
	}
	return nil
}

func runTaskMergedCleanup() {
	// Single-instance guard: this face reaps merged branches and the worktrees
	// they occupy — the same git surgery the merger performs (epic
	// merger-always-flock).
	defer mergerWorkLock(mergedCleanupFaceProg)()
	repo := tools.GitSyncRepoRoot()
	if repo == "" {
		fmt.Fprintln(os.Stderr, "gopi task merged-cleanup: not inside a git checkout")
		os.Exit(1)
	}
	for _, line := range mergedCleanupPassOnce(repo) {
		fmt.Println(line)
	}
}

// mergedCleanupPassOnce is this face's BODY, returning its lines instead of
// printing them (Rules 13/14): the production entry point above prints them, and
// the reachability proof drives the SAME composition without standing in an
// `os.Exit` + flock face it cannot undo.
//
// The two halves, in the order they act:
//
//  1. the merged-branch cleanup report — what a merged branch's worktree became;
//  2. the ownership-INDEPENDENT registration reap — which registrations nothing
//     on this host will ever remember. See serveWorktreeReapSweep for why THIS
//     face is its carrier and why it is not on a timer.
//
// THE REAP IS NOT TIMED HERE (a verb is not a tick): an operator typed the
// command, so the answer is due now. Its own guard ladder (age floor, holder pass, declared roots, a
// removal verb with no --force) is what keeps a manual run safe, and it is
// unchanged — Rule 35: no new dependency, no new switch, no new cadence.
func mergedCleanupPassOnce(repo string) []string {
	var out []string
	rep := tools.MergedCleanupSweep(tools.MergedCleanupOptions{Repo: repo, ScanMax: mergedCleanupScanMax, ReportOnly: true})
	out = append(out, fmt.Sprintf("merged-cleanup (%s): removed=%d would-reap=%d skipped=%d failed=%d",
		repo, len(rep.Removed), len(rep.Reported), len(rep.Skipped), len(rep.Failed)))
	out = append(out, mergedCleanupReportLines(rep)...)
	// A reap failure is REPORTED on this stream and never fails the command:
	// the cleanup report above is this face's own verdict, and the reap's
	// ladder already refuses anything it cannot prove safe.
	for _, line := range serveWorktreeReapSweep(worktreeReapDryRun) {
		out = append(out, fmt.Sprintf("[%s] %s", worktreeReapLegLabel, line))
	}
	return out
}

// mergedCleanupIntervalFromEnv resolves the cleanup cadence (Rule 2 — the
// env is read in exactly one place). Same contract as syncIntervalFromEnv:
// absent/0 → default; negative → disabled; parse-fail → error.
func mergedCleanupIntervalFromEnv() (interval time.Duration, disabled bool, err error) {
	raw := strings.TrimSpace(os.Getenv(mergedCleanupIntervalEnv))
	if raw == "" {
		return mergedCleanupIntervalDefault, false, nil
	}
	n, aerr := strconv.Atoi(raw)
	if aerr != nil {
		return 0, false, fmt.Errorf("%s: %q is not an integer (seconds)", mergedCleanupIntervalEnv, raw)
	}
	if n < 0 {
		return 0, true, nil
	}
	if n == 0 {
		return mergedCleanupIntervalDefault, false, nil
	}
	return time.Duration(n) * time.Second, false, nil
}

// serveLockPath resolves the merger's singleton lock path. It exists so the
// READ-ONLY status path can name the lock WITHOUT going through
// acquireServeLock (which writes our pid into the file — a status check must
// not leave a trace, let alone look like a running daemon).
func serveLockPath() (string, error) {
	gd, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("resolve gopi dir: %w", err)
	}
	return filepath.Join(gd, serveLockFilename), nil
}

// acquireServeLock takes the exclusive non-blocking flock on
// <gopi data root>/serve.lock, stamps the holder's pid into the file and
// returns the open file (closing it releases the lock). An already-locked
// file means another serve is running: the error names the pid recorded by
// the holder. The lock lives under config.GopiDir() (GOPI_HOME aware —
// every gopi-data path resolves there), so a sandboxed serve (--home /
// GOPI_HOME) never contends with the real profile's daemon.
func acquireServeLock() (*os.File, error) {
	// THE ADMISSION RULE FOR A LOCK HOLDER'S OWN FACES (epic merger-takeover-lock):
	// a process that is a designated descendant of the current holder — carrying
	// the launcher's hand-off marker AND having a live ancestor that holds the
	// flock, both required (mergerLockHeldByAncestor) — is admitted WITHOUT taking
	// anything. It inherits no lock descriptor (the holder fences its children on
	// purpose), so without this rule it would be refused the lock its own ancestor
	// holds while that ancestor WAITS on it: a deadlock, not a safety property.
	// The retired merger's take-over ran a `gopi merger --once` tick (whose
	// close loop carries the rebuild contract); merge-execute is this shape.
	//
	// IT IS CHECKED HERE, at the ONE acquisition funnel, rather than at each face:
	// every face that must hold the lock before writing refs comes through this
	// function, so putting the rule anywhere else would leave faces that bypass it
	// (Rule 2/13 — one funnel, one rule).
	if mergerLockHeldByAncestor() {
		// The handle exists for the diagnostics and identity reads the callers
		// below perform; it holds NO lock and releases NOTHING (see
		// inheritedLockHandleForExemptFace). A failure to open it is not a reason
		// to refuse a face the live holder is waiting on — the ancestor's flock is
		// the actual guard — so it is REPORTED and the face proceeds.
		handle, herr := inheritedLockHandleForExemptFace()
		if herr != nil {
			fmt.Fprintf(os.Stderr, "%s: admitted under an ancestor's single-instance lock, but %v — proceeding (the guard is the ancestor's, not this descriptor's)\n", mergerProgTopLevel, herr)
		}
		return handle, nil
	}
	path, err := serveLockPathResolved()
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < serveLockIdentityRetries; attempt++ {
		f, err := lockAndVerifyIdentity(path)
		if err == nil {
			// Winning the flock proves only that THIS path's inode is unheld
			// Winning the flock proves only that THIS path's inode is unheld
			// — an unlink underneath a running daemon made the path point at
			// a fresh, free inode while that daemon kept merging (2026-09-12).
			// Refuse while such a stray holder exists: the next daemon to
			// accept the free file double-merges against a daemon whose guard
			// is detached.
			if stray := tools.OrphanLockHolder(path); stray > 0 {
				refusal := serveLockStrayHolderRefusalError(f, stray, path)
				f.Close() //stylegate:ignore close of the freshly won flock we refuse to keep; releasing it before refusing is the point
				return nil, refusal
			}
			return f, nil
		}
		if !errors.Is(err, errServeLockReplaced) {
			// THE SELF-HOLD ADMISSION RULE (serve_lock_self_hold.go): a contention
			// whose ONLY kernel-proved holder is THIS process is admitted,
			// announced — never refused. flock lives on the open file description,
			// so a face that asks for the single-instance lock while an outer step
			// of the SAME process already holds it is refused by its own guard;
			// that refusal protects nothing (the writer the lock excludes is the
			// same writer) and it made `gopi task finish-cleanup --apply` refuse
			// itself, naming its own pid as "another merge process". Checked HERE,
			// at the ONE funnel, for the same reason the ancestor rule is
			// (Rules 2/13). A third party, a stray holder, or an unprovable scan
			// leaves this false and the refusal below stands unchanged.
			if errors.Is(err, errServeLockHeld) {
				if selfHeld, _ := serveLockSelfHeld(); selfHeld {
					return serveLockAdmitSelfHeld(mergerTakeoverArgv0), nil
				}
			}
			return nil, err
		}
	}
	return nil, fmt.Errorf(serveLockIdentityRefusal, path)
}

// serveLockStrayHolderRefusalError builds the STRAY-HOLDER refusal. It is the
// SIBLING of serveLockContentionError and exists for the same reason: the
// sentence alone says "a pid holds an unlinked copy", which is the one fact the
// operator already could not act on (task 01a136c7, 2026-10-16: the holder was
// alive, `flock -n` reported the path FREE, and `rm -f serve.lock` did not clear
// the refusal — the sentence sent the reader to the wrong remedy).
//
// The diagnosis appended is the SAME three lines the contention arm carries
// (lock-holder / lock-stray-holder / lock-kernel-records), rendered by the same
// production renderer (serveLockRefusalDiagnosis — Rule 2, one renderer per
// question), so the two refusals of the single-instance lock can no longer
// disagree about what the operator gets to see. The descriptor is the one the
// acquirer just won the flock on and is about to close: it is still open here,
// which is what makes its inode the correct join key for the kernel's table.
func serveLockStrayHolderRefusalError(f *os.File, stray int, path string) error {
	return fmt.Errorf("%w\n%s", fmt.Errorf(serveLockStrayHolderRefusal, stray, path, stray),
		serveLockRefusalDiagnosis(f, path))
}

// serveLockPathResolved resolves <gopi data root>/serve.lock and makes sure its
// directory exists (the creation half shared by every acquisition path).
func serveLockPathResolved() (string, error) {
	gd, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("resolve gopi dir: %w", err)
	}
	path := filepath.Join(gd, serveLockFilename)
	if err := os.MkdirAll(filepath.Dir(path), serveLockDirMode); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	return path, nil
}

// errServeLockReplaced marks "won the flock on an inode that is no longer the
// file the lock path names" — the retryable half of the identity check (the
// caller re-opens and re-locks the fresh file).
var errServeLockReplaced = errors.New("serve lock file replaced while acquiring")

// errServeLockHeld marks "a live holder still owns the single-instance lock" —
// the ONE refusal a bounded wait can legitimately resolve (the holder is a
// daemon on its way out). Every other acquisition failure means the lock cannot
// be used at all, and waiting for it would only delay the diagnosis.
var errServeLockHeld = errors.New("serve lock held by a live daemon")

// serveLockRefusalDiagnosis renders the evidence lines appended to a flock
// CONTENTION refusal: the stray holder (with its LIVENESS) and the kernel's
// own lock-table records for the inode the acquirer was ACTUALLY refused on.
//
// It takes the ACQUIRED DESCRIPTOR, not the path, for the inode — the whole
// reason the earlier diagnostic was worthless: a `stat` of the PATH names
// whatever file the name resolves to NOW, while the refusal happened against
// the descriptor's inode. After a replacement those are two different files,
// and joining the kernel table on the path's inode reports "no record" for a
// lock that is very much held (2026-09-17: a refusal said "held by pid
// 1510545" while /proc/locks carried no record for the lock file's inode and
// that pid was dead).
//
// The descriptor is still open here (the flock failed, the file is ours to
// stat), so fstat is the ONE correct join key. Both halves report
// "unprovable" explicitly rather than degrading to an empty answer (Rule 15).
func serveLockRefusalDiagnosis(f *os.File, path string) string {
	var b strings.Builder
	// THREE lines, in the order an operator reads them: who the stamp names and
	// whether that pid is ALIVE (the branch's half — a bare pid sent operators
	// hunting a process that was already gone), who holds an UNLINKED copy of
	// the path (master's half), and what the kernel's own table says about the
	// inode the acquirer was refused on.
	b.WriteString(serveLockHolderDiagnosis(f))
	b.WriteString(serveLockStrayHolderDiagnosis(path))
	b.WriteString(serveLockKernelRecordDiagnosis(f))
	return b.String()
}

// serveLockHolderDiagnosis renders the `lock-holder:` line: the pid stamped
// into the lock file, paired with that pid's liveness (the rework union's
// half). It renders through serveLockHolder — the SAME function that builds the
// refusal sentence's holder — so the sentence and this line can never disagree
// about whether the stamp names a live process, and the dead-note is spelled
// once, inside serveLockHolder.
func serveLockHolderDiagnosis(lock *os.File) string {
	return serveLockDiagnosisLine(serveLockHolderPrefix, serveLockHolder(lock))
}

// serveLockDiagnosisLine composes one labelled diagnostic line (label +
// payload + newline), so the label constants and the rendering cannot drift
// (Rule 1b).
func serveLockDiagnosisLine(prefix, payload string) string {
	return prefix + payload + serveLockLineEnd
}

// serveLockStrayHolderDiagnosis renders the `lock-stray-holder:` line: who, if
// anyone, holds an UNLINKED copy of the lock path (tools.OrphanLockHolder),
// paired with its liveness (tools.PidAlive). A pid without that pairing is
// exactly the artifact this epic removes — an inherited descriptor keeps a
// flock alive and still names a DEAD owner.
func serveLockStrayHolderDiagnosis(path string) string {
	if stray := tools.OrphanLockHolder(path); stray > 0 {
		return serveLockDiagnosisLine(serveLockStrayHolderPrefix, fmt.Sprintf(serveLockStrayHolderFormat, stray, path, serveLockLivenessNote(stray)))
	}
	return serveLockDiagnosisLine(serveLockStrayHolderPrefix, fmt.Sprintf(serveLockNoStrayHolder, path))
}

// serveLockLivenessNote renders the liveness half of a holder report.
func serveLockLivenessNote(pid int) string {
	if tools.PidAlive(pid) {
		return serveLockHolderLive
	}
	return serveLockHolderDead
}

// serveLockKernelRecordDiagnosis renders the `lock-kernel-records:` line from
// the ACQUIRED DESCRIPTOR's own inode (fstat), via the tools package's
// /proc/locks reader (tools.KernelFlockRecords — the same parser the status
// and stop surfaces read the table with, so a second parse cannot drift).
//
// Two unprovable cases are ANNOUNCED, never rendered as "no records"
// (Rule 15): an unreadable /proc/locks (no procfs, a hardened /proc) and a
// descriptor whose inode cannot be resolved.
//
// The payload carries master's record list PLUS the rework union's two further
// facts, on the same inode the record list was joined on (so the two halves of
// the diagnosis can never name different files): whether the table carried a
// record AT ALL (a refusal with none is a REPLACED lock path, not a live
// holder), and which live pids hold a DESCRIPTOR on that inode — the only
// authoritative statement about who keeps a flock alive, since a flock is
// released when the LAST descriptor copy closes, whichever process holds it.
func serveLockKernelRecordDiagnosis(f *os.File) string {
	fi, err := f.Stat()
	if err != nil {
		return serveLockDiagnosisLine(serveLockKernelRecordsPrefix, fmt.Sprintf(serveLockKernelNoInodeFormat, err))
	}
	ino := tools.InodeOfFileInfo(fi)
	if ino == 0 {
		return serveLockDiagnosisLine(serveLockKernelRecordsPrefix, fmt.Sprintf(serveLockKernelNoInodeFormat, "no inode on this platform"))
	}
	records, err := tools.KernelFlockRecords(ino)
	if err != nil {
		return serveLockDiagnosisLine(serveLockKernelRecordsPrefix, fmt.Sprintf(serveLockKernelUnreadableFormat, err))
	}
	rendered := serveLockKernelNoRecords
	if len(records) > 0 {
		rendered = strings.Join(records, serveLockKernelEvidenceJoin)
	} else {
		// (b) the table is READABLE and carries no record for the refused
		// descriptor's inode — the refusal is against an inode this path no
		// longer names (a replaced lock file), never a live holder. Saying so
		// is the difference between "the holder is a corpse" and "you are
		// looking at the wrong inode" (rework union, 2026-09-23).
		rendered = fmt.Sprintf(serveLockKernelNoMatch, strconv.FormatUint(ino, 10))
	}
	holders, complete := serveLockFdHolders(strconv.FormatUint(ino, 10))
	// (c) the fd-holder scan, appended so ONE line answers "which processes
	// keep this lock alive?" — a scan that could not judge every process says
	// so rather than claiming "none" (Rule 15). The LABEL changes with the
	// answer (epic 01a14845): a complete scan names the live holders, an
	// incomplete one names only what it PROVED, so "unknown" can never be read
	// as "these are the holders".
	rendered += serveLockKernelEvidenceJoin + serveLockFdHoldersLine(holders, complete)
	return serveLockDiagnosisLine(serveLockKernelRecordsPrefix, fmt.Sprintf(serveLockKernelRecordsFormat, len(records), ino, rendered))
}

// lockAndVerifyIdentity opens+flocks the lock path and then proves the
// descriptor it locked is STILL what the name resolves to, by comparing
// device+inode of the descriptor and of a fresh stat of the path. A mismatch
// means a concurrent unlink/replace happened underneath us — the flock we
// hold is unreachable by name (worthless as a guard) — and the caller must
// retry on the fresh file. The stamp (our pid) is only written once identity
// is confirmed, so no refusal path leaves our pid in a file we do not own.
// serveLockErrnoIsContention reports whether a `syscall.Flock` failure means
// "someone else holds this lock" — the ONLY reading that licenses the
// live-daemon refusal. EWOULDBLOCK is the single errno a non-blocking flock
// returns for a contention; every other errno (EBADF, EINVAL, ENOLCK, a full or
// read-only filesystem) is a real failure about the lock itself, and reporting
// it as a holder invents a process the operator cannot find (Rule 15).
//
// It is a named predicate rather than an inline comparison so the mapping can be
// red-proven by its own test (TestServeLockRefusalNamesTheActualErrnor): the
// defect was precisely that ALL errnos took the "held" branch.
func serveLockErrnoIsContention(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK)
}

// serveLockHeldError is the refusal for a flock CONTENTION — the only errno
// erServeLockHeld is allowed to describe. It is a named constructor (not an
// inline fmt.Errorf) so the message and the wrapped sentinel are ONE definition:
// a caller that legitimately WAITS for the holder (the restart handover) tells
// "someone is running" from "this lock is unusable" with errors.Is, never by
// matching prose.
func serveLockHeldError(path, holder string) error {
	return fmt.Errorf("%w: %w", errServeLockHeld, fmt.Errorf(serveLockHeldRefusal, path, holder))
}

// serveLockRefusalForErrno maps a `syscall.Flock` failure to the refusal the
// caller must see. "Held by another daemon" is the meaning of ONE errno
// (EWOULDBLOCK), so only a CONTENTION takes the held-refusal arm; every other
// errno is a real syscall failure about the lock itself and is reported as one
// (Rule 15: a failure must say what happened, never blame a phantom process).
//
// It is a named function rather than an inline if/else because the CONTENTION
// arm READS THE REFUSED DESCRIPTOR — its inode and the kernel's record for it —
// so it must run while that descriptor is still open. Putting the errno mapping
// in one place is what lets the caller close the descriptor AFTER building the
// refusal and still keep the two arms in the right order (measured 2026-09-17:
// closing first made every contention refusal read "cannot stat the refused
// descriptor — its inode is unknown", i.e. the evidence was dead at the one
// moment it exists for).
func serveLockRefusalForErrno(path string, lock *os.File, err error) error {
	if !serveLockErrnoIsContention(err) {
		return fmt.Errorf("flock %s (fd %d): %w", path, lock.Fd(), err)
	}
	return serveLockContentionError(path, lock)
}

func lockAndVerifyIdentity(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, serveLockFileMode)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		// The CONTENTION arm reports the kernel's record for the inode of THIS
		// descriptor, so it must be built while the descriptor is still OPEN
		// (measured 2026-09-17: closing first made every refusal read "cannot
		// stat the refused descriptor — its inode is unknown", i.e. the
		// evidence was dead on arrival at the one moment it exists for).
		// Therefore the errno mapping decides FIRST (see
		// serveLockRefusalForErrno) and the close follows it.
		refusal := serveLockRefusalForErrno(path, f, err)
		f.Close() //stylegate:ignore close of a file already written and flushed by the checked calls
		return nil, refusal
	}
	// Winning the flock proves only that THIS path's inode is unheld — an
	// unlink underneath a running daemon made the path point at a fresh,
	// free inode while that daemon kept merging (2026-09-12). Refuse while
	// such a stray holder exists: the next daemon to accept the free file
	// double-merges against a daemon whose guard is detached.
	if stray := tools.OrphanLockHolder(path); stray > 0 && stray != os.Getpid() {
		refusal := serveLockStrayHolderRefusalError(f, stray, path)
		f.Close() //stylegate:ignore close of the freshly won flock we refuse to keep; releasing it is the point of the refusal
		return nil, refusal
	}
	held, err := sameFileAsPath(f, path)
	if err != nil {
		f.Close() //stylegate:ignore close of the lock we cannot verify; refusing is the safe half of the check
		return nil, err
	}
	if !held {
		f.Close() //stylegate:ignore close of the flock bound to a replaced inode — releasing it is correct, the lock is unreachable by name
		return nil, errServeLockReplaced
	}
	if err := f.Truncate(0); err == nil {
		if _, werr := f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0); werr == nil {
			_ = f.Sync() //stylegate:ignore durability flush of the serve-lock pid; the flock itself is what guards
		}
	}
	return f, nil
}

// ensureServeLockIdentity re-proves that the flock the daemon holds is still
// the file the lock path NAMES, and repairs the guard when an unlink
// underneath the daemon detached it (2026-09-12 incident).
//
// Why this must run per tick and not only at startup: `flock` lives on the
// INODE. An `rm serve.lock` (or any unlink+recreate) while a merger runs
// leaves that merger locking an inode no path points at, while the path gets
// a FRESH inode whose lock is free — so the next daemon's acquireServeLock
// SUCCEEDS and two mergers sweep the same branches. A one-shot check at
// acquisition cannot see an unlink that happens later; only a re-check on the
// working loop turns "silently unguarded" into "restored or stopped".
//
// On a mismatch the daemon:
//  1. reports the stray holder (pid) so the operator has the evidence,
//  2. drops the detached lock (its inode is unreachable — worthless),
//  3. takes a FRESH lock on the current path, making the name↔inode identity
//     true again, and
//  4. EXITS when another process already took the fresh file: at that instant
//     a second daemon is sweeping, and the only safe move is to stop rather
//     than double-merge.
func ensureServeLockIdentity(lock *os.File, prog string) *os.File {
	path, err := serveLockPathResolved()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot verify the single-instance lock (%v) — continuing under the held lock\n", prog, err)
		return lock
	}
	ok, err := sameFileAsPath(lock, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: cannot verify the single-instance lock (%v) — continuing under the held lock\n", prog, err)
		return lock
	}
	if ok {
		return lock
	}
	// The refusal/repair text is FACE-NEUTRAL (`this merger`, not `the daemon`):
	// the flock is held by whichever merger form holds it — a cron tick or the
	// resident `--every` loop — and calling that holder a daemon would name a
	// face the merger no longer has (epic merger-self-driving, 2026-09-18).
	fmt.Fprintf(os.Stderr, "%s: single-instance lock was replaced underneath this merger — the guard is DOWN; stray holder %s; re-acquiring %s\n",
		prog, strayHolderNote(tools.OrphanLockHolder(path)), path)
	lock.Close() //stylegate:ignore the detached flock's inode is unreachable by name; closing it is the correct repair step before re-acquiring
	reacquired, err := lockAndVerifyIdentity(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v — stopping instead of merging unguarded\n", prog, err)
		os.Exit(1)
	}
	fmt.Printf("[merger] guard restored: %s is again LOCKED by this merger (pid %d)\n", path, os.Getpid())
	return reacquired
}

// strayHolderNote renders the orphan-holder pid for the daemon's repair
// message ("" when no pid could be attributed).
func strayHolderNote(pid int) string {
	if pid <= 0 {
		return "unattributable"
	}
	return serveLockPIDPrefix + strconv.Itoa(pid)
}

// sameFileAsPath reports whether the open descriptor is the very file the
// path names right now (same device + inode). This is the check that catches
// the unlink-underneath-a-held-flock hazard: after an unlink+recreate the
// path resolves to a NEW inode while our descriptor still holds the OLD one.
func sameFileAsPath(f *os.File, path string) (bool, error) {
	fdInfo, err := f.Stat()
	if err != nil {
		return false, fmt.Errorf("stat lock fd: %w", err)
	}
	pathInfo, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// The name is GONE while we hold a descriptor: the unlink-underneath
			// state — identity is definitively broken (not an I/O failure we
			// cannot judge), so the caller repairs instead of continuing.
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	fdSys, ok := fdInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return true, nil // non-Linux/unsupported: no identity to compare, do not invent a refusal
	}
	pathSys, ok := pathInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return true, nil
	}
	return fdSys.Dev == pathSys.Dev && fdSys.Ino == pathSys.Ino, nil
}

// serveLockHolder renders the holder the flock REFUSAL names, from the
// DESCRIPTOR it opened and flocked, never a re-resolution of the path (an
// unlink underneath would make the name a different file). It reports the pid
// STAMPED INTO the lock file, PAIRED WITH ITS LIVENESS.
//
// Why the pairing is not optional (measured twice — c50e7f2a's E2E refusal read
// "held by pid 1630650" while that pid was gone; the operator's session
// reported the same of pid 1653050): a flock lives on the OPEN FILE DESCRIPTION
// and is not invalidated by unlink/replace, so the lock stays held by a
// DESCRIPTOR COPY after the process that STAMPED the file has died (os/exec
// ExtraFiles, a fork). The bare stamp then names a corpse, and the operator
// hunts a process that no longer exists.
func serveLockHolder(lock *os.File) string {
	pid := tools.ReadStampedPID(lock.Name())
	if pid <= 0 {
		return serveLockHolderUnknown
	}
	if tools.PidAlive(pid) {
		// The ALIVE half is today's wording and stays ONE definition: the alive
		// suffix comes from the shared lock-holder note (empty today, so the
		// refusal reads exactly as before for a live holder).
		return serveLockPIDPrefix + strconv.Itoa(pid) + tools.LockHolderNote(pid, true)
	}
	// The DEAD half must NOT use LockHolderNote(false): that text asserts "the
	// lock is FREE", which a CONTENTION refusal contradicts — the kernel just
	// refused our flock, so a live descriptor copy is holding it. The stamp is
	// dead; the holder is not necessarily gone.
	return serveLockPIDPrefix + strconv.Itoa(pid) + serveLockHolderDeadNote
}

// serveLockTableRecords returns every FLOCK record in the kernel's table for
// the file the refused descriptor names, spelled as the table spells it — the
// LAST column of the record, "<maj>:<min>:<ino>" (serveLockDeviceField). It
// returns (nil, false) when the table cannot be read at ALL — "I cannot say",
// never "nobody holds it" (Rule 15: the two are different answers and only one
// of them may be reported as one).
func serveLockTableRecords(lock *os.File) ([]string, bool) {
	fi, err := lock.Stat()
	if err != nil {
		return nil, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, false
	}
	want := serveLockDeviceField(st)
	b, err := os.ReadFile(serveLockProcLocksPath)
	if err != nil {
		return nil, false
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		cols := strings.Fields(line)
		if len(cols) <= serveLockFlockDeviceInodeColumn || len(cols) < 2 || cols[1] != serveLockFlockTypeColumn {
			continue
		}
		// Match the FULL device/inode field, not the inode alone: two
		// filesystems can carry the same inode number, and a record on another
		// filesystem's coincidentally-equal inode would be reported as the
		// holder of THIS lock.
		if cols[serveLockFlockDeviceInodeColumn] != want {
			continue
		}
		out = append(out, strings.TrimSpace(line))
	}
	return out, true
}

// serveLockDeviceField spells a descriptor's file as the kernel's lock table
// spells it: "<maj>:<min>:<ino>". The major/minor pair is decoded from the
// st_dev the stat call returned (Linux kdev_t encoding) — the same numbers the
// table prints, so the comparison is a string equality on the kernel's own
// spelling rather than a re-derived one.
func serveLockDeviceField(st *syscall.Stat_t) string {
	dev := st.Dev
	major := (dev & serveLockDevMajorMask) >> serveLockDevMajorShift
	minor := (dev & serveLockDevMinorMask) | ((dev >> serveLockDevMinorShiftHi) & serveLockDevMinorMaskHi)
	return fmt.Sprintf(serveLockDeviceFieldFormat, major, minor, st.Ino)
}

// serveLockFdHolders lists the live pids whose descriptor table has an OPEN
// file on ino, and whether that scan could be answered for every process.
//
// It is the fd-holder half of the evidence (mlPidsHoldingFdOn's shape): a flock
// lives on the open file description and is released when the LAST fd copy
// closes, whichever process holds it — so this list, not the pid stamp and not
// the lock table's (possible dead) owner column, is who keeps the lock alive.
// A pid also UNAMBIGUOUSLY present in /proc is proof of a live process: no
// separate liveness probe is needed.
//
// An unreadable fd table (another user's process, a hardened /proc) is NOT
// proof of absence: the second return value goes false, and the rendering says
// the scan was incomplete (Rule 15 — never report "I cannot say" as "none").
func serveLockFdHolders(ino string) ([]int, bool) {
	return serveLockFdHoldersAt(serveLockProcRoot, ino, tools.PidAlive)
}

// serveLockFdHoldersAt is the ONE scan, parameterised by procfs root and the
// liveness predicate so its two judging rules are testable without a live /proc
// (Rules 2/13: production and test drive the same code, never a copy):
//
//   - root: the procfs mount to read (the real one in production);
//   - alive: the liveness predicate a pid must pass to be CALLED a holder
//     (tools.PidAlive in production — signal-0 plus "a zombie counts as gone").
//
// Both parameters exist because the pre-fix behaviour was wrong in exactly the
// two ways these two inputs decide (epic 01a14845): a pid with no /proc entry was
// printed as a live holder, and an unjudgeable process was folded into a list
// that still asserted holders.
func serveLockFdHoldersAt(root, ino string, alive func(int) bool) ([]int, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, false
	}
	var out []int
	complete := true
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		holds, err := serveLockPidHoldsDescriptorOnAt(root, pid, ino)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				complete = false // EACCES etc.: this process could not be judged
			}
			continue
		}
		// LIVENESS IS VERIFIED PER PID, not inferred from having read its fd
		// table (epic 01a14845, measured): the report used to name pids with NO
		// /proc entry at all as "live pids holding an fd on that inode" (pid
		// 3496987 was printed live while /proc/3496987 was GONE). A pid the
		// kernel has recycled or reaped between the readdir and here is not
		// evidence of a holder, and printing it sends the operator hunting a
		// process that does not exist. tools.PidAlive is the ONE liveness
		// predicate (signal-0 + a zombie counts as gone), so the fd list and
		// every other liveness verdict in the CLI cannot disagree.
		if holds && alive(pid) {
			out = append(out, pid)
		}
	}
	sort.Ints(out)
	return out, complete
}

// serveLockPidHoldsDescriptorOn reports whether one process has an open
// descriptor on ino. Stat'ing /proc/<pid>/fd/<n> follows the link to the open
// FILE (the kernel resolves it even for an unlinked target, which is how the
// orphan case is seen), so the inode compare needs no path string.
func serveLockPidHoldsDescriptorOn(pid int, ino string) (bool, error) {
	return serveLockPidHoldsDescriptorOnAt(serveLockProcRoot, pid, ino)
}

// serveLockPidHoldsDescriptorOnAt is that scan against an explicit procfs root
// (see serveLockFdHoldersAt for why the root is a parameter).
func serveLockPidHoldsDescriptorOnAt(root string, pid int, ino string) (bool, error) {
	fdDir := filepath.Join(root, strconv.Itoa(pid), serveLockProcFdDirName)
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return false, err
	}
	for _, fd := range fds {
		info, err := os.Stat(filepath.Join(fdDir, fd.Name()))
		if err != nil {
			continue // a descriptor that vanished mid-scan: not evidence either way
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		if strconv.FormatUint(st.Ino, 10) == ino {
			return true, nil
		}
	}
	return false, nil
}

// serveLockFdHoldersLine renders the whole fd-holder field — LABEL and answer —
// in one place, so the three answers of the scan (holders, none, unknown) can
// never be paired with the wrong label (epic 01a14845: an incomplete scan
// printed under a label that asserted the pids were live holders).
func serveLockFdHoldersLine(pids []int, complete bool) string {
	note := serveLockKernelFdHoldersNote
	if !complete {
		note = serveLockKernelFdHoldersNoteIncomplete
	}
	return fmt.Sprintf(note, serveLockPidList(pids, complete))
}

// serveLockPidList renders the fd-holder half of the evidence. An incomplete
// scan with no holders found says so instead of claiming "none"; an incomplete
// scan WITH holders reports them and flags the gap.
func serveLockPidList(pids []int, complete bool) string {
	rendered := serveLockKernelHoldersNone
	if len(pids) > 0 {
		parts := make([]string, 0, len(pids))
		for _, p := range pids {
			parts = append(parts, strconv.Itoa(p))
		}
		rendered = strings.Join(parts, serveLockPidListJoin)
	}
	if !complete {
		if len(pids) == 0 {
			return serveLockKernelHoldersUnknown
		}
		return rendered + serveLockKernelHoldersIncompleteSuffix
	}
	return rendered
}

// serveLockContentionError builds the CONTENTION refusal: the sentinel-wrapped
// refusal SENTENCE (unchanged, so the restart handover still matches on
// errors.Is, never on prose) followed by the full diagnosis, of which it names
// only the first line itself. The diagnosis is appended rather than summarised
// so the sentence an existing caller pins stays byte-identical while the
// evidence behind it is available to the operator reading the refusal.
func serveLockContentionError(path string, lock *os.File) error {
	holder := serveLockHolder(lock)
	// THIS PROCESS IS NOT "ANOTHER" HOLDER. When the flock is kept alive by a
	// THIRD PARTY but the stamp carries OUR pid (a duplicated descriptor, a
	// replaced stamp), the sentence must name the holders that are actually
	// keeping it — saying "another merge process … held by pid <self>" sends the
	// operator hunting a process that is the command they just typed.
	if stamped := tools.ReadStampedPID(path); stamped == os.Getpid() {
		if _, thirdParty := serveLockSelfHeld(); len(thirdParty) > 0 {
			holder = serveLockThirdPartyHolderText(thirdParty)
		}
	}
	return fmt.Errorf("%w: %w\n%s", errServeLockHeld,
		fmt.Errorf(serveLockHeldRefusal, path, holder),
		serveLockRefusalDiagnosis(lock, path))
}

// taskRemovedAdvice returns the replacement path for a removed task
// sub-verb (new/next): the pointed message the CLI prints on refusal.
func taskRemovedAdvice(verb string) string {
	if syncVerbRemovalSentence(verb) != "" {
		// THE CROSS-NODE FAMILY (2026-11-05). Every spelling in it answers with
		// ONE sentence, and the sentence is chosen by WHICH HALF the reader was
		// using: the row legs (`send`/`receive`/`pull`) moved a pending row
		// between live ledgers and are simply gone — a row is now handed over by
		// putting it on the shared git ref, which needs no verb; the peer-session
		// legs (`sync`/`harvest`/`push`/`diff`) read or wrote a peer's ledger over
		// ssh. `sync-pass` was the manual one-shot of the same machinery.
		return syncVerbRemovalSentence(verb)
	}
	if verb == taskverbs.Run {
		// epic worker-run-entry (2026-09-16): running a row is `gopi worker`'s
		// single job. The replacement names BOTH faces (explicit row, drain)
		// because the retired verb could do both (`task run <id>` and
		// `task run next`), and the reader must be able to see which one they
		// meant.
		return `running a task is the worker's job now — "gopi worker <task-id>" runs ONE row in the foreground (exit 0 completed / 1 failed), "gopi worker" or "gopi worker --once" drains the queue; "gopi task" manages the ledger only`
	}
	if verb == taskverbs.SweepZombies {
		// zombie-sweeper retirement (2026-10-26). The replacement names what the
		// operator can actually do about a stuck row, and says plainly that
		// gopi will no longer move it: a row left in_progress is the evidence
		// that a close-out never ran, and sweeping it away hid that defect.
		return `gopi no longer disposes or rewrites a row on its own; a row stuck at in_progress/claimed is READ as stale wherever it is displayed ("gopi task list", "gopi task show <id>" — the holder's identity is resolved per row) and stays exactly where it is until a human closes it out ("gopi task update <id> --status completed" / "--status failed") or a new run of the row finishes it (devdoc/zombie-sweep-removed.md)`
	}
	if verb == taskverbs.ForBranch {
		// Retired 2026-10-30 (task 01a18799). The capability did NOT leave: the
		// row is addressed by its deliverable through `gopi task show --branch`,
		// which resolves through the SAME seam (tools.TaskForBranch) this verb
		// called, and prints WHO owns the branch plus the row. The plural
		// "every owner" answer of the old verb was reconstructed from rows that
		// no longer exist in the current model (a branch is the row id itself),
		// so the surviving face answers the one question an operator still asks:
		// which row does this branch belong to?
		return `the branch→row lookup is a flag on the read now — "gopi task show --branch <branch>" (or --worktree <path>) names the row that records that deliverable, through the SAME resolver this verb used; the branch name IS a dispatched row's ledger address (task/<id>), so "gopi task show <id>" is the direct spelling`
	}
	if verb == taskverbs.BranchRefs {
		// Retired 2026-10-30 (task 01a18799). It made a claim about a PEER's ref
		// advertisement for every terminal row's deliverable branch — a delivery
		// channel this repository no longer runs, so the sentence it printed
		// described a workflow the reader could not act on. The refs themselves
		// are the machinery's business: `gopi merge` (the sweep + the guarded
		// merge), `gopi task sweep-worktrees --apply` (the ownership-independent
		// worktree reap) and git's own `git ls-remote` / `git for-each-ref`
		// answer "does this ref exist, and whose is it".
		//
		// `c5548b88` repaired the SAME circle in `retry-delivery` and
		// `reconcile`, and missed this third one: the sentence named
		// `gopi task finish-cleanup` as where a terminal row's delivery is
		// handled, and that spelling was retired in the SAME epic (`58b5618b`,
		// slice 3/4) — so a reader who typed it got a second removal message
		// pointing back at a command that no longer exists. Measured, on the
		// binary built at `c5548b88`:
		//
		//	$ gopi task branch-refs --peer peerbox ; echo $?
		//	… "gopi task finish-cleanup" (what a terminal row's delivery should do) …
		//	1
		//	$ gopi task finish-cleanup ; echo $?
		//	… the terminal-row close-out chain was retired …
		//	1
		//
		// The SURVIVING leg is the one the document already names
		// (devdoc/gopi-task-help-surface-inventory.md, the `branch-refs` row):
		// `gopi task sweep-worktrees --apply`. Code and document agree again.
		return `the peer-ref audit was retired with the worktree+branch delivery semantics it audited — the ref facts are readable from their owners: "git ls-remote --heads <peer>" (what a peer advertises), "git for-each-ref refs/heads" (what this host holds), "gopi task sweep-worktrees --apply" (the ownership-independent worktree reap), "gopi task merge" (the merge sweep and its ref consumed)`
	}
	if verb == taskverbs.Orphans {
		// Retired 2026-10-30 (task 01a18799). Its four measurements were built to
		// tell "landed, ref consumed" from "ref deleted after a delivery that
		// never happened" — a question about a delivery channel this repository
		// no longer runs, and the readout answered it by consulting that channel.
		// What an operator can still act on is stated instead: the row's own
		// record (which names the branch and the repo it records) and git's own
		// answer for that ref.
		return `the orphaned-deliverable readout was retired with the ref-based delivery it audited — a terminal row's record still names its branch and repo ("gopi task show <id>" prints deliverable.branch / deliverable.repo), and the ref itself is answered by git ("git -C <repo> for-each-ref refs/heads/<branch>", "git -C <repo> log --all --grep <id>")`
	}
	if verb == taskverbs.Verify {
		// Retired 2026-10-30 (task 01a18799, slice 2/4). The capability did NOT
		// leave as a duty — it left as a SECOND SPAWN. A program may not
		// manufacture an authority over itself (operator ruling 2026-10-25):
		// verification belongs to a TEST PROGRAM, so the verdict is written by
		// the run that measured it, through `gopi task update --status
		// verified`, which still refuses an empty evidence field
		// (tools.TaskVerifyEvidenceArgs).
		return `the separate verification pass was retired — verification belongs to a TEST PROGRAM, not to a second gopi spawn ("一个程序不能自己检验自己", operator ruling 2026-10-25): the run that measured it records the verdict with "gopi task update <id> --status verified --result \"evidence: <the test command> + its output tail\"" (a "reason: …" pass-through is the other accepted spelling); a red measurement is "gopi task update <id> --status failed --result \"…\"" and a reissue is "gopi task submit <the same work>" (or "gopi task clone <id> --reason <why>")`
	}
	if verb == taskverbs.FinishCleanup {
		// Retired 2026-10-30 (task 01a18799, slice 3/4). WHAT WAS WRONG: this
		// function had NO case for it, so the refusal fell through to the
		// closure below — the sentence written for the manual-CLAIM spelling
		// (`next`) — and an operator who typed `gopi task finish-cleanup` to
		// close out ONE row was told that "manual claiming was removed" and to
		// use `gopi worker`. That is an answer to a question nobody asked, on
		// the one surface whose whole job is to name where the capability went
		// (Rule 15: a removal must explain itself — an unrelated sentence is
		// the silent removal wearing a message).
		//
		// THE TRUTH: the close-out CHAIN is gone with the worktree+branch
		// delivery semantics it served; each duty it bundled has a command that
		// names it, and a clone-channel run publishes at its own close-out.
		return `the terminal-row close-out chain was retired with the worktree+branch delivery semantics it served — a run releases its own ground as it ends ("own worktree released" on the release line), so there is nothing for an operator to sweep afterwards; what the pass also bundled is reachable by its own command: "gopi task sweep-worktrees --apply" (the ownership-independent worktree reap), "gopi task list --status failed" (the failed-needs-reissue census), and a row is closed out by "gopi task update <id> --status completed|failed"`
	}
	if verb == taskverbs.RetryDelivery {
		// Retired 2026-10-30 (task 01a18799, slice 2/4). It was the repair leg of
		// the worktree+branch hand-off: ask a PEER whether it holds the BRANCH,
		// and re-run the close-out chain when it does not. The clone channel
		// publishes at the run's own close-out, so there is no blocked branch
		// waiting for a second visit — what remains is the terminal row's own
		// close-out, which has a verb.
		return `the blocked-delivery repair was retired with the peer/branch hand-off it repaired — a clone-channel run publishes at its own close-out and owes no separate retry; a row's delivery facts are readable from their owners ("git ls-remote --heads <peer>" for what a peer advertises, "git for-each-ref refs/heads" for what this host holds) and the row itself is closed by "gopi task update <id> --status completed|failed"`
	}
	if verb == taskverbs.Reconcile {
		// Retired 2026-10-30 (task 01a18799, slice 2/4). It was a VERB WHOSE BODY
		// WAS "run these other legs". Every leg has a command that names it, so
		// the pass has no content of its own left.
		return `the whole-ledger pass was retired — each of its legs has a command that names it: "gopi task list" (the stale read-out), "gopi task sweep-worktrees --apply" (the ownership-independent worktree reap), and "gopi task list --status failed" (the failed-needs-reissue census, with [queue-skip] naming a parked chain's parent); a run's own close-out rides its end (the release line), so there is no terminal-row pass left to invoke`
	}
	if verb == taskverbs.Stranded {
		// Retired 2026-10-30 (task 01a18799). It joined two facts — the ledger's
		// status and the row's own recorded ground/run log — and printed the
		// contradiction. Both facts are still readable, and the join is what left:
		// a LIVE row's status and its ground line are on the read, the run log is
		// a file the row's session directory holds. The parent-gate half of the
		// readout is asserted by the claim funnel itself.
		return `the contradiction readout was retired — both halves are still on the read: "gopi task show <id>" prints the ledger status AND the advisory ground line for the same row, and a run log is a file in that row's session directory (/home/user/.gopi/sessions/<task-id>/output.log); "gopi task list --status pending" marks an unclaimable row [queue-skip] with the parent gate's own reason`
	}
	if verb == taskverbs.New {
		return `task creation via the CLI was removed — use "gopi task submit <summary>" (enqueue as pending; the in-process auto-registration path is unaffected)`
	}
	// The remaining caller is the removed manual-claim spelling (`next`): its
	// replacement is the QUEUE face — the claim happens inside the runner now,
	// so the pointer names `gopi worker`, never the retired verb (epic
	// worker-run-entry: a removal message that points at another removed
	// spelling sends the reader in a circle).
	return `manual claiming was removed — use "` + taskProgName + `" (claims and runs the next eligible row) or "gopi task serve" (the daemon loop)`
}

// taskStatusExit* are the `gopi task status` exit codes — the machine-
// readable half of the verb's contract (a script asks "is this row really
// running?" and reads the code, never the prose). Named per Rule 1.
const (
	// taskStatusExitRunning: the row has a verified-live holder process —
	// a second run would double-execute.
	taskStatusExitRunning = 1
	// taskStatusExitUnknown: the evidence does not settle the question
	// (foreign host, stale/unreadable stamp). Distinct from running: the
	// caller must decide, so an automated resume must NOT treat it as free.
	taskStatusExitUnknown = 3
	// taskStatusExitGroundInUse: the row's LEDGER entry is free (a terminal
	// status, no live holder) but the GROUND it records holds a live process
	// right now, so a resume would put a second run into the same directory.
	// Its own code rather than taskStatusExitRunning because the two are
	// different answers to the question a script is asking: "the row's holder
	// is alive" (1) versus "the row's directory is occupied" (2). Both mean
	// DO NOT RESUME; the distinction is what tells an operator where to look
	// (the ledger vs the recorded ground).
	taskStatusExitGroundInUse = 2
	// taskStatusExitStatusRefused: the holder is free and the ground is free,
	// but the row's LEDGER STATUS says the WORK IS FINISHED (a success or
	// retired terminal) — see tools.TaskRunResumeStatusGate. `failed` is NOT
	// this code: a failed attempt is resumable (the two-level policy in
	// internal/tools/task_run_retry.go). Its own code because the remedy is
	// neither "wait for the run" nor "free the directory": the operator has to
	// re-copy the work into a CLEAN ROOM (`gopi task clone`, a fresh row) or submit it
	// anew. Collapsing it into 1 would tell a script a process is running when
	// none is.
	taskStatusExitStatusRefused = 5
)

// taskStatusExitNoRow maps a DELIVERABLE-SELECTOR failure to the code this verb
// must exit with.
//
// Two selector outcomes, two codes, and the split is the whole point:
//
//   - the ledger WAS read and no row names that branch/worktree: the family's
//     "no row owns this" ANSWER code (tools.TaskForBranchExitNoOwner), exactly
//     as `gopi task show --branch` answers it. Exit 1 must NOT be used here: on
//     THIS verb 1 means "running — do NOT resume" (taskStatusExitRunning), so a
//     selector naming nothing would answer "the row is alive" to every script
//     that reads the code. That confusion is the defect this verb exists to
//     remove (a wrong answer that looks like a certain one).
//   - everything else (the ledger could not be READ at all, both selectors
//     given): the generic failure code 1. Note the asymmetry is deliberate and
//     safe in this direction: 1 is already the running code, so a script that
//     treats "selector could not be resolved" as alarming is right.
//
// A selector error is REFUSED, never retried as an id: a branch name that
// happens to equal a task id must not silently address a different row.
func taskStatusExitNoRow(err error) int {
	if errors.Is(err, tools.ErrDeliverableNoOwner) {
		return tools.TaskForBranchExitNoOwner
	}
	return 1
}

// runTaskStatus implements
// `gopi task status [<task_id>] [--branch B | --worktree PATH] [--json] [--db PATH]`:
// the DEFINITE answer to "is this in_progress row actually running?" that the
// epic needs — the ledger status, the tri-factor verdict for the row's holder
// process (pid + process start time + command line, verified against the live OS
// process), and every raw fact behind it.
//
// Why a verb and not a flag on show: `task show` renders the RECORD (what the
// ledger holds); this renders the VERDICT (what is true of the machine right
// now), which requires probing /proc and is the gate `gopi session resume` asks. The
// exit code carries the verdict so shell callers need no parsing.
//
// THE ROW IS ADDRESSED THE SAME THREE WAYS `show` ADDRESSES IT (epic
// task-status-accepts-branch-or-worktree): the positional task id, or the
// DELIVERABLE the operator actually holds — `--branch B` (what
// `git branch --show-current` prints in a task worktree) or `--worktree PATH`
// (what `pwd` prints there). The zombie question is asked FROM a worktree ("is
// the row that owns this ground still alive?"), so the row id is exactly the
// fact that workflow does not carry. The resolution is the SHARED seam
// resolveDeliverableSelector (cli_tasks_show.go), so the two verbs cannot
// disagree about which row a selector names; the exit codes are the one place
// they legitimately differ, and taskStatusExitNoRow above states why.
func runTaskStatus(opts *tasksOptions) {
	taskID, provenance, selErr := resolveDeliverableSelector(context.Background(), opts)
	if selErr != nil {
		// The git provenance block (when there is one) goes to STDOUT — the
		// same channel the verdict renders on — so the operator who asked about
		// the ground he is standing in gets the git reading FIRST and the
		// refusal on stderr beside it, never a silent exit (Rule 15).
		if provenance != "" {
			fmt.Print(provenance)
		}
		fmt.Fprintf(os.Stderr, "gopi task status: %v\n", selErr)
		os.Exit(taskStatusExitNoRow(selErr))
	}
	st, err := tools.TaskProcessStatusFor(taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task status error: %v\n", err)
		os.Exit(1)
	}
	if opts.jsonOut {
		out, err := tools.TaskProcessStatusJSON(st)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi task status: json: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(out)
	} else {
		fmt.Print(tools.TaskProcessStatusReport(st))
	}
	// Exit code = the verdict (0 = a resume may proceed).
	switch st.Verdict {
	case tools.TaskProcessStateRunning:
		os.Exit(taskStatusExitRunning)
	case tools.TaskProcessStateForeign, tools.TaskProcessStateUnknown:
		os.Exit(taskStatusExitUnknown)
	}
	if st.GroundInUse {
		// The holder verdict said nothing is running; the row's own RECORDED
		// ground says somebody is. Refusing the resume is the only answer that
		// keeps the exit-code contract ("0 = a resume may proceed") true.
		os.Exit(taskStatusExitGroundInUse)
	}
	if st.StatusRefusal != "" {
		// The holder AND the ground are free, and the STATUS still forbids a
		// resume (a `failed` row's record IS a verdict). The refusal sentence
		// is already on STDOUT with the report — here it becomes the exit code,
		// so a script that never reads prose still cannot treat the row as
		// resumable.
		os.Exit(taskStatusExitStatusRefused)
	}
}

// runTaskClone implements `gopi task clone <task_id>`: the CLEAN-ROOM copy of a
// row. Its whole design is the status gate the engine owns (a row that may be
// RUNNING is refused, an unrun row is refused, a cancelled row needs an explicit
// yes); this arm only carries the operator's input across.
func runTaskClone(opts *tasksOptions) {
	out, err := tools.TaskCloneWithOptions(context.Background(), tools.TaskCloneOptions{
		TaskID:           opts.taskID,
		Reason:           opts.reason,
		Repo:             opts.successorRepo,
		DryRun:           opts.cloneDryRun,
		ConfirmCancelled: opts.confirmCancelled,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task clone: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out)
}

// ExpandFileRef is exported from internal/config so both the generic flag
// path (--prompt) and the hand-rolled CLI flag sets (task --payload /
// --reason) share ONE definition of the '@path' file-reference convention.
var ExpandFileRef = config.ExpandFileRef

// gopiExecutable resolves the gopi binary for task run's subprocess: the
// same running executable (go 1.19+ os.Executable), so a worktree build
// dispatches its own build.
func gopiExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return "gopi"
	}
	return exe
}

// taskSubmitPositionalPayloadNote is the stderr note printed when
// `gopi task submit "<text>"` runs without --payload: the text is mirrored
// into the payload column so the row stays dispatchable (Rule 1b — a named
// constant, not a bare literal).
const taskSubmitPositionalPayloadNote = "gopi task submit: no --payload given — the positional summary is mirrored into the payload (a payload-less row is never claimed by worker/serve)\n"

// taskSubmitIndependentParentNote is the stderr note printed when
// `--parent-task-id independent` was typed on the CLI face. The value is the
// task_submit TOOL's sentinel (toolnames.TaskSubmitIndependentSentinel); on
// this face it resolves to a parentless row. Saying so on stderr is what keeps
// the two faces ONE behaviour instead of two silent ones: the caller who read
// the tool schema learns that the spelling was understood, not ignored.
// The value is injected from its ONE definition (Rule 1/13) — the sentence
// cannot drift from the constant that decides the branch.
const taskSubmitIndependentParentNoteFormat = "gopi task submit: --parent-task-id %q is the task_submit tool's INDEPENDENT sentinel — this row is submitted with NO parent (the CLI never auto-chains, so omitting --parent-task-id has the same effect)\n"

// gopiProgramWord is the gopi COMMAND WORD as it is spelled inside a remote
// command string (`gopi task sync --serve-remote`) — the peer's binary name,
// the same word internal/runcmd publishes as BinaryName and internal/procname
// as NameGopi. Rule 1/13: one definition per package that must spell it, the
// compiler follows a rename, and no decision site re-types the literal.
const gopiProgramWord = cliverbs.Program

// sshOptionPairFlag is ssh's `-o KEY=VALUE` option flag — one argument plus
// its VALUE. It is named because two places in this package's argv parsing
// must agree on it: the passthrough prefix walk below, and the coverage lock
// in the task-family parser (parseTasksArgs) that skips ssh-option values
// when it looks for the sub-verb of a raw ssh-forwarded argv.
const sshOptionPairFlag = "-o"

// isArgvVerb reports whether token is a top-level gopi CLI verb word (any of
// the cliVerbs constants) — the ONE membership question the task router asks
// before it may read anything in argv as a remote command. Rule 1: the words
// come from the cliVerbs block, never from bare literals here.
func isArgvVerb(token string) bool {
	for _, v := range topLevelCommandNames {
		if token == v {
			return true
		}
	}
	return false
}

// argvAfterSSHPrefix walks argv past an ssh TRANSPORT prefix — option pairs
// (`-o KEY=VALUE`) and lone transport flags (`-4`, `-t`), then the host token —
// and returns what remains: the remote command line. nil means argv does NOT
// carry such a prefix (nothing was consumed, or argv begins with a gopi verb),
// i.e. it already starts at a command line and there is no ssh shape to guess
// about.
//
// ONE definition, TWO callers, because they must agree exactly: the router
// (isTasksSubcommand) uses it to decide whether a forwarded argv is a task
// invocation, and the parser entry (runTasksSubcommand) uses it to strip the
// very same prefix before parsing. A routed argv the parser then refuses
// (`unknown subcommand "BatchMode=yes"`) would leave the hijack half-fixed —
// measured before this edit: `gopi -o BatchMode=yes bnode gopi task list`
// reached runTasksSubcommand with the transport prefix still on the slice.
//
// WHY THIS PREDICATE EXISTS (the reported defect, 2026-09-29): the passthrough
// arm used to run whenever args[0] != "task" and ASSUMED the first non-flag
// token was an ssh HOST. `gopi web search "task list"` therefore read `web` as
// a host, word-split the rest, found the literal `task` inside the QUERY TEXT,
// and dispatched runTasksSubcommand(["list"]) — a ledger listing, exit 0, for a
// command the operator never typed (measured on the built binary; the
// `--method` flag of the abandoned web search even came out as an "unknown
// flag" of the task parser). The hijack is possible ONLY while a first token
// that NAMES A gopi VERB is still guessed to be a host, so the walk REFUSES
// such an argv outright: a genuine ssh argv begins with ssh OPTIONS (transport
// flags a remote command has no business carrying — the M4.13 shape this arm
// was written for), a bare `ssh <host> …` is stripped by the login shell before
// gopi ever sees it, and our own verb words are never host names.
func argvAfterSSHPrefix(args []string) []string {
	if len(args) == 0 || isArgvVerb(args[0]) {
		return nil
	}
	rest := args
	for len(rest) > 0 {
		if rest[0] == sshOptionPairFlag && len(rest) > 1 { // ssh option pair
			rest = rest[2:]
			continue
		}
		if strings.HasPrefix(rest[0], "-") { // lone transport flag (-4, -t, …)
			rest = rest[1:]
			continue
		}
		break // first non-flag = the host; stop skipping here
	}
	if len(rest) == 0 || len(rest) == len(args) || rest[0] == "" {
		return nil // nothing consumed — no transport prefix on this argv
	}
	return rest[1:] // drop the host token; the remote command is what follows
}

// runTaskSubmit implements `gopi task submit`: the human/software enqueue
// entry (mirrors the task_submit tool with source=human default). --payload
// supplies the briefing; when absent the summary IS the briefing — and it is
// ALSO mirrored into the payload column (taskSubmitPositionalPayloadNote),
// because a payload-less row is filtered out of every dispatcher selection
// (TaskNextFiltered's bug028 guard) and would never be claimed by a worker.
// The two refusal sentences of `gopi task submit`'s --source gate (Rule 1b:
// message text is a named constant, never a literal at the call site). Both
// name the OFFENDING value, the ACCEPTED vocabulary and — for the human claim —
// the escape hatch, because a refusal that does not say how to proceed sends
// the caller to the source code (Rule 15).
const (
	// taskSubmitInvalidSourceFormat: the caller spelled a source that is not in
	// the vocabulary at all.
	taskSubmitInvalidSourceFormat = "gopi task submit error: invalid --source %q (want %s)\n"
	// taskSubmitHumanClaimRefusedFormat: the caller CLAIMED a person filed the
	// row and this process cannot back the claim (it carries an agent chain).
	// Pre-fix this argv exited 0 and the row was stored as `agent` with no
	// warning; the refusal replaces that silent rewrite.
	taskSubmitHumanClaimRefusedFormat = "gopi task submit error: refusing --source %s: %s (this process is inside an agent session, so it may not claim a person filed the row — the row would be stored as %s). " +
		"File it with the role you actually are (%s/%s/%s), or, if you are the operator, re-run with %s=1\n"
)

func runTaskSubmit(opts *tasksOptions) {
	// The caller's OWN spelling is checked before anything is derived from it.
	// Derivation maps an unknown value onto the process's honest role, which is
	// right for a LIBRARY caller but wrong for a typed flag: it would turn
	// `--source hman` into a silent `agent` row and hide the typo (Rule 15).
	// Measured live: without this check the misspelling was accepted and
	// recorded as agent.
	if opts.source != "" && !tasksource.Known(opts.source) {
		fmt.Fprintf(os.Stderr, taskSubmitInvalidSourceFormat,
			opts.source, tasksource.Policy())
		os.Exit(1)
	}
	// An EXPLICIT human claim the gate denies is REFUSED, never rewritten. The
	// derivation below maps it onto this process's honest role, which for a
	// library caller is the right silent repair — but a typed `--source human`
	// is a CALLER'S ASSERTION, and an assertion silently turned into a
	// different value is the silent failure Rule 15 forbids (measured
	// 2026-09-23: rc=0, `Task queued: … source=agent`, the rule visible only in
	// this function's comments). The gate itself is unchanged and deliberate:
	// only a process with no agent chain above it, or the operator's own
	// GOPI_HUMAN_TERMINAL claim, may record human.
	if tasksource.IsHumanClaim(opts.source) && tasksource.HumanClaimDeniedNow() {
		fmt.Fprintf(os.Stderr, taskSubmitHumanClaimRefusedFormat,
			tasksource.Human,
			"only a process with no agent chain above it may record "+tasksource.Human,
			tasksource.DefaultFor(config.AgentSpawnSignatureNow()),
			tasksource.Agent, tasksource.Worker, tasksource.Server,
			config.HumanTerminalEnv)
		os.Exit(1)
	}
	// The source is DERIVED, never defaulted (epic task source fake human,
	// 2026-09-23). This line used to read `if source == "" { source = "human" }`,
	// which stamped EVERY row filed through the CLI as a human claim — including
	// the 457 agent-session rows on the operator's ledger that also carried a
	// model and a session id. Only a process with no agent chain above it (or
	// the operator's own GOPI_HUMAN_TERMINAL claim) may record `human`;
	// everything else records the role it actually is (agent/worker/server).
	source := tasksource.DeriveNow(opts.source)
	if opts.payloadMirrored {
		fmt.Fprint(os.Stderr, taskSubmitPositionalPayloadNote)
	}
	if opts.independentParent {
		// The sentinel was typed: report what was DONE with it, on the same
		// channel the positional-payload note uses. A caller who read the tool
		// schema and then typed this here must not have to guess whether it was
		// honoured or dropped (Rule 15).
		fmt.Fprintf(os.Stderr, taskSubmitIndependentParentNoteFormat, toolnames.TaskSubmitIndependentSentinel)
	}
	out, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary:       opts.summary,
		Payload:       opts.payload,
		Tags:          opts.tags,
		ParentTaskID:  opts.parentTaskID,                                             // parent 1 (set by the parser)
		ParentTaskIDs: tools.TaskParentIDsJoin(multiParentsRest(opts.parentTaskIDs)), // parents 2..N
		Source:        source,
		SessionID:     opts.sessionID,
		TaskID:        opts.taskID,
		Model:         opts.model,    // empty = left empty; writeback backfills
		Repo:          opts.repo,     // M4.8: verified + stamped as deliverable.repo
		Priority:      opts.priority, // M4.11: 0 = derive from tags (rule ④ override)
		Attachments:   opts.attachments,
		// RefuseUngroundableID — the ARM (epic 01a12b86, the migration that made
		// this armable). It was left false in 01a12b5c because the budget run
		// measured 67 tests in this very package driving THIS verb with slug
		// fixture ids (`--task-id t-g1-surface`, `t-positional-submit`,
		// `t-gate-role-agent`), which the refusal would have refused: a fixture
		// cannot be told from an operator's typo anywhere along this path — the
		// two are the same command with the same shapes — so the ORDER was the
		// only discriminator, and it is now discharged. Those fixture ids are
		// full uuids on this branch, so the arm refuses exactly the shape no
		// CLI-driven fixture produces any more. Measured: with the arm set, the
		// budget run over '--task-id t-*' finds nothing left to migrate and the
		// suite is GREEN.
		RefuseUngroundableID: taskSubmitRefusesUngroundableID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task submit error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out)
}

// mergerSerialFaceLegs runs the pass's NON-TICK legs — the part of the pass a
// ROUND CANNOT RUN FOR ITSELF — in the order they require. It is ONE definition
// reached by BOTH faces (Rule 14): the pass itself, and the tick-subset the
// LAUNCHING face runs before it starts a take-over
// (mergerLaunchSurvivingLegs, merger_tick.go) — a second caller that the
// daemon's deletion removed with the tick; the FIRST paragraph above is
// history, the ORDER contract below is what this file keeps.
//
// WHY THEY MUST RIDE THE LAUNCHING FACE TOO (measured 2026-09-18/19). The
// declaring face of these legs is an AGENT round: the round lands branches,
// writes the durable per-repo rebuild mark BEFORE it lands, and runs no side of
// this ladder on its own unless it survives. A round killed in flight is
// normal, expected and explicitly cheap — but with every leg below carried only
// by `--once` (a face no deployment ran; the merger tick and its repeat both
// returned from the LAUNCH branch), a killed round's
// obligations were never discharged by anybody:
//
//   - the declared-rebuild drain had NO reader on the live face, so the
//     artifact stayed stale for ever;
//   - the dead holder — the round's own worktree, parked by its death — was
//     never resumed and never reclaimed, so its branch stayed PROTECTED and
//     withheld from merging, while the protected line advertised a release
//     nothing would perform (the measured 2026-09-13 shape, one face over).
//
// THE ORDER inside is load-bearing:
//
//	① the DRAIN first (mergerPassCloseLoop(nil)) — it is the pass's very first
//	   act so a pass that dies a second later has already healed a stale
//	   artifact (see mergerPassOnce for the full reasoning);
//	② the RESUME leg, then ③ the RECLAIM leg. RESUME continues the interrupted
//	   RUN of a holder this host proved dead and removes nothing, so it is safe
//	   anywhere; RECLAIM FREES that holder's worktree, and a run whose worktree
//	   has been removed can no longer be continued. Recovering after a reclaim
//	   is impossible, so the reverse order would silently turn recoverable work
//	   into abandoned work while both legs reported success.
//
// Both ladder legs walk the LEDGER and match it against git's own worktree
// registry, so they are repo-agnostic and run once, not once per configured
// repo. They sit BEFORE the repo sweep because a dead holder is exactly what
// makes the sweep skip the branch as PROTECTED: doing this at the tick boundary
// (no merge in flight) is what lets the very same pass see the branch as
// ordinary merge input.
//
//	④⑤ the cross-node channel passes (epic 2026-09-01/08-31): the peer-facing
//	   half — the server fetches the client's feat/* and adopts them in (clean
//	   merge / conflict dispatch), and adoption discovers + probe-merges
//	   push-back branches. Both are strict no-ops when no sync peer is declared
//	   (the single-node settings shape), and each is throttled on its OWN
//	   process-wide clock, which is what makes this call re-entrant from the
//	   launching face: the inner call pays what the outer has not paid.
//
// NOTHING HERE IS A MERGE JUDGEMENT: the drain rebuilds what a landing already
// did, the ladder judges a HOLDER (dead or not, from the ledger and /proc), and
// the cross-node passes move refs the operator declared. Which BRANCH lands
// stays in the merge face's own page (`gopi task merge -h`; its shipped prompt
// cmd/agent/merge_prompt.md went with the retired one-shot verb, epic
// merge-verb-removal 2026-11-01) — the
// per-repo sweep is deliberately NOT part of this helper.
// mergerSerialFaceLegs runs the pass's NON-TICK legs — the part of the pass a
// ROUND CANNOT RUN FOR ITSELF — in the order they require.
//
// IT REACHED TWO FACES; BOTH ARE GONE. The second caller was the tick-subset the
// LAUNCHING face ran before it started a take-over (mergerLaunchSurvivingLegs,
// merger_tick.go) — both the launching face and the take-over were deleted with
// the retired daemon (epic merger-delete-dead-impl, 2026-09-19). The FIRST
// caller, mergerPassOnce, went with the same epic's follow-on: measured
// 2026-11-01 (task 01a19466, tree 52657bb6), renaming this declaration makes the
// compiler name exactly ONE reference — the `mergerSerialFaceLegs(opts)` call
// inside mergerPassOnce — and renaming mergerPassOnce makes `go build ./...`
// exit 0. So today this
// function's only caller is a TEST (cli_tasks_dead_holder_accept_test.go).
//
// WHICH LEGS SURVIVE ON A LIVE FACE, AND WHICH DO NOT — measured, not assumed:
//
//	① the DRAIN (mergerPassCloseLoop) — LIVE, on a second carrier that outlived
//	  the pass: the `mergerCloseLoopAfterLanding(rep.Repo)` call in
//	  runTaskMergeExecute. The declared-rebuild marks ARE discharged.
//	④⑤ the cross-node passes — NOT LIVE EITHER. They rode `gopi task
//	  sync-pass --once` (cli_tasks_sync_pass.go:141-142), and BOTH the verb and
//	  that file are DELETED (2026-11-05; measured: `gopi task sync-pass --once`
//	  exits 1 with "removed — the cross-node sync surface is DELETED").
//	  Recorded as a fact, not hidden: a reader who follows the old trail finds
//	  the removal sentence instead of a pass that cannot be started.
//	②③ the dead-holder ladder — NOT REACHED BY ANY PRODUCTION FACE. That is the
//	  honest state, and it is recorded here rather than papered over: a terminal
//	  row whose holder died still keeps its branch PROTECTED, and nothing
//	  resident reclaims its worktree. Re-homing this ladder is a DUTY of the
//	  re-development, and this is where it goes back (Rule 15: name the gap,
//	  never let a comment claim a release nothing performs).
//
// The function and its ORDER contract are kept whole for that re-development:
// the order below is the whole point of the file, and a revived face must not
// have to re-derive it.
//
// WHY THEY MUST RIDE THE LAUNCHING FACE TOO (measured 2026-09-18/19). The
// declaring face of these legs is an AGENT round: the round lands branches,
// writes the durable per-repo rebuild mark BEFORE it lands, and runs no side of
// this ladder on its own unless it survives. A round killed in flight is
// normal, expected and explicitly cheap — but with every leg below carried only
// by `--once` (a face no deployment ran; the merger tick and its repeat both
// returned from the LAUNCH branch), a killed round's
// obligations were never discharged by anybody:
//
//   - the declared-rebuild drain had NO reader on the live face, so the
//     artifact stayed stale for ever;
//   - the dead holder — the round's own worktree, parked by its death — was
//     never resumed and never reclaimed, so its branch stayed PROTECTED and
//     withheld from merging, while the protected line advertised a release
//     nothing would perform (the measured 2026-09-13 shape, one face over).
//
// THE ORDER inside is load-bearing:
//
//	① the DRAIN first (mergerPassCloseLoop(nil)) — it is the pass's very first
//	   act so a pass that dies a second later has already healed a stale
//	   artifact (see mergerPassOnce for the full reasoning);
//	② the RESUME leg, then ③ the RECLAIM leg. RESUME continues the interrupted
//	   RUN of a holder this host proved dead and removes nothing, so it is safe
//	   anywhere; RECLAIM FREES that holder's worktree, and a run whose worktree
//	   has been removed can no longer be continued. Recovering after a reclaim
//	   is impossible, so the reverse order would silently turn recoverable work
//	   into abandoned work while both legs reported success.
//
// Both ladder legs walk the LEDGER and match it against git's own worktree
// registry, so they are repo-agnostic and run once, not once per configured
// repo. They sit BEFORE the repo sweep because a dead holder is exactly what
// makes the sweep skip the branch as PROTECTED: doing this at the tick boundary
// (no merge in flight) is what lets the very same pass see the branch as
// ordinary merge input.
//
//	④⑤ the cross-node channel passes (epic 2026-09-01/08-31): the peer-facing
//	   half — the server fetches the client's feat/* and adopts them in (clean
//	   merge / conflict dispatch), and adoption discovers + probe-merges
//	   push-back branches. Both are strict no-ops when no sync peer is declared
//	   (the single-node settings shape), and each is throttled on its OWN
//	   process-wide clock, which is what makes this call re-entrant from the
//	   launching face: the inner call pays what the outer has not paid.
//
// NOTHING HERE IS A MERGE JUDGEMENT: the drain rebuilds what a landing already
// did, the ladder judges a HOLDER (dead or not, from the ledger and /proc), and
// the cross-node passes move refs the operator declared. Which BRANCH lands
// stays in the merge face's own page (`gopi task merge -h`; its shipped prompt
// cmd/agent/merge_prompt.md went with the retired one-shot verb, epic
// merge-verb-removal 2026-11-01) — the
// per-repo sweep is deliberately NOT part of this helper.
func mergerSerialFaceLegs(opts *tasksOptions) {
	// ① THE DURABLE-MARK DRAIN. A repo marked by a landing whose process was
	// interrupted before its close loop ran is brought current here, before any
	// other work (internal/tools/rebuild_pending.go owns the marks).
	mergerPassCloseLoop(nil)

	// ②③ THE DEAD-HOLDER LADDER, resume before reclaim (see the order note
	// above).
	mergerResumeDeadHolderLeg()

	// The reclaim's three guards (dead claim pid, clean worktree, no live
	// process inside) are the arm's own; it never touches the BRANCH — the
	// branch is the deliverable.
	for _, line := range mergeDeadHolderReclaimLeg() {
		hostLifecyclePrint("[merger] dead-holder reclaim: %s\n", line)
	}

	// ④ THE CROSS-NODE ADOPTION PASS — NOT the local config-repo sweep: it
	// delivers branches ACROSS nodes.
	//
	// The git-sync pass that used to follow it (serveGitSyncSweep) is DELETED
	// (2026-11-05): its two legs were the row sync tick and the delta-sync
	// switch, both removed with the sync family, so what remained was a clock
	// with nothing to run. The adoption sweep stays because adoption is a
	// LOCAL git operation — it discovers and chains branches on this node.
	serveAdoptionSweep(opts.repo)
}

// mergerPassOnce runs ONE full merger pass: the serial face legs above, then a
// pure-git sweep turn per configured repo.
//
// NO PRODUCTION CALLER (measured 2026-11-01, task 01a19466, tree 52657bb6): with
// its declaration renamed, `go build ./...` exits 0. It is kept WHOLE for
// re-development — see the block above "THE RETIRED MERGER TICK" — so its body is
// not evidence of what production runs; `gopi task merge` → tools.TaskMerge is.
func mergerPassOnce(opts *tasksOptions) {
	// THE TICK-LEVEL REBUILD INVARIANT (epic merger-always-rebuilds) is ARMED
	// FIRST — the tick's very first statement, before any work and before the
	// repo list is even resolved — as a DEFER, so it runs on EVERY exit path of
	// this function rather than only the fall-through one. It fires at the END
	// of the tick, after every master-advancing step, which is the ordering the
	// operator asked for: the binary follows the master the tick landed.
	//
	// Why a defer, why first (measured defect, this epic's closeout): the pass
	// has exits BEFORE its own sweep — unreadable settings, an empty repos
	// table — and the cross-node passes above the sweep (serveAdoptionSweep)
	// advance master. A rebuild a `return` can step over is not an invariant, so
	// the arm precedes all of them. The CLOSURE is load-bearing: `defer
	// mergerPassCloseLoop(repos)` would evaluate `repos` at the defer statement,
	// i.e. before the resolve below, and rebuild nothing forever. An exit that
	// never resolved a list carries the empty one, which is a cheap no-op (a
	// repo with no build contract is skipped in silence — mergerPassCloseLoop).
	//
	// `repos` is DECLARED here (not resolved here) for exactly that reason: the
	// defer's closure is legal only if the name is already in scope, and the
	// value it will see is whatever the resolve below assigns.
	var repos []string
	defer func() { mergerPassCloseLoop(repos) }()
	// DRAIN THE DURABLE MARKS FIRST — this pass's very first ACT, before any
	// work, before the sweep, before any child (see
	// internal/tools/rebuild_pending.go for the marks themselves).
	//
	// Why the drain cannot live only at the pass END (where the defer above also
	// runs it): a SIGKILL runs NO defer, so the pass-end close loop is exactly
	// the step a killed pass never reaches — and killed passes are the measured
	// reality. The 08:50-era daemon left three state files (2436009, 2456472,
	// 2470012) each with passes=0: every pass was killed INSIDE its sweep (the
	// 30m pass cap, or the operator's stop), so `bin/gopi` stayed older than the
	// source it no longer contained for ~4h — the operator's symptom.
	//
	// Placing the drain at the START inverts that: the first microsecond of any
	// pass heals a stale artifact, so a pass that dies a second later has already
	// done the work this epic exists for. It is free on a clean host (no marks ⇒
	// nothing is scanned beyond one directory read) and idempotent (a repo whose
	// binary already follows its tip costs one `--version` probe).
	// THE SERIAL FACE LEGS — the part of the pass a ROUND CANNOT RUN FOR ITSELF.
	//
	// ONE definition, reached by BOTH faces (Rule 14): this pass, and the
	// tick-subset the LAUNCHING face ran (cmd/agent/merger_tick.go,
	// mergerLaunchSurvivingLegs — both deleted with the retired daemon, epic
	// merger-delete-dead-impl, 2026-09-19). The extraction was the point rather
	// than a second copy of these five calls: every leg below survives a round
	// that does NOT survive, so ANY face that deploys one must run them itself,
	// and this is where a future resident face reaches them.
	mergerSerialFaceLegs(opts)

	// The core pass: iterate every configured repo (settings.json "repos"
	// table keys) and run that repo's pure-git sweep turn, then rebuild it
	// when the turn landed work. No repos table = nothing to sweep (the
	// merger logs and continues — no error, no guess).
	var err error
	repos, err = config.ResolveMergerRepos()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[merger] [repos] resolve: %v\n", err)
		return
	}
	if len(repos) == 0 {
		hostLifecyclePrintln("[merger] [repos] no configured repos in settings repos table — nothing to sweep")
		return
	}
	for _, repo := range repos {
		mergerSweepRepo(repo)
	}
	mergerPassCloseLoop(repos)
	// CWD RELEASE AT THE PASS BOUNDARY (worktree-release coverage, 2026-09-14):
	// the merger is the resident component on a task host, so anything that
	// leaves THIS process's cwd inside a task worktree pins that worktree
	// forever — guard ③ of the reclaim arm can never stop refusing, because
	// the holder never exits. The pass's OWN steps are cwd-disciplined (every
	// git call carries -C <repo>, the resolve/verify children are spawned with
	// an explicit Dir), so this is the defensive half: a pass that somehow
	// ended up in a task worktree — the row it just merged was removed under
	// it — hands the directory back before the next tick. Best-effort and
	// reported (Rule 15); ONE implementation (Rules 13/14).
	worktreerelease.ReleaseReporting(os.Stderr, mergerProgTopLevel)
}

// mergerSweepRepo runs MergerRepoSweep over one configured repo and — when
// the tick landed or pruned work — rebuilds that repo's declared build (the
// rebuild-after-merge contract, per repo). Fail-soft per repo: a sweep error
// degrades to a log line; the tick continues to the next repo.
//
// UNREACHABLE IN PRODUCTION (measured 2026-11-01, task 01a19466, tree 52657bb6):
// its only caller is mergerPassOnce, which has no caller either. Renaming this
// declaration makes the compiler name exactly one reference — the call to
// mergerSweepRepo inside mergerPassOnce — so no other production site reaches it. The reachable merge path is `gopi task merge` → tools.TaskMerge, which
// discovers its candidates through tools.MergerSweepPreview (task_merge.go).
//
// The merger's own face is the PURE one (epic merger-ff-park-file): no
// resolver closure, no verify child, no wait. What the sweep may do to a repo
// is merge a fast-forwardable branch, REPORT a non-fast-forwardable one
// (nothing is written for it — epic merger-remove-park, 2026-09-17), refuse an
// uncompilable one, and tidy merged worktrees.
func mergerSweepRepo(repo string) tools.RepoSweepResult {
	// Mechanism-2 snapshot (epic merger-failure-dual-mechanism, 2026-09-08):
	// BEFORE the sweep runs, capture master's pre-sweep tip + the branch
	// registry, so a landing whose rebuild FAILS can be auto-reverted to the
	// pre-sweep buildable state (master must stay buildable — nick). If the
	// capture fails (no PreTip) the tick simply lands WITHOUT auto-revert
	// capability — never abort the sweep over the snapshot.
	capture := tools.CaptureFailedMergeRevert(repo)
	res := tools.MergerRepoSweep(repo, 0)
	if res.Err != "" {
		fmt.Fprintf(os.Stderr, "[merger] [%s] skipped: %s\n", repo, res.Err)
		return res
	}
	summary := []string{fmt.Sprintf("merged=%d", res.Merged)}
	if res.Deleted > 0 {
		summary = append(summary, fmt.Sprintf("deleted=%d", res.Deleted))
	}
	if res.NotFF > 0 {
		// Branches this tick could NOT land: reported, nothing touched (no tag,
		// no ledger row, no worktree removal). They keep their place in the next
		// tick's candidate set and the `gopi merge` path hands them over
		// (repoSweepNotFFOutcome, epic merger-remove-park).
		summary = append(summary, fmt.Sprintf("not-ff=%d", res.NotFF))
	}
	if res.RefGone > 0 {
		// Candidates whose ref was deleted between discovery and the decision:
		// skipped before any gate, and every live row still claiming the branch
		// carries a `ref-gone` event on its own audit trail (repo_sweep_ref_guard.go).
		// Named on the operator's line — a disappearance folded into a silence is
		// the defect this arm closes.
		summary = append(summary, fmt.Sprintf("ref-gone=%d", res.RefGone))
	}
	if res.ReworkSubmitted > 0 {
		summary = append(summary, fmt.Sprintf("rework-submitted=%d", res.ReworkSubmitted))
	}
	if res.ContractRefused > 0 {
		summary = append(summary, fmt.Sprintf("contract-refused=%d", res.ContractRefused))
	}
	if res.BuildRefused > 0 {
		summary = append(summary, fmt.Sprintf("build-refused=%d", res.BuildRefused))
	}
	if res.BuildArchived > 0 {
		summary = append(summary, fmt.Sprintf("build-archived=%d", res.BuildArchived))
	}
	if res.Protected > 0 {
		// The protected arm is reported through ONE renderer
		// (RepoSweepPreview.DescribeProtected), so the merger log and the
		// operator's status preview cannot drift — and a branch a PROVABLY
		// DEAD holder is occupying is NAMED here instead of vanishing into a
		// bare count (the 2026-09-12 incident: seven finished deliverables
		// withheld behind "protected=7").
		summary = append(summary, tools.RepoSweepPreview{
			Protected:      res.Protected,
			DeadHolder:     res.DeadHolder,
			Held:           res.Held,
			Attribution:    res.Attribution,
			AttributionErr: res.AttributionErr,
		}.DescribeProtected())
	}
	if len(res.DirtySalvage) > 0 {
		// The dirty dead-holder salvage lines are printed verbatim: each names
		// the branch, the row that carried the work and the capture's sha256 —
		// an operator reading the merger log sees the hand-over, not a count.
		for _, l := range res.DirtySalvage {
			hostLifecyclePrint("[merger] [%s] %s\n", repo, l)
		}
		summary = append(summary, fmt.Sprintf("dirty-salvage=%d", len(res.DirtySalvage)))
	}
	// One line per candidate: the tick's whole branch surface is visible, in the
	// order it was decided — that ordering is the evidence that a branch the tick
	// cannot land does not stop the tick (the branches after it are printed after it).
	for _, o := range res.Outcomes {
		hostLifecyclePrint("[merger] [%s] branch %s (%s): %s\n", repo, o.Branch, o.Action, o.Detail)
	}
	hostLifecyclePrint("[merger] [%s] %s\n", repo, strings.Join(summary, " "))
	// Rebuild-after-merge (epic 2026-09-02): a tick that LANDED work leaves the
	// repo's binary following the new master tip (a prune is a report now, so it
	// moves nothing). No build contract
	// → silent skip. Mechanism 2: if the landing rebuild FAILS, the snapshot
	// lets serveRebuildAfterMerge auto-revert this tick's merges back to the
	// pre-sweep buildable master (territory the caller landed — only THIS
	// round, only when capture.PreTip was known).
	if res.Merged > 0 || res.Deleted > 0 {
		// The handle carries the pre-round tip (+ branch tips) that a failed
		// rebuild must restore. It used to carry this round's merged branch
		// names as well, purely to label the version-tag mint's message; that
		// mint is deleted (epic version-identity, 2026-09-24) and the field
		// with it, so nothing here has to be collected for a name.
		var rvt *tools.FailedMergeRevert
		if capture.PreTip != "" {
			rvt = &tools.FailedMergeRevert{
				Repo:       repo,
				PreTip:     capture.PreTip,
				BranchTips: capture.BranchTips,
			}
		}
		serveRebuildAfterMerge(repo, rvt)
	}
	return res
}

// mergerResumeDeadHolderLeg is the MERGER-pass entry point of the dead-holder
// RESUME arm (epic dead-holder-resume, 2026-09-16). A named seam for the same
// reason mergeDeadHolderReclaimLeg is one: the regression it guards is "the
// merger never offered the recovery", which an assertion against the arm
// itself cannot see — the capability would exist, pass its own tests, and never
// be reached by the component an operator actually runs (the measured
// 2026-09-13 shape).
//
// THAT REGRESSION IS NOW THE MEASURED STATE, and it must be said here rather
// than by a comment implying otherwise: measured 2026-11-01 (task 01a19466, tree
// 52657bb6), the compiler names exactly one reference to this declaration —
// the `mergerResumeDeadHolderLeg()` call inside mergerSerialFaceLegs — and that
// function's only caller is mergerPassOnce, which has no caller. So NO production face runs the
// resume arm today; only its own test does. Re-homing it onto a live face is a
// duty of the re-development (see the "IT REACHED TWO FACES; BOTH ARE GONE" note
// above it), not something this comment can discharge.
//
// It walks the LEDGER once per configured repo (a row's worktree is matched
// against that repo's own linked-worktree registry) and prints one line per row
// it JUDGED: a resume that happened (naming the session it continues) or a
// refusal an operator must see (an unreadable probe, or the attempt cap). A row
// whose holder is LIVE prints nothing: the branch is simply still owned, and a
// line per tick about work proceeding normally is noise.
func mergerResumeDeadHolderLeg() {
	repos, err := config.ResolveMergerRepos()
	if err != nil {
		// An unreadable settings document is REPORTED here and then declined:
		// the caller (mergerPassOnce) is the site that prints the resolution
		// failure, and it has no production caller today, so nothing is
		// swallowed silently — this arm simply declines to guess a repo set.
		return
	}
	for _, repo := range repos {
		for _, o := range tools.ResumeDeadHolderRuns(repo) {
			fmt.Printf("[merger] [%s] %s\n", repo, o.Line())
		}
	}
}

// mergeDeadHolderReclaimLeg is the MERGER-pass spelling of the reclaim leg —
// the ONLY face left since 58b5618b retired the worker-tick sweep (see
// finishCleanupDeadHolderReclaimLines for why the merger owns it). It is a
// named seam rather than an inlined call so the merger's duty to run it is
// testable on its own: the regression it guards is precisely "the merger never
// invoked the arm", which an assertion against the arm itself cannot see.
//
// THAT REGRESSION IS NOW THE MEASURED STATE, like its RESUME sibling above:
// measured 2026-11-01 (task 01a19466, tree 52657bb6), the compiler names exactly
// one reference to this declaration — the `mergeDeadHolderReclaimLeg()` call in
// mergerSerialFaceLegs — whose only caller is mergerPassOnce (no caller). No
// production face reclaims a dead holder's worktree today. Re-homing it is a
// duty of the re-development (Rule 15: the gap is recorded, not hidden).
func mergeDeadHolderReclaimLeg() []string {
	return finishCleanupDeadHolderReclaimLines()
}

// runTasksSubcommand parses the sub-verb's flags and dispatches to the shared
// tools.Task* functions. It exits the
// process with a non-zero status on argument/usage errors; engine errors are
// echoed to stderr and exit 1 (script-friendly, like code-search).
// runTaskList renders the `task list` verb: the ledger read, then the
// archived-row filter. It exists as a named function so the read-path contract
// is ONE call — the verb is a PURE READ: it touches no ledger row and fires no
// notify event (the sweep it used to be able to request is deleted;
// cli_tasks_list_session_scan_test.go and cli_tasks_list_ledger_hint_test.go
// drive this body through the real parse path).
func runTaskList(opts *tasksOptions) (string, error) {
	// The archived-row visibility predicate lives in tools (TaskListVisible),
	// NOT here: it used to be this function's `if !opts.all` while the
	// conversational `/task list` (commands.go) called tools.TaskList bare and
	// rendered the tombstones — one rule, two spellings, opposite answers.
	return tools.TaskListVisible(opts.status, opts.all)
}

func runTasksSubcommand(args []string) {
	// The verb is the first positional; parse it out before flag parsing
	// (flags like --db may appear before or after the verb). The -h scan
	// runs over the REST so `gopi task sync -h` can print the per-verb
	// sync-family head (syncUsageHead) instead of the whole family blob.
	verb := ""
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		verb = args[0]
		rest = args[1:]
	}
	for _, a := range rest {
		if a == "-h" || a == "--help" {
			switch verb {
			case taskverbs.Archive:
				// The archive verb's page is its OWN (cli_archive.go owns the
				// text; the branch-burial rationale is the first thing its
				// reader needs). It became reachable only through this group
				// when the top-level `gopi task archive` was retired (epic
				// verb-retirement, 2026-10-05) — so the -h scan must hand the
				// verb its page HERE, or the page survives with no door.
				fmt.Print(archiveUsage())
				os.Exit(0)
			case taskverbs.SyncPass:
				// A REMOVED spelling's `-h`: the removal sentence, exit 0
				// (P0-1: a usage query answers). Its page went with the pass
				// itself — cli_tasks_sync_pass.go is deleted, and a page that
				// documents a deleted pass is a lie a reader would act on.
				fmt.Printf("gopi task %s: removed — %s\n",
					verb, taskRemovedAdvice(verb))
				os.Exit(0)
			case taskverbs.Sync, taskverbs.Diff, taskverbs.Harvest, taskverbs.Push,
				taskverbs.Send, taskverbs.Receive, taskverbs.Pull:
				// A REMOVED spelling's `-h`: the removal sentence, exit 0 —
				// the same contract the retired `run` case below states
				// (P0-1: a usage query answers, whatever the state of the
				// world; printing the family blob would document a verb the
				// family no longer has, and a page documenting a deleted
				// protocol is one a reader would act on).
				fmt.Printf("gopi task %s: removed — %s\n",
					verb, taskRemovedAdvice(verb))
				os.Exit(0)
			case taskverbs.Serve, taskverbs.Merger:
				// The two aliases of the RETIRED merger daemon (2026-09-19,
				// devdoc/merger-retired.md). Their -h prints ONE pointer line
				// and exits 0, never the whole family blob (epic
				// cli-help-dedup — the operator who types the OLD spelling must
				// learn the new home from a sentence, not from a page they have
				// to diff against the canonical one). The line names the face
				// that still merges, because the verb the two spellings used to
				// point at (`gopi merger`) now refuses (Rule 15).
				//
				// A USAGE QUERY IS NOT A RUN: -h exits 0 here while every other
				// argument list reaches retireMergerFace below (exit 1). That
				// split is the contract — refusing `-h` would answer a question
				// with a failure, which is the P0-1 rule the retired `task run`
				// case beside it states.
				printTaskServeAliasPointer(verb)
				os.Exit(0)
			case taskverbs.Merge, taskverbs.MergeExecute,
				taskverbs.MergedCleanup, taskverbs.Adopt, taskverbs.AuditMerges:
				// THE GIT ATOMS' OWN PAGES (task 01a0dd4f, 2026-09-25). The
				// family help POINTS at `gopi merge -h` for these spellings
				// ("the git-infrastructure atoms … are documented by 'gopi
				// merge -h'"), and until this case existed the pointer was a
				// dead end: all five fell through to the family blob, so
				// `gopi task merge -h` was BYTE-IDENTICAL to `gopi task -h`
				// (measured: same md5, 20421 bytes). It is a defect and not an
				// ugly page because the neighbouring argv RUNS A MERGE: the one
				// argv an operator types by mistake to LOOK (`-h`, a mis-typed
				// flag) sits one keystroke from the argv that writes refs under
				// the merge lock — so the page must at least say which verb he
				// asked about.
				//
				// The pages live in cli_tasks_merger_help.go, print and return
				// without taking the lock or touching a repo (P0-1: a usage
				// query answers whatever the state of the world), and their
				// flag spellings are composed from the merge face's OWN
				// constants so the page cannot advertise a spelling the parser
				// refuses (Rules 1/13/14).
				if page := taskMergerAtomUsage(verb); page != "" {
					fmt.Print(page)
					os.Exit(0)
				}
			case taskverbs.Run:
				// A RETIRED spelling's `-h`: the removal sentence, exit 0.
				// Printing the whole family blob would document a verb the
				// family no longer has, and exiting non-zero would refuse a
				// usage query (the P0-1 contract: -h answers, whatever the
				// state of the world).
				fmt.Printf("gopi task %s: removed (epic worker-run-entry) — %s\n",
					taskverbs.Run, taskRemovedAdvice(taskverbs.Run))
				os.Exit(0)
			}
			fmt.Print(tasksUsage)
			os.Exit(0)
		}
	}
	if verb == "" {
		fmt.Fprintf(os.Stderr, "gopi task: missing subcommand\n%s", tasksUsage)
		os.Exit(1)
	}

	// The agent-spawn guard that stood here (operator ruling 2026-09-12,
	// devdoc/pa-never-runs-tasks-20260912.md) is GONE with the verb it guarded
	// (epic worker-run-entry, 2026-09-16): `gopi task run` no longer executes
	// anything — it prints where running moved and exits — so refusing it to an
	// agent would refuse a message. The agent-side protection now lives where
	// the run does (the `gopi worker` dispatcher's own guardVerbOrExit), which
	// is the face a stalled agent would actually reach.

	// THE RETIRED ALIASES ARE CLOSED BEFORE THE PARSER (epic
	// merger-delete-dead-impl, 2026-09-19). `task serve` and `task merger` are
	// the deprecated spellings of the resident merger daemon; they used to
	// dispatch to the full merger face with its own flag table, and the
	// retired-daemon flags (`--daemon`/`--start`/`--wait`/`--force`) were
	// refused there by name. That face is deleted, so the generic parser would
	// answer `task merger --daemon --status` with "unknown flag --daemon" plus
	// the whole family blob — which reads as a REGRESSION to an operator whose
	// cron line still carries the flag, and buries the decision (Rule 15: a
	// refusal must explain itself). The check therefore sits ABOVE
	// parseTasksArgs: every spelling of either alias reaches the retirement
	// sentence, whatever flags follow it, and nothing is parsed, no lock is
	// taken and no state is written on the way to the message.
	//
	// It must ALSO sit above the `-h` scan's fall-through — it does: the scan
	// returns its own pointer for these two verbs, and this guard is what
	// catches every other argument list.
	if verb == taskverbs.Serve || verb == taskverbs.Merger {
		retireMergerFace(verb)
	}

	// THE CROSS-NODE FAMILY IS CLOSED BEFORE THE PARSER, and for the same reason
	// the merger aliases below are: their flags (`--peer`, `--peers`,
	// `--serve-remote`, `--once`, …) are NOT ledger flags, so the family parser
	// would answer a script's `gopi task sync --peer hp` with "unknown flag
	// --peer" plus the whole family blob — which reads as a REGRESSION and
	// buries the decision (Rule 15: a refusal must explain itself). Every
	// spelling reaches its removal sentence whatever flags follow it, and
	// nothing is parsed, no peer is dialled, no lock is taken and no state is
	// written on the way to the message.
	//
	// `-h` never reaches here: the help scan above answers it with the same
	// sentence and exit 0 (P0-1 — a usage query answers).
	switch verb {
	case taskverbs.Sync, taskverbs.Send, taskverbs.Receive, taskverbs.Pull,
		taskverbs.Push, taskverbs.Diff, taskverbs.Harvest, taskverbs.SyncPass:
		fmt.Fprintf(os.Stderr, "gopi task %s: removed — %s\n", verb, taskRemovedAdvice(verb))
		os.Exit(1)
	}

	opts, err := parseTasksArgs(verb, rest)
	if err != nil {
		// "new"/"next" were REMOVED from the CLI surface (M4.5 台账治理:
		// creation goes through task submit, claiming through task run
		// next) — a removed verb gets the pointed removal message, not the
		// generic usage dump.
		if verb == taskverbs.New || verb == taskverbs.Next {
			fmt.Fprintf(os.Stderr, "gopi task %s: removed (M4.5) — %s\n", verb, taskRemovedAdvice(verb))
			os.Exit(1)
		}
		// A CANONICAL MERGE-FACE FLAG GIVEN TO THE ATOM (task 01a0ecea,
		// 2026-09-29). `gopi task merge` and `gopi merge` share an engine but
		// NOT a parser (taskFlagMember admits four flags for the atom;
		// parseMergeArgs reads nine for the canonical verb), so an operator who
		// read the two faces as one spelling gets "unknown flag --status" plus
		// the 20 KB family blob — the refusal did not say that the flag exists
		// elsewhere, which is the whole content of the answer (Rule 15: a
		// refusal is obeyed-or-refused BY NAME and must explain itself).
		//
		// The pointer is printed INSTEAD OF the family blob, for the same reason
		// the retired aliases above are caught before this parser: a 20 KB dump
		// buries the sentence that answers the question. Only a spelling the
		// canonical table really owns is named this way — any other unknown flag
		// keeps the ordinary refusal, so the pointer can never claim a flag the
		// canonical face does not read (the biconditional gate pins both
		// directions).
		fmt.Fprintf(os.Stderr, "gopi task %s: %v\n%s", verb, err, tasksUsage)
		os.Exit(1)
	}

	// --db → GOPI_MEMORY_DB so the shared engine functions use that path.
	if opts.db != "" {
		if err := os.Setenv("GOPI_MEMORY_DB", opts.db); err != nil {
			fmt.Fprintf(os.Stderr, "gopi task %s: --db: %v\n", verb, err)
			os.Exit(1)
		}
	}

	switch verb {
	case taskverbs.Submit:
		runTaskSubmit(opts)
	case taskverbs.Run:
		// The retired spelling, at the ONE place that would have run it: a
		// sentence naming where the function went (Rule 15 — a removal must
		// explain itself), never the generic usage dump, and never a
		// silently-different runner.
		fmt.Fprintf(os.Stderr, "gopi task %s: removed (epic worker-run-entry) — %s\n", taskverbs.Run, taskRemovedAdvice(taskverbs.Run))
		os.Exit(1)
	case taskverbs.Verify, taskverbs.Reconcile, taskverbs.RetryDelivery:
		// THE THREE RETIRED IMPLEMENTATION-SHAPED VERBS (2026-10-30, task
		// 01a18799, slice 2/4), at the ONE place that would have run them: a
		// sentence naming what each did and where its reader goes instead
		// (Rule 15 — a removal must explain itself), never the generic usage
		// dump and never a silently-different runner. The exit is non-zero
		// because nothing was measured: a script that gated on `verify` must
		// hear "this surface is gone", not read a clean status as "judged".
		fmt.Fprintf(os.Stderr, "gopi task %s: removed — %s\n", verb, taskRemovedAdvice(verb))
		os.Exit(1)
	case taskverbs.BranchRefs, taskverbs.ForBranch, taskverbs.Orphans, taskverbs.Stranded:
		// THE FOUR RETIRED READ-ONLY AUDITS (2026-10-30, task 01a18799), at the
		// ONE place that would have run them: a sentence naming what each did
		// and where its reader goes instead (Rule 15 — a removal must explain
		// itself), never the generic usage dump and never a silently-different
		// reader. The exit is non-zero because nothing was measured: a script
		// that gated on `branch-refs`/`orphans`/`stranded` must hear "this
		// surface is gone", not read a clean status as "no defects".
		//
		// `reopen` is NOT in this label set any more: master 545b5b2 deleted the
		// verb outright (its mechanism moved to `session resume` / `task clone`),
		// so the master side of this rebase carries no Reopen constant and no
		// runTaskReopen — the rebase union keeps the four audits' removal and
		// drops the reissue case master already retired.
		fmt.Fprintf(os.Stderr, "gopi task %s: removed — %s\n", verb, taskRemovedAdvice(verb))
		os.Exit(1)
	case taskverbs.Clone:
		runTaskClone(opts)
	case taskverbs.Adopt:
		runTaskAdopt(opts.repo, opts.submit, opts.master)
	case taskverbs.Archive:
		// THE BRANCH ARCHIVE (epic verb-restructure, 2026-10-05). The parser
		// sent this arm every `task archive <operand>` whose operand is not a
		// row id, so all that is left here is to hand the argv to the re-homed
		// engine: parseArchiveArgs + the one implementation `gopi task archive`
		// also reaches, with the SPELLING the operator typed passed through so
		// its error lines name `gopi task archive`. Both archives exit inside
		// their own runner (the row one on error, the branch one on a refused
		// archive), so this arm never returns to a caller that would print a
		// second status.
		if opts.branchArchive {
			runArchiveSubcommand(taskProgArchive, opts.archiveArgs)
			os.Exit(0) // unreachable for every run shape: each arm above exits
		}
		out, err := tools.TaskArchive(opts.taskID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi task archive error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(out)
	case taskverbs.List:
		// A PURE READ, and now by construction rather than by a flag default.
		// The verb used to be able to request the stale sweep (`--sweep`, a
		// persisted-clock throttle and a re-arm UPDATE behind it); that whole
		// carrier is deleted on 2026-11-05 — no process watches another
		// process for it, and a stale row is READ as stale wherever it is
		// displayed. Measured 2026-09-14 (the reason the default was already
		// pure): with one stale row present a `task list` paid ~45 ms against
		// a ~4 ms render, and paid it in WRITES — an updated_at bump plus a
		// "stale" event on the operator's sink.
		result, err := runTaskList(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi task list error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(result)
	case taskverbs.Show:
		// The show face renders the row's digest plus the READ-ONLY advisory
		// ground line (runTaskShow — epic 01a0bb4d follow-up): the block above
		// is what the row RECORDS, the advisory line is what resolves NOW.
		result, err := runTaskShow(opts)
		// The provenance block of a selector no row records rides the SAME
		// output channel as a digest, and is printed BEFORE the refusal is
		// reported: the exit code stays non-zero (nothing was resolved) while
		// the operator still gets the git reading he asked for (see
		// runTaskShow). An empty result prints nothing, so every other failure
		// mode keeps its silent-stdout shape.
		if result != "" {
			fmt.Print(result)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi task show error: %v\n", err)
			os.Exit(1)
		}
	case taskverbs.Status:
		runTaskStatus(opts)
	case taskverbs.Update:
		// bugfix-task-p1: a status move MAY carry a result digest (--result)
		// — routed to TaskUpdateWithResult so the digest lands in the
		// append-only task_events trail (bug056: summary immutable; the
		// CLI-only TaskUpdate is status-only and would drop the evidence).
		// The verify-loop child uses this to record its verdict evidence.
		// task-update-reason epic: --reason records the operator-visible
		// WHY of the move ('' = the engine stamps the status-specific
		// default — a lazy caller still leaves a meaning). TaskUpdateReason
		// owns both carriers in one call.
		//
		// --repo (epic 01a0d55b) is the REPAIR arm and it runs FIRST: a row
		// whose deliverable.repo names a dead directory cannot be trusted to
		// transition correctly (and the operator's whole reason for typing the
		// command is to make the row runnable again). Alone it is a pure
		// repair; beside --status it repairs and then transitions.
		//
		// `--status verified` still demands evidence (TaskVerifyEvidenceArgs
		// below), and that is a DIFFERENT thing from the removed gate: this is
		// the OPERATOR's own verb refusing to record a verdict with an empty
		// evidence field. What is gone is the automatic trigger that
		// manufactured a verifier row for every closed bug row — the program
		// judging itself, which the operator ruled out on 2026-10-25
		// ("verify 是由测试程序来做的，不是 gopi 内部的逻辑，一个程序不能自己检验自己").
		// A human or a test program may still record a verdict here; nothing
		// enqueues one on its own.
		if opts.repo != "" {
			repair, rerr := tools.TaskRepointDeliverableRepo(opts.taskID, opts.repo)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "gopi task update error: %v\n", rerr)
				os.Exit(1)
			}
			fmt.Println(repair)
		}
		// --payload (epic 01a0fea1 item [2/5]) is the REVISION arm: it
		// replaces the row's briefing and records the old value in the
		// append-only task_events trail (old/new + sha256s + the required
		// --reason) BEFORE the column moves. It runs AFTER the repo repair
		// above and BEFORE the status transition, so an invocation that
		// carries all three does exactly what it says in the order it says
		// it, and a failure in the revision leaves the status untouched (a
		// refused edit must not consume a transition).
		//
		// The engine refuses a blank payload and a missing --reason by name;
		// both refusals exit non-zero and neither is pre-empted here (one
		// definition, one message — Rule 14).
		if opts.payloadSet {
			revised, rerr := tools.TaskSetPayload(opts.taskID, opts.payload, opts.reason)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "gopi task update error: %v\n", rerr)
				os.Exit(1)
			}
			fmt.Println(revised)
		}
		// --parent-task-id ID / --clear-parent (Rule 33: the EDGE becomes
		// movable) is the third arm, running after the briefing revision and
		// BEFORE the status transition — the same order contract, so a refused
		// reparent leaves the status untouched and a successful one can be
		// followed by the transition it was meant to enable in ONE invocation.
		//
		// Both arms are one engine call: clearing passes an empty target, so
		// "set the edge" and "remove the edge" cannot drift apart in the
		// ledger's validation (existence / self-reference / cycle / reason are
		// decided in ONE place). The engine refuses everything that would make
		// the edge unnavigable, and every refusal exits non-zero with nothing
		// written.
		if opts.parentSet || opts.clearParent {
			// The WHOLE typed set replaces the row's parent set, in typed order
			// (parent 1 first, so an older binary on a peer still sees it).
			// --clear-parent passes the empty list — clearing is now "clear the
			// LIST", which is what a row with several parents needs it to mean.
			var targets []string
			if !opts.clearParent {
				targets = opts.parentTaskIDs
			}
			moved, rerr := tools.TaskReparentParents(opts.taskID, targets, opts.reason)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "gopi task update error: %v\n", rerr)
				os.Exit(1)
			}
			fmt.Println(moved)
		}
		// --repair-session (epic 01a17df9 ③) is the PROVENANCE pass: it repairs
		// columns written before the write source was gated. It runs after the
		// other edit arms and before the transition for the same order contract
		// they follow — a repair is an EDIT, not a status move, and a caller who
		// typed both gets both in the order typed. DRY by default: rewriting a
		// column from a provenance judgement is not something to do for someone
		// who did not type --apply.
		if opts.repairSession {
			rep, rerr := tools.TaskRepairSessionIDs(opts.taskID, opts.repairApply)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "gopi task update error: %v\n", rerr)
				os.Exit(1)
			}
			for _, line := range rep.Lines() {
				fmt.Println(line)
			}
		}
		if opts.statusOmitted {
			return
		}
		if opts.status == tools.TaskStatusVerified && tools.TaskVerifyEvidenceArgs(opts.result) == "" {
			fmt.Fprintf(os.Stderr, "gopi task update error: %s\n", tools.TaskVerifyEvidenceError())
			os.Exit(1)
		}
		var result string
		// ONE call for both shapes: TaskUpdateReasonForced takes the reason and
		// the digest as separate, possibly-empty values, and the delivery-proof
		// escape as the third input. Passing the flag HERE (and only here) is
		// what keeps it a `task update` door — the harness writeback, the
		// merger's executor and every other entry point cannot inherit a
		// release nobody typed (the task-lifecycle tick that used to be the
		// third example was deleted 2026-10-30; the property it illustrates
		// is unchanged).
		result, err = tools.TaskUpdateReasonForced(opts.taskID, opts.status, opts.reason, opts.result, opts.force)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi task update error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(result)
	case taskverbs.Search:
		result, err := tools.TaskSearch(opts.query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi task search error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(result)
	case taskverbs.MergedCleanup:
		runTaskMergedCleanup()
	case taskverbs.SweepZombies:
		// The retired spelling, at the ONE place that would have run it: a
		// sentence naming where the function went (Rule 15 — a removal must
		// explain itself), never the generic usage dump.
		fmt.Fprintf(os.Stderr, "gopi task %s: removed — %s\n", taskverbs.SweepZombies, taskRemovedAdvice(taskverbs.SweepZombies))
		os.Exit(1)
	case taskverbs.FinishCleanup:
		// The retired spelling, at the ONE place that would have run it: a
		// sentence naming where the function went (Rule 15 — a removal must
		// explain itself), never the generic usage dump.
		fmt.Fprintf(os.Stderr, "gopi task %s: removed — %s\n", taskverbs.FinishCleanup, taskRemovedAdvice(taskverbs.FinishCleanup))
		os.Exit(1)
	case taskverbs.SweepWorktrees:
		runTaskSweepWorktrees(opts)
	case taskverbs.Improvements:
		// The agency miner's report-only face.
		runTaskImprovements()
	case taskverbs.AuditMerges:
		runTaskAuditMerges(opts)
	case taskverbs.DedupScan:
		// Re-homed from `gopi memory memsync --dedup-scan` (epic cli-help-dedup):
		// the twin-task audit is a ledger maintenance read, so it answers to
		// the ledger's verb. Same engine call, same output, same exit 2.
		runTaskDedupScan(opts.flags, opts.db)
	case taskverbs.MergeExecute:
		runTaskMergeExecute(opts)
	case taskverbs.Serve, taskverbs.Merger:
		// THE THIRD ENTRY POINT (2026-09-19, devdoc/merger-retired.md): the two
		// DEPRECATED aliases of the resident merger daemon, reached with
		// argv[0]="task" so main.go's isMergerSubcommand never sees them. Both
		// spellings must be refused BY NAME, and the refusal must be the LAST
		// word — a case that fell through instead would leave the process with
		// nothing to do and exit 0, which is the silent no-op this retirement
		// exists to prevent (Rule 15). retireMergerFace exits non-zero.
		//
		// The `-h` half is closed above, in the help scan: a usage query must
		// reach the same sentence, never the family blob.
		retireMergerFace(verb)
	case taskverbs.Merge:
		runTaskMerge(opts)
	}
}

// runTaskImprovements implements `gopi task improvements`: run the agency
// miner over the ledger in REPORT-ONLY mode and print the candidate patterns.
// It writes NOTHING — the improvement-proposal write path is the task-end
// hook gated by GOPI_AGENCY, and this verb is how an operator (or the
// historical-replay harness) sees what that pass would propose.
func runTaskImprovements() {
	res, err := tools.TaskImprovementReport(config.AgencyMinFamilySize(agencyscan.MinTaskFamilySize))
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task improvements error: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(agencyscan.RenderReport(res))
}

// taskAuditMergesTable renders the report-only audit as a stable-text table
// (one line per branch; grep-friendly for e2e assertions).
func taskAuditMergesTable(branches []tools.MergeBacklogBranch) string {
	if len(branches) == 0 {
		return "no unmerged branches (clean)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-40s %6s %10s %s\n", "BRANCH", "AHEAD", "TIP", "DATE")
	for _, br := range branches {
		fmt.Fprintf(&b, "%-40s %6d %10s %s\n", br.Name, br.AheadCount, br.LastCommit, br.LastDate)
	}
	return strings.TrimRight(b.String(), "\n")
}

// runTaskAuditMerges implements `gopi task audit-merges` (foresee-review):
// report the branch backlog; --submit enqueues the merge tasks.
func runTaskAuditMerges(opts *tasksOptions) {
	counts, err := tools.AuditMergeBacklog(tools.MergeBacklogOptions{
		Repo:   opts.repo,
		DBPath: opts.db,
		Submit: opts.submitAudit,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task audit-merges error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("merge-backlog audit (%s): scanned=%d submitted=%d duped=%d skipped=%d failed=%d\n",
		map[bool]string{false: "report-only", true: "submit"}[opts.submitAudit],
		counts.Scanned, counts.Submitted, counts.Duped, counts.Skipped, len(counts.Failed))
	if len(counts.Branches) > 0 {
		fmt.Print(taskAuditMergesTable(counts.Branches))
		fmt.Println()
	}
	for _, e := range counts.Failed {
		fmt.Fprintf(os.Stderr, "  failed: %s\n", e)
	}
}

// runTaskMergeExecute implements `gopi task merge-execute <task-id>`
// (epic tasks-merge-executor-live): run the guarded merge for ONE pending
// merge-backlog row. Prints the executor report; exit 0 on a handled run
// (completed/failed/skipped all count as handled), 1 on engine errors.
func runTaskMergeExecute(opts *tasksOptions) {
	// Single-instance guard: this face lands ONE merge-backlog row's branch
	// onto master (epic merger-always-flock).
	defer mergerWorkLock(mergeExecuteFaceProg)()
	rep, err := tools.TaskMergeBacklogExecute(opts.taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task merge-execute error: %v\n", err)
		os.Exit(1)
	}
	// Close loop (epic merger-always-rebuilds): this face merges a branch into
	// master, so it owes the same rebuild the merger pass and `gopi task merge`
	// owe — it was the ONE landing path with neither a rebuild nor a version
	// name. The engine has returned by here (the guarded merge ran inside
	// TaskMergeBacklogExecute), so the ordering the operator asked for holds:
	// the rebuild follows the child's success/failure return. Only a COMPLETED
	// run advanced master; failed/skipped rows leave the repo where it was.
	if tools.MergeExecLanded(rep) {
		mergerCloseLoopAfterLanding(rep.Repo)
	}
	fmt.Printf("merge-execute %s: status=%s", rep.TaskID, rep.Status)
	if rep.Branch != "" {
		fmt.Printf(" branch=%s", rep.Branch)
	}
	if rep.Repo != "" {
		fmt.Printf(" repo=%s", rep.Repo)
	}
	if rep.Reason != "" {
		fmt.Printf(" reason=%q", rep.Reason)
	}
	fmt.Println()
	for _, s := range rep.Steps {
		mark := "ok"
		if !s.OK {
			mark = "FAIL"
		}
		fmt.Printf("  [%s] %s %s\n", mark, s.Step, s.Detail)
	}
}
