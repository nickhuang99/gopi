package tools

// `gopi task clone` — the CLEAN-ROOM copy of a row.
//
// WHY THIS VERB EXISTS — and why it is the SECOND of exactly two levels.
// There is a ladder an operator climbs when a row's run went wrong, and it has
// two rungs, no more:
//
//	① `gopi session resume <id>` / `gopi worker --resume` / `gopi task status`
//	   — re-enter the SAME row in the SAME recorded ground with the SAME
//	   session: continue the attempt (level 1, and it admits a `failed` row).
//	② `gopi task clone <id>` — the ORIGINAL WORKSPACE IS NO LONGER TRUSTWORTHY:
//	   hand me a clean room (a NEW id, a NEW session, a NEW directory) with the
//	   same work in it.
//
// THE THIRD RUNG THAT USED TO SIT BETWEEN THEM IS GONE ON PURPOSE. A failed row
// has exactly two honest exits: RESUME it (same id, same session, same
// directory) or CLONE it into a fresh room (new id, new session, new
// directory). The retired verb minted a new id/session/directory — i.e. COPY
// mechanics — while its NAME and its `--reason`/gate were shaped by the verdict,
// as if it continued the failure. Operators reached for it to CONTINUE work and
// got a fresh ground with the context abandoned. That is the confusion its name
// produced, and it is why the mechanism was deleted rather than renamed: the
// work it did is exactly what `clone` already does, and the intent it advertised
// is exactly what `session resume` already does.
//
// So this verb's gate is about the ROOM: a row that may be RUNNING is refused
// (a second run in a second room is exactly the double-execution the runner
// exists to prevent), a row that has not run yet is refused (nothing to
// distrust — the room is unused, not dirty: RESUME or RUN it), and a row
// abandoned on purpose (`cancelled`) is refused unless the operator confirms,
// because minting work from an abandoned row silently would resurrect a
// decision somebody already made.
//
// WHAT THE NEW ROW GETS AND WHAT IT DOES NOT:
//
//	GETS   the source's summary and payload VERBATIM (the briefing is the
//	       work), its tags, its priority-derived weight (through the one
//	       zero-value resolver), its `deliverable.repo` (inherited, or the
//	       operator's `--repo` override), its ATTACHMENT manifest with every
//	       replica re-materialised in the NEW row's own session dir, and the
//	       lineage edge back to the source (`parent_task_id` + the rework
//	       lineage tag, without which Rule 33 would park it forever against a
//	       `failed` parent).
//	DOES NOT GET its `worktree`/`branch`/`grounds` — those are RUN-allocated,
//	       and "the source's directory" is precisely what the caller declared
//	       untrustworthy. The successor's ground is minted from its OWN id, so
//	       it cannot be the source's directory even by accident (asserted, not
//	       assumed: see the ground axis below).
//
// RULE 35 — no new persistent dependency: nothing here ticks, spawns or
// schedules. It is one ledger write plus a file copy, reached through the CLI
// verb the operator types.

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/tasksource"
	"github.com/nickhuang99/gopi/internal/tasktags"
)

const (
	// TaskCloneSuccessorIDFmt is the ONE spelling of the LEGACY clone-row id:
	// the source row, the clone marker and the ordinal (`<uuid>-c1`).
	//
	// IT IS NO LONGER MINTED (operator ruling 2026-11-05, memory #27273: a task
	// id is an identity and appending a suffix destroys that). It survives
	// because the rows written in this shape BEFORE the ruling are still on
	// disk and still have to be recognisable — an id is the ledger's primary
	// key, so they are not renamed. Every reader of this constant is therefore
	// a MIGRATION reader: the shape test and the live-successor gate
	// (taskCloneIsSuccessorID / taskCloneLiveSuccessorFind), plus
	// sessionContainerRowOfTask's directory fallback. Nothing writes it any
	// more — taskCloneMintID mints a fresh uuid v7 instead.
	TaskCloneSuccessorIDFmt = "%s-c%d"

	// taskCloneFirstOrdinal is the ordinal of the first legacy clone
	// (`<id>-c1`). The shape test accepts this or any higher ordinal.
	taskCloneFirstOrdinal = 1
)

// The phrases a clone result line carries. Named so an operator or a test
// matches one string instead of re-parsing prose (Rule 14).
const (
	// TaskCloneMarker is the noun this verb calls what it mints. It does NOT
	// say "reissue": the two verbs must stay
	// distinguishable in the ledger's own output.
	TaskCloneMarker = "clean-room copy of"
	// TaskCloneGroundMarker prefixes the ground axis: the directory the new
	// row's run will open, and the fact that it is NOT the source's.
	TaskCloneGroundMarker = "ground="
	// TaskCloneGroundUntracked says the source recorded no ground, so there is
	// nothing to differ from. Stated, never silent (Rule 15): "no ground
	// recorded" and "the ground differs" are different answers.
	TaskCloneGroundUntracked = "the source recorded no worktree, so it has no ground to replace (the new row mints one at run time)"
	// TaskCloneDryRunMarker prefixes a dry-run line.
	TaskCloneDryRunMarker = "dry run — nothing was written; this is what `gopi task clone` would do"
)

// The three shapes of refusal, each a format carrying the row and its status
// (Rule 1: no assembled prose at the call site; the operator gets the operand
// of the remedy so either line can be copied verbatim).
const (
	// taskCloneRefusalRunningFmt: the row may be RUNNING. Its ground has an
	// owner, and a clone would put a second run beside the first.
	taskCloneRefusalRunningFmt = "refused — task %s is %s: it may be RUNNING right now, so its ground has an owner " +
		"and a clone would put a second run beside the first. Wait for it to finish " +
		"(`gopi task status %s`), or cancel it if it is a zombie (`gopi task update %s --status cancelled`)."
	// taskCloneRefusalUnrunFmt: nothing has run, so nothing can be distrusted.
	taskCloneRefusalUnrunFmt = "refused — task %s is %s: it has not run yet, so its ground is not suspect — it is " +
		"UNUSED. The remedy for an unrun row is to RUN it (`gopi worker %s`), not to copy it."
	// taskCloneRefusalCancelledFmt: abandoned on purpose.
	taskCloneRefusalCancelledFmt = "refused — task %s is %s: it was abandoned on purpose, so a clone would resurrect " +
		"a decision somebody already made. Re-run with `--confirm-cancelled` if that decision is being reversed."
	// taskCloneRefusalUnterminalFmt is the catch-all for a status this verb has
	// no opinion about — refused rather than admitted, because a status the
	// gate does not name is a status whose ground state nobody measured.
	taskCloneRefusalUnterminalFmt = "refused — task %s is %s: this verb clones rows whose run is OVER " +
		"(completed|failed|verified|cancelled), and a %s row is not one this gate can judge."
)

// TaskCloneOptions is the clone's full input: the source row, the operator's
// optional reason (it becomes a NOTE in the briefing, never a requirement —
// the work is unchanged, so there is no failure the briefing must carry), the
// optional repo OVERRIDE, the dry-run switch, and the explicit confirmation
// `cancelled` needs.
type TaskCloneOptions struct {
	TaskID string
	// Reason is OPTIONAL: the work is unchanged, so the briefing needs no
	// failure to carry. When given it rides the payload as a provenance line, so
	// a reader learns why a clean room was wanted.
	Reason string
	// Repo is the operator's explicit override (`--repo PATH`). Empty =
	// inherit the source row's deliverable.repo.
	Repo string
	// DryRun reports the whole plan — the new id, the ground, the repo, the
	// attachments — and writes NOTHING. Default false.
	DryRun bool
	// ConfirmCancelled admits a `cancelled` source row. It is the operator's
	// explicit "yes, I mean the abandoned one".
	ConfirmCancelled bool
}

// taskCloneStatusAdmits is the STATUS GATE — the criterion this verb exists
// for, in one predicate so the CLI, the engine and the tests cannot disagree.
//
// The three answers are the three refusals plus the admitted set, and the
// ADMITTED set is terminalTaskStatus minus `superseded`... no: it is EXACTLY
// {completed, failed, verified}, with `cancelled` conditional. `superseded`
// means "the work is done elsewhere" — copying it would re-create work another
// row already carries, so it stays out, and it is refused BY NAME rather than
// falling into the catch-all (Rule 15: a refusal that names the status teaches
// the operator what the gate is).
func taskCloneStatusAdmits(status string, confirmCancelled bool) (bool, string, string) {
	switch status {
	case taskStatusInProgress, taskStatusClaimed:
		return false, "running", taskCloneRefusalRunningFmt
	case taskStatusPending:
		return false, "unrun", taskCloneRefusalUnrunFmt
	case taskStatusCancelled:
		if confirmCancelled {
			return true, "", ""
		}
		return false, "cancelled", taskCloneRefusalCancelledFmt
	case taskStatusCompleted, taskStatusFailed, taskStatusVerified:
		return true, "", ""
	}
	// superseded, reopened (retired), and anything unknown.
	return false, "unterminal", taskCloneRefusalUnterminalFmt
}

// taskCloneRefusal renders the refusal for one row: the STATUS-specific
// sentence plus the ONE line naming what IS the remedy when a clone is not
// (Rule 15: a refusal that does not say what to do instead is half an answer).
func taskCloneRefusal(taskID, status string) error {
	var msg string
	switch status {
	case taskStatusInProgress, taskStatusClaimed:
		msg = fmt.Sprintf(taskCloneRefusalRunningFmt, taskID, status, taskID, taskID)
	case taskStatusPending:
		msg = fmt.Sprintf(taskCloneRefusalUnrunFmt, taskID, status, taskID)
	case taskStatusCancelled:
		msg = fmt.Sprintf(taskCloneRefusalCancelledFmt, taskID, status)
	default:
		msg = fmt.Sprintf(taskCloneRefusalUnterminalFmt, taskID, status, status)
	}
	return fmt.Errorf("%s\nNext: a row whose run is over and whose WORK is still wanted is copied into a "+
		"clean room with `gopi task clone %s`; if the room is fine and only its recorded repo is stale, "+
		"repair the row in place with `gopi task update %s --repo PATH`.", msg, taskID, taskID)
}

// TaskCloneWithOptions mints the clean-room copy and returns the operator line.
//
// The ORDER of the steps is the contract: every refusal is decided BEFORE the
// first write, so a refused clone leaves the ledger exactly as it found it
// (asserted by the tests: "the three refusal statuses each prove no row was
// added").
func TaskCloneWithOptions(ctx context.Context, opts TaskCloneOptions) (string, error) {
	srcID := taskIDNormalize(opts.TaskID)
	if srcID == "" {
		return "", fmt.Errorf("task clone: task_id must not be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	src, err := taskByID(ctx, db, srcID)
	if err != nil {
		return "", fmt.Errorf("task clone: %w", err)
	}
	if src == nil {
		return "", fmt.Errorf("task clone: task %q does not exist", srcID)
	}
	// ① THE STATUS GATE — the verb's whole reason for existing.
	if ok, _, _ := taskCloneStatusAdmits(src.Status, opts.ConfirmCancelled); !ok {
		return "", taskCloneRefusal(srcID, src.Status)
	}
	// ② THE LIVE-SUCCESSOR GATE. A second clone of the same row is a second run
	// of the same work: the first one already carries it, so the caller's next
	// step is to watch that row. Checked before anything is written (no row, no
	// attachment copy).
	liveID, liveStatus, err := taskCloneLiveSuccessorFind(ctx, db, srcID)
	if err != nil {
		return "", fmt.Errorf("task clone: %w", err)
	}
	if liveID != "" {
		return "", fmt.Errorf("refused — task %s already has a live clone %s (status=%s): that row already carries "+
			"this work in a clean room, so a second one would dispatch it twice. Next: `gopi task show %s` and "+
			"wait for it, or `gopi task update %s --status cancelled` if that clone is wrong",
			srcID, liveID, liveStatus, liveID, liveID)
	}
	// ③ THE ID. A clone is a ROW OF ITS OWN with its OWN uuid — never
	// `<src>-c<ordinal>` (operator ruling 2026-11-05: an id is an identity, and
	// a suffix destroys it for every downstream reader). The lineage the suffix
	// used to spell moves into data: `parent_task_id` + the tags built by
	// taskCloneTags. taskIDGenerate only READS the ledger (a uniqueness probe),
	// so the dry run below can mint the same candidate without writing anything.
	newID, err := taskCloneMintID(ctx, db)
	if err != nil {
		return "", err
	}
	// ④ THE REPO AXIS: override > inherited (repaired through the ONE durable
	// resolver) > stated-absent.
	repo, repoLine, err := taskCloneDeliverable(srcID, src.Deliverable, opts.Repo)
	if err != nil {
		return "", err
	}
	// ⑤ THE GROUND AXIS: the directory the new row will open, derived from the
	// NEW id under the operator's configured workspace root — never copied from
	// the source, which is the one thing this verb was asked to replace.
	groundPath, groundLine, err := taskCloneGround(src, newID)
	if err != nil {
		return "", err
	}
	// ⑤b THE WIP-SNAPSHOT AXIS: a source run that was interrupted leaves its
	// uncommitted work in ITS session dir (task_wip_snapshot.go); the clean room
	// this verb promises would make those bytes invisible, so the successor's
	// briefing names the deposit and the returned line states the path — or
	// states the ABSENCE (Rule 15). Reached through the ONE existing
	// implementation (task_successor_wip_pointer.go), never re-implemented here.
	wipBriefing, wipLine := taskSuccessorWIPAxis(srcID)
	repoLine += wipLine
	payload := taskClonePayload(src, opts.Reason) + wipBriefing
	tags := taskCloneTags(src.Tags, srcID)
	summary := fmt.Sprintf("%s %s: %s", TaskCloneMarker, srcID, src.Summary)
	if opts.DryRun {
		return taskCloneDryRunLine(src, newID, summary, payload, repo, repoLine, groundPath, groundLine), nil
	}
	// ⑥ THE EVIDENCE AXIS: the source's attachment manifest, re-materialised in
	// the NEW row's own session dir — BEFORE the insert, so the row is born with
	// its evidence and no second column write is needed. It is reached through the ONE existing implementation
	// (taskSuccessorCarryAttachments): a clone whose briefing says "see attachment"
	// while carrying no manifest is the defect that axis was built for, and a
	// second copy of that logic would be a second answer to "what does carrying
	// evidence mean".
	finalBlob, attLine, aerr := taskSuccessorCarryAttachments(newID, srcID, repo, src.Deliverable)
	if aerr != nil {
		return "", fmt.Errorf("task clone: %w", aerr)
	}
	repoLine += attLine
	// ⑦ THE ROW. Inserted with the lineage tag BESIDE parent_task_id, or Rule 33
	// parks it forever against a `failed` source (the tag selects the verdict-edge
	// semantics; it never widens the release set).
	id, err := insertTaskRow(ctx, db, taskCreateFields{
		TaskID:       newID,
		Summary:      summary,
		Status:       taskStatusPending,
		ParentTaskID: srcID,
		Tags:         tags,
		Payload:      payload,
		Source:       tasksource.Worker,
		Deliverable:  finalBlob,
	})
	if err != nil {
		return "", fmt.Errorf("task clone: insert: %w", err)
	}
	newID = id
	// ⑧ THE PAYLOAD-COPY RECORD. The new row's briefing is the source's text, so
	// its own created_at is NOT that text's writing moment — recorded here, at
	// the moment the copy happens, so a later reader learns it from the ledger
	// instead of from the code path that once ran (Rule 15: a lost audit row is
	// stated, never swallowed).
	copyLine := ""
	if src.Payload != "" {
		if evErr := taskEventWriteErr(ctx, db, newID, BriefingPayloadCopyEventName,
			TaskBriefingCopyEventNote(srcID, newID, src.Payload, opts.Reason)); evErr != nil {
			copyLine = " — WARNING: the payload-copy audit row could not be written (" + evErr.Error() +
				"), so this clone's briefing writing moment cannot be established from the ledger"
		}
	}
	// ⑨ THE CHILDREN MOVE WITH IT, through the ONE existing implementation
	// (rework_reparent_children.go): the source row stays terminal, so every
	// ordinary child still naming it would be parked forever (Rule 33), while
	// the work they waited on has just moved to the clone.
	reparentLine := ""
	if rep := ReworkReparentChildren(srcID, newID, "clone"); rep.Refused != "" || rep.Children() > 0 {
		reparentLine = " — " + rep.Render()
	}
	return fmt.Sprintf("Task cloned: %s (status=pending, parent=%s) — %s %s%s%s%s%s",
		newID, srcID, TaskCloneMarker, srcID, repoLine, groundLine, reparentLine, copyLine), nil
}

// taskClonePayload builds the new row's briefing: the source's payload
// VERBATIM, plus a provenance header and (when given) the operator's note.
//
// It is a PREFIX, not a rewrite: the briefing is the work, and a clone that
// paraphrased it would be a different task. The header exists so a reader of
// the new row knows — from the row itself — where the text came from and why a
// clean room was wanted.
func taskClonePayload(src *Task, reason string) string {
	header := fmt.Sprintf("--- %s %s (clean room; the source row's workspace is not reused) ---",
		TaskCloneMarker, src.TaskID)
	if r := strings.TrimSpace(reason); r != "" {
		header += "\n--- clone reason ---\n" + r
	}
	body := strings.TrimSpace(src.Payload)
	if body == "" {
		return header
	}
	return header + "\n" + src.Payload
}

// taskCloneTags carries the source's tags and adds the LINEAGE marks, which are
// not optional:
//
//   - tasks.tags' ReworkLineageTag — without it the new row is an ordinary child
//     of a terminal parent, i.e. `[queue-skip]` forever (Rule 33; tasktags' own
//     doc explains why the mark selects an edge SEMANTICS instead of widening
//     the release set);
//   - tasks.tags' CloneLineageTag and `rework-of:<source id>` — the lineage that
//     used to be spelled by the id suffix (`<src>-c1`) and is now DATA on the
//     row, which is the operator's 2026-11-05 ruling in one sentence: identity
//     belongs in the id, provenance belongs in the columns.
func taskCloneTags(tags, srcID string) string {
	out := strings.TrimSpace(tags)
	add := func(tok string) {
		if taskHasTagToken(out, tok) {
			return
		}
		if out != "" {
			out += ","
		}
		out += tok
	}
	add(tasktags.ReworkLineageTag)
	add(tasktags.CloneLineageTag)
	add(tasktags.ReworkOfTagPrefix + srcID)
	return out
}

// taskCloneMintID returns the new row's id: a FRESH uuid v7 through the ONE
// existing minter (taskIDGenerate), which already carries the uniqueness retry.
//
// WHY A FRESH UUID AND NOT `<src>-c<N>` (operator ruling 2026-11-05, memory
// #27273). A task id is an IDENTITY: `parent_task_id`, `deliverable`, notify's
// session routing and `--resume`'s selector all resolve a row by it. Appending
// an ordinal breaks exactly that — the id stops being a uuid, so a reader that
// validates the shape rejects the row and a reader that does not cannot tell
// which of the two rows (`<uuid>` or `<uuid>-c1`) was meant. The lineage the
// suffix used to spell is now DATA on the row: `parent_task_id` (the edge) plus
// tasks.tags' clone/rework/rework-of markers (why it exists and what it copies).
//
// WHAT THIS DOES NOT DO: it does not rename the `<uuid>-c<N>` rows already on
// disk — an id is the ledger's PRIMARY KEY, so rewriting one severs every
// external reference to it. Those rows stay addressable, and
// taskCloneIsSuccessorID keeps recognising their shape (see its own comment).
func taskCloneMintID(ctx context.Context, db *sql.DB) (string, error) {
	id, err := taskIDGenerate(ctx, db)
	if err != nil {
		return "", fmt.Errorf("task clone: %w", err)
	}
	return id, nil
}

// taskCloneIsSuccessorID — STILL TRUE FOR THE ROWS ALREADY ON DISK, AND NEVER
// AGAIN FOR A NEW ONE (`<uuid>-c1` / `<uuid>-c2`, …).
//
// It reports whether candidate is a clone row id minted off srcID. Since the
// 2026-11-05 ruling the mint no longer produces this shape (taskCloneMintID
// mints a fresh uuid), so this predicate is a MIGRATION reader and nothing
// else: it is what keeps every `<uuid>-c1` row written before the ruling
// addressable, nameable in a refusal, and readable as a clone rather than as a
// coincidental child. Deleting it would strand those rows, so the shape stays
// known even though nothing writes it any more.
//
// The live-successor gate is keyed on it for exactly that reason: a pre-ruling
// clone still in flight must still block a second clone of the same source, or
// the same work would be dispatched twice.
func taskCloneIsSuccessorID(srcID, candidate string) bool {
	prefix := fmt.Sprintf(TaskCloneSuccessorIDFmt, srcID, taskCloneFirstOrdinal)
	prefix = strings.TrimSuffix(prefix, strconv.Itoa(taskCloneFirstOrdinal))
	if !strings.HasPrefix(candidate, prefix) {
		return false
	}
	ordinal := candidate[len(prefix):]
	if ordinal == "" {
		return false
	}
	// A leading zero is not the ordinal spelling the mint produces (`-c1`, not
	// `-c01`): a hand-made id in that shape is somebody else's row, and treating
	// it as ours would make this gate refuse a clone that does not exist.
	if len(ordinal) > 1 && ordinal[0] == '0' {
		return false
	}
	for _, r := range ordinal {
		if r < '0' || r > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(ordinal)
	return err == nil && n >= taskCloneFirstOrdinal
}

// taskCloneMarksName reports whether a row's tags say it IS a clean-room copy of
// srcID: tasks.tags' clone mark AND the `rework-of:<srcID>` entry taskCloneTags
// writes at birth. Both, never either — the mark alone would let one source's
// gate match another source's clone, and the rework-of entry alone is what every
// rework row carries.
//
// The comparison is by COMMA TOKEN (taskHasTagToken), the spelling the tag set is
// written and read in everywhere else, so a substring hit can never impersonate a
// token.
func taskCloneMarksName(tags, srcID string) bool {
	return taskHasTagToken(tags, tasktags.CloneLineageTag) &&
		taskHasTagToken(tags, tasktags.ReworkOfTagPrefix+srcID)
}

// taskCloneLiveSuccessorFind returns the first NON-terminal clone of srcID (id
// and status), or empty when there is none.
//
// THE EDGE IS `parent_task_id` plus BOTH lineage marks, NOT the id's shape. Since
// the 2026-11-05 ruling a clone's id is a fresh uuid with no relation to the
// source id, so an id-prefix scan finds nothing a new clone mints — the gate
// would silently stop working and the same work would be dispatched twice, each
// row with its own ground. The evidence that a row IS a clean-room copy of srcID
// is now the row's own columns (taskCloneTags writes them at birth):
//
//	parent_task_id = srcID  AND  tags carry "clone"
//	                             AND  tags carry "rework-of:<srcID>"
//
// The LEGACY `<src>-c<N>` shape is still honoured (taskCloneIsSuccessorID), both
// in the scan below and in the Go test, so a pre-ruling clone still in flight
// blocks a second one exactly as it did before.
func taskCloneLiveSuccessorFind(ctx context.Context, db *sql.DB, srcID string) (id, status string, err error) {
	shapePrefix := fmt.Sprintf(TaskCloneSuccessorIDFmt, srcID, taskCloneFirstOrdinal)
	shapePrefix = strings.TrimSuffix(shapePrefix, strconv.Itoa(taskCloneFirstOrdinal))
	rows, err := db.QueryContext(ctx,
		`SELECT task_id, status, COALESCE(tags,'') FROM tasks
		  WHERE status IN (`+taskLiveStatusSQLList()+`)
		    AND (parent_task_id = ? OR substr(task_id, 1, ?) = ?)
		  ORDER BY id`,
		srcID, len(shapePrefix), shapePrefix)
	if err != nil {
		return "", "", fmt.Errorf("read the live clones of %s: %w", srcID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var candidate, candidateStatus, candidateTags string
		if err := rows.Scan(&candidate, &candidateStatus, &candidateTags); err != nil {
			return "", "", fmt.Errorf("scan the live clones of %s: %w", srcID, err)
		}
		if taskCloneIsSuccessorID(srcID, candidate) || taskCloneMarksName(candidateTags, srcID) {
			return candidate, candidateStatus, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", "", fmt.Errorf("walk the live clones of %s: %w", srcID, err)
	}
	return "", "", nil
}

// taskCloneDeliverable decides the new row's deliverable blob, and returns the
// line fragment the caller appends to its own result.
//
//	pinned    = the operator's --repo, resolved to its DURABLE main checkout
//	            through the SAME resolver every other seam uses, so a
//	            `git worktree list` path lands on the repository;
//	inherited = the source's deliverable.repo, repaired through durableRowRepo
//	            (a clone born naming a reclaimable workspace dies at dispatch —
//	            the defect that resolver was written for);
//	absent    = "" blob + the STATED line, never a silent unstartable row.
//
// `worktree`/`branch`/`grounds` are deliberately NOT carried: the source's
// directory is exactly what the caller declared untrustworthy.
func taskCloneDeliverable(srcID, srcDeliverable, override string) (blob, line string, err error) {
	override = strings.TrimSpace(override)
	if override != "" {
		durable, rerr := DurableRepoForSubmit(override)
		if rerr != nil {
			return "", "", fmt.Errorf("task clone: --repo: %w", rerr)
		}
		return taskDeliverableRepoJSON(durable), " " + TaskSuccessorRepoCarriedMarker + durable + " (from --repo)", nil
	}
	inherited := strings.TrimSpace(taskDeliverableParse(srcDeliverable).Repo)
	if inherited != "" {
		repaired, note := durableRowRepo(inherited)
		out := " " + TaskSuccessorRepoCarriedMarker + repaired + " (inherited from " + srcID + ")"
		if note != "" {
			out += "; " + note
		}
		return taskDeliverableRepoJSON(repaired), out, nil
	}
	return "", " " + TaskSuccessorNoRepoMarker + taskSuccessorNoRepoDetail, nil
}

// taskCloneGround answers WHICH directory the new row's run will open, and the
// line fragment stating it.
//
// This IS the verb's deliverable: "a clean room" is a claim about a path, so it
// is derived from the NEW row's id under the operator's configured workspace
// root — the same derivation the clone channel uses (`devworkspace.Dir`) — and
// never copied from the source. A source with no recorded worktree has no ground
// to replace, and that absence is STATED (Rule 15) rather than reported as "the
// grounds differ".
func taskCloneGround(src *Task, newID string) (path, line string, err error) {
	srcWT := strings.TrimSpace(taskDeliverableParse(src.Deliverable).Worktree)
	if srcWT == "" {
		return "", " " + TaskCloneGroundMarker + TaskCloneGroundUntracked, nil
	}
	root, rootSource, rerr := config.WorkspaceRootConfigured()
	if rerr != nil {
		return "", "", fmt.Errorf("task clone: %s unusable: %w", config.WorkspaceRootSettingsKey, rerr)
	}
	if strings.TrimSpace(root) == "" {
		return "", " " + TaskCloneGroundMarker + TaskCloneGroundUntracked, nil
	}
	path = devworkspace.Dir(root, newID)
	srcDir := filepath.Clean(srcWT)
	if filepath.Clean(path) == srcDir {
		// UNREACHABLE by construction (the id differs, so the leaf differs), and
		// therefore fatal rather than tolerated: a clean-room copy that resolved
		// to the dirty room would be the one outcome this verb must never
		// produce, and a silent "same path" here would be a lying line.
		return "", "", fmt.Errorf("task clone: derived ground %s equals the source row's %s — refusing to "+
			"promise a clean room that is the same directory", path, srcDir)
	}
	return path, " " + TaskCloneGroundMarker + path + " (from " + rootSource + ", not the source's " + srcDir + ")", nil
}

// taskCloneDryRunLine renders the whole plan without writing anything: the id
// that WOULD be minted, the summary, the repo, the ground, the payload's first
// line and whether evidence would be carried. A dry run that only said "ok"
// would be a promise; this is the plan the operator can check.
func taskCloneDryRunLine(src *Task, newID, summary, payload, repo, repoLine, groundPath, groundLine string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", TaskCloneDryRunMarker)
	fmt.Fprintf(&b, "  new task id:  %s (status=pending, parent=%s)\n", newID, src.TaskID)
	fmt.Fprintf(&b, "  summary:      %s\n", summary)
	if strings.TrimSpace(repo) == "" {
		fmt.Fprintf(&b, "  deliverable:  (none)%s\n", repoLine)
	} else {
		fmt.Fprintf(&b, "  deliverable:  %s%s\n", repo, repoLine)
	}
	if strings.TrimSpace(groundPath) == "" {
		fmt.Fprintf(&b, "  ground:       %s%s\n", TaskCloneGroundUntracked, "")
	} else {
		fmt.Fprintf(&b, "  ground:       %s%s\n", groundPath, strings.TrimPrefix(groundLine, " "+TaskCloneGroundMarker+groundPath))
	}
	fmt.Fprintf(&b, "  payload:      %d bytes (the source's text verbatim, behind a provenance header)\n", len(payload))
	fmt.Fprintf(&b, "  tags:         %s\n", taskCloneTags(src.Tags, src.TaskID))
	return b.String()
}
