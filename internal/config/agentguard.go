package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/boolvocab"
	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/procname"
)

// agentguard.go — the SIGNAL + GUARD behind the operator's 2026-09-12 ruling:
// hermes/PA (an LLM agent session) must never start `gopi worker` or
// `gopi task run`, because both are effectively never-returning processes: the
// agent's tool call blocks on the child forever, the agent goes unresponsive,
// and the operator cannot even exit hermes (04:12 incident — killing hermes
// SIGTERMed 6 co-process-group workers and left 8 rows in_progress for 50 min).
//
// THE RULING (operator, verbatim): 「以后禁止 hermes 运行任务，我手动运行一个
// worker，你 PA 只能提交任务」— and on the escape hatch he had already ruled
// out: 「要保证它不是你的子进程这个也是无解，干脆就不允许你运行 worker」. The
// mechanism therefore cannot be "detach the child" (setsid only leaves the
// SESSION, not the process tree/cgroup — proven by the 04:12 cohort kill); it
// has to be "refuse to start at all".
//
// WHAT MAKES THIS MECHANICAL (and why it is not a prompt): the refusal lives in
// the two verbs themselves, so it fires for ANY agent — hermes, a shell script
// the agent wrote, a wrapper, a `bash -c`, a background job. An agent cannot
// talk its way past a process that exits.
//
// THE SIGNALS — researched on the live host (2026-09-12), not assumed:
//
//	① GOPI_EXECUTOR_SESSION_ID — exported by cmd/agent/main.go at startup for
//	   every gopi AGENT process, and inherited by every process the agent then
//	   spawns (its bash tool, and whatever that shell starts). MEASURED on the
//	   live host: the agent's bash carried it, the operator's own terminal
//	   (`-bash` on pts/0, the shell hermes itself runs under) did NOT.
//	② HERMES_SESSION_ID — exported by the hermes process itself and inherited
//	   by its children (its terminal tool included). Same measurement: agent
//	   yes, operator terminal no.
//	③ GOPI_AGENT_SESSION=<pid> — the ANCHOR MARKER: exported by cmd/agent for a
//	   gopi agent process, carrying the EXPORTER'S PID. The pid is what turns
//	   "this variable exists" into "this variable is MINE": a process that
//	   exported it sees its own pid, while a process that merely inherited it
//	   sees the agent's pid and knows an agent sits above it.
//	④ GOPI_HUMAN_TERMINAL — the operator's negative signature (see below).
//	⑤ the PROCESS CHAIN — our immediate parent's argv (procfs), which is NOT
//	   inheritable and therefore exact where the environment is ambiguous.
//
// ③ and ⑤ are the anchoring halves, and they are what make the guard survive
// the two obvious counterexamples:
//
//	(a) the operator runs `gopi worker` from a shell he opened INSIDE hermes.
//	    The environment there really IS inherited from the agent process (① and
//	    ② are present), so "was my env inherited from an agent" is NOT the
//	    question. The question gopi can answer exactly is "am I the agent, or
//	    under one": the anchor's pid equals os.Getpid() for the agent session
//	    itself (⇒ a human's launch ⇒ ALLOW) and some other pid for anything it
//	    spawned (⇒ REFUSE). cmd/agent also CLEARS the anchor in its `task run`
//	    child's environment, so the execution child never has to rely on the
//	    comparison.
//	(b) the worker→`task run` chain. The worker is a direct parent whose argv
//	    says `worker`, so ParentIsWorker exempts the sanctioned execution path —
//	    a procfs read, immune to whatever the environment carried down.
//
// HONEST LIMIT (devdoc/pa-never-runs-tasks-20260912.md §honest limits): signals
// travel with the ENVIRONMENT, so the guard stops every accidental and scripted
// path an agent takes, but an agent that deliberately deletes the signature
// (`env -u GOPI_EXECUTOR_SESSION_ID gopi worker`) defeats it. No prompt fixes
// that either — it is a deliberate rule violation, and its detection is the
// worker count + the log, not a refusal.

// The environment signatures the guard reads (Rule 1b/14: one definition site,
// referenced by every reader — cmd/agent's hook, the main.go writer, the tests
// and the documented operator escape).
const (
	// AgentSessionEnv is exported by cmd/agent for every gopi AGENT process and
	// is inherited by everything it spawns. tools.TaskExecutorSessionEnv spells
	// the same name for the writeback reader; the value has ONE home here (a
	// cmd/agent main-package constant cannot be imported by internal/config).
	AgentSessionEnv = "GOPI_EXECUTOR_SESSION_ID"
	// HermesSessionIDEnv is hermes' own session id, exported by the hermes
	// process and inherited by its children. A second, FOREIGN signature means
	// the guard does not depend on gopi's own export having happened (a bare
	// hermes runs `gopi worker` from its terminal tool with no gopi ancestor).
	HermesSessionIDEnv = "HERMES_SESSION_ID"
	// AgentSessionAncestorEnv is the ANCHOR MARKER: its VALUE is the exporting
	// gopi agent process's pid (see the package comment).
	AgentSessionAncestorEnv = "GOPI_AGENT_SESSION"
	// HumanTerminalEnv is the operator's negative signature: a shell carrying it
	// is the operator's own, so the guard steps aside without any override. It
	// must NOT be exported from a shell profile: hermes is a CHILD of the
	// operator's shell and would inherit the declaration, which would then speak
	// for every agent shell under hermes — the exact process the guard refuses.
	// Correct use is per-invocation on his own command line
	// (`GOPI_HUMAN_TERMINAL=1 gopi-worker-up.sh 2`); the agent cannot forge it,
	// because the agent's shell inherits the agent's environment, not his.
	HumanTerminalEnv = "GOPI_HUMAN_TERMINAL"
	// AgentSpawnGuardOverrideEnv is the explicit per-run override for a HUMAN
	// who really means it. The agent has no legitimate use for it, and the
	// devdoc says so in as many words.
	AgentSpawnGuardOverrideEnv = "GOPI_ALLOW_AGENT_SPAWN"
	// AgentSpawnGuardOffEnv disables the guard wholesale. Two legitimate uses: a
	// subprocess test harness that IS the operator's actor (it drives the real
	// binary directly and would otherwise inherit a signature), and a bootstrap
	// whose parent chain happens to carry one. Never a production default.
	AgentSpawnGuardOffEnv = "GOPI_AGENT_SPAWN_GUARD_OFF"
	// AgentSpawnGuardExitCodeLabel is the wording that announces the refusal's
	// exit code in the guard's own output. It is a CONSTANT because the worker
	// help page documents the same code in its own words, and a rule test
	// asserts the two agree: the label is the ONE place the refusal names its
	// code (Rule 1b — a spelling that must not drift lives in a constant).
	AgentSpawnGuardExitCodeLabel = "Exit code: "
	// AgentSpawnGuardHelpAnchor is the anchor the refusal uses to tell the
	// reader WHERE the code is documented (`gopi <verb> -h`). The rule test
	// asserts this anchor also occurs on the worker help page, so the pointer
	// cannot dangle.
	AgentSpawnGuardHelpAnchor = "-h"
	// AgentSpawnGuardLockName is the single-instance lock file a launched
	// worker would take, named here so the refusal can say that the refusal
	// took NOTHING (a caller may check that no lock was created). It MIRRORS
	// cmd/agent's workerLockFilename for the same reason DaemonEnvLeakVars
	// mirrors tools.DaemonContextEnvVars: internal/config cannot import
	// cmd/agent. TestDaemonEnvLeakVarsMirrorTools' sibling in cmd/agent
	// (TestGuardRefusalNamesTheWorkerLockFile) compares the two name-for-name,
	// so a drift is a failing test instead of a silent lie.
	AgentSpawnGuardLockName = "worker.lock"
)

// DaemonEnvLeakVars are the execution-context env names a spawn seam must
// strip from an executor child's environment, so each task a resident daemon
// executes starts with ITS OWN context instead of the daemon's launch
// context (daemon-context-isolation epic, 2026-09-13).
//
// This is a MIRROR of tools.DaemonContextEnvVars, and a mirror is the exact
// defect Rule 14 names — it exists only because the import cannot run the
// other way: internal/tools imports internal/config, so config cannot
// reference the tools list. The single source of truth stays
// tools.DaemonContextEnvVars (the STRIP uses it); this copy exists so
// cmd/agent can VERIFY the strip against both, and a drift is a failing test
// rather than a silent hole (TestDaemonEnvLeakVarsMirrorTools in package
// main compares the two lists name-for-name).
var DaemonEnvLeakVars = []string{
	AgentSessionEnv,
	AgentSessionAncestorEnv,
}

// AgentGuardRefusalExitCode is the exit code of a refused spawn. It is NOT 1
// (the generic error) and not 2 (usage): a refusal is its own outcome, so a
// caller — and the acceptance test — can tell "the guard said no" apart from
// "the verb broke". Rule 1: the code lives here, one definition site.
const AgentGuardRefusalExitCode = 3

// procFSRoot is the procfs mount the process-chain read uses (Rule 1: one
// definition site; Linux-only, which is the only platform this guard needs).
// It is a VARIABLE only so a test can point the chain read at a fixture; the
// value is never written in production (nothing assigns it outside this file's
// test), and it defaults to the one shared constant.
var procFSRoot = procname.ProcRootDefault

// envTruthy reports whether an environment value is present and says "yes"
// (the canonical truthy set boolvocab.TrueValues: 1/true/yes/on/enabled,
// case-insensitive) — read from the ONE definition instead of re-spelling a
// narrower copy of it here (Rule 13/14). An empty or whitespace value is ABSENT
// — the convention the repo's other boolean knobs already use, so
// `GOPI_HUMAN_TERMINAL=` never counts as a claim.
func envTruthy(raw string) bool {
	return boolvocab.IsTrue(raw)
}

// AgentSpawnSignature is the set of facts the guard decides on, captured once
// so the decision function is pure and testable without a live environment.
type AgentSpawnSignature struct {
	// AgentSession: the agent-session env is present (AgentSessionEnv).
	AgentSession bool
	// HermesSession: hermes' session env is present (HermesSessionIDEnv).
	HermesSession bool
	// AnchorSelf: the anchor marker is present AND carries THIS process's pid —
	// i.e. this process is the agent session that exported it.
	AnchorSelf bool
	// AnchorInherited: the anchor marker is present with ANOTHER process's pid —
	// i.e. an agent sits above us in the process chain.
	AnchorInherited bool
	// HumanTerminal: the operator's escape is claimed (HumanTerminalEnv).
	HumanTerminal bool
	// ParentIsWorker: our IMMEDIATE parent is a live `gopi worker` process — the
	// one sanctioned task executor, so the worker→`task run` chain is a human's
	// path even when the worker's own environment looks agent-ish (the operator
	// launched the launcher from a terminal he opened inside hermes). Read from
	// procfs, never from the environment, so inheritance cannot confuse it.
	ParentIsWorker bool
}

// AgentRun reports whether this process was started from inside an agent
// session — the ONE question the guard asks.
func (s AgentSpawnSignature) AgentRun() bool {
	if s.HumanTerminal {
		return false
	}
	if s.ParentIsWorker {
		// A worker is the operator's own task executor; its `task run` child is
		// the sanctioned execution path.
		return false
	}
	if s.AnchorSelf {
		// We are the agent session itself (or a runner that re-executed one):
		// the signature belongs to THIS process, not to an agent above it.
		return false
	}
	if s.AnchorInherited {
		return true
	}
	// No anchor (a foreign harness, or a gopi that predates the anchor): an
	// agent signature in our environment then has no self-claim to explain it,
	// so it means an agent spawned us — through hermes' own terminal, or
	// through an older gopi agent's shell.
	return s.HermesSession || s.AgentSession
}

// String renders the signature for the refusal message: which signal fired, so
// an operator reading the log knows WHY gopi refused (a bare "refused" in a log
// is the silent-failure trap Rule 15 warns about).
func (s AgentSpawnSignature) String() string {
	var hits []string
	if s.ParentIsWorker {
		// The exempting fact must be NAMED here too: String renders the
		// signature the refusal quotes, and "no signature present" for a
		// worker's own task-run child would read as a guard that has no idea
		// why it stepped aside (the sanctioned chain, parentIsWorkerGopi).
		hits = append(hits, "parent is a "+procname.NameGopi+" "+procname.VerbWorker)
	}
	if s.AnchorInherited {
		hits = append(hits, AgentSessionAncestorEnv+"(inherited)")
	}
	if s.AgentSession {
		hits = append(hits, AgentSessionEnv)
	}
	if s.HermesSession {
		hits = append(hits, HermesSessionIDEnv)
	}
	if len(hits) == 0 {
		return "no signature present"
	}
	return strings.Join(hits, ", ")
}

// AgentSpawnSignatureNow captures the signature from the process environment
// and the process chain. The anchor's pid is compared against this process's
// own pid — the self-vs-inherited discriminator (see the package comment).
func AgentSpawnSignatureNow() AgentSpawnSignature {
	sig := AgentSpawnSignature{
		AgentSession:   strings.TrimSpace(os.Getenv(AgentSessionEnv)) != "",
		HermesSession:  strings.TrimSpace(os.Getenv(HermesSessionIDEnv)) != "",
		HumanTerminal:  envTruthy(os.Getenv(HumanTerminalEnv)),
		ParentIsWorker: parentIsWorkerGopi(),
	}
	if raw := strings.TrimSpace(os.Getenv(AgentSessionAncestorEnv)); raw != "" {
		if pid, err := strconv.Atoi(raw); err == nil && pid > 0 {
			if pid == os.Getpid() {
				sig.AnchorSelf = true
			} else {
				sig.AnchorInherited = true
			}
		} else {
			// A marker without a usable pid (a hand-written export, or a value
			// from an older release): it still claims an agent chain but cannot
			// claim to be US — the conservative reading is "inherited".
			sig.AnchorInherited = true
		}
	}
	return sig
}

// parentIsWorkerGopi reports whether our IMMEDIATE parent runs the gopi PROGRAM
// with argv[1] == procname.VerbWorker — the shared process vocabulary, Rule 14:
// never a mirrored literal.
//
// WHY procfs and not the environment: the worker→`task run` chain is the
// sanctioned execution path, and a worker may itself have been started by the
// operator from a shell that carries agent-ish variables (a terminal opened
// inside hermes). Environment inheritance cannot separate "an agent started
// this chain" from "a human started it there", but the process chain can: the
// worker is a direct parent and it is the operator's own tool.
//
// THE PROGRAM IS READ FROM THE PARENT'S IMAGE, NOT FROM ITS comm (bug
// 01a131fcb8337c1b, measured 2026-10-13): comm is the executable FILE's
// basename, so a gopi binary built as bin/gopi-fix ran a worker whose parent
// this function refused to recognize — the guard then treated the sanctioned
// chain as an unsanctioned one. procname.IsGopiImage owns the judgement
// (/proc/<ppid>/exe, then the parent's own argv[0]); this function only supplies
// the channels.
//
// Fail-open on every unreadable case (no procfs, a parent already reaped): an
// unreadable /proc must never invent a refusal.
func parentIsWorkerGopi() bool {
	// /proc/self/stat field 4 is the parent pid, but comm (field 2) may contain
	// arbitrary bytes including spaces and parens — the field is read through
	// internal/procname.StatField, the ONE implementation of the
	// count-from-the-last-')' rule (Rules 13/14).
	ppidRaw, ok := procname.StatFieldString(procFSRoot+"/self/"+procname.StatName, procname.StatFieldParent)
	if !ok {
		return false
	}
	ppid, err := strconv.Atoi(ppidRaw)
	if err != nil || ppid <= 0 {
		return false
	}
	pid := strconv.Itoa(ppid)
	argv, ok := procname.Cmdline(procFSRoot, pid)
	if !ok || len(argv) < 2 || argv[1] != procname.VerbWorker {
		return false
	}
	// The parent's comm is the THIRD channel and the weakest one: it is handed
	// to the shared judgement only as a fallback, so an unreadable exe link with
	// an empty cmdline still has evidence — and an unreadable one with no comm
	// is a missing measurement, not a licence to refuse (Rule 15a).
	comm, _ := os.ReadFile(fmt.Sprintf("%s/%s/%s", procFSRoot, pid, procname.CommName)) //stylegate:ignore best-effort fallback: per the note above, an unreadable comm is a missing measurement, not a licence to refuse
	isGopi, _ := procname.IsGopiImage(procFSRoot, pid, argv[0], string(comm))
	return isGopi
}

// AgentSpawnDecision is the guard's verdict for one verb.
type AgentSpawnDecision struct {
	// Allowed reports whether the verb may start.
	Allowed bool
	// Reason is why it was refused (empty when allowed).
	Reason string
	// Override reports that the process was allowed ONLY because an explicit
	// override env was set — the caller prints a warning for that case, so an
	// override is never silent (Rule 15: no silent failure).
	Override bool
}

// GuardAgentSpawn decides whether `verb` may start in this process. It is the
// single decision function — the production call sites and the tests both call
// it, so the tested predicate IS the production predicate.
//
// verb is the verb word for the message only (cmd/agent owns the authoritative
// spellings; package main cannot be imported here).
func GuardAgentSpawn(verb string, sig AgentSpawnSignature) AgentSpawnDecision {
	if envTruthy(os.Getenv(AgentSpawnGuardOffEnv)) {
		return AgentSpawnDecision{Allowed: true, Override: true}
	}
	if !sig.AgentRun() {
		return AgentSpawnDecision{Allowed: true}
	}
	if envTruthy(os.Getenv(AgentSpawnGuardOverrideEnv)) {
		return AgentSpawnDecision{Allowed: true, Override: true}
	}
	return AgentSpawnDecision{
		Allowed: false,
		Reason: fmt.Sprintf(
			"refusing to start: this process is inside an agent session (%s).\n"+
				"  `gopi %s` is a never-returning process — waiting on it stalls the agent\n"+
				"  and it cannot be detached (setsid leaves the process tree/cgroup intact).\n"+
				"  The operator runs workers himself:   ~/bin/gopi-worker-up.sh N\n"+
				"    (a launcher run from an agent's shell is refused exactly like the bare verb —\n"+
				"     the guard judges the LAUNCHER's environment; only clearing that environment\n"+
				"     gets past it, and that is a deliberate rule violation, not an accident)\n"+
				"  To hand work to the queue instead:   gopi task submit \"<summary>\" --payload \"<brief>\"\n"+
				"  Operator escape (HIS shell only — never ~/.bashrc: hermes would inherit it):\n"+
				"                                       %s=1 gopi %s\n"+
				"  Deliberate one-off override:         %s=1 gopi %s\n"+
				"  %s%d — the Exit-codes line of `gopi %s %s` documents it beside 0, 1\n"+
				"  and 2, and this refusal started nothing and took no %s.",
			sig.String(), verb, HumanTerminalEnv, verb, AgentSpawnGuardOverrideEnv, verb,
			AgentSpawnGuardExitCodeLabel, AgentGuardRefusalExitCode, verb, AgentSpawnGuardHelpAnchor,
			AgentSpawnGuardLockName),
	}
}

// RefuseBarePrompt prints a bare-prompt refusal to stderr and exits with the
// same dedicated code, so a caller can tell "the guard said no" apart from "the
// run failed". It is a separate function from RefuseAgentSpawn because this
// surface has no verb word to prefix with (a bare word IS not a verb — that is
// the whole defect), so the line starts with the program word alone.
func RefuseBarePrompt(d AgentSpawnDecision) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", cliverbs.Program, d.Reason)
	os.Exit(AgentGuardRefusalExitCode)
}

// RefuseAgentSpawn prints the refusal to stderr and exits with
// AgentGuardRefusalExitCode. cmd/agent's two guarded verbs call it.
func RefuseAgentSpawn(verb string, d AgentSpawnDecision) {
	fmt.Fprintf(os.Stderr, "gopi %s: %s\n", verb, d.Reason)
	os.Exit(AgentGuardRefusalExitCode)
}

// ---------------------------------------------------------------------------
// THE BARE-PROMPT FACE — the third guarded surface
// ---------------------------------------------------------------------------
//
// The two verbs above are never-returning, and that is why the operator's
// 2026-09-12 ruling refuses them from an agent. The bare-prompt face is a
// DIFFERENT defect with the same root — an agent that launches it does not
// stall, it MULTIPLIES:
//
//	agent → (bash) → gopi <word> → a REAL paid agent run → agent → …
//
// MEASURED on this host (bug row 01a1ac14, 2026-11-05): a worker's agent ran
// `gopi submit-x` ("submit-x" is no verb and no near-miss, so the dispatch chain
// falls through to the bare-prompt face), which started a full paid run that
// registered an `[auto] submit-x` ledger row and created a clone ground under
// the SHARED /home/nick/workspace; two such cascaded ghosts cost 440,153 + 855,807
// prompt tokens, and the ledger already carries 900 `[auto]` rows. Each layer
// "investigated" the bare word by running it again — self-replication, with no
// depth limit at all.
//
// WHAT THE REFUSAL IS *NOT*: it does not forbid a person from typing a prompt.
// `gopi "summarize this file"` from a terminal is the documented usage and must
// keep working — GoPi's own tests drive it (testutil.MockEnv's bare-prompt
// children carry HumanTerminalEnv, and AgentRun() consults that before
// anything else). The refusal fires only when `sig.AgentRun()` is TRUE, i.e.
// when this process is inside an agent session and nothing anchors the marker
// to us.
//
// THE ANCHOR IS WHAT MAKES IT SAFE FOR THE AGENT ITSELF: a gopi AGENT process
// re-executed for its own prompt sees AnchorSelf (the exported pid is its own),
// so AgentRun() is false and its own run is never refused. Only a CHILD of an
// agent — the shape that recurses — sees AnchorInherited.

// BarePromptFace is the name the refusal quotes for this surface. It is a
// CONSTANT because the message, the help pointer and the acceptance test all
// spell it (Rule 1/13/14: one definition site): a bare word is not a verb, so
// there is no verb word to name the refusal after.
const BarePromptFace = "the bare-prompt fallback"

// The three addresses the refusal offers, and the ledger marker it quotes. They
// are constants for the same reason (Rules 1/13/14): the message is the ONLY
// place an agent learns the correct entry point, so a spelling that drifts here
// sends it back to the surface that just refused it.
const (
	// AutoTaskRowPrefix is the ledger summary marker the bare-prompt face
	// writes (the `[auto] <word>` row an unattended run registers). The
	// refusal quotes it so the reader can find the rows this defect produced.
	AutoTaskRowPrefix = "[auto] "
	// BarePromptTaskSubmitAddress is the queue entry point — the agent's ONLY
	// legitimate way to start work it does not run itself.
	BarePromptTaskSubmitAddress = cliverbs.Program + " " + cliverbs.Task + " submit \"<summary>\" --payload \"<brief>\""
	// BarePromptChatAddress is the conversation entry point: the surface an
	// agent uses when what it wants is a turn, not a dispatched row.
	BarePromptChatAddress = cliverbs.Program + " " + cliverbs.Chat
	// BarePromptVerbHelpAddress is the usage entry point, with the anchor
	// spelled from the constant the worker refusal already uses.
	BarePromptVerbHelpAddress = cliverbs.Program + " <verb> " + AgentSpawnGuardHelpAnchor
)

// BarePromptWord returns the positional that would become this run's task text
// on the bare-prompt face — i.e. the word the dispatch chain could not resolve
// — or "" when no such word governs THIS invocation.
//
// It is the SCREEN, and GuardBarePrompt is the DECISION; splitting them keeps
// the guard from having to re-decide what a prompt is (Rule 2: the resolution
// lives in one place, promptInForce). The three "no word" answers:
//
//   - the invocation carries an explicit prompt channel (`--prompt X`,
//     settings' "prompt", a NON-AMBIENT GOPI_PROMPT): the positionals are then
//     input FILES, exactly as the verb guard above already reads them, and a
//     file named `submit-x` is a file, not an unresolved verb;
//   - there are no positionals at all (stdin-only run, `--prompt` alone);
//   - cfg is nil (the caller's resolution failed and is being reported
//     elsewhere).
func BarePromptWord(cfg *Config) string {
	if cfg == nil || promptInForce(cfg) {
		return ""
	}
	if len(cfg.Positionals) == 0 {
		return ""
	}
	return cfg.Positionals[0]
}

// GuardBarePrompt decides whether this process may start a REAL agent run from
// the bare-prompt face. It is the single decision function — the production
// call site (cmd/agent/main.go) and the regression test both call it, so the
// tested predicate IS the production predicate (the same contract as
// GuardAgentSpawn).
//
// firstPositional is the word the dispatch chain could not resolve. It is used
// for the message only: the caller has already decided that no verb matched,
// and an explicit prompt (`--prompt …`, GOPI_PROMPT) never reaches this gate.
func GuardBarePrompt(firstPositional string, sig AgentSpawnSignature) AgentSpawnDecision {
	// THE SIGNATURE IS ASKED FIRST, and the order is deliberate: the bare-prompt
	// face serves every human invocation of `gopi "<prompt>"`, so an override
	// warning printed before this question would fire on runs the guard never
	// had an opinion about (`GOPI_AGENT_SPAWN_GUARD_OFF=1` in a developer's
	// shell would print it on every interactive run). The two verbs above run
	// rarely enough that their order costs nothing this one would cost daily.
	if !sig.AgentRun() {
		return AgentSpawnDecision{Allowed: true}
	}
	if envTruthy(os.Getenv(AgentSpawnGuardOffEnv)) {
		return AgentSpawnDecision{Allowed: true, Override: true}
	}
	if envTruthy(os.Getenv(AgentSpawnGuardOverrideEnv)) {
		return AgentSpawnDecision{Allowed: true, Override: true}
	}
	return AgentSpawnDecision{
		Allowed: false,
		Reason: fmt.Sprintf(
			"refusing to start: this process is inside an agent session (%s),\n"+
				"  and %q is not a gopi verb — it would fall through to %s,\n"+
				"  which starts a REAL agent run: a new session, a ledger row marked %q\n"+
				"  and a clone ground, all paid for, and none of that is undone by killing it.\n"+
				"  A run started this way is also how an agent recurses (agent → gopi <word> → agent)\n"+
				"  with no depth limit — every layer 'investigates' the word by running it again.\n"+
				"  The right entry point depends on what you meant:\n"+
				"    hand work to the queue:   %s\n"+
				"    continue a conversation:  %s\n"+
				"    ask what a verb does:     %s\n"+
				"  A person typing a prompt in a terminal is unaffected (this refusal needs the\n"+
				"  agent signature); %s=1 clears it deliberately, and %s=1 disables the guard.\n"+
				"  %s%d — this refusal started nothing: no session, no ledger row, no ground.",
			sig.String(), firstPositional, BarePromptFace, AutoTaskRowPrefix+firstPositional,
			BarePromptTaskSubmitAddress, BarePromptChatAddress,
			BarePromptVerbHelpAddress,
			HumanTerminalEnv, AgentSpawnGuardOffEnv,
			AgentSpawnGuardExitCodeLabel, AgentGuardRefusalExitCode),
	}
}
