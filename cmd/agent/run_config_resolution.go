package main

// The ONE configuration resolution the RUN-SHAPED faces share.
//
// WHY IT IS A FUNCTION AND NOT A BLOCK INSIDE main(). Two faces are the same
// shape — the bare-prompt run (`gopi "<prompt>" --settings X`) and the no-verb
// resume (`gopi --session <id> "<prompt>" --settings X`) — and the epic that
// produced this file (resume-surface convergence, config-knob symmetry) demands
// that they agree on the configuration knobs rather than each having an opinion.
// A copy of main()'s five-step block inside the resume face would be a SECOND
// resolution: the same knobs, a second precedence, drifting on the first edit.
// So the block moved here VERBATIM and both faces call it (Rule 2: one reader,
// one order).
//
// The steps are, in this order (the order is the contract, and each step's WHY
// is the comment that moved with it):
//
//	1. config.FlagOverrides — CLI flags, and the ONLY authority on which flag
//	   names exist. A word the catalog does not carry is an unknown-flag error,
//	   never a silently ignored option (Rule 15);
//	2. --home is exported into GOPI_HOME BEFORE the settings read, because the
//	   default settings path resolves through config.GopiDir(), which reads that
//	   variable — otherwise `--home X` would load the operator's real
//	   ~/.gopi/settings.json (P0-1);
//	3. the settings file is read (flag > env > default) and a malformed one is a
//	   HARD, NAMED exit — never a nil base and a later untraceable API error;
//	4. config.ApplyEnv publishes settings then flags (flags always win);
//	5. the temp workspace is ranked and published (--tmpdir > caller TMPDIR >
//	   GOPI_TMPDIR > settings key > default), then --settings is exported for
//	   deepseek.SettingsPath(), which scans for it.
//
// Returning the error instead of exiting is what lets the resume face answer in
// its OWN voice ("gopi --session: …") while the run path keeps printing the
// usage block — the split is the caller's, the resolution is not.

import (
	"errors"
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
)

// resolveRunConfig resolves the configuration knobs an invocation carries
// (CLI flags > env > settings.json > defaults) and publishes them into this
// process's environment, so every reader downstream — the agent path, a resume,
// and every child either spawns — sees one resolved configuration.
//
// base is the settings layer, overrides the explicit-flag layer; both are
// returned because the temp-workspace ranking needs to tell them apart
// (ResolveTmpDirConfigured ranks the flag channel above the settings key).
func resolveRunConfig(args []string) (base, overrides *config.Config, err error) {
	overrides, err = config.FlagOverrides(args)
	if err != nil {
		return nil, nil, err
	}

	// --home PATH must reach GOPI_HOME BEFORE the settings read: the default
	// settings path resolves through config.GopiDir(), which reads GOPI_HOME
	// from the process env. Injecting here (rather than only via ApplyEnv
	// below) is what makes --home redirect the settings.json read too —
	// otherwise `--home X` silently loads ~/.gopi/settings.json (real
	// api_key / model / compute_nodes) instead of X/.gopi/settings.json
	// (P0-1 fix: data-root isolation must cover settings).
	if overrides.GopiHomePath != "" {
		os.Setenv(config.GopiDirEnv, overrides.GopiHomePath)
	}

	// Settings path: flag --settings > GOPI_SETTINGS env > default.
	settingsPath := overrides.SettingsPath
	if settingsPath == "" {
		settingsPath = deepseek.SettingsPath()
	}
	base, sErr := config.FromSettingsFile(settingsPath)
	if sErr != nil {
		return nil, nil, &runConfigSettingsError{err: sErr}
	}
	// WHAT THIS RESOLUTION IS ABOUT TO DECIDE IS MEASURED, NOT RE-DERIVED
	// (runtime_config_snapshot.go). The environment is photographed BEFORE the
	// first ApplyEnv and diffed AFTER both, which is what lets `/config` name
	// the channel a value came from: a name this run changed was supplied by
	// the settings document, a name it left alone is the operator's own export.
	// One photograph per resolution, at the ONE place the precedence lives
	// (Rule 2) — a second reading of settings.json in the report would be a
	// second answer to the same question.
	envBeforeResolution := runtimeConfigEnvSnapshot()
	config.ApplyEnv(base, false)     // fill env vars not already set (settings < env)
	config.ApplyEnv(overrides, true) // flags always win over env
	recordResolutionSources(envBeforeResolution, overrides)

	// Temp workspace: settings.json `tmpdir` / GOPI_TMPDIR / --tmpdir / a
	// caller's TMPDIR → gopi's OWN environment, BEFORE any spawn. Every child
	// gopi starts (task-run children, the bash tool, git, the merger's
	// resolve/verify runs) begins from os.Environ(), so one publication here is
	// what gives the whole process tree one temp workspace instead of the
	// ambient /tmp. The precedence lives in config.ResolveTmpDirConfigured, one
	// place, and is documented in devdoc/tmpdir-param-not-banner.md.
	injectSettingsTmpDir(base, overrides)

	// --settings PATH must also reach deepseek.SettingsPath() (which scans
	// os.Args for it); if the user passed it as a standalone flag, keep it.
	// We set it on the env so the reader picks it up regardless.
	if settingsPath != "" {
		os.Setenv("GOPI_SETTINGS", settingsPath)
	}
	return base, overrides, nil
}

// runConfigSettingsError marks the settings-file arm, because main() answers it
// with a sentence of its own (P1-D below) instead of the flag-line one.
type runConfigSettingsError struct{ err error }

// Error satisfies error; the message is the settings loader's.
func (e *runConfigSettingsError) Error() string { return e.err.Error() }

// Unwrap keeps the loader's error inspectable (Rule 16).
func (e *runConfigSettingsError) Unwrap() error { return e.err }

// reportRunConfigError is the run path's half of the split described above: the
// same sentence main() has always printed for an unusable flag line (the
// offending name, then the usage block).
func reportRunConfigError(err error) {
	reportConfigErrorDetail(err)
	if isRunConfigSettingsError(err) {
		return
	}
	fmt.Fprintf(os.Stderr, "gopi: %v\n\n%s", err, usage)
}

// isRunConfigSettingsError answers "was this the settings FILE" — the arm whose
// sentence is different, and the one a caller may want to report without the
// usage block (`gopi --session` prints its own page pointer instead).
func isRunConfigSettingsError(err error) bool {
	var settingsErr *runConfigSettingsError
	return errors.As(err, &settingsErr)
}

// reportConfigErrorDetail prints the settings-file arm's extra lines (the fix
// hint), and nothing for any other error.
func reportConfigErrorDetail(err error) {
	var settingsErr *runConfigSettingsError
	if errors.As(err, &settingsErr) {
		// P1-D (devdoc/review2-cli-config-options-variant-20260903.md): a
		// malformed settings.json must NEVER nil-panic the agent. A parse
		// error here returns a NIL *Config, and config.ApplyEnv dereferences
		// it (fields, SetEnvVars) without a nil check — a trailing-comma typo
		// used to die with `panic: invalid memory address or nil pointer
		// dereference` instead of a clean error. Because settings.json is the
		// provider surface (api_key / base_url / model), proceeding on empty
		// defaults would only fail later at the API call with an untraceable
		// auth/network error — so the choice is a HARD EXIT with the settings
		// path and the JSON problem named, never a nil base past this point.
		fmt.Fprintf(os.Stderr, "[gopi] config: %v\n", err)
		fmt.Fprintf(os.Stderr, "[gopi] fix the settings file and re-run, or pass --settings PATH to use a different one\n")
	}
}
