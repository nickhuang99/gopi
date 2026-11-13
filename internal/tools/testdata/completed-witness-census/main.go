package main

// Command completed-witness-census measures ONE population and answers ONE
// question about it, read-only against the LIVE ledger (GOPI.md Rule 38: a
// census figure names its generator and the commit it was measured at).
//
// THE POPULATION: every row whose status is `completed` and whose last write is
// at or after a CUTOFF commit's own committer time.
//
// THREE ANSWERS, and the third is the one this epic exists for. A delivered row
// is `witnessed`; a row whose own recorded reason is the clone channel's
// `nothing to deliver` exit is `concluded` — it completed on a MEASUREMENT the
// channel took (`git rev-list --count HEAD --not origin/master` → 0), which is a
// different fact from an unbacked claim; a row with NEITHER is `WITNESSLESS`.
// The production criterion (internal/tools/task_completion_witness.go) answers
// the same three, so the two can never disagree about one row.
//
// THE QUESTION (epic 01a182bf): does the row carry a DELIVERY WITNESS — any
// artefact outside the ledger that says this row produced something? Three
// witnesses are asked, in the order the production criterion asks them:
//
//	ref      a ref in the repo names the row's recorded deliverable branch
//	         (refs/heads/<branch> or refs/remotes/<any>/<branch>)
//	landed   the row's trail records a clone-channel PUSH naming a commit this
//	         repo still holds (DeliveryLandedEventName's sentence) — the arm
//	         that needs no directory and no trailer
//	commit   a commit reachable from any ref carries the row's id in its own
//	         `task:` trailer (internal/tools/git_commit_task_trailer.go) — the
//	         index that survives the ground's deletion and the ref's removal
//
// NO GROUND ARM. A worktree directory that still exists says nothing about
// whether the run PRODUCED anything — an untouched ground is exactly the false
// positive the epic is about — so neither the production criterion nor this
// census asks it. The ground is printed as CONTEXT, never counted as a witness.
//
// WHAT THE MEASUREMENT FOUND, and why ref-absence alone is not a defect: of the
// 46 completed rows after the cutoff, "no ref carries the branch" was true of
// ALL 46 — that is the NORMAL state after a delivered close-out (the clone
// channel publishes HEAD:master and finish-cleanup removes the address ref). 39
// were witnessed (landed record and/or `task:` trailer) and the other 7 carried
// the channel's own `nothing to deliver` conclusion. Zero were witnessless —
// INCLUDING row 01a18253-f7c4, whose surviving artefact is commit 20e832891:
//
//	$ git -C /home/nick/work/gopi show -s --format='%(trailers)' 20e832891
//	task: 01a18253-f7c4-7cf7-8cb3-868647a25401
//
// So the epic's third check ("no matching master commit") was answered by a
// commit that DOES match, and the row is class ③-adjacent but NOT lost: its ref
// and its ground are gone, its work is on master.
//
// It lives under testdata/ because it is a MAIN package: the Go tool ignores
// testdata directories, so it is never compiled by `go build ./...` or
// `go test ./internal/tools/`, while a reader can still run it verbatim (the
// shape internal/tools/testdata/delivery-proof-census.go established).
//
// RUN (read-only; writes no ledger row, no ref, no file):
//
//	go run internal/tools/testdata/completed-witness-census.go \
//	    --db /home/nick/.gopi/memory.db --repo /home/nick/work/gopi \
//	    --since 20c15007c74b8af04a5304fe9728bfbc8ed6cd01
//
// `--since` accepts any revision git can resolve in --repo; its committer
// epoch is the cutoff, so the population is anchored to a COMMIT rather than to
// a hand-typed date. `--verbose` prints every row, not only the witnessless
// ones.

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/nick/gopi/internal/devworkspace"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// censusGitTimeout bounds one git read (a wedged repo must not stall a census).
const censusGitTimeout = 60 * time.Second

// taskStatusCompleted is the one status value this census selects (the ledger's
// own spelling; Rule 1 — no bare keyword at the query site).
const taskStatusCompleted = "completed"

// deliveryLandedEventName is DeliveryLandedEventName's value in
// internal/tools/tasks.go — spelled here because this generator is a separate
// main package and must not import the tools layer (it would link the whole
// ledger machinery into a read-only census). A change to the production constant
// is caught by the census's own landed count going to 0, which is printed.
const deliveryLandedEventName = "clone-delivery-landed"

func main() {
	dbPath := flag.String("db", "", "the LIVE ledger to read (read-only)")
	repo := flag.String("repo", "", "the git repo whose refs and commit trailers are the witnesses")
	since := flag.String("since", "", "a revision in --repo; its committer time is the cutoff")
	verbose := flag.Bool("verbose", false, "print every row measured, not only the witnessless ones")
	flag.Parse()
	if strings.TrimSpace(*dbPath) == "" || strings.TrimSpace(*repo) == "" || strings.TrimSpace(*since) == "" {
		fmt.Fprintln(os.Stderr, "completed-witness-census: --db, --repo and --since are all required — a census must name its ledger, its witness sources and its cutoff")
		os.Exit(2)
	}
	cutoff, cutoffAt := commitEpoch(*repo, *since)
	fmt.Printf("# completed-witness census — measured at %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("# generator: go run internal/tools/testdata/completed-witness-census.go --db %s --repo %s --since %s\n",
		*dbPath, *repo, *since)
	fmt.Printf("# cutoff: %s committed at %s (epoch %d) — rows completed at/after it\n", *since, cutoffAt, cutoff)

	rows, err := completedRows(*dbPath, cutoff)
	if err != nil {
		fmt.Fprintf(os.Stderr, "completed-witness-census: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("# rows measured: %d\n\n", len(rows))

	refs := liveRefs(*repo)
	named := commitNamedTaskIDs(*repo)
	landed := landedTips(*dbPath)
	fmt.Printf("# witness sources: %d ref(s) in %s; %d row id(s) named by a `task:` commit trailer reachable from any ref; %d row(s) with a landed record\n\n",
		len(refs), *repo, len(named), len(landed))

	var witnessless, refAndCommitAbsent []rowFinding
	counts := map[string]int{}
	for _, r := range rows {
		f := rowFinding{Row: r}
		var got []string
		hasRef, hasCommit := false, named[r.TaskID] != ""
		landedTip := landed[r.TaskID]
		hasLanded := landedTip != "" && commitReadable(*repo, landedTip)
		if r.Branch != "" {
			for ref := range refs {
				if strings.HasSuffix(ref, "/"+r.Branch) {
					got = append(got, "ref:"+ref)
					hasRef = true
					break
				}
			}
		}
		if hasLanded {
			got = append(got, "landed:"+short(landedTip))
		}
		if hasCommit {
			got = append(got, "commit:"+short(named[r.TaskID]))
		}
		f.Witness = got
		key := "witnessed"
		switch {
		case len(got) > 0:
			// witnessed — the ordinary delivered close-out
		case concludedReason(r.Reason):
			key = "concluded"
		default:
			key = "WITNESSLESS"
			witnessless = append(witnessless, f)
		}
		counts[key]++
		if !hasRef {
			counts["no-ref"]++
		}
		if !hasCommit {
			counts["no-commit"]++
		}
		if !hasLanded {
			counts["no-landed"]++
		}
		// THE EPIC'S NARROWER CLASS: the row records a deliverable branch, NO
		// ref carries it, and NO reachable commit names the row. This is the
		// exact sentence row 01a18253-f7c4 was filed on.
		if r.Branch != "" && !hasRef && !hasCommit && !hasLanded {
			counts["ref-and-commit-absent"]++
			refAndCommitAbsent = append(refAndCommitAbsent, f)
		}
		if *verbose {
			fmt.Printf("%s\t%s\tref=%v commit=%v\t%s\n", r.TaskID, key, hasRef, hasCommit, strings.Join(got, " "))
		}
	}
	fmt.Printf("witnessed:               %d\n", counts["witnessed"])
	fmt.Printf("concluded (measured):    %d\n", counts["concluded"])
	fmt.Printf("WITNESSLESS:             %d\n", counts["WITNESSLESS"])
	fmt.Printf("no ref carries the branch: %d\n", counts["no-ref"])
	fmt.Printf("no landed record (readable commit): %d\n", counts["no-landed"])
	fmt.Printf("no `task:` trailer commit: %d\n", counts["no-commit"])
	fmt.Printf("BOTH ABSENT (ref + commit): %d\n\n", counts["ref-and-commit-absent"])
	if len(refAndCommitAbsent) > 0 {
		fmt.Println("## the strict class: branch ref absent AND no commit names the row")
		for _, f := range refAndCommitAbsent {
			fmt.Printf("%s\t%s\tbranch=%s\tworktree=%s\n",
				f.Row.TaskID, time.Unix(int64(f.Row.EndedAt), 0).Format("2006-01-02 15:04:05"),
				orNone(f.Row.Branch), orNone(f.Row.Worktree))
		}
		fmt.Println()
	}
	if len(witnessless) == 0 {
		fmt.Println("no witnessless completed row in this population")
		return
	}
	sort.Slice(witnessless, func(i, j int) bool { return witnessless[i].Row.EndedAt < witnessless[j].Row.EndedAt })
	fmt.Println("## the witnessless rows (row id, completion time, recorded branch, recorded worktree, run method)")
	for _, f := range witnessless {
		fmt.Printf("%s\t%s\tbranch=%s\tworktree=%s\trun=%s\n",
			f.Row.TaskID, time.Unix(int64(f.Row.EndedAt), 0).Format("2006-01-02 15:04:05"),
			orNone(f.Row.Branch), orNone(f.Row.Worktree), orNone(f.Row.RunMethod))
	}
}

// rowFinding pairs a ledger row with the witnesses that were found for it.
type rowFinding struct {
	Row     ledgerRow
	Witness []string
}

// ledgerRow is the ledger-side fact set this census reads, and nothing more.
type ledgerRow struct {
	TaskID    string
	Branch    string
	Worktree  string
	RunMethod string
	Reason    string
	EndedAt   float64
}

// conclusionMarkers are the sentences the clone channel COMPOSES for its
// nothing-to-deliver exit (internal/devworkspace/delivery_anchor.go's
// deliveryEmptyRefusalFmt and cmd/agent/devworkspace_channel.go's sentinel).
// A row whose recorded reason carries one completed on that measurement — it is
// `concluded`, not unproven. Two spellings are listed because the exit has two
// arms (nothing to push / nothing to fetch); a THIRD arm would make this count
// drop, which is printed, so the drift is visible rather than silent.
var conclusionMarkers = []string{
	"carries no commit to deliver",
	// Composed from the ONE prefix definition (devworkspace.CloneChannelPrefix):
	// the marker itself is spelled here because this census reads a row's TEXT,
	// and a second copy of the prefix would be a copy that drifts.
	devworkspace.CloneChannelPrefix + "nothing to deliver",
}

// concludedReason reports whether a row's recorded reason is the channel's
// measured conclusion rather than an unbacked claim.
func concludedReason(reason string) bool {
	for _, m := range conclusionMarkers {
		if strings.Contains(reason, m) {
			return true
		}
	}
	return false
}

// completedRows lists the rows this census measures: status=completed with a
// last write at/after the cutoff, newest first. `updated_at` is the column the
// terminal write moves, so it is the one that answers "when did this row close".
func completedRows(dbPath string, cutoff int64) ([]ledgerRow, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=query_only(true)")
	if err != nil {
		return nil, fmt.Errorf("open %s read-only: %w", dbPath, err)
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT task_id, deliverable, run_method, reason, updated_at FROM tasks
		 WHERE status = ? AND updated_at >= ? ORDER BY updated_at ASC`,
		taskStatusCompleted, float64(cutoff))
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", dbPath, err)
	}
	defer rows.Close()
	var out []ledgerRow
	for rows.Next() {
		var r ledgerRow
		var deliverable string
		if err := rows.Scan(&r.TaskID, &deliverable, &r.RunMethod, &r.Reason, &r.EndedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		var d struct {
			Branch   string `json:"branch"`
			Worktree string `json:"worktree"`
		}
		// A malformed deliverable blob is not an error here: the row is still
		// measured, it simply names no branch and no worktree (lenient parse,
		// the same contract taskDeliverableParse has).
		_ = json.Unmarshal([]byte(deliverable), &d)
		r.Branch, r.Worktree = strings.TrimSpace(d.Branch), strings.TrimSpace(d.Worktree)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// landedTips reads every row's NEWEST clone-channel landed record and returns
// the commit it names (TaskID → sha). It parses the record with the production
// grammar's own shape: the sentence carries " at <sha> " — a record whose sha
// cannot be found is skipped rather than guessed (the production reader refuses
// an unparseable record for exactly this reason).
func landedTips(dbPath string) map[string]string {
	out := map[string]string{}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=query_only(true)")
	if err != nil {
		fmt.Fprintf(os.Stderr, "completed-witness-census: landed records: %v\n", err)
		return out
	}
	defer db.Close()
	rows, err := db.Query(
		`SELECT task_id, detail FROM task_events WHERE event = ? ORDER BY id DESC`,
		deliveryLandedEventName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "completed-witness-census: landed records: %v\n", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var taskID, detail string
		if err := rows.Scan(&taskID, &detail); err != nil {
			fmt.Fprintf(os.Stderr, "completed-witness-census: landed record scan: %v\n", err)
			return out
		}
		if _, seen := out[taskID]; seen {
			continue // newest first: the LAST push is the one the row ended on
		}
		if sha := landedRecordSHA(detail); sha != "" {
			out[taskID] = sha
		}
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "completed-witness-census: landed records ended badly: %v\n", err)
	}
	return out
}

// landedRecordSHA pulls the commit sha out of a landed record's sentence. The
// production record reads "… delivered <branch> from <dir> at <sha> — pushed
// master to origin after N attempt(s)", so the sha is the long hex token that
// follows " at ". A record without one is unparseable and is skipped.
func landedRecordSHA(detail string) string {
	const at = " at "
	i := strings.Index(detail, at)
	if i < 0 {
		return ""
	}
	rest := detail[i+len(at):]
	end := strings.IndexAny(rest, " \t\u2014")
	if end < 0 {
		end = len(rest)
	}
	tok := rest[:end]
	if len(tok) < 40 {
		return ""
	}
	for _, r := range tok {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return ""
		}
	}
	return tok
}

// commitReadable reports whether the repo still holds the commit AT ALL — a
// record about a vanished object is not evidence that anything survived.
func commitReadable(repo, sha string) bool {
	if strings.TrimSpace(sha) == "" {
		return false
	}
	out := gitOut(repo, "cat-file", "-t", sha)
	return strings.TrimSpace(out) == "commit"
}

// commitEpoch resolves a revision's committer time — the cutoff, anchored to a
// commit so a reader can re-derive the exact population.
func commitEpoch(repo, rev string) (int64, string) {
	out := gitOut(repo, "show", "-s", "--format=%ct %cI", rev)
	f := strings.Fields(out)
	if len(f) != 2 {
		fmt.Fprintf(os.Stderr, "completed-witness-census: cannot resolve %s in %s: %q\n", rev, repo, out)
		os.Exit(1)
	}
	n, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "completed-witness-census: committer time of %s is not a number: %q\n", rev, f[0])
		os.Exit(1)
	}
	return n, f[1]
}

// liveRefs lists every ref name the repo holds (heads, remotes, tags) — one
// enumeration, so per-row lookups are a map hit and not a fork.
func liveRefs(repo string) map[string]bool {
	out := gitOut(repo, "for-each-ref", "--format=%(refname)")
	refs := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			refs[line] = true
		}
	}
	return refs
}

// commitNamedTaskIDs maps every row id named by a `task:` trailer on a commit
// reachable from ANY ref to that commit's sha — the witness that survives both
// the ground's deletion and the address ref's removal.
func commitNamedTaskIDs(repo string) map[string]string {
	out := gitOut(repo, "log", "--all", "--format=%H%x09%(trailers:key=task,valueonly)")
	named := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		sha, id := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if id == "" || sha == "" {
			continue
		}
		// The trailer may carry several ids (a run that landed a fix and its
		// regression in one commit); each is a witness for its own row.
		for _, one := range strings.Fields(id) {
			if _, seen := named[one]; !seen {
				named[one] = sha
			}
		}
	}
	return named
}

// gitOut runs ONE git read and returns its stdout, trimmed. A failed read is
// reported and answered with "" — never silently treated as "nothing there",
// which is why every caller above distinguishes an empty map from a failure by
// the printed source counts.
func gitOut(repo string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), censusGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "completed-witness-census: git %s: %v\n", strings.Join(args, " "), err)
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

func short(sha string) string {
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}
