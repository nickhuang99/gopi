package tools

// Dead-holder RESUME (epic dead-holder-resume, 2026-09-16).
//
// THE GAP THIS CLOSES. The merger never has to babysit a child — but it also
// never RECOVERS one. Everything the merger knows about a holder is an
// ANNOTATION: RepoSweepPreview.DeadHolder says "a provably dead holder still
// occupies this branch" and the reclaim arm (ReclaimDeadHolderWorktrees) frees
// the DIRECTORY it names. Nothing continues the interrupted RUN, so the work
// that child had in flight — a half-written fix, a worktree full of commits,
// a session checkpoint with the full context — is abandoned by default. A
// branch protected by a dead holder is therefore either withheld forever or
// (after reclaim) merged as if it were finished.
//
// THE DECISION (deterministic Go, no LLM in the path): for each worktree-
// protected branch, ask the LEDGER who holds it, then ask the MACHINE whether
// that holder is still alive — the same three-factor verdict the rest of the
// ledger trusts (pid + process start ticks + command line, i.e. the row's own
// proc_pid/proc_start_ticks/proc_cmd triple). Proven dead → RESUME the owning
// row's session (`gopi session resume <session-id> --task <task-id>`), detached, so
// the interrupted work continues instead of being abandoned.
//
// WHY A SPAWN IS THE RIGHT FACE (and not an in-process agent): `gopi session resume`
// is the existing, tested recovery entry point — it loads the session
// checkpoint (the byte-exact full context at the last request), gates on the
// row's identity, performs the OWNERSHIP HANDOFF (it stamps ITSELF as the new
// holder) and re-links the row. Re-implementing any of that here would be a
// second recovery mechanism, which the epic forbids. The merger spawns that
// verb and moves on — the child is a process in its own right (Setsid), so
// the merger still never waits, never babysits, never kills.
//
// LOOP SAFETY (the anti-loop requirement). Every resume appends a bounded
// counter tag to the row (`dead-holder-resume:<n>`); at
// deadHolderResumeMaxAttempts the arm STOPS and reports the row as failed with
// a named reason. The resumed run itself re-stamps the row with LIVE identity,
// so the next pass sees a live holder and does nothing — the cap is the
// backstop for the case where every attempt dies again.
//
// RULE 15 (never fabricate a death). A resume is an ACT, so its verdict must
// be the conservative one: only a POSITIVE, readable answer — /proc/<pid>
// absent, a zombie, or a start-tick mismatch — counts as dead. An unreadable
// probe is "unreadable", never "dead", and blocks the resume.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/runcmd"
)

const (
	// deadHolderResumeMaxAttempts caps how many times this arm may resume the
	// SAME row. A row whose every resume dies again would otherwise be
	// restarted once per merger tick forever (a restart loop that burns a
	// model call each turn). Three matches the other bounded retry ladders in
	// this package.
	deadHolderResumeMaxAttempts = 3

	// deadHolderResumeTagPrefix labels the attempt counter tags. It must NOT
	// be a prefix of the resume-lineage tag (taskTagResumedPrefix): the
	// lineage tag records which sessions a row was resumed as, while these
	// record how many times the DEAD-HOLDER arm did it.
	deadHolderResumeTagPrefix = "dead-holder-resume:"

	// deadHolderResumeLogName is the per-task file the detached resume's own
	// output lands in (beside the task's session dir), so the merger log does
	// not absorb a whole agent transcript: one path, printed once.
	deadHolderResumeLogName = "dead-holder-resume.log"

	// deadHolderResumeSpawnGrace is how long the spawn check waits for an
	// immediate exec failure to surface. exec.Start() itself is synchronous
	// (a missing binary / EACCES is returned by Start), so this bound only
	// keeps the wait short and non-blocking.
	deadHolderResumeSpawnGrace = 2 * time.Second
)

// holderProbeVerdict is the three-way answer of a holder probe. Three states,
// never a boolean: the whole point of Rule 15 here is that "I could not tell"
// must not collapse into "dead" (the fail-open bug of memory #2632, which
// deleted a live session's worktree).
type holderProbeVerdict int

const (
	// holderProbeLive: the recorded process is still the SAME process
	// (start ticks match, or the stronger /proc cwd evidence matches).
	holderProbeLive holderProbeVerdict = iota
	// holderProbeDead: this host POSITIVELY proved the process is gone.
	holderProbeDead
	// holderProbeUnreadable: the probe could not answer (procfs absent, pid
	// not visible). NEVER treated as dead.
	holderProbeUnreadable
)

// String renders the verdict for operator-facing lines.
func (v holderProbeVerdict) String() string {
	switch v {
	case holderProbeLive:
		return "live"
	case holderProbeDead:
		return "dead"
	default:
		return "unreadable"
	}
}

// holderProbe is one liveness question about one recorded holder. It exists so
// the SECONDARY evidence (/proc/<pid>/cwd — the only channel that can connect a
// process to a DIRECTORY) can resolve the case the triple alone cannot: several
// ledger rows name the same worktree, and the recorded identity matches a live
// process that is in fact holding a DIFFERENT directory.
//
// The primary evidence is ALWAYS the triple, delegated to
// finishCleanupProcessHolderLive — the single existing liveness implementation
// (Rule 14). This probe adds no second identity capture and no second start-tick
// reader; it only asks the cwd question when, and only when, that function
// answered "not live".
type holderProbe struct {
	ident procIdentity
	// worktree is the directory whose occupancy is in question. Empty = the
	// cwd cross-check is not applicable.
	worktree string
	// cwdLookup is injected for tests (defaults to finishCleanupProcCWD).
	cwdLookup func(pid string) (string, bool)
}

// resolve answers the liveness question: Live (HOLD the branch and do NOT
// resume), Dead (resume), or Unreadable (do nothing — rule 15).
func (p holderProbe) resolve() holderProbeVerdict {
	if p.ident.Pid <= 0 || (p.ident.StartTicks <= 0 && p.ident.Cmd == "") {
		// The row names a holder but recorded no identity (a pre-column row,
		// or a stamp whose capture failed). The triple cannot be consulted,
		// so the older, NARROWER evidence decides — exactly the fallback
		// finishCleanupDeadLocalHolder uses: a claim pid this host can see is
		// GONE is a death proof that carries no false positive.
		if !claimPidLocallyDead(p.ident) {
			return holderProbeUnreadable
		}
		return holderProbeDead
	}
	live, why := procname.RunnerLive(identityProcRoot,
		identityLiveness(p.ident.Pid, p.ident.StartTicks, p.ident.StartTicks > 0, p.ident.Cmd))
	if why != "" {
		// The host did not answer, so nothing is proved: an unmeasurable factor
		// is NOT a death (Rule 15a) and this probe refuses to resume on it.
		return holderProbeUnreadable
	}
	if live {
		return holderProbeLive
	}
	// The shared predicate MEASURED the runner not-live. Before accepting that,
	// ask the ONE question it cannot answer — the checkout-occupancy question:
	// is a live process standing IN this worktree? A pid can be recorded
	// correctly yet belong to a run of a DIFFERENT task (the multi-row
	// ambiguity), and its own recorded identity is then not this row's evidence.
	// A positive cwd match is proof of life; an unreadable cwd is not proof of
	// death. That question keeps its own implementation (scratch.HeldByProcesses
	// / LiveHolders); this predicate never answers it.
	if p.cwdHoldsWorktree() {
		return holderProbeLive
	}
	// A measured not-live (process directory gone, incarnation differs, or a
	// zombie): resume.
	return holderProbeDead
}

// cwdHoldsWorktree reports whether a live process's working directory IS the
// worktree in question. It is the SECONDARY evidence, consulted only after the
// primary verdict failed to corroborate life.
func (p holderProbe) cwdHoldsWorktree() bool {
	if p.worktree == "" || p.ident.Pid <= 0 {
		return false
	}
	lookup := p.cwdLookup
	if lookup == nil {
		lookup = finishCleanupProcCWD
	}
	cwd, ok := lookup(strconv.Itoa(p.ident.Pid))
	if !ok {
		return false // unreadable (a foreign session under ptrace_scope=1) — never a death proof
	}
	return sameWorktreePath(cwd, p.worktree)
}

// sameWorktreePath compares a /proc cwd against a worktree path. BOTH sides are
// resolved (a worktree recorded through a symlink never string-matches the
// resolved path /proc reports — the same tolerance finishCleanupWorktreeInUse
// applies), and a process standing in a SUBdirectory of the tree still counts
// as occupying it (the executor chdir'd into a nested package), which is the
// relation finishCleanupWorktreeInUse establishes for its own cwd scan.
func sameWorktreePath(cwd, worktree string) bool {
	cwd = strings.TrimSpace(cwd)
	worktree = strings.TrimSpace(worktree)
	if cwd == "" || worktree == "" {
		return false
	}
	tree := resolveExistingPath(worktree)
	here := resolveExistingPath(cwd)
	if tree == "" || here == "" {
		return false
	}
	if here == tree {
		return true
	}
	return strings.HasPrefix(here, tree+string(os.PathSeparator))
}

// resolveExistingPath returns the symlink-resolved absolute path when the path
// exists, else its cleaned absolute form (a destroyed worktree must still
// compare, not silently answer "no").
func resolveExistingPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

// claimPidLocallyDead is the fallback death proof for a holder that recorded NO
// identity: the claim stamp parses as THIS host's pid (claimHolder's ok — a
// foreign or blank stamp is unknowable here, hence never dead) and that pid is
// PROVABLY GONE. It reuses the package's ONE probe (pidGone), never a second.
//
// PidGone rather than `!pidAlive` because this is a DEATH PROOF the resume arm
// acts on: it takes the row away from the holder. An EPERM answer means the
// holder EXISTS under another user — a measurement of life, not of death — so
// it must not license the takeover (epic 01a17e87).
func claimPidLocallyDead(ident procIdentity) bool {
	if ident.Pid <= 0 {
		return false
	}
	return pidGone(ident.Pid)
}

// deadHolderResumeRow is a ledger row that carries a deliverable (the candidate
// set this arm judges): either a recorded worktree (the path axis) or a
// recorded branch (the branch axis — see deadHolderResumeRowWorktree).
type deadHolderResumeRow struct {
	TaskID      string
	SessionID   string
	Status      string
	ClaimedBy   string
	Deliverable string
	Ident       procIdentity
	Attempts    int
}

// deadHolderResumeOutcome is one row's disposition, rendered by the caller.
type deadHolderResumeOutcome struct {
	TaskID    string
	Worktree  string
	SessionID string
	Verdict   string
	Resumed   bool
	Attempts  int
	Reason    string
	Action    string // the literal command spawned, when Resumed
	LogPath   string
}

// Line renders one outcome as a single operator-facing log line. ONE renderer
// so the merger pass, the CLI face and the tests cannot drift (Rule 14).
func (o deadHolderResumeOutcome) Line() string {
	if o.Resumed {
		return fmt.Sprintf("resume-dead-holder: %s (session %s, attempt %d/%d) — spawned: %s (log: %s)",
			o.TaskID, o.SessionID, o.Attempts, deadHolderResumeMaxAttempts, o.Action, o.LogPath)
	}
	wt := ""
	if o.Worktree != "" {
		wt = " worktree " + o.Worktree
	}
	log := ""
	if o.LogPath != "" {
		log = " (log: " + o.LogPath + ")"
	}
	return fmt.Sprintf("resume-dead-holder: %s%s — not resumed: %s (holder verdict: %s, attempts %d/%d)%s",
		o.TaskID, wt, o.Reason, o.Verdict, o.Attempts, deadHolderResumeMaxAttempts, log)
}

// ResumeDeadHolderRuns judges every ledger row that names a worktree of repo
// and resumes the ones whose holder is PROVABLY dead. Returns one outcome per
// candidate whose holder was judged (live holders are silent: the branch is
// simply still owned).
//
// THE OCCUPANCY QUESTION IS ASKED ON BOTH AXES (2026-09-16). git is the
// occupancy source of truth, never the ledger: the row's branch is paired with
// the worktree git reports as CHECKING IT OUT. That pairing is what keeps the
// arm honest for a row whose deliverable records no directory at all — the
// comparison used to be between the row's (empty) worktree field and git's
// path, which never matched, so the row was dropped by an empty-string
// comparison instead of being judged. Because that relation is asked of
// mergedCleanupWorktrees, the set of rows that survive it is EXACTLY the set
// their own recorded directory would have selected: a row that records a
// different directory still fails the pairing, so no worktree's occupancy
// evidence is widened and no veto is weakened.
func ResumeDeadHolderRuns(repo string) []deadHolderResumeOutcome {
	rows, err := deadHolderResumeRows()
	if err != nil || len(rows) == 0 {
		return nil // no ledger / no candidates: nothing to judge (never a guess)
	}
	byBranch, _, err := mergedCleanupWorktrees(repo)
	if err != nil {
		byBranch = nil // repo unreadable: fall back to the row's own path
	}
	var out []deadHolderResumeOutcome
	for _, r := range rows {
		wt, ok := deadHolderResumeRowWorktree(r, byBranch)
		if !ok {
			continue // the row does not hold a checkout of this repo
		}
		oc, judged := resumeDeadHolderRow(repo, r, wt)
		if judged {
			out = append(out, oc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

// deadHolderResumeRowWorktree resolves WHICH directory this row is holding, on
// whichever axis the row records — or ok=false when it holds none of this repo.
// One decision, spelled once, so the two axes cannot drift apart (Rule 14).
//
//	(a) PATH AXIS — the row records a directory. git must register the row's
//	    BRANCH as checked out; the directory that is returned is GIT's, because
//	    git is the occupancy source of truth and the ledger's recorded path is
//	    measured wrong on the live host (row 01a0a9b5: it records
//	    gopi-next-tick while git registers gopi-merger-next-tick — memory
//	    #3231/#3234). The two agree for a healthy row, so the ordinary case is
//	    unchanged; when they disagree the registered checkout is the ground
//	    that IS there, and dropping the row over a stale ledger path is the
//	    direction that abandons an interrupted run. Resolution kept BOTH SIDES
//	    of the merge: the branch's exact-pairing structure, master's
//	    git-names-the-checkout semantics (the two axes are mutually exclusive
//	    on a mismatched path — git wins).
//	(b) BRANCH AXIS — the row records a branch and NO directory. A row's
//	    deliverable is a RECORD, and a record outlives the process that wrote
//	    it; the branch backfill lands at writeback, while a run killed in flight
//	    may never have had deliverable.worktree written at all. git's statement
//	    — "this branch is checked out HERE" — is therefore the occupancy
//	    evidence, exactly as it is for the worktree sweep's branch arm, and the
//	    discovery is confined to the row's BRANCH: a matching name held at a
//	    path the row never named is still this row's branch, never another
//	    row's directory (the branch axis selects on the branch, it never
//	    licenses a path).
//
// An unreadable repo (byBranch nil) has no occupancy to consult: the path axis
// falls back to the row's recorded directory (it IS the only ground evidence),
// and the branch axis cannot answer, so it claims nothing.
func deadHolderResumeRowWorktree(r deadHolderResumeRow, byBranch map[string]string) (string, bool) {
	wt := taskDeliverableWorktree(r.Deliverable)
	branch := taskDeliverableBranchOrEmpty(r.Deliverable)
	if byBranch == nil {
		// Repo unreadable: the recorded directory is the only occupancy
		// evidence there is (a branch-only row has none, so it claims
		// nothing — Rule 15: never a guess).
		return wt, wt != ""
	}
	if branch == "" {
		return "", false
	}
	// OCCUPANCY is git's statement, not the ledger's (the same rule
	// deadHolderProtectedBranches and branchAttributionCheckout apply): a
	// branch git no longer registers as checked out holds no ground, and the
	// checkout git DOES register is the one the verdict must judge.
	path, ok := byBranch[branch]
	if !ok {
		return "", false
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return "", false // the registration outlived its directory: no ground
	}
	return path, true
}

// resumeDeadHolderRow judges ONE candidate and resumes it when the holder is
// provably dead. judged=false means the holder is live (the branch is still
// owned — the merger says nothing, because nothing happened).
func resumeDeadHolderRow(repo string, r deadHolderResumeRow, worktree string) (deadHolderResumeOutcome, bool) {
	probe := holderProbe{ident: r.Ident, worktree: worktree}
	verdict := probe.resolve()
	if verdict == holderProbeLive {
		return deadHolderResumeOutcome{}, false
	}
	oc := deadHolderResumeOutcome{
		TaskID:    r.TaskID,
		Worktree:  worktree,
		SessionID: r.SessionID,
		Verdict:   verdict.String(),
		Attempts:  r.Attempts,
	}
	if verdict == holderProbeUnreadable {
		// Rule 15: this host could not answer. Do NOT resume (a resume would
		// double-run a holder that may well be alive) and do NOT call it dead
		// in the report.
		oc.Reason = "holder liveness unreadable on this host (no identity recorded and the claim pid is not provably gone)"
		return oc, true
	}
	if r.Attempts >= deadHolderResumeMaxAttempts {
		oc.Reason = fmt.Sprintf("resume attempt cap reached (%d) — the resumed runs kept dying; failing the row for an operator decision instead of restarting it forever",
			deadHolderResumeMaxAttempts)
		oc.Action = "fail-row"
		failDeadHolderRow(r.TaskID, oc.Reason)
		return oc, true
	}
	// WHICH SESSION THIS CONTINUES — resolved, never read off the column
	// (internal/tools/task_resume_session.go). r.SessionID is the ledger's
	// `session_id`, which for an agent-submitted row names the session that
	// SUBMITTED it: resuming it replays another conversation into this row.
	// TaskResumeSessionFor prefers the row's OWN run directory and falls back to
	// the row id, whose task-mode resolution (tools.TaskResumeTarget) reads that
	// directory itself and — when the row's directory holds no run at all —
	// walks to the row's `session_id` column as the pre-layout/re-homed pointer.
	// MEASURED (2026-10-29, row 01a14b50): no run directory, column = one bare
	// chat record; the arm spawned `session resume 01a14b50-…` and that run
	// continued the COLUMN's session — which is the fallback working as
	// documented, not this line's claim that the directory answers.
	rs := TaskResumeSessionFor(r.TaskID)
	target := strings.TrimSpace(rs.SessionID)
	oc.SessionID = target
	if rs.Differs() {
		// Rule 15: the ledger column disagreed with the row's own run. Report it
		// in the same line the operator reads the outcome from — never silently.
		if oc.Reason == "" {
			oc.Reason = rs.Note()
		} else {
			oc.Reason += "; " + rs.Note()
		}
	}
	action, logPath, childPid, err := deadHolderResumeSpawnFn(repo, r.TaskID, target)
	if err != nil {
		// pid == 0: nothing was started, so no attempt was spent (an exec
		// failure is not the run's fault and must not consume the cap).
		// pid > 0: the run STARTED and died at once — an attempt, counted
		// below so a resume that cannot survive its own startup is bounded.
		oc.Reason = err.Error()
		// Keep the child's own log path in the outcome even on this path: the
		// exit reason is a one-liner and the log is where the real cause is.
		oc.LogPath = logPath
		if childPid > 0 {
			oc.Attempts = r.Attempts + 1
			if nErr := noteDeadHolderResumeAttempt(r.TaskID, oc.Attempts); nErr != nil {
				fmt.Fprintf(os.Stderr, "[merger] resume-dead-holder: %s: attempt counter write failed (%v) — the cap may not hold\n", r.TaskID, nErr)
			}
		}
		return oc, true
	}
	// OWNERSHIP HANDOFF, performed by the PARENT on the child's behalf: the
	// moment the recovery run exists, the ledger must name IT as the holder —
	// otherwise the next tick (or the reclaim arm, which frees directories of
	// dead holders) still reads the dead pid and acts on a row that is alive
	// again. Capturing the child's identity here also closes the window in
	// which a CONCURRENT resume would judge the row unheld.
	if err := TaskStampHolder(r.TaskID, claimHolderOfPid(childPid), childPid); err != nil {
		// The child IS running; the stamp is what keeps it from being judged
		// dead (and its worktree reclaimed) on the next pass. A failure here
		// must be visible, not swallowed (Rule 15b) — the attempt counter is
		// deliberately NOT advanced, so the next pass re-runs the handoff
		// rather than treating this spawn as a spent attempt.
		oc.Reason = fmt.Sprintf("resume run (pid %d) started but the ownership stamp failed: %v — the next pass will retry", childPid, err)
		return oc, true
	}
	oc.Resumed = true
	oc.Action = action
	oc.LogPath = logPath
	oc.Attempts = r.Attempts + 1
	// The attempt counter is written AFTER a successful spawn, and the lineage
	// tag is left to `gopi session resume` itself (it stamps the run's own session id):
	// this arm records only what it did — how many times it had to.
	if err := noteDeadHolderResumeAttempt(r.TaskID, oc.Attempts); err != nil {
		// Rule 15b: a failed counter write must not be swallowed — without it
		// the cap can be outrun by repeated passes.
		fmt.Fprintf(os.Stderr, "[merger] resume-dead-holder: %s: attempt counter write failed (%v) — the cap may not hold\n",
			r.TaskID, err)
	}
	return oc, true
}

// deadHolderResumeRows loads the candidates. Scope is deliberately narrow: a
// row that carries a DELIVERABLE and is in a status that can carry a live
// holder (taskLiveHolderStatus). A terminal row's work is finished or failed —
// there is nothing to continue, and the reclaim arm owns its directory.
//
// THE EVIDENCE SET CARRIES BOTH OWNERSHIP AXES (2026-09-16). The scan used to
// filter on the worktree needle alone, so a claimed row whose deliverable
// records only a branch — {"repo":"…","branch":"…"}, the shape a run leaves
// when its tree was never backfilled — was invisible to the arm that exists to
// protect it, while every direct-row test kept passing. That is the very
// truncation measured and fixed one file over (worktreeSweepTaskRows,
// 2026-09-15: on a real probe ledger a branch-only row was absent from the
// evidence set entirely). A needle can never be kept in step with the
// deliverable blob's spelling, so the scan takes every row carrying a
// deliverable at all and lets the parsers decide: the branch axis is resolved
// by taskDeliverableBranchOrEmpty, the path axis by taskDeliverableWorktree.
func deadHolderResumeRows() ([]deadHolderResumeRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), finishCleanupDBTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	// NO LIMIT: recency does not correlate with liveness (the 2026-09-14
	// truncation incident, task 01a09d67) — an age-ASC cut drops exactly the
	// live rows.
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, COALESCE(session_id,''), status, COALESCE(claimed_by,''), COALESCE(deliverable,''),
		        COALESCE(`+tasksProcPidColumnName+`,0), COALESCE(`+tasksProcStartColumnName+`,0),
		        COALESCE(`+tasksProcCmdColumnName+`,''), COALESCE(tags,'') FROM tasks
		 WHERE COALESCE(deliverable,'') <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deadHolderResumeRow
	for rows.Next() {
		var r deadHolderResumeRow
		var procPid, procStart int64
		var tags string
		if err := rows.Scan(&r.TaskID, &r.SessionID, &r.Status, &r.ClaimedBy, &r.Deliverable,
			&procPid, &procStart, &r.Ident.Cmd, &tags); err != nil {
			return nil, err
		}
		if !taskLiveHolderStatus(r.Status) {
			continue // terminal / holding: no run to continue
		}
		if taskDeliverableBranchOrEmpty(r.Deliverable) == "" && taskDeliverableWorktree(r.Deliverable) == "" {
			continue // names neither a branch nor a directory: nothing to attribute or recover
		}
		r.Ident.Pid = int(procPid)
		r.Ident.StartTicks = procStart
		r.Attempts = deadHolderResumeAttempts(tags)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// deadHolderResumeAttempts counts this arm's attempt tags on a row.
func deadHolderResumeAttempts(tags string) int {
	n := 0
	for _, tg := range strings.Split(tags, ",") {
		if strings.HasPrefix(strings.TrimSpace(tg), deadHolderResumeTagPrefix) {
			n++
		}
	}
	return n
}

// noteDeadHolderResumeAttempt appends one attempt tag (the append-only counter
// the cap reads). The tag VALUE carries the ordinal so the trail reads in
// order rather than as N identical entries.
func noteDeadHolderResumeAttempt(taskID string, attempt int) error {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	tag := deadHolderResumeTagPrefix + strconv.Itoa(attempt)
	added, err := taskAddTag(ctx, db, taskID, tag)
	if err != nil {
		return err
	}
	if !added {
		return nil // already recorded (an idempotent re-run of the same ordinal)
	}
	return nil
}

// failDeadHolderRow records the cap outcome ON THE ROW: the run could not be
// recovered, and an operator must decide. A failure with a named reason is the
// honest end of the ladder (Rule 15: an unbounded restart loop would be a
// silent failure dressed as progress).
func failDeadHolderRow(taskID, reason string) {
	if _, err := TaskUpdateReason(taskID, taskStatusFailed, reason, ""); err != nil {
		fmt.Fprintf(os.Stderr, "[merger] resume-dead-holder: %s: could not record the failed row (%v)\n", taskID, err)
	}
}

// deadHolderResumeSpawnFn is the arm's SPAWN SEAM. It exists so a test can
// observe WHICH target the arm passes to `gopi session resume` without starting
// a process — the question the session_id defect turns on (the arm used to pass
// the ledger column, i.e. the SUBMITTER's session, and replay another
// conversation into the row). Production default is spawnDeadHolderResume; the
// same shape cmd/agent uses for its abnormal-child retry (taskRunResumeSpawn),
// so there is one convention for "a spawn a test must see".
var deadHolderResumeSpawnFn = spawnDeadHolderResume

// spawnDeadHolderResume launches `gopi session resume <target> --task <task-id>` as a
// DETACHED child (Setsid: its own session, so the merger's lifetime never
// bounds it) and returns the rendered command plus the log path. The merger
// does not wait for it — that is the point of the whole epic ("the merger never
// has to wait on/babysit/kill a child").
func spawnDeadHolderResume(repo, taskID, target string) (action, logPath string, pid int, err error) {
	argv := runcmd.ResumeArgv(target, taskID)
	logPath = deadHolderResumeLogPath(taskID)
	// The repo is not passed to the child: it resolves its own workdir from
	// the row's deliverable (cli_resume's worktree chdir).
	_ = repo
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f, ferr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); ferr == nil {
		cmd.Stdout = f
		cmd.Stderr = f
		defer f.Close()
	} else {
		fmt.Fprintf(os.Stderr, "[merger] resume-dead-holder: %s: cannot open %s (%v) — the resume's output goes to the merger's own stream\n",
			taskID, logPath, ferr)
	}
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return "", "", 0, fmt.Errorf("spawning `%s` failed: %w", runcmd.Command(argv), err)
	}
	// Wait for an IMMEDIATE exec failure (a bad binary) to surface, then let
	// the child go: the reaper is deliberately not held, so the child is
	// orphaned into init's care the moment the merger exits — the standard
	// shape for a detached recovery run. A bounded wait, never a wait on the
	// RUN (that is the labour this arm exists to avoid).
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	select {
	case werr := <-waitCh:
		// It started and died before doing any work. Report it WITH the pid
		// (pid > 0 = an attempt WAS spent) so the caller counts it and the cap
		// bounds a run that can never get past its own startup — without the
		// count this branch would restart it once per merger tick forever.
		return "", logPath, cmd.Process.Pid, fmt.Errorf("resume run `%s` exited immediately: %w", runcmd.Command(argv), werr)
	case <-time.After(deadHolderResumeSpawnGrace):
		// Still running after the grace window: it is an agent now, not a
		// failed exec. The reaper goroutine above already owns cmd.Wait.
	}
	return runcmd.Command(argv), logPath, cmd.Process.Pid, nil
}

// SpawnTaskResume launches `gopi session resume <target> --task <task-id>` as a
// DETACHED child and reports the rendered command, where its own output goes,
// and its pid when it survived its startup. It is the EXPORTED face of the one
// spawn implementation above (spawnDeadHolderResume) so a SECOND caller — the
// `gopi task run` wrapper's abnormal-child retry (epic wrapped-executor-child
// 4/5) — reuses the same recovery entry point instead of growing a second
// spawn mechanism (Rule 14). A pid > 0 means the run STARTED (and therefore
// consumed an attempt): the caller counts it even when err != nil, which is the
// same contract the dead-holder arm reads.
func SpawnTaskResume(taskID, target string) (action, logPath string, pid int, err error) {
	action, logPath, pid, err = spawnDeadHolderResume("", taskID, target)
	if pid <= 0 {
		return action, logPath, pid, err
	}
	// OWNERSHIP HANDOFF, performed by the parent on the child's behalf — the
	// same step the dead-holder arm performs at its own call site, through the
	// same single implementation (TaskStampHolder): the moment a recovery run
	// exists the ledger must name IT, or the next pass still reads the dead
	// pid and acts on a row that is alive again (and a second resume could be
	// judged admissible). The resumed child also self-stamps on its own
	// bootstrap (cli_resume's TaskStampSelfClaim) — this closes the window
	// BEFORE the child gets that far. A failure is recorded, never swallowed
	// (Rule 15b); the caller still sees pid > 0, because the run DID start.
	if serr := TaskStampHolder(taskID, claimHolderOfPid(pid), pid); serr != nil {
		taskNotifyLog("task %s: resume run (pid %d) started but the ownership handoff failed: %v", taskID, pid, serr)
	}
	return action, logPath, pid, err
}

// claimHolderOfPid renders the "host:pid" claim stamp for a recovery run this
// process spawned — the same shape taskDefaultClaimHolder writes for itself,
// built from the SAME constants (the host and the separator), so a stamped
// holder is indistinguishable from one a self-stamping process wrote.
func claimHolderOfPid(pid int) string {
	return fmt.Sprintf("%s:%d", hostnameOrEmpty(), pid)
}

// deadHolderResumeLogPath is where the detached resume writes: beside the
// task's own session dir when it is reachable, else the process-wide temp tree.
func deadHolderResumeLogPath(taskID string) string {
	if dir, err := taskSessionDir(taskID); err == nil && strings.TrimSpace(dir) != "" {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr == nil {
			return filepath.Join(dir, deadHolderResumeLogName)
		}
	}
	return filepath.Join(os.TempDir(), taskID+"-"+deadHolderResumeLogName)
}

// runcmdLauncher is the gopi binary a spawned recovery run must be: the
// launcher this process was started as (runcmd.Launcher — GOPI_BINARY, then
// GOPI_SELF, then this executable), i.e. the SAME build the merger is.
func runcmdLauncher() string {
	return runcmd.Launcher()
}
