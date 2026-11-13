package tools

// Symlink-aware write tests (the 2026-08-28 settings.json incident): the
// atomic write's final os.Rename replaces the symlink NODE with a regular
// file — the link is corrupted and the target keeps its old content. The
// default must refuse; follow_symlink must write the target in place and
// keep the link. All tests use t.TempDir() — never the real ~/.gopi/.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWrite_SymlinkRefusedByDefault verifies the default refuses with a hint
// naming the target, and that neither the link nor the target was touched.
func TestWrite_SymlinkRefusedByDefault(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "settings.json")
	link := filepath.Join(dir, "link.json")
	if err := os.WriteFile(real, []byte("old config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	err := Write(link, "new config\n")
	if err == nil {
		t.Fatal("expected error writing to a symlink by default, got nil")
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

	// The link must still be a symlink (not replaced by a regular file).
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Error("default write must not replace the symlink with a regular file")
	}
	// And the target must keep its old content (the silent-loss symptom).
	assertFileContent(t, real, "old config\n")
}

// TestWrite_ThroughSymlinkWritesTarget verifies follow_symlink=true updates
// the target's content and leaves the symlink node itself intact.
func TestWrite_ThroughSymlinkWritesTarget(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "settings.json")
	link := filepath.Join(dir, "link.json")
	if err := os.WriteFile(real, []byte("old config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := WriteThroughSymlink(link, "new config\n"); err != nil {
		t.Fatalf("WriteThroughSymlink error: %v", err)
	}

	// Target content updated.
	assertFileContent(t, real, "new config\n")
	// Reading through the link sees the new content too.
	assertFileContent(t, link, "new config\n")
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

// TestWrite_RegularPathUnaffected is the regression guard: a non-symlink
// path behaves exactly as before in both modes.
func TestWrite_RegularPathUnaffected(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.txt")

	if err := Write(plain, "v1\n"); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	assertFileContent(t, plain, "v1\n")

	if err := Write(plain, "v2\n"); err != nil {
		t.Fatalf("Write overwrite error: %v", err)
	}
	assertFileContent(t, plain, "v2\n")

	// follow=true on a regular path is a no-op distinction.
	if err := WriteThroughSymlink(plain, "v3\n"); err != nil {
		t.Fatalf("WriteThroughSymlink error: %v", err)
	}
	assertFileContent(t, plain, "v3\n")

	info, err := os.Lstat(plain)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("regular file must not become a symlink")
	}
}

// TestWrite_BrokenSymlinkFollowErrors verifies follow_symlink=true on a
// dangling link surfaces the resolution error instead of silently replacing
// the dead node with a regular file.
func TestWrite_BrokenSymlinkFollowErrors(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "missing-target"), link); err != nil {
		t.Fatal(err)
	}

	if err := Write(link, "x"); err == nil {
		t.Fatal("expected error for dangling symlink by default, got nil")
	}
	err := WriteThroughSymlink(link, "x")
	if err == nil {
		t.Fatal("expected error following a dangling symlink, got nil")
	}
	if !strings.Contains(err.Error(), "cannot resolve symlink") {
		t.Errorf("error should mention resolution failure, got: %v", err)
	}
	// The dangling link node must survive both attempts.
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Error("broken symlink must not be replaced by a regular file")
	}
}
