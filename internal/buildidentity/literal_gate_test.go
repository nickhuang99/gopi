package buildidentity_test

// Rule 13 exhaustive literal gate for the build-metadata keys.
//
// THE DEFECT THIS PINS (measured 2026-10-16, task 01a1362e): writing this
// package's own consumer, internal/sessionevent/artifact.go, typed
// `const buildInfoKeyRevision = "vcs.revision"` — a SECOND definition site of a
// key internal/version already declares, with a comment claiming it "cannot be
// derived from a constant in this tree". The key is read back by three
// production packages (internal/version's printer, internal/tools' image
// self-check, internal/sessionevent's session record); two spellings is exactly
// the drift Rule 13/14 exists to stop, and NOTHING caught it — no gate scanned
// this literal.
//
// The scan is AST-BASED, not a byte grep, and that is not a refinement here: the
// literal is DISCUSSED in comments and doc strings all over the production tree
// (internal/version/self.go, cmd/agent/cli_version.go, this package's own
// header). A byte-level gate would drown in false positives and be muted; a
// comment is not in the AST, a string literal is.
//
// The gate is REPO-WIDE for the same reason internal/sessionevent's is: the
// whole point of a leaf constant package is that the value crosses package
// boundaries, so a package-scoped scan would miss the consumers that matter.
//
// TESTS ARE NOT SCANNED, deliberately: a test's job is often to spell the
// literal and assert against the REAL toolchain output (`go version -m` prints
// "vcs.revision"; cmd/agent/cli_version_stamp_test.go parses exactly that). The
// discipline is about production code — the code that must agree with itself.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/buildidentity"
)

// gateDefinitionFile is the ONE production file allowed to carry the literals:
// their single definition site (repo-root-relative, slash path).
const gateDefinitionFile = "internal/buildidentity/buildidentity.go"

// gateSkipDirs are directories that never carry first-party production Go
// sources (or are prohibitively large): skipping them bounds the walk.
var gateSkipDirs = map[string]bool{
	".git":         true,
	"testdata":     true,
	"vendor":       true,
	"node_modules": true,
}

// gateLiterals is the quoted form of each shared KEY, taken FROM the constants
// rather than re-typed — so the gate follows a value change instead of pinning a
// spelling that no longer exists.
//
// BuildInfoValueModifiedTrue is deliberately ABSENT: its value is `"true"`, and
// an exhaustive gate on that literal reports every unrelated boolean string in
// the tree (measured on the first run of this gate: internal/scratch/tmproot.go,
// internal/text2img/render.go, internal/tools/tasks.go … ). A gate that fires on
// a hundred false positives is a gate someone mutes, which is worse than the
// narrow gap. What matters for the value is that it agrees with what the
// toolchain writes, and that is asserted directly (TestBuildInfoValuesAgree).
var gateLiterals = []string{
	`"` + buildidentity.BuildInfoKeyRevision + `"`,
	`"` + buildidentity.BuildInfoKeyModified + `"`,
	`"` + buildidentity.BuildInfoKeyTime + `"`,
}

// TestBuildInfoKeysHaveOneDefinitionSite FAILS the build if any production file
// outside gateDefinitionFile types one of the keys as a string literal.
func TestBuildInfoKeysHaveOneDefinitionSite(t *testing.T) {
	root, err := repoRootFrom(".")
	if err != nil {
		t.Fatalf("locate module root: %v", err)
	}
	hits, scanned, err := scanForBuildInfoLiterals(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	// A zero-file scan is a VACUOUS PASS: it would report green on a wrong root,
	// an unreadable tree, or a walk that skipped everything. Refuse it (Rule 15
	// — a scan that found nothing must be distinguishable from a scan that never
	// ran).
	const minScanned = 100
	if scanned < minScanned {
		t.Fatalf("scanned only %d production .go files under %s — the walk did not reach the tree, so this gate proves nothing",
			scanned, root)
	}
	for _, h := range hits {
		t.Errorf("%s — reference the constant (defined in %s, re-exported by internal/version) instead of re-typing the key",
			h, gateDefinitionFile)
	}
}

// repoRootFrom walks up from dir until it finds the module root (the directory
// holding go.mod), so the gate scans the same tree whichever package directory
// the test binary runs in.
func repoRootFrom(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for d := abs; ; {
		if _, statErr := os.Stat(filepath.Join(d, "go.mod")); statErr == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", os.ErrNotExist
		}
		d = parent
	}
}

// scanForBuildInfoLiterals returns the offending file:literal pairs plus how
// many production files were actually parsed.
func scanForBuildInfoLiterals(root string) (hits []string, scanned int, err error) {
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
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
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if rel == gateDefinitionFile {
			return nil // the ONE allowed holder
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			hits = append(hits, rel+": unreadable: "+readErr.Error())
			return nil
		}
		scanned++
		fileHits, parseErr := buildInfoLiteralsIn(path, src)
		if parseErr != nil {
			// An unparseable file is a FAILURE, not a skip: silently passing it
			// is how a gate loses coverage without saying so.
			hits = append(hits, rel+": unparseable: "+parseErr.Error())
			return nil
		}
		for _, h := range fileHits {
			hits = append(hits, rel+" carries "+h)
		}
		return nil
	})
	return hits, scanned, walkErr
}

// buildInfoLiteralsIn reports which gate literals occur inside one of src's
// string literals. Comments never count — they are not in the AST.
func buildInfoLiteralsIn(path string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	var hits []string
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

// TestBuildInfoWireValues pins the serialized VALUES of the shared keys.
//
// These strings are not gopi's to choose: they are the TOOLCHAIN's wire names,
// read back out of a real artifact's `runtime/debug.BuildInfo` by every consumer
// (and printed by `go version -m`). Because they now travel through a leaf
// constant package, a rename is a one-line change that every call site silently
// follows — which is exactly why the one line needs a guard: a wrong value
// compiles, passes the literal gate (which reads the constant, not the toolchain)
// and simply never matches a real binary again.
func TestBuildInfoWireValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"BuildInfoKeyRevision", buildidentity.BuildInfoKeyRevision, "vcs.revision"},
		{"BuildInfoKeyTime", buildidentity.BuildInfoKeyTime, "vcs.time"},
		{"BuildInfoKeyModified", buildidentity.BuildInfoKeyModified, "vcs.modified"},
		{"BuildInfoValueModifiedTrue", buildidentity.BuildInfoValueModifiedTrue, "true"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q — this value is the toolchain's wire format, not a gopi choice: a wrong value silently stops matching every real artifact",
				tc.name, tc.got, tc.want)
		}
	}
}
