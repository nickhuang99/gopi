package tools

// worktree_sweep_chat_test.go — the acceptance and refusal halves of the CHAT
// axis of the worktree sweep (epic chat-ground-reclaim, 2026-09-29).
//
// WHAT IS BEING PINNED, in one sentence: an INERT chat ground — a ground whose
// session is provably over, whose tree is clean and whose branch carries
// nothing the integration branch lacks — is reclaimed WITH its branch, by an
// arm that names itself; and every one of those four conditions failing keeps
// it, in the words of the condition that failed.
//
// Every test below drives the REAL runner (worktreeSweepRunWith) against a REAL
// git repository and a REAL ledger row, because the pitch of this arm is
// exactly that it uses the machinery that already exists: the registration
// listing, the holder scan, the chats index and git's own `branch -d`.
//
// The fixtures are the diagnosis document's own shapes
// (devdoc/chat-session-ground-lifecycle-diagnosis.md §1): six grounds on this
// host are clean, 0 ahead of the integration branch and owned by no row — the
// RESIDUE this arm exists to collect.

import (
	"context"
	"github.com/nickhuang99/gopi/internal/scratch"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// The fixtures' session ids: 32 hex characters, the exact shape the retired
// producer's stem left behind (a UUID with its dashes stripped), so the
// reader's two-spelling lookup is exercised rather than assumed.
const (
	chatSweepFixtureStemA = "3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c"
	chatSweepFixtureStemB = "9e8d7c6b5a4938271605f4e3d2c1b0a9"
	chatSweepFixtureStemC = "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	chatSweepFixtureStemD = "0f1e2d3c4b5a69788796a5b4c3d2e1f0"
	chatSweepFixtureStemE = "112233445566778899aabbccddeeff00"
	chatSweepFixtureStemF = "ffeeddccbbaa99887766554433221100"
)

// chatSweepFixture is one chat ground built the way the producer builds it: a
// linked worktree under a worktree root, on branch `chat/<stem>`, with a
// session directory under the sessions root (cmd/agent/chat_ground.go +
// chat_register.go).
type chatSweepFixture struct {
	repo   string
	root   string
	ground string
	branch string
	dbPath string
}

// chatSweepEnv isolates the profile AND the ledger: a temp GOPI_HOME (the
// sessions root the freshness probe reads), a temp worktree root, and a
// per-test ledger through the SQLITE SCAFFOLD (Rule 32 — the test never
// hand-rolls a database path, and the operator's profile is never touched).
//
// The index itself is MATERIALISED through the production opener, because an
// EMPTY index is a different fact from an ABSENT one: the reader answers "the
// index knows no session with this id" for the first (a reclaimable ground) and
// refuses to answer at all for the second (a keep). A fixture that left the
// file uncreated would be testing the second shape while claiming to test the
// first.
func chatSweepEnv(t *testing.T) (root, dbPath string) {
	t.Helper()
	root = sweepFixtureRoot(t)
	dbPath = testutil.NewTestDBPath(t)
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		t.Fatalf("materialise the chat index at %s: %v", dbPath, err)
	}
	// The handle's ONLY job here is to materialise the index (schema + path);
	// the fixture is then built by the production recorders, each of which
	// opens its own handle. So the close is checked and reported: a fixture
	// ledger that could not be closed is a broken fixture, and silently
	// discarding it would leave the next failure to surface somewhere else
	// (Rule 15).
	if err := db.Close(); err != nil {
		t.Fatalf("close the chat index at %s: %v", dbPath, err)
	}
	return root, dbPath
}

// chatSweepFixtureRepo builds a repo whose `master` is the integration branch
// (IntegrationBranchName) and returns its path.
func chatSweepFixtureRepo(t *testing.T) string {
	t.Helper()
	return sweepInitRepo(t)
}

// chatSweepGround creates the ground for one stem: `git worktree add
// <root>/gopi-<stem> -b chat/<stem>`, aged past the ordinary age floor
// (the floor the chat arm REPLACES with a proof of endedness — so a fixture
// that reaps under it is evidence the proof, not the clock, did the work).
func chatSweepGround(t *testing.T, repo, root, stem string) chatSweepFixture {
	t.Helper()
	ground := filepath.Join(root, TaskGroundDirPrefix+stem)
	sweepGit(t, repo, finishCleanupWorktreeVerb, "add", ground, "-b", ChatGroundBranchPrefix+stem)
	// The registration's creation record, aged 72h: the ordinary ladder would
	// still reclaim THIS fixture on age alone, which is why the tests that must
	// prove "the chat arm decided" also assert the RULE the finding carries.
	ageRegistrationRecord(t, worktreeFullGitdir(t, ground), time.Now().Add(-72*time.Hour))
	return chatSweepFixture{
		repo:   repo,
		root:   root,
		ground: ground,
		branch: ChatGroundBranchPrefix + stem,
	}
}

// chatSweepRoots is the injected root list: the fixture's worktree root, in the
// worktree role — the same evidence production resolves from the settings table
// (worktreeSweepRoots), injected so the fixture asks a question the policy can
// actually see (worktree_sweep_injected_roots_redproof_test.go).
func chatSweepRoots(root string) []worktreeroots.Root {
	return []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
}

// chatSweepRun executes one real sweep over the fixture with the ledger read.
func chatSweepRun(t *testing.T, f chatSweepFixture, dryRun bool) worktreeSweepResult {
	t.Helper()
	opts := sweepOptionsNow(dryRun)
	opts.MinAge = time.Hour
	return worktreeSweepRunWith(context.Background(), opts, worktreeSweepDeps{
		Repos:      []string{f.repo},
		Roots:      chatSweepRoots(f.root),
		LedgerRead: true,
		Holders:    scratch.HolderScan{},
	}, nil)
}

// chatSweepFindingFor returns the finding whose path is the fixture's ground.
func chatSweepFindingFor(res worktreeSweepResult, ground string) (worktreeSweepFinding, bool) {
	for _, f := range res.Findings {
		if f.Path == ground {
			return f, true
		}
	}
	return worktreeSweepFinding{}, false
}

// chatSweepSeedRow writes the session's index row through the PRODUCTION
// recorder (ChatEnsureRow), then dates it: endedAt > 0 closes it, and 0 leaves
// it open (the crash window's shape). The stamps are written through the
// production opener because the recorder always stamps "now" — a fixture that
// needs "closed an hour ago" must say so, and it says it in the same db the
// reader reads.
func chatSweepSeedRow(t *testing.T, dbPath, stem string, endedAt time.Time) {
	t.Helper()
	if _, err := ChatEnsureRow(ChatSession{SessionID: stem, Mode: ChatModeCLI, Model: "fixture"}); err != nil {
		t.Fatalf("seed the chat row for %s: %v", stem, err)
	}
	if endedAt.IsZero() {
		return
	}
	ctx := context.Background()
	db, err := openChatDB(ctx)
	if err != nil {
		t.Fatalf("open the chat index: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `UPDATE chats SET ended_at = ? WHERE session_id = ?`,
		float64(endedAt.Unix()), stem); err != nil {
		t.Fatalf("date the chat row for %s: %v", stem, err)
	}
}

// chatSweepBranchExists reports whether a ref is still there (the branch axis's
// own assertion — the reclaim is only complete when the REF is gone too).
func chatSweepBranchExists(t *testing.T, repo, branch string) bool {
	t.Helper()
	out := sweepGit(t, repo, worktreeSweepBranchVerb, "--list", branch)
	return strings.TrimSpace(out) != ""
}

// ---------------------------------------------------------------------------
// THE ACCEPTANCE DIRECTION
// ---------------------------------------------------------------------------

// TestSweepReclaimsAnInertChatGroundAndItsBranch is the epic's core claim: a
// chat ground whose session is CLOSED, whose tree is clean and whose branch is
// spent is reclaimed, its branch freed, and the finding names this arm rather
// than the ordinary ladder.
func TestSweepReclaimsAnInertChatGroundAndItsBranch(t *testing.T) {
	root, dbPath := chatSweepEnv(t)
	repo := chatSweepFixtureRepo(t)
	f := chatSweepGround(t, repo, root, chatSweepFixtureStemA)
	chatSweepSeedRow(t, dbPath, chatSweepFixtureStemA, time.Now().Add(-2*time.Hour))

	res := chatSweepRun(t, f, false)

	if res.Reclaimed != 1 {
		t.Fatalf("reclaimed=%d (%s), want 1 — an inert chat ground must be collectable", res.Reclaimed, sweepFindingsText(res))
	}
	if _, err := os.Stat(f.ground); !os.IsNotExist(err) {
		t.Fatalf("the inert ground survived the sweep: %v", err)
	}
	if chatSweepBranchExists(t, repo, f.branch) {
		t.Fatalf("the ground's branch %s survived the reclaim — the ref was the OTHER half of this leak", f.branch)
	}
	if res.BranchesReaped != 1 {
		t.Fatalf("branches_reaped=%d, want 1", res.BranchesReaped)
	}
	fd, ok := chatSweepFindingFor(res, f.ground)
	if !ok {
		t.Fatalf("no finding named the ground:\n%s", sweepFindingsText(res))
	}
	if fd.Rule != string(worktreeRuleChatInert) {
		t.Fatalf("rule = %q, want %q — the decision must name the chat axis, not the ordinary ladder (%s)",
			fd.Rule, worktreeRuleChatInert, fd.Reason)
	}
	if !strings.Contains(fd.Reason, worktreeSweepChatClosedMarker) {
		t.Fatalf("the reason does not quote the close stamp that decided it: %s", fd.Reason)
	}
	if !strings.Contains(fd.Reason, "git agreed it was safe") {
		t.Fatalf("the reason does not record the branch deletion: %s", fd.Reason)
	}
}

// TestSweepChatGroundBranchDeleteIsRecordedUnderItsOwnArm pins the audit trail:
// the ref's deletion is recorded, and the record names the CHAT arm — a reader
// grepping the log must be able to tell this decision from the dead-registration
// arm's (removalaudit's own design).
func TestSweepChatGroundBranchDeleteIsRecordedUnderItsOwnArm(t *testing.T) {
	root, dbPath := chatSweepEnv(t)
	repo := chatSweepFixtureRepo(t)
	sink := auditSink(t)
	f := chatSweepGround(t, repo, root, chatSweepFixtureStemB)
	chatSweepSeedRow(t, dbPath, chatSweepFixtureStemB, time.Now().Add(-time.Hour))

	// The audit pin: the record's own assertions are auditProbe's (the named
	// assertion this suite already uses), so the RED/GREEN shape is inherited
	// rather than re-invented.
	res := chatSweepRun(t, f, false)
	if res.Reclaimed != 1 || res.BranchesReaped != 1 {
		t.Fatalf("reclaimed=%d branches=%d (%s), want 1/1", res.Reclaimed, res.BranchesReaped, sweepFindingsText(res))
	}
	if why := auditProbe(t, sink, removalaudit.ArmSweepChatGroundBranch, f.branch); why != "" {
		t.Fatalf("GREEN half: %s", why)
	}
}

// TestSweepReclaimsAnInertChatGroundWithNoRowAtAll covers the shape the
// diagnosis document actually measured: a ground whose run left NO index row
// and NO session directory (a chat killed before its first turn). Nothing the
// session owns exists any more, so nothing can be coming back.
func TestSweepReclaimsAnInertChatGroundWithNoRowAtAll(t *testing.T) {
	root, _ := chatSweepEnv(t)
	repo := chatSweepFixtureRepo(t)
	f := chatSweepGround(t, repo, root, chatSweepFixtureStemC)

	res := chatSweepRun(t, f, false)

	if res.Reclaimed != 1 {
		t.Fatalf("reclaimed=%d (%s), want 1 — a ground whose session left no trace is the residue this arm exists for",
			res.Reclaimed, sweepFindingsText(res))
	}
	fd, _ := chatSweepFindingFor(res, f.ground)
	if fd.Rule != string(worktreeRuleChatInert) || !strings.Contains(fd.Reason, worktreeSweepChatNoRowMarker) {
		t.Fatalf("rule=%q reason=%q, want the chat arm quoting the missing row", fd.Rule, fd.Reason)
	}
	if chatSweepBranchExists(t, repo, f.branch) {
		t.Fatalf("branch %s survived", f.branch)
	}
}

// TestSweepDryRunReportsAnInertChatGroundAndTouchesNothing is the posture guard
// for the new arm specifically: the plan names the reap, the run performs none
// — and neither the ref nor the directory is touched.
func TestSweepDryRunReportsAnInertChatGroundAndTouchesNothing(t *testing.T) {
	root, dbPath := chatSweepEnv(t)
	repo := chatSweepFixtureRepo(t)
	f := chatSweepGround(t, repo, root, chatSweepFixtureStemD)
	chatSweepSeedRow(t, dbPath, chatSweepFixtureStemD, time.Now().Add(-time.Hour))

	res := chatSweepRun(t, f, true)

	if res.WouldReclai != 1 || res.Reaped != 0 || res.BranchesReaped != 0 {
		t.Fatalf("dry run: would_reclaim=%d reaped=%d branches=%d (%s), want 1/0/0",
			res.WouldReclai, res.Reaped, res.BranchesReaped, sweepFindingsText(res))
	}
	if _, err := os.Stat(f.ground); err != nil {
		t.Fatalf("the dry run removed the ground: %v", err)
	}
	if !chatSweepBranchExists(t, repo, f.branch) {
		t.Fatalf("the dry run deleted the branch %s", f.branch)
	}
}

// ---------------------------------------------------------------------------
// THE REFUSAL DIRECTION — one test per condition of inertness
// ---------------------------------------------------------------------------

// TestSweepKeepsAChatGroundHeldByALiveProcess: a chat carrier stands in its own
// ground for the whole session, so a live process there IS the session. The
// GENERIC holder rung (②) is what fires — a live process is the strongest
// evidence a registration is in use, and it is not a chat-specific fact — and
// the chat predicate asks the same question again as its own defence in depth
// (a caller that reaches it without the ladder, e.g. the branch axis, must not
// be able to free a held ground).
func TestSweepKeepsAChatGroundHeldByALiveProcess(t *testing.T) {
	root := sweepFixtureRoot(t)
	reg := worktreeRegistration{
		Repo:   "/fixture/repo",
		Path:   filepath.Join(root, TaskGroundDirPrefix+chatSweepFixtureStemE),
		Branch: ChatGroundBranchPrefix + chatSweepFixtureStemE,
	}
	holders := scratch.HolderScan{Paths: []string{filepath.Join(reg.Path, "sub")}}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	f := worktreeSweepJudge(reg, nil, holders, chatSweepRoots(root), opts, ho)
	if f.Disposition != string(worktreeKeep) {
		t.Fatalf("disposition = %s (%s), want keep", f.Disposition, f.Reason)
	}
	if f.Rule != string(worktreeRuleLiveHolder) {
		t.Fatalf("rule = %q, want %q", f.Rule, worktreeRuleLiveHolder)
	}
	if !strings.Contains(f.Reason, "live process") {
		t.Fatalf("the refusal does not name the holder: %s", f.Reason)
	}

	// The chat predicate's OWN holder check, asked directly: the branch axis
	// (worktreeSweepReapChatBranch) and any future caller reach the predicate
	// without the ladder, and a held ground must never be inert for them either.
	inert, evidence := worktreeSweepChatInert(reg, holders, opts, ho.CreatedAt)
	if inert {
		t.Fatalf("the chat predicate called a HELD ground inert")
	}
	if !strings.Contains(evidence, worktreeSweepChatLiveHolderMarker) {
		t.Fatalf("the predicate's refusal does not name the live holder: %s", evidence)
	}
}

// TestSweepKeepsAChatGroundWhenTheScanWasTruncated: the SAME held-ness
// question, asked with a scan that stopped before the end of the process table.
// The ground must still be kept — an unfinished scan is not evidence of absence
// — but the SENTENCE must not claim the session's liveness: "a live process
// holds the ground" is a chat-specific fact that was never observed, and an
// operator reading it would go looking for a carrier that does not exist.
func TestSweepKeepsAChatGroundWhenTheScanWasTruncated(t *testing.T) {
	root := sweepFixtureRoot(t)
	reg := worktreeRegistration{
		Repo:   "/fixture/repo",
		Path:   filepath.Join(root, TaskGroundDirPrefix+chatSweepFixtureStemE),
		Branch: ChatGroundBranchPrefix + chatSweepFixtureStemE,
	}
	holders := scratch.HolderScan{Truncated: true}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	inert, evidence := worktreeSweepChatInert(reg, holders, opts, ho.CreatedAt)
	if inert {
		t.Fatalf("the chat predicate called a ground inert under a TRUNCATED scan")
	}
	if !strings.Contains(evidence, worktreeSweepChatScanTruncatedMarker) {
		t.Fatalf("the refusal does not disclose the truncation: %s", evidence)
	}
	if strings.Contains(evidence, worktreeSweepChatLiveHolderMarker) {
		t.Fatalf("the refusal dresses an unfinished scan as the session's liveness: %s", evidence)
	}

	// And through the ladder: the plan's rung renders the same distinction.
	f := worktreeSweepJudge(reg, nil, holders, chatSweepRoots(root), opts, ho)
	if f.Disposition != string(worktreeKeep) {
		t.Fatalf("disposition = %s (%s), want keep", f.Disposition, f.Reason)
	}
	if strings.Contains(f.Reason, worktreeSweepChatLiveHolderMarker) {
		t.Fatalf("the ladder's refusal claims a live holder nothing observed: %s", f.Reason)
	}
	// The ladder reaches it through its GENERIC holder rung, which renders the
	// scan's own disclosure (scratch.HolderScanWithheldReason) rather than the
	// chat-specific phrasing — both are honest, and what this test pins is the
	// property the two share: the sentence says the scan did not FINISH.
	if !strings.Contains(f.Reason, "TRUNCATED") {
		t.Fatalf("the ladder's refusal does not disclose the truncation: %s", f.Reason)
	}
}

// TestSweepKeepsAChatGroundWhoseSessionIsOpen: a row that exists and was never
// closed, written to recently — the conversation is being had. The fixture ages
// the REGISTRATION past the floor precisely so the keep cannot be explained by
// the clock.
func TestSweepKeepsAChatGroundWhoseSessionIsOpen(t *testing.T) {
	root, dbPath := chatSweepEnv(t)
	repo := chatSweepFixtureRepo(t)
	f := chatSweepGround(t, repo, root, chatSweepFixtureStemF)
	// An open row: ChatEnsureRow stamps started_at = now and leaves ended_at NULL.
	chatSweepSeedRow(t, dbPath, chatSweepFixtureStemF, time.Time{})
	// And the session's own directory, written just now — the filesystem's half
	// of "this is alive" (a chat writes its turns there).
	chatSweepTouchSessionDir(t, chatSweepFixtureStemF)

	res := chatSweepRun(t, f, false)

	if res.Reclaimed != 0 {
		t.Fatalf("reclaimed=%d (%s), want 0 — an open conversation's ground is not residue", res.Reclaimed, sweepFindingsText(res))
	}
	fd, ok := chatSweepFindingFor(res, f.ground)
	if !ok {
		t.Fatalf("no finding named the ground:\n%s", sweepFindingsText(res))
	}
	if fd.Rule != string(worktreeRuleChatSessionActive) {
		t.Fatalf("rule = %q, want %q (%s)", fd.Rule, worktreeRuleChatSessionActive, fd.Reason)
	}
	if !strings.Contains(fd.Reason, worktreeSweepChatOpenMarker) {
		t.Fatalf("the refusal does not say the session is still going: %s", fd.Reason)
	}
	if !chatSweepBranchExists(t, repo, f.branch) {
		t.Fatalf("the branch was deleted under a live session")
	}
}

// TestSweepKeepsAChatGroundCarryingUnpushedCommits: the session is over, but its
// branch holds commits the integration branch does not have. That ref is their
// ONLY home (no merge path reads `chat/<id>`), so the ground is kept and the
// finding tells the operator what to do about it.
func TestSweepKeepsAChatGroundCarryingUnpushedCommits(t *testing.T) {
	root, dbPath := chatSweepEnv(t)
	repo := chatSweepFixtureRepo(t)
	f := chatSweepGround(t, repo, root, chatSweepFixtureStemA)
	chatSweepSeedRow(t, dbPath, chatSweepFixtureStemA, time.Now().Add(-2*time.Hour))
	// Work the branch alone holds.
	if err := os.WriteFile(filepath.Join(f.ground, "deliverable.txt"), []byte("the work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sweepGit(t, f.ground, "add", "deliverable.txt")
	sweepGit(t, f.ground, "commit", "-q", "-m", "a chat session's committed work")

	res := chatSweepRun(t, f, false)

	if res.Reclaimed != 0 {
		t.Fatalf("reclaimed=%d (%s), want 0 — the commits' only home is this ref", res.Reclaimed, sweepFindingsText(res))
	}
	fd, ok := chatSweepFindingFor(res, f.ground)
	if !ok {
		t.Fatalf("no finding named the ground:\n%s", sweepFindingsText(res))
	}
	if fd.Rule != string(worktreeRuleChatCarriesWork) {
		t.Fatalf("rule = %q, want %q (%s)", fd.Rule, worktreeRuleChatCarriesWork, fd.Reason)
	}
	if !strings.Contains(fd.Reason, worktreeSweepChatCarriesWorkReason) {
		t.Fatalf("the refusal does not say what to do about the commits: %s", fd.Reason)
	}
	// The reason is the JUDGE's, with the lineage fact appended — not a second
	// copy of it. The first draft re-prefixed it, and the real verb printed a
	// doubly-nested sentence ("chat ground X is inert (chat ground X is inert —
	// …)"). A suite could not see that (Rule 31: only the invocation could), so
	// the shape is pinned here as the regression guard.
	if strings.Count(fd.Reason, "is inert") != 1 {
		t.Fatalf("the reason states the inertness more than once (the runner re-prefixed the judge's own sentence): %s", fd.Reason)
	}
	if _, err := os.Stat(f.ground); err != nil {
		t.Fatalf("the ground holding committed work was removed: %v", err)
	}
	if !chatSweepBranchExists(t, repo, f.branch) {
		t.Fatalf("the branch holding the commits was deleted — that would have destroyed the only home of the work")
	}
}

// TestSweepKeepsADirtyChatGround is the operator's own criterion, pinned: an
// uncommitted change in a chat ground is that SESSION'S DELIVERABLE (the ground
// exists so a chat's writes have somewhere to land), so the arm must keep the
// tree and report it — and the report must name the session, because there is
// no ledger row an operator could look up instead.
func TestSweepKeepsADirtyChatGround(t *testing.T) {
	root, dbPath := chatSweepEnv(t)
	repo := chatSweepFixtureRepo(t)
	f := chatSweepGround(t, repo, root, chatSweepFixtureStemB)
	chatSweepSeedRow(t, dbPath, chatSweepFixtureStemB, time.Now().Add(-2*time.Hour))
	// Uncommitted work in the ground.
	if err := os.WriteFile(filepath.Join(f.ground, "notes.md"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := chatSweepRun(t, f, false)

	if res.Reclaimed != 0 {
		t.Fatalf("reclaimed=%d (%s), want 0 — uncommitted work is the deliverable", res.Reclaimed, sweepFindingsText(res))
	}
	fd, ok := chatSweepFindingFor(res, f.ground)
	if !ok {
		t.Fatalf("no finding named the ground:\n%s", sweepFindingsText(res))
	}
	// The arm is the ORPHAN ESCALATION, under its OWN rule (worktreeRuleOrphan —
	// a plan that files an escalation as a "dirty-worktree" report cannot be told
	// from the report of a dirty tree whose owner is still on the host), not the
	// plain dirty report: a dirty ground owned by NO row is the shape ③O hands to
	// a human, and a chat ground is by construction owned by no row. What matters here is that the
	// tree is KEPT and the report names the session whose deliverable it is.
	if fd.Rule != string(worktreeRuleOrphan) || fd.Disposition != string(worktreeEscalateOrphan) {
		t.Fatalf("disposition=%s rule=%q (%s), want %s/%q", fd.Disposition, fd.Rule, fd.Reason, worktreeEscalateOrphan, worktreeRuleOrphan)
	}
	if !strings.Contains(fd.Reason, worktreeSweepChatOrphanClause+chatSweepFixtureStemB+")") {
		t.Fatalf("the dirty report does not name the chat session whose deliverable it is: %s", fd.Reason)
	}
	if _, err := os.Stat(filepath.Join(f.ground, "notes.md")); err != nil {
		t.Fatalf("the uncommitted file was destroyed: %v", err)
	}
	if !chatSweepBranchExists(t, repo, f.branch) {
		t.Fatalf("the branch was deleted under a dirty ground")
	}
}

// TestSweepDirtyChatGroundsYoungRegistrationStillNamesTheSession is the
// regression guard for a defect the REAL verb caught while this arm was being
// built (Rule 31: a build and a suite are not an invocation). TWO rungs report
// dirt — ③O's fall-through (dirty, unowned, not yet past the age floor) and ④
// (the plain report) — and ③O RETURNS before ④ is reached. The first draft
// appended the chat clause only in ④, so a dirty chat ground whose registration
// was minutes old took the fall-through and lost the sentence, which is exactly
// the operator the clause exists for: a chat's dirt has no ledger row to look
// up. Both rungs now render through worktreeSweepDirtyReportReason, and this
// test drives the FALL-THROUGH (a registration inside the age floor), not the
// escalation above it.
func TestSweepDirtyChatGroundsYoungRegistrationStillNamesTheSession(t *testing.T) {
	root := sweepFixtureRoot(t)
	reg := worktreeRegistration{
		Repo:   "/fixture/repo",
		Path:   filepath.Join(root, TaskGroundDirPrefix+chatSweepFixtureStemA),
		Branch: ChatGroundBranchPrefix + chatSweepFixtureStemA,
	}
	opts := sweepOptionsNow(true)
	// Minutes old: inside the age floor, so ③O falls through to the plain report
	// instead of escalating.
	ho := worktreeSweepHostObservations{
		CreatedAt:   opts.Now.Add(-5 * time.Minute),
		DirtyReason: "?? take-this-with-you.txt",
	}

	f := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, chatSweepRoots(root), opts, ho)
	if f.Disposition != string(worktreeReport) || f.Rule != string(worktreeRuleDirty) {
		t.Fatalf("disposition=%s rule=%q (%s), want report/%q", f.Disposition, f.Rule, f.Reason, worktreeRuleDirty)
	}
	if !strings.Contains(f.Reason, worktreeSweepChatDirtyReasonSuffix+chatSweepFixtureStemA+")") {
		t.Fatalf("the fall-through dirty report lost the chat clause — the operator cannot tell whose deliverable this is: %s", f.Reason)
	}
	// And the escalation path renders the SAME fact in the orphan reason.
	ho.CreatedAt = opts.Now.Add(-72 * time.Hour)
	f = worktreeSweepJudge(reg, nil, scratch.HolderScan{}, chatSweepRoots(root), opts, ho)
	if f.Disposition != string(worktreeEscalateOrphan) {
		t.Fatalf("disposition = %s, want the orphan escalation", f.Disposition)
	}
	if !strings.Contains(f.Reason, worktreeSweepChatOrphanClause+chatSweepFixtureStemA+")") {
		t.Fatalf("the escalation does not name the session: %s", f.Reason)
	}
}

// TestSweepKeepsAChatGroundWhenTheIndexCannotBeRead: fail closed. An unreadable
// index answers no question at all, and "I could not ask" is not endedness — so
// the ground is KEPT, and the finding says exactly that rather than pretending
// the age floor decided.
func TestSweepKeepsAChatGroundWhenTheIndexCannotBeRead(t *testing.T) {
	root := sweepFixtureRoot(t)
	// A chat index override pointing at a file that does not exist: the reader
	// refuses to open it, which is the drift THIS test exists to catch.
	missing := filepath.Join(t.TempDir(), "no-such-ledger.db")
	t.Setenv(chatDBEnv, missing)

	reg := worktreeRegistration{
		Repo:   "/fixture/repo",
		Path:   filepath.Join(root, TaskGroundDirPrefix+chatSweepFixtureStemC),
		Branch: ChatGroundBranchPrefix + chatSweepFixtureStemC,
	}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	f := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, chatSweepRoots(root), opts, ho)
	if f.Disposition != string(worktreeKeep) {
		t.Fatalf("disposition = %s (%s), want keep", f.Disposition, f.Reason)
	}
	if f.Rule != string(worktreeRuleChatSessionActive) || !strings.Contains(f.Reason, worktreeSweepChatUnreadableMarker) {
		t.Fatalf("rule=%q reason=%q, want the unreadable-index refusal", f.Rule, f.Reason)
	}
}

// TestTheChatArmDoesNotClaimATaskGround is the blast-radius guard: the chat
// arm's classifier is the BRANCH namespace (`chat/`), because a ground's
// DIRECTORY (`gopi-<stem>`) is the shape a TASK's ground carries too. A task
// ground with no owning row must therefore reach the ORDINARY rungs — here the
// plain unclaimed reclaim — and never the chat arm.
func TestTheChatArmDoesNotClaimATaskGround(t *testing.T) {
	root := sweepFixtureRoot(t)
	stem := chatSweepFixtureStemD
	reg := worktreeRegistration{
		Repo:   "/fixture/repo",
		Path:   filepath.Join(root, TaskGroundDirPrefix+stem),
		Branch: TaskGroundBranchPrefix + stem,
	}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	f := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, chatSweepRoots(root), opts, ho)
	if f.Disposition != string(worktreeReclaim) {
		t.Fatalf("disposition = %s (%s), want the ordinary reclaim", f.Disposition, f.Reason)
	}
	if f.Rule != string(worktreeRuleUnclaimed) {
		t.Fatalf("rule = %q, want %q — the chat axis must not classify a TASK's ground (its directory is the same shape)",
			f.Rule, worktreeRuleUnclaimed)
	}
}

// TestChatSessionIDSpellingsAgree pins the reader's two-spelling bridge against
// the LIVE producer's own constants: the branch names `chat/<stem>`, the index
// names the same session with its dashes, and the reader must reach the row
// from the stem alone. The measured pair is on the operator's profile
// (`chat/c5f1189ace3444f2a40c74ebcb148fc7` ↔
// `c5f1189a-ce34-44f2-a40c-74ebcb148fc7`).
func TestChatSessionIDSpellingsAgree(t *testing.T) {
	const stem = "c5f1189ace3444f2a40c74ebcb148fc7"
	const want = "c5f1189a-ce34-44f2-a40c-74ebcb148fc7"
	if got := worktreeSweepChatDashedSessionID(stem); got != want {
		t.Fatalf("dashed spelling = %q, want %q", got, want)
	}
	// Both spellings are offered to the index, in that order.
	ids := worktreeSweepChatSessionIDs(stem)
	if len(ids) != 2 || ids[0] != stem || ids[1] != want {
		t.Fatalf("session id spellings = %v, want [%s %s]", ids, stem, want)
	}
	// An already-dashed id is not re-dashed, and a non-UUID stem yields none.
	if got := worktreeSweepChatDashedSessionID(want); got != "" {
		t.Fatalf("an already-dashed id was re-dashed: %q", got)
	}
	if got := worktreeSweepChatDashedSessionID("not-a-uuid"); got != "" {
		t.Fatalf("a non-UUID stem produced a session id: %q", got)
	}
}

// TestChatTableNameMatchesTheRecorder is the anti-drift pin between the WRITER
// of the chat index and this reader: the reader selects from
// worktreeSweepChatTableName and reads started_at/ended_at, while chat.go
// renders the DDL. A rename on either side must fail HERE rather than silently
// turning every chat ground into "the index could not be read" (kept forever).
func TestChatTableNameMatchesTheRecorder(t *testing.T) {
	ddl := chatsTable()
	if !strings.Contains(ddl, worktreeSweepChatTableName) {
		t.Fatalf("the recorder's DDL does not create %q — the reader would never find the table", worktreeSweepChatTableName)
	}
	for _, column := range []string{"session_id", "started_at", "ended_at"} {
		if !strings.Contains(ddl, column) {
			t.Fatalf("the recorder's DDL does not carry the %q column the reader selects", column)
		}
	}
}

// chatSweepSeedClosedRow writes a session's index row through the PRODUCTION
// recorder and stamps it CLOSED — the positive evidence a reclaim rests on. It
// is the shared fixture behind both the gate case and the runner tests, so the
// two cannot disagree about what "the session is over" looks like.
func chatSweepSeedClosedRow(t *testing.T, stem string) {
	t.Helper()
	chatSweepSeedRow(t, testutil.NewTestDBPath(t), stem, time.Now().Add(-2*time.Hour))
}

// chatSweepDenyIndex points the chat index at a path that does not exist, so the
// reader's unanswerable branch is exercised (fail closed). t.Setenv, never
// os.Setenv (Rule 17): the value is restored at the test's end.
func chatSweepDenyIndex(t *testing.T) {
	t.Helper()
	t.Setenv(chatDBEnv, filepath.Join(t.TempDir(), "no-such-chat-index.db"))
}

// chatSweepTouchSessionDir creates (or re-stamps) a session's own directory
// under the sessions root — the filesystem half of the liveness evidence.
func chatSweepTouchSessionDir(t *testing.T, stem string) {
	t.Helper()
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("gopi dir: %v", err)
	}
	dir := filepath.Join(gd, taskSessionsRootName, stem)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	now := time.Now()
	if err := os.Chtimes(dir, now, now); err != nil {
		t.Fatalf("stamp %s: %v", dir, err)
	}
}
