package tools

// task_briefing_master_reading_test.go — the "which master" clause's regression
// guard (epic 01a13af7, measured 2026-10-14 on row 01a13524's own close-out).
//
// THE DEFECT THESE TESTS PIN. A briefing's acceptance criterion said «use
// master's version», and the deliverable's verdict flipped with the ref the
// reader held: in ONE tree, three different `devdoc/DOC-INDEX.tsv` blobs
// answered to "master" — refs/heads/master (2502 lines), the peer mirror
// (2533), the ground's cut commit (2540) — with the local ref 183 commits
// behind. The seam rendered the integration branch as a bare NAME and never as
// the ref+sha it denotes, so nothing in the child's briefing said which of the
// two readings the criterion had been measured on.
//
// WHAT IS ASSERTED, and why each half is needed:
//
//	① RED — on a repository whose local integration ref is BEHIND the mirrored
//	   peer tip, the clause names BOTH full ref names, BOTH tips, and the commit
//	   distance between them, and it states that they are not the same commit.
//	   The distance is asserted as a MEASUREMENT (the same count read
//	   independently in the test), never as a number the clause typed.
//
//	② POSITIVE CONTROL — on a repository whose local integration ref IS the
//	   mirror's tip, the clause must NOT report a lag. A guard that stamped
//	   "N behind" on every tree would be an alarm the reader learns to ignore,
//	   and this arm is what makes ① mean something.
//
//	③ UNMEASURED — a tree where the refs cannot be read renders a clause that
//	   NAMES the failure AND forbids the exact substitution that produced this
//	   epic: reading refs/heads/master as though it were the only "master".
//	   (Rule 15: a record that cannot name its subject says so.)
//
// The clause is ADDITIVE and the payload's own prose is untouched, so no test
// here asserts anything about the briefing text — only about what gopi's own
// appended clause says.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// masterReadingFixture is the repository the arms measure against.
type masterReadingFixture struct {
	repo string // the tree the clause is read in
	tip  string // the mirror ref's tip (the peer's integration tip)
}

// masterReadingTestRepo builds a real repository shaped like a client host: a
// local integration branch that STOPPED EARLY, and the sync lane's mirror ref
// carrying the commits the peer has since taken — which is the measured host
// condition (183 commits on 2026-10-14, 249 on this run's own ground).
//
// behindBranch is the number of commits the mirror carries on top of the local
// integration ref: 0 builds the POSITIVE CONTROL's shape (the two readings are
// one commit).
func masterReadingTestRepo(t *testing.T, behind int) masterReadingFixture {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=masterreading", "GIT_AUTHOR_EMAIL=masterreading@test",
			"GIT_COMMITTER_NAME=masterreading", "GIT_COMMITTER_EMAIL=masterreading@test",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", IntegrationBranchName)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "base")
	// The local integration ref stops here; the peer moves on without this host.
	for i := 0; i < behind; i++ {
		name := filepath.Join(repo, fmt.Sprintf("peer-%d.txt", i))
		if err := os.WriteFile(name, []byte("peer\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		run("add", filepath.Base(name))
		run("commit", "-q", "-m", fmt.Sprintf("peer commit %d", i))
	}
	tip := run("rev-parse", "HEAD")
	// The mirror ref the sync lane maintains, then the local ref put back to the
	// commit it had before the peer moved on.
	run("update-ref", gitPushbackCheckRef, tip)
	run("update-ref", refsHeadsPrefix+IntegrationBranchName, tip+"~"+strconv.Itoa(behind))
	return masterReadingFixture{repo: repo, tip: tip}
}

// TestMasterReadingClauseNamesBothRefsAndTheLag is ①: the RED arm.
func TestMasterReadingClauseNamesBothRefsAndTheLag(t *testing.T) {
	const behind = 3
	f := masterReadingTestRepo(t, behind)

	clause, record := TaskBriefingMasterReadingClause(f.repo)
	if clause == "" {
		t.Fatalf("a readable tree must render a master-reading clause")
	}
	if !strings.HasPrefix(clause, MasterReadingLabel) {
		t.Errorf("the clause must carry its label %q so a reader can address it by name:\n%s", MasterReadingLabel, clause)
	}

	// The two FULL ref names — the whole point is that a bare branch name is not
	// enough. Both are rendered from the constants that define them, so the test
	// asserts the REFS a reader greps for, not a spelling typed here.
	localRef := refsHeadsPrefix + IntegrationBranchName
	for _, want := range []string{localRef, gitPushbackCheckRef} {
		if !strings.Contains(clause, want) {
			t.Errorf("the clause must name the ref %q in full — a criterion that says only %q cannot be settled:\n%s",
				want, IntegrationBranchName, clause)
		}
	}

	// The two TIPS, each read independently in the test so the clause is compared
	// with a measurement rather than with itself.
	localSHA := masterReadingTestGit(t, f.repo, gitRevParseVerb, localRef)
	if !strings.Contains(clause, shortSha(localSHA)) {
		t.Errorf("the clause must name %s's tip %s:\n%s", localRef, shortSha(localSHA), clause)
	}
	if !strings.Contains(clause, shortSha(f.tip)) {
		t.Errorf("the clause must name %s's tip %s:\n%s", gitPushbackCheckRef, shortSha(f.tip), clause)
	}

	// The DISTANCE, likewise measured here rather than trusted.
	wantLag := masterReadingTestGit(t, f.repo, integrationClaimRevListVerb, integrationClaimCountFlag,
		localRef+integrationClaimRelativeRefspecSeparator+gitPushbackCheckRef)
	if wantLag != strconv.Itoa(behind) {
		t.Fatalf("FIXTURE: the local ref must be %d behind the mirror; git says %s", behind, wantLag)
	}
	if !strings.Contains(clause, wantLag) {
		t.Errorf("the clause must state the measured lag (%s) — that number is what makes the criterion unambiguous:\n%s", wantLag, clause)
	}
	// And it must say in words that the two readings DIFFER, not merely print two
	// shas a reader has to compare character by character.
	if !strings.Contains(clause, "NOT the same commit") {
		t.Errorf("the clause must state that the two readings are different commits:\n%s", clause)
	}

	// The record carries the MEASUREMENT (both refs, both tips, the distance) and
	// is the artifact an evidence reader greps.
	for _, wantField := range []string{localRef, gitPushbackCheckRef, shortSha(localSHA), shortSha(f.tip), wantLag} {
		if !strings.Contains(record, wantField) {
			t.Errorf("the record must carry %q (the measurement, not a verdict); got:\n%s", wantField, record)
		}
	}
	t.Logf("measured on the fixture (%d behind): `%s` clause = %q", behind, MasterReadingLabel, firstClauseLine(clause))
}

// TestMasterReadingClauseDoesNotClaimALagWhenTheReadingsAgree is ②: the positive
// control. A guard that stamped a lag on every tree would be noise, and this arm
// is what makes the RED arm above mean something.
func TestMasterReadingClauseDoesNotClaimALagWhenTheReadingsAgree(t *testing.T) {
	f := masterReadingTestRepo(t, 0)

	clause, record := TaskBriefingMasterReadingClause(f.repo)
	if clause == "" {
		t.Fatalf("a readable tree must render a master-reading clause")
	}
	if strings.Contains(clause, "NOT the same commit") {
		t.Errorf("the clause must NOT report a difference when both refs name one commit:\n%s", clause)
	}
	if !strings.Contains(clause, "AT "+gitPushbackCheckRef) {
		t.Errorf("the clause must state positively that the local ref is AT the mirror's tip:\n%s", clause)
	}
	// The lag it prints is the MEASURED one (0), never a fabricated number, and
	// the record agrees with the clause.
	if got := MasterReading(f.repo); !got.Aligned() || got.Behind != gitSyncLagAlignedValue {
		t.Fatalf("the fixture must read as aligned with a 0 lag; got behind=%q note=%q evidence=%q", got.Behind, got.Note, got.Evidence)
	}
	if strings.Contains(record, gitSyncLagUnknown) {
		t.Errorf("an aligned pair must not be recorded as unmeasurable; got:\n%s", record)
	}
}

// TestMasterReadingClauseStatesTheUnreadPairAndForbidsTheFallback is ③: the arm
// that keeps an unread tree from silently becoming "the local master, then" —
// the exact substitution that produced this epic.
func TestMasterReadingClauseStatesTheUnreadPairAndForbidsTheFallback(t *testing.T) {
	// A directory that is not a repository at all: neither ref resolves, and the
	// distance between them cannot be counted.
	repo := t.TempDir()

	read := MasterReading(repo)
	if read.Measured() {
		t.Fatalf("a non-repository must not read as a measured pair; got local=%q tip=%q behind=%q", read.LocalSHA, read.TipSHA, read.Behind)
	}
	if read.Note == "" || strings.TrimSpace(read.Evidence) == "" {
		t.Errorf("an unread pair must be STATED with its evidence; got note=%q evidence=%q", read.Note, read.Evidence)
	}

	clause, record := TaskBriefingMasterReadingClause(repo)
	if clause == "" {
		t.Fatalf("an unreadable tree must still render a clause — the run must be told what was NOT measured")
	}
	if !strings.Contains(clause, "could NOT be read") {
		t.Errorf("the clause must say the refs were not read:\n%s", clause)
	}
	if !strings.Contains(clause, "do NOT fall back") {
		t.Errorf("the clause must explicitly forbid substituting the local ref — that substitution IS this epic:\n%s", clause)
	}
	if !strings.Contains(record, gitSyncLagUnknown) {
		t.Errorf("the record must never let an unread pair print as an empty (i.e. aligned) reading; got:\n%s", record)
	}

	// No tree at all ⇒ no clause and no record: a clause naming no subject is
	// worse than none (Rule 15), the same contract the sibling seams keep.
	if c, r := TaskBriefingMasterReadingClause("  "); c != "" || r != "" {
		t.Errorf("an empty repo ⇒ no clause and no record, got clause=%q record=%q", c, r)
	}
}

// masterReadingTestGit runs ONE git query in dir and returns its stdout.
func masterReadingTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=masterreading", "GIT_AUTHOR_EMAIL=masterreading@test",
		"GIT_COMMITTER_NAME=masterreading", "GIT_COMMITTER_EMAIL=masterreading@test",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return strings.TrimSpace(string(out))
}

// firstClauseLine keeps a failure message readable: the clause's opening line.
func firstClauseLine(clause string) string {
	line, _, _ := strings.Cut(clause, "\n")
	return line
}

// TestMasterReadingClauseStatesTheMirrorIsARefNotARemote is the arm the row's
// own premise produced (measured 2026-10-17, row 01a14877 on host-a): the
// clause's ref table named refs/remotes/gitPushcheck/master and nothing about
// that name being a NAMESPACE rather than a configured remote, so briefings
// built from it told executors to run `git fetch gitPushcheck` — which that tree
// answers with `fatal: 'gitPushcheck' does not appear to be a git repository`
// (exit 128), while the ref itself is perfectly usable for an ancestry probe.
//
// THREE ARMS, and the positive control is what makes the red arm mean anything:
//
//	① NO REMOTE UNDER THAT NAME (the measured host shape: one remote, `peerbox`):
//	   the clause must say the name is NOT a remote, quote git's own sentence and
//	   its exit status, name the remotes the tree DOES have, and render a refresh
//	   command built from the lane's own refspec — reaching the ref without
//	   `git fetch gitPushcheck`.
//	② THE SAME NAME AS A REAL REMOTE (positive control): the clause must render
//	   NO such warning — otherwise the arm is noise every reader learns to skip.
//	③ NO REMOTE AT ALL (empty, known list): no runnable refresh exists, so the
//	   clause must say so and must NOT print a refresh command.
func TestMasterReadingClauseStatesTheMirrorIsARefNotARemote(t *testing.T) {
	const peerRemote = "peer-under-test"
	t.Run("no-remote-under-the-mirror-name", func(t *testing.T) {
		f := masterReadingTestRepo(t, 2)
		runGitIn(t, f.repo, "remote", "add", peerRemote, f.repo)

		read := MasterReading(f.repo)
		if !read.RemotesKnown {
			t.Fatalf("the fixture's remotes must be readable; evidence=%q", read.Evidence)
		}
		if read.NamespaceIsRemote {
			t.Fatalf("FIXTURE: %q must NOT be a configured remote here (git remote lists %v)", gitPushbackCheckNamespace, read.Remotes)
		}

		clause, record := TaskBriefingMasterReadingClause(f.repo)
		for _, want := range []string{
			gitPushbackCheckRef,
			"is a REF",
			"`git remote` lists " + peerRemote,
			gitPushbackCheckNamespace,
			"does not appear to be a git repository",
			strconv.Itoa(mergeVerifyGitExitFatal),
			"merge-base --is-ancestor",
			gitPeerMirrorRefspec(),
		} {
			if !strings.Contains(clause, want) {
				t.Errorf("the clause must carry %q — a reader told only the ref's NAME tries `git fetch %s` (rc=%d) and reads its own tooling as broken:\n%s",
					want, gitPushbackCheckNamespace, mergeVerifyGitExitFatal, clause)
			}
		}
		// The instruction it renders must be RUNNABLE: the refspec names a ref and
		// a target that exists, never the namespace.
		// The namespace may appear ONLY as the thing being WARNED ABOUT: the
		// spelling is safe exactly when it is followed by git's own refusal
		// sentence, and unsafe when it stands as an instruction on its own.
		banned := gitPeerFetchVerb + " " + gitPushbackCheckNamespace
		for at := strings.Index(clause, banned); at >= 0; {
			tail := clause[at+len(banned):]
			if !strings.HasPrefix(tail, "` fails with") {
				t.Errorf("`%s` appears outside the warning that makes it safe (it must be followed by git's own refusal sentence, not stand as an instruction):\n%s",
					banned, clause)
				break
			}
			next := strings.Index(tail, banned)
			if next < 0 {
				break
			}
			at += len(banned) + next
		}
		if !strings.Contains(clause, gitPeerFetchVerb+" "+peerRemote+" "+gitPeerMirrorRefspec()) {
			t.Errorf("the refresh it renders must be RUNNABLE: `%s %s %s` (a remote this tree has, plus the lane's own refspec):\n%s",
				gitPeerFetchVerb, peerRemote, gitPeerMirrorRefspec(), clause)
		}
		if !strings.Contains(record, peerRemote) {
			t.Errorf("the record must carry the measured remote layout (%q), not just a verdict; got:\n%s", peerRemote, record)
		}
	})

	t.Run("the-same-name-as-a-real-remote", func(t *testing.T) {
		f := masterReadingTestRepo(t, 1)
		runGitIn(t, f.repo, "remote", "add", gitPushbackCheckNamespace, f.repo)

		if read := MasterReading(f.repo); !read.NamespaceIsRemote {
			t.Fatalf("FIXTURE: %q must read as a configured remote here; remotes=%v known=%v",
				gitPushbackCheckNamespace, read.Remotes, read.RemotesKnown)
		}
		clause, _ := TaskBriefingMasterReadingClause(f.repo)
		if strings.Contains(clause, "`git remote` lists") || strings.Contains(clause, "does not appear to be a git repository") {
			t.Errorf("on a tree where the name IS a remote the clause must not warn about it:\n%s", clause)
		}
	})

	t.Run("no-remote-at-all", func(t *testing.T) {
		f := masterReadingTestRepo(t, 1)

		read := MasterReading(f.repo)
		if !read.RemotesKnown || len(read.Remotes) != 0 {
			t.Fatalf("FIXTURE: a repo with no remote must read as known-and-empty; got known=%v remotes=%v", read.RemotesKnown, read.Remotes)
		}
		clause, _ := TaskBriefingMasterReadingClause(f.repo)
		if !strings.Contains(clause, "do NOT run") {
			t.Errorf("the clause must forbid the fetch that cannot work here:\n%s", clause)
		}
		// No REFSPEC may be printed: the clause names the command it forbids,
		// which is the point — what must be absent is a runnable refresh.
		if strings.Contains(clause, gitPeerMirrorRefspec()) {
			t.Errorf("with no remote there is no runnable refresh, so no refspec may be printed:\n%s", clause)
		}
	})
}

// runGitIn runs one git command in dir, failing the test on a non-zero exit.
func runGitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testutil.GitEnv(
		"GIT_AUTHOR_NAME=masterreading", "GIT_AUTHOR_EMAIL=masterreading@test",
		"GIT_COMMITTER_NAME=masterreading", "GIT_COMMITTER_EMAIL=masterreading@test",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}
