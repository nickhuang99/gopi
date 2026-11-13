package runend

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReasonVocabulary pins the ONE mapping every reader and writer shares:
// an engine-forced end is truncated, a model finish is not, and the ledger
// spelling round-trips through the marker text (TokenIn is the reader half —
// `gopi task show` and the wrapper's writeback both call it).
func TestReasonVocabulary(t *testing.T) {
	engine := []Reason{WallclockCap, StuckLoop, StuckToolLoop, MaxIterations, RunContextCancelled, DeadlineExceeded}
	for _, r := range engine {
		if !r.Truncated() {
			t.Errorf("%q must be an engine-forced (truncated) end", r)
		}
		d := r.Detail()
		if !strings.Contains(d, TruncationMarker) {
			t.Errorf("%q detail %q must carry the shared marker %q", r, d, TruncationMarker)
		}
		if got := TokenIn(d); got != r.Token() {
			t.Errorf("TokenIn(%q) = %q, want %q — the reader must recover the token the writer spelled", d, got, r.Token())
		}
		if got := TokenIn(r.ReasonText()); got != r.Token() {
			t.Errorf("TokenIn(reason column %q) = %q, want %q — the row's reason slot must round-trip too", r.ReasonText(), got, r.Token())
		}
		if r.ReasonText() == "" {
			t.Errorf("%q must state its WHY in the reason column", r)
		}
	}

	// The model's own finish is NOT a truncation and records nothing: every
	// ordinary run's digest and reason column stay exactly as they were.
	if Finished.Truncated() {
		t.Error("a model finish is not an engine-forced end")
	}
	if d := Finished.Detail(); d != "" {
		t.Errorf("a finished run records no truncation marker, got %q", d)
	}
	if r := Finished.ReasonText(); r != "" {
		t.Errorf("a finished run must not stamp a reason (the status default applies), got %q", r)
	}
}

// TestTokenInIgnoresUnrelatedDetail: a detail that mentions no truncation (the
// ordinary run digest) must read as no truncation — never a false positive that
// would mark a finished run as truncated.
func TestTokenInIgnoresUnrelatedDetail(t *testing.T) {
	for _, d := range []string{
		"",
		" — elapsed=12.5s iter=7 tokens=4321",
		"run closeout (child normal exit, wrapper writeback never landed)",
		"TRUNCATED run",       // marker without a token: malformed, not a claim
		"TRUNCATED run (oops", // marker, unterminated token
		"gopi run completed — run: elapsed=1s",
	} {
		if got := TokenIn(d); got != "" {
			t.Errorf("TokenIn(%q) = %q, want \"\" (no truncation claim)", d, got)
		}
	}
}

// TestTruncationMarkerHasOneHome: the marker + explanation are a shared
// spelling (Rules 13/14) — a literal mirrored into a sibling package would let
// a writer record a marker the reader no longer recognises, which is exactly
// the class this vocabulary exists to close. The gate walks every production
// .go file under internal/ and cmd/ and fails any that carries the literal
// outside its single definition site.
func TestTruncationMarkerHasOneHome(t *testing.T) {
	root := moduleRoot(t)
	definitionFile := filepath.Join(root, "internal", "runend", "runend.go")

	markers := []struct {
		name  string
		value string
	}{
		{"TruncationMarker", TruncationMarker},
		{"TruncationExplain", TruncationExplain},
	}

	scanned := 0
	for _, dir := range []string{filepath.Join(root, "internal"), filepath.Join(root, "cmd")} {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if path == definitionFile {
				return nil
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			// Compare against the quoted GO SOURCE spelling of the value, so a
			// substring of a longer word cannot trip the gate.
			scanned++
			for _, m := range markers {
				if bytes.Contains(src, []byte(`"`+m.value+`"`)) {
					t.Errorf("%s carries the literal %q — reference runend.%s (defined in %s)",
						relPath(t, root, path), m.value, m.name, relPath(t, root, definitionFile))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	// Read-floor: a broken walk must not read green-blind.
	if scanned < 100 {
		t.Fatalf("scanned only %d production .go files — the walk lost its root, re-point it", scanned)
	}
}

// moduleRoot walks up from the test's cwd to the go.mod root (the gate needs
// the whole tree, not just this package).
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

func relPath(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
