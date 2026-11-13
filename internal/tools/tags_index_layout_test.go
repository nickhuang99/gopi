package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures below pin the ONE property the index probe must not assume: that
// the ctags pseudo-tag block ("!_TAG_FILE_FORMAT\t…", "!_TAG_FILE_SORTED\t…")
// starts at byte 0.
//
// MEASURED (exuberant-ctags 5.9~svn20110310, this repo, 2026-10-11 — the full
// transcript is in artifacts/exuberant-layout.txt of the fixing run):
//
//	ctags -R --languages=Go --extra=+q --fields=+ln -f idx.tags .
//	→ 12,008,061 bytes; the prefix "!_TAG_FILE_FORMAT" occurs at 4294, 9111758
//	  and 11233762. ONLY the first is a pseudo-tag LINE (line 21, preceded by a
//	  newline and followed by a TAB); the other two are ordinary Go entries whose
//	  search PATTERN quotes the literal (next byte '"'), i.e.
//	  /^const tagFileHeaderPrefix = "!_TAG_FILE_FORMAT"$/ — this file's own
//	  constant, indexed by this host's ctags.
//
// Ebuerant writes a BYTE-SORTED file and sorts its six pseudo-tags among the
// entries; ' ' (0x20) < '!' (0x21), so any entry whose pattern's first line
// begins with a space sorts BEFORE the block. On this repo 20 such entries do
// (lines 1-20 above start with 0x20), which puts the block at byte 4294. A
// byte-0 prefix probe therefore rejects every index this host produces, so
// code_search permanently degrades to the ripgrep fallback — silently, because
// ensureTags just rebuilds and re-rejects on every call.

// TestTagPseudoTagLineOffset pins the matcher itself: line-anchored AND
// followed by a tab, with NUL checked by the caller.
func TestTagPseudoTagLineOffset(t *testing.T) {
	block := []byte(tagFileHeaderPrefix + "\t2\t/extended format/\n")
	// The measured exuberant head: space-leading entries (lines 1-20 of this
	// repo's real index), then the block.
	exuberantHead := "      GOPI_HUMAN_TERMINAL=1 go\n  PASS: gopi task update %s\n" + string(block)
	cases := []struct {
		name string
		head string
		want int
	}{
		{"universal-ctags shape: block at byte 0", string(block), 0},
		{"exuberant shape: block behind space-leading entries",
			exuberantHead, len(exuberantHead) - len(string(block))},
		{"block behind a CRLF-only separator is still line-anchored",
			"entry\n" + string(block), 6},
		{"entry pattern quoting the prefix text is NOT the block",
			"c\tinternal/tools/symbol.go\t/^const tagFileHeaderPrefix = \"!_TAG_FILE_FORMAT\"$/;\"\tc\n\tanother\n", -1},
		{"prefix mid-line is not line-anchored", "xx" + tagFileHeaderPrefix + "\t2\n", -1},
		{"prefix without the tab separator is not a pseudo-tag line",
			tagFileHeaderPrefix + "/nope\n", -1},
		{"prefix truncated by the window's end is not accepted",
			string([]byte(tagFileHeaderPrefix)[:len(tagFileHeaderPrefix)-1]), -1},
		{"no prefix at all", "package a\nfunc Foo() {}\n", -1},
		{"empty window", "", -1},
	}
	for _, tc := range cases {
		if got := tagPseudoTagLineOffset([]byte(tc.head)); got != tc.want {
			t.Errorf("%s: tagPseudoTagLineOffset = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestTagsIndexProbeAcceptsBlockNotAtByteZero is the minimal regression guard
// (Rule 5) for the defect: an index whose pseudo-tag block is NOT at byte 0
// must be accepted, and the byte-0 shape must keep being accepted.
func TestTagsIndexProbeAcceptsBlockNotAtByteZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, tagsIndexFileName)

	// The smallest sample that reproduces it: ONE byte-sortable entry before the
	// block, its pattern's first line beginning with a space.
	entries := "      GOPI_HUMAN_TERMINAL=1 go\n"
	block := tagFileHeaderPrefix + "\t2\t/extended format/\n" +
		"!_TAG_FILE_SORTED\t1\t/0=unsorted/\n"
	body := "Foo\tinternal/tools/symbol.go\t/^func Foo() {$/;\"\tf\tline:1\tlanguage:Go\n"

	for _, tc := range []struct {
		name string
		data string
	}{
		{"block not at byte 0 (exuberant)", entries + block + body},
		{"block at byte 0 (universal-ctags)", block + entries + body},
	} {
		if err := os.WriteFile(path, []byte(tc.data), 0o644); err != nil {
			t.Fatal(err)
		}
		if !tagsIndexValid(path) {
			t.Errorf("%s: tagsIndexValid = false, want true", tc.name)
		}
		if len(tc.data) <= 0 {
			t.Fatal("unreachable")
		}
	}

	// Still not a licence to accept anything: an entry QUOTING the prefix text is
	// not the block, and a NUL byte still rejects (the 2026-09-10 incident).
	for _, tc := range []struct {
		name string
		data string
	}{
		{"only an entry quoting the prefix text",
			"c\tx.go\t/^const tagFileHeaderPrefix = \"!_TAG_FILE_FORMAT\"$/;\"\tc\tline:1\n"},
		{"NUL-filled artifact (2.5MB all-NUL shape, sampled)",
			strings.Repeat("\x00", 4096) + block},
		{"NUL after a valid block", block + "\x00\x00torn"},
		{"empty file", ""},
	} {
		if err := os.WriteFile(path, []byte(tc.data), 0o644); err != nil {
			t.Fatal(err)
		}
		if tagsIndexValid(path) {
			t.Errorf("%s: tagsIndexValid = true, want false", tc.name)
		}
	}
}

// TestTagsIndexProbeOnRealCtagsLayout drives the probe over bytes THIS host's
// ctags actually produced, and pins the layout it produced as a measured fact
// rather than an assumption. The assertion is deliberately shape-independent: a
// real index is accepted wherever its block sits. On a host whose ctags places
// the block at byte 0 (universal-ctags) the acceptance half still holds and the
// layout half is a report, not a failure.
func TestTagsIndexProbeOnRealCtagsLayout(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed — this test needs a real ctags index")
	}
	full := buildRealTagsIndex(t)

	off := tagPseudoTagLineOffset(full)
	if off < 0 {
		t.Fatal("no pseudo-tag line found in a real ctags index — the matcher is wrong, not the ctags")
	}
	nl := bytes.Count(full[:off], []byte("\n")) + 1
	t.Logf("host ctags index: %d bytes; pseudo-tag block at byte offset %d (line %d)", len(full), off, nl)

	// The block is not the only occurrence of the prefix text: an entry whose
	// pattern quotes it must not be mistaken for it. That is exactly why the
	// matcher requires a LINE START plus a tab.
	if n := bytes.Count(full, []byte(tagFileHeaderPrefix)); n > 1 {
		t.Logf("prefix text occurs %d times (only the block is line-anchored + tab-terminated)", n)
	}

	path := filepath.Join(t.TempDir(), tagsIndexFileName)
	if err := os.WriteFile(path, full, 0o644); err != nil {
		t.Fatal(err)
	}
	if !tagsIndexValid(path) {
		t.Errorf("tagsIndexValid = false on the real index (block at offset %d) — the probe still requires byte 0", off)
	}

	// A real index from this very repository, placed behind a prefix so the block
	// is provably off byte 0, must also be accepted — the property the defect
	// turned into a permanent false.
	if off == 0 {
		shifted := append([]byte("      filler entry that sorts first\n"), full...)
		if err := os.WriteFile(path, shifted, 0o644); err != nil {
			t.Fatal(err)
		}
		if !tagsIndexValid(path) {
			t.Error("tagsIndexValid = false on a real index whose block was moved off byte 0")
		}
	}
}

// TestEnsureTagsUsesTheIndexWhenTheBlockIsNotAtByteZero proves the fix at the
// level that matters — not the predicate but the wiring: with a block-off-zero
// index on disk that is NEWER than the sources, ensureTags must SERVE it instead
// of rebuilding (a rebuild costs a full-tree ctags run), i.e. it must not be
// classified as unusable. Before the fix this path rebuilt-and-re-rejected on
// every call.
func TestEnsureTagsUsesTheIndexWhenTheBlockIsNotAtByteZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shimLog := testTornCtagsShim(t)

	// A FINISHED index whose pseudo-tag block sits behind a space-leading entry.
	idx := "      GOPI_HUMAN_TERMINAL=1 go\n" +
		tagFileHeaderPrefix + "\t2\t/extended format/\n" +
		"!_TAG_FILE_SORTED\t1\t/0=unsorted/\n" +
		"Foo\ta.go\t/^func Foo() {$/;\"\tf\tline:3\tlanguage:Go\n"
	tagsPath := filepath.Join(dir, tagsIndexFileName)
	if err := os.WriteFile(tagsPath, []byte(idx), 0o644); err != nil {
		t.Fatal(err)
	}
	// Newer than the source, so it is SERVED rather than regenerated for staleness.
	// (master's noteCompleteTagsIndex takes the artifact's FileInfo — the file
	// identity is what separates an atomic publish from an in-place rewrite —
	// where the pre-merge branch passed a bare size.)
	info, err := os.Stat(tagsPath)
	if err != nil {
		t.Fatalf("stat %s: %v", tagsPath, err)
	}
	noteCompleteTagsIndex(dir, info)
	resetStalenessCache()
	resetTagsIndexWatch()

	got, err := ensureTags(context.Background(), dir)
	if err != nil {
		t.Fatalf("ensureTags: %v", err)
	}
	if got != tagsPath {
		t.Errorf("ensureTags = %q, want the existing index %q", got, tagsPath)
	}
	if n := countAttempts(t, shimLog); n != 0 {
		t.Errorf("ctags invocations = %d, want 0 — a usable index must be served, not rebuilt", n)
	}
}
