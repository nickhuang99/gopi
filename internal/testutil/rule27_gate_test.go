package testutil

// Rule 27 (NEVER generate device load to satisfy a test) as a CHECK.
//
// WHY THIS FILE EXISTS
// --------------------
// The ban was written into GOPI.md after the SECOND incident (2026-09-11) and
// the THIRD still happened (2026-09-16): three overlapping `iopress.sh` batches,
// 48 concurrent `dd ... oflag=direct conv=fsync` writers, load 56.42, every dd in
// state D. A principle is not effective; a check is. This file is the check for
// the part the scripts audit (scripts_gate_test.go) could not see:
//
//   1. the Go half — a `dd` invocation inside a *_test.go, or an unbounded
//      `for {}` wrapping a device writer;
//   2. the LOOP half — a `while :; do … done` (or the bounded-loop variant that
//      fooled nobody: `iopress.sh`'s own loop WAS bounded) wrapping a command
//      that writes to a device;
//   3. the GENERATED half — the generator that caused 2026-09-16 was not a
//      checked-in file, it was hand-written into a worktree's `.evid/`. So this
//      gate scans the WORKING TREE (untracked files and dot-directories
//      included), not just the index, and it says where that reach ends.
//
// The three prior incidents, mechanically:
//   2026-08-27  `dd if=/dev/zero of=… oflag=direct conv=fsync` loops; `/` filled.
//   2026-09-11  `while :; do dd … conv=fsync; done` (script `suite-ab.sh`);
//               / 100% full, hermes had to be killed.
//   2026-09-16  three `iopress.sh` batches × 16 workers, PPID=1, outliving their
//               producer; 48 O_DIRECT+fsync writers; load 56.42.
//
// WHAT A HAND-WRITTEN GENERATOR GETS AND WHAT IT DOES NOT
// ------------------------------------------------------
//   CAUGHT   a file under the session's own worktree, tracked OR untracked,
//            including `internal/`, `scripts/`, `devdoc/`, `testdata/` and a dot
//            directory such as `.evid/` — i.e. exactly where 2026-09-16's
//            generator lived, when the test runs in that checkout.
//   CAUGHT   a script generated at run time into a temp path, provided the test
//            points the walk at it — the red proof does exactly that, so the
//            generated form is demonstrated rather than asserted.
//   REPORTED (not failed) a generator sitting in a SIBLING worktree/clone under
//            ~/workspace: it belongs to another agent's checkout, and failing
//            THIS repo's gate for a foreign file would be an unactionable red.
//            The ambient half prints what it found, bounded, on every run.
//   NOT CAUGHT a file written outside the worktree and outside ~/workspace, or
//            a generator that never names dd/fio/stress (e.g. a Go program
//            issuing O_DIRECT writes itself): a text gate cannot see those, and
//            that limit is stated in devdoc/heavy-tests-rule27-gate.md rather
//            than glossed.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	// rule27KindSyncWrite is the absolute-ban kind: a write asking for O_DIRECT
	// or an fsync per round. No bound makes it acceptable — that IS the
	// incident shape (2026-08-27, 2026-09-11, 2026-09-16).
	rule27KindSyncWrite = "device-sync-write"
	// rule27KindLoop is an unbounded loop whose body touches a device writer.
	rule27KindLoop = "device-write-in-unbounded-loop"
	// rule27KindRawDevice is a dd naming a raw block device.
	rule27KindRawDevice = "dd-raw-device"
	// rule27KindHelper is a committed synthetic-pressure/load-generator helper.
	rule27KindHelper = "synthetic-pressure-helper"
	// rule27KindHelperFile is the same helper recognised by ITS FILE NAME, so
	// renaming the file (or building the path at run time: `E/iopress.sh`) while
	// keeping the device-writing body cannot launder it. Content and name are
	// reported separately on purpose — a rename fixes neither.
	rule27KindHelperFile = "synthetic-pressure-helper-file"

	// rule27LoopWindow bounds the lookahead used to decide whether an unbounded
	// loop wraps a device writer. A shell loop body a static scan can follow is
	// far shorter than this; the window is a constant so the rule is tunable in
	// exactly one place (Rule 1a).
	rule27LoopWindow = 40
	// rule27ScanFileCap bounds the working-tree walk. A cap that is hit is
	// DISCLOSED (Rule 15: never a silent truncation that reads like a pass).
	rule27ScanFileCap = 6000
	// rule27AmbientDepth bounds the sibling-checkout scan (/home/nick/workspace is
	// a flat pile of ~150 worktrees, clones and probe dirs).
	rule27AmbientDepth = 2
	// rule27AmbientFileCap bounds that scan for the same reason.
	rule27AmbientFileCap = 3000
	// rule27WorkspaceDirName is the parent holding sibling checkouts.
	rule27WorkspaceDirName = "workspace"
)

// rule27DeviceWriteRes match a command that writes to a device or a file in a
// way that costs real device work.
var rule27DeviceWriteRes = []*regexp.Regexp{
	regexp.MustCompile(`\bdd\b`),
	regexp.MustCompile(`\bfio\b`),
	regexp.MustCompile(`\b(cp|tee|cat)\b[^|;]*>[^|;]*/dev/`),
}

// rule27SyncWriteRes match the O_DIRECT / fsync-per-round shape. Absolute ban.
var rule27SyncWriteRes = []*regexp.Regexp{
	regexp.MustCompile(`\boflag=direct\b`),
	regexp.MustCompile(`\bconv=(f?sync|fdatasync)\b`),
	regexp.MustCompile(`\b(direct|dsync)=1\b`),
}

// rule27RawDeviceRes match a dd naming a raw block device (the guard-probe
// shape: `dd if=/dev/zero of=/dev/sda`).
var rule27RawDeviceRes = []*regexp.Regexp{
	regexp.MustCompile(`\bdd\b[^|;]*\b(if|of)=/dev/(sd|hd|nvme|vd|mmcblk|disk)`),
}

// rule27HelperNameRes match a synthetic-pressure/load-generator helper by name —
// the class Rule 27 names outright. `iopress` is the 2026-09-16 script.
var rule27HelperNameRes = []*regexp.Regexp{
	regexp.MustCompile(`\b(stress-ng|stress|iopress|ioblitz|loadgen|load-generator|io-pressure|synthetic-pressure)\b`),
}

// rule27HelperFileRes match the same tokens in a FILE NAME. It is a separate
// pattern because `\b` treats `_` as a word character, so the content pattern
// would accept `stress_helper.sh` — and a rename is exactly the laundering route
// this half closes (`iopress.sh` was invoked as a run-time-built path, so its
// name lived in the call, not in a comment).
var rule27HelperFileRes = []*regexp.Regexp{
	regexp.MustCompile(`(^|[^a-z0-9])(stress-ng|stress|iopress|ioblitz|loadgen|load-generator|io-pressure|synthetic-pressure)($|[^a-z0-9])`),
}

// rule27GoTestExemptions are *_test.go files allowed to carry ONE KIND of
// pattern, with the reason written out. A file-level exemption would hide a new
// generator in the same file, so each entry names the KINDS it covers: a sync
// flag or an unbounded loop in these files still fails.
var rule27GoTestExemptions = map[string]struct {
	kinds  []string
	reason string
}{
	"internal/testutil/rule27_gate_test.go": {
		kinds: []string{rule27KindSyncWrite, rule27KindLoop, rule27KindRawDevice,
			rule27KindHelper},
		reason: "the detector itself, which must NAME every banned shape in its patterns and feed synthetic generator sources through its own predicate. Same precedent as the scripts audit's exemption. A generator cannot hide here unnoticed: this file IS the check.",
	},
	"internal/testutil/scripts_gate_test.go": {
		kinds:  []string{rule27KindHelper},
		reason: "the scripts audit's banned-shape table names `iopress|stress|loadgen` as PATTERNS and its red proofs feed synthetic corpora through them. A real generator here would still be reported for every other kind.",
	},
	"internal/tools/bash_test.go": {
		kinds:  []string{rule27KindRawDevice},
		reason: "the bash guard's own policy probes: `dd if=…/dev/sda of=/dev/null` and friends are inert STRING arguments the guard must REJECT, and the pseudo-device writes are bounded (count=1/500) into a temp dir with no sync flag. A sync flag, a loop, or a load helper in this file is still reported.",
	},
	"internal/tools/bash_path_policy_test.go": {
		kinds:  []string{rule27KindRawDevice},
		reason: "the declared-roots deny list is a table of inert command STRINGS (`dd if=/dev/zero of=/dev/sda`) that the policy must refuse; nothing here is executed.",
	},
}

// rule27ScriptExemptRels are the scripts allowed to NAME the banned shapes in
// prose. Comments are stripped before matching, so these are exempt only for
// their documentation — a real generator in either file still fails.
var rule27ScriptExemptRels = map[string]bool{
	"internal/testutil/scripts_gate_test.go": true,
	"scripts/lib/tmp-ban.sh":                 true,
	"scripts/lib/tmp_ban.py":                 true,
}

// rule27Source is one scanned file: its path relative to the walk root and its
// raw lines.
type rule27Source struct {
	rel   string
	lines []string
}

// rule27Hit is one finding, kept as data so the red proofs can assert on it
// without a *testing.T (a gate whose predicate is only reachable through
// t.Errorf cannot be red-proved).
type rule27Hit struct {
	rel     string
	line    int
	kind    string
	text    string
	exempt  bool
	whyExem string
}

// rule27FindIn returns every Rule 27 hit in one source file, exempting only the
// reviewed (file, kind) pairs. Comments are stripped per line before matching:
// both incidents are NAMED in the prose of the documents banning them, and a
// detector that cannot tell a quotation from a command gets relaxed to a no-op
// the first time it fires on documentation (the argv-check precedent).
func rule27FindIn(src rule27Source, exemptions map[string]struct {
	kinds  []string
	reason string
}) []rule27Hit {
	var hits []rule27Hit
	exemptKinds := map[string]string{}
	if ex, ok := exemptions[src.rel]; ok {
		for _, k := range ex.kinds {
			exemptKinds[k] = ex.reason
		}
	}
	report := func(i int, kind, text string) {
		h := rule27Hit{rel: src.rel, line: i + 1, kind: kind, text: strings.TrimSpace(text)}
		if why, ok := exemptKinds[kind]; ok {
			h.exempt, h.whyExem = true, why
		}
		hits = append(hits, h)
	}
	// A committed helper is recognised by its FILE NAME before any line is
	// looked at: the 2026-09-16 generator was invoked as `"$E/iopress.sh"`, i.e.
	// by a path built at run time, and a name-only-in-path helper whose body
	// writes to a device is the same defect under a fresh file name.
	if matchesAny(rule27HelperFileRes, filepath.Base(src.rel)) {
		report(0, rule27KindHelperFile, filepath.Base(src.rel))
	}
	for i, line := range src.lines {
		trimmed := strings.TrimSpace(stripComment(line))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// The absolute ban first: a sync/O_DIRECT writer is a generator whether
		// or not a loop contains it (iopress.sh's loop was bounded).
		if matchesAny(rule27SyncWriteRes, trimmed) {
			report(i, rule27KindSyncWrite, line)
		}
		// A committed helper is recognised by NAME and by its inner dd alike:
		// both are reported, so the fix cannot be a rename (a `while :; do dd`
		// loop with a friendlier filename is the same defect).
		if matchesAny(rule27HelperNameRes, trimmed) {
			report(i, rule27KindHelper, line)
		}
		if matchesAny(rule27RawDeviceRes, trimmed) {
			report(i, rule27KindRawDevice, line)
		}
		if loopIndex := rule27UnboundedLoopAt(src.lines, i); loopIndex >= 0 {
			if rule27LoopWrapsDeviceWrite(src.lines, loopIndex) {
				report(i, rule27KindLoop, line)
			}
		}
	}
	return hits
}

// rule27UnboundedLoopAt reports the header index of the unbounded loop starting
// at i, or -1. It reuses the scripts audit's own loop vocabulary so the two
// halves of this ban cannot disagree about what "unbounded" means.
func rule27UnboundedLoopAt(lines []string, i int) int {
	if matchesAny(unboundedLoopRes, lines[i]) {
		return i
	}
	return -1
}

// rule27LoopWrapsDeviceWrite walks the loop body (bounded window, nesting
// tracked by the same `done`/`fi` block rules the audit uses) and reports
// whether any line in it invokes a device writer.
func rule27LoopWrapsDeviceWrite(lines []string, headerIdx int) bool {
	depth := 0
	for i := headerIdx + 1; i < len(lines) && i < headerIdx+rule27LoopWindow; i++ {
		trimmed := strings.TrimSpace(stripComment(lines[i]))
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "while ") || strings.HasPrefix(trimmed, "until ") ||
			strings.HasPrefix(trimmed, "for ") {
			depth++
		}
		if trimmed == "done" || strings.HasPrefix(trimmed, "done ") {
			if depth == 0 {
				return false // the loop closed with no device writer inside
			}
			depth--
		}
		if matchesAny(rule27DeviceWriteRes, trimmed) {
			return true
		}
	}
	return false
}

// rule27Collect walks root and returns every *.sh/*.py file (includeHidden
// decides whether dot-directories such as .evid/ are entered) plus whether the
// file cap truncated the walk — a truncated walk must be disclosed, never
// silently reported as clean.
func rule27Collect(root string, includeHidden bool, maxDepth, cap int) (files []rule27Source, scannedAll bool) {
	baseDepth := strings.Count(filepath.Clean(root), string(filepath.Separator))
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // an unreadable entry is skipped, not fatal (best-effort walk)
		}
		if info.IsDir() {
			name := info.Name()
			if path != root && !includeHidden && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			if maxDepth > 0 && strings.Count(filepath.Clean(path), string(filepath.Separator))-baseDepth >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".sh") && !strings.HasSuffix(path, ".py") {
			return nil
		}
		if len(files) >= cap {
			return filepath.SkipAll
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		files = append(files, rule27Source{rel: filepath.ToSlash(rel), lines: strings.Split(string(raw), "\n")})
		return nil
	})
	if walkErr != nil && walkErr != filepath.SkipAll {
		return files, false
	}
	scannedAll = len(files) < cap
	return files, scannedAll
}

// TestRule27NoDeviceLoadGenerators is the gate: the working tree (untracked and
// dot-directories included — where the 2026-09-16 generator actually lived) is
// scanned for Rule 27 shapes, and the sibling-checkout neighbourhood is
// REPORTED so a generator parked next to a session (the same incident's other
// hiding place) is visible on every run.
func TestRule27NoDeviceLoadGenerators(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}

	files, scannedAll := rule27Collect(root, true, 0, rule27ScanFileCap)
	if len(files) < gateMinScriptFiles {
		t.Fatalf("the Rule 27 walk found %d scripts (want at least %d) — the scan is "+
			"not looking where the scripts live (vacuous-pass guard)", len(files), gateMinScriptFiles)
	}
	if !scannedAll {
		t.Errorf("the Rule 27 walk stopped at its cap of %d scripts — the remainder was "+
			"NOT scanned, so a generator could sit in the unchecked tail (Rule 15: a "+
			"truncated scan must not read as a clean one)", rule27ScanFileCap)
	}
	reported, withheld := 0, 0
	for _, f := range files {
		if rule27ScriptExemptRels[f.rel] {
			continue
		}
		for _, h := range rule27FindIn(f, rule27GoTestExemptions) {
			if h.exempt {
				continue
			}
			reported++
			if reported > gateMaxReportedHits {
				withheld++
				continue
			}
			t.Errorf("%s:%d Rule 27 device-load generator [%s]: %q — a test may never "+
				"produce a number by making the shared device sick. Measure the operation "+
				"under test (its own duration) instead; if a contention-dependent case is "+
				"genuinely needed it must be a checked-in, capped, opt-in test, never a "+
				"hand-written helper (2026-08-27, 2026-09-11, 2026-09-16 were all this shape)",
				h.rel, h.line, h.kind, h.text)
		}
	}
	assertReportedAllHits(t, "working-tree ("+filepath.Base(root)+")", reported, withheld)

	rule27ScanGoTestSources(t, root)
	rule27ReportAmbientGenerators(t, root)
}

// rule27ScanGoTestSources is the Go half of the ban: a device-load generator
// hidden inside a test source (a Bash(...) helper call, a generated script
// written from Go) is exactly as banned as one in a shell script.
func rule27ScanGoTestSources(t *testing.T, root string) {
	t.Helper()
	for _, rel := range goTestFilesUnder(t, root) {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		src := rule27Source{rel: rel, lines: rule27StripGoComments(strings.Split(string(raw), "\n"))}
		reported, withheld := 0, 0
		for _, h := range rule27FindIn(src, rule27GoTestExemptions) {
			if h.exempt {
				continue
			}
			reported++
			if reported > gateMaxReportedHits {
				withheld++
				continue
			}
			t.Errorf("%s:%d Rule 27 device-load generator [%s] in a test source: %q — "+
				"drive the code under test, not the disk", h.rel, h.line, h.kind, h.text)
		}
		assertReportedAllHits(t, rel, reported, withheld)
	}
}

// goTestFilesUnder returns repo-relative paths of every *_test.go below the
// module root, dot-directories skipped (a vendored or cached copy is not this
// repo's test source).
func goTestFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s for test sources: %v", root, walkErr)
	}
	sort.Strings(out)
	return out
}

// rule27ReportAmbientGenerators prints what a bounded walk of the sibling
// checkouts (~/workspace/<repo>) finds. It does not fail: the hit belongs to
// another agent's checkout, and reddening THIS repo's gate for a foreign file
// would be an unactionable red. It is printed anyway because the 2026-09-16
// generator lived in exactly that neighbourhood (`gopi-firstopen/.evid/`) and
// the failure mode was that nobody looked.
func rule27ReportAmbientGenerators(t *testing.T, root string) {
	t.Helper()
	workspace := filepath.Join(filepath.Dir(filepath.Dir(root)), rule27WorkspaceDirName)
	if fi, err := os.Stat(workspace); err != nil || !fi.IsDir() {
		t.Logf("Rule 27 ambient scan: %s is absent — skipped (nothing to look at)", workspace)
		return
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Logf("Rule 27 ambient scan: cannot read %s: %v", workspace, err)
		return
	}
	var hits []string
	files, scannedAll := 0, true
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(workspace, e.Name())
		if samePath(dir, root) {
			continue // this session's own worktree is already fully scanned above
		}
		collected, full := rule27Collect(dir, true, rule27AmbientDepth, rule27AmbientFileCap-files)
		files += len(collected)
		if !full {
			scannedAll = false
		}
		for _, f := range collected {
			for _, h := range rule27FindIn(f, nil) {
				if h.kind != rule27KindSyncWrite && h.kind != rule27KindLoop && h.kind != rule27KindHelper {
					continue // ambient: only the unambiguous generator shapes
				}
				hits = append(hits, filepath.Join(e.Name(), f.rel)+":"+strconv.Itoa(h.line)+" ["+h.kind+"] "+h.text)
			}
		}
	}
	sort.Strings(hits)
	t.Logf("Rule 27 ambient scan: %d sibling checkout(s) walked, %d script(s), %d generator "+
		"shape(s) found, %d file(s) unchecked before the cap", len(entries), files, len(hits), rule27AmbientFileCap-files)
	for _, h := range hits {
		t.Logf("Rule 27 AMBIENT GENERATOR (sibling checkout — report to its owner): %s", h)
	}
	if !scannedAll {
		t.Logf("Rule 27 ambient scan: cap reached — the listed %d file(s) are the CHECKED part only", files)
	}
}

// samePath reports whether two paths denote the same directory (symlinks and
// ../ resolved), so the ambient scan never re-reports this session's own tree.
func samePath(a, b string) bool {
	ra, erra := filepath.EvalSymlinks(a)
	rb, errb := filepath.EvalSymlinks(b)
	if erra != nil || errb != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// ---------------------------------------------------------------------------
// red proofs — a gate that cannot be shown failing is a gate nobody can trust.
// ---------------------------------------------------------------------------

// rule27ProbeFind runs the predicate over in-memory source, standing in for a
// file at rel so exemptions can be exercised too.
func rule27ProbeFind(rel, src string) []rule27Hit {
	return rule27FindIn(rule27Source{rel: rel, lines: strings.Split(src, "\n")}, rule27GoTestExemptions)
}

// TestRule27SyncWriteRedProof plants the exact incident shape (O_DIRECT+fsync
// inside an unbounded loop) and asserts both kinds are reported — and that a
// bounded, unsynced write into a temp file is NOT.
func TestRule27SyncWriteRedProof(t *testing.T) {
	generator := `#!/usr/bin/env bash
# io pressure generator
dir=$1
while :; do
  dd if=/dev/zero of=$dir/blk bs=1M count=48 oflag=direct conv=fsync 2>/dev/null
  rm -f $dir/blk
done
`
	hits := rule27ProbeFind("probe-generator.sh", generator)
	kinds := map[string]int{}
	for _, h := range hits {
		kinds[h.kind]++
	}
	if kinds[rule27KindSyncWrite] == 0 {
		t.Errorf("the O_DIRECT+conv=fsync writer was NOT reported — the 2026-08-27/09-11/09-16 "+
			"shape must always fail (hits: %+v)", hits)
	}
	if kinds[rule27KindLoop] == 0 {
		t.Errorf("the unbounded `while :; do … done` wrapping the device writer was NOT reported "+
			"(hits: %+v)", hits)
	}

	compliant := `#!/usr/bin/env bash
tmp=$(mktemp -d)
for i in 1 2 3; do dd if=/dev/zero of=$tmp/probe.bin bs=1024 count=8; done
`
	if got := rule27ProbeFind("probe-ok.sh", compliant); len(got) != 0 {
		t.Errorf("a bounded, unsynced write into a temp dir was reported (%+v) — the gate would "+
			"punish ordinary file work and get relaxed to a no-op", got)
	}
}

// TestRule27GeneratedScriptIsCaughtRedProof is the "generated script" half: a
// generator written AT RUN TIME into a path outside the index must be found by
// the walk. This is the argv-check lesson made mechanical — the previous check
// reported PASS on the shape it could not see, so coverage of the generated form
// is DEMONSTRATED here rather than asserted in prose.
func TestRule27GeneratedScriptIsCaughtRedProof(t *testing.T) {
	dir := t.TempDir()
	generated := filepath.Join(dir, "iopress.sh")
	body := "#!/usr/bin/env bash\n" +
		"# synthetic io pressure for the epic's acceptance condition\n" +
		"trap 'pkill -f iopress.sh' EXIT\n" +
		"for ((i=0;i<4000;i++)); do\n" +
		"  dd if=/dev/zero of=w$i bs=1M count=48 oflag=direct conv=fsync\n" +
		"done\n"
	if err := os.WriteFile(generated, []byte(body), 0o755); err != nil {
		t.Fatalf("plant the generated script: %v", err)
	}
	files, scannedAll := rule27Collect(dir, true, 0, rule27ScanFileCap)
	if !scannedAll || len(files) != 1 {
		t.Fatalf("the walk did not find the generated script (files=%d scannedAll=%v)", len(files), scannedAll)
	}
	hits := rule27FindIn(files[0], nil)
	found := false
	for _, h := range hits {
		if h.kind == rule27KindSyncWrite {
			found = true
		}
	}
	if !found {
		t.Fatalf("a generator written at run time outside the index was NOT reported (hits: %+v) — "+
			"the walk must cover the working tree, not just tracked files", hits)
	}
	// And the same script proves the KIND of the helper-name rule fires too.
	named := false
	for _, h := range hits {
		if h.kind == rule27KindHelper {
			named = true
		}
	}
	if !named {
		t.Errorf("the committed helper was not recognised by name (iopress/stress/loadgen): %+v", hits)
	}
	// The RENAME half: a helper whose device-writing loop is gone but whose FILE
	// NAME is still a load-generator's is reported by name alone — a rename is
	// not a fix, and a body that no longer names dd must not read as clean.
	inertName := "stress_helper.sh"
	if err := os.WriteFile(filepath.Join(dir, inertName), []byte("#!/bin/sh\necho warming up\n"), 0o755); err != nil {
		t.Fatalf("plant the renamed helper: %v", err)
	}
	files2, _ := rule27Collect(dir, true, 0, rule27ScanFileCap)
	byName := false
	for _, f := range files2 {
		if filepath.Base(f.rel) != inertName {
			continue
		}
		for _, h := range rule27FindIn(f, nil) {
			if h.kind == rule27KindHelperFile {
				byName = true
			}
		}
	}
	if !byName {
		t.Errorf("a load-generator helper was NOT reported by its file name (%s): the allow-list "+
			"would accept a rename as a fix", inertName)
	}
}

// TestRule27ExemptionsAreNarrowAndReal pins the two properties that keep the
// allow-list honest: every exempted file exists and carries the pattern it is
// exempted for (a stale exemption is a silent permission), and an exemption of
// ONE kind does not cover another.
func TestRule27ExemptionsAreNarrowAndReal(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	for rel, ex := range rule27GoTestExemptions {
		raw, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			t.Errorf("%s is exempted (%q) but cannot be read: %v — a stale exemption hides the rule",
				rel, ex.reason, rerr)
			continue
		}
		for _, kind := range ex.kinds {
			if !rule27FileCarriesKind(rule27StripGoComments(strings.Split(string(raw), "\n")), kind) {
				t.Errorf("%s is exempted for %s (%q) but no longer carries it — delete the exemption "+
					"so the allow-list keeps ratcheting down", rel, kind, ex.reason)
			}
		}
	}
	// Narrowness: the same kind must still be reported for a file with no
	// exemption, and a NON-exempt kind must still be reported inside an exempt
	// file (otherwise the exemption is a file-level hole).
	sync := "dd if=/dev/zero of=/dev/sda oflag=direct\n"
	hits := rule27ProbeFind("internal/tools/bash_test.go", sync)
	reportable := false
	for _, h := range hits {
		if h.kind == rule27KindSyncWrite && !h.exempt {
			reportable = true
		}
	}
	if !reportable {
		t.Errorf("an exempt file's exemption swallowed a %s hit — the exemption must be keyed by "+
			"(file, kind), never by file alone (hits: %+v)", rule27KindSyncWrite, hits)
	}
	raw := "dd if=/dev/zero of=/dev/sda\n"
	var exempted bool
	for _, h := range rule27ProbeFind("internal/tools/bash_test.go", raw) {
		if h.kind == rule27KindRawDevice && h.exempt {
			exempted = true
		}
	}
	if !exempted {
		t.Errorf("the reviewed raw-device probe exemption no longer applies to its own file — " +
			"the gate would red the guard's own deny-list test")
	}
}

// rule27StripGoComments blanks Go comments so a doc line quoting a past
// generator is not mistaken for one. It is line-oriented and deliberately
// simple: a `//` INSIDE a string literal is not special-cased, which can only
// keep a comment visible (a false POSITIVE), never hide a real generator.
func rule27StripGoComments(lines []string) []string {
	out := make([]string, len(lines))
	inBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case inBlock:
			if end := strings.Index(line, "*/"); end >= 0 {
				inBlock = false
				line = line[end+len("*/"):]
			} else {
				out[i] = ""
				continue
			}
			trimmed = strings.TrimSpace(line)
		case strings.HasPrefix(trimmed, "/*"):
			if end := strings.Index(trimmed, "*/"); end > 2 {
				line = trimmed[end+len("*/"):]
				trimmed = strings.TrimSpace(line)
			} else {
				inBlock = true
				out[i] = ""
				continue
			}
		}
		out[i] = stripComment(line)
	}
	return out
}

// rule27FileCarriesKind reports whether a file's lines still carry the pattern
// behind kind — used to keep an exemption from outliving the code it explains.
func rule27FileCarriesKind(lines []string, kind string) bool {
	for i := range lines {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		switch kind {
		case rule27KindRawDevice:
			if matchesAny(rule27RawDeviceRes, trimmed) {
				return true
			}
		case rule27KindSyncWrite:
			if matchesAny(rule27SyncWriteRes, trimmed) {
				return true
			}
		case rule27KindHelper:
			if matchesAny(rule27HelperNameRes, trimmed) {
				return true
			}
		case rule27KindLoop:
			if matchesAny(rule27DeviceWriteRes, trimmed) {
				return true
			}
		}
	}
	return false
}
