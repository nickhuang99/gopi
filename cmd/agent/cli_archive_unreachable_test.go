package main

// cli_archive_unreachable_test.go — the `--unreachable` census and the
// `--rescue-tip` rescue, driven through the REAL binary against a real repo.
//
// WHY A REAL BINARY AND NOT A UNIT TEST: the defect this surface exists for was
// invisible from the code — the difference between "a durable ref holds this
// tip" and "the reflog grace window is the only thing holding it" is a property
// of the REPO, and it was measured by running `git fsck`/`git reflog` over the
// real backlog. The probes below therefore build a repo whose tip is orphaned
// exactly the way the real ones were (a branch ref deleted while its reflog
// still names the commits), run the verb, and assert on the output an operator
// reads.
//
// RED-FIRST: before this change the verb's flag set was closed
// (archiveFlagMembers), so `task archive --unreachable` answered
// "unknown flag --unreachable" and exited 1 — the tests below fail against that
// binary by construction, which is the proof recorded in the report.
//
// THE AMBIENT LOCALE IS AN INPUT (measured 2026-10-12, epic 01a1306b): the
// census reads git's English word `unreachable` out of `git fsck`'s report, and
// git TRANSLATES that word. The guards below therefore drive the binary under a
// localizing locale rather than trusting whatever the invoking shell carried —
// see TestArchiveCLIUnreachableIsIndependentOfTheAmbientLocale.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// archUnreachFixtureWithOrphan builds a repo plus ONE orphan chain of two
// commits (tip = the second), deleted by branch ref the way the real backlog
// was produced. It returns the repo and the tip sha.
func archUnreachFixtureWithOrphan(t *testing.T, branch string) (string, string) {
	t.Helper()
	repo := archCLIRepo(t, "feat/other")
	archCLIGit(t, repo, "checkout", "-q", "-b", branch, "master")
	archCLIGit(t, repo, "commit", "-q", "--allow-empty", "-m", "orphaned work part one")
	archCLIGit(t, repo, "commit", "-q", "--allow-empty", "-m", "orphaned work part two")
	tip := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "HEAD"))
	archCLIGit(t, repo, "checkout", "-q", "master")
	archCLIGit(t, repo, "branch", "-D", branch)
	return repo, tip
}

// archUnreachExpire clears every reflog: the ONE command that drops the grace
// window the census classifies on.
func archUnreachExpire(t *testing.T, repo string) {
	t.Helper()
	archCLIGit(t, repo, "reflog", "expire", "--expire=now", "--all")
}

// TestArchiveCLIUnreachableReportsTheKeepingWindow is the census's end-to-end
// contract on the real command line: the same repo is measured BEFORE and AFTER
// the reflog expiry, and the classification must flip while the objects stay
// present (the census reports a keeping window, never a deletion).
//
// The reflog that holds the tip here is README's own HEAD reflog and not
// `refs/heads/<deleted>`: git deletes a branch's reflog WITH the branch, so the
// surviving entry is the one the checkout left behind — which is exactly the
// mechanism that keeps the REAL backlog tips alive (measured on the peer: 33 of
// 329 tips are named by a reflog, and none of those reflogs belongs to the
// deleted branch itself).
func TestArchiveCLIUnreachableReportsTheKeepingWindow(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo, tip := archUnreachFixtureWithOrphan(t, "feat/orphaned")
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	home := t.TempDir()
	short := tip[:12]

	out, err := runArchiveProbe(t, bin, home, repo, dbPath,
		CliVerbTask, "archive", "--unreachable", "--repo", repo)
	if err != nil {
		t.Fatalf("`gopi task archive --unreachable` exited non-zero: %v\n%s", err, out)
	}
	for _, want := range []string{
		"unreachable tips:    1",
		"kept by a reflog:    1",
		"past the reflog:     0",
		short,
		"reflog:",
		"orphaned work part two",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("census output is missing %q:\n%s", want, out)
		}
	}
	// READ-ONLY: the census wrote no ref.
	if tags := archCLITags(t, repo); len(tags) != 0 {
		t.Fatalf("the census minted %v", tags)
	}

	archUnreachExpire(t, repo)
	out, err = runArchiveProbe(t, bin, home, repo, dbPath,
		CliVerbTask, "archive", "--unreachable", "--repo", repo)
	if err != nil {
		t.Fatalf("census after reflog expiry exited non-zero: %v\n%s", err, out)
	}
	if !strings.Contains(out, "kept by a reflog:    0") || !strings.Contains(out, "past the reflog:     1") {
		t.Errorf("the classification did not flip after `git reflog expire --expire=now --all`:\n%s", out)
	}
	// The object is still there — `past-reflog` is a keeping window, not a loss.
	if typ := archCLIObjectType(t, repo, tip); typ != "commit" {
		t.Errorf("the tip's object type after expiry = %q, want commit", typ)
	}
}

// TestArchiveCLIUnreachableIsIndependentOfTheAmbientLocale is the REGRESSION
// GUARD for the 2026-10-12 flake, on the surface that broke: the census through
// THE REAL BINARY, under a locale whose git diagnostics are translated.
//
// THE DEFECT IT PINS (measured). `gopi task archive --unreachable` reads git's
// English word `unreachable` out of `git fsck`'s report. git translates that
// word, so under LANG=zh_CN.UTF-8 the SAME repo holding the SAME orphan printed
// `不可达 commit <sha>`, matched no row, and rendered
//
//	unreachable tips:    0
//	no unreachable tip — every commit is named by a ref
//
// — the census's clean-looking zero, for a repo whose tip its own fixture had
// just orphaned. That is why the test looked like a flake: the foreground shell
// carried LC_ALL=C while the suite runner carried LANG=zh_CN.UTF-8 with no
// LC_ALL, and the locale had never been pinned on the git children.
//
// The assertion is NOT loosened: the census must find the tip in EVERY locale,
// because the locale is an INPUT to a parser and the pin (internal/tools/
// git_child_locale.go) is what makes that input fixed. A host that cannot
// translate git skips LOUDLY, because on such a host this guard could not tell
// a pin from an absent translation.
func TestArchiveCLIUnreachableIsIndependentOfTheAmbientLocale(t *testing.T) {
	bin := gopiTestBinPath(t)
	if !archCLILocaleGeneratable(t, archCLILocaleProbe) {
		t.Skipf("%s cannot be set on this host, so git will not translate its diagnostics here and this guard could not tell a pinned locale from an untranslated host (install it, or run on a host that has it)", archCLILocaleProbe)
	}
	repo, tip := archUnreachFixtureWithOrphan(t, "feat/locale")
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	home := t.TempDir()
	short := tip[:12]

	// NEGATIVE CONTROL, printed: the ambient environment of THIS process, with
	// the locale left alone, must actually produce a translated report — without
	// this, a green assertion below could just mean the locale never reached
	// git.
	control := exec.Command("git", "-C", repo, archCLIFSCKVerb, archCLINoProgressVerb, archCLIUnreachableVerb, archCLINoReflogsVerb)
	control.Env = append(archCLIEnvWithoutLocale(os.Environ()), "LANG="+archCLILocaleProbe, "LC_ALL="+archCLILocaleProbe)
	controlOut, _ := control.CombinedOutput() //stylegate:ignore the negative CONTROL's own output is the subject: a non-zero exit is the untranslated state the arm has to observe, so the status is not a failure to report
	if !strings.Contains(string(controlOut), archCLITranslatedPrefix) {
		t.Fatalf("the unpinned control did not translate git's report under %s (got %q) — git is not localizing on this host, so this guard would prove nothing",
			archCLILocaleProbe, strings.TrimSpace(string(controlOut)))
	}

	out, err := runArchiveProbeInLocale(t, bin, home, repo, dbPath, archCLILocaleProbe,
		CliVerbTask, "archive", "--unreachable", "--repo", repo)
	if err != nil {
		t.Fatalf("`gopi task archive --unreachable` exited non-zero under %s: %v\n%s", archCLILocaleProbe, err, out)
	}
	for _, want := range []string{"unreachable tips:    1", "kept by a reflog:    1", short} {
		if !strings.Contains(out, want) {
			t.Errorf("the census under %s is missing %q — git's diagnostics are translated per the ambient locale and this verb PARSES them, so the locale must be pinned on the git child (internal/tools/git_child_locale.go):\n%s",
				archCLILocaleProbe, want, out)
		}
	}
	// And nothing was silently mis-filed as a degradation: an unreadable report
	// must ANNOUNCE itself (the census's Unrecognized note), so a green run whose
	// note fired would be a green for the wrong reason.
	if strings.Contains(out, "could not be classified") {
		t.Errorf("the census reported unclassifiable line(s) under %s — the locale pin is not reaching its git child:\n%s", archCLILocaleProbe, out)
	}
}

// archCLILocaleProbe is a locale whose git diagnostics are NOT English — a value
// with meaning (asserted on, printed in the skip message), so it is named rather
// than spelled at the use site. The same value and rationale as the sibling
// guards in internal/tools (git_child_locale_test.go) and cmd/agent's merge lane
// (merge_locale_test.go): one spelling of "a locale that translates git".
const archCLILocaleProbe = "zh_CN.UTF-8"

// archCLILocaleUnavailableMarker is what glibc prints when a locale cannot be
// set — the one string that separates "this host has no such locale" from a
// locale that merely spells its charmap differently.
const archCLILocaleUnavailableMarker = "Cannot set"

// archCLITranslatedPrefix is the translated first field of git's unreachable
// report — the bytes a parser reading `unreachable` cannot match.
const archCLITranslatedPrefix = "不可达"

// archCLILocaleEnvPrefixes are the four spellings that select a message
// catalogue, stripped before a probe pins its own.
var archCLILocaleEnvPrefixes = [...]string{"LANG=", "LC_ALL=", "LC_MESSAGES=", "LANGUAGE="}

// archCLIEnvWithoutLocale drops the locale spellings from env: a duplicated
// entry is decided by the READER, not by one rule (glibc gettext takes the
// FIRST, Go's os.Getenv takes the FIRST too, and only dash/bash take the LAST — ChildLocaleDuplicateKeyRule,
// internal/tools/git_child_locale.go, measured 2026-10-16), so appending a pin
// beside an inherited value does not override it for every reader.
func archCLIEnvWithoutLocale(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		drop := false
		for _, prefix := range archCLILocaleEnvPrefixes {
			if strings.HasPrefix(kv, prefix) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// archCLILocaleGeneratable reports whether this host can actually set the
// locale. It ASKS glibc rather than string-comparing `locale -a`: the listing
// spells this host's locale `zh_CN.utf8` while the caller writes `zh_CN.UTF-8`,
// so a listing comparison says "absent" on a host where it works perfectly
// (measured 2026-10-12 — the internal/tools guard skipped for exactly this
// reason while git translated fine). Same probe as merge_locale_test.go's
// mergeLocaleIsGeneratable.
func archCLILocaleGeneratable(t *testing.T, locale string) bool {
	t.Helper()
	cmd := exec.Command("locale", "charmap")
	cmd.Env = append(archCLIEnvWithoutLocale(os.Environ()), "LANG="+locale, "LC_ALL="+locale)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("`locale charmap` under LC_ALL=%s failed (%v): %s", locale, err, strings.TrimSpace(string(out)))
		return false
	}
	return !strings.Contains(string(out), archCLILocaleUnavailableMarker)
}

// runArchiveProbeInLocale is runArchiveProbe with the message locale REPLACED —
// the environment the failure needed: a localizing LANG with no LC_ALL to
// override it.
func runArchiveProbeInLocale(t *testing.T, bin, home, repo, dbPath, locale string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, familyAddressArgs(args)...)
	cmd.Dir = repo
	cmd.Env = append(archCLIEnvWithoutLocale(cliContractEnv(home)), "LANG="+locale)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// archCLIFSCKVerb and its three companions name the probe's argv once (Rule 1):
// the flag values carry meaning (they SELECT what git reports), so they are not
// spelled inline at the use site.
const (
	archCLIFSCKVerb        = "fsck"
	archCLINoProgressVerb  = "--no-progress"
	archCLIUnreachableVerb = "--unreachable"
	archCLINoReflogsVerb   = "--no-reflogs"
)

// TestArchiveCLIRescueTipMintsTheDurableRefAndItsReason drives the rescue
// through the real binary and reads BOTH refs back, then proves the point of the
// whole surface: the tip survives the act that would have destroyed it (a full
// reflog expiry plus `git gc --prune=now`).
func TestArchiveCLIRescueTipMintsTheDurableRefAndItsReason(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo, tip := archUnreachFixtureWithOrphan(t, "feat/orphaned2")
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	home := t.TempDir()
	wantBranch := "rescued-" + tip[:12]

	out, err := runArchiveProbe(t, bin, home, repo, dbPath,
		CliVerbTask, "archive", "--rescue-tip", tip, "--reason", tools.ArchiveReasonHandMistake,
		"--detail", "orphaned by a branch delete", "--repo", repo)
	if err != nil {
		t.Fatalf("`--rescue-tip` exited non-zero: %v\n%s", err, out)
	}
	if !strings.Contains(out, "archive/"+wantBranch) {
		t.Fatalf("the rescue did not name the durable ref it wrote:\n%s", out)
	}
	if got := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "refs/tags/archive/"+wantBranch)); got != tip {
		t.Errorf("the rescue tag points at %q, want %s", got, tip)
	}
	if reason := tools.ArchiveTagReason(archCLIReasonRecord(t, repo, wantBranch)); reason != tools.ArchiveReasonHandMistake {
		t.Errorf("recorded reason = %q, want %q", reason, tools.ArchiveReasonHandMistake)
	}

	// The measured effect, through the git that would have collected it.
	archUnreachExpire(t, repo)
	if o, err := exec.Command("git", "-C", repo, "gc", "--prune=now").CombinedOutput(); err != nil {
		t.Fatalf("gc: %v: %s", err, o)
	}
	if typ := archCLIObjectType(t, repo, tip); typ != "commit" {
		t.Fatalf("the rescued tip is GONE after a reflog expiry + gc (type=%q) — the rescue did not make it durable", typ)
	}
	out, err = runArchiveProbe(t, bin, home, repo, dbPath,
		CliVerbTask, "archive", "--unreachable", "--repo", repo)
	if err != nil {
		t.Fatalf("census after the rescue exited non-zero: %v\n%s", err, out)
	}
	if !strings.Contains(out, "unreachable tips:    0") {
		t.Errorf("the rescued tip is still in the backlog census:\n%s", out)
	}

	// Idempotent: a second rescue is REFUSED (nothing to rescue), exit 0, and
	// it names the durable holder — never a second tag.
	before := len(archCLITags(t, repo))
	out, err = runArchiveProbe(t, bin, home, repo, dbPath,
		CliVerbTask, "archive", "--rescue-tip", tip, "--reason", tools.ArchiveReasonHandMistake, "--repo", repo)
	if err != nil {
		t.Fatalf("a re-rescue must be a refusal (exit 0), got: %v\n%s", err, out)
	}
	if !strings.Contains(out, "rescue refused") || !strings.Contains(out, "refs/tags/archive/"+wantBranch) {
		t.Errorf("the refusal must name the durable holder; got:\n%s", out)
	}
	if after := len(archCLITags(t, repo)); after != before {
		t.Errorf("the refused rescue still wrote a tag: %d -> %d", before, after)
	}
}

// TestArchiveCLIUnreachableRefusesAFailedScan is the Rule 15 half on the real
// command line: a scan that cannot answer exits 1 and says why — never a clean
// zero that reads as "nothing to report".
func TestArchiveCLIUnreachableRefusesAFailedScan(t *testing.T) {
	bin := gopiTestBinPath(t)
	notARepo := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	home := t.TempDir()
	out, err := runArchiveProbe(t, bin, home, notARepo, dbPath,
		CliVerbTask, "archive", "--unreachable", "--repo", notARepo)
	if err == nil {
		t.Fatalf("a scan over a non-repo exited 0:\n%s", out)
	}
	if !strings.Contains(out, "--unreachable") {
		t.Errorf("the failure must name the mode that failed; got:\n%s", out)
	}
}

// TestArchiveCLIOperandlessModesAreSingleValued pins the parser contract that
// keeps the census and the rescue from being silently mixed with each other or
// with the sweeps: two modes on one command line is a misread flag, and a
// branch operand beside a mode is the same (the flag set is closed).
func TestArchiveCLIOperandlessModesAreSingleValued(t *testing.T) {
	ok, err := parseArchiveArgs([]string{"--unreachable"})
	if err != nil {
		t.Fatalf("`--unreachable` alone must parse (it is an operand-less mode): %v", err)
	}
	if !ok.Unreachable {
		t.Error("parseArchiveArgs dropped --unreachable — the mode would be accepted and ignored (Rule 15)")
	}
	if _, err := parseArchiveArgs([]string{"--unreachable", "--check"}); err == nil {
		t.Error("--unreachable and --check must be refused together")
	}
	// The mode is identified by its FLAG, and a mode never carries a branch:
	// the branch archive's own parse arm is what refuses a missing --reason, so
	// a value that ends up in the positional slot cannot reach the census (which
	// reads no operand at all).
	if o, err := parseArchiveArgs([]string{"--unreachable"}); err != nil || o.Branch != "" {
		t.Errorf("`--unreachable` must parse to a branch-less census mode: opts=%+v err=%v", o, err)
	}
	rescue, err := parseArchiveArgs([]string{"--rescue-tip", "deadbeef", "--reason", tools.ArchiveReasonHandMistake})
	if err != nil {
		t.Fatalf("`--rescue-tip` must parse: %v", err)
	}
	if rescue.RescueTip != "deadbeef" {
		t.Errorf("RescueTip = %q, want deadbeef", rescue.RescueTip)
	}
	if _, err := parseArchiveArgs([]string{"--rescue-tip", "deadbeef", "--reclaim"}); err == nil {
		t.Error("--rescue-tip and --reclaim must be refused together")
	}
	// The branch archive is unchanged by the new modes.
	if _, err := parseArchiveArgs([]string{"--bogus"}); err == nil {
		t.Error("an unknown flag must still be refused — the flag set stays closed")
	}
}
