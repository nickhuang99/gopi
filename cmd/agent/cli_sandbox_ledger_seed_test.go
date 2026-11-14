// cli_sandbox_ledger_seed_test.go — the `--ledger-seed` FLAG's reachability, at
// the seam where it can silently break (task 01a19574).
//
// WHY THIS FILE EXISTS. internal/sandbox has supported seeding a sandbox ledger
// since the ledger-shape work: Options.LedgerSeedPath is honoured by
// ledgerSeedPath(), copied by value, refused when it is not a database or lives
// inside the sandbox — all of it tested. What NO test covered is the only thing
// a caller touches: the flag. The knob was reachable from the ENVIRONMENT ONLY,
// so the executor of a SIGPIPE experiment that needed a real, populated,
// WRITABLE ledger never discovered it and hand-built six `cp` directories of
// 645 MiB each, outside the sandbox channel and outside every reclaim leg
// (measured 2026-11-01, ~3.8 GB, all still on disk).
//
// THE FAILURE MODE THIS PINS IS SILENT. If the parser loses its `--ledger-seed`
// arm, nothing downstream complains: Options.LedgerSeedPath keeps its zero
// value, ledgerSeedPath() falls through to the environment, finds nothing, and
// the sandbox is created with an EMPTY ledger — a valid sandbox, exit 0, and an
// experiment that measures nothing. So the assertion is not "the flag parses":
// it is that the seed's ROW is readable through the sandbox the run made, i.e.
// the flag reached the initializer.
//
// THE RED PROOF (stated, and it is the state of master before this commit): the
// pre-fix binary exits 2 and prints
//
//	gopi sandbox create: unexpected argument "--ledger-seed" (want --label NAME, --task-id ID, --sandbox-dir PATH, -- or -h)
//
// because sandboxParseFlags has no arm for the spelling and falls to its
// `default`. Any breakage re-introduced later fails here for the same reason.
package main

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sandbox"
	"github.com/nickhuang99/gopi/internal/tools"
)

// cliSandboxSeedRowID is the task id the probe seeds the ledger with. It is a
// name no production ledger holds, so reading it back proves the row came from
// THIS seed and not from anything the sandbox found on its own.
const cliSandboxSeedRowID = "sandbox-seed-probe-row"

// cliSandboxSeedRowSummary is the summary the fixture's row carries — a string
// that exists nowhere else, so reading it back proves the bytes came from THIS
// seed.
const cliSandboxSeedRowSummary = "seeded row: a real ledger copied into a sandbox"

// cliSandboxSeedFixturePerm is the seed fixture's mode — the ORDINARY writable
// mode, deliberately NOT the sample's 0444 seal: this fixture stands for "a real
// operator ledger", which its owner may write.
const cliSandboxSeedFixturePerm = 0o644

// cliSandboxLedgerSeedFixture builds a REAL (a genuine SQLite database, opened
// through the tree's own DSN) but SMALL ledger holding one row, and returns its
// path.
//
// WHY NOT testutil.SampleDBPath — the honest reason, because it is the reason
// the epic exists. The sample IS real and populated, but two of its properties
// make it useless HERE: it is sealed 0444 and opened `mode=ro` by its own
// contract, and its DDL is the READER-visible surface only (tasks / task_events
// / distilled_qa), NOT the ledger's own schema. So it can neither be written
// (the experiment's need) nor answer `gopi task show` (the assertion's need).
// This fixture is the third shape: writable, and shaped like the file the
// production reader opens.
//
// THE SIZE IS THE POINT (Rule 34's measured lesson): the operator's real ledger
// is 645 MiB, and copying THAT to prove a flag is precisely the defect this
// change removes. A schema plus one row is ~30 KiB and proves the same thing.
func cliSandboxLedgerSeedFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), config.MemoryDBFileName)
	// Rule 32: a helper that builds a database ITSELF must pin the DB
	// environment in the same breath, or a tools-layer call in any test sharing
	// this one resolves the OPERATOR'S live ledger (append-only: a row written
	// there is permanent). testutil.NewTestDBPath is the other door and gives an
	// EMPTY database, which is the shape this fixture must NOT be — the whole
	// point is a ledger with a row already in it.
	//
	// db-scaffold-ok: the pin IS below — t.Setenv(config.MemoryDBEnv, path) — and
	// the gate reads only a STRING LITERAL as Setenv's first argument
	// (dbScaffoldSetsDBEnv, db_scaffold_gate_test.go:399), so a named constant is
	// invisible to it. Spelling the literal here to satisfy the detector would be
	// Rule 1b's defect (a second copy of the env name, free to drift from
	// config.MemoryDBEnv); the constant is the correct spelling and the escape
	// records why the detector cannot see it.
	t.Setenv(config.MemoryDBEnv, path)
	db, err := sql.Open("sqlite", tools.LedgerDSN(path))
	if err != nil {
		t.Fatalf("open the seed ledger %s: %v", path, err)
	}
	if _, err := db.Exec(cliSandboxSeedFixtureDDL); err != nil {
		_ = db.Close() //stylegate:ignore the fixture is being torn down after a fatal error
		t.Fatalf("create the seed ledger's table: %v", err)
	}
	if _, err := db.Exec(cliSandboxSeedFixtureInsert, cliSandboxSeedRowID, cliSandboxSeedRowSummary); err != nil {
		_ = db.Close() //stylegate:ignore the fixture is being torn down after a fatal error
		t.Fatalf("seed the fixture row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the seed ledger: %v", err)
	}
	if err := os.Chmod(path, cliSandboxSeedFixturePerm); err != nil {
		t.Fatalf("chmod the seed ledger: %v", err)
	}
	return path
}

// cliSandboxSeedFixtureDDL / …Insert are the fixture's shape and its one row.
// The column set is the minimum the production reader's `summary` query needs;
// it is a FIXTURE, not a second schema authority (the live Rule-31 run seeds
// from a ledger the production tool itself wrote — see the run's evidence).
var (
	cliSandboxSeedFixtureDDL = `CREATE TABLE tasks (
		task_id TEXT PRIMARY KEY,
		summary TEXT NOT NULL
	)`
	cliSandboxSeedFixtureInsert = `INSERT INTO tasks (task_id, summary) VALUES (?, ?)`
)

// cliSandboxTaskSummary reads one row's summary out of a ledger, through the
// SAME driver and DSN the tree opens ledgers with — so the reading is the
// database's own answer, not a byte count and not a substring search of the
// file. An absent row returns "" (the caller compares it against the seed's
// summary, so absence fails there with a readable message).
func cliSandboxTaskSummary(t *testing.T, ledgerPath, taskID string) string {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(ledgerPath))
	if err != nil {
		t.Fatalf("open the sandbox ledger %s: %v", ledgerPath, err)
	}
	defer db.Close() //stylegate:ignore probe handle closed after its single read
	var summary string
	err = db.QueryRow("SELECT summary FROM tasks WHERE task_id = ?", taskID).Scan(&summary)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ""
		}
		t.Fatalf("read %q from %s: %v", taskID, ledgerPath, err)
	}
	return summary
}

// TestSandboxLedgerSeedFlagReachesTheInitializer is the acceptance test for
// deliverable 1: `--ledger-seed PATH` must reach Options.LedgerSeedPath, and the
// only observable that proves it is a row that came from the seed.
func TestSandboxLedgerSeedFlagReachesTheInitializer(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	seed := cliSandboxLedgerSeedFixture(t)

	// The ENVIRONMENT says otherwise, on purpose: the flag must WIN. Without
	// this the test would pass on a binary that ignores the flag and merely
	// happens to read a right-looking environment.
	t.Setenv(sandbox.GopiSandboxLedgerSeedEnvName, "")

	out, code := cliSandboxRun(t, home, root, CliVerbSandbox, SandboxCreateVerb,
		SandboxLabelFlag, "seed-flag", SandboxLedgerSeedFlag, seed)
	if code != 0 {
		t.Fatalf("gopi tool sandbox create %s %s: exit %d\n%s", SandboxLedgerSeedFlag, seed, code, out)
	}
	sb := cliSandboxLastPath(t, out, root)
	ledger := sandbox.LedgerPathIn(sb)

	// (a) The ledger is a REAL file, not a symlink, and not the seed itself.
	info, err := os.Lstat(ledger)
	if err != nil {
		t.Fatalf("the seeded ledger %s does not exist: %v\n%s", ledger, err, out)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the seeded ledger %s is a SYMLINK — the seed is copied BY VALUE (Rule 34)", ledger)
	}
	if info.Mode().IsRegular() == false {
		t.Fatalf("the seeded ledger %s is not a regular file (mode %v)", ledger, info.Mode())
	}

	// (b) The sandbox carries its ownership marker — the property the six
	// hand-built directories lack, and the thing SweepSandboxes reads.
	if isNotExist(t, filepath.Join(sb, config.SandboxOwnerFileName)) {
		t.Fatalf("the seeded sandbox %s has no %s — it is outside the reclaim path",
			sb, config.SandboxOwnerFileName)
	}

	// (c) THE ROW CAME FROM THE SEED. This is the assertion the flag lives or
	// dies by: a parsed-but-dropped flag yields an empty ledger here, exit 0.
	if got := cliSandboxTaskSummary(t, ledger, cliSandboxSeedRowID); got != cliSandboxSeedRowSummary {
		t.Fatalf("the seeded ledger's row %q reads %q, want %q — the flag did not reach "+
			"Options.LedgerSeedPath (the sandbox was created with an empty ledger)\n%s",
			cliSandboxSeedRowID, got, cliSandboxSeedRowSummary, out)
	}
}

// TestSandboxLedgerSeedFlagWinsOverTheEnvironment pins Rule 2's ONE precedence
// site from the outside: Options > env, decided in ledgerSeedPath() and nowhere
// else. The flag must not become a second reader of the environment — it FEEDS
// the options (see the parser's comment), and this test is what fails if someone
// "fixes" a seed problem by adding an os.Getenv to the CLI layer.
func TestSandboxLedgerSeedFlagWinsOverTheEnvironment(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	flagSeed := cliSandboxLedgerSeedFixture(t)
	// A second, DIFFERENT seed through the environment: it names no such file,
	// so a binary that let the env win would fail the create outright (the seed
	// must be a readable database). That makes "which channel won" observable
	// even if both produced some ledger.
	t.Setenv(sandbox.GopiSandboxLedgerSeedEnvName, filepath.Join(t.TempDir(), "does-not-exist.db"))

	out, code := cliSandboxRun(t, home, root, CliVerbSandbox, SandboxCreateVerb,
		SandboxLabelFlag, "seed-flag-wins", SandboxLedgerSeedFlag, flagSeed)
	if code != 0 {
		t.Fatalf("the flag did not outrank %s (the create failed on the env seed): exit %d\n%s",
			sandbox.GopiSandboxLedgerSeedEnvName, code, out)
	}
	sb := cliSandboxLastPath(t, out, root)
	if got := cliSandboxTaskSummary(t, sandbox.LedgerPathIn(sb), cliSandboxSeedRowID); got != cliSandboxSeedRowSummary {
		t.Fatalf("the sandbox ledger's row reads %q, want %q — the env seed won, so the "+
			"precedence is no longer decided in ONE place (Rule 2)\n%s", got, cliSandboxSeedRowSummary, out)
	}
}

// TestSandboxLedgerSeedFlagIsOnTheUsagePage pins deliverable 2: the capability
// is INVISIBLE to a caller who does not already know the environment variable,
// and the usage page is where an agent looks before inventing a directory. The
// page must name the flag, the recipe, and the two scaffolds that do not answer
// the need — a page that merely lists the flag leaves the reader to rediscover
// the empty/read-only trap on their own.
func TestSandboxLedgerSeedFlagIsOnTheUsagePage(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	out, code := cliSandboxRunEnv(t, bin, cliContractEnv(home), CliVerbSandbox, HelpFlag)
	if code != 0 {
		t.Fatalf("gopi tool sandbox %s: exit %d\n%s", HelpFlag, code, out)
	}
	for _, want := range []string{
		SandboxLedgerSeedFlag,
		sandbox.GopiSandboxLedgerSeedEnvName,
		"NewTestDBPath",
		"SampleDBPath",
		"READ-ONLY",
		"UNRECLAIMABLE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the usage page does not carry %q — a caller cannot find the capability, "+
				"which is the whole defect this flag closes:\n%s", want, out)
		}
	}
}

// TestSandboxShowRefusesTheSeedFlagByName keeps `show` honest: it materialises
// nothing, so a seed it accepted would be a flag parsed and ignored (Rule 15).
func TestSandboxShowRefusesTheSeedFlagByName(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	out, code := cliSandboxRunEnv(t, bin, cliContractEnv(home),
		CliVerbSandbox, SandboxShowVerb, SandboxLedgerSeedFlag, "/some/seed.db")
	if code != sandboxExitUsage {
		t.Errorf("`gopi tool sandbox show %s …` exited %d, want the usage status %d:\n%s",
			SandboxLedgerSeedFlag, code, sandboxExitUsage, out)
	}
	if !strings.Contains(out, SandboxLedgerSeedFlag) {
		t.Errorf("the refusal does not name the flag it refused:\n%s", out)
	}
}
