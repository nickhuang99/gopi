package tools

// auditTotalityAllow is the CLASSIFICATION half of the assertion-totality
// audit gate (assertion_totality_audit_test.go). Every negative-totality
// assertion the predicate finds in the test trees must appear here with a
// reason; an absent key fails the build, and a reason shorter than
// auditAllowMinReasonLen fails too. The point is not the map — it is that a
// future agent writing "no step may run" meets the question "is this bundled
// claim right?" at the moment of writing it.
//
// Keys are "path:TestFunc" — deliberately NOT line numbers, so the audit
// survives line shifts and sibling edits. The full table with the incident
// reference and the per-site evidence is
// devdoc/assert-the-bug-audit-20260917.md §2.
//
// FORMAT: "<correct | over-broad | UNCERTAIN> — <the claim, the set, and the
// evidence that makes the verdict right>".
// ── rows from the manual walk that are NOT in the gate's hit list ───────────
//
// The predicate sees one shape (a negation over a collection whose message
// carries an action verb). Two rows of the audit table carry the SAME bundle
// while escaping that vocabulary, so — by design — they are documented here
// instead of keyed, and the stale-key half of the gate cannot silently forget
// them. Full verdicts, evidence and line anchors: devdoc
// devdoc/assert-the-bug-audit-20260917.md §2a row 3 and §2c row 1.
//
//   task_finish_cleanup_test.go:TestFinishCleanupPlanKeepsEveryGuard (:1758)
//     FIXED (2026-09-19, the client-push-rebase epic) — the dry-run claim was
//     split: the refusal plan must predict NO removal, and (after the fix) it
//     must STILL predict the push. Its message carries no action verb, so the
//     predicate does not fire and no key is owed; the note stays so a reader
//     knows the site was reviewed rather than overlooked.
//
//   task_finish_cleanup_test.go:TestServerRecycleDirtyWorktreeSkipped (:958)
//     FIXED (6b1d753c "feat(align): make integration-branch alignment a
//     precondition of delivery") — the key that documented this site is
//     DELETED from the map, and the site is recorded here because the same
//     function is also the live witness of
//     TestAssertionTotalityAuditGateClassifiesTheLiveTree (see the audit file).
//     The NEGATION ITSELF WAS REWRITTEN, not removed: 95650429 bundled it as
//     `len(rep.Steps) != 0 && !onlyUntrackedResidueNotes(rep.Steps)` with the
//     message "the server recycle has no other act than the removal, so none
//     may run on a dirty worktree" — a genuine negative totality, correctly
//     keyed. 6b1d753c hoisted the ALIGNMENT step AHEAD of the dirty gate
//     (task_finish_cleanup.go:1343 — "ALIGNMENT IS A PRECONDITION OF THE
//     DELIVERY, ASKED HERE — BEFORE THE DIRTY GATE BELOW"), so a dirty
//     worktree now legitimately records an align step, the old condition fired
//     on a CORRECT tree, and the assertion became `len(rep.Steps) == 0 ||
//     !strings.Contains(rep.Steps[len(rep.Steps)-1].Detail,
//     finishCleanupUntrackedResidueNote)` — a POSITIVE claim that the LAST
//     recorded step is the removal's refusal note. It is no longer a negation
//     of a count, so the predicate stops firing and no key is owed (the
//     predicate cannot be widened to reach it, and must not be: rewriting it
//     to `len(x) == 0 || …` would re-hunt every positive-emptiness assertion
//     in the tree). The permit is kept OUT of the map on purpose — a stale
//     key "silently re-arms the next agent" (the gate's own words), and this
//     file's precedent is that a closed key is documented, not parked.
//
//   task_merged_cleanup_test.go:TestMergedCleanupDirtyWorktreeSkipped (:193)
//     correct — the CONTROL row: the identical dirty guard with the opposite
//     verdict, because that sweep's only act is `worktree remove`
//     (task_merged_cleanup.go:431) and it has no push step at all. A future
//     agent meeting "dirty ⇒ skipped" in two places must be able to see why one
//     is right and the other is not; this note is that pointer.

var auditTotalityAllow = map[string]string{
	// ── the incident family: the same bundled claim 01a0aab8 owns ───────────
	// These keys are the AUDIT's permits, and the client-push-rebase epic
	// (2026-09-19) CLOSED the condition each one documented:
	//
	//   • TestFinishCleanupChainDirtyWorktree — the key is DELETED: the test no
	//     longer makes the over-broad claim. It asserts the split (the push ran
	//     AND the peer holds the ref; the removal did not run and says why), so
	//     the gate's predicate finds nothing at that function. A stale permit
	//     "silently re-arms the next agent" (the gate's own words), which is
	//     why the entry went with the fix rather than staying as history.
	//   • TestServerRecycleDirtyWorktreeSkipped — the key is DELETED TOO, and
	//     the reason is the same failure mode one step later: the assertion was
	//     REWRITTEN (not removed) by 6b1d753c, which hoisted the alignment step
	//     ahead of the dirty gate, so the negation became a positive claim that
	//     the LAST step is the removal's refusal note. No key is owed. The full
	//     history lives in this file's "rows NOT in the gate's hit list"
	//     section, where the same function is also the test witness of
	//     TestAssertionTotalityAuditGateClassifiesTheLiveTree — so the site is
	//     documented twice on purpose: once for what it no longer claims, once
	//     for what it now proves.
	// ── (a liveness/ownership permit used to live here) ───────────────────
	// The permit for
	// `internal/tools/task_finish_cleanup_test.go:TestFinishCleanupServerRecycleSkipsWorktreeInUse`
	// is DELETED, not moved: 58b5618 retired the finish-cleanup mechanism and
	// deleted that test file, so the assertion it excused no longer exists and
	// the key was reported STALE (an excused site that is gone re-arms the next
	// agent with a reason about nothing). The live counterpart of that claim is
	// the ground-collect arm — see run_ground_collect_clone_test.go — which
	// omits a ground from the collectable set on a live holder and asserts the
	// ground survives.

	// ── correct-by-construction: the set is EVIDENCE OF ABSENCE ────────────
	"internal/tools/wip_worktree_repro_test.go:TestMergedCleanupWIPWithCommitIsProtected":                            "correct — 'a worktree with its own commit must never be reaped' guards a DATA-LOSS path: the branch's commit is not in master, so removal destroys work. There is no idempotent step in the reaping sweep's set, and the guard is the sweep's whole safety property. Evidence: the sibling test TestMergedCleanupOrphanWorktreeStillReported (task_merged_cleanup_test.go:649 — RETIRED its mutating half for report-only, epic sweep-report-not-act 2026-09-19, so the '…StillRemoved' name cited here is dead) pins the other direction on a live fixture: the pair is NAMED and the removal callback is armed to fail if it ever fires.",
	"internal/tools/task_merged_cleanup_test.go:TestMergedCleanupHandsRowOwnedWorktreeToItsOwner":                    "correct — the claim is 'the sweep itself must not remove a row-owned worktree'; the sweep hands it to the owner chain instead. Ownership is not dirtiness: a foreign-owned directory must not be touched by THIS actor at all, so no step of this sweep's set is legitimately excludable. Evidence: the same test asserts asked[0]==f.wtDir and rep.Notes carries finishCleanupSweepNotPreferred.",
	"internal/tools/task_merged_cleanup_test.go:TestMergedCleanupIdempotentSecondRun":                                "correct — the assertion is about the SECOND run over an already-clean fixture ('a clean no-op'), i.e. the set is empty because the work is done, not because a guard fired. The first run in the same test asserts Removed==1, so the sweep is proven able to act. Evidence: rep1.Removed must be 1 three lines above.",
	"internal/tools/task_sync_git_pushback_test.go:TestPushbackNoBranchPushesNothing":                                "correct — the fixture has NO non-master local branch (newPBWorld), so there is nothing to push; the negation is a statement about the INPUT set, not about a guard blocking an action. Evidence: 'no-branch client' in the test's own name and the fixture's master-only setup.",
	"internal/tools/memory_sync_task_gitpushback_cov_test.go:TestDefaultGitPushbackSkipsIntegratedPushesUnmerged":    "correct — 'no push should fail in this topology' is an ERROR-FREE claim (PushFailed is a failure list, not a step list); a push that succeeds and a push that never runs both satisfy it. The sibling assertion in the same test pins the positive half (the unmerged deliverable IS in BranchesPushed).",
	"internal/tools/memory_sync_task_gitpushback_cov_test.go:TestDefaultGitPushbackUnreachablePeerFailsSoft":         "correct — 'an unreachable peer must push nothing (blind push resurrects refs)' guards a DATA-LOSS path: pushing to an unreachable peer cannot be verified, and a blind push can resurrect a ref the server's adoption chain deliberately deleted. The claim is about a peer that cannot be reached, not about a guard reaching too far. Evidence: the test asserts the fetch failure IS reported (PushFailed/Skipped non-empty).",
	"internal/tools/tasknotify_test.go:TestTaskNotifyEnvWinsOverSettings":                                            "correct — 'settings cmd must NOT fire when env overrides' pins PRECEDENCE between two logger sinks (Rule 2's one-reader rule), and the same test asserts the env sink received all 3 events. The negation is the whole point of the precedence contract; there is no idempotent step in the set.",
	"internal/testutil/worker_ownership_rounds_test.go:TestWorkerOwnershipGateStaysGreenAcrossSuccessiveMergeRounds": "correct — 'the gate is RED on the tip' refers to a DETECTION outcome (uncovered pointers in a document), not to a step that ran. A gate that detects nothing and a gate that is wrong both matter, and this assertion pins the first.",
	"internal/signalrec/signalrec_test.go:TestSignalRecAgentChildMarkerWithAMismatchedRowIsRefused":                  "correct — 'a refusal must write nothing' is the DATA-LOSS property of a refusal: the record had no valid home, so writing it anywhere would file a record under the wrong session. Evidence: the same test asserts the returned error names the handed dir (the operator can see which pair disagreed).",
	"internal/scratch/gc_queue_subtree_test.go:TestSweepHomeKeepsUnmarkedQueueArtifacts":                             "correct — 'a flat artifact is not a run' guards a DATA-DELETION path in the scratch GC: an in-flight artifact must survive the sweep. The set is a swap plan, and the safety property is precisely that nothing is planned for it. Evidence: the same test then calls SweepHome and asserts 0 swept.",
	"cmd/agent/cli_voice_review_test.go:TestVoiceTranscribeRejectsOverLimitUpload":                                   "correct — 'the rejected upload reached the engine (spool entries)' is an EVIDENCE-OF-ABSENCE probe on a directory, paired with overBody.n read == 0. The set is a spool directory, not a step list; the guard (decide from the declared Content-Length) is the subject, and NO push-like idempotent member exists in it. Corroboration on the text: the guard answers 413 BEFORE the spool directory is opened, so had it not fired this upload's bytes would have been fully read (overBody.n > 0) and could have reached the engine.",
	// THE KEY WAS RE-POINTED, NOT DROPPED (epic merger-delete-dead-impl,
	// 2026-09-19). Its old subject, TestKilledWorkerDaemon_MergerTickRecovers
	// TheStalledRound, drove a BARE `gopi merger` tick — a verb that is now
	// retired, so the test went with it. The SAME assertion (a `--once` round
	// must leave ZERO gopi processes alive) is driven by the surviving
	// worker-daemon case, whose step set is identical and whose claim is the
	// stronger one: not merely "no worker daemon survived" but "the process that
	// did the recovery is not resident either". The classification is unchanged
	// in substance — process-list evidence of absence, no step set involved —
	// and the key follows the assertion that is actually in the tree, which is
	// what the gate's stale-key check requires.

	// ── epic sweep-report-not-act (2026-09-19): the reaper became a REPORTER ─
	// The banner is PERMANENT: these six sites are the conversion's own residue.
	// Every fixture calls mergedCleanupRun (task_merged_cleanup_test.go:142),
	// which passes ReportOnly: true — and ReportOnly IS production's only
	// setting (cmd/agent/cli_tasks.go:3058 in serveMergedCleanupSweep and
	// :3434 in the sweep report), so these tests observe exactly what the
	// production sweep does. report-only is the epic's permanent design, not a
	// migration state.
	//
	// THE UNIFORM VERDICT: correct — half (b) of the totality question. The
	// retired claim "the reaper mutates nothing" is TRUE, and it is the
	// conversion's SUBJECT: the assertion that used to pin the disposal is now
	// the proof it is gone. What is FALSE is the OTHER half — the one this
	// suite no longer carries: "a batch whose whole ladder cleared is
	// announced". Reporting is the retired mutation's replacement output, and
	// those sites prove the ladder CLEARED while this proof only says how LOUD
	// the pass was. Each proof below names its own carrier of the positive
	// half and what that carrier can still not separate.
	//
	// (SweepReported is the batch-wide count appended by report-only at
	// task_merged_cleanup.go:586 for the pair lane and :682 for the
	// orphan-ref lane — the ONE spelling of "this pass named a ref instead of
	// taking it", and fields are test-only, so a carrier can use it directly.)
	"internal/tools/task_merged_cleanup_orphan_test.go:TestMergedCleanupReportsOrphanMergedBranchWithNoWorktree": "correct — the reaper mutates nothing: the orphan-ref lane is REF-ONLY by construction (task_merged_cleanup.go:682 appends Removed only under !opts.ReportOnly, and :562/:669 return before it), ReportOnly is production's only setting (cmd/agent/cli_tasks.go:3058/:3434), and the test's OWN next assertion pins the carrier of the positive half: len(rep.Reported)==1 with Reported[0].Worktree==mergedCleanupOrphanWorktreeLabel — the ref-only label, i.e. the orphan lane's own report step. Residue of epic sweep-report-not-act: the test message still reads 'mutates', but the claim is true and it is what the conversion exists to pin. What the proof can still not separate: the positive half it does NOT carry is 'a batch whose whole ladder cleared is announced EVERYWHERE' — a pass that reported this ref and silently dropped a SECOND lane's ref would satisfy both assertions.",

	"internal/tools/task_merged_cleanup_test.go:TestMergedCleanupNoTaskRowStillReported": "correct — the reaper mutates nothing: the pair lane reaches Removed only past the !opts.ReportOnly return (task_merged_cleanup.go:562), ReportOnly is production's only setting (cmd/agent/cli_tasks.go:3058/:3434), and this is the serve-merge shape — a branch merged with NO ledger row, so no owner chain exists to be the reporter instead. The positive half IS carried here, by the test's own next assertion: len(rep.Reported)==1 plus rep.Reported[0].Detail containing repoSweepOwnerNoneText — the pass NAMED the pair and said on the record that no owner row exists. What the proof can still not separate: the sweep's TWO production faces (serveMergedCleanupSweep, the sweep report) both go through this one function, so an assertion here cannot tell whether both faces would render the entry.",

	"internal/tools/task_merged_cleanup_test.go:TestMergedCleanupOrphanWorktreeStillReported": "correct — the reaper mutates nothing: the pair lane has no removal path at all under ReportOnly (task_merged_cleanup.go:562 appends Reported; :586 appends Removed only in the mutating arm, which no production caller reaches). The companion assertion is on the OTHER half of the same conversion — len(rep.Reported)==1, pinned BEFORE the Removed check — and the test also arms SetFinishCleanupSweepOwnerRemover with a callback that fails the test if it is ever invoked, so the assertion additionally proves no handover happened. Residue of epic sweep-report-not-act (message wording only; the verdict is correct). What the proof can still not separate: nothing here can distinguish this sweep's single orphan-worktree shape from the multi-pair shape — an orphan WORKTREE (directory present, branch merged) and a merged-only ref both land in Reported.",

	"internal/tools/task_merged_cleanup_test.go:TestMergedCleanupReportsFrozenPair": "correct — the reaper mutates nothing: this is epic case (a) converted, merged branch + clean worktree + terminal row, and the proof is PHYSICAL as well as structural — the test asserts Reported[0].Branch/Worktree carry the pair with repoSweepOwnerEvidenceLabel in Detail, then os.Stat(f.wtDir) succeeds (the directory SURVIVES) and assertBranchPresent pins the ref at its own tip. That is the conversion's own subject: the disposal is retired, so its absence IS the specification, and ReportOnly is production's only setting (cmd/agent/cli_tasks.go:3058/:3434). What the proof can still not separate: the surviving-directory half is asserted for THIS pair only; a second pair in the same fixture left un-reported (or a directory removed by something outside this pass) would not be caught here.",

	"internal/tools/wip_worktree_repro_test.go:TestMergedCleanupProgressedWorktreeStillReported": "correct — the reaper mutates nothing: the same ReportOnly arm as every caller (task_merged_cleanup.go:562, production cmd/agent/cli_tasks.go:3058/:3434), and the test's own next assertion carries the positive half — len(rep.Reported)==1 with the branch that passed every guard, which is the point of the case: a legitimate recycle is NOT broken, it is NAMED. The test's own comment states the conversion ('it is now REPORTED, and nothing is taken away'), so the negation is a true claim, not a stale permit. What the proof can still not separate: the assertion does not check the Reported entry's Detail/lane prefix, so which lane reported the pair (pair vs orphan) is not pinned here.",

	// ── the close-out mint's witness is GONE, and the key went with it ─────
	// cmd/agent/merge_run_test.go:TestMergeCloseOutNamesNothingWhenThePositionDidNotMove
	// was allowed here. The epic version-identity (2026-09-24) DELETED the
	// close-out mint (mintAdvancedPositionAtRunClose) with the per-landing
	// mint it complemented, and the test asserting the mint's refusal went
	// with the code it asserted about. A permit whose assertion is no longer
	// in the tree is a STALE key — the very failure this gate reports — so it
	// is deleted rather than kept 'just in case': nothing about a merge path
	// mints a version name any more (see devdoc/version-identity.md).

	// ── the peer-ref NAME COLLISION lane: a refusal that may not push ───────
	// (rework of task/01a0c53c — that branch was archived for this one
	// unclassified site, so the classification is made on the restored
	// content rather than by weakening the gate.)
	"internal/tools/task_sync_git_pushback_peeref_test.go:TestPushbackPeerRefCollisionRefusedByName": "correct — half (b): the set legitimately HAS no idempotent member for a colliding name, because the peer-ref gate's collision arm IS the exclusion the claim names, and it is the LAST decision before the push. gitPushbackPlan appends the refusal and `continue`s (internal/tools/task_sync_git_pushback.go:634-636), while the loop's only act past that point is gitPushbackPushOne plus the flush.Pushed append (:649-652) — so a colliding name can reach Pushed only if the collision arm is removed, which is exactly the defect. (The 2026-09-16 push/dirty shape would be an idempotent step surviving the guard; here there is no such step — only the push the refusal exists to prevent, and it is never reached for this name.) The claim is NOT 'this lane never pushes': the opposite direction is pinned on live fixtures in the SAME file by TestPushbackPeerRefAncestorFastForwardedNotCollision, which fetches the peer's CONTAINING tip and then asserts len(flush.Pushed)==1 with Pushed[0] prefixed 'feat/ff' (task_sync_git_pushback_peeref_test.go:220-223), and by TestPushbackPeerRefAbsentPushesPlainName, which asserts Pushed[0]==\"feat/new\" (:251-254) — so an empty Pushed in the colliding case is the collision arm's own work, not a Pushed list that cannot be reached. The refusal is measured on the peer's OWN refs, not on the report: pbForEachRef(w) before/after must be byte-identical and pbRefSha(w,\"feat/dup\")==peerTip (:177-181), plus our own tip left unmoved (:184-187). Same-function positive carriers: len(flush.Failed)==1 (:141) and the report line naming BOTH tips and the remedy instead of the anonymous push failure (:150-170). What the proof can still not separate: the fixture gives the client exactly ONE branch of this name and no other pushable local branch, so within this test 'nothing was pushed' is not separated from 'nothing was selected to push' — that reachability half is carried by the two sibling tests above, on their own fixtures, not here.",

	// ── the memory-sync tick's no-peer arm: a statement about the INPUT ─────
	// (added 2026-09-28 — the site went UNCLASSIFIED when the no-declared-peer
	// arm landed; classified here, and the predicate was NOT relaxed.)

	// ── chat reporting: EVIDENCE OF ABSENCE over a directory ───────────────
	// (Re-keyed 2026-09-29 with the epic chat-session-ground-retired-when-not-a-row:
	// the same claim survives, the test that carries it was re-pointed from
	// TestChatGroundDegradesInANonRepoDirectory to
	// TestChatGroundReportsANonRepoDirectoryByAnswer when the creation path was
	// deleted — the non-repo arm stopped being a DEGRADATION and became a
	// reported answer.)
	"internal/tools/chat_ground_test.go:TestChatGroundReportsANonRepoDirectoryByAnswer": "correct — 'the non-repo directory gained %d entries — reporting must write nothing' is evidence of ABSENCE over a directory listing, not a step set: the count is of ENTRIES IN A TEMP DIRECTORY that was empty before the call, and no idempotent member can exist in the checked set because the creation path is DELETED. The evidence is PRODUCTION CONTROL FLOW: ChatGroundFor's not-inside-a-work-tree arm returns ChatGroundResolution{Dir: base, Detail: fmt.Sprintf(chatGroundNotRepoDetailFmt, base)} (internal/tools/chat_ground.go) — it calls no producer at all, and there is no producer left to call: the `git worktree add` path (taskGroundCreate), the branch namespace it wrote and the chdir that entered it were removed with the epic (devdoc/chat-session-ground-lifecycle-diagnosis.md records why: a chat is not a ledger row, so its ground had no collector on this machine). The positive halves are asserted in the same test: got.Degraded is false (an absent checkout is an ANSWER, not a failure), got.Dir names the directory, and got.Detail carries the reason 'not inside a git work tree'. The opposite polarity is pinned on live fixtures by TestChatGroundCreatesNothingAndReportsTheMainCheckout and TestChatGroundOutsideAMainCheckoutIsReportedNotMoved, which assert a real repo path IS reported as a main checkout and that neither a branch nor a worktree appears — so an untouched directory here is the report working, not a path that cannot fire. What the proof can still not separate: the count is non-recursive (os.ReadDir, one level), so a write into an already-existing subdirectory of that path would not be seen; the directory is freshly created per test, which is what makes the one-level count sufficient here.",

	// ── the session-stamp repair door's exclusion of a submitter value ──────
	// (added 2026-10-29 — the site went UNCLASSIFIED when d4b3721 added the
	// repair arm and its test in one commit; classified here, the predicate was
	// NOT relaxed, and no assertion was edited.)
	//
	// gofmt keeps this row long on purpose: the reason IS the deliverable (the gate
	// asks for a decision, not a token).
	"internal/tools/task_session_stamp_test.go:TestTaskRepairSessionIDsRepairsOnlyAnExecutorStampFromAnotherRow": "correct — both halves. The claim is \"this submitter-shaped row produced NO repair candidate\", and the negated set is the pass's answer list rep.Candidates, which is empty because taskSessionRepairValue answered \"\" for this row and the loop therefore `continue`d into the REPORT arm (task_session_stamp.go:376-383) instead of reaching EITHER append (:396, :414). Half (a) the claim is right: the loop's single write is the CAS `UPDATE tasks SET session_id = ? … WHERE task_id = ? AND session_id = ?` at :401, whose RowsAffected==1 arm (:404) is the ONLY producer of a candidate whose Applied is true, and it is reachable only through the newSID != \"\" branch — so an empty Candidates means no rewrite was built for this row, and on the apply=false iteration it also means no write can have been attempted. Had the guard not fired, the submitter row would have entered Candidates and, on the apply=true pass, that CAS would have replaced session_id with the row's own run record — and that column IS the anti-self-reference fence's input (tasks.go:7869, `session_id != ?` inside the pending-claim query), so every agent-filed row of that chat session would have become claimable by the very session that filed it, i.e. the guard's failure would hand work back to its own submitter. Half (b) no idempotent member is excluded: Candidates is the pass's whole answer and each row is visited exactly once per iteration (:374), so \"no candidate for this row\" cannot suppress a step some later stage still owed — the alternative outcome for this row is the Skipped line, not a deferred repair. The exclusion itself is asserted POSITIVELY in the same test: the submitter row must land in rep.Skipped with len==1 (:212) and its column must read back unchanged (:215), while the polluted row in the SAME pass yields candidates==1 with Applied=true (:193) and a rewritten column (:200) — so the emptiness is this row's exclusion, not a Candidates list that cannot be reached. What the proof can still not separate: the pass is narrowed to ONE row here, so a defect that dropped OTHER rows' candidates would not be seen (the sibling TestTaskRepairSessionIDsSweepsEveryRowWhenNoIDIsGiven carries that half), and it reads one pass over the ledger rather than every door that can write the column.",

	// ── the run-ground collect legs' PRE-ACT refusals (added 2026-10-26) ────
	// Five sites went UNCLASSIFIED when the clone leg landed (6c394e5 and the
	// gone rung beside it); they are the SAME disposition, so they share one
	// reasoning: each asserts that a record is ABSENT because the act never ran.
	// The predicate was NOT relaxed, no assertion was edited, and the gate file
	// is untouched (Rule 41: the red was filed as its own row before this fix).
	//
	// gofmt keeps this row long on purpose: the reason IS the deliverable (the gate
	// asks for a decision, not a token).
	"internal/tools/run_ground_collect_clone_test.go:TestRunGroundCollectRefusesACloneGroundCarryingUndeliveredWork": "correct — both halves. The claim is \"this ground was NOT removed, so the removal audit carries no record for it\", and the negated set is the audit sink filtered to removalaudit.KindWorktreeRemoval entries whose target is THIS fixture's directory (run_ground_collect_clone_test.go:133 removalRecords). Half (a) correct: the claim is EVIDENCE OF ABSENCE about an act, and the act is not performed — RunGroundCollect writes the record IMMEDIATELY BEFORE the removal on each leg (internal/tools/run_ground_anchor.go:706 for the clone leg, :727 for the linked leg) and every refusal above it is a `skipped` line plus a `continue`, so a refused ground reaches no writer at all. Half (b) no idempotent member is excluded: the checked set is a RECORD SET, not a step sequence — there is no member a resolver could still owe, and the one step that must never run (the removal) is exactly what the record would announce, so an empty set is the removal not having happened. The specific refusal is asked at the PLAN step: runGroundAnchorLiveness applies worktreeSweepUndeliveredRefusal before a ground can be called collectable, which the test asserts POSITIVELY two lines above (the ground is in `kept` with the rung's own sentence and NOT in `collectable`), and the apply is then run over that already-empty collectable selection. What the proof can still not separate: the filter is (kind, target-path), so a removal record written for a DIFFERENT path in the same sink would not be seen here; and it reads one sink, not every sink a second writer could choose.",
	// gofmt keeps this row long on purpose: the reason IS the deliverable (the gate
	// asks for a decision, not a token).
	"internal/tools/run_ground_collect_clone_test.go:TestRunGroundCollectRefusesADirtyCloneGround": "correct — both halves. The claim is \"this ground was NOT removed, so the removal audit carries no record for it\", and the negated set is the audit sink filtered to removalaudit.KindWorktreeRemoval entries whose target is THIS fixture's directory (run_ground_collect_clone_test.go:133 removalRecords). Half (a) correct: the claim is EVIDENCE OF ABSENCE about an act, and the act is not performed — RunGroundCollect writes the record IMMEDIATELY BEFORE the removal on each leg (internal/tools/run_ground_anchor.go:706 for the clone leg, :727 for the linked leg) and every refusal above it is a `skipped` line plus a `continue`, so a refused ground reaches no writer at all. Half (b) no idempotent member is excluded: the checked set is a RECORD SET, not a step sequence — there is no member a resolver could still owe, and the one step that must never run (the removal) is exactly what the record would announce, so an empty set is the removal not having happened. The refusal is runGroundCollectCloneVerdict's ② CLEAN rung in its TRACKED-change arm, asserted POSITIVELY in the same test: the skip line must name reclaimSkipDirtyReason AND the offending file (seed.txt), and os.Stat proves the tree survived. What the proof can still not separate: the filter is (kind, target-path), so a removal record written for a DIFFERENT path in the same sink would not be seen here; and it reads one sink, not every sink a second writer could choose.",
	// gofmt keeps this row long on purpose: the reason IS the deliverable (the gate
	// asks for a decision, not a token).
	"internal/tools/run_ground_collect_clone_test.go:TestRunGroundCollectRefusesACloneGroundWithScratchInIt": "correct — both halves. The claim is \"this ground was NOT removed, so the removal audit carries no record for it\", and the negated set is the audit sink filtered to removalaudit.KindWorktreeRemoval entries whose target is THIS fixture's directory (run_ground_collect_clone_test.go:133 removalRecords). Half (a) correct: the claim is EVIDENCE OF ABSENCE about an act, and the act is not performed — RunGroundCollect writes the record IMMEDIATELY BEFORE the removal on each leg (internal/tools/run_ground_anchor.go:706 for the clone leg, :727 for the linked leg) and every refusal above it is a `skipped` line plus a `continue`, so a refused ground reaches no writer at all. Half (b) no idempotent member is excluded: the checked set is a RECORD SET, not a step sequence — there is no member a resolver could still owe, and the one step that must never run (the removal) is exactly what the record would announce, so an empty set is the removal not having happened. The refusal is the SAME rung's UNTRACKED-residue arm — the parity half 6c394e5 added, because the linked leg's `git worktree remove` refuses that tree too while `os.RemoveAll` would not: the test asserts POSITIVELY that the skip names reclaimUntrackedResidueReason and the entry (scratch.log), and that the tree survived. What the proof can still not separate: the filter is (kind, target-path), so a removal record written for a DIFFERENT path in the same sink would not be seen here; and it reads one sink, not every sink a second writer could choose.",
	// gofmt keeps this row long on purpose: the reason IS the deliverable (the gate
	// asks for a decision, not a token).
	"internal/tools/run_ground_collect_clone_test.go:TestRunGroundCollectRefusesALiveCloneGround": "correct — both halves. The claim is \"this ground was NOT removed, so the removal audit carries no record for it\", and the negated set is the audit sink filtered to removalaudit.KindWorktreeRemoval entries whose target is THIS fixture's directory (run_ground_collect_clone_test.go:133 removalRecords). Half (a) correct: the claim is EVIDENCE OF ABSENCE about an act, and the act is not performed — RunGroundCollect writes the record IMMEDIATELY BEFORE the removal on each leg (internal/tools/run_ground_anchor.go:706 for the clone leg, :727 for the linked leg) and every refusal above it is a `skipped` line plus a `continue`, so a refused ground reaches no writer at all. Half (b) no idempotent member is excluded: the checked set is a RECORD SET, not a step sequence — there is no member a resolver could still owe, and the one step that must never run (the removal) is exactly what the record would announce, so an empty set is the removal not having happened. The refusal is the LIVENESS rung: the fixture stamps this test's own pid as the taker, so the ground cannot even be called collectable — asserted POSITIVELY above (not in `collectable`, present in `kept` with its reason, directory survives via os.Stat). What the proof can still not separate: the filter is (kind, target-path), so a removal record written for a DIFFERENT path in the same sink would not be seen here; and it reads one sink, not every sink a second writer could choose.",
	// gofmt keeps this row long on purpose: the reason IS the deliverable (the gate
	// asks for a decision, not a token).
	"internal/tools/run_ground_collect_clone_test.go:TestRunGroundCollectSkipsAGoneCloneGround": "correct — both halves. The claim is \"this ground was NOT removed, so the removal audit carries no record for it\", and the negated set is the audit sink filtered to removalaudit.KindWorktreeRemoval entries whose target is THIS fixture's directory (run_ground_collect_clone_test.go:133 removalRecords). Half (a) correct: the claim is EVIDENCE OF ABSENCE about an act, and the act is not performed — RunGroundCollect writes the record IMMEDIATELY BEFORE the removal on each leg (internal/tools/run_ground_anchor.go:706 for the clone leg, :727 for the linked leg) and every refusal above it is a `skipped` line plus a `continue`, so a refused ground reaches no writer at all. Half (b) no idempotent member is excluded: the checked set is a RECORD SET, not a step sequence — there is no member a resolver could still owe, and the one step that must never run (the removal) is exactly what the record would announce, so an empty set is the removal not having happened. The specific path is the GONE rung (run_ground_anchor.go:652, os.Stat + os.IsNotExist), which is asked FIRST and continues before any other question: the directory does not exist, so no verb could remove it, and the test asserts POSITIVELY that the skip names runGroundCollectGoneReason and does NOT quote git's \"is not a working tree\". What the proof can still not separate: the filter is (kind, target-path), so a removal record written for a DIFFERENT path in the same sink would not be seen here; and it reads one sink, not every sink a second writer could choose.",
	// gofmt keeps this row long on purpose: the reason IS the deliverable (the gate
	// asks for a decision, not a token).
	"internal/tools/task_run_ground_clone_anchor_test.go:TestRunGroundCollectStillRefusesARepoLessLinkedWorktree": "correct — both halves. The claim is \"the leg did not free this ground, so the removal audit carries no record for it\", and the negated set is the audit sink filtered to removalaudit.KindWorktreeRemoval entries whose target is THIS fixture's directory (removalRecords). Half (a) correct: the act is not performed — RunGroundCollect writes the removal record IMMEDIATELY BEFORE each leg's removal (run_ground_anchor.go, the clone leg and the linked leg), and every refusal above those writers is a `skipped` line plus a `continue`, so a refused ground reaches no writer at all. Half (b) no idempotent member is excluded: the checked set is a RECORD SET, not a step sequence — there is no member a resolver could still owe, and the one step that must never run (the removal) is exactly what the record would announce, so an empty set IS the removal not having happened. The refusal is asserted POSITIVELY in the same test FOUR ways: `removed` must not contain the path, `why` must be a reported skip naming the unknown repository (either the anchor's own no-repo refusal or the undelivered-commits probe's `repo \\\"\\\"`), os.Stat proves the directory survived, and git rev-parse --verify proves the BRANCH survived too (this arm never removes a ref). The ground is also driven to the apply leg rather than merely asserted absent from `collectable`: the anchor carries a dead pid, so liveness clears it. What the proof can still not separate: the filter is (kind, target-path), so a removal record written for a DIFFERENT path in the same sink would not be seen here; and it reads one sink, not every sink a second writer could choose.",
	// ── the two e2e/gate assertions the 2026-11-06 measurement found unclassified
	// (row 01a1ace3; each is a GOOD assertion whose site carried no decision — the
	// classification list simply lagged the assertion it was written for).
	//
	// gofmt keeps these rows long on purpose: the reason IS the deliverable.
	"cmd/agent/bare_prompt_guard_e2e_test.go:TestBarePromptIsRefusedInsideAnAgentSession": "correct — both halves. The claim is 'a refused bare-prompt launch minted NO session directory under the pinned GOPI_HOME', and the negated set is every entry barePromptSessionDirNames reads back from <home>/sessions (os.ReadDir; os.IsNotExist yields nil). Half (a) — the claim is right, and the act is NOT performed: cmd/agent/main.go:873 calls guardBarePromptOrExit BEFORE readInput (main.go:884) and BEFORE newAgent (main.go:903), and the ONE production writer of a sessions directory is the MkdirAll at internal/agent/session.go:327, reached from NewSessionLoggerForRun (session.go:171) inside newAgent — so the refusal (config.RefuseBarePrompt, internal/config/agentguard.go:379-381, os.Exit(AgentGuardRefusalExitCode)) exits upstream of every writer, and had the refusal not fired the very next statement would have minted the directory this assertion measures absent. Half (b) — no idempotent member is wrongly suppressed: the checked set is a DIRECTORY LISTING, not a step sequence; a session directory has no member that must still be created on a refusal (the run it would record does not exist), so an empty set is the refusal having cost nothing, never a member being hidden. The POSITIVE carrier in the SAME file is TestBarePromptStillRunsFromAHumanTerminal (bare_prompt_guard_e2e_test.go:198): it drives the SAME argv past the SAME guard with HumanTerminalEnv and asserts mock.RequestCount() != 0 at :230 — that is what stops this guard from passing by forbidding the face outright; and the two sibling negative assertions in this test pin the defect's other artifacts (:160 the model was reached 0 times, so the refusal precedes the spend; :173-174 the ledger carries no [auto] row). What the proof can still not separate: it reads ONE directory, so a session container minted at a second root (a GOPI_PROFILE override, or a sandbox's own .gopi) would not be seen here; and it observes the listing, not the writer, so a refusal that created and then removed a directory would read identically.",
	// gofmt keeps this row long on purpose: the reason IS the deliverable.
	"internal/standards/cwd_release_gate_leg2_test.go:TestGateCwdReleaseDeliveryRetryLeg2StillRejectsACaller": "correct — both halves. The claim is 'cwdReleaseDeliveryRetryCallerViolations returns NO offending arm for a fixture that removes a directory and calls no arm', i.e. the verdict keys on the ARM and not on the removal verb; the negated set is the three names in cwdReleaseDeliveryRetryArms (DeliveryRetryTaskID / DeliveryRetryLines / DeliveryRetryJSON) searched in the fixture's COMMENT-STRIPPED code via cwdReleaseCode (internal/standards/cwd_release_gate_test.go:716-738, whose scanner.ScanComments blanks every comment byte). Half (a) — the claim is right: the negated observation is unreachable for this fixture, which is the point. The fixture source (cwd_release_gate_leg2_test.go:64-72) carries ZERO arm token — measured: `sed -n '64,72p' ... | grep -c DeliveryRetry` = 0, comment included — and its only call is reclaimGitRemove (internal/tools/task_reclaim.go:2690), which is the REMOVAL verb and not an arm. Its positive control is asserted FIRST in the same test (:105 requires a non-empty verdict for cwdReleaseLeg2FixtureCalling, which does call tools.DeliveryRetryTaskID and tools.DeliveryRetryLines), so a predicate that flagged this fixture too would be keying on the removal verb — exactly the 0ee5745c conjunction this leg exists to kill (the decision block at cwd_release_gate_test.go:642-666 records why: the conjunction made the caller-side half unsatisfiable, because leg ① asserts the arms' library reaches no removal verb). Half (b) — no idempotent member is wrongly suppressed: the checked set is a SET OF ARM NAMES, not a step sequence, so there is no member a resolver could still owe; the one thing that must NOT be reported here (a caller obligation this fixture does not have) is precisely what the empty verdict states is absent. What the proof can still not separate: it proves the verdict is clean on ONE fixture, not that no scanned-tree production caller exists (that is the exhaustive arm at cwd_release_gate_test.go:1371); and it reads a token set, so a future arm spelled as a prefix of a removal verb would satisfy both halves without really being an arm.",
}
