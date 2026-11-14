// cli_memory_help_surface_basic_test.go — G3 of the help-surface hardening:
// every `gopi memory` verb/flag the family help ADVERTISES gets a BASIC test
// that RUNS it the way the help says to run it — a happy path plus the verb's
// first refusal.
//
// WHY THIS FILE IS SHAPED THIS WAY. The G1 sibling (`gopi task`) exists because
// `gopi task sync --push-only --task-id T` was implemented, compiled,
// unit-tested, reported complete, and had NEVER been typed (PROJECT.md Rule 31).
// A suite that enters through a seam the author chose cannot see that class, so
// every case below drives the REAL BINARY over the help's own argv, with a
// sandboxed GOPI_HOME and a per-test ledger, and asserts on the help's CLAIM.
//
// THE SURFACE LIST IS DERIVED, NOT AUTHORED: the table in
// devdoc/gopi-memory-help-surface-inventory.md is generated from `gopi memory -h`
// by scripts/g3-memory-help-surface.sh, and TestMemoryHelpSurfaceMatchesInventory
// re-derives the same list here from the usage constant the binary prints. A
// verb added to the help without a row (and without a case) is the defect.
//
// REUSED FIXTURES (both pre-existing, deliberately not reinvented):
//   - the binary: gopiTestBinPath (gopibin_test.go) — ONE build per suite;
//   - the env:    cliContractEnv (cli_contracts_test.go) — the scrubbed
//     GOPI_*/ANTHROPIC_*/DEEPSEEK_* environment, which is what keeps
//     a probe away from the operator's real profile (and its
//     GOPI_BREW_INTERVAL_SEC=0 is what stops a probe's child from
//     firing a brew sweep).
//
// Rule 17: in-process env mutation goes through t.Setenv. Rule 22/32: every
// workspace is a t.TempDir(); no probe names a temp root itself.
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nickhuang99/gopi/internal/capturesurvey"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// g3MemoryVerbTimeout bounds ONE probe's child (Rule 27: a probe that can park
// the package's budget carries its own ceiling). A correctly routed CLI handler
// never reaches the agent loop, so a hang is a routing defect, not a slow test.
const g3MemoryVerbTimeout = 25 // seconds

// g3SimPeer / g3SimNode are the DECLARED topology the sync probes use: a peer
// alias the whitelist accepts (so the whitelist is exercised as a whitelist and
// not only as a refusal) and a node name that makes the serve leg's reply
// attributable to this process rather than to the hostname.
const (
	g3SimPeer = "g3-probe-peer"
	g3SimNode = "g3-probe-node"
)

// --- the surface list, re-derived from the help -------------------------------

// g3AdvertisedVerbs returns every verb the family help advertises a synopsis
// head for, read from the usage constant the binary prints (memoryUsage) — the
// SAME text `gopi memory -h` emits, so no probe is needed to enumerate it.
//
// DEDUP IS LOAD-BEARING: `provenance` prints TWO heads and `feedback` FOUR (the
// mode flags do not fit on one line), so the raw match list carries duplicates.
func g3AdvertisedVerbs() []string {
	re := regexp.MustCompile(`(?m)^  gopi memory ([a-z][a-z-]*)\b`)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(memoryUsage, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// g3AdvertisedFlags returns the flags spelled in EVERY synopsis of one verb,
// continuation lines included — a long synopsis WRAPS, and two of them wrap
// twice.
//
// The scanner mirrors scripts/g3-memory-help-surface.sh line for line, because
// the doc and the test must not be able to disagree: a head sets the current
// verb; a line indented to the bracket column (>=18 spaces) that opens a group
// (`[`) or continues one (`|`) belongs to it; every other line is prose and is
// neither collected nor a terminator — which is what lets a multi-head verb
// accumulate across its own description block.
func g3AdvertisedFlags(verb string) []string {
	lines := strings.Split(memoryUsage, "\n")
	head := regexp.MustCompile(`^  gopi memory ([a-z][a-z-]*)\b`)
	cur := ""
	buf := []string{}
	for _, ln := range lines {
		if m := head.FindStringSubmatch(ln); m != nil {
			cur = m[1]
			if cur == verb {
				buf = append(buf, ln)
			}
			continue
		}
		if cur != verb {
			continue
		}
		if (strings.HasPrefix(ln, "                  ") && strings.HasPrefix(strings.TrimLeft(ln, " "), "[")) ||
			(strings.HasPrefix(ln, "                  ") && strings.HasPrefix(strings.TrimLeft(ln, " "), "|")) {
			buf = append(buf, ln)
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, ln := range buf {
		for _, m := range regexp.MustCompile(`--[a-z][a-z-]*`).FindAllString(ln, -1) {
			f := strings.TrimPrefix(m, "--")
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	sort.Strings(out)
	return out
}

// g3ExpectedAdvertisedVerbs is the surface this file was written against — the
// briefing's ten subcommands. The gate compares the DERIVED list against it AND
// against g3CoveredVerbs, so neither a help edit nor a test deletion can go
// unnoticed.
//
//nolint:gochecknoglobals // the pinned expectation, read by the gate
var g3ExpectedAdvertisedVerbs = []string{
	"search", "store", "get", "list", "update", "delete", "feedback",
	memoryBackfillVerb,
	CliVerbBrew, tools.LedgerBackupVerb, memoryProvenanceVerb,
	memoryCaptureStatsVerb, memoryConvergeKeywordsVerb,
	// `sync` came back on 2026-11-06 as a MANUAL door (one invocation = one
	// session, exit) after 5677cc6e deleted the whole cross-node sync CLI
	// family. It is covered by cli_memory_sync_test.go: the three direction
	// shapes, the both-flags refusal, the whitelist refusal that names this
	// node's peers, and the page/parser agreement.
	tools.MemorySyncVerb,
}

// g3CoveredVerbs is the inventory's COVERED set: a verb with a basic test in
// this file. A verb absent here must be listed in the inventory with a reason.
//
//nolint:gochecknoglobals // the inventory's declared answer, read by the gate
var g3CoveredVerbs = []string{
	"search", "store", "get", "list", "update", "delete", "feedback",
	memoryBackfillVerb,
	CliVerbBrew, tools.LedgerBackupVerb, memoryProvenanceVerb,
	// capture-stats is covered HERE (TestMemoryVerbCaptureStatsRunsAsAdvertised)
	// rather than by a doc row alone: its refusals are the interesting half, and
	// a read-only survey that quietly measured the wrong corpus would still
	// print a table.
	memoryCaptureStatsVerb,
	// converge-keywords arrived in e983a78 with its test
	// (TestMemoryVerbConvergeKeywordsRunsAsAdvertised) and its inventory row,
	// but WITHOUT being named in either list here — so the gate this file owns
	// was red on master, reading the live verb as an unaccounted surface.
	memoryConvergeKeywordsVerb,
	// covered by cli_memory_sync_test.go (see the note in the expectation list
	// above) — the verb's decisions are CLI-level, so the basic test lives
	// there rather than in this file.
	tools.MemorySyncVerb,
}

// TestMemoryHelpSurfaceMatchesInventory is the re-derivation gate: the help's
// synopsis set, the pinned expectation and the claimed coverage must agree.
func TestMemoryHelpSurfaceMatchesInventory(t *testing.T) {
	got := g3AdvertisedVerbs()
	if len(got) == 0 {
		t.Fatal("the derivation found no advertised verb — the scanner is reading blind, so a green run would prove nothing")
	}
	if diff := g3StringDiff(g3ExpectedAdvertisedVerbs, got); diff != "" {
		t.Errorf("the family help's advertised verbs changed:\n%s\n"+
			"Every advertised verb needs a test in this file or a row in "+
			"devdoc/gopi-memory-help-surface-inventory.md (with a reason), and this "+
			"expectation updated in the same change.", diff)
	}
	if diff := g3StringDiff(g3CoveredVerbs, got); diff != "" {
		t.Errorf("coverage and the advertised surface disagree:\n%s\n"+
			"An advertised verb with no test here is the defect G3 exists to "+
			"remove; a covered verb the help does not advertise is a claim about "+
			"a surface that no longer exists.", diff)
	}
	for _, v := range got {
		if len(g3AdvertisedFlags(v)) == 0 {
			t.Errorf("verb %q advertises no flags at all — either the synopsis scanner is broken or the verb's line is (an advertised verb with no flag spelling is legal only if the help really shows none)", v)
		}
	}
}

// g3InventoryRowRe reads one row of the checked-in inventory table
// (devdoc/gopi-memory-help-surface-inventory.md): `| 3 | \`for-branch\` | …`.
var g3InventoryRowRe = regexp.MustCompile("(?m)^\\| *[0-9]+ *\\| *`([a-z][a-z-]*)` *\\|")

// TestMemoryHelpSurfaceInventoryTableMatchesHelp closes the loop the briefing
// asks for by name — "the inventory table is reproducible from the quoted
// help-derived command". A table that lists a verb the help no longer advertises
// (or omits one it does) describes last month's binary.
func TestMemoryHelpSurfaceInventoryTableMatchesHelp(t *testing.T) {
	rows := g3InventoryRowRe.FindAllStringSubmatch(g3InventoryDoc(t), -1)
	var verbs []string
	for _, m := range rows {
		verbs = append(verbs, m[1])
	}
	sort.Strings(verbs)
	want := g3AdvertisedVerbs()
	if diff := g3StringDiff(want, verbs); diff != "" {
		t.Errorf("devdoc/gopi-memory-help-surface-inventory.md and the help disagree:\n%s\n"+
			"Every advertised verb needs a table ROW (the table is the deliverable); "+
			"a row for a verb the help does not advertise is a stale claim.", diff)
	}
	if len(rows) != len(want) {
		t.Errorf("the inventory table has %d verb rows, the help advertises %d — "+
			"the table must carry exactly one row per advertised verb", len(rows), len(want))
	}
}

// g3InventoryDoc reads the checked-in inventory table from the repo root.
func g3InventoryDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "devdoc", "gopi-memory-help-surface-inventory.md"))
	if err != nil {
		t.Fatalf("read the inventory table (the deliverable this gate protects): %v", err)
	}
	return string(b)
}

// TestMemoryHelpGroupPageAdvertisesEveryDispatchableSubcommand is the help-vs-
// dispatch gate at the group level, and it is a FINDING's regression guard: the
// group page omitted `backup` entirely while the dispatcher, the completion set
// and the unknown-subcommand error line all advertised it. A subcommand the
// binary accepts but its own group page never mentions is undiscoverable from
// the surface a reader is told to read.
func TestMemoryHelpGroupPageAdvertisesEveryDispatchableSubcommand(t *testing.T) {
	advertised := map[string]bool{}
	for _, v := range g3AdvertisedVerbs() {
		advertised[v] = true
	}
	for _, sub := range memorySubcommandNames {
		if !advertised[sub] {
			t.Errorf("`gopi memory %s` is dispatched (memorySubcommandNames) but the group help prints no synopsis for it — a subcommand only reachable by knowing its name is undiscoverable", sub)
		}
	}
	// The reverse direction: a synopsis the dispatcher does not accept would be
	// a promise the CLI breaks on the next keystroke.
	for _, v := range g3AdvertisedVerbs() {
		if !containsString(memorySubcommandNames, v) {
			t.Errorf("the group help advertises `gopi memory %s` but memorySubcommandNames does not carry it — typing the advertised spelling would be refused", v)
		}
	}
	// The two mode flags the group page now names must be reachable there: a
	// reader who learns a flag only from the sub-verb page has a page they were
	// never pointed at.
	// `--serve-assets` dropped off this list with `gopi memory sync` (DELETED
	// 2026-11-05): the flag belonged to that verb's peer-side mode, so requiring
	// the group page to spell it would demand a promise nothing can keep.
	for _, want := range []string{tools.LedgerBackupServeFlag} {
		if !strings.Contains(memoryUsage, "--"+want) {
			t.Errorf("the group help never spells --%s, though both are advertised by their own page (and both are ssh'd by an initiator) — name them here, or the reader cannot tell the modes apart", want)
		}
	}
}

// TestMemoryHelpSurfaceAdvertisedFlagsAreAdmitted closes the SECOND half of the
// same class, at the parser boundary: every flag a verb's synopsis advertises
// must be ADMITTED by that verb's flag table. The measured defect on the G1
// sibling was exactly this shape (`task sweep-worktrees --json` advertised and
// refused), which is why it is a SWEEP over the family rather than a case for
// one verb.
//
// A refusal for any OTHER reason (a missing companion flag, a bad combination)
// is fine — this test asks one question: does the FLAG TABLE admit the spelling
// the help prints?
func TestMemoryHelpSurfaceAdvertisedFlagsAreAdmitted(t *testing.T) {
	probeDir := t.TempDir()
	val := map[string]string{
		"db": filepath.Join(probeDir, "g3-probe.db"), "limit": "1", "intent": "debug",
		"keywords": "k1,k2", "peer": g3SimPeer, "dir": probeDir, "memory-id": "1",
		"name": "memory.db.backup-20000101-000000.db", "reason": "probe",
		"topic": "t", "question": "q", "answer": "a", "source": "s", "confidence": "0.5",
	}
	pos := map[string]string{
		"search": "q", "get": "1", "update": "1", "delete": "1", "feedback": "1",
	}
	scanned := 0
	for _, verb := range g3AdvertisedVerbs() {
		flags := g3AdvertisedFlags(verb)
		for _, f := range flags {
			scanned++
			argv := []string{CliVerbMemory, verb}
			if p, ok := pos[verb]; ok {
				argv = append(argv, p)
			}
			if v, need := val[f]; need {
				argv = append(argv, "--"+f, v)
			} else {
				argv = append(argv, "--"+f)
			}
			argv = append(argv, "-h") // the flag must at least be ADMITTED; -h stops before any effect
			out, code := g3RunProbe(t, t.TempDir(), nil, argv...)
			if code == 0 {
				continue // admitted and ran the help path
			}
			if strings.Contains(out, "unknown flag --"+f) {
				t.Errorf("`gopi memory %s --%s` is REFUSED by the flag table while the synopsis advertises it:\n%s\n"+
					"An advertised flag is either obeyed or refused by NAME (Rule 15) — add it to the flag table for %q, or take it out of the synopsis.",
					verb, f, out, verb)
			}
		}
	}
	// The floor is the measured surface (51 advertised verb-flag pairs at the
	// time of writing) minus headroom: a scanner that regressed to head-lines-
	// only would report far fewer and must fail rather than pass quietly.
	if scanned < 50 {
		t.Fatalf("the scan inspected only %d advertised flag sites — the synopsis parser is broken, so a green run proves nothing", scanned)
	}
}

// g3StringDiff renders the two-way difference between want and got.
func g3StringDiff(want, got []string) string {
	inGot := map[string]bool{}
	for _, s := range got {
		inGot[s] = true
	}
	inWant := map[string]bool{}
	for _, s := range want {
		inWant[s] = true
	}
	var b strings.Builder
	for _, s := range want {
		if !inGot[s] {
			fmt.Fprintf(&b, "  want-only (advertised/claimed but not present): %s\n", s)
		}
	}
	for _, s := range got {
		if !inWant[s] {
			fmt.Fprintf(&b, "  got-only (present but unaccounted for): %s\n", s)
		}
	}
	return b.String()
}

// --- fixtures -----------------------------------------------------------------

// g3Sandbox is one probe's world: a GOPI_HOME the child may write anything
// into, and the ledger path inside it. Both live under the suite's pinned
// scratch root (Rule 22), never under the operator's profile.
type g3Sandbox struct {
	bin  string
	home string
	db   string
}

func g3NewSandbox(t *testing.T) g3Sandbox {
	t.Helper()
	sb := g3Sandbox{bin: gopiTestBinPath(t), home: t.TempDir()}
	sb.db = filepath.Join(sb.home, "g3-ledger.db")
	return sb
}

// g3Run drives the real binary with the sandboxed env, the ledger override and
// the declared memory peer. A timeout is its own failure: a correctly routed
// memory verb never reaches the agent loop, so a probe that hangs is a routing
// defect.
func (s g3Sandbox) g3Run(t *testing.T, extraEnv []string, args ...string) (string, int) {
	t.Helper()
	return g3RunProbe(t, s.home, append([]string{
		config.MemoryDBEnv + "=" + s.db,
		config.MemoryPeersEnv + "=" + g3SimPeer,
		"GOPI_SYNC_NODE=" + g3SimNode,
	}, extraEnv...), args...)
}

// g3RunProbe is the process driver: scrubbed env (cliContractEnv), plus the
// caller's overrides, against the one built binary. Its argv[0] is MAPPED by
// familyAddressArgs — see g3RunVerbatimProbe for the arm that must not be.
func g3RunProbe(t *testing.T, home string, extraEnv []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(gopiTestBinPath(t), familyAddressArgs(args)...)
	cmd.Env = append(cliContractEnv(home), extraEnv...)
	return g3AwaitProbe(t, cmd, strings.Join(args, " "))
}

// g3RunVerbatim is g3RunProbe's OPPOSITE door: argv reaches the binary EXACTLY
// as the caller typed it, with NO familyAddressArgs rewrite.
//
// WHY IT EXISTS (measured 2026-11-06, task 01a1aca1). `familyAddressArgs` is the
// door MAPPER: it translates a retired flat word at argv[0] into the family
// address that dispatches it (`brew` → `memory brew`). Every assertion driven
// through `g3Run`/`g3RunOKEnv` therefore measures the GROUP door, whatever word
// the caller typed — so a sentence claiming the FLAT spelling still runs was
// green while the flat door refuses:
//
//	$ gopi brew --dir /tmp/x      # verbatim
//	exit=1
//	gopi: `brew` is RETIRED — use `gopi memory brew` (`gopi memory brew -h` for its help). …
//
// A retirement claim is a claim about argv, so it can only be settled by a run
// that hands argv over unchanged. This driver is that run, and it is the ONLY
// difference from g3RunProbe: same sandbox env, same ledger override, same
// timeout (a hang is a routing defect, not a slow test).
func (s g3Sandbox) g3RunVerbatim(t *testing.T, extraEnv []string, args ...string) (string, int) {
	t.Helper()
	return g3RunVerbatimProbe(t, s.home, append([]string{
		config.MemoryDBEnv + "=" + s.db,
		config.MemoryPeersEnv + "=" + g3SimPeer,
		"GOPI_SYNC_NODE=" + g3SimNode,
	}, extraEnv...), args...)
}

// g3RunVerbatimProbe is the process driver with argv passed through UNTOUCHED —
// the sibling of g3RunProbe, and the only place in this file that starts the
// binary without the door mapper.
func g3RunVerbatimProbe(t *testing.T, home string, extraEnv []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(gopiTestBinPath(t), args...)
	cmd.Env = append(cliContractEnv(home), extraEnv...)
	return g3AwaitProbe(t, cmd, strings.Join(args, " "))
}

// g3AwaitProbe is the shared wait: one bounded wall-clock, and a timeout is its
// own failure rather than a silent pass. Both drivers (mapped and verbatim) end
// here, so the bound and its message cannot drift between them.
func g3AwaitProbe(t *testing.T, cmd *exec.Cmd, spelled string) (string, int) {
	t.Helper()
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(g3MemoryVerbTimeout * time.Second):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		<-done
		t.Fatalf("gopi %s: TIMEOUT after %ds — argv fell through to the agent loop, or parked on a child",
			spelled, g3MemoryVerbTimeout)
	}
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("gopi %s: %v\n%s", spelled, err, out)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// g3RunOK runs through the sandbox env and requires exit 0.
func (s g3Sandbox) g3RunOK(t *testing.T, args ...string) string {
	t.Helper()
	return s.g3RunOKEnv(t, nil, args...)
}

// g3RunEnv runs with extra env and returns output + exit code.
func (s g3Sandbox) g3RunEnv(t *testing.T, extraEnv []string, args ...string) (string, int) {
	t.Helper()
	return s.g3Run(t, extraEnv, args...)
}

// g3RunOKEnv runs with extra env and requires exit 0.
func (s g3Sandbox) g3RunOKEnv(t *testing.T, extraEnv []string, args ...string) string {
	t.Helper()
	out, code := s.g3Run(t, extraEnv, args...)
	if code != 0 {
		t.Fatalf("gopi %s: exit %d (want 0)\n%s", strings.Join(args, " "), code, out)
	}
	return out
}

// g3Seed writes ONE distilled row through the CLI's own `memory store` — the
// door an operator uses — and returns the id it reports, so no probe depends on
// a hand-rolled INSERT (Rule 32).
func g3Seed(t *testing.T, sb g3Sandbox, topic, question, answer string) int {
	t.Helper()
	out := sb.g3RunOK(t, CliVerbMemory, "store",
		"--topic", topic, "--question", question, "--answer", answer,
		"--keywords", "g3-breadcrumb,probe", "--source", "g3-fixture")
	re := regexp.MustCompile(`id=(\d+)`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("`gopi memory store` did not report an id:\n%s", out)
	}
	var id int
	if _, err := fmt.Sscanf(m[1], "%d", &id); err != nil {
		t.Fatalf("parse the stored id %q: %v", m[1], err)
	}
	return id
}

// --- one case per advertised verb ---------------------------------------------
//
// Each case is BASIC by design: the help's own invocation on the happy path,
// plus the verb's first refusal (the mis-invocation an operator makes before he
// reads the description). Deeper contracts already have their own homes
// (cli_memory_test.go, cli_memory_sync_test.go, cli_memory_backup_test.go).

func TestMemoryVerbSearchRunsAsAdvertised(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	sb := g3NewSandbox(t)
	g3Seed(t, sb, "g3 search topic", "g3 search question", "g3 search answer")

	out := sb.g3RunOK(t, CliVerbMemory, "search", "g3 search question")
	if !strings.Contains(out, "g3 search topic") {
		t.Errorf("`gopi memory search \"<query>\"` must find the stored row:\n%s", out)
	}
	// The three advertised flags, each on the help's own spelling.
	if o := sb.g3RunOK(t, CliVerbMemory, "search", "g3 search question", "--limit", "1"); !strings.Contains(o, "g3 search topic") {
		t.Errorf("--limit must be admitted and honored:\n%s", o)
	}
	if o := sb.g3RunOK(t, CliVerbMemory, "search", "g3 search question", "--intent", "debug"); !strings.Contains(o, "g3 search topic") {
		t.Errorf("--intent must be admitted:\n%s", o)
	}
	// A miss is an ANSWER, and it says so (never a blank success).
	if o := sb.g3RunOK(t, CliVerbMemory, "search", "zzz-g3-definitely-absent"); !strings.Contains(o, "No memory") && !strings.Contains(o, "no result") && strings.TrimSpace(o) == "" {
		t.Errorf("a miss must be reported by name rather than printing nothing:\n%s", o)
	}
	// First refusal: the synopsis requires the query positional.
	out, code := sb.g3Run(t, nil, CliVerbMemory, "search")
	if code == 0 {
		t.Fatalf("search without a query must be refused:\n%s", out)
	}
	if !strings.Contains(out, "missing <query>") {
		t.Errorf("the refusal must name the missing positional:\n%s", out)
	}
	// Second refusal: --limit is documented as N, a positive integer.
	out, code = sb.g3Run(t, nil, CliVerbMemory, "search", "q", "--limit", "0")
	if code == 0 {
		t.Fatalf("--limit 0 must be refused:\n%s", out)
	}
	if !strings.Contains(out, "positive integer") {
		t.Errorf("the refusal must name the domain:\n%s", out)
	}
}

func TestMemoryVerbStoreRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	out := sb.g3RunOK(t, CliVerbMemory, "store",
		"--topic", "g3 store topic", "--question", "g3 store question",
		"--answer", "g3 store answer", "--keywords", "k1,k2", "--source", "g3-probe")
	if !strings.Contains(out, "Memory stored") {
		t.Errorf("the help promises `Memory stored (id=N).`:\n%s", out)
	}
	// The row is readable back through the group's own reader.
	if o := sb.g3RunOK(t, CliVerbMemory, "search", "g3 store question"); !strings.Contains(o, "g3 store topic") {
		t.Errorf("the stored row must be searchable:\n%s", o)
	}
	// The dedup signal the help documents ("or the deduped signal").
	dedup := sb.g3RunOK(t, CliVerbMemory, "store",
		"--topic", "g3 store topic dup", "--question", "g3 store question",
		"--answer", "g3 store answer", "--source", "g3-probe")
	if !strings.Contains(dedup, "Memory stored") && !strings.Contains(dedup, "dedup") {
		t.Errorf("a re-store must report either a store or the documented dedup signal:\n%s", dedup)
	}
	// First refusal: the three required parameters.
	out, code := sb.g3Run(t, nil, CliVerbMemory, "store", "--topic", "t")
	if code == 0 {
		t.Fatalf("store without --question/--answer must be refused:\n%s", out)
	}
	if !strings.Contains(out, "Missing required parameter") {
		t.Errorf("the refusal must name the missing flag:\n%s", out)
	}
	// Second refusal: the synopsis takes NO positional.
	out, code = sb.g3Run(t, nil, CliVerbMemory, "store",
		"--topic", "t", "--question", "q", "--answer", "a", "stray")
	if code == 0 {
		t.Fatalf("a stray positional must be refused:\n%s", out)
	}
	if !strings.Contains(out, "unexpected positional") {
		t.Errorf("the refusal must name the problem:\n%s", out)
	}
}

func TestMemoryVerbGetRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	id := g3Seed(t, sb, "g3 get topic", "g3 get question", "g3 get answer")

	out := sb.g3RunOK(t, CliVerbMemory, "get", fmt.Sprint(id))
	for _, want := range []string{"g3 get topic", "g3 get question", "g3 get answer"} {
		if !strings.Contains(out, want) {
			t.Errorf("`gopi memory get <id>` must render the record in full (%q missing):\n%s", want, out)
		}
	}
	// First refusal: the id positional.
	out, code := sb.g3Run(t, nil, CliVerbMemory, "get")
	if code == 0 {
		t.Fatalf("get without an id must be refused:\n%s", out)
	}
	if !strings.Contains(out, "missing required parameter") && !strings.Contains(out, "id") {
		t.Errorf("the refusal must name the missing id:\n%s", out)
	}
	// Second refusal: an id that names no row fails LOUDLY (exit 1).
	out, code = sb.g3Run(t, nil, CliVerbMemory, "get", "999999")
	if code == 0 {
		t.Fatalf("an absent id must not exit 0 (a blank success is the defect the verb exists to remove):\n%s", out)
	}
}

func TestMemoryVerbListRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	g3Seed(t, sb, "g3 list topic", "g3 list question", "g3 list answer")

	// The bare form is the documented default (N = 20) enumeration.
	out := sb.g3RunOK(t, CliVerbMemory, "list")
	if !strings.Contains(out, "g3 list topic") {
		t.Errorf("`gopi memory list` must enumerate recent records:\n%s", out)
	}
	if !strings.Contains(out, "memory_get") && !strings.Contains(out, "memory get") {
		t.Errorf("the listing must point at the per-id reader (the maintenance loop the help describes):\n%s", out)
	}
	// --limit N is the advertised cap.
	if o := sb.g3RunOK(t, CliVerbMemory, "list", "--limit", "1"); !strings.Contains(o, "g3 list topic") {
		t.Errorf("--limit must be admitted and honored:\n%s", o)
	}
	// First refusal: a non-positive limit.
	out, code := sb.g3Run(t, nil, CliVerbMemory, "list", "--limit", "0")
	if code == 0 {
		t.Fatalf("--limit 0 must be refused:\n%s", out)
	}
	if !strings.Contains(out, "positive integer") {
		t.Errorf("the refusal must name the domain:\n%s", out)
	}
	// Second refusal: the synopsis takes no positional.
	out, code = sb.g3Run(t, nil, CliVerbMemory, "list", "stray")
	if code == 0 {
		t.Fatalf("a positional must be refused:\n%s", out)
	}
}

func TestMemoryVerbUpdateRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	id := g3Seed(t, sb, "g3 update topic", "g3 update question", "g3 update answer")

	// The verb's headline use: --tier cold demotes without deleting.
	out := sb.g3RunOK(t, CliVerbMemory, "update", fmt.Sprint(id), "--tier", "cold")
	if !strings.Contains(out, "Memory updated") {
		t.Errorf("`gopi memory update <id> --tier cold` must report the edit:\n%s", out)
	}
	if o := sb.g3RunOK(t, CliVerbMemory, "get", fmt.Sprint(id)); !strings.Contains(o, "tier=cold") {
		t.Errorf("the demotion must be readable on the record afterwards (an accepted-and-ignored flag is Rule 15's silent failure):\n%s", o)
	}
	// The other advertised fields, on one invocation.
	out = sb.g3RunOK(t, CliVerbMemory, "update", fmt.Sprint(id),
		"--topic", "g3 update topic 2", "--question", "g3 update question 2",
		"--answer", "g3 update answer 2", "--source", "g3-updated",
		"--confidence", "0.5", "--keywords", "k9")
	if !strings.Contains(out, "Memory updated") {
		t.Errorf("the documented field set must run in one invocation:\n%s", out)
	}
	// First refusal: nothing to update.
	out, code := sb.g3Run(t, nil, CliVerbMemory, "update", fmt.Sprint(id))
	if code == 0 {
		t.Fatalf("update with no field must be refused (the help says at least one is required):\n%s", out)
	}
	if !strings.Contains(out, "nothing to update") {
		t.Errorf("the refusal must name the problem:\n%s", out)
	}
	// Second refusal: --confidence is documented as 0..1.
	out, code = sb.g3Run(t, nil, CliVerbMemory, "update", fmt.Sprint(id), "--confidence", "2")
	if code == 0 {
		t.Fatalf("--confidence 2 must be refused:\n%s", out)
	}
	if !strings.Contains(out, "[0,1]") {
		t.Errorf("the refusal must name the domain:\n%s", out)
	}
}

func TestMemoryVerbDeleteRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	id := g3Seed(t, sb, "g3 delete topic", "g3 delete question", "g3 delete answer")

	out := sb.g3RunOK(t, CliVerbMemory, "delete", fmt.Sprint(id))
	if !strings.Contains(out, "Memory deleted") {
		t.Errorf("`gopi memory delete <id>` must report the removal:\n%s", out)
	}
	// The claim the help makes is that the record is GONE — asserted, not assumed.
	if o, code := sb.g3Run(t, nil, CliVerbMemory, "get", fmt.Sprint(id)); code == 0 {
		t.Errorf("the deleted record must no longer be readable:\n%s", o)
	}
	// First refusal: deleting the same id twice (no row).
	out, code := sb.g3Run(t, nil, CliVerbMemory, "delete", fmt.Sprint(id))
	if code == 0 {
		t.Fatalf("a second delete must be refused:\n%s", out)
	}
	if !strings.Contains(out, "no record with id") {
		t.Errorf("the refusal must name the reason:\n%s", out)
	}
	// Second refusal: the id positional.
	out, code = sb.g3Run(t, nil, CliVerbMemory, "delete")
	if code == 0 {
		t.Fatalf("delete without an id must be refused:\n%s", out)
	}
}

func TestMemoryVerbFeedbackRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	id := g3Seed(t, sb, "g3 feedback topic", "g3 feedback question", "g3 feedback answer")

	// Vote mode: the three ratings, one at a time, each on its own invocation.
	for _, rating := range []string{"useful", "useless", "unused"} {
		out := sb.g3RunOK(t, CliVerbMemory, "feedback", fmt.Sprint(id), "--"+rating)
		if !strings.Contains(out, "rating="+rating) {
			t.Errorf("`gopi memory feedback <id> --%s` must record that rating:\n%s", rating, out)
		}
	}
	// --reason and --task are the advertised companions (both on one run).
	out := sb.g3RunOK(t, CliVerbMemory, "feedback", fmt.Sprint(id), "--useful", "--reason", "g3 probe reason", "--task", "T-g3")
	if !strings.Contains(out, "g3 probe reason") {
		t.Errorf("--reason must reach the audit trail:\n%s", out)
	}
	// Mode --list, with the advertised --memory-id filter.
	if o := sb.g3RunOK(t, CliVerbMemory, "feedback", "--list"); !strings.Contains(o, "g3 feedback topic") {
		t.Errorf("`gopi memory feedback --list` must audit the voted records:\n%s", o)
	}
	if o := sb.g3RunOK(t, CliVerbMemory, "feedback", "--list", "--memory-id", fmt.Sprint(id)); !strings.Contains(o, "g3 feedback topic") {
		t.Errorf("--memory-id must narrow the audit to that record:\n%s", o)
	}
	// Mode --suggest-cold: an ANSWER even when there is nothing to suggest.
	out = sb.g3RunOK(t, CliVerbMemory, "feedback", "--suggest-cold")
	if strings.TrimSpace(out) == "" {
		t.Errorf("--suggest-cold must answer by name when there is no candidate:\n%s", out)
	}
	// Mode --recompute rebuilds the materialized columns from the event history.
	if o := sb.g3RunOK(t, CliVerbMemory, "feedback", "--recompute"); !strings.Contains(o, "recomputed") {
		t.Errorf("`gopi memory feedback --recompute` must report the rebuild:\n%s", o)
	}
	// First refusal: exactly one rating.
	out, code := sb.g3Run(t, nil, CliVerbMemory, "feedback", fmt.Sprint(id))
	if code == 0 {
		t.Fatalf("a vote with no rating must be refused:\n%s", out)
	}
	if !strings.Contains(out, "exactly one of") {
		t.Errorf("the refusal must name the missing choice:\n%s", out)
	}
	// Second refusal: the ratings are mutually exclusive.
	out, code = sb.g3Run(t, nil, CliVerbMemory, "feedback", fmt.Sprint(id), "--useful", "--useless")
	if code == 0 {
		t.Fatalf("--useful --useless must be refused:\n%s", out)
	}
	if !strings.Contains(out, "mutually exclusive") {
		t.Errorf("the refusal must say why:\n%s", out)
	}
}

func TestMemoryVerbBackupRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	g3Seed(t, sb, "g3 backup topic", "g3 backup question", "g3 backup answer")
	backupDir := t.TempDir()

	// Happy path: --no-ship takes the snapshot only, and the output must carry
	// the four things the help promises (path, size, sha256, verified count).
	out := sb.g3RunOK(t, CliVerbMemory, tools.LedgerBackupVerb,
		"--db", sb.db, "--dir", backupDir, "--no-ship")
	for _, want := range []string{"ledger backup:", "sha256=", "read-only open OK", "PRAGMA quick_check = ok", "select count(*) from tasks"} {
		if !strings.Contains(out, want) {
			t.Errorf("`gopi memory backup --no-ship` must report %q:\n%s", want, out)
		}
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read the backup dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the snapshot must land as ONE artifact in --dir, got %d entries", len(entries))
	}
	// --name is the advertised override, and its domain is the artifact layout.
	named := "memory.db.backup-20000101-000000.db"
	if o := sb.g3RunOK(t, CliVerbMemory, tools.LedgerBackupVerb,
		"--db", sb.db, "--dir", backupDir, "--no-ship", "--name", named); !strings.Contains(o, named) {
		t.Errorf("--name must name the artifact:\n%s", o)
	}
	if _, err := os.Stat(filepath.Join(backupDir, named)); err != nil {
		t.Errorf("the artifact --name named must exist: %v", err)
	}
	// --dir is measured, not assumed: the artifact lands where it was told to.
	if _, err := os.Stat(filepath.Join(backupDir, entries[0].Name())); err != nil {
		t.Errorf("the default-named artifact must exist in --dir: %v", err)
	}

	// --serve is the peer side: one envelope in, one JSON receipt out.
	body, err := os.ReadFile(filepath.Join(backupDir, named))
	if err != nil {
		t.Fatalf("read the artifact: %v", err)
	}
	sum := g3Sha256(t, filepath.Join(backupDir, named))
	envelope := fmt.Sprintf("{\"version\":1,\"filename\":%q,\"size\":%d,\"checksum\":%q,\"checksum_algo\":\"sha256\"}\n%s",
		named, len(body), sum, string(body))
	recvDir := t.TempDir()
	out, code := g3RunWithStdin(t, sb.home, envelope, nil,
		CliVerbMemory, tools.LedgerBackupVerb, "--serve", "--dir", recvDir)
	if code != 0 {
		t.Fatalf("`gopi memory backup --serve` with a verified envelope must answer a receipt, exit %d:\n%s", code, out)
	}
	var receipt tools.LedgerBackupReceipt
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &receipt); err != nil {
		t.Fatalf("the serve leg's stdout must be the JSON receipt and nothing else: %v\n%q", err, out)
	}
	if receipt.Filename != named || receipt.Checksum != sum || receipt.Consistency != "ok" {
		t.Errorf("the receipt must name the artifact and the proof: %+v", receipt)
	}
	if _, err := os.Stat(filepath.Join(recvDir, named)); err != nil {
		t.Errorf("the received artifact must be on disk: %v", err)
	}
	// Refusal: an envelope whose bytes do not match its header is refused and
	// leaves NOTHING behind (a truncated .db in backups/ is the file trusted later).
	short := fmt.Sprintf("{\"version\":1,\"filename\":%q,\"size\":%d,\"checksum\":%q,\"checksum_algo\":\"sha256\"}\n%s",
		named, len(body), sum, string(body[:len(body)/2]))
	out, code = g3RunWithStdin(t, sb.home, short, nil, CliVerbMemory, tools.LedgerBackupVerb, "--serve", "--dir", recvDir)
	if code == 0 {
		t.Fatalf("a short transfer must be refused:\n%s", out)
	}
	if e, err := os.ReadDir(recvDir); err != nil || len(e) != 1 {
		t.Errorf("a refused transfer must leave no artifact (dir has %d entries, err=%v)", len(e), err)
	}

	// First refusal: a mistyped flag. Every flag of this sub-verb writes
	// something or ships over the network, so a typo must be REFUSED rather
	// than silently ignored (the help says so in its own words).
	out, code = sb.g3Run(t, nil, CliVerbMemory, tools.LedgerBackupVerb, "--db", sb.db, "--dir", backupDir, "--no-shipp")
	if code == 0 {
		t.Fatalf("a mistyped --no-ship must be refused:\n%s", out)
	}
	if !strings.Contains(out, "unknown flag --no-shipp") {
		t.Errorf("the refusal must name the flag that was not understood:\n%s", out)
	}
	// Second refusal: --name's documented domain (a bare artifact name).
	out, code = sb.g3Run(t, nil, CliVerbMemory, tools.LedgerBackupVerb, "--db", sb.db, "--dir", backupDir, "--no-ship", "--name", "../escape")
	if code == 0 {
		t.Fatalf("--name ../escape must be refused:\n%s", out)
	}
	// Third refusal: --peer names a DECLARED peer; an undeclared one is refused
	// and the local artifact is still reported as good (the help's own promise:
	// a silently failed backup is worse than none).
	out, code = sb.g3Run(t, nil, CliVerbMemory, tools.LedgerBackupVerb, "--db", sb.db, "--dir", backupDir, "--peer", "g3-undeclared-peer")
	if code == 0 {
		t.Fatalf("an undeclared --peer must fail the ship leg:\n%s", out)
	}
	if !strings.Contains(out, "whitelist") {
		t.Errorf("the ship refusal must name the missing declaration:\n%s", out)
	}
	if !strings.Contains(out, "ledger backup:") {
		t.Errorf("a FAILED ship must still report the local snapshot as taken and verified:\n%s", out)
	}
}

func TestMemoryVerbProvenanceRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	g3Seed(t, sb, "g3 provenance topic", "g3 provenance question", "g3 provenance answer")

	// Every mode the help advertises, one invocation each.
	shape := sb.g3RunOK(t, CliVerbMemory, memoryProvenanceVerb, "--shape")
	if !strings.Contains(shape, "total rows") {
		t.Errorf("--shape must render the origin distribution:\n%s", shape)
	}
	// A bare run is the documented DEFAULT, and the default is the read-only one.
	if bare := sb.g3RunOK(t, CliVerbMemory, memoryProvenanceVerb); bare != shape {
		t.Errorf("a bare `gopi memory provenance` must be the --shape report (the safe default):\nbare:\n%s\n--shape:\n%s", bare, shape)
	}
	backfill := sb.g3RunOK(t, CliVerbMemory, memoryProvenanceVerb, "--backfill")
	if !strings.Contains(backfill, "scanned=") {
		t.Errorf("--backfill must report its pass:\n%s", backfill)
	}
	// The documented idempotency witness: a second run fills nothing.
	again := sb.g3RunOK(t, CliVerbMemory, memoryProvenanceVerb, "--backfill")
	if !strings.Contains(again, "filled=0") {
		t.Errorf("--backfill must be idempotent (a second run reports filled=0):\n%s", again)
	}
	if o := sb.g3RunOK(t, CliVerbMemory, memoryProvenanceVerb, "--reclassify"); !strings.Contains(o, "scanned=") {
		t.Errorf("--reclassify must report its pass:\n%s", o)
	}
	if o := sb.g3RunOK(t, CliVerbMemory, memoryProvenanceVerb, "--counts"); !strings.Contains(o, "total rows") {
		t.Errorf("--counts must render the persisted verification query:\n%s", o)
	}
	if o := sb.g3RunOK(t, CliVerbMemory, memoryProvenanceVerb, "--latent"); !strings.Contains(o, "unknown rows") {
		t.Errorf("--latent must render its three-way split:\n%s", o)
	}
	candidates := sb.g3RunOK(t, CliVerbMemory, memoryProvenanceVerb, "--latent-candidates")
	if !strings.Contains(candidates, "harvestable") || !strings.Contains(candidates, "reproduce without this binary") {
		t.Errorf("--latent-candidates must render the candidate population AND the SQL that reproduces it:\n%s", candidates)
	}
	// First refusal here is its own evidence: --latent-crosscheck reads a column
	// that only --harvest-latent writes, and it says so instead of rendering an
	// empty table.
	out, code := sb.g3Run(t, nil, CliVerbMemory, memoryProvenanceVerb, "--latent-crosscheck")
	if code == 0 {
		t.Fatalf("--latent-crosscheck before --harvest-latent must be refused:\n%s", out)
	}
	if !strings.Contains(out, "harvest-latent") {
		t.Errorf("the refusal must name the mode that must run first:\n%s", out)
	}
	if o := sb.g3RunOK(t, CliVerbMemory, memoryProvenanceVerb, "--harvest-latent"); !strings.Contains(o, "rows written this pass") {
		t.Errorf("--harvest-latent must report what it wrote:\n%s", o)
	}
	if o := sb.g3RunOK(t, CliVerbMemory, memoryProvenanceVerb, "--latent-crosscheck"); !strings.Contains(o, "manual cross-check") {
		t.Errorf("--latent-crosscheck must render the harvested rows once the column exists:\n%s", o)
	}
	if o := sb.g3RunOK(t, CliVerbMemory, memoryProvenanceVerb, "--latent-calibrate"); !strings.Contains(o, "resolve on disk") {
		t.Errorf("--latent-calibrate must render both resolve rates:\n%s", o)
	}
	// Second refusal: the modes are mutually exclusive.
	out, code = sb.g3Run(t, nil, CliVerbMemory, memoryProvenanceVerb, "--shape", "--counts")
	if code == 0 {
		t.Fatalf("--shape --counts must be refused:\n%s", out)
	}
	if !strings.Contains(out, "mutually exclusive") {
		t.Errorf("the refusal must say why:\n%s", out)
	}
	// Third refusal: the synopsis takes no positional.
	out, code = sb.g3Run(t, nil, CliVerbMemory, memoryProvenanceVerb, "stray")
	if code == 0 {
		t.Fatalf("a positional must be refused:\n%s", out)
	}
}

// TestMemoryLegacyFlatVerbsStillRunAsAdvertised covers the surface the group
// help's own closing paragraph advertises, by name: "The legacy flat verbs
// (gopi memory_search, memory_store, memory_get, memory_update, memory_delete,
// memory_feedback, memory_feedback_list) still work but print a deprecation
// note pointing here". An advertised spelling that no longer runs would be the
// same help/code disagreement this task hunts, one paragraph lower.
func TestMemoryLegacyFlatVerbsStillRunAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	id := g3Seed(t, sb, "g3 legacy topic", "g3 legacy question", "g3 legacy answer")

	cases := []struct {
		verb string
		args []string
		want string
	}{
		{"memory_search", []string{"g3 legacy question"}, "g3 legacy topic"},
		{"memory_store", []string{"--topic", "g3 legacy 2", "--question", "g3 legacy question 2", "--answer", "a"}, "Memory stored"},
		{"memory_get", []string{fmt.Sprint(id)}, "g3 legacy topic"},
		{"memory_update", []string{fmt.Sprint(id), "--tier", "cold"}, "Memory updated"},
		{"memory_feedback", []string{fmt.Sprint(id), "--useful"}, "rating=useful"},
		{"memory_feedback_list", nil, "feedback"},
		{"memory_delete", []string{fmt.Sprint(id)}, "Memory deleted"},
	}
	for _, c := range cases {
		out, code := sb.g3Run(t, nil, append([]string{c.verb}, c.args...)...)
		if code != 0 {
			t.Errorf("`gopi %s` is advertised as still working but exited %d:\n%s", c.verb, code, out)
			continue
		}
		if !strings.Contains(out, "is deprecated — use gopi memory ") {
			t.Errorf("`gopi %s` must print the advertised deprecation note pointing at the grouped form:\n%s", c.verb, out)
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("`gopi %s` must still do the work (%q missing):\n%s", c.verb, c.want, out)
		}
	}
}

// TestBrewRunsAsAdvertised covers the ONE memory-family verb that is not under
// the `memory` group word: `gopi memory brew`, whose page the briefing names beside
// the group. The sweep itself needs an LLM and session logs, so what is
// asserted here is the surface's own contract — the advertised flags reach the
// pipeline and the refusals are by name.
func TestBrewRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	emptyDir := t.TempDir() // no sessions: the sweep runs, calls no LLM, stores nothing

	// The brew ENGINE resolves an LLM credential at construction even when
	// there is nothing to sweep, so the probe supplies a placeholder — the
	// value is never used (zero sessions = zero LLM calls, and the sweep's own
	// counters are asserted below). A credential is a config value, not a
	// mocked peer: nothing here answers as a host.
	cred := []string{"ANTHROPIC_AUTH_TOKEN=g3-probe-placeholder"}

	// The bare advertised spelling, against a directory with nothing to sweep.
	out := sb.g3RunOKEnv(t, cred, CliVerbBrew, "--dir", emptyDir)
	if !strings.Contains(out, "brew done in") {
		t.Errorf("`gopi memory brew --dir <PATH>` must render the run summary:\n%s", out)
	}
	if !strings.Contains(out, "sessions=0") {
		t.Errorf("an empty --dir must sweep zero sessions (the probe must not depend on the host's logs):\n%s", out)
	}
	// --dry-run is "shows what WOULD be stored without writing anything".
	if o := sb.g3RunOKEnv(t, cred, CliVerbBrew, "--dir", emptyDir, "--dry-run"); !strings.Contains(o, "brew done in") {
		t.Errorf("--dry-run must run the pipeline:\n%s", o)
	}
	// --limit N caps the session count; --json renders the machine-readable form.
	if o := sb.g3RunOKEnv(t, cred, CliVerbBrew, "--dir", emptyDir, "--limit", "1"); !strings.Contains(o, "brew done in") {
		t.Errorf("--limit must be admitted:\n%s", o)
	}
	js := sb.g3RunOKEnv(t, cred, CliVerbBrew, "--dir", emptyDir, "--json")
	if !strings.HasPrefix(strings.TrimSpace(js), "{") || !strings.Contains(js, "sessions_scanned") {
		t.Errorf("--json must render the stats object:\n%s", js)
	}
	// --session PATH names one log; a path that does not exist is reported as an
	// error by NAME (never a silent empty sweep).
	missing := filepath.Join(emptyDir, "g3-absent-session.jsonl")
	out, code := sb.g3RunEnv(t, cred, CliVerbBrew, "--session", missing)
	if code != 0 {
		t.Fatalf("a bad --session is a reported error, not a crash (exit %d):\n%s", code, out)
	}
	if !strings.Contains(out, "g3-absent-session.jsonl") {
		t.Errorf("the missing session path must be named in the run's errors:\n%s", out)
	}
	// First refusal: the page advertises no positional at all.
	out, code = sb.g3Run(t, nil, CliVerbBrew, "stray")
	if code == 0 {
		t.Fatalf("a positional must be refused:\n%s", out)
	}
	if !strings.Contains(out, "unexpected argument") {
		t.Errorf("the refusal must name the problem:\n%s", out)
	}
	// Second refusal: --limit is documented as N.
	out, code = sb.g3RunEnv(t, cred, CliVerbBrew, "--limit", "abc")
	if code == 0 {
		t.Fatalf("--limit abc must be refused:\n%s", out)
	}
	if !strings.Contains(out, "positive integer") {
		t.Errorf("the refusal must name the domain:\n%s", out)
	}
	// The leading -h is the page (the position the page's own usage line uses).
	if o, code := sb.g3Run(t, nil, CliVerbBrew, "-h"); code != 0 || !strings.Contains(o, "GoPi brew — distill reusable lessons") {
		t.Errorf("`gopi memory brew -h` must print the page:\n%s", o)
	}
	// The trailing -h is the page, at ANY position: every verb in the family
	// scans its whole argv for -h (runMemorySubcommand, runMemorySync,
	// runMemoryBackup), and brew now does too. This assertion used to pin the
	// OPPOSITE — brew answered -h only in the LEADING position, so
	// `gopi memory brew --json -h` ran the pipeline instead of printing the
	// page (inventory finding 3). The fix landed with the strict flag table
	// (2026-11-05), because a page is what a reader reaches for at exactly the
	// moment the argv is wrong.
	if o, code := sb.g3RunEnv(t, cred, CliVerbBrew, "--json", "-h"); code != 0 || !strings.Contains(o, "GoPi brew — distill reusable lessons") {
		t.Errorf("brew's trailing -h must print the page at any position (got exit %d):\n%s", code, o)
	}
}

// TestMemoryGroupedBrewAndMemsyncRunAsAdvertised covers the two verbs the
// DEMOTION added to this group's page (epic verb-restructure, 2026-10-05):
// `gopi memory brew` and `gopi memory memsync`. They are reached the way the
// page spells them — never `-h` — because the page is what a reader types, and
// a synopsis naming an argv the dispatcher refuses is the defect G3 exists to
// catch.
//
// THE DEMOTION HAS TWO HALVES AND THEY POINT OPPOSITE WAYS, which is why both
// are asserted here: the GROUP door must RUN, and the FLAT door must REFUSE.
// The second half is the one that was silently missing until 2026-11-06 — the
// assertion claimed the flat spelling "must still run", but it was driven
// through `g3RunOKEnv`, whose driver maps argv[0] with `familyAddressArgs`, so
// it measured the GROUP door and could never see the refusal. Measured on the
// real binary (verbatim argv, this test's own sandbox env):
//
//	$ gopi brew --dir <PATH>
//	exit=1
//	gopi: `brew` is RETIRED — use `gopi memory brew` (`gopi memory brew -h` for its help). …
//
// A retirement claim is a claim about argv, so the arm that makes it runs
// through `g3RunVerbatim` (no mapping) — and it asserts the refusal BY NAME,
// not merely a non-zero status: a refusal that did not say RETIRED would be a
// different fact wearing the same exit code.
func TestMemoryGroupedBrewRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	emptyDir := t.TempDir() // no sessions: the sweep runs, calls no LLM, stores nothing
	cred := []string{"ANTHROPIC_AUTH_TOKEN=g3-probe-placeholder"}

	// --- the grouped spelling of brew, with the page's own flags -------------
	out := sb.g3RunOKEnv(t, cred, CliVerbMemory, CliVerbBrew, "--dir", emptyDir, "--dry-run")
	if !strings.Contains(out, "brew done in") || !strings.Contains(out, "sessions=0") {
		t.Errorf("`gopi memory %s --dir <PATH> --dry-run` must run the same pipeline as the flat verb:\n%s",
			CliVerbBrew, out)
	}
	// ITS OWN page, not the group's — the synopsis promises a flag set, and a
	// reader who follows the pointer must land on it.
	if o, code := sb.g3Run(t, cred, CliVerbMemory, CliVerbBrew, "-h"); code != 0 || !strings.Contains(o, "GoPi brew — distill reusable lessons") {
		t.Errorf("`gopi memory %s -h` must print brew's own page (exit %d):\n%s", CliVerbBrew, code, o)
	}
	// First refusal, unchanged from the flat verb's contract.
	if o, code := sb.g3Run(t, cred, CliVerbMemory, CliVerbBrew, "--limit", "abc"); code == 0 || !strings.Contains(o, "positive integer") {
		t.Errorf("`gopi memory %s --limit abc` must be refused by name:\n%s", CliVerbBrew, o)
	}
	// THE FLAT DOOR IS RETIRED — asserted VERBATIM, so the driver cannot answer
	// for the wrong door. `--dir` is the flag the group spelling above accepts,
	// so the only thing that can make this run exit 0 is the refusal being gone.
	g3AssertRetiredFlatDoor(t, sb, cred, groupHome(CliVerbMemory, CliVerbBrew), CliVerbBrew, "--dir", emptyDir)
	// And the SAME argv through the mapping driver still reaches the work: the
	// pair is what proves the retirement moved the door rather than deleting the
	// command. Without this arm, "flat is refused" would also hold for a binary
	// that had lost brew entirely.
	if o := sb.g3RunOKEnv(t, cred, CliVerbBrew, "--dir", emptyDir); !strings.Contains(o, "brew done in") {
		t.Errorf("`gopi %s --dir <PATH>` through the family-address driver must reach `%s`:\n%s",
			CliVerbBrew, programAddress(groupHome(CliVerbMemory, CliVerbBrew)), o)
	}
}

// g3AssertRetiredFlatDoor asserts that a RETIRED flat spelling is refused BY
// NAME when it is handed to the binary VERBATIM — no family-address rewrite.
//
// It carries the retirement's OWN vocabulary: the address comes from
// `programAddress(groupHome(...))` (the same derivation the refusal renders,
// Rule 13/14), and the claim is the phrase the refusal leads with. Since the
// refusal text is a constant (`verbAliasRetiredRefusal`: "gopi: `%s` is
// RETIRED — use `%s` …"), checking the word surrounded by that phrase is what
// separates "the retirement refused me" from "something else exited non-zero".
func g3AssertRetiredFlatDoor(t *testing.T, sb g3Sandbox, env []string, address, flatWord string, args ...string) {
	t.Helper()
	out, code := sb.g3RunVerbatim(t, env, append([]string{flatWord}, args...)...)
	if code == 0 {
		t.Errorf("`gopi %s %s` exited 0 — the word is RETIRED (the group door is `%s`), so a verbatim run must be refused:\n%s",
			flatWord, strings.Join(args, " "), address, out)
		return
	}
	if !strings.Contains(out, "`"+flatWord+"` is RETIRED") {
		t.Errorf("`gopi %s %s` exited %d but did not say the word is RETIRED — a refusal must name the retirement, not just fail (want %q):\n%s",
			flatWord, strings.Join(args, " "), code, "`"+flatWord+"` is RETIRED", out)
	}
	if !strings.Contains(out, address) {
		t.Errorf("`gopi %s %s` refusal never names the live door %q, so the reader is left at a dead end (Rule 15):\n%s",
			flatWord, strings.Join(args, " "), address, out)
	}
}

// --- probe helpers ------------------------------------------------------------

// g3RunWithStdin drives the binary with a stdin payload (the two --serve legs'
// protocol is stdin/stdout).
func g3RunWithStdin(t *testing.T, home, stdin string, extraEnv []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(gopiTestBinPath(t), familyAddressArgs(args)...)
	cmd.Env = append(cliContractEnv(home), extraEnv...)
	cmd.Stdin = strings.NewReader(stdin)
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(g3MemoryVerbTimeout * time.Second):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		<-done
		t.Fatalf("gopi %s: TIMEOUT after %ds with stdin", strings.Join(args, " "), g3MemoryVerbTimeout)
	}
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("gopi %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// g3RunWithEnvOK runs the binary with an explicit env and requires exit 0.
func g3RunWithEnvOK(t *testing.T, home string, env []string, args ...string) string {
	t.Helper()
	out, code := g3RunWithStdin(t, home, "", env, args...)
	if code != 0 {
		t.Fatalf("gopi %s: exit %d (want 0)\n%s", strings.Join(args, " "), code, out)
	}
	return out
}

// g3Sha256 shells `sha256sum` for the probe's envelope header — the same
// algorithm the artifact's own checksum used, so the receipt is comparable.
func g3Sha256(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("sha256sum", path).Output()
	if err != nil {
		t.Fatalf("sha256sum %s: %v", path, err)
	}
	return strings.Fields(string(out))[0]
}

// TestMemoryVerbBackfillTokensRunsAsAdvertised drives the one-shot jieba_token
// re-derivation the way the help spells it: --dry-run first (which must report
// the scan/change split and write NOTHING), then the real run, then a second
// run that must re-derive 0 — the idempotence the synopsis promises, because
// that is what makes re-running it after an interruption safe.
//
// The legacy state is produced through the binary's own door as far as
// possible: the row is stored by `memory store`, and only the pre-swap SPELLING
// of the token column (a comma-joined 2-char gram list, what an older gopi
// wrote) is installed by one UPDATE through the sqlite driver this package
// already links. No second implementation of the token rule is introduced
// (Rule 32: the schema and its fixtures come from the engine).
func TestMemoryVerbBackfillTokensRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)
	id := g3Seed(t, sb, "逐步缩小检索算法", "逐步缩小 AND 检索算法在哪里实现",
		"hermes 是参考实现，这个答案正文足够长以通过质量门。")

	legacy := "逐步, 步缩, 缩小, 小检, 检索, 索算"
	db, err := sql.Open("sqlite", tools.LedgerDSN(sb.db))
	if err != nil {
		t.Fatalf("open the sandbox ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore probe handle closed on return
	if _, err := db.Exec("UPDATE distilled_qa SET jieba_token=? WHERE id=?", legacy, id); err != nil {
		t.Fatalf("install the legacy token spelling: %v", err)
	}

	// --dry-run: reports, writes nothing.
	dry := sb.g3RunOK(t, CliVerbMemory, memoryBackfillVerb, "--dry-run")
	if !strings.Contains(dry, "dry-run") || !strings.Contains(dry, "1 row(s) scanned") {
		t.Errorf("--dry-run must report the scan/change split:\n%s", dry)
	}
	var after string
	if err := db.QueryRow("SELECT jieba_token FROM distilled_qa WHERE id=?", id).Scan(&after); err != nil {
		t.Fatalf("read the token back: %v", err)
	}
	if after != legacy {
		t.Fatalf("--dry-run WROTE the ledger: token is now %q", after)
	}

	// The real run. `flag.ExitOnError` gives the binary `-h`/`--help`/an
	// unknown flag as an exit-2 refusal; a STRAY POSITIONAL is not refused by
	// the flag package at all (no positional is declared), so the refusal this
	// surface owes is exercised by the unknown-flag form below.
	run := sb.g3RunOK(t, CliVerbMemory, memoryBackfillVerb)
	if !strings.Contains(run, "1 row(s) scanned") || !strings.Contains(run, "1 jieba_token value(s) re-derived") {
		t.Errorf("the run must report one row re-derived:\n%s", run)
	}
	if err := db.QueryRow("SELECT jieba_token FROM distilled_qa WHERE id=?", id).Scan(&after); err != nil {
		t.Fatalf("read the token back: %v", err)
	}
	// Real segmentation, read as TOKENS rather than as substrings: the
	// re-derived value is space-separated words, so the segmenter's own word
	// for 逐步缩小 (逐步 + 缩小) must be present as TOKENS while the legacy
	// comma-joined grammar must be gone. (A substring test would pass on the
	// gram list too — 逐步 and 缩小 both occur inside ", 步缩, 缩小" — which is
	// exactly the false green this fixture exists to avoid.)
	got := strings.Fields(after)
	if !containsString(got, "逐步") || !containsString(got, "缩小") {
		t.Errorf("the re-derived token does not carry the segmenter's words as tokens: %q", got)
	}
	// The legacy spelling is gone: the OLD value was the gram list of one
	// phrase (逐步, 步缩, 缩小, …), so what distinguishes the two is not the
	// presence of a comma — the row's own keywords field legitimately contains
	// one ("g3-breadcrumb,probe"), and the segmenter emits "," as a token — but
	// that the stored value is no longer ONLY grams. The stored text must carry
	// words the gram window could not produce (检索 / 算法 appear as TOKENS at
	// the head of the value, before the blended prose).
	if len(got) < 4 || got[0] != "逐步" || got[1] != "缩小" {
		t.Errorf("the re-derived value does not begin with the segmenter's words: %q", got[:min(4, len(got))])
	}
	// The row the operator stored is the row that is still there.
	var topic, question string
	if err := db.QueryRow("SELECT topic, question FROM distilled_qa WHERE id=?", id).Scan(&topic, &question); err != nil {
		t.Fatalf("read the row back: %v", err)
	}
	if topic != "逐步缩小检索算法" || question != "逐步缩小 AND 检索算法在哪里实现" {
		t.Errorf("the backfill changed the row's own prose: topic=%q question=%q", topic, question)
	}

	// Idempotent: the second run re-derives nothing.
	second := sb.g3RunOK(t, CliVerbMemory, memoryBackfillVerb)
	if !strings.Contains(second, "0 jieba_token value(s) re-derived") {
		t.Errorf("a second run must re-derive nothing (the synopsis promises it is safe to re-run):\n%s", second)
	}

	// First refusal: an unknown flag is refused by name (exit 2), which is the
	// refusal this surface owes — the help names `--dry-run` and `--db`, and
	// anything else must not be silently accepted.
	out, code := sb.g3Run(t, nil, CliVerbMemory, memoryBackfillVerb, "--nope")
	if code == 0 {
		t.Fatalf("an unknown flag must be refused:\n%s", out)
	}
	if !strings.Contains(out, "nope") {
		t.Errorf("the refusal must name the flag it does not know:\n%s", out)
	}
}

// TestMemoryVerbConvergeKeywordsRunsAsAdvertised drives the one-shot DATA
// migration the way the help spells it: --dry-run first (which must report the
// before histogram and write NOTHING), then the real run, then a second run that
// must change 0 — the idempotence the synopsis promises, because that is what
// makes re-running it after an interruption safe.
//
// The duplicated shape is the state a pre-7139b28 writer left behind, produced
// through the binary's own door as far as possible: the row is stored by
// `memory store` (which appends ONE tail, the fixed writer's behaviour), and only
// the leftover SECOND block is installed by one UPDATE through the sqlite driver
// this package already links. No second implementation of the tail rule is
// introduced here (Rule 32).
func TestMemoryVerbConvergeKeywordsRunsAsAdvertised(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	sb := g3NewSandbox(t)
	id := g3Seed(t, sb, "g3 converge topic", "g3 converge question",
		"这个正文里含 keywords: 这个词但不是尾块，必须逐字节保留。")

	db, err := sql.Open("sqlite", tools.LedgerDSN(sb.db))
	if err != nil {
		t.Fatalf("open the sandbox ledger: %v", err)
	}
	defer db.Close() //stylegate:ignore probe handle closed on return
	var answer, kw string
	if err := db.QueryRow("SELECT answer, search_kw FROM distilled_qa WHERE id=?", id).Scan(&answer, &kw); err != nil {
		t.Fatalf("read the stored row: %v", err)
	}
	// The stored answer already ends with the CURRENT tail (search_kw). The
	// legacy defect is that block sitting AFTER an older one: build it by
	// inserting a STALE block in front, which is exactly the residue a keyword
	// update used to leave.
	stale := "g3-stale,old-breadcrumb"
	body := answer[:strings.LastIndex(answer, "\n\nkeywords: ")]
	dup := body + "\n\nkeywords: " + stale + answer[strings.LastIndex(answer, "\n\nkeywords: "):]
	if _, err := db.Exec("UPDATE distilled_qa SET answer=? WHERE id=?", dup, id); err != nil {
		t.Fatalf("install the duplicated-tail shape: %v", err)
	}

	// --dry-run: reports the histogram, writes nothing.
	dry := sb.g3RunOK(t, CliVerbMemory, memoryConvergeKeywordsVerb, "--dry-run")
	if !strings.Contains(dry, "DRY-RUN") || !strings.Contains(dry, "before {") {
		t.Errorf("--dry-run must report the before histogram:\n%s", dry)
	}
	if !strings.Contains(dry, "affected=1") {
		t.Errorf("--dry-run must report the affected row:\n%s", dry)
	}
	var still string
	if err := db.QueryRow("SELECT answer FROM distilled_qa WHERE id=?", id).Scan(&still); err != nil {
		t.Fatalf("read the answer back: %v", err)
	}
	if still != dup {
		t.Fatalf("--dry-run WROTE the ledger")
	}

	// The real run: two blocks collapse to ONE, keeping the LAST (current).
	run := sb.g3RunOK(t, CliVerbMemory, memoryConvergeKeywordsVerb)
	if !strings.Contains(run, "affected=1") || !strings.Contains(run, "changed=1") {
		t.Errorf("the run must report one row changed:\n%s", run)
	}
	var after string
	if err := db.QueryRow("SELECT answer FROM distilled_qa WHERE id=?", id).Scan(&after); err != nil {
		t.Fatalf("read the answer back: %v", err)
	}
	if n := strings.Count(after, "\n\nkeywords: "); n != 1 {
		t.Fatalf("tail blocks after the run = %d, want 1 (answer %q)", n, after)
	}
	if strings.Contains(after, stale) {
		t.Errorf("the STALE block was kept — the convergence must keep the LAST (current) block:\n%s", after)
	}
	if got := after[:strings.LastIndex(after, "\n\nkeywords: ")]; got != body {
		t.Errorf("the body moved:\n got %q\nwant %q", got, body)
	}

	// Idempotent: a second run changes nothing.
	second := sb.g3RunOK(t, CliVerbMemory, memoryConvergeKeywordsVerb)
	if !strings.Contains(second, "changed=0") {
		t.Errorf("a second run must change nothing (the synopsis promises it is safe to re-run):\n%s", second)
	}

	// First refusal: an unknown flag is refused by name (exit 2).
	out, code := sb.g3Run(t, nil, CliVerbMemory, memoryConvergeKeywordsVerb, "--nope")
	if code == 0 {
		t.Fatalf("an unknown flag must be refused:\n%s", out)
	}
	if !strings.Contains(out, "nope") {
		t.Errorf("the refusal must name the flag it does not know:\n%s", out)
	}
}

// TestMemoryVerbCaptureStatsRunsAsAdvertised drives the read-only survey verb on
// the real binary against a corpus the test OWNS.
//
// WHY IT IS COVERED HERE rather than by a doc row alone: the verb's failure mode
// is not a crash, it is a WRONG CORPUS. A survey that quietly measured the
// operator's live profile, or that rendered a rate off an empty directory, would
// still print a table — so the test pins (a) the numbers it derives from a
// fixture whose bodies it wrote, (b) the refusal on a corpus with no session log,
// and (c) that the default root is the PROFILE's own session directory, not the
// process' cwd.
func TestMemoryVerbCaptureStatsRunsAsAdvertised(t *testing.T) {
	sb := g3NewSandbox(t)

	// A corpus with three bodies: one real markdown table (captured), one short
	// acknowledgement (too-short), one long shapeless prose (no-structure).
	root := filepath.Join(sb.home, "survey-corpus")
	session := filepath.Join(root, "session-g3")
	if err := os.MkdirAll(session, 0o755); err != nil {
		t.Fatalf("mkdir corpus: %v", err)
	}
	// The table body must clear the gate's LENGTH floor, so the fixture carries
	// the table inside a body long enough to be considered at all — the same
	// shape the operator's real round had (1153 runes around a four-column
	// table). The padding is NEUTRAL prose: no box glyph, no marker vocabulary,
	// so the table remains the body's only shape.
	table := respGateTestFiller(700) + "\n\n账单口径对齐了。\n\n| 产品 | 计费单元 | 单价 | 用量 |\n|---|---|---|---|\n| 豆包 | 极速版 | ¥4.5/小时 | 1.9674 小时 |\n"
	shapeless := strings.Repeat("这一轮只是在交代进度，没有结论也没有数字要记住。", 30)
	events := []string{
		`{"type":"request"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":` + g3JSONString(table) + `}]}}`,
		`{"type":"request"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"收到，这就去办。"}]}}`,
		`{"type":"request"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":` + g3JSONString(shapeless) + `}]}}`,
	}
	log := filepath.Join(session, "session.jsonl")
	if err := os.WriteFile(log, []byte(strings.Join(events, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write corpus: %v", err)
	}

	out := sb.g3RunOK(t, CliVerbMemory, memoryCaptureStatsVerb, "--"+memoryCaptureStatsRootFlag, root)
	if !strings.Contains(out, "3 visible response body") {
		t.Errorf("the survey must report the bodies it read (3):\n%s", out)
	}
	if !strings.Contains(out, "table") {
		t.Errorf("the markdown table must be attributed to the table arm:\n%s", out)
	}
	if !strings.Contains(out, "too-short") || !strings.Contains(out, "no-structure") {
		t.Errorf("both refusal buckets must be reported separately:\n%s", out)
	}
	// The comparison the verb exists for: the note corpus' measured rate is
	// quoted on the surface, not asserted in prose.
	if !strings.Contains(out, "note gate") {
		t.Errorf("the report must show the note gate's measured rate beside its own:\n%s", out)
	}

	// --json is the SAME numbers in a diffable shape (Rule 38: a figure names
	// its generator, and a generator has to be comparable between two hosts).
	js := sb.g3RunOK(t, CliVerbMemory, memoryCaptureStatsVerb, "--"+memoryCaptureStatsRootFlag, root, "--"+memoryCaptureStatsJSONFlag)
	var doc memoryCaptureStatsDoc
	if err := json.Unmarshal([]byte(js), &doc); err != nil {
		t.Fatalf("--json must print one JSON document: %v\n%s", err, js)
	}
	if doc.Bodies != 3 || doc.Rounds != 3 {
		t.Errorf("--json reported bodies=%d rounds=%d, want 3/3", doc.Bodies, doc.Rounds)
	}
	if doc.CapturedBodies != 1 {
		t.Errorf("--json reported %d captured bodies, want 1", doc.CapturedBodies)
	}
	if doc.NoteRatePct != memoryCaptureStatsNoteRatePct {
		t.Errorf("--json note rate = %v, want the quoted %v", doc.NoteRatePct, memoryCaptureStatsNoteRatePct)
	}

	// Refusal 1: a corpus holding no session log is refused BY NAME — a rate off
	// an empty directory reads as a finding.
	empty := filepath.Join(sb.home, "empty-corpus")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}
	out, code := sb.g3Run(t, nil, CliVerbMemory, memoryCaptureStatsVerb, "--"+memoryCaptureStatsRootFlag, empty)
	if code == 0 {
		t.Fatalf("an empty corpus must be refused:\n%s", out)
	}
	if !strings.Contains(out, capturesurvey.SurveySessionFileName) {
		t.Errorf("the refusal must name what it looked for:\n%s", out)
	}

	// Refusal 2: an unknown flag is refused by NAME. A typo silently ignored
	// would survey the DEFAULT corpus and read as a finding about the fixture.
	out, code = sb.g3Run(t, nil, CliVerbMemory, memoryCaptureStatsVerb, "--roots", root)
	if code == 0 {
		t.Fatalf("an unknown flag must be refused:\n%s", out)
	}
	if !strings.Contains(out, "roots") {
		t.Errorf("the refusal must name the argument it does not know:\n%s", out)
	}

	// The default root is the PROFILE's session directory (never the cwd): with
	// no --root and an empty profile, the verb refuses rather than measuring
	// whatever the test process was started in.
	out, code = sb.g3Run(t, nil, CliVerbMemory, memoryCaptureStatsVerb)
	if code == 0 {
		t.Errorf("with no --root and no sessions in the profile, the survey must refuse:\n%s", out)
	}
}

// g3JSONString encodes a body as a JSON string literal, so a fixture containing
// quotes, newlines or pipes cannot break the hand-written JSONL around it.
func g3JSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// respGateTestFiller returns n copies of a rune that carries no glyph of any
// gate arm (no box drawing, no pipe, no marker vocabulary), for padding a
// fixture to the length floor without introducing a shape.
func respGateTestFiller(n int) string {
	return strings.Repeat("甲", n)
}
