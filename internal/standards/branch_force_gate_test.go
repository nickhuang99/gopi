package standardgate

import (
	"strings"
	"testing"
)

// branch_force_gate_test.go — the red proofs for the branch-creation force-form
// gate, one per SHAPE.
//
// Each shape gets its own fixture and its own assertion, because "the gate has a
// red proof" is a claim about a shape, not about a gate: a vocabulary that grew a
// fifth force spelling while this file kept proving the first four would report
// green while being blind to the new one. The fixtures plant the form the way a
// shipped surface would carry it — a Go BRIEFING literal (rendered into a child's
// prompt), a markdown CODE SPAN (what a reader copies), and a rule-table ROW
// (where the rule file names its subject).
//
// The GREEN half of each proof matters as much: the safe spellings MUST stay
// silent, or the gate would push a future editor to delete the instruction
// instead of fixing the flag.

// TestBranchForceGateRedProof proves every destructive shape is reported and
// every safe spelling is not.
func TestBranchForceGateRedProof(t *testing.T) {
	// RED 1 — the flagship shape: a worktree created with the force flag. The
	// fixture is an interpreted Go string (a briefing literal), so it is the
	// PRE-CHANGE shape of the command GOPI.md prescribes, with -b turned into -B.
	assertBranchForceFixture(t, "probe.go",
		"package p\n\nimport \"fmt\"\n\n"+
			"func brief(b *strings.Builder) {\n"+
			"\tfmt.Fprintf(b, \"git worktree add ~/workspace/gopi-x -B feat/x\\n\")\n"+
			"}\n",
		6, "-B")

	// RED 2 — `git checkout -B`: the same reset through the porcelain verb.
	assertBranchForceFixture(t, "probe.go",
		"package p\n\nimport \"fmt\"\n\n"+
			"func brief(b *strings.Builder) {\n"+
			"\tfmt.Fprintf(b, \"git checkout -B feat/x master\\n\")\n"+
			"}\n",
		6, "-B")

	// RED 3 — `git switch -C`: the third verb, and the one whose only difference
	// from the others is a capital letter.
	assertBranchForceFixture(t, "probe.go",
		"package p\n\nimport \"fmt\"\n\n"+
			"func brief(b *strings.Builder) {\n"+
			"\tfmt.Fprintf(b, \"git switch -C feat/x master\\n\")\n"+
			"}\n",
		6, "-C")

	// RED 4 — the force-into-place pair: a SAFE create flag that a force flag
	// makes equivalent to -B. `-b` alone is refused by git and must stay silent
	// (GREEN 1 below); `-b … -f` is not.
	assertBranchForceFixture(t, "probe.go",
		"package p\n\nimport \"fmt\"\n\n"+
			"func brief(b *strings.Builder) {\n"+
			"\tfmt.Fprintf(b, \"git checkout -b feat/x -f master\\n\")\n"+
			"}\n",
		6, "-f")

	// RED 5 — `git branch -f`: the refs-only verb, which moves the ref without
	// touching a worktree. Same destruction, no checkout needed.
	assertBranchForceFixture(t, "probe.go",
		"package p\n\nimport \"fmt\"\n\n"+
			"func brief(b *strings.Builder) {\n"+
			"\tfmt.Fprintf(b, \"git branch -f feat/x master\\n\")\n"+
			"}\n",
		6, "-f")

	// RED 6 — the MARKDOWN carrier, as a code span: what GOPI.md's step looks
	// like with the force flag. This is the half a Go-only gate would miss.
	assertBranchForceFixture(t, "probe.md",
		"## Rule\n\n```bash\ngit worktree add ~/workspace/gopi-x -B feat/x\n```\n",
		4, "-B")

	// RED 7 — the markdown carrier inside a rule-table ROW, where the rule file
	// writes the banned spelling as a subject. A reader copies what is in the
	// backticks, so it is instruction wherever it sits on the line.
	assertBranchForceFixture(t, "probe.md",
		"| never | `git checkout -B <name>` | destroys the other task's ref |\n",
		1, "-B")

	// RED 8 — the SEGMENT rule, the half that keeps the gate precise: the
	// destructive flag belongs to the command it shares a segment with, so a
	// chain whose SECOND command forces files must be reported (and GREEN 3
	// below shows the first command must not be blamed for it).
	assertBranchForceFixture(t, "probe.go",
		"package p\n\nimport \"fmt\"\n\n"+
			"func brief(b *strings.Builder) {\n"+
			"\tfmt.Fprintf(b, \"git worktree add ~/workspace/wt -b feat/x && git checkout -B feat/y\\n\")\n"+
			"}\n",
		6, "-B")

	// GREEN 1 — the SAFE create flag, alone: git refuses an existing name, so
	// this is the spelling the rule file prescribes and it must never be
	// reported. A gate that reported it would make the instruction unusable.
	assertBranchForceSilent(t, "probe.go",
		"package p\n\nimport \"fmt\"\n\n"+
			"func brief(b *strings.Builder) {\n"+
			"\tfmt.Fprintf(b, \"git worktree add ~/workspace/gopi-x -b feat/x\\n\")\n"+
			"\tfmt.Fprintf(b, \"git checkout -b feat/x master\\n\")\n"+
			"\tfmt.Fprintf(b, \"git switch -c feat/x master\\n\")\n"+
			"}\n")

	// GREEN 2 — the guarded forms the rule file PRESCRIBES for archiving and
	// teardown: `git branch -D` is a loud, named, deliberate deletion, and
	// `worktree remove --force` removes a checkout without touching a branch.
	// Reporting either would make PROJECT.md Rule 9 (archive the loser) a violation.
	assertBranchForceSilent(t, "probe.go",
		"package p\n\nimport \"fmt\"\n\n"+
			"func brief(b *strings.Builder) {\n"+
			"\tfmt.Fprintf(b, \"git tag archive/x-2026 && git branch -D feat/x\\n\")\n"+
			"\tfmt.Fprintf(b, \"git worktree remove --force %s\\n\", wt)\n"+
			"}\n")

	// GREEN 3 — a force flag that is NOT in a segment with a create flag: the
	// file-restoring `git checkout -f` and the precheck's own `git rev-parse`.
	// The segment rule is what keeps this clean, and it is also what lets the
	// rule file name the banned flags in a table row that carries no verb.
	assertBranchForceSilent(t, "probe.go",
		"package p\n\nimport \"fmt\"\n\n"+
			"func brief(b *strings.Builder) {\n"+
			"\tfmt.Fprintf(b, \"git rev-parse --verify --quiet refs/heads/feat/x\\n\")\n"+
			"\tfmt.Fprintf(b, \"git checkout -f -- .\\n\")\n"+
			"\tfmt.Fprintf(b, \"git worktree list\\n\")\n"+
			"}\n")

	// GREEN 4 — the rule file's OWN table row: naming the banned FLAGS as
	// vocabulary, with no git verb beside them, must not make GOPI.md its own
	// first violation. This is the whole reason the predicate requires a verb:
	// a document must be able to say "never use -B" (and this rule does), while
	// `git checkout -b x -f` in the same shape is RED (RED 4).
	assertBranchForceSilent(t, "probe.md",
		"| never | `-B`, `-C`, `--force` | makes room destructively |\n")

	// GREEN 5 — a MARKDOWN carrier of the verb+flag pair is RED (RED 6/RED 7),
	// so a rule file can only name the pair with the reviewed inline escape. This
	// proof pins the other half of that contract: the escape is the ONLY way a
	// markdown line may carry the pair.
	escaped := "| never | `git checkout -B <name>` " + branchForceEscape + " names the banned spelling |\n"
	got := branchForceFind(gate{}, &parsed{rel: "probe.md", src: []byte(escaped)})
	if len(got) != 1 {
		t.Fatalf("the escape fixture must still be REPORTED by the predicate — the escape is "+
			"applied by the gate's classifier, so it stays visible in the data report: %+v", got)
	}
	// …and the escape is what silences it, through the same helper the gate uses.
	if !markerOn(linesAbove([]byte(escaped), got[0].line), branchForceEscape) {
		t.Fatalf("the inline escape %q did not silence the reported line", branchForceEscape)
	}
}

// assertBranchForceSilent fails when a fixture that must NOT be reported is.
func assertBranchForceSilent(t *testing.T, name, src string) {
	t.Helper()
	var got []hit
	if strings.HasSuffix(name, branchForceGoSuffix) {
		fset, f := parseSource(t, name, src)
		got = branchForceFind(gate{}, &parsed{rel: name, fset: fset, file: f, src: []byte(src)})
	} else {
		got = branchForceFind(gate{}, &parsed{rel: name, src: []byte(src)})
	}
	if len(got) != 0 {
		t.Fatalf("FALSE POSITIVE in %s: %+v\n  source: %s", name, got, src)
	}
}

// TestBranchForceGateScansTheRealTree is the non-vacuity half: the gate must
// read the real rule file and the real production tree, and the tree must be
// GREEN. A root typo or a failed read otherwise passes while seeing nothing.
func TestBranchForceGateScansTheRealTree(t *testing.T) {
	root := repoRoot(t)
	g := branchForceGate()
	findings := branchForceScanInstruction(t, root)
	got, goFiles, files := branchForceScanGo(t, root)
	if goFiles < branchForceMinGoFiles {
		t.Fatalf("read only %d production Go files (floor %d) — the gate's root is wrong, and a "+
			"gate that sees nothing reports a clean green", goFiles, branchForceMinGoFiles)
	}
	findings = append(findings, got...)
	// The instruction carrier's own parsed record, so the ONE classifier below
	// applies to it exactly as it does to the Go tree (Rule 14: the arm that
	// decides a site is escaped must be the gate's own, never a second copy).
	files[branchForceInstructionRel] = &parsed{rel: branchForceInstructionRel, src: readBranchForceInstruction(t, root)}
	var escaped int
	for _, h := range findings {
		if classify(g, h, files[h.rel]) == classEscaped {
			escaped++
			continue // reviewed inline exception: visible in the data report, not silent
		}
		t.Errorf("%s:%d carries a destructive branch-creation spelling\n  %s\n"+
			"  use %s (%s), which git refuses loudly — or mark a site whose SUBJECT is the "+
			"form with %q and a reason (devdoc/branch-creation-forms.md)",
			h.rel, h.line, h.desc, "the create forms", "branchform.FlagCreate", branchForceEscape)
	}
	t.Logf("real tree: %d production Go file(s), %d finding(s), %d escaped by the reviewed inline marker",
		goFiles, len(findings), escaped)
	// The scan must have looked at the rule file's own branch-creation step:
	// a string match on the precheck command proves the file is the ONE the
	// gate reads, not a same-named copy somewhere else.
	step := readInstructionForProof(t, root)
	if !strings.Contains(step, "rev-parse --verify --quiet refs/heads/") {
		t.Fatalf("%s does not carry the branch-name precheck — the gate is reading the wrong file, "+
			"or the instruction lost its precheck step", branchForceInstructionRel)
	}
}
