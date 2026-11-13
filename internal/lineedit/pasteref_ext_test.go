package lineedit

// pasteref_ext_test.go — the reference's `source` and `size` fields: what the
// NEW renderer writes, and what the reader must still accept from the OLD data.
//
// COMPATIBILITY HAS ONE DIRECTION (operator ruling, 2026-10-06): new code reads
// old data, and an absent field is simply NOT SHOWN. Nothing here asserts that
// an old reader can read a new reference — the old reader no longer runs, and
// building for it would be building for a reader that does not exist.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/inputsource"
)

const (
	// pasteRefExtPath is the payload path these cases name. It deliberately
	// contains a SPACE and a directory whose name carries `=` — the two shapes
	// that break a parser cutting at the first space or the first `=`.
	pasteRefExtPath = "/home/o/paste dir/a=b.txt"
	// pasteRefExtOld is a reference in the shape the store wrote BEFORE this
	// extension: rune count, separator, path, nothing else. It is spelled as a
	// literal on purpose — it is historical data, and a variable built out of
	// today's constants would drift with today's format instead of pinning the
	// old one.
	pasteRefExtOld = "[paste 4095: /home/o/paste dir/a=b.txt]"
)

// TestPasteRef_RendersSourceAndSize pins the new shape field by field.
func TestPasteRef_RendersSourceAndSize(t *testing.T) {
	ref := PasteRefFor(PasteRef{Path: pasteRefExtPath, Bytes: 4095, Runes: 4001, Source: inputsource.Notify})
	want := "[paste 4001: " + pasteRefExtPath + " source=notify size=4095B]"
	if ref != want {
		t.Fatalf("PasteRefFor = %q, want %q", ref, want)
	}
	info, ok := PasteRefInfoFor(ref)
	if !ok {
		t.Fatalf("the reader did not recognise its own rendering: %q", ref)
	}
	if info.Path != pasteRefExtPath {
		t.Errorf("path = %q, want %q (a path with a space must survive)", info.Path, pasteRefExtPath)
	}
	if info.Runes != 4001 {
		t.Errorf("runes = %d, want 4001", info.Runes)
	}
	if info.Size != 4095 {
		t.Errorf("size = %d, want 4095", info.Size)
	}
	if info.Source != inputsource.Notify {
		t.Errorf("source = %q, want %q", info.Source, inputsource.Notify)
	}
}

// TestPasteRef_OldSpellingIsStillRead is the compatibility gate: the reader
// accepts a reference written before the extension, reporting only what it
// carries — an empty Source and a zero Size, never a guessed default.
func TestPasteRef_OldSpellingIsStillRead(t *testing.T) {
	info, ok := PasteRefInfoFor(pasteRefExtOld)
	if !ok {
		t.Fatalf("the reader refused an old-format reference: %q", pasteRefExtOld)
	}
	if info.Path != pasteRefExtPath {
		t.Errorf("path = %q, want %q", info.Path, pasteRefExtPath)
	}
	if info.Runes != 4095 {
		t.Errorf("runes = %d, want 4095", info.Runes)
	}
	if info.Size != 0 {
		t.Errorf("size = %d, want 0 — an old reference states no byte count, and inventing one is worse than showing none", info.Size)
	}
	if info.Source != "" {
		t.Errorf("source = %q, want empty — an old reference names no face", info.Source)
	}
	// The other readers agree with it: the path is found, the shape is accepted.
	if got := PasteRefPaths(pasteRefExtOld); len(got) != 1 || got[0] != pasteRefExtPath {
		t.Errorf("PasteRefPaths = %q, want [%q]", got, pasteRefExtPath)
	}
	if !HasPasteRef(pasteRefExtOld) {
		t.Error("HasPasteRef refused an old-format reference")
	}
	if srcs := SourcesIn(pasteRefExtOld); len(srcs) != 0 {
		t.Errorf("SourcesIn = %q, want none — an old reference carries no provenance", srcs)
	}
}

// TestPasteRef_MissingFieldsAreNotRendered is the rendering half of the same
// rule: a zero-valued field produces NO field, so the one renderer writes both
// shapes and the old spelling is reproducible.
func TestPasteRef_MissingFieldsAreNotRendered(t *testing.T) {
	ref := PasteRefFor(PasteRef{Path: "/s/p.txt", Bytes: 12, Runes: 12})
	if strings.Contains(ref, refSourceKey) {
		t.Errorf("rendering %q carries a source field for a PasteRef that names none", ref)
	}
	if !strings.Contains(ref, refSizeKey+"=") {
		t.Errorf("rendering %q dropped the size field, which this PasteRef does carry", ref)
	}
	// Both fields absent: byte-identical to the pre-extension spelling.
	bare := PasteRefFor(PasteRef{Path: "/s/p.txt", Runes: 12})
	if bare != "[paste 12: /s/p.txt]" {
		t.Errorf("PasteRefFor with no provenance and no size = %q, want the historic spelling", bare)
	}
}

// TestPasteRef_KeysInsideThePathAreNotFields pins the boundary the fields live
// on: a payload path may itself contain `source=` or `size=` (the store joins
// it from a session directory the operator names), and only a field preceded by
// the separator counts as one.
func TestPasteRef_KeysInsideThePathAreNotFields(t *testing.T) {
	path := "/tmp/source=weird/size=9B.txt"
	ref := PasteRefFor(PasteRef{Path: path, Bytes: 7, Runes: 7, Source: inputsource.Human})
	info, ok := PasteRefInfoFor(ref)
	if !ok {
		t.Fatalf("the reader refused %q", ref)
	}
	if info.Path != path {
		t.Errorf("path = %q, want %q — a key inside the path is part of the path", info.Path, path)
	}
	if info.Source != inputsource.Human || info.Size != 7 {
		t.Errorf("source=%q size=%d, want %q / 7", info.Source, info.Size, inputsource.Human)
	}
}

// TestSourcesIn_ReadsEveryReference pins the multi-reference scan: a line
// carrying two spills of different provenance reports both, once each.
func TestSourcesIn_ReadsEveryReference(t *testing.T) {
	line := PasteRefFor(PasteRef{Path: "/s/a.txt", Bytes: 1, Runes: 1, Source: inputsource.Notify}) +
		" and " +
		PasteRefFor(PasteRef{Path: "/s/b.txt", Bytes: 2, Runes: 2, Source: inputsource.Human}) +
		" then " +
		PasteRefFor(PasteRef{Path: "/s/c.txt", Bytes: 3, Runes: 3, Source: inputsource.Notify})
	got := SourcesIn(line)
	want := []string{inputsource.Notify, inputsource.Human}
	if len(got) != len(want) {
		t.Fatalf("SourcesIn = %q, want %q (each face once, in first-appearance order)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SourcesIn = %q, want %q", got, want)
		}
	}
	if !SourcesAreKnown(line) {
		t.Error("SourcesAreKnown refused the vocabulary's own values")
	}
}

// TestSourcesAreKnown_RefusesAnUnknownFace is the Rule 15 half: a provenance
// outside the vocabulary is reported rather than silently treated as one of the
// three.
func TestSourcesAreKnown_RefusesAnUnknownFace(t *testing.T) {
	line := PasteRefFor(PasteRef{Path: "/s/a.txt", Bytes: 1, Runes: 1, Source: "somebody-else"})
	if SourcesAreKnown(line) {
		t.Errorf("SourcesAreKnown accepted %q, which is not in the vocabulary", line)
	}
	if got := SourcesIn(line); len(got) != 1 || got[0] != "somebody-else" {
		t.Fatalf("SourcesIn = %q, want the unknown value itself so the caller can name it", got)
	}
}

// TestPasteRef_SeparatorKeepsItsHistoricGap pins the human-visible half of the
// format: the gap after the colon is unchanged, so an old reference and a new
// one read the same way at the point the reader splits on.
func TestPasteRef_SeparatorKeepsItsHistoricGap(t *testing.T) {
	ref := PasteRefFor(PasteRef{Path: "/s/p.txt", Bytes: 5, Runes: 5, Source: inputsource.Human})
	if !strings.HasPrefix(ref, "[paste 5: /s/p.txt ") {
		t.Fatalf("reference = %q, want the historic `[paste <runes>: <path> ` prefix", ref)
	}
}
