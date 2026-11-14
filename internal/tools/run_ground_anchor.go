// run_ground_anchor.go — the RUN-GROUND ANCHOR (epic worktree-reclaim-two-gaps,
// Gap B).
//
// THE GAP. Every reclaim arm keys on a ledger row: the delivery-point producer,
// the dead-holder arm, the ownership gates. A run that grounds itself in a
// worktree WITHOUT a row — the bare-prompt/chat face, whose auto-registered row
// may not exist yet or may end without naming the directory — therefore leaves a
// registration that no arm can attribute. The arms that do fire today fire on
// ABSENCE ("no ledger row names it, no live holder, clean, old enough"), which
// is a guess about a directory nobody ever claimed.
//
// THE FIX is the producer writing the anchor AT THE MOMENT IT TAKES THE
// DIRECTORY, and a sweeper reading that record — the deterministic-lifecycle
// principle (devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md): ownership
// is RECORDED by the owner at acquisition, never reconstructed later from
// /proc absence. This file is both halves: the write side (TaskGroundAnchorRecord,
// called from the two grounding seams) and the read side (RunGroundAnchors +
// RunGroundLiveness), so the record's shape and its reader cannot drift.
//
// WHY THE RECORD IS NOT SPOOFABLE BY THE /proc SHAPES THE OTHER ARM HANDLES.
// The anchor stores the taker's OWN identity — pid, start-ticks (the kernel's
// incarnation number, never reused for a pid) and session id — so a reader asks
// "is the run that took this directory still here?" and gets an answer that
// survives every way a process can change shape:
//
//	reparenting (ppid → 1)   the identity fields do not change with the parent,
//	                         so the anchor still names the taker exactly.
//	a recycled pid           start-ticks differ, so the pair cannot match.
//	a daemonised taker       the session is still alive and the incarnation is
//	                         still listed, so the run reads as LIVE.
//	an UNREADABLE /proc      the reader answers LIVE (unknown is never dead —
//	                         Rule 15a, memory #2633).
//
// Every unknown errs toward KEEPING the directory. What the collector may take
// is only what the record states outright.
package tools

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// runGroundAnchorEvidenceFmt is the anchor's one-line evidence sentence —
	// what the taker was, in words, for a reader that is not the classifier.
	runGroundAnchorEvidenceFmt = "ground taken by pid %s (start-ticks %s, session %s)"
	// runGroundAnchorUnrecorded is the value the anchor stores for an identity
	// part it could not read. It is an explicit word, never an empty string, so
	// a record that MEASURED nothing is distinguishable from one that never
	// tried — and both are read as LIVE by the classifier.
	runGroundAnchorUnrecorded = "unrecorded"
)

// TaskGroundAnchorRecord writes the run-ground anchor for a directory a run has
// just TAKEN — the producer half of Gap B.
//
// It must be called at ACQUISITION, from the seam that takes the directory, and
// never later: an anchor written when the run already stands in the directory is
// still evidence (the taker's own identity is captured live), but one written by
// someone else's later sweep would be the very reconstruction this design
// replaces.
//
// One line per GROUND, not per call: a seam that fires once per bash round (the
// creation-time detection seams do) would otherwise append hundreds of identical
// lines for one directory. The dedup is a read of the records already there —
// the directory is the key, and the FIRST anchor for a directory is the one that
// names its taker.
func TaskGroundAnchorRecord(taskID, worktree, branch string) {
	dir := strings.TrimSpace(worktree)
	if dir == "" {
		return
	}
	if runGroundAnchorExists(dir) {
		return // this ground is already anchored — one line per ground
	}
	repo := TaskDeliverableRepoFor(taskID)
	pid := os.Getpid()
	pidStr := strconv.Itoa(pid)
	ticks := runGroundAnchorUnrecorded
	if got, ok := procname.StartTicks(procRootName, pidStr); ok && strings.TrimSpace(got) != "" {
		ticks = strings.TrimSpace(got)
	}
	sid := runGroundAnchorUnrecorded
	if got, ok := procname.ProcSessionKey(procRootName, pidStr); ok && got > 0 {
		sid = strconv.Itoa(got)
	}
	evidence := "ground taken by pid " + pidStr + " (start-ticks " + ticks + ", session " + sid + ")"
	removalaudit.WorktreeGround(removalaudit.ArmTaskGroundAnchor, repo, dir,
		strings.TrimSpace(branch), taskIDNormalize(taskID), pidStr, ticks, sid, evidence)
}

// TaskGroundAnchorPath is the sink the anchors share with the removal records —
// ONE path for the package's history (Rule 2/14), honouring the same env
// override the sink itself honours.
func TaskGroundAnchorPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(removalaudit.EnvSink)); override != "" {
		return override, nil
	}
	return removalaudit.DefaultPath()
}

// runGroundAnchorExists reports whether a directory already has an anchor.
// A read failure answers FALSE — writing a second anchor for a directory that
// may already have one is the safe direction (an extra line is a cosmetic
// cost; a MISSING anchor loses the ground's whole record).
func runGroundAnchorExists(dir string) bool {
	path, err := TaskGroundAnchorPath()
	if err != nil {
		return false
	}
	anchors, err := RunGroundAnchors(path)
	if err != nil {
		return false
	}
	for _, a := range anchors {
		if sameWorkingPath(a.Worktree, dir) {
			return true
		}
	}
	return false
}

// RunGroundAnchor is one anchored ground, as read back from the sink.
type RunGroundAnchor struct {
	Worktree   string
	Branch     string
	TaskID     string
	Repo       string
	PID        string
	StartTicks string
	Session    string
	At         time.Time
	// Arm is the emitting arm the record carries (ArmTaskGroundAnchor for an
	// ACQUISITION, ArmTaskGroundAnchorBackfill for a backfill). It is kept so a
	// reader can tell the two KINDS of record apart without re-deriving it from
	// the identity fields — see Backfilled.
	Arm string
	// Backfilled marks an anchor written by run_ground_adopt.go: a record of a
	// MEASUREMENT (nobody stands in the ground, the tree is clean, the work is
	// delivered), never of a taker. It is DERIVED from Arm here so the two
	// readers of that fact cannot disagree (Rule 13) — the writer passes the arm,
	// the ladder asks this flag.
	Backfilled bool
}

// RunGroundAnchors reads every run-ground anchor in the sink at path, newest
// record last (file order). A missing file is not an error — it means no run has
// ever anchored a ground, which is a legitimate state and not a failure.
func RunGroundAnchors(path string) ([]RunGroundAnchor, error) {
	recs, err := removalaudit.Records(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []RunGroundAnchor
	for _, r := range recs {
		if r.Kind != removalaudit.KindWorktreeGround {
			continue
		}
		out = append(out, RunGroundAnchor{
			Worktree:   r.Target,
			Branch:     r.Branch,
			TaskID:     r.TaskID,
			Repo:       r.Repo,
			PID:        r.HolderPID,
			StartTicks: r.HolderStart,
			Session:    r.HolderSID,
			At:         runGroundAnchorTime(r.TS),
			Arm:        r.Arm,
			Backfilled: r.Arm == string(removalaudit.ArmTaskGroundAnchorBackfill),
		})
	}
	return out, nil
}

// runGroundAnchorTime parses a record's stamp; an unparseable one is the zero
// time, which the age floor reads as "unmeasurable" and therefore KEEPS.
func runGroundAnchorTime(ts string) time.Time {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(ts))
	if err != nil {
		return time.Time{}
	}
	return t
}

// RunGroundLiveness is the verdict on one anchored ground, with the reason that
// reached it — the reason is the deliverable, not a debug aid (Rule 15: the
// operator must be able to see WHY a directory was kept or collected).
type RunGroundLiveness struct {
	Anchor     RunGroundAnchor
	Live       bool
	Reason     string
	Unmeasurbl bool // the identity could not be measured at all
	// Gone records that the directory the anchor names is not on disk — the
	// third outcome of the anchor axis, neither collectable nor kept: there is
	// nothing there to free and nothing there to protect. It is set by
	// runGroundMeasureGround's answer, the SAME criterion the removal leg reads,
	// so the plan a dry run prints and the act an apply performs cannot disagree
	// about whether a ground exists. A Gone ground is never Live-collectable and
	// never removed; it is reported so the count reconciles.
	Gone bool
}

// RunGroundAnchorLiveness classifies ONE anchor: may this ground be released?
// Two INDEPENDENT facts are asked, and the answer is LIVE unless BOTH of them
// say the working life is over:
//
//	LAUNCH  runGroundLaunchLiveness — is the recorded launch context (the
//	        incarnation, then the session) still alive? Ordered so every
//	        uncertain rung KEEPS (unknown is never dead — Rule 15a).
//	LEDGER  the row the anchor NAMED (RunGroundAnchor.TaskID) — does the ledger
//	        say that row's life is over (worktreeSweepRowLifeEnded: a terminal
//	        status, or a live status whose ended_at is past the enqueue grace)?
//
// WHY THE LEDGER IS NOT OPTIONAL (measured 2026-10-07, task 01a1151f; the
// defect this rung fixes). A single fact is not enough, and the launch ladder
// alone is not merely weak — it is WRONG in the common shape. The taker recorded
// at claim time is the process that CLAIMED the row, and in this fleet that is a
// long-lived `gopi worker` loop: measured pid 3577762 (start 07:53:02, alive for
// hours, cgroup session-1174.scope) claimed the 08:45 row 01a113ac, the 12:23
// row 01a11494 and the 14:18 row 01a114f3 — three different runs of three
// different tasks, one pid. So "the recorded incarnation is alive" is a fact
// about the WORKER, never about the ground: every ground that worker ever
// claimed stayed LIVE for as long as the worker lived, long after its own run
// had ended (01a113ac failed, 01a11494 completed — both still kept). The
// reason string said "the run that took this ground is still here", which was
// simply false.
//
// The ledger rung is the independent second fact (Rule 24's principle one layer
// up: judge by what the record measures, not by a proxy that happens to be
// alive). It RELAXES a LIVE launch verdict only when the named row's life is
// PROVABLY over — a terminal status, or a live status whose own ended_at stamp
// is past the enqueue window. Everything else KEEPS, one direction only:
// unreadable ledger, absent row, unnamed row, live row → keep, exactly as
// before. The classification itself is NOT re-implemented here: the "the
// ledger's own record says this run is over" predicate is
// worktreeSweepRowLifeEnded, the same one the registration sweep's rungs ask
// (Rule 14).
//
// The DIRT question is not this ladder's either: the removal arm shells `git
// worktree remove` WITHOUT --force, so a worktree holding uncommitted work is
// refused by git itself (the skip names git's own sentence) — Rule 30's line is
// kept by the tool that owns the files, not by a second porcelain parser here.
func RunGroundAnchorLiveness(a RunGroundAnchor) RunGroundLiveness {
	lookup := newGroundRowLookup()
	defer lookup.Close()
	return runGroundAnchorLiveness(a, lookup)
}

// runGroundAnchorLiveness is the ONE body both entry points share (Rules 13/14):
// the exported classifier opens its own ledger read, while
// RunGroundCollectableGrounds hands in a lookup it opened ONCE for a whole scan
// (a sweep walks thousands of anchors; one open per anchor would be a
// per-anchor schema check for no extra information).
func runGroundAnchorLiveness(a RunGroundAnchor, lookup *groundRowLookup) RunGroundLiveness {
	verdict := runGroundBackfillOrLaunchLiveness(a)
	if verdict.Live {
		over, why := lookup.rowLifeEnded(a.TaskID)
		if !over {
			return verdict
		}
		verdict.Live = false
		verdict.Unmeasurbl = false
		verdict.Reason = why
	}
	// THE UNDELIVERED-COMMITS RUNG (worktree_sweep_undelivered.go) — the LAST
	// question before this axis calls a ground collectable, asked on BOTH ways of
	// reaching that answer, because neither of them can ask it:
	//
	//	· the LAUNCH ladder answers "is the taker gone?", a statement about a
	//	  process — it returned COLLECTABLE for /home/user/workspace/01a163df-…
	//	  on this host (measured 2026-10-22), having never looked at the tree;
	//	· the LEDGER rung answers "is the row over?", a statement about a record.
	//
	// NEITHER says the work landed: a ground whose branch was never pushed and
	// whose merge never happened carries its commits in the one place the removal
	// deletes. The rung is deliberately placed HERE (after both, before the
	// caller's default: collectable), so a future rung on this ladder cannot be
	// added in front of it by accident — the collection decision has exactly one
	// exit, and this is it.
	//
	// The probe itself decides its own scope: it answers "" for a LINKED worktree
	// (whose branch and objects survive `worktree remove` in the main repo) and
	// only judges a CLONE-CHANNEL ground, where the directory IS the repository.
	if why := worktreeSweepUndeliveredRefusal(a.Worktree, a.Repo, a.Branch); why != "" {
		verdict.Live = true
		verdict.Unmeasurbl = false
		verdict.Reason = why
		return verdict
	}
	return verdict
}

// runGroundPidIsGopi answers "is the process wearing this pid THIS PROGRAM?"
// through the ONE existing identity predicate (procname.IsGopiImage — image, then
// argv, then comm), never through a second implementation (Rules 13/14).
//
// A pid that is not alive, or whose identity cannot be measured at all, answers
// FALSE here — deliberately, and this is the one place where "unknown" is not
// read as "alive": this function's only job is to stop rung ① from calling a bare
// pid EXISTENCE a live taker. Every caller that may RELAX something runs this
// answer only as "the incarnation is not vouchsafed", after which the ladder keeps
// descending to the session rung and the ledger rung — both of which can only
// keep the ground. So a false here never frees anything by itself; it removes a
// proxy (bug 01a17d00: an unrelated number was read as a live gopi).
func runGroundPidIsGopi(pid string) bool {
	if n, err := strconv.Atoi(strings.TrimSpace(pid)); err != nil || n <= 0 || !pidAlive(n) {
		return false
	}
	return finishCleanupProcRunsGopi(pid)
}

// finishCleanupProcRunsGopi is the ONE predicate answering "does this process run
// the gopi PROGRAM?" for the process-identity callers in this package: the
// kernel's image link and the process's own argv (procname.IsGopiImage) UNION the
// kernel's comm field.
//
// WHY A UNION AND NOT THE IMAGE ALONE (Rule 41, bug 01a131fc's family, and the
// fixture shape every test here produces): a re-exec'd binary reached through a
// symlink named `gopi` has comm == "gopi" while its /proc/<pid>/exe resolves to
// the REAL file, whose base name is not `gopi` — the image channel answers "not
// gopi" and the process is one. Counting a candidate on either channel can only
// ever KEEP more, never release more, so the union is the safe direction here
// (Rule 15a): the census built on it decides "is something still holding this?",
// and being wrong on the "still holding" side costs a kept directory, not lost
// work.
func finishCleanupProcRunsGopi(pid string) bool {
	if strings.TrimSpace(pid) == "" {
		return false
	}
	comm := finishCleanupProcCommand(pid)
	if comm == procname.NameGopi {
		return true
	}
	argv, _ := procname.Cmdline(procRootName, pid)
	argv0 := ""
	if len(argv) > 0 {
		argv0 = argv[0]
	}
	isGopi, _ := procname.IsGopiImage(procRootName, pid, argv0, comm)
	return isGopi
}

// runGroundAttributionDirs is the directory set a launch-session census must
// find a live gopi process INSIDE for the ground to stay held (one definition of
// "in this ground", reused by the census's candidate test — Rules 13/14).
//
// The ground itself plus its resolved spelling: a run that chdir'd through a
// symlinked path would otherwise never match, and a MISSING directory is not an
// error here — the anchor axis has its own GONE rung for that shape, and an empty
// set is handled by the caller as the unattributed question.
func runGroundAttributionDirs(worktree string) []string {
	dir := strings.TrimSpace(worktree)
	if dir == "" {
		return nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = filepath.Clean(dir)
	}
	dirs := []string{abs}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil && resolved != abs {
		dirs = append(dirs, resolved)
	}
	return dirs
}

// runGroundBackfillOrLaunchLiveness picks WHICH identity question this record can
// be asked, and the choice is a fact about the record, not a preference:
//
//	· an ACQUISITION anchor (TaskGroundAnchorRecord) carries the taker's own pid,
//	  start-ticks and session, so the LAUNCH ladder can ask "is that incarnation
//	  still alive?" — that is runGroundLaunchLiveness, below.
//	· a BACKFILL anchor (run_ground_adopt.go) carries runGroundAnchorUnrecorded
//	  in all three, because no taker was ever recorded and inventing one is the
//	  reconstruction the anchor design forbids. The launch ladder's first rung
//	  would read `unrecorded` as "no identity to measure" and answer LIVE
//	  (unknown is never dead), keeping the ground forever — which is exactly the
//	  population this leg exists to rescue. So the backfilled record is asked its
//	  OWN question, and the rung is NAMED in the reason so an operator reading a
//	  collectable line knows why a ground with no live owner is being freed.
//
// NEITHER BRANCH IS A WEAKENING. Every rung that decides whether a directory may
// actually be freed still runs AFTER this one and unchanged: the LEDGER rung (a
// row whose life is over), the age floor, the occupancy guard, and the removal
// leg's own three clone rungs (origin, clean, delivered).
func runGroundBackfillOrLaunchLiveness(a RunGroundAnchor) RunGroundLiveness {
	if a.Backfilled {
		return RunGroundLiveness{Anchor: a, Reason: runGroundAdoptTakerReason}
	}
	return runGroundLaunchLiveness(a)
}

// runGroundLaunchLiveness asks the LAUNCH ladder alone: is the recorded launch
// context (incarnation → session) still alive? Ordered so that every uncertain
// rung KEEPS the directory (unknown is never dead — Rule 15a):
//
//	① the recorded INCARNATION is alive            → LIVE (that pid, that start)
//	② the recorded SESSION is alive                → LIVE if any live gopi
//	                                                 process is in it; otherwise
//	                                                 the taker reparented and
//	                                                 only a bystander session is
//	                                                 left → COLLECTABLE
//	③ the session is gone and the incarnation is
//	  gone                                         → COLLECTABLE
//	④ nothing measurable                           → LIVE (unmeasurable)
//
// A LIVE verdict here is a statement about the LAUNCH CONTEXT, never about the
// run that took the ground — that is the distinction the ledger rung above
// exists for, and the reason strings below say so in the operator's own reading
// (a false sentence on an operator's screen is Rule 15's silent failure,
// inverted).
func runGroundLaunchLiveness(a RunGroundAnchor) RunGroundLiveness {
	verdict := RunGroundLiveness{Anchor: a}
	pid, err := strconv.Atoi(strings.TrimSpace(a.PID))
	if err != nil || pid <= 0 {
		verdict.Live = true
		verdict.Unmeasurbl = true
		verdict.Reason = "the anchor records no usable pid — nothing to measure, keeping the ground"
		return verdict
	}
	// ① the INCARNATION: the recorded start-ticks must still describe this pid.
	// A mismatch means the pid was recycled (or the process exited and the number
	// was handed to another) — the taker is gone, whatever now wears the pid.
	incarnationLive := false
	if ticks := strings.TrimSpace(a.StartTicks); ticks != "" && ticks != runGroundAnchorUnrecorded {
		if now, ok := procname.StartTicks(procRootName, strconv.Itoa(pid)); ok {
			incarnationLive = strings.TrimSpace(now) == ticks
		}
	} else if pidAlive(pid) {
		// No incarnation recorded: fall back to the pid alone, which can only
		// ever say "something with this number is alive" — so it may prove
		// LIFE (rung ①) but never DEATH. And "something" must be THIS PROGRAM:
		// a bare pid existence check is a proxy, and the measured cost of that
		// proxy is bug 01a17d00 — the launch number was being read off a
		// session, and a recycled/unrelated number would have kept a finished
		// ground forever. The image/comm/argv channels are the existing single
		// implementation of "is this the gopi program" (procname.IsGopiImage);
		// an unmeasurable answer is NOT "not gopi" for the memory question, but
		// here it falls through to rung ②, which is the conservative rung.
		incarnationLive = runGroundPidIsGopi(strconv.Itoa(pid))
	}
	if incarnationLive && pidAlive(pid) {
		verdict.Live = true
		// The sentence names the LAUNCH, not the run: the recorded pid is the
		// claim-time taker, and in this fleet that is a long-lived worker loop
		// that claims row after row (see RunGroundAnchorLiveness) — so claiming
		// "the run is still here" from it would be a statement the measurement
		// does not support.
		verdict.Reason = "the launch context that took this ground (pid " + a.PID + ") is still alive"
		return verdict
	}
	// ② the SESSION: the kernel keeps a session id alive while its leader is
	// dead, so a live session is a real fact about the launch — but a live
	// session holding no gopi process is only a leftover shell.
	sid, sidErr := strconv.Atoi(strings.TrimSpace(a.Session))
	if sidErr == nil && sid > 0 {
		if pidAlive(sid) {
			// The questioner is excluded: the sweep runs inside SOMEBODY's
			// launch session — usually the operator's own — so a census that
			// counted the asker would answer "there is a live gopi here" about
			// any session it was launched from, and never collect anything.
			// THE QUESTION IS ABOUT THIS GROUND, not about the shell session the
			// run was launched from (measured 2026-10-27, bug 01a17d00). The
			// unattributed form asked "does session <sid> still hold a live gopi
			// process?" — and a run launched from an operator's terminal stands in
			// THAT terminal's session, which this host keeps stocked with gopi
			// processes belonging to other grounds. Measured: session 823212 (the
			// operator's shell, comm=bash, alive 22h) held two `gopi worker` rounds
			// while the ground's own taker (pid 2345865, start-ticks 95908227) was
			// long dead — so this rung answered LIVE for a provably finished ground
			// and kept it, together with the row that was stuck `in_progress`.
			//
			// The attribution is not a weakening: a gopi process standing IN the
			// ground (its cwd, its argv, or its image path) still holds it, and the
			// independent occupancy guard (finishCleanupWorktreeInUse) is unchanged
			// — it vetoes any live process in the directory whoever its image is.
			// What the rung stops counting is a bystander in somebody else's shell.
			if finishCleanupSessionHasLiveGopiInExcept(sid, os.Getpid(), runGroundAttributionDirs(a.Worktree)) {
				verdict.Live = true
				verdict.Reason = "the launch session " + a.Session + " still holds a live gopi process standing in this ground"
				return verdict
			}
			verdict.Reason = "the run that took this ground is gone (pid " + a.PID + " wears a different incarnation) and its session " + a.Session + " holds no live gopi process in this ground"
			return verdict
		}
		verdict.Reason = "the run that took this ground is gone (pid " + a.PID + " wears a different incarnation) and its session " + a.Session + " is no longer alive"
		return verdict
	}
	if a.Session == runGroundAnchorUnrecorded || strings.TrimSpace(a.Session) == "" {
		verdict.Live = true
		verdict.Unmeasurbl = true
		verdict.Reason = "the anchor records no session and the taker's incarnation is gone — keeping the ground (the launch cannot be measured)"
		return verdict
	}
	verdict.Reason = "the run that took this ground is gone (pid " + a.PID + " wears a different incarnation)"
	return verdict
}

// groundRowLookup answers the LEDGER rung for a whole scan: given an anchor's
// recorded task id, does the ledger say that row's life is over? It is opened
// ONCE per scan (a sweep walks thousands of anchors) and closed by its opener.
//
// Absence is not a verdict: a lookup whose database could not be opened, a row
// the ledger does not hold, and an anchor that names no row ALL answer "no"
// (the ground is kept). That direction is the whole rule — unknown is never
// dead (Rule 15a) — and it is also what keeps the arm honest about histories it
// cannot see (a foreign ledger, an anchor written by an older build).
type groundRowLookup struct {
	db     *sql.DB
	closeF func()
	opened bool
	cache  map[string]groundRowVerdict
}

// groundRowVerdict is one row's answer, cached per scan so N anchors naming the
// same row cost one read (the long-lived-worker shape makes that the common
// case: one worker's pid, many rows).
type groundRowVerdict struct {
	over bool
	why  string
}

// newGroundRowLookup returns a lookup whose ledger handle is opened LAZILY, on
// the first anchor that actually names a row. The laziness is not an
// optimisation: a scan whose anchors name no row (and the ladder's own unit
// callers) must not touch a ledger at all — an eager open would read whatever
// profile happened to be in force for a question nobody asked.
func newGroundRowLookup() *groundRowLookup {
	return &groundRowLookup{cache: map[string]groundRowVerdict{}}
}

// dbHandle opens the shared ledger read-only on first use; a FAILED open stays
// failed for this lookup (recorded in `opened`), so a busy ledger is not
// re-attempted once per anchor.
func (l *groundRowLookup) dbHandle() *sql.DB {
	if l.db != nil || l.opened {
		return l.db
	}
	l.opened = true
	db, closeF, err := taskSyncDB()
	if err != nil {
		return nil
	}
	l.db, l.closeF = db, closeF
	return l.db
}

// Close releases the ledger handle. A nil receiver and a never-opened lookup
// are both fine (the callers close unconditionally).
func (l *groundRowLookup) Close() {
	if l == nil || l.closeF == nil {
		return
	}
	l.closeF()
	l.closeF = nil
}

// rowLifeEnded answers the ledger rung for one anchor's recorded task id.
func (l *groundRowLookup) rowLifeEnded(taskID string) (bool, string) {
	id := taskIDNormalize(taskID)
	if id == "" {
		// The anchor names no row (the bare-prompt face mints one, but an older
		// record may predate that): there is no ledger fact to ask, and the
		// launch ladder's answer stands untouched.
		return false, ""
	}
	if v, ok := l.cache[id]; ok {
		return v.over, v.why
	}
	v := l.askLedger(id)
	l.cache[id] = v
	return v.over, v.why
}

// askLedger is the ONE read (Rule 14). An absent row or an unreadable ledger
// answers "not over" — see groundRowLookup's doc.
func (l *groundRowLookup) askLedger(id string) groundRowVerdict {
	if l == nil {
		return groundRowVerdict{}
	}
	db := l.dbHandle()
	if db == nil {
		return groundRowVerdict{}
	}
	row, err := taskByID(context.Background(), db, id)
	if err != nil || row == nil {
		return groundRowVerdict{}
	}
	if !worktreeSweepRowLifeEnded(tasksRowForSweep{Status: row.Status, EndedAt: row.EndedAt}, worktreeSweepOptions{Now: time.Now()}) {
		return groundRowVerdict{}
	}
	if taskCleanupEligibleStatus(row.Status) {
		return groundRowVerdict{
			over: true,
			why: "the launch context is alive but the run is over: task " + id +
				" is " + row.Status + " — a claim-time worker outlives the row it claimed, so this ground is the run's leftovers, not a live run",
		}
	}
	// A LIVE status beside a stamped-over life is the record-not-owner shape
	// worktreeSweepRowLifeEnded documents (the enqueue window is the one benign
	// way to produce it, which is what the grace covers).
	return groundRowVerdict{
		over: true,
		why: "the launch context is alive but the run is over: task " + id +
			" is still " + row.Status + " while its life was already stamped over (ended_at past the " +
			worktreeSweepEndedGrace.String() + " enqueue grace)",
	}
}

const (
	// runGroundSweepLineFmt is the read-out's one line shape: the disposition in
	// a fixed-width column, the axis that produced the verdict, the directory,
	// then the reason. One spelling, so a reader (and a later parser) sees the
	// same columns for every disposition.
	runGroundSweepLineFmt = "%s [%s] %s  %s"
	// runGroundSweepHeaderFmt is the scan's summary line: what --apply would
	// free, how many anchors are stale, and how many grounds are deliberately
	// kept. `gone` is its own figure (see RunGroundSweepHeader).
	runGroundSweepHeaderFmt = "run-ground anchors: collectable=%d gone=%d kept=%d"
	// runGroundSweepAxis is the axis label every line of this read-out carries:
	// `sweep-worktrees` prints a SECOND axis above this one (the registration
	// scan), and a line whose origin is unstated is a line the operator has to
	// attribute by eye.
	runGroundSweepAxis = "run-ground-anchor"
	// The three disposition labels, padded to one width so the directories line
	// up in the column an operator scans.
	runGroundSweepCollectableLabel = "collectable"
	runGroundSweepGoneLabel        = "gone       "
	runGroundSweepKeepLabel        = "keep       "
)

// RunGroundCollectableGrounds partitions the anchors: collectable ones (their
// taker is provably gone AND the ground is older than the floor) and the kept
// ones with the reason each was kept.
//
// The age floor is the SAME knob the worktree sweep uses, deliberately: a ground
// a run took seconds ago is not a leak, and one knob for "how long is a directory
// allowed to look abandoned" is one thing to reason about.
//
// A directory that still holds a LIVE process — by the cwd liveness guard, not
// by the anchor — is never collectable: the anchor says who TOOK the ground, the
// liveness guard says who stands in it now, and both must agree before anything
// is released. An UNMEASURABLE age keeps the ground.
func RunGroundCollectableGrounds(now time.Time, minAge time.Duration) (collectable []RunGroundLiveness, kept []RunGroundLiveness, err error) {
	path, err := TaskGroundAnchorPath()
	if err != nil {
		return nil, nil, err
	}
	anchors, err := RunGroundAnchors(path)
	if err != nil {
		return nil, nil, err
	}
	// NEWEST anchor per directory decides: a ground taken twice (a resumed run
	// re-anchoring after its predecessor's record) is judged by its last taker.
	perDir := map[string]RunGroundAnchor{}
	var order []string
	for _, a := range anchors {
		key := strings.TrimSpace(a.Worktree)
		if key == "" {
			continue
		}
		if _, seen := perDir[key]; !seen {
			order = append(order, key)
		}
		perDir[key] = a
	}
	lookup := newGroundRowLookup()
	defer lookup.Close()
	for _, key := range order {
		a := perDir[key]
		// THE PRESENCE RUNG — FIRST, and before the identity ladder, because
		// every question below it is about a directory that would have to exist.
		// The removal leg asks the identical question through the identical
		// function (Rule 14), so the plan a dry run prints and the act an apply
		// performs now agree about the same ground by construction.
		switch presence, why := runGroundMeasureGround(a.Worktree); presence {
		case runGroundGroundGone:
			// Gone stays in the CANDIDATE list, flagged — not moved to `kept`.
			// The removal leg is fed this list, and its first rung already skips a
			// gone directory with this exact sentence (unchanged, byte for byte);
			// moving the entry to another slice would have changed what the ACT
			// receives for no gain, and this change is a report fix. What the flag
			// buys is that the READ-OUT and the COUNT no longer claim it: the
			// disposition line says `gone`, and RunGroundSweepCounts keeps it out
			// of `collectable=`.
			collectable = append(collectable, RunGroundLiveness{Anchor: a, Gone: true, Reason: runGroundCollectGoneReason})
			continue
		case runGroundGroundUnmeasurable:
			kept = append(kept, RunGroundLiveness{
				Anchor:     a,
				Live:       true,
				Unmeasurbl: true,
				Reason:     "kept: " + fmt.Sprintf(runGroundMeasureUnmeasurableFmt, why),
			})
			continue
		}
		v := runGroundAnchorLiveness(a, lookup)
		if v.Live {
			v.Reason = "kept: " + v.Reason
			kept = append(kept, v)
			continue
		}
		// THE AGE RUNG, asked through ONE helper because there are TWO measured
		// ages and they are different facts (Rule 14: one implementation of "how
		// long has this looked abandoned"):
		//
		//	· an ACQUISITION anchor records WHEN THE TAKER TOOK the ground, so
		//	  the record's own stamp is its age;
		//	· a BACKFILL anchor was written NOW for a ground that is already old
		//	  (run_ground_adopt.go), so its stamp says nothing about the ground —
		//	  aging it by the record would park every rescued ground for a whole
		//	  floor, which is the population loss that leg exists to end. Its age
		//	  is the DIRECTORY's, measured.
		aged := runGroundAged(a, now, minAge)
		if !aged.Aged {
			v.Live = true
			v.Reason = "kept: " + aged.Why
			kept = append(kept, v)
			continue
		}
		if runGroundOccupied(a.Worktree) {
			v.Live = true
			v.Reason = "kept: a live process still stands in the ground (cwd/fd/exe)"
			kept = append(kept, v)
			continue
		}
		collectable = append(collectable, v)
	}
	return collectable, kept, nil
}

// runGroundOccupied reports whether a live process still stands in dir — the
// SAME guard every other removal arm consults (Rule 14: one implementation of
// "is anyone in there"), including its orphan-reading input, so a ground held
// only by an orphaned cwd-inheritor is not kept alive by it. It is a thin
// boolean face of TaskGroundInUse (task_ground_in_use.go), so this arm and the
// row-level surfaces (`gopi task status`, the resume/retry gate) read ONE
// criterion rather than two similar ones.
func runGroundOccupied(dir string) bool {
	_, reason := TaskGroundInUse(dir)
	return reason != ""
}

// RunGroundSweepReport renders the operator's read-out for one scan: every
// collectable ground with the evidence that cleared it, then every GONE ground,
// then every kept ground with the reason it was kept. One line per anchor, so a
// dry run is a complete answer rather than a count.
//
// THE GONE LINE IS THE FIX FOR THIS AXIS' ONE JOB. A dry run produces exactly
// this read-out, and the question it must answer is "how many would --apply
// free?". Before this rung the answer was wrong by 98.7% on this host (measured
// 2026-10-27: `collectable=759`, of which 749 directories were not on disk), and
// the removal leg then skipped every one of them at its first rung. The grounds
// are now separated HERE, and printed rather than silently dropped — a number
// that shrinks without saying where it went is the same unhelpfulness in a
// quieter form.
func RunGroundSweepReport(collectable, kept []RunGroundLiveness) []string {
	var lines []string
	for _, v := range collectable {
		if v.Gone {
			continue // printed with the other gone grounds, below
		}
		lines = append(lines, runGroundSweepLine(runGroundSweepCollectableLabel, v))
	}
	for _, v := range collectable {
		if v.Gone {
			lines = append(lines, runGroundSweepLine(runGroundSweepGoneLabel, v))
		}
	}
	for _, v := range kept {
		if v.Gone {
			continue
		}
		lines = append(lines, runGroundSweepLine(runGroundSweepKeepLabel, v))
	}
	return lines
}

// RunGroundSweepCounts is the ONE tally of a scan, so the header line and the
// per-anchor read-out cannot count the same scan differently (Rule 14).
// `collectable` counts only grounds that EXIST — the number an operator reads as
// "what would --apply free" — while `gone` keeps the stale anchors visible as
// their own figure instead of folding them into either side.
//
// The gone grounds travel in the `collectable` SLICE deliberately: the removal
// leg is fed that slice, and its own first rung already skips a gone directory
// with the same sentence, so nothing about the ACT changes with this fix. What
// changes is what the READ-OUT and this tally say about it.
func RunGroundSweepCounts(collectable, kept []RunGroundLiveness) (collectableN, goneN, keptN int) {
	for _, v := range collectable {
		if v.Gone {
			goneN++
			continue
		}
		collectableN++
	}
	keptN = len(kept)
	return collectableN, goneN, keptN
}

// RunGroundSweepHeader renders the scan's summary line from the same tally the
// read-out uses. `gone` stands beside `collectable` on purpose: the two figures
// an operator compares are "what would this free" and "what is a stale anchor",
// and a gone count folded into the kept count would make a scan that is 98.7%
// stale read exactly like a healthy one.
func RunGroundSweepHeader(collectable, kept []RunGroundLiveness) string {
	collectableN, goneN, keptN := RunGroundSweepCounts(collectable, kept)
	return fmt.Sprintf(runGroundSweepHeaderFmt, collectableN, goneN, keptN)
}

// runGroundSweepLine is the one read-out line shape (Rule 1: the column width
// and the axis label are stated once).
func runGroundSweepLine(label string, v RunGroundLiveness) string {
	return fmt.Sprintf(runGroundSweepLineFmt, label, runGroundSweepAxis, v.Anchor.Worktree, v.Reason)
}

// runGroundCollectGoneReason is the sentence for an anchor whose directory no
// longer exists. It is a SKIP and not a failure: there is nothing to remove, and
// reporting it as a removal error is how the old leg turned 679 stale anchors
// into 679 exit-128 lines an operator had to read past.
//
// ONE SENTENCE, TWO LEGS (Rule 14). The report leg and the removal leg both
// render this fact about a ground, and the operator reads both in one run — so
// they must read as one finding, not two similar ones. It lives here, beside the
// measurement that reaches it (runGroundMeasureGround), never copied.
const runGroundCollectGoneReason = "the directory is already gone — nothing to remove (a stale anchor; no removal was attempted and no record was written)"

// runGroundMeasureUnmeasurableFmt is the sentence for an anchor whose directory
// cannot be measured AT ALL (a read error that is not ENOENT — a permission
// boundary, a loop, an I/O error). Unknown is never gone (Rule 15a), so the
// ground is KEPT, and the sentence names the errno so a reader can tell a
// transient failure from a permanent one. Shared by both legs for the same
// reason as the constant above.
const runGroundMeasureUnmeasurableFmt = "the directory could not be measured (%s) — keeping it, an unmeasurable ground is never collected"

// runGroundPresence is the answer to ONE question about an anchor: is the
// directory it names still on disk? Three answers, and the third is not the
// second (Rule 15a).
type runGroundPresence int

const (
	// runGroundGroundExists — the directory is there; the ladder's other
	// questions are the ones that decide its fate.
	runGroundGroundExists runGroundPresence = iota
	// runGroundGroundGone — measured ABSENT. Nothing can be removed, and no
	// other question about this ground has a true answer.
	runGroundGroundGone
	// runGroundGroundUnmeasurable — the measurement itself failed for a reason
	// that is not absence.
	runGroundGroundUnmeasurable
)

// runGroundMeasureGround is the ONE place this package asks whether an anchor's
// directory still exists — the criterion BOTH legs read (Rule 14).
//
// It exists because the two legs DISAGREED, and the disagreement was the whole
// defect (measured on this host 2026-10-27: `gopi task sweep-worktrees
// --dry-run` reported `collectable=759` while 749 of those directories were not
// on disk — 98.7% of the plan was grounds the apply step would skip at its very
// first rung). The removal leg had asked `os.Stat` all along; the report leg
// never did, so the only artefact a dry run produces could not answer "how many
// would this actually free?". Two implementations of one question is how that
// happens, so there is now one.
//
// The criterion is the FILESYSTEM, measured, never git's wording: a stale
// REGISTRATION also makes git say "is not a working tree", and a clone ground
// answers the same sentence for a different reason — git's words cannot tell
// "vanished" from "wrong shape", and this question needs the first only.
func runGroundMeasureGround(dir string) (presence runGroundPresence, why string) {
	if strings.TrimSpace(dir) == "" {
		return runGroundGroundUnmeasurable, "the anchor names no directory"
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return runGroundGroundGone, ""
		}
		return runGroundGroundUnmeasurable, err.Error()
	}
	return runGroundGroundExists, ""
}

// runGroundCollectCloneUnrootedFmt is the clone leg's origin refusal: a
// whole-directory removal may only be aimed at a directory INSIDE the configured
// workspace root, the one place the clone channel creates grounds (Rule 42 — a
// deletion is never performed on a path the code cannot show it owns). The
// refusal names both paths, so a reader can tell a moved knob from a poisoned
// anchor without reading the sink.
const runGroundCollectCloneUnrootedFmt = "the clone ground %s is not inside the configured workspace root %s — refusing a whole-directory removal outside it (check %s / the settings %s key)"

// runGroundCollectCloneDirtyFmt is the clone leg's DIRT refusal. `git worktree
// remove` refuses a dirty linked worktree by itself; nothing in the clone leg
// does, because the removal is `os.RemoveAll` — so this arm asks the ONE dirty
// classifier (reclaimDirtyVerdict) and prints its verdict, which already
// distinguishes a tracked change from untracked-only residue and quotes the
// entries back.
const runGroundCollectCloneDirtyFmt = "the clone ground is not clean — %s"

// runGroundAnchorNoRepoReason is the named refusal for an anchor whose
// repository cannot be established — the LINKED shape only. A linked worktree is
// a REGISTRATION of some other repository and the anchor does not name which, so
// there is nothing in the directory that could supply the answer: guessing would
// mean aiming a `git worktree remove` at a repository the record never named.
//
// IT IS NOT THE CLONE SHAPE'S SENTENCE. A clone ground's `.git` is a DIRECTORY —
// the directory IS its own repository (devworkspace, measured by
// deliveryProofIsCloneGround) — so for that shape the repo is MEASURED, and an
// empty anchor field costs nothing. Before the split this one sentence covered
// both shapes, which left every repo-less clone ground in no reclaim population
// at all (RunGroundCollect's clone-shape branch is the other half of the fix).
const runGroundAnchorNoRepoReason = "the anchor names no repo and the directory is not a clone ground (a linked worktree's registration lives in a repository the anchor does not name) — refusing to guess which repository it belongs to"

// runGroundCollectCloneLabel is the clone leg's shape sentence, appended to
// every refusal it renders so an operator can tell which leg spoke (the two legs
// ask different questions and only one of them removes a whole repository).
const runGroundCollectCloneLabel = "clone ground: "

// RunGroundCollect frees the collectable grounds, and it is deliberately narrow:
// it removes a DIRECTORY, never a branch — the branch is the deliverable the
// merger reads, and no arm in this epic is licensed to touch one.
//
// TWO GROUND SHAPES, TWO REMOVALS, ONE HISTORY. The directory's own `.git` says
// which, and the answer decides the verb (measured 2026-10-26 on this host: 725
// collectable clone grounds, 351 directories / 28.9 GB still on `/`, and this
// arm's single verb — `git worktree remove` — answering exit 128 `is not a
// working tree` for every clone ground in the list):
//
//	· LINKED WORKTREE (`.git` is a FILE) — a REGISTRATION of the main repo:
//	  `git worktree remove` drops the registration and the directory, while the
//	  branch and its objects stay in the repo the registration shares. git
//	  itself refuses a dirty tree and an occupied one, which is why this leg
//	  needs no dirty/delivered rung of its own.
//	· CLONE GROUND (`.git` is a DIRECTORY, internal/devworkspace) — the
//	  directory IS a repository: removing it takes the objects and any ref that
//	  existed only there (`devworkspace.Reclaim` → `os.RemoveAll`). Nothing
//	  refuses for it, so THIS leg asks the two questions git would have asked
//	  of a registration: is the tree clean (reclaimDirtyVerdict) and does the
//	  branch's work exist outside this directory (worktreeSweepUndeliveredRefusal).
//
// BOTH legs re-ask the liveness question immediately before acting (a ground can
// acquire a holder between the scan and the act), ask the shared deterministic
// checklist (reclaim_ground_checklist.go) for the two questions the liveness
// ladder does not answer — "is this the ground I am standing in?" and "does the
// branch share history with the integration branch?" — and write the removal
// record to the audit sink IMMEDIATELY BEFORE the act, with the anchor's own
// evidence, so the operator can account for it afterwards (Rule 9: one writer,
// one record, and a record that precedes the thing it explains).
//
// Returns the directories removed, the ones skipped on the re-check (with the
// reason), and a transport error only for a failure to reach the sink.
func RunGroundCollect(collectable []RunGroundLiveness) (removed, skipped []string, err error) {
	for _, v := range collectable {
		dir := strings.TrimSpace(v.Anchor.Worktree)
		if dir == "" {
			continue
		}
		// THE GONE RUNG — FIRST, and before every other question. An anchor whose
		// directory no longer exists cannot be freed by any verb, and asking the
		// questions below about it produces sentences an operator has to un-learn:
		// the linked leg's `git worktree remove` answers exit 128 `is not a working
		// tree` (measured 2026-10-26: 679 of 725 collectable anchors on this host
		// named directories that were already gone), and the clone leg's checks
		// would judge a repository that is not there. No record is written: nothing
		// was removed, and an audit line for an act that never happened is the same
		// lie as a missing one (Rule 15).
		//
		// The criterion is runGroundMeasureGround — the SAME function the report
		// leg's presence rung asks (Rule 14), so the plan and the act cannot drift
		// about whether a ground exists. It is re-asked HERE even though the scan
		// already separated the gone grounds, because this leg is reachable with a
		// hand-built list, and a directory can vanish between the scan and the act:
		// the check that matters is the one taken closest to the removal.
		switch presence, why := runGroundMeasureGround(dir); presence {
		case runGroundGroundGone:
			skipped = append(skipped, dir+" ("+runGroundCollectGoneReason+")")
			continue
		case runGroundGroundUnmeasurable:
			skipped = append(skipped, dir+" ("+fmt.Sprintf(runGroundMeasureUnmeasurableFmt, why)+")")
			continue
		}
		if runGroundOccupied(dir) {
			skipped = append(skipped, dir+" (a live process appeared after the scan)")
			continue
		}
		if again := RunGroundAnchorLiveness(v.Anchor); again.Live {
			skipped = append(skipped, dir+" ("+again.Reason+")")
			continue
		}
		repo := strings.TrimSpace(v.Anchor.Repo)
		if repo == "" {
			repo = TaskDeliverableRepoFor(v.Anchor.TaskID)
		}
		if repo == "" {
			// THE CLONE SHAPE ANSWERS THIS ITSELF, and it is a MEASUREMENT, not a
			// guess (Rule 42: a removal is performed only on what the code can
			// show it owns). `.git` is a DIRECTORY ⇒ this directory IS a
			// repository of its own (devworkspace.GroundModelOf, through the
			// package's one discriminator deliveryProofIsCloneGround), so there
			// is no "registration's repository" to guess: the registration is
			// the directory.
			//
			// Without this branch the refusal below covered BOTH shapes with one
			// sentence, and the sentence is simply FALSE for a clone ground — so
			// a repo-less clone ground (the auto-registration rows, which carry
			// no deliverable.repo at all) could be anchored and still never
			// reach either removal leg. That was the second half of the
			// population gap:
			// /home/user/workspace/002c43f2-d6dc-4c4b-82d9-f2b40163d53c would
			// have been listed as collectable and then skipped here with
			// "the anchor names no repo" — a directory kept forever by a
			// question whose answer was standing in it.
			if deliveryProofIsCloneGround(dir) {
				repo = dir
			}
		}
		if repo == "" {
			skipped = append(skipped, dir+" ("+runGroundAnchorNoRepoReason+")")
			continue
		}
		// THE DETERMINISTIC CHECKLIST (reclaim_ground_checklist.go). This arm is a
		// SWEEPER: the liveness ladder above proves the run that ANCHORED the
		// ground is over, which is a statement about the TAKER, never a licence to
		// free a directory this process is standing in — `git worktree remove`
		// deletes it under the live process, and the loss stays invisible until
		// something asks for its own cwd. So the rung is asked in its plain form
		// (reclaimGroundChecklistVerdict, not the close-out form): this arm frees
		// SOMEBODY ELSE'S ground, exactly like the sweep arm and the ledger-keyed
		// reclaim arm, and there the asker's own position IS a veto. Rung ② (the
		// branch shares history with the integration branch) is asked with the
		// anchor's own branch, and an anchor that names none skips it — the same
		// "a registration without a branch has no lineage to judge" reading the
		// checklist documents. A refusal SKIPS this ground and leaves the worktree,
		// the registration and the branch exactly as they were; removal is never
		// the response to a failed check.
		if why := reclaimGroundChecklistVerdict(repo, dir, v.Anchor.Branch); why != "" {
			skipped = append(skipped, dir+" ("+why+")")
			continue
		}
		// THE SHAPE SPLIT. `.git` decides which verb can free this directory, and
		// asking it AFTER every refusal above is deliberate: the refusals are
		// shape-independent, so a ground that cannot be freed is never measured a
		// second time for a question that does not change its answer.
		if deliveryProofIsCloneGround(dir) {
			if why := runGroundCollectCloneVerdict(dir, repo, v.Anchor.Branch); why != "" {
				skipped = append(skipped, dir+" ("+why+")")
				continue
			}
			evidence := runGroundCollectCloneLabel + v.Reason + "; " + runGroundAnchorEvidence(v.Anchor)
			// The record precedes the act (see the linked leg's comment below),
			// AND IT CARRIES THE TIP (measured 2026-10-30). This leg is the
			// OTHER arm that destroys a CLONE ground with `os.RemoveAll`, i.e.
			// the second code path that annihilates the only place a run's HEAD
			// can be read. It wrote the same `kind=worktree-removal` record as
			// the channel's own reclaim but with no sha — so a ground collected
			// HERE left the delivery proof with the identical hole this epic
			// repairs on the channel's path (sink census: 5 of 17709
			// worktree-removal records carry a sha, all five from
			// clone-channel-reclaim; this arm's were still empty).
			// `RecordGroundRemovalTip` reads HEAD while the directory exists and
			// emits through `WorktreeRemoveAt`, so the record still precedes the
			// act and there is exactly ONE record for this site.
			RecordGroundRemovalTip(removalaudit.ArmTaskGroundAnchorClone, repo, dir, v.Anchor.Branch, v.Anchor.TaskID, evidence)
			ok, rerr := devworkspace.Reclaim(dir)
			if rerr != nil {
				skipped = append(skipped, dir+" ("+runGroundCollectCloneLabel+"the directory could not be removed: "+groundFirstLine(rerr.Error())+")")
				continue
			}
			if !ok {
				// Reclaim answers (false, nil) for a directory that vanished between
				// the Stat above and the removal — the same fact as the gone rung,
				// reported the same way (nothing was removed, so nothing is listed
				// as removed).
				skipped = append(skipped, dir+" ("+runGroundCollectCloneLabel+runGroundCollectGoneReason+")")
				continue
			}
			removed = append(removed, dir)
			continue
		}
		evidence := v.Reason + "; " + runGroundAnchorEvidence(v.Anchor)
		// The record is emitted IMMEDIATELY BEFORE the removal, which is the
		// contract every removal arm in this repo honours (and the reason the
		// standards gate can check that the emit precedes the act): a record
		// written after the fact would be missing exactly when the removal is
		// what went wrong.
		removalaudit.WorktreeRemove(removalaudit.ArmTaskGroundAnchor, repo, dir, v.Anchor.Branch, v.Anchor.TaskID, evidence)
		if out, rerr := taskGroundGit(repo, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, dir); rerr != nil {
			skipped = append(skipped, dir+" (git could not remove the registration: "+strings.TrimSpace(out)+groundFirstLine(rerr.Error())+")")
			continue
		}
		removed = append(removed, dir)
	}
	return removed, skipped, nil
}

// runGroundCollectCloneRung names WHICH of the clone leg's four questions
// produced a refusal. It exists because the three answers are three different
// repairs and a caller that cannot tell them apart can only print one of them —
// which is the defect this type was added to end (see
// runGroundAdoptRungReason: the backfill leg used to stamp a cleanliness prefix
// onto all of them, so a ground whose `git status --porcelain` was empty was
// told "the tree is not clean").
//
// The rung is a VALUE, not a substring: a caller asserts on the rung it reached,
// never on wording (Rule 13/14 — one definition of "which question is this").
type runGroundCollectCloneRung string

const (
	// runGroundCollectCloneRungNone — the verdict is "" (no refusal): every
	// question passed.
	runGroundCollectCloneRungNone runGroundCollectCloneRung = ""
	// runGroundCollectCloneRungUnrooted — ① ORIGIN: the workspace root could not
	// be resolved, or the ground is not inside it. Rule 42's question ("is this a
	// directory the code can show it owns"), never a statement about the tree.
	runGroundCollectCloneRungUnrooted runGroundCollectCloneRung = "unrooted"
	// runGroundCollectCloneRungCleanlinessUnmeasured — ② could not be ASKED:
	// `git status --porcelain` failed, so nobody knows whether work is loose in
	// the tree. Distinct from "dirty" on purpose (Rule 15a): unknown is not a
	// finding about the tree.
	runGroundCollectCloneRungCleanlinessUnmeasured runGroundCollectCloneRung = "cleanliness-unmeasured"
	// runGroundCollectCloneRungDirty — ② answered: the tree holds uncommitted or
	// untracked content.
	runGroundCollectCloneRungDirty runGroundCollectCloneRung = "dirty"
	// runGroundCollectCloneRungUndelivered — ③ answered: the branch holds commits
	// no delivery point has. The tree is CLEAN when this rung fires.
	runGroundCollectCloneRungUndelivered runGroundCollectCloneRung = "undelivered"
)

// runGroundCollectCloneVerdict is the clone leg's verdict as ONE sentence — the
// form every removal arm prints. The questions it answers are documented on
// runGroundCollectCloneRungVerdict, which is the implementation (Rules 13/14).
func runGroundCollectCloneVerdict(dir, repo, branch string) string {
	_, why := runGroundCollectCloneRungVerdict(dir, repo, branch)
	return why
}

// runGroundCollectCloneRungVerdict answers the CLONE leg's own question — the two
// things `git worktree remove` refuses for a registration and NOTHING refuses for
// an `os.RemoveAll` — plus the origin check a whole-directory removal owes:
//
//	① ORIGIN — the directory must be inside the configured workspace root, the
//	   one place the clone channel creates grounds (Rule 42: a removal is
//	   performed only on what the code can show it owns; an anchor is a record,
//	   and a record is not an ownership proof for an arbitrary path).
//	② CLEAN — reclaimDirtyVerdict's three-way split, and ANY dirt refuses: a
//	   TRACKED change is uncommitted work and refuses (a human's call, the files
//	   named), and untracked-only residue refuses TOO — the verb this leg stands
//	   in for refuses it (`git worktree remove` answering "contains modified or
//	   untracked files, use --force", measured 2026-10-26) while `os.RemoveAll`
//	   would ask nothing at all.
//	③ DELIVERED — worktreeSweepUndeliveredRefusal: does the branch hold commits no
//	   delivery point has? A clone ground's directory IS its only repository, so
//	   this is the rung that decides whether the removal destroys work.
//
// "" means the leg may proceed. Every refusal is the shared rung's own sentence
// (Rule 13: one spelling per fact), prefixed with the leg's label so an operator
// reading a skipped line can tell which of the two removals refused.
//
// It returns the RUNG as well as the sentence, and that pair is the point: a
// caller that has to SAY which question refused (the backfill leg's report) reads
// the rung, while a caller that only prints the refusal reads the sentence. The
// wording of a sentence is never used to infer its rung — that inference is the
// defect this return value exists to remove.
func runGroundCollectCloneRungVerdict(dir, repo, branch string) (runGroundCollectCloneRung, string) {
	root, source, rerr := config.WorkspaceRootConfigured()
	if rerr != nil {
		return runGroundCollectCloneRungUnrooted, runGroundCollectCloneLabel + "the workspace root could not be resolved (" + rerr.Error() +
			") — refusing a whole-directory removal whose origin cannot be checked"
	}
	if root != "" {
		if canonical := scratch.CanonicalPath(dir); !scratch.PathWithin(filepath.Clean(canonical), filepath.Clean(scratch.CanonicalPath(root))) {
			return runGroundCollectCloneRungUnrooted, fmt.Sprintf(runGroundCollectCloneUnrootedFmt, dir, root, source, config.WorkspaceRootSettingsKey)
		}
	}
	porcelain, derr := reclaimGitStatus(dir)
	if derr != nil {
		// An UNMEASURABLE tree is kept, not removed (Rule 15a): this leg deletes a
		// whole repository, and "git could not tell me whether work is loose in
		// there" is exactly the sentence that must stop it.
		return runGroundCollectCloneRungCleanlinessUnmeasured, runGroundCollectCloneLabel + "the tree's cleanliness could not be measured (" + groundFirstLine(derr.Error()) + ") — keeping it"
	}
	if why := reclaimDirtyVerdict(porcelain); why != reclaimStatusClean {
		// ANY dirt refuses here — untracked-only included — and that is PARITY
		// with the linked leg rather than a stricter whim: `git worktree remove`
		// refuses a tree holding untracked files (it asks for --force, which this
		// arm never passes), and `os.RemoveAll` would delete them with no
		// equivalent question. The classifier's SPLIT still buys something: the
		// sentence names which kind of dirt it is, so the operator's next step
		// differs (clean your scratch vs a human's call on uncommitted work).
		return runGroundCollectCloneRungDirty, fmt.Sprintf(runGroundCollectCloneDirtyFmt, reclaimPorcelainOneLine(why))
	}
	// ③ DELIVERY. Its refusal is the rung's OWN sentence, not a rewording — and a
	// ground that reaches here has already PASSED the cleanliness rung above, so
	// a caller may not describe this answer as a statement about the tree.
	if why := worktreeSweepUndeliveredRefusal(dir, repo, branch); why != "" {
		return runGroundCollectCloneRungUndelivered, why
	}
	return runGroundCollectCloneRungNone, ""
}

// runGroundAnchorEvidence renders the anchor's identity sentence, for the audit
// record's evidence field.
func runGroundAnchorEvidence(a RunGroundAnchor) string {
	return "ground anchored by pid " + a.PID + " start-ticks " + a.StartTicks + " session " + a.Session
}

// groundFirstLine collapses a multi-line error into the single line an operator
// reads in a report (the removal arms' convention).
func groundFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return ": " + strings.TrimSpace(s[:i])
	}
	return ": " + strings.TrimSpace(s)
}
