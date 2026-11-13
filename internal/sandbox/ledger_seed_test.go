package sandbox

// ledger_seed_test.go — the SEED's own contract: a sandbox is handed its
// starter database from OUTSIDE, never from its own contents.
//
// WHY THIS FILE EXISTS. The refuted symlink-shape branch guarded exactly one
// property that master's ledger shape did NOT carry over: it refused to point a
// sandbox at something INSIDE the sandbox (its `memory.db` was a link, and a
// link whose target lived in the same directory is a sandbox configured by
// whatever it happens to contain — "silently tests nothing"). Its LINK shape is
// refuted (Rule 34: the ledger is a REAL writable file, see ledger.go), but the
// PROPERTY is sound and the seed channel is where master still lacked it: a
// `LedgerSeedPath` (or GOPI_SANDBOX_LEDGER_SEED) naming a file inside the
// sandbox made the ledger a COPY of a file in the very directory it
// configures — a refusal master's settings link already had, and its ledger
// seed did not.
//
// Every test here drives the REAL initializer (Create/Ensure, no fixture
// double) with real paths, and the refusals are asserted to be REFUSALS —
// nothing is skipped silently (Rule 15).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// sandboxSeedRefusalPhrase is the marker every refusal in this file must carry,
// so a test cannot pass on an unrelated error that merely names the same path.
const sandboxSeedRefusalPhrase = "INSIDE the sandbox"

// TestLedgerSeedInsideTheSandboxIsRefused tables the three shapes a
// self-referential seed takes. Each is refused BY NAME — the error carries the
// path, the sandbox and the one thing to do — and the sandbox is left otherwise
// untouched (the refusal happens before any ledger is written, so nothing is
// half-materialised).
func TestLedgerSeedInsideTheSandboxIsRefused(t *testing.T) {
	settings := settingsFile(t, testutil.TestScratchDir(t, "sandbox-seed-guard-settings"))
	root := testutil.TestScratchDir(t, "sandbox-seed-guard")

	cases := []struct {
		id   string
		name string
		seed func(t *testing.T, sandboxDir string) string
	}{
		{
			id:   "ledger-path",
			name: "the path the ledger resolver computes",
			seed: func(t *testing.T, sandboxDir string) string {
				return LedgerPathIn(sandboxDir)
			},
		},
		{
			id:   "sandbox-dir",
			name: "the sandbox directory itself",
			seed: func(t *testing.T, sandboxDir string) string {
				return sandboxDir
			},
		},
		{
			id:   "via-link",
			name: "a path that reaches INSIDE the sandbox through a symlink",
			seed: func(t *testing.T, sandboxDir string) string {
				link := filepath.Join(testutil.TestScratchDir(t, "sandbox-seed-guard-link"), "into-the-sandbox")
				if err := os.Symlink(sandboxDir, link); err != nil {
					t.Fatalf("symlink %s -> %s: %v", link, sandboxDir, err)
				}
				return link
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb, err := Create(root, "", Options{
				Label:               "seedy-" + tc.id,
				ProfileSettingsPath: settings,
			})
			if err != nil {
				t.Fatalf("Create without a seed: %v", err)
			}
			if _, err := os.Lstat(LedgerPathIn(sb.Path)); err != nil {
				t.Fatalf("the unseeded ledger %s is missing before the guarded call: %v", LedgerPathIn(sb.Path), err)
			}

			seed := tc.seed(t, sb.Path)
			// Ensure is the repair half of Create (the same materialisation
			// path), so this is the surface a re-run of `gopi tool sandbox create`
			// reaches with a self-referential seed.
			_, err = Ensure(sb.Path, root, "", Options{
				ProfileSettingsPath: settings,
				LedgerSeedPath:      seed,
			})
			if err == nil {
				t.Fatalf("Ensure accepted the self-referential seed %s — a seed inside the sandbox must be refused", seed)
			}
			if !strings.Contains(err.Error(), sandboxSeedRefusalPhrase) {
				t.Errorf("the refusal does not say the seed is %s: %v", sandboxSeedRefusalPhrase, err)
			}
			if !strings.Contains(err.Error(), sb.Path) {
				t.Errorf("the refusal does not name the sandbox it concerns (%s): %v", sb.Path, err)
			}

			// The refused call must not have replaced the ledger with a copy of
			// itself: the sandbox's own history stays the file it already had.
			info, statErr := os.Lstat(LedgerPathIn(sb.Path))
			if statErr != nil {
				t.Fatalf("the ledger %s did not survive the refusal: %v", LedgerPathIn(sb.Path), statErr)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				t.Errorf("the ledger %s became a symlink after a refused seed", LedgerPathIn(sb.Path))
			}
		})
	}
}

// TestLedgerSeedFromTheEnvChannelIsRefusedWhenInside is the same refusal through
// the channel a PLAIN SHELL uses: `GOPI_SANDBOX_LEDGER_SEED` is the only way a
// script (and `gopi tool sandbox create`) names a seed, so a guard that covered only
// the Options field would leave the CLI open.
func TestLedgerSeedFromTheEnvChannelIsRefusedWhenInside(t *testing.T) {
	settings := settingsFile(t, testutil.TestScratchDir(t, "sandbox-seed-env-settings"))
	root := testutil.TestScratchDir(t, "sandbox-seed-env")

	sb, err := Create(root, "", Options{Label: "envseed", ProfileSettingsPath: settings})
	if err != nil {
		t.Fatalf("Create without a seed: %v", err)
	}
	t.Setenv(GopiSandboxLedgerSeedEnvName, LedgerPathIn(sb.Path))

	_, err = Ensure(sb.Path, root, "", Options{ProfileSettingsPath: settings})
	if err == nil {
		t.Fatalf("%s=%s was accepted — the env channel must reach the same refusal as Options.LedgerSeedPath",
			GopiSandboxLedgerSeedEnvName, LedgerPathIn(sb.Path))
	}
	if !strings.Contains(err.Error(), sandboxSeedRefusalPhrase) {
		t.Errorf("the env-channel refusal does not say the seed is %s: %v", sandboxSeedRefusalPhrase, err)
	}
}

// TestLedgerSeedOutsideTheSandboxIsAccepted is the POSITIVE control: the
// property is "the seed comes from outside", not "seeds are refused". It also
// pins the seeded shape — a real, private, writable file that is a COPY of the
// seed (a distinct inode), never a link to it and never the seed itself.
func TestLedgerSeedOutsideTheSandboxIsAccepted(t *testing.T) {
	settings := settingsFile(t, testutil.TestScratchDir(t, "sandbox-seed-outside-settings"))
	root := testutil.TestScratchDir(t, "sandbox-seed-outside")
	sample := testutil.SampleDBPath(t)

	sb, err := Create(root, "", Options{
		Label:               "seeded",
		ProfileSettingsPath: settings,
		LedgerSeedPath:      sample,
	})
	if err != nil {
		t.Fatalf("Create with the shared sample as seed: %v", err)
	}

	ledger := LedgerPathIn(sb.Path)
	if ledger != sb.LedgerPath {
		t.Errorf("Create reported the ledger %s, the resolver computes %s", sb.LedgerPath, ledger)
	}
	info, err := os.Lstat(ledger)
	if err != nil {
		t.Fatalf("the seeded ledger %s does not exist: %v", ledger, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the seeded ledger %s is a symlink — a ledger is a write target (Rule 34)", ledger)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("the seeded ledger %s is not a regular file (%s)", ledger, info.Mode().Type())
	}
	if perm := info.Mode().Perm(); perm != sandboxLedgerFilePerm {
		t.Errorf("the seeded ledger is mode %o, want %o", perm, sandboxLedgerFilePerm)
	}
	if info.Size() == 0 {
		t.Errorf("the seeded ledger %s is empty — the seed was not copied", ledger)
	}

	sampleInfo, err := os.Stat(sample)
	if err != nil {
		t.Fatalf("stat the seed %s: %v", sample, err)
	}
	if os.SameFile(info, sampleInfo) {
		t.Errorf("the ledger %s and the seed %s are the SAME file — the seed was linked, not copied "+
			"(the sample's 0444 seal and a run's need to write must not be traded against each other)", ledger, sample)
	}
}

// TestExistingLedgerIsNeverRewrittenByASeed is the "the sandbox's own rows are
// its history" line, asserted where a regression would be silent: a second
// Create naming a seed must leave a NON-EMPTY ledger byte-for-byte alone.
func TestExistingLedgerIsNeverRewrittenByASeed(t *testing.T) {
	settings := settingsFile(t, testutil.TestScratchDir(t, "sandbox-ledger-history-settings"))
	root := testutil.TestScratchDir(t, "sandbox-ledger-history")

	sb, err := Create(root, "", Options{Label: "history", ProfileSettingsPath: settings})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ledger := LedgerPathIn(sb.Path)
	history := []byte("the sandbox's own rows, not the seed's")
	if err := os.WriteFile(ledger, history, sandboxLedgerFilePerm); err != nil {
		t.Fatalf("write the sandbox's own ledger content: %v", err)
	}

	if _, err := Create(root, "", Options{
		Label:               "history",
		ProfileSettingsPath: settings,
		LedgerSeedPath:      testutil.SampleDBPath(t),
	}); err != nil {
		t.Fatalf("re-Create with a seed: %v", err)
	}

	got, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatalf("read the ledger %s back: %v", ledger, err)
	}
	if string(got) != string(history) {
		t.Errorf("the seed REWROTE the sandbox's existing ledger (%q), want it untouched (%q)", got, history)
	}
}
