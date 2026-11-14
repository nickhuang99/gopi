// Package standardgate implements machine-checkable coding-standard gates that
// run as part of `go test ./...` — no external linter, no CI, no CGO, and no new
// dependency (stdlib go/parser + go/token + go/ast only).
//
// Why a test gate rather than a linter config: GoPi has no linter history and
// ~160 production files carrying existing violations. A whole-tree blocking
// linter would be red on day one and would therefore be switched off. A
// `go test` gate with a *baseline allow-list* is green the day it lands and
// turns red on the first NEW violation — it ratchets instead of demanding a
// big-bang migration (devdoc/coding-standards-checklist.md §11).
//
// Each gate has three parts:
//
//  1. a pure predicate over one parsed file that reports hits;
//  2. a repo-wide scan that fails on any hit which is neither in the per-gate
//     baseline (an already-known legacy violation) nor carries the documented
//     inline escape on its own line;
//  3. probe tests that feed a deliberately violating snippet to the predicate
//     and assert it reports, plus a compliant snippet that asserts it does not.
//     The probes are what keep a gate non-vacuous: a predicate that silently
//     stopped matching would otherwise leave the gate green forever.
//
// Baselines are exact SITE entries rather than file globs, so the allow-list
// cannot rot into a blanket file exemption, and a baseline entry that no longer
// matches is reported too: a stale exemption is a silent permission.
//
// A site is identified by its CONTENT, not by its line number — the key is
// `<path> \t <normalized line text> \t #<ordinal among identical lines>`, minted
// by internal/sitekey. The key used to be a bare `file:line`, and that key was
// a function of the LINE NUMBER, so every edit ABOVE a baselined site rotted its
// entry: one defect, reported twice by one gate (a NEW violation at the new line
// and a STALE entry at the old one). Measured 2026-10-16:
// anchor_number_material_test.go moved :155 -> :166, and the
// same shape reappeared at internal/tools/git_child_locale_test.go :243 -> :248
// as soon as the first was re-keyed by hand. See
// devdoc/standards-baseline-verdict-content-key.md.
//
// WHAT A CONTENT KEY DOES NOT FIX (so a reader does not mistake it for immunity
// it does not have): editing the SITE'S OWN line moves the key, and so does
// inserting or deleting an EARLIER line whose normalized text is identical to
// the site's (that renumbers the ordinal). Both are content edits at or beside
// the site, and both report loudly — STALE plus NEW — never a silent pass.
package standardgate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/nickhuang99/gopi/internal/sitekey"
)

// maxBaselineEntries is the review budget for one gate's allow-list. A baseline
// no human can read in one sitting is not a reviewed allow-list, it is a
// suppressed tree — and a gate suppressed by its own baseline reads green, which
// is worse than no gate.
//
// The value is a CEILING the gates sit under — after the style sweep (branch
// feat/style-sweep, which ran the two codemods this comment used to schedule)
// the legacy gates are at Rule 16 = 10 and Rule 17 = 100 entries, and it is the
// bar any future codemod has to clear: the ratchet target is to bring each gate
// under a third of this ceiling (devdoc/standards-gate.md, devdoc/style-sweep-20260912.md).
// Lowering it is the ratchet; a gate that hits it fails instead of quietly
// growing, so raising it is a deliberate, reviewable edit rather than silent
// drift.
const maxBaselineEntries = 400

// probeArmRoot is the ONE name a reader is told to run for a gate: every gate
// is enforced as a SUBTEST of this test, `-run 'TestStandardGates/<probe>'`.
//
// WHY THIS CONSTANT EXISTS (the defect it makes unrepresentable, measured
// 2026-10-28 in task 01a18235): the registry published each gate's SHORT probe
// name — `gate.probe`, e.g. TestGate9GitEnvRedProof — and that name selects the
// standalone red proof, a predicate test over a planted snippet that never reads
// the tree. So `go test ./internal/standards/ -run 'TestGate9GitEnvRedProof'`
// exited 0 while three unannotated gate-9 hits sat in the tree: the documented
// enforcement command could not fail. The arm that reads the tree is the subtest
// below, and the two arms are deliberate opposites — the red proof must NOT read
// the tree (it is the predicate's own proof), the subtest must (scan →
// classifyAll → enforce). Collapsing them into one test would delete one half of
// the design, so the fix is a NAME: gate.probePath() is what the registry, the
// baseline block and every doc publish, and gate.probe stays the red proof's own
// name, never offered as a command to enforce anything.
const probeArmRoot = "TestStandardGates"

// probeRunPrefix is the command prefix both readers use to publish a gate's
// enforcement command (Rule 13: one spelling, referenced everywhere).
const probeRunPrefix = "go test -run "

// moduleFileName locates the module root from any test's working directory (a
// test binary's CWD is its own package directory).
const moduleFileName = "go.mod"

// prodRoots are the trees holding production (non-test) Go code. Scoped
// explicitly so the scan cannot wander into unrelated trees.
var prodRoots = []string{"internal", "cmd"}

// testRoots are the trees holding _test.go files. It is prodRoots plus
// e2eTestRoot, the top-level end-to-end suite: the Rule 17 test-env leak it
// guards against is a property of EVERY test in the process, and a leak
// introduced in the e2e suite pollutes sibling tests exactly as one in
// internal/ does. Only a gate with wantTest = true (Rule 17) scans it today;
// the other gates keep prodRoots so their line-keyed baselines are untouched.
var testRoots = append(append([]string{}, prodRoots...), e2eTestRoot)

// prodAndTestRoots is the ONE list a two-topology gate scans, in the ONE order
// its two halves must keep: the production files first (the topology the gate
// has always covered, so its long-standing behaviour is the default) and the
// test files second. It is testRoots by identity — a gate whose second pass
// must reach the e2e suite would otherwise need a third copy of the root list.
var prodAndTestRoots = testRoots

// textScanRoot is the one root the conflict-marker gate walks: the whole module
// tree, because a conflict marker is a property of ANY text file and has nothing
// to do with which tree a file lives in. prodRoots/testRoots cannot express that
// (they are Go-only by construction), so this gate names its own root and the
// skip rule below decides which files it can read.
const textScanRoot = "."

// textRoot and goSourceSuffix: the conflict-marker gate reads EVERY text file
// under textScanRoot (deferring the binary question to git — see textFilesUnder)
// while still refusing to read the compiled artefacts that share the tree with
// no marker of their own.
const goSourceSuffix = ".go"

// maxConflictedTextFileBytes bounds one file the conflict-marker gate will
// READ. A conflict marker is a line in a text file; a file above this size is
// a data blob or a checked-in artefact, not a source document, and reading it
// only to grep it is the cost this ceiling refuses. The ceiling is deliberately
// generous (the largest tracked text file in this repo is orders of magnitude
// below it) so the gate never silently stops covering a shape a human would
// call "a text file".
const maxConflictedTextFileBytes = 8 << 20 // 8 MiB

// Conflict-marker vocabulary: the two marker shapes git writes into a file it
// cannot merge. They are named here because they ARE the subject of this gate —
// every other mention in the tree (doc prose, a fixture, a quoted diff) is
// either inside a fenced block (covered below) or uses a marker this gate
// deliberately does not spell out.
//
// A marker is the seven-character run only when the line STARTS with it: git
// writes markers in column 0, and an indented run is prose (a nested quote).
// The trailing shape is equally fixed — git appends the ref name after a space,
// so `^<<<<<<< HEAD` and `^>>>>>>> fix/some-branch` match, while a seven-char
// run inside a sentence or a longer run (`<<<<<<<<`) does not (see
// conflictMarkerRe).
//
// The `=======` line is NOT in this gate on purpose, and the omission is
// measured, not a guess: `=======` is a legitimate Markdown heading underline
// (setext H1) and appears in this repo's own documents, and the variant with
// more or fewer `=` is a banner in shell fixtures. A git marker run with no
// `<<<<<<<`/`>>>>>>>` partner is not what a half-resolved merge looks like, so
// the two-sided pair is the whole subject: precision here is worth more than
// the extra hit.
//
// conflictFence is the delimiter that quotes the vocabulary: a marker line
// inside a fenced code block is documentation OF a conflict (GoPi's own
// devdoc/repo-context-gate-reverify.md prints the verbatim three-line shape),
// never an unresolved merge — that is why the subject is stated in one place
// here and NOT deleted from the document it documents.
//
// The marker runs themselves are ASSEMBLED from these components rather than
// written out: a file that spells the run it reports becomes its own first
// violation, and a gate satisfied by wrapping its own source in a fence is one
// step from being satisfied by deleting the subject. One component per side plus
// one length keeps the vocabulary single-sourced (PROJECT.md Rule 14).
const (
	conflictMarkerOpener = "<"
	conflictMarkerCloser = ">"
	conflictMarkerRunLen = 7
	conflictFence        = "```"
)

// conflictStartMarker / conflictEndMarker are the two marker runs, derived.
var (
	conflictStartMarker = strings.Repeat(conflictMarkerOpener, conflictMarkerRunLen)
	conflictEndMarker   = strings.Repeat(conflictMarkerCloser, conflictMarkerRunLen)
)

// conflictMarkerRe matches a git conflict marker in column 0: the seven-char run
// followed by either the end of the line or a space and a ref name. The run
// length is exact (not `+`), so a longer run of the same character stays out of
// the vocabulary.
var conflictMarkerRe = regexp.MustCompile(fmt.Sprintf(`^(%s{%d}|%s{%d})(?:$| .*$)`,
	conflictMarkerOpener, conflictMarkerRunLen, conflictMarkerCloser, conflictMarkerRunLen))

// e2eTestRoot is the top-level end-to-end test suite — a test tree that lives
// outside prodRoots, named here so the scan list and the traversal share one
// source of truth (PROJECT.md Rule 14: shared values get one definition).
const e2eTestRoot = "testsuite"

// conflictMarkerEscape marks a line that deliberately carries a conflict-marker
// shape outside a fenced block — a fixture whose subject IS the marker. The two
// markers in the escape are spelled by joining the named halves, so this file
// never contains the seven-character literal run it reports (the gate must not
// be its own first violation).
const conflictMarkerEscape = "conflict-marker-ok:"

// hit is one reported violation.
type hit struct {
	rel  string
	line int
	desc string
	// digest is the hit's CONTENT key (internal/sitekey.Key), stamped once by
	// stamp() where the file's bytes are in hand. It is a field rather than a
	// method so that the key is computed at exactly ONE place per scan: two
	// sites cannot disagree about a hit's identity (Rule 14), and a consumer
	// that reads the key never re-reads or re-parses a file to get it.
	//
	// `line` stays on the hit and stays authoritative for the READER — the
	// failure message names the line so a human can land on it — while the
	// baseline is keyed by digest, which is what makes the entry immune to a
	// line-number shift. The two are deliberately separate: a hit whose line
	// moves is the SAME site, and that is the property this change exists for.
	digest string
}

// key renders a hit as the stable identifier used by baselines: its content
// digest when one was stamped, else the empty string — which matches no permit
// and therefore reads as a NEW violation rather than as a silent exemption.
//
// The empty-string fallback is the safe direction, not the intended one: a hit
// that reaches a baseline comparison without a digest is a defect of the
// CALLER (a scan path that forgot to stamp), and enforce() refuses it by name.
func (h hit) key() string { return h.digest }

// keyOn returns the hit's content key, computing it from p when the hit was not
// stamped. It exists for the tests that drive a predicate directly
// (`g.find(g, p)` without the scan): those never see a stamped hit, and they
// must still classify — and re-key — against the same key the scan mints.
//
// The computation is sitekey.Key, the ONE definition, so a direct caller and a
// scan can never disagree about a hit's identity.
func (h hit) keyOn(p *parsed) string {
	if h.digest != "" {
		return h.digest
	}
	if p == nil {
		return ""
	}
	return keyOf(p, h.line)
}

// keyOf renders one line of one parsed file as its content key, or "" when the
// site has no identity (a line the file does not have, or a blank line). The ""
// is deliberately NOT a permit: it matches no baseline entry, so a site that
// cannot be keyed is reported as a NEW violation — loudly, in the gate's own
// terms — instead of being silently excused. enforce() additionally refuses it
// by name, because a hit on a blank line is a defect of the PREDICATE that
// reported it, not a property of the tree.
func keyOf(p *parsed, line int) string {
	if p == nil {
		return ""
	}
	k, err := sitekey.Key(p.rel, line, p.src)
	if err != nil {
		return ""
	}
	return k
}

// stamp fills in every hit's digest from the file it was found in. It is called
// by scan() and scanText() — the two entry points every baseline comparison goes
// through — so classify(), enforce() and both coverage arms consume the SAME key
// for a site (PROJECT.md Rule 14: one definition, no mirroring).
//
// It is a post-pass rather than work inside each predicate for the same reason:
// a predicate's job is to find sites, and a key is a fact about the FILE, so
// computing it per-predicate would put the same logic in twelve places and let
// one of them drift.
func stamp(hits []hit, files map[string]*parsed) {
	for i := range hits {
		if hits[i].digest != "" {
			continue
		}
		hits[i].digest = keyOf(files[hits[i].rel], hits[i].line)
	}
}

// gate bundles one machine-checkable rule.
type gate struct {
	// id is the rule as it appears in GOPI.md.
	id string
	// checklistRow is the checklist row the gate came from (traceability).
	checklistRow string
	// probe is the STANDALONE red proof's test name: the predicate proved over a
	// planted snippet plus a clean control. It deliberately does NOT read the
	// tree (a red proof that scanned the tree would be a second, weaker copy of
	// the enforcement arm and would report tree state as a predicate failure).
	// It is therefore NOT a command that enforces anything — publish
	// probePath() instead (see probeArmRoot).
	probe string
	// wantTest selects the file set: true = _test.go only.
	wantTest bool
	// scanRoots overrides the trees scanned when non-empty. A gate that
	// applies to every test file (Rule 17) scans testRoots so the top-level
	// e2e suite is covered; a gate whose baseline is keyed by line inside
	// internal/ + cmd/ leaves this empty and keeps prodRoots, so adding the
	// e2e tree here cannot re-point another gate's permits.
	scanRoots []string
	// scanTextRoot switches the gate to the TEXT topology: the gate walks every
	// text file under this root (conflictMarkerFind's subject is a line in a
	// file, not a Go construct), and scanRoots/wantTest/scanTests are unused.
	// A file set is selected by exactly one of the two fields, so a text gate
	// can never be silently served a Go-only set.
	scanTextRoot string
	// textCorpus narrows a TEXT gate's walk to the files it owns. Empty = every
	// file the walker returns (the conflict-marker gate: a marker is a property
	// of any text file). It exists so a text gate whose subject is an
	// INSTRUCTION SURFACE — the trees gopi renders into a briefing, as opposed
	// to the devdoc/tasks corpora that QUOTE them — can say so in one place
	// instead of excluding trees inside its predicate, where the exclusion
	// would be invisible to a reader of the scan.
	textCorpus func(rel string) bool
	// scanTests adds a SECOND traversal over the _test.go topology after the
	// production one. It exists for a rule that binds production code and test
	// code DIFFERENTLY (the worktree-root rule: production must reference the
	// constant, a fixture must at least not reach for a bare host root),
	// because a single file set cannot express two rules. The second pass reads
	// secondRoots; the FIRST pass keeps scanRoots/wantTest, so a gate that opts
	// in changes nothing about the traversal it already had.
	scanTests bool
	// secondRoots overrides the trees the scanTests pass walks (empty =
	// prodAndTestRoots, the same roots Rule 17 declares as the test set).
	secondRoots []string
	// escape is the inline marker that suppresses a hit on that line ("" = none).
	escape string
	// escapeTest is the marker for _test.go files when it differs from escape:
	// a gate that holds two topologies to two DIFFERENT rules must let each say
	// WHY in its own words (the worktree-root gate: production's "this literal
	// IS the subject" versus a fixture's "this path must stay where the host
	// put it, or the test inverts"). The dispatch is by FILE, so neither marker
	// can suppress a site in the other topology; both stay visible in the data
	// report, so neither becomes a quiet suppression. Empty = escape applies.
	escapeTest string
	// baselineReason is the one-line allow-list justification for this gate's
	// baseline (rendered into the list's header comment and the data report).
	baselineReason string
	// baseline are exact repo-relative file:line entries for violations that
	// exist at the commit the gate landed. Permit-only: they never exempt the
	// rest of the file.
	baseline []string
	// find reports every hit in one parsed file.
	find func(g gate, p *parsed) []hit
}

// probePath is the test path a reader must run to enforce this gate: the
// real-tree subtest, never the standalone red proof. It is the ONE source of
// the published name (Rule 14) — the registry's baseline block, the NEW-violation
// message and every doc that tells a reader what to run take it from here, so a
// doc can never publish a command that only reaches the fixture arm.
func (g gate) probePath() string {
	return probeArmRoot + "/" + g.probe
}

// baselineOf renders one gate's baseline for the builder below, carrying the
// gate's allow-list reason and the verification commands.
func baselineOf(g gate) *baselineFile {
	return &baselineFile{
		gate:    g.id,
		row:     g.checklistRow,
		why:     g.baselineReason,
		verify:  probeRunPrefix + g.probePath() + " ./internal/standards/",
		entries: g.baseline,
	}
}

// ---------------------------------------------------------------------------
// repo traversal + parsing
// ---------------------------------------------------------------------------

// repoRoot walks up from the current test working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, serr := os.Stat(filepath.Join(dir, moduleFileName)); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no %s above %s — cannot locate the module root", moduleFileName, dir)
		}
		dir = parent
	}
}

// goFilesUnder returns repo-relative slash paths of .go files below roots,
// filtered by wantTest.
func goFilesUnder(t *testing.T, root string, roots []string, wantTest bool) []string {
	t.Helper()
	var out []string
	for _, r := range roots {
		base := filepath.Join(root, filepath.FromSlash(r))
		if _, err := os.Stat(base); err != nil {
			continue
		}
		walkErr := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") != wantTest {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", r, walkErr)
		}
	}
	sort.Strings(out)
	return out
}

// parsed is one parsed source file plus its raw bytes.
type parsed struct {
	rel  string
	fset *token.FileSet
	file *ast.File
	src  []byte
}

// parseSource parses in-memory source (used by the probe tests) under a
// synthetic filename.
func parseSource(t *testing.T, name, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse probe %s: %v", name, err)
	}
	return fset, f
}

// parseRepoFile parses one repo-relative file. A syntax error is a hard failure:
// silently skipping an unparsable file would be a hole in an exhaustive gate.
func parseRepoFile(t *testing.T, root, rel string) *parsed {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, parser.ParseComments)
	if perr != nil {
		t.Fatalf("parse %s: %v", rel, perr)
	}
	return &parsed{rel: rel, fset: fset, file: f, src: src}
}

// conflictMarkerFind reports conflict-marker lines in a text file — the pure
// per-file predicate, so the probe tests can feed it a synthetic document.
//
// THE FENCE RULE IS THE POINT. A `<<<<<<<` line inside a fenced code block is a
// document quoting a conflict (GoPi's devdoc/repo-context-gate-reverify.md does
// exactly that, and that quote is the evidence the document exists to carry) —
// reporting it would push a future agent to DELETE the evidence to satisfy the
// gate, which is the defect this rule was written after: the gate must be
// fence-aware, never satisfied by weakening its subject. Fences are tracked with
// their opening run, so a longer run (a nested fence inside a document) closes
// only on a run at least as long as the one that opened it.
func conflictMarkerFind(g gate, p *parsed) []hit {
	var out []hit
	openRun := ""
	for i, raw := range strings.Split(string(p.src), "\n") {
		if run := fenceOpeningRun(raw); run != "" {
			switch {
			case openRun == "":
				openRun = run // a fenced block opens
			case len(run) >= len(openRun) && strings.TrimRight(strings.TrimSpace(raw), string(conflictFence[0])) == "":
				openRun = "" // …and closes on a run at least as long, alone on its line
			}
			continue
		}
		if openRun != "" {
			continue
		}
		if !conflictMarkerRe.MatchString(raw) {
			continue
		}
		out = append(out, hit{rel: p.rel, line: i + 1, desc: conflictMarkerDesc(raw)})
	}
	return out
}

// fenceOpeningRun returns the leading run of fence characters when the line
// opens or closes a fenced block, "" for every other line. The run is taken from
// the LINE (the whole trimmed prefix), so an indented fence inside a list still
// registers; a fence character mid-line does not (it is inline code, which
// cannot span lines and therefore cannot hide a marker).
func fenceOpeningRun(raw string) string {
	trimmed := strings.TrimSpace(raw)
	n := 0
	for n < len(trimmed) && trimmed[n] == conflictFence[0] {
		n++
	}
	if n < len(conflictFence) {
		return ""
	}
	return trimmed[:n]
}

// conflictMarkerDesc renders one hit for the failure message: the marker as it
// appears (truncated, so one absurd line cannot flood the report) and the two
// remedies.
func conflictMarkerDesc(raw string) string {
	seen := strings.TrimRight(raw, "\r")
	if len(seen) > conflictMarkerDescWidth {
		seen = seen[:conflictMarkerDescWidth] + "…"
	}
	return fmt.Sprintf("conflict marker %q in a text file — resolve the merge, or (if the "+
		"marker is QUOTED evidence) put it inside a fenced block so it reads as a quote", seen)
}

// conflictMarkerDescWidth bounds how much of the offending line a failure
// message quotes back.
const conflictMarkerDescWidth = 48

// binaryProbeBytes is how much of a file the text/binary probe reads. The NUL
// byte is the classic binary signal and every text file on this host is NUL-free
// in its head; probing the head (rather than the whole file) is what keeps one
// oversized or binary file from costing a full read.
const binaryProbeBytes = 8 << 10

// vcsDirName is the one directory the text walk refuses to enter: the object
// store is a graph of packed artefacts, not tree content.
const vcsDirName = ".git"

// textFilesUnder lists the repo-relative path of every TEXT file below
// scanRoot. The text/binary decision is a NUL probe on the file's head plus the
// size ceiling above — deliberately NOT a git query: git's binary heuristic
// needs a git process per scan, and this gate must keep working on a tree git
// cannot speak for (and on files git has never seen, which is exactly where a
// tool-generated marker has been found before). A file above the ceiling or
// carrying a NUL in its head is skipped, never read.
func textFilesUnder(t *testing.T, root, scanRoot string) []string {
	t.Helper()
	var out []string
	base := filepath.Join(root, filepath.FromSlash(scanRoot))
	if _, err := os.Stat(base); err != nil {
		t.Fatalf("scan root %s is missing: %v — a gate that walks nothing reports a "+
			"green tree", scanRoot, err)
	}
	walkErr := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == vcsDirName {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > maxConflictedTextFileBytes {
			return nil
		}
		if isBinaryHead(path) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", scanRoot, walkErr)
	}
	sort.Strings(out)
	return out
}

// isBinaryHead reports whether a file's head carries a NUL byte. An unreadable
// file reports "not binary" and is caught by the read in scanText — a skipped
// read must never become a silent gap in an exhaustive gate.
func isBinaryHead(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, binaryProbeBytes)
	n, err := f.Read(head)
	if n <= 0 && err != nil {
		return false
	}
	return bytes.IndexByte(head[:n], 0) >= 0
}

// scanText runs a text gate over the whole file tree. It is the text counterpart
// of scan(): the same hit/baseline/escape accounting, a different file set.
func scanText(t *testing.T, g gate) ([]hit, map[string]*parsed) {
	t.Helper()
	root := repoRoot(t)
	files := map[string]*parsed{}
	var hits []hit
	for _, rel := range textFilesUnder(t, root, g.scanTextRoot) {
		if g.textCorpus != nil && !g.textCorpus(rel) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		p := &parsed{rel: rel, src: src}
		files[rel] = p
		hits = append(hits, g.find(g, p)...)
	}
	stamp(hits, files)
	return hits, files
}

// scan applies a gate to the tree: a text gate walks the file tree, every other
// gate runs over the parsed Go topologies. It is the ONE entry point every
// caller uses, so no call site can accidentally drive a text gate with a Go-only
// file set (or the reverse).
func scan(t *testing.T, g gate) ([]hit, map[string]*parsed) {
	t.Helper()
	if g.scanTextRoot != "" {
		return scanText(t, g)
	}
	root := repoRoot(t)
	roots := g.scanRoots
	if len(roots) == 0 {
		roots = prodRoots
	}
	files := map[string]*parsed{}
	var hits []hit
	for _, rel := range goFilesUnder(t, root, roots, g.wantTest) {
		p := parseRepoFile(t, root, rel)
		files[rel] = p
		hits = append(hits, g.find(g, p)...)
	}
	if g.scanTests {
		second := g.secondRoots
		if len(second) == 0 {
			second = prodAndTestRoots
		}
		for _, rel := range goFilesUnder(t, root, second, true) {
			p := parseRepoFile(t, root, rel)
			files[rel] = p
			hits = append(hits, g.find(g, p)...)
		}
	}
	stamp(hits, files)
	return hits, files
}

// enforce applies the per-line escape marker and the baseline to a gate's hits,
// failing on any new violation and on any stale baseline entry.
//
// THE KEY IT COMPARES IS THE HIT'S CONTENT DIGEST (internal/sitekey), never a
// `file:line`: that is what makes a permit survive an edit above its site. Both
// halves of the ratchet are kept and neither is softened — a hit matching no
// entry is a NEW violation, an entry matching no hit is STALE — so the change is
// strictly a re-keying of the same accounting, not a relaxation.
func enforce(t *testing.T, g gate, hits []hit, files map[string]*parsed) {
	t.Helper()
	seen := map[string]bool{}
	var fresh []string
	var unkeyable []string
	for _, h := range hits {
		p := files[h.rel]
		k := h.keyOn(p)
		if k == "" {
			// A hit with no content key can neither match a permit nor be
			// reported as a stale one, so it must NOT be dropped: silence here
			// would be a violation the gate stopped seeing (PROJECT.md Rule 15).
			unkeyable = append(unkeyable, fmt.Sprintf("%s:%d  [%s]", h.rel, h.line, h.desc))
			continue
		}
		seen[k] = true
		switch classify(g, h, p) {
		case classBaselined:
			continue // known legacy site, recorded with its reason in baseline.go
		case classEscaped:
			continue // reviewed inline escape (same line, or the comment block above)
		}
		fresh = append(fresh, fmt.Sprintf("%s  (%s:%d)  [%s]", k, h.rel, h.line, h.desc))
	}
	sort.Strings(unkeyable)
	for _, u := range unkeyable {
		t.Errorf("%s: a hit carries NO content key at %s\n"+
			"  the hit is on a line with no identity (blank after whitespace is collapsed), so\n"+
			"  no permit can name it. This is a defect of the PREDICATE that reported it —\n"+
			"  report the site on a line that carries text (checklist %s).",
			g.id, u, g.checklistRow)
	}
	sort.Strings(fresh)
	for _, f := range fresh {
		t.Errorf("%s: NEW violation (checklist %s) at %s\n"+
			"  fix it, or — if it is a known legacy site — add it to the %s baseline\n"+
			"  (baseline.go, one-line reason) and keep the ratchet downward.\n"+
			"  run: %s%s ./internal/standards/",
			g.id, g.checklistRow, f, g.probe, probeRunPrefix, g.probePath())
	}
	var stale []string
	for _, b := range g.baseline {
		if !seen[b] {
			stale = append(stale, b+"\n"+staleDetail(b, files))
		}
	}
	sort.Strings(stale)
	for _, s := range stale {
		t.Errorf("%s: STALE baseline entry %s\n"+
			"  matches no violation — the site was fixed or MOVED. Delete the entry so the\n"+
			"  gate keeps ratcheting; re-key it (never hand-write) with: %s (checklist %s).",
			g.id, s, sitekey.KeyCreatedBy, g.checklistRow)
	}
}

// staleDetail explains ONE stale entry in the terms a reader needs to act: where
// the text the permit names still is, and what the gate now reports there.
//
// It exists because a content key is the site's text, so "stale" has three
// distinct causes that a bare entry string cannot tell apart (Rule 15: an error
// must say what happened):
//
//	the text is GONE from the file        → the site was edited or deleted: delete the permit
//	the text is present, ordinal differs  → an identical line was inserted/removed beside it
//	the text is present, same ordinal     → the PREDICATE changed, not the tree
//
// The reading is taken only from the files this run already scanned; an entry
// whose file is not in the scan is reported as such rather than guessed at.
func staleDetail(entry string, files map[string]*parsed) string {
	rel := sitekey.File(entry)
	if rel == "" {
		return "  (the entry is not a well-formed content key — see " + sitekey.KeyCreatedBy + ")"
	}
	p := files[rel]
	if p == nil {
		return fmt.Sprintf("  (the file %s is not in this gate's scan, so the text cannot be located here)", rel)
	}
	if line := sitekey.Locate(p.src, entry); line > 0 {
		return fmt.Sprintf("  the text this entry names is still at %s:%d (the site MOVED, or an identical\n"+
			"  line near it was inserted/removed, which renumbers the key's ordinal)", rel, line)
	}
	return fmt.Sprintf("  the text this entry names is GONE from %s — the site was repaired or deleted;\n"+
		"  delete the entry (the ratchet moving down)", rel)
}

// gateClass is how one hit is accounted for. Every hit falls in exactly one
// class, and the class determines both the failure and the report:
//
//   - classUnannotated   — a real violation, not recorded anywhere → FAILS.
//   - classBaselined     — a known legacy site, permitted line-by-line → passes.
//   - classEscaped       — a true positive the team decided to KEEP, annotated in
//     the code with the gate's marker. passes, but stays visible in
//     the data report (TestStandardGateDataReport counts it), so
//     a self-inflicted deliberate violation is never invisible.
//
// The separation matters because the two live in different places: a baseline
// entry records "the linter was told to be quiet about this line", an escape
// records "this line is correct and here is why". Only the second survives
// reading the code.
type gateClass int

const (
	classUnannotated gateClass = iota
	classBaselined
	classEscaped
)

// escapeFor returns the inline marker that applies to one file: a gate may give
// the fixture topology its own marker (escapeTest), and falls back to the
// general one for every other file.
func (g gate) escapeFor(rel string) string {
	if g.escapeTest != "" && strings.HasSuffix(rel, testFileSuffix) {
		return g.escapeTest
	}
	return g.escape
}

// classify buckets one hit for its gate. The baseline half compares the hit's
// CONTENT key (see keyOf): an entry names the site's text, not its line, so a
// permit survives an edit above the site.
func classify(g gate, h hit, p *parsed) gateClass {
	k := h.keyOn(p)
	if k != "" {
		for _, b := range g.baseline {
			if b == k {
				return classBaselined
			}
		}
	}
	if marker := g.escapeFor(p.rel); marker != "" && markerOn(linesAbove(p.src, h.line), marker) {
		return classEscaped
	}
	return classUnannotated
}

// lineHasMarker reports whether the 1-based line of src carries marker.
func lineHasMarker(src []byte, line int, marker string) bool {
	return markerOn([]string{lineOf(src, line)}, marker)
}

// lineOf returns the 1-based line of src ("" when out of range).
func lineOf(src []byte, line int) string {
	lines := strings.Split(string(src), "\n")
	if line-1 < 0 || line-1 >= len(lines) {
		return ""
	}
	return lines[line-1]
}

// linesAbove returns the 1-based line of src plus the contiguous comment block
// directly above it. An escape marker may sit on the offending line OR at the
// end of the comment block that documents it — a `panic(panicErr)` that
// re-panics is a statement, not a func line, so the marker has nowhere to go on
// the offending line itself. Continuity is strict: the first non-comment line
// upward ends the window, so a marker cannot float at the top of a file and
// silence everything below it.
func linesAbove(src []byte, line int) []string {
	lines := strings.Split(string(src), "\n")
	if line-1 < 0 || line-1 >= len(lines) {
		return nil
	}
	out := []string{lines[line-1]}
	for i := line - 2; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "/*") {
			break
		}
		out = append(out, trimmed)
	}
	return out
}

// markerOn reports whether any candidate line carries marker.
func markerOn(candidates []string, marker string) bool {
	for _, c := range candidates {
		if strings.Contains(c, marker) {
			return true
		}
	}
	return false
}

// countHits is the reporting helper used by the data tests.
func countHits(hits []hit) int { return len(hits) }

// classCount is one gate's hit accounting, as rendered by the data report.
type classCount struct {
	total     int
	baselined int
	escaped   int
	unannot   int
}

// classifyAll buckets every hit of one scan.
func classifyAll(g gate, hits []hit, files map[string]*parsed) classCount {
	var c classCount
	c.total = len(hits)
	for _, h := range hits {
		switch classify(g, h, files[h.rel]) {
		case classBaselined:
			c.baselined++
		case classEscaped:
			c.escaped++
		default:
			c.unannot++
		}
	}
	return c
}

// classCountLine renders one gate's accounting in the ONE shape every surface
// prints it (TestStandardGates, TestStandardGateDataReport and the exhaustive
// arm), so a reader can never compare two lines that count different things.
func classCountLine(g gate, c classCount) string {
	return fmt.Sprintf("%s: hits=%d baselined=%d escaped=%d NEW=%d", g.id, c.total, c.baselined, c.escaped, c.unannot)
}

// envReaderUnexcusedFailure is the verdict an exhaustive arm asks of a REAL-TREE
// scan: "" means the tree is clean, otherwise the one sentence explaining why it
// is not.
//
// IT JUDGES THE CLASSIFIER'S VERDICT, NEVER THE RAW HIT COUNT, and that is the
// whole point. A raw count cannot tell a violation from a PERMITTED site: the
// escape marker (envReaderEscape) and the baseline both exist to keep a hit in
// the scan while excusing it — they live in the code and in baseline.go, and
// they are already VISIBLE in the counts. An arm asserting `len(hits) != 0`
// therefore makes the permitted site unreleasable: the escape is honoured by
// enforce() (so TestStandardGates is green) while the exhaustive arm stays red
// forever, and the only two exits left are deleting a constant the rule says to
// keep, or writing a baseline entry — the ratchet-up this gate exists to
// prevent. Measured before this function existed:
//
//	hits=1 baselined=0 escaped=1 NEW=0  →  FAIL
//
// It is ONE definition rather than a condition copied into each arm because the
// arms and the planted proof must agree: a proof that passes against a predicate
// the real tree is not judged by proves nothing.
func envReaderUnexcusedFailure(g gate, c classCount) string {
	if c.unannot == 0 {
		return ""
	}
	return fmt.Sprintf("%s: the production tree carries %d env hook(s) with no reader and no "+
		"reviewed exemption (%s) — a hook with no consumer reads as a working control surface; "+
		"wire it, delete it, or escape it with %q and a reason (%s)",
		g.id, c.unannot, classCountLine(g, c), envReaderEscape, classUnannotatedHint)
}

// classUnannotatedHint is the one-line reminder an unexcused hit's verdict ends
// with, so the failure names the two legitimate exits instead of only the defect.
const classUnannotatedHint = "it is neither baselined nor escaped, so nothing records it"

// escapedHits lists the escaped sites of one scan, so the report can name the
// deliberate violations instead of hiding them in a count.
func escapedHits(g gate, hits []hit, files map[string]*parsed) []string {
	var out []string
	for _, h := range hits {
		if classify(g, h, files[h.rel]) == classEscaped {
			out = append(out, fmt.Sprintf("%s  (%s:%d)  [%s]", h.keyOn(files[h.rel]), h.rel, h.line, h.desc))
		}
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// gate 1 — fmt.Errorf must wrap with %w, not %v   (checklist row 2.2)
// ---------------------------------------------------------------------------

// errorWrapEscape marks a site that deliberately keeps the wrapped error out of
// the chain.
const errorWrapEscape = "// no-wrap:"

var percentV = regexp.MustCompile(`%v`)

// errorWrapFind reports `fmt.Errorf` calls that render a wrapped error with %v.
// Only fmt.Errorf is considered: a %v in a log line or fmt.Sprintf is string
// formatting, not a broken error chain, and flagging those would be noise.
//
// THE FORMAT STRING IS RESOLVED, NOT REQUIRED TO BE A LITERAL. A named constant
// (`fmt.Errorf(someFormat, err)`) is the shape this predicate used to skip in
// silence — see gate1_fmtformat.go for why that was a Rule-15 failure inside the
// gate and how the AST-only resolution reaches it. A site whose format string
// cannot be resolved statically is RECORDED through noteUndecidableFormat and
// reported by the data report; it is not a hit, because the verbs it holds are
// not knowable, and it is never dropped.
func errorWrapFind(g gate, p *parsed) []hit {
	var hits []hit
	ast.Inspect(p.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isFmtErrorf(call) || len(call.Args) < 2 {
			return true
		}
		line := p.fset.Position(call.Pos()).Line
		fmtStr, ok := formatText(p, call.Args[0], 0)
		if !ok {
			noteUndecidableFormat(p, line, call.Args[0])
			return true
		}
		if !percentV.MatchString(fmtStr) {
			return true
		}
		for _, a := range call.Args[1:] {
			if !mentionsError(a) {
				continue
			}
			hits = append(hits, hit{rel: p.rel, line: p.fset.Position(call.Pos()).Line,
				desc: "fmt.Errorf renders the wrapped error with %v — errors.Is/As lose the chain; use %w"})
			break
		}
		return true
	})
	return hits
}

// isFmtErrorf matches the `fmt.Errorf` selector call.
func isFmtErrorf(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Errorf" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "fmt"
}

// mentionsError is a deliberately conservative shape check: an identifier named
// err-prefixed/-suffixed, or a selector/call ending in Err/Unwrap. Keeping it
// conservative is what holds this gate's false-positive count at zero for
// non-error values formatted with %v.
func mentionsError(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == "err" || strings.HasSuffix(v.Name, "Err")
	case *ast.SelectorExpr:
		return strings.HasSuffix(v.Sel.Name, "Err")
	case *ast.CallExpr:
		if f, ok := v.Fun.(*ast.SelectorExpr); ok {
			return f.Sel.Name == "Err" || f.Sel.Name == "Unwrap"
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// gate 2 — tests must not leak process environment   (checklist row 5.2)
// ---------------------------------------------------------------------------

// testEnvEscape marks an individually reviewed os.Setenv site.
const testEnvEscape = "// env-leak-ok:"

// testEnvFind reports os.Setenv/os.Unsetenv in test files. t.Setenv restores the
// previous value automatically; os.Setenv leaks process state into sibling tests
// (the classic order-dependent failure).
func testEnvFind(g gate, p *parsed) []hit {
	hits := findPkgCalls(p, "os", map[string]bool{"Setenv": true, "Unsetenv": true},
		"os.Setenv/os.Unsetenv in a test leaks process state — use t.Setenv")
	return append(hits, testEnvRestoreShapeFind(g, p)...)
}

// testEnvRestoreShapePrefix leads the description of a lossy hand-rolled
// save/restore hit. It is a named constant so the probe tests select the shape
// hits by identity instead of by matching prose.
const testEnvRestoreShapePrefix = "os.Getenv cannot restore absence: "

// testEnvRestoreShapeFind reports the LOSSY hand-rolled save/restore inside one
// function: a variable captured with `os.Getenv(key)` and later written back
// into the same key with `os.Setenv(key, thatVar)`.
//
// Why the shape is a defect even when the line carries an escape reason:
// os.Getenv renders "unset" and "set to the empty string" IDENTICALLY, so a
// variable that was ABSENT before the flip comes back SET TO "". The value is
// typically a t.TempDir() path the framework has already removed, so a sibling
// test reading the variable gets a path that no longer exists — the
// environment is polluted, not restored. os.LookupEnv cannot make that mistake,
// which is why the reviewed form is testutil.SnapshotEnv.
//
// The scope is deliberately narrow (the gates' precision discipline — a noisy
// gate gets switched off):
//   - only the WRITE of a non-literal VALUE is considered. A constant value
//     (a table-driven case) has nothing to restore, and a `t.Setenv` is a
//     different, already-correct tool handled by its own rule.
//   - only a variable captured WITHIN THE SAME function counts, so a value
//     passed in as a parameter is never mistaken for a leaked capture.
//   - a site that consults os.LookupEnv AT ALL for that key is doing the
//     presence-preserving dance and is not reported: the predicate flags the
//     lossy form, never the hand-rolled-but-correct one.
//
// Non-vacuity: the tree held three such sites on the commit this landed
// (including one that omitted a variable's restore entirely, leaking a scratch
// path); all three now restore through testutil.SnapshotEnv, so the shape is
// reported rather than silently permitted.
func testEnvRestoreShapeFind(g gate, p *parsed) []hit {
	var out []hit
	ast.Inspect(p.file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		captured := map[string]string{} // env key -> variable it was read into
		lookedUp := map[string]bool{}   // env key consulted via os.LookupEnv
		type write struct {
			key  string
			expr ast.Expr
			pos  token.Pos
		}
		var writes []write
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			switch node := inner.(type) {
			case *ast.AssignStmt:
				for i, rhs := range node.Rhs {
					key, ok := osEnvCallKey(rhs, "Getenv")
					if !ok || i >= len(node.Lhs) {
						continue
					}
					if id, ok := node.Lhs[i].(*ast.Ident); ok {
						captured[key] = id.Name
					}
				}
			case *ast.CallExpr:
				if key, ok := osEnvCallKey(node, "LookupEnv"); ok {
					lookedUp[key] = true
				}
				if key, ok := osEnvCallKey(node, "Setenv"); ok && len(node.Args) == 2 {
					if isStringLiteral(node.Args[1]) {
						break // constant per call: nothing to restore
					}
					writes = append(writes, write{key: key, expr: node.Args[1], pos: node.Pos()})
				}
			}
			return true
		})
		for _, w := range writes {
			id, ok := w.expr.(*ast.Ident)
			if !ok || captured[w.key] != id.Name || lookedUp[w.key] {
				continue
			}
			out = append(out, hit{
				rel:  p.rel,
				line: p.fset.Position(w.pos).Line,
				desc: testEnvRestoreShapePrefix + fmt.Sprintf("os.Getenv(%s) captured into %s and written back — os.Getenv cannot "+
					"see absence, so an UNSET %s returns set to \"\"; capture with os.LookupEnv "+
					"(internal/testutil.SnapshotEnv)", w.key, id.Name, w.key),
			})
		}
		return true
	})
	return out
}

// isStringLiteral reports whether e is a quoted string constant.
func isStringLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

// osEnvCallKey reports whether e is `os.<fn>("<literal key>", ...)` and returns
// the unquoted key. The key must be a string literal: a computed key cannot be
// matched across two call sites without dataflow analysis, and a guess would be
// a false positive.
func osEnvCallKey(e ast.Expr, fn string) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != fn {
		return "", false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || ident.Name != "os" {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	key, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return key, true
}

// findPkgCalls reports every `pkg.Fn(...)` call for the given package alias.
func findPkgCalls(p *parsed, pkg string, fns map[string]bool, desc string) []hit {
	var hits []hit
	ast.Inspect(p.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !fns[sel.Sel.Name] {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != pkg {
			return true
		}
		hits = append(hits, hit{rel: p.rel, line: p.fset.Position(call.Pos()).Line, desc: desc})
		return true
	})
	return hits
}

// ---------------------------------------------------------------------------
// gate 3 — no else after a terminating return   (checklist row 4.3)
// ---------------------------------------------------------------------------

// elseAfterReturnEscape marks a reviewed `} else {`.
const elseAfterReturnEscape = "// else-ok:"

// elseAfterReturnFind reports an `if` statement with an else branch whose if-body
// ends in a terminating statement (return/continue/break). The guard-clause
// reading is strictly simpler: the else branch can be de-indented. Purely
// syntactic, so there is no judgement call and no false positive on style.
func elseAfterReturnFind(g gate, p *parsed) []hit {
	var hits []hit
	ast.Inspect(p.file, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || ifs.Else == nil || ifs.Body == nil || len(ifs.Body.List) == 0 {
			return true
		}
		if !terminates(ifs.Body.List[len(ifs.Body.List)-1]) {
			return true
		}
		hits = append(hits, hit{rel: p.rel, line: p.fset.Position(ifs.Else.Pos()).Line,
			desc: "else after a terminating if-body — drop the else and de-indent (early return)"})
		return true
	})
	return hits
}

// terminates reports whether a statement ends control flow in its block.
func terminates(s ast.Stmt) bool {
	switch v := s.(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		// BranchStmt covers break/continue/goto/fallthrough.
		_ = v
		return true
	case *ast.BlockStmt:
		if len(v.List) == 0 {
			return false
		}
		return terminates(v.List[len(v.List)-1])
	}
	return false
}

// ---------------------------------------------------------------------------
// gate 4 — no panic outside main/init/Must*   (checklist 2.7)
// ---------------------------------------------------------------------------

// panicOutsideInitEscape marks a reviewed in-band panic.
const panicOutsideInitEscape = "// panic-ok:"

// panicFunNames are the function names allowed to panic: the Must* constructor
// convention (Google Go Style best-practices) where a panic is part of the API.
var panicFunNames = map[string]bool{"init": true, "main": true}

// panicFind reports panic(...) calls anywhere other than an init function, a
// main function, or a function whose name starts with Must.
//
// Deliberately conservative in two directions, because a gate that fires on the
// wrong thing gets switched off:
//   - every enclosing FuncDecl is checked, so a closure inside init is still init;
//   - a function whose name merely CONTAINS "Need"/"required" is NOT exempt —
//     only the explicit Must* convention is.
func panicFind(g gate, p *parsed) []hit {
	var hits []hit
	for _, decl := range p.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name == nil {
			continue
		}
		if panicAllowedFunc(fn.Name.Name) {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok || ident.Name != "panic" {
				return true
			}
			// The hit is reported at the enclosing FUNC line, not the call line:
			// the escape marker has to be able to sit in the function's doc
			// comment, and a niladic any-panic func line cannot carry it inline
			// (see linesAbove).
			hits = append(hits, hit{rel: p.rel, line: p.fset.Position(fn.Pos()).Line,
				desc: "panic outside init/main/Must* in " + fn.Name.Name + " — return the error instead (checklist 2.7)"})
			return true
		})
	}
	return hits
}

// panicAllowedFunc reports whether a function name may panic.
func panicAllowedFunc(name string) bool {
	return panicFunNames[name] || strings.HasPrefix(name, "Must")
}

// ---------------------------------------------------------------------------
// gate 5 — no is/get/handle prefix on lowerCamel accessors   (checklist 1.4 /
// Google Go Style decisions#getters)
// ---------------------------------------------------------------------------

// accessorPrefixEscape marks a reviewed prefix-named method.
const accessorPrefixEscape = "// name-ok:"

// accessorPrefixes are the niladic-name prefixes Google Go Style decisions
// §getters forbids on accessors. `get`/`is`/`handle` are the three whose removal
// is unambiguous; the rule is scoped to methods because a niladic PACKAGE
// function named Get/Handle is a command/tool name (`gopi tool get`), not an
// accessor, and the checklist's own tool surface uses that shape. Scoping is
// what holds the false-positive count at zero.
var accessorPrefixes = []string{"get", "is", "handle"}

// accessorPrefixFind reports `func (r T) getFoo() X` / `isFoo` / `handleFoo`
// methods: a niladic method taking no arguments whose name is
// <prefix>+<UppercaseRest>.
func accessorPrefixFind(g gate, p *parsed) []hit {
	var hits []hit
	for _, decl := range p.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Type == nil || fn.Type.Params == nil {
			continue
		}
		if len(fn.Type.Params.List) != 0 {
			continue // takes arguments: a command, not an accessor
		}
		if reason := accessorPrefixViolation(fn.Name.Name); reason != "" {
			hits = append(hits, hit{rel: p.rel, line: p.fset.Position(fn.Name.Pos()).Line, desc: reason})
		}
	}
	return hits
}

// accessorPrefixViolation returns the reason the method name violates the rule,
// or "" when the name is acceptable.
func accessorPrefixViolation(name string) string {
	for _, prefix := range accessorPrefixes {
		if !strings.HasPrefix(name, prefix) || len(name) <= len(prefix) {
			continue
		}
		rest := []rune(name[len(prefix):])
		if !unicode.IsUpper(rest[0]) {
			continue // islowerCamel: "getter-style" name is not a prefix ("island", "handled")
		}
		return name + " — drop the " + prefix + " prefix (accessor) or take an argument (command)"
	}
	return ""
}
