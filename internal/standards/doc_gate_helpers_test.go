package standardgate

// doc_gate_helpers_test.go — the small file/git helpers the remaining gates
// share.
//
// WHY THIS FILE EXISTS (2026-10-16, doc-pipeline removal slice 1/5): these
// helpers used to live beside the gates that used them — readFileString /
// readConfigDoc beside the CONFIG.md document gate, gitOut beside the manifest
// red proof — and those gates were deleted whole, because they were the only
// importers of the document-index and doc-class packages here. The helpers
// survive HERE because gates that are NOT about the document pipeline still
// call them: readFileString by the
// advertised-env-name gate, readConfigDoc by the CONFIG.md flag-cell gate (the
// reference document is still part of the product; only what read IT changed),
// gitOut by the removal-audit census's blob-identity arm. Deleting a caller is
// not a licence to delete a helper a survivor needs — and re-writing a second
// copy in each survivor would be the second definition Rule 13/14 forbids.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The reference document's path, relative to the module root. It is spelled
// HERE, beside the helper that opens it, because the doc gate that used to
// define it left this package in the 2026-10-16 doc-pipeline removal while
// CONFIG.md itself stayed a document of record.
const configDocName = "CONFIG.md"

// readConfigDoc reads CONFIG.md from the module root.
func readConfigDoc(t *testing.T, root string) string {
	t.Helper()
	return readFileString(t, filepath.Join(root, configDocName))
}

// readFileString reads a file or fails the test.
func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// gitOut runs git in dir and returns its TRIMMED combined output. The trim is
// what a revision-shaped reading wants; an arm that must compare a blob's own
// bytes uses its own untrimmed helper.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
