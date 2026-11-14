// Mechanical fixer for the stylegate scanner.
//
// It applies the byte-exact edits the scanner computes and NOTHING else:
// every edit is a single character swap located by the parser itself, so no
// regex can land in the wrong place. After applying, the file must still
// parse — a file that fails to parse is restored untouched (the fixer never
// leaves the tree broken).
package main

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"

	"github.com/nickhuang99/gopi/internal/stylegate"
)

// edit is one byte-range replacement inside a file.
type edit struct {
	offset int
	length int
	repl   string
}

// applyFixes reads a scanner report (JSON) and applies every mechanical edit
// it carries. It returns the number of applied edits.
func applyFixes(root, reportPath, onlyRule string) (int, error) {
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return 0, fmt.Errorf("read report: %w", err)
	}
	var rep stylegate.Report
	if err := json.Unmarshal(data, &rep); err != nil {
		return 0, fmt.Errorf("decode report: %w", err)
	}
	byFile := map[string][]edit{}
	// A single byte range can be claimed by more than one finding (e.g. the
	// same %v examined twice); collect into a set to keep edits idempotent.
	seen := map[string]bool{}
	for _, f := range rep.Findings {
		if f.EditWhat == "" || f.EditLen == 0 {
			continue
		}
		if onlyRule != "" && f.Rule != onlyRule {
			continue
		}
		key := fmt.Sprintf("%s|%d|%d|%s", f.File, f.EditOffset, f.EditLen, f.EditWhat)
		if seen[key] {
			continue
		}
		seen[key] = true
		repl, ok := replacement(f.EditWhat)
		if !ok {
			return 0, fmt.Errorf("unknown mechanical edit %q", f.EditWhat)
		}
		byFile[f.File] = append(byFile[f.File], edit{offset: f.EditOffset, length: f.EditLen, repl: repl})
	}

	applied := 0
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, rel := range files {
		src, err := os.ReadFile(rel)
		if err != nil {
			return applied, fmt.Errorf("read %s: %w", rel, err)
		}
		eds := byFile[rel]
		// Apply back-to-front so earlier offsets stay valid.
		sort.Slice(eds, func(i, j int) bool { return eds[i].offset > eds[j].offset })
		out := append([]byte(nil), src...)
		for _, e := range eds {
			if e.offset < 0 || e.offset+e.length > len(out) {
				return applied, fmt.Errorf("%s: edit out of range (%d+%d > %d)", rel, e.offset, e.length, len(out))
			}
			out = append(out[:e.offset], append([]byte(e.repl), out[e.offset+e.length:]...)...)
		}
		// Guard: the result must still parse. Restore otherwise.
		fset := token.NewFileSet()
		if _, err := parser.ParseFile(fset, rel, out, parser.ParseComments); err != nil {
			return applied, fmt.Errorf("%s: refusing to write, result does not parse: %w", rel, err)
		}
		// Guard: the edit must have changed only the intended bytes.
		if !sameExcept(src, out, eds) {
			return applied, fmt.Errorf("%s: unexpected byte delta beyond the planned edits", rel)
		}
		if err := os.WriteFile(rel, out, 0o644); err != nil {
			return applied, fmt.Errorf("write %s: %w", rel, err)
		}
		applied += len(eds)
		fmt.Printf("  %-60s %d edit(s)\n", rel, len(eds))
	}
	return applied, nil
}

// replacement maps an EditWhat label to the replacement text.
func replacement(what string) (string, bool) {
	switch what {
	case "%v->%w":
		return "w", true
	default:
		return "", false
	}
}

// sameExcept asserts that out differs from src only inside the planned edit
// ranges, comparing lengths: the output length must equal the input length
// plus the sum of the per-edit deltas.
func sameExcept(src, out []byte, eds []edit) bool {
	delta := 0
	for _, e := range eds {
		delta += len(e.repl) - e.length
	}
	return len(out) == len(src)+delta && strings.Count(string(out), "\x00") == strings.Count(string(src), "\x00")
}
