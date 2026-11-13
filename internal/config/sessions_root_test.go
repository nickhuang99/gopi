package config

import (
	"path/filepath"
	"testing"
)

// TestSessionsRootIsOneResolution pins the ONE resolution every session writer
// and reader goes through: the relocation channel when it is set, and the
// documented default otherwise.
//
// It is the seam's own contract test, and it is written as a pair because the
// two halves are the defect: a resolution that honoured the override while
// losing the default would move every ordinary run out of its profile, and one
// that lost the override would put a relocated run's session back in the
// operator's own tree (the flood this channel exists to stop).
func TestSessionsRootIsOneResolution(t *testing.T) {
	home := t.TempDir()
	t.Setenv(GopiDirEnv, home)
	t.Setenv(SessionsRootEnv, "")

	def, err := SessionsRoot()
	if err != nil {
		t.Fatalf("SessionsRoot (default): %v", err)
	}
	want := filepath.Join(home, ProfileDirName, SessionsDirName)
	if def != want {
		t.Fatalf("SessionsRoot() = %q, want the profile's own sessions tree %q", def, want)
	}

	relocated := t.TempDir()
	t.Setenv(SessionsRootEnv, relocated)
	got, err := SessionsRoot()
	if err != nil {
		t.Fatalf("SessionsRoot (relocated): %v", err)
	}
	// VERBATIM, not joined: a caller that exports a root means that root is the
	// root (see the function's comment). A "clever" join would answer
	// <sink>/sessions and the writer would land somewhere the caller never
	// named.
	if got != relocated {
		t.Fatalf("SessionsRoot() = %q with %s=%q, want the value VERBATIM", got, SessionsRootEnv, relocated)
	}
}

// TestSessionsRootInMatchesTheProfileJoin is the anti-drift half of Rule 14: the
// explicit-argument join and the resolved default must be the SAME arithmetic,
// or the two spellings of "where sessions live" would part company.
func TestSessionsRootInMatchesTheProfileJoin(t *testing.T) {
	home := t.TempDir()
	t.Setenv(GopiDirEnv, home)
	t.Setenv(SessionsRootEnv, "")

	resolved, err := SessionsRoot()
	if err != nil {
		t.Fatalf("SessionsRoot: %v", err)
	}
	gd, err := GopiDir()
	if err != nil {
		t.Fatalf("GopiDir: %v", err)
	}
	if got := SessionsRootIn(gd); got != resolved {
		t.Fatalf("SessionsRootIn(GopiDir()) = %q but SessionsRoot() = %q — one join, two spellings", got, resolved)
	}
}
