package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExpandFileRef_ReadsFileVerbatim(t *testing.T) {
	dir := t.TempDir()
	content := "line one\nwith \"quotes\" and $special `chars`\n\ttabbed\n\n"
	path := writeTemp(t, dir, "briefing.md", content)

	got, err := ExpandFileRef("@" + path)
	if err != nil {
		t.Fatalf("ExpandFileRef: %v", err)
	}
	if got != content {
		t.Errorf("expansion not verbatim:\n got %q\nwant %q", got, content)
	}
}

func TestExpandFileRef_Passthrough(t *testing.T) {
	cases := []string{
		"plain text",
		"",
		"@",         // bare @ — no path → literal
		"user@host", // @ not at start → untouched
		// whitespace in the path → almost certainly prose, not a ref
		// (a file ref must be a single path token: '@<path>', no spaces).
		"@nick mentioned this in #chan",
		"see @briefing.md for details", // @ not at start
	}
	for _, in := range cases {
		got, err := ExpandFileRef(in)
		if err != nil {
			t.Fatalf("ExpandFileRef(%q): %v", in, err)
		}
		if got != in {
			t.Errorf("ExpandFileRef(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestExpandFileRef_EscapeHatch(t *testing.T) {
	got, err := ExpandFileRef("@@literal")
	if err != nil {
		t.Fatalf("ExpandFileRef: %v", err)
	}
	if got != "@literal" {
		t.Errorf("ExpandFileRef(@@literal) = %q, want %q", got, "@literal")
	}
}

func TestExpandFileRef_MissingFileError(t *testing.T) {
	_, err := ExpandFileRef("@/nonexistent/path/definitely-missing.txt")
	if err == nil {
		t.Fatal("want error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "@/nonexistent/path/definitely-missing.txt") {
		t.Errorf("error should name the reference, got: %v", err)
	}
}

func TestExpandFileRef_OverSizeRefused(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "big.bin", strings.Repeat("x", maxFileRefBytes+1))
	_, err := ExpandFileRef("@" + path)
	if err == nil {
		t.Fatal("want error for oversized file, got nil")
	}
	if !strings.Contains(err.Error(), "byte limit") {
		t.Errorf("error should mention the byte limit, got: %v", err)
	}
}

func TestIsFileRef(t *testing.T) {
	if IsFileRef("@") {
		t.Error("bare @ is not a file reference")
	}
	if IsFileRef("plain") || IsFileRef("") || IsFileRef("a@b") {
		t.Error("non-@ strings are not file references")
	}
	if !IsFileRef("@-") || !IsFileRef("@path") {
		t.Error("@- and @path are file references")
	}
}
