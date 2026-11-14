package tools

// The suite's git children must speak ONE locale, and assertion ① must be
// language-independent without being loosened.
//
// Two halves, one mechanism:
//
//	① TestSuitePinsTheGitChildLocale — the pin itself. It is installed in
//	   TestMain (toolsPinChildLocale) and inherited by every git child this
//	   binary spawns, INCLUDING the children the process under test spawns
//	   (gitPushbackGitRun → gitPeerRun inherits this process's environment —
//	   its own doc says so). MEASURED 2026-10-12 on host-b: without the pin,
//	   `LC_ALL=zh_CN.utf8 LANG=zh_CN.utf8` made
//	   TestPushbackUnresolvedRemoteNameIsDiagnosedByTheRealLane red with
//	   `fatal: 无法读取远程仓库。` in the refusal — the PRODUCT was right (it
//	   kept git's words) and the ASSERTION was the locale-dependent half.
//	   This test is the regression guard: delete the pin from TestMain and it
//	   fails, naming the ambient locale it would otherwise inherit.
//
//	② TestPushbackRefusalKeepsGitsOwnSentenceInAnyLanguage — the assertion
//	   itself, proven language-independent by RUNNING git under the very
//	   locale that broke it. It captures git's REAL sentence in-process (no
//	   ambient locale needed), then holds the production refusal to the
//	   predicate the lane test uses, and shows the predicate is still STRICT:
//	   a refusal whose git sentence was REPLACED still fails it. That is the
//	   property memory #6073 protects — the fix is "compare against git's own
//	   sentence", never "stop asserting the sentence".
//
// RULE 17: t.Setenv is used only at the top level of a test body (never inside
// a closure), where its undo scope IS the test — the same shape the rest of
// this package uses.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// pbcLocalizedLocale is a locale whose git diagnostics are NOT English — the
// one measured to break assertion ① on host-b (2026-10-12). It is a NAMED
// value, not a probe: the test asserts the failure mode, it does not search
// for a locale that happens to be installed.
const pbcLocalizedLocale = "zh_CN.utf8"

// pbcEnglishSentence is git's own wording for an unreachable peer in the C
// locale — the string the shipped assertion hardcoded. It appears here ONLY as
// the red-proof's negative half ("this is what a locale-dependent assertion
// would look for"), never as something the production refusal must carry: the
// lane embeds whatever git printed.
const pbcEnglishSentence = "Could not read from remote repository"

// pushbackKeepsGitSentence is the language-independent form of the lane test's
// assertion ①: the refusal must still carry git's OWN sentence, byte for byte,
// as the caller measured it.
//
// It is a predicate over (refusal, git's sentence) rather than a hardcoded
// phrase, and it is DEFINED ONCE so the lane test and this guard cannot drift
// apart (Rule 14) — the failure the drift would produce is precisely the one
// that was fixed: one half updated to the new spelling, the other still
// comparing against English.
//
// It is deliberately not "lenient": an empty sentence is FALSE, not vacuously
// true, and the comparison is exact substring containment — the refusal must
// quote git, not paraphrase it.
func pushbackKeepsGitSentence(refusal, gitSentence string) bool {
	sentence := strings.TrimSpace(gitSentence)
	if sentence == "" {
		return false
	}
	return strings.Contains(refusal, sentence)
}

// pbcGitEnvWithLocale returns the current environment with the message-locale
// variables replaced by `locale` (rather than appended): a duplicated entry is
// decided by the READER, not by one rule (glibc gettext and Go os.Getenv take
// the FIRST, dash and bash take the LAST — ChildLocaleDuplicateKeyRule,
// internal/tools/git_child_locale.go, measured 2026-10-16), so an appended
// override may never be the one the child reads — the same trap the suite's own
// bashChildEnv avoids by filtering before appending.
func pbcGitEnvWithLocale(locale string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "LANG", "LC_ALL", "LC_MESSAGES", "LANGUAGE":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "LANG="+locale, "LC_ALL="+locale)
}

// pbcFetchUnresolvablePeer runs the lane's REAL first git call (the
// peer-master fetch, the verb that fails before any push) at a peer name this
// machine cannot resolve, under the given locale, and returns git's raw stderr
// — MEASURED, so a caller can assert what git actually says instead of typing
// a phrase (which is what made the lane test locale-dependent).
func pbcFetchUnresolvablePeer(t *testing.T, client, target, locale string) string {
	t.Helper()
	cmd := exec.Command("git", gitPeerFetchVerb, gitPeerFetchForceFlag, gitPeerFetchNoTagsFlag,
		target, gitPeerMirrorRefspec())
	cmd.Dir = client
	cmd.Env = pbcGitEnvWithLocale(locale)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the fetch to %q must fail (it does not resolve on this machine); got %q", target, out)
	}
	return strings.TrimSpace(string(out))
}

// TestSuitePinsTheGitChildLocale is the REGRESSION GUARD for the pin. It fails
// if toolsPinChildLocale is removed from TestMain — the state in which every
// English-phrase assertion in this suite (assertion ① among them) is decided
// by the operator's ambient locale rather than by the product.
func TestSuitePinsTheGitChildLocale(t *testing.T) {
	for _, name := range []string{"LANG", "LC_ALL"} {
		if got := os.Getenv(name); got != toolsPinnedChildLocale {
			t.Errorf("%s must be pinned to %q for the whole suite (toolsPinChildLocale in TestMain), got %q — an unpinned locale makes every test that quotes git's English diagnostics depend on the host, and the git children the process under test spawns inherit this process's environment verbatim",
				name, toolsPinnedChildLocale, got)
		}
	}
	// The gettext selector is stripped, not left pointing at a translation the
	// suite did not choose (bashChildEnv strips the same four).
	if got, ok := os.LookupEnv("LANGUAGE"); ok {
		t.Errorf("LANGUAGE must be unset by the pin (a stray gettext selector is a second, silent input to a suite that quotes English sentences), got %q", got)
	}
	// And the pin must be the value the PRODUCTION bash child uses: one
	// spelling of "byte-stable English diagnostics" for both (Rule 14).
	if toolsPinnedChildLocale != bashDefaultLocale {
		t.Errorf("the suite's child locale %q must be the locale production pins on its own children (%q) — two spellings of one policy",
			toolsPinnedChildLocale, bashDefaultLocale)
	}
}

// TestPushbackRefusalKeepsGitsOwnSentenceInAnyLanguage drives the production
// refusal under a locale whose git diagnostics are localized, and holds it to
// the language-independent assertion ①.
func TestPushbackRefusalKeepsGitsOwnSentenceInAnyLanguage(t *testing.T) {
	w := newPBWorld(t)
	ctx := context.Background()

	// The locale that broke the shipped assertion — set for THIS test only
	// (top-level t.Setenv: its undo scope is the test).
	t.Setenv("LC_ALL", pbcLocalizedLocale)
	t.Setenv("LANG", pbcLocalizedLocale)

	gitSentence := pbcFetchUnresolvablePeer(t, w.client, pbGhostAlias+":~/work/GoPi", pbcLocalizedLocale)
	if gitSentence == "" {
		t.Fatal("git must say something about an unresolvable peer")
	}
	// If this locale is not installed on this host, git falls back to English
	// and the test would prove nothing about localization. It does NOT skip
	// silently (Rule 15): the skip message carries the measurement it could not
	// make, and the red proof it stands for is that git's sentence under this
	// locale is NOT the English phrase the shipped assertion compared against.
	if strings.Contains(gitSentence, pbcEnglishSentence) {
		t.Skipf("git did not localize under %s on this host (stderr stays English: %q) — the language-independence of assertion ① could not be exercised here; install the locale or run on a host that has it", pbcLocalizedLocale, gitSentence)
	}

	target := pbGhostAlias + ":" + w.server
	refusal := gitPushbackTargetFailureDetail(ctx, w.client, target,
		"server-master fetch: exit status 128: "+gitSentence)

	// ① the refusal keeps git's own words — compared against what git ACTUALLY
	// printed in this locale, which is the whole point of the fix.
	if !pushbackKeepsGitSentence(refusal, gitSentence) {
		t.Errorf("the refusal must keep git's own sentence verbatim under %s.\nrefusal: %q\ngit said: %q",
			pbcLocalizedLocale, refusal, gitSentence)
	}
	// The rest of the finding is locale-independent by construction (it names
	// things this process measured), so it must survive too.
	for _, want := range []string{pbGhostAlias, "origin", "sync_peers"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the finding must carry %q whatever the locale: %q", want, refusal)
		}
	}

	// THE PREDICATE IS STILL STRICT — the property memory #6073 guards. A
	// refusal whose git sentence was DROPPED (replaced by the lane's own
	// verdict) must still fail it: the fix changes WHAT is compared, never
	// WHETHER the sentence must be there.
	replaced := gitPushbackTargetFailureDetail(ctx, w.client, target,
		"server-master fetch: exit status 128: the transport failed")
	if pushbackKeepsGitSentence(replaced, gitSentence) {
		t.Errorf("a refusal that replaced git's sentence must NOT satisfy assertion ①, got %q", replaced)
	}
	// And the empty-sentence case is a FAILURE, never a vacuous pass: an
	// assertion that accepts a refusal with no measurement behind it is the
	// loosening this fix refuses to be.
	if pushbackKeepsGitSentence(refusal, "   ") {
		t.Error("an empty measured sentence must never satisfy assertion ①")
	}
}
