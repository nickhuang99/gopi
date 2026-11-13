package config

import (
	"strings"
	"testing"
)

// TestHelpStatesThePathShapeContract — the `gopi -h` half of the RP-2 fix
// (end-user audit slice 8). Usage line 2 promises `gopi [options] input.txt
// Run one task read from a text file`; the `Inputs/"<prompt>"` row must state
// the OTHER half of the same contract — that a path-SHAPED positional is a
// FILE, so a missing one is refused by name rather than silently becoming the
// task text, and both deliberate spellings exist.
//
// The gate is worded on the FACTS the refusal implements (a "/" or a named
// file extension; the two escape spellings; the nonzero exit), not on a
// paraphrase, so the page cannot drift from the classifier the run path uses.
func TestHelpStatesThePathShapeContract(t *testing.T) {
	page := FormatUsage()
	if page == "" {
		t.Fatal("FormatUsage() rendered nothing")
	}
	// The contract sentence must live inside the Inputs block: the operator
	// asking "what IS this positional?" reads that block, and a claim parked
	// in an unrelated one would not answer them.
	start := strings.Index(page, "\nInputs:\n")
	if start < 0 {
		t.Fatalf("the page has no Inputs section:\n%s", page)
	}
	inputs := page[start:]
	if i := strings.Index(inputs, "\nOptions ("); i >= 0 {
		inputs = inputs[:i]
	}
	// Collapse the renderer's indents and soft wraps, so a claim is asserted
	// by its WORDS rather than by where the page happened to wrap.
	inputs = strings.Join(strings.Fields(inputs), " ")
	for _, want := range []string{
		"A token that LOOKS like a path",
		"is a FILE",
		"refused by name",
		"exit 1",
		"--prompt TEXT",
		`\@TEXT`,
		".txt",
		".md",
		".png",
	} {
		if !strings.Contains(inputs, want) {
			t.Errorf("the Inputs section does not state %q — the path-shape contract must be readable from `gopi -h`:\n%s", want, inputs)
		}
	}
	// The pre-existing promise must SURVIVE the edit (this row is the oracle
	// the usage gate quotes elsewhere).
	if !strings.Contains(inputs, "With no prompt and no stdin, gopi prints this help and exits 1.") {
		t.Error("the Inputs/<prompt> row lost the no-input promise it carried before the RP-2 edit")
	}
}
