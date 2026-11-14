// ref_guard_shell_nesting_test.go — the acceptance set for the ref-guard family's
// PARSE LAYER (Rule 5 minimal repro; Rule 31 real surface).
//
// THE MEASURED BYPASS (row 01a1964f, 2026-11-01). With this run's ground
// announced and the shared checkout declared, on the revision this file was cut
// from (1d16b04):
//
//	git -C /home/user/work/gopi checkout -b task/x-legal
//	  → REFUSED ("cannot create branch \"task/x-legal\" … declared shared checkout")
//	OUT=$(git -C /home/user/work/gopi checkout -b task/x-legal 2>&1); echo "$OUT"
//	  → RAN. exit 0, and git executed (its own `fatal:` came back through $OUT).
//
// The root cause is a PARSE fact, not a missing spelling: refGuardClauses cut the
// line on `; & |`/newline only, so the second line produced ONE clause whose first
// word was `OUT=$(git`; refGuardGitSubcommand looks for `git` as the clause's
// command HEAD, found none, and the clause was dropped — the git invocation inside
// the substitution was never a clause at all.
//
// SO THESE TESTS HAVE THREE HALVES, and the split is the point:
//
//	…_ParseLayerCoversEveryNestingForm   the parse layer's own table: $( ),
//	                                     backticks, eval, bash -c and one nesting
//	                                     level deeper each produce a simple command
//	                                     the recogniser can read.
//	…_CommandSubstitutionIsRefused       the GREEN half through BashInDir — the
//	                                     real bash-tool entry point — so a case can
//	                                     only pass if the judgement is WIRED IN.
//	…_ReadOnlySubstitutionStillPasses    the no-weakening half: `$(git rev-parse …)`
//	                                     is the ordinary idiom and stays allowed,
//	                                     in the ground AND against the shared
//	                                     checkout.

package tools

import (
	"strings"
	"testing"
)

// The spellings these cases share, named once (Rule 1).
const (
	rgsnLegalBranch      = "task/x-legal"
	rgsnBranchSentence   = "cannot create branch"
	rgsnUnreadSentence   = "could NOT read a command nested inside"
	rgsnSharedCheckoutWr = "declared shared checkout"
)

// rgsnVerbs returns every git verb the parse layer + recogniser can reach in one
// command text — the parse layer's answer, without the guard's verdict.
func rgsnVerbs(command, workdir string) []string {
	var verbs []string
	for _, clause := range refGuardClauses(command, workdir) {
		for _, seg := range refGuardClauseSegments(clause) {
			words := seg.words
			verb, _, ok := refGuardGitSubcommand(refGuardUnwrapShellHead(words))
			if !ok || verb == "" {
				continue
			}
			verbs = append(verbs, verb)
		}
	}
	return verbs
}

// TestRefGuardShellNesting_ParseLayerCoversEveryNestingForm is the STRUCTURAL
// half: every nesting construct the shell executes yields a simple command whose
// verb the recogniser can read. A form added to the shell later is caught here,
// not by another spelling-specific row in the guard's own table.
func TestRefGuardShellNesting_ParseLayerCoversEveryNestingForm(t *testing.T) {
	const shared = "/home/user/work/gopi"
	for _, tc := range []struct {
		what    string
		command string
		want    string // the verb that MUST be reachable
	}{
		{"plain clause (the baseline)", "git -C " + shared + " checkout -b " + rgsnLegalBranch, gitCheckoutSubcmd},
		{"command substitution, assignment prefix", "OUT=$(git -C " + shared + " checkout -b " + rgsnLegalBranch + " 2>&1); echo \"$OUT\"", gitCheckoutSubcmd},
		{"command substitution as a bare word", "$(git -C " + shared + " checkout -b " + rgsnLegalBranch + ")", gitCheckoutSubcmd},
		{"command substitution inside double quotes", "echo \"$(git -C " + shared + " checkout -b " + rgsnLegalBranch + ")\"", gitCheckoutSubcmd},
		{"backtick substitution", "OUT=`git -C " + shared + " checkout -b " + rgsnLegalBranch + "`; echo \"$OUT\"", gitCheckoutSubcmd},
		{"eval body", "eval \"git -C " + shared + " checkout -b " + rgsnLegalBranch + "\"", gitCheckoutSubcmd},
		{"bash -c body", "bash -c 'git -C " + shared + " checkout -b " + rgsnLegalBranch + "'", gitCheckoutSubcmd},
		{"sh -lc body (short-flag cluster)", "sh -lc 'git -C " + shared + " checkout -b " + rgsnLegalBranch + "'", gitCheckoutSubcmd},
		{"interpreter named by path", "/bin/bash -c 'git -C " + shared + " checkout -b " + rgsnLegalBranch + "'", gitCheckoutSubcmd},
		{"two nesting levels", "OUT=$(bash -c 'git -C " + shared + " checkout -b " + rgsnLegalBranch + "'); echo \"$OUT\"", gitCheckoutSubcmd},
		{"compound-command keyword before the command", "if true; then git -C " + shared + " checkout -b " + rgsnLegalBranch + "; fi", gitCheckoutSubcmd},
		{"negation before the command", "! git -C " + shared + " checkout -b " + rgsnLegalBranch, gitCheckoutSubcmd},
		{"environment assignment prefix", "GIT_DIR=" + shared + "/.git git checkout -b " + rgsnLegalBranch, gitCheckoutSubcmd},
	} {
		t.Run(tc.what, func(t *testing.T) {
			verbs := rgsnVerbs(tc.command, shared)
			for _, got := range verbs {
				if got == tc.want {
					t.Logf("reachable verb %q in %q", got, tc.command)
					return
				}
			}
			t.Fatalf("the parse layer cannot reach git %q in %q — reached only %v", tc.want, tc.command, verbs)
		})
	}
}

// TestRefGuardShellNesting_UnreadNestingIsRefusedNotSkipped pins the fail-closed
// direction: an UNTERMINATED substitution is text the parse layer could not read
// to the end, and it must produce a refusal marker rather than vanishing. The
// original defect was exactly this — "I did not understand it" read as "there is
// nothing here to judge" (Rule 15).
func TestRefGuardShellNesting_UnreadNestingIsRefusedNotSkipped(t *testing.T) {
	const shared = "/home/user/work/gopi"
	command := "OUT=$(git -C " + shared + " checkout -b " + rgsnLegalBranch
	marked := false
	for _, clause := range refGuardClauses(command, shared) {
		for _, seg := range refGuardClauseSegments(clause) {
			if refGuardSegmentUnread(seg.words) {
				marked = true
			}
		}
	}
	if !marked {
		t.Fatalf("an unterminated substitution in %q produced no refusal marker", command)
	}
}

// TestRefGuardShellNesting_HeadUnwrapKeepsTheBranchName pins the wording half: the
// wrapper's closing delimiter must be stripped from the LAST word, otherwise the
// refusal would quote a branch name wearing a stray `)` and tell the caller the
// guard objected to something they did not write.
func TestRefGuardShellNesting_HeadUnwrapKeepsTheBranchName(t *testing.T) {
	words := []string{"OUT=$(git", "-C", "/repo", "checkout", "-b", rgsnLegalBranch + ")"}
	got := refGuardUnwrapShellHead(words)
	if len(got) != len(words) || got[0] != "git" {
		t.Fatalf("unwrapping must leave `git` as the command head, got %v", got)
	}
	if last := got[len(got)-1]; last != rgsnLegalBranch {
		t.Fatalf("the closing delimiter must be stripped from the branch name, got %q", last)
	}
	// A word that opens nothing is returned UNCHANGED, so a caller never guesses.
	plain := []string{"git", "status", "--porcelain"}
	if got := strings.Join(refGuardUnwrapShellHead(plain), " "); got != strings.Join(plain, " ") {
		t.Fatalf("a plain clause must be untouched, got %q", got)
	}
}

// rgsnGuardsOn pins BOTH switches the bash face honours, for the duration of a
// test (another test in this package sets RejectDangerous false, so a case that
// pinned only one would pass or fail by test ORDER — Rule 15).
func rgsnGuardsOn(t *testing.T) {
	t.Helper()
	prevEscape, prevReject := escapeDangerous, RejectDangerous
	escapeDangerous, RejectDangerous = false, true
	t.Cleanup(func() { escapeDangerous, RejectDangerous = prevEscape, prevReject })
}

// TestCloneGroundBranchCreate_CommandSubstitutionIsRefused is the RED-proof
// table, driven through BashInDir — the real bash-tool entry point. Every case
// carries the SAME legal branch name (`task/x-legal`), so a refusal can only come
// from the guard: git itself accepts the name (`git check-ref-format --branch`
// exits 0 for it), which is what makes this a bypass measurement rather than a
// re-measurement of git's own validation.
func TestCloneGroundBranchCreate_CommandSubstitutionIsRefused(t *testing.T) {
	rgsnGuardsOn(t)
	for _, tc := range []struct {
		what    string
		command func(shared, ground string) string
	}{
		{
			what: "the measured bypass: assignment + command substitution",
			command: func(shared, _ string) string {
				return "OUT=$(git -C " + shared + " checkout -b " + rgsnLegalBranch + " 2>&1); echo \"$OUT\""
			},
		},
		{
			what: "backtick substitution",
			command: func(shared, _ string) string {
				return "OUT=`git -C " + shared + " checkout -b " + rgsnLegalBranch + " 2>&1`; echo \"$OUT\""
			},
		},
		{
			what: "eval of a quoted command",
			command: func(shared, _ string) string {
				return "eval \"git -C " + shared + " checkout -b " + rgsnLegalBranch + "\""
			},
		},
		{
			what: "bash -c body",
			command: func(shared, _ string) string {
				return "bash -c 'git -C " + shared + " checkout -b " + rgsnLegalBranch + "'"
			},
		},
		{
			what: "sh -c body carrying a cd",
			command: func(shared, _ string) string {
				return "sh -c 'cd " + shared + " && git checkout -b " + rgsnLegalBranch + "'"
			},
		},
		{
			what: "two nesting levels",
			command: func(shared, _ string) string {
				return "OUT=$(bash -c 'git -C " + shared + " checkout -b " + rgsnLegalBranch + "'); echo \"$OUT\""
			},
		},
		{
			what: "switch -c inside a substitution",
			command: func(shared, _ string) string {
				return "OUT=$(git -C " + shared + " switch -c " + rgsnLegalBranch + " 2>&1); echo \"$OUT\""
			},
		},
		{
			what: "worktree add -b inside a substitution",
			command: func(shared, ground string) string {
				return "OUT=$(git -C " + shared + " worktree add -b " + rgsnLegalBranch + " " + ground + "-wt 2>&1); echo \"$OUT\""
			},
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			shared, ground := cgbcFixture(t)
			cgbcAnnounce(t, ground)
			headBefore := strings.TrimSpace(cgbcRun(t, shared, "rev-parse", "--abbrev-ref", "HEAD"))
			cmd := tc.command(shared, ground)

			out, err := BashInDir(cmd, ground)
			if err == nil {
				t.Fatalf("the guard must refuse %q, it ran: %s", cmd, out)
			}
			sentence := out + " " + err.Error()
			if !strings.Contains(sentence, rgsnBranchSentence) && !strings.Contains(sentence, rgsnUnreadSentence) {
				t.Fatalf("the refusal must carry the guard's own wording, got: %s", sentence)
			}
			if !strings.Contains(sentence, shared) || !strings.Contains(sentence, ground) {
				t.Fatalf("the refusal must name the shared checkout %q and the ground %q, got: %s", shared, ground, sentence)
			}
			// "Nothing was created" is MEASURED, not asserted from the wording.
			if headAfter := strings.TrimSpace(cgbcRun(t, shared, "rev-parse", "--abbrev-ref", "HEAD")); headAfter != headBefore {
				t.Fatalf("a refused creation must leave the shared checkout on %q, it is on %q", headBefore, headAfter)
			}
			if _, exists := cgbcTry(shared, "rev-parse", "--verify", "--quiet", cgbcRefHeads+rgsnLegalBranch); exists {
				t.Fatalf("a refused creation must create nothing, %s%s exists", cgbcRefHeads, rgsnLegalBranch)
			}
			t.Logf("refused as required: %s", strings.TrimSpace(sentence))
		})
	}
}

// TestCloneGroundBranchCreate_ReadOnlySubstitutionStillPasses is the
// NO-WEAKENING half. A guard that refused `$(git rev-parse --short HEAD)` would
// break the ordinary idiom every run uses to name a revision, and one that
// refused `git status --porcelain | wc -l` would refuse the close-out's own
// Rule 30 check. Each case asserts the command RAN (no error) and — where it
// prints — that the answer arrived.
func TestCloneGroundBranchCreate_ReadOnlySubstitutionStillPasses(t *testing.T) {
	rgsnGuardsOn(t)
	for _, tc := range []struct {
		what    string
		command func(shared, ground string) string
		want    string // substring the output must carry, "" when any output will do
	}{
		{
			what:    "read-only substitution in the ground",
			command: func(_, _ string) string { return "X=$(git rev-parse --short HEAD); echo \"$X\"" },
		},
		{
			what: "read-only substitution against the SHARED checkout",
			command: func(shared, _ string) string {
				return "X=$(git -C " + shared + " rev-parse --short HEAD); echo \"$X\""
			},
		},
		{
			what:    "read-only substitution inside double quotes",
			command: func(_, _ string) string { return "echo \"head=$(git rev-parse --short HEAD)\"" },
		},
		{
			what:    "status piped to wc",
			command: func(_, _ string) string { return "git status --porcelain | wc -l" },
		},
		{
			what: "status of the shared checkout piped to wc",
			command: func(shared, _ string) string {
				return "git -C " + shared + " status --porcelain | wc -l"
			},
		},
		{
			what:    "creation inside the run's own ground",
			command: func(_, _ string) string { return "git checkout -b task/x-inside-ground" },
		},
		{
			what: "creation inside the ground after an explicit cd",
			command: func(_, ground string) string {
				return "cd " + ground + " && git checkout -b task/x-after-cd"
			},
		},
		{
			what:    "plain checkout of an existing branch",
			command: func(_, _ string) string { return "git checkout -q " + cgbcIntegration },
		},
		{
			what:    "eval of a read-only command",
			command: func(_, _ string) string { return "eval \"git rev-parse --short HEAD\"" },
		},
		{
			what:    "bash -c of a read-only command",
			command: func(_, _ string) string { return "bash -c 'git rev-parse --short HEAD'" },
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			shared, ground := cgbcFixture(t)
			cgbcAnnounce(t, ground)
			cmd := tc.command(shared, ground)
			out, err := BashInDir(cmd, ground)
			if err != nil {
				t.Fatalf("must NOT refuse %q: %v\n%s", cmd, err, out)
			}
			if tc.want != "" && !strings.Contains(out, tc.want) {
				t.Fatalf("%q must still produce %q, got: %s", cmd, tc.want, out)
			}
			t.Logf("allowed as required: %q → %s", cmd, strings.TrimSpace(out))
		})
	}
}

// TestCloneGroundBranchCreate_LegacyBareSpellingStillRefused is the REGRESSION
// half (row 01a19643 / 0cfa412cd): the parse-layer change must not lose the bare
// spelling it was built around, nor the `-C` spelling the previous fix added.
func TestCloneGroundBranchCreate_LegacyBareSpellingStillRefused(t *testing.T) {
	rgsnGuardsOn(t)
	for _, tc := range []struct {
		what    string
		command func(shared, ground string) string
	}{
		{
			what: "bare spelling with a cd",
			command: func(shared, _ string) string {
				return "cd " + shared + " && git checkout -b " + rgsnLegalBranch
			},
		},
		{
			what: "git -C spelling (the 0cfa412cd fix)",
			command: func(shared, _ string) string {
				return "git -C " + shared + " checkout -b " + rgsnLegalBranch
			},
		},
		{
			what: "worktree add -b spelling",
			command: func(shared, ground string) string {
				return "git -C " + shared + " worktree add -b " + rgsnLegalBranch + " " + ground + "-wt2"
			},
		},
		{
			what: "branch verb with no flag at all",
			command: func(shared, _ string) string {
				return "git -C " + shared + " branch " + rgsnLegalBranch
			},
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			shared, ground := cgbcFixture(t)
			cgbcAnnounce(t, ground)
			cmd := tc.command(shared, ground)
			out, err := BashInDir(cmd, ground)
			if err == nil {
				t.Fatalf("the guard must still refuse %q, it ran: %s", cmd, out)
			}
			sentence := out + " " + err.Error()
			if !strings.Contains(sentence, rgsnBranchSentence) {
				t.Fatalf("the bare spelling must keep its OWN sentence (%q), got: %s", rgsnBranchSentence, sentence)
			}
			if !strings.Contains(sentence, rgsnSharedCheckoutWr) {
				t.Fatalf("the refusal must name the declaration that made the tree shared, got: %s", sentence)
			}
			if _, exists := cgbcTry(shared, "rev-parse", "--verify", "--quiet", cgbcRefHeads+rgsnLegalBranch); exists {
				t.Fatalf("a refused creation must create nothing, %s%s exists", cgbcRefHeads, rgsnLegalBranch)
			}
			t.Logf("refused as required: %s", strings.TrimSpace(sentence))
		})
	}
}
