package tools

// Symlink-aware read tests (2026-08-28 settings.json incident): reading a
// symlink path must follow the link (correct behavior) but the result must
// note the link target so the model knows the path is a link before editing
// or writing it. All tests use t.TempDir() — never the real ~/.gopi/.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRead_SymlinkNotesTarget verifies a full read of a symlink path returns
// the target's content plus the hint line naming the link and its target.
func TestRead_SymlinkNotesTarget(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "settings.json")
	link := filepath.Join(dir, "link.json")
	if err := os.WriteFile(real, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	got, err := Read(link, 0, 0)
	if err != nil {
		t.Fatalf("Read(symlink) error: %v", err)
	}
	if !strings.Contains(got, "hello world") {
		t.Errorf("read should follow the link to the target content, got: %q", got)
	}
	wantHint := fmt.Sprintf("[symlink: %s -> %s]", link, real)
	if !strings.Contains(got, wantHint) {
		t.Errorf("read should note the symlink target, want %q in: %q", wantHint, got)
	}

	// The read must not have touched the link itself.
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Error("read must leave the symlink intact")
	}
}

// TestRead_SymlinkHintBeforeTruncationFooter verifies the hint line sits
// between the content and any truncation footer on a byte-budget-truncated
// paged read (a line-limited page ends without a footer, so this crosses
// maxPageReadBytes to force the footer).
func TestRead_SymlinkHintBeforeTruncationFooter(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	var content strings.Builder
	// 600 lines × ~100 bytes ≈ 60KB > the 50KB page budget → footer.
	for i := 1; i <= 600; i++ {
		fmt.Fprintf(&content, "line %03d: %s\n", i, strings.Repeat("x", 90))
	}
	if err := os.WriteFile(real, []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	got, err := Read(link, 0, 600)
	if err != nil {
		t.Fatalf("Read(symlink, offset=0, limit=600) error: %v", err)
	}
	idxHint := strings.Index(got, "[symlink: ")
	idxFooter := strings.Index(got, "[read truncated:")
	if idxHint < 0 {
		t.Fatalf("symlink hint missing in paged read: %q", got)
	}
	if idxFooter < 0 {
		t.Fatalf("truncation footer missing in paged read: %q", got)
	}
	if idxHint > idxFooter {
		t.Errorf("symlink hint (at %d) must precede the truncation footer (at %d)", idxHint, idxFooter)
	}
}

// TestRead_RegularFileNoSymlinkHint is the regression guard: a plain file's
// read must be byte-identical to before (no hint line injected).
func TestRead_RegularFileNoSymlinkHint(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(plain, []byte("just text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Read(plain, 0, 0)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if strings.Contains(got, "[symlink:") {
		t.Errorf("regular file must not gain a symlink hint, got: %q", got)
	}
	if got != "just text\n" {
		t.Errorf("regular-file read changed: got %q, want %q", got, "just text\n")
	}
}
