package tools

// git_prose_locale_test.go — the regression guard for the locale-translated git
// prose THREE more runners fed to a DECISION (epic 01a18d50, measured
// 2026-10-30, the sibling of git_child_locale_test.go's census guard).
//
// THE CLASS. git translates its own diagnostics (gettext). git_child_locale.go
// pins the locale for the runners that were audited when the census bug landed;
// three more runners were never measured, and each of them hands git's sentence
// to a PREDICATE rather than to an operator:
//
//	task_merge.go          taskMergeGit       → isGitNoMergeToAbort
//	git_peer_fetch.go      gitPeerRun         → gitPushbackTargetAliasNotRemote
//	manifest_union_gate.go CatFileBlob        → manifestUnionGateAbsentBlobMarkers
//
// Each test below drives ONE of them under a locale that really translates git
// on this host, and asserts the DECISION the predicate makes — never a rendered
// string, which would pass while the decision was wrong.
//
// Each is RED without its pin, and the red proves the test is not vacuous:
//
//	$ LC_ALL=C            git merge --abort
//	fatal: There is no merge to abort (MERGE_HEAD missing).
//	$ LC_ALL=zh_CN.UTF-8  git merge --abort
//	致命错误：没有要终止的合并（MERGE_HEAD 丢失）。
//
//	$ LC_ALL=C            git -C <repo> cat-file -p HEAD:no/such/file
//	fatal: path 'no/such/file' does not exist in 'HEAD'
//	$ LC_ALL=zh_CN.UTF-8  git -C <repo> cat-file -p HEAD:no/such/file
//	致命错误：路径 'no/such/file' 不在 'HEAD' 中
//
//	$ LC_ALL=C            git merge-base --is-ancestor <absent> HEAD
//	fatal: Not a valid commit name <sha>
//	$ LC_ALL=zh_CN.UTF-8  git merge-base --is-ancestor <absent> HEAD
//	致命错误：Not a valid commit name <sha>
//
// The last one shows WHY the pin is the fix and not a translated word added to
// the parser: the English BODY survives translation while the `fatal: ` PREFIX
// does not, so a marker's survival under a foreign locale is an accident of
// which fragment git happened to leave alone — never a contract.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// gitProseLocaleTrap exports a locale whose git diagnostics are translated, and
// skips LOUDLY when this host cannot translate git at all (a silent pass would
// report "the pin holds" from a measurement that never ran — Rule 15).
//
// t.Setenv is the allowed mechanism (Rule 17) and its scope is the test, so the
// exported values cannot leak into a sibling. The three gettext selectors are
// cleared first: the pin's own strip is what the test measures on the CHILD, so
// the parent must hand the child a live translation to strip.
func gitProseLocaleTrap(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if !localeInstalled(localeLocalized) {
		t.Skipf("%s is not installed on this host, so git cannot localize its diagnostics here and this guard could not tell a pin from an absent translation (install it, or run on a host that has it)", localeLocalized)
	}
	// A NEGATIVE CONTROL, in the form the decision actually takes: with these
	// values in force and NO pin, git's answer must be demonstrably translated.
	// If it is not, the locale was not really applied and everything below
	// would pass for the wrong reason.
	t.Setenv("LANG", localeLocalized)
	t.Setenv("LC_ALL", localeLocalized)
	t.Setenv("LANGUAGE", localeLocalized)
	t.Setenv("LC_MESSAGES", localeLocalized)
}

// gitProseAmbientTranslated reports whether an UNPINNED git child of this
// process really answers in Chinese — the precondition of every guard below.
func gitProseAmbientTranslated(t *testing.T, dir string, args ...string) bool {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// The AMBIENT environment on purpose: that is the thing under test. Routing
	// variables are stripped so the probe cannot be pointed at another repo
	// (testutil.GitEnv keeps everything else, so the exported locale still
	// reaches git — the same helper the sibling census guard uses).
	cmd.Env = testutil.GitEnv()
	out, err := cmd.CombinedOutput()
	// A PROBE THAT FAILED TO RUN IS NOT EVIDENCE OF TRANSLATION. The command is
	// expected to exit non-zero (it is chosen because it FAILS), so exit status
	// alone says nothing; what makes the answer usable is that git RAN and spoke.
	// Reporting "not translated" on a run failure makes the guard below SKIP
	// LOUDLY rather than pass — the correct reading of "we could not measure it".
	if err != nil && len(out) == 0 {
		return false
	}
	return strings.Contains(string(out), proseLocaleChineseFatalPrefix)
}

// probeLocaleChineseFatalPrefix is git's translated "fatal: " — the one
// fragment measured to change on this host. Its presence means "an unpinned
// child would speak Chinese", which is the condition a pin exists to remove.
const proseLocaleChineseFatalPrefix = "致命错误"

// proseLocaleFatalPrefix is its English original, used by the one test that
// records an error carrying NO git prose at all.
const proseLocaleFatalPrefix = "fatal: "

// proseLocaleAbsentSHA is a well-formed command name that resolves to nothing.
const proseLocaleAbsentSHA = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

// proseLocaleFixture builds a repo with one commit and no extra refs.
func proseLocaleFixture(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "master")
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "seed")
	return repo
}

// TestTaskMergeGitPinsTheLocaleBehindNoMergeToAbort is the guard for
// taskMergeGit.
//
// THE DEFECT (measured on the REAL binary, 2026-10-30): an orphan branch makes
// `git merge` fail PRE-FLIGHT (unrelated histories) with no merge state left,
// so ③b runs `git merge --abort` and isGitNoMergeToAbort must recognise git's
// answer. Unpinned, the same invocation printed:
//
//	LC_ALL=zh_CN.UTF-8 gopi task merge orphan --repo <fixture>
//	  merge orphan   error   merge --abort: git … merge --abort: exit status
//	                         128: 致命错误：没有要终止的合并（MERGE_HEAD 丢失）。
//
// — the operator was told a CONFLICT SCENE could not be aborted, while master
// was untouched and the real cause (`refusing to merge unrelated histories`) was
// dropped. Pinned, the same command answers "no conflict state to abort" and
// carries the original failure, which is what the branch needs.
func TestTaskMergeGitPinsTheLocaleBehindNoMergeToAbort(t *testing.T) {
	gitProseLocaleTrap(t)
	repo := proseLocaleFixture(t)
	// The probe whose prose the predicate matches, run UNPINNED: it must be
	// Chinese here, or this test proves nothing about the pin.
	if !gitProseAmbientTranslated(t, repo, "merge", "--abort") {
		t.Skipf("an unpinned git child did not translate `merge --abort` on this host, so the locale the pin removes was not in force and the assertion below would be vacuous")
	}

	// Through the PRODUCTION runner — not a hand-built exec — so the pin under
	// test is the one production uses.
	_, err := taskMergeGit(repo, "merge", "--abort")
	if err == nil {
		t.Fatal("git merge --abort with no merge in progress returned nil; the fixture is wrong (something staged a merge)")
	}
	if got := err.Error(); strings.Contains(got, proseLocaleChineseFatalPrefix) {
		t.Fatalf("taskMergeGit's child spoke Chinese: %q\nThe locale pin is gone or ineffective. isGitNoMergeToAbort matches the ENGLISH sentence, so this error is classified as a conflict scene.", got)
	}
	if !isGitNoMergeToAbort(err) {
		t.Fatalf("isGitNoMergeToAbort = false for %q\ngit's own 'no merge to abort' answer must be recognised or a pre-flight failure is reported as an unabortable conflict", err)
	}
}

// TestManifestUnionGateBlobTreatsAnAbsentSideAsAbsentWhateverTheLocale is the
// guard for manifestUnionGateCatFileBlob.
//
// THE DEFECT: an absent blob is an ORDINARY tree state the gate converts to
// ("", nil) and ABSTAINS on. Its recogniser is manifestUnionGateAbsentBlobMarkers,
// which spells `does not exist` — and git's translated form of that sentence is
// a completely different string, so the identical fixture produced a READ ERROR
// under zh_CN and an abstention under C: the gate's verdict depended on the
// operator's desktop language.
func TestManifestUnionGateBlobTreatsAnAbsentSideAsAbsentWhateverTheLocale(t *testing.T) {
	gitProseLocaleTrap(t)
	repo := proseLocaleFixture(t)
	if !gitProseAmbientTranslated(t, repo, "cat-file", "-p", "HEAD:no/such/file") {
		t.Skipf("an unpinned git child did not translate `cat-file -p HEAD:no/such/file` on this host, so the locale the pin removes was not in force and the assertion below would be vacuous")
	}

	workRoot := t.TempDir()
	body, err := manifestUnionGateBlob(repo, workRoot, manifestUnionGateOursBase, "HEAD", "no/such/file")
	if err != nil {
		t.Fatalf("manifestUnionGateBlob on a path absent from HEAD = (%q, %v), want (\"\", nil) — an absent side is a legitimate tree state the gate ABSTAINS on, never a read failure; a locale-dependent verdict is a Rule 15 silent failure", body, err)
	}
	if body != "" {
		t.Fatalf("manifestUnionGateBlob returned body %q for an absent path, want the empty read that encodes 'absent'", body)
	}

	// The classification itself, at the reader that owns the markers: the
	// error's TEXT is what the predicate matches, so it is asserted directly.
	if _, err := manifestUnionGateCatFileBlob(repo, "HEAD:no/such/file"); err == nil {
		t.Fatal("manifestUnionGateCatFileBlob returned nil for an absent path; the fixture is wrong")
	} else {
		matched := false
		for _, marker := range manifestUnionGateAbsentBlobMarkers {
			if strings.Contains(err.Error(), marker) {
				matched = true
			}
		}
		if !matched {
			t.Fatalf("no absent-blob marker matched %q (markers: %v)\nThe reader's child must speak English whatever the caller's locale, or 'absent' and 'unreadable' collapse.", err, manifestUnionGateAbsentBlobMarkers)
		}
	}
}

// TestAdoptionGitPinsTheLocale is the guard for adoptionGit, the adoption
// lane's ONE git runner.
//
// THE DEFECT SHAPE (③b): a branch whose PROBE merge failed pre-flight
// (`fatal: refusing to merge unrelated histories` — an orphan branch) leaves no
// merge state, so the MANDATORY `merge --abort` answers "there is no merge to
// abort", and that is translated. Unpinned, the row recorded
// `failed: abort: … 没有要终止的合并（MERGE_HEAD 丢失）。` under zh_CN and a
// different sentence under C for the same repository state.
func TestAdoptionGitPinsTheLocale(t *testing.T) {
	gitProseLocaleTrap(t)
	repo := proseLocaleFixture(t)
	if !gitProseAmbientTranslated(t, repo, "merge", "--abort") {
		t.Skipf("an unpinned git child did not translate `merge --abort` on this host, so the locale the pin removes was not in force and the assertion below would be vacuous")
	}

	_, err := adoptionGit(repo, "merge", "--abort")
	if err == nil {
		t.Fatal("adoptionGit's `merge --abort` with no merge in progress returned nil; the fixture is wrong")
	}
	if got := err.Error(); strings.Contains(got, proseLocaleChineseFatalPrefix) {
		t.Fatalf("adoptionGit's child spoke Chinese: %q\nThe locale pin is gone or ineffective, so the branch's audit record carries a sentence that depends on the operator's desktop language.", got)
	}
}

// TestTaskRefGitCarriesNoGitProseToAClassifier records a MEASURED NEGATIVE, so a
// future reader does not "fix" this runner into a translation table.
//
// A pin was TRIED here and removed, because the red proof refused to justify it:
// with the pin deleted this test still passed. That is not a weak assertion, it
// is the finding — taskRefGit reads through cmd.Output() and returns
// `fmt.Errorf("git %s: %w", args, err)`, so its error text is
// `git log ...: exit status 128` and contains NO sentence git could translate.
// Its two callers (taskRefGitNamedTaskIDs, task_completion_witness.go) wrap that
// error and match nothing in it, so a zh_CN desktop cannot move their verdicts
// and a pin would have been a precaution with no measurement behind it.
//
// The property is still worth guarding, in the form that CAN rot: the day a
// caller starts classifying this error's TEXT, the text has to be English, and
// that day this test fails and says why.
func TestTaskRefGitCarriesNoGitProseToAClassifier(t *testing.T) {
	gitProseLocaleTrap(t)
	repo := proseLocaleFixture(t)

	_, err := taskRefGit(context.Background(), repo, "merge-base", "--is-ancestor", proseLocaleAbsentSHA, "HEAD")
	if err == nil {
		t.Fatal("taskRefGit's failure case returned nil; the fixture is wrong")
	}
	if got := err.Error(); strings.Contains(got, proseLocaleChineseFatalPrefix) {
		t.Fatalf("taskRefGit's error now CARRIES git's translated prose: %q\nA caller can classify that text. Pin the child locale at this runner (the policy is git_child_locale.go's ChildLocaleEnv) before shipping the classifier.", got)
	}
	if got := err.Error(); strings.Contains(got, proseLocaleFatalPrefix) {
		t.Fatalf("taskRefGit's error now carries git's ENGLISH prose too: %q\nthe guarantee this test records is that the error is `exit status N` only; a classifier reading git's sentence needs the child locale pinned first.", got)
	}
}

// TestRepoSweepRefPresentIsLocaleSafeByConstruction records the ONE suspect that
// needs NO pin, so a future reader does not "fix" it into a translation table.
//
// repoSweepRefPresent decides from the EXIT CODE plus an EMPTY-output test: git
// answers an absent ref with `--verify --quiet`, which exits 1 and prints
// nothing in EVERY locale (the flag suppresses the message, not just its
// translation). It never reads git's prose, so there is nothing for gettext to
// corrupt — asserted here rather than assumed, because "this one is fine" is
// exactly the claim that rots silently.
func TestRepoSweepRefPresentIsLocaleSafeByConstruction(t *testing.T) {
	gitProseLocaleTrap(t)
	repo := proseLocaleFixture(t)

	present, err := repoSweepRefPresent(repo, "master")
	if err != nil || !present {
		t.Fatalf("repoSweepRefPresent(master) = (%v, %v), want (true, nil)", present, err)
	}
	present, err = repoSweepRefPresent(repo, "no-such-branch")
	if err != nil {
		t.Fatalf("repoSweepRefPresent(no-such-branch) error = %v, want (false, nil) — the absence path reads the exit code and the EMPTY output, never a sentence, so no locale can move it", err)
	}
	if present {
		t.Fatal("repoSweepRefPresent(no-such-branch) = true, want false")
	}
}

// TestGitPeerRunPinsTheLocale is the guard for gitPeerRun — the ONE `git -C`
// runner behind task_sync_git_pushback.go and task_sync_git.go, whose callers
// read git's sentence (pbUnresolvableRepoPhrase, the documented
// `fatal: Not a valid object name` exit table).
//
// The probe is `merge-base --is-ancestor <absent> HEAD`: git answers with a
// sentence whose PREFIX translates on this host. That is the point — a marker
// matching the surviving body would pass by luck under zh_CN and by luck under
// another locale, so the pin, not the wording, is what makes the contract hold.
func TestGitPeerRunPinsTheLocale(t *testing.T) {
	gitProseLocaleTrap(t)
	repo := proseLocaleFixture(t)
	if !gitProseAmbientTranslated(t, repo, "merge-base", "--is-ancestor", proseLocaleAbsentSHA, "HEAD") {
		t.Skipf("an unpinned git child did not translate `merge-base --is-ancestor` on this host, so the locale the pin removes was not in force and the assertion below would be vacuous")
	}

	_, err := gitPeerRun(context.Background(), repo, nil, "merge-base", "--is-ancestor", proseLocaleAbsentSHA, "HEAD")
	if err == nil {
		t.Fatal("git merge-base --is-ancestor on an absent commit returned nil; the fixture is wrong")
	}
	if got := err.Error(); strings.Contains(got, proseLocaleChineseFatalPrefix) {
		t.Fatalf("gitPeerRun's child spoke Chinese: %q\nThe locale pin is gone or ineffective, so every caller that classifies git's English prose (pbUnresolvableRepoPhrase, the Not-a-valid-object-name exit table) is matching a contract git can rewrite per locale.", got)
	}

	// A CALLER-SUPPLIED env must keep its own entries AND take the pin: the
	// pin is applied through the slice, never in place of it. This is the shape
	// taskGroundGit uses (withoutGitDirEnv), and a pin that discarded the slice
	// would silently reintroduce a routed GIT_DIR — a worse defect than the one
	// being fixed.
	sentinel := "GOPI_LOCALE_PIN_SENTINEL=kept"
	_, err = gitPeerRun(context.Background(), repo, append(os.Environ(), sentinel), "merge-base", "--is-ancestor", proseLocaleAbsentSHA, "HEAD")
	if err == nil {
		t.Fatal("git merge-base --is-ancestor on an absent commit returned nil through the caller-env path")
	}
	if got := err.Error(); strings.Contains(got, proseLocaleChineseFatalPrefix) {
		t.Fatalf("gitPeerRun with a CALLER-SUPPLIED env spoke Chinese: %q\nthe pin must be applied THROUGH that slice (ChildLocaleEnvFrom), not skipped because the caller built one", got)
	}
}
