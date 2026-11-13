package devworkspace

// devworkspace_source_refspec_test.go — THE SOURCE HALF of the destination gate
// (the effect half is publish_ground_refspec_test.go).
//
// WHY BOTH: `publishGround` builds its argv inside its own body, so no test can
// read the argv a caller would get. What a test CAN read is the source text —
// and the class of defect this epic removed is exactly a SOURCE/PROSE
// disagreement: a paragraph saying "TWO destinations, one of them forced with
// `+`" above a body that names one. A behavioural test cannot see the second
// refspec until somebody pushes with it; a source test sees it the moment it is
// typed. The pair is the point: effect proves the one destination is what lands,
// source proves the function cannot grow a second one quietly.
//
// THE QUESTION THIS ASKS, in the only spelling that survives a refactor: it
// reads the DECLARED FUNCTION (from its signature to its closing brace) and
// asserts (a) it takes no ground parameter, (b) its argument list names exactly
// ONE `refsHeadsPrefix + …` destination, (c) that destination is the base, and
// (d) no `+` force prefix is built. A future author who adds a second refspec
// gets a red test that names the refspec it found — not a lint, a measurement of
// the command this repo documents.

import (
	"os"
	"strings"
	"testing"
)

// publishGroundSourcePath is the file whose publishGround this gate reads (Rule
// 1: the path is spelled once, and the reader fails loudly when it is gone
// rather than reading an empty string as "clean").
const publishGroundSourcePath = "devworkspace.go"

// publishGroundFuncName is the declared function this gate measures.
const publishGroundFuncName = "publishGround"

// publishGroundForcedPrefix is the `+` refspec force spelling the removed
// paragraph argued for. Its absence from the body is the claim.
const publishGroundForcedPrefix = `"+`

// TestPublishGroundSourceNamesExactlyOneDestination reads the declaration.
func TestPublishGroundSourceNamesExactlyOneDestination(t *testing.T) {
	body, err := os.ReadFile(publishGroundSourcePath)
	if err != nil {
		t.Fatalf("this gate reads %s's own source and cannot: %v — a scan that read nothing must not pass (Rule 15)",
			publishGroundSourcePath, err)
	}
	src := string(body)
	declared := declaredFuncSource(t, src, publishGroundFuncName)
	if declared == "" {
		t.Fatalf("no `func %s(` declaration found in %s — the gate is measuring nothing", publishGroundFuncName, publishGroundSourcePath)
	}

	// (a) NO DEAD PARAMETER: `ground` was a second formal that no byte of the
	// command referenced from 9ab17237e on.
	signature := declared[:strings.Index(declared, "{")]
	if strings.Contains(signature, ", ground ") || strings.Contains(signature, "ground, ") || strings.Contains(signature, "(ground ") {
		t.Fatalf("publishGround still accepts a `ground` parameter: %q — its command names one destination, so the "+
			"parameter is dead and the prose that explains it can only be false", signature)
	}

	// (b) EXACTLY ONE DESTINATION in the argument list.
	if got := strings.Count(declared, "refsHeadsPrefix +"); got != 1 {
		t.Fatalf("publishGround builds %d `refsHeadsPrefix +` destinations, want exactly 1 — it pushes ONE refspec, "+
			"and the comment above it says so:\n%s", got, declared)
	}
	// (c) …AND IT IS THE BASE.
	if !strings.Contains(declared, "refsHeadsPrefix + base") {
		t.Fatalf("publishGround's one destination is not `base`:\n%s", declared)
	}
	// (d) NO FORCE SPELLING. `+` is how a refspec is forced; the removed
	// paragraph's whole argument was that the second refspec needed it.
	if strings.Contains(declared, publishGroundForcedPrefix) {
		t.Fatalf("publishGround builds a FORCED (`+`) refspec:\n%s — nothing it pushes is rewritten in place, so a "+
			"force would only be papering over a non-fast-forward the loop is supposed to rebase away", declared)
	}
}

// declaredFuncSource returns the text of `func <name>(…)` from its `func` keyword
// to the closing brace of the body, or "" when the declaration is absent. Braces
// are counted from the body's opening brace, so a test can assert on ONE function
// rather than on the file (a second call site elsewhere must not satisfy it).
func declaredFuncSource(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "func "+name+"(")
	if start < 0 {
		return ""
	}
	open := strings.Index(src[start:], "{")
	if open < 0 {
		return ""
	}
	depth := 0
	for i := start + open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	return ""
}
