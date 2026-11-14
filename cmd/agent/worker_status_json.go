package main

// worker_status_json.go — the MACHINE-QUERYABLE half of `gopi worker --status`
// (epic worker-status-task-field, 2026-10-23).
//
// WHY A SECOND FORM EXISTS AT ALL. The human report answers "what is running?
// read it" — one sentence per worker, grouped in plain language. That shape is
// deliberately bad at the OTHER question the operator asked: "which task is
// each pid running, as a VALUE I can query?" A human sentence has to be
// re-parsed to answer it (that was the whole defect: the id lived inside an
// arbitrary command string), so the read grows a `--json` spelling that emits
// the SAME enumeration as ONE document whose fields are the vocabulary of the
// question:
//
//	gopi worker --status --json | jq -r '.workers[] | "\(.pid)\t\(.task)"'
//
// THE RULE THAT KEEPS THE TWO FORMS FROM DRIFTING (Rules 2/13): the JSON form
// derives every fact through the SAME functions the text form uses — the one
// enumeration (workerStatusEnumerate), the one task derivation
// (workerStatusWorker.Task / workerTaskOfPID), the one state-word decision
// (workerStateField). There is no second scan and no second grammar, so a
// worker cannot be described one way here and another way above.
//
// IT IS STILL READ-ONLY (the face's contract, pinned by
// status_action_conflict_e2e_test.go): encoding a value in memory and printing
// it resolves nothing and writes nothing — the same ControlDirReadOnly/
// workerStatusEnumerate pair the text form uses.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// workerStatusJSONFlag is the machine-readable spelling of the status read.
	// It is a READ-ONLY modifier of the same enumeration (never a second
	// action), so it is routed like workerStatusResidueVerboseFlag and
	// classified read-only like it — an unclassified alias would make the
	// read+act refusal blind (the F1 defect, see workerControlRoleOf).
	workerStatusJSONFlag = "--json"

	// workerStatusJSONShape* are the three worker shapes as the machine form
	// spells them. They are the same three the human report distinguishes, and
	// they are STABLE WIRE VALUES: a consumer switches on them, so they are
	// constants (Rule 1b), never literals at the render site.
	workerStatusJSONShapeStateRecord  = "state-record"
	workerStatusJSONShapeArgvFallback = "argv-fallback"
	workerStatusJSONShapeForeground   = "foreground"

	// workerStatusJSONMarshalFailFmt reports the one failure this form can hit
	// (Rule 15b: encoding a value CAN fail — a NaN in a float field, a channel
	// someone adds later — and a silent empty document would read as "no
	// workers"). %s = program name, %v = the error.
	workerStatusJSONMarshalFailFmt = "%s: could not encode the worker status report: %v\n"
)

// workerStatusJSONWorker is ONE worker in the machine form. Every field is
// always present (no omitempty on the facts a consumer needs): a missing key
// reads as "this version has no such field", which is a DIFFERENT lie from
// "the worker has no task" — the latter is spelled by task_recorded=false and
// task="(not recorded)" (Rule 15a).
type workerStatusJSONWorker struct {
	PID    int    `json:"pid"`
	Shape  string `json:"shape"`
	State  string `json:"state"`
	Task   string `json:"task"`
	Source string `json:"task_source"`
	// TaskRecorded is the boolean a consumer SHOULD switch on: comparing a
	// string against the not-recorded marker is a coupling to prose.
	TaskRecorded bool `json:"task_recorded"`
	// Command is the pid's own command line (the text form's description of a
	// shape with no record) — "" when /proc would not answer.
	Command string `json:"command,omitempty"`
	// Image / ImageRevision / ImageDiskRevision are WHICH IMAGE the pid runs and
	// the two stamps that decide it (internal/tools/image_peer.go). They are a
	// first-class fact of a worker entry — not a debugging extra — because the
	// sentences a worker prints are produced by that image: a worker running an
	// older binary reported an ALREADY-DELIVERED row as "delivery did not land"
	// (row 01a18301), and a consumer had no field to notice it with.
	//
	// Image is one of tools.PeerImageState* (current / replaced /
	// rebuilt-same-revision / older-than-disk / not-measurable); a consumer
	// switches on that token, never
	// on the human sentence. No omitempty: "the probe could not run" is a state,
	// and omitting it would read as "this version has no such field" (Rule 15a).
	Image             string `json:"image"`
	ImageRevision     string `json:"image_revision"`
	ImageDiskRevision string `json:"image_disk_revision"`
	// StateFile is the record this entry was read from, "" for the shapes that
	// own none. It is what tells an operator WHERE the task value came from on
	// disk, which is the audit trail for the source="state" rows.
	StateFile string `json:"state_file,omitempty"`
}

// workerStatusJSONForeign is ONE other-profile worker, in the machine form: the
// pid and the profile its own environment named. It is a SEPARATE list because
// it answers a different question (whose fleet is this?) and must never be
// summed into this profile's workers.
type workerStatusJSONForeign struct {
	PID     int    `json:"pid"`
	Profile string `json:"profile"`
}

// workerStatusJSONDoc is the whole document: the three worker shapes in ONE
// list (that is the point — a consumer iterates workers, not shapes), the
// other profile's fleet, and the residue COUNT.
//
// The residue is counted, not listed: a residue file is not a worker (it is a
// state file of a pid that is gone or recycled), so putting it in `workers`
// would rebuild the miscount this face exists to prevent, and dropping it
// entirely would hide a fact the text form prints. The count keeps the JSON
// form honest without inviting a consumer to treat corpses as a fleet.
type workerStatusJSONDoc struct {
	Program        string                    `json:"program"`
	Profile        string                    `json:"profile,omitempty"`
	StateDir       string                    `json:"state_dir"`
	Workers        []workerStatusJSONWorker  `json:"workers"`
	Foreign        []workerStatusJSONForeign `json:"foreign"`
	ResidueCount   int                       `json:"residue_count"`
	ResidueVerbose bool                      `json:"residue_verbose"`
}

// workerStatusTaskOfWorker is the ONE task derivation for a worker entry of
// either non-foreground shape.
func workerStatusTaskOfWorker(w workerStatusWorker) workerTask { return w.Task() }

// workerStatusJSONWorkers folds the scan into the machine list, in pid order
// across ALL three shapes — the human report prints the foreground block after
// the record block, but a machine consumer asking "which pid runs what" wants
// one ordered list (and the `shape` field keeps the distinction visible).
func workerStatusJSONWorkers(scan workerStatusScan) []workerStatusJSONWorker {
	out := make([]workerStatusJSONWorker, 0, len(scan.Workers)+len(scan.Foreground))
	for _, w := range scan.Workers {
		t := workerStatusTaskOfWorker(w)
		shape := workerStatusJSONShapeStateRecord
		if w.Fallback {
			shape = workerStatusJSONShapeArgvFallback
		}
		cmd := ""
		if w.Fallback {
			cmd = w.Command
		}
		img := tools.PeerImageProbeAt(procnameRoot(), w.PID)
		out = append(out, workerStatusJSONWorker{
			PID: w.PID, Shape: shape,
			State: workerStateField(w.Record, w.Fallback),
			Task:  t.ID, Source: t.sourceWord(), TaskRecorded: t.Recorded(),
			Command: cmd, StateFile: w.StatePath,
			Image: img.StateWord(), ImageRevision: img.ImageRevision,
			ImageDiskRevision: img.DiskRevision,
		})
	}
	for _, pid := range scan.Foreground {
		t := workerTaskOfPID(pid)
		img := tools.PeerImageProbeAt(procnameRoot(), pid)
		out = append(out, workerStatusJSONWorker{
			PID: pid, Shape: workerStatusJSONShapeForeground,
			State: workerStatusStateUnknown,
			Task:  t.ID, Source: t.sourceWord(), TaskRecorded: t.Recorded(),
			Command: workerCommandLineOrUnknown(pid),
			Image:   img.StateWord(), ImageRevision: img.ImageRevision,
			ImageDiskRevision: img.DiskRevision,
		})
	}
	// ONE order for the whole list (pid ascending), so two runs over the same
	// fleet diff cleanly — the human report's GROUPING (records, then foreground,
	// then foreign) is a reading aid, not a contract a consumer should inherit.
	// The sort is the stdlib's (Rule: reuse, never hand-roll a second one).
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// workerStatusJSON builds the document for one scan. It is a pure function of
// the scan (no I/O beyond the /proc reads the task derivation performs), so the
// text and machine forms are two renderings of ONE measurement.
func workerStatusJSON(scan workerStatusScan, dir string, verbose bool) workerStatusJSONDoc {
	doc := workerStatusJSONDoc{
		Program:        workerProgTopLevel,
		Profile:        scan.Profile,
		StateDir:       dir,
		Workers:        workerStatusJSONWorkers(scan),
		Foreign:        make([]workerStatusJSONForeign, 0, len(scan.Foreign)),
		ResidueCount:   len(scan.Residue),
		ResidueVerbose: verbose,
	}
	for _, f := range scan.Foreign {
		doc.Foreign = append(doc.Foreign, workerStatusJSONForeign{PID: f.PID, Profile: f.Profile})
	}
	return doc
}

// workerStatusReportJSON emits the machine form and returns the same exit code
// the text form would: the directory failure is REPORTED on stderr and the
// document is still printed for the half of the enumeration that did run — an
// empty `workers` list must never be the only signal that the record half was
// missing (Rule 15).
func workerStatusReportJSON(dir string, scan workerStatusScan, scanErr error, verbose bool) int {
	doc := workerStatusJSON(scan, dir, verbose)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		fmt.Fprintf(os.Stderr, workerStatusJSONMarshalFailFmt, workerProgTopLevel, err)
		return workerControlExitFailed
	}
	if scanErr != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", workerProgTopLevel, scanErr)
		return workerControlExitFailed
	}
	return workerControlExitOK
}
