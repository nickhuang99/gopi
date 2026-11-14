package main

// REGRESSION GATE for the settings.json `tmpdir` VALUE (tmpdir-settings-value
// epic, 2026-09-15).
//
// WHAT THIS PINS, and why the previously-merged tests did not pin it. The
// injection MECHANISM (cmd/agent/tmpdir_inject.go, commit ec4c860a) is fully
// covered by cli_tmpdir_inject_e2e_test.go — but every one of those cases
// supplies its own settings FILE and therefore proves only "the code works when
// configured". On the real host the key was ABSENT from
// ~/.gopi/settings.json, so the mechanism was inert and gopi + its whole child
// tree fell back to the implementation default (/tmp — a directory on the `/`
// partition here; measured: both device 2051). 4.0G of never-reaped residue is
// the result. The missing half of the contract is the OPERATOR's configuration,
// so this test reads the OPERATOR's settings file (never a fixture) and refuses
// to let the host drift back into the fallback:
//
//	gate 1  the settings key exists, and its target is off `/` BY MEASUREMENT
//	        (statfs device id), never by its spelling (Rule 22);
//	gate 2  a child gopi SPAWNS actually lands its temp files off `/` and was
//	        HANDED the configured value in its own /proc environ — the
//	        kernel's record, not the parent's intention.
//
// WHY THE CHILD IS DRIVEN THROUGH `gopi tool shot`'s BROWSER: it is a real spawn on a
// deterministic path (no model, no task ledger, no daemon) and the browser
// binary is injectable (the shot verb's binary override), so a stub can be the
// child whose temp workspace is observed from both sides — its own
// /proc/self/environ (inheritance) and where its `mktemp` actually landed (the
// substance, read back from the capture AND from the temp root's own listing).
//
// THE RED STATE IS ASSERTED TOO. TestTmpDirSettingsValueAmbientFallbackLeaks
// drives the same probe with TMPDIR pinned at the ambient temp root (the
// injection's caller-wins latch then leaves the fallback in place — the host's
// actual state before the key was configured) and requires the leak to be
// OBSERVED: a new temp dir under the ambient root, measuring as `/`'s device.
// Without that case this file could be a function that always passes (Rule 13:
// an assertion never seen failing is not an assertion); it also reclaims
// exactly the one directory it caused, by path, with its size printed
// (Rule 22 bans `rm -rf <temp root>` and bans unnamed reclaim).
//
// NOTHING HERE TOUCHES HOST CONFIG (Rule 22): no ~/.bashrc, no /etc/*. The only
// files written are this probe's own artifacts, all of them under temp dirs.

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/shot"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// tmpDirValueStubName is the fake browser gopi is told to launch (through
	// the shot verb's binary override). Named, never inlined (Rule 1b).
	tmpDirValueStubName = "tmpdir-value-chrome-stub"

	// tmpDirValueStubUUID is the browser-id segment of the DevTools line the
	// stub prints. It MUST match wsURLPattern's `[0-9a-f-]+` class — MEASURED:
	// the previous spelling was the word "stub", which does not match, so the
	// launcher never saw the line, waited out chromeStartTimeout (15s) and only
	// then fell back to an attached launch. That fallback was the 17s.
	tmpDirValueStubUUID = "00000000-0000-0000-0000-000000000000"

	// tmpDirValueClosedPort is a loopback port nothing listens on. The
	// discovery fixture points its page target's WebSocket URL here, so the CDP
	// dial fails IMMEDIATELY (ECONNREFUSED) instead of polling out
	// discoveryPollTimeout — the run's failure is the point; its speed is what
	// keeps the case honest about which path it exercised.
	tmpDirValueClosedPort = "1"

	// tmpDirValueProbePrefix is the mktemp template prefix the stub uses, so a
	// probe-named entry under a shared temp root is unambiguously this probe's.
	// It no longer decides the residue verdict (that is scoped to the child's own
	// reported directory); it is what keeps the ambient-root listings — the leak
	// evidence in the GREEN and fallback cases — free of concurrent writers.
	tmpDirValueProbePrefix = "gopi-tmpdir-value-probe-"

	// tmpDirValueMktempField is the capture field naming where the child's real
	// `mktemp -d -t` landed: the probe's OWN directory, the one path this file's
	// residue guards audit (Rule 22 — the child's own report, never a re-derived
	// root). Never inlined at a lookup site (Rule 13).
	tmpDirValueMktempField = "mktemp"

	// tmpDirValueNoCallerTmpDir is the caller TMPDIR that means "absent": the
	// child is handed none, so the settings value alone decides where its temp
	// workspace lands — the shape the GREEN case drives.
	tmpDirValueNoCallerTmpDir = ""

	// tmpDirValueFixtureHeaderTimeout bounds the discovery fixture's request
	// header read. It is a fixture, not a server under test, so the bound only
	// keeps a half-open request from parking a goroutine past the case.
	tmpDirValueFixtureHeaderTimeout = 5 * time.Second

	// tmpDirValueUrl is the URL the shot verb is asked for: a closed port on
	// loopback, so the run fails fast and deterministically AFTER the stub has
	// already reported (the test does not depend on the run's exit status). The
	// URL embeds no configurable value, so it is inlined nowhere else.
	tmpDirValueUrl = "http://127.0.0.1:1/"
)

// tmpDirValueSettingsName is the fixture settings file NAME the red case writes.
const tmpDirValueSettingsName = "settings.json"

// tmpDirValueStubBody builds the fake browser. It reports, from inside a
// process GOPI SPAWNED:
//
//	ckid=           its own pid (the kernel's record of the spawn);
//	environ lines   its own /proc/self/environ entries — what execve HANDED it.
//	                A shell `echo $TMPDIR` would be a claim about itself; the
//	                environ block cannot be true unless inheritance happened.
//	                MEASURED: the block is rewritten with sed's `\x0` escape
//	                because dash's `tr '\0' '\n'` spelling silently produces an
//	                EMPTY capture on this host (the same trap the injection's
//	                own e2e test records);
//	mktemp=         where a REAL `mktemp -d -t` landed — GNU mktemp honours
//	                TMPDIR when set and falls back to the temp root when it is
//	                not, which is exactly the fallback this epic is about
//	                (measured on this host);
//
// THE REPORT PATH IS BAKED IN, not read from the environment. MEASURED: the
// launcher starts the browser as a transient systemd user unit, and such a unit
// gets a CLEAN environment — it does not inherit the caller's. A stub that
// resolved its report path from an env var therefore wrote nothing (`> ""`) the
// moment the launch stopped falling back to the attached path, which is the
// path that DOES inherit. Interpolating the absolute path into the script keeps
// the report exactly what it was — the child's OWN written observation, not a
// path this test guessed — and makes it independent of which launch path the
// engine chose.
//
// The DevTools line goes to STDERR, the only channel the launcher reads (the
// unit path greps its stderr redirect file, the attached path scans the child's
// stderr pipe) — MEASURED: writing it to stdout meant no launcher ever saw it,
// the wait ran out chromeStartTimeout (15s) and the run reached the stub only
// through the fallback. Port and browser id must MATCH wsURLPattern
// (`ws://127\.0\.0\.1:(\d+)/devtools/browser/[0-9a-f-]+`); devtoolsPort is the
// test's own discovery fixture, so the run walks the handshake a healthy chrome
// would. The stub then exits: the capture is expected to fail at the CDP dial
// (the fixture points at a closed port), and this file asserts on the REPORT,
// never on the run's exit status.
func tmpDirValueStubBody(devtoolsPort, outPath string) string {
	return "#!/bin/sh\n" +
		"{\n" +
		"  echo \"ckid=$$\"\n" +
		"  sed -e 's/\\x0/\\n/g' /proc/self/environ | grep -E '^(TMPDIR|GOPI_HOME)=' || true\n" +
		"  d=$(mktemp -d -t " + tmpDirValueProbePrefix + "XXXXXX)\n" +
		"  echo \"mktemp=$d\"\n" +
		"} > " + shellSingleQuote(outPath) + "\n" +
		"echo 'DevTools listening on ws://127.0.0.1:" + devtoolsPort + "/devtools/browser/" + tmpDirValueStubUUID + "' >&2\n" +
		"exit 0\n"
}

// shellSingleQuote renders s as one POSIX-sh single-quoted word, so an
// interpolated path can never be re-read as shell syntax (a quote or `$` in a
// temp directory name would otherwise end the literal).
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// tmpDirValueCDPDiscovery starts a loopback server answering the ONE endpoint
// the capture engine asks for before it dials a page (/json/list), and returns
// its port for the stub's DevTools line.
//
// WHY IT EXISTS (measured): with a DevTools line the launcher accepts, the very
// next step is CDP discovery, and against a port nothing serves it polls for
// discoveryPollTimeout (2s) before reporting. That 2s is unrelated to what this
// file asserts — the report is written by the stub before any of it — so the
// fixture answers discovery at once and points the page target at a CLOSED port
// (tmpDirValueClosedPort), where the WebSocket dial fails immediately. The run
// still fails, still fails for a real reason, and does so in the time the case
// is about.
func tmpDirValueCDPDiscovery(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback listener for the CDP discovery fixture: %v", err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		ln.Close() //stylegate:ignore the fixture's own listener, closed as the case aborts before it is served
		t.Fatalf("split the fixture's listener address %q: %v", ln.Addr(), err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		// One page target, as a real chrome serves it once the browser is up.
		// Its debugger URL names a CLOSED port on purpose: the dial must fail
		// fast rather than the case reporting a hang. The field names are
		// Chrome's wire vocabulary, spelled here because internal/shot keeps its
		// target type unexported — a rename there would make this fixture
		// answer a shape the engine no longer reads, and the case would go red
		// on the missing report rather than silently passing.
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprintf(w,
			`[{"id":%q,"type":"page","webSocketDebuggerUrl":%q}]`,
			tmpDirValueStubUUID,
			"ws://127.0.0.1:"+tmpDirValueClosedPort+"/devtools/page/"+tmpDirValueStubUUID,
		); err != nil {
			t.Logf("CDP discovery fixture: writing the target list: %v", err)
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: tmpDirValueFixtureHeaderTimeout}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Logf("CDP discovery fixture stopped serving: %v", err)
		}
	}()
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Logf("CDP discovery fixture close: %v", err)
		}
	})
	return port
}

// tmpDirValueOperatorSettings resolves the OPERATOR's settings file — the one
// gopi reads when nothing overrides it, and the artifact under test.
//
// The suite entry point deliberately pins BOTH knobs at sandboxes
// (GOPI_SETTINGS at a non-existent sandbox path for notify hermeticity,
// GOPI_HOME at the per-run profile) because every other test here must not
// touch the operator's real state. This test is the exception, and it says so
// explicitly: the pin is REVERSED for its own duration — restored on every exit
// path by SnapshotEnv, absence included (Rule 17) — because "the operator's
// host falls back to the temp root on /" is only observable in the operator's
// own configuration.
func tmpDirValueOperatorSettings(t *testing.T) string {
	t.Helper()
	for _, name := range []string{config.SettingsEnv, config.GopiDirEnv} {
		t.Cleanup(testutil.SnapshotEnv(t, name))
		if err := os.Unsetenv(name); err != nil { // env-leak-ok: the SnapshotEnv above restores the pinned value (absence included) at test end //stylegate:ignore SnapshotEnv registered via t.Cleanup IS the restore; the CALLER's own file must be resolved by the production reader, which is why this is an explicit unset rather than a t.Setenv
			t.Fatalf("unset %s: %v", name, err)
		}
	}
	return config.ResolveSettingsPath()
}

// tmpDirValueEnv builds the child gopi's environment: the suite's standard
// sandbox with TMPDIR REMOVED (the ambient host shape this epic fixes — a
// leftover suite pin would silently turn every case into "the caller wins"),
// plus the settings path, the browser stub and, when
// callerTmpDir is non-empty, an explicit TMPDIR for the red case. Each key
// appears at most ONCE: a child environment is a slice and os.Getenv returns the
// FIRST match, so a duplicate would silently win (the pitfall the injection's
// own comments record).
func tmpDirValueEnv(t *testing.T, gopiHome, settingsPath, stubPath, callerTmpDir string) []string {
	t.Helper()
	base := testutil.MockEnv(t, gopiHome, tmpDirValueUrl)
	env := make([]string, 0, len(base)+4)
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		if key == scratch.TmpDirEnvName {
			continue // dropped: the child must resolve the ambient shape
		}
		env = append(env, kv)
	}
	env = append(env,
		config.SettingsEnv+"="+settingsPath,
		shot.ChromeEnvName+"="+stubPath,
	)
	if callerTmpDir != "" {
		env = append(env, scratch.TmpDirEnvName+"="+callerTmpDir)
	}
	return env
}

// tmpDirValueRun spawns one child gopi on the shot verb and returns the stub's
// capture, verbatim. A missing capture is a failed observation, never an assumed
// pass (Rule 15): the stub is the only thing that can produce the evidence, so
// its absence means the spawn under test did not happen.
func tmpDirValueRun(t *testing.T, gopiHome, settingsPath, callerTmpDir string) string {
	t.Helper()
	probeDir := t.TempDir()
	// The stub reports the port of the test's OWN discovery fixture, so the run
	// walks the health-path handshake (DevTools line → discovery → page target)
	// and fails at the closed-port dial instead of waiting the start timeout
	// out. Without this the case would still pass — through the 15s fallback,
	// which is exactly the waste this fixture removes.
	devtoolsPort := tmpDirValueCDPDiscovery(t)
	outPath := filepath.Join(probeDir, "stub-report")
	stubPath := filepath.Join(probeDir, tmpDirValueStubName)
	if err := os.WriteFile(stubPath, []byte(tmpDirValueStubBody(devtoolsPort, outPath)), 0o700); err != nil {
		t.Fatalf("write the browser stub: %v", err)
	}

	env := tmpDirValueEnv(t, gopiHome, settingsPath, stubPath, callerTmpDir)
	stdout, stderr, code := runGopiMapped(t,
		[]string{"shot", "--url", tmpDirValueUrl}, "", env, probeDir)
	if code == 0 {
		t.Logf("NOTE: the shot run exited 0 (unexpected for a stub browser)\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("the browser stub gopi was told to launch produced no capture at %s — the spawn under test did not happen\nstdout:\n%s\nstderr:\n%s",
			outPath, stdout, stderr)
	}
	return string(raw)
}

// tmpDirValueField returns the value of one "name=value" line of a capture.
func tmpDirValueField(t *testing.T, capture, name string) string {
	t.Helper()
	for _, line := range strings.Split(capture, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), name+"="); ok {
			return v
		}
	}
	t.Fatalf("the stub's capture carries no %q line:\n%s", name, capture)
	return ""
}

// tmpDirValueAmbientEntries lists the ambient temp root's entries whose name
// carries this probe's prefix. Reading the DIRECTORY is what makes the leak
// evidence independent of the stub's self-report, while the prefix filter keeps
// a concurrent writer's files out of the verdict.
func tmpDirValueAmbientEntries(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(worktreeroots.AmbientTempRoot)
	if err != nil {
		t.Skipf("cannot list the ambient temp root %s: %v", worktreeroots.AmbientTempRoot, err)
	}
	var mine []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpDirValueProbePrefix) {
			mine = append(mine, e.Name())
		}
	}
	return mine
}

// TestTmpDirSettingsValueReachesTheSpawnedChild is the epic's acceptance in one
// case: the OPERATOR's settings.json configures `tmpdir`; a child gopi spawns
// resolves its temp workspace off `/` BY MEASUREMENT and was handed the
// configured value in its own kernel-recorded environ.
func TestTmpDirSettingsValueReachesTheSpawnedChild(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	settingsPath := tmpDirValueOperatorSettings(t)
	cfg, err := config.FromSettingsFile(settingsPath)
	if err != nil {
		t.Fatalf("read the operator's settings %s: %v", settingsPath, err)
	}
	if cfg.TmpDir == "" {
		t.Fatalf("the operator's settings %s carries no %q key — gopi and every child it spawns fall back to %s, "+
			"which is on the `/` partition on this host. Set the key to a directory whose device id differs from `stat -c %%d /` "+
			"(Rule 22: the configured value is the fix; this test is the gate that keeps the host from losing it)",
			settingsPath, config.TmpDirSettingsKey, worktreeroots.AmbientTempRoot)
	}

	// Gate 1: the configured target is off `/` by MEASUREMENT (never spelling).
	targetDev, err := scratch.DeviceID(cfg.TmpDir)
	if err != nil {
		t.Fatalf("cannot measure the configured temp root %q: %v — an unmeasurable value is never 'safe'", cfg.TmpDir, err)
	}
	rootDev, err := scratch.RootDeviceID()
	if err != nil {
		t.Fatalf("cannot measure the filesystem of /: %v", err)
	}
	if targetDev == rootDev {
		t.Fatalf("%s %q is on the SAME filesystem as / (device %d) — the exact configuration that filled the boot partition with residue",
			config.TmpDirSettingsKey, cfg.TmpDir, targetDev)
	}
	t.Logf("settings %s: %s=%q device=%d vs / device=%d (MEASURED, not spelled)",
		settingsPath, config.TmpDirSettingsKey, cfg.TmpDir, targetDev, rootDev)

	// Gate 2: a real spawned child.
	before := tmpDirValueAmbientEntries(t)
	capture := tmpDirValueRun(t, t.TempDir(), settingsPath, tmpDirValueNoCallerTmpDir)

	// (a) inheritance: the kernel's record of what execve handed the child.
	handed := tmpDirValueField(t, capture, "TMPDIR")
	if handed != cfg.TmpDir {
		t.Fatalf("the child gopi spawned was handed TMPDIR=%q (its own /proc/self/environ), want the configured %q — "+
			"the settings value did not reach the child tree\ncapture:\n%s", handed, cfg.TmpDir, capture)
	}
	childPid := tmpDirValueField(t, capture, "ckid")
	if childPid == "" {
		t.Fatalf("the stub reported no pid — the capture is not a real child's report:\n%s", capture)
	}

	// (b) substance: the child's temp files really landed off `/`, measured.
	landed := tmpDirValueField(t, capture, tmpDirValueMktempField)
	landedDev, err := scratch.DeviceID(landed)
	if err != nil {
		t.Fatalf("cannot measure where the child's temp dir landed (%q): %v", landed, err)
	}
	if landedDev == rootDev {
		t.Fatalf("the child's mktemp dir %q is on the `/` filesystem (device %d) — the child's temp workspace still lands on the boot partition",
			landed, landedDev)
	}
	t.Logf("child (pid %s) /proc/self/environ TMPDIR=%q; its mktemp landed at %q device=%d (off /, device %d)",
		childPid, handed, landed, landedDev, rootDev)

	// (b') The reclaim is registered NOW, before anything below can fail: this case
	// used to reclaim NOTHING, so every run left one `gopi-tmpdir-value-probe-*`
	// directory inside the very temp root this epic publishes (measured 2026-09-16:
	// 22 such entries, one per past run — the exact residue this epic exists to
	// prevent, produced by its own test). Registering it as cleanup rather than an
	// inline call at the end means a failure anywhere below still reclaims the
	// directory, and it goes through the SHARED helper the RED case uses so the two
	// cannot drift apart (Rule 22: the child's own reported path, never a recursive
	// wipe of a root).
	t.Cleanup(func() { tmpDirValueReclaim(t, landed) })

	// (c) no NEW entry under the fallback temp root. The listing was taken
	// BEFORE the spawn, so an entry that appeared since is this run's doing — a
	// listing ALONE can only ever say that: the SHARED root carries other runs'
	// entries (measured 2026-09-28: a probe directory 58m old, from an earlier
	// unrelated run), which is exactly why the residue claim is made about this
	// probe's own reported directory instead.
	after := tmpDirValueAmbientEntries(t)
	added := tmpDirValueNewEntries(before, after)
	if len(added) > 0 {
		t.Fatalf("the spawn added %d entr%s under %s (the fallback temp root on `/`): %v — the child did not use the configured temp root",
			len(added), plural(len(added)), worktreeroots.AmbientTempRoot, added)
	}
}

// TestTmpDirValueProbeLeavesNothingBehind is the residue guard for this probe's
// own landing directory.
//
// WHY IT IS SCOPED TO THIS RUN'S DIRECTORY instead of auditing the shared temp
// root. The earlier shape listed the temp root and flagged every probe-named
// entry older than tmpDirValueProbeStaleAfter. That reads foreign history as
// this run's failure: the root is SHARED, an abort (Ctrl-C, OOM, SIGKILL) leaves
// another run's directory behind, and the age bound only delays the verdict —
// measured 2026-09-28, the guard was red on a PRISTINE master checkout while this
// run created nothing and reclaimed nothing (before=1 after=1; the flagged entry
// was an earlier unrelated run's, 58m55s old). A guard that fails on a
// stranger's litter and passes for a run that leaks cannot tell the two apart, so
// it is a liar either way.
//
// The self-caused residue has EXACTLY ONE measurable location: the directory the
// CHILD reported as its landing path — the path tmpDirValueReclaim is registered
// on, never a re-derived guess (Rule 22: measured, by the child's own report).
//
// The leak it guards is the one the epic exists for: with the reclaim disabled
// the GREEN case ends with that directory still on disk, and this assertion sees
// it inside the same run — no sleeping, no age bound. The mutation proof (the
// reclaim withheld) is TestTmpDirValueProbeResidueGuardRedProof below, where the
// ONE predicate this guard uses is observed red on the probe's real directory.
func TestTmpDirValueProbeLeavesNothingBehind(t *testing.T) {
	if _, err := scratch.DefaultTmpDir(); err != nil {
		t.Skipf("no default temp root on this host (%v) — the residue this guard audits cannot accumulate there", err)
	}
	landed := tmpDirValueField(t, tmpDirValueRun(t, t.TempDir(), tmpDirValueOperatorSettings(t), ""), tmpDirValueMktempField)
	if landed == "" {
		t.Fatalf("the child's capture names no landing directory — without it the residue this guard audits has no address")
	}
	t.Cleanup(func() { tmpDirValueReclaim(t, landed) })
	if _, err := os.Stat(landed); err != nil {
		t.Fatalf("the child reported its temp workspace at %q, yet it is not there: %v — the landing report and the filesystem disagree", landed, err)
	}
	// One reclaim, then ONE audit of it, both naming the same path. The audit is a
	// positive claim about a directory that already exists (asserting on a path
	// that never existed would prove nothing), and it is taken before the cleanup
	// above can start passing the assertion for it.
	if err := os.RemoveAll(landed); err != nil {
		t.Fatalf("reclaiming the probe's own temp dir %s: %v", landed, err)
	}
	if err := tmpDirValueAssertReclaimed(landed); err != nil {
		t.Fatalf("%v", err)
	}
	t.Logf("the probe's own landing directory was reclaimed: %s no longer exists", landed)
}

// tmpDirValueAssertReclaimed is the ONE residue predicate: it returns nil only
// while the directory the CHILD ITSELF reported is gone. Every caller inspects
// the same path with the same call — the guard in the green case and the red
// proof's control — so a leak cannot pass one and fail the other.
func tmpDirValueAssertReclaimed(dir string) error {
	_, err := os.Stat(dir)
	if err == nil {
		return fmt.Errorf("the probe's own landing directory %s still exists after the case — its reclaim did not happen, "+
			"so every run grows the temp root this epic publishes by one entry (Rule 22)", dir)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot audit the probe's own landing directory %s: %w", dir, err)
	}
	return nil
}

// TestTmpDirValueProbeResidueGuardRedProof is the residue guard's own red proof,
// and it is what keeps the guard from going vacuous: asserting that a directory a
// test created for itself is gone would prove nothing about the probe's reclaim.
// The control here is the PROBE'S OWN directory — the same spawn, the same
// child-reported path, the same predicate the green case uses — with ONLY the
// reclaim withheld, which is precisely the regression the guard exists for. The
// predicate must be RED before the reclaim and GREEN after it, so a leaked
// landing directory cannot pass unnoticed.
func TestTmpDirValueProbeResidueGuardRedProof(t *testing.T) {
	if _, err := scratch.DefaultTmpDir(); err != nil {
		t.Skipf("no default temp root on this host (%v) — the leak this proof exhibits cannot happen there", err)
	}
	landed := tmpDirValueField(t, tmpDirValueRun(t, t.TempDir(), tmpDirValueOperatorSettings(t), ""), tmpDirValueMktempField)
	if landed == "" {
		t.Fatalf("the child's capture names no landing directory — this proof has nothing to withhold the reclaim from")
	}
	reclaimed := false
	t.Cleanup(func() {
		if !reclaimed {
			if err := os.RemoveAll(landed); err != nil {
				t.Logf("reclaiming %s at cleanup: %v", landed, err)
			}
		}
	}) //stylegate:ignore rule22-reclaim: the child's OWN reported path (never a re-derived root), removed by name so the withheld reclaim is cleaned up at the end

	if err := tmpDirValueAssertReclaimed(landed); err == nil {
		t.Fatalf("the residue predicate passed BEFORE any reclaim — %s is already gone, so it is blind to the leak it exists to catch "+
			"(and this proof would be vacuous)", landed)
	}
	t.Logf("RED: %s is present before the reclaim, exactly as a leaked landing directory would be left", landed)

	if err := os.RemoveAll(landed); err != nil {
		t.Fatalf("reclaiming the probe's own temp dir %s: %v", landed, err)
	}
	reclaimed = true
	if err := tmpDirValueAssertReclaimed(landed); err != nil {
		t.Fatalf("the residue predicate is still red after the ONE reclaim it recognises: %v", err)
	}
	t.Logf("GREEN: the same predicate is satisfied once the probe's own directory is reclaimed")
}

// TestTmpDirSettingsValueAmbientFallbackLeaks is the in-tree RED PROOF: with the
// SAME probe but TMPDIR pinned at the ambient temp root (so the injection's
// caller-wins latch leaves the fallback in force — the host's actual state
// before the key was configured), the leak must be OBSERVED: a new temp
// directory appears under the ambient root and measures as `/`'s device. This is
// what makes the GREEN case meaningful — the observation is capable of failing.
//
// It skips on a host where the ambient root is off `/` (no leak to demonstrate)
// and reclaims exactly the one directory it caused, by reported path, with the
// size printed (Rule 22: an explicit path list, never a recursive wipe).
func TestTmpDirSettingsValueAmbientFallbackLeaks(t *testing.T) {
	ambientDev, err := scratch.DeviceID(worktreeroots.AmbientTempRoot)
	if err != nil {
		t.Skipf("cannot measure %s: %v", worktreeroots.AmbientTempRoot, err)
	}
	rootDev, err := scratch.RootDeviceID()
	if err != nil {
		t.Fatalf("cannot measure /: %v", err)
	}
	if ambientDev != rootDev {
		t.Skipf("%s is NOT on the / filesystem here (device %d vs %d) — the ambient fallback cannot leak onto the boot partition",
			worktreeroots.AmbientTempRoot, ambientDev, rootDev)
	}

	// The settings file for this case: exists, but carries no `tmpdir` key —
	// the operator's pre-fix shape.
	gopiHome := t.TempDir()
	settingsPath := filepath.Join(gopiHome, tmpDirValueSettingsName)
	if err := os.WriteFile(settingsPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write the keyless settings fixture: %v", err)
	}

	before := tmpDirValueAmbientEntries(t)
	capture := tmpDirValueRun(t, gopiHome, settingsPath, worktreeroots.AmbientTempRoot)
	landed := tmpDirValueField(t, capture, tmpDirValueMktempField)
	landedDev, err := scratch.DeviceID(landed)
	if err != nil {
		t.Fatalf("cannot measure the child's fallback temp dir %q: %v", landed, err)
	}
	if landedDev != rootDev {
		t.Fatalf("the fallback case did NOT land on `/`: %q is device %d — this negative case is not exercising the leak", landed, landedDev)
	}
	added := tmpDirValueNewEntries(before, tmpDirValueAmbientEntries(t))
	if len(added) == 0 {
		t.Fatalf("with %s at %s the child's temp workspace landed at %q yet no new entry appeared under that root — "+
			"the leak detection is blind, so the GREEN case proves nothing",
			scratch.TmpDirEnvName, worktreeroots.AmbientTempRoot, landed)
	}
	t.Logf("RED PROOF: with the ambient fallback in force the child landed at %q (device %d = /) and added %v under %s",
		landed, landedDev, added, worktreeroots.AmbientTempRoot)

	// Reclaim exactly what this case caused (the same helper the GREEN case
	// uses — the two must not drift).
	tmpDirValueReclaim(t, landed)
}

// tmpDirValueReclaim removes exactly the ONE directory a probe case caused, by
// the path the child itself reported — never a recursive wipe of a root (Rule
// 22). It is shared by the GREEN case and the ambient-fallback RED case so a
// reclaim regression cannot hide in one of them, and it reports the size it
// freed (the number the operator wants when auditing the temp root).
func tmpDirValueReclaim(t *testing.T, dir string) {
	t.Helper()
	if dir == "" {
		t.Fatalf("refusing to reclaim an empty path — the child reported no landing directory")
	}
	size := tmpDirValueDirSize(dir)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("reclaiming the probe's own temp dir %s: %v", dir, err)
	}
	t.Logf("reclaimed %s (%d bytes) — the one path this case created", dir, size)
}

// tmpDirValueNewEntries returns the entries present in after but not in before.
func tmpDirValueNewEntries(before, after []string) []string {
	seen := make(map[string]bool, len(before))
	for _, name := range before {
		seen[name] = true
	}
	var added []string
	for _, name := range after {
		if !seen[name] {
			added = append(added, name)
		}
	}
	return added
}

// tmpDirValueDirSize sums the file sizes under dir (the reclaim report's
// number). A partial walk still reports what it measured rather than failing —
// the number is informational.
func tmpDirValueDirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	}) //stylegate:ignore size census over a fixture dir; a walk error only leaves the total short and no assertion depends on it
	return total
}

// plural renders the count's suffix in a failure message (one spelling).
func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
