package tools

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// TestTaskTickTockRendersAgeAndVerdict pins the tick-tock contract: a liveness
// stamp is rendered with its AGE against one clock read and an explicit
// fresh/stale verdict measured on the SAME stall grace the zombie sweep uses.
// A bare wall-clock stamp (the pre-change shape) made the reader subtract from
// their own clock and know the grace — this test fails if either half is
// dropped again.
func TestTaskTickTockRendersAgeAndVerdict(t *testing.T) {
	// The verdict is measured on config.ResolveTaskStallGrace's default
	// stall grace (30 min, internal/config) — an explicit value here would
	// be a second source of truth for the display (Rule 2), so the test
	// asserts around the default instead: 10 min is inside any sane grace,
	// 26 h is far past it.
	now := time.Now()
	fresh := now.Add(-10 * time.Minute)
	stale := now.Add(-26 * time.Hour)

	got := taskTickTock(true, float64(fresh.Unix()), now)
	if !strings.Contains(got, "(age 10m") || !strings.Contains(got, "— "+taskTickTockFresh+")") {
		t.Errorf("fresh stamp rendered %q, want a ~10m age with verdict %q", got, taskTickTockFresh)
	}
	if !strings.Contains(got, fresh.Format(taskTickTockClockFormat)) {
		t.Errorf("fresh stamp %q must carry the wall-clock form %q", got, fresh.Format(taskTickTockClockFormat))
	}

	if got := taskTickTock(true, float64(stale.Unix()), now); !strings.Contains(got, taskTickTockStale) {
		t.Errorf("26h-old stamp rendered %q, want verdict %q (past any stall grace)", got, taskTickTockStale)
	}

	// A NULL column is an ABSENCE, never a stale fact (the tasklist column
	// stays NULL on many runs — see devdoc/tasklist-issued-at-throttle-fix.md).
	for _, got := range []string{taskTickTock(false, 0, now), taskTickTock(true, 0, now)} {
		if !strings.Contains(got, taskTickTockUnknown) {
			t.Errorf("absent stamp rendered %q, want the %q verdict", got, taskTickTockUnknown)
		}
		if strings.Contains(got, taskTickTockStale) {
			t.Errorf("absent stamp rendered %q — an unrecorded fact must never read as stale", got)
		}
	}
}

// TestTaskTickTockClockSkewGuard pins the negative-age guard: a stamp minted
// by a peer host's clock can sit in the FUTURE here, and a renderer that
// subtracts blindly prints "-2m" as an age (a nonsense an operator would then
// try to interpret).
func TestTaskTickTockClockSkewGuard(t *testing.T) {
	now := time.Now()
	future := now.Add(2 * time.Minute)
	got := taskTickTock(true, float64(future.Unix()), now)
	if strings.Contains(got, "age -") {
		t.Errorf("future stamp rendered a negative age: %q", got)
	}
	if !strings.Contains(got, "age 0") {
		t.Errorf("future stamp rendered %q, want the age clamped to 0", got)
	}
}

// TestTaskShowLivenessLineUsesTickTock is the show-output guard: the liveness
// line must render BOTH stamps through taskTickTock (aged + verdict), so the
// "is it still running?" answer is on the surface the operator actually reads.
func TestTaskShowLivenessLineUsesTickTock(t *testing.T) {
	now := time.Now()
	tk := &Task{
		TaskID:           "01a00000-ticktock",
		Status:           taskStatusInProgress,
		CreatedAt:        float64(now.Add(-2 * time.Hour).Unix()),
		UpdatedAt:        float64(now.Add(-2 * time.Hour).Unix()), // frozen == the epic's symptom
		HasClaim:         true,
		ClaimedAt:        float64(now.Add(-2 * time.Hour).Unix()),
		HasToolcall:      true,
		LastToolcallAt:   float64(now.Add(-5 * time.Second).Unix()),
		HasTasklistIssue: false, // NULL column → absence, not a stale fact
		TasklistIssuedAt: 0,
		Summary:          "tick-tock render test",
	}
	line := taskShowTickTockLine(tk, now)
	if !strings.Contains(line, "last_toolcall_at=") || !strings.Contains(line, "— "+taskTickTockFresh+")") {
		t.Errorf("liveness line %q must render the toolcall stamp with an age and a verdict", line)
	}
	if !strings.Contains(line, taskTickTockUnknown) {
		t.Errorf("liveness line %q must render a NULL tasklist stamp as %q", line, taskTickTockUnknown)
	}
	if !strings.Contains(line, now.Format(taskTickTockClockFormat)) {
		t.Errorf("liveness line %q must carry the clock its ages were measured against", line)
	}
}

// TestTaskStateSuffixMarksStaleHeartbeat pins the LIST-side half of the epic:
// a row that is demonstrably ALIVE but whose tool batches stopped past the
// stall grace must not read as a plain working run. Its suffix keeps the
// pre-existing "[running <dur> since <clock>]" wording and ADDS the
// orthogonal fact, so "alive but wedged" is legible from the list alone.
func TestTaskStateSuffixMarksStaleHeartbeat(t *testing.T) {
	t.Setenv(config.GopiDirEnv, t.TempDir())
	now := time.Now()
	restore := stubLiveHolderProbe(t, true)
	defer restore()

	base := func(hb time.Duration) *Task {
		return &Task{
			Status: taskStatusInProgress, ClaimedBy: taskStateTestHolder(os.Getpid()),
			HasClaim: true, ClaimedAt: float64(now.Add(-2 * time.Hour).Unix()),
			CreatedAt: float64(now.Add(-2 * time.Hour).Unix()),
			// HasToolcall with a stamp: the primary heartbeat (not the
			// created_at fallback) is what the marker measures.
			HasToolcall: true, LastToolcallAt: float64(now.Add(-hb).Unix()),
		}
	}

	// Alive + heartbeat inside any sane grace: no marker (the pre-existing
	// wording, byte for byte).
	fresh := taskStateSuffix(base(10*time.Second), now)
	if strings.Contains(fresh, taskHeartbeatStaleSuffix) {
		t.Errorf("fresh heartbeat rendered %q — a working run must not carry the stale marker", fresh)
	}
	if !strings.Contains(fresh, taskStateRunning) || !strings.Contains(fresh, "since ") {
		t.Errorf("suffix %q must keep the running wording", fresh)
	}

	// Alive + heartbeat 26h old: the marker names it, with the age.
	stale := taskStateSuffix(base(26*time.Hour), now)
	if !strings.Contains(stale, taskHeartbeatStaleSuffix) {
		t.Errorf("26h-old heartbeat rendered %q, want the %q marker", stale, taskHeartbeatStaleSuffix)
	}
	if !strings.Contains(stale, "26h") {
		t.Errorf("marker %q must carry the heartbeat age", stale)
	}
	if !strings.HasSuffix(stale, "]") {
		t.Errorf("marker must stay INSIDE the suffix brackets: %q", stale)
	}
}

// TestTaskHeartbeatReadingFallbacks pins the ONE heartbeat-reading helper both
// surfaces share. Its subject is the HEARTBEAT, so created_at is never a
// substitute for a missing one: a row whose creation is 26h old but which
// recorded no liveness fact has NO heartbeat to judge, and rendering its age as
// "heartbeat stale" is a false verdict about a run that may be three seconds
// old (Rule 15 — an absence is an absence). The sweep's opposite use of that
// same instant — created_at as the row's AGE — went with the sweep itself on
// 2026-11-05, so nothing in this tree reads a row age as a heartbeat any more.
func TestTaskHeartbeatReadingFallbacks(t *testing.T) {
	t.Setenv(config.GopiDirEnv, t.TempDir())
	now := time.Now()

	// THE REGRESSION: created_at 26h old, NO liveness fact recorded. List used
	// to render "+ heartbeat stale 26h" on exactly this row (measured in
	// production: a row claimed 3 seconds ago read "heartbeat stale 3h56m").
	bare := &Task{Status: taskStatusInProgress, CreatedAt: float64(now.Add(-26 * time.Hour).Unix())}
	if age, stale, known := taskHeartbeatReading(bare, now); known || stale {
		t.Errorf("created_at-only row: age=%v known=%v stale=%v, want 0/false/false — "+
			"created_at is the row's age, not its heartbeat", age, known, stale)
	}
	// THE OTHER DIRECTION: a REAL heartbeat older than the grace still renders
	// stale, with its own age — the fix must not delete the feature.
	realStale := &Task{Status: taskStatusInProgress, CreatedAt: float64(now.Add(-time.Minute).Unix()),
		HasToolcall: true, LastToolcallAt: float64(now.Add(-26 * time.Hour).Unix())}
	age, stale, known := taskHeartbeatReading(realStale, now)
	if !known || !stale {
		t.Errorf("26h-old recorded heartbeat: known=%v stale=%v, want true/true", known, stale)
	}
	if age < 25*time.Hour {
		t.Errorf("recorded heartbeat age = %v, want ~26h", age)
	}

	// Zero stamps: nothing to measure → not known, never stale.
	empty := &Task{Status: taskStatusInProgress}
	if _, stale, known := taskHeartbeatReading(empty, now); known || stale {
		t.Errorf("unstamped row: known=%v stale=%v, want false/false", known, stale)
	}

	// Clock skew: a stamp in the future clamps to a zero age, never negative.
	future := &Task{Status: taskStatusInProgress, CreatedAt: float64(now.Add(2 * time.Minute).Unix()),
		HasToolcall: true, LastToolcallAt: float64(now.Add(2 * time.Minute).Unix())}
	if age, stale, known := taskHeartbeatReading(future, now); !known || stale || age < 0 {
		t.Errorf("future stamp: age=%v stale=%v known=%v, want a clamped non-negative fresh reading", age, stale, known)
	}
}

// TestFreshClaimNeverRendersStaleHeartbeat is the surface-level reproduction of
// the reported defect, end to end through the LIST renderer: a row claimed 3
// seconds ago, with no liveness fact yet (a run that has not started its first
// tool batch), must render the plain running suffix. Its created_at is hours
// old by design — a row is created, queued, and claimed much later.
func TestFreshClaimNeverRendersStaleHeartbeat(t *testing.T) {
	t.Setenv(config.GopiDirEnv, t.TempDir())
	now := time.Now()
	restore := stubLiveHolderProbe(t, true)
	defer restore()

	row := &Task{
		Status: taskStatusInProgress, ClaimedBy: taskStateTestHolder(os.Getpid()),
		HasClaim:  true,
		ClaimedAt: float64(now.Add(-3 * time.Second).Unix()),
		CreatedAt: float64(now.Add(-4 * time.Hour).Unix()), // queued hours ago
	}
	suffix := taskStateSuffix(row, now)
	if strings.Contains(suffix, taskHeartbeatStaleSuffix) {
		t.Errorf("a claim 3s old rendered %q — a fresh claim has no stale heartbeat to report", suffix)
	}
	if !strings.Contains(suffix, taskStateRunning) {
		t.Errorf("suffix %q must still read as a running row", suffix)
	}

	// The same row once a REAL heartbeat is recorded and goes stale: the
	// marker comes back. Both directions on the same surface.
	row.HasToolcall, row.LastToolcallAt = true, float64(now.Add(-4*time.Hour).Unix())
	if suffix := taskStateSuffix(row, now); !strings.Contains(suffix, taskHeartbeatStaleSuffix) {
		t.Errorf("suffix %q must carry the stale marker once a recorded heartbeat is 4h old", suffix)
	}
}

// TestHeartbeatColumnLiteralGate is the Rule 13 anti-regression guard for the
// two M4.5 heartbeat column names: after the constants exist, a bare
// "last_toolcall_at" / "tasklist_issued_at" literal may live ONLY at the
// definition site (tasks.go). A re-inlined literal is how the constant and the
// SQL drift apart — one site gets renamed, the other keeps writing the old
// column, and the heartbeat silently stops ticking on the read side.
func TestHeartbeatColumnLiteralGate(t *testing.T) {
	const holder = "tasks.go" // the ONE allowed holder of the two literals
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	// SCOPE (stated, not implied): this gate covers the PRODUCTION .go files of
	// internal/tools — the package that owns the constants, the schema, the
	// migrations, the projections, the writers and both sweeps, i.e. every
	// site where a drifted literal would silently mis-read the heartbeat.
	// cmd/agent/cli_tasks.go still spells the column in its operator help text
	// (2 sites): that prose NAMES the DB column for an operator grepping the
	// ledger, cannot reach an unexported constant in another package, and
	// moving it behind an exported leaf package is a separate change — so it
	// is left visible here rather than papered over by widening the gate's
	// claim. The same choice (package-scoped scan, stated scope) as
	// TestUnknownNode_MirrorLiteralGate.
	// The scan runs over the AST, not the file BYTES: a raw byte search sees
	// the literals in a COMMENT (prose naming a column is not a bypass) and
	// MISSES the real defect shape — a column name inlined inside a SQL
	// string, e.g. `COALESCE(last_toolcall_at, created_at)`, which carries no
	// surrounding quotes. go/parser + go/ast is stdlib, so this gate needs no
	// dependency and no CGO (Rule 10.7).
	fset := token.NewFileSet()
	scanned := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		if e.Name() == holder {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		scanned++ // read-floor: a broken walk must not pass vacuously
		var bad []string
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			// Unquote once so both the exact-key form ("last_toolcall_at")
			// and the SQL-embedded form ("… COALESCE(last_toolcall_at, …)")
			// are visible to the same check — the embedded form is invisible
			// to a byte search for the quoted spelling.
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				val = lit.Value
			}
			for _, col := range []string{taskLastToolcallAtColumn, taskTasklistIssuedAtColumn} {
				if heartbeatColumnOccurrence(val, col) {
					bad = append(bad, fmt.Sprintf("%s:%d (%s)", e.Name(),
						fset.Position(lit.Pos()).Line, col))
				}
			}
			return true
		})
		for _, b := range bad {
			t.Errorf("%s carries the column literal — reference taskLastToolcallAtColumn/taskTasklistIssuedAtColumn (defined in %s)", b, holder)
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no production files — the gate would pass vacuously")
	}
}

// heartbeatColumnOccurrence reports whether a decoded Go string literal names
// the column with IDENTIFIER boundaries: `last_toolcall_at` matches, while a
// longer identifier that merely contains it (a future `last_toolcall_at_ms`)
// does not — a substring check would fire on that and make the gate noise,
// which is how a gate gets disabled instead of fixed.
func heartbeatColumnOccurrence(val, col string) bool {
	for idx := 0; ; {
		i := strings.Index(val[idx:], col)
		if i < 0 {
			return false
		}
		start := idx + i
		end := start + len(col)
		if !isIdentByteAt(val, start-1) && !isIdentByteAt(val, end) {
			return true
		}
		idx = end
	}
}

// isIdentByteAt reports whether the byte at i (if any) can be part of a Go/SQL
// identifier (letter, digit or underscore) — the boundary test above.
func isIdentByteAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// TestHeartbeatWritersDoNotTouchUpdatedAt is the WRITER-PURITY guard behind
// the epic's report: `updated_at` is the M4.13 sync/state anchor, and the epic
// exists because an operator read its frozen value as "the task stopped
// running". The liveness path (the two heartbeat touches, and — until
// 2026-11-05 — the stale sweep's re-arm bump) is therefore the ONLY SQL in
// this package allowed anywhere near updated_at on an in_progress row; the
// re-arm is DELETED with the sweep, so the exemption below is history. This test pins the fact that made the diagnostic hard: whatever
// writes the heartbeat, it is not updated_at.
func TestHeartbeatWritersDoNotTouchUpdatedAt(t *testing.T) {
	b, err := os.ReadFile("taskheartbeat.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	src := string(b)
	// The heartbeat file may MENTION updated_at in prose (the dedup guard's
	// rationale) and may READ it in the dedup WHERE clause (`updated_at < ?`)
	// — neither is a heartbeat write. Only an ASSIGNMENT is a violation of
	// the epic's finding: the tick-tock columns are the heartbeat, and
	// updated_at must not be bumped on the liveness path (the only write it
	// ever had here, the sweep re-arm bump in taskZombieBumpSQL, is deleted
	// 2026-11-05 with the sweep itself — the exemption stays so the gate keeps
	// its shape).
	assignment := regexp.MustCompile(`updated_at\s*=\s*[?']`)
	for i, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !assignment.MatchString(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "//") || strings.Contains(trimmed, "taskZombieBumpSQL") {
			continue
		}
		t.Errorf("taskheartbeat.go:%d assigns updated_at (%q) — the heartbeat columns are %s/%s; updated_at is the sync anchor, never a tick-tock",
			i+1, trimmed, taskLastToolcallAtColumn, taskTasklistIssuedAtColumn)
	}
}

// TestHeartbeatColumnOccurrenceProbes pins the boundary rule the gate rests
// on, with positive AND negative probes: the exact key, the SQL-embedded form
// and a prefixed form must all fire; a longer identifier that merely CONTAINS
// the name, and unrelated prose, must not (a substring check would flag a
// future `last_toolcall_at_ms` column and turn the gate into noise).
func TestHeartbeatColumnOccurrenceProbes(t *testing.T) {
	col := taskLastToolcallAtColumn
	cases := []struct {
		val  string
		want bool
	}{
		{col, true},                                     // the exact key
		{"SELECT " + col + " FROM tasks", true},         // embedded in SQL
		{"COALESCE(" + col + ", created_at) < ?", true}, // the sweep's shape
		{"x_" + col, false},                             // a different identifier that merely encloses it
		{"t" + col, false},                              // ditto (identifier char before the name)
		{col + "s", false},                              // longer identifier: NOT the column
		{"last_toolcall_at_ms", false},                  // a future column must not fire
		{"last toolcall at", false},                     // unrelated prose
		{"tasklist_issued_at", false},                   // the OTHER column is not this one
	}
	for _, c := range cases {
		if got := heartbeatColumnOccurrence(c.val, col); got != c.want {
			t.Errorf("heartbeatColumnOccurrence(%q, %q) = %v, want %v", c.val, col, got, c.want)
		}
	}
	if !heartbeatColumnOccurrence("SELECT "+taskTasklistIssuedAtColumn, taskTasklistIssuedAtColumn) {
		t.Errorf("the tasklist column must be detected by the same rule")
	}
}
