package devworkspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// THE REGRESSION THIS FILE GUARDS (epic 01a1847f): this package PARSES git's
// human-readable output — couldNotApplyMarker, gitHintLinePrefix, the conflict
// paths — so a git that answers in another language silently empties the parser
// instead of failing it. Measured on git 2.43.0 with the caller's environment
// exported as zh_CN.UTF-8:
//
//	错误：不能应用 4eaa590... ours      ← what the parser does NOT recognize
//	error: could not apply 4eaa590... ours   ← what it does
//
// The test is therefore written to run the REAL channel (AttemptDelivery: clone
// → pull --rebase → blocker) with a REAL conflict, in a Chinese locale, and to
// assert what the reader is handed, not what a string constant says.

// The locale this test exports into its own environment. It is a named constant
// (Rule 1) and it must be a locale the host actually carries — the probe below
// MEASURES that rather than assuming it.
const (
	zhCNLocaleEnvValue = "zh_CN.UTF-8"
	// zhCNSingleRevisionMarker is git's Chinese for the `fatal:` prefix on
	// `rev-parse --verify <missing ref>` — the cheapest way to ask a git child
	// "which language did YOU answer in?". Measured output on this host:
	// `致命错误：需要一个单独的版本`.
	zhCNSingleRevisionMarker = "致命错误"
	// zhCNCouldNotApplyMarker is git's Chinese spelling of the line
	// couldNotApplyMarker recognizes (`cannot apply`). Its PRESENCE in the
	// blocker's words is the defect this file guards.
	zhCNCouldNotApplyMarker = "不能应用"
)

// exportChineseLocale makes THIS process's environment Chinese for the rest of
// the test, so every child the package spawns inherits it — which is exactly
// the condition the defect needs. t.Setenv restores the previous values and
// forbids t.Parallel in this test (Rule 17).
func exportChineseLocale(t *testing.T) {
	t.Helper()
	t.Setenv(localeEnvName, zhCNLocaleEnvValue)
	t.Setenv(languageEnvName, zhCNLocaleEnvValue)
	t.Setenv("LANG", zhCNLocaleEnvValue)
}

// gitAnswersInChinese MEASURES whether a git child of this test, inheriting the
// test's own environment, really answers in Chinese. It spawns git directly —
// NOT through this package's runner — because it asks about the RUNNER's
// environment, not about the fix; a probe that went through the pinned runner
// would answer "English" no matter what the host does, and the test below would
// then pass for a reason that has nothing to do with the defect.
func gitAnswersInChinese(t *testing.T, dir string) bool {
	t.Helper()
	cmd := exec.Command(gitBinary, "rev-parse", "--verify", "refs/heads/"+gitLocaleProbeRef)
	cmd.Dir = dir
	// testutil.GitEnv starts from the caller's environment (so the locale under
	// test IS inherited — that is what is being measured) and strips the
	// repository-routing names, so the probe cannot be pointed at somebody
	// else's repository by a GIT_DIR in the suite's environment (the 2026-10-13
	// incident that helper exists for).
	cmd.Env = testutil.GitEnv()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the locale probe asked git for a ref that must not exist (%q) and it resolved: %s",
			gitLocaleProbeRef, out)
	}
	return strings.Contains(string(out), zhCNSingleRevisionMarker)
}

// gitLocaleProbeRef is the ref the probe asks for; it must never exist.
const gitLocaleProbeRef = "gopi-locale-probe-does-not-exist"

// TestPullConflictParsesInAnInheritedChineseLocale drives the channel end to end
// under the caller's Chinese locale and asserts the blocker carries git's own
// decisive line, in the ONE spelling the parser knows.
//
// RED BEFORE THE FIX (measured: see the epic's report): the words held
// `错误：不能应用 4eaa590... ours` and `Blocker.Words must carry git's own
// account of the conflict ("could not apply" missing)` was the failure.
func TestPullConflictParsesInAnInheritedChineseLocale(t *testing.T) {
	origin := newOrigin(t)
	exportChineseLocale(t)
	if !gitAnswersInChinese(t, origin) {
		t.Skipf("this host's git does not answer in Chinese under %s=%s — the defect cannot be exercised here "+
			"(install the locale, or run this test on a host that has it); skipping is stated, never silent (Rule 15)",
			localeEnvName, zhCNLocaleEnvValue)
	}

	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	// THE SAME LINE, BOTH SIDES — the same shape the English-locale test uses.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "README.md")
	gitIn(t, dir, "commit", "-q", "-m", "ours")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, origin, "add", "README.md")
	gitIn(t, origin, "commit", "-q", "-m", "theirs")
	// The commit the rebase will stop on is OURS: `pull --rebase` replays the
	// local commits onto the fetched remote, so git names this id.
	conflictCommit := strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD"))

	res, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("a content conflict is a BLOCKER, not an error: %v", err)
	}
	if blk == nil || blk.Kind != BlockerKindPull {
		t.Fatalf("want a pull blocker, got %+v (%+v)", blk, res)
	}

	// ① THE DECISIVE LINE SURVIVES — the whole point of the fix. Not a
	// translation of it: git's own English spelling, which is what the parser,
	// the decisive-line extractor and every downstream reader are keyed on.
	if !strings.Contains(blk.Words, couldNotApplyMarker) {
		t.Fatalf("Blocker.Words must carry %q even when the caller's locale is %s=%s (git answered in Chinese: "+
			"the parser recognized nothing); got %q",
			couldNotApplyMarker, localeEnvName, zhCNLocaleEnvValue, blk.Words)
	}
	// ② THE PARSER RECOVERS THE COMMIT, and it is the RIGHT one — a marker that
	// matched with an empty or wrong id would be a pass with no information.
	got := measuredConflictCommits(blk.Words)
	if len(got) != 1 {
		t.Fatalf("measuredConflictCommits must recover exactly one commit from the blocker, got %v (words: %q)", got, blk.Words)
	}
	if !strings.HasPrefix(conflictCommit, got[0]) {
		t.Fatalf("the recovered commit %q is not the commit the rebase stopped on (%q); words: %q",
			got[0], conflictCommit, blk.Words)
	}
	// ③ THE CHINESE SPELLING MUST NOT REACH THE READER AS THE ONLY ACCOUNT:
	// if git's localized line were still in the words while the English one was
	// too, the fix would be a duplication rather than a pin.
	if strings.Contains(blk.Words, zhCNCouldNotApplyMarker) {
		t.Fatalf("the blocker still carries git's localized line %q — the child was not pinned to %s=%s; words: %q",
			zhCNCouldNotApplyMarker, localeEnvName, cLocaleValue, blk.Words)
	}
	// ④ THE REST OF THE EVIDENCE IS STILL THERE (the fix must not trade one
	// criterion for another): the conflicted path is what a reader acts on.
	if len(blk.Paths) != 1 || blk.Paths[0] != "README.md" {
		t.Fatalf("a pull blocker still fills Paths with the conflicted path(s), got %q", blk.Paths)
	}
}

// TestPinChildLocaleOutranksTheCallersPrecedenceList pins the MECHANISM, not the
// symptom: the locale is a precedence LIST, so the pin must survive whatever
// combination the caller exported — including `LANGUAGE`, gettext's own list,
// which outranks every locale CATEGORY when the message locale is not C
// (measured: `LANG=en_US.UTF-8 LANGUAGE=zh_CN.UTF-8` answers `致命错误：…`, and
// `LANG=zh_CN.UTF-8 LANGUAGE=en_US.UTF-8` answers English). An explicit C pin
// was measured to beat the list in six shapes, so this test asserts the pin's
// SHAPE (one entry per name, everything else inherited) rather than a claim
// about which variable libc ranks first.
func TestPinChildLocaleOutranksTheCallersPrecedenceList(t *testing.T) {
	calls := []struct {
		name string
		env  []string
	}{
		{"caller exported all three", []string{
			localeEnvName + "=" + zhCNLocaleEnvValue,
			languageEnvName + "=" + zhCNLocaleEnvValue,
			languageListEnvName + "=zh_CN",
			"PATH=/usr/bin",
		}},
		{"caller exported only the list", []string{
			languageListEnvName + "=zh_CN",
			"PATH=/usr/bin",
		}},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			pinned := pinChildLocale(call.env)
			// The pin is present EXACTLY ONCE: a duplicated variable is a value
			// whose winner depends on libc internals (Rule 15: never leave the
			// answer to chance).
			for name, want := range map[string]string{
				localeEnvName:       cLocaleValue,
				languageEnvName:     cLocaleValue,
				languageListEnvName: "",
			} {
				var seen []string
				for _, kv := range pinned {
					if got, _, _ := strings.Cut(kv, "="); got == name {
						seen = append(seen, kv)
					}
				}
				if len(seen) != 1 || seen[0] != name+"="+want {
					t.Fatalf("%s must appear exactly once as %q, got %q (full env: %q)", name, name+"="+want, seen, pinned)
				}
			}
			// Everything else is INHERITED, untouched: the pin is a language
			// pin, not an environment reset.
			if !slices.Contains(pinned, "PATH=/usr/bin") {
				t.Fatalf("the caller's PATH must survive the pin, got %q", pinned)
			}
		})
	}
}

// TestDeclaredBuildRunsWithThePinnedLocale is the build child's arm of the same
// fact, and it exists because the pin reaches runBuild THROUGH run(): a build
// command is as free to translate as git is, and decisive_lines.go scans its
// output with an English vocabulary. Measured on this host, `make bad` answers
// `make: *** [Makefile:6：bad] 错误 1` under zh_CN and `… Error 1` under C — the
// `make: *** ` token survives both, `错误 1` matches no token at all.
//
// It asserts the DECLARED COMMAND IS UNCHANGED (the stamp text is exactly what
// was declared) and that the child that ran it was pinned — one criterion each,
// so neither can hide the other.
func TestDeclaredBuildRunsWithThePinnedLocale(t *testing.T) {
	dir := t.TempDir()
	exportChineseLocale(t)
	// One declared command that both reports its own locale and proves the
	// declaration is what ran (not a re-worded version of it).
	const declared = `printf 'BUILT-BY-DECLARATION LC_ALL=%s' "$LC_ALL"`
	out, err := runBuild(context.Background(), dir, declared, buildStampTimeout)
	if err != nil {
		t.Fatalf("the declared build command failed: %v (%s)", err, out)
	}
	want := "BUILT-BY-DECLARATION " + localeEnvName + "=" + cLocaleValue
	if out != want {
		t.Fatalf("runBuild produced %q, want %q — the build child did not get the pin, or the declared command "+
			"was not the one that ran", out, want)
	}
}

// buildStampTimeout bounds the build stamp above; a build that cannot finish in
// this long is a hang, not a slow build (Rule 1: a limit is named, never typed).
const buildStampTimeout = 30 * time.Second

// TestPinChildLocaleIsWhatARealGitChildSees is the END-TO-END arm: the pinned
// environment is handed to a real child and the child reports its own locale.
// A slice that is correct in Go but wrong in `env` is the failure this catches.
func TestPinChildLocaleIsWhatARealGitChildSees(t *testing.T) {
	cmd := exec.Command(shellBinary, "-c", `printf '%s|%s|%s' "$LC_ALL" "$LC_MESSAGES" "$LANGUAGE"`)
	cmd.Env = pinChildLocale([]string{
		localeEnvName + "=" + zhCNLocaleEnvValue,
		languageListEnvName + "=zh_CN",
		"PATH=/usr/bin:/bin",
	})
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v (%s)", shellBinary, err, out)
	}
	want := cLocaleValue + "|" + cLocaleValue + "|"
	if string(out) != want {
		t.Fatalf("the child reports LC_ALL|LC_MESSAGES|LANGUAGE = %q, want %q — the pin did not reach it", out, want)
	}
}
