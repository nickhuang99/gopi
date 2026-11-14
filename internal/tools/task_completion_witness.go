package tools

// task_completion_witness.go — THE COMPLETION-WITNESS CRITERION: can this row's
// `completed` claim be told apart from a claim that delivered nothing?
//
// INCIDENT (epic 01a182bf, filed on row 01a18253-f7c4-7cf7-8cb3-868647a25401).
// The row read `completed`, and every address the ledger names for it was gone:
//
//	$ git -C /home/user/work/gopi rev-parse --verify --quiet refs/heads/task/01a18253f7c47cf78cb3868647a25401
//	(no output, exit 1)
//	$ git -C /home/user/work/gopi rev-parse --verify --quiet refs/remotes/origin/task/01a18253f7c47cf78cb3868647a25401
//	(no output, exit 1)
//	$ ls -d /home/user/workspace/01a18253-f7c4-7cf7-8cb3-868647a25401
//	ls: cannot access '…': No such file or directory
//
// That state is NORMAL after a delivered close-out (the clone channel publishes
// HEAD:master and reclaims the ground; finish-cleanup removes the address ref),
// so ref-absence alone proves nothing. What makes the row readable is a WITNESS
// elsewhere — and on this row one exists: the commit the channel recorded
// pushing carries the row's id in its own `task:` trailer.
//
//	$ git -C /home/user/work/gopi show -s --format='%(trailers)' 20e8328
//	task: 01a18253-f7c4-7cf7-8cb3-868647a25401
//
// THE CRITERION. A `completed` writeback asks ONE question — does ANY artefact
// outside this row's own reason say the run produced something? Three witnesses
// are asked, in the order of how directly each one knows it:
//
//	ref      a ref in the row's repo names its recorded deliverable branch;
//	landed   the clone channel recorded a landed push naming a commit the repo
//	         still holds (TaskDeliveryRecordLanded's grammar);
//	commit   a commit reachable from any ref carries this row's id in its
//	         `task:` trailer or its subject (an index that survives both the
//	         ground's deletion and the address ref's removal).
//
// `witnessless` means none of them answered — NOT "the work is lost". It is a
// RECORD, never a refusal, and it never changes the status being written: a
// `completed` row that cannot be witnessed is still `completed` (turning it
// `failed` would re-open the very defect row 01a18244 fixed, where an items=0
// counter-proof run was recorded as a failed delivery). What the record buys is
// the distinction the epic requires: `completed` with a conclusion, versus
// `completed` with nothing behind it, readable on the row itself without
// re-running anything.
//
// WHAT IT DOES NOT DO. It writes no ref, deletes nothing, pushes nothing, and
// it never rewrites a report. The one thing it can write is ONE attributed
// trail entry, on the `witnessless` verdict only (Rule 15's loud half).
//
// THREE-VALUED, like every other close-out arm here: an unreadable repository,
// an unreadable ledger or an unreadable trail is `unknown` and is REPORTED —
// a guard that reads "could not look" as "nothing there" would accuse every row
// on a host whose ledger is briefly locked.

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// The criterion's names (Rules 1b/13: one spelling each, referenced everywhere).
const (
	// CompletionWitnesslessEvent is the attributed trail record for a
	// `completed` claim with no witness behind it. It is appended AFTER the
	// terminal write lands, so it annotates the completed row rather than
	// blocking it.
	CompletionWitnesslessEvent = "completed-without-witness"
	// CompletionWitnesslessMarker is the phrase the recorded sentence carries,
	// so an operator (or a test) matches it without knowing the words around it.
	CompletionWitnesslessMarker = "no witness says this run produced anything"
	// completionWitnessConcludedMarker is the same handle for the OTHER shape:
	// a completion that rests on a measured conclusion. It is deliberately a
	// different phrase, because the whole point of the verdict is that an
	// operator can tell the two apart without opening the log.
	completionWitnessConcludedMarker = "the completion rests on a measured conclusion, not on a delivery"
	// completionWitnessRefPrefix is the ref prefix a branch name is matched
	// against when asking the `ref` arm: a local head or any remote's head.
	completionWitnessRefBranchSep = "/"
	// completionWitnessRecordLimit is how many IDENTICAL witnessless entries a
	// row may ever carry: ONE. (The residue arm's 3 is that arm's own brake; a
	// second definition here would be a second meaning — Rule 13.)
	completionWitnessRecordLimit = 1
	// completionWitnessHeadRef is what the witnessless sentence asks the row's
	// recorded ground for: the commit it stands on, which is the address an
	// operator needs when the work never reached the destination.
	completionWitnessHeadRef = "HEAD"
)

// The four verdicts. `witnessless` is the defect class; `witnessed` is an
// ordinary delivered close-out; `concluded` is the shape the epic requires this
// arm to be able to tell apart from it — a completion that rests on a
// CONCLUSION the channel itself measured rather than on a delivery (the
// nothing-to-deliver exit's `rev-list --count … → 0`); `unknown` is the honest
// non-answer.
const (
	CompletionWitnessed     = "witnessed"
	CompletionConcluded     = "concluded"
	CompletionWitnessless   = "witnessless"
	completionWitnessUnkown = "unknown"
)

// CompletionWitness is ONE row's reading, carrying every measurement behind the
// verdict so a reviewer can re-check it without re-running the arm.
type CompletionWitness struct {
	TaskID   string
	Verdict  string
	Repo     string
	Branch   string
	Worktree string
	// Witnesses are the artefacts that answered, each as "<arm>:<value>".
	Witnesses []string
	// Conclusion is the measurement a `concluded` verdict rests on — the
	// nothing-to-deliver exit's own sentence ("" when the caller is a delivery
	// claim, which is the case this arm must judge rather than excuse).
	Conclusion string
	// Note is the sentence the row carries when the verdict needs explaining.
	Note string
}

// TaskCompletionWitness judges whether a row that is claiming a delivery has any
// artefact behind it. It is exported so the evidence can be re-taken from a test
// or a session; the close-out channel is its production caller.
//
// A row with no recorded repository is NOT witnessed and NOT accused: it is
// `unknown` with the reason stated, because "this row names no repo" is a
// different fact from "this row's repo holds nothing" (Rule 15).
func TaskCompletionWitness(taskID string) (CompletionWitness, error) {
	return TaskCompletionWitnessOn(taskID, "", "")
}

// TaskCompletionWitnessOn is the door the close-out takes: the same judgement,
// with the two facts the CALLER knows and this package cannot derive —
//
//	status      what the row is claiming (an arm that is not a delivery claim is
//	            not judged at all);
//	conclusion  the measurement a `nothing to deliver` exit took, verbatim. It is
//	            what makes `concluded` answerable: a completion that rests on a
//	            count the channel took is a DIFFERENT fact from a completion with
//	            nothing behind it, and the epic's requirement is that the ledger
//	            can tell the two apart (row 01a18244 fixed the first, this arm
//	            names the second).
//
// A non-empty conclusion is never overridden by a witness (the two are not
// exclusive: a run may legitimately commit nothing AND have an earlier landed
// push); it is carried on the outcome so a reader sees both.
func TaskCompletionWitnessOn(taskID, status, conclusion string) (CompletionWitness, error) {
	out := CompletionWitness{TaskID: taskID, Conclusion: strings.TrimSpace(conclusion)}
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return out, fmt.Errorf("completion witness: task_id must not be empty")
	}
	out.TaskID = taskID
	repo, branch, worktree := taskRowDeliverable(taskID)
	out.Repo, out.Branch, out.Worktree = repo, branch, worktree
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()

	// ① THE LIVE REF — the ordinary mid-flight close-out. A branch that still
	// resolves is its own witness.
	if branch != "" && repo != "" {
		if tip := branchDeleteTipOf(ctx, repo, branch); tip != "" {
			out.Verdict = CompletionWitnessed
			out.Witnesses = append(out.Witnesses, "ref:"+branch+"="+branchDeleteShortHash(tip))
			return out, nil
		}
		refs, err := branchDeleteListRefs(ctx, repo)
		if err == nil {
			for _, ref := range refs {
				if strings.HasSuffix(ref, completionWitnessRefBranchSep+branch) {
					out.Verdict = CompletionWitnessed
					out.Witnesses = append(out.Witnesses, "ref:"+ref)
					break
				}
			}
			if out.Verdict == CompletionWitnessed {
				return out, nil
			}
		}
		// AN UNREADABLE REF NAMESPACE IS NOT "NO REF HOLDS IT" — it falls
		// through to the other arms, and if they are also silent the verdict is
		// `unknown`, never `witnessless` (the err is carried into the note).
		if err != nil {
			out.Note = "the repository's refs could not be read (" + err.Error() + ")"
		}
	}

	// ② THE CHANNEL'S OWN LANDED RECORD — the arm that needs no directory. It
	// names the commit the channel pushed; the commit must still exist in the
	// repo before it counts, because a record about a vanished object is not
	// evidence that anything survived.
	if branch != "" && repo != "" {
		if tip := deliveryProofLandedTip(taskID, branch); tip != "" {
			if deliveryProofCommitReadable(ctx, repo, tip) {
				out.Verdict = CompletionWitnessed
				out.Witnesses = append(out.Witnesses, "landed:"+branchDeleteShortHash(tip))
				return out, nil
			}
		}
	}

	// ③ THE COMMIT INDEX — a commit reachable from any ref names this row. This
	// is the witness that survives the ground AND the address ref, and it is the
	// `task:` trailer (internal/tools/git_commit_task_trailer.go) that makes it
	// answerable at all.
	if repo != "" {
		named, raw, err := completionWitnessNamedCommits(ctx, repo)
		switch {
		case err != nil:
			out.Note = "the reachable history could not be read (" + err.Error() + ")"
		case named[taskID] != "":
			out.Verdict = CompletionWitnessed
			out.Witnesses = append(out.Witnesses, "commit:"+branchDeleteShortHash(named[taskID]))
			return out, nil
		case strings.TrimSpace(raw) == "":
			out.Note = "the repository holds no commits at all, so no commit can name this row"
		}
	}

	if repo == "" {
		out.Verdict = completionWitnessUnkown
		out.Note = "the row records no repository, so there is no git history to ask"
		return out, nil
	}
	if out.Note != "" {
		// One of the reads refused: the honest non-answer, never an accusation.
		out.Verdict = completionWitnessUnkown
		return out, nil
	}
	// THE CONCLUSION IS ITS OWN EVIDENCE, and it is the distinction the epic
	// requires: this row completed by MEASURING that it had nothing to publish,
	// which is a conclusion about the ground rather than an unbacked claim. It is
	// recorded as such and never as the defect.
	if out.Conclusion != "" {
		out.Verdict = CompletionConcluded
		out.Note = completionConcludedNote(out)
		return out, nil
	}
	out.Verdict = CompletionWitnessless
	out.Note = completionWitnesslessNote(out)
	return out, nil
}

// completionConcludedNote names the conclusion a `concluded` completion rests
// on, so a reader of the row can tell it from both a delivered close-out and a
// witnessless one.
func completionConcludedNote(out CompletionWitness) string {
	return fmt.Sprintf("run %s claims `%s` for %s on a CONCLUSION rather than on a delivery — %s: branch %s resolves to no ref, no reachable commit names this row, and the completion rests on the channel's own measurement: %s",
		out.TaskID, taskStatusCompleted, orUnknownRepo(out.Repo),
		completionWitnessConcludedMarker, orNoneBranch(out.Branch), out.Conclusion)
}

// completionWitnesslessNote is the defect sentence: WHO makes the claim, WHICH
// branch it named, WHAT was asked and found silent, and WHERE the work would be
// if it was never published — so a reader cannot mistake it for "the work is
// lost" (the arm deletes and restores nothing) nor for a refusal (the status
// still stands).
//
// THE GROUND AND ITS HEAD ARE NAMED (bug 01a193f8, measured 2026-10-31 on rows
// 01a190bb / 01a190b4). The pre-fix sentence said only that nothing was found,
// which left an operator with commits stranded in a clone and no pointer to
// where they were: the two samples' sessions each had to be opened and their
// logs read by hand to retrieve ONE fact this arm already had in hand. So the
// sentence carries the row's recorded ground and, when that directory still
// exists, the commit it stands on — the exact recovery address. A ground that is
// gone is stated as gone rather than omitted: "there is nothing to point at" and
// "nobody looked" are different facts (Rule 15).
func completionWitnesslessNote(out CompletionWitness) string {
	where := completionWitnessGroundAddress(out)
	return fmt.Sprintf("run %s claims `%s` for %s, but %s: branch %s resolves to no ref, the channel recorded no landed push naming a commit this repo still holds, and no reachable commit names this row in its `%s:` trailer or subject — %s; this RECORDS the absence and changes no status (the claim stands, the work is neither proven nor deleted)",
		out.TaskID, taskStatusCompleted, orUnknownRepo(out.Repo), CompletionWitnesslessMarker,
		orNoneBranch(out.Branch), TaskTrailerKey, where)
}

// completionWitnessGroundAddress renders WHERE the row's unpublished work would
// be: the recorded ground and the commit it stands on when that directory is
// still there, or an explicit statement that it is not.
//
// It is deliberately a READ of the ground and never a claim about the delivery:
// a HEAD inside a directory is evidence of what that directory holds, not of
// what reached a peer — which is exactly the distinction the verdict exists to
// keep.
func completionWitnessGroundAddress(out CompletionWitness) string {
	ground := strings.TrimSpace(out.Worktree)
	if ground == "" {
		return "the row records no ground, so this arm cannot name where its work would be"
	}
	if _, err := os.Stat(ground); err != nil {
		return fmt.Sprintf("the recorded ground %s is no longer on disk (%v), so the commits it held are only recoverable from its remote", ground, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	if head := branchDeleteRevParse(ctx, ground, completionWitnessHeadRef); head != "" {
		return fmt.Sprintf("the recorded ground %s still stands on %s — a commit the destination does not have names the gap", ground, branchDeleteShortHash(head))
	}
	return fmt.Sprintf("the recorded ground %s is still on disk but its HEAD could not be read, so its unpublished work cannot be named from here", ground)
}

func orUnknownRepo(repo string) string {
	if strings.TrimSpace(repo) == "" {
		return "no recorded repository"
	}
	return repo
}

func orNoneBranch(branch string) string {
	if strings.TrimSpace(branch) == "" {
		return "(none recorded)"
	}
	return branch
}

// completionWitnessNamedCommits indexes, ONCE per repo, every row id named by a
// commit reachable from any ref — from the `task:` TRAILER first (the reliable
// index a worker's own commit carries) and from the SUBJECT as well, through the
// same extractor the reachability arm uses (`taskRefIDsInSubject`, so the merger's
// `Merge … (re-lands 01a12ff2)` spelling is honoured by both). Rule 14: one
// extractor, two readers.
//
// The raw output is returned so a caller can tell "read fine, named nothing"
// from "the repository has no commits" — two different sentences.
func completionWitnessNamedCommits(ctx context.Context, repo string) (map[string]string, string, error) {
	out := map[string]string{}
	raw, err := taskRefGit(ctx, repo, taskRefLogVerb, taskRefAllFlag,
		taskRefFormatFlag+"="+completionWitnessLogFormat)
	if err != nil {
		return out, "", fmt.Errorf("read the reachable history of %s: %w", repo, err)
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, taskRefHistorySep, 2)
		if len(parts) != 2 {
			continue
		}
		sha := strings.TrimSpace(parts[0])
		subject, trailers := parts[1], ""
		if i := strings.Index(subject, taskRefHistorySep); i >= 0 {
			subject, trailers = subject[:i], subject[i+1:]
		}
		if sha == "" {
			continue
		}
		for _, id := range strings.Fields(trailers) {
			if _, seen := out[id]; !seen {
				out[id] = sha
			}
		}
		for _, id := range taskRefIDsInSubject(subject) {
			if _, seen := out[id]; !seen {
				out[id] = sha
			}
		}
	}
	return out, raw, nil
}

// completionWitnessLogFormat is the ONE format the index is read with: the sha,
// the unit separator, the subject, the unit separator, the `task:` trailer's
// value. It is a constant rather than a literal at the call site (Rule 1) and it
// is spelled from the separator constant the reachability arm already owns.
const completionWitnessLogFormat = "%H" + taskRefHistorySep + "%s" + taskRefHistorySep +
	"%(trailers:key=" + TaskTrailerKey + ",valueonly)"

// TaskCompletionWitnessAtWriteback is the RECORD half, called by a close-out
// that has just written a DELIVERY claim. It appends ONE attributed trail entry
// when the claim has no witness behind it, and does nothing at all for a
// witnessed row (the healthy case must stay silent — the trail is an exception
// log, not a heartbeat).
//
// It never changes the status that was already written: this arm is a READ plus
// one append, and the epic's constraint is explicit that `completed` must never
// be turned back into `failed` (row 01a18244 fixed a defect in exactly that
// direction).
func TaskCompletionWitnessAtWriteback(taskID, status, conclusion string) (CompletionWitness, error) {
	out, err := TaskCompletionWitnessOn(taskID, status, conclusion)
	if err != nil {
		taskNotifyLog("completion witness for %s: %v", taskID, err)
		return out, err
	}
	if !taskStatusClaimsDelivery(status) {
		return out, nil
	}
	switch out.Verdict {
	case CompletionWitnessed, CompletionConcluded:
		// The healthy shapes stay SILENT on the trail (it is an exception log,
		// not a heartbeat). `concluded` is reported on stderr so the operator
		// reading the run sees which of the two healthy shapes happened.
		if out.Verdict == CompletionConcluded {
			fmt.Fprintln(os.Stderr, "[task] "+out.Note)
		}
	case CompletionWitnessless:
		completionWitnessRecord(taskID, out.Note)
		fmt.Fprintln(os.Stderr, "[task] "+out.Note)
	case completionWitnessUnkown:
		// Reported, never swallowed, and never turned into an accusation.
		taskNotifyLog("completion witness for %s NOT judged: %s", taskID, out.Note)
		fmt.Fprintln(os.Stderr, "[task] "+out.Note)
	}
	return out, nil
}

// completionWitnessRecord appends the attributed trail entry, deduplicated the
// same way every other close-out record is (`taskEventCountForDetailOn` against
// the ONE cap), so a re-stamped row cannot drip a second identical sentence.
func completionWitnessRecord(taskID, note string) {
	if strings.TrimSpace(note) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("completion witness event %s: %v", taskID, err)
		return
	}
	defer db.Close()
	if taskEventCountForDetailOn(ctx, db, taskID, CompletionWitnesslessEvent, note) >= completionWitnessRecordLimit {
		return
	}
	taskEventWrite(ctx, db, taskID, CompletionWitnesslessEvent, note)
}
