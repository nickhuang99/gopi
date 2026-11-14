// task_ground_reconcile.go — the READ-SIDE reconciliation of a HISTORICAL
// owner-less ground.
//
// THE GAP (epic 01a0e074, the half 6a4f556c left open). A run that mints its own
// branch/worktree now RECORDS it (the plural deliverable.grounds axis). Rows
// minted BEFORE that producer landed still record nothing, so every
// deliverable→row lookup answers "no owner" for a LIVE ground — the measured
// shape (row 01a0ddc4-c410, session 3592a5ef): the run created
// /home/user/workspace/gopi-evi-pushback on fix/pushback-rule31-evidence while
// its row named its own ground, and NOTHING in the ledger names the created
// directory or branch.
//
// WHY READ SIDE, NOT A BACKFILL VERB. A backfill would WRITE a ground onto a row
// the ledger never recorded — for a rebuilt skeleton it would be a
// RECONSTRUCTION — and this axis' one writer (taskBackfillDeliverableGrounds) is
// deliberately restricted to the moment a run CREATES a ground, in-process,
// where the taker's own identity is observable. A historical ground has no taker
// left to ask, so the write would be an inference; and writing the inference
// DOWN (rather than deriving it per query) freezes it: a wrong row would then be
// the ledger's own claim, un-appealable, and the reconciliation evidence (a
// session's command line, git's registration, the row's life window) is exactly
// what a later reader must be able to re-check. That is also Rule 35: the
// artifact such a verb needs — a written reconstruction, plus a dry-run whose
// output is itself a state a reader must trust — buys nothing the read cannot
// give, and it creates a second writer of the ground axis. So the ledger row
// stays the store, and the answer is DERIVED per query from the evidence the
// ledger already holds.
//
// THE EVIDENCE, AND WHY EACH PIECE IS REQUIRED. The reconciliation names a row
// only when ALL FOUR hold (any one alone is a guess):
//
//	① git registers the asked ground LIVE — a `git worktree list` registration
//	   whose directory still exists (taskRepoLinkedWorktrees, the ONE reader).
//	   Without it there is no ground to reconcile at all.
//	② the candidate row's deliverable.repo IS the repository that registers the
//	   ground — the same repo, and the ground is not already one of that row's
//	   axes (nothing matched, by construction of the caller).
//	③ the row was ALIVE when the registration was created (created_at ≤ the
//	   registration's birth ≤ the row's end), so a release row of today cannot
//	   be read as the owner of a ground minted yesterday. The registration's
//	   birth is git's own bookkeeping (the admin dir's gitdir mtime — the ONE
//	   reader branchAttributionCheckoutAge already uses for the same fact).
//	④ the row's OWN SESSION minted it: the session JSONL that belongs to
//	   row.SessionID contains a `git worktree add … -b <the branch>` command
//	   (the ONE parser, taskWorktreeAddBranchFromCommand) whose own timestamp
//	   falls inside the row's life window. This is the piece that makes the
//	   answer CHECKABLE — an operator re-runs the same command line.
//
// WHAT IT DELIBERATELY DOES NOT DO. No write (no ledger UPDATE, no anchor, no
// file), so a re-query always re-derives the same answer from the same evidence;
// no `git log`/reflog forensics, which are not needed once ④ answers; and no
// verdict when the reading is inconclusive — an unreadable session, a missing
// registration timestamp or a detached-only ground is reported as NOT
// reconciled, never as "no owner", so the miss line (Rule 15b) says what was
// looked for and where.

package tools

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// The reconciliation's sentences. Named once (Rule 1b): the provenance line the
// lookup prints for a reconstructed owner, and the Rule-15b miss line, are read
// by the operator and asserted by the tests, so they cannot drift apart.
const (
	// taskGroundReconcileProvenancePrefix opens the provenance sentence of a
	// RECONSTRUCTED owner — the marker that tells a reader this owner came from
	// evidence, not from a recorded axis (a grep for it finds every such answer).
	taskGroundReconcileProvenancePrefix = "reconciled ground (not recorded on the row): "
	// taskGroundReconcileWhyFmt names ④ and ③: the session, the command, when it
	// ran, and the row's own life window.
	taskGroundReconcileWhyFmt = "the row's session %s ran `git worktree add %s -b %s` at %s, inside the row's life %s"
	// taskGroundReconcileRegistrationFmt names ① and ②: git's own registration of
	// the ground, in the repository the row records.
	taskGroundReconcileRegistrationFmt = " and %s still registers that directory checked out at %s (tip %s)"
	// taskGroundReconcileLookedFoundFmt is the miss line's cousin: what the
	// reconciliation measured when it DID name a row.
	taskGroundReconcileLookedFoundFmt = "repository %s registers the %s %s at %s; %d row(s) of that repository were alive when the registration was created and session %s records minting it"
	// taskGroundReconcileLookedMissFmt is the Rule-15b line: WHAT was looked for
	// (the registration, the rows alive at its birth, the session commands) and
	// WHERE, so "no owner" is never a blank assertion.
	taskGroundReconcileLookedMissFmt = "reconciled against the git side: repository %s registers the %s %s at %s (%s), and %s"
	// taskGroundReconcileNoCandidateFmt is the sub-sentence for the ordinary miss:
	// none of the rows alive at the registration's birth recorded minting it.
	taskGroundReconcileNoCandidateFmt = "none of the %d row(s) of that repository alive when the registration was created recorded `git worktree add … -b %s` in its session (%s)"
	// taskGroundReconcileNoSessionRowFmt is the sub-sentence for the case that
	// cannot be answered at all: the rows alive at that moment record no session.
	taskGroundReconcileNoSessionRowFmt = "the %d row(s) of that repository alive when the registration was created record no session, so no session command could be read"
	// taskGroundReconcileNoRegistrationFmt is the sub-sentence for the branch
	// axis when the repo git side showed no registration at all.
	taskGroundReconcileNoRegistrationFmt = "no repository read (%s) registers the %s %q — there is no ground to reconcile against"
	// taskGroundReconcileNoBranchFmt is the sub-sentence for a WORKTREE ground
	// git reports with no branch (a detached checkout): the session command that
	// mints a ground names the branch it created, so a detached ground cannot be
	// reconciled this way — said out loud rather than answered wrongly.
	taskGroundReconcileNoBranchFmt = "repository %s registers the worktree %s, but with no branch checked out (detached), and the minting command a session records names the branch it created — this ground cannot be reconciled from that evidence"
	// taskGroundReconcileRegistrationAge unknown: the registration exists but its
	// birth could not be measured (the same honest limit branchAttributionCheckoutAge
	// reports), so ③ cannot be applied and no row may be named.
	taskGroundReconcileNoBirthFmt = "repository %s registers the %s %s at %s, but when that registration was created could not be read (%s), so no row can be shown to have been alive at the time — nothing may be concluded from an unmeasurable window"
)

// taskGroundReconcileAnswer is the whole answer of one reconciliation pass: the
// row the evidence names (when one did), the ground that was measured, and the
// Rule-15b sentence describing the search.
type taskGroundReconcileAnswer struct {
	// Found reports that Owner is a row the evidence names.
	Found bool
	// Owner is the reconstructed owner row (zero value when !Found).
	Owner TaskForBranchOwner
	// Repo/Worktree/Branch/Tip describe the ground that was measured ("" when
	// git registered no such ground).
	Repo     string
	Worktree string
	Branch   string
	Tip      string
	// GroundKnown reports that a LIVE registration of the asked value was found
	// — the operator-visible difference between "there is a ground nobody owns"
	// and "there is no ground at all".
	GroundKnown bool
	// Looked is the ONE sentence the lookup renders: what was measured, where,
	// and — when nothing matched — why.
	Looked string
	// Matched is how many rows the session evidence named (the answer is
	// ambiguous when this is > 1, and the caller reports it rather than hiding
	// the choice).
	Matched int
}

// taskGroundReconcileQuery is the parsed input of one pass: the asked value
// (exactly one of branch/worktree), plus the optional repo narrowing the caller
// already carries.
type taskGroundReconcileQuery struct {
	Branch   string
	Worktree string
	Repo     string
}

// taskGroundReconcile is the ONE entry point: it MEASURES the ground the asked
// selector names and then reconstructs the owning row from the evidence above.
//
// It is a pure read: one ledger SELECT, `git worktree list` per candidate
// repository, one stat per registration and one session file per candidate row.
func taskGroundReconcile(ctx context.Context, db *sql.DB, q taskGroundReconcileQuery) taskGroundReconcileAnswer {
	var out taskGroundReconcileAnswer
	branch := strings.TrimSpace(q.Branch)
	worktree := strings.TrimSpace(q.Worktree)
	if branch == "" && worktree == "" {
		return out // nothing asked: nothing to reconcile (the caller refused earlier)
	}
	// ① the ground, as git reports it. A BRANCH question is answered from the
	// repositories git can see (the declared table, then the working directory —
	// the same candidate order the git-provenance reader uses); a WORKTREE
	// question carries its own repository in the path itself.
	repo, wtPath, wtBranch, tip, known, note := taskGroundReconcileFindGround(q)
	if !known {
		out.Looked = note
		return out
	}
	out.Repo, out.Worktree, out.Branch, out.Tip, out.GroundKnown = repo, wtPath, wtBranch, tip, true
	if wtBranch == "" {
		// A detached registration: ④ can never match (the minting command names a
		// branch), and answering from weaker evidence is exactly what this file
		// refuses to do.
		out.Looked = fmt.Sprintf(taskGroundReconcileNoBranchFmt, repo, wtPath)
		return out
	}
	// ③ the registration's BIRTH, from git's own bookkeeping (the ONE reader).
	birth, birthKnown := taskGroundReconcileRegistrationTime(repo, wtPath)
	if !birthKnown {
		out.Looked = fmt.Sprintf(taskGroundReconcileNoBirthFmt,
			repo, taskGroundReconcileKindWord(q), taskGroundReconcileValueWord(q), wtPath,
			taskGroundReconcileRegistrationClockUnknown)
		return out
	}
	// ②③ the candidate rows: same repository, alive when the registration was
	// created. Read once, with the times this pass needs (the deliverable scan
	// deliberately carries the LOOKUP's own fields only — see
	// taskDeliverableScanRows — so the window axis is read here rather than
	// widening every consumer's owner construction).
	cands, scanErr := taskGroundReconcileCandidates(ctx, db, repo, birth, q.Repo)
	if scanErr != nil {
		out.Looked = fmt.Sprintf(taskGroundReconcileScanErrFmt, repo, scanErr)
		return out
	}
	var withSession int
	var matches []taskGroundReconcileMatch
	for _, c := range cands {
		withSession++
		ts, ok := taskGroundReconcileSessionMinted(c.sessionID, c.taskID, wtPath, wtBranch, c.createdAt, c.endAt)
		if !ok {
			continue
		}
		matches = append(matches, taskGroundReconcileMatch{row: c, at: ts})
	}
	if len(matches) == 0 {
		if withSession == 0 {
			out.Looked = fmt.Sprintf(taskGroundReconcileLookedMissFmt, repo,
				taskGroundReconcileKindWord(q), taskGroundReconcileValueWord(q), wtPath, tip,
				fmt.Sprintf(taskGroundReconcileNoSessionRowFmt, len(cands)))
			return out
		}
		out.Looked = fmt.Sprintf(taskGroundReconcileLookedMissFmt, repo,
			taskGroundReconcileKindWord(q), taskGroundReconcileValueWord(q), wtPath, tip,
			fmt.Sprintf(taskGroundReconcileNoCandidateFmt, len(cands), wtBranch,
				taskGroundReconcileSessionText(withSession)))
		return out
	}
	// Newest match wins, deterministically (the same tie-break the owner family
	// uses for a shared selector: newest task_id first). The count travels so an
	// ambiguous answer is reported, never hidden.
	best := matches[0]
	for _, m := range matches[1:] {
		if m.row.createdAt.After(best.row.createdAt) {
			best = m
		}
	}
	out.Found, out.Matched = true, len(matches)
	out.Owner = TaskForBranchOwner{
		TaskID:           best.row.taskID,
		Status:           best.row.status,
		SessionID:        best.row.sessionID,
		Worktree:         best.row.worktree,
		Branch:           best.row.branch,
		Repo:             best.row.repo,
		ClaimedBy:        best.row.claimedBy,
		Terminal:         taskTerminalStatus(best.row.status),
		Live:             isLiveTaskStatus(best.row.status),
		HolderProvenDead: finishCleanupDeadLocalHolder(best.row.status, best.row.claimedBy, best.row.ident),
		Provenance: taskGroundReconcileProvenancePrefix +
			fmt.Sprintf(taskGroundReconcileWhyFmt, best.row.sessionID, wtPath, wtBranch,
				best.at.UTC().Format(time.RFC3339), best.row.lifeText()) +
			fmt.Sprintf(taskGroundReconcileRegistrationFmt, repo, wtPath, tip),
	}
	out.Looked = fmt.Sprintf(taskGroundReconcileLookedFoundFmt, repo,
		taskGroundReconcileKindWord(q), taskGroundReconcileValueWord(q), wtPath,
		len(cands), best.row.sessionID)
	return out
}

// taskGroundReconcileMatch is one candidate row whose session recorded minting
// the ground, with the instant the command ran.
type taskGroundReconcileMatch struct {
	row taskGroundReconcileRow
	at  time.Time
}

// taskGroundReconcileRow is one candidate ledger row: the deliverable axes the
// evidence pairs against, its session, and the LIFE WINDOW ③ needs.
type taskGroundReconcileRow struct {
	taskID    string
	status    string
	sessionID string
	repo      string
	branch    string
	worktree  string
	claimedBy string
	ident     procIdentity
	createdAt time.Time
	endAt     time.Time
}

// lifeText renders the window ③ applied, as the operator reads it.
func (r taskGroundReconcileRow) lifeText() string {
	return r.createdAt.UTC().Format(time.RFC3339) + "–" + r.endAt.UTC().Format(time.RFC3339)
}

// taskGroundReconcileKindWord / ValueWord name the asked selector so every
// sentence this file renders says WHICH question it answered (Rule 15 — the
// same discipline taskOwnerSubject follows).
func taskGroundReconcileKindWord(q taskGroundReconcileQuery) string {
	if strings.TrimSpace(q.Worktree) != "" {
		return deliverableSelectorWorktreeKind
	}
	return deliverableSelectorBranchKind
}

func taskGroundReconcileValueWord(q taskGroundReconcileQuery) string {
	if w := strings.TrimSpace(q.Worktree); w != "" {
		return w
	}
	return strings.TrimSpace(q.Branch)
}

// taskGroundReconcileSessionText names how many of the candidate rows carried a
// session at all — part of the miss line's honesty: "none of the rows that HAD a
// session recorded the command" and "no row recorded a session" are different
// findings.
func taskGroundReconcileSessionText(n int) string {
	return fmt.Sprintf("%d of them record a session", n)
}

// taskGroundReconcileFindGround measures the asked ground with git: which
// repository registers it, at which directory, on which branch, at which tip.
//
// A BRANCH question is asked of the repositories git can name without the
// ledger (the declared table — Rule 12's contract — then the working directory),
// which is exactly the candidate pair TaskGitProvenanceFor falls back to. A
// WORKTREE question carries its own repository: the path's own root, then the
// main checkout git answers for it, then the same declared/cwd pair.
//
// Every "no" returns its own sentence (Rule 15b), never a bare false.
func taskGroundReconcileFindGround(q taskGroundReconcileQuery) (repo, worktree, branch, tip string, ok bool, note string) {
	kind, value := taskGroundReconcileKindWord(q), taskGroundReconcileValueWord(q)
	if strings.TrimSpace(q.Worktree) != "" {
		if root, err := repoRootOf(value); err == nil && root != "" {
			if common, cerr := worktreeSweepCommonGitDir(root); cerr == nil {
				if main := worktreeSweepMainCheckoutOf(common); main != "" {
					if p, b, found := taskGroundReconcileRegisteredAt(main, value); found {
						return main, p, b, repoSweepReadTip(main, b), true, ""
					}
				}
			}
		}
	}
	for _, cand := range taskGroundReconcileRepos(q.Repo) {
		switch {
		case strings.TrimSpace(q.Worktree) != "":
			if p, b, found := taskGroundReconcileRegisteredAt(cand, value); found {
				return cand, p, b, repoSweepReadTip(cand, b), true, ""
			}
		default:
			if p, found := taskGroundResolveList(cand, value); found {
				return cand, p, value, repoSweepReadTip(cand, value), true, ""
			}
		}
	}
	return "", "", "", "", false, fmt.Sprintf(taskGroundReconcileNoRegistrationFmt,
		strings.Join(taskGroundReconcileRepos(q.Repo), ", "), kind, value)
}

// taskGroundReconcileRegisteredAt finds the LIVE registration of a worktree
// path in repo: the directory git checks out there and the branch it holds.
// Both facts come from the ONE porcelain reader (taskRepoLinkedWorktrees), so
// this pass and the resume path can never disagree about where a ground is.
func taskGroundReconcileRegisteredAt(repo, worktree string) (path, branch string, ok bool) {
	linked, err := taskRepoLinkedWorktrees(repo)
	if err != nil {
		return "", "", false
	}
	for _, e := range linked {
		if e.path != "" && sameWorkingPath(e.path, worktree) {
			return e.path, e.branch, true
		}
	}
	return "", "", false
}

// taskGroundReconcileRepos is the repository candidate list a BRANCH question is
// asked of, in precedence order: the caller's narrowing, the declared repos
// table (Rule 12), then the working directory. Deduped by path key (the lookup
// family's own comparison), so one repository read once.
func taskGroundReconcileRepos(narrow string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		key := taskWorktreePathKey(p)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, p)
	}
	add(narrow)
	if declared, err := config.ResolveMergerRepos(); err == nil {
		for _, r := range declared {
			add(r)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		add(cwd)
	}
	return out
}

// taskGroundReconcileRegistrationTime is ③'s measurement: WHEN git created the
// registration of this worktree. It reuses the ONE reader of that fact
// (branchAttributionCheckoutAge, which measures the admin dir's gitdir mtime)
// and converts its age into the instant, so the two consumers of "how old is
// this checkout" cannot disagree.
func taskGroundReconcileRegistrationTime(repo, worktree string) (time.Time, bool) {
	age, known := branchAttributionCheckoutAge(repo, worktree)
	if !known {
		return time.Time{}, false
	}
	return branchAttributionNow().Add(-age), true
}

// taskGroundReconcileCandidates reads the rows ②③ admit: same repository as the
// ground, and alive when the registration was created. The window is
// [created_at, end], where end is ended_at when the row has one and its last
// update otherwise — a row with neither cannot be shown to have been alive, and
// is left out (never guessed in).
//
// It carries its OWN SELECT because the columns it needs (the life window) are
// not the lookup family's — taskDeliverableScanRows deliberately carries only
// the fields an owner is built from, and widening it would change every
// consumer of that scan (Rule 14: a second SELECT with a DIFFERENT purpose is
// not a second answer to the same question; the parse of the blob is still the
// ONE parser, taskDeliverableParse).
func taskGroundReconcileCandidates(ctx context.Context, db *sql.DB, repo string, birth time.Time, narrow string) ([]taskGroundReconcileRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, COALESCE(session_id,''), COALESCE(deliverable,''),
		        COALESCE(claimed_by,''), COALESCE(`+tasksProcPidColumnName+`,0),
		        COALESCE(`+tasksProcStartColumnName+`,0), COALESCE(`+tasksProcCmdColumnName+`,''),
		        created_at, COALESCE(ended_at,0), COALESCE(updated_at,0)
		   FROM tasks
		  WHERE COALESCE(deliverable,'') <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	birthUnix := float64(birth.Unix())
	var out []taskGroundReconcileRow
	for rows.Next() {
		var id, status, sessionID, deliverable, claimedBy, procCmd string
		var procPid, procStart int64
		var createdAt, endedAt, updatedAt float64
		if err := rows.Scan(&id, &status, &sessionID, &deliverable, &claimedBy, &procPid, &procStart, &procCmd,
			&createdAt, &endedAt, &updatedAt); err != nil {
			return nil, err
		}
		d := taskDeliverableParse(deliverable)
		if !sameWorkingPath(d.Repo, repo) {
			continue // ② this row does not work in the repository that registers the ground
		}
		if narrow != "" && !sameWorkingPath(d.Repo, narrow) {
			continue // the caller's own narrowing (the same filter its ledger scan used)
		}
		end := endedAt
		if end <= 0 {
			end = updatedAt
		}
		if createdAt <= 0 || end <= 0 {
			continue // no measurable life: ③ cannot be applied, so the row is not a candidate
		}
		if birthUnix < createdAt || birthUnix > end {
			continue // ③ the row was not alive when the ground was created
		}
		out = append(out, taskGroundReconcileRow{
			taskID: id, status: status, sessionID: strings.TrimSpace(sessionID),
			repo: d.Repo, branch: d.Branch, worktree: d.Worktree, claimedBy: claimedBy,
			ident:     procIdentity{Pid: int(procPid), StartTicks: procStart, Cmd: procCmd},
			createdAt: time.Unix(int64(createdAt), 0), endAt: time.Unix(int64(end), 0),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// taskGroundReconcileSessionMinted is ④: the row's OWN session records
// `git worktree add <the ground> -b <the branch>`, and the command's own
// timestamp falls inside the row's life window.
//
// The parser is the ONE parser of that command (taskWorktreeAddBranchFromCommand,
// the same one the creation seam and the completion-time backfill use), and the
// session file is found by the ONE session resolver (taskFindSessionJSONL), so
// this pass cannot read a different file or a different command shape than the
// writers do.
//
// The DIRECTORY is checked as well as the branch: `git worktree add A -b x` and
// `git worktree add B -b x` are two different grounds, and a row that minted
// another directory on the same branch name is not the owner of this one. The
// command string is the evidence, so the directory is compared by field
// containment (the command literally names it) — a mention is NOT enough on its
// own, which is why the registration (①), the repository (②) and the window (③)
// all have to hold too.
func taskGroundReconcileSessionMinted(sessionID, taskID, worktree, branch string, createdAt, endAt time.Time) (time.Time, bool) {
	gd, err := config.GopiDir()
	if err != nil {
		return time.Time{}, false
	}
	root := filepath.Join(gd, config.SessionsDirName)
	// THE RUN'S OWN RECORD. Two layouts are read: the per-task run directory
	// (sessions/<task_id>/<uuid>.jsonl — where a task run's record actually
	// lives) and, failing that, the session named by the row's session_id. BOTH
	// are scanned rather than "the first one found": a resumed task has more
	// than one run record, and the minting command may be in any of them.
	paths := taskFindSessionJSONLByTask(root, taskID)
	if p := taskFindSessionJSONL(root, sessionID); p != "" {
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		return time.Time{}, false
	}
	var found time.Time
	for _, path := range paths {
		if at, ok := taskGroundReconcileScanSession(path, worktree, branch, createdAt, endAt); ok {
			found = at // chronological LAST minting moment wins
		}
	}
	if found.IsZero() {
		return time.Time{}, false
	}
	return found, true
}

// taskGroundReconcileScanSession scans ONE session JSONL for the minting of a
// ground inside a life window (④), returning the instant of the LAST such
// command in the file.
func taskGroundReconcileScanSession(path, worktree, branch string, createdAt, endAt time.Time) (time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 20<<20)
	var found time.Time
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte("tool_use")) {
			continue
		}
		ground := false
		for _, needle := range taskGroundToolNeedles {
			if bytes.Contains(line, needle) {
				ground = true
				break
			}
		}
		if !ground {
			continue
		}
		var ev struct {
			Timestamp string `json:"timestamp"`
			Message   struct {
				Content []struct {
					Type  string `json:"type"`
					Name  string `json:"name"`
					Input struct {
						Action  string `json:"action"`
						Command string `json:"command"`
					} `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		at, atErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(ev.Timestamp))
		if atErr != nil {
			continue // an unmeasurable moment cannot be placed in the window
		}
		if at.Before(createdAt) || at.After(endAt) {
			continue
		}
		for _, b := range ev.Message.Content {
			if b.Type != "tool_use" {
				continue
			}
			command, ok := taskGroundCommandBlock(b.Name, b.Input.Action, b.Input.Command)
			if !ok {
				continue
			}
			if taskWorktreeAddBranchFromCommand(command) != branch {
				continue
			}
			if !taskWorktreeAddNamesDir(command, worktree) {
				continue
			}
			found = at
		}
	}
	if found.IsZero() {
		return time.Time{}, false
	}
	return found, true
}

// taskWorktreeAddNamesDir reports whether command's `git worktree add` names the
// given directory. The comparison is a FIELD test (the same Fields() split the
// branch parser uses) against the path and against its basename, so
// `/home/user/workspace/gopi-x` matches both the absolute spelling and the
// `~/workspace/gopi-x`/relative one a run may have typed — the registration is
// what pins the identity, this predicate only asks whether the command's own
// words point at it.
func taskWorktreeAddNamesDir(command, worktree string) bool {
	want := strings.TrimSpace(worktree)
	if want == "" {
		return false
	}
	wantKey := taskWorktreePathKey(want)
	base := filepath.Base(strings.TrimRight(want, string(os.PathSeparator)))
	for _, f := range strings.Fields(command) {
		f = strings.TrimRight(strings.TrimSpace(f), ";&|")
		f = strings.Trim(f, `"'`)
		if f == "" {
			continue
		}
		if strings.HasPrefix(f, "-") {
			continue
		}
		if f == want || filepath.Base(f) == base {
			return true
		}
		if key := taskWorktreePathKey(strings.Replace(f, "~", os.Getenv("HOME"), 1)); key != "" && key == wantKey {
			return true
		}
	}
	return false
}

// taskGroundReconcileRegistrationClockUnknown is the honest word for ③'s
// unmeasurable half (the same discipline branchAttributionCheckoutAge follows:
// an unknown age prints nothing rather than a zero).
const taskGroundReconcileRegistrationClockUnknown = "the registration's birth could not be measured"

// taskGroundReconcileScanErrFmt is the read failure's sentence: an unreadable
// ledger is never "no candidate row" (Rule 15).
const taskGroundReconcileScanErrFmt = "the rows of repository %s could not be read (%v), so no row could be checked against the registration"
