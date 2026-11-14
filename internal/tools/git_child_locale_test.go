package tools

// git_child_locale_test.go — the regression guard for the LOCALE-TRANSLATED git
// diagnostic (epic 01a1306b, measured 2026-10-12).
//
// THE BUG. git translates its own diagnostics (gettext), so the unreachable
// census — which selects its rows by the literal first field `unreachable` —
// matched NOTHING under a non-English locale and rendered its clean-looking
// zero for a repo holding an orphaned tip:
//
//	$ LANG=zh_CN.UTF-8 LC_ALL=zh_CN.UTF-8 gopi task archive --unreachable --repo <repo>
//	  unreachable tips:    0        ← the object store held one
//
// It surfaced as a "flake": the SAME test passed in isolation and failed under
// the suite, because the two were launched from environments with different
// LANG values (the foreground shell carried LC_ALL=C; the background job
// runner carried LANG=zh_CN.UTF-8 with no LC_ALL). The fixture and the binary
// were identical — the ambient locale was the only difference, and it was an
// INPUT to a parser reading our strings out of git's output.
//
// The fix is the locale pin in git_child_locale.go. These tests hold it there
// and prove it by DRIVING the localized locale rather than dropping it.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// localeLocalized — a locale whose git diagnostics are NOT English. It is a
// NAMED value, never a probe (the test asserts the failure mode; it does not
// search for a locale that happens to be installed), and it is the locale that
// was measured to break the census on this host. The same value and rationale
// as the sibling guard in task_sync_git_pushback_locale_test.go (Rule 14: one
// spelling of "a locale that translates git").
const localeLocalized = "zh_CN.utf8"

// localeCensusFixture builds a repo with ONE orphan chain (two commits, tip =
// the second) by deleting the branch ref, exactly the way the real backlog was
// produced. It returns the repo and the tip.
//
// It builds the fixture through the AMBIENT locale on purpose: the defect was
// in READING git's output, not in git's behaviour, so the fixture must be
// representative of the ordinary path rather than built under a pin.
func localeCensusFixture(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo, tips := unreachFixtureRepo(t)
	if len(tips) == 0 {
		t.Fatal("the fixture produced no orphan tip")
	}
	return repo, tips[0]
}

// localeCensusFixtureIn materialises the SAME shape (one orphan chain, tip
// second) directly inside repo, for a caller that owns the directory.
func localeCensusFixtureIn(t *testing.T, repo string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	gitRun(t, repo, "init", "-q", "-b", "master")
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "seed")
	gitRun(t, repo, "checkout", "-q", "-b", "orphan", "master")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "orphan one")
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "orphan two")
	tip := strings.TrimSpace(repoSweepOut(t, repo, gitRevParseVerb, "HEAD"))
	gitRun(t, repo, "checkout", "-q", "master")
	gitRun(t, repo, "branch", "-D", "orphan")
	return tip
}

// repoSweepOut runs one git command through the SHARED runner and returns its
// output, failing the test on error. A fixture helper, so git's own message
// reaches the failure.
func repoSweepOut(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := repoSweepGit(repo, args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), repo, err)
	}
	return out
}

// TestCensusReadsGitsReportWhateverTheLocale is the REGRESSION GUARD.
//
// RED BEFORE THE FIX (measured, this is what the pin closes): the census
// returned 0 commits / 0 tips under localeLocalized while the same repo
// answered 2 commits / 1 tip under "C" — the zero was rendered as "no
// unreachable tip — every commit is named by a ref", a Rule 15 silent failure
// in which "I measured nothing" and "there is nothing" look alike.
//
// The assertion is NOT loosened to accommodate a translation: the census must
// find the tip, in every locale, because the locale is the parser's input and
// the pin is what makes that input fixed.
func TestCensusReadsGitsReportWhateverTheLocale(t *testing.T) {
	if !localeInstalled(localeLocalized) {
		// Loud, never silent (Rule 15): the guard must say which measurement it
		// could not make rather than pass by doing nothing.
		t.Skipf("%s is not installed on this host, so git cannot localize its diagnostics here and the locale-dependence of the census could not be exercised (install it, or run on a host that has it)", localeLocalized)
	}
	repo, tip := localeCensusFixture(t)

	// The ENGLISH reference: what the census answers with a byte-stable locale.
	// It is the control, so the localized arm below cannot pass by the fixture
	// being empty.
	t.Setenv("LANG", "C")
	t.Setenv("LC_ALL", "C")
	wantCensus, err := ArchiveUnreachableCensusAt(repo, time.Now())
	if err != nil {
		t.Fatalf("census under LANG=C: %v", err)
	}
	if len(wantCensus.Tips) == 0 {
		t.Fatalf("control census under LANG=C found no tip — the fixture is empty and this guard would prove nothing:\n%s",
			ArchiveUnreachableReport(wantCensus))
	}

	// THE ARM UNDER TEST: the same repo, the locale that translates git.
	t.Setenv("LANG", localeLocalized)
	t.Setenv("LC_ALL", localeLocalized)
	got, err := ArchiveUnreachableCensusAt(repo, time.Now())
	if err != nil {
		t.Fatalf("census under %s: %v", localeLocalized, err)
	}
	if got.UnreachableCommits != wantCensus.UnreachableCommits || len(got.Tips) != len(wantCensus.Tips) {
		t.Fatalf("the census's answer depends on the ambient locale.\n"+
			"  LANG=C:      %d commits / %d tips\n"+
			"  %s: %d commits / %d tips\n"+
			"git translates its own diagnostics, and this census reads the word %q out of them — so the locale is an INPUT to the parser and must be pinned (git_child_locale.go).\n%s",
			wantCensus.UnreachableCommits, len(wantCensus.Tips),
			localeLocalized, got.UnreachableCommits, len(got.Tips),
			archiveUnreachableLinePrefix, ArchiveUnreachableReport(got))
	}
	if !censusHasTip(got, tip) {
		t.Errorf("the census did not report the fixture's orphaned tip %s under %s:\n%s",
			tip, localeLocalized, ArchiveUnreachableReport(got))
	}
	// And nothing was silently mis-filed as a degradation: a localized report
	// this census could not read must SAY so (Unrecognized), never render clean.
	if got.Unrecognized != 0 {
		t.Errorf("the census reported %d unclassifiable line(s) under %s — the locale pin is not reaching the git child:\n%s",
			got.Unrecognized, localeLocalized, ArchiveUnreachableReport(got))
	}
}

// TestGitChildLocaleEnvReplacesRatherThanAppends pins the ONE property that
// makes the pin work at all.
//
// A duplicated entry in the child's environment is decided by the READER, and
// not by one rule: glibc gettext and Go's os.Getenv take the FIRST copy while
// dash and bash take the LAST (measured 2026-10-16 — the reading and its host are in
// ChildLocaleDuplicateKeyRule, internal/tools/git_child_locale.go). So
// `append(os.Environ(), "LC_ALL=C")` is a pin only while the order AND the
// reader happen to agree, and `append` cannot express "the child's locale is C"
// at all. The helper must therefore REMOVE the inherited entries first — which
// is also why the assertion is "exactly one entry per name", not "the
// environment contains the pin": one entry is the property that holds for every
// reader, and it is the REASON for the strip, not a consequence of it.
//
// LANG and LC_ALL are the names the pin WRITES; LC_MESSAGES and LANGUAGE are
// the gettext selectors it STRIPS (LC_ALL outranks them, but leaving one
// pointing at a translation this process did not choose is a second, silent
// input to a parser that quotes English). Both halves are asserted: a pin that
// merely appended would leave two entries of the written name, and one that
// forgot to strip would leave a selector behind.
func TestGitChildLocaleEnvReplacesRatherThanAppends(t *testing.T) {
	t.Setenv("LANG", localeLocalized)
	t.Setenv("LC_ALL", localeLocalized)
	t.Setenv("LANGUAGE", localeLocalized)
	t.Setenv("LC_MESSAGES", localeLocalized)

	env := ChildLocaleEnv()
	counts := map[string]int{}
	values := map[string]string{}
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, childLocaleAssignment)
		if !ok {
			continue
		}
		if ChildLocaleEnvName(name) {
			counts[name]++
			values[name] = value
		}
	}
	// WRITTEN: exactly one entry each, carrying the pinned locale.
	for _, name := range ChildLocaleLocalePinnedNames {
		if counts[name] != 1 {
			t.Errorf("%s appears %d time(s) in the child env, want exactly 1 — a duplicated entry is decided by the READER (see ChildLocaleDuplicateKeyRule), so an inherited translation would still win for glibc while the shell took the pin: only one entry is a pin for every reader", name, counts[name])
		}
		if values[name] != ChildLocaleEnglish {
			t.Errorf("%s=%q in the child env — the pin must REPLACE it with %q", name, values[name], ChildLocaleEnglish)
		}
	}
	// STRIPPED: no entry at all.
	for _, name := range ChildLocaleSelectorNames {
		if counts[name] != 0 {
			t.Errorf("%s appears %d time(s) in the child env (%q), want 0 — the gettext selectors are stripped, never inherited", name, counts[name], values[name])
		}
	}
	// The pin is the same value production already uses for its own children:
	// one spelling of "byte-stable English diagnostics" (Rule 14).
	if ChildLocaleEnglish != bashDefaultLocale {
		t.Errorf("the git child locale %q must be the locale production pins on its bash children (%q) — two spellings of one policy",
			ChildLocaleEnglish, bashDefaultLocale)
	}
}

// TestRepoSweepGitPinsTheLocale is the guard for the SHARED runner: every git
// child it spawns must carry the pin, because its callers include parsers that
// match English diagnostics.
//
// It proves the pin by BEHAVIOUR rather than by reading the source: under a
// localizing locale the runner's child must answer in ENGLISH, while a git
// child given this very process's environment — the negative control — comes
// back translated. Without the control the assertion would pass on a host where
// the locale is merely not installed, proving nothing.
func TestRepoSweepGitPinsTheLocale(t *testing.T) {
	if !localeInstalled(localeLocalized) {
		t.Skipf("%s is not installed on this host, so git cannot localize its diagnostics here and this guard could not tell a pin from an absent translation (install it, or run on a host that has it)", localeLocalized)
	}
	repo := t.TempDir()
	localeCensusFixtureIn(t, repo)

	t.Setenv("LANG", localeLocalized)
	t.Setenv("LC_ALL", localeLocalized)

	// `git fsck --unreachable --no-reflogs` is the probe: its findings reach
	// STDOUT (which is what repoSweepGit returns) as `<kind> <type> <sha>`, whose
	// first field git translates. The fixture's orphan is what gives it
	// something to report.
	probe := []string{gitFSCKVerb, gitNoProgressArg, archiveUnreachableFSCKArg, archiveUnreachableNoReflogsArg}

	// NEGATIVE CONTROL: the same argv, the same cwd, the ambient environment —
	// git localizes, so the assertion below is a measurement of the pin rather
	// than a coincidence of this host.
	//
	// The probe is `git fsck --unreachable`, whose findings git reports on
	// STDOUT (repoSweepGit returns stdout; stderr rides the error), and whose
	// prose is exactly what gettext translates: `unreachable commit <sha>` in
	// the C locale, `不可达 commit <sha>` under this one.
	control := exec.Command("git", append([]string{"-C", repo}, probe...)...)
	// testutil.GitEnv (Rule 13/15, and the ONE repair for this class): the
	// control must inherit an AMBIENT environment so the localized LANG/LC_ALL
	// this test pinned still reach git, but it must NOT inherit a
	// repository-routing variable — a suite started from a shell that exported
	// GIT_DIR would otherwise route this child at SOMEONE ELSE'S repository
	// (2026-10-13: 12 fixture commits landed on real master; cmd.Dir cannot
	// override GIT_DIR). The helper strips exactly the routing names and keeps
	// everything else, so the measurement this control exists for is unchanged.
	control.Env = testutil.GitEnv()
	controlOut, controlErr := control.CombinedOutput()
	if controlErr != nil {
		// The control's EXIT STATUS is not part of the measurement (its prose
		// is), but a failing control has not run: `git fsck` errors out on a
		// damaged object store, and a control that could not run would leave
		// the assertion below passing on silence. It must succeed here — the
		// fixture is empty commits and one orphan.
		t.Fatalf("the unpinned control (git %v) failed in %s: %v — the negative control did not run, so it could not prove that git localizes here\nit said: %q",
			probe, repo, controlErr, strings.TrimSpace(string(controlOut)))
	}
	if strings.Contains(string(controlOut), archiveUnreachableLinePrefix) {
		t.Fatalf("the unpinned control answered in ENGLISH under %s (got %q) — git is not localizing here, so this test could not distinguish a pin from an absent translation",
			localeLocalized, strings.TrimSpace(string(controlOut)))
	}

	// MEASURED: the shared runner's child answers in English anyway.
	got, err := repoSweepGit(repo, probe...)
	if err != nil {
		t.Fatalf("repoSweepGit %v: %v", probe, err)
	}
	if !strings.Contains(got, archiveUnreachableLinePrefix) {
		t.Errorf("the shared git runner's child did not answer in English under %s — the locale pin is not reaching it.\n"+
			"the child said: %q\n"+
			"the unpinned control said: %q\n"+
			"git translates its diagnostics, and parsers in this package match their ENGLISH text, so the runner must pin the locale (git_child_locale.go)",
			localeLocalized, strings.TrimSpace(got), strings.TrimSpace(string(controlOut)))
	}
}

// localeInstalled reports whether the host can actually make git localize: a
// locale named in the environment is not the same as one installed, and a host
// without it falls back to English (which would make every assertion above pass
// for the wrong reason). The caller SKIPS loudly on false, carrying the
// measurement it could not make (Rule 15).
//
// IT ASKS glibc, NOT `locale -a`, and the difference was MEASURED (2026-10-12,
// this epic). The listing spells this host's locale `zh_CN.utf8` while a caller
// writes `zh_CN.UTF-8`; the first version of this probe string-compared the
// listing, said "not installed", and BOTH guards below skipped — so the pin
// looked green while nothing had been measured, and only a manual
// `LANG=zh_CN.UTF-8 go test` showed the red state. Whether a locale is usable is
// what glibc itself answers when it is pinned to it; the same probe and the same
// reasoning as the sibling guard in cmd/agent (merge_locale_test.go,
// mergeLocaleIsGeneratable — one spelling of "can this host translate git").
func localeInstalled(locale string) bool {
	cmd := exec.Command("locale", "charmap")
	cmd.Env = append(localeEnvWithoutSelectors(), "LANG="+locale, "LC_ALL="+locale)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return !strings.Contains(string(out), localeUnavailableMarker)
}

// localeUnavailableMarker is what glibc prints when the locale cannot be set —
// the one string that separates "this host has no such locale" from a locale
// that merely spells its charmap differently.
const localeUnavailableMarker = "Cannot set"

// localeEnvWithoutSelectors is this process's environment with the four locale
// spellings removed, for a probe that pins its OWN locale (an inherited one
// would decide the answer before the pin was read).
func localeEnvWithoutSelectors() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, childLocaleAssignment)
		if ChildLocaleEnvName(name) {
			continue
		}
		out = append(out, kv)
	}
	return out
}
