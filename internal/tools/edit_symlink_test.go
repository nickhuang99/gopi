package tools

// Symlink-aware edit tests (the 2026-08-28 settings.json incident): the old
// edit flow read through the symlink (os.ReadFile follows it) and then
// renamed onto the link path — the edit was applied to a temp file and the
// rename replaced the LINK node, so the target kept its old content while
// the edit "succeeded". The default must refuse; follow_symlink must read
// and write the target in place. All tests use t.TempDir() — never the real
// ~/.gopi/.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEdit_SymlinkRefusedByDefault verifies the default refuses with a hint
// naming the target, and that neither the link nor the target was touched.
func TestEdit_SymlinkRefusedByDefault(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "settings.json")
	link := filepath.Join(dir, "link.json")
	if err := os.WriteFile(real, []byte("old key: old value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	err := Edit(link, "old value", "new value", false)
	if err == nil {
		t.Fatal("expected error editing a symlink by default, got nil")
	}
	if !strings.Contains(err.Error(), "is a symlink") {
		t.Errorf("error should say the path is a symlink, got: %v", err)
	}
	if !strings.Contains(err.Error(), real) {
		t.Errorf("error should name the target %s, got: %v", real, err)
	}
	if !strings.Contains(err.Error(), "follow_symlink=true") {
		t.Errorf("error should offer follow_symlink=true, got: %v", err)
	}

	// The link must still be a symlink.
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Error("default edit must not replace the symlink with a regular file")
	}
	// The target must keep its old content (the silent-loss symptom).
	assertFileContent(t, real, "old key: old value\n")
}

// TestEdit_ThroughSymlinkEditsTarget verifies follow_symlink=true applies the
// edit to the target file and leaves the symlink node itself intact.
func TestEdit_ThroughSymlinkEditsTarget(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "settings.json")
	link := filepath.Join(dir, "link.json")
	if err := os.WriteFile(real, []byte("old key: old value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := EditThroughSymlink(link, "old value", "new value", false); err != nil {
		t.Fatalf("EditThroughSymlink error: %v", err)
	}

	// Target content edited.
	assertFileContent(t, real, "old key: new value\n")
	// Reading/editing through the link sees the new content too.
	assertFileContent(t, link, "old key: new value\n")
	// The link is STILL a symlink pointing at the same target.
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatal("follow_symlink=true must keep the symlink node intact")
	}
	if target, err := os.Readlink(link); err != nil || target != real {
		t.Errorf("link target changed: got %q (err %v), want %q", target, err, real)
	}
	// No staging temp files left behind.
	assertNoStagingFiles(t, dir)
}

// TestEdit_RegularPathUnaffected is the regression guard: a non-symlink path
// behaves exactly as before in both modes.
func TestEdit_RegularPathUnaffected(t *testing.T) {
	path := writeEditFixture(t, "alpha beta\n")

	if err := Edit(path, "beta", "gamma", false); err != nil {
		t.Fatalf("Edit error: %v", err)
	}
	assertFileContent(t, path, "alpha gamma\n")

	if err := EditThroughSymlink(path, "gamma", "delta", false); err != nil {
		t.Fatalf("EditThroughSymlink error: %v", err)
	}
	assertFileContent(t, path, "alpha delta\n")

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("regular file must not become a symlink")
	}
}

// TestEdit_ThroughSymlinkPreservesMode verifies follow-mode edits preserve
// the target file's permissions (the rename must apply the target's mode).
func TestEdit_ThroughSymlinkPreservesMode(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "secret.key")
	link := filepath.Join(dir, "link.key")
	if err := os.WriteFile(real, []byte("token: abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := EditThroughSymlink(link, "abc", "xyz", false); err != nil {
		t.Fatalf("EditThroughSymlink error: %v", err)
	}
	assertFileContent(t, real, "token: xyz\n")

	info, err := os.Stat(real)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600 (preserved through symlink edit)", info.Mode().Perm())
	}
}
