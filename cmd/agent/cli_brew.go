// gopi memory brew — CLI subcommand exposing the M3 brew pipeline: sweep session
// JSONLs, LLM-judge the three lesson conditions, distill QA rows, store them
// tagged source=brew. Also the trigger hook: gopi's session-exit calls
// RunBrew once per 24h (GOPI_BREW_INTERVAL_SEC overrides; 0 disables).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// brew CLI constants (Rule 1/2: every tunable in one place).
const (
	// The flags `gopi memory brew` accepts, named ONCE (Rule 1b/13). THREE
	// surfaces read this vocabulary and none may spell a flag inline:
	//   - the parser table (newBrewFlagSet), which is STRICT: an undeclared
	//     flag is refused BY NAME instead of being silently accepted;
	//   - brewUsage's Usage line, through brewSynopsis;
	//   - the group page's synopsis for this verb (memoryUsage), through
	//     memoryBrewSynopsisBlock.
	// The group page advertised `[--since DURATION] [--verbose]` until
	// 2026-11-05 — two flags this verb has never parsed, borrowed from the
	// neighbouring `memory sync` / `task sync` pages. A page may not promise a
	// flag its parser refuses, and the parser may not accept one the page
	// never mentions.
	brewFlagDryRun  = "dry-run"
	brewFlagJSON    = "json"
	brewFlagLimit   = "limit"
	brewFlagSession = "session"
	brewFlagDir     = "dir"

	// brewSynopsis is the ONE usage line for this verb — the flag set as a
	// reader meets it. Both help pages print it, so a flag added to or removed
	// from the tables below moves both pages with it.
	brewSynopsis = "gopi memory brew [--dry-run] [--limit N] [--session PATH] [--dir PATH] [--json]"

	// memoryBrewSynopsisBlock is this verb's block on the MEMORY GROUP page
	// (`gopi memory -h`). It is BUILT from brewSynopsis (const concatenation),
	// so the group page cannot advertise a flag set the parser does not accept.
	//
	// REBASE NOTE (delivery round 2/3, commit 9c83cfa8 onto 2e3ec8d2): the two
	// sides are ORTHOGONAL and both are kept. This branch rewrote the FIRST
	// line (the flag set — `[--since DURATION] [--verbose]` were flags brew
	// never parsed) and hoisted the block into this constant; master (2e3ec8d2)
	// rewrote the PROSE below it — `gopi brew` is the RETIRED flat door and the
	// page has to say so instead of naming `gopi memory brew` as "the flat
	// spelling", which is what the old sentence did and which is false (that
	// string IS the live home). Neither half may be dropped: the flags are this
	// run's deliverable, the retirement claim is master's, and the sentence
	// "the same pipeline this spelling runs" is master's correction of a
	// self-reference this branch had carried over unchanged.
	memoryBrewSynopsisBlock = "  " + brewSynopsis + `
      Distill reusable lessons from recent session logs INTO this knowledge
      base (the producing half of the memory dimension). The transfer half is
      the MANUAL 'gopi memory sync --peer <alias>' session — a unique-only
      merge of the two nodes' knowledge bases, bidirectional unless --push or
      --pull names one direction — beside the one-way 'gopi memory backup'
      snapshot. Neither is automatic: nothing schedules either leg.
      The same pipeline this spelling runs — 'gopi memory brew -h' carries
      the flag set and the exit codes.
      (The flat ` + brewProg + ` door is RETIRED: it refuses and exits 1.)`

	brewUsage = `GoPi brew — distill reusable lessons from session logs into the knowledge base.

Usage:
  ` + brewSynopsis + `
      Sweep ~/.gopi/sessions/*.jsonl (newest first), judge each session slice
      against the three lesson conditions (可重复模式 / 可复现闭环 / 收益达标),
      distill surviving lessons into Q&A rows (mixed 中英 breadcrumbs) and
      store them tagged source=brew. --dry-run shows what WOULD be stored
      without writing anything. --limit N caps the number of sessions.

Trigger (session exit):
  Runs at most once per 24h at gopi session exit (GOPI_BREW_INTERVAL_SEC=0
  disables; >0 overrides the interval in seconds).`

	brewStateFile = "brew_state.json"

	// brewProg is the FLAT, RETIRED spelling of the brew pipeline (`gopi
	// brew`), quoted where a page must say that word refuses — never as a
	// spelling to use. Rule 1b: the retired spelling lives in a constant, the
	// same way the live one does (the memory page names both).
	brewProg = "gopi " + CliVerbBrew
)

// The brew flag set, split by ARITY: the parser needs the distinction (a
// boolean must never claim the following token) and the synopsis shows it
// (`--limit N` carries a value, `--dry-run` does not).
var (
	brewBooleanFlagNames = []string{brewFlagDryRun, brewFlagJSON}
	brewValueFlagNames   = []string{brewFlagLimit, brewFlagSession, brewFlagDir}
)

// newBrewFlagSet returns the flag table for `gopi memory brew`: every flag this
// verb accepts, declared via expect()/boolean().
//
// Declaring them is what makes the parser STRICT for this verb (and only this
// verb — newMemoryFlagSet() stays permissive for the family's other callers):
// before this table existed, `gopi memory brew --sinc 2099-01-01` was ACCEPTED
// and the pipeline ran anyway, paying for a full LLM sweep on a typo. An
// undeclared flag must now be refused by name, before the engine is built.
func newBrewFlagSet() *memoryFlagSet {
	fs := newMemoryFlagSet()
	for _, b := range brewBooleanFlagNames {
		fs.boolean(b)
		fs.expect(b)
	}
	for _, v := range brewValueFlagNames {
		fs.expect(v)
	}
	return fs
}

// brewSubcommand is the PREDICATE half of isBrewSubcommand: it decides
// whether argv begins with the brew verb and executes NOTHING. It is split
// out because the decision is the unit a caller (and a test) can
// legitimately make without paying for the subcommand's side effects:
// runBrewSubcommand builds the brew engine, which needs an LLM credential,
// and exits the process (os.Exit) when one is absent — so a test that only
// wants to know "does argv[0] == brew?" must not go through the executing
// half. Rule 1b: the verb comes from CliVerbBrew, never a bare literal.
func brewSubcommand(args []string) bool {
	return len(args) > 0 && args[0] == CliVerbBrew
}

// isBrewSubcommand reports whether argv begins with the brew subcommand.
// When it does it runs the subcommand and returns true (the caller must not
// treat the remaining args as an input file) — the cli_memory.go pattern.
// The decision itself lives in brewSubcommand (the pure predicate); this
// function is the dispatch half and must keep executing.
func isBrewSubcommand(args []string) bool {
	if brewSubcommand(args) {
		runBrewSubcommand(args[1:])
		return true
	}
	return false
}

// runBrewSubcommand parses flags and runs the pipeline. Exit codes: 0 ok,
// 1 usage/fatal error, 2 partial failures (progress made, some errors).
//
// ARGUMENT ERRORS COST NOTHING: the flag table is strict (newBrewFlagSet), so
// an unknown flag is refused here — before NewBrewEngine resolves a credential
// and before RunBrew spends an LLM call. `-h` ANYWHERE in argv prints the page,
// which is the family's convention (runMemorySubcommand / runMemorySync /
// runMemoryBackup all scan the whole argv) and matches the top-level dispatch,
// which resolves -h before this function is reached.
func runBrewSubcommand(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(brewUsage)
			os.Exit(0)
		}
	}
	fs := newBrewFlagSet()
	positional, err := fs.parse(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi memory brew: %v\n\n%s", err, brewUsage)
		os.Exit(1)
	}
	if len(positional) > 0 {
		fmt.Fprintf(os.Stderr, "gopi memory brew: unexpected argument %q\n\n%s", positional[0], brewUsage)
		os.Exit(1)
	}
	opts := tools.RunBrewOptions{
		DryRun:      fs.has(brewFlagDryRun),
		SessionsDir: fs.get(brewFlagDir),
	}
	if p := fs.get(brewFlagSession); p != "" {
		opts.SessionPaths = []string{p}
	}
	if v := fs.get(brewFlagLimit); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			fmt.Fprintf(os.Stderr, "gopi memory brew: --limit must be a positive integer\n")
			os.Exit(1)
		}
		opts.Limit = n
	}
	opts.Progress = func(format string, a ...interface{}) {
		fmt.Printf("  "+format+"\n", a...)
	}

	bc, err := tools.NewBrewEngine()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi memory brew: %v\n", err)
		os.Exit(1)
	}
	stats, err := tools.RunBrew(context.Background(), bc, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi memory brew: %v\n", err)
		os.Exit(1)
	}
	if fs.has(brewFlagJSON) {
		out, _ := json.MarshalIndent(stats, "", "  ") //stylegate:ignore pretty-printing an in-process value for a log line; a failure is non-fatal
		fmt.Println(string(out))
	}
	printBrewStats(stats)
	if len(stats.Errors) > 0 && stats.Stored+stats.Deduped > 0 {
		os.Exit(2) // partial success
	}
}

// printBrewStats renders the run summary for humans (also after --json).
func printBrewStats(stats *tools.BrewStats) {
	fmt.Printf("brew done in %.1fs: sessions=%d chunks=%d accepted=%d rejected=%d stored=%d deduped=%d errors=%d\n",
		stats.ElapsedSec, stats.SessionsScanned, stats.ChunksJudged, stats.LessonsAccepted,
		stats.LessonsRejected, stats.Stored, stats.Deduped, len(stats.Errors))
	for _, e := range stats.Errors {
		fmt.Printf("  ! %s\n", e)
	}
}

// maybeBrewOnSessionExit is the session-end trigger. Called by the agent at
// chat exit; at most one LLM sweep per interval (default 24h, state kept in
// ~/.gopi/brew_state.json). Never fails the session: all errors are stderr
// noise. Interval 0 (GOPI_BREW_INTERVAL_SEC=0) disables entirely.
func maybeBrewOnSessionExit() {
	interval := tools.BrewDefaultInterval
	if v := os.Getenv(tools.BrewIntervalEnv); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return // malformed → treat as disabled
		}
		interval = time.Duration(n) * time.Second
	}
	if interval == 0 {
		return
	}
	statePath, err := brewStatePath()
	if err != nil {
		return
	}
	if st, err := os.Stat(statePath); err == nil && time.Since(st.ModTime()) < interval {
		return // ran recently
	}
	// Touch the state file FIRST — a crashed brew must not loop-retry on
	// every session exit (cost budget ④).
	payload := `{"last_run":"` + time.Now().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(statePath, []byte(payload), 0644); err != nil {
		return
	}
	go func() {
		bc, err := tools.NewBrewEngine()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[brew] %v\n", err)
			return
		}
		stats, err := tools.RunBrew(context.Background(), bc, tools.RunBrewOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[brew] %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "[brew] background sweep: sessions=%d stored=%d deduped=%d errors=%d\n",
			stats.SessionsScanned, stats.Stored, stats.Deduped, len(stats.Errors))
	}()
}

// brewStatePath resolves <gopi dir>/brew_state.json.
func brewStatePath() (string, error) {
	dir, err := config.GopiDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, brewStateFile), nil
}
