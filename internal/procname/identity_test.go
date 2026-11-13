package procname

// The contract of the ONE identity predicate: three factors, FOUR outcomes, and
// a named unmeasurable that never shares a value with "not live" (Rule 15a).
//
// These are synthetic-procfs tests: RunnerIdentity takes the procfs root as a
// parameter precisely so the production ladder can be driven over a tree the
// test controls, including the shape that cannot be produced on a live host at
// will — a process DIRECTORY that exists while its stat file cannot be read.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	// identityFixturePid is the pid slot the synthetic trees use.
	identityFixturePid = "4242"
	// identityFixtureComm is the fixture process's comm. It carries parentheses
	// and a space on purpose: the parse must count fields after the LAST ')'
	// (a comm containing them shifts every later column).
	identityFixtureComm = "probe (gopi) worker"
	// identityFixtureTicks is the fixture's starttime (stat field 22).
	identityFixtureTicks = "900001"
	// identityFixtureOtherTicks is a DIFFERENT incarnation at the same pid — the
	// pid-reuse shape.
	identityFixtureOtherTicks = "900002"
	// identityFixtureState is the fixture's state letter: running.
	identityFixtureState = "R"
)

// writeIdentityFixture builds a synthetic procfs tree holding one running
// process, and returns its root. statUnreadable chmods the stat file to 0000,
// which is how a test produces "the directory is there, the kernel did not
// answer" without root (the running test process is not root).
func writeIdentityFixture(t *testing.T, statUnreadable bool) (root string, argv []string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, identityFixturePid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("fixture: mkdir %s: %v", dir, err)
	}
	argv = []string{"/usr/bin/" + NameGopi, VerbWorker, "run"}
	if err := os.WriteFile(filepath.Join(dir, CmdlineName),
		[]byte(strings.Join(argv, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatalf("fixture: write cmdline: %v", err)
	}
	// The stat line is built through the SAME field numbering the reader uses,
	// so the fixture cannot drift from the parser: the state letter is field 3
	// and starttime field 22.
	fields := make([]string, StatFieldStartTime)
	for i := range fields {
		fields[i] = "0"
	}
	fields[StatFieldState-1] = identityFixtureState
	fields[StatFieldStartTime-1] = identityFixtureTicks
	line := identityFixturePid + " (" + identityFixtureComm + ") " + strings.Join(fields[StatFieldState-1:], " ") + "\n"
	statPath := filepath.Join(dir, StatName)
	if err := os.WriteFile(statPath, []byte(line), 0o644); err != nil {
		t.Fatalf("fixture: write stat: %v", err)
	}
	if statUnreadable {
		if err := os.Chmod(statPath, 0o000); err != nil {
			t.Fatalf("fixture: chmod stat: %v", err)
		}
		// A chmod-000 file on this host is only unreadable for a non-root EUID;
		// if the suite ever runs as root the fixture silently measures nothing,
		// so the test says so rather than passing vacuously.
		if _, err := os.ReadFile(statPath); err == nil {
			t.Skip("running as a user that can read a 0000 file (root?) — the unmeasurable fixture cannot be built")
		}
	}
	return root, argv
}

// TestUnmeasurableNeverRendersAsNotLive is the Rule 15a contract: a factor the
// kernel did not answer is a NAMED unmeasurable, and a caller reading the
// boolean form gets a reason — never a bare "not live" it would treat as a
// death proof (the answer the destructive reclaim/release arms act on).
func TestUnmeasurableNeverRendersAsNotLive(t *testing.T) {
	root, _ := writeIdentityFixture(t, true)
	want := Liveness{Pid: identityFixturePid, StartTicks: mustTicks(t, identityFixtureTicks)}

	outcome, why := RunnerIdentity(root, want)
	if outcome != OutcomeUnmeasurable {
		t.Fatalf("an unreadable stat answered %q, want %q — the process directory IS there, so the kernel was "+
			"silent and the answer is UNKNOWN, never a measured death", outcome, OutcomeUnmeasurable)
	}
	if why != UnmeasurableNoProcfs {
		t.Errorf("the unmeasurable answer carried reason %q, want %q — an unmeasurable outcome must NAME which "+
			"factor was missing", why, UnmeasurableNoProcfs)
	}
	live, whyBool := RunnerLive(root, want)
	if live {
		t.Fatalf("RunnerLive reported a live runner for an unreadable stat — a fact the kernel never stated")
	}
	if whyBool == "" {
		t.Fatalf("RunnerLive answered not-live with an EMPTY reason for an unreadable stat — that is "+
			"%q-shaped: the caller cannot tell 'the kernel did not answer' (%q) from 'the runner is %q'",
			OutcomeGone, OutcomeUnmeasurable, OutcomeGone)
	}

	// The CONTROL, on the same fixture shape with a readable stat: a MISMATCHED
	// incarnation is a MEASURED not-live — the outcome is named, and the boolean
	// form carries no reason. Both halves are asserted, so the test proves the
	// distinction rather than one side of it.
	readable, _ := writeIdentityFixture(t, false)
	recycled := Liveness{Pid: identityFixturePid, StartTicks: mustTicks(t, identityFixtureOtherTicks)}
	if outcome, why := RunnerIdentity(readable, recycled); outcome != OutcomeRecycled || why != "" {
		t.Errorf("a differing incarnation answered (%q, %q), want (%q, \"\") — a MEASURED not-live must not be "+
			"reported as unmeasurable, or the two states become one", outcome, why, OutcomeRecycled)
	}
	if live, why := RunnerLive(readable, recycled); live || why != "" {
		t.Errorf("RunnerLive answered (%v, %q) for a differing incarnation, want (false, \"\")", live, why)
	}

	// A record that states NO factor is unmeasurable too: a pid alone is a NUMBER
	// the kernel recycles, so neither "live" (a stranger) nor "gone" (a fabricated
	// death) is a measured answer.
	if outcome, why := RunnerIdentity(readable, Liveness{Pid: identityFixturePid}); outcome != OutcomeUnmeasurable || why != UnmeasurableNoRecord {
		t.Errorf("a record with no factor answered (%q, %q), want (%q, %q)", outcome, why, OutcomeUnmeasurable, UnmeasurableNoRecord)
	}
}

// TestIncarnationWinsOverTheCommandLine pins the rule the two former ladders
// disagreed about: once an incarnation is recorded, a differing command line is
// the SAME process (an in-place exec), and the command line is an axis only for
// a record that has no incarnation.
func TestIncarnationWinsOverTheCommandLine(t *testing.T) {
	root, argv := writeIdentityFixture(t, false)
	ticks := mustTicks(t, identityFixtureTicks)

	// Matching incarnation + a totally different argv ⇒ LIVE (the runner
	// re-exec'd in place). This is the assertion whose opposite used to live in
	// this package, while the ledger's ladder answered "running" for the same
	// record — one question, two answers.
	sameIncarnation := Liveness{Pid: identityFixturePid, StartTicks: ticks,
		Cmdline: []string{"/bin/sh", "-c", "sleep 30"}}
	if outcome, _ := RunnerIdentity(root, sameIncarnation); outcome != OutcomeLive {
		t.Errorf("a matching incarnation with a differing argv answered %q, want %q — the tick IS the "+
			"incarnation, so this is the same process with a replaced image", outcome, OutcomeLive)
	}
	// NO incarnation recorded ⇒ the command line is the only factor: the argv
	// ROLE check still separates a worker from the daemon that re-execs the same
	// binary (the worker count's own question).
	roleOnly := Liveness{Pid: identityFixturePid, Cmdline: []string{NameGopi, VerbTask}}
	if outcome, _ := RunnerIdentity(root, roleOnly); outcome != OutcomeRecycled {
		t.Errorf("a record with no incarnation and a different argv answered %q, want %q — with no tick the argv "+
			"is the only factor there is", outcome, OutcomeRecycled)
	}
	// NO incarnation, argv that MATCHES ⇒ live: the fixture is worker-shaped.
	if outcome, _ := RunnerIdentity(root, Liveness{Pid: identityFixturePid, Cmdline: argv}); outcome != OutcomeLive {
		t.Errorf("a matching argv with no incarnation answered %q, want %q", outcome, OutcomeLive)
	}
}

// TestWorkersWhoseWorkerVerdictAndPredicateAgree keeps the worker count's value
// set and the ONE predicate from drifting: every case the count's vocabulary is
// still exactly the predicate's mapping, so a caller reading Verdict and a caller
// reading RunnerIdentity can never answer differently about one record.
func TestWorkersWhoseWorkerVerdictAndPredicateAgree(t *testing.T) {
	root, argv := writeIdentityFixture(t, false)
	// A pid-absent record is a MEASURED not-live (the process directory is gone),
	// which the count's vocabulary spells the same way it spells a reused number:
	// to a caller with no ledger, "gone" and "recycled" are one answer.
	absentPid := strconv.Itoa(1 << 28)
	cases := []struct {
		want string
		rec  Liveness
	}{
		{VerdictAlive, Liveness{Pid: identityFixturePid, StartTime: identityFixtureTicks, Cmdline: argv}},
		{VerdictRecycled, Liveness{Pid: identityFixturePid, StartTime: identityFixtureOtherTicks}},
		{VerdictRecycled, Liveness{Pid: absentPid, StartTime: identityFixtureTicks}},
		{VerdictUnknown, Liveness{Pid: identityFixturePid}},
	}
	for _, c := range cases {
		if got := Verdict(root, c.rec); got != c.want {
			t.Errorf("Verdict(%+v) = %q, want %q", c.rec, got, c.want)
		}
		outcome, why := RunnerIdentity(root, c.rec)
		live, rwhy := RunnerLive(root, c.rec)
		if (outcome == OutcomeLive) != live || (why != "") != (rwhy != "") {
			t.Errorf("the value set and the predicate disagree for %+v: RunnerIdentity=(%q,%q) "+
				"RunnerLive=(%v,%q) — one comparison must produce one answer", c.rec, outcome, why, live, rwhy)
		}
	}
}

// mustTicks parses one fixture tick count through the package's own conversion,
// so the fixture and the reader cannot spell a number differently.
func mustTicks(t *testing.T, raw string) int64 {
	t.Helper()
	n, ok := parseTickCount(raw)
	if !ok {
		t.Fatalf("fixture tick %q does not parse", raw)
	}
	return n
}
