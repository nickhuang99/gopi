package tools

// Regression tests for the REASONED archive lane (epic hand-archive-gate,
// 2026-09-16). The incident: nine branches were archived by hand at 21:04
// with LIGHTWEIGHT tags — no reason, no ledger row — five of them over task
// rows the ledger called `completed`. These tests pin the four properties
// that make that batch impossible to repeat silently:
//
//	① the writer records a reason READABLE from a ref, while the archive tag
//	   itself keeps its pre-existing shape (LIGHTWEIGHT, at the branch tip) —
//	   an annotated tag would re-point a ref every reader treats as a commit;
//	② a completed/verified owner row is REFUSED, and only an explicit named
//	   reason + acknowledgement proceeds;
//	③ failed/cancelled and unowned branches proceed (the ordinary cases);
//	④ the backlog check reproduces the 21:04 shape (lightweight tag over a
//	   completed row) and stays empty once a reason is recorded.
//
// Real git in temp repos, per the repo_sweep_test.go fixture style: the
// subject of this package IS git orchestration, so a mocked git would test
// the mock.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// archTestRepo initializes a temp repo with a seed commit and one branch
// carrying a divergence — the shape every archive case needs (a branch that
// exists and can be tagged).
func archTestRepo(t *testing.T, branches ...string) (repo string) {
	t.Helper()
	repo = t.TempDir()
	archGit(t, repo, "init", "-q", "-b", masterBranchName)
	archGit(t, repo, "config", "user.email", "t@t")
	archGit(t, repo, "config", "user.name", "t")
	archGit(t, repo, "commit", "--allow-empty", "-q", "-m", "seed")
	for _, b := range branches {
		archGit(t, repo, "branch", b)
	}
	return repo
}

// archOrphanBranch creates branch at a NEW ROOT commit (no parent, written with
// `commit-tree`), i.e. a history that shares NO commit with master — the shape
// `gopi merge`'s unrelated-history verdict is about, and the only shape for
// which the archive-ancestry filter is discriminating: an ordinary branch
// created off a shared commit is an ANCESTOR-relation question that any tag on
// that commit answers, so a fixture built that way cannot separate a working
// filter from a blind one (measured 2026-09-22). Returns the branch tip.
func archOrphanBranch(t *testing.T, repo, branch, msg string) string {
	t.Helper()
	tree := strings.TrimSpace(archGit(t, repo, "rev-parse", "HEAD^{tree}"))
	tip := strings.TrimSpace(archGit(t, repo, "commit-tree", tree, "-m", msg))
	archGit(t, repo, "branch", "-f", branch, tip)
	return tip
}

// archGit runs git -C repo and fails the test on error.
func archGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// archObjectType is git's own answer for a ref's shape: "commit" for a
// lightweight tag (the shape every archive lane writes, and the shape the ref's
// readers require), "tag" for an annotated one.
func archObjectType(t *testing.T, repo, ref string) string {
	t.Helper()
	return strings.TrimSpace(archGit(t, repo, "cat-file", "-t", ref))
}

// archRefCommit is where a ref actually points, with tags PEELED to the commit
// they name (`rev-parse <ref>^{commit}`). The un-peeled form is the property
// that matters to every reader of the archive namespace: on an annotated tag
// `git rev-parse archive/<branch>` answers with the tag object itself, so this
// helper is how the shape contract is asserted.
func archRefCommit(t *testing.T, repo, ref string) string {
	t.Helper()
	return strings.TrimSpace(archGit(t, repo, "rev-parse", ref+"^{commit}"))
}

// archRawRef is the UNPEELED answer — what a reader that treats the archive ref
// as a commit gets. Pinned against archRefCommit by the shape test below.
func archRawRef(t *testing.T, repo, ref string) string {
	t.Helper()
	return strings.TrimSpace(archGit(t, repo, "rev-parse", ref))
}

// archReasonRecord is the companion reason record's blob — the readback path
// the CLI prints (`git cat-file blob refs/gopi/archive-reason/<branch>`).
func archReasonRecord(t *testing.T, repo, branch string) string {
	t.Helper()
	return archGit(t, repo, "cat-file", "blob", archiveReasonRefPrefix+branch)
}

// archLedgerFile points GOPI_MEMORY_DB at a temp ledger for the test.
func archLedgerFile(t *testing.T) string {
	t.Helper()
	db := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv(memoryDBEnv, db)
	return db
}

// archSeedRow creates a ledger row owning branch, in the given status — the
// ledger half of every refusal case. Built from the package's own TaskNew /
// TaskUpdate so the fixture cannot drift from the schema (a raw INSERT would
// have to restate the column set the writers already own), and walked through
// the LEGAL transition path because the writers enforce a state machine — a
// fixture that bypassed it would be testing a row the real code cannot
// produce. Paths used: in_progress is TaskNew's own default; cancelled /
// failed / completed are one hop from it; verified is two
// (in_progress → completed → verified).
func archSeedRow(t *testing.T, taskID, status, branch string) {
	t.Helper()
	if _, err := TaskNew("archive-gate fixture "+taskID, taskID, "", ""); err != nil {
		t.Fatalf("TaskNew(%s): %v", taskID, err)
	}
	switch status {
	case TaskStatusInProgress:
		// TaskNew's default — nothing to walk.
	case TaskStatusVerified:
		for _, step := range []string{TaskStatusCompleted, TaskStatusVerified} {
			if _, err := TaskUpdate(taskID, step); err != nil {
				t.Fatalf("TaskUpdate(%s -> %s): %v", taskID, step, err)
			}
		}
	default:
		if _, err := TaskUpdate(taskID, status); err != nil {
			t.Fatalf("TaskUpdate(%s -> %s): %v", taskID, status, err)
		}
	}
	db, err := openTasksDB(t.Context(), memoryDBPath())
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	deliverable := `{"repo":"/x","branch":"` + branch + `"}`
	if _, err := db.Exec(`UPDATE tasks SET deliverable = ? WHERE task_id = ?`, deliverable, taskID); err != nil {
		t.Fatalf("seed deliverable for %s: %v", taskID, err)
	}
}

// TestArchiveWriterRecordsAReadableReason — ① the reason is recorded and
// readable, and the archive tag keeps the shape every lane has always written:
// LIGHTWEIGHT, pointing straight at the branch tip.
//
// The shape half is a REGRESSION pin: the first implementation of this gate made
// the tag annotated, and `git rev-parse archive/<branch>` then answered with the
// TAG object rather than the commit. The merger's park lane asserts exactly that
// (`TestRepoSweepParksNonFFBranchAndContinues`: "the park ref points at the
// branch tip"), and three more readers repo-wide treat the ref as a commit, so
// gating the namespace must not move what it points at.
func TestArchiveWriterRecordsAReadableReason(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/a")
	tip := archRawRef(t, repo, "feat/a")

	tag, err := ArchiveTagReasoned(repo, "feat/a", ArchiveReasonSuperseded, "landed elsewhere")
	if err != nil {
		t.Fatalf("ArchiveTagReasoned: %v", err)
	}
	if tag != archiveBranchPrefix+"feat/a" {
		t.Fatalf("tag = %q, want %q", tag, archiveBranchPrefix+"feat/a")
	}
	// The shape contract: lightweight, and pointing at the commit — no peel.
	if got := archObjectType(t, repo, tag); got != gitObjectTypeCommit {
		t.Fatalf("archive ref object type = %q, want %q (a reason must not change what the ref points at)",
			got, gitObjectTypeCommit)
	}
	if got := archRawRef(t, repo, tag); got != tip {
		t.Fatalf("raw rev-parse of the archive ref = %s, want the branch tip %s", got, tip)
	}
	// The scan's own reading path.
	entries, err := archiveNamespaceEntries(repo)
	if err != nil {
		t.Fatalf("archiveNamespaceEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("namespace has %d entries, want 1", len(entries))
	}
	if entries[0].branch != "feat/a" {
		t.Fatalf("entry branch = %q, want feat/a", entries[0].branch)
	}
	rec := archReasonRecord(t, repo, "feat/a")
	if got := ArchiveTagReason(rec); got != ArchiveReasonSuperseded {
		t.Fatalf("reason read back = %q, want %q (record=%q)", got, ArchiveReasonSuperseded, rec)
	}
	// The record still carries the audit half. `repo:` names the REPOSITORY the
	// ref was written into, which for an ordinary checkout is its toplevel —
	// where `repo` here IS the main checkout, so the two agree.
	for _, want := range []string{"branch: feat/a", "repo:   " + repo, "detail: landed elsewhere"} {
		if !strings.Contains(rec, want) {
			t.Errorf("reason record missing %q; got:\n%s", want, rec)
		}
	}
}

// TestArchiveWriterRejectsAnUnknownReason — the vocabulary is closed, so a
// free-form reason cannot be recorded as if it were one of the codes.
func TestArchiveWriterRejectsAnUnknownReason(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/a")
	if _, err := ArchiveTagReasoned(repo, "feat/a", "because-i-said-so", ""); err == nil {
		t.Fatal("unknown reason accepted — the vocabulary must be closed")
	}
	if out := archGit(t, repo, "tag", "-l", archiveBranchPrefix+"feat/a"); strings.TrimSpace(out) != "" {
		t.Fatalf("a rejected write still created %q", out)
	}
}

// TestArchiveWriterIsIdempotentByReason — re-running does not rewrite a
// recorded reason (a reviewer may already have read it), and it DOES record one
// for a legacy reason-less ref (the remediation path for the 21:04 batch) —
// without touching that legacy tag: the ref is only ever created, never forced.
func TestArchiveWriterIsIdempotentByReason(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/legacy")

	// Legacy shape: a lightweight tag with no record, exactly as the 21:04
	// batch left it.
	archGit(t, repo, "tag", archiveBranchPrefix+"feat/legacy", "feat/legacy")
	legacyTip := archRawRef(t, repo, archiveBranchPrefix+"feat/legacy")
	if got := ArchiveTagReason(mustArchRecord(t, repo, "feat/legacy")); got != "" {
		t.Fatalf("fixture already has a reason (%q)", got)
	}

	if _, err := ArchiveTagReasoned(repo, "feat/legacy", ArchiveReasonHandMistake, "batch was a mistake"); err != nil {
		t.Fatalf("remediating a legacy tag: %v", err)
	}
	if got := ArchiveTagReason(mustArchRecord(t, repo, "feat/legacy")); got != ArchiveReasonHandMistake {
		t.Fatalf("legacy ref not remediated (reason=%q)", got)
	}
	// The legacy TAG is untouched: same shape, same commit.
	if got := archObjectType(t, repo, archiveBranchPrefix+"feat/legacy"); got != gitObjectTypeCommit {
		t.Fatalf("remediation changed the tag shape (type=%q)", got)
	}
	if got := archRawRef(t, repo, archiveBranchPrefix+"feat/legacy"); got != legacyTip {
		t.Fatalf("remediation moved the archive ref: %s → %s", legacyTip, got)
	}
	first := archReasonRecord(t, repo, "feat/legacy")

	// Idempotent: the second call leaves the recorded reason alone.
	if _, err := ArchiveTagReasoned(repo, "feat/legacy", ArchiveReasonSuperseded, "a different story"); err != nil {
		t.Fatalf("second ArchiveTagReasoned: %v", err)
	}
	if got := archReasonRecord(t, repo, "feat/legacy"); got != first {
		t.Fatalf("recorded reason was rewritten:\n%s\n→\n%s", first, got)
	}
}

// mustArchRecord reads a reason record, treating "no record" as "" (the
// reason-less case a fixture asserts on) but failing on a genuine git error —
// a missing-ref message and a broken repo must not look alike in a test.
func mustArchRecord(t *testing.T, repo, branch string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "cat-file", "blob", archiveReasonRefPrefix+branch).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "Not a valid object name") {
		t.Fatalf("cat-file %s: %v: %s", archiveReasonRefPrefix+branch, err, out)
	}
	if err != nil {
		return ""
	}
	return string(out)
}

// TestArchiveGateRefusesACompletedOwner — ② the epic's core rule. A reason
// alone does not clear it: the acknowledgement is the second half.
func TestArchiveGateRefusesACompletedOwner(t *testing.T) {
	archLedgerFile(t)
	archSeedRow(t, "t-done", TaskStatusCompleted, "feat/done")

	dec, err := ArchiveDecide("feat/done", ArchiveReasonSuperseded, false)
	if err != nil {
		t.Fatalf("ArchiveDecide: %v", err)
	}
	if dec.Verdict != ArchiveVerdictRefused {
		t.Fatalf("verdict = %q, want %q (a completed row must be refused)", dec.Verdict, ArchiveVerdictRefused)
	}
	for _, want := range []string{"t-done", TaskStatusCompleted, "override-completed"} {
		if !strings.Contains(dec.Refusal, want) {
			t.Errorf("refusal does not name %q: %s", want, dec.Refusal)
		}
	}
	// No acknowledgement, even WITH a reason → still refused.
	//stylegate:ignore archive verdict in a fixture; the returned verdict is the assertion
	if dec, _ := ArchiveDecide("feat/done", ArchiveReasonSuperseded, false); dec.Verdict != ArchiveVerdictRefused {
		t.Fatal("a reason without the acknowledgement cleared the gate")
	}
	// Acknowledgement WITH a reason → proceeds, carrying that reason.
	dec, err = ArchiveDecide("feat/done", ArchiveReasonSuperseded, true)
	if err != nil {
		t.Fatalf("ArchiveDecide (override): %v", err)
	}
	if dec.Verdict != ArchiveVerdictProceed {
		t.Fatalf("verdict = %q, want %q", dec.Verdict, ArchiveVerdictProceed)
	}
	if dec.EffectiveReason != ArchiveReasonSuperseded {
		t.Fatalf("effective reason = %q, want %q", dec.EffectiveReason, ArchiveReasonSuperseded)
	}
}

// TestArchiveGateRefusesALiveOwner — the safe reading of the case the epic
// does not name: a row still in flight must not have its deliverable buried.
func TestArchiveGateRefusesALiveOwner(t *testing.T) {
	archLedgerFile(t)
	archSeedRow(t, "t-live", TaskStatusInProgress, "feat/live")
	dec, err := ArchiveDecide("feat/live", ArchiveReasonSuperseded, true)
	if err != nil {
		t.Fatalf("ArchiveDecide: %v", err)
	}
	if dec.Verdict != ArchiveVerdictRefused {
		t.Fatalf("verdict = %q, want %q — an in-flight row's branch must not be buried", dec.Verdict, ArchiveVerdictRefused)
	}
	if !strings.Contains(dec.Refusal, "IN FLIGHT") {
		t.Errorf("refusal does not say the work is in flight: %s", dec.Refusal)
	}
}

// TestArchiveGateProceedsForDecidedAndUnowned — ③ the ordinary cases, which
// must NOT be gated: archiving failed/cancelled work is bookkeeping, and a
// branch no row tracks records the absence (reason forced to unowned, so a
// caller cannot substitute a story for a fact).
func TestArchiveGateProceedsForDecidedAndUnowned(t *testing.T) {
	archLedgerFile(t)
	archSeedRow(t, "t-failed", TaskStatusFailed, "fix/dead")
	archSeedRow(t, "t-cancelled", TaskStatusCancelled, "fix/dropped")

	for _, tc := range []struct{ branch, wantReason string }{
		{"fix/dead", ArchiveReasonFailedVerify},
		{"fix/dropped", ArchiveReasonFailedVerify},
	} {
		dec, err := ArchiveDecide(tc.branch, tc.wantReason, false)
		if err != nil {
			t.Fatalf("ArchiveDecide(%s): %v", tc.branch, err)
		}
		if dec.Verdict != ArchiveVerdictProceed {
			t.Fatalf("%s: verdict = %q, want %q (owner already decided against)", tc.branch, dec.Verdict, ArchiveVerdictProceed)
		}
	}

	// Unowned: proceeds, and the caller's reason is OVERRIDDEN by the fact.
	dec, err := ArchiveDecide("feat/nobody", ArchiveReasonSuperseded, false)
	if err != nil {
		t.Fatalf("ArchiveDecide(unowned): %v", err)
	}
	if dec.Verdict != ArchiveVerdictProceed || !dec.Unowned {
		t.Fatalf("unowned branch: verdict=%q unowned=%v, want proceed/true", dec.Verdict, dec.Unowned)
	}
	if dec.EffectiveReason != ArchiveReasonUnowned {
		t.Fatalf("effective reason = %q, want %q — an untracked branch's reason is the ABSENCE of a row",
			dec.EffectiveReason, ArchiveReasonUnowned)
	}
}

// TestArchiveBranchEndToEndRefusesThenRecords is the full human path: refused
// (nothing written), then overridden (the ref carries the reason and the
// owner's id).
func TestArchiveBranchEndToEndRefusesThenRecords(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/done")
	archSeedRow(t, "t-done", TaskStatusCompleted, "feat/done")

	res, err := ArchiveBranch(repo, "feat/done", ArchiveReasonSuperseded, "landed as feat/x", false)
	if err != nil {
		t.Fatalf("ArchiveBranch (refused path): %v", err)
	}
	if res.Verdict != ArchiveVerdictRefused || res.Recorded {
		t.Fatalf("refusal wrote a ref: verdict=%q recorded=%v", res.Verdict, res.Recorded)
	}
	if out := archGit(t, repo, "tag", "-l", archiveBranchPrefix+"feat/done"); strings.TrimSpace(out) != "" {
		t.Fatalf("refused path created a tag: %q", out)
	}

	res, err = ArchiveBranch(repo, "feat/done", ArchiveReasonSuperseded, "landed as feat/x", true)
	if err != nil {
		t.Fatalf("ArchiveBranch (override): %v", err)
	}
	if !res.Recorded {
		t.Fatal("override did not record the archive")
	}
	entries, err := archiveNamespaceEntries(repo)
	if err != nil || len(entries) != 1 {
		t.Fatalf("namespace = %d entries (err=%v), want 1", len(entries), err)
	}
	if got := ArchiveTagReason(mustArchRecord(t, repo, "feat/done")); got != ArchiveReasonSuperseded {
		t.Fatalf("reason read back = %q, want %q", got, ArchiveReasonSuperseded)
	}
	if rec := archReasonRecord(t, repo, "feat/done"); !strings.Contains(rec, "task:   t-done") {
		t.Errorf("reason record does not name the owning row:\n%s", rec)
	}
}

// TestRepoResolveRowRedProof — the A/B RED PROOF of the MECHANISM-side owner
// gate, written against the mechanism entry point alone (repoSweepArchiveBranch,
// which is what the merge-time compile gate calls). It asserts the two things
// the epic requires of a red proof, and it is written so that the PRE-FIX
// function (repoSweepArchiveBranch(repo, branch) — mint a lightweight tag, ask
// nothing, record nothing) does not satisfy them:
//
//  1. RED for a branch owned by a row the ledger calls FINISHED and no recorded
//     override: the call must FAIL and must leave the archive namespace EMPTY —
//     a refusal that still minted the tag would bury the deliverable it refused
//     to bury (the tag IS the exclusion: repoSweepArchiveExempt).
//  2. RED for a branch whose owner is still IN FLIGHT: same refusal, same empty
//     namespace, no bare `git tag`-shaped burial by an unattended lane.
//
// The CONTROL arm is the third case: the same writer, a branch no ledger row
// owns, proceeds and records the reason — so the red proof cannot pass by
// refusing everything.
//
// Reproduce (pre-fix tree = master, where repoSweepArchiveBranch takes no reason
// and consults no owner): copy this file into a master worktree and run
// `go test -run TestRepoResolveRowRedProof ./internal/tools/`.
func TestRepoResolveRowRedProof(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/finished-owner", "feat/in-flight-owner", "feat/unowned")
	archSeedRow(t, "t-finished", TaskStatusCompleted, "feat/finished-owner")
	archSeedRow(t, "t-inflight", TaskStatusInProgress, "feat/in-flight-owner")

	// ① the finished owner: refused, and NOTHING is written.
	err := repoSweepArchiveBranch(repo, "feat/finished-owner", ArchiveReasonCompileFailure)
	if err == nil {
		t.Fatal("RED PROOF: the mechanism archived a FINISHED task's branch — that is the burial this gate exists to stop")
	}
	if !strings.Contains(err.Error(), "t-finished") || !strings.Contains(err.Error(), TaskStatusCompleted) {
		t.Errorf("the refusal must name the owning row and its status, got: %v", err)
	}
	if out := archGit(t, repo, "tag", "-l", archiveBranchPrefix+"feat/finished-owner"); strings.TrimSpace(out) != "" {
		t.Fatalf("the refusal still minted the archive tag %q — the burial that was refused", out)
	}
	//stylegate:ignore reads an archive reason the test wrote; the compared string is the assertion
	if rec, _ := archiveReasonRecordOf(repo, "feat/finished-owner"); rec != "" {
		t.Errorf("the refusal wrote a reason record for a decision it refused: %q", rec)
	}

	// ② the in-flight owner: the safe reading of an unspecified case — an
	// unattended lane must not bury a running deliverable either.
	if err := repoSweepArchiveBranch(repo, "feat/in-flight-owner", ArchiveReasonCompileFailure); err == nil {
		t.Fatal("RED PROOF: the mechanism archived an IN-FLIGHT task's branch")
	}
	if out := archGit(t, repo, "tag", "-l", archiveBranchPrefix+"feat/in-flight-owner"); strings.TrimSpace(out) != "" {
		t.Fatalf("the in-flight refusal still minted the tag %q", out)
	}

	// ③ the NO-REASON arm: the same writer, a branch nothing owns — the write
	// must still be refused when the call supplies no WHY, because a tag with no
	// recorded reason is exactly the 21:04 record. (Red on the pre-fix tree,
	// which had no reason parameter at all and minted the tag regardless.)
	if err := repoSweepArchiveBranch(repo, "feat/unowned", ""); err == nil {
		t.Fatal("RED PROOF: the mechanism wrote an archive with NO reason")
	}
	if out := archGit(t, repo, "tag", "-l", archiveBranchPrefix+"feat/unowned"); strings.TrimSpace(out) != "" {
		t.Fatalf("the no-reason refusal still minted the tag %q", out)
	}

	// ④ CONTROL: a named reason and no owning row → the writer proceeds and the
	// reason is on the record, so the refusals above are not a writer that always
	// refuses.
	if err := repoSweepArchiveBranch(repo, "feat/unowned", ArchiveReasonCompileFailure); err != nil {
		t.Fatalf("control arm: an unowned branch must be archivable, got %v", err)
	}
	if got := ArchiveTagReason(mustArchRecord(t, repo, "feat/unowned")); got != ArchiveReasonCompileFailure {
		t.Fatalf("control arm recorded %q, want %q", got, ArchiveReasonCompileFailure)
	}
}

// TestRetireUnrelatedHistoryBranchIsDurableAndObeysTheOwnerGate is the
// MECHANISM half of epic unrelated-history-durable-retire: what
// RetireUnrelatedHistoryBranch writes, what a second call does, and which
// branch it REFUSES to write for.
//
// Four arms, each a property the merge-side verdict depends on:
//
//  1. UNOWNED → the retirement lands: archive/<branch> exists at the branch tip,
//     the REF IS PRESERVED (refs/heads/<branch> still resolves — a tag, never
//     `branch -d`), and the record carries ArchiveReasonUnrelatedHistory.
//  2. A SECOND CALL is a no-op: no error, and the record is byte-identical —
//     the decision is not restamped and the tag is not re-pointed. This is what
//     makes the epic's second `gopi merge` pass come up empty.
//  3. A branch whose ledger row says the work FINISHED is REFUSED (the owner
//     gate), with NO tag and NO record written — a retirement that buried a
//     finished deliverable would be the 21:04 defect reached through a third
//     door. The arm also pins the ORDER between the gate and the idempotence
//     probe: it is the not-yet-archived branch that must be refused.
//  4. A branch that ALREADY carries the decision (tag + reason record) is a
//     SUCCESS even when a ledger row now owns it: an earlier pass decided it,
//     and a later pass must not turn that standing decision into a failure —
//     the reason record keeps the ORIGINAL why.
//
// Arm 5 closes the epic's generator-side audit as a test: the merge-backlog
// discovery filter (archiveTagAncestry) drops the retired branch, so the
// next generator pass owes it no "merge <branch> into master" row.
func TestRetireUnrelatedHistoryBranchIsDurableAndObeysTheOwnerGate(t *testing.T) {
	// Every branch here is an ORPHAN ROOT (archOrphanBranch): the shape this epic
	// is about, and the only fixture shape for which the ancestry filter in arm ⑤
	// is meaningful. The first draft of this test created ORDINARY branches off a
	// shared seed commit, which made archive/feat/decided — an arm-④ tag sitting
	// on that shared commit — an ANCESTOR of every other tip, so the filter
	// dropped all of them and the control arm could not tell a working filter
	// from a blind one (measured 2026-09-22, which is why the fixture is built
	// this way).
	repo := archTestRepo(t)
	orphanTip := archOrphanBranch(t, repo, "feat/orphan", "orphan root")
	tip := orphanTip
	archOrphanBranch(t, repo, "feat/finished", "finished orphan root")
	archOrphanBranch(t, repo, "feat/decided", "decided orphan root")
	unretiredTip := archOrphanBranch(t, repo, "feat/unretired", "unretired orphan root")
	// The ledger comes from the scaffold (the env pin every arm's owner gate reads).
	archLedgerFile(t)

	// ① unowned → retired, ref preserved, reason recorded.
	if err := RetireUnrelatedHistoryBranch(repo, "feat/orphan"); err != nil {
		t.Fatalf("an unowned branch must be retirable, got %v", err)
	}
	tipAfter := strings.TrimSpace(archGit(t, repo, "rev-parse", "refs/heads/feat/orphan"))
	if tipAfter != tip {
		t.Fatalf("the retirement moved the branch ref %s -> %s", tip, tipAfter)
	}
	if got := strings.TrimSpace(archGit(t, repo, "rev-parse", archiveBranchPrefix+"feat/orphan")); got != tip {
		t.Fatalf("the retirement tag points at %s, want the branch tip %s", got, tip)
	}
	if !archRefResolves(t, repo, refsHeadsPrefix+"feat/orphan") {
		t.Fatal("the branch REF was removed — the retirement is a tag, never a deletion")
	}
	if got := ArchiveTagReason(mustArchRecord(t, repo, "feat/orphan")); got != ArchiveReasonUnrelatedHistory {
		t.Fatalf("the retirement recorded reason %q, want %q", got, ArchiveReasonUnrelatedHistory)
	}
	firstRecord := mustArchRecord(t, repo, "feat/orphan")

	// ② idempotent: same ref, same record, no error.
	if err := RetireUnrelatedHistoryBranch(repo, "feat/orphan"); err != nil {
		t.Fatalf("a second retirement of the same branch must be a no-op, got %v", err)
	}
	if got := mustArchRecord(t, repo, "feat/orphan"); got != firstRecord {
		t.Errorf("the second call rewrote the reason record:\n--- first ---\n%s\n--- second ---\n%s", firstRecord, got)
	}

	// ③ a FINISHED owner is refused, and NOTHING is written.
	archSeedRow(t, "t-finished", TaskStatusCompleted, "feat/finished")
	if err := RetireUnrelatedHistoryBranch(repo, "feat/finished"); err == nil {
		t.Fatal("the owner gate let a FINISHED task's branch be retired — that buries a finished deliverable")
	}
	if archRefResolves(t, repo, "refs/tags/"+archiveBranchPrefix+"feat/finished") {
		t.Error("the refusal still minted the retirement tag")
	}
	if mustArchRecord(t, repo, "feat/finished") != "" {
		t.Error("the refusal still wrote a reason record")
	}

	// ④ an ALREADY-decided branch: the standing decision is not contradicted by
	// a later row owning the branch.
	if _, err := ArchiveTagReasoned(repo, "feat/decided", ArchiveReasonCompileFailure, "decided in an earlier pass"); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	archSeedRow(t, "t-decided", TaskStatusCompleted, "feat/decided")
	if err := RetireUnrelatedHistoryBranch(repo, "feat/decided"); err != nil {
		t.Fatalf("an already-recorded decision must not become a failure, got %v", err)
	}
	if got := ArchiveTagReason(mustArchRecord(t, repo, "feat/decided")); got != ArchiveReasonCompileFailure {
		t.Errorf("the standing record was rewritten to %q — a decided branch keeps its original why", got)
	}

	// ⑤ the generator side: the retired branch is not a merge-backlog candidate.
	// The question the filter asks is `git merge-base --is-ancestor <tag> <tip>`,
	// and the verdict tags the branch AT ITS OWN TIP — so the retired branch is
	// filtered, while the control branch is an INDEPENDENT root no tag reaches
	// (which is what makes this arm discriminating; a control descending from the
	// tag would be filtered too, and a shared-commit fixture filters everything).
	kept := archiveTagAncestry(repo, []MergeBacklogBranch{
		{Name: "feat/orphan", LastCommit: tip},
		{Name: "feat/unretired", LastCommit: unretiredTip},
	})
	for _, b := range kept {
		if b.Name == "feat/orphan" {
			t.Error("archiveTagAncestry kept the retired branch — the generator would file a merge row for it every pass")
		}
	}
	if len(kept) != 1 || kept[0].Name != "feat/unretired" {
		t.Errorf("archiveTagAncestry kept %v, want only the un-retired branch (the filter must not go blind)", kept)
	}
}

// archRefResolves reports whether ref resolves, with the exit status as the
// answer ("missing" is a legitimate reading for a ref under test).
func archRefResolves(t *testing.T, repo, ref string) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", ref).CombinedOutput()
	if err != nil && len(out) == 0 {
		return false
	}
	return err == nil
}

// TestRepoResolveRowOwnerGateOverride — the ONE way past the mechanism gate: an
// explicit, recorded permission in the repo's settings entry
// (repos.<path>.archive_bury_override). Without it the refusal above is
// unconditional; with it the archive proceeds AND the reason still lands on the
// record, so "the gate refused" and "an operator overrode it" never look alike.
func TestRepoResolveRowOwnerGateOverride(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/finished-owner")
	archSeedRow(t, "t-finished", TaskStatusCompleted, "feat/finished-owner")

	settings := filepath.Join(t.TempDir(), "settings.json")
	body := `{"repos":{"` + repo + `":{"` + config.RepoArchiveBuryOverrideKey + `":"landed by hand as feat/replacement — burying this one deliberately"}}}`
	if err := os.WriteFile(settings, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settings)

	if err := repoSweepArchiveBranch(repo, "feat/finished-owner", ArchiveReasonSuperseded); err != nil {
		t.Fatalf("a recorded override must clear the refusal, got %v", err)
	}
	if got := ArchiveTagReason(mustArchRecord(t, repo, "feat/finished-owner")); got != ArchiveReasonSuperseded {
		t.Fatalf("override recorded %q, want %q — the WHY must still land on the record", got, ArchiveReasonSuperseded)
	}
}

// TestArchiveBacklogReproducesThe2104Batch — ④ the check the incident asks
// for. It must find a reason-less tag over a completed row, must NOT report a
// reason-less tag whose owner is failed (that archive is legible from the
// row), and must go green once the reason is recorded.
func TestArchiveBacklogReproducesThe2104Batch(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/buried", "fix/legible", "feat/reasoned")
	// The 21:04 shape: lightweight tags written by hand.
	archGit(t, repo, "tag", archiveBranchPrefix+"feat/buried", "feat/buried")
	archGit(t, repo, "tag", archiveBranchPrefix+"fix/legible", "fix/legible")
	archSeedRow(t, "t-buried", TaskStatusCompleted, "feat/buried")
	archSeedRow(t, "t-legible", TaskStatusFailed, "fix/legible")
	// A properly reasoned archive over a completed row: closed, not reported.
	archSeedRow(t, "t-reasoned", TaskStatusVerified, "feat/reasoned")
	if _, err := ArchiveTagReasoned(repo, "feat/reasoned", ArchiveReasonHandMistake, "batch was a mistake"); err != nil {
		t.Fatalf("reasoned fixture: %v", err)
	}

	rows, err := ArchiveReasonlessBacklog(repo)
	if err != nil {
		t.Fatalf("ArchiveReasonlessBacklog: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("backlog = %d row(s) %+v, want exactly the one buried-over-completed tag", len(rows), rows)
	}
	if rows[0].Branch != "feat/buried" || rows[0].OwnerTaskID != "t-buried" || rows[0].OwnerStatus != TaskStatusCompleted {
		t.Fatalf("backlog row = %+v, want feat/buried owned by t-buried/completed", rows[0])
	}

	// Remediate: recording the reason clears the row, and the report empties.
	if _, err := ArchiveTagReasoned(repo, "feat/buried", ArchiveReasonHandMistake, "batch was a mistake"); err != nil {
		t.Fatalf("remediation: %v", err)
	}
	report, n, err := ArchiveBacklogReport(repo)
	if err != nil {
		t.Fatalf("ArchiveBacklogReport: %v", err)
	}
	if n != 0 {
		t.Fatalf("backlog still reports %d row(s) after remediation:\n%s", n, report)
	}
	if !strings.Contains(report, "clean") {
		t.Errorf("clean report does not say so: %s", report)
	}
}

// TestArchiveNamespaceReadingIsReasonAware pins the status surface's half: the
// `archived:` line must be able to distinguish a recorded reason from a
// reason-less tag (otherwise the 21:04 batch is again invisible in --status).
func TestArchiveNamespaceReadingIsReasonAware(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/reasoned", "feat/bare")
	archGit(t, repo, "tag", archiveBranchPrefix+"feat/bare", "feat/bare")
	if _, err := ArchiveTagReasoned(repo, "feat/reasoned", ArchiveReasonCompileFailure, ""); err != nil {
		t.Fatalf("ArchiveTagReasoned: %v", err)
	}
	// The 21:04 shape: the reason-less branch is owned by a row the ledger calls
	// FINISHED — that pair is what "buried" means, and it is what the surface
	// must shout rather than fold into a count.
	archSeedRow(t, "arch-buried-owner", TaskStatusCompleted, "feat/bare")

	got := map[string]string{}
	for _, a := range ArchiveNamespace(repo) {
		got[a.Branch] = a.Reason
	}
	if len(got) != 2 {
		t.Fatalf("namespace = %v, want both branches", got)
	}
	if got["feat/reasoned"] != ArchiveReasonCompileFailure {
		t.Errorf("feat/reasoned reason = %q, want %q", got["feat/reasoned"], ArchiveReasonCompileFailure)
	}
	if got["feat/bare"] != "" {
		t.Errorf("feat/bare reason = %q, want empty (no record was ever written)", got["feat/bare"])
	}

	// The ROW face (`gopi merge --status` renders these rows itself): names,
	// owning rows, statuses, the reason, and the buried owner.
	rows, err := ArchiveRowsForRepo(repo)
	if err != nil {
		t.Fatalf("ArchiveRowsForRepo: %v", err)
	}
	byBranch := map[string]ArchiveBacklogRow{}
	for _, r := range rows {
		byBranch[r.Branch] = r
	}
	if got := byBranch["feat/bare"]; got.OwnerTaskID != "arch-buried-owner" || got.OwnerStatus != TaskStatusCompleted || got.Reason != "" {
		t.Errorf("feat/bare row = %+v, want its owning completed row and no reason", got)
	}
	if got := byBranch["feat/reasoned"]; got.Reason != ArchiveReasonCompileFailure {
		t.Errorf("feat/reasoned row = %+v, want the recorded reason", got)
	}

	// The sweep's one-line arm renders the same records (Rule 13: one set, two
	// faces) — so the merger log line and `gopi merge --status` cannot disagree
	// about how much is archived or which entry is the buried one.
	line := RepoSweepPreview{Archived: ArchiveNamespace(repo)}.DescribeArchived()
	for _, want := range []string{"archived=2", "BURIED", "feat/bare", "arch-buried-owner"} {
		if !strings.Contains(line, want) {
			t.Errorf("DescribeArchived missing %q: %q", want, line)
		}
	}
}

// TestArchiveReasonRecordDoesNotOutliveItsTag — the companion namespace must not
// keep a record for a branch whose archive tag is gone. Removing the tag is the
// namespace's documented way to UN-archive a branch (it re-arms the branch for a
// future merge), and a surviving record would then make the next archive of that
// branch a no-op: the idempotence probe would find a reason for a decision that
// was reversed, and refuse to record the new one. Reported, never auto-deleted.
func TestArchiveReasonRecordDoesNotOutliveItsTag(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/gone", "feat/kept")
	for _, b := range []string{"feat/gone", "feat/kept"} {
		if _, err := ArchiveTagReasoned(repo, b, ArchiveReasonSuperseded, ""); err != nil {
			t.Fatalf("ArchiveTagReasoned(%s): %v", b, err)
		}
	}
	if orphans, err := ArchiveNamespaceOrphans(repo); err != nil || len(orphans) != 0 {
		t.Fatalf("orphans = %v (err=%v), want none", orphans, err)
	}

	// Un-archive one branch the documented way: drop its tag.
	archGit(t, repo, "tag", "-d", archiveBranchPrefix+"feat/gone")

	orphans, err := ArchiveNamespaceOrphans(repo)
	if err != nil {
		t.Fatalf("ArchiveNamespaceOrphans: %v", err)
	}
	if len(orphans) != 1 || orphans[0] != "feat/gone" {
		t.Fatalf("orphans = %v, want exactly [feat/gone]", orphans)
	}

	// And the consequence the check exists to surface: while the stale record
	// stands, re-archiving that branch keeps the OLD reason (the record is not
	// rewritten). The TAG, however, is restored — the tag is ensured before the
	// record is consulted, because the park lane's contract is that parking
	// re-protects the branch. Getting this order wrong left the branch in the
	// merge queue (TestRepoSweepParkBranchRowIsIdempotent caught it).
	res, err := ArchiveBranch(repo, "feat/gone", ArchiveReasonCompileFailure, "re-archived", true)
	if err != nil {
		t.Fatalf("ArchiveBranch after un-archive: %v", err)
	}
	if !res.Recorded {
		t.Fatal("re-archive did not record")
	}
	if got := ArchiveTagReason(mustArchRecord(t, repo, "feat/gone")); got != ArchiveReasonSuperseded {
		t.Fatalf("re-archive recorded %q — the stale record IS the reason the orphan is reported", got)
	}
	tags, err := archiveNamespaceEntries(repo)
	if err != nil {
		t.Fatalf("archiveNamespaceEntries: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("archive namespace = %d ref(s), want 2 — the re-archive must restore the dropped arch tag", len(tags))
	}
}

// archCommitTypedReasonRef plants the WRONG-TYPED companion ref: a ref inside
// refs/gopi/archive-reason/ that points at a COMMIT instead of a blob — the
// exact shape measured in the real repo (task/01a143b4…, 2026-10-12), where it
// made every archive face die at once.
func archCommitTypedReasonRef(t *testing.T, repo, branch string) {
	t.Helper()
	sha := strings.TrimSpace(archGit(t, repo, "rev-parse", "HEAD"))
	archGit(t, repo, "update-ref", archiveReasonRefPrefix+branch, sha)
}

// archWrongTypeDependsOnType is the RED HALF of the regression, written the way
// the OLD scan read the namespace: name only, then `cat-file blob` per ref and
// return on the first failure. It is deliberately a test-local reproduction of
// the pre-fix shape rather than a call to archived code — the production reader
// no longer has that failure mode, and a fixture that cannot fail proves
// nothing (Rule 10's minimal-sample habit: show the failure, then show it gone).
func archWrongTypeDependsOnType(t *testing.T, repo string) error {
	t.Helper()
	out, err := repoSweepGit(repo, "for-each-ref", "--format="+archiveRefNameFormat, archiveReasonRefPrefix)
	if err != nil {
		return err
	}
	for _, ref := range strings.Split(out, "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, err := repoSweepGit(repo, "cat-file", "blob", ref); err != nil {
			return err
		}
	}
	return nil
}

// TestArchiveReasonScanClassifiesByObjectTypeRatherThanAborting is the minimal
// repro of the 2026-10-12 outage: ONE commit-typed ref in the companion
// namespace used to make the WHOLE namespace unreadable, so `git cat-file blob
// <ref>`'s exit 128 disabled the archive's ref removal repo-wide (5 of 11
// landed branches unreclaimed in one merge run).
//
// The contract now: the readable records still come back, and the bad entry is
// REPORTED BY NAME (Rule 15 — a record that cannot be read must not look like a
// record never written). The classification is on the OBJECT TYPE; the ref NAME
// stays the one key three readers agree on.
func TestArchiveReasonScanClassifiesByObjectTypeRatherThanAborting(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/good", "feat/broken")
	if _, err := ArchiveTagReasoned(repo, "feat/good", ArchiveReasonSuperseded, ""); err != nil {
		t.Fatalf("ArchiveTagReasoned(feat/good): %v", err)
	}
	archCommitTypedReasonRef(t, repo, "feat/broken")
	// The listing is driven by the ARCHIVE TAG namespace, so the broken branch
	// needs a tag of its own to appear — the shape in the real repo, where the
	// commit-typed companion ref sits beside a genuine archive decision.
	archGit(t, repo, "tag", archiveBranchPrefix+"feat/broken", "feat/broken")

	// RED: the pre-fix reader aborts on the very same namespace.
	if err := archWrongTypeDependsOnType(t, repo); err == nil {
		t.Fatalf("pre-fix shape did NOT fail on a commit-typed reason ref — the fixture cannot prove the regression")
	}

	// GREEN: the read-only namespace face still lists BOTH branches; the broken
	// one reads as no-reason (its record is unreadable, not missing), and the
	// readable one keeps its recorded reason.
	got := map[string]string{}
	for _, a := range ArchiveNamespace(repo) {
		got[a.Branch] = a.Reason
	}
	if len(got) != 2 {
		t.Fatalf("namespace = %v, want both branches (one bad ref must not hide the rest)", got)
	}
	if got["feat/good"] != ArchiveReasonSuperseded {
		t.Errorf("feat/good reason = %q, want %q", got["feat/good"], ArchiveReasonSuperseded)
	}
	if got["feat/broken"] != "" {
		t.Errorf("feat/broken reason = %q, want empty (its record is unreadable, not recorded)", got["feat/broken"])
	}

	// The scan returns the readable records AND names the bad one.
	records, bad, err := archiveReasonRecords(repo)
	if err != nil {
		t.Fatalf("archiveReasonRecords: %v", err)
	}
	if _, ok := records["feat/good"]; !ok {
		t.Fatalf("records = %v, want feat/good's record", records)
	}
	if _, ok := records["feat/broken"]; ok {
		t.Fatalf("records = %v, want NO entry for the commit-typed ref", records)
	}
	if len(bad) != 1 {
		t.Fatalf("malformed = %+v, want exactly one entry", bad)
	}
	wantRef := archiveReasonRefPrefix + "feat/broken"
	if bad[0].Ref != wantRef || bad[0].Branch != "feat/broken" || bad[0].ObjectType != gitObjectTypeCommit {
		t.Fatalf("malformed entry = %+v, want %s as a %s", bad[0], wantRef, gitObjectTypeCommit)
	}

	// The AGGREGATE report names every bad entry (Rule 15: all at once), and is
	// empty for a clean namespace.
	text := ArchiveReasonMalformedReport(bad)
	if !strings.Contains(text, wantRef) || !strings.Contains(text, gitObjectTypeCommit) {
		t.Errorf("malformed report does not name the ref and its type:\n%s", text)
	}
	if clean := ArchiveReasonMalformedReport(nil); clean != "" {
		t.Errorf("clean namespace rendered %q, want silence", clean)
	}

	// The same finding, through the surface that owns the exit status.
	named, err := ArchiveNamespaceMalformed(repo)
	if err != nil {
		t.Fatalf("ArchiveNamespaceMalformed: %v", err)
	}
	if len(named) != 1 || named[0].Ref != wantRef {
		t.Fatalf("ArchiveNamespaceMalformed = %+v, want exactly %s", named, wantRef)
	}
}

// TestArchiveReasonScanReportsEveryMalformedEntryAtOnce — the aggregate half of
// Rule 15: an operator who fixes one bad ref and re-runs must not then discover
// the second. Two wrong-typed entries produce TWO named lines in one report.
func TestArchiveReasonScanReportsEveryMalformedEntryAtOnce(t *testing.T) {
	archLedgerFile(t)
	repo := archTestRepo(t, "feat/one", "feat/two")
	for _, b := range []string{"feat/one", "feat/two"} {
		archCommitTypedReasonRef(t, repo, b)
		archGit(t, repo, "tag", archiveBranchPrefix+b, b)
	}

	bad, err := ArchiveNamespaceMalformed(repo)
	if err != nil {
		t.Fatalf("ArchiveNamespaceMalformed: %v", err)
	}
	if len(bad) != 2 {
		t.Fatalf("malformed = %+v, want both entries", bad)
	}
	text := ArchiveReasonMalformedReport(bad)
	for _, ref := range []string{archiveReasonRefPrefix + "feat/one", archiveReasonRefPrefix + "feat/two"} {
		if !strings.Contains(text, ref) {
			t.Errorf("aggregate report missing %s:\n%s", ref, text)
		}
	}
}
