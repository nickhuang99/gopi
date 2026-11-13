package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tools"
)

// CliVerbSetup (defined in cli_verbs.go, the top-level verb SSOT) is the
// `gopi config setup` verb implemented here — bring a host to a run-ready state in
// ONE run: the profile data root, a settings.json, the sessions/ tree and
// the task ledger (memory.db, schema initialized), each reported with its
// path and whether this run created it. Idempotent — a second run creates
// nothing and still exits 0.
//
// WHY A VERB AND NOT A SCRIPT (the epic that produced this file): gopi's
// zero-dependency rule (PROJECT.md Rule 10.7) allows no runtime shell script, so
// a setup script would itself be the deployment defect it was meant to cure.
// Every path here is created by gopi, at run time, with no external binary.

// Provisioning modes. Private by default: the profile directory holds
// settings.json (which may carry an api_key) and memory.db (distilled lessons).
// MkdirAll/WriteFile never rewrite the mode of an existing path, so a live
// install's own modes are left alone.
const (
	setupProfileDirMode = 0o700
	setupRuntimeDirMode = 0o700
	setupSettingsMode   = 0o600
)

// setupEmptySettings is the settings file a fresh profile gets. `{}` is the
// same shape every parser here accepts as "no overrides configured"; writing an
// unusable stub would be worse than writing none.
const setupEmptySettings = "{}\n"

// setupState names how each path was found, so the printed report distinguishes
// work done from work already done (a re-run must be visibly a no-op).
const (
	setupStateCreated     = "created"
	setupStatePresent     = "present"
	setupStateInitialized = "initialized"
)

const setupUsage = `gopi config setup — bring this host to a run-ready state in ONE run.

Usage:
  gopi config setup

Provisioning is idempotent and prints every path with its state, so a deployment
can be verified without reading source:

  created       this run made it
  initialized   it existed but its schema was (re)built — the ledger
  present       already there, nothing done

What it provisions (all under the gopi data root, $GOPI_HOME/.gopi when set):
  - the profile data root itself
  - settings.json (an empty {} when absent — an existing file is never touched)
  - sessions/ — the per-task session tree
  - memory.db — the task ledger + knowledge base, schema initialized

No shell script and no external binary is involved: this is gopi's own
zero-dependency provisioning path, the ONE supported way to make a fresh host
run-ready.

Exit status:
  0   the profile is run-ready (nothing to do is still success)
  1   a path could not be created — the reason is named on stderr
`

// isSetupSubcommand reports whether argv begins with the setup verb. Mirrors
// isConfigSubcommand's contract: once it returns true the verb has consumed the
// argv and the caller must not treat the remainder as a prompt.
func isSetupSubcommand(args []string) bool {
	if len(args) > 0 && args[0] == CliVerbSetup {
		runSetupSubcommand(args[1:])
		return true
	}
	return false
}

// runSetupSubcommand dispatches the verb. It returns nothing: main's dispatch
// chain expects the verb to handle its own exit.
func runSetupSubcommand(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", HelpWord:
			fmt.Print(setupUsage)
			return
		default:
			fmt.Fprintf(os.Stderr, "gopi config setup: unexpected argument %q\n\n%s", args[0], setupUsage)
			os.Exit(1)
		}
	}
	report, err := runSetup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi config setup: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(report)
}

// setupItem is one provisioned path: what it is, where it lives and how this
// run found it.
type setupItem struct {
	label string
	path  string
	state string
}

// runSetup provisions the profile and returns the human report. The returned
// error is the ONLY failure mode (a path that cannot be created); a
// fully-present profile is success, not a no-op to complain about.
func runSetup() (string, error) {
	dir, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("resolve the gopi data root: %w", err)
	}
	if dir == "" {
		return "", fmt.Errorf("resolve the gopi data root: no home directory and no %s", config.GopiDirEnv)
	}

	var items []setupItem
	dirCreated, err := ensureSetupDir(dir, setupProfileDirMode)
	if err != nil {
		return "", err
	}
	items = append(items, setupItem{"profile data root", dir, setupState(dirCreated)})

	// sessions/ is the runtime tree a task writes into. Its name comes from its
	// owner (config.SessionsDirName), so this file never re-spells it. The raw
	// log tree is deliberately NOT provisioned here: its location is
	// configurable (config.RawLogDir) and new sessions co-locate their raw log
	// with the session JSONL, so a hardcoded <profile>/raw would create a
	// directory the live configuration may never read — the ledger below plus
	// sessions/ are what a first run actually requires.
	{
		path := filepath.Join(dir, config.SessionsDirName)
		created, err := ensureSetupDir(path, setupRuntimeDirMode)
		if err != nil {
			return "", err
		}
		// The report label is the directory's own name (config.SessionsDirName),
		// never a second spelling of it (Rule 1b/13): the printed word and the
		// path stay one value.
		items = append(items, setupItem{config.SessionsDirName, path, setupState(created)})
	}

	// config.DefaultSettingsPath cannot fail once GopiDir() resolved above (it
	// resolves the same root), so there is no fallback spelling of the file
	// name to drift here.
	settings := config.DefaultSettingsPath()
	settingsCreated, err := ensureSetupFile(settings, []byte(setupEmptySettings), setupSettingsMode)
	if err != nil {
		return "", err
	}
	items = append(items, setupItem{"settings", settings, setupState(settingsCreated)})

	// The ledger is the one path with an override chain (GOPI_MEMORY_DB >
	// GOPI_HIPPO_DB > <profile>/memory.db), so it is NEVER guessed here: the
	// same config resolver the rest of gopi reads decides it (Rule 2 — one
	// reader of the override). An overridden path is reported, not created:
	// gopi heals the PROFILE ledger, and it does not materialize a file where
	// the operator pointed elsewhere.
	ledger := config.LedgerPath()
	if ledger == "" {
		return "", fmt.Errorf("resolve the task ledger path: no %s and no home directory", config.MemoryDBEnv)
	}
	ledgerState := setupStatePresent
	if ledger == filepath.Join(dir, config.MemoryDBFileName) && !fileExistsAt(ledger) {
		if err := tools.EnsureLedger(ledger); err != nil {
			return "", fmt.Errorf("initialize the task ledger %s: %w", ledger, err)
		}
		ledgerState = setupStateInitialized
	}
	items = append(items, setupItem{"task ledger", ledger, ledgerState})

	var created int
	var b []byte
	line := func(s string) { b = append(b, s...) }
	line(fmt.Sprintf("gopi profile: %s\n", dir))
	for _, it := range items {
		if it.state != setupStatePresent {
			created++
		}
		line(fmt.Sprintf("  %-18s %-12s %s\n", it.label, it.state, it.path))
	}
	if created == 0 {
		line("nothing to do — the profile is run-ready\n")
	} else {
		line(fmt.Sprintf("%d path(s) provisioned — the profile is run-ready\n", created))
	}
	return string(b), nil
}

// setupState maps "did this run create it" to the printed word.
func setupState(created bool) string {
	if created {
		return setupStateCreated
	}
	return setupStatePresent
}

// ensureSetupDir creates dir when absent and reports whether it did. An
// existing path that is not a directory is an error, never silently reused.
func ensureSetupDir(dir string, perm os.FileMode) (bool, error) {
	fi, err := os.Stat(dir)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", dir, err)
	}
	if err == nil {
		if !fi.IsDir() {
			return false, fmt.Errorf("%s exists and is not a directory", dir)
		}
		return false, nil
	}
	if err := os.MkdirAll(dir, perm); err != nil {
		return false, fmt.Errorf("create %s: %w", dir, err)
	}
	return true, nil
}

// ensureSetupFile writes content to path only when path is absent, and reports
// whether it did. An existing file is NEVER touched — a settings.json the
// operator wrote is the operator's.
func ensureSetupFile(path string, content []byte, perm os.FileMode) (bool, error) {
	_, statErr := os.Stat(path)
	if statErr == nil {
		return false, nil
	}
	if !os.IsNotExist(statErr) { // no-wrap: OsNotExist is the question, not an error to chain
		return false, fmt.Errorf("stat %s: %w", path, statErr)
	}
	// THE GUARDED WRITE. This is gopi's only settings-WRITING path, and it goes
	// through the loss guard (internal/config/settings_write_guard.go) so gopi
	// cannot become the writer that silently drops a key it does not model — the
	// measured defect this exists to close. Nothing is on disk on this branch,
	// so the guard has nothing to lose; if another process created the file
	// between the stat above and this write, the guard refuses rather than
	// clobbering it, and the refusal is reported (Rule 15).
	if err := config.WriteSettingsPreservingKeys(path, content, perm); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// fileExistsAt reports whether path names an existing file (or symlink). It is
// local to this file on purpose: the boolean is the question here, and a
// package-level export would invite a second spelling of it.
func fileExistsAt(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
