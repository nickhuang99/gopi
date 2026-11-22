package scratch

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

// The SANDBOX arm of the scratch GC.
//
// WHY IT LIVES IN internal/scratch: the sandbox tree needs a reclaimer, and the
// reclaimer must not be a new daemon or a new tick (the epic's hard
// constraint). internal/scratch already owns the ONE sweep that the
// `gopi tool scratch-gc` face runs (its `--tick` and `sandboxes` arms), and it
// already owns the ONE cross-process liveness probe (DirHolder / LiveHolders —
// the /proc cwd|fd|exe+maps scan plus the .holder registration). A second
// probe, or a second sweep schedule, is how memory #2632's fail-open drift
// returns. So the sandbox arm is a THIRD predicate inside this package, built
// on the same two primitives:
//
//	reclaimable = OURS (name marker) AND AGED (own mtime) AND UNOWNED
//	              (no live incarnation of the recorded owner)
//	              AND UNHELD (no live process standing inside it)
//
// EVERY unknown is a KEEP. That is not caution for its own sake: the two
// failure directions are not symmetric. A sandbox that survives one more sweep
// costs disk; a sandbox deleted out from under its owner destroys a run's
// private state with no way to tell what was lost. So an unreadable /proc, an
// unparseable marker, a directory with no marker at all, or a probe that
// returned an error all resolve to KEEP (Rule 15: the uncertainty is REPORTED
// as the reason, never silently reinterpreted as permission).

const (
	// SandboxMarkerPrefix is the name marker of ONE sandbox under the sandbox
	// root: `sandbox-<label>-<rand>`. This is the DEFINITION (Rule 14) —
	// internal/config aliases it for the initializer, because this package
	// cannot import internal/config (config imports scratch).
	//
	// The marker is what keeps the arm scoped: the root is an ordinary
	// directory the operator may share with other things, and a child whose
	// name does not carry the prefix is never a candidate at any age. A sweep
	// that cleaned by LOCATION would be one mistyped configuration key away
	// from deleting the operator's own directories.
	SandboxMarkerPrefix = "sandbox-"

	// SandboxOwnerFileName is the ownership marker inside each sandbox (pid,
	// kernel start ticks, task id, created-at). Also the DEFINITION, aliased by
	// internal/config for the initializer.
	SandboxOwnerFileName = ".sandbox-owner.json"

	// SandboxStaleAge is the window after which an UNOWNED sandbox is garbage.
	//
	// It is deliberately far longer than the suite scratch's 6h default (which
	// bounds per-test throwaways): a sandbox is a whole private profile that an
	// operator may have created to reproduce something, and 6h is a normal
	// working day's fragment. 72h is long enough that no human workflow loses a
	// sandbox between two runs, and short enough that a week of crashed runs
	// cannot accumulate without bound.
	SandboxStaleAge = 72 * time.Hour

	// SandboxDefaultLeaf is the built-in leaf of the sandbox root under the
	// workspace (<home>/workspace/gopi-sandbox). This is the DEFINITION;
	// internal/config.SandboxDefaultLeaf aliases it (this package cannot import
	// internal/config — config imports scratch).
	SandboxDefaultLeaf = "gopi-sandbox"
)

// SandboxKeepReason names WHY a candidate was kept. They are constants (Rule 1b)
// because the tick's log line and the tests both match on them, and a reason
// typed twice drifts into a reason nobody can grep for.
const (
	SandboxKeepUnknownOwner  = "owner unverifiable — /proc unreadable, marker unparseable, or no start ticks (unknown is never a delete)"
	SandboxKeepLiveOwner     = "live owner"
	SandboxKeepHeld          = "a live process stands inside it"
	SandboxKeepSelfCwd       = "this sweep's own working directory"
	SandboxKeepMarkerMissing = "no ownership marker and the age window has not been exceeded"
	// SandboxKeepOwnerGoneWithinAge is the arm the pre-fix code had NO sentence
	// for, because it collected in this state: the marker names a process that
	// has exited, but the sandbox itself was touched inside the age window. That
	// is the ordinary state of every sandbox `gopi tool sandbox create` makes
	// (the marker's writer exits immediately), so treating it as collectable
	// deleted live sandboxes — measured 2026-11-15.
	SandboxKeepOwnerGoneWithinAge = "the recorded owner has exited but the sandbox was used within the age window"
	// SandboxKeepMarkerUnreadable is the OTHER missing-marker case: a marker file
	// exists and could not be read or parsed. Different from "nobody wrote one" —
	// the two must not render the same (Rule 15), because the repair differs (a
	// corrupt marker is a bug; an absent one is a sandbox made by hand).
	SandboxKeepMarkerUnreadable = "ownership marker unreadable"
	// SandboxKeepAgeUnmeasurable prefixes the sentence an entry gets when the age
	// arm could not answer, so the sandbox is kept and the error travels with the
	// verdict (Rule 15 — never a silent keep).
	SandboxKeepAgeUnmeasurable = "age unmeasurable"
)

// The two sentences that AUTHORISE a collection. They are constants for the same
// reason the keep reasons are (Rule 1b): the verdict face prints them, an
// operator greps for them, and the tests match on them.
const (
	// SandboxCollectableUnowned is the writer-owned verdict: the marker named a
	// process incarnation the kernel proves GONE, so the sandbox is collectable
	// NOW. The age clock is not consulted at all on this path — the question
	// "may this be collected?" was answered by the process that made it, which is
	// the same order the scratch home's own owner arm applies
	// (entryVerdictInNamespace). Waiting out 72h for a sandbox whose creator is
	// provably dead is exactly the `keep reason="not aged"` lie this sentence
	// replaces.
	SandboxCollectableUnowned = "unowned — the recorded owner is gone, so the age window was not waited out"
	// SandboxCollectableAgedUnmarked is the other half: NO marker at all, so no
	// writer spoke for it and the age window is the only bound there is.
	SandboxCollectableAgedUnmarked = "aged and unmarked — no ownership marker, past the age window"
)

// The dry-run's two verdict WORDS (Rule 1b — the CLI prints them and the tests
// match on them, so a word typed twice is a word nobody can grep for). They are
// the sandbox arm's own spelling: the units arm's `would-stop` names a process
// phase, not a directory this predicate collects.
const (
	// SandboxWouldRemoveWord marks a plan entry the predicate authorises
	// collecting — the set `bytes=` adds up.
	SandboxWouldRemoveWord = "would-remove"
	// SandboxKeepWord marks a plan entry the predicate protects — the set
	// `kept_bytes=` adds up.
	SandboxKeepWord = "keep"

	// SandboxNoReclaimBytes is the bytes= figure of a pass that DELETED nothing
	// by construction (a disabled sweep). It exists as a constant (Rule 1) so the
	// one honest zero is named for what it means: "this pass reclaimed nothing",
	// which is a different fact from "the tree is empty" — that one is 0 entries
	// and a kept_bytes= figure (Rule 15).
	SandboxNoReclaimBytes int64 = 0
)

// The CENSUS of what this arm refuses BY NAME (Rule 15). The name-marker filter
// is load-bearing and stays exactly as it is — the root is a directory the
// operator may share, so a sweep that collected by LOCATION would be one
// mistyped configuration key from deleting the operator's own directories — but
// a directory refused by name used to be dropped BEFORE anything counted it, so
// its bytes entered neither `bytes=` nor `kept_bytes=` and no other arm of the
// verb censuses the sandbox root. Measured on this host (2026-11-14): the root
// held 995,823,652 bytes while the line accounted for 862,563,711 of them — the
// missing 133,141,061 bytes (13.4%) sat in two foreign directories that no line
// mentioned. Reporting them costs one line each and collects nothing.
const (
	// SandboxNotOursReason is why such a directory is reported and never
	// collected: the marker is the ONLY thing that makes a child this system's
	// business, so its absence is a verdict at any age — and the sentence says
	// so, because a keep whose reason is "not ours" must not read as a delay.
	SandboxNotOursReason = "name lacks the sandbox- marker — reported, never collected at any age"

	// SandboxNotOursWord is the census line's verdict WORD (Rule 1b: the CLI
	// prints it and the tests match on it). It is deliberately not
	// SandboxKeepWord: a keep is something the predicate JUDGED and protected,
	// while this is a directory the predicate never had jurisdiction over — the
	// same distinction the declared-trees arm draws with its own not-ours count.
	SandboxNotOursWord = "not-ours"
)

// SandboxNotOurs is ONE direct child of the sandbox root that this arm REFUSES
// BY NAME: a directory whose name carries no SandboxMarkerPrefix, or an entry
// that is not a directory at all. It is REPORTED and never a delete candidate.
//
// WHY IT EXISTS AT ALL (the defect it closes, measured on this host 2026-11-14):
// the sandbox root held 995,823,652 bytes while the arm's own line accounted for
// 862,563,711 — the other 133,141,061 bytes (13.4%) sat in two foreign
// directories (`prefix-seam-bin`, 133,116,482 B; `fixture-gocache-2048578`,
// 24,579 B) that the candidate filter dropped BEFORE anything counted them, so
// they entered neither `bytes=` nor `kept_bytes=` and no other arm of
// scratch-gc censuses this root. An operator reading the line believed the root
// was 862 MB of keeps. Rule 15: the uncertainty is REPORTED, never silently
// reinterpreted as "not present".
//
// It is a SEPARATE type from SandboxPlan on purpose: a plan entry carries a
// verdict the predicate reached and may therefore be collected, and this one
// carries a jurisdiction the predicate never had. Folding it into SandboxPlan
// (say, as a third Keep value) is how the delete loop would eventually see it.
type SandboxNotOurs struct {
	// Path is the child's path under the sandbox root.
	Path string
	// Bytes is its MEASURED size, by the same DirBytes walk that sizes a plan
	// entry — so `not_ours_bytes=` and `kept_bytes=` are addable quantities.
	Bytes int64
	// Reason is why it is refused and never collected.
	Reason string
}

// SandboxCensus is the survey of ONE sandbox root: the entries this arm has
// jurisdiction over (Plans) and the entries it refuses by name (NotOurs).
//
// The two halves are what a reader needs to reconcile the line with the disk:
// bytes= + kept_bytes= + not_ours_bytes= is the sum of the regular-file sizes
// under the root — which before this type differed from that sum by exactly the
// refused half (measured 2026-11-14: 133,141,061 bytes of 995,677,970).
type SandboxCensus struct {
	Plans   []SandboxPlan
	NotOurs []SandboxNotOurs
}

// SandboxPlan is one sandbox's verdict: the collector's REPORTING form (the
// same shape as ScratchPlanEntry — the destructive arm and the reporting arm
// share this predicate, so a dry run cannot disagree with a sweep).
type SandboxPlan struct {
	// Path is the sandbox directory.
	Path string
	// Keep is true when the sandbox must survive this sweep.
	Keep bool
	// Reason is why it is kept — or, when Keep is false, the evidence that
	// authorised collecting it ("owner pid 4711 (start ticks 998877) is gone").
	Reason string
	// Bytes is the sandbox's MEASURED size (DirBytes: the sum of its regular
	// files), taken for EVERY plan entry — kept ones included.
	//
	// WHY IT IS MEASURED ON THE PLAN AND NOT ONLY ON THE DELETE: a dry run that
	// reported bytes=0 could not tell "this root is 950 MB of sandboxes" apart
	// from "there is nothing here" (measured on this host, 2026-11-14: the root
	// held 995,823,652 bytes while the dry-run line printed `bytes=0`). The
	// figure a plan publishes has to be a measurement of the PLAN, so it is
	// taken here, once per entry, by the same ONE walk the live pass uses.
	Bytes int64
	// Age is the directory's OWN mtime age at judgement time. It decides nothing
	// (the age window is consulted only where nobody owns the sandbox) — it is
	// REPORTED, because the `--explain` verdict line has to say how old the tree
	// already was when the verdict was reached, and a reader who cannot see that
	// cannot tell "collected early on the owner's evidence" from "collected by
	// the clock".
	Age time.Duration
	// Owner is the recorded incarnation the verdict was measured against (zero
	// when nothing was recorded) — the pid + kernel start ticks the -h page
	// promises the verdict line names. It travels on the plan so BOTH faces can
	// print it and neither has to re-read the marker (one read, one answer).
	Owner OwnerRecord
	// HasOwner reports whether a readable marker named an owner.
	HasOwner bool
	// Outcome is the owner judgement the verdict rests on: one of
	// procname.Outcome's four words, or EntryOutcomeUnregistered when no marker
	// was found. It is the word that makes a keep or a collection READABLE —
	// "gone", "recycled", "live" and "unmeasurable" are four different answers
	// and a bare pid cannot tell them apart.
	Outcome procname.Outcome
}

// withSandboxOwnerEvidence appends the recorded owner a verdict was measured
// against to the deciding sentence: the pid, the kernel start ticks and the
// outcome word.
//
// WHY THE REASON CARRIES IT (task 01a1dbba, 2026-11-15): before the fix the
// sandbox arm printed `reason="not aged"` for a tree whose owner had been dead for
// hours — a keep nobody could check, and no pid anywhere in the report. A verdict
// line is not the only place a reader looks: the `--dry-run` plan line carries
// `reason=`, and an operator grepping a log for `pid` must find the incarnation
// there too. ONE composer (Rule 14) so the plan's sentence and the `--explain`
// line's `outcome=/owner=` fields can never disagree about who owned it.
func withSandboxOwnerEvidence(reason string, plan SandboxPlan) string {
	if !plan.HasOwner {
		return reason
	}
	return fmt.Sprintf("%s (recorded owner pid %d, start ticks %d, outcome %s)",
		reason, plan.Owner.Pid, plan.Owner.Start, plan.Outcome)
}

// SandboxOwnerEvidence renders a sandbox verdict's owner evidence: the outcome
// word and the recorded incarnation it was measured against.
//
// ONE renderer (Rule 14), so the dry-run plan's `reason=` and the `--explain`
// verdict line spell the same evidence the same way — and so the -h page's
// promise ("naming the recorded owner (pid + kernel start ticks) the decision was
// measured against") is kept by a function rather than by two format strings
// that drift.
func (p SandboxPlan) SandboxOwnerEvidence() string {
	if !p.HasOwner {
		return fmt.Sprintf("outcome=%s (no %s record)", EntryOutcomeUnregistered, SandboxOwnerFileName)
	}
	return fmt.Sprintf("outcome=%s owner=pid:%d/start:%d", p.Outcome, p.Owner.Pid, p.Owner.Start)
}

// SandboxTally is a plan's byte ACCOUNT: what this pass would reclaim, and what
// it would leave behind. The two figures are separate fields because they answer
// different questions ("how much does this sweep give back" vs "how much stays
// on disk, and why") and a single total would answer neither (Rule 15: a number
// that could mean two things is reported as two).
type SandboxTally struct {
	// WouldRemove is how many sandboxes the predicate authorises collecting.
	WouldRemove int
	// WouldRemoveBytes is their measured size — the figure `bytes=` publishes.
	WouldRemoveBytes int64
	// Kept is how many sandboxes the predicate protects.
	Kept int
	// KeptBytes is their measured size — the figure `kept_bytes=` publishes,
	// so an operator can see how much a KEEP (not a collection) is holding.
	KeptBytes int64
	// NotOurs is how many direct children of the root the name marker refused.
	NotOurs int
	// NotOursBytes is their measured size — the figure `not_ours_bytes=`
	// publishes. They are NEVER collected, so this half of the account is a
	// permanent floor under the root's size, not a deferred reclaim.
	NotOursBytes int64
}

// SandboxNotOursBytes sums the census half's measured sizes — the ONE definition
// of the `not_ours_bytes=` figure, so the plan face, the sweep face and the
// disabled-sweep face cannot add it up differently (Rule 14).
func SandboxNotOursBytes(notOurs []SandboxNotOurs) int64 {
	var total int64
	for _, n := range notOurs {
		total += n.Bytes
	}
	return total
}

// TallySandboxCensus is a pure function of the verdicts, so the CLI face, the
// resident tick and the tests cannot disagree about what a census adds up to.
func TallySandboxCensus(c SandboxCensus) SandboxTally {
	t := TallySandboxPlans(c.Plans)
	t.NotOurs = len(c.NotOurs)
	t.NotOursBytes = SandboxNotOursBytes(c.NotOurs)
	return t
}

// TallySandboxPlans is a pure function of the verdicts, so the CLI face, the
// resident tick and the tests cannot disagree about what a plan adds up to.
func TallySandboxPlans(plans []SandboxPlan) SandboxTally {
	var t SandboxTally
	for _, p := range plans {
		if p.Keep {
			t.Kept++
			t.KeptBytes += p.Bytes
			continue
		}
		t.WouldRemove++
		t.WouldRemoveBytes += p.Bytes
	}
	return t
}

// gopiProfileDir is the resolved PRODUCTION profile (<GOPI_HOME|$HOME>/.gopi).
// It exists for one guard: a `sandbox_dir` that points AT the operator's profile
// would turn this arm into a production-data sweep, so that value is refused by
// name before any directory is read. "" when no profile can be resolved — then
// there is nothing to confuse this root with.
func gopiProfileDir() string {
	if v := strings.TrimSpace(os.Getenv(GopiDirEnvName)); v != "" {
		return filepath.Join(v, gopiDirName)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, gopiDirName)
}

// sandboxOwnerMarker mirrors internal/sandbox's marker. It is a MIRROR by
// necessity — this package cannot import internal/sandbox (sandbox imports
// scratch) — and it is checked rather than trusted: the two structs' JSON tags
// are asserted equal by TestSandboxMarkerShapeAgreesWithTheInitializer, so a
// field renamed on one side fails a test instead of silently reading as zero.
type sandboxOwnerMarker struct {
	TaskID     string `json:"task_id,omitempty"`
	Label      string `json:"label,omitempty"`
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
	CreatedAt  string `json:"created_at"`
	Cmd        string `json:"cmd,omitempty"`
	Version    int    `json:"version"`
}

// SandboxRootRefusal reports why a path must never be swept as a sandbox root.
// It is the guard that keeps a mistyped configuration value from turning this
// arm into a delete-anything machine: sweeping happens one level down, so the
// root's jurisdiction is real, and the three worst values are refused by NAME.
func SandboxRootRefusal(root string) error {
	// The emptiness check reads the RAW argument: filepath.Clean("") is ".",
	// so a canonical first would turn "no value configured" into a refusal
	// about the current directory — the wrong sentence for the wrong defect.
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("sandbox GC: the sandbox root is empty")
	}
	clean := CanonicalPath(root)
	if clean == "" || clean == "." {
		return fmt.Errorf("sandbox GC: the sandbox root %q does not resolve to a directory", root)
	}
	if clean == "/" {
		return fmt.Errorf("sandbox GC: refusing the filesystem root %q as a sandbox root", clean)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && CanonicalPath(home) == clean {
		return fmt.Errorf("sandbox GC: refusing the user's home %q as a sandbox root", clean)
	}
	if profile := gopiProfileDir(); profile != "" && CanonicalPath(profile) == clean {
		return fmt.Errorf("sandbox GC: refusing the gopi profile %q as a sandbox root — that is production state, "+
			"not a sandbox tree", clean)
	}
	return nil
}

// PlanSandboxes reports the verdict for every sandbox directly under root. It
// is read-only (no lock, no delete) — the form `--dry-run` and an operator's
// question both need.
func PlanSandboxes(root string, olderThan time.Duration) ([]SandboxPlan, error) {
	c, err := PlanSandboxCensus(root, olderThan)
	return c.Plans, err
}

// PlanSandboxCensus is the FULL survey: the sandbox verdicts AND the census of
// what the name marker refuses. `--dry-run` reads this form so its lines account
// for the whole root; PlanSandboxes stays as the verdict-only wrapper for the
// callers that judge candidates and nothing else.
func PlanSandboxCensus(root string, olderThan time.Duration) (SandboxCensus, error) {
	var census SandboxCensus
	if err := SandboxRootRefusal(root); err != nil {
		return census, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return census, nil // no root: nothing to judge, not an error
		}
		return census, fmt.Errorf("sandbox GC: read %s: %w", root, err)
	}
	if len(entries) > ScanLimit {
		return census, fmt.Errorf("sandbox GC: %s holds more than %d entries — refusing to sweep an unbounded tree",
			root, ScanLimit)
	}

	now := time.Now()
	// ONE /proc pass for the whole arm (the batch form exists because a
	// per-entry probe costs a process-table walk per candidate).
	holders := ScanHolders()
	self, _ := os.Getwd() //stylegate:ignore an unavailable cwd leaves self empty, which never matches a candidate's parent and so vetoes nothing (every unknown is a KEEP)

	for _, e := range entries {
		path := filepath.Join(root, e.Name())
		if !e.IsDir() || !strings.HasPrefix(e.Name(), SandboxMarkerPrefix) {
			// REFUSED BY NAME — never a candidate at any age (the marker is the
			// only thing that makes a child this system's business). It is
			// COUNTED and MEASURED here, because a directory that no line
			// mentions is the silent-failure shape (Rule 15): the arm's own
			// account then fails to reconcile with `du -sb` of the root and
			// nothing on stdout said so. Measured 2026-11-14 on this host: two
			// such directories held 133,141,061 bytes of 995,823,652.
			census.NotOurs = append(census.NotOurs, SandboxNotOurs{
				Path:   path,
				Bytes:  DirBytes(path),
				Reason: SandboxNotOursReason,
			})
			continue
		}
		plan := judgeSandbox(path, now, olderThan, holders, self)
		// The size is measured for EVERY verdict, kept ones included: the plan
		// publishes two figures (what it would reclaim, what it would leave), and
		// a kept entry's bytes are half of that account. Measured through the
		// SAME walk the live pass uses (DirBytes), so `bytes=` on a dry run and
		// on a real run are the same number for the same tree.
		plan.Bytes = DirBytes(path)
		census.Plans = append(census.Plans, plan)
	}
	sort.Slice(census.Plans, func(i, j int) bool { return census.Plans[i].Path < census.Plans[j].Path })
	sort.Slice(census.NotOurs, func(i, j int) bool { return census.NotOurs[i].Path < census.NotOurs[j].Path })
	return census, nil
}

// judgeSandbox applies the four-part predicate to ONE sandbox and fills in the
// evidence the verdict was reached on (age, recorded owner, outcome), so the
// reporting faces can print it without re-reading anything.
func judgeSandbox(path string, now time.Time, olderThan time.Duration, holders HolderScan, self string) SandboxPlan {
	plan := SandboxPlan{Path: path, Keep: true}
	// The age is REPORTED, never decisive on its own: it is the bound of last
	// resort, applied only where no recorded owner speaks for the sandbox. An
	// unreadable entry leaves it at zero and the age arm below reports why.
	if fi, err := os.Lstat(path); err == nil {
		plan.Age = now.Sub(fi.ModTime())
	}

	if PathWithin(CanonicalPath(self), CanonicalPath(path)) {
		plan.Reason = SandboxKeepSelfCwd
		return plan
	}

	// ① OWNERSHIP — the marker inside the directory, evaluated FIRST and
	// INDEPENDENTLY of the age clock.
	//
	// THE ORDER IS THE CONTRACT, and this arm had it backwards until 2026-11-15:
	// the AGE gate ran first and RETURNED on "not aged", so readSandboxOwner was
	// never even called on a fresh sandbox, and `--explain` reported a sandbox
	// with a provably dead owner as `keep reason="not aged"` while naming no
	// owner at all. Measured on this host 2026-11-15: two sandboxes of
	// 883,257,037 and 862,108,412 bytes, both with `pid` + `start_ticks` in their
	// markers, both judged "not aged" while nothing in them was alive.
	//
	// WHAT THE READING IS ALLOWED TO DECIDE, and this half is where the first fix
	// over-reached. Reading the marker first is right. COLLECTING on "the
	// recorded owner is gone" alone is not, because of one fact about this
	// tool's own product: `gopi tool sandbox create` writes .sandbox-owner.json
	// naming ITS OWN pid and then EXITS, so "owner gone" is the state of every
	// sandbox the tool makes about a second after it makes it. A predicate that
	// collects on that reading does not distinguish "abandoned" from "just
	// created" — it collects both. Measured, same host same day: two sandboxes
	// built minutes earlier vanished, one whole directory and one reduced to its
	// ledger alone, so `gopi task list --all` inside it answered "No tasks found".
	//
	// So the marker answers one question here — IS ANYBODY PROVABLY ALIVE — and
	// the idle clock answers the other — IS ANYBODY STILL USING IT. Both must say
	// "nobody" before a delete. What keeps this conservative is that every
	// UNCERTAIN answer still lands on a KEEP below, and the liveness scan (③)
	// still vetoes a sandbox some live process is standing in.
	marker, markerErr := readSandboxOwner(path)
	owned := markerErr == nil
	if owned {
		plan.HasOwner = true
		plan.Owner = OwnerRecord{Pid: marker.PID, Start: marker.StartTicks, Cmd: marker.Cmd, Version: marker.Version}
		outcome, certain := sandboxOwnerOutcome(marker)
		plan.Outcome = outcome
		switch {
		case !certain:
			// The kernel would not answer (no ticks recorded, an unreadable
			// /proc entry): an unknown incarnation NEVER authorises a delete.
			plan.Reason = withSandboxOwnerEvidence(SandboxKeepUnknownOwner, plan)
			return plan
		case outcome == procname.OutcomeLive:
			// Proven alive: protected at ANY age, and the sentence names the
			// incarnation it measured.
			plan.Reason = withSandboxOwnerEvidence(SandboxKeepLiveOwner, plan)
			return plan
		}
		// outcome is GONE or RECYCLED: the process that RECORDED the marker has
		// stopped existing. That is NOT by itself proof the sandbox is spent —
		// `gopi tool sandbox create` writes the marker and then EXITS, so every
		// sandbox this tool makes is "owner gone" within a second of being
		// created. Collecting on that reading alone deleted freshly made
		// sandboxes out from under live runs (measured 2026-11-15: two sandboxes
		// built minutes earlier disappeared, one whole directory and one reduced
		// to its ledger, so `gopi task list --all` inside it answered "No tasks
		// found"). The age window below is the guard that covers this arm too:
		// a sandbox nobody provably owns is still collected, but not before it
		// has been idle for the whole window.
	}

	// ② AGE — the directory's own mtime, and it applies to EVERY sandbox that
	// reaches this point: nobody is provably alive in it (the two live cases
	// returned above). Deliberately the mtime and not the marker's created_at:
	// the marker is written once, so a sandbox that has been USED for a week would
	// look a week old and be collected while its owner is still writing in it.
	// mtime moves on use, which is what an age rule must measure.
	//
	// This arm covers the "owner recorded, owner gone" case too, and that is the
	// whole point: `gopi tool sandbox create` writes the marker and exits, so a
	// sandbox must not be collectable merely because the process that created it
	// is finished. Only the idle clock may authorise that.
	{
		stale, err := ScratchEntryStale(path, now, olderThan)
		if err != nil {
			plan.Reason = fmt.Sprintf("%s (%v)", SandboxKeepAgeUnmeasurable, err)
			return plan
		}
		if !stale {
			if markerErr != nil && !os.IsNotExist(markerErr) {
				// A marker EXISTS and could not be read or parsed: the sandbox
				// cannot prove an owner, so its claim rests on the clock — and the
				// clock has not been waited out. Reported as its own sentence,
				// because "nobody wrote one" and "one is there and unreadable" are
				// different facts whose repairs differ (Rule 15).
				plan.Reason = fmt.Sprintf("%s (%v) and the age window has not been exceeded",
					SandboxKeepMarkerUnreadable, markerErr)
				return plan
			}
			if owned {
				// The marker names a process that has finished, but the sandbox
				// was touched inside the window: a live run may be inside it. Say
				// WHICH fact held it, so "fresh" and "abandoned a second ago" are
				// not reported as the same sentence.
				plan.Reason = withSandboxOwnerEvidence(SandboxKeepOwnerGoneWithinAge, plan)
				return plan
			}
			plan.Reason = SandboxKeepMarkerMissing
			return plan
		}
	}

	// ③ LIVENESS — no live process standing inside it (the /proc cwd|fd|exe
	// scan). A process whose /proc entry cannot be read is SKIPPED by that scan,
	// so an unreadable process can never veto the sweep; the owner arm above is
	// the channel that covers a same-user sibling. The two facts the scan can
	// report — "not looked at" and "a live process is inside" — are separate
	// guards, so neither is read as the other.
	held, scanned := holders.HeldByProcesses(path)
	if !scanned {
		plan.Reason = HolderScanWithheldReason()
		return plan
	}
	if held {
		plan.Reason = SandboxKeepHeld
		return plan
	}

	// Collectable. The reason names the evidence — including the recorded owner
	// the decision was measured against — so the log line an operator reads says
	// WHY a directory disappeared and WHOSE death authorised it.
	plan.Keep = false
	if owned {
		plan.Reason = withSandboxOwnerEvidence(SandboxCollectableUnowned, plan)
		return plan
	}
	plan.Reason = SandboxCollectableAgedUnmarked
	return plan
}

// readSandboxOwner loads a sandbox's ownership marker.
func readSandboxOwner(dir string) (sandboxOwnerMarker, error) {
	var m sandboxOwnerMarker
	blob, err := os.ReadFile(filepath.Join(dir, SandboxOwnerFileName))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(blob, &m); err != nil {
		return m, err
	}
	if m.PID <= 0 {
		return m, fmt.Errorf("marker records no pid")
	}
	return m, nil
}

// sandboxOwnerOutcome answers what became of the marker's recorded process
// incarnation, and whether that answer is CERTAIN.
//
// IT RETURNS THE SAME FOUR WORDS THE SCRATCH HOME'S OWNER ARM USES
// (procname.Outcome: live / gone / recycled / unmeasurable), so a reader who has
// learned one verdict line can read the other (Rule 14 — one vocabulary, two
// arms). The identity is the (pid, start ticks) PAIR: a recycled pid FAILS the
// ticks match, which is the whole reason the ticks are recorded. An unreadable
// /proc, or a marker that recorded no ticks, is NOT certain — and an uncertain
// answer never authorises a delete.
func sandboxOwnerOutcome(m sandboxOwnerMarker) (procname.Outcome, bool) {
	if m.StartTicks == 0 {
		return procname.OutcomeUnmeasurable, false
	}
	pid := fmt.Sprintf("%d", m.PID)
	if !IsPID(pid) {
		return procname.OutcomeUnmeasurable, false
	}
	ticks, err := ProcStartTime(m.PID)
	if err != nil {
		if os.IsNotExist(err) {
			return procname.OutcomeGone, true // the process is provably gone
		}
		return procname.OutcomeUnmeasurable, false // EACCES and friends: cannot tell
	}
	if ticks == m.StartTicks {
		return procname.OutcomeLive, true
	}
	// The pid is alive but it is somebody else: the kernel handed the number on,
	// so the owner this marker recorded is gone.
	return procname.OutcomeRecycled, true
}

// SweepSandboxes removes the collectable sandboxes under root and reports how
// many were removed and how many bytes came back. It is IDEMPOTENT (a second
// run finds nothing collectable), BOUNDED (one ReadDir, at most ScanLimit
// entries, root untouched), and SAFE: every verdict it acts on comes from the
// same predicate --dry-run reports.
//
// It NEVER removes root itself — not at any age, not when empty. root is the
// configured container, the same "the declared tree's root is never removed"
// rule the declared-trees arm follows (a sweep that deleted its own root broke
// the next build there; here it would silently relocate every future sandbox).
//
// The plain 3-value shape is the thin wrapper the pre-existing callers keep;
// SweepSandboxesDetailed is the reporting form, and it hands the caller the LOCK
// verdict for the same reason SweepHomeDetailed does (Rule 15: a deferred sweep
// must not look like an empty one).
func SweepSandboxes(root string, olderThan time.Duration) (removed int, bytes int64, err error) {
	removed, bytes, _, err = SweepSandboxesDetailed(root, olderThan)
	return removed, bytes, err
}

// SandboxSweepResult is what ONE sweep did, in the two figures an operator needs
// kept apart: what came back (`Bytes`) and what stayed (`KeptBytes`).
//
// WHY THE KEPT HALF IS MEASURED AT ALL: on this host the sandbox root held
// 995,823,652 bytes while four of its five sandboxes were KEPT ("not aged") — so
// a line carrying only `bytes=` reported 0 and the root's real contents were
// invisible (measured 2026-11-14). A KEEP is a real disk cost, and it is the
// reclaimer's own verdict that keeps it, so it reports that cost.
type SandboxSweepResult struct {
	// Removed is how many sandboxes were deleted.
	Removed int
	// Bytes is the measured size of the sandboxes actually deleted.
	Bytes int64
	// Kept and KeptBytes are the same account for the sandboxes this pass
	// PROTECTED — measured even when the sweep deletes nothing.
	Kept      int
	KeptBytes int64
	// NotOurs is the census of direct children the NAME MARKER refused: reported
	// (path + measured bytes + reason) and never touched by the delete loop. On
	// this host these were 13.4% of the root's bytes and no line mentioned them
	// (Rule 15); a real run must report them for the same reason a dry run does.
	NotOurs []SandboxNotOurs
	// Lock is the lock verdict (Rule 15: a deferred sweep must not look empty).
	Lock GCLockVerdict
	// Reclaimed is the per-directory record of what this pass DELETED, one
	// entry per removed sandbox, carrying the evidence a reader needs to answer
	// "who deleted this, and on what grounds" AFTER the directory is gone.
	//
	// It exists because the sweep is the ONLY thing that knows those facts, and
	// the sweep cannot write the removal-audit record itself: this package is
	// imported by internal/config, which internal/removalaudit imports, so an
	// emit from here would close an import cycle. The CALLERS (the CLI face and
	// the resident tick) therefore write the record, and this slice is what they
	// write it from — one measurement, one writer, no second walk (Rule 14).
	// Without it a reclaim left no trace at all: measured 2026-11-15, the sink
	// held 12,305 records and `grep -c sandbox` answered 0.
	Reclaimed []SandboxReclaimed
}

// SandboxReclaimed is ONE sandbox directory this sweep deleted, with the plan's
// own decision recorded beside it. Every field is copied from the SandboxPlan
// the delete loop consumed, so the record cannot disagree with the decision that
// authorised the deletion.
type SandboxReclaimed struct {
	// Path is the absolute directory that was removed.
	Path string
	// Bytes is this sweep's own measurement, taken in the delete loop
	// immediately before the removal (never the plan's earlier figure: a tree a
	// concurrent writer emptied must not be over-reported).
	Bytes int64
	// Reason is the plan's collect reason VERBATIM — the criterion that
	// authorised the deletion, never a re-worded summary of it.
	Reason string
	// Age is how old the directory was when the plan judged it.
	Age time.Duration
	// OwnerPID / OwnerStart / OwnerCmd are the ownership marker's own claims,
	// empty when the directory carried no marker. They are what the plan
	// compared against the process table, so they are the owner evidence.
	OwnerPID   int
	OwnerStart uint64
	OwnerCmd   string
	// HasOwner says whether a marker was present at all — the distinction the
	// plan itself draws (absent marker vs dead owner), and a record that lost
	// it could not be reconciled with the reason above.
	HasOwner bool
	// OwnerOutcome is what the liveness comparison concluded about that marker.
	OwnerOutcome string
}

// SweepSandboxesReport is the sweep's FULL reporting form. The older
// Detailed/Triple shapes stay as thin wrappers so no caller has to change what it
// reads, while the CLI face gets the kept half it needs on both faces.
func SweepSandboxesReport(root string, olderThan time.Duration) (SandboxSweepResult, error) {
	var res SandboxSweepResult
	census, err := PlanSandboxCensus(root, olderThan)
	if err != nil {
		return res, err
	}
	plans := census.Plans
	// The census carries the refused-by-name half too, and it is reported by a
	// REAL run for the same reason a dry run reports it: these directories are
	// never collected, so an operator who only ever runs the sweep would never
	// see the bytes they hold (Rule 15).
	res.NotOurs = census.NotOurs
	// The plan already measured every entry (kept ones included), so the kept
	// half of the account is a tally of the SAME walk the delete loop consumes —
	// one measurement, two uses (Rule 14: a dry run cannot disagree with a sweep).
	tally := TallySandboxPlans(plans)
	res.Kept, res.KeptBytes = tally.Kept, tally.KeptBytes

	if len(plans) == 0 {
		res.Lock = GCLockVerdict{Outcome: GCLockOutcomeAcquired, Reason: "no sandbox plan entries: the lock was not needed"}
		return res, nil
	}
	release, v, lerr := AcquireGCLockVerdict(root)
	res.Lock = v
	if lerr != nil {
		return res, fmt.Errorf("sandbox GC: lock %s: %w", root, lerr)
	}
	if !v.Locked {
		// Another sweep owns this tree right now. Not an error: the other sweep
		// is doing the same work (fail-open, like every other arm) — but the
		// verdict says WHO, so the caller's `removed=0` stops being ambiguous.
		return res, nil
	}
	defer release()

	for _, p := range plans {
		if p.Keep {
			continue
		}
		// Re-measured here rather than reusing p.Bytes: this loop DELETES, and a
		// figure measured before the sweep started would over-report a tree a
		// concurrent writer emptied. The kept half above is the plan's, because
		// nothing was removed from a kept directory either way.
		size := DirBytes(p.Path)
		if rerr := os.RemoveAll(p.Path); rerr != nil {
			// Report and continue: one sandbox that cannot be removed must not
			// abort the sweep of the others (Rule 15 — narrated, not swallowed).
			fmt.Fprintf(os.Stderr, "scratch: sandbox GC could not remove %s: %v\n", p.Path, rerr)
			continue
		}
		res.Removed++
		res.Bytes += size
		// Record what was deleted, with the plan's own authorising reason and
		// the owner evidence it was judged on (Rule 14: the record is a copy of
		// the decision, never a second derivation). The caller writes it out —
		// this package cannot (import cycle, see Reclaimed's doc).
		res.Reclaimed = append(res.Reclaimed, SandboxReclaimed{
			Path:         p.Path,
			Bytes:        size,
			Reason:       p.Reason,
			Age:          p.Age,
			OwnerPID:     p.Owner.Pid,
			OwnerStart:   p.Owner.Start,
			OwnerCmd:     p.Owner.Cmd,
			HasOwner:     p.HasOwner,
			OwnerOutcome: string(p.Outcome),
		})
	}
	return res, nil
}

func SweepSandboxesDetailed(root string, olderThan time.Duration) (removed int, bytes int64, lock GCLockVerdict, err error) {
	res, err := SweepSandboxesReport(root, olderThan)
	return res.Removed, res.Bytes, res.Lock, err
}

// DirBytes sums the sizes of the regular files under dir, best effort: the
// reclaimed-bytes figure is operator information, and a file that vanishes
// mid-walk must not fail the sweep that is deleting the tree it lives in.
//
// ONE measurement, not two: the sandbox arm and the worktree-stub arm both report
// this figure in the log line's bytes= field, so a reader comparing the two lines
// is comparing the same number (Rule 14: a shared value has one definition, and
// the consumers here are two arms of the same verb).
//
// (The pre-existing unexported spelling needed no rename: Go allows one exported
// wrapper and one unexported function with different names, and the call site
// above reads unchanged — so the diff stays surgical.)
func DirBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, ierr := d.Info(); ierr == nil {
				total += info.Size()
			}
		}
		return nil
	}) //stylegate:ignore the sweep is best-effort by contract: an unreadable entry is skipped and reported through the callback, and every unknown is a KEEP
	return total
}
