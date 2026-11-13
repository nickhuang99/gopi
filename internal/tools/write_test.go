package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "hello, world"

	if err := Write(path, content); err != nil {
		t.Fatalf("Write(%q, %q) error: %v", path, content, err)
	}

	// Read back and verify.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error: %v", path, err)
	}
	if string(got) != content {
		t.Fatalf("expected %q, got %q", content, string(got))
	}
}

func TestWriteReadBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readback.txt")
	content := "multi\nline\ncontent\n"

	if err := Write(path, content); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile error: %v", err)
	}
	if string(got) != content {
		t.Fatalf("expected %q, got %q", content, string(got))
	}

	// Overwrite and verify.
	updated := "overwritten content"
	if err := Write(path, updated); err != nil {
		t.Fatalf("Write (overwrite) error: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile after overwrite error: %v", err)
	}
	if string(got) != updated {
		t.Fatalf("expected %q after overwrite, got %q", updated, string(got))
	}
}

func TestWriteCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "deep.txt")
	content := "deeply nested"

	if err := Write(path, content); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile error: %v", err)
	}
	if string(got) != content {
		t.Fatalf("expected %q, got %q", content, string(got))
	}
}

func TestWriteEmptyPath(t *testing.T) {
	err := Write("", "anything")
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestWriteDirectoryRejection(t *testing.T) {
	dir := t.TempDir()
	err := Write(dir, "content")
	if err == nil {
		t.Fatal("expected error when path is a directory, got nil")
	}
}

func TestWriteTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	dir := t.TempDir()

	// Override HOME so ~/ resolves to our temp dir.
	t.Setenv("HOME", dir)

	path := "~/tilde-test.txt"
	content := "tilde expanded"

	if err := Write(path, content); err != nil {
		t.Fatalf("Write(%q, %q) error: %v", path, content, err)
	}

	expectedPath := filepath.Join(dir, "tilde-test.txt")
	got, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error: %v", expectedPath, err)
	}
	if string(got) != content {
		t.Fatalf("expected %q, got %q", content, string(got))
	}

	_ = home // suppress unused warning
}

// assertNoStagingFiles fails the test if any .gopi-tmp staging file remains
// in dir — the atomic write must always clean up, on success and on failure.
func assertNoStagingFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".gopi-tmp-") {
			t.Errorf("staging file not cleaned up: %s", filepath.Join(dir, e.Name()))
		}
	}
}

// TestWrite_AtomicSuccessNoResidue verifies a successful atomic write lands
// the full content and leaves no staging file behind.
func TestWrite_AtomicSuccessNoResidue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := "atomic content\nwith two lines\n"

	if err := Write(path, content); err != nil {
		t.Fatalf("Write(%q) error: %v", path, err)
	}
	assertNoStagingFiles(t, dir)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile error: %v", err)
	}
	if string(got) != content {
		t.Fatalf("expected %q, got %q", content, string(got))
	}
}

// TestWrite_UnwritableDirErrors verifies that an unwritable target directory
// produces a permission error (with hint) and no staging residue. Skipped as
// root — root bypasses permission checks.
func TestWrite_UnwritableDirErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	//stylegate:ignore sets a fixture file's mode; the mode read back is the assertion
	defer func() { _ = os.Chmod(dir, 0o755) }()

	err := Write(filepath.Join(dir, "x.txt"), "content")
	if err == nil {
		t.Fatal("expected error writing into an unwritable dir, got nil")
	}
	if !strings.Contains(err.Error(), "permission") {
		t.Errorf("error should mention permission, got: %v", err)
	}
	assertNoStagingFiles(t, dir)
}

// TestWrite_RenameFailureCleansTemp drives the post-staging failure path
// deterministically: atomicWriteFile onto an existing DIRECTORY (bypassing
// writeImpl's earlier dir check) stages a temp file and then fails at the
// rename — the deferred cleanup must remove the staging file.
func TestWrite_RenameFailureCleansTemp(t *testing.T) {
	parent := t.TempDir()
	targetDir := filepath.Join(parent, "target-dir")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := atomicWriteFile(targetDir, "content", false)
	if err == nil {
		t.Fatal("expected error renaming a file over a directory, got nil")
	}
	assertNoStagingFiles(t, parent)
}

// TestWrite_OverwritePreservesMode verifies that overwriting an existing file
// keeps its permissions (os.WriteFile semantics preserved by the atomic path,
// which would otherwise reset the mode to the temp file's 0600).
func TestWrite_OverwritePreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mode.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Write(path, "new"); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	assertNoStagingFiles(t, dir)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600 (preserved on overwrite)", info.Mode().Perm())
	}
	got, _ := os.ReadFile(path) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}
}
