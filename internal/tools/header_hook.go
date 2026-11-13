package tools

// header_hook.go — the write path's devdoc classification hook, NOW IN-PROCESS.
//
// WHAT IT USED TO COST. The hook shelled out to the corpus's generators
// (`python3 tools/docclass/classify.py --only <path>`, then `banner.py --only`):
// two process launches per written document, carrying a fixed startup cost each.
// Measured on this host over repeated writes: ~10.3s idle, 35.07s under load —
// which is why the hook had to be moved OUTSIDE the write's own deadline, its
// own bound introduced, and a defect class ("the caller was told the write timed
// out after it had succeeded") repaired. All of that machinery exists to make an
// EXPENSIVE hook survivable.
//
// WHAT IT COSTS NOW. The class lives in the file's own first line, so the hook
// is a read of that line and a parse against a closed vocabulary: no process,
// no interpreter, no I/O beyond the file the caller just wrote. The measured
// before/after numbers are in the change's report.
//
// WHAT IT DOES, exactly three things, and each is a RECORD rather than a write:
//
//	① an unwritten devdoc (a markdown file under devdoc/ with no class header)
//	   is REFUSED before the bytes land — the content the caller handed us is
//	   amended to open with `<!-- doc-class: historical -->`, the class a fresh
//	   record is by nature, and the amendment is reported back in the tool result
//	   so the author sees the class they are publishing;
//	② an existing document whose header is ILLEGAL is amended the same way;
//	③ every other outcome (a well-formed header, a non-prose record, a path
//	   outside the corpus) is a no-op, and NOTHING about the corpus is rewritten.
//
// It never re-decides a class the author already declared: the header IS the
// source of truth, so an existing well-formed banner is left exactly as it is
// (the ownership rule the old row-based hook needed — "a write may not
// re-adjudicate another writer's classification" — is now a property of the
// data instead of a guard that had to be remembered).
//
// WHAT IT NO LONGER HAS: the manifest. A devdoc under this repo once needed a
// row in DOC-INDEX.tsv, and this hook wrote it. There is no row any more; the
// header is the whole record, so a document needs nothing else to be complete.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/devdoc"
)

const (
	// devdocFreshClass is the class a NEW document gets when its author declared
	// none. A document being created during a run is, by nature, that run's
	// record; the author who wants a standing claim (normative), an intent
	// (roadmap) or an index entry (index-meta) writes the banner themselves, and
	// this constant never overrides a declared one.
	devdocFreshClass = devdoc.ClassHistorical
	// devdocBannerNote is the sentence a caller sees when the hook amended its
	// content. It names the class and the one edit that changes it: a default the
	// author cannot see is a default that silently spreads.
	devdocBannerNote = "added the corpus header %q on line 1 (a devdoc declares its truth class in its own first line; change it if this document is not a run's record)"
	// goModName is the marker this walk stops at — the same marker every other
	// corpus locator in this repo uses (docindex.CorpusPath, the standards gates).
	goModName = "go.mod"
	// devdocCorpusRoot is the corpus directory under that marker, taken from the
	// package that owns the vocabulary (Rule 14 — never a second spelling).
	devdocCorpusRoot = devdoc.Root
)

// corpusDocPath resolves an absolute path to (repo root, repo-relative slash
// path, whether it is a prose document under the corpus). The walk stops at the
// nearest `go.mod`, so a path outside any module is simply not corpus.
func corpusDocPath(path string) (root, rel string, ok bool) {
	if strings.TrimSpace(path) == "" {
		return "", "", false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", false
	}
	dir := filepath.Dir(abs)
	for {
		if _, serr := os.Stat(filepath.Join(dir, goModName)); serr == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
	rel, err = filepath.Rel(dir, abs)
	if err != nil {
		return "", "", false
	}
	rel = filepath.ToSlash(rel)
	if rel != devdocCorpusRoot && !strings.HasPrefix(rel, devdocCorpusRoot+"/") {
		return "", "", false
	}
	if !devdoc.IsProse(rel) {
		// A non-prose record (a script, a capture, a fixture) is historical by
		// definition and carries no banner a reader would meet — the gate's own
		// scope rule, asked here rather than re-typed.
		return "", "", false
	}
	return dir, rel, true
}

// amendDocHeader is the WRITE-TIME half: given the bytes a caller is about to
// land and the path they will land at, it returns the content to write instead
// and a note for the caller ("" when nothing changed).
//
// It looks at the path on disk too, and that is not redundant: editing an
// EXISTING document passes only the replacement bytes, so the first line of the
// content is often a heading the author wrote while the file's own line 1 is
// (correctly) absent from it — the header is added by the write path, not
// re-typed by every caller.
func amendDocHeader(absPath, content string) (string, string) {
	_, _, ok := corpusDocPath(absPath)
	if !ok {
		return content, ""
	}
	if _, err := devdoc.HeaderClass(firstLineOf(content)); err == nil {
		return content, ""
	}
	// The bytes about to land carry no legal header. A file already on disk may
	// carry one (an edit that replaces the body): keep it, and PREPEND it to the
	// content so the write does not drop a declaration the author made earlier.
	if onDisk, derr := devdoc.ReadHeaderClass(absPath); derr == nil && onDisk != "" {
		return devdoc.Banner(onDisk) + "\n" + content,
			fmt.Sprintf(devdocBannerNote, onDisk)
	}
	return devdoc.Banner(devdocFreshClass) + "\n" + content,
		fmt.Sprintf(devdocBannerNote, devdocFreshClass)
}

// preserveDocHeader is the EDIT half. An edit hands us the file's OLD content
// (already read) and the new content after the replacement; the header is
// carried across when the replacement dropped it.
//
// Why this is not amendDocHeader: that function treats content as something
// about to be CREATED, so a missing header means "give it the fresh class". An
// edit is a change to a document that already exists and already declared a
// class — so the class to restore is the file's OWN, read from the old bytes,
// and a document that had none (a pre-banner file, an on-disk document a caller
// is rewriting wholesale) falls back to the same fresh class a create would
// get. Restoring the file's own class rather than a default is the whole point:
// an edit to a normative document must not reclassify it as a run's record.
func preserveDocHeader(absPath, oldContent, newContent string) (string, string) {
	_, _, ok := corpusDocPath(absPath)
	if !ok {
		return newContent, ""
	}
	if _, err := devdoc.HeaderClass(firstLineOf(newContent)); err == nil {
		return newContent, ""
	}
	class := ""
	if c, cerr := devdoc.HeaderClass(firstLineOf(oldContent)); cerr == nil {
		class = c
	} else if onDisk, derr := devdoc.ReadHeaderClass(absPath); derr == nil {
		class = onDisk
	}
	if class == "" {
		class = devdocFreshClass
	}
	return devdoc.Banner(class) + "\n" + newContent, fmt.Sprintf(devdocBannerNote, class)
}

// firstLineOf is the first line of an in-memory string — the content a caller is
// about to write, which has no file to read yet.
func firstLineOf(content string) string {
	if i := strings.IndexByte(content, '\n'); i >= 0 {
		return content[:i]
	}
	return content
}
