package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

// The single-source gate for the chat/session placeholder word.
//
// THE DEFECT THIS CLOSES. The value had two spellings: the unexported
// `chatNoTextWord` in internal/tools/chat.go and a bare literal in
// cmd/agent/cli_session.go's `session show` renderer. It was re-typed for a
// structural reason — an unexported const cannot be read across packages — so
// the *fix* is the exported alias (tools.ChatNoTextWord) AND this gate, because
// "the cmd layer now imports it" is a fact about today's call site, not a
// property of the tree. Rule 1/13/14. Precedent: the mirror gate
// TestUnknownNode_MirrorLiteralGate (internal/tools/memory_sync_task_test.go).
const (
	// noTextWordOwner is the ONE production file permitted to carry the value
	// as a string literal: the const's definition site.
	noTextWordOwner = "internal/tools/chat.go"
	// noTextWordConsumer is the file that must REFERENCE the constant. It is
	// held to the stricter rule — no spelling of the value at all, quoted or
	// not — because it renders the placeholder and is where the bare literal
	// was planted.
	noTextWordConsumer = "cmd/agent/cli_session.go"
	// noTextWordDeclPrefix is the declaration the value is extracted from: the
	// gate reads its needle FROM the owner's source (Rule 38/avoid-a-copy: a
	// hand-typed expectation here would pass vacuously the day the word
	// changes).
	noTextWordDeclPrefix = "const chatNoTextWord = "
	// noTextWordScanFloor refuses a scan that reached nothing: a green over an
	// empty walk is indistinguishable from a clean tree (Rule 15).
	noTextWordScanFloor = 1000
	// noTextWordSkipDirVendor is never production Go.
	noTextWordSkipDirVendor = "vendor"
)

// chatNoTextWordValue extracts the placeholder from the owner file's own
// declaration, so the needle below cannot drift from the value it guards.
func chatNoTextWordValue(t *testing.T, ownerSrc string) string {
	t.Helper()
	i := strings.Index(ownerSrc, noTextWordDeclPrefix)
	if i < 0 {
		t.Fatalf("%s carries no %q declaration — the gate cannot derive the value it guards",
			noTextWordOwner, strings.TrimSpace(noTextWordDeclPrefix))
	}
	rest := ownerSrc[i+len(noTextWordDeclPrefix):]
	end := strings.IndexByte(rest, '\n')
	if end < 0 {
		end = len(rest)
	}
	v, err := strconv.Unquote(strings.TrimSpace(rest[:end]))
	if err != nil {
		t.Fatalf("%s: the declaration's value is not a quoted string literal: %v", noTextWordOwner, err)
	}
	return v
}

// TestChatNoTextWordIsSingleSourced walks EVERY production .go file under the
// repository root and asserts the value spelled as a string literal appears in
// exactly one of them (the const's definition site). The consumer file must
// carry no spelling of it at all: it references tools.ChatNoTextWord.
func TestChatNoTextWordIsSingleSourced(t *testing.T) {
	root := filepath.Join("..", "..")
	ownerSrc, err := os.ReadFile(filepath.Join(root, noTextWordOwner))
	if err != nil {
		t.Fatalf("read the owner file: %v", err)
	}
	word := chatNoTextWordValue(t, string(ownerSrc))
	needle := `"` + word + `"`

	if tools.ChatNoTextWord != word {
		t.Fatalf("tools.ChatNoTextWord = %q but %s defines %q — the exported name must ALIAS the unexported one, not re-type the string",
			tools.ChatNoTextWord, noTextWordOwner, word)
	}

	var scanned, holders int
	var offences []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			// Dot-directories (.git, .gopi, editor state) hold no production Go.
			if path != root && (strings.HasPrefix(name, ".") || name == noTextWordSkipDirVendor) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		switch rel {
		case noTextWordOwner:
			if strings.Contains(string(src), needle) {
				holders++
			} else {
				offences = append(offences, rel+": the definition site no longer carries "+needle+" — this gate is now guarding a word nothing defines")
			}
		case noTextWordConsumer:
			if strings.Contains(string(src), word) {
				offences = append(offences, rel+": carries the placeholder inline at "+chatNoTextWordLine(string(src), word)+" — reference tools.ChatNoTextWord (defined in "+noTextWordOwner+")")
			}
		default:
			if strings.Contains(string(src), needle) {
				offences = append(offences, rel+": carries the literal "+needle+" at "+chatNoTextWordLine(string(src), needle)+" — reference tools.ChatNoTextWord (defined in "+noTextWordOwner+")")
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}
	if scanned < noTextWordScanFloor {
		t.Fatalf("scanned %d production .go files, floor is %d — a walk that reached nothing cannot pass", scanned, noTextWordScanFloor)
	}
	if holders != 1 {
		offences = append(offences, noTextWordOwner+": definition site counted "+strconv.Itoa(holders)+" times, want exactly 1")
	}
	for _, o := range offences {
		t.Error(o)
	}
	if t.Failed() {
		t.Logf("scanned %d production .go files; the ONE holder is %s", scanned, noTextWordOwner)
	}
}

// chatNoTextWordLine names the first line number carrying needle, so a failure
// points at the line to edit instead of at a whole file.
func chatNoTextWordLine(src, needle string) string {
	for i, ln := range strings.Split(src, "\n") {
		if strings.Contains(ln, needle) {
			return "line " + strconv.Itoa(i+1)
		}
	}
	return "an unlocated line"
}

// TestSessionShowRendersTheSharedPlaceholder is the behavioural half: the gate
// above proves the cmd layer READS the constant, this proves the rendered page
// actually carries its value — a reference that is assigned to nothing would
// pass the source scan and still print an empty head.
func TestSessionShowRendersTheSharedPlaceholder(t *testing.T) {
	out := renderSessionDetail(tools.ChatSessionDetail{SessionID: "cli-no-text-33333333"})
	firstLine := strings.SplitN(out, "\n", 2)[0]
	if firstLine != "session cli-no-text-33333333" {
		t.Fatalf("the page head did not render as the id line: %q", firstLine)
	}
	if !strings.Contains(out, tools.ChatNoTextWord) {
		t.Errorf("a session with neither a title nor a first message does not render %q:\n%s", tools.ChatNoTextWord, out)
	}
}
