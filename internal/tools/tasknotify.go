package tools

// Task state-transition notifications: whenever a task row moves along the
// dispatcher state machine (pending → claimed → in_progress →
// completed/failed, plus the recovery moves), an optional notification is
// fired to configurable targets — a webhook URL (HTTP POST of the JSON
// payload) or a shell command (payload on stdin).
//
// Configuration resolves per-knob through config.ResolveNotify — the SINGLE
// resolver (CLI flag > GOPI_TASK_NOTIFY_* env > settings.json "notify"
// section > code default, following the config package's declared-topology
// + FormatEnvTable conventions) and is re-read on EVERY firing (the
// ~/work/tasks subscribe hot-reload philosophy: config changes take effect
// without restarting the process). settings.json is the DURABLE layer: a
// serve daemon spawned with no notify env (Hermes background / shell
// wrapper / restart) still delivers to the settings-configured target; env
// only overrides for a one-off run:
//
//	settings.json "notify": { "webhook": "", "cmd": "", "events":
//	  "claimed,in_progress,completed,failed,superseded", "pattern": "", "async": false }
//	GOPI_TASK_NOTIFY_WEBHOOK  webhook URL to POST the JSON payload to
//	GOPI_TASK_NOTIFY_CMD      shell command to run with the payload on stdin
//	GOPI_TASK_NOTIFY_EVENTS   comma list of statuses/synthetic alarms that
//	                          fire (default
//	                          "claimed,in_progress,completed,failed,superseded";
//	                          "all"/"any" = every status incl.
//	                          pending/cancelled PLUS every synthetic alarm
//	                          — taskNotifyAlarmEvents is that list)
//	GOPI_TASK_NOTIFY_PATTERN  glob filter (task_type_pattern equivalent)
//	                          matched against the task's tags AND task_id;
//	                          empty = broadcast (every task)
//	GOPI_TASK_NOTIFY_ASYNC    truthy = deliver in a detached goroutine so a
//	                          slow webhook never delays the ledger writeback
//
// Delivery failures are logged to stderr and NEVER fatal: a notification is
// a side-channel, the ledger transition itself is already committed.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/config"
)

// Notification delivery timeouts and env-var names — every literal lives
// here (single source of truth, no magic numbers at call sites).
const (
	taskNotifyWebhookTimeout = 10 * time.Second // HTTP POST deadline
	taskNotifyCmdTimeout     = 30 * time.Second // per-command deadline

	// taskNotifyLineTerminator is the byte the cmd target's stdin payload ends
	// with — EXACTLY one, whatever the caller handed us (see
	// taskNotifyCmdStdin). Without it the documented sink configuration
	// (notify.cmd = `cat >> …/task-events.jsonl`) concatenates consecutive
	// JSON objects into ONE unterminated line: `wc -l` reports 1 for a
	// multi-megabyte file, and every line-oriented reader (tail/jq/read)
	// silently reads it wrong. Measured 2026-09-20 on the operator's sink:
	// 135,159 events under a single `\n`. The webhook body is untouched —
	// this is the line-framing of a stream, not the JSON payload.
	taskNotifyLineTerminator = '\n'

	// taskNotifyLedgerJSONKey is the wire spelling of the payload's origin-ledger
	// field (taskNotifyPayload.Ledger). It is a constant because the Go field,
	// the test that pins the wire shape, and the shell sink that RENDERS it
	// (scripts/notify/notify.sh's build_message) all name the same fact — a
	// second spelling at any of those three sites is how the message and the
	// payload drift apart (Rule 1b/13).
	taskNotifyLedgerJSONKey = "ledger"

	// taskNotifyResumedFromJSONKey is the wire spelling of the payload's
	// resume-witness field (taskNotifyPayload.ResumedFrom). Same reason as its
	// sibling above (Rule 1/13/14): the Go field, the test that pins the wire
	// shape, and any reader of the emitted JSON all name the same fact, and a
	// struct tag cannot hold a constant — so the two spellings live apart by
	// force and a test pins them together
	// (TestTaskNotifyResumedFromJSONKeyMatchesTheStructTag).
	taskNotifyResumedFromJSONKey = "resumed_from"

	// --- THE ALLOW-LIST DECISION SITE -------------------------------------
	//
	// taskNotifyEventsExcludedDefault + taskNotifyEventsExcludedDefaultMembers
	// (declared below) are WHERE THE DECISION IS RECORDED. The default event
	// set must decide about every WRITABLE task status
	// (taskStatusWritable, tasks.go), so each writable status is either a
	// MEMBER of the default set or an entry of the exclusion table carrying
	// its reason. TestTaskNotifyEventsDefaultCoversEveryWritableStatus fails
	// on a status present in NEITHER (and on an exclusion naming a status
	// that is not writable, or one that is also a member), so the set can no
	// longer drift away from the vocabulary in silence.
	//
	// THE DEFECT THIS CLOSES: the default was hand-spelled when the
	// vocabulary had four members, so `superseded` — a writable terminal
	// status added 2026-10-07 by the parent-release-statuses epic — was
	// absent from a set that had to decide about it. Measured before the
	// fix: `gopi task update <row> --status superseded` exits 0 and the row
	// reads superseded while the notify layer drops the event (Rule 15
	// diagnostic, delivery filter — the write is not affected).
	//
	// WHY `superseded` IS A MEMBER (a decision, not a default):
	//   - it is a Rule-33 parent-GATE RELEASE status (taskParentReleaseStatuses
	//     in tasks.go): reaching it releases the row's parked children — the
	//     single most consequential thing a parent's lifecycle does. A
	//     release nobody is told about is the silent-success shape Rule 15
	//     exists to forbid, and its rank-3 peer `cancelled` is allow-listed
	//     on the measured host, so silence here would be an inconsistency
	//     between two statuses with the same lifecycle weight;
	//   - the semantic is "the work is carried ELSEWHERE" (a successor row
	//     exists), i.e. the operator's dependency chain MOVED — the same
	//     class of news as `cancelled`, which is subscribed;
	//   - its cost is bounded: it is written only by an explicit decision
	//     (`gopi task update … --status superseded`, `gopi task send
	//     --cancel-source`), never by the worker/mover paths, so it cannot
	//     become the noise that keeps `in_progress` out of a narrow list.
	//
	// The recorded EXCLUSIONS are `pending`, `cancelled` and `verified` —
	// three, not zero and not one: see taskNotifyEventsExcludedDefault
	// below for the reason each carries, and
	// TestTaskNotifyEventsDecidedStatusesAreExactly for why the LIST is
	// pinned (moving a status out of the default set and into the table is
	// a policy change, and it must edit that test and this comment too).
	//
	// --- end of the decision site: the members and the exclusion table ---
	taskNotifyEventsAll = "all"
	// taskNotifyEventsAny is the accepted synonym of taskNotifyEventsAll.
	taskNotifyEventsAny = "any"

	// taskNotifyEventTerminalPushFailed is the M4.19 ② synthetic alarm
	// event: the end-of-life push to sync_peers exhausted its retries.
	// Operators subscribe via events (like "stale").
	taskNotifyEventTerminalPushFailed = "terminal-push-failed"

	// taskNotifyEventLegacyConflict is the M4.19 ③ synthetic alarm event:
	// a delta import hit a both-sides-wrote conflict. The conflict-ruling
	// is legacy/bug-path only since the M4.18 claim broadcast — a hit here
	// means the start-point atomicity failed and needs a root-cause look.
	taskNotifyEventLegacyConflict = "legacy-conflict"

	// taskNotifyEventLedgerConflictHumanTask is the M4.16 semantic-fix
	// alarm (bug053): the sync server reached a conflict ruling on a REAL
	// human/LLM ticket (source manual, payload non-empty). Historically the
	// server blind-resolved such rows to completed + sync-resolved — which
	// FAKE-CLOSED real work: bug050/051/052 all carried sync-resolved but
	// the implementation never existed (the conflict ruling granted a
	// verdict only the real work merited). A real ticket can never be
	// blind-flattened — it must surface for a human/PA to adjudicate.
	taskNotifyEventLedgerConflictHumanTask = "ledger-conflict-human-task"

	// The three SESSION-level alarms are FIRED by internal/agent (its
	// loop-stuck / toolStreak / wallclock detectors) but their wire names
	// are DECLARED here: the "all" expansion enumerates every alarm from
	// taskNotifyAlarmEvents below, and a name mirrored into a second
	// package drifts out of that enumeration silently (Rule 14).
	// internal/agent aliases these constants for its call sites.
	taskNotifyEventLoopStuck            = "loop-stuck"
	taskNotifyEventLoopStuckToolful     = "loop-stuck-toolful"
	taskNotifyEventLoopStuckObservation = "loop-stuck-observation"
	taskNotifyEventWallclockExceeded    = "session-wallclock-exceeded"
)

// Exported aliases of the session-level alarm wire names — internal/agent's
// call sites use these so each name has exactly ONE definition.
const (
	NotifyEventLoopStuck        = taskNotifyEventLoopStuck
	NotifyEventLoopStuckToolful = taskNotifyEventLoopStuckToolful
	// NotifyEventStuckObservation is the observation bound's alarm (the
	// look-without-fixing net, §6.3): the run looked at live state and never
	// acted. Same session-alarm family as its two siblings.
	NotifyEventStuckObservation  = taskNotifyEventLoopStuckObservation
	NotifyEventWallclockExceeded = taskNotifyEventWallclockExceeded
)

// taskNotifyAlarmEvents is EVERY synthetic alarm event the notify pipeline can
// fire — the signals with no ledger transition behind them (a merged repo
// whose build failed, a spinning task, a withdrawn merge, a rollback …). It is
// the SINGLE SOURCE OF TRUTH for the "all"/"any" expansion: an operator whose
// whole configuration says "tell me everything" receives a new alarm by that
// alarm's constant being added HERE and nowhere else.
//
// The defect this closes: the expansion HAND-LISTED three names, so an alarm
// introduced later was silently dropped for exactly the subscriber who asked
// for all of them — measured 2026-09-19 on 0d3ce544, EVENTS=all delivered NONE
// of merge-reverted / build-failed / stale / loop-stuck (the agent-side alarms
// were mirrored literals in internal/agent, invisible to the expansion).
// TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth fails if a fired alarm is
// missing from this slice, and fails again if an entry is not a declared
// constant.
var taskNotifyAlarmEvents = []string{
	// M4.19/② and ③: the must-succeed push's and the delta-conflict alarms.
	taskNotifyEventTerminalPushFailed,
	taskNotifyEventLegacyConflict,
	// M4.16 semantic-fix alarm (bug053) — a real ticket must never be
	// blind-flattened, it surfaces for human adjudication.
	taskNotifyEventLedgerConflictHumanTask,
	// The task-lifecycle and merge-gate alarms.
	TaskHeartbeatNotifyEvent, // "stale"
	BuildFailedEvent,         // "build-failed"
	// "merge-handover-failed" and "merge-withdrawn" BOTH LEFT THIS LIST in the
	// 2026-11-01 landing, and the constants went with them. The landings came
	// from two different sides and touch the same list, so the resolution is the
	// UNION OF TWO REMOVALS — neither side is kept (see below, per alarm):
	//
	//   - "merge-handover-failed": its only writer was `gopi merge`'s hand-over
	//     failure counter (`internal/tools/merge_handover_counter.go`), deleted
	//     in the same change on the upstream side. Measured on the merge result:
	//     `git grep -n MergeHandoverEvent -- '*.go'` → three hits, NONE of them
	//     a reference: this comment, plus two lines of
	//     `internal/standards/citation_selector_gate_test.go` whose text is a
	//     `-run` selector EXAMPLE (`TestMergeHandoverEventLiteralMirrorGate`, a
	//     test name that exists nowhere —
	//     `git grep -n 'func TestMergeHandoverEventLiteralMirrorGate' -- '*.go'`
	//     → no hits). The statement "no hits" was wrong when it was written:
	//     those two fixture lines landed upstream in `91c79ae4` and are quoted
	//     selectors, which is exactly the distinction a bare grep count loses.
	//   - "merge-withdrawn": its only two fire sites were inside
	//     `internal/tools/merge_pass_verify.go` (the retired top-level merge
	//     verb's post-pass verify/withdraw lane), deleted in the same commit
	//     that removes this entry. Measured: the only spellings of the literal
	//     left in the tree are this file's own comments.
	//
	// WHY A UNION OF REMOVALS AND NOT "ONE SIDE WINS": the shared premise of
	// both landings is that EVERY alarm registered here must have a production
	// call site that FIRES it — otherwise `GOPI_TASK_NOTIFY_EVENTS=all`
	// promises a subscriber an event nothing can emit, which is the shape this
	// list exists to prevent, arrived at from the other side. Keeping either
	// entry would land that dead promise. The reverse direction is the check:
	// TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth derives the required set
	// from the code's own NotifySyntheticEvent call sites, so a registered alarm
	// with no fire is a provider of nothing (Rule 43: the file names said
	// "hand-over counter" and "pass verify", and those are what left).
	// `RevertedEvent` and the two `MergeVerifyStale*` alarms below are LIVE
	// (measured at the deletion commit: fire sites in merge_revert.go /
	// rebuild_vet_revert.go and task_staleness_guard.go) and are untouched.
	RevertedEvent,         // "merge-reverted" — the merge-rollback alarm
	MergeVerifyStaleEvent, // "merge-verify-stale-rework"
	MergeVerifyStaleLandedEvent,
	// The worktree/cleanup/refusal notices — an operator subscribing to
	// EVERY event must not be the one subscriber that never hears about a
	// dirty worktree, a reaped one, or a refusal/escalation (Rule 15: the
	// refusal is never silent).
	worktreeOrphanEscalationEvent, // "worktree-orphan"
	mergedCleanupReapedEvent,      // "worktree-reaped"
	taskOffPolicyEvent,            // "worktree-off-policy"
	repoSweepWouldPruneNoticeEvent,
	// The merge-backlog dispatch guard's alarm: it closes a row whose branch
	// ref does not resolve and fires the guard's own event constant, so an
	// operator subscribed to everything hears the refusal instead of reading
	// logs. Same side-channel as the merge notices above, and it must be
	// registered here or GOPI_TASK_NOTIFY_EVENTS=all drops it silently
	// (TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth derives the required
	// set from the fire site, so the next fired alarm lands here too).
	// The wire literal is spelled ONLY at that constant's definition site.
	mergeBacklogGhostEvent,
	// The SAME guard's second arm: the row's branch is a RETIRED, DECIDED
	// EXCLUSION (an archive tag with a vocabulary reason) rather than a ref that
	// vanished. A different fact, so a different alarm — and registered here for
	// the same reason as its sibling: an operator who asked for everything must
	// hear it.
	mergeBacklogRetiredEvent,
	// A RUN'S TERMINAL WRITEBACK THAT DID NOT LAND (task_writeback_lost.go,
	// measured 2026-10-27). The wrapper's failed-branch writeback used to
	// discard the ledger's refusal, so a row stayed `in_progress` with a dead
	// holder and nothing anywhere said why. This is the alarm half; the same
	// constant is the row's own trail event (verdictTrailEventNames), so the
	// subscriber who asked for everything and the operator reading
	// `task show` learn the same fact from the same name (Rule 1b/13).
	WritebackLostEvent,
	// The agent-side session alarms (declared above, fired by internal/agent).
	taskNotifyEventLoopStuck,
	taskNotifyEventLoopStuckToolful,
	taskNotifyEventLoopStuckObservation,
	taskNotifyEventWallclockExceeded,
	// (the run close-out reap's `run-closeout-reaped` event was removed with the
	// reap itself on 2026-11-05: a mechanism that should not run has no alarm
	// worth subscribing to, and a vocabulary entry left behind would teach the
	// next reader that it still fires.)
}

// --- THE DEFAULT EVENT SET AND ITS RECORDED EXCLUSIONS (decision site) ----
//
// taskNotifyEventsExcludedDefault is the OTHER half of the default-set
// decision: the writable statuses deliberately NOT in it, each with the
// reason it is out. Membership and exclusion are ONE decision expressed in
// two tables — TestTaskNotifyEventsDefaultCoversEveryWritableStatus requires
// every status of taskStatusWritable to appear in exactly one of them, so a
// status added to the vocabulary (or dropped from the default string) fails
// the build until someone writes down what should happen to it.
//
// "A status is in neither table" and "a status is in both" are BOTH failures:
// the first is the `superseded` defect (silence nobody decided on), the
// second a contradiction (a set cannot both deliver and refuse an event).
var taskNotifyEventsExcludedDefault = map[string]string{
	taskStatusPending: "row BIRTH, not a transition: TaskSubmit/TaskNewOID mint " +
		"rows born pending (or in_progress) and a birth has no previous status to " +
		"announce. The enqueue path is the operator's own action, so telling them " +
		"about it is a deliberate `events=pending` subscription, never a default. " +
		"(task-notify-terminal-gap, 2026-09-12 — the same reason the default " +
		"member table above has never listed it.)",

	taskStatusCancelled: "writable and DELIVERABLE on subscription, but not a " +
		"default: the default is the narrow set an operator who configured nothing " +
		"gets, and `cancelled` is a decision already taken by the operator's OWN " +
		"list (the measured host settings.json carries " +
		"claimed,in_progress,completed,failed,stale,build-failed,cancelled). Adding " +
		"it here would change nothing on that host and would only widen the " +
		"untouched-config default, which is a noise-budget decision this fix has no " +
		"mandate to make. NB this is NOT the `superseded` case: cancelled was " +
		"reachable in practice via the operator's list, superseded was in NEITHER " +
		"the default nor that list — undeliverable in every configuration.",

	taskStatusVerified: "writable and DELIVERABLE on subscription, but not a " +
		"default, on the same noise-budget reasoning as cancelled above: `verified` " +
		"is the verify loop's UPGRADE of a row that already announced `completed`, so " +
		"defaulting it would double-report one lifecycle unless the operator asks " +
		"(and the measured host does not list it). Recorded here rather than left " +
		"unmentioned so the choice is a decision on the record, not silence.",
}

// taskNotifyEventsExcludedDefaultMembers is the SET view of the table above:
// the builder consults it, and the membership gate reads the table's text.
// Derived, not mirrored (Rule 2) — one literal spills here.
var taskNotifyEventsExcludedDefaultMembers = func() map[string]bool {
	out := make(map[string]bool, len(taskNotifyEventsExcludedDefault))
	for status := range taskNotifyEventsExcludedDefault {
		out[status] = true
	}
	return out
}()

// taskNotifyEventsMembers is the MEMBER TABLE's source of truth: the statuses
// delivered when neither GOPI_TASK_NOTIFY_EVENTS nor settings.json
// "notify".events is set.
//
// WHAT IS IN IT, AND WHY EACH ONE IS. It is not a list of conveniences: each
// member is a transition an operator reads as news about work they own.
//
//	claimed      — "the row was picked up" (the original P2 migration set)
//	in_progress  — "work started"
//	completed    — "the work finished" (success)
//	failed       — "the work finished" (with a verdict to act on)
//	superseded   — "the row is retired because a SUCCESSOR carries the work":
//	               the Rule-33 release signal for its parked children, the
//	               rank-3 lifecycle peer of the allow-listed `cancelled`, and
//	               written only by an explicit decision (never by a mover
//	               path) — see the decision-site comment above.
//
// `cancelled` and `verified` are deliberately NOT default members either —
// they are writable and fully deliverable on subscription, but the default is
// the set an operator who configured NOTHING gets, and both are a noise-budget
// decision rather than a gap: they are RECORDED as exclusions below with that
// reason, which is the difference between "the decision is this" and the
// `superseded` defect (in neither table, in no configuration deliverable).
var taskNotifyEventsMembers = []string{
	taskStatusClaimed,
	taskStatusInProgress,
	taskStatusCompleted,
	taskStatusFailed,
	taskStatusSuperseded,
}

// taskNotifyEventsDefault is the wire string the default members render to.
// Built from taskNotifyEventsMembers (below) so the string and the table
// cannot drift; taskStatusSet/taskNotifyEventsMembers hold the constants, so
// no status literal is spelled in the default itself (Rule 1/13).
//
// FUNCTION, not a package-level var initialised from taskStatusWritable: it
// keeps the package's variable-init order clear (taskStatusWritable and the
// tables above are all initialised in the same file set, and a package-level
// call here would depend on which file's var block the compiler reaches
// first). callers are few and non-hot (loadTaskNotifyConfig runs once per
// notify firing, already doing a settings read).
func taskNotifyEventsDefault() string {
	return strings.Join(taskNotifyEventsMembers, ",")
}

// The five GOPI_TASK_NOTIFY_* env-var names, as local aliases into the
// config package — the config package owns the CANONICAL mapping (Rule 2:
// each knob read in exactly one place, config.ResolveNotify); these aliases
// keep the tools-package tests and call sites on the short spelling.
const (
	taskNotifyWebhookEnv = config.NotifyWebhookEnv
	taskNotifyCmdEnv     = config.NotifyCmdEnv
	taskNotifyEventsEnv  = config.NotifyEventsEnv
	taskNotifyPatternEnv = config.NotifyPatternEnv
	taskNotifyAsyncEnv   = config.NotifyAsyncEnv
)

// taskNotifyTokensRe extracts the token count the auto-register harness
// appends to the summary tail ("… — elapsed=12.3s iter=4 tokens=5678").
// Best-effort: tasks without the suffix report tokens=0.
var taskNotifyTokensRe = regexp.MustCompile(`tokens=(\d+)`)

// taskNotifyConfig is the resolved notification configuration for one
// firing. Zero-value (no webhook, no cmd) = notifications disabled.
type taskNotifyConfig struct {
	webhookURL string
	cmd        string
	events     map[string]bool
	// eventsSpec is the RESOLVED raw event spec the map above was parsed
	// from (env/settings value, else taskNotifyEventsDefault()). Kept so the
	// "not delivered" diagnostic can quote the operator's own allow-list
	// instead of an unexplained "[task-notify]" line — task
	// fix/task-update-error-message: that line, printed on stderr next to a
	// successful write, is what made a successful cancel read as a failure.
	eventsSpec string
	pattern    string
	async      bool
}

// loadTaskNotifyConfig resolves the notification configuration fresh on
// every call (the ~/work/tasks subscribe hot-reload philosophy: config
// changes take effect without restarting the process). Resolution is
// delegated to config.ResolveNotify — the SINGLE resolver for every notify
// path — which merges per-knob: GOPI_TASK_NOTIFY_* env (explicit override,
// even when set to "") > settings.json "notify" section > code default.
// The settings layer makes notify configuration DURABLE: a serve daemon
// spawned with no notify env (Hermes background / shell wrapper — the
// 2026-09-02 incident where a restart dropped GOPI_TASK_NOTIFY_CMD and
// stale events silently stopped landing) still delivers to the
// settings-configured cmd/webhook. Zero-value config = notify disabled.
func loadTaskNotifyConfig() taskNotifyConfig {
	cfg := taskNotifyConfig{}
	s := config.ResolveNotify()
	if s.Webhook != "" || s.Cmd != "" {
		cfg = taskNotifyConfig{
			webhookURL: s.Webhook,
			cmd:        s.Cmd,
			pattern:    s.Pattern,
			async:      s.Async,
		}
	}
	events := strings.TrimSpace(s.Events)
	if events == "" {
		events = taskNotifyEventsDefault()
	}
	cfg.eventsSpec = events
	cfg.events = make(map[string]bool)
	for _, ev := range strings.Split(events, ",") {
		ev = strings.TrimSpace(ev)
		if ev == "" {
			continue
		}
		if strings.EqualFold(ev, taskNotifyEventsAll) || strings.EqualFold(ev, taskNotifyEventsAny) {
			for _, s := range taskStatusSet {
				cfg.events[s] = true
			}
			// Every synthetic alarm rides "all" too — from the registry, NOT
			// from a hand-list: a subscriber who asked for everything must
			// not silently miss an alarm, and the list of alarms must not
			// need a second edit site when one is added. A new alarm is
			// enabled here by its constant entering taskNotifyAlarmEvents.
			for _, alarm := range taskNotifyAlarmEvents {
				cfg.events[alarm] = true
			}
			continue
		}
		cfg.events[ev] = true
	}
	return cfg
}

// enabled reports whether the config has at least one delivery target.
func (c taskNotifyConfig) enabled() bool {
	return c.webhookURL != "" || c.cmd != ""
}

// taskNotifyPayload is the JSON body delivered to webhook/cmd targets.
type taskNotifyPayload struct {
	Event          string `json:"event"`           // status just reached
	PreviousStatus string `json:"previous_status"` // status before the transition
	TaskID         string `json:"task_id"`
	// Ledger names the ledger THIS event came out of, and it is present only
	// when that ledger is not the profile's own (see taskNotifyOriginLedger).
	//
	// WHY IT EXISTS (measured, 2026-10-23): gopi resolves the LEDGER and the
	// NOTIFY TARGET through two INDEPENDENT channels. `--db` / GOPI_MEMORY_DB
	// moves only the ledger; config.ResolveNotify reads settings.json in the
	// PROFILE ROOT ($GOPI_HOME/.gopi, else $HOME/.gopi). A run that isolates
	// only its ledger — a probe program, a `--db` CLI call — therefore
	// delivers into the OPERATOR's live sink, and the event it lands names a
	// task id that exists in NO ledger the console can open: exactly the two
	// rows (`01a16515-…`, `probe-dead-holder`) the operator could neither
	// confirm nor reissue. The finding is stamped rather than filtered,
	// because an event from a legitimately separate profile is still an event
	// the operator may want; what must never happen is an UNTRACEABLE one.
	Ledger         string  `json:"ledger,omitempty"` // the wire spelling is taskNotifyLedgerJSONKey — a struct tag cannot hold a const, so a test pins the two
	Status         string  `json:"status"`
	Summary        string  `json:"summary"`
	Result         string  `json:"result,omitempty"` // bug056: latest task_events detail (run stats / failure digest) — the summary is immutable, the digest rides the audit trail
	Reason         string  `json:"reason,omitempty"` // task-update-reason epic: the operator-visible WHY of the transition (row's reason column)
	SessionID      string  `json:"session_id,omitempty"`
	Model          string  `json:"model,omitempty"`
	Tags           string  `json:"tags,omitempty"`
	Source         string  `json:"source,omitempty"`
	ClaimedBy      string  `json:"claimed_by,omitempty"`
	ElapsedSeconds float64 `json:"elapsed_seconds"` // created_at → ended_at (fallback updated_at)
	Tokens         int     `json:"tokens"`          // parsed from summary tail, 0 if absent
	CreatedAt      float64 `json:"created_at"`
	UpdatedAt      float64 `json:"updated_at"`
	Timestamp      string  `json:"timestamp"` // RFC3339 delivery time
	// Zombie carries the M4.8 ③ evidence chain (taskZombieEvidence JSON,
	// "" when absent): the verify agent reads it to pick the action —
	// confirmed → reissue the work as a fresh row or cancel+redispatch; suspected → look
	// closer (session JSONL mtime in the evidence).
	Zombie string `json:"zombie,omitempty"`
	// ResumedFrom names the session a RE-DISPATCHED run continues, and it is
	// present only for a run the door declared a re-dispatch (see
	// taskRunResumeWitness). The wire spelling is
	// taskNotifyResumedFromJSONKey — a struct tag cannot hold a const, so a
	// test pins the two.
	//
	// WHY THE PAYLOAD NEEDED IT (measured, 2026-10-31): a row's `completed`
	// event and its resume-close `completed` event were BYTE-IDENTICAL
	// (`event`/`previous_status`/`task_id`/`status`/`summary` — both
	// in_progress → completed), so a subscriber could not tell "this ran to
	// its end" from "this was resumed and closed after an earlier failure".
	// The fact already existed on the door (tools.TaskRedispatchResumeFrom,
	// decided BEFORE the spawn) and in session_start
	// (rundir.ResumedFrom); it simply was not forwarded here.
	//
	// WHY IT IS A FIELD AND NOT A NEW EVENT NAME: resume is a PROPERTY of the
	// claimed/completed transition, not a signal of its own. A new name would
	// enter the events=all expansion (no increment for a subscriber already
	// told `completed`), add a second place to keep in step with
	// taskNotifyAlarmEvents, and state the wrong semantic (Rule 15a: an
	// optional outgoing field, so `omitempty` keeps every ordinary event
	// byte-identical and adds zero bytes for the steady state).
	ResumedFrom string `json:"resumed_from,omitempty"`
}

// taskNotifyPatternMatch is the task_type_pattern equivalent: the glob is
// matched against the task's tags (the whole comma string AND each
// individual tag, so "feat,web" matches "feat" / "web" / "feat,web") and the
// task_id. Empty pattern = broadcast. An invalid glob matches nothing.
func taskNotifyPatternMatch(pattern, taskID, tags string) bool {
	if pattern == "" {
		return true
	}
	candidates := []string{taskID, tags}
	candidates = append(candidates, strings.Split(tags, ",")...)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if ok, err := filepath.Match(pattern, c); err == nil && ok {
			return true
		}
	}
	return false
}

// taskNotifyElapsed computes elapsed seconds for the payload: ended_at is
// authoritative for terminal states; updated_at is the fallback for
// intermediate ones (claimed/in_progress) and pre-P2 rows.
func taskNotifyElapsed(t *Task) float64 {
	from := t.UpdatedAt
	if t.HasEnded && t.EndedAt >= t.CreatedAt {
		from = t.EndedAt
	}
	if from <= t.CreatedAt {
		return 0
	}
	return from - t.CreatedAt
}

// taskNotifyTokens parses the best-effort token count out of the summary
// tail (last tokens= occurrence wins; 0 when the suffix is absent).
func taskNotifyTokens(summary string) int {
	m := taskNotifyTokensRe.FindAllStringSubmatch(summary, -1)
	if len(m) == 0 {
		return 0
	}
	n, err := strconv.Atoi(m[len(m)-1][1])
	if err != nil {
		return 0
	}
	return n
}

// taskNotifyStatsSource returns the stats text a notify payload should
// parse tokens from: the row's summary when it already carries stats (a
// legacy row / submit-time stats), otherwise the latest task_events detail
// (bug056: the writeback's stats live in the audit trail now — the summary
// is immutable). Best-effort: any failure falls back to the summary.
func taskNotifyStatsSource(t *Task) string {
	if taskNotifyTokensRe.MatchString(t.Summary) {
		return t.Summary
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return t.Summary
	}
	defer db.Close()
	if _, detail := taskEventLatest(ctx, db, t.TaskID); detail != "" {
		// A STATUS event now carries the three-element lifecycle record plus an
		// optional digest trailer (epic 01a1a899-d05d). A notification about the
		// run must quote the RUN's line, not the record's grammar, so the digest
		// wins whenever one rides the detail; a plain transition (no trailer)
		// keeps its record, which is more than the empty string it used to
		// contribute here.
		if d := taskStatusEventStripTrailer(detail); d != detail {
			return d
		}
		return detail
	}
	return t.Summary
}

// notifyTaskTransition is the single hook call-site helper: resolves the
// config, filters (event set + pattern), and delivers. Never returns an
// error — delivery problems are logged, the ledger transition stands.
// The undecorated face of notifyTaskTransitionDecorate (see the bus file
// for the buffered callers).
func notifyTaskTransition(prevStatus string, t *Task) {
	notifyTaskTransitionDecorate(prevStatus, t, nil)
}

// notifyTaskTransitionDecorate is notifyTaskTransition plus an optional
// payload decorator — the hook through which an event-specific field (the
// zombie evidence chain on the "stale" alarm) reaches a payload that is
// otherwise derived from the task row alone. Keeping ONE implementation is
// the point: the pre-bus tree had a second, hand-rolled delivery function
// whose config/events/pattern gates were a copy of this one.
func notifyTaskTransitionDecorate(prevStatus string, t *Task, decorate taskNotifyPayloadDecorator) {
	if t == nil || prevStatus == t.Status {
		return // same-status writeback is an idempotent no-op, not a transition
	}
	cfg := loadTaskNotifyConfig()
	if !cfg.enabled() {
		return // no target configured — nothing to deliver to
	}
	if !cfg.events[t.Status] {
		// Rule 15: a configured operator subscriber that does NOT list this
		// status is a drop the operator should be able to see when hunting
		// a missing line (the events allow-list is the documented filter —
		// this only makes the filter's effect visible, it never widens it).
		// The wording says what it IS (a delivery filter) and what to change,
		// because the bare line used to sit on stderr right after a
		// successful write and read as an error (task
		// fix/task-update-error-message).
		taskNotifyLog("event %q for %s not in the events allow-list — not delivered (this is a delivery FILTER, not a failure; the write succeeded — %s=%q; add %q to it, or set it to %q for every event)",
			t.Status, t.TaskID, notifyEventsKnobName(), cfg.eventsSpec, t.Status, taskNotifyEventsAll)
		return
	}
	if !taskNotifyPatternMatch(cfg.pattern, t.TaskID, t.Tags) {
		taskNotifyLog("event %q for %s filtered by pattern %q — not delivered", t.Status, t.TaskID, cfg.pattern)
		return
	}
	payload := taskNotifyPayload{
		Event:          t.Status,
		PreviousStatus: prevStatus,
		TaskID:         t.TaskID,
		Ledger:         taskNotifyOriginLedger(),
		Status:         t.Status,
		Summary:        t.Summary,
		Result:         taskNotifyStatsSource(t), // bug056: the failure digest / run stats ride events, not the summary
		Reason:         t.Reason,                 // task-update-reason: the WHY of the transition the notify announces
		SessionID:      t.SessionID,
		Model:          t.Model,
		Tags:           t.Tags,
		Source:         t.Source,
		ClaimedBy:      t.ClaimedBy,
		ElapsedSeconds: taskNotifyElapsed(t),
		// bug056: the writeback's run stats no longer ride the summary —
		// they live in the task_events trail. Parse tokens from the latest
		// event detail when the summary itself carries none (a legacy /
		// submit-time-stats summary still parses directly).
		Tokens:    taskNotifyTokens(taskNotifyStatsSource(t)),
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
		Timestamp: time.Now().Format(time.RFC3339),
		// The door's resume declaration, forwarded (epic: notify payload
		// carries resumed_from). Empty on every ordinary run, so omitempty
		// keeps the steady state byte-identical; present on a
		// re-dispatched run, where it is the ONE fact that separates a
		// resumed close from an ordinary one.
		ResumedFrom: taskRunResumeWitness(),
	}
	// Event-specific payload fields (the zombie evidence chain) are applied
	// through the shared path rather than a forked delivery function.
	if decorate != nil {
		decorate(&payload)
	}
	if cfg.async {
		go deliverTaskNotify(cfg, payload)
		return
	}
	deliverTaskNotify(cfg, payload)
}

// taskNotifyOriginLedger returns the ledger path an event is being emitted
// from, and "" when that ledger is the profile's OWN (`<gopi dir>/memory.db`).
//
// IT IS ALSO THE DELIVERY GATE's criterion (taskNotifyForeignLedgerReason):
// "is this ledger the profile's own?" is a PROPERTY, computed here once, never
// a filename or path-prefix guess. The payload stamp and the suppression below
// therefore cannot disagree — the older `ledger=` stamp was traceability only,
// and a stamp nobody filters on still announces a row the operator cannot open.
//
// It exists so a console can tell "this event came out of the ledger you are
// looking at" from "this event came out of a ledger you cannot open", without
// having to guess. The `omitempty` on the field is the truth condition: the
// ordinary case (the profile's own ledger — every worker, every CLI call, the
// whole steady state) adds ZERO bytes to the payload, so the shipped contract
// and every existing consumer are unchanged; only the case that produced the
// defect carries the stamp.
//
// THE ONE READER (Rule 2): memoryDBPath already owns the ranked resolution
// (`--db` > GOPI_MEMORY_DB > GOPI_HIPPO_DB > settings "memory_db" > default),
// so this compares against defaultMemoryDBPath rather than re-deriving either
// side. A ledger path that differs by SPELLING but resolves to the same file is
// not worth a special case — the comparison is textual on purpose, because a
// symlinked profile path that reports itself as "foreign" is a far cheaper
// error than a foreign ledger that reports itself as "own".
func taskNotifyOriginLedger() string {
	path := memoryDBPath()
	if path == "" {
		return ""
	}
	if filepath.Clean(path) == filepath.Clean(defaultMemoryDBPath()) {
		return ""
	}
	return path
}

// taskNotifyDelivers reports whether a payload may reach a configured delivery
// target, and returns the one-line reason when it may not.
//
// THE BUG IT CLOSES (measured on this host, 2026-11-04). gopi resolves the
// LEDGER and the NOTIFY TARGET through two INDEPENDENT channels: `--db` /
// GOPI_MEMORY_DB moves only the ledger, while config.ResolveNotify reads the
// PROFILE ROOT's settings.json ($GOPI_HOME/.gopi, else $HOME/.gopi). So a run
// that isolates ONLY its ledger delivered into the OPERATOR's live sink —
// chat slots plus ~/.gopi/notify/task-events.jsonl — and every event it landed
// named a task id that exists in NO ledger the operator can open. The prior fix
// (taskNotifyOriginLedger) stamped those payloads with the emitting ledger, so
// they became LOCATABLE; nothing stopped them being DELIVERED, and the shape is
// not rare: every red-proof run of every row writes its own private ledger
// (measured: session 01a1a60e-7c67's redproof/ carried g2/g3/g5/g6/r1/r2 — six
// ledgers, six broadcast rounds of invented rows "parent A"/"parent B").
//
// WHY THE DEFAULT IS SUPPRESSION, and not a knob (Rule 35): a private-ledger
// event reaching the operator's sink is a FALSE ALARM about work nobody
// dispatched — the reader's trust in the channel is the thing being spent. The
// safe direction is therefore the default, not a flag somebody has to remember
// to pass. A run that wants its own events broadcast isolates its PROFILE
// (GOPI_HOME), and then its ledger IS its profile's own and this gate passes it
// through unchanged — that is the sandbox-correct shape, and it keeps working
// byte for byte.
//
// WHAT THIS IS NOT. It does not filter by path PREFIX or file NAME (a private
// ledger is not "/tmp/x.db" — it is "not this profile's own", a property), it
// does not touch notify.events (that allow-list is a separate, documented
// filter), and it does not stop the event being RECORDED: the ledger write and
// its task_events audit row happen exactly as before. Only the DELIVERY to
// another profile's sink is withheld.
func taskNotifyDelivers(payload taskNotifyPayload) (bool, string) {
	foreign := taskNotifyOriginLedger()
	if foreign == "" {
		return true, ""
	}
	return false, fmt.Sprintf(
		"event %q for task %s NOT delivered: emitted from ledger %s, which is not this profile's own (%s) — its events belong to that ledger's own sink; to broadcast them, isolate the profile (GOPI_HOME) so its ledger becomes the profile's own",
		payload.Event, payload.TaskID, foreign, defaultMemoryDBPath())
}

// deliverTaskNotify fires every configured target for one payload.
// Failures go to stderr; delivery is best-effort by design.
func deliverTaskNotify(cfg taskNotifyConfig, payload taskNotifyPayload) {
	// THE ONE GATE, and it is HERE on purpose: every producer converges on this
	// function — notifyTaskTransitionDecorate, NotifySyntheticEvent and the
	// memory-sync legs (memory_sync_delta.go, memory_sync_claim.go) — so a
	// single check covers all of them and no future call site can forget it.
	if ok, reason := taskNotifyDelivers(payload); !ok {
		// Rule 15: a suppressed event is REPORTED with its reason, never
		// dropped in silence. Without this line "no notification" and "the
		// notify pipeline is broken" look identical to whoever is hunting.
		taskNotifyLog("%s", reason)
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		taskNotifyLog("marshal payload: %v", err)
		return
	}
	if cfg.webhookURL != "" {
		if code, err := taskNotifyWebhook(cfg.webhookURL, body); err != nil {
			taskNotifyLog("webhook %s: %v", cfg.webhookURL, err)
		} else if code < 200 || code >= 300 {
			taskNotifyLog("webhook %s: HTTP %d", cfg.webhookURL, code)
		} else {
			taskNotifyLog("webhook %s: ok (HTTP %d)", cfg.webhookURL, code)
		}
	}
	if cfg.cmd != "" {
		stdout, err := taskNotifyCmd(cfg.cmd, body)
		if err != nil {
			taskNotifyLog("cmd %q: %v", cfg.cmd, err)
		} else if stdout != "" {
			// Success is SILENT unless the command actually said something:
			// a notify hook that answers nothing (the shipped notify.sh is
			// exactly that — it fans out to the chat slots and appends to
			// task-events.jsonl, printing nothing) used to print one line per
			// EVENT, and "events" defaults to "all", so a single worker round
			// (claimed → in_progress → completed, each fanned out per chat
			// slot) buried the worker's own output under identical
			// `cmd ok (stdout: )` lines. Output the hook DOES produce still
			// rides the same line, so a hook that reports something keeps
			// its report; failure keeps its own line below.
			taskNotifyLog("cmd ok (stdout: %s)", stdout)
		}
	}
}

// taskNotifyWebhook POSTs the payload JSON to the configured URL.
func taskNotifyWebhook(url string, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), taskNotifyWebhookTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: taskNotifyWebhookTimeout}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// taskNotifyCmdStdin frames the payload for a line-oriented consumer: the
// bytes exactly as marshalled, terminated by exactly ONE '\n'.
//
// The producer owns framing — never the consumer. `sh -c` runs an arbitrary
// command (a file append, a webhook, a script, a cross-node hop), so a fix at
// that layer would have to be re-applied to every command the operator ever
// configures; guaranteeing the property once, here, covers all of them.
//
// Idempotent by construction: every trailing '\n' is dropped before the single
// terminator is appended, so a body that already ends in one produces no empty
// line, and json.Marshal's own output (which never ends in a newline) is
// byte-identical to before plus that one byte. The JSON itself is never
// touched — `cat >>` therefore appends one self-contained line per event.
func taskNotifyCmdStdin(body []byte) []byte {
	framed := make([]byte, 0, len(body)+1)
	framed = append(framed, bytes.TrimRight(body, "\n")...)
	return append(framed, taskNotifyLineTerminator)
}

// taskNotifyCmd runs the configured command via sh -c with the payload on
// stdin (framed by taskNotifyCmdStdin). Process-group isolation: a hung child
// is killed together with its children at the deadline (the ~/work/tasks
// fireCmd pattern).
func taskNotifyCmd(cmdLine string, body []byte) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), taskNotifyCmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdLine)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = bytes.NewReader(taskNotifyCmdStdin(body))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return stdout.String(), fmt.Errorf("timeout after %v", taskNotifyCmdTimeout)
			}
			if exitErr, ok := err.(*exec.ExitError); ok {
				return stdout.String(), fmt.Errorf("exit code %d (stderr: %s)", exitErr.ExitCode(), stderr.String())
			}
			return stdout.String(), err
		}
		return stdout.String(), nil
	case <-ctx.Done():
		if cmd.Process != nil {
			// The group id is RE-VERIFIED before delivery: it is this child's pid, and the
			// kernel may have recycled that number to an unrelated group (see killGroupGuard.go).
			_, _ = signalProcessGroup(cmd.Process.Pid, cmd.Process.Pid, syscall.SIGKILL) //stylegate:ignore signal to a process group we are already abandoning; ESRCH is the expected error
		}
		<-done
		return "", fmt.Errorf("timeout after %v", taskNotifyCmdTimeout)
	}
}

// taskNotifyLog writes delivery outcomes to stderr — the notification is a
// side-channel, so problems surface in logs without failing the tool call.
func taskNotifyLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[task-notify] "+format+"\n", args...)
}

// notifyEventsKnobName names the single knob behind the event allow-list, in
// BOTH of its spellings (config owns the strings — Rule 2): the env override
// and the durable settings.json section. Quoted by the "not delivered"
// diagnostics so the operator who sees the line knows which setting to edit.
func notifyEventsKnobName() string {
	return fmt.Sprintf("%s (env) / the %q section in settings.json", config.NotifyEventsEnv, config.NotifySettingsKey)
}

// memsyncWarnf is the warn face for sync-channel anomalies (M4.19 ③
// legacy-conflict re-label): stderr, always on — a conflict hit is a bug
// signal by definition, it must be visible even with notify unconfigured.
func memsyncWarnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[memsync][warn] "+format+"\n", args...)
}
