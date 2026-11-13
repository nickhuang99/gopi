package tools

// The /proc/<pid>/stat parse has ONE implementation — procname.StatField — and
// three former copies now DELEGATE to it (internal/scratch's
// ParseProcStartTime, this package's procStartTicks and
// finishCleanupProcParent). The delegation is a rule, not a convention:
// a re-inlined "count the fields after the last ')'" body is a FOURTH copy of
// the trap, and the trap's failure mode is silent (a wrong tick column reads as
// a plausible number, so nothing notices until a liveness verdict is wrong).
//
// This gate is the mechanical half of Rules 13/14 for that parse:
//   1. the delegations are PRESENT (a refactor that drops the call and keeps a
//      local body cannot pass by deleting the import), and
//   2. the parse BODY is absent from every production file but its one home,
//      detected by the shape of the copy rather than by a comment.
//
// It deliberately scans PRODUCTION files only: a test may legitimately replay
// the naive body to prove it misreads (see
// internal/procname's TestNaiveWhitespaceSplitMisreadsTheTrapLine).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// procStatParseGate is one delegating site: the file, the call that proves it
// delegates, and the trap-shaped text that must NOT survive in it.
type procStatParseGate struct {
	rel         string
	delegation  string
	forbidden   []string
	description string
}

var procStatParseGateSites = []procStatParseGate{
	{
		rel:        "internal/scratch/holder.go",
		delegation: "procname.StatStartTime(",
		forbidden:  []string{`LastIndex(raw, ")")`},
		description: "internal/scratch.ParseProcStartTime — the (0, error) shape the holder " +
			"registration reports (Rule 15: no fabricated zero)",
	},
	{
		rel:        "internal/tools/taskproc.go",
		delegation: "procname.StatFieldString(",
		forbidden:  []string{`LastIndexByte(b, ')')`, `LastIndex(string(b), ")")`},
		description: "tools.procStartTicks — the task row's incarnation (" +
			"its own n<=0 guard kept on the caller side)",
	},
	{
		rel:         "internal/tools/finish_cleanup_survivors.go",
		delegation:  "procname.StatFieldString(",
		forbidden:   []string{`LastIndex(string(b), ")")`, `LastIndexByte(b, ')')`},
		description: "tools.finishCleanupProcParent — the parent column (field 4) of the same parse " +
			"(moved here from task_finish_cleanup.go by 58b5618, which retired the mechanism " +
			"and kept this symbol on a live reader's path)",
	},
	{
		rel:         "internal/config/agentguard.go",
		delegation:  "procname.StatFieldString(",
		forbidden:   []string{`LastIndexByte(line, ')')`},
		description: "config.parentIsWorkerGopi — the parent column of the spawn guard",
	},
	{
		rel:         "internal/testutil/procident.go",
		delegation:  "procname.StatField(",
		forbidden:   []string{`LastIndexByte(raw, ')')`},
		description: "testutil.parseProcStat — the identity triple (starttime/ppid/session/state)",
	},
	{
		rel:         "internal/daemoncwd/daemoncwd.go",
		delegation:  "procname.StatFieldString(",
		forbidden:   []string{`LastIndex(s, ")")`},
		description: "daemoncwd.ParentOf — the daemon ancestry walk's parent column",
	},
	{
		rel:         "internal/signalrec/signalrec.go",
		delegation:  "procname.StatField(",
		forbidden:   []string{`LastIndex(raw, ")")`},
		description: "signalrec.sidOf — the session field of the signal record",
	},
	{
		rel:         "cmd/agent/cli_worker_status.go",
		delegation:  "procname.StatFieldString(",
		forbidden:   []string{`LastIndex(stat, ")")`},
		description: "cmd/agent.workerDetached — the session field of the worker-status fallback",
	},
}

// TestProcStatParseHasOneImplementation is the gate. It fails when a delegating
// site stops delegating, or when a production file re-inlines the
// count-from-the-last-')' stat parse.
func TestProcStatParseHasOneImplementation(t *testing.T) {
	root := toolsRepoRoot(t)

	// --- half 1: the single home really holds the parse ---------------------
	home := filepath.Join(root, "internal/procname/procname.go")
	homeSrc := readProductionFile(t, home)
	if n := strings.Count(homeSrc, "\nfunc StatField("); n != 1 {
		t.Fatalf("%s must hold EXACTLY one StatField definition (found %d) — it is the ONE "+
			"implementation the whole repo delegates to (Rules 13/14)", home, n)
	}
	if !strings.Contains(homeSrc, `LastIndex(statLine, ")")`) {
		t.Fatalf("%s no longer contains the count-from-the-LAST-')' parse — the trap guard "+
			"(a comm carrying spaces/parentheses) must live at the single definition", home)
	}

	// --- half 2: each former copy delegates, none re-inlines the body -------
	for _, site := range procStatParseGateSites {
		src := readProductionFile(t, filepath.Join(root, site.rel))
		if !strings.Contains(src, site.delegation) {
			t.Errorf("%s must delegate to %s (%s) — the parse has ONE home, internal/procname",
				site.rel, site.delegation, site.description)
		}
		for _, bad := range site.forbidden {
			if strings.Contains(src, bad) {
				t.Errorf("%s carries its own stat-parse body (%q) — that is a FOURTH copy of the "+
					"comm trap; delegate to procname.StatField/StatFieldString/StatStartTime (Rules 13/14)",
					site.rel, bad)
			}
		}
	}

	// --- half 3: no OTHER production file re-inlines the shape --------------
	scanned := scanForReinlinedStatParse(t, root, home)
	if scanned < productionFileReadFloor {
		t.Fatalf("only %d production files were scanned (floor %d) — the scan lost its source tree "+
			"and would pass green over a re-inlined parse", scanned, productionFileReadFloor)
	}
}

// productionFileReadFloor is the read floor of the tree scan: the repo's
// internal/ + cmd/ production surface is hundreds of files, and a scan that
// finds far fewer has a broken walk rather than a clean tree (Rule 13's
// read-floor precedent — a gate that reads nothing must fail, never pass).
const productionFileReadFloor = 200

// scanForReinlinedStatParse walks every production .go file under internal/ and
// cmd/ (the single home excepted) and fails on the COPY SHAPE: a
// LastIndex-of-')' line followed within a few lines by a strings/bytes Fields
// split of the same buffer — i.e. someone counting stat fields by hand again.
// Comments are not excluded on purpose: the shape is a code shape, and the
// message tells the reader what to do instead.
func scanForReinlinedStatParse(t *testing.T, root, home string) int {
	t.Helper()
	scanned := 0
	for _, dir := range []string{"internal", "cmd"} {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if path == home {
				return nil // the ONE definition site
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			scanned++
			rel, _ := filepath.Rel(root, path) //stylegate:ignore path arithmetic in a fixture; the compared string is the assertion
			lines := strings.Split(string(src), "\n")
			for i, line := range lines {
				if !strings.Contains(line, "LastIndex") || !strings.Contains(line, "')'") {
					continue
				}
				for j := i + 1; j < len(lines) && j <= i+procStatParseScanWindow; j++ {
					if strings.Contains(lines[j], "Fields(") {
						t.Errorf("%s:%d re-inlines the /proc/<pid>/stat parse (a LastIndex of ')' "+
							"feeding a Fields split at line %d) — that is a FOURTH copy of the comm trap; "+
							"delegate to internal/procname.StatField/StatFieldString (Rules 13/14)",
							rel, i+1, j+1)
						break
					}
				}
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("scan %s: %v", dir, walkErr)
		}
	}
	return scanned
}

// procStatParseScanWindow is how many lines after the ')' search a Fields split
// still counts as the same hand-rolled parse (the copies were 4-6 lines long).
const procStatParseScanWindow = 6

// toolsRepoRoot locates the repo root from this package's working directory.
func toolsRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	return root
}

// readProductionFile reads one production file and refuses an empty read (the
// read-floor half: an unreadable file must fail the gate, never satisfy it).
func readProductionFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(b) == 0 {
		t.Fatalf("%s read back empty — the gate cannot judge a file it did not read", path)
	}
	return string(b)
}
