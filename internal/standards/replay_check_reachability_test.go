package standardgate

// THE REGRESSION THIS FILE GUARDS (measured 2026-10-27, task
// 01a17886-f940). scripts/replay-check.sh judges doc anchors that pin a commit,
// a tag or a preimage revision by reading the GROUND's own object store. A
// clone-channel ground fetches `refs/heads/*` only, so every pinned object it
// does not carry is legitimately absent — and the script read that absence as
// CONTENT DRIFT: ten rows reported STALE and the run exited 1, while the same
// script in the main repository measured nine of them green.
//
// Two distinct defects were folded into that one symptom, and each has an arm
// below:
//
//  1. ABSENCE WAS REPORTED AS DRIFT. `git cat-file -e <rev>` answering 128 is a
//     statement about the STORE, not about the doc. The fix introduces the third
//     outcome — UNREACHABLE: printed with git's own words, counted, and NOT
//     counted as stale.
//  2. ABSENCE WAS SILENTLY MEASURED AS ZERO. `git show <absent>:<path> | wc -l`
//     is 0, so C12a printed `OK recorded=0 importers / 0 entries measured=0
//     importers / 0 entries` about a revision it never read — a green produced
//     by two failed reads. Same for C7a (`before=0`), C10b and C15. A check that
//     cannot read its subject must not report a number (Rule 15).
//
// The fixture is an EMPTY repository: it holds the docs and the script and no
// objects at all, which is the ground's condition reduced to its essence. It is
// built with `git init` ONLY — no commit is created, so no object store exists
// to be warmed, and the test never asserts anything about a real history.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// replayCheckDocs are the three documents the script audits (its DOC_ACCEPTANCE
// / DOC_DRIVER / DOC_ASSEMBLY constants). They are copied into the fixture so the
// run is about REACHABILITY, never about the docs' content.
var replayCheckDocs = []string{
	"devdoc/chat-film-replay-acceptance.md",
	"devdoc/chat-film-replay-driver.md",
	"devdoc/film-assembly-and-verification.md",
}

const (
	// The script under test.
	replayCheckScript  = "scripts/replay-check.sh"
	replayCheckFixture = "replay-check-reachability"

	// The verdict markers the script prints. Named here (Rule 1) and also
	// asserted on, so a rename in the script fails THIS test instead of quietly
	// making it pass by matching nothing.
	markUnreachable = "UNREACHABLE"
	markStale       = "STALE"
	markOK          = "OK "

	// The drift control. C11a judges the chat flag count the doc publishes on the
	// row naming `chat-ideal-geometry, film` — a row measurable WITHOUT any pinned
	// object (its figure comes from the help page the binary prints and the census
	// script reads, both files on disk). That is what makes it the one anchor a
	// store-with-nothing can still judge, and therefore the right control: if the
	// absence fix ever swallowed it, the gate would have lost its last red.
	driftDoc      = "devdoc/chat-film-replay-acceptance.md"
	driftAnchor   = "| chat-ideal-geometry, film,"
	driftBogusCnt = "| 99 |"

	// The two artifacts C11a needs. The binary is SYMLINKED into the fixture, not
	// copied: it is ~68 MB and the fixture only needs it to answer `-h`.
	//
	// Its name inside the fixture is the build contract's own path tail, spelled
	// from testutil's ONE definition (BinDirName/BinFileName) rather than
	// re-typed, because replay-check.sh's default BIN is that relative path and
	// the script is run from the fixture root.
	driftBin = testutil.BinDirName + "/" + testutil.BinFileName
	// WHICH build the fixture drives is the suite's own tested binary, asked for
	// through testutil.GopiBinOptional below — the same ONE precedence every e2e
	// case asks — never a path this test assembled for itself. A build named by
	// hand would execute whichever tree's `bin/gopi` happened to be reachable
	// (Rule 6a), which is the stale-binary defect the resolver exists to remove.
	driftCensus = "scripts/g5-chat-web-help-surface.sh"
)

// replayCheckFixtureDir builds an EMPTY git repository carrying the docs and the
// script, and returns its path. Nothing is committed: the point of the fixture is
// that the store cannot answer a single object question.
func replayCheckFixtureDir(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	dir := testutil.TestScratchDir(t, replayCheckFixture)

	mustCopy := func(rel string) {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("fixture: read %s: %v", rel, err)
		}
		dst := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("fixture: mkdir %s: %v", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, body, 0o755); err != nil {
			t.Fatalf("fixture: write %s: %v", dst, err)
		}
	}
	mustCopy(replayCheckScript)
	// The THREE documents the script audits, named rather than globbed: the
	// corpus is thousands of pages and copying it would make this arm about disk
	// I/O instead of reachability.
	for _, d := range replayCheckDocs {
		mustCopy(d)
	}
	// `git init`, and NOTHING else: an empty store is the fixture's whole point.
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("fixture: git init: %v: %s", err, out)
	}
	return dir
}

// linkDriftArtifacts makes C11a executable inside the fixture: it needs the
// binary (`BIN`, symlinked so a 68 MB copy is not paid per run) and the census
// script. It returns false — with the reason PRINTED, never swallowed (Rule 15) —
// when nothing resolved a tested binary, which is the ordinary state of a fresh
// checkout: the drift control then has no red left to demonstrate, and the caller
// says so instead of passing on a vacuum.
func linkDriftArtifacts(t *testing.T, dir string) bool {
	t.Helper()
	root := repoRoot(t)
	// The binary the fixture drives is the SUITE's tested build, asked for
	// through the ONE resolver (testutil.GopiBinOptional); the fixture's own
	// path for it is composed from the same definitions the script's default
	// BIN spells, because the script runs with cmd.Dir = dir.
	bin, ok, why := testutil.GopiBinOptional(t)
	if !ok {
		t.Logf("drift control NOT demonstrated: no tested binary resolved (%s) — "+
			"build it (CGO_ENABLED=0 go build -o %s ./cmd/agent/) and re-run to exercise it", why, driftBin)
		return false
	}
	if info, err := os.Stat(bin); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		t.Logf("drift control NOT demonstrated: %s is not an executable binary — "+
			"build it (CGO_ENABLED=0 go build -o %s ./cmd/agent/) and re-run to exercise it", bin, driftBin)
		return false
	}
	if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(driftBin)), 0o755); err != nil {
		t.Fatalf("fixture: mkdir %s: %v", filepath.Dir(driftBin), err)
	}
	if err := os.Symlink(bin, filepath.Join(dir, driftBin)); err != nil {
		t.Fatalf("fixture: link %s: %v", driftBin, err)
	}
	dst := filepath.Join(dir, driftCensus)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("fixture: mkdir %s: %v", filepath.Dir(dst), err)
	}
	if err := os.Symlink(filepath.Join(root, driftCensus), dst); err != nil {
		t.Fatalf("fixture: link %s: %v", dst, err)
	}
	return true
}

// runReplayCheck executes the fixture's script and returns stdout+stderr, the
// exit status, and the lines split out.
func runReplayCheck(t *testing.T, dir string) (string, int, []string) {
	t.Helper()
	cmd := exec.Command("bash", replayCheckScript)
	cmd.Dir = dir
	// The caller's git state is STRIPPED, not blanked: an inherited GIT_DIR would
	// aim the fixture's reads at THIS repository's store (which holds the very
	// objects the fixture lacks, so the arm would prove nothing), and setting it
	// to the EMPTY string is not "unset" — git reads `GIT_DIR=` as the path ''
	// and answers `fatal: not a git repository: ''` (measured while writing this
	// arm, 2026-10-27). So the variables are FILTERED OUT of the environment.
	stripped := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_DIR=") ||
			strings.HasPrefix(kv, "GIT_WORK_TREE=") ||
			strings.HasPrefix(kv, "GIT_INDEX_FILE=") ||
			strings.HasPrefix(kv, "GIT_OBJECT_DIRECTORY=") ||
			strings.HasPrefix(kv, "GIT_ALTERNATE_OBJECT_DIRECTORIES=") ||
			strings.HasPrefix(kv, "HOME=") {
			continue
		}
		stripped = append(stripped, kv)
	}
	cmd.Env = append(stripped, "HOME="+dir)
	raw, err := cmd.CombinedOutput()
	rc := 0
	if err != nil {
		exitErr, isExit := err.(*exec.ExitError)
		if !isExit {
			t.Fatalf("replay-check did not run: %v\n%s", err, raw)
		}
		rc = exitErr.ExitCode()
	}
	out := string(raw)
	return out, rc, strings.Split(out, "\n")
}

// countMark counts the lines that begin with a verdict marker.
func countMark(lines []string, mark string) int {
	n := 0
	for _, ln := range lines {
		if strings.HasPrefix(ln, mark) {
			n++
		}
	}
	return n
}

// TestReplayCheckReportsAnAbsentObjectAsUnreachableNotStale is the red proof for
// defect 1: in a store that holds nothing, no anchor may be called STALE, every
// unreadable anchor-group must be reported UNREACHABLE with git's own words, and
// the run must exit 0 (an unmeasurable anchor is not a failed one).
func TestReplayCheckReportsAnAbsentObjectAsUnreachableNotStale(t *testing.T) {
	dir := replayCheckFixtureDir(t)
	out, rc, lines := runReplayCheck(t, dir)

	if got := countMark(lines, markStale); got != 0 {
		t.Errorf("an empty store produced %d STALE row(s); absence must not be reported as drift.\n"+
			"Every STALE line follows:\n%s", got, staleLines(lines))
	}
	if got := countMark(lines, markUnreachable); got == 0 {
		t.Fatalf("no %s row at all in a store that holds no objects — the check is not reporting what it "+
			"could not read (a silent skip).\nOutput:\n%s", markUnreachable, out)
	}
	// git's OWN words must ride along: a paraphrase is a claim about git, and the
	// reader needs the line that says which object was missing. `git init` leaves
	// git naming HEAD (and every spelling after it) as "Not a valid object name".
	if !strings.Contains(out, "Not a valid object name") &&
		!strings.Contains(out, "unknown revision") {
		t.Errorf("no UNREACHABLE row quotes git's own refusal — a reader cannot tell which object was "+
			"missing.\nOutput:\n%s", out)
	}
	if rc != 0 {
		t.Errorf("exit=%d in a store that holds no objects: an unmeasurable anchor is not a failed one.\n"+
			"Output:\n%s", rc, out)
	}
	// The summary must never let "0 stale" stand in for "0 measured".
	if !strings.Contains(out, "UNREACHABLE") || !strings.Contains(out, "NOT\n") &&
		!strings.Contains(out, "were NOT") {
		t.Errorf("the summary does not state that the unreachable anchors were not judged.\nOutput:\n%s", out)
	}
}

// TestReplayCheckStillFailsOnARealDrift is the green-to-red control: the same
// empty store, one perturbed anchor, and the run MUST be STALE and exit 1 — so
// the absence fix did not turn the gate into a check that cannot fail.
//
// The perturbed anchor is deliberately one whose value is measurable with NO
// pinned object (C11a counts a flag row on the page the binary prints): if the
// script ever reported this row as unreachable, the drift signal itself would be
// gone.
func TestReplayCheckStillFailsOnARealDrift(t *testing.T) {
	dir := replayCheckFixtureDir(t)
	if !linkDriftArtifacts(t, dir) {
		t.Skip("no built binary in this tree — the drift control has no measurable anchor to break")
	}
	doc := filepath.Join(dir, driftDoc)
	body, err := os.ReadFile(doc)
	if err != nil {
		t.Fatalf("read %s: %v", doc, err)
	}
	// The flag-count cell of the `chat` row: its LAST `| <n> |` is what C11a
	// reads (`grep -oE "$RE_INT" | tail -1`), so the row is rewritten wholesale
	// with a bogus trailing count. Anchoring on the row (not on "24 flags", which
	// the doc does not write) is what keeps this arm from silently asserting
	// nothing.
	orig := strings.Split(string(body), "\n")
	hit := false
	for i, ln := range orig {
		if strings.Contains(ln, driftAnchor) {
			orig[i] = ln + " " + driftBogusCnt
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("the drift row %q is not in %s — this arm would assert nothing", driftAnchor, doc)
	}
	if err := os.WriteFile(doc, []byte(strings.Join(orig, "\n")), 0o644); err != nil {
		t.Fatalf("write %s: %v", doc, err)
	}

	out, rc, lines := runReplayCheck(t, dir)
	if got := countMark(lines, markStale); got == 0 {
		t.Fatalf("a doc publishing a bogus chat flag count measured 0 STALE rows — the gate cannot fail.\n"+
			"Output:\n%s", out)
	}
	if rc != 1 {
		t.Errorf("exit=%d for a genuinely drifted anchor; want 1.\nOutput:\n%s", rc, out)
	}
}

// staleLines returns just the STALE rows, so a failure message is readable
// instead of being the whole run.
func staleLines(lines []string) string {
	var b strings.Builder
	for _, ln := range lines {
		if strings.HasPrefix(ln, markStale) {
			b.WriteString(ln)
			b.WriteString("\n")
		}
	}
	return b.String()
}
