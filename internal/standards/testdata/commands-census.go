// Command commands-census renders the generated verb index inside COMMANDS.md
// from the source that decides the CLI's verbs (GOPI.md Rule 38: a figure a
// reader acts on must name how it is re-derived, and here the generator IS the
// derivation).
//
// It lives under testdata/ because it is a MAIN package: the Go tool ignores
// testdata directories, so this file is never compiled by `go build ./...` or
// `go test ./internal/standards/`, while a reader can still run it verbatim.
// Nothing in the build executes it — the gate reads the DOCUMENT, never this
// program (the same shape internal/standards/testdata/repo-hygiene-census.go
// has with its freeze table).
//
// RUN (rewrites the generated block in the document):
//
//	go run internal/standards/testdata/commands-census.go . COMMANDS.md
//
// RE-DERIVE WITHOUT WRITING (prints the delta and exits 1 when the document
// and the source disagree):
//
//	go run internal/standards/testdata/commands-census.go . COMMANDS.md --check
//
// The verb vocabulary is read by internal/cliindex, the SAME reader the gate
// uses, so the document and the gate cannot disagree about what the CLI
// offers: the source is cmd/agent/cli_verbs.go (verb spellings) joined with
// cmd/agent/cli_contracts_test.go's dispatcherOfToken map (routing).
//
// The argv[0] COLUMN. The two sources above decide which words exist and how
// they are routed; neither states the LEVEL a word is routed at, so the block
// taught refused spellings as if they ran (`gopi capture` next to
// `gopi task`) — measured 2026-10-06, COMMANDS.md §4 sending readers to
// thirteen commands that exit 1. The level comes from the refusal table
// instead: config.AliasAddresses, the map the argv[0] refusal AND the
// positional guard both read (internal/config/cli_verb_guard.go). This
// program is a MAIN package outside the module's library surface, so it may
// import internal/config and hand the table to cliindex (which cannot), and
// the gate imports the same table and renders the same cell — one authority,
// two consumers.
package main

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/cliindex"
	"github.com/nickhuang99/gopi/internal/config"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: commands-census <repo-root> <COMMANDS.md> [--check]")
		os.Exit(2)
	}
	root, docPath := os.Args[1], os.Args[2]
	check := len(os.Args) > 3 && os.Args[3] == "--check"

	// The level authority: the same table the refusal prints its address from.
	cliindex.SetAliasAddresses(func(word string) string { return config.AliasAddresses()[word] })

	verbs, err := cliindex.Load(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "commands-census: %v\n", err)
		os.Exit(1)
	}
	block, err := cliindex.Block(verbs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "commands-census: %v\n", err)
		os.Exit(1)
	}
	raw, err := os.ReadFile(docPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "commands-census: %v\n", err)
		os.Exit(1)
	}
	updated, err := cliindex.ReplaceBlock(string(raw), block)
	if err != nil {
		fmt.Fprintf(os.Stderr, "commands-census: %v\n", err)
		os.Exit(1)
	}

	if check {
		if updated == string(raw) {
			fmt.Printf("commands-census: %s is in step with the source (%d verbs)\n", docPath, len(verbs))
			return
		}
		fmt.Fprintf(os.Stderr, "commands-census: %s disagrees with the source — re-run without --check to regenerate\n", docPath)
		os.Exit(1)
	}
	if updated == string(raw) {
		fmt.Printf("commands-census: %s already up to date (%d verbs)\n", docPath, len(verbs))
		return
	}
	if err := os.WriteFile(docPath, []byte(updated), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "commands-census: write %s: %v\n", docPath, err)
		os.Exit(1)
	}
	fmt.Printf("commands-census: wrote %s (%d verbs)\n", docPath, len(verbs))
}
