// cli_sandbox_profile_nearmiss_test.go — the NEAR-MISS GOPI_HOME, pinned
// through the REAL BINARY (Rule 31), because the defect was invisible to every
// unit test of the parts: the initializer materialised exactly what it was
// told, the ledger resolver resolved exactly what it is documented to resolve,
// and the composite outcome — a second, nested profile nobody reclaims — only
// exists at the invocation level.
//
// TWO MEASURED INCIDENTS, ONE PREDICATE. The guard began as a test of the
// value's PARENT — a sibling sandbox marker, or a second profile leaf — and the
// arithmetic it refuses sits one level ABOVE that window: a GOPI_HOME that names
// a profile is the near miss whatever its parent is. So the cases come in two
// families and both are pinned here.
//
//   - SANDBOX SPRINT (measured 2026-09-26, end-user audit): `GOPI_HOME=$SB/.gopi`
//     is the spelling an operator reaches for after reading `gopi tool sandbox -h`,
//     which prints `<sandbox>/.gopi` as the sandbox's PROFILE. But GOPI_HOME is a
//     HOME: the documented arithmetic is <GOPI_HOME>/<profile leaf>, so that value
//     resolves to `$SB/.gopi/.gopi` — a SECOND profile, with no .sandbox-owner.json,
//     invisible to SweepSandboxes (it sweeps a sandbox ROOT's children), while the
//     sandbox's own ledger and sessions stay empty.
//   - ORDINARY HOME (measured 2026-10-13 on sager): `GOPI_HOME=/home/user/.gopi`
//     — no sandbox anywhere in the path — resolved to `/home/user/.gopi/.gopi`,
//     whose memory.db held 2 rows while the real ledger held 1376, and
//     `gopi task submit` printed a normal `Task queued: <id>` with exit 0. The
//     first version of this guard deliberately let that value through, on the
//     reasoning that an operator's own profile root is a different situation;
//     it is not, and this family is the one that bit a real host.
//
// Both are silent: for every reader, a nested profile looks exactly like an
// empty one (Rule 15).
//
// WHAT THIS TEST ASSERTS (and why each half is here):
//
//  1. RED PATH, both families — the near-miss spelling produces NO
//     <value>/.gopi tree (measured with test -d semantics, before AND after) and
//     a non-zero exit whose message carries the correction. Against a PRE-fix
//     binary the sandbox arm FAILS: the pre-fix binary created
//     `$SB/.gopi/.gopi/memory.db` on a `memory store` (a WRITER verb — the tree
//     appears where a run first writes), which is what makes this a red proof
//     rather than a re-assertion of new behaviour.
//  2. GREEN PATH — the canonical `GOPI_HOME=$SB` (the value `gopi tool sandbox run`
//     itself exports) is NOT refused, and an ordinary home WRITES the ledger a
//     reader resolves from the same environment. A guard that also refused the
//     documented spelling would break the very path it protects; the first draft
//     of this check did exactly that, which is why the arm is pinned rather than
//     assumed — and the write/read pair is what makes "still works" measurable
//     instead of a non-refusal.
//  3. DEGRADATION IS ANNOUNCED — the operator's own opt-out switch
//     (config.SandboxNestedProfileOptOutEnv) is honoured, but the sentence
//     saying so is printed: a nested profile remains a profile nobody reclaims,
//     and a silent opt-in would be the same silent failure with a flag.
//
// The sandbox is materialised by the REAL `gopi tool sandbox create` and the
// environment is the project's scrubbed contract env (cliContractEnv): nothing
// here can reach the operator's own profile (Rule 34, Rule 32).
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/toolnames"
)

// cliNearMissProbeTopic is the topic the writer verb stores and the reader
// probe searches for — spelled once, so the green arm's two halves cannot
// disagree about what they are looking for (Rule 1b).
const cliNearMissProbeTopic = "near-miss probe"

// cliNearMissProbeArgv is the probe's argv: a WRITER verb, because the nested
// tree appears where the run first WRITES (the ledger's first open creates its
// directory), not on a read-only verb. `memory store` is the smallest such
// surface and needs no credentials.
var cliNearMissProbeArgv = []string{
	CliVerbMemory, toolnames.ToolMemoryActionStore,
	"--topic", cliNearMissProbeTopic,
	"--question", "does this profile nest",
	"--answer", "a stored row is what a reader can be asked to find again",
}

// cliNearMissEnv is the probe environment: the scrubbed contract env
// (cliContractEnv) with GOPI_HOME replaced by the caller's value, and the
// opt-out cleared so the refusal arm is the default. GOPI_HOME is DROPPED from
// the inherited env first — cliContractEnv pins one, and "which value won" for a
// duplicated entry is a property of the READER and of the environment's order
// (glibc and Go take the FIRST copy, the shells take the LAST —
// tools.ChildLocaleDuplicateKeyRule's measurement, 2026-10-16), never of this
// test.
func cliNearMissEnv(home, gopiHome string) []string {
	out := make([]string, 0, len(os.Environ())+2)
	for _, kv := range cliContractEnv(home) {
		if strings.HasPrefix(kv, config.GopiDirEnv+"=") ||
			strings.HasPrefix(kv, config.SandboxNestedProfileOptOutEnv+"=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, config.GopiDirEnv+"="+gopiHome)
}

// cliNearMissRun drives the real binary with the near-miss environment and
// returns its combined output and exit code.
func cliNearMissRun(t *testing.T, home, gopiHome, bin string, extraEnv []string) (string, int) {
	t.Helper()
	return cliSandboxRunEnv(t, bin, append(cliNearMissEnv(home, gopiHome), extraEnv...), cliNearMissProbeArgv...)
}

// nestedProfilePath is the tree the near miss would create: the profile root
// of the value the operator typed (<GOPI_HOME>/<profile leaf>), i.e.
// <sandbox>/.gopi/.gopi for <sandbox>/.gopi.
func nestedProfilePath(sandbox string) string {
	return config.ProfileDirIn(config.ProfileDirIn(sandbox))
}

// TestSandboxProfileNearMissIsRefusedAndCreatesNothing is the acceptance arm of
// the epic: no nested tree, and an observable refusal naming the correction.
func TestSandboxProfileNearMissIsRefusedAndCreatesNothing(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "sbx")
	sb := cliSandboxCreate(t, home, root, "nearmiss")

	nested := nestedProfilePath(sb)
	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s already exists (%v) — the probe cannot measure creation", nested, err)
	}

	out, code := cliNearMissRun(t, home, config.ProfileDirIn(sb), gopiTestBinPath(t), nil)

	// ① the tree is not created — the measure is taken AFTER the run, which is
	// the whole point (the pre-fix binary created it here).
	if _, err := os.Stat(nested); err == nil {
		t.Fatalf("%s was CREATED by a run launched with %s=%s — the near miss still nests a second profile\n--- output ---\n%s",
			nested, config.GopiDirEnv, config.ProfileDirIn(sb), out)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", nested, err)
	}

	// ② the outcome is OBSERVABLE (Rule 15: a refusal, not a silence).
	if code == 0 {
		t.Fatalf("the near-miss value exited 0 — a silent near miss is the defect\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, config.GopiHomeMisuseErrorPrefix) {
		t.Errorf("the refusal does not carry the one prefix every surface agrees on (%q)\n--- output ---\n%s",
			config.GopiHomeMisuseErrorPrefix, out)
	}
	// ③ the correction is the canonical spelling, not a vague warning.
	if want := config.GopiDirEnv + "=" + sb; !strings.Contains(out, want) {
		t.Errorf("the refusal does not name the correction %q\n--- output ---\n%s", want, out)
	}
}

// TestSandboxCanonicalGopiHomeIsNotRefused is the green half: the guard must not
// touch the value the sandbox's own run path exports. Without it, a guard that
// over-reached would pass the acceptance arm above while breaking every sandbox.
func TestSandboxCanonicalGopiHomeIsNotRefused(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "sbx")
	sb := cliSandboxCreate(t, home, root, "canonical")

	out, code := cliNearMissRun(t, home, sb, gopiTestBinPath(t), nil)
	if strings.Contains(out, config.GopiHomeMisuseErrorPrefix) {
		t.Fatalf("%s=%s (the CANONICAL spelling) was refused — the guard broke the path it protects\n--- output ---\n%s",
			config.GopiDirEnv, sb, out)
	}
	if _, err := os.Stat(nestedProfilePath(sb)); err == nil {
		t.Errorf("the canonical spelling also created %s", nestedProfilePath(sb))
	}
	_ = code // the verb's own status is not what this arm measures; the refusal absence is
}

// TestSandboxNearMissOptOutIsAnnounced pins the third behaviour: the operator's
// opt-out is honoured AND announced. A silent degradation would be the same
// defect wearing a flag (Rule 15/25).
func TestSandboxNearMissOptOutIsAnnounced(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "sbx")
	sb := cliSandboxCreate(t, home, root, "optout")

	out, _ := cliNearMissRun(t, home, config.ProfileDirIn(sb), gopiTestBinPath(t),
		[]string{config.SandboxNestedProfileOptOutEnv + "=1"})

	if strings.Contains(out, config.GopiHomeMisuseErrorPrefix) {
		t.Fatalf("the opt-out did not lift the refusal\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, config.SandboxNestedProfileOptOutEnv) {
		t.Errorf("the opt-out was honoured SILENTLY — the run must announce the degradation and name the switch\n--- output ---\n%s", out)
	}
}

// TestSandboxNearMissOrdinaryHomeRootIsRefusedAndCreatesNothing is the SECOND
// family of the near miss, driven through the real binary: a value that names an
// ORDINARY home's profile root — no sandbox, no marker, nothing sandbox-shaped
// anywhere in it. Measured 2026-10-13 on sager with the pre-fix binary:
// `GOPI_HOME=/home/user/.gopi` resolved to /home/user/.gopi/.gopi, whose
// memory.db held 2 rows while the real ledger held 1376, and `gopi task submit`
// printed a normal `Task queued: <id>` with exit 0.
//
// The sandbox arm above cannot catch this shape — its value has no sandbox
// marker to ask about — which is exactly why the predicate moved from the
// value's PARENT to its own LEAF.
func TestSandboxNearMissOrdinaryHomeRootIsRefusedAndCreatesNothing(t *testing.T) {
	home := t.TempDir()
	value := filepath.Join(home, config.ProfileDirName)
	nested := filepath.Join(value, config.ProfileDirName)

	for _, absent := range []string{nested, value} {
		if _, err := os.Stat(absent); !os.IsNotExist(err) {
			t.Fatalf("precondition: %s already exists (%v) — the probe cannot measure creation", absent, err)
		}
	}

	out, code := cliNearMissRun(t, home, value, gopiTestBinPath(t), nil)

	// ① nothing is materialised — the whole tree the refused value would imply.
	for _, absent := range []string{nested, value} {
		if _, err := os.Stat(absent); err == nil {
			t.Fatalf("%s was CREATED by a run launched with %s=%s — an ordinary home's profile root "+
				"is the same near miss\n--- output ---\n%s", absent, config.GopiDirEnv, value, out)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", absent, err)
		}
	}

	// ② it is observable, and it names the value, the arithmetic and the fix.
	if code == 0 {
		t.Fatalf("a profile root with no sandbox in it exited 0 — the 2026-10-13 defect is back\n--- output ---\n%s", out)
	}
	if !strings.Contains(out, config.GopiHomeMisuseErrorPrefix) {
		t.Errorf("the refusal does not carry the one prefix every surface agrees on (%q)\n--- output ---\n%s",
			config.GopiHomeMisuseErrorPrefix, out)
	}
	if !strings.Contains(out, value) || !strings.Contains(out, nested) {
		t.Errorf("the refusal must name the value it refused (%s) and the profile it resolves to (%s)\n--- output ---\n%s",
			value, nested, out)
	}
	if want := config.GopiDirEnv + "=" + home; !strings.Contains(out, want) {
		t.Errorf("the refusal does not name the correction %q\n--- output ---\n%s", want, out)
	}
	// ③ and it must not claim a SANDBOX symptom: the sandbox clause would be an
	// invented symptom for a value with no sandbox in it (Rule 41).
	if strings.Contains(out, config.SandboxOwnerFileName) {
		t.Errorf("the refusal described a sandbox symptom for a value with no sandbox in it\n--- output ---\n%s", out)
	}
}

// TestCanonicalGopiHomeWritesTheLedgerAReaderResolves is the GREEN half made
// MEASURABLE: it is not enough that a legitimate value is not refused — the
// documented spelling must still put the row where the reader looks for it.
//
// The probe is a WRITE followed by a READ in the SAME environment, both through
// the real binary: `memory store` (a writer verb) then `memory search` (the
// production reader). The ledger is then counted through the same driver the
// tree opens its ledgers with, at the ONE path the resolver computes
// (config.ProfileDirIn). A guard that refused the canonical value, or that
// re-pointed it, fails here even though the refusal-absence arm above passed.
func TestCanonicalGopiHomeWritesTheLedgerAReaderResolves(t *testing.T) {
	home := t.TempDir()
	bin := gopiTestBinPath(t)

	out, code := cliNearMissRun(t, home, home, bin, nil)
	if code != 0 {
		t.Fatalf("gopi memory store under the canonical %s=%s: exit %d\n%s",
			config.GopiDirEnv, home, code, out)
	}
	ledger := config.ProfileDirIn(home) + string(os.PathSeparator) + config.MemoryDBFileName
	if _, err := os.Stat(ledger); err != nil {
		t.Fatalf("the writer did not leave a ledger at %s (the path the resolver computes): %v\n%s",
			ledger, err, out)
	}

	// The READER, same environment, through the real binary: a store that
	// landed somewhere else reads back as nothing.
	readOut, readCode := cliSandboxRunEnv(t, bin, cliNearMissEnv(home, home),
		CliVerbMemory, toolnames.ToolMemoryActionSearch, cliNearMissProbeTopic)
	if readCode != 0 {
		t.Fatalf("gopi memory search under the canonical %s=%s: exit %d\n%s",
			config.GopiDirEnv, home, readCode, readOut)
	}
	if !strings.Contains(readOut, cliNearMissProbeTopic) {
		t.Errorf("the reader cannot see the row the writer stored — the write did not land in the profile "+
			"the reader resolves\n--- reader output ---\n%s", readOut)
	}

	// And the nested tree stays absent: the legitimate spelling does not nest.
	if _, err := os.Stat(config.ProfileDirIn(config.ProfileDirIn(home))); err == nil {
		t.Errorf("the canonical spelling also created %s", config.ProfileDirIn(config.ProfileDirIn(home)))
	}
}

// TestNearMissDoublyNestedSpellingIsRefused pins the second predicate through
// the EXPORTED surface the process uses: an already-doubly-nested value is
// refused on the PATH ALONE, so a tree a pre-fix binary left behind cannot be
// used to keep nesting. It needs no sandbox, which is the point.
func TestNearMissDoublyNestedSpellingIsRefused(t *testing.T) {
	home := t.TempDir()
	doubled := filepath.Join(home, config.ProfileDirName, config.ProfileDirName)

	profile, _, ok := config.LookupSandboxProfileMisuse(doubled)
	if !ok {
		t.Fatalf("%s must be recognised as a profile inside a profile", doubled)
	}
	if profile != config.ProfileDirIn(doubled) {
		t.Errorf("resolved profile = %q, want %q", profile, config.ProfileDirIn(doubled))
	}
	// The body message names the value and the profile it resolves to; the
	// refusal PREFIX is prepended by the process that refuses (the CLI prints
	// "gopi: " + prefix + the message this function returns — one wording, one
	// place).
	msg := config.GopiHomeMisuseMessage(doubled)
	if !strings.Contains(msg, doubled) || !strings.Contains(msg, config.ProfileDirIn(doubled)) {
		t.Errorf("the refusal message for %s must name both the value and the profile it resolves to, got %q", doubled, msg)
	}

	// The FIXED CONTROL — the arm this test used to assert the OPPOSITE of, and
	// the flip is the change: a single trailing leaf IS the near miss on its own
	// leaf, with no sibling sandbox marker to ask about. The pre-fix predicate
	// required one, which is why `GOPI_HOME=/home/user/.gopi` walked through it
	// (measured 2026-10-13: a second ledger, 2 rows against the real 1376, and a
	// `Task queued: <id>` with exit 0).
	if profile, canonical, ok := config.LookupSandboxProfileMisuse(filepath.Join(home, config.ProfileDirName)); !ok {
		t.Errorf("a single trailing %s is the near miss on its own leaf — the pre-fix exemption is back",
			config.ProfileDirName)
	} else {
		if want := config.ProfileDirIn(filepath.Join(home, config.ProfileDirName)); profile != want {
			t.Errorf("resolved profile = %q, want %q", profile, want)
		}
		if canonical != home {
			t.Errorf("correction = %q, want the parent %q", canonical, home)
		}
	}
	// And the CANONICAL spelling is never a near miss: the leaf test is the WHOLE
	// leaf, so an ordinary home — including one whose name merely CONTAINS the
	// word — is left alone.
	for _, legit := range []string{home, home + config.ProfileDirName + "-backup"} {
		if _, _, ok := config.LookupSandboxProfileMisuse(legit); ok {
			t.Errorf("%s was read as a near miss — the guard over-reached", legit)
		}
	}
}
