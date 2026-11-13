package cliindex

// THE ANCHOR'S IDENTITY IS THE SYMBOL, NOT THE LINE — pinned at the renderer,
// where the identity is chosen.
//
// WHY THIS FILE EXISTS. COMMANDS.md's generated verb index published
// `cmd/agent/cli_tasks.go:528` for `gopi task`, and a line number is not an
// identity: it is the address of a name in ONE revision of ONE file, so any edit
// above it invalidates it. Measured, twice in one day: dcd25ce61 re-anchored
// that row at 09:50, and c6e93569e — a change to a DIFFERENT verb's help text —
// invalidated it four hours later (13:04). The gate went red, and the "fix"
// people kept applying was to write the number down again: eight rounds of the
// same repair, each resetting a timer instead of removing it. This is the unit
// guard that makes the repair impossible to reapply by accident: a Verb whose
// LINE changes must render the SAME anchor, because the line is not part of it.

import (
	"testing"

	"github.com/nick/gopi/internal/cliverbs"
)

// anchorProbe is a Verb shaped exactly like the ones Load builds for a routed
// verb (a constant in cmd/agent, a predicate in its own file).
func anchorProbe(constLine, predLine int) Verb {
	return Verb{
		Word:        "archive",
		Const:       "CliVerbArchive",
		ConstFile:   "cmd/agent/cli_verbs.go",
		ConstLine:   constLine,
		ConstAnchor: "CliVerbArchive",
		Predicate:   "ArchiveSubcommand",
		PredFile:    "cmd/agent/cli_archive.go",
		PredLine:    predLine,
	}
}

func TestRefCarriesTheSymbolNotALine(t *testing.T) {
	v := anchorProbe(52, 136)
	if got, want := v.Ref(), "cmd/agent/cli_verbs.go"+AnchorSep+"CliVerbArchive"; got != want {
		t.Errorf("Ref() = %q, want %q — the anchor must name the symbol", got, want)
	}
	if got, want := v.PredRef(), "cmd/agent/cli_archive.go"+AnchorSep+"isArchiveSubcommand"; got != want {
		t.Errorf("PredRef() = %q, want %q — the anchor must name the predicate FUNCTION "+
			"(the contract map's value has its `is` prefix stripped, so the renderer must put it back; "+
			"getting this wrong renders isArchiveSubcommandSubcommand, a symbol no file declares)", got, want)
	}
}

// TestRefSurvivesALineMove is the drift this epic removed, at the unit level:
// every edit that moves the DECLARATION without renaming it must leave the
// anchor byte-identical. A line identity fails every one of these.
func TestRefSurvivesALineMove(t *testing.T) {
	before := anchorProbe(52, 136)
	for _, c := range []struct {
		name                string
		constLine, predLine int
	}{
		{"a member added above the constant", 53, 136},
		{"a comment block inserted in the predicate's file", 52, 200},
		{"the file's whole header rewritten", 1, 2},
	} {
		after := anchorProbe(c.constLine, c.predLine)
		if after.Ref() != before.Ref() || after.PredRef() != before.PredRef() {
			t.Errorf("%s: anchors moved (%q/%q → %q/%q) — the identity is still the line",
				c.name, before.Ref(), before.PredRef(), after.Ref(), after.PredRef())
		}
	}
}

// TestRefNamesTheDeclaringSymbol pins the ALIASED case, which is where a symbol
// anchor could produce a false red: `cli_verbs.go` declares
// `CliVerbWorker = cliverbs.Worker`, so the anchor that row publishes points at
// cliverbs.go — a file that declares `Worker`, not `CliVerbWorker` (Rule 14: the
// one definition, not the re-export). Reading the identifier off the declaration
// site is what makes the anchor resolvable at all.
//
// THE FIXTURE MOVED FROM `merge` TO `worker` (epic merge-removal, item 2): the
// `CliVerbMerge = cliverbs.Merge` pair it used to spell no longer exists in the
// tree, so a fixture naming it would prove the property about a shape nothing
// carries. The property being pinned — the anchor resolves to the DECLARING
// file and symbol, not to the re-export — is unchanged; only the live pair it is
// demonstrated on moved.
func TestRefNamesTheDeclaringSymbol(t *testing.T) {
	v := Verb{Word: cliverbs.Worker, Const: "CliVerbWorker", ConstFile: "internal/cliverbs/cliverbs.go",
		ConstLine: 56, ConstAnchor: cliverbs.Worker}
	if got, want := v.Ref(), "internal/cliverbs/cliverbs.go"+AnchorSep+cliverbs.Worker; got != want {
		t.Errorf("Ref() = %q, want %q", got, want)
	}
	// A Verb built without a recorded declaring symbol (a hand-made fixture, or
	// a word whose spelling is a literal) falls back to the constant's own name
	// rather than rendering an empty identity.
	lit := Verb{Word: "archive", Const: "CliVerbArchive", ConstFile: "cmd/agent/cli_verbs.go", ConstLine: 52}
	if got, want := lit.Ref(), "cmd/agent/cli_verbs.go"+AnchorSep+"CliVerbArchive"; got != want {
		t.Errorf("Ref() without ConstAnchor = %q, want %q (the constant's own name)", got, want)
	}
}
