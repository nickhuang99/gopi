// cli_checkpoint_root_channel_test.go — the page's ROOT-CHANNEL CLAIM vs the
// code's real resolution.
//
// THE DEFECT. `gopi session checkpoint -h` documented the scan root as a two-channel
// choice (--dir, else ~/.gopi/sessions), while checkpointDir() reads a THIRD
// channel first: the rawLogDirEnv variable (GOPI_RAW_LOG_DIR, catalogued in
// CONFIG.md as `raw_log_dir`). Measured on the pre-fix binary (this worktree's
// merge base), against a sandbox profile:
//
//	$ GOPI_HOME=<sb> gopi session checkpoint list
//	scanned 1 directory(ies) … <sb>/.gopi/sessions   ← the page's default
//	$ GOPI_HOME=<sb> GOPI_RAW_LOG_DIR=/elsewhere gopi session checkpoint list
//	scanned 1 directory(ies) … /elsewhere            ← the page never said so
//
// A reader of the page could not know the variable exists, and a script that
// exports it makes the verb scan a completely different tree with the help
// saying nothing (PROJECT.md Rule 31: the help is the promise the resolver must
// honour).
//
// WHAT THIS FILE PINS, and why each half is needed:
//
//	(a) DERIVED, NOT TRANSCRIBED — every env name the page advertises is read
//	    back out of the usage CONSTANT the binary prints, and each one must be
//	    a row of internal/config's own catalog (the table `gopi --help-env`
//	    renders). A page that names a knob nobody can set fails here, and so
//	    does one that stops naming the knob checkpointDir() reads.
//	(b) THE PAGE POINTS AT THE CATALOG — the page's root paragraph must name
//	    the knob's settings key and CONFIG.md, the two things that let a reader
//	    act on the variable (the doc row itself is checked by
//	    internal/standards' config_doc_gate, which resolves its `file.go:NNN`
//	    anchor).
//	(c) THE CLAIM IS TRUE OF THE CODE — the resolution the page describes is
//	    EXECUTED, twice: in-process through checkpointDir()/rawLogDir(), and
//	    for real through the binary, whose own scan report names the tree each
//	    channel selects and (with --dir) the precedence the page ranks.
//
// Rule 1/13: the env name is never re-typed here — it comes from rawLogDirEnv,
// the constant checkpointDir()/rawLogDir() read. Rule 17: the in-process flip
// that must restore PRESENCE (the absent state is half the assertion) goes
// through testutil.SnapshotEnv. Rule 22/32: every directory comes from
// t.TempDir() / testutil.TestDeviceScratchDir under the harness's pinned
// scratch root.
package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// cprScanRootSection is the page heading under which the root channels are
	// stated — the promise boundary this file reads.
	cprScanRootSection = "WHICH ROOT IS SCANNED"
	// cprRawLogDirKey is the settings key of the knob the page must point at
	// (the env name itself comes from rawLogDirEnv).
	cprRawLogDirKey = "raw_log_dir"
	// cprConfigDocName is the document of record the page sends a reader to.
	cprConfigDocName = "CONFIG.md"
	// cprEnvFlag is the argv spelling of the same knob on the top-level config
	// surface. This group is dispatched BEFORE config resolution, so it refuses
	// the flag by name and the page says so.
	cprEnvFlag = "--raw-log-dir"
	// cprListSubcommand is the verb every probe drives.
	cprListSubcommand = "list"
	// cprRootOpt is the per-invocation override the page ranks first.
	cprRootOpt = "--dir"
	// cprListColumn is a fixed column of `checkpoint list`'s table: its
	// presence proves a raw log was INDEXED, not merely that a directory was
	// visited.
	cprListColumn = "last-model"
	// cprMinAdvertisedNames is the read-floor: a scan of this page yielding
	// fewer env names than this has lost the literal it was pointed at, so a
	// green run would prove nothing.
	cprMinAdvertisedNames = 1
	// cprMinScanRoots is the read-floor for the report parser below: every run
	// here scans at least one directory, so a parse yielding none has lost the
	// report's own shape and must fail loud rather than compare two empty sets.
	cprMinScanRoots = 1
	// cprEntriesMarker is the invariant part of a scan-report root line
	// ("<path> — N entries, M raw log(s)"): the parser keys on the PATH as the
	// line's first field, and this marker keeps it from reading a line of
	// another shape as a root.
	cprEntriesMarker = " entries, "
	// cprProbeTimeout bounds ONE child. A correctly routed CLI verb never
	// enters the agent loop, so a probe that hangs is a routing defect.
	cprProbeTimeout = 30 // seconds
)

// cprEnvNamePattern is the operator-visible env spelling — the same join key
// internal/standards' help_env_catalog_gate applies to every help page.
var cprEnvNamePattern = regexp.MustCompile(`\b(GOPI|ANTHROPIC|DEEPSEEK)_[A-Z0-9_]+\b`)

// cprAdvertisedEnvNames returns every env name the checkpoint page advertises.
func cprAdvertisedEnvNames() []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range cprEnvNamePattern.FindAllString(checkpointUsage, -1) {
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// cprCatalogRow looks name up in the LIVE catalog (never a re-parse of its
// source), returning the row's settings key.
func cprCatalogRow(name string) (string, bool) {
	for _, e := range config.EnvVars {
		if e.Env == name {
			return e.Key, true
		}
	}
	return "", false
}

// cprSectionText returns the page's root-channel paragraph: the lines from the
// section heading up to the next blank line. ONE reading, so the assertions
// below cannot see different prose than each other.
func cprSectionText() string {
	lines := strings.Split(checkpointUsage, "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, cprScanRootSection) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	var out []string
	for i := start; i < len(lines); i++ {
		if i > start && strings.TrimSpace(lines[i]) == "" {
			break
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

// TestCheckpointPageAdvertisesTheEnvItReads is (a)+(b): every env name the page
// prints is a catalog row, the variable checkpointDir() reads is among them,
// and the page's root paragraph names the knob's key and CONFIG.md.
//
// THIS IS THE DEFECT THE EPIC WAS FILED FOR: the page named NO env name at all,
// so the loop below ran over an empty set while the variable that silently
// relocates the scan root was invisible to every reader.
func TestCheckpointPageAdvertisesTheEnvItReads(t *testing.T) {
	names := cprAdvertisedEnvNames()
	if len(names) < cprMinAdvertisedNames {
		t.Fatalf("the checkpoint page advertises %d env name(s) — a reader cannot learn that "+
			"%s relocates the scan root; the derivation is blind too, so a green run would prove "+
			"nothing", len(names), rawLogDirEnv)
	}
	for _, n := range names {
		if _, ok := cprCatalogRow(n); !ok {
			t.Errorf("`gopi session checkpoint -h` advertises %s, which is not a row of internal/config's "+
				"catalog — the page promises a knob `gopi --help-env` does not document", n)
		}
	}
	key, ok := cprCatalogRow(rawLogDirEnv)
	if !ok {
		t.Fatalf("%s is not a catalog row — checkpointDir() reads a variable no operator can "+
			"discover", rawLogDirEnv)
	}
	if !cprHasName(names, rawLogDirEnv) {
		t.Errorf("the checkpoint page does NOT name %s, the variable checkpointDir() reads BEFORE "+
			"falling back to %s — a reader cannot see the channel that decides which tree is "+
			"scanned", rawLogDirEnv, config.SessionsDirName)
	}
	if key != cprRawLogDirKey {
		t.Errorf("the catalog row for %s carries key %q, not %q — the page's pointer to CONFIG.md "+
			"and the doc's own row must name the same knob", rawLogDirEnv, key, cprRawLogDirKey)
	}
	section := cprSectionText()
	if section == "" {
		t.Fatalf("the page carries no %q section — the root channels are stated nowhere a reader "+
			"of the Usage block will look", cprScanRootSection)
	}
	if !strings.Contains(section, rawLogDirEnv) || !strings.Contains(section, cprRootOpt) {
		t.Errorf("the %q section must state BOTH overrides (%s and %s) beside the default, in the "+
			"order the resolver applies them:\n%s", cprScanRootSection, cprRootOpt, rawLogDirEnv, section)
	}
	if !strings.Contains(section, key) {
		t.Errorf("the %q section does not name the catalog key %q — the page must send the reader "+
			"to the knob's own row:\n%s", cprScanRootSection, key, section)
	}
	if !strings.Contains(section, cprConfigDocName) {
		t.Errorf("the %q section does not point at %s — the page states a channel without naming "+
			"the document that owns it:\n%s", cprScanRootSection, cprConfigDocName, section)
	}
	if !strings.Contains(section, cprEnvFlag) {
		t.Errorf("the %q section does not name %s, the flag CONFIG.md lists beside this variable "+
			"and this group refuses by name — a reader arriving from the doc row is left to read "+
			"the refusal as the page disagreeing with itself:\n%s", cprScanRootSection, cprEnvFlag, section)
	}
}

// cprHasName reports whether names carries want.
func cprHasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestCheckpointDirResolutionMatchesThePage is (c) in-process: the resolver's
// real order — GOPI_RAW_LOG_DIR replaces the sessions default, and an ABSENT
// variable leaves that default in place.
//
// The flip goes through testutil.SnapshotEnv rather than t.Setenv: the absent
// state is half the assertion, and SnapshotEnv restores PRESENCE, which
// t.Setenv's empty-string restore cannot.
func TestCheckpointDirResolutionMatchesThePage(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home) // the default branch is only measurable against a pinned profile
	wantDefault := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)

	defer testutil.SnapshotEnv(t, rawLogDirEnv)()
	if err := os.Unsetenv(rawLogDirEnv); err != nil { // env-leak-ok: SnapshotEnv above restores presence, not just value
		t.Fatalf("unset %s: %v", rawLogDirEnv, err)
	}
	if got := checkpointDir(); got != wantDefault {
		t.Fatalf("with %s absent, checkpointDir() = %q, want the sessions default %q the page "+
			"documents", rawLogDirEnv, got, wantDefault)
	}
	wantFlat := filepath.Join(home, config.ProfileDirName, scratch.RawLogDirName())
	if got := rawLogDir(); got != wantFlat {
		t.Fatalf("with %s absent, rawLogDir() = %q, want the legacy flat default %q",
			rawLogDirEnv, got, wantFlat)
	}

	relocated := testutil.TestDeviceScratchDir(t, "cpr-relocated")
	if err := os.Setenv(rawLogDirEnv, relocated); err != nil { // env-leak-ok: SnapshotEnv above restores presence, not just value //stylegate:ignore the variable is PRESENT at process entry, and the test's own unset is half the assertion, so the restore must be SnapshotEnv's — a t.Setenv undo rides t.Cleanup and lands AFTER the deferred restore, leaving the sibling the wrong state (measured)
		t.Fatalf("set %s: %v", rawLogDirEnv, err)
	}
	if got := checkpointDir(); got != relocated {
		t.Fatalf("with %s=%s, checkpointDir() = %q — the page states the variable replaces the "+
			"default outright, sessions root included", rawLogDirEnv, relocated, got)
	}
	if got := rawLogDir(); got != relocated {
		t.Fatalf("with %s=%s, rawLogDir() = %q — both resolvers must honour the one channel the "+
			"page names", rawLogDirEnv, relocated, got)
	}
}

// TestCheckpointListScansTheTreeThePageNames is the Rule 31 half: the REAL
// binary is driven through the page's three channels, and each run's own scan
// report must name exactly the tree that channel selects.
//
// The fixture is a raw log in the DEFAULT tree, so the difference is observable
// as CONTENT (the table column) as well as by path: the default channel indexes
// one log, the relocated channel indexes none — the page's warning made
// concrete.
func TestCheckpointListScansTheTreeThePageNames(t *testing.T) {
	home := t.TempDir()
	def := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	// The fixture raw log sits ONE level below the default root — the layout the
	// writer has used since the session-dir unification, and the level the
	// indexer descends into (checkpoint.IndexTree's sessionScanDepth).
	defRun := filepath.Join(def, statsFixtureSessionID)
	if err := os.MkdirAll(defRun, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", defRun, err)
	}
	copyFixture(t, filepath.Join("raw", statsFixtureRawLogName),
		filepath.Join(defRun, statsFixtureRawLogName))

	// The relocated tree is EMPTY on purpose: the page's warning ("replaces the
	// default below outright") is what this run demonstrates.
	elsewhere := testutil.TestDeviceScratchDir(t, "cpr-elsewhere")
	dirFlag := testutil.TestDeviceScratchDir(t, "cpr-dirflag")

	bin := gopiTestBinPath(t)
	base := cliContractEnv(home)

	// (1) The default channel: <gopi dir>/sessions, and its fixture log indexed.
	stdout, stderr, code := cprRunCheckpointList(t, bin, base, "", cprListSubcommand)
	if code != 0 {
		t.Fatalf("`gopi session checkpoint %s` exit=%d\n--- stdout ---\n%s\n--- stderr ---\n%s",
			cprListSubcommand, code, stdout, stderr)
	}
	if roots := cprScanRoots(t, stderr); roots[0] != def {
		t.Errorf("with %s absent, the scan report's first root is %q, want the default tree %s the "+
			"page documents (roots: %v):\n%s", rawLogDirEnv, roots[0], def, roots, stderr)
	} else if cprRootsContain(roots, elsewhere) {
		t.Errorf("with %s absent, the RELOCATED tree %s appears in the report although the variable "+
			"is unset:\n%s", rawLogDirEnv, elsewhere, stderr)
	}
	if !strings.Contains(stdout, cprListColumn) {
		t.Errorf("with %s absent, the default tree's fixture raw log was NOT indexed — the page "+
			"promises that channel scans the sessions root:\n%s", rawLogDirEnv, stdout)
	}

	// (2) The channel the page now documents: the variable REPLACES it.
	stdout, stderr, code = cprRunCheckpointList(t, bin, base, elsewhere, cprListSubcommand)
	if code != 0 {
		t.Fatalf("`gopi session checkpoint %s` with %s exit=%d\n--- stdout ---\n%s\n--- stderr ---\n%s",
			cprListSubcommand, rawLogDirEnv, code, stdout, stderr)
	}
	if roots := cprScanRoots(t, stderr); len(roots) != 1 || roots[0] != elsewhere {
		t.Errorf("with %s=%s the scan report names %v, want that tree ALONE — the page says the "+
			"variable replaces the default outright:\n%s", rawLogDirEnv, elsewhere, roots, stderr)
	}
	if strings.Contains(stdout, cprListColumn) {
		t.Errorf("with %s set, the fixture raw log in the DEFAULT tree %s was still indexed — the "+
			"page's precedence (the variable replaces the default) is wrong:\n%s",
			rawLogDirEnv, def, stdout)
	}

	// (3) The per-invocation channel outranks the variable — the page RANKS the
	// three, and a page that lists channels without their order is the defect's
	// other half.
	stdout, stderr, code = cprRunCheckpointList(t, bin, base, elsewhere,
		cprListSubcommand, cprRootOpt, dirFlag)
	if code != 0 {
		t.Fatalf("`gopi session checkpoint %s %s` exit=%d\n--- stdout ---\n%s\n--- stderr ---\n%s",
			cprListSubcommand, cprRootOpt, code, stdout, stderr)
	}
	if roots := cprScanRoots(t, stderr); len(roots) != 1 || roots[0] != dirFlag {
		t.Errorf("%s must outrank %s (the page ranks it first): the scan report names %v, want "+
			"exactly %s\n%s", cprRootOpt, rawLogDirEnv, roots, dirFlag, stderr)
	}
}

// TestCheckpointRefusesTheFlagThePageNames pins the refusal the page states:
// --raw-log-dir is the same knob's top-level spelling and this group, which
// runs BEFORE config resolution, refuses it by name (never silently obeys it).
func TestCheckpointRefusesTheFlagThePageNames(t *testing.T) {
	bin := gopiTestBinPath(t)
	base := cliContractEnv(t.TempDir())
	stdout, stderr, code := cprRunCheckpointList(t, bin, base, "", cprListSubcommand, cprEnvFlag, t.TempDir())
	if code == 0 {
		t.Fatalf("`gopi session checkpoint %s %s` was accepted (exit 0) although the page says this group "+
			"refuses it:\n--- stdout ---\n%s\n--- stderr ---\n%s", cprListSubcommand, cprEnvFlag, stdout, stderr)
	}
	if !strings.Contains(stderr, "unknown flag "+cprEnvFlag) {
		t.Errorf("the refusal is not the NAMED one the page describes (want %q):\n%s",
			"unknown flag "+cprEnvFlag, stderr)
	}
}

// cprScanRoots parses the scan report on stderr into the directories the run
// actually scanned: the root lines are two-space indented and carry
// "<path> — N entries, M raw log(s)", so the PATH is the line's first field.
// The parser keys on the report's ONE implementation (checkpoint.ScanReport.
// Describe) rather than on a second reading of the same data.
func cprScanRoots(t *testing.T, stderr string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, "  ") || !strings.Contains(line, cprEntriesMarker) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
			continue
		}
		out = append(out, fields[0])
	}
	if len(out) < cprMinScanRoots {
		t.Fatalf("the scan report yielded %d scanned root(s) — the report's shape changed and every "+
			"assertion above would compare nothing; stderr:\n%s", len(out), stderr)
	}
	return out
}

// cprRootsContain reports whether the parsed root list carries dir.
func cprRootsContain(roots []string, dir string) bool {
	for _, r := range roots {
		if r == dir {
			return true
		}
	}
	return false
}

// cprRunCheckpointList drives the real binary through `gopi session checkpoint …` with
// rawDir exported for the child (empty = the variable ABSENT, which the base
// env already guarantees: cliContractEnv drops every inherited GOPI_* variable
// and re-adds GOPI_HOME only — so the probe's two states are the two the page
// documents, and nothing ambient can widen them).
func cprRunCheckpointList(t *testing.T, bin string, base []string, rawDir string, argv ...string) (string, string, int) {
	t.Helper()
	// The group's verb is prepended here: every call site spells the subcommand
	// and its flags, and the ADDRESS is the session family's — `gopi session
	// checkpoint <word>`, the spelling the hierarchy teaches and the one
	// verb-retirement leaves standing. Both spellings answer in this tree; the
	// probe drives the family address so the same file measures the same
	// surface on either side of the retirement (task 01a10e039a5e7a31ad0ba8455e7dc3ee, 2026-10-06).
	argv = append([]string{CliVerbSession, CliVerbCheckpoint}, argv...)
	env := base
	for _, kv := range base {
		if strings.HasPrefix(kv, rawLogDirEnv+"=") {
			t.Fatalf("the probe base env carries %s although the case needs it ABSENT", rawLogDirEnv)
		}
	}
	if rawDir != "" {
		env = append(append([]string{}, base...), rawLogDirEnv+"="+rawDir)
	}
	cmd := exec.Command(bin, familyAddressArgs(argv)...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	done := make(chan error, 1)
	var err error
	go func() {
		err = cmd.Run()
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(cprProbeTimeout * time.Second):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child this probe spawned and is tearing down
		<-done
		t.Fatalf("gopi %s: TIMEOUT after %ds — argv fell through to the agent loop, or parked on a child",
			strings.Join(argv, " "), cprProbeTimeout)
	}
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("gopi %s: %v\n%s%s", strings.Join(argv, " "), err, stdout.String(), stderr.String())
		}
		code = ee.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}
