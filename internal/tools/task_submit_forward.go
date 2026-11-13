package tools

// task_submit_forward.go — the LEDGER ADDRESS half of task_submit: which
// ledger a submit is allowed to write into.
//
// WHY THIS EXISTS (epic remote-submit-central-ledger): the task ledger is
// AUTHORITATIVE on one node (the declared central peer — config `sync_to`,
// read through ResolveNodeRole) and every remote leg (HP / RPi5) MIRRORS it.
// The submit engine used to write unconditionally into the PROCESS-LOCAL
// ledger (memoryDBPath() → ~/.gopi/memory.db), which on a compute leg is the
// mirror — so a `gopi task submit` issued there produced a row nobody would
// ever claim, notify or verify: a black hole. The write SUCCEEDED locally and
// the operator was told "Task queued: …", i.e. a silent failure of exactly
// the shape Rule 15b bans.
//
// The fix is an ADDRESS decision made once, before the row is inserted:
//
//   - the node IS the authoritative ledger (a declared server, or no
//     declared topology at all — the single-machine case) → insert locally,
//     byte-identical to the pre-change behaviour;
//   - the node is a CLIENT (a declared role other than server) → the row is
//     FORWARDED to the peer the node already reports to (sync_to), the peer
//     is asked to CONFIRM the row (its own `task show`), and the receipt is
//     ECHOED back into the local mirror ledger (so `task show` answers on
//     both nodes). A forward that cannot be delivered or confirmed — no peer
//     declared, ssh refused, the peer does not carry the row — is a LOUD
//     REFUSAL naming the authoritative target, the ledger path this process
//     would have written, and the remedy: never a silent node-local write.
//
// Everything after the insert (attachments, spill, bug tally, idempotent
// re-forward) reuses the existing primitives: the forward channel is the
// SESSION channel's push verb (the peer's `task sync --serve-remote`
// handler → SyncTaskImportDelta, which INSERTS an unknown row verbatim; the
// CLAIM verb does not — measured: its handler only writes a terminal ghost
// row, so a pending row pushed over it is accepted and stored nowhere), and
// the echo re-uses the same row bytes through the same import path.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/tasksource"
	"github.com/nick/gopi/internal/taskverbs"
)

// taskSubmitForwardKind marks the row a remote submit writes into the LOCAL
// mirror ledger: "this row's authoritative home is elsewhere". A client
// ledger carries the flag on every accepted forward, so the dispatcher can
// tell a mirror copy (never executed here) from locally-authored work —
// the node that declares itself a client does not own its queue.
const taskSubmitForwardKind = "task-submit-forward"

// taskSubmitForwardedByPrefix tags the MIRROR copy written by the
// forwarding node ("this row travelled to <peer>"). It namespaces the
// forward provenance on the ECHO half only — the wire row that reaches the
// authoritative ledger carries no such tag (the peer owns the row; a tag
// describing a journey the peer never made would be a lie in its ledger).
const taskSubmitForwardedByPrefix = "forwarded-to:"

// taskSubmitForwardTimeout bounds ONE forward leg (the import round trip,
// the confirmation, the echo). It is deliberately roomier than the claim
// protocol's budget: a submit is a human/LLM-facing action, so waiting beats
// a retry loop (the retry is the operator's — the refusal names what failed).
const taskSubmitForwardTimeout = 30 * time.Second

// taskSubmitConfirmVerb is the peer-side CONFIRMATION: the peer's own
// `gopi task show <id>` must exit 0 and print the row's task_id. A silently
// dropped patch is the exact failure the confirmation exists to catch — the
// ssh leg answering 0 with an empty envelope would otherwise pass as a
// successful forward (Rule 15b: prove the row landed, never assume).
//
// Composed from the existing constants, never re-typed (Rule 1b): the remote
// BINARY is syncRemoteGopi (the ONE spelling the ssh transport uses) and the
// family verb is taskverbs.Show. The GROUP verb ("task") has no importable
// constant — cmd/agent's CliVerbTask is unimportable from here — so it is
// declared once, next to its single use.
const taskSubmitConfirmVerb = syncRemoteGopi + " " + taskSubmitGroupVerb + " " + taskverbs.Show + " %s"

// taskSubmitGroupVerb is the `gopi task` group verb as this package spells it
// on the wire (the peer's own CLI parses it).
const taskSubmitGroupVerb = "task"

// taskSubmitForwardSource is the Source stamped on the LOCAL MIRROR ECHO —
// this node's copy of a row whose authoritative home is the peer's ledger —
// and it is the value the retired alias "auto" used to stand for here.
//
// THE ROLE, DECIDED (this replaces the stale pre-be17a9bd reasoning, which
// argued the value only had to be "a source", not the right one):
//
//   - the AUTHORITATIVE row (the wire row) carries the SUBMITTER'S OWN role —
//     opts.Source, already resolved by TaskSubmit's funnel into ledger
//     vocabulary (an LLM's task_submit is `agent`, the operator's typed
//     `gopi task submit` is `human`, a worker's follow-up row is `worker`).
//     Forwarding moves a row's HOME, never its authorship, and the peer's
//     ledger is where the row is claimed, run and adjudicated: stamping the
//     TRANSPORT there would hand the receiving ledger a role that answers
//     none of the questions provenance is read for. The journey is recorded
//     where it belongs — in the TAGS (taskSubmitForwardKind rides both
//     copies, taskSubmitForwardedByPrefix rides the local one).
//   - the ECHO row (the local mirror copy) is stamped with THIS constant,
//     tasksource.Forward: the copy travelled here, and in THIS ledger that is
//     the fact worth rendering. The old value could not say it — "auto"
//     resolves (tasksource.Normalize) to `merger`, so every forwarded submit
//     was narrated as a merge-path machine enqueue, both on the mirror and on
//     the authoritative ledger.
//
// The constant keeps the property the old comment claimed for it, on the copy
// where the claim is true: the mirror's own provenance is distinguishable
// (internal/tasksource's vocabulary, referenced, never re-typed — Rule 1b/14).
const taskSubmitForwardSource = tasksource.Forward

// taskSubmitRefusalFmt is the ONE refusal wording (Rule 14): the submitter
// must learn (a) that the row was NOT written locally, (b) which ledger is
// authoritative, (c) the exact path this process would have written, and
// (d) the remedy. Every refusal arm renders through this format.
const taskSubmitRefusalFmt = "task_submit: REFUSED — this row was NOT written to any local ledger. " +
	"Authoritative ledger: %s (%s). Local ledger this process would have written (the node MIRROR): %s. " +
	"Reason: %s. Remedy: there is NOTHING on this node to repair — the forward leg is retired and no setting revives it, " +
	"so the row is submitted ON the authoritative node instead (`gopi task submit \"<briefing>\"` there, which stamps that " +
	"host's own repo and ledger — Rule 28 / Rule 40). A node that must stop behaving as a client MIRROR clears %s."

// taskSubmitNeedsForward reports whether THIS process stands on a mirror leg
// AND this node has been told to exchange task rows automatically. It is the
// resolved-node-identity predicate, not a topology guess (Rule 2): the
// authoritative side is the node that declared itself `server`, or a node
// that declared no topology at all (the single-machine case, where the local
// ledger IS authoritative).
//
// THE SWITCH IS THE SECOND HALF OF THE ANSWER (epic: the five conflated
// axes). `role=client` is IDENTITY — it decides conflict arbitration and
// whose finish-cleanup branch push-back runs — and it is NOT a statement
// that this node's ledger is a remote mirror: that is collaboration, and it
// is declared by config.TaskSyncEnabled, default OFF. Before this switch the
// two were welded, so declaring a node a client removed its ability to
// submit locally: the row was forwarded, or (with no sync_to) refused. A
// client node whose operator has not turned task sync on now submits into its
// OWN ledger, which is what a single node's ledger is — the row it will
// actually claim and run.
func taskSubmitNeedsForward() bool {
	if !config.TaskSyncEnabled() {
		return false
	}
	role, _ := config.ResolveNodeRole()
	switch role {
	case config.NodeRoleServer:
		return false
	case "":
		// Undeclared role = single-machine default. The LEGACY hostname
		// fallback of IsServerNode is deliberately NOT consulted: a
		// hostname that merely happens to equal a sync_peers entry would
		// silently turn a real forward into a local write — the exact
		// defect this file exists to remove.
		return false
	case config.NodeRoleClient:
		return true
	default:
		// Any other declared role is still "not the authoritative node":
		// ResolveNodeRole validates against the known set, so this arm is
		// future-proofing, never the current spelling path.
		return true
	}
}

// taskSubmitForward inserts the submitted row into the AUTHORITATIVE
// ledger at peer and returns the row's receipt line. The row travels as a
// session-shaped v2 patch, carried by the SAME verb the mirror ledgers
// already speak, so nothing new is parsed on either side.
func taskSubmitForward(opts TaskSubmitOptions, repo string, attManifest []TaskAttachment, localDBPath, peer string) (string, error) {
	row := taskSubmitForwardRow(opts, repo, attManifest)
	// The wire row's provenance (the SUBMITTER's role) is snapshotted before
	// the echo re-stamps the copy — both lines are reported, never conflated.
	wireSource := row.Source
	patch := SyncTaskPatch{
		Version:    syncPatchVersionDelta,
		Node:       claimBroadcastNodeName(),
		ExportedAt: time.Now().Unix(),
		Generator:  taskSubmitForwardKind,
		Rows:       []SyncTaskRow{row},
	}
	body, err := json.Marshal(&patch)
	if err != nil {
		return "", taskSubmitForwardRefusal(localDBPath, peer, fmt.Errorf("encode row: %w", err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskSubmitForwardTimeout)
	defer cancel()
	if _, err := forwardPatchToPeer(ctx, peer, body); err != nil {
		return "", taskSubmitForwardRefusal(localDBPath, peer, err)
	}
	// The peer must SHOW the row before this submit claims success: the
	// import's answer is an envelope, and the row's presence is the only
	// evidence that the queue that will actually run it carries it.
	if err := confirmTaskForwardedToPeer(ctx, peer, row.TaskID); err != nil {
		return "", taskSubmitForwardRefusal(localDBPath, peer, err)
	}
	// The echo: the SAME row through the shared import path, tagged as this
	// node's copy and re-stamped with the MIRROR's own role. The local
	// mirror is a mirror (importTaskTags stamps the provenance channel; the
	// forward tag says WHERE the row lives), and the round-trip is what
	// keeps `task show` answering here. The source is the one field that
	// differs from the wire row: the peer's ledger already carries the
	// submitter's role, while in THIS ledger the fact worth recording is
	// that the copy was forwarded (taskSubmitForwardSource).
	row.Tags = append(row.Tags, taskSubmitForwardedByPrefix+peer)
	row.Source = taskSubmitForwardSource
	echo := SyncTaskPatch{
		Version:    syncPatchVersionDelta,
		Node:       taskSubmitForwardEchoNode(peer),
		ExportedAt: time.Now().Unix(),
		Generator:  taskSubmitForwardKind,
		Rows:       []SyncTaskRow{row},
	}
	// The echo's stats are READ, not assumed (Rule 15b): a refused echo row
	// means the local mirror rejected a row the peer already accepted, and a
	// mirror that silently lacks the row is the same class of defect as the
	// black hole this file removes — the operator must hear about it (the
	// peer DOES carry the row, so this is a warning, never a refusal).
	stats, err := SyncTaskImportDelta(&echo)
	if err != nil {
		return "", taskSubmitForwardRefusal(localDBPath, peer,
			fmt.Errorf("row reached %s, but the LOCAL mirror echo failed: %w", peer, err))
	}
	if stats.RowsRefused > 0 {
		fmt.Fprintf(os.Stderr, "task_submit: %s — %d row(s) refused; local mirror %s may not carry this row (peer %s does)\n",
			row.TaskID, stats.RowsRefused, localDBPath, peer)
	}
	// The receipt names BOTH provenances (Rule 15b: two ledgers, two facts):
	// the authoritative row carries the submitter's role, the local mirror
	// copy the forward role. One value here would misdescribe one of them.
	return fmt.Sprintf("Task queued: %s (status=%s, source=%s, mirror_source=%s, tags=%s, ledger=%s)",
		row.TaskID, taskStatusPending, wireSource, row.Source, orDash(taskSubmitForwardTags(opts)), peer), nil
}

// taskSubmitForwardEchoNode names the echo patch's "peer". The import path
// stamps provenance as from-<patch.Node> and REFUSES an empty node — the
// forwarding node's OWN name is the honest provenance here (the copy did
// originate on this node, it merely lives elsewhere).
func taskSubmitForwardEchoNode(peer string) string {
	if self := strings.TrimSpace(claimBroadcastNodeName()); self != "" && self != syncTaskUnknownNode {
		return self
	}
	return peer
}

// taskSubmitForwardRow renders the row that travels. Status stays PENDING:
// the row's authoritative home has not executed it, and the peer's verb
// inserts an unknown row exactly as sent (the terminal arm's status switch
// defaults a pending row through unchanged). claimed_by/claimed_at are not
// part of the wire row (SyncTaskRow carries the work definition only).
//
// Source is the SUBMITTER'S role (opts.Source, resolved by TaskSubmit's
// funnel into ledger vocabulary), never a transport value: the authoritative
// ledger is where this row will be claimed and run, and its provenance line
// must answer "who filed this" — see taskSubmitForwardSource for the
// decision. The local echo is the copy that is re-stamped.
func taskSubmitForwardRow(opts TaskSubmitOptions, repo string, attManifest []TaskAttachment) SyncTaskRow {
	now := time.Now().Unix()
	return SyncTaskRow{
		TaskID:             opts.TaskID,
		Summary:            opts.Summary,
		Payload:            opts.Payload,
		Tags:               splitSources(taskSubmitForwardTags(opts)),
		Source:             opts.Source,
		Status:             taskStatusPending,
		CreatedAt:          now,
		UpdatedAt:          now,
		Deliverable:        taskDeliverableSubmitJSON(repo, attManifest),
		DeliverableVersion: deliverableVersionKnown,
	}
}

// taskSubmitForwardTags carries the row's own tags plus the declared-forward
// provenance. It is used for the wire row AND for the echo, so both ledgers
// carry the same tags.
func taskSubmitForwardTags(opts TaskSubmitOptions) string {
	return normalizeTaskTags(opts.Tags + "," + taskSubmitForwardKind)
}

// taskSubmitForwardPeer resolves the authoritative peer alias for a forward
// (config `sync_to`, which ResolveNodeRole merges: env GOPI_SYNC_TO >
// settings.json "sync_to" — Rule 2, one reader).
func taskSubmitForwardPeer() string {
	_, syncTo := config.ResolveNodeRole()
	return strings.TrimSpace(syncTo)
}

// taskSubmitForwardRefusal renders the ONE refusal wording (Rule 15b: the
// would-be write path is NAMED, never silently taken) and returns it as the
// error the submit faces return. It also records the refusal on stderr, so
// an internal auto-submit caller (which logs the returned error) and a CLI
// caller (which prints os.Exit's message) both surface the same sentence.
func taskSubmitForwardRefusal(localDBPath, peer string, cause error) error {
	target := strings.TrimSpace(peer)
	if target == "" {
		target = "NONE DECLARED"
	}
	reason := "forward to the authoritative ledger failed"
	if cause != nil {
		reason = cause.Error()
	}
	err := fmt.Errorf(taskSubmitRefusalFmt, target, config.SyncToSettingsKey, localDBPath, reason,
		config.SyncToSettingsKey)
	fmt.Fprintf(os.Stderr, "%v\n", err)
	return err
}

// taskSubmitAuthority is the resolved ledger address for THIS process: ok =
// the row may be written locally; peer = the authoritative peer to forward
// to (non-empty only when a forward is required).
func taskSubmitAuthority(localDBPath string) (peer string, needForward bool) {
	if !taskSubmitNeedsForward() {
		return "", false
	}
	return taskSubmitForwardPeer(), true
}

// guardTaskSubmitLedgerAddress is the gate the submit engine calls before it
// opens/creates the local ledger: on a mirror leg it either resolves a
// forwardable peer or refuses LOUDLY. Returning an error here is the point —
// the ledger is NOT touched, so no black-hole row can be created.
func guardTaskSubmitLedgerAddress(localDBPath string) (string, error) {
	peer, need := taskSubmitAuthority(localDBPath)
	if !need {
		return "", nil
	}
	if peer == "" {
		return "", taskSubmitForwardRefusal(localDBPath, "",
			fmt.Errorf("this node declares role=%s (a mirror leg) but no authoritative peer is configured (%s / %s)",
				string(config.NodeRoleClient), config.SyncToSettingsKey, config.SyncToEnv))
	}
	if _, err := ValidateSSHPeerAlias(peer); err != nil {
		return "", taskSubmitForwardRefusal(localDBPath, peer, fmt.Errorf("peer alias: %w", err))
	}
	return peer, nil
}

// taskSubmitLocalIDs samples the local ledger's pre-existing task ids so a
// forward mints an id that COLLIDES WITH NEITHER LEDGER. The local ledger is
// opened as late as possible (never created just to sample) and a failure is
// loud: without the sample the minted id could collide here after the peer
// accepted it.
func taskSubmitLocalIDs(ctx context.Context, localDBPath string) (map[string]bool, error) {
	if _, err := os.Stat(localDBPath); err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, taskSubmitForwardRefusal(localDBPath, taskSubmitForwardPeer(),
			fmt.Errorf("inspect the local mirror ledger: %w", err))
	}
	db, err := openTasksDB(ctx, localDBPath)
	if err != nil {
		return nil, taskSubmitForwardRefusal(localDBPath, taskSubmitForwardPeer(),
			fmt.Errorf("open the local mirror ledger: %w", err))
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT task_id FROM tasks`)
	if err != nil {
		return nil, taskSubmitForwardRefusal(localDBPath, taskSubmitForwardPeer(),
			fmt.Errorf("read the local mirror ledger: %w", err))
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, taskSubmitForwardRefusal(localDBPath, taskSubmitForwardPeer(),
				fmt.Errorf("scan the local mirror ledger: %w", err))
		}
		ids[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, taskSubmitForwardRefusal(localDBPath, taskSubmitForwardPeer(),
			fmt.Errorf("read the local mirror ledger: %w", err))
	}
	return ids, nil
}

// taskSubmitMintForwardID mints a uuid v7 task id that the LOCAL mirror
// ledger does not already carry. The peer side needs no probe: a uuid v7 is
// collision-free by construction, and the only id the peer could already
// hold is the SAME row from an earlier attempt of this very submit (whose
// echo the local dedup absorbs). The local sample is the half that can
// actually collide, and the echo insert enforces it again (UNIQUE).
func taskSubmitMintForwardID(localIDs map[string]bool) (string, error) {
	for attempt := 0; ; attempt++ {
		id, err := uuid.NewV7()
		if err != nil {
			return "", fmt.Errorf("task_submit: mint task id: %w", err)
		}
		cand := id.String()
		if !localIDs[cand] {
			return cand, nil
		}
		if attempt >= taskIDMaxAttempts {
			return "", fmt.Errorf("task_submit: could not mint a unique task_id in the local mirror ledger after %d attempts", taskIDMaxAttempts)
		}
	}
}

// taskSubmitLedgerTargets reports the two ledger paths a submit crosses on a
// mirror leg — the authoritative peer and the local mirror — for the
// confirmation line, so no reader has to guess which ledger owns the row.
func taskSubmitLedgerTargets(localDBPath, peer string) string {
	return fmt.Sprintf("authoritative=%s (%s), local mirror=%s", peer, config.SyncToSettingsKey, localDBPath)
}

// --- small seams over the existing sync primitives -------------------------

// forwardPatchToPeer carries one patch to the peer's SESSION handler under a
// caller-supplied context. The session verb is the one that CREATES a row the
// peer has never seen (SyncRemoteServe → SyncTaskImportDelta → insert); the
// claim verb would NOT: its handler only inserts a TERMINAL ghost row (a
// lock verb must never conjure a pending row), so forwarding a pending
// submit over it silently accepted the patch and stored nothing.
//
// The ssh argv shape mirrors syncSSHExchange exactly (BatchMode, connect
// timeout, ONE remote-command element — the quoted form is load-bearing).
func forwardPatchToPeer(ctx context.Context, peer string, body []byte) ([]byte, error) {
	// Peer-alias guard (option injection): the alias travels as an ssh argv
	// element below — a leading "-" would be parsed as an OPTION.
	peer, err := ValidateSSHPeerAlias(peer)
	if err != nil {
		return nil, fmt.Errorf("task_submit forward: %w", err)
	}
	if taskSubmitForwardSeam != nil {
		// Test seam (the syncSSHExchangeHook pattern): static.
		return taskSubmitForwardSeam(peer, body)
	}
	if taskSubmitForwardHook != nil {
		// Test seam, dynamic-resolver form.
		return taskSubmitForwardHook(peer, body)
	}
	// THE PRODUCTION PATH IS GONE, and this is where it used to be. The leg
	// used to compose `ssh <peer> gopi task sync --serve-remote`; that verb is
	// DELETED (2026-11-05) and answers with a removal notice + exit 1, so
	// dialling would have turned a removal into a protocol error at the far
	// end of a connection. The refusal happens HERE, before any ssh argv
	// exists (remote_leg_retired.go states the mechanism once).
	return nil, remoteLegRetired("task submit forward", peer)
}

// confirmTaskForwardedToPeer asks the PEER to show the row over ssh — the
// evidence that the authoritative queue carries it. The answer is a raw
// `gopi task show` rendering at an already-tested layer (the string a test
// fabricates for the confirmation leg is the SAME line the real binary
// prints, so the seam consumes the string and not a struct), which is why
// the consumer is a named predicate rather than a parse.
func confirmTaskForwardedToPeer(ctx context.Context, peer, taskID string) error {
	remote := fmt.Sprintf(taskSubmitConfirmVerb, taskID)
	if taskSubmitConfirmSeam != nil {
		return taskSubmitConfirmSeam(peer, remote, taskID)
	}
	// The confirmation rode the same ssh transport the forward used, so it is
	// retired with it: asking a peer to `task show` a row this binary can no
	// longer hand it would be a second connection discovering the same
	// removal. `remote` is still composed for the seam (a test asserts the
	// exact confirmation argv), and never reaches an argv of its own.
	return remoteLegRetired("task submit confirm", peer)
}

// taskSubmitConfirmationMissing reports an error when the peer's `task show`
// answer does not carry the row (an empty answer, or a "no task with
// task_id" refusal): the forward's success is the row's PRESENCE, never the
// ssh leg's exit code.
func taskSubmitConfirmationMissing(peer, taskID, answer string) error {
	if strings.Contains(answer, taskID) {
		return nil
	}
	return fmt.Errorf("peer %s does not carry task %s after the import (answer: %q)", peer, taskID, strings.TrimSpace(answer))
}

// taskSubmitForwardSeam / taskSubmitForwardHook / taskSubmitConfirmSeam
// replace the forward transport when non-nil (tests run the peer's real
// handler in-process). Package-level like their siblings: a forward is
// process-global state. Seams are declared here and set ONLY by tests.
var (
	taskSubmitForwardSeam func(peer string, body []byte) ([]byte, error)
	taskSubmitForwardHook func(peer string, body []byte) ([]byte, error)
	taskSubmitConfirmSeam func(peer, remote, taskID string) error
)
