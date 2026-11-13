package tools

// shared_integration_ref_guard_spelling_test.go — the acceptance set for the
// SPELLING hole in refGuardClauses/refGuardGitSubcommand (row 01a1964e, measured
// 2026-11-01; bug row 01a19650-53a3).
//
// THE DEFECT. Every guard face in this package reads a command through ONE
// scanner (refGuardClauses) and asks refGuardGitSubcommand whether a clause
// invokes git. That reader required the clause's FIRST WORD to be `git`, so:
//
//	git -C <shared> checkout -b X            → refused (the guard saw it)
//	OUT=$(git -C <shared> checkout -b X 2>&1) → RAN (the clause's first word was
//	                                           `OUT=$(git`, no `git` word was
//	                                           found, the clause was dropped)
//
// Two spellings of ONE command, two opposite verdicts. The scanner now descends
// into `$( … )`, `` `…` ``, a bare `( … )` group and the command line carried by
// `sh -c '…'` / `eval '…'`, so every spelling is judged by the same predicate.
//
// WHAT THIS FILE PINS, and why each half is separate:
//
//	TestRefGuard_WrappedSpellingsGetTheSameVerdict
//	    the measured hole: EVERY spelling must be REFUSED, and nothing may be
//	    created. Driven through BashInDir — the real bash-tool entry point — so a
//	    case can only pass if the scanner is WIRED IN, not merely corrected in a
//	    predicate beside it (Rule 31).
//	TestRefGuard_WrappedSpellingsInsideTheGroundArePermitted
//	    the no-weakening half: the same spellings creating a branch in the run's
//	    OWN ground are the ordinary way a ground is used and must stay allowed.
//	    Without it, "refuse everything that mentions a substitution" would pass
//	    the table above.
//	TestRefGuard_ScannerFindsTheCommandHeadBehindEveryWrapper
//	    the SCANNER-level claim on its own (no fixture, no git): a wrapped git
//	    invocation yields a clause whose command head IS `git`. This is the clause
//	    every guard face consumes — the shared-integration-ref face, the publish
//	    face and the branch-create face all read it — so the assertion belongs
//	    where the break was.

import (
	"path/filepath"
	"strings"
	"testing"
)

// The spellings are built from the family's own tokens where one exists (Rule 13:
// the guard's refusal wording and git's own words are named constants above; the
// spellings below are shell syntax, which has no constant home).
const (
	refGuardSpellingBranch   = "task/01a1964e-spelling"
	refGuardSpellingGroundBr = "feat/inside-my-ground"
)

// refGuardSpellingCase is ONE spelling of the same command: the name it is
// reported under, and a renderer taking the directory the invocation must name.
type refGuardSpellingCase struct {
	name string
	cmd  func(dir string) string
}

// refGuardSpellingCases renders one command per spelling, all naming the SAME
// directory and the SAME branch — the property the defect violated. The renderer
// takes the directory so a fixture case and a parser-only case share ONE table.
func refGuardSpellingCases(dir, branch string) []refGuardSpellingCase {
	return []refGuardSpellingCase{
		{
			// The control: the spelling the guard already read.
			name: "bare",
			cmd: func(d string) string {
				c := " -C " + d + " "
				return gitBinaryName + c + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch
			},
		},
		{
			// THE MEASURED HOLE, verbatim from the bug row.
			name: "assignment-command-substitution",
			cmd: func(d string) string {
				c := " -C " + d + " "
				return "OUT=$(" + gitBinaryName + c + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch + " 2>&1); echo \"$OUT\""
			},
		},
		{
			// The same substitution with no assignment in front of it.
			name: "bare-command-substitution",
			cmd: func(d string) string {
				c := " -C " + d + " "
				return "echo $(" + gitBinaryName + c + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch + ")"
			},
		},
		{
			// A DOUBLE-quoted substitution: the quotes are data, the substitution
			// inside them still runs — and it is the ordinary spelling.
			name: "double-quoted-command-substitution",
			cmd: func(d string) string {
				c := " -C " + d + " "
				return "OUT=\"$(" + gitBinaryName + c + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch + ")\"; echo \"$OUT\""
			},
		},
		{
			name: "backtick-substitution",
			cmd: func(d string) string {
				c := " -C " + d + " "
				return "OUT=`" + gitBinaryName + c + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch + " 2>&1`; echo \"$OUT\""
			},
		},
		{
			// A GROUP is a sequence of its own: it substitutes nothing, it only
			// groups, and the scanner must still read its body as commands.
			name: "paren-group",
			cmd: func(d string) string {
				c := " -C " + d + " "
				return "( " + gitBinaryName + c + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch + " )"
			},
		},
		{
			// The shell parses `sh -c`'s operand AFTER stripping its quotes.
			name: "sh-dash-c",
			cmd: func(d string) string {
				c := " -C " + d + " "
				return "sh -c '" + gitBinaryName + c + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch + "'"
			},
		},
		{
			name: "bash-dash-c",
			cmd: func(d string) string {
				c := " -C " + d + " "
				return "bash -c '" + gitBinaryName + c + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch + "'"
			},
		},
		{
			name: "eval",
			cmd: func(d string) string {
				c := " -C " + d + " "
				return "eval '" + gitBinaryName + c + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch + "'"
			},
		},
		{
			// A pass-through wrapper: `env` runs its remaining words untouched.
			name: "env-passthrough",
			cmd: func(d string) string {
				c := " -C " + d + " "
				return "env " + gitBinaryName + c + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch
			},
		},
		{
			// The run directory must CARRY INTO the substitution: the `cd` inside
			// it selects the shared checkout for the git that follows.
			name: "cd-inside-the-substitution",
			cmd: func(d string) string {
				return "OUT=$(cd " + d + " && " + gitBinaryName + " " + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch + " 2>&1); echo \"$OUT\""
			},
		},
	}
}

// TestRefGuard_WrappedSpellingsGetTheSameVerdict is the acceptance table: one
// directory, one branch name, every spelling, the SAME refusal.
func TestRefGuard_WrappedSpellingsGetTheSameVerdict(t *testing.T) {
	if escapeDangerous {
		t.Skip("GOPI_ALLOW_DANGEROUS is set: the guard shares that hatch by design, so the refusal cannot be observed in this process")
	}
	prevReject := RejectDangerous
	RejectDangerous = true
	t.Cleanup(func() { RejectDangerous = prevReject })

	for _, tc := range refGuardSpellingCases("", refGuardSpellingBranch) {
		t.Run(tc.name, func(t *testing.T) {
			// A fresh fixture per case: the permitted arm of the family really
			// creates a branch, and a shared fixture would leak that state.
			shared, ground := cgbcFixture(t)
			cgbcAnnounce(t, ground)
			// The cases are rendered against the fixture's own ABSOLUTE path — a
			// relative one would measure another directory (the trap the
			// branch-create set documents).
			cmd := tc.cmd(shared)

			out, err := BashInDir(cmd, ground)
			if err == nil {
				t.Fatalf("every spelling must be refused, %q ran: %s", cmd, out)
			}
			text := out + err.Error()
			if !strings.Contains(text, cgbcGuardRefusal) {
				t.Fatalf("the refusal must carry the guard's own wording (%q), got: %s", cgbcGuardRefusal, text)
			}
			// And nothing may have been CREATED by a refused spelling.
			head := strings.TrimSpace(cgbcRun(t, shared, "rev-parse", "--abbrev-ref", "HEAD"))
			if head != cgbcIntegration {
				t.Fatalf("%q was refused but the shared checkout moved to %q", cmd, head)
			}
			if _, exists := cgbcTry(shared, "rev-parse", "--verify", "--quiet", cgbcRefHeads+refGuardSpellingBranch); exists {
				t.Fatalf("%q was refused but %s%s exists", cmd, cgbcRefHeads, refGuardSpellingBranch)
			}
			t.Logf("refused: %s", strings.TrimSpace(text))
		})
	}
}

// TestRefGuard_WrappedSpellingsInsideTheGroundArePermitted is the no-weakening
// half: the SAME wrappers, a branch created in the run's own ground.
func TestRefGuard_WrappedSpellingsInsideTheGroundArePermitted(t *testing.T) {
	if escapeDangerous {
		t.Skip("GOPI_ALLOW_DANGEROUS is set: the guard shares that hatch by design")
	}
	prevReject := RejectDangerous
	RejectDangerous = true
	t.Cleanup(func() { RejectDangerous = prevReject })

	cases := []struct {
		name string
		cmd  func(branch string) string
	}{
		{"bare", func(b string) string {
			return gitBinaryName + " " + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + b
		}},
		{"assignment-command-substitution", func(b string) string {
			return "OUT=$(" + gitBinaryName + " " + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + b + " 2>&1); echo \"$OUT\""
		}},
		{"backtick-substitution", func(b string) string {
			return "OUT=`" + gitBinaryName + " " + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + b + " 2>&1`; echo \"$OUT\""
		}},
		{"paren-group", func(b string) string {
			return "( " + gitBinaryName + " " + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + b + " )"
		}},
		{"sh-dash-c", func(b string) string {
			return "sh -c '" + gitBinaryName + " " + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + b + "'"
		}},
		{"eval", func(b string) string {
			return "eval '" + gitBinaryName + " " + gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + b + "'"
		}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ground := cgbcFixture(t)
			cgbcAnnounce(t, ground)
			// A distinct branch per case: the fixture is fresh, but a shared name
			// would make a re-run measure the previous branch's existence.
			branch := refGuardSpellingGroundBr + "-" + tc.name + "-" + string(rune('a'+i))
			cmd := tc.cmd(branch)
			out, err := BashInDir(cmd, ground)
			if err != nil {
				t.Fatalf("creating a branch in the run's OWN ground must stay permitted, %q: %v (%s)", cmd, err, out)
			}
			if _, exists := cgbcTry(ground, "rev-parse", "--verify", "--quiet", cgbcRefHeads+branch); !exists {
				t.Fatalf("the command was permitted but created nothing — the case proves nothing: %q", cmd)
			}
		})
	}
}

// TestRefGuard_ScannerFindsTheCommandHeadBehindEveryWrapper is the scanner-level
// claim, with no fixture: the clause a guard face consumes must have `git` as its
// command head for EVERY spelling. This is the break itself — the verdict tables
// above can only pass if this holds, and a future wrapper added to the tables
// without being added here fails HERE first, naming the spelling.
func TestRefGuard_ScannerFindsTheCommandHeadBehindEveryWrapper(t *testing.T) {
	shared := filepath.Join(string(filepath.Separator)+"shared-checkout-fixture", "repo")
	for _, tc := range refGuardSpellingCases(shared, refGuardSpellingBranch) {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.cmd(shared)
			// SEGMENTS are the parse layer's contract with the three guard faces
			// (ref_guard_shell_nesting.go): a nested construct's body is returned
			// as its own segment, so THAT is where a wrapped invocation must show
			// up. Asserting on clauses instead would pin an internal detail the
			// faces never read, and would go red on a correct implementation.
			// Empty workdir: every case names its directory absolutely, so nothing
			// here probes a repo.
			clauses := refGuardClauses(cmd, "")
			for _, clause := range clauses {
				for _, seg := range refGuardClauseSegments(clause) {
					verb, _, ok := refGuardGitSubcommand(refGuardUnwrapShellHead(seg.words))
					if ok && verb == gitCheckoutSubcmd {
						return
					}
				}
			}
			t.Fatalf("no segment of %q yields a git checkout command head; the scanner saw %v", cmd, refGuardClauseWords(clauses))
		})
	}
}

// refGuardClauseWords renders the clauses a failure message quotes, so a red says
// WHAT the scanner produced rather than only that it produced nothing useful.
func refGuardClauseWords(clauses []refGuardClause) []string {
	out := make([]string, 0, len(clauses))
	for _, c := range clauses {
		out = append(out, strings.Join(c.words, " "))
	}
	return out
}

// TestRefGuard_RefFaceSeesTheSameTargetBehindEverySpelling is the SECOND face's
// half of the same claim, at the seam that decides it: the shared-integration-ref
// guard does not read a command line, it reads the TARGETS
// refGuardRecognisedTargets extracts from it, and it refuses only what it was
// handed. Growing that extraction on one spelling and not another is exactly how
// a wrapped write reaches a shared ref unjudged, so every spelling must yield the
// SAME single target here — no fixture and no git, because this is a parse.
func TestRefGuard_RefFaceSeesTheSameTargetBehindEverySpelling(t *testing.T) {
	shared := filepath.Join(string(filepath.Separator)+"shared-checkout-fixture", "repo")
	branch := refGuardFixtureNewBranch
	// The sha is never read: update-ref's refname is the judged operand, and the
	// face judges the NAME (a nonexistent object is git's failure, not the guard's
	// question), so the value is the conventional placeholder.
	const refGuardFixtureDeadbeef = "deadbeef"
	for _, tc := range refGuardSpellingCases(shared, branch) {
		t.Run(tc.name, func(t *testing.T) {
			// The table spells `checkout -b`, which this face deliberately does not
			// judge (`-b` refuses an existing name, so git is already the guard for
			// it). The spelling question is about the WRAPPER, so each row's own
			// CREATE is re-spelled as the explicit ref verb — `git update-ref
			// refs/heads/<b> <sha>`, whose refname is read literally and needs no
			// probe — while the wrapper stays exactly as the row wrote it.
			create := gitCheckoutSubcmd + " " + taskBranchCreateFlagShort + " " + branch
			replacement := refGuardUpdateRefVerb + " " + refsHeadsPrefix + branch + " " + refGuardFixtureDeadbeef
			cmd := strings.Replace(tc.cmd(shared), create, replacement, 1)
			if cmd == tc.cmd(shared) {
				t.Fatalf("the row did not render the create this test replaces: %q", cmd)
			}
			targets, commands := refGuardRecognisedTargets(cmd, "")
			if len(targets) != 1 {
				t.Fatalf("%q must yield exactly one judged target, got %d (%v) from clauses %v",
					cmd, len(targets), targets, commands)
			}
			if got := targets[0].branch; got != branch {
				t.Fatalf("%q must judge branch %q, got %q", cmd, branch, got)
			}
		})
	}
}
