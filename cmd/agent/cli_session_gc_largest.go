package main

// cli_session_gc_largest.go — the two read/write arms `gopi session gc` grew
// when the disk measurement showed its age-based judgement could not see the
// problem.
//
// WHY THEY EXIST (measured 2026-10-30, host the host; the row this file
// answers). The family's ONE reclaim verb judged by AGE: `gopi session gc`
// reported 3281 directories scanned, 110 collectable, 14.7 MiB. The tree it was
// judging held 83 GB, and the shape of the occupancy was the OPPOSITE of what an
// age rule can collect — the biggest things were the NEWEST ones by
// construction:
//
//	$ df -h /            → 53G free of 1.8T (98% used)
//	$ du -sh ~/.gopi/sessions → 83G
//	*.raw.log: 1095 files / 64.4 GiB, p50 48.4 MiB, p90 115.7 MiB, max 1515.7 MiB
//
// So this file adds the two questions the verb could not answer:
//
//	--largest N        WHO is big. Read-only, sorted by bytes, keeping the SAME
//	                   keep/reason column an apply would act on — so "big AND
//	                   collectable" is visible instead of inferred.
//	--trim-raw-log S   WHAT can be bounded without losing the record. A raw log
//	                   over the cap is rewritten to head + one honest marker +
//	                   tail; a log within the cap is never opened; a log a live
//	                   process holds open is refused. The default is a DRY RUN.
//
// WHAT NEITHER OF THEM IS. Neither deletes a session directory (`--apply` on the
// original judgement still owns that door), and neither runs on a schedule
// (Rule 35 — no timer, no unit, no cron line: a human runs the verb and reads
// the report).

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// sessionGCLargestFlag is `--largest N`: an integer, not a boolean, because
	// the operator's question is always "the top N" and a fixed N would be a
	// second thing to change (Rule 1).
	sessionGCLargestFlag = "--largest"

	// sessionGCTrimRawLogFlag is `--trim-raw-log SIZE`: the bounded rewrite of
	// a session's raw HTTP log. SIZE accepts a byte count or a unit spelling
	// ("64MiB", "512MiB", "2GiB").
	sessionGCTrimRawLogFlag = "--trim-raw-log"

	// sessionGCLargestDefaultLimit is how many rows `--largest` prints when the
	// operator gives no N. Ten: enough to answer "what is filling the disk" on
	// one screen, small enough to stay a report rather than a dump.
	sessionGCLargestDefaultLimit = 10

	// sessionGCBytesPerKiB is the unit the flag parser and the render share
	// (Rule 1: no bare 1024 at a format or comparison site).
	sessionGCBytesPerKiB = 1024

	// sessionGCTrimTopLimit caps how many offending files the DRY RUN lists by
	// name. The apply arm always acts on ALL of them — only the printed list is
	// bounded, and the summary states how many were not printed (Rule 15: a
	// truncated list must say it is truncated).
	sessionGCTrimTopLimit = 20
)

// sessionGCLargestUsage is the `--largest` help fragment, printed by the verb's
// own page and by `-h` (one spelling, Rule 13). The default count is inserted
// with its own format verb, so changing the constant needs no second edit.
var sessionGCLargestUsage = fmt.Sprintf(`  --largest [N]
      report the N largest session directories (default %d), biggest
      first, with the SAME keep/reason column an apply acts on. READ-ONLY:
      nothing is deleted, nothing is written. This is the question the age
      judgement cannot answer — the biggest directories are usually the young
      ones, which is exactly why they are big.
`, sessionGCLargestDefaultLimit)

// sessionGCTrimUsage is the `--trim-raw-log` help fragment.
const sessionGCTrimUsage = `  --trim-raw-log SIZE [--apply]
      bound each session's *.raw.log to SIZE (a byte count, or "64MiB"). An
      over-cap log is rewritten to: its HEAD (the creation header and the first
      rounds), ONE marker line stating how many bytes were dropped and when,
      and its TAIL (the most recent rounds, on complete lines). The result is
      <= SIZE. A log within the cap is never opened; a log a live process holds
      open is refused by name.
      WHAT A TRIMMED LOG CANNOT DO: the dropped middle is gone, so the file can
      no longer answer a checkpoint. "gopi session checkpoint export" on it
      refuses by name (it does not hand back the head's first round as "the
      last state"), "gopi session checkpoint list" labels the row
      TRIMMED, and a resume from it is refused. The marker line inside the file
      says so too. Trimming is a size bound, not a lossless rotation.
      DRY RUN by default: it reports what it WOULD free. --apply performs it.
`

// sessionGCTopRow is one line of the `--largest` report.
type sessionGCTopRow struct {
	Path   string
	Name   string
	Bytes  int64
	Age    time.Duration
	Keep   bool
	Reason string
}

// sessionGCLargestOptions is the parsed argv of `--largest`.
type sessionGCLargestOptions struct {
	limit int
	json  bool
}

// sessionGCTrimOptions is the parsed argv of `--trim-raw-log`.
type sessionGCTrimOptions struct {
	capBytes int64
	apply    bool
	json     bool
}

// runSessionGCLargest implements `gopi session gc --largest [N] [--json]`.
// It is read-only: PlanSessions takes no lock and writes nothing.
func runSessionGCLargest(opts sessionGCLargestOptions) int {
	tools.RegisterSessionGCLedger()
	root, err := scratch.SessionsDirRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: resolving the sessions root: %v\n", sessionSubcmdGC, err)
		return 1
	}
	plans, err := scratch.LargestSessions(root, scratch.SessionsGCStaleAge, opts.limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdGC, err)
		return 1
	}
	var total, collectableTotal int64
	for _, p := range plans {
		total += p.Bytes
		if p.Collectable() {
			collectableTotal += p.Bytes
		}
	}
	if opts.json {
		rows := make([]sessionGCPlanRow, 0, len(plans))
		for _, p := range plans {
			rows = append(rows, sessionGCPlanRow{
				Path: p.Path, Name: p.Name, AgeHours: int64(p.Age / time.Hour),
				Bytes: p.Bytes, Keep: p.Keep, Reason: p.Reason,
			})
		}
		out, jerr := marshalGCJSON(rows)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdGC, jerr)
			return 1
		}
		fmt.Print(out)
		return 0
	}

	var b strings.Builder
	fmt.Fprintf(&b, "gopi session %s --largest %d — READ-ONLY (nothing deleted, nothing written)\n", sessionSubcmdGC, opts.limit)
	fmt.Fprintf(&b, "root:  %s\n", root)
	fmt.Fprintf(&b, "shown: %d largest of the tree, biggest first\n\n", len(plans))
	for i, p := range plans {
		status := "keep"
		if p.Collectable() {
			status = "collect"
		}
		fmt.Fprintf(&b, "  %2d. %-*s  %10s  %6s  %-*s  %s\n",
			i+1, sessionGCNameWidth, p.Name,
			formatGCBytes(p.Bytes), p.Age.Round(time.Hour),
			sessionGCReasonWidth, status, p.Reason)
	}
	fmt.Fprintf(&b, "\nsummary: shown=%d bytes=%s collectable_bytes_in_shown=%s\n",
		len(plans), formatGCBytes(total), formatGCBytes(collectableTotal))
	fmt.Fprintf(&b, "         the age judgement protects the biggest directories by design: a big\n"+
		"         log is big BECAUSE it is new. Reclaim by size with %s.\n", sessionGCTrimRawLogFlag)
	fmt.Print(b.String())
	return 0
}

// runSessionGCTrimRawLog implements `gopi session gc --trim-raw-log SIZE
// [--apply] [--json]`. The dry run reports; the apply rewrites and reports the
// MEASURED result of each rewrite, never a prediction.
func runSessionGCTrimRawLog(opts sessionGCTrimOptions) int {
	root, err := scratch.SessionsDirRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: resolving the sessions root: %v\n", sessionSubcmdGC, err)
		return 1
	}
	plans, err := scratch.PlanRawLogTrim(root, opts.capBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdGC, err)
		return 1
	}
	// Biggest saving first: the operator reads the top of the list.
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].DropBytes > plans[j].DropBytes })

	candidates, refusals := 0, 0
	var dropTotal, overTotal int64
	for _, p := range plans {
		if p.Trim {
			candidates++
			dropTotal += p.DropBytes
			overTotal += p.Bytes
			continue
		}
		if p.Reason != scratch.SessionsRawLogWithinCap {
			refusals++
		}
	}

	if !opts.apply {
		out := sessionGCTrimText(root, opts.capBytes, plans, false, nil)
		if opts.json {
			blob, jerr := marshalGCJSON(sessionGCTrimDoc{
				Root: root, CapBytes: opts.capBytes, DryRun: true,
				Candidates: candidates, Refusals: refusals,
				OverCapBytes: overTotal, WouldFreeBytes: dropTotal,
			})
			if jerr != nil {
				fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdGC, jerr)
				return 1
			}
			fmt.Print(blob)
			return 0
		}
		fmt.Print(out)
		return 0
	}

	outcomes := make([]scratch.RawLogTrimOutcome, 0, candidates)
	var freed int64
	for _, p := range plans {
		if !p.Trim {
			continue
		}
		o, terr := scratch.TrimRawLog(p.Path, opts.capBytes)
		if terr != nil {
			// ONE file's failure must not stop the rest, and it must not be
			// silent either (Rule 15): it is named on stderr and the exit code
			// reports it.
			fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdGC, terr)
			continue
		}
		if o.Trimmed {
			freed += o.Before - o.After
		}
		outcomes = append(outcomes, o)
	}
	if opts.json {
		blob, jerr := marshalGCJSON(sessionGCTrimDoc{
			Root: root, CapBytes: opts.capBytes, DryRun: false,
			Candidates: candidates, Refusals: refusals,
			OverCapBytes: overTotal, WouldFreeBytes: dropTotal,
			FreedBytes: freed, Outcomes: outcomes,
		})
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "gopi session %s: %v\n", sessionSubcmdGC, jerr)
			return 1
		}
		fmt.Print(blob)
		return 0
	}
	fmt.Print(sessionGCTrimText(root, opts.capBytes, plans, true, outcomes))
	if len(outcomes) != candidates {
		fmt.Fprintf(os.Stderr, "gopi session %s: %d of %d rewrites did not land (each is named above)\n",
			sessionSubcmdGC, candidates-len(outcomes), candidates)
		return 1
	}
	return 0
}

// sessionGCTrimDoc is the JSON form of the trim report.
type sessionGCTrimDoc struct {
	Root           string                      `json:"root"`
	CapBytes       int64                       `json:"cap_bytes"`
	DryRun         bool                        `json:"dry_run"`
	Candidates     int                         `json:"candidates"`
	Refusals       int                         `json:"refusals"`
	OverCapBytes   int64                       `json:"over_cap_bytes"`
	WouldFreeBytes int64                       `json:"would_free_bytes"`
	FreedBytes     int64                       `json:"freed_bytes,omitempty"`
	Outcomes       []scratch.RawLogTrimOutcome `json:"outcomes,omitempty"`
}

// sessionGCTrimText renders the trim report. Every number in the apply half is
// the MEASURED result (stat before / stat after), never the plan's estimate —
// an apply that echoed its own prediction would be unable to report a rewrite
// that did not land.
func sessionGCTrimText(root string, capBytes int64, plans []scratch.RawLogTrimPlan,
	applied bool, outcomes []scratch.RawLogTrimOutcome) string {
	var b strings.Builder
	mode := "dry-run (nothing was written)"
	if applied {
		mode = "apply"
	}
	fmt.Fprintf(&b, "gopi session %s %s — %s\n", sessionSubcmdGC, sessionGCTrimRawLogFlag, mode)
	fmt.Fprintf(&b, "root:  %s\n", root)
	fmt.Fprintf(&b, "cap:   %s per *.raw.log\n", formatGCBytes(capBytes))
	fmt.Fprintf(&b, "files: %d raw log(s) under the tree\n\n", len(plans))

	printed, candidates := 0, 0
	var dropTotal, overTotal int64
	for _, p := range plans {
		if p.Reason == scratch.SessionsRawLogWithinCap {
			continue
		}
		if p.Trim {
			candidates++
			dropTotal += p.DropBytes
			overTotal += p.Bytes
		}
		if printed < sessionGCTrimTopLimit {
			status := "refuse"
			if p.Trim {
				status = "trim"
			}
			fmt.Fprintf(&b, "  %-*s  %10s  %10s  %6s  %s\n",
				sessionGCReasonWidth, status, formatGCBytes(p.Bytes),
				formatGCBytes(p.DropBytes), p.Age.Round(time.Hour), shortenGCFile(p.Path))
			printed++
		}
	}
	if candidates+countRefusals(plans) > printed {
		fmt.Fprintf(&b, "  … %d more not listed (the counts below cover ALL of them)\n",
			candidates+countRefusals(plans)-printed)
	}

	fmt.Fprintf(&b, "\nsummary: candidates=%d refusals=%d\n", candidates, countRefusals(plans))
	if !applied {
		fmt.Fprintf(&b, "         would free %s (of %s held by the over-cap files)\n",
			formatGCBytes(dropTotal), formatGCBytes(overTotal))
		fmt.Fprintf(&b, "         a dry run: NOTHING was written. Re-run with %s to trim.\n", sessionGCApplyFlag)
		return b.String()
	}
	var freed int64
	for _, o := range outcomes {
		if o.Trimmed {
			freed += o.Before - o.After
		}
	}
	fmt.Fprintf(&b, "         rewrote=%d freed=%s (measured before/after, not estimated)\n",
		len(outcomes), formatGCBytes(freed))
	for _, o := range outcomes {
		if !o.Trimmed {
			continue
		}
		fmt.Fprintf(&b, "           %s → %s (head=%s tail=%s)\n",
			formatGCBytes(o.Before), formatGCBytes(o.After),
			formatGCBytes(o.Head), formatGCBytes(o.Tail))
	}
	return b.String()
}

// countRefusals is the number of over-cap files the plan refused to rewrite.
func countRefusals(plans []scratch.RawLogTrimPlan) int {
	n := 0
	for _, p := range plans {
		if !p.Trim && p.Reason != scratch.SessionsRawLogWithinCap {
			n++
		}
	}
	return n
}

// shortenGCFile keeps the LAST two path segments: an id plus its file name is
// what a reader needs, and the constant root prefix is noise printed once above.
func shortenGCFile(path string) string {
	parts := strings.Split(path, string(os.PathSeparator))
	if len(parts) <= 2 {
		return path
	}
	return strings.Join(parts[len(parts)-2:], string(os.PathSeparator))
}

// parseGCSizeBytes reads a `--trim-raw-log` SIZE: a bare byte count, or a
// number with KiB/MiB/GiB (case-insensitive, optional space). It refuses
// everything else BY NAME — a silently-ignored unit would bound the tree to the
// wrong number (Rule 15).
func parseGCSizeBytes(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("needs a size (a byte count, or a spelling like 64MiB)")
	}
	units := []struct {
		suffix string
		mult   int64
	}{
		{"kib", sessionGCBytesPerKiB}, {"mib", sessionGCBytesPerKiB * sessionGCBytesPerKiB},
		{"gib", sessionGCBytesPerKiB * sessionGCBytesPerKiB * sessionGCBytesPerKiB},
		{"kb", sessionGCBytesPerKiB}, {"mb", sessionGCBytesPerKiB * sessionGCBytesPerKiB},
		{"gb", sessionGCBytesPerKiB * sessionGCBytesPerKiB * sessionGCBytesPerKiB},
		{"k", sessionGCBytesPerKiB}, {"m", sessionGCBytesPerKiB * sessionGCBytesPerKiB},
		{"g", sessionGCBytesPerKiB * sessionGCBytesPerKiB * sessionGCBytesPerKiB},
	}
	lower := strings.ToLower(s)
	mult := int64(1)
	for _, u := range units {
		if strings.HasSuffix(lower, u.suffix) {
			mult = u.mult
			lower = strings.TrimSpace(strings.TrimSuffix(lower, u.suffix))
			break
		}
	}
	n, err := strconv.ParseInt(lower, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a size (a byte count, or a spelling like 64MiB): %w", raw, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%q must be positive", raw)
	}
	return n * mult, nil
}

// marshalGCJSON is one indenting render for both arms (Rule 13: one spelling of
// the JSON contract).
func marshalGCJSON(v interface{}) (string, error) {
	blob, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render the report as JSON: %w", err)
	}
	return string(blob) + "\n", nil
}
