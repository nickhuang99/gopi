package taskreport

// own_test.go — the OWNERSHIP PREDICATE's proof (epic
// nested-report-path-clobber, 2026-09-15). The predicate is the single decision
// every face of the fix shares (Rule 14), so it is tested here rather than
// re-derived at each caller: a bug in it would either destroy a parent's report
// (too permissive) or break the hand-off that writes every report at all (too
// strict), and both failures are silent.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOwnedBy(t *testing.T) {
	base := t.TempDir()
	own := filepath.Join(base, "sessions", "own-session")
	other := filepath.Join(base, "sessions", "other-session")
	loose := filepath.Join(base, "loose")
	for _, d := range []string{own, other, loose} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("prepare %s: %v", d, err)
		}
	}

	cases := []struct {
		name    string
		path    string
		owner   string
		dir     string
		want    bool
		because string
	}{
		{
			name: "destination inside the session dir, no marker", path: filepath.Join(own, FileName),
			owner: "", dir: own, want: true,
			because: "the harness exports no marker — the directory IS the claim",
		},
		{
			name: "destination inside the session dir, marker agrees", path: filepath.Join(own, FileName),
			owner: filepath.Base(own), dir: own, want: true,
			because: "marker and directory agree",
		},
		{
			name: "destination in another session dir", path: filepath.Join(other, FileName),
			owner: "", dir: own, want: false,
			because: "another session's directory is another session's report",
		},
		{
			name: "destination outside any session dir", path: filepath.Join(loose, FileName),
			owner: "", dir: own, want: false,
			because: "not this session's ground",
		},
		{
			name: "marker names another session", path: filepath.Join(own, FileName),
			owner: filepath.Base(other), dir: own, want: false,
			because: "a foreign marker is decisive even inside the directory",
		},
		{
			name: "no session dir of its own", path: filepath.Join(own, FileName),
			owner: "", dir: "", want: false,
			because: "with no directory of its own the process can prove nothing",
		},
		{
			name: "marker without a destination", path: "",
			owner: filepath.Base(own), dir: own, want: false,
			because: "a marker names no destination to write",
		},
		{
			name: "empty report path", path: "", owner: "", dir: own, want: false,
			because: "an empty path is not a destination",
		},
		{
			name: "padded marker and path still match", path: " " + filepath.Join(own, FileName) + " ",
			owner: "  " + filepath.Base(own) + " ", dir: own, want: true,
			because: "a launcher's whitespace must not turn its own contract into a foreign one",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OwnedBy(tc.path, tc.owner, tc.dir); got != tc.want {
				t.Fatalf("OwnedBy(%q, %q, %q) = %v, want %v — %s", tc.path, tc.owner, tc.dir, got, tc.want, tc.because)
			}
		})
	}
}

// TestOwnedByResolvesEquivalentSpellings: a symlinked or relative spelling of
// the session's own directory is the same directory — refusing it would break
// a legitimate hand-off for a cosmetic reason.
func TestOwnedByResolvesEquivalentSpellings(t *testing.T) {
	base := t.TempDir()
	sessions := filepath.Join(base, "sessions")
	own := filepath.Join(sessions, "own-session")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatalf("prepare %s: %v", own, err)
	}
	link := filepath.Join(base, "sessions-link")
	if err := os.Symlink(sessions, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	viaLink := filepath.Join(link, "own-session", FileName)
	if !OwnedBy(viaLink, "", own) {
		t.Fatalf("OwnedBy(%q, \"\", %q) = false — a symlinked spelling of the session's own directory is still its own", viaLink, own)
	}
}

// TestContractPresentAndEnvValue: the two env readers the faces share. Presence
// is the fact (an empty value still means a launcher exported the name), and a
// duplicate name resolves to the LAST entry — this helper's own scan, not a
// claim about how a child's loader reads it (tools.ChildLocaleDuplicateKeyRule).
func TestContractPresentAndEnvValue(t *testing.T) {
	empty := []string{"A=1"}
	if ContractPresent(empty) {
		t.Fatalf("ContractPresent reported a contract in %v", empty)
	}
	valueless := []string{PathEnv + "="}
	if !ContractPresent(valueless) {
		t.Fatalf("a present-but-empty %s was reported as absent — presence is the fact, not truthiness", PathEnv)
	}
	dup := []string{PathEnv + "=/first", PathEnv + "=/last"}
	if got := EnvValue(dup, PathEnv); got != "/last" {
		t.Fatalf("EnvValue on a duplicate name = %q, want the LAST entry (/last) — it is the one the child receives", got)
	}
	if got := EnvName("A=b=c"); got != "A" {
		t.Fatalf("EnvName(\"A=b=c\") = %q, want \"A\" (split on the FIRST separator)", got)
	}
}
