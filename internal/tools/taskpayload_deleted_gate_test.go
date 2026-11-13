package tools

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// deletedAccessorNeedle is the identifier of the accessor removed by
// devdoc/taskpayload-dead-accessor.md, assembled from fragments so this gate
// file never carries the literal itself (a gate that names its needle in one
// piece would flag its own source the moment the scan widened to _test.go).
var deletedAccessorNeedle = "Task" + "Payload"

// deletedAccessorPattern matches the deleted identifier as an IDENTIFIER, not
// as a substring. The original form of this gate used strings.Contains, which
// was correct only while no other identifier carried the needle as an infix:
// master's sanitizeTaskPayloadText (internal/tools/tasksummarytext.go) and its
// call sites do, so a substring scan reports three production files that
// mention the SURVIVING, differently-named function. A gate that fails on
// correct code is a gate that gets deleted, so the predicate is anchored:
// \b…\b on a Go identifier boundary.
var deletedAccessorPattern = regexp.MustCompile(`\b` + deletedAccessorNeedle + `\b`)

// gateSkippedDirs are the trees that hold no production Go: the git object
// store, vendored copies, and build output.
var gateSkippedDirs = map[string]bool{
	".git":         true,
	"vendor":       true,
	"node_modules": true,
	"bin":          true,
}

// TestCovToolsXDeletedAccessorStaysDeleted is the Rule-13 regression gate for
// the dead exported accessor deleted in devdoc/taskpayload-dead-accessor.md.
//
// That accessor had ZERO production callers while its doc comment named `task
// run` as the caller — and `task run` actually reads TaskBriefing, whose doc
// records that summary+payload must come from ONE round-trip. Keeping a
// payload-only accessor would re-introduce exactly the two-accessors pattern
// that doc forbids, so the accessor was deleted rather than wired.
//
// The deleted identifier is spelled exactly once in this file, at
// deletedAccessorNeedle below — that assembly is why this doc comment names
// neither the function nor its wire form (this file is exempt from its own
// scan, but the acceptance grep `grep -rn <name> --include=*.go .` is not,
// and a gate that cannot be grep-verified against its own needle is weaker
// evidence than one that can).
//
// A constant that exists but is bypassed is a defect (Rule 13); a deleted
// accessor that silently reappears is the same defect in reverse. The
// identifier may therefore appear in NO production .go file repo-wide: a
// re-add means either the accessor is back without a real consumer (the drift
// returns) or someone re-derived the payload read instead of calling
// TaskBriefing (a mirrored definition — Rule 14). Either way, make it fail here
// so the decision is re-made deliberately, not by drift.
func TestCovToolsXDeletedAccessorStaysDeleted(t *testing.T) {
	root := filepath.Join("..", "..")
	var offenders []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A read the gate cannot perform must be loud, never silently
			// skipped: an unscanned tree is a gate that pretends to pass.
			return err
		}
		if d.IsDir() {
			if gateSkippedDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if deletedAccessorPattern.Match(data) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path // absolute path is still actionable
			}
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan repo for the deleted accessor: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("%s reappeared in production code: %v\n"+
			"TaskBriefing is the single payload reader (one round-trip, summary+payload); "+
			"if this is a deliberate re-add, ship a real caller and update "+
			"devdoc/taskpayload-dead-accessor.md",
			deletedAccessorNeedle, offenders)
	}
}
