package tools

// rework_landing_probe_test.go — the red→green proof for the already-landed
// probe, driven against the MEASURED case rather than a fixture invented for the
// test.
//
// THE RED CASE IS THE REAL ONE. Task 01a12b85 filed a branch-rework row for
// branch tip 8fafbafee ("fix(code_search): accept a ctags index whose pseudo-tag
// block is not at byte 0"). Master already carried a commit with that EXACT
// subject (ffa1cf4f8 non-short: ffa1cf4f806ecfe40c4bc6cc0fc19c07f8ad7daa). At
// HEAD — before this change — nothing in the rework briefing could say so, so
// the test builds that same shape in a throwaway repository and asserts the
// probe fires on it. The patch-identity probes are asserted to FAIL on that same
// shape, because that is the measurement which chose the discriminator: a later
// reader who "simplifies" this probe back to `git patch-id` gets a red test, not
// a silent regression.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// reworkLandingTestSubject is the measured subject, verbatim (both the branch's
// tip and the landed commit carry it).
const reworkLandingTestSubject = "fix(code_search): accept a ctags index whose pseudo-tag block is not at byte 0"

// reworkLandingTestFileSubject is a generic, unrelated subject used for the
// silent arm.
const reworkLandingTestFileSubject = "feat(somewhere): an unrelated change nobody landed"

// reworkLandingTestDeepBurial is how many unrelated commits are laid on top of
// the landed commit in TestReworkLandingProbeFindsADeeplyBuriedLanding.
//
// The number is the regression: the probe's FIRST implementation read only the
// newest 400 commits of the integration branch, and the measured case has the
// landed commit 1313 commits below master's tip, so that window returned
// "NOT ALREADY LANDED (probe ran)" — a confident false negative. 450 commits
// (the burial plus the commits the fixture itself makes, > 400) makes the test
// red against any implementation that reintroduces a recency window.
const reworkLandingTestDeepBurial = 450

// reworkLandingTestOldWindow is the recency window the probe's first
// implementation scanned. Kept ONLY so the regression test can assert its
// fixture buries the landing deeper than that window.
const reworkLandingTestOldWindow = 400

// reworkLandingTestBuriedSubject is a distinct real-shaped subject for the
// deep-burial case, so the match cannot come from the other fixture's commits.
const reworkLandingTestBuriedSubject = "fix(deep): a change buried far below the integration tip"

// reworkLandingTestRepo builds a repository carrying the measured shape:
//
//	master  ← h1 (unrelated) ← LANDED (the subject above) ← h3 (unrelated)
//	branch  ← h1' (a DIFFERENT patch, SAME subject as LANDED)
//
// The branch's patch differs from LANDED's (an extra line, so patch-ids differ),
// exactly like the measured pair: the fixes were written twice.
func reworkLandingTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(subject string) string {
		t.Helper()
		run("add", ".")
		run("commit", "-m", subject)
		return strings.TrimSpace(run("rev-parse", "HEAD"))
	}

	run("init", "-q", "-b", IntegrationBranchName)
	write("seed.txt", "seed\n")
	commit("chore: seed the fixture repository")

	// The branch forks HERE: it is cut before the landed commit exists, so the
	// landed commit can never be an ancestor of the branch tip.
	run("checkout", "-q", "-b", "task/rework-target")
	write("symbol.go", "package p\n\n// branch's own fix\nfunc A() {}\n")
	write("branch-extra.txt", "the branch's own extra line (so the patch-id differs)\n")
	tip := commit(reworkLandingTestSubject)

	run("checkout", "-q", IntegrationBranchName)
	write("symbol.go", "package p\n\n// the landed fix\nfunc A() {}\n")
	// THE CONTENT-CARRYING HALF. The branch's own extra line is ALSO written at
	// the landing, so the landed commit CARRIES the branch's change (plus a file
	// of its own) — the measured shape: the same fix, written twice, absorbed onto
	// the integration branch. Without this, the pair is a subject match whose
	// content never arrived, which the content corroboration now (correctly)
	// reports as a NAME COLLISION rather than a landing — see
	// TestReworkLandingProbeRefutesSubjectMatchWithoutTheContent.
	write("branch-extra.txt", "the branch's own extra line (so the patch-id differs)\n")
	landed := commit(reworkLandingTestSubject)

	// h3 keeps master's tip past the landed commit, so the probe must FIND the
	// match rather than reading only the tip.
	write("later.txt", "later work on master\n")
	commit("feat(later): unrelated work on master after the landing")

	// PRECONDITION: the two commits' patch-ids DIFFER and `git cherry` calls the
	// tip patch-unique — the measured reason this probe is subject-based. If this
	// ever stops holding, the fixture no longer models the case.
	if patchID(t, dir, tip) == patchID(t, dir, landed) {
		t.Fatalf("fixture is wrong: tip %s and landed %s share a patch-id, so the measured case is not reproduced", tip, landed)
	}
	if cherry := run("cherry", landed, tip); !strings.HasPrefix(strings.TrimSpace(cherry), "+") {
		t.Fatalf("fixture is wrong: `git cherry` must report the tip %s as patch-unique (+), got %q", tip, cherry)
	}
	// The fixture's master must actually stay on IntegrationBranchName at the
	// final checkout, because the probe reads refs/heads/<IntegrationBranchName>.
	run("checkout", "-q", IntegrationBranchName)
	return dir
}

// patchID is the patch-identity probe this file deliberately does NOT use, kept
// here so the fixture can prove the measured pair is reproduced.
func patchID(t *testing.T, dir, rev string) string {
	t.Helper()
	show := exec.Command("git", "show", rev, "--format=")
	show.Dir = dir
	piped, err := show.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	id := exec.Command("git", "patch-id", "--stable")
	id.Dir = dir
	id.Stdin = piped
	if err := show.Start(); err != nil {
		t.Fatal(err)
	}
	out, err := id.Output()
	if err != nil {
		t.Fatalf("patch-id for %s: %v", rev, err)
	}
	if err := show.Wait(); err != nil {
		t.Fatalf("git show %s: %v", rev, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		t.Fatalf("patch-id for %s printed nothing", rev)
	}
	return fields[0]
}

// TestReworkLandingProbeFiresOnSubjectEquality is the RED→GREEN core: at HEAD
// the probe does not exist, so this test does not compile — the red proof. With
// it, the measured shape yields a signal naming the landed commit.
func TestReworkLandingProbeFiresOnSubjectEquality(t *testing.T) {
	repo := reworkLandingTestRepo(t)
	branch := "task/rework-target"

	ev := ProbeReworkLanding(repo, branch)
	if ev.Err != "" {
		t.Fatalf("probe must answer on a healthy repo, got Err=%q", ev.Err)
	}
	if !ev.Signal() {
		t.Fatalf("the probe must fire on the measured shape (same subject, different patch): %+v", ev)
	}
	if !ev.LandedDistinct {
		t.Fatalf("the landed match must be a DIFFERENT commit from the tip: %+v", ev)
	}
	if ev.Subject != reworkLandingTestSubject || ev.LandedSubject != reworkLandingTestSubject {
		t.Fatalf("both sides of the match must be the measured subject: %+v", ev)
	}
	if ev.Tip == ev.Landed {
		t.Fatalf("tip and landed must differ: %+v", ev)
	}
	// The landed sha must be a REAL commit on the integration branch: resolve it
	// through the same runner git uses, so a fabricated sha cannot pass.
	cmd := exec.Command("git", "-C", repo, "log", "-1", "--format=%H", ev.Landed)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the reported landed sha %s does not resolve: %v", ev.Landed, err)
	}
	if strings.TrimSpace(string(out)) != ev.Landed {
		t.Fatalf("the reported landed sha %s is not the commit it names", ev.Landed)
	}
	// The reported sha must NOT be reachable from the branch tip (it is a
	// different commit, not an ancestor of the branch).
	anc := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", ev.Landed, ev.Tip)
	if err := anc.Run(); err == nil {
		t.Fatalf("the landed commit %s must not be an ancestor of the branch tip %s", ev.Landed, ev.Tip)
	}

	section := ev.Section()
	if !strings.Contains(section, reworkLandingTestSubject) {
		t.Fatalf("the section must name the subject it matched on:\n%s", section)
	}
	if !strings.Contains(section, shortSha(ev.Landed)) {
		t.Fatalf("the section must name the landed commit %s:\n%s", shortSha(ev.Landed), section)
	}
	// The section must SAY which discriminator it used, so the executor does not
	// re-run the probe that does not work.
	for _, want := range []string{"git patch-id", "git cherry", "SUBJECT"} {
		if !strings.Contains(section, want) {
			t.Fatalf("the section must name %q as (non-)discriminator:\n%s", want, section)
		}
	}
}

// TestReworkLandingProbeFindsADeeplyBuriedLanding is the regression for the
// probe's FIRST implementation, which read only the newest 400 commits of the
// integration branch. Measured on zbook: the landed commit of the case this file
// exists for (ffa1cf4f8) is 1313 commits below master's tip, so that window
// returned "NOT ALREADY LANDED (probe ran)" — a confident FALSE NEGATIVE on the
// probe's own case. This test buries the equivalent commit deeper than any such
// window, so reintroducing one turns this test red.
func TestReworkLandingProbeFindsADeeplyBuriedLanding(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	// commit writes ONE file and commits it. The file matters: two EMPTY commits
	// with the same parent, message and timestamps collapse to the SAME sha, so an
	// empty-commit fixture would compare a commit to itself and quietly stop
	// modelling the case (this test was written that way first and caught it).
	commit := func(file, body, subject string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		run("add", file)
		run("commit", "-q", "-m", subject)
		return strings.TrimSpace(run("rev-parse", "HEAD"))
	}

	run("init", "-q", "-b", IntegrationBranchName)
	commit("seed.txt", "seed\n", "chore: seed the fixture repository")

	// The branch forks below the burial, so the landed commit can never be an
	// ancestor of the tip. Its content differs from the landing's — same subject,
	// DIFFERENT patch, which is the measured shape.
	run("checkout", "-q", "-b", "task/buried")
	tip := commit("branch.txt", "the branch's own version of the fix\n", reworkLandingTestBuriedSubject)

	// The equivalent commit lands NOW, on the integration branch, and it CARRIES
	// the branch's change (the same file, the same body) — the shape the content
	// corroboration must accept, and the one that lets this test keep measuring
	// the deep-burial regression rather than the name-collision refutation.
	run("checkout", "-q", IntegrationBranchName)
	// The landing writes the branch's own file with the SAME body (so the content
	// corroboration confirms it) and one file of its own — the latter keeps the shas
	// and the patch-ids distinct, because two commits with the same tree, message and
	// parent collapse into ONE sha and the fixture would stop modelling the case.
	if err := os.WriteFile(filepath.Join(dir, "branch.txt"), []byte("the branch's own version of the fix\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// `commit` stages ONLY the file it writes, so the carried file is staged here.
	run("add", "branch.txt")
	landed := commit("landing-notes.txt", "the landing's own note\n", reworkLandingTestBuriedSubject)
	if landed == tip {
		t.Fatalf("fixture is wrong: the landing and the tip are the SAME commit %s", landed)
	}

	// ...and is then buried under an unrelated pile deeper than the window the
	// first implementation used (reworkLandingTestDeepBurial > 400).
	for i := 0; i < reworkLandingTestDeepBurial; i++ {
		commit(fmt.Sprintf("churn-%d.txt", i),
			fmt.Sprintf("churn %d\n", i),
			fmt.Sprintf("feat(churn): unrelated commit %d on the integration branch", i))
	}
	// PRECONDITION, asserted rather than assumed: the landed commit really is
	// further from the tip than the window that used to be used, so this test
	// cannot silently stop exercising the regression.
	buried := strings.TrimSpace(run("rev-list", "--count", landed+".."+IntegrationBranchName))
	//stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
	if n, _ := strconv.Atoi(buried); n <= reworkLandingTestOldWindow {
		t.Fatalf("fixture is wrong: the landing is only %s commits deep, which the old %d-commit window would have seen",
			buried, reworkLandingTestOldWindow)
	}

	ev := ProbeReworkLanding(dir, "task/buried")
	if ev.Err != "" {
		t.Fatalf("probe must answer, got Err=%q", ev.Err)
	}
	if !ev.Signal() {
		t.Fatalf("a landing %s commits below the tip must still be found: %+v", buried, ev)
	}
	if !ev.LandedDistinct {
		t.Fatalf("the buried landing must be reported as a DIFFERENT commit from the tip: %+v", ev)
	}
	if ev.Landed != landed {
		t.Fatalf("the reported landed commit must be the buried one %s, got %s", landed, ev.Landed)
	}
	if ev.Tip != tip {
		t.Fatalf("tip mismatch: want %s got %s", tip, ev.Tip)
	}
}

// TestReworkLandingProbeIgnoresSubjectAsSubstringOfALongerSubject pins the
// distinction that git's `--grep` blurs: `--grep` matches the commit MESSAGE, so
// it is a candidate filter, and the equality comparison is the gate. Measured on
// real master, the top-scoring candidate for the incident's subject is
// `Merge commit 8fafbafee (re-land): fix(code_search): accept a ctags index …` —
// a commit whose subject only CONTAINS the string. Reporting it as the landed
// equivalent would name a merge commit as this branch's landed work.
func TestReworkLandingProbeIgnoresSubjectAsSubstringOfALongerSubject(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	writeCommit := func(file, body, subject string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		run("add", file)
		run("commit", "-q", "-m", subject)
	}

	run("init", "-q", "-b", IntegrationBranchName)
	writeCommit("seed.txt", "seed\n", "chore: seed the fixture repository")

	run("checkout", "-q", "-b", "task/substring")
	writeCommit("branch.txt", "the branch's own fix\n", reworkLandingTestSubject)

	run("checkout", "-q", IntegrationBranchName)
	// (a) the subject as a SUFFIX of a longer subject — the real shape
	writeCommit("re-land.txt", "a re-land\n",
		"Merge commit deadbeef (re-land): "+reworkLandingTestSubject)
	// (b) the subject as a BODY line only, under a different subject
	if err := os.WriteFile(filepath.Join(dir, "body.txt"), []byte("body\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "body.txt")
	run("commit", "-q", "-m", "chore: a commit that merely mentions the work",
		"-m", reworkLandingTestSubject)

	ev := ProbeReworkLanding(dir, "task/substring")
	if ev.Err != "" {
		t.Fatalf("probe must answer, got Err=%q", ev.Err)
	}
	if ev.Signal() {
		t.Fatalf("a subject that is a SUBSTRING of another subject must not signal: %+v", ev)
	}
	if ev.Landed != "" {
		t.Fatalf("no landed sha may be reported for a substring-only match: %+v", ev)
	}
	if !strings.Contains(ev.Section(), "NOT ALREADY LANDED (probe ran)") {
		t.Fatalf("the answer must be the stated negative:\n%s", ev.Section())
	}
}

// TestReworkLandingProbeSilentOnGenericSubject is the SILENT arm: a branch whose
// tip subject nothing on the integration branch carries yields no signal and no
// landed sha — and the section still states the negative, so "no signal" and
// "the probe did not run" never read alike.
func TestReworkLandingProbeSilentOnGenericSubject(t *testing.T) {
	repo := reworkLandingTestRepo(t)
	// A branch off master with an unrelated subject.
	cmd := exec.Command("git", "-C", repo, "checkout", "-q", "-b", "task/generic")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repo, "generic.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "generic.txt"}, {"commit", "-m", reworkLandingTestFileSubject}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	ev := ProbeReworkLanding(repo, "task/generic")
	if ev.Err != "" {
		t.Fatalf("probe must answer, got Err=%q", ev.Err)
	}
	if ev.Signal() {
		t.Fatalf("a generic subject must NOT produce a signal: %+v", ev)
	}
	if ev.Landed != "" {
		t.Fatalf("no landed sha may be reported for a generic subject: %+v", ev)
	}
	section := ev.Section()
	if !strings.Contains(section, reworkLandingTestFileSubject) {
		t.Fatalf("even the negative answer names the tip subject:\n%s", section)
	}
	if !strings.Contains(section, "NOT ALREADY LANDED") {
		t.Fatalf("the negative answer must be stated, not silent:\n%s", section)
	}
}

// TestReworkLandingProbeUnknownIsNotClean pins Rule 15 on this probe: a repo or
// branch the probe cannot read yields Err set, Signal() false, and a section
// that says the probe did not run — never the clean answer.
func TestReworkLandingProbeUnknownIsNotClean(t *testing.T) {
	for _, tc := range []struct{ repo, branch string }{
		{repo: filepath.Join(t.TempDir(), "does-not-exist"), branch: "task/x"},
		{repo: reworkLandingTestRepo(t), branch: "task/no-such-branch"},
		{repo: "", branch: "task/x"},
		{repo: reworkLandingTestRepo(t), branch: ""},
	} {
		ev := ProbeReworkLanding(tc.repo, tc.branch)
		if ev.Err == "" {
			t.Fatalf("an unreadable probe must set Err (repo=%q branch=%q): %+v", tc.repo, tc.branch, ev)
		}
		if ev.Signal() {
			t.Fatalf("an unreadable probe must not signal: %+v", ev)
		}
		section := ev.Section()
		if strings.Contains(section, "NOT ALREADY LANDED (probe ran)") {
			t.Fatalf("an unrun probe must NOT print the clean answer:\n%s", section)
		}
		if !strings.Contains(section, "DID NOT RUN") {
			t.Fatalf("an unrun probe must say so:\n%s", section)
		}
	}
}

// TestReworkLandingProbeSkipsBookkeepingSubjects pins the conservatism that
// makes the probe safe on THIS repo, where one bookkeeping subject repeats
// hundreds of times: a branch whose tip is such a commit must not claim an
// equivalence on the strength of shared plumbing.
func TestReworkLandingProbeSkipsBookkeepingSubjects(t *testing.T) {
	repo := reworkLandingTestRepo(t)
	// Every subject this repo's machinery writes on all branches, plus a
	// too-short one. None may carry a signal.
	for _, subject := range []string{
		"delivery-point sync: merge master into the delivered branch before close-out",
		"pre-merge: verify branch against peer master before pushing",
		"Merge branch 'master' into task/whatever",
		"Merge master into fix/whatever (rework: re-merge on current master)",
		"Revert \"feat(x): something\"",
		"wip",
	} {
		if reworkLandingSubjectCarriesSignal(subject) {
			t.Fatalf("subject %q must not carry a signal (it repeats across branches or identifies nothing)", subject)
		}
	}
	// A real fix subject must.
	if !reworkLandingSubjectCarriesSignal(reworkLandingTestSubject) {
		t.Fatalf("the measured subject must carry a signal")
	}
	_ = repo
}

// THE TWO WIRE TESTS THAT STOOD HERE WERE RETIRED 2026-10-31 (task 01a18d61)
// with their subjects: TestMergeCeilingPayloadCarriesTheLandingSection asserted
// mergeCeilingPayload's body and TestHandoverEscalatePayloadCarriesTheLandingSection
// asserted mergeHandoverEscalatePayload's, and both functions are gone with the
// merge-ceiling and hand-over-escalation lanes (merge_ceiling_rework.go,
// merge_handover_escalate.go — deleted in the same change). A source-level
// assertion about a function that no longer exists asserts nothing.
//
// What they guarded is NOT unguarded: the PROPERTY was "a rework filing lane
// threads ProbeReworkLanding's section into the payload it submits". The lane
// that still mints rework rows for this reason — the verify-revert lane — is
// covered by its own arm below, and the probe itself keeps every arm in this
// file. Do not reinstate these two without reintroducing the lanes.

// reworkLandingRefutationRepo builds the SHAPE THE CORROBORATION EXISTS FOR: the
// integration branch carries a commit with the SAME SUBJECT as the branch's tip,
// but that commit does NOT carry the branch's work — a name collision, which a
// subject-only probe reports as "master may already carry this change".
//
// It is the same fork shape as reworkLandingTestRepo (so the pair's patch-ids
// still differ and `git cherry` still calls the tip patch-unique), with ONE
// difference: the landed commit writes a DIFFERENT file, so none of the branch's
// added lines are at the destination.
func reworkLandingRefutationRepo(t *testing.T) (repo, tip, landed string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = testutil.GitEnv(
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(subject string) string {
		t.Helper()
		run("add", ".")
		run("commit", "-m", subject)
		return strings.TrimSpace(run("rev-parse", "HEAD"))
	}

	run("init", "-q", "-b", IntegrationBranchName)
	write("seed.txt", "seed\n")
	commit("chore: seed the fixture repository")

	// The branch forks here, so no landed commit can be its ancestor.
	run("checkout", "-q", "-b", "task/refuted")
	write("branch-work.go", "package p\n\n// this branch's own work\nfunc BranchWork() {}\n")
	tip = commit(reworkLandingTestSubject)

	// The integration branch lands an UNRELATED change under the SAME subject.
	run("checkout", "-q", IntegrationBranchName)
	// THE SAME PATH with DIFFERENT work: this is the "same subject, different
	// change" shape at its sharpest, and it is the one whose refutation can quote
	// the missing lines (`added-lines-missing`) rather than only an absent path.
	write("branch-work.go", "package p\n\n// a different change that happens to share the subject\nfunc Else() {}\n")
	landed = commit(reworkLandingTestSubject)
	if landed == tip {
		t.Fatalf("fixture is wrong: the landing and the tip are the SAME commit %s", landed)
	}
	// PRECONDITION: the pair still reproduces the measured patch shape, so this
	// fixture cannot silently degrade into a case git could answer by patch.
	if patchID(t, dir, tip) == patchID(t, dir, landed) {
		t.Fatalf("fixture is wrong: tip %s and landed %s share a patch-id", tip, landed)
	}
	run("checkout", "-q", IntegrationBranchName)
	return dir, tip, landed
}

// TestReworkLandingProbeCorroboratesTheMatchWithContent is acceptance (a): a
// rebase that rewrote the commits but landed the SAME content must still be
// reported as landed — and the evidence must QUOTE the existing content arm's own
// sentence, not a second measurement written for this probe (Rule 43).
func TestReworkLandingProbeCorroboratesTheMatchWithContent(t *testing.T) {
	repo := reworkLandingTestRepo(t)
	branch := "task/rework-target"

	ev := ProbeReworkLanding(repo, branch)
	if ev.Err != "" {
		t.Fatalf("probe must answer, got Err=%q", ev.Err)
	}
	if !ev.ContentJudged {
		t.Fatalf("the content arm must answer on a same-repo fixture: %+v", ev)
	}
	if !ev.ContentLanded {
		t.Fatalf("the landed commit carries the branch's added lines, so the corroboration must confirm it: %+v", ev)
	}
	if !ev.Signal() {
		t.Fatalf("a content-corroborated subject match must still signal: %+v", ev)
	}
	// THE REUSE PROOF: the corroboration sentence is the delivery proof arm's own
	// wording. Re-derive it by calling the SAME arm directly and compare bytes —
	// a second implementation written for this probe could not match it.
	ctx := context.Background()
	landedRef := fmt.Sprintf(reworkLandingIntegrationRefFormat, IntegrationBranchName)
	wantLanded, wantWhy, wantJudged := deliveryProofContentLanded(ctx, repo, repo, ev.Landed, ev.Landed, ev.Tip)
	if !wantJudged || !wantLanded {
		t.Fatalf("the arm must answer the same way it did inside the probe: landed=%v why=%q judged=%v",
			wantLanded, wantWhy, wantJudged)
	}
	if ev.ContentWhy != wantWhy {
		t.Fatalf("the probe must quote the content arm's own sentence (Rule 43: no second implementation)\nprobe: %s\narm:   %s", ev.ContentWhy, wantWhy)
	}
	section := ev.Section()
	if !strings.Contains(section, wantWhy) {
		t.Fatalf("the signal section must carry the content arm's evidence:\n%s", section)
	}
	if !strings.Contains(section, "CONTENT CORROBORATION") {
		t.Fatalf("the section must name the corroboration it ran:\n%s", section)
	}
	_ = landedRef
}

// TestReworkLandingProbeRefutesSubjectMatchWithoutTheContent is acceptance (b):
// the integration branch carries a commit with this branch's tip subject, but
// that commit does NOT carry the branch's added lines. A subject-only probe calls
// this a duplicate branch; with the corroboration it must NOT report landed, and
// it must NAME the missing lines.
func TestReworkLandingProbeRefutesSubjectMatchWithoutTheContent(t *testing.T) {
	repo, tip, landed := reworkLandingRefutationRepo(t)
	branch := "task/refuted"

	ev := ProbeReworkLanding(repo, branch)
	if ev.Err != "" {
		t.Fatalf("probe must answer, got Err=%q", ev.Err)
	}
	// THE PREMISE: the subject match really is there. Without this the test could
	// pass by the probe never finding a match at all.
	if ev.Landed != landed || ev.Tip != tip {
		t.Fatalf("the subject match must still be found (landed=%s tip=%s): %+v", landed, tip, ev)
	}
	if !ev.LandedDistinct {
		t.Fatalf("the match must be a different commit from the tip: %+v", ev)
	}
	if !ev.ContentJudged {
		t.Fatalf("the content arm must answer on this fixture: %+v", ev)
	}
	if ev.ContentLanded {
		t.Fatalf("the landed commit does NOT carry the branch's added lines, so the corroboration must refute it: %+v", ev)
	}
	if ev.Signal() {
		t.Fatalf("a refuted content match must NOT report the branch as landed: %+v", ev)
	}

	section := ev.Section()
	if strings.Contains(section, ReworkLandingSignalHeadline) {
		t.Fatalf("the refuted answer must not print the signal headline:\n%s", section)
	}
	if strings.Contains(section, ReworkLandingCleanHeadline) {
		t.Fatalf("the refuted answer must not print the clean headline either (the name DID match):\n%s", section)
	}
	if !strings.Contains(section, ReworkLandingRefutedHeadline) {
		t.Fatalf("the refuted answer must state what it found:\n%s", section)
	}
	// The missing lines, by name: the branch's own file and function.
	for _, want := range []string{"branch-work.go", "func BranchWork() {}"} {
		if !strings.Contains(section, want) {
			t.Fatalf("the refutation must name the missing work (%q):\n%s", want, section)
		}
	}
}

// TestReworkLandingProbeUnjudgedContentKeepsTheSignal pins Rule 15 on the new
// decision: an arm that could NOT answer must not turn a real subject match into
// "not landed". It also proves the corroboration is really the arm's answer and
// not an unconditional true — with the arm switched off (its own red proof's
// switch), the signal stands and the section says the corroboration is missing.
func TestReworkLandingProbeUnjudgedContentKeepsTheSignal(t *testing.T) {
	repo := reworkLandingTestRepo(t)
	branch := "task/rework-target"

	deliveryProofContentArmOff = true
	defer func() { deliveryProofContentArmOff = false }()

	ev := ProbeReworkLanding(repo, branch)
	if ev.Err != "" {
		t.Fatalf("probe must answer, got Err=%q", ev.Err)
	}
	if ev.ContentJudged {
		t.Fatalf("with the arm switched off nothing may be judged: %+v", ev)
	}
	if !ev.Signal() {
		t.Fatalf("an unanswered corroboration must leave the name match standing (Rule 15): %+v", ev)
	}
	section := ev.Section()
	if !strings.Contains(section, reworkLandingContentUnjudgedLine) {
		t.Fatalf("the section must STATE that the content was not measured:\n%s", section)
	}
}
