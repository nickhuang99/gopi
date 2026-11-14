package gopilog

// gopilog_wiring_test.go — the LAYERING guards for the binary-briefing fix
// (PROJECT.md Rule 14: one shared definition, no mirroring; Rule 13: a constant
// that exists is referenced, never bypassed).
//
// gopilog is the RENDERER half of the defect and must stay a CONSUMER of the
// shared predicate: the decision "is this text?" lives in internal/textguard,
// used by both internal/gopilog and internal/tools. Two ways this wiring could
// silently rot are closed here:
//
//  1. a second, private implementation growing inside gopilog (a copy of the
//     predicate would drift from the ledger's, and the two views would then
//     disagree about the same session);
//  2. gopilog importing internal/tools to reach the ledger's copy — the
//     dependency that a leaf package exists to prevent (internal/tools imports
//     internal/gopilog-adjacent packages; the cycle is real).

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/textguard"
)

// importToolsRe matches an actual IMPORT of the ledger package: the quoted full
// path (a comment that merely names internal/tools does not carry the quotes).
var importToolsRe = regexp.MustCompile(`"github\.com/the operator/gopi/internal/tools"`)

// TestGopilogConsumesTheSharedPredicate pins that the guard is the textguard
// one — the same predicate internal/tools uses, exercised through the actual
// gopilog entry points.
func TestGopilogConsumesTheSharedPredicate(t *testing.T) {
	// The renderers must agree with the shared predicate byte-for-byte: a
	// string textguard.IsText accepts passes through unchanged, and one it
	// rejects never reaches the output raw.
	for _, s := range []string{
		"plain task",
		"briefing with CJK 中文 and emoji 🙂",
		"multi\nline\ttabs",
	} {
		if !textguard.IsText(s) {
			t.Fatalf("textguard.IsText(%q) = false — this test's premise is broken", s)
		}
		if got := sessionTaskText(s); got != s {
			t.Errorf("sessionTaskText(%q) = %q, want identity", s, got)
		}
	}
	binary := binaryPromptTask(elfPromptFixtureBytes)
	if textguard.IsText(binary) {
		t.Fatalf("textguard.IsText accepted an ELF head — the predicate is broken")
	}
	guard := sessionTaskText(binary)
	if strings.Contains(guard, "\x00") {
		t.Errorf("sessionTaskText left NUL bytes in %q", guard)
	}
	if !strings.Contains(guard, "\\x7fELF") {
		t.Errorf("sessionTaskText(%q) = %q, want the ELF magic bytes marked and visible", binary[:32], guard)
	}
}

// TestGopilogDoesNotImportToolsNorMirrorThePredicate is the layering gate: the
// package's own sources must (a) import the leaf package, (b) never import
// internal/tools, and (c) never re-implement the decision (no local
// IsText/IsPrintableRune/printable-range literals outside textguard).
func TestGopilogDoesNotImportToolsNorMirrorThePredicate(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	importsGuard, scanned := false, 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		body := string(src)
		scanned++
		if strings.Contains(body, `"github.com/nickhuang99/gopi/internal/textguard"`) {
			importsGuard = true
		}
		if importToolsRe.MatchString(body) {
			t.Errorf("%s imports internal/tools — the shared text decision lives in internal/textguard precisely so this cycle cannot exist", name)
		}
		if strings.HasSuffix(name, "_test.go") {
			continue // tests may name the predicate to assert on it
		}
		// A mirrored implementation would re-derive "printable" locally.
		for _, needle := range []string{"func IsText(", "func IsPrintableRune(", "func isPrintableRune(", "unicode.IsPrint"} {
			if strings.Contains(body, needle) {
				t.Errorf("%s re-implements the text predicate (%q) — reference internal/textguard instead (Rule 14: never mirror)", name, needle)
			}
		}
	}
	if !importsGuard {
		t.Errorf("no production file imports internal/textguard — the shared guard is not wired")
	}
	if scanned < 5 {
		t.Fatalf("scanned only %d files — the gate read the wrong tree", scanned)
	}
}
