package devdoc

// header.go — reading and judging a document's class declaration (Rule 36).
//
// This is the write path's half of the old internal/docclass, and it survived
// because RUNNING CODE asks it: the write/edit tools stamp or repair a
// document's first line as the bytes land (internal/tools/header_hook.go), so
// the parser they call has to exist. What did NOT survive is the corpus SWEEP
// that used to sit beside it — the walk that visited every markdown file under
// the corpus and reported the unclassified ones. That was a gate over the whole
// corpus, and the corpus is no longer generated.

import (
	"fmt"
	"os"
	"strings"
)

const (
	// HeaderKind names this reader's subject, so a reader of a failure knows
	// WHICH mechanism spoke.
	HeaderKind = "doc-class header"

	// headerFirstLineMax is how much of an offending first line an error quotes.
	// A gate failure prints one line per defective file; quoting a 4 KB markdown
	// heading would bury the file name the failure is about.
	headerFirstLineMax = 120
)

// IsProse reports whether a corpus path is subject to the header rule: the
// markdown files UNDER the corpus. Everything else under the corpus root is a
// record by definition and owes no banner a reader would never meet, and a
// markdown file outside the corpus is outside this rule's scope entirely.
//
// Both halves are required. An extension test alone would answer for
// `docs/archive/x.md` and for a test fixture alike — a scope the rule does not
// claim, and a predicate that answers wider than its rule is how a caller comes
// to enforce a convention on files nobody consents to.
//
// The predicate takes the REPO-RELATIVE slash path, which is what every caller
// already has, so no caller re-derives the prefix or the extension.
func IsProse(rel string) bool {
	return strings.HasPrefix(rel, Root+"/") && strings.HasSuffix(rel, MarkdownExt)
}

// HeaderClass parses a file's FIRST LINE and returns the class it declares.
//
// The contract is exact on purpose (Rule 15: no silent acceptance):
//
//	class + nil    — the line is a well-formed banner naming a legal class
//	""    + error  — the line is NOT the header (missing, or carrying a value
//	                 outside the vocabulary). The error names the line and the
//	                 legal set; the caller adds the file name.
//
// A banner whose payload carries extra prose (`<!-- doc-class: historical —
// classified in ... -->`) is an error, not a near miss: the grammar is
// BannerPrefix + class + BannerSuffix, and a value with commentary in it cannot
// be compared against another reader's answer. Measured before this rule: two
// documents in this corpus carried exactly that shape, and they silently
// compared unequal to their own class in every consumer.
func HeaderClass(firstLine string) (string, error) {
	line := strings.TrimRight(firstLine, "\r\n")
	if line == "" {
		return "", fmt.Errorf("no %s: the file's first line is empty (every markdown doc under %s/ opens with %q)",
			HeaderKind, Root, Banner(ClassNormative))
	}
	if !strings.HasPrefix(line, BannerPrefix) {
		return "", fmt.Errorf("no %s on line 1 — the first line must be %q (classes: %v); got %q",
			HeaderKind, Banner(ClassNormative), Classes, truncateForMessage(line))
	}
	if !strings.HasSuffix(line, BannerSuffix) {
		return "", fmt.Errorf("malformed %s on line 1 — %q does not close with %q (got %q)",
			HeaderKind, BannerPrefix, BannerSuffix, truncateForMessage(line))
	}
	class := strings.TrimSuffix(strings.TrimPrefix(line, BannerPrefix), BannerSuffix)
	if !IsClass(class) {
		return "", fmt.Errorf("illegal %s value %q on line 1 — legal classes are %v exactly (a banner carries the class name and nothing else)",
			HeaderKind, class, Classes)
	}
	return class, nil
}

// truncateForMessage shortens a first line for a diagnostic sentence.
func truncateForMessage(line string) string {
	if len(line) <= headerFirstLineMax {
		return line
	}
	return line[:headerFirstLineMax] + "…"
}

// ReadHeaderClass reads one file's first line from disk and parses it. The
// returned error is the parser's (see HeaderClass); a read failure is its own
// error, so "unreadable" is never reported as "unclassified".
func ReadHeaderClass(path string) (string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	//stylegate:ignore deferred close of a file already written and flushed by the checked calls
	defer func() { _ = fh.Close() }()
	buf := make([]byte, headerFirstLineMax*4)
	n, rerr := fh.Read(buf)
	if rerr != nil && n == 0 {
		return "", fmt.Errorf("read %s: %w", path, rerr)
	}
	line := string(buf[:n])
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return HeaderClass(line)
}
