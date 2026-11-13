package main

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"sort"
	"strings"

	"github.com/nick/gopi/internal/stylegate"
)

// annotatableRules are the rules whose violation is cleared by a documented
// reason comment.
var annotatableRules = map[string]bool{
	stylegate.RuleIgnoredError:   true,
	stylegate.RuleUncheckedClose: true,
	stylegate.RuleTestSetenv:     true,
	stylegate.RuleTestSkip:       true,
	stylegate.RuleTestSleep:      true,
}

func annotatable(rule string) bool { return annotatableRules[rule] }

// reasonPlaceholder is only used to build a marker prefix for the
// already-annotated check.
const reasonPlaceholder = ""

// lineHasMarker reports whether the file's line already carries the marker.
func lineHasMarker(file string, line int, marker string) bool {
	src, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	lines := strings.Split(string(src), "\n")
	if line < 1 || line > len(lines) {
		return false
	}
	return strings.Contains(lines[line-1], marker)
}

// markerFor picks the marker spelling a rule accepts. time.Sleep has its own
// marker because it is a documented exception, not an ignored error.
func markerFor(rule string) string {
	if rule == stylegate.RuleTestSleep {
		return stylegate.AllowMarker
	}
	return stylegate.IgnoreMarker
}

// ins is one comment insertion: a byte offset, or a negated line number
// meaning "insert a new line before that line".
type ins struct {
	offset int
	text   string
}

// annotate applies the sweep's explicit-ignore policy: every reported site
// NAMED BY ITS OWN TABLE KEY gets a reason comment (`//stylegate:ignore
// <reason>`), which is what turns an unhandled error into a documented,
// deliberate ignore. A site absent from the table is left UNTOUCHED and stays a
// finding — the tool never invents a justification.
//
// THE TABLE IS KEYED PER SITE (`file:line`) AND NOTHING ELSE. It used to fall
// back from `file:line` to `file::callee` to a bare `callee`, so one line of
// JSON silenced EVERY call to that function in the repository — 48 of the 64
// ignored-error sites on this ground were cleared by 30-odd callee keys, which
// is why the mechanism recurred: a group reason is not a reason, and a table
// that accepts one turns "justify this discard" into "name the callee once".
// A whole-file key (`file.go`) is refused for the same reason, one level up.
// The fallbacks are gone; an entry whose key is not `file:line` is a TABLE
// ERROR (the caller refuses it) rather than a silently-broader exemption.
//
// Every edit is comment-only: the proof is -prove, which re-scans the changed
// files and refuses any file whose token stream (comments stripped) differs
// from HEAD.
func annotate(root, reportPath, tablePath, onlyRule string) (int, error) {
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return 0, fmt.Errorf("read report: %w", err)
	}
	var rep stylegate.Report
	if err := json.Unmarshal(data, &rep); err != nil {
		return 0, fmt.Errorf("decode report: %w", err)
	}
	tableRaw, err := os.ReadFile(tablePath)
	if err != nil {
		return 0, fmt.Errorf("read reason table: %w", err)
	}
	var table map[string]string
	if err := json.Unmarshal(tableRaw, &table); err != nil {
		return 0, fmt.Errorf("decode reason table: %w", err)
	}
	if err := stylegate.ValidateReasonTable(table); err != nil {
		return 0, fmt.Errorf("%s: %w", tablePath, err)
	}

	byFile := map[string][]ins{}
	seen := map[string]bool{}
	unmatched := map[string]int{}
	applied := 0
	for _, f := range rep.Findings {
		if onlyRule != "" && f.Rule != onlyRule {
			continue
		}
		if !annotatable(f.Rule) {
			continue
		}
		marker := markerFor(f.Rule) + " " + reasonPlaceholder
		key := fmt.Sprintf("%s|%d", f.File, f.Line)
		if seen[key] {
			continue
		}
		// The ONLY accepted key is this site's own `file:line`. No callee-wide
		// and no file-wide fallback: see the doc comment above.
		reason := table[stylegate.SiteKey(f.File, f.Line)]
		if reason == "" {
			unmatched[f.Callee]++
			continue
		}
		// A line already carrying this rule's marker is annotated: two rules
		// can fire on the same line (an ignored error that is also an env set)
		// and a second marker would be noise, not information.
		if lineHasMarker(f.File, f.Line, markerFor(f.Rule)) {
			seen[key] = true
			continue
		}
		seen[key] = true
		marker = markerFor(f.Rule) + " " + reason
		if f.CommentSafe {
			byFile[f.File] = append(byFile[f.File], ins{offset: f.CommentOffset, text: " " + marker})
		} else {
			// No room after the statement: put the marker on its own line
			// directly above it (the scanner accepts that form too).
			byFile[f.File] = append(byFile[f.File], ins{offset: -f.Line, text: marker})
		}
		applied++
	}

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
		out, err := applyInsertions(src, byFile[rel])
		if err != nil {
			return applied, fmt.Errorf("%s: %w", rel, err)
		}
		fset := token.NewFileSet()
		if _, err := parser.ParseFile(fset, rel, out, parser.ParseComments); err != nil {
			return applied, fmt.Errorf("%s: refusing to write, result does not parse: %w", rel, err)
		}
		// Parsing is not enough. A marker inserted at a wrong offset can leave a
		// file that PARSES while having changed the program: measured 2026-09-27,
		// two shapes got through a parse check — a marker landing inside a string
		// literal ("e2e q2 beta //stylegate:ignore …?") and one splitting an
		// identifier ("dra" + marker + "inTestCtx(t, dir)"). Both are comment-only
		// edits that were not comment-only, and only execution found them. The
		// proof the tool already promises is cheap: strip comments from both sides
		// and refuse if the TOKEN STREAMS differ. That is the invariant the
		// annotation claims, so it is the one to check.
		if err := refuseNonCommentChange(src, out); err != nil {
			return applied, fmt.Errorf("%s: refusing to write, the annotation is not comment-only: %w", rel, err)
		}
		if err := os.WriteFile(rel, out, 0o644); err != nil {
			return applied, fmt.Errorf("write %s: %w", rel, err)
		}
		fmt.Printf("  %-58s %d annotation(s)\n", rel, len(byFile[rel]))
	}
	if len(unmatched) > 0 {
		keys := make([]string, 0, len(unmatched))
		for k := range unmatched {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Printf("unmatched callees (left untouched, still findings): %d\n", len(keys))
		for _, k := range keys {
			fmt.Printf("  %4d  %s\n", unmatched[k], k)
		}
	}
	return applied, nil
}

// applyInsertions applies comment insertions. A negative offset encodes a
// line number (-line) meaning "insert a new line before that line, indented
// like it".
func applyInsertions(src []byte, list []ins) ([]byte, error) {
	// Resolve EVERY insertion to an absolute byte offset against the ORIGINAL
	// source first, then apply back-to-front. Mixing byte offsets with line
	// numbers while the buffer mutates silently shifts the line insertions —
	// that misplacement happened once (batch 3) and is why this is one pass.
	type resolved struct {
		offset int
		text   string
	}
	all := make([]resolved, 0, len(list))
	for _, e := range list {
		r := resolved{offset: e.offset, text: e.text}
		if e.offset < 0 {
			start, indent, err := lineStart(src, -e.offset)
			if err != nil {
				return nil, err
			}
			r.offset = start
			r.text = indent + e.text + "\n"
		}
		all = append(all, r)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].offset > all[j].offset })
	out := append([]byte(nil), src...)
	for _, e := range all {
		if e.offset < 0 || e.offset > len(out) {
			return nil, fmt.Errorf("comment offset %d out of range", e.offset)
		}
		out = append(out[:e.offset], append([]byte(e.text), out[e.offset:]...)...)
	}
	return out, nil
}

// lineStart returns the byte offset of a 1-based line and its indentation.
func lineStart(src []byte, line int) (int, string, error) {
	off, n := 0, 1
	for n < line {
		i := indexByte(src[off:], '\n')
		if i < 0 {
			return 0, "", fmt.Errorf("line %d beyond end of file", line)
		}
		off += i + 1
		n++
	}
	end := off
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	return off, string(src[off:end]), nil
}

func indexByte(b []byte, c byte) int {
	return strings.IndexByte(string(b), c)
}

// refuseNonCommentChange is the annotator's own -prove: it compares the token
// stream of the annotated source against the original with comments stripped.
// An annotation may add comments and NOTHING else, so any difference — a marker
// that landed inside a string literal, or between an identifier's characters —
// is a refusal, not a warning. Two such shapes were measured on 2026-09-27 and
// both files PARSED, which is why a parse check is not the invariant.
func refuseNonCommentChange(before, after []byte) error {
	a, err := tokenStream(before)
	if err != nil {
		return fmt.Errorf("scan original: %w", err)
	}
	b, err := tokenStream(after)
	if err != nil {
		return fmt.Errorf("scan annotated: %w", err)
	}
	if len(a) != len(b) {
		return fmt.Errorf("token count %d -> %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			return fmt.Errorf("token %d changed: %q -> %q", i, a[i], b[i])
		}
	}
	return nil
}

// tokenStream renders a source's tokens (literals included, comments excluded)
// in order — the shape a comment-only edit must leave untouched.
func tokenStream(src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f := fset.AddFile("src", -1, len(src))
	var s scanner.Scanner
	s.Init(f, src, func(token.Position, string) {}, scanner.ScanComments)
	var out []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return out, nil
		}
		if tok == token.COMMENT {
			continue
		}
		out = append(out, tok.String()+"|"+lit)
	}
}
