package tools

import (
	"strings"
	"testing"
)

// The two spellings the filed claim is about. Named (Rule 1b) so the missing-word
// row and the assertion cannot drift apart.
const (
	andListWordMissing  = "gopi_no_such_command_zzz"
	andListExitPrefix   = "exit="
	andListSuccinctWord = "true"
)

// TestBash_AndListPropagatesTheFailure is the DISPROOF of a claim that was filed
// against this tree (Rule 41, task 01a12e6f-f2d8-7ebb-b940-10dd588acfd9's own
// subject row): that "`cmd && true` returns 0 even when `cmd` is not a command
// at all", so that "exit=0" is no evidence the command ran.
//
// MEASURED, and FALSE. bash's and-list returns the status of the LAST command
// ONLY WHEN IT RUNS; a failing left-hand side short-circuits the list and its
// status IS the list's status. So:
//
//	true            && true -> 0     (right side runs)
//	false           && true -> 1     (right side never runs; 1 is propagated)
//	<missing word>  && true -> 127   (127 is propagated — not 0)
//
// The shapes that DO mask a failure are different ones, and this test pins them
// too so the two families are never confused again: `|| true`, a pipeline whose
// final command succeeds, `; true`, `cmd && true || true` (the OR-LIST is what
// masks, not the and-list), and `local x=$(false)` inside a function.
//
// It asserts through OUR OWN surface — Bash, and therefore the `exit=N` line a
// model actually reads — rather than by invoking a shell outside the tool: the
// claim is about what THIS tool reports, so the tool is what must be measured
// (Rule 31: the surface you speak about is the surface you run).
func TestBash_AndListPropagatesTheFailure(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		// wantMasked is the claim under test: true = the wrapping shape hides the
		// inner failure and the result reads "exit=0".
		wantMasked bool
	}{
		{"and-list/propagates-failure", "false && " + andListSuccinctWord, false},
		{"and-list/propagates-missing-word", andListWordMissing + " && " + andListSuccinctWord, false},
		{"and-list/success-still-zero", andListSuccinctWord + " && " + andListSuccinctWord, true},

		// Contrast family — these are the shapes that really do mask.
		{"or-list/masks-failure", "false || " + andListSuccinctWord, true},
		{"or-list/masks-missing-word", andListWordMissing + " || " + andListSuccinctWord, true},
		{"or-list/masks-in-a-longer-chain", "false && " + andListSuccinctWord + " || " + andListSuccinctWord, true},
		{"list-separator/masks-failure", "false; " + andListSuccinctWord, true},
		{"pipeline/masks-failure", "false | " + andListSuccinctWord, true},
		{"assignment-swallows-substitution", "f(){ local y=$(false); }; f", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Bash(tc.cmd)
			if err != nil {
				t.Fatalf("Bash(%q) returned error %v (out=%q)", tc.cmd, err, out)
			}
			if !strings.HasPrefix(out, andListExitPrefix) {
				t.Fatalf("Bash(%q) = %q: want an %sN line first", tc.cmd, out, andListExitPrefix)
			}
			gotZero := strings.HasPrefix(out, andListExitPrefix+"0")
			if gotZero != tc.wantMasked {
				verb := "masks the failure"
				if !tc.wantMasked {
					verb = "PROPAGATES the failure"
				}
				t.Errorf("Bash(%q) = %q: want the shape to %s (exit=0? %v), got %q",
					tc.cmd, andListFirstLine(out), verb, tc.wantMasked, andListFirstLine(out))
			}
		})
	}
}

// TestBash_AndListTailProvesTheWordRan states the operational consequence of the
// measurement above, as a direct anti-regression against the filed claim: when
// the probe word DOES exist the and-list is 0, and when it does NOT exist the
// same command line is NONZERO. Same shape, two words, two different statuses —
// which is exactly the discrimination the claim said did not exist.
func TestBash_AndListTailProvesTheWordRan(t *testing.T) {
	realWord := gitBinaryName + " --version >/dev/null 2>&1 && " + andListSuccinctWord
	missingWord := andListWordMissing + " --version >/dev/null 2>&1 && " + andListSuccinctWord

	realOut, err := Bash(realWord)
	if err != nil {
		t.Fatalf("Bash(%q): %v", realWord, err)
	}
	missingOut, err := Bash(missingWord)
	if err != nil {
		t.Fatalf("Bash(%q): %v", missingWord, err)
	}

	if !strings.HasPrefix(realOut, andListExitPrefix+"0") {
		t.Errorf("the real word must read exit=0, got %q", andListFirstLine(realOut))
	}
	if strings.HasPrefix(missingOut, andListExitPrefix+"0") {
		t.Errorf("the MISSING word must NOT read exit=0 — that is the filed claim; got %q", andListFirstLine(missingOut))
	}
}

// firstLine keeps a failure message short when a command prints a lot.
func andListFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
