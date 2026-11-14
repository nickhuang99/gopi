package main

// cli_tasks_advert_gate_test.go — the anti-readd gate of the CLI-surface
// restructure (devdoc/cli-task-surface-restructure-20260915.md).
//
// THE INVARIANT (one function, one home, one help):
//
//	the ledger group (`gopi task -h`) advertises ONLY ledger verbs. It may
//	POINT at a service or a git-infrastructure atom ("see 'gopi merger -h'"),
//	but it must never teach one again — no `gopi task <service|git-atom>`
//	SYNOPSIS line, and none of those verbs' documentable flags restated in
//	the family usage.
//
// WHY A GATE AND NOT A COMMENT: the state this removes was comfortable. Every
// service and merge atom was documented in BOTH places (the top-level page and
// the ledger blob), which reads as thorough and is in fact two homes for one
// contract — the copy that is not updated becomes a lie. A rule stated in prose
// does not survive; this predicate is what makes the next re-addition fail.
//
// The three surfaces checked (all derived, never hand-listed — Rule 13/14):
//
//  1. the family usage constant's `gopi task <verb>` synopsis heads, against
//     taskverbs.MergerVerbs ∪ taskverbs.ServiceAliases (the verbs whose home is
//     `gopi merger -h`) and the sync verbs (home: `gopi task sync -h`, which
//     get folded pointer lines, never a synopsis);
//  2. the `gopi task <TAB>` candidate set (taskverbs.FamilyVerbs, asserted by
//     TestTaskCompletionNamesAreTheSharedFamilySurface);
//  3. the top-level index row's enumeration.
//
// The RED PROOF (TestNoServiceOrSyncVerbInLedgerGroupRedProof) runs the same
// predicate over the PRE-RESTRUCTURE shapes — the exact strings this task
// deleted from cli_tasks.go — and requires each to be flagged, so a green run
// cannot be a blind one (Rule 10.4).

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskverbs"
)

// ledgerAdvertOffense is one reason the ledger group re-advertises a verb whose
// home is elsewhere.
type ledgerAdvertOffense struct {
	verb string
	why  string
}

// documentableServiceFlags are the flags of the component verbs and of the
// git-infrastructure atoms that a re-addition would most likely restate
// (`--daemon` detaches a service; `--force`/`--wait` act on a resident one).
// Used only as an EXTRA signal on the family usage — a synopsis line is the
// primary defect.
//
// `--no-branch-scan` LEFT THIS LIST (F4, 01a0b1b3) because the flag itself is
// gone: its only reader was the unmerged-branch inventory, retired with the
// merge-backlog pipeline it fed. A flag no face accepts any more cannot be
// re-advertised on a face that would refuse it — the same reason `--daemon`
// is still here and `--start` never was.
//
// `--every` IS BACK IN THIS LIST (epic merger-self-driving, 2026-09-18). It left
// with the poll loop (epic daemon-machinery-removal, 2026-09-17) on the argument
// that "nobody can re-advertise a flag that no face accepts any more" — and that
// argument was sound right up to the moment the flag was restored: `gopi merger
// --every D` accepts it again, so the ledger group can restate it again, and a
// flag the family usage re-advertises without the merger page's semantics is
// exactly what this gate exists to catch. A retired flag that no face accepts
// (`--daemon`, `--force`, `--wait`) stays off the list for the same reason it
// always was.
//
// `--force` IS NO LONGER SERVICE-ONLY (epic 01a17dcd, 2026-10-27), and it did
// not leave the list — the list stays as it is, and the predicate below asks a
// DERIVED question instead: a flag the ledger's own admission tables accept is
// no longer a service spelling being smuggled in. `gopi task update --force` is
// the operator's delivery-proof escape, so `--force` appearing in the family
// usage is now CORRECT text, and a bare "does this string occur" test would
// red-flag the very documentation this gate's neighbours require. Not a
// loosening: the exemption is read from `taskFlagMember`, so if `task update`
// ever stops accepting `--force` the exemption disappears with it and this
// check starts enforcing again by itself (Rule 13/14 — derived, never
// hand-listed).
var documentableServiceFlags = []string{
	"--daemon", "--every", "--force", "--wait",
}

// ledgerOwnsFlag reports whether ANY ledger verb admits `flag` in its own
// admission table. It is the derived exemption above: the family usage may
// document a flag a ledger verb actually takes.
func ledgerOwnsFlag(flag string) bool {
	word := strings.TrimPrefix(flag, "--")
	for _, verb := range taskverbs.FamilyVerbs {
		if taskFlagMember(verb, word) {
			return true
		}
	}
	return false
}

// ledgerAdvertOffenses is the predicate: everything the ledger group's usage
// text does that re-teaches a non-ledger verb.
func ledgerAdvertOffenses(usageBody string) []ledgerAdvertOffense {
	// The verbs whose advertised home is NOT the ledger group: the git atoms
	// and the service spellings (`gopi merger -h`), and the sync verbs
	// (`gopi task sync -h`).
	nonLedger := map[string]string{}
	for _, v := range taskverbs.MergerVerbs {
		nonLedger[v] = "gopi merger -h"
	}
	for _, v := range taskverbs.ServiceAliases {
		nonLedger[v] = "gopi merger -h"
	}
	// taskverbs.SyncVerbs is EMPTY now (2026-11-05, every spelling moved to
	// RemovedVerbs), so the loop that used to run over it contributes nothing —
	// but the requirement does NOT disappear with the list, it changes owner: a
	// DELETED spelling must not get a synopsis row here either, or the ledger
	// group re-advertises a command the binary can only refuse. The population
	// is therefore the RETIRED list (the production constant, Rule 13/14), and
	// its "home" is the removal note rather than a manual page.
	for _, v := range syncVerbRemovalSpellings {
		nonLedger[v] = "a removal sentence (the cross-node surface is deleted — see `gopi task -h`)"
	}

	var out []ledgerAdvertOffense
	// ① Any SYNOPSIS line for a non-ledger verb. A synopsis is a line whose
	// head is `gopi task <verb>` — that is the shape that presents the verb as
	// part of this group. A POINTER sentence ("see 'gopi merger -h'", the
	// deprecation note) is prose, not a synopsis, and must stay allowed: the
	// group is allowed to say where the contract went.
	for _, line := range strings.Split(usageBody, "\n") {
		verb, ok := synopsisVerb(line)
		if !ok {
			continue
		}
		if verb == "" {
			continue // the `gopi task <subcommand>`/placeholder rows
		}
		home, isNonLedger := nonLedger[verb]
		if !isNonLedger {
			continue
		}
		out = append(out, ledgerAdvertOffense{
			verb: verb,
			why:  "the family usage carries a `gopi task " + verb + "` SYNOPSIS line — that verb's one advertised home is " + home + "; keep the pointer sentence, not the synopsis",
		})
	}
	// ② A service verb's documentable flag restated in the family usage. A flag
	// a LEDGER verb itself admits is exempt by derivation (ledgerOwnsFlag):
	// `--force` became `task update`'s own escape, so documenting it here is now
	// the correct text rather than a smuggled service spelling.
	for _, flag := range documentableServiceFlags {
		if !strings.Contains(usageBody, flag) || ledgerOwnsFlag(flag) {
			continue
		}
		for _, v := range append(append([]string{}, taskverbs.MergerVerbs...), taskverbs.ServiceAliases...) {
			out = append(out, ledgerAdvertOffense{
				verb: v,
				why:  "the family usage documents the flag " + flag + ", which belongs to the service/git-infrastructure surface (`gopi merger -h` / `gopi worker -h`) — the ledger group must not restate it",
			})
		}
		break
	}
	return out
}

// synopsisVerb reports whether a usage line is a synopsis head (two leading
// spaces, `gopi task `, a verb) and returns the verb.
func synopsisVerb(line string) (string, bool) {
	const head = "  gopi task "
	if !strings.HasPrefix(line, head) {
		return "", false
	}
	rest := strings.TrimPrefix(line, head)
	end := strings.IndexAny(rest, " \t[<")
	verb := rest
	if end >= 0 {
		verb = rest[:end]
	}
	verb = strings.TrimSuffix(verb, "-h")
	if verb == "" || strings.Contains(verb, "|") {
		return "", false
	}
	return verb, true
}

// TestNoServiceOrSyncVerbInLedgerGroup is the gate: the ledger group's help
// text advertises no verb whose home is elsewhere.
func TestNoServiceOrSyncVerbInLedgerGroup(t *testing.T) {
	usageBody := familyUsageBody(t)
	if len(usageBody) < 2000 {
		t.Fatalf("extracted only %d bytes of tasksUsage — the extraction broke, so a green run would prove nothing", len(usageBody))
	}
	// The extraction must actually be reading the family help: its append-only
	// rule text is the marker.
	if !strings.Contains(usageBody, "append-only") {
		t.Fatal("the extracted region is not tasksUsage (no append-only text) — the gate would be vacuous")
	}
	found := 0
	for _, verb := range append(append([]string{}, taskverbs.MergerVerbs...), taskverbs.SyncVerbs...) {
		if strings.Contains(usageBody, "gopi task "+verb) {
			found++
		}
	}
	if found == 0 {
		t.Fatal("no non-ledger verb is even MENTIONED in the family usage — that means the pointers were deleted too; the group must still say where each contract lives (a mention is not a re-teach)")
	}
	for _, offense := range ledgerAdvertOffenses(usageBody) {
		t.Errorf("ledger group re-advertises %q: %s", offense.verb, offense.why)
	}
}

// TestNoServiceOrSyncVerbInLedgerGroupRedProof proves the gate is not vacuous:
// the predicate runs over the PRE-RESTRUCTURE shapes — verbatim excerpts of the
// text this task removed from cli_tasks.go — and each must be flagged. A gate
// you have never seen fail is a gate you cannot trust (Rule 10.4).
func TestNoServiceOrSyncVerbInLedgerGroupRedProof(t *testing.T) {
	for _, probe := range []struct {
		name      string
		body      string
		wantVerbs []string
	}{
		{
			name: "the pre-restructure merger synopsis (+ its flags)",
			body: `  gopi task merger [--every 5m] [--once] [--db PATH]
  gopi task merger --status
      READ-ONLY: is the merger running (flock liveness, never taking
      the lock), which branches the next sweep would merge.
  gopi task merger --stop | --restart | --start [--force] [--wait D]
      Act on the RESIDENT merger from a separate process.
      --every accepts Go durations (30s, 5m, 1h; default 5m).
`,
			wantVerbs: []string{taskverbs.Merger},
		},
		{
			name: "the pre-restructure worker block (a service taught in the ledger group)",
			body: `  gopi worker [--once]
      TASK-MANAGEMENT LOOP (client AND server): the ONLY process that
      claims and runs tasks. A worker is a drain tool, not a daemon.
`,
			wantVerbs: []string{},
		},
		{
			name: "the pre-restructure sync synopsis",
			body: `  gopi task sync [--peer ALIAS | --peers a,b | --status]
      Delta sync: bidirectional timestamp-delta sessions with whitelisted peers.
  gopi task diff --peer ALIAS [--db PATH]
      RECONCILIATION SELF-CHECK (read-only).
`,
			// Three synopsis lines: sync, sync (second head), diff.
			wantVerbs: []string{taskverbs.Sync, taskverbs.Diff},
		},
		{
			name: "the pre-restructure merge atom",
			body: `  gopi task merge [<branch>...] [--all] [--repo PATH] [--db PATH] [--submit]
      Token-free merge back-pressure: for each branch, already merged → branch -d.
`,
			wantVerbs: []string{taskverbs.Merge},
		},
		{
			// The NEGATIVE probe that keeps the gate's scope honest: a pointer
			// sentence is the correct shape and must NOT be flagged.
			name: "the correct pointer shape (allowed)",
			body: `Services (not ledger verbs — the group only points at them):
  gopi merger   merge pass           → 'gopi merger -h'
  gopi worker   task-management loop → 'gopi worker -h'
      Deprecated spellings 'gopi task merger …' and the older 'gopi task serve …'
      keep dispatching for existing deployments and scripts, and say so on use.

Cross-node sync — 'gopi task sync -h' (the patch-FILE channel is 'gopi memory memsync').
`,
			wantVerbs: []string{},
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			got := map[string]bool{}
			for _, o := range ledgerAdvertOffenses(probe.body) {
				got[o.verb] = true
			}
			for _, want := range probe.wantVerbs {
				if !got[want] {
					var names []string
					for v := range got {
						names = append(names, v)
					}
					sort.Strings(names)
					t.Errorf("probe %q did not flag %q (flagged: %v) — the gate would miss that re-addition", probe.name, want, names)
				}
			}
			if len(probe.wantVerbs) == 0 && len(got) > 0 {
				var names []string
				for v := range got {
					names = append(names, v)
				}
				sort.Strings(names)
				t.Errorf("probe %q must produce no finding (the pointer shape is correct), got %v", probe.name, names)
			}
			// The worker probe is the one case where the ledger group taught a
			// verb that is not a taskverbs constant at all: assert it is
			// flagged by NAME so the gate covers the plain `gopi worker` block
			// too (its synopsis head is `gopi worker`, not `gopi task worker`).
			if probe.name == "the pre-restructure worker block (a service taught in the ledger group)" {
				if !ledgerTeachesServiceBlock(probe.body) {
					t.Error("the gate must also reject a bare `gopi worker`/`gopi merger` SYNOPSIS block inside the family usage — that is a service page in the ledger group")
				}
			}
		})
	}
}

// ledgerTeachesServiceBlock reports whether the family usage carries a whole
// service SYNOPSIS block (`  gopi worker …` / `  gopi merger …` as a line head)
// — the shape the restructure removed. A pointer row that merely NAMES the verb
// in a sentence stays allowed.
func ledgerTeachesServiceBlock(usageBody string) bool {
	for _, line := range strings.Split(usageBody, "\n") {
		for _, svc := range []string{"worker", taskverbs.Merger, taskverbs.Serve} {
			if strings.HasPrefix(line, "  gopi "+svc+" ") || strings.HasPrefix(line, "  gopi "+svc+"\n") {
				return true
			}
		}
	}
	return false
}

// TestLedgerGroupTeachesNoServiceBlock applies the same predicate to the REAL
// family usage: no `gopi worker`/`gopi merger` synopsis line survives there.
func TestLedgerGroupTeachesNoServiceBlock(t *testing.T) {
	if ledgerTeachesServiceBlock(familyUsageBody(t)) {
		t.Error("the family usage carries a service SYNOPSIS block (`gopi worker …` / `gopi merger …`) — a service is documented by its own page (`gopi merger -h` / `gopi worker -h`); the ledger group may only point at it")
	}
}

// familyUsageBody returns the tasksUsage raw-string body (the ledger group's
// help text) — read from the source, so the gate tests what ships.
func familyUsageBody(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("cli_tasks.go")
	if err != nil {
		t.Fatalf("read cli_tasks.go: %v", err)
	}
	src := string(raw)
	const marker = "const tasksUsage = `"
	i := strings.Index(src, marker)
	if i < 0 {
		t.Fatal("tasksUsage const not found in cli_tasks.go")
	}
	rest := src[i+len(marker):]
	end := strings.Index(rest, "`")
	if end < 0 {
		t.Fatal("tasksUsage raw string unterminated in cli_tasks.go")
	}
	return rest[:end]
}

// TestNoHelpTextIsDuplicatedAcrossPages is the epic's invariant applied to the
// help TEXT itself (the epigraph line "grep-ing any documented flag should
// land in exactly one place"). A verb's synopsis and flag semantics must occur
// in ONE page: the two pages of one function are how the drift starts — the
// copy that is not updated becomes a lie (the merger control flags were
// documented on BOTH `gopi merger -h` and `gopi task -h` until this change,
// and the family copy taught `--daemon`/`--status`/`--stop` for a `gopi task
// worker` spelling that never accepted them).
//
// The check is DELIBERATELY narrow so it cannot become noise: only the FLAG
// SEMANTICS lines of each page are compared (synopsis lines start with two
// spaces and `gopi `; semantics are the deeper-indented prose), and the
// comparison runs on the three big pages that share vocabulary — the ledger
// group, the merger page and the worker page. A whole-line hit on another
// page is the defect; the pages legitimately share short words like `--db`.
func TestNoHelpTextIsDuplicatedAcrossPages(t *testing.T) {
	// THE MERGER PAGE LEFT THIS SET WITH THE RETIRED DAEMON (2026-09-19): the
	// page documented the resident merger's flags, and both went with
	// devdoc/merger-retired.md. The gate is about pages that still exist and
	// still share vocabulary, so the two that remain are compared — and when the
	// merger's own re-development lands a page again, it belongs back here.
	pages := map[string]string{
		"tasksUsage":      familyUsageBody(t),
		"workerUsageHead": workerUsageHead,
	}
	// Semantic lines only: indentation >= 6 spaces and long enough to carry a
	// sentence (a shared word is not a shared contract).
	var dupes int
	names := make([]string, 0, len(pages))
	for name := range pages {
		names = append(names, name)
	}
	sort.Strings(names)
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := pages[names[i]], pages[names[j]]
			for _, line := range semanticHelpLines(a) {
				if strings.Contains(b, line) {
					dupes++
					t.Errorf("%s and %s both carry the help line %q — one function, one help text: keep it on the owner page and POINT at it from the other", names[i], names[j], strings.TrimSpace(line))
					if dupes > 20 {
						t.Fatal("too many duplicates — the predicate is probably matching prose that is not a contract")
					}
				}
			}
		}
	}
}

// semanticHelpLines returns the semantics lines of a help page: indented at
// least six spaces (deeper than a synopsis head's two) and at least 40 bytes
// long, so a shared short flag word never counts.
func semanticHelpLines(page string) []string {
	var out []string
	for _, line := range strings.Split(page, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) < 6 || len(trimmed) < 40 {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// TestNoHelpTextIsDuplicatedAcrossPagesRedProof proves the duplicate detector
// is not vacuous: it must flag a line that the pre-restructure pages really
// shared, and must NOT flag the pointer shape (Rule 10.4 — a gate you have
// never seen fail is a gate you cannot trust).
func TestNoHelpTextIsDuplicatedAcrossPagesRedProof(t *testing.T) {
	// The pre-restructure FAMILY page carried the merger's semantics, and the
	// merger page carried the same sentence — a real historical duplicate.
	sharedLine := `      Act on the RESIDENT merger from a separate process: --stop asks it to`
	ledgerPage := "  gopi task merger --stop | --restart | --start [--force] [--wait D]\n" + sharedLine + "\n"
	mergerPage := "  gopi merger --stop\n" + sharedLine + "\n"
	hits := 0
	for _, line := range semanticHelpLines(ledgerPage) {
		if strings.Contains(mergerPage, line) {
			hits++
		}
	}
	if hits == 0 {
		t.Fatal("the detector missed a verbatim shared semantics line — the predicate (indent>=6, len>=40) does not cover the shape this epic removed")
	}
	// The correct shape: a POINTER sentence, which names the other page but
	// restates no contract line.
	pointer := "Deprecated spellings keep dispatching; their contract is on 'gopi merger -h'."
	if strings.Contains(mergerPage, pointer) {
		t.Fatal("the pointer shape must not be a duplicate of any page line")
	}
	if got := len(semanticHelpLines(pointer)); got != 0 {
		t.Errorf("a pointer line must not be classified as a semantics line, got %d", got)
	}
	// And the live pages must be non-empty for the real test to mean anything.
	// Floor 10: the family page (176) clears it; the worker page carries only 2
	// semantics lines by design (its page is a compact synopsis + flag list the
	// restructure did not touch), so the floor is asserted where it is
	// meaningful — the gate would otherwise fail on the page with the least
	// prose, which is not the page the epic rewrote.
	for name, page := range map[string]string{"tasksUsage": familyUsageBody(t), "workerUsageHead": workerUsageHead} {
		floor := 10
		if name == "workerUsageHead" {
			floor = 2
		}
		if len(semanticHelpLines(page)) < floor {
			t.Errorf("%s yielded fewer than %d semantics lines — the extraction broke, so the duplicate test proves nothing", name, floor)
		}
	}
}
