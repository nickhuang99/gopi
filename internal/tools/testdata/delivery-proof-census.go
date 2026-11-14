package main

// Command delivery-proof-census re-takes the delivery proof for every row that
// carries a `delivery-proof-unverified` record and reports, per row, what the
// arm answers NOW (GOPI.md Rule 38: a census figure names its generator and the
// commit it was measured at; a figure a reader acts on must be re-derivable).
//
// It lives under testdata/ because it is a MAIN package: the Go tool ignores
// testdata directories, so this file is never compiled by `go build ./...` or
// `go test ./internal/tools/`, while a reader can still run it verbatim. Nothing
// in the build executes it — the arms it measures judge the LEDGER, never this
// program — the shape internal/standards/testdata/config-anchor-census.go had
// with its table, until that generator and its table were deleted with the
// documentation-citation family in 78bdf05a (2026-11-05).
//
// WHY A PROGRAM AND NOT A TEST (measured, memory #14229): a census run as a go
// test measures NOTHING, because the test harness pins GOPI_MEMORY_DB to the
// shared READ-ONLY SAMPLE ledger — every row then answers "records no deliverable
// branch" and the numbers mean nothing. This program reads the LIVE ledger by
// path and calls the REAL `tools.TaskDeliveryProof`, which is also the Rule 31
// evidence for the ground-gone repair: the surface is executed, not described.
//
// RUN (read-only against the live ledger; prints the census):
//
//	go run internal/tools/testdata/delivery-proof-census.go \
//	    --db /home/user/.gopi/memory.db
//
// Add `--landed` to also answer, per row, whether the row's own accepted tip is
// an ancestor of the ledger repo's origin/master (`git merge-base --is-ancestor`
// in --repo, default /home/user/work/gopi) — that is the "已落地比例" figure.
//
//	go run internal/tools/testdata/delivery-proof-census.go \
//	    --db /home/user/.gopi/memory.db --landed --repo /home/user/work/gopi
//
// It WRITES NOTHING: no ledger row, no ref, no file. Its output is the figure.

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nick/gopi/internal/tools"
)

// censusGitTimeout bounds one `git merge-base` probe.
const censusGitTimeout = 30 * time.Second

func main() {
	dbPath := flag.String("db", "", "the LIVE ledger to read (GOPI_MEMORY_DB), read-only")
	repo := flag.String("repo", "", "the repo whose origin/master answers --landed (e.g. /home/user/work/gopi)")
	limit := flag.Int("limit", 0, "measure at most N rows, newest first (0 = every row carrying the record)")
	landed := flag.Bool("landed", false, "also report whether each row's accepted tip is an ancestor of origin/master")
	flag.Parse()
	if strings.TrimSpace(*dbPath) == "" {
		fmt.Fprintln(os.Stderr, "delivery-proof-census: --db is required — a census must name the ledger it read")
		os.Exit(2)
	}
	rows, err := unverifiedRows(*dbPath, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "delivery-proof-census: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("# delivery-proof census — measured at %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("# generator: go run internal/tools/testdata/delivery-proof-census.go --db %s --landed --repo %s\n",
		*dbPath, *repo)
	fmt.Printf("# rows carrying %s: %d\n\n", tools.DeliveryProofUnverifiedEvent, len(rows))

	states := map[string]int{}
	landedByState := map[string]int{}
	tips := map[string]string{}
	// measured / unnamable / onMaster are the THREE denominators a reader needs
	// before acting on "已落地比例": how many rows the ancestry arm could look at,
	// how many it could not, and how many it found on the integration branch.
	measured, unnamable, onMaster := 0, 0, 0
	for _, id := range rows {
		out, err := tools.TaskDeliveryProof(id)
		if err != nil {
			fmt.Printf("%s\terror\t%v\n", id, err)
			continue
		}
		states[out.State]++
		short := out.Tip
		if len(short) > 9 {
			short = short[:9]
		}
		fmt.Printf("%s\t%s\ttip=%s\n", id, out.State, short)
		tips[id] = out.Tip
		if !*landed || strings.TrimSpace(*repo) == "" {
			continue
		}
		if strings.TrimSpace(out.Tip) == "" {
			// NO TIP IS NAMEABLE: the row is not "not landed", it is not
			// MEASURABLE by ancestry — and conflating the two is how a census
			// reports a percentage of rows it never looked at.
			unnamable++
			continue
		}
		measured++
		if isAncestor(*repo, out.Tip) {
			landedByState[out.State]++
			onMaster++
		}
	}
	fmt.Println()
	keys := make([]string, 0, len(states))
	for k := range states {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("state %-12s %d\n", k, states[k])
	}
	if *landed && strings.TrimSpace(*repo) != "" {
		fmt.Println()
		fmt.Printf("rows measured for ancestry: %d of %d (unnamable tip: %d)\n", measured, len(rows), unnamable)
		fmt.Printf("landed on refs/remotes/origin/master: %d of %d measured rows (%.1f%%)\n",
			onMaster, measured, pct(onMaster, measured))
		for _, k := range keys {
			if landedByState[k] > 0 {
				fmt.Printf("  landed & state=%-12s %d\n", k, landedByState[k])
			}
		}
	}
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}

// unverifiedRows lists (newest first) every DISTINCT row that carries a
// delivery-proof-unverified record — the population this census measures, taken
// from the ledger itself rather than from a caller's list.
func unverifiedRows(dbPath string, limit int) ([]string, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=query_only(true)")
	if err != nil {
		return nil, fmt.Errorf("open %s read-only: %w", dbPath, err)
	}
	defer db.Close()
	q := `SELECT task_id, MAX(id) AS newest FROM task_events WHERE event = ? GROUP BY task_id ORDER BY newest DESC`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.Query(q, tools.DeliveryProofUnverifiedEvent)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", dbPath, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		var newest int64
		if err := rows.Scan(&id, &newest); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// isAncestor answers whether commit is on the repo's origin/master — the
// "已落地" half of the figure, measured by git in the named repo.
func isAncestor(repo, commit string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), censusGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "merge-base", "--is-ancestor", commit, "refs/remotes/origin/master")
	return cmd.Run() == nil
}
