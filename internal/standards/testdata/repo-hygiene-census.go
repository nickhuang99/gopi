// Command repo-hygiene-census measures the tracked-blob hygiene violations of
// the repository it is pointed at and (re)writes the gate's freeze table
// (GOPI.md Rule 39).
//
// It lives under testdata/ because it is a MAIN package: the Go tool ignores
// testdata directories, so this file is never compiled by `go build ./...` or
// `go test ./internal/standards/`, while a reader can still run it verbatim.
// Nothing in the build executes it — the Go gates read the TABLE, never this
// program (the same shape devdoc/DOC-INDEX.tsv has with tools/docclass).
//
// RUN (writes the table):
//
//	go run internal/standards/testdata/repo-hygiene-census.go . \
//	    internal/standards/testdata/repo-hygiene-baseline.tsv
//
// RE-DERIVE WITHOUT WRITING (Rule 38: a census figure names a re-runnable
// generator; this prints the delta and exits 1 when the table and the tree
// disagree):
//
//	go run internal/standards/testdata/repo-hygiene-census.go . \
//	    internal/standards/testdata/repo-hygiene-baseline.tsv --check
//
// The scan is `git ls-files` (the index) — never a worktree walk, so an
// untracked 135 MB residue is not this rule's target (it is Rule 30's).
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// The thresholds are DUPLICATED from the gate on purpose and kept in sync by
// the census's own `--check` mode: the census cannot import the gate (it is a
// main package) and the gate cannot compile this file. If either threshold
// moves without the other, --check reports the delta as a class change.
const (
	oversizeLimitBytes = 256 * 1024
	fontLimitBytes     = 1 << 20
	nulProbeBytes      = 8192
)

// sourceExtensions / textOfRecordExtensions are the gate's own two lists, and
// they are MIRRORED here for the same reason the thresholds are: this is a main
// package under testdata/, so it cannot import internal/standards, and that
// package cannot compile this file. `--check` is the sync mechanism — a list
// that drifts makes the table disagree with the tree and the check exits 1,
// with the differing rows printed (Rule 38: the table is re-derived, never
// hand-edited).
var sourceExtensions = []string{
	".go", ".c", ".h", ".cc", ".cpp", ".hpp", ".rs", ".py",
	".js", ".ts", ".tsx", ".jsx", ".java", ".rb", ".sh", ".bash",
	".zsh", ".pl", ".lua", ".sql", ".proto", ".css",
}

var textOfRecordExtensions = []string{".md", ".tsv"}

// isSourceOrTextOfRecord reports whether the extension puts the path outside
// Rule 39's SIZE arm ("not source or text of record"). The magic and NUL arms
// deliberately do not consult it.
func isSourceOrTextOfRecord(rel string) bool {
	ext := strings.ToLower(filepath.Ext(rel))
	for _, want := range sourceExtensions {
		if ext == want {
			return true
		}
	}
	for _, want := range textOfRecordExtensions {
		if ext == want {
			return true
		}
	}
	return false
}

var magics = []struct {
	name string
	head []byte
}{
	{"PNG", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}},
	{"GIF", []byte{'G', 'I', 'F', '8'}},
	{"JPEG", []byte{0xFF, 0xD8, 0xFF}},
	{"zip", []byte{'P', 'K', 0x03, 0x04}},
	{"sqlite", []byte("SQLite f")},
	{"ELF", []byte{0x7F, 'E', 'L', 'F'}},
}

var fontExtensions = []string{".ttf", ".otf", ".ttc", ".woff", ".woff2", ".eot"}

// violation is one row of the freeze table.
type violation struct {
	path   string
	class  string
	reason string
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: repo-hygiene-census <repo-root> <baseline.tsv> [--check]")
		os.Exit(2)
	}
	root, out := os.Args[1], os.Args[2]
	check := len(os.Args) > 3 && os.Args[3] == "--check"
	rows, err := census(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "census:", err)
		os.Exit(2)
	}
	rendered := render(rows)
	if check {
		onDisk, err := os.ReadFile(out)
		if err != nil {
			fmt.Fprintln(os.Stderr, "census --check:", err)
			os.Exit(2)
		}
		if !bytes.Equal(onDisk, []byte(rendered)) {
			fmt.Fprintf(os.Stderr, "census --check: %s does NOT match the tracked tree\n", out)
			reportDelta(string(onDisk), rendered)
			os.Exit(1)
		}
		fmt.Printf("census --check: %s matches the tracked tree (%d rows)\n", out, len(rows))
		return
	}
	if err := os.WriteFile(out, []byte(rendered), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(2)
	}
	fmt.Printf("wrote %s: %d rows\n", out, len(rows))
	byClass := map[string]int{}
	for _, r := range rows {
		byClass[r.class]++
	}
	classes := make([]string, 0, len(byClass))
	for c := range byClass {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	for _, c := range classes {
		fmt.Printf("  %-9s %d\n", c, byClass[c])
	}
}

// reportDelta names the first rows that differ, so a --check failure is
// diagnosable without re-running anything by hand.
func reportDelta(want, got string) {
	wantRows := rowSet(want)
	gotRows := rowSet(got)
	var paths []string
	for p := range wantRows {
		paths = append(paths, p)
	}
	for p := range gotRows {
		if _, ok := wantRows[p]; !ok {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	shown := 0
	for _, p := range paths {
		w, okW := wantRows[p]
		g, okG := gotRows[p]
		if okW && okG && w == g {
			continue
		}
		fmt.Fprintf(os.Stderr, "  -table %s\n  +tree  %s\n", orAbsent(w, okW), orAbsent(g, okG))
		shown++
		if shown >= 10 {
			fmt.Fprintln(os.Stderr, "  … (first 10 shown)")
			break
		}
	}
}

func orAbsent(line string, ok bool) string {
	if !ok {
		return "(no row)"
	}
	return line
}

func rowSet(text string) map[string]string {
	rows := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := line[:strings.IndexByte(line, '\t')]
		rows[p] = line
	}
	return rows
}

// census measures every tracked blob and returns the frozen rows.
func census(root string) ([]violation, error) {
	raw, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files in %s: %w", root, err)
	}
	fields := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	var rows []violation
	for _, p := range fields {
		if p == "" {
			continue
		}
		classes, err := classesOf(root, p)
		if err != nil {
			return nil, err
		}
		for _, class := range classes {
			rows = append(rows, violation{path: p, class: class, reason: reasonFor(p, class)})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].path < rows[j].path })
	return rows, nil
}

// classesOf returns the classes one tracked path violates, in the gate's own
// order (oversize → font → magic → nul).
func classesOf(root, rel string) ([]string, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	fi, err := os.Lstat(full)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, nil
	}
	size := fi.Size()
	var classes []string
	// Rule 39's size arm refuses a file above 256 KiB that is "not source or text
	// of record" — so the exemption is part of the PREDICATE, not of the table:
	// a .go / .md file is never oversize whatever its size, and the magic/NUL
	// arms below still judge its content.
	if size > oversizeLimitBytes && !isSourceOrTextOfRecord(rel) {
		classes = append(classes, "oversize")
	}
	ext := strings.ToLower(filepath.Ext(rel))
	for _, want := range fontExtensions {
		if ext == want && size > fontLimitBytes {
			classes = append(classes, "font")
			break
		}
	}
	if size == 0 {
		return classes, nil
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	buf := make([]byte, nulProbeBytes)
	n, rerr := ioReadFull(f, buf)
	f.Close()
	if rerr != nil {
		return nil, fmt.Errorf("read %s: %w", rel, rerr)
	}
	head := buf[:n]
	for _, m := range magics {
		if len(head) >= len(m.head) && bytes.Equal(head[:len(m.head)], m.head) {
			classes = append(classes, "magic")
			return classes, nil
		}
	}
	if bytes.IndexByte(head, 0) >= 0 {
		classes = append(classes, "nul")
	}
	return classes, nil
}

// reasonFor is the reviewed reason carried into the table. It is written here,
// once, and copied into the table by the run — the table is DATA, never
// hand-edited (the precedent devdoc/DOC-INDEX.tsv sets).
func reasonFor(p, class string) string {
	top := p
	if i := strings.IndexByte(p, '/'); i >= 0 {
		top = p[:i]
	}
	ext := strings.ToLower(filepath.Ext(p))
	switch {
	case p == "docs/quickstart.gif":
		return "the README's own four-frame quickstart GIF: README.md embeds it by path, so it is the first thing a visitor sees, and it IS regenerable — docs/quickstart.md carries the exact gopi tool gif invocation that re-renders it from four real command runs. It is frozen here rather than removed because the recipe and the artefact are one deliverable; a future clean-up must re-render it, not delete the embed"
	case top == "sandbox":
		return "a committed sandbox/ tree holding copied .gopi state and a compiled binary: sandboxes are execution environments (Rule 34), never tracked artefacts; removal owned by epic 01a0fd2e scope 3"
	case class == "font":
		return "a font over the font limit — the source or the fetch URL is the artefact, not the built face; removal owned by epic 01a0fd2e scope 3"
	case p == "internal/tokenizer/tokenizer.json":
		return "the DeepSeek-V3 vocabulary itself, embedded by go:embed so a fresh checkout counts tokens with no download; the artefact IS the published data (no regenerator exists), the operator ruled on it explicitly for task 01a198b3-9d7b-7215-bd6e-142ae3a751ff, and the binary grows by exactly its 7,847,602 bytes"
	case class == "oversize" && (ext == ".txt" || ext == ".log"):
		return "a recorded run log/transcript: regenerable, and removal is owned by epic 01a0fd2e scope 3, not by this gate"
	case class == "oversize" && (ext == ".raw" || ext == ".pcm" || ext == ".wav"):
		return "recorded capture/analysis output: regenerable, and removal is owned by epic 01a0fd2e scope 3, not by this gate"
	case class == "oversize":
		return "a demo/evidence asset (screenshot, replay film) regenerated by its recording script; removal owned by epic 01a0fd2e scope 3, not by this gate"
	case class == "magic":
		return "a tracked blob whose first bytes carry a binary magic (PNG/GIF/JPEG/zip/sqlite/ELF): not source, not diffable; removal owned by epic 01a0fd2e scope 3, not by this gate"
	case class == "nul":
		return "a tracked blob carrying NUL bytes in its first 8 KiB (a compressed capture or a raw audio/voice sample): not source, not diffable; removal owned by epic 01a0fd2e scope 3, not by this gate"
	}
	return "frozen at the gate's landing — reason not classified"
}

// render writes the table with its header. The header is part of the file's
// contract: the gate skips '#' lines, and the ratchet sentence is what a reader
// who is about to add a row needs to have read.
func render(rows []violation) string {
	var b strings.Builder
	b.WriteString("# repo-hygiene baseline (GOPI.md Rule 39) — frozen violations of the tracked-tree hygiene gate.\n")
	b.WriteString("# columns: path<TAB>class<TAB>reason. Ratchet DOWN only: an entry whose file is gone\n")
	b.WriteString("# or no longer violates is an ERROR, and the gate refuses a class outside {oversize,font,magic,nul}.\n")
	b.WriteString("# NEVER hand-edit: regenerate with\n")
	b.WriteString("#   go run internal/standards/testdata/repo-hygiene-census.go . internal/standards/testdata/repo-hygiene-baseline.tsv\n")
	b.WriteString("# and re-derive (exit 1 on any delta) by appending --check (GOPI.md Rule 38).\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", r.path, r.class, r.reason)
	}
	return b.String()
}

// ioReadFull reads up to len(buf) bytes; a SHORT read is not an error here (a
// 3-byte file still matches the 3-byte GIF magic), so EOF is folded into the
// count and only a real read failure propagates.
func ioReadFull(f *os.File, buf []byte) (int, error) {
	n, err := io.ReadFull(f, buf)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		return n, nil
	}
	return n, err
}
