package stylegate

// The anti-regression gate: the style sweep's rules run inside `go test`, so a
// reintroduced time.Sleep / os.Setenv / unexplained t.Skip, a new %v-wrapped
// error or a fresh silent error discard turns the suite red. Without this, the
// sweep is a one-off cleanup that decays back to the baseline.
//
// WHY A CEILING AND NOT ZERO: most remaining hits are TEST-fixture sites whose
// verdict is "no code change is correct here" — a sleep that IS the wait (a
// process-liveness hold), an os.Setenv a child process must inherit. The gate
// holds them with a measured, per-rule ceiling, so the number can only go down
// or be raised deliberately in a reviewable diff. Production (non-test) code
// has no ceiling: it must stay at zero, and the few grandfathered sites are
// listed explicitly with their reason (see devdoc/style-sweep-20260912.md).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gateCeilings are the measured maximums from the sweep, keyed by rule and
// whether the site sits in a _test.go file.
//
// errorf-verb was tightened 9 -> 8 by fix/stylegate-green-from-master
// (2026-09-15, on master tip 807dda98) — the RATCHET direction, measured, not
// guessed: two production sites reaching the count with a silent bug behind
// them were FIXED rather than annotated, so the ceiling now equals the measured
// count (the same discipline as 2858a4b4, which set 10 -> 9):
//
//	internal/signalrec/signalrec.go:472   `serr` was wrapped with %v by a
//	  combined `serr != nil || !fi.IsDir()` guard whose !IsDir() arm reaches
//	  the format call with serr == nil (`%w` on nil renders `%!w(<nil>)`, and
//	  the same guard dereferenced a nil fi) — the guard is split, and the stat
//	  error now stays IN the chain with %w (exit 0 preserved behaviour).
//	cmd/agent/cli_voice_client.go:811     the removal failure (`rmErr`,
//	  provably non-nil under its own guard) was rendered with %v, dropping it
//	  from errors.Is/As — now %w (a second %w verb, Go 1.20+).
//
// CORRECTED (2026-10-22): this block used to claim the remaining hits were
// "the long-baselined internal/standards Rule-16 sites". That was WRONG and it
// hid a real defect for weeks — measured, the Rule-16 predicate requires a
// STRING LITERAL as fmt.Errorf's first argument, so five of the eight sites
// (which pass a named const) were never in its scan at all, baselined or
// otherwise. The two detectors differ by exactly that: this rule resolves a
// const format string through go/types, and counts what it cannot resolve as
// undecidable instead of dropping it. See
// devdoc/rule16-two-gates-disagree-20261022.md, and the ceiling below for what
// is left and why.
var gateCeilings = map[string]map[bool]int{
	// 8 -> 7 (2026-09-16, fix/standards-reds): the two sites that had drifted in
	// since the 8 was set were resolved in opposite directions — task_merge.go's
	// "no conflict state to abort" message became %w (the permit for it was
	// pruned from errorWrapBaseline), merge_build_gate_gocache.go's fmt-const
	// became %w too, while worktree_sweep.go's unlock message stays %v with its
	// `// no-wrap:` reason (the unlock error is deliberately outside the chain).
	// The ceiling follows the measurement down; it is not raised back.
	//
	// RESTORED (2026-09-18, fix/standards-gate-red): the task_merge.go half of
	// that paragraph had been silently UNDONE — a master-canonical merge
	// resolution (86382ebb, landed as 333fb3ac) took the %v side back after
	// a2bf8ef4 had already fixed it to %w, and an inline `// no-wrap:` escape
	// then recorded the regression as a deliberate exception. Measured
	// consequence, which is what this ceiling caught: errorf-verb read 8
	// against this ceiling of 7, with `internal/tools/task_merge.go:367 (%v
	// wraps error (expr mergeErr))` named in the hit list. The site is %w again,
	// so the measurement is 7 == ceiling and the ceiling does not move. NOTE:
	// stylegate's errorf-verb honours NO inline escape — an escape in the code
	// buys nothing here and the ceiling is where a deliberate unwrap must be
	// declared, so this rule is the double-check for any `// no-wrap:` claim.
	// 7 -> 6 (2026-09-27, fix/stylegate-green-from-master): the three
	// cmd/agent/cli_uistate_compare.go sites were GUARDED (each sits under its
	// own `if xErr != nil`) and rendered the error with %v while the message
	// const in the same file was already a %w, so the call-site argument was
	// the only thing off the chain. They are %w now and the ceiling follows
	// the measurement down — the same direction as 9 -> 8, 8 -> 7 and 10 -> 9.
	// The remaining 6 are the long-baselined Rule-16 sites (memory_procedural
	// x4, memory_sync_procedural) plus worktree_sweep.go:2199, which carries
	// the standards gate's `// no-wrap:` reason (this rule honours no inline
	// escape, so its ceiling is where that reason is declared).
	// 6 -> 1 (2026-10-22, task/01a1638e): the eight production hits measured on
	// that branch were adjudicated ONE BY ONE and all but one are gone.
	//
	// FIXED to %w (the value is an error by construction at the site, so %w can
	// never render `%!w(<nil>)` and the message is byte-identical):
	//   cmd/agent/memory_sync_tick.go      the two measurement failures
	//     (DeviceID / RootDeviceID, each under its own `err != nil`) and the
	//     MkdirAll failure — the reason constants carry %w now.
	//   cmd/agent/worker_control_selector.go  the "ledger could not be read"
	//     format const; one call site is under `if err != nil`, the other passes
	//     a fmt.Errorf, which is an error by construction.
	//   internal/tools/memory_procedural.go + memory_sync_procedural.go  the
	//     "name dedup check" wraps: the ONE guard was reaching the format call
	//     with a nil err (sql.ErrNoRows, and nil itself), so it is SPLIT — the
	//     sentinel falls through to the insert path and the failure arm wraps
	//     with %w. Their internal/standards Rule-16 permits were pruned in the
	//     same change (a repaired site's permit is stale by construction).
	//
	// 1 -> 0 (2026-10-27, task/01a17abc, MEASURED on 88f9b5f): the paragraph
	// above KEPT this ceiling at 1 for one site —
	// internal/tools/worktree_sweep.go's unlock message — and that site is no
	// longer a hit. 59d5014 ("three sites go %w, and a dead status field goes
	// with its silent failure") made it `%w` on BOTH verbs
	// (`"unlock did not hold (%w) and the lock file could not be cleared: %w"`,
	// worktree_sweep.go:2490), so the last production escape in this rule is
	// gone and the measurement is 0. The ceiling follows the measurement down;
	// it is not left standing as a silent permission for the next `%v`.
	//
	// MEASURED, not inferred:
	//
	//	go run ./internal/stylegate/cmd/stylegate -root . -rule errorf-verb
	//	→ errorf-verb  0 / TOTAL 0 / undecidable 621
	//
	// The double-check property this rule carries is unaffected: it still
	// honours no inline escape, so a future `// no-wrap:` claim that is not
	// true of the code turns this count red at 0.
	// Re-measured at 64301ad (2026-10-29): 3 production hits, all three in
	// internal/agent/resume_dump_fallback.go (2f6537c), which wrapped with %v
	// and broke the errors.Is/As chain Rule 16 exists to keep. Fixed at the
	// sites (%w), so the ceiling stays 0 — it never moved.
	RuleErrorfVerb: {false: 0, true: 0},
	// 182 -> 163 (2026-09-16, fix/standards-reds): the delta that had drifted in
	// (35 sites) was documented per site with its own reason, and 19 permits from
	// master's testutil refactor that no longer matched any site were dropped.
	// The ratchet may only go down.
	// 163 -> 77 (2026-10-27, task/01a17abc, MEASURED on 88f9b5f): the ceiling
	// was 86 sites above the live reading, so it was licensing 86 silent
	// discards that nobody had ever decided on. The 77 that remain are ALL
	// `_test.go` sites and the production scope reads 0 — which is what
	// `grandfatheredNone` requires of a non-test rule.
	//
	// MEASURED:
	//
	//	go run ./internal/stylegate/cmd/stylegate -root . -rule ignored-error
	//	→ ignored-error  77 / TOTAL 77 (all in _test.go) / undecidable 621
	//
	// The 86-site gap is NOT scanner blindness, and that had to be PROVED rather
	// than assumed — a detector that stopped seeing its subject reports exactly
	// this shape. Three measurements, each a command and its raw output:
	//
	//  1. DETECTION POWER, minimal sample (`/dev/shm/sgprobe`, a one-file
	//     module): an unmarked `y, _ := R()` is reported and a marked sibling
	//     `z, _ := R() //stylegate:ignore <reason>` is not — the count reads 1,
	//     neither 0 (blind) nor 2 (marker ignored). The rule still sees its
	//     subject and still honours its marker.
	//  2. A/B AGAINST THE BASE. On a detached worktree of 964b240 — the commit
	//     that WROTE this 163 — the same instrument reads
	//     `ignored-error 78 (73 test + 5 production)`, NOT 163. So 163 has never
	//     equalled a reading this repository can reproduce: it was stale from
	//     the snapshot that introduced it, and 86 of its 89-site gap was
	//     permission, not history.
	//  3. The five production sites that base tree carried
	//     (cmd/agent/worker_control_selector.go:340/:347,
	//     internal/tools/memory_procedural.go:241,
	//     internal/tools/memory_sync_procedural.go:245,
	//     internal/tools/worktree_sweep.go:2494) are all GONE at 88f9b5f —
	//     production 5 -> 0 — which is where the test-side 73 -> 77 grew from.
	//
	// THE REMAINING SITES DO **NOT** CARRY MARKERS — this paragraph used to say
	// they did ("each carries its own `//stylegate:ignore <reason>`"), and that
	// sentence was FALSE about the tree it was standing in. Corrected
	// 2026-11-01 (task 01a196f4, MEASURED at 5c56318): the reading is 66, and
	// the number of those 66 sites with `//stylegate:ignore` on the counted
	// line is ZERO — not 66, not "most". Counted, not eyeballed: the
	// instrument's own JSON over every finding, with each finding's source line
	// read back from disk, reports `with marker on the counted line: 0` and
	// `near-marker count: 0 of 66` (nothing within two lines either). They are
	// legitimate test-fixture discards — a handle closed to force the reopen
	// that IS the assertion, a Kill on a teardown path, a probe whose exit code
	// IS the assertion — but they are held by the CEILING ENTRY ALONE, not by a
	// per-site reason. That is a real difference in KIND: a marker is a promise
	// the site makes about itself, a ceiling is a number somebody decided on,
	// and these 66 are the second thing.
	//
	// THE CEILING IS THEREFORE A MEASUREMENT AND NOT PERMISSION — and the way
	// to see that is the counterintuitive step below, which is measured, not
	// reasoned. ANNOTATING A SITE MAKES THE READING GO DOWN AND THE GATE GO RED:
	// a site that carries a reason is not counted, so adding a marker does not
	// merely "make the site legitimate", it removes it from the count — and a
	// ceiling that then sits ABOVE its measurement is unused headroom, which
	// TestEveryCeilingEqualsItsMeasurement refuses. MEASURED (one marker added
	// to cmd/agent/chat_session_slots_test.go:342, then restored):
	//
	//	go run ./internal/stylegate/cmd/stylegate -root . -rule ignored-error
	//	→ ignored-error       65          (66 before the marker)
	//
	//	go test ./internal/stylegate/ -count=1 -run 'TestEveryCeilingEqualsItsMeasurement'
	//	→ --- FAIL: TestEveryCeilingEqualsItsMeasurement (3.54s)
	//	     ratchet_headroom_redproof_test.go:191: stylegate ignored-error: ceiling 66
	//	     vs measured 65 (test scope): 1 site(s) of UNUSED HEADROOM — the ceiling
	//	     is 66, the measurement is 65, so 1 silent discard(s) may arrive without
	//	     moving a number anyone decided on. Tighten the ceiling to the reading in
	//	     the same change that removed the site (Rule 1: 只能向下棘轮, and a ceiling
	//	     above its measurement is a licence, not history)
	//
	// So the two halves move TOGETHER or not at all: annotating the 66 sites
	// without lowering the ceiling to 0 leaves 66 of headroom and a red gate,
	// and lowering the ceiling without annotating leaves a red gate too. That
	// is why the marker-less state is not an oversight to be tidied up in
	// passing — closing it is ONE change that annotates every remaining site
	// and takes the ignored-error test-scope ceiling DOWN TO ZERO in the same
	// commit (the ceiling is the `true:` column of that rule's row in the map
	// below), and it belongs to its own row, not to this comment.
	//
	// Two commands reproduce everything above, and both are safe to run:
	//
	//	go run ./internal/stylegate/cmd/stylegate -root . -rule ignored-error
	//	go test ./internal/stylegate/ -count=1
	//
	// The ceiling now equals the reading, so the count may only move down from
	// here — and because these sites carry no marker, "down" cannot be reached
	// by reasoning about the sites at all; it is reached by fixing or
	// annotating one AND moving the ceiling in the same change.
	//
	// 77 -> 72 (2026-10-28, task/01a17e38, MEASURED at 8fe1d4b): the reading
	// had drifted 77 -> 78 on a ceiling that sits at its own measurement, so
	// the drift was audited instead of tolerated.
	//
	// THE BASELINE IS 38eecbe, not 88f9b5f. 38eecbe ("two ceilings meet their
	// measurement") is the commit that wrote 77, and at that commit the
	// instrument read 77 — the ceiling has never been permission, it has been
	// a measurement. (88f9b5f is merely the last test-touching commit BEFORE
	// it, and I named it as the baseline once; the wrong attribution is
	// corrected here rather than left as a plausible-looking rumour.)
	//
	// THE ARITHMETIC, measured as a multiset difference of the instrument's own
	// JSON at 38eecbe vs 8fe1d4b (`go run ./internal/stylegate/cmd/stylegate
	// -root . -json`), not by reading diffs:
	//
	//	ignored-error    77 -> 78   = +6 new sites, -5 sites whose files 5a9af79 retired
	//	test-sleep        0 ->  1   = +1 new site
	//	unchecked-close   0 ->  2   = +2 new sites
	//	                -------------
	//	TOTAL            77 -> 81   = 9 sites added after the ceiling was measured
	//
	// All nine were judged individually — none is a blanket exemption — and the
	// nine went two ways. FIVE of them sit in files upstream ALSO swept on its
	// own (6111467, before this branch reached master): two of those upstream
	// made CHECK the error they used to discard, three it annotated at its own
	// site. This branch takes upstream's text verbatim for those five (a
	// multiset difference of the two instruments' JSON is empty on them), so
	// what remains as THIS branch's own work is the other four, listed here
	// with the reason it carries at its own line:
	//
	//	cmd/agent/worker_control_sigint_e2e_test.go:316    teardown after the identity read already failed
	//	cmd/agent/worker_control_sigint_e2e_test.go:317    reaping that just-killed child; Fatalf decided the outcome
	//	cmd/agent/worker_hangup_drain_test.go:98           the poll IS the measurement (survives the whole settle window)
	//	internal/agent/rowidentity_test.go:162             append-only session log; the assertion is the minted id
	//
	// (The annotation lines for cli_resume_record_gap_test.go,
	// git_publish_target_guard_test.go and
	// task_reclaim_clone_ground_verdict_test.go that this branch wrote before
	// the delivery rebase are GONE from the tree — upstream's resolution of the
	// same sites superseded them. Naming a line that no longer carries the
	// reason would be the wrong-attribution mistake this file already paid for
	// once.)
	//
	// The five that went AWAY (cmd/agent/merge_stop_test.go x4,
	// merge_already_landed_test.go x1) belong to 5a9af79, which retired the
	// merge lane's test files; their sites went with them. The ceiling follows
	// the measurement down in both directions — it may never stand as
	// permission for the next silent discard.
	//
	// MEASURED, not inferred — three trees, one instrument
	// (`go run ./internal/stylegate/cmd/stylegate -root <tree>`):
	//
	//	38eecbe   (wrote the 77)   ignored-error 77 / TOTAL 77
	//	8fe1d4b   (this branch's base before the delivery rebase)
	//	                           ignored-error 78 / TOTAL 81
	//	                           (+ test-sleep 1, unchecked-close 2)
	//	this tree (rebased onto 229793f, nine sites annotated)
	//	                           ignored-error 72 / TOTAL 72 / undecidable 640
	//
	// Upstream annotated SEVEN of those same nine on its own (6111467, measured
	// 74 and leaving the ceiling at 77), so the two branches now differ by
	// exactly the two sites whose file that commit did not touch —
	// cmd/agent/worker_control_sigint_e2e_test.go:316/:317. A multiset
	// difference of the instrument's own JSON, this tree against 229793f:
	// "in upstream, not here: those 2; here, not in upstream: 0". The new
	// ceiling is therefore this tree's MEASUREMENT, not a haircut and not a
	// permit: it is equal to the reading, so the next silent discard adds 1
	// and has to be argued for at its own line.
	// 72 -> 71 (2026-10-29, task/01a183d8, MEASURED at d1129c8): the
	// ledger-agreement precondition's automatic leg was retired, taking two
	// test-fixture discards with it (the redproof file that went with the
	// mechanism). The reading is the ceiling, so unused headroom cannot become
	// a licence for the next silent discard.
	// 72 -> 71 KEPT, re-measured at 64301ad (2026-10-29): the reading came
	// back as 72, and the extra site is not headroom returning — it is the
	// unannotated discard that arrived with 2f6537c's new file
	// (internal/agent/resume_dump_fallback_test.go:161). Annotating that site
	// restored the reading to 71, so the ceiling did NOT move: a new discard
	// is what the ceiling is for, and the fix belongs at the site.
	// 71 -> 68 (2026-10-30, task 01a18799, MEASURED on this tree): the four retired
	// read-only audit verbs took three test-fixture discards with them. The reading is
	// the ceiling, so the ratchet only moved DOWN (Rule 1).
	// 68 -> 67 (2026-10-30, task 01a18e25, MEASURED): re-measured in a ground whose
	// base had already moved to ac85baa, the reading was 71 against the 68 for three
	// NEW unannotated sites (cmd/agent/closeout_reap_split_test.go:100 and :102,
	// cmd/agent/ground_kill_identity_test.go:156) — plus the site ac85baa's rule fix
	// repaired at the source (cmd/agent/cli_term_once_test.go:640, which reads as a
	// skip violation no more). The three fixture reaps carry their reason now, and
	// the locale control checks the error it was discarding. The reading is 67, so
	// the ceiling follows the measurement DOWN: it is not raised, and it is not left
	// with headroom (TestEveryCeilingEqualsItsMeasurement fails on headroom too).
	// 68 -> 67 (2026-10-30, task 01a18e25, MEASURED): re-measured in a ground whose
	// base had already moved to ac85baa, the reading was 71 against the 68 for three
	// NEW unannotated sites (cmd/agent/closeout_reap_split_test.go:100 and :102,
	// cmd/agent/ground_kill_identity_test.go:156) — plus the site ac85baa's rule fix
	// repaired at the source (cmd/agent/cli_term_once_test.go:640, which reads as a
	// skip violation no more). The three fixture reaps carry their reason now, and
	// the locale control checks the error it was discarding. The reading is 67, so
	// the ceiling follows the measurement DOWN: it is not raised, and it is not left
	// with headroom (TestEveryCeilingEqualsItsMeasurement fails on headroom too).
	//
	// 67 -> 66 (2026-10-30, task 01a18e45, MEASURED on d36b6d7): the ignore marker
	// written on a CONTAINER HEADER is now read by the site inside that container,
	// so the one site whose reason had been silently discarded counts no more —
	// cmd/agent/worker_stop_group_self_guard_test.go:88 (marker on the `if` header
	// one line above it). This is a DETECTOR repair, not an annotation: nothing was
	// added to the tree to bring the number down, and the regression probe
	// (internal/stylegate/ignore_marker_container_test.go) pins that a bare site and
	// a marker detached by a blank line are still reported. Reading 67 at d36b6d7
	// before the fix, 66 after; the ceiling follows DOWN.
	//
	// RE-MEASURED 2026-11-04 (task 01a1a626, measured in its ground cut at
	// d1881a4): the reading had drifted to 5 production + 68 test against this
	// row, and the drift was a MULTISET DIFFERENCE against d36b6d7's own JSON
	// (`-json` on a `git archive d36b6d7` export), not a diff read: 7 sites
	// arrived, 0 left. All seven are annotated with their own reason and the
	// row stays {false: 0, true: 66} — NOT raised. The production five were
	// added by internal/uiclient/sgrstream.go's reader in one change and each
	// is a `strconv` parse of HEADER or TRAILER metadata, never an input to
	// framing, so no site there can silently accept a wrong stream; the test
	// two are chat_sgr_stream_test.go's negative assertions on a parser whose
	// only error arm also sets `matched`, so the discard cannot hide a failure.
	// The number therefore does not move: it was equal to its measurement
	// before and it is equal after, and the ceiling is still a measurement
	// rather than a permission.
	//
	// 66 -> 0 (2026-11-05, task 01a1a89d, MEASURED in its ground cut at
	// d04e981): the COUNTING ratchet is retired for this rule in BOTH scopes.
	// A ceiling of N is a number somebody decided on, so "justify this
	// discard" kept degrading into "raise the number" — five recurrences,
	// each one re-measuring a count and moving a digit. The mechanism that
	// replaces it is the one the rules below already had
	// (`unchecked-close`, `test-sleep`, `test-setenv`, …): EVERY counted site
	// states its own reason, so the reading is 0, the ceiling is 0, and the
	// next bare discard cannot be absorbed by headroom.
	//
	// The instrument's own reading on that ground, both scopes:
	//
	//	go run ./internal/stylegate/cmd/stylegate -root . -rule ignored-error
	//	→ ignored-error 64  (1 production + 63 test)   [before this change]
	//	→ ignored-error  0                              [see the two halves below]
	//
	// The ONE production site (cmd/agent/cli_tasks.go:2714, a discarded
	// tools.TaskReleaseLockPhase) was REPAIRED AT THE SOURCE, not annotated:
	// the yield path now reports a clear failure on stderr instead of dropping
	// it.
	//
	// The 63 test sites were NOT all annotated by hand: 48 carried a
	// CALLEE-wide reason in internal/stylegate/reasons/ignored-error.json
	// (`strconv.Atoi`, `cmd.Wait`, …) — the exact shape this change retires,
	// because a callee-wide entry justifies a GROUP and a group is never a
	// reason. That table is now keyed per SITE (`file:line`) and the 15 sites
	// it could not explain were given a reason written for them (a probe's exit
	// status that IS the subject, a fixture count whose assertion below fails
	// on zero, …). The annotator refuses any key that is not `file:line`
	// (internal/stylegate/cmd/stylegate/annotate.go), so a callee-wide entry
	// cannot clear a site again — and a site whose LINE MOVES under a later
	// edit returns as a finding instead of silently keeping a stale
	// justification.
	//
	// What a ceiling of 0 means here: a new `_ = err` with no reason is RED and
	// TestStyleGate names its file and line (it lists every site); adding
	// `//stylegate:ignore <reason>` on that line turns it green. Both
	// directions are driven on a real throwaway module by
	// TestIgnoredErrorPerSiteReasonIsTheGate (per_site_reason_redproof_test.go).
	RuleIgnoredError: {false: 0, true: 0},
	// The test ceiling is 0 after the 2026-09-11 per-site audit of all 34 sites: every
	// one is a _test.go cleanup (drain-close of a logger whose file the test
	// already read, a handle closed to force the reopen that IS the assertion,
	// a pipe/fixture close whose buffered bytes the next read asserts, a
	// listener teardown, or an error-path close on an already-failing t.Fatalf
	// path) and now carries its own per-site reason, so the count is 0 and the
	// ratchet has no headroom. No production site was involved.
	RuleUncheckedClose:  {false: 0, true: 0},
	RuleTestSleep:       {true: 0},
	RuleTestSetenv:      {true: 0},
	RuleTestSetenvScope: {true: 0},
	RuleTestSkip:        {true: 0},
	// RuleTestChdir has no baseline: the 8 sites the audit found were fixed in
	// the same change that added the rule, so any hit is a new silent cwd
	// restore (devdoc/chdir-restore-check.md).
	RuleTestChdir: {true: 0},
}

// grandfatheredNone is the set of non-test rules whose ceiling must be zero:
// they are the ones the sweep actually closed, so any new hit is a regression.
var grandfatheredNone = map[string]string{
	RuleIgnoredError:    "every discarded error in production code now carries a //stylegate:ignore reason",
	RuleUncheckedClose:  "every bare Close() in production code now carries a //stylegate:ignore reason",
	RuleTestSkip:        "every t.Skip in the repo now carries a literal reason or a reason comment",
	RuleTestSleep:       "every time.Sleep in a test carries a //stylegate:allow-sleep reason or was removed",
	RuleTestSetenv:      "every os.Setenv in a test carries a //stylegate:ignore reason or became t.Setenv",
	RuleTestSetenvScope: "t.Setenv's undo scope is the TEST, so a per-call t.Setenv inside a closure must be an explicit os.Setenv+restore pair (the rule exists because an automated os.Setenv→t.Setenv conversion broke cmd/agent's dispatch test — see the sweep report)",
}

// repoRootFromGateFile walks up from the test's working directory to the module
// root (the directory holding go.mod).
func repoRootFromGateFile(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// TestStyleGate is the ratchet. It scans the whole repository (this package
// included) and fails when any rule exceeds its ceiling.
func TestStyleGate(t *testing.T) {
	root := repoRootFromGateFile(t)
	rep, err := Scan(Options{Root: root, IncludeTests: true})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	if len(rep.PackagesFailed) > 0 {
		t.Errorf("type information failed for %d package(s) — findings may be incomplete: %v",
			len(rep.PackagesFailed), rep.PackagesFailed)
	}
	if rep.Undecidable > 0 {
		t.Logf("undecidable sites (reported, never guessed): %d", rep.Undecidable)
	}

	counts := map[string]map[bool]int{}
	sites := map[string]map[bool][]string{}
	for _, f := range rep.Findings {
		if counts[f.Rule] == nil {
			counts[f.Rule] = map[bool]int{}
			sites[f.Rule] = map[bool][]string{}
		}
		inTest := isTestPath(f.File)
		counts[f.Rule][inTest]++
		sites[f.Rule][inTest] = append(sites[f.Rule][inTest],
			fmt.Sprintf("%s:%d (%s)", f.File, f.Line, f.Detail))
	}

	for rule, byTest := range gateCeilings {
		for inTest, ceiling := range byTest {
			got := counts[rule][inTest]
			if got <= ceiling {
				continue
			}
			scope := "production"
			if inTest {
				scope = "test"
			}
			hint := ""
			if why, ok := grandfatheredNone[rule]; ok && !inTest {
				hint = " (" + why + ")"
			}
			// Name every hit, not just the count. A bare count forces the
			// next agent to re-derive which site appeared (a real incident:
			// one run reported 33 unchecked closes where every other run
			// reported 32, and the count alone gave no way to find the
			// 33rd). With the list, a recurrence identifies itself.
			t.Errorf("stylegate %s: %d %s hit(s), ceiling %d%s — new violations of a swept rule are regressions; "+
				"fix them or document an explicit reason (see devdoc/style-sweep-20260912.md)\n  sites:\n    %s",
				rule, got, scope, ceiling, hint, strings.Join(sites[rule][inTest], "\n    "))
		}
	}
}

// isTestPath reports whether a repo-relative path is a test file.
func isTestPath(rel string) bool {
	return len(rel) > len(testSuffix) && rel[len(rel)-len(testSuffix):] == testSuffix
}

// testSuffix is the Go test-file suffix (Rule 1b: named, never inline).
const testSuffix = "_test.go"

// TestGateCeilingsAreReachable guards the gate itself: a ceiling that no longer
// matches reality (because a batch landed) must be tightened, and a ceiling
// that can never be met (below zero / wrong key) is a silent hole. The check is
// cheap: every ceiling must be a non-negative number for a rule that exists.
func TestGateCeilingsAreReachable(t *testing.T) {
	known := map[string]bool{}
	for _, r := range AllRules {
		known[r] = true
	}
	for rule, byTest := range gateCeilings {
		if !known[rule] {
			t.Errorf("ceiling for unknown rule %q — the gate would never fail on it", rule)
		}
		if len(byTest) == 0 {
			t.Errorf("rule %q has no scope in the ceiling table — it is ungated", rule)
		}
		for inTest, ceiling := range byTest {
			if ceiling < 0 {
				t.Errorf("rule %q scope test=%v has a negative ceiling %d", rule, inTest, ceiling)
			}
		}
	}
	for _, r := range AllRules {
		if _, ok := gateCeilings[r]; !ok {
			t.Errorf("rule %q has no ceiling — it is ungated", r)
		}
	}
}
