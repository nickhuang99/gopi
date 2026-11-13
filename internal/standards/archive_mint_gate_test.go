package standardgate

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// archive_mint_gate_test.go — the red proof for the archive-mint gate, plus the
// non-vacuity floor that keeps the real-tree scan from reading nothing.
//
// THE RED PROOF IS THE PRE-FIX LINE, VERBATIM. The gate was written because
// scripts/post-merge-cleanup.sh minted an archive tag with no reason, so the
// proof feeds the gate the exact two lines that did it — not a paraphrase, not a
// simplification. A gate proved against a synthetic shape it invented is a gate
// proved against nothing; the failure this one exists to catch has a spelling,
// and the spelling is below.
//
// The SECOND half of the proof is the half that gets forgotten: the same file
// must be CLEAN once the funnel is in place, and a reader must never be reported.
// safe-merge.sh's exemption probe and d2-disposition.sh's two `-l` probes read
// this very namespace — a gate that reported those would be switched off within a
// day, and the whole point is that it stays on.
func TestArchiveMintGateRedProof(t *testing.T) {
	g := gateByProbe(t, "TestArchiveMintGateRedProof")

	// The PRE-FIX shape, copied out of scripts/post-merge-cleanup.sh before the
	// funnel: a local variable holding the minted name, then the create form.
	preFix := `TAG_NAME="${TAG_PREFIX}/${BRANCH}-$(date +%Y%m%d)"
git tag -f "$TAG_NAME" "$BRANCH" >/dev/null 2>&1`
	hits := archiveMintFind(g, &parsed{rel: "scripts/probe.sh", src: []byte(preFix)})
	if len(hits) != 1 {
		t.Fatalf("pre-fix shape: expected exactly 1 hit, got %d (%+v) — the gate does not "+
			"report the very line this rule was written from", len(hits), hits)
	}
	if hits[0].line != 2 {
		t.Fatalf("pre-fix shape: hit reported on line %d, want 2 (the create form)", hits[0].line)
	}

	// The POST-FIX shape — the funnel. It must be clean: this is the green arm the
	// regression guard promises a future editor.
	funnelled := `if "${GOPI_BIN}" task archive "$BRANCH" --reason "$DEFAULT_ARCHIVE_REASON" --detail "$ARCH_DETAIL" --override-completed; then
    echo "   🏷  已归档 $BRANCH"
fi`
	if got := archiveMintFind(g, &parsed{rel: "scripts/probe.sh", src: []byte(funnelled)}); len(got) != 0 {
		t.Fatalf("post-fix shape must be CLEAN, got %+v — the gate would fail the fix it exists for", got)
	}

	// READERS must never be reported. Each line below is a real form measured in
	// this tree (safe-merge.sh, d2-disposition.sh, gopi-stats.sh).
	for _, reader := range []string{
		`if git tag -l "archive/$BRANCH" | grep -q .; then`,
		`done < <(git tag -l "${ARCHIVE_TAG_PREFIX}*")`,
		`for t in $(git tag -l "${ARCHIVE_TAG_PREFIX}*${flat}*"); do`,
		`echo "tags archive/*:   $(git tag -l 'archive/*' | cnt)"`,
		`echo "tags total:       $(git tag | cnt)"`,
		`if git tag --contains "$TIP" | grep -q .; then`,
	} {
		if got := archiveMintFind(g, &parsed{rel: "scripts/probe.sh", src: []byte(reader)}); len(got) != 0 {
			t.Errorf("reader form reported as a mint (the gate would be switched off): %q → %+v", reader, got)
		}
	}

	// A full-line COMMENT is prose, so a script may quote the shape it replaced —
	// including this gate's own subject. Without this arm the funnel's explanatory
	// comment (which names the old form) would itself be a violation.
	commented := "#    TAG_NAME=\"${TAG_PREFIX}/${BRANCH}-$(date +%Y%m%d)\""
	if got := archiveMintFind(g, &parsed{rel: "scripts/probe.sh", src: []byte(commented)}); len(got) != 0 {
		t.Fatalf("a commented-out mint was reported: %+v — the escape hatch is broken", got)
	}

	// The reviewed escape suppresses a real hit, and the gate does not merely
	// adopt every create form: a mint inside a command substitution counts (a
	// shell mints there as readily as in a statement).
	subst := `tag=$(git tag -f "archive/$b-$(date +%Y%m%d)" "$b")`
	if got := archiveMintFind(g, &parsed{rel: "scripts/probe.sh", src: []byte(subst)}); len(got) != 1 {
		t.Fatalf("command-substitution mint: expected 1 hit, got %+v", got)
	}

	// Escape detection is the framework's, and it reads the reported line.
	escaped := `git tag -f "archive/$b" "$b"   # archive-mint-ok: fixture quotes the pre-fix shape`
	ehits := archiveMintFind(g, &parsed{rel: "scripts/probe.sh", src: []byte(escaped)})
	if len(ehits) != 1 {
		t.Fatalf("escaped shape: expected 1 hit before escaping, got %+v", ehits)
	}
	if !lineHasMarker([]byte(escaped), ehits[0].line, archiveMintEscape) {
		t.Fatal("escape marker on the offending line was not detected — the escape hatch is broken")
	}
}

// TestArchiveMintGateReportsTheInstructionCarrier is the arm that makes this gate
// cover the path that actually minted the ten tags.
//
// THE SCRIPT CARRIER WAS THE WRONG SUSPECT, and the measurement is what settled
// it: post-merge-cleanup.sh mints only when `AHEAD > 0`, and `AHEAD` is exactly
// `git rev-list --count master..<branch>`, while the script only ever cleans
// branches ALREADY merged into master — so every branch it touches has AHEAD=0 and
// the arm is DEAD (run master's own script against a fixture repo: all three
// merged branches print `ahead=0`, zero tags minted). The ten tags carry
// `-20261012`, which is PROJECT.md Rule 9 step 2's prescribed spelling
// (`git tag archive/<name>-<date>`) — an INSTRUCTION gopi ships into every agent's
// context, plus two Go string literals that render the same bare mint.
//
// So the carriers below are the reachable ones, and each arm is the shape this
// tree actually carried before the funnel:
func TestArchiveMintGateReportsTheInstructionCarrier(t *testing.T) {
	g := gateByProbe(t, "TestArchiveMintGateRedProof")

	// CARRIER 1 — the rule file. Rule 9's prescription, verbatim as it read.
	rule := "2. **败者分支归档清理**：`git tag archive/<name>-<date>` + `git branch -D <name>`"
	if got := archiveMintFind(g, &parsed{rel: archiveMintInstructionRel, src: []byte(rule)}); len(got) != 1 {
		t.Fatalf("GOPI.md carrier: Rule 9's prescription must be reported, got %+v", got)
	}

	// CARRIER 2 — a Go STRING LITERAL, the form a briefing renders into a child's
	// prompt. Both spellings below are verbatim from this tree before the funnel.
	for _, lit := range []string{
		"This is a VISIBILITY hint: merge it, archive it (git tag archive/<name> + branch -D), ",
		"archive it (`git tag archive/x x`) or delete it by hand",
	} {
		p := parseGoLiteralFile(t, lit)
		if got := archiveMintFind(g, p); len(got) != 1 {
			t.Errorf("Go-literal carrier must be reported, got %+v for %q", got, lit)
		}
	}

	// A Go COMMENT is a reviewer's note, never an instruction — this tree is full
	// of comments documenting the incident, and reporting them would be the reason
	// the gate gets switched off.
	if got := archiveMintFind(g, &parsed{rel: "internal/tools/probe.go", src: []byte(
		"// path: `git tag archive/<branch> <branch>` typed at a shell.\n")}); len(got) != 0 {
		t.Fatalf("a Go comment was reported: %+v — comments document, they do not instruct", got)
	}

	// HISTORY IS OUT OF SCOPE, deliberately: devdoc/ and tasks/ must stay able to
	// quote the pre-fix command, or the record of this defect could not print its
	// own subject.
	for _, rel := range []string{
		"devdoc/archive-mint-outside-writer.md",
		"tasks/01a12dff-report.md",
		"internal/standards/archive_mint_gate_test.go",
	} {
		if got := archiveMintFind(g, &parsed{rel: rel, src: []byte(rule)}); len(got) != 0 {
			t.Errorf("out-of-scope carrier %s was reported: %+v — history must be able to quote the shape", rel, got)
		}
	}
}

// archiveMintFindLiterals is GONE with the AST split it served: it read Go files
// through `parsed.file`, which a text gate's traversal never populates, so it was
// unreachable in the real scan. archiveMintFind's header records the measurement.
//
// parseGoLiteralFile still exists so the literal CARRIER can be probed through the
// same text path the production scan uses — the file's `src`, not its AST.
func parseGoLiteralFile(t *testing.T, lit string) *parsed {
	t.Helper()
	src := "package p\n\nvar s = " + strconv.Quote(lit) + "\n"
	return &parsed{rel: "internal/tools/probe.go", src: []byte(src)}
}

// TestArchiveMintGateScansTheRealScripts is the non-vacuity check: the gate's
// root exists and holds at least the floor of files. A scan root that resolved to
// nothing — a renamed directory, a filtered walk — would print a clean green
// while reading no script at all, which is the failure mode a green gate is
// least able to show.
func TestArchiveMintGateScansTheRealScripts(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, archiveMintScanRoot)
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("read %s: %v — the gate's scan root must exist", archiveMintScanRoot, err)
	}
	scripts := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sh") {
			scripts++
		}
	}
	if scripts < archiveMintMinScripts {
		t.Fatalf("%s holds %d shell scripts (floor %d) — the scan is not looking at the real tree",
			archiveMintScanRoot, scripts, archiveMintMinScripts)
	}
}
