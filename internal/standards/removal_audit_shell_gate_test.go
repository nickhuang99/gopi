package standardgate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// removal_audit_shell_gate_test.go — the red proof and the non-vacuity floor for
// the census's TEXT half (removal_audit_shell_gate.go).
//
// THE RED PROOF IS THE PRE-FIX LINE, VERBATIM. The half was written because
// scripts/post-merge-cleanup.sh ran a raw branch deletion and wrote nothing to
// the audit sink, so the proof is fed the exact lines that did it — the flag
// assigned to a variable in one branch of an if, then the delete that reads the
// variable — and NOT a paraphrase. A gate proved against a synthetic shape it
// invented is a gate proved against nothing. The second pre-fix spelling is
// PROJECT.md Rule 9's own prose, which taught the same verb to every agent, and the
// third is the remote deletion (`git push --delete` / the `:refs/heads/<b>`
// refspec) that the same census could not see one host away.
//
// The GREEN arm is the FUNNEL: `gopi task archive <branch> --reason …` removes
// the ref through the one writer, so the delete is recorded by the writer's own
// reclaim leg. A gate that reported the funnel would fail the fix it exists for.
//
// The third half is the half that gets forgotten: the READERS. This tree is full
// of legitimate `git branch` invocations that do not remove anything — a merge
// scan, a list, a `--show-current`, a branch CREATION — and a `git push --force`
// that MOVES a remote ref. A gate that fired on those would be switched off
// within a day, and the whole point is that it stays on.
func TestRemovalAuditGateShellHalfRedProof(t *testing.T) {
	g := gateByProbe(t, "TestRemovalAuditGateShellHalfRedProof")

	find := func(rel, src string) []hit {
		t.Helper()
		return removalShellFind(g, &parsed{rel: rel, src: []byte(src)})
	}
	requireHitAt := func(rel, src, what string, wantLine int) {
		t.Helper()
		hits := find(rel, src)
		if len(hits) != 1 {
			t.Fatalf("%s: expected exactly 1 hit, got %d (%+v) — the half does not report the "+
				"shape this rule was written from", what, len(hits), hits)
		}
		if hits[0].line != wantLine {
			t.Fatalf("%s: hit reported on line %d, want %d", what, hits[0].line, wantLine)
		}
	}

	// (1) scripts/post-merge-cleanup.sh BEFORE the funnel, copied out of the file
	// as it shipped: the flag is put in a VARIABLE and the delete reads it.
	preFixScript := `    # 2. 删除分支 — 已合分支 -d 安全；--force 升级 -D
    if [ "$FORCE" -eq 1 ]; then
        DEL_FLAG="-D"
    else
        DEL_FLAG="-d"
    fi
    if git branch $DEL_FLAG "$BRANCH" >/dev/null 2>&1; then`
	requireHitAt("scripts/post-merge-cleanup.sh", preFixScript, "pre-fix script shape", 7)

	// (2) PROJECT.md Rule 9 step 2's prose, verbatim — the instruction carrier. The
	// backticks around the command are Markdown quoting, and the gate reads
	// through them.
	preFixProse := "2. **败者分支归档清理**：`gopi task archive <branch> --reason hand-mistake --detail \"<为什么>\"` +\n" +
		"   `git branch -D <name>` + `git worktree remove <path>`"
	requireHitAt(archiveMintInstructionRel, preFixProse, "pre-fix GOPI.md prose", 2)

	// (3) the remote half: a push that names no source deletes the remote branch,
	// and `--delete` says the same thing in words.
	for _, remote := range []string{
		`git push origin :refs/heads/feat/gone`,
		`git push --delete origin feat/gone`,
	} {
		requireHitAt("scripts/probe.sh", remote, "remote deletion "+remote, 1)
	}

	// (4) the 2026-10-12 sequel, one language over: a ref rewritten directly.
	requireHitAt("scripts/probe.sh", `git update-ref -d refs/heads/feat/gone`, "update-ref deletion", 1)

	// (5) THE NEGATIVE SPACE OF THE MENTION/USE SPLIT (2026-11-05). Narrowing the
	// flag reading so a prose FAMILY LIST is not read as one of its members must
	// not let a deletion through. These are the spellings the narrowing could
	// have swallowed, and each must still be reported:
	//
	//   - an ALL-DELETION alternation (`-d/-D`): the token still says "remove it
	//     one way or the other", which is a use, not a vocabulary;
	//   - a SHORT CLUSTER (`-fD`): the shell reads it back as `-f -D`, so it
	//     removes the ref.
	for _, dele := range []string{
		`git branch -d/-D feat/gone`,
		`git branch -fD feat/gone`,
	} {
		requireHitAt("scripts/probe.sh", dele, "deletion spelled as "+dele, 1)
	}

	// (6) THE FALSE POSITIVE THIS RUN FIXES, AS A PROBE. A rule or a refusal that
	// NAMES the family — `git branch -f/-D/-B` — is a mention, not an
	// instruction, and must be clean WITHOUT an inline escape: the gate's own
	// reading has to make the distinction, not a marker a reader can add to any
	// line (the measured defect: the two surfaces that REFUSE the destructive
	// ref move were the two lines the gate reported, so commit 9ccb8f8a could
	// only silence them with `removal-audit-ok:` comments).
	for _, family := range []struct{ rel, src string }{
		// Project Rule 37's banned-vocabulary bullet, as it read on master when
		// this run measured the defect (no marker): the family, never a use.
		{"PROJECT.md",
			"- **NO DESTRUCTIVE REF MOVE**, at any size: `git branch -f/-D/-B`, `git\n" +
				"  update-ref`, `git reset --hard` — anything that rewrites what a ref points at"},
		// the chat refusal string, likewise unmarked.
		{"internal/agent/chat_pa_duty.go",
			`		"REF MOVE: ` + "`" + `git branch -f/-D/-B` + "`" + `, ` + "`" + `git update-ref` + "`" + `, ` + "`" + `git reset --hard` + "`" + ` over a ref this run does not own. " +`},
		// the same family with a pipe alternation, the shell spelling.
		{"scripts/probe.sh", `# this console stops at a ref: git branch -f|-D|-B is operator-only`},
	} {
		if got := find(family.rel, family.src); len(got) != 0 {
			t.Fatalf("a rule statement NAMING the family %q was reported as an instruction (%+v) — "+
				"the gate cannot tell a mention from a use, so its only remedy is a marker on the "+
				"lines that refuse the act", family.src, got)
		}
	}

	// (5) the PRODUCTION Go carrier: a rendered command for an operator to run.
	requireHitAt("internal/tools/probe.go", `const hint = "delete it with `+"`"+`git branch -D "+branch+"`+"`"+`"`, "Go literal", 1)

	// THE GREEN ARM — every line below is the fix, and each must be CLEAN.
	for _, fixed := range []struct{ rel, src string }{
		// the funnel, as scripts/post-merge-cleanup.sh now writes it
		{"scripts/post-merge-cleanup.sh",
			`        if "${GOPI_BIN}" task archive "$BRANCH" --reason "$DEFAULT_ARCHIVE_REASON" --detail "$ARCH_DETAIL" --override-completed; then`},
		// the worktree step is NOT a ref removal (its own arm and its own record)
		{"scripts/post-merge-cleanup.sh", `    git worktree remove "$WT"`},
		// PROJECT.md Rule 9 step 2, post-fix
		{archiveMintInstructionRel,
			"2. **败者分支归档清理**：`gopi task archive <branch> --reason hand-mistake --detail \"<为什么>\"`\n" +
				"   —— 归档**只能**走 `gopi task archive` 这一个写入者"},
		// a Go comment documents the verb it replaced; prose is not prescription
		{"internal/tools/probe.go", `// 过去这里跑 git branch -D "$BRANCH"，现在走唯一写入者`},
	} {
		if got := find(fixed.rel, fixed.src); len(got) != 0 {
			t.Fatalf("post-fix shape must be CLEAN, got %+v: %q — the half would fail the fix it exists for",
				got, fixed.src)
		}
	}

	// READERS move no ref and must never be reported. Each line is a real form
	// measured in this tree.
	for _, reader := range []string{
		`    mapfile -t BRANCHES < <(git branch --merged "$MASTER" --format='%(refname:short)')`,
		`    if ! git branch --merged "$MASTER" --list "$BRANCH" | grep -q .; then`,
		`current="$(git branch --show-current)"`,
		`git branch "$ref-recovered" "$ref"`,
		`git branch -vv`,
		`git branch -a`,
		`git branch --list 'archive/*'`,
		`git branch -f "$NAME" "$BASE"`,
		`git update-ref refs/heads/master 1508e3821`,
		`git push --force origin master`,
		`git push origin feat/some-branch`,
		`git show-ref --verify --quiet "refs/heads/$BRANCH"`,
		// the PROSE that documents the verb: safe-merge.sh's own note
		`# 一个已经把 ref 删掉的分支（e2e 探针、已 merge 后被 ` + "`git branch -d`" + `、拼错的`,
	} {
		if got := find("scripts/probe.sh", reader); len(got) != 0 {
			t.Fatalf("FALSE POSITIVE on a reader/mover: %+v for %q", got, reader)
		}
	}

	// THE SCOPE: history and fixtures quote the shape and stay out of the gate —
	// a record that cannot print its own subject is not a record. (Production Go
	// under internal/ + cmd/ IS a carrier, so this half's own source is one too:
	// that is why the file below keeps its subject in comments and never in a
	// literal.) cmd/agent/*.md is NOT in this list: an LLM instruction file IS a
	// carrier (removalShellIsInstruction) — the measured defect this arm was
	// widened for — and its own proof is
	// TestRemovalAuditGateShellHalfScansTheRealCarriers.
	for _, out := range []string{
		"devdoc/closeout-branch-delete-force-20261014.md",
		"briefings/brief-branch-deleted-before-push.md",
		"tasks/01a13b36-some-note.md",
		"internal/standards/removal_audit_gate_test.go",
		"cmd/agent/merge_prompt_gate_test.go",
	} {
		if got := find(out, `git branch -D "$BRANCH"`); len(got) != 0 {
			t.Fatalf("%s is OUT of this half's carriers, got %+v", out, got)
		}
	}

	// (4) THE INSTRUCTION-SURFACE HALF, PROVED ON A SYNTHETIC PATH — the measured
	// defect was an LLM instruction file whose action column prescribed a raw
	// deletion, so the predicate must still fire on prose.
	//
	// IT IS A PROBE PATH, NOT THE SHIPPED ONE (2026-11-01). The one file this arm
	// was measured against, cmd/agent/merge_prompt.md, was deleted with the
	// one-shot merge verb it instructed (epic merge-verb-removal, item 3) — that
	// child no longer exists in any spelling, so the corpus now reaches zero
	// instruction carriers (a fact TestRemovalAuditGateShellHalfScansTheRealCarriers
	// states and asserts instead of hiding behind a floor of 0). The PREDICATE is
	// unchanged and is what this arm still proves: prose prescribing a raw
	// deletion is reported, and prose routing through the one writer is clean.
	const probePromptRel = "cmd/agent/some-future-prompt.md"
	requireHitAt(probePromptRel,
		"| ① | you landed it … | **merged** | `git branch -d <branch>` once no worktree holds it |",
		"pre-fix prompt disposition row (the MEASURED shape, 2026-10-16)", 1)
	requireHitAt(probePromptRel,
		"5. ONLY NOW: git branch -D <branch>       # (a) archived, (b) queued",
		"pre-fix prompt rework step 5", 1)
	for _, fixed := range []string{
		"| ① | you landed it … | **merged** | `gopi task archive <branch> --reason superseded --override-completed` |",
		"| ④ | … | **rework filed** | **`gopi task archive <branch> --reason superseded --override-completed`** |",
	} {
		if got := find(probePromptRel, fixed); len(got) != 0 {
			t.Fatalf("the post-fix instruction shape must be CLEAN, got %+v: %q — the half would "+
				"fail the fix it exists for", got, fixed)
		}
	}
}

// TestRemovalAuditGateShellHalfScansTheRealCarriers is the non-vacuity half: the
// predicate above is fed a snippet, which would stay green even if the walk never
// opened a tracked script or GOPI.md. Two assertions close that hole:
//
//   - the carrier corpus the walk really builds is at least the declared floor
//     (a broken root or a filter that matches nothing reports a clean green while
//     reading nothing — archiveMintMinScripts' failure mode, one tree over);
//   - the REAL post-merge-cleanup.sh is clean AS IT SHIPS, and becomes RED when
//     the pre-fix delete line is put back into its own source — so the predictor
//     is proved against the file the epic is about, not against a probe file.
func TestRemovalAuditGateShellHalfScansTheRealCarriers(t *testing.T) {
	g := gateByProbe(t, "TestRemovalAuditGateShellHalfRedProof")
	root := repoRoot(t)

	var scanned []string
	for _, rel := range textFilesUnder(t, root, g.scanTextRoot) {
		if g.textCorpus != nil && !g.textCorpus(rel) {
			continue
		}
		scanned = append(scanned, rel)
	}
	if len(scanned) < removalShellMinScannedFiles {
		t.Fatalf("the carrier corpus is %d file(s) — below the %d floor, so a green "+
			"verdict would have read almost nothing", len(scanned), removalShellMinScannedFiles)
	}
	var scripts, goFiles int
	for _, rel := range scanned {
		switch {
		case strings.HasPrefix(rel, removalShellScriptsDir+"/"):
			scripts++
		case strings.HasSuffix(rel, removalShellGoSuffix):
			goFiles++
		}
	}
	if scripts == 0 || goFiles == 0 {
		t.Fatalf("the corpus is not the mixed one this half declares: %d script(s), %d Go file(s)",
			scripts, goFiles)
	}
	// The INSTRUCTION carriers, counted and — more to the point — PROVED present
	// by name: a widened filter that reached nothing would still pass the count
	// above (scripts/ + Go files alone satisfy it), which is the vacuity this
	// third arm closes.
	var instructions []string
	for _, rel := range scanned {
		if removalShellIsInstruction(rel) {
			instructions = append(instructions, rel)
		}
	}
	// THE INSTRUCTION CARRIERS ARE ZERO, AND THAT IS A MEASUREMENT (2026-11-01).
	// This half exists for a MEASURED defect (2026-10-16: a merge child read
	// cmd/agent/merge_prompt.md's disposition table and ran the raw deletion it
	// prescribed). That file was deleted with the one-shot merge verb it
	// instructed (epic merge-verb-removal, item 3): the child it spoke to no
	// longer exists, so there is no instruction surface left under
	// removalShellInstructionRoot for the walk to reach.
	//
	// A FLOOR OF 0 WOULD BE THE VACUITY THIS ARM WAS WIDENED TO CLOSE, so the arm
	// is not deleted and not floored away — it states the count it expects and
	// FAILS in BOTH directions: a future instruction file re-opens the surface
	// (and this assertion above it, with the predicate proof in the red half,
	// becomes the arm for it again), while a shipped file the walk cannot SEE
	// would show up as a count that disagrees with the tree.
	//
	// The measured number is a CONSTANT with its reason (Rule 1/13), not a
	// literal buried in a comparison: whoever re-opens an instruction surface
	// reads this and knows the arm is live.
	if len(instructions) != removalShellShippedInstructionFiles {
		t.Fatalf("the walk reached %d instruction carrier(s) (%v), but this tree ships %d — "+
			"cmd/agent/merge_prompt.md was deleted with the merge verb it instructed (2026-11-01), "+
			"so either a new instruction surface has appeared (then the predicate proof in "+
			"TestRemovalAuditGateShellHalfRedProof is the arm for it, and this constant is the "+
			"number to raise) or the walk no longer sees the tree it declares (%s)",
			len(instructions), instructions, removalShellShippedInstructionFiles,
			removalShellInstructionRoot)
	}
	t.Logf("carrier corpus: %d file(s) — %d under %s/, %d production Go, %d instruction (%v), plus %s",
		len(scanned), scripts, removalShellScriptsDir, goFiles, len(instructions), instructions, archiveMintInstructionRel)

	// THE TWO MEASURED FALSE POSITIVES, ON THE REAL CARRIERS (2026-11-05). Both
	// files are IN this half's corpus, and both spend their words REFUSING the
	// destructive ref move — so a predicate that reads a family listing as one of
	// its members reports the two surfaces whose whole meaning is the ban. The
	// assertion below is taken with the ESCAPE IGNORED (removalShellFind returns
	// hits before any marker is consulted), so it measures the READING, not a
	// suppression: an inline `removal-audit-ok:` can no longer be the only thing
	// standing between these files and a red gate.
	for _, rel := range []string{archiveMintInstructionRel, removalShellPADutyRel} {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		raw := mustReadBytes(t, abs)
		if got := removalShellFind(g, &parsed{rel: rel, src: raw}); len(got) != 0 {
			t.Fatalf("%s reports %d hit(s) (%+v) — it is a rule/refusal that NAMES the destructive "+
				"ref-move family, so the gate is reading a mention as an instruction and the only "+
				"remedy left is a marker on the lines that forbid the act", rel, len(got), got)
		}
	}
	t.Logf("no false positive on the two refusal carriers (%s, %s)", archiveMintInstructionRel, removalShellPADutyRel)

	// The real-tree green half, then the real-tree RED half: the same file, with
	// the pre-fix invocation spliced back in at the place the fix removed it.
	const rel = "scripts/post-merge-cleanup.sh"
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	src := string(raw)
	if got := removalShellFind(g, &parsed{rel: rel, src: []byte(src)}); len(got) != 0 {
		t.Fatalf("the shipped %s still reports %+v — the fix this half was written for is not in the file", rel, got)
	}
	// The two invocations are the file's own history: the second is what this
	// task removed from it. Both are spelled once (Rule 14) and BOTH are used —
	// the green assertion reads the shipped form, the red one splices the
	// pre-fix form in its place.
	const shippedFunnel = `"${GOPI_BIN}" task archive "$BRANCH"`
	const preFixDelete = `git branch -D "$BRANCH"`
	if !strings.Contains(src, shippedFunnel) {
		t.Fatalf("%s no longer routes its ref removal through the one writer (no %s) — this probe "+
			"must be updated with the site's current shape rather than left vacuously green", rel, shippedFunnel)
	}
	reverted := strings.Replace(src, shippedFunnel, preFixDelete, 1)
	dirty := removalShellFind(g, &parsed{rel: rel, src: []byte(reverted)})
	if len(dirty) == 0 {
		t.Fatalf("with %s put back in place of the funnel, %s reports NOTHING — the predicate "+
			"cannot see the real file, so its green verdict is meaningless", preFixDelete, rel)
	}
	t.Logf("RED on the real file (funnel reverted): %+v", dirty)

	// THE INSTRUCTION CARRIER, TWO SIDED — ON A PROBE PATH (2026-11-01). It used to
	// run against the SHIPPED cmd/agent/merge_prompt.md, which is gone with the
	// merge verb it instructed (epic merge-verb-removal, item 3). The predicate is
	// unchanged, and a synthetic carrier still proves it in both directions:
	// routing a disposition row through the one writer is CLEAN, and putting the
	// pre-fix row back makes it fire with a file:line.
	const promptRel = "cmd/agent/some-future-prompt.md"
	promptSrc := "| ① | you landed it … | **merged** | " + removalShellPromptRoutedRow + " |\n"
	if got := removalShellFind(g, &parsed{rel: promptRel, src: []byte(promptSrc)}); len(got) != 0 {
		t.Fatalf("a carrier that routes its rows through the one writer reports %+v — a prompt that "+
			"prescribes a raw ref deletion sends a child down the unaudited path this half exists "+
			"to close", got)
	}
	promptReverted := strings.Replace(promptSrc, removalShellPromptRoutedRow, removalShellPromptRawRow, 1)
	promptDirty := removalShellFind(g, &parsed{rel: promptRel, src: []byte(promptReverted)})
	if len(promptDirty) == 0 {
		t.Fatalf("with the pre-fix row put back in place of the routed one, %s reports NOTHING — "+
			"the predicate cannot see an instruction carrier, so its green verdict is meaningless",
			promptRel)
	}
	t.Logf("RED on a probe instruction carrier (row reverted): %+v", promptDirty)
}

// removalShellPromptRoutedRow / removalShellPromptRawRow are the shipped
// disposition row and the pre-fix row it replaced, spelled ONCE here so the
// green assertion and the red splice cannot drift onto two different strings
// (Rule 14). The raw row is the MEASURED shape: the merge child's own transcript
// carries exactly this action cell, and the ref it deleted has no branch-delete
// record on any host (2026-10-16).
const (
	removalShellPromptRoutedRow = "`gopi task archive <branch> --reason superseded --override-completed` once no worktree holds it"
	removalShellPromptRawRow    = "`git branch -d <branch>` once no worktree holds it"
)

// TestRemovalAuditGateShellHalfDevdocCarriers is the arm this epic adds: the
// census's text half must read the devdoc INSTRUCTION SURFACES a child is handed
// — not the whole devdoc corpus (a record must stay able to print its subject),
// but the named carrier set, whose first member is the merger's own lifecycle
// rule set that cmd/agent/merge_prompt.md cites by section (§3.6.1).
//
// Three assertions, and the second and third are the ones a count cannot make:
//
//   - the walk REACHES the carrier set (floor + the sentinel BY NAME, because a
//     widened filter that read nothing would still satisfy a floor);
//   - the REAL sentinel is CLEAN as it ships, and putting the pre-fix
//     disposition row back into its own text drives the predicate RED on the
//     real file with a file:line — the evidence, measured against the shipped
//     artifact rather than a probe;
//   - the RESTORE is verified by CONTENT IDENTITY AGAINST A REVISION
//     (`git hash-object <path>` == `git rev-parse HEAD:<path>`), never by an
//     index-relative diff: a restore seeded from the index writes back the very
//     bytes the index holds, so the diff would be empty whatever the file says
//     (Rule 5's red-proof criterion). The file is restored in a t.Cleanup that
//     runs even when an assertion above fails.
func TestRemovalAuditGateShellHalfDevdocCarriers(t *testing.T) {
	g := gateByProbe(t, "TestRemovalAuditGateShellHalfRedProof")
	root := repoRoot(t)

	var carriers []string
	for _, rel := range textFilesUnder(t, root, g.scanTextRoot) {
		if g.textCorpus != nil && !g.textCorpus(rel) {
			continue
		}
		if removalShellIsDevdocCarrier(rel) {
			carriers = append(carriers, rel)
		}
	}
	if len(carriers) < removalShellMinDevdocCarriers {
		t.Fatalf("the corpus reached %d devdoc carrier(s), below the %d floor — the arm written for "+
			"the merger's own lifecycle docs is reading nothing", len(carriers), removalShellMinDevdocCarriers)
	}
	foundSentinel := false
	for _, rel := range carriers {
		if rel == removalShellDevdocSentinel {
			foundSentinel = true
		}
	}
	if !foundSentinel {
		t.Fatalf("%s is not among the devdoc carriers the walk scanned %v — this half was widened FOR "+
			"that file (cmd/agent/merge_prompt.md cites its §3.6.1 disposition table), so a green "+
			"verdict that never opened it is meaningless", removalShellDevdocSentinel, carriers)
	}
	t.Logf("devdoc carrier corpus: %d file(s) of the %d declared %v", len(carriers),
		len(removalShellDevdocCarriers), removalShellDevdocCarriers)

	// THE REAL FILE, BOTH SIDES. The shipped text must be clean; the pre-fix
	// disposition row spliced back in its place must be reported, on the real
	// file, with a line number.
	const rel = removalShellDevdocSentinel
	abs := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	src := string(raw)
	if got := removalShellFind(g, &parsed{rel: rel, src: []byte(src)}); len(got) != 0 {
		t.Fatalf("the shipped %s still reports %+v — a devdoc that prescribes a raw ref removal hands "+
			"the child the unaudited path this half exists to close", rel, got)
	}
	if !strings.Contains(src, removalShellDevdocRoutedRow) {
		t.Fatalf("%s no longer routes its disposition rows through the one writer (no %q) — this probe "+
			"must be updated with the row's current shape rather than left vacuously green",
			rel, removalShellDevdocRoutedRow)
	}

	// Park the restore FIRST, so a failing assertion below cannot leave the tree
	// dirty (Rule 30). The reading is a BYTE comparison inside Go (`bytes.Equal`
	// of the file as it stands against the bytes this test read before touching
	// it) rather than a digest pair — deliberately, because a two-digest
	// comparison with no revision named on either side is the shape
	// TestGateRedProofRestoreRedProof reports: an identity reading that cannot
	// invalidate itself when the file is written again. The REVISION-anchored
	// arm of this same test (below) is where the epic's criterion
	// (`hash-object` == `rev-parse HEAD:<path>`) is measured, on a carrier whose
	// bytes a revision holds.
	restore := func() {
		if err := os.WriteFile(abs, raw, 0o644); err != nil {
			t.Fatalf("restore %s: %v", rel, err)
		}
		if now := mustReadBytes(t, abs); !bytes.Equal(now, raw) {
			t.Fatalf("restore of %s did not put the pre-splice bytes back (%d bytes now, %d parked)",
				rel, len(now), len(raw))
		}
	}
	t.Cleanup(restore)

	spliced := strings.Replace(src, removalShellDevdocRoutedRow, removalShellDevdocRawRow, 1)
	if spliced == src {
		t.Fatalf("the splice did not change %s — the raw row in the proof is not the routed row's shape", rel)
	}
	if err := os.WriteFile(abs, []byte(spliced), 0o644); err != nil {
		t.Fatalf("splice %s: %v", rel, err)
	}
	onDisk, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("re-read %s: %v", rel, err)
	}
	if bytes.Equal(onDisk, raw) {
		t.Fatalf("the splice of %s did not change the file — it was not written", rel)
	}
	dirty := removalShellFind(g, &parsed{rel: rel, src: onDisk})
	if len(dirty) == 0 {
		t.Fatalf("with %s put back in place of the routed row, the ON-DISK %s reports NOTHING — the "+
			"predicate cannot see the real devdoc, so its green verdict is meaningless",
			removalShellDevdocRawRow, rel)
	}
	if dirty[0].rel != rel {
		t.Fatalf("the hit names %q, want %q — the report must be attributable to the file that was spliced",
			dirty[0].rel, rel)
	}
	t.Logf("RED on the real devdoc carrier (row reverted): %+v", dirty)

	// The restore is exercised here rather than only at cleanup, so the reading
	// is taken in THIS test body; the cleanup is the guarantee, this is the proof.
	restore()
	if got := removalShellFind(g, &parsed{rel: rel, src: []byte(mustReadString(t, abs))}); len(got) != 0 {
		t.Fatalf("after the restore %s still reports %+v", rel, got)
	}

	// THE REVISION-ANCHORED ARM (the acceptance criterion, verbatim): the reading
	// is `git hash-object <path>` == `git rev-parse HEAD:<path>`, taken across a
	// splice and a restore, and the carrier is one of the declared set so the arm
	// cannot drift onto a file the corpus does not read.
	//
	// The arm is written to work in BOTH run states, which is the point of
	// anchoring to a revision rather than to the index: it first puts HEAD's OWN
	// bytes on disk (this epic's edits are uncommitted, so "on disk" and "at HEAD"
	// are different content until the branch's commit lands), asserts the reading
	// green, splices the raw row in and asserts the SAME reading turns red by
	// itself — a later write moves it, which is exactly the property an
	// index-relative diff lacks — then restores and asserts green. The working
	// bytes are put back at the end, verified by their own blob identity.
	const headRel = removalShellDevdocPrefix + "merge-backlog-terminal-guard.md"
	headAbs := filepath.Join(root, filepath.FromSlash(headRel))
	working := mustReadString(t, headAbs)
	headBytes := gitOutRaw(t, root, "show", "HEAD:"+headRel)
	readHEAD := func() (string, string) {
		return gitOut(t, root, "hash-object", headRel), gitOut(t, root, "rev-parse", "HEAD:"+headRel)
	}
	restoreWorking := func() {
		mustWriteFile(t, headAbs, working)
		if now := mustReadBytes(t, headAbs); !bytes.Equal(now, []byte(working)) {
			t.Fatalf("the working bytes of %s were not put back", headRel)
		}
	}
	t.Cleanup(restoreWorking)

	mustWriteFile(t, headAbs, headBytes)
	if got, want := readHEAD(); got != want {
		t.Fatalf("HEAD's own bytes written back do not read as HEAD's blob: %s vs %s (%s)", got, want, headRel)
	}
	mustWriteFile(t, headAbs, headBytes+"\n"+removalShellDevdocRawRow+"\n")
	if got, want := readHEAD(); got == want {
		t.Fatalf("the splice into %s left the HEAD-anchored reading green — the criterion is not "+
			"measuring the file", headRel)
	}
	mustWriteFile(t, headAbs, headBytes)
	if got, want := readHEAD(); got != want {
		t.Fatalf("after restore %s: hash-object=%s, rev-parse HEAD:%s=%s — the criterion this epic "+
			"specifies did not come back green", headRel, got, headRel, want)
	}
	t.Logf("HEAD-anchored restore verified: %s hash-object == rev-parse HEAD:%s", headRel, headRel)
	restoreWorking()
}

// gitOutRaw is gitOut without the trailing-whitespace trim: a revision's own
// bytes are compared by blob identity, so trimming them would change the very
// thing under test.
func gitOutRaw(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

// mustReadBytes reads a file's bytes or fails the test.
func mustReadBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// mustWriteFile writes a file or fails the test.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mustReadString reads a file or fails the test — the small local helper the
// assertion above needs, kept here rather than reaching for mustRead (whose
// failure message belongs to the docindex proof that defined it).
func mustReadString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
