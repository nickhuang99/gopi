package main

// inputpath_test.go — the regression guard for the RP-2 fix (end-user audit
// slice 8): a PATH-SHAPED positional that names nothing is refused by name,
// before any session or ledger row, exactly like its `@PATH` twin — while
// plain prose (including prose that MENTIONS a path) keeps running.
//
// The red-then-green proof lives in the report: every `refused` case here
// failed (rc-equivalent: readInput returned the token as the prompt) on the
// tree before this change.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// TestPathShapedReason is the classifier contract: the SHAPE decides, never a
// guess about intent. The refused column is the path spelling; every `run`
// case is a control that must not be caught by the refusal.
func TestPathShapedReason(t *testing.T) {
	cases := []struct {
		token   string
		refused bool
		why     string
	}{
		// --- path-shaped: refused when the file is missing ---
		{"/tmp/fx/nope.txt", true, "separator"},
		{"./missing.md", true, "separator"},
		{"sub/dir/note.txt", true, "separator"},
		{"~/x.png", true, "separator"},
		{"/tmp/fx/nope", true, "separator"},
		{"../frobnicate", true, "separator"},
		{"missing.png", true, "image extension"},
		{"missing.png", true, ""},
		{"notes.txt", true, "extension"},
		{"BRIEF.MD", true, "extension (case-insensitive)"},
		{"pic.webp", true, "image extension"},

		// --- prompt text: unchanged behavior, the shape says nothing ---
		{"frobnicate", false, ""},
		{"version", false, ""},
		{"test", false, ""},
		{"your task is written in image ~/x.png", false, ""}, // prose mentioning a path
		{"see /tmp/nonexistent.png for details", false, ""},  // prose mentioning a path
		{"fix the failing test in foo_test.go", false, ""},   // prose, whitespace-bearing
		{"", false, ""},
		{"hello world", false, ""},
	}
	for _, c := range cases {
		why := pathShapedReason(c.token)
		if got := why != ""; got != c.refused {
			t.Errorf("pathShapedReason(%q) refused=%v, want %v (why=%q)", c.token, got, c.refused, why)
		}
		if c.refused && why == "" {
			t.Fatalf("pathShapedReason(%q) refused with no reason text", c.token)
		}
	}
}

// TestReadInput_MissingPathShapedRefused is the defect itself: the RP-2
// invocation shape (`gopi … /tmp/fx/nope.txt`) must return an error — not a
// prompt — and the message must name the path, the missing-file reason, and
// both legal spellings.
func TestReadInput_MissingPathShapedRefused(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.txt")

	in, err := readInput(&config.Config{Positionals: []string{missing}})
	if err == nil {
		t.Fatalf("readInput(%q) returned no error — the path string would be sent as the task text (prompt=%q)", missing, in.prompt)
	}
	msg := err.Error()
	for _, want := range []string{missing, "no such file", "--prompt", literalPromptEscape, payloadFilePrefix} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal does not mention %q:\n%s", want, msg)
		}
	}
}

// TestReadInput_MissingPathShapedMatchesAtTwin: both spellings of the same
// defect refuse, and both name the path — the audit's core "two spellings, one
// semantics" claim.
func TestReadInput_MissingPathShapedMatchesAtTwin(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.txt")

	_, bareErr := readInput(&config.Config{Positionals: []string{missing}})
	_, atErr := readInput(&config.Config{Positionals: []string{"@" + missing}})
	if bareErr == nil || atErr == nil {
		t.Fatalf("bare err=%v, @PATH err=%v — both spellings must refuse", bareErr, atErr)
	}
	if !strings.Contains(bareErr.Error(), missing) || !strings.Contains(atErr.Error(), missing) {
		t.Errorf("refusals must name the path:\nbare: %s\n@:    %s", bareErr, atErr)
	}
	if !strings.Contains(bareErr.Error(), "no such file") {
		t.Errorf("bare refusal must carry the file-not-found reason: %s", bareErr)
	}
}

// TestMissingPathPositional_NamesThePathInOneGrammaticalClause — the refusal is
// read by a user, so the shape reason must not be pasted beside the path as a
// dangling clause. Measured on the real binary (probe `bare_missing`, round 3):
// the pre-reword template rendered `"/tmp/fx/nope.txt" it contains the path
// separator "/"`, i.e. a quoted path followed by a sentence fragment — the
// message named the right facts in an unreadable order. Pinned here so the
// concatenation cannot come back: the path is introduced by "no such file at",
// and no `"<path>" it ` shape occurs anywhere in the message.
func TestMissingPathPositional_NamesThePathInOneGrammaticalClause(t *testing.T) {
	const token = "/tmp/fx/nope.txt"
	why := pathShapedReason(token)
	if why == "" {
		t.Fatalf("pathShapedReason(%q) = \"\" — this test needs a path-shaped control", token)
	}
	msg := missingPathPositional(token, why).Error()
	if !strings.Contains(msg, fmt.Sprintf("no such file at %q", token)) {
		t.Errorf("refusal must introduce the path with the missing-file reason:\n%s", msg)
	}
	if strings.Contains(msg, fmt.Sprintf("%q it ", token)) {
		t.Errorf("refusal pastes a sentence fragment after the path:\n%s", msg)
	}
	for _, want := range []string{token, why, "--prompt", literalPromptEscape, payloadFilePrefix} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal does not mention %q:\n%s", want, msg)
		}
	}
}

// TestReadInput_PromptFlagMakesTheRefusalSpellingsReal — the refusal advises
// `--prompt TEXT`; that advice must be a spelling that WORKS on the very token
// it refused. Measured on the real binary (probes FC1/FC2 in the report):
// before this scoping the advice was CIRCULAR — `gopi --prompt frobnicate
// /tmp/fx/nope.txt` refused with the message that tells you to do exactly that,
// and so did `--prompt "/tmp/fx/nope.txt"` (round 2 of the audit's own advice).
func TestReadInput_PromptFlagMakesTheRefusalSpellingsReal(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.txt")

	in, err := readInput(&config.Config{Prompt: "frobnicate", Positionals: []string{missing}})
	if err != nil {
		t.Fatalf("--prompt TEXT + a missing path-shaped positional refused: %v — the refusal advises this spelling, so it must run", err)
	}
	if in.prompt != "frobnicate" {
		t.Errorf("prompt=%q, want the --prompt text", in.prompt)
	}

	// FC2: --prompt naming the path ITSELF is text-by-declaration, and that is
	// the only way to send a path-shaped string as the prompt deliberately.
	in2, err := readInput(&config.Config{Prompt: missing, Positionals: []string{missing}})
	if err != nil {
		t.Fatalf("--prompt <the path> refused: %v", err)
	}
	if in2.prompt != missing {
		t.Errorf("prompt=%q, want %q", in2.prompt, missing)
	}
}

// TestReadInput_ProseAndPlainWordsStillRun — the positive controls. A plain
// word and a sentence (even one carrying a path) stay PROMPT TEXT: the
// refusal is about the token's shape, never a guess at intent.
func TestReadInput_ProseAndPlainWordsStillRun(t *testing.T) {
	for _, token := range []string{
		"frobnicate",
		"test",
		"check the tests",
		"your task is written in image ~/x.png",
		"see /tmp/nonexistent.png for details",
	} {
		in, err := readInput(&config.Config{Positionals: []string{token}})
		if err != nil {
			t.Errorf("readInput(%q) refused: %v — prose must keep running", token, err)
			continue
		}
		if in.prompt != token {
			t.Errorf("readInput(%q) prompt=%q", token, in.prompt)
		}
	}
}

// TestReadInput_ExistingPathShapesUnchanged — the file half of the contract:
// an existing text file is read, an existing image is attached, a DIRECTORY
// positional keeps its own (pre-existing) refusal text, and `--prompt` beside
// a path-shaped positional still makes that positional a FILE argument.
func TestReadInput_ExistingPathShapesUnchanged(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(txt, []byte("fix the bug\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	in, err := readInput(&config.Config{Positionals: []string{txt}})
	if err != nil {
		t.Fatalf("existing text file refused: %v", err)
	}
	if in.prompt != "fix the bug" {
		t.Errorf("prompt=%q", in.prompt)
	}

	img := writeTestPNG(t, dir, "shot.png")
	in, err = readInput(&config.Config{Prompt: "describe", Positionals: []string{img}})
	if err != nil {
		t.Fatalf("existing image refused: %v", err)
	}
	if len(in.images) != 1 {
		t.Errorf("images=%d, want 1", len(in.images))
	}

	// A directory positional: the pre-existing refusal, unchanged (it is not a
	// text file and not an image — the read error stands).
	if _, err := readInput(&config.Config{Positionals: []string{dir}}); err == nil {
		t.Errorf("a directory positional must still be refused")
	} else if !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("the directory refusal text changed: %s", err)
	}
}
