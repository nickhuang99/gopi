// Command mancensus regenerates the GENERATED blocks inside the man sources
// under man/ from the `-h` pages the built binary prints — the "same source"
// half of epic "man page 方案 A" (operator decision, 2026-10-11).
//
// RUN (rewrites man/*.md in place):
//
//	make man                       # build + regenerate + render the .1 pages
//	go run internal/standards/testdata/mancensus.go . bin/gopi
//
// RE-DERIVE WITHOUT WRITING (exits 1 when a source is stale):
//
//	go run internal/standards/testdata/mancensus.go . bin/gopi --check
//
// It lives under testdata/ because it is a MAIN package and the tree keeps
// exactly one built artifact (Rule 35, Makefile's "ONE BINARY" note): the Go
// tool ignores testdata directories, so this program is never compiled by
// `go build ./...`. The precedent is commands-census.go beside it — and the
// same shape (a census program that names how a figure is re-derived, Rule 38).
//
// WHAT IT WRITES, AND WHAT IT DELIBERATELY DOES NOT. Only the two generated
// regions a man source carries — the verb index (the authoritative verb list)
// and the SYNOPSIS block (the verb's own usage rows, byte for byte as `-h`
// prints them). The page's PROSE is a human's: it is written and reviewed like
// any other document, and this program never touches a byte of it. The markers
// it rewrites between are defined once in internal/mansource — the same package
// the drift gate (cmd/agent/man_source_gate_test.go) reads the file through, so
// the writer and the refuser cannot disagree about where a region is.
//
// THE FACT SOURCE IS THE BINARY, NEVER A SOURCE FILE. The synopsis rows are
// read from `gopi <verb> -h` — the surface an operator sees (Rule 31) — not
// from the usage constant in cmd/agent, which is a different thing that merely
// ought to agree. That choice is what makes the gate meaningful: the man source
// and the help text are tied to the same one artifact.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/mansource"
)

// usage is the argv contract, printed on misuse (Rule 15: a refusal names the
// way to run it).
const usage = "usage: mancensus <repo-root> [<gopi-binary>] [--check]"

// page is one man source and the `-h` page it is derived from. Verb == "" is
// the top-level page (`gopi -h`), which additionally carries the verb index.
type page struct {
	path string
	verb string
	// index marks the page that carries the generated verb index.
	index bool
}

// pages is the 12-page plan (step 2 of three): the top-level page plus one page
// per published verb, which is the whole published verb set — so this list and
// `gopi config completion zsh`'s command list cannot drift without a page
// failing to render. Adding a page here is the ONE place a new man source is
// registered for generation.
var pages = []page{
	{path: "man/gopi.md", index: true},
	{path: "man/gopi-chat.md", verb: "chat"},
	{path: "man/gopi-config.md", verb: "config"},
	{path: "man/gopi-memory.md", verb: "memory"},
	// `man/gopi-merge.md` LEFT THIS SET with the verb it documents (epic
	// merge-removal, item 2) — the SAME removal the gate's own list records in
	// cmd/agent/man_source_gate_test.go, and the two lists must agree or this
	// generator exits 2 running `gopi merge -h` against a verb that refuses
	// (measured: exit status 1, the retirement sentence). The FILE stays in the
	// tree: this slice removes the command surface, not the merge engine.
	{path: "man/gopi-notify.md", verb: "notify"},
	{path: "man/gopi-screen.md", verb: "screen"},
	{path: "man/gopi-session.md", verb: "session"},
	{path: "man/gopi-task.md", verb: "task"},
	{path: "man/gopi-tool.md", verb: "tool"},
	{path: "man/gopi-ui_state.md", verb: "ui_state"},
	{path: "man/gopi-version.md", verb: "version"},
	{path: "man/gopi-web.md", verb: "web"},
	{path: "man/gopi-worker.md", verb: "worker"},
}

func main() {
	args := os.Args[1:]
	check := false
	var pos []string
	for _, a := range args {
		if a == "--check" {
			check = true
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) < 1 || len(pos) > 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	root := pos[0]
	bin := filepath.Join(root, "bin", "gopi")
	if len(pos) == 2 {
		bin = pos[1]
	}
	if _, err := os.Stat(bin); err != nil {
		fmt.Fprintf(os.Stderr, "mancensus: %s: %v\nrun `make build` first (the help pages are read from the built binary, never from a source file)\n", bin, err)
		os.Exit(2)
	}

	stale := 0
	for _, p := range pages {
		help, err := helpText(bin, p.verb)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mancensus: %v\n", err)
			os.Exit(2)
		}
		docPath := filepath.Join(root, p.path)
		doc, err := os.ReadFile(docPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mancensus: %v\n", err)
			os.Exit(2)
		}
		updated, err := regenerate(string(doc), help, p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mancensus: %s: %v\n", p.path, err)
			os.Exit(2)
		}
		if updated == string(doc) {
			fmt.Printf("mancensus: %s — %s\n", p.path, mansource.UpToDateToken)
			continue
		}
		stale++
		if check {
			fmt.Printf("mancensus: %s — %s (regenerate with `%s`)\n", p.path, mansource.StaleToken, mansource.RegenerateHint)
			continue
		}
		if err := os.WriteFile(docPath, []byte(updated), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "mancensus: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("mancensus: %s — regenerated\n", p.path)
	}
	if check && stale > 0 {
		os.Exit(1)
	}
}

// regenerate swaps both generated regions of one document.
func regenerate(doc, help string, p page) (string, error) {
	out, err := mansource.ReplaceBlock(doc, mansource.SynopsisBegin, mansource.SynopsisEnd, synopsisBody(help))
	if err != nil {
		return "", err
	}
	if !p.index {
		return out, nil
	}
	return mansource.ReplaceBlock(out, mansource.IndexBegin, mansource.IndexEnd, indexBody(help))
}

// synopsisBody is the verb's usage rows as `-h` prints them — the literal block
// a reader compares against the help page, wrapping and all.
func synopsisBody(help string) string {
	rows := mansource.UsageRows(help)
	if len(rows) == 0 {
		// A page with no usage block is a fact about that page, not a licence
		// to write an empty region: the marker pair would then be a comment
		// nobody can check. Fail loudly, naming the page (Rule 15).
		return "@@ NO USAGE BLOCK @@\n"
	}
	return "\n" + "```\n" + strings.Join(rows, "\n") + "\n```\n\n"
}

// indexBody is the authoritative top-level verb list, one `gopi <verb>` row per
// line, as the top-level help page itself lists them. The reader is
// mansource.HelpVerbs — the same function the drift gate compares through.
func indexBody(help string) string {
	verbs := mansource.HelpVerbs(help)
	var b strings.Builder
	b.WriteString("\n")
	for _, v := range verbs {
		fmt.Fprintf(&b, "- `gopi %s`\n", v)
	}
	b.WriteString("\n")
	return b.String()
}

// helpText runs `<bin> [<verb>] -h` and returns stdout+stderr. A page that does
// not print is an error with the child's own output attached, never a silent
// empty string (Rule 15).
func helpText(bin, verb string) (string, error) {
	args := []string{}
	if verb != "" {
		args = append(args, verb)
	}
	args = append(args, "-h")
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("`%s %s` exited: %w (output: %s)", bin, strings.Join(args, " "), err, out)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return "", fmt.Errorf("`%s %s` printed nothing", bin, strings.Join(args, " "))
	}
	return string(out), nil
}
