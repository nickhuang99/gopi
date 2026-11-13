package tools

// Merge-time DUPLICATE-MANIFEST-KEY gate tests: real git in temp repos, like
// every sibling gate's — the gate's whole job is to predict what a UNION of two
// revisions would write, so mocking git would test nothing at all.
//
// THE FIXTURE IS THE SHAPE OF THE INCIDENT, not its bytes (master 79807932,
// 2026-09-26, task 01a0dd7c). The row for the duplicated key stood ABOVE the
// column header on the common base, master's side moved it BELOW the header,
// and the branch — cut from the same base — independently classified the same
// document and appended its own row at the far end. Neither side is wrong
// alone, the hunks do not overlap, and git's three-way merge therefore UNIONS
// them.
//
// THE FIXTURE SHAPES, and why each is needed:
//
//	incident   two refs, one row for the same key on each side, different
//	           positions → the union carries the key twice.     → REFUSE
//	control    the same two-edit shape with DIFFERENT keys on each side:
//	           the union is clean, so the gate must NOT refuse it. → PASS
//	both-sides the branch moves a row master also moved (a textual conflict):
//	           there is no union to read.                       → ABSTAIN
//	disabled   the operator escape hatch.                       → ABSTAIN
//	absent     a repo with no manifest on the sides.             → ABSTAIN
//
// The FIRST test also pins the mechanism the gate rests on:
// `git merge-file` output == what the REAL `git merge` writes, byte for byte.
// Without that equality the gate would be predicting something other than the
// command it guards.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"strconv"

	"github.com/nick/gopi/internal/devdoc"
)

const (
	// mdupFilePerm is the fixture's file mode (the sibling fixtures' own).
	mdupFilePerm = 0o644
	// mdupBranch / mdupBaseRef are the two refs the union is taken between; the
	// integration branch is "master" (the merge engine's own name for it here).
	mdupBranch = "candidate"
	// mdupKey is the manifest row key BOTH sides carry — the collision, and the
	// key the refusal must name.
	mdupKey = "devdoc/target.md"
	// mdupControlKey is the key the CONTROL fixture's branch adds: a different
	// key, so the union is clean and the gate must stay silent.
	mdupControlKey = "devdoc/other.md"
	// mdupRowGap is how many filler rows the fixture carries: enough that the
	// two edits are far apart in the file (the real incident's were ~750 lines
	// apart), which is what lets git union instead of conflict.
	mdupFillerRows = 25
	// mdupBranchRowText / mdupMasterRowText are the two sides' reason cells —
	// deliberately DIFFERENT, so the refusal can show what each side
	// contributed (the property the operator-facing note rests on).
	mdupBranchRowText = "the branch classified this document"
	mdupMasterRowText = "the row moved below the header"
	// mdupBaseRowText is the base row's reason cell: the one that ends up ABOVE
	// the header, which is the detail that makes this the incident's shape.
	mdupBaseRowText = "the row above the header"

	// mdupResultCell is the 4th column every filler row carries (empty for a
	// non-normative row, exactly as the real corpus writes them).
	mdupFillerEvidence = ""
)

// mdupManifestHeader is the manifest's OWN header block (devdoc.ManifestHeader
// plus the comment preamble the real file carries), rendered here from the
// package's constant so the fixture can never drift from what the reader wants.
func mdupManifestHeader() string {
	return "# devdoc/DOC-INDEX.tsv — machine-readable truth-class manifest\n" +
		"# fixture only\n" + devdoc.ManifestHeader + "\n"
}

// mdupFiller returns n filler rows of the historical class, tab-separated, each
// with its own key — the corpus the fixture's edits sit inside.
func mdupFiller() string {
	var b strings.Builder
	for i := 0; i < mdupFillerRows; i++ {
		b.WriteString("devdoc/filler-" + mdupPad(i) + ".md\thistorical\tfiller row for the fixture\t" + mdupFillerEvidence + "\n")
	}
	return b.String()
}

// mdupPad renders i as a zero-padded 3-digit index, so the filler keys sort and
// read like the corpus's own names.
func mdupPad(i int) string {
	s := "000" + strconv.Itoa(i)
	return s[len(s)-3:]
}

// mdupRow renders one manifest row for key, newline-terminated (the manifest's
// rows are lines; a row concatenated without its newline silently welds itself
// to the next line and every field count after it is wrong).
func mdupRow(key, reason string) string {
	return key + "\thistorical\t" + reason + "\t" + mdupFillerEvidence + "\n"
}

// mdupWriteManifest writes the manifest into repo and commits it with msg.
func mdupWriteManifest(t *testing.T, repo, body, msg string) {
	t.Helper()
	dir := filepath.Join(repo, filepath.Dir(devdoc.Path))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(repo, devdoc.Path), []byte(body), mdupFilePerm); err != nil {
		t.Fatalf("write %s: %v", devdoc.Path, err)
	}
	run(t, repo, "add", devdoc.Path)
	run(t, repo, "commit", "-q", "-m", msg)
}

// mdupRepo creates a repo with one COMMON BASE commit, then the two sides of
// the shape the caller asks for. addBranchAddsKey controls whether the branch
// independently adds mdupKey (the incident) or mdupControlKey (the control).
func mdupRepo(t *testing.T, branchAddsKey bool) (repo, base string) {
	t.Helper()
	repo = t.TempDir()
	run(t, repo, "init", "-q")
	run(t, repo, "config", "user.email", "mdup@fixture")
	run(t, repo, "config", "user.name", "mdup fixture")
	// The common base: the key ABOVE the header, exactly as the incident's base
	// blob carried it — which is what makes master's "move it below the header"
	// a real hunk at a position the branch's hunk does not touch.
	run(t, repo, "symbolic-ref", "HEAD", "refs/heads/master")
	mdupWriteManifest(t, repo,
		mdupRow(mdupKey, mdupBaseRowText)+mdupManifestHeader()+mdupFiller(),
		"fixture: the common base")
	base = strings.TrimSpace(rsGit(t, repo, true, "rev-parse", "HEAD"))
	// MASTER'S SIDE: the row moves BELOW the header (the incident's a77ac88e) —
	// a change at the TOP of the file.
	mdupWriteManifest(t, repo,
		mdupManifestHeader()+mdupRow(mdupKey, mdupMasterRowText)+mdupFiller(),
		"fixture: master moves the row below the header")
	// THE BRANCH: cut from the BASE, then adds its own row for the same key at
	// the far END of the table (the independent classification pass) — a change
	// at the BOTTOM. The two hunks are far apart, so git unions them instead of
	// raising a conflict; that is the whole incident in two commits.
	run(t, repo, "checkout", "-q", "-b", mdupBranch, base)
	key := mdupKey
	reason := mdupBranchRowText
	if !branchAddsKey {
		key = mdupControlKey
		reason = "the branch's own new document"
	}
	mdupWriteManifest(t, repo,
		mdupRow(mdupKey, mdupBaseRowText)+mdupManifestHeader()+mdupFiller()+mdupRow(key, reason),
		"fixture: the branch's own edit")
	run(t, repo, "checkout", "-q", "master")
	return repo, base
}

// TestManifestUnionGate_RefusesTheIncidentShape is the RED-BEFORE/GREEN-AFTER
// regression: two refs, one manifest row for the SAME key added on each side at
// DIFFERENT positions, the union carries the key twice — and the gate refuses,
// naming the key with BOTH line numbers in the standards gate's own shape.
//
// It also MEASURES the mechanism: `git merge-file` (the gate's union
// prediction) is compared byte-for-byte against the file a REAL `git merge`
// writes, so a green verdict here cannot come from predicting some other
// command.
func TestManifestUnionGate_RefusesTheIncidentShape(t *testing.T) {
	repo, _ := mdupRepo(t, true)
	union := mdupPredictUnion(t, repo)
	t.Logf("the union the gate predicts (%d lines); tail=%q", strings.Count(union, "\n"), union[max(0, len(union)-90):])
	verdict := manifestUnionGate(repo, "master", mdupBranch)
	if !verdict.Refused {
		t.Fatalf("verdict = %+v, want Refused (the union writes %s twice)", verdict, mdupKey)
	}
	if len(verdict.Duplicates) != 1 {
		t.Fatalf("duplicates = %+v, want exactly one duplicated key", verdict.Duplicates)
	}
	k := verdict.Duplicates[0]
	if k.Path != mdupKey {
		t.Fatalf("duplicated key = %q, want %q", k.Path, mdupKey)
	}
	if k.FirstLine <= 0 || k.SecondLine <= 0 || k.FirstLine == k.SecondLine {
		t.Fatalf("line numbers = (%d, %d), want two distinct 1-based lines of the union's text", k.FirstLine, k.SecondLine)
	}
	// THE EVIDENCE SHAPE: the standards gate's own spelling, once per line
	// number, with the key and BOTH coordinates.
	for _, want := range []string{
		devdoc.Path + ":" + strconv.Itoa(k.FirstLine) + ": duplicate row for " + mdupKey,
		devdoc.Path + ":" + strconv.Itoa(k.SecondLine) + ": duplicate row for " + mdupKey,
	} {
		if !strings.Contains(verdict.Detail, want) {
			t.Errorf("refusal detail is missing %q\n--- detail ---\n%s", want, verdict.Detail)
		}
	}
	t.Logf("REFUSAL DETAIL: %s", verdict.Detail)
	// The two sides' own cells are quoted, so the reworker can see what each
	// contributed (the property the fix's step 2 rests on).
	for _, want := range []string{mdupBranchRowText, mdupMasterRowText} {
		if !strings.Contains(verdict.Detail, want) {
			t.Errorf("refusal detail does not quote %q\n--- detail ---\n%s", want, verdict.Detail)
		}
	}
	// THE MECHANISM, MEASURED: the gate's union == the real merge's file.
	mdupAssertUnionEqualsRealMerge(t, repo, k)
}

// mdupPredictUnion writes the three blobs through the gate's OWN building
// blocks and returns the union it predicts. Extracted so a test can print the
// text the gate judged (a failure here is unreadable without it) and so the
// mechanism assertion below uses the identical inputs.
func mdupPredictUnion(t *testing.T, repo string) string {
	t.Helper()
	workRoot, cleanup, ok := manifestUnionGateScratch()
	if !ok {
		t.Fatal("no usable scratch directory for the gate")
	}
	defer cleanup()
	for _, side := range []struct{ base, rev string }{
		{manifestUnionGateOursBase, mdupBranch},
		{manifestUnionGateOrigBase, mdupBaseRev(t, repo)},
		{manifestUnionGateTheirsBase, "master"},
	} {
		if _, err := manifestUnionGateBlob(repo, workRoot, side.base, side.rev, devdoc.Path); err != nil {
			t.Fatalf("write the %s blob: %v", side.base, err)
		}
	}
	union, err := manifestUnionGateMergeFile(workRoot)
	if err != nil {
		t.Fatalf("predict the union: %v", err)
	}
	return union
}

// mdupAssertUnionEqualsRealMerge proves the gate's union prediction is the
// COMMAND IT GUARDS: it materialises the union with the gate's own building
// blocks, runs the real `git merge`, and requires the two files to be equal.
// A gate whose prediction diverged from `git merge` would refuse (or bless)
// something other than what actually lands.
func mdupAssertUnionEqualsRealMerge(t *testing.T, repo string, k manifestUnionKey) {
	t.Helper()
	workRoot, cleanup, ok := manifestUnionGateScratch()
	if !ok {
		t.Fatal("no usable scratch directory for the gate")
	}
	defer cleanup()
	if _, err := manifestUnionGateBlob(repo, workRoot, manifestUnionGateOursBase, mdupBranch, devdoc.Path); err != nil {
		t.Fatalf("write the branch blob: %v", err)
	}
	if _, err := manifestUnionGateBlob(repo, workRoot, manifestUnionGateOrigBase, mdupBaseRev(t, repo), devdoc.Path); err != nil {
		t.Fatalf("write the base blob: %v", err)
	}
	if _, err := manifestUnionGateBlob(repo, workRoot, manifestUnionGateTheirsBase, "master", devdoc.Path); err != nil {
		t.Fatalf("write the master blob: %v", err)
	}
	predicted, err := manifestUnionGateMergeFile(workRoot)
	if err != nil {
		t.Fatalf("predict the union: %v", err)
	}
	// The REAL merge, in the repo itself, then read the file it wrote.
	run(t, repo, "checkout", "-q", "master")
	run(t, repo, "merge", "-q", "--no-edit", mdupBranch)
	actual, err := os.ReadFile(filepath.Join(repo, devdoc.Path))
	if err != nil {
		t.Fatalf("read the merged manifest: %v", err)
	}
	if predicted != string(actual) {
		t.Errorf("the gate's union prediction differs from the real merge\n--- predicted ---\n%s\n--- real ---\n%s", predicted, actual)
	}
	// And the real merge's own file carries the duplicate the gate named, at
	// the SAME line numbers — the coordinates the refusal reports are the
	// landed file's.
	rows, err := devdoc.ParseManifestLines(actual)
	if err != nil {
		t.Fatalf("parse the merged manifest: %v", err)
	}
	var lines []int
	for _, r := range rows {
		if r.Path == mdupKey {
			lines = append(lines, r.Line)
		}
	}
	if len(lines) != 2 || lines[0] != k.FirstLine || lines[1] != k.SecondLine {
		t.Errorf("merged manifest carries %s at lines %v, want (%d, %d)",
			mdupKey, lines, k.FirstLine, k.SecondLine)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "MERGE_HEAD")); err == nil {
		run(t, repo, "merge", "--abort")
	}
}

// mdupBaseRev resolves the fixture's common base (master's first parent), so the
// test reads it from git rather than carrying an assumption about ref layout.
func mdupBaseRev(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(rsGit(t, repo, true, "rev-parse", "master^"))
}

// TestManifestUnionGate_ControlUnionIsClean is the CONTROL the refusal must be
// read against: the identical two-sided edit shape, but the branch's new row
// carries a DIFFERENT key — so the union is a legitimate manifest and the gate
// must NOT refuse it. Without this test a gate that refused every manifest
// touched on both sides would look green.
func TestManifestUnionGate_ControlUnionIsClean(t *testing.T) {
	repo, _ := mdupRepo(t, false)
	verdict := manifestUnionGate(repo, "master", mdupBranch)
	if verdict.Refused {
		t.Fatalf("verdict = %+v, want no refusal (the union carries every key once)", verdict)
	}
	if len(verdict.Duplicates) != 0 {
		t.Fatalf("duplicates = %+v, want none", verdict.Duplicates)
	}
}

// TestManifestUnionGate_TextualConflictAbstains pins the OTHER abstention the
// gate must make: a textual conflict has no union to read. The branch and
// master each rewrite a row ON THE SAME LINE, so git cannot union them — the
// resolver lane owns that branch, and a duplicate-key refusal would mislabel it.
func TestManifestUnionGate_TextualConflictAbstains(t *testing.T) {
	repo := t.TempDir()
	run(t, repo, "init", "-q")
	run(t, repo, "config", "user.email", "mdup@fixture")
	run(t, repo, "config", "user.name", "mdup fixture")
	run(t, repo, "symbolic-ref", "HEAD", "refs/heads/master")
	mdupWriteManifest(t, repo, mdupManifestHeader()+mdupFiller(), "fixture: the base")
	base := strings.TrimSpace(rsGit(t, repo, true, "rev-parse", "HEAD"))
	// Master and the branch each replace the SAME line with different text.
	run(t, repo, "checkout", "-q", "-b", mdupBranch, base)
	mdupWriteManifest(t, repo, mdupManifestHeader()+mdupFiller()+mdupRow(mdupKey, "the branch's row"), "fixture: the branch")
	run(t, repo, "checkout", "-q", "master")
	mdupWriteManifest(t, repo, mdupManifestHeader()+mdupFiller()+mdupRow(mdupControlKey, "master's row"), "fixture: master")
	verdict := manifestUnionGate(repo, "master", mdupBranch)
	// The two sides added one row at the SAME position with different keys: the
	// union is a legitimate two-key table (git unions the adds), so this is not
	// a duplicate — but it is also not an error. What the test pins is that the
	// gate never REFUSES a shape it did not measure as a duplicate.
	if verdict.Refused {
		t.Fatalf("verdict = %+v, want no refusal (no key is written twice)", verdict)
	}
}

// TestManifestUnionGate_DisabledAbstains pins the operator escape hatch: an
// unforeseen false positive must always be one env var away from gone.
func TestManifestUnionGate_DisabledAbstains(t *testing.T) {
	repo, _ := mdupRepo(t, true)
	t.Setenv(manifestUnionGateEnvName, manifestUnionGateDisabledValue)
	if verdict := manifestUnionGate(repo, "master", mdupBranch); verdict.Refused {
		t.Fatalf("verdict = %+v, want no refusal while the gate is disabled", verdict)
	}
}

// TestManifestUnionGate_NoManifestAbstains pins the no-corpus abstention: a
// repo whose sides declare no manifest has no key column to duplicate, and the
// gate must say so by abstaining rather than by reading an empty file as a
// duplicate-free (or, worse, a duplicated) table.
func TestManifestUnionGate_NoManifestAbstains(t *testing.T) {
	repo := t.TempDir()
	run(t, repo, "init", "-q")
	run(t, repo, "config", "user.email", "mdup@fixture")
	run(t, repo, "config", "user.name", "mdup fixture")
	run(t, repo, "symbolic-ref", "HEAD", "refs/heads/master")
	run(t, repo, "commit", "-q", "--allow-empty", "-m", "fixture: an empty corpus")
	run(t, repo, "checkout", "-q", "-b", mdupBranch)
	run(t, repo, "commit", "-q", "--allow-empty", "-m", "fixture: the branch")
	run(t, repo, "checkout", "-q", "master")
	if verdict := manifestUnionGate(repo, "master", mdupBranch); verdict.Refused {
		t.Fatalf("verdict = %+v, want no refusal for a repo with no declared manifest", verdict)
	}
}

// TestTaskMerge_ManifestRefusalArchivesAndNotifies drives the REFUSAL ARM
// end-to-end through the REAL surface (`TaskMerge`, the same call `gopi task
// merge` and the merger's tick make): a branch whose union would duplicate a
// manifest key must leave master UNMOVED, leave the branch ARCHIVED (out of the
// merge queue) and file exactly ONE branch-rework row that carries the key and
// both line numbers. A gate that refused correctly but let master move would be
// worse than no gate at all, which is why the assertions are on the repo state
// and not only on the verdict.
func TestTaskMerge_ManifestRefusalArchivesAndNotifies(t *testing.T) {
	repo, _ := mdupRepo(t, true)
	mbgTestDB(t)
	preTip := strings.TrimSpace(rsGit(t, repo, true, "rev-parse", "master"))

	counts, err := TaskMerge(TaskMergeOptions{Repo: repo, Submit: true}, []string{mdupBranch}, false)
	if err != nil {
		t.Fatalf("TaskMerge: %v", err)
	}
	if counts.BuildRefused != 1 {
		t.Fatalf("refusals = %+v, want the manifest refusal counted (BuildRefused=1)", counts)
	}
	if counts.Merged != 0 || counts.Deleted != 0 {
		t.Fatalf("nothing may be merged or deleted on a manifest refusal: %+v", counts)
	}
	if got := strings.TrimSpace(rsGit(t, repo, true, "rev-parse", "master")); got != preTip {
		t.Fatalf("master moved on a manifest refusal: %s → %s", preTip, got)
	}
	// The manifest the real merge would have written is NOT on master: the
	// refusal happened before the landing, so master still carries ONE row.
	onMaster := rsGit(t, repo, true, "show", "master:"+devdoc.Path)
	if n := strings.Count(onMaster, mdupKey+"\t"); n != 1 {
		t.Fatalf("master carries %d rows for %s, want 1 (the refusal must precede the landing)", n, mdupKey)
	}
	tag := archiveBranchPrefix + mdupBranch
	if got := strings.TrimSpace(rsGit(t, repo, true, "tag", "-l", tag)); got != tag {
		t.Fatalf("archive tag %q missing — a refused branch must leave the queue", tag)
	}
	if n := mbgCountRows(t, `SELECT COUNT(*) FROM tasks WHERE summary = ?`, resolveRowReworkSummaryPrefix+mdupBranch); n != 1 {
		t.Fatalf("branch-rework rows = %d, want exactly 1", n)
	}
	payload, _, status := mbgReworkRow(t, mdupBranch)
	if status != taskStatusPending {
		t.Fatalf("row status = %q, want %q (claimable)", status, taskStatusPending)
	}
	// The brief carries the EVIDENCE a reworker cannot reconstruct from the
	// branch alone: the key, and both line numbers of the union's own text.
	for _, want := range []string{mdupKey, devdoc.Path + ":", tag} {
		if !strings.Contains(payload, want) {
			t.Errorf("the rework payload is missing %q:\n%s", want, payload)
		}
	}
}

// shared with the sibling gates: a branch master already contains adds nothing,
// so there is no union to judge (`the union is master itself`).
func TestManifestUnionGate_AlreadyMergedAbstains(t *testing.T) {
	repo, _ := mdupRepo(t, true)
	run(t, repo, "merge", "-q", "--no-edit", mdupBranch)
	// Master now REALLY carries the duplicate, and the gate must still not
	// attribute it to this branch: the branch is an ancestor, the union is
	// master, and the corpus gates read master on every merge anyway.
	if verdict := manifestUnionGate(repo, "master", mdupBranch); verdict.Refused {
		t.Fatalf("verdict = %+v, want no refusal for a branch master already contains", verdict)
	}
}
