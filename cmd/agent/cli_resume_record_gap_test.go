package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
)

// cli_resume_record_gap_test.go — THE REGRESSION GUARD for row 01a17df9-99c4:
// "a resume that resolves no session record must NEVER succeed silently".
//
// WHAT WAS MEASURED BEFORE THE FIX (isolated profile, this host, 2026-10-28):
//
//	$ gopi chat --resume <run-dir>/<id>.raw.log      # the JSONL deleted
//	[gopi] chat: seeded 1 history message(s) from .../PRUN/cccc1111-….raw.log
//	exit=0
//	$ ls .../PRUN     → cccc1111-….raw.log           (untouched — the "resumed" session)
//	$ ls .../sessions → … PRUN a49c1546-4da1-…       (a BRAND-NEW directory)
//
// The process exited 0, printed no complaint, and wrote the whole run into a
// session nobody asked for. Both halves of that are pinned here: the refusal
// (non-zero, naming the checkpoint, the directory and its CONTENTS) and the
// absence of the artifact (no new directory, no new record).
//
// THE TWO CASES THE EPIC NAMES are covered as separate arms, because they are
// answered by different lines of code and one of them used to be reachable only
// through the other's error path:
//
//   - the run directory EXISTS and holds a checkpoint but no record  → the
//     record gap (resumeIdentityFor reports recordFound=false);
//   - the run directory DOES NOT EXIST (and the id names no session anywhere)
//     → the resolution failure (resolveResumeLogPath).
//
// Both are driven for real, through the built binary, on three faces
// (`gopi --session`, `gopi session resume`, `gopi chat --resume`), because the
// faces are separate addresses over the resume core and a fix that reached only
// one of them would still let the operator's own command line mint a session.

// resumeGapRawLogToken is the placeholder a case's argv uses for the fixture's
// checkpoint path, substituted at run time (the path is not known until the
// temporary home exists).
const resumeGapRawLogToken = "%RAWLOG%"

// resumeGapRunDir lays down a run directory holding ONLY a raw-log checkpoint —
// the "the record was pruned / the jsonl was deleted" shape. It returns the dir
// and the checkpoint's path.
func resumeGapRunDir(t *testing.T, home, runID string) (dir, rawLog string) {
	t.Helper()
	dir = filepath.Join(sessionFlagSessionsDir(t, home), runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	rawLog = filepath.Join(dir, runID+"-checkpoint.raw.log")
	body := "→ REQUEST\n" +
		`{"model":"deepseek-chat","messages":[{"role":"user","content":"work from the old run"}]}` + "\n" +
		"← RESPONSE\n"
	if err := os.WriteFile(rawLog, []byte(body), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	return dir, rawLog
}

// resumeGapAssertNoMint fails when the probe created any session DIRECTORY
// besides the fixture's own. This is the half the epic calls "静默成功这条路径不存在":
// a refusal that still leaves a fresh sessions/<uuid>/ behind has not refused
// the mint, it has only hidden it.
func resumeGapAssertNoMint(t *testing.T, what, home, fixtureRunID string) {
	t.Helper()
	entries, err := os.ReadDir(sessionFlagSessionsDir(t, home))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("%s: read sessions dir: %v", what, err)
	}
	for _, e := range entries {
		if e.Name() == fixtureRunID {
			continue
		}
		t.Fatalf("%s: created session artifact %q — a resume that cannot find a record must mint nothing\n", what, e.Name())
	}
}

// TestResumeRecordGapRefusalNamesPathsAndContents pins the refusal SENTENCE for
// the face the DEFECT was measured on: `gopi chat --resume <checkpoint>`.
//
// The SESSION faces answer this shape DIFFERENTLY, on purpose, and their own arm
// is TestResumeRecordGapSessionFaceRebuildsInPlace below: they rebuild the record
// from the checkpoint under the checkpoint's OWN id and directory, so the session
// the operator named is the session that continues (a recovery, announced — not a
// mint). Only the chat face loses the id, and only the chat face therefore has to
// refuse.
func TestResumeRecordGapRefusalNamesPathsAndContents(t *testing.T) {
	bin := gopiTestBinPath(t)
	const runID = "01a17df9-99c4-resumegap"

	cases := []struct {
		name string
		// argv is the invocation; a literal %RAWLOG% is replaced with the
		// fixture's checkpoint path.
		argv []string
		// progName is the spelling the face puts in its own diagnostics; the
		// refusal carries it so a reader knows which command they typed.
		progName string
	}{
		// The chat face takes the CHECKPOINT PATH — that is the form the
		// defect was measured in (the operator pastes the raw log they still
		// have). A bare run id on this face goes through a different door and
		// is covered by the third test below.
		{
			name:     "chat --resume <checkpoint path>",
			argv:     []string{CliVerbChat, chatFlagResume, resumeGapRawLogToken},
			progName: chatProgLabel + " " + chatFlagResume,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			dir, rawLog := resumeGapRunDir(t, home, runID)
			argv := make([]string, len(tc.argv))
			for i, a := range tc.argv {
				argv[i] = strings.ReplaceAll(a, resumeGapRawLogToken, rawLog)
			}

			out, err := runCLIContractProbeRaw(t, bin, home, argv...)
			if err == nil {
				t.Fatalf("gopi %s exited 0 on a resume with no session record — the silent mint is back\n%s",
					strings.Join(argv, " "), out)
			}
			for _, want := range []struct{ what, needle string }{
				{"the face's own name", tc.progName},
				{"the checkpoint that WAS found", rawLog},
				{"the directory it looked in", dir},
				{"the directory's actual contents", filepath.Base(rawLog)},
				{"the opt-in's name", config.ResumeAllowNewSessionEnv},
			} {
				if !strings.Contains(out, want.needle) {
					t.Errorf("the refusal does not name %s (%q) — an operator cannot act on it:\n%s",
						want.what, want.needle, out)
				}
			}
			resumeGapAssertNoMint(t, "gopi "+strings.Join(argv, " "), home, runID)
		})
	}
}

// TestResumeRecordGapRefusesAnEmptyRunDirectory: the run directory EXISTS with
// nothing in it at all — no record and no checkpoint. A DIFFERENT line answers
// this than the record gap (there is no checkpoint to seed from), and the
// requirement is the same in both directions: it must refuse, and it must mint
// nothing. The two are separate arms on purpose — a fix that only handled "a
// checkpoint with no record" would leave this shape free to mint.
func TestResumeRecordGapRefusesAnEmptyRunDirectory(t *testing.T) {
	bin := gopiTestBinPath(t)
	const runID = "01a17df9-99c4-emptyrundir"
	home := t.TempDir()
	root := sessionFlagSessionsDir(t, home)
	dir := filepath.Join(root, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}

	for _, argv := range [][]string{
		{CliVerbSession, CliVerbResume, runID},
		{sessionResumeFlag, runID},
	} {
		out, err := runCLIContractProbeRaw(t, bin, home, argv...)
		if err == nil {
			t.Fatalf("gopi %s on an EMPTY run directory exited 0 — a resume must never mint\n%s",
				strings.Join(argv, " "), out)
		}
		// THE SHAPE THAT USED TO BE INDISTINGUISHABLE FROM A TYPO. The
		// directory exists — someone pruned the jsonl and the raw log — so the
		// refusal has to say "(empty)" beside the path, not just name the root.
		// Without it an operator cannot tell "my run directory is there and got
		// wiped" from "I typed the wrong id" without running a second command.
		for _, want := range []struct{ what, needle string }{
			{"the sessions root it looked under", root},
			{"the run directory it measured", dir},
			{"that the run directory EXISTS and is empty", "(empty)"},
		} {
			if !strings.Contains(out, want.needle) {
				t.Errorf("gopi %s: the failure does not name %s (%q):\n%s",
					strings.Join(argv, " "), want.what, want.needle, out)
			}
		}
		resumeGapAssertNoMint(t, "gopi "+strings.Join(argv, " "), home, runID)
	}
	// The fixture directory is still empty afterwards: the refusal wrote nothing
	// into the run it declined to resume.
	if rest, err := os.ReadDir(dir); err != nil {
		t.Fatalf("read the fixture run dir: %v", err)
	} else if len(rest) != 0 {
		t.Fatalf("the refused resume wrote %d entries into the run directory it declined", len(rest))
	}
}

// TestResumeRecordGapRefusesANonexistentDirectory is the epic's SECOND case,
// answered by a different line than the record gap: the directory is not there
// at all, so the target names no session anywhere under the root. It must fail
// by naming what it looked under, and (again) mint nothing.
func TestResumeRecordGapRefusesANonexistentDirectory(t *testing.T) {
	bin := gopiTestBinPath(t)
	const unknown = "01a096c9-6f7d-7e43-8e46-9ecca58e9cd0"
	home := t.TempDir()
	root := sessionFlagSessionsDir(t, home)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir sessions root: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, unknown)); !os.IsNotExist(statErr) {
		t.Fatalf("fixture precondition: %s must NOT exist under %s", unknown, root)
	}

	for _, argv := range [][]string{
		{CliVerbSession, CliVerbResume, unknown},
		{sessionResumeFlag, unknown},
		{CliVerbChat, chatFlagResume, unknown},
	} {
		out, err := runCLIContractProbeRaw(t, bin, home, argv...)
		if err == nil {
			t.Fatalf("gopi %s exited 0 for a target that names nothing — a resume must never mint\n%s",
				strings.Join(argv, " "), out)
		}
		if strings.TrimSpace(out) == "" {
			t.Fatalf("gopi %s failed with NO output at all — a silent non-zero exit is the same defect in another costume",
				strings.Join(argv, " "))
		}
		// THE REFUSAL MUST ANSWER THREE QUESTIONS, and this arm pins all three
		// because the epic names them: WHERE it looked (the layouts), WHAT was
		// actually there (the run directory's own listing), and whether anything
		// was started (it was not).
		for _, want := range []struct{ what, needle string }{
			{"the sessions root it looked under", root},
			{"the layout it expected (the run directory's record)", filepath.Join(root, unknown, sessionlayout.RecordName)},
			{"the run directory it measured", filepath.Join(root, unknown)},
			{"that the run directory does not exist", "(the directory does not exist)"},
		} {
			if !strings.Contains(out, want.needle) {
				t.Errorf("gopi %s: the failure does not name %s (%q):\n%s",
					strings.Join(argv, " "), want.what, want.needle, out)
			}
		}
		resumeGapAssertNoMint(t, "gopi "+strings.Join(argv, " "), home, unknown)
	}
}

// TestResumeRecordGapCheckGatesOnTheExplicitOptIn drives the decision function
// itself, so the two directions are pinned without an LLM behind them:
//
//	not asked → REFUSED (non-zero, no mint) — the default the epic requires;
//	asked     → the run PROCEEDS, and the WARNING is what the caller writes
//	            FIRST, before any line reporting what was loaded.
func TestResumeRecordGapCheckGatesOnTheExplicitOptIn(t *testing.T) {
	home := t.TempDir()
	dir, rawLog := resumeGapRunDir(t, home, "01a17df9-99c4-optin")

	t.Run("not asked: refused", func(t *testing.T) {
		t.Setenv(config.ResumeAllowNewSessionEnv, "")
		proceed, code := resumeRecordGapCheck(resumeProgName, "target-x", rawLog)
		if proceed {
			t.Fatal("resumeRecordGapCheck proceeded with no opt-in — the silent mint is back")
		}
		if code == 0 {
			t.Fatalf("the refusal returned exit status 0 — a refusal that exits 0 is the defect itself")
		}
	})

	t.Run("asked: proceeds", func(t *testing.T) {
		t.Setenv(config.ResumeAllowNewSessionEnv, "1")
		proceed, code := resumeRecordGapCheck(resumeProgName, "target-x", rawLog)
		if !proceed {
			t.Fatalf("resumeRecordGapCheck refused with %s=1 — the opt-in does not open the door",
				config.ResumeAllowNewSessionEnv)
		}
		if code != 0 {
			t.Fatalf("the proceed path returned exit status %d, want 0", code)
		}
	})

	// The directory is untouched by either decision: this function decides,
	// it never writes. The mint (or its absence) is the caller's act.
	if entries, err := os.ReadDir(dir); err != nil {
		t.Fatalf("read the fixture run dir: %v", err)
	} else if len(entries) != 1 {
		t.Fatalf("the decision function wrote into the run directory: %d entries, want 1 (the checkpoint)", len(entries))
	}
}

// TestResumeRecordGapWarningIsTheFirstLine pins the ORDER the epic asks for:
// when the operator opts in, the correction is the FIRST thing printed, not a
// footnote under a report of what was successfully loaded. A warning that
// arrives after the seed line reads as an aside to a success.
func TestResumeRecordGapWarningIsTheFirstLine(t *testing.T) {
	bin := gopiTestBinPath(t)
	const runID = "01a17df9-99c4-warnfirst"
	home := t.TempDir()
	_, rawLog := resumeGapRunDir(t, home, runID)

	// The RUN itself cannot be completed here (no endpoint), so this arm reads
	// what the process says BEFORE it gets that far: the refusal is gone
	// (opted in) and the WARNING is line one of its output.
	out, probeErr, timedOut := cliContractProbeWithEnv(bin, home, cliContractTimeout,
		[]string{config.ResumeAllowNewSessionEnv + "=1"},
		CliVerbChat, chatFlagResume, rawLog)
	if !timedOut && probeErr == nil {
		// Without an endpoint this run cannot COMPLETE: past the gate it fails at
		// the model client. An exit of 0 means it never got there, so the warning
		// this arm asserts would be the output of a different (shorter) run.
		t.Fatalf("the opted-in resume exited 0 although no endpoint is configured — it cannot have reached the "+
			"model call this arm's window assumes:\n%s", out)
	}
	if timedOut {
		// A run that got past the gate and then waited on an endpoint is the
		// SUCCESS shape for this arm — but its output must still have led with
		// the warning, so fall through to the assertion below.
		t.Logf("the opted-in run reached the model call and was cut by the probe bound (expected without an endpoint)")
	}
	first := firstUsageLine(out)
	if !strings.HasPrefix(first, "WARNING:") {
		t.Fatalf("the opted-in resume did not LEAD with the warning — first line is %q\nfull output:\n%s", first, out)
	}
	if !strings.Contains(first, config.ResumeAllowNewSessionEnv) {
		t.Errorf("the warning does not name the switch that armed it (%s):\n%s", config.ResumeAllowNewSessionEnv, first)
	}
	if strings.Contains(out, "but no session RECORD to continue") {
		t.Errorf("the opted-in run still printed the refusal — the opt-in did not open the door:\n%s", out)
	}
}

// TestResumeRecordGapSessionFaceRebuildsInPlace is the OTHER answer, pinned so a
// later change cannot collapse the two: on the session faces a run directory that
// holds a checkpoint but no record is a RECOVERY, not a gap — the record is
// rebuilt under the checkpoint's own run id and in the same directory, so the
// session the operator named is the one that continues. The refusal would be
// wrong here (it would throw away a session that can still be continued), and
// silence would be wrong too: the operator is told the record was rebuilt, in
// words that state the id and the directory are the SAME.
//
// The run then fails at the model client (no endpoint in this profile), which is
// why the probe's non-zero exit is NOT the assertion: what is measured is the
// announcement and the SESSION TREE.
func TestResumeRecordGapSessionFaceRebuildsInPlace(t *testing.T) {
	bin := gopiTestBinPath(t)
	const runID = "01a17df9-99c4-rebuild"
	home := t.TempDir()
	dir, rawLog := resumeGapRunDir(t, home, runID)

	out, probeErr := runCLIContractProbeRaw(t, bin, home, CliVerbSession, CliVerbResume, rawLog)
	// The probe's non-zero exit is NOT this arm's assertion (the run fails at the
	// model client: no endpoint in this profile) — but its ABSENCE is: a face that
	// exited 0 while the run directory held no record is the silent mint this file
	// exists to catch, and it would otherwise pass on the announcement alone.
	if probeErr == nil {
		t.Fatalf("the session resume face exited 0 on a run directory holding no session record:\n%s", out)
	}
	for _, want := range []struct{ what, needle string }{
		{"the recovery announcement", "held no session RECORD"},
		{"that the id and directory are the SAME", "SAME id and directory"},
		{"the directory that continues", dir},
	} {
		if !strings.Contains(out, want.needle) {
			t.Errorf("the session face must announce the recovery, naming %s (%q):\n%s",
				want.what, want.needle, out)
		}
	}
	if strings.Contains(out, "no session RECORD to continue") {
		t.Errorf("the session face must NOT refuse a checkpoint it can rebuild in place:\n%s", out)
	}
	// Nothing was minted beside it: the run directory is still the only session
	// artifact, and it is the same one.
	resumeGapAssertNoMint(t, "gopi "+CliVerbSession+" "+CliVerbResume, home, runID)
}

// TestResumeCheckpointNamesARunDirPinsTheAddress is THE REGRESSION GUARD for the
// function's SECOND half — the one that used to be a comment.
//
// WHAT WAS WRONG (found 2026-10-30 in row 01a1847e's epic, measured against this
// host's own sessions root). The function ended with
//
//	if filepath.Dir(dir) != rootAbs { return false }
//	// ... Require the directory to BE an address: the run id names it.
//	return strings.TrimSpace(filepath.Base(dir)) != ""
//
// The last line is TRUE BY CONSTRUCTION once the comparison above it has passed:
// a direct child of an absolute root path cannot render an empty base name. So
// the doc comment promised a second check ("the run id names it") and the code
// answered yes unconditionally — a promise with no check behind it, in the very
// file whose purpose is to stop that shape (Rule 15).
//
// The fix REMOVED the tautology and stated the real criterion in one comparison.
// This arm pins the criterion, and — this is the arm's whole value — it pins the
// answer for the shape a NAIVE "make it strict" fix would have broken: the RETRY
// container (`…-r1` / `…-r2` / `…-c1`), which is NOT a canonical uuid and is
// nevertheless a real run directory. Measured on this host: of 118 non-canonical
// children of the sessions root, 46 hold a checkpoint, and they are all retry
// containers minted by the task lifecycle. A strict-uuid test here would refuse
// to resume a real retry's session — the over-blocking defect, which is worse
// than the tautology this arm guards.
//
// The root is PINNED with config.SessionsRootEnv, which is the ONE channel the
// function resolves through (config.SessionsRoot) — no hand-rolled root, no
// GopiDir dance, so the fixture and the function cannot disagree about where the
// tree is (Rules 13/14/32's spirit: one derivation).
func TestResumeCheckpointNamesARunDirPinsTheAddress(t *testing.T) {
	root := t.TempDir()
	t.Setenv(config.SessionsRootEnv, root)

	// A checkpoint under a directory of the given name, laid down for real.
	mk := func(name string) string {
		t.Helper()
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		log := filepath.Join(dir, name+"-checkpoint.raw.log")
		if err := os.WriteFile(log, []byte("→ REQUEST\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", log, err)
		}
		return log
	}

	canonical := mk("01a17df9-99c4-7b4f-8485-bd5555e2b0ce")
	retry := mk("01a17a65-5aed-7ba2-bc70-84f024ffa7b3-r2")
	collector := mk("01a17a10-04a9-7770-b616-84b283d0242e-c1")

	// A loose file IN the root itself: its parent IS the root, so the directory
	// above it is not an entry of the root at all. This is the shape the parent
	// comparison alone must reject — the old tautological line could not have
	// helped here either, which is exactly why it was dead weight.
	loose := filepath.Join(root, "loose-export.raw.log")
	if err := os.WriteFile(loose, []byte("→ REQUEST\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", loose, err)
	}

	// One level too deep: a checkpoint in a subdirectory OF a run directory is
	// not that run's checkpoint.
	deepDir := filepath.Join(root, "01a17df9-99c4-7b4f-8485-bd5555e2b0ce", "artifacts")
	if err := os.MkdirAll(deepDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", deepDir, err)
	}
	deep := filepath.Join(deepDir, "nested.raw.log")
	if err := os.WriteFile(deep, []byte("→ REQUEST\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", deep, err)
	}

	// A checkpoint outside this profile's tree altogether (a copy, an export).
	outside := filepath.Join(t.TempDir(), "copied.raw.log")
	if err := os.WriteFile(outside, []byte("→ REQUEST\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", outside, err)
	}

	// The SAME run directory, spelled relatively: the address test is about
	// where the file lands, not about how the operator spelled it (filepath.Abs
	// is part of the check, not a second gate). The detour is resolved from the
	// TEST's own working directory, because that is what filepath.Abs resolves
	// against — a path relative to the fixture root would name a different file
	// and would measure the fixture, not the function.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	detour, err := filepath.Rel(cwd, canonical)
	if err != nil {
		t.Fatalf("rel %s: %v", canonical, err)
	}

	for _, tc := range []struct {
		what string
		path string
		want bool
	}{
		{"a canonical run id's own checkpoint", canonical, true},
		{"a RETRY container's checkpoint (…-r2) — not a uuid, still a real run directory", retry, true},
		{"a COLLECTOR container's checkpoint (…-c1)", collector, true},
		{"a file lying loose in the sessions root (names no run)", loose, false},
		{"a checkpoint nested one level inside a run directory", deep, false},
		{"a checkpoint outside this profile's sessions tree", outside, false},
		{"the same run checkpoint spelled relatively", detour, true},
	} {
		if got := resumeCheckpointNamesARunDir(tc.path); got != tc.want {
			t.Errorf("resumeCheckpointNamesARunDir(%q) = %v, want %v — %s",
				tc.path, got, tc.want, tc.what)
		}
	}

	// The early return for an UNRESOLVABLE root is NOT driven here, and the
	// reason is stated rather than hidden: config.SessionsRoot only fails when
	// the profile dir cannot be resolved at all, which needs the process's home
	// resolution to fail — something a test cannot provoke through t.Setenv
	// without mutating the user record. Forcing it with an invalid path spelling
	// is REFUSED by os.Setenv itself (measured: `setenv: invalid argument`), so a
	// subtest that "pinned" it would be pinning the tooling's refusal, not the
	// function's. What IS pinned here is the reachable direction: a root that
	// resolves but does not contain the checkpoint answers false (the `loose`,
	// `deep` and `outside` arms above), which is the same refusal an operator
	// would see.
}

// TestResumeFreshSessionNoticeNamesTheRealPlace pins the fix for row 01a18d6c's
// epic: the notice printed for a checkpoint that is NOT a run directory used to
// carry ONE fixed sentence — "the checkpoint sits outside the sessions root" —
// for every cause, including the cause where the checkpoint's parent IS the
// sessions root.
//
// MEASURED BEFORE THE FIX (this host, 2026-10-30):
//
//	$ gopi session resume <sessions-root>/loose-export.raw.log
//	… is not a run directory of this profile (the checkpoint sits outside the
//	  sessions root), so it names no session to continue …
//
// The gate answered FALSE correctly (a file lying loose among the run
// directories is not one of them), but the reason it printed contradicted the
// filesystem: the checkpoint was IN the root. Two different false answers shared
// one string — Rule 15's own defect, one sentence covering two causes.
//
// THE THREE SHAPES ARE ASSERTED ONE BY ONE, because the classifier branches on
// the checkpoint's real position and a single arm would let the other two
// collapse back onto one sentence without a red:
//
//  1. loose IN the root           → says so, and must NOT say "outside"
//  2. BELOW a run directory       → names the run directory it is under
//  3. outside the tree altogether → the one shape allowed to say "outside"
//
// The assertions are on the RENDERED LINE (what an operator reads), not on the
// classifier alone: a classifier that answered right and a switch that printed
// the wrong case would satisfy a classifier-only test (Rule 31 — execute the
// surface).
func TestResumeFreshSessionNoticeNamesTheRealPlace(t *testing.T) {
	root := t.TempDir()
	t.Setenv(config.SessionsRootEnv, root)
	const sessionID = "11111111-2222-3333-4444-555555555555"
	const prog = "gopi session resume"

	write := func(dir, name string) string {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("→ REQUEST\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return path
	}

	// ① A loose checkpoint IN the sessions root: its parent IS the root.
	loose := write(root, "loose-export.raw.log")
	looseLine := resumeFreshSessionNotice(prog, loose, sessionID)
	if strings.Contains(looseLine, "sits outside the sessions root") {
		t.Errorf("① a checkpoint loose in the root must NOT be called 'outside the sessions root' — the operator goes looking where the file demonstrably is not:\n%s", looseLine)
	}
	if !strings.Contains(looseLine, "lies loose IN the sessions root "+root) {
		t.Errorf("① the loose-in-root notice must name the root and say the file is in it:\n%s", looseLine)
	}
	if !strings.Contains(looseLine, loose) || !strings.Contains(looseLine, sessionID) {
		t.Errorf("① the notice must name the checkpoint and the session it starts:\n%s", looseLine)
	}

	// ② A checkpoint BELOW a run directory (a capture under artifacts/): the run
	// directory above it is named, so the reader knows which run the file belongs
	// to and that it is not that run's own checkpoint.
	runDir := filepath.Join(root, "01a17df9-99c4-7b4f-8485-bd5555e2b0ce")
	below := write(filepath.Join(runDir, "artifacts"), "captured.raw.log")
	belowLine := resumeFreshSessionNotice(prog, below, sessionID)
	if strings.Contains(belowLine, "sits outside the sessions root") {
		t.Errorf("② a checkpoint below a run directory is INSIDE the sessions root — the outside sentence is false there:\n%s", belowLine)
	}
	if !strings.Contains(belowLine, "BELOW the run directory "+runDir) {
		t.Errorf("② the nested notice must name the run directory it sits under:\n%s", belowLine)
	}

	// ③ A checkpoint outside this profile's tree: the ONLY shape allowed to say
	// "outside the sessions root", and it prints the root so the claim is
	// checkable.
	outside := write(t.TempDir(), "copied.raw.log")
	outsideLine := resumeFreshSessionNotice(prog, outside, sessionID)
	if !strings.Contains(outsideLine, "sits outside the sessions root "+root) {
		t.Errorf("③ an outside-the-tree checkpoint must say so AND name the root it is outside of:\n%s", outsideLine)
	}

	// The three lines are three DIFFERENT sentences. This is the property the
	// defect lacked: if any two comparisons collapse, the branch was not real.
	if looseLine == outsideLine || looseLine == belowLine || belowLine == outsideLine {
		t.Errorf("the three shapes must render three different notices:\n  loose:   %s\n  below:   %s\n  outside: %s", looseLine, belowLine, outsideLine)
	}

	// …and every one of them carries the fact that this run does NOT continue
	// the named session, which is why the notice exists at all (Rule 15).
	for what, line := range map[string]string{
		"loose in root": looseLine, "below a run dir": belowLine, "outside the tree": outsideLine,
	} {
		if !strings.Contains(line, "STARTS session "+sessionID) {
			t.Errorf("%s: every shape must say the run STARTS its own session:\n%s", what, line)
		}
	}
}

// TestResumeCheckpointLocaleAgreesWithTheGate is the BRIDGE arm: the classifier
// that renders the notice and the gate that decides whether to print it must
// agree on the one question they share — "is this a run directory of this
// profile?" — for every shape, or the notice would describe a different
// filesystem than the one the gate looked at.
//
// It is deliberately NOT a re-derivation of the gate's criterion: it asserts the
// two answers stay in step, so the gate's tested boundary (including the RETRY
// container, which is not a canonical uuid) cannot be loosened by a future edit
// to the classifier.
func TestResumeCheckpointLocaleAgreesWithTheGate(t *testing.T) {
	root := t.TempDir()
	t.Setenv(config.SessionsRootEnv, root)

	mk := func(dir, name string) string {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("→ REQUEST\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return path
	}

	retryDir := filepath.Join(root, "01a17a65-5aed-7ba2-bc70-84f024ffa7b3-r2")
	for _, tc := range []struct {
		what string
		path string
		want resumeCheckpointLocale
	}{
		{"a canonical run directory", mk(filepath.Join(root, "01a17df9-99c4-7b4f-8485-bd5555e2b0ce"), "run.raw.log"), resumeCheckpointLocaleInRunDir},
		{"a RETRY container (…-r2) — not a uuid, still a run directory", mk(retryDir, "retry.raw.log"), resumeCheckpointLocaleInRunDir},
		{"a checkpoint loose in the root", mk(root, "loose.raw.log"), resumeCheckpointLocaleLooseInRoot},
		{"a checkpoint below a run directory", mk(filepath.Join(retryDir, "artifacts"), "nested.raw.log"), resumeCheckpointLocaleBelowRunDir},
		{"a checkpoint outside the tree", mk(t.TempDir(), "copied.raw.log"), resumeCheckpointLocaleOutsideRoot},
	} {
		got, _, err := resumeCheckpointLocaleOf(tc.path)
		if err != nil {
			t.Fatalf("%s: resumeCheckpointLocaleOf(%q): %v", tc.what, tc.path, err)
		}
		if got != tc.want {
			t.Errorf("%s: locale = %v, want %v", tc.what, got, tc.want)
		}
		// THE BRIDGE: the gate is true exactly on the InRunDir locale.
		if gate := resumeCheckpointNamesARunDir(tc.path); gate != (got == resumeCheckpointLocaleInRunDir) {
			t.Errorf("%s: gate=%v but locale=%v — the notice would describe a different filesystem than the gate judged",
				tc.what, gate, got)
		}
	}
}
