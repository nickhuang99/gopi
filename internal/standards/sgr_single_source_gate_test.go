package standardgate

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sgr_single_source_gate_test.go — Rule 13/14 over the terminal escape
// vocabulary: internal/sgr is the ONE place a control sequence is spelled, and
// every other package reaches it by NAME.
//
// WHY this is a gate and not a paragraph. The duplication this task retired was
// invisible to the compiler: `feat/chat-turn-layout` declared its own ANSI
// family (`ansiDim = "\x1b[2m"`, a hand-composed `CSI row ; col H`) in the same
// tree where master had just landed internal/sgr for exactly that job. Two
// families type-check, both render, and the drift only shows up as a terminal
// that dims the wrong thing. Measured 2026-09-22: the branch's family was never
// on master (attr.go was V1's predecessor, re-landed as internal/sgr +
// screenattr.go), so the merge had nothing to resolve textually — the defect
// was only visible by asking "how many ways does this tree spell ESC[?".
//
// The predicate is deliberately SHAPE-BASED, not name-based: a literal is a
// literal whatever const it is assigned to, so a future package that invents
// `dimOn = "\x1b[2m"` is caught where a "no identifier called ansiDim" check
// would not be. Conversely a call (sgr.DimSeq()) and a reference to the sgr
// package's own constants are exactly what the rule PRESCRIBES, so they pass —
// which is the property that keeps the gate from degenerating into a ban on
// talking about escapes at all (comments naming `\x1b` are not literals).

// sgrLiteralOwners are the files allowed to contain an escape BYTE LITERAL, each
// with the reason it is the right home. A permit that no longer matches its
// reason fails the gate (the allow-list may only ratchet down).
var sgrLiteralOwners = map[string]string{
	// The single source of truth itself: CSI is the one spelling of ESC[, and
	// the builders beside it compose every sequence the project emits.
	"internal/sgr/sgr.go": "internal/sgr OWNS the escape vocabulary (Rule 14)",
}

// sgrSequenceFinals are the CSI final bytes. A string literal that carries one
// of them immediately after a `[` (or after digits and `;`) is a composed
// sequence, not prose.
const sgrEscapeByte = "\x1b"

// TestEscapeLiteralsLiveOnlyInSGR fails on any production .go file whose
// INTERPRETED or RAW string literals contain a raw ESC byte (0x1b) or a `\033`
// escape, outside internal/sgr.
func TestEscapeLiteralsLiveOnlyInSGR(t *testing.T) {
	root := repoRoot(t)
	files := goFilesUnder(t, root, []string{"internal", "cmd"}, false)
	if len(files) < 50 {
		t.Fatalf("scan found only %d production files — the scan is broken, not the tree", len(files))
	}
	scanned := 0
	for _, rel := range files {
		if _, ok := sgrLiteralOwners[rel]; ok {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if bytes.Contains(src, []byte(sgrEscapeByte)) {
			// The raw byte in the FILE (not only in a Go escape) — a literal
			// ESC is the same defect and is harder to see in a diff.
			t.Errorf("%s carries a raw ESC byte — the escape vocabulary lives in internal/sgr (Rule 13/14)", rel)
		}
		scanned++
		// Parse so a `\033` inside a COMMENT is not a hit (comments about
		// escapes are how the rule itself is explained) while a `\x1b` inside
		// a string literal is.
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, rel, src, parser.ParseComments)
		if perr != nil {
			t.Fatalf("parse %s: %v", rel, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if sgrLiteralCarriesEscape(lit.Value) {
				pos := fset.Position(lit.Pos())
				t.Errorf("%s:%d carries the escape spelling %s in a string literal — compose it through internal/sgr (Rule 13/14)",
					rel, pos.Line, strconv.Quote(lit.Value))
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("scanned no files — the gate would pass vacuously")
	}
}

// sgrLiteralCarriesEscape reports whether a Go string-literal TOKEN's source text
// spells a raw escape: a `\x1b`, `\033` (octal), or a `\u001b`/`\U0000001b`
// form. The literal is inspected as SOURCE (lit.Value keeps its quotes and its
// backslash spellings), so an interpreted value is not needed and a comment can
// never reach here.
func sgrLiteralCarriesEscape(v string) bool {
	lower := strings.ToLower(v)
	for _, spelling := range []string{`\x1b`, `\033`, `\u001b`, `\u00001b`, `\x1B`} {
		if strings.Contains(v, spelling) || strings.Contains(lower, strings.ToLower(spelling)) {
			return true
		}
	}
	return false
}

// TestEscapeLiteralsGateHasARedProof proves the gate is not vacuous: every
// spelling of a raw escape is reported, and the forms the rule PRESCRIBES are
// not. The red list is the family this task retired plus its spellings; the
// green list is what a correct caller writes — so the gate cannot be satisfied
// by deleting every mention of an escape.
func TestEscapeLiteralsGateHasARedProof(t *testing.T) {
	red := []string{
		`"\x1b[2m"`,      // the retired family's ansiDim (the flagship shape)
		`"\033[0m"`,      // the octal spelling of the same fact
		`"\u001b[1m"`,    // the unicode spelling
		`"\x1b[" + "2K"`, // a hand-composed sequence, half of it escaped
		`"x\x1b[?25h"`,   // a mode sequence built by hand
	}
	for _, spell := range red {
		if !sgrLiteralCarriesEscape(spell) {
			t.Errorf("the gate does NOT report %q — a shape it exists to catch (Rule 13/14)", spell)
		}
	}
	green := []string{
		`"m"`,                    // a final byte alone is a character, not a sequence
		`"[thinking] "`,          // the textual non-TTY marker is not an escape
		`"internal/sgr owns it"`, // prose
		`"\r"`,                   // CR is not an escape sequence
		`sgr.CursorSeq(1, 1)`,    // a CALL is the prescribed form, and not a literal
	}
	for _, spell := range green {
		if sgrLiteralCarriesEscape(spell) {
			t.Errorf("the gate FALSE-POSITIVES on %q — it would push an editor to delete a reference instead of fixing a literal", spell)
		}
	}
}

// TestSGRPackageOwnsTheSequenceBuilders pins the other half of Rule 14: the
// builders a caller needs must EXIST in internal/sgr, so "route it through sgr"
// is an available instruction rather than an aspiration. A package that has to
// hand-compose a CUP is a package that will declare its own family again.
func TestSGRPackageOwnsTheSequenceBuilders(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "internal", "sgr", "sgr.go"))
	if err != nil {
		t.Fatalf("read internal/sgr/sgr.go: %v", err)
	}
	for _, want := range []string{
		"func CursorSeq(",
		"func MoveCursorSeq(",
		"func ScrollRegionSeq(",
		"func ResetScrollRegionSeq(",
		"func EraseLineSeq(",
		"func EraseDisplaySeq(",
		"func PrivateModeSeq(",
		"func BracketedPasteSeq(",
		"func CursorShowSeq(",
		"func DimSeq(",
		"func ResetSeq(",
	} {
		if !bytes.Contains(src, []byte(want)) {
			t.Errorf("internal/sgr does not define %s — a caller needing that sequence would compose it by hand (Rule 14)", want)
		}
	}
}
