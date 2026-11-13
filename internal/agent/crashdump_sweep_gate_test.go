// crashdump_sweep_gate_test.go — the WHEN of the orphan sweep (see
// crashdump_sweep_gate.go). Three acceptance properties:
//
//  1. The verdict table: every uncertainty in the marker FAILS OPEN to a walk
//     (absent / unreadable / truncated / wrong version / foreign root /
//     future-dated), and the fast path SKIPS only for a marker whose change
//     evidence still matches the tree.
//  2. The COST proof, by COUNTER: while the fast path is valid, an agent start
//     does not walk the sessions tree at all — proven END-TO-END by running
//     the real binary over a sandbox tree holding an orphan whose dump does not
//     exist yet (a stamped-valid marker must leave it undumped; a tree touched
//     after the marker must dump it). The observation is the sweep's own
//     product (a dump file), never a timing threshold.
//  3. The epic's HEADLINE acceptance: a session orphaned AFTER the marker is
//     detected on the NEXT start — the property the retired day-key gate could
//     not satisfy, because a same-day stamp skipped the walk outright.
//
// Plus the bounded-cadence self-heal: a marker that is intact but older than
// the full-walk interval is DUE, so no wrong marker can switch the sweep off
// for longer than that interval.

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/testutil"
)

// crashDumpFixtureDeadPID is a PID no process can hold (the Linux pid_max
// ceiling + 1), so the classifier's liveness clause reads it GONE — the
// fixture must be a PROVABLE crash, not a merely end-less file.
const crashDumpFixtureDeadPID = 4194305

// gateFixture is one sandbox home with a sessions tree, the marker path, and
// helpers to drive the REAL writer/reader pair.
type gateFixture struct {
	t           *testing.T
	home        string
	gopiDir     string
	sessionsDir string
	root        string
	marker      string
}

func newGateFixture(t *testing.T) *gateFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	gopiDir := filepath.Join(home, ".gopi")
	sessionsDir := filepath.Join(gopiDir, config.SessionsDirName)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &gateFixture{
		t:           t,
		home:        home,
		gopiDir:     gopiDir,
		sessionsDir: sessionsDir,
		root:        sessionsDir,
		marker:      filepath.Join(gopiDir, crashDumpSweepMarkerName),
	}
}

// writeOrphan plants an end-less, dead-pid-stamped JSONL (a provable crash)
// and backdates it, because a freshly written end-less JSONL is inside the
// orphanLiveWindow quiet period and would be read as "still being written" —
// the fixture must be a crash under either semantics.
func (f *gateFixture) writeOrphan(id string) string {
	f.t.Helper()
	p := filepath.Join(f.sessionsDir, id+sessionJSONLSuffix)
	line := fmt.Sprintf(`{"type":%q,"%s":%d}`+"\n",
		sessionevent.EventTypeSessionStart, ProcessAliveStampField, crashDumpFixtureDeadPID)
	if err := os.WriteFile(p, []byte(line), 0o644); err != nil {
		f.t.Fatal(err)
	}
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		f.t.Fatal(err)
	}
	return p
}

// writeSessionDir plants the per-session DIRECTORY shape
// (<root>/<id>/session.jsonl) — a name added at the root, which is what the
// change probe reads. Distinct from writeOrphan's flat-file shape on purpose:
// both are production layouts (see DetectOrphanSessions).
func (f *gateFixture) writeSessionDir(id string) string {
	f.t.Helper()
	dir := filepath.Join(f.sessionsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	return dir
}

func (f *gateFixture) dumpPathFor(jsonl string) string {
	return strings.TrimSuffix(jsonl, sessionJSONLSuffix) + dumpFileSuffix
}

// countDumps counts the crash dumps actually present under the sandbox tree —
// the sweep's own product, so an assertion on it needs no timer.
func (f *gateFixture) countDumps() int {
	f.t.Helper()
	n := 0
	err := filepath.WalkDir(f.sessionsDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, dumpFileSuffix) {
			n++
		}
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestOrphanSweepDue_VerdictTableFailsOpenOnEveryUncertainty pins the whole
// decision surface. The gate is the only thing between a gopi start and a
// full-tree walk, so a marker defect must be visible as a DUE verdict with a
// specific reason — never as a quiet skip.
func TestOrphanSweepDue_VerdictTableFailsOpenOnEveryUncertainty(t *testing.T) {
	f := newGateFixture(t)
	noon := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

	// 1. No marker at all (first start on this host) → due.
	if due, reason := OrphanSweepDue(noon); !due || reason != crashDumpSweepReasonNoMarker {
		t.Errorf("no marker: due=%v reason=%q, want true/%q", due, reason, crashDumpSweepReasonNoMarker)
	}

	// 2. A completed sweep writes a marker that a start inside the budget may
	// trust: the ONE skip in this table, and it must round-trip through the
	// REAL writer (a marker that never round-trips restores the every-start
	// walk silently).
	if err := MarkOrphanSweepDone(noon); err != nil {
		t.Fatalf("MarkOrphanSweepDone: %v", err)
	}
	if due, reason := OrphanSweepDue(noon.Add(time.Second)); due || reason != crashDumpSweepReasonFresh {
		t.Errorf("fresh marker: due=%v reason=%q, want false/%q", due, reason, crashDumpSweepReasonFresh)
	}

	// 3. A TREE TOUCH forces the walk even inside the budget — the change
	// evidence outranks the budget. The touch is a NEW SESSION DIRECTORY at
	// the root (the production shape of a startup kill: the dir appears, the
	// JSONL inside is never written), i.e. a change to the root's name set.
	f.writeSessionDir("bbbbbbbb-0000-4000-8000-000000000002")
	// The parent-directory timestamp is published LAZILY on this filesystem
	// (measured ~100 ms): a probe read microseconds after the mkdir can still
	// see the old pair. POLL for the change instead of sleeping a fixed
	// interval — the assertion is "a namespace change becomes visible", and
	// polling both proves it sooner and does not silently pass when the
	// machine is slower than the chosen constant.
	awaitTreeChangeVisible(t, f, noon.Add(2*time.Second))
	if due, reason := OrphanSweepDue(noon.Add(2 * time.Second)); !due || reason != crashDumpSweepReasonChanged {
		t.Errorf("tree changed: due=%v reason=%q, want true/%q", due, reason, crashDumpSweepReasonChanged)
	}

	// 4. After re-stamping, the budget is back but the FLOOR still bounds it:
	// past the full-walk interval the gate walks regardless of evidence.
	if err := MarkOrphanSweepDone(noon); err != nil {
		t.Fatalf("MarkOrphanSweepDone (re-stamp): %v", err)
	}
	if due, reason := OrphanSweepDue(noon.Add(crashDumpSweepFullWalkInterval)); !due || reason != crashDumpSweepReasonFloorDue {
		t.Errorf("floor elapsed: due=%v reason=%q, want true/%q", due, reason, crashDumpSweepReasonFloorDue)
	}

	// 5. force → due regardless of a perfectly valid marker.
	t.Setenv(crashDumpSweepEnvName, crashDumpSweepEnvForce)
	if due, reason := OrphanSweepDue(noon.Add(time.Second)); !due || reason != CrashDumpSweepReasonForced {
		t.Errorf("force: due=%v reason=%q, want true/%q", due, reason, CrashDumpSweepReasonForced)
	}

	// 6. off → never due, even with no marker at all.
	t.Setenv(crashDumpSweepEnvName, crashDumpSweepEnvOff)
	if err := os.Remove(f.marker); err != nil {
		t.Fatal(err)
	}
	if due, reason := OrphanSweepDue(noon); due || reason != crashDumpSweepReasonDisabled {
		t.Errorf("off: due=%v reason=%q, want false/%q", due, reason, crashDumpSweepReasonDisabled)
	}
	t.Setenv(crashDumpSweepEnvName, "")
}

// TestOrphanSweepDue_UnreadableOrInvalidMarkerIsDue walks the stale/broken
// marker paths one by one. Each must WALK: a marker that cannot be believed
// must never be able to suppress the sweep (this is the fail-closed property
// the whole design turns on).
func TestOrphanSweepDue_UnreadableOrInvalidMarkerIsDue(t *testing.T) {
	valid := func(f *gateFixture, at time.Time) string {
		f.t.Helper()
		if err := MarkOrphanSweepDone(at); err != nil {
			f.t.Fatal(err)
		}
		raw, err := os.ReadFile(f.marker)
		if err != nil {
			f.t.Fatal(err)
		}
		return string(raw)
	}
	noon := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		breakIt    func(f *gateFixture, body string)
		wantReason string
	}{
		{
			name: "unreadable (a directory sits at the marker path)",
			breakIt: func(f *gateFixture, _ string) {
				if err := os.Remove(f.marker); err != nil {
					f.t.Fatal(err)
				}
				if err := os.MkdirAll(f.marker, crashDumpSweepDirPerm); err != nil {
					f.t.Fatal(err)
				}
			},
			wantReason: crashDumpSweepReasonUnreadable,
		},
		{
			name: "truncated by a crash mid-write (no probe line survives)",
			breakIt: func(f *gateFixture, body string) {
				cut := strings.Index(body, crashDumpSweepMarkerFieldProbe+"=")
				if cut <= 0 {
					f.t.Fatalf("marker body has no %s line to cut at:\n%s",
						crashDumpSweepMarkerFieldProbe, body)
				}
				if err := os.WriteFile(f.marker, []byte(body[:cut]), crashDumpSweepFilePerm); err != nil {
					f.t.Fatal(err)
				}
			},
			wantReason: crashDumpSweepReasonUnreadable,
		},
		{
			name: "probe value is garbage (hand-edited / partially written)",
			breakIt: func(f *gateFixture, body string) {
				broken := replaceMarkerField(t, body, crashDumpSweepMarkerFieldProbe, "not,a,number")
				if err := os.WriteFile(f.marker, []byte(broken), crashDumpSweepFilePerm); err != nil {
					f.t.Fatal(err)
				}
			},
			wantReason: crashDumpSweepReasonUnreadable,
		},
		{
			name: "empty file",
			breakIt: func(f *gateFixture, _ string) {
				if err := os.WriteFile(f.marker, nil, crashDumpSweepFilePerm); err != nil {
					f.t.Fatal(err)
				}
			},
			wantReason: crashDumpSweepReasonUnreadable,
		},
		{
			name: "foreign version (written by a future gopi)",
			breakIt: func(f *gateFixture, body string) {
				broken := strings.Replace(body,
					crashDumpSweepMarkerFieldVersion+"="+crashDumpSweepMarkerVersion,
					crashDumpSweepMarkerFieldVersion+"=99", 1)
				if err := os.WriteFile(f.marker, []byte(broken), crashDumpSweepFilePerm); err != nil {
					f.t.Fatal(err)
				}
			},
			wantReason: crashDumpSweepReasonUnreadable,
		},
		{
			name: "unparseable timestamp (clock garbage)",
			breakIt: func(f *gateFixture, body string) {
				broken := replaceMarkerField(t, body, crashDumpSweepMarkerFieldSweptAt, "not-a-time")
				if err := os.WriteFile(f.marker, []byte(broken), crashDumpSweepFilePerm); err != nil {
					f.t.Fatal(err)
				}
			},
			wantReason: crashDumpSweepReasonUnreadable,
		},
		{
			name: "marker describes a DIFFERENT data root (copied state)",
			breakIt: func(f *gateFixture, body string) {
				broken := replaceMarkerField(t, body, crashDumpSweepMarkerFieldRoot, "/some/other/root")
				if err := os.WriteFile(f.marker, []byte(broken), crashDumpSweepFilePerm); err != nil {
					f.t.Fatal(err)
				}
			},
			wantReason: crashDumpSweepReasonForeign,
		},
		{
			name: "marker dated in the future (clock skew backwards)",
			breakIt: func(f *gateFixture, _ string) {
				if err := MarkOrphanSweepDone(noon.Add(48 * time.Hour)); err != nil {
					f.t.Fatal(err)
				}
			},
			wantReason: crashDumpSweepReasonFutureDated,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newGateFixture(t)
			body := valid(f, noon)
			tc.breakIt(f, body)
			due, reason := OrphanSweepDue(noon.Add(time.Second))
			if !due {
				t.Fatalf("due=false (reason=%q) — a marker that cannot be believed must WALK, never skip", reason)
			}
			if reason != tc.wantReason {
				t.Errorf("reason=%q, want %q", reason, tc.wantReason)
			}
		})
	}
}

// replaceMarkerField rewrites one `key=value` line of a marker body.
func replaceMarkerField(t *testing.T, body, key, value string) string {
	t.Helper()
	lines := strings.Split(body, "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			lines[i] = key + "=" + value
			found = true
		}
	}
	if !found {
		t.Fatalf("marker body has no %s line:\n%s", key, body)
	}
	return strings.Join(lines, "\n")
}

// TestOrphanSweepDue_UnstattableRootIsDue: with no sessions tree at all there
// is no evidence to skip on, so the verdict is DUE (and the sweep reports
// nothing). A skip here would be the silent no-sweep failure.
func TestOrphanSweepDue_UnstattableRootIsDue(t *testing.T) {
	f := newGateFixture(t)
	noon := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	if err := MarkOrphanSweepDone(noon); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.sessionsDir); err != nil {
		t.Fatal(err)
	}
	due, reason := OrphanSweepDue(noon.Add(time.Second))
	if !due || reason != crashDumpSweepReasonRootUnknown {
		t.Errorf("missing tree: due=%v reason=%q, want true/%q", due, reason, crashDumpSweepReasonRootUnknown)
	}
}

// TestMarkOrphanSweepDone_ReportsUnwritableMarker: the write error must come
// back to the caller (Rule 15) instead of being swallowed, so the startup path
// can tell the operator the next start will sweep again.
func TestMarkOrphanSweepDone_ReportsUnwritableMarker(t *testing.T) {
	f := newGateFixture(t)
	// A DIRECTORY at the marker path makes the atomic rename fail.
	if err := os.MkdirAll(f.marker, crashDumpSweepDirPerm); err != nil {
		t.Fatal(err)
	}
	err := MarkOrphanSweepDone(time.Now())
	if err == nil {
		t.Fatal("MarkOrphanSweepDone returned nil for an uninstallable marker — an unreportable failure")
	}
	if !strings.Contains(err.Error(), crashDumpSweepMarkerName) {
		t.Errorf("error %q does not name the marker %q", err, crashDumpSweepMarkerName)
	}
	// And the tree keeps only the offending directory: no stray temp files.
	entries, rerr := os.ReadDir(f.gopiDir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), crashDumpSweepMarkerName) && e.Name() != crashDumpSweepMarkerName {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// TestStartupGate_SkipsOnlyWhenTheMarkerProvesNoChange is the COST proof on
// the real startup path, asserted by COUNTER: run the actual binary in the
// bare-prompt form (the only form that reaches main's startup sweep) against a
// sandbox tree holding an orphan, and observe the sweep's own product.
//
//	phase 1 — a valid marker and an untouched tree: 0 dumps, no walk.
//	phase 2 — the SAME marker, but the tree touched after it: the walk runs,
//	          so the orphan is dumped. This is the epic's headline acceptance:
//	          a session orphaned after the marker is detected on the NEXT
//	          start, which a day-key stamp could not do.
func TestStartupGate_SkipsOnlyWhenTheMarkerProvesNoChange(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the real binary twice")
	}
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "phase answer"})
	defer llm.Close()

	f := newGateFixture(t)
	env := testutil.MockEnv(t, f.home, llm.URL())

	// The tree is created FIRST, so the marker records the state the sweep
	// actually saw (nothing orphaned yet) — an empty tree, exactly like a
	// host whose last sweep found nothing.
	if err := MarkOrphanSweepDone(time.Now()); err != nil {
		t.Fatal(err)
	}

	// Phase 1 — nothing has changed since the marker: every start inside the
	// budget must skip the walk entirely. Run it TWICE to prove the skip is
	// repeatable (not a one-shot side effect of the first run).
	for i := 0; i < 2; i++ {
		if _, stderr, code := testutil.RunGopi(t, []string{"say ok"}, "", env, f.home); code != 0 {
			t.Fatalf("stamped run #%d exit=%d stderr=%s", i+1, code, stderr)
		}
		if n := f.countDumps(); n != 0 {
			t.Fatalf("run #%d wrote %d dump(s) although the marker proves no change — the walk is not gated", i+1, n)
		}
	}

	// Phase 2 — a session orphans AFTER the marker (its JSONL appears in the
	// tree), the marker is untouched, and the NEXT start must walk and dump
	// it. No re-stamp, no env override, no timer: this is the change signal
	// alone doing the work.
	orphan := f.writeOrphan("cccccccc-0000-4000-8000-000000000003")
	if _, stderr, code := testutil.RunGopi(t, []string{"say ok"}, "", env, f.home); code != 0 {
		t.Fatalf("post-change run exit=%d stderr=%s", code, stderr)
	}
	dump := f.dumpPathFor(orphan)
	if _, err := os.Stat(dump); err != nil {
		t.Fatalf("a session orphaned after the marker was NOT detected on the next start (%v) — headline acceptance broken", err)
	}
	if n := f.countDumps(); n != 1 {
		t.Errorf("dumps=%d, want exactly 1 (the newly orphaned session)", n)
	}
}

// TestStartupGate_SkipsTheWalkWhileMarkerFresh is the ORIGINAL cost proof,
// kept and re-pointed at the new marker: with a valid marker a start writes no
// dump at all even though the predicate would flag the orphan, and with a
// floor-expired marker the same orphan is dumped. It guards the regression
// that would silently restore a per-start walk.
func TestStartupGate_SkipsTheWalkWhileMarkerFresh(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the real binary twice")
	}
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: "phase answer"})
	defer llm.Close()

	f := newGateFixture(t)
	env := testutil.MockEnv(t, f.home, llm.URL())

	// A pre-existing orphan (a crash from an EARLIER gopi run) plus a marker
	// that records it as already seen: the sweep ran when that file already
	// existed, so it must not be re-dumped on a warm start.
	orphan := f.writeOrphan("aaaaaaaa-0000-4000-8000-000000000001")
	orphanDump := f.dumpPathFor(orphan)
	if err := os.WriteFile(orphanDump, []byte("{}\n"), crashDumpSweepFilePerm); err != nil {
		t.Fatal(err)
	}
	if err := MarkOrphanSweepDone(time.Now()); err != nil {
		t.Fatal(err)
	}

	// Phase 1 — marker valid, tree untouched: the walk must NOT run. Observe
	// it by census: a walk would have written a dump for the second orphan
	// planted below... which is exactly why the census is taken with a
	// FRESH-but-matching marker and an untouched tree: the only dump present
	// is the pre-existing one.
	if _, stderr, code := testutil.RunGopi(t, []string{"say ok"}, "", env, f.home); code != 0 {
		t.Fatalf("prompt run (fresh marker) exit=%d stderr=%s", code, stderr)
	}
	if n := f.countDumps(); n != 1 {
		t.Errorf("fresh marker: dumps=%d, want 1 (the pre-existing dump only) — a walk ran when it must not", n)
	}

	// Phase 2 — expire the floor: the marker is intact and the tree is
	// untouched, so ONLY the bounded cadence can make it walk. That is the
	// self-heal path, and it must actually sweep.
	if err := MarkOrphanSweepDone(time.Now().Add(-2 * crashDumpSweepFullWalkInterval)); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := testutil.RunGopi(t, []string{"say ok"}, "", env, f.home); code != 0 {
		t.Fatalf("prompt run (floor expired) exit=%d stderr=%s", code, stderr)
	}
	// The sweep ran: it re-announced nothing new (the orphan is already
	// dumped) but it must have re-stamped the marker with the CURRENT time.
	m, err := readCrashDumpSweepMarker(f.marker)
	if err != nil {
		t.Fatalf("read marker after the floor-expired run: %v", err)
	}
	if time.Since(m.SweptAt) > crashDumpSweepFullWalkInterval/2 {
		t.Errorf("marker was not re-stamped by the floor-expired run (swept_at=%s) — the sweep did not run",
			m.SweptAt.Format(time.RFC3339))
	}
}

// TestGateConstantsAreUsedByTheGate is the Rule 13 guard on this file: the
// marker's field names, its version and its layout must be referenced through
// their constants, and no bare spelling may reappear. A literal that drifts
// from the constant is a marker the reader can no longer parse — i.e. a
// permanently useless fast path that nothing reports.
func TestGateConstantsAreUsedByTheGate(t *testing.T) {
	const fileName = "crashdump_sweep_gate.go"
	body, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatalf("read %s: %v", fileName, err)
	}
	code := stripGoCommentsAndConstBlock(t, string(body))
	for _, literal := range []string{
		`"` + crashDumpSweepMarkerName + `"`,
		`"` + crashDumpSweepMarkerFieldVersion + `"`,
		`"` + crashDumpSweepMarkerFieldSweptAt + `"`,
		`"` + crashDumpSweepMarkerFieldProbe + `"`,
		`"` + crashDumpSweepMarkerFieldRoot + `"`,
	} {
		if strings.Contains(code, literal) {
			t.Errorf("%s carries the literal %s in executable code — reference the constant it is defined as (Rule 13)", fileName, literal)
		}
	}
	// The marker's own file NAME is never typed inline either: it is used to
	// build the temp prefix, so the literal may exist only at its definition.
	if strings.Count(string(body), `"`+crashDumpSweepMarkerName+`"`) != 1 {
		t.Errorf("%s must spell the marker name exactly once (its definition); found %d",
			fileName, strings.Count(string(body), `"`+crashDumpSweepMarkerName+`"`))
	}
	// The layout and the version are referenced, not typed inline.
	src := string(body)
	if !strings.Contains(src, "crashDumpSweepMarkerTimeLayout") {
		t.Error("the marker time layout constant is never referenced")
	}
	if !strings.Contains(src, "crashDumpSweepFullWalkInterval") {
		t.Error("the bounded-cadence constant is never referenced")
	}
	if !strings.Contains(src, "crashDumpSweepBudgetSec") {
		t.Error("the budget constant is never referenced")
	}
}

// stripGoCommentsAndConstBlock removes doc comments and the `const (...)` block
// from a Go source file, leaving the executable code. The Rule-13 scan must not
// fire on a constant's own DEFINITION (or the prose that documents it), only on
// a literal used where the constant belongs.
func stripGoCommentsAndConstBlock(t *testing.T, src string) string {
	t.Helper()
	var out []string
	inBlockComment := false
	inConstBlock := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inBlockComment:
			if strings.Contains(trimmed, "*/") {
				inBlockComment = false
			}
			continue
		case strings.HasPrefix(trimmed, "/*"):
			if !strings.Contains(trimmed, "*/") {
				inBlockComment = true
			}
			continue
		case strings.HasPrefix(trimmed, "//"):
			continue
		case inConstBlock:
			if trimmed == ")" {
				inConstBlock = false
			}
			continue
		case trimmed == "const (":
			inConstBlock = true
			continue
		}
		out = append(out, line)
	}
	if inConstBlock || inBlockComment {
		t.Fatal("unterminated comment or const block — the scan would be unsound")
	}
	return strings.Join(out, "\n")
}

// awaitTreeChangeVisible polls until the gate reports the tree as CHANGED,
// failing the test if that never happens. It replaces a fixed sleep so the
// test asserts the property (a namespace change becomes visible to the probe)
// rather than the latency of one filesystem's parent-timestamp update.
func awaitTreeChangeVisible(t *testing.T, f *gateFixture, now time.Time) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if due, reason := OrphanSweepDue(now); due && reason == crashDumpSweepReasonChanged {
			return
		}
		if time.Now().After(deadline) {
			probe, err := treeChangeProbe(f.sessionsDir)
			if err != nil {
				t.Fatalf("tree change never became visible: %v", err)
			}
			t.Fatalf("tree change never became visible to the probe (probe=%v)", probe)
		}
		time.Sleep(5 * time.Millisecond) //stylegate:allow-sleep poll interval while waiting for ext4's lazy parent-timestamp publication; the loop has a hard deadline
	}
}
