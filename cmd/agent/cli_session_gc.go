package main

// cli_session_gc.go — `gopi session gc`: the session family's ONE reclaim verb.
//
// WHY IT EXISTS (measured 2026-10-29, host host-a, the bug row this file
// answers). The family carried EIGHT verbs and every one of them was a READER.
// Nothing could reclaim a session directory, so ~/.gopi/sessions grew to
// 90 GB / 3175 directories on a `/` partition at 98% (48 G free), and the only
// exit left was an operator's hand-run `rm -rf` — a destructive operation outside
// gopi's own machinery, which Rule 42 forbids an agent from performing and which
// leaves no record of what was lost.
//
// WHAT IT IS, in three sentences. It REPORTS which session directories are
// collectable and why, it DELETES NOTHING unless asked (`--apply` is the only
// door, so the default is a dry run), and it removes a container only when four
// hard predicates all pass — ours by name, aged, unheld by any live process, and
// named by no task ledger row — with every unknown resolving to KEEP and being
// REPORTED as the reason.
//
// WHAT IT IS NOT. It is not a second sweep: the predicate, the liveness probe,
// the staleness rule, the byte measurement and the lock are all
// internal/scratch/sessions_gc.go's, which are in turn the primitives the
// sandbox and declared-tree arms already use. This file owns only the argv, the
// two renders and the exit code — the same split `gopi task stranded` and
// `gopi scratch-gc` follow.
//
// IT IS A ONE-SHOT VERB, DELIBERATELY (Rule 35 — zero dependency): nothing
// schedules it. No timer, no unit, no cron line, no resident tick. A human runs
// it, reads the report, and decides — which is the whole difference between a
// reclaim path and a machine that deletes on a schedule nobody remembers.

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// sessionSubcmdGC is the sub-verb word. It is a named constant because the
	// dispatcher's switch, the family's vocabulary slice, the usage page and the
	// `-h` contract all compare it (Rule 1b: never type the word twice).
	sessionSubcmdGC = "gc"

	// sessionGCApplyFlag is the ONLY door to a delete. Its absence is the dry
	// run, which is the default the epic requires: an operator who types
	// `gopi session gc` to look must not lose anything.
	sessionGCApplyFlag = "--apply"

	// sessionGCDryRunFlag is the explicit spelling of the default. It exists so
	// a script can say what it means, and so `--dry-run --apply` is a refusal
	// rather than a silently-preferred order (Rule 15: an accepted-and-ignored
	// option is a defect).
	sessionGCDryRunFlag = "--dry-run"

	// sessionGCOlderThanFlag is the freshness window: a bare count is DAYS, and a
	// Go duration spelling with its own unit is honoured as written
	// (`--older-than 30`, `--older-than 30d`, `--older-than 72h`).
	sessionGCOlderThanFlag = "--older-than"

	// sessionGCDay is ONE day, the unit this flag's default and its page are
	// written in. Named (Rule 1/13) because the conversion happens at three
	// sites — the parser, the page's default rendering and the epic's own
	// window arithmetic — and a bare `24 * time.Hour` at each of them is exactly
	// the literal the rule bans.
	sessionGCDay = 24 * time.Hour

	// sessionGCDaysSuffix is the day unit an operator TYPES: `--older-than 8d`
	// names the same window as `--older-than 8`. Go's time.ParseDuration has no
	// day unit at all, so this suffix is stripped HERE rather than handed to it
	// (measured: `--older-than 8d` used to exit 2 with the whole usage page).
	sessionGCDaysSuffix = "d"

	// sessionGCJSONFlag is `--json`: the machine-readable plan, so a script can
	// read the candidate set without parsing the table.
	sessionGCJSONFlag = sessionJSONFlag

	// sessionGCUsageFingerprint is the verb's page's first line — the string the
	// help contract pins.
	sessionGCUsageFingerprint = "gopi session gc — report which session directories are collectable (dry run by default)"

	// sessionGCReasonWidth is the label column of the per-candidate lines. Wide
	// enough for the longest status word the render prints ("keep"/"collect"),
	// so a reader sees one column (Rule 1: no bare literal).
	sessionGCReasonWidth = 8

	// sessionGCNameWidth is the name column of the per-candidate lines: a
	// canonical uuid is 36 cells, and a truncation there would print an id the
	// operator cannot paste back into a sibling verb.
	sessionGCNameWidth = 38

	// sessionGCSizeWidth is the size column, in MiB with one decimal.
	sessionGCSizeWidth = 10

	// sessionGCBytesPerMiB is the unit the report prints. Named (Rule 1) because
	// a bare 1048576 at a format site is exactly the magic number the rule bans.
	sessionGCBytesPerMiB = 1 << 20
)

// sessionGCUsage is the verb's own page.
var sessionGCUsage = sessionGCUsageFingerprint + `

A session directory holds one run's record: its session.jsonl, its raw request
log, its artifacts and — for a task run — the attachments and the report a
review cites. Nothing in the tree bounded its growth, so this verb is the ONE
reclaim path, and it is a DRY RUN unless you pass ` + "`" + sessionGCApplyFlag + "`" + `.

Usage:
  gopi session gc [--apply] [--older-than DAYS] [--json]
                  [--largest N] [--trim-raw-log SIZE [--apply]]
      Report every directory directly under the sessions root, one line each:
        collect  the four hard predicates all passed (see below)
        keep     at least one of them did NOT, and the line says WHICH and WHY
      Then a summary: how many are collectable, how many bytes they hold, how
      many are kept and for which reason (counted by reason, so "everything is
      protected by a ledger row" and "nothing has aged out" are different lines).

  ` + "`" + sessionGCDryRunFlag + "`" + `
      the DEFAULT, spelled out. Deletes nothing. Passing it together with
      ` + "`" + sessionGCApplyFlag + "`" + ` is refused by name.
  ` + "`" + sessionGCApplyFlag + "`" + `
      actually remove the collectable directories.
  ` + "`" + sessionGCOlderThanFlag + "`" + ` DAYS
      the freshness window (default ` + formatGCDays(scratch.SessionsGCStaleAge) + `). A bare count is DAYS and "Nd" names the
      same window ("8" and "8d" are one window); "192h"/"30m" is a Go duration.
      A DRY RUN may ask for a shorter window to preview it; an APPLY floors the
      window at ` + formatGCDays(scratch.SessionsGCStaleAge) + `, because this arm
      deletes a run's whole record and its floor is the point of it existing.
  ` + "`" + sessionGCJSONFlag + "`" + `
      the plan as JSON (path, name, age, bytes, keep, reason).

` + sessionGCLargestUsage + `
` + sessionGCTrimUsage + `
THE FOUR PREDICATES, all required, and every UNKNOWN is a KEEP:
  OURS          the directory's NAME is a session-container shape this system
                MINTS: a canonical uuid, optionally carrying ONE minted ordinal
                suffix — ` + "`<uuid>-r<N>`" + ` for a reopen successor row and
                ` + "`<uuid>-c<N>`" + ` for a clean-room clone row. The suffix is not
                evidence of ownership, only of the SHAPE: a suffixed container
                with a ledger row is still kept by UNREFERENCED, and one that
                aged out is collected like any other. The root is shared with
                other shapes, so a name we did not mint is never a candidate at
                any age.
  AGED          its own mtime is older than the window (a container that has
                been USED looks recently used; a record inside it does not move).
  UNHELD        no live process stands inside it (the /proc scan every gopi
                sweep uses, plus a ` + "`.holder`" + ` registration), and the scan
                FINISHED — a pass that stopped at its bound keeps everything,
                because "not looked at" is not "nobody there".
  UNREFERENCED  no task ledger row names it, by TASK ID (the container's own
                name) or by SESSION ID (a row's run log). EVERY status counts,
                completed included: a finished row is the row you still read.

An unreadable /proc, an unreadable ledger, an age that cannot be measured and a
name this system does not mint all resolve to KEEP and are reported with their
own reason. The sessions ROOT itself is never a candidate at any age.

The verb is IDEMPOTENT (a second run finds nothing collectable) and BOUNDED (one
directory listing, at most ` + strconv.Itoa(scratch.SessionsGCScanLimit) + ` entries).
It takes the tree's own lock, so two concurrent invocations cannot delete the
same directory twice — the second one reports the deferral instead.
`

// formatGCDays renders a window as whole days for the page.
func formatGCDays(d time.Duration) string {
	return fmt.Sprintf("%d%s", int(d/sessionGCDay), sessionGCDaysSuffix)
}

// sessionGCOptions is the parsed argv.
type sessionGCOptions struct {
	apply     bool
	dryRun    bool
	olderThan time.Duration
	json      bool
	// largest is `--largest N`: how many of the biggest containers to report.
	// largestSet records that the FLAG was given, because N=0 is a legal
	// "print nothing but the summary" and would otherwise be indistinguishable
	// from the flag being absent (Rule 15: an affirmative side-channel, never a
	// zero-value reading).
	largest    int
	largestSet bool
	// trimCap is `--trim-raw-log SIZE` in bytes; trimSet records that the flag
	// was given, for the same reason (a byte count has no safe zero).
	trimCap int64
	trimSet bool
}

// runSessionGC implements `gopi session gc [--apply] [--older-than D] [--json]`
// plus the two arms that answer the questions the age judgement cannot
// (`--largest N`, `--trim-raw-log SIZE`; see cli_session_gc_largest.go).
func runSessionGC(args []string) int {
	opts, code := parseSessionGCArgs(args)
	if code >= 0 {
		return code
	}
	if opts.largestSet {
		return runSessionGCLargest(sessionGCLargestOptions{limit: opts.largest, json: opts.json})
	}
	if opts.trimSet {
		return runSessionGCTrimRawLog(sessionGCTrimOptions{capBytes: opts.trimCap, apply: opts.apply, json: opts.json})
	}
	// The reader the UNREFERENCED predicate needs. It is wired HERE rather than
	// in an init so the seam is visible where it is used: internal/scratch must
	// stay a leaf (internal/tools imports it), so internal/tools provides the
	// implementation and this call installs it.
	tools.RegisterSessionGCLedger()

	root, err := scratch.SessionsDirRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: resolving the sessions root: %v\n", sessionSubcmdGC, err)
		return 1
	}
	// A DRY RUN may shorten the window to preview one; an APPLY may not (see the
	// page). The floor lives in scratch (SessionsGCWindow) so the two faces of
	// this verb cannot disagree about what the window means.
	window := opts.olderThan
	if opts.apply {
		window = scratch.SessionsGCWindow(opts.olderThan)
	}

	plans, err := scratch.PlanSessions(root, window)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdGC, err)
		return 1
	}

	if opts.apply {
		return runSessionGCApply(root, window, plans, opts.json)
	}
	if opts.json {
		out, jerr := sessionGCJSON(root, window, plans, 0, 0, true)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdGC, jerr)
			return 1
		}
		fmt.Print(out)
		return 0
	}
	fmt.Print(sessionGCText(root, window, plans, true, 0, 0))
	return 0
}

// runSessionGCApply is the destructive arm: it reports the SAME plan the dry run
// would, then performs it. The plan is printed BEFORE anything is removed, so a
// transcript of an apply run shows what the decision was even if the removal
// half is interrupted.
func runSessionGCApply(root string, window time.Duration, plans []scratch.SessionsPlan, asJSON bool) int {
	removed, bytes, lock, err := scratch.SweepSessionsDetailed(root, window)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdGC, err)
		return 1
	}
	if asJSON {
		out, jerr := sessionGCJSON(root, window, plans, removed, bytes, false)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdGC, jerr)
			return 1
		}
		fmt.Print(out)
	} else {
		fmt.Print(sessionGCText(root, window, plans, false, removed, bytes))
	}
	// A DEFERRED sweep is not a successful one, and the difference must survive
	// into the exit code: the lock verdict says another invocation owns the tree
	// right now, so `removed=0` would otherwise read as "nothing to collect"
	// (Rule 15).
	if lock.Outcome != "" && !lock.Locked && lock.Outcome != scratch.GCLockOutcomeAcquired {
		fmt.Fprintf(os.Stderr, "gopi session %s: the sweep was DEFERRED — %s (%s)\n",
			sessionSubcmdGC, lock.Outcome, lock.Reason)
		return 3
	}
	return 0
}

// sessionGCPlanRow is one JSON row of the plan.
type sessionGCPlanRow struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	AgeHours int64  `json:"age_hours"`
	Bytes    int64  `json:"bytes"`
	Keep     bool   `json:"keep"`
	Reason   string `json:"reason"`
}

// sessionGCPlanDoc is the whole JSON document.
type sessionGCPlanDoc struct {
	Root      string             `json:"root"`
	WindowHrs int64              `json:"window_hours"`
	DryRun    bool               `json:"dry_run"`
	Rows      []sessionGCPlanRow `json:"rows"`
	// Summary is carried in the document too, so a script does not have to
	// recompute what the human render prints.
	Collectable int            `json:"collectable"`
	Bytes       int64          `json:"collectable_bytes"`
	Removed     int            `json:"removed"`
	RemovedByte int64          `json:"removed_bytes"`
	KeptByWhy   map[string]int `json:"kept_by_reason"`
}

// sessionGCJSON renders the plan as JSON.
func sessionGCJSON(root string, window time.Duration, plans []scratch.SessionsPlan,
	removed int, bytes int64, dryRun bool) (string, error) {
	doc := sessionGCPlanDoc{
		Root:        root,
		WindowHrs:   int64(window / time.Hour),
		DryRun:      dryRun,
		Rows:        make([]sessionGCPlanRow, 0, len(plans)),
		Removed:     removed,
		RemovedByte: bytes,
		KeptByWhy:   map[string]int{},
	}
	for _, p := range plans {
		doc.Rows = append(doc.Rows, sessionGCPlanRow{
			Path: p.Path, Name: p.Name, AgeHours: int64(p.Age / time.Hour),
			Bytes: p.Bytes, Keep: p.Keep, Reason: p.Reason,
		})
		if p.Collectable() {
			doc.Collectable++
			doc.Bytes += p.Bytes
			continue
		}
		doc.KeptByWhy[sessionGCReasonClass(p.Reason)]++
	}
	blob, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render the plan: %w", err)
	}
	return string(blob) + "\n", nil
}

// sessionGCText renders the plan for a human: one line per directory, then the
// summary.
//
// EVERY DIRECTORY IS PRINTED, keeps included. A page that listed only candidates
// would report "0 collectable" identically for "nothing has aged out" and
// "everything is protected", which is the ambiguity Rule 15 refuses — the
// operator's real question is usually the second one ("why is the disk still
// full?"), and only the keep lines answer it.
func sessionGCText(root string, window time.Duration, plans []scratch.SessionsPlan,
	dryRun bool, removed int, bytes int64) string {
	var b strings.Builder
	mode := "apply"
	if dryRun {
		mode = "dry-run (nothing was deleted)"
	}
	fmt.Fprintf(&b, "gopi session %s — %s\n", sessionSubcmdGC, mode)
	fmt.Fprintf(&b, "root:   %s\n", root)
	fmt.Fprintf(&b, "window: %s (a directory untouched for longer than this is a candidate)\n", window)
	fmt.Fprintf(&b, "scanned: %d %s\n\n", len(plans), pluralSuffix(len(plans), "directory", "directories"))

	collectable, kept, freed := 0, 0, int64(0)
	byReason := map[string]int{}
	reasonOrder := []string{}
	for _, p := range plans {
		status := "keep"
		if p.Collectable() {
			status = "collect"
			collectable++
			freed += p.Bytes
		} else {
			kept++
			class := sessionGCReasonClass(p.Reason)
			if byReason[class] == 0 {
				reasonOrder = append(reasonOrder, class)
			}
			byReason[class]++
		}
		fmt.Fprintf(&b, "  %-*s  %-*s  %8.1f MiB  %6s  %s\n",
			sessionGCReasonWidth, status,
			sessionGCNameWidth, p.Name,
			float64(p.Bytes)/sessionGCBytesPerMiB,
			p.Age.Round(time.Hour), p.Reason)
	}

	fmt.Fprintf(&b, "\nsummary: collectable=%d (%s) kept=%d scanned=%d\n",
		collectable, formatGCBytes(freed), kept, len(plans))
	if dryRun {
		fmt.Fprintf(&b, "         a dry run: NOTHING was deleted. Re-run with %s to collect.\n", sessionGCApplyFlag)
	} else {
		fmt.Fprintf(&b, "         removed=%d reclaimed=%s\n", removed, formatGCBytes(bytes))
	}
	// Counted BY REASON, in first-seen order: the summary's job is to answer
	// "what is holding the tree", and one total would not.
	for _, r := range reasonOrder {
		fmt.Fprintf(&b, "         kept %4d × %s\n", byReason[r], r)
	}
	return b.String()
}

// sessionGCReasonClass folds a keep reason down to its class so the summary's
// counts group by CAUSE rather than by the per-row detail (a ledger reason
// carries which row, an age reason carries the error).
func sessionGCReasonClass(reason string) string {
	if i := strings.Index(reason, " ("); i > 0 {
		return reason[:i]
	}
	return reason
}

// formatGCBytes renders a byte count for a human.
func formatGCBytes(n int64) string {
	const kib = 1024
	switch {
	case n >= kib*kib*kib:
		return fmt.Sprintf("%.1f GiB", float64(n)/(kib*kib*kib))
	case n >= kib*kib:
		return fmt.Sprintf("%.1f MiB", float64(n)/(kib*kib))
	case n >= kib:
		return fmt.Sprintf("%.1f KiB", float64(n)/kib)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// pluralSuffix picks a suffix: "1 directory", "3 directories".
func pluralSuffix(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// parseSessionGCArgs parses the verb's argv. code >= 0 means the caller must
// exit with it (a usage page was printed, or the argv was refused).
func parseSessionGCArgs(args []string) (sessionGCOptions, int) {
	opts := sessionGCOptions{olderThan: scratch.SessionsGCStaleAge}
	for i := 0; i < len(args); i++ {
		a := args[i]
		val := ""
		if strings.HasPrefix(a, "--") {
			if eq := strings.Index(a, "="); eq >= 0 {
				val, a = a[eq+1:], a[:eq]
			}
		}
		switch a {
		case HelpFlag, HelpFlagLong:
			fmt.Print(sessionGCUsage)
			return opts, 0
		case sessionGCApplyFlag:
			opts.apply = true
		case sessionGCDryRunFlag:
			opts.dryRun = true
		case sessionGCJSONFlag:
			opts.json = true
		case sessionGCOlderThanFlag:
			raw := nextVal(args, &i, val, sessionGCOlderThanFlag)
			d, err := parseGCDuration(raw)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gopi session %s: %s %v\n%s", sessionSubcmdGC, sessionGCOlderThanFlag, err, sessionGCUsage)
				return opts, 2
			}
			opts.olderThan = d
		case sessionGCLargestFlag:
			opts.largest = sessionGCLargestDefaultLimit
			opts.largestSet = true
			// The count is OPTIONAL (`--largest` alone means the default). It is
			// taken only when the next argv is a bare integer, so
			// `--largest --json` reads as "the default count, as JSON" rather
			// than as a parse error nobody can explain.
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil && n >= 0 {
					opts.largest = n
					i++
				}
			}
			if val != "" {
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 {
					fmt.Fprintf(os.Stderr, "gopi session %s: %s %q is not a count\n%s",
						sessionSubcmdGC, sessionGCLargestFlag, val, sessionGCUsage)
					return opts, 2
				}
				opts.largest = n
			}
		case sessionGCTrimRawLogFlag:
			raw := nextVal(args, &i, val, sessionGCTrimRawLogFlag)
			n, err := parseGCSizeBytes(raw)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gopi session %s: %s %v\n%s", sessionSubcmdGC, sessionGCTrimRawLogFlag, err, sessionGCUsage)
				return opts, 2
			}
			opts.trimCap = n
			opts.trimSet = true
		default:
			fmt.Fprintf(os.Stderr, "gopi session %s: unexpected argument %q\n%s", sessionSubcmdGC, a, sessionGCUsage)
			return opts, 2
		}
	}
	// The two doors are exclusive. Preferring one silently would make
	// `--dry-run --apply` a coin flip the reader cannot see (Rule 15).
	if opts.apply && opts.dryRun {
		fmt.Fprintf(os.Stderr, "gopi session %s: %s and %s are exclusive — pick one\n%s",
			sessionSubcmdGC, sessionGCDryRunFlag, sessionGCApplyFlag, sessionGCUsage)
		return opts, 2
	}
	// `--largest` is a READER: it never deletes and never rewrites, so an
	// `--apply` beside it would be an option that silently does nothing — the
	// exact defect Rule 15 names. It is refused by name instead.
	if opts.largestSet && opts.apply {
		fmt.Fprintf(os.Stderr, "gopi session %s: %s is read-only and cannot be combined with %s\n%s",
			sessionSubcmdGC, sessionGCLargestFlag, sessionGCApplyFlag, sessionGCUsage)
		return opts, 2
	}
	// Two different subjects in one invocation would make the report answer
	// neither question completely.
	if opts.largestSet && opts.trimSet {
		fmt.Fprintf(os.Stderr, "gopi session %s: %s and %s ask different questions — run them separately\n%s",
			sessionSubcmdGC, sessionGCLargestFlag, sessionGCTrimRawLogFlag, sessionGCUsage)
		return opts, 2
	}
	return opts, -1
}

// parseGCDuration reads `--older-than`: a bare count is DAYS (the unit an
// operator thinks in for this window), the same count carrying the day suffix
// ("8d") names THAT SAME window, and a Go duration spelling with its own unit
// (`72h`, `30m`) is honoured as written.
//
// The day suffix is handled HERE because time.ParseDuration has no day unit: it
// answers `8d` with "unknown unit \"d\" in duration \"8d\"", which the verb used
// to turn into a usage refusal and a whole help page. `8days`, `8D` and `8d5h`
// are still refused — the suffix is one shape, not a prefix strip.
func parseGCDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("needs a value (days, a day count like 8d, or a duration like 72h)")
	}
	if n, err := strconv.Atoi(raw); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("%q must be positive", raw)
		}
		return time.Duration(n) * sessionGCDay, nil
	}
	// One optional day suffix, and nothing else: the digits must be followed by
	// exactly the suffix (case-, space- and trailing-anything-refusing).
	if digits, ok := strings.CutSuffix(raw, sessionGCDaysSuffix); ok {
		if n, err := strconv.Atoi(digits); err == nil {
			if n <= 0 {
				return 0, fmt.Errorf("%q must be positive", raw)
			}
			return time.Duration(n) * sessionGCDay, nil
		}
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%q is neither a number of days (8, 8d) nor a duration (72h): %w", raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%q must be positive", raw)
	}
	return d, nil
}
