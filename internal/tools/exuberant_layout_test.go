package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixtures here reproduce the layout exuberant-ctags ACTUALLY writes, which
// the byte-0 prefix probe rejected at real repo scale (measured 2026-10-11 on
// this host, exuberant 5.9~svn20110310).
//
// The mechanism is sorting, not omission: exuberant writes a byte-sorted file
// and therefore SORTS its six pseudo-tags among the entries. This repo's
// internal/heavytest/boundary.go carries multi-line string literals, and the Go
// parser formats those into search patterns whose first line begins with six
// spaces — so those entries' names start with ' ' (0x20), which sorts BEFORE '!'
// (0x21). Once 20 of them exist, the whole !_TAG_* block lands at line 21, byte
// offset 4294, of a 12,288,019-byte index. On a small directory those literals
// are absent and the block sits at byte 0, which is why a byte-0 test shipped
// working and only broke at real scale.

// tagExuberantBlockOffsetMeasured is the byte offset at which this repo's
// exuberant index carries its pseudo-tag block (measured 2026-10-11; line 21 of
// a 12,288,019-byte file). It is the number tagHeaderProbeBytes must cover.
const tagExuberantBlockOffsetMeasured = 4294

// exuberantEntriesBeforePseudoTags is the measured count of entries that sort
// before the block in that index: 20 entries whose name begins with six spaces.
const exuberantEntriesBeforePseudoTags = 20

// exuberantPseudoTagBlock is the six-line pseudo-tag block, abbreviated to the
// lines that matter for the probe (the real block also carries PROGRAM_AUTHOR,
// PROGRAM_URL and PROGRAM_VERSION).
func exuberantPseudoTagBlock() string {
	return "!_TAG_FILE_FORMAT\t2\t/extended format; --format=1 will not append ;\" to lines/\n" +
		"!_TAG_FILE_SORTED\t1\t/0=unsorted, 1=sorted, 2=foldcase/\n" +
		"!_TAG_PROGRAM_NAME\tExuberant Ctags\t//\n"
}

// exuberantLayoutIndex builds a COMPLETE index in exuberant's layout: entries
// whose names begin with a space first, then the pseudo-tag block sorted after
// them, then a normal entry. It returns the bytes and the block's byte offset.
func exuberantLayoutIndex() (data []byte, blockOffset int) {
	var sb strings.Builder
	for i := 0; i < exuberantEntriesBeforePseudoTags; i++ {
		sb.WriteString(fmt.Sprintf("      GOPI_%03d\ta.go\t/^x$/;\"\tf\tline:%d\tlanguage:Go\n", i, i+1))
	}
	blockOffset = sb.Len()
	sb.WriteString(exuberantPseudoTagBlock())
	sb.WriteString("ZzzExuberant\ta.go\t/^func ZzzExuberant() {}$/;\"\tf\tline:9\tlanguage:Go\n")
	return []byte(sb.String()), blockOffset
}

// TestTagsIndexValidAcceptsOffByteZeroPseudoTagBlock is the regression pin for
// the reported defect: a complete index whose pseudo-tag block is NOT at byte 0
// is a ctags index and must be accepted. Pre-fix every real exuberant index
// failed this, so ensureTags deleted the 12MB artifact and paid a full-tree
// ctags run on every single code_search call.
func TestTagsIndexValidAcceptsOffByteZeroPseudoTagBlock(t *testing.T) {
	data, blockOffset := exuberantLayoutIndex()
	if blockOffset == 0 {
		t.Fatal("fixture error: the block must not sit at byte 0")
	}
	if blockOffset > tagHeaderProbeBytes {
		t.Fatalf("fixture error: block at %d is outside the %d-byte probe window", blockOffset, tagHeaderProbeBytes)
	}
	dir := t.TempDir()
	tagsPath := filepath.Join(dir, tagsIndexFileName)
	writeTornFixture(t, tagsPath, data)

	if !tagsIndexValid(tagsPath) {
		t.Errorf("tagsIndexValid = false for an index whose pseudo-tag block sits at byte %d — "+
			"exuberant's byte-sorted layout must be judged usable", blockOffset)
	}

	head := data
	if len(head) > tagHeaderProbeBytes {
		head = head[:tagHeaderProbeBytes]
	}
	if got := tagPseudoTagLineOffset(head); got != blockOffset {
		t.Errorf("tagPseudoTagLineOffset = %d, want %d (the block's real position)", got, blockOffset)
	}
}

// TestTagsIndexValidRejectsPatternQuotedPrefixOnly keeps the position-
// independent probe a real test rather than a substring search. On this repo's
// index two ordinary Go entries have the prefix text INSIDE their search pattern
// (`FileHeaderPrefix = "!_TAG_FILE_FORMAT"`, measured at offsets 9376658 and
// 11504509, where the byte after the prefix is '$' and the byte before it is
// '"'). An index holding only such an entry has no pseudo-tag block and must be
// rejected — otherwise any file that merely mentions the string would pass.
func TestTagsIndexValidRejectsPatternQuotedPrefixOnly(t *testing.T) {
	entry := "TagFileHeaderPrefix\ta.go\t/^const tagFileHeaderPrefix = \"!_TAG_FILE_FORMAT\"$/;\"\tc\tline:1\tlanguage:Go\n"
	for _, tc := range []struct {
		name    string
		content []byte
	}{
		{"pattern-quotes-prefix", []byte(entry)},
		{"prefix-mid-line", []byte("Zzz\ta.go\t/^var X = !_TAG_FILE_FORMAT$/;\"\tf\n")},
		// The prefix at a line start but NOT followed by the pseudo-tag
		// separator is a pattern too, not the block.
		{"line-start-followed-by-nonspace", []byte("!_TAG_FILE_FORMATx\ta.go\t/^x$/;\"\tf\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tagsPath := filepath.Join(dir, tagsIndexFileName)
			writeTornFixture(t, tagsPath, tc.content)
			if tagsIndexValid(tagsPath) {
				t.Errorf("tagsIndexValid = true for %s — the prefix text alone is not a pseudo-tag block", tc.name)
			}
		})
	}
}

// TestTagHeaderProbeWindowCoversMeasuredExuberantLayout pins the probe budget
// against the measurement it exists for: the scan must reach past the deepest
// pseudo-tag block a real producer writes, or a valid index is rejected for a
// reason ("the block is past the window") that has nothing to do with validity.
// It also pins that the budget stays FAR from a full-file scan — the whole point
// of a head probe on the highest-frequency tool.
func TestTagHeaderProbeWindowCoversMeasuredExuberantLayout(t *testing.T) {
	if tagHeaderProbeBytes < tagExuberantBlockOffsetMeasured {
		t.Errorf("tagHeaderProbeBytes = %d, want >= %d — the probe cannot see this repo's exuberant block",
			tagHeaderProbeBytes, tagExuberantBlockOffsetMeasured)
	}
	const realIndexBytes = 12288019 // the measured size of this repo's exuberant index
	if tagHeaderProbeBytes >= realIndexBytes {
		t.Errorf("tagHeaderProbeBytes = %d — the probe must stay a HEAD read, not a scan of the %d-byte index",
			tagHeaderProbeBytes, realIndexBytes)
	}
}

// TestEnsureTagsKeepsCompleteExuberantLayoutIndex is the defect at its own
// level: ensureTags must SERVE a complete exuberant-layout index, not delete it.
// The instrumented ctags logs any generation run, so the pre-fix behaviour —
// probe says "not a ctags index" → remove → regenerate, on every call — shows up
// as an invocation count above zero (and as a clobbered artifact, since the shim
// writes its own torn one).
func TestEnsureTagsKeepsCompleteExuberantLayoutIndex(t *testing.T) {
	logPath := testTornCtagsShim(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"),
		[]byte("package a\n\nfunc ZzzExuberant() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, blockOffset := exuberantLayoutIndex()
	tagsPath := filepath.Join(dir, tagsIndexFileName)
	writeTornFixture(t, tagsPath, data)
	// Keep the index NEWER than the source: the artifact must be served (and
	// therefore judged), not replaced for staleness before anything looks at it.
	now := time.Now()
	if err := os.Chtimes(tagsPath, now, now); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(tagsPath)
	if err != nil {
		t.Fatal(err)
	}
	resetStalenessCache()
	resetTagsIndexWatch()
	tagsSelfHealCount.Store(0)

	for call := 1; call <= 2; call++ {
		if _, err := ensureTags(context.Background(), dir); err != nil {
			t.Fatalf("ensureTags call %d over a complete exuberant-layout index: %v", call, err)
		}
	}
	if n := countAttempts(t, logPath); n != 0 {
		t.Errorf("ctags invocations = %d, want 0 — a COMPLETE exuberant-layout index was deleted and rebuilt "+
			"(12MB artifact, one full-tree run per code_search call)", n)
	}
	if n := tagsSelfHealCount.Load(); n != 0 {
		t.Errorf("self-heal count = %d, want 0 — the index was treated as corrupt, not as a ctags index", n)
	}
	after, err := os.Stat(tagsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("index regenerated on the second call — a valid index must be reused")
	}
	got, err := os.ReadFile(tagsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Error("index bytes changed — the valid artifact was rewritten")
	}
	if off := tagPseudoTagLineOffset(got); off != blockOffset {
		t.Errorf("pseudo-tag block moved to %d, want %d", off, blockOffset)
	}
}
