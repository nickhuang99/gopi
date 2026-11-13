package tools

// manifest_union_gate.go — the MERGE-TIME DUPLICATE-MANIFEST-KEY gate (⓪d):
// the union of two branches is read as a MANIFEST, and a key that appears TWICE
// in what the union would write is refused before it can land.
//
// THE INCIDENT IT CLOSES. devdoc/DOC-INDEX.tsv is an append-mostly table whose
// key is column 1 — the corpus path — and a MERGE is the one writer that can
// duplicate it. Measured on master 79807932 (2026-09-26, task 01a0dd7c): the
// row for devdoc/docindex-write-path-row-ownership.md stood at BOTH line 10 and
// line 762. `git show --stat <merge>` reported `+1 -0` against EACH parent, so
// neither side was wrong on its own: the base blob carried the row ABOVE the
// column header, master's side had moved it BELOW the header, and the feature
// branch had independently classified the SAME document and appended its own
// row at the far end. The two hunks do not overlap, so git's three-way merge
// UNIONED them instead of raising a conflict — and the result is a manifest no
// gate had examined before it landed.
//
// WHY THIS LANE AND NOT THE EXISTING ONES. The union-compile gate (⓪c) builds
// the merge result with `git read-tree -m`, and THAT BUILDER CANNOT SEE THIS
// SHAPE AT ALL: measured on a reproduction of the incident, `read-tree -m`
// leaves the index unmerged and `write-tree` fails, so the ⓪c lane abstains
// (`MERGE_RESULT_UNAVAILABLE`) on exactly the union it would have to inspect —
// the sibling gate's alternative index has no slot for a non-conflicting
// duplicate. The union is therefore materialised here with `git merge-file -p`,
// git plumbing's OWN three-way merge, on the three blobs the merge would use.
// Measured BYTE-IDENTICAL to what `git merge` writes for the same three inputs
// (md5-equal on the reproduction; pinned by the regression test). No index, no
// worktree, no checkout: three `cat-file` reads, one pipe, one scratch
// directory.
//
// WHY NOT THE CLIENT'S PRE-MERGE VERIFY HOOK: that hook merges peer master INTO
// the branch and runs AFTER the commit, so it cannot refuse what is already
// committed; and it runs on the CLIENT, where the server's manifest is not the
// artifact being landed. The merge-TIME arm is the one with VETO: the branch is
// still unlanded, master is untouched, and the refusal happens before any
// mutation.
//
// THE ROW COUNT IS NOT AN ERROR. A manifest legitimately mixes field counts
// (measured: 8 one-field lines / 2436 four-field rows), and "is this a valid
// manifest" is the reader's business, not this gate's. Only a DUPLICATE KEY
// refuses, and it is reported in the standards gate's own shape —
// `<file>:<line>: duplicate row for <path>` — once per line number, so the key
// and BOTH coordinates appear exactly as `internal/standards` prints them.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/devdoc"
	"github.com/nick/gopi/internal/scratch"
)

const (
	// manifestUnionGateEnvName names the operator escape hatch (Rule 2: this is
	// the ONE reader; the constant below is the default).
	manifestUnionGateEnvName = "GOPI_MANIFEST_DUP_GATE"

	// manifestUnionGateDisabledValue turns the gate off — an unforeseen
	// false-positive must always be one env var away from gone.
	manifestUnionGateDisabledValue = "0"

	// ManifestUnionGateName is how the merger log and the refusal brief name
	// this lane (Rule 1b: the spelling lives once, beside the gate).
	ManifestUnionGateName = "MANIFEST-DUP gate"

	// manifestUnionGateScratchPattern names the gate's per-call scratch inside
	// the merge-build runtime class (Rule 22: off the `/` partition, GC-bounded
	// if a killed merger leaves one behind).
	manifestUnionGateScratchPattern = "manifest-dup-gate-"

	// The three blobs a three-way merge needs, in the file names the scratch
	// directory uses. The SIDES are named for the merge this predicts: "side"
	// is the side being LANDED (the branch), "target" is the integration
	// branch — the same orientation `git merge <branch>` uses when run from the
	// integration branch's checkout, which is what makes this a prediction of
	// THAT command rather than of some other merge.
	manifestUnionGateOursBase   = "ours"
	manifestUnionGateOrigBase   = "orig"
	manifestUnionGateTheirsBase = "theirs"
	manifestUnionGateBlobSuffix = ".blob"

	// manifestUnionGateGitBinaryName is the ONE tool this gate needs; a host
	// without git cannot answer the question and ABSTAINS.
	manifestUnionGateGitBinaryName = "git"

	// The git spellings this gate dispatches on (Rule 1b).
	manifestUnionGateCatFileVerb   = "cat-file"
	manifestUnionGateBlobArg       = "blob"
	manifestUnionGateMergeFileVerb = "merge-file"
	manifestUnionGateStdoutFlag    = "-p"
	manifestUnionGateLsTreeVerb    = "ls-tree"
	manifestUnionGateRecursiveFlag = "-r"
	manifestUnionGateNameOnlyFlag  = "--name-only"

	// manifestUnionGateFullTreeFlag makes `ls-tree`'s pathspec resolve against
	// the REPOSITORY ROOT rather than the process's working directory. Without
	// it, `ls-tree <rev> devdoc/DOC-INDEX.tsv` run from a SUBDIRECTORY of the
	// repo answers about `<subdir>/devdoc/DOC-INDEX.tsv` and prints nothing for
	// the manifest that is actually there — measured while developing this
	// gate, and the reason it abstained on a real pair of refs instead of
	// refusing them.
	manifestUnionGateFullTreeFlag = "--full-tree"

	// manifestUnionGateMergeCleanExit is `git merge-file`'s only clean exit: 0
	// means the three inputs merged without conflict markers. A NEGATIVE status
	// counts the conflict markers left in the output (a textual conflict — the
	// resolver lane's case, never this gate's); a POSITIVE one is a real error.
	manifestUnionGateMergeCleanExit = 0

	// manifestUnionGateRefuseStackSep joins the evidence lines of one refusal.
	// The merger's log line and the row's detail field are single-line
	// consumers, so a refusal never smuggles a newline into either.
	manifestUnionGateRefuseStackSep = " | "

	// manifestUnionGateEvidenceLimit caps how many duplicate keys one refusal
	// quotes verbatim. A manifest whose union damaged dozens of keys is a
	// different failure from this one, and the refusal must not turn into a
	// file listing — every key beyond the cap is still COUNTED.
	manifestUnionGateEvidenceLimit = 5

	// manifestUnionGateNoteSep separates the evidence block from the note about
	// which side the union kept for each duplicated key.
	manifestUnionGateNoteSep = " — "
)

// manifestUnionGateSidesIdenticalText is the honest answer when both sides
// carry the SAME text for the duplicated key: content cannot attribute the
// duplicate, and a guess would send a reworker to edit the wrong side.
const manifestUnionGateSidesIdenticalText = "both sides carry identical text for this key"

// manifestUnionGateAbsentBlobMarkers are the two git texts that mean "this path
// is not on that side at all" — a legitimate tree state the gate reports as
// absent rather than as a read failure (Rule 15: the two facts stay distinct).
var manifestUnionGateAbsentBlobMarkers = []string{"does not exist", "Not a valid object name", "exists on disk, but not in"}

// manifestUnionGateVerdict is the gate's decision for one branch.
type manifestUnionGateVerdict struct {
	// Refused is true when the branch must not be merged: the manifest the
	// union would write carries a duplicated row key.
	Refused bool
	// Detail is the refusal reason ("" unless Refused): the gate-shaped
	// evidence lines plus the note naming what each side contributed.
	Detail string
	// Manifest is the path whose union was judged ("" unless Refused).
	Manifest string
	// Duplicates are the duplicated keys, in file order, with both line
	// numbers — the machine-readable half of the refusal.
	Duplicates []manifestUnionKey
}

// manifestUnionKey is one duplicated row key in the manifest the union would
// write.
type manifestUnionKey struct {
	// Path is the row key (manifest column 1).
	Path string
	// FirstLine / SecondLine are the two 1-based line numbers in the union's
	// own text, in ascending order — the SAME coordinates the standards gate
	// reports, so an operator reading either lane sees one set of numbers.
	FirstLine  int
	SecondLine int
	// FirstText / SecondText are the union's OWN lines at those numbers: the
	// bytes that would land, which is what the note quotes.
	FirstText  string
	SecondText string
	// FirstSide / SecondSide name which side's hunk produced each union line
	// ("" when both sides carry that exact line, so nothing can attribute it) —
	// a fact read off the bytes, never a guess.
	FirstSide  string
	SecondSide string
}

// manifestUnionBodyHasLine reports whether body carries line as a whole line.
func manifestUnionBodyHasLine(body, line string) bool {
	if line == "" {
		return false
	}
	for _, l := range strings.Split(body, "\n") {
		if l == line {
			return true
		}
	}
	return false
}

// manifestUnionLineSide attributes ONE union line to the side whose hunk
// produced it: the line is looked up in each side's own blob, and a line BOTH
// sides carry is deliberately attributed to neither ("" — identical text cannot
// say which side added the second copy, and the note says so instead of picking
// one).
func manifestUnionLineSide(line, branchSrc, targetSrc string) string {
	inBranch := manifestUnionBodyHasLine(branchSrc, line)
	inTarget := manifestUnionBodyHasLine(targetSrc, line)
	switch {
	case inBranch && inTarget:
		return ""
	case inBranch:
		return manifestUnionSideBranchName
	case inTarget:
		return manifestUnionSideTargetName
	}
	return ""
}

// manifestUnionGate predicts the manifest the union would write and refuses a
// duplicated row key.
//
// ABSTAIN IS THE DEFAULT, exactly as the sibling gates' (the merger's
// DETECT-never-block contract): a missing git, an unresolvable merge base, no
// declared manifest on both sides, an unreadable blob, a `git merge-file` that
// leaves conflict markers, or a union that does not parse as a manifest.
// Exactly ONE shape refuses: the cleanly-merged manifest carries the same row
// key twice.
func manifestUnionGate(repo, master, branch string) manifestUnionGateVerdict {
	if manifestUnionGateDisabled() {
		return manifestUnionGateVerdict{}
	}
	// A branch the target already CONTAINS adds nothing: the union is master
	// itself, which the corpus gate reads on every merge anyway. One ancestry
	// read replaces three blob reads and keeps the verdict wording truthful
	// about what was measured.
	if mergeBuildGateBranchAlreadyInTarget(repo, branch, master) {
		manifestUnionGateLog("ABSTAINS for %s (%s already contains %s — nothing to union) — merging unchanged", branch, master, branch)
		return manifestUnionGateVerdict{}
	}
	if _, err := exec.LookPath(manifestUnionGateGitBinaryName); err != nil {
		manifestUnionGateLog("ABSTAINS for %s (%s is unavailable: %v) — merging unchanged", branch, manifestUnionGateGitBinaryName, err)
		return manifestUnionGateVerdict{}
	}
	base, err := repoSweepGit(repo, mergeBuildGateMergeBaseVerb, master, branch)
	if err != nil || strings.TrimSpace(base) == "" {
		manifestUnionGateLog("ABSTAINS for %s (merge-base with %s unresolved: %s) — merging unchanged",
			branch, master, errTextOr(err, "no merge base"))
		return manifestUnionGateVerdict{}
	}
	// WHICH FILE: the manifest the SIDES declare, taken from the reader that
	// defines what a manifest is (devdoc.Path) — never spelled here, or the
	// gate would be a second definition of the corpus's shape (Rule 14).
	// A repo that declares no manifest on both sides abstains: there is no key
	// column to duplicate.
	declared, okPath, pathErr := manifestUnionGateDeclaredManifest(repo, base, branch, master)
	if pathErr != nil {
		// An unreadable tree is a measurement the gate could not make — never a
		// refusal, and never silently folded into "no manifest" (Rule 15: the
		// two facts route an operator to different places).
		manifestUnionGateLog("ABSTAINS for %s (listing %s: %v) — merging unchanged", branch, devdoc.Path, pathErr)
		return manifestUnionGateVerdict{}
	}
	if !okPath {
		manifestUnionGateLog("ABSTAINS for %s (no manifest at %s on all three sides) — merging unchanged", branch, devdoc.Path)
		return manifestUnionGateVerdict{}
	}
	workRoot, cleanup, okScratch := manifestUnionGateScratch()
	if !okScratch {
		manifestUnionGateLog("ABSTAINS for %s (no usable scratch directory) — merging unchanged", branch)
		return manifestUnionGateVerdict{}
	}
	defer cleanup()

	branchSrc, err := manifestUnionGateBlob(repo, workRoot, manifestUnionGateOursBase, branch, declared)
	if err != nil {
		manifestUnionGateLog("ABSTAINS for %s (%v) — merging unchanged", branch, err)
		return manifestUnionGateVerdict{}
	}
	baseSrc, err := manifestUnionGateBlob(repo, workRoot, manifestUnionGateOrigBase, base, declared)
	if err != nil {
		manifestUnionGateLog("ABSTAINS for %s (%v) — merging unchanged", branch, err)
		return manifestUnionGateVerdict{}
	}
	targetSrc, err := manifestUnionGateBlob(repo, workRoot, manifestUnionGateTheirsBase, master, declared)
	if err != nil {
		manifestUnionGateLog("ABSTAINS for %s (%v) — merging unchanged", branch, err)
		return manifestUnionGateVerdict{}
	}
	// A manifest absent on ANY side is not this gate's case: it merges by
	// taking the side that carries it (nothing to union, nothing to duplicate),
	// and a deletion on one side is a modify/delete conflict the resolver lane
	// owns. The empty read is the encoder of "absent", and it is checked here
	// rather than merged as an empty file, which would fabricate a union
	// nobody would get.
	if branchSrc == "" || baseSrc == "" || targetSrc == "" {
		manifestUnionGateLog("ABSTAINS for %s (%s is not present on all three sides of the union) — merging unchanged",
			branch, declared)
		return manifestUnionGateVerdict{}
	}
	union, err := manifestUnionGateMergeFile(workRoot)
	if err != nil {
		manifestUnionGateLog("ABSTAINS for %s (%v) — merging unchanged", branch, err)
		return manifestUnionGateVerdict{}
	}
	rows, err := devdoc.ParseManifestLines([]byte(union))
	if err != nil {
		// The union is not a readable manifest (conflict markers in the middle
		// of the table, a truncated cell, an unknown class). That is the
		// resolver lane's or the author's case — ABSTAIN, never refuse on a
		// shape this gate failed to parse.
		manifestUnionGateLog("ABSTAINS for %s (%s: the union is not a readable manifest: %v) — merging unchanged",
			branch, declared, err)
		return manifestUnionGateVerdict{}
	}
	keys := manifestUnionGateDuplicates(rows, union, branchSrc, targetSrc)
	if len(keys) == 0 {
		manifestUnionGateLog("PASS for %s (the union of %s into %s writes every %s row key once)", branch, branch, master, declared)
		return manifestUnionGateVerdict{}
	}
	return manifestUnionGateVerdict{
		Refused:    true,
		Detail:     manifestUnionGateDetail(declared, keys),
		Manifest:   declared,
		Duplicates: keys,
	}
}

// manifestUnionGateDeclaredManifest answers whether the manifest the reader
// declares exists on ALL THREE revisions of the union, and returns its path.
// The question is asked of git's own trees (`ls-tree`), never of the working
// tree, which holds whatever the operator happens to have checked out.
func manifestUnionGateDeclaredManifest(repo, base, branch, master string) (string, bool, error) {
	for _, rev := range []string{base, branch, master} {
		out, err := repoSweepGit(repo, manifestUnionGateLsTreeVerb, manifestUnionGateFullTreeFlag,
			manifestUnionGateRecursiveFlag, manifestUnionGateNameOnlyFlag, rev, devdoc.Path)
		if err != nil {
			return "", false, fmt.Errorf("list %s: %w", rev, err)
		}
		found := false
		for _, line := range strings.Split(out, "\n") {
			// The listing is TREE-relative once --full-tree is in force, so the
			// declared path is what a match looks like from any working
			// directory.
			if name := strings.TrimSpace(line); name == devdoc.Path || strings.HasSuffix(name, "/"+devdoc.Path) {
				found = true
				break
			}
		}
		if !found {
			return "", false, nil
		}
	}
	return devdoc.Path, true, nil
}

// manifestUnionGateScratch prepares the gate's per-call directory and its
// cleanup. It creates NO worktree and NO ref, so there is nothing of this
// gate's to reap beyond the directory.
func manifestUnionGateScratch() (workRoot string, cleanup func(), ok bool) {
	classDir := scratch.RuntimeScratchDir(scratch.RuntimeClassMergeBuild)
	if classDir == "" {
		return "", func() {}, false
	}
	workRoot, err := scratch.MkdirTempIn(classDir, manifestUnionGateScratchPattern)
	if err != nil {
		taskNotifyLog("merger: %s scratch unusable: %v", ManifestUnionGateName, err)
		return "", func() {}, false
	}
	return workRoot, func() {
		if err := os.RemoveAll(workRoot); err != nil {
			taskNotifyLog("merger: %s scratch %s not removed: %v", ManifestUnionGateName, workRoot, err)
		}
	}, true
}

// manifestUnionGateBlob writes one side's blob into the scratch directory and
// returns its text. The bytes go to a FILE because `git merge-file` takes
// paths: the merge then reads exactly what git would feed it, through git's own
// reader, rather than through this gate's newline handling.
func manifestUnionGateBlob(repo, workRoot, base, rev, path string) (string, error) {
	raw, err := manifestUnionGateCatFileBlob(repo, rev+":"+path)
	if err != nil {
		for _, marker := range manifestUnionGateAbsentBlobMarkers {
			if strings.Contains(err.Error(), marker) {
				// Absent on this side is a legitimate tree state, and it is
				// NOT this gate's case (a manifest on one side only merges by
				// taking that side; a deletion is a modify/delete conflict the
				// resolver lane owns). One empty file keeps the three-way
				// merge well-formed; the caller abstains on any empty side.
				return "", nil
			}
		}
		return "", fmt.Errorf("read %s:%s: %w", rev, path, err)
	}
	body := string(raw)
	if err := os.WriteFile(filepath.Join(workRoot, base+manifestUnionGateBlobSuffix), raw, scratch.FilePerm); err != nil {
		return "", fmt.Errorf("write %s blob: %w", base, err)
	}
	return body, nil
}

// manifestUnionGateCatFileBlob reads a blob's EXACT bytes.
//
// It deliberately does NOT go through the package's shared `repoSweepGit`
// helper: that runner TRIMS its output, because it was built for line-oriented
// plumbing verbs where a trailing newline is noise. A manifest's last row ends
// in a TAB, and the trailing whitespace of a manifest is not noise — it is the
// row's empty evidence column. Measured during this gate's own development: the
// trimmed read rewrote the last row of both sides, so the union ended
// "...document" instead of "...document\t" and the very duplicate this gate
// exists to refuse came back as an UNPARSABLE row (`3 fields, want 4`) — a
// false abstention produced entirely by the reader. A merge input must be the
// bytes git holds, so it is read through a writer that cannot normalise them.
func manifestUnionGateCatFileBlob(repo, spec string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), repoSweepGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, manifestUnionGateGitBinaryName,
		append([]string{"-C", repo, manifestUnionGateCatFileVerb, manifestUnionGateBlobArg}, spec)...)
	// THE LOCALE IS PINNED: this reader's ERROR is a decision input one level
	// up (manifestUnionGateBlob classifies it against
	// manifestUnionGateAbsentBlobMarkers). Measured on this host 2026-10-30:
	//
	//	$ LC_ALL=C            git cat-file -p HEAD:no/such/file
	//	fatal: path 'no/such/file' does not exist in 'HEAD'
	//	$ LC_ALL=zh_CN.UTF-8  git cat-file -p HEAD:no/such/file
	//	致命错误：路径 'no/such/file' 不在 'HEAD' 中
	//
	// Unpinned, "the manifest is absent on this side" (an ordinary tree state
	// the gate ABSTAINS on) became "read <rev>:<path>: … 不在 … 中" — a READ
	// FAILURE, i.e. the same input produced a different gate verdict by locale.
	cmd.Env = ChildLocaleEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s %s %s: %w: %s", manifestUnionGateCatFileVerb, manifestUnionGateBlobArg,
			spec, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// manifestUnionGateMergeFile runs the three-way merge of the three blobs and
// returns the union's text: `git merge-file -p <ours> <orig> <theirs>` with no
// label flags, because the output must be the BYTES the merge would write, not
// a labelled rendering of them.
//
// A non-zero exit is not a failure by itself: git reports a textual conflict
// with a negative status and leaves markers in the output, which the manifest
// parse then rejects — the caller abstains, and never refuses on a conflict's
// shape. Only a POSITIVE status (git itself failed) is an error here.
func manifestUnionGateMergeFile(workRoot string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), repoSweepGitTimeout)
	defer cancel()
	blob := func(base string) string {
		return filepath.Join(workRoot, base+manifestUnionGateBlobSuffix)
	}
	cmd := exec.CommandContext(ctx, manifestUnionGateGitBinaryName,
		manifestUnionGateMergeFileVerb, manifestUnionGateStdoutFlag,
		blob(manifestUnionGateOursBase), blob(manifestUnionGateOrigBase), blob(manifestUnionGateTheirsBase))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if !errorsAsExitError(err, &exitErr) {
			return "", fmt.Errorf("%s could not be run: %w", manifestUnionGateMergeFileVerb, err)
		}
		if code := exitErr.ExitCode(); code > manifestUnionGateMergeCleanExit {
			return "", fmt.Errorf("%s failed (%d): %s", manifestUnionGateMergeFileVerb, code,
				strings.TrimSpace(stderr.String()))
		}
	}
	return stdout.String(), nil
}

// manifestUnionGateDuplicates finds the keys the union's rows repeat, recording
// for each the two line numbers the standards gate would report, the union's
// own lines at those numbers (the bytes that would land) and — per line — which
// side's hunk produced it.
func manifestUnionGateDuplicates(rows []devdoc.Row, union, branchSrc, targetSrc string) []manifestUnionKey {
	lines := strings.Split(union, "\n")
	seen := map[string]bool{}
	var out []manifestUnionKey
	for _, r := range rows {
		if !seen[r.Path] {
			seen[r.Path] = true
			continue
		}
		first := manifestUnionGateFirstLine(rows, r.Path, r.Line)
		k := manifestUnionKey{
			Path:       r.Path,
			FirstLine:  first,
			SecondLine: r.Line,
			FirstText:  manifestUnionLineAt(lines, first),
			SecondText: manifestUnionLineAt(lines, r.Line),
		}
		k.FirstSide = manifestUnionLineSide(k.FirstText, branchSrc, targetSrc)
		k.SecondSide = manifestUnionLineSide(k.SecondText, branchSrc, targetSrc)
		out = append(out, k)
	}
	return out
}

// manifestUnionLineAt returns the union's line n (1-based), "" when the number
// is outside the body.
func manifestUnionLineAt(lines []string, n int) string {
	if n <= 0 || n > len(lines) {
		return ""
	}
	return lines[n-1]
}

// manifestUnionGateFirstLine returns the FIRST line the manifest carries for
// key, searching the rows up to (and excluding) the duplicate found at
// lastLine. The rows come from the union's own text, so the number is the
// union's coordinate — the second half of the pair the refusal must quote.
func manifestUnionGateFirstLine(rows []devdoc.Row, key string, lastLine int) int {
	for _, r := range rows {
		if r.Path == key && r.Line < lastLine {
			return r.Line
		}
	}
	return 0
}

// manifestUnionGateDetail renders the refusal: the standards gate's own
// evidence shape, once per line number (so the key AND both coordinates appear
// verbatim), followed by a note naming what each side contributed.
//
// The note is a FACT, never an attribution-by-guess: it quotes each side's row
// text and says outright when the two are identical (in which case nothing can
// tell the sides apart and the reworker must decide, which is exactly what the
// project's own post-mortem concluded about a merged duplicate — see
// devdoc/docindex-merge-dup-gate.md).
func manifestUnionGateDetail(path string, keys []manifestUnionKey) string {
	quoted := keys
	capped := false
	if len(quoted) > manifestUnionGateEvidenceLimit {
		quoted = quoted[:manifestUnionGateEvidenceLimit]
		capped = true
	}
	var parts []string
	for _, k := range quoted {
		for _, line := range []int{k.FirstLine, k.SecondLine} {
			if line <= 0 {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s:%d: duplicate row for %s", path, line, k.Path))
		}
	}
	detail := strings.Join(parts, manifestUnionGateRefuseStackSep)
	if capped {
		detail += fmt.Sprintf(" (+%d more duplicated key(s))", len(keys)-manifestUnionGateEvidenceLimit)
	}
	for _, k := range quoted {
		detail += manifestUnionGateNoteSep + manifestUnionGateKeyNote(k)
	}
	return detail
}

// manifestUnionGateKeyNote renders one duplicated key's note on the ONE line the
// refusal detail keeps to: the union's own two lines (the bytes that would
// land), each attributed to the side whose hunk produced it where the bytes
// allow it.
func manifestUnionGateKeyNote(k manifestUnionKey) string {
	var parts []string
	for _, l := range []struct{ text, side string }{
		{k.FirstText, k.FirstSide},
		{k.SecondText, k.SecondSide},
	} {
		if l.text == "" {
			continue
		}
		if l.side == "" {
			parts = append(parts, l.text+" ("+manifestUnionGateSidesIdenticalText+")")
			continue
		}
		parts = append(parts, l.side+": "+l.text)
	}
	if len(parts) == 0 {
		return k.Path
	}
	return strings.Join(parts, manifestUnionGateRefuseStackSep)
}

// The two side names a refusal quotes (Rule 1b: the same spelling reaches the
// rework brief and every test).
const (
	manifestUnionSideBranchName = "branch"
	manifestUnionSideTargetName = "integration branch"
)

// manifestUnionGateLog renders one verdict line, greppable as one family beside
// the sibling gates' own lines.
func manifestUnionGateLog(format string, args ...any) {
	taskNotifyLog("merger: "+ManifestUnionGateName+" "+format, args...)
}

// manifestUnionGateDisabled reports whether the operator escape hatch is set.
// Rule 2: the ONE reader; unset (or any other value) means ENABLED.
func manifestUnionGateDisabled() bool {
	return strings.TrimSpace(os.Getenv(manifestUnionGateEnvName)) == manifestUnionGateDisabledValue
}

// errorsAsExitError is the errors.As spelling for *exec.ExitError, kept local so
// the gate's error classification reads as one expression at its call site.
func errorsAsExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}
