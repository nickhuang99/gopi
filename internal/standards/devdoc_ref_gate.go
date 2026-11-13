package standardgate

// devdoc reference gate — a citation in a production comment must name a file
// that EXISTS in this tree.
//
// WHY THIS GATE EXISTS (the measured defect, 2026-11-14). Two production
// comments pointed a reader at a devdoc that has never existed in any commit of
// this repository: `git log --all --oneline -- <that path>` printed nothing, so
// the pointer was unreachable from the day it was typed — and it was typed
// TWICE, in two packages, as the supporting evidence for a claim. A reader who
// follows a citation and finds nothing has lost the ability to check the claim
// it supports, and the shape is silent: a comment cannot fail to compile.
//
// The same sweep found thirteen more: refs to documents that were never
// committed, and refs that are not citations at all (an illustrative example in
// prose, a quoted dirty-entry listing, a fixture's sample path). The gate
// therefore judges ALL of them and gives the second class the inline escape, so
// a reader can tell "this citation is checkable" from "this is an example".
//
// WHAT IT JUDGES. Every `devdoc/<name>.md` occurrence inside a COMMENT of a
// PRODUCTION Go file under prodRoots. Not string literals (a fixture may need to
// carry a path as data), not _test.go (a test's fixture corpus is its own
// business), not the devdoc corpus itself (a document citing a sibling document
// is prose, and that corpus has its own gate).
//
// WHAT IT NEVER DOES. It never creates the missing document and never rewrites
// the comment: whether a pointer should be deleted, repointed at a document that
// does exist, or backed by a new one is the author's judgement. The gate only
// refuses to let an unreachable pointer stand.
//
// A REFERENCE IT CANNOT RESOLVE IS A HIT, NEVER A PASS (Rule 15). The
// existence check needs the module root; the root is derived from the scanned
// file's own absolute path (the parser's file set carries it) and falls back to
// repoRootForScan. When neither answers, the site is reported with that reason
// spelled out, because "I could not look" and "it is there" must never print the
// same thing.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// devdocRefEscape marks a reported line that deliberately carries the
	// reference shape WITHOUT being a citation — an illustrative example in
	// prose, a quoted message, a fixture's sample path. Same inline-escape
	// pattern as the Rules 16-21 gates: a reviewed, visible exception on the
	// line itself, never a baseline entry that rots.
	devdocRefEscape = "// devdoc-ref-ok:"

	// devdocRefRootDir is the corpus a citation names, spelled once (Rule 14).
	devdocRefRootDir = "devdoc"

	// devdocRefExt is the extension a citation must carry: a devdoc is a
	// markdown file, and any other extension is not this gate's subject.
	devdocRefExt = ".md"

	// devdocRefFileFloor and devdocRefCitationFloor are the non-vacuity floors:
	// how many production files the walk must READ and how many citation
	// occurrences it must FIND for this gate to be believed. A scan that
	// silently stopped matching would otherwise read green forever (the shape
	// Rule 42's own history documents: a named check that matches nothing
	// prints `no tests to run` and exits 0).
	//
	// MEASURED at the commit that landed this gate, by the real-tree arm's own
	// printed line: 1134 production files read, 460 citation occurrences (of
	// which 9 are escaped non-citations, i.e. the population this gate actually
	// judges is 451). The floors sit well below
	// those numbers so a legitimate comment clean-up cannot fail the gate while
	// a broken recognizer does.
	devdocRefFileFloor     = 250
	devdocRefCitationFloor = 100
)

// devdocRefRe matches a citation as it is typed in prose. The character class is
// NARROW — the names this corpus uses and nothing else — but it INCLUDES the
// slash, because the corpus has subdirectories (`devdoc/competitor/gap-cline.md`,
// `devdoc/requirements/merge-and-branches.md`) and a recognizer that stopped at
// the first separator would leave every nested citation unchecked. A placeholder
// written with angle brackets matches nothing at all: the gate must not be its
// own first violation, and prose ABOUT the rule should not need the escape that
// suppresses a real citation.
var devdocRefRe = regexp.MustCompile(devdocRefRootDir + `/[A-Za-z0-9_./-]+` + regexp.QuoteMeta(devdocRefExt))

// devdocRefFind reports every citation in one parsed production file whose
// target does not exist in this tree, plus any citation it could not resolve at
// all.
//
// The walk is over the file's COMMENT groups (the parser ran with
// ParseComments), so a path spelled inside a string literal — a fixture, a
// generated message, an argv — is not a hit. A comment group that is an escape
// line is still reported and classified by enforce(), exactly like every other
// gate: the escape does not hide a site, it explains it.
func devdocRefFind(g gate, p *parsed) []hit {
	if p == nil || p.file == nil || p.fset == nil {
		return []hit{{rel: relOf(p), line: 1, desc: "devdoc citation scan: the file was not parsed with comments, " +
			"so its citations could not be read — that is a defect of the GATE, not of the tree"}}
	}
	root := devdocRefRoot(p)
	var hits []hit
	for _, site := range devdocRefSites(p) {
		desc, missing := devdocRefMissing(root, site.ref)
		if missing {
			hits = append(hits, hit{rel: p.rel, line: site.line, desc: desc})
		}
	}
	return hits
}

// devdocRefSite is one citation occurrence: the ref as written and the line of
// the COMMENT that carries it. The gate's population is the set of these, which
// is what the non-vacuity floor counts — counting only the FAILING ones (the
// hits) would make a recognizer that matched a single file look like a healthy
// scan of the corpus.
type devdocRefSite struct {
	ref  string
	line int
}

// devdocRefSites enumerates every citation occurrence in one file's comments.
// It is the ONE walker: devdocRefFind and the real-tree arm both read it, so the
// population a floor is measured over and the population that is judged cannot
// drift apart (Rule 14).
func devdocRefSites(p *parsed) []devdocRefSite {
	if p == nil || p.file == nil || p.fset == nil {
		return nil
	}
	var sites []devdocRefSite
	for _, group := range p.file.Comments {
		for _, c := range group.List {
			line := p.fset.Position(c.Pos()).Line
			for _, ref := range devdocRefRe.FindAllString(c.Text, -1) {
				sites = append(sites, devdocRefSite{ref: ref, line: line})
			}
		}
	}
	return sites
}

// relOf names the file a defect belongs to when the parsed value cannot (a nil
// p has no path, and a hit that named nothing would be unactionable).
func relOf(p *parsed) string {
	if p == nil || p.rel == "" {
		return "?"
	}
	return p.rel
}

// devdocRefMissing answers "is this citation unreachable?", returning the
// sentence that says WHY so the failure is actionable as printed.
//
// Three answers, and they are deliberately three (Rule 15):
//   - the root is unknown  → "could not be resolved" (never "missing");
//   - the target is absent → "does not exist";
//   - the target is present → no hit.
func devdocRefMissing(root, ref string) (string, bool) {
	if root == "" {
		return fmt.Sprintf("%s could not be resolved — the module root could not be located from %q, so the "+
			"citation's existence was NOT checked (a scan that cannot look must not read green)", ref, ref), true
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ref))); err != nil {
		return fmt.Sprintf("%s does not exist in this tree — a citation a reader cannot follow is a claim a "+
			"reader cannot check (git log --all -- %s is empty)", ref, ref), true
	}
	return "", false
}

// devdocRefRoot locates the module root for one parsed file from the parser's
// own file set, which carries the ABSOLUTE path the file was read from: stripping
// the repo-relative path off it yields the root without a second walk. The
// repoRootForScan fallback keeps a caller that parsed synthetic source working
// (and reports the same root the rest of the suite uses).
func devdocRefRoot(p *parsed) string {
	if p != nil && p.fset != nil && p.file != nil {
		abs := p.fset.Position(p.file.Pos()).Filename
		if suffix := filepath.FromSlash(p.rel); suffix != "" && strings.HasSuffix(abs, suffix) {
			if root := strings.TrimSuffix(abs, suffix); root != "" {
				return filepath.Clean(root)
			}
		}
	}
	return repoRootForScan()
}

// devdocRefGate describes the gate for the registry.
func devdocRefGate() gate {
	return gate{
		id:           "a devdoc citation names a file that exists",
		checklistRow: "devdoc-citation",
		probe:        "TestGate10DevdocRefRedProof",
		escape:       devdocRefEscape,
		baselineReason: "no existing site: the fourteen unreachable citations and the six non-citation references " +
			"the gate found on the commit that landed it were FIXED in that commit (the pointers deleted, the " +
			"examples marked with the inline escape), so the baseline is empty — a new unreachable citation " +
			"fails on the commit that introduces it.",
		baseline: devdocRefBaseline,
		find:     devdocRefFind,
	}
}

// devdocRefBaseline is empty BY CONSTRUCTION: every site the first scan reported
// was repaired rather than permitted, so the ratchet starts at zero and a
// baseline entry here would be a silent permission for exactly the defect the
// gate exists to report.
var devdocRefBaseline []string

// devdocRefTargets returns the distinct citation targets a set of hits named,
// sorted — the list a repair pass works from, and what the real-tree test prints
// so a reader can see the population it judged rather than only its failures.
func devdocRefTargets(hits []hit) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range hits {
		for _, ref := range devdocRefRe.FindAllString(h.desc, -1) {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			out = append(out, ref)
		}
	}
	sort.Strings(out)
	return out
}
