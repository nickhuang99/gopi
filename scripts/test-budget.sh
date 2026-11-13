#!/usr/bin/env bash
# test-budget.sh — run the Go suite with ONE explicit, single-source
# per-package test budget, and make a TIMEOUT say so in the output.
#
# WHY THIS SCRIPT EXISTS
# ----------------------
# `go test` gives every package a WALL-CLOCK budget and Go's default is 10
# minutes. On this host that default is not a hang detector, it is a load
# detector: the same tree that finishes internal/tools in ~189-291 s on a quiet
# box takes >600 s when other agents' suites are running (measured: FAIL
# `internal/tools 600.3xx s` at load 42-63, with a `panic: test timed out`
# dump of idle database/sql connectionOpener goroutines). The reported verdict
# is then `FAIL github.com/nick/gopi/internal/tools 600.3xx s` — text that
# reads like a failing assertion while proving nothing about the code (Rule 15:
# a failure must NAME itself).
#
# So the budget is declared here, ONCE (Rule 1a/2 — no `-timeout 25m` sprinkled
# through ad-hoc invocations), and a trip is reported as a TIMEOUT with its own
# exit code, never as a bare FAIL.
#
# The budget is generous by design: it never makes a run faster (a green suite
# exits as soon as its last test passes — the cap is only consulted when a test
# is still running). It exists so that a genuine hang is still caught, at a
# wall-clock distance a loaded 4-core host cannot reach.
#
# Usage:
#   scripts/test-budget.sh                       # whole module (./...)
#   scripts/test-budget.sh ./internal/tools/     # one package
#   scripts/test-budget.sh -run TestTask ./internal/tools/
#   scripts/test-budget.sh --latest              # print the newest completed run's stream path
#   scripts/test-budget.sh --scope-check <stream.json> [pattern...]
#                                                # does this stream carry only the
#                                                # requested scope? (exit 4 = no)
#
# Env:
#   GOPI_TEST_TIMEOUT   per-package budget override — the ONLY reader (Rule 2).
#   GOPI_TEST_BUDGET_STAMP  stamp file override (default: scratch queue).
#   CGO_ENABLED         honoured if set; default 0 (matches the declared build).
#
# Exit codes (a timeout is NEVER 1):
#   0  green
#   1  a test or the build failed (an assertion/compile failure)
#   2  usage/setup error (bad budget, scratch root unavailable)
#   3  TIMEOUT — the per-package budget was exhausted (named in the output)
#   4  --scope-check only: the stream carries out-of-scope packages
#   5  NO TEST EXECUTED — the run passed a filter (-run/-skip/-bench) and the
#      whole run executed ZERO tests, so "PASS" means "the filter matched
#      nothing", not "the code is green" (named in the output)
#   6  NOT MEASURED — `go test` produced NO STRUCTURED EVENT at all, so this run
#      never measured the tree and no exit status can speak for it (the stream
#      is 0 bytes, or it holds only unstructured text — the shape a FULL
#      partition produces, where go refuses before the test framework starts;
#      named in the output, with go's own words quoted)
#   7  DISK EXHAUSTION — a build of a package was REFUSED BY THE FILESYSTEM, so
#      the `[build failed]` lines in this run are fabrications: the code was
#      never compiled and no test verdict about it exists (its own code, so a
#      reader can branch on it without parsing prose; named in the output, with
#      the compiler's own rows quoted and both filesystems' figures). Without
#      this code a full disk was indistinguishable from a real compile error —
#      every red observed while the disk is full is a FALSE RED for the repo.
#
# Output artifacts (PER-RUN FIRST; the shared name is a POINTER, never a target):
#   <scratch queue>/test-budget/run-<epoch>-<pid>.json
#                                               THIS run's raw `go test -json`
#                                               stream: the redirect target from
#                                               the very start, and the file the
#                                               summary below is rendered from.
#                                               Unique per run, so two concurrent
#                                               budget runs cannot interleave into
#                                               one file at all.
#   <scratch queue>/test-budget/last.json       a SYMLINK to the newest completed
#                                               run's stream — the canonical
#                                               "latest" as a POST-HOC pointer.
#                                               Never opened for writing, which is
#                                               what removes the shared target.
#                                               Resolve it with `--latest`.
#   <scratch queue>/test-budget/stamps.log      one appended line per run:
#                                               time, package, budget, elapsed,
#                                               load average before/after, stream name,
#                                               outcome
#                                               COMPLETION ONLY: a run that was
#                                               killed has no line here, which is
#                                               what makes "did this run finish?"
#                                               a question with an answer.
#   <scratch queue>/test-budget/run-<epoch>-<pid>-start.txt
#                                               THIS run's START record, written
#                                               (atomically) BEFORE `go test`
#                                               starts, so a KILLED run is still
#                                               queryable: it carries start/pid/
#                                               host/pkg/budget and the STREAM
#                                               name. Not a stamp — see below.
#   <scratch queue>/test-budget/run-<epoch>-<pid>-{scope,stream,out-of-scope}-packages.txt
#                                               the scope check's inputs and its
#                                               verdict, per run (see below)
#
# A stream carrying a package OUTSIDE the requested scope is reported as an
# anomaly ("STREAM OUT OF SCOPE") instead of being summarised as this run's own
# evidence: a shared artifact is exactly what produces that, and a reader must
# hear it before quoting the numbers. The comparison lives in
# scripts/lib/stream-scope.sh, so it is callable on its own
# (`--scope-check <stream.json> [pattern...]`) and testable with synthetic input.

set -uo pipefail

# --- the budget (Rule 1a: a named constant; this is its ONLY occurrence) ----
# 45m — RE-DERIVED 2026-10-15 against this block's own criterion, which is the
# sentence two paragraphs down: the budget "exists so that a genuine hang is
# still caught, at a wall-clock distance a loaded 4-core host cannot reach".
# The criterion is a property of the MEASURED DISTRIBUTION of the slowest
# package on a loaded host, not a round number, so the constant is set from
# measurements and the arithmetic is written out here rather than remembered:
#
#   ./cmd/agent, the slowest package in the repo (it carries the CLI e2e
#   fixtures: ~360 ledger-opening fixtures and 560 test files, ~2817 top-level
#   arms). Measured on this host (4 cores), full scope `./cmd/agent/`:
#     - 2026-10-15T07:03, load 2.8 -> 10.7 (pipeline: other agents' suites
#       ramping up)   : THE 25m CAP EXPIRED, exit 3, ΣElapsed 1493.0s of the
#                      1500s cap, 2660 of 2817 arms reported, zero assertion
#                      failures. stream run-1792019037-1777144.json
#     - 2026-10-15T09:03, load 4.9 -> 1.1 (a normally loaded host: load ~5 on
#       4 cores is the ordinary state here, well under the load 42-63 that the
#       older calibration quoted): 993s wall, PASS, zero failures. stream
#                       run-1792026228-2214329.json
#   So the package's own measured demand on a normally loaded 4-core host is
#   993s (16m33s), and at the heavier end of ordinary load it is >=1500s and
#   still climbing when the cap arrived.
#
#   THE DERIVATION, stated so the next reader can re-do it from the streams:
#     demand_quiet   = 993s   (one full pass, load ~5, this host)
#     demand_loaded  >= 1500s (it did NOT finish; the 25m cap ended it)
#     criterion      = a distance a LOADED 4-core host cannot reach
#     need           > load multiplier x demand. The same tree spans roughly
#                      1.5x-3x between a quiet and a spread-out host (the older
#                      table recorded 290s -> 1123s for internal/tools), i.e.
#                      cmd/agent's loaded demand is 1500s-3000s.
#     45m = 2700s clears the whole measured span with room for a third agent
#     arriving mid-run, and is still a bounded distance from a real hang: an
#     unbounded hang is now caught by 45m instead of 25m. The cost of an
#     over-generous cap is TIME on a package that truly hung; the cost of a
#     tight one is a FALSE timeout verdict on a package that was merely slow,
#     and this repo has already paid that cost once.
# The previous value (25m / 1500s) was set as "2.5x the default" against a
# calibration for internal/tools that no longer describes this tree's slowest
# package — a constant derived from a sentence, not from the distribution. It
# is recorded above rather than deleted, because it is the value whose
# expiry produced the measurement the new one is derived from.
test_budget="45m"

# Checked before use so a typo fails HERE, not silently as Go's default.
case "${GOPI_TEST_TIMEOUT:-}" in
    "") : ;;
    [0-9]*m | [0-9]*s | [0-9]*h | [0-9]*ms | [0-9]*us | [0-9]*ns)
        test_budget="$GOPI_TEST_TIMEOUT"
        ;;
    *)
        echo "test-budget: GOPI_TEST_TIMEOUT='$GOPI_TEST_TIMEOUT' is not a Go duration (e.g. 25m, 1500s)" >&2
        exit 2
        ;;
esac

# --- report/shape constants (Rule 1a) ---------------------------------------
default_package="./..."
slowest_report_n=15
budget_subdir="test-budget"
# THIS run's stream. The name carries the start epoch AND the pid: the epoch
# alone is only second-granular, and two budget runs launched inside the same
# second would then share a file and interleave into it — the very defect this
# naming removes (measured 2026-09-19: one `last.json` held 2422 cmd/agent
# events followed by 1328 internal/tools events from a concurrent run). Age-GC'd
# with the rest of the scratch queue.
raw_json_prefix="run-"
raw_json_suffix=".json"
# The tested binary this run builds and hands to every package (Rule 1a/14):
# one build per RUN, so a multi-package run verifies ONE revision instead of
# each package resolving whatever <module root>/bin/gopi it happens to reach.
tested_bin_dir_suffix="-tested-bin"
tested_bin_name="gopi"
# Per-package constants of the timings field, so the two writers that emit a
# machine-readable line agree on one format string (Rule 1a; the gate greps it).
timings_prefix="TIMINGS "
timings_field_sep="|"
timings_key_sep=":"
timings_pkg_key="pkg"
timings_sec_key="sec"
timings_wall_key="wall"
# The canonical "latest" name. It is a POINTER to a completed run's stream
# (a relative symlink), never a redirect target: a shared write target is what
# let a concurrent run's events land in the file a summary was rendered from.
# Kept because readers (scripts/heavy-drift-report.sh, the smoke e2e) and
# operators refer to it by name; creating it is idempotent, so concurrent runs
# need no lock — each either creates or replaces the same link to its OWN file.
latest_link_name="last.json"
stamp_name="stamps.log"
# THIS run's START record — the one artifact written BEFORE `go test` starts.
#
# WHY IT EXISTS (measured 2026-10-13, task 01a12f82): a run's stream is created
# by the redirect from the very first byte, but EVERYTHING that names it — the
# stamps.log line, the last.json pointer, the printed summary — is written after
# `go test` RETURNS. A run killed mid-suite (its own `timeout`, a Ctrl-C, a
# SIGKILL from a parent job) therefore leaves an orphaned stream: `<scratch
# queue>/test-budget/run-<epoch>-<pid>.json` holds the run's real verdict
# (`fail internal/standards 150.298`) while NOTHING on disk says that file
# belongs to a run, when it started, or what scope it was asked for. The verdict
# survives; only the query that would find it does not — the one build gate that
# was RED at 15:26 became invisible, and the next report quoted a later filtered
# run instead. That is Rule 15's silent failure at the level of the RUN.
#
# So the run's identity is written FIRST, as a flat line-oriented record beside
# the stream (its own file, so nothing about it is shared between concurrent
# runs — the same defect the per-run stream name removed). It SURVIVES SIGKILL
# by construction: it is complete and on disk before `go test` is exec'd, and
# no trap or cleanup removes it — the stream it names is itself evidence, and a
# record that disappears on the way out is not durable.
#
# It carries start (epoch + ISO), pid, host, the requested scope, the budget,
# and the STREAM NAME — the stream stays the evidence, this only makes the
# stream FINDABLE. Deliberately NOT a second stamp: a line is appended to
# stamps.log on COMPLETION and only on completion, so "is there a stamps.log
# line for this run?" remains the answer to "did it finish?" (Rule 14: the two
# jobs stay in two places, and the completed-run stamp contract is untouched).
start_record_suffix="-start.txt"
# The label of the line that names the stream, inside the start record. It is
# the field a reader greps for, and what the gate pins.
start_record_stream_label="stream="
# The heading the start record opens with, so a reader arriving at the file
# cold knows which side of the run it was written on.
start_record_heading="--- start record: written BEFORE go test started (survives a kill) ---"
# --- concurrency report (Rule 1b) -------------------------------------------
# The glob that finds the OTHER runs' start records in this run's own artifact
# directory. It is the existing record every run already writes; nothing new is
# created for this check (Rule 35), and `budget_overlap_records_glob` is the ONE
# spelling of "a start record" used by the reader below.
budget_overlap_records_glob="run-*-start.txt"
# The warning's headline — what a reader greps for, and what the gate pins. It
# says CONCURRENT and OVERLAP (not "busy host") because that is the exact
# condition that makes a red un-attributable, and any weaker word would be a
# warning a reader learns to ignore.
overlap_marker="CONCURRENT BUDGET RUN OVERLAPS THIS SCOPE"
# The two facts a reader must take away when the marker prints: this run does NOT
# refuse to start, and a red seen in the window is not a verdict about anybody's
# change. Both are constants rather than prose in the middle of the block so the
# gate pins the exact sentence (a reword that drops the caveat is a regression).
overlap_starts_anyway_note="This run STARTS ANYWAY: a later run never cancels an earlier one, and stopping a neighbour is not this run's call."
overlap_attribution_note="a red inside the overlap window does NOT belong to any one run's change (重叠窗口内的红不作代码归属) — such a red is a verdict only when it is REPRODUCED ALONE, after the peer finishes."
# How many peer runs the warning names, so a burst of neighbours cannot turn the
# header into a wall of rows. The count is printed beside it, so a truncated list
# can never read as the whole list (Rule 15).
overlap_report_n=5
timeout_panic_marker="panic: test timed out after"
# The scope-anomaly report (see report_stream_scope_anomaly): the qualifier that
# marks a package as outside the requested scope, and the headline a reader sees.
anomaly_marker="STREAM OUT OF SCOPE"
# The module's import path — the prefix every `go test -json` Package value
# carries, so the requested scope is expressed as import paths and compared
# against the stream's own values. It is READ FROM go.mod at run time, never
# assumed: the branch-scoped smoke gate executes this script inside a FIXTURE
# module (github.com/nick/gopi/smokefixture), where a hardcoded repo path would
# mark every package out of scope — a warning that cries wolf is one a reader
# learns to ignore. The constant below is the REPO'S own path, used only when
# go.mod cannot be read; the gate pins it against go.mod
# (assertBudgetScopePrefixMatchesGoMod), so the fallback cannot silently rot.
module_import_path_fallback="github.com/nick/gopi"
# The three scratch files the anomaly check works from, inside the run's own
# artifact directory (never an ambient temp path — Rule 22).
scope_pkgs_filename="scope-packages.txt"
stream_pkgs_filename="stream-packages.txt"
anomalous_pkgs_filename="out-of-scope-packages.txt"
# The shared scope predicate (Rule 14: one definition, sourced by the script and
# exercised directly by the gate). Path is repo-relative, like
# heavy_inventory_rel, so sourcing needs no path math.
stream_scope_lib="scripts/lib/stream-scope.sh"
# The shared CONCURRENCY predicate (Rule 14, same shape as the two above): "is
# another budget run on this host alive AND asking for a package I asked for".
# It reads the EXISTING start records (`run-<epoch>-<pid>-start.txt`) — the one
# artifact every run already writes before `go test` starts — so this check adds
# no file, no lock and no daemon (Rule 35); a run with no neighbour writes
# exactly what it wrote before.
#
# WHY IT EXISTS (measured 2026-10-16, bug row 01a142e4): two runs of THIS script
# were executing on this host with two packages in common (pid 2885273 asked for
# four, a neighbour session for two), both wrote start records, both ran
# `go test -json` — and neither printed a word about it. Every red inside that
# window was then quotable as one run's own verdict, which it was not: this
# repository's own rule text names the shape an observed failure ("Two go test
# sets in one run"). The check therefore REPORTS and never refuses — a later run
# must not be able to cancel an earlier one, and its own verdict stays its own.
budget_overlap_lib="scripts/lib/budget-overlap.sh"
# The shared FAILURE predicate (Rule 14, same shape): which failures this run
# actually had, read from the stream's structured events — never from the text
# shape of a `--- FAIL:` line, which a PASSING test prints whenever it quotes
# its own fixture (measured 2026-10-12: this block reported 6 failures for 5
# `Action=="fail"` events, two of the named tests not existing in the repo at
# all, and printed NOTHING for the package-level ones).
stream_failures_lib="scripts/lib/stream-failures.sh"
# The failures block's two headings. They NAME their source ("this run's own
# events") because that is the whole difference from the text harvest, and the
# gate pins both so a reword cannot quietly drop the report.
failures_heading='--- failures (from this run'"'"'s own events: Action=="fail"; the budget did NOT trip) ---'
package_failures_heading="--- package-level failures (build/panic — no test to name) ---"
# The SIGNAL-interruption section: a third, distinct verdict shape, never folded
# into the heading above.
#
# WHY IT NEEDS ITS OWN SECTION (measured 2026-10-23, bug row 01a164f6). Two
# `cmd/agent` budget runs ended with `failing tests: 0` and one package-level
# verdict, and the report accounted for them under the package-level heading with
# the sentence "a package-level verdict — a panic or a TestMain exit; no test to
# name". The cause was NEITHER: the test binary had been killed by SIGHUP, and
# `go test` records that as a package-level `fail` plus the os/exec line
# `signal: hangup`. So the report named a cause that is false about a red nobody
# had to fix, which is exactly the misattribution Rule 15 forbids even when the
# failure itself IS reported.
signal_failures_heading="--- SIGNAL INTERRUPTION (the test binary was killed — not a panic, not a TestMain exit) ---"
# One signalled package's line: the package and WHICH signal ended it. The
# signal is interpolated because the readers it sends away from each other are
# different (`SIGHUP` is a terminal/ssh teardown, `SIGTERM` is a budget timeout
# or a supervisor) and the evidence that decides it is in the next line.
signal_package_fmt="  INTERRUPTED: %s — killed by %s (no test carries a verdict: the process died)"
# The sentence stating what the interruption means for the reader, and it is the
# whole point of the classification: the tests that never ran did not FAIL.
signal_failures_note="  ^ this package's red is an INTERRUPTION, not a code verdict: the run was ended by a"
signal_failures_note2="    signal, so the tests that never reported a verdict did not fail — they never ran."
# Printed when a package is signalled but the stream carries no `signal: <name>`
# row for it. A DIFFERENT claim from "the signal is known", and it must not share
# a rendering with it (Rule 15): the classifier can know the signal from a
# test-scoped row while a package-scoped quote finds nothing, and an empty quote
# under a claim reads like a bug in the report.
signal_evidence_absent="      (the stream carries no 'signal: ' row for this package — the interruption is"
signal_evidence_absent2="       classified from the package's failing verdict; its own evidence is absent)"
# The failures block's third heading half: the package-level VERDICTS go's own
# stream recorded (every package that ended in `Action=="fail"`), so a reader
# sees which packages failed and not only which tests. It is the other half of
# the stream path printed at the end — the two together are what makes the block
# re-readable without the stream.
package_verdict_prefix="  package verdict: FAIL"
# How many of a failing test's OWN output lines the block quotes beneath its
# marker. A test's log is unbounded (measured: 220 rows for ONE failing test in
# run-1791849386-333342), and what a reader needs is the REASON — the assertion
# text, which sits at the head of the log and immediately before the marker —
# never a transcript of the whole run.
failure_body_report_n=20
# The sentence printed when a failing test's own log is EMPTY in this stream.
# It is a DIFFERENT claim from "this test failed" (Rule 15) and must not share a
# rendering with it: every `Action=="fail"` event this stream writes carries no
# Output field at all (measured: all 17 fail events of run-1791849386-333342),
# so the body can only come from the test's output rows — and when the stream
# has none, the honest report is that the verdict is recorded and its evidence
# is absent, not an empty list that reads like a green result.
failure_body_absent="      (NO output rows in this stream for this test: the 'Action\":\"fail' event carries no body of its own, so this verdict is recorded with its evidence absent — the full stream below is the only place left to look)"
# The prefix every quoted body line gets, so the quoting is visible as quoting.
failure_body_indent="      "
# The indent of a marker line, so the marker and its body read as one block.
failure_marker_indent="  "
# --- the UNATTRIBUTED OUTPUT block (see report_unattributed_fail_marks) ------
# A `--- FAIL:` line can reach this stream as a test's own TEXT — a passing test
# quoting a captured `go test` result prints exactly that shape (measured twice:
# task 01a12e58's run listed `TestCommandsDocInStep`, whose only existence is the
# arm of `TestBranchCheckAnchorArmRefusesADroppedRegeneration`; and
# `TestPublishedAnchorResolves`, quoted by `TestBranchCheckRunQuotesTheFailingPackage`).
# The runner started neither name, so neither may be reported as a failure of this
# run — and neither may be silently dropped either: the reader who counted markers
# has to be told where they went.
unattributed_heading="--- NOT failures: marker lines the runner never attributed to a test (a test PRINTED them — quoted output, not a verdict) ---"
# How many such rows the block prints. The cap keeps a test that prints a whole
# `go test` transcript from burying the failure list; the count is a named cap
# rather than a literal at the slice site (Rule 1).
unattributed_report_n=10
# The indents, so the claimed name and the enclosing test read as one block.
unattributed_marker_indent="  "
unattributed_detail_indent="      "
# The two sentences the block's rows and its trailing note are rendered with.
unattributed_row_fmt='%s--- FAIL (printed, NOT a verdict): %s  (%s)\n'
unattributed_origin_fmt="%sthe line above was printed inside the log of %s; the runner started no test of that name, so this run records no verdict about it\n"
unattributed_note='     a reader who counts the marker lines in this report counts the failing tests PLUS these quoted rows; the "failing tests" count above is the one the runner'"'"'s own events give'
# How many of the stream's timeout-panic marker lines the TIMEOUT block prints.
# The marker names the wall clock, not a test; three is enough to show one
# expiry and any repeat, and the count is a named cap rather than a literal at
# the slice site (Rule 1).
timeout_marker_report_n=3
# --- the ATTRIBUTION block (see report_attribution) -------------------------
# The ONE reader of "where did the wall-clock go", over this run's own stream.
# Repo-relative like stream_scope_lib, so sourcing needs no path math.
stream_attribution_lib="scripts/lib/stream-attribution.sh"
# The heading a reader greps for, and what the gate pins. It says WHERE the
# numbers come from (this run's own stream) because that is the whole
# difference from the heartbeat's last-started-test names, which prove nothing
# about who absorbed the wall (the bug row's own honest gap). It is printed by
# EVERY run, not only a trip: "the package took 4s of a 25m cap" is the reading
# that makes the next trip interpretable, and a block that appears only on
# failure is one nobody has read before it matters.
attribution_heading="--- where the wall-clock went (this run's own stream) ---"
# The second line of the same heading, kept separate on purpose: the heading is
# QUOTED back to the reader by the TIMEOUT block, and a heading three clauses
# long made that pointer unreadable (measured on the first post-fix trip run).
# The qualifications live here, where they still name the two limitations that
# matter: the sum is over FINISHED arms only and is not a wall clock.
attribution_qualifier="    finished arms only; subtests excluded; ELAPSED is a per-arm figure, NOT a wall; an arm the stream never announced cannot be counted here"
# How many arms per package the block names. The tail is what a reader needs;
# naming a whole suite would be a wall of text with the signal inside it.
attribution_report_n=15
# The per-package line. The label ("ΣElapsed" or its LOWER BOUND twin) is
# decided per package by the reader — a package the cap stopped mid-run has no
# Elapsed for the arms that never finished, so its sum is a floor. Rule 15: the
# partial sum is NEVER printed without the word.
attribution_pkg_fmt="  %s: %s = %ss over %d finished arm(s)"
# The line that READS the lower bound: how many arms began and never reported.
attribution_unfinished_fmt="  ^ %d arm(s) started and never finished: %s"
attribution_arm_header="  top arms by wall-clock (seconds):"
attribution_arm_fmt="  %8.2f  %s"
attribution_none_line="  (no finished test carried an Elapsed in this stream)"
# THE AGE of an arm the cap killed, and the reading that turns `running tests:`
# from a name into a duration. WHY (measured 2026-11-08, row 01a1b971): go's own
# dump prints `running tests:` with a per-name age, but that age is TRUNCATED to
# whole seconds and it is the age at the INSTANT of the dump — so the test that
# happened to be the newest starter reads `(0s)` next to a 600s package, and a
# reader concludes "it just started, so it cannot be the culprit". The stream
# carries the two timestamps that answer it properly: the arm's own `run` event
# and the `running tests:` line itself. Their difference is that arm's in-flight
# time, and it is printed beside the name so the question "in flight for 1.8s
# against a 180s cap, or for 179s?" is answered by the report instead of guessed.
attribution_age_fmt="    %s: in flight %.1fs when the cap expired (its run event %s, the cap's line %s)"
attribution_unknown_age_fmt="    %s: in flight for an UNMEASURED time — the stream carries no \`running tests:\` timestamp to measure against"
# The marker that identifies the cap's own line inside the stream. It is the
# same text go prints, so it is spelled once here and passed to the ONE reader.
attribution_running_marker="running tests:"
# The two labels of the sum, and they are different CLAIMS, not two wordings of
# one: the second is a floor. Both are spelled once (Rule 13).
attribution_label_complete="ΣElapsed"
attribution_label_lower="ΣElapsed (LOWER BOUND)"
# The sentence the TIMEOUT block uses to state that the CAP ended the run (not
# the code) and to point at the attribution block above, because that block is
# where the answer is. The heading is passed in as an argument rather than
# spelled again here (Rule 13: one definition of the heading).
timeout_cap_note="The cap stopped this run, not the code: no assertion ended it — the wall clock did."
# The calibration readings, taken from THIS run's own stream. It replaces a
# HARDCODED pair ("this suite takes ~189-291s quiet and >1123s at load 42-63")
# that had drifted away from the tree it described: those numbers were one
# measured suite at one base commit, never re-taken, and a stale calibration
# pair is worse than none because it reads as evidence about today's tree. The
# replacement is not a second remembered pair — it is read per run, from the
# stream this run wrote (Rule 38: a figure a reader acts on is re-derivable and
# names the run it came from). Three %s: per-arm elapsed accounted for (a LOWER
# BOUND where the block above says so), arms that started and never reported,
# and the number of packages in this run's stream.
timeout_attribution_reading="  per-arm elapsed accounted for before the cap: %ss (LOWER BOUND for any package the block above labels so); arms started and never reported: %s; packages in this stream: %s"
# How to use the two numbers (the retired sentence's job, without the guess):
# which package to re-run with headroom, taken from this run's own arm sums.
timeout_attribution_point="  read the '%s' block above for the packages and arms that absorbed the wall; separate 'slow' from 'stuck' by re-running ONE package with more headroom on a quiet host:"
timeout_recheck_fmt="  GOPI_TEST_TIMEOUT=45m scripts/test-budget.sh %s"
# How many of the build's own diagnostic lines are quoted per failed build.
build_output_report_n=10
# The only non-zero exit that is NOT a test verdict: `--scope-check` said the
# stream carries packages the requested scope does not cover.
scope_check_failed_exit=4
# A filtered run that executed ZERO tests: "PASS" here means "the filter matched
# nothing", not "the code is green". Its own code, so it can never be read as a
# test verdict (1) or a budget trip (3).
no_test_executed_exit=5
# A stream carrying NO STRUCTURED EVENT is not a measurement at all — the run
# never reached the test framework, so no exit status can speak for it. Its own
# code, so a reader can branch on it without parsing prose (see the "NOT
# MEASURED" block below). It is NOT 1: go exiting non-zero because the
# filesystem refused a write is not a failing assertion, and 1 is exactly the
# reading that hid the cause (measured 2026-10-12).
not_measured_exit=6
# The structured-event predicate and the sentences built on it. `go test -json`
# writes one JSON object per line, so the presence of a single `{`-prefixed line
# IS the whole test of "did the toolchain get as far as the framework". Measured
# 2026-10-12 on this host: /home sat at 100% and the run's stream was 0 bytes —
# go died writing its own build output, and the report said `executed: 0 test(s)
# ran` + `outcome=fail`, so the cause appeared NOWHERE in a report whose whole
# job is to name it.
stream_event_prefix="{"
# The two readings of that verdict, because they arrive by different doors:
# a 0-byte stream (go wrote no report at all) and a stream that holds text but
# not one structured event (go spoke, the framework never started).
not_measured_empty_shape="the stream is 0 bytes — go wrote no report at all"
# The banner headline; it is what a reader greps for, and what the gate pins.
not_measured_marker="NOT MEASURED, GO PRODUCED NO TEST EVENT"
# A run whose BUILD OR TEST was killed by the FILESYSTEM — as opposed to failing
# on its own merits — is its own verdict, and this is what names it.
#
# THE DEFECT (measured 2026-10-15, this host, /home at 100%): the Go linker's
# `link: mapping output file failed: no space left on device` arrives as
#
#     FAIL	<import path> [build failed]
#
# which is CHARACTER-FOR-CHARACTER the line a real compile error produces. Every
# red observed on a full disk is therefore a FALSE RED for the whole repository:
# the branch was never compiled, nothing about it was measured, and yet the
# report names packages as failed. The merge-time BUILD gate learned this on
# 2026-10-12 and abstains (internal/tools/merge_build_gate.go's
# mergeBuildGateDiskExhausted); the declared TEST ENTRY POINT did not, so the
# surface a developer actually runs was the one that kept lying.
#
# The markers, the diagnostic-shape guard and the both-filesystems reading are
# the SAME rules the Go gate uses, spelled once on each side and pinned together
# by a gate that reads BOTH files (internal/scratch/disk_exhaustion_markers_test.go)
# — the drift a bare literal here would cause is exactly Rule 13's subject.
#
# Matched case-insensitively: the phrase is a symptom, not an identifier, and the
# toolchain's spelling of it varies.
disk_exhaustion_markers="no space left on device|enospc|database or disk is full|disk quota exceeded"
# The shape of a COMPILER DIAGNOSTIC (`file:line:col:`). A build output carrying
# one is a statement about the REVISION, whatever else it mentions — this is the
# guard that keeps the class from swallowing a real compile failure, and the one
# that keeps a TEST'S OWN OUTPUT (which this repository's own fixtures print)
# from being read as build evidence at all: the diagnostics are read from the
# stream's `build-output` rows ONLY, never by grepping the raw text.
disk_exhaustion_diagnostic_reshaper='[0-9]+:[0-9]+:'
# The verdict's banner, and the exit code it answers with.
disk_exhaustion_marker="DISK EXHAUSTION, NOT A TEST VERDICT"
disk_exhaustion_exit=7
# The suffix of the per-run file the verdict's matched package rows are written
# to (Rule 1b: a name shape, spelled once, beside the other artifact suffixes).
disk_exhaustion_pkgs_suffix="-disk-exhaustion"

# The STALE-TESTED-BINARY NOTE (see report_freshness_refusals). Its heading, the
# cap on how much of a refusal a reader is shown, and the sentence that states the
# runner's own design — measured 2026-10-13 on run-1791849386-333342, where 12 of
# 15 failing tests were this condition and the summary named none of them.
freshness_heading="--- STALE-TESTED-BINARY REFUSALS (the failing test never ran a test body) ---"
# THE INLINE HALF — the fix for the 2026-10-16 misreading (task 01a14161). A
# refusal's failing test is marked ON ITS OWN marker line, and the conclusion is
# printed ABOVE the list, so the distinction no longer depends on the reader
# reaching a trailing paragraph: measured on that run, 8 of the failing tests were
# refusals (the tested binary was stamped 11:48:37, the tree was committed at
# 11:48:34), the block listed them exactly like real reds, and they were read as
# code failures. The reader that decides WHICH tests refused is the shared
# predicate (gopi_stream_freshness_tests) — never a second reading of the stream.
#
# The marker asserts NOTHING about the code: it names the row as a refusal and
# points at the block below, which carries the guard's own verbatim VERDICT and
# the runner's own design note. The refusal verdict itself is untouched.
refusal_inline_marker="[REFUSED: no test body ran — stale tested binary; see the STALE-TESTED-BINARY REFUSALS block below]"
# The sentence printed BEFORE the failure list when this run has refusals, with
# the count as its one %s (the shape freshness_remedy_note uses, and for the same
# reason: a count two writers must agree on is how a summary states a stale
# number).
failures_refusals_lead="  !! %s of the failing test(s) below are NOT code failures: they are STALE-TESTED-BINARY REFUSALS (no test body ran) — marked inline [REFUSED]."
# …and the one line that stops the marker from being read as "these are fine".
failures_refusals_lead_note="     They are the runner's own one-build design meeting a tree that moved under it. The remedy is at the end of this block: rebuild the tested binary and re-run."
# The lead byte of each entry in list_failure_tests' refusal INDEX — a newline,
# spelled once (a second spelling is how the index and its lookup stop agreeing).
refusal_index_lead=$'\n'
freshness_report_n=2
freshness_report_bytes=220
freshness_design_note="this script builds the tested binary ONCE, before the suite starts, and hands that one artifact to every package — so a production source that changes AFTER that build makes every e2e freshness guard refuse for the rest of the run (a red proof that mutates a source and restores it, or an editor saving a file, is enough)."
# The remedy line is a PRINTF FORMAT with one %s (the refusal count), composed
# with the line that prints it — an expanded spelling needs the count as a
# variable two writers must agree on, and a placeholder no shell checks is how a
# summary comes to state a stale number.
freshness_remedy_note="Read it as: %s failing test(s) are NOT a verdict about this tree's code — no test body ran. The guard's own VERDICT lines above name which of the two causes it is; fix that cause, rebuild, re-run."
# --- the RUN CONDITION block (see report_run_condition) ----------------------
# The heading a reader greps for, and the two sentences the block cannot be
# written without: what the comparison IS, and why the run's failures are that
# condition rather than a code verdict. Measured 2026-10-14 on master cbdaca0: a
# mid-run CONTENT change to a production source made N e2e guards refuse, and the
# summary named none of them — the failures block listed N unrelated test names.
#
# The heading names the COMPARISON, not its outcome: the block prints whenever the
# run has refusals AND the comparison was taken, and a comparison that came out
# SETTLED is as much a reading as one that came out differing (it says the
# refusals are NOT this condition). A heading that asserted the drift would
# contradict its own detail line in that case.
run_condition_heading="--- RUN CONDITION (this run's refusals, against the tested binary's drift from this tree) ---"
# The criterion sentence: the base is the artifact's CONTENT identity, compared by
# content. mtime is deliberately absent — the guard retired it (Rule 31's shape),
# and a second mtime criterion in the summary would be a second definition.
run_condition_criterion_note="drift base: the tested binary's build identity — the revision the RUNNER stamped into it (internal/buildidentity), compared by CONTENT (git diff --name-only REV -- <scope>), never by mtime. The scope is the packages THIS RUN requested; a requested package the binary does not compile is reported below as unmeasurable rather than counted as settled."
run_condition_remedy_note="Read it as: THIS RUN's own refusals, with the drift that explains all of them — the failures above are one condition (the tested binary no longer contains the tree), not N independent code verdicts. Rebuild the tested binary and re-run."
# The two causes of drift, and the sentence for the unmeasurable case. Rule 15:
# a base that cannot be read must never be reported as "settled" — the artifact
# gets no benefit of the doubt from a comparison that could not be taken.
run_condition_drift_changed="%s production source(s) in this run's scope differ from the revision the tested binary was built from (%s) — so the binary the run drove does not contain this tree:"
run_condition_drift_settled="every tracked production source in the packages THIS RUN REQUESTED (%s package(s) compared) is byte-for-byte what the tested binary was built from (%s) — the binary contains this tree's content for the scope that ran."
run_condition_drift_unmeasurable="the drift could NOT be measured (%s) — this is NOT a settled tree; nothing here says the tested binary contains the tree's production sources."
# The packages the tested binary is COMPILED FROM — the scope a content
# comparison must be taken in. A requested package outside these trees is not
# something the tested binary can be behind (a change there cannot make it stale),
# so the distinction is stated rather than silently widened or narrowed. The two
# names are the guard's own two trees (cmd/agent/gopibin_identity_guard_test.go's
# sourceTreeCmdDir/sourceTreeInternalDir) and the SAME pair its `git diff` is taken
# over, so the summary's scope and the refusal's scope are one scope (Rule 14).
run_condition_bin_scope="cmd internal"
# The same scope as the awk FILTER's regex, derived from the const above rather
# than retyped beside it (Rule 13/14: a second spelling of the scope is how a
# summary comes to compare a different tree than the guard refuses on).
run_condition_bin_scope_re="$(printf '%s' "$run_condition_bin_scope" | tr ' ' '|')"
# The test-only trees the tested binary does NOT compile, so a change under them
# cannot make it stale — the guard's own list
# (cwdProbeBinaryScopeSkippedDir, task_session_cwd_e2e_test.go), spelled here
# because the summary must apply the same exclusion or it would report drift the
# guard itself calls settled (measured: internal/testutil/legacy_residue.go).
run_condition_bin_skipped="internal/testutil internal/standards"
# The two row states the comparison below prints. `run_condition_state_settled` is
# a WORD and is deliberately not one of git's state letters (A/C/D/M/R/T/U/X), so a
# reader can never mistake "settled" for "differing" — the two halves of the same
# column must be tellable apart by shape.
run_condition_state_settled="settled"
run_condition_state_diff="differ"
# The third row state: the comparison could NOT be taken for that package (it is
# not one the tested binary compiles, or the run never ran it). It is a row rather
# than an omission, so "settled" is never printed for a package nobody compared —
# the artifact gets no benefit of the doubt (Rule 15).
run_condition_state_unmeasurable="unmeasurable"
# The states git reports for a path that DIFFERS from the compared revision. The
# list is git's own vocabulary for `diff --name-status` (Rule 1b: one spelling).
run_condition_diff_states="ACDMRTUX"
# How many rows each half of the block prints: enough to recognise the writer's
# footprint and the refusing tests, never a wall of paths.
run_condition_report_n=10
# How many bytes of go's OWN diagnostics the block quotes verbatim. Go's
# diagnostics are unstructured (build-output events carry no Package, and a
# die-before-the-framework run emits none at all), so the block quotes the TEXT
# — and says that it is quoting it (Rule 15: the cause must appear, not merely
# be pointed at).
not_measured_diagnostic_bytes=600
# How many bytes of go's stderr tail the ALWAYS-printed `executed:` line carries
# when the stream holds no structured event: a reader who stops at the summary
# must already see the real cause, not only a pointer to the block below.
executed_fallback_tail_bytes=120
# The disk figures a not-measured verdict must carry: the ARTIFACT directory's
# own filesystem AND `/`. Both, because on this host they are DIFFERENT
# partitions (measured 2026-10-12: `/` was 25% used with 131G free while /home —
# which holds every worktree, the build cache and this artifact directory — was
# 100% used with 2.9G free). A figure for `/` alone next to an ENOSPC death
# reads as a contradiction and sends the reader hunting for a code defect.
disk_root_path="/"
# The unmeasurable-figure sentence, mirroring the Go-side rendering
# (internal/tools/merge_build_gate.go's disk state): a figure that cannot be
# taken is REPORTED, never silently dropped or replaced by a default (Rule 15b).
disk_unmeasurable_fmt="%s (filesystem unmeasurable: %s)"

# --- "the filter matched nothing" is NOT a pass (Rule 15) -------------------
# `go test -run <filter>` prints `testing: warning: no tests to run` and EXITS 0
# when the filter matches no test at all, so a mistyped test name is
# indistinguishable from a real pass by exit status: both are `PASS` + `ok …`
# (measured 2026-10-12, bug row 01a12cbe-324f-75a6-860e-f32ba6a7ce39 — the
# difference is one suffix on stdout that nothing on the merge/verify path
# reads). This script IS that path's declared entry point (make test / make
# test-budget), so the discrimination lives here: a filtered run that executed
# ZERO tests gets its own exit code and a banner naming the filter.
#
# The marker is Go's OWN sentence, not a paraphrase: if upstream rewords it this
# verdict switches off silently, so the gate derives this constant out of the
# script and asserts it still occurs in a filtered stream that ran nothing
# (internal/testutil/testbudget_notests_gate_test.go) — the text stays in ONE
# place.
no_tests_marker="testing: warning: no tests to run"
# The banner headline; it is what a reader greps for, and what the gate pins.
no_match_marker="NO TEST EXECUTED, THE FILTER MATCHED NOTHING"
# The filter flags whose "matched nothing" this verdict is about. A DENYLIST on
# purpose: an unseen filter flag (a future Go release) leaves the check blind
# rather than inventing a verdict out of a flag that filters nothing.
filter_flags=(-run -skip -bench)
# The `go test -json` event action that means a test was actually EXECUTED —
# counted for every test event, so the count is Go's own report, never ours. A
# test that merely SKIPPED never emits one.
executed_action="run"
# The `go test -json` field carrying an event's ACTION, and the field carrying a
# test's NAME. The heartbeat's progress reading greps on both (Rule 1b: one
# spelling each, referenced by every reader).
action_field="Action"
test_field="Test"
# Cap on the byte window a heartbeat reads from the tail of the stream. Bounded
# on purpose (Rule 27): the stream can reach hundreds of megabytes, and the
# heartbeat must add no meaningful device work to the run it is reporting on.
heartbeat_progress_bytes="4096"
# How many executed test names the summary lists before it stops naming them.
executed_names_report_n=10

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root" || exit 2

# The run's identity, taken BEFORE anything is named after it: the stream file
# below carries this epoch, and the stamp reports it (Rule 1a: one reader of
# the clock for naming, one for reporting).
# The run's own OBSERVABLE surface (Rules 15/31): a caller watching this script
# must be able to tell "still working" from "dead" WITHOUT reading the stream.
#
# WHY (measured 2026-10-14, task 01a13875). The entry point printed its header
# only AFTER the tested-binary build (`go build ./cmd/agent/`, tens of seconds on
# a cold cache) and then ran `go test … -json >"$raw_json" 2>&1` with NO liveness
# channel at all: the whole human summary is rendered from the stream only after
# `go test` RETURNS. So the process was silent for the entire suite — the caller
# saw 0 bytes on stdout/stderr for 7+ minutes, indistinguishable from a hang, and
# the foreground `bash` tool killed the run on its own 2-minute no-output net.
# The stream was never the problem (it is written from the first byte); the
# OBSERVABLE surface was. These constants are that surface's declared shape.
heartbeat_label="heartbeat"
# Interval between heartbeat lines. Cheap on a loaded host, and an order of
# magnitude inside the measured 120 s no-output net, so the net never trips
# while `go test` is genuinely working.
heartbeat_interval_sec="10"
# The liveness poll SLICE: the heartbeat's sleep is taken in slices this long, so
# the moment `go test` exits the loop notices within a second instead of burning
# the rest of the interval — the heartbeat must not lengthen a run it observes.
heartbeat_poll_sec="1"
# Iteration cap for the heartbeat loop — a BOUND, not a timeout (Rule 27): the
# loop is ALSO guarded by `kill -0`, so it ends as soon as `go test` exits. The
# cap makes it provably finite even if the liveness probe misread; a run longer
# than cap×interval simply stops printing heartbeats and still completes, because
# `wait` — not this loop — collects the verdict.
heartbeat_max_rounds="240"
# The variable the interval may be overridden with, so a regression gate can
# drive the REAL script on a short scale instead of waiting the production
# interval out. ONE reader (Rule 2): the `case` below, which also resolves the
# value the header prints, so the header can never describe a different interval
# from the one the loop sleeps.
heartbeat_interval_env="GOPI_TEST_BUDGET_HEARTBEAT_SEC"
# Positive WHOLE seconds only: the loop bounds its slices with an integer
# comparison, and an interval of 0 would make the heartbeat a busy spin. A value
# that cannot be honoured is refused by name (Rule 15) — never silently ignored.
case "${!heartbeat_interval_env:-}" in
    "") : ;;
    *[!0-9]* | 0)
        echo "test-budget: $heartbeat_interval_env='${!heartbeat_interval_env}' is not a positive whole number of seconds" >&2
        exit 2
        ;;
    *)
        heartbeat_interval_sec="${!heartbeat_interval_env}"
        ;;
esac

start_epoch="$(date +%s)"
start_iso="$(date -Is)"

# Scratch contract: artifacts land under the shared, off-`/`, age-GC'd scratch
# queue — never an ambient temp path (see scripts/lib/tmp-ban.sh).
# shellcheck source=lib/tmp-ban.sh
. "$root/scripts/lib/tmp-ban.sh"
# The heavy-classification floor + inventory path, shared with
# scripts/heavy-derive.sh (Rule 14: one definition, sourced by each consumer).
# shellcheck source=lib/heavy-threshold.sh
. "$root/scripts/lib/heavy-threshold.sh"
# The stream-scope predicate (out-of-scope package detection), shared with the
# gate so the mingled case is asserted rather than waited for.
# shellcheck source=lib/stream-scope.sh
. "$root/$stream_scope_lib"
# The failure predicate (README-level: which failures this run had), shared with
# the gate for the same reason — a synthetic stream can carry a PASSING test that
# prints a `--- FAIL:` line, and that case must be asserted, not waited for.
# shellcheck source=lib/stream-failures.sh
. "$root/$stream_failures_lib"
# The attribution reader ("where did the wall-clock go"), shared with the gate so
# the lower-bound criterion and the subtest exclusion are asserted against a
# synthetic stream instead of trusted (Rule 14).
# shellcheck source=lib/stream-attribution.sh
. "$root/$stream_attribution_lib"
# The concurrency predicate (is another budget run on this host ALIVE and asking
# for a package this run asks for), shared with the gate for the same reason as
# its three siblings: a synthetic neighbour record makes the overlap case
# ASSERTED rather than waited for.
# shellcheck source=lib/budget-overlap.sh
. "$root/$budget_overlap_lib"
budget_dir="$(gopi_scratch_queue)/$budget_subdir"
if ! mkdir -p "$budget_dir"; then
    echo "test-budget: cannot create artifact directory $budget_dir" >&2
    exit 2
fi
gopi_assert_off_root_fs "$budget_dir" || exit 2
stamp_file="${GOPI_TEST_BUDGET_STAMP:-$budget_dir/$stamp_name}"
# Every artifact of THIS run is named after its own tag (start epoch + pid), so
# no two runs share a *write target* — not the stream, not the scratch files the
# scope check works from (a fixed helper name would be the same defect one file
# over).
run_tag="$raw_json_prefix${start_epoch}-$$"
# The stream `go test -json` is redirected into from the very first byte; unique
# at creation, so no `>` can truncate a neighbour's file and no event from
# another scope can land in the stream this run summarises.
raw_json="$budget_dir/$run_tag$raw_json_suffix"
# THIS run's start record, beside its stream and named after the same tag.
start_record="$budget_dir/$run_tag$start_record_suffix"
# Where report_disk_exhaustion writes the package rows its verdict matched. It is
# per-run for the same reason the stream is: two concurrent runs must never read
# each other's verdict (Rule 22 — a scratch artifact under the scratch tree, never
# a fixed name in a shared directory).
disk_exhaustion_pkgs_file="$budget_dir/$run_tag$disk_exhaustion_pkgs_suffix"
scope_pkgs_file="$budget_dir/$run_tag-$scope_pkgs_filename"
stream_pkgs_file="$budget_dir/$run_tag-$stream_pkgs_filename"
anomalous_pkgs_file="$budget_dir/$run_tag-$anomalous_pkgs_filename"
# The canonical "latest" NAME (also what the docs header lists). Read-only:
# nothing is ever redirected into it. It is pointed at THIS run's completed
# stream by a relative symlink further down (publish_latest_stream), which is
# why it may briefly not exist between two runs' link swaps.
latest_json="$budget_dir/$latest_link_name"
inventory_path="$root/$heavy_inventory_rel"

# The module import path is the prefix every `go test -json` Package value
# carries. Read from the RUNNING module's go.mod (the script is executed inside
# fixture modules too), falling back to the repo's own path when go.mod is not
# readable — and SAYING SO, so a wrong scope can never be silently assumed
# (Rule 15). Both values are grounded, not guessed: the constant is pinned to
# the repo go.mod by the gate.
module_import_path="$(awk '/^module[[:space:]]/ {print $2; exit}' "$root/go.mod" 2>/dev/null)"
if [ -z "$module_import_path" ]; then
    echo "test-budget: NOTE: no 'module' line in $root/go.mod — the stream-scope check falls back to" >&2
    echo "test-budget:       $module_import_path_fallback (packages from another module would be" >&2
    echo "test-budget:       reported out of scope); the summary below is unaffected." >&2
    module_import_path="$module_import_path_fallback"
fi

# Prerequisite: the CLI end-to-end guards in cmd/agent + internal/testutil drive
# a REAL binary. THIS script builds it, from THIS tree, ONCE for the whole run,
# and hands it to every package through GOPI_TEST_BIN (internal/testutil pins
# GOPI_TEST_BIN, then a per-case GOPI_BIN, then <repo>/bin/gopi).
#
# Why the runner builds it instead of exporting GOPI_BIN="$root/bin/gopi"
# (measured 2026-10-03, task 01a1023c): $root here is the worktree the script is
# executed in, but the e2e cases resolve the module root by walking up from
# their OWN CWD — and in a shared checkout that reachable root is a run-garden
# worktree whose bin/gopi was built from a DIFFERENT revision. The same code
# then went green in one two-package run and RED in the next, each run verifying
# a binary nobody had built: the failing run reported an OLD binary's production
# refusal (its tree lacked commit 1d901244) as a flake of the tree under test. A
# binary this script builds is the only one whose revision the run can state.
# build_tested_binary is the run's FIRST slow step (a cold-cache `go build` of
# ./cmd/agent/), so it is wrapped in a function and called AFTER the header is
# printed — a header printed after it would be a header a caller watching the
# run never sees (see report_run_header).
build_tested_binary() {
tested_bin_dir="$budget_dir/$run_tag$tested_bin_dir_suffix"
# The tested binary is STAMPED with the revision it is built from. Why: the Go
# toolchain writes `vcs.revision` only in a checkout whose `.git` is a DIRECTORY,
# and a task ground is a linked worktree (`.git` is a FILE) — so a worktree build
# carries no identity at all (devdoc/binary-identity-on-a-worktree-ground.md).
# The stamp is what lets cmd/agent's freshness guard (gopibin_identity_guard_test.go)
# compare CONTENT — this tree's HEAD — instead of mtimes, which is the whole
# point of it: an mtime cannot tell a byte-identical rewrite from a real edit.
#
# The operand's path half is composed from the module's own source of truth
# (`go list` on the declaring package) and the variable name is named here once
# (Rule 1b), never retyped as a literal import path: the linker checks no
# operand, so a stale path would stamp NOTHING at exit 0 with empty stderr.
tested_bin_identity_var="Revision"
tested_bin_identity_pkg="./internal/version"
# The operand as the LINKER RECORDS IT (`go version -m` prints `-X <pkg>.<var>=`).
# One spelling for the writer (the build below) and the reader
# (gopi_tested_bin_revision), so the two cannot drift apart (Rule 13/14).
tested_bin_identity_qualifier="$tested_bin_identity_pkg.$tested_bin_identity_var"
# The reader's needle: the qualified operand, with the linker's own `-X ` prefix
# dropped, because the reader searches for the operand's VALUE and takes the
# characters that follow — the prefix is not part of what identifies the value.
tested_bin_identity_operand="${tested_bin_identity_qualifier#./}="
tested_ldflag_args=()
tested_bin_rev="$(git -C "$root" rev-parse HEAD 2>/dev/null || true)"
tested_bin_pkg="$(cd "$root" && go list -f '{{.ImportPath}}' "$tested_bin_identity_pkg" 2>/dev/null || true)"
if [ -n "$tested_bin_rev" ] && [ -n "$tested_bin_pkg" ]; then
    tested_ldflag_args=(-ldflags "-X $tested_bin_pkg.$tested_bin_identity_var=$tested_bin_rev")
else
    echo "test-budget: NOTE: no build identity for the tested binary ($tested_bin_dir/$tested_bin_name):" >&2
    echo "test-budget:       rev=${tested_bin_rev:-<none>} pkg=${tested_bin_pkg:-<none>}. The freshness guards in" >&2
    echo "test-budget:       cmd/agent then fall back to comparing mtimes, which cannot tell a byte-identical" >&2
    echo "test-budget:       rewrite from a real edit — the E2E verdicts below are weaker than they look, and" >&2
    echo "test-budget:       each failure that rests on them says so." >&2
fi
if mkdir -p "$tested_bin_dir" && CGO_ENABLED=0 go build ${tested_ldflag_args[@]+"${tested_ldflag_args[@]}"} -o "$tested_bin_dir/$tested_bin_name" ./cmd/agent/; then
    export GOPI_TEST_BIN="$tested_bin_dir/$tested_bin_name"
else
    echo "test-budget: NOTE: building the tested binary ($tested_bin_dir/$tested_bin_name) FAILED — the CLI" >&2
    echo "test-budget:       E2E guards fall back to <repo>/bin/gopi, whose revision this run did not build." >&2
    echo "test-budget:       Their verdict is then not evidence about this tree; fix the build first." >&2
fi
}

read_load() {
    awk '{print $1, $2, $3}' /proc/loadavg 2>/dev/null || printf 'n/a n/a n/a\n'
}

# report_run_header prints the run's identity BEFORE anything slow starts. It is
# a FUNCTION called early (just before the tested-binary build), not a block at
# the bottom, because the build and the suite are the two long silences: a header
# printed after them is a header a caller watching the run never sees in time
# (Rule 15 — the run's own existence must be observable while it is alive).
#
# Every value it prints is a global already resolved by the time it is called.
report_run_header() {
    # The requested scope as RESOLVED import paths, from the same predicate the
    # out-of-scope check uses (Rule 14: one definition). A reader of a filtered
    # run needs it to tell "this package carries no test files" from "the filter
    # matched nothing here" — both print `no tests to run` for a package.
    scope_paths="$(gopi_scope_packages "$module_import_path" "${args[@]}" | tr '\n' ' ')"
    echo "=== gopi test budget run ==="
    echo "packages: ${args[*]}"
    echo "scope:    ${scope_paths:-<none resolved>}"
    echo "budget:   $test_budget per package (scripts/test-budget.sh test_budget; override GOPI_TEST_TIMEOUT)"
    echo "host:     load $load_start  cores $cores"
    echo "raw json: $raw_json"
    echo "          (THIS run's stream only; ${latest_link_name} is a pointer to the newest completed run)"
    echo "$heartbeat_label: every ${heartbeat_interval_sec}s while go test runs (${heartbeat_interval_env} overrides)"
    echo
    # The concurrency report rides the header, because the header is where a
    # caller is ALREADY looking for "what is this run about to do" — and because
    # the two long silences that follow (the build, the suite) are exactly the
    # window in which nobody would otherwise learn that a neighbour is running.
    #
    # It is called as the body of an `if` and NOT bare: a check that could not
    # inspect the host returns non-zero, and Rule 15 says that state must be
    # REPORTED rather than left looking like "no neighbour found" — the one
    # silent failure this block must not commit, because its whole job is to
    # stop a reader from trusting an unqualified red.
    if ! report_concurrent_overlap; then
        echo "test-budget: NOTE: the concurrent-overlap check did NOT run (see the line above) —" >&2
        echo "test-budget:       this header says nothing about other budget runs on this host." >&2
    fi
}

# report_concurrent_overlap names the OTHER budget runs that are alive on this
# host AND asking for a package this run asks for.
#
# WHY IT IS A WARNING AND NEVER A REFUSAL (the epic's constraint, and the reason
# it is worth stating in code rather than in a commit message): whichever run
# started first must not win. A run that refused to start when it saw a neighbour
# would make a neighbour's start decide a third party's verdict — and "there is
# another go test somewhere on this host" is not by itself a reason to run
# nothing at all. So the run always proceeds; what it cannot do is proceed
# SILENTLY, because the cost of the silence is paid later, by whoever reads the
# red.
#
# WHY THE RED LOSES ITS MEANING (the measured defect, bug row 01a142e4): two runs
# with two packages in common both wrote streams, and a failure produced inside
# that window is a fact about the HOST, not about either diff — this repository's
# own rule text names "Two go test sets in one run" an observed failure. The
# warning therefore does not merely say "a neighbour exists"; it says what the
# neighbour does to this run's reds, and what restores them (a solo re-run).
#
# WHAT IT COSTS NOTHING: the input is the start records every run already writes,
# read once, and liveness is the process table (Rule 24 — never the ppid). No
# lock, no daemon, no registry, no new artifact (Rule 35); a run with no
# neighbour prints what it printed before.
report_concurrent_overlap() {
    # scope_pkgs_file is this run's own resolved scope, written through the same
    # predicate the stream-scope check uses (Rule 14: one definition). It is not
    # a new artifact — it is the file the post-run scope check already owns, in
    # the same per-run directory.
    gopi_scope_packages "$module_import_path" "${args[@]}" >"$scope_pkgs_file"
    if [ ! -s "$scope_pkgs_file" ]; then
        # A vacuous check that LOOKS green is the silent failure this whole block
        # exists to remove: no readable scope means the comparison never ran, and
        # the caller is told that (it prints the NOTE) rather than left with a
        # header that reads as "no neighbour found" (Rule 15).
        echo "test-budget: NOTE: no package pattern could be read from the command line ('${args[*]}')" >&2
        return 1
    fi
    local -a peer_records=()
    local record
    for record in "$budget_dir"/$budget_overlap_records_glob; do
        # An unmatched glob stays a literal, and a record that vanished between
        # the glob and the read is not evidence of anything: both are skipped
        # explicitly instead of being handed to the reader as a path.
        if [ -f "$record" ] && [ "$record" != "$start_record" ]; then
            peer_records+=("$record")
        fi
    done
    if [ "${#peer_records[@]}" -eq 0 ]; then
        return 0
    fi
    local overlapping_rows all_rows live_peers overlap_n
    overlapping_rows="$(gopi_budget_overlapping_runs "$module_import_path" "$scope_pkgs_file" "$$" "${peer_records[@]}")"
    # The same stream carries a row per live peer whether or not it overlaps, so
    # the two quiet cases stay distinguishable: no row at all = nobody was alive
    # (a run genuinely alone), rows with an empty overlap field = neighbours were
    # running, none of them on this run's packages. Only the first is silent; the
    # second is worth one line, because "another go test is on this host" already
    # weakens a wall-clock verdict even when the package sets are disjoint.
    live_peers="$(printf '%s\n' "$overlapping_rows" | grep -c . || true)"
    case "$live_peers" in
        '' | *[!0-9]*) live_peers=0 ;;
    esac
    if [ "$live_peers" -eq 0 ]; then
        # The quiet header a run genuinely alone is entitled to: no live peer at
        # all. (A live peer on DISJOINT packages is NOT silent — see below.)
        return 0
    fi
    local sep="$gopi_budget_overlap_sep"
    all_rows="$(printf '%s\n' "$overlapping_rows" | awk -F"$sep" -v n="$overlap_report_n" '
        NR <= n { print }
        NF && $4 != "" { hit++ }
        END { if (NR > n) printf "--%s%d\t<not shown>\t\t\n", "more live run(s): ", NR - n }
    ')"
    local hits
    hits="$(printf '%s\n' "$overlapping_rows" | awk -F"$sep" 'NF && $4 != "" { n++ } END { print n + 0 }')"
    if [ "$hits" -eq 0 ]; then
        echo "note: $live_peers other test-budget run(s) are ALIVE on this host, on DISJOINT packages:"
        printf '%s\n' "$all_rows" | awk -F"$sep" '{ printf "        pid=%s started %s pkg=%s\n", $1, $2, $3 }'
        echo "      (their packages do not intersect this run's, so no overlap caveat applies)"
        echo
        return 0
    fi

    overlap_n="$hits"
    echo
    echo "!!!!!!!!!!!!!!!! $overlap_marker !!!!!!!!!!!!!!!!"
    echo "$overlap_n of $live_peers other test-budget run(s) alive on this host asked for packages THIS RUN ALSO ASKS FOR:"
    # One line per peer, one line per fact. Every awk program in this function is
    # written WITHOUT a line that holds a lone closing brace: the gate's shell
    # body reader closes a function at the first line whose trimmed text is `}`,
    # so a pretty-printed awk block would cut the reporter short (measured while
    # writing this: an indented `}` at the end of a printf block truncated the
    # body before the two caveat sentences).
    printf '%s\n' "$all_rows" | awk -F"$sep" '
        $1 == "" { next }
        { printf "  %s\n      its packages:  %s\n      shared:        %s\n      started:       %s\n", $1, $3, $4, $2 }
    '
    echo "  this run's packages: ${args[*]}"
    echo "$overlap_starts_anyway_note"
    echo "$overlap_attribution_note"
    echo "to settle a red seen here: wait for those pid(s) to exit, then re-run this scope alone."
    echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
    echo
    return 0
}

# run_go_test_with_heartbeat execs the suite in the BACKGROUND and keeps the
# caller informed while it runs. Two properties are load-bearing and neither may
# be traded away:
#
#  1. THE STREAM IS UNTOUCHED. `go test … -json` still has its stdout AND stderr
#     redirected straight into "$raw_json" — the heartbeat writes to THIS
#     script's stdout, never into the stream. The run's evidence stays a pure
#     `go test -json` stream, so every reader (the summary below, the scope
#     check, the failure report, --scope-check) keeps parsing exactly what it
#     parsed before. That is why the heartbeat is a sibling of the redirect and
#     not a `tee` into it.
#
#  2. THE VERDICT IS STILL `go test`'s OWN. The child is backgrounded only so the
#     parent has a loop to run in; `wait` collects the child's status and THAT is
#     the run's status, so the exit-code contract (0/1/2/3/4/5/6) is unchanged.
#
# The loop is bounded twice — by `kill -0` (the child exiting ends it at once)
# and by heartbeat_max_rounds (a provable ceiling, Rule 27). It generates no
# load: one `wc -c` over a 4 KiB tail window per interval.
heartbeat_while_running() {
    local pid="${1:?heartbeat_while_running needs the pid of the suite}"
    local round=0 waited beat_epoch beat_elapsed
    while [ "$round" -lt "$heartbeat_max_rounds" ]; do
        # The interval is slept in SLICES, re-checking liveness in each one: the
        # loop must notice the suite's exit within one slice, or the heartbeat
        # would LENGTHEN the run it exists to observe (up to a whole interval of
        # dead time after `go test` returned). `wait` still collects the status.
        waited=0
        while [ "$waited" -lt "$heartbeat_interval_sec" ]; do
            sleep "$heartbeat_poll_sec"
            kill -0 "$pid" 2>/dev/null || return 0
            waited=$((waited + heartbeat_poll_sec))
        done
        round=$((round + 1))
        beat_epoch="$(date +%s)"
        beat_elapsed=$((beat_epoch - start_epoch))
        printf '%s t+%ss: go test still running (%s)\n' \
            "$heartbeat_label" "$beat_elapsed" "$(stream_progress_of "$raw_json")"
    done
    return 0
}

summarise_json() {
    command -v python3 >/dev/null 2>&1 || return 0
    python3 - "$1" "$slowest_report_n" <<'PY'
import json, sys

path, n = sys.argv[1], int(sys.argv[2])
tests, failed_pkgs, pkgs = {}, set(), set()
with open(path, errors="replace") as fh:
    for line in fh:
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        action, pkg, test = ev.get("Action"), ev.get("Package", "?"), ev.get("Test")
        if action in ("pass", "fail"):
            pkgs.add(pkg)
            if action == "fail":
                failed_pkgs.add(pkg)
            if test:
                tests[(pkg, test)] = ev.get("Elapsed", 0.0)
        elif action == "skip" and test:
            tests.pop((pkg, test), None)

print("packages: %d finished, %d with a failure" % (len(pkgs), len(failed_pkgs)))
for pkg in sorted(failed_pkgs):
    print("  FAILED: %s" % pkg)
slowest = sorted(tests.items(), key=lambda kv: kv[1], reverse=True)[:n]
if slowest:
    print("slowest %d tests (wall-clock seconds):" % len(slowest))
    for (pkg, test), sec in slowest:
        print("  %7.2f  %-24s %s" % (sec, pkg.rsplit("/", 1)[-1], test))
PY
}

# args_request_filter reports whether the run's own arguments narrow the test
# set through one of filter_flags. It is the precondition of the emptiness
# verdict: with no filter the run executes the packages' whole test sets, so "0
# tests executed" would mean something else entirely (a package set with no test
# files, e.g. `?   pkg  [no test files]`) and must keep its old reading.
args_request_filter() {
    local a
    for a in "$@"; do
        local f
        for f in "${filter_flags[@]}"; do
            case "$a" in
                "$f" | "$f"=*) return 0 ;;
            esac
        done
    done
    return 1
}

# count_executed_tests prints the number of tests the run actually EXECUTED,
# read from the run's own `go test -json` stream: one event per test as it
# starts, so the count is Go's report rather than this script's inference. A
# test that merely skipped never emits a start event and is not counted — the
# verdict is about execution.
count_executed_tests() {
    grep -a -c -- "\"Action\":\"$executed_action\",\"Package\"" "$raw_json" 2>/dev/null || true
}

# count_stream_events prints how many STRUCTURED EVENT lines this run's stream
# carries. `go test -json` writes exactly one JSON object per line and nothing
# else, so ONE line starting with `{` is the whole test of "did the toolchain
# get as far as the test framework" — and its absence is the difference between
# "the suite ran zero tests" and "the suite never ran at all" (see
# report_not_measured). A 0-byte stream reads 0 here, which is why the two
# shapes share one verdict rather than needing two counts.
count_stream_events() {
    grep -a -c -- "^$stream_event_prefix" "$raw_json" 2>/dev/null || true
}

# stream_bytes prints the size of this run's stream, so the verdict can state
# WHICH shape it saw (0 bytes vs non-empty-with-no-events) instead of asserting
# a cause the evidence does not carry.
stream_bytes() {
    wc -c <"$raw_json" 2>/dev/null | tr -d ' \n'
}

# stream_progress_of prints a BOUNDED reading of path, for a heartbeat line: how
# many bytes the run's stream holds and the most recent test that started. It is
# what makes a heartbeat informative rather than a bare "still alive" — the
# caller sees the suite advancing, and sees it stop advancing.
#
# READING THE TAIL, NOT THE FILE, is deliberate (Rule 27's spirit: measure the
# thing, never manufacture load): a stream reaches hundreds of megabytes, and a
# full-file scan every interval would put real device work on the shared disk
# for no signal a reader needs. The window is capped by a named constant, so the
# heartbeat's cost is bounded no matter how long the suite runs. A window that
# holds no `run` event yet says so rather than printing an empty field.
stream_progress_of() {
    local path="${1:?stream_progress_of needs a path}"
    local bytes last
    bytes="$(wc -c <"$path" 2>/dev/null | tr -d ' \n')"
    case "$bytes" in
        '' | *[!0-9]*) bytes=0 ;;
    esac
    # Two greps, not one: `go test -json` puts `Package` BETWEEN `Action` and
    # `Test` on a test event line, so an adjacency match on
    # `"run":"Test"` matches nothing at all — measured on a real stream
    # (`{"Action":"run","Package":"…","Test":"…"}`). The first grep selects the
    # events of interest; the second pulls the field out of them, and `tail -n 1`
    # (never `head`) keeps a closed consumer from raising the pipeline's status
    # through `set -o pipefail` and landing SIGPIPE noise in a green run's output
    # — the defect testbudget_report_pipe_gate_test.go pins.
    last="$(tail -c "$heartbeat_progress_bytes" "$path" 2>/dev/null |
        grep -a -- "\"$action_field\":\"$executed_action\"" 2>/dev/null |
        grep -a -o -- "\"$test_field\":\"[^\"]*\"" 2>/dev/null |
        tail -n 1 | sed 's/.*:"//; s/"$//')"
    printf '%s bytes, last test started: %s' "$bytes" "${last:-<none yet>}"
}

# quote_stream_diagnostics prints Go's OWN words from the stream, verbatim and
# bounded — the whole point of the not-measured verdict. Go's diagnostics are
# UNSTRUCTURED by design: a `build-output` event carries no Package field, and a
# run that died before the framework emitted no event at all, so the only place
# the cause exists is the raw text. Bounded because a full stream can be
# hundreds of megabytes; the reader gets the head plus the path (Rule 15: the
# cause appears, and the pointer to the rest is explicit). NUL bytes are
# dropped so the quote cannot turn a terminal into a binary sink.
quote_stream_diagnostics() {
    head -c "$not_measured_diagnostic_bytes" "$raw_json" 2>/dev/null | tr -d '\000'
}

# report_disk_space_of renders ONE filesystem figure for path: the path, the
# device id that decides WHICH filesystem it is (Rule 22 is judged by device id,
# never by a spelling), and the free space in bytes — a human's `df -h` rounding
# is exactly what hides a 2G remainder. A figure that cannot be taken is
# REPORTED inside the string (Rule 15b), never silently dropped and never
# replaced by a default.
report_disk_space_of() {
    local path="$1" dev free why=""
    dev="$(stat -c %d "$path" 2>/dev/null || true)"
    free="$(df -P -B1 "$path" 2>/dev/null | awk 'NR > 1 { v = $4 } END { print v }')"
    [ -n "$dev" ] || why="stat -c %d failed"
    [ -n "$free" ] || why="${why:+$why; }df -P -B1 failed"
    if [ -n "$why" ]; then
        printf "$disk_unmeasurable_fmt" "$path" "$why"
        return 0
    fi
    printf '%s (device %s, %s bytes free)' "$path" "$dev" "$free"
}

# report_disk_state prints the TWO figures a not-measured verdict must carry:
# the artifact directory's own filesystem and `/`. Both, because on this host
# they are different partitions (the artifact directory, every worktree and the
# go build cache live on /home; `/` is a separate disk) — and a figure for `/`
# alone next to an ENOSPC death reads as a contradiction, sending the reader
# after a code defect that is not there.
report_disk_state() {
    printf 'disk:     %s; %s\n' "$(report_disk_space_of "$budget_dir")" "$(report_disk_space_of "$disk_root_path")"
}

# report_not_measured is the Rule 15 half of the new verdict: when `go test`
# produced NO STRUCTURED EVENT, this run measured NOTHING about the tree, and
# the report must say exactly that — with go's own words and the disk figures —
# rather than rendering `executed: 0 test(s) ran` + `outcome=fail`, which reads
# as "the suite is broken/configured wrong" (measured 2026-10-12: the report of
# a run whose stream was 0 bytes, on a host whose /home was 100% full, named the
# cause NOWHERE). It reports; the caller decides the exit code.
report_not_measured() {
    local shape="$1"
    echo
    echo "=================== $not_measured_marker ==================="
    echo "This run did NOT measure the tree: go test produced NO structured event, so"
    echo "the test framework never started and NOTHING was verified."
    echo "  shape:  $shape"
    echo "  stream: $(basename "$raw_json") ($(stream_bytes) byte(s), $(count_stream_events) structured event(s))"
    echo "  go exit status: $status — recorded, but it cannot speak for a run that never"
    echo "                  got as far as a measurement."
    echo
    echo "go's OWN words from that stream (verbatim, first $not_measured_diagnostic_bytes bytes):"
    quote_stream_diagnostics | sed 's/^/  /'
    echo "  (the whole stream, which is the run's evidence, stays at the path printed in"
    echo "   the header above; nothing was deleted)"
    echo
    report_disk_state
    echo
    echo "Usual causes, and how to tell them apart:"
    echo "  * a FULL filesystem — the two figures above say whether this file's own"
    echo "    partition ran out; on this host the worktrees, the go build cache, TMPDIR"
    echo "    and the scratch queue all share ONE partition, so filling it stops go"
    echo "    before it can write a single event."
    echo "  * go never started at all (a shell/toolchain/module-cache problem) — then"
    echo "    the quoted text above is a 'command not found' or a toolchain refusal."
    echo
    echo "Fix the cause, then re-run the SAME command — this verdict is not a test"
    echo "result and must never be reported as one (see the exit-code list in"
    echo "scripts/test-budget.sh, where 6 is this verdict and nothing else)."
    echo "=========================================================================="
}

# list_executed_tests prints up to executed_names_report_n names of the tests
# that executed, one per line (`package.Test`). The verdict NAMES its evidence:
# a reader must see which tests ran, not only how many.
list_executed_tests() {
    local rows sorted
    # The dedupe+slice is taken with sort/awk on captured text, NOT `… | sort -u |
    # head -n`: `head` exits at the cap and leaves `sort` writing into a closed
    # pipe, and the shell prints `sort: write failed: 'standard output': Broken
    # pipe` / `sort: write error` right in the middle of a GREEN report — noise
    # that reads like a defect of the run under report (Rule 15). Same shape as
    # the body slice in list_failure_tests below.
    rows="$(grep -a -o -- "\"Action\":\"$executed_action\",\"Package\":\"[^\"]*\",\"Test\":\"[^\"]*\"" "$raw_json" 2>/dev/null |
        sed 's/.*"Package":"//; s/","Test":"/  /; s/"$//')"
    [ -n "$rows" ] || return 0
    sorted="$(printf '%s\n' "$rows" | sort -u)"
    awk -v n="$executed_names_report_n" 'NR <= n' <<<"$sorted"
}

# report_no_test_executed is the Rule 15 half of the emptiness verdict: when a
# FILTERED run executed zero tests, say so by name — the filter, the marker Go
# printed, and what the exit code means. It reports; the caller decides the exit
# code.
report_no_test_executed() {
    echo
    echo "=================== $no_match_marker ==================="
    echo "This run passed a test filter, and NOT ONE test was executed:"
    echo "  filter requested: $(filter_args_of "$@" | tr '\n' ' ')"
    echo "  go reported:      \"$no_tests_marker\""
    echo "  tests executed:   0 (counted from this run's own stream: $(basename "$raw_json"))"
    echo
    echo "A mistyped or over-narrow filter is the usual cause, and Go exits 0 for"
    echo "it — so this verdict is NOT 'the code is green'. Nothing was verified."
    echo "  Check the name against the tree:  go test -list '.*' <the same package patterns>"
    echo "(that listing ignores -run, so it shows the package's real test names; note"
    echo "that with -list the run only LISTS tests by design — then nothing running is"
    echo "expected, and the exit code is this script saying so.)"
    echo "Every other exit code keeps its own meaning: 1 = assertion/compile failure,"
    echo "3 = the per-package budget expired."
    echo "=========================================================================="
}

# filter_args_of prints the filter flag and its value as a pair per line — the
# filter a reader must repair. It steps over the package patterns, which are
# not part of the filter.
filter_args_of() {
    local a prev="" f skip_next=0
    for a in "$@"; do
        if [ "$skip_next" -eq 1 ]; then
            printf '%s %s\n' "$prev" "$a"
            skip_next=0
            continue
        fi
        for f in "${filter_flags[@]}"; do
            case "$a" in
                "$f")
                    prev="$a"
                    skip_next=1
                    ;;
                "$f"=*)
                    # The glued spelling (`-run=^TestX$`) carries flag and value
                    # in one argv, so there is nothing to step over.
                    printf '%s\n' "$a"
                    ;;
            esac
        done
    done
}

# It stays quiet where a full smoke run cannot produce evidence: with heavy
# tests enabled every heavy test RUNS (so its duration says nothing about the
# classification), and a narrowed package scope (make test-budget PKGS=...) 
# reads a partial stream whose slow set is not the suite's.
# report_unclassified_slow names the DRIFT the inventory cannot see: tests at
# or above the classification floor that are NOT in the inventory. Delegated to
# scripts/heavy-drift-report.sh (one definition of the comparison, shared with
# the manual invocation documented in devdoc/heavy-tests-rederivation.md).
#
# report_attribution prints WHERE THE WALL-CLOCK WENT, from this run's own
# stream — the reading the pre-fix runner never produced (a trip said
# `outcome=timeout` and left the reader to guess which package and which arms
# absorbed the cap; the heartbeat's last-started names prove nothing, which is
# the bug row's own honest gap).
#
# It is printed by EVERY run, green or not. Two reasons, and the second is the
# one that matters: (a) a green run's attribution is what makes the NEXT trip
# interpretable — the same package's same sum is the baseline the cap is
# compared against; (b) a block that exists only on failure has never been read
# on a day it worked, so its first appearance is also its first test. The cost
# is one capped pass over a stream the run already wrote (Rule 27: no device
# work is manufactured, and no new measurement is taken).
#
# The two claims it can make are kept apart by their LABELS, never by tone:
# a package the cap stopped mid-run carries "ΣElapsed (LOWER BOUND)" and the
# count of arms that began and never reported, so the floor is visible as a
# floor (Rule 15).
report_attribution() {
    echo "$attribution_heading"
    echo "$attribution_qualifier"
    gopi_stream_attribution "$raw_json" "$attribution_report_n" \
        "$attribution_pkg_fmt" "$attribution_unfinished_fmt" \
        "$attribution_arm_header" "$attribution_arm_fmt" "$attribution_none_line" \
        "$attribution_label_lower" "$attribution_label_complete" "$timeout_panic_marker" \
        "$attribution_running_marker" "$attribution_age_fmt" "$attribution_unknown_age_fmt"
}

# attribution_totals prints TWO readings taken from THIS run's own stream, for
# the TIMEOUT block's calibration sentence: the summed per-arm Elapsed of every
# terminal test event, and how many packages the stream names. It replaces a
# HARDCODED pair ("this suite takes ~189-291s quiet and >1123s at load 42-63")
# that had drifted away from the tree it described: those numbers are one
# measured suite at one base commit, they were never re-taken, and a stale
# calibration pair is worse than none because it reads as evidence. The
# replacement is not a second remembered pair — it is read here, per run, from
# the stream this run wrote (Rule 38: a figure a reader acts on is re-derivable
# and names the run it came from).
#
# The sum counts EVERY terminal event, subtests included, because it is a
# statement about the stream's own content ("per-arm elapsed"), and it says so
# by name — it is a LOWER BOUND on the wall, not a wall.
attribution_totals() {
    command -v python3 >/dev/null 2>&1 || return 0
    [ -s "$raw_json" ] || return 0
    python3 - "$raw_json" <<'PY'
import json, sys
total, pkgs, unfinished = 0.0, set(), 0
begun, done = set(), set()
with open(sys.argv[1], errors="replace") as fh:
    for line in fh:
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        pkg, test, action = ev.get("Package"), ev.get("Test"), ev.get("Action")
        if pkg:
            pkgs.add(pkg)
        if not test:
            continue
        if action == "run":
            begun.add((pkg, test))
        elif action in ("pass", "fail", "skip"):
            done.add((pkg, test))
            total += ev.get("Elapsed", 0.0)
unfinished = sum(1 for key in begun if key not in done and "/" not in key[1])
print("%.1f %d %d" % (total, len(pkgs), unfinished))
PY
}

# attribution_busiest_package prints the ONE package to name in the "re-run with
# more headroom" line: the package with the largest finished-arm sum in this
# run's stream. It is chosen from the run's own evidence rather than typed as a
# package name, so the sentence cannot point at a package this run never ran
# (the retired text said `./<pkg>/`, a placeholder a tired reader fills in
# wrongly).
attribution_busiest_package() {
    command -v python3 >/dev/null 2>&1 || return 0
    [ -s "$raw_json" ] || return 0
    python3 - "$raw_json" "$module_import_path" <<'PY'
import json, sys
path, module = sys.argv[1], sys.argv[2]
sums, order = {}, []
with open(path, errors="replace") as fh:
    for line in fh:
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        pkg, test = ev.get("Package"), ev.get("Test")
        if not pkg or not test or "/" in test:
            continue
        sums.setdefault(pkg, 0.0)
        if ev.get("Action") in ("pass", "fail", "skip"):
            sums[pkg] += ev.get("Elapsed", 0.0)
if not sums:
    sys.exit(0)
best = max(sums, key=lambda p: sums[p])
# Reported as the pattern a reader would type: the module-relative directory,
# with a trailing slash — the same spelling the entry point's own scope uses.
print("./" + best[len(module) + 1:] + "/" if best.startswith(module + "/") else best)
PY
}

report_unclassified_slow() {
    [ -x "$root/$heavy_drift_report" ] || return 0
    scope="${args[*]}"
    [ -z "$scope" ] && scope="$default_package"
    [ "$scope" = "$default_package" ] || return 0
    [ -n "${GOPI_HEAVY_TESTS:-}" ] && return 0
    echo "unclassified-slow drift — read from THIS run's stream (${raw_json##*/}):"
    "$root/$heavy_drift_report" "$raw_json" || true
}

# report_failures prints the run's failures from the stream's STRUCTURED events
# (Action/Test/Package), which is the only reading that can tell a verdict from
# the same text appearing as DATA. The old shape here was `grep -a -o --
# '--- FAIL: [^"\\]*'` over the whole stream, and a PASSING test prints exactly
# that line whenever it quotes its own fixture (measured 2026-10-12 on this
# script's own narrow run: 6 reported failures for 5 `Action=="fail"` events,
# and 2 of the 6 named tests do not exist in the repository — `go test
# ./internal/tools/ -list` lists neither). The package-level failures the old
# block could NOT print at all (a package that never compiled emits no Test
# field, so no marker line ever carried it) are reported by the second block
# below, and the compiler's own diagnostics are quoted with them (Rule 15).
report_failures() {
    local n
    n="$(gopi_stream_failure_count "$raw_json")"
    # THE REFUSAL-HEAD COUNT, read BEFORE a single failure row is printed. It is
    # the same number report_freshness_refusals prints BELOW the list, and it is
    # read FIRST on purpose: the conclusion has to sit ABOVE the rows it corrects,
    # because a reader (and an agent grepping the report) counts `--- FAIL:` lines
    # as defects and never reaches a trailing paragraph. Measured 2026-10-16 on
    # task 01a14161: 8 of that run's failing tests were refusals, the block said
    # so only after the list, and the failing lines were read as code reds.
    local refusals
    refusals="$(gopi_stream_freshness_refusal_count "$raw_json")"
    if [ ! -s "$raw_json" ]; then
        # No stream, no evidence — say THAT, rather than printing an empty list
        # that reads like a green run (Rule 15).
        echo "$failures_heading"
        echo "  (no readable stream: $raw_json — see the NOTE above; the failure list cannot be read)"
        return 0
    fi
    echo "$failures_heading"
    echo "  failing tests: $n"
    if [ "$n" -gt 0 ]; then
        # THE CONCLUSION BEFORE THE LIST, when this run has refusals: the rows
        # under it are marked individually (list_failure_tests) AND counted here,
        # so `grep -c FAIL` cannot take a refusal for a defect even if the reader
        # never looks at the marker line's own annotation.
        if [ "${refusals:-0}" -gt 0 ]; then
            printf "$failures_refusals_lead\n" "$refusals"
            echo "$failures_refusals_lead_note"
        fi
        # Readable text kept from the marker line ("--- FAIL: <test>"), with the
        # package beside it — the reader still sees a failure the way `go test`
        # prints it, it is simply no longer harvested that way. Each marker is
        # followed by THAT TEST'S OWN output (Rule 15), which is the half this
        # block was missing.
        list_failure_tests
        if [ "$n" -gt "$slowest_report_n" ]; then
            echo "  … and $((n - slowest_report_n)) more (the full list is in the stream below)"
        fi
    fi
    report_package_verdicts
    report_signal_interruptions
    report_package_failures
    # The rows that reached the stream and are NOT verdicts: a `--- FAIL:` line a
    # test printed. Printed LAST, under its own heading, because it corrects the
    # reader's count of markers rather than adding a failure (see
    # report_unattributed_fail_marks).
    report_unattributed_fail_marks
    echo "full stream: $raw_json"
}

# report_freshness_refusals names the failures that never executed a test body:
# the CLI e2e guards' STALE-TESTED-BINARY refusal, where the tested binary is
# older than a production source and the test therefore refuses to run at all.
#
# WHY THE SUMMARY OWES THE READER THIS BLOCK (measured 2026-10-13, this script's
# own run-1791849386-333342, `./internal/agent/ ./cmd/agent/`). Twelve of that
# run's fifteen failing tests were this refusal, and the failures block printed
# them as bare `--- FAIL:` markers — a reader sees twelve red tests and a red
# package, i.e. a code verdict, while the run had executed no test body for any
# of them. The runner is also the party that CAUSED it and the only party that
# knows its own design: it builds the tested binary ONCE (tested_bin_dir above),
# so a source rewritten after that build poisons every guard downstream —
# including a source rewritten by the run's own red proof, which is exactly what
# happened there (sed -i on internal/agent/clock.go 1.6s after the build,
# restored 2m16s later; the refusals name the restored mtime).
#
# It CLASSIFIES NOTHING ITSELF. The guard's refusal carries a VERDICT line that
# distinguishes "the tree moved while the suite ran" from "the binary predates
# the edit"; re-deriving that here would be a second definition of a value the
# guard owns (Rule 14), and a commentary copy that drifts from what the test
# printed is worse than none. The block quotes the verdict lines verbatim and
# caps how much of the refusal it prints (freshness_report_n/…_bytes), because
# the full text is a paragraph per test.
report_freshness_refusals() {
    local n refusals verdicts artifacts bin_line bin_mtime
    n="$(gopi_stream_freshness_refusal_count "$raw_json")"
    [ "${n:-0}" -gt 0 ] || return 0
    refusals="$(gopi_stream_freshness_refusals "$raw_json" | cut -f3)"
    echo
    echo "$freshness_heading"
    echo "  $n distinct refusal(s) in this stream — that many failing test(s) never ran a test body."
    # The artifact and the ONE build the runner does, so the reader can see the
    # runner's side of the comparison without opening the stream. The artifact is
    # read from the REFUSALS too (a refusal names the binary it refused), because
    # the tested binary this run built and the artifact a refusal is about are the
    # same thing only when the build succeeded.
    artifacts="$(gopi_stream_freshness_artifacts "$raw_json")"
    if [ -n "$artifacts" ]; then
        printf '%s\n' "$artifacts" | sed "s|^|  the refusal(s) name this artifact: |"
    fi
    bin_line=""
    bin_mtime="$(stat -c %y "$GOPI_TEST_BIN" 2>/dev/null | cut -d. -f1)"
    [ -n "${GOPI_TEST_BIN:-}" ] && bin_line="$GOPI_TEST_BIN"
    if [ -n "$bin_line" ] && [ -n "$bin_mtime" ]; then
        echo "  tested binary: $bin_line (mtime $bin_mtime)"
    fi
    echo "  runner's design: $freshness_design_note"
    # The guard's own verdicts, verbatim and de-duplicated: the ONE place the two
    # causes are told apart, and both are relayed when a run saw both.
    verdicts="$(gopi_stream_freshness_verdicts "$raw_json")"
    if [ -n "$verdicts" ]; then
        echo "  the guard's own verdict(s), quoted from the stream:"
        printf '%s\n' "$verdicts" | awk -v indent="$failure_body_indent" 'NF { print indent $0 }'
    else
        echo "  (no VERDICT line in the stream's refusals — the refusals below carry the numbers that decide it)"
    fi
    # shellcheck disable=SC2059  # the format IS the constant above (Rule 1a): one %s, the refusal count
    printf "  $freshness_remedy_note\n" "$n"
    # A cap that truncates nothing is not worth naming: the header states the
    # count only when it is actually withholding refusals.
    if [ "$n" -gt "$freshness_report_n" ]; then
        echo "  refusal(s), verbatim ($freshness_report_n of $n — the rest are in the stream):"
    else
        echo "  refusal(s), verbatim:"
    fi
    printf '%s\n' "$refusals" | awk -v n="$freshness_report_n" -v indent="$failure_body_indent" \
        'NR <= n { print indent substr($0, 1, '"$freshness_report_bytes"') }'
    echo "full stream: $raw_json"
}

# report_disk_exhaustion prints the DISK EXHAUSTION verdict and returns 0 when
# the run's OWN build evidence says the filesystem refused a write.
#
# IT IS A PREDICATE AND A REPORT IN ONE (the caller branches on the status), so
# the sentence a reader gets and the verdict the script exits with can never
# disagree.
#
# WHY IT IS NOT "A FAILURE WITH A DIFFERENT WORD" (the whole design): the claim
# is made from the STREAM'S OWN STRUCTURED EVIDENCE, never from a grep of the raw
# text — a passing test that PRINTS a disk-exhaustion sentence (this repository's
# own gates print such fixtures) would satisfy a grep, and then a green run would
# be reported as a full disk. So the evidence is read from the `build-output` rows
# of `build`-failed packages, and the CLASS is claimed only when the build carries
# NO COMPILER DIAGNOSTIC at all: a diagnostic pointing at file:line:col means the
# revision WAS compiled and the failure is about the code, whatever else the
# output mentions. That is the Go gate's own rule (mergeBuildGateDiskExhausted),
# restated on the surface a developer runs.
report_disk_exhaustion() {
    local records kind pkg detail marker hit=0
    records="$(gopi_stream_failure_records "$raw_json")"
    [ -n "$records" ] || return 1
    while IFS="$stream_col_sep" read -r kind pkg detail; do
        [ "$kind" = "$stream_kind_build" ] || continue
        # A diagnostic in this package's own compiler output makes the failure a
        # statement about the code — not the filesystem (see the header).
        if printf '%s' "$detail" | grep -Eq -- "$disk_exhaustion_diagnostic_reshaper"; then
            continue
        fi
        marker="$(printf '%s' "$detail" | grep -Eio -- "$disk_exhaustion_markers" | head -n 1)"
        [ -n "$marker" ] || continue
        hit=1
        printf '%s\t%s\n' "$pkg" "$marker"
    done <<< "$records" >"$disk_exhaustion_pkgs_file"
    [ "$hit" -eq 1 ] || return 1

    echo
    echo "=================== $disk_exhaustion_marker ==================="
    echo "The FILESYSTEM refused a write. The package(s) below did NOT fail on their"
    echo "own merits: nothing about this tree was compiled, so nothing about it was"
    echo "measured. Every '[build failed]' line above is therefore a FALSE RED."
    echo
    echo "go's OWN words from this run's stream (the compiler's rows, verbatim):"
    print_disk_exhaustion_evidence
    echo
    report_disk_state
    echo
    echo "the marker(s) matched, and the package(s) that carried them:"
    sed 's/^/  /' "$disk_exhaustion_pkgs_file"
    echo
    echo "This is an ENVIRONMENT failure, not a code verdict. Free space, then re-run"
    echo "the SAME command — and never report this line as 'tests failed' (see the"
    echo "exit-code list in scripts/test-budget.sh, where $disk_exhaustion_exit is this"
    echo "verdict and nothing else)."
    echo "========================================================================"
    return 0
}

# print_disk_exhaustion_evidence quotes the COMPILER'S OWN ROWS (the stream's
# build-output events) for the packages the verdict is about — never the whole
# stream's text, which would quote some other package's or some test's output and
# read as evidence for this claim.
print_disk_exhaustion_evidence() {
    local pkg printed=0
    while IFS="$stream_col_sep" read -r pkg _; do
        [ -n "$pkg" ] || continue
        gopi_stream_build_output_for "$raw_json" "$pkg" | sed 's/^/  /'
        printed=1
    done <"$disk_exhaustion_pkgs_file"
    if [ "$printed" -eq 0 ]; then
        # A build can be refused before the toolchain prints any diagnostic row:
        # the refusal IS the absence of a diagnostic, which the block states
        # rather than leaving blank (Rule 15).
        echo "  (the stream carries no compiler diagnostic for them at all — the write was"
        echo "   refused before any diagnostic could be printed, which is exactly how a full"
        echo "   filesystem presents itself)"
        quote_stream_diagnostics | sed 's/^/  /'
    fi
}

# report_run_condition prints the RUN CONDITION: the tested binary no longer
# contains the tree the suite ran against, WITH this run's own refusals — the
# reading that turns N unattributed failures into ONE named condition.
#
# WHY THE SUMMARY OWES THIS BLOCK (measured 2026-10-14, task 01a13c43, master
# cbdaca0): the runner builds the tested binary ONCE, before the suite starts, and
# hands that artifact to every package. A production source changed while the
# suite runs therefore makes every e2e freshness guard refuse — and on master the
# report did not name that condition at all: the failures block listed N test names
# and the reader had to work out that they shared one cause. The guard DOES decide
# it (decideBinFreshness's content arm) and its refusal names the binary, so the
# information was in the stream; what was missing was the summary's own sentence
# tying the drift to the refusals.
#
# WHY THE DRIFT BASE IS THE ARTIFACT'S BUILD IDENTITY, never an mtime. The runner
# STAMPS the tested binary with the revision it builds it from
# (build_tested_binary above, internal/buildidentity), and the guard compares
# CONTENT against that same revision. mtime was retired deliberately — it cannot
# tell a byte-identical rewrite from a real edit (devdoc: run 1791902369-3257849) —
# so a summary that drifted on mtime would be a SECOND, weaker criterion for a
# question the guard answers by content. The two readings below therefore ask the
# SAME question of the same revision, in the same scope.
#
# TWO READINGS, AND BOTH MUST BE PRESENT: the drift (what the artifact no longer
# contains) and the refusals (which tests would not verify). Either alone is a fact
# a reader cannot act on — a tree that moved under a run whose tests all passed on
# the affected package is not a condition to report, and refusals with no drift are
# the OTHER arm's subject (report_freshness_refusals, which is about mtimes and a
# moved tree). A base that cannot be measured is REPORTED, never treated as
# "settled" (Rule 15: a comparison that could not be taken gives the artifact no
# benefit of the doubt).
report_run_condition() {
    local refusals rows detail changed settled unmeasurable bin
    refusals="$(gopi_stream_freshness_tests "$raw_json")"
    [ -n "$refusals" ] || return 0
    rows=""
    detail=""
    bin="${GOPI_TEST_BIN:-}"
    gopi_tested_bin_revision "$bin"
    rev="$rev_resolved"
    if [ -n "$rev" ]; then
        rows="$(gopi_run_condition_drift "$rev" "${args[@]}")"
        changed="$(printf '%s\n' "$rows" | grep -c "^$run_condition_state_diff$stream_col_sep" || true)"
        settled="$(printf '%s\n' "$rows" | grep -c "^$run_condition_state_settled$stream_col_sep" || true)"
        unmeasurable="$(printf '%s\n' "$rows" | sed -n "s/^$run_condition_state_unmeasurable$stream_col_sep//p" | head -1)"
        if [ "${changed:-0}" -gt 0 ]; then
            detail="$(printf "$run_condition_drift_changed" "$changed" "$rev")"
        elif [ -n "$unmeasurable" ]; then
            detail="$(printf "$run_condition_drift_unmeasurable" "$unmeasurable")"
        else
            detail="$(printf "$run_condition_drift_settled" "$settled" "$rev")"
        fi
    else
        detail="$(printf "$run_condition_drift_unmeasurable" "$rev_reason")"
    fi
    echo
    echo "$run_condition_heading"
    echo "  $run_condition_criterion_note"
    printf '%s\n' "$detail" | sed 's/^/  /'
    printf '%s\n' "$rows" | awk -v n="$run_condition_report_n" -v indent="$failure_body_indent" \
        'NF >= 2 && NR <= n { print indent $0 }'
    # The refusals, per test, each naming the artifact it refused — the half that
    # says WHICH tests the condition hit, so the failures block above can be read
    # back against it.
    echo "  this run's refusals, by test (each names the artifact it would not verify):"
    printf '%s\n' "$refusals" | awk -F"$stream_col_sep" -v n="$run_condition_report_n" -v indent="$failure_body_indent" \
        'NR <= n { printf "%s--- REFUSED: %s  (%s)\n%s    would not verify: %s\n", indent, $2, $1, indent, $3 }'
    # shellcheck disable=SC2059  # the format IS the constant above (Rule 1a)
    printf "  $run_condition_remedy_note\n"
    echo "full stream: $raw_json"
}

# gopi_run_condition_pkgs ARGS… — the PACKAGE arguments among a run's arguments.
# The runner's argv carries filter flags (filter_flags: -run/-skip/-bench, each
# with a value), which are not packages and make `go list` fail; dropping them is
# what lets the comparison resolve the scope the run ACTUALLY ran (Rule 15: an
# unresolvable scope must not read as "settled").
gopi_run_condition_pkgs() {
    local skip_next=0 a f
    for a in "$@"; do
        if [ "$skip_next" -eq 1 ]; then
            skip_next=0
            continue
        fi
        for f in "${filter_flags[@]}"; do
            if [ "$a" = "$f" ]; then
                skip_next=1
                continue 2
            fi
        done
        case "$a" in
            -*) continue ;;
        esac
        printf '%s\n' "$a"
    done
}

# gopi_run_condition_pkg_is_test_only PKG — whether the import path names a tree
# the tested binary does NOT compile (run_condition_bin_skipped). It compares the
# path UNDER the module root against the guard's own list of skipped trees, so the
# summary's exclusion and the guard's are the same exclusion by construction
# (Rule 14: one value, two readers).
gopi_run_condition_pkg_is_test_only() {
    local pkg="$1" rel skip
    rel="${pkg#"$module_import_path"/}"
    [ "$rel" != "$pkg" ] || return 1
    for skip in $run_condition_bin_skipped; do
        case "$rel" in
            "$skip" | "$skip"/*) return 0 ;;
        esac
    done
    return 1
}

# gopi_run_condition_drift REV ARGS… — the drift between the tree and the revision
# the tested binary was built from, as `<state><TAB><subject>` rows, one per line:
#
#   differ<TAB><path>   a tracked production source that DIFFERS from REV, with
#                       git's own state letter in front of the path (so the row
#                       says HOW it differs, not merely that it does)
#   settled<TAB><pkg>   a requested package the tested binary compiles, all of
#                       whose sources are byte-for-byte what REV holds — printed
#                       so the block can STATE that a comparison was taken rather
#                       than merely find nothing
#   unmeasurable<TAB>…  the comparison could NOT be taken, with the reason: a
#                       requested package the tested binary does not compile, a
#                       scope that does not resolve, or a git that refused. A row
#                       rather than an omission, so "settled" is never the answer
#                       for something nobody compared (Rule 15).
#
# Only the packages the run ACTUALLY REQUESTED contribute a `settled` row, and
# only the ones under the binary's own trees; the packages the run requested but
# the binary does not compile get an `unmeasurable` row AND are named in prose by
# the caller, because they cannot make the binary stale and must not be silently
# mixed into the comparison.
gopi_run_condition_drift() {
    local rev="${1:?gopi_run_condition_drift needs the tested-binary revision}"
    shift
    local raw rc=0 pkgs requested
    requested="$(gopi_run_condition_pkgs "$@" | tr '\n' ' ')"
    pkgs="$(cd "$root" && go list $requested 2>/dev/null || true)"
    if [ -n "$requested" ] && [ -z "$pkgs" ]; then
        printf '%s%sgo list could not resolve this run'"'"'s requested packages (%s), so no package could be compared\n' \
            "$run_condition_state_unmeasurable" "$stream_col_sep" "$requested"
    fi
    printf '%s\n' "$pkgs" | while IFS= read -r pkg; do
        [ -n "$pkg" ] || continue
        case "$pkg" in
            "$module_import_path/cmd" | "$module_import_path/cmd/"* | "$module_import_path/internal" | "$module_import_path/internal/"*)
                if gopi_run_condition_pkg_is_test_only "$pkg"; then
                    printf '%s%sthis run requested %s, which the tested binary does not compile (its freshness can say nothing about the artifact)\n' \
                        "$run_condition_state_unmeasurable" "$stream_col_sep" "$pkg"
                    continue
                fi
                printf '%s%s%s\n' "$run_condition_state_settled" "$stream_col_sep" "$pkg"
                ;;
            *)
                printf '%s%sthis run requested %s, which is not under the tested binary'"'"'s source trees (%s)\n' \
                    "$run_condition_state_unmeasurable" "$stream_col_sep" "$pkg" "$run_condition_bin_scope"
                ;;
        esac
    done
    raw="$(cd "$root" && git diff --name-status "$rev" -- $run_condition_bin_scope 2>&1)" || rc=$?
    if [ "$rc" -ne 0 ]; then
        echo "test-budget: NOTE: git could not compare the tested binary's revision $rev against this tree:" >&2
        echo "test-budget:       ${raw:-no output}" >&2
        printf '%s%sgit diff --name-status %s failed: %s\n' \
            "$run_condition_state_unmeasurable" "$stream_col_sep" "$rev" "${raw:-no output}"
        return 0
    fi
    printf '%s\n' "$raw" | awk -F"$stream_col_sep" -v sep="$stream_col_sep" -v diff="$run_condition_diff_states" \
        -v diff_state="$run_condition_state_diff" -v scope_re="$run_condition_bin_scope_re" -v skipped="$run_condition_bin_skipped" '
        NF >= 2 {
            state = $1
            path = $2
            if (NF >= 3) path = $3
            if (path !~ /\.go$/ || path ~ /_test\.go$/) next
            if (index(diff, substr(state, 1, 1)) == 0) next
            dir = path
            sub(/\/[^/]*$/, "", dir)
            if (dir !~ "^(" scope_re ")(/|$)") next
            n = split(skipped, skip, " ")
            for (i = 1; i <= n; i++) if (dir == skip[i] || dir ~ "^" skip[i] "/") next
            print diff_state sep state " " path
        }'
}

# gopi_tested_bin_revision BIN — the revision the ARTIFACT carries in its embedded
# `-ldflags` setting, read from the file with the toolchain's own reader. It sets
# $rev_resolved (empty when the artifact carries no stamp) and $rev_reason (why).
#
# A GLOBAL-SETTING FUNCTION, not one whose result is captured with `$( )`: the
# reason is the caller's too, and a command substitution runs in a SUBSHELL, so a
# reason assigned in there never arrives — measured 2026-10-14 (this run's first
# end-to-end exercise): the block printed an empty drift line and `set -u` then
# aborted the report with "rev_reason: unbound variable", i.e. the failure surface
# itself failed (Rule 15: the reason must reach the reader, not the subshell).
#
# WHY THE STAMP IS READ FROM THE ARTIFACT AND NOT TAKEN FROM `tested_bin_rev`: the
# value the runner intended to stamp and the value the artifact actually carries
# are two different facts, and a stamp the linker never wrote (a stale operand
# path, a build that fell back) would make the summary compare against the wrong
# base in silence. Reading it back is what makes the drift base the artifact's OWN
# identity.
#
# The operand is located by STRING INDEX, never by a sed substitution: the operand
# carries the module's import path, whose `/` characters end a `s/…/…/` expression
# early — measured in this same run, where the reader answered
# "sed: -e expression #1, char 25: unknown option to `s'" and reported no revision
# at all (a parse failure wearing a missing stamp's clothes).
gopi_tested_bin_revision() {
    local bin="${1:-}" line i rest
    rev_resolved=""
    rev_reason=""
    if [ -z "$bin" ]; then
        rev_reason="the run built no tested binary, so it carries no identity"
        return 0
    fi
    if [ ! -f "$bin" ]; then
        rev_reason="$bin does not exist"
        return 0
    fi
    line="$(go version -m "$bin" 2>/dev/null |
        awk -v needle="$tested_bin_identity_operand" '
            { i = index($0, needle); if (i > 0) { print substr($0, i + length(needle)); exit } }')"
    rest="${line%%[!0-9a-fA-F]*}"
    if [ -z "$rest" ]; then
        rev_reason="$bin carries no $tested_bin_identity_var in its $tested_bin_identity_pkg setting (the linker operand was not written)"
        return 0
    fi
    rev_resolved="$rest"
}

# report_package_verdicts prints the packages this run's stream recorded as
# FAILED — `Action=="fail"` with no Test field, one line per package, read from
# the events rather than from the text of go's summary line.
#
# WHY BOTH THIS AND THE TEST LIST. The test list is capped at slowest_report_n
# entries (so a run with a hundred failures is still readable), and a cap is
# exactly the shape that lets a failed package leave the report unmentioned. The
# package set is never capped and never empty for a failing run — `go test -json`
# writes one verdict per package — so together the two lines are a COMPLETE
# reading of the stream, and a reader who trusts the block is not reading a
# prefix of it. Measured on run-1791849386-333342: 15 failing tests and 2 failing
# packages — `cmd/agent` accompanying 12 of them and `internal/agent` the other 3
# — so a reader who saw only the (capped, 15-marker) test list could not tell
# either package had failed.
report_package_verdicts() {
    local pkgs pkg sigs sig
    pkgs="$(gopi_stream_failed_packages "$raw_json")"
    [ -n "$pkgs" ] || return 0
    sigs="$(gopi_stream_signals "$raw_json")"
    while IFS= read -r pkg; do
        [ -n "$pkg" ] || continue
        # A package whose run a SIGNAL ended still failed from `go test`'s point
        # of view, so it is listed — but the verdict line says WHICH signal, so a
        # reader is not left holding an unqualified red (Rule 15: the same line
        # must not make an interrupted run look like a code verdict; bug row
        # 01a164f6).
        sig="$(printf '%s\n' "$sigs" | awk -F"$stream_col_sep" -v p="$pkg" '$1 == p { print $2 }' | LC_ALL=C sort -u | paste -sd'/' -)"
        if [ -n "$sig" ]; then
            printf '%s: %s (killed by %s — see the SIGNAL INTERRUPTION block)\n' "$package_verdict_prefix" "$pkg" "$sig"
            continue
        fi
        printf '%s: %s\n' "$package_verdict_prefix" "$pkg"
    done <<<"$pkgs"
}

# list_failure_tests prints each failing test as the marker line a reader
# recognises (`--- FAIL: <test>  (<package>)`), and UNDER it that test's OWN
# output rows, quoted from the stream (Rule 15).
#
# WHY THE BODY IS HERE AND NOT LEFT TO "see the stream". Before this, the block
# printed marker lines only. Measured 2026-10-13 on run-1791849386-333342: the
# `--- FAIL:` rows arrived with nothing under them, which reads exactly like a
# failure with no reason — and the reasons were in the stream all along, e.g.
# "…-tested-bin/gopi (mtime 07:56:32) is OLDER than …/internal/agent/clock.go
# (mtime 07:56:33)". A reader had to parse the JSON by hand to learn that 12 of
# the 15 failing tests were the runner's own stale pre-built binary, which is a
# defect in the REPORT as much as in the run: the block's whole job is to make a
# failure name itself, and its evidence was one field away.
#
# The quoting is visible as quoting (every line indented, read from `Action ==
# "output"` rows of THAT test in THAT package), and an empty body prints its own
# sentence — never an empty line, which would read as "there is nothing to say".
list_failure_tests() {
    local pkg test body body_lines rows refusing_index is_refused
    # WHICH of these failures are REFUSALS, read once (Rule 14: the reader that
    # decides "is this a stale-binary refusal" is the shared predicate, never a
    # second reading here) and held as an index so each marker line can be marked
    # as it is printed. The index is `\n<package><TAB><test>\n` per row, because a
    # tab is the one character neither field can contain (the stream's own
    # separator) and a newline the one that cannot appear inside either.
    refusing_index="$refusal_index_lead"
    while IFS="$stream_col_sep" read -r rpkg rtest _artifact; do
        [ -n "$rtest" ] || continue
        refusing_index="$refusing_index$rpkg$stream_col_sep$rtest$refusal_index_lead"
    done <<<"$(gopi_stream_freshness_tests "$raw_json")"
    # The LIST is sliced the same way the body below is, and for the same
    # reason: `gopi_stream_failures … | head -N` is a producer feeding an
    # early-exit consumer — `head` exits at the cap and leaves the producer
    # writing into a closed pipe, so `set -uo pipefail` prints the noise in the
    # middle of the report. A producer is captured first, and the slice is taken
    # from the captured text.
    rows="$(gopi_stream_failures "$raw_json")"
    [ -n "$rows" ] || return 0
    rows="$(awk -v n="$slowest_report_n" 'NR <= n' <<<"$rows")"
    while IFS="$stream_col_sep" read -r pkg test; do
        [ -n "$test" ] || continue
        # INLINE, BEFORE THE BODY: a refusal is marked on its OWN marker line, so
        # a reader who never reaches the summary (or greps the report) still sees
        # which rows are not code verdicts. Measured 2026-10-16 on task 01a14161:
        # the eight refusing rows were rendered exactly like the real ones and the
        # conclusion sat only in a trailing paragraph.
        is_refused=0
        case "$refusing_index" in
            *"$refusal_index_lead$pkg$stream_col_sep$test$refusal_index_lead"*) is_refused=1 ;;
        esac
        if [ "$is_refused" -eq 1 ]; then
            printf '%s--- FAIL: %s  (%s)  %s\n' \
                "$failure_marker_indent" "$test" "$pkg" "$refusal_inline_marker"
            continue
        fi
        printf '%s--- FAIL: %s  (%s)\n' "$failure_marker_indent" "$test" "$pkg"
        body="$(gopi_stream_failure_bodies "$raw_json" "$pkg" "$test")"
        if [ -z "$body" ]; then
            echo "$failure_body_absent"
            continue
        fi
        # The slice is taken with awk on a here-string, NOT `… | head -n`: a
        # pipe into `head` leaves the producer writing into a closed pipe, and
        # the shell prints a `write error: Broken pipe` right in the middle of
        # the report — noise that reads like a defect of the run under report.
        body_lines="$(printf '%s\n' "$body" | grep -c . || true)"
        awk -v n="$failure_body_report_n" -v indent="$failure_body_indent" \
            'NR <= n { print indent $0 }' <<<"$body"
        if [ "${body_lines:-0}" -gt "$failure_body_report_n" ]; then
            printf '%s… and %s more line(s) of this test'"'"'s own output (read them from the stream above)\n' \
                "$failure_body_indent" "$((body_lines - failure_body_report_n))"
        fi
    done <<<"$rows"
}

# report_unattributed_fail_marks prints the `--- FAIL:` lines this stream carries
# which the RUNNER never attributed to a test, as their own block.
#
# WHY THE BLOCK EXISTS AT ALL (the measured shape of this epic, twice). The
# failures block above is rendered from the runner's per-test events, and that is
# the only reading that can tell a verdict from the same text appearing as DATA —
# but the previous fix stopped there, so a marker line a test PRINTED simply
# vanished from the report. Measured on run-1793204886-2346679 (the run this epic
# names): the stream carries two such lines and the block listed ONE failure, with
# nothing saying where the other two went. A `grep -c '--- FAIL:'` over the report
# then counts a number that matches neither the verdicts nor the stream, and a
# reader chasing the difference has to parse the JSON by hand (Rule 15: a row the
# reader cannot see is missing is a lost row, not a filtered one).
#
# WHAT IT IS NOT. It is NOT a failure list: no count of failing tests, no verdict,
# no exit-code contribution — the budget's arithmetic and its exit semantics are
# untouched (this block only renders). Each row names the marker's CLAIMED name,
# the package whose log carried it, and the test that printed it, so a reader who
# wondered whether `TestPublishedAnchorResolves` really failed learns in one line
# that it was quoted by `TestBranchCheckRunQuotesTheFailingPackage` — which is the
# answer, and it was in the stream all along.
report_unattributed_fail_marks() {
    local rows n
    [ -s "$raw_json" ] || return 0
    rows="$(gopi_stream_unattributed_fail_marks "$raw_json")"
    [ -n "$rows" ] || return 0
    n="$(gopi_stream_unattributed_fail_mark_count "$raw_json")"
    echo "$unattributed_heading"
    awk -v n="$unattributed_report_n" -v indent="$unattributed_marker_indent" \
        -v detail="$unattributed_detail_indent" \
        -v rowfmt="$unattributed_row_fmt" -v originfmt="$unattributed_origin_fmt" '
        NR <= n {
            pkg = $1; claim = $2; owner = $3
            printf rowfmt, indent, claim, pkg
            if (owner != "") printf originfmt, detail, owner
        }
    ' <<<"$rows"
    if [ "${n:-0}" -gt "$unattributed_report_n" ]; then
        printf '  … and %s more quoted row(s) (the full stream below is the only place left to look)\n' \
            "$((n - unattributed_report_n))"
    fi
    echo "$unattributed_note"
}

# report_package_failures names the failures that have NO test to report: a
# package that never compiled (build), and a package-level verdict (a panic or a
# TestMain exit — it carries no Test field either). Both were INVISIBLE in the
# failures block before this change, which is the Rule 15 half of the fix: a
# build failure is the loudest thing a Go run can have and the block said
# nothing at all.
#
# Each section quotes THAT PACKAGE's own evidence — its compiler diagnostics
# (selected by the full import path), or for a nameless verdict its own output
# rows. Quoting the whole stream's build output under one package's heading (the
# first shape of this block) reads like the package's evidence and is not: with
# two same-basename packages each heading carried the OTHER's compiler line
# (measured: devdoc/test-budget-package-attribution.md), and a package-level
# verdict carried lines belonging to a package that merely failed to compile.
# report_signal_interruptions names the packages whose run a SIGNAL ended, under
# their OWN heading.
#
# WHY A SEPARATE FUNCTION AND A SEPARATE HEADING (measured 2026-10-23, bug row
# 01a164f6). A `cmd/agent` budget run ended `failing tests: 0` with one
# package-level verdict, and the report rendered it as "a package-level verdict —
# a panic or a TestMain exit; no test to name". Both halves were wrong: there was
# no panic (no stack trace in the stream) and no TestMain exit (`exit status N`
# is a different os/exec line), the binary had been KILLED, and the interrupted
# tests carry no verdict at all — which is why `failing tests: 0` and a red
# package arrive together. A reader who is told "panic or TestMain" goes looking
# for a defect in their own code; the classification below sends them to the
# signal, which is a fact about the RUN, not about the code.
#
# It is printed BEFORE the package-level failures block so that block's heading
# can never be the only account of an interrupted package (Rule 15).
report_signal_interruptions() {
    local rows pkg sig own listed=0
    rows="$(gopi_stream_signals "$raw_json")"
    [ -n "$rows" ] || return 0
    echo
    echo "$signal_failures_heading"
    while IFS="$stream_col_sep" read -r pkg sig; do
        [ -n "$pkg" ] || continue
        listed=1
        printf "$signal_package_fmt\n" "$pkg" "${sig:-a signal}"
        # The package's OWN evidence, quoted from the stream: os/exec's
        # `signal: <name>` line is the only thing a killed run leaves, and it is
        # what makes this section self-evidencing rather than an assertion.
        #
        # THE READER IS gopi_stream_signal_rows, not the package-output one: the
        # real run's signal row is TEST-scoped (the process died while a test was
        # live), so a package-scoped-only quote printed the section's claim with
        # the suite's startup lines under it and NOT the row that proves it —
        # measured on the replay of run-1792694665-3875354, 2026-10-23.
        own="$(gopi_stream_signal_rows "$raw_json" "$pkg")"
        if [ -z "$own" ]; then
            echo "$signal_evidence_absent"
        fi
        awk -v n="$build_output_report_n" -v indent="$failure_body_indent" \
            'NR <= n { print indent $0 }' <<<"$own"
    done <<<"$rows"
    [ "$listed" -eq 1 ] || return 0
    echo "$signal_failures_note"
    echo "$signal_failures_note2"
}

report_package_failures() {
    local rows kind pkg listed=0 own
    rows="$(gopi_stream_package_failures "$raw_json")"
    [ -n "$rows" ] || return 0
    echo "$package_failures_heading"
    while IFS="$stream_col_sep" read -r pkg kind; do
        [ -n "$pkg" ] || continue
        case "$kind" in
            "$stream_kind_signaled")
                # A SIGNAL INTERRUPTION belongs to report_signal_interruptions,
                # which prints it under its own heading. It must NOT fall through
                # to the EVIDENCE branch below: that branch's sentence says "a
                # panic or a TestMain exit", and saying it about a killed binary
                # is a false attribution of a red nobody has to fix (bug row
                # 01a164f6). Skipped, not silently: the section above printed it.
                continue
                ;;
        esac
        listed=1
        case "$kind" in
            build)
                echo "  BUILD FAILED: $pkg (the package never compiled — its tests did not run)"
                own="$(gopi_stream_build_output_for "$raw_json" "$pkg")"
                if [ -z "$own" ]; then
                    echo "      (no compiler diagnostic carries this package's import path — the"
                    echo "       stream named the build failure without quoting its output)"
                fi
                awk -v n="$build_output_report_n" -v indent="$failure_body_indent" \
                    'NR <= n { print indent $0 }' <<<"$own"
                ;;
            *)
                echo "  PACKAGE FAILED: $pkg (a package-level verdict — a panic or a TestMain exit; no test to name)"
                own="$(gopi_stream_package_output_for "$raw_json" "$pkg")"
                if [ -z "$own" ]; then
                    echo "      (the stream carries no package-level output for this package — the"
                    echo "       verdict is recorded, its evidence is not)"
                fi
                awk -v n="$build_output_report_n" -v indent="$failure_body_indent" \
                    'NR <= n { print indent $0 }' <<<"$own"
                ;;
        esac
    done <<<"$rows"
    [ "$listed" -eq 1 ] || return 0
    echo "  (a failed package is not a failed test: 'go test' prints one verdict"
    echo "   per package, and the tests inside a package that never built never ran)"
}

# fan_out_packages lists the packages present in the stream — the context for
# both blocks below (which packages were even in this run).
fan_out_packages() {
    gopi_stream_packages "$raw_json" | sed 's/^/  package in this stream: /'
}

# report_stream_scope_anomaly is the Rule 15 half of the fix: a stream that
# carries a package outside the requested scope is REPORTED as an anomaly
# instead of being summarised as this run's own evidence.
#
# Why it is needed even with per-run streams: the defect this change removes
# produced exactly this signature (measured 2026-09-19 — one stream held 2422
# cmd/agent events followed by 1328 internal/tools events from a concurrent
# run), and a shared artifact is the general cause. A reader who sees the
# anomaly knows the numbers below are not this scope's, without having to
# notice the package names themselves. It reports, never fails the run: the
# test verdict is still this run's verdict.
#
# The comparison itself lives in scripts/lib/stream-scope.sh, so the gate can
# call the SAME predicate with synthetic inputs (the mingled case is asserted,
# not waited for).
report_stream_scope_anomaly() {
    gopi_scope_packages "$module_import_path" "$@" >"$scope_pkgs_file"
    gopi_stream_packages "$raw_json" >"$stream_pkgs_file"
    if [ ! -s "$stream_pkgs_file" ]; then
        return 0
    fi
    if [ ! -s "$scope_pkgs_file" ]; then
        # No package pattern could be read out of the command line: the check
        # would be vacuous, and a vacuous check that LOOKS green is the silent
        # failure this whole report exists to remove — say so instead.
        echo "test-budget: NOTE: no package pattern could be read from the command line" >&2
        echo "test-budget:       ('$*') — the stream-scope anomaly check did not run." >&2
        return 0
    fi
    local anomalous
    anomalous="$(gopi_stream_scope_anomalies "$module_import_path" "$scope_pkgs_file" "$stream_pkgs_file")"
    if [ -z "$anomalous" ]; then
        return 0
    fi
    printf '%s\n' "$anomalous" >"$anomalous_pkgs_file"
    echo
    echo "!!!!!!!!!!!!!!!!!!! $anomaly_marker !!!!!!!!!!!!!!!!!!!"
    echo "the stream $raw_json carries package(s) THIS RUN DID NOT REQUEST:"
    printf '%s\n' "$anomalous" | sed 's/^/  out of scope: /'
    echo "requested scope: $(tr '\n' ' ' <"$scope_pkgs_file")"
    echo "The stream is not this run's alone — the summary below (slowest tests,"
    echo "package set, timeout markers) may be drawn partly from another run's"
    echo "events. DO NOT quote these numbers as this run's evidence; re-run with a"
    echo "unique artifact directory (GOPI_TEST_SCRATCH_HOME) and check the stamp's"
    echo "stream=<file> name matches the file you read."
    echo "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"
}

# publish_latest_stream points the canonical "latest" name at this run's
# COMPLETED stream: `last.json` becomes a relative symlink, never a `>` target,
# so there is no shared file for a neighbour to truncate. Atomic per run (a
# temp link renamed over the name), and deliberately last-in — a reader either
# sees the previous run's pointer or this run's, never a half-written stream.
publish_latest_stream() {
    local tmp_link="$latest_json.$$.tmp"
    if ! ln -s "$(basename "$raw_json")" "$tmp_link" 2>/dev/null; then
        echo "test-budget: could not create the pointer $tmp_link — $latest_link_name stays as it was (Rule 15b: reported, not swallowed)" >&2
        return 0
    fi
    if ! mv -f "$tmp_link" "$latest_json"; then
        echo "test-budget: could not publish $latest_json (Rule 15b: reported, not swallowed)" >&2
        rm -f "$tmp_link"
    fi
}

# seal_run_stream makes this run's COMPLETED stream read-only BEFORE the pointer
# is published, so the canonical latest name never points at a writable file.
#
# Why this is not paranoia: the per-run target removes the shared write, but the
# pointer re-introduces an ALIAS to the run's evidence, and anything that writes
# through the alias destroys a finished run's stream. Two real writers exist —
# a `go test -json > last.json` typed by hand, and an OUT-OF-DATE checkout of
# this very script (measured 2026-09-19: a concurrent stale run resolved
# last.json, its shell truncated the target, and one of two acceptance runs'
# completed streams was destroyed after the fact, silently). Sealing costs
# nothing — the file is complete the moment the run returns — and turns that
# silent overwrite into a loud `Permission denied` (Rule 15).
seal_run_stream() {
    if ! chmod a-w "$raw_json" 2>/dev/null; then
        echo "test-budget: NOTE: could not make this run's stream ($raw_json) read-only — a writer" >&2
        echo "test-budget:       following $latest_link_name could still truncate it (Rule 15b)." >&2
    fi
}

# fan_out_timeout_marker prints ONLY the budget-expiry evidence — the line that
# says a wall clock ran out. It is kept apart from the failure report so the
# TIMEOUT block never presents an ordinary failing assertion as the timeout:
# the two mean different things and a reader must not have to guess which is
# which (Rule 15 — the failure has to NAME itself). It reads the panic's own
# text, which is not a `go test -json` event at all (the marker is Go's panic
# output inside an output event), so it stays a text read — scoped to the ONE
# string that cannot occur in a passing test's fixture: the panic.
fan_out_timeout_marker() {
    # The slice is taken from captured text with awk, NOT `… | head -3`: `head`
    # exits at the cap and leaves the producer writing into a closed pipe, and
    # `set -uo pipefail` then prints a broken-pipe line in the middle of the
    # report — noise that reads like a defect of the run under report. The
    # captured text is checked before it is sliced, so a stream with no marker
    # prints NOTHING (an empty here-string would slice to one empty line).
    local found
    found="$(grep -a -o -- "$timeout_panic_marker[^\"\\]*" "$raw_json" 2>/dev/null)"
    [ -n "$found" ] || return 0
    awk -v n="$timeout_marker_report_n" 'NR <= n' <<<"$found"
}

args=("$@")

# --- the DEFAULT scope: what THIS checkout changed, never the whole module ----
#
# The operator's 2026-11-06 ruling: a caller who types the bare entry point does
# not know what it costs, and the whole module costs 688 s (measured on
# cmd/agent alone, 01a1ad2b-effe) against a 2-minute foreground budget — 5.7x,
# which is structurally un-runnable and, worse, indistinguishable from a hang.
# So the bare invocation means "test what I touched", and the whole module is a
# NAMED opt-in (`make test-all`, or an explicit `./...` argument).
#
# THREE outcomes, and the empty one is NOT a silent green:
#   changed packages found -> exactly those packages run
#   no changed packages    -> nothing to test, exit 0 WITH a stated verdict
#   git cannot answer      -> the whole module (fail LOUD, Rule 15: a scope that
#                             could not be determined is not "nothing changed")
#
# `git diff --name-only base...HEAD` is the SYMMETRIC range, so it lists what
# THIS branch changed since the merge base — not what the integration branch
# changed meanwhile (same operator as internal/smoke's diffRangeSeparator).
scope_default_base="origin/master"
scope_default_base_fallback="HEAD~1"
scope_go_suffix=".go"

# gopi_changed_packages — one package directory per line, sorted. Empty output
# means "nothing in this module changed"; a non-zero return means git could not
# answer and the caller must NOT read the empty output as "nothing changed".
#
# TWO diffs, unioned, because the working tree is where the change IS while it is
# being worked on:
#   committed   `git diff base`  — what this branch has beyond the base
#   uncommitted `git diff HEAD`  — what is in the working tree but in no commit
# Using only the first (base...HEAD) missed the ordinary case entirely: an agent
# edits a file and runs the tests BEFORE committing, so the range was empty and
# the run was skipped — a silent green over an uncommitted change, which is worse
# than the whole-tree default this replaced.
gopi_changed_packages() {
    local base="$1" files committed uncommitted
    committed="$(git -C "$root" diff --name-only "$base" -- "*$scope_go_suffix" 2>/dev/null)" || return 1
    uncommitted="$(git -C "$root" diff --name-only HEAD -- "*$scope_go_suffix" 2>/dev/null)" || return 1
    files="$(printf '%s\n%s\n' "$committed" "$uncommitted" | sed '/^$/d' | sort -u)"
    [ -n "$files" ] || { printf '%s' ""; return 0; }
    while IFS= read -r f; do
        [ -n "$f" ] || continue
        dir="$(dirname "$f")"
        printf './%s\n' "${dir#./}"
    done <<<"$files" | sort -u
}

if [ "${#args[@]}" -eq 0 ]; then
    scope_base="$scope_default_base"
    if ! git -C "$root" rev-parse --verify --quiet "$scope_base" >/dev/null 2>&1; then
        scope_base="$scope_default_base_fallback"
    fi
    if changed_pkgs="$(gopi_changed_packages "$scope_base")"; then
        if [ -z "$changed_pkgs" ]; then
            echo "test-budget: nothing to test — no .go file differs from $scope_base in this checkout." >&2
            echo "test-budget: a documentation-only change needs no test run; use 'make test-all' to run the whole module anyway." >&2
            exit 0
        fi
        mapfile -t args <<<"$changed_pkgs"
    else
        echo "test-budget: WARNING: cannot diff against $scope_base — falling back to $default_package." >&2
        echo "test-budget:          the default scope is 'what I touched'; an unknowable diff is NOT an empty one." >&2
        args=("$default_package")
    fi
fi

# Was this run FILTERED? Asked before anything runs: the emptiness verdict
# below only applies to a filtered run (see args_request_filter).
request_filtered=0
if args_request_filter "${args[@]}"; then
    request_filtered=1
fi

# --- read-only verbs: they inspect artifacts and run nothing -----------------
# Both exist because the artifacts belong to a READER: an operator asking "which
# stream is the newest?" and a review (or the gate) asking "does THIS stream
# carry only the scope it was asked for?". Answering those by re-implementing
# the logic in a second script is how two definitions drift apart (Rule 14).
case "${1:-}" in
    --latest)
        if [ ! -e "$latest_json" ]; then
            echo "test-budget: no completed run stream yet ($latest_json does not exist)" >&2
            exit 2
        fi
        # Print the RESOLVED path: $latest_json is a relative symlink, harmless
        # to `cat`, wrong to quote in a report from another directory.
        latest_target="$(readlink -f "$latest_json" 2>/dev/null || true)"
        if [ -z "$latest_target" ] || [ ! -f "$latest_target" ]; then
            echo "test-budget: $latest_json is not a readable pointer ($(readlink "$latest_json" 2>/dev/null || echo 'not a symlink'))" >&2
            exit 2
        fi
        printf '%s\n' "$latest_target"
        exit 0
        ;;
    --scope-check)
        shift
        stream="${1:-}"
        if [ -z "$stream" ]; then
            echo "test-budget: usage: scripts/test-budget.sh --scope-check <stream.json> [package-pattern...]" >&2
            exit 2
        fi
        shift
        if [ ! -f "$stream" ]; then
            echo "test-budget: no stream at $stream" >&2
            exit 2
        fi
        if [ "$#" -eq 0 ]; then
            set -- "$default_package"
        fi
        gopi_scope_packages "$module_import_path" "$@" >"$scope_pkgs_file"
        gopi_stream_packages "$stream" >"$stream_pkgs_file"
        anomalous="$(gopi_stream_scope_anomalies "$module_import_path" "$scope_pkgs_file" "$stream_pkgs_file")"
        if [ -z "$anomalous" ]; then
            echo "test-budget: $stream is within scope ($(tr '\n' ' ' <"$scope_pkgs_file"))"
            exit 0
        fi
        printf '%s\n' "$anomalous" | sed 's/^/  out of scope: /' >&2
        echo "test-budget: $anomaly_marker — $stream carries the above, which the requested scope does not cover" >&2
        exit "$scope_check_failed_exit"
        ;;
esac

export CGO_ENABLED="${CGO_ENABLED:-0}"
cores="$(nproc 2>/dev/null || echo '?')"
load_start="$(read_load)"

# The header FIRST, then the build: the two slow steps that follow (the tested
# binary, then the suite) are exactly the silences a caller must be able to see
# through. Printing here costs nothing and moves the run's first observable line
# from "after the build" to "as soon as the script has the values it prints".
report_run_header
build_tested_binary

# write_start_record makes THIS run's existence + its stream name DURABLE before
# `go test` is exec'd (see start_record_suffix above for the defect it closes).
#
# It is called on the line BEFORE the `go test` redirect, and it is the ONLY
# writer of that file: no reader parses it as anything but lines, so a reader
# gets the run's identity without knowing anything about this script's state.
#
# A failure here is REPORTED, never swallowed (Rule 15): a run whose start could
# not be recorded is exactly the run whose stream becomes an orphan, so the
# reader is told while the run is still alive rather than after it is lost.
#
# The record is written to a temp name and RENAMED into place, and that is not
# tidiness — it is the difference between a record and a decoy. `>"$start_record"`
# creates (and truncates) the file the instant it opens, so a reader that polls
# for the NAME can find it while the shell is still filling it, and a kill in
# that window leaves a truncated file that LOOKS like a start record and names no
# stream (measured while writing the gate: the poll saw the file after two lines
# and the SIGKILL kept exactly those two). `rename(2)` is atomic, so a reader
# either sees no record at all or the complete one.
write_start_record() {
    local tmp="${start_record}.$$.tmp"
    if {
        echo "$start_record_heading"
        printf 'start_iso=%s\n' "$start_iso"
        printf 'start_epoch=%s\n' "$start_epoch"
        printf 'host=%s\n' "$(hostname -s 2>/dev/null || echo '?')"
        printf 'pid=%s\n' "$$"
        printf 'pkg=%s\n' "${args[*]}"
        printf 'budget=%s\n' "$test_budget"
        printf 'stream=%s\n' "$(basename "$raw_json")"
    } >"$tmp" && mv -f "$tmp" "$start_record"; then
        return 0
    fi
    rm -f "$tmp" 2>/dev/null
    echo "test-budget: could not write this run's start record ($start_record) — a killed run's" >&2
    echo "test-budget:       stream would be left unnameable (Rule 15b: reported, not swallowed)." >&2
    return 0
}

status=0
# THE STREAM EXISTS BEFORE THE RECORD THAT POINTS AT IT. The start record's whole
# contract is "a pointer to real evidence", and until 2026-11-08 that was false for
# one window: the record is renamed into place here, while the stream file was
# created later, by the background redirection below — so a reader (and the gate
# that pins this, TestBudgetKilledRunLeavesAStartRecordNamingItsStream) could see a
# complete record naming a file that did not exist yet, and a kill inside that
# window left exactly the decoy the record exists to prevent. Measured in a
# whole-package run on this host (internal/testutil): the record was on disk at
# 0.96 s and the stream had not been created.
#
# Creating it here is not a second writer: `go test`'s own `>""$raw_json"` below
# still owns every byte, and an empty file is the truthful state of a run that has
# produced no event. A creation failure is REPORTED, never swallowed (Rule 15).
if ! : >"$raw_json"; then
    echo "test-budget: could not create this run's stream ($raw_json) — the start record written" >&2
    echo "test-budget:       next would name a file that is not there (Rule 15b: reported, not swallowed)." >&2
fi
write_start_record
# The suite runs in the BACKGROUND so this shell has a loop to run — the
# heartbeat — while it works. The redirect (both streams into the run's own
# stream file) and the verdict (`wait`, which returns the child's own status) are
# exactly what they were when `go test` ran in the foreground: the heartbeat is
# an OBSERVER that costs the run a bounded tail-read and changes no artifact.
#
# THE SUITE LEADS ITS OWN SESSION — spawned through setsid, never in this shell's
# process group.
#
# WHY (measured 2026-10-24). A test binary may signal a process GROUP
# (`syscall.Kill(-pgid, sig)`), and several in this tree do. A group id is a
# RECYCLABLE pid number, so "the group I meant" and "the group that id names now"
# can be different processes. That is survivable while the only member is the
# suite; it is NOT when the suite inherits its launcher's group, because that
# group is the operator's terminal session. Measured on this host: a run launched
# from a chat shared the chat's group with the operator's recorder and browser,
# and an over-broad group signal killed them.
#
# setsid(1) puts the child in a NEW session and a new process group whose id is
# its own pid, so every group-addressed signal reaches the suite and nothing else.
# Verified on this host:
#   $ setsid bash -c 'ps -o pid=,pgid=,sid= -p $$'
#   555015  555015  555015          (inside: its own session)
#   outer shell: pid 555008 pgid 555008 sid 60277
#
# `wait` still returns the child's own status (setsid execs, it does not fork), so
# the verdict, the heartbeat and every artifact are unchanged. A host without
# setsid(1) keeps the previous behaviour and SAYS SO rather than silently running
# the suite in the caller's group (Rule 15: no silent failure).
if command -v setsid >/dev/null 2>&1; then
    test_budget_launcher=(setsid)
else
    test_budget_launcher=()
    echo "test-budget: setsid(1) not found — the suite runs in THIS shell's process group," >&2
    echo "test-budget:       so a group-addressed signal sent by a test can reach the caller's session (reported, not swallowed)." >&2
fi
"${test_budget_launcher[@]}" go test -count=1 -timeout "$test_budget" -json "${args[@]}" >"$raw_json" 2>&1 &
go_test_pid=$!
heartbeat_while_running "$go_test_pid"
wait "$go_test_pid" || status=$?
elapsed=$(( $(date +%s) - start_epoch ))
load_end="$(read_load)"
seal_run_stream
publish_latest_stream
report_stream_scope_anomaly "${args[@]}"

# The "did anything actually run?" count is taken from the stream NOW, before
# the summary reads it, so the number and the verdict cannot disagree. Beside it
# is the STRUCTURED-EVENT count, which is what separates "the suite ran zero
# tests" from "the suite never ran" (see report_not_measured) — both are read
# here, once, so the summary and the exit code cannot disagree with each other.
executed_tests=0
stream_events=0
stream_size="$(stream_bytes)"
case "$stream_size" in
    '' | *[!0-9]*) stream_size=0 ;;
esac
stream_events="$(count_stream_events)"
executed_tests="$(count_executed_tests)"
case "$executed_tests" in
    '' | *[!0-9]*) executed_tests=0 ;;
esac
case "$stream_events" in
    '' | *[!0-9]*) stream_events=0 ;;
esac

# The SHAPE of an event-less stream, named from the evidence rather than
# asserted: a missing file (nothing could be written), a 0-byte file (go wrote
# no report at all — the measured disk-exhaustion shape), or a file holding text
# without one structured event (go spoke, the framework never started).
not_measured_shape=""
if [ ! -e "$raw_json" ]; then
    not_measured_shape="the stream file does not exist — nothing could be written to $(dirname "$raw_json")"
elif [ "$stream_size" -eq 0 ]; then
    not_measured_shape="$not_measured_empty_shape"
else
    not_measured_shape="the stream holds $stream_size byte(s) of TEXT but ZERO structured events (no line begins with '$stream_event_prefix')"
fi

timeout_tripped=0
if grep -q -- "$timeout_panic_marker" "$raw_json"; then
    timeout_tripped=1
fi

outcome="pass"
exit_code=0
if [ "$timeout_tripped" -eq 1 ]; then
    # A budget trip outranks the exit status: go exits 1 for it, and 1 is what
    # made the wall-clock cap indistinguishable from a real failure.
    outcome="timeout"
    exit_code=3
elif [ "$status" -ne 0 ]; then
    outcome="fail"
    exit_code=1
fi

# The emptiness verdict (see no_tests_marker): a FILTERED run that executed zero
# tests is not a pass, whatever go exited with — Go prints `no tests to run` and
# exits 0 for a filter that matches nothing, which is how a mistyped test name
# came to read as a green run on this very path (bug row
# 01a12cbe-324f-75a6-860e-f32ba6a7ce39). The count was taken from the run's own
# stream above, and defensively defaulted, so an unreadable stream can never turn
# this into a pass.
#
# PRECEDENCE, stated once so the two verdicts cannot both claim the run: the
# NOT-MEASURED verdict is decided FIRST and the filter verdict only applies to a
# run that DID reach the framework. "The filter matched nothing" is a statement
# about a suite that RAN; with no structured event there was no suite, so 6 (not
# measured) is the honest code and 5 would be a claim the evidence does not
# support.
not_measured_run=0
if [ "$stream_events" -eq 0 ]; then
    not_measured_run=1
    outcome="not-measured"
    exit_code="$not_measured_exit"
fi

zero_test_run=0
if [ "$request_filtered" -eq 1 ] && [ "$executed_tests" -eq 0 ] && [ "$not_measured_run" -eq 0 ]; then
    zero_test_run=1
    outcome="no-test-executed"
    exit_code="$no_test_executed_exit"
fi

# The DISK-EXHAUSTION verdict, decided from the run's own BUILD evidence (see
# report_disk_exhaustion). It outranks the plain failure verdict because it is
# the same red with a different MEANING: the branch was never compiled, so a
# reader who took `FAIL <pkg> [build failed]` at face value has been told
# something false about their code. It deliberately does NOT outrank a timeout or
# the event-less shapes above — those already state their own cause, and claiming
# exhaustion on top of them would be a second, weaker answer.
disk_exhausted_run=0
disk_exhaustion_report=""
if [ "$not_measured_run" -eq 0 ] && [ "$timeout_tripped" -eq 0 ] && [ "$exit_code" -ne 0 ]; then
    # Captured rather than printed: the verdict is only REACHED here, and its
    # banner belongs in the failures accounting below, where the `[build failed]`
    # lines it corrects are (Rule 15 — the cause appears where the reader is
    # looking).
    if disk_exhaustion_report="$(report_disk_exhaustion)"; then
        disk_exhausted_run=1
        outcome="disk-exhausted"
        exit_code="$disk_exhaustion_exit"
    fi
fi

echo "--- summary (${elapsed}s wall, load $load_start -> $load_end) ---"
# WHAT this run actually did, in numbers and names: with a `-run` filter the
# reader must be able to tell "the filter matched nothing" from "the filter
# matched something that passed" without opening the stream. Both lines are
# always printed, so their absence is itself a signal.
if [ "$request_filtered" -eq 1 ]; then
    echo "filter:   $(filter_args_of "${args[@]}" | tr '\n' ' ')"
else
    echo "filter:   (none — every test of the selected packages was due to run)"
fi
echo "executed: $executed_tests test(s) ran in this run (counted from the stream)"
# The count alone is not enough when NOTHING structured came back: `executed: 0`
# then reads as "the suite ran and had nothing to do", which is the opposite of
# what happened and is exactly how the measured disk-exhaustion run read. The
# number is therefore never printed on its own in that case — go's own words
# ride the same line (Rule 15: the cause appears where the reader is looking).
if [ "$not_measured_run" -eq 1 ]; then
    echo "          go produced NO structured event at all — see the $not_measured_marker block below."
    echo "          go's own words: $(quote_stream_diagnostics | tr '\n' ' ' | cut -c1-"$executed_fallback_tail_bytes")"
fi
if [ "$stream_events" -gt 0 ]; then
    echo "events:   $stream_events structured event(s) in this run's stream (0 would mean the suite never started)"
fi
if [ "$executed_tests" -gt 0 ]; then
    list_executed_tests | sed 's/^/  ran: /'
fi
summarise_json "$raw_json"
echo
report_attribution
echo
report_unclassified_slow

if [ "$not_measured_run" -eq 1 ]; then
    report_not_measured "$not_measured_shape"
fi

if [ "$zero_test_run" -eq 1 ]; then
    report_no_test_executed "${args[@]}"
fi

if [ "$timeout_tripped" -eq 1 ]; then
    echo
    echo "================== TIMEOUT, NOT AN ASSERTION FAILURE =================="
    echo "The per-package test budget ($test_budget) ran out — a WALL-CLOCK cap,"
    echo "not a failing assertion. The cap applies per package: the package(s)"
    echo "still running when it expired were killed and reported as 'fail'."
    echo
    echo "the budget expired here:"
    fan_out_timeout_marker | sed 's/^/  /'
    echo
    echo "separate assertion failures in the same run (NOT the timeout):"
    list_failure_tests | sed 's/^/  /'
    echo
    fan_out_packages
    echo
    echo "$timeout_cap_note"
    attr_readings="$(attribution_totals)"
    attr_total="${attr_readings%% *}"
    attr_rest="${attr_readings#* }"
    attr_pkgs="${attr_rest%% *}"
    attr_unfinished="${attr_rest##* }"
    printf "$timeout_attribution_reading\n" "${attr_total:-?}" "${attr_unfinished:-?}" "${attr_pkgs:-?}"
    printf "$timeout_attribution_point\n" "$attribution_heading"
    printf "$timeout_recheck_fmt\n" "$(attribution_busiest_package)"
    echo "Never report this line as 'tests failed' — it is 'the budget expired'."
    echo "======================================================================"
elif [ "$zero_test_run" -eq 1 ]; then
    echo
    echo "--- NOT A FAILURE: nothing ran (the filter matched no test) ---"
    echo "the filter matched nothing; see the NO TEST EXECUTED block above."
    echo "full stream: $raw_json"
elif [ "$not_measured_run" -eq 1 ]; then
    # The run never reached a measurement, so there is no failure to report —
    # the failures block would read as "your code failed", which is the
    # misreading this verdict exists to remove. The evidence is go's own text,
    # already quoted by the NOT MEASURED block above.
    echo
    echo "--- NOT A FAILURE EITHER: no measurement was taken ---"
    echo "no structured event was produced, so no test verdict exists; see the"
    echo "$not_measured_marker block above."
    echo "full stream: $raw_json"
elif [ "$exit_code" -ne 0 ]; then
    echo
    report_failures
    # Printed INSIDE the failures accounting, because that is what it corrects: a
    # reader who counted the `--- FAIL:` rows above has counted tests that never
    # ran (Rule 15: the cause appears where the reader is looking).
    report_freshness_refusals
    # And, when BOTH are measured, the run-level reading that ties this run's own
    # refusals to the drift that caused them (report_run_condition). It is printed
    # only inside the failures accounting: a run with no refusal has no condition
    # to report, and the exit status is untouched either way.
    report_run_condition
    # The DISK-EXHAUSTION verdict, when this run's build evidence claimed it —
    # rendered here beside the `[build failed]` lines it corrects. The banner was
    # composed above (the verdict and the exit code are decided there); this is
    # where a reader of the failure list meets it.
    if [ "$disk_exhausted_run" -eq 1 ] && [ -n "$disk_exhaustion_report" ]; then
        printf '%s\n' "$disk_exhaustion_report"
    fi
fi

stamp="$(printf '%s host=%s pkg=%s budget=%s elapsed=%ss load_start=%s load_end=%s cores=%s exit=%d outcome=%s stream=%s' \
    "$start_iso" "$(hostname -s 2>/dev/null || echo '?')" "${args[*]}" "$test_budget" \
    "$elapsed" "$load_start" "$load_end" "$cores" "$exit_code" "$outcome" "$(basename "$raw_json")")"
echo
echo "stamp: $stamp"
echo "$stamp" >>"$stamp_file" || echo "test-budget: could not append to $stamp_file (Rule 15b: reported, not swallowed)" >&2

exit "$exit_code"
