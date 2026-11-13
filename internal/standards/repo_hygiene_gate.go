package standardgate

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// tracked-blob hygiene gate — the "git 存代码不存 binary" rule (PROJECT.md Rule 39)
//
// Rule 5's sibling, one axis over. `scratch_gate.go` refuses junk PATHS (a test
// binary at the root, a committed sandbox tree); this gate refuses junk
// CONTENT at any path — a blob that is large, or whose first bytes are a binary
// magic, is not a tracked artefact whatever directory it sits in.
//
// The measured state this gate was written against (master @ 83dac97c, 7,318
// tracked paths, 284.6 MB of blob content): 198 paths over 256 KiB, 753 paths
// whose first bytes carry a recognised binary magic, and 56 more whose first
// 8 KiB carry a NUL — 822 distinct paths, frozen in
// testdata/repo-hygiene-baseline.tsv. This file delivers the RULE and the
// RATCHET; removing those blobs is the epic's own later scope.
//
// The scan is `git ls-files` for the same reason scratch_gate's is: the defect
// is "this is TRACKED", and the index is the only thing that answers it — a
// worktree walk would report the untracked 135 MB residue of 2026-09-26
// (devdoc/scratch-repo-residue-01a0db54.md) as if it were this rule's target.
// ---------------------------------------------------------------------------

// gitLsFilesNulArgv lists every tracked path NUL-separated, so a path holding a
// space or a newline cannot be split into two phantom entries. Named for the
// same Rule 1b reason its sibling in scratch_gate.go is.
var gitLsFilesNulArgv = []string{"ls-files", "-z"}

const (
	// oversizeLimitBytes is the ordinary blob ceiling, and it applies to BLOBS
	// only: Rule 39's own sentence refuses "a tracked file above 256 KiB that is
	// **not source or text of record**", so a source file or a transcribed
	// record is legitimate at any size and the size arm does not judge it
	// (isSourceOrTextOfRecord). A binary wearing a source file's extension is
	// still caught — the magic and NUL arms do not consult the extension.
	oversizeLimitBytes = 256 * 1024
	// fontLimitBytes is the separate, looser ceiling for a FONT: a built face
	// is legitimately bigger than source, so it is not swept up by the ordinary
	// limit — but a megabyte-scale binary face still has no business in the
	// tracked tree (the source, or the URL it is fetched from, is the artefact).
	fontLimitBytes = 1 << 20
	// nulProbeBytes is how far into a blob the NUL check reads. A recognised
	// magic is matched in the first 8 bytes; the NUL arm exists for the blobs
	// no magic names (a .gz capture, a .wav/.pcm voice sample) and looks wide
	// enough to see the container header, never the whole file.
	nulProbeBytes = 8192
	// maxMagicLenBytes / magicProbeBytes bound the magic comparison: a magic
	// longer than the probe would panic on the slice, so the two are asserted
	// against each other rather than trusted.
	maxMagicLenBytes = 8
	// basenameProbeChars caps how much of a basename a violation line renders,
	// so a long path cannot push the report past a readable width.
	basenameProbeChars = 120
)

// binaryMagic is one recognised file signature. name is the human word the
// report prints (the rule, not the fact); head is the byte prefix itself.
type binaryMagic struct {
	name string
	head []byte
}

// binaryMagics is the reviewed signature list. It is a LIST and not a heuristic
// on purpose: every entry was chosen because the epic's incident names that
// format, and adding one is a reviewed edit here (Rule 1b/13).
var binaryMagics = []binaryMagic{
	{"PNG", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}},
	{"GIF", []byte{'G', 'I', 'F', '8'}},
	{"JPEG", []byte{0xFF, 0xD8, 0xFF}},
	{"zip", []byte{'P', 'K', 0x03, 0x04}},
	{"sqlite", []byte("SQLite f")},
	{"ELF", []byte{0x7F, 'E', 'L', 'F'}},
}

// fontExtensions are the extensions the font arm keys on. An extension list is
// the right shape here (unlike the magic list): the limit is a PER-FORMAT
// policy, and only a font is allowed the looser ceiling.
var fontExtensions = []string{
	".ttf", ".otf", ".ttc", ".woff", ".woff2", ".eot",
}

// sourceExtensions and textOfRecordExtensions are the two halves of Rule 39's
// own exception: the size arm refuses a file above the ceiling that is "not
// source or text of record", so a file whose extension names SOURCE (code a
// reviewer reads as code) or a transcribed RECORD (a document that would lose
// the run it records if regenerated) is never oversize.
//
// This is a fixed list and not a heuristic, for the same reason binaryMagics is:
// adding an extension is a reviewed edit here (Rule 1b/13). It is deliberately
// NARROW — a `.txt`/`.log` capture is a transcript of one run (regenerable, and
// owned by the removal epic), a `.json` vocabulary blob is data, and neither is
// exempted.
//
// THE EXEMPTION NARROWS EXACTLY ONE ARM. A 300 KiB file named `x.go` whose first
// bytes are the PNG or ELF signature is still reported (classMagic), and one
// carrying a NUL in its first 8 KiB is still reported (classNul): the extension
// is a claim about the CONTENT, and the magic/NUL arms test the content itself
// (Rule 39's own words: "a blob regardless of its name").
var sourceExtensions = []string{
	".go", ".c", ".h", ".cc", ".cpp", ".hpp", ".rs", ".py",
	".js", ".ts", ".tsx", ".jsx", ".java", ".rb", ".sh", ".bash",
	".zsh", ".pl", ".lua", ".sql", ".proto", ".css",
}

// textOfRecordExtensions are the document formats this repository keeps AS the
// record (a transcribed evidence page, a census table): regenerating one would
// destroy the run it documents, which is the opposite of a regenerable artefact.
var textOfRecordExtensions = []string{".md", ".tsv"}

// isSourceOrTextOfRecord reports whether the path's extension puts it outside
// Rule 39's size arm. The census generator carries the SAME two lists (it is a
// main package under testdata/, which cannot import this one) and the census's
// --check mode is what keeps them honest: a list that drifts makes the table
// disagree with the tree and the check exits 1.
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

// Violation classes: the vocabulary of the baseline's `class` column. A class
// outside this set is a baseline error, never silently skipped — a typo'd class
// would otherwise freeze a file the gate no longer understands.
const (
	classOversize = "oversize"
	classFont     = "font"
	classMagic    = "magic"
	classNul      = "nul"
)

// knownClasses is the complete set, used to reject an unknown baseline class.
var knownClasses = map[string]bool{
	classOversize: true,
	classFont:     true,
	classMagic:    true,
	classNul:      true,
}

// ---------------------------------------------------------------------------
// the baseline table — the ratchet
// ---------------------------------------------------------------------------

// repoHygieneBaselineRel is where the frozen violations live, relative to the
// module root (Rule 14: one definition, referenced by the gate, the test and
// the report). The census that produced it is `repo-hygiene-census.go`, kept
// beside this file so the figures can be re-derived rather than believed
// (PROJECT.md Rule 38).
const repoHygieneBaselineRel = "internal/standards/testdata/repo-hygiene-baseline.tsv"

// repoHygieneCensusRel is the census generator for the table above. It is not
// compiled by the build (its filename does not end in `_test.go`, so `go test`
// ignores it and `go build ./...` never sees it either — nothing imports this
// package as a program), and it exists so a later reader can re-measure instead
// of trusting this file's numbers.
//
// The value names the file where it actually lives: `testdata/`, beside the
// table it writes. It used to read `internal/standards/repo-hygiene-census.go`
// — a path this tree has never had — and nothing caught it because no code
// referenced the constant; the arm hardcoded its own (correct) path. The arm now
// reads THIS constant, so the two cannot drift again.
const repoHygieneCensusRel = "internal/standards/testdata/repo-hygiene-census.go"

// baselineKey is what a permit is keyed on: the PATH **and** the class. It is
// deliberately not the path alone — one blob can violate several classes at
// once (fonts/face.ttf was frozen as font + oversize + nul), and a path-keyed
// table could hold only one of them, so the other two would be re-reported as
// new on every run.
type baselineKey struct {
	path  string
	class string
}

// baselineRow is one frozen violation: the reviewed reason the (path, class)
// pair is allowed to stay.
type baselineRow struct {
	reason string
}

// blobViolation is one hit on a tracked blob — the path, the class, and the
// sentence the report prints (which, for a baselined path, is its frozen
// reason).
type blobViolation struct {
	path   string
	class  string
	detail string
}

// ---------------------------------------------------------------------------
// the scan
// ---------------------------------------------------------------------------

// hygieneViolations applies every blob class to one tracked path and returns
// one violation per class that fired, in a stable order (oversize → font →
// magic → nul). It is PURE apart from reading the file, so the red proof can
// feed it a fixture tree instead of poisoning the real index — the same split
// scratch_gate.go uses for junkTrackedPaths.
//
// A path that cannot be read is a violation of its own: an unreadable tracked
// file is not "clean", it is unknown, and Rule 15 does not let a scan call
// unknown a pass.
func hygieneViolations(root, rel string) []blobViolation {
	full := filepath.Join(root, filepath.FromSlash(rel))
	fi, err := os.Lstat(full)
	if err != nil {
		return []blobViolation{{rel, classOversize,
			fmt.Sprintf("tracked path cannot be read (%v) — the gate does not score an unreadable blob as clean", err)}}
	}
	if !fi.Mode().IsRegular() {
		return nil // a symlink or a gitlink is not a blob; its target is measured where the target is tracked.
	}
	size := fi.Size()
	var out []blobViolation
	// The size arm judges BLOBS. Source and text of record are legitimate at any
	// size — Rule 39's own sentence says so — and the magic/NUL arms below still
	// test their CONTENT, so a binary named `.go` is caught by the class it
	// actually is rather than let through by its extension.
	if size > oversizeLimitBytes && !isSourceOrTextOfRecord(rel) {
		out = append(out, blobViolation{rel, classOversize,
			fmt.Sprintf("%d bytes > the %d-byte limit", size, oversizeLimitBytes)})
	}
	if size > fontLimitBytes && hasFontExtension(rel) {
		out = append(out, blobViolation{rel, classFont,
			fmt.Sprintf("%d bytes > the %d-byte font limit", size, fontLimitBytes)})
	}
	if size == 0 {
		return out
	}
	head, err := readHead(full, nulProbeBytes)
	if err != nil {
		out = append(out, blobViolation{rel, classMagic,
			fmt.Sprintf("first bytes cannot be read (%v) — the gate does not score an unreadable blob as clean", err)})
		return out
	}
	if name, ok := matchBinaryMagic(head); ok {
		out = append(out, blobViolation{rel, classMagic,
			fmt.Sprintf("first bytes are the %s signature — a blob, not source", name)})
		return out
	}
	if bytes.IndexByte(head, 0) >= 0 {
		out = append(out, blobViolation{rel, classNul,
			fmt.Sprintf("a NUL byte within the first %d bytes — a blob, not text", nulProbeBytes)})
	}
	return out
}

// matchBinaryMagic reports the first signature the head matches.
func matchBinaryMagic(head []byte) (string, bool) {
	for _, m := range binaryMagics {
		if len(head) >= len(m.head) && bytes.Equal(head[:len(m.head)], m.head) {
			return m.name, true
		}
	}
	return "", false
}

// hasFontExtension reports whether the path's extension is a font format.
func hasFontExtension(rel string) bool {
	ext := strings.ToLower(filepath.Ext(rel))
	for _, want := range fontExtensions {
		if ext == want {
			return true
		}
	}
	return false
}

// readHead reads at most n bytes from the front of path. A short read is not an
// error: a 3-byte file is still matched against the 3-byte magic.
func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	got, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	return buf[:got], nil
}

// trackedPathsIn lists every tracked path in the repository rooted at root. It
// uses `git -C root ls-files -z`, and it FAILS on anything else: a gate that
// degrades to "no paths" reads green on every tree, which is worse than no gate
// (Rule 15 — a scan that found nothing must be distinguishable from one that
// never ran).
func trackedPathsIn(root string) ([]string, error) {
	argv := append([]string{"-C", root}, gitLsFilesNulArgv...)
	out, err := exec.Command(gitBinary, argv...).Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s in %s: %w", gitBinary, strings.Join(gitLsFilesNulArgv, " "), root, err)
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	paths := make([]string, 0, len(fields))
	for _, p := range fields {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s %s listed no paths in %s — the gate cannot read the index and refuses to pass blind",
			gitBinary, strings.Join(gitLsFilesNulArgv, " "), root)
	}
	return paths, nil
}

// scanTrackedBlobs runs the whole scan over root and returns the violations in
// index order (stable, so a report diff is readable).
func scanTrackedBlobs(root string) ([]blobViolation, error) {
	paths, err := trackedPathsIn(root)
	if err != nil {
		return nil, err
	}
	var out []blobViolation
	for _, p := range paths {
		out = append(out, hygieneViolations(root, p)...)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// the baseline
// ---------------------------------------------------------------------------

// loadBaseline parses the frozen-violation table. A missing file is an ERROR,
// not an empty baseline: "no permits" and "no table" would otherwise both read
// as an empty map, and the gate would report 822 violations as if the table had
// been deleted on purpose.
func loadBaseline(path string) (map[baselineKey]baselineRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("baseline %s: %w", path, err)
	}
	defer f.Close()
	rows := map[baselineKey]baselineRow{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("baseline %s:%d: want 3 tab-separated fields (path, class, reason), got %d",
				path, lineNo, len(parts))
		}
		if !knownClasses[parts[1]] {
			return nil, fmt.Errorf("baseline %s:%d: unknown class %q (want one of %s) — a typo'd class would freeze a file the gate no longer understands",
				path, lineNo, parts[1], strings.Join(knownClassNames(), "/"))
		}
		if strings.TrimSpace(parts[2]) == "" {
			return nil, fmt.Errorf("baseline %s:%d: empty reason for %q — a permit without a reason is not a reviewed permit",
				path, lineNo, parts[0])
		}
		key := baselineKey{path: parts[0], class: parts[1]}
		if _, dup := rows[key]; dup {
			return nil, fmt.Errorf("baseline %s:%d: duplicate row for %q [%s]", path, lineNo, parts[0], parts[1])
		}
		rows[key] = baselineRow{reason: parts[2]}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("baseline %s: %w", path, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("baseline %s carries no rows — refusing an empty permit table", path)
	}
	return rows, nil
}

// knownClassNames returns the class vocabulary in a stable order for messages.
func knownClassNames() []string {
	return []string{classOversize, classFont, classMagic, classNul}
}

// classifyViolations splits scan output into the three verdicts the gate
// reports: a baselined hit whose (path, class) pair is frozen, a baselined path
// whose class SET changed (stale/traded — the ratchet's error), and an
// unbaselined hit.
//
// A path's fired classes are compared as a SET against its frozen classes,
// because one blob can violate several classes at once: a path frozen as
// {font, oversize, nul} that now fires {font, oversize} is not clean (its `nul`
// permit went stale), and a path frozen as {magic} that now fires {magic, nul}
// has gained a violation the table does not cover.
func classifyViolations(hits []blobViolation, baseline map[baselineKey]baselineRow) (baselined, traded []blobViolation, stale []string, fresh []blobViolation) {
	frozenByPath := map[string]map[string]bool{}
	for key := range baseline {
		if frozenByPath[key.path] == nil {
			frozenByPath[key.path] = map[string]bool{}
		}
		frozenByPath[key.path][key.class] = true
	}
	firedByPath := map[string]map[string]bool{}
	for _, h := range hits {
		if frozenByPath[h.path] == nil {
			fresh = append(fresh, h)
			continue
		}
		if firedByPath[h.path] == nil {
			firedByPath[h.path] = map[string]bool{}
		}
		firedByPath[h.path][h.class] = true
	}

	for _, h := range hits {
		if frozenByPath[h.path] == nil {
			continue
		}
		if frozenByPath[h.path][h.class] {
			h.detail = baseline[baselineKey{path: h.path, class: h.class}].reason
			baselined = append(baselined, h)
			continue
		}
		h.detail = fmt.Sprintf("frozen class set {%s} but now fires %q (%s)",
			strings.Join(sortedClasses(frozenByPath[h.path]), ","), h.class, h.detail)
		traded = append(traded, h)
	}

	// A permit whose class no longer FIRES at that path is stale — the file was
	// cleaned, deleted, or changed shape, and the row must go so the baseline
	// ratchets DOWN. Sorted, so the report names the first one deterministically.
	for key := range baseline {
		if !firedByPath[key.path][key.class] {
			stale = append(stale, key.path+" ["+key.class+"]")
		}
	}
	sort.Strings(stale)
	return baselined, traded, stale, fresh
}

// sortedClasses renders a class set deterministically for a message.
func sortedClasses(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// hygieneReport renders the failure text the gate prints. Kept separate from
// the test so the red proof asserts on the MESSAGE (the "stale entry is an
// error" behaviour is a sentence, not a count).
func hygieneReport(baselined, traded []blobViolation, stale []string, fresh []blobViolation, baselinePath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "repo hygiene (GOPI.md Rule 39): %d baselined, %d new, %d class-traded, %d stale permit(s) — permits: %s\n",
		len(baselined), len(fresh), len(traded), len(stale), baselinePath)
	if len(fresh) > 0 {
		fmt.Fprintf(&b, "\nNEW tracked blobs (%d) — untrack the file, or add the artefact's source/regenerator and delete the blob; a permit belongs in the baseline ONLY with a reviewed reason:\n", len(fresh))
		for _, h := range fresh {
			fmt.Fprintf(&b, "  %s [%s] %s\n", trimForReport(h.path), h.class, h.detail)
		}
	}
	if len(traded) > 0 {
		fmt.Fprintf(&b, "\nBASELINED PATH VIOLATING AS A DIFFERENT CLASS (%d) — the ratchet compares the class, so trading one violation for another is not clean:\n", len(traded))
		for _, h := range traded {
			fmt.Fprintf(&b, "  %s [%s] %s\n", trimForReport(h.path), h.class, h.detail)
		}
	}
	if len(stale) > 0 {
		fmt.Fprintf(&b, "\nSTALE PERMITS (%d) — that (path, class) no longer fires: the file is gone, cleaned, or changed shape; DELETE the row so the baseline ratchets DOWN (first: %s):\n",
			len(stale), trimForReport(stale[0]))
		for _, p := range stale {
			fmt.Fprintf(&b, "  %s\n", trimForReport(p))
		}
	}
	return b.String()
}

// trimForReport keeps a pathological path from widening the report beyond a
// readable line, and makes the truncation visible (Rule 15: never a silent
// edit of the evidence).
func trimForReport(p string) string {
	if len(p) <= basenameProbeChars {
		return p
	}
	return p[:basenameProbeChars] + "…(" + strconv.Itoa(len(p)) + " chars)"
}
