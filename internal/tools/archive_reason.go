package tools

// The REASONED archive lane (epic hand-archive-gate, 2026-09-16).
//
// INCIDENT: at 21:04 on 2026-09-16 nine `refs/tags/archive/<branch>` refs
// appeared within ONE second (the loose-ref mtimes are identical to the
// millisecond) for nine branches in /home/user/work/GoPi — five of them
// carrying `completed` task rows — with no ledger row and no reason recorded
// anywhere. Every in-code archive lane (the park lane, the merge-time compile
// gates) writes an ANNOTATED record next to the ref; these nine had none, and
// three of them (`fix/merger-status-log-rewire`, `feat/web-m1-b4b7`,
// `feat/web-m2-b5b8b9b12`) were not even non-fast-forwardable, so not one
// automatic lane could have produced them. That is the definition of the HAND
// path: `git tag archive/<branch> <branch>` typed at a shell.
//
// The defect is not the tag — the tag is the project's ONE decided-exclusion
// namespace and five readers depend on it (repoSweepArchiveExempt,
// taskMergeExempt, task_deliverable_branch.go, task_merged_cleanup.go,
// scripts/safe-merge.sh). The defect is that a tag carries a DECISION with no
// WHY, and a decision without a why cannot be reviewed, mined or reversed by
// anyone who was not in that shell.
//
// The fix has two halves, both here:
//
//	① the REASON VOCABULARY (below) plus ONE writer (ArchiveTagReasoned) that
//	   records the reason in a COMPANION ref beside the archive tag:
//	   refs/gopi/archive-reason/<branch>, a blob whose first line is
//	   `reason: <code>`. Every archive path in the project — the merger's park
//	   lane, the merge-time compile gates, the human `gopi task archive` verb —
//	   writes through it, so "was a reason recorded?" becomes a mechanical
//	   question (the companion ref is there, or it is not) rather than a
//	   convention nobody can check.
//	② the REFUSAL GATE (ArchiveDecide), the human face: archiving a branch
//	   whose owning task row reached completed/verified is REFUSED unless the
//	   caller names a reason AND acknowledges the override. Archiving work the
//	   ledger calls finished is exactly the 21:04 batch, and it must cost more
//	   than one word.
//
// What deliberately does NOT change: the ref NAME, its namespace, and the
// SHAPE of refs/tags/archive/<branch>. The tag stays a LIGHTWEIGHT ref on the
// branch tip — exactly what every archive lane has always written — and the
// reason lives in a companion ref outside refs/tags/.
//
// The first design recorded the reason in the tag's OWN message (an ANNOTATED
// tag). It reads back more directly — no second ref — but it CHANGES WHAT THE
// REF POINTS AT: on an annotated tag `git rev-parse archive/<branch>` returns
// the TAG object, not the commit. Measured: the merger's park lane asserts
// "the park ref points at the branch tip — the parked work must be exactly
// what was not merged" (TestRepoSweepParksNonFFBranchAndContinues), and that
// test went RED on this branch. The archive ref is read repo-wide as a COMMIT
// (verify_gate.go's ancestry walk, task_merged_cleanup.go's is-ancestor probe,
// task_merge_backlog.go's branch enumeration), so the annotation was not a
// local change to one lane: it silently re-pointed a shared ref at a tag
// object. A gate must never change the shape of the thing it gates; the
// companion ref records the WHY while leaving the decided-exclusion ref
// byte-identical to what every reader already expects.
//
// The companion ref also removes a class of silent failure. With a
// message-based record, "was a reason recorded?" is an INFERENCE from the
// object type plus git's %(contents:subject) paragraph folding — where a
// mis-emitted blank line turns the record back into prose and the scan reads
// no reason. With a ref, presence IS the record. And it rewrites nothing: the
// tag is only ever CREATED, never forced, so no historical ref is touched (the
// reason-less ones stay the evidence they are, and are reported by
// ArchiveReasonlessBacklog until a human records a reason).

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The reason vocabulary (Rule 1b: every code spelled once, and every code the
// CLI accepts is one of these). These are the five WHY's the epic names, plus
// unowned for the branch no ledger row ever tracked — the 21:04 batch
// contained one of those (`fix/merger-status-log-rewire`), and a reason lane
// that cannot express "nobody owned this" would push an operator back to the
// unreasoned tag.
const (
	// ArchiveReasonCompileFailure — the branch does not compile while the
	// merge target does (the merge-time BUILD/UNION/contract gates).
	ArchiveReasonCompileFailure = "compile-failure"
	// ArchiveReasonNotFFAblePark — the branch does not fast-forward: the park
	// lane's reason (conflict resolution is semantic work the daemon must not
	// invent).
	ArchiveReasonNotFFAblePark = "not-ff-able-park"
	// ArchiveReasonFailedVerify — the branch's post-merge verification failed
	// and the work is not being repaired.
	ArchiveReasonFailedVerify = "failed-verify"
	// ArchiveReasonSuperseded — a later branch/commit already carries the
	// intent; this one is spent.
	ArchiveReasonSuperseded = "superseded"
	// ArchiveReasonHandMistake — a human archived it by hand; the code names
	// exactly the path that produced the 21:04 batch, so a future operator
	// filing work this way says so out loud.
	ArchiveReasonHandMistake = "hand-mistake"
	// ArchiveReasonUnowned — no ledger row ever tracked this branch, so there
	// is no task decision to consult; the tag records the absence.
	ArchiveReasonUnowned = "unowned"
	// ArchiveReasonManifestDuplicate — the merge result would write a manifest
	// row key TWICE (two sides each added a row for one key at different
	// positions, so git unioned them instead of conflicting), written by the
	// merge-time MANIFEST gate. A fourth AUTOMATIC lane, so it sits with its
	// siblings in the vocabulary's ordering.
	ArchiveReasonManifestDuplicate = "manifest-duplicate"
	// ArchiveReasonPublishedAnchor — the merge result would carry a DOCUMENT OF
	// RECORD whose published anchors no longer match the sources it publishes
	// (the document's generator was not re-run after the anchor sources moved,
	// on one side or on the union of both), written by the merge-time
	// PUBLISHED-ANCHOR gate. A fifth AUTOMATIC lane, so it sits with its
	// siblings in the vocabulary's ordering.
	ArchiveReasonPublishedAnchor = "published-anchor"
	// ArchiveReasonUnrelatedHistory — the branch shares NO commit with the
	// integration branch (git merge-base exits 1), so no merge lane can ever
	// land it: git refuses the merge outright and there is no conflict for a
	// resolver to rework. Third AUTOMATIC lane's reason, written by `gopi
	// merge`'s unrelated-history verdict (cmd/agent/merge_run.go), and the
	// reason it exists is DURABILITY: without it the verdict is terminal for
	// one run ONLY — the branch stays a candidate, the next pass re-decides it
	// and the merge-backlog generator re-files "merge <branch> into master"
	// every pass (measured 2026-09-22: feat/sync-conflict-completed was filed
	// 4×). The tag is the durable half of the refusal; the refusal itself is
	// unchanged (no --allow-unrelated-histories lane).
	ArchiveReasonUnrelatedHistory = "unrelated-history"
	// ArchiveReasonReclaimCarried — the archive tag ALREADY recorded the
	// decision; this code says the head ref was then RECLAIMED, i.e. the arm
	// that removes a decided branch's ref carried the existing decision through
	// and re-checked, immediately before the removal, that the tag still named
	// that very commit. Written by the archive-ref reclaim arm
	// (archiveReclaimDelete → ArchiveTagReasoned), which used to delete the head
	// and record NOTHING: measured 2026-10-17, 11 decided heads removed by this
	// arm produced 10 reason-less archive refs, so the operator's own
	// `gopi task archive --check` reported the arm's output as debt.
	//
	// It is NOT the code for a fresh archive decision — a lane that decides a
	// branch against carries its own reason (park/compile/unrelated-history/…).
	// This one is deliberately the LAST thing that happens to a decision, which
	// is why it can never overwrite a recorded one: when the tag already has a
	// reason (every other lane writes one), the record stays as its author wrote
	// it and this code is never used. It exists for the legacy reason-less tags
	// the reclaim arm is the only surviving reader of.
	ArchiveReasonReclaimCarried = "reclaim-carried"
)

// ArchiveReasons is the ordered vocabulary an operator sees (usage text,
// refusal messages, completion). Ordered by how often a lane needs it, not
// alphabetically: the two automatic lanes' codes come first.
var ArchiveReasons = []string{
	ArchiveReasonNotFFAblePark,
	ArchiveReasonCompileFailure,
	// The third automatic lane (epic unrelated-history-durable-retire): it sits
	// with its two siblings because it is written by a lane, not chosen by an
	// operator — the ordering above is "automatic lanes first".
	ArchiveReasonUnrelatedHistory,
	// The fourth automatic lane (merge-time duplicate-manifest gate): its code
	// sits with the other three for the same reason — a lane writes it, no
	// operator chooses it.
	ArchiveReasonManifestDuplicate,
	// The fifth automatic lane (merge-time published-anchor gate): a lane writes
	// it, so it sits with the other four and no operator chooses it.
	ArchiveReasonPublishedAnchor,
	// The reclaim arm's code (epic archive-reclaim-reason). It is written by a
	// LANE, not chosen by an operator — but it is deliberately LAST in this
	// automatic group, because it is the code a decision acquires when the tag
	// had none and the arm that carries the decision through is the only reader
	// left: the reason says "this was already decided, the ref is now reclaimed"
	// rather than naming why the branch lost.
	ArchiveReasonReclaimCarried,
	ArchiveReasonFailedVerify,
	ArchiveReasonSuperseded,
	ArchiveReasonHandMistake,
	ArchiveReasonUnowned,
}

// ArchiveReasonDoc is the one-line human gloss for a reason code. The codes
// live in ArchiveReasons; their DOCUMENTATION lives here beside the writer so
// `gopi task archive -h` and the refusal message can never describe a lane
// differently (Rule 13: one definition, every reader references it).
func ArchiveReasonDoc(code string) string {
	switch code {
	case ArchiveReasonNotFFAblePark:
		return "the branch does not fast-forward into master (the merger's park lane's own reason)"
	case ArchiveReasonCompileFailure:
		return "the branch does not build against master (a merge-time compile gate refused it)"
	case ArchiveReasonUnrelatedHistory:
		return "the branch shares no commit with master (git refuses the merge outright); retired by the since-removed `gopi merge` verb, never merged"
	case ArchiveReasonManifestDuplicate:
		return "the merge result would write a manifest row key twice (two sides added the same key at different positions and git unioned them); refused by the merge-time manifest gate"
	case ArchiveReasonPublishedAnchor:
		return "the merge result would carry a document of record whose published anchors no longer match the sources it publishes (the generator was not re-run after the anchor sources moved); refused by the merge-time published-anchor gate"
	case ArchiveReasonFailedVerify:
		return "the branch's post-merge verification failed and the work is not being repaired"
	case ArchiveReasonSuperseded:
		return "a later branch/commit already carries the intent"
	case ArchiveReasonHandMistake:
		return "a human archived it by hand and the batch was a mistake"
	case ArchiveReasonUnowned:
		return "no task row ever tracked this branch"
	case ArchiveReasonReclaimCarried:
		return "the branch's archive decision was already recorded as a tag and its head ref was then reclaimed (the tag still named that ref's own commit); written by the archive-ref reclaim arm"
	}
	return ""
}

// ArchiveReasonList renders the vocabulary for a usage/refusal message (one
// definition, so the CLI help and the refusal cannot list different codes).
func ArchiveReasonList() string { return strings.Join(ArchiveReasons, "|") }

// ArchiveReasonKnown reports whether code is one of the named reasons. The
// gate uses it so a free-form "reason" cannot slip in: the whole point of the
// vocabulary is that the WHY is queryable.
func ArchiveReasonKnown(code string) bool {
	for _, r := range ArchiveReasons {
		if r == code {
			return true
		}
	}
	return false
}

// archiveReasonLinePrefix is the machine-readable marker the reason record's
// first line carries. The record is the companion ref's BLOB (see
// ArchiveTagReasoned), read with `git cat-file blob` — so the marker is parsed
// out of a body whose lines this code owns, never out of git's folded
// ref-metadata view of a tag message.
const archiveReasonLinePrefix = "reason: "

// ArchiveBranchResult is the outcome of the human archive path, as the CLI
// prints it: what was decided, and what ref now carries the record.
type ArchiveBranchResult struct {
	Tag     string `json:"tag"`
	Branch  string `json:"branch"`
	Reason  string `json:"reason"`
	Verdict string `json:"verdict"`
	Refusal string `json:"refusal,omitempty"`
	Owner   string `json:"owner_task_id,omitempty"`
	Status  string `json:"owner_status,omitempty"`
	// Successor, when set, names the rework row whose existence the archive was
	// allowed to proceed PAST (ArchiveDecision.SuccessorTaskID). It is rendered
	// beside the owner so the operator sees the exception being taken.
	Successor string `json:"successor_task_id,omitempty"`
	Unowned   bool   `json:"unowned,omitempty"`
	Recorded  bool   `json:"recorded"`
}

// RetireUnrelatedHistoryBranch is the MECHANISM half of `gopi merge`'s
// unrelated-history verdict (epic unrelated-history-durable-retire): it writes
// the durable retirement for a branch no merge lane can ever land, into the
// SAME archive namespace every other merge path already honors
// (repoSweepArchiveExempt → mergeCandidateBranches / taskMergeExempt /
// safe-merge.sh), with the reason ArchiveReasonUnrelatedHistory.
//
// IT IS THE RECORD-WRITER, NOT A SECOND DECISION. Whether the branch may be
// retired at all is the owner gate's answer inside the one writer
// (repoSweepArchiveBranch → repoSweepArchiveOwnerGate): a branch whose ledger
// row says the work FINISHED is NOT retired, and the returned error carries the
// gate's own refusal sentence for the caller to print. It exists as its own
// exported name so cmd/agent never reaches into the tools package's mechanism —
// the caller's spellings live beside the reason vocabulary that defines them.
//
// The BRANCH REF IS NEVER DELETED. The tag is a decision, not a deletion: the
// ref stays in refs/heads, and removing the tag re-arms the branch for a future
// merge (archive_reason.go's namespace contract).
func RetireUnrelatedHistoryBranch(repo, branch string) error {
	return repoSweepArchiveBranch(repo, branch, ArchiveReasonUnrelatedHistory)
}

// ArchiveBranch is the WHOLE human archive path in one function: gate, write,
// report. It exists so the CLI, a test and any future caller share one
// sequence (Rule 13) — a caller that ran the gate but wrote its own ref would
// be the second writer this epic removes.
//
// A refusal is returned as a RESULT with Verdict=refused and a nil error: the
// gate fired, which is the answer, not a failure of the call. Only a genuine
// error (bad reason, unreadable ledger, git failure) is an error — so a caller
// can distinguish "I asked and was told no" from "I could not ask".
func ArchiveBranch(repo, branch, reason, detail string, ack bool) (ArchiveBranchResult, error) {
	dec, err := ArchiveDecide(branch, reason, ack)
	if err != nil {
		return ArchiveBranchResult{}, err
	}
	res := ArchiveBranchResult{Branch: strings.TrimSpace(branch), Verdict: dec.Verdict,
		Refusal: dec.Refusal, Owner: dec.OwnerTaskID, Status: dec.OwnerStatus, Unowned: dec.Unowned,
		Successor: dec.SuccessorTaskID, Reason: dec.EffectiveReason}
	if dec.Verdict == ArchiveVerdictRefused {
		return res, nil
	}
	tag, err := ArchiveTagReasoned(repo, res.Branch, res.Reason, detail)
	if err != nil {
		return res, err
	}
	res.Tag = tag
	res.Recorded = true
	return res, nil
}

// ArchiveBranchCwd is ArchiveBranch against the repository the process is
// standing in. The repo is RESOLVED, never assumed: an archive tag written into
// the wrong repo is the worst possible outcome of a gate that exists to make
// decisions legible.
//
// IT IS NOT THE `gopi task archive` VERB'S DOOR (measured defect 01a1301b,
// 2026-10-12). The verb used to call THIS function for its branch lane, which
// made `--repo` inert: the flag was parsed, advertised in the usage text and
// honoured by every census mode, while the branch lane silently acted on the
// process cwd. The verb now resolves the repo once through the SAME resolver the
// other four modes use (cmd/agent/cli_archive.go's archiveTargetRepo →
// tools.RepoRootAt) and calls ArchiveBranch with the resolved path. A caller
// that HAS a repo to act on must therefore call ArchiveBranch directly; this
// entry point survives only for a caller whose subject is genuinely "wherever
// this process stands", and the CLI is no longer one of them.
func ArchiveBranchCwd(branch, reason, detail string, ack bool) (ArchiveBranchResult, error) {
	repo, err := repoRootOf(".")
	if err != nil {
		return ArchiveBranchResult{}, err
	}
	return ArchiveBranch(repo, branch, reason, detail, ack)
}

// ArchiveTagReason extracts the reason code out of a reason RECORD — the
// companion ref's blob (see ArchiveTagReasoned). An empty record means no
// reason was ever recorded ("" = reason-less, the 21:04 shape).
//
// Only a code from the closed vocabulary counts: an unknown spelling is not a
// recorded reason (Rule 15 — no credit for a typo), and the closed vocabulary
// is what makes the recorded WHY queryable instead of prose.
func ArchiveTagReason(record string) string {
	for _, line := range strings.Split(record, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if !strings.HasPrefix(line, archiveReasonLinePrefix) {
			continue
		}
		code := strings.TrimSpace(strings.TrimPrefix(line, archiveReasonLinePrefix))
		if ArchiveReasonKnown(code) {
			return code
		}
		return "" // a malformed reason line is not a reason
	}
	return ""
}

// archiveReasonRefPrefix is the companion-ref namespace carrying a decided
// branch's WHY: refs/gopi/archive-reason/<branch>, one ref per archived branch.
//
// It is deliberately OUTSIDE refs/tags/. The tag namespace is what five
// readers match on and what git's own `git tag -l` / `git log --tags` surfaces
// list, so a reason kept as a second TAG would double every listing and invite
// a reader to mistake a reason for an archive decision. refs/gopi/ is this
// tool's private namespace (Rule 14: one definition, referenced by every
// reader), and it is not swept, pruned or pushed anywhere — verified: the only
// mirrors that can see it push everything (memory-sync's refspec).
const archiveReasonRefPrefix = "refs/gopi/archive-reason/"

// gitObjectTypeCommit is git's object-type name for the shape an archive tag
// MUST keep: a ref that resolves straight to a COMMIT. Every archive lane
// writes a lightweight tag, and readers repo-wide treat the ref as a commit
// (verify_gate.go's ancestry walk, task_merged_cleanup.go's is-ancestor probe,
// task_merge_backlog.go's branch enumeration) — an annotated tag would answer
// `git rev-parse archive/<branch>` with a TAG object instead (measured).
const gitObjectTypeCommit = "commit"

// archiveReasonRecordFmt is the reason record's first line — the reason alone,
// because this line is what the scan reads. The blank line after it is
// LOAD-BEARING: it separates the machine line from the audit body (the
// Summary/blank/Body convention), so the record reads as a summary plus
// provenance rather than one run-on paragraph.
const archiveReasonRecordFmt = archiveReasonLinePrefix + "%s\n"

// archiveReasonRecordBodyFmt is the audit body under the reason line: WHAT was
// archived, WHERE the decision lives, WHO did it and WHEN — the facts a reviewer
// of a decided branch needs and the 21:04 batch left none of. The `repo:` value
// is the REPOSITORY (archiveReasonRepo), never the caller's checkout.
// Its label is archiveReasonRepoFieldPrefix.
const archiveReasonRecordBodyFmt = `branch: %s
repo:   %s
actor:  %s
task:   %s
at:     %s
detail: %s
`

// archiveDetailUnstated is the honest placeholder for a caller that supplied
// no free-text detail: the record SAYS the detail was not given rather than
// leaving an empty line that reads as an omission.
const archiveDetailUnstated = "(none stated)"

// archiveActor resolves who is recording the decision — the OS user and host
// of the process writing the ref. Best-effort by design: an unresolvable
// identity degrades to the honest "unknown" instead of failing an archive the
// operator explicitly asked for (the REASON is the mandatory half, not the
// identity).
const archiveActorUnknown = "unknown"

func archiveActor() string {
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return archiveActorUnknown
	}
	host, herr := os.Hostname()
	if herr != nil || host == "" {
		return u.Username
	}
	return u.Username + "@" + host
}

// archiveReasonRecord renders the reason record. Split from the writer so a
// test can pin the exact shape the scan parses without running git (the scan
// reads line 1; the writer must never move the reason off it).
//
// `repo` is the CALLER's path — the checkout it stood in, which may be a linked
// worktree. The record's `repo:` field is NOT that path: it is the repository
// the ref was actually written into (see archiveReasonRepo).
func archiveReasonRecord(repo, branch, reason, detail, taskID string, now time.Time) string {
	if strings.TrimSpace(detail) == "" {
		detail = archiveDetailUnstated
	}
	if taskIDNormalize(taskID) == "" {
		taskID = archiveOwnerNone
	}
	return fmt.Sprintf(archiveReasonRecordFmt, reason) + "\n" +
		fmt.Sprintf(archiveReasonRecordBodyFmt, branch, archiveReasonRepo(repo), archiveActor(), taskID,
			now.Format(time.RFC3339), detail)
}

// The git vocabulary archiveReasonRepo speaks. The COMMON-DIR spelling is not
// repeated here: reading that convention is mergeExecMainRepo's single job
// (Rule 13/14), and this function calls it rather than parsing it a second time.
//
//	finishCleanupWorktreeVerb/ListVerb/ListFlag + finishCleanupPorcelainWorktreePrefix
//	— `git worktree list --porcelain` and the "worktree <path>" entry prefix.
//	These are NOT re-spelled here: task_finish_cleanup.go owns the git-verb and
//	porcelain vocabulary of this package (Rules 1b/13, guarded by
//	TestFinishCleanupWorktreeVerbLiteralGate), and a second spelling of the
//	porcelain entry prefix ("worktree" plus a space) is exactly how the two
//	readers of one porcelain registry drift.
//	archiveReasonBareRepoFlag/TrueToken — `--is-bare-repository` answers "true"
//	for a bare repo, which has no work tree to name.
//	archiveReasonUnresolvedFmt — what the record says when the repository
//	cannot be resolved; the reason travels WITH it (Rule 15: an admitted
//	unknown, never a wrong fact asserted).
const (
	archiveReasonBareRepoFlag  = "--is-bare-repository"
	archiveReasonTrueToken     = "true"
	archiveReasonUnresolvedFmt = "unknown (%s: %v)"
	// archiveReasonRepoFieldPrefix is the record's `repo:` label, spelled once
	// so a READER of the field (a report, a test, a human scanning the blob)
	// matches the same token the body format writes.
	archiveReasonRepoFieldPrefix = "repo:"
)

// archiveReasonRepo answers the ONE question the record's `repo:` field exists
// to answer: WHICH REPOSITORY holds the ref that was just written?
//
// It is deliberately NOT the caller's path (epic 01a14a8c, measured 2026-10-15):
// `gopi task archive` is normally run from a linked worktree
// (/home/user/workspace/gopi-<task>), while the ref store it writes into is the
// SHARED one (<main>/.git). Recording the caller's checkout made the field name
// a directory the finish-cleanup chain deletes at hand-over — so the record's
// claim about where the decision lives became false the moment the worktree was
// reaped, and two archives of one branch from different worktrees differed in a
// field that describes the REPOSITORY, not the shell.
//
// The resolution walks the git facts in order, and every step is one git call
// because that is the instrument that knows the answer:
//
//	① `--is-bare-repository` → the repo IS its own path (a bare repo has no work
//	   tree to name, and the ref store is inside it);
//	② `--git-common-dir` → the shared git dir, read through `mergeExecMainRepo`,
//	   the ONE reader of that convention (Rule 13/14): <dir>/.git means <dir> is
//	   the main checkout; otherwise it is a linked worktree's own git dir
//	   (<main>/.git/worktrees/<name>) and the main checkout is its ancestor
//	   before the ".git" component;
//	③ `git worktree list --porcelain` → the MAIN checkout's own registered path,
//	   corroborated by the prefix check against step ② — it is the directory a
//	   human would open and whose .git holds the ref, so it beats a path
//	   reconstructed from a string split;
//	④ nothing resolvable → the record states that, WITH the reason
//	   (archiveReasonUnresolvedFmt), instead of asserting the caller's path as
//	   if it had been the repository. Rule 15: a wrong fact is worse than an
//	   admitted unknown.
func archiveReasonRepo(repo string) string {
	bare, err := repoSweepGit(repo, gitRevParseVerb, archiveReasonBareRepoFlag)
	if err != nil {
		return fmt.Sprintf(archiveReasonUnresolvedFmt, repo, err)
	}
	if strings.TrimSpace(bare) == archiveReasonTrueToken {
		if abs, aerr := filepath.Abs(repo); aerr == nil {
			return abs
		}
		return filepath.Clean(repo)
	}
	main, err := mergeExecMainRepo(repo)
	if err != nil {
		return fmt.Sprintf(archiveReasonUnresolvedFmt, repo, err)
	}
	if out, lerr := repoSweepGit(repo, finishCleanupWorktreeVerb,
		finishCleanupWorktreeListVerb, finishCleanupWorktreeListFlag); lerr == nil {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(strings.TrimRight(line, "\r"))
			path, ok := strings.CutPrefix(line, finishCleanupPorcelainWorktreePrefix)
			if !ok || path == "" { // not a "worktree <path>" line
				continue
			}
			path = strings.TrimSpace(path)
			if path == main || strings.HasPrefix(main, path+string(filepath.Separator)) {
				return path
			}
		}
	}
	return main
}

// archiveOwnerNone is how the tag body names the absent ledger row (the
// ArchiveReasonUnowned case) — one spelling, so a reader never has to guess
// between "", "none" and "n/a".
const archiveOwnerNone = "(none)"

// ArchiveTagReasoned records the DECISION on refs/tags/archive/<branch> and
// its WHY on the companion ref refs/gopi/archive-reason/<branch>, returning the
// tag name it created (or found). It is the ONE archive writer in the project
// (Rule 13/14): the merger's park lane, the merge-time compile gates and the
// `gopi task archive` CLI all reach the namespace through it, so the reason record
// cannot be a property of one caller.
//
// The tag is LIGHTWEIGHT and stays so — the shape every archive lane has
// always written, and the shape the ref's readers depend on (see the package
// comment: an annotated tag makes `rev-parse` answer with a TAG object and
// re-points a ref that repo-wide code reads as a commit). The reason is a
// separate ref, so gating the archive namespace does not change the archive
// namespace.
//
// Idempotence is BY REASON, in two directions:
//   - an archive tag that already has a reason record is left alone (the
//     decision is recorded, and re-writing the record would rewrite a note a
//     reviewer may already have read);
//   - an existing tag WITHOUT a record (a legacy lightweight ref, e.g. the
//     21:04 batch) gets the reason supplied — that is the remediation path for
//     the backlog. The TAG itself is never rewritten: `git update-ref` would
//     keep the same commit, but the ref is not touched at all, so the
//     remediation cannot alter what the branch was archived at.
func ArchiveTagReasoned(repo, branch, reason, detail string) (string, error) {
	return archiveTagReasonedRef(repo, branch, branch, reason, detail)
}

// archiveTagReasonedAt is the AT-A-REVISION door of the same writer: the archive
// tag is minted at an EXPLICIT revision instead of a branch name. It exists for
// the one caller whose subject is a commit NO branch names — the unreachable-tip
// rescue (`gopi task archive --rescue-tip`) — and it is deliberately NOT exported
// and NOT a second writer: it is the same function, with the same tag shape
// (lightweight, at a commit), the same companion reason ref, the same "%w"
// errors and the same idempotence rules. The only thing that differs is how the
// tag's target is SPELLED, which is why it is a parameter and not a copy.
func archiveTagReasonedAt(repo, branch, rev, reason, detail string) (string, error) {
	return archiveTagReasonedRef(repo, branch, rev, reason, detail)
}

// archiveTagReasonedRef is the ONE archive writer: tag at rev, WHY on the
// companion ref. `target` is the revision git is asked to tag — a branch name on
// the ordinary path, a full commit sha on the rescue path.
func archiveTagReasonedRef(repo, branch, target, reason, detail string) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", fmt.Errorf("archive: branch must not be empty")
	}
	if !ArchiveReasonKnown(reason) {
		return "", fmt.Errorf("archive: unknown reason %q (want %s)", reason, ArchiveReasonList())
	}
	tag := archiveBranchPrefix + branch
	// The TAG is ensured FIRST, unconditionally, and that order is
	// load-bearing. The park lane's contract is that parking a branch restores
	// its archive ref ("the park is what keeps the branch out of the queue" —
	// TestRepoSweepParkBranchRowIsIdempotent): a re-park after a dropped tag
	// must re-protect the branch, and the park row that already exists is about
	// the ROW, never about leaving the ref gone. Consulting the record first
	// would skip the tag whenever a stale record outlived its tag.
	cnt, err := repoSweepGit(repo, "tag", "-l", tag)
	if err != nil {
		return "", fmt.Errorf("archive tag probe %s: %w", tag, err)
	}
	if strings.TrimSpace(cnt) == "" {
		// Lightweight, at the branch tip: the shape every reader expects. A tag
		// that already exists is NOT re-written (see the doc comment), so
		// re-archiving cannot move what the ref points at.
		if _, err := repoSweepGit(repo, gitTagVerb, tag, target); err != nil {
			return "", fmt.Errorf("archive %s: %w", tag, err)
		}
	}
	rec, err := archiveReasonRecordOf(repo, branch)
	if err != nil {
		return "", err
	}
	if ArchiveTagReason(rec) != "" {
		// Already recorded — idempotent, and never rewritten: a reviewer may
		// already have read this record, and a RETRY must not restamp the
		// record's "at:" time (the record names when the decision was made).
		return tag, nil
	}
	taskID := archiveOwnerTaskID(branch)
	rec = archiveReasonRecord(repo, branch, reason, detail, taskID, time.Now())
	if err := archiveWriteReasonRecord(repo, branch, rec); err != nil {
		return "", err
	}
	return tag, nil
}

// archiveWriteReasonRecord writes the companion ref's blob. The record goes in
// through `git hash-object -w --stdin`, so the message is passed as stdin —
// never as an argv word, where a NUL or a newline in a caller's --detail would
// be mangled by the shell's argv encoding (the reason vocabulary is closed, but
// the detail is free text).
func archiveWriteReasonRecord(repo, branch, record string) error {
	sha, err := repoSweepGitStdin(repo, record, "hash-object", "-w", "--stdin")
	if err != nil {
		return fmt.Errorf("archive reason blob: %w", err)
	}
	if sha == "" {
		return fmt.Errorf("archive reason blob: git hash-object returned no object id")
	}
	ref := archiveReasonRefPrefix + branch
	if _, err := repoSweepGit(repo, "update-ref", ref, sha); err != nil {
		return fmt.Errorf("archive reason ref %s: %w", ref, err)
	}
	return nil
}

// archiveReasonRecordOf reads ONE branch's reason record ("" when none was ever
// recorded). It reads the companion ref through the same namespace enumeration
// the backlog scan uses (one parser, so the writer's idempotence check and the
// scan can never disagree about what a reason looks like — Rule 13) rather than
// a second `cat-file` shape.
func archiveReasonRecordOf(repo, branch string) (string, error) {
	// A malformed entry for THIS branch reads as "no record": the caller then
	// writes a fresh blob over the ref, which is the documented repair (a
	// ref that carries no readable reason has nothing to preserve). Every
	// OTHER branch's entries are unaffected — the scan is per entry.
	records, _, err := archiveReasonRecords(repo)
	if err != nil {
		return "", err
	}
	return records[strings.TrimSpace(branch)], nil
}

// archiveRefFormat / archiveFieldSep / archiveFieldCount define the ONE wire
// shape both namespace scans use (Rule 13): ref name, object type, ref message
// subject. Each scan is a single `git for-each-ref` over its whole namespace —
// no per-ref subprocess — so a scan stays O(1) git calls however many decided
// branches the repo accumulates.
const (
	archiveRefFormat  = "%(refname:short)%09%(objecttype)%09%(contents:subject)"
	archiveFieldSep   = "\t"
	archiveFieldCount = 3
)

// archiveNamespaceSubjectSep splits a branch name out of the message subject
// git folds for a LIGHTWEIGHT ref. `%(contents:subject)` is the COMMIT's own
// first line for a lightweight tag (an archive tag has no message of its own),
// and a commit subject routinely ends with " (branch <name>)" — the tool's own
// commit convention. The archive namespace scan therefore takes the branch from
// the REF NAME (`%(refname:short)`, authoritative, and all five readers match
// on exactly that) and uses the subject only as a fallback.
const archiveNamespaceSubjectSep = " (branch "

// archiveReasonRefFormat is the companion namespace's scan shape: the ref name
// and its OBJECT TYPE, and nothing else.
//
// The tag namespace's 3-field format (name + objecttype + contents:subject)
// MUST NOT be reused here, and the first version of this scan did — the
// companion ref points at a BLOB, and a blob has no `%(contents:subject)`, so
// git emitted a 2-field line, the 3-field guard skipped every entry, and the
// whole namespace read as "no reason recorded". The writer's idempotence test
// caught it (the record was rewritten on a second call). A namespace's wire
// shape belongs to that namespace: this one needs two fields, so it asks for
// two.
//
// THE TYPE IS WHAT MAKES ONE BAD REF SURVIVABLE (measured defect 01a143b4,
// 2026-10-12). The scan used to ask for the name alone and then read every ref
// with `git cat-file blob <ref>`; a single ref pointing at a COMMIT made that
// call exit 128, the scan returned the error, and EVERY archive face died at
// once — `--check`, the branch-archive write, the park lane, the unreachable
// census — leaving 5 of 11 landed branches unreclaimed for a whole merge run.
// The type comes from the SAME for-each-ref call (no extra subprocess), so the
// reader can CLASSIFY instead of guess, and a wrong-typed entry is reported by
// name while the readable records are still returned (Rule 15's aggregate).
//
// archiveObjectTypeFormat is the shared for-each-ref atom (never a bare
// literal): the tag namespace asks for it too.
const archiveReasonRefFormat = archiveRefNameFormat + archiveFieldSep + archiveObjectTypeFormat

// archiveReasonRecords reads the WHOLE companion namespace in one git call,
// branch → record. One call per scan, not one per archived branch: the
// namespace accumulates a ref per decided branch, and a per-branch read would
// turn every status line into N git subprocesses.
//
// The scan asks for the FULL refname (`%(refname)`) AND the object type
// (`%(objecttype)`): the branch key is a prefix trim against the full namespace
// path, and the type decides HOW the record is read. The record body itself is
// still read per ref with `cat-file` (a blob body is not a for-each-ref field).
//
// ONE WRONG-TYPED REF MUST NOT BLIND EVERY READER (measured defect 01a143b4,
// 2026-10-12). The scan used to read EVERY ref with `git cat-file blob <ref>`
// and return on the first failure, so a single ref pointing at a COMMIT made
// the whole namespace unreadable and disabled the archive faces repo-wide
// (--check, the branch-archive write, the park lane, the unreachable census) —
// 5 of 11 landed branches went unreclaimed for a merge run. The type now comes
// back WITH the name, so a wrong-typed entry is CLASSIFIED and SKIPPED, and the
// readable records are still returned.
//
// The skip is REPORTED, never swallowed (Rule 15: a record that cannot be read
// must not look like a record that was never written). The malformed entries
// come back as the second return, ALL AT ONCE, so every caller decides how loud
// to be: the surfaces that own a report render them by name (and count them as
// a finding), while a reader whose answer is not about the namespace proceeds
// on the readable records instead of dying.
func archiveReasonRecords(repo string) (map[string]string, []ArchiveReasonMalformed, error) {
	return archiveReasonRecordsClassified(repo)
}

// ArchiveReasonMalformed is one companion-namespace ref the scan could not read
// as a reason record, named so an operator can act on it. It is a REPORT, not
// an internal detail: the message a terminal prints is the Ref, the Type git
// gave it, and the reason it was refused.
type ArchiveReasonMalformed struct {
	// Ref is the FULL ref name (refs/gopi/archive-reason/<branch>) — the exact
	// word `git update-ref` needs, so the report is actionable.
	Ref string `json:"ref"`
	// Branch is the key the refname carries (the prefix-trimmed branch).
	Branch string `json:"branch"`
	// ObjectType is what git says the ref names (blob for a healthy record).
	ObjectType string `json:"object_type"`
	// Detail says WHY it was refused (a wrong type, or an unreadable body).
	Detail string `json:"detail"`
}

// ArchiveNamespaceMalformed answers "which companion refs exist but cannot be
// read as a reason record", by name, for the surface that holds the operator's
// EXIT STATUS. It is the same one scan every other reader uses (Rule 13), so the
// `--check` finding and the scan's own skips cannot disagree about which refs
// are bad — nor about how many.
func ArchiveNamespaceMalformed(repo string) ([]ArchiveReasonMalformed, error) {
	_, bad, err := archiveReasonRecordsClassified(repo)
	if err != nil {
		return nil, err
	}
	return bad, nil
}

// ArchiveReasonMalformedReport renders the AGGREGATE finding: EVERY refused
// entry, each on its own line, so one run names all of them (a reader who fixes
// one and re-runs must not discover the second only then). It returns "" for a
// clean namespace, so a caller can decide between silence and a section.
//
// Exported because the finding is not the backlog scan's private business: the
// `--check` terminal is the surface an operator reads, and it must render the
// same words the scan used (Rule 13: one spelling, two faces).
func ArchiveReasonMalformedReport(bad []ArchiveReasonMalformed) string {
	if len(bad) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, archiveReasonMalformedCountFmt, len(bad))
	for _, m := range bad {
		b.WriteString(archiveReasonMalformedLineFmt(m))
	}
	return b.String()
}

// The malformed-namespace report's spellings (Rule 1b: the scan, the aggregate
// error and the `--check` renderer all read THESE, so a reworded line cannot
// leave one surface naming a different remedy than another).
const (
	archiveReasonMalformedPrefix    = "archive reason namespace: "
	archiveReasonMalformedCountFmt  = "%d unreadable reason record(s) — the readable records are still listed below\n"
	archiveReasonWrongTypeDetailFmt = "ref points at a %s, not a %s (a reason record is a BLOB)"
	archiveReasonUnreadableDetail   = "the blob body could not be read"
	archiveReasonMalformedEntryFmt  = "  %s (%s): %s\n"
	archiveReasonFieldCount         = 2
	// archiveReasonRepairHintFmt names the remedy for a ref the scan refused:
	// re-point it at a blob written from the SAME reason, never delete an
	// archive decision (`refs/tags/archive/` is a contract; the companion ref
	// is the WHY that must survive it).
	archiveReasonRepairHintFmt = "  repair: printf '%%s\\n' '<reason line>' | git hash-object -w --stdin | xargs git update-ref %s\n" // red-proof-identity-ok: this is an operator remedy that WRITES a ref from a reason line; it takes no content-identity reading of a tracked file at all, so there is no side to anchor to a revision
)

// archiveReasonMalformedLineFmt renders one entry: the ref, its type and why it
// was refused, plus the repair command.
func archiveReasonMalformedLineFmt(m ArchiveReasonMalformed) string {
	return fmt.Sprintf(archiveReasonMalformedEntryFmt, m.Ref, m.ObjectType, m.Detail) +
		fmt.Sprintf(archiveReasonRepairHintFmt, m.Ref)
}

// archiveReasonRecordsClassified is the ONE scan behind every reader: it lists
// the namespace once, splits each line into (ref, type), and reads only the
// entries whose type is a BLOB.
//
// The classification is on the OBJECT TYPE and never on the ref NAME (the
// namespace, the branch key and the tag are all name-shaped contracts three
// readers agree on — a name filter would be the same defect wearing a hat).
func archiveReasonRecordsClassified(repo string) (map[string]string, []ArchiveReasonMalformed, error) {
	out, err := repoSweepGit(repo, gitForEachRefVerb, gitFormatFlagPrefix+archiveReasonRefFormat, archiveReasonRefPrefix)
	if err != nil {
		return nil, nil, fmt.Errorf("archive reason namespace scan: %w", err)
	}
	records := map[string]string{}
	var bad []ArchiveReasonMalformed
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		ref, objectType := archiveReasonFieldsOf(line)
		if ref == "" {
			continue // a line without a refname carries nothing to read
		}
		branch := strings.TrimPrefix(ref, archiveReasonRefPrefix)
		if objectType != gitObjectTypeBlob {
			bad = append(bad, ArchiveReasonMalformed{
				Ref: ref, Branch: branch, ObjectType: objectType,
				Detail: fmt.Sprintf(archiveReasonWrongTypeDetailFmt, objectType, gitObjectTypeBlob),
			})
			continue
		}
		rec, err := repoSweepGit(repo, gitCatFileVerb, gitObjectTypeBlob, ref)
		if err != nil {
			// A blob that cannot be READ is the same class of finding: the
			// namespace holds an entry this reader cannot answer for, and the
			// other entries must still come back (Rule 15: reported, not fatal).
			bad = append(bad, ArchiveReasonMalformed{
				Ref: ref, Branch: branch, ObjectType: objectType,
				Detail: archiveReasonUnreadableDetail + ": " + err.Error(),
			})
			continue
		}
		records[branch] = rec
	}
	sort.Slice(bad, func(i, j int) bool { return bad[i].Ref < bad[j].Ref })
	return records, bad, nil
}

// archiveReasonFieldsOf splits one namespace scan line into (ref, objecttype).
// The line is the scan's own two-field shape, so the split is bounded: a refname
// cannot contain the field separator, and a line git emitted with fewer fields
// than the format asked for answers ("","") — the caller skips it, because a
// ref it cannot name is not a ref it may read or report.
func archiveReasonFieldsOf(line string) (ref, objectType string) {
	f := strings.SplitN(line, archiveFieldSep, archiveReasonFieldCount)
	if len(f) != archiveReasonFieldCount {
		return "", ""
	}
	return f[0], f[1]
}

// The primary-branch reader that used to live here (deliverableBranchOf: its own
// json.Unmarshal of the row's "branch" field) is GONE — epic 01a13214. It was the
// second reader of a fact the ledger's deliverable blob already has ONE parser
// for (taskDeliverableParse, task_session.go), and the reason it had to go is the
// divergence it produced: it read the PRIMARY axis only, so a branch a run
// CREATED (a recorded ground) made its row a candidate for `merge --status` but
// not for the archive face, and one branch got two owner readings.
//
// The axes are enumerated in task_owner_axes.go, through the same scan and the
// same parser the branch lookup uses. Nothing here needs to re-read the blob.

// ArchiveBacklogRow is one reason-less archive tag over a branch whose owning
// task row reached a terminal, DECIDED status. It is the unit of the 21:04
// batch report.
type ArchiveBacklogRow struct {
	Tag         string `json:"tag"`
	Branch      string `json:"branch"`
	OwnerTaskID string `json:"owner_task_id,omitempty"`
	OwnerStatus string `json:"owner_status,omitempty"`
	ObjectType  string `json:"object_type"`
	Reason      string `json:"reason,omitempty"`
}

// ArchiveReasonlessBacklog reports every archive tag in repo that carries NO
// reason while its branch's owning task row says the work was DECIDED —
// completed or verified (taskStatusCompleted / taskStatusVerified).
//
// That intersection IS the 21:04 batch: a decided branch archived without a
// why is indistinguishable from a mistake, and the batch buried five
// `completed` rows this way. Branches whose owner is failed/cancelled are
// deliberately NOT reported — archiving failed work is the ordinary, correct
// outcome and its reason is legible from the row itself.
//
// Unreadable ledger: the function reports the error rather than returning an
// empty (clean) backlog — a scan that cannot see the ledger must never be
// mistaken for a scan that found nothing (Rule 15: no silent green).
func ArchiveReasonlessBacklog(repo string) ([]ArchiveBacklogRow, error) {
	entries, err := archiveNamespaceEntries(repo)
	if err != nil {
		return nil, err
	}
	reasons, _, err := archiveReasonRecords(repo)
	if err != nil {
		return nil, err
	}
	owners, err := archiveBranchOwners()
	if err != nil {
		return nil, err
	}
	var rows []ArchiveBacklogRow
	for _, e := range entries {
		if ArchiveTagReason(reasons[e.branch]) != "" {
			continue // a recorded reason closes the row
		}
		owner, ok := owners[e.branch]
		if !ok || !archiveOwnerDecided(owner.status) {
			continue
		}
		rows = append(rows, ArchiveBacklogRow{
			Tag: e.tag, Branch: e.branch, ObjectType: e.objectType,
			OwnerTaskID: owner.taskID, OwnerStatus: owner.status,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Tag < rows[j].Tag })
	return rows, nil
}

// ArchiveNamespace lists the repo's decided-exclusion refs with each ref's
// recorded reason. Read-only, and it never fails a caller: an unreadable
// namespace yields nil (the status surface reports what it can — Rule 15's
// "record it" half is the Err field on the caller, which the sweep already
// populates). Order is the git for-each-ref order (refname), so the status
// line is stable between ticks.
func ArchiveNamespace(repo string) []ArchiveBacklogRow {
	entries, err := archiveNamespaceEntries(repo)
	if err != nil {
		return nil
	}
	// The reasons come from their own namespace. An UNREADABLE reason record is
	// reported as no-reason here rather than failing the whole listing: this is
	// a read-only operator surface (`--status`), and dropping the branch list
	// because one record is unreadable would hide the graveyard the line exists
	// to show. The backlog check, whose whole answer is the reason, does NOT
	// degrade this way — it reports the error (Rule 15).
	reasons, _, rerr := archiveReasonRecords(repo)
	// The OWNING rows come from the same probe the refusal gate and the backlog
	// check use (archiveBranchOwners), so "who owns this branch" and "is that
	// row's work finished" are answered identically in the report, the crosswalk
	// line and the gate (Rule 13). An unreadable ledger leaves the owner fields
	// empty rather than failing the listing — but then nothing is marked BURIED,
	// which is why the caller ALSO reports a listing failure (Rule 15).
	owners, oerr := archiveBranchOwners()
	if oerr != nil {
		return nil
	}
	out := make([]ArchiveBacklogRow, 0, len(entries))
	for _, e := range entries {
		reason := ""
		if rerr == nil {
			reason = ArchiveTagReason(reasons[e.branch])
		}
		o := owners[e.branch]
		out = append(out, ArchiveBacklogRow{Tag: e.tag, Branch: e.branch, Reason: reason,
			OwnerTaskID: o.taskID, OwnerStatus: o.status})
	}
	return out
}

// ArchiveRowsForRepo is the ARCHIVED SET of repo as DATA: one row per archive
// tag, carrying its branch, its ref, the reason its record holds and the owning
// ledger row with its status. Rendering is the caller's job (the merger's sweep
// line and `gopi merge --status` both render these rows), so the two faces read
// one set instead of enumerating the namespace twice (Rule 13).
//
// The second return is the LISTING error, kept separate from an empty set: the
// caller must be able to say "the question was never answered" rather than
// "nothing is archived" (Rule 15). ArchiveNamespace folds that failure into a
// nil slice, which a count would render as a clean zero — this wrapper does not.
func ArchiveRowsForRepo(repo string) ([]ArchiveBacklogRow, error) {
	if _, err := archiveNamespaceEntries(repo); err != nil {
		return nil, err
	}
	return ArchiveNamespace(repo), nil
}

// ArchiveNamespaceOrphans lists reason records whose archive TAG is gone: the
// companion namespace has a ref for a branch that refs/tags/archive/ no longer
// names. Deleting an archive tag is what un-arms a branch for a future merge
// (the namespace's documented contract), so a record must not outlive its tag —
// otherwise the next archive of that branch would find a record for a decision
// that was reversed, and the idempotence probe would refuse to write the new
// reason.
//
// Reported, never auto-deleted: removing a ref is a decision, and this
// function's job is to make the residue visible (Rule 15), not to make it.
func ArchiveNamespaceOrphans(repo string) ([]string, error) {
	entries, err := archiveNamespaceEntries(repo)
	if err != nil {
		return nil, err
	}
	reasons, _, err := archiveReasonRecords(repo)
	if err != nil {
		return nil, err
	}
	tagged := make(map[string]bool, len(entries))
	for _, e := range entries {
		tagged[e.branch] = true
	}
	var orphans []string
	for branch := range reasons {
		if !tagged[branch] {
			orphans = append(orphans, branch)
		}
	}
	sort.Strings(orphans)
	return orphans, nil
}

// archiveOwnerDecided reports whether a task status means the work was
// DECIDED (completed/verified) — the statuses whose branch must never be
// archived without a reason. Reads the exported TaskStatus* constants so a
// status rename cannot leave this predicate behind.
func archiveOwnerDecided(status string) bool {
	return status == TaskStatusCompleted || status == TaskStatusVerified
}

// The owner-row ranks, most-cautious first. They are ORDINALS for the pick in
// archiveBranchOwners — never a verdict, never a status spelling a caller
// compares against (Rule 1b: a caller asks archiveOwnerRank, it does not
// restate the order). The middle rank is named for the predicate that owns it
// rather than for the statuses it contains, so "which statuses count as
// completed" stays a one-place question.
const (
	// archiveOwnerRankLive — the row may be a running deliverable, so it is the
	// reading an archive decision must act on. Assigned to every status this
	// package does not classify (pending/claimed/in_progress AND any future or
	// unknown status): a status nobody reasoned about must not become the
	// permission to delete refs, which is the default-deny direction Rule 15's
	// "an unanswered question is not a state" asks for.
	archiveOwnerRankLive = iota
	// archiveOwnerRankDecided — completed/verified: the work finished, so an
	// archive buries it and needs the caller's explicit override.
	archiveOwnerRankDecided
	// archiveOwnerRankArchivable — failed/cancelled/superseded: the ledger
	// already decided against the work, so tagging the branch is bookkeeping.
	archiveOwnerRankArchivable
)

// archiveOwnerRank orders owner STATUSES by how safe it is to act on the row,
// most cautious first — the SMALLER rank wins the pick in archiveBranchOwners,
// so live beats decided beats archivable. It is the ONE place that answers
// "which of these rows describes the branch", so the human gate (ArchiveDecide),
// mechanism gate (repoSweepArchiveOwnerGate), the backlog check and the
// `gopi merge --status` line can never disagree about it (Rule 13).
//
// The order is a safety order, and that is deliberate rather than tidy: the
// cheap predicates (archiveOwnerDecided / archiveOwnerArchivable) are
// INDEPENDENT booleans that do NOT partition the status space — a `cancelled`
// row satisfies neither archiveOwnerDecided nor "live", and the pre-fix pick
// ("an open row supersedes a decided one") treated it as the open side
// (archive_reason.go's archiveBranchOwners carries the measurement). Ranking
// the three readings explicitly makes the classification total: every status
// lands in exactly one rank, and the one it lands in is the safest reading it
// can support.
func archiveOwnerRank(status string) int {
	switch {
	case archiveOwnerArchivable(status):
		return archiveOwnerRankArchivable
	case archiveOwnerDecided(status):
		return archiveOwnerRankDecided
	}
	return archiveOwnerRankLive
}

// archiveNamespaceEntry is one ref in the archive namespace, as the single
// for-each-ref returns it. branch is derived from the REF NAME (authoritative —
// see archiveNamespaceSubjectSep); subject is kept for the scan's diagnostics.
type archiveNamespaceEntry struct {
	tag        string
	branch     string
	objectType string
	subject    string
}

// archiveNamespaceEntries lists the whole archive namespace in one git call.
func archiveNamespaceEntries(repo string) ([]archiveNamespaceEntry, error) {
	out, err := repoSweepGit(repo, "for-each-ref", "--format="+archiveRefFormat, archiveTagRefPrefix)
	if err != nil {
		// An empty namespace is not an error (`for-each-ref` prints nothing),
		// so a failure here is a real git/repo failure — report it.
		return nil, fmt.Errorf("archive namespace scan: %w", err)
	}
	var entries []archiveNamespaceEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.SplitN(line, archiveFieldSep, archiveFieldCount)
		if len(f) < archiveFieldCount {
			continue
		}
		entries = append(entries, archiveNamespaceEntry{
			tag: f[0], branch: archiveBranchOfEntry(f[0], f[2]), objectType: f[1], subject: f[2],
		})
	}
	return entries, nil
}

// archiveBranchOfEntry derives the gated branch from an archive-entry ref name.
//
// The entry's ref comes from `%(refname:short)`, which STRIPS `refs/` — and
// whenever two refs would shorten alike git disambiguates by prepending the
// remaining path (a branch literally named `archive/<x>` makes the tag print as
// `tags/archive/<x>`). So the branch is taken as everything after the FIRST
// `archive/` in the ref name, which is correct for every short form git can
// produce and needs no knowledge of which refs happen to collide right now.
// The ref's message subject is only a fallback, never the authority:
// `%(contents:subject)` is the COMMIT's own first line for a lightweight tag.
func archiveBranchOfEntry(ref, subject string) string {
	if i := strings.Index(ref, archiveBranchPrefix); i >= 0 {
		return ref[i+len(archiveBranchPrefix):]
	}
	if i := strings.Index(subject, archiveNamespaceSubjectSep); i >= 0 {
		return strings.TrimSuffix(subject[i+len(archiveNamespaceSubjectSep):], ")")
	}
	return strings.TrimSpace(subject)
}

// archiveOwner is the ledger half of a backlog row: which row tracked the
// branch, and where it stands.
type archiveOwner struct {
	taskID string
	status string
	// shape is the row's own text (tags + summary), carried so the SUCCESSOR
	// question can be asked of the row the gate is actually holding
	// (task_rework_successor.go). It is the pick's payload, not a second
	// lookup: two rows naming the branch keep their own shapes.
	shape reworkRowShape
}

// archiveBranchOwners maps branch name → the row that owns it, for EVERY
// branch an archive tag names (and for the branches that carry no tag at all —
// the map is a property of the LEDGER, and the callers look up the branches they
// hold).
//
// THE CANDIDATE SET IS THE SHARED ONE (epic 01a13214, task_owner_axes.go). The
// rows come from taskOwnerAxesAll, the ONE enumeration of "which ledger rows
// name a branch, through which axis" — so a row whose branch appears as a GROUND
// it created is a candidate here exactly as it is in TaskForBranch, and
// `gopi task archive --check` can no longer answer `(none)` for a branch
// `gopi merge --status` resolves to a row (the measured divergence this epic
// closes). The AXES are unified; the RANK below is this face's own opinion and
// stays.
//
// WHEN SEVERAL ROWS NAME ONE BRANCH, THE MOST CAUTIOUS READING WINS — and that
// rule is the fix for a measured lie (2026-09-21/22, branch
// fix/stash-guard-compound-chain). The pick used to be "a decided row beats a
// live one", which meant a branch with a `completed` row AND an `in_progress`
// row rendered as `(completed)`: `gopi merge --status` reported the branch
// buried while two rows were still building on it, and the branch's ground was
// removed 22:52:20 on the strength of that reading. A row's status was being
// printed for a branch it was not the state of. The statuses are read from the
// ledger either way (this function invents nothing); what changes is WHICH
// row's status the branch is described by. A branch NO row names stays absent
// from the map — the caller renders that gap as `unknown`, never as a status
// the ledger does not hold (Rule 15a: a report that cannot be trusted is worse
// than no report).
//
// THE RANK IS THE SAFETY ORDER, NOT THE LIFECYCLE ORDER (measured 2026-10-07,
// zpeer, run 01a115c3, branch task/01a113e467767eb9a4c33cb1df1cdfbb). Four rows
// named that ONE branch — among them a `cancelled` one, a `completed` one and
// an `in_progress` one — and the old rule ("an OPEN row supersedes a DECIDED
// one") was ALREADY satisfied by the `cancelled` row: archiveOwnerDecided is
// false for it, so it counted as the open side and it was kept (it came first
// in the scan). The gate then read that status through archiveOwnerArchivable →
// Proceed, so `gopi task archive <branch> --reason hand-mistake` exited 0 and
// minted `archive/<branch>` with NO override, over a branch whose run was still
// building. That tag is exactly what arms ArchiveReclaimDecidedRefs's
// head-deleting arm, so the near miss was a running deliverable, not a
// bookkeeping record. "Open" therefore may not mean "not completed": the row
// that describes the branch is the row whose reading is SAFEST to act on, which
// is the order archiveOwnerRank assigns — LIVE (pending/claimed/in_progress,
// and every status this package does not classify) ahead of DECIDED
// (completed/verified) ahead of ARCHIVABLE (failed/cancelled/superseded).
// Equal ranks are broken by task id so repeated runs answer identically
// whatever order the rows come back in.
func archiveBranchOwners() (map[string]archiveOwner, error) {
	records, err := taskOwnerAxesAll()
	if err != nil {
		return nil, err
	}
	owners := map[string]archiveOwner{}
	for _, rec := range records {
		row := archiveOwner{taskID: rec.taskID, status: rec.status, shape: rec.shape}
		prev, seen := owners[rec.branch]
		if !seen || archiveOwnerSafer(row, prev) {
			owners[rec.branch] = row
		}
	}
	return owners, nil
}

// archiveOwnerSafer reports whether a (the candidate) describes the branch more
// safely than b (the row held so far), by the rank order archiveOwnerRank
// defines. Equal ranks are broken by TASK ID so the pick does not depend on the
// order the ledger returned the rows in: without that tie-break the same ledger
// could answer differently across runs (a real hazard here, since the old pick
// was itself order-sensitive — see archiveBranchOwners' measurement), and a
// decision that varies between two identical runs is not a decision. The
// empty-id case falls through both comparisons and keeps b, which is correct:
// a row with no id owns nothing (ArchiveDecide already treats "" as unowned).
func archiveOwnerSafer(a, b archiveOwner) bool {
	if ra, rb := archiveOwnerRank(a.status), archiveOwnerRank(b.status); ra != rb {
		return ra < rb
	}
	return strings.TrimSpace(a.taskID) != "" && a.taskID < b.taskID
}

// archiveOwnerTaskID returns the id of the row owning branch ("" when no row
// does) — the tag body's provenance line.
func archiveOwnerTaskID(branch string) string {
	owners, err := archiveBranchOwners()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(owners[branch].taskID)
}

// The refusal gate's verdicts (Rule 1b: the CLI, the tests and the refusal
// message all spell them from here).
const (
	// ArchiveVerdictProceed — the archive may go ahead (with the effective
	// reason the decision carries).
	ArchiveVerdictProceed = "proceed"
	// ArchiveVerdictRefused — the archive is refused; Refusal says why.
	ArchiveVerdictRefused = "refused"
)

// archiveLiveOwnerEscapeVerbFmt is the ONE spelling of the operable escape a
// LIVE-owner refusal names: the `gopi task update` that concludes the row the
// gate is holding. A refusal an operator cannot act on is a wall, not a gate
// (ArchiveDecision.Refusal's own contract), and "wait for the row to conclude"
// without the verb is exactly that wall — the operator has no way to know the
// row is theirs to conclude, or how.
const archiveLiveOwnerEscapeVerbFmt = "gopi task update %s --status " + TaskStatusCancelled +
	"  (or let it finish: " + TaskStatusCompleted + ")"

// ArchiveDecision is the human archive path's gate verdict for one branch.
type ArchiveDecision struct {
	Verdict string
	// Refusal is the operator-facing explanation when Verdict is refused. It
	// always names the owning row, its status, and the exact flag that
	// overrides — a refusal an operator cannot act on is a wall, not a gate.
	Refusal string
	// EffectiveReason is the reason the tag will carry. For the unowned case
	// it is forced to ArchiveReasonUnowned: a caller's guess about an untracked
	// branch is worth less than the recorded fact that nothing tracked it.
	EffectiveReason string
	OwnerTaskID     string
	OwnerStatus     string
	Unowned         bool
	// SuccessorTaskID names the rework row this branch is the SOURCE of, when
	// the gate let the archive through on that exception rather than on the
	// ledger already having decided against the work. It is empty for every
	// other proceed (archivable owner, unowned, override). It exists so an
	// operator reading `gopi task archive`'s output can see WHY a LIVE row did
	// not stop the archive — the exception is named at the moment it is taken,
	// never left to be inferred (Rule 15).
	SuccessorTaskID string
}

// archiveProtectedRefusalSentence is guard ⓪'s operator-facing refusal: the ONE
// sentence both the archive gate and (through its derivation) the reclaim sweep's
// guard-④ row speak, so a reader who saw the sweep refuse `master` and a reader
// who saw the verb refuse it are reading the SAME measurement.
//
// It is DERIVED from ArchiveProtectedRefusalText rather than restating the fact,
// and it appends the two things a caller of an ARCHIVE needs and a reader of a
// reclaim row does not: that nothing was written, and that the refusal is
// terminal (there is no override flag — the predicate is a name, and a name is
// not an argument a flag can win).
func archiveProtectedRefusalSentence(branch string) string {
	return fmt.Sprintf(
		"archive refused: %s\n"+
			"  %s\n"+
			"  NOTHING WAS WRITTEN: no refs/tags/%s%s and no %s%s — the refusal runs BEFORE the first write, "+
			"so the integration head is never recorded as a decided loser (GOPI.md Rule 9).\n"+
			"  There is no override for this one: archiving master/main is not a decision about a branch, it is "+
			"a decision about the integration head, and every merge path reads an archive tag as exactly that.",
		archiveBranchPrefix+branch, ArchiveProtectedRefusalText(branch),
		archiveBranchPrefix, branch, archiveReasonRefPrefix, branch)
}

// ArchiveDecide answers "may this branch be archived, and with what reason?"
// for the HAND path — the `gopi task archive` verb. It implements the epic's
// three-way rule exactly:
//
//	① the owning row is failed/cancelled → PROCEED (the work is already
//	   decided against; archiving it is bookkeeping, not burial).
//	② the owning row is completed/verified → REFUSE unless the caller passed
//	   an explicit override naming a reason. This is the 21:04 batch's exact
//	   shape: five `completed` rows buried by hand.
//	③ no owning row → PROCEED, with the reason forced to ArchiveReasonUnowned
//	   so the record says "nothing tracked this" instead of accepting a story.
//
// One case the epic does not name, decided here in the SAFE direction and
// documented rather than left to a caller's judgement: a LIVE owning row
// (pending/claimed/in_progress) is REFUSED too. Archiving a branch whose work
// is still in flight buries a running deliverable, and the safe reading of an
// unspecified case is the one that does not destroy work.
//
// ONE documented exception to that live rule, and it exists because the rule as
// written is a CLOSED LOOP (task_rework_successor.go carries the measurement):
// a row that is the branch's REWORK SUCCESSOR — a live row in the rework lane
// whose summary names this branch as its SOURCE while its deliverable is a
// different branch — does not block archiving the branch it succeeds. The merge
// run's own recipe (§3.6.1 case ④) files that row so the ref CAN go; a gate
// that blocked on it kept the ref forever and the next pass filed a duplicate
// row for the same branch. The exception is read from the row's own
// tags/summary/deliverable, never from a caller's claim, and it is the only
// looseness this gate has: every OTHER live row still refuses.
//
// WHICH ROW, when several name the branch: this gate never sees the rows — it
// sees the ONE row archiveBranchOwners picked, and that pick is the most
// cautious reading of them all (archiveOwnerRank assigns live the smallest
// rank, so live beats decided beats archivable). So a branch carrying both a
// `cancelled` row and an `in_progress` row is refused here with the IN FLIGHT
// sentence below, no matter which of the two the ledger returned first; the
// measured case where it was not is at archiveBranchOwners.
//
// overrideReason is the caller's explicit reason ("" when none was given) and
// overrideAck is the caller's explicit acknowledgement flag. BOTH are
// required to clear a refusal: a reason code without the acknowledgement is
// a typo away from an accident, and an acknowledgement without a reason
// records nothing — the epic's rule is "refused unless the caller names WHY",
// and naming it is a two-part act.
func ArchiveDecide(branch, overrideReason string, overrideAck bool) (ArchiveDecision, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return ArchiveDecision{}, fmt.Errorf("archive: branch must not be empty")
	}
	// GUARD ⓪ — THE VETO BEFORE THE WRITE (measured defect, 2026-10-12).
	//
	// The reclaim arm has always refused master/main by NAME (guard ④,
	// archiveReclaimProtected). The WRITE did not: `ArchiveBranch` wrote
	// refs/tags/archive/master AND refs/gopi/archive-reason/master and only THEN
	// asked the reclaim arm to carry the decision through — which refused, so the
	// same invocation printed `archived master as archive/master` and
	// `skipped … never a reclaim candidate` at once, exit 0.
	//
	// The harm is not the contradiction. refs/tags/archive/<branch> is the
	// project's decided-EXCLUSION namespace and every merge path reads it as "a
	// loser was decided here" (Rule 9). Tagging the INTEGRATION HEAD with it is
	// the exact inversion the archive lane exists to prevent: the branch the whole
	// system merges INTO, recorded as decided against.
	//
	// The question "is this branch out of an archive lane's reach by name?" has
	// ONE definition (archiveReclaimProtected, Rule 13) and this is its second
	// caller; the refusal SENTENCE comes from the same place the sweep's row does
	// (ArchiveProtectedRefusalText). It runs BEFORE any ledger read and before any
	// ref exists, so a refused archive leaves no trace at all — the property the
	// mechanism-side owner gate already documents for its own refusal.
	if archiveReclaimProtected(branch) {
		return ArchiveDecision{Verdict: ArchiveVerdictRefused,
			Refusal: archiveProtectedRefusalSentence(branch)}, nil
	}
	owners, err := archiveBranchOwners()
	if err != nil {
		return ArchiveDecision{}, err
	}
	owner, ok := owners[branch]
	if !ok || strings.TrimSpace(owner.taskID) == "" {
		return ArchiveDecision{Verdict: ArchiveVerdictProceed, EffectiveReason: ArchiveReasonUnowned,
			Unowned: true}, nil
	}
	d := ArchiveDecision{OwnerTaskID: owner.taskID, OwnerStatus: owner.status,
		EffectiveReason: strings.TrimSpace(overrideReason)}
	switch {
	case archiveOwnerArchivable(owner.status):
		d.Verdict = ArchiveVerdictProceed
	case archiveOwnerDecided(owner.status):
		d.Verdict = ArchiveVerdictRefused
		d.Refusal = fmt.Sprintf(
			"archive refused: %s is archived as a DECIDED loser, but task %s owns this branch and its status is %q — "+
				"the ledger says that work finished, so burying it silently is the 21:04 batch this gate exists to stop.\n"+
				"  To archive it anyway, name WHY and acknowledge the override:\n"+
				"    gopi task archive %s --reason <%s> --override-completed",
			archiveBranchPrefix+branch, owner.taskID, owner.status, branch, ArchiveReasonList())
		if overrideAck && ArchiveReasonKnown(strings.TrimSpace(overrideReason)) {
			// The acknowledgement plus a named reason clears it: the decision
			// is explicit and its WHY is on the record.
			d.Verdict = ArchiveVerdictProceed
			d.Refusal = ""
		}
	default:
		// THE SUCCESSOR EXCEPTION (task_rework_successor.go). A live row that
		// names this branch as the SOURCE of a rework it has already filed is
		// not a tenant of the branch — it is the branch's SUCCESSOR, minted
		// precisely so the ref could go (merge_prompt §3.6.1 case ④: diff
		// archived → row submitted → ref removed). Refusing here is the closed
		// loop measured 2026-10-18: the ref survives every run and the next
		// merge pass files a duplicate rework row for it. It is refused by the
		// gate only when the same row could still be BUILDING on the branch,
		// which is the shape the predicate's own (c) test excludes.
		if taskReworkSuccessorOf(owner.shape, branch) {
			d.Verdict = ArchiveVerdictProceed
			d.SuccessorTaskID = owner.taskID
			break
		}
		d.Verdict = ArchiveVerdictRefused
		d.Refusal = fmt.Sprintf(
			"archive refused: task %s owns this branch and is %q — the work is IN FLIGHT. "+
				"Archiving it buries a running deliverable; wait for the row to conclude (or cancel it deliberately).\n"+
				"  If that row is instead this branch's REWORK SUCCESSOR — a live %s-lane row whose SUMMARY is a rework "+
				"OF %s (\"rework %s: …\") — then the archive is the step that should go ahead: the ref is spent and "+
				"the work lives on that row. Re-run this command once the row carries that shape; the gate proceeds on it.\n"+
				"  Otherwise the operable escape is the row itself:  %s",
			owner.taskID, owner.status, reworkLaneName, branch, branch,
			fmt.Sprintf(archiveLiveOwnerEscapeVerbFmt, owner.taskID))
	}
	if d.Verdict == ArchiveVerdictProceed && d.EffectiveReason == "" {
		d.EffectiveReason = ArchiveReasonHandMistake
	}
	return d, nil
}

// ArchiveReasonMissingLabel is what any render says about a reason-less archive
// tag — the 21:04 batch's shape, named rather than hidden. It lives HERE (the
// package that owns the reason vocabulary) so the merger's sweep line and the
// `gopi merge --status` report cannot spell it differently (Rule 13/14).
const ArchiveReasonMissingLabel = "no reason recorded"

// ArchiveBacklogReport renders the backlog check for a terminal (Rule 5: a
// check that only returns a bool is unreadable; an operator must see WHICH
// tags and WHICH task rows). Exit-code policy lives in the CLI: a non-empty
// report is a red result there.
func ArchiveBacklogReport(repo string) (string, int, error) {
	rows, err := ArchiveReasonlessBacklog(repo)
	if err != nil {
		return "", 0, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "archive backlog: %s\n", repo)
	if len(rows) == 0 {
		b.WriteString("  clean — every archive tag over a completed/verified task row carries a reason\n")
		return b.String(), 0, nil
	}
	fmt.Fprintf(&b, "  %d reason-less archive tag(s) over a completed/verified task row:\n", len(rows))
	for _, r := range rows {
		fmt.Fprintf(&b, "    %s  (task %s, %s)\n", r.Tag, r.OwnerTaskID, r.OwnerStatus)
	}
	b.WriteString("  remediate one with: gopi task archive <branch> --reason <code> --override-completed\n")
	return b.String(), len(rows), nil
}
