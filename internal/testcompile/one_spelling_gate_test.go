package testcompile_test

// The exhaustive-literal gate for this package's ONE spelling (Rules 13/14).
//
// THE DEFECT THIS PINS (measured 2026-11-03, row 01a19d46): the test-compile
// command lived in TWO places in internal/tools — a literal in
// merge_build_gate.go and a `Sprintf` format in merge_union_gate.go — and the
// clone channel's delivery loop, which is where master actually lands, could
// read NEITHER, because `internal/tools` imports `internal/devworkspace` in 46
// files and the reverse edge would be a cycle. Two spellings of one command is
// exactly the drift Rule 13 forbids: both keep compiling after either side
// learns a new flag, and nothing notices.
//
// The scan is a WALK of the production tree and a substring test on the RAW
// BYTES, with comment lines dropped first. A byte scan is the right instrument
// HERE (unlike the AST scan internal/buildidentity uses for its linker keys),
// because the thing being pinned is a COMMAND LINE: it appears inside string
// literals, which an identifier-shaped AST query cannot see, and the one place a
// second copy would hide is exactly a string literal.
//
// Prose is not a violation and is not counted: this repository discusses this
// command in a dozen comments and devdocs (correctly — they explain WHY the leg
// exists). The gate is about code a caller can reach.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testcompile"
)

// gateScanRoots are the production trees. `cmd/` is included because a
// duplication there (a CLI helper shelling out to the same command) would be
// just as unreachable from this package.
var gateScanRoots = []string{"..", "../../cmd"}

// gateCommentPrefixes are the line-initial tokens that make a match prose. A
// line is prose when its FIRST non-space runes are one of these; a line whose
// comment starts after code still carries code and is therefore judged.
var gateCommentPrefixes = []string{"//", "/*", "*"}

// gateOwnPackage is the ONE file allowed to carry the spelling, relative to the
// scan root.
const gateOwnPackage = "testcompile/testcompile.go"

func TestTheCommandSpellingOccursExactlyOnce(t *testing.T) {
	// The needle is the FORMAT, because that is the fragment both spellings
	// necessarily share (a finished spelling could differ in the toolchain word
	// and still be a second copy).
	needle := testcompile.CommandFormat

	hits := map[string]int{}
	walked := 0
	for _, root := range gateScanRoots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				// testdata trees hold deliberately-broken fixtures and census
				// generators; they are not caller-reachable code.
				if info.Name() == "testdata" || info.Name() == "node_modules" {
					return filepath.SkipDir
				}
				// Dot-directories (.git, .cache, tool caches) are never the
				// source tree this gate is about, and walking .git is pure cost.
				if strings.HasPrefix(info.Name(), ".") && path != root {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			walked++
			for _, line := range strings.Split(string(raw), "\n") {
				trimmed := strings.TrimSpace(line)
				prose := false
				for _, p := range gateCommentPrefixes {
					if strings.HasPrefix(trimmed, p) {
						prose = true
						break
					}
				}
				if prose || !strings.Contains(line, needle) {
					continue
				}
				rel := filepath.ToSlash(path)
				hits[rel]++
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	// A SCAN THAT LOOKED AT NOTHING IS NOT A PASS (Rule 15): the floor makes a
	// broken root or a changed layout fail loudly instead of reading green.
	if walked < gateWalkFloor {
		t.Fatalf("scanned only %d production Go files, below the floor of %d — the walk is broken, not the tree clean",
			walked, gateWalkFloor)
	}

	found := 0
	for path, n := range hits {
		found += n
		if !strings.HasSuffix(path, gateOwnPackage) {
			t.Errorf("the command spelling appears in %s (%d line(s)) — it has ONE definition, in %s (Rule 13/14)",
				path, n, gateOwnPackage)
		}
	}
	if found != 1 {
		t.Fatalf("the command spelling occurs %d time(s) across %d file(s); want exactly 1 (in %s)",
			found, len(hits), gateOwnPackage)
	}
	t.Logf("scanned %d production Go files; the command spelling occurs exactly once", walked)
}

// gateWalkFloor is the minimum number of production Go files the walk must reach
// for its verdict to mean anything. It is deliberately well below the real count
// (measured 1044 production Go files across internal/ and cmd/ on 2026-11-03) so
// ordinary file churn never trips it, while an empty or misrooted walk does.
const gateWalkFloor = 500
