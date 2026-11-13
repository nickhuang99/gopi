package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// Suite gopi-profile isolation — the RESIDUAL internal/tools suite hang
// (t-1788361069 round 3, 2026-09-13).
//
// ROOT CAUSE THIS FILE EXISTS FOR: the tmpfs scratch-root pin (scratchroot.go)
// fixed the WRITE side — a suite's throwaway SQLite files no longer fsync the
// boot device. The READ side was never isolated: a test that pins only its DB
// path (the conventional `t.Setenv("GOPI_MEMORY_DB", …)`) leaves
// config.GopiDir() resolving the DEVELOPER'S REAL profile, so every
// read-only audit lookup lands in ~/.gopi.
//
// The lookup that made it fatal is `task show`'s session resolution:
// taskSessionLogPath → taskFindSessionJSONL does an os.ReadDir of the sessions
// dir AND an os.ReadDir + first-line read + json.Unmarshal of a session.jsonl
// inside EVERY subdirectory of it, looking for one short session id. `task
// show` resolves the session ~4x per render, so ONE call walks the whole tree
// several times. Measured on the developer host (2026-09-13): 54,847 entries
// under ~/.gopi/sessions (54,178 of them directories holding a session.jsonl).
//
// The cost is LINEAR in the ambient tree size — measured with the same binary,
// `go test ./internal/tools/ -run '^TestTaskStampRun$'`:
//
//	sessions entries   wall
//	                0   18.8s
//	            4,000   84.3s
//	           16,000  272.9s
//
// and the real 54,847-entry host measured 77s for that ONE test at load ~20
// (extrapolated from the law above, ~900s at load 1). The whole suite was
// 438.9s with 73.6s / 49.5s / 38.1s / 27.2s spent by four such tests. Go's
// 600s per-package timeout is wall-clock, so on a host with a large profile
// (and/or a loaded box) the suite panics: "panic: test timed out". That is the
// moving-target hang — NOT a deadlock and NOT a leaked connection: the SIGQUIT
// dump shows the test goroutine RUNNABLE in os.ReadDir and pprof attributes
// 45% of CPU to the GC collector, because the per-directory strings and
// json.Unmarshal scratch are transient heap churn.
//
// The fix is isolation, not a timeout (Rule 10.3: never raise a budget to hide
// a hang) and not a production rewrite: point the suite's ENTIRE gopi data
// root at an EMPTY per-run sandbox inside the already-pinned scratch root, so
// no test can read the developer's profile by accident. This is the same
// recipe (and the same reasoning) as the notify sandbox in suiteenv.go: "a
// test that wants the durable thing must NAME it" — here, a test that wants a
// session file or the real ledger sets GOPI_HOME itself (19 tests in
// internal/tools already do), and everything else resolves to an empty tree
// whose cost is zero at any host size.
//
// It also closes a real privacy leak: without this pin the suite reads the
// developer's live sessions and ledger (the agency-replay evidence harness
// deliberately does; every accidental reader did so silently).

const (
	// SuiteGopiProfileDirName is the per-run sandbox HOME the suite's gopi data
	// root resolves under (GOPI_HOME → <scratch root>/<this>, so GopiDir()
	// becomes <scratch root>/<this>/.gopi).
	SuiteGopiProfileDirName = "suite-gopi-home"
	// gopiProfileDirName is the `.gopi` component GopiDir joins onto the
	// profile home — the same value internal/config owns, spelled once here for
	// the sandbox creation (Rule 1b: never a bare literal at a use site).
	gopiProfileDirName = ".gopi"

	// suiteGopiProfileMaxEntries bounds how many entries the suite's sandbox
	// sessions tree may hold at the profile guard's check. The tree STARTS
	// empty (the pin creates it), so every entry is suite-created: a fixture
	// that wrote a session file under the shared sandbox root. That direction
	// is bounded and harmless; the bound catches the OTHER direction — a
	// fixture (or a copied profile) materializing the developer's real,
	// unbounded tree in the sandbox, which is the scan this pin removes. Set
	// far above what this suite writes (a few dozen) and far below any real
	// profile (the developer's holds 54,847).
	suiteGopiProfileMaxEntries = 256
)

// PinSuiteGopiProfile points GopiDirEnv at an EMPTY per-run sandbox home
// inside the scratch root, so the suite's gopi data root (settings, sessions,
// memory.db, raw logs) is a fresh empty tree instead of the developer's real
// profile.
//
// It is deliberately an EMPTY tree, not a copy: the suite is supposed to
// sandbox what it needs (19 internal/tools tests already t.Setenv GOPI_HOME at
// a fixture), and copying a 54k-entry session tree would reintroduce the very
// scan this pin removes.
//
// Errors are REPORTED, never swallowed (Rule 15b) — and never fatal, exactly
// like the scratch-root pin: a suite whose scratch home is unavailable still
// runs, on the ambient profile, with the cost/leak made visible on stderr.
func PinSuiteGopiProfile(suite string) {
	if !SuiteTempRootPinned() {
		fmt.Fprintf(os.Stderr,
			"testutil: suite %q: scratch root is NOT pinned, so %s is left AMBIENT — audit lookups "+
				"(e.g. `task show`'s session resolution) will walk the real profile %s, whose cost is "+
				"linear in its entry count and can stall the suite. This is the accepted %s escape path.\n",
			suite, config.GopiDirEnv, ambientGopiDir(), gotestSuiteNoPinEnvName)
		return
	}
	home := filepath.Join(SuiteTempRoot(), SuiteGopiProfileDirName)
	// Create the .gopi root itself: a resolver that only READS an absent root
	// is fine, but the empty directory keeps every lookup's ErrNotExist path
	// out of the hot loop and matches what a real profile looks like.
	if err := os.MkdirAll(filepath.Join(home, gopiProfileDirName), dirPerm); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: gopi profile sandbox %s: %v — %s left ambient\n",
			suite, home, err, config.GopiDirEnv)
		return
	}
	if err := os.Setenv(config.GopiDirEnv, home); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: suite %q: pinning %s: %v\n", suite, config.GopiDirEnv, err)
	}
}

// ambientGopiDir is the profile root the pin REPLACES — the operator's real
// one. It is computed the same way config.GopiDir computes its default (so the
// message names the actual tree a lookup would walk) and is used for reporting
// only, never to read anything.
func ambientGopiDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, gopiProfileDirName)
	}
	return "~/" + gopiProfileDirName
}

// AssertSuiteGopiProfileIsPinned is the guard every suite can run as a
// one-line test: it fails when the suite's gopi data root is NOT a sandbox
// inside the pinned scratch root. Without it a future TestMain edit silently
// re-opens the profile dependence — and the dependence is invisible from
// inside the suite, which is exactly why it survived the scratch-root fix.
func AssertSuiteGopiProfileIsPinned(t *testing.T) {
	t.Helper()
	if !SuiteTempRootPinned() {
		t.Skipf("no pinned scratch root (%s absent and no %s override) — the suite runs on the ambient "+
			"profile by design (%s)", ShmDir, SuiteTempRootEnvName, gotestSuiteNoPinEnvName)
	}
	home := os.Getenv(config.GopiDirEnv)
	if home == "" {
		t.Fatalf("%s is unset — the suite fell back to the developer's real gopi profile (%s), whose audit "+
			"lookups walk its whole sessions tree. The suite entry point must pin it (%s)",
			config.GopiDirEnv, ambientGopiDir(), "PinSuiteGopiProfile")
	}
	if !pathUnder(home, SuiteTempRoot()) {
		t.Fatalf("%s=%q is not inside the suite scratch root %q — the suite would read the developer's "+
			"real profile instead of an empty sandbox", config.GopiDirEnv, home, SuiteTempRoot())
	}
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("resolving the suite's gopi data root: %v", err)
	}
	if !pathUnder(gd, SuiteTempRoot()) {
		t.Fatalf("the suite's gopi data root resolves to %q, outside the scratch root %q", gd, SuiteTempRoot())
	}
	// The scan-cost invariant that makes the suite's runtime independent of the
	// host: the sandbox sessions tree STARTS EMPTY (the pin creates it), so it
	// may only hold what this suite itself wrote. What the bound catches is a
	// fixture (or a copied profile) materializing the developer's real,
	// unbounded tree in the sandbox — exactly the scan this pin removes.
	// The names are listed so a leak shows WHICH fixture wrote it.
	sessRoot := filepath.Join(gd, config.SessionsDirName)
	entries, err := os.ReadDir(sessRoot)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading the sandbox sessions tree: %v", err)
	}
	if len(entries) > suiteGopiProfileMaxEntries {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		if len(names) > 20 {
			names = append(names[:20], fmt.Sprintf("…(+%d more)", len(entries)-20))
		}
		t.Fatalf("the suite's sessions tree %s holds %d entries — a fixture materialized a profile tree in the "+
			"sandbox, so `task show`'s session resolution pays a directory scan per lookup (the residual hang "+
			"at t-1788361069). A test that needs a session file must name its OWN %s sandbox. Entries: %s",
			sessRoot, len(entries), config.GopiDirEnv, strings.Join(names, ", "))
	}
}
