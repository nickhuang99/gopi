package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestCodeSearchSelfHealsCorruptIndex is the regression guard for the
// 2026-09-10 corrupt-index incident: a crash/disk-full left .gopi-tags as
// 2,584,800 bytes of pure NUL, and ctags refuses to overwrite a file that
// "doesn't look like a tag file" — so EVERY later code_search call failed
// until a human deleted the artifact by hand. A damaged index must instead be
// treated as a cache MISS: drop it, rebuild once, return the results.
func TestCodeSearchSelfHealsCorruptIndex(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	cases := []struct {
		name    string
		content []byte
	}{
		{"nul-filled", bytes.Repeat([]byte{0}, 2<<20)}, // the incident's exact shape
		{"truncated-header", []byte("!_TAG_")},
		{"foreign-text", []byte("this file is not a ctags index\n")},
		{"empty", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "a.go"),
				[]byte("package a\n\nfunc CsSelfHealMe() {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			tagsPath := filepath.Join(dir, tagsIndexFileName)
			if err := os.WriteFile(tagsPath, tc.content, 0o644); err != nil {
				t.Fatal(err)
			}
			resetStalenessCache()
			tagsSelfHealCount.Store(0)

			scope := SearchScope{RootPath: dir, IncludeGlob: CodeSearchGlobs, MaxResult: 50}
			hits, _, _, err := CodeSearch(scope, "CsSelfHealMe")
			if err != nil {
				t.Fatalf("CodeSearch over a corrupt index: %v (a corrupt .gopi-tags must self-heal, not disable code_search)", err)
			}
			if len(hits) == 0 {
				t.Fatal("no hits for a symbol that exists — the rebuilt index is empty")
			}
			if hits[0].SymbolKind == "reference" {
				t.Errorf("SymbolKind = %q (ripgrep fallback), want a ctags definition hit — the index was not rebuilt",
					hits[0].SymbolKind)
			}
			if got := tagsSelfHealCount.Load(); got != 1 {
				t.Errorf("self-heal count = %d, want 1", got)
			}
			if !tagsIndexValid(tagsPath) {
				t.Error("index still invalid on disk — the artifact was not repaired")
			}

			// The repaired index must be reused, not re-healed: a second call
			// is a clean cache hit (no extra regeneration, still no error).
			repaired, err := os.Stat(tagsPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := CodeSearch(scope, "CsSelfHealMe"); err != nil {
				t.Fatalf("CodeSearch after repair: %v", err)
			}
			if got := tagsSelfHealCount.Load(); got != 1 {
				t.Errorf("self-heal count = %d after the repair round, want 1 (one-shot heal)", got)
			}
			after, err := os.Stat(tagsPath)
			if err != nil {
				t.Fatal(err)
			}
			if !after.ModTime().Equal(repaired.ModTime()) {
				t.Error("index regenerated on the second call — a valid index must be reused")
			}
		})
	}
}

// TestTagsIndexValid pins the validity probe itself (unit, no ctags needed):
// a real header is accepted, every corruption shape is rejected.
func TestTagsIndexValid(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, content []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	header := "!_TAG_FILE_FORMAT\t2\t/extended format/\n!_TAG_FILE_SORTED\t1\t/0=unsorted/\n"
	cases := []struct {
		name    string
		content []byte
		want    bool
	}{
		{"valid-header", []byte(header + "Foo\ta.go\t/^func Foo() {$/;\"\tf\n"), true},
		{"header-only", []byte(header), true},
		{"nul-filled", bytes.Repeat([]byte{0}, 4096), false},
		{"empty", nil, false},
		{"truncated-header", []byte("!_TAG_"), false},
		{"foreign", []byte("#!/bin/sh\necho hi\n"), false},
		{"header-then-nul", append([]byte(header), 0), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := write(tc.name, tc.content)
			if got := tagsIndexValid(p); got != tc.want {
				t.Errorf("tagsIndexValid(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
	if tagsIndexValid(filepath.Join(dir, "missing")) {
		t.Error("tagsIndexValid(missing file) = true, want false")
	}
}

// TestEnsureTagsSelfHealsCorruptIndex exercises the ensureTags contract
// directly: it returns a usable index path (never an error) and leaves the
// artifact repaired on disk.
func TestEnsureTagsSelfHealsCorruptIndex(t *testing.T) {
	if !ctagsAvailable() {
		t.Skip("ctags not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc CsEnsure() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tagsPath := filepath.Join(dir, tagsIndexFileName)
	if err := os.WriteFile(tagsPath, bytes.Repeat([]byte{0}, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	resetStalenessCache()
	tagsSelfHealCount.Store(0)

	got, err := ensureTags(ctx, dir)
	if err != nil {
		t.Fatalf("ensureTags over a NUL-filled index: %v", err)
	}
	if got != tagsPath {
		t.Errorf("ensureTags path = %q, want %q", got, tagsPath)
	}
	if !tagsIndexValid(tagsPath) {
		t.Error("index not repaired after ensureTags")
	}
	if n := tagsSelfHealCount.Load(); n != 1 {
		t.Errorf("self-heal count = %d, want 1", n)
	}
}
