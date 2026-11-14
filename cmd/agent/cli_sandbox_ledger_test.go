// cli_sandbox_ledger_test.go — the sandbox LEDGER's shape, driven through the
// REAL BINARY (Rule 31), because the defect this pins was invisible to a unit
// test: `gopi tool sandbox create` produced a profile whose ledger the ledger
// resolver never found, and the node it advertised instead was a symlink to a
// SHARED read-only artifact.
//
// THE ONE TRUTH, in executable form: a sandbox's ledger is a REAL, private,
// writable file at the path the resolver computes — `<sandbox>/.gopi/memory.db`
// — and a real `gopi memory store` inside that sandbox writes THIS file.
//
// The pre-fix shape is asserted to be absent rather than assumed away: no
// top-level `memory.db` (there is no shared-sample link to advertise, and so
// none to swap), and the resolved ledger is not a symlink.
//
// Fixtures are reused, not reinvented: gopiTestBinPath (gopibin_test.go) builds
// the binary ONCE per suite, cliContractEnv (cli_contracts_test.go) scrubs the
// environment so no probe can reach the operator's profile, and the sandbox is
// materialised by the REAL `gopi tool sandbox create` — never by a hand-built
// directory (Rule 34).
package main

import (
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sandbox"
	"github.com/nickhuang99/gopi/internal/tools"
)

// cliSandboxProbeTimeout bounds one probe's child. A correctly routed CLI verb
// never reaches the agent loop, so a park here is a routing defect, not a slow
// test (the same ceiling the memory help-surface probes use).
const cliSandboxProbeTimeout = 40 * time.Second // seconds: applied as a multiplied Duration below

// cliSandboxRun drives the real binary in a scrubbed environment, with the
// sandbox under a caller-owned root, and returns its combined output.
//
// ROOT is pinned through GOPI_SANDBOX_DIR (the env channel) rather than the
// flag, and the choice is load-bearing: the spec for a new test that must fail
// on the PRE-fix code is that it drives a surface the pre-fix binary already
// had. That keeps a failure attributable to the LEDGER instead of to a flag the
// old surface rejected (the flag has its own test below).
func cliSandboxRun(t *testing.T, home, root string, args ...string) (string, int) {
	t.Helper()
	return cliSandboxRunEnv(t, gopiTestBinPath(t),
		append(cliContractEnv(home), config.SandboxEnvName+"="+root), args...)
}

// cliSandboxRunEnv is cliSandboxRun with the BINARY and the ENVIRONMENT chosen
// by the caller — the seam the near-miss regression test needs for two reasons
// that are both load-bearing:
//
//   - the BINARY: the red proof runs the SAME probe against a pre-fix build, so
//     the binary must be a parameter and not an implicit fresh build of the
//     working tree;
//   - the ENVIRONMENT verbatim: the sandbox-handover and near-miss probes set
//     GOPI_HOME themselves (to the value under test), so the caller owns the
//     env slice and no default is appended behind its back.
func cliSandboxRunEnv(t *testing.T, bin string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, familyAddressArgs(args)...)
	cmd.Env = env
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(cliSandboxProbeTimeout):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		<-done
		t.Fatalf("gopi %s: TIMEOUT after %ds", strings.Join(args, " "), cliSandboxProbeTimeout)
	}
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("gopi %s: %v", strings.Join(args, " "), err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// cliSandboxCreate materialises a sandbox and returns its path — read from the
// LAST stdout line, which is the CLI's documented capture contract
// (`SB=$(gopi tool sandbox create … | tail -1)`). It also pins the `--sandbox-dir`
// flag: the resolver documents flag > env > settings > default, and this verb
// must honour its own flag.
func cliSandboxCreate(t *testing.T, home, root, label string) string {
	t.Helper()
	out, code := cliSandboxRun(t, home, root, CliVerbSandbox, SandboxCreateVerb, SandboxLabelFlag, label)
	if code != 0 {
		t.Fatalf("gopi tool sandbox create (root=%s via %s): exit %d\n%s", root, config.SandboxEnvName, code, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	path := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(path, filepath.Clean(root)+string(os.PathSeparator)) {
		t.Fatalf("gopi tool sandbox create printed %q as its path, want a directory under %s\n%s", path, root, out)
	}
	if !strings.Contains(out, sandbox.LedgerPathIn(path)) {
		t.Errorf("the create report does not name the ledger it materialised (%s):\n%s", sandbox.LedgerPathIn(path), out)
	}
	return path
}

// TestSandboxLedgerIsARealWritableFileAtTheResolvedPath is the shape contract:
// the initializer leaves a REAL file where the ledger resolver looks, and no
// shared-artifact symlink anywhere in the sandbox.
func TestSandboxLedgerIsARealWritableFileAtTheResolvedPath(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	sb := cliSandboxCreate(t, home, root, "ledger-shape")

	ledger := sandbox.LedgerPathIn(sb)
	info, err := os.Lstat(ledger)
	if err != nil {
		t.Fatalf("the resolved ledger %s does not exist after create: %v", ledger, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the resolved ledger %s is a SYMLINK — a sandbox ledger is a WRITE TARGET", ledger)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("the resolved ledger %s is not a regular file (%s)", ledger, info.Mode().Type())
	}
	if perm := info.Mode().Perm(); perm != sandboxLedgerWantPerm {
		t.Errorf("the ledger is mode %o, want %o", perm, sandboxLedgerWantPerm)
	}
	if _, err := os.OpenFile(ledger, os.O_WRONLY|os.O_APPEND, 0); err != nil {
		t.Errorf("the ledger is not writable: %v", err)
	}

	// The pre-fix node must be GONE: a top-level memory.db in a sandbox was the
	// symlink to the shared read-only sample, and its absence is what makes the
	// silent swap impossible.
	if top := filepath.Join(sb, config.MemoryDBFileName); !isNotExist(t, top) {
		t.Errorf("the sandbox still carries a top-level %s — the shared-sample link is back", config.MemoryDBFileName)
	}
}

// TestSandboxLedgerAcceptsARealRun is the ACCEPTANCE half: the shape exists so a
// run inside the sandbox can write memory, so this test performs that write with
// the real binary and reads the result back out of the file the resolver names.
func TestSandboxLedgerAcceptsARealRun(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	bin := gopiTestBinPath(t)

	// `sandbox run` materialises AND executes — the allocation+execution entry
	// point — so this is the same one-verb path a script uses.
	out, code := cliSandboxRun(t, home, root, CliVerbSandbox, SandboxRunVerb,
		SandboxLabelFlag, "ledger-run", SandboxSepToken,
		bin, "memory", "store",
		"--topic", "sandbox ledger",
		"--question", "can a real run write its own sandbox ledger",
		"--answer", "this row exists because the sandbox's ledger is a real writable file")
	if code != 0 {
		t.Fatalf("gopi tool sandbox run … memory store: exit %d\n%s", code, out)
	}
	sb := cliSandboxLastPath(t, out, root)
	ledger := sandbox.LedgerPathIn(sb)

	info, err := os.Lstat(ledger)
	if err != nil {
		t.Fatalf("the run's ledger %s does not exist: %v\n%s", ledger, err, out)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the run's ledger %s is a symlink after the write", ledger)
	}
	if info.Size() == 0 {
		t.Fatalf("the run reported success but its ledger %s is empty", ledger)
	}
	if rows := cliSandboxLedgerRows(t, ledger); rows < 1 {
		t.Errorf("the run's own ledger holds %d distilled_qa rows, want >= 1 — the write did not land in the file "+
			"the sandbox resolves", rows)
	}
}

// cliSandboxLastPath reads the materialised sandbox's path from the probe's
// output (the last line that names a directory under root).
func cliSandboxLastPath(t *testing.T, out, root string) string {
	t.Helper()
	prefix := filepath.Clean(root) + string(os.PathSeparator)
	path := ""
	for _, ln := range strings.Split(out, "\n") {
		if ln = strings.TrimSpace(ln); strings.HasPrefix(ln, prefix) {
			path = ln
		}
	}
	if path == "" {
		t.Fatalf("no sandbox path under %s in the probe output:\n%s", root, out)
	}
	return path
}

// cliSandboxLedgerRows counts the rows in a sandbox ledger through the SAME
// driver the tree opens its ledgers with (modernc/sqlite, registered by the
// tools package), so the count is the database's own answer rather than a byte
// count.
func cliSandboxLedgerRows(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(path))
	if err != nil {
		t.Fatalf("open the sandbox ledger %s: %v", path, err)
	}
	defer db.Close() //stylegate:ignore probe handle closed after its single read
	var n int
	if err := db.QueryRow("SELECT count(*) FROM distilled_qa").Scan(&n); err != nil {
		t.Fatalf("count distilled_qa in %s: %v", path, err)
	}
	return n
}

// isNotExist reports whether path is absent, failing the test on any other
// error (a permission problem must not read as "absent").
func isNotExist(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		return false
	}
	if !os.IsNotExist(err) {
		t.Fatalf("lstat %s: %v", path, err)
	}
	return true
}

// TestSandboxDirFlagDecidesTheRoot pins the flag half of the knob's documented
// precedence (flag > env > settings > default). It is the ONE assertion in this
// file that the pre-fix binary fails for a NON-ledger reason: the verb's own
// parser rejected `--sandbox-dir` as an unexpected argument, though the same
// flag was accepted globally and the verb's help advertised the precedence.
// Kept separate so a ledger regression and a flag regression stay attributable.
func TestSandboxDirFlagDecidesTheRoot(t *testing.T) {
	home := t.TempDir()
	envRoot := t.TempDir()
	flagRoot := t.TempDir()
	// THE DOOR IS `tool sandbox` (familyAddressArgs maps the retired bare
	// `sandbox` to it), so the argv is built through the SAME mapper the rest of
	// the probe uses and then the create's own flags are appended. The pre-fix
	// form took familyAddressArgs(...)[0] — `tool` — and pushed the verb words
	// as ARGUMENTS behind it, which the group answers with its unknown-command
	// refusal. The assertion was never about the group's argv; it is about the
	// flag's precedence, so the door is spelled here in full (varargs, no
	// indexing) and nothing else changes.
	cmd := exec.Command(gopiTestBinPath(t), append(familyAddressArgs([]string{CliVerbSandbox, SandboxCreateVerb}),
		SandboxDirFlag, flagRoot, SandboxLabelFlag, "flag-root")...)
	// BOTH channels are set, so the flag's WIN is observable and not merely
	// "the only value in play".
	cmd.Env = append(cliContractEnv(home), config.SandboxEnvName+"="+envRoot)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gopi tool sandbox create %s %s: %v\n%s", SandboxDirFlag, flagRoot, err, out)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	path := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(path, filepath.Clean(flagRoot)+string(os.PathSeparator)) {
		t.Errorf("the sandbox landed at %q — %s did not outrank %s (%s)",
			path, SandboxDirFlag, config.SandboxEnvName, envRoot)
	}
	//stylegate:ignore lists a directory the test populated; the entries are the assertion
	if entries, _ := os.ReadDir(envRoot); len(entries) != 0 {
		t.Errorf("the env root %s gained %d entries — the flag did not win", envRoot, len(entries))
	}
}

// sandboxLedgerWantPerm is the mode a sandbox ledger must carry (Rule 1: a
// permission is a named constant, not a literal at the assertion). It mirrors
// internal/sandbox's own sandboxLedgerFilePerm, which is unexported and stays
// that way — exporting production API so a test can read a number would be the
// worse trade.
const sandboxLedgerWantPerm = 0o600

// TestSandboxPlainshellShapeIsExactlyOneRealLedger is the PLAIN-SHELL pin, and
// it is deliberately stronger than the two shape tests above in three ways.
//
//  1. NOTHING IS SET. Every GOPI_* variable is cleared before the probe, so this
//     is what a terminal `gopi tool sandbox create` produces with no seed, no home
//     override and no root override — the caller the usage text is written for.
//     (cliSandboxRun also passes the env root, which is what makes the probe's
//     sandbox land under a directory this test owns.)
//  2. THE WHOLE TREE IS COUNTED, not one path. "Exactly one database" is a
//     statement about the sandbox, so the test walks it and counts: a second
//     shape (a top-level memory.db, a stray sample copy) fails here even if the
//     resolved path is correct.
//  3. THE UNSEEDED DEFAULT IS PINNED: the ledger starts EMPTY and writable, and
//     the ledger's own first open is what initializes its schema — never the
//     initializer running DDL (that authority lives in internal/tools).
//
// It is the acceptance sentence of the epic, executed rather than asserted in
// prose: one ledger, real, 0600, EMPTY, at <sandbox>/.gopi/memory.db, and no
// symlink at that path anywhere in the tree.
func TestSandboxPlainshellShapeIsExactlyOneRealLedger(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()

	// A plain shell: no seed, no overrides. t.Setenv is set-and-restored by the
	// testing package, so an empty value here means "unset" for the child too.
	for _, k := range []string{
		sandbox.GopiSandboxLedgerSeedEnvName,
		config.SandboxEnvName,
		"GOPI_HOME",
		"GOPI_SETTINGS",
		"GOPI_TMPDIR",
		"TMPDIR",
	} {
		t.Setenv(k, "")
	}

	out, code := cliSandboxRun(t, home, root, CliVerbSandbox, SandboxCreateVerb, SandboxLabelFlag, "plainshell")
	if code != 0 {
		t.Fatalf("gopi tool sandbox create (plain shell): exit %d\n%s", code, out)
	}
	sb := cliSandboxLastPath(t, out, root)
	if strings.TrimSpace(out) != "" && !strings.Contains(out, sb) {
		t.Errorf("the create report does not name the sandbox it made (%s):\n%s", sb, out)
	}

	// (a) The ledger the RESOLVER computes, in the shape the epic claims: real,
	// private, EMPTY, writable.
	ledger := sandbox.LedgerPathIn(sb)
	info, err := os.Lstat(ledger)
	if err != nil {
		t.Fatalf("the resolved ledger %s does not exist after a plain-shell create: %v", ledger, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the resolved ledger %s is a SYMLINK — it is a WRITE TARGET (Rule 34)", ledger)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("the resolved ledger %s is not a regular file (%s)", ledger, info.Mode().Type())
	}
	if perm := info.Mode().Perm(); perm != sandboxLedgerWantPerm {
		t.Errorf("the ledger is mode %o, want %o", perm, sandboxLedgerWantPerm)
	}
	if info.Size() != 0 {
		t.Errorf("an UNSEEDED ledger is %d bytes, want an EMPTY file (the schema is the ledger's own first "+
			"open to initialize, never this initializer's DDL)", info.Size())
	}
	if _, err := os.OpenFile(ledger, os.O_WRONLY|os.O_APPEND, 0); err != nil {
		t.Errorf("the ledger is not writable: %v", err)
	}

	// (b) EXACTLY ONE database in the whole sandbox. The pre-fix shape shipped a
	// second: a top-level `memory.db` symlink to the shared read-only sample.
	var dbs []string
	var links []string
	if err := filepath.WalkDir(sb, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			links = append(links, p)
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".db") {
			dbs = append(dbs, p)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk the sandbox %s: %v", sb, err)
	}
	if len(dbs) != 1 || dbs[0] != ledger {
		t.Errorf("the sandbox holds %d database(s) %v, want exactly the resolved ledger %s — a second "+
			"database is a second SHAPE", len(dbs), dbs, ledger)
	}

	// (c) The ledger is not among the links, and every link present is one of
	// the DOCUMENTED ones. The settings entry used to be a symlink and is now a
	// read-only COPY (settings_doc.go) — a redirect through the old link
	// destroyed the operator's live settings.json on 2026-09-27 — so the entry
	// may legitimately be present as a regular file or absent altogether (a host
	// whose settings document does not exist gets `settings=…
	// (absent-not-linked)`, never a dangling link). What must NEVER appear is a
	// link anywhere other than the plugin tree.
	settingsEntry := filepath.Join(sb, config.ProfileDirName, config.SandboxSettingsFileName)
	for _, l := range links {
		if l == ledger {
			t.Errorf("the ledger %s is a symlink", l)
		}
		if l != filepath.Join(sb, config.ProfileDirName, config.PluginsDirName) {
			t.Errorf("unexpected symlink %s in the sandbox — the only documented link left is the plugin tree %s",
				l, filepath.Join(sb, config.ProfileDirName, config.PluginsDirName))
		}
	}
	// The settings entry, when present at all, is a regular READ-ONLY file — the
	// shape that makes a redirected write land on the copy instead of following a
	// link out of the sandbox.
	if info, lerr := os.Lstat(settingsEntry); lerr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("the settings entry %s is a SYMLINK — the pre-fix write-through shape", settingsEntry)
		}
		if perm := info.Mode().Perm(); perm != 0o444 {
			t.Errorf("the settings entry %s is mode %o, want 0444 (read-only, so a redirect is refused by the kernel)",
				settingsEntry, perm)
		}
	}

	// (d) The profile-level entries are READERS' paths, at the profile root —
	// not siblings of it. A `sessions/` beside `.gopi/` is the shape no reader
	// resolves (the divergence PROJECT.md's rule records).
	profileRoot := config.ProfileDirIn(sb)
	for _, name := range []string{config.SandboxSessionsDirName, config.SandboxRawDirName, config.SandboxLogsDirName} {
		if isNotExist(t, filepath.Join(profileRoot, name)) {
			t.Errorf("the profile root %s is missing the %s directory readers resolve", profileRoot, name)
		}
		if !isNotExist(t, filepath.Join(sb, name)) {
			t.Errorf("the sandbox carries a top-level %s — the profile root is %s, and a sibling of it is "+
				"a shape nothing resolves", name, profileRoot)
		}
	}
}
