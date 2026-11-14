package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/nickhuang99/gopi/internal/proxy"
)

// Proxy startup banner (Rule 1/13). The readiness phrase is a CONSTANT because
// this file and the suite's regression guard must branch on the same words.
// The sentence `gopi tool proxy: listening on …` is a promise to a caller — requests
// for this address will be served — so it has exactly one emission site
// (proxyServe), which is handed a listener net.Listen already bound: the
// ordering is structural, not a matter of remembering the right sequence.
const (
	// proxyBannerPrefix labels every startup line this subcommand prints.
	proxyBannerPrefix = "gopi tool proxy: "
	// proxyListeningPrefix opens the readiness banner. It may never precede a
	// successful bind, which is what the busy-port regression test pins.
	proxyListeningPrefix = proxyBannerPrefix + "listening on "
)

// proxyBannerAddr is the address the startup line names: the bound listener's
// OWN address, so a `--listen 127.0.0.1:0` reports the port the kernel chose
// rather than the literal 0 the operator asked for. A "[::]" host is rendered
// the way `ss -ltn` renders it, so the printed line can be matched against the
// kernel's own table line by line.
func proxyBannerAddr(addr string, ln net.Listener) string {
	if ln == nil {
		return addr
	}
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil || port == "" {
		return addr
	}
	if host == "" || host == "::" {
		host = proxyWildcardHost
	}
	return net.JoinHostPort(host, port)
}

// proxyWildcardHost is how a wildcard bind is spelled in the startup line: the
// same token `ss -ltn` prints for a v6 socket that accepts v4 too.
const proxyWildcardHost = "[::]"

// proxyServe prints the readiness banner and serves the already-bound listener.
// The banner is emitted here, after the caller's net.Listen returned, and this
// function's parameters carry no way to print it earlier — so "listening on"
// cannot precede the bind on any path, however the startup code is later
// refactored (the busy-port regression test drives exactly the denied case).
//
// The caller does not serve: it hands over the listener it bound itself
// (proxyStartListener), which is what keeps the bind below the banner by
// CONSTRUCTION rather than by remembering the order.
func proxyServe(ln net.Listener, addr, mode string, up proxy.UpstreamConfig, verbose bool, logger *log.Logger) error {
	srv := proxy.NewServer(up, verbose, logger)
	httpSrv := &http.Server{Handler: srv.Handler()} // No read/write timeouts: SSE streams must be able to idle.
	fmt.Fprintf(os.Stderr, "%s%s (upstream=%s base=%s model=%s)\n",
		proxyListeningPrefix, proxyBannerAddr(addr, ln), mode, up.BaseURL, up.Model)
	return httpSrv.Serve(ln)
}

const proxyUsage = `Usage: gopi tool proxy --upstream openai|anthropic [options]

Local Anthropic->OpenAI protocol proxy. Listens on an HTTP port and accepts
Anthropic /v1/messages requests (stream + non-stream), translates them to the
OpenAI Chat Completions protocol, forwards to an upstream, and translates the
response back to the Anthropic SSE format. Point any Anthropic client's
base_url at this proxy to talk to an OpenAI-only provider (e.g. GLM-5.3-Flash).

Options:
  --listen ADDR            Address to listen on. Default 127.0.0.1:8090.
  --upstream MODE          Requires 'openai' or 'anthropic'.
  --openai-base-url URL    OpenAI-compatible base URL (--upstream openai).
                           e.g. https://open.bigmodel.cn/api/paas/v4
  --openai-model MODEL     Model name, e.g. glm-5.3-flash.
  --openai-key KEY         Upstream API key.
  --anthropic-base-url URL Upstream Anthropic base URL (--upstream anthropic).
  --anthropic-key KEY      Upstream API key for the anthropic mode.
  --anthropic-model MODEL  Optional model override for anthropic mode.
  --verbose                Log every request/response.
  -h, --help               Show this help.
`

// proxyOptions holds parsed values for the proxy subcommand.
type proxyOptions struct {
	listen         string
	upstream       string
	openaiBase     string
	openaiModel    string
	openaiKey      string
	anthropicBase  string
	anthropicKey   string
	anthropicModel string
	verbose        bool
}

// isProxySubcommand reports whether argv begins with the proxy subcommand. When
// it does it runs the subcommand and returns true.
func isProxySubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == CliVerbProxy {
		// ARGV[0] IS THE WORD, AND THE PARSER WANTS ONLY THE TAIL: parseProxyArgs
		// switches on flags from the FIRST element, so the word must be dropped
		// here — `runProxySubcommand(args)` would hand it `["proxy","--upstream",…]`
		// and the parser would report `unexpected argument "proxy"` (measured).
		//
		// The defect that was here was NOT this slice (task 01a113ac…, measured
		// 2026-10-07): `gopi tool proxy` fell through to the group's unknown-word
		// refusal BEFORE reaching this arm, because the chain's isToolSubcommand
		// hands the word to runToolSubcommand, which looked the word up in
		// toolSubcommandPredicates and found `proxy` missing — so the address the
		// group PRINTS (`gopi tool proxy`, in its own usage block) was the one
		// spelling that could not run. The arm is reached at argv[0] (`gopi
		// proxy`, the retired spelling), and the fix that makes the GROUP address
		// work is the registry entry + the bare non-carrier rule in
		// runToolSubcommand, both in cli_tool.go.
		runProxySubcommand(args[1:])
		return true
	}
	return false
}

// parseProxyArgs parses the proxy subcommand arguments without mutating the
// process. It returns the options or an error.
func parseProxyArgs(args []string) (*proxyOptions, error) {
	opts := &proxyOptions{
		listen:   "127.0.0.1:8090",
		upstream: "",
	}
	var positionals []string
	var unknown []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			fmt.Print(proxyUsage)
			os.Exit(0)
		case a == "--verbose":
			opts.verbose = true
		case a == "--upstream":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--upstream requires a value")
			}
			i++
			opts.upstream = args[i]
		case a == "--listen":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--listen requires a value")
			}
			i++
			opts.listen = args[i]
		case a == "--openai-base-url":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--openai-base-url requires a value")
			}
			i++
			opts.openaiBase = args[i]
		case a == "--openai-model":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--openai-model requires a value")
			}
			i++
			opts.openaiModel = args[i]
		case a == "--openai-key":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--openai-key requires a value")
			}
			i++
			opts.openaiKey = args[i]
		case a == "--anthropic-base-url":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--anthropic-base-url requires a value")
			}
			i++
			opts.anthropicBase = args[i]
		case a == "--anthropic-key":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--anthropic-key requires a value")
			}
			i++
			opts.anthropicKey = args[i]
		case a == "--anthropic-model":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--anthropic-model requires a value")
			}
			i++
			opts.anthropicModel = args[i]
		default:
			if len(a) > 0 && a[0] == '-' {
				unknown = append(unknown, a)
			} else {
				positionals = append(positionals, a)
			}
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown flag %s", unknown[0])
	}
	if len(positionals) > 0 {
		return nil, fmt.Errorf("unexpected argument %q", positionals[0])
	}
	if opts.upstream == "" {
		return nil, fmt.Errorf("--upstream is required (openai|anthropic)")
	}
	return opts, nil
}

// runProxySubcommand runs `gopi tool proxy ...`.
func runProxySubcommand(args []string) {
	opts, err := parseProxyArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool proxy: %v\n\n%s", err, proxyUsage)
		os.Exit(1)
	}

	var up proxy.UpstreamConfig
	switch opts.upstream {
	case "openai":
		if opts.openaiBase == "" || opts.openaiKey == "" || opts.openaiModel == "" {
			fmt.Fprintln(os.Stderr, "gopi tool proxy: --upstream openai requires --openai-base-url, --openai-model and --openai-key")
			os.Exit(1)
		}
		up = proxy.UpstreamConfig{BaseURL: opts.openaiBase, Model: opts.openaiModel, Key: opts.openaiKey}
	case "anthropic":
		if opts.anthropicBase == "" || opts.anthropicKey == "" {
			fmt.Fprintln(os.Stderr, "gopi tool proxy: --upstream anthropic requires --anthropic-base-url and --anthropic-key")
			os.Exit(1)
		}
		// An Anthropic-mode upstream still speaks the OpenAI wire protocol only
		// when pointed at an OpenAI-compatible endpoint; for a true
		// Anthropic-native upstream we'd forward Anthropic-to-Anthropic. This
		// mode keeps the classic GLM (OpenAI-only) translation but against a
		// different base URL/key/model.
		model := opts.anthropicModel
		if model == "" {
			model = "default"
		}
		up = proxy.UpstreamConfig{BaseURL: opts.anthropicBase, Model: model, Key: opts.anthropicKey}
	default:
		fmt.Fprintf(os.Stderr, "gopi tool proxy: invalid --upstream %q (expected openai or anthropic)\n", opts.upstream)
		os.Exit(1)
	}

	// Wire verbose logging to stderr. The proxy's own log lines already carry
	// a "[proxy]" label, so no extra prefix here.
	var logger *log.Logger
	if opts.verbose {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	// BIND, then announce — never the other way round. net.Listen is the only
	// thing that can refuse the address, and its error is reported before any
	// line claims the address is served; a busy or denied port therefore exits
	// rc=1 with the bind error and NO readiness banner (the ordering the
	// regression test in cli_proxy_test.go pins).
	ln, err := net.Listen("tcp", opts.listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s%v\n", proxyBannerPrefix, err)
		os.Exit(1)
	}
	err = proxyServe(ln, opts.listen, opts.upstream, up, opts.verbose, logger)
	if err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "%s%v\n", proxyBannerPrefix, err)
		os.Exit(1)
	}
}
