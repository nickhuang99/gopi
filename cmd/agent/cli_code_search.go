package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/tools"
)

// codeSearchDefaultRoot is the search root when --root is omitted (the
// current directory — matches the agent loop's codeSearchScope()).
const codeSearchDefaultRoot = "."

// codeSearchMaxResults caps the returned symbol hits (mirrors the agent
// loop's maxSearchResults so the CLI never floods a script with one noisy
// symbol's full cluster).
const codeSearchMaxResults = 50

// codeSearchUsage is the help text for the code-search CLI subcommand. It is
// printed when the subcommand is missing the required <symbol> argument or
// the user passes -h/--help.
//
// It is a FORMAT string with one %d per exit code (0, 1, %d-as-refusal below):
// the code table and the process's own os.Exit calls then read the SAME
// constants (Rule 13 — a documented code that drifted from the implemented one
// is a defect of the same cost as a wrong code). Render it with
// codeSearchUsageText(), never by printing this constant: the %d's would reach
// the operator verbatim.
const codeSearchUsage = `GoPi code-search subcommand — expose the code_search tool as a CLI command.
Behavior is identical to the tool_use call (same function path — it builds a
ctags index at .gopi-tags, lazily regenerating it when stale, and falls back
to ripgrep on a clean ctags miss).

Usage:
  gopi tool code-search <symbol> [--root DIR]
      Look up a symbol definition in the current directory (or --root DIR)
      and print structured hits — one per definition:
        <name>  <file>:<line>  <kind>  <signature>
      greppable (file:line is always present for a definition). A hit with
      kind "reference" comes from the ripgrep fallback (occurrence, not a
      ctags definition).

  gopi tool code-search NewAgent                    # look up in current dir
  gopi tool code-search codeSearchScope --root internal/agent

Exit status:
  %d    at least one hit printed
  %d    the search RAN on a real tree and found nothing — printed as
        "no symbol found: <symbol>" — or a usage error, or the ctags index
        could not be built (ctags not installed → clear message)
  %d    REFUSED — the search could NOT run: no code repository at the search
        root. The refusal names the absolute root, the channel it came from
        (--root or the default cwd), the git work tree it sits in, and what
        the walk saw. This is NOT "found nothing": nothing was searched, so a
        caller that only checks the miss code would read it as an answer about
        a tree that was never read. The same code is used by gopi
        keyword_search and by gopi task show --branch ("no row records it" is an answer,
        not a failure).

Flags:
  --root DIR   search root (default: the current directory — the command
               searches WHERE YOU STAND, so the cwd premise is part of the
               call). It must exist and hold at least one file this search can
               read that is NOT gopi's own state (the profile directory and a
               sandbox's ownership marker are not searchable material); a
               directory with nothing searchable is REFUSED (see the exit
               table above) and the refusal names what it found. Remedy: pass
               --root DIR naming the repository, or run the command from
               inside it.
`

// codeSearchUsageArgs returns the fmt arguments codeSearchUsage expects — the
// exit codes of this verb, in the page's own order. It is the ONE definition of
// that list: codeSearchUsageText() renders with it, and the width sweep
// (help_width_test.go) measures with it, so the lines a test measures are the
// lines a user sees (Rule 14 — a second literal list would be a mirror).
func codeSearchUsageArgs() []any {
	return []any{searchExitOK, searchExitNoHits, searchExitWrongGround}
}

// codeSearchUsageText renders the page above with the REAL exit codes, so the
// printed table cannot disagree with the code path (Rule 13).
func codeSearchUsageText() string {
	return fmt.Sprintf(codeSearchUsage, codeSearchUsageArgs()...)
}

// isCodeSearchSubcommand reports whether argv begins with the code-search
// subcommand. When it does it runs the subcommand and returns true (the
// caller must not treat the remaining args as an input file).
func isCodeSearchSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == CliVerbCodeSearch {
		runCodeSearchSubcommand(args[1:])
		return true
	}
	return false
}

// codeSearchOptions is the parsed result of a code-search subcommand
// invocation.
type codeSearchOptions struct {
	symbol string
	root   string
	// rootGiven records whether --root was passed at all: the refusal names the
	// CHANNEL the root came from (the flag vs the default cwd), and that fact is
	// lost the moment "" is defaulted to "." (Rule 15 — a channel nobody can
	// name is a channel the caller cannot fix).
	rootGiven bool
}

// parseCodeSearchArgs parses args for the code-search subcommand without
// touching the process (no os.Exit, no env mutation). It returns the parsed
// options or a usage-style error. Separate from dispatch so the parsing is
// unit-testable.
func parseCodeSearchArgs(args []string) (*codeSearchOptions, error) {
	fs := newMemoryFlagSet() // same hand-rolled `--name value` parser
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}
	for _, fname := range fs.order {
		if fname != "root" {
			return nil, fmt.Errorf("unknown flag --%s", fname)
		}
	}

	if len(positionals) == 0 {
		return nil, fmt.Errorf("missing <symbol> argument")
	}
	if len(positionals) > 1 {
		return nil, fmt.Errorf("unexpected extra argument %q (code-search takes exactly one <symbol>)", positionals[1])
	}

	opts := &codeSearchOptions{
		symbol:    strings.TrimSpace(positionals[0]),
		root:      fs.get(searchRootFlag),
		rootGiven: fs.has(searchRootFlag),
	}
	if opts.root == "" {
		opts.root = codeSearchDefaultRoot
	}
	if opts.symbol == "" {
		return nil, fmt.Errorf("missing <symbol> argument")
	}
	return opts, nil
}

// runCodeSearchSubcommand parses the subcommand's own flags and dispatches to
// the shared tools.CodeSearch function so CLI behavior is identical to
// tool_use. It exits the process with a non-zero status on argument/usage
// errors, a not-found symbol, or a backend (ctags) failure.
func runCodeSearchSubcommand(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(codeSearchUsageText())
			os.Exit(0)
		}
	}

	opts, err := parseCodeSearchArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool code-search: %v\n%s", err, codeSearchUsageText())
		os.Exit(1)
	}

	// Scope mirrors the agent loop's codeSearchScope() (see loop.go), with
	// --root overriding the search root and the same indexed-language globs,
	// exclusions and result cap. The root is resolved to an absolute path so
	// the backend's ctags invocation (which sets its working dir to the root
	// and writes the tags file at root/.gopi-tags) works for any relative
	// --root too — a relative tagsPath composed against a non-cwd root would
	// otherwise point at a nonexistent <root>/<root>/.gopi-tags.
	root := opts.root
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	scope := tools.SearchScope{
		RootPath:    root,
		IncludeGlob: tools.CodeSearchGlobs,
		ExcludeGlob: []string{".git/**", "vendor/**"},
		MaxResult:   codeSearchMaxResults,
	}

	// The shared ground guard (cli_search_root.go) — the ONE place the two
	// search verbs decide whether a search can run at all. A root with nothing
	// this scope can read is a REFUSAL with its own exit code, never a "no
	// symbol found": the two were byte-alike before and told the caller opposite
	// things (devdoc/cli-search-wrong-ground-refusal.md).
	searchGuardOrExit("gopi tool code-search", root, searchRootChannel(opts.rootGiven), scope.IncludeGlob)

	hits, dropped, searchCap, err := tools.CodeSearch(scope, opts.symbol)
	if err != nil { // ctags not installed (or the index build failed) → explicit, friendly
		// error instead of a raw exec error. Scripts can key on the exit code.
		fmt.Fprintf(os.Stderr, "gopi tool code-search: %v\n", err)
		os.Exit(1)
	}

	// The same rule as the keyword verb: a truncated READ cannot say "no symbol
	// found" (the query did not reach the end), and a partial list says so
	// underneath. FormatCodeSymbolHitsWithCap owns both cases; a non-truncated
	// call renders exactly as before.
	if searchCap.Stopped {
		fmt.Print(tools.FormatCodeSymbolHitsWithCap(opts.symbol, hits, dropped, searchCap))
		fmt.Println()
		return
	}

	if len(hits) == 0 {
		fmt.Printf("no symbol found: %s\n", opts.symbol)
		os.Exit(searchExitNoHits)
	}

	fmt.Print(tools.FormatCodeSymbolHits(opts.symbol, hits, dropped))
	fmt.Println()
}
