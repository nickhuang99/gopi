package main

// status_action_conflict_e2e_test.go — the READ + ACT refusal (devdoc/worker-daemon.md
// F1, 2026-09-13): `gopi worker --status --stop` (and every other read+act
// combination on the worker face) must be REFUSED, never silently reduced to
// the read.
//
// THE MERGER FACE IS NO LONGER IN THIS SUITE (epic daemon-machinery-removal,
// 2026-09-17). The collision it modelled was "a read flag and an ACT that
// signals a RESIDENT service"; the merger has no resident service now — it is a
// cronjob, one tick and out (or that tick repeated in-process by `--every D`,
// merger-self-driving 2026-09-18) — so `--status` beside `--stop`/`--restart`/`--start`
// is no longer a read+act collision but an UNKNOWN FLAG, refused synchronously
// by the ordinary parser. That half is NOT dropped, it MOVED to where the new
// mechanism lives: `TestMergerRetiredFlagsAreRefusedNotIgnored`
// (merger_retired_face_test.go) pins every retired spelling, and
// `TestAliasDaemonStatusIsRefused` below pins the deprecated alias route — the
// one merger-shaped case that must keep being asserted here, because its
// property ("never a silent no-op with exit 0, decided before anything is
// written") is exactly this file's subject and it survives the machinery.
//
// THE DEFECT (measured on master 7c24ea06, bin/gopi):
//
//	$ gopi worker --status --stop    # prints the live-worker lines, exit 0, nothing signalled
//	$ gopi merger --status --stop    # exit 0, nothing signalled (the action is not
//	                                 #   even reached: --status is parsed as statusOnly
//	                                 #   and the control command runs first)
//
// Root cause: the worker face routed the READ-ONLY status action BEFORE the
// conflict check (`if workerStatusRequested(...) { os.Exit(workerStatusReport()) }`
// in isWorkerSubcommand), so `workerControlConflicts` — the function whose whole
// job is to refuse "pick ONE action" — was unreachable whenever `--status` was
// present. The merger face had the same shape with a different mechanism:
// `--status` is a PARSED option (`opts.statusOnly`) and the dispatch consults it
// only AFTER the control command has already signalled the resident service.
// Either way the operator's monitor sees exit 0 and concludes the service was
// stopped. It was not (Rule 15: a silent failure with a success code).
//
// WHAT THESE TESTS PIN — all against the REAL binary, because the property is
// "does the SHIPPED CLI refuse", and a unit assertion on the predicate would
// not prove the refusal happens before the action path:
//
//	① `--status` + an ACTING verb exits NON-ZERO (the worker's usage class 2,
//	   the merger's documented failure 1), and the message NAMES BOTH flags —
//	   so a caller can tell "you asked for two things" from any other refusal;
//	② the refusal is BEFORE the effect: no stop/force REQUEST file, no worker
//	   state file and no state directory was created in the private GOPI_HOME
//	   (the strongest side-effect assertion — the action path CREATES the
//	   directory via tools.ControlDir before it writes anything);
//	③ the read-only guarantee is intact: a bare `--status` still exits 0 and
//	   still leaves the profile empty;
//	④ the acting verbs are untouched: `--stop` alone still takes the action
//	   path (with no daemon running that is the honest "no daemon" line + exit
//	   0, which is a DIFFERENT line than the refusal — proving the refusal did
//	   not overshoot into a blanket ban on `--stop`).
//
// The three faces are covered separately because they reach the action
// DIFFERENTLY: `gopi worker` refuses in main.go before its status route;
// `gopi merger` is decided in main.go before any control parser is reached; and the
// deprecated `gopi task merger` alias refuses in cli_tasks.go's dispatch. A fix
// applied to one face and not the others is the exact drift this suite exists to
// catch.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tools"
)

// sscTaskVerb is the `gopi task` group verb, spelling the deprecated merger
// alias' dispatch token (cli_verbs.go owns TOP-LEVEL verbs only, so the group
// word is mirrored here exactly as merger_status_cli_test.go's mscTaskVerb is).
const sscTaskVerb = "task"

// sscRefusalTokens are the words the refusal MUST carry: both flags (so the
// operator sees which two actions collided) and the "pick ONE action" remedy.
// Named once so every face's assertion reads the same contract.
var sscRefusalTokens = []string{"cannot be combined", "pick ONE action"}

// sscConflictFaces describes the three dispatch faces and the acting verbs each
// must refuse when combined with `--status`, plus that face's expected exit
// code. The worker and the merger document DIFFERENT failure codes (2 = usage
// class, 1 = "the requested end state was not reached"), so the table carries
// the expectation rather than assuming one.
type sscConflictFace struct {
	name     string
	prefix   []string
	actVerbs []string
	exit     int
}

// sscFaceWorker names the ONE dispatch face this suite still models. The
// merger's two faces (the top-level verb and the `gopi task merger` alias) lost
// their control surface with the resident service; the alias's surviving
// property is pinned by TestAliasDaemonStatusIsRefused.
const sscFaceWorker = "worker"

// sscFaces is the surface under test: the worker face, whose exit code is
// pinned to workerUnexpectedArgExitCode — the constant, not a literal
// (Rule 1b/13).
//
// --daemon is deliberately NOT in the worker row: the worker's --daemon is
// refused in combination with --status by its own argv scanner (a separate,
// pre-existing guard this suite does not restate).
func sscFaces() []sscConflictFace {
	// THE MERGER ROWS ARE GONE with the merger's control face (epic
	// daemon-machinery-removal, 2026-09-17): there is no resident merger to stop
	// or restart, so there is no read+act collision to refuse on that face. The
	// worker face is untouched — it still has a resident service behind it — and
	// it is the only face this suite now models.
	//
	// THE MERGER'S RETIRED FLAGS ARE STILL REFUSED, and that half is NOT dropped:
	// `--daemon`/`--stop`/`--restart`/`--start` on the merger face are unknown
	// flags now, refused synchronously by both the top-level verb and the alias
	// (TestAliasDaemonStatusIsRefused below pins the alias route, and
	// merger_retired_flags_test.go pins the whole list on the top-level verb). A
	// silent no-op is the failure this suite exists to prevent, so the property
	// is kept and the mechanism it is asserted through changed.
	return []sscConflictFace{
		{name: sscFaceWorker, prefix: nil, actVerbs: []string{workerControlStop, workerControlRestart, workerControlStart, workerControlForce}, exit: workerUnexpectedArgExitCode},
	}
}

// sscFaceArgv renders one face's argv: the binary's VERB (sscFaceVerb —
// `worker` / `merger` / `task`), the face's remaining prefix words, then the
// control flags. The verb is rendered from one place so a face can never
// degenerate into the ambiguous `gopi --status --stop` (the agent face, which
// refuses `--status` through the unrelated unknown-flag path — a refusal
// assertion would then pass against the wrong code path).
func sscFaceArgv(f sscConflictFace, flags ...string) []string {
	argv := []string{sscFaceVerb(f)}
	if len(f.prefix) > 1 {
		argv = append(argv, f.prefix[1:]...)
	}
	return append(argv, flags...)
}

// sscFaceVerb is the argv[0] the face's binary dispatch reads (the worker verb).
func sscFaceVerb(f sscConflictFace) string {
	if len(f.prefix) > 0 {
		return f.prefix[0]
	}
	return CliVerbWorker
}

// sscPrivateHome mints the private profile and returns it plus the environment
// the child runs with: the operator-shell shape (the agent-spawn signature
// stripped, so a refusal can never be confused with the guard's own), pointed at
// the private GOPI_HOME, with no sync peers and no compute nodes.
func sscPrivateHome(t *testing.T) (string, []string) {
	t.Helper()
	home := t.TempDir()
	env := stripAgentSignature(guardTestEnv(t, home)(
		"GOPI_SETTINGS="+filepath.Join(home, "nonexistent.json"),
		"GOPI_COMPUTE_NODES=",
		"GOPI_SYNC_PEERS=",
	))
	return home, env
}

// sscProfileSnapshot lists every path under the private GOPI_HOME, sorted and
// relative — the side-effect fingerprint. A refusal must leave it EMPTY (the
// profile does not even exist yet), which is strictly stronger than "no stop
// request file": the action path creates <home>/.gopi/worker before it writes
// anything, so ANY trace means the action path was entered.
func sscProfileSnapshot(t *testing.T, home string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(home, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == home {
			return nil
		}
		rel, rerr := filepath.Rel(home, path)
		if rerr != nil {
			return rerr
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk the private profile %s: %v", home, err)
	}
	return out
}

// TestStatusWithActingActionIsRefused is ①②: every face refuses a read+act argv
// with a NON-ZERO code, names both flags, and leaves the profile untouched.
func TestStatusWithActingActionIsRefused(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, f := range sscFaces() {
		for _, act := range f.actVerbs {
			t.Run(f.name+"/"+act, func(t *testing.T) {
				home, env := sscPrivateHome(t)
				argv := sscFaceArgv(f, workerControlStatus, act)

				out, code := runGopiIn(t, bin, env, "", argv...)

				// ① the refusal is a FAILURE, never a success code.
				if code != f.exit {
					t.Fatalf("`gopi %s`: exit=%d want %d (refusal)\noutput:\n%s",
						strings.Join(argv, " "), code, f.exit, out)
				}
				// ① the message names BOTH flags — the operator must see which
				// two requests collided, not a generic parse complaint — and
				// carries the refusal wording that face uses.
				for _, want := range append([]string{workerControlStatus, act}, sscRefusalTokens...) {
					if !strings.Contains(out, want) {
						t.Errorf("refusal must mention %q:\n%s", want, out)
					}
				}
				// ② nothing acted: the profile is untouched to the last byte.
				if snap := sscProfileSnapshot(t, home); len(snap) != 0 {
					t.Errorf("the refusal WROTE to the private profile: %v\noutput:\n%s", snap, out)
				}
			})
		}
	}
}

// TestStatusAloneStaysReadOnly is ③: the refusal must not overshoot. A bare
// `--status` on every face still exits 0 and still creates nothing — this is
// the documented read-only contract (a status probe must never create the state
// directory or take a lock), and it is the half a careless "refuse when another
// flag is present" fix would break.
func TestStatusAloneStaysReadOnly(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, f := range sscFaces() {
		t.Run(f.name, func(t *testing.T) {
			home, env := sscPrivateHome(t)
			argv := sscFaceArgv(f, workerControlStatus)

			out, code := runGopiIn(t, bin, env, "", argv...)

			if code != 0 {
				t.Fatalf("`gopi %s` must stay exit 0\noutput:\n%s", strings.Join(argv, " "), out)
			}
			if strings.Contains(out, "cannot be combined") {
				t.Errorf("a bare --status must not be refused:\n%s", out)
			}
			if snap := sscProfileSnapshot(t, home); len(snap) != 0 {
				t.Errorf("`--status` created %v in the private profile — it must be side-effect free", snap)
			}
		})
	}
}

// TestActingActionAloneStillActs is ④: the other direction of the same
// non-overshoot boundary. With a private (empty) profile there is no daemon, so
// the documented answer is the honest "no daemon is running" line and exit 0 —
// a DIFFERENT output from the refusal. If the conflict predicate were written
// too broadly (e.g. "refuse any two flags"), this case would return the refusal
// text and the test fails.
func TestActingActionAloneStillActs(t *testing.T) {
	bin := gopiTestBinPath(t)
	for _, f := range sscFaces() {
		t.Run(f.name+"/stop", func(t *testing.T) {
			_, env := sscPrivateHome(t)
			argv := sscFaceArgv(f, workerControlStop)

			out, code := runGopiIn(t, bin, env, "", argv...)

			if code != 0 {
				t.Fatalf("`gopi %s` with no daemon running must exit 0\noutput:\n%s", strings.Join(argv, " "), out)
			}
			if strings.Contains(out, "cannot be combined") {
				t.Fatalf("`--stop` alone must not be refused as a conflict:\n%s", out)
			}
			// The action path DID run: it resolves its state directory (that is
			// the honest side effect of asking a service to stop), and reports
			// the empty state.
			if !strings.Contains(strings.ToLower(out), "no ") {
				t.Errorf("expected the empty-state note from the acting path, got:\n%s", out)
			}
		})
	}
}

// TestStatusWithActingActionTouchesNoRequestFile is the sharpest form of ②:
// even when a pid-stamped state file is SEEDED (so the stop path would have a
// target and would write a request file), the refusal must still write nothing.
// Without this, "the profile is empty" could pass simply because an empty
// profile gives the action nothing to do.
func TestStatusWithActingActionTouchesNoRequestFile(t *testing.T) {
	bin := gopiTestBinPath(t)
	home, env := sscPrivateHome(t)

	// HERMETIC FIRST: tools.ControlDir resolves config.GopiDir() from the
	// PROCESS environment, and a child's env (the `env` vector this test passes
	// to runGopiIn) cannot relocate the parent. Without this line the seed
	// landed in the OPERATOR's real <home>/.gopi/worker — measured 2026-09-14:
	// running this test wrote worker-state-3518718 into ~/.gopi/worker, where
	// `gopi worker --status` reported it as a phantom "worker pid N" (its pid
	// was THIS test process, whose comm is the test binary), and where EVERY
	// later `go test ./cmd/agent/` added one more file. The test passed anyway,
	// because it only ever asserted on the private profile it handed the child.
	t.Setenv(config.GopiDirEnv, home)

	// Seed a live-looking worker state for our OWN pid (a live pid, so
	// tools.LiveStateFiles would report it as a running worker) and read the
	// exact path the stop path would write a request to.
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Skipf("cannot resolve the worker control dir in this environment: %v", err)
	}
	statePath := tools.ControlStatePath(dir, workerStateFilePrefix, os.Getpid())
	requestPath := tools.ControlRequestPath(dir, workerStopRequestPrefix, os.Getpid())
	if err := tools.WriteControlText(statePath, []byte(
		`{"pid":`+strconv.Itoa(os.Getpid())+`,"started":"2026-09-13T00:00:00Z","state":"idle"}`)); err != nil {
		t.Fatalf("seed the worker state file: %v", err)
	}
	before := sscProfileSnapshot(t, home)

	out, code := runGopiIn(t, bin, env, "", CliVerbWorker, workerControlStatus, workerControlStop)
	if code != workerUnexpectedArgExitCode {
		t.Fatalf("`gopi worker --status --stop` with a live target: exit=%d want %d\noutput:\n%s",
			code, workerUnexpectedArgExitCode, out)
	}
	if _, err := os.Stat(requestPath); err == nil {
		t.Errorf("the refusal WROTE the stop request file %s — the stop path was entered", requestPath)
	}
	if after := sscProfileSnapshot(t, home); strings.Join(before, ",") != strings.Join(after, ",") {
		t.Errorf("the refusal changed the profile:\nbefore %v\nafter  %v", before, after)
	}
}

// TestAliasDaemonStatusIsRefused is the ALIAS route of the retired --daemon
// flag. The session-audit of the F1 fix found that the top-level `gopi merger
// --daemon --status` detached a daemon and exited 0 (a read+act, routed by its
// own predicate, not by the control parser). The flag is RETIRED now (epic
// daemon-machinery-removal, 2026-09-17) and the aliases reach it by a path that
// has no --daemon in its flag table, so the parser refuses it with the
// "retired" hint — and that refusal is what this test pins: whatever the alias
// answers, it must never be a silent no-op with exit 0, and it must be decided
// BEFORE anything is written. The property survived the machinery; only the
// reason the flag is refused changed.
func TestAliasDaemonStatusIsRefused(t *testing.T) {
	// REWRITTEN FOR THE RETIREMENT (2026-09-19, devdoc/merger-retired.md): the
	// alias family's argv is no longer PARSED at all — the retired face refuses
	// the whole verb before the parser runs — so a "refusal must name --daemon /
	// --status" assertion would be asserting about a code path the retirement
	// removed on purpose. The property the case exists for is unchanged and is
	// what is asserted below: a read beside an act is never a SILENT success,
	// and the refusal is decided BEFORE anything is written.
	// BINARY FIRST (the resolver builds with a relative `go build .`; the pin
	// below chdirs out of the package directory). Then the repo-context sandbox:
	// the child is the real binary and the argv carries a merger verb beside a
	// status modifier (the collision family), so it must not inherit this
	// process's cwd — the live repository. See repo_context_sandbox_test.go.
	bin := gopiTestBinPath(t)
	SandboxRepoContext(t)
	for _, argv := range [][]string{
		{sscTaskVerb, CliVerbMerger, daemonFlagLong, workerControlStatus},
		{sscTaskVerb, CliVerbMerger, workerControlStatus, daemonFlagLong},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			home, env := sscPrivateHome(t)

			out, code := runGopiIn(t, bin, env, "", argv...)

			if code == 0 {
				t.Fatalf("`gopi %s` must not exit 0 — a read beside an act is never a silent success\noutput:\n%s",
					strings.Join(argv, " "), out)
			}
			if snap := sscProfileSnapshot(t, home); len(snap) != 0 {
				t.Errorf("the refusal wrote %v into the private profile", snap)
			}
			// The refusal is now the retirement's own sentence: the verb is
			// RETIRED, so the collision this case was written about cannot arise
			// — and the sentence names the cause (Rule 15: a refusal that
			// swallows the flag OR the reason is the silent no-op this file is
			// about).
			if !strings.Contains(out, mergerRetiredCause) {
				t.Errorf("the alias's answer must carry the retirement's cause:\n%s", out)
			}
		})
	}
}
