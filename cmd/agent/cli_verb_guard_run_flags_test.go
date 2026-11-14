package main

// cli_verb_guard_run_flags_test.go — the same defect as cli_verb_guard_test.go,
// pinned over the FIVE RUN FLAGS the epic names (--db, --timeout, --raw-log,
// --model, --base-url) rather than one representative flag.
//
// Why a matrix and not one case: the refusal depends on the guard's positional
// boundary agreeing with the PARSER's, and each of these five exercises a
// DIFFERENT branch of that agreement — a scalar option with a separate value
// token (--db PATH, --timeout N, --model M, --base-url U, --raw-log VALUE).
// They are exactly the flags an operator types before a verb, and a regression
// that re-opens the hole for one spelling would leave the others green.
//
// Two halves, both driven through the REAL binary (Rule 31):
//
//  1. the MISPLACED order — `gopi --flag VALUE version` — refuses, and the
//     refusal NAMES the flag that was misplaced (the epic's message
//     requirement: not merely "wrong order" but which flag to move), mints no
//     session directory, writes no session_start anywhere, and registers no
//     ledger row;
//  2. the CORRECT order — `gopi version --flag VALUE` — still runs the verb and
//     prints the version, so the refusal cannot be "achieved" by breaking the
//     ordinary spelling.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/sessionevent"
	gopiversion "github.com/nickhuang99/gopi/internal/version"
)

// The flag VALUES the probes carry. Named, not inline (Rule 1): a bare literal
// would be an argument the reader cannot tell from a limit, and these values
// are fixtures whose shape matters — a path, a small bound, a boolean word, a
// model name and an unreachable endpoint.
const (
	probeDBFileName    = "probe.db"
	probeTimeoutValue  = "3"
	probeRawLogValue   = "off"
	probeModelValue    = "deepseek-v4-pro"
	probeBaseURLValue  = "http://127.0.0.1:9"
	probeFlagTokenPref = "--"
	// probeRefusalMarker is the phrase every refusal carries. Asserting on the
	// phrase (not on a whole sentence) keeps the contract readable while still
	// failing if the guard stops speaking.
	probeRefusalMarker = "is a gopi command, not prompt text"
	// probeGoModFileName names the module file the walk skips: it is not a run
	// artifact and reading every file of a sandbox home is only worth doing
	// when the walk has no judgement of its own.
	probeGoModFileName = "go.mod"
)

// prefixRunFlagCase is one row of the matrix: the catalog flag, and the value
// the probe passes it.
type prefixRunFlagCase struct {
	flag  string
	value string
}

// prefixRunFlagsForProbe builds the matrix from the flags this epic names,
// taking each value from the fixture constants above.
func prefixRunFlagsForProbe(t *testing.T, home string) []prefixRunFlagCase {
	t.Helper()
	return []prefixRunFlagCase{
		{flag: "db", value: filepath.Join(home, probeDBFileName)},
		{flag: "timeout", value: probeTimeoutValue},
		{flag: "raw-log", value: probeRawLogValue},
		{flag: "model", value: probeModelValue},
		{flag: "base-url", value: probeBaseURLValue},
	}
}

// TestRunPathRefusesVerbAfterEveryPrefixRunFlag is the epic's acceptance
// matrix on the built binary: for each of the five prefix run flags, the whole
// line must REFUSE loudly rather than start a run.
func TestRunPathRefusesVerbAfterEveryPrefixRunFlag(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, tc := range prefixRunFlagsForProbe(t, t.TempDir()) {
		t.Run(tc.flag, func(t *testing.T) {
			home := t.TempDir()
			args := []string{probeFlagTokenPref + tc.flag, tc.value, CliVerbVersion}
			out, err := runCLIContractProbeEnv(t, bin, home, args...)
			if err == nil {
				t.Fatalf("gopi %s: exit 0 — the verb after %s was swallowed instead of refused\n--- output ---\n%s",
					strings.Join(args, " "), probeFlagTokenPref+tc.flag, out)
			}
			// The refusal must name the misplaced flag AND the verb: a message
			// that says "wrong order" without saying WHICH flag leaves the
			// operator to re-read the usage, which is the cost this epic exists
			// to remove.
			for _, want := range []string{probeRefusalMarker, CliVerbVersion, probeFlagTokenPref + tc.flag} {
				if !strings.Contains(out, want) {
					t.Errorf("gopi %s: refusal does not mention %q\n--- output ---\n%s",
						strings.Join(args, " "), want, out)
				}
			}
			assertNoRunArtifacts(t, strings.Join(args, " "), home, out)
			assertNoSessionStartUnderHome(t, strings.Join(args, " "), home)
		})
	}
}

// TestRunPathPrintsTheVersionWhenTheVerbComesFirst is the other half of the
// matrix: the spelling the refusal PRESCRIBES must itself work — for every one
// of the five flags. A refusal that only "passes" while the legal order is
// broken would be a second bug wearing the first one's fix (Rule 31).
func TestRunPathPrintsTheVersionWhenTheVerbComesFirst(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, tc := range prefixRunFlagsForProbe(t, t.TempDir()) {
		t.Run(tc.flag, func(t *testing.T) {
			home := t.TempDir()
			args := []string{CliVerbVersion, probeFlagTokenPref + tc.flag, tc.value}
			out, err := runCLIContractProbeEnv(t, bin, home, args...)
			if err != nil {
				t.Fatalf("gopi %s: exit error %v — the CORRECT order must dispatch the verb\n--- output ---\n%s",
					strings.Join(args, " "), err, out)
			}
			if strings.Contains(out, probeRefusalMarker) {
				t.Fatalf("gopi %s: the guard refused a line whose verb is FIRST\n--- output ---\n%s",
					strings.Join(args, " "), out)
			}
			if !strings.Contains(out, gopiversion.Version) {
				t.Fatalf("gopi %s: the verb did not run — output carries no version %q\n--- output ---\n%s",
					strings.Join(args, " "), gopiversion.Version, out)
			}
		})
	}
}

// assertNoSessionStartUnderHome proves the RUN PATH never began: a
// session_start event is written only by a minted session, so its absence
// under the sandbox home is the strongest no-run signature available to a test
// that cannot read the ledger. It scans the home's files rather than asserting
// on the sessions directory alone, because a future layout change must not turn
// this into a vacuous check (Rule 15).
func assertNoSessionStartUnderHome(t *testing.T, argv, home string) {
	t.Helper()
	found, err := homeContainsSessionStart(home)
	if err != nil {
		t.Fatalf("gopi %s: walking sandbox home %s: %v", argv, home, err)
	}
	if found != "" {
		t.Fatalf("gopi %s: a session_start was written at %s — the verb reached the run path instead of being refused",
			argv, found)
	}
}

// homeContainsSessionStart returns the first file under root whose contents
// carry a session_start marker, or "".
func homeContainsSessionStart(root string) (string, error) {
	var hit string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if hit != "" || d.IsDir() {
			return nil
		}
		if d.Name() == probeGoModFileName {
			return nil // module metadata, never an artifact of this run
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			// An unreadable file cannot make a session_start appear, and the
			// walk's own error is still reported by the caller.
			return nil
		}
		if strings.Contains(string(b), sessionevent.EventTypeSessionStart) {
			hit = path
		}
		return nil
	})
	return hit, err
}
