package standardgate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepoRootHasNoTrackedJunk is the gate proper: no tracked path may be a
// test binary, a leak-probe tree, an e2e evidence dir, or a root merge leftover.
func TestRepoRootHasNoTrackedJunk(t *testing.T) {
	paths := trackedPaths(t)
	if hits := junkTrackedPaths(paths); len(hits) > 0 {
		t.Errorf("tracked scratch/junk paths in the repo index (%d):\n  %s\n"+
			"untrack with a targeted `git rm -r --cached <path>` and add the pattern to .gitignore "+
			"(see devdoc/repo-hygiene-tracked-scratch.md)",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// TestRepoRootJunkGateRedProof proves the gate is not vacuous: with the very
// paths that had to be untracked present in the index list, it must report them.
// A predicate that silently stopped matching would otherwise leave the gate
// green forever.
func TestRepoRootJunkGateRedProof(t *testing.T) {
	// The exact shapes the sweep had to untrack (repo-root relative).
	poisoned := []string{
		".leakprobe-agent.test",
		".leakprobe/home/.gopi/memory.db",
		".leakprobe/home/.gopi/sessions/8e3a2e7e-685a-4008-abb9-4dc63b7f4e8a.jsonl",
		".leakprobe2/home/.cache/go-build/00/002652a240e20f376134468874de61ab0f022a6dfa893fc4594975a6ef127ee3-d",
		"tmp-e2e/session-logs/run1.err",
		"e2e-m420-rank/a/.gopi/memory.db",
		"w.txt",
		"y.txt",
		"base.txt",
		"shared.txt",
		"result.json",
		"s6.png",
		"memory.db",
		"recycle-epic-marker",
		"worktree-owner",
		// Root report docs — the parking-lot class (37 were committed here).
		"report-r3.md",
		"FINAL-VERIFICATION-REPORT.md",
		// Root build outputs — `go build ./cmd/agent/` (no -o) writes ./agent.
		"agent",
		"gopi",
		// An explicit `-o` at the root. The class is a PREFIX, so one sample
		// per shape is enough to pin it: a name the tracked recipe writes
		// (gopi-behind) and one a devdoc narrative writes (gopi-before).
		"gopi-behind",
		"gopi-before",
		// The repo-relative scratch/ class: the three paths committed by
		// 6cf7d1ef that made `<repo>/scratch/` read as a convention (the
		// causal precondition of the 2026-09-26 135 MB residue), plus a
		// nested path so the class is pinned as a TREE and not three names.
		"scratch/payload.md",
		"scratch/expected.md",
		"scratch/run-clean.log",
		"scratch/replay-01a0db54-audit/evidence.jsonl",
	}
	hits := junkTrackedPaths(poisoned)
	if len(hits) != len(poisoned) {
		t.Fatalf("red-proof failed: %d of %d poisoned paths reported:\n  %s",
			len(hits), len(poisoned), strings.Join(hits, "\n  "))
	}

	// The complement — what the gate must NOT fire on. A test temp dir holds
	// these very basenames; internal/standards/env_reader.go legitimately ends
	// in ".go" not ".test"; and the e2e script's scratch clone is not the root.
	clean := []string{
		"go.mod",
		".gitignore",
		"internal/standards/scratch_gate.go",
		"internal/standards/scratch_gate_test.go",
		"cmd/agent/cli_branch_scan_test.go",
		"bin/x.testdata/keep",            // ".test" must not match a PREFIX
		"internal/testdata/.leakprobe/x", // only the root tree, not a nested lookalike
		"text2img-eval/TEXT2IMG-EVAL-codex.md",
		"reports/e2e-m420-rank-notes.md",
		"reports/tmp-e2e.md", // a root doc basename in its proper home
		"scripts/e2e-task-merge.sh",
		"devdoc/repo-hygiene-tracked-scratch.md",
		// The two root docs that are allowed to STAY TRACKED: the release
		// surface and the documents of record. GOPI.md is no longer one of them —
		// it is the client's own rule file, ignored at the root (see the
		// .gitignore comment and TestRepoRootGopiDocIsUntrackedButReadable).
		"CHANGELOG.md",
		"CONFIG.md",
		"COMMANDS.md",
		"reports/archive/2026-08/report-r3.md",
		"reports/archive/2026-09/report-memory.md",
		"devdoc/workspace-hygiene-report.md",
		"bin/agent", // a binary in the (ignored) build dir is where it belongs
		// The repo-relative scratch/ class is root-ANCHORED, so the package
		// whose NAME happens to start with the same letters and a root path
		// that merely shares the prefix boundary stay legitimate.
		"internal/scratch/gc.go",
		"internal/scratch/tmproot.go",
		"internal/standards/scratch_gate.go",
		"scratchpad-notes.txt", // shares the string prefix, never the path component
		"reports/scratch/replay-audit.md",
		// The root-artifact PREFIX class must not reach a directory. These are
		// the two packages the 2026-09 unanchored `agent`/`gopi` patterns
		// swallowed (the accident .gitignore records); the class is anchored to
		// the root AND prefixed with the hyphen, so both must stay clean — if
		// either ever matches, the pattern was loosened.
		"cmd/agent/main.go",
		"internal/agent/loop.go",
	}
	if hits := junkTrackedPaths(clean); len(hits) > 0 {
		t.Errorf("gate fired on legitimate tracked paths:\n  %s", strings.Join(hits, "\n  "))
	}
}

// TestRepoRootJunkGateScansTheRealIndex asserts the gate is wired to the real
// index rather than to a fixture — the sibling of the red-proof. It runs the
// same listing the gate uses and fails if it comes back empty, so a broken git
// invocation is a failure and never a silent pass.
func TestRepoRootJunkGateScansTheRealIndex(t *testing.T) {
	paths := trackedPaths(t)
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, moduleFileName)); err != nil {
		t.Fatalf("module root %s unreadable: %v", root, err)
	}
	// The tree is large; a single-digit count means we are not looking at it.
	if len(paths) < 100 {
		t.Fatalf("%s %s listed only %d paths — not the real index",
			gitBinary, strings.Join(gitLsFilesArgv, " "), len(paths))
	}
}

// TestRepoRootJunkPathsAreGitignored asserts every junk class the index gate
// refuses is ALSO covered by .gitignore. The two halves are complementary: the
// index gate cannot see an untracked path (the `??` in `git status` that invites
// the next `git add`), and the ignore rules never apply to a tracked path.
//
// This is the check that would have caught the two typos this task made:
// `/w.txt`//`/y.txt` were missing entirely, and the two dot-markers were written
// as `/recycle-epic-marker` and `/worktree-owner` — ignoring names that never
// existed while the real `.recycle-epic-marker` / `.worktree-owner` stayed in
// `git status` as untracked.
func TestRepoRootJunkPathsAreGitignored(t *testing.T) {
	paths := make([]string, 0, len(junkIgnoreSamples))
	for _, s := range junkIgnoreSamples {
		paths = append(paths, s.path)
	}
	ignored := ignoredPaths(t, paths)
	var missed []string
	for _, s := range junkIgnoreSamples {
		if !ignored[s.path] {
			missed = append(missed, s.path+" ("+s.why+")")
		}
	}
	if len(missed) > 0 {
		t.Errorf("%d junk path(s) are NOT covered by .gitignore — an untracked one shows up in `git status` and is one `git add` from being committed:\n  %s",
			len(missed), strings.Join(missed, "\n  "))
	}
	if len(ignored) == 0 {
		t.Fatalf("%s reported nothing ignored — refusing to pass blind", gitBinary)
	}

	// The patterns are anchored/root-scoped on purpose; they must not start
	// swallowing legitimate paths that merely share a basename.
	allowed := make([]string, 0, len(junkIgnoreAllowed))
	for _, s := range junkIgnoreAllowed {
		allowed = append(allowed, s.path)
	}
	for p, hit := range ignoredPaths(t, allowed) {
		if hit {
			t.Errorf("legitimate path %q is ignored — the pattern is too broad", p)
		}
	}
}

// TestRepoRootJunkGateScansImmediately is the ratchet's timing half: it runs the
// index check over the real index and fails on a junk path that is ALREADY
// tracked, which is the state the sweep found (457 paths). It is separate from
// TestRepoRootHasNoTrackedJunk so a future reader can see the "before" shape
// documented next to the "after" assertion.
func TestRepoRootJunkGateScansImmediately(t *testing.T) {
	if hits := junkTrackedPaths(trackedPaths(t)); len(hits) > 0 {
		t.Errorf("the index still carries junk (this is the pre-sweep state):\n  %s", strings.Join(hits, "\n  "))
	}
}

// TestRepoRootGopiDocIsUntrackedButReadable pins the two halves of the
// 2026-10-04 decision, because each half alone reads as an ordinary tidy-up —
// and both still hold AFTER the 2026-10-19 split, which moved the project's
// contract into PROJECT.md and left this file as the client's own policy:
//
//  1. GOPI.md is the CLIENT's own rule file, so git must not carry it — the
//     index names it nowhere, and .gitignore hides it so it never returns as a
//     `??` that the next `git add .` sweeps back in. A checkout gets the file
//     from its operator.
//  2. gopi STILL READS it: the loader resolves the name from the run's cwd, not
//     from the index, so un-tracking must not be mistaken for un-shipping. A
//     checkout carrying only this file still gets its rules injected — that is
//     the second candidate name in internal/agent's loader.
//
// THE ABSENT HALF IS NOW LEGAL (2026-10-19): a BARE clone has no GOPI.md — it
// has PROJECT.md instead. This test therefore checks (1) always and (2) only
// when the operator's file is present, and the tracked half is pinned by
// TestRepoRootProjectDocIsTrackedAndReadable below. Before the split both were
// asserted here, which is why every worker in a fresh worktree failed this test.
func TestRepoRootGopiDocIsUntrackedButReadable(t *testing.T) {
	root := repoRoot(t)
	for _, p := range trackedPaths(t) {
		if p == localProjectContextFileName {
			t.Fatalf("%s is TRACKED again — it is the client's own rule file and must not travel by clone; "+
				"it stays ignored at the root (see the .gitignore comment)", localProjectContextFileName)
		}
	}
	onDisk := filepath.Join(root, localProjectContextFileName)
	info, err := os.Stat(onDisk)
	if err != nil {
		// A bare clone: the client's file is simply not here, and the tracked
		// PROJECT.md is what the loader resolves instead.
		return
	}
	if info.Size() < gopiDocMinInstructionBytes {
		t.Fatalf("%s is %d bytes (floor %d) — the read is not looking at the real rule file",
			localProjectContextFileName, info.Size(), gopiDocMinInstructionBytes)
	}
	if _, err := os.ReadFile(onDisk); err != nil {
		t.Fatalf("read %s: %v — the loader would fail the same way", localProjectContextFileName, err)
	}
}

// TestRepoRootProjectDocIsTrackedAndReadable pins the other half of the split:
// PROJECT.md is the PROJECT's contract, so it IS tracked (every clone and every
// linked worktree carries it) and it carries the rules — the read floor keeps a
// stub from satisfying the check.
func TestRepoRootProjectDocIsTrackedAndReadable(t *testing.T) {
	root := repoRoot(t)
	tracked := false
	for _, p := range trackedPaths(t) {
		if p == projectContextFileName {
			tracked = true
			break
		}
	}
	if !tracked {
		t.Fatalf("%s is NOT tracked — it is the project's engineering contract and must travel with every "+
			"clone and worktree (the 2026-10-19 split moved Rules 1-41 into it)", projectContextFileName)
	}
	if !rootDocAllowlist[projectContextFileName] {
		t.Fatalf("%s is tracked but not in rootDocAllowlist — a root doc is legal only as a reviewed entry there",
			projectContextFileName)
	}
	onDisk := filepath.Join(root, projectContextFileName)
	raw, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("read %s: %v", projectContextFileName, err)
	}
	if len(raw) < gopiDocMinInstructionBytes {
		t.Fatalf("%s is %d bytes (floor %d) — the tracked contract is a stub",
			projectContextFileName, len(raw), gopiDocMinInstructionBytes)
	}
	if !strings.Contains(string(raw), gopiDocReadAnchor) {
		t.Errorf("%s does not carry %q — the file may be a stub rather than the rule file",
			projectContextFileName, gopiDocReadAnchor)
	}
	if !strings.Contains(string(raw), "## Rule 1:") {
		t.Errorf("%s carries no Rule 1 heading — the rules did not move with the split", projectContextFileName)
	}
}

const (
	// projectContextFileName / localProjectContextFileName are the TWO rule-file
	// names, spelled once here: the loader's own constants live in
	// internal/agent (which this package must not import — a standards gate that
	// depended on the code it gates could not catch the code's own drift), so the
	// gate keeps its own copies WITH this comment naming the other pair.
	// projectContextFileName is the TRACKED contract; the local one is the
	// client's own file, which git must not carry.
	projectContextFileName      = "PROJECT.md"
	localProjectContextFileName = "GOPI.md"
	// gopiDocMinInstructionBytes is the read floor: a truncated or stub file is
	// not the rule surface, and a gate that read a stub would report a clean
	// green.
	gopiDocMinInstructionBytes = 1024
	// gopiDocReadAnchor is a phrase the shipped rule file carries on its own
	// first lines (its opening statement of what the file IS), so "the file
	// exists" is not the only shape the check can tell apart — a stub or a
	// placeholder does not carry it.
	gopiDocReadAnchor = "GoPi reads this file from the current working directory at startup"
)

// TestRepoRootReadmeIsTrackedAndIsTheFace pins the 2026-11-06 entry that put
// README.md on the root allowlist.
//
// The entry is what makes the file TRACKABLE (the root's markdown surface is
// exactly rootDocAllowlist), so without this arm the entry could be deleted and
// the only symptom would be a `.gitignore` that quietly swallows the file again
// — a checkout that explains its verbs and its env vars but never says what
// the program IS. That silence is the measured gap this file closes:
// devdoc/competitor/summary.md §5 ranked "no named entry point" first by ROI
// across 18 peers, and its §4.1 recorded that gopi had no README at all.
//
// Three halves, each red on its own:
//   - the allowlist entry exists (otherwise `.gitignore`'s `/*.md` re-swallows it);
//   - `.gitignore` actually un-ignores it (the allowlist alone is not enough —
//     `/*.md` is matched FIRST, so the `!` line is the half that lets it through);
//   - the file carries the FACE's content, not a stub (read floor + the two
//     headings a reader is promised: what it can do, and how to install it).
func TestRepoRootReadmeIsTrackedAndIsTheFace(t *testing.T) {
	if !rootDocAllowlist[rootReadmeName] {
		t.Fatalf("%s is not in rootDocAllowlist — the root markdown surface is exactly that map, so "+
			"the file cannot be tracked and `.gitignore`'s `/*.md` swallows it", rootReadmeName)
	}

	root := repoRoot(t)
	onDisk := filepath.Join(root, rootReadmeName)
	raw, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("read %s: %v", rootReadmeName, err)
	}
	if len(raw) < rootReadmeMinBytes {
		t.Fatalf("%s is %d bytes (floor %d) — that is a stub or a placeholder, not the project's face",
			rootReadmeName, len(raw), rootReadmeMinBytes)
	}
	body := string(raw)
	// The sections a reader is promised by the gap this closes: what the
	// program can DO, and where the details live. The comparison is over
	// HEADING TEXT in whichever language the face currently speaks
	// (rootReadmeAnchors), so the message names what was actually compared —
	// the previous wording ("lost the section that answers it") was false for
	// the shape it actually met: the section was present, in English.
	for _, anchor := range rootReadmeMissingAnchors(body) {
		t.Errorf("%s carries none of the heading spellings of its %q section (%s) — this arm compares "+
			"HEADING TEXT, so either the section was RENAMED (add the new spelling to rootReadmeAnchors "+
			"and keep the old one) or it is genuinely GONE (restore it)",
			rootReadmeName, anchor.section, anchor.quotedSpellings())
	}
	// The install section must name the target the Makefile actually declares,
	// so the document cannot promise a command that does not exist (Rule 31).
	if !strings.Contains(body, rootReadmeInstallCommand) {
		t.Errorf("%s does not name %q — the document would point a reader at a command the tree does not have",
			rootReadmeName, rootReadmeInstallCommand)
	}
}

// TestRepoRootReadmeIsUnIgnored is the `.gitignore` half, measured rather than
// assumed: `git check-ignore` on the real path must NOT match. The allowlist
// entry alone does not do this — `/*.md` is evaluated first and wins unless a
// `!` line names the file — so a reader who deleted the `!` line would see the
// allowlist still green while the file vanished from every clone.
func TestRepoRootReadmeIsUnIgnored(t *testing.T) {
	if _, err := os.Stat(filepath.Join(repoRoot(t), ".gitignore")); err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	want := "!" + "/" + rootReadmeName
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == want {
			return
		}
	}
	t.Fatalf(".gitignore carries no %q line — `/*.md` matches %s first, so without the negation "+
		"the file is ignored in every fresh clone no matter what rootDocAllowlist says",
		want, rootReadmeName)
}

const (
	// rootReadmeName is the project's face — the ONE root document written for a
	// reader who has not installed anything yet.
	rootReadmeName = "README.md"
	// rootReadmeMinBytes is the read floor: a stub, a placeholder, or a file
	// truncated to a title must not satisfy "the project has a face".
	rootReadmeMinBytes = 4096
	// rootReadmeInstallCommand is the named entry point the Makefile declares,
	// so the document is tied to the artifact rather than to prose.
	rootReadmeInstallCommand = "make install"
)

// rootReadmeAnchor is ONE section of the face in EVERY language it has been
// written in — the English heading README.md carries today, and the Chinese one
// it carried before commit 81b83934 translated the document.
//
// It is a PAIR rather than two flat entries in a string list because the two
// spellings are the SAME promise: flattened, a document could keep one and drop
// the other and still pass, so the section would be gone while the check stayed
// green. Same philosophy as docMapUnitAlternation
// (docmap_figure_gate_test.go) — an alternation says "either spelling satisfies
// me" — though the spelling here is a slice rather than one regexp, because a
// `##` heading carries regexp metacharacters and a slice needs no escaping.
type rootReadmeAnchor struct {
	// section names the promise in the failure message, in NEITHER language's
	// heading text, so a reader learns WHAT is missing instead of which string
	// failed to match.
	section string
	// spellings are the heading texts this section has been written with. ANY
	// one of them satisfies the arm. None may be empty or whitespace: an empty
	// spelling matches every body (`strings.Contains(body, "")` is always
	// true), which is exactly the rubber stamp
	// TestRepoRootReadmeAnchorListRefusesADeletedSection exists to refuse.
	spellings []string
}

// quotedSpellings renders a section's heading texts for the failure message, so
// the reader sees exactly what was compared rather than a bare count.
func (a rootReadmeAnchor) quotedSpellings() string {
	q := make([]string, len(a.spellings))
	for i, s := range a.spellings {
		// Deliberately not strconv.Quote: headings never carry a quote or a
		// control byte, and one plain pair of quotes keeps the message
		// readable without a new import.
		q[i] = `"` + s + `"`
	}
	return strings.Join(q, " / ")
}

// rootReadmeAnchors are the sections the face must carry: what it can do, and
// where the details live.
//
// IT IS A LITERAL, HAND-MAINTAINED LIST, AND THAT IS THE POINT. Deriving it
// from README.md (every H2 the file happens to carry) would make the gate
// assert that whatever the file says is promised — deleting a section would
// delete the check with it and the arm would be a tautology, green over an
// empty file. Deriving it from a translation table or from the other language's
// document would make the gate read the file twice and compare it with itself.
// The list is the PROMISE; the file is the FULFILMENT; the gate compares the
// two. That is why a rename is a deliberate edit here (keep the old spelling,
// append the new one), and why a genuinely DELETED section still fails — proved
// by TestRepoRootReadmeAnchorListRefusesADeletedSection, which deletes each
// section from the real body and requires the same predicate to report it.
var rootReadmeAnchors = []rootReadmeAnchor{
	{
		section:   "what it can do",
		spellings: []string{"## What it can do", "## 它能做什么"},
	},
	{
		section:   "the documentation map",
		spellings: []string{"## Documentation map", "## 文档地图"},
	},
}

// rootReadmeMissingAnchors returns the sections of rootReadmeAnchors that body
// does not carry in ANY of its spellings. ONE predicate serves both the real arm
// and its red proof (Rule 14), so the proof cannot be measuring a different
// check than the gate runs.
func rootReadmeMissingAnchors(body string) []rootReadmeAnchor {
	var missing []rootReadmeAnchor
	for _, anchor := range rootReadmeAnchors {
		carried := false
		for _, spelling := range anchor.spellings {
			if strings.Contains(body, spelling) {
				carried = true
				break
			}
		}
		if !carried {
			missing = append(missing, anchor)
		}
	}
	return missing
}

// TestRepoRootReadmeAnchorListRefusesADeletedSection is the RED PROOF for the
// arm above, and it is the half that keeps the repair from ratcheting the gate
// down to a rubber stamp.
//
// The repairable half of that arm was "it only read the Chinese headings"; the
// half that had to survive the repair is "a section that is genuinely gone
// still fails". Without this test, widening the list would be indistinguishable
// from emptying it: an entry with no spellings, or with an empty spelling,
// would satisfy every body.
//
// The fixture is the REAL README.md with one section's every spelling deleted,
// so it differs from the shipped file by exactly the defect being proved; the
// same predicate the arm runs is what judges it.
func TestRepoRootReadmeAnchorListRefusesADeletedSection(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), rootReadmeName))
	if err != nil {
		t.Fatalf("read %s: %v", rootReadmeName, err)
	}
	body := string(raw)

	// Precondition: the shipped file must satisfy the list, or the red proof
	// below would pass for the wrong reason (a body that already fails proves
	// nothing about deletion).
	if missing := rootReadmeMissingAnchors(body); len(missing) != 0 {
		t.Fatalf("fixture precondition: the shipped %s already misses %d section(s) — fix that first, "+
			"otherwise this red proof cannot tell deletion from an already-red list", rootReadmeName, len(missing))
	}

	// The rubber-stamp guard: every section must name at least one spelling and
	// every spelling must be real text. An empty spelling matches ANY body, so
	// this is the one entry shape that would make the gate unconditional.
	for _, anchor := range rootReadmeAnchors {
		if len(anchor.spellings) == 0 {
			t.Errorf("section %q lists no heading spelling — it can never be satisfied, and it asserts "+
				"nothing about the file", anchor.section)
			continue
		}
		for _, spelling := range anchor.spellings {
			if strings.TrimSpace(spelling) == "" {
				t.Errorf("section %q carries an empty heading spelling — `strings.Contains(body, \"\")` "+
					"is always true, so this entry turns the arm into a rubber stamp", anchor.section)
			}
		}
	}

	// The vacuum arm: a body that carries NO headings must report EVERY section.
	// A predicate that matched everything (the failure mode the guard above
	// prevents) would report none here.
	if n := len(rootReadmeMissingAnchors("")); n != len(rootReadmeAnchors) {
		t.Errorf("an empty body reports %d missing section(s), want all %d — the predicate does not "+
			"discriminate on heading text at all", n, len(rootReadmeAnchors))
	}

	// THE RED PROOF: delete one section at a time (every spelling of it) from
	// the real body and require exactly that section to be reported, and no
	// other. A gate repaired by widening to the English headings must still fail
	// when the section is genuinely removed.
	for _, anchor := range rootReadmeAnchors {
		pruned := body
		for _, spelling := range anchor.spellings {
			pruned = strings.ReplaceAll(pruned, spelling, "")
		}
		if pruned == body {
			t.Errorf("deleting the %q section changed nothing — none of its spellings (%s) occurs in "+
				"%s, so the precondition above should have caught it", anchor.section,
				anchor.quotedSpellings(), rootReadmeName)
			continue
		}
		missing := rootReadmeMissingAnchors(pruned)
		if len(missing) != 1 || missing[0].section != anchor.section {
			got := make([]string, len(missing))
			for i, m := range missing {
				got[i] = m.section
			}
			t.Errorf("deleting the %q section (%s) must leave exactly that section missing, got %d: %v",
				anchor.section, anchor.quotedSpellings(), len(missing), got)
		}
	}
}
