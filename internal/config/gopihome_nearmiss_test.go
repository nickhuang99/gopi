// gopihome_nearmiss_test.go — the LIBRARY half of the near-miss guard: the
// sentinel config.GopiDir returns, and the arms the predicate must NOT touch.
//
// The composite defect (a second, nested profile that no reader of the profile
// you meant ever sees) is pinned end-to-end through the real binary in
// cmd/agent/cli_sandbox_profile_nearmiss_test.go; this file pins the ONE
// predicate every consumer shares (Rule 14), so a caller that resolves the
// profile through the library cannot slip past it either.
//
// TWO MEASURED INCIDENTS, ONE PREDICATE. The predicate began as a test of the
// value's PARENT — a sibling sandbox marker, or a second profile leaf — and the
// arithmetic it refuses is one level ABOVE that window: a GOPI_HOME that names
// a profile is the near miss whatever its parent is. Measured 2026-10-13 on
// sager, `GOPI_HOME=/home/nick/.gopi` (parent `/home/nick`, clean, no marker)
// resolved to /home/nick/.gopi/.gopi, whose memory.db held 2 rows while the real
// ledger held 1376, and `gopi task submit` printed a normal `Task queued: <id>`
// with exit 0. So the cases below come in two families, and the second is the
// one that bit this host.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ownerMarkerPerm is the mode of the sandbox ownership marker this test writes
// to make a directory provably a sandbox (Rule 1a: no bare literal). The real
// marker is written by internal/sandbox; here only its PRESENCE is load-bearing.
const ownerMarkerPerm = 0o600

// sandboxDirFixture materialises the two things a SANDBOX home needs: the
// directory and the ownership marker inside it.
func sandboxDirFixture(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("materialise %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, SandboxOwnerFileName), []byte("{}"), ownerMarkerPerm); err != nil {
		t.Fatalf("write the ownership marker: %v", err)
	}
	return dir
}

// plainDirFixture materialises an ordinary home — no sandbox marker anywhere.
func plainDirFixture(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("materialise %s: %v", dir, err)
	}
	return dir
}

// TestGopiDirRefusesTheSandboxProfileNearMiss — the sentinel: a GOPI_HOME that
// names a sandbox's profile does not resolve silently into a nested profile.
func TestGopiDirRefusesTheSandboxProfileNearMiss(t *testing.T) {
	sb := sandboxDirFixture(t, "sandbox-near-1a2b3c4d")
	t.Setenv(SandboxNestedProfileOptOutEnv, "")
	t.Setenv(GopiDirEnv, ProfileDirIn(sb))

	gd, err := GopiDir()
	if err == nil {
		t.Fatalf("GopiDir accepted %s=%s and resolved %s — the near miss must be refused",
			GopiDirEnv, ProfileDirIn(sb), gd)
	}
	if !strings.Contains(err.Error(), GopiHomeMisuseErrorPrefix) {
		t.Errorf("the refusal must carry the shared prefix %q, got %q", GopiHomeMisuseErrorPrefix, err)
	}
	// The refusal names the canonical value — the whole point of the message.
	if !strings.Contains(err.Error(), GopiDirEnv+"="+sb) {
		t.Errorf("the refusal must name the correction %s=%s, got %q", GopiDirEnv, sb, err)
	}
	// And it created nothing: a refused resolution leaves no directory behind.
	if _, statErr := os.Stat(ProfileDirIn(ProfileDirIn(sb))); statErr == nil {
		t.Errorf("the refused resolution created %s", ProfileDirIn(ProfileDirIn(sb)))
	}
}

// TestGopiDirRefusesAnOrdinaryHomeProfileRoot is the arm the FIRST version of
// this guard deliberately let through, and the one that was measured writing
// rows into a second ledger. The parent carries no sandbox marker, so the
// predicate cannot have asked about it: the value is refused on its own
// arithmetic, and the correction is the profile's parent — whatever it is.
func TestGopiDirRefusesAnOrdinaryHomeProfileRoot(t *testing.T) {
	home := plainDirFixture(t, "ordinary-home")
	value := ProfileDirIn(home)
	t.Setenv(SandboxNestedProfileOptOutEnv, "")
	t.Setenv(GopiDirEnv, value)

	gd, err := GopiDir()
	if err == nil {
		t.Fatalf("GopiDir accepted %s=%s and resolved %s — a profile root is a near miss "+
			"whatever its parent is (the measured 2026-10-13 defect)", GopiDirEnv, value, gd)
	}
	if !strings.Contains(err.Error(), GopiHomeMisuseErrorPrefix) {
		t.Errorf("the refusal must carry the shared prefix %q, got %q", GopiHomeMisuseErrorPrefix, err)
	}
	if !strings.Contains(err.Error(), GopiDirEnv+"="+home) {
		t.Errorf("the refusal must name the correction %s=%s, got %q", GopiDirEnv, home, err)
	}
	// The consequence clause must describe THIS shape honestly: there is no
	// sandbox here, so the message must not claim a sandbox symptom (Rule 41).
	if strings.Contains(err.Error(), SandboxOwnerFileName) {
		t.Errorf("the refusal described a sandbox symptom for a value with no sandbox in it: %q", err)
	}
	if _, statErr := os.Stat(ProfileDirIn(ProfileDirIn(home))); statErr == nil {
		t.Errorf("the refused resolution created %s", ProfileDirIn(ProfileDirIn(home)))
	}
}

// TestGopiDirNearMissOptOutIsHonoured — the operator's deliberate switch is
// obeyed (and, in cmd/agent, announced: a degradation is never silent, Rule 15).
func TestGopiDirNearMissOptOutIsHonoured(t *testing.T) {
	sb := sandboxDirFixture(t, "sandbox-near-optout")
	t.Setenv(SandboxNestedProfileOptOutEnv, "1")
	t.Setenv(GopiDirEnv, ProfileDirIn(sb))

	gd, err := GopiDir()
	if err != nil {
		t.Fatalf("the opt-out must be honoured, got %v", err)
	}
	if want := ProfileDirIn(ProfileDirIn(sb)); gd != want {
		t.Errorf("GopiDir = %q, want the nested profile %q the opt-out deliberately allows", gd, want)
	}
}

// TestGopiDirLeavesLegitimateValuesAlone — the exclusions are load-bearing: a
// guard that over-reached would refuse the canonical spelling (the one every
// sandbox's run path exports), an ordinary home, or a home whose leaf merely
// CONTAINS the profile word without being it.
func TestGopiDirLeavesLegitimateValuesAlone(t *testing.T) {
	sb := sandboxDirFixture(t, "sandbox-near-control")
	plain := plainDirFixture(t, "ordinary-home")

	for _, tc := range []struct {
		name string
		home string
		want string
	}{
		{"the CANONICAL sandbox home", sb, ProfileDirIn(sb)},
		{"an ordinary home", plain, ProfileDirIn(plain)},
		// The leaf test is the WHOLE leaf, never a prefix: a home directory
		// whose name merely starts with the profile word is a home.
		{"a home whose leaf contains the profile word", plain + ProfileDirName + "-backup", ProfileDirIn(plain + ProfileDirName + "-backup")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(SandboxNestedProfileOptOutEnv, "")
			t.Setenv(GopiDirEnv, tc.home)
			gd, err := GopiDir()
			if err != nil {
				t.Fatalf("GopiDir(%s=%s) was refused: %v — the guard over-reached", GopiDirEnv, tc.home, err)
			}
			if gd != tc.want {
				t.Errorf("GopiDir = %q, want %q", gd, tc.want)
			}
		})
	}

	// The doubly-nested spelling is refused too — same leaf test, so a tree a
	// pre-fix binary left behind cannot be used to keep nesting.
	t.Setenv(SandboxNestedProfileOptOutEnv, "")
	doubled := ProfileDirIn(ProfileDirIn(plain))
	profile, canonical, ok := LookupSandboxProfileMisuse(doubled)
	if !ok {
		t.Errorf("%s must be refused as a profile inside a profile", doubled)
	}
	if canonical != ProfileDirIn(plain) {
		t.Errorf("the correction for %s = %q, want its parent %s (the profile's own home)",
			doubled, canonical, ProfileDirIn(plain))
	}
	if profile != ProfileDirIn(doubled) {
		t.Errorf("the resolved profile for %s = %q, want %q", doubled, profile, ProfileDirIn(doubled))
	}
	t.Setenv(GopiDirEnv, doubled)
	if _, err := GopiDir(); err == nil {
		t.Errorf("GopiDir accepted the doubly-nested spelling %s", doubled)
	}
}
