package standardgate

import (
	"go/ast"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/branchform"
)

// branch_force_gate.go — a shipped instruction must never teach a DESTRUCTIVE
// branch-creation spelling.
//
// WHY A GATE OVER PROSE. GOPI.md's branch-creation step is not documentation a
// reader consults; it is the instruction every agent runs. The same is true of a
// Go string literal that a briefing RENDERS into a child's prompt. A command in
// one of those is a command that gets typed — so `git worktree add <path> -B
// <branch>` there is not a stale comment, it is a one-line ref destruction
// (measured 2026-09-24, devdoc/branch-creation-forms.md):
//
//	$ git checkout -B feat/x                 # feat/x holds another task's commits
//	Switched to and reset branch 'feat/x'
//	$ git reflog show feat/x
//	34fa243 feat/x@{0}: branch: Reset to HEAD
//
// The victim's commits survive only in the reflog. The create forms (`-b`,
// `switch -c`, `worktree add -b`) need NO gate: git refuses an existing name
// loudly (`fatal: A branch named 'feat/x' already exists.`), so git is the guard.
// The force forms have no guard at all — this gate is theirs.
//
// THE PREDICATE IS NOT WRITTEN HERE. `internal/branchform` owns the vocabulary
// and the segment/flag analysis (Rule 14): the shipped surfaces and this gate
// must not drift, or the day a shape is added one side stops recognising it.
// Extending the gate to a new SHAPE (a new force flag) means adding it to that
// package and giving it its own red proof below — never adding a fifth literal
// to this test.
//
// TWO CARRIERS, because both are instruction, and both are read with the
// precision each one has:
//
//   - Go STRING LITERALS under internal/ + cmd/ (production files only): a
//     literal RENDERS into a briefing; a comment documents the code and is
//     legitimately allowed to name the forms (that is where a reviewer expects
//     to read "we never use -B here"). Precision comes from reading only
//     literals — the smoke_path_gate precedent, and the same reason.
//   - GOPI.md, read as LINES. A markdown file has no literals to distinguish
//     from prose and no comments either, so every line is instruction.
//
// WHAT IS DELIBERATELY OUT OF SCOPE, so the boundary is a decision (a gate that
// fires on prose gets switched off):
//
//   - devdoc/, briefings/, tasks/, reports/: these are HISTORY and must stay
//     able to QUOTE the destructive command — a record that cannot print its own
//     subject is not a record (the conflict-marker gate's fence rationale). A
//     briefing that PRESCRIBES one is a different matter and is filed as its own
//     defect; gating the whole directory would make the rule unfalsifiable.
//   - Test files: a fixture exists to construct the shape (the red proof below
//     does exactly that), and prodRoots excludes them for free.
//   - `git branch -D` / `-d`: deliberate, loud, named deletion — PROJECT.md Rule 9
//     prescribes it for an archived loser.
//   - `git worktree remove --force`: removes a checkout, resets no branch.
//
// The baseline is EMPTY by construction: measured at the commit the gate landed,
// no production literal and no GOPI.md line carried a verb+force-flag pair, so a
// future occurrence fails on the commit that introduces it. The inline escape
// `branch-force-ok: <reason>` exists for a site whose SUBJECT is the form (Rule
// 25's "mention is not prescription"); it stays visible in the data report, so a
// deliberate exception never becomes a quiet suppression.
func branchForceGate() gate {
	return gate{
		id:             "branch-creation force forms (no destructive spelling in a shipped instruction)",
		checklistRow:   "branch-form/destructive",
		probe:          "TestBranchForceGateRedProof",
		escape:         branchForceEscape,
		baselineReason: "EMPTY by construction: a destructive branch-creation spelling in a shipped instruction is never an acceptable state — there is nothing to permit. The rule file NAMES the banned flags without spelling a verb+flag pair, so it needs no permit either; a site whose subject IS the form carries the inline escape and stays visible in the data report.",
		baseline:       branchForceBaseline,
		find:           branchForceFind,
	}
}

// branchForceBaseline is empty BY CONSTRUCTION. Every measured site is either
// clean (the vocabulary constants in internal/tools/task_deliverable_worktree.go
// are bare flag tokens with no verb, so they are not forms) or out of the gated
// surfaces (history trees, test files). A baseline entry here would be a
// permanent exemption for an instruction that destroys a ref.
var branchForceBaseline []string

// branchForceEscape marks a line whose SUBJECT is the destructive form — the
// reviewed exception (Rule 25: mention is not prescription). It is spelled in
// one place so the gate, its red proof and its report share it (Rule 14).
const branchForceEscape = "branch-force-ok:"

// branchForceInstructionRel is the agent-facing rule file, read from the module
// root. It is the surface this gate exists for: every agent reads it at startup
// and follows its branch-creation step literally.
const branchForceInstructionRel = ProjectDocName

// branchForceGoSuffix is the one suffix whose files are read as Go literals
// rather than as lines. Everything else in the scan is text.
const branchForceGoSuffix = ".go"

// branchForceMinGoFiles is the non-vacuity floor for the Go half: a broken root
// or a filtered glob would otherwise report a clean green while reading nothing.
// Set from the measured production tree (see the report) with slack below it, so
// ordinary file churn cannot trip it but a mis-specified root does.
const branchForceMinGoFiles = 300

// branchForceMinInstructionBytes is the non-vacuity floor for the rule file: a
// failed read or a truncated file must be a failure, never a green scan of an
// empty string.
const branchForceMinInstructionBytes = 20000

// branchForceFind reports the destructive branch-creation forms in one file: per
// STRING LITERAL for a Go source, per LINE for any other text. It is the pure
// predicate the red proofs probe with a synthetic source, and the real scanner
// below is its only production caller.
func branchForceFind(g gate, p *parsed) []hit {
	if strings.HasSuffix(p.rel, branchForceGoSuffix) {
		return branchForceFindLiterals(p)
	}
	return branchForceFindLines(p)
}

// branchForceFindLines reports every destructive form on every line.
func branchForceFindLines(p *parsed) []hit {
	var out []hit
	for i, line := range strings.Split(string(p.src), "\n") {
		for _, f := range branchform.Destructive(line) {
			out = append(out, hit{
				rel:  p.rel,
				line: i + 1,
				desc: branchForceDesc(f),
			})
		}
	}
	return out
}

// branchForceFindLiterals reports a destructive form only where it is RENDERED:
// inside a string literal. A comment naming the form is a reviewer's note and is
// never reported — the distinction the smoke-path gate established, and the
// reason this half cannot be a plain line scan.
func branchForceFindLiterals(p *parsed) []hit {
	if p.file == nil {
		return nil
	}
	var out []hit
	ast.Inspect(p.file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok {
			return true
		}
		val, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true // not a string literal: not something a briefing renders
		}
		for _, f := range branchform.Destructive(val) {
			out = append(out, hit{
				rel:  p.rel,
				line: p.fset.Position(lit.Pos()).Line,
				desc: branchForceDesc(f),
			})
		}
		return true
	})
	return out
}

// branchForceDesc names one form the way a reader can act on it.
func branchForceDesc(f branchform.Form) string {
	if f.Base != "" {
		return "destructive branch creation: `git " + f.Verb + " " + f.Base + " … " + f.Flag +
			"` forces the create past an existing branch"
	}
	return "destructive branch creation: `git " + f.Verb + " " + f.Flag +
		"` resets an existing branch (reflog-only recovery)"
}

// readBranchForceInstruction reads the rule file's bytes, failing HARD on a
// missing or truncated file. A gate that read nothing reports a clean green,
// which is the defect it exists to prevent — so the read is never best-effort.
func readBranchForceInstruction(t *testing.T, root string) []byte {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(branchForceInstructionRel))
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — the shipped instruction is the surface this gate covers", branchForceInstructionRel, err)
	}
	if len(src) < branchForceMinInstructionBytes {
		t.Fatalf("%s is %d bytes (floor %d) — the scan is not looking at the real rule file",
			branchForceInstructionRel, len(src), branchForceMinInstructionBytes)
	}
	return src
}

// branchForceScanInstruction reports the forms in the rule file.
func branchForceScanInstruction(t *testing.T, root string) []hit {
	t.Helper()
	src := readBranchForceInstruction(t, root)
	return branchForceFind(gate{}, &parsed{rel: branchForceInstructionRel, src: src})
}

// readInstructionForProof returns the rule file as text for a test that asserts
// WHICH file the gate read (a same-named copy elsewhere would otherwise satisfy
// the scan while the real instruction stayed unfixed).
func readInstructionForProof(t *testing.T, root string) string {
	t.Helper()
	return string(readBranchForceInstruction(t, root))
}

// branchForceScanGo runs the gate's Go half over the real tree: every production
// file under prodRoots, read as a literal set. It returns the parsed files
// alongside the findings so the caller can apply the SAME classifier the gate
// applies (enforce/classify) — an arm that reported every raw finding would make
// the documented inline escape unreachable on the real tree while staying
// reachable in the data report, i.e. a gate that forbids correct behaviour.
func branchForceScanGo(t *testing.T, root string) (findings []hit, goFiles int, files map[string]*parsed) {
	t.Helper()
	files = map[string]*parsed{}
	for _, rel := range goFilesUnder(t, root, prodRoots, false) {
		goFiles++
		p := parseRepoFile(t, root, rel)
		files[rel] = p
		findings = append(findings, branchForceFind(gate{}, p)...)
	}
	return findings, goFiles, files
}

// assertBranchForceFixture is the SHARED red-proof body: a source text, the file
// name it is scanned under, the line the form must be reported on and the flag
// that must be named. Each shape gets its own call, so a shape added to the
// vocabulary without a proof is visible as an unproven case rather than as a
// silently greener gate.
func assertBranchForceFixture(t *testing.T, name, src string, wantLine int, wantFlag string) {
	t.Helper()
	var got []hit
	if strings.HasSuffix(name, branchForceGoSuffix) {
		fset, f := parseSource(t, name, src)
		got = branchForceFind(gate{}, &parsed{rel: name, fset: fset, file: f, src: []byte(src)})
	} else {
		got = branchForceFind(gate{}, &parsed{rel: name, src: []byte(src)})
	}
	if len(got) == 0 {
		t.Fatalf("RED PROOF FAILED for %s: the planted form was not reported — the gate is vacuous\n  source: %s", name, src)
	}
	if wantLine > 0 && got[0].line != wantLine {
		t.Fatalf("RED PROOF for %s: reported line %d, want %d (the line that carries the form)", name, got[0].line, wantLine)
	}
	if !strings.Contains(got[0].desc, wantFlag) {
		t.Fatalf("RED PROOF for %s: reported %q, which does not name %s", name, got[0].desc, wantFlag)
	}
}
