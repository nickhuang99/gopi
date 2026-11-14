package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// memoryUsage is the help text for the `gopi memory` group. It is printed
// when the group is invoked without a subcommand, with an unknown
// subcommand, or the user passes -h/--help.
const memoryUsage = `GoPi memory — the memory tool family as one subcommand group.

Usage:
  gopi memory <subcommand> [args]

Subcommands:
  gopi memory search "<query>" [--limit N] [--intent X] [--db PATH]
      Search the distilled knowledge base, printing matching records exactly
      as the tool renders them. tokens=N is how many query terms the hit
      covers — use it to spot low-quality rows and demote them.

      The first line is now=<RFC3339>, the instant every age= below it was
      measured against; a hit then says stored=<when the row was WRITTEN>
      age=<N>d [about=<YYYY-MM-DD its own text states, omitted when none>].

      Two sections, ranked separately: [distilled] is the knowledge base above;
      [session] is the round-note index of past RUNS (session_id, message id,
      an evidence slice with omitted stretches counted). Widen with
      gopi session notes "<query>" / gopi session show <session-id>.

  gopi memory store --topic T --question Q --answer A
                    [--keywords "k1,k2"] [--source S] [--db PATH]
                    [--session-id ID --round-index N]
      Store a distilled lesson. Prints "Memory stored (id=N)." or the
      deduped signal, exactly as the tool renders them.
      --session-id / --round-index record WHERE the lesson came from (the
      session and that session's own 1-based round number), so a reader can
      walk back to the turn that produced it. Both are optional; the agent
      loop stamps them by itself when it stores, and a plain "gopi memory
      store" records no provenance rather than a made-up one.

  gopi memory get <id> [--db PATH]
      Inspect one record in full (the id from a memory search result).

  gopi memory list [--limit N] [--db PATH]
      List the N most recent records (id / tier / topic / question) so every
      record's id is discoverable without a search query — the enumeration
      entry point of the maintenance loop (list → memory_get <id> →
      memory_update / memory_delete). Default N = 20.

  gopi memory update <id> [--tier hot|warm|cold] [--topic T] [--question Q]
                    [--answer A] [--source S] [--confidence 0..1]
                    [--keywords "k1,k2"] [--db PATH]
      Edit one record. At least one field is required. --tier cold demotes a
      low-quality row so it is no longer 1st tier to return (it only fills
      slots warm/hot rows cannot) — without deleting the knowledge.

  gopi memory delete <id> [--db PATH]
      Permanently remove one record (and its FTS index entries).

  gopi memory feedback <id> [--useful|--useless|--unused]
                        [--reason "text"] [--task ID] [--db PATH]
      Record an explicit quality vote on a record (the memory evolution
      loop): useful = adopted/helped, useless = contradicted/failed/
      misleading, unused = seen but not used (neutral). --reason is kept in
      the audit trail; --task tags the vote for session dedup (one vote per
      record per task). Votes only steer ranking — tier changes stay manual.

  gopi memory feedback --list [--memory-id N] [--db PATH]
      Audit: records that received feedback, with decayed vote masses and
      the Beta posterior mean (fb=mean(votes)).

  gopi memory feedback --suggest-cold [--db PATH]
      List cold-demotion candidates (useless ≥ 3, useful = 0) as SUGGESTIONS
      only — demotion is executed manually via gopi memory update --tier cold.

  gopi memory feedback --recompute [--db PATH]
      Rebuild the materialized vote columns from the append-only event
      history (the feedback layer is a pure function of its events).

  gopi memory converge-keywords [--dry-run] [--db PATH]
      Collapse the duplicated "keywords: …" tail blocks a pre-7139b28 writer
      left on a row's answer into ONE, keeping the LAST block (the current
      breadcrumbs) and leaving the body byte-identical. Writes only the answer
      column, in one transaction, and is safe to re-run. --dry-run reports the
      before/after histograms and the scan/change split, writing nothing.

  gopi memory backfill-tokens [--dry-run] [--db PATH]
      Re-derive the jieba_token index column of every existing row with the
      CURRENT CJK segmenter, so rows written before the segmenter changed are
      addressable by the words a query is segmented into. Writes only that
      column (prose, tier and counters are untouched), skips rows already
      current, and is safe to re-run after an interruption. --dry-run reports
      the scan/change split and writes nothing. Run it once after upgrading a
      binary that changed the segmenter — it is not scheduled by anything.

  gopi memory sync --peer <alias> [--db PATH] [--dry-run]
                   [--push | --pull] [--timeout SECONDS]
  gopi memory sync --serve-assets [--db PATH]
      MANUAL unique-only session with a declared memory_peers peer: each side
      ends up with the superset (identity = the exact question; an existing
      row is never touched). --push / --pull name one direction, neither =
      BIDIRECTIONAL; --dry-run sizes the patch without contacting the peer;
      --serve-assets is the PEER side (ssh'd, never typed). One invocation,
      one session, exit — nothing schedules it; 'memory backup' is the OTHER
      door (snapshot + one-way ship, no import), not a peer merge.
  gopi memory backup [--dir PATH] [--db PATH] [--peer ALIAS] [--no-ship]
                     [--name FILE] [--serve]
      Snapshot the LIVE ledger (SQLite's own VACUUM INTO — never a file copy),
      prove the artifact openable and consistent READ-ONLY, report path + size
      + sha256, and (unless --no-ship) stream it to a declared memory_peers
      peer. --serve is the PEER side of that leg, ssh'd by an initiator and
      never run by a human. RESTORING is documented, never performed here:
      'gopi memory backup -h'.

  gopi memory provenance [--shape|--backfill|--reclassify|--counts|--latent]
                         [--db PATH]
  gopi memory provenance [--latent-candidates | --harvest-latent
                         | --latent-crosscheck | --latent-calibrate]
      Rule 16 provenance governance: measure and classify each knowledge
      row's ORIGIN so a downstream task can resolve it without re-doing
      the work. Reads the free-text "sources" column and writes the
      additive "source_type" column — "sources" itself is never modified.
        --shape      (default) data-driven distribution of row origins over
                     the live DB — read-only, re-runnable, sums to the row
                     population.
        --backfill   classify rows whose source_type is still empty, in ONE
                     transaction. Idempotent: a second run reports filled=0.
        --reclassify re-derive EVERY row's source_type from its current
                     "sources" text (repair after a sources edit).
        --counts     the persisted per-type counts — the verification query.
        --latent     of the rows recording NO origin, how many still name an
                     artifact in their own text versus how many name nothing
                     anywhere (the strong-sense untraceable).
        --latent-candidates  the re-runnable candidate population + the SQL to
                             reproduce it without this binary (read-only).
        --latent-calibrate   resolve-rate of the RECORDED paths vs the
                             HARVESTED heading paths — the rule's justification.
        --harvest-latent     record the verdicts. Idempotent: a second run
                             reports untouched=N, harvested=0.
        --latent-crosscheck  the harvested rows as a manual-check table.

` + memoryBrewSynopsisBlock + `

` + memoryCaptureStatsUsageLine + memoryCaptureStatsUsageProse + `
The measurements and decisions behind these synopses — the documented RESTORE
procedure and the latent-provenance rates — are in
devdoc/memory-page-background.md.

Common flags:
  --db PATH   override the knowledge base path (default: ~/.gopi/memory.db)

  A --db PATH THAT DOES NOT EXIST IS NOT AN EMPTY KNOWLEDGE BASE. The
  READ-ONLY subcommands (search, get, list, provenance's reporting modes,
  feedback --list / --suggest-cold / --recompute, converge-keywords --dry-run,
  backfill-tokens --dry-run) REFUSE an explicitly named path that is absent:
  they exit non-zero, name both the path you wrote and the ledger that is
  active, and create nothing. A WRITER (store, update, delete, feedback <id>
  with a rating, provenance --backfill, backfill-tokens / converge-keywords
  without --dry-run) keeps the ability to create a ledger, and prints
  "created a new knowledge base at <path>" when it does — so an empty knowledge
  base and a freshly created one are never the same sentence.
  The capture-stats sub-verb opens no ledger at all, and refuses --db.

Memory-search flags:
  --limit N   max records to return (default 5)
  --intent X  routing hint for retrieval (optional)

The legacy flat verbs (gopi memory_search, memory_store, memory_get,
memory_update, memory_delete, memory_feedback, memory_feedback_list) still
work but print a deprecation note pointing here — the grouped form is the
canonical surface (tab-completion friendly: gopi memory <TAB>).
`

// memoryLegacyAliases maps the pre-group flat verbs to their grouped form
// (memory_search → memory search). The flat verbs are kept as thin
// aliases so scripts and muscle memory keep working — they print a
// one-line deprecation note on stderr before running identically.
//
// These are cmd/agent USER-FACING CLI spellings, not LLM tool wire names.
// The four memory_* keys here were the PRE-MERGE LLM tool names
// (memory_search / memory_store / memory_update / memory_feedback — before
// the tool-family merge folded them into the single `memory` tool with an
// action parameter); they kept working as flat CLI verbs through the group
// surface. The toolnames constants they once referenced are now GONE
// (merged into toolnames.ToolMemory), so the keys are literal by design
// (Rule 1: no ToolMemorySearch-style constant exists for them any more) —
// same as the maintenance verbs memory_get/memory_list/memory_delete and
// memory_feedback_list, which never had an LLM tool counterpart (cmd/agent
// maintenance surfaces calling tools.Memory* helpers directly).
var memoryLegacyAliases = map[string]string{
	"memory_search":        "search",
	"memory_store":         "store",
	"memory_get":           "get",
	"memory_list":          "list",
	"memory_update":        "update",
	"memory_delete":        "delete",
	"memory_feedback":      "feedback",
	"memory_feedback_list": "feedback", // + --list is implied
}

// isMemorySubcommand reports whether argv begins with the `memory` group
// verb (or one of its legacy flat-verb aliases). When it does it runs the
// subcommand and returns true (the caller must not treat the remaining args
// as an input file). A bare `gopi memory` (no subcommand) prints the group
// usage — it must NOT fall through to the agent loop.
func isMemorySubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case CliVerbMemory:
		runMemoryGroup(args[1:])
		return true
	default:
		// Legacy flat-verb alias: `gopi memory_feedback_list` etc.
		if sub, ok := memoryLegacyAliases[args[0]]; ok {
			rest := args[1:]
			if args[0] == "memory_feedback_list" {
				// The list audit form never existed as a real dispatch —
				// only as dead help text. Route it to the group's audit
				// subcommand, keeping --memory-id working.
				rest = append([]string{"--list"}, rest...)
			}
			fmt.Fprintf(os.Stderr, "note: gopi %s is deprecated — use gopi memory %s\n", args[0], sub)
			runMemoryGroup(append([]string{sub}, rest...))
			return true
		}
		return false
	}
}

// memorySubcommandNames is the canonical `gopi memory` second level
// (tab-completion's second candidate set).
// memoryProvenanceVerb is the `gopi memory provenance` subcommand spelling
// (the user-facing CLI verb), and memoryProvenanceOp is the internal
// operation key the dispatcher threads into runMemorySubcommand — the same
// verb/op split the other subcommands use (cf. "search" → "memory_search").
//
// Rule 1b: both are named constants — the dispatch switch, the usage text and
// the parse/memberOf tables all reference these, never the bare literals.
const (
	memoryProvenanceVerb = "provenance"
	memoryProvenanceOp   = "memory_provenance"
)

// memoryProvenanceModes are the mutually exclusive `gopi memory provenance`
// modes: the read-only shape report, the additive backfill, the forced
// re-derivation, and the persisted-counts verification. They are the flag
// spellings, so they double as the flag names.
var memoryProvenanceModes = []string{
	memoryProvenanceDefaultMode, memoryProvenanceBackfillMode,
	memoryProvenanceReclassifyMode, memoryProvenanceCountsMode,
	memoryProvenanceLatentMode,
	memoryProvenanceHarvestFlag, memoryProvenanceCandidatesFlag,
	memoryProvenanceCrossCheckFlag, memoryProvenanceCalibrateFlag,
}

// The latent-provenance modes are named constants (Rule 1b): the mode table,
// the dispatch switch and the usage text all reference these, never a bare
// literal. They are spelled as the flag the user types, like the modes above.
const (
	memoryProvenanceHarvestFlag    = "harvest-latent"
	memoryProvenanceCandidatesFlag = "latent-candidates"
	memoryProvenanceCrossCheckFlag = "latent-crosscheck"
	memoryProvenanceCalibrateFlag  = "latent-calibrate"
)

// memoryProvenanceDefaultMode is the mode a bare `gopi memory provenance` runs.
// The READ-ONLY shape report — a mutating default would be a footgun on a
// production DB an agent is reading.
const memoryProvenanceDefaultMode = "shape"

// The two provenance modes that WRITE. They are named constants because the
// ledger guard (cli_memory_ledger_guard.go) has to tell them from the reporting
// modes: `provenance --backfill` may create a ledger, `provenance` may not
// (Rule 1b/13 — one spelling, read by both).
const (
	memoryProvenanceBackfillMode   = "backfill"
	memoryProvenanceReclassifyMode = "reclassify"
	memoryProvenanceCountsMode     = "counts"
	memoryProvenanceLatentMode     = "latent"
)

// memoryFeedbackVoteMode is `gopi memory feedback <id> --useful|--useless|--unused`
// — the only WRITING feedback mode (the audit modes --list / --suggest-cold /
// --recompute report and must never create a ledger). Named for the same reason
// as the pair above: the guard reads it.
const memoryFeedbackVoteMode = "vote"

// runMemoryProvenance executes one provenance mode and renders its report.
// Split from the dispatch switch so the rendering is unit-testable without a
// process (no os.Exit).
func runMemoryProvenance(mode string) (string, error) {
	switch mode {
	case "shape":
		rep, err := tools.MemoryProvenanceShape()
		if err != nil {
			return "", err
		}
		return strings.Join(tools.ProvenanceShapeLines(rep), "\n"), nil
	case memoryProvenanceBackfillMode:
		res, err := tools.MemoryProvenanceBackfill()
		if err != nil {
			return "", err
		}
		return renderProvenanceBackfill(res), nil
	case memoryProvenanceReclassifyMode:
		res, err := tools.MemoryProvenanceReclassify()
		if err != nil {
			return "", err
		}
		return renderProvenanceBackfill(res), nil
	case "counts":
		counts, err := tools.ProvenanceCounts()
		if err != nil {
			return "", err
		}
		return renderProvenanceCounts(counts), nil
	case "latent":
		rep, err := tools.MemoryProvenanceLatent()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"unknown rows: %d  latent (a path in the row's own text): %d  no path anywhere: %d",
			rep.UnknownRows, rep.LatentRecoverable, rep.LatentNone), nil
	case memoryProvenanceCandidatesFlag:
		rep, err := tools.MemoryProvenanceLatentCandidates()
		if err != nil {
			return "", err
		}
		// The SQL is printed UNINDENTED and starting at column 0 so the report
		// is directly pipeable into sqlite3 (`… --latent-candidates | sed -n
		// '/^SELECT/,$p' | sqlite3 -readonly /home/user/.gopi/memory.db`) — the
		// re-runnability the acceptance criteria ask for.
		return renderLatentResult(rep) + "\n\nreproduce without this binary:\n" +
			tools.LatentCandidateSQL(), nil
	case memoryProvenanceHarvestFlag:
		rep, err := tools.MemoryProvenanceLatentHarvest()
		if err != nil {
			return "", err
		}
		return renderLatentResult(rep), nil
	case memoryProvenanceCrossCheckFlag:
		rows, err := tools.MemoryProvenanceLatentRows(0)
		if err != nil {
			return "", err
		}
		return renderLatentRows(rows), nil
	case memoryProvenanceCalibrateFlag:
		rep, err := tools.CalibrateProvenanceLatent()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"RECORDED sources (source_type=local_path): %d/%d resolve on disk (%.0f%%)\n"+
				"HARVESTED heading paths (empty sources):   %d/%d resolve on disk (%.0f%%)",
			rep.RecordedResolving, rep.RecordedRows, rep.RecordedResolveRate(),
			rep.HeadingResolving, rep.HeadingRows, rep.HeadingResolveRate()), nil
	}
	return "", fmt.Errorf("unknown provenance mode %q", mode)
}

// renderProvenanceBackfill renders one backfill/reclassify pass. The unchanged
// count is printed explicitly — it is the idempotency witness a second run must
// show as equal to the row population.
func renderProvenanceBackfill(res *tools.ProvenanceBackfillResult) string {
	return fmt.Sprintf(
		"scanned=%d filled=%d unchanged=%d unknown=%d",
		res.Scanned, res.Filled, res.Unchanged, res.Unknown)
}

// renderLatentResult renders one latent-provenance pass. Every counter is
// printed — including the ones that show work NOT done (unharvestable), because
// the honest residual is the deliverable, not a number to bury.
// renderLatentResult renders one latent-provenance pass. Every counter is
// printed — including the ones that show work NOT done (unharvestable), because
// the honest residual is the deliverable, not a number to bury. The residual is
// broken into its bases by the rule's own predicate, so a reader can see WHY
// each unlabelled row was refused instead of taking a total on faith.
func renderLatentResult(rep *tools.LatentProvenanceResult) string {
	return fmt.Sprintf(
		"rows=%d recorded=%d empty_sources=%d heading_shaped=%d\n"+
			"  harvestable(rule can defend)=%d  harvested(written this pass)=%d  resolve on disk=%d\n"+
			"  unharvestable(no origin, no defensible signal)=%d\n"+
			"    = no path token at all=%d + token not file-shaped=%d + mentioned only (subject, not origin)=%d + declared but not repeated=%d + bare filename declared+repeated=%d\n"+
			"  (the bare-filename rung is REFUSED: it names a file the next task cannot locate — a basename is not addressable)\n"+
			"  rows written this pass=%d  already labelled before this pass=%d (idempotency: a 2nd run writes 0)",
		rep.Total, rep.Recorded, rep.EmptySources, rep.HeadingShaped,
		rep.Harvestable, rep.Harvested, rep.ResolvesOnDisk, rep.Unharvestable,
		rep.ResidualNoPathToken, rep.ResidualTokenNotFileShape, rep.ResidualMentionOnly,
		rep.ResidualDeclaredNoRepeat, rep.ResidualBareFilename,
		rep.Written, rep.Untouched)
}

// renderLatentRows renders the harvested rows as the manual cross-check table:
// the harvested path, the marker, the recorded-sources column it must NOT have
// overwritten, and whether the path exists on disk.
func renderLatentRows(rows []tools.LatentProvenanceRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "harvested rows (manual cross-check) — %d of the requested sample\n", len(rows))
	for _, r := range rows {
		onDisk := "MISSING"
		if r.Resolves {
			onDisk = "on-disk"
		}
		// The CONTENT check is printed next to the path check: a file that
		// exists is not yet evidence that the row is ABOUT it.
		agreement := "content: (file unreadable)"
		if r.AgreementChecked {
			agreement = fmt.Sprintf("content: %d/%d identifiers of the answer found in the file",
				r.AgreementFound, r.AgreementTotal)
		}
		fmt.Fprintf(&b, "  id=%-5d %-8s %-34s recorded=%-10q %s\n      %s\n      topic: %s\n",
			r.ID, r.Provenance, r.LatentType+":"+r.Evidence, r.Recorded, onDisk,
			agreement, truncateRunes(r.Topic, 90))
	}
	return strings.TrimRight(b.String(), "\n")
}

// truncateRunes cuts s to at most n runes (byte slicing would corrupt CJK).
func truncateRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}

// renderProvenanceCounts renders the persisted source_type distribution (the
// verification query) with its population total, in deterministic label order.
func renderProvenanceCounts(counts map[string]int) string {
	labels := make([]string, 0, len(counts))
	total := 0
	for k, v := range counts {
		labels = append(labels, k)
		total += v
	}
	sort.Strings(labels)
	var b strings.Builder
	fmt.Fprintf(&b, "total rows: %d\n", total)
	for _, l := range labels {
		label := l
		if label == "" {
			label = "(empty)"
		}
		fmt.Fprintf(&b, "  %-12s %5d\n", label, counts[l])
	}
	return strings.TrimRight(b.String(), "\n")
}

var memorySubcommandNames = []string{
	"search", "store", "get", "list", "update", "delete", "feedback",
	memoryBackfillVerb, memoryConvergeKeywordsVerb,
	CliVerbBrew, memoryProvenanceVerb, tools.LedgerBackupVerb, tools.MemorySyncVerb,
	// memoryCaptureStatsVerb is the READ-ONLY survey of the auto-memory response
	// gate (cli_memory_capture_stats.go): the measure that keeps the gate's
	// thresholds re-derivable on any host (Rule 38), and it belongs to this
	// family because the numbers it prints are about the knowledge base's own
	// intake.
	memoryCaptureStatsVerb,
}

// runMemoryGroup dispatches one `gopi memory <subcommand>` invocation:
// search|store|get|list|update|delete|feedback|sync. The per-subcommand runner
// handles its own -h/--help (runMemorySubcommand prints the group usage;
// runMemorySync prints the sync-specific head — the unique-only /
// --serve-assets contract).
//
// NOTE (toolnames boundary): the string args threaded into runMemorySubcommand
// here ("memory_search", "memory_store", …) are the INTERNAL memory-operation
// key, NOT a CLI mirror of the LLM wire name — they select a Go branch that
// then calls a typed tools.MemoryX function. memory_get / memory_delete /
// memory_list have NO toolnames constant (they are cmd/agent maintenance
// verbs, not LLM tools, cf. tools.MemoryGet/ListRecent/Delete, and
// memory_delete/memory_list also appear in cli_log toolFamilies as such), so
// this dispatch namespace is inherently mixed tool+CLI verb and stays as one
// consistent operation-key vocabulary. The tool-family merge retired the
// memory_* LLM wire names (now the single `memory` tool with actions), so
// the memoryLegacyAliases KEYS above are literal CLI spellings by design.
func runMemoryGroup(args []string) {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, memoryUsage)
		os.Exit(1)
	}
	sub := args[0]
	if sub == "-h" || sub == "--help" {
		fmt.Print(memoryUsage)
		os.Exit(0)
	}
	switch sub {
	case "search":
		runMemorySubcommand("memory_search", args[1:])
	case "store":
		runMemorySubcommand("memory_store", args[1:])
	case "get":
		runMemorySubcommand("memory_get", args[1:])
	case "list":
		runMemorySubcommand("memory_list", args[1:])
	case "update":
		runMemorySubcommand("memory_update", args[1:])
	case "delete":
		runMemorySubcommand("memory_delete", args[1:])
	case "feedback":
		runMemorySubcommand("memory_feedback", args[1:])
	case memoryBackfillVerb:
		runMemoryBackfillTokens(args[1:])
	case memoryConvergeKeywordsVerb:
		// The one-shot DATA migration for the duplicated "keywords: …" tails
		// the pre-7139b28 writer left behind (cli_memory_converge_keywords.go).
		runMemoryConvergeKeywords(args[1:])
	case CliVerbBrew:
		// The brew pipeline belongs to the KNOWLEDGE dimension, not to the
		// top-level namespace (epic verb-restructure, 2026-10-05). ONE
		// implementation, ONE runnable spelling: the entry point is the same
		// runBrewSubcommand, so its -h page, its flag set and its exit codes
		// (0 ok / 1 usage / 2 partial) are that verb's, never a second copy
		// (Rules 13/14). The flat word is refused at argv[0] (measured
		// 2026-11-06: `gopi brew --dir X` exits 1, "`brew` is RETIRED").
		runBrewSubcommand(args[1:])
	case tools.MemorySyncVerb:
		// The MANUAL cross-node knowledge sync (cli_memory_sync.go). It was
		// deleted with the cross-node sync CLI family in 5677cc6e and restored
		// 2026-11-06 while every one of its unattended drivers — the tick, the
		// store trigger, the --every loop — stays retired.
		runMemorySync(args[1:])
	case tools.LedgerBackupVerb:
		runMemoryBackup(args[1:])
	case memoryCaptureStatsVerb:
		// The survey owns its own argv (cli_memory_capture_stats.go): it is not
		// a memory_* operation over the ledger, so it never reaches
		// parseMemoryArgs — there is no --db to thread and no row to read.
		runMemoryCaptureStats(args[1:])
	case memoryProvenanceVerb:
		runMemorySubcommand(memoryProvenanceOp, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "gopi memory: unknown subcommand %q — want %s\n\n%s",
			sub, strings.Join(memorySubcommandNames, "|"), memoryUsage)
		os.Exit(1)
	}
}

// memoryFlagSet is a small hand-rolled flag parser for the two memory
// subcommands (we avoid the stdlib flag package because its single-dash
// conventions and error handling don't match the tool-style `--name value`
// signature).
type memoryFlagSet struct {
	values map[string]string // flag name (no dashes) → raw value
	order  []string
	seen   map[string]bool
	multi  map[string][]string // flag name → every occurrence, in order (repeatable flags)

	// seenRepeat is the repeatable-flag whitelist: flags declared via
	// repeat() accumulate in multi; everything else keeps last-wins.
	seenRepeat map[string]bool

	// closed is the STRICT-mode whitelist: a caller that declares its flags
	// via expect() makes an UNDECLARED flag a hard error. Default false —
	// every existing caller keeps the permissive behaviour.
	closed bool

	// expected is the declared-flag whitelist used only in strict mode.
	expected map[string]bool

	// optionalValueFlags are flags whose value is OPTIONAL: the bare spelling
	// is a complete instruction, and a following token that starts with "--"
	// is the NEXT flag rather than their value. Without this the parser's
	// blanket "a bare flag takes the next token as its value" rule makes the
	// bare spelling inexpressible — the flag can only be given WITH a value,
	// which is exactly how `gopi task sync --peers` came to be refused while
	// its own refusal text advertised "--peers (all)".
	//
	// The grammar is deliberately narrower than the general case (a list flag
	// splitting on a separator), because the value this fixes is the COMMA
	// LIST: a bare-optional flag whose value is a single token. Widening it
	// to `--peers hp` (a space-separated token) would silently redefine
	// "every declared peer" as "the peer hp" — a silent reinterpretation,
	// which is the one outcome the sync group refuses by name instead.
	optionalValueFlags map[string]string

	// equalsFlags records the flags whose value arrived ATTACHED (`--name=v`)
	// rather than as the following token. The two spellings are NOT
	// interchangeable for an optional-value flag: `--peers=a,b` states its
	// value explicitly, while `--peers a,b` leaves the scanner deciding
	// whether the next token is that flag's value or a stray word — and a
	// caller that refuses the ambiguous shape by name has to be able to SEE
	// which of the two the operator typed.
	equalsFlags map[string]bool

	// booleanFlags are flags that take NO value (--server, --client, …).
	// Without this the parser's "a bare flag takes the next token as its
	// value" rule silently eats the following argument: `--client clip.wav`
	// would record from the device and never see the path, and
	// `voice_input --server` as the last argument would leave server=false.
	booleanFlags map[string]bool
}

func newMemoryFlagSet() *memoryFlagSet {
	return &memoryFlagSet{
		values:       map[string]string{},
		seen:         map[string]bool{},
		multi:        map[string][]string{},
		seenRepeat:   map[string]bool{},
		booleanFlags: map[string]bool{},

		optionalValueFlags: map[string]string{},
		equalsFlags:        map[string]bool{},
	}
}

// boolean declares name as a valueless flag, so parse() never consumes the
// token after it.
func (f *memoryFlagSet) boolean(name string) { f.booleanFlags[name] = true }

// expect declares name as the ONLY kind of flag this invocation accepts and
// switches the parser into STRICT mode: an undeclared `--flag` is then a hard
// error instead of being silently recorded and ignored (parse()'s own doc
// claim, which only strict mode makes true). A sub-verb whose flags all mutate
// something must opt in — `gopi memory backup --no-shipp` (one keystroke) would
// otherwise take and SHIP a full snapshot the operator never asked for. A flag
// is declared expected by whichever of boolean()/repeat()/expect() introduces
// it; boolean()/repeat() do not imply strictness on their own.
func (f *memoryFlagSet) expect(name string) {
	if f.expected == nil {
		f.expected = map[string]bool{}
	}
	f.closed = true
	f.expected[name] = true
}

// optionalValue declares name as a flag whose VALUE IS OPTIONAL: `--name` on
// its own is a complete instruction (get() stays "" — the caller reads has()),
// while `--name value` and `--name=value` keep carrying their value.
//
// TWO NARROWING RULES, so a bare spelling cannot eat a neighbour:
//   - a value may never start with "--" (the next flag wins);
//   - the value must be a SINGLE argument — no whitespace, no comma — because
//     the caller's bare semantic is "ALL", and folding a token that reads as a
//     LIST into that branch would answer a different question than the one the
//     operator asked.
//
// A value the rules reject is REFUSED by name (parse returns the error), never
// reinterpreted: "the operator typed something I do not understand" and "the
// operator typed nothing" are different instructions, and silently choosing
// one for the other is the defect class this option exists to fix.
func (f *memoryFlagSet) optionalValue(name string) { f.optionalValueFlags[name] = name }

// optionalValueBare reports whether the flag was given WITHOUT a value — the
// spelling the option above declares. get() is "" for it, so a caller whose
// bare spelling carries its own meaning reads THIS, never "get() == ”" (which
// is also true of a flag that never appeared, and of an empty value).
func (f *memoryFlagSet) optionalValueBare(name string) bool {
	_, ok := f.optionalValueFlags[name]
	return ok && f.seen[name] && f.values[name] == "" && !f.equalsFlags[name]
}

// valueAttached reports whether the flag's value arrived in the `--name=value`
// spelling. A caller that must tell "the operator stated this value" from "the
// scanner took the next token as this value" reads it; the two spellings look
// identical in get() alone.
func (f *memoryFlagSet) valueAttached(name string) bool { return f.equalsFlags[name] }

// repeat declares name as a repeatable flag: every `--name value` occurrence
// accumulates in argument order (list()); the last value also lands in
// get() (the conventional single-value read still works for code that does
// not care about repeats). Flags NOT declared repeatable keep the parser's
// original last-wins semantics — zero behavior change for existing callers.
func (f *memoryFlagSet) repeat(name string) {
	f.seenRepeat[name] = true
	if _, ok := f.multi[name]; !ok {
		f.multi[name] = []string{}
	}
}

// list returns every occurrence of a repeatable flag, in argument order
// (nil when the flag never appeared). Repeatable flags are declared via
// repeat() before parse; a non-repeatable flag has no list entry.
func (f *memoryFlagSet) list(name string) []string { return f.multi[name] }

// parse consumes args as `flag value` / `flag=value` pairs and returns any
// positional (non-flag) arguments in order. Unknown flags produce an error.
func (f *memoryFlagSet) parse(args []string) ([]string, error) {
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			positional = append(positional, arg)
			continue
		}
		name := arg
		value := ""
		hasValue := false
		if eq := strings.Index(arg, "="); eq >= 0 {
			name = arg[:eq]
			value = arg[eq+1:]
			hasValue = true
		} else {
			name = arg
		}
		name = strings.TrimPrefix(name, "--")
		if name == "" {
			return nil, fmt.Errorf("empty flag")
		}
		if f.closed && !f.expected[name] {
			return nil, fmt.Errorf("unknown flag --%s", name)
		}
		if _, known := f.seen[name]; !known {
			f.order = append(f.order, name)
		}
		f.seen[name] = true
		if _, isBool := f.booleanFlags[name]; isBool {
			// A valueless flag never claims the next argument.
			f.values[name] = ""
		} else if _, isOptional := f.optionalValueFlags[name]; isOptional && !hasValue {
			// OPTIONAL-VALUE flag, bare spelling: the flag is a complete
			// instruction on its own (the value stays "" — the caller reads
			// optionalValueBare). The value slot is claimed ONLY by a token
			// that is not itself a flag: `--peers a,b --db p` gives --db its
			// own value, and `--peers --status` is the bare spelling followed
			// by the next flag. WHICH shapes a caller accepts inside that
			// token is the CALLER's judgement (the sync parser refuses the
			// ambiguous shape by name, cli_tasks_sync.go): the scanner decides
			// where a value ENDS, not what it means.
			//
			// The assignment goes to the LOCAL value, not to f.values: the
			// store below is unconditional, so writing the map here would be
			// overwritten by the empty local — the token would be consumed
			// and DISCARDED (measured while writing this: `--peers hp`
			// parsed as the bare spelling with hp silently dropped).
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], argvFlagPrefix) {
				value = args[i+1]
				i++
			}
		} else if !hasValue {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				f.values[name] = ""
			} else {
				value = args[i+1]
				i++
			}
		}
		f.values[name] = value
		if hasValue {
			f.equalsFlags[name] = true
		}
		// Repeatable flags accumulate every occurrence (the CLI exposes the
		// multi-value flag set through repeat()); non-repeatable flags keep
		// the last-wins value in values as before.
		if _, isMulti := f.seenRepeat[name]; isMulti {
			f.multi[name] = append(f.multi[name], value)
		}
	}
	return positional, nil
}

func (f *memoryFlagSet) get(name string) string { return f.values[name] }
func (f *memoryFlagSet) has(name string) bool   { return f.seen[name] }

// optionalValueShapeError is DEFINED BY THE FLAG GROUP THAT OWNS THE GRAMMAR —
// syncPeersValueShape in cli_tasks_sync.go, beside the sync family's constants
// and the refusal text operators read. What lives HERE is the scanner's
// mechanism: optionalValueFlags, the parse branch that applies it, and
// optionalValueBare.

// memoryOptions is the parsed result of a memory subcommand invocation.
type memoryOptions struct {
	// Common
	db string
	// memory_search
	query  string
	limit  int
	intent string
	// memory_store
	topic    string
	question string
	answer   string
	keywords []string
	source   string
	// memory_store provenance pointer (epic 自动记忆 item 2/5): the session the
	// row was captured in and the session's own 1-based round number. A human or
	// a script replaying a note from a session names them here; the agent loop
	// stamps them without any flag (internal/agent/memory_origin_stamp.go).
	sessionID  string
	roundIndex string
	// round / hasRound are the parsed form of roundIndex.
	round    int
	hasRound bool
	// memory_get / memory_update / memory_delete
	id          int
	tier        string
	confidence  float64
	hasConf     bool
	hasKeywords bool
	// memory_list
	listLimit int
	// memory_feedback
	mode     string // vote | list | suggest-cold | recompute
	rating   string
	reason   string
	task     string
	memoryID int
}

// parseMemoryArgs parses args for the given subcommand name without touching
// the process (no os.Exit, no env mutation). It returns the parsed options or
// a usage-style error. Separate from dispatch so the parsing is unit-testable.
func parseMemoryArgs(name string, args []string) (*memoryOptions, error) {
	fs := newMemoryFlagSet()
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}
	for _, fname := range fs.order {
		if !memberOf(name, fname) {
			return nil, fmt.Errorf("unknown flag --%s", fname)
		}
	}
	opts := &memoryOptions{
		db:       fs.get("db"),
		limit:    0,
		intent:   fs.get("intent"),
		topic:    fs.get("topic"),
		question: fs.get("question"),
		answer:   fs.get("answer"),
		source:   fs.get("source"),
		keywords: splitKeywords(fs.get("keywords")),
		tier:     fs.get("tier"),

		sessionID:  strings.TrimSpace(fs.get("session-id")),
		roundIndex: strings.TrimSpace(fs.get("round-index")),
	}
	if fs.has("keywords") {
		opts.hasKeywords = true
	}
	switch name {
	case "memory_search":
		if len(positionals) == 0 {
			return nil, fmt.Errorf("missing <query> argument")
		}
		opts.query = strings.Join(positionals, " ")
		if fs.has("limit") {
			v, err := strconv.Atoi(strings.TrimSpace(fs.get("limit")))
			if err != nil || v <= 0 {
				return nil, fmt.Errorf("--limit must be a positive integer, got %q", fs.get("limit"))
			}
			opts.limit = v
		}
	case "memory_store":
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
		for _, key := range []string{"topic", "question", "answer"} {
			if !fs.has(key) || strings.TrimSpace(fs.get(key)) == "" {
				return nil, fmt.Errorf("Missing required parameter: %s (--%s)", key, key)
			}
		}
		// The provenance pointer (epic 自动记忆 item 2/5). The round is parsed
		// HERE so a typo is a usage error rather than a silently dropped
		// pointer: a value that is not a round number is refused by name, and
		// the session/round completeness rule is the store's own (it refuses a
		// round with no session).
		if opts.roundIndex != "" {
			v, err := strconv.Atoi(opts.roundIndex)
			if err != nil || !sessionevent.RoundIndexRecorded(v) {
				return nil, fmt.Errorf("--round-index must be a recorded round (>= %d, the session's own 1-based numbering), got %q",
					sessionevent.RoundIndexBase, opts.roundIndex)
			}
			opts.round = v
			opts.hasRound = true
		}
	case "memory_get", "memory_update", "memory_delete":
		if len(positionals) > 1 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[1])
		}
		// The id comes from --id N or a single positional (ergonomic
		// `gopi memory_get 524`).
		raw := fs.get("id")
		if raw == "" && len(positionals) == 1 {
			raw = positionals[0]
		}
		if raw == "" {
			return nil, fmt.Errorf("missing required parameter: id (--id N or positional)")
		}
		v, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("id must be a positive integer, got %q", raw)
		}
		opts.id = v
		if name == "memory_update" {
			if fs.has("confidence") {
				c, err := strconv.ParseFloat(strings.TrimSpace(fs.get("confidence")), 64)
				if err != nil || c < 0 || c > 1 {
					return nil, fmt.Errorf("--confidence must be a number in [0,1], got %q", fs.get("confidence"))
				}
				opts.confidence = c
				opts.hasConf = true
			}
			if !fs.has("topic") && !fs.has("question") && !fs.has("answer") &&
				!fs.has("source") && !fs.has("tier") && !fs.has("confidence") &&
				!fs.has("keywords") {
				return nil, fmt.Errorf("memory_update: nothing to update (set at least one field)")
			}
		}
	case "memory_list":
		// Positional-free enumeration; --limit N caps the recent-rows query.
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
		if fs.has("limit") {
			v, err := strconv.Atoi(strings.TrimSpace(fs.get("limit")))
			if err != nil || v <= 0 {
				return nil, fmt.Errorf("--limit must be a positive integer, got %q", fs.get("limit"))
			}
			opts.listLimit = v
		}
	case memoryProvenanceOp:
		// Exactly one mode flag; --db is common. The modes are mutually
		// exclusive (a single run either reports, fills, re-derives or counts).
		if len(positionals) > 0 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[0])
		}
		modes := 0
		for _, m := range memoryProvenanceModes {
			if fs.has(m) {
				modes++
				opts.mode = m
			}
		}
		if modes > 1 {
			return nil, fmt.Errorf("memory provenance: --%s are mutually exclusive",
				strings.Join(memoryProvenanceModes, " / --"))
		}
		if modes == 0 {
			// Default to the read-only shape report: the safe, informative
			// invocation. A mutating default would be a footgun.
			opts.mode = memoryProvenanceDefaultMode
		}
	case "memory_feedback":
		// Mode selection: exactly one of --recompute / --suggest-cold /
		// --list / a vote (the default). Modes are mutually exclusive.
		modes := 0
		for _, m := range []string{"recompute", "suggest-cold", "list"} {
			if fs.has(m) {
				modes++
				opts.mode = m
			}
		}
		if modes > 1 {
			return nil, fmt.Errorf("memory_feedback: --recompute / --suggest-cold / --list are mutually exclusive")
		}
		if opts.mode == "recompute" {
			if len(positionals) > 0 {
				return nil, fmt.Errorf("memory_feedback --recompute takes no id")
			}
			return opts, nil
		}
		if opts.mode == "suggest-cold" {
			if len(positionals) > 0 {
				return nil, fmt.Errorf("memory_feedback --suggest-cold takes no id")
			}
			return opts, nil
		}
		if opts.mode == "list" {
			if len(positionals) > 0 {
				return nil, fmt.Errorf("memory_feedback --list takes no id (use --memory-id N to filter)")
			}
			if fs.has("memory-id") {
				v, err := strconv.Atoi(strings.TrimSpace(fs.get("memory-id")))
				if err != nil || v <= 0 {
					return nil, fmt.Errorf("--memory-id must be a positive integer, got %q", fs.get("memory-id"))
				}
				opts.memoryID = v
			}
			return opts, nil
		}
		// Vote mode: id + exactly one rating flag.
		if len(positionals) > 1 {
			return nil, fmt.Errorf("unexpected positional argument %q", positionals[1])
		}
		raw := fs.get("id")
		if raw == "" && len(positionals) == 1 {
			raw = positionals[0]
		}
		if raw == "" {
			return nil, fmt.Errorf("missing required parameter: id (--id N or positional)")
		}
		v, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("id must be a positive integer, got %q", raw)
		}
		opts.id = v
		for _, r := range []string{"useful", "useless", "unused"} {
			if fs.has(r) {
				if opts.rating != "" {
					return nil, fmt.Errorf("memory_feedback: --useful / --useless / --unused are mutually exclusive")
				}
				opts.rating = r
			}
		}
		if opts.rating == "" {
			return nil, fmt.Errorf("memory_feedback: exactly one of --useful / --useless / --unused is required")
		}
		opts.reason = fs.get("reason")
		opts.task = fs.get("task")
		opts.mode = memoryFeedbackVoteMode
	}
	return opts, nil
}

// runMemorySubcommand parses the subcommand's own flags and dispatches to the
// shared tools.MemorySearch / tools.MemoryStore functions so CLI behavior is
// identical to tool_use. It exits the process with a non-zero status on
// argument/usage errors; engine errors are echoed to stderr and exit 1.
func runMemorySubcommand(name string, args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(memoryUsage)
			os.Exit(0)
		}
	}

	opts, err := parseMemoryArgs(name, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n%s", memoryDisplayName(name), err, memoryUsage)
		os.Exit(1)
	}

	// THE LEDGER GUARD (cli_memory_ledger_guard.go). An explicitly named --db
	// that is not there is NOT an empty knowledge base: the READ-ONLY half of
	// this verb family refuses it by name, and the WRITING half is allowed to
	// create it but must SAY so. Before this guard every read-only verb opened
	// the path through the ordinary creator, printed "knowledge base is empty"
	// and left a 128 KiB file behind (measured 2026-11-06).
	creating := memoryLedgerCreatingOp(name, opts.mode)
	if !creating {
		if refusal := memoryLedgerReadOnlyRefusal(memoryDisplayName(name), opts.db); refusal != "" {
			fmt.Fprint(os.Stderr, refusal)
			os.Exit(1)
		}
	}
	// The writer's notice is computed BEFORE the open (that is the only moment
	// the absence can be observed) and PRINTED only after the write succeeded —
	// a sentence claiming "created" must not appear on a run that failed.
	createdNotice := ""
	if creating {
		createdNotice = memoryLedgerWriterNotice(memoryDisplayName(name),
			opts.db, memoryLedgerEffectivePath(opts.db))
	}

	// --db → GOPI_MEMORY_DB so the shared engine functions use that path.
	if opts.db != "" {
		if err := os.Setenv("GOPI_MEMORY_DB", opts.db); err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s: --db: %v\n", memoryDisplayName(name), err)
			os.Exit(1)
		}
	}

	switch name {
	case "memory_search":
		result, err := tools.MemorySearch(opts.query, opts.limit, opts.intent)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s error: %v\n", memoryDisplayName(name), err)
			os.Exit(1)
		}
		// Procedural rows merge inside tools.MemorySearch.
		fmt.Println(result)
	case "memory_store":
		// The provenance pointer the agent loop stamps automatically
		// (internal/agent/memory_origin_stamp.go) is available to a human or a
		// script through these two flags; with neither, the row records no
		// provenance at all — the honest state for `gopi memory store`.
		origin := tools.MemoryOrigin{SessionID: opts.sessionID}
		if opts.hasRound {
			origin.Round = opts.round
		}
		result, err := tools.MemoryStoreWithOrigin(opts.topic, opts.question, opts.answer, opts.source, opts.keywords, origin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s error: %v\n", memoryDisplayName(name), err)
			os.Exit(1)
		}
		fmt.Println(result)
	case "memory_get":
		result, err := tools.MemoryGet(opts.id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s error: %v\n", memoryDisplayName(name), err)
			os.Exit(1)
		}
		fmt.Println(result)
	case "memory_list":
		result, err := tools.MemoryListRecent(opts.listLimit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi memory_list error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(result)
	case "memory_update":
		fields := tools.MemoryUpdateFields{
			Topic:    opts.topic,
			Question: opts.question,
			Answer:   opts.answer,
			Source:   opts.source,
			Tier:     opts.tier,
		}
		if opts.hasConf {
			c := opts.confidence
			fields.Confidence = &c
		}
		if opts.hasKeywords {
			fields.Keywords = opts.keywords
			if fields.Keywords == nil {
				fields.Keywords = []string{} // explicitly provided but empty → clear keywords
			}
		}
		result, err := tools.MemoryUpdate(opts.id, fields)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s error: %v\n", memoryDisplayName(name), err)
			os.Exit(1)
		}
		fmt.Println(result)
	case "memory_delete":
		result, err := tools.MemoryDelete(opts.id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s error: %v\n", memoryDisplayName(name), err)
			os.Exit(1)
		}
		fmt.Println(result)
	case memoryProvenanceOp:
		result, err := runMemoryProvenance(opts.mode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s error: %v\n", memoryDisplayName(name), err)
			os.Exit(1)
		}
		fmt.Println(result)
	case "memory_feedback":
		var result string
		var err error
		switch opts.mode {
		case "recompute":
			result, err = tools.FeedbackRecompute()
		case "suggest-cold":
			result, err = tools.FeedbackSuggestCold()
		case "list":
			result, err = tools.FeedbackHistory(opts.memoryID)
		default: // vote
			result, err = tools.MemoryFeedback(opts.id, opts.rating, opts.reason, opts.task)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s error: %v\n", memoryDisplayName(name), err)
			os.Exit(1)
		}
		fmt.Println(result)
	}
	// The writer's "this ledger did not exist and I created it" line
	// (cli_memory_ledger_guard.go). Printed only after the write succeeded, and
	// to STDERR on purpose: a script parsing stdout for the row keeps working,
	// while a human (and a log reader) can no longer mistake a freshly created
	// ledger for an empty one.
	if createdNotice != "" {
		fmt.Fprintln(os.Stderr, createdNotice)
	}
}

// memoryDisplayName maps an internal memory verb name to the canonical
// grouped CLI spelling used in error prefixes (memory_search → "memory
// search") — errors must teach the tab-completion-friendly form.
func memoryDisplayName(name string) string {
	return strings.Replace(name, "memory_", "memory ", 1)
}

// memberOf reports whether a flag name is valid for the given subcommand.
// memory_search takes db/limit/intent; memory_store takes db/topic/question/
// answer/keywords/source. Cross-subcommand flags are rejected (mirrors the
// strict tool-arg validation).
func memberOf(name, flag string) bool {
	switch name {
	case "memory_search":
		switch flag {
		case "db", "limit", "intent":
			return true
		}
	case "memory_store":
		switch flag {
		case "db", "topic", "question", "answer", "keywords", "source", "session-id", "round-index":
			return true
		}
	case "memory_get", "memory_delete":
		switch flag {
		case "db", "id":
			return true
		}
	case "memory_list":
		switch flag {
		case "db", "limit":
			return true
		}
	case memoryProvenanceOp:
		// The provenance modes are the flags themselves (--shape, --backfill,
		// --reclassify, --counts) plus --db.
		if flag == "db" {
			return true
		}
		for _, m := range memoryProvenanceModes {
			if flag == m {
				return true
			}
		}
		return false
	case "memory_update":
		switch flag {
		case "db", "id", "topic", "question", "answer", "source", "tier", "confidence", "keywords":
			return true
		}
	case "memory_feedback":
		switch flag {
		case "db", "id", "useful", "useless", "unused", "reason", "task", "list", "memory-id", "suggest-cold", "recompute":
			return true
		}
	}
	return false
}

// splitKeywords splits a comma-separated --keywords value into a []string,
// trimming whitespace and dropping empty entries.
func splitKeywords(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
