package sessionevent

// Rule 13 exhaustive literal gate: the session-event names exist as the
// constants in events.go — and are REFERENCED, not bypassed. A constant that
// is defined once and bypassed at N call sites is a defect: the constant gets
// renamed (or fixed) and the bypassed sites keep the old spelling, so the
// writer and the reader silently stop matching.
//
// This gate is deliberately REPO-WIDE, not package-scoped: the whole point of
// the leaf package is that the value crosses package boundaries
// (internal/agent writes it, internal/gopilog / internal/tools /
// internal/testutil read it), and a package-scoped grep would have missed the
// very files that carried the literal before. The single allowed holder is
// the definition file itself.
//
// The scan is AST-based (go/parser), not a raw byte grep, for two reasons:
//   - precision: a COMMENT explaining the event (or a doc example) is not a
//     bypass and must not be reported, while every string literal — including
//     a raw-string probe like []byte(`"session_start"`) — is;
//   - exhaustiveness: a file that does not parse is a hard failure instead of
//     a silently skipped file.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gateDefinitionFile is the ONE production file allowed to carry the
// literals: their single definition site (repo-root-relative, slash path).
const gateDefinitionFile = "internal/sessionevent/events.go"

// gateLiterals are the session-event wire values exactly as they appear in
// source: a quoted JSON string value. A literal whose decoded bytes CONTAIN
// this sequence is a bypass — that covers both `"session_start"` and the
// raw-string probe form `"session_start"` inside backticks.
var gateLiterals = []string{
	`"` + EventTypeSessionStart + `"`,
	`"` + EventTypeSessionEnd + `"`,
}

// gateSkipDirs are directories that never carry first-party production Go
// sources (or are prohibitively large): skipping them keeps the walk bounded.
var gateSkipDirs = map[string]bool{
	".git":         true,
	"testdata":     true,
	"vendor":       true,
	"node_modules": true,
}

// repoRootFrom walks up from dir until it finds the module root (the
// directory holding go.mod) — so the gate scans the same tree no matter which
// package's directory the test binary runs in.
func repoRootFrom(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	dir = abs
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// gateScanResult is what scanSessionEventLiterals reports: the offending
// (file:literal) pairs plus how many production files were actually parsed.
// A zero scan is a vacuous pass, so the caller asserts it is non-zero.
type gateScanResult struct {
	hits    []string
	scanned int
}

// fileCarriesGateLiteral reports the gate literals whose quoted form occurs
// inside one of src's string literals. Comments never count (they are not in
// the AST).
func fileCarriesGateLiteral(path string, src []byte) (hits []string, parseErr error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		for _, g := range gateLiterals {
			if strings.Contains(lit.Value, g) {
				hits = append(hits, g)
			}
		}
		return true
	})
	return hits, nil
}

// scanSessionEventLiterals walks root and returns every production .go file
// (excluding _test.go and the definition file) that still types a session
// event name as a string literal.
func scanSessionEventLiterals(root string) (gateScanResult, error) {
	var res gateScanResult
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if gateSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if rel == gateDefinitionFile {
			return nil // the ONE allowed holder
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			res.hits = append(res.hits, rel+": unreadable: "+rerr.Error())
			return nil
		}
		res.scanned++
		hits, perr := fileCarriesGateLiteral(path, src)
		if perr != nil {
			res.hits = append(res.hits, rel+": unparseable: "+perr.Error())
			return nil
		}
		for _, h := range hits {
			res.hits = append(res.hits, rel+" carries "+h)
		}
		return nil
	})
	return res, err
}

// TestSessionEventWireValues pins the serialized VALUES. The constants are
// referenced from one place now, so a future rename is a one-line change —
// which is exactly why the one line needs a guard: these strings are the
// persisted wire format of every session JSONL on disk (and of files written
// by older binaries), read back by `gopi session log`, `gopi session stats`, `gopi task show`
// and the chat backfill scan. Changing a value here without a migration would
// make every already-recorded session unreadable, silently.
func TestSessionEventWireValues(t *testing.T) {
	want := map[string]string{
		EventTypeSessionStart: "session_start",
		EventTypeSessionEnd:   "session_end",
	}
	for got, expect := range want {
		if got != expect {
			t.Errorf("wire value changed: got %q, want %q — session JSONL files already on disk use the old value; a change needs a migration",
				got, expect)
		}
	}
}

// TestSessionEventLiteralGate is the anti-regression gate: no production Go
// file outside the definition site may re-inline a session-event name.
func TestSessionEventLiteralGate(t *testing.T) {
	root, err := repoRootFrom(".")
	if err != nil {
		t.Fatalf("cannot locate module root from %q: %v", ".", err)
	}
	res, err := scanSessionEventLiterals(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	for _, h := range res.hits {
		t.Errorf("Rule 13 violation: %s — reference sessionevent.EventType* instead (defined only in %s)",
			h, gateDefinitionFile)
	}
	if res.scanned == 0 {
		t.Fatalf("gate parsed no production file under %s — the check would pass vacuously", root)
	}
	if res.scanned < 50 {
		t.Fatalf("gate parsed only %d production .go files under %s — the walk is not repo-wide", res.scanned, root)
	}
}

// TestSessionEventLiteralGateScansWholeRepo proves the gate is not
// accidentally scoped to the leaf package's own directory (a package-local
// grep would have missed every file that carried the literal before this
// change): the walk must reach the writer (internal/agent) and the readers
// (internal/gopilog, internal/tools, internal/testutil), and the definition
// file must live in the tree the walk visits.
func TestSessionEventLiteralGateScansWholeRepo(t *testing.T) {
	root, err := repoRootFrom(".")
	if err != nil {
		t.Fatalf("cannot locate module root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, gateDefinitionFile)); err != nil {
		t.Fatalf("definition file %s not found under %s: %v", gateDefinitionFile, root, err)
	}
	for _, dir := range []string{"internal/agent", "internal/gopilog", "internal/tools", "internal/testutil"} {
		if st, err := os.Stat(filepath.Join(root, dir)); err != nil || !st.IsDir() {
			t.Fatalf("consumer package %s missing under %s: %v", dir, root, err)
		}
	}
}

// TestSessionEventLiteralGateHasTeeth plants a literal in a scratch file and
// asserts the scanner reports it — proving the gate FAILS when a future
// change re-inlines the value (a gate that cannot fail is decoration). Both
// bypass forms are planted: a plain quoted literal and the raw-string probe.
func TestSessionEventLiteralGateHasTeeth(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "reoffender.go")
	body := "package x\n\n" +
		"var a = " + gateLiterals[0] + "\n" +
		"var b = []byte(`" + gateLiterals[1] + "`)\n"
	if err := os.WriteFile(plain, []byte(body), 0o644); err != nil {
		t.Fatalf("write planted file: %v", err)
	}
	res, err := scanSessionEventLiterals(dir)
	if err != nil {
		t.Fatalf("scan %s: %v", dir, err)
	}
	if res.scanned != 1 {
		t.Fatalf("scanned %d files, want 1 (the planted one)", res.scanned)
	}
	if len(res.hits) != 2 {
		t.Fatalf("gate reported %v, want 2 hits (plain + raw-string probe)", res.hits)
	}
}

// TestSessionEventLiteralGateIgnoresComments is the precision half of the
// gate contract: prose that QUOTES the event name (a doc comment, a doc
// example) is not a bypass — only a string literal in code is. Without this
// the gate would force every explanatory comment to spell the name
// unquoted, which is worse documentation for no safety gain.
func TestSessionEventLiteralGateIgnoresComments(t *testing.T) {
	dir := t.TempDir()
	commented := filepath.Join(dir, "documented.go")
	body := "package x\n\n" +
		"// the " + gateLiterals[0] + " event is the first line of the JSONL\n" +
		"var ok = 1\n"
	if err := os.WriteFile(commented, []byte(body), 0o644); err != nil {
		t.Fatalf("write planted file: %v", err)
	}
	res, err := scanSessionEventLiterals(dir)
	if err != nil {
		t.Fatalf("scan %s: %v", dir, err)
	}
	if len(res.hits) != 0 {
		t.Fatalf("gate flagged a comment: %v (comments are not bypasses)", res.hits)
	}
}
