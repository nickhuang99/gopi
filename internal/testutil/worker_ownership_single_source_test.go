package testutil

// ANTI-DRIFT GATE for the worker-ownership pointer's SINGLE SOURCE OF TRUTH
// (Rule 13/14: a constant that is defined but bypassed by bare literals is a
// defect, and a shared value lives in exactly ONE small exported-constant leaf
// package).
//
// THE DEFECT THIS CLOSES. The pointer's identity is three values — the marker
// token, the canonical document's path, and the command shapes that make a
// document a trigger. Both faces of the write-time fix depend on them agreeing:
//
//	the WRITER (internal/tools write/edit) annotates a document when it
//	   matches workerownership.Needles, and emits workerownership.Line
//	the GATE   (worker_ownership_gate_test.go) flags a document when it
//	   matches the SAME needles and demands the SAME line
//
// If either face carries its own copy of a value, the pair drifts silently in
// exactly one direction: the writer emits something the gate does not accept,
// or the gate flags a shape the writer never learned — which is the per-round
// master red this task exists to end, re-created by a rename.
//
// THE CHECK IS RELATIONAL, NOT A BLANKET LITERAL BAN, and that distinction is
// deliberate. A gate that forbade the marker string anywhere outside the leaf
// package would redden on the ~40 documents and tests whose SUBJECT is the
// pointer line itself (it is quoted in the canonical block, in the gate's own
// help text, in the documents that carry it, and in every fixture that asserts
// one). What must be impossible is a SECOND DEFINITION — another constant, in
// another package, holding the same value. So this file asserts the values are
// DEFINED ONCE and that the definitions agree, which is the failure mode the
// hardcoding review actually found (a constant mirrored per package, drifting
// apart one rename at a time).
//
// The sizes are small on purpose: five greps over the production trees, no
// repository walk, no build.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/workerownership"
)

// adrDefinedOnce asserts that a string value is defined as a constant in
// EXACTLY one production file — the leaf package — no matter how many files
// mention it.
func adrDefinedOnce(t *testing.T, value, wantFile string) {
	t.Helper()
	definers := adrDefiners(t, value)
	if len(definers) == 0 {
		t.Fatalf("no production file defines %q — the value must live in %s", value, wantFile)
	}
	for _, rel := range definers {
		if rel != wantFile {
			t.Errorf("%s also defines %q as a constant — the value has ONE home (%s); a second definition is the drift this gate refuses (Rule 13/14)",
				rel, value, wantFile)
		}
	}
}

// adrDefiners returns every PRODUCTION file (never a _test.go) that binds a
// constant to the exact string value, repo-root-relative.
func adrDefiners(t *testing.T, value string) []string {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	var definers []string
	fset := token.NewFileSet()
	for _, dir := range []string{"internal", "cmd"} {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, parseErr := parser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				return parseErr
			}
			ast.Inspect(f, func(n ast.Node) bool {
				vs, ok := n.(*ast.ValueSpec)
				if !ok {
					return true
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					if strings.Trim(lit.Value, "`\"") == value {
						rel, relErr := filepath.Rel(root, path)
						if relErr == nil {
							definers = append(definers, filepath.ToSlash(rel))
						}
					}
				}
				return true
			})
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", dir, walkErr)
		}
	}
	return definers
}

// TestWorkerOwnershipValuesAreDefinedOnce is the drift gate: each of the shared
// values has ONE production definition, in the leaf package.
func TestWorkerOwnershipValuesAreDefinedOnce(t *testing.T) {
	const leafFile = "internal/workerownership/workerownership.go"
	adrDefinedOnce(t, workerownership.Marker, leafFile)
	adrDefinedOnce(t, workerownership.CanonicalRel, leafFile)

	// The needles are BUILT in the leaf package from internal/procname's
	// constants (never typed whole), so the assertion for them is scoped to the
	// two faces that DECIDE with them — the gate's package and the writer's
	// package. A blanket repo-wide ban would redden on unrelated legitimate
	// constants that merely render the same text for logs (cmd/agent's
	// `taskProgName = "gopi task run"` is a log label, not a matcher), and a
	// gate that cries wolf gets deleted. Inside the deciding packages, a
	// re-typed needle is exactly the drift: one side learns a shape the other
	// does not.
	for _, needle := range workerownership.Needles {
		for _, rel := range adrDefiners(t, needle) {
			if !strings.HasPrefix(rel, "internal/testutil/") && !strings.HasPrefix(rel, "internal/tools/") {
				continue
			}
			t.Errorf("%s defines the needle %q as its own constant — it must come from internal/workerownership.Needles (built from internal/procname), or the matcher and the writer drift", rel, needle)
		}
	}
}

// TestWorkerOwnershipGateAliasesTheLeafPackage is the second half: the gate
// must USE the leaf package's values rather than re-declaring them, and the
// gate's own deciding functions must be the leaf package's predicates. A gate
// that kept its own copy of the marker would pass the "defined once" check
// while still comparing against a different string.
func TestWorkerOwnershipGateAliasesTheLeafPackage(t *testing.T) {
	// The aliases the gate declares must resolve to the leaf package's values.
	for _, c := range []struct {
		name, got, want string
	}{
		{"workerOwnershipCanonicalRel", workerOwnershipCanonicalRel, workerownership.CanonicalRel},
		{"workerOwnershipMarker", workerOwnershipMarker, workerownership.Marker},
		{"workerOwnershipTaskRunSubverb", workerOwnershipTaskRunSubverb, workerownership.TaskRunSubverb},
		{"workerOwnershipAuditedDocSuffix", workerOwnershipAuditedDocSuffix, workerownership.SuffixDoc},
		{"workerOwnershipAuditedShSuffix", workerOwnershipAuditedShSuffix, workerownership.SuffixSh},
		{"workerOwnershipAuditedTxtSuffix", workerOwnershipAuditedTxtSuffix, workerownership.SuffixPlain},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, but the leaf package says %q — the gate and the writer have drifted", c.name, c.got, c.want)
		}
	}
	if len(workerOwnershipVerbNeedles) != len(workerownership.Needles) {
		t.Fatalf("the gate matches %d needles, the writer %d — a shape only one side knows is exactly the per-round red",
			len(workerOwnershipVerbNeedles), len(workerownership.Needles))
	}
	for i, n := range workerownership.Needles {
		if workerOwnershipVerbNeedles[i] != n {
			t.Errorf("needle %d: gate %q, writer %q", i, workerOwnershipVerbNeedles[i], n)
		}
	}
	// Audited scope: the walk and the injection must answer the same question.
	for _, suffix := range workerownership.AuditedSuffixes() {
		if !workerownership.Audited(suffix) {
			t.Errorf("Audited(%q) is false for a suffix in AuditedSuffixes()", suffix)
		}
	}
	for _, root := range workerownership.AuditedRoots() {
		if !workerownership.InAuditedTree(filepath.Join(root, "x"+workerownership.SuffixDoc)) {
			t.Errorf("InAuditedTree does not recognise an audited root %q", root)
		}
	}
}
