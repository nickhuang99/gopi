package standardgate

// smoke_path_gate_test.go — the merge/verify path must not PRESCRIBE the
// whole-tree suite to the model it briefs.
//
// WHY A GATE OVER PROSE (the merger_takeover_prompt_gate precedent). The
// merge/verify path's deliverables are largely TEXT: the briefing internal/tools
// renders for a rework child / verify-retry child, and the take-over prompt
// cmd/agent embeds for every merger tick. That text is an INSTRUCTION a model
// follows — so a whole-tree command in it is not a stale comment, it is a
// command that gets run: measured on this repo, a whole-tree suite on the
// merge/verify path costs minutes of real fixture work per child while the tick
// holds the merge lock, and it is exactly what the heavy/smoke classification
// (heavytest + testdata/heavy-tests.tsv) retired.
//
// WHAT IS REPORTED. On the merge/verify path, the whole-tree command spelled as
// a runnable command span — smokePathCmdSpanDelim + smokegate.WholeTreeNeedle +
// smokePathCmdSpanDelim ("the thing a reader reads as an instruction"). Two
// carriers are scanned, because both are instruction:
//
//   - Go string literals under internal/tools (the merger gate + the briefing
//     renderers) and cmd/agent. Only LITERALS are
//     read, never comments: a comment documents the repository, a literal
//     RENDERS into a child's briefing. That distinction is what keeps the
//     gate precise — internal/tools legitimately names a whole-tree run in a
//     comment (the merge path's scope rule lives in internal/smokegate, and the
//     comment in `merge_pass_verify.go` that used to say the suite is
//     deliberately NOT part of the merge path went with that file on 2026-11-01)
//     and a comment is the right place for that.
//   - the shipped briefing artifact cmd/agent/merger_takeover_prompt.md, WHEN IT
//     IS THERE. It was the retired daemon's take-over prompt and was deleted
//     with it (epic merger-delete-dead-impl, 2026-09-19); the scan below gates
//     it on presence, so a briefing that lands at that path is covered from its
//     first commit rather than after someone remembers to re-register it.
//
// WHAT IS DELIBERATELY OUT OF SCOPE, with the reason, so this scope is a
// decision rather than an oversight:
//
//   - the merge path's own comments name the whole-tree command to say it is
//     NOT what runs here, and `devdoc/` records the retired baseline sweep.
//     What was retired is prescribing it to the JUDGING child; a comment that
//     documents why the suite is out of the merge path is its own case.
//   - `make test-heavy` and the standalone `go test ./...` a developer types
//     are legitimate — heavy runs are the deliberate off-peak opt-in
//     (PROJECT.md Rule 27). The gate is scoped to briefings, not to the repo.
//   - devdoc/, briefings/, tasks/ and bugs/ are HISTORY: they must stay able to
//     QUOTE the command that was retired (a report that cannot quote its
//     subject is not a record). Gating them would make the record
//     unfalsifiable, so they are named here as excluded on purpose.
//
// The gate lands with an EMPTY permit list: the three sites that carried the
// prescription (verify_gate.go's rework child, verify_gate.go's verify-retry
// child, merger_takeover_prompt.md §3.8) were rewritten to the smoke command in
// the same change, so a future re-inline fails on the commit that introduces it.
// The prompt (the third site, and the only one under cmd/agent) was itself
// deleted with the retired daemon — the permit list stays empty either way.

import (
	"go/ast"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/smokegate"
)

const (
	// smokePathToolsRoot holds the merger gate and the briefing renderers.
	smokePathToolsRoot = "internal/tools"
	// smokePathCmdRoot holds the take-over launcher and its prompt artifact.
	smokePathCmdRoot = "cmd/agent"
	// smokePathTakeoverPromptRel is the shipped, LLM-facing briefing artifact
	// the retired daemon's take-over handed to a child gopi. Optional since the
	// daemon was deleted (epic merger-delete-dead-impl, 2026-09-19): see the
	// scan below, which gates it only when the file is there.
	smokePathTakeoverPromptRel = smokePathCmdRoot + "/merger_takeover_prompt.md"
	// smokePathCmdSpanDelim is the quoting that turns a mention into an
	// instruction — a markdown inline code span, which is how every command in
	// these briefings (and in the prompt artifact) is written.
	smokePathCmdSpanDelim = "`"
	// smokePathMinGoFiles is the non-vacuity floor: a gate that read no file (a
	// broken root, a filtered glob) would report a clean green while seeing
	// nothing. Set from the measured tree (see the report) with slack below it,
	// so ordinary file churn cannot trip it but a mis-specified root does.
	smokePathMinGoFiles = 60
)

// smokePathPrescription is one reported prescription.
type smokePathPrescription struct {
	rel    string // repo-relative slash path
	line   int    // 1-based
	source string // the offending text, trimmed
}

// smokeCmdSpan is the text this gate looks for: the whole-tree command as a
// quoted command span. Built from smokegate's constant (Rule 14: one spelling
// of the command, shared with the text the briefings DO prescribe).
func smokeCmdSpan() string {
	return smokePathCmdSpanDelim + smokegate.WholeTreeNeedle + smokePathCmdSpanDelim
}

// smokePathStringLiterals returns the unquoted value of every string literal in
// one parsed file, with its 1-based line. Comments are not literals and are
// never returned — the whole precision of this gate rests on that line.
func smokePathStringLiterals(p *parsed) []struct {
	line int
	val  string
} {
	var out []struct {
		line int
		val  string
	}
	ast.Inspect(p.file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok {
			return true
		}
		val, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true // not a string literal (number/char): not a briefing
		}
		out = append(out, struct {
			line int
			val  string
		}{line: p.fset.Position(lit.Pos()).Line, val: val})
		return true
	})
	return out
}

// smokePathFind reports every quoted whole-tree prescription in one parsed Go
// file. It is the predicate the red-proof probes with a synthetic source.
func smokePathFind(p *parsed) []smokePathPrescription {
	var out []smokePathPrescription
	for _, lit := range smokePathStringLiterals(p) {
		if !strings.Contains(lit.val, smokeCmdSpan()) {
			continue
		}
		out = append(out, smokePathPrescription{rel: p.rel, line: lit.line, source: smokeCmdSpan()})
	}
	return out
}

// smokePathFindInMarkdown reports every quoted whole-tree prescription in a
// markdown briefing artifact. A markdown file has no literals to distinguish
// from prose, but it also has no comments — every match IS instruction.
func smokePathFindInMarkdown(rel, src string) []smokePathPrescription {
	var out []smokePathPrescription
	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, smokeCmdSpan()) {
			out = append(out, smokePathPrescription{rel: rel, line: i + 1, source: strings.TrimSpace(line)})
		}
	}
	return out
}

// smokePathScan runs the gate over the real tree: every production Go file under
// the merge/verify path's roots, plus the shipped take-over prompt.
func smokePathScan(t *testing.T) (findings []smokePathPrescription, goFiles int) {
	t.Helper()
	root := repoRoot(t)
	for _, rel := range goFilesUnder(t, root, []string{smokePathToolsRoot, smokePathCmdRoot}, false) {
		goFiles++
		findings = append(findings, smokePathFind(parseRepoFile(t, root, rel))...)
	}
	// THE ARTIFACT HALF IS OPTIONAL (epic merger-delete-dead-impl,
	// 2026-09-19): the take-over prompt was the retired daemon's, and it was
	// deleted with it. A missing artifact is not a pass that hides a
	// prescription — it is a subject that no longer ships. An artifact that IS
	// present is gated exactly as before, so a future briefing that lands at
	// this path is covered from its first commit, not after someone remembers to
	// re-add it here (Rule 15: the check follows the file, not a checklist).
	promptPath := filepath.Join(root, filepath.FromSlash(smokePathTakeoverPromptRel))
	if prompt, err := os.ReadFile(promptPath); err == nil {
		findings = append(findings, smokePathFindInMarkdown(smokePathTakeoverPromptRel, string(prompt))...)
	} else if !os.IsNotExist(err) {
		// A permission/IO failure is NOT the same as an absent artifact: it means
		// the gate could not look at a file that is there, which must be reported
		// rather than read as "nothing to see" (Rule 15).
		t.Fatalf("read %s: %v", smokePathTakeoverPromptRel, err)
	}
	return findings, goFiles
}

// TestSmokePathPrescribesNoWholeTreeSuite is the gate. It fails on any
// whole-tree command span in a merge/verify-path briefing, and it fails when it
// read too few files to have looked at all (non-vacuity).
func TestSmokePathPrescribesNoWholeTreeSuite(t *testing.T) {
	findings, goFiles := smokePathScan(t)
	if goFiles < smokePathMinGoFiles {
		t.Fatalf("the gate read only %d production Go files under %s + %s (floor %d) — its roots are "+
			"wrong, and a gate that sees nothing reports a clean green",
			goFiles, smokePathToolsRoot, smokePathCmdRoot, smokePathMinGoFiles)
	}
	for _, f := range findings {
		t.Errorf("%s:%d prescribes the whole-tree suite on the merge/verify path\n"+
			"  %s\n"+
			"  use smokegate.Command (%q) scoped to the touched packages — %s (%s)",
			f.rel, f.line, f.source, smokegate.Command, smokegate.ScopedCommand, smokegate.Doc)
	}
}

// TestSmokePathGateRedProof proves the predicate reports the shape it exists
// for, stays silent on the shapes it deliberately excludes (a comment, and the
// smoke command itself), and finds the briefing renderers in the real tree.
func TestSmokePathGateRedProof(t *testing.T) {
	// RED: a briefing that renders the whole-tree command into a child's text.
	// This is the PRE-CHANGE TEXT of verify_gate.go's rework child, which is
	// what the gate was written against (an interpreted string, not a raw one,
	// so the snippet can carry the backtick command spans verbatim).
	violating := "package p\n\nimport \"fmt\"\n\n" +
		"func fileRework(b *strings.Builder) {\n" +
		"\tfmt.Fprintf(b, \"run the project's own verification there (`go build ./...` and the \"+\n" +
		"\t\t\"tests the original fix touched — `go test ./...` for the whole tree).\\n\")\n" +
		"}\n"
	fset, f := parseSource(t, "probe.go", violating)
	got := smokePathFind(&parsed{rel: "probe.go", fset: fset, file: f, src: []byte(violating)})
	if len(got) == 0 {
		t.Fatalf("RED PROOF FAILED: the retired rework-child text was not reported — the gate is vacuous")
	}
	if got[0].line != 7 {
		t.Fatalf("RED PROOF: reported line %d, want 7 (the Fprintf that renders it)", got[0].line)
	}

	// GREEN: the smoke command as the briefings now spell it, plus a COMMENT
	// naming the whole-tree command (documenting where the suite legitimately
	// runs is legitimate and must not be reported). Interpreted strings again,
	// because the snippet must carry backtick command spans.
	allowed := "package p\n\nimport \"fmt\"\n\n" +
		"// the merge path never runs a `go test ./...`.\n" +
		"func smoke(b *strings.Builder) {\n" +
		"\tfmt.Fprintf(b, \"run `make test` scoped to what you touched — never the whole tree.\\n\")\n" +
		"}\n"
	fset2, f2 := parseSource(t, "probe.go", allowed)
	if got := smokePathFind(&parsed{rel: "probe.go", fset: fset2, file: f2, src: []byte(allowed)}); len(got) != 0 {
		t.Fatalf("FALSE POSITIVE on the smoke command / a comment: %+v", got)
	}

	// GREEN: an unquoted mention is prose, not a command a reader would run —
	// the repo's own GOPI.md and reports talk about it constantly.
	prose := "package p\n\nimport \"fmt\"\n\n" +
		"func doc(b *strings.Builder) {\n" +
		"\tfmt.Fprintf(b, \"the whole-tree go test ./... suite is what the baseline sweep runs.\\n\")\n" +
		"}\n"
	fset3, f3 := parseSource(t, "probe.go", prose)
	if got := smokePathFind(&parsed{rel: "probe.go", fset: fset3, file: f3, src: []byte(prose)}); len(got) != 0 {
		t.Fatalf("FALSE POSITIVE on an unquoted mention: %+v", got)
	}

	// RED (markdown): the artifact form — §3.8's shape before this change.
	if got := smokePathFindInMarkdown("prompt.md", "Run `go test ./...` before you call it done.\n"); len(got) != 1 {
		t.Fatalf("markdown carrier not reported: %+v", got)
	}

	// THE REAL TREE: the gate must find the renderers it gates. A root typo
	// would otherwise make the whole gate pass while reading nothing.
	findings, goFiles := smokePathScan(t)
	if goFiles < smokePathMinGoFiles {
		t.Fatalf("scanned %d production Go files, floor %d", goFiles, smokePathMinGoFiles)
	}
	if len(findings) != 0 {
		t.Fatalf("the real tree is RED: %+v", findings)
	}
}
