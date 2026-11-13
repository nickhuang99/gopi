package tools

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/taskverdict"
)

// Reframe telemetry (SPEC-reframe-evaluate-merge.md §4, §7.7): the anti-
// bureaucracy loop is MEASURED, not assumed. Every task(action=reframe)
// appends ONE append-only task_events row (event=reframe) carrying the
// verdict and its payload cardinality; the distribution and the per-task
// reframe count are read back from that trail (`gopi task verdicts`). The
// ledger itself then shows that an epic was already judged and with what —
// the zero-information re-decision the SPEC documents as the loop's root
// cause (a task with count > 1 is the visible symptom).
const (
	// ReframeEventName is the task_events.event value for a reframe record.
	ReframeEventName = "reframe"

	// reframeVerdictFieldPrefix opens the verdict field in the event detail
	// ("verdict=TOO_BIG items=3"). The detail is greppable plain text (the
	// task_events trail's convention), not JSON.
	reframeVerdictFieldPrefix = "verdict="

	// reframeItemsFieldPrefix opens the payload-cardinality field: how many
	// roadmap items the verdict carried (0 for DOABLE/IMPOSSIBLE). It is the
	// "does the exit carry new work" quantity the hard constraints protect.
	reframeItemsFieldPrefix = "items="

	// reframeOriginFieldPrefix opens the provenance field: which session/process
	// dispatched THIS record. One dispatch = one row, so the count is a real
	// dispatch count — but the row must also be ATTRIBUTABLE: an inherited
	// wrapper identity once let nested processes (a `go test ./internal/agent/`
	// run inside a task run) write their rows under the wrapper's task_id, which
	// is what made a 2-dispatch task carry 22 rows (session 01a0898e,
	// 2026-09-10). Origin is that missing half: rows stamped
	// `origin=<session>/<pid>` show where a row really came from, while
	// childenv.go stops the identity from being inherited at all.
	reframeOriginFieldPrefix = "origin="

	// ReframeRoadmapTag marks the child tasks the harness auto-enqueues for
	// a TOO_BIG roadmap (one task per item) — the operator and telemetry can
	// separate harness-decomposed children from independent submissions.
	ReframeRoadmapTag = "reframe-roadmap"

	// reframeVerdictsPerTaskCap bounds the per-task section of the `task
	// verdicts` render (the repeated-reframe list must not print unbounded).
	reframeVerdictsPerTaskCap = 50

	// reframeVerdictsTaskCap bounds how many distinct tasks the per-task
	// section lists (worst-case offenders first).
	reframeVerdictsTaskCap = 20

	// reframeVerdictUnknown buckets a reframe record the render cannot
	// attribute to a vocabulary verdict — an absent `verdict=` field, or a
	// token outside taskverdict.Valid (retired/corrupt; see
	// parseReframeVerdict) — never silently dropped, never attributed to a
	// real verdict (Rule 15). A RENDER-only bucket: it is not a wire value, so
	// taskverdict.Valid(reframeVerdictUnknown) must stay false.
	reframeVerdictUnknown = "UNKNOWN"

	// taskOriginNoSession is the origin's session half when the writing process
	// has no session of its own (CLI, test binary): the pid alone still
	// identifies the writer, which is what the field is for.
	taskOriginNoSession = "no-session"
)

// reframeEventDetail renders the audit detail of one reframe record. origin is
// the dispatching session/process identity ("" = unknown, field omitted — a
// legacy caller). The detail stays greppable plain text (task_events'
// convention), never JSON.
func reframeEventDetail(verdict string, items int, origin string) string {
	detail := reframeVerdictFieldPrefix + verdict + " " + reframeItemsFieldPrefix + strconv.Itoa(items)
	if origin = strings.TrimSpace(origin); origin != "" {
		detail += " " + reframeOriginFieldPrefix + origin
	}
	return detail
}

// parseReframeOrigin pulls the provenance out of a reframe event detail ("" when
// absent — rows written before the origin field existed).
func parseReframeOrigin(detail string) string {
	for _, f := range strings.Fields(detail) {
		if v, ok := strings.CutPrefix(f, reframeOriginFieldPrefix); ok {
			return v
		}
	}
	return ""
}

// parseReframeVerdict pulls the verdict out of a reframe event detail. It
// returns "" — the caller counts the record as unknown — when the detail is
// malformed: either the verdict field is absent, or the token is OUT OF
// VOCABULARY (a retired verdict like the pre-2026-09-10 NEEDS_RESEARCH, or a
// corrupt token from a foreign/hand-edited writer). The vocabulary check is
// taskverdict.Valid — the same single source of truth the harness applies at
// the write gate (TaskReframeRecord, internal/agent/reframe.go): rows written
// before a verdict was retired, or by a writer that bypassed the gate, are
// append-only and cannot be re-validated on write, so the READER must classify
// them the way the writer would have. Without it a non-empty unknown token
// became its own distribution key, counted by the header total but absent from
// the table (Rule 15: a recorded row must never be silently dropped).
func parseReframeVerdict(detail string) string {
	for _, f := range strings.Fields(detail) {
		if v, ok := strings.CutPrefix(f, reframeVerdictFieldPrefix); ok {
			if !taskverdict.Valid(v) {
				return ""
			}
			return v
		}
	}
	return ""
}

// reframeVerdictTableOrder returns the verdicts the distribution table must
// render, in render order: the vocabulary (taskverdict.All — schema order) then
// the UNKNOWN bucket, and finally ANY residual key observed in dist but absent
// from both, sorted for determinism.
//
// The invariant this encodes: every counted dist key is printable. The reader
// (parseReframeVerdict) already funnels out-of-vocabulary tokens into UNKNOWN,
// but the table's accounting must not DEPEND on that — a key that reached dist
// by any route (a future reader, a hand-built map) still renders, so
// sum(table counts) == header total can never be broken by invisibility
// (Rule 15; regression test TestReframeVerdicts_OutOfVocabularyLandsInUnknownEndToEnd).
func reframeVerdictTableOrder(dist map[string]int) []string {
	var order []string
	seen := make(map[string]bool, len(dist))
	for _, v := range append(taskverdict.All(), reframeVerdictUnknown) {
		seen[v] = true
		if dist[v] > 0 {
			order = append(order, v)
		}
	}
	var residual []string
	for v, n := range dist {
		// "seen" covers the whole vocabulary (counted or not): a vocabulary
		// verdict is rendered in vocabulary order above, never in the tail.
		if n > 0 && !seen[v] {
			residual = append(residual, v)
		}
	}
	sort.Strings(residual)
	return append(order, residual...)
}

// parseReframeItems pulls the payload cardinality out of a reframe event
// detail (0 when absent/unparseable).
func parseReframeItems(detail string) int {
	for _, f := range strings.Fields(detail) {
		if v, ok := strings.CutPrefix(f, reframeItemsFieldPrefix); ok {
			n, err := strconv.Atoi(v)
			if err != nil {
				return 0
			}
			return n
		}
	}
	return 0
}

// TaskReframeRecord appends ONE reframe telemetry row. An empty task id or an
// invalid verdict is an error (the caller records it) — telemetry must never
// invent a verdict or attribute a reframe to no task (Rule 15: never silent).
//
// origin is the DISPATCHING process's identity — the caller's in-process session
// id, never an env lookup (an inherited env is exactly how a nested process came
// to write under a row it does not own). Empty means "no session": the row is
// then stamped pid-only, still identifiable.
func TaskReframeRecord(taskID, verdict string, items int, origin string) error {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return fmt.Errorf("task: reframe telemetry needs a task_id (the ledger row the reframe belongs to)")
	}
	if !taskverdict.Valid(verdict) {
		return fmt.Errorf("task: reframe telemetry: invalid verdict %q", verdict)
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return fmt.Errorf("task: reframe telemetry: %w", err)
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, ReframeEventName, reframeEventDetail(verdict, items, withProcessIdentity(origin)))
	return nil
}

// withProcessIdentity completes an origin with this process's pid, so two
// writers sharing a session id (or none) stay distinguishable: "<session>/<pid>"
// or "no-session/<pid>". The pid is the un-forgeable half — a nested process
// cannot claim another's pid (reframe-telemetry-doublecount, 2026-09-10).
func withProcessIdentity(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return taskOriginNoSession + "/" + strconv.Itoa(os.Getpid())
	}
	return origin + "/" + strconv.Itoa(os.Getpid())
}

// TaskChildren lists the rows that name parentID as their parent.
//
// For a TOO_BIG reframe this returns only the FIRST roadmap item: since the
// serial-chain change (epic 01a10a24, operator ruling 2026-10-05) the roadmap
// is enqueued as a CHAIN, so item 1's parent is the epic row and item i's
// parent is item i-1 — the epic row has exactly one child, and the rest are
// reachable by walking the chain one link at a time (see reframeSubmitFn's
// callers in internal/agent/reframe.go). Ordered by insertion (rowid), which
// for one parent's children is the submit order.
//
// Used by tests (and by `task show` via taskChildrenWithDB) to prove a
// split's rows are real, claimable, parent-linked rows. A reader that needs a
// SPECIFIC row (walking a chain, following one link) uses TaskShowRow instead
// of scanning this list.
func TaskChildren(parentID string) ([]*Task, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil, fmt.Errorf("task: children: parent task_id must not be empty")
	}
	ctx := context.Background()
	// READ-ONLY OPENER — this face is a PURE READ: its SELECTs are the whole of
	// what it does, and it calls no writer (openTasksDBReadOnly skips the tasks DDL
	// batch and takes no BEGIN IMMEDIATE, so a reader never queues on the writer
	// that holds the ledger).
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return taskChildrenWithDB(ctx, db, parentID)
}

// taskChildrenWithDB is the open-handle form of TaskChildren (Rule 2: one
// query, two callers — the show renderer already holds the handle).
func taskChildrenWithDB(ctx context.Context, db *sql.DB, parentID string) ([]*Task, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil, fmt.Errorf("task: children: parent task_id must not be empty")
	}
	rows, err := db.QueryContext(ctx,
		`SELECT `+taskSelectColumns+` FROM tasks WHERE parent_task_id = ? ORDER BY id ASC`,
		parentID)
	if err != nil {
		return nil, fmt.Errorf("task: children: %w", err)
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("task: children: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("task: children: %w", err)
	}
	return out, nil
}

// TaskReframeStats renders the telemetry view for `gopi task verdicts`: the
// verdict distribution (with the total roadmap items each verdict produced)
// plus the per-task reframe counts. Two observable facts: a task reframed
// more than once (count > 1) is the bureaucracy loop's visible symptom, and a
// distribution that is not 100% DOABLE proves the honest exits really happen.
func TaskReframeStats() (string, error) {
	ctx := context.Background()
	// READ-ONLY OPENER — this face is a PURE READ: its SELECTs are the whole of
	// what it does, and it calls no writer (openTasksDBReadOnly skips the tasks DDL
	// batch and takes no BEGIN IMMEDIATE, so a reader never queues on the writer
	// that holds the ledger).
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, COALESCE(detail,'') FROM task_events
		 WHERE event = ? ORDER BY id ASC`, ReframeEventName)
	if err != nil {
		return "", fmt.Errorf("task: verdicts: %w", err)
	}
	defer rows.Close()

	dist := map[string]int{}
	items := map[string]int{}
	byTask := map[string][]string{}
	origins := map[string]map[string]bool{}
	var order []string
	total := 0
	for rows.Next() {
		var taskID, detail string
		if err := rows.Scan(&taskID, &detail); err != nil {
			return "", fmt.Errorf("task: verdicts: %w", err)
		}
		v := parseReframeVerdict(detail)
		if v == "" {
			v = reframeVerdictUnknown
		}
		dist[v]++
		items[v] += parseReframeItems(detail)
		if _, seen := byTask[taskID]; !seen {
			order = append(order, taskID)
		}
		byTask[taskID] = append(byTask[taskID], v)
		if o := parseReframeOrigin(detail); o != "" {
			if origins[taskID] == nil {
				origins[taskID] = map[string]bool{}
			}
			origins[taskID][o] = true
		}
		total++
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("task: verdicts: %w", err)
	}

	var b strings.Builder
	if total == 0 {
		b.WriteString("no reframe records yet (no task(action=reframe) call has been recorded)\n")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "%d reframe record(s) across %d task(s)\n", total, len(order))
	fmt.Fprintf(&b, "%-12s %6s %6s\n", "VERDICT", "COUNT", "ITEMS")
	// Every key dist holds renders (reframeVerdictTableOrder): the table's
	// counts therefore sum to `total` by construction — a counted record can
	// never be invisible in the table (Rule 15).
	for _, v := range reframeVerdictTableOrder(dist) {
		fmt.Fprintf(&b, "%-12s %6d %6d\n", v, dist[v], items[v])
	}
	b.WriteString("per task:\n")
	// Most-reframed first: the bureaucracy symptom sorts to the top.
	sort.SliceStable(order, func(i, j int) bool { return len(byTask[order[i]]) > len(byTask[order[j]]) })
	for i, id := range order {
		if i >= reframeVerdictsTaskCap {
			fmt.Fprintf(&b, "  … %d more task(s)\n", len(order)-reframeVerdictsTaskCap)
			break
		}
		vs := byTask[id]
		shown := vs
		if len(shown) > reframeVerdictsPerTaskCap {
			shown = shown[:reframeVerdictsPerTaskCap]
		}
		fmt.Fprintf(&b, "  %s ×%d (%s)", id, len(vs), strings.Join(shown, ", "))
		// Provenance: how many distinct processes reported under this task.
		// More than one is the attribution symptom — a nested process inherited
		// this run's wrapper identity and wrote under a row it does not own
		// (reframe-telemetry-doublecount, 2026-09-10: 2 dispatches / 22 rows).
		if n := len(origins[id]); n > 1 {
			fmt.Fprintf(&b, "  ⚠ %d distinct origins reported under this task — telemetry attributed from outside the run", n)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}
