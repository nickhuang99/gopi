// task_session.go — Phase 1 of the epic-task-reframe session-dir asset
// system (SPEC-epic-task-reframe-session-dir.md): the per-task session
// directory container and the attachment copy engine.
//
// Design (approved in the Phase-1 briefing; root relocated by
// session-dir-unify S1 per the SPEC's 2026-09-08 decision):
//
//	<gopi-dir>/sessions/<task_id>/attachments/<abs-path-minus-root>
//
// Each submitted task owns a directory that will carry the whole asset
// payload across phases (attachments/ now; payload/reframed_epic/artifacts/
// logs in later phases). S1 moved the container root from the interim
// <gopi-dir>/task-sessions/ into <gopi-dir>/sessions/ — the same root that
// holds the session logs — so all of a task's assets (session run files,
// raw logs, attachments) live under one per-task directory
// (sessions/<task_id>/). The container is keyed by the LEDGER identity
// (task_id, always a t-* id or an explicit submit --task-id), which cannot
// collide with the session log tree: session dirs and legacy flat files are
// keyed by UUID, never t-*.
//
// Attachments are copied at SUBMIT time: the task_id is minted before the
// ledger INSERT (insertTaskRow owns the id), so the replica paths in the
// attachments manifest — and therefore the [attachmentN:] index the EPIC
// block injects at run time — are fully determined before the task is ever
// claimed. The copy is a plain file copy that DEREFERENCES symlinks (a
// symlink argument copies the linked file's real content), never recurses
// into directories, never moves the original, and preserves the original
// absolute path structure under attachments/ (the bidirectional mapping
// 原始绝对路径 ↔ 副本路径 the SPEC requires):
//
//	/tmp/log.txt      → attachments/tmp/log.txt
//	/home/user/log.txt → attachments/home/user/log.txt
//
// (strip the leading '/' and expand the remainder as subdirectories — two
// files with the same name in different directories never collide). The
// manifest is stored in the row's deliverable JSON ("attachments" array —
// Phase 1 scope; the SPEC's metadata.json file lands with the payload/
// reframed assets in a later phase), each entry carrying:
//
//	original_abs_path / replica_rel_path / sha256 / file_size / submitted_at
//
// The replica absolute path (what the LLM reads) is derivable as
// <session-dir>/<replica_rel_path>, and the manifest also embeds the
// session dir itself ("session_dir") so consumers never re-derive it.
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionid"
)

// taskSessionsRootName is the directory under the gopi data root that holds
// every task-session container (Rule 1: one named constant — the root name
// is the single knob that relocates the whole tree). Session-dir-unify S1:
// the container root moved INTO the sessions tree (sessions/<task_id>/ —
// the same root as the session run logs) so one task owns one directory.
// The dir-name source of truth is config.SessionsDirName (every sessions
// consumer in the repo — agent, gopilog, cmd/agent — joins the same
// constant); this alias keeps the task-domain naming local to the call
// sites that read it.
const taskSessionsRootName = config.SessionsDirName

// taskAttachmentsDirName is the attachments subdirectory inside each task
// session dir (SPEC: the copy area for submitted attachment replicas).
const taskAttachmentsDirName = "attachments"

// TaskAttachmentReplicaMissingLabel is the render for a manifest entry whose
// replica_rel_path is empty/whitespace — the ONE contract for that case
// (attachment-index positional contract, below): submission is the only
// writer of a manifest and it always stamps a non-empty replica_rel_path
// (taskAttachmentRelPath returns "" only for a non-absolute source, and
// TaskCopyAttachments validates absolute-ness BEFORE copying). An empty rel
// is therefore not "no attachment" — it is a row whose derived replica name
// was lost (corrupt or cross-version blob). Dropping such an entry would
// shorten the index and MISLABEL every later replica (position i of the
// manifest paired with position j<i of a shortened index), so the entry
// keeps its position and renders this explicit label instead. EXPORTED
// (Rule 14): the label is a cross-package value — the EPIC briefing's
// [attachmentN:] renderer (cmd/agent) and any later read face must be able
// to recognise the sentinel without mirroring the string.
const TaskAttachmentReplicaMissingLabel = "(no replica: empty replica_rel_path in manifest)"

// taskDeliverableVersionWithAttachments is the deliverable blob schema
// version that adds the "attachments" array (bumped from 1). Sync gates on
// deliverableVersionKnown (memory_sync_delta.go / task_sync_deliverable_test)
// so any version bump must land here in lockstep with
// deliverableVersionKnown = 2.
const taskDeliverableVersionWithAttachments = 2

// TaskAttachment is one entry of the deliverable attachments manifest — the
// submit-time record of a copied attachment replica (spec: metadata.json
// manifest; Phase 1 stores it in the deliverable JSON per the briefing).
type TaskAttachment struct {
	// OriginalAbsPath is the absolute path of the file as given at submit
	// (the source of the copy).
	OriginalAbsPath string `json:"original_abs_path"`
	// ReplicaRelPath is the copy's path relative to the task session dir
	// (the absolute source path minus its leading '/' — the bidirectional
	// mapping key: <session_dir>/<replica_rel_path> is the replica). An
	// EMPTY value keeps its manifest position and renders
	// TaskAttachmentReplicaMissingLabel instead of being dropped — see that
	// constant for the contract (positional index integrity).
	ReplicaRelPath string `json:"replica_rel_path"`
	// SHA256 is the hex digest of the copied content (integrity check —
	// the LLM can verify a replica it reads against the recorded digest).
	SHA256 string `json:"sha256"`
	// FileSize is the copied file size in bytes.
	FileSize int64 `json:"file_size"`
	// SubmittedAt is the copy's wall-clock submission time (unix seconds).
	SubmittedAt int64 `json:"submitted_at"`
}

// TaskAttachmentIndex carries the run-time index the EPIC block injects
// ([attachmentN: <replica abs path>]): the replica paths read by the LLM's
// file-read tool. Ordered — index N maps to the Nth submit --attachment
// argument, in submission order.
type TaskAttachmentIndex struct {
	// Path is the replica's absolute path (under the task session dir).
	Path string `json:"path"`
}

// taskLegacySessionsRootName is the Phase-1 container root that S1 retired:
// <gopi-dir>/task-sessions/. Roughly a dozen pre-S1 task containers live
// there (task-sessions/<task_id>/attachments/...); the migration below
// moves them into the sessions tree on first use.
const taskLegacySessionsRootName = "task-sessions"

// taskSessionDir returns the per-task asset container for taskID:
// <gopi-dir>/sessions/<taskID> (session-dir-unify S1 — same root as the
// session run logs). A taskID here is always the ledger id (t-<unix> or an
// explicit submit --task-id) — the container is keyed by the LEDGER
// identity, which is what the wrapper/EPIC/run plumbing holds.
func taskSessionDir(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	// THE SHAPE GATE (bug row 01a12f3c, 2026-10-12): the id is about to become
	// a PATH ELEMENT, and this tree spells a task id in exactly two admissible
	// ways (canonical uuid, or the t-… auxiliary/legacy channel). Anything else
	// — measured: the branch/worktree spelling, a uuid with every dash dropped
	// — used to be joined silently, which is how a caller holding the branch
	// spelling minted a SECOND container beside the row's own. Refused here, at
	// the ONE seam every task-container path goes through, so the refusal
	// reaches attachments, reports, worker logs and the signal-record
	// destination alike. An EMPTY id keeps its long-standing contract (it
	// resolves the sessions root — see TestCovTaskSessionDirEmptyIDBoundary),
	// so id "" is NOT gated.
	if taskID != "" && !sessionid.IsAdmissible(taskID) {
		// sessionidRefusal, not sessionid.Refuse directly: the refusal carries
		// the OPERAND's provenance (which channel handed this process the
		// string), the one fact the leaf cannot see — see
		// internal/tools/sessionid_refusal.go.
		return "", fmt.Errorf("task session dir: %w", sessionidRefusal(taskID))
	}
	gd, err := config.GopiDir()
	if err != nil {
		return "", err
	}
	// One-shot Phase-1 → S1 relocation (idempotent, best-effort).
	taskMigrateLegacySessionDir(taskID)
	return filepath.Join(gd, taskSessionsRootName, taskID), nil
}

// taskMigrateLegacySessionDir moves one Phase-1 container from the retired
// <gopi-dir>/task-sessions/<taskID> root into the S1 location
// <gopi-dir>/sessions/<taskID> (os.Rename — same filesystem, atomic).
// The legacy root is small (~a dozen dirs, each with an attachments/ tree);
// the SPEC's S1 boundary says "量小可移" — migrate rather than dual-read.
// Idempotent + collision-safe: a container already at the new location wins
// (the legacy copy is removed); an absent legacy container is a no-op.
// Best-effort — a failed migration logs and returns the S1 path anyway
// (the caller creates the dir on use).
func taskMigrateLegacySessionDir(taskID string) {
	gd, err := config.GopiDir()
	if err != nil {
		return
	}
	legacy := filepath.Join(gd, taskLegacySessionsRootName, taskID)
	fi, err := os.Stat(legacy)
	if err != nil || !fi.IsDir() {
		return // nothing to migrate (or legacy root already gone)
	}
	dst := filepath.Join(gd, taskSessionsRootName, taskID)
	if _, err := os.Stat(dst); err == nil {
		// New location already exists — drop the legacy copy.
		os.RemoveAll(legacy)
		return
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return
	}
	if err := os.Rename(legacy, dst); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] task session: migrate %s → %s failed (continuing): %v\n", legacy, dst, err)
		return
	}
	fmt.Fprintf(os.Stderr, "[gopi] task session: migrated %s → %s\n", legacy, dst)
}

// TaskSessionDir exposes taskSessionDir to the cmd/agent package (the EPIC
// injection composes replica paths there; Rule 3 forbids widening the
// internal/tools surface for a helper that is one directory join away).
func TaskSessionDir(taskID string) (string, error) { return taskSessionDir(taskID) }

// taskAttachmentDir returns the attachments copy area of a task session dir.
func taskAttachmentDir(taskID string) (string, error) {
	sd, err := taskSessionDir(taskID)
	if err != nil {
		return "", err
	}
	return filepath.Join(sd, taskAttachmentsDirName), nil
}

// TaskAttachmentsDir exposes taskAttachmentDir (cmd/agent reads replica
// paths for the [attachmentN:] index and needs the attachments root).
func TaskAttachmentsDir(taskID string) (string, error) { return taskAttachmentDir(taskID) }

// taskAttachmentRelPath maps an attachment's original absolute path to its
// replica path relative to the task session dir: the absolute path minus its
// leading '/' ("" on a non-absolute path — the submit face validates
// absolute-ness before calling). /tmp/log.txt → tmp/log.txt;
// /home/user/log.txt → home/nick/log.txt — same-name files in different
// directories land in different replica subdirectories and never collide.
func taskAttachmentRelPath(absPath string) string {
	absPath = filepath.ToSlash(strings.TrimSpace(absPath))
	if !strings.HasPrefix(absPath, "/") {
		return ""
	}
	return strings.TrimPrefix(absPath, "/")
}

// TaskAttachmentRelPath exposes taskAttachmentRelPath (the cmd tests verify
// the expansion rule through the exported face).
func TaskAttachmentRelPath(absPath string) string { return taskAttachmentRelPath(absPath) }

// taskAttachmentsAreaRel is the INVERSE of taskAttachmentRelPath: it turns a
// manifest's replica_rel_path ("attachments/home/user/log.txt", relative to the
// task session dir) back into the path relative to the session dir's
// ATTACHMENTS AREA ("home/nick/log.txt"), which is what taskAttachmentCopyTo
// takes. One helper, because copyTo re-applies the area prefix — passing the
// session-relative form straight through lands the replica one "attachments/"
// too deep, which is a path task show prints and the reader cannot open.
// A rel that does not carry the area prefix is an error (never a guess).
func taskAttachmentsAreaRel(rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	prefix := taskAttachmentsDirName + "/"
	if !strings.HasPrefix(rel, prefix) {
		return "", fmt.Errorf("replica_rel_path %q is not under %q", rel, taskAttachmentsDirName)
	}
	rest := strings.TrimPrefix(rel, prefix)
	if rest == "" || filepath.Clean(rest) == "." {
		return "", fmt.Errorf("replica_rel_path %q names no file", rel)
	}
	return rest, nil
}

// taskAttachmentCopy performs the single-file copy of one attachment into
// the task session dir's attachments area (Phase-1 scope: files only — the
// submit face validates a regular-file source; directories are refused
// there). The source is read through os.Stat + os.Open so a symlink
// argument copies the LINKED FILE's real content (dereference — spec); the
// original is never moved or modified. The replica path preserves the
// original absolute directory structure (attachments/tmp/log.txt etc.).
// Returns the copy's sha256 + size (the manifest fields).
func taskAttachmentCopy(taskID, srcAbs string) (shaHex string, size int64, err error) {
	rel := taskAttachmentRelPath(srcAbs)
	if rel == "" {
		return "", 0, fmt.Errorf("attachment %q is not an absolute path", srcAbs)
	}
	return taskAttachmentCopyTo(taskID, srcAbs, rel)
}

// taskAttachmentCopyTo is the ONE file-copy implementation, taking the
// replica's path relative to the session dir's attachments area explicitly.
// The caller-supplied attachment face mirrors the source's absolute path
// (taskAttachmentCopy above); a SYNTHETIC source — a text the engine itself
// materialized, with no submitter path to mirror (the payload spill) — names
// its replica directly instead, because mirroring an internal temp path
// would bury the replica under a meaningless deep directory chain.
// Validation (regular file, dereferenced) is identical for both faces: a
// replica is only ever written from a validated regular file.
func taskAttachmentCopyTo(taskID, srcAbs, rel string) (shaHex string, size int64, err error) {
	attDir, err := taskAttachmentDir(taskID)
	if err != nil {
		return "", 0, err
	}
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" {
		return "", 0, fmt.Errorf("attachment %q: empty replica path", srcAbs)
	}
	// os.Stat follows symlinks: the mode/size below describe the TARGET,
	// not the link node — dereferencing is the spec's copy semantic.
	fi, err := os.Stat(srcAbs)
	if err != nil {
		return "", 0, fmt.Errorf("attachment %q: %w", srcAbs, err)
	}
	if !fi.Mode().IsRegular() {
		return "", 0, fmt.Errorf("attachment %q is not a regular file (mode %v) — only files are copied", srcAbs, fi.Mode())
	}
	in, err := os.Open(srcAbs)
	if err != nil {
		return "", 0, fmt.Errorf("attachment %q: open: %w", srcAbs, err)
	}
	defer in.Close()
	dst := filepath.Join(attDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, fmt.Errorf("attachment %q: mkdir replica dir: %w", srcAbs, err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", 0, fmt.Errorf("attachment %q: create replica: %w", srcAbs, err)
	}
	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hasher), in)
	if err != nil {
		out.Close() //stylegate:ignore writer close after the payload was flushed by a checked call
		return "", 0, fmt.Errorf("attachment %q: copy: %w", srcAbs, err)
	}
	if err := out.Close(); err != nil {
		return "", 0, fmt.Errorf("attachment %q: close replica: %w", srcAbs, err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), n, nil
}

// taskEnsureTaskSessionDirs creates the task session container for taskID
// with its attachments subdirectory (MkdirAll — idempotent; a re-submit
// with the same explicit task-id reuses the existing container and adds
// replicas). Returns the session dir. The metadata.json / payload.txt /
// artifacts / logs memberships arrive in later phases — Phase 1 creates
// only attachments/ (the briefing's explicit boundary: avoid over-creating
// empty dirs).
func taskEnsureTaskSessionDirs(taskID string) (string, error) {
	sd, err := taskSessionDir(taskID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(sd, taskAttachmentsDirName), 0o755); err != nil {
		return "", fmt.Errorf("task session dir for %s: %w", taskID, err)
	}
	return sd, nil
}

// taskSubmitMetadataFileName is the skeleton metadata.json a submit writes
// into the task session container (SPEC db-cleanup-uuid-taskid item 2: the
// sessions/<uuid>/ dir is born with its metadata, so the pending row's
// identity/scope is inspectable the moment the uuid is returned — before
// the task is ever claimed). The run-time logger (agent/session.go
// writeMetadataJSON) MERGES into this same file (read-modify-write,
// preserving the submit fields), so the submit skeleton and the run-time
// session metadata coexist in one metadata.json.
const taskSubmitMetadataFileName = "metadata.json"

// taskWriteSubmitMetadata writes the submit-time skeleton metadata.json for
// a minted task's session container: identity (task_id) + provenance
// (summary/payload/source/tags/priority/parent) + status "pending" +
// submitted_at + the full attachment manifest. The manifest's canonical
// home stays the deliverable column (the submit call site renders it); this
// file is the container-local mirror a consumer (or the LLM) reads without
// a DB handle. Best-effort by design — the container + the ledger row are
// the truth; the caller logs a metadata failure and never fails the
// enqueue (mirrors the run-time writeMetadataJSON contract).
func taskWriteSubmitMetadata(taskID string, o TaskSubmitOptions, atts []TaskAttachment) error {
	sd, err := taskEnsureTaskSessionDirs(taskID)
	if err != nil {
		return err
	}
	meta := map[string]interface{}{
		"task_id":      taskID,
		"status":       "pending",
		"summary":      o.Summary,
		"source":       o.Source,
		"submitted_at": time.Now().UTC().Format(time.RFC3339),
	}
	if o.Payload != "" {
		meta["payload"] = o.Payload
	}
	if len(o.Tags) > 0 {
		meta["tags"] = o.Tags
	}
	if o.Priority > 0 {
		meta["priority"] = o.Priority
	}
	if o.ParentTaskID != "" {
		meta["parent_task_id"] = o.ParentTaskID
	}
	// THE CREATOR, in the session's own metadata as well as the row's
	// submitter_session column (task_fixture_reclaim.go): the reclaim reads the
	// LEDGER first, but a reader holding only a session directory must be able
	// to answer "who filed this row?" without a DB handle.
	if sid := strings.TrimSpace(o.SessionID); sid != "" {
		meta[TaskSubmitCreatorSessionKey] = sid
	}
	if len(atts) > 0 {
		meta["attachments"] = atts
	}
	out, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(sd, taskSubmitMetadataFileName), out, 0o644)
}

// taskDeliverableGround is ONE secondary ground of a row: a directory the run
// CREATED that is not the ground its deliverable already records.
//
// WHY THE AXIS IS PLURAL AND WHY IT EXISTS AT ALL (measured 2026-09-26, row
// 01a0ddc4-c410): a run's deliverable ground is single-valued on purpose — the
// primary worktree/branch are the row's own ground and a detection seam must
// never REBIND them (the 2026-09-12 foreign-owner hijack). But a run may also
// create a SECOND ground mid-flight (the incident's own shape: the row named
// .../gopi-01a0ddc4… on task/01a0ddc4… while the run minted
// .../gopi-evi-pushback on fix/pushback-rule31-evidence for an evidence
// worktree). That directory was left in NO record anywhere: not the row (the
// primary axes were taken), not the run-ground anchor (the creation seam
// returned at its FIRST resolvable candidate, the run's own), and therefore not
// resumable and not attributable — `gopi task show --branch fix/pushback-rule31-
// evidence` answered "no owner" while that branch lived in a registered linked
// worktree. The plural axis is the place the ROW has for that second ground,
// and the primary axes stay exactly as authoritative as before.
type taskDeliverableGround struct {
	Repo     string `json:"repo,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
}

// taskDeliverable is a parsed deliverable blob: the PRIMARY axes (repo/branch/
// worktree), the SECONDARY grounds a run created (Grounds), and the submit-time
// attachment manifest verbatim.
//
// It is a struct rather than four return values for the reason the attachments
// parameter already documents: a field the parser returns is a field a rebuild
// cannot silently DROP. Grounds joins that set at the same price — a writer that
// rebuilds a blob from a parse carries the whole blob forward, never the three
// fields it happened to ask for.
type taskDeliverable struct {
	Repo        string
	Branch      string
	Worktree    string
	Grounds     []taskDeliverableGround
	Attachments json.RawMessage
}

// taskDeliverableParse splits a deliverable blob into its fields (lenient: every
// field zero on a missing/empty/non-JSON blob — "" repo, nil attachments). The
// attachments entries come back as raw JSON objects (the submit writes
// original_abs_path etc.; consumers needing a typed view decode them — the merge
// helpers here only need to PRESERVE the array byte-for-byte, never reinterpret
// it).
func taskDeliverableParse(raw string) taskDeliverable {
	var d taskDeliverable
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return d
	}
	var wire struct {
		Repo        string                  `json:"repo"`
		Branch      string                  `json:"branch"`
		Worktree    string                  `json:"worktree"`
		Grounds     []taskDeliverableGround `json:"grounds"`
		Attachments json.RawMessage         `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return taskDeliverable{}
	}
	d.Repo, d.Branch, d.Worktree = wire.Repo, wire.Branch, wire.Worktree
	d.Grounds, d.Attachments = wire.Grounds, wire.Attachments
	return d
}

// taskDeliverableGroundsJSON renders the grounds array for a rebuild, or nil
// when there is nothing to carry — one renderer, so the axis can never be
// spelled two ways (Rule 13/14).
func taskDeliverableGroundsJSON(grounds []taskDeliverableGround) json.RawMessage {
	if len(grounds) == 0 {
		return nil
	}
	b, err := json.Marshal(grounds)
	if err != nil {
		return nil
	}
	return b
}

// taskDeliverableRebuild renders a deliverable blob from a parsed deliverable,
// preserving every axis the parse carried: the primary repo/branch/worktree,
// any SECONDARY grounds, and any existing attachments array (nil = no
// attachments key — the blob stays v1-shaped; a non-nil array marshals as v2
// with the key present). Ordered keys: repo, branch, worktree, grounds,
// attachments — a stable rendering keeps byte-diffing deterministic.
//
// IT TAKES THE WHOLE PARSE, not the fields one writer happens to own. That is
// the attachment precedent (a worktree backfill must never drop the attachment
// manifest) extended to the grounds axis: a writer that rebuilds from a parse
// carries the entire blob forward, so an axis added here cannot be silently
// erased by a writer that never heard of it (Rule 15 — the loss would be
// invisible).
func taskDeliverableRebuild(d taskDeliverable) string {
	out := map[string]interface{}{}
	if d.Repo != "" {
		out["repo"] = d.Repo
	}
	if d.Branch != "" {
		out["branch"] = d.Branch
	}
	if d.Worktree != "" {
		out["worktree"] = d.Worktree
	}
	if grounds := taskDeliverableGroundsJSON(d.Grounds); len(grounds) > 0 {
		out["grounds"] = grounds
	}
	if len(d.Attachments) > 0 {
		out["attachments"] = d.Attachments
	}
	if len(out) == 0 {
		return ""
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

// taskDeliverableRenderV2 renders a full v2 deliverable blob carrying the
// attachments manifest (submit-time): {repo, branch?, worktree?,
// attachments:[...]}. repo is required (the M4.8 closure); attachments is
// the marshaled manifest array (nil/empty = no key — a no-attachment submit
// keeps a repo-only v1-shaped blob so downstream repoOnly/forwardable logic
// is untouched by the phase). Marshal failure returns "" (the caller treats
// it as no deliverable — matches taskDeliverableRepoJSON's contract).
func taskDeliverableRenderV2(repo string, atts []TaskAttachment) string {
	out := map[string]interface{}{"repo": repo}
	if len(atts) > 0 {
		out["attachments"] = atts
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

// taskDeliverableAttachmentIndex builds the ordered [attachmentN:] index for
// a task's deliverable: each manifest entry resolves to its replica
// ABSOLUTE path (<session-dir>/<replica_rel_path>) and is stamped with its
// original source path (the SPEC's bidirectional mapping surfaced to the
// LLM — knowing which original file a replica mirrors lets the model reason
// about "the screenshot at /tmp/x.png" without guessing). Ordered by
// submission order (the manifest array order). A nil/absent attachments
// array returns nil (no attachments — the EPIC block omits the index).
//
// LENGTH CONTRACT (attachment-index positional contract): the returned index
// is ALWAYS the same length as the decoded manifest, so caller-side
// positional pairing (task show's atts[i]/idx[i]) can never mislabel a
// replica. A manifest entry whose replica_rel_path is empty/whitespace is
// therefore NOT dropped — its slot survives and carries the explicit
// TaskAttachmentReplicaMissingLabel (see that constant for why: dropping
// shortens the list and shifts every later replica one slot, and the
// [attachmentN:] briefing numbering then diverges from task show for the
// same row).
func taskDeliverableAttachmentIndex(taskID string, raw string) ([]TaskAttachmentIndex, error) {
	attJSON := taskDeliverableParse(raw).Attachments
	if len(attJSON) == 0 {
		return nil, nil
	}
	var atts []TaskAttachment
	if err := json.Unmarshal(attJSON, &atts); err != nil {
		return nil, fmt.Errorf("task %s: attachments manifest not JSON: %w", taskID, err)
	}
	sd, err := taskSessionDir(taskID)
	if err != nil {
		return nil, err
	}
	idx := make([]TaskAttachmentIndex, 0, len(atts))
	for _, a := range atts {
		rel := strings.TrimSpace(a.ReplicaRelPath)
		if rel == "" {
			// Position-preserving placeholder: the entry is present but has
			// no resolvable replica path (the contract above).
			idx = append(idx, TaskAttachmentIndex{Path: TaskAttachmentReplicaMissingLabel})
			continue
		}
		idx = append(idx, TaskAttachmentIndex{
			Path: filepath.Join(sd, filepath.FromSlash(rel)),
		})
	}
	return idx, nil
}

// taskDeliverableAttachmentsRaw is the show-time read face for one row's raw
// deliverable blob: parses the attachments manifest (typed) AND the resolved
// replica index from the blob alone (no DB handle needed — taskShowWithDB
// already holds the row). Returns (nil,nil,nil) when the blob has no
// attachments array. An unparsable manifest returns the error (the caller
// renders a marker line rather than swallowing a corrupt deliverable).
func taskDeliverableAttachmentsRaw(taskID, raw string) (atts []TaskAttachment, idx []TaskAttachmentIndex, err error) {
	if len(strings.TrimSpace(raw)) == 0 {
		return nil, nil, nil
	}
	idx, err = taskDeliverableAttachmentIndex(taskID, raw)
	if err != nil {
		return nil, nil, err
	}
	attJSON := taskDeliverableParse(raw).Attachments
	if len(attJSON) == 0 {
		return nil, nil, nil
	}
	if err := json.Unmarshal(attJSON, &atts); err != nil {
		return nil, nil, err
	}
	return atts, idx, nil
}

// TaskDeliverableAttachments returns a task's attachment manifest (typed)
// plus the replica absolute paths (the EPIC index material) — the run-time
// read face for taskRunBriefing's attachment injection. Returns (nil, nil)
// when the row has no attachments. Reads the row by task_id.
//
// LENGTH CONTRACT (attachment-index positional contract): replicaPaths has
// exactly one element per manifest entry — len(replicaPaths) == len(atts) —
// so the briefing's [attachmentN:] numbering and task show's per-entry line
// agree on order and count for the same row. An entry with an empty
// replica_rel_path occupies its slot carrying
// TaskAttachmentReplicaMissingLabel rather than vanishing (see
// taskDeliverableAttachmentIndex).
func TaskDeliverableAttachments(taskID string) (atts []TaskAttachment, replicaPaths []string, err error) {
	taskID = taskIDNormalize(taskID)
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return nil, nil, err
	}
	if t == nil {
		return nil, nil, nil
	}
	attJSON := taskDeliverableParse(t.Deliverable).Attachments
	if len(attJSON) == 0 {
		return nil, nil, nil
	}
	if err := json.Unmarshal(attJSON, &atts); err != nil {
		return nil, nil, fmt.Errorf("task %s: attachments manifest not JSON: %w", taskID, err)
	}
	idx, err := taskDeliverableAttachmentIndex(taskID, t.Deliverable)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range idx {
		replicaPaths = append(replicaPaths, e.Path)
	}
	return atts, replicaPaths, nil
}

// taskDeliverableVersionStamp is the version a submit stamps when its
// deliverable carries attachments (deliverableVersionKnown must equal
// taskDeliverableVersionWithAttachments — see task_session.go doc). The
// sync gate reads the row column separately; this const exists so the
// submit and the const live adjacently.
const taskSubmitDeliverableVersion = taskDeliverableVersionWithAttachments

// TaskCopyAttachments copies the submitted attachment paths (absolute) into
// the task's session dir and returns the manifest. This is THE Phase-1 copy
// entry, called by TaskSubmit right after the task_id is minted and BEFORE
// the ledger INSERT — a copy failure aborts the whole submit (an attachment
// that cannot be archived must not be silently dropped: the EPIC briefing
// would reference a file the session never received). The task session dir
// is created here (the row is about to exist — creating the container just
// before the INSERT keeps the row+assets atomic from the queue's
// perspective). Order preserved; same-name files in different dirs land in
// different replica subdirs (taskAttachmentRelPath).
//
// A duplicate attachment (the same absolute path twice) copies twice — the
// manifest then lists two entries with identical replica paths. That is
// harmless (same content, same digest) and preserves the submitter's exact
// argument count for the [attachmentN:] index; the run-time index stays
// ordered 1..N as submitted.
//
// Validation precedes copying: EVERY path is checked (absolute + a readable
// regular file, symlink-dereferenced) before the FIRST copy lands — a bad
// second attachment must not leave a half-copied first one behind (the
// whole submit aborts, so the session dir would orphan partial replicas).
func TaskCopyAttachments(taskID string, paths []string) ([]TaskAttachment, error) {
	taskID = taskIDNormalize(taskID)
	var manifest []TaskAttachment
	if len(paths) == 0 {
		return manifest, nil
	}
	// Pass 1: validate every path before any copy.
	for _, p := range paths {
		abs := strings.TrimSpace(p)
		if !filepath.IsAbs(abs) {
			return nil, fmt.Errorf("task %s: attachment %q is not an absolute path (submit --attachment <abs_path>)", taskID, p)
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("task %s: attachment %q: %w", taskID, abs, err)
		}
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("task %s: attachment %q is not a regular file (mode %v) — only files are copied", taskID, abs, fi.Mode())
		}
	}
	if _, err := taskEnsureTaskSessionDirs(taskID); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	// Pass 2: copy (all paths validated — no partial-abort hazard).
	for _, p := range paths {
		abs := strings.TrimSpace(p)
		sha, size, err := taskAttachmentCopy(taskID, abs)
		if err != nil {
			return nil, err
		}
		// replica_rel_path is the copy's path relative to the SESSION DIR
		// (the spec's bidirectional mapping key — <session_dir>/<rel> is
		// the replica). The copy physically lands under the attachments
		// area, so the recorded rel carries the attachments/ prefix:
		// <abs-minus-root> "tmp/log.txt" → "attachments/tmp/log.txt".
		// Consumers resolve the replica absolute path with ONE join
		// (<session_dir>/<replica_rel_path>) — never re-deriving the area.
		rel := filepath.ToSlash(filepath.Join(taskAttachmentsDirName, taskAttachmentRelPath(abs)))
		manifest = append(manifest, TaskAttachment{
			OriginalAbsPath: abs,
			ReplicaRelPath:  rel,
			SHA256:          sha,
			FileSize:        size,
			SubmittedAt:     now,
		})
	}
	return manifest, nil
}
