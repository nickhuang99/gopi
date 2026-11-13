package main

// cli_config_env.go — `gopi config env`: measure this host and write the
// environment description the agent's prompt points at.
//
// WHY IT LIVES UNDER `gopi config` AND NOT AS A NEW TOP-LEVEL VERB: the
// content is configuration of the host — what machines this gopi can reach, at
// which addresses — and `gopi config` is already the verb that answers "what
// is in force on this machine, and where did it come from". A new top-level
// verb would be a new namespace for an existing question (PROJECT.md Rule 3's
// spirit: no new surface where an existing one answers).
//
// WHY A COMMAND AND NOT A BACKGROUND REFRESH (Rule 35, zero dependency): a
// resident refresher, a timer or a cron line would be a new persistent
// dependency whose whole job is to hide the moments when it fails — this repo
// has already paid for that shape once (the retired HP autostart, GOPI.md
// Rule 35). Generation is ONE SHOT, run by a person, and the artifact carries
// its own timestamp so a stale file is visible as stale. `gopi` never writes it
// during a run: the write paths are `--write` here and nothing else.
//
// THE VERB CANNOT LEAK A SECRET. It reads exactly two things — the kernel's own
// view of this host (interfaces, routing table, os-release) and the ssh config's
// stanza keys (Host/HostName/User/Port) — and it never opens a key file, an
// env file, or a settings file. Nothing it could print is a credential, which
// is the property that lets the artifact be handed to an agent at all.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nick/gopi/internal/envctx"
)

// configEnvVerb is the sub-verb word.
const configEnvVerb = "env"

// configEnvWriteFlag is the WRITING mode. The writing half is asked for BY
// NAME: a bare read must never rewrite the description
// (a generation run is a measurement, and a measurement taken by accident is
// a timestamp nobody can trust).
const configEnvWriteFlag = "--write"

// configEnvPathFlag points the verb at another location — the read arm of a
// test, or an operator generating a description for a peer's home.
const configEnvPathFlag = "--path"

// configEnvUsage is the sub-verb's help. `gopi config env -h` answers.
const configEnvUsage = `gopi config env — measure THIS host and render its environment description.

Usage:
  gopi config env            Print the description to stdout. Writes nothing.
  gopi config env --write    Measure and write it to the gopi profile
                             (default ` + envctx.FileName + `), then print the path.
  gopi config env --path P   Use P instead of the profile's own location
                             (combine with --write to materialise it there).
  gopi config env -h         This text.

What it is: the machine's own inventory — hostname, OS, interface addresses,
default gateway (all MEASURED by this run, timestamped) plus the ssh alias
table (read from ~/.ssh/config, reported AS WRITTEN, never dialled). Where two
sources disagree the disagreement is RECORDED, not resolved: when two aliases
claim one address, or a stanza's Host line contradicts its own HostName, both
spellings stay in the file with a sentence naming the ambiguity — picking one
for you would be inventing the fact that matters most.

It is NOT refreshed automatically and gopi never writes it during a run: a
description is a measurement at a moment, and a stale one should look stale.
Re-run this command when the machine changes. The file lives OUTSIDE version
control (the gopi profile) on purpose — it is a host property, not a project's,
and it names internal addresses that do not belong in a commit.

The agent reads it through the ordinary ` + "`read`" + ` tool: the prompt carries only
a summary and this file's path.
`

// isConfigEnvArg reports whether the config group's args name the `env`
// sub-verb. Kept as a predicate so the dispatch reads like its siblings.
func isConfigEnvArg(args []string) bool {
	return len(args) > 0 && args[0] == configEnvVerb
}

// runConfigEnv runs the sub-verb and OWNS its process exit.
func runConfigEnv(args []string) {
	write := false
	path := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help", HelpWord:
			fmt.Print(configEnvUsage)
			return
		case configEnvWriteFlag:
			write = true
		case configEnvPathFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "gopi config env: %s requires a path\n\n%s", configEnvPathFlag, configEnvUsage)
				os.Exit(1)
			}
			i++
			path = args[i]
		default:
			// An unknown flag is an ERROR, never ignored: a silently dropped
			// flag would report a description the caller did not ask for (the
			// same contract `gopi config show` keeps).
			fmt.Fprintf(os.Stderr, "gopi config env: unexpected argument %q\n\n%s", args[i], configEnvUsage)
			os.Exit(1)
		}
	}
	if path == "" {
		resolved, err := envctx.Path()
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi config env: %v\n", err)
			os.Exit(1)
		}
		path = resolved
	}

	facts := envctx.Measure()
	text := envctx.Render(facts)

	if !write {
		fmt.Print(text)
		// The read arm says WHERE a write would land and that the conflicts
		// above are real, so a reader piping this to a file still learns both.
		fmt.Fprintf(os.Stderr, "[gopi] config env: measured %d ssh alias(es), %d conflict(s); would write to %s\n",
			len(facts.Nodes), len(facts.Conflicts), path)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "gopi config env: cannot create %s: %v\n", filepath.Dir(path), err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "gopi config env: cannot write %s: %v\n", path, err)
		os.Exit(1)
	}
	// The success line is a VALUE with the two numbers that matter, and the
	// conflicts are named right here: an operator who runs this and sees
	// "0 conflicts" on a host where two aliases share an address has learned
	// something is wrong with the SOURCES, not with the addresses.
	fmt.Printf("wrote %s (%d bytes) — %d ssh alias(es), %d conflict(s) recorded, measured %s\n",
		path, len(text), len(facts.Nodes), len(facts.Conflicts),
		facts.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"))
	for _, c := range facts.Conflicts {
		fmt.Printf("  conflict: %s\n", c.Detail)
	}
}

// configEnvReportLine renders the `gopi config show` row for the description:
// present (with its size, so a reader can see it is a real document and not an
// empty stub) or absent WITH the remedy. A path that cannot be resolved at all
// says so rather than printing an empty column.
func configEnvReportLine() string {
	path, err := envctx.Path()
	if err != nil {
		return "unresolvable (" + err.Error() + ")"
	}
	fi, err := os.Stat(path)
	if err != nil {
		return path + "  (ABSENT — this run's agent knows nothing about this host's nodes; run `gopi config env --write`)"
	}
	return fmt.Sprintf("%s  (%d bytes)", path, fi.Size())
}
