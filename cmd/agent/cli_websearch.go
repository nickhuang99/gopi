package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// webSearchMethods is the valid set of method values for the web_search
// subcommand. It mirrors the tool's InputSchema enum; empty method = merged
// multi-method default.
var webSearchMethods = map[string]bool{
	"bing":     true,
	"google":   true,
	"arxiv":    true,
	"deepseek": true,
	"github":   true,
	"wiki":     true,
}

// webSearchDefaultCount is the default count (mirrors tools.webSearchDefaultCount).
const webSearchDefaultCount = 5

// webSearchMaxCount caps --count (mirrors tools.webSearchMaxCount).
const webSearchMaxCount = 20

// webPluginExitUnavailable is the exit status of a run whose payload carries
// status=unavailable — the tool could not run (no plugin scripts at the
// resolved directory, an unknown method, a script crash or timeout, a merge
// that produced nothing, unparseable output). It is the point of the
// status-to-exit mapping: a payload saying "unavailable" is a FAILURE, and a
// scripted caller branches on the status instead of on prose.
//
// ONE constant for BOTH faces of the plugin (web search and web fetch, Rule
// 14): they share the scripts directory, the python3 scripts and this failure
// class, so a caller wrapping `gopi web …` branches once. It is DISTINCT from
// the usage status (1): "your argument was wrong" and "the tool could not run"
// are different facts, and a caller retrying one must not retry the other —
// while status=error (a malformed URL the tool itself refuses) stays 0, because
// the tool RAN and answered.
const webPluginExitUnavailable = 3

// webSearchUsage is the help text for the web_search CLI subcommand. It is
// printed when the subcommand is missing required arguments, passes an invalid
// method/count, or the user passes -h/--help.
const webSearchUsage = `GoPi web search — expose the web_search tool as a CLI command.
Behavior is identical to the tool_use call (same function path — it runs the
websearch plugin scripts under ` + config.WebSearchScriptsDirEnvName + ` or
$HOME/.gopi/plugins/websearch/scripts).

Usage:
  gopi web search "<query>" [--method M] [--count N]
      --method is one of: bing, google, arxiv, deepseek, github, wiki.
      Run a web search and print the unified JSON payload ({status,count,results})
      exactly as the tool returns it.

  gopi web search "<query>"                     # merged bing+google+arxiv
  gopi web search "<query>" --method github     # github repository search
  gopi web search "<query>" --method wiki       # wikipedia articles
  gopi web search "<query>" --method deepseek   # AI answer

Flags:
  --method X   bing | google | arxiv | deepseek | github | wiki. Empty = merged
               multi-method default. Invalid values error with this usage.
  --count N    max results to return (default 5, max 20). Ignored for
               deepseek (AI-ask method).

Exit status — the payload is NOT the only signal:
  0   the payload carries results (status=ok). A search with no hits is
      status=empty, which is a natural answer, not a failure.
  1   a usage error (a missing <query>, an unknown flag, an invalid --method or
      --count) — the message is on stderr with this usage.
  3   the payload carries status=unavailable: the tool COULD NOT RUN (no plugin
      scripts at the resolved directory, an unknown method, a script crash or
      timeout, a merge that produced nothing). The JSON is still printed, and
      the reason is on stderr — a scripted caller can branch on the exit status
      without parsing the payload.

If the search reports status=unavailable, check the scripts directory the
failure names: the plugin scripts are python3 programs that live in the
operator's profile (` + config.WebSearchScriptsDirEnvName + ` overrides it), and
inside a sandbox that directory is a read-only link to them — a sandbox created
before the link existed prints the path it could not find.

The flat spelling gopi web_search is RETIRED, and this binary refuses it by
name: gopi web_search (no -h) exits 1 with the retirement line, and gopi
web_search -h answers with this page. Its dash-cased twin gopi web-search is
refused the same way — a spelling this binary has no command for is never spent
as a prompt (and never bought as a model run). The live spelling is
` + "`gopi " + CliVerbTool + " " + CliVerbWeb + " " + webGroupSubcmdSearch + "`" + `;
gopi web search is the same command under the web group.
`

// isWebSearchSubcommand reports whether argv begins with the RETIRED legacy
// `web_search` flat verb (pre-group surface). The word is recognised so it can
// be REFUSED BY NAME with its live home (`gopi tool web search`) — never run,
// never silently handed to the agent loop (epic verb-restructure, 2026-10-05;
// the refusal half is refuseWebToolFlatVerb). It returns false so the dispatch
// chain below it is unaffected for every other argv.
func isWebSearchSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == toolnames.ToolWebSearch {
		refuseWebToolFlatVerb(toolnames.ToolWebSearch, webSearchGroupProg, args[1:], webSearchUsage)
		return false // unreachable: refuseWebToolFlatVerb exits
	}
	return false
}

// webSearchGroupProg / webFetchGroupProg are the LIVE homes of the two web tool
// faces after the restructure: the `tool` group, where slice 1/5 collected the
// local tools under one noun (`gopi tool web search`, whose `web` word forwards
// to this same implementation). They are assembled from the constants that own
// each word (Rule 1b/13) rather than typed here.
var (
	webSearchGroupProg = "gopi " + CliVerbTool + " " + CliVerbWeb + " " + webGroupSubcmdSearch
	webFetchGroupProg  = "gopi " + CliVerbTool + " " + CliVerbWeb + " " + webGroupSubcmdFetch
)

// refuseWebToolFlatVerb answers the retired TOP-LEVEL flat spelling of a web
// tool (`gopi web_search`, `gopi web_fetch`). It has TWO arms, because a usage
// query is not a run (the P0-1 rule this repo states wherever a retired verb is
// `-h`-ed):
//
//   - `-h` is ANSWERED — the retirement line first, then the live home's own
//     page (the same text `gopi tool web search -h` prints) — and exits 0. The
//     operator asked what the word does; the honest answer is both "it moved"
//     and the page, and answering a question with a failure would be a lie
//     about the exit status.
//   - anything else is REFUSED (exit 1) with the retirement line and NOTHING
//     runs: no plugin script, no network, no session.
//
// WHY A REFUSAL AND NOT A FORWARD (epic verb-restructure, 2026-10-05). These
// two were deprecation aliases that PRINTED a note and then ran the same
// command as the canonical spelling. A top-level word with a home in a group
// stops being a door: two spellings for one command is exactly what the
// restructure removes. `usage` is the live home's page, passed by the caller
// from the SAME constant that page is printed from, so the two cannot drift.
func refuseWebToolFlatVerb(flat, home string, rest []string, usage string) {
	fmt.Fprintf(os.Stderr, "gopi %s: retired — use `%s` (the same tool, one spelling)\n", flat, home)
	if !hasHelpFlag(rest) {
		os.Exit(1)
	}
	fmt.Print(usage)
	os.Exit(0)
}

// hasHelpFlag reports whether a usage query was asked for in an argument list:
// `-h`/`--help` anywhere in it. It is the ONE predicate the retired flat tool
// verbs and their tests read, so "is this a run" cannot be decided differently
// in two places.
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == HelpFlag || a == HelpFlagLong {
			return true
		}
	}
	return false
}

// webSearchOptions is the parsed result of a web_search subcommand invocation.
type webSearchOptions struct {
	query  string
	method string
	count  int
}

// parseWebSearchArgs parses args for the web_search subcommand without
// touching the process (no os.Exit, no env mutation). It returns the parsed
// options or a usage-style error. Separate from dispatch so the parsing is
// unit-testable.
func parseWebSearchArgs(args []string) (*webSearchOptions, error) {
	fs := newMemoryFlagSet() // same hand-rolled `--name value` parser
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}
	for _, fname := range fs.order {
		if !webSearchFlagMember(fname) {
			return nil, fmt.Errorf("unknown flag --%s", fname)
		}
	}

	if len(positionals) == 0 {
		return nil, fmt.Errorf("missing <query> argument")
	}

	opts := &webSearchOptions{
		query:  strings.TrimSpace(strings.Join(positionals, " ")),
		method: strings.TrimSpace(fs.get("method")),
		count:  webSearchDefaultCount,
	}

	// Query is required.
	if opts.query == "" {
		return nil, fmt.Errorf("missing <query> argument")
	}

	// A bare `--method` (present, no value) is a usage error, not a silent
	// "merged" fallback: the help spells the flag as `--method X`, and empty is
	// how the flag is OMITTED, never how it is passed. Found by the G5
	// help-surface probe (cli_chat_web_help_surface_basic_test.go): the sibling
	// `--count` already refused a missing value, so the two flags of one synopsis
	// disagreed about what "no value" means.
	if fs.has("method") && opts.method == "" {
		return nil, fmt.Errorf("--method needs a value")
	}

	// Method must be a known enum value, else Usage error with the enum.
	if opts.method != "" && !webSearchMethods[opts.method] {
		return nil, fmt.Errorf("invalid --method %q (must be one of bing|google|arxiv|deepseek|github|wiki)", opts.method)
	}

	// Count: positive integer, default 5, max 20.
	if fs.has("count") {
		v, err := strconv.Atoi(strings.TrimSpace(fs.get("count")))
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("--count must be a positive integer, got %q", fs.get("count"))
		}
		opts.count = v
		if opts.count > webSearchMaxCount {
			opts.count = webSearchMaxCount
		}
	}

	return opts, nil
}

// webSearchFlagMember reports whether a flag name is valid for the web_search
// subcommand.
func webSearchFlagMember(flag string) bool {
	switch flag {
	case "method", "count":
		return true
	}
	return false
}

// runWebSearchSubcommand parses the subcommand's own flags and dispatches to
// the shared tools.WebSearch function so CLI behavior is identical to
// tool_use. It exits the process with a non-zero status on argument/usage
// errors, and — the half that was missing — with webSearchExitUnavailable when
// the payload says the tool could not run. The JSON is printed either way: the
// exit status is the machine-readable signal, the payload stays the detail, and
// dropping the payload would lose the reason the caller is being told about.
func runWebSearchSubcommand(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(webSearchUsage)
			os.Exit(0)
		}
	}

	opts, err := parseWebSearchArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi web search: %v\n%s", err, webSearchUsage)
		os.Exit(1)
	}

	out := tools.WebSearch(opts.query, opts.method, opts.count)
	fmt.Println(out)
	if tools.UnavailableStatus(out) {
		// The payload carries the reason (its `message`); stderr carries the
		// contract, so a caller reading only one stream still learns that the
		// tool did not run.
		fmt.Fprintf(os.Stderr, "gopi web search: the tool did not run (payload status=%s) — see the message in the JSON above; exit=%d\n",
			tools.WebSearchStatusUnavailable, webPluginExitUnavailable)
		os.Exit(webPluginExitUnavailable)
	}
}
