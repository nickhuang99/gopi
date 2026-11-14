package tools

// Attachment-claim tests: the false-claim marker (devdoc/attachment-claim-marker.md).
//
// The defect: a task payload could ASSERT that evidence is attached ("screenshot
// attached", "see the attachment") while the submitter archived nothing, and
// `task show` printed NOTHING — the fabricated claim was indistinguishable from
// a row that merely mentions a path. Four acceptance cases:
//
//	(a) marker FIRES — no manifest + asserting prose → the explicit NONE marker
//	(b) marker DOES NOT FIRE — no manifest + clean prose
//	(c) marker DOES NOT FIRE — prose that merely names paths or DISCUSSES the
//	    attachments feature (the false-positive guard: most real payloads name
//	    paths; flagging them makes the marker noise)
//	(d) a row WITH a manifest renders its [attachmentN:] lines and gets no marker
//
// Plus the write-face (TaskSubmit's warning) and the grep gate that keeps the
// marker literals at their single definition site (Rule 13).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claimTestEnv points GOPI_HOME and the ledger at temp dirs and returns the
// sandbox home (callers that need a real source file write under it).
func claimTestEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	return home
}

// claimTaskID maps a fixture name onto a task-unique row id (sibling coverage
// branches submit into the same sandbox, so ids must be prefix-unique).
func claimTaskID(name string) string {
	return "t-attclaim-" + strings.ReplaceAll(name, " ", "-")
}

// claimSubmitRow submits one fixture row and returns its id plus the raw
// submit confirmation (which the write-face warning rides on).
func claimSubmitRow(t *testing.T, name, summary, payload string, attachments []string) (string, string) {
	t.Helper()
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary:     summary,
		Payload:     payload,
		TaskID:      claimTaskID(name),
		Attachments: attachments,
	})
	if err != nil {
		t.Fatalf("TaskSubmit(%q): %v", name, err)
	}
	return extractTaskID(t, out), out
}

// TestAttachmentClaimMarkerFiresOnAssertingProse is acceptance (a): the row has
// NO manifest and its payload asserts an attachment → `task show` prints the
// explicit marker. RED before the change (show printed nothing at all here).
func TestAttachmentClaimMarkerFiresOnAssertingProse(t *testing.T) {
	claimTestEnv(t)
	id, _ := claimSubmitRow(t, "asserting prose", "claim fires",
		"the failure screenshot is attached to this task — see the attachment", nil)

	show, err := TaskShow(id)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, TaskAttachmentClaimNoneLabel) {
		t.Fatalf("task show lacks the false-claim marker %q:\n%s", TaskAttachmentClaimNoneLabel, show)
	}
	// The marker must NAME the phrase it matched — a bare "NONE" would leave
	// the reader unable to tell which words tripped it.
	if !strings.Contains(show, attachClaimPhraseAttachedToThisTask) {
		t.Errorf("marker does not quote the matched phrase %q:\n%s", attachClaimPhraseAttachedToThisTask, show)
	}
	if !strings.Contains(show, TaskAttachmentClaimNoneTail) {
		t.Errorf("marker lacks the tail sentence %q:\n%s", TaskAttachmentClaimNoneTail, show)
	}
	// It must NOT claim an archived attachment exists (the whole point).
	if strings.Contains(show, "attachment1:") {
		t.Errorf("marker row must carry no [attachmentN:] line:\n%s", show)
	}
}

// TestAttachmentClaimMarkerFiresOnTheMeasuredDefectPayload replays the row that
// motivated the epic: task 01a0b535's payload said the image was attached to the
// originating session, and the row carried no manifest.
func TestAttachmentClaimMarkerFiresOnTheMeasuredDefectPayload(t *testing.T) {
	claimTestEnv(t)
	id, _ := claimSubmitRow(t, "originating session", "measured defect replayed",
		"the screenshot is attached to the originating session", nil)

	show, err := TaskShow(id)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, TaskAttachmentClaimNoneLabel) ||
		!strings.Contains(show, attachClaimPhraseAttachedToOriginating) {
		t.Fatalf("the measured defect's payload must trip the marker:\n%s", show)
	}
}

// TestAttachmentClaimMarkerQuietOnCleanProse is acceptance (b): no manifest, no
// asserting phrase → no marker. Attachment-less rows stay legal and silent.
func TestAttachmentClaimMarkerQuietOnCleanProse(t *testing.T) {
	claimTestEnv(t)
	id, _ := claimSubmitRow(t, "clean prose", "claim quiet",
		"run go test ./internal/tools and report the failing package", nil)

	show, err := TaskShow(id)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.Contains(show, TaskAttachmentClaimNoneLabel) {
		t.Errorf("clean prose must not trip the false-claim marker:\n%s", show)
	}
}

// TestAttachmentClaimMarkerQuietOnPathAndFeatureProse is acceptance (c) — the
// false-positive guard. These payloads either NAME files (the ordinary case: a
// run writes a log, a report cites a source) or DISCUSS the attachments feature
// without claiming evidence. Firing here would make the marker useless on real
// payloads, which is why the predicate has no bare-article entry.
func TestAttachmentClaimMarkerQuietOnPathAndFeatureProse(t *testing.T) {
	claimTestEnv(t)
	payloads := []struct {
		name    string
		payload string
	}{
		{"absolute log path", "the run log lives at /home/user/workspace/gopi-x/run.log — read it after the run"},
		{"workspace scratch dir", "evidence under /home/user/workspace/_tmp/task-01a0b53b/, keep the dir"},
		{"relative path citation", "see devdoc/attachment-claim-marker.md for the decision record"},
		{"log mention", "the bash tool writes its stdout to the session log; check stderr for the trace"},
		{"attachments feature discussion", "the attachments feature copies files into the session dir when --attachment is passed"},
		{"attachment as a noun with a qualifier", "attachment copy happens before the row insert"},
		{"embedded in a longer word", "the reattached driver was attached again after the preattached-check failed"},
	}
	for _, tc := range payloads {
		t.Run(tc.name, func(t *testing.T) {
			id, _ := claimSubmitRow(t, tc.name, "claim path "+tc.name, tc.payload, nil)
			show, err := TaskShow(id)
			if err != nil {
				t.Fatalf("TaskShow: %v", err)
			}
			if strings.Contains(show, TaskAttachmentClaimNoneLabel) {
				t.Errorf("payload merely quoting a path/discussing the feature tripped the marker:\n%s\n--- payload:\n%s", show, tc.payload)
			}
		})
	}
}

// TestAttachmentClaimMarkerAbsentWhenManifestPresent is acceptance (d): a row
// whose manifest is non-empty renders its [attachmentN:] line + `from:` mirror
// and gets NO marker — the marker would otherwise be a lie (evidence IS
// archived). The assertion is on the exact lines the pre-change renderer
// produced (the byte-identical contract cmd/agent's
// cli_tasks_attach_index_test.go also guards at the briefing face).
func TestAttachmentClaimMarkerAbsentWhenManifestPresent(t *testing.T) {
	home := claimTestEnv(t)
	src := filepath.Join(home, "shot.png")
	content := []byte("png-bytes")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	// The prose ALSO asserts an attachment — with a real manifest the claim is
	// true, so the marker must stay away.
	id, _ := claimSubmitRow(t, "manifest present", "claim manifest",
		"the screenshot is attached to this task", []string{src})

	show, err := TaskShow(id)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.Contains(show, TaskAttachmentClaimNoneLabel) {
		t.Errorf("a row WITH a manifest must not be told NONE:\n%s", show)
	}
	if n := strings.Count(show, "attachment1:"); n != 1 {
		t.Errorf("attachment1 line count = %d, want 1:\n%s", n, show)
	}
	// The `from:` mirror line must be byte-identical to the pre-change
	// renderer's ("attachment%d:    %s\n" / "  from:         %s (sha256 %s, %d bytes)\n").
	atts, _, err := TaskDeliverableAttachments(id)
	if err != nil || len(atts) != 1 {
		t.Fatalf("manifest read back (%d atts, err=%v), want 1", len(atts), err)
	}
	wantFrom := fmt.Sprintf("  from:         %s (sha256 %s, %d bytes)", atts[0].OriginalAbsPath, atts[0].SHA256, atts[0].FileSize)
	if !strings.Contains(show, wantFrom) {
		t.Errorf("attachment entry lost its exact `from:` mirror line %q:\n%s", wantFrom, show)
	}
}

// TestAttachmentClaimSubmitWarning is the write-face acceptance: submitting an
// attachment-asserting payload with NO attachments warns, naming the mismatch,
// and the row still submits (a wording nit must never block a dispatch).
func TestAttachmentClaimSubmitWarning(t *testing.T) {
	claimTestEnv(t)
	id, out := claimSubmitRow(t, "submit warning", "submit warning case",
		"the crash log is attached below", nil)

	if !strings.Contains(out, "warning: the payload asserts an attachment") {
		t.Fatalf("submit confirmation lacks the false-claim warning:\n%s", out)
	}
	if !strings.Contains(out, attachClaimPhraseAttachedBelow) {
		t.Errorf("warning does not name the matched phrase %q:\n%s", attachClaimPhraseAttachedBelow, out)
	}
	// The warning is advisory: the row exists and is dispatchable.
	if id == "" {
		t.Fatalf("row was not created for an asserting payload (must stay dispatchable): %q", out)
	}
	if show, err := TaskShow(id); err != nil || !strings.Contains(show, id) {
		t.Errorf("row %s not readable after the warned submit (err=%v)", id, err)
	}
}

// TestAttachmentClaimSubmitWarningAbsentWhenArchived: the warning must NOT fire
// when the submitter DID archive the evidence, and must not fire on prose that
// makes no claim.
func TestAttachmentClaimSubmitWarningAbsentWhenArchived(t *testing.T) {
	home := claimTestEnv(t)
	src := filepath.Join(home, "trace.log")
	if err := os.WriteFile(src, []byte("trace"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, withAtt := claimSubmitRow(t, "archived claim", "archived claim",
		"the trace is attached to this task", []string{src})
	if strings.Contains(withAtt, "warning: the payload asserts an attachment") {
		t.Errorf("warning fired although the evidence IS archived:\n%s", withAtt)
	}
	_, clean := claimSubmitRow(t, "clean claim", "clean claim",
		"the trace lives at "+src, nil)
	if strings.Contains(clean, "warning: the payload asserts an attachment") {
		t.Errorf("warning fired on prose that names a path only:\n%s", clean)
	}
}

// TestAttachmentClaimScanTable pins the predicate: which phrases match, which do
// not, and that the reported phrase is the most specific one.
func TestAttachmentClaimScanTable(t *testing.T) {
	cases := []struct {
		name    string
		summary string
		payload string
		want    string
		fire    bool
	}{
		{name: "screenshot attached", payload: "Screenshot attached.", want: attachClaimPhraseScreenshotAttached, fire: true},
		{name: "attached screenshots wins over the singular", payload: "attached screenshots of the crash", want: attachClaimPhraseAttachedScreenshots, fire: true},
		{name: "see the attachment", payload: "see the attachment for the trace", want: attachClaimPhraseSeeTheAttachment, fire: true},
		{name: "as an attachment", payload: "the image went out as an attachment", want: attachClaimPhraseAsAnAttachment, fire: true},
		{name: "longest of two overlapping", payload: "see the attachment list", want: attachClaimPhraseSeeTheAttachmentList, fire: true},
		{name: "case insensitive", payload: "SCREENSHOT ATTACHED", want: attachClaimPhraseScreenshotAttached, fire: true},
		{name: "summary is scanned too", summary: "verify the fix (log attached)", want: attachClaimPhraseLogAttached, fire: true},
		{name: "payload wins over summary", summary: "log attached", payload: "screenshot attached", want: attachClaimPhraseScreenshotAttached, fire: true},
		{name: "punctuation boundary", payload: "evidence attached — see below", want: attachClaimPhraseEvidenceAttached, fire: true},
		{name: "CJK adjacent still matches", payload: "见 attached file", want: attachClaimPhraseAttachedFile, fire: true},
		{name: "the measured defect payload", payload: "attached to the originating session", want: attachClaimPhraseAttachedToOriginating, fire: true},
		{name: "path only", payload: "/home/user/work/GoPi/devdoc/x.md", fire: false},
		{name: "reattached is not attached", payload: "the driver was reattached", fire: false},
		{name: "preattached-check", payload: "the preattached-check passed", fire: false},
		{name: "feature prose", payload: "the attachments area holds the replicas", fire: false},
		{name: "bare article is not a claim", payload: "the attachment schema is a JSON array", fire: false},
		{name: "empty prose", fire: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, fired := TaskAttachmentClaimScan(tc.summary, tc.payload)
			if fired != tc.fire {
				t.Fatalf("fire = %v, want %v (phrase %q) for payload %q", fired, tc.fire, got, tc.payload)
			}
			if fired && got != tc.want {
				t.Errorf("phrase = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAttachmentClaimPhraseTable pins the match set's invariants: no duplicates,
// every entry lower-case (the haystack is ASCII-lowered), and the ORDER the scan
// relies on — longest first, so the reported phrase is the specific one.
func TestAttachmentClaimPhraseTable(t *testing.T) {
	if len(taskAttachmentClaimPhrases) < 40 {
		t.Fatalf("phrase table has %d entries — the declared constant set is larger", len(taskAttachmentClaimPhrases))
	}
	if got := taskAttachmentClaimSortPhrases(taskAttachmentClaimPhrases); !claimPhrasesEqual(got, taskAttachmentClaimPhrases) {
		t.Errorf("phrase table is not in the longest-first order the scan relies on:\n got %v\nwant %v", taskAttachmentClaimPhrases, got)
	}
	seen := make(map[string]bool, len(taskAttachmentClaimPhrases))
	for i, p := range taskAttachmentClaimPhrases {
		if strings.TrimSpace(p) == "" {
			t.Fatalf("phrase %d is empty", i)
		}
		if p != strings.ToLower(p) {
			t.Errorf("phrase %q must be lower-case (the scan folds ASCII-lower)", p)
		}
		if seen[p] {
			t.Errorf("duplicate phrase %q", p)
		}
		seen[p] = true
	}
	// Word-boundary self-check: no phrase may be a claim only when embedded.
	for _, p := range taskAttachmentClaimPhrases {
		if taskAttachmentClaimMatch("x"+p, p) {
			t.Errorf("phrase %q matched embedded in a longer word — the boundary rule is broken", p)
		}
	}
}

func claimPhrasesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestAttachmentClaimGate_LiteralsLiveOnlyAtTheirDefinition is the Rule 13
// anti-regression gate: the marker strings may appear ONLY in
// task_attachment_claim.go, so a future re-inline (a second copy of the marker
// text inside a renderer) fails here instead of drifting from the constant.
func TestAttachmentClaimGate_LiteralsLiveOnlyAtTheirDefinition(t *testing.T) {
	const definitionFile = "task_attachment_claim.go"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	needles := []string{
		"[Attachments] NONE — but the payload references an attachment",
		"No manifest, no replica files: the referenced evidence is NOT archived with this task.",
		"warning: the payload asserts an attachment (%q)",
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == definitionFile {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		for _, n := range needles {
			if strings.Contains(string(data), n) {
				t.Errorf("%s carries the attachment-claim literal %q — reference the constant in %s instead (Rule 13)",
					name, n, definitionFile)
			}
		}
	}
	// Read-floor (the vacuous-pass guard): a broken glob must not make this
	// gate green by scanning nothing.
	if scanned < 50 {
		t.Fatalf("scanned only %d production files — the gate's glob is broken, refusing a vacuous pass", scanned)
	}
	// Presence half: the gate must find the definition site itself, so deleting
	// the constants cannot satisfy the absence check above.
	def, err := os.ReadFile(definitionFile)
	if err != nil {
		t.Fatalf("read definition file: %v", err)
	}
	for _, n := range needles {
		if !strings.Contains(string(def), n) {
			t.Errorf("%s no longer defines %q — the gate cannot be satisfied by deletion", definitionFile, n)
		}
	}
}
