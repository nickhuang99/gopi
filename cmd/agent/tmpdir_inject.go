// Temp workspace (tmpdir) → gopi's own environment: the ONE publisher that makes
// every child inherit one temp workspace.
//
// WHY INJECT INTO GOPI'S OWN ENV INSTEAD OF PASSING IT TO EACH SPAWN: gopi
// spawns children from many places — `gopi task run` children, the bash tool
// (internal/tools/bash.go's cmd.Env = append(bashChildEnv(), …)), git, the
// merger's resolve/verify runs, worker output capture. Each one starts from the
// PARENT's environment, so a single os.Setenv here reaches all of them by
// construction, with no per-spawn site to update and no site to forget. That is
// the whole reason this is one call in main() rather than N call-site edits
// (the 2026-09-14 audit found the per-site approach had already been missed in
// the places that mattered: 23.5G of residue in /tmp).
//
// TWO ENTRY POINTS, ONE FUNCTION. main() publishes twice, because gopi has two
// settings-resolution moments and children are spawned on both paths:
//
//	tmpDirBootstrapFromArgs — BEFORE the subcommand chain. The group verbs
//	  (`gopi task worker`, `gopi task run`, `gopi merger`) return from main()
//	  at that chain and never reach the agent path's config block, and those
//	  verbs are the ones that spawn the most children.
//	injectSettingsTmpDir — after the agent path's authoritative settings load,
//	  where `--settings` may have been parsed from anywhere in argv and the
//	  settings file is therefore known exactly.
//
// Both delegate to publishTmpDir, so the value and its precedence cannot differ
// between the two moments. The second call is a NO-OP when it resolves the same
// value the first one published (which is the normal case).
//
// SILENT BY DEFAULT (the operator's decision, 2026-09-16). The publisher used to
// print a `[gopi] tmpdir: …` line on stderr on EVERY invocation — including
// `--help` and `--version`, which spawn no children at all. The temp workspace
// is an ordinary parameter with a default; a run does not announce its
// parameters. The value is reachable through ONE deliberate surface instead:
// `GOPI_DEBUG=1 gopi …` (--debug / the settings `debug` key) prints one line
// naming the effective root and the channel that supplied it, via
// scratch.ReportPublishedTmpDirIfVerbose — no second mechanism was invented for
// this. See devdoc/tmpdir-param-not-banner.md.
//
// LOUD ON FAILURE, and that half survives the banner's removal (Rule 15): a temp
// workspace that cannot be provided or written is reported with its PATH, the
// REASON and the CHANNEL that supplied it (scratch.TmpDirUnusableError), and a
// CONFIGURED value that cannot be used is a hard exit — the operator named that
// directory, so quietly running with a different one would be the silent
// fallback this epic exists to end. The built-in default failing to be created
// is reported just as loudly but does not abort, so `gopi --help` still works on
// a host whose workspace is not writable.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// tmpDirUnusableExitCode is the exit status of a REFUSED start: the temp
// workspace the operator configured cannot be provided or written, so the run
// stops before spawning anything (the same class as an unreadable settings
// file). It is deliberately the ordinary failure code — a caller does not need
// to distinguish it from other configuration refusals, only to read the line.
const tmpDirUnusableExitCode = 1

// tmpDirBootstrapFromArgs is the PRE-subcommand-chain publication: it reads the
// settings file the argv hoist selected AND the flags in argv (so `--tmpdir` and
// `--settings` are honoured wherever they appear), resolves the knob through
// config.ResolveTmpDirConfigured, and publishes the result. A settings read
// failure is reported and ignored here (never fatal): the subcommand that
// follows reports its own configuration errors in its own voice, and a broken
// settings file must not make `gopi help` unusable.
func tmpDirBootstrapFromArgs(args []string) {
	overrides, err := config.FlagOverrides(args)
	if err != nil {
		// An argv the flag parser cannot read is the agent path's business to
		// report (it prints the usage and exits); here there is simply no flag
		// channel to rank, so the remaining channels decide.
		overrides = nil
	}
	cfg, err := config.FromSettingsFile(config.ResolveSettingsPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s reading %s: %v (children keep the ambient temp root)\n",
			scratch.ProbeReportPrefix, config.ResolveSettingsPath(), err)
		cfg = nil
	}
	publishTmpDir(cfg, overrides)

	// The scratch sweep's STALE WINDOW rides the same two configurations and
	// the same pre-chain moment, for the same reason: the group verbs (worker,
	// merger, task run — the ones carrying the resident sweep) return from main
	// at the subcommand chain and never reach the agent path's config block, so
	// a window configured here is the only one they will ever see. One
	// publisher, one precedence (flag > env > settings > default).
	config.PublishScratchGCStaleMin(cfg, overrides)

	// THE RETIRED-KNOB REPORTS ARE **NOT** HERE ANY MORE (2026-11-06), and the
	// reason is the same one the paragraph below records for the worker-face
	// notices: a prompt belongs to the command whose JOB it is. This moment is the
	// one point EVERY verb passes through, so the five `reportRetired*` calls that
	// stood here put two long lines in front of `gopi task list`, `gopi config -h`
	// and every other command — measured on this host before the move, `gopi task
	// list --status in_progress` printed ~700 bytes of retirement prose before its
	// first row. A stale key is a fact about the CONFIGURATION, so it is reported by
	// `gopi config show`, from ONE registry (internal/config/retired_settings_keys.go)
	// that also carries what each knob DID and what replaces it. Adding the next
	// retirement is a ROW there — no function in this package, and no new call here,
	// so this class of noise cannot come back by adding a report.

	// THE WORKER-FACE STARTUP NOTICES ARE DELIBERATELY **NOT** HERE
	// (2026-10-07, task 01a11548). The compute_nodes report stood at this
	// pre-chain moment on the "every verb passes through" argument above — and
	// that was the defect: the knob is about the WORKER face, so printing it here
	// put a ~400-char stderr line in front of verbs that cannot read a dispatch
	// layer at all (`gopi notify`, `gopi task show`, `gopi task submit`, a bare
	// one-shot prompt run — measured). A notice belongs with its subject, on the
	// face it is about.
	//
	// Both notices that used to live here are now GONE, and neither left a
	// report behind:
	//
	//	compute_nodes  deleted with the knob (2026-10-22, task 01a160a2): the
	//	               whitelist had no consumer, so it was removed rather than
	//	               announced on every worker start.
	//	dev_repo       the empty-dev_repo DEPRECATION was withdrawn (operator
	//	               ruling 2026-10-23, task 01a16541-57c0): the state it
	//	               called deprecated is the NORMAL one, so `dev_repo` is an
	//	               OPTIONAL third source of the run-repository chain
	//	               (internal/reposource) rather than a switch every host
	//	               must set. What speaks is the refusal after all three
	//	               sources decline, and `gopi config show` reports the
	//	               resolved answer read-only.
	//
	// The rule that put them on the worker face and not here still holds: a line
	// every command prints is a line every reader learns to skip.
}

// injectSettingsTmpDir is the agent path's publication, after the authoritative
// settings load and ApplyEnv. It takes the same two configurations the bootstrap
// resolved, so the value and its precedence are identical on both paths.
func injectSettingsTmpDir(settingsCfg, flagCfg *config.Config) {
	publishTmpDir(settingsCfg, flagCfg)
}

// publishTmpDir resolves the temp workspace from every configuration channel
// (config.ResolveTmpDirConfigured owns the precedence: --tmpdir > a caller's
// TMPDIR > GOPI_TMPDIR > the settings key > the built-in default), proves it
// usable, and writes it into gopi's OWN environment so every child inherits it.
//
// Returns true when this call installed the value (a test can assert which
// branch ran without re-deriving it from the environment).
func publishTmpDir(settingsCfg, flagCfg *config.Config) bool {
	configured, source := config.ResolveTmpDirConfigured(settingsCfg, flagCfg)
	configuredByOperator := configured != ""

	dir, resolvedSource, err := scratch.ResolvePublishedTmpDir(configured, source)
	if err != nil {
		failTmpDir(err, configuredByOperator)
		return false
	}
	current := os.Getenv(scratch.TmpDirEnvName)
	if current != dir {
		if err := os.Setenv(scratch.TmpDirEnvName, dir); err != nil {
			// Rule 15b: a failed publication leaves every child on the ambient
			// temp root, which is the exact defect this epic fixes — never
			// swallowed.
			failTmpDir(&scratch.TmpDirUnusableError{
				Path:   dir,
				Source: source,
				Reason: fmt.Sprintf("cannot publish %s into the environment: %v", scratch.TmpDirEnvName, err),
			}, true)
			return false
		}
	}
	// The LABEL travels with the value, through the same environment every child
	// inherits, and it is written whenever it differs — INCLUDING the pass whose
	// value was already in place (the bootstrap's second moment, or the
	// caller's own export). A reader (the publisher's next pass, `gopi config
	// show`, a debug line) reads the pair back instead of re-ranking channels
	// the publication has already changed: the name that lost is the name whose
	// value was overwritten, so a re-derivation reports the wrong channel by
	// construction. A failure here is the same loud refusal as above (Rule 15b):
	// a value with no label would report a provenance nobody measured.
	if os.Getenv(scratch.TmpDirSourceEnvName) != resolvedSource {
		if err := os.Setenv(scratch.TmpDirSourceEnvName, resolvedSource); err != nil {
			failTmpDir(&scratch.TmpDirUnusableError{
				Path:   dir,
				Source: source,
				Reason: fmt.Sprintf("cannot publish %s into the environment: %v", scratch.TmpDirSourceEnvName, err),
			}, true)
			return false
		}
	}
	if current == dir {
		// Already exactly this value (the bootstrap published it, or the caller
		// exported it themselves): nothing was installed, but still worth one
		// line under --debug for a reader asking where temp work lands.
		scratch.ReportPublishedTmpDirIfVerbose(dir, resolvedSource)
		reportTmpDirHeadroom(dir, resolvedSource)
		return false
	}
	scratch.ReportPublishedTmpDirIfVerbose(dir, resolvedSource)
	reportTmpDirHeadroom(dir, resolvedSource)
	return true
}

// reportTmpDirHeadroom asks the ONE headroom criterion whether the temp root it
// just published has room for the work every child will do in it, and prints the
// warning when it does not.
//
// WHY IT IS HERE AND NOT A REFUSAL: the publication SUCCEEDED — the directory
// exists and takes a write — so refusing would take down `gopi --help` on a host
// whose workspace is merely nearly full. What must not happen is the SILENCE
// this call ends: a `go build` started from a root with 2.3 GiB left dies with
// `no space left on device` inside an unrelated package's link step, and the
// reader has nothing pointing at the partition. The warning names the path, the
// device, the free bytes and the reclaim candidates; the run continues
// (Rule 15 loud, not fatal — reclaim is the operator's call, Rule 42).
//
// It is a no-op on a root with room, and the criterion prints once per process
// (scratch's tmpDirHeadroomOnce). A MEASUREMENT failure is reported on the same
// channel: an unmeasurable root is never assumed safe.
func reportTmpDirHeadroom(dir, source string) {
	if err := scratch.ReportTmpDirHeadroomIfLow(dir, source); err != nil && !errors.Is(err, scratch.ErrTmpDirLowHeadroom) {
		fmt.Fprintf(os.Stderr, "%sheadroom: %v\n", scratch.ProbeReportPrefix, err)
	}
}

// failTmpDir reports an unusable temp workspace LOUDLY — path, reason and the
// channel that supplied it, all from the error's own text (Rule 1b: no field is
// re-spelled here) — and exits when the operator CONFIGURED that value.
//
// configured distinguishes the two cases the publisher must tell apart:
//
//	true  — a flag/env/settings value named this directory. The operator asked
//	        for it, so running with a DIFFERENT one would be the silent
//	        substitution this epic exists to end: report and exit.
//	false — the built-in default could not be created (a host whose workspace is
//	        not writable). Reported just as loudly, but the run continues on the
//	        ambient root, because nothing was configured and refusing to start
//	        would take `gopi --help` down with it.
func failTmpDir(err error, configured bool) {
	if configured {
		fmt.Fprintf(os.Stderr, "[gopi] tmpdir: %v\n", err)
		os.Exit(tmpDirUnusableExitCode)
	}
	fmt.Fprintf(os.Stderr, "[gopi] tmpdir: %v (children keep the ambient temp root)\n", err)
	_ = scratch.ReportTempRootIfOnRootFS() //stylegate:ignore diagnostics-only: the refusal above already names the cause; this adds the measured device ids of the AMBIENT root the children are about to use, and cannot change it
}
