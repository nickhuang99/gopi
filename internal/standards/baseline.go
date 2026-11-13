package standardgate

import "strings"

// Baselines for the machine-checkable gates in gate.go.
//
// HOW TO READ: each entry is an exact `file:line` of a violation that ALREADY
// existed when its gate landed. It is a permit, not an exemption of the file:
// the same violation on any other line is reported. A gate reports two kinds of
// error — a NEW violation (fix it, or add the line here with a reason) and a
// STALE entry (the site was fixed/moved; delete the entry so the gate ratchets
// down). Never replace an entry with a file glob.
//
// The entries are keyed by line, so they are regenerated whenever a gated file
// is edited above a listed line. That regeneration is intentional: it forces a
// human to look at each site while re-baselining.

// errorWrapBaseline: fmt.Errorf sites that render a wrapped error with %v
// (PROJECT.md Rule 16 / checklist 2.2).
// Allow-list reason: the `%w` migration HAS run (branch feat/style-sweep,
// devdoc/style-sweep-20260912.md — 352 sites absorbed, this list pruned from
// 352 to its current size). What remains are ENUMERATED nil-guarded sites: the
// value is nil exactly where it must stay out of the chain
// (`if err != sql.ErrNoRows { … }`, `if err != nil && err != sql.ErrNoRows`), so
// %w would render `%!w(<nil>)` and break the message. These are a deliberate
// KEEP, not unfixed debt. A NEW %v wrap is a defect, not a baseline entry.
//
// 9 of these 10 entries are ALSO reported by the type-aware stylegate
// (its count: 9). The extra one is the cmd/agent/main.go mentionsError false
// positive that this gate keeps on purpose: the call already wraps with %w and
// its two %v verbs format the payloadFilePrefix STRING constant — mentionsError
// flags it only because an argument is named `err`. The 10-vs-9 split is
// documented in devdoc/style-sweep-20260912.md.
//
// UPDATED (2026-09-18, feat/merger-takeover-identity-stats): that false
// positive is GONE — the site's two string verbs became %s, so the gate no
// longer reports it and its permit is deleted from the list below. The count is
// now 9 entries; the 10-vs-9 paragraph above is kept as the record of the split
// it described.
//
// MEASURED AT THE TIME OF WRITING — historical, NOT a statement about later
// trees. The row below was printed by THIS list as it stood in commit
// 0153132f6 (2026-09-18, the commit that wrote this paragraph), where the
// block held 6 permit keys:
//
//	go test ./internal/standards/ -run 'TestStandardGates/TestGate1ErrorWrapRedProof'
//	→ Rule 16: 7 total hits, 6 baselined, 1 escaped, 0 new, 0 stale (exit 0).
//
// CORRECTED (2026-10-13, task/01a135abdfbf74439b2ee6a1d91235b0) — the row above
// was quoted for a while as if it described the live tree, which it cannot: the
// ratchet has since moved DOWN by four keys, so the same command prints a
// DIFFERENT row. Anything quoting "6 baselined" is tracing the 2026-09-18
// reading, not any ref reachable today (cf21ceb4e / 795f10044 / master all
// carry the byte-identical 2-key block:
// `git show <rev>:internal/standards/baseline.go | awk '/^var errorWrapBaseline/,/^}/' | sha256sum`
// → 683b7afb1f4e1973 for all three). The four retired keys, and the fixes that
// retired them, are memory_procedural.go:437/:456/:474 (99a100d94, 2026-09-28)
// and memory_sync_claim.go:912 (1fed197f8, 2026-10-05): 6 keys → 3 → 2.
// MEASURED on master aed220d5b (2026-10-13, this worktree, HEAD == master):
//
//	go test -count=1 -v -run 'TestStandardGates/TestGate1ErrorWrapRedProof' ./internal/standards/
//	→ Rule 16 (%w error wrapping): 3 total hits, 2 baselined, 1 escaped, 0 new (unannotated), 2 baseline entries  (exit 0)
//
// Both the numbers AND the column names move: the trailing column here is an
// ENTRY count, not the old "0 stale" tally (devdoc/standards-gate-accounting.md).
// So never read a count out of this comment as if it described the live tree —
// run the command above. (The paragraph above this block quotes "9 entries"
// while the block it annotates held 6 at that same commit: the prose in this
// header drifts the same way, and the block is the only measurable part.)
//
// RE-KEY (2026-09-12, feat/pa-never-runs-tasks): the site moved 515 → 681 —
// the agent-spawn guard added lines to cmd/agent/main.go ABOVE it (the
// guardVerbOrExit call in isWorkerSubcommand), and the merge of master into
// this branch added more. The violation is unchanged; only its line-key is.
// The same is true of internal/tools/memory_sync_claim.go (890 → 912): master
// edited the package ABOVE that site while this branch was in flight, so the
// multi-line terminal-push wrap sits further down. Both keys are therefore
// updated together because the gate fails BOTH ways: a NEW report with no
// entry, AND an entry matching nothing (a stale permit — a silent permission).
//
// RE-KEY (2026-09-19, fix/standards-baseline-repair, base 9e6d7844): the
// memory_sync_claim.go key is re-pointed 912 → 917 — the same drift again. The
// drift is 9155fa11 (the branch change the merge 26cc53e5 landed), which added
// lines to internal/tools/memory_sync_claim.go ABOVE the site. Proof of identity
// (not a guess about lines) — the same statement, byte-identical, on the tree
// the permit was written for (61b8d983, the last commit before the drift) and
// on 9e6d7844:
//
//	git show 61b8d983:internal/tools/memory_sync_claim.go | grep -n 'terminal push %s: failed'
//	→ 912
//	grep -n 'terminal push %s: failed' internal/tools/memory_sync_claim.go   # 9e6d7844
//	→ 917
//
// The two lines are byte-identical; only the key moved. Nothing became
// compliant, so this is a re-point and not a deletion.
//
// RE-KEY (2026-09-21, feat/ledger-agreement-precondition, base 96325d30): the
// key is re-pointed 917 → 938 — the third drift on this same site, and the
// same mechanism: the branch added the claim-protocol TRANSPORT SEAM
// (claimSSHExchangeHook + SetClaimSSHExchangeHookForTest, 21 lines) to
// internal/tools/memory_sync_claim.go ABOVE the site, so the multi-line
// terminal-push wrap sits further down. Proof of identity (not a guess about
// lines) — the same statement, byte-identical, on the tree the permit was
// written for and on this branch:
//
//	git show 96325d30:internal/tools/memory_sync_claim.go | grep -n 'terminal push %s: failed'
//	→ 917
//	grep -n 'terminal push %s: failed' internal/tools/memory_sync_claim.go   # this branch
//	→ 938
//
// The two lines are byte-identical; only the key moved. Nothing became
// compliant, so this is a re-point and not a deletion.
//
// PRUNE (2026-09-21, feat/task-sync-switch-off): the permit is DELETED, not
// re-keyed — this time the site really did become compliant. The
// terminal-push retry-exhaustion wrap was converted to %w (the same fix the
// two internal/tools/memory.go permits above went through), so the entry
// matched no violation and the gate reported it STALE. A permit that matches
// nothing is a silent permission and must not stay:
//
//	grep -n 'terminal push %s: failed' internal/tools/memory_sync_claim.go
//	→ 1058: return fmt.Errorf("terminal push %s: failed after %d attempt(s): %w", …)
//
// The rule permits may only RATCHET DOWN (this file's own contract), and this
// is that ratchet acting.
var errorWrapBaseline = []string{
	// The two former internal/tools/memory.go permits (dedup check /
	// memory_feedback) were pruned when those two sites were converted to %w
	// during the t-mem-flake work — a permit that matches no violation is a
	// silent permission, so it must not stay. Master's entries for the same
	// two sites (memory.go:2191 / :2802) were re-keyed by master's memory.go
	// rewrite and likewise match no violation in the merged tree, so they go
	// with them; the intent (no stale permits) is preserved on both sides.
	// re-key (merge fix/merger-top-level-verb into master): master's promoted
	// `gopi merger` help/const block inserted lines above this site, shifting
	// it 646→675. Same site, same reason (the call already wraps with %w; its
	// two %v verbs format the payloadFilePrefix STRING constant).
	// re-key: the top-level `gopi merger --status` guard (isMergerSubcommand,
	// fix/merger-status 3cea736d) added its argv short-circuit above this
	// site, shifting it 675→685. Same site, same reason.
	// re-key (2026-09-12, merge fix/merger-status into master): the merged tree
	// carries BOTH sides' insertions — master's `gopi merger` block AND this
	// branch's `gopi merger --status` guard, on top of master's agent-spawn
	// guard — so the site sits below BOTH recorded keys (HEAD 685, master 681)
	// at 691. Same site, same reason (the call already wraps with %w; its two
	// %v verbs format the payloadFilePrefix STRING constant). The merged line
	// key is the one below; the gate fails BOTH ways (a NEW report with no
	// entry AND an entry matching nothing), so it is re-keyed rather than
	// relaxed.
	// The live key for that site in THIS merged tree (verified by
	// `grep -n 'escape a literal prompt' cmd/agent/main.go` → 829):
	//
	// re-key (2026-09-12, fix/worker-unknown-flag): the `gopi worker` unknown-
	// argument REFUSAL (isWorkerSubcommand) replaced the bare `-h`/`--once`
	// literals with the named workerFlag* constants and added the refusal
	// helper + its doc — all of it ABOVE this site, shifting it 691→753. Same
	// site, same reason (the call already wraps with %w; its two %v verbs
	// format the payloadFilePrefix STRING constant); the line-key moved, the
	// violation did not.
	//
	// re-key (2026-09-12, fix/worker-session-cwd): this branch added the
	// SESSION WORKDIR bootstrap (14 lines in main's env-flag block) and the
	// FRONT-RUN CONSOLE LOG attach (15 lines after printLogPaths) — BOTH above
	// this site, shifting it 753→782. Same site, same reason (the call already
	// wraps with %w; its two %v verbs format the payloadFilePrefix STRING
	// constant); the line-key moved, the violation did not.
	//
	// merge-note (2026-09-12, merge fix/worker-session-cwd into master):
	// HEAD keyed this site at 782 (its worker-unknown-flag + worker-session-cwd
	// re-keys carried it 691→753→782), while master keyed it at 800 (its
	// graceful-restart control surface — `gopi merger --stop/--restart/--start`
	// inserted into isMergerSubcommand and mergerUsageHead, plus the same
	// `gopi worker` unknown-argument REFUSAL — sat ABOVE the site). The merged
	// tree carries BOTH sides' insertions, so BOTH recorded keys are stale
	// there — the same unchanged call (it already wraps with %w; its two %v
	// verbs format the payloadFilePrefix STRING constant) now sits at 829.
	// One merged key carries both intents; the gate fails BOTH ways (a NEW
	// report with no entry AND an entry matching nothing — a silent
	// permission), so it is re-keyed rather than relaxed, and neither parent's
	// stale permit is kept.
	//
	// re-key (2026-09-13, feat/signal-provenance): the signalrec.Install call
	// plus its rationale comment were added ABOVE this site in main(), shifting
	// it 846→860 (the site is the SAME unchanged call; its two %v verbs format
	// the payloadFilePrefix STRING constant, and no error is being wrapped
	// there — %w is impossible and was never intended). The key is re-keyed,
	// not relaxed: the gate still fails on a new %v wrap anywhere else.
	//
	// merge-note (2026-09-13, merge fix/tmpdir-settings into master): HEAD
	// keyed this site at 879 (its tmpdir-settings injection — two call sites,
	// one before the subcommand chain and one after the agent path's settings
	// load), while master keyed it at 913 (fix/daemon-cgroup-detach's
	// `--require-scope` refusal shifted 860→882, then
	// feat/worker-daemon-singleton's worker control routing + resident-daemon
	// help block shifted 882→913). The merged tree carries BOTH sides'
	// insertions, so BOTH recorded keys are stale: the SAME unchanged call
	// (it already wraps with %w; its two %v verbs format the payloadFilePrefix
	// STRING constant — the literal per cent verbs deliberately describe how
	// to escape a payload, they wrap nothing) now sits at 932, verified by
	// `grep -n 'escape a literal prompt' cmd/agent/main.go` in the merged tree.
	//
	// re-key (2026-09-13, fix/worker-status-stop-conflict): the read+act
	// refusal (F1, devdoc/worker-daemon.md) added ONE guard block to
	// isWorkerSubcommand and one to isMergerSubcommand — both ABOVE this site
	// in main() — shifting its key 940→955. The site is unchanged (the same
	// %w call whose two %v verbs format the payloadFilePrefix STRING
	// constant). The key is re-keyed with its reason, never relaxed or
	// deleted: the gate still fails on the stale entry AND on a new %v wrap.
	// One merged key carries both intents: the gate fails BOTH ways (a NEW
	// report with no entry AND an entry matching nothing — a silent
	// permission), so it is re-keyed rather than relaxed, and neither parent's
	// stale permit is kept.
	//
	// merge-note (resolve merge fix/rawlog-test-home-leak into master): BOTH
	// sides re-keyed this ONE permit for the SAME unchanged site, and both
	// recorded keys are stale in the MERGED tree, so neither parent's number
	// is kept. The site (the %w call whose two %v verbs format the
	// payloadFilePrefix STRING constant) is located ONCE, mechanically, in the
	// merged tree:
	//   grep -n 'escape a literal prompt' cmd/agent/main.go  ->  1035
	// HEAD keyed it at 961 assuming only master's 4a896cc3 daemonFlagLong arm
	// had shifted main.go; the merged tree carries that arm AND every
	// other master-side insertion, so 961 matches nothing. master's block
	// below already reconciles its own chain of shifts and lands on 1035,
	// which is the number the merged tree actually has. Re-keyed with its
	// reason, never relaxed and never deleted: the gate still fails on a new
	// %v wrap anywhere else AND on the stale entry, so the ratchet stays
	// honest. HEAD's rationale, kept for audit and corrected to the merged
	// key:
	// re-key (2026-09-13, master 4a896cc3 "fix(cmd/agent): --daemon is an act
	// too"): that change added 6 lines to cmd/agent/main.go ABOVE this site
	// (the daemonFlagLong arm in controlActionRoleOf plus the
	// mergerDaemonRequested guard), shifting its key 955→961. The site is
	// UNCHANGED — the same %w call whose two %v verbs format the
	// payloadFilePrefix STRING constant (literal per cent verbs describing how
	// to escape a payload; they wrap nothing). Master merged that branch
	// without re-keying, so its own standards gate was RED ON MASTER ALONE (a
	// NEW report at :961 AND a STALE permit at :955), and that master-side red
	// was what the post-merge verify read as this branch's verdict. Re-keying
	// is the documented repair: the gate still fails on a new %v wrap anywhere
	// else and on the stale entry, so the ratchet stays honest.
	// (end of HEAD-side audit block)
	// re-key (2026-09-14, merge 32fb9da3 into feat/peak-hour-guard): TWO
	// independent line shifts land on the SAME site and must be combined, not
	// chosen between. (1) the master-side signalrec.Install wiring (2fd022ca,
	// feat/signal-provenance-20260913) added lines ABOVE it in main(), shifting
	// 955→961; (2) this branch's peak-hour start guard (cmd/agent/peak_guard.go,
	// invoked from main() above the site) shifts it 961→982. The site itself is
	// unchanged in both cases — the same %w call whose two %v verbs format the
	// payloadFilePrefix STRING constant (the literal per cent verbs deliberately
	// describe how to escape a payload; they wrap nothing, so %w is impossible
	// there). Verified in the MERGED tree by
	// `grep -n 'escape a literal prompt' cmd/agent/main.go`, which prints exactly
	// one line (982). Re-keyed with its reason, never relaxed and never deleted:
	// the gate still fails on a stale entry AND on a new %v wrap anywhere else,
	// so pristine master (whose permit is stale at 961) fails BOTH ways while
	// this merged tree is clean.
	//
	// The master-side entries for the SAME key and the SAME site, recorded
	// independently while master was made green (both parents re-keyed this one
	// permit; the merged tree carries exactly one entry, at the line both sides
	// already agree on — 982, verified by the grep above):
	// re-key (2026-09-14, feat/fix-master-red-standards-stylegate-signalrec):
	// the master-side signalrec.Install wiring (2fd022ca,
	// feat/signal-provenance-20260913) added lines ABOVE this site in main(),
	// shifting it 955→961 and leaving the permit stale on master (the gate then
	// failed BOTH ways on pristine master 45f213d9: a NEW hit at :961 and a
	// STALE entry at :955). The SITE is unchanged — the same %w call whose two
	// %v verbs format the payloadFilePrefix STRING constant (the literal per
	// cent verbs deliberately describe how to escape a payload; they wrap
	// nothing, so %w is impossible there). Re-keyed with its reason, never
	// relaxed and never deleted: the gate still fails on a new %v wrap
	// anywhere else.
	//
	// re-key (2026-09-14, resolve merge feat/peak-hour-guard into master): the
	// peak-hour start guard (cmd/agent/peak_guard.go, called from main() ABOVE
	// this site) added lines to main(), shifting it 961→982 and leaving the
	// permit stale on the merged tree (the gate failed BOTH ways: a NEW hit at
	// :982 and a STALE entry at :961). The SITE is again unchanged — the same
	// %w call whose two %v verbs format the payloadFilePrefix STRING constant.
	// Verified by `grep -n 'escape a literal prompt' cmd/agent/main.go` in the
	// merged tree, which prints exactly one line (992). Re-keyed with its
	// reason, never relaxed and never deleted.
	//
	// Re-keyed 982 → 992 by fix/control-conflict-full-flags: the worker control
	// route in the same file gained the shared conflict-refusal call (one
	// `if controlConflictRefuse(...) { return true }` guard), which moved every
	// later line of main.go down by exactly 10. The site itself is untouched —
	// same statement, same reason, still the only hit of its grep probe.
	//
	// Re-keyed 992 → 999 by fix/worker-status-residue-output: the worker
	// --status usage text gained the --status-verbose spelling (+7 lines) in the
	// same file. Again the SITE is untouched — verified by
	// `grep -n 'escape a literal prompt' cmd/agent/main.go`, which prints exactly
	// one line (999): the same %w call whose two %v verbs format the
	// payloadFilePrefix STRING constant, not an error.
	//
	// Re-keyed 999 → 1025 by fix/daemon-no-privilege-remedy: the
	// "Privileges (--daemon)" usage section added to BOTH usage heads in the
	// same file (+26 lines) moved every later line of main.go down again. The
	// SITE is untouched — verified by
	// `grep -n 'escape a literal prompt' cmd/agent/main.go`, which prints exactly
	// one line (1025): still the same %w call whose two %v verbs format the
	// payloadFilePrefix STRING constant, not an error. Re-keyed with its reason,
	// never relaxed and never deleted.
	//
	// Re-keyed 1025 → 1035 by the SAME branch, merging master: the answer to the
	// operator's own --once question ("does --once also need a privilege?") was
	// restored to both usage heads from the arrived hint, +10 lines in the same
	// file. The SITE is untouched — `grep -n 'escape a literal prompt'
	// cmd/agent/main.go` prints exactly one line: the same %w call whose
	// two %v verbs format the payloadFilePrefix STRING constant, not an error.
	//
	// Re-keyed 1035 → 1040 by the peak-guard token-free exemption
	// (fix/peak-guard-token-free-verbs): the start guard's comment above
	// `costFreeInvocation(dispatchArgs)` grew by 5 lines worth of argv
	// comment, so the site moved by +5 (1040). The service-help epic then
	// SHRANK workerUsageHead/mergerUsageHead above it by 97 lines, moving the
	// SAME byte-identical site to 943 — re-verified by
	// `grep -n 'escape a literal prompt' cmd/agent/main.go` (one line, 943).
	// The site is a FALSE POSITIVE: its only rendered %v args are strings
	// (`payloadFilePrefix`, twice) and the error IS wrapped with %w.
	//
	// MERGE RE-KEY (fix/peak-guard-cost-class × master, 2026-09-14) and REMOVAL
	// RE-KEY (fix/remove-peak-guard, 2026-09-14): the same site, re-measured in
	// the RESOLVED tree rather than picked from a parent's number — a line-keyed
	// permit may only name the tree it was measured in (a stale permit is a
	// silent permission).
	//
	// The arithmetic of the resolution, stated so the next re-key starts from
	// facts: master added +28 (its cost-class comment block replaced the guard's
	// comment) and this branch deleted the guard's start-gate block above the
	// site, −54 net — and both parents' hunks sit ABOVE the site, so the two
	// edits COMPOSE. The permit is therefore re-measured, not merged: `grep -n
	// 'escape a literal prompt' cmd/agent/main.go` prints exactly ONE line, 917,
	// the same %w call whose two %v verbs format the payloadFilePrefix STRING
	// constant. Removing a feature that happened to sit above a permitted line is
	// a line shift, not a new violation and not a reason to relax the gate.
	//
	// RE-KEY (cli-task-surface-restructure, 2026-09-15): the restructure added
	// the "Single-pass atoms" + "Cross-node sync" sections to
	// mergerUsageHead/workerUsageHead in cmd/agent/main.go ABOVE this site
	// (+37 net), shifting the same %w call 917→954. Same site, same reason
	// (its only rendered %v args are the payloadFilePrefix STRING); the site
	// itself is untouched, so this is a re-measure, never a new permit.
	// Verified: `grep -n 'escape a literal prompt' cmd/agent/main.go` prints
	// exactly ONE line, 954, and the gate reports 9 hits / 7 baselined / 1
	// escaped / 0 NEW / 0 STALE. (SUPERSEDED 2026-09-18, fix/standards-gate-red:
	// after the task_merge.go %w restore the same three numbers read
	// 8 hits / 7 baselined / 1 escaped / 0 NEW / 0 STALE — the count moved
	// because a violation was FIXED, never because a permit was added.)
	//
	// RE-KEY (2026-09-15, the binary-summary guard 4b00b0bd — this branch
	// merged as f2daebad; master reached the same re-key as
	// fix/stylegate-green-from-master on tip 807dda98): the guard inserted 11
	// lines above this site in cmd/agent/main.go (the text-input refusal in
	// readInput), shifting the same %w call 954→965. Re-measured on the
	// RESOLVED tree — the permit names the tree it was measured in, so the
	// number above belongs to the pre-guard tree and this one supersedes it;
	// the line-keyed permit pinned at 954 went STALE while the SAME %w call was
	// reported as a NEW violation at 965 — two reds for one unchanged site.
	// Evidence, both directions: `git show 4b00b0bd^:cmd/agent/main.go | grep
	// -n 'escape a literal prompt'` → 954, `git show 4b00b0bd:cmd/agent/main.go
	// | grep -n 'escape a literal prompt'` → 965, byte-identical line (the
	// commit's --stat is 11 insertions / 0 deletions in cmd/agent/main.go); 954
	// in the current tree is now the DIFFERENT line `return "", fmt.Errorf("Error
	// reading stdin payload: %w", err)` (no %v, hence not a violation). This is
	// a re-measure of a moved permit — same site, same reason (its only
	// rendered %v args are the payloadFilePrefix STRING constant, twice, and
	// the error IS wrapped with %w) — never a new permit, never a relaxation:
	// the entry count is unchanged at 8.
	// Verified: `grep -n 'escape a literal prompt' cmd/agent/main.go` prints
	// exactly ONE line, and the gate reports the hits / baselined / escaped /
	// NEW / STALE tally for the tree it scans. The single NEW is the
	// PRE-EXISTING cmd/agent/cli_voice_client.go:811 %v wrap (row 01a0a3f9),
	// unrelated to this entry and deliberately left unfixed here.
	//
	// The line NUMBER moves with any edit above it in main.go (this entry has
	// been re-pointed three times: 965 → 972, when the merger's help text was
	// rewritten by the merger-stop-immediate epic; 972 → 973, when the worker
	// verb's unexpected-argument refusal stopped advertising the retired
	// --worker-log-root and gained its replacement note — epic
	// child-stream-task-output, 2026-09-17; then 973 → 987, when the temp
	// workspace became an ordinary parameter and main.go's two publication call
	// sites grew their explanatory comments — epic tmpdir-param-not-banner,
	// 2026-09-17, which added 14 net lines above this site). That is what a
	// line-addressed baseline buys and costs — the site is the same one, so the
	// entry is re-pointed rather than deleted; a DELETED entry would license a
	// genuinely new %v wrap at that address instead.
	//
	// RE-KEY (2026-09-17, epic daemon-machinery-removal): 987 → 1008 is a
	// re-measure of the SAME site after the merger's resident-daemon machinery
	// was deleted from main.go — the dispatch block that routed --daemon / --stop
	// and their control vocabulary was 30 net lines ABOVE this call, so removing
	// them moved the call UP (68 insertions / 98 deletions in main.go, none of
	// them at or near this line). The call is byte-identical and its reason is
	// unchanged (its only rendered %v args are the payloadFilePrefix STRING
	// constant, twice, and the error IS wrapped with %w); the entry count is
	// unchanged, no permit was added, none was relaxed.
	// RE-KEY (2026-09-17, same epic, one commit later): 1008 → 1011. The
	// re-key above was measured on fb877170; 75a6d989 (restore the --once
	// completion line and the report digest surface) then added 3 net lines to
	// main.go ABOVE this call, so the entry went STALE and the site was
	// reported NEW in the same run — exactly the drift this file's own header
	// warns about. RE-MEASURE THIS KEY IN THE FINAL TREE OF A BRANCH, not at the
	// commit that first moved the site. The call is byte-identical to master's
	// at cmd/agent/main.go:1038 (only its line number differs) and its reason is
	// unchanged (its only rendered %v args are the payloadFilePrefix STRING
	// constant, twice, and the error IS wrapped with %w); the entry count is
	// unchanged, no permit was added, none was relaxed.
	// RE-KEY (2026-09-18, epic worker-positional-taskid): 1011 → 1025. The
	// re-key above was measured on this epic's base; the worker-positional-taskid
	// change added 14 net lines to main.go (the usage head + the refusal
	// renderer's retired-spelling arm) ABOVE this call, so the entry went STALE
	// in the same run — the drift this file's own header warns about, for the
	// third time. RE-MEASURE THIS KEY IN THE FINAL TREE OF A BRANCH. The call is
	// byte-identical to the one master carries at 1011 (only its line number
	// differs) and its reason is unchanged (its only rendered %v args are the
	// payloadFilePrefix STRING constant, twice, and the error IS wrapped with
	// %w); the entry count is unchanged, no permit was added, none was relaxed.
	// Verified: `grep -n 'escape a literal prompt' cmd/agent/main.go` prints
	// exactly ONE line, and the gate reports 8 hits / 7 baselined /
	// 1 escaped / 0 NEW / 0 STALE. (SUPERSEDED 2026-09-18, fix/standards-gate-red:
	// task_merge.go's mergeErr wrap was restored to %w, so a violation left the
	// scan. Counts move by FIXING, never by adding a permit.)
	//
	// re-key (2026-09-18, feat/merger-every-loop): the merger usage head became a
	// rendered var (with the self-driving paragraph and the help-width note) ABOVE
	// this site in main(), shifting it 1025→1131. The site is the SAME unchanged
	// call (the @ payload readFile refusal; its two %v verbs format the
	// payloadFilePrefix STRING constant, so no error is being wrapped there and %w
	// is impossible). The key is re-keyed, not relaxed: the gate still fails on a
	// new %v wrap anywhere else.
	//
	// RE-KEY (2026-09-18, fix/merger-takeover-lock): the lock-adoption block
	// (mergerTakeoverLockAdopt, run before argument parsing) added 20 lines to
	// main() ABOVE this site, shifting it 1131→1151 — the fourth occurrence of the
	// drift this file's header warns about, and again the entry went STALE and the
	// site NEW in the same run. Re-keyed, not relaxed, and the site was MEASURED
	// byte-identical to master's (only its line number differs):
	//   diff <(git show master:cmd/agent/main.go | grep 'escape a literal prompt') \
	//        <(grep 'escape a literal prompt' cmd/agent/main.go)   → no output
	// Its reason is unchanged (the two %v verbs format the payloadFilePrefix STRING
	// constant; the error IS wrapped with %w). `grep -n` finds exactly ONE such
	// line; the entry count is unchanged, no permit was added, none was relaxed.
	//
	// RE-KEY #2 (2026-09-18, fix/merger-takeover-lock, commit de59efe1): the
	// adoption block grew 7 more lines when adoption was gated on the take-over's
	// IDENTITY (argv[0]) as well as the hand-off marker, shifting this site
	// 1151→1158 — the fifth occurrence of the drift this header warns about, in the
	// same branch. Same discipline: key re-measured in the FINAL tree (not at the
	// commit that moved it), site MEASURED byte-identical to master's, one
	// occurrence, entry count unchanged, nothing relaxed:
	//   diff <(git show master:cmd/agent/main.go | grep 'escape a literal prompt') \
	//        <(grep 'escape a literal prompt' cmd/agent/main.go)   → no output
	// RETIRED (2026-09-18, feat/merger-takeover-identity-stats): the permit is
	// DELETED, not re-keyed again — the site it chased through five re-keys was
	// FIXED instead. Its two rendered %v verbs formatted the payloadFilePrefix
	// STRING constant, which is text and not an error chain, so the fix was to
	// stop spelling string formatting with the verb this gate exists to catch
	// (%s), leaving the one %w that wraps the real error untouched. Entry count
	// moves DOWN by one: the ratchet turns on a fix, never on a permit. The
	// history above is kept as the record of why line-addressed permits drift.
	// The corpus-walk refusal at docclass.go:348 was a NEW entry here (2026-09-19,
	// fix/standards-baseline-repair): master 4a0364ea ("cover the 7 unrowd devdoc
	// records, drop the duplicate row, keep ignored litter out of the corpus
	// walk") added this refusal ABOVE the live keys and landed no permit for it,
	// so the gate has been red on it since. Reason it is a KEEP, not unfixed
	// debt — same class as the enumerated permits above, and the same class
	// devdoc/style-sweep-20260912.md records for the other 6:
	//
	//	return nil, fmt.Errorf("classifying %s/: cannot consult git's ignore rule (%v) — refusing to guess which files the corpus holds", Root, err)
	//
	// The call is a TERMINAL refusal of the whole corpus walk (the only caller
	// returns it straight up), and `err` here is NOT an error to chain: the
	// branch is taken exactly when `errors.As(err, &ee)` is FALSE, i.e. err is
	// never an *exec.ExitError — it is the context deadline / exec plumbing
	// error, already rendered into the operator-facing sentence by the %v verb.
	// There is no sentinel to unwrap to (the refusal IS the sentinel), so %w
	// would add a chain nobody probes. A caller that ever wants to branch on it
	// must get a sentinel error first; until then this is a reviewed exception.
	// RE-KEYED 348 -> 350 (2026-09-27, fix/stylegate-green-from-master): the
	// annotation added for the docclass ignored-error site (a comment on the
	// deferred close of ParseManifest's manifest file) sits above this refusal,
	// so the permit's line moved by two. The reason above is unchanged and still
	// holds — the shift is the drift this permit's own history is about, and a
	// line-addressed permit follows its site.
	// retired (memsearch-query-safety, task/01a0e477a731700d9274d269337ce239):
	// three memory_procedural.go entries (437/456/474) permitted the %v wraps on
	// procedural_search's three engine-error returns. The query-safety fix edits
	// the same functions (the route-1 guard above the FTS5 call, and the wrap
	// itself), so instead of re-keying the permits the sites were CORRECTED to
	// %w — the ratchet only moves down, and a fixed site's permit is stale by
	// construction. Entry count 4 -> 1 for this file.
	// PRUNED (2026-10-22, task/01a1638e): both permits named the "name dedup
	// check" %v wraps — the last two sites where the value could not simply
	// become %w, because the SAME line is reached with sql.ErrNoRows, whose only
	// meaning there is "no row yet — go insert". Wrapping that sentinel would
	// hand callers a value they must treat as SUCCESS through errors.Is, so the
	// guard was SPLIT instead: the sentinel arm falls through to the insert
	// path, and the failure arm wraps with %w (err is provably non-nil there —
	// `%w` on nil renders `%!w(<nil>)`). The sites are repaired, so the permits
	// match nothing and are stale by construction; the ratchet only moves down.
	// Verified: the Rule-16 gate now reports hits=1 baselined=0 escaped=1 NEW=0
	// (the single escape is internal/tools/worktree_sweep.go's reviewed
	// `// no-wrap:`), and stylegate's errorf-verb counts 1 production hit.
	// merge-note: HEAD's line-key (260) and master's (277) named the SAME %v
	// site at different line numbers; the merged task_merge.go has exactly one
	// such site, so a single re-keyed entry carries both intents.
	// re-key: the adoption token-match fix (fix/adoption-tag-token-dedup,
	// 96ebbac2) edited the const block above this site, shifting it 287→294.
	// Same site, same reason — the line-key moved, the violation did not.
	// re-key (merge-build-gate, 2026-09-13): the merge-time BUILD gate added
	// its step ⓪b + refusal block above this site, shifting it 294→362. Same
	// site (`merge %s failed (no conflict state to abort)` rendering mergeErr
	// with %v in the SAME message as %s), same reason.
	// re-key (merge-union-compile-gate 1a5e8ea7, 2026-09-16): the ⓪c
	// MERGE-TIME UNION COMPILE gate extracted the refusal lane into
	// taskMergeRefuseUncompilable (62 insertions / 13 deletions in this file,
	// all above this site), shifting it 362→366. Same site — the statement is
	// byte-identical at both revisions (`return fail(fmt.Errorf("merge %s
	// failed (no conflict state to abort): %v", branch, mergeErr))`: verified
	// with `diff <(git show 1a5e8ea7^:internal/tools/task_merge.go | grep -n …)
	// <(git show 1a5e8ea7:… | grep -n …)`, only the line NUMBER differs) — so
	// this is a PURE LINE SHIFT, not a new violation. Re-keyed in place: the
	// entry count is unchanged (a deleted entry would license a genuinely new
	// %v wrap at that address). Count unchanged, nothing added or deleted.
	// merge-note (fix/standards-baseline-rekey2, 2026-09-17): both parents
	// carry this site at the SAME key and record the SAME re-key fact — the
	// branch side (fix/standards-baseline-rekey, 07b2a152) spells the proof
	// out, master (6072be5e) condensed it to three lines. HEAD's full form is
	// kept verbatim and master's condensed line is its summary, so neither
	// side's intent is dropped. Re-verified against the MERGED tree:
	// `grep -n 'no conflict state to abort' internal/tools/task_merge.go` → 366.
	//
	// RETIRED (sweep 2026-09-17, fix/standards-stylegate-master-red): the site
	// is now written WITH the gate's own reviewed inline escape
	// (`// no-wrap: …`) — the permit chased this address through four re-keys
	// and each re-key left the gate red (NEW@later + STALE@earlier). With the
	// escape on the line the site reports as a deliberate exception, not a
	// violation, so the permit is DELETED and the ratchet moves down.
	//
	// CORRECTED (2026-09-18, fix/standards-gate-red): that escape was a FALSE
	// POSITIVE and has been REMOVED — the site wraps mergeErr with %w again, so
	// it is no longer a hit at all (escaped=1 -> 0 for this gate; the report's
	// one remaining escape is worktree_sweep.go). The reason: a2bf8ef4 (Fix
	// stylegate: the same site) had already fixed this wrap to %w and pruned
	// this permit; the master-canonical merge resolution 86382ebb — landed as
	// 333fb3ac — silently took the %v side back, and the escape then recorded
	// that regression as a deliberate exception instead of reverting it.
	// Evidence: `git show a2bf8ef4 -- internal/tools/task_merge.go` (%v -> %w),
	// `git show 333fb3ac^:internal/tools/task_merge.go | grep -n 'no conflict'`
	// → `:366 …: %v`, on a tree where a2bf8ef4 is already an ancestor; the same
	// revert left internal/stylegate's errorf-verb ceiling red (8 measured vs 7
	// declared) because that gate's hits list still names task_merge.go. The
	// permit stays deleted and the site stays fixed: no entry is re-added here.
}

// testEnvBaseline: os.Setenv/os.Unsetenv in test files (PROJECT.md Rule 17 /
// checklist 5.2).
// Allow-list reason: the `t.Setenv` sweep HAS run (branch feat/style-sweep,
// devdoc/style-sweep-20260912.md — every convertable site became t.Setenv, and
// this list was pruned from 115 to its current size). What remains is
// deliberate: os.Setenv for a value a CHILD PROCESS must inherit, where
// t.Setenv's automatic restore is wrong (the child needs the var at exec time,
// and some sites must also drive the production os.Getenv path rather than a
// test hook), plus sites where the test is itself a process-level save/restore
// (hand-rolled LookupEnv/Setenv/Unsetenv pairs a conversion would have to
// re-shape). A NEW hand-rolled save/restore is a defect.
//
// 2026-09-12 shrink (100 -> 96): the four internal/agent/testmain_isolation_test.go
// sites were this gate's clearest members of the first class, yet they sat here as
// line-keyed permits instead of inline escapes. They are now reasoned env-leak-ok
// escapes (see the comment above pinSuiteLedgerEnv). An escape outlives a line shift —
// the exact failure that turned this gate red inside master's OWN commit 6093c76, which
// rewrote comments above those statements and left entries pointing at :62-65 — and it
// keeps the sites VISIBLE in TestStandardGateDataReport instead of reading as debt.
// devdoc/rule17-gate-report.md carries the incident.
//
// HONESTY NOTE (2026-09-12): this header used to claim "Each site carries a
// //stylegate:ignore reason". That was FALSE for most of the list. Measured against the
// 96 entries below: 31 carry a reason on the offending line itself, 43 carry one within
// +/-3 lines, 47 within +/-6 — i.e. 53 entries have no reason even 3 lines away, and the
// count only grows with the window (no single "annotated" number is window-independent).
// The unannotated bulk is mostly bare os.Unsetenv, which leaks just as far as Setenv (the
// test is restored by the NEXT test's t.Setenv, never by its own). The child-process class
// above IS deliberate; the rest are unreviewed, and the rendered per-gate reason (which
// describes the list as hand-rolled save/restore pairs) does not fit them either.
var testEnvBaseline = []string{
	// PRUNE (2026-09-21, feat/task-sync-switch-off): the
	// cmd/agent/singlenode_serve_test.go permit is DELETED, not re-keyed — the
	// site's last statement was a redundant `os.Unsetenv(config.SyncPeersEnv)`
	// after a `t.Setenv` of the same variable, and this branch removed it
	// (t.Setenv already restores the variable AND its absence, so the manual
	// unset was a process-state write the framework owns). A permit matching no
	// violation is a silent permission, so it ratchets down:
	//
	//	grep -n 'os.Unsetenv' cmd/agent/singlenode_serve_test.go   # no hits
	"internal/agent/loop_stuck_test.go\tos.Setenv(\"GOPI_TASK_NOTIFY_ASYNC\", \"0\") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(\"GOPI_TASK_NOTIFY_ASYNC\", oldAsync) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(\"GOPI_TASK_NOTIFY_CMD\", \"cat >> \"+captureFile) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(\"GOPI_TASK_NOTIFY_CMD\", oldCmd) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(\"GOPI_TASK_NOTIFY_EVENTS\", \"loop-stuck,loop-stuck-toolful,session-wallclock-exceeded,stale\") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(\"GOPI_TASK_NOTIFY_EVENTS\", oldEvents) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(name, \"0\") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(name, \"0\") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#2",
	"internal/agent/loop_stuck_test.go\tos.Setenv(name, \"2\") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(name, \"3\") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(name, \"bogus\") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(name, \"bogus\") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#2",
	"internal/agent/loop_stuck_test.go\tos.Setenv(name, old) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(name, old) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#2",
	"internal/agent/loop_stuck_test.go\tos.Setenv(sessionWallclockEnv, \"0\") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(sessionWallclockEnv, \"1\") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(sessionWallclockEnv, oldCap) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(sessionWallclockEnv, oldCap) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#2",
	"internal/agent/loop_stuck_test.go\tos.Setenv(stuckToolfulRoundsEnv, \"0\") //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Setenv(stuckToolfulRoundsEnv, oldRounds) //stylegate:ignore notify pipeline env for this test; the notify command is spawned as a CHILD process, so a process-global set plus an explicit restore (recorded before) is the honest form\t#1",
	"internal/agent/loop_stuck_test.go\tos.Unsetenv(\"GOPI_TASK_NOTIFY_ASYNC\")\t#1",
	"internal/agent/loop_stuck_test.go\tos.Unsetenv(\"GOPI_TASK_NOTIFY_CMD\")\t#1",
	"internal/agent/loop_stuck_test.go\tos.Unsetenv(\"GOPI_TASK_NOTIFY_EVENTS\")\t#1",
	"internal/agent/loop_stuck_test.go\tos.Unsetenv(name)\t#1",
	"internal/agent/loop_stuck_test.go\tos.Unsetenv(name)\t#2",
	"internal/agent/loop_stuck_test.go\tos.Unsetenv(name)\t#3",
	"internal/agent/loop_stuck_test.go\tos.Unsetenv(name)\t#4",
	"internal/agent/loop_stuck_test.go\tos.Unsetenv(sessionWallclockEnv)\t#1",
	"internal/agent/loop_stuck_test.go\tos.Unsetenv(sessionWallclockEnv)\t#2",
	"internal/agent/loop_stuck_test.go\tos.Unsetenv(stuckToolfulRoundsEnv)\t#1",
	// PRUNE (2026-11-02, task 01a19968 — the raw-log record framing): the LAST
	// rawlog_rounds_test.go permit is DELETED, not re-keyed. That test's
	// `os.Unsetenv("GOPI_RAW_LOG")` became `t.Setenv("GOPI_RAW_LOG", "")` while the
	// test was rewritten to write through the ONE writer (`rl.LogFunc()` with a
	// rawlogfmt tag) instead of the removed bare-bytes `Write`, so the situs the
	// permit named is GONE — and a permit that matches no violation is a silent
	// permission. The substitution carries the same input: the logger is disabled
	// on the value "OFF" and on nothing else, so an absent variable and an empty
	// one take the identical default-enable path, while t.Setenv restores the
	// caller's state INCLUDING ABSENCE. Reasons identical to the 2026-09-26 prune
	// below, which is the same substitution applied to rawlog_test.go.
	// PRUNE (2026-09-26, feat/standards-reds): the EIGHT rawlog_test.go permits
	// that stood here are DELETED, not re-keyed — the file's
	// `os.Unsetenv("GOPI_RAW_LOG")` sites became
	// `t.Setenv("GOPI_RAW_LOG", "")`, the sanctioned instrument, and this
	// permission is what did NOT survive: e.g.
	//
	//	grep -n 'os.Unsetenv("GOPI_RAW_LOG")' internal/agent/rawlog_test.go   # no hits
	//
	// The substitution carries the same INPUT to the reader: newRawLoggerInDir
	// disables the logger on the value "OFF" and on nothing else, so an absent
	// variable and an empty one take the identical default-enable path — while
	// t.Setenv restores the caller's state INCLUDING ABSENCE (the bare unset
	// left the variable set to ""). The equivalence is documented once in the
	// file itself. A permit matching no violation is a silent permission that
	// also rots on the next edit above it, so the ratchet takes 88 → 80 here.
	// merge-note (fix/rule17-baseline-from-master x master): master's 9583fe9
	// DELETED the testmain_isolation_test.go entries below as stale debt (it made
	// those four suite-pin sites reasoned inline `env-leak-ok:` escapes, so they
	// keep showing in TestStandardGateDataReport as reviewed true positives).
	// Resolution = master's intent wins for those four entries: they are omitted
	// here on purpose (re-adding the 76-79 lines would re-introduce exactly the
	// stale line-keyed permits master retired). Nothing else is dropped: our side
	// kept only the line-spelling form of the same list, and the entries this side
	// carried that master lacks — taskendstore_test.go:83/84 and
	// taskstartsearch_test.go:93/94/125/126 — are still gone, but by MASTER's own
	// change, not by this resolution: master's commit converted those env sites to
	// t.Setenv (the sites no longer exist in the merged working tree), so keeping
	// them would be a STALE entry. Verify with
	// `go test ./internal/standards/ -run 'TestGate2|TestStandardGate'`.
	// PRUNE (fix/standards-agent-preexisting-reds): TWELVE line-keyed permits
	// are DELETED, not re-keyed — the 2026-09-23 count of 88 above already
	// retired one such entry for exactly this reason ("it rots on the next
	// edit above it"), and these twelve had rotted the same way: eight in
	// memory_sync_claim_test.go and four in memory_sync_delta_test.go, every
	// one of them matching no violation on master 2ec0c45b (measured: the gate
	// reported "12 STALE baseline entry" before this change). The sites they
	// once covered are still present and still gated; they are simply no
	// longer reported at the recorded line, which is what a line-keyed permit
	// cannot survive. Deleting a stale permit is the ratchet, not a weakening:
	// classify/enforce fail a stale entry BY DESIGN.
	"internal/config/cov_config_envbool_test.go\t_ = os.Setenv(k, v) //stylegate:ignore child-process env handoff; the child treats an empty value as 'unset'\t#1",
	"internal/config/cov_config_envbool_test.go\t_ = os.Unsetenv(k) //stylegate:ignore clears a fixture variable; the presence check afterwards is the assertion\t#1",
	"internal/config/cov_config_envbool_test.go\tif err := os.Unsetenv(covCfgProbeEnv); err != nil {\t#1",
	"internal/config/gopihome_test.go\tdefer os.Unsetenv(\"GOPI_HOME\")\t#1",
	"internal/config/notify_resolve_test.go\t_ = os.Setenv(env, orig) //stylegate:ignore child-process env handoff; the child treats an empty value as 'unset'\t#1",
	"internal/config/notify_resolve_test.go\t_ = os.Unsetenv(env) //stylegate:ignore clears a fixture variable; the presence check afterwards is the assertion\t#1",
	"internal/config/notify_resolve_test.go\tif err := os.Unsetenv(env); err != nil {\t#1",
	"internal/deepseek/config_test.go\tos.Setenv(\"ANTHROPIC_MODEL\", \"env-model\") //stylegate:ignore env-driven config resolution read synchronously by this test\t#1",
	"internal/deepseek/config_test.go\tos.Setenv(\"GOPI_SETTINGS\", \"/custom/path/settings.json\") //stylegate:ignore env-driven config resolution read synchronously by this test\t#1",
	"internal/deepseek/config_test.go\tos.Setenv(g.key, g.value) //stylegate:ignore env-driven config resolution read synchronously by this test\t#1",
	"internal/deepseek/config_test.go\tos.Unsetenv(\"ANTHROPIC_MODEL\")\t#1",
	"internal/deepseek/config_test.go\tos.Unsetenv(\"ANTHROPIC_MODEL\")\t#2",
	"internal/deepseek/config_test.go\tos.Unsetenv(\"ANTHROPIC_MODEL\")\t#3",
	"internal/deepseek/config_test.go\tos.Unsetenv(\"DEEPSEEK_MODEL\")\t#1",
	"internal/deepseek/config_test.go\tos.Unsetenv(\"DEEPSEEK_MODEL\")\t#2",
	"internal/deepseek/config_test.go\tos.Unsetenv(\"DEEPSEEK_MODEL\")\t#3",
	"internal/deepseek/config_test.go\tos.Unsetenv(\"DEEPSEEK_MODEL\")\t#4",
	"internal/deepseek/config_test.go\tos.Unsetenv(g.key)\t#1",
	"internal/deepseek/error_matrix_test.go\tt.Cleanup(func() { os.Unsetenv(\"GOPI_ERROR_DUMP_DIR\") })\t#1",
	"internal/deepseek/retry_debuglog_repro_test.go\tif err := os.Unsetenv(\"GOPI_DEBUG\"); err != nil {\t#1",
	"internal/deepseek/retry_debuglog_repro_test.go\tif err := os.Unsetenv(\"GOPI_DEBUG\"); err != nil {\t#2",
	"internal/tools/memory_sync_claim_cov_test.go\tos.Unsetenv(\"GOPI_TASK_NOTIFY_ASYNC\")\t#1",
	"internal/tools/memory_sync_claim_cov_test.go\tos.Unsetenv(\"GOPI_TASK_NOTIFY_CMD\")\t#1",
	"internal/tools/memory_sync_claim_cov_test.go\tos.Unsetenv(\"GOPI_TASK_NOTIFY_CMD\")\t#2",
	"internal/tools/memory_sync_claim_cov_test.go\tos.Unsetenv(\"GOPI_TASK_NOTIFY_EVENTS\")\t#1",
	// PRUNE (fix/standards-agent-preexisting-reds): SEVEN sites in this file
	// and memory_sync_delta_test.go are REPAIRED rather than permitted — each
	// was `t.Cleanup(func() { os.Unsetenv(<v>) })` immediately after a
	// `t.Setenv(<v>, ...)` of the SAME variable in the same test, so the
	// framework already restored presence AND value at test end; the manual
	// unset was a process-state write gopi owns. They are now
	// `t.Setenv(<v>, "")` (the absent default), which is why no permit for
	// them exists below (the last pair, at :397/:398, was the one this prune
	// list originally opened with).
	// RE-KEY (2026-09-19, fix/standards-baseline-repair, base 9e6d7844): the ten
	// memory_sync_claim_test.go entries above and the four memory_sync_delta_test.go
	// ones are RE-POINTED, not added — each is the same statement the entry
	// already permitted, moved. Line-shift proof (all four commands in this
	// worktree, base file vs the gate's own report):
	//
	//	git show e9d72669^:internal/tools/memory_sync_claim_test.go | grep -n 'os.Unsetenv'
	//	→ 391,392,444,445,484,485,538,539,571,595
	//	grep -n 'os.Unsetenv' internal/tools/memory_sync_claim_test.go      # 9e6d7844
	//	→ 397,398,450,451,490,491,544,545,577,601        (uniform +6)
	//	git show 98bc26e5^:internal/tools/memory_sync_delta_test.go | grep -n 'os.Unsetenv'
	//	→ 937,993,1037,1138
	//	grep -n 'os.Unsetenv' internal/tools/memory_sync_delta_test.go      # 9e6d7844
	//	→ 944,1000,1044,1145                             (uniform +7)
	//
	// The +6 is commit e9d72669 (claim-family fixture de-pinning) and the +7 is
	// 98bc26e5 (newDeltaTestDBClient) — the two master commits that landed
	// WITHOUT the shift, leaving exactly these permits pointing six/seven lines
	// above their sites. Uniform Δ across every site of a file, on statements
	// byte-identical between the two trees, is the identity argument; nothing
	// became compliant, so each is a re-point rather than a ratchet-down.
	// (The same pair of commits is why the header's 2026-09-12 re-key note is
	// still the right reading of the failure mode.)
	//
	"internal/tools/memory_sync_session_cov_test.go\tos.Unsetenv(configSyncPeersEnv)\t#1",
	"internal/tools/memory_sync_session_cov_test.go\tt.Cleanup(func() { os.Unsetenv(configSyncPeersEnv) })\t#1",
	// merge-note (fix/rule17-env-escape-annotations <- master): these three keys
	// are LINE-KEYED permits, so both sides' numbers were stale in the merged
	// tree — master shifted BOTH files (its edits above the sites moved the
	// os.Unsetenv pairs 138/139->144/145 and 1103->1133) and landed the keys
	// WITHOUT the shift, while the branch kept the old keys against the old
	// lines. A key must name the MERGED tree's line, so it is re-keyed here to
	// the actual sites (re-derived, not copied from either side).
	"internal/tools/task_deliverable_branch_test.go\tos.Unsetenv(TaskChildSessionEnv)\t#1",
	"internal/tools/task_deliverable_branch_test.go\tos.Unsetenv(TaskExecutorSessionEnv)\t#1",
	// RATCHET-DOWN (2026-10-12): the four permits for
	// internal/tools/taskid_whitespace_normalize_residual_test.go are DELETED,
	// not re-keyed — the entry this note replaces already named this as the next
	// step of the ratchet ("the honest ratchet is to DELETE the two Unsetenv
	// lines in the test file once a reviewer confirms the notify path does not
	// need the variables ABSENT"), and the fix landed:
	//
	//	grep -c '^[[:space:]]*os\.Unsetenv(' internal/tools/taskid_whitespace_normalize_residual_test.go
	//	# 0 — 4 at the previous commit. Match the STATEMENT, not the text: a
	//	bare `grep os.Unsetenv` matches this note too (4 lines after the fix).
	//
	// Both sites were the restore half of one notify fixture — t.Setenv(CMD),
	// t.Setenv(EVENTS), then t.Cleanup(func(){ os.Unsetenv(...) x2 }). The pair
	// was redundant: stdlib testing.go:1326 (common.Setenv) reads LookupEnv and
	// its cleanup calls os.Unsetenv when the variable started ABSENT and
	// os.Setenv(prev) when it did not, so t.Setenv already restores the variable
	// AND its absence. The statements were removed from the test file, so the
	// line-keyed keys were removed here rather than re-pointed: a permit matching
	// no violation is a silent permission.
	// RATCHET-DOWN (2026-09-23): the permit for
	// internal/tools/tasks_test.go:1133 is DELETED, not re-keyed. Two reasons,
	// and the second is why this differs from the re-points above.
	//
	// (a) The site is now CORRECT, not merely permitted: the line is
	// `os.Unsetenv(TaskExecutorSessionEnv)` with no matching os.Getenv, inside a
	// function that also calls `t.Setenv(TaskExecutorSessionEnv, sessID)` at
	// line 1075 — which is the reviewed os.Setenv + `testutil.SnapshotEnv`
	// restore pair this permit's own note said was "the next step for this
	// gate's ratchet". SnapshotEnv captures with os.LookupEnv, so the ABSENT
	// variable comes back ABSENT (a hand-rolled save/restore would leave it
	// present and holding a removed t.TempDir()). The site now carries the
	// visible inline escape `// env-leak-ok:` instead of a line-keyed permit —
	// a reviewed exception, in the file, where the next reader sees it.
	// (b) The permit itself was STALE: master's aed9e482 ("retire 122
	// hand-rolled DB-path setup pairs onto testutil.NewTestDBPath") deleted 24
	// lines above this statement without re-keying its permit, so the key
	// 1133 pointed below the site it permitted. One stale key reports TWICE in
	// the same run — the stale entry AND the freed site as a NEW violation —
	// so master's ./internal/standards/ was red on this pair. Re-keying would
	// have moved the number; removing it stops the drift class and leaves the
	// exception auditable as a comment rather than as a line number that rots
	// the next time anyone edits above it.
	// merge-note (2026-09-23, gopi merge run): branch
	// fix/docclass-manifest-and-rule17-permit-drift reached the SAME decision for
	// this one site by a different route (delete the drifted permit; mark the
	// site with an inline env-leak-ok escape). The two were collapsed to ONE
	// resolution: this permit is deleted, and the site's escape is the visible
	// inline marker described above. Neither branch's hunk is lost; the
	// difference was wording, not mechanism.
}

// elseAfterReturnBaseline: `} else {` after a terminating if-body (GOPI.md
// Rule 18 / checklist 4.3).
// Allow-list reason: each entry predates the gate and is behaviour-identical to
// the guard-clause form; the refactor of those functions is not in scope. The
// gate keeps the shape from spreading while the list ratchets down.
var elseAfterReturnBaseline = []string{
	// merge-note (2026-09-12, merge fix/merger-status into master): NEITHER
	// side's `cmd/agent/cli_tasks.go:1171` entry is carried. Both sides agree
	// on the violation: master had
	// `if atts, replicaPaths, err := ...; err != nil { return ... } else if
	// len(replicaPaths) > 0 {`, HEAD rewrote it into a guard-clause pair
	// (`if err != nil { return ... }` / `_ = atts` / `if len(replicaPaths) > 0`),
	// so no `} else if` exists there in the merged tree and an entry keyed to
	// it would be a STALE permit (the gate fails on a stale permit exactly as
	// it fails on a new violation). Neither side introduced an else-after-return
	// at that site or anywhere else in that file, so no replacement key is due.
	// HEAD's own history on this list: this branch's merge resolution briefly
	// resurrected the `} else if` shape (a union of the two sides) which needed
	// a re-pin; the final merge with master a0bc3e11 restored the guard-clause
	// form, so the pin went stale again and is deleted. Verified: the gate
	// reports 5 hits / 5 baselined / 0 new / 0 stale with the entry gone, and
	// 1 NEW + 1 STALE with it present (the signature of a moved site, and of a
	// permit that no longer applies). The ratchet only moves down.
	// Pre-guard key note: HEAD's `cmd/agent/cli_tasks_rebuild.go:66` is also NOT
	// carried below — master's agent-spawn guard inserted lines above that site,
	// so the live keys are the re-keyed `:76` / `:86` pair that follows this
	// comment. Same site, same reason, one live key set.
	//
	// RETIRED (rework of fix/launcher-install-refresh, 2026-09-13): both `:76`
	// and `:86` are GONE. Extracting the build half out of serveRebuildAfterMerge
	// into serveRebuild (so the caller can tell whether the round auto-reverted)
	// let those two `if err != nil { … } else if …` chains be rewritten as guard
	// clauses, which is what Rule 18 asks for — so the sites stopped being
	// violations and their permits were deleted. The ratchet moves DOWN by two;
	// nothing was added. (A later line shift must NOT re-add them: they are not
	// permits for a legacy shape any more, the shape itself is fixed.)
	// RETIRED (01a0ee1b, Rule 18 site fixed 2026-09-27): this entry was the
	// `} else if err != nil {` pair at the END of CountRequests' head-sample —
	// it sat at :181 when the gate landed (81bec216, the file was 185 lines)
	// and the IndexTree addition later shifted the same site to :403 without
	// re-keying the permit, so the gate read it as 1 NEW + 1 STALE at once.
	// The fix is the guard-clause form the rule asks for (`_, rerr := ...` /
	// `if rerr == io.EOF` / `if rerr != nil`), which is why the permit is
	// DELETED rather than re-keyed: there is no else-after-return left at this
	// site, so a key for it would be stale on arrival. The ratchet only
	// moves DOWN — a later line shift must NOT re-add it.
	// RETIRED (01a0b1b3, F6): this entry chased the ⑤ same-branch-once
	// guard's `} else if live {` chain in adoptionSweep through five re-keys.
	// The F6 fix replaced that else-chain with flat guards (`if live {
	// continue }` / `if concluded { continue }`), which is exactly what
	// Rule 18 asks for — the site is gone, so the permit retires with it and
	// the ratchet only goes down. The guard's BEHAVIOUR is unchanged and is
	// pinned by TestAdoption_NoReDispatchAfterTerminalResolveRow
	// (internal/tools/task_merge_resolve_identity_test.go).
	// merge-note: master's second entry (its `} else { mergeErr = err }`) no
	// longer exists — HEAD's ③ rewrote that branch into a guard (`if mergeErr
	// == nil { ... }`), so the merged tree has ONE else-after-return here. The
	// surviving entry is the `-d` chain both sides kept.
	// re-key: the same fix edited task_merge.go's const block, shifting the
	// `-d` chain 257→264. Same site, same reason.
	// re-key (merge-build-gate, 2026-09-13): the BUILD gate's step ⓪b +
	// refusal block shifted this `} else {` chain 264→332. Same site (the
	// ③b `if !isGitNoMergeToAbort(...)` / `else` pair), same reason.
	// re-key (merge-union-compile-gate 1a5e8ea7, 2026-09-16): the same commit's
	// ⓪c union-compile extraction (62 insertions / 13 deletions, all above this
	// site) shifted the `} else if _, err := taskMergeGit(repo, "branch", "-d",
	// branch); err != nil { … } else { … }` chain 332→336. Same site — the
	// chain is byte-identical at both revisions (same `diff … | grep -n`
	// proof as the Rule 16 entry above: `branch", "-d", branch` sits at 330
	// before and 334 after, the `already merged into …` detail at 334→338,
	// an identical -4/+4 on every one of the three occurrences) — a PURE LINE
	// SHIFT. Re-keyed in place, ratchet unchanged: no entry added, none deleted;
	// count unchanged.
	// merge-note (fix/standards-baseline-rekey2, 2026-09-17): both parents agree
	// on the key (336) and differ only in how much of the proof they spell out —
	// HEAD's full `-d`-chain form is kept, master's condensed form is its
	// summary; no fact from either side is dropped. Re-verified against the
	// MERGED tree: the gate reports `} else {` at
	// internal/tools/task_merge.go:336 (`branch", "-d", branch` at 334, the
	// `already merged into …` detail at 338).
	// re-key (sweep 2026-09-17, fix/standards-stylegate-master-red): the merged
	// tree moved this `} else {` chain 336→334 (the same -2 shift the Rule 16
	// site above took). Same site — `grep -n '} else {' internal/tools/task_merge.go`
	// → 334 in the merged tree. Re-keyed in place; count unchanged.
}

// panicOutsideInitBaseline: panic(...) outside init/main/Must* (PROJECT.md Rule 19 /
// checklist 2.7).
// Allow-list reason: EMPTY BY CONSTRUCTION, not by sampling. The census found
// exactly one site outside the gate's predicate (internal/agent/crashdump.go's
// re-panic, which is also the recover() convention) and it carries the inline
// `panic-ok:` escape in its comment block; every other panic in the tree is in
// init/main/Must* or outside the scanned trees. An empty baseline is the ideal
// state: every future panic is reported as NEW, and nothing is silently permitted.
var panicOutsideInitBaseline = []string{}

// accessorPrefixBaseline: get/is/handle-prefixed niladic methods (GOPI.md
// Rule 20 / checklist 1.4).
// Allow-list reason: EMPTY BY CONSTRUCTION. The four census hits
// (internal/shot/x11.go getRootImage/getImageBand/handshake) are TRUE POSITIVES
// deliberately kept as X11 protocol names, and each carries the inline
// `name-ok:` escape, so they are annotated rather than baselined — an escape
// documents the decision where the code is; a baseline only records that the
// checker was told to be quiet. Because they stay escapes, the data report keeps
// showing them (4 deliberate, 0 new).
var accessorPrefixBaseline = []string{}

// envReaderBaseline is Rule 21's allow-list. It is EMPTY BY CONSTRUCTION: the
// tree had no unread env hook when the gate landed (the one that motivated it —
// GOPI_TEST_SCRATCH_GC_MIN — was wired in the same change), and an intentional
// exception belongs in the code as `// env-reader-exempt: <reason>` where a
// reader can see it, not in a list on the other side of the repo.
var envReaderBaseline = []string{}

// ---------------------------------------------------------------------------
// baseline mechanics
// ---------------------------------------------------------------------------

// baselineFile renders one gate's allow-list as a reviewable Go source block.
// The reason is carried IN the file next to the entries (not in a commit
// message) so a reviewer reading `git diff internal/standards/baseline.go` sees
// the permission and its justification on the same screen — Rule 15b's
// no-silent-permission rule, applied to the allow-list itself.
type baselineFile struct {
	gate    string
	row     string
	why     string
	verify  string
	entries []string
}

// render emits the //go:generate-able source block for a gate baseline.
func (b *baselineFile) render() string {
	var sb strings.Builder
	sb.WriteString("// " + b.gate + " (checklist " + b.row + ")\n")
	sb.WriteString("// " + b.why + "\n")
	sb.WriteString("// verify: " + b.verify + "\n")
	sb.WriteString("var " + baselineVarName(b.gate) + " = []string{\n")
	for _, e := range b.entries {
		sb.WriteString("\t\"" + e + "\",\n")
	}
	sb.WriteString("}\n")
	return sb.String()
}

// baselineVarName maps a gate id to its baseline variable name.
func baselineVarName(gateID string) string {
	word := strings.Fields(gateID)
	return strings.ToLower(word[len(word)-1]) + "Baseline"
}

// RE-MEASURED ON THE MERGED TREE (2026-10-14, task/01a13587dffe7bdead8445f5db009313).
// The list below was measured at this branch's own merge base, 369 commits behind
// master, so it was stale by construction once the branch met today's master. The
// rework merged master INTO the gate and the working tree was then re-swept; the
// gate's own log line is the reading, and it is reproduced verbatim:
//
//	$ go test ./internal/standards/ -run 'TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly' -count=1 -v
//	git-env gate: 1874 test files scanned, 75 hit(s), 71 baseline entries   [BEFORE the pass below]
//	git-env gate: 1874 test files scanned, 75 hit(s), 74 baseline entries   [AFTER, re-keyed here]
//	→ 75 hits = 74 baselined + 1 escaped + 0 new, 0 stale. PASS.
//
// The delta the re-measure absorbed is THREE sites that arrived from master
// (cmd/agent/merge_already_landed_test.go:16, internal/tools/task_report_facts_test.go:44,
// internal/tools/zz_redproof_reportfacts_test.go:46 — all fixture git children of the
// same class, none of them touched by this branch; they looked "new" only because the
// list predated them), and ONE site deliberately NOT put in the list: the witness's red
// arm and the fixture-isolation witness at anchor_number_fixture_isolation_test.go:204,
// which carries the
// inline `// git-env-ok:` escape because INHERITING the environment is the very thing
// that arm measures.
//
// gitEnvBaseline is the git-env gate's allow-list: every entry is a fixture
// git call that hands its child the inherited process environment — i.e.
// exactly the class that took real master from 7678 files to 2 on 2026-10-13.
// The list is a MEASUREMENT of the tree at the commit the gate landed, and the
// ratchet is downward: repairing a site with internal/testutil.GitEnv (or the
// inline `// git-env-ok:` escape, where the inherited environment is the
// point) deletes its entry, and a stale entry fails the gate.
//
// RE-MEASURED ON THIS GROUND, task/01a141cc4f827b42945bb9b91d3afd09 (2026-10-16).
// The branch that carries this line is a NAMING + DOC change to the coverage arm
// (TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly); it touches no gate logic.
// It DID have to re-key this list, because the list was already stale on the
// ground's own cut point 28c679d, before anything here was edited, for two reasons:
//
//	pinned to rel:line, one entry ROTTED — cli_tasks_help_surface_basic_test.go:1365
//	was still keyed while the hit had moved to :1367 (any edit above a baselined
//	site perturbs its entry), which the coverage arm reports as a stale entry;
//	4 sites of this class arrived from master — g1Git
//	(cli_tasks_help_surface_basic_test.go:1367), anchorNumberMaterialGitEnv
//	(anchor_number_material_test.go:155), masterReadingTestRepo (:67) and
//	masterReadingTestGit (:226) in task_briefing_master_reading_test.go. All four
//	are fixture git children appending os.Environ(), i.e. sites whose ONLY hazard is
//	the inherited environment, which is a real repair and not a permit.
//
// A ROTATED KEY AND A REPAIRABLE SITE ARE BOTH REASONS TO *FIX*, NOT TO ADD A
// PERMIT. All five of the entries added at that cut point are gone as of this
// writing — deleted by the follow-up run task/01a1420470227746a450e53ed95502f9,
// which repaired the four with testutil.GitEnv (the same edit that moves the
// g1Git hit off the line the fifth key names). The permits below are the
// population that is still UNREPAIRED at this commit; nothing else is keyed.
//
// MEASURED, this ground, both arms, `-count=1` (the readings, not a summary):
//
//	BEFORE this branch's edits — 2 findings (the pair quoted above)
//	$ go test ./internal/standards/ -run 'TestStandardGates/TestGate9GitEnvRedProof' -count=1   → FAIL
//	AFTER  — 0 new, 0 stale
//	$ go test ./internal/standards/ -run 'TestStandardGates/TestGate9GitEnvRedProof' -count=1   → ok
//	$ go test ./internal/standards/ -run 'TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly' -count=1 -v
//	git-env gate: 1905 test files scanned, 74 hit(s), 73 baseline entries   → ok
//
// RE-MEASURED AGAIN, task/01a142df476f71188e4df1d246966d49 (2026-10-16), cut 14861c1.
// ONE entry left this list, and it was a ROTTED KEY rather than a repair debt:
// `internal/tools/git_child_locale_test.go:243` named a line that no longer
// existed, because a commit landing ABOVE it (the locale guard's own comment
// block, test/comment text only) shifted the site by +5 — so the same edit that
// made the permit STALE also made the live site read as an UNBASELINED new
// violation, and the gate reported ONE defect from both arms. Repaired at the
// SITE with testutil.GitEnv() and the entry DELETED, never re-keyed: the
// control child's only hazard was the inherited repository routing (its
// purpose is to inherit the ambient locale), so the helper is the fix. The
// ratchet moved DOWN — 73 entries → 72 — and no entry was added.
//
//	$ go test ./internal/standards/ -run 'TestStandardGates/TestGate9GitEnvRedProof' -count=1   → ok
//	git-env gate: 1910 test files scanned, 73 hit(s), 72 baseline entries   → 0 new, 0 stale
//
// RE-MEASURED AGAIN, at 4a7c538c (2026-10-31). EIGHT entries left this list and
// NONE was re-keyed: every one named a file that no longer exists on disk, so
// there was no text to locate. The rename that produced them is the same one
// that moved the finish-cleanup fixtures into
// finish_cleanup_moved_helpers_test.go, and 0e9fdbca0 restored those fixtures
// VERBATIM from the pre-retirement revision — which carried the old
// `cmd.Env = append(os.Environ(), ...)` spelling, so the restore turned two
// permits into two NEW violations at once. Both fixtures build their OWN
// repository in t.TempDir() (`git init`, `worktree add`, `push` to their own
// bare), so neither needs the caller's routing, and `cmd.Dir` cannot override
// GIT_DIR (the 2026-10-13 shape): fixed at the SITE with testutil.GitEnv, not
// baselined. The ratchet moved DOWN — 68 entries → 60 — and no entry was added.
//
//	$ go test ./internal/standards/ -run 'TestStandardGates/TestGate9GitEnvRedProof' -count=1   → ok
//	git-env gate: 60 total hits, 60 baselined, 0 escaped, 0 new (unannotated), 60 baseline entries
var gitEnvBaseline = []string{
	"cmd/agent/cli_archive_unreachable_test.go\tcontrol.Env = append(archCLIEnvWithoutLocale(os.Environ()), \"LANG=\"+archCLILocaleProbe, \"LC_ALL=\"+archCLILocaleProbe)\t#1",
	"cmd/agent/cli_resume_worktree_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"cmd/agent/cli_tasks_merged_cleanup_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"cmd/agent/cli_tasks_recycle_e2e_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"cmd/agent/cli_tasks_recycle_e2e_test.go\tcmd.Env = append(os.Environ(),\t#2",
	"cmd/agent/cli_tasks_recycle_e2e_test.go\tcmd.Env = append(os.Environ(),\t#3",
	"cmd/agent/cli_tasks_show_advisory_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"cmd/agent/cli_tasks_show_git_provenance_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"cmd/agent/cli_tasks_show_git_provenance_test.go\tcmd.Env = append(os.Environ(), \"GIT_CONFIG_GLOBAL=/dev/null\", \"GIT_CONFIG_SYSTEM=/dev/null\")\t#1",
	"cmd/agent/cli_tasks_worker_recycle_e2e_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"cmd/agent/repo_context_sandbox_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"cmd/agent/task_claim_ground_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/agent/reframe_durable_repo_test.go\tcmd.Env = append(os.Environ(), \"GIT_CONFIG_GLOBAL=/dev/null\", \"GIT_CONFIG_SYSTEM=/dev/null\")\t#1",
	"internal/agent/task_submit_repo_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/smoke/smoke_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/git_peer_fetch_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/gitblame_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/gitblame_test.go\tcmd.Env = append(os.Environ(),\t#2",
	"internal/tools/image_selfcheck_test.go\tcmd.Env = append(os.Environ(), \"GOFLAGS=\")\t#1",
	"internal/tools/rework_landing_probe_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/rework_landing_probe_test.go\tcmd.Env = append(os.Environ(),\t#2",
	"internal/tools/rework_landing_probe_test.go\tcmd.Env = append(os.Environ(),\t#3",
	"internal/tools/rework_landing_probe_test.go\tcmd.Env = append(os.Environ(),\t#4",
	"internal/tools/shared_stash_guard_test.go\tcmd.Env = append(withoutGitDirEnv(os.Environ()),\t#1",
	"internal/tools/task_deliverable_branch_test.go\tcmd.Env = append(os.Environ(), \"GIT_CONFIG_GLOBAL=/dev/null\", \"GIT_CONFIG_SYSTEM=/dev/null\")\t#1",
	"internal/tools/task_deliverable_branch_test.go\tcmd.Env = append(os.Environ(), \"GIT_CONFIG_GLOBAL=/dev/null\", \"GIT_CONFIG_SYSTEM=/dev/null\")\t#2",
	"internal/tools/task_deliverable_claim_stamp_test.go\tcmd.Env = append(os.Environ(), \"GIT_CONFIG_GLOBAL=/dev/null\", \"GIT_CONFIG_SYSTEM=/dev/null\")\t#1",
	"internal/tools/task_deliverable_git_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_deliverable_worktree_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_finish_fixture_helpers_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_finish_fixture_helpers_test.go\tcmd.Env = append(os.Environ(),\t#2",
	"internal/tools/task_finish_push_contract_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_finish_push_contract_test.go\tcmd.Env = append(os.Environ(),\t#2",
	"internal/tools/task_finish_push_tip_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_ground_cutpoint_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_ground_cutpoint_test.go\tcmd.Env = append(os.Environ(),\t#2",
	"internal/tools/task_ground_reconcile_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_holder_resume_e2e_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_report_facts_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_resume_ground_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_resume_ground_test.go\tcmd.Env = append(os.Environ(),\t#2",
	"internal/tools/task_resume_ground_test.go\tcmd.Env = append(os.Environ(), \"GIT_CONFIG_GLOBAL=/dev/null\", \"GIT_CONFIG_SYSTEM=/dev/null\")\t#1",
	"internal/tools/task_resume_ground_test.go\tcmd.Env = append(os.Environ(), \"GIT_CONFIG_GLOBAL=/dev/null\", \"GIT_CONFIG_SYSTEM=/dev/null\")\t#2",
	"internal/tools/task_run_ground_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_sync_git_pushback_ancestry_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_sync_git_pushback_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_sync_git_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/task_sync_git_test.go\tcmd.Env = append(os.Environ(),\t#2",
	"internal/tools/task_sync_git_test.go\tcmd.Env = append(os.Environ(),\t#3",
	"internal/tools/task_sync_git_test.go\tcmd.Env = append(os.Environ(),\t#4",
	"internal/tools/task_sync_git_test.go\tcmd.Env = append(os.Environ(),\t#5",
	"internal/tools/tasks_deliverable_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/tasks_deliverable_test.go\tcmd.Env = append(os.Environ(),\t#2",
	"internal/tools/worktree_refusal_arms_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/worktree_self_position_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"internal/tools/zz_redproof_reportfacts_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"testsuite/chat_ground_test.go\tcmd.Env = append(os.Environ(),\t#1",
	"testsuite/worktree_fresh_path_e2e_test.go\tcmd.Env = append(os.Environ(),\t#1",
}
