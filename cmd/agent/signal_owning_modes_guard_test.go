package main

// signal_owning_modes_guard_test.go — the machine guard for the ONE premise the
// signal recorder rests on, and the per-mode regression tests for the gaps this
// epic closed.
//
// THE PREMISE. internal/signalrec registers SIGTERM/SIGINT/SIGHUP/SIGUSR1/
// SIGUSR2 with os/signal for EVERY gopi verb (watchedSignals). Registering a
// signal with os/signal REPLACES its kernel default disposition, so from that
// moment the process no longer dies on it by itself. The recorder then decides,
// per mode, whether to RE-RAISE — and modesThatOwnTheirSignalHandlers is the
// list of modes it leaves alone, on the stated ground that "this mode owns its
// own handlers". That sentence is only true if the mode's own signal.Notify
// really covers what the recorder took over.
//
// WHAT WENT WRONG WHEN NOBODY CHECKED (measured, task 01a15bb9 and this epic).
// worker's set lacked SIGHUP; chat's set lacked SIGHUP; serve/web/resume/
// worker-control had no way to receive it either. The result of every one of
// those was the same and left no trace anywhere: the recorder documented the
// SIGHUP in the provenance ledger, declined to re-raise it because the mode was
// in the table, and the process neither exited nor drained. A recorded signal
// that reaches no handler is a SILENT DROP — the exact failure Rule 15 exists to
// forbid, wearing the recorder's own evidence as cover.
//
// THE INVARIANT THIS FILE ENFORCES, in one sentence: every mode the recorder
// declares handler-owning must have a REAL registration whose set is a SUPERSET
// of signalrec.GracefulStopSignals(), or must be a face that exits before any
// handler could matter (the retired verbs) — stated as such, by name.
//
// HOW A MODE'S SET IS OBTAINED, and why each way is evidence rather than a
// second copy of the truth:
//
//   - signalSet: call the PRODUCTION value/function that feeds the mode's own
//     signal.Notify (workerStopSignals, chatGracefulSignals(), workerControlSignals).
//     Removing a signal there turns this guard red, as it should.
//   - notifySource: the mode's registration is a literal inside a large
//     function, so the guard reads the SOURCE at the anchor and requires the
//     operand to be signalrec.GracefulStopSignals() — i.e. the set is the
//     recorder's own definition, not a hand-typed list that can drift behind it.
//   - exitsBeforeHandlers: the face refuses and exits before installing
//     anything, so "owns handlers" costs a dead process nothing. The guard
//     requires the refusal call to still be there, so a revived daemon cannot
//     inherit the exemption silently.
//
// A mode in the table with NO row here fails: the guard's own list is checked
// against signalrec.OwningModeNames(), so adding a mode to the recorder's table
// without auditing its registration is a red build, not a later incident.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nick/gopi/internal/signalrec"
)

// The evidence kinds. They are named constants because they are compared in the
// switch below (Rule 1b: no bare string at a decision site).
const (
	simEvidenceSignalSet           = "signal-set"
	simEvidenceNotifySource        = "notify-source"
	simEvidenceExitsBeforeHandlers = "exits-before-handlers"
)

// simSourceFloor is the READ FLOOR for every file this guard scans. A scan that
// read an empty file (a wrong path, a rename, a truncated checkout) would find
// no anchor and could pass by finding nothing; each file named below is orders
// of magnitude larger than this.
const simSourceFloor = 4096

// simModeAudit is one row of the audit: which mode of the recorder's table this
// is, and what proves its registration.
type simModeAudit struct {
	// mode is the recorder's own spelling, matched against
	// signalrec.OwningModeNames() — several rows may share one mode when the
	// mode has more than one carrier (worker's drain loop and its --stop control
	// command are two different processes with two different sets).
	mode string
	// carrier names the concrete face this row audits, so a failure says WHICH
	// process is missing SIGHUP.
	carrier string
	// kind is one of the simEvidence* constants.
	kind string
	// signals is the production accessor for kind=signal-set.
	signals func() []os.Signal
	// file/anchor locate the registration for kind=notify-source.
	file   string
	anchor string
	// refusal is the call that must still precede any handler install for
	// kind=exits-before-handlers.
	refusal string
}

// simModeAudits is the audit table. Every entry is a CLAIM ABOUT PRODUCTION CODE
// and each one is checked against that code, never restated from it.
func simModeAudits() []simModeAudit {
	return []simModeAudit{
		// ---- the modes that DO own handlers, in production today -------------
		{
			mode:    "worker",
			carrier: "gopi worker (drain loop)",
			kind:    simEvidenceSignalSet,
			signals: func() []os.Signal { return workerStopSignals },
		},
		{
			// The `worker --stop` CONTROL command is a separate process that
			// inherits the same "worker" argv label, so it is covered by the same
			// table entry and owes the same set. It is its own row because its
			// set is a different value: it deliberately excludes SIGUSR1 (the
			// escalation spelling must never be read as "stop waiting").
			mode:    "worker",
			carrier: "gopi worker --stop (control command)",
			kind:    simEvidenceSignalSet,
			signals: func() []os.Signal { return workerControlSignals },
		},
		{
			mode:    "chat",
			carrier: "gopi chat",
			kind:    simEvidenceSignalSet,
			signals: chatGracefulSignals,
		},
		{
			mode:    "web",
			carrier: "gopi web (server)",
			kind:    simEvidenceNotifySource,
			file:    "cli_web.go",
			anchor:  "signal.Notify(sigCh, signalrec.GracefulStopSignals()...)",
		},
		{
			// "serve" has no dispatch verb of its own: roleFromArgv returns it
			// from a typed word, and every such process runs the bare-prompt arm
			// in main(). So the arm IS serve's registration.
			mode:    "serve",
			carrier: "gopi <bare prompt> (the arm every unclassified word reaches, `serve` included)",
			kind:    simEvidenceNotifySource,
			file:    "main.go",
			anchor:  "signal.Notify(sigCh, signalrec.GracefulStopSignals()...)",
		},
		{
			mode:    "resume",
			carrier: "gopi resume (top-level alias)",
			kind:    simEvidenceNotifySource,
			file:    "cli_resume.go",
			anchor:  "signal.Notify(resumeSigCh, signalrec.GracefulStopSignals()...)",
		},
		{
			// `gopi session resume` is the SAME runResume body but a DIFFERENT
			// table entry: the recorder's roleFromArgv reads args[1] only, so the
			// group word "session" wins over the "resume" leaf. It is listed as its
			// own carrier because the audit's job is to name every (argv → mode)
			// pair the recorder can classify, not every function.
			mode:    "session",
			carrier: "gopi session resume (same runResume body, classified as `session`)",
			kind:    simEvidenceNotifySource,
			file:    "cli_resume.go",
			anchor:  "signal.Notify(resumeSigCh, signalrec.GracefulStopSignals()...)",
		},
		{
			// The `session` GROUP is readers plus the playback; the readers are
			// one-shot and exit before a signal could matter, so the carrier this
			// mode's exemption rests on is the playback, whose registration lives
			// in internal/replayplay (the terminal it must restore).
			mode:    "session",
			carrier: "gopi session replay (internal/replayplay)",
			kind:    simEvidenceNotifySource,
			file:    filepath.Join("..", "..", "internal", "replayplay", "loop.go"),
			anchor:  "signal.Notify(s.sigCh, signalrec.GracefulStopSignals()...)",
		},

		// ---- the retired faces: no handler, because there is no run ----------
		{
			mode:    "merger",
			carrier: "gopi merger (retired daemon)",
			kind:    simEvidenceExitsBeforeHandlers,
			// RE-POINTED 2026-10-31: the file was renamed to daemon_retired.go
			// in the same change that deleted the one-shot `gopi merge`
			// implementation (epic merge-removal, item 3/6) — it now carries
			// MORE than the merger's retirement, so its name stopped being a
			// lie. The guard reads production sources BY NAME and fails loudly
			// when one is gone (Rule 15), which is how this re-point was found:
			// the suite said `open merger_retired.go: no such file or directory`
			// rather than reading green over nothing.
			file:    "daemon_retired.go",
			refusal: "retireMergerFace(",
		},
		{
			mode:    "task merger",
			carrier: "gopi task merger / task serve (retired aliases)",
			kind:    simEvidenceExitsBeforeHandlers,
			file:    "cli_tasks.go",
			refusal: "retireMergerFace(",
		},
		{
			mode:    "task run",
			carrier: "gopi task run (retired runner)",
			kind:    simEvidenceExitsBeforeHandlers,
			file:    "cli_tasks.go",
			refusal: "taskRemovedAdvice(taskverbs.Run)",
		},
	}
}

// TestEveryOwningModeCoversTheRecordersTerminatingSet is the invariant, one
// subtest per carrier. It is the guard the epic asked for: it walks the
// recorder's OWN table (signalrec.OwningModeNames) so a mode added there is
// audited by construction, and it holds each mode to the recorder's OWN set
// (signalrec.GracefulStopSignals) so there is one definition of "terminating".
func TestEveryOwningModeCoversTheRecordersTerminatingSet(t *testing.T) {
	audits := simModeAudits()
	required := signalrec.GracefulStopSignals()

	// EVERY declared mode must appear in the audit. This is the half that makes
	// the guard grow with the table instead of behind it.
	audited := map[string]int{}
	for _, a := range audits {
		audited[a.mode]++
	}
	declared := signalrec.OwningModeNames()
	if len(declared) == 0 {
		t.Fatal("signalrec.OwningModeNames() is empty — the recorder's table read as nothing, so this guard would pass by checking no mode at all")
	}
	for _, mode := range declared {
		if audited[mode] == 0 {
			t.Errorf("signalrec declares mode %q handler-owning but no audit row covers it — the recorder will record its signals and then leave them alone, so the mode MUST be shown to register %v (or to exit before a handler matters); add a row to simModeAudits",
				mode, signalNames(required))
		}
	}
	// The other direction: an audit row for a mode the recorder no longer
	// declares is a stale claim (it would keep passing after the table changed).
	declaredSet := map[string]bool{}
	for _, mode := range declared {
		declaredSet[mode] = true
	}
	for _, a := range audits {
		if !declaredSet[a.mode] {
			t.Errorf("audit row %q names carrier %q, but signalrec no longer declares that mode handler-owning — the row is stale and must be removed or re-derived", a.mode, a.carrier)
		}
	}

	for _, a := range audits {
		a := a
		t.Run(a.mode+" / "+a.carrier, func(t *testing.T) {
			switch a.kind {
			case simEvidenceSignalSet:
				if a.signals == nil {
					t.Fatalf("audit row %q has kind %s with no accessor — a row that cannot read the set would pass by measuring nothing", a.carrier, a.kind)
				}
				got := a.signals()
				if len(got) == 0 {
					t.Fatalf("%s registered NO signals — the recorder took %v over for this mode and declined to re-raise, so every one of them would be dropped in silence", a.carrier, signalNames(required))
				}
				for _, want := range required {
					if !containsSignal(got, want) {
						t.Errorf("%s does not register %s: the recorder registers it for every verb (replacing its default death) and then declines to re-raise for mode %q, so the signal would be recorded and DROPPED — the process neither exits nor drains (measured: task 01a15bb9)",
							a.carrier, signalrec.SignalName(want), a.mode)
					}
				}
			case simEvidenceNotifySource:
				src := simReadSource(t, a.file)
				if !strings.Contains(src, a.anchor) {
					t.Errorf("%s: %s carries no `%s` — this mode's registration must take its set from signalrec.GracefulStopSignals() (one definition, %v); a hand-typed list here is exactly how SIGHUP went missing",
						a.carrier, a.file, a.anchor, signalNames(required))
				}
			case simEvidenceExitsBeforeHandlers:
				src := simReadSource(t, a.file)
				if !strings.Contains(src, a.refusal) {
					t.Errorf("%s: %s no longer carries `%s` — this mode is exempt from the registration duty ONLY because it refuses and exits before installing a handler; with the refusal gone the exemption is a silent gap again",
						a.carrier, a.file, a.refusal)
				}
			default:
				t.Fatalf("audit row %q has unknown kind %q — this guard must not accept evidence it does not know how to check", a.carrier, a.kind)
			}
		})
	}
}

// TestRecorderTerminatingSetCarriesTheThreeGracefulSignals pins the SET the
// invariant is stated against, so the guard above cannot be satisfied by the
// definition shrinking underneath it (drop SIGHUP from gracefulStopSignals and
// every mode would comply with a set that no longer contains the signal this
// whole epic is about).
func TestRecorderTerminatingSetCarriesTheThreeGracefulSignals(t *testing.T) {
	got := signalrec.GracefulStopSignals()
	for _, want := range []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
		if !containsSignal(got, want) {
			t.Errorf("signalrec.GracefulStopSignals() lacks %s — it is the set the recorder takes over for every verb and the set every handler-owning mode is held to; shrinking it would make the coverage guard vacuous",
				signalrec.SignalName(want))
		}
	}
	if len(got) != 3 {
		t.Errorf("signalrec.GracefulStopSignals() has %d entries (%v), want exactly 3 — SIGUSR1/SIGUSR2 are gopi's control spellings, not termination, and must not be imposed on every mode",
			len(got), signalNames(got))
	}
	// The recorder's own registration must still be a SUPERSET of the set: it is
	// what took the defaults away in the first place.
	for _, want := range got {
		if !containsSignal(signalrec.WatchedSignals(), want) {
			t.Errorf("signalrec's own registration no longer watches %s, which it imposes on every mode", signalrec.SignalName(want))
		}
	}
}

// TestRecorderTerminatingSetIsACopyNotTheDefinition is the mutation guard on the
// accessor: a caller that sorted or truncated the returned slice in place would
// otherwise corrupt the recorder's own list for the life of the process.
func TestRecorderTerminatingSetIsACopyNotTheDefinition(t *testing.T) {
	first := signalrec.GracefulStopSignals()
	if len(first) == 0 {
		t.Fatal("GracefulStopSignals() is empty")
	}
	first[0] = syscall.SIGQUIT
	second := signalrec.GracefulStopSignals()
	if len(second) > 0 && second[0] == syscall.SIGQUIT {
		t.Fatalf("GracefulStopSignals() returned the DEFINITION, not a copy: writing through the returned slice changed the recorder's own list — every later caller would install a set nobody chose")
	}
}

// signalNames renders a set the way an operator reads it.
func signalNames(sigs []os.Signal) []string {
	out := make([]string, 0, len(sigs))
	for _, s := range sigs {
		out = append(out, signalrec.SignalName(s))
	}
	return out
}

// containsSignal reports membership by the signal's own value.
func containsSignal(hay []os.Signal, want os.Signal) bool {
	for _, got := range hay {
		if got == want {
			return true
		}
	}
	return false
}

// simReadSource reads one file this guard checks, with the read floor.
func simReadSource(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — the guard reads the production sources it audits and cannot run without them", path, err)
	}
	if len(b) < simSourceFloor {
		t.Fatalf("%s is %d bytes, below the %d-byte read floor — the scan would be vacuous (wrong path, truncated checkout)", path, len(b), simSourceFloor)
	}
	return string(b)
}
