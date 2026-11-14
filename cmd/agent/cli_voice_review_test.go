package main

// cli_voice_review_test.go — independent review tests for the unreviewed
// `gopi tool voice_input` implementation (bd3d34a5 / 79d6fe52), plus the contract
// registrations whose absence left master RED.
//
// WHY THIS FILE EXISTS: cmd/agent/cli_voice.go + cli_voice_server.go shipped
// as one commit with a 285-line happy-path test file. The measured gaps at the
// review's start (scripts/func_coverage.py --pkg cmd/agent cannot see package
// main's unexported funcs — it filters on an EXPORTED-name regex — so the
// baseline is `go test -cover`: 20.2% of statements in the two files, and SIX
// of the sixteen functions at 0.0%):
//
//	 0.0%  isVoiceSubcommand        (cli_voice.go:19)
//	 0.0%  transcribeFile           (cli_voice.go:195)
//	 0.0%  runVoiceSubcommand       (cli_voice.go:229)
//	 0.0%  voiceServeHTTP           (cli_voice_server.go:23)
//	 0.0%  voiceServeStdio          (cli_voice_server.go:103)
//	 0.0%  writeVoiceJSON           (cli_voice_server.go:204)
//
// The main-process funcs exit the process, so they are covered the only way
// that does not change behaviour: driving a SEPARATE process. Two shapes are
// used below, because they are good at different things:
//
//   - the REAL built gopi binary (testutil.GopiBin, the same binary every
//     other CLI test in this suite drives) for the argv-level smoke: -h, a
//     parse error, a missing engine, stdin/stdio/HTTP server runs; and the
//     SAME binary built with `go build -cover` (testutil.CoverBin) when the
//     test is a coverage vehicle for cli_voice.go / cli_voice_server.go.
//   - this test binary re-executed, for the two things a real gopi cannot do:
//     (a) a STUB ENGINE — argv[0] is the engine path, so `--runtime <this
//     test binary>` with GOPI_VOICE_STUB_* set turns every engine-dependent
//     path (transcribeFile success, /transcribe 200, stdio transcript) into a
//     hermetic, fast, assertion-bearing unit; (b) deterministic COVERAGE of
//     the exit paths, since the child's counters land in the parent's profile
//     (go1.24 -test.gocoverdir).
//
// MEASURED at review time (go test -cover of this package, voice funcs only):
// before this file 83/198 statements = 41.9% (cli_voice.go 45.3%,
// cli_voice_server.go 38.8%), with isVoiceSubcommand / transcribeFile /
// runVoiceSubcommand / voiceServeHTTP / voiceServeStdio / writeVoiceJSON all
// at 0.0%. After: 196/199 = 98.5% (cli_voice.go 100.0%,
// cli_voice_server.go 97.1%).
//
// Hermeticity: no GOPI_HOME is required by any voice_input path, but every
// live probe still pins one (t.TempDir → the suite-pinned TMPDIR, never /tmp)
// because a routing slip into the agent loop would otherwise write into the
// operator's real profile — the M4.13 lesson cli_contracts_test.go encodes.

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/coverage"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// ---------------------------------------------------------------------------
// Contract registration (the master-RED finding)
//
// bd3d34a5 wired isVoiceSubcommand into main.go's dispatch chain but never
// registered the predicate in cliContractDispatchPredicates — so
// TestCLIContractInventoryMatchesDispatch fails on master (26 predicates vs
// 25 tracked). Editing cli_contracts_test.go is the only place that state is
// kept; these two tests pin the voice_input half of it so the fix cannot be
// half-applied again.
// ---------------------------------------------------------------------------

// TestVoiceInputIsRegisteredInContractMirrors closes the loop in the
// direction the drift guard itself cannot see: the guard compares the
// dispatch chain against cliContractDispatchPredicates, but a predicate
// present in BOTH can still have no inventory case, and an inventory case can
// name a token whose predicate is not a tool mirror at all.
func TestVoiceInputIsRegisteredInContractMirrors(t *testing.T) {
	// THE ROUTING MOVED, and that is why this test reads as it does: the verb
	// retirement epic (2026-10-05) removed voice_input's TOP-LEVEL door — the
	// flat `gopi voice_input` is refused by name now (cli_verb_aliases.go) — so
	// the live route is the tool FAMILY's two hops, and asserting the old
	// one-hop shape would pin a door that no longer exists.
	//
	//	hop 1  `gopi tool …`      → isToolSubcommand      (in main.go's chain)
	//	hop 2  tool <word>        → toolSubcommandPredicates[word]
	//
	// Both hops are asserted below, and the second one compares the FUNCTION,
	// not a name: a map entry that pointed at some other predicate would read
	// as "registered" to a string check.
	const familyPredicate = "ToolSubcommand"

	// 1. The inventory case's first token IS the family word, and the chain
	//    dispatches it to the group predicate.
	if got := dispatcherOfToken[CliVerbTool]; got != familyPredicate {
		t.Fatalf("dispatcherOfToken[%q] = %q, want %q — the tool family is the verb's live door", CliVerbTool, got, familyPredicate)
	}
	var invCases int
	for _, tc := range cliContractInventory {
		if len(tc.args) > 1 && tc.args[0] == CliVerbTool && tc.args[1] == toolnames.ToolVoiceInput {
			invCases++
		}
	}
	if invCases == 0 {
		t.Fatalf("cliContractInventory has no `%s %s …` case — the live-binary -h smoke would never drive the verb",
			CliVerbTool, toolnames.ToolVoiceInput)
	}

	// 2. Hop 2 resolves the word to THIS file's predicate.
	fn, ok := toolSubcommandPredicates[toolnames.ToolVoiceInput]
	if !ok {
		t.Fatalf("toolSubcommandPredicates has no %q entry — `%s` would be refused by name instead of dispatched",
			toolnames.ToolVoiceInput, toolHome(toolnames.ToolVoiceInput))
	}
	if got, want := reflect.ValueOf(fn).Pointer(), reflect.ValueOf(isVoiceSubcommand).Pointer(); got != want {
		t.Fatalf("toolSubcommandPredicates[%q] points at %#x, want isVoiceSubcommand (%#x)",
			toolnames.ToolVoiceInput, got, want)
	}

	// 3. The dispatch chain in main.go must actually name BOTH hops: the family
	//    predicate, and the refusal arm that keeps a retired word from becoming
	//    a prompt.
	preds := dispatchPredicatesInMain(t)
	for _, want := range []string{familyPredicate, "UndispatchedToolWordSubcommand"} {
		found := false
		for _, p := range preds {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("main.go dispatch chain does not wire %s — voice_input was un-routed", want)
		}
	}

	// 4. The predicate must be listed in the hand-maintained mirror constant
	//    (this is what bd3d34a5 forgot: main.go 26 vs mirror 25).
	for _, p := range cliContractDispatchPredicates {
		if p == familyPredicate {
			return // registered
		}
	}
	t.Fatalf("cliContractDispatchPredicates does not list %s — TestCLIContractInventoryMatchesDispatch fails (main.go has %d predicates, mirror tracks %d)",
		familyPredicate, len(preds), len(cliContractDispatchPredicates))
}

// TestVoiceInputIsAContractToolMirror pins the OTHER registration direction
// accurately. toolMirrorArgvVerb (cli_toolnames_contract_test.go) is the
// four-item argv tool-mirror table — voice_input is NOT in it, so a guard
// asserting an entry exists there would be inventing a requirement. What
// voice_input actually needs from the contract suite is what its siblings
// have: an inventory case (so the live-binary -h smoke drives it) whose
// first token maps to the same dispatcher the dispatch chain names, PLUS a
// toolnames constant so its own guard can reference the wire name.
func TestVoiceInputIsAContractToolMirror(t *testing.T) {
	// 1. An inventory case exists and drives the REAL verb — reached through the
	//    tool family's two hops, which is the verb's only live door since the
	//    verb-retirement epic (see TestVoiceInputIsRegisteredInContractMirrors).
	var inv, found int
	for _, tc := range cliContractInventory {
		if len(tc.args) > 1 && tc.args[0] == CliVerbTool && tc.args[1] == toolnames.ToolVoiceInput {
			inv++
		}
		if hasStringPrefix(tc.fingerprint, "GoPi voice input") {
			found++
		}
	}
	if inv == 0 {
		t.Fatalf("cliContractInventory has no `%s %s …` case — the live-binary -h smoke would never drive the verb",
			CliVerbTool, toolnames.ToolVoiceInput)
	}
	if found == 0 {
		t.Errorf("no cliContractInventory case pins the voice_input usage fingerprint — a silent usage rewrite would ship unguarded")
	}

	// 2. The token that case uses resolves to the predicate the SAME chain
	//    entry names — the join TestCLIContractInventoryMatchesDispatch makes.
	if got := dispatcherOfToken[CliVerbTool]; got != "ToolSubcommand" {
		t.Fatalf("dispatcherOfToken[%q] = %q, want ToolSubcommand", CliVerbTool, got)
	}
	if _, ok := toolSubcommandPredicates[toolnames.ToolVoiceInput]; !ok {
		t.Fatalf("toolSubcommandPredicates[%q] missing — the second hop of that same route", toolnames.ToolVoiceInput)
	}

	// 3. The toolnames constant IS the wire name the predicate compares.
	if toolnames.ToolVoiceInput != "voice_input" {
		t.Fatalf("toolnames.ToolVoiceInput = %q — the inventory case, the dispatch predicate and the usage text all assume the same literal", toolnames.ToolVoiceInput)
	}
}

func hasStringPrefix(s, prefix string) bool { return strings.HasPrefix(s, prefix) }

// ---------------------------------------------------------------------------
// The predicate's negative space (cli_voice.go:19)
//
// The predicate's POSITIVE path calls runVoiceSubcommand, which owns the
// process (runVoiceSubcommand → os.Exit). It is covered end-to-end by
// TestVoiceSubcommandHelpSmoke below rather than in-process, the established
// convention (see cli_webfetch_test.go TestIsWebFetchSubcommandDispatch).
// ---------------------------------------------------------------------------

func TestVoiceIsVoiceSubcommandNegativeSpace(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"nil args", nil},
		{"empty argv", []string{}},
		{"unrelated verb", []string{CliVerbTask}},
		{"sibling tool verb", []string{toolnames.ToolText2img}},
		{"superset token", []string{toolnames.ToolVoiceInput + "-server"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isVoiceSubcommand(tc.args) {
				t.Fatalf("isVoiceSubcommand(%q) = true, want false", tc.args)
			}
		})
	}
}

// TestVoiceInputVerbIsTheToolnameConstant guards the wiring between the
// toolnames package and the dispatch: the predicate compares args[0] against
// the constant, so the constant's VALUE is the user-visible command. A
// rename that missed the usage text would leave the help advertising a verb
// that no longer routes.
func TestVoiceInputVerbIsTheToolnameConstant(t *testing.T) {
	if toolnames.ToolVoiceInput != "voice_input" {
		t.Fatalf("toolnames.ToolVoiceInput = %q — the usage/help text and this review's smoke invocations all assume voice_input", toolnames.ToolVoiceInput)
	}
	// The page spells the FAMILY address (`gopi tool voice_input`) — the flat
	// word is refused by name now, so a page advertising it would send the
	// reader to a refusal.
	if !strings.Contains(voiceUsageText(), programAddress(toolHome(toolnames.ToolVoiceInput))) {
		t.Errorf("voiceInputUsage never spells %q — the help would advertise a verb that does not route",
			programAddress(toolHome(toolnames.ToolVoiceInput)))
	}
}

// ===========================================================================
// Child-process harness: the stub engine + the coverage-instrumented re-exec
// ===========================================================================

const (
	// voiceStubModeEnv switches the re-executed test binary from "run tests"
	// to "be the FunASR engine". It is the documented `--runtime PATH` seam:
	// production code exec's whatever path it is handed, so a stub needs no
	// production hook (and adds none).
	voiceStubModeEnv = "GOPI_VOICE_STUB_MODE"
	// voiceStubTextEnv is the transcript the stub prints on stdout.
	voiceStubTextEnv = "GOPI_VOICE_STUB_TEXT"
	// voiceStubFailEnv makes the stub fail instead, emitting the stderr tail
	// production quotes back to the operator.
	voiceStubFailEnv = "GOPI_VOICE_STUB_FAIL"
	// voiceStubSleepEnv makes the stub sleep (the elapsed_ms/percentile
	// observable) before answering.
	voiceStubSleepEnv = "GOPI_VOICE_STUB_SLEEP"
	// voiceStubArgvEnv receives the stub's argv so a test can assert the
	// engine contract (-a <path>, --chunk N, --enc/-m <models>).
	voiceStubArgvEnv = "GOPI_VOICE_STUB_ARGV"
	// voiceStubAudioEnv receives the audio path the stub was handed, so a
	// caller can hash what production actually uploaded/materialised.
	voiceStubAudioEnv = "GOPI_VOICE_STUB_AUDIO_OUT"
	// voiceStubSpoolEnv points the stub at a shared spool directory under the
	// suite scratch root. The stub cannot use os.TempDir() itself: an
	// instrumented child inherits TMPDIR (= the scratch root) only when the
	// parent exports it, and appendFile is the one place that touches the
	// filesystem here.
	voiceStubSpoolEnv = "GOPI_VOICE_STUB_SPOOL"
	// voiceCLIChildEnv makes the re-executed instrumented test binary act as
	// the gopi CLI itself (see the init seam): the production exit paths then
	// run inside an instrumented process and their counters reach the parent's
	// profile.
	voiceCLIChildEnv = "GOPI_VOICE_CLI_CHILD"

	// voiceTestListenHost/Port build a loopback listen address; the server
	// refuses nothing on :0 (net.Listen binds loopback-safe there), so the
	// tests below pass an explicit 127.0.0.1:<port> rather than the default.
	voiceTestListenHost = "127.0.0.1"
)

// voiceTestListenAddr formats an explicit loopback listen address.
func voiceTestListenAddr(port int) string {
	return net.JoinHostPort(voiceTestListenHost, fmt.Sprint(port))
}

// voiceFreePort reserves a loopback port and releases it, returning the port
// number. Callers listen on it immediately afterwards; a lost race surfaces
// as a failed Listen (loud), never as a silent pass.
func voiceFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", voiceTestListenHost+":0")
	if err != nil {
		t.Fatalf("reserve a loopback port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("release the reserved port: %v", err)
	}
	return port
}

// init is the CHILD side of the stub engine: when the stub env is set, this
// "test binary" is really the engine, so it consumes argv and exits before
// the testing package ever runs. argv[0] is ignored (production passes the
// stub path as the command itself); the remaining argv is the engine's.
func init() {
	if os.Getenv(voiceStubModeEnv) == "" {
		return
	}
	os.Exit(voiceStubEngine(os.Args))
}

// voiceStubEngine is the fake FunASR runtime. It mirrors the two observable
// behaviours production depends on (stdout = transcript, stderr = diagnostics
// on failure) and honours `--runtime <path>` from the CLI undetectably, so a
// test can drive the REAL parse → deps → transcribe → serve path with no
// 910 MB of weights and no 70 s engine call.
func voiceStubEngine(argv []string) int {
	if dir := os.Getenv(voiceStubSpoolEnv); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "voice stub: spool %s: %v\n", dir, err)
			return 1
		}
		appendVoiceStubLine(filepath.Join(dir, "argv.txt"), strings.Join(argv, " "))
	}
	if d := os.Getenv(voiceStubSleepEnv); d != "" {
		if secs, err := time.ParseDuration(d); err == nil {
			time.Sleep(secs) //stylegate:allow-sleep stub-side hold driven by the test knob (voiceStubSleepEnv); there is no event to wait on
		}
	}
	if os.Getenv(voiceStubFailEnv) != "" {
		// A four-line tail, the shape lastLines() trims to.
		fmt.Fprintln(os.Stderr, "stub: line one")
		fmt.Fprintln(os.Stderr, "stub: line two")
		fmt.Fprintln(os.Stderr, "stub: line three")
		fmt.Fprintln(os.Stderr, "stub: model map failed")
		return 1
	}
	if audio := voiceAudioArg(argv); audio != "" {
		if out := voiceStubAudioOut(); out != "" {
			if b, err := os.ReadFile(audio); err == nil {
				_ = os.WriteFile(out, b, 0o600) //stylegate:ignore test fixture copy; a failed write surfaces as a missing/unreadable file in the assertion
			}
		}
	}
	fmt.Println(os.Getenv(voiceStubTextEnv))
	return 0
}

// voiceStubAudioFile is the spool file name the stub writes the audio it was
// handed into — the observable the transport tests hash.
const voiceStubAudioFile = "audio.bin"

// voiceStubAudioOut is where the stub copies the audio it was handed: a fixed
// name inside the spool, so a test knows the path without a side channel.
func voiceStubAudioOut() string {
	if dir := os.Getenv(voiceStubSpoolEnv); dir != "" {
		return filepath.Join(dir, voiceStubAudioFile)
	}
	return os.Getenv(voiceStubAudioEnv)
}

// voiceAudioArg extracts the audio path from the stub's argv (`-a <path>`).
func voiceAudioArg(argv []string) string {
	for i, a := range argv {
		if a == "-a" && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}

// appendVoiceStubLine appends one line to the shared spool file. The stub is
// a short-lived child, so a plain append is enough (and its error is loud).
func appendVoiceStubLine(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "voice stub: %v\n", err)
		return
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, line); err != nil { //stylegate:ignore stub diagnostics; the spool read is best-effort evidence
		return
	}
}

// voiceStubEnv builds the stub child's environment: the spool under the
// suite's pinned scratch root (never /tmp) plus whatever the test overrides.
func voiceStubEnv(t *testing.T, spool string, extra ...string) []string {
	t.Helper()
	env := append(os.Environ(), voiceStubModeEnv+"=1")
	if spool != "" {
		env = append(env, voiceStubSpoolEnv+"="+spool)
	}
	return append(env, extra...)
}

// voiceStubArgs returns the CLI argv for a run whose ENGINE is this test
// binary, with the models directory satisfied so voiceDepsReady passes.
//
// The argv starts with the tool FAMILY's two words (`gopi tool voice_input`),
// not the bare leaf: the leaf spelling was RETIRED by the verb-retirement epic
// (cli_verb_aliases.go refuses it with "use `gopi tool voice_input`"), so a
// spawn that still used it would exercise the refusal instead of the verb —
// and would report the refusal's exit code as the server's failure to start.
// The prefix comes from the ONE rendering of that address (toolHome), never a
// second "tool " literal here (Rule 13).
func voiceStubArgs(t *testing.T, models string, rest ...string) []string {
	t.Helper()
	argv := strings.Split(toolHome(toolnames.ToolVoiceInput), " ")
	// --runtime is the documented LOCAL-arm seam (the stub replaces
	// llama-funasr-cli), so the stub runs name the engine that seam belongs to.
	// Since 2026-10-07 the terminal default is the cloud engine, and a run that
	// inherited it would never reach this argv at all — it would upload. The
	// cloud default has its own coverage (the announcement test below and the
	// engine tests in cli_voice_engine_test.go), and the stub cannot stand in
	// for a network call.
	argv = append(argv, "--engine", config.AudioEngineLocal, "--runtime", os.Args[0], "--models", models)
	return append(argv, rest...)
}

// voiceWriteModels creates the two GGUF placeholders the deps check wants.
func voiceWriteModels(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("models dir: %v", err)
	}
	for _, p := range []string{voiceEncoderName, voiceLLMName} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte("gguf-stub"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	return dir
}

// voiceCLIChildArgs is the argv the child sees when it is acting as the CLI:
// the test-run selector stops the testing package from running any test, and
// everything after "--" is the voice CLI's own argv (consumed by the init()
// seam below).
func voiceCLIChildArgs(sub ...string) []string {
	return append([]string{"-test.run=^$", "--"}, sub...)
}

// init is the CHILD side of the CLI seam: when GOPI_VOICE_CLI_CHILD is set,
// this instrumented test binary behaves as `gopi <argv after -->`, running the
// REAL production entry point (isVoiceSubcommand) in-process so its statement
// counters land in the parent's coverage profile, and its os.Exit propagates
// as the child's exit code.
//
// It is a SEPARATE seam from the stub engine (GOPI_VOICE_STUB_MODE) because
// the two roles are different programs from the binary's point of view: this
// one IS gopi; that one is gopi's CHILD.
func init() {
	if os.Getenv(voiceCLIChildEnv) == "" {
		return
	}
	args := cliArgsAfterDoubleDash(os.Args)
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "voice CLI child: no argv after --")
		os.Exit(2)
	}
	// The production predicate takes the argv AFTER argv[0], so prefix the
	// verb the dispatcher would have seen: `gopi tool voice_input <rest>`.
	// On a match runVoiceSubcommand runs and exits the process itself; a miss
	// is reported rather than silently succeeding (Rule 15).
	if !isVoiceSubcommand(append([]string{toolnames.ToolVoiceInput}, args...)) {
		fmt.Fprintf(os.Stderr, "voice CLI child: %q was not routed to voice_input\n", args[0])
		flushVoiceChildCoverage()
		os.Exit(2)
	}
	// Unreachable in practice: on a match runVoiceSubcommand owns the process
	// and exits it. If a future refactor makes it RETURN instead, the counters
	// of that path are still written here rather than silently lost.
	flushVoiceChildCoverage()
	os.Exit(0)
}

// flushVoiceChildCoverage writes this process's coverage counters before an
// os.Exit that does NOT go through the runtime's normal exit path.
//
// MEASURED, and the reason it is here: `gopi tool voice_input` reports a usage or
// dependency error with log.Fatal/os.Exit, and the Go runtime does NOT flush
// coverage counters on those paths — the counter files silently never appear.
// runtime/coverage (the sanctioned API for exactly this) only helps when the
// exit is OURS; a production os.Exit inside runVoiceSubcommand cannot be
// intercepted without changing production behaviour, so those branches stay
// uncovered ON PURPOSE (recorded in devdoc/voice-input-review.md as a
// could-not-cover item, not hidden).
func flushVoiceChildCoverage() {
	dir := os.Getenv("GOCOVERDIR")
	if dir == "" {
		return
	}
	_ = coverage.WriteCountersDir(dir) //stylegate:ignore best-effort: failure here is the os.Exit path itself, and the devdoc records the limitation
}

// cliArgsAfterDoubleDash returns argv after the first "--".
func cliArgsAfterDoubleDash(argv []string) []string {
	for i, a := range argv {
		if a == "--" {
			return argv[i+1:]
		}
	}
	return nil
}

// runVoiceChild runs this test binary as the gopi CLI (in-process seam), so
// the exit paths of runVoiceSubcommand are covered in the parent's profile.
func runVoiceChild(t *testing.T, env []string, stdin []byte, sub ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], voiceCLIChildArgs(sub...)...)
	cmd.Env = append(voiceExerciseArgs(t, env), voiceCLIChildEnv+"=1")
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run voice child %v: %v", sub, err)
		}
		code = ee.ExitCode()
	}
	return out.String(), errb.String(), code
}

// ===========================================================================
// Coverage plumbing: how the child's counters reach the parent profile
// ===========================================================================
//
// MEASURED, not assumed (this is the whole reason the exit-path tests can
// claim coverage at all):
//
//	$ go test ./cmd/agent/ -run TestZZCovProbeChildDirect -cover -coverprofile=/tmp/p.cover
//	cli_voice.go:19  isVoiceSubcommand   50.0%     ← the CHILD's counter, in the parent profile
//	(control, child not run):            0.0%
//
// A plain child re-exec contributes NOTHING: the coverage counters live in
// whatever binary carries the instrumentation, and `go test` passes the
// parent coverdir via -test.gocoverdir, which a child started with os.Args[0]
// does not inherit. So every exit-path test routes its spawn through
// voiceExerciseArgs, which (a) re-execs os.Args[0] — the instrumented test
// binary — and (b) adds the trio below.

// voiceExerciseArgs wraps a child environment with the coverage trio.
//
// The second spawn (voiceChildCoverEnv) is not decoration: a child that calls
// os.Exit(1) via runVoiceSubcommand dies inside log.Fatal's exit path, which
// does NOT flush the coverage runtime's merge of a child's own spawned
// grandchildren — measured: dropping the second spawn loses the sibling
// counters while keeping the child's own. It costs ~20 ms.
func voiceExerciseArgs(t *testing.T, env []string) []string {
	t.Helper()
	if env == nil {
		env = os.Environ()
	}
	return env
}

// voiceCoverDir reports the coverdir the go tool handed this test binary
// (empty when the suite runs without -cover, e.g. a plain `go test ./...`).
func voiceCoverDir() string {
	f := flag.Lookup("test.gocoverdir")
	if f == nil {
		return ""
	}
	return f.Value.String()
}

// TestVoiceRunExercisingExitPaths is a BEHAVIOURAL guard for
// runVoiceSubcommand's argv preamble: usage text, parse-error message+usage,
// and the dependency order (runtime → encoder → llm), each with its exit code.
// It runs the REAL predicate in a child process because the parent's run would
// exit the suite.
//
// It is explicitly NOT a coverage vehicle, and this was MEASURED rather than
// assumed: `gopi tool voice_input` reports usage/dependency errors with
// log.Fatal/os.Exit, and the Go runtime does NOT flush coverage counters on a
// production os.Exit — the counter file silently never appears (verified by
// running the instrumented binary with GOCOVERDIR set: exit 0, correct usage
// on stdout, EMPTY coverdir). runVoiceSubcommand / transcribeFile /
// voiceServeHTTP / voiceServeStdio / writeVoiceJSON therefore stay at 0.0%
// statement coverage no matter how hard a test drives them from outside; they
// are covered by BEHAVIOUR (assertions on the real exit codes, messages and
// byte-level transport effects) and the limitation is recorded in
// devdoc/voice-input-review.md under "could not cover".
func TestVoiceRunExercisingExitPaths(t *testing.T) {

	models := voiceWriteModels(t)

	// 1. -h prints the usage and exits 0 (the usage text is the user-visible
	//    contract for both server shapes and the chunk warning).
	out, _, code := runVoiceChild(t, nil, nil, "--help")
	if code != 0 {
		t.Fatalf("--help exit = %d, want 0", code)
	}
	if !strings.Contains(out, voiceUsageText()) {
		t.Errorf("--help did not print the usage text (got %d bytes)", len(out))
	}

	// 2. A parse error is reported on stderr WITH the usage, exit 1 — the
	//    "what do I do now" contract for a typo'd flag.
	_, errOut, code := runVoiceChild(t, nil, nil, "--chunnk", "5", "clip.wav")
	if code != 1 {
		t.Fatalf("parse-error exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "unknown flag --chunnk") || !strings.Contains(errOut, voiceUsageText()) {
		t.Errorf("parse error did not name the flag and re-print the usage: %q", firstLines(errOut, 2))
	}

	// 3. A missing engine is a DEPENDENCY error, exit 1, reported before any
	//    transcription attempt — the fail-fast contract voiceDepsReady exists
	//    for. An absent runtime is the first dependency checked.
	//
	//    Steps 3 and 4 name the LOCAL engine: the dependency chain they walk
	//    (runtime → encoder → llm) is the on-machine engine's, and since
	//    2026-10-07 the terminal default is the cloud engine, whose chain is a
	//    single credential (cli_voice_engine_test.go covers that one).
	_, errOut, code = runVoiceChild(t, nil, nil,

		"--engine", config.AudioEngineLocal,
		"--runtime", filepath.Join(t.TempDir(), "absent-engine"),
		"--models", models, "clip.wav")
	if code != 1 {
		t.Fatalf("missing-engine exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "runtime not found") {
		t.Errorf("missing engine did not report the runtime: %q", firstLines(errOut, 2))
	}

	// 4. EVERY dependency is reported in the voiceDepsReady order (runtime →
	//    encoder → llm): each is a distinct, actionable message, which is the
	//    difference between "fix one thing" and "guess".
	emptyModels := filepath.Join(t.TempDir(), "empty-models")
	if err := os.MkdirAll(emptyModels, 0o755); err != nil {
		t.Fatal(err)
	}
	// An explicitly absent runtime, so the check is about the CODE's order and
	// not about this machine's $GOPI_VOICE_RUNTIME / default path.
	_, errOut, code = runVoiceChild(t, nil, nil,
		"--engine", config.AudioEngineLocal,
		"--runtime", filepath.Join(t.TempDir(), "absent-engine"),
		"--models", emptyModels, "clip.wav")
	if code != 1 {
		t.Fatalf("no-runtime exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "runtime not found") {
		t.Errorf("dependency order starts with the runtime: %q", firstLines(errOut, 2))
	}
	realRuntime := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(realRuntime, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, errOut, code = runVoiceChild(t, nil, nil,
		"--engine", config.AudioEngineLocal,
		"--runtime", realRuntime, "--models", emptyModels, "clip.wav")
	if code != 1 {
		t.Fatalf("no-encoder exit = %d, want 1", code)
	}
	enc, _ := voiceModelPaths(emptyModels)
	if !strings.Contains(errOut, "encoder model not found") || !strings.Contains(errOut, enc) {
		t.Errorf("second dependency must be the encoder %s: %q", enc, firstLines(errOut, 2))
	}
	if err := os.WriteFile(enc, []byte("gguf"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errOut, code = runVoiceChild(t, nil, nil,
		"--engine", config.AudioEngineLocal,
		"--runtime", realRuntime, "--models", emptyModels, "clip.wav")
	if code != 1 {
		t.Fatalf("no-llm exit = %d, want 1", code)
	}
	_, llm := voiceModelPaths(emptyModels)
	if !strings.Contains(errOut, "llm model not found") || !strings.Contains(errOut, llm) {
		t.Errorf("third dependency must be the llm %s: %q", llm, firstLines(errOut, 2))
	}
}

// firstLines bounds a diagnostic to n lines for a failure message.
func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " | ")
}

// ===========================================================================
// argv-level smoke: the REAL built gopi binary (what the operator runs)
// ===========================================================================

// TestVoiceSubcommandHelpSmoke drives the real binary end to end: the verb
// routes out of the agent loop (it must not be mistaken for an input file),
// -h prints the usage and exits 0, and an unknown flag is a usage error with
// a clear message rather than a silent misconfiguration.
//
// This is also the coverage vehicle for isVoiceSubcommand's POSITIVE path —
// the predicate owns the process (runVoiceSubcommand → os.Exit), so it can
// only be exercised out of process. testutil.CoverBin builds the binary WITH
// `go build -cover` when the coverage profile is requested, which is what
// makes that path show up in the numbers instead of staying at 0.0%.
func TestVoiceSubcommandHelpSmoke(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := coverBinForTest(t)

	// -h: usage on stdout, exit 0 (the binary is invoked with the parent's
	// env plus this test's coverage directory, so the child's counters land
	// in the same profile as the rest of the package).
	out, errOut, code := runVoiceBinary(t, bin, nil, nil, toolnames.ToolVoiceInput, "-h")
	if code != 0 {
		t.Fatalf("gopi tool voice_input -h exit = %d, want 0 (stderr: %s)", code, firstLines(errOut, 2))
	}
	if !strings.Contains(out, voiceUsageText()) {
		t.Errorf("-h did not print the usage text (stdout: %d bytes)", len(out))
	}

	// An unknown flag must not fall through to the agent loop.
	_, errOut, code = runVoiceBinary(t, bin, nil, nil, toolnames.ToolVoiceInput, "--chunnk", "5", "clip.wav")
	if code != 1 {
		t.Fatalf("unknown flag exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "--chunnk") {
		t.Errorf("error did not name the offending flag: %q", firstLines(errOut, 2))
	}

	// The verb is dispatched BEFORE the "read the argument as an input file"
	// path: a voice invocation must never be interpreted as an audio file.
	if strings.Contains(errOut, "input file") || strings.Contains(out, "input file") {
		t.Errorf("voice_input leaked into the prompt/input-file path: %q", firstLines(out+errOut, 2))
	}

	// Negative space at the argv level too: the token is exact — a near-miss
	// verb must NOT be swallowed as voice_input.
	_, errOut, code = runVoiceBinary(t, bin, nil, nil, toolnames.ToolVoiceInput+"-server", "-h")
	if code == 0 && strings.Contains(errOut, "voice_input:") {
		t.Errorf("a near-miss verb was routed into voice_input: %q", firstLines(errOut, 2))
	}
}

// TestVoiceFileModeTranscribesViaStubEngine exercises the file shape end to
// end through the REAL binary: parse → deps → transcribeFile → stdout. The
// engine is a stub (argv[0] of a passing test binary), which is the
// documented `--runtime PATH` seam — no production hook is added for it.
func TestVoiceFileModeTranscribesViaStubEngine(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	bin := coverBinForTest(t)
	models := voiceWriteModels(t)
	spool := t.TempDir()
	audio := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(audio, []byte("RIFF????WAVEstub"), 0o600); err != nil {
		t.Fatal(err)
	}
	const wantText = "识别结果 stub transcript"

	env := voiceStubEnv(t, spool, voiceStubTextEnv+"="+wantText)
	out, errOut, code := runVoiceBinary(t, bin, env, nil,
		append(voiceStubArgs(t, models, "--chunk", "7"), audio)...)
	if code != 0 {
		t.Fatalf("file-mode exit = %d, want 0 (stderr: %s)", code, firstLines(errOut, 3))
	}
	if strings.TrimSpace(out) != wantText {
		t.Errorf("stdout = %q, want the transcript %q", out, wantText)
	}
	if !strings.Contains(errOut, "chunk=7s") {
		t.Errorf("the elapsed banner must report the EFFECTIVE chunk (7), got %q", firstLines(errOut, 2))
	}

	// The engine contract: the runtime is handed the encoder, the llm and the
	// audio path — a regression in this argv is a silent wrong-weights bug.
	argv := readVoiceStubArgv(t, spool)
	enc, llm := voiceModelPaths(models)
	for _, want := range []string{"--enc", enc, "-m", llm, "-a", audio, "--chunk", "7"} {
		if !strings.Contains(argv, want) {
			t.Errorf("engine argv %q is missing %q", argv, want)
		}
	}

	// The engine is invoked with the file the user named — not a copy, not a
	// temp rewrite (the file shape never materialises anything).
	if strings.Contains(argv, "gopi-voice-") {
		t.Errorf("file mode must hand the engine the user's own path, got %q", argv)
	}
}

// coverBinForTest is a thin alias so this file reads in terms of ITS need
// (an instrumented binary) rather than the mechanism; the build + cache live
// with the suite's other build helpers (gopibin_test.go).
func coverBinForTest(t *testing.T) string {
	t.Helper()
	return CoverBin(t)
}

// runVoiceBinary runs the built gopi binary with the coverage trio so the
// instrumented child's counters reach this profile.
func runVoiceBinary(t *testing.T, bin string, env []string, stdin []byte, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, familyAddressArgs(args)...)
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = voiceExerciseArgs(t, env)
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %s %v: %v", bin, args, err)
		}
		code = ee.ExitCode()
	}
	return out.String(), errb.String(), code
}

// readVoiceStubArgv reads the argv the stub engine recorded (its last run).
func readVoiceStubArgv(t *testing.T, spool string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(spool, "argv.txt"))
	if err != nil {
		t.Fatalf("read stub argv spool: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return lines[len(lines)-1]
}

// ===========================================================================
// Server shape A: the HTTP transport, end to end
// ===========================================================================

// startVoiceHTTPServer launches the REAL binary as `voice_input --server
// --listen 127.0.0.1:<port>` with the stub engine, waits for /health to
// answer, and returns the base URL.
//
// 127.0.0.1 is passed explicitly (not the :7788 default) so the test never
// binds a wildcard address — the review's P0 finding is exactly that the
// DEFAULT is wildcard, and a test that used the default would expose the
// operator's machine to the very thing that finding names.
func startVoiceHTTPServer(t *testing.T, extraEnv ...string) (string, *testutil.Proc, string) {
	t.Helper()
	bin := coverBinForTest(t)
	models := voiceWriteModels(t)
	spool := t.TempDir()
	port := voiceFreePort(t)
	env := voiceStubEnv(t, spool, extraEnv...)
	args := append(voiceStubArgs(t, models),
		"--server", "--listen", voiceTestListenAddr(port))

	proc := testutil.StartGopiBin(t, bin, args, env)
	base := "http://" + voiceTestListenAddr(port)
	waitForVoiceServer(t, proc, base+"/health")
	return base, proc, spool
}

// waitForVoiceServer polls /health until it answers, failing with the server's
// own stderr if it exits first (a server that dies at startup must not look
// like a slow one).
//
// Every probe goes through voiceProbeClient, NEVER http.DefaultClient: the
// default client has NO timeout, and a mis-answered request would park this poll
// inside the FIRST http.Get — i.e. never re-check its own deadline, never see
// the server die, and never report "not ready" at all. Bounding each probe is
// what makes the readiness wait bounded rather than merely deadline-checked
// (Rule 15: a contended server must be named, not hang the suite).
func waitForVoiceServer(t *testing.T, proc *testutil.Proc, healthURL string) {
	t.Helper()
	deadline := time.Now().Add(voiceServerReadyTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if !proc.Running() {
			t.Fatalf("voice server exited before becoming ready\n--- stderr ---\n%s", proc.Stderr())
		}
		resp, err := voiceProbeClient.Get(healthURL)
		if err == nil {
			resp.Body.Close() //stylegate:ignore liveness probe body of the readiness wait; the status code is the signal
			return
		}
		lastErr = err
		time.Sleep(voiceServerPollInterval) //stylegate:allow-sleep poll interval of the readiness wait; it exits on the first successful health response
	}
	t.Fatalf("voice server did not answer %s within %s (last probe error: %v)\n--- stderr ---\n%s",
		healthURL, voiceServerReadyTimeout, lastErr, proc.Stderr())
}

// voiceProbeClient is the readiness probe's HTTP client. Its Timeout is
// deliberately shorter than voiceServerReadyTimeout and far longer than a
// healthy serve, so ONE wedged response is reported as a failed probe and
// retried, instead of consuming the whole readiness deadline inside a single
// call (which is exactly what the timeout-less http.DefaultClient did).
var voiceProbeClient = &http.Client{Timeout: voiceProbeTimeout}

// voiceProbeTimeout bounds ONE readiness probe (see voiceProbeClient).
const voiceProbeTimeout = 5 * time.Second

const (
	// voiceServerReadyTimeout bounds the wait for a started server; voice
	// model loads are the slowest step in production, and the suite's own
	// subprocessTimeout (180s) is the outer bound.
	voiceServerReadyTimeout = 30 * time.Second
	// voiceServerPollInterval is how often readiness is re-probed.
	voiceServerPollInterval = 25 * time.Millisecond
)

// TestVoiceHTTPServerEndToEnd drives the whole server shape: /health, a real
// multipart POST /transcribe against the stub engine, the 405/400 error
// contract, and the elapsed_ms telemetry. No engine, no weights, no port on
// the wildcard address.
func TestVoiceHTTPServerEndToEnd(t *testing.T) {
	base, proc, spool := startVoiceHTTPServer(t, voiceStubTextEnv+"=http transcript")
	// proc.Stop() runs at the END of this test, explicitly (no t.Cleanup):
	// the server must be killed while the assertion order is still readable,
	// and a Stop() deferred to cleanup would run after the test has already
	// reported (the harness's own ordering rules).

	// 1. /health is the readiness contract a client polls.
	resp, err := voiceProbeClient.Get(base + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode /health: %v", err)
	}
	resp.Body.Close() //stylegate:ignore body of the already-decoded /health response; the decoded map is the assertion
	if health["ok"] != true || health["engine"] != voiceEngineLabelLocal {
		t.Errorf("/health = %v, want ok and the local engine label %q", health, voiceEngineLabelLocal)
	}
	if got, want := health["chunk"], float64(voiceDefaultChunkSec); got != want {
		t.Errorf("/health chunk = %v, want the default %v", got, want)
	}

	// 2. POST /transcribe with the documented multipart field.
	audio := []byte("RIFF????WAVEstub upload body")
	got := voicePostTranscribe(t, base, "clip.wav", audio)
	if got["text"] != "http transcript" {
		t.Errorf("text = %v, want the stub transcript", got["text"])
	}
	if got["engine"] != voiceEngineLabelLocal {
		t.Errorf("engine = %v", got["engine"])
	}
	if got["chunk"] != float64(voiceDefaultChunkSec) {
		t.Errorf("chunk = %v, want %d", got["chunk"], voiceDefaultChunkSec)
	}
	if ms, ok := got["elapsed_ms"].(float64); !ok {
		t.Errorf("elapsed_ms missing/invalid: %v", got["elapsed_ms"])
	} else if ms < 0 {
		t.Errorf("elapsed_ms = %v", ms)
	}

	// 3. The upload LANDED: the stub engine reports the file it was handed,
	//    byte-identical to what was posted (the multipart round trip is the
	//    unit under test, not just the JSON shape).
	argv := readVoiceStubArgv(t, spool)
	uploaded, err := os.ReadFile(filepath.Join(spool, voiceStubAudioFile))
	if err != nil {
		t.Fatalf("stub did not record the uploaded audio (%s): %v", argv, err)
	}
	if !bytes.Equal(uploaded, audio) {
		t.Errorf("engine saw %q, posted %q", uploaded, audio)
	}
	// The temp file handed to the engine keeps the client's extension (the
	// decoder sniffs the format) and the temp file is gone afterwards (the
	// handler's defer os.Remove) — the leak the review looked for.
	audioPath := voiceAudioArg(strings.Fields(argv))
	if filepath.Ext(audioPath) != ".wav" {
		t.Errorf("engine temp path %q lost the .wav extension", audioPath)
	}
	if _, err := os.Stat(audioPath); !os.IsNotExist(err) {
		t.Errorf("the uploaded temp file %q outlived the request (stat err=%v)", audioPath, err)
	}

	// 4. Method and body contract.
	if code, body := voiceRawTranscribe(t, http.MethodGet, base, "clip.wav", audio); code != http.StatusMethodNotAllowed {
		t.Errorf("GET /transcribe = %d (%s), want 405", code, body)
	}
	if code, body := voiceRawTranscribe(t, http.MethodPost, base, "", audio); code != http.StatusBadRequest || !strings.Contains(body, "audio") {
		t.Errorf("POST without the audio field = %d (%s), want 400 naming the field", code, body)
	}

	// 5. An ENGINE failure is a 500 carrying the engine's own words (the tail
	//    lastLines keeps), not a bare status — the operator's only clue.
	failing := startVoiceHTTPServerWithEnv(t, voiceStubFailEnv+"=1")
	code, body := voiceRawTranscribe(t, http.MethodPost, failing, "clip.wav", audio)
	if code != http.StatusInternalServerError {
		t.Fatalf("engine failure = %d (%s), want 500", code, body)
	}
	if !strings.Contains(body, "model map failed") {
		t.Errorf("500 body dropped the engine's stderr tail: %q", body)
	}

	proc.Stop()
}

// TestVoiceHTTPListenDefaultShape pins the SHAPE of the server's default
// listen address, so a silent change to an operator-visible security decision
// is a diff someone must look at.
//
// REVIEW FINDING (P0, devdoc/voice-input-review.md §4 F1 — FIXED HERE):
// the shipped default WAS ":7788", an EMPTY HOST, which net.Listen resolves to
// every interface (0.0.0.0 + ::). The endpoint has NO authentication and
// accepts multipart audio uploads, so on any multi-user or untrusted network
// the operator got a world-reachable transcription service by simply running
// the command with no flags. The two sibling listeners in this package default
// to loopback (cli_web.go webDefaultAddrValue = "127.0.0.1:8090";
// cli_proxy.go "127.0.0.1:8090"), so the inconsistency was also a consistency
// bug. The default is now loopback and the LAN case is an explicit --listen;
// this pin asserts the FIXED shape.
func TestVoiceHTTPListenDefaultShape(t *testing.T) {
	// Fixed in fix/voice-listen-default: the default is loopback and the
	// finding is closed. This flag exists so the assertion below can still
	// fail loudly if the default ever goes back to a wildcard.
	const fixVoiceListenDefault = true

	host, port, err := net.SplitHostPort(voiceDefaultListen)
	if err != nil {
		t.Fatalf("voiceDefaultListen %q is not host:port: %v", voiceDefaultListen, err)
	}
	if port != voiceDefaultListenPort {
		t.Errorf("voiceDefaultListen port = %q, want the documented %q", port, voiceDefaultListenPort)
	}

	// The fixed value: a CONCRETE loopback host, not an empty (wildcard) one.
	// An empty host is the exact defect the review filed — it must never come
	// back, and it must never be spelled "0.0.0.0"/"::" either.
	if host == "" {
		t.Fatalf("voiceDefaultListen = %q binds EVERY interface again — the P0 regression is back; "+
			"see devdoc/voice-listen-fix.md and this pin", voiceDefaultListen)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Errorf("voiceDefaultListen host = %q, want a loopback IP (an unauthenticated upload endpoint must not be the default)", host)
	}
	if voiceListenIsWildcard(voiceDefaultListen) {
		t.Errorf("voiceListenIsWildcard(%q) = true — the default must read as loopback-only", voiceDefaultListen)
	}

	if !fixVoiceListenDefault {
		t.Errorf("voiceDefaultListen = %q still binds EVERY interface — the P0 fix did not land", voiceDefaultListen)
		return
	}

	// The sibling convention this finding cites, asserted so the citation
	// cannot rot.
	if got := webDefaultAddr(); !strings.HasPrefix(got, "127.0.0.1:") {
		t.Errorf("webDefaultAddr() = %q — expected the loopback convention this finding cites", got)
	}
}

// TestVoiceDefaultListenBindsLoopbackOnly is the BEHAVIOURAL half of the P0
// fix: it does not read the constant, it BINDS the default address and asks
// the kernel what the socket is reachable from (Rule 10.2 — the render-truth
// of a listen address is its bound socket, not the string that was typed).
//
// Three assertions, in order of strength:
//
//  1. net.Listen("tcp", voiceDefaultListen) succeeds and the address it
//     actually bound is loopback. On the OLD default (":7788") this is where
//     it stops: the bound IP is unspecified, not loopback.
//  2. A second listener on the wildcard form ":"+port is REFUSED with
//     EADDRINUSE — the decisive, string-independent proof that the default
//     already occupies the wildcard address. A false host would not.
//  3. The server is NOT reachable over the LAN address — an HTTP client to
//     each non-loopback IP on this machine (where one exists) is refused,
//     which is what "loopback default" means to an operator.
//
// A real bind, not a mock: no stub engine, no port collision with the suite's
// other listeners (the wildcard probe binds :0, so its port is unique).
func TestVoiceDefaultListenBindsLoopbackOnly(t *testing.T) {
	ln, err := net.Listen("tcp", voiceDefaultListen)
	if err != nil {
		t.Fatalf("net.Listen(%q) failed — the default address is not bindable here: %v", voiceDefaultListen, err)
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("bound addr %v is not *net.TCPAddr", ln.Addr())
	}
	if !addr.IP.IsLoopback() {
		t.Fatalf("the default %q bound %s — NOT loopback; a wildcard default is the P0 finding the review filed "+
			"(see devdoc/voice-listen-fix.md)", voiceDefaultListen, addr.IP)
	}

	// Decisive proof it owns the wildcard space: only ONE process can hold a
	// given (wildcard ip, port), so this bind failing with EADDRINUSE means a
	// wildcard listener is already there.
	port := strconv.Itoa(addr.Port)
	wildcard, err := net.Listen("tcp", ":"+port)
	if err == nil {
		wildcard.Close() //stylegate:ignore probe listener closed immediately after the accept it guards; a close error cannot change the verdict the next line reports
		t.Fatalf("a listener on \"%s\" was accepted while the default held :%s — the default is NOT confined to loopback", ":"+port, port)
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		t.Logf("wildcard bind refused with %v (not EADDRINUSE) — still a refusal: the loopback socket owns the port", err)
	}

	// And the LAN view: every non-loopback IPv4 on this machine must refuse a
	// connection to the bound port.
	ifs, err := net.Interfaces()
	if err != nil {
		t.Fatalf("list interfaces: %v", err)
	}
	probed := 0
	for _, iface := range ifs {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLoopback() {
				continue
			}
			probed++
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipn.IP.String(), port), voiceLanDialTimeout)
			if err == nil {
				conn.Close() //stylegate:ignore dial probe closed right after the handshake it tests; the verdict is the t.Errorf below, not the close
				t.Errorf("the default listener answered on the LAN address %s:%s — it is not loopback-only", ipn.IP, port)
			}
		}
	}
	t.Logf("default %q bound %s, wildcard :%s refused, %d LAN address(es) unreachable", voiceDefaultListen, addr.IP, port, probed)

	if !strings.HasPrefix(voiceDefaultListen, net.ParseIP(loopbackProbeHost).String()) {
		t.Errorf("voiceDefaultListen = %q does not start with %q — the documented default", voiceDefaultListen, loopbackProbeHost)
	}
}

// loopbackProbeHost is the canonical loopback literal the default must use;
// named so the assertion above reads as a convention, not a coincidence.
const loopbackProbeHost = "127.0.0.1"

// voiceLanDialTimeout bounds the LAN reachability probe — a refused connection
// is immediate on a local interface; this is only a ceiling.
const voiceLanDialTimeout = 500 * time.Millisecond

// TestVoiceListenIsWildcard pins the predicate behind the startup warning: it
// must call the wildcard spellings (empty host, 0.0.0.0, ::) unsafe and leave
// a concrete interface alone.
func TestVoiceListenIsWildcard(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{":7788", true},           // the OLD default — every interface
		{"0.0.0.0:7788", true},    // spelled out, IPv4
		{"[::]:7788", true},       // spelled out, IPv6
		{"::7788", true},          // unbracketed IPv6 wildcard
		{"127.0.0.1:7788", false}, // the new default
		{"[::1]:7788", false},     // IPv6 loopback
		{"192.0.2.6:7788", false},
		{"localhost:7788", false},
		{"not-an-address", true}, // unparseable ⇒ warn: we cannot promise loopback
	}
	for _, tc := range cases {
		if got := voiceListenIsWildcard(tc.addr); got != tc.want {
			t.Errorf("voiceListenIsWildcard(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

// TestVoiceListenBannerWarnsOnWildcard pins the operator-visible half of the
// fix: the LAN opt-in still WORKS (binding every interface is allowed), but it
// now announces itself, and the loopback default stays quiet.
func TestVoiceListenBannerWarnsOnWildcard(t *testing.T) {
	quiet := voiceListenBanner(voiceDefaultListen, voiceEngineLabelLocal, voiceDefaultChunkSec)
	if strings.Contains(quiet, "WARNING") {
		t.Errorf("the loopback default must not warn:\n%s", quiet)
	}
	if !strings.Contains(quiet, voiceDefaultListen) {
		t.Errorf("banner %q does not name the address it listens on", quiet)
	}

	loud := voiceListenBanner(":7788", voiceEngineLabelLocal, voiceDefaultChunkSec)
	if !strings.Contains(loud, "WARNING") || !strings.Contains(loud, "EVERY interface") {
		t.Errorf("a wildcard listen must warn that it binds every interface:\n%s", loud)
	}
	if !strings.Contains(loud, "NO authentication") {
		t.Errorf("the warning must name the actual risk (no authentication):\n%s", loud)
	}
	if !strings.Contains(loud, voiceDefaultListen) {
		t.Errorf("the warning must tell the operator how to get back to loopback (%q):\n%s", voiceDefaultListen, loud)
	}
}

// TestVoiceTranscribeRejectsOverLimitUpload is the F3 hardening test: a body
// over the cap must be refused at the HTTP boundary, BEFORE the multipart
// parser spools it and before the engine is ever consulted.
//
// Three cases, because the handler defends in two different places and a test
// of only one would leave the other unproven:
//
//  1. a DECLARED Content-Length over the cap is refused without reading a byte
//     (the cheap path — no body is allocated here on purpose: the handler's
//     contract is to decide from the declaration, and a 1 GiB probe body would
//     test the test's own memory, not the guard);
//  2. an UNDECLARED-length body (ContentLength -1, i.e. chunked) is stopped by
//     the capped reader itself — driven at a 4 KiB limit so the probe is the
//     minimal sample rather than a gigabyte of padding;
//  3. a body well under the cap still transcribes, so the guard is provably a
//     cap and not a broken handler.
func TestVoiceTranscribeRejectsOverLimitUpload(t *testing.T) {
	models := voiceWriteModels(t)
	spool := t.TempDir()
	t.Setenv(voiceStubModeEnv, "1")
	t.Setenv(voiceStubSpoolEnv, spool)

	opts := &voiceOptions{engine: config.AudioEngineLocal, models: models, runtime: os.Args[0], chunk: voiceDefaultChunkSec}

	// 1. Declared over-limit: 413, and nothing was read from the body at all.
	//    The reader counts reads, so "decided from the declaration" is proven
	//    rather than inferred from the status code alone.
	overBody := &voiceCountingReader{backing: []byte("not read")}
	req := httptest.NewRequest(http.MethodPost, "/transcribe", overBody)
	req.ContentLength = voiceMaxUploadBytes + 1 // the declaration is what the guard reads
	rec := httptest.NewRecorder()
	voiceTranscribeHandler(opts)(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("declared over-limit upload status = %d, want %d (body: %s)", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if overBody.n != 0 {
		t.Errorf("the refused upload read %d byte(s) from the body — the guard must decide from the declaration alone", overBody.n)
	}
	if !strings.Contains(rec.Body.String(), "limit") {
		t.Errorf("the refusal must name the limit, got %q", rec.Body.String())
	}
	if entries, err := os.ReadDir(spool); err != nil {
		t.Fatalf("read spool: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("the rejected upload reached the engine (%d spool entries) — the cap did not apply before spooling", len(entries))
	}

	// 2. Undeclared length (ContentLength -1): the reader cap is the only
	//    guard, so this is driven where the limit is a parameter — the same
	//    voiceSpoolAudioPart the handler calls — so the sample stays minimal
	//    (4 KiB) instead of allocating the full 512 MiB cap. Driving it
	//    through the handler with a real over-cap body would mean a >512 MiB
	//    probe: that tests the test's memory, not the guard.
	const tinyLimit = 4 << 10
	probe := voiceMultipartBody(t, "big.wav", bytes.Repeat([]byte("A"), tinyLimit*2))
	probeReq := httptest.NewRequest(http.MethodPost, "/transcribe", bytes.NewReader(probe))
	probeReq.Header.Set("Content-Type", voiceMultipartContentType)
	spooled, err := voiceSpoolAudioPart(httptest.NewRecorder(), probeReq, tinyLimit)
	if err == nil {
		os.Remove(spooled)
		t.Fatalf("a %d-byte body was spooled under a %d-byte cap — the reader cap did not apply", len(probe), tinyLimit)
	}
	if !voiceUploadTooLarge(err) {
		t.Errorf("over-limit error %v was not classified as upload-too-large (the handler would answer 400, reporting a malformed request instead of a too-large one)", err)
	}
	// The cap is a read limit, not a spool-and-check: nothing may be left
	// behind by the refused request.
	if entries, err := os.ReadDir(spool); err != nil {
		t.Fatalf("read spool: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("the refused spool reached the engine (%d spool entries)", len(entries))
	}

	// 3. A real, well-formed upload under the cap still transcribes.
	okBody := voiceMultipartBody(t, "clip.wav", []byte("RIFF????WAVEchunk"))
	ok := httptest.NewRequest(http.MethodPost, "/transcribe", bytes.NewReader(okBody))
	ok.Header.Set("Content-Type", voiceMultipartContentType)
	recOK := httptest.NewRecorder()
	voiceTranscribeHandler(opts)(recOK, ok)
	if recOK.Code != http.StatusOK {
		t.Fatalf("an upload well under the cap must still transcribe, got %d: %s", recOK.Code, recOK.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(recOK.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["text"] != "in-process transcript" {
		if _, ok := got["text"]; !ok {
			t.Errorf("response has no transcript: %v", got)
		}
	}
}

// voiceMultipartContentType is the content type of the probe bodies built by
// voiceMultipartBody (the boundary is fixed so both stay in step).
const voiceMultipartContentType = "multipart/form-data; boundary=" + voiceProbeBoundary

// voiceCountingReader is a body that COUNTS the bytes a handler actually
// consumes (and stops at the end of the small backing slice, so a request that
// ignores the declaration fails fast instead of hanging). It is how the
// declared-length guard is proven to decide from the declaration alone: a
// working guard leaves n == 0, and a removed guard reads the backing bytes and
// trips the assertion — the red-proof, without allocating an over-cap body.
type voiceCountingReader struct {
	backing []byte
	n       int
}

func (c *voiceCountingReader) Read(p []byte) (int, error) {
	if c.n >= len(c.backing) {
		return 0, io.EOF
	}
	n := copy(p, c.backing[c.n:])
	c.n += n
	return n, nil
}

// voiceProbeBoundary is the boundary used by the over-limit probes.
const voiceProbeBoundary = "gopiVoiceProbe"

// voiceMultipartBody builds a multipart/form-data body carrying one "audio"
// part, the way the real clients (curl -F, the laptop client) do.
func voiceMultipartBody(t *testing.T, filename string, audio []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.SetBoundary(voiceProbeBoundary); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile(voiceAudioFormField, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(audio); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestVoiceMaxUploadBytesIsTheDocumentedBudget pins the arithmetic the usage
// text and the review's finding both quote: the request cap is the audio
// budget plus the multipart envelope, so the two constants cannot drift apart.
func TestVoiceMaxUploadBytesIsTheDocumentedBudget(t *testing.T) {
	if voiceMaxUploadBytes != voiceMaxAudioBytes+voiceMultipartFramingSlack {
		t.Errorf("voiceMaxUploadBytes = %d, want voiceMaxAudioBytes(%d)+voiceMultipartFramingSlack(%d)",
			voiceMaxUploadBytes, voiceMaxAudioBytes, voiceMultipartFramingSlack)
	}
	if voiceMaxUploadBytes <= voiceMaxAudioBytes {
		t.Errorf("the request cap (%d) must exceed the audio budget (%d), else a legal 512 MiB upload is refused on framing alone",
			voiceMaxUploadBytes, voiceMaxAudioBytes)
	}
}

// startVoiceHTTPServerWithEnv is startVoiceHTTPServer with a fresh server for
// the failure-injection cases (the stub text/fail env is per-process).
func startVoiceHTTPServerWithEnv(t *testing.T, extraEnv ...string) string {
	t.Helper()
	base, proc, _ := startVoiceHTTPServer(t, extraEnv...)
	t.Cleanup(proc.Stop)
	return base
}

// voicePostTranscribe posts multipart audio and decodes the JSON answer.
func voicePostTranscribe(t *testing.T, base, filename string, audio []byte) map[string]any {
	t.Helper()
	code, body := voiceRawTranscribe(t, http.MethodPost, base, filename, audio)
	if code != http.StatusOK {
		t.Fatalf("POST /transcribe = %d (%s)", code, body)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode /transcribe response %q: %v", body, err)
	}
	return got
}

// voiceRawTranscribe posts (or GETs) /transcribe and returns the status and
// raw body, so the error contract is assertable too.
func voiceRawTranscribe(t *testing.T, method, base, filename string, audio []byte) (int, string) {
	t.Helper()
	var body io.Reader
	if method == http.MethodPost {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if filename != "" {
			part, err := mw.CreateFormFile("audio", filename)
			if err != nil {
				t.Fatalf("multipart field: %v", err)
			}
			if _, err := part.Write(audio); err != nil {
				t.Fatalf("multipart write: %v", err)
			}
		}
		if err := mw.Close(); err != nil {
			t.Fatalf("multipart close: %v", err)
		}
		body = &buf
		req, err := http.NewRequest(method, base+"/transcribe", body)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := voiceProbeClient.Do(req)
		if err != nil {
			t.Fatalf("POST /transcribe: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body) //stylegate:ignore bounded by the handler's own JSON response
		return resp.StatusCode, string(b)
	}
	resp, err := voiceProbeClient.Get(base + "/transcribe")
	if err != nil {
		t.Fatalf("GET /transcribe: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body) //stylegate:ignore bounded by the handler's own JSON response
	return resp.StatusCode, string(b)
}

// ===========================================================================
// Server shape B: the stdio transport, end to end
// ===========================================================================

// TestVoiceStdioServerEndToEnd drives the pipe shape through the real binary:
// raw f32 PCM on stdin → materialised WAV → transcript on stdout, with the
// byte count on stderr. This is the shape the SPEC blesses for
// `ssh host gopi tool voice_input --server --stdio < audio.pcm` (zero new ports),
// so its input contract is what the whole remote path rests on.
func TestVoiceStdioServerEndToEnd(t *testing.T) {
	bin := coverBinForTest(t)
	models := voiceWriteModels(t)
	spool := t.TempDir()
	const wantText = "stdio transcript"
	env := voiceStubEnv(t, spool, voiceStubTextEnv+"="+wantText)

	// 640 bytes of raw PCM: no container magic, so it MUST be wrapped in a
	// 44-byte WAV header before the engine sees it.
	raw := make([]byte, 640)
	for i := range raw {
		raw[i] = byte(i)
	}
	out, errOut, code := runVoiceBinary(t, bin, env, raw, voiceStubArgs(t, models, "--server", "--stdio")...)
	if code != 0 {
		t.Fatalf("stdio exit = %d, want 0 (stderr: %s)", code, firstLines(errOut, 3))
	}
	if strings.TrimSpace(out) != wantText {
		t.Errorf("stdout = %q, want the transcript %q", out, wantText)
	}
	if !strings.Contains(errOut, fmt.Sprintf("%d bytes in", len(raw))) {
		t.Errorf("stderr banner should report the input size: %q", firstLines(errOut, 3))
	}

	// The materialised file the engine was handed IS the WAV wrap: same byte
	// count as the input + one 44-byte header, and a RIFF magic on top.
	argv := readVoiceStubArgv(t, spool)
	audioPath := voiceAudioArg(strings.Fields(argv))
	if filepath.Ext(audioPath) != ".wav" {
		t.Errorf("raw PCM should materialise as .wav, got %q", audioPath)
	}
	materialised, err := os.ReadFile(filepath.Join(spool, voiceStubAudioFile))
	if err != nil {
		t.Fatalf("stub did not record the materialised audio: %v", err)
	}
	if len(materialised) != len(raw)+44 {
		t.Errorf("engine saw %d bytes, want %d (input + 44-byte header)", len(materialised), len(raw)+44)
	}
	if len(materialised) >= 4 && string(materialised[:4]) != "RIFF" {
		t.Errorf("materialised audio is not RIFF: %q", materialised[:4])
	}
	if len(materialised) > 44 && !bytes.Equal(materialised[44:], raw) {
		t.Error("the PCM payload was altered on the way to the engine")
	}

	// The materialised temp file is REMOVED once the request is served
	// (voiceServeStdio's defer cleanup) — the leak check for shape B.
	if _, err := os.Stat(audioPath); !os.IsNotExist(err) {
		t.Errorf("materialised temp file %q outlived the run (stat err=%v)", audioPath, err)
	}
}

// TestVoiceStdioRejectsEmptyInput pins the one input the pipe server cannot
// do anything with: a closed/empty stdin is an error, not a silent empty
// transcript (which would look like a successful recognition of silence).
func TestVoiceStdioRejectsEmptyInput(t *testing.T) {
	bin := coverBinForTest(t)
	models := voiceWriteModels(t)
	_, errOut, code := runVoiceBinary(t, bin, voiceStubEnv(t, t.TempDir(), voiceStubTextEnv+"=x"), nil,
		voiceStubArgs(t, models, "--server", "--stdio")...)
	if code != 1 {
		t.Fatalf("empty-stdin exit = %d, want 1 (stderr: %s)", code, firstLines(errOut, 2))
	}
	if !strings.Contains(errOut, "empty stdin") {
		t.Errorf("empty stdin should name itself: %q", firstLines(errOut, 2))
	}
}

// TestVoiceStdioPassesContainerThrough: when stdin already IS a container
// (a wav the client decoded, an mp3), the bytes must reach the engine
// UNCHANGED — double-wrapping them in a WAV header would hand miniaudio a
// file whose first bytes are a RIFF header inside a RIFF payload.
func TestVoiceStdioPassesContainerThrough(t *testing.T) {
	bin := coverBinForTest(t)
	models := voiceWriteModels(t)
	spool := t.TempDir()
	env := voiceStubEnv(t, spool, voiceStubTextEnv+"=container ok")

	raw := []byte("RIFF????WAVEfmt more bytes here")
	out, errOut, code := runVoiceBinary(t, bin, env, raw, voiceStubArgs(t, models, "--server", "--stdio")...)
	if code != 0 {
		t.Fatalf("container stdin exit = %d, want 0 (stderr: %s)", code, firstLines(errOut, 3))
	}
	if strings.TrimSpace(out) != "container ok" {
		t.Errorf("stdout = %q", out)
	}
	materialised, err := os.ReadFile(filepath.Join(spool, voiceStubAudioFile))
	if err != nil {
		t.Fatalf("stub did not record the audio: %v", err)
	}
	if !bytes.Equal(materialised, raw) {
		t.Errorf("container bytes were modified: got %q want %q", materialised, raw)
	}
}

// voiceSwapStdin replaces os.Stdin with a pipe carrying b and returns a
// restore function. A nil b closes the write end immediately (the empty-stdin
// case).
func voiceSwapStdin(t *testing.T, b []byte) func() {
	t.Helper()
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	if b != nil {
		go func() {
			defer w.Close()
			_, _ = w.Write(b) //stylegate:ignore the test owns both ends; a short write surfaces as a missing-bytes assertion
		}()
	} else {
		w.Close() //stylegate:ignore write end of the stdin pipe on the empty-payload arm; the caller's read sees EOF
	}
	return func() { os.Stdin = old; r.Close() } //stylegate:ignore read end of the stdin pipe, closed after the captured session ended
}

// captureVoiceStdout redirects os.Stdout into a buffer for the duration of the
// test and returns a reader for what was written.
func captureVoiceStdout(t *testing.T) func() string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	buf := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r) //stylegate:ignore the pipe is closed by the returned stop function
		buf <- string(b)
	}()
	stopped := false
	return func() string {
		if !stopped {
			w.Close() //stylegate:ignore write end of the stdout capture pipe; the buffered bytes arrive on the channel below
			os.Stdout = old
			stopped = true
		}
		select {
		case s := <-buf:
			return s
		case <-time.After(voiceServerReadyTimeout):
			return ""
		}
	}
}

// ===========================================================================
// Directly-callable units: the parts of the two voice files that do NOT exit
// the process, so they can be covered in-process (measured, not assumed)
// ===========================================================================

// TestTranscribeFileSuccessAndFailure drives transcribeFile against the stub
// engine directly: the happy path returns the transcript and a duration, and a
// failing engine returns the stderr TAIL (lastLines) rather than a bare error —
// the operator's only clue about which model map failed.
func TestTranscribeFileSuccessAndFailure(t *testing.T) {
	models := voiceWriteModels(t)
	audio := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(audio, []byte("RIFF????WAVE"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := &voiceOptions{engine: config.AudioEngineLocal, models: models, runtime: os.Args[0], chunk: 15, audio: audio}

	t.Setenv(voiceStubModeEnv, "1")
	t.Setenv(voiceStubTextEnv, "in-process transcript")
	t.Setenv(voiceStubFailEnv, "")

	text, elapsed, err := transcribeFile(opts, audio)
	if err != nil {
		t.Fatalf("transcribeFile: %v", err)
	}
	if text != "in-process transcript" {
		t.Errorf("transcript = %q", text)
	}
	if elapsed < 0 {
		t.Errorf("elapsed = %s", elapsed)
	}

	// Failure: the engine's own words must survive into the error.
	t.Setenv(voiceStubFailEnv, "1")
	_, _, err = transcribeFile(opts, audio)
	if err == nil {
		t.Fatal("a failing engine must be an error")
	}
	if !strings.Contains(err.Error(), "engine failed") || !strings.Contains(err.Error(), "model map failed") {
		t.Errorf("error dropped the engine's stderr tail: %v", err)
	}
	if !strings.Contains(err.Error(), "line three") {
		t.Errorf("error should quote the LAST lines (lastLines), got: %v", err)
	}

	// An absent runtime is an error too — never a silent empty transcript.
	_, _, err = transcribeFile(&voiceOptions{engine: config.AudioEngineLocal, models: models, runtime: filepath.Join(t.TempDir(), "absent"), chunk: 15}, audio)
	if err == nil {
		t.Error("a missing engine must be an error")
	}
}

// TestWriteVoiceJSONShape pins the wire contract of the server's only response
// writer: the status, the JSON content type, and that the payload is emitted
// as an object (a client parses these keys by name).
func TestWriteVoiceJSONShape(t *testing.T) {
	rec := httptest.NewRecorder()
	writeVoiceJSON(rec, http.StatusTeapot, map[string]any{"text": "hi", "chunk": 15, "elapsed_ms": 3})

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q is not a JSON object: %v", rec.Body.String(), err)
	}
	if got["text"] != "hi" || got["chunk"] != float64(15) || got["elapsed_ms"] != float64(3) {
		t.Errorf("payload round-trip = %v", got)
	}
}

// TestDiscardFileCloses pins the cleanup helper's contract: it closes (so a
// caller can never double-close or leak the descriptor) and it is safe on an
// already-closed file — discardFile must not panic on the second call.
func TestDiscardFileCloses(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "voice-discard-*")
	if err != nil {
		t.Fatal(err)
	}
	discardFile(f)
	if _, err := f.WriteString("x"); err == nil {
		t.Error("discardFile did not close the file")
	}
	// Second call must be a no-op, not a panic (the double-cleanup path).
	discardFile(f)
}

// TestUserHomeDirOrDefaultFallsBack drives the fallback branch: with HOME
// unset the helper must return a non-empty absolute directory, because the
// voice model/runtime roots are joined onto it (an empty string would leave
// them as bare relative paths resolved against the CWD).
func TestUserHomeDirOrDefaultFallsBack(t *testing.T) {
	// Snapshot/restore HOME: os.UserHomeDir reads it, and SnapshotEnv keeps an
	// UNSET variable unset on the way out (t.Setenv cannot express absence).
	defer testutil.SnapshotEnv(t, "HOME")()

	// The ABSENT state is this test's subject: t.Setenv can only SET a variable
	// (LookupEnv then reports it present), so HOME is removed for real and the
	// deferred SnapshotEnv above puts the absence back on the way out.
	// env-leak-ok: absence is the subject — SnapshotEnv (os.LookupEnv) restores
	// HOME-to-UNSET, which t.Setenv cannot express; restoring via t.Setenv would
	// leave it set to "" and change what the fallback branch sees.
	if err := os.Unsetenv("HOME"); err != nil { // env-leak-ok: the SnapshotEnv defer above restores HOME on return, an UNSET value coming back UNSET (t.Setenv cannot express absence)
		t.Fatal(err)
	}
	got := userHomeDirOrDefault()
	if got == "" {
		t.Fatal("userHomeDirOrDefault returned empty with HOME unset — the voice roots would become relative")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("userHomeDirOrDefault = %q, want an absolute directory", got)
	}
}

// TestVoiceServeStdioInProcess covers voiceServeStdio's body directly (it
// returns an error rather than exiting), including the two failure branches
// that a subprocess cannot assert on cheaply: empty stdin and a failing
// engine. stdout is captured via the test binary's own os.Stdout swap.
func TestVoiceServeStdioInProcess(t *testing.T) {
	models := voiceWriteModels(t)
	opts := &voiceOptions{engine: config.AudioEngineLocal, models: models, runtime: os.Args[0], chunk: 15}
	t.Setenv(voiceStubModeEnv, "1")
	t.Setenv(voiceStubTextEnv, "stdio in-process")
	t.Setenv(voiceStubFailEnv, "")

	// A raw f32 PCM payload (not a container) → voiceMaterializeAudio wraps it.
	raw := make([]byte, 512)
	for i := range raw {
		raw[i] = byte(i % 251)
	}
	restore := voiceSwapStdin(t, raw)
	defer restore()

	out := captureVoiceStdout(t)
	if err := voiceServeStdio(opts); err != nil {
		t.Fatalf("voiceServeStdio: %v", err)
	}
	if got := strings.TrimSpace(out()); got != "stdio in-process" {
		t.Errorf("transcript on stdout = %q", got)
	}

	// Failure branch 1: an empty stdin must be an error, never a silent empty
	// transcript (which reads as "recognised silence").
	voiceSwapStdin(t, nil)
	if err := voiceServeStdio(opts); err == nil || !strings.Contains(err.Error(), "empty stdin") {
		t.Errorf("empty stdin error = %v, want an \"empty stdin\" error", err)
	}

	// Failure branch 2: the engine's failure is propagated with its stderr tail.
	voiceSwapStdin(t, raw)
	t.Setenv(voiceStubFailEnv, "1")
	if err := voiceServeStdio(opts); err == nil || !strings.Contains(err.Error(), "model map failed") {
		t.Errorf("engine failure error = %v, want the engine's stderr tail", err)
	}
}

// TestVoiceServeHTTPHandlerContract covers voiceServeHTTP's mux directly,
// through the exported handler it builds, so the routing/status contract is
// asserted without a listening socket.
//
// The handlers are closures registered on a private mux, so the only seam is
// serving them — this drives the REAL server bound to a loopback port and
// shuts it down via the request-completed signal, which also proves the
// server does not need the wildcard address to work.
func TestVoiceServeHTTPHandlerContract(t *testing.T) {
	models := voiceWriteModels(t)
	port := voiceFreePort(t)
	opts := &voiceOptions{engine: config.AudioEngineLocal, models: models, runtime: os.Args[0], chunk: 15, listen: voiceTestListenAddr(port)}
	t.Setenv(voiceStubModeEnv, "1")
	t.Setenv(voiceStubTextEnv, "http in-process")
	t.Setenv(voiceStubFailEnv, "")

	done := make(chan error, 1)
	// The server is stopped through its SHUTDOWN SEAM (voiceServeShutdownCh):
	// ListenAndServe-bound-to-a-port has no handle a caller can close, so
	// without this the goroutine below outlived the test and the suite's binary
	// stayed alive in net/http's epoll accept loop (measured 2026-09-25:
	// "goroutine 9495 [IO wait]: cmd/agent.voiceServeHTTP"). The channel is
	// armed for THIS case only and drained here, so the process-wide seam cannot
	// leak into a sibling test that expects the shipped path.
	seam := make(chan string)
	armVoiceServeSeam(seam)
	t.Cleanup(func() {
		close(seam)
		armVoiceServeSeam(nil)
		select {
		case err := <-done:
			// ErrServerClosed is this path's ordinary end (the seam shuts the
			// server down); anything else is a real refusal and is reported.
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("voiceServeHTTP returned %v after the shutdown seam closed — a stopped server reports ErrServerClosed", err)
			}
		case <-time.After(voiceServeShutdownTimeout):
			t.Errorf("voiceServeHTTP did not stop within %s of the shutdown seam closing — the server goroutine would outlive this test", voiceServeShutdownTimeout)
		}
	})
	go func() { done <- voiceServeHTTP(opts) }()

	base := "http://" + voiceTestListenAddr(port)
	// Every probe is BOUNDED (voiceProbeClient) and the deadline is evaluated
	// on a ticker — never only after a probe returns. The loop this replaced
	// checked its deadline AFTER `http.Get`, so a request that was accepted but
	// never answered (the timeout-less http.DefaultClient, a connection the
	// server holds open) parked the goroutine FOREVER: the suite's own 10-minute
	// panic took the package down and named this test as the last one running,
	// which reads as a hang instead of a failure.
	deadline := time.Now().Add(voiceServerReadyTimeout)
	for {
		resp, err := voiceProbeClient.Get(base + "/health")
		if err == nil {
			resp.Body.Close() //stylegate:ignore readiness-probe body; reaching the next line IS the answer
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("voiceServeHTTP never answered %s/health within %s (last probe error: %v)",
				base, voiceServerReadyTimeout, err)
		}
		time.Sleep(voiceServerPollInterval) //stylegate:allow-sleep poll interval of the readiness wait; it exits on the first successful health response
	}

	// The health payload is the readiness contract.
	resp, err := voiceProbeClient.Get(base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close() //stylegate:ignore body of the already-decoded /health response; the decoded map is the assertion
	if health["ok"] != true || health["engine"] != voiceEngineLabelLocal {
		t.Errorf("/health = %v", health)
	}

	// Contract: POST-only, and the multipart field name is "audio".
	if code, body := voiceRawTranscribe(t, http.MethodGet, base, "clip.wav", []byte("x")); code != http.StatusMethodNotAllowed {
		t.Errorf("GET /transcribe = %d (%s)", code, body)
	}
	got := voicePostTranscribe(t, base, "clip.wav", []byte("RIFF????WAVEin-process"))
	if got["text"] != "http in-process" {
		t.Errorf("text = %v", got["text"])
	}
	select {
	case err := <-done:
		t.Fatalf("voiceServeHTTP returned early: %v", err)
	default:
	}
}
