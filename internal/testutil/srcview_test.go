package testutil

import (
	"strings"
	"testing"
)

// A POSITION-PRESERVING "code only" VIEW of Go source, for the raw-text gates in
// this package.
//
// WHY IT EXISTS (measured 2026-10-31, task 01a18fd1): declaresTestMain did a
// line-prefix match on RAW source, so `func TestMain(` matched in two places
// that are not declarations —
//
//   - inside a COMMENT (this very package's gates spell the shape in prose), and
//   - inside a RAW-STRING FIXTURE BODY (internal/tools/task_briefing_run_alternatives_test.go
//     builds test files in a backtick literal; the fixture text carries a real
//     `func TestMain`, so the gate counted two TestMain entries for
//     internal/tools while the package compiled fine — `go list` said
//     TestGoFiles=587, XTestGoFiles=0, and `go test -run NoSuchTest` was ok).
//
// The false positive was reported as "this package does NOT COMPILE", which is
// the exact opposite of the truth. So the gate now judges a view in which
// neither a comment body nor a literal's CONTENT can be seen.
//
// THE BOUNDARY, and why it is the OPPOSITE of memory #12353's rule. That memory
// says "never blank string literals" — correct for a gate hunting a defect that
// IS spelled as a literal (`"-test.run="`, `"TestNoSuch"`): blanking the literal
// there is the false-NEGATIVE floor caving in. This gate hunts a DECLARATION,
// and a declaration cannot be inside a literal — the compiler proves it. So
// blanking literal content here removes only prose, never code, and it is the
// cure for the measured false positive. Comment blanking is needed in BOTH
// worlds and stays.
//
// POSITION is preserved exactly: every blanked byte becomes scanViewBlank, so
// len(view) == len(src) and every '\n' stays where it was. A caller that
// computes a line number from an offset in the view is therefore reporting the
// READER's line number, not a shifted one.

// scanViewBlank replaces a byte the view must hide. A space (not a NUL) keeps
// the view readable when a diagnostic prints it, and keeps `strings.Split(…,
// "\n")` producing the same number of lines as the original.
const scanViewBlank = ' '

// scanViewCodeOnly returns src with every COMMENT BODY and every STRING/RUNE/RAW
// -STRING LITERAL CONTENT replaced by scanViewBlank, byte for byte, newlines
// kept. Quoting is honoured (a `//` or a quote inside a literal opens nothing)
// and `\`-escapes are skipped, so `"\\""` cannot end an interpreted string
// early and `'\”` cannot end a rune literal early.
func scanViewCodeOnly(src string) string {
	out := []byte(src)
	n := len(out)
	i := 0
	for i < n {
		ch := src[i]
		switch {
		case ch == '/' && i+1 < n && src[i+1] == '/':
			i = scanBlankLineComment(out, src, i)
		case ch == '/' && i+1 < n && src[i+1] == '*':
			i = scanBlankBlockComment(out, src, i)
		case ch == '`':
			i = scanBlankQuoted(out, src, i, '`', false)
		case ch == '"':
			i = scanBlankQuoted(out, src, i, '"', true)
		case ch == '\'':
			i = scanBlankQuoted(out, src, i, '\'', true)
		default:
			i++
		}
	}
	return string(out)
}

// scanBlankLineComment blanks a `//` comment (both slashes, its body, but NOT
// the terminating newline) and returns the index of that newline (or n).
func scanBlankLineComment(out []byte, src string, start int) int {
	i := start
	for i < len(src) {
		if src[i] == '\n' {
			return i
		}
		out[i] = scanViewBlank
		i++
	}
	return i
}

// scanBlankBlockComment blanks a `/* … */` body (delimiters included, newlines
// kept) and returns the index just past the closing `*/`. An unterminated block
// comment blanks to end of source — the scanner never invents bytes.
func scanBlankBlockComment(out []byte, src string, start int) int {
	i := start
	out[i], out[i+1] = scanViewBlank, scanViewBlank
	i += 2
	for i < len(src) {
		if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
			out[i], out[i+1] = scanViewBlank, scanViewBlank
			return i + 2
		}
		if src[i] != '\n' {
			out[i] = scanViewBlank
		}
		i++
	}
	return i
}

// scanBlankQuoted blanks one quoted literal (delimiters and content) and returns
// the index just past its closing delimiter. escapes reports whether `\`-escapes
// apply: true for interpreted strings and rune literals, false for raw strings.
// An unterminated literal stops at end of line (interpreted/rune) or end of
// source (raw) — never at a byte it did not read.
func scanBlankQuoted(out []byte, src string, start int, delim byte, escapes bool) int {
	i := start
	out[i] = scanViewBlank
	i++
	for i < len(src) {
		if escapes && src[i] == '\\' && i+1 < len(src) {
			out[i] = scanViewBlank
			if src[i+1] != '\n' {
				out[i+1] = scanViewBlank
			}
			i += 2
			continue
		}
		if src[i] == delim {
			out[i] = scanViewBlank
			return i + 1
		}
		if escapes && src[i] == '\n' {
			return i
		}
		if src[i] != '\n' {
			out[i] = scanViewBlank
		}
		i++
	}
	return i
}

// TestDeclaresTestMainJudgesCodeNotProse is the regression arm for the measured
// false positive: BOTH DIRECTIONS in one table, because a scanner that never
// fires would "prove" comments and literals are ignored while proving nothing —
// the rows below that must COUNT are the fake-negative floor.
func TestDeclaresTestMainJudgesCodeNotProse(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"line comment spelling the declaration", "// func TestMain(m *testing.M) {\npackage p\n", false},
		{"block comment spelling the declaration", "/*\nfunc TestMain(m *testing.M) {\n*/\npackage p\n", false},
		{"raw-string fixture declaring it", "package p\n\nvar fixture = `\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n`\n", false},
		{"interpreted-string fixture declaring it", "package p\n\nvar fixture = \"\\nfunc TestMain(m *testing.M) {\\n\"\n", false},
		{"real declaration counted", "package p\n\nfunc TestMain(m *testing.M) {\n\tos.Exit(m.Run())\n}\n", true},
		{"indented real declaration counted", "package p\n\n\tfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n", true},
		{"declaration after a literal still counted", "package p\n\nvar s = \"text with a } brace\"\n\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n", true},
		{"comment marker inside a literal opens no comment", "package p\n\nvar sep = \"//\"\n\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n", true},
		{"escaped quote cannot end a literal early", "package p\n\nvar s = \"a \\\" func TestMain(\"\n\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n", true},
	}
	counted := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := declaresTestMain(tc.src); got != tc.want {
				t.Fatalf("declaresTestMain = %v, want %v\nsource:\n%s\nview:\n%s", got, tc.want, tc.src, scanViewCodeOnly(tc.src))
			}
		})
		if tc.want {
			counted++
		}
	}
	if counted < srcViewMinCountingRows {
		t.Fatalf("only %d of the table's rows COUNT a declaration (want at least %d) — a scanner that never "+
			"fires would pass every must-not-count row while proving nothing (fake-negative floor)",
			counted, srcViewMinCountingRows)
	}
}

// srcViewMinCountingRows is the floor for the counting half of the table above.
const srcViewMinCountingRows = 4

// TestScanViewIsPositionPreserving pins the property every caller depends on:
// the view is the SAME LENGTH as the source and keeps every newline where it was,
// so a line number computed from a view offset is the READER's line number.
func TestScanViewIsPositionPreserving(t *testing.T) {
	src := "package p\n\n// a comment\nvar s = \"lit\neral\"\nvar r = `raw\nstring`\n\nfunc TestMain(m *testing.M) {\n\tos.Exit(m.Run())\n}\n"
	view := scanViewCodeOnly(src)
	if len(view) != len(src) {
		t.Fatalf("view length %d != source length %d — blanking must preserve byte positions", len(view), len(src))
	}
	if got, want := strings.Count(view, "\n"), strings.Count(src, "\n"); got != want {
		t.Fatalf("view carries %d newlines, source %d — line numbers would shift", got, want)
	}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' && view[i] != '\n' {
			t.Fatalf("byte %d: newline replaced in the view", i)
		}
	}
}
