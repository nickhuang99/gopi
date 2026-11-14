package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// ---------------------------------------------------------------------------
// The seam half of the throwaway-profile guard (epic: a child must never resolve
// a throwaway profile).
//
// bashChildEnv is the environment EVERY shell the harness spawns is built from,
// so it is the ONE place the strip has to hold. Both arms are pinned:
//   - a GOPI_HOME naming a code checkout is DROPPED (red proof: revert the strip
//     and the first arm fails, because the shell again resolves
//     <worktree>/.gopi);
//   - a sandbox GOPI_HOME still REACHES the child (the keep arm — without it the
//     documented `GOPI_HOME=$SB gopi …` path would break).
// ---------------------------------------------------------------------------

// childEnvValue returns the value a spawned shell would see for name, and
// whether it is present at all (present-with-empty is a distinct answer: the
// entry itself is what a reader resolves through).
func childEnvValue(env []string, name string) (string, bool) {
	for _, kv := range env {
		if n, v, ok := strings.Cut(kv, "="); ok && n == name {
			return v, true
		}
	}
	return "", false
}

// TestBashChildEnv_DropsACheckoutGOPIHome is the RED side of the seam: it fails
// if stripWrapperIdentity stops dropping a checkout-shaped GOPI_HOME.
func TestBashChildEnv_DropsACheckoutGOPIHome(t *testing.T) {
	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, config.CheckoutMarkerName), 0o755); err != nil {
		t.Fatalf("mark %s as a code checkout: %v", worktree, err)
	}
	t.Setenv(config.GopiDirEnv, worktree)

	env := bashChildEnv()
	if v, ok := childEnvValue(env, config.GopiDirEnv); ok {
		t.Fatalf("bashChildEnv handed the shell %s=%s — a spawned shell must never inherit a home that "+
			"names a code checkout: it would resolve %s/%s as its profile (throwaway, unswept, and it "+
			"dirties the checkout)", config.GopiDirEnv, v, worktree, config.ProfileDirName)
	}
	if !IsCheckoutHomeEnv(config.GopiDirEnv, worktree) {
		t.Fatalf("IsCheckoutHomeEnv(%q) = false — the seam predicate and the strip must agree", worktree)
	}
}

// TestBashChildEnv_KeepsASandboxGOPIHome is the KEEP ARM: a sandbox home (a real
// directory holding a profile, no checkout marker) still reaches the child.
func TestBashChildEnv_KeepsASandboxGOPIHome(t *testing.T) {
	sandbox := t.TempDir()
	if err := os.MkdirAll(config.ProfileDirIn(sandbox), 0o755); err != nil {
		t.Fatalf("materialise the sandbox profile: %v", err)
	}
	t.Setenv(config.GopiDirEnv, sandbox)

	env := bashChildEnv()
	v, ok := childEnvValue(env, config.GopiDirEnv)
	if !ok {
		t.Fatalf("bashChildEnv DROPPED %s=%s — a sandboxed profile is a real home and MUST inherit: "+
			"without it the documented `%s=$SB gopi …` path resolves the operator's profile instead",
			config.GopiDirEnv, sandbox, config.GopiDirEnv)
	}
	if v != sandbox {
		t.Fatalf("bashChildEnv rewrote %s to %q, want %q", config.GopiDirEnv, v, sandbox)
	}
}

// TestCheckoutHomeStripRecord_AnnouncesTheStrip pins Rule 15's half: the strip is
// never silent, and the record carries the VALUE and the reason. It also pins the
// quiet direction — an ordinary environment renders nothing, so a normal run's
// stderr is unchanged.
func TestCheckoutHomeStripRecord_AnnouncesTheStrip(t *testing.T) {
	worktree := t.TempDir()
	env := []string{config.GopiDirEnv + "=" + worktree}

	got := checkoutHomeStripRecord([]string{config.GopiDirEnv}, env)
	if got == "" {
		t.Fatal("a dropped checkout-shaped GOPI_HOME left NO record — a silent strip is the defect (Rule 15)")
	}
	for _, want := range []string{config.GopiDirEnv, worktree, config.CheckoutMarkerName} {
		if !strings.Contains(got, want) {
			t.Fatalf("the strip record does not name %q: %s", want, got)
		}
	}

	// Quiet arm: nothing removed → nothing printed.
	if q := checkoutHomeStripRecord(nil, env); q != "" {
		t.Fatalf("a run whose GOPI_HOME was KEPT printed a strip record: %q", q)
	}
	// And a removal of some OTHER name (the daemon handle) is not this record.
	if q := checkoutHomeStripRecord([]string{TaskWrapperEnv}, env); q != "" {
		t.Fatalf("an unrelated strip rendered the checkout record: %q", q)
	}
}
