package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// webFetchDefaultMaxChars is the default --max-chars value (mirrors
// tools.webFetchDefaultMaxChars).
const webFetchDefaultMaxChars = 8000

// webFetchMaxMaxChars caps --max-chars (mirrors tools.webFetchMaxChars).
const webFetchMaxMaxChars = 100000

// webFetchExitUnavailable is NOT a second constant: both web faces report this
// failure class with the SAME status (webPluginExitUnavailable, cli_websearch.go)
// — one definition, referenced by both (Rule 14). The alias below exists so this
// file's call site reads in its own vocabulary without retyping the value.
const webFetchExitUnavailable = webPluginExitUnavailable

// webFetchUsage is the help text for the web_fetch CLI subcommand. It is
// printed when the subcommand is missing required arguments, passes an
// invalid --max-chars, or the user passes -h/--help.
const webFetchUsage = `GoPi web fetch — expose the web_fetch tool as a CLI command.
Behavior is identical to the tool_use call (same function path — the page is
fetched on the search-bot VPS jump host, boilerplate HTML is stripped, and
the text is tail-truncated to max_chars).

Search ≠ Read: web search returns snippets; web fetch reads ONE URL's
cleaned full text.

Usage:
  gopi web fetch <url> [--max-chars N]
      Fetch one http(s) URL and print its cleaned full text as the unified
      JSON payload ({status,url,chars,truncated,content,elapsed_ms,...})
      exactly as the tool returns it.

  gopi web fetch https://github.com/example/repo          # read a README
  gopi web fetch https://example.com/docs --max-chars 2000

Flags:
  --max-chars N   max characters of cleaned text to return (default 8000,
                  capped at 100000). Values <= 0 restore the default.

Exit status — the payload is NOT the only signal:
  0   the tool ran and answered (including status=error: a URL the tool itself
      refuses is an answer, not a failure).
  1   a usage error (a missing <url>, an unknown flag, a non-integer
      --max-chars) — the message is on stderr with this usage.
  3   the payload carries status=unavailable: the tool COULD NOT RUN (no
      web_fetch.py at the resolved scripts directory, a script crash or timeout,
      unparseable output). The JSON is still printed, and the reason is on
      stderr — a scripted caller can branch on the exit status without parsing
      the payload.

The flat spelling gopi web_fetch is RETIRED, and this binary refuses it by
name: gopi web_fetch (no -h) exits 1 with the retirement line, and gopi
web_fetch -h answers with this page. Its dash-cased twin gopi web-fetch is
refused the same way — a spelling this binary has no command for is never spent
as a prompt (and never bought as a model run). The live spelling is
` + "`gopi " + CliVerbTool + " " + CliVerbWeb + " " + webGroupSubcmdFetch + "`" + `;
gopi web fetch is the same command under the web group.
`

// isWebFetchSubcommand reports whether argv begins with the RETIRED legacy
// `web_fetch` flat verb (pre-group surface). Like its web_search sibling it is
// recognised only to be REFUSED BY NAME with its live home (`gopi web fetch`)
// — never run, never sent to the agent loop (epic verb-restructure,
// 2026-10-05).
func isWebFetchSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == toolnames.ToolWebFetch {
		refuseWebToolFlatVerb(toolnames.ToolWebFetch, webFetchGroupProg, args[1:], webFetchUsage)
		return false // unreachable: refuseWebToolFlatVerb exits
	}
	return false
}

// webFetchOptions is the parsed result of a web_fetch subcommand invocation.
type webFetchOptions struct {
	url      string
	maxChars int
}

// parseWebFetchArgs parses args for the web_fetch subcommand without
// touching the process (no os.Exit, no env mutation). It returns the parsed
// options or a usage-style error. Separate from dispatch so the parsing is
// unit-testable.
func parseWebFetchArgs(args []string) (*webFetchOptions, error) {
	fs := newMemoryFlagSet() // same hand-rolled `--name value` parser
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}
	for _, fname := range fs.order {
		if !webFetchFlagMember(fname) {
			return nil, fmt.Errorf("unknown flag --%s", fname)
		}
	}

	if len(positionals) == 0 {
		return nil, fmt.Errorf("missing <url> argument")
	}

	opts := &webFetchOptions{
		url:      strings.TrimSpace(strings.Join(positionals, " ")),
		maxChars: webFetchDefaultMaxChars,
	}

	// URL is required. Scheme validation stays in tools.WebFetch, which
	// returns a structured error payload the CLI prints verbatim.
	if opts.url == "" {
		return nil, fmt.Errorf("missing <url> argument")
	}

	// Max chars: an integer, default 8000, capped at 100000, and <= 0 restores
	// the default. The <= 0 arm is the HELP's own promise ("Values <= 0 restore
	// the default") and the rule the tool layer already applies
	// (tools.WebFetch → clampWebFetchMaxChars). The G5 help-surface probe
	// (cli_chat_web_help_surface_basic_test.go) drives both spellings, because
	// refusing them here was a disagreement with the page: the same argv
	// reached the tool_use path and restored the default.
	if fs.has("max-chars") {
		v, err := strconv.Atoi(strings.TrimSpace(fs.get("max-chars")))
		if err != nil {
			return nil, fmt.Errorf("--max-chars must be an integer, got %q", fs.get("max-chars"))
		}
		if v > webFetchMaxMaxChars {
			v = webFetchMaxMaxChars
		} else if v <= 0 {
			v = webFetchDefaultMaxChars
		}
		opts.maxChars = v
	}

	return opts, nil
}

// webFetchFlagMember reports whether a flag name is valid for the web_fetch
// subcommand.
func webFetchFlagMember(flag string) bool {
	switch flag {
	case "max-chars":
		return true
	}
	return false
}

// runWebFetchSubcommand parses the subcommand's own flags and dispatches to
// the shared tools.WebFetch function so CLI behavior is identical to
// tool_use. It exits the process with a non-zero status on argument/usage
// errors; fetch-level failures come back as the tool's structured JSON
// payload and print verbatim. A payload that says the tool could not run
// (status=unavailable) additionally exits webFetchExitUnavailable, so a script
// can branch without parsing the payload.
func runWebFetchSubcommand(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(webFetchUsage)
			os.Exit(0)
		}
	}

	opts, err := parseWebFetchArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi web fetch: %v\n%s", err, webFetchUsage)
		os.Exit(1)
	}

	out := tools.WebFetch(opts.url, opts.maxChars)
	fmt.Println(out)
	if tools.UnavailableStatus(out) {
		// The JSON above carries the reason (its `message`); stderr carries the
		// contract, so a caller reading only one stream learns the tool did not
		// run. A status=error payload is NOT this path: there the tool ran and
		// refused the input, which is an answer.
		fmt.Fprintf(os.Stderr, "gopi web fetch: the tool did not run (payload status=%s) — see the message in the JSON above; exit=%d\n",
			tools.WebSearchStatusUnavailable, webFetchExitUnavailable)
		os.Exit(webFetchExitUnavailable)
	}
}
