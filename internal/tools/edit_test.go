package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEdit_StrictUniqueRejectsMultiple(t *testing.T) {
	path := writeEditFixture(t, "foo foo\n")

	err := Edit(path, "foo", "bar", false)
	if err == nil {
		t.Fatal("expected error for non-unique oldStr, got nil")
	}
	if !strings.Contains(err.Error(), "unique") {
		t.Errorf("error should mention uniqueness, got: %v", err)
	}
	// The file must be untouched after the refused edit.
	assertFileContent(t, path, "foo foo\n")
}

func TestEdit_StrictUniqueSingleMatch(t *testing.T) {
	path := writeEditFixture(t, "foo bar\n")

	if err := Edit(path, "foo", "baz", false); err != nil {
		t.Fatalf("Edit single match: %v", err)
	}
	assertFileContent(t, path, "baz bar\n")
}

func TestEdit_ReplaceAll(t *testing.T) {
	path := writeEditFixture(t, "foo foo\nfoo bar\n")

	if err := Edit(path, "foo", "baz", true); err != nil {
		t.Fatalf("Edit replace_all: %v", err)
	}
	assertFileContent(t, path, "baz baz\nbaz bar\n")
}

func TestEdit_ReplaceAllNotFoundStillErrors(t *testing.T) {
	path := writeEditFixture(t, "nothing here\n")

	// count==0 errors even with replace_all=true (oldStr not found).
	err := Edit(path, "missing", "x", true)
	if err == nil {
		t.Fatal("expected error for missing oldStr with replace_all, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestEdit_NoopRejected(t *testing.T) {
	path := writeEditFixture(t, "foo bar\n")

	// oldStr == newStr is a no-op edit — refused in both modes.
	err := Edit(path, "foo", "foo", false)
	if err == nil {
		t.Fatal("expected error for oldStr == newStr, got nil")
	}
	if !strings.Contains(err.Error(), "must differ") {
		t.Errorf("error should mention 'must differ', got: %v", err)
	}
	if err := Edit(path, "foo", "foo", true); err == nil {
		t.Fatal("expected error for oldStr == newStr with replace_all, got nil")
	}
	assertFileContent(t, path, "foo bar\n")
}

func TestEdit_ReplaceAllDefaultFalseUnchanged(t *testing.T) {
	// Regression guard: replaceAll=false must behave exactly like the old
	// strict-unique Edit — replace_all is purely additive.
	path := writeEditFixture(t, "unique token here\n")

	if err := Edit(path, "unique", "only", false); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	assertFileContent(t, path, "only token here\n")
}

// TestEdit_PreservesFileMode guards the permission regression: editing a
// restricted file (0600) must NOT reset its mode to the hardcoded 0644 used
// by the old non-atomic os.WriteFile(path, …, 0644). Editing a private key
// and having it become world-readable is a security bug.
func TestEdit_PreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("old secret"), 0o600); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod fixture: %v", err)
	}

	if err := Edit(path, "old", "new", false); err != nil {
		t.Fatalf("Edit restricted file: %v", err)
	}
	assertFileContent(t, path, "new secret")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat edited file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("edited file mode = %o, want 600 (permission reset regression?)", got)
	}
}

// TestEdit_EmptyNewStrRefused — the silent-content-loss shape, measured
// 2026-10-04 (task 01a103c2-36e2-7808-bc42-d652762c4691): a caller whose
// replacement arrived as "" reached strings.Replace and the matched line was
// DELETED while the call reported success. An edit whose replacement is empty
// is a deletion wearing an edit's success message, and it is refused at this
// level too — the invariant belongs to the operation, not to one caller.
func TestEdit_EmptyNewStrRefused(t *testing.T) {
	const content = "line-A\n| anchor-row | keep me |\nline-C\n"
	path := writeEditFixture(t, content)

	err := Edit(path, "| anchor-row | keep me |", "", false)
	if err == nil {
		t.Fatal("an empty newStr was performed: the matched line would be DELETED and the call would report success")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("the refusal should name the empty replacement, got: %v", err)
	}
	assertFileContent(t, path, content)

	// replace_all is not an escape from it either.
	if err := Edit(path, "| anchor-row | keep me |", "", true); err == nil {
		t.Fatal("replace_all performed an empty replacement")
	}
	assertFileContent(t, path, content)
}

// TestEdit_MultilineNewStrKeepsTheMatchedLine is the minimal regression sample
// of the same report: a SINGLE-LINE oldStr with a MULTI-LINE newStr must land
// the new content AND keep the matched line — the file grows by one line.
func TestEdit_MultilineNewStrKeepsTheMatchedLine(t *testing.T) {
	path := writeEditFixture(t, "line-A\n| anchor-row | keep me |\nline-C\n")

	if err := Edit(path, "| anchor-row | keep me |", "| NEW-ROW | added |\n| anchor-row | keep me |", false); err != nil {
		t.Fatalf("Edit single-line oldStr with a multi-line newStr: %v", err)
	}
	assertFileContent(t, path, "line-A\n| NEW-ROW | added |\n| anchor-row | keep me |\nline-C\n")
}

// writeEditFixture creates a temp file with the given content and returns its
// path.
func writeEditFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "edit.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create fixture: %v", err)
	}
	return path
}

// assertFileContent fails the test unless path contains exactly want.
func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", path, err)
	}
	if string(got) != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}
