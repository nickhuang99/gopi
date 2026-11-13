// Package devdoc carries the devdoc corpus vocabulary a LIVE path still needs:
// the manifest's name, column line and row parser, the markdown extension, and
// the class banner.
//
// IT IS WHAT SURVIVED internal/docclass, and nothing more. That package held
// the devdoc truth-classification MACHINERY — the anchor-NUMBER baseline tables
// and their provenance/permits, the prose-anchor and prose-path-line baselines,
// the evidence tiers and their re-verification, the corpus walk and its
// coverage gates. Those gates policed a corpus the document pipeline generated,
// and the pipeline is gone (doc-pipeline removal slice 3/5): the gates went
// with the corpus, and their arithmetic — baseline columns, key spellings,
// permit reasons, drift reports — deliberately did NOT come here. A package
// that kept it "just in case" would keep the machinery alive without the thing
// it measured.
//
// WHAT IS LEFT IS ASKED BY RUNNING CODE, not by a gate over the corpus:
//
//   - the merge lane (cmd/agent/merge_union.go) resolves a conflicted manifest
//     by unioning both sides' rows, so it must PARSE a manifest — the header
//     line, the four tab-separated columns, the declared class;
//   - the preland lane (cmd/agent/merge_preland_gate.go) attributes a conflicted
//     document's truth-class, which needs the corpus root, the markdown
//     extension, the class names and the banner spelling;
//   - the write path's hook (internal/tools/header_hook.go) stamps and repairs
//     the class banner on a document as it lands (PROJECT.md Rule 36);
//   - the manifest-union gate (internal/tools/manifest_union_gate.go) proves a
//     landing did not lose a row.
//
// WHY a package and not literals at those call sites (Rule 14): the manifest
// path, the column line and the banner spelling are each read by more than one
// package, and a second spelling of any of them is a silent scope mismatch — a
// gate reading a file the writer does not produce. The package is a LEAF:
// constants, one row type, one parser, one first-line reader. It imports only
// the standard library, so no consumer can create a cycle.
package devdoc

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

const (
	// Root is the documented corpus.
	Root = "devdoc"

	// ManifestName is the manifest's file name inside Root.
	ManifestName = "DOC-INDEX.tsv"

	// Path is the manifest, repository-relative. Root and ManifestName are
	// composed rather than re-typed, so a rename moves both halves at once.
	Path = Root + "/" + ManifestName

	// ManifestHeader is the manifest's column line. The parser asserts it, so a
	// reformatted manifest fails loudly instead of parsing into zero rows.
	ManifestHeader = "path\tclass\treason\tevidence"

	// MarkdownExt is the extension of the corpus's prose documents — the only
	// files the banner convention applies to (Rule 1b: one spelling, or a second
	// one silently narrows a caller's scope).
	MarkdownExt = ".md"

	// ClassNormative marks a doc that makes a standing claim about master.
	ClassNormative = "normative"
	// ClassHistorical marks a past run's own record, frozen by nature.
	ClassHistorical = "historical"
	// ClassRoadmap marks intended future work, not a claim about the present.
	ClassRoadmap = "roadmap"
	// ClassIndexMeta marks a doc that routes or describes the corpus itself.
	ClassIndexMeta = "index-meta"

	// BannerPrefix / BannerSuffix bracket the class banner a document opens
	// with: `<!-- doc-class: historical -->`. An HTML comment, so it is
	// invisible in the rendered doc, and on line 1, so a reader meets the class
	// before the title (Rule 36).
	BannerPrefix = "<!-- doc-class: "
	// BannerSuffix closes the banner.
	BannerSuffix = " -->"
)

const (
	// manifestSep is the manifest's column separator: a TAB, named rather than
	// typed so no call site is one keystroke from a space (the parser would read
	// a space-separated line as a single path cell).
	manifestSep = "\t"
	// manifestCols is the manifest's column count: path|class|reason|evidence.
	manifestCols = 4
	// manifestColumnsHint names the columns in header order, so a refusal says
	// which one the line is missing.
	manifestColumnsHint = "path, class, reason, evidence"
	// scannerMaxBytes bounds one manifest line, as the deleted reader did: a
	// reason is a sentence and a spot-check is a path, but neither is a novel.
	scannerMaxBytes = 1024 * 1024
)

// Classes is the closed class vocabulary, in the order the manifest documents it.
var Classes = []string{ClassNormative, ClassHistorical, ClassRoadmap, ClassIndexMeta}

// Banner returns the class banner declaration for class.
func Banner(class string) string { return BannerPrefix + class + BannerSuffix }

// IsClass reports whether s is one of the declared classes.
func IsClass(s string) bool {
	for _, c := range Classes {
		if c == s {
			return true
		}
	}
	return false
}

// Row is one manifest row.
type Row struct {
	Path     string // repo-relative slash path, e.g. devdoc/hippocampus.md
	Class    string // one of Classes
	Reason   string // why this class
	Evidence string // normative rows: one spot-check; others: empty
	Line     int    // 1-based line in the manifest, for diagnostics
}

// ParseBanner returns the class named by line when line is a class banner, and
// "" otherwise. The grammar is exact — BannerPrefix + class + BannerSuffix — so
// a banner carrying extra prose is "no banner" here rather than a near match a
// second reader could compare unequal to.
func ParseBanner(line string) string {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, BannerPrefix) || !strings.HasSuffix(line, BannerSuffix) {
		return ""
	}
	class := strings.TrimSuffix(strings.TrimPrefix(line, BannerPrefix), BannerSuffix)
	if !IsClass(class) {
		return ""
	}
	return class
}

// FirstLine reads a file's first line ("" for an unreadable or empty file). A
// banner is only meaningful on line 1, so this is the whole accessor.
func FirstLine(path string) string {
	fh, err := os.Open(path)
	if err != nil {
		return ""
	}
	//stylegate:ignore deferred close of a file already written and flushed by the checked calls
	defer func() { _ = fh.Close() }()
	sc := bufio.NewScanner(fh)
	if !sc.Scan() {
		return ""
	}
	return sc.Text()
}

// ParseManifestLines parses a manifest's CONTENT rather than a file on disk.
//
// It takes bytes because every caller has bytes: the merge lane reads a
// conflicted path's two INDEX STAGES, and the manifest-union gate reads a blob
// out of a revision. None of them has a worktree file to hand it.
//
// The structural rules are exact (Rule 15): `#` comments and blank lines above
// the column line are tolerated, the column line is asserted once, and below it
// every line is a row of exactly manifestCols tab-separated cells naming a
// declared class. A line that does not fit is an ERROR with its line number —
// never a skipped row, because a silently half-read manifest shrinks a caller's
// scope to whatever happened to parse.
func ParseManifestLines(body []byte) ([]Row, error) {
	var rows []Row
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), scannerMaxBytes)
	line := 0
	seenHeader := false
	for sc.Scan() {
		line++
		text := sc.Text()
		if strings.HasPrefix(text, "#") {
			continue
		}
		if !seenHeader {
			if strings.TrimSpace(text) == "" {
				continue
			}
			seenHeader = true
			if text != ManifestHeader {
				return nil, fmt.Errorf("manifest line %d: unexpected header %q — want the %s columns", line, text, manifestColumnsHint)
			}
			continue
		}
		if problem := manifestLineProblem(line, text); problem != "" {
			return nil, fmt.Errorf("manifest %s", problem)
		}
		f := strings.Split(text, manifestSep)
		if !IsClass(f[1]) {
			return nil, fmt.Errorf("manifest line %d: unknown class %q — declare it in devdoc.Classes", line, f[1])
		}
		rows = append(rows, Row{Path: f[0], Class: f[1], Reason: f[2], Evidence: f[3], Line: line})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return rows, nil
}

// manifestLineProblem renders the ONE sentence every reader prints for a line
// the shape rule cannot accept, or "" when the line is a row.
//
// A blank line is named as such and carries NO field count: "1 field(s), want 4"
// for a line with no field at all sends the operator looking for a truncated row
// that does not exist (measured in the deleted reader).
func manifestLineProblem(lineNo int, line string) string {
	if strings.TrimSpace(line) == "" {
		return fmt.Sprintf("line %d: blank line, want %d tab-separated columns (%s)", lineNo, manifestCols, manifestColumnsHint)
	}
	if n := len(strings.Split(line, manifestSep)); n != manifestCols {
		return fmt.Sprintf("line %d: %d field(s), want %d (tab-separated)", lineNo, n, manifestCols)
	}
	return ""
}
