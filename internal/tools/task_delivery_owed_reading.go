package tools

// task_delivery_owed_reading.go — THE "DELIVERY OWED" READING: does this row's
// ground hold commits that no remote-tracking ref reaches, and if so WHICH ones.
//
// THE DEFECT THIS ANSWERS (epic 01a1a8df). The clone channel publishes a run's
// work from its END hook, once. A run whose publish was refused, or that never
// reached the hook at all (a signal, a crash), leaves its commits in ONE place:
// its own clone's object store, on a branch no server ref names. The ledger then
// reads `failed` or `completed`, and NOTHING on either side says the delivery is
// outstanding — so the next sweep collects the ground and the commits are gone.
// Measured victims on this host: row 01a1a87b (HEAD b77a1882, 1 commit, never
// pushed) and row 01a1a876 (3 commits, survived only because somebody minted a
// rescue tag by hand).
//
// THE QUESTIONS, and who answers each (Rule 14 — no second spelling of a
// reader that already exists):
//
//	1. HOW MANY commits does the ground carry that no refs/remotes/ ref reaches?
//	   → unpublishedCommitProbe (the bash tool's own unpublished-commit notice
//	     asks this exact question with this exact command; reusing it is what
//	     stops two surfaces disagreeing about what "unpublished" means).
//	2. WHICH commit is HEAD there?                    → rev-parse HEAD
//	3. Is that tip already held by a RESCUE ref?      → for-each-ref on the
//	   archive/rescued- prefix the `--rescue-tip` face writes
//	   (cmd/agent/cli_archive.go owns the writer; this is the reader).
//	4. What did the PEER last answer?                 → deliveryRetryPeerTip
//	   (the retry path's own read-only `ls-remote`).
//
// WHAT IT IS NOT. It is NOT a writer: no ledger event, no ref, no push, no
// change of delivery timing. It is NOT a resident pass (Rule 35): it is called
// from two renders that already exist and from the one-shot sync pass.
//
// RULE 15, THE WHOLE POINT OF THE `ok` RETURNS: a git that refused, a directory
// that is not a checkout, a count that is not a number — every one of those is
// an UNREADABLE axis, and an unreadable axis is never rendered as zero. The
// sentence says what could not be read.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const (
	// TaskDeliveryOwedSuffixLabel is the tail's leading word on a rendered
	// `task list` row. It is the SAME word the ledger's recorded delivery-debt
	// line uses (TaskShowDeliveryOwedLabel), so a reader greps one spelling and
	// finds both faces — the recorded fact and the measured one.
	TaskDeliveryOwedSuffixLabel = TaskShowDeliveryOwedLabel

	// taskDeliveryOwedOpen / taskDeliveryOwedClose delimit the tail on a row,
	// matching the ground tail's own punctuation (tasklist_ground.go) so the
	// two read as one family.
	taskDeliveryOwedOpen  = " ["
	taskDeliveryOwedClose = "]"

	// taskDeliveryOwedAheadBodyFmt is the counted tail's body. Slots: count,
	// short sha, and (only when the row records one) the branch.
	taskDeliveryOwedAheadBodyFmt = "%d commit(s) ahead of every remote at %s"
	// taskDeliveryOwedBranchFmt appends the branch to that body.
	taskDeliveryOwedBranchFmt = " on %s"

	// taskDeliveryOwedUnreadableFmt is the tail when the probe could not be
	// answered. Slots: directory, reason. It is a DIFFERENT sentence on
	// purpose: "I could not measure" and "nothing is owed" must never print the
	// same thing (Rule 15).
	taskDeliveryOwedUnreadableFmt = "unreadable in %s (%s)"

	// THE VANISHED-TIP FACE (epic 01a1e58d). `ahead of every remote at <sha>`
	// reads as an ACTION INSTRUCTION — go and push that commit — and it is
	// executable only while <sha> resolves SOMEWHERE. When the tip no longer
	// exists even in the row's OWN ground there is nothing to push and no
	// recovery to perform, so the same label carries a DIFFERENT sentence: the
	// cause by name, and the decision the row now needs (a fresh dispatch, or an
	// explicit void). Slots: short sha, ground directory.
	//
	// Measured shape this faces: `git rev-parse HEAD` still answers (it reads the
	// ref, not the object) while `git cat-file -e <sha>^{commit}` fails — the torn
	// store a collected or interrupted clone leaves behind.
	taskDeliveryOwedGoneFmt = "gone — the tip %s no longer resolves in %s: nothing can be pushed from it, " +
		"so this row's owed work must be %s, or the row explicitly %s"

	// The three words that sentence is built from, named so the render, the
	// sibling face and the regression arms cannot drift apart (Rule 1/14).
	taskDeliveryOwedGoneWord       = "gone"
	taskDeliveryOwedRedispatchWord = "re-dispatched"
	taskDeliveryOwedVoidWord       = "voided"

	// taskDeliveryOwedCommitishSuffix peels a sha to the COMMIT it names, so a
	// tag or a tree of the same id cannot stand in for the object a push would
	// carry.
	taskDeliveryOwedCommitishSuffix = "^{commit}"

	// taskDeliveryOwedShortShaLen is how much of the sha the LIST row prints —
	// the row is one line and its tail must not push the summary off screen.
	// `task show` prints the full sha, which is what an operator pastes.
	taskDeliveryOwedShortShaLen = 12

	// taskDeliveryOwedRescueRefPrefix is the ref namespace the `--rescue-tip`
	// face writes a durable ref into (cmd/agent/cli_archive.go:519). Read, never
	// written, by this file — and never moved or deleted by any arm (the epic's
	// constraint).
	taskDeliveryOwedRescueRefPrefix = "refs/tags/archive/rescued-"

	// The git spellings this file runs. Each is taken from the constant that
	// already owns it where one exists (Rule 13/14).
	taskDeliveryOwedForEachRefVerb = "for-each-ref"
	taskDeliveryOwedPointsAtFlag   = "--points-at"
	taskDeliveryOwedFormatAtom     = "--format=%(refname) %(objectname)"
	taskDeliveryOwedShortShaVerb   = integrationClaimRevParseVerb
	taskDeliveryOwedShortFlag      = "--short=" + taskDeliveryOwedShortShaLenText
	// taskDeliveryOwedShortShaLenText is the flag's digit half, kept beside the
	// length constant so the two cannot drift.
	taskDeliveryOwedShortShaLenText = "12"

	// The `task show` labels' extra spellings (the two labels it adds beside
	// TaskShowDeliveryOwedLabel are shared with the recorded-debt render).
	TaskDeliveryOwedCommitsLabel = "delivery.ahead:"
	TaskDeliveryOwedHeadLabel    = "delivery.head:"
	TaskDeliveryOwedRescueLabel  = "delivery.rescue:"
	TaskDeliveryOwedPeerLabel    = "delivery.peer:"
)

// taskDeliveryOwedShortSha is how much of a sha a render prints. The LIST row is
// one line and its tail must not push the summary off screen; `task show` prints
// the full sha, which is what an operator pastes.
func taskDeliveryOwedShortSha(sha string) string {
	if len(sha) > taskDeliveryOwedShortShaLen {
		return sha[:taskDeliveryOwedShortShaLen]
	}
	return sha
}

// taskDeliveryOwedGoneBody is the ONE sentence both faces print for a tip whose
// object is no longer in the ground's store.
func taskDeliveryOwedGoneBody(tip, dir string) string {
	return fmt.Sprintf(taskDeliveryOwedGoneFmt, taskDeliveryOwedShortSha(tip), dir,
		taskDeliveryOwedRedispatchWord, taskDeliveryOwedVoidWord)
}

// taskDeliveryOwedTipGone answers the question the counted tail silently assumed:
// does the commit the row would push STILL EXIST in the ground's own store?
//
// `git rev-parse HEAD` is not an answer to it — it reads the REF, so it happily
// prints a sha whose object is gone (measured: the torn state this file faces).
// Only `git cat-file -e <sha>^{commit}` asks the object store itself, which is
// why this probe uses that and nothing else.
//
// Three outcomes, and the third is not "gone":
//
//	tip != "", gone=true   — the store is TORN: the ref names a commit that is not there
//	tip != "", gone=false  — the tip resolves; the counted reading can speak for it
//	tip == "", gone=false  — no HEAD to name at all (an empty or headless ground),
//	                         which is the ordinary collected shape and NOT a loss
//
// It never reports "gone" for a directory it could not read: an unreadable
// ground is the existing unreadable axis's business (Rule 15 — "I could not
// measure" and "the object is gone" are different findings with different
// repairs, and one sentence must not stand for both).
func taskDeliveryOwedTipGone(dir string) (tip string, gone bool) {
	if strings.TrimSpace(dir) == "" || liveDir(dir) == "" {
		return "", false
	}
	out, err := gitProbeOutput(dir, unpublishedRevParseVerb, unpublishedHeadArg)
	if err != nil {
		return "", false
	}
	tip = strings.TrimSpace(out)
	if tip == "" {
		return "", false
	}
	if _, err := gitProbeOutput(dir, gitCatFileVerb, archiveLandExistsFlag,
		tip+taskDeliveryOwedCommitishSuffix); err != nil {
		return tip, true
	}
	return tip, false
}

// taskDeliveryOwedProbe is THE ONE predicate behind every face in this file: how
// many commits does dir's HEAD carry that no refs/remotes/ ref reaches, and what
// is that HEAD.
//
// Three outcomes, and the third is why the signature is not a plain int:
//
//	ahead > 0, ok=true  — the delivery is outstanding, by exactly this many
//	                      commits, at this sha;
//	ahead == 0, ok=true — MEASURED and nothing is owed (a pushed run, and the
//	                      never-committed row alike — requirement ④);
//	ok == false         — the question was NOT answered, and why. Rule 15: this
//	                      is never rendered as a zero.
//
// THE EMPTY-REMOTES GUARD IS THE SUBTLE HALF. `rev-list --count HEAD --not
// --remotes` counts EVERY commit when the repository holds no refs/remotes/
// entry at all — a clone that has never fetched, or one whose remote refs were
// pruned. That reading is indistinguishable from a real debt by the number
// alone, so it is refused as unreadable rather than reported as owed: a false
// "delivery owed" on a row whose work never existed is the misjudgement the
// epic's requirement ④ forbids, and it is the same class of error as a silent
// zero — a claim the observation does not support.
func taskDeliveryOwedProbe(dir string) (ahead int, head string, ok bool, why string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return 0, "", false, "the row records no ground directory"
	}
	if liveDir(dir) == "" {
		return 0, "", false, "the recorded ground no longer exists on disk"
	}
	if !taskDeliveryOwedHasRemoteRefs(dir) {
		return 0, "", false, "this ground holds no remote-tracking ref, so \"ahead of every remote\" cannot be computed"
	}
	n, nok := unpublishedCommitProbe(dir)
	if !nok {
		return 0, "", false, "`" + unpublishedProbeCommand() + "` did not answer"
	}
	sha, err := gitProbeOutput(dir, unpublishedRevParseVerb, unpublishedHeadArg)
	if err != nil {
		return 0, "", false, "HEAD could not be resolved: " + err.Error()
	}
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return 0, "", false, "HEAD resolved to nothing"
	}
	return n, sha, true, ""
}

// taskDeliveryOwedHasRemoteRefs reports whether dir holds ANY ref under
// refs/remotes/ — the precondition that makes the ahead count mean what it says.
// An unreadable answer is `false`, which the caller renders as unreadable: the
// safe direction is "I could not measure", never "nothing is owed".
//
// IT COUNTS LINES rather than passing `--count`: `--count` takes the NEXT
// argument as its number, so `for-each-ref --count refs/remotes` is a malformed
// invocation that git refuses (measured while writing this file — the probe
// answered "no remote-tracking refs" for every ground, including a real clone
// with a live origin). One read, counted here, has no such trap.
func taskDeliveryOwedHasRemoteRefs(dir string) bool {
	out, err := gitProbeOutput(dir, taskDeliveryOwedForEachRefVerb, refsRemotesPrefix)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

// taskDeliveryOwedRescueRef returns the rescue ref that already holds tip in
// dir ("" when none does, or when the question could not be asked). The empty
// answer and the unreadable answer are the same STRING here and the caller must
// not treat them as one: taskDeliveryOwedRescueRead returns the distinction.
func taskDeliveryOwedRescueRead(dir, tip string) (ref string, ok bool) {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(tip) == "" {
		return "", false
	}
	out, err := gitProbeOutput(dir, taskDeliveryOwedForEachRefVerb, taskDeliveryOwedPointsAtFlag,
		tip, taskDeliveryOwedFormatAtom)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(out, "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		if strings.HasPrefix(name, taskDeliveryOwedRescueRefPrefix) {
			return name, true
		}
	}
	return "", true
}

// taskDeliveryOwedListSuffix renders the `task list` row tail: "" when nothing
// is owed (the common case, and the reason an ordinary listing is unchanged
// byte for byte), a counted tail when it is, and an explicit unreadable tail
// when the probe could not answer.
//
// WHICH ROWS PAY: a TERMINAL row that records a ground directory. An in-flight
// row has not delivered yet by definition, so its tail would be the noise this
// rendering must not add; a pending row has no ground. That is the same
// population taskListGroundSuffix reasons about, from the other end of the row's
// life.
//
// COST: two read-only git calls per candidate row, and none at all for the
// ordinary listing whose rows are all non-terminal or ground-less. The list
// stays a pure read (Rule 15's read-path contract): no sweep, no write, no
// notify, no fetch.
func taskDeliveryOwedListSuffix(t *Task) string {
	if t == nil || !taskTerminalStatus(t.Status) {
		return ""
	}
	dir := DeliverableWorktreePath(t.Deliverable)
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	branch := TaskDeliverableBranch(t.Deliverable)
	// THE VANISHED-TIP FACE runs BEFORE the counted probe, because the counted
	// probe's own precondition (`rev-list --count HEAD --not --remotes`) FAILS on
	// this shape and would otherwise fall through to the generic unreadable
	// sentence — which names no cause and asks for no decision. The row owes
	// work that cannot be pushed from anywhere: that is a fact about the store,
	// not a measurement this host failed to take, so it gets its own sentence.
	if tip, gone := taskDeliveryOwedTipGone(dir); gone {
		return taskDeliveryOwedOpen + TaskDeliveryOwedSuffixLabel + " " +
			taskDeliveryOwedGoneBody(tip, dir) + taskDeliveryOwedClose
	}
	ahead, head, ok, why := taskDeliveryOwedProbe(dir)
	if !ok {
		// A ground that is GONE is the ordinary state of a terminal row whose
		// work was collected — nothing can be measured there and nothing can be
		// delivered from it, so reporting it would put a line on every old row
		// in the ledger and bury the one row that matters. A ground that still
		// EXISTS and cannot be measured is the opposite: an anomaly on a row
		// whose work may be sitting there, so it is stated (Rule 15).
		if liveDir(dir) == "" {
			return ""
		}
		return taskDeliveryOwedOpen + TaskDeliveryOwedSuffixLabel + " " +
			fmt.Sprintf(taskDeliveryOwedUnreadableFmt, dir, why) + taskDeliveryOwedClose
	}
	if ahead <= 0 {
		return ""
	}
	short := taskDeliveryOwedShortSha(head)
	body := fmt.Sprintf(taskDeliveryOwedAheadBodyFmt, ahead, short)
	if branch != "" {
		body += fmt.Sprintf(taskDeliveryOwedBranchFmt, branch)
	}
	return taskDeliveryOwedOpen + TaskDeliveryOwedSuffixLabel + " " + body + taskDeliveryOwedClose
}

// taskShowDeliveryOwedReading renders the full four-fact block `task show`
// prints for a row whose delivery is outstanding: commits ahead, the exact
// commit, the rescue ref if one holds it, and the peer's last answer.
//
// IT RENDERS NOTHING FOR A ROW THAT OWES NOTHING — the recorded-debt line
// (TaskShowDeliveryOwedLabel, driven by the event trail) already covers the
// historical case, and a second line saying "nothing owed" on every row would
// bury the one row that matters. The two faces are complementary and neither
// displaces the other: the trail says what WAS recorded, this says what is
// MEASURED now.
func taskShowDeliveryOwedReading(t *Task) string {
	if t == nil || !taskTerminalStatus(t.Status) {
		return ""
	}
	dir := DeliverableWorktreePath(t.Deliverable)
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	// THE SAME VANISHED-TIP FACE as the list marker, on the same label: a tip
	// whose object is gone is stated with its cause and the decision the row now
	// needs, never as a count a reader would try to push. The two faces share ONE
	// sentence (taskDeliveryOwedGoneBody), so they cannot disagree.
	if tip, gone := taskDeliveryOwedTipGone(dir); gone {
		return padTaskShowLabel(TaskShowDeliveryOwedLabel) + taskDeliveryOwedGoneBody(tip, dir) + "\n"
	}
	ahead, head, ok, why := taskDeliveryOwedProbe(dir)
	if ok && ahead <= 0 {
		return ""
	}
	var b strings.Builder
	if !ok {
		// UNREADABLE is stated as such, on the same label, with its reason.
		b.WriteString(padTaskShowLabel(TaskShowDeliveryOwedLabel) +
			"UNCHECKED — " + why + " (ground " + dir + ")\n")
		return b.String()
	}
	branch := TaskDeliverableBranch(t.Deliverable)
	b.WriteString(padTaskShowLabel(TaskShowDeliveryOwedLabel) +
		strconv.Itoa(ahead) + " commit(s) exist in this row's ground that NO remote ref reaches\n")
	b.WriteString(padTaskShowLabel(TaskDeliveryOwedCommitsLabel) +
		strconv.Itoa(ahead) + "\n")
	b.WriteString(padTaskShowLabel(TaskDeliveryOwedHeadLabel) + head + " (ground " + dir + ")\n")
	// ③ THE RESCUE AXIS: a tip already held by an archive/rescued- tag survives
	// a sweep, so it is the difference between "recoverable" and "lost". The
	// reader reports it; no arm here moves or deletes such a ref (epic
	// constraint).
	if ref, rok := taskDeliveryOwedRescueRead(dir, head); rok && ref != "" {
		b.WriteString(padTaskShowLabel(TaskDeliveryOwedRescueLabel) + ref + " already holds this tip — the commits survive a sweep\n")
	} else if rok {
		b.WriteString(padTaskShowLabel(TaskDeliveryOwedRescueLabel) +
			"none — no " + taskDeliveryOwedRescueRefPrefix + "* ref holds this tip, so the commits live ONLY in that ground\n")
	} else {
		b.WriteString(padTaskShowLabel(TaskDeliveryOwedRescueLabel) + "UNCHECKED — the ref list could not be read\n")
	}
	// ④ THE PEER'S LAST ANSWER, through the delivery's own read-only channel.
	// branch=="" means the row records no branch, which is itself the answer
	// (there is no name to ask the peer about).
	if branch == "" {
		b.WriteString(padTaskShowLabel(TaskDeliveryOwedPeerLabel) +
			"not asked — the row records no branch\n")
		return b.String()
	}
	ctx, cancel := context.WithTimeout(context.Background(), branchDeleteGitTimeout)
	defer cancel()
	mainDir := finishCleanupMainRepoDir(finishCleanupParseDeliverable(t.Deliverable).Repo)
	target := finishCleanupRowTargetResolution(mainDir).SSHTarget
	if target == "" {
		b.WriteString(padTaskShowLabel(TaskDeliveryOwedPeerLabel) +
			"no peer resolves on this host, so there is no channel to re-drive through\n")
		return b.String()
	}
	peerTip := deliveryRetryPeerTip(ctx, mainDir, target, branch)
	if peerTip == "" {
		b.WriteString(padTaskShowLabel(TaskDeliveryOwedPeerLabel) +
			target + " advertises no ref for " + branch + "\n")
		return b.String()
	}
	b.WriteString(padTaskShowLabel(TaskDeliveryOwedPeerLabel) +
		target + " advertises " + branch + " at " + peerTip +
		" (this row's tip is " + head + ")\n")
	return b.String()
}

// padTaskShowLabel renders one `task show` label padded to the renderer's width,
// built from the constant the renderer itself uses (Rule 1b: the width is
// declared once, in tasks.go).
func padTaskShowLabel(label string) string {
	if len(label) >= taskShowLabelWidth {
		return label + " "
	}
	return label + strings.Repeat(" ", taskShowLabelWidth-len(label))
}

// TaskDeliveryOwedRowIDs is the LIVE inventory the re-drive leg consumes: the
// terminal rows whose recorded ground exists and holds commits no remote ref
// reaches.
//
// WHY IT PROBES INSTEAD OF READING A TRAIL EVENT (Rule 41a — read the thing
// that answers the question). The existing inventory face,
// deliveryRetryOwedRowIDs, selects rows whose NEWEST trail event is
// `delivery-blocked`; that event is written only by an attempt that RAN and was
// refused, and nothing has run since the retry verb was retired — so the trail
// is empty exactly when the defect is most likely. The LIVE fact (a ground with
// unreachable commits) is what the repair must be keyed on, and it is the same
// probe the two render faces use, so the read and the repair cannot disagree
// about which rows owe.
//
// An unreadable row is SKIPPED and reported in the second return, never silently
// dropped (Rule 15): the caller prints the reason.
func TaskDeliveryOwedRowIDs() (owed []string, unreadable []string, err error) {
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT task_id, COALESCE(status,''), COALESCE(deliverable,'') FROM tasks`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, status, deliv string
		if serr := rows.Scan(&id, &status, &deliv); serr != nil {
			return nil, nil, serr
		}
		if !taskTerminalStatus(status) {
			continue
		}
		dir := DeliverableWorktreePath(deliv)
		if strings.TrimSpace(dir) == "" {
			continue
		}
		ahead, _, ok, why := taskDeliveryOwedProbe(dir)
		// THE VANISHED-TIP ROW IS NOT A RE-DRIVE CANDIDATE, and it must not be
		// reported as one: the leg's whole action is "push this ground's
		// commits to the peer", which needs the commit to exist. A row whose tip
		// object is gone is therefore skipped BY CAUSE — the same sentence the
		// two render faces print — instead of falling through to the counted
		// probe's generic "did not answer", which would hide a loss behind a
		// measurement failure (Rule 15).
		tip, gone := taskDeliveryOwedTipGone(dir)
		switch {
		case gone:
			unreadable = append(unreadable, id+" ("+taskDeliveryOwedGoneBody(tip, dir)+")")
		case !ok:
			if liveDir(dir) != "" {
				unreadable = append(unreadable, id+" ("+why+")")
			}
		case ahead > 0:
			owed = append(owed, id)
		}
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, nil, rerr
	}
	return owed, unreadable, nil
}
