package tools

// Regression for the CONTENT-EQUIVALENCE arm of the delivery proof
// (task_delivery_proof_content.go).
//
// THE DEFECT THIS FILE PINS. The clone channel delivers with
// `pull --rebase → build → push`, so a run's commits are REPLAYED onto a moved
// destination: the sha changes, and `git patch-id --stable` changes too whenever
// the rebase re-derives a hunk's context. Both old arms then answer "not
// delivered" for work that IS at the destination, and three measured grounds
// were refused on that basis (01a17a67, 01a17a5f, 01a17abb).
//
// FIVE ARMS, all driven through the REAL write path (TaskUpdateWithResult — the
// one local mutation core) against REAL git repositories:
//
//	A  the destination carries the run's change under a DIFFERENT commit with an
//	   IDENTICAL patch-id (the pure-rebase shape);
//	B  the destination's copy differs only in a `measured-at:` metadata line (the
//	   row that records the base it was measured at, which every rebase rewrites);
//	C  the destination's copy differs only in a comment line and a blank line;
//	D  the work is genuinely NOT at the destination: the destination carries a
//	   SMALLER version of the same file, so the run's added lines are absent
//	   (row 01a17abb's shape — master is missing functions the ground has);
//	E  the landing commit is BEHIND the destination's bounded path-history window
//	   (it landed, then was superseded, then buried by later commits touching the
//	   same path) — the work is at the destination and must not be refused.
//
// RED PROOF, and it is why A/B/C exist as three arms rather than one: each of
// them flips deliveryProofContentArmOff, re-runs the write, and asserts the row
// is REFUSED. If the content arm were not doing the work, they would pass with
// the arm off too and prove nothing.
//
// THE FIXTURE'S SHAPE MATTERS, and it is measured, not invented. A clone ground
// that never fetched has the run's COMMITS and a STALE destination; the recorded
// repo has a FRESH destination and NOT the run's commits. That is exactly why
// neither ancestry nor `git cherry` can answer (both need the tip's object in
// the repo they are asked in) and why the content arm reads each side where it
// lives — WITHOUT a fetch, a push or a ref move.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// dpRebaseWorkPath is the file the run's work lands in.
	dpRebaseWorkPath = "work.go"
	// dpRebaseBasePath is the file the fixture's base commit carries.
	dpRebaseBasePath = "base.txt"
	// dpRebaseMetadataLine is the SELF-REFERENTIAL row: it names the revision it
	// was measured at, so a rebase is guaranteed to change it (sample B).
	dpRebaseMetadataLine = "# measured-at: "
	// dpRebaseCommentLine is the comment a rebase may legitimately reflow
	// (sample C).
	dpRebaseCommentLine = "// adopted: reflowed by the rebase, same work"
)

// dpRebaseChannel builds the delivered-somewhere-else shape and returns
// (ground, bare, repo):
//
//	ground  a CLONE of bare that never fetched after the adoption — its
//	        origin/<branch> is the CUT POINT, and it holds the run's commits;
//	repo    a SECOND clone that fetched the adoption — its origin/<branch> is
//	        the fresh destination, and it does NOT hold the run's commits.
func dpRebaseChannel(t *testing.T) (ground, bare, repo string) {
	t.Helper()
	root := t.TempDir()
	bare = filepath.Join(root, "server")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, bare, "init", "-q", "--bare", "-b", masterBranchName)

	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "init", "-q", "-b", masterBranchName)
	dpRebaseIdentity(t, seed)
	if err := os.WriteFile(filepath.Join(seed, dpRebaseBasePath), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, seed, "add", dpRebaseBasePath)
	rebuildTestGit(t, seed, "commit", "-q", "-m", "base")
	rebuildTestGit(t, seed, "push", "-q", bare, masterBranchName)

	ground = filepath.Join(root, "ground")
	rebuildTestGit(t, root, "clone", "-q", bare, ground)
	dpRebaseIdentity(t, ground)
	repo = filepath.Join(root, "repo")
	rebuildTestGit(t, root, "clone", "-q", bare, repo)
	dpRebaseIdentity(t, repo)
	if info, err := os.Stat(filepath.Join(ground, dpGroundGit)); err != nil || !info.IsDir() {
		t.Fatalf("the fixture ground must be a CLONE (.git a directory): %v", err)
	}
	return ground, bare, repo
}

// dpRebaseIdentity configures a committer, so a fixture commit works without the
// host's global git config.
func dpRebaseIdentity(t *testing.T, dir string) {
	t.Helper()
	rebuildTestGit(t, dir, "config", "user.email", "dp@test")
	rebuildTestGit(t, dir, "config", "user.name", "dp")
}

// dpRebaseCommitWork writes the run's work in the ground and returns its tip.
// The body is deliberately several lines, so a difference of ONE line is a
// partial match rather than a total miss.
func dpRebaseCommitWork(t *testing.T, ground, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ground, dpRebaseWorkPath), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, ground, "add", dpRebaseWorkPath)
	rebuildTestGit(t, ground, "commit", "-q", "-m", "feat: the run's work")
	return rebuildTestGit(t, ground, "rev-parse", "HEAD")
}

// dpRebaseAdopt lands the SAME change on the destination from the repo's side
// (the merger's adoption: a different commit, the same work) and returns the
// destination tip. `perturb` edits the destination's copy BEFORE committing, so
// an arm can shape B (metadata) or C (comment/blank) — and gets "" for the
// identical-copy case A.
func dpRebaseAdopt(t *testing.T, ground, bare, repo, body string, perturb func(string) string) string {
	t.Helper()
	dest := body
	if perturb != nil {
		dest = perturb(body)
	}
	if err := os.WriteFile(filepath.Join(repo, dpRebaseWorkPath), []byte(dest), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", dpRebaseWorkPath)
	rebuildTestGit(t, repo, "commit", "-q", "-m", "adoption: the same work")
	rebuildTestGit(t, repo, "push", "-q", deliveryProofOriginRemote, masterBranchName)
	// The repo is FRESH: its tracking ref follows the point it just pushed to.
	rebuildTestGit(t, repo, "fetch", "-q", deliveryProofOriginRemote)
	return rebuildTestGit(t, repo, "rev-parse", "HEAD")
}

// dpRebaseBlobOf reads the blob a commit/revision holds for a path — the
// fixture's own evidence that arm A is the sample-A shape: the SAME content under
// a DIFFERENT commit (a cherry-pick reproduces the patch exactly).
func dpRebaseBlobOf(t *testing.T, dir, rev, path string) string {
	t.Helper()
	return rebuildTestGit(t, dir, "rev-parse", rev+":"+path)
}

// dpRebaseRunWrite seeds a row whose deliverable names the fixture's ground and
// repo, then drives the REAL write path with `completed`.
func dpRebaseRunWrite(t *testing.T, dbPath, id, repo, ground string) error {
	t.Helper()
	dpSeedRow(t, dbPath, id, "feat: rebase-delivered work", taskStatusInProgress)
	dpWriteDelivery(t, dbPath, id, repo, masterBranchName, ground)
	_, err := TaskUpdateWithResult(id, taskStatusCompleted, "work done")
	return err
}

// dpRebaseAssertDelivered is the should-pass half: the write lands `completed`.
func dpRebaseAssertDelivered(t *testing.T, dbPath, id, repo, ground, why string) {
	t.Helper()
	if err := dpRebaseRunWrite(t, dbPath, id, repo, ground); err != nil {
		t.Fatalf("%s: the write must NOT be refused, got: %v", why, err)
	}
	if got := dpRowStatus(t, dbPath, id); got != taskStatusCompleted {
		t.Errorf("%s: row status = %q, want %q", why, got, taskStatusCompleted)
	}
}

// dpRebaseAssertRefusedWithoutTheArm is the RED PROOF: with the content arm
// switched OFF the very same fixture must be REFUSED — which is what proves the
// arm (and not something else in the chain) is what lets the close through.
func dpRebaseAssertRefusedWithoutTheArm(t *testing.T, dbPath, id, repo, ground, why string) {
	t.Helper()
	deliveryProofContentArmOff = true
	defer func() { deliveryProofContentArmOff = false }()
	err := dpRebaseRunWrite(t, dbPath, id+"-off", repo, ground)
	if err == nil {
		t.Fatalf("%s: with the content arm OFF the write must be REFUSED (red proof); "+
			"it landed, so arms A/B/C prove nothing about the arm they claim to cover", why)
	}
	if !strings.Contains(err.Error(), deliveryProofMarker) {
		t.Errorf("%s: the refusal must carry the marker %q, got: %v", why, deliveryProofMarker, err)
	}
	if got := dpRowStatus(t, dbPath, id+"-off"); got != taskStatusInProgress {
		t.Errorf("%s: with the arm OFF the row must stay OPEN, got %q", why, got)
	}
}

// ARM A — the pure rebase: the destination carries the run's change under a
// DIFFERENT commit, byte-identical to the run's own copy, and the ground's cache
// never saw it.
//
// THE FIXTURE IS THE MEASURED SYMPTOM, not a convenient one. On the real ground
// (01a17a67) `git cherry` printed `+` for 50b57f8 even though that commit's own
// `git patch-id --stable` (f9fbd4d1…) EQUALLED master 5e3ce2e18's — a stale
// tracking ref is enough to make the patch arm answer "absent", and the recorded
// repo cannot answer at all because it never fetched the run's commits. So the
// fixture reproduces BOTH halves: the ground has the commits and a stale cache,
// the repo has the destination and not the commits. Only a content comparison
// that reads each side where it lives can answer.
func TestDeliveryProofContent_RebaseUnderAnotherCommitIsDelivered(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, repo := dpRebaseChannel(t)
	body := "package x\n\nfunc Work() string {\n\treturn \"delivered by a rebase\"\n}\n"
	groundTip := dpRebaseCommitWork(t, ground, body)
	destTip := dpRebaseAdopt(t, ground, "", repo, body, nil)

	if groundTip == destTip {
		t.Fatalf("fixture is wrong: the adoption must be a DIFFERENT commit (%s)", branchDeleteShortHash(destTip))
	}
	if g, d := dpRebaseBlobOf(t, ground, groundTip, dpRebaseWorkPath), dpRebaseBlobOf(t, repo, destTip, dpRebaseWorkPath); g == "" || g != d {
		t.Fatalf("fixture is wrong: sample A needs the IDENTICAL content under a different commit, got ground=%q dest=%q", g, d)
	}
	// The measured symptom: the ground's STALE cache makes the patch arm say the
	// run's patch is ABSENT — which is exactly what refused the real row.
	if got := rebuildTestGit(t, ground, "cherry", "refs/remotes/"+deliveryProofOriginRemote+"/"+masterBranchName, groundTip); !strings.HasPrefix(got, deliveryProofCherryAbsentMark) {
		t.Fatalf("fixture is wrong: the stale-cache patch arm must report the run's patch ABSENT, got %q", got)
	}

	dpRebaseAssertDelivered(t, db, "dp-content-a", repo, ground, "A: same change under a rewritten commit")
	// THE RED PROOF FOR THIS SHAPE CHANGED, AND THE CHANGE IS ITSELF A FINDING
	// (measured 2026-10-29, together with the pre-judgement refresh). This arm
	// used to assert that switching the CONTENT leg off alone refuses the write,
	// on the premise that the ground's STALE tracking ref makes `git cherry`
	// report the run's patch ABSENT. That premise is now REPAIRED rather than
	// relied on: the arm refreshes the destination view once before judging
	// (deliveryProofRefreshDestination), so for case A — identical content under
	// another commit — the patch leg answers by itself. Measured: with only
	// deliveryProofContentArmOff set, this very fixture LANDS.
	//
	// So case A no longer proves anything about the content leg, and asserting
	// that it does would pin a stale view as if it were the criterion. What still
	// has to be proved is that A's close is the CRITERION working rather than the
	// chain being permissive, in the two directions of the repair below.
	dpRebaseAssertDeliveredByThePatchArmAlone(t, db, "dp-content-a-patchoff", repo, ground)
}

// ARM B — the destination's copy differs ONLY in a `measured-at:` metadata line:
// the row that records the base it was measured at, which every rebase rewrites
// and which no patch comparison can ever match.
func TestDeliveryProofContent_MetadataLineDifferenceIsDelivered(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, repo := dpRebaseChannel(t)
	body := "package x\n\nfunc Work() string {\n\treturn \"delivered under a rewritten base\"\n}\n"
	dpRebaseCommitWork(t, ground, body)
	dpRebaseAdopt(t, ground, "", repo, body, func(s string) string {
		return dpRebaseMetadataLine + "9f1c0ffee0000000000000000000000000000000\n" + s
	})

	dpRebaseAssertDelivered(t, db, "dp-content-b", repo, ground, "B: only a measured-at: line differs")
	dpRebaseAssertRefusedWithoutTheArm(t, db, "dp-content-b", repo, ground, "B: only a measured-at: line differs")
}

// ARM C — the destination's copy differs ONLY in a comment line and a blank
// line: the shape that changes a patch id without changing any work.
func TestDeliveryProofContent_CommentAndBlankLineDifferenceIsDelivered(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, repo := dpRebaseChannel(t)
	body := "package x\n\nfunc Work() string {\n\treturn \"delivered through a reflowed hunk\"\n}\n"
	dpRebaseCommitWork(t, ground, body)
	dpRebaseAdopt(t, ground, "", repo, body, func(s string) string {
		return dpRebaseCommentLine + "\n\n" + s
	})

	dpRebaseAssertDelivered(t, db, "dp-content-c", repo, ground, "C: only a comment line and a blank line differ")
	dpRebaseAssertRefusedWithoutTheArm(t, db, "dp-content-c", repo, ground, "C: only a comment line and a blank line differ")
}

// ARM E — THE LANDING COMMIT IS BEHIND THE DESTINATION'S PATH-HISTORY WINDOW.
//
// MEASURED DEFECT this arm pins (2026-10-29, master bb673af): the historical
// landing test walked only the newest `deliveryProofContentClosestCandidates`
// (8) destination commits that TOUCH the path, then answered "not carried". Once
// the integration branch moves on, the commit that really landed the work falls
// out of that window and a DELIVERED change is refused — the false refusal this
// whole criterion exists to remove. Measured on the real tree: the two lines
// added by ground commit e0b1726 were carried by master 1ae64e138, which sits at
// position 10 of `git log master -- internal/tools/task_delivery_proof.go`, while
// the destination's tip no longer holds them (d010dfd0a rewrote them), so every
// arm answered "absent".
//
// The fixture reproduces the shape without any real tree: the adoption lands the
// same lines under a different commit, a LATER destination commit supersedes
// them, and the destination then moves on with more commits touching that same
// path than the window can see. The work is at the destination — the answer must
// be DELIVERED.
func TestDeliveryProofContent_LandingCommitBehindThePathHistoryWindowIsDelivered(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, repo := dpRebaseChannel(t)
	body := "package x\n\nfunc Window() string {\n\treturn \"adopted, then superseded by a later copy\"\n}\n"
	dpRebaseCommitWork(t, ground, body)
	landing := dpRebaseAdopt(t, ground, "", repo, body, nil)

	// The destination SUPERSEDES those lines: the tip no longer carries them, so
	// only the history can answer.
	superseded := "package x\n\nfunc Window() string {\n\treturn \"rewritten by a later commit\"\n}\n"
	if err := os.WriteFile(filepath.Join(repo, dpRebaseWorkPath), []byte(superseded), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, repo, "add", dpRebaseWorkPath)
	rebuildTestGit(t, repo, "commit", "-q", "-m", "refactor: supersede the adopted copy")

	// …and it moves on with MORE commits touching the same path than the bounded
	// window can see. The window is referenced by its constant (Rule 1/13), so
	// widening it cannot silently empty this arm.
	workPath := filepath.Join(repo, dpRebaseWorkPath)
	for i := 0; i < deliveryProofContentClosestCandidates+2; i++ {
		// Read-then-write rather than an append handle: the handle's Close error
		// would have to be checked on every path (the swept rule this package's
		// own style gate enforces), and a fixture has no use for a partial write.
		body, err := os.ReadFile(workPath)
		if err != nil {
			t.Fatal(err)
		}
		note := dpRebaseCommentLine + " (destination note row)\n"
		if err := os.WriteFile(workPath, append(body, note...), 0o644); err != nil {
			t.Fatal(err)
		}
		rebuildTestGit(t, repo, "add", dpRebaseWorkPath)
		rebuildTestGit(t, repo, "commit", "-q", "-m", "docs: destination moves on")
	}
	rebuildTestGit(t, repo, "push", "-q", deliveryProofOriginRemote, masterBranchName)
	rebuildTestGit(t, repo, "fetch", "-q", deliveryProofOriginRemote)
	destTip := rebuildTestGit(t, repo, "rev-parse", "HEAD")

	// PRE-CONDITIONS — the fixture must BE the shape, or the arm proves nothing.
	landedText := rebuildTestGit(t, repo, "show", landing+":"+dpRebaseWorkPath)
	for _, line := range deliveryProofNormalizeLines(body) {
		if !strings.Contains(landedText, line) {
			t.Fatalf("fixture is wrong: the landing commit %s does not carry %q", branchDeleteShortHash(landing), line)
		}
	}
	window := strings.Fields(rebuildTestGit(t, repo, "log", "--format=%H", "-n",
		fmt.Sprint(deliveryProofContentClosestCandidates), destTip, "--", dpRebaseWorkPath))
	if len(window) < deliveryProofContentClosestCandidates {
		t.Fatalf("fixture is wrong: the destination must carry at least %d commits touching %s, got %d",
			deliveryProofContentClosestCandidates, dpRebaseWorkPath, len(window))
	}
	for _, c := range window {
		if c == landing {
			t.Fatalf("fixture is wrong: the landing commit must be OUTSIDE the %d-commit window, it is inside",
				deliveryProofContentClosestCandidates)
		}
	}
	if tipText := rebuildTestGit(t, repo, "show", destTip+":"+dpRebaseWorkPath); strings.Contains(tipText, "adopted, then superseded") {
		t.Fatalf("fixture is wrong: the destination tip must NOT carry the adopted line any more")
	}

	dpRebaseAssertDelivered(t, db, "dp-content-e", repo, ground,
		"E: the landing commit is behind the destination's path-history window")
}

// ARM D — the work is genuinely NOT at the destination (row 01a17abb's shape):
// the destination carries a SMALLER version of the same file, so the run's added
// lines are absent. This arm must be REFUSED with all arms ON, and the refusal
// must say WHICH lines are missing and WHICH destination commit came closest.
func TestDeliveryProofContent_TrulyUndeliveredWorkIsStillRefused(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, repo := dpRebaseChannel(t)
	// The ground adds four functions; the destination gets only the first two —
	// the measured shape of a ground whose work master has not got.
	body := "package x\n\nfunc A() {}\n\nfunc B() {}\n\nfunc C() {}\n\nfunc D() {}\n"
	dpRebaseCommitWork(t, ground, body)
	dpRebaseAdopt(t, ground, "", repo, body, func(s string) string {
		return "package x\n\nfunc A() {}\n\nfunc B() {}\n"
	})

	id := "dp-content-d"
	err := dpRebaseRunWrite(t, db, id, repo, ground)
	if err == nil {
		t.Fatalf("UNDELIVERED work must stay refused: the destination carries only A() and B() while the run added C() and D()")
	}
	if !strings.Contains(err.Error(), deliveryProofMarker) {
		t.Fatalf("the refusal must carry the marker %q, got: %v", deliveryProofMarker, err)
	}
	// THE REFUSAL MUST SAY WHERE IT DIFFERS — the half that tells a reader a real
	// gap from a re-derived base.
	for _, want := range []string{
		deliveryProofClassLinesMiss,
		"func C() {}",
		"closest",
		dpRebaseWorkPath,
		"content comparison",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q so a reader can see WHAT is missing; got: %v", want, err)
		}
	}
	if got := dpRowStatus(t, db, id); got != taskStatusInProgress {
		t.Errorf("an undelivered row must stay OPEN, got %q", got)
	}
	if n := dpEventCount(t, db, id, deliveryProofOwedEvent); n == 0 {
		t.Errorf("the refusal must leave a %s trail entry", deliveryProofOwedEvent)
	}
	// And with the arm OFF it is refused for a different reason — the arm is not
	// what refuses it, it is what keeps A/B/C from being refused.
	dpRebaseAssertRefusedWithoutTheArm(t, db, "dp-content-d", repo, ground, "D: genuinely undelivered")
}

// TestDeliveryProofContent_NormaliserKeepsTheWork pins the normaliser's OWN
// boundary (Rule 1: one spelling): it drops blank, comment-only, trailing-space
// and `measured-at:` lines, and keeps every line that carries work. A normaliser
// that dropped too much would let arm D pass — that is the failure this arm
// guards.
func TestDeliveryProofContent_NormaliserKeepsTheWork(t *testing.T) {
	text := strings.Join([]string{
		"package x",
		"",
		"// a comment",
		"# measured-at: deadbeef",
		"\treturn 1   ",
		"\treturn 2",
	}, "\n")
	got := deliveryProofNormalizeLines(text)
	want := []string{"package x", "return 1", "return 2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("normaliser = %q, want %q (blank/comment/metadata dropped, trailing space trimmed, work KEPT)",
			got, want)
	}
	// THE ANCHOR (measured 2026-10-28): a WORK line that merely NAMES the
	// marker is work, and it used to be dropped by a `strings.Contains` test —
	// which would have judged a commit adding it content-free at the
	// destination, i.e. delivered by nothing. Every one of these must survive
	// while the anchored census headers keep being dropped.
	for _, work := range []string{
		"deliveryProofMetadataMarker = \"measured-at:\"", // a Go constant naming it
		"prefix measured-at: tail",                       // prose mentioning it
		"x := \"# measured-at: \"",                       // code building the header
	} {
		if kept := deliveryProofNormalizeLines(work); len(kept) != 1 || kept[0] != strings.TrimSpace(work) {
			t.Errorf("normaliser dropped the WORK line %q (got %q) — the metadata test must be anchored, "+
				"or a commit adding it is judged content-free at the destination", work, kept)
		}
	}
	for _, meta := range []string{
		"# measured-at: deadbeef",   // the census header every table writes
		"measured-at: 2026-10-28",   // a bare YAML/TOML metadata row
		"  # measured-at: 33a990cb", // indented, as a table nests it
	} {
		if kept := deliveryProofNormalizeLines(meta); len(kept) != 0 {
			t.Errorf("normaliser kept the metadata line %q (got %q) — the anchored shapes must still be "+
				"dropped, or a rebase that rewrites the measured base is refused", meta, kept)
		}
	}
	if _, err := TaskDeliveryProof(""); err == nil {
		t.Errorf("TaskDeliveryProof must refuse an empty task id rather than judge nothing (Rule 15)")
	}
	_ = context.Background()
}

// ARM E — the REAL undelivered row, measured at the moment it WAS undelivered.
//
// THE BRIEFING'S PREMISE HAS EXPIRED, and that is itself the finding this arm
// records. Row 01a17abb was reported as "truly undelivered: master is missing 4
// functions and a 235-line red proof". Measured 2026-10-27 23:1x, that is no
// longer true: the ground's four commits were ADOPTED onto master under other
// shas, with the ground's own timestamps —
//
//	$ git -C <ground> log --format='%h %ad %s' --date=iso <cut>..HEAD
//	ec6383f 2026-10-27 23:08:04 +0800 fix(delivery-proof): the row's address has TWO spellings…
//	489bc1a 2026-10-27 22:55:21 +0800 fix(delivery-proof): the landed record names the row's ADDRESS…
//	572673f 2026-10-27 09:58:11 +0800 test(agent): the landed-record reader opens the ledger…
//	e0b1726 2026-10-27 08:57:31 +0800 fix(delivery-proof): a rebase that re-derived the content…
//	$ git -C /home/nick/work/gopi log -1 --format='%h %ad %s' --date=iso d010dfd0a
//	d010dfd0a 2026-10-27 23:08:04 +0800 fix(delivery-proof): the row's address has TWO spellings…
//	$ git -C /home/nick/work/gopi grep -c "func CloneDeliveryTip" 6c752f6 -- 'internal/tools/*.go'
//	6c752f6:internal/tools/task_delivery_proof.go:1
//	$ git -C /home/nick/work/gopi show 6c752f6:internal/tools/zz_redproof_delivery_proof_rebase_test.go | wc -l
//	235
//
// So walking that ground TODAY must answer DELIVERED — refusing it would be the
// false refusal this whole criterion repairs. What must stay refused is the same
// ground measured against master AS IT WAS BEFORE THE ADOPTION (`<adoption>^`),
// which is the state the briefing actually described. That is a real repository
// state, not a fixture: the same ground, the same master history, the right
// moment.
//
// The test SKIPS when those real paths are absent (a different host, a fresh
// machine): the real-tree evidence is worth having where it exists, and a skip is
// honest where the tree is not there. The load-bearing arm for "still refused"
// is ARM D, which needs no real tree at all.
func TestDeliveryProofContent_RealUndeliveredGroundRefusedAtItsPreAdoptionTip(t *testing.T) {
	const (
		realGround   = "/home/nick/workspace/01a17abb-7de0-7479-a8a7-20340f489b5d"
		realRepo     = "/home/nick/work/gopi"
		adoptionHead = "fix(delivery-proof): the row's address has TWO spellings, and the reader must accept both"
	)
	ctx := context.Background()
	if _, err := os.Stat(realGround); err != nil {
		t.Skipf("real ground %s is not on this host: %v (ARM D carries the refusal without it)", realGround, err)
	}
	if _, err := os.Stat(realRepo); err != nil {
		t.Skipf("real repo %s is not on this host: %v", realRepo, err)
	}
	adoption := deliveryProofGitRead(ctx, realRepo, "log", "--format=%H", "-n", "1",
		refsHeadsPrefix+masterBranchName, "--grep", adoptionHead)
	if adoption == "" {
		t.Skipf("master carries no adoption commit titled %q — the history this arm measures is not here", adoptionHead)
	}
	preAdoption := adoption + "^"
	tip := deliveryProofGitRead(ctx, realGround, "rev-parse", "HEAD")
	if tip == "" {
		t.Fatalf("%s exists but git cannot read its HEAD", realGround)
	}

	// (1) TODAY: the same ground against the current destination must be
	// DELIVERED — its content was adopted. Anything else is a false refusal.
	today := deliveryProofGitRead(ctx, realRepo, "rev-parse", refsHeadsPrefix+masterBranchName)
	landed, why, judged := deliveryProofContentLanded(ctx, realGround, realRepo, today, "", tip)
	if !judged {
		t.Fatalf("today's comparison must be judgeable (tip %s is in %s)", branchDeleteShortHash(tip), realGround)
	}
	if !landed {
		t.Errorf("the row's content IS on master (%s adopted the same four commits); refusing it would be the "+
			"false refusal this criterion repairs, got: %s", branchDeleteShortHash(adoption), why)
	}

	// (2) BEFORE THE ADOPTION: must be UNDELIVERED, and the refusal must name the
	// work that is missing — the briefing's own "4 functions + 235-line red proof".
	landed, why, judged = deliveryProofContentLanded(ctx, realGround, realRepo, preAdoption, "", tip)
	if !judged {
		t.Fatalf("the pre-adoption comparison must be judgeable")
	}
	if landed {
		t.Fatalf("at %s^ (master BEFORE the adoption) the ground's work is nowhere: this MUST be refused",
			branchDeleteShortHash(adoption))
	}
	for _, want := range []string{deliveryProofClassLinesMiss, deliveryProofClassAbsent} {
		if !strings.Contains(why, want) {
			t.Errorf("the refusal must classify the gap as %q so a reader sees WHAT is missing, got: %s", want, why)
		}
	}
	if !strings.Contains(why, dpRebaseWorkProofPath) && !strings.Contains(why, "task_delivery_proof.go") {
		t.Errorf("the refusal must name the file the work is missing from, got: %s", why)
	}
}

// dpRebaseWorkProofPath is the path the briefing's red proof names, used above to
// assert the refusal quotes a real missing file rather than a generic sentence.
const dpRebaseWorkProofPath = "zz_redproof_delivery_proof_rebase_test.go"

// ARM F — THE `*` SHAPE, both directions at once, because they are one rule.
//
// The normaliser used to carry a bare `"*"` in deliveryProofCommentPrefixes, and
// the byte means OPPOSITE things depending on what follows it:
//
//	*state += chatBannerSeedMixA      a deref assignment — WORK
//	 * a block comment continuation   inside `/* … */` — not work
//
// Dropping the first kind (measured: 72 such lines in this tree) made a commit
// whose additions are ALL deref lines normalise to ZERO lines, i.e. `content-free`
// by the normaliser's own definition, i.e. LANDED at every destination without a
// comparison — the false-positive direction. Removing `"*"` outright, however,
// would keep every block-comment continuation as "work", so a rebase that
// reflows a Go block comment would be refused as undelivered — the
// false-negative direction this whole arm exists to prevent. So the arm pins
// BOTH halves, and the work lines below are copied from this tree rather than
// invented.
func TestDeliveryProofContent_StarShapeKeepsDerefWorkAndDropsBlockComments(t *testing.T) {
	// (1) THE LIST ITSELF: one spelling, and no bare marker in it — the list can
	// only judge a line by its PREFIX, and `*` is decided by its suffix.
	for _, prefix := range deliveryProofCommentPrefixes {
		if prefix == deliveryProofBlockCommentStar {
			t.Fatalf("the bare %q is back in deliveryProofCommentPrefixes: it drops deref WORK lines, and a "+
				"commit that adds only them is judged content-free (delivered by nothing)", prefix)
		}
	}

	// (2) DEREF / POINTER ASSIGNMENTS SURVIVE — real lines, real files.
	for _, work := range []string{
		"*state += chatBannerSeedMixA",  // cmd/agent/chat_banner.go:1355
		"\t*hits = append(*hits, body)", // cmd/agent/chat_input_classifier_test.go:309
		"*dst = err",                    // cmd/agent/chat_once.go:695
		"**p = 1",                       // a double deref
		"*(ptr) = 2",                    // a deref whose operand is parenthesised
	} {
		kept := deliveryProofNormalizeLines(work)
		want := strings.TrimSpace(work)
		if len(kept) != 1 || kept[0] != want {
			t.Errorf("the normaliser dropped the DEREF WORK line %q (got %q) — that is the false-positive "+
				"direction: a commit adding only such lines is judged content-free at the destination", work, kept)
		}
	}

	// (3) BLOCK-COMMENT CONTINUATIONS ARE STILL DROPPED — a reflowed `/* … */`
	// must not refuse a delivered row.
	for _, comment := range []string{
		"* a block comment continuation", // the ordinary reflowed line
		"*\tan indented continuation",    // tab-indented, as gofmt produces
		"*",                              // the marker alone: the close of a star-banner
		"*/",                             // a lone close marker
		"/* comment */",                  // a one-line block comment
	} {
		if kept := deliveryProofNormalizeLines(comment); len(kept) != 0 {
			t.Errorf("the normaliser KEPT the comment line %q (got %q) — a rebase that reflows a block "+
				"comment would then be refused as undelivered (the false-negative direction)", comment, kept)
		}
	}

	// (4) THE PREDICATE ITSELF, at its boundary: shape, not position in a file.
	for line, want := range map[string]bool{
		deliveryProofBlockCommentStar:                                            true,
		deliveryProofBlockCommentStar + deliveryProofBlockCommentGap + "x":       true,
		deliveryProofBlockCommentStar + deliveryProofBlockCommentIndentGap + "x": true,
		deliveryProofBlockCommentStar + "p = 1":                                  false,
		deliveryProofBlockCommentStar + "(p) = 1":                                false,
		deliveryProofBlockCommentStar + "*p = 1":                                 false,
	} {
		if got := deliveryProofIsBlockCommentContinuation(line); got != want {
			t.Errorf("deliveryProofIsBlockCommentContinuation(%q) = %v, want %v", line, got, want)
		}
	}
}

// ARM G — END TO END, the direction that matters: a commit whose ONLY additions
// are deref lines must NOT be judged content-free at a destination that lacks
// them.
//
// The fixture is the measured shape: the destination carries the run's BASE file
// (so the refusal cannot come from an earlier commit), the run's second commit
// adds two deref assignments and nothing else, and the destination has neither.
// With the shape rule in force the two added lines are WORK, they are absent, and
// the write is REFUSED with them named. Under the legacy bare-`"*"` rule the same
// commit normalises to zero lines, the `len(added) == 0` branch declares it
// content-free, and the row lands `completed` — the defect reproduced on demand
// by the switch below, which is what proves the rule is doing the work.
func TestDeliveryProofContent_DerefOnlyCommitIsNotContentFreeAtTheDestination(t *testing.T) {
	db := testutil.NewTestDBPath(t)
	ground, _, repo := dpRebaseChannel(t)

	// Commit 1: the file the run's work lives in — and the destination gets the
	// SAME content, so only commit 2 can be the reason for a refusal.
	base := "package x\n\nfunc bump(buf *int, hits *[]string) {\n}\n"
	dpRebaseCommitWork(t, ground, base)
	dpRebaseAdopt(t, ground, "", repo, base, nil)

	// Commit 2: NOTHING but deref assignments are added.
	const (
		dpDerefAdd  = "\t*buf = *buf + 1"
		dpDerefHits = "\t*hits = append(*hits, \"x\")"
	)
	derefOnly := base[:strings.Index(base, "}\n")] + dpDerefAdd + "\n" + dpDerefHits + "\n}\n"
	if err := os.WriteFile(filepath.Join(ground, dpRebaseWorkPath), []byte(derefOnly), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuildTestGit(t, ground, "add", dpRebaseWorkPath)
	rebuildTestGit(t, ground, "commit", "-q", "-m", "fix: the deref updates")
	derefTip := rebuildTestGit(t, ground, "rev-parse", "HEAD")

	// The additions of THAT commit are exactly the two deref lines — the premise
	// of the whole arm, measured from git rather than assumed.
	added := deliveryProofCommitAddedLines(context.Background(), ground, derefTip, dpRebaseWorkPath)
	if strings.Join(added, "|") != strings.TrimSpace(dpDerefAdd)+"|"+strings.TrimSpace(dpDerefHits) {
		t.Fatalf("fixture is wrong: the deref-only commit must add exactly the two deref lines, got %q", added)
	}

	// (1) WITH THE SHAPE RULE: REFUSED, and the refusal names the work that is
	// missing — not a generic sentence.
	id := "dp-content-g"
	err := dpRebaseRunWrite(t, db, id, repo, ground)
	if err == nil {
		t.Fatalf("a commit adding ONLY deref lines must be refused where the destination lacks them: "+
			"treating it as content-free marks the row DELIVERED with no comparison at all (%s)", derefTip)
	}
	if !strings.Contains(err.Error(), deliveryProofMarker) {
		t.Fatalf("the refusal must carry the marker %q, got: %v", deliveryProofMarker, err)
	}
	for _, want := range []string{deliveryProofClassLinesMiss, strings.TrimSpace(dpDerefAdd), dpRebaseWorkPath} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q so a reader sees WHICH work is missing; got: %v", want, err)
		}
	}
	if got := dpRowStatus(t, db, id); got != taskStatusInProgress {
		t.Errorf("the row must stay OPEN, got %q", got)
	}

	// (2) AND THE DEFECT, REPRODUCED IN THE TEST ITSELF: under the legacy rule (the
	// bare `"*"` in the prefix list) these very lines normalise AWAY, so the
	// commit is content-free and lands with no comparison. The legacy rule is
	// rebuilt here rather than kept alive in production: a switch that can
	// reintroduce a defect is a defect with an escape hatch (Rule 35).
	legacyPrefixes := append(append([]string{}, deliveryProofCommentPrefixes...), deliveryProofBlockCommentStar)
	for _, line := range []string{dpDerefAdd, dpDerefHits} {
		dropped := false
		for _, prefix := range legacyPrefixes {
			if strings.HasPrefix(strings.TrimSpace(line), prefix) {
				dropped = true
				break
			}
		}
		if !dropped {
			t.Fatalf("this arm's premise is wrong: the legacy bare-%q rule does NOT drop %q, so the arm is not "+
				"measuring the defect it claims", deliveryProofBlockCommentStar, line)
		}
	}
	// The file-level red proof (revert the shape predicate, re-run, restore)
	// turns THIS arm red — that is the gate, and it cannot be reached by a switch.
}

// dpRebaseAssertDeliveredByThePatchArmAlone proves the OTHER half of case A's new
// shape: with the CONTENT leg off — nothing but ancestry and `git cherry` left —
// the fixture still closes, because the pre-judgement refresh made the patch
// comparison able to answer at all. With the refresh off this very fixture is
// reported as unmeasurable (the arm below), so these two pin the repair's two
// directions: the refreshed view makes the criterion answerable, and an
// unrefreshed one degrades to `unverified` rather than to a refusal.
func dpRebaseAssertDeliveredByThePatchArmAlone(t *testing.T, dbPath, id, repo, ground string) {
	t.Helper()
	deliveryProofContentArmOff = true
	defer func() { deliveryProofContentArmOff = false }()
	err := dpRebaseRunWrite(t, dbPath, id, repo, ground)
	if err != nil {
		t.Fatalf("with the content leg OFF the refreshed patch leg must still recognise the run's patch as present "+
			"(a rewritten commit, the same patch), got a refusal: %v", err)
	}
	if got := dpRowStatus(t, dbPath, id); got != taskStatusCompleted {
		t.Errorf("row status = %q, want %q", got, taskStatusCompleted)
	}
}
