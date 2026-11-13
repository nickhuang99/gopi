package tools

// Attachment CLAIM detection: "the payload says evidence is attached, but the
// row carries no attachment manifest."
//
// WHY THIS EXISTS (measured, task 01a0b53b): the attachments feature archives
// real files (TaskCopyAttachments → manifest in the row's deliverable JSON →
// `task show` renders one line per entry). A payload that ASSERTS evidence was
// attached while the submitter passed no `attachments` produced a row whose
// `task show` printed NOTHING for attachments — indistinguishable from a row
// that merely mentions a path. The reader (operator or dispatched child) then
// hunts for evidence that was never archived: the false claim is silent, which
// is the failure mode Rule 15 (no silent failure) forbids.
//
// THE CONTRACT (two faces, one predicate — Rule 14):
//   - READ face: TaskShow renders TaskAttachmentClaimMarker(phrase) when the
//     row has NO manifest and its payload/summary prose asserts an attachment.
//   - WRITE face: TaskSubmit appends TaskSubmitAttachmentClaimWarningText's
//     output to its confirmation (which the CLI and the task_submit tool both
//     surface) under the same conditions, so the author learns BEFORE the row
//     is dispatched.
//
// WHAT THE PREDICATE DELIBERATELY DOES NOT DO:
//   - It never looks for bare file paths. A payload quoting
//     "/home/nick/workspace/x.log" (a file the run itself writes, a cited
//     source, a scratch path) is ordinary prose and stays legal — flagging it
//     would fire on most real payloads and the marker would be ignored. Only an
//     asserting PHRASE fires.
//   - It carries no bare-article entry ("the attachment"): a payload DISCUSSING
//     the attachments feature ("the attachments area is created when …") is
//     ordinary prose too, and an article is not a claim. Every phrase here is
//     claim-SHAPED (something attached / see the attachment / included as
//     attachment).
//   - It is not a gate: the row still submits and stays dispatchable. A wrong
//     prose phrase is a diagnosability defect, not a reason to refuse work (an
//     abort would let a wording nit block a dispatch, and a false-claim row is
//     still better than no row).
//   - It does not fire when a manifest exists: that row's [attachmentN:] block
//     already names what IS archived, so the marker would be a lie.
//
// WHY THE CONSTANTS LIVE HERE and not in a leaf package (Rule 14's "shared
// values live in a small exported-constant package"): these have exactly ONE
// consumer — this package (TaskShow's renderer and TaskSubmit's warning share
// one predicate). cmd/agent prints the marker text it receives as part of the
// rendered string, so no second package needs a literal; a package for a single
// consumer would be the mirroring Rule 14 bans. The anti-regression guard is
// the grep-gate test (task_attachment_claim_test.go): the marker strings may
// appear ONLY in this file, so a re-inline fails CI (Rule 13).

import (
	"fmt"
	"sort"
	"strings"
)

// Attachment-asserting phrases: every one CLAIMS that evidence is archived with
// the task. Matched case-insensitively on word boundaries (see
// TaskAttachmentClaimScan). Named constants, never inline literals (Rules
// 1b/13); the phrase TABLE below is derived from them at init so the two halves
// can never drift.
const (
	// Attachment claimed relative to the task/row/session it was submitted
	// with — the measured defect's shape ("(attached to the originating
	// session)" in task 01a0b535's payload).
	attachClaimPhraseAttachedToOriginating = "attached to the originating session"
	attachClaimPhraseAttachedToThisTask    = "attached to this task"
	attachClaimPhraseAttachedToTheTask     = "attached to the task"
	attachClaimPhraseAttachedToTheRow      = "attached to the row"
	attachClaimPhraseAttachedToTheSession  = "attached to the session"
	attachClaimPhraseAttachedWithTheTask   = "attached with the task"
	attachClaimPhraseAttachedForRef        = "attached for reference"
	attachClaimPhraseAttachedSeparately    = "attached separately"

	// A screenshot/image is claimed archived.
	attachClaimPhraseAttachedScreenshots = "attached screenshots"
	attachClaimPhraseAttachedScreenshot  = "attached screenshot"
	attachClaimPhraseScreenshotsAttached = "screenshots attached"
	attachClaimPhraseScreenshotAttached  = "screenshot attached"
	attachClaimPhraseAttachedImages      = "attached images"
	attachClaimPhraseAttachedImage       = "attached image"
	attachClaimPhraseImagesAttached      = "images attached"
	attachClaimPhraseImageAttached       = "image attached"

	// A file is claimed archived.
	attachClaimPhraseAttachedFiles = "attached files"
	attachClaimPhraseAttachedFile  = "attached file"
	attachClaimPhraseFilesAttached = "files attached"

	// A log is claimed archived — the most common evidence shape in this repo.
	attachClaimPhraseAttachedLogs = "attached logs"
	attachClaimPhraseAttachedLog  = "attached log"
	attachClaimPhraseLogsAttached = "logs attached"
	attachClaimPhraseLogAttached  = "log attached"

	// A diff/patch is claimed archived.
	attachClaimPhraseAttachedDiff  = "attached diff"
	attachClaimPhraseAttachedPatch = "attached patch"
	attachClaimPhraseDiffAttached  = "diff attached"
	attachClaimPhrasePatchAttached = "patch attached"

	// Evidence in general.
	attachClaimPhraseAttachedEvidence = "attached evidence"
	attachClaimPhraseEvidenceAttached = "evidence attached"

	// A recording is claimed archived.
	attachClaimPhraseAttachedRecording = "attached recording"
	attachClaimPhraseRecordingAttached = "recording attached"

	// The attachment itself is called out.
	attachClaimPhraseAttachmentAttached = "attachment attached"
	attachClaimPhraseAttachmentIncluded = "attachment included"
	attachClaimPhraseIncludedAsAttach   = "included as attachment"
	attachClaimPhraseAsAnAttachment     = "as an attachment"
	attachClaimPhraseAsAttachments      = "as attachments"

	// Deictic references to an attachment the reader is told to open.
	attachClaimPhraseSeeTheAttachmentList = "see the attachment list"
	attachClaimPhraseSeeTheAttachments    = "see the attachments"
	attachClaimPhraseSeeTheAttachment     = "see the attachment"
	attachClaimPhraseSeeAttachments       = "see attachments"
	attachClaimPhraseSeeAttachment        = "see attachment"

	// Positional claims ("attached below") — the payload points at a place
	// where an attachment should be, which is the same assertion.
	attachClaimPhraseAttachedBelow = "attached below"
	attachClaimPhraseAttachedAbove = "attached above"
	attachClaimPhraseAttachedHere  = "attached here"

	// The attachment is described as carrying the screenshots.
	attachClaimPhraseScreensInAttach = "screenshots in the attachment"
)

// taskAttachmentClaimPhrases is the ordered match set — LONGEST FIRST, so a
// payload carrying both "attached file" and "see the attachment" reports the
// most specific diagnostic (the phrase is quoted in the marker). Derived at
// init from the declared constants: a hand-maintained order is drift waiting to
// happen, and an entry added to the const block but not to a hand-written table
// would silently never match (Rule 13). The test pins both invariants.
var taskAttachmentClaimPhrases = taskAttachmentClaimSortPhrases([]string{
	attachClaimPhraseAttachedToOriginating,
	attachClaimPhraseAttachedToThisTask,
	attachClaimPhraseAttachedToTheTask,
	attachClaimPhraseAttachedToTheRow,
	attachClaimPhraseAttachedToTheSession,
	attachClaimPhraseAttachedWithTheTask,
	attachClaimPhraseAttachedForRef,
	attachClaimPhraseAttachedSeparately,
	attachClaimPhraseAttachedScreenshots,
	attachClaimPhraseAttachedScreenshot,
	attachClaimPhraseScreenshotsAttached,
	attachClaimPhraseScreenshotAttached,
	attachClaimPhraseAttachedImages,
	attachClaimPhraseAttachedImage,
	attachClaimPhraseImagesAttached,
	attachClaimPhraseImageAttached,
	attachClaimPhraseAttachedFiles,
	attachClaimPhraseAttachedFile,
	attachClaimPhraseFilesAttached,
	attachClaimPhraseAttachedLogs,
	attachClaimPhraseAttachedLog,
	attachClaimPhraseLogsAttached,
	attachClaimPhraseLogAttached,
	attachClaimPhraseAttachedDiff,
	attachClaimPhraseAttachedPatch,
	attachClaimPhraseDiffAttached,
	attachClaimPhrasePatchAttached,
	attachClaimPhraseAttachedEvidence,
	attachClaimPhraseEvidenceAttached,
	attachClaimPhraseAttachedRecording,
	attachClaimPhraseRecordingAttached,
	attachClaimPhraseAttachmentAttached,
	attachClaimPhraseAttachmentIncluded,
	attachClaimPhraseIncludedAsAttach,
	attachClaimPhraseAsAnAttachment,
	attachClaimPhraseAsAttachments,
	attachClaimPhraseSeeTheAttachmentList,
	attachClaimPhraseSeeTheAttachments,
	attachClaimPhraseSeeTheAttachment,
	attachClaimPhraseSeeAttachments,
	attachClaimPhraseSeeAttachment,
	attachClaimPhraseAttachedBelow,
	attachClaimPhraseAttachedAbove,
	attachClaimPhraseAttachedHere,
	attachClaimPhraseScreensInAttach,
})

// taskAttachmentClaimSortPhrases orders the match set longest-first (ties by
// lexical order, so the result is deterministic). Longest-first is what makes
// the reported phrase the specific one: a payload carrying "attached file" also
// carries the generic "attached file" — but one carrying "attached files" must
// report the plural phrase, not the singular prefix of it.
func taskAttachmentClaimSortPhrases(in []string) []string {
	out := append([]string(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// Markers. TaskAttachmentClaimNoneLabel is the loud read-face marker TaskShow
// prints where the [Attachments] block would go; the warning constant is the
// write-face counterpart. Both are EXPORTED so the CLI face and the tests
// reference one spelling (Rule 13: never re-inline a diagnostic string).
const (
	// TaskAttachmentClaimNoneLabel is the `task show` marker printed when the
	// row has NO attachment manifest but its prose asserts one. The wording is
	// deliberately "NONE": a reader must not mistake the ABSENCE of the
	// [attachmentN:] block for "no claim was made".
	TaskAttachmentClaimNoneLabel = "[Attachments] NONE — but the payload references an attachment"
	// TaskAttachmentClaimNoneTail is the marker's second sentence: what the
	// operator may conclude from the row (the referenced evidence is not on
	// disk for this task).
	TaskAttachmentClaimNoneTail = "No manifest, no replica files: the referenced evidence is NOT archived with this task."
	// TaskSubmitAttachmentClaimWarning is the submit-time warning TEMPLATE
	// appended to TaskSubmit's confirmation when the prose asserts an
	// attachment and no attachments argument was passed. The row still submits
	// — see the file comment for why refusal is the wrong instrument. The %q
	// slot takes the matched phrase; callers use
	// TaskSubmitAttachmentClaimWarningText, never Sprintf directly.
	TaskSubmitAttachmentClaimWarning = "warning: the payload asserts an attachment (%q) but no attachments were submitted — nothing was archived with this task; pass the file's absolute path via --attachment/attachments, or drop the claim"
)

// TaskSubmitAttachmentClaimWarningText renders the write-face warning for a
// submitter whose prose claims an attachment while nothing was archived. Same
// shape as the read-face marker: the diagnostic text lives in ONE constant and
// the matched phrase is quoted into it by this one helper (never by a caller).
func TaskSubmitAttachmentClaimWarningText(phrase string) string {
	return fmt.Sprintf(TaskSubmitAttachmentClaimWarning, phrase)
}

// TaskAttachmentClaimScan reports the first attachment-asserting phrase in the
// task's prose, searching the payload first and the summary second (the payload
// is the briefing the reader trusts; a claim there is the defect this predicate
// exists for). Both empty → no claim.
//
// Matching is case-insensitive and word-bounded: "attached" inside "reattached"
// does not match, so prose ABOUT an attachment having been made for the THIRD
// time ("the driver was reattached") is not misread as a claim. The returned
// phrase comes from the constant list — the caller quotes it, never raw payload
// bytes.
func TaskAttachmentClaimScan(summary, payload string) (string, bool) {
	if p, ok := taskAttachmentClaimScanText(payload); ok {
		return p, true
	}
	return taskAttachmentClaimScanText(summary)
}

// taskAttachmentClaimScanText is the single-text half of the scan. Folding is
// per haystack (ASCII lower) and only the boolean matters — no position
// arithmetic crosses a case-folding boundary.
func taskAttachmentClaimScanText(s string) (string, bool) {
	if strings.TrimSpace(s) == "" {
		return "", false
	}
	lower := strings.ToLower(s)
	for _, p := range taskAttachmentClaimPhrases {
		if taskAttachmentClaimMatch(lower, p) {
			return p, true
		}
	}
	return "", false
}

// taskAttachmentClaimMatch reports whether phrase p occurs in the (lower-cased)
// haystack bounded by non-word bytes on both sides. The word alphabet is ASCII
// [A-Za-z0-9_]: deliberately NOT full Unicode word-ness, so a phrase adjacent
// to a CJK character still matches ("见图 attached file" — the CJK byte is
// non-word), which is what a mixed-language payload needs, while a phrase
// embedded in a longer identifier ("preattached-check") does not.
func taskAttachmentClaimMatch(haystack, phrase string) bool {
	from := 0
	for {
		i := strings.Index(haystack[from:], phrase)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(phrase)
		leftOK := start == 0 || !taskAttachmentClaimWordByte(haystack[start-1])
		rightOK := end == len(haystack) || !taskAttachmentClaimWordByte(haystack[end])
		if leftOK && rightOK {
			return true
		}
		from = start + 1
	}
}

// taskAttachmentClaimWordByte reports whether b counts as a word byte for
// boundary purposes (the ASCII identifier alphabet).
func taskAttachmentClaimWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '_':
		return true
	}
	return false
}

// TaskAttachmentClaimMarker renders the read-face marker for `task show`: the
// label, the matched phrase (what the prose actually claimed), and the tail
// sentence. One line, so it lands where the [attachmentN:] block would have
// been and reads as a table entry rather than a paragraph.
func TaskAttachmentClaimMarker(phrase string) string {
	return TaskAttachmentClaimNoneLabel + " (\"" + phrase + "\"). " + TaskAttachmentClaimNoneTail
}
