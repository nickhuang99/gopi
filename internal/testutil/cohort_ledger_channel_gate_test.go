package testutil

// scripts/nonff-able-cohort-after.sh measured NOTHING on a host without the
// `sqlite3` command line, and reported it as a verdict about branches.
//
// Measured shape (the reason this gate exists): the script seeded a ledger row
// per cohort branch and read that row's `task_events` trail through `sqlite3`.
// With the command absent, both statements failed and both failures were
// DISCARDED — the seed with a bare call, the read with `2>/dev/null` inside a
// command substitution. The trail then came back EMPTY, and an empty trail is
// exactly what the script's own predicate calls "non-ff-able and UNEXPLAINED",
// so every branch scored as the harness's worst category and the script exited
// 1 for a reason that existed only on its stdio (Rule 15 — silent failure).
//
// The repair is a zero-dependency channel (scripts/lib/ledgersql over
// modernc.org/sqlite, already a module dependency) plus checked exit statuses.
// This gate pins the two properties a later edit would quietly undo:
//
//  1. no `sqlite3` INVOCATION comes back (the prose may name it — a comment
//     explaining why it is gone is the point, so only command position counts);
//  2. the script sources the ledger channel, and the channel's Go source is
//     present (a `source` of a missing file is another silent-failure shape).
//
// It is deliberately narrow: scripts in this repo that DO use `sqlite3` are
// their own business and their own task. This one is pinned because its number
// is a verdict somebody acts on.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// cohortScriptPath is the script whose measurement this gate defends.
	cohortScriptPath = "scripts/nonff-able-cohort-after.sh"
	// ledgerChannelShell / ledgerChannelGo are the two halves of the channel
	// the script must route its ledger access through.
	ledgerChannelShell = "scripts/lib/ledger-sql.sh"
	ledgerChannelGo    = "scripts/lib/ledgersql/ledgersql.go"
	// cohortScriptFloor is the minimum size of a script that still contains the
	// measurement — a scan that read an empty or truncated file must not pass.
	cohortScriptFloor = 4000
)

// sqliteInvocationRe matches `sqlite3` in COMMAND position: at the start of a
// line, after a shell separator, or as a word inside a command substitution.
// A line that merely mentions it (prose, a comment, an echo) does not match,
// which is what keeps this gate honest in both directions.
var sqliteInvocationRe = regexp.MustCompile(`(?m)(^|[;|&(]|\$\()\s*(sqlite3)\s`)

// TestCohortScriptHasNoSqliteCommandLinePath is the regression guard for the
// silent-failure bug: the script must reach the ledger through its own Go
// helper, and never through a command this host does not have.
func TestCohortScriptHasNoSqliteCommandLinePath(t *testing.T) {
	root := repoRootOrFatal(t)

	scriptPath := filepath.Join(root, cohortScriptPath)
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v — the cohort measurement's entry point is gone, so this gate has nothing to defend", cohortScriptPath, err)
	}
	if len(body) < cohortScriptFloor {
		t.Fatalf("%s is %d bytes, floor is %d — the scan read a truncated file and would pass vacuously",
			cohortScriptPath, len(body), cohortScriptFloor)
	}

	text := string(body)
	if hit := sqliteInvocationRe.FindString(text); hit != "" {
		t.Errorf("%s invokes the sqlite3 command line (%q). It is NOT installed on this host and this repo does not take it as a dependency (Rule 35): the seed/read must go through scripts/lib/ledger-sql.sh, whose NON-ZERO exit the caller handles instead of discarding (Rule 15).",
			cohortScriptPath, strings.TrimSpace(hit))
	}

	if !strings.Contains(text, ledgerChannelShell) {
		t.Errorf("%s does not source %s — without it the script has no ledger channel at all",
			cohortScriptPath, ledgerChannelShell)
	}
	if _, err := os.Stat(filepath.Join(root, ledgerChannelShell)); err != nil {
		t.Errorf("%s is missing (%v): the script sources it, so a missing file is a startup failure, not a degraded mode",
			ledgerChannelShell, err)
	}
	if _, err := os.Stat(filepath.Join(root, ledgerChannelGo)); err != nil {
		t.Errorf("%s is missing (%v): the channel's shell half compiles THIS source, so its absence turns every ledger access into a refusal",
			ledgerChannelGo, err)
	}
}
