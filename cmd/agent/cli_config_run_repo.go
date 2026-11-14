package main

// cli_config_run_repo.go — `gopi config show`'s answer to the question the repo
// resolution raises: WHICH repository would a run started here work in?
//
// WHY THIS VERB. The three-source chain (internal/reposource) has one caller
// that matters to an operator — the run itself — and a refusal is worth nothing
// if the only way to see it is to start a task. `gopi config show` is already
// the "what configuration is actually in force" surface (its own header says
// so), and "where would this host's runs work" is precisely that question. It
// is READ-ONLY: it resolves, prints, and touches nothing.
//
// IT IS ALSO THE SURFACE THE ACCEPTANCE RUNS IN (Rule 31): the four shapes the
// retirement turns on — cwd repo with no dev_repo, cwd outside a repo with no
// dev_repo, cwd outside a repo WITH dev_repo, a row's own repo beating the cwd
// — are all observable here by running the real binary from a real directory
// with a real settings document. A unit test can say the resolver is correct;
// only this verb can show that the shipped binary, on this host, prints nothing
// where it used to print a deprecation.
//
// THE VERB EXITS ZERO EITHER WAY, and that is deliberate: `config show` is a
// REPORT of what configuration is in force, and "this host resolves no run
// repository, here is why" is a successful report of a real state. A non-zero
// exit would make every scripted `config show` fail on a host that simply needs
// a `cd`, and the refusal text is what a caller acts on. The row says
// `(none — see the refusal below)` so a reader of stdout alone is not misled
// into thinking a resolution happened.
//
// A RESOLVED SOURCE PRINTS NO WARNING. That is the operator's ruling of
// 2026-10-23 and the whole point of the change: the cwd arm used to be flagged
// as the deprecated path, and it is in fact the normal one. The ONLY line that
// carries a verdict here is the refusal, and it lists all three sources.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/reposource"
)

const (
	// configRunRepoLabel is the report's row label. It is deliberately NOT
	// "repo": the settings file has its own repos TABLE and a reader must not
	// confuse a declared repo entry with the answer to "where would a run
	// work".
	configRunRepoLabel = "run repo"
	// configRunRepoResolvedFmt names the winning directory and the source that
	// decided it (Rule 15's positive half: a decision is never anonymous).
	configRunRepoResolvedFmt = "%s (from %s)"
	// configRunRepoAbsent is printed when nothing resolved — the refusal text
	// follows on stderr, so this row says where to look rather than repeating
	// it.
	configRunRepoAbsent = "(none — see the refusal below)"
	// runRepoProgName labels the refusal on stderr. It is THIS verb's own name,
	// never the worker's: a line prefixed `gopi worker:` from a `gopi config
	// show` run sends the reader to the wrong command (measured while taking
	// this task's evidence — the refusal was attributed to the worker face).
	runRepoProgName = "gopi " + CliVerbConfig
)

// showRunRepo reports the run repository this host resolves to, from THIS
// process's working directory. The row's own deliverable.repo is not
// consultable here — `config show` is about the host, not about a row — so the
// chain is reported with source 1 explicitly empty; a reader who wants the row's
// answer asks `gopi task show <id>`.
func showRunRepo() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("  %-22s %s\n", configRunRepoLabel, "(cwd unreadable: "+err.Error()+")")
		return
	}
	devRepo, devSource, devErr := config.DevRepoConfigured()
	// AN UNREADABLE SETTINGS DOCUMENT IS A FACT ABOUT THE THIRD SOURCE, not a
	// reason to refuse: the first two sources do not need it. It is reported in
	// the row so a reader who is about to fault dev_repo knows the document
	// itself could not be read.
	declared := devRepo
	if devErr != nil {
		declared = ""
	}
	res, resolveErr := reposource.Resolve(reposource.Input{
		CWD:           cwd,
		DevRepo:       declared,
		RenderDevRepo: devworkspace.SourceRepoDisplayPath,
	})
	if resolveErr != nil {
		fmt.Printf("  %-22s %s\n", configRunRepoLabel, configRunRepoAbsent)
		fmt.Fprintf(os.Stderr, "%s: %v\n", runRepoProgName, resolveErr)
		return
	}
	source := res.Source
	if source == reposource.SourceDevRepo {
		// WHICH CHANNEL delivered the value — env and settings are different
		// places for an operator to go and edit, so the row names the one that
		// actually answered and falls back to the key's own name when the
		// resolver did not say (Rule 15: a decision is never anonymous).
		via := strings.TrimSpace(devSource)
		if via == "" {
			via = config.DevRepoSettingsKey
		}
		source = reposource.SourceDevRepo + " via " + via
	}
	fmt.Printf("  %-22s %s\n", configRunRepoLabel, fmt.Sprintf(configRunRepoResolvedFmt, res.Dir, source))
	if devErr != nil {
		fmt.Fprintf(os.Stderr, "%s: %s unreadable: %v\n", runRepoProgName, config.DevRepoSettingsKey, devErr)
	}
}
