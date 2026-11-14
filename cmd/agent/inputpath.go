package main

// inputpath.go — the PATH-SHAPE contract of a bare positional (end-user audit
// slice 8, finding RP-2, F2).
//
// The defect it closes (measured 2026-09-26 by the audit slice 8 run, control
// `@PATH`):
//
//	gopi --base-url http://127.0.0.1:18123 --db <sb>/.gopi/memory.db /tmp/fx/nope.txt
//	→ rc=0: a real run started, a session dir + a ledger row were created, and
//	  the PATH STRING became the task text sent to the model
//	gopi … @/tmp/fx/nope.txt
//	→ rc=1: `Error reading @ payload file "…": no such file or directory …`
//
// Two spellings of ONE input cannot have opposite failure semantics in silence.
// `gopi -h` Usage line 2 (`gopi [options] input.txt  Run one task read from a
// text file`) and the `image.png` Inputs row ("a positional naming an EXISTING
// .png/… file") describe a file positional as READING a file, so a
// path-shaped positional that names nothing is refused by name, before any
// session or ledger row, exactly like its `@PATH` twin.
//
// WHAT IS REFUSED — and what must keep running. The classification is
// deliberately NARROW (a refusal here is a statement about the token's SHAPE,
// never a guess about what the operator meant):
//
//	refused   /abs/path.txt  ./missing.md  sub/dir/note.txt  ~/x.png
//	          /tmp/fx/nope     (a separator: a slash-separated spelling with no
//	                            extension IS a path — `./frobnicate` is a path)
//	          missing.png      (the documented attachment extension, no separator)
//	runs      frobnicate     plain prose, no separator, no known extension
//	          "check the tests and fix the failing one"     prose
//	          "see /tmp/nonexistent.png for details"        prose with a path
//	                                                        MENTION (whitespace)
//	          "x/y"           — an internal detail needing no guard, see below
//
// A token containing WHITESPACE is never judged: a bare positional that is a
// whole sentence is prompt text by construction (`TestReadInput_LiteralPromptText`
// and `TestReadInput_ImageExtensionWithoutFile` pin the two documented prose
// shapes, both of which mention a path inside a sentence).

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/tools"
)

// pathSeparator is the one character that makes a token a slash-separated
// spelling — the POSIX/URL separator gopi's own surfaces are written in. It is
// the discriminator for tokens with no extension (`./frobnicate`, `/tmp/fx/nope`),
// and one of the two shapes the `gopi -h` Inputs row names.
const pathSeparator = "/"

// literalPromptEscape is the escape spelling that makes a prompt starting with
// the payload prefix (`@`) literal. It is ONE definition (Rule 1b/13): the
// refusal message tells the operator this spelling, and every reader of the
// `@`-prefix syntax (readInput, readPayloadFile) compares against it.
const literalPromptEscape = "\\@"

// escapeBackslash is the escape character itself, the prefix stripped when a
// `\@…` spelling is turned back into its literal text. Named for the same
// reason as literalPromptEscape: it is a value that is COMPARED and STRIPPED,
// not an incidental byte in a string.
const escapeBackslash = "\\"

// prosePathExtensions are the file extensions that make a SEPARATOR-LESS token
// path-shaped. They are limited to the names the help page and the vision path
// already use as file vocabulary — the `image.png` Inputs row, whose extension
// set lives in internal/tools (visionMediaTypes), plus the text-payload
// extensions the Usage lines spell (`input.txt`) and the Inputs row's own
// exemplar (`./PROJECT.md`, the tracked project contract; it was `./GOPI.md`
// before the rules file was split — the extension set is unchanged). A token ending in one of these has already been
// DECLARED a file by the page the operator read, so a missing one is a refusal.
//
// The narrowness is the point: an arbitrary "this looks like a filename"
// heuristic is not decidable from a word, and a wrong guess is worse than an
// unguarded path — a token whose shape says nothing keeps its old meaning
// (prompt text).
var prosePathExtensions = []string{
	".txt",
	".md",
}

// pathShapedReason reports why a bare positional is path-shaped — the file
// spelling the operator is being refused for — or "" when the token is prompt
// TEXT.
//
// The second return is a human reason for the SHAPE, used by the refusal text
// so it names the datum the operator can act on (a separator, or an extension).
func pathShapedReason(token string) string {
	// Whitespace ⇒ a sentence, never a path token. A positional with a space is
	// prompt text whatever else it contains — the two documented prose controls
	// (`your task is written in image ~/x.png`, `see /tmp/nonexistent.png for
	// details`) both carry a path inside prose and must keep running.
	if token == "" || strings.ContainsAny(token, " \t\n\r") {
		return ""
	}
	if strings.Contains(token, pathSeparator) {
		return fmt.Sprintf("it contains the path separator %q", pathSeparator)
	}
	if tools.IsImagePath(token) {
		return fmt.Sprintf("it ends in the image extension %q", filepath.Ext(token))
	}
	ext := strings.ToLower(filepath.Ext(token))
	for _, known := range prosePathExtensions {
		if ext == known {
			return fmt.Sprintf("it ends in the file extension %q", ext)
		}
	}
	return ""
}

// missingPathPositional returns the refusal for a path-shaped positional that
// names nothing on disk, or nil when the run may proceed. The message mirrors
// its `@PATH` twin: it names the path, the reason (the file does not exist,
// with the SHAPE that made the token a path — the operator's actionable datum),
// and BOTH legal spellings for the operator who really does want the text —
// `--prompt TEXT` and the `\@` escape — plus the `@PATH` spelling when the
// operator meant a file that does exist under another name.
//
// The sentence is written as one grammatical clause per fact (`no such file at
// "%q" (%s) — …`), because the shape reason is a phrase like `it contains the
// path separator "/"`: a template that simply concatenated `%q` and the phrase
// rendered `"/tmp/fx/nope.txt" it contains …`, which is not a sentence a user
// should have to parse (measured on the real binary, probe `bare_missing`).
func missingPathPositional(token, shapeWhy string) error {
	return fmt.Errorf("no such file at %q (%s) — gopi reads a path-shaped positional as a FILE, so this is refused rather than sent to the model as prompt text. Spell the file with %s%s, or pass the text deliberately: `--prompt %q` (or the escaped literal %s%s)",
		token, shapeWhy, payloadFilePrefix, token, token, literalPromptEscape, token)
}
