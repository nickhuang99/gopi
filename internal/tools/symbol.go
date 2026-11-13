// Package tools implements the tool functions (read, write, edit, bash,
// code_search, keyword_search) used by the GoPi coding agent's ReAct loop.
package tools

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// tagProbeChunkBytes is the backward-scan window for locating a line start
// during the tags binary search (lineStartAt). Tags lines are short
// (name\tfile\tpattern), so 8KB covers them comfortably; the scan widens for
// pathological lines.
const tagProbeChunkBytes = 8 * 1024

// tagLineReadChunkBytes is the forward-read chunk for reading a tags line
// during a probe (readTagLineAt). Small enough that the per-query scratch
// buffer stays cheap; lines longer than a chunk are accumulated chunk-wise.
const tagLineReadChunkBytes = 4 * 1024

// tagMaxLineBytes caps a single tags line in both the binary-search probe
// (readTagLineAt) and the cluster scan (scanTagCluster). A line longer than
// this means a pathological index.
const tagMaxLineBytes = 1024 * 1024

// tagsIndexFileName is the on-disk name of the ctags index code_search builds
// inside the search root. It is a gitignored build artifact: nothing else
// (no other tool, no user) is expected to write it, and a crash or disk-full
// during a ctags write can leave it torn.
const tagsIndexFileName = ".gopi-tags"

// tagsIndexFileMode is the mode of the published index. It is set EXPLICITLY
// on the temp file before the rename because tagsIndexTempPattern's file comes
// out of os.CreateTemp at 0600, and rename(2) replaces the target node rather
// than its contents — publishing without the chmod would narrow every index.
const tagsIndexFileMode = 0o644

// tagsIndexTempPattern is the temp-file pattern ctags writes into before the
// atomic publish (runCtags). It keeps the dot prefix — the index's own
// convention for "a build artifact of this tool, not a source file" — so the
// build temp is identified the same way the target is; the operator's own
// scratch dirs, `.gopi/` sessions/logs and the committed `devdoc/` corpus are
// reached through `*` (nobody names a file this way), and the pattern holds no
// filesystem separator so it can never escape the index's directory.
//
// tagsIndexTempPrefix is the same thing as a PREFIX, which is what a reader
// needs to tell an in-flight temp from a source file by name alone (the
// staleness snapshot below, which must not treat a publish as a tree change).
const (
	tagsIndexTempPrefix  = tagsIndexFileName + "-tmp-"
	tagsIndexTempPattern = tagsIndexTempPrefix + "*"
)

// isTagsIndexArtifact reports whether a name in the search root belongs to this
// tool's own index rather than to the indexed sources: the published index or
// one of its in-flight temps. It exists because the staleness snapshot records
// only what the walk READS, and these are exactly the entries the walk and
// ctags both ignore — a publish landing in the root must not invalidate the
// snapshot it was just computed against.
func isTagsIndexArtifact(name string) bool {
	return name == tagsIndexFileName || strings.HasPrefix(name, tagsIndexTempPrefix)
}

// tagFileHeaderPrefix is the pseudo-tag that marks a ctags index —
// "!_TAG_FILE_FORMAT\t2\t/extended format; ..." — and it is the one structural
// invariant that distinguishes a real index from a foreign/garbage file.
//
// It is NOT the first line of every index, which is what this constant used to
// claim. MEASURED (exuberant-ctags 5.9~svn20110310, this repo, 2026-10-11):
// exuberant writes a byte-sorted file, so it SORTS the six pseudo-tags among
// the entries rather than putting them at byte 0. In this repo's 12,288,019-byte
// index the whole block sits at line 21, byte offset 4294, because exactly 20
// entries sort before '!': the Go parser formats a multi-line string literal
// into a search pattern, and internal/heavytest/boundary.go's literals begin
// with six spaces, so their first pattern line reads
// "      GOPI_HUMAN_TERMINAL=1 go test -run <Test> ..." and ' ' (0x20) < '!'
// (0x21). On a SMALL directory those literals are absent and the block lands at
// byte 0 (verified: the same ctags over a two-file temp dir opens with
// "!_TAG_FILE_FORMAT"), which is why the defect only appears at real repo scale
// and why a byte-0 prefix test shipped working. universal-ctags keeps the block
// at byte 0 as this constant always assumed.
const tagFileHeaderPrefix = "!_TAG_FILE_FORMAT"

// tagPseudoTagSeparator is the byte that must follow the prefix on a real
// pseudo-tag line ("!_TAG_FILE_FORMAT\t2\t..."), and it is what separates a
// pseudo-tag from an ordinary ENTRY whose pattern merely CONTAINS the prefix
// text: on this repo's index two Go entries carry
// `FileHeaderPrefix = "!_TAG_FILE_FORMAT"` inside their search pattern (offsets
// 9376658 and 11504509), where the byte after the prefix is '$' and the byte
// before it is '"' — neither is at a line start, and neither is followed by a
// tab, so the line-anchored probe below cannot mistake them for the block.
const tagPseudoTagSeparator = '\t'

// tagHeaderProbeBytes caps the validity probe's read window — the HEAD BUDGET
// of a bounded scan, not a one-shot read of the first line (it was 256 while
// the probe was a byte-0 prefix test). It must cover the pseudo-tag block's
// position in the worst real producer layout: measured 4294 bytes on this
// repo's exuberant index, and exactly 20 space-leading entries precede it.
// 64KiB is 15x that measurement, still a single cheap read, and 192x cheaper
// than the 12MB index itself — the probe costs no full-file scan. A window's
// worth of the file depends on its bytes, not on ctags or its version.
const tagHeaderProbeBytes = 64 * 1024

// Tags-file tail constants. MEASURED (universal-ctags 5.9.0, 2026-10-08, four
// full-tree runs over this repo): ctags appends WHOLE lines and closes a
// completed index with a trailing newline — the last bytes of a 17.7MB index
// are "...2026-09-10（最终版）\n". It prints NO end-of-file trailer (the string
// "sorted intact" appears neither in the binary nor in any generated index —
// checked with `strings $(which ctags)` and `grep -c` over the artifact), so a
// clean trailing newline IS the whole completion signal available on disk.
const tagsIndexTailProbeBytes = 16

// tagsIndexTailScanBytes bounds the byte-order scan over the END of the index.
// ctags writes its -f target IN PLACE (open O_WRONLY|O_APPEND, then ftruncate
// to 0 and append as it walks — measured 2026-10-08: same inode across the
// rewrite, size 0 → 17.7MB over ~10s), so a reader that opens the file mid-run
// sees the header plus a partial body. A reader's danger zone is its OWN EOF
// (that is where the writer was when the size was taken), so the scan reads the
// last 2MiB and requires the entry names inside it to be byte-ordered — the
// property a torn write breaks. 2MiB of a warm index costs ~1ms, against the
// 12.5s a full-tree ctags run costs, so this is affordable on the serve path.
const tagsIndexTailScanBytes = 2 * 1024 * 1024

// errTagsIndexInvalid marks an index file that is present but cannot be used
// as a ctags index (empty, NUL-filled, truncated, or a non-tag file). Such an
// index is a cache MISS, never a fatal error: ensureTags removes and
// regenerates it, because ctags itself refuses to clobber a file that "doesn't
// look like a tag file" — leaving code_search dead for the life of the repo
// until a human deletes the artifact by hand (the 2026-09-10 incident: a
// 2.58MB all-NUL .gopi-tags disabled every code_search call).
// tagsSelfHealMaxRetries bounds the ctags-refusal self-heal: after removing a
// damaged index the ctags run is retried this many times (one), never looped.
const tagsSelfHealMaxRetries = 1

// errTagsIndexIncomplete marks an index that is present, header-valid and
// UNFINISHED: the write that produced it never closed (a ctags run still in
// flight, or one killed mid-write — e.g. a code_search whose ctags exceeded
// the tool timeout). Distinct from errTagsIndexInvalid, which marks a file
// that is not a ctags index at all; this one IS a ctags index, it is just not
// all of one. See tagsIndexComplete for the measurement.
var errTagsIndexIncomplete = errors.New("tags index incomplete")

// tagsRegenSettleWindow bounds the incomplete-index rebuild: a rebuild costs a
// full-tree ctags run, so if the index was rebuilt inside this window and is
// UNFINISHED again, the cause is a concurrent writer in the same root rather
// than a damaged artifact, and re-indexing on every call would multiply the
// load instead of fixing anything. ensureTags/queryDefinition refuse loudly
// from then on (Rule 15) until a COMPLETE index is seen — never a silent serve,
// never a rebuild storm. Sized well above one ctags run (12.5s here) and well
// below "the operator walked away": a genuine one-off tear is repaired
// indefinitely.
const tagsRegenSettleWindow = 5 * time.Minute

// tagsIndexWatch is the per-root record that makes an unfinished index
// distinguishable from a clean one that merely LOOKS torn — the snapshot that
// lands exactly on a line boundary, whose last byte is a newline and whose
// prefix is byte-ordered because it IS a prefix. It carries three facts:
//
//   - completeSize / completeInfo — the size AND the file identity (device +
//     inode) of the last index this process saw FINISHED (clean tail plus
//     byte-ordered window). The identity is the half that survives a torn
//     in-place write stopping exactly on a line boundary (its bytes are
//     shape-identical to a small complete index): the atomic publish always
//     lands a NEW node, so the SAME node at a different length names a foreign
//     writer rewriting the very file we served. A DIFFERENT node at a different
//     size is a PUBLISH — ours or a PEER's — and is judged on its own bytes,
//     which is what stops a peer's healthy index being refused as unfinished
//     (see tagsIndexComplete).
//   - writtenSize / rejectedSize — the artifact this process PUBLISHED itself.
//     A rebuild that did not help must not be repeated off its own output, so a
//     size a probe actually REJECTED is refused; the rejection is about that
//     proven artifact, never about a number, so a complete index of the same
//     size is still served.
//   - rebuiltAt — when this root was last rebuilt for an unfinished index.
//   - verdictAt/verdictSize/verdictMtime/verdictOK — the last full probe's
//     result, reused while the artifact is byte-for-byte the same file (size AND
//     mtime). The scan costs 8.2ms on this repo's 15.9MB index, which is real
//     money on the highest-frequency tool, and the artifact cannot change without
//     changing one of those two facts (ctags begins a rewrite by truncating, so
//     even the size-0 moment invalidates the entry). Same shape as
//     cachedStalenessVerdict, one TTL (stalenessCacheTTL) for both facts about
//     one root's index.
//
// Keyed by root so two worktrees never mask each other; mutex-guarded because
// code_search may run inside R4's read-only parallel batches.
type tagsIndexWatch struct {
	completeSize int64
	writtenSize  int64
	rebuiltAt    time.Time

	// verdict* — the last full probe's result for this root's index and the
	// (size, mtime) it was computed at.
	verdictAt    time.Time
	verdictSize  int64
	verdictMtime time.Time
	verdictOK    bool

	// rejectedSize is the size of an artifact this process's own publish
	// produced and a completeness probe then REJECTED (verdictOK false at that
	// same size). It is what makes "do not accept the file we just published"
	// a statement about an artifact rather than about a number: the probes are
	// reached again on every lookup, and a verdict that has since flipped back
	// to true (the artifact was repaired and republished at the same size)
	// clears it, so a writer that produces a COMPLETE index can never be
	// refused by a stale rejection. Rules 1/13: one field, one meaning.
	rejectedSize int64

	// completeInfo is the file IDENTITY (device + inode) of the artifact the
	// completeSize observation was made on. It is what separates an ATOMIC
	// PUBLISH (rename lands a new node) from an IN-PLACE REWRITE of the very
	// file we served (same node, new length) — the only disk-visible trace left
	// by a torn in-place write that happens to stop on a line boundary, since
	// its bytes are shape-identical to a small complete index. The zero value
	// means "no node recorded", which never triggers the SameFile arm.
	completeInfo os.FileInfo
}

// sizeAlreadyJudgedUnfinished reports whether this process's own probe already
// rejected an artifact of exactly size bytes in this root — the arm that stops
// a rebuild from accepting the output of the rebuild it just ran.
func (w tagsIndexWatch) sizeAlreadyJudgedUnfinished(size int64) bool {
	return w.rejectedSize != 0 && w.rejectedSize == size
}

var (
	tagsIndexWatchByRoot = map[string]tagsIndexWatch{}
	tagsIndexWatchMu     sync.Mutex
)

// noteCompleteTagsIndex records that root's index was seen FINISHED — its size
// and file identity (info) and when the observation was made, keeping the rest
// of the record. Seeing a finished index also lifts the incomplete-rebuild
// throttle: whatever was wrong, the artifact is good now. The identity is what
// makes a later in-place rewrite of that very node detectable (the atomic
// publish always lands a new node, so a SameFile hit names a foreign writer).
func noteCompleteTagsIndex(root string, info os.FileInfo) {
	tagsIndexWatchMu.Lock()
	w := tagsIndexWatchByRoot[root]
	w.completeSize = info.Size()
	w.completeInfo = info
	w.writtenSize = 0
	w.rejectedSize = 0
	w.rebuiltAt = time.Time{}
	tagsIndexWatchByRoot[root] = w
	tagsIndexWatchMu.Unlock()
}

// noteTagsCompleteVerdict caches the probe's verdict for root's index as of
// (size, mtime), so an unchanged artifact is not re-scanned on every call.
func noteTagsCompleteVerdict(root string, size int64, mtime time.Time, ok bool) {
	tagsIndexWatchMu.Lock()
	w := tagsIndexWatchByRoot[root]
	w.verdictAt = time.Now()
	w.verdictSize = size
	w.verdictMtime = mtime
	w.verdictOK = ok
	tagsIndexWatchByRoot[root] = w
	tagsIndexWatchMu.Unlock()
}

// cachedTagsCompleteVerdict returns the cached probe verdict for root's index
// when it was computed for this exact (size, mtime) inside stalenessCacheTTL.
func cachedTagsCompleteVerdict(root string, size int64, mtime time.Time) (bool, bool) {
	w := tagsIndexWatchFor(root)
	if w.verdictAt.IsZero() || w.verdictSize != size || !w.verdictMtime.Equal(mtime) {
		return false, false
	}
	if time.Since(w.verdictAt) > stalenessCacheTTL {
		return false, false
	}
	return w.verdictOK, true
}

// notePublishedTagsIndex records the SIZE of the index this process last
// published into root (runCtags renames a finished artifact into place) and
// invalidates any earlier "finished at size S" observation — that file no
// longer exists. It is an OBSERVATION, not a verdict: a size that differs from
// it can only name a newer snapshot (a peer's publish, or another run of ours
// after this record), and a newer snapshot is judged on its own bytes. Where
// that matters is the reverse direction, and it is deliberate: an index of
// exactly this size that fails the completeness probes is the artifact WE
// published, so it can never serve as the "finished" witness for itself
// (otherwise a rebuild that did not help would validate its own output).
func notePublishedTagsIndex(root string, size int64) {
	tagsIndexWatchMu.Lock()
	w := tagsIndexWatchByRoot[root]
	w.completeSize = 0
	w.writtenSize = size
	w.rejectedSize = 0 // a new artifact: no verdict about the previous one survives it
	tagsIndexWatchByRoot[root] = w
	tagsIndexWatchMu.Unlock()
}

// noteTagsIndexUnfinished records that the completeness probes REJECTED root's
// index at this (size, mtime) and arms the artifact-level refusal: an index of
// exactly this size is the file THIS process published and could not make
// complete, so it can never serve as the "finished" witness for itself.
func noteTagsIndexUnfinished(root string, size int64, mtime time.Time) {
	tagsIndexWatchMu.Lock()
	w := tagsIndexWatchByRoot[root]
	w.verdictAt = time.Now()
	w.verdictSize = size
	w.verdictMtime = mtime
	w.verdictOK = false
	w.rejectedSize = size
	tagsIndexWatchByRoot[root] = w
	tagsIndexWatchMu.Unlock()
}

// noteIncompleteRebuild records that root had to rebuild an unfinished index.
func noteIncompleteRebuild(root string) {
	tagsIndexWatchMu.Lock()
	w := tagsIndexWatchByRoot[root]
	w.rebuiltAt = time.Now()
	tagsIndexWatchByRoot[root] = w
	tagsIndexWatchMu.Unlock()
}

// tagsIndexWatchFor returns root's record (the zero value when it has none).
func tagsIndexWatchFor(root string) tagsIndexWatch {
	tagsIndexWatchMu.Lock()
	defer tagsIndexWatchMu.Unlock()
	return tagsIndexWatchByRoot[root]
}

// clearTagsIndexWatch forgets root entirely, once a COMPLETE index is on disk:
// the repair worked, so a later tear deserves its own rebuild instead of
// inheriting a stale refusal.
func clearTagsIndexWatch(root string) {
	tagsIndexWatchMu.Lock()
	delete(tagsIndexWatchByRoot, root)
	tagsIndexWatchMu.Unlock()
}

// resetTagsIndexWatch clears the table (tests).
func resetTagsIndexWatch() {
	tagsIndexWatchMu.Lock()
	tagsIndexWatchByRoot = map[string]tagsIndexWatch{}
	tagsIndexWatchMu.Unlock()
}

// stalenessCacheTTL bounds how long a code_search staleness verdict is reused
// without re-walking the tree (R6 — devdoc/perf-vs-claude-fix-plan.md §R6).
// code_search is the highest-frequency tool (toolRoutingRules: every symbol
// lookup starts with it) and each call previously did a full filepath.Walk of
// the tree (50-500ms on 10-50K-file repos). The TTL is > 0 and shorter than a
// round, so a source change is recognized at most one round late; the mtime
// guard (tags file mtime compared on every call) makes the cache invalidate
// immediately when the index itself changes.
const stalenessCacheTTL = 2 * time.Second

// stalenessCacheEntry is one cached staleness verdict for a search root.
// tagsMtime is the .gopi-tags modification time the verdict was computed at —
// a change in it (regeneration, touch) invalidates the entry even inside the
// TTL (mtime guard, not a pure time window).
type stalenessCacheEntry struct {
	tagsMtime time.Time // .gopi-tags mtime the verdict was computed at
	stale     bool      // verdict: tags older than the newest source file?
	walkedAt  time.Time // when the walk behind the verdict ran
}

// stalenessCache holds the per-root staleness verdicts. A single agent run
// searches one root, but code_search may run concurrently inside R4's
// read-only parallel batches, so the map is mutex-guarded.
var (
	stalenessCache   = map[string]stalenessCacheEntry{} // keyed by root
	stalenessCacheMu sync.Mutex
	// stalenessWalkCount counts real full-tree walks (observability + tests).
	stalenessWalkCount atomic.Int64
	// tagsSelfHealCount counts invalid-index self-heals (the index was found
	// unusable and rebuilt) — observability for the 2026-09-10 corrupt-index
	// class, and the signal tests assert on (a healthy repo stays at zero).
	tagsSelfHealCount atomic.Int64
)

// stalenessWalkCacheTTL bounds how long a walk snapshot is reused without a
// full re-walk. The R2 A/B measured the TTL-expired full-tree walk at 151ms
// on a 30K-file repo (hermes) — every code_search round after the verdict
// expired paid it. The incremental check (stat the root + its immediate
// subdirectories) runs in microseconds and reuses the last walk's newest
// source mtime when nothing structural changed. The 2s verdict TTL and the
// tags mtime guard are untouched — only the re-derivation is cheaper.
// Tradeoff: a source file edited in place does not bump directory mtimes,
// so it is recognized at most stalenessWalkCacheTTL late; a new file,
// deletion or rename bumps the directory chain up to the top level and
// re-walks immediately.
const stalenessWalkCacheTTL = 30 * time.Second

// walkSnapshot is the incremental-validation record for one search root:
// the newest indexed source mtime from the last full walk plus the entries
// of the root and the mtimes of its immediate subdirectories at walk time.
// Revalidation stats only those entries — if all are unchanged, no file was
// added, removed or renamed anywhere under the root (directory mtimes
// propagate up the tree), so the cached newest stays valid. Only real walks
// refresh the snapshot (takenAt never moves on incremental hits, so the TTL
// still bounds in-place-edit recognition).
//
// rootEntries holds the root's OWN non-directory names, and it is not an
// optimization: a directory mtime is bumped by any entry being created,
// renamed or removed inside it — including this tool's own index publish
// (runCtags creates a temp beside the target and renames it on) — so the bare
// root mtime cannot distinguish "a source file appeared" from "we wrote our
// index here", and reading it as a change would force a full walk after every
// single regeneration (measured: TestStalenessWalk_IncrementalSkipsWalk went
// red with one extra walk). The names ARE what the walk reads: a source file
// added, removed or renamed at any depth under the root changes the name set
// of its own directory, and that change bumps the mtime of every ancestor
// directory chain — which is exactly what subdirs records one level down.
type walkSnapshot struct {
	newest      time.Time
	rootEntries map[string]bool      // root's non-directory names at walk time (index excluded)
	subdirs     map[string]time.Time // immediate subdirectory name → mtime
	takenAt     time.Time
}

var (
	walkCache   = map[string]walkSnapshot{} // keyed by root
	walkCacheMu sync.Mutex
)

// ctagsLanguages is the comma-separated language set passed to ctags
// --languages. Keep in sync with indexedSourceExtensions: ctags must parse
// every extension the staleness check and the ripgrep fallback cover.
// (universal-ctags language names: Go, Python, Sh, C++, C, Rust, JavaScript,
// TypeScript — TypeScript is a separate parser, not a JavaScript alias.)
// C++ must stay listed even when only .h is wanted: universal-ctags routes
// .h through the C++ parser when C is not selected (verified 5.9.0).
// Entries missing from the installed binary (exuberant-ctags 5.9 has no
// Rust/TypeScript parsers) are filtered out by ctagsCapabilities — a bare
// unknown-language entry makes exuberant bail, never a partial index.
const ctagsLanguages = "Go,Python,Sh,C++,C,Rust,JavaScript,TypeScript"

// ctagsLanguageMapArgs maps the extensions universal-ctags does not assign to
// a parser by default (.cjs → JavaScript, .tsx → TypeScript) so the index
// covers every extension in indexedSourceExtensions. Must be passed as
// separate args: a comma-joined "--map-A=+x,--map-B=+y" option only applies
// the first mapping. Exuberant-ctags does not know --map-* at all, so these
// are capability-probed (see ctagsCapabilities) before use.
var ctagsLanguageMapArgs = []string{"--map-JavaScript=+.cjs", "--map-TypeScript=+.tsx"}

// ctagsExuberantLangmap is the exuberant-ctags fallback: fold every
// JavaScript-adjacent extension into the JavaScript parser via --langmap
// (exuberant has no --map-*; its --langmap takes dot-form extension lists
// like "JavaScript:.js.cjs.mjs.ts.tsx" — verified 5.9~svn20110310). Used only
// when the --map-* probe fails (see resolveCaps).
const ctagsExuberantLangmap = "JavaScript:.js.cjs.mjs.ts.tsx"

// ctagsCaps holds the option surface the installed ctags binary actually
// supports. Exuberant 5.9 (still shipped by several LTS distros) rejects
// --map-* and --extras outright and refuses unknown --languages entries with
// exit 1 — probing once per process keeps ensureTags from failing on it
// while universal-ctags keeps the full-featured args.
type ctagsCaps struct {
	languages  string   // filtered --languages list (non-empty)
	mapArgs    []string // --map-* args (empty when unsupported)
	extrasFlag string   // "--extras=+q" (universal) or "--extra=+q" (exuberant)
	langmapArg string   // --langmap fallback for exuberant (empty for universal)
}

var (
	capsOnce    sync.Once
	capsShared  ctagsCaps
	capsResolve = resolveCaps // injectable for tests
)

// resolveCaps probes the ctags binary once: which of the wanted languages it
// lists, whether --map-* parses, and whether the qualified-alias extra is
// spelled --extras (universal) or --extra (exuberant).
func resolveCaps() ctagsCaps {
	var caps ctagsCaps

	// Wanted ∩ listed languages. `ctags --list-languages` works on both
	// variants and is exact (no guessy probes per language).
	if out, err := exec.Command("ctags", "--list-languages").Output(); err == nil {
		listed := map[string]bool{}
		for _, ln := range strings.Split(string(out), "\n") {
			listed[strings.TrimSpace(ln)] = true
		}
		var kept []string
		for _, lang := range strings.Split(ctagsLanguages, ",") {
			if listed[lang] {
				kept = append(kept, lang)
			}
		}
		caps.languages = strings.Join(kept, ",")
	} else {
		caps.languages = ctagsLanguages // probe failed — pass through unchanged
	}

	// --map-* support (universal only).
	if err := exec.Command("ctags", "--map-JavaScript=+.cjs", "--version").Run(); err == nil {
		caps.mapArgs = ctagsLanguageMapArgs
	} else {
		// Exuberant-ctags 5.9 fallback: no --map-*, but --langmap exists and
		// accepts dot-form extension lists. JavaScript-adjacent extensions
		// (.cjs/.mjs/.ts/.tsx) are folded into the JavaScript parser so
		// JS/TS sources still land in the index (verified 5.9~svn20110310:
		// "JavaScript:.js.cjs.mjs.ts.tsx" parses functions/consts in
		// .ts/.tsx; the tag `language:` field then reads JavaScript — a
		// documented degradation, not a silent gap).
		caps.langmapArg = ctagsExuberantLangmap
	}

	// Qualified aliases: --extras=+q (universal) vs --extra=+q (exuberant).
	if err := exec.Command("ctags", "--extras=+q", "--version").Run(); err == nil {
		caps.extrasFlag = "--extras=+q"
	} else {
		caps.extrasFlag = "--extra=+q"
	}
	return caps
}

// ctagsCapabilities returns the probed capability set (resolved once per
// process and cached — every stale-index regeneration reuses it).
func ctagsCapabilities() ctagsCaps {
	capsOnce.Do(func() { capsShared = capsResolve() })
	return capsShared
}

// indexedSourceExtensions is the set of source file extensions code_search
// covers. .h is deliberately INCLUDED: headers are where C/C++ definitions
// live for a reviewer (structs, prototypes, macros) and code_search was
// blind to them before. Excluded: .pyi (stub files, marginal for review).
var indexedSourceExtensions = []string{
	".go", ".py",
	".sh", ".bash",
	".c", ".h",
	".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx",
	".rs",
	".js", ".jsx", ".mjs", ".cjs",
	".ts", ".tsx",
}

// CodeSearchGlobs is the ripgrep include-glob list for the code_search scope
// ("**/*.go", "**/*.py", ...), derived from indexedSourceExtensions so the
// ctags index, the staleness check, and the ripgrep fallback always cover the
// same languages. The agent's codeSearchScope uses it directly.
var CodeSearchGlobs = func() []string {
	globs := make([]string, len(indexedSourceExtensions))
	for i, ext := range indexedSourceExtensions {
		globs[i] = "**/*" + ext
	}
	return globs
}()

// queryDefinition looks up a symbol in the ctags index at root. The index is
// generated lazily on first use (or regenerated if stale). Returns structured
// symbol hits: name, kind, file:line, signature.
//
// A miss (nil, nil) is not an error — the caller (CodeSearch) falls back to
// ripgrep so an existing symbol never comes back empty.
//
// R3-6: the lookup is a binary search on the sorted index instead of a linear
// scan — the R2 A/B measured the scan at 14ms on a 2.8MB index (387 files)
// and ~210ms on a 115MB one (hermes), so every lookup paid the full file
// size. The tags file is sorted by name in byte order (verified against
// universal-ctags 5.9: case-sensitive, "!_TAG_..." headers first), which is
// exactly Go's string ordering, so the query lands on its name cluster in
// O(log n) probes. The bare-name cluster is complete: --extras=+q qualified
// aliases (pkg.Name) always pair with a bare entry at the same file:line,
// which the dedup below collapses anyway.
func queryDefinition(ctx context.Context, root, symbol string) ([]CodeSymbolHit, error) {
	tagsPath, err := ensureTags(ctx, root)
	if err != nil {
		return nil, err
	}

	hits, err := searchTagIndex(tagsPath, symbol)
	if err == nil && len(hits) > 0 {
		return hits, nil
	}

	// The index was present and header-valid, yet unsearchable (torn content
	// that kept the header, an oversized line, I/O failure) — a corrupt
	// artifact must cost one regeneration, not disable code_search forever.
	// Drop it and rebuild from scratch, then retry exactly once.
	if err != nil {
		if rmErr := removeInvalidTagsIndex(tagsPath); rmErr != nil {
			return nil, err
		}
		tagsSelfHealCount.Add(1)
		tagsPath, rebuildErr := ensureTags(ctx, root)
		if rebuildErr != nil {
			return nil, rebuildErr
		}
		return searchTagIndex(tagsPath, symbol)
	}

	// A CLEAN MISS over an index the writer has not finished is not a miss.
	// ctags appends its entries in name order while it walks the tree, so a
	// symbol sorting after the running writer's position is legitimately absent
	// from the bytes on disk right now — reading that as "undefined" is exactly
	// the silent degradation this fix exists to stop (the tool falls back to
	// ripgrep and answers with reference-only hits for a symbol that exists,
	// e.g. code_search's own definition). A miss over a FINISHED index is a
	// real miss and still falls through to the fallback.
	if tagsIndexComplete(tagsPath) {
		return hits, nil
	}
	if w := tagsIndexWatchFor(root); !w.rebuiltAt.IsZero() && time.Since(w.rebuiltAt) < tagsRegenSettleWindow {
		return nil, incompleteTagsIndexError(tagsPath, time.Since(w.rebuiltAt))
	}
	if rmErr := removeInvalidTagsIndex(tagsPath); rmErr != nil {
		return nil, rmErr
	}
	tagsSelfHealCount.Add(1)
	noteIncompleteRebuild(root)
	tagsPath, rebuildErr := ensureTags(ctx, root)
	if rebuildErr != nil {
		return nil, rebuildErr
	}
	if !tagsIndexComplete(tagsPath) {
		return nil, incompleteTagsIndexError(tagsPath, 0)
	}
	return searchTagIndex(tagsPath, symbol)
}

// incompleteTagsIndexError is the loud refusal both call sites share: an index
// that a rebuild has not turned into a finished one is a condition a human has
// to look at (a concurrent writer in the same root), never something to serve
// or to retry on every call (Rule 15 — no silent failure, no rebuild storm).
// since > 0 means an earlier rebuild inside tagsRegenSettleWindow is the
// evidence.
func incompleteTagsIndexError(tagsPath string, since time.Duration) error {
	if since > 0 {
		return fmt.Errorf("code_search: %s: %w (rebuilt %s ago and the index on disk is still unfinished) — refusing to re-index the whole tree on every call",
			tagsPath, errTagsIndexIncomplete, since.Truncate(time.Second))
	}
	return fmt.Errorf("code_search: %s: %w (rebuilt and still unfinished — a concurrent writer is indexing this root)",
		tagsPath, errTagsIndexIncomplete)
}

// searchTagIndex runs the binary-search lookup against the index at tagsPath
// and returns the deduplicated definition hits. An empty slice + nil error is
// a clean miss (the caller falls back to ripgrep).
func searchTagIndex(tagsPath, symbol string) ([]CodeSymbolHit, error) {
	f, err := os.Open(tagsPath)
	if err != nil {
		return nil, fmt.Errorf("code_search: open tags: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("code_search: stat tags: %w", err)
	}

	start, err := tagsLowerBound(f, info.Size(), symbol)
	if err != nil {
		return nil, err
	}
	if start >= info.Size() {
		return nil, nil // every entry sorts before the symbol — clean miss
	}
	hits, err := scanTagCluster(f, start, symbol)
	if err != nil {
		return nil, err
	}

	// Deduplicate duplicate entries of the same definition (defensive — a
	// sorted index emits each definition once; the R2 A/B dedup also covered
	// qualified aliases, which the binary search no longer collects).
	seen := make(map[string]bool)
	out := hits[:0]
	for _, h := range hits {
		key := fmt.Sprintf("%s:%d:%s", h.FilePath, h.LineNo, h.SymbolKind)
		if !seen[key] {
			seen[key] = true
			out = append(out, h)
		}
	}
	return out, nil
}

// tagsLowerBound returns the byte offset of the first tags entry whose name
// (first tab-separated field) is >= target — the start of the target's
// cluster, or the insertion point when the name is absent (== size when it
// sorts after every entry). The file is sorted by name in byte order, so the
// comparison is Go string ordering. Probe points land mid-line: the line
// start is resolved backward (lineStartAt) so the line containing a probe is
// always examined — the standard lower-bound invariant holds: every line
// before lo sorts before target, every line at/after hi sorts at/after it.
//
// Per-probe buffers are small (tagProbeChunkBytes / tagLineReadChunkBytes) —
// the first binary-search version allocated tagMaxLineBytes (1MB) per probe
// and ran 3x slower than the linear scan it replaced (~3.4ms vs ~1ms on a
// 300KB index); at these sizes the per-query alloc churn is negligible.
//
// Every probe offset this function hands to readTagLineAt lies in [0, size]
// (ls comes from lineStartAt, which clamps to >= 0 and to the probe point, and
// the EOF position size is a legitimate result here) — readTagLineAt's doc
// comment defines what each of those offsets means.
func tagsLowerBound(f *os.File, size int64, target string) (int64, error) {
	lo, hi := int64(0), size
	for lo < hi {
		mid := (lo + hi) / 2
		ls, err := lineStartAt(f, mid)
		if err != nil {
			return 0, err
		}
		text, err := readTagLineAt(f, ls, size)
		if err != nil {
			return 0, err
		}
		if tagLineKey(text) < target {
			lo = ls + int64(len(text)) + 1
			if lo > size {
				lo = size
			}
		} else {
			hi = ls
		}
	}
	return lo, nil
}

// scanTagCluster reads the contiguous run of entries at start whose name
// equals symbol (the file is sorted, so the run ends at the first mismatch).
// The scan is bounded by the cluster size, not the file size — a generic
// name's cluster is the only case that scans more than a few lines.
func scanTagCluster(f *os.File, start int64, symbol string) ([]CodeSymbolHit, error) {
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, fmt.Errorf("code_search: seek tags: %w", err)
	}
	var hits []CodeSymbolHit
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, tagMaxLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if tagLineKey(line) != symbol {
			break // past the cluster — the file is sorted
		}
		h := formatTagEntry(line)
		// Normalize to the queried name — ctags emits both qualified and
		// unqualified variants of the same symbol.
		h.SymbolName = symbol
		hits = append(hits, h)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("code_search: read tags: %w", err)
	}
	return hits, nil
}

// tagLineKey returns the first tab-separated field of a tags line — the
// entry's name, which the file is sorted by.
func tagLineKey(line string) string {
	if i := strings.IndexByte(line, '\t'); i >= 0 {
		return line[:i]
	}
	return line
}

// lineStartAt returns the offset of the start of the line containing pos:
// the byte after the last newline in [0, pos). The window is scanned
// backward in tagProbeChunkBytes chunks and widens for lines longer than a
// chunk (tags lines are short in practice; the scan is bounded by
// tagMaxLineBytes).
func lineStartAt(f *os.File, pos int64) (int64, error) {
	if pos <= 0 {
		return 0, nil
	}
	for window := int64(tagProbeChunkBytes); ; window *= 2 {
		if window > tagMaxLineBytes {
			return 0, fmt.Errorf("code_search: tags line exceeds %d bytes", tagMaxLineBytes)
		}
		start := pos - window
		if start < 0 {
			start = 0
		}
		// Bound the read to [start, pos): a newline at/after pos terminates a
		// LATER line and must not count as this line's start (a trailing
		// newline at EOF would otherwise resolve to pos == size).
		readLen := window
		if pos-start < window {
			readLen = pos - start
		}
		buf := make([]byte, readLen)
		n, err := f.ReadAt(buf, start)
		if err != nil && err != io.EOF {
			return 0, fmt.Errorf("code_search: read tags: %w", err)
		}
		if i := bytes.LastIndexByte(buf[:n], '\n'); i >= 0 {
			return start + int64(i) + 1, nil
		}
		if start == 0 {
			return 0, nil // no newline anywhere in [0, pos) — one long line
		}
	}
}

// readTagLineAt reads the full line starting at off (up to the next newline
// or EOF) in small chunks — a 1MB per-probe allocation made the first
// binary-search version 3x slower than the linear scan (see tagsLowerBound).
// A line longer than tagMaxLineBytes is an error — a tags line that long
// means a pathological index (the cluster scan uses the same cap).
//
// The (off, size) contract, for the binary-search caller:
//   - off in [0, size): the line starting at off — its text, or "" when a
//     newline sits at off itself (an empty line). Whether the line is
//     newline-terminated makes no difference; text before EOF is returned.
//   - off == size: EOF. Returns ("", nil) — size is a legitimate result
//     (tagsLowerBound returns it for a name sorting after every entry), so
//     this position must be safe to read; there is simply no line there.
//   - off < 0 or off > size: an out-of-range position, reported as an error.
//
// The loop is bounded by size: it stops AT EOF, so pos never advances past it,
// and that bound is what keeps the EOF clamp `readLen = size - pos`
// non-negative. ReadAt cannot do this job for us — it reports io.EOF only on a
// SHORT read, so a probe landing on an exactly-sized, unterminated tail (the
// index's last line with no trailing newline) reads a full chunk with
// err == nil, the loop's own EOF branch (a zero-length read) never fires, and
// stepping pos by the CHUNK carried it over size. `size - pos` then went
// negative and buf[:readLen] panicked — the 2026-09-21 P0 "slice bounds out of
// range [:-3952]" (task 01a0c37d), which killed the whole agent process.
func readTagLineAt(f *os.File, off, size int64) (string, error) {
	if off < 0 || off > size {
		return "", fmt.Errorf("code_search: read tags: line offset %d out of range [0, %d]", off, size)
	}
	var sb strings.Builder
	buf := make([]byte, tagLineReadChunkBytes)
	for pos := off; pos < size && sb.Len() < tagMaxLineBytes; pos += int64(len(buf)) {
		readLen := int64(len(buf))
		if size-pos < readLen {
			readLen = size - pos
		}
		n, err := f.ReadAt(buf[:readLen], pos)
		if i := bytes.IndexByte(buf[:n], '\n'); i >= 0 {
			sb.Write(buf[:i])
			return sb.String(), nil
		}
		sb.Write(buf[:n])
		if err == io.EOF {
			return sb.String(), nil // last line without a trailing newline
		}
		if err != nil {
			return "", fmt.Errorf("code_search: read tags: %w", err)
		}
	}
	if sb.Len() >= tagMaxLineBytes {
		return "", fmt.Errorf("code_search: tags line exceeds %d bytes", tagMaxLineBytes)
	}
	// EOF reached on an exactly-sized read: the last line has no trailing
	// newline and is returned as-is (see the contract above for off == size).
	return sb.String(), nil
}

// formatTagEntry converts a ctags line into a structured symbol hit.
//
// ctags format: name\tfile\tpattern;"\tkind\t<extras>
//
//	e.g. NewClient\tinternal/deepseek/client.go\t/^func NewClient.../;"\tf\tline:12\tpackage:deepseek\ttyperef:typename:(*Client, error)
//
// The pattern field doubles as the signature — it is a snapshot of the raw
// definition line (ctags anchors patterns to the defining line). LineNo is
// parsed from the line:N extra and the kind name is disambiguated by the
// language:X extra (both emitted by --fields=+ln); entries from indices
// generated without those flags carry LineNo 0 and the Go kind mapping.
func formatTagEntry(line string) CodeSymbolHit {
	parts := strings.Split(line, "\t")
	h := CodeSymbolHit{SymbolKind: "symbol"}
	if len(parts) > 0 {
		h.SymbolName = parts[0]
	}
	if len(parts) < 4 {
		return h
	}
	h.FilePath = parts[1]
	h.Signature = signatureFromPattern(parts[2])
	kind := ""
	language := ""
	for _, p := range parts[3:] {
		switch {
		case strings.HasPrefix(p, "line:"):
			if n, err := strconv.Atoi(strings.TrimPrefix(p, "line:")); err == nil {
				h.LineNo = n
			}
		case strings.HasPrefix(p, "language:"):
			language = strings.TrimPrefix(p, "language:")
		case len(p) == 1 && !strings.Contains(p, ":"):
			kind = p
		}
	}
	h.SymbolKind = kindName(kind, language)
	return h
}

// signatureFromPattern extracts the definition line text from a ctags search
// pattern, e.g. "/^func NewClient() (*Client, error) {$/;\"" — anchored to the
// line start (^) and usually the line end ($). The signature is the pattern
// with anchors and ctags escaping stripped.
func signatureFromPattern(pattern string) string {
	sig := pattern
	if strings.HasPrefix(sig, "/^") {
		sig = strings.TrimPrefix(sig, "/^")
	} else if strings.HasPrefix(sig, "/") {
		sig = strings.TrimPrefix(sig, "/")
	}
	// The closing "/;" is part of the field; a trailing "$" is the end anchor.
	if i := strings.LastIndex(sig, "$/;\""); i >= 0 {
		sig = sig[:i]
	} else if i := strings.LastIndex(sig, "/;\""); i >= 0 {
		sig = sig[:i]
	}
	// ctags escapes regex metacharacters inside patterns: "\/" → "/", "\\" → "\".
	sig = strings.ReplaceAll(sig, `\/`, "/")
	sig = strings.ReplaceAll(sig, `\\`, `\`)
	return sig
}

// kindName maps single-letter ctags kinds to readable names. Kind letters
// collide across languages — Python/C++/Rust "c" is a class, Go "c" is a
// const — so the language field (emitted by --fields=+l) disambiguates.
// Entries without a language field (e.g. indices built by older versions)
// fall back to the Go mapping.
func kindName(kind, language string) string {
	if names, ok := languageKindNames[language]; ok {
		if name, ok := names[kind]; ok {
			return name
		}
		if kind == "" {
			return "symbol"
		}
		return kind
	}
	if name, ok := goKindNames[kind]; ok {
		return name
	}
	if kind == "" {
		return "symbol"
	}
	return kind
}

// goKindNames is the Go kind map, used as the default for entries without a
// language field.
var goKindNames = map[string]string{
	"f": "func", "s": "struct", "i": "interface", "m": "method",
	"t": "type", "v": "var", "c": "const", "p": "package",
}

// languageKindNames maps ctags kinds per indexed language, from
// `ctags --list-kinds-full=<lang>` for the kinds the index can emit.
var languageKindNames = map[string]map[string]string{
	"Python": {
		"c": "class", "f": "func", "m": "method", "v": "var",
		"i": "import", "I": "namespace",
	},
	"Sh": {
		"a": "alias", "f": "func", "h": "heredoc", "s": "script",
	},
	"C++": {
		"c": "class", "d": "macro", "e": "enumerator", "f": "func",
		"g": "enum", "h": "header", "m": "member", "n": "namespace",
		"s": "struct", "t": "type", "u": "union", "v": "var",
	},
	"Rust": {
		"M": "macro", "P": "method", "c": "impl", "e": "enumerator",
		"f": "func", "g": "enum", "i": "interface", "m": "field",
		"n": "module", "s": "struct", "t": "type", "v": "var",
	},
	// JavaScript kinds from `ctags --list-kinds-full=JavaScript`. Covers
	// .js, .jsx, .mjs and .cjs files (the latter via ctagsLanguageMapArgs).
	"JavaScript": {
		"C": "const", "G": "getter", "M": "field", "S": "setter",
		"c": "class", "f": "func", "g": "generator", "m": "method",
		"p": "property", "v": "var",
	},
	// TypeScript kinds from `ctags --list-kinds-full=TypeScript`. Covers
	// .ts and .tsx files (the latter via ctagsLanguageMapArgs).
	"TypeScript": {
		"C": "const", "G": "generator", "a": "alias", "c": "class",
		"e": "enumerator", "f": "func", "g": "enum", "i": "interface",
		"m": "method", "n": "namespace", "p": "property", "v": "var",
	},
}

// cachedStalenessVerdict returns the cached staleness verdict for root when
// it is still valid: the tags file's mtime matches the one the verdict was
// computed at (mtime guard — a touch or regeneration invalidates immediately)
// AND the walk happened within stalenessCacheTTL (so a source change is
// recognized at most one round late). ok=false means the caller must walk.
func cachedStalenessVerdict(root string, tagsMtime time.Time) (stale, ok bool) {
	stalenessCacheMu.Lock()
	defer stalenessCacheMu.Unlock()
	e, found := stalenessCache[root]
	if !found || e.tagsMtime != tagsMtime || time.Since(e.walkedAt) > stalenessCacheTTL {
		return false, false
	}
	return e.stale, true
}

// cacheStalenessVerdict records a staleness verdict for root, stamped with
// the tags file's mtime at computation time and the walk time.
func cacheStalenessVerdict(root string, tagsMtime time.Time, stale bool) {
	stalenessCacheMu.Lock()
	defer stalenessCacheMu.Unlock()
	stalenessCache[root] = stalenessCacheEntry{
		tagsMtime: tagsMtime,
		stale:     stale,
		walkedAt:  time.Now(),
	}
}

// resetStalenessCache clears all cached verdicts AND walk snapshots (test
// isolation — both caches hold per-root state that must not leak between
// tests).
func resetStalenessCache() {
	stalenessCacheMu.Lock()
	defer stalenessCacheMu.Unlock()
	stalenessCache = map[string]stalenessCacheEntry{}
	walkCacheMu.Lock()
	defer walkCacheMu.Unlock()
	walkCache = map[string]walkSnapshot{}
}

// newestSourceFileCached returns the newest indexed source file mtime under
// root, reusing the cached walk result when the incremental check passes
// (see walkSnapshot and stalenessWalkCacheTTL). realWalk reports whether a
// full tree walk ran — stalenessWalkCount counts only real walks, so tests
// can observe the incremental path.
func newestSourceFileCached(ctx context.Context, root string) (newest time.Time, realWalk bool, err error) {
	walkCacheMu.Lock()
	snap, ok := walkCache[root]
	walkCacheMu.Unlock()
	if ok && time.Since(snap.takenAt) <= stalenessWalkCacheTTL && topLevelUnchanged(root, snap) {
		return snap.newest, false, nil
	}
	newest, err = newestSourceFile(ctx, root)
	if err != nil {
		return time.Time{}, false, err
	}
	walkCacheMu.Lock()
	walkCache[root] = snapshotWalk(root, newest)
	walkCacheMu.Unlock()
	return newest, true, nil
}

// topLevelUnchanged reports whether the root and every immediate
// subdirectory recorded in the snapshot still carry the same mtimes. A new
// or removed subdirectory, or any entry added/removed/renamed anywhere below
// (the directory chain bumps up to the top), fails the check and forces a
// re-walk. Non-directory entries in the root are covered by the root mtime.
func topLevelUnchanged(root string, snap walkSnapshot) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	dirs := 0
	files := 0
	for _, e := range entries {
		if e.IsDir() {
			dirs++
			m, ok := snap.subdirs[e.Name()]
			if !ok {
				return false
			}
			ei, err := e.Info()
			if err != nil || !ei.ModTime().Equal(m) {
				return false
			}
			continue
		}
		// Our own index and its in-flight temps are written by the publish
		// itself, so they are excluded by name on BOTH sides of the compare:
		// their arrival is not a tree change (see walkSnapshot.rootEntries).
		if isTagsIndexArtifact(e.Name()) {
			continue
		}
		files++
		if !snap.rootEntries[e.Name()] {
			return false
		}
	}
	return dirs == len(snap.subdirs) && files == len(snap.rootEntries)
}

// snapshotWalk records a walk result (newest source mtime + top-level
// directory mtimes) for incremental revalidation.
func snapshotWalk(root string, newest time.Time) walkSnapshot {
	snap := walkSnapshot{
		newest:      newest,
		rootEntries: map[string]bool{},
		subdirs:     map[string]time.Time{},
	}
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				if ei, err := e.Info(); err == nil {
					snap.subdirs[e.Name()] = ei.ModTime()
				}
				continue
			}
			if !isTagsIndexArtifact(e.Name()) {
				snap.rootEntries[e.Name()] = true
			}
		}
	}
	snap.takenAt = time.Now()
	return snap
}

// ensureTags generates (or regenerates) the ctags index at root/.gopi-tags.
// Regeneration triggers when the tags file is missing or older than the newest
// indexed source file (simple staleness check — full correctness is not
// required; a stale index still returns useful symbol locations).
//
// R6: when the tags file exists, the staleness verdict is served from the
// cache (cachedStalenessVerdict) whenever the tags mtime is unchanged and the
// verdict is inside the TTL — the full-tree walk is skipped on cache hits.
//
// --fields=+ln adds the definition line number and the language to each entry,
// which CodeSymbolHit.LineNo and the per-language kind name are parsed from.
func ensureTags(ctx context.Context, root string) (string, error) {
	// Normalize to an absolute root: the index path and the per-root watch key
	// must name ONE tree, and callers differ (the agent passes ".", the CLI
	// passes --root, a test chdirs into a temp dir). With a relative key, two
	// different trees that happen to be the CWD would share the same witness.
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	tagsPath := filepath.Join(root, tagsIndexFileName)
	watchKey := filepath.Dir(tagsPath)

	stale := false
	needsRemove := false
	if info, err := os.Stat(tagsPath); err == nil {
		// A present-but-unusable index (NUL fill / torn write / crash-truncated)
		// is a cache MISS: ctags refuses to clobber a file that is not a tag
		// file, so without this check the artifact would disable code_search
		// permanently. Probe the head BEFORE any staleness work — one
		// bounded 64KiB read of a head window (tagHeaderProbeBytes), and it
		// also skips the tree walk for a file we are about to replace anyway.
		if !tagsIndexValid(tagsPath) {
			tagsSelfHealCount.Add(1)
			needsRemove = true
			stale = true
		} else if !tagsIndexComplete(tagsPath) {
			// A header-valid index whose writer never finished (a ctags write
			// in flight, or one that died mid-write — see tagsIndexComplete):
			// its body is a partial prefix, so the sorted-body assumption the
			// binary search relies on is false and code_search would degrade
			// silently to the ripgrep fallback. It is a cache MISS, like the
			// NUL-filled class above — with one bound: a rebuild costs a
			// full-tree ctags run (12.5s here), so if this root was rebuilt
			// inside tagsRegenSettleWindow and the index is STILL unfinished,
			// the situation is systemic (a concurrent writer in the same
			// worktree) and re-indexing on every call only multiplies the
			// load. Refuse loudly instead (Rule 15) — never serve it and
			// never loop.
			if w := tagsIndexWatchFor(watchKey); !w.rebuiltAt.IsZero() && time.Since(w.rebuiltAt) < tagsRegenSettleWindow {
				return "", incompleteTagsIndexError(tagsPath, time.Since(w.rebuiltAt))
			}
			tagsSelfHealCount.Add(1)
			noteIncompleteRebuild(watchKey)
			needsRemove = true
			stale = true
		} else if cached, ok := cachedStalenessVerdict(root, info.ModTime()); ok {
			// Reuse the last verdict when the tags file is unchanged and fresh;
			// otherwise re-derive the verdict — via the incremental walk check
			// (R3-7: stat only the top-level dirs when the tree is unchanged) or
			// a full walk, caching both outcomes.
			stale = cached
		} else {
			newest, _, err := newestSourceFileCached(ctx, root)
			if err == nil && newest.After(info.ModTime()) {
				stale = true
			}
			cacheStalenessVerdict(root, info.ModTime(), stale)
		}
	} else {
		stale = true // missing — never served from cache (no tags mtime)
	}

	if stale {
		if needsRemove {
			if err := removeInvalidTagsIndex(tagsPath); err != nil {
				return "", err
			}
		}
		// Last-resort self-heal: ctags consulted its own rules and found the
		// target unusable (a refusal our header probe did not predict, e.g. a
		// differently-formatted foreign file). ctags made no write, so dropping
		// the artifact and retrying once is safe — a damaged artifact must cost
		// one retry, never a permanently dead tool. The second failure is
		// returned untouched.
		for attempt := 0; ; attempt++ {
			runErr := runCtags(ctx, root, tagsPath)
			if runErr == nil {
				break
			}
			if attempt < tagsSelfHealMaxRetries && !needsRemove && tagsIndexInvalidErr(runErr) {
				tagsSelfHealCount.Add(1)
				if rmErr := removeInvalidTagsIndex(tagsPath); rmErr == nil {
					continue
				}
			}
			return "", runErr
		}
		// Record a fresh verdict with the regenerated tags mtime so a
		// same-round follow-up code_search skips the walk. A COMPLETE index
		// also lifts the incomplete-index throttle: the repair worked. The
		// SIZE just measured is recorded as this process's own observation of
		// the artifact, whether or not it looked complete at this instant: the
		// publish is atomic (runCtags renames a finished temp into place), so a
		// size that differs from it belongs to a PEER's newer snapshot, and a
		// peer's complete index must be judged on its own bytes rather than
		// refused against a stale number of ours.
		if info, err := os.Stat(tagsPath); err == nil {
			cacheStalenessVerdict(root, info.ModTime(), false)
			notePublishedTagsIndex(watchKey, info.Size())
			if tagsIndexComplete(tagsPath) {
				clearTagsIndexWatch(watchKey)
			}
		}
	}
	return tagsPath, nil
}

// runCtags generates the index at tagsPath from the sources under root
// (cmd.Dir = root, so the relative "." recursion and the index path line up).
// The capabilities probe picks the universal-ctags flags or the exuberant
// fallback.
//
// The publish is ATOMIC: ctags writes a unique temp file beside the target
// (tagsIndexTempPattern, same directory so the rename cannot cross a
// filesystem) and the finished artifact is renamed onto tagsPath, which
// rename(2) does atomically inside one directory. ctags itself writes its -f
// target IN PLACE (measured: open O_WRONLY|O_APPEND, truncate to 0, append as
// it walks — ~10.2s of a 12.5s run with the target visible as a 2.2MB
// byte-ordered prefix), so without the rename EVERY reader that opens the
// index while a rebuild is in flight — including a second gopi process or a
// concurrent test in the same worktree — sees a partial artifact by
// construction, and the tree went red with `tags index incomplete` for a
// writer that was merely still running.
func runCtags(ctx context.Context, root, tagsPath string) error {
	caps := ctagsCapabilities()
	args := []string{"-R", "--languages=" + caps.languages}
	args = append(args, caps.mapArgs...)
	// Exuberant fallback: fold .ts/.tsx/.cjs/.mjs into the JavaScript
	// parser via --langmap (empty for universal-ctags, which uses the
	// --map-* args above instead).
	if caps.langmapArg != "" {
		args = append(args, "--langmap="+caps.langmapArg)
	}
	// The temp name is UNIQUE PER RUN and shares the target's directory: two
	// concurrent rebuilders of the same root then never write the same temp
	// file, and the rename that publishes the artifact is same-directory.
	tmp, err := os.CreateTemp(filepath.Dir(tagsPath), tagsIndexTempPattern)
	if err != nil {
		return fmt.Errorf("code_search: tags index temp: %w", err)
	}
	tmpPath := tmp.Name()
	if closeErr := tmp.Close(); closeErr != nil {
		return fmt.Errorf("code_search: tags index temp %s: %w", tmpPath, closeErr)
	}
	// CreateTemp RESERVED the name; ctags is meant to CREATE its target, so the
	// placeholder is removed before the run. Two reasons, both measured: ctags'
	// refusal path is about a target it did not write ("doesn't look like a tag
	// file"), and a freshly created empty file is exactly a thing a stand-in (or
	// a stricter ctags build) refuses — which would defeat every attempt.
	if err := os.Remove(tmpPath); err != nil {
		return fmt.Errorf("code_search: tags index temp %s: %w", tmpPath, err)
	}
	defer func() {
		if tmpPath != "" {
			// A pending temp is always OUR litter (the rename below clears the
			// variable on success) — never leave one behind next to the index.
			_ = os.Remove(tmpPath) //stylegate:ignore a pending temp is always this call's own litter (the rename below clears the variable on success); a failed remove leaves it for the next run
		}
	}()
	args = append(args, caps.extrasFlag, "--fields=+ln", "-f", tmpPath, ".")
	cmd := exec.CommandContext(ctx, "ctags", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("code_search: ctags: %w: %s", err, out)
	}
	// Carry the mode explicitly: CreateTemp makes the temp 0600, and POSIX
	// rename(2) REPLACES the target node, so publishing without this would
	// silently narrow a 0644 index to 0600 (the same node-replacement trap the
	// sandbox profile symlinks document).
	if err := os.Chmod(tmpPath, tagsIndexFileMode); err != nil {
		return fmt.Errorf("code_search: tags index temp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, tagsPath); err != nil {
		return fmt.Errorf("code_search: publish tags index %s: %w", tagsPath, err)
	}
	tmpPath = "" // published: the deferred cleanup has nothing to remove
	return nil
}

// tagsIndexValid reports whether the file at tagsPath is usable as a ctags
// index: non-empty, carrying the tag-file pseudo-tag block
// (tagFileHeaderPrefix), and free of NUL bytes. The probe reads only
// tagHeaderProbeBytes from the head and scans it for a pseudo-tag LINE.
//
// It must NOT require the prefix at BYTE 0, which is what it used to do (a
// bytes.HasPrefix test) and what made it reject every index this repo's
// exuberant-ctags produces. MEASURED (exuberant 5.9~svn20110310, 2026-10-11):
// exuberant writes a byte-sorted file and sorts its six pseudo-tags AMONG the
// entries, so on this repo the block sits at line 21 / offset 4294 — behind 20
// entries whose names begin with a space (the Go parser's first pattern line for
// a multi-line string literal in internal/heavytest/boundary.go). A small
// directory has no such literal and the block lands at byte 0, so the byte-0
// test passed there and only broke at real scale. The probe is therefore
// POSITION-INDEPENDENT over the head window, while still bounded: it never
// scans the whole 12MB index (that is a full-file read on the
// highest-frequency tool, and a full-tree ctags run costs 0.94s on this host).
//
// What keeps it a real test rather than a substring search:
//
//   - the prefix must open a LINE and be followed by tagPseudoTagSeparator, so
//     an ordinary entry whose search PATTERN contains the prefix text — this
//     repo's index has two such Go entries, at offsets 9376658 and 11504509 —
//     cannot be mistaken for the block;
//   - a NUL anywhere in the window still rejects (the 2026-09-10 incident: a
//     2.58MB all-NUL artifact). A NUL-free window is also what makes the byte
//     scan safe over arbitrary bytes.
//
// It is deliberately a HEAD test with no completion signal (one 64KiB read,
// called on every code_search): a pseudo-tag-bearing index whose body is torn
// is the separate class tagsIndexComplete answers, and the two are used
// together by ensureTags — this one rejects foreign/empty/NUL artifacts, that
// one rejects a body the writer has not finished.
func tagsIndexValid(tagsPath string) bool {
	f, err := os.Open(tagsPath)
	if err != nil {
		return false // unreadable (permissions, dangling) = unusable
	}
	defer f.Close()
	buf := make([]byte, tagHeaderProbeBytes)
	n, err := f.Read(buf)
	if (err != nil && err != io.EOF) || n == 0 {
		return false
	}
	head := buf[:n]
	if bytes.ContainsRune(head, 0) {
		return false // torn/NUL-filled artifact — never a ctags index
	}
	return tagPseudoTagLineOffset(head) >= 0
}

// tagPseudoTagLineOffset returns the offset of the first ctags pseudo-tag line
// in head — tagFileHeaderPrefix at the start of a line (preserving whatever
// line start the head window happened to begin at) and followed by a tab — or
// -1 when the window carries none, which is not a ctags index head. It is the
// ONE definition of "this window holds the block", so the probe and the tests
// that pin the producer layouts cannot disagree about what counts.
//
// MERGE NOTE (re-land of 8fafbafee onto master, 2026-10-13). This conflict was
// NOT a semantic clash: the re-landed commit and master's ffa1cf4f8 are the SAME
// change from the SAME base (identical subject and body), so both sides carried
// the same predicate. The resolution therefore chose a spelling, it did not
// choose a behaviour: master's comment/constant blocks are kept (they carry the
// later measured numbers) and this function's body is the re-landed commit's own
// (`off < len(head)` with an `end` variable, which is what the archived diff
// carries — verified against ~/workspace/merge-run-01a12b/branch3-*.diff).
// Both spellings are gofmt-clean (measured: `gofmt -l` lists neither form) and
// both return the offset of the first prefix that opens a line and is followed by
// a tab, accepting the block at byte 0 (start == 0) or at the window's first byte.
func tagPseudoTagLineOffset(head []byte) int {
	prefix := []byte(tagFileHeaderPrefix)
	for off := 0; off < len(head); {
		i := bytes.Index(head[off:], prefix)
		if i < 0 {
			return -1
		}
		start := off + i
		atLineStart := start == 0 || head[start-1] == '\n'
		end := start + len(prefix)
		if atLineStart && end < len(head) && head[end] == tagPseudoTagSeparator {
			return start
		}
		// Not a line-anchored pseudo-tag: an entry pattern quoting the prefix
		// text, or a prefix fragment cut by the window's end. Resume after it.
		off = end
	}
	return -1
}

// tagsIndexComplete reports whether the index at tagsPath is FINISHED — the
// writer closed it — as opposed to a header-valid fragment of a write still in
// flight. tagsIndexValid answers "is this a ctags index at all"; this answers
// "may its sorted-body assumption be trusted", and the two failure classes are
// independent:
//
//   - the 2026-09-10 class (all-NUL / foreign / truncated header) fails the
//     header probe;
//   - THIS class keeps a perfectly good header and a real, byte-ordered PREFIX
//     of the final content, because ctags writes its -f target in place: it
//     opens O_WRONLY|O_APPEND, truncates to 0 and appends as it walks the tree
//     (measured 2026-10-08: same inode across the rewrite, size 0 → 17.7MB in
//     ~10s). A reader that opens it mid-run binary-searches a body that is not
//     there yet, misses a symbol that exists, and code_search silently degrades
//     to the ripgrep fallback (reference-only hits) — the reported symptom.
//
// The check is CHEAP on purpose: code_search is the highest-frequency tool and
// a full-tree ctags run costs 12.5s, so validating the whole body (O(n)) is not
// an option. Two O(1) probes cover the class:
//
//  1. the TAIL — ctags appends WHOLE lines, so a COMPLETED index ends with a
//     terminating newline and a write that stopped mid-entry has none. There
//     is NO end-of-file trailer to look for: none is printed by any ctags
//     variant, and none is probed below (the older wording here named a
//     "\nsorted intact\n" trailer that exists in no binary and no generated
//     index — see the measurement above tagsIndexTailProbeBytes).
//     tagsIndexEndsCleanly's newline test is the whole disk-visible signal;
//  2. the byte ORDER of the last tagsIndexTailScanBytes — a truncated write
//     ends wherever the walk happened to stop, so the entries straddling the
//     reader's EOF are out of order. ctags' comparison is byte-exact numeric
//     string ordering (strcmp, measured: a full 17.7MB index passes an
//     LC_ALL=C sort -c and is unchanged across five post-exit samples), which
//     is exactly Go's string ordering, so a clean run never trips this.
//  3. the NODE IDENTITY, which is the only probe that sees the residue of a
//     write that stopped exactly ON a line boundary: those bytes are a whole
//     line-terminated, byte-ordered PREFIX, i.e. shape-identical to a small
//     complete index. What distinguishes them is observation — the artifact is
//     the very file this process already served as complete (os.SameFile) at a
//     different length. Our own publish is a rename, which always lands a NEW
//     node, so a SameFile hit names a writer that does not publish atomically.
//
// A path that cannot be stat'ed is NOT usability: false, so ensureTags treats
// a vanished index as a miss (it regenerates) rather than serving it.
func tagsIndexComplete(tagsPath string) bool {
	f, err := os.Open(tagsPath)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return false
	}
	if !tagsIndexEndsCleanly(f, info.Size()) {
		// ctags appends WHOLE lines: an unterminated final line can only be a
		// write that stopped mid-entry.
		return false
	}
	root := filepath.Dir(tagsPath)
	if abs, err := filepath.Abs(root); err == nil {
		root = abs // one key per TREE: a relative and an absolute root naming the same directory must not be two witnesses
	}
	w := tagsIndexWatchFor(root)
	inPlaceTorn := false
	if w.completeSize > 0 && os.SameFile(w.completeInfo, info) {
		// The artifact on disk is the SAME FILE NODE this process served as
		// complete, at a DIFFERENT LENGTH: a writer that does not publish
		// atomically (rename always lands a NEW node) is rewriting this very
		// file in place, so what is on disk is a prefix of its former or future
		// self. That is the shape the completeness probes CANNOT see by
		// themselves when the write stops exactly on a line boundary (the tail
		// is a whole line, and a prefix of a sorted file is sorted) — which is
		// why the node identity is carried at all.
		//
		// Same node and SAME length is the other half, and it is not evidence:
		// an in-place writer that has reached this length has written every
		// byte, so the artifact is judged on its contents below (the restored
		// index of the probe fixture lands exactly here), and an unchanged
		// (mtime, size) pair is answered from the cached verdict.
		inPlaceTorn = info.Size() != w.completeSize
	}
	if inPlaceTorn {
		// Do not trust the bytes. This is the shape the completeness probes
		// CANNOT see by themselves when a write stops exactly on a line
		// boundary (the tail is a whole line, and a prefix of a sorted file is
		// sorted), which is why the node identity is carried at all.
		noteTagsIndexUnfinished(root, info.Size(), info.ModTime())
		return false
	}
	if w.completeSize > 0 && info.Size() != w.completeSize {
		// A DIFFERENT node at a different length: the artifact was PUBLISHED,
		// not rewritten in place — our own later rebuild, or a PEER's publish in
		// the same worktree (the window that made the old check fire on a peer's
		// perfectly good index and refuse it as "unfinished"). The stale size is
		// therefore not evidence against the bytes in front of us: drop it and
		// let the probes below judge the new artifact on its own contents,
		// because a refusal here is what turns a healthy tree red.
		w.completeSize = 0
	}
	if w.writtenSize != 0 && info.Size() == w.writtenSize && w.sizeAlreadyJudgedUnfinished(info.Size()) {
		// The bytes are exactly the artifact this process PUBLISHED, and the
		// completeness probes have already judged that exact size unfinished —
		// a rebuild that produced the same unfinished file must not be accepted
		// as the finished thing (that is how a rebuild storm would start; the
		// stand-in that never produces a whole index is the fixture of it).
		// Only a size a probe actually REJECTED arms this, so a writer that
		// produces a COMPLETE index of that size is served on its own bytes
		// (the ordinary stale-rebuild case) — the refusal is about a proven
		// unfinished artifact, never about a number.
		noteTagsCompleteVerdict(root, info.Size(), info.ModTime(), false)
		return false
	}
	if ok, cached := cachedTagsCompleteVerdict(root, info.Size(), info.ModTime()); cached {
		// The full probe already ran for THIS artifact (same size and mtime) —
		// the 2MiB scan is 8.2ms on this repo's index, which is not a per-call
		// price on the highest-frequency tool.
		return ok
	}
	if !tagsTailIsSorted(f, info.Size()) {
		noteTagsIndexUnfinished(root, info.Size(), info.ModTime())
		return false
	}
	// Confirmed: the tail is a whole line AND the names in the last window are
	// byte-ordered. Adopt the size and the node as this process's witness, so a
	// later replacement or an in-place rewrite of this root's index is
	// detectable at O(1) — and clear any rejection recorded for a same-sized
	// artifact, because a complete index of that size has just been served (the
	// repair path).
	noteCompleteTagsIndex(root, info)
	noteTagsCompleteVerdict(root, info.Size(), info.ModTime(), true)
	return true
}

// completeTagsIndexSize returns the size at which root's index was last seen
// FINISHED, and whether such an observation exists. Every artifact this process
// PUBLISHED clears that observation (the file it named no longer exists), so a
// publish is only ever re-adopted as complete by a probe that accepts it.
func completeTagsIndexSize(root string) (int64, bool) {
	w := tagsIndexWatchFor(root)
	if w.completeSize == 0 {
		return 0, false
	}
	return w.completeSize, true
}

// tagsIndexEndsCleanly reports whether the index's last byte is a line
// terminator — the property ctags' whole-line appending gives a finished index.
func tagsIndexEndsCleanly(f *os.File, size int64) bool {
	off := size - tagsIndexTailProbeBytes
	if off < 0 {
		off = 0
	}
	buf := make([]byte, size-off)
	if _, err := f.ReadAt(buf, off); err != nil {
		return false
	}
	return buf[len(buf)-1] == '\n'
}

// tagsTailIsSorted reports whether the entry names in the LAST window of the
// index are in non-descending byte order — the property ctags guarantees for a
// completed run and a torn write breaks. A window holding fewer than two
// entries is trivially ordered.
func tagsTailIsSorted(f *os.File, size int64) bool {
	off := size - tagsIndexTailScanBytes
	if off < 0 {
		off = 0
	}
	buf := make([]byte, size-off)
	if _, err := f.ReadAt(buf, off); err != nil {
		return false // a window we cannot read is not evidence of order
	}
	lines := strings.Split(string(buf), "\n")
	if off > 0 && len(lines) > 0 {
		// The window begins mid-line: that leading fragment is not an entry.
		lines = lines[1:]
	}
	prev := ""
	for _, line := range lines {
		if !tagLineIsEntry(line) {
			continue
		}
		key := tagLineKey(line)
		if prev != "" && key < prev {
			return false
		}
		prev = key
	}
	return true
}

// tagLineIsEntry reports whether a tags line is a real entry (name\tfile\t...)
// rather than a pseudo-tag header line (`!_TAG_...`) or blank filler. Headers
// are block-sorted by ctags but are not part of the name ordering, so they are
// excluded from the order check.
func tagLineIsEntry(line string) bool {
	if line == "" || strings.HasPrefix(line, "!_TAG_") {
		return false
	}
	return strings.Count(line, "\t") >= 2
}

// removeInvalidTagsIndex deletes an unusable index so the next ctags run can
// create a fresh one (ctags refuses to overwrite a non-tag file, and a
// half-written index would be appended to by --extras=+q runs otherwise).
// A already-absent file is not an error.
func removeInvalidTagsIndex(tagsPath string) error {
	if err := os.Remove(tagsPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("code_search: remove invalid tags index %s: %w", tagsPath, err)
	}
	return nil
}

// tagsIndexInvalidErr reports whether a failed ctags run failed because its
// -f target is not a tag file (ctags' own refusal: `"<path>" doesn't look like
// a tag file; I refuse to overwrite it.`). Only this refusal is safe to
// self-heal — a syntax error in the sources must not trigger a delete+retry.
func tagsIndexInvalidErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "doesn't look like a tag file") ||
		strings.Contains(msg, "not a tag file")
}

// newestSourceFile returns the modification time of the most recently modified
// indexed source file under root (excluding vendor and hidden dirs).
func newestSourceFile(ctx context.Context, root string) (time.Time, error) {
	stalenessWalkCount.Add(1) // R6: observe real walks (cache skips don't count)
	var newest time.Time
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if info.IsDir() {
			name := info.Name()
			if name == "vendor" || name == ".git" || (name != "." && strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if hasIndexedExt(path) && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	if newest.IsZero() {
		return time.Time{}, fmt.Errorf("no indexed source files found")
	}
	return newest, err
}

// hasIndexedExt reports whether path ends with one of the indexed source
// file extensions.
func hasIndexedExt(path string) bool {
	for _, ext := range indexedSourceExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
