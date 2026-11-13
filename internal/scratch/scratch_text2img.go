package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// scratch_text2img.go — WHERE a text2img render with no `--out` lands, and WHO
// collects it.
//
// THE DEFECT THIS CLOSES (measured 2026-11-07, task 01a1b3cf). Both `--out`-less
// faces of the renderer — `gopi tool text2img <src>` and the model's own
// `text2img` tool — defaulted the destination to `<src 去扩展名>.png`, i.e. BESIDE
// THE SOURCE FILE (`internal/text2img/render.go`'s `out == ""` branch). When src
// lives inside a git work tree — and a task run's cwd IS its clone ground — the
// render leaves an untracked PNG in that tree, which is PROJECT.md Rule 30's
// exact defect (`completed-unclean`, debris no merge path picks up). The measured
// repro, run before the fix in a clean clone:
//
//	git status --porcelain | wc -l     # 0
//	gopi tool text2img probe.txt       # writes probe.png beside probe.txt
//	git status --porcelain             # ?? probe.png
//
// This is the SAME defect the capture family carried (task 01a1b3b1,
// scratch_captures.go), and it gets the SAME answer for the same reasons —
// documented there and not repeated here: land in a DUMP CLASS TREE that an
// existing GC arm already walks (DeclaredTrees → `gopi tool scratch-gc trees` and
// scratchGCTick's resident pass), NOT the caller's cwd, NOT the session directory
// (no collector there), and NOT "the file never exists" (the summary reports
// output PATHS and the next step reads them back).
//
// WHAT THE `/text2img` SLASH COMMAND ALREADY DID RIGHT, AND WHY IT IS UNTOUCHED.
// `cmd/agent/commands.go` fills its own out from `scratch.DumpDir(RuntimeClassText2Img)`
// and never reaches the default branch below. That face was already correct; this
// change does not move it, and the class directory it uses (`dumps/text2img`, the
// RUNTIME scratch class) is deliberately a DIFFERENT directory from this dump
// class (`dumps/gopi-text2img`) — one is a staging area for a slash command, the
// other is the default destination of a render a caller asked for by path.

const (
	// DumpClassText2Img is the class directory a default-named render lands in —
	// `<scratch home>/dumps/gopi-text2img`, the join `ScratchDumpsClassRoot`
	// makes (the same join `DumpDir` makes), exactly like the dump and capture
	// classes beside it.
	//
	// The `gopi-` prefix is the DUMP namespace, not decoration: it keeps this
	// class disjoint from the slash command's RUNTIME staging class
	// (`RuntimeClassText2Img` = "text2img", i.e. `dumps/text2img`), which is a
	// different thing that happens to share the noun.
	DumpClassText2Img = "gopi-text2img"

	// Text2ImgTreeName is this tree's identity on the watchdog log line
	// (`tree=`), in the declared-set gate's diagnostics and in `--tree`'s
	// selector. One definition (Rules 13/14).
	Text2ImgTreeName = "scratch-text2img"

	// Text2ImgDumpFileSuffix is the extension a default-named render carries.
	//
	// IT IS NOT `.png`, AND THAT IS THE SAFETY PROPERTY — the same argument
	// CaptureDumpFileSuffix carries. The reclaim predicate must be able to say
	// "this file is OURS to collect" from the NAME alone, and
	// `dumps/gopi-text2img/` is a directory a human may also drop a PNG into (it
	// is on the operator's own filesystem). `<stem>.dump.png` is a namespace only
	// this writer produces, so a hand-placed `diagram.png` beside it is NEVER a
	// candidate.
	//
	// IT IS ALSO THE MULTI-PAGE SHAPE, and that is why the renderer splits the
	// root on this WHOLE suffix rather than on `filepath.Ext`: a three-page render
	// must be `<stem>-1.dump.png`, `<stem>-2.dump.png`, `<stem>-3.dump.png` — not
	// `<stem>.dump-1.png`, which is a name NO predicate here accepts and which no
	// sweep would therefore ever collect.
	Text2ImgDumpFileSuffix = ".dump.png"

	// Text2ImgDumpStemPrefix names the family, so a reader of the directory can
	// tell which writer dropped a file. Referenced by the predicate below rather
	// than re-spelled (Rules 13/14).
	Text2ImgDumpStemPrefix = "text2img-"
)

// Text2ImgStaleAge is how long an unread default-named render may sit before the
// sweep may collect it.
//
// IT IS THE DUMP WINDOW ON PURPOSE (its own NAME, the same VALUE, the same
// reasoning scratch_captures.go records for the capture class): a render is
// replay/evidence material for a step that has already happened, and the window
// is generous because the failure it guards against — collecting a PNG a live
// run is about to read — costs a run, while the failure it accepts costs a few
// hundred KB until the next pass.
const Text2ImgStaleAge = ScratchDumpsStaleAge

// text2imgDumpStemPattern is the alphabet a default-named render's stem may use:
// the family prefix, the unix stamp, and the OPTIONAL page discriminator the
// renderer appends for page 2 onward. It is a PATTERN rather than a prefix test
// because the reclaim's whole safety argument is "only names this writer produces
// are candidates" — `text2img-<digits>` with an optional `-<page>` and nothing
// else.
var text2imgDumpStemPattern = regexp.MustCompile(`^` + Text2ImgDumpStemPrefix + `[0-9]+(-[1-9][0-9]*)?$`)

// IsText2ImgDumpName reports whether a file NAME is a default-named render this
// writer created, i.e. whether the text2img class may collect it. It is the row's
// namespace predicate AND the writer's own name check (Rule 14: one definition,
// so the writer cannot produce a name its collector refuses) — including EVERY
// page of a multi-page render, which is what a predicate written for a
// single-page shape would silently leave behind.
func IsText2ImgDumpName(name string) bool {
	stem, ok := strings.CutSuffix(name, Text2ImgDumpFileSuffix)
	if !ok || stem == "" {
		return false
	}
	return text2imgDumpStemPattern.MatchString(stem)
}

// Text2ImgDumpStem is the base name (suffix excluded) of a render minted at
// stamp — the ONE spelling of the family's name shape.
func Text2ImgDumpStem(stamp int64) string {
	return Text2ImgDumpStemPrefix + strconv.FormatInt(stamp, 10)
}

// Text2ImgDumpName is the file name of one PAGE of a default-named render:
// page 1 (or a single page) is `<stem><suffix>`, page N > 1 is
// `<stem>-<N><suffix>`.
//
// It exists so the multi-page shape is a DEFINITION rather than a second
// `fmt.Sprintf` in the renderer: `IsText2ImgDumpName` must accept exactly what
// this produces, and a test drives both against each other.
func Text2ImgDumpName(stamp int64, page int) string {
	stem := Text2ImgDumpStem(stamp)
	if page > 1 {
		stem = fmt.Sprintf("%s-%d", stem, page)
	}
	return stem + Text2ImgDumpFileSuffix
}

// Text2ImgDumpRoot resolves the class directory a default-named render lands in,
// WITHOUT creating it — the contract the other class-tree resolvers honour.
func Text2ImgDumpRoot() (string, error) {
	if HomeDir() == "" {
		return "", nil
	}
	return ScratchDumpsClassRoot(DumpClassText2Img)
}

// Text2ImgDumpFile resolves and CREATES the destination of a render that was
// given no `--out`, returning the ABSOLUTE path.
//
// FAILURE IS REPORTED, NEVER SWALLOWED (Rule 15): falling back to the source
// file's directory when the scratch home cannot be created would silently
// reinstate the very defect this function exists to close.
func Text2ImgDumpFile() (string, error) {
	dir, err := Text2ImgDumpRoot()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", fmt.Errorf("text2img: no scratch home to write into (scratch.HomeDir is empty) — pass an explicit --out")
	}
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		return "", fmt.Errorf("text2img: create %s: %w", dir, err)
	}
	stamp := time.Now().Unix()
	// Two renders inside the same second (a batch of probes) must not overwrite
	// each other — and the DISAMBIGUATOR IS THE STAMP ITSELF, advanced by one
	// second per collision, NOT an extra `-N` group.
	//
	// WHY NOT the `-<n>` shape CaptureDumpFile uses: in THIS family `-<N>` is
	// already the PAGE discriminator (`Text2ImgDumpName`), so
	// `text2img-<stamp>-2.dump.png` would mean both "the second render of this
	// second, page 1" and "the first render, page 2". Both are collectable, so
	// the ambiguity costs no reclaim — it costs a READER, who cannot tell a
	// collision from a page when looking at the directory. Advancing the stamp
	// keeps ONE meaning per name shape (Rule 14's reason, applied to a name).
	for n := 0; ; n++ {
		path := filepath.Join(dir, Text2ImgDumpName(stamp+int64(n), 1))
		if _, serr := os.Lstat(path); os.IsNotExist(serr) {
			return path, nil
		}
		if n >= text2imgNameCollisionLimit {
			return "", fmt.Errorf("text2img: no free file name in %s after %d attempts — pass an explicit --out", dir, text2imgNameCollisionLimit)
		}
	}
}

// text2imgNameCollisionLimit bounds the loop above (Rule 1: no magic number). It
// is the capture class's limit, for its reason: more than a handful of renders in
// one second is a sign of something else wrong, and an unbounded loop would spin
// on a directory the process cannot write.
const text2imgNameCollisionLimit = captureNameCollisionLimit

// text2imgDumpTree is the class tree's declared row: the SHAPE the dump and
// capture classes already use (files carrying ONE suffix, judged by the shared
// age rule), rooted at the class directory the writer creates.
//
// The class is TreeClassErrorDump because the FAMILY is the same one — "a file a
// writer drops into a class directory and nobody reads back" — and a reader of
// `scratch-gc trees` must be able to see this row beside the rows it is a
// sibling of. The row's NAME is what distinguishes it.
func text2imgDumpTree() DeclaredTree {
	return DeclaredTree{
		Name:       Text2ImgTreeName,
		Class:      TreeClassErrorDump,
		Kind:       EntryKindFileSuffix,
		Suffix:     Text2ImgDumpFileSuffix,
		StaleAge:   Text2ImgStaleAge,
		EntryMatch: IsText2ImgDumpName,
		Resolve:    Text2ImgDumpRoot,
	}
}

// Text2ImgDumpTrees returns the text2img class rows. It is the ONE place they
// are assembled (DeclaredTrees appends this), so a caller that needs the set
// reads the same construction the sweep walks rather than a second spelling.
func Text2ImgDumpTrees() []DeclaredTree {
	return []DeclaredTree{text2imgDumpTree()}
}
