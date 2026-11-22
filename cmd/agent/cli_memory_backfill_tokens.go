package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nickhuang99/gopi/internal/tools"
)

// memoryBackfillVerb is the sub-verb word dispatched by `gopi memory <sub>`
// for the one-shot jieba_token re-derivation. It is a constant because it is a
// CLI spelling compared in more than one place (Rule 1b/14).
const memoryBackfillVerb = "backfill-tokens"

// runMemoryBackfillTokens implements `gopi memory backfill-tokens`.
//
// WHY THIS VERB EXISTS. The jieba_token column changed meaning: it used to
// hold a comma-joined list of 2-char CJK grams and now holds the words a real
// segmenter (internal/cjkseg, gse) finds. New and updated rows derive the new
// value automatically, but a row written by an older binary keeps its old gram
// list — findable by the token index only at the granularity of a substring
// scan, never at the segmenter's word boundary. This command is the ONE
// re-derivation path for those rows.
//
// WHY IT IS A SUB-VERB AND NOT A SCRIPT (Rule 35). The alternative — a
// backfill script beside the binary — would be a second implementation of
// "what the token column should contain", free to drift from the writer's
// rule, and a new persistent artifact whose lifecycle nobody owns. The
// capability belongs to gopi, so it lives in gopi's Go code and is invoked
// explicitly by whoever upgraded the binary; NOTHING schedules it, and there
// is no daemon, cron line, state file or lock. The ledger it writes is the one
// gopi already owns and already resolves from GOPI_MEMORY_DB / the active
// profile.
//
// The work itself is a bounded UPDATE over distilled_qa (see
// tools.BackfillJiebaTokens): read the row's own source text, re-derive with
// the same segmenter the writer uses, write ONLY the jieba_token column, and
// reindex the FTS row so the index agrees. It is idempotent — a second run
// changes nothing — so an interrupted run is repaired by running it again.
//
// -h is a real run of this surface (Rule 31: a changed surface is executed),
// and --dry-run reports the same scan/change split without writing.
func runMemoryBackfillTokens(args []string) {
	fs := flag.NewFlagSet(memoryBackfillVerb, flag.ExitOnError)
	dry := fs.Bool("dry-run", false, "report what would change; write nothing")
	// --db is ADMITTED here because the group page has always advertised it
	// (`gopi memory backfill-tokens [--dry-run] [--db PATH]`) while the parser
	// refused it — measured 2026-11-06: `--db /tmp/x.db --dry-run` exited 2 with
	// "flag provided but not defined: -db", so the one documented way to point
	// the verb at another ledger did not exist. The path is threaded through the
	// SAME resolution the siblings use (Rule 14).
	dbFlag := fs.String("db", "", "ledger path (default: the resolved profile ledger)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	ctx := context.Background()

	path := memoryLedgerEffectivePath(*dbFlag)
	// Progress goes to STDERR as PLAIN UNBUFFERED writes so the one-line summary
	// on stdout stays a single parseable record AND — the point of this whole
	// file — a pipe or a redirect sees every line as it happens. There is no
	// bufio wrapper and no isatty check anywhere on this path: os.Stderr is
	// unbuffered at the syscall level, and a line destined for `tee` or a log
	// file must appear at the same instant it would on a terminal.
	//
	// THE LINES ARE TIME-DRIVEN, NOT BATCH-DRIVEN (measured 2026-11-21, task
	// 01a1fc80). Emitting on a batch stride put the first line 62 s into a 99 s
	// run and made the gap grow with every slower page. The heartbeat owns its
	// own ticker, so it speaks during a slow page and before the first one
	// returns; the callback below only RECORDS the newest measurement.
	beat := newMemoryBackfillHeartbeat(memoryBackfillVerb, os.Stderr,
		memoryBackfillHeartbeatEvery, time.Now)
	// THE LEDGER GUARD (cli_memory_ledger_guard.go): --dry-run READS, so an
	// explicitly named --db that is not there is a refusal naming the path (the
	// read-only opener would fail anyway, but with a lower-level sentence that
	// never mentions which ledger is active). A non-dry run is a WRITER and may
	// create — and then says it did.
	if *dry {
		if refusal := memoryLedgerReadOnlyRefusal(memoryDisplayName(memoryBackfillVerb), *dbFlag); refusal != "" {
			fmt.Fprint(os.Stderr, refusal)
			os.Exit(1)
		}
	}
	// The creation notice is COMPUTED before the open (the only moment the
	// absence can be observed) and PRINTED after it succeeded: a sentence
	// claiming "created" must not appear on a run that then failed to open.
	createdNotice := ""
	if !*dry {
		createdNotice = memoryLedgerWriterNotice(memoryDisplayName(memoryBackfillVerb), *dbFlag, path)
	}
	db, err := tools.OpenMemoryLedger(path, *dry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "memory %s: open %s: %v\n", memoryBackfillVerb, path, err)
		os.Exit(1)
	}
	if createdNotice != "" {
		fmt.Fprintln(os.Stderr, createdNotice)
	}
	defer db.Close() //stylegate:ignore process-lifetime ledger handle closed on return

	// t=0: announce BEFORE any page is read, so the first thing the command says
	// is what it is about to traverse — the whole point of the fix is that a
	// 99-second run is never silent for its first minute.
	span, spanErr := tools.JiebaBackfillPageExtent(ctx, db)
	beat.announce(memoryBackfillVerb, span, spanErr)
	beat.start()
	// A FAILED RUN STILL STOPS THE BEAT. Every exit path below halts it, so an
	// error sentence can never race a tick that outlived the scan.

	if *dry {
		scanned, would, err := tools.ScanJiebaTokensWithProgress(ctx, db, beat.observe)
		if err != nil {
			beat.halt()
			fmt.Fprintf(os.Stderr, "memory %s: %v\n", memoryBackfillVerb, err)
			os.Exit(1)
		}
		beat.closeout()
		fmt.Printf("memory %s: dry-run — %d row(s) scanned, %d would be re-derived (db=%s)\n",
			memoryBackfillVerb, scanned, would, path)
		return
	}
	scanned, changed, err := tools.BackfillJiebaTokensWithProgress(ctx, db, beat.observe)
	if err != nil {
		beat.halt()
		fmt.Fprintf(os.Stderr, "memory %s: %v\n", memoryBackfillVerb, err)
		os.Exit(1)
	}
	beat.closeout()
	fmt.Printf("memory %s: %d row(s) scanned, %d jieba_token value(s) re-derived (db=%s)\n",
		memoryBackfillVerb, scanned, changed, path)
}
