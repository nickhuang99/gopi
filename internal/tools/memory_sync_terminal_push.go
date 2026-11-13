package tools

// memory_sync_terminal_push.go: the END-OF-LIFE push leg, moved VERBATIM out
// of internal/tools/memory_sync_claim.go when the cross-node claim lock was
// removed. No semantics changed -- only the file.
//
// WHY THIS LEG SURVIVES THE DELETION IT WAS MOVED FOR: a row that reached a
// terminal status (completed / failed) is pushed to every declared sync peer
// by the writeback (internal/agent's close-out calls PushTerminalTaskSync),
// and the manual `gopi task sync --push-only` face is the operator's own copy
// of it. Removing the claim LOCK protocol does not remove that: the lock was
// removed because a single node can never contest it, while this leg is
// inert-by-configuration -- it returns nil the moment `task_sync` is off or no
// peer is declared, which is exactly this host's shape.
//
// Its transport (claimSSHExchange + ClaimPushResponse + exportClaimPatch) now
// lives in memory_sync_push_transport.go; the ssh argv helpers it shares with
// the other sync legs live in ssh_command.go. terminalPushRetries and the
// receipt constants moved with it, unchanged (Rule 1: the literals stay in one
// named place).

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
)

// terminalPushRetries is the must-succeed push's retry schedule (Rule 1:
// the M4.19 epic's 1s/2s/4s exponential backoff, one place). Each entry is
// one full push round against every peer; the sleep precedes rounds 2+. A
// var (arrays cannot be const): the production default lives here; tests
// may shorten it.
var (
	terminalPushMaxAttempts = 3
	terminalPushRetry1      = 1 * time.Second
	terminalPushRetry2      = 2 * time.Second
	terminalPushRetry3      = 4 * time.Second
)

var terminalPushRetries = [3]time.Duration{
	terminalPushRetry1,
	terminalPushRetry2,
	terminalPushRetry3,
}

// SyncTaskPushOnly is the push-only face of the claim channel (epic ③'s
// manual verb): push our rows to EVERY whitelisted peer WITHOUT pulling.
// taskID "" = the whole current delta window; a specific id = exactly that
// row (the manual re-push, idempotent — the peer's merge applies or no-ops
// it). One line per peer is returned for printing. The serve tick and the
// CLI consume this same primitive.
func SyncTaskPushOnly(taskID string) ([]string, error) {
	peers, err := config.ResolveSyncPeers(nil)
	if err != nil {
		return nil, err
	}
	return SyncTaskPushOnlyPeers(taskID, peers)
}

// SyncTaskPushOnlyPeers is SyncTaskPushOnly scoped to an EXPLICIT peer list
// (the CLI's --peer / --peers resolution, Rule 15: an admitted flag is
// obeyed, never ignored). Membership is validated against the DECLARED
// whitelist here — the same "sync topology is declared, never discovered"
// contract the session verbs enforce — so `--push-only --peer <undeclared>`
// is refused by name instead of silently pushing to everyone else.
//
// An empty list keeps the wrapper's long-standing meaning: no sync_peers
// declared is a named refusal, not a silent no-op (the caller asked for a
// push and nothing in the topology can receive it).
func SyncTaskPushOnlyPeers(taskID string, peers []string) ([]string, error) {
	taskID = taskIDNormalize(taskID)
	// THE DECLARED-TOPOLOGY GATE, ASKED FIRST — RequireDeclaredPeer owns the
	// refusal (peer_topology.go, Rule 14: one sentence for the whole family,
	// resolved from the declared sources when cliPeers is nil), and it is
	// asked BEFORE the empty-list refusal below so that a peer the node never
	// declared is refused as UNDECLARED however the list is shaped. This
	// block used to spell its own copy of the sentence and to check the list
	// length first, so `--peers <undeclared>` on a node with an empty
	// sync_peers was reported as "no sync_peers declared" — the wrong
	// diagnosis, and a second source of truth for the wording the literal
	// gate in peer_topology_test.go forbids.
	for _, p := range peers {
		// MERGE NOTE (rework run 01a0d7a3ca337b2ea0aeefaa289db1a4): the
		// archived branch's hunk at this site replaced a re-typed whitelist
		// sentence with declaredPeerError(p). master had already landed that
		// intent here in a STRONGER form, four lines up: RequireDeclaredPeer
		// owns the refusal AND resolves the declared topology itself, so the
		// caller needs no `declared` slice and cannot pass one it computed
		// wrong. master's line is therefore kept and the branch's hunk is
		// dropped — the branch's Rule 13 intent (one definition of the
		// sentence) is satisfied by master, not lost.
		if err := RequireDeclaredPeer(p, nil); err != nil {
			return nil, fmt.Errorf("memsync push: %w", err)
		}
	}
	if len(peers) == 0 {
		return nil, fmt.Errorf("memsync push: no sync_peers declared (settings.json %q / %s)", config.SyncPeersSettingsKey, config.SyncPeersEnv)
	}
	// No anchor window on a manual push: the peer's merge is idempotent
	// (echo rows no-op), and the caller explicitly wants THESE rows to
	// travel — the anchor gate would silently hide exactly the row being
	// re-pushed.
	//
	// The row-selection closure: with taskID set, the caller wants EXACTLY
	// that row (full export fallback included). It runs per peer in the loop
	// below because the M4.24 gate makes the export per-peer — each patch
	// excludes the rows THAT peer sent us, and one shared export cannot
	// express a per-peer exclusion.
	rowFilter := func(src *SyncTaskPatch) ([]SyncTaskRow, error) {
		if taskID == "" {
			return src.Rows, nil
		}
		rows := make([]SyncTaskRow, 0, 1)
		for _, r := range src.Rows {
			if r.TaskID == taskID {
				rows = append(rows, r)
			}
		}
		if len(rows) > 0 {
			return rows, nil
		}
		// The window gate is the silent-loss bug this tool exists to
		// counter — a row the anchor already passed is NOT FOUND in the
		// window yet is exactly what the caller asked for. Fall back to
		// a single-row full export (the claim-broadcast AfterSource=0
		// pattern; the peer's merge idempotently no-ops what it has).
		full, _, ferr := SyncTaskExportDelta(SyncTaskDeltaOptions{AfterSource: 0})
		if ferr != nil {
			return nil, ferr
		}
		for _, r := range full.Rows {
			if r.TaskID == taskID {
				rows = append(rows, r)
				break
			}
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("memsync push: task %s not found in the ledger (window AND full export)", taskID)
		}
		return rows, nil
	}
	lines := make([]string, 0, len(peers))
	for _, peer := range peers {
		patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{Peer: peer})
		if err != nil {
			lines = append(lines, fmt.Sprintf("push %s: export (fail-soft): %v", peer, err))
			continue
		}
		rows, err := rowFilter(patch)
		if err != nil {
			return nil, err
		}
		patch.Rows = rows
		body, err := json.Marshal(patch)
		if err != nil {
			lines = append(lines, fmt.Sprintf("push %s: marshal (fail-soft): %v", peer, err))
			continue
		}
		stdout, err := claimSSHExchange(peer, body)
		if errors.Is(err, errRemoteLegRetired) {
			// NOT "unreachable": nothing was attempted and nothing can be.
			// The manual push's line says the truth an operator can act on —
			// the lock leg is gone, so this command carries a row nowhere, and
			// the receipt-shaped wording ("LOCK mirrored …") would describe an
			// exchange that did not happen.
			lines = append(lines, fmt.Sprintf(
				"push %s: "+syncClaimRetiredMarker+" — no row crossed (the claim-broadcast leg is gone, so this "+
					"command no longer reaches a peer's ledger; a row that must run there is filed there — "+
					"Rule 28 / Rule 40): %v", peer, err))
			continue
		}
		if err != nil {
			lines = append(lines, fmt.Sprintf("push %s: unreachable (fail-soft): %v", peer, err))
			continue
		}
		var resp ClaimPushResponse
		if err := json.Unmarshal(stdout, &resp); err != nil {
			lines = append(lines, fmt.Sprintf("push %s: response: %v", peer, err))
			continue
		}
		if !resp.Accepted {
			lines = append(lines, fmt.Sprintf("push %s: REFUSED: %s", peer, resp.Reason))
			continue
		}
		lines = append(lines, syncClaimPushReceipt(peer, len(patch.Rows), taskID))
	}
	return lines, nil
}

// ── the manual push's RECEIPT, and why it had to change ─────────────────
//
// WHAT WENT WRONG (measured 2026-09-21 on zbook, re-measured 2026-10-12 from
// sager): the manual push rode `--claim-remote`, a CLAIM-BROADCAST handler
// that mirrors the row's LOCK and answers an accepted/refused VERDICT. It
// never stores the row — so `push zbook: accepted (1 row(s))` was TRUE about
// the LOCK and read as "the row arrived", and the operator's own follow-up
// found zbook's tasks table missing it entirely (`max(updated_at)` older than
// the row's creation). The GLOBAL policy paragraph on the client host pointed
// at exactly this command as "the way to get a row into zbook's queue", which
// turned a lock receipt into a persistence criterion (Rule 15b: prove the
// EFFECT, never the receipt).
//
// THE FIX IS WORDING, NOT SEMANTICS (constraint 3): --push-only IS the claim
// broadcast, the whole-window push and the serve tick share this primitive,
// and nothing about what crosses the wire changes here. What changes is that
// the line now NAMES which half moved, NAMES what did NOT happen, and hands
// the operator the two things the old sentence never did — the verb that DOES
// write the row, and a peer-side check they can paste.
//
// The peer's own verdict word is kept verbatim (the JSON `accepted` field is
// the peer's vocabulary, and dropping it would hide the exchange that did
// occur) but it is now quoted INSIDE a clause that binds it to the LOCK.
//
// EVERY value below is a named constant (Rules 1/13/14) — including the
// `--task-id` selector, which comes from cliverbs (the leaf package) because
// cmd/agent's parser spells the same word and a second literal here would be
// the mirror Rule 13 bans on the one string this line asks the operator to
// type.
const (
	// syncClaimPushReceiptFormat is the ONE spelling of the manual push's
	// per-peer line. Four facts, in the order the operator needs them: who
	// answered, how many rows the lock leg carried, that the LEDGER was not
	// written, and the two commands that settle the question.
	syncClaimPushReceiptFormat = "push %s: %s (%d row(s); the peer answered %q about the LOCK) — " +
		syncClaimNotStoredMarker + " (this leg is the CLAIM BROADCAST) | file the row there: %s | check the peer: `%s`"

	// syncClaimRetiredMarker is the manual push's verdict once the leg it rode
	// is gone: no row crossed, and the line must not borrow the receipt shape
	// (a "LOCK mirrored …" sentence would describe an exchange that did not
	// happen).
	syncClaimRetiredMarker = "RETIRED"

	// syncClaimLockMirroredMarker names WHAT actually crossed the wire. The
	// old line's verb ("accepted") described the LOCK and was read as the
	// ROW; this marker is the word the receipt leads with instead.
	syncClaimLockMirroredMarker = "LOCK mirrored"

	// syncClaimNotStoredMarker is the explicit absence — the half the old
	// sentence omitted, and the whole reason this epic exists.
	syncClaimNotStoredMarker = "the row is NOT in the peer's ledger"

	// syncClaimAcceptedVerdictWord is the peer's own verdict token (the
	// ClaimPushResponse `accepted` field). Kept because the exchange DID
	// happen and hiding it would be its own small lie — it is now quoted
	// inside the LOCK clause rather than standing alone as the headline.
	syncClaimAcceptedVerdictWord = "accepted"

	// syncClaimDepositRecipe is what the operator DOES about a row that is not
	// on the peer — and it is deliberately a SENTENCE, not a command line this
	// receipt can print.
	//
	// WHAT IT USED TO BE: `gopi task send --task-id <row>`, the cross-node row
	// leg. That verb is DELETED (cmd/agent/task_sync_verb_removal.go) and now
	// answers with a removal notice + exit 1, so the receipt was printing a
	// command that cannot run — a live face recommending a dead one, which is
	// the defect class this epic exists to close. There is no replacement VERB
	// to print, because the ability is gone rather than re-homed: a row that
	// must live on another ledger is FILED there (`gopi task submit` on that
	// host — Rule 28 / Rule 40), which needs that row's own briefing text and
	// therefore cannot be pre-rendered here.
	syncClaimDepositRecipe = "re-submit it ON that host (see the submit refusal, " +
		config.TaskSyncSettingsKey + ")"

	// syncClaimPeerRecheckFormat is the peer-side reader the operator runs to
	// settle it from the OTHER end — the same `task show` the claim broadcast
	// cannot satisfy (the peer's ledger has no such row). It is NOT declared
	// here: task_submit_forward.go already owns the ONE spelling
	// (taskSubmitConfirmVerb), and a second declaration would be the mirror
	// Rule 13 bans. The receipt references that constant directly, so the
	// check it recommends is byte-for-byte the check the submit forwarder
	// already proves a landed row with.

	// syncClaimRowPlaceholder is the row selector printed when the push was
	// the WHOLE delta window (no --task-id): a window has no single row to
	// name, and inventing one would print a command that resolves to nothing.
	syncClaimRowPlaceholder = "<row-id>"
)

// syncClaimPushReceipt renders the manual push's ONE per-peer line.
//
// It is a function rather than an inline Sprintf so the wording and the
// arguments that feed it live together (Rule 14: one owner), and so the
// peer-free regression test can drive the REAL renderer instead of re-typing
// its format string.
func syncClaimPushReceipt(peer string, rows int, taskID string) string {
	row := strings.TrimSpace(taskID)
	if row == "" {
		row = syncClaimRowPlaceholder
	}
	deposit := syncClaimDepositRecipe
	// The peer-side check travels through the peer's own login shell, so the
	// remote command is ONE quoted argument (harvestShellQuote — the same
	// quoting the row legs put on the wire, Rule 14: one escaper).
	recheck := sshCommandWord + " " + peer + " " + harvestShellQuote(fmt.Sprintf(taskSubmitConfirmVerb, row))
	return fmt.Sprintf(syncClaimPushReceiptFormat, peer, syncClaimLockMirroredMarker, rows,
		syncClaimAcceptedVerdictWord, deposit, recheck)
}

// PushTerminalTaskSync is the END-of-life must-succeed push (epic M4.19 ②,
// nick: "at end we are a bit aggressive — there is no reason to fail, must
// be done"): a task just reached a terminal status locally (completed /
// failed); its row is pushed to EVERY whitelisted peer via the claim
// channel's push-only path. Unlike the lock protocol (unreachable →
// retry/yield), this push RETRIES each peer on transient failure with
// exponential backoff, and reports success only when every peer accepted.
//
// Return contract (evaluated per peer):
//
//	ok        = every declared peer accepted (or none declared — nothing to
//	            push to is vacuous success; the start hook never ran sync
//	            either)
//	err       = ≥1 peer stayed unreachable/refused after all retries
//
// Pushing rides exportClaimPatch (the row's CURRENT verbatim state) — a
// terminal row is a forward move on any peer (applyNewer ladder), and the
// merge is idempotent, so a retry echo is harmless.
func PushTerminalTaskSync(taskID string) error {
	taskID = taskIDNormalize(taskID)
	// The task-row-sync switch (config.TaskSyncSettingsKey, default OFF):
	// with automatic task-row exchange off the terminal verdict stays on the
	// ledger that executed the row. `nil` is the SAME answer the no-peer arm
	// below returns, and it is deliberately not an error — the caller
	// (internal/agent's close-out) logs "terminal push: ok" and moves on,
	// which is exactly right: there was nothing to push TO. The manual faces
	// (`gopi task sync --push-only`, `task diff`, `task harvest`) never reach
	// this function, so the operator keeps the verb.
	if !config.TaskSyncEnabled() {
		return nil
	}
	peers, err := config.ResolveSyncPeers(nil)
	if err != nil {
		return fmt.Errorf("terminal push %s: resolve peers: %w", taskID, err)
	}
	if len(peers) == 0 {
		return nil // no sync topology → nothing to push (start hook behaved identically)
	}
	var lastErr error
	for attempt, delay := range terminalPushRetries {
		if attempt > 0 {
			time.Sleep(delay)
		}
		lastErr = pushTerminalToPeers(peers, taskID)
		if lastErr == nil {
			return nil
		}
		// A RETIRED PROTOCOL IS NOT A RETRYABLE FAILURE, and retrying it is not
		// harmless: the loop sleeps `terminalPushRetries` apart (measured 6.02s
		// on a faked peer) merely to re-learn the same fact, and then hands the
		// run's close-out a permanent FAILED. The peer cannot be reached because
		// the leg no longer exists — the SAME condition as `len(peers) == 0`
		// above, nothing to push TO — so it takes that arm's answer (nil) and
		// the terminal verdict stays on the ledger that executed the row.
		// (The SUBMIT forward is the opposite case and stays a loud refusal:
		// there the row would be LOST, here it is already recorded locally.)
		if errors.Is(lastErr, errRemoteLegRetired) {
			return nil
		}
	}
	return fmt.Errorf("terminal push %s: failed after %d attempt(s): %w", taskID, len(terminalPushRetries), lastErr)
}

// pushTerminalToPeers runs ONE push round: every peer must accept. A peer
// that refuses a terminal row is itself a bug signal (the forward ladder
// has no terminal-to-terminal refusals except state races the merge already
// tolerates) — reported as an error either way; the must-succeed contract
// only distinguishes "delivered" from "not delivered".
func pushTerminalToPeers(peers []string, taskID string) error {
	var failures []string
	for _, peer := range peers {
		// Per-peer export (M4.24 gate): each patch excludes rows THAT peer
		// sent us — one shared export cannot express a per-peer exclusion,
		// and a terminal verdict relayed back to its sender is the reflow.
		patch, err := exportClaimPatch(taskID, "", peer)
		if err != nil {
			return fmt.Errorf("export: %w", err)
		}
		patch.Generator = "terminal-push"
		body, err := json.Marshal(patch)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		stdout, err := claimSSHExchange(peer, body)
		if errors.Is(err, errRemoteLegRetired) {
			// Returned WRAPPED (%w), not flattened into the failure list: the
			// caller distinguishes "the peer said no" from "there is no protocol
			// left to say anything" with errors.Is, and a %v here would turn the
			// second into the first — the defect that made the claim classifier
			// yield and roll a live row back.
			return fmt.Errorf("terminal push %s: %w", peer, err)
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s unreachable: %v", peer, err))
			continue
		}
		var resp ClaimPushResponse
		if err := json.Unmarshal(stdout, &resp); err != nil {
			failures = append(failures, fmt.Sprintf("%s response: %v", peer, err))
			continue
		}
		if !resp.Accepted {
			failures = append(failures, fmt.Sprintf("%s REFUSED: %s", peer, resp.Reason))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d/%d peer(s) failed: %s", len(failures), len(peers), strings.Join(failures, "; "))
	}
	return nil
}

// notifyTerminalPushFailed is the alarm face of the must-succeed push
// (epic M4.19 ②): after every retry failed, the terminal-push-failed
// synthetic event goes through the SAME delivery pipeline as the status
// transitions (GOPI_TASK_NOTIFY_WEBHOOK / _CMD) — an abnormal signal, never
// silent. Mirrors taskheartbeat.go's notifyStaleTask precedent.
func notifyTerminalPushFailed(taskID string, pushErr error) {
	taskID = taskIDNormalize(taskID)
	cfg := loadTaskNotifyConfig()
	if !cfg.enabled() {
		return
	}
	if !cfg.events[taskNotifyEventTerminalPushFailed] {
		return
	}
	payload := taskNotifyPayload{
		Event:     taskNotifyEventTerminalPushFailed,
		TaskID:    taskID,
		Status:    taskStatusCompleted,
		Tags:      "sync-terminal-push-failed",
		Summary:   fmt.Sprintf("terminal state push to sync_peers failed after retries: %v", pushErr),
		Timestamp: time.Now().Format(time.RFC3339),
	}
	if cfg.async {
		go deliverTaskNotify(cfg, payload)
		return
	}
	deliverTaskNotify(cfg, payload)
}

// NotifyTerminalPushFailed is the exported wrapper (internal/agent cannot
// reach the unexported helper): deliver the alarm event for a failed
// terminal push.
func NotifyTerminalPushFailed(taskID string, pushErr error) {
	taskID = taskIDNormalize(taskID)
	notifyTerminalPushFailed(taskID, pushErr)
}
