// Brew part 3 — the orchestrator and the write path. RunBrew sweeps session
// JSONLs, slices, judges, distills and stores. storeBrewLesson tags every
// row source=brew and refuses to overwrite non-brew rows (③ 防自指: a future
// refill pass must never clobber human/agent-stored knowledge, and brew
// output must never be re-ingested as session input).
package tools

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// brewSessionsDir is the default session-log directory (config.GopiDir() +
// /sessions — same place SessionLogger writes).
func brewSessionsDir() (string, error) {
	dir, err := config.SessionsRoot()
	if err != nil {
		return "", fmt.Errorf("brew: %w", err)
	}
	return dir, nil
}

// listSessionJSONLs returns up to limit (0 = brewMaxSessions) newest session
// RECORD files in dir, sorted newest-first (fresh sessions hold the best
// lessons). A missing sessions dir is "nothing to brew", not an error (fresh
// GOPI_HOME installs legitimately have none — the m4 HP acceptance test hit
// this via the hermetic TestIsBrewSubcommand).
//
// The scan is the SHARED layout-keyed classification
// (internal/sessionlayout.Records), so a session dir's side files
// (signal-provenance.jsonl — signalrec's provenance record for the SAME
// session) are never brewed as if they were sessions: a provenance line has
// no conversation to slice, and feeding one to the slicer would emit
// meaningless "lessons" while double-counting the session.
func listSessionJSONLs(dir string, limit int) ([]string, error) {
	records, err := sessionlayout.Records(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("brew: read sessions dir: %w", err)
	}
	type dated struct {
		path string
		mod  time.Time
	}
	var files []dated
	for _, r := range records {
		info, err := os.Stat(r.Path)
		if err != nil {
			continue
		}
		files = append(files, dated{r.Path, info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	max := brewMaxSessions
	if limit > 0 && limit < max {
		max = limit
	}
	if len(files) > max {
		files = files[:max]
	}
	var out []string
	for _, f := range files {
		out = append(out, f.path)
	}
	return out, nil
}

// RunBrewOptions configures one brew run.
type RunBrewOptions struct {
	// SessionPaths: explicit session files. Empty → sweep the sessions dir
	// (newest brewMaxSessions).
	SessionPaths []string
	// SessionsDir overrides the sweep directory (tests / custom GOPI_HOME).
	SessionsDir string
	// Limit caps how many session files one run processes (0 = the
	// brewMaxSessions default). Cost-budget knob (④).
	Limit int
	// DryRun judges and distills but stores nothing (acceptance sampling).
	DryRun bool
	// Stderr receives one progress line per session (nil = quiet).
	Progress func(format string, args ...interface{})
}

// RunBrew executes the full pipeline. It never returns per-lesson errors as
// a fatal failure — one bad LLM reply skips that chunk and is counted in
// BrewStats.Errors (batch resilience: a 64-session sweep must not die on
// chunk #3).
func RunBrew(ctx context.Context, bc brewClient, opts RunBrewOptions) (*BrewStats, error) {
	start := time.Now()
	stats := &BrewStats{}

	paths := opts.SessionPaths
	if len(paths) == 0 {
		dir := opts.SessionsDir
		if dir == "" {
			var err error
			if dir, err = brewSessionsDir(); err != nil {
				return stats, err
			}
		}
		var err error
		if paths, err = listSessionJSONLs(dir, opts.Limit); err != nil {
			return stats, err
		}
	}

	for _, path := range paths {
		if ctx.Err() != nil {
			break
		}
		transcript, err := LoadSessionText(path)
		if err != nil {
			stats.Errors = append(stats.Errors, err.Error())
			continue
		}
		if strings.TrimSpace(transcript) == "" {
			continue
		}
		stats.SessionsScanned++

		stored := 0
		for _, chunk := range sliceSession(transcript) {
			cands, err := judgeChunk(ctx, bc, chunk)
			if err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("%s: judge: %v", filepath.Base(path), err))
				continue
			}
			stats.ChunksJudged++
			if len(cands) == 0 {
				stats.LessonsRejected++
				continue
			}
			stats.LessonsAccepted += len(cands)
			for _, cand := range cands {
				// Procedural routing (工作流类 lesson 分流): kind=workflow
				// candidates distill into a procedural row (trigger/steps)
				// instead of a distilled_qa QA row. Routing never fails the
				// run — a bad distill counts as a rejection.
				if cand.Kind == "workflow" {
					wf, err := distillWorkflow(ctx, bc, cand)
					if err != nil {
						stats.Errors = append(stats.Errors, fmt.Sprintf("%s: distill workflow %q: %v", filepath.Base(path), cand.Topic, err))
						stats.LessonsRejected++
						continue
					}
					if opts.DryRun {
						stats.Distilled++
						stored++
						continue
					}
					deduped, err := storeBrewWorkflow(wf)
					if err != nil {
						stats.Errors = append(stats.Errors, fmt.Sprintf("%s: store workflow %q: %v", filepath.Base(path), wf.Name, err))
						continue
					}
					if deduped {
						stats.Deduped++
					} else {
						stats.Stored++
					}
					stored++
					continue
				}
				qa, err := distillLesson(ctx, bc, cand)
				if err != nil {
					stats.Errors = append(stats.Errors, fmt.Sprintf("%s: distill %q: %v", filepath.Base(path), cand.Topic, err))
					stats.LessonsRejected++
					continue
				}
				if opts.DryRun {
					stats.Distilled++ // would-be row; nothing written
					stored++
					continue
				}
				deduped, err := storeBrewLesson(qa)
				if err != nil {
					stats.Errors = append(stats.Errors, fmt.Sprintf("%s: store %q: %v", filepath.Base(path), qa.Question, err))
					continue
				}
				if deduped {
					stats.Deduped++
				} else {
					stats.Stored++
				}
				stored++
			}
		}
		if opts.Progress != nil && stored > 0 {
			opts.Progress("brew: %s → %d lesson(s)", filepath.Base(path), stored)
		}
	}
	stats.ElapsedSec = time.Since(start).Seconds()
	return stats, nil
}

// storeBrewLesson writes one distilled QA row tagged source=brew.
//
// Anti-self-reference (③), two layers:
//  1. tag — every row carries sources=brew so a future refill/backfill pass
//     can skip brew rows with one WHERE clause;
//  2. never-overwrite — when a dedup-matched existing row (exact key OR the
//     similarity path MemoryStore uses) is NOT brew-sourced, the brew write
//     is refused (reported as deduped; nothing clobbered). Existing brew
//     rows still merge sources (idempotent re-runs stay cheap).
func storeBrewLesson(qa *brewDistillReply) (deduped bool, err error) {
	// Guard 2 must see the row's identity BEFORE the insert. MemoryStore
	// owns the dedup+merge logic, so we replay its two dedup keys here:
	// the exact (question, topic-agnostic) key and the similarity screen.
	blocked, err := brewGuardBlocked(qa)
	if err != nil {
		return false, err
	}
	if blocked {
		return true, nil
	}
	res, err := MemoryStore("brew: "+qa.Topic, qa.Question, qa.Answer, BrewSourceTag, qa.Keywords)
	if err != nil {
		return false, fmt.Errorf("memory_store: %w", err)
	}
	// MemoryStore signals dedup through its storeResult text; parse it so
	// idempotent re-runs count as deduped rather than fresh rows.
	return strings.Contains(res, "deduped into an existing entry"), nil
}

// storeBrewWorkflow writes one distilled procedural row tagged
// source=brew,procedural (both export gates — QA channel and procedural
// channel — can then scope it). The procedural path's own dedup
// (name exact + name+trigger similarity) already merges; a deduped store is
// counted the same way storeBrewLesson parses its result.
func storeBrewWorkflow(wf *brewWorkflowReply) (deduped bool, err error) {
	w := &ProceduralWorkflow{
		Name:          wf.Name,
		Trigger:       wf.Trigger,
		Preconditions: wf.Preconditions,
		ToolsUsed:     wf.ToolsUsed,
	}
	for _, s := range wf.Steps {
		w.Steps = append(w.Steps, ProceduralStep{Action: "step", Detail: s})
	}
	res, err := ProceduralStore(w, BrewSourceTag)
	if err != nil {
		return false, fmt.Errorf("procedural_store: %w", err)
	}
	return strings.Contains(res, "deduped"), nil
}

// brewGuardBlocked reports whether a brew write must be refused because a
// matching existing row is not brew-sourced. Mirrors MemoryStore's dedup:// (a) exact question key (MemoryStore pairs it with topic, but question is
// the distinctive half; a topic-keyed miss just falls through to insert);
// (b) the FTS pre-screen + trigram-Jaccard similarity match.
func brewGuardBlocked(qa *brewDistillReply) (bool, error) {
	db, err := openStoreDB(context.Background(), memoryDBPath())
	if err != nil {
		return false, err
	}
	defer db.Close()
	ctx := context.Background()

	// (a) exact question key.
	q := truncateRunes(strings.TrimSpace(qa.Question), dedupQuestionRunes)
	var sources string
	err = db.QueryRowContext(ctx,
		`SELECT COALESCE(sources,'') FROM distilled_qa WHERE question=?`, q).Scan(&sources)
	switch {
	case err == nil:
		return !strings.Contains(sources, BrewSourceTag), nil
	case err == sql.ErrNoRows:
		// keep going to the similarity screen
	default:
		return false, fmt.Errorf("brew: dedup guard query: %w", err)
	}

	// (b) similarity path — same screen MemoryStore applies (findSimilarDuplicate).
	if id, _ := findSimilarDuplicate(ctx, db, qa.Question, "brew: "+qa.Topic); id > 0 {
		var src string
		if err := db.QueryRowContext(ctx,
			`SELECT COALESCE(sources,'') FROM distilled_qa WHERE id=?`, id).Scan(&src); err != nil {
			return false, nil // row vanished mid-flight → let MemoryStore decide
		}
		return !strings.Contains(src, BrewSourceTag), nil
	}
	return false, nil
}
