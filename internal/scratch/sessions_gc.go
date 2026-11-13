package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The SESSIONS arm of the reclaim surface — `gopi session gc`.
//
// THE DEFECT THIS CLOSES (measured 2026-10-29, host nick-sager, the bug row this
// file answers): `gopi session` carried EIGHT verbs and every one of them was a
// READER. Nothing could reclaim a session directory, so the tree grew to 90 GB /
// 3175 directories on a `/` partition at 98% (48 G free), and the only exit was
// an operator's hand-run `rm -rf` — a destructive operation outside gopi's own
// machinery, which Rule 42 forbids an agent from performing and which leaves no
// record of what was lost.
//
// WHY IT IS A THIRD PREDICATE IN THIS PACKAGE, not a new mechanism:
// internal/scratch already owns the ONE cross-process liveness probe
// (ScanHolders: the /proc cwd|exe|fd|maps scan plus the `.holder` registration),
// the ONE staleness rule (ScratchEntryStale), the ONE byte measurement (DirBytes)
// and the ONE lock (AcquireGCLockVerdict). A sessions arm built on anything else
// would be the second liveness probe memory #2632 names as how a live directory
// gets deleted.
//
// THE PREDICATE — four parts, and EVERY unknown is a KEEP:
//
//	reclaimable = OURS   (a session container NAME this system mints — a
//	                      canonical uuid, or one carrying ONE minted ordinal
//	                      suffix `<uuid>-r<N>`/`<uuid>-c<N>`; see
//	                      IsMintedSessionContainerName. The suffix answers the
//	                      SHAPE question only: AGE/LIVENESS/LEDGER still decide
//	                      whether the directory has an owner)
//	              AND AGED (the directory's own mtime is older than the window)
//	              AND UNHELD (no live process stands inside it, no `.holder`
//	                      registration names a live owner, and the /proc pass
//	                      FINISHED — a truncated pass keeps everything)
//	              AND UNREFERENCED (no task ledger row names it, by task id or by
//	                      session id)
//
// The asymmetry is deliberate and is the same one the sandbox arm states: a
// directory that survives one more sweep costs disk; a directory deleted out from
// under a run destroys that run's record, its report and its attachments with no
// way to tell what was lost. So an unreadable /proc, an unparseable ledger, an
// age that cannot be measured and a name this system does not mint all resolve to
// KEEP, and each is REPORTED as its own reason rather than folded into "not
// collectable" (Rule 15: the uncertainty is the reason, never permission).

const (
	// SessionsGCStaleAge is the freshness window: a session container younger
	// than this is never collected, whatever else is true of it.
	//
	// IT IS THE SAME NUMBER AS SessionRawLogStaleAge ON PURPOSE, and the reason
	// is the same measurement: a session directory is what `gopi session log`,
	// `gopi session stats`, `gopi session raw` and `gopi session resume` all
	// address, and the raw-log window was set to 14 days because that is the
	// point past which an old run's VERBATIM record stops being something a
	// human asks for while every current verb keeps working. Reusing the window
	// means the two arms cannot disagree about when a run stopped mattering.
	//
	// It is a DEFAULT, not a hard bound: `gopi session gc --older-than` may ask
	// for a LONGER window (the operator may always be more conservative) and may
	// ask for a shorter one only to SEE it in a dry run — the apply path floors
	// the window at this constant (SessionsGCWindow) for the reason the sandbox
	// arm floors nothing and the scratch-dir arm floors everything: this arm
	// deletes a run's whole record, so its floor is the whole point of it
	// existing.
	SessionsGCStaleAge = 14 * 24 * time.Hour

	// SessionsGCScanLimit caps how many root entries ONE planning pass visits, so
	// a pathological root cannot turn the verb into a hang. It is deliberately
	// NOT ScanLimit (10,000 — the bound for ONE directory of the shared scratch
	// home): the sessions tree measured 3,282 entries on this host and is
	// expected to keep growing, so a bound tuned for the scratch home would
	// silently SKIP the tree (memory #4402's measured trap: a per-directory
	// bound applied to a whole tree reported `bytes=0` while the log said the
	// tree was visited).
	SessionsGCScanLimit = TreeEntryScanLimit
)

// The KEEP reasons. They are constants (Rule 1b) because the CLI's report line,
// the JSON field and the tests all match on them, and a reason typed twice
// drifts into a reason nobody can grep for.
const (
	// SessionsKeepNotOurs: the directory's NAME is not one this system mints for
	// a session container, so it is never a candidate at any age. The sessions
	// root is shared with a handful of other shapes (a probe's evidence
	// directory, a legacy task container); cleaning by LOCATION would be one
	// mistyped root away from deleting something that was never ours.
	SessionsKeepNotOurs = "not a session container name (a session directory is named by a canonical uuid, optionally carrying ONE minted ordinal suffix: -r<N> for a reissue, -c<N> for a clone)"

	// SessionsKeepFresh: inside the freshness window.
	SessionsKeepFresh = "not aged (inside the freshness window)"

	// SessionsKeepHeld: a live process stands inside it (the /proc scan saw it).
	SessionsKeepHeld = "a live process stands inside it"

	// SessionsKeepHolderRecord: a `.holder` registration names a live owner.
	SessionsKeepHolderRecord = "a .holder registration names a live owner"

	// SessionsKeepLedgerRow: a task ledger row names it — by its task id or by
	// its session id. This is the epic's own hard predicate: a container any row
	// resolves through is never touched.
	SessionsKeepLedgerRow = "a task ledger row names it"

	// SessionsKeepSelfCwd: this sweep is running inside it.
	SessionsKeepSelfCwd = "this sweep's own working directory"

	// SessionsKeepUnmeasurable: the age could not be read. Reported as its own
	// reason rather than as "fresh": "younger than the window" and "I could not
	// look" are different facts, and only one of them is a reason to stop
	// looking.
	SessionsKeepUnmeasurable = "age unmeasurable"
)

// SessionsLedgerUnwiredReason is the KEEP reason used when the task-ledger reader
// was never installed. It is a distinct sentence from a ledger that failed to
// READ, because they are different defects: this one is a wiring bug in the
// caller, that one is a fact about the host.
const SessionsLedgerUnwiredReason = "the task ledger reader is not wired — every candidate is kept"

// SessionsLedgerUnreadableReason is the KEEP reason used when the ledger read
// failed. Its text carries the underlying error, appended by the plan.
const SessionsLedgerUnreadableReason = "the task ledger could not be read — every candidate is kept"

// SessionsPlan is ONE session directory's verdict: the collector's REPORTING form.
// The destructive arm and the reporting arm share ONE predicate, so a dry run
// cannot promise a different set from the one an apply collects.
type SessionsPlan struct {
	// Path is the session container directory.
	Path string
	// Name is its base name (the id every reader addresses it by).
	Name string
	// Age is how long it has been untouched by its own mtime.
	Age time.Duration
	// Bytes is its measured size (regular files below it, symlinks not
	// followed). It is measured for KEEPS too, because the operator's question
	// is "what would I get back", and a kept directory's size is the part of the
	// tree this verb is NOT reclaiming.
	Bytes int64
	// Keep is true when the directory must survive.
	//
	// IT IS THE AFFIRMATIVE FIELD ON PURPOSE (Rule 15's zero-value discipline),
	// and the measured reason is in this file's own test history: an earlier
	// draft carried `Keep bool` as the store and read `Collectable()` as
	// `!Keep`, so a ZERO-VALUE SessionsPlan — the value a map miss returns, the
	// value any future caller constructs — read as "collectable". The live test
	// caught it (`TestSessionsGCNotOursKeeps`: a foreign directory looked
	// collectable because its plan row was absent, not because it had been
	// judged). The zero value of a destructive predicate must be the SAFE one,
	// so the field is KEEP and everything unjudged reads as keep.
	Keep bool
	// Reason says why it is kept — or, when Collectable() is true, the evidence
	// that authorised collecting it.
	Reason string
}

// Collectable reports whether the plan's entry may be removed. It is the one
// reading the CLI and the sweep both take, so neither can invert the flag — and
// a plan this system did not judge (the zero value) is NOT collectable.
func (p SessionsPlan) Collectable() bool { return !p.Keep && p.Name != "" }

// SessionsLedgerNamesFunc resolves the set of NAMES a task ledger row claims,
// mapped to a human description of the row ("task 01a1… (completed)") for the
// keep reason. It is a SEAM, not a direct dependency: internal/scratch must stay
// a leaf (internal/tools imports THIS package), so the owner of the ledger
// registers the reader at startup exactly as internal/config registers the
// declared-tree resolvers.
type SessionsLedgerNamesFunc func() (map[string]string, error)

// sessionsLedgerNames is the registered reader, or nil when nothing wired it.
var sessionsLedgerNames SessionsLedgerNamesFunc

// RegisterSessionsLedgerNames installs the ONE task-ledger reader the sessions
// arm consults. A nil argument is ignored (a caller cannot accidentally unwire a
// reader that is already installed).
func RegisterSessionsLedgerNames(fn SessionsLedgerNamesFunc) {
	if fn != nil {
		sessionsLedgerNames = fn
	}
}

// SessionsRootRefusal reports why a path must never be swept as a sessions root.
//
// It is the guard that keeps a mistyped configuration value from turning this arm
// into a delete-anything machine: sweeping happens one level DOWN, so a root that
// resolves to `/`, to the operator's home or to the gopi PROFILE would put every
// directory on the machine inside this arm's jurisdiction. Each of those is
// refused by NAME, the way the sandbox arm refuses them.
func SessionsRootRefusal(root string) error {
	// The emptiness check reads the RAW argument: filepath.Clean("") is ".",
	// so a canonical first would turn "no value configured" into a refusal
	// about the current directory — the wrong sentence for the wrong defect.
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("session GC: the sessions root is empty")
	}
	clean := CanonicalPath(root)
	if clean == "" || clean == "." {
		return fmt.Errorf("session GC: the sessions root %q does not resolve to a directory", root)
	}
	if clean == "/" {
		return fmt.Errorf("session GC: refusing the filesystem root %q as a sessions root", clean)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && CanonicalPath(home) == clean {
		return fmt.Errorf("session GC: refusing the user's home %q as a sessions root", clean)
	}
	if profile := gopiProfileDir(); profile != "" && CanonicalPath(profile) == clean {
		return fmt.Errorf("session GC: refusing the gopi profile %q as a sessions root — that is production state, not a sessions tree", clean)
	}
	return nil
}

// SessionsDirRoot returns the sessions tree this arm judges
// (<gopi dir>/sessions), resolved through the SAME declared-tree resolver the
// `session-raw` row uses — one spelling of the root, not a second one.
func SessionsDirRoot() (string, error) { return SessionRawLogRoot() }

// SessionsGCWindow is the ONE window an APPLY is allowed to judge by: the
// caller's request, floored at SessionsGCStaleAge.
//
// A dry run may ask for a SHORTER window to answer "what would a 1-day sweep
// collect?" (a diagnostic question with no destructive consequence), while an
// apply that could shorten this arm's own window would make the constant a value
// nothing reads. The floor is applied in exactly one place so the two faces of
// the verb cannot disagree about what the window means.
func SessionsGCWindow(requested time.Duration) time.Duration {
	if requested < SessionsGCStaleAge {
		return SessionsGCStaleAge
	}
	return requested
}

// PlanSessions reports the verdict for every directory directly under root. It is
// read-only: no lock, no delete, no directory created.
//
// It returns EVERY directory it looked at — keeps included — because the operator
// asking "what can go, and why not the rest?" needs both halves on one page. A
// plan that listed only candidates would report `0 candidates` identically for
// "nothing aged out" and "everything is protected", which is the ambiguity
// Rule 15 refuses.
//
// THE LEDGER IS A HARD PREREQUISITE when there is anything to judge: without it
// the UNREFERENCED half of the predicate cannot be answered, and an answered
// "unknown" must never read as "unreferenced". So a root with candidates and no
// readable ledger is an ERROR (nothing is collected, the caller reports why), not
// a plan of keeps — with ONE exception, a root holding no session container at
// all, where the question never arises and the verb answers trivially.
func PlanSessions(root string, olderThan time.Duration) ([]SessionsPlan, error) {
	if err := SessionsRootRefusal(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no root: nothing to judge, not an error
		}
		return nil, fmt.Errorf("session GC: read %s: %w", root, err)
	}
	if len(entries) > SessionsGCScanLimit {
		return nil, fmt.Errorf("session GC: %s holds more than %d entries — refusing to sweep an unbounded tree",
			root, SessionsGCScanLimit)
	}
	// THE LEDGER IS CONSULTED ONLY WHEN A CANDIDATE SHAPE EXISTS. A root holding
	// no session container has nothing the ledger could protect, so reading it
	// would be a needless dependency — but the plan is still BUILT and RETURNED,
	// because every directory in it is a KEEP with its own reason and the
	// operator's question ("why is the disk full?") is answered by the keep
	// lines. Returning nil here is what made a foreign directory look
	// collectable to a caller reading the plan as a map (the defect
	// TestSessionsGCNotOursKeeps caught).
	var ledger map[string]string
	if sessionsRootHoldsAContainer(entries) {
		var lerr error
		ledger, lerr = sessionsLedgerIndex()
		if lerr != nil {
			return nil, lerr
		}
	}

	now := time.Now()
	// ONE /proc pass for the whole plan, indexed once (heldSet) — the
	// per-candidate form costs a full process-table walk per directory, the
	// measured trap on this very tree (memory #4402: a per-candidate probe turned
	// a dry run past two minutes; the index made it 3.3s).
	scan := ScanHolders()
	held := heldTreeScan{index: heldSet(scan, root), scan: scan}
	self, _ := os.Getwd() //stylegate:ignore an unavailable cwd leaves self empty, which never matches a candidate and so vetoes nothing (every unknown is a KEEP)

	plans := make([]SessionsPlan, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue // a root-level FILE is not a session container (its own family has its own row)
		}
		path := filepath.Join(root, e.Name())
		p := judgeSessionDir(path, e.Name(), root, now, olderThan, held, ledger, self)
		// DirBytes is best-effort by contract (it sums the regular files it can
		// read and skips the rest), so a measurement it could not complete is a
		// smaller number rather than an error — and the KEEP/DELETE verdict never
		// depends on the size.
		p.Bytes = DirBytes(path)
		plans = append(plans, p)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].Path < plans[j].Path })
	return plans, nil
}

// sessionsRootHoldsAContainer reports whether any entry under the root carries the
// session-container shape — the non-vacuity test PlanSessions uses to decide
// whether the ledger has to be read at all.
func sessionsRootHoldsAContainer(entries []os.DirEntry) bool {
	for _, e := range entries {
		if e.IsDir() && IsMintedSessionContainerName(e.Name()) {
			return true
		}
	}
	return false
}

// sessionsLedgerIndex reads the task ledger's claimed names, converting every
// failure into the one error the caller reports. It is the ONLY place the seam is
// read (Rule 2's single-reader shape).
func sessionsLedgerIndex() (map[string]string, error) {
	if sessionsLedgerNames == nil {
		return nil, fmt.Errorf("session GC: %s", SessionsLedgerUnwiredReason)
	}
	names, err := sessionsLedgerNames()
	if err != nil {
		return nil, fmt.Errorf("session GC: %s: %w", SessionsLedgerUnreadableReason, err)
	}
	if names == nil {
		return nil, fmt.Errorf("session GC: %s: the reader answered no rows and no error", SessionsLedgerUnreadableReason)
	}
	return names, nil
}

// judgeSessionDir applies the four-part predicate to ONE directory under the
// sessions root. The order is the cheap-and-decisive one: name, then age, then
// liveness, then the ledger — with the UNFINISHED-SCAN check before any liveness
// answer is trusted.
func judgeSessionDir(path, name, root string, now time.Time, olderThan time.Duration,
	held heldTreeScan, ledger map[string]string, self string) SessionsPlan {
	p := SessionsPlan{Path: path, Name: name, Keep: true}

	// ⓪ OURS — by NAME, never by location. The root is shared with other shapes
	// (a hand-made evidence directory, a legacy task container) and a name this
	// system does not mint is never a candidate, whatever its age.
	if !IsMintedSessionContainerName(name) {
		p.Reason = SessionsKeepNotOurs
		return p
	}

	// The root itself is refused BEFORE anything else and again at the delete
	// (SweepSessionsDetailed): a root is not a candidate at any age.
	if CanonicalPath(path) == CanonicalPath(root) {
		p.Reason = "the sessions root itself — never a candidate"
		return p
	}
	if self != "" && PathWithin(CanonicalPath(self), CanonicalPath(path)) {
		p.Reason = SessionsKeepSelfCwd
		return p
	}

	// ① AGE — the directory's own mtime. Deliberately the mtime rather than any
	// timestamp inside it: a container that has been USED must look recently used,
	// and its metadata's own record does not move.
	stale, err := ScratchEntryStale(path, now, olderThan)
	if err != nil {
		p.Reason = fmt.Sprintf("%s (%v)", SessionsKeepUnmeasurable, err)
		return p
	}
	if fi, serr := os.Lstat(path); serr == nil {
		p.Age = now.Sub(fi.ModTime())
	}
	if !stale {
		p.Reason = SessionsKeepFresh
		return p
	}

	// ② LIVENESS — and the ORDER of these two questions is the safety property: a
	// /proc pass that stopped at its bound never looked at the tail of the process
	// table, so "no holder found" is MISSING evidence, not a measured negative
	// (Rule 15). The flag is applied before the index is consulted, so a truncated
	// pass can never produce a candidate.
	if !treeScanComplete(held) {
		p.Reason = HolderScanWithheldReason()
		return p
	}
	if heldIn(path, held.index) {
		p.Reason = SessionsKeepHeld
		return p
	}
	// The WRITER-owned half: a `.holder` registration is a claim by the process
	// that made the directory, and it is read from inside the candidate. An
	// unreadable record KEEPS (OwnerKeeps), which is the same asymmetry.
	if !unheldRegistered(path) {
		p.Reason = SessionsKeepHolderRecord
		return p
	}

	// ③ REFERENCED — no task ledger row names it. Asked on the LEDGER's own
	// vocabulary: a row names its run directory by its task id, and it names its
	// log by its session id, so both are asked (a container is reachable through
	// either, and `gopi task show` uses both today).
	if why, ok := ledger[name]; ok {
		p.Reason = fmt.Sprintf("%s (%s)", SessionsKeepLedgerRow, why)
		return p
	}

	// Collectable. The reason names the EVIDENCE, so the line an operator reads
	// says why a directory disappeared rather than merely that it did.
	p.Keep = false
	p.Reason = fmt.Sprintf("aged %s, no live process inside, no task ledger row names it", p.Age.Round(time.Hour))
	return p
}

// SweepSessions removes the collectable session containers under root and reports
// how many were removed and how many bytes came back. It is IDEMPOTENT, BOUNDED
// (one ReadDir, at most SessionsGCScanLimit entries) and it NEVER removes root
// itself — not at any age, not when empty. root is the configured container; a
// sweep that deleted its own root would relocate every future session.
//
// It re-checks every candidate IMMEDIATELY before that candidate's delete. Which
// checks can be re-made cheaply is a measured decision, stated here so a later
// reader does not "strengthen" it into a hang: the ROOT identity, the NAME shape,
// the AGE and the LEDGER membership are one Lstat, one string test and one map
// lookup each — they are re-made per candidate. The LIVENESS question is NOT
// re-made per candidate, because a /proc pass costs one full process-table walk
// (measured 387ms for 55,946 holder paths on this host) and repeating it per
// candidate is O(candidates × processes), the trap memory #4402 records. It is
// instead re-made ONCE for the whole delete batch, after the plan, so the batch is
// judged by a scan younger than the plan itself; a process that starts standing
// inside a directory DURING the batch is caught by the next invocation, and the
// `.holder` registration is re-read per candidate because that read IS cheap.
func SweepSessions(root string, olderThan time.Duration) (removed int, bytes int64, err error) {
	removed, bytes, _, err = SweepSessionsDetailed(root, olderThan)
	return removed, bytes, err
}

// SweepSessionsDetailed is SweepSessions with the LOCK verdict and the report the
// caller keeps (Rule 15: a deferred sweep must not look like an empty one).
func SweepSessionsDetailed(root string, olderThan time.Duration) (removed int, bytes int64, lock GCLockVerdict, err error) {
	plans, err := PlanSessions(root, olderThan)
	if err != nil {
		return 0, 0, GCLockVerdict{}, err
	}
	candidates := 0
	for _, p := range plans {
		if p.Collectable() {
			candidates++
		}
	}
	if candidates == 0 {
		return 0, 0, GCLockVerdict{Outcome: GCLockOutcomeAcquired, Reason: "no collectable session: the lock was not needed"}, nil
	}
	release, v, lerr := AcquireGCLockVerdict(root)
	if lerr != nil {
		return 0, 0, v, fmt.Errorf("session GC: lock %s: %w", root, lerr)
	}
	if !v.Locked {
		// Another sweep owns this tree right now. Not an error — the other sweep is
		// doing the same work (fail-open, like every other arm) — but the verdict
		// says WHO, so the caller's `removed=0` stops being ambiguous.
		return 0, 0, v, nil
	}
	defer release()

	// THE FRESH LIVENESS PASS for the delete batch (see the function header for
	// why it is per batch and not per candidate).
	scan := ScanHolders()
	held := heldTreeScan{index: heldSet(scan, root), scan: scan}
	if !treeScanComplete(held) {
		// The plan was made on a complete pass; this one is not. Deleting on a
		// pass that did not finish is deleting on unmeasured evidence, so the
		// batch is abandoned and SAID.
		fmt.Fprintf(os.Stderr, "scratch: session GC: %s\n", HolderScanWithheldReason())
		return 0, 0, v, nil
	}
	canonRoot := CanonicalPath(root)

	for _, p := range plans {
		if p.Keep {
			continue
		}
		if reason := sessionDeleteRefusal(p, canonRoot, held); reason != "" {
			fmt.Fprintf(os.Stderr, "scratch: session GC kept %s — %s\n", p.Path, reason)
			continue
		}
		size := p.Bytes
		if rerr := os.RemoveAll(p.Path); rerr != nil {
			// Report and continue: one directory that cannot be removed must not
			// abort the sweep of the others (Rule 15 — narrated, not swallowed).
			fmt.Fprintf(os.Stderr, "scratch: session GC could not remove %s: %v\n", p.Path, rerr)
			continue
		}
		removed++
		bytes += size
	}
	return removed, bytes, v, nil
}

// sessionDeleteRefusal is the LAST question asked before one delete: "" means the
// candidate may go. It re-makes every check that is cheap to re-make, which is
// what makes the gap between "planned" and "deleted" harmless.
func sessionDeleteRefusal(p SessionsPlan, canonRoot string, held heldTreeScan) string {
	if CanonicalPath(p.Path) == canonRoot {
		return "the sessions root itself — never a candidate"
	}
	if !IsMintedSessionContainerName(p.Name) {
		return SessionsKeepNotOurs
	}
	stale, err := ScratchEntryStale(p.Path, time.Now(), SessionsGCStaleAge)
	if err != nil {
		return fmt.Sprintf("%s (%v)", SessionsKeepUnmeasurable, err)
	}
	if !stale {
		return SessionsKeepFresh
	}
	if heldIn(p.Path, held.index) {
		return SessionsKeepHeld
	}
	if !unheldRegistered(p.Path) {
		return SessionsKeepHolderRecord
	}
	return ""
}
