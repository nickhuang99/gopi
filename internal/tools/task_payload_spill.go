package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// taskPayloadInlineMaxBytes is the largest payload the submit funnel writes
// into the ledger payload column (and therefore the largest [Briefing
// payload] block a dispatched child receives). A payload beyond it is not
// truncated — it is PRESERVED VERBATIM as a task attachment and the payload
// column is replaced by a one-line pointer naming the replica (epic
// "task payload length must be caped"). Rule 1: the ONE knob; there is no
// env override (a size contract the dispatcher's context budget depends on
// must not be silently retunable per process).
//
// Why a size this small: the payload lands VERSATIM in the child LLM's
// first user message (taskRunBriefing's [Briefing payload] block), so it is
// a context-budget quantity, not a storage one. 16 KiB is ~4k tokens —
// large enough that every legitimate one-shot briefing (the ledger's
// historical payloads max out around 15 KB) stays inline and byte-identical
// to what it always was, and small enough that a pasted log/diff cannot
// crowd out the child's working context. The attachment replica is complete
// and byte-exact, and the pointer tells the child to read it, so the epic's
// "detail is too long, read attachment xxx" flow is what an oversized
// payload now gets instead of silent context growth.
const taskPayloadInlineMaxBytes = 16 << 10 // 16 KiB

// taskPayloadSpillSourceDirName is the subdirectory of the task session dir
// holding a spilled payload's SYNTHETIC source file. The spill needs an
// on-disk original for the regular attachment copy path (validation, sha256,
// replica mirroring); keeping it out of the session-dir root means the
// container root stays what it always was (metadata.json + run logs).
const taskPayloadSpillSourceDirName = "payload"

// taskPayloadSpillFileName is the spill source's file name (an extension
// the read tool renders as text — the payload is text by construction).
const taskPayloadSpillFileName = "spilled-payload.txt"

// taskPayloadSpillReplicaRel is the spilled payload's replica, relative to
// the session dir — the value recorded in the manifest's replica_rel_path
// and joined by every consumer (<session-dir>/<replica_rel_path>). It
// carries the attachments/ area prefix exactly like a mirrored entry does;
// the copy call takes only the name INSIDE that area (taskAttachmentCopyTo's
// rel), which is why the two are derived from one constant.
//
// The spill is a SYNTHETIC source: it has no submitter path to mirror, and
// mirroring its internal <session-dir>/payload/... path would bury the
// replica under a meaningless deep chain — so the replica is named directly
// inside the attachments area, where a mirrored entry can never collide
// (a mirrored rel begins with the source's absolute directory path, so it
// can never be exactly "spilled-payload.txt").
const taskPayloadSpillReplicaRel = taskAttachmentsDirName + "/" + taskPayloadSpillFileName

// taskPayloadSpillPointerPrefix opens the replacement payload line. It is a
// NAMED constant because it is a contract, not prose: the child LLM, a
// human reading `task show`, and the regression tests all recognize a
// spilled row by it.
const taskPayloadSpillPointerPrefix = "[Briefing payload omitted:"

// taskPayloadSpillAttachmentIndex is the manifest position the spilled
// payload occupies (1 = first). The spill is prepended to the user's
// --attachment list, and TaskSubmitOptions.Attachments documents index 1 as
// the payload slot, so `--attachment` never renumbers the payload pointer.
// The pointer text is rendered from THIS constant — a bare "attachment1"
// literal would drift the moment the ordering changed.
const taskPayloadSpillAttachmentIndex = 1

// taskPayloadOriginalName is the pointer's label for a spilled payload's
// source: a spilled payload has no submitter-given path (it arrived as
// --payload text / @file expansion), so the pointer names the ORIGINAL as
// the construct it was — while the attachment's `from:` line in task show
// names the on-disk spill file (the byte-exact artifact the manifest
// records). One text, two truthful descriptions: "where it came from" is
// the payload, "what was archived" is the file.
const taskPayloadSpillOriginalName = "the submitted --payload text"

// taskPayloadPointerPreviewBytes bounds the head-of-payload preview the
// pointer carries. The preview keeps an oversized SINGLE-LINE payload
// findable (branch-scan merge-backlog / agency dedup key on a payload
// substring, and `task list --grep` searches the payload column), while the
// hard byte cap keeps the pointer from re-growing the very block the spill
// exists to shrink. Applies to the payload's first line only.
const taskPayloadPointerPreviewBytes = 200

// taskSummaryHeadlineMaxBytes bounds the rendered headline (the clipped
// first line of an oversized summary). Rule 15: the headline is a POINTER
// to the preserved text, never the text itself.
const taskSummaryHeadlineMaxBytes = 400

// taskSpilledTextMerge separates two texts that both have to live in the
// one payload slot (an oversized summary's full text plus a distinct
// explicit payload) inside the single spilled attachment.
const taskSpilledTextMerge = "\n\n"

// taskInlineSpillCandidate returns the text that must leave the inline
// columns, or "" when it fits. Both the payload AND the summary go through
// this ONE budget: the summary is the ledger HEADLINE (task list renders
// it, dedup keys read it) but taskRunBriefing embeds it VERBATIM in the
// child's briefing too — the positional submit form
// (`gopi task submit "$(cat huge.txt)"`) puts the whole requested text in
// the summary column, so an oversized summary is the same context hazard as
// an oversized payload and gets the same remedy. Overflow is measured in
// BYTES (the quantity that actually reaches the child's context), not runes.
func taskInlineSpillCandidate(text string) string {
	if len(text) <= taskPayloadInlineMaxBytes {
		return ""
	}
	return text
}

// taskSummaryHeadline reduces an oversized summary to what the headline
// column is for: its FIRST LINE, clipped to taskSummaryHeadlineMaxBytes
// with an ellipsis. Only ever applied to a summary that already exceeds the
// inline budget — a normal headline is returned untouched, so the ledger
// face of every existing row is unchanged.
func taskSummaryHeadline(summary string) string {
	line := summary
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		line = strings.TrimSpace(summary)
	}
	if len(line) > taskSummaryHeadlineMaxBytes {
		line = strings.TrimRight(line[:taskSummaryHeadlineMaxBytes], " \t") + "…"
	}
	return line
}

// taskSpillPayload preserves an oversized payload verbatim as a task
// attachment and returns its manifest entry. Runs at SUBMIT time, right
// after the task id is minted — the same window TaskCopyAttachments uses —
// so the replica exists, hashed and recorded, before the ledger row is
// inserted (a claimed task never re-materializes it).
//
// Both copies are kept: the synthetic source at
// <session-dir>/payload/spilled-payload.txt is the byte-exact original the
// pointer's size/sha describe, and the regular attachment replica under the
// attachments area is what the [attachmentN:] index (and therefore the
// child's read tool) resolves. A failure aborts the whole submit (Rule 15:
// a pointer whose replica does not exist must never be written).
func taskSpillPayload(taskID, payload string) (TaskAttachment, error) {
	sd, err := taskEnsureTaskSessionDirs(taskID)
	if err != nil {
		return TaskAttachment{}, err
	}
	dir := filepath.Join(sd, taskPayloadSpillSourceDirName)
	if err := scratch.MkdirAllWithSuiteRunOwner(dir); err != nil {
		return TaskAttachment{}, fmt.Errorf("task %s: payload spill dir: %w", taskID, err)
	}
	src := filepath.Join(dir, taskPayloadSpillFileName)
	// The payload handed in is the stored (trimmed) text — TaskSubmit trims
	// before this runs — so writing it verbatim makes the spill byte-for-byte
	// equal to the payload the inline path would have stored.
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		return TaskAttachment{}, fmt.Errorf("task %s: payload spill write: %w", taskID, err)
	}
	sha, size, err := taskAttachmentCopyTo(taskID, src, taskPayloadSpillFileName)
	if err != nil {
		return TaskAttachment{}, fmt.Errorf("task %s: payload spill copy: %w", taskID, err)
	}
	return TaskAttachment{
		OriginalAbsPath: src,
		ReplicaRelPath:  taskPayloadSpillReplicaRel,
		SHA256:          sha,
		FileSize:        size,
		SubmittedAt:     time.Now().Unix(),
	}, nil
}

// taskPayloadReplicaAbsPath resolves a manifest entry's replica to its
// absolute path — the SAME single join every consumer uses
// (<session-dir>/<replica_rel_path>), kept in one place so the pointer text
// and the [attachmentN:] index can never describe different files.
func taskPayloadReplicaAbsPath(taskID string, att TaskAttachment) (string, error) {
	sd, err := taskSessionDir(taskID)
	if err != nil {
		return "", err
	}
	return filepath.Join(sd, filepath.FromSlash(strings.TrimSpace(att.ReplicaRelPath))), nil
}

// taskPayloadPointerText renders the ONE-LINE payload replacement for a
// spilled payload: what was too long, where the full text now lives, and
// the instruction to read it. It is deliberately a single line with no
// embedded newline — the child's briefing must stay a few hundred bytes,
// and a multi-line pointer would itself bloat the block the spill exists to
// shrink. The ABSOLUTE replica path is rendered into the line (not just
// "attachment1") so the pointer stays self-sufficient even for a row whose
// deliverable blob is absent (a repo-less submit drops the manifest, and a
// child must still find its briefing).
func taskPayloadPointerText(taskID, payload string, att TaskAttachment) string {
	replica, err := taskPayloadReplicaAbsPath(taskID, att)
	if err != nil {
		// Unresolvable session dir: fall back to the manifest's relative
		// path — a pointer naming a relative replica beats one naming none.
		replica = att.ReplicaRelPath
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %d bytes exceed the %d-byte inline limit — the full payload is preserved verbatim as attachment%d at %s (sha256 %s). Read that file with the read tool before starting; the [Attachments] index below repeats its path.]",
		taskPayloadSpillPointerPrefix, len(payload), taskPayloadInlineMaxBytes,
		taskPayloadSpillAttachmentIndex, replica, att.SHA256)
	if preview := taskPayloadPointerPreview(payload); preview != "" {
		fmt.Fprintf(&b, " (payload begins: %q — from %s)", preview, taskPayloadSpillOriginalName)
	}
	return b.String()
}

// taskSummaryPointerText renders the summary-column replacement for an
// oversized summary: the same shape as the payload pointer (absolute
// replica path + digest, one line), naming the SUMMARY as the preserved
// text so the child knows the attachment carries the whole request.
func taskSummaryPointerText(taskID, summary string, att TaskAttachment) string {
	replica, err := taskPayloadReplicaAbsPath(taskID, att)
	if err != nil {
		replica = att.ReplicaRelPath
	}
	return fmt.Sprintf("[Task summary omitted: %d bytes exceed the %d-byte inline limit — the full summary text is preserved verbatim as attachment%d at %s (sha256 %s). Read that file with the read tool before starting.]",
		len(summary), taskPayloadInlineMaxBytes, taskPayloadSpillAttachmentIndex, replica, att.SHA256)
}

// taskPointerEnvelopeMaxBytes bounds everything the pointer says AROUND the
// replica path: the size clause, the digest, the read instruction and the
// ≤taskPayloadPointerPreviewBytes preview. The PATH itself is deliberately
// not bounded — it is the file's identity (and for a repo-less submit, whose
// row carries no [Attachments] index, the only pointer the child has), and
// it is naturally short (a session dir plus the source's name). The
// envelope budget is what keeps the pointer from re-creating the context
// blow-up this mechanism prevents: a spilled payload was >16 KiB by
// construction, while the pointer stays under a kilobyte plus the path.
const taskPointerEnvelopeMaxBytes = 768

// taskPayloadPointerPreview returns the payload's first line clipped to
// taskPayloadPointerPreviewBytes — the retrieval breadcrumb that keeps an
// oversized single-line payload findable through payload-substring scans.
func taskPayloadPointerPreview(payload string) string {
	line := payload
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if len(line) > taskPayloadPointerPreviewBytes {
		line = strings.TrimRight(line[:taskPayloadPointerPreviewBytes], " \t")
	}
	return line
}

// taskSubmitPayloadSpillNote is the submit-time confirmation line for a
// spilled payload, appended to TaskSubmit's returned confirmation (and
// therefore visible to the CLI submitter AND to the task_submit tool's
// caller). Rule 15: the conversion is never silent — an author who shipped
// a 900 KB payload must learn that the ledger now carries a pointer.
func taskSubmitPayloadSpillNote(taskID, payload string, att TaskAttachment) string {
	replica, err := taskPayloadReplicaAbsPath(taskID, att)
	if err != nil {
		replica = att.ReplicaRelPath
	}
	return fmt.Sprintf("task_submit: payload is %d bytes — over the %d-byte inline limit; stored verbatim as attachment%d (%s) and the payload column now carries a one-line pointer (the dispatched child reads the file instead of receiving the text inline)",
		len(payload), taskPayloadInlineMaxBytes, taskPayloadSpillAttachmentIndex, replica)
}
