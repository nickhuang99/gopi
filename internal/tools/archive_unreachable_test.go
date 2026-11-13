package tools

// archive_unreachable_test.go — the census/rescue contract.
//
// The subject is a repo state nothing else in the suite builds: commits that
// exist but which NO ref names, some of them still inside the reflog grace
// window and some already past it. Every fixture here is built with git
// plumbing (Rule 27: no load, no synthetic pressure — a handful of empty
// commits), and the assertion is always over the MEASURED object presence
// (`git cat-file -t`) or the measured ref set, never over a proxy.
//
// The reflog half is the point of the whole surface, so it is measured in both
// directions: a freshly-orphaned tip reports `reflog:<ref>` (it survives until
// that reflog is expired), and the SAME repo after
// `git reflog expire --expire=now --all` reports `past-reflog` for every one of
// them. A census that answered the same thing in both states would be a census
// that measures nothing.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// unreachFixtureRepo builds a throwaway repo with two orphan CHAINS of commits
// (each chain = one tip whose parent is also unreachable, which is the shape the
// backlog is made of and the shape `git fsck --unreachable=<sha>` mis-answers —
// see the file header) plus one reachable commit. It returns the repo path and
// the two tip shas.
//
// The orphans are produced the way the real ones are: a branch is created,
// committed on, and then the BRANCH REF is deleted while a reflog entry still
// names the commits.
func unreachFixtureRepo(t *testing.T) (repo string, tips []string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	gitRun(t, root, "init", "-q", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-q", "-b", "master")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "seed")

	for i, name := range []string{"orphan-a", "orphan-b"} {
		gitRun(t, repo, "checkout", "-q", "-b", name, "master")
		// TWO commits per chain: the first is then a PARENT of another
		// unreachable commit, so it is NOT a tip — the exact distinction the
		// census exists to draw.
		gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "unreachable-"+name+"-1")
		gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "unreachable-"+name+"-2")
		tips = append(tips, unreachRev(t, repo, "HEAD"))
		gitRun(t, repo, "checkout", "-q", "master")
		gitRun(t, repo, "branch", "-D", name)
		_ = i
	}
	return repo, tips
}

// unreachRev resolves a revision in the fixture.
func unreachRev(t *testing.T, repo, rev string) string {
	t.Helper()
	out, err := repoSweepGit(repo, gitRevParseVerb, rev)
	if err != nil {
		t.Fatalf("rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(out)
}

// unreachExpireReflogs drops every reflog entry: the ONE command that converts
// "held by the grace window" into "kept by nothing".
func unreachExpireReflogs(t *testing.T, repo string) {
	t.Helper()
	gitRun(t, repo, "reflog", "expire", "--expire=now", "--all")
}

// TestArchiveUnreachableCensusSplitsTheKeepingWindow is the census's core
// contract: two orphan chains yield exactly TWO tips (not four commits), each
// tip reports the reflog still naming it, and after the reflogs are expired the
// SAME repo reports every tip as past-reflog.
func TestArchiveUnreachableCensusSplitsTheKeepingWindow(t *testing.T) {
	repo, tips := unreachFixtureRepo(t)
	now := time.Now()

	census, err := ArchiveUnreachableCensusAt(repo, now)
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	if census.UnreachableCommits != 4 {
		t.Fatalf("unreachable commits = %d, want 4 (two chains of two)", census.UnreachableCommits)
	}
	if len(census.Tips) != 2 {
		t.Fatalf("tips = %d, want 2 (each chain's FIRST commit is its tip's parent): %+v", len(census.Tips), census.Tips)
	}
	if census.ReflogHeld != 2 || census.PastReflog != 0 {
		t.Fatalf("keeping windows = held:%d past:%d, want 2 held / 0 past — a freshly orphaned tip is inside the window",
			census.ReflogHeld, census.PastReflog)
	}
	got := map[string]bool{}
	for _, tip := range census.Tips {
		got[tip.SHA] = true
		if !tip.ReflogHeld || len(tip.ReflogRefs) == 0 {
			t.Errorf("tip %s: ReflogHeld=%v refs=%v — the branch reflog still names it",
				tip.Short, tip.ReflogHeld, tip.ReflogRefs)
		}
		if tip.Subject == "" {
			t.Errorf("tip %s: empty subject — the census must render what the work says", tip.Short)
		}
		if !strings.Contains(ArchiveUnreachableHeldLabel(tip), archiveHeldReflogPrefix) {
			t.Errorf("tip %s: held label %q must name the reflog holding it", tip.Short, ArchiveUnreachableHeldLabel(tip))
		}
		if ArchiveUnreachableDecisionLabel(tip) != archiveDecisionNone {
			t.Errorf("tip %s: decision label %q, want %q — no archive tag names it",
				tip.Short, ArchiveUnreachableDecisionLabel(tip), archiveDecisionNone)
		}
	}
	for _, want := range tips {
		if !got[want] {
			t.Errorf("tip %s missing from the census", archiveShortSHA(want))
		}
	}

	// The SAME repo, one reflog expiry later: the grace window is gone.
	unreachExpireReflogs(t, repo)
	after, err := ArchiveUnreachableCensusAt(repo, now)
	if err != nil {
		t.Fatalf("census after reflog expiry: %v", err)
	}
	if len(after.Tips) != 2 {
		t.Fatalf("tips after reflog expiry = %d, want 2 (the objects are still present)", len(after.Tips))
	}
	if after.ReflogHeld != 0 || after.PastReflog != 2 {
		t.Fatalf("keeping windows after expiry = held:%d past:%d, want 0 held / 2 past",
			after.ReflogHeld, after.PastReflog)
	}
	for _, tip := range after.Tips {
		if ArchiveUnreachableHeldLabel(tip) != archiveHeldPastReflog {
			t.Errorf("tip %s: label after expiry = %q, want %q",
				tip.Short, ArchiveUnreachableHeldLabel(tip), archiveHeldPastReflog)
		}
	}
	// The label is not cosmetic: the objects really are still present.
	for _, tip := range after.Tips {
		if typ, err := repoSweepGit(repo, gitCatFileVerb, "-t", tip.SHA); err != nil || strings.TrimSpace(typ) != gitObjectTypeCommit {
			t.Errorf("tip %s: cat-file -t = %q err=%v, want commit — past-reflog is a keeping window, not a deletion", tip.Short, typ, err)
		}
	}
}

// TestArchiveUnreachableCensusOnACleanRepoIsAMeasurementNotAFailure pins the
// empty case: a repo whose every commit is reachable answers zero, and it
// answers it as a MEASUREMENT (no error, and a report that says so).
func TestArchiveUnreachableCensusOnACleanRepoIsAMeasurementNotAFailure(t *testing.T) {
	repo := branchGuardFixtureRepo(t)
	census, err := ArchiveUnreachableCensusAt(repo, time.Now())
	if err != nil {
		t.Fatalf("census of a clean repo: %v", err)
	}
	if census.UnreachableCommits != 0 || len(census.Tips) != 0 {
		t.Fatalf("clean repo reported %d commits / %d tips, want 0/0", census.UnreachableCommits, len(census.Tips))
	}
	report := ArchiveUnreachableReport(census)
	if !strings.Contains(report, "no unreachable tip") {
		t.Errorf("the zero report must SAY it measured zero (an empty census and an unasked question must not look alike):\n%s", report)
	}
}

// TestArchiveUnreachableCensusReportsAScanFailure is the Rule 15 half: a repo
// the scan cannot read is an ERROR, never a clean zero.
func TestArchiveUnreachableCensusReportsAScanFailure(t *testing.T) {
	notARepo := t.TempDir()
	if _, err := ArchiveUnreachableCensusAt(notARepo, time.Now()); err == nil {
		t.Fatal("census of a non-repo returned no error — a scan that could not answer must not render as a clean zero")
	}
	if _, err := ArchiveUnreachableCensusAt(filepath.Join(notARepo, "does-not-exist"), time.Now()); err == nil {
		t.Fatal("census of a missing directory returned no error")
	}
}

// TestArchiveUnreachableDecisionsNamesTheArchiveTag is the decision lookup's
// positive control, taken at the function (a tip that an archive tag names is
// NOT unreachable any more, so the census itself can only ever print `none`):
// the tag namespace is read, the branch is taken from the REF NAME, and the
// reason comes from the companion record — including the reason-LESS shape the
// 21:04 batch left behind.
func TestArchiveUnreachableDecisionsNamesTheArchiveTag(t *testing.T) {
	repo, tips := unreachFixtureRepo(t)
	tip := tips[0]

	// A REASON-LESS tag (the 21:04 shape): the tag namespace names the commit,
	// the companion namespace says nothing.
	gitRun(t, repo, "tag", archiveBranchPrefix+"reasonless", tip)
	dec, err := archiveUnreachableDecisions(repo)
	if err != nil {
		t.Fatalf("decision scan: %v", err)
	}
	got, ok := dec[tip]
	if !ok {
		t.Fatalf("the archive tag naming %s was not found by the decision scan", archiveShortSHA(tip))
	}
	if got.ArchiveBranch != "reasonless" {
		t.Errorf("decision branch = %q, want reasonless (taken from the ref name)", got.ArchiveBranch)
	}
	label := ArchiveUnreachableDecisionLabel(got)
	if !strings.Contains(label, ArchiveReasonMissingLabel) {
		t.Errorf("a reason-less tag must render as %q, got %q", ArchiveReasonMissingLabel, label)
	}

	// The same commit, now with a recorded reason: the record is what the
	// lookup joins on.
	if _, err := ArchiveTagReasoned(repo, "reasonless", ArchiveReasonHandMistake, "test"); err != nil {
		t.Fatalf("record a reason: %v", err)
	}
	dec, err = archiveUnreachableDecisions(repo)
	if err != nil {
		t.Fatalf("decision scan after recording: %v", err)
	}
	if got := ArchiveUnreachableDecisionLabel(dec[tip]); got != "reasonless ("+ArchiveReasonHandMistake+")" {
		t.Errorf("decision label = %q, want %q", got, "reasonless ("+ArchiveReasonHandMistake+")")
	}
}

// TestArchiveRescueTipMintsADurableRefThroughTheOneWriter is the rescue's
// contract end to end, and it is the REASON the rescue exists: a commit nothing
// names becomes one that survives `git gc --prune=now` AND a full reflog
// expiry, while the census first reports it as unreachable.
//
// It also pins the two refusals: a second rescue of the same commit writes
// nothing (a durable ref already holds it), and an unknown reason is an error
// (the reason vocabulary is closed).
func TestArchiveRescueTipMintsADurableRefThroughTheOneWriter(t *testing.T) {
	repo, tips := unreachFixtureRepo(t)
	tip := tips[0]

	census, err := ArchiveUnreachableCensusAt(repo, time.Now())
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	if !censusHasTip(census, tip) {
		t.Fatalf("fixture premise broken: %s is not reported as an unreachable tip", archiveShortSHA(tip))
	}

	res, err := ArchiveRescueTip(repo, tip, ArchiveReasonHandMistake, "orphaned by a branch delete")
	if err != nil {
		t.Fatalf("rescue: %v", err)
	}
	if !res.Recorded {
		t.Fatalf("rescue recorded nothing: %q", res.Refusal)
	}
	wantBranch := archiveUnreachableBranchPrefix + archiveShortSHA(tip)
	if res.Branch != wantBranch {
		t.Errorf("rescue branch = %q, want %q", res.Branch, wantBranch)
	}
	if res.Tag != archiveBranchPrefix+wantBranch {
		t.Errorf("rescue tag = %q, want %q", res.Tag, archiveBranchPrefix+wantBranch)
	}
	// BOTH refs exist, through the one writer: the tag at the TIP, the reason
	// record beside it.
	tagTip, err := repoSweepGit(repo, gitRevParseVerb, archiveTagRefPrefix+wantBranch)
	if err != nil || strings.TrimSpace(tagTip) != tip {
		t.Fatalf("archive tag points at %q (err=%v), want %s", strings.TrimSpace(tagTip), err, tip)
	}
	rec, err := repoSweepGit(repo, gitCatFileVerb, "blob", archiveReasonRefPrefix+wantBranch)
	if err != nil {
		t.Fatalf("reason record: %v", err)
	}
	if got := ArchiveTagReason(rec); got != ArchiveReasonHandMistake {
		t.Errorf("recorded reason = %q, want %q", got, ArchiveReasonHandMistake)
	}

	// THE MEASURED EFFECT: the tip survives the act that would have destroyed
	// it — a full reflog expiry plus a prune.
	unreachExpireReflogs(t, repo)
	if out, err := exec.Command("git", "-C", repo, "gc", "--prune=now").CombinedOutput(); err != nil {
		t.Fatalf("gc: %v: %s", err, out)
	}
	if typ, err := repoSweepGit(repo, gitCatFileVerb, "-t", tip); err != nil || strings.TrimSpace(typ) != gitObjectTypeCommit {
		t.Fatalf("rescued tip is GONE after gc (type=%q err=%v) — the rescue did not make it durable", typ, err)
	}
	after, err := ArchiveUnreachableCensusAt(repo, time.Now())
	if err != nil {
		t.Fatalf("census after rescue: %v", err)
	}
	if censusHasTip(after, tip) {
		t.Errorf("the rescued tip is still reported as unreachable — the durable ref did not take it out of the backlog")
	}

	// Idempotent refusal: a durable holder means there is nothing to rescue.
	again, err := ArchiveRescueTip(repo, tip, ArchiveReasonHandMistake, "second call")
	if err != nil {
		t.Fatalf("second rescue returned an error: %v", err)
	}
	if again.Recorded {
		t.Error("a second rescue of the same tip wrote another durable ref — one tip must not get two names to keep in step")
	}
	if !strings.Contains(again.Refusal, res.Tag) {
		t.Errorf("the refusal must NAME the durable holder; got %q", again.Refusal)
	}

	// The reason vocabulary is closed, before any write.
	if _, err := ArchiveRescueTip(repo, tips[1], "not-a-reason", ""); err == nil {
		t.Error("rescue accepted an unknown reason — the vocabulary is what makes the WHY queryable")
	}
}

// TestArchiveRescueTipIgnoresANonDurableHolder is the sibling fix's lesson
// applied to the rescue: a ref the pushback lane writes and prunes within one
// pass is NOT a reason to skip a rescue (it is exactly how the tip became
// unreachable). The same fixture as the sibling guard's red proof.
func TestArchiveRescueTipIgnoresANonDurableHolder(t *testing.T) {
	repo, tips := unreachFixtureRepo(t)
	tip := tips[0]
	gitRun(t, repo, "update-ref", gitPushbackCheckRootRef+IntegrationBranchName, tip)

	res, err := ArchiveRescueTip(repo, tip, ArchiveReasonHandMistake, "only a scratch mirror held it")
	if err != nil {
		t.Fatalf("rescue over a mirror-only holder: %v", err)
	}
	if !res.Recorded {
		t.Fatalf("a NON-durable holder must not block a rescue: %q", res.Refusal)
	}
	if len(res.NonDurableHolders) == 0 {
		t.Error("the rescue must REPORT the holder it declined to count (a silent ignore hides the fact the tip was one pass from gone)")
	}
}

// TestArchiveRescueTipNamesAnUnresolvableRevision keeps the rescue's error path
// loud: a revision that is not a commit is an error naming the revision, never a
// silently-minted tag over whatever git happened to answer.
func TestArchiveRescueTipNamesAnUnresolvableRevision(t *testing.T) {
	repo := branchGuardFixtureRepo(t)
	if _, err := ArchiveRescueTip(repo, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", ArchiveReasonHandMistake, ""); err == nil {
		t.Fatal("rescue of a nonexistent revision returned no error")
	}
	// A TREE is not a commit: the peel is what refuses it.
	tree, err := repoSweepGit(repo, gitRevParseVerb, "HEAD^{tree}")
	if err != nil {
		t.Fatalf("tree rev-parse: %v", err)
	}
	if _, err := ArchiveRescueTip(repo, strings.TrimSpace(tree), ArchiveReasonHandMistake, ""); err == nil {
		t.Fatal("rescue of a TREE returned no error — the ^{commit} peel must refuse it")
	}
}

// TestArchiveUnreachableReportRendersEveryClass pins the renderer against the
// failure mode the sibling epic measured (Rule 15): a report that prints only a
// total makes a wrong classification invisible. The fixture has both arms, so
// the rendered lines must carry the split, the per-tip age, and the rescue hint.
func TestArchiveUnreachableReportRendersEveryClass(t *testing.T) {
	repo, _ := unreachFixtureRepo(t)
	census, err := ArchiveUnreachableCensusAt(repo, time.Now())
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	report := ArchiveUnreachableReport(census)
	for _, want := range []string{
		"unreachable commits: 4",
		"unreachable tips:    2",
		"kept by a reflog:    2",
		"past the reflog:     0",
		archiveHeldReflogPrefix,
		archiveDecisionNone,
		"--rescue-tip",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q:\n%s", want, report)
		}
	}
	// The age label is derived from the SAME duration the row carries, so a
	// fixture committed today must not render as the unknown placeholder.
	if strings.Contains(report, "  "+archiveAgeUnknown+"  ") {
		t.Errorf("a dated tip rendered as %q:\n%s", archiveAgeUnknown, report)
	}
	if got := ArchiveUnreachableAgeLabel(0); got != archiveAgeUnknown {
		t.Errorf("age label for an unknown date = %q, want %q", got, archiveAgeUnknown)
	}
}

// censusHasTip reports whether the census names a sha among its tips.
func censusHasTip(c ArchiveUnreachableCensus, sha string) bool {
	for _, tip := range c.Tips {
		if tip.SHA == sha {
			return true
		}
	}
	return false
}

// TestArchiveUnreachableCensusSurvivesAnUnreadableObjectStoreNote documents the
// one degradation the census KEEPS rather than failing on: a commit fsck
// reported but `git log` could not describe is counted and printed, never
// dropped silently. It is asserted through the renderer because that is where a
// dropped row would become invisible.
func TestArchiveUnreachableCensusNotesUndescribedCommits(t *testing.T) {
	c := ArchiveUnreachableCensus{Repo: "/x", UnreachableCommits: 3, Undescribed: 1,
		Tips: []ArchiveUnreachableTip{}, Notes: []string{"1 unreachable commit(s) returned no `git log` row"}}
	out := ArchiveUnreachableReport(c)
	if !strings.Contains(out, "UNDESCRIBED: 1") {
		t.Errorf("an undescribed commit must be printed, got:\n%s", out)
	}
}
