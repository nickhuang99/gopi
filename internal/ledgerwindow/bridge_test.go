package ledgerwindow_test

// The bridge's contract test: `gopi tool ledger-window` prints the epoch bounds the
// SAME predicate resolves, against a synthetic ledger — so the shell side gets
// its numbers from the one definition, and the command's output format is
// pinned rather than described.
//
// The bridge was the separate `cmd/ledger-window` program until 2026-10-02; it is
// now a verb of the ONE binary, so the test BUILDS THAT binary and runs the verb
// — which is also the stronger check (it exercises the subcommand's real dispatch
// path, not a program whose only job was this one function).

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nickhuang99/gopi/internal/ledgerwindow"
)

const (
	// bridgePkg is the ONE binary's package path: the bridge is a verb of it now,
	// so the test exercises the real dispatch chain rather than a dedicated main.
	bridgePkg = "./cmd/agent"
	// bridgeBinName is the binary the test builds into its own temp directory.
	bridgeBinName = "gopi"
	// bridgeVerb is the subcommand word the bridge answers to.
	bridgeVerb = "ledger-window"
	// bridgeGroupVerb is the group verb the bridge now hangs under: the bare
	// `gopi ledger-window` spelling is RETIRED (cmd/agent's alias table refuses
	// it with "use `gopi tool ledger-window`"), so the test must drive the
	// surface a reader is actually told to run. Measured red before this was
	// added — task 01a18281, both arms failing with the verb's own refusal.
	bridgeGroupVerb = "tool"
	// bridgeFields is how many tab-separated fields the command prints:
	// since, until, offset.
	bridgeFields = 3
	// syncLedgerDriver is the driver the repo registers.
	syncLedgerDriver = "sqlite"
	// syncLedgerSpreadHours spreads the synthetic rows beyond the window on
	// both sides, so a shifted window selects a different count.
	syncLedgerSpreadHours = 6
	// syncLedgerHourSec is the grid's step.
	syncLedgerHourSec = 3600
	// dbFlagEnv names the database the command reads.
	dbFlagEnv = "GOPI_MEMORY_DB"
	// envPath is the runtime's own PATH variable name (spelled once, so the
	// child's lookup path is not a bare literal at the call site).
	envPath = "PATH"
	// bridgeSince / bridgeUntil are the window the bridge test asks about.
	bridgeSince = "2026-09-16 22:00"
	bridgeUntil = "2026-09-17 05:00"
)

// TestBridgePrintsThePredicatesBounds runs the real command against an on-disk
// SYNTHETIC ledger (never ~/.gopi/memory.db) and asserts its three fields
// against the package's own resolution of the same window.
func TestBridgePrintsThePredicatesBounds(t *testing.T) {
	root := moduleRoot(t)
	dbPath := filepath.Join(t.TempDir(), "synthetic-ledger.db")
	db := createSyntheticLedger(t, dbPath)

	w := ledgerwindow.Window{Since: bridgeSince, Until: bridgeUntil}
	want, err := ledgerwindow.ResolveBounds(context.Background(), db, w)
	if err != nil {
		t.Fatalf("resolve the window's bounds through the package: %v", err)
	}

	out := runBridge(t, root, dbPath, bridgeSince, bridgeUntil)
	fields := parseBridgeOutput(t, out)
	wantFields := []string{
		strconv.FormatInt(want.Since, 10),
		strconv.FormatInt(want.Until, 10),
		strconv.Itoa(want.OffsetSec),
	}
	for i, wf := range wantFields {
		if fields[i] != wf {
			t.Errorf("bridge field %d = %q, want %q (the predicate's own resolution) — the shell side and the Go "+
				"side must read ONE definition (full output %v)", i, fields[i], wf, fields)
		}
	}
}

// TestBridgeCountsAgreeWithTheHelper closes the loop the shell surface actually
// needs: the count a script prints from the bridge's bounds equals
// CountInWindow over the same window.
func TestBridgeCountsAgreeWithTheHelper(t *testing.T) {
	root := moduleRoot(t)
	dbPath := filepath.Join(t.TempDir(), "synthetic-ledger.db")
	db := createSyntheticLedger(t, dbPath)

	w := ledgerwindow.Window{Since: bridgeSince, Until: bridgeUntil}
	fields := parseBridgeOutput(t, runBridge(t, root, dbPath, bridgeSince, bridgeUntil))
	since, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		t.Fatalf("bridge lower bound %q is not an epoch number: %v", fields[0], err)
	}
	until, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		t.Fatalf("bridge upper bound %q is not an epoch number: %v", fields[1], err)
	}

	var viaBounds int64
	query := ledgerwindow.SelectCountPrefix + ledgerwindow.ColumnClaimedAt +
		" BETWEEN ? AND ?"
	if err := db.QueryRow(query, since, until).Scan(&viaBounds); err != nil {
		t.Fatalf("count via the bridge's bounds (%s): %v", query, err)
	}
	want, err := ledgerwindow.CountInWindow(context.Background(), db, ledgerwindow.ColumnClaimedAt, w)
	if err != nil {
		t.Fatalf("count through the helper: %v", err)
	}
	if viaBounds != want {
		t.Errorf("a script counting via the bridge's bounds gets %d, the helper gets %d — the two surfaces must "+
			"answer one question with one number", viaBounds, want)
	}
}

// TestBridgeRejectsAnInvalidWindow pins the loud half: a window that cannot
// describe an interval exits non-zero rather than printing bounds for a
// question nobody asked.
func TestBridgeRejectsAnInvalidWindow(t *testing.T) {
	root := moduleRoot(t)
	dbPath := filepath.Join(t.TempDir(), "synthetic-ledger.db")
	createSyntheticLedger(t, dbPath)

	cases := []struct {
		name        string
		args        []string
		wantNonZero bool
	}{
		{name: "inverted window", args: []string{bridgeUntil, bridgeSince}, wantNonZero: true},
		{name: "empty bound", args: []string{"", bridgeUntil}, wantNonZero: true},
		{name: "unparseable bound", args: []string{"last night", bridgeUntil}, wantNonZero: true},
		{name: "one argument", args: []string{bridgeSince}, wantNonZero: true},
		{name: "valid window", args: []string{bridgeSince, bridgeUntil}, wantNonZero: false},
	}
	for _, tc := range cases {
		code, combined := runBridgeRaw(t, root, dbPath, tc.args...)
		if tc.wantNonZero && code == 0 {
			t.Errorf("%s: the bridge exited 0 and printed %q — an unanswerable window must fail loudly",
				tc.name, combined)
		}
		if !tc.wantNonZero && code != 0 {
			t.Errorf("%s: the bridge exited %d: %s", tc.name, code, combined)
		}
	}
}

// runBridge runs the command and requires success, returning its output.
func runBridge(t *testing.T, root, dbPath string, args ...string) string {
	t.Helper()
	code, combined := runBridgeRaw(t, root, dbPath, args...)
	if code != 0 {
		t.Fatalf("ledger-window %v exited %d: %s", args, code, combined)
	}
	return combined
}

// runBridgeRaw builds and runs the command against dbPath, returning its exit
// status and combined output. The binary lands in the test's own temp directory
// (never the checkout's bin/, which other suites share).
func runBridgeRaw(t *testing.T, root, dbPath string, args ...string) (int, string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), bridgeBinName)
	build := exec.Command("go", "build", "-o", bin, bridgePkg)
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", bridgePkg, err, out)
	}

	cmd := exec.Command(bin, append([]string{bridgeGroupVerb, bridgeVerb}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), dbFlagEnv+"="+dbPath, envPath+"="+os.Getenv(envPath))
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("run %s: %v\n%s", bin, err, out)
	return -1, ""
}

// parseBridgeOutput splits the command's one-line output.
func parseBridgeOutput(t *testing.T, out string) []string {
	t.Helper()
	line := strings.TrimRight(out, "\n")
	fields := strings.Split(line, "\t")
	if len(fields) != bridgeFields {
		t.Fatalf("the bridge printed %d field(s) (%q), want %d (since, until, offset)", len(fields), line, bridgeFields)
	}
	return fields
}

// createSyntheticLedger writes a ledger file holding an hourly grid spanning
// the window plus syncLedgerSpreadHours on each side, and returns the open
// handle for the expectation side of the test.
func createSyntheticLedger(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open(syncLedgerDriver, path)
	if err != nil {
		t.Fatalf("create the synthetic ledger at %s: %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() }) //stylegate:ignore test fixture handle; the assertions above run first

	ddl := "CREATE TABLE " + ledgerwindow.TableTasks + " (" + ledgerwindow.ColumnCreatedAt + " INTEGER, " +
		ledgerwindow.ColumnClaimedAt + " INTEGER, " + ledgerwindow.ColumnEndedAt + " INTEGER)"
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("create the synthetic ledger's table (%s): %v", ddl, err)
	}
	since, err := ledgerwindow.ParseBound(bridgeSince)
	if err != nil {
		t.Fatalf("parse %q: %v", bridgeSince, err)
	}
	until, err := ledgerwindow.ParseBound(bridgeUntil)
	if err != nil {
		t.Fatalf("parse %q: %v", bridgeUntil, err)
	}
	insert := "INSERT INTO " + ledgerwindow.TableTasks + " (" + ledgerwindow.ColumnCreatedAt + ", " +
		ledgerwindow.ColumnClaimedAt + ") VALUES (?, ?)"
	for at := since - syncLedgerSpreadHours*syncLedgerHourSec; at <= until+syncLedgerSpreadHours*syncLedgerHourSec; at += syncLedgerHourSec {
		if _, err := db.Exec(insert, at, at); err != nil {
			t.Fatalf("seed the synthetic ledger at %d: %v", at, err)
		}
	}
	return db
}
