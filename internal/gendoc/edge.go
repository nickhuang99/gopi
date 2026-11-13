package gendoc

// edge.go — THE MACHINE-QUERYABLE EDGE between a source file's
// line numbers and the document that publishes them.
//
// (This file was internal/standards/generated_doc_deps.go; it moved here, to a
// leaf package, because the delivery path consumes it and a consumer in
// internal/tools importing internal/standards makes `go test
// ./internal/standards/` fail with "import cycle not allowed in test" — see
// internal/standards/export_test.go, which re-exports every name that package's
// own tests read. The content below is master's, ported forward, never rolled
// back.)
//
// THE DEFECT THIS EXISTS FOR (measured twice, task 01a12b80 → 01a12b86): a
// commit inserts a line above a function whose line number a document's
// generated block publishes as an anchor, and does NOT re-run the generator.
// The document goes stale, and nothing on the committing branch says so: the
// blocking check (`TestCommandsDocGeneratedBlockMatchesSource`) lives in THIS
// package, which the branch never runs, so the drift is discovered by whoever
// next runs the package — typically long after it landed on master, in a
// session that has no context to diagnose it.
//
// THE EDGE, in one line, derived and never typed:
//
//	anchor source file  →  the document that publishes its line numbers
//	                    →  the command that regenerates that document
//	                    →  the package whose test blocks on it
//
// Every one of the four legs is READ from the tree rather than listed here
// (Rule 1/13/14: a hand-typed list is the drift it is supposed to catch):
//
//   - the DOCUMENTS are the tracked markdown files that name a `go run …` of a
//     generator program — the instruction the document itself carries, which is
//     the only place the doc ⇄ generator pairing is stated;
//   - the GENERATOR is the program path in that instruction (`…/testdata/x.go`),
//     which must exist in the tree or the pair is reported as broken;
//   - the ANCHOR SOURCES are the `file.go:NNN` spans the document itself
//     carries — the files whose line numbers the document publishes;
//   - the GATE PACKAGE is the Go package that names this document in a test —
//     the package that must RUN for the drift to be caught.
//
// The read floors are the point (原则无效，检查才有效): a scan that discovered no
// pair, a document with no anchors, or a document no package asserts on, is each
// a FINDING rather than a silent empty answer.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// Marker names a generated region inside a document. A document
	// carrying one is regenerated, never hand-edited.
	Marker = "BEGIN GENERATED"
	// VerbIndexMarker is the sibling spelling the man sources use
	// (internal/mansource): the verb index region.
	VerbIndexMarker = "BEGIN VERB INDEX"
	// MinEdges is the read-floor for the DISCOVERY, and it belongs to the
	// discovery alone: only a document that STATES its own regeneration command
	// is an edge, and this tree has exactly one such document (COMMANDS.md), so
	// the floor answers "did the scan find the edge that exists?" — the NAMED
	// assertions beside it (the generator path, the anchor source, the gate
	// package) are what pin the edge's content, and a floor counted from the
	// corpus would rot the day a document is added.
	//
	// IT IS NOT A CONSUMER'S GATE, and this is the defect the Answer type below
	// exists for (measured 2026-10-12, row 01a12de0): a delivery path runs on ANY
	// deliverable.repo, and a consumer that reads "zero edges" as "the edge is
	// BROKEN" refuses every delivery in every tree that is not this one. A tree
	// with no edge is ABSENT from the edge, not broken in it; see
	// ClassifyParticipation.
	MinEdges = 1
	// MinNodes is the read-floor for the tree walk: fewer markdown
	// files than this and the walk did not happen.
	MinNodes = 2

	// BeginWord and EndWord are the two words a generated region's markers
	// are built from ("BEGIN GENERATED" … "END GENERATED"), so the end marker
	// of a region is DERIVED from its begin marker rather than listed a second
	// time (Rule 1b/14): a second spelling of the pair is the drift this
	// package exists to prevent.
	BeginWord = "BEGIN"
	EndWord   = "END"
	// CommentPrefix is the leading token of a Go comment line. A published
	// anchor that lands on one points at prose about a declaration rather than
	// at the declaration — the measured drift shape (see
	// regionAnchorFindings).
	CommentPrefix = "//"
	// AnchorBlankLineVerdict and AnchorCommentLineVerdict are the two verdict
	// fragments the numeric arm of anchorFindings has always carried, and
	// AnchorSeparatorOnlyVerdict is the third, added beside them.
	//
	// WHY THEY ARE NAMED AND WHY THE THIRD ONE IS NOT "a declaration" NEGATED.
	// A blank line and a comment line are both INDECISIVE about the code — a
	// blank line carries nothing and a comment carries prose about something —
	// so "not the declaration" is the whole story for them. A structural
	// punctuation line (`}`, `{`, `},`) is DECISIVE: it is the skeleton of a
	// declaration, so an anchor landing there is not a near miss on a comment;
	// it is the wrong KIND of line, and the reader must be told which, because
	// the two shapes are repaired differently (re-point at the declaration vs
	// re-point AND check the address never meant the block's closing brace).
	AnchorBlankLineVerdict     = "a blank line"
	AnchorCommentLineVerdict   = "a comment line"
	AnchorSeparatorOnlyVerdict = "only brackets or separators — structural punctuation, not a fact"
	// TestSuffix is the file suffix of a Go test file, and TestDataDir the
	// directory the Go tool ignores. An anchor owned by either is a test's
	// business, not a production source's.
	TestSuffix  = "_test.go"
	TestDataDir = "testdata/"
	// AbsentBaseNote is how an answer STATES that it could not look
	// under a base directory the tree simply does not have. An absent base is
	// neither an error nor a broken edge (see BaseDirs), but it is
	// also not nothing: whatever the scan failed to find there, it failed to look
	// for a stated reason (Rule 15 — no silent short answer).
	AbsentBaseNote = " (this tree has no base dir %s, so the scan could not look there)"
	// HiddenDirPrefix is the leading character of a directory the
	// walk does not descend into (a vendor/toolchain dot dir).
	HiddenDirPrefix = "."
	// GoSuffix is the file suffix the gate-package walk reads.
	GoSuffix = ".go"

	// MarkdownSuffix is the file suffix a document of record carries. The
	// candidate scope is spelled with it in ONE place (CandidateDirs) so the
	// glob and the single-path test cannot disagree about what a document is
	// (Rule 1b/14).
	MarkdownSuffix = ".md"
)

// BaseDirs are the two halves of the tree the gate-package leg
// walks. They are the module's own layout, named once here rather than spelled
// at the call site (Rule 1/13).
//
// A base this tree DOES NOT HAVE is SKIPPED, never walked: a fixture repository
// shaped like the real tree minus cmd/, and a consumer tree of a different shape
// altogether, are trees the scan must ANSWER about — the edge's contract is that
// an unusable edge is a FINDING, and a tree without a cmd/ half is not a broken
// edge but a different shape. Walking the absent directory made filepath.WalkDir
// report lstat ENOENT and turned that answer into a hard error, which is how a
// touch of a published anchor in such a tree came back as an ABSTENTION carrying
// raw lstat text.
//
// The distinction is the whole fix (Rule 15): ABSENT is a measured, stated skip
// (recorded on the discovered edge as SkippedBases and named in the finding that
// would otherwise read as "no package asserts on this document"); PRESENT BUT
// UNUSABLE — unreadable, not a directory — still fails loudly with its own error.
var BaseDirs = []string{"internal", "cmd"}

// RunPattern matches the regeneration instruction a document
// carries: `go run <path>.go`. The path is the generator program.
var RunPattern = regexp.MustCompile("go run ([A-Za-z0-9_./-]+/[A-Za-z0-9_-]+\\.go)")

// CorpusPresent reports whether the tree rooted at root carries a
// generated-document corpus AT ALL — a candidate document (see Candidates) that
// bears a generated region (Marker / VerbIndexMarker). It is what separates the
// two facts an empty discovery can mean, and the separation is the fix (measured
// 2026-10-12):
//
//   - TRUE, zero edges discovered — a corpus EXISTS and its edges were lost:
//     either a document lost its `go run` regeneration instruction, or the tree
//     where the corpus lives is not the tree the scan walked. That is the broken
//     edge the read floor reports, and it must keep refusing: whoever follows
//     that document's instruction runs nothing.
//   - FALSE, zero edges discovered — the tree has no generated region and never
//     had one (a fixture repository, a consumer tree of another shape). Nothing
//     was lost, so there is no broken edge to report and the answer is an
//     ordinary empty one.
//
// The predicate is deliberately the REGION and not the regeneration command: a
// document that carries the region but lost its `go run` line is precisely the
// lost-edge case above, so keying on the command would excuse the one shape the
// floor exists for.
//
// Deliberately a MEASUREMENT OF THE TREE and not a parameter: a caller that
// could pass "no corpus" would be a caller able to switch the guard off, and the
// whole point is that an unusable edge is found by the scan rather than declared
// by its consumer. Any error is returned, never folded into the boolean — a tree
// that could not be read is not a tree with no corpus (Rule 15).
func CorpusPresent(root string) (bool, error) {
	// A root that cannot be MEASURED is not a tree without a corpus. Without
	// this, a mistyped path and a corpus-less tree are the same answer: Glob on
	// an absent directory returns no matches and NO error, so the scan would
	// report "no corpus" about a tree it never opened (Rule 15).
	info, err := os.Stat(root)
	if err != nil {
		return false, fmt.Errorf("generated-doc corpus: root %s could not be measured: %w", root, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("generated-doc corpus: root %s is not a directory", root)
	}
	docs, err := Candidates(root)
	if err != nil {
		return false, err
	}
	for _, rel := range docs {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return false, fmt.Errorf("generated-doc corpus: read %s: %w", rel, err)
		}
		text := string(body)
		if strings.Contains(text, Marker) || strings.Contains(text, VerbIndexMarker) {
			return true, nil
		}
	}
	return false, nil
}

// Dep is one discovered edge. It is a fact about the tree, not a
// policy: Generator is the command the document names, AnchorSources are the
// files whose line numbers it publishes, GatePackage is the package that
// asserts on it.
type Dep struct {
	Doc           string   // "COMMANDS.md" (tree-relative slash path)
	Generator     string   // "internal/standards/testdata/commands-census.go"
	AnchorSources []string // source files the doc publishes `file.go:NNN` anchors into, sorted
	GatePackages  []string // Go package dirs whose tests name this document, sorted
	// RegionAnchors are the `file.go:NNN` spans the document publishes INSIDE
	// its generated regions — the anchors a reader of the block acts on,
	// separated from the prose anchors the document carries beside them. The
	// DECLARATION invariant (regionAnchorFindings) is raised over these and
	// only these: a document's prose may cite whatever it is talking about,
	// while the region is rendered by a generator and must point where the
	// generator's source of truth is.
	RegionAnchors []string
	// ProseAnchors are the spans the SAME document publishes OUTSIDE its
	// generated regions — the hand-written sentences, tables and headings a
	// reader acts on directly. They are a separate field from RegionAnchors
	// because the two populations have different WRITERS (a generator vs a
	// human), and the value of measuring the second one is that it is the half
	// no generator re-derives.
	//
	// The DECLARATION invariant is raised over these too, for the documents
	// JudgesProseAnchors names — measured 2026-11-02: COMMANDS.md's prose
	// carried 107 such spans and the whole arm was blind to them, so 20 of them
	// pointed at comment or blank lines with every gate green.
	ProseAnchors []string
	// SkippedBases are the base directories the gate-package leg could not look
	// under because this tree does not have them. Empty on the real tree; on a
	// tree of a different shape it is how the answer stays VISIBLE rather than
	// reading as silence (and as a finding below).
	SkippedBases []string
	// GateUnderivable is the MEASURED fact that a gate package cannot be derived
	// for this document even in principle: the walk read a PRODUCTION source
	// carrying this document's name in a test file, and no such source is a test
	// package — because the file it reads the document in IS the generator
	// program this document names, and a generator lives under the directory the
	// Go tool does not test.
	//
	// It is what separates the two facts an empty GatePackages can mean, and the
	// separation is the whole point of the field (Rule 15 — the two have opposite
	// remedies):
	//
	//	TRUE  — nobody CAN own this document: whoever moved a line under one of
	//	        its anchors would reach the integration branch uncaught, and no
	//	        package the edge could name would catch it. A defect in the tree,
	//	        stated at ERROR level (EmptyGateDerivedFormat).
	//	FALSE — a package that SHOULD read this document does not yet (a real
	//	        candidate nobody has written the test for), which is a note about
	//	        the corpus rather than a hole in the guard.
	//
	// It is a different PREDICATE from having zero gate packages, never a
	// restatement of it: on this repo COMMANDS.md HAS gate packages
	// (internal/gendoc, internal/standards, internal/tools) and is not a
	// candidate, while a second command would leave it gate-package-less AND
	// its generator-derived candidate underivable — both measured on the real
	// tree, 2026-10-14 (devdoc/gendoc-empty-gate-package-error-class.md).
	GateUnderivable bool
}

// Deps discovers every dependency edge in the tree rooted at root.
// The returned slice is sorted by Doc so a report is stable. An empty result is
// NOT "nothing to check" and it is not a defect either: the caller asks
// GeneratedDocAnswer (ClassifyParticipation), which distinguishes a tree that
// publishes no generated document at all — an ABSTENTION, whatever its shape —
// from a tree that STATES one and yields no followable edge, which Impact
// reports as a finding.
func Deps(root string) ([]Dep, error) {
	deps, _, _, err := DepsSurvey(root)
	return deps, err
}

// DepsSurvey is the SAME walk as Deps, plus the two counts the participation
// classification is measured from — every document the walk read, and how many
// of them STATE a generated region — so "is this tree part of the edge at all?"
// is answered by ONE walk rather than by a second scan that could disagree with
// it (Rule 14: one definition, and one read of the tree).
//
//	marked — candidate documents that carry a generated-region marker. A tree
//	         with one of these PARTICIPATES whether or not an edge could be
//	         derived from it, so a document that lost its `go run` instruction
//	         stays a finding instead of becoming an abstention.
//	candidates — every document the walk read at all. Zero means the tree has
//	         no place a document of record could live.
func DepsSurvey(root string) (deps []Dep, marked, candidates int, err error) {
	docs, err := Candidates(root)
	if err != nil {
		return nil, 0, 0, err
	}
	candidates = len(docs)
	depByDoc := map[string]*Dep{}
	for _, rel := range docs {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, 0, 0, fmt.Errorf("generated-doc deps: read %s: %w", rel, err)
		}
		text := string(body)
		if !strings.Contains(text, Marker) && !strings.Contains(text, VerbIndexMarker) {
			continue
		}
		marked++
		m := RunPattern.FindStringSubmatch(text)
		if m == nil {
			continue // a generated document that names no generator is a finding below
		}
		dep := &Dep{Doc: rel, Generator: m[1]}
		// The document's anchors, read ONCE. RegionAnchors is the SUBSET a
		// generator renders (the declaration invariant's scope); AnchorSources
		// stays the document's anchors as a whole, so a consumer that asks "did
		// this change touch something this document publishes?" keeps the answer
		// it had — this change ADDS a finding, it does not narrow an existing
		// predicate.
		dep.RegionAnchors = RegionAnchors(text, Marker, VerbIndexMarker)
		for _, ref := range AnchorPattern.FindAllStringSubmatch(text, -1) {
			dep.AnchorSources = append(dep.AnchorSources, ref[1])
		}
		dep.AnchorSources = dedupSorted(dep.AnchorSources)
		depByDoc[rel] = dep
	}
	// The gate half: which package's tests name the document — and, when that
	// answer is EMPTY, whether it is empty because nobody CAN own the document
	// (Dep.GateUnderivable). Both are measured from this one walk, so the two
	// readers of the fact cannot disagree.
	for doc, dep := range depByDoc {
		pkgs, skipped, err := GatePackages(root, doc)
		if err != nil {
			return nil, 0, 0, err
		}
		dep.GatePackages = pkgs
		dep.SkippedBases = skipped
		// The class is claimed only over a COMPLETE walk: an empty set measured
		// with a base the tree does not have is "I could not look", never "none
		// can exist" (Rule 15 — the two have opposite remedies, and the second is
		// a refusal).
		dep.GateUnderivable = len(pkgs) == 0 && len(skipped) == 0 && gatePackageUnderivable(dep.Generator)
	}
	out := make([]Dep, 0, len(depByDoc))
	for _, dep := range depByDoc {
		out = append(out, *dep)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Doc < out[j].Doc })
	return out, marked, candidates, nil
}

// AnchorPattern matches the anchor shapes a document publishes — the same
// `path/file.go:…` spans the block's gate resolves.
//
// THE ADDRESS HAS TWO SHAPES, and the reader must accept BOTH or it silently
// reads nothing. A block written before f571564a0 addressed a declaration by its
// LINE NUMBER (`cli_log.go:611`); that commit re-keyed the generated block on
// SYMBOL IDENTITY (`cli_notify.go:isNotifySubcommand`), because a line number
// stales on any insertion above the function while a symbol name does not. The
// second shape is therefore now the one the real block emits — 86 spans of it in
// COMMANDS.md — and a `(\d+)`-only pattern reads every one of them as no anchor
// at all (measured on master 02e138f: RegionAnchors returned 0 over a block that
// publishes 86). An extraction that reads nothing makes the declaration
// invariant below vacuously silent, which is the failure mode this package
// exists to prevent, so the pattern is widened rather than the invariant relaxed.
var AnchorPattern = regexp.MustCompile("`([A-Za-z0-9_./-]+\\.go):([A-Za-z0-9_]+)`")

// anchorSymbolPattern reports whether an anchor's address segment is a
// DECLARATION NAME rather than a line number — the discrimination the two
// address shapes need, in one place so the reader and the invariant cannot
// disagree about which shape they are looking at (Rule 1b/14).
var anchorSymbolPattern = regexp.MustCompile("^[A-Za-z_][A-Za-z0-9_]*$")

// CandidateDirs are the directories a document of record can live in: the
// module root, plus the man sources. Named here because TWO readers need the
// same scope — the discovery walk (Candidates) and IsCandidateDocument, which a
// consumer uses to recognise the document inside a changed-path set — and a
// second spelling of the scope is the drift this package exists to prevent
// (Rule 1b/14).
var CandidateDirs = []string{"", "man"}

// IsCandidateDocument reports whether a tree-relative path is a document this
// edge can carry — the same scope Candidates globs, asked of one path.
//
// It exists so a consumer can decide "did this change touch the document of
// record?" from a git diff's path list WITHOUT materialising the tree (the
// union half of a landing has to ask that question about paths only, and a
// consumer that re-spelled the scope would answer it differently the day the
// scope moves). It is deliberately a scope test, not a content test: whether a
// candidate CARRIES a generated region is a fact about the file's bytes, which
// only a read of the tree can give (DepsSurvey does exactly that).
func IsCandidateDocument(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || !strings.HasSuffix(rel, MarkdownSuffix) {
		return false
	}
	for _, dir := range CandidateDirs {
		if dir == "" {
			if !strings.Contains(rel, "/") {
				return true
			}
			continue
		}
		if strings.HasPrefix(rel, dir+"/") && !strings.Contains(strings.TrimPrefix(rel, dir+"/"), "/") {
			return true
		}
	}
	return false
}

// Candidates lists the markdown files that can carry a generated
// region: the module root plus the man sources. The scope is deliberately
// narrow — a devdoc is prose about the corpus, not a document of record — and
// the list is DISCOVERED (globbed), never typed.
func Candidates(root string) ([]string, error) {
	var out []string
	for _, dir := range CandidateDirs {
		matches, err := filepath.Glob(filepath.Join(root, dir, "*.md"))
		if err != nil {
			return nil, fmt.Errorf("generated-doc deps: glob %s: %w", dir, err)
		}
		for _, m := range matches {
			rel, err := filepath.Rel(root, m)
			if err != nil {
				return nil, fmt.Errorf("generated-doc deps: %s is not under %s: %w", m, root, err)
			}
			out = append(out, filepath.ToSlash(rel))
		}
	}
	sort.Strings(out)
	return out, nil
}

// GatePackages returns the Go package directories whose files name
// doc (a test that reads the document is the package that must run for the
// drift to be caught), plus the base directories THIS TREE DOES NOT HAVE — the
// skips the answer must state rather than swallow. Both halves are derived by
// reading the tree, never declared.
func GatePackages(root, doc string) (pkgs []string, skippedBases []string, err error) {
	var out []string
	for _, base := range BaseDirs {
		dir := filepath.Join(root, base)
		present, err := BaseAvailable(dir, base)
		if err != nil {
			return nil, nil, err
		}
		if !present {
			// A tree of a different shape: SKIP it and say so. This is the fix —
			// the walk below used to run unconditionally and filepath.WalkDir
			// answered lstat ENOENT, which the caller turned into a hard error:
			// a fixture repository shaped like the real tree minus cmd/ could not
			// be ANSWERED about at all, and a touch of a published anchor in it
			// came back as an abstention carrying raw lstat text.
			skippedBases = append(skippedBases, base)
			continue
		}
		err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), HiddenDirPrefix) && path != dir {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), GoSuffix) {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !strings.Contains(string(body), `"`+doc+`"`) {
				return nil
			}
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, nil, fmt.Errorf("generated-doc deps: scan %s: %w", base, err)
		}
	}
	return dedupSorted(out), skippedBases, nil
}

// gatePackageUnderivable reports whether this document's gate-package set CANNOT
// be derived — the measured predicate behind Dep.GateUnderivable, and the fact
// the ERROR class of empty-gate-package finding states. It is asked only when the
// set came back EMPTY (a non-empty set is its own answer), so it never has to
// re-walk the tree the sibling leg already walked.
//
// THE MEASUREMENT, positively: the document's own regeneration instruction names
// a generator UNDER TestDataDir, and the Go tool does not test what lives there —
// a file there belongs to no package the toolchain builds a test binary for. So
// the document's own program is the only thing in the module that reads it (a
// regenerator takes the document as an ARGUMENT, which is why the program does
// not carry the document's name either), and no package anywhere names it: the
// gate set is empty by CONSTRUCTION, not because a gate was forgotten. Whoever
// moved a line under one of the document's anchors would reach the integration
// branch uncaught, and no package the edge could run would catch it.
//
// A BASE THE TREE DID NOT HAVE VETOES THE CLAIM, and it is the caller's job to
// pass that in (see Dep.SkippedBases): "no package names it" measured over a walk
// that could not enter a base is not the same fact as "no package CAN name it",
// and this predicate must not turn the first into the second. The caller
// therefore asks it only when the walk was complete (DepsSurvey).
//
// WHAT IT DELIBERATELY DOES NOT REPORT: a generator at the module ROOT (a program
// can live outside BaseDirs entirely; a walk that did not look there has measured
// nothing), a generator under a base directory that is NOT testdata (its
// directory is a package the toolchain builds, so the empty set means no test has
// been written — the corpus note it always was), and any document that HAS a gate
// package (this predicate is never asked about one).
func gatePackageUnderivable(generator string) bool {
	if generator == "" {
		return false
	}
	return strings.Contains(filepath.ToSlash(filepath.Dir(filepath.FromSlash(generator)))+"/", TestDataDir)
}

// BaseAvailable reports whether a base directory is THERE TO BE
// WALKED, and separates the two conditions the fix turns on (Rule 15 — no
// silent failure, and no invented failure either):
//
//   - absent (fs.ErrNotExist) — the tree simply has no such half. NOT an error:
//     the caller skips it and RECORDS the skip, so the answer states what it
//     could not look at instead of failing about a directory the tree was never
//     shaped to have.
//   - exists but is not a directory — a hard error. A file standing where the
//     base belongs is not a differently-shaped tree; it is a tree this scan
//     cannot read, and a scan that silently returned no packages for it would be
//     the silent-empty answer this file exists to refuse.
//   - any other stat error (a permission failure, an I/O error) — a hard error
//     carrying the underlying cause. "I could not look" and "there is nothing
//     there" are different facts, so only the second one is allowed to pass.
func BaseAvailable(dir, base string) (bool, error) {
	info, err := os.Stat(dir)
	switch {
	case err == nil && info.IsDir():
		return true, nil
	case err == nil:
		return false, fmt.Errorf("generated-doc deps: base %s is not a directory (%s)", base, dir)
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("generated-doc deps: base %s is present but could not be measured (%s): %w", base, dir, err)
	}
}

// GeneratedDocGates answers THE question a committing path has to ask: given the
// tree-relative paths a change touched, which test packages MUST run before that
// change may be delivered? The answer is derived from the discovered edges (see
// Deps), so a consumer never holds a list of its own — and the
// findings half is returned alongside, because a broken edge must be reported,
// never read as "nothing to run" (Rule 15).
//
// A base directory this tree simply does NOT HAVE (a fixture shaped like the real
// tree minus cmd/, a consumer tree of another shape) is SKIPPED, and the skip is
// stated in the findings that depend on it — that is an answer about a
// differently-shaped tree, not an error. A base that is THERE but unreadable
// (not a directory, a permission failure) is still a hard error: "I could not
// look" and "there is nothing there" are different facts.
//
// It is EXPORTED on purpose: this package is the one place the edge can live
// (Rule 14 — one definition, importable), and the consumer that needs it is the
// delivery/verify path, which is a different package. A consumer that adds a
// gate package of its own would be a second opinion about a fact the tree
// already states.
func GeneratedDocGates(root string, changed []string) (required []string, findings []string, err error) {
	ans, err := GeneratedDocAnswer(root, changed)
	if err != nil {
		return nil, nil, err
	}
	return ans.Required, ans.Findings, nil
}

// --- participation: "this tree has no such edge" vs "this edge is broken" ----

// The three classifications of a tree's participation in the published-anchor
// edge. They are the ONE vocabulary for the question "does this tree take part
// at all?", so a consumer never re-derives it from a count (Rule 14).
const (
	// ParticipationAbsent — the tree publishes no generated document, so it has
	// no published anchor for a change to move and no edge for a reader to
	// follow. NO finding can be raised against it: an absent edge is a fact
	// about the tree's shape, not a defect in it.
	ParticipationAbsent = "absent"
	// ParticipationPresent — the tree publishes at least one generated
	// document, so its edges are MEASURED and any unusable one is a finding.
	ParticipationPresent = "present"
	// ParticipationUnknown — the question was not answered (a caller holding a
	// zero Answer). It renders as its own word rather than as an empty field, so
	// an unfilled answer cannot read as a pass (Rule 15a).
	ParticipationUnknown = "unknown"
)

// The machine-readable readings behind ParticipationAbsent. Each names the
// FLOOR that came back empty, so an abstention states which measurement produced
// it instead of a sentence a reader has to trust.
const (
	// NoCandidateCode — no candidate document at all (the tree holds no
	// markdown file where a document of record could live).
	NoCandidateCode = "no-candidate-document"
	// NoRegionCode — candidate documents exist, none carries a generated
	// region, and none states a regeneration command.
	NoRegionCode = "no-generated-region"
)

// The reason sentences an abstention carries. Built here, not at the consumer,
// because they state a fact about THIS read (the counts, the floors) and a
// second copy in a consumer is the drift this package exists to prevent.
const (
	// AbsentNoCandidateReason names the empty candidate walk.
	AbsentNoCandidateReason = "the walk found no candidate document in this tree: it publishes no " +
		"generated document, so it takes part in no published-anchor edge"
	// AbsentNoRegionReasonFormat names the empty discovery together with the
	// walk's own count, so the reader sees the scan DID read the tree.
	AbsentNoRegionReasonFormat = "this tree's %d candidate document(s) carry no generated region, and none " +
		"states a regeneration command — it publishes no generated document, so it takes part in no " +
		"published-anchor edge"
)

// Answer is the edge's WHOLE reply about one tree and one change, from one read
// of that tree: what must run, what is broken, and whether this tree takes part
// in the edge at all. A consumer reads the fields; it never re-derives any of
// them (Rule 14 — one definition, in a leaf package both consumers can import).
//
// THE DISTINCTION IT EXISTS FOR (Rule 15: the two failures have opposite
// remedies). ABSENT — no generated document in this tree — is an abstention:
// there is nothing to measure, and a consumer on an arbitrary deliverable.repo
// must not refuse a delivery over a document the tree never had. BROKEN — a
// document that STATES a generated region or a regeneration command whose edge
// cannot be followed — is a refusal, and it is exactly the defect this package
// exists to catch. Findings only ever describe the second kind: Impact validates
// the edges the discovery RETURNED, and an empty discovery returns none.
//
// The ZERO VALUE takes part in nothing (TakesPart is false) and carries no
// reason: an Answer nobody filled abstains rather than passes, so a caller that
// forgot to fill it cannot hand out a green light (Rule 15a).
type Answer struct {
	// Required are the gate packages the change obliges (empty when nothing
	// published was touched).
	Required []string
	// Findings are the UNUSABLE edges: a document whose generator is not in the
	// tree, an edge with no gate package, a generated region with no anchor. On
	// an absent tree this is empty BY CONSTRUCTION — see the type's doc.
	//
	// It carries the NON-ERROR class only, because it is the field every
	// existing refusal arm already reads: a change that only ADDS a severity
	// must not re-decide what those arms do. The ERROR class is beside it, in
	// Errors, and every arm that refuses on a broken EDGE reads both (Rule 15 —
	// a class nobody acts on is not a refusal).
	Findings []string
	// Errors is the ERROR-level class of finding: what this walk measured that
	// a committing path must not read as "a note". It is a SEPARATE channel from
	// Findings, not a copy of part of it — Findings keeps exactly what it carried
	// before this class existed, so an existing refusal arm's behaviour cannot
	// change as a side effect of the class being added (see SeparateErrors for
	// the measured reason).
	//
	// It is filled UNCONDITIONALLY by GeneratedDocAnswer (then a participating
	// tree's Skip empties it): an ERROR class that only exists when a consumer
	// remembered to ask for it is a class that a future consumer silently drops,
	// which makes "nobody read the doc" unanswerable (Rule 15).
	Errors []string
	// Skip is an OPERATOR's documented escape for THE ERROR CLASS ALONE: when it
	// is true and this tree participates, Errors is emptied and the refusal that
	// would have quoted it does not happen. Findings is untouched by it, so the
	// non-ERROR half of the answer — the broken edges that were already refusals
	// before this class existed — cannot be switched off here.
	//
	// It exists for the one case the ERROR cannot tell apart from a defect: the
	// document's OWN generator is the thing reading it (the walk cannot name it
	// as its own gate package without also naming the program that renders the
	// document as a gate ON the document). It is a caller's explicit decision,
	// never a default and never an inference, and a tree that is left skipped
	// says so in its own vocabulary (the caller's sentence) rather than passing
	// silently.
	Skip bool
	// Participation is ParticipationAbsent / ParticipationPresent /
	// ParticipationUnknown.
	Participation string
	// Reason is the prose behind an abstention (empty when the tree takes
	// part): the reading, stated for an operator.
	Reason string
	// Reading is the machine code behind an abstention (NoCandidateCode /
	// NoRegionCode; empty when the tree takes part).
	Reading string
	// Docs is the number of discovered edges the classification was measured
	// from, and Candidates the number of documents the walk read. Both are
	// reported so "the scan looked and found nothing" is never something a
	// reader has to infer from silence.
	Docs       int
	Candidates int
	// Touched reports whether the change MOVED a published anchor source of any
	// edge — measured by the same walk, not re-derived by the caller. It is
	// separate from Required because the two facts part company exactly where
	// this package's ERROR class lives: a change can move a document's anchors
	// and the edge still name NOTHING to run, because that document has no gate
	// package. A caller reading Required alone would then say "this change
	// touches no published anchor source", which is false — and false in the
	// direction that reads as a pass (measured on the empty-gate fixture,
	// 2026-10-14).
	Touched bool
}

// TakesPart reports whether this tree publishes at least one generated document
// — the only answer on which a change's requirements and findings mean
// anything. Every other value (including the zero value) is NOT a pass: it is
// an abstention the caller must record.
func (a Answer) TakesPart() bool { return a.Participation == ParticipationPresent }

// GeneratedDocAnswer answers the whole question a consumer of the edge has
// about this tree and this change: GeneratedDocGates' two halves PLUS the
// participation classification that tells a consumer whether the two halves are
// a statement about this tree or a statement about an edge the tree does not
// have. It is the surface a delivery path reads — one read of the tree, one
// vocabulary, no second opinion in the caller (Rule 14).
func GeneratedDocAnswer(root string, changed []string) (Answer, error) {
	deps, marked, candidates, err := DepsSurvey(root)
	if err != nil {
		return Answer{}, err
	}
	required, findings := Impact(root, deps, changed)
	edgeFindings, edgeErrors := SeparateErrors(findings)
	ans := Answer{
		Required:   required,
		Findings:   edgeFindings,
		Errors:     edgeErrors,
		Docs:       len(deps),
		Candidates: candidates,
		Touched:    anyTouches(deps, changed),
	}
	ans.Participation, ans.Reading, ans.Reason = ClassifyParticipation(candidates, marked, len(deps))
	if !ans.TakesPart() {
		// A tree that publishes no generated region has no edge to be broken:
		// what remains of Impact's answer is its read floor, which is a statement
		// about a PARTICIPATING tree that lost its edges. The abstention says
		// that instead, with the counts behind it (Rule 15) — never a finding
		// that a delivery path would read as a broken edge and refuse on.
		//
		// The ABSENT arm empties BOTH channels: Skip is about a class this tree
		// cannot have, and an abstention that left one channel unemptied would
		// read as a defect on a tree of a different shape (the measured
		// 2026-10-12 defect this arm exists for).
		ans.Findings = nil
		ans.Errors = nil
	}
	return ans, nil
}

// GeneratedDocAnswerOpts is GeneratedDocAnswer for a caller that carries a
// DOCUMENTED DECISION about the ERROR class — the operator's Skip, and nothing
// else. It exists as its own entry point rather than as a fourth parameter on
// GeneratedDocAnswer so that every existing caller keeps reading the edge the
// same way (Rule 4: an added knob is not a re-signing of the call sites), and so
// that the decision is spelled at the one place that can articulate it.
//
// Skip empties Answer.Errors ON A PARTICIPATING TREE ONLY, and it never touches
// Answer.Findings: the non-ERROR half of a broken edge was a refusal before this
// class existed and is not switchable here. A caller that skips is expected to
// say so in its own vocabulary — an emptied channel is a decision that has to be
// readable, never a silence indistinguishable from a clean edge (Rule 15).
func GeneratedDocAnswerOpts(root string, changed []string, skipErrorClass bool) (Answer, error) {
	ans, err := GeneratedDocAnswer(root, changed)
	if err != nil {
		return ans, err
	}
	if skipErrorClass && ans.TakesPart() {
		ans.Errors = nil
		ans.Skip = true
	}
	return ans, nil
}

// ClassifyParticipation is THE predicate for "does this tree take part in the
// published-anchor edge?", stated once for every consumer. It is a function of
// the three counts one walk produced — never of the change, and never of a
// document's name — so a differently-shaped tree is answered rather than
// guessed at:
//
//   - no candidate document at all          → ABSENT (NoCandidateCode)
//   - candidates, none carrying a generated
//     region, and no edge discovered        → ABSENT (NoRegionCode)
//   - at least one generated document, OR a
//     candidate that CARRIES a generated
//     region the discovery could not turn
//     into an edge                          → PRESENT
//
// The second PRESENT arm is the tooth: a document that STATES a generated region
// and yields no followable edge is a defect in the tree — a finding — not a
// property of its shape, so the tree still participates and Impact's read-floor
// finding still speaks. Feeding the counts is how a caller cannot accidentally
// buy the abstention by losing a document.
func ClassifyParticipation(candidates, marked, docs int) (participation, reading, reason string) {
	if docs > 0 || marked > 0 {
		return ParticipationPresent, "", ""
	}
	if candidates == 0 {
		return ParticipationAbsent, NoCandidateCode, AbsentNoCandidateReason
	}
	return ParticipationAbsent, NoRegionCode, fmt.Sprintf(AbsentNoRegionReasonFormat, candidates)
}

// GeneratedDocMandate renders the sentence a briefing carries when a change
// touches a published anchor source: WHAT the document is, WHICH generator
// rewrites it, and WHICH package blocks on it. It is BUILT from the discovered
// edge, never typed by a briefing author — a hand-written reminder is the drift
// it is meant to catch, and it rots the moment the generator moves.
//
// Empty return means the change obliges nothing (no published anchor source was
// touched): a caller then says nothing rather than inventing a caution.
func GeneratedDocMandate(root string, changed []string) (string, error) {
	deps, err := Deps(root)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, dep := range deps {
		if !Touches(dep, changed) {
			continue
		}
		pkgs := make([]string, 0, len(dep.GatePackages))
		for _, p := range dep.GatePackages {
			pkgs = append(pkgs, "./"+p+"/")
		}
		fmt.Fprintf(&b, "\n%s publishes `file.go:NNN` anchors for this change: regenerate it with "+
			"`go run %s . %s`, and run the package that blocks on it — `go test %s`.\n",
			dep.Doc, dep.Generator, dep.Doc, strings.Join(pkgs, " "))
	}
	return b.String(), nil
}

// anyTouches reports whether the change moved a published anchor source of ANY
// edge — the ONE predicate behind Answer.Touched, so no consumer re-derives it.
func anyTouches(deps []Dep, changed []string) bool {
	for _, dep := range deps {
		if Touches(dep, changed) {
			return true
		}
	}
	return false
}

// Touches reports whether changed names one of the dep's published
// anchor sources.
func Touches(dep Dep, changed []string) bool {
	for _, src := range dep.AnchorSources {
		for _, f := range changed {
			if src == f {
				return true
			}
		}
	}
	return false
}

// --- the predicate both consumers read ---------------------------------------

// Impact maps a change (the tree-relative paths it touched) onto the
// packages that must run, and reports every unusable edge. Both halves are
// returned: a caller that only wants the requirement still gets the findings, so
// a broken edge can never be read as "nothing to run" (Rule 15).
//
// changedFiles may be nil — the edges are still validated, which is the shape
// the real-tree assertion uses for its precondition.
func Impact(root string, deps []Dep, changedFiles []string) ([]string, []string) {
	var findings []string
	if len(deps) == 0 {
		// AN EMPTY DISCOVERY IS READ AGAINST THE TREE, NOT ASSUMED (measured
		// 2026-10-12). The floor below is a finding about a tree that HAS a
		// corpus: a document of record whose generator instruction is gone is an
		// edge a reader would follow into a command that runs nothing.
		//
		// A tree with NO generated-document corpus at all is a different shape —
		// it is every fixture repository and every consumer tree — and there the
		// same sentence is FALSE: nothing was lost, the corpus was never there.
		// Read as a broken edge it refused the whole close-out of a branch whose
		// change had nothing to do with documents: 24 tests in internal/tools
		// failed with "the published-anchor edge is BROKEN — …", each one a
		// worktree that the delivery path then refused to release.
		//
		// The distinction is derived from the tree (CorpusPresent), never from a
		// caller's flag: a consumer cannot turn the guard off, and the fact is
		// stated by the scan that measured it.
		present, err := CorpusPresent(root)
		if err != nil || present {
			// An UNMEASURABLE corpus keeps the finding: "I could not look" must
			// never read as "there is nothing to be broken" (Rule 15).
			return nil, []string{"no generated-document edge was discovered at all — the scan " +
				"either did not read the tree or every document lost its generator instruction"}
		}
		return nil, nil
	}
	touched := map[string]bool{}
	for _, f := range changedFiles {
		touched[f] = true
	}
	var required []string
	for _, dep := range deps {
		// The DECLARATION invariant: what a generated region publishes must
		// resolve to a declaration in a production source, not to the prose
		// above one. Measured stale cells that RESOLVED and were therefore
		// carried by a resolution-only check: `cmd/agent/cli_ui.go:125` and
		// `cmd/agent/cli_doccheck.go:234`, each the comment line above its
		// predicate (2026-10-13). This is additive: the findings that were here
		// before are unchanged.
		findings = append(findings, regionAnchorFindings(root, dep)...)
		if len(dep.AnchorSources) == 0 {
			findings = append(findings, dep.Doc+": carries a generated region but publishes no anchor — "+
				"no change can be mapped onto it")
		}
		if dep.Generator == "" || !PathExists(root, dep.Generator) {
			findings = append(findings, dep.Doc+": names generator "+Quoted(dep.Generator)+
				", which is not in the tree — the instruction a reader would follow runs nothing")
		}
		if len(dep.GatePackages) == 0 {
			// TWO FACTS, TWO CLASSES, and the separation is the whole point
			// (Rule 15 — opposite remedies). An empty gate-package set whose
			// DERIVATION is impossible is a hole in the guard: no package could
			// ever catch a line moved under this document's anchors, so the
			// drift reaches the integration branch with nothing standing in its
			// way — an edge defect, and the ERROR class a committing path
			// refuses on. An empty set that COULD still hold a candidate is a
			// note about this corpus (nobody has written the gate yet) and stays
			// the non-ERROR finding it always was, in the same channel, so a
			// consumer that reads Findings alone sees what it saw before.
			if dep.GateUnderivable {
				findings = append(findings, fmt.Sprintf(EmptyGateDerivedFormat, dep.Doc, dep.Generator))
			} else {
				findings = append(findings, fmt.Sprintf(EmptyGateFindingFormat, dep.Doc, SkippedBaseNote(dep)))
			}
		}
		if Touches(dep, changedFiles) {
			required = append(required, dep.GatePackages...)
		}
	}
	return dedupSorted(required), findings
}

// --- the DECLARATION invariant on what a generated region publishes ----------

// ErrorFindingPrefix marks a finding of the ERROR-level class. The severity
// lives in ONE place (here) and is rendered into the finding text, so a
// consumer holding only the strings and one holding the typed Answer.Errors
// field classify the same way (Rule 14).
const ErrorFindingPrefix = "ERROR: "

// SeparateErrors splits the findings a walk produced into the two channels a
// committing path reads: Findings carries what was there before this change
// (unchanged, so every existing `len(ans.Findings) > 0` refusal behaves exactly
// as it did), and Errors carries the ERROR-level class this change ADDS.
//
// The ERROR is deliberately NOT duplicated into Findings. Measured 2026-10-13:
// duplicating it changed the refusal an operator reads on the anchor lane
// (`TestBranchCheckAnchorArmRefusesADroppedRegeneration` — the branch that drops
// a regeneration used to reach the document's GATE test and quote it; with the
// ERROR in Findings the lane refused earlier, on the invariant, and the gate the
// document obliged never ran). An ERROR class becomes a refusal by wiring it at
// the arm that should refuse — one line in internal/tools, a deliberate follow-up
// — never as a side effect of adding the class here.
//
// THE SAME MEASUREMENT CARRIES ONE LEVEL FURTHER, and it is why a consumer
// selects a SUB-CLASS rather than reading this channel whole
// (EmptyGateDerivedErrors): Errors is every ERROR-level finding, the classes have
// different remedies, and an arm that refused on the channel itself reproduced
// the regression above the moment a second class existed — measured again on
// 2026-10-14, when wiring the empty-gate-package class turned the anchor lane's
// dropped-regeneration arm into a refusal of the DECLARATION invariant, again
// before the gate ran.
func SeparateErrors(findings []string) (all []string, errors []string) {
	for _, f := range findings {
		if strings.HasPrefix(f, ErrorFindingPrefix) {
			errors = append(errors, f)
			continue
		}
		all = append(all, f)
	}
	return all, errors
}

// EmptyGateDerivedErrors selects the empty-gate-package ERROR class out of the
// findings an edge raised — the sub-class whose remedy is "write the gate, or
// retire the class in writing", as opposed to any other ERROR the walk may have
// measured (the DECLARATION invariant, whose remedy is "regenerate the
// document" and whose own arms own it).
//
// It exists so a committing path refuses on the class it means to refuse on.
// Read the measured reason on EmptyGateDerivedMark: an arm that took the whole
// Errors channel regressed the anchor lane, because the reference check ran
// instead of the test the document obliged — the two classes share a severity
// and nothing else.
//
// The selection is by the ONE marker constant, never by a re-typed sentence
// (Rule 1b/14): a second spelling is the drift this package exists to prevent.
func EmptyGateDerivedErrors(findings []string) []string {
	var out []string
	for _, f := range findings {
		if strings.Contains(f, EmptyGateDerivedMark) {
			out = append(out, f)
		}
	}
	return out
}

// RegionEnd returns the marker that closes a region opened by begin — the same
// words with BEGIN → END. It is DERIVED, never listed, so the two markers of a
// region cannot drift apart (Rule 1b/14).
func RegionEnd(begin string) string {
	return strings.Replace(begin, BeginWord, EndWord, 1)
}

// RegionAnchors returns the `file.go:NNN` anchors published INSIDE the
// generated regions of text — the spans the block's reader acts on, as opposed
// to the prose anchors a document carries beside them.
//
// A document may carry the boundaries in more than one region, and a region
// whose end marker is missing is read to the end of the document: an
// unterminated region is reported by its own gate, and silently publishing NO
// anchor for it would turn "the marker is gone" into "nothing to check".
func RegionAnchors(text string, begins ...string) []string {
	var out []string
	lines := strings.Split(text, "\n")
	for _, begin := range begins {
		end := RegionEnd(begin)
		inside := false
		for _, line := range lines {
			switch {
			case strings.Contains(line, begin):
				inside = true
				continue
			case inside && strings.Contains(line, end):
				inside = false
				continue
			}
			if !inside {
				continue
			}
			for _, m := range AnchorPattern.FindAllStringSubmatch(line, -1) {
				out = append(out, m[1]+":"+m[2])
			}
		}
	}
	return dedupSorted(out)
}

// ProseAnchors returns the `file.go:NNN` anchors a document publishes OUTSIDE
// its generated regions — the hand-written half: the sentences, tables and
// headings a reader of the page acts on directly, which no generator re-derives.
//
// It is the exact COMPLEMENT of RegionAnchors over the same text, and the
// complement is why it exists: the declaration invariant was raised over the
// region and nothing else, so an anchor typed into the prose was never
// content-checked (measured 2026-11-02: COMMANDS.md carried 107 of them, 20
// pointing at comment or blank lines, every gate green). The two functions walk
// the SAME markers (both begin spellings, with the end marker DERIVED by
// RegionEnd), so a region cannot be inside one reader's scope and outside the
// other's — a second spelling of the boundary is the drift this package exists
// to prevent (Rule 1b/14).
//
// An unterminated region is read to the end of the document, exactly as
// RegionAnchors reads it: the "marker is gone" defect belongs to the marker's
// own gate, and moving those anchors into the prose population would report a
// different invariant's name against it.
func ProseAnchors(text string, begins ...string) []string {
	occurrences := ProseAnchorOccurrences(text, begins...)
	out := make([]string, 0, len(occurrences))
	for _, occurrence := range occurrences {
		out = append(out, occurrence.File+":"+occurrence.Num)
	}
	return dedupSorted(out)
}

// AnchorOccurrence is ONE `file.go:NNN` span a document publishes, together with
// the document LINE it was read from.
//
// WHY THE LINE IS CARRIED, and why this is not a second reader (measured
// 2026-11-02, task 01a195d7). ProseAnchors answers "which spans does this page
// publish", which is all a population-level check needs. The CONTENT check
// cannot stop there: the claim an anchor makes is the sentence it sits in (a
// table row says `GOPI_X` in one cell and cites `file.go:NNN` in another; a
// parenthetical says “(`thing`, `file.go:NNN`)“), so judging whether a target
// line mentions what its anchor CLAIMS is only answerable with the citing line in
// hand. Adding that field here — rather than re-walking the markers in a second
// package — keeps the boundary in ONE definition (Rules 13/14): ProseAnchors now
// derives from this function, so the two cannot disagree about which spans are
// prose, and a document cannot carry an anchor that is prose to one reader and
// invisible to the other.
type AnchorOccurrence struct {
	Line int    // 1-based line number within the document
	Text string // that document line, verbatim
	Ref  string // the span as published, backticks included
	File string // the cited file, tree-relative
	Num  string // the clause after the colon: decimal digits, or a symbol name
}

// ProseAnchorOccurrences returns the prose anchors of text in document order,
// duplicates kept: each occurrence is judged on its own citing line, so two rows
// citing the same span are two claims and folding them would judge one of them.
//
// The region walk is ProseAnchors's, moved here so both readers share it.
func ProseAnchorOccurrences(text string, begins ...string) []AnchorOccurrence {
	var out []AnchorOccurrence
	// RegionEnd is DERIVED per begin marker, so the ends are computed once here
	// rather than typed as a second list.
	ends := make([]string, 0, len(begins))
	for _, begin := range begins {
		ends = append(ends, RegionEnd(begin))
	}
	inside := false
	for index, line := range strings.Split(text, "\n") {
		switch {
		case containsAny(line, begins):
			inside = true
			continue
		case inside && containsAny(line, ends):
			inside = false
			continue
		}
		if inside {
			continue
		}
		for _, match := range AnchorPattern.FindAllStringSubmatch(line, -1) {
			out = append(out, AnchorOccurrence{
				Line: index + 1,
				Text: line,
				Ref:  match[0],
				File: match[1],
				Num:  match[2],
			})
		}
	}
	return out
}

// containsAny reports whether line carries any of the markers. It exists so the
// two marker spellings (Marker, VerbIndexMarker) are honoured by ONE loop shape
// in both readers instead of a hard-coded pair in each.
func containsAny(line string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

// proseAnchorDocsCarryingAnchors is the COUNT of documents that publish at
// least one hand-written anchor on this corpus, and it exists so a scan which
// judged nothing is a REFUSAL rather than a green (Rule 15/38/42).
//
// It is deliberately a FLOOR, not a list of names: the population is every
// generated document's prose (see anchorPopulations), so a document the walk
// finds tomorrow is judged without an edit here — while a tree in which
// NOTHING was judged fails, which is the shape that lets a broken reader pass
// as a clean corpus (measured 2026-10-25: RecordAnchors returned zero over a
// block publishing 86 and the invariant read as silence).
const proseAnchorDocFloor = 1

// The two sentences regionAnchorFindings raises. They are built here, not at a
// consumer, because they state a fact about THIS read (the line the anchor
// reached) that a consumer cannot re-derive from the anchor alone.
const (
	// EmptyGateFindingFormat is the NOTE an empty gate-package set raises when a
	// candidate gate could still be derived. It is the sentence this edge has
	// always carried, word for word: a consumer that reads it (or matches on it)
	// sees exactly what it saw before the ERROR class existed.
	EmptyGateFindingFormat = "%s: no test package reads this document — a line moved under " +
		"one of its anchors would reach the integration branch uncaught%s"
	// EmptyGateDerivedMark is the STABLE marker of the empty-gate-package ERROR
	// edge class — the substring an arm selects that sub-class by, so a consumer
	// never re-types the sentence (Rule 1b/14) and never matches on prose it
	// happens to share with another ERROR class.
	//
	// The channel is why the marker exists at all. Answer.Errors carries EVERY
	// ERROR-class finding the edge raises, and the classes have different
	// remedies; an arm that refused on the whole channel would refuse on the
	// DECLARATION invariant too, and that is a measured regression, not a
	// hypothetical one: task 01a1379477077d60887376e52b75441b red
	// TestBranchCheckAnchorArmRefusesADroppedRegeneration the moment the
	// invariant rode the same channel an arm read — the lane refused EARLIER, on
	// the invariant, and the gate the document obliged never ran. The refusal an
	// operator reads must be the one this class is about.
	EmptyGateDerivedMark = "no test package reads this document, and none CAN"
	// EmptyGateDerivedFormat is the ERROR-level class (ErrorFindingPrefix) for the
	// empty gate-package set whose derivation is IMPOSSIBLE — Dep.GateUnderivable.
	//
	// THE DEFECT IT CLOSES (measured 2026-10-14, and it is the shape this whole
	// package was built for): a ROOT document that carries a generated region and
	// states its own regeneration instruction, whose generator lives under
	// `testdata/`. GatePackages walks BaseDirs (the module's own layout), so it
	// never looks at a generator's directory, and the document's gate set comes
	// back EMPTY — the walk found no package whose TEST reads a document only the
	// generator program names. The edge knew, reported a FINDING, and every
	// committing path that refuses on a broken edge kept merging: a line moved
	// under one of the document's anchors reached the integration branch with
	// nothing anywhere able to catch it. That is not a note about the corpus, it
	// is a hole in the guard, and it is stated at the severity the committing
	// paths act on.
	//
	// WHY A GENERATOR UNDER testdata/ PRODUCES ZERO GATE PACKAGES, positively:
	// the generator is a Go PROGRAM, and the Go tool does not test a program that
	// lives where the toolchain's own convention puts data (TestDataDir) — so the
	// only package that could name this document in a test is the generator's own
	// directory, and `go test` builds nothing for it. The command the document
	// itself instructs a reader to run is therefore the only consumer of its
	// INPUT, which is why the sentence says so rather than blaming the corpus.
	EmptyGateDerivedFormat = ErrorFindingPrefix + "%s: " + EmptyGateDerivedMark +
		" — the document's own generator %s names it, and a generator is a program the Go tool does not test " +
		"(it lives under " + TestDataDir + "), so a line moved under one of its anchors would reach the " +
		"integration branch uncaught by any package the edge could run"

	// regionAnchorNotADeclarationFormat — the anchor resolves, but to a line
	// that declares nothing.
	//
	// The first %s is the POPULATION the anchor was read from (see
	// anchorPopulations), not a re-typed "generated region": the same predicate
	// judges the hand-written prose beside it, and a sentence that named the
	// region for both would tell the reader to regenerate a block nobody
	// generated.
	regionAnchorNotADeclarationFormat = ErrorFindingPrefix + "%s: the anchor %s published %s points at %s, " +
		"not a declaration — the line the anchor was written for moved (a blank/comment line still RESOLVES, " +
		"so the drift is invisible to a reference check that only asks whether the anchor lands on a line)"
	// regionAnchorTestOwnedFormat — the anchor's file belongs to a test.
	//
	// The first %s is the POPULATION, as above. The severity is the same NOTE the
	// region arm has always carried (it is not an ErrorFindingPrefix class), and
	// widening the population deliberately keeps it that way: a test-owned anchor
	// is a statement about where a fact lives, not a stale address.
	regionAnchorTestOwnedFormat = "%s: the anchor %s published %s is owned by a test " +
		"(%s) — a production document that cites a test as the home of a fact has no production source to " +
		"drift against, so nothing but that test can keep it honest"
	// proseAnchorPopulationFormat is the POPULATION sentence for the anchors a
	// document carries OUTSIDE its generated regions. Its wording says what the
	// reader must do about it, because the remedy differs from the block's: no
	// generator will rewrite this half, so it is re-pointed by hand.
	proseAnchorPopulationFormat = "in the hand-written prose beside its generated region (no generator re-derives this — " +
		"re-point it at the declaration it describes)"
	// regionAnchorPopulationFormat is the POPULATION sentence for the anchors a
	// generator renders. It is the sentence this invariant has carried since it
	// existed, word for word.
	regionAnchorPopulationFormat = "inside its generated region"
)

// regionAnchorFindings reports every anchor published inside dep's generated
// region that does not resolve to a DECLARATION in a production source file of
// the tree rooted at root.
//
// THE DEFECT THIS IS FOR (measured 2026-10-13, task 01a13528). The
// generated verb index inside COMMANDS.md publishes `cmd/agent/cli_ui.go:126`
// for `gopi ui`; a refactor inserted a line above the predicate and did not
// re-run the generator, so the block published `:125` — a comment line about
// the predicate. The anchor RESOLVED (line 125 exists), so the
// anchor-resolution check (whose criterion was a
// BLANK line — deleted with internal/docclass in 5a35f61, so the comparison
// described here is what remains) passed the stale cell, and the only thing that
// caught it was the
// block-vs-source comparison — a check that has to run, in a package the
// committing branch may never run. Two anchors were stale that way at once
// (`cli_ui.go:125`, `cli_doccheck.go:234`), both pointing at the comment ABOVE
// their declaration: a shape nothing on an arbitrary changed-path set can see,
// and the shape this invariant names.
//
// What it deliberately does NOT do: it does not re-run the generator (that is
// the block's own check, and a second opinion about the same fact is the drift
// this package prevents), and it does not report an anchor whose file is missing
// from the tree or whose line is past the end of the file — those are the
// dangling-reference class another check owns, and re-deciding them here would
// report this invariant's name against a different defect.
//
// THE POPULATION WAS WIDENED (measured 2026-11-02, task 01a195c9). Until this
// change the walk read RegionAnchors and nothing else, so a document's
// HAND-WRITTEN anchors were never content-checked at all: COMMANDS.md carried
// 107 prose spans beside its 84 rendered ones, and 20 of them pointed at comment
// or blank lines while every gate stayed green (both spans this row was opened
// for — `cmd/agent/cli_scratch_gc.go:555` and `:594` — were of that shape). The
// population is now the document's anchors with the region's excluded
// (ProseAnchors), and the predicate underneath is UNCHANGED: the same
// blank/comment/absent-declaration test, the same "could not be measured"
// finding. Widening a population is not relaxing an invariant — a narrowed
// predicate would have bought the same green by judging less.
//
// ONE sub-predicate IS population-scoped, deliberately: the test-owned NOTE
// (see anchorPopulation.judgesTestOwnership). It states a defect of a GENERATOR
// — "a production document that cites a test as the home of a fact has no
// production source to drift against" — and holding hand-written prose to it
// would forbid the page from documenting its own gates (measured: five
// legitimate citations on this corpus, all of them tests the page is ABOUT).
//
// A document with anchors OUTSIDE a region but NO region at all is still not a
// Dep: Deps is a walk over generated documents (`marked` in DepsSurvey), and
// turning every markdown anchor into an edge would change what that walk means.
// That limit is stated rather than engineered around.
func regionAnchorFindings(root string, dep Dep) []string {
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dep.Doc)))
	if err != nil {
		// A document this Dep names but the tree does not carry is not
		// measurable here; the anchor-resolution class owns it. Reporting it as
		// a declaration finding would attach THIS invariant's name to a defect
		// it did not measure (Rule 15: name the failure you actually saw).
		return nil
	}
	var findings []string
	for _, pop := range anchorPopulations(string(body), dep) {
		findings = append(findings, anchorFindings(root, dep.Doc, pop)...)
	}
	return findings
}

// anchorPopulation is ONE set of anchors with the sentence a finding says about
// it. The sentence is a field rather than a caller's argument so the two
// populations cannot be reported under each other's name (Rule 1b/14): the
// remedy differs — a block is regenerated, prose is re-pointed by hand — and a
// reader told the wrong one would do the wrong thing.
type anchorPopulation struct {
	refs     []string
	sentence string
	// judgesTestOwnership turns on the test-owned NOTE for this population. It
	// is population-scoped, not a severity relaxation: the note says "a
	// PRODUCTION DOCUMENT that cites a test as the home of a fact has no
	// production source to drift against", and that defect is a GENERATOR
	// publishing a test's line — the generator derived a fact from a test.
	// Hand-written prose citing a test is the opposite: a page about the CLI
	// MUST be able to say "the contract suite asserts this"
	// (`cmd/agent/cli_contracts_test.go:361`, four more like it in COMMANDS.md),
	// and the note rides the Findings channel every committing path refuses on,
	// so holding prose to it would forbid the document from documenting its own
	// gates. Measured 2026-11-02: five such citations, all legitimate.
	judgesTestOwnership bool
}

// anchorPopulations returns the populations this invariant judges for one
// document, in a stable order: the rendered region FIRST (the sentence every
// existing consumer reads), then the hand-written prose beside it.
func anchorPopulations(text string, dep Dep) []anchorPopulation {
	populations := []anchorPopulation{{
		refs:                RegionAnchors(text, Marker, VerbIndexMarker),
		sentence:            regionAnchorPopulationFormat,
		judgesTestOwnership: true,
	}}
	populations = append(populations, anchorPopulation{
		refs:     ProseAnchors(text, Marker, VerbIndexMarker),
		sentence: proseAnchorPopulationFormat,
	})
	return populations
}

// anchorFindings is the DECLARATION predicate over ONE population: every anchor
// that does not resolve to a declaration in a production source file of the
// tree rooted at root is reported, under the population's own sentence.
func anchorFindings(root, doc string, pop anchorPopulation) []string {
	var findings []string
	for _, ref := range pop.refs {
		file, numText, ok := strings.Cut(ref, ":")
		if !ok {
			continue
		}
		if pop.judgesTestOwnership && (strings.HasSuffix(file, TestSuffix) || strings.Contains(file, TestDataDir)) {
			findings = append(findings, fmt.Sprintf(regionAnchorTestOwnedFormat, doc, ref, pop.sentence, file))
			continue
		}
		num, err := strconv.Atoi(numText)
		if err != nil {
			// A SYMBOL-identity anchor: the block names the declaration instead
			// of the line it sits on, so "is the anchor still pointing at what
			// it was written for" is answered by looking the NAME up rather than
			// by reading one line. Skipping it (the pre-fix behaviour, where the
			// pattern matched only `\d+` and this branch never ran) is exactly
			// the vacuity the invariant must not have: the whole real block is
			// symbol-addressed, so a skip here is a check reading nothing.
			decl, declErr := declarationInFile(root, file, numText)
			if declErr != nil {
				// The anchor could not be MEASURED (the cited file is absent or
				// unparsable). Reporting it as "no declaration of that name"
				// would name a fact this read never established (Rule 15), but
				// skipping it would let an anchor nobody could look up read as a
				// satisfied one — so it is a finding, and the finding says
				// which failure it actually saw.
				findings = append(findings, fmt.Sprintf(regionAnchorNotADeclarationFormat, doc, ref, pop.sentence,
					fmt.Sprintf("an anchor that could not be measured (%v)", declErr)))
				continue
			}
			if !decl.OK {
				findings = append(findings, fmt.Sprintf(regionAnchorNotADeclarationFormat, doc, ref, pop.sentence, decl.Verdict))
			}
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			continue
		}
		lines := strings.Split(string(src), "\n")
		if num < 1 || num > len(lines) {
			continue
		}
		trimmed := strings.TrimSpace(lines[num-1])
		switch {
		case trimmed == "":
			findings = append(findings, fmt.Sprintf(regionAnchorNotADeclarationFormat, doc, ref, pop.sentence, AnchorBlankLineVerdict))
		case strings.HasPrefix(trimmed, CommentPrefix):
			findings = append(findings, fmt.Sprintf(regionAnchorNotADeclarationFormat, doc, ref, pop.sentence, AnchorCommentLineVerdict))
		case isSeparatorOnlyLine(trimmed):
			// THE THIRD OUTCOME (task 01a1b0c7). A target made of nothing but
			// brackets and separators declares nothing, so an anchor landing
			// there is as misplaced as one on a comment — and before this arm
			// existed the predicate fell through to GREEN, so the drift was
			// invisible to BOTH populations. It is an ADDITION: the two arms
			// above are untouched, and the order is the measured one (a `// }`
			// line is a comment before it is punctuation).
			findings = append(findings, fmt.Sprintf(regionAnchorNotADeclarationFormat, doc, ref, pop.sentence, AnchorSeparatorOnlyVerdict))
		}
	}
	return findings
}

// separatorOnlyPunctuation is the character class a STRUCTURAL line is made of:
// the brackets and separators a Go declaration's skeleton is spelled in, and
// nothing else. Letters, digits, spaces, quotes, operators and the semicolon are
// deliberately ABSENT, because the predicate below is a claim about the whole
// line — a line carrying ANY of them says something (`if err != nil {`,
// `} else {`, `foo(a, b)`) and is not structural punctuation.
//
// It is a NAMED constant rather than a literal inside the predicate (Rule 1/13)
// because the wall this arm must not cross is defined by exactly this set: widen
// it and `} else {` starts reading as punctuation, which is the false positive
// the measured wall in the arm's own test forbids.
const separatorOnlyPunctuation = "{}()[],"

// isSeparatorOnlyLine reports whether a trimmed source line is STRUCTURAL
// PUNCTUATION — every one of its bytes is in separatorOnlyPunctuation, and it
// carries at least one.
//
// WHY THIS PREDICATE LIVES HERE AND IS DEFINED ONCE (Rules 1/13/14). The task
// that commissioned it named `internal/standards/prose_anchor_gate.go` and a
// `proseAnchorSeparatorOnly` spelling to reuse. Measured on this tree at the
// base commit (05f8f88): `git ls-files | grep -i prose_anchor` returns nothing
// and `grep -rn proseAnchorSeparatorOnly internal/` matches nothing — the file
// and the symbol DO NOT EXIST, so there was nothing to reuse; inventing a
// dependency on an absent package to satisfy a cross-package-sharing rule would
// be worse than the duplicate the rule forbids. The two real candidates also
// cannot host it: `internal/standards`'s doc gates judge CONFIG.md, which
// carries NO generated marker and therefore never reaches this predicate, and
// importing `internal/standards` from `internal/gendoc` (or back) would put a
// gate package on a library's import graph. So it is defined ONCE, here, and a
// future consumer imports THIS one.
//
// THE FALSE-POSITIVE WALL, MEASURED (never assumed). The wall is: a line that
// conventional Go spelling puts at the START of a structural character but that
// carries MEANING — `} else {`, `} else if x {`, `} else {` in a switch, and a
// code-block continuation like `},` followed by a comment. Every one of them
// carries at least one non-punctuation byte, so `isSeparatorOnlyLine` is false
// for all of them and the arm stays silent. What IS structural alone, and is
// therefore judged, is only the closing/opening brace on its own line.
func isSeparatorOnlyLine(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	for _, r := range trimmed {
		if !strings.ContainsRune(separatorOnlyPunctuation, r) {
			return false
		}
	}
	return true
}

// declarationVerdict is what a symbol-identity anchor resolved to: whether the
// name IS a declaration in the cited file, and — when it is not — the sentence
// fragment the finding carries, so the reader is told what it landed on rather
// than only that it failed.
type declarationVerdict struct {
	OK      bool
	Verdict string
}

// declarationInFile reports whether name is DECLARED in the production Go source
// rel under root, using the Go parser rather than a line scan: a symbol anchor
// addresses a declaration by name, so the truthful question is "does this file
// declare it", and only a parse answers that (a `strings.Contains` would be
// satisfied by a mention in a comment or a call site — the same class of
// false green the line-number shape had when a stale number still RESOLVED).
//
// An error return means the file could not be measured at all (absent,
// unparsable); the caller must report that as a finding of its own, never as a
// pass — an unmeasurable edge reading as a satisfied one is the vacuity this
// whole arm exists to refuse (Rule 15).
func declarationInFile(root, rel, name string) (declarationVerdict, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	src, err := os.ReadFile(full)
	if err != nil {
		return declarationVerdict{}, err
	}
	file, err := parser.ParseFile(token.NewFileSet(), full, src, parser.SkipObjectResolution)
	if err != nil {
		return declarationVerdict{}, err
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.FuncDecl:
			if d.Name != nil && d.Name.Name == name {
				found = true
			}
		case *ast.ValueSpec:
			for _, nm := range d.Names {
				if nm.Name == name {
					found = true
				}
			}
		case *ast.TypeSpec:
			if d.Name != nil && d.Name.Name == name {
				found = true
			}
		}
		return !found
	})
	if !found {
		return declarationVerdict{Verdict: "no declaration of that name in the file"}, nil
	}
	return declarationVerdict{OK: true}, nil
}

// Quoted renders a maybe-empty value for a finding, so an empty
// generator reads as a stated fact rather than as a gap in the sentence.
func Quoted(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

// SkippedBaseNote qualifies a finding with the base directories this
// tree does not have, so a "no package reads this document" answer on a
// differently-shaped tree states the reason it is short rather than reading as
// the real tree's verdict (Rule 15). Empty when every base was walked.
func SkippedBaseNote(dep Dep) string {
	if len(dep.SkippedBases) == 0 {
		return ""
	}
	var b strings.Builder
	for _, base := range dep.SkippedBases {
		fmt.Fprintf(&b, AbsentBaseNote, base)
	}
	return b.String()
}

// PathExists reports whether a tree-relative path is a file in the
// tree rooted at root. A root that could not be resolved reports NOT-exists: an
// unmeasurable edge must read as a finding, never as a satisfied one.
func PathExists(root, rel string) bool {
	if root == "" || rel == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil && !info.IsDir()
}

// dedupSorted returns the values sorted and de-duplicated.
func dedupSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
