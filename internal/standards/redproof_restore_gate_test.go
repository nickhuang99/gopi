package standardgate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// ---------------------------------------------------------------------------
// red-proof restore criterion — the three halves every gate here needs
// (red proof, read floor, real tree) PLUS the live demonstration this gate
// exists for: the failure mode reproduced on a real git index, and the
// hardened criterion catching it.
//
// The live half is the point. A gate whose subject is "a criterion is blind to
// index state" must not be proven by prose: it has to run the recipe against a
// real index and show the fragile criterion passing while the file still
// carries the mutation (PROJECT.md Rule 31 — a surface you did not execute is not
// delivered).
// ---------------------------------------------------------------------------

// Fixture vocabulary (Rule 1: no bare literals in the test body).
const (
	redproofFixtureFile      = "prod.go"
	redproofFixtureSrc       = "func F() int { return 1 }\n"
	redproofFixtureMutated   = "func F() int { return 2 } // MUTATION\n"
	redproofFixtureUserMail  = "gate@example.invalid"
	redproofFixtureUserName  = "standardgate red proof"
	redproofProbeDocRel      = "docs/probe.md"
	redproofFixtureCommitMsg = "base"
)

// Arm (c)'s fixture vocabulary — the same rule: named, never typed inline.
const (
	// redproofIdentitySnapshot is a local copy artifact, the shape a proof taken
	// "against the snapshot saved this loop" compares against.
	redproofIdentitySnapshot = "prod.go.saved"
	// redproofFixtureSwapped is a LATER write of the same file — the measured
	// defect's 11:13:12 edit, after the reading was taken.
	redproofFixtureSwapped = "func F() int { return 3 } // LATER WRITE\n"
	// redproofIdentityBlobA/B are two 40-hex blob spellings. They are literal
	// digests only because the fixture must not claim a blob it never computed;
	// in the LIVE test every digest is read off the repository.
	redproofIdentityBlobA = "0123456789abcdef0123456789abcdef01234567"
	redproofIdentityBlobB = "fedcba9876543210fedcba9876543210fedcba98"
	// redproofIdentityCorrectSpelling is the revision-anchored spelling the arm
	// exempts, asserted PRESENT in GOPI.md so the real-tree green is calibrated
	// against a shape that actually occurs.
	redproofIdentityCorrectSpelling = "git rev-parse HEAD:"
	// redproofIdentityProbeLine is the fragile comparison the line-number
	// assertion points at — the `cmp`-plus-operator form, whose comment carries
	// the words a report uses for a verdict without spelling one (`exit 0 =
	// identical = restored`), so no literal verdict token lives in a fixture.
	redproofIdentityProbeLine = "cmp -s " + redproofFixtureFile + " " +
		redproofIdentitySnapshot + "   # exit 0 = identical = restored"
	// redproofIdentityProbeQuoted is the measured defect's SHAPE, spliced from
	// named pieces: the arm must report a reading spelled this way, and the file
	// must not read as a corpus hit against itself.
	redproofIdentityProbeQuoted = "hash=" + redproofIdentityBlobA + " saved=" +
		redproofIdentityBlobA + " same" + "=YES"
)

// redproofIdentityProbePair is the recorded-pair fixture, spliced like
// redproofIdentityProbeQuoted and for the same reason.
func redproofIdentityProbePair() string {
	return "internal/standards/generated_doc_deps_test.go " + redproofIdentityProbeQuoted + "\n"
}

// TestGateRedProofRestoreRedProof is half 1: the predicate reports the fragile
// recipe and stays silent on every corrected spelling. Both halves matter — a
// predicate loosened to "report every unit that mentions git checkout" would
// pass the positive half alone and be routed around immediately.
func TestGateRedProofRestoreRedProof(t *testing.T) {
	g := gateByProbe(t, "TestGateRedProofRestoreRedProof")
	if g.scanTextRoot == "" {
		t.Fatal("the red-proof restore gate carries no text scan root — it would walk the Go " +
			"topology only, and its subject is the prose gopi renders into a briefing")
	}
	if g.textCorpus == nil {
		t.Fatal("the red-proof restore gate carries no corpus filter — it would sweep the whole " +
			"tree, where devdoc/ and tasks/ legitimately QUOTE the fragile criterion")
	}

	// POSITIVE: the recipe as it is copied into reports — restore by index, then
	// an index-relative criterion reading "restored".
	fragile := "## Red proof\n" +
		"\n" +
		"```\n" +
		"sed -i 's/return 1/return 2/' " + redproofFixtureFile + "\n" +
		"go test ./internal/tools/ -run TestX   # FAIL observed\n" +
		"git checkout -- " + redproofFixtureFile + "\n" +
		"git diff --stat " + redproofFixtureFile + "   # empty = restored\n" +
		"```\n"
	lines := strings.Split(fragile, "\n")
	wantLine := lineNumber(lines, "git diff --stat "+redproofFixtureFile+"   # empty = restored")
	if wantLine == 0 {
		t.Fatal("probe construction error: the criterion line is not in the probe document")
	}
	hits := g.find(g, &parsed{rel: redproofProbeDocRel, src: []byte(fragile)})
	if len(hits) != 1 {
		t.Fatalf("RED PROOF FAILED: the fragile recipe produced %d hit(s) (%+v), want exactly 1 — "+
			"a predicate that stopped matching would leave this gate green forever", len(hits), hits)
	}
	if hits[0].line != wantLine {
		t.Errorf("hit names line %d, want %d — the failure must point at the CRITERION line, which "+
			"is the line a reader has to change", hits[0].line, wantLine)
	}
	if hits[0].rel != redproofProbeDocRel {
		t.Errorf("hit names %q, want %q — a hit must name the file it came from", hits[0].rel, redproofProbeDocRel)
	}
	if !strings.Contains(hits[0].desc, "git diff") {
		t.Errorf("the hit's message does not carry the correction: %q", hits[0].desc)
	}

	// The same pair written as ONE sentence (the shape docs actually use): the
	// unit must still pair them, or the gate covers only fenced recipes.
	oneSentence := "The production line was reverted (`git restore " + redproofFixtureFile +
		"`) and the worktree is clean; `git diff` printed nothing, so the restore is proven.\n"
	if got := g.find(g, &parsed{rel: redproofProbeDocRel, src: []byte(oneSentence)}); len(got) != 1 {
		t.Fatalf("RED PROOF FAILED: a single-sentence pair produced %d hit(s) (%+v), want 1 — the "+
			"failure mode is prose far more often than it is a fenced block", len(got), got)
	}

	// POSITIVE, second spelling: the operator's own proposed hardening —
	// `git diff <file>` — MEASURED to be index-relative (worktree vs index), so
	// it is reported, not exempted. A criterion that looks different and measures
	// the same thing is worse than the original.
	ownProposal := "git checkout -- " + redproofFixtureFile + "\ngit diff " + redproofFixtureFile + "\n"
	if got := g.find(g, &parsed{rel: redproofProbeDocRel, src: []byte(ownProposal)}); len(got) != 1 {
		t.Fatalf("`git diff <file>` with no revision was not reported (%+v) — it is worktree-vs-INDEX, "+
			"the exact comparison this gate exists to replace; exempting it would ship a criterion "+
			"that reads clean on an added file", got)
	}

	// NEGATIVES: the corrected criteria and the rev-explicit restores.
	negatives := []struct {
		why string
		src string
	}{
		{"restore unchanged, criterion against HEAD (explicit revision)",
			"git checkout -- " + redproofFixtureFile + "\ngit diff --stat HEAD -- " + redproofFixtureFile + "\n"},
		{"restore unchanged, criterion against HEAD through the `git diff HEAD` stat form",
			"git checkout -- " + redproofFixtureFile + "\ngit diff HEAD --stat -- " + redproofFixtureFile + "\n"},
		{"restore unchanged, index-free blob comparison",
			"git restore " + redproofFixtureFile + "\ntest \"$(git hash-object " + redproofFixtureFile +
				")\" = \"$(git rev-parse HEAD:" + redproofFixtureFile + ")\"\n"},
		{"restore seeded from an explicit revision",
			"git checkout 0123456 -- " + redproofFixtureFile + "\ngit diff --stat\n"},
		{"restore seeded from an explicit revision, long form",
			"git checkout 0123456789abcdef0123456789abcdef01234567 " + redproofFixtureFile + "\ngit diff\n"},
		{"the mirror operation (index against HEAD) is not a worktree restore",
			"git checkout -- " + redproofFixtureFile + "\ngit diff --cached --stat " + redproofFixtureFile + "\n"},
		{"a restore that reseeds from a revision does not depend on index state",
			"git restore --source=0123456 " + redproofFixtureFile + "\ngit diff --stat\n"},
		{"a `git diff` between two revisions is not index-relative",
			"git checkout -- " + redproofFixtureFile + "\ngit diff --stat 0123456 7890abc\n"},
		{"an index-relative diff with NO restore claim in the unit is a legitimate usage",
			"Scope check: `git diff --stat` lists exactly two production files, both intended.\n"},
		{"a restore claim with no criterion at all says nothing about index state",
			"The tree was restored with `git checkout -- " + redproofFixtureFile + "` and re-driven to green.\n"},
		{"a `git checkout <branch>` is not a worktree restore by path",
			"git checkout fix/some-branch\ngit diff --stat\n"},
	}
	for _, neg := range negatives {
		if got := g.find(g, &parsed{rel: redproofProbeDocRel, src: []byte(neg.src)}); len(got) != 0 {
			t.Errorf("FALSE POSITIVE on «%s»: reported %+v — a gate that fires on the CORRECTED "+
				"spellings would be routed around, and the recipe would stay fragile", neg.why, got)
		}
	}

	// The inline escape: an instruction surface that must QUOTE the fragile form
	// in order to correct it stays visible in the data report, never silent.
	escaped := fragile + "\n" + redproofRestoreEscape + " this report quotes the fragile form on purpose\n"
	if got := g.find(g, &parsed{rel: redproofProbeDocRel, src: []byte(escaped)}); len(got) != 0 {
		t.Errorf("the inline escape did not suppress the unit: %+v", got)
	}
}

// TestGateRedProofRestoreCorpusAndFloor is half 2: the corpus this gate scans is
// the INSTRUCTION SURFACE, and the scan cannot pass vacuously. Both are asserted
// against the real tree, not a fixture: the failure this pins is a walker that
// silently returns nothing (indistinguishable from a clean tree, Rule 15) and a
// corpus that drifted onto the history trees.
func TestGateRedProofRestoreCorpusAndFloor(t *testing.T) {
	g := gateByProbe(t, "TestGateRedProofRestoreRedProof")
	root := repoRoot(t)
	all := textFilesUnder(t, root, g.scanTextRoot)

	files, units := 0, 0
	for _, rel := range all {
		if !g.textCorpus(rel) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		files++
		units += len(redproofUnits(strings.Split(string(src), "\n")))
	}
	if files < redproofRestoreMinFiles {
		t.Fatalf("the scan covered %d file(s), below the %d floor — a walker that reads nothing "+
			"reports a green tree, which is the one failure mode a scan can hide", files, redproofRestoreMinFiles)
	}
	if units < redproofRestoreMinUnits {
		t.Fatalf("the scan parsed %d unit(s), below the %d floor — a unit splitter that stopped "+
			"splitting would leave the FILE count intact and this gate vacuous", units, redproofRestoreMinUnits)
	}

	// PRESENCE: the rules themselves are in the corpus (a gate that missed the
	// normative text would cover only the copies).
	for _, rel := range redproofRestoreNamedFiles {
		if !g.textCorpus(rel) {
			t.Errorf("the corpus filter excludes %s — the normative text is the first place a "+
				"fragile criterion is written, and the copies inherit it", rel)
		}
	}
	// ABSENCE: the history corpora are NOT scanned, so a record can still quote
	// the criterion it measured (and the READ of those trees stays their own
	// gates' business).
	for _, rel := range []string{"devdoc/conflict-marker-gate.md", "tasks/example-report.md"} {
		if g.textCorpus(rel) {
			t.Errorf("the corpus filter includes %s — devdoc/ and tasks/ are RECORDS; a gate that "+
				"forbids them from quoting a criterion makes the record unfalsifiable", rel)
		}
	}
}

// TestGateRedProofRestoreRealTreeIsGreen is half 3: on the tree as it ships, the
// gate reports nothing — and the run SAYS how much it read, so "green" cannot be
// a quiet empty scan.
func TestGateRedProofRestoreRealTreeIsGreen(t *testing.T) {
	g := gateByProbe(t, "TestGateRedProofRestoreRedProof")
	root := repoRoot(t)
	read, hits := 0, 0
	for _, rel := range textFilesUnder(t, root, g.scanTextRoot) {
		if !g.textCorpus(rel) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		read++
		for _, h := range g.find(g, &parsed{rel: rel, src: src}) {
			hits++
			t.Errorf("fragile red-proof restore criterion at %s:%d\n  %s", h.rel, h.line, h.desc)
		}
	}
	if read < redproofRestoreMinFiles {
		t.Fatalf("the real-tree scan read only %d file(s) — the green result below means nothing", read)
	}
	t.Logf("red-proof restore criterion: %d file(s) scanned, %d hit(s)", read, hits)
}

// TestGateRedProofRestoreLiveFailureAndHardenedCriterion is the demonstration
// the epic asked for, and it runs the REAL recipe against a REAL index — the
// fragile pair, quoted here as the gate's subject (red-proof-restore-ok: this
// comment states the failure mode the test reproduces; the commands it actually
// runs are the argv slices below, and the criterion it asserts is the
// HEAD-anchored one):
//
//	git add <file>          # the index now holds the mutation
//	mutate <file>
//	git checkout -- <file>  # exit 0, and a NO-OP: the index is written back
//	git diff --stat <file>  # EMPTY — the fragile criterion reads "restored"
//
// then it asserts the hardened criteria catch the surviving mutation, and that
// the honest restore (`git checkout HEAD -- <file>`) turns all of them empty.
// Without this half, the gate's own claim ("the criterion is blind") would be an
// assertion nobody measured.
func TestGateRedProofRestoreLiveFailureAndHardenedCriterion(t *testing.T) {
	dir := t.TempDir()
	// Rule 22 — checked as a CRITERION (device id), never as a path spelling.
	if same, err := redproofSameDevice(dir, "/"); err != nil {
		t.Fatalf("device comparison failed: %v", err)
	} else if same {
		t.Fatalf("the fixture lives on the / partition (%s) — a temporary workspace must be "+
			"provably off it (GOPI.md Rule 22)", dir)
	}
	git := func(argv ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(gitBinary, argv...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("git %s: %v\n%s", strings.Join(argv, " "), err, out)
			}
		}
		return string(out), code
	}
	writeFile := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	if out, code := git("init", "-q"); code != 0 {
		t.Fatalf("git init: exit %d\n%s", code, out)
	}
	git("config", "user.email", redproofFixtureUserMail)
	git("config", "user.name", redproofFixtureUserName)
	writeFile(redproofFixtureFile, redproofFixtureSrc)
	git("add", redproofFixtureFile)
	if out, code := git("commit", "-qm", redproofFixtureCommitMsg); code != 0 {
		t.Fatalf("git commit: exit %d\n%s", code, out)
	}

	// STEP 1 — the plant, plus the staging that turns the restore into a no-op.
	writeFile(redproofFixtureFile, redproofFixtureMutated)
	git("add", redproofFixtureFile)
	indexBlob := strings.TrimSpace(mustGit(t, dir, "rev-parse", ":"+redproofFixtureFile))
	stagedBlob := strings.TrimSpace(mustGit(t, dir, "hash-object", redproofFixtureFile))
	committedBlob := strings.TrimSpace(mustGit(t, dir, "rev-parse", "HEAD:"+redproofFixtureFile))
	if indexBlob != stagedBlob {
		t.Fatalf("the plant did not reach the index (index %s, worktree %s) — the fixture must "+
			"stage the mutation, which is what makes the recipe's restore a no-op", indexBlob, stagedBlob)
	}
	if indexBlob == committedBlob {
		t.Fatalf("the staged blob equals HEAD's (%s) — there is no mutation to demonstrate", indexBlob)
	}

	// STEP 2 — the restore the recipe prescribes.
	if out, code := git("checkout", "--", redproofFixtureFile); code != 0 {
		t.Fatalf("git checkout -- %s exited %d, want 0 — the defect is a no-op that SUCCEEDS\n%s",
			redproofFixtureFile, code, out)
	}
	body, err := os.ReadFile(filepath.Join(dir, redproofFixtureFile))
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if !strings.Contains(string(body), "MUTATION") {
		t.Fatalf("the mutation did not survive `git checkout -- %s` — this fixture no longer "+
			"demonstrates the failure mode it exists for", redproofFixtureFile)
	}

	// STEP 3 — the FRAGILE criterion says "restored" while the mutation is live.
	fragile, fragileCode := git("diff", "--stat", redproofFixtureFile)
	if strings.TrimSpace(fragile) != "" || fragileCode != 0 {
		t.Fatalf("the fragile criterion is no longer blind here: `git diff --stat %s` printed %q "+
			"(exit %d), want empty/0. The demonstration has lost its subject, so the gate below it "+
			"proves nothing.", redproofFixtureFile, fragile, fragileCode)
	}
	headBlob := strings.TrimSpace(mustGit(t, dir, "rev-parse", "HEAD:"+redproofFixtureFile))
	workBlob := strings.TrimSpace(mustGit(t, dir, "hash-object", redproofFixtureFile))
	if workBlob == headBlob {
		t.Fatalf("worktree blob equals HEAD after the no-op restore (%s) — the plant is gone", headBlob)
	}

	// STEP 4 — the HARDENED criteria catch it. The FIRST of these is asserted as
	// a POSITIVE (it must fire) and the correction it names is the HEAD-anchored
	// form — `git diff <path>` alone is index-relative, which this fixture
	// measured on 2026-10-12.
	if got, code := git("diff", "HEAD", "--", redproofFixtureFile); strings.TrimSpace(got) == "" || code != 0 {
		t.Fatalf("the hardened criterion `git diff HEAD -- %s` printed nothing (exit %d) while the "+
			"mutation is live", redproofFixtureFile, code)
	}
	if got, code := git("diff", redproofFixtureFile); strings.TrimSpace(got) != "" || code != 0 {
		t.Fatalf("`git diff %s` (no revision) printed %q (exit %d) while the index holds the plant — "+
			"this spelling is worktree-vs-index and MUST stay blind here; if it is no longer blind, "+
			"the corpus warning that names it as fragile is wrong", redproofFixtureFile, got, code)
	}

	// STEP 5 — the honest restore is seeded from a revision, and then BOTH
	// criteria (and the blob comparison) agree that the tree is restored.
	git("reset", "-q", redproofFixtureFile) // the staged plant is the other half of the trap
	if out, code := git("checkout", "HEAD", "--", redproofFixtureFile); code != 0 {
		t.Fatalf("git checkout HEAD -- %s exited %d\n%s", redproofFixtureFile, code, out)
	}
	for _, argv := range [][]string{
		{"diff", "--stat", redproofFixtureFile},
		{"diff", redproofFixtureFile},
		{"diff", "HEAD", "--", redproofFixtureFile},
	} {
		if got, code := git(argv...); strings.TrimSpace(got) != "" || code != 0 {
			t.Fatalf("after the honest restore, `git %s` still reports %q (exit %d)", strings.Join(argv, " "), got, code)
		}
	}
	if again := strings.TrimSpace(mustGit(t, dir, "hash-object", redproofFixtureFile)); again != headBlob {
		t.Fatalf("after the honest restore the worktree blob is %s, want HEAD's %s", again, headBlob)
	}
}

// TestGateRedProofRestoreIdentityRedProof is arm (c)'s red proof: a
// content-identity comparison with no revision on EITHER side is reported, and
// every revision-anchored spelling — including the recorded-verdict shape the
// measured defect used — is silent. The measured defect itself is the first
// positive, verbatim as it was printed.
func TestGateRedProofRestoreIdentityRedProof(t *testing.T) {
	g := gateByProbe(t, "TestGateRedProofRestoreRedProof")

	// The fixtures below SPELL the fragile reading in order to refuse it. The
	// marker is the arm's own escape, and it is honest in this file: every
	// fixture here is passed to the predicate DIRECTLY (as `pos.src`), so the
	// escape cannot suppress a probe — it only keeps this file's own source out
	// of the tree-wide scan, which is the same shape the other text gate's
	// header comment uses.
	//
	// red-proof-identity-ok: this file's fixtures quote the fragile shape on
	// purpose and each one is exercised as a probe against the predicate.
	positives := []struct {
		why string
		src string
	}{
		{"the measured defect, verbatim: a digest and a copy's digest, recorded as a verdict",
			redproofIdentityProbePair()},
		{"a byte comparison of two files, neither of them a revision",
			"cmp -s " + redproofFixtureFile + " " + redproofIdentitySnapshot +
				"   # exit 0 = identical = restored\n"},
		{"a shell equality of two digests, both computed from local files",
			"test \"$(sha256sum " + redproofFixtureFile + ")\" = \"$(sha256sum " +
				redproofIdentitySnapshot + ")\"\n"},
		{"the same claim with the inequality operator",
			"[ \"$(sha1sum " + redproofFixtureFile + ")\" != \"$(sha1sum " +
				redproofIdentitySnapshot + ")\" ]\n"},
	}
	for _, pos := range positives {
		got := g.find(g, &parsed{rel: redproofProbeDocRel, src: []byte(pos.src)})
		if len(got) != 1 {
			t.Fatalf("RED PROOF FAILED on «%s»: got %d hit(s) (%+v), want exactly 1 — an arm that "+
				"stopped matching leaves the second half of Rule 5 unguarded while the suite stays "+
				"green", pos.why, len(got), got)
		}
		if !strings.Contains(got[0].desc, "revision") {
			t.Errorf("the hit's message does not carry the correction: %q", got[0].desc)
		}
	}

	// The hit must name the LINE the comparison sits on — the line a reader has
	// to change — not the escaping or the fence.
	multiline := "## Red proof\n\n" +
		"```\n" +
		"cp " + redproofFixtureFile + " " + redproofIdentitySnapshot + "\n" +
		redproofIdentityProbeLine + "\n" +
		"```\n"
	lines := strings.Split(multiline, "\n")
	wantLine := lineNumber(lines, redproofIdentityProbeLine)
	if wantLine == 0 {
		t.Fatal("probe construction error: the comparison line is not in the probe document")
	}
	got := g.find(g, &parsed{rel: redproofProbeDocRel, src: []byte(multiline)})
	if len(got) != 1 || got[0].line != wantLine {
		t.Fatalf("hit at %+v, want exactly one at line %d — the failure must point at the COMPARISON "+
			"line", got, wantLine)
	}

	// NEGATIVES: every revision-anchored spelling, and every shape that merely
	// MENTIONS a digest without comparing one.
	negatives := []struct {
		why string
		src string
	}{
		{"the corrected spelling: worktree blob against the committed blob",
			"test \"$(git hash-object " + redproofFixtureFile + ")\" = \"$(git rev-parse HEAD:" +
				redproofFixtureFile + ")\"\n"},
		{"a digest compared against a revision-qualified path",
			"test \"$(sha256sum " + redproofFixtureFile + ")\" = \"$(git rev-parse 0123456:" +
				redproofFixtureFile + ")\"\n"},
		{"`git show <rev>:<path>` reads the committed blob, so one side is a revision",
			"cmp -s " + redproofFixtureFile + " <(git show 0123456:" + redproofFixtureFile + ")\n"},
		{"the verdict shape with a revision named on one side",
			redproofFixtureFile + " worktree=" + redproofIdentityBlobB + " same" + "=NO (HEAD is " +
				redproofIdentityBlobA + ")\n"},
		{"a digest MENTIONED in prose with no comparison",
			"the artifact's sha256sum is recorded in its envelope header, so a receipt is comparable\n"},
		{"a digest MENTIONED with no comparison and no verdict key",
			"sha256sum " + redproofFixtureFile + "\n"},
		{"`cmp` as a shell function or a longer identifier is not the byte comparator",
			"func fbcmp(a, b string) bool { return a == b }\n"},
		{"`cmp` in prose is not a command",
			"the comparator named cmp decides only when to stop, never what is true\n"},
		{"a digest of the committed blob, read through `git cat-file` and compared with a local digest",
			"test \"$(git cat-file -p HEAD:" + redproofFixtureFile + " | sha256sum)\" = \"$(sha256sum " +
				redproofIdentitySnapshot + ")\"\n"},
		{"both sides read through `git rev-parse`, one of them HEAD-qualified",
			"test \"$(git rev-parse HEAD:" + redproofFixtureFile + ")\" = \"$(git rev-parse " +
				redproofIdentityBlobB[:7] + ":" + redproofFixtureFile + ")\"\n"},
		{"a Go comparison whose one side is read from HEAD and the other from the worktree",
			"want := gitRevParse(t, dir, \"HEAD:\"+path)\ngot := gitHashObject(t, dir, path)\n" +
				"if got != want {\n\tt.Fatalf(\"blob mismatch\")\n}\n"},
		{"the index-relative criterion arm (a) already owns, with no identity comparison at all",
			"git diff HEAD -- " + redproofFixtureFile + "   # worktree vs the committed blob\n"},
	}
	for _, neg := range negatives {
		if got := g.find(g, &parsed{rel: redproofProbeDocRel, src: []byte(neg.src)}); len(got) != 0 {
			t.Errorf("FALSE POSITIVE on «%s»: reported %+v — a gate that fires on the corrected "+
				"spellings would be routed around", neg.why, got)
		}
	}

	// The arm's OWN escape suppresses the arm's OWN shape, and does not need the
	// other arm's marker (two subjects, two markers).
	quoted := "the fragile reading was `" + redproofIdentityProbeQuoted + "`\n\n" +
		redproofIdentityEscape + " quoted in order to correct it\n"
	if got := g.find(g, &parsed{rel: redproofProbeDocRel, src: []byte(quoted)}); len(got) != 0 {
		t.Errorf("the identity escape did not suppress arm (c): %+v", got)
	}
}

// TestGateRedProofRestoreIdentityLive is the LIVE demonstration of the second
// half, run against a REAL git repository: a copy-anchored reading keeps saying
// `same=YES` after the file is written a second time, while a revision-anchored
// reading of the SAME file turns red on its own. Without this, the arm's claim
// ("a revision anchor is what makes the reading self-invalidating") would be
// prose.
func TestGateRedProofRestoreIdentityLive(t *testing.T) {
	dir := t.TempDir()
	git := func(argv ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(gitBinary, argv...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("git %s: %v\n%s", strings.Join(argv, " "), err, out)
			}
		}
		return string(out), code
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	if out, code := git("init", "-q"); code != 0 {
		t.Fatalf("git init: exit %d\n%s", code, out)
	}
	git("config", "user.email", redproofFixtureUserMail)
	git("config", "user.name", redproofFixtureUserName)
	write(redproofFixtureFile, redproofFixtureSrc)
	git("add", redproofFixtureFile)
	if out, code := git("commit", "-qm", redproofFixtureCommitMsg); code != 0 {
		t.Fatalf("git commit: exit %d\n%s", code, out)
	}

	// STEP 1 — the loop's own artifacts: the file, a snapshot copy of it, and the
	// digest pair the report recorded. A copy-anchored reading is taken here.
	snapshot := filepath.Join(dir, redproofIdentitySnapshot)
	if err := os.WriteFile(snapshot, []byte(redproofFixtureSrc), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	snapshotHash := strings.TrimSpace(mustGit(t, dir, "hash-object", redproofIdentitySnapshot))
	provenHash := strings.TrimSpace(mustGit(t, dir, "hash-object", redproofFixtureFile))
	if snapshotHash != provenHash {
		t.Fatalf("the snapshot (%s) and the file (%s) differ before the proof — the fixture must "+
			"start from a TRUE reading, or the blindness below is trivial", snapshotHash, provenHash)
	}
	t.Logf("copy-anchored reading before the later write: file=%s saved=%s same=YES",
		provenHash, snapshotHash)
	// The real-tree arm over this very reading: it is the fragile shape, and the
	// gate says so.
	reading := redproofFixtureFile + " hash=" + provenHash + " saved=" + snapshotHash + " same" + "=YES\n"
	if got := redproofIdentityFind(redproofRestoreGate(),
		&parsed{rel: redproofProbeDocRel, src: []byte(reading)}); len(got) != 1 {
		t.Fatalf("the LIVE reading is not reported by arm (c) (%+v) — the predicate and the "+
			"demonstration disagree about the shape", got)
	}

	// STEP 2 — a LATER write of the same file (the measured defect's 11:13:12).
	write(redproofFixtureFile, redproofFixtureSwapped)

	// STEP 3 — the copy-anchored reading is BLIND: it still says same=YES, and
	// says nothing about the file having moved. This is asserted, not described.
	stillSaved := strings.TrimSpace(mustGit(t, dir, "hash-object", redproofIdentitySnapshot))
	if stillSaved != snapshotHash {
		t.Fatalf("the saved copy changed (%s → %s) — the fixture no longer models a reading taken "+
			"before the later write", snapshotHash, stillSaved)
	}
	// A fresh re-reading of the file NOW shows a different digest, so the old
	// reading is about a state that no longer exists — and nothing in it says so.
	afterHash := strings.TrimSpace(mustGit(t, dir, "hash-object", redproofFixtureFile))
	if afterHash == provenHash {
		t.Fatalf("the later write did not change the file (%s) — the fixture has nothing to be "+
			"blind about", afterHash)
	}
	t.Logf("copy-anchored reading after the later write: hash=%s saved=%s same=YES (blind: the "+
		"file is now %s)", provenHash, snapshotHash, afterHash)

	// STEP 4 — the revision-anchored reading of the SAME file turns RED by
	// itself, because HEAD's blob is a thing a later write cannot change.
	headBlob := strings.TrimSpace(mustGit(t, dir, "rev-parse", "HEAD:"+redproofFixtureFile))
	if headBlob != provenHash {
		t.Fatalf("HEAD's blob is %s, want the pre-write blob %s — the anchor must name the state "+
			"the proof was taken against", headBlob, provenHash)
	}
	if afterHash == headBlob {
		t.Fatalf("the revision-anchored reading did NOT turn red (worktree %s == HEAD %s) — the "+
			"whole contrast this test exists for is gone", afterHash, headBlob)
	}
	t.Logf("revision-anchored reading after the later write: worktree=%s HEAD=%s same=NO", afterHash, headBlob)
}

// TestGateRedProofRestoreIdentityRealTreeIsGreen asserts the new arm over the
// tree as it ships, with its own read figure: the arm must read a real corpus
// and report nothing, and the tree's own corrected spelling — `git hash-object`
// against `git rev-parse HEAD:<path>` in PROJECT.md Rule 5 — must be the thing it
// reads rather than the thing it reports.
func TestGateRedProofRestoreIdentityRealTreeIsGreen(t *testing.T) {
	g := gateByProbe(t, "TestGateRedProofRestoreRedProof")
	root := repoRoot(t)
	read, hits := 0, 0
	for _, rel := range textFilesUnder(t, root, g.scanTextRoot) {
		if !g.textCorpus(rel) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		read++
		for _, h := range redproofIdentityFind(g, &parsed{rel: rel, src: src}) {
			hits++
			t.Errorf("unanchored content-identity comparison at %s:%d\n  %s", h.rel, h.line, h.desc)
		}
	}
	if read < redproofRestoreMinFiles {
		t.Fatalf("the identity arm read only %d file(s) — a green result here would mean nothing", read)
	}
	// PRESENCE: the corrected spelling this arm exempts really is in the corpus,
	// so "0 hits" is not the consequence of the shape being absent everywhere.
	rule, err := os.ReadFile(filepath.Join(root, redproofRestoreNamedFiles[0]))
	if err != nil {
		t.Fatalf("read %s: %v", redproofRestoreNamedFiles[0], err)
	}
	if !strings.Contains(string(rule), redproofIdentityCorrectSpelling) {
		t.Fatalf("%s no longer carries %q — the revision-anchored spelling this arm is calibrated "+
			"against has moved, and the arm's exemption is untested on the real tree",
			redproofRestoreNamedFiles[0], redproofIdentityCorrectSpelling)
	}
	t.Logf("content-identity anchor: %d file(s) scanned, %d hit(s)", read, hits)
}

// mustGit runs git in dir and fails the test on a non-zero exit.
func mustGit(t *testing.T, dir string, argv ...string) string {
	t.Helper()
	cmd := exec.Command(gitBinary, argv...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(argv, " "), err, out)
	}
	return string(out)
}

// redproofSameDevice reports whether two paths sit on the same filesystem, by
// DEVICE ID — the criterion PROJECT.md Rule 22 states, never a path spelling.
func redproofSameDevice(a, b string) (bool, error) {
	da, err := redproofDeviceID(a)
	if err != nil {
		return false, err
	}
	db, err := redproofDeviceID(b)
	if err != nil {
		return false, err
	}
	return da == db, nil
}

// redproofDeviceID reads one path's device id off the stat result.
func redproofDeviceID(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, nil
	}
	return uint64(st.Dev), nil
}
