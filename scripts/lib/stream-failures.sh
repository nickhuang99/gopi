# shellcheck shell=bash
# stream-failures.sh — the SHARED predicate behind "which failures did THIS run
# actually have", read from the STRUCTURED events of a `go test -json` stream.
#
# WHY IT IS A LIBRARY AND NOT FIVE LINES INSIDE test-budget.sh
# ------------------------------------------------------------
# The defect this removes (measured 2026-10-12, task 01a12e42's narrow run): the
# failures block harvested its list with
#
#     grep -a -o -- '--- FAIL: [^"\\]*' "$raw_json"
#
# i.e. by the TEXT SHAPE of the marker, over the whole stream. A `--- FAIL:`
# line inside an event's `Output` is DATA, not a verdict — and a PASSING test
# emits exactly that shape whenever it quotes its own fixture. Measured on that
# run's own stream (queue/test-budget/run-1791782756-1190003.json):
#
#   `TestPublishedAnchorGateRefusesAFailingGate`        (PASS) prints
#       --- FAIL: TestAnchor (0.00s)
#   `TestFinishCleanupLocalSyncQuotesTheFailingPackage` (PASS) prints
#       --- FAIL: TestPublishedAnchorResolves (0.00s)
#
# so the block reported 6 failures while the stream carried 5 `Action=="fail"`
# events — and 2 of the 6 named tests do not exist in the repository at all
# (`go test ./internal/tools/ -list` lists neither). The same block printed
# NOTHING for the package-level failures (a package that never compiled carries
# no Test field), which is Rule 15's silent failure inside a report whose whole
# job is to name failures.
#
# "The package failed" is an Event.Action. So the predicate reads the event, and
# the marker text is never a verdict again. It lives here — not inline — for the
# same reason scripts/lib/stream-scope.sh does: the reporter must be exercised by
# SYNTHETIC streams in a gate (internal/testutil/testbudget_failures_gate_test.go)
# instead of waiting for a real run to print a fixture.
#
# ONE EXTRACTOR, SEVERAL VIEWS
# ----------------------------
# Every event that is ANY kind of failure is normalised by gopi_stream_failure_events
# into one TAB-separated line, and each public function is a trivial read over
# that normalisation — so the JSON field extraction (and its tolerance of the two
# encodings Go may emit) exists in exactly ONE place (Rule 14).
#
#   kind              package                test or text
#   test              <import path>          <failing test name>
#   package           <import path>          (empty — a package-level verdict)
#   build             <import path>          (empty)
#   build-output      <import path>          <the compiler's own line>
#
# Usage (source, do not execute):
#     . "$(dirname "$0")/lib/stream-failures.sh"
#     gopi_stream_failure_count "$stream"          # how many TESTS failed
#     gopi_stream_failures "$stream"               # the failing tests, pkg<TAB>test
#     gopi_stream_package_failures "$stream"       # build/panic verdicts, pkg<TAB>kind
#     gopi_stream_failed_packages "$stream"        # every failed package
#     gopi_stream_build_output "$stream"           # the compiler's diagnostics
#     gopi_stream_build_output_for "$stream" PKG   # …for ONE package (attributed)
#     gopi_stream_package_output_for "$stream" PKG # …the package's own output
#     gopi_stream_freshness_refusals "$stream"      # stale-tested-binary refusals, pkg<TAB>text
#     gopi_stream_freshness_refusal_count "$stream" # how many such refusals the stream carries

# --- shape constants (Rule 1a: the literals live here, once) -----------------
# The `go test -json` actions that mean a failure. `fail` is the verdict of a
# test OR of a package; `build-fail` is a package that never compiled (its
# events carry NO Package and NO Test field — the whole reason the old block
# printed nothing for it).
stream_failure_action="fail"
stream_build_fail_action="build-fail"
stream_build_output_action="build-output"
# The action that carries a package's OWN output. A row of it with NO Test field
# is package-level evidence (a TestMain exiting non-zero, a panic caught before
# any test ran) — the only evidence such a verdict has, since it has no test to
# name and no compiler diagnostic either.
stream_package_output_action="output"
# The normalised kinds gopi_stream_failure_events emits.
stream_kind_test="test"
stream_kind_package="package"
stream_kind_build="build"
stream_kind_build_output="build-output"
stream_kind_package_output="package-output"
# The kind emitted for the runner's START event about ONE test (`Action=="run"`).
# It is not a failure — it is the AUTHORITY the classifier needs: "the runner
# reported this test" is the one fact that separates a real per-test verdict from
# a `--- FAIL:` line that some test merely PRINTED into its own log (see
# gopi_stream_unattributed_fail_marks). Without it the reader has no way to tell
# the two apart except by matching names against a list it does not have.
stream_kind_started="started"
# The action that carries the runner's per-test START event.
stream_started_action="run"
# The kind emitted for a `--- FAIL:` marker line found INSIDE an output row, i.e.
# text a test printed, never the runner's verdict about that text. Its rows are
# `kind<TAB>package<TAB>claimed-name<TAB>enclosing-test` (`enclosing-test` empty
# when the row was package-scoped).
stream_kind_fail_mark="fail-mark"
# The kinds that are a FAILURE VERDICT about a package (as opposed to a line of
# the compiler's output).
stream_kind_verdicts="$stream_kind_test|$stream_kind_package|$stream_kind_build"
# The kind gopi_stream_failure_events emits for a TEST's OWN `output` row — the
# row carrying a Test field, i.e. one line of that test's log. It is a distinct
# kind from `package-output` on purpose: a package-level verdict quotes the rows
# with NO Test, a test's failure body quotes ITS OWN rows, and one reader cannot
# serve both without handing one of them the other's evidence.
#
# WHY THIS KIND EXISTS AT ALL (measured 2026-10-13, run-1791849386-333342). The
# failures block printed only the marker line — `--- FAIL: <test> (<elapsed>)` —
# because its input was gopi_stream_failures, the `package<TAB>test` list, which
# by construction carries no body. The `Action=="fail"` event itself has no
# Output field at all (the stream's 17 fail events all read Output=None), so the
# failure's REASON lives ONLY in the output rows that precede it. Measured on
# that run: four `--- FAIL:` rows arrived with no body in the report, a reader
# could not tell a real assertion from a marker quote, and the run's own
# evidence — e.g. "…/gopi (mtime 07:56:32) is OLDER than …/internal/agent/
# clock.go (mtime 07:56:33)" — was in the stream the whole time (Rule 15: a
# failure must NAME itself). The full mechanism, the four sources those rows
# turned out to have, and the measured mid-run source rewrite are recorded in
# devdoc/test-budget-failure-bodies.md — the standing claim this kind implements.
stream_kind_test_output="test-output"
# The text `go test` prints in front of a test's outcome (`--- FAIL: <name>`).
# It is DATA when it appears inside a test's own log — a PASSING test that quotes
# a `go test` result emits exactly this shape — and the only place the literal
# lives (Rule 1/13: a compared value has ONE definition), so the reader that
# classifies a line as "someone printed a failure marker" and the report that
# prints the same shape both read it from here.
stream_test_result_marker="--- FAIL: "
# The kind emitted for a package whose run was ended by a SIGNAL (SIGHUP and
# friends) — the third shape a package-level `Action=="fail"` can have, and the
# one neither of the other two describes.
#
# WHY IT EXISTS (measured 2026-10-23, bug row 01a164f6). A `cmd/agent` budget run
# ended with `failing tests: 0` and a package-level verdict, and the report said
# "a package-level verdict — a panic or a TestMain exit; no test to name". The
# real cause was neither: the test BINARY was killed by SIGHUP, and `go test`
# records that as a package-level `fail` plus ONE package-level output row
#
#	signal: hangup
#
# which is os/exec rendering `"signal: " + status.Signal()` (os/exec_posix.go
# line 123) — the SHELL-layer spelling of a signalled child, not a panic and not
# a TestMain exit. `go test -json` writes it as an `output` event with NO Test
# field (minimal repro, measured: a test that raises SIGHUP on itself emits
# exactly `{"Action":"output","Package":P,"Output":"signal: hangup\n"}` followed
# by `{"Action":"fail","Package":P}` and nothing else), so without this kind it
# lands in `package-output` and reaches the `package` verdict arm by default.
#
# The consequences of NOT having this kind, both measured: an operator reads a
# red their code did not cause (the interrupted tests have no verdict at all),
# and the report NAMES a cause that is false (Rule 15 — the failure is reported,
# its attribution is wrong).
stream_kind_signaled="signaled"
# The row `go test` prints when the test binary was killed by a signal. The
# value is os/exec's `"signal: " + Signal()`, so it is matched on the PREFIX and
# the signal name is taken from the rest: the report names WHICH signal, because
# a hangup (a teardown) and a termination (a budget timeout) send a reader to
# different places.
stream_signal_prefix="signal: "
# Event key names. Package/Test/ImportPath/Action carry IDENTIFIERS, which Go
# does not escape, so the delimited read is exact for them.
stream_key_action="Action"
stream_key_package="Package"
stream_key_test="Test"
stream_key_import_path="ImportPath"
stream_key_output="Output"
# The substring that makes a STALE-TESTED-BINARY refusal recognisable inside a
# failure's own output — the one place it is spelled on the shell side.
#
# IT MUST MATCH THE GUARD'S OWN SENTENCE, and the guard is a GO FILE
# It lives in cmd/agent/task_session_cwd_freshness_evidence_test.go, the file that
# also holds the refusal's VERDICT vocabulary, because on current master the mtime
# comparison belongs to the artifact-identity guard
# (cmd/agent/gopibin_identity_guard_test.go, decideBinFreshnessByMtime) and that is
# the file whose evidence arm composes the sentence this marker matches.
#
# A shell literal beside a Go literal drifts silently, so the two spellings are
# pinned together by a gate rather than by this comment:
# cmd/agent/task_session_cwd_freshness_evidence_test.go reads BOTH files and fails
# if either spelling moves alone (Rule 13/14 — one value, two readers, one checked
# definition).
#
# The refusal it matches, measured 2026-10-13 on run-1791849386-333342:
#   <tested-bin>/gopi (mtime 07:56:32) is OLDER than …/internal/agent/clock.go
#   (mtime 07:58:50) — this test drives the BUILT binary, so it would verify
#   code that is not in that binary. …
# Twelve of that run's fifteen failing tests carried it, and the run's summary
# said only `--- FAIL:` — a reader could not tell a stale-binary refusal (zero
# tests executed) from a code verdict.
stream_freshness_marker="drives the BUILT binary"
# The guard's refusal SIGNATURE — the marker inside the ONE sentence every refusal
# arm prints (cmd/agent/gopibin_identity_guard_test.go's freshnessRefusalSignature,
# which COMPOSES it from the same marker constant; the two spellings are compared
# by a gate rather than by this comment — Rule 13/14, one value two readers).
#
# IT IS THE CLASSIFIER'S KEY (see gopi_stream_freshness_rows), not the bare marker,
# because the marker alone is matched by any paraphrase. WHY A KEY IS NEEDED AT
# ALL (the defect measured on master cbdaca0, task 01a13c43): the CONTENT arm — the
# one a mid-run content change actually fires, rendering "<bin> does NOT contain
# the tree's production sources: N file(s) differ from the revision it was built
# from (<rev>).") — carried NO marker, so the runner recognised none of that run's
# refusals and the report named none of them: N unattributed failures.
stream_freshness_signature="this test drives the BUILT binary, so it would verify code that is not in that binary."
# The refusal's OTHER composed line: the guard names WHICH criterion decided every
# refusal with the same prefix, on every arm. The runner reads it to say the
# criterion, rather than re-deriving one (Rule 14).
stream_freshness_criterion_prefix="deciding criterion: "
# The column separator of every line this library emits.
stream_col_sep="	"
# The escape-order placeholder used while unescaping a JSON string (see
# gopi_stream_failure_events): an escaped backslash is parked here so the
# later unescapes cannot re-read it.
stream_escape_marker="\001"

# gopi_stream_failure_events STREAM — every failure-shaped event of the stream,
# normalised to one TAB-separated `kind<TAB>package<TAB>test-or-text` line (see
# the header). The single place where the JSON is read (Rule 14).
#
# `go test -json` writes compact JSON with a fixed key order and no space after
# the colon; the read tolerates a space anyway, because a re-encoding by another
# tool must not silently empty the report. The scan starts at the FIRST
# occurrence of the key, which is safe for the keys read here: Package and Test
# precede Output in the encoding, so a key name occurring inside some later
# value's text cannot win.
#
# `grep -a` on the way in: a stream can carry a NUL byte (a test feeding binary
# data to a tool), and plain grep would then call the whole file binary and print
# NOTHING — the silent-empty answer this file exists to avoid.
gopi_stream_failure_events() {
    local stream="${1:?gopi_stream_failure_events needs a stream path}"
    grep -a '^{' "$stream" 2>/dev/null |
        awk \
            -v kaction="$stream_key_action" -v kpkg="$stream_key_package" \
            -v ktest="$stream_key_test" -v kimp="$stream_key_import_path" \
            -v kout="$stream_key_output" -v failed="$stream_failure_action" \
            -v bfail="$stream_build_fail_action" -v bout="$stream_build_output_action" \
            -v pout="$stream_package_output_action" \
            -v kind_test="$stream_kind_test" -v kind_pkg="$stream_kind_package" \
            -v kind_build="$stream_kind_build" -v kind_bout="$stream_kind_build_output" \
            -v kind_pout="$stream_kind_package_output" \
            -v kind_tout="$stream_kind_test_output" \
            -v kind_started="$stream_kind_started" -v started="$stream_started_action" \
            -v kind_fmark="$stream_kind_fail_mark" -v markf="$stream_test_result_marker" \
            -v kind_sig="$stream_kind_signaled" -v sigpfx="$stream_signal_prefix" \
            -v sep="$stream_col_sep" -v mark="$stream_escape_marker" '
            function fstr(s, k,   pat, start, i, c, out) {
                pat = "\"" k "\":"
                start = index(s, pat)
                if (start == 0) return ""
                i = start + length(pat)
                if (substr(s, i, 1) == " ") i++
                if (substr(s, i, 1) != "\"") return ""
                i++
                out = ""
                # Scan to the value TERMINATING quote, stepping over escapes:
                # this is the only read that is exact whatever follows the value
                # (`,"Elapsed":0}` after Output, `,"Action":"fail"` before it),
                # which a shape-anchored regex cannot be.
                while (i <= length(s)) {
                    c = substr(s, i, 1)
                    if (c == "\\") { out = out substr(s, i, 2); i += 2; continue }
                    if (c == "\"") break
                    out = out c
                    i++
                }
                return out
            }
            # Undo a JSON string body EXACTLY: the escaped backslash is parked
            # first, so the later rules cannot re-read it.
            function unesc(s) {
                gsub(/\\\\/, mark, s)
                gsub(/\\n/, " ", s)
                gsub(/\\t/, " ", s)
                gsub(/\\r/, "", s)
                gsub(/\\"/, "\"", s)
                gsub(mark, "\\", s)
                gsub(/[ \t]+$/, "", s)
                return s
            }
            function pkg_of(s,   p) {
                p = fstr(s, kpkg)
                if (p != "") return p
                p = fstr(s, kimp)
                sub(/ \[[^]]*\]$/, "", p)   # `M/internal/tools [M/internal/tools.test]`
                return p
            }
            {
                action = fstr($0, kaction)
                if (action == failed) {
                    test = fstr($0, ktest)
                    if (test != "") print kind_test sep pkg_of($0) sep test
                    else print kind_pkg sep pkg_of($0) sep ""
                    next
                }
                if (action == started) {
                    # The runner own per-test START event: the AUTHORITY the
                    test = fstr($0, ktest)
                    if (test != "") print kind_started sep pkg_of($0) sep test
                    next
                }
                if (action == bfail) { print kind_build sep pkg_of($0) sep ""; next }
                if (action == bout) { print kind_bout sep pkg_of($0) sep unesc(fstr($0, kout)); next }
                if (action == pout) {
                    # An `output` row belongs to a TEST when it carries a Test
                    # field, and to the PACKAGE when it does not. The two are
                    # separate kinds so each reader gets its own rows, and neither
                    # is handed the evidence of the other (a package-level verdict
                    # quoting a test log asserts a line belonging to somebody
                    # else).
                    text = unesc(fstr($0, kout))
                    owner = fstr($0, ktest)
                    if (owner != "") {
                        print kind_tout sep pkg_of($0) sep owner sep text
                    } else {
                        print kind_pout sep pkg_of($0) sep text
                    }
                    # A `--- FAIL:` marker INSIDE a log line is text a test
                    # PRINTED. It is normalised as its own kind — with the name it
                    # CLAIMS and the test whose log carried it — so the reporter
                    # can classify it as unattributed output instead of silently
                    # dropping it (Rule 15: an unmentioned row is a lost row).
                    at = index(text, markf)
                    if (at > 0) print kind_fmark sep pkg_of($0) sep claimed_of(substr(text, at + length(markf))) sep owner
                    next
                }
            }
            # claimed_of reads the test NAME a printed marker claims: everything
            # after `--- FAIL: ` up to the first space (the runner prints
            # `<name> (<elapsed>)`, and a fixture quoting it keeps that shape).
            function claimed_of(rest,   n) {
                n = rest
                sub(/[[:space:]].*$/, "", n)
                sub(/[[:space:]]+$/, "", n)
                return n
            }
        '
}

# gopi_stream_signals STREAM — the packages whose run `go test` recorded as ended
# by a SIGNAL, as `package<TAB>signal-name`, sorted and de-duplicated.
#
# BOTH ROW SHAPES ARE READ, because both are measured. os/exec's
# `"signal: " + Signal()` line reaches the stream as an `output` event, and
# whether that event carries a Test field depends on WHERE the process died:
#
#	package-scoped (minimal repro, measured 2026-10-23):
#	  {"Action":"output","Package":P,"Output":"signal: hangup\n"}
#	  {"Action":"fail","Package":P,"Elapsed":0.052}
#
#	test-scoped (the real budget run, run-1792694665-3875354, 02:51:48):
#	  {"Action":"output","Package":P,"Test":"TestWorkerRoundClaimsWhenNoRequestStands",
#	   "Output":"signal: hangup\n"}
#
# A reader that only looked at the package-scoped rows would classify the real
# defect as "no cause at all"; one that only looked at test-scoped rows would
# miss the minimal repro. So this function reads the text of EVERY `output` row
# regardless of which kind the normalisation gave it, and attributes it by the
# row's PACKAGE — the signal row's Test field is go's "whichever test was live"
# and is not a claim about which test the signal is about (the process died, so
# no test carries that verdict).
gopi_stream_signals() {
    local stream="${1:?gopi_stream_signals needs a stream path}"
    gopi_stream_failure_events "$stream" |
        awk -F"$stream_col_sep" \
            -v kind_pout="$stream_kind_package_output" -v kind_tout="$stream_kind_test_output" \
            -v sigpfx="$stream_signal_prefix" -v sep="$stream_col_sep" '
            ($1 == kind_pout || $1 == kind_tout) && $2 != "" {
                text = ($1 == kind_pout) ? $3 : $4
                if (index(text, sigpfx) != 1) next
                name = substr(text, length(sigpfx) + 1)
                sub(/[[:space:]].*$/, "", name)
                if (name != "") print $2 sep name
            }
        ' | LC_ALL=C sort -u
}

# gopi_stream_signal_rows STREAM PACKAGE — the verbatim TEXT of the rows that made
# this package signalled, one line each.
#
# WHY THE CALLER CANNOT REUSE gopi_stream_package_output_for: that reader returns
# package-SCOPED output only, and the real run's signal row was TEST-scoped (see
# gopi_stream_signals above). Quoting it would have left the report SAYING
# "killed by hangup" with no `signal: hangup` line under it — the assertion
# without its evidence (measured on the replay of the real stream, 2026-10-23).
#
# The TEXT is returned, never a paraphrase, so the reader sees os/exec's own
# spelling — the one fact that separates a hangup from a termination.
gopi_stream_signal_rows() {
    local stream="${1:?gopi_stream_signal_rows needs a stream path}"
    local pkg="${2:?gopi_stream_signal_rows needs a package}"
    gopi_stream_failure_events "$stream" |
        awk -F"$stream_col_sep" \
            -v kind_pout="$stream_kind_package_output" -v kind_tout="$stream_kind_test_output" \
            -v sigpfx="$stream_signal_prefix" -v pkg="$pkg" '
            ($1 == kind_pout || $1 == kind_tout) && $2 == pkg {
                text = ($1 == kind_pout) ? $3 : $4
                if (index(text, sigpfx) == 1) print text
            }
        '
}

# gopi_stream_failures STREAM — the failing TESTS, as `package<TAB>test`, sorted
# and de-duplicated. This is the list the text harvest could not produce: a
# failing test is the one whose own event says Action=="fail" AND carries a Test
# field. A build-failed package has neither, so it is not a nameless entry here;
# gopi_stream_package_failures names it.
gopi_stream_failures() {
    local stream="${1:?gopi_stream_failures needs a stream path}"
    gopi_stream_failure_events "$stream" |
        awk -F"$stream_col_sep" -v kind="$stream_kind_test" -v sep="$stream_col_sep" \
            '$1 == kind { print $2 sep $3 }' |
        sort -u
}

# gopi_stream_failed_packages STREAM — every package the stream reports as
# failed, one import path per line (sorted, de-duplicated), whatever the KIND of
# failure: an assertion, a package-level verdict, a build error. It is the
# package-side set a per-test list cannot cover, and the thing a reader compares
# against `Action=="fail"` when the two disagree.
gopi_stream_failed_packages() {
    local stream="${1:?gopi_stream_failed_packages needs a stream path}"
    gopi_stream_failure_events "$stream" |
        awk -F"$stream_col_sep" -v verdicts="$stream_kind_verdicts" '$1 ~ "^("verdicts")$" && $2 != "" { print $2 }' |
        sort -u
}

# gopi_stream_package_failures STREAM — the failures that have NO test to name:
# a build error, a panic that took the package down, a TestMain that exited
# non-zero. One line per package, `package<TAB>kind`, kind being `build` (never
# compiled) or `package` (a package-level verdict). BOTH kinds printed NOTHING
# in the old block (Rule 15).
gopi_stream_package_failures() {
    local stream="${1:?gopi_stream_package_failures needs a stream path}"
    local signalled
    signalled="$(gopi_stream_signals "$stream" | cut -f1 | LC_ALL=C sort -u)"
    gopi_stream_failure_events "$stream" |
        awk -F"$stream_col_sep" \
            -v kind_build="$stream_kind_build" -v kind_pkg="$stream_kind_package" \
            -v kind_test="$stream_kind_test" -v kind_sig="$stream_kind_signaled" -v signalled="$signalled" '
            FNR == NR {
                # The signalled set arrives as the first input (see the caller);
                # a package whose run died by a signal is classified by that fact
                # ALONE, never by whether some test also emitted a verdict.
                if ($0 != "") siged[$0] = 1
                next
            }
            $1 == kind_build && $2 != "" { build[$2] = 1; next }
            $1 == kind_test && $2 != "" { hastest[$2] = 1; next }
            $1 == kind_pkg && $2 != "" { nameless[$2] = 1; next }
            END {
                # `build` first, and a `package` row only when the package has
                # NOTHING else naming it: every package that failed also emits a
                # Test-less `Action=="fail"` event (measured), so the nameless
                # event alone does not mean "there is no test to name" — a panic
                # or a TestMain exit is the case where there is genuinely none.
                for (p in build) print p "\t" "build"
                # A SIGNALLED package is its own kind, and it is decided FIRST:
                # the "no test to name" rule below would otherwise swallow it
                # exactly when the stream carries a test-scoped signal row (the
                # real shape — `hastest` is set by the very events that prove the
                # interruption), which is the defect this kind removes.
                for (p in siged) print p "\t" kind_sig
                for (p in nameless) if (!(p in build) && !(p in siged) && !(p in hastest)) print p "\t" "package"
            }
        ' <(printf '%s\n' "$signalled") <(gopi_stream_failure_events "$stream") | sort
}

# gopi_stream_failure_records STREAM — every failure record as
# `kind<TAB>package<TAB>detail`, sorted. Unlike the verdict-only readers above,
# this one carries the package's OWN COMPILER DIAGNOSTICS in the detail column of
# a `build` row, joined from that package's `build-output` rows.
#
# WHY IT EXISTS (the whole point of the third column): a judgement that has to
# read the compiler's own text — "was this build refused by the FILESYSTEM or by
# the CODE?" — cannot be made from a verdict alone. The build's diagnostics are
# the only rows carrying that text, and they are attributed to a package by
# ImportPath. Reading them SOME OTHER WAY (grepping the raw stream) is the exact
# defect this library was created to remove: the raw text also contains a TEST's
# own output, so a test that PRINTS a disk-exhaustion sentence while it passes
# would be read as the package's build evidence (measured shape: this repository's
# own gates print such fixtures, so the false positive is live, not theoretical).
#
# A `build` row with NO diagnostics is printed with an empty detail — the
# distinction between "diagnostics exist and don't match" and "there are none" is
# what the caller's absence-of-a-compiler-diagnostic arm reads.
gopi_stream_failure_records() {
    local stream="${1:?gopi_stream_failure_records needs a stream path}"
    gopi_stream_failure_events "$stream" |
        awk -F"$stream_col_sep" \
            -v kind_test="$stream_kind_test" -v kind_build="$stream_kind_build" \
            -v kind_pkg="$stream_kind_package" -v kind_bout="$stream_kind_build_output" \
            -v kind_tout="$stream_kind_test_output" -v sep="$stream_col_sep" '
            function strip(s) { sub(/ \[[^]]*\]$/, "", s); return s }
            $1 == kind_bout { b = strip($2); out[b] = out[b] " " $3; next }
            $1 == kind_test { test[$2] = 1; next }
            $1 == kind_build { build[strip($2)] = 1; next }
            $1 == kind_pkg && $2 != "" { pkg[$2] = 1; next }
            $1 == kind_tout { next }   # a TEST'"'"'s own log is never build evidence
            END {
                for (p in build) print kind_build sep p sep substr(out[p], 2)
                for (p in pkg) if (!(p in build) && !(p in test)) print kind_pkg sep p sep ""
            }
        ' | sort
}

# gopi_stream_build_output STREAM — the compiler's own diagnostics, one line
# each, from the `build-output` events (which carry no Package field at all).
gopi_stream_build_output() {
    local stream="${1:?gopi_stream_build_output needs a stream path}"
    gopi_stream_failure_events "$stream" |
        awk -F"$stream_col_sep" -v kind="$stream_kind_build_output" '$1 == kind { sub(/^[^\t]*\t[^\t]*\t/, ""); print }'
}

# gopi_stream_rows_for_kind STREAM KIND PACKAGE — the third column of the rows of
# ONE kind belonging to ONE package. The attribution is by the package's FULL
# import path, never by a substring of it: two packages in one run can share a
# basename (`p1/tools`, `p2/tools`), and a substring filter then gives each one
# the other's evidence — a wrong answer that reads exactly like a right one
# (measured: devdoc/test-budget-package-attribution.md).
gopi_stream_rows_for_kind() {
    local stream="${1:?gopi_stream_rows_for_kind needs a stream path}"
    local kind="${2:?gopi_stream_rows_for_kind needs a kind}"
    local pkg="${3-}"
    [ -n "$pkg" ] || return 0
    gopi_stream_failure_events "$stream" |
        awk -F"$stream_col_sep" -v kind="$kind" -v pkg="$pkg" \
            '$1 == kind && $2 == pkg { sub(/^[^\t]*\t[^\t]*\t/, ""); print }'
}

# gopi_stream_build_output_for STREAM PACKAGE — the compiler diagnostics of ONE
# build-failed package. This is what a per-package report quotes: the whole
# stream's diagnostics are every package's mixed together, which is the shape that
# cannot say whose line is whose.
gopi_stream_build_output_for() {
    gopi_stream_rows_for_kind "${1:?gopi_stream_build_output_for needs a stream path}" \
        "$stream_kind_build_output" "${2-}"
}

# gopi_stream_package_output_for STREAM PACKAGE — the package-level output of ONE
# package: the rows it emitted OUTSIDE any test (a TestMain that exited non-zero
# says `exit status 3` there and nowhere else). A `package`-kind verdict has no
# test to name and no compiler diagnostic, so its own output is the only evidence
# it has — quoting the stream's build output instead is somebody else's line.
gopi_stream_package_output_for() {
    gopi_stream_rows_for_kind "${1:?gopi_stream_package_output_for needs a stream path}" \
        "$stream_kind_package_output" "${2-}"
}

# gopi_stream_failure_bodies STREAM PACKAGE TEST — the failing TEST's own log,
# one line per row, in the order `go test` printed it (the `--- FAIL:` marker is
# the last line). The row shape is `package<TAB>test<TAB>test<TAB>line`, i.e. the
# test name IS in the row twice because the normalisation carries it in the
# test-name column (one shape for every kind, Rule 14) — this reader drops the
# duplicated name and prints the LINE, which is what a reader needs.
#
# WHY IT IS ASKED FOR ONE TEST AT A TIME. The attribution is by (package, test)
# exactly: `p1/tools` and `p2/tools` can each hold a `TestRealFailure`, and a
# substring filter would hand one of them the other's assertion text — a wrong
# answer that reads exactly like a right one (the measured defect
# devdoc/test-budget-package-attribution.md records for build output). Handing a
# reader ANOTHER test's text is the same defect with a worse consequence: the
# sentence would be evidence, and it would be false.
#
# WHY IT STOPS AT THE NEXT VERDICT. The body ENDS where the test's own log ends.
# Counting rows backwards (`tail -n N`) needs a number for a log whose length is
# unbounded (measured: 220 rows for ONE failing test in run-1791849386-333342),
# so a fixed lookback silently truncates exactly the failure a reader needed.
# The scan therefore runs forward and stops at the first row that is a VERDICT
# about any test in the same package — the rows are stream-ordered, so that row
# is the next test's `Action=="fail"`/`pass`, i.e. the end of this one's log.
#
# AN EMPTY BODY IS A REPORTED FACT, not an omission: the caller prints a distinct
# sentence for it, because "the stream carries no output for this test" and "this
# test failed with an empty body" are different claims and must not share a
# rendering (Rule 15).
gopi_stream_failure_bodies() {
    local stream="${1:?gopi_stream_failure_bodies needs a stream path}"
    local pkg="${2:?gopi_stream_failure_bodies needs a package import path}"
    local test="${3:?gopi_stream_failure_bodies needs a test name}"
    gopi_stream_failure_events "$stream" |
        awk -F"$stream_col_sep" \
            -v kind_tout="$stream_kind_test_output" \
            -v kind_test="$stream_kind_test" \
            -v pkg="$pkg" -v test="$test" '
            {
                if ($1 == kind_test && $2 == pkg) { if (seen) exit; next }
                if ($1 != kind_tout || $2 != pkg || $3 != test) next
                seen = 1
                print $4
            }
        '
}

# gopi_stream_failure_count STREAM — how many TESTS failed, one integer. A count
# taken from the same events as the list cannot disagree with it, which is the
# property the text harvest never had.
gopi_stream_failure_count() {
    local stream="${1:?gopi_stream_failure_count needs a stream path}"
    local n
    n="$(gopi_stream_failures "$stream" | grep -c . 2>/dev/null || true)"
    case "$n" in
        '' | *[!0-9]*) n=0 ;;
    esac
    printf '%s\n' "$n"
}

# gopi_stream_unattributed_fail_marks STREAM — the `--- FAIL:` marker lines the
# stream carries that the RUNNER never attributed to a test, one line each as
# `package<TAB>claimed-name<TAB>enclosing-test`.
#
# WHY THIS FUNCTION EXISTS (the defect it removes, measured twice: task 01a12e58
# and again on every run whose log quotes a `go test` result). A `go test` stream
# carries TWO kinds of `--- FAIL:` text and only one of them is a verdict:
#
#   the RUNNER's own line — the last row of a test that really failed. It is
#   accompanied by `Action=="run"` for that name and `Action=="fail"` with that
#   Test field, so the failure list never needs the text at all.
#
#   a TEST'S OWN TEXT — a passing test asserting on a failure it captured prints
#   the marker into its log (`TaskBranchCheckRunQuotesTheFailingPackage` quotes
#   `--- FAIL: TestPublishedAnchorResolves`, a test that exists only as a fixture
#   string). The runner started no test of that name, so it has no verdict to
#   attribute and none may be invented.
#
# The two are told apart by the RUNNER'S AUTHORITY, never by the shape of the
# line: a marker is attributed iff some `Action=="run"` event in this stream
# started a test whose name it claims. Names are compared whole, so a subtest path
# (`TestX/case`) and its parent are distinct claims.
#
# WHY IT IS REPORTED AT ALL rather than dropped, which is what the event-based
# reader did before (the measured shape of THIS epic): a row that reaches the
# stream and reaches no category is a row the reader cannot see is missing, and a
# `grep -c FAIL` over the report then counts nothing while the stream carries
# twelve markers (Rule 15 — an unmentioned row is a lost row). So the unattributed
# marks are printed as their own block, with the name they claim and the test
# whose log carried them: a reader who wondered about that name learns where it
# came from instead of guessing it was a failure of this run.
#
# A row is NOT unattributed merely because its enclosing test passed: the runner
# really can print a marker for a name that then reports `pass` (a `--- FAIL:`
# followed by a retry that succeeded), and that is still the runner speaking.
gopi_stream_unattributed_fail_marks() {
    local stream="${1:?gopi_stream_unattributed_fail_marks needs a stream path}"
    awk -F"$stream_col_sep" -v kind_started="$stream_kind_started" \
        -v kind_fmark="$stream_kind_fail_mark" -v sep="$stream_col_sep" '
        $1 == kind_started { started[$3] = 1; next }
        $1 == kind_fmark && $3 != "" && !($3 in started) {
            print $2 sep $3 sep $4
        }
    ' <(gopi_stream_failure_events "$stream") | LC_ALL=C sort -u
}

# gopi_stream_unattributed_fail_mark_count STREAM — how many unattributed marker
# rows the stream carries, one integer. It comes from the same rows as the list,
# so the two cannot disagree (the property the old text harvest never had).
gopi_stream_unattributed_fail_mark_count() {
    local stream="${1:?gopi_stream_unattributed_fail_mark_count needs a stream path}"
    local n
    n="$(gopi_stream_unattributed_fail_marks "$stream" | grep -c . 2>/dev/null || true)"
    case "$n" in
        '' | *[!0-9]*) n=0 ;;
    esac
    printf '%s\n' "$n"
}

# gopi_stream_freshness_rows STREAM — the stale-tested-binary refusal ROWS, as
# `package<TAB>test<TAB>text`, and the ONE reader every freshness view below is
# derived from (Rule 14).
#
# TWO CONDITIONS, and both are load-bearing:
#
#  1. the row's TEXT carries the guard's refusal SIGNATURE
#     (stream_freshness_signature — the marker inside the ONE sentence every
#     refusal arm prints). The signature is matched against the text, never
#     against a test's name, and it is the SIGNATURE rather than the bare marker
#     because the marker alone is matched by a paraphrase while the signature is
#     the shape the guard renders on EVERY arm — including the CONTENT arm, which
#     on master cbdaca0 carried no marker at all and so was invisible to this
#     classifier (the defect this reader's key was changed for).
#  2. the row belongs to a test that FAILED (gopi_stream_failures). This is the
#     same distinction the failure list itself was rebuilt on: a PASSING test
#     that prints a refusal it captured — a gate quoting the guard's own message,
#     which this repository really does (internal/testutil asserts on it) — would
#     otherwise be reported as a stale-binary refusal of THIS run, and the report
#     would blame the runner for a string a test was carrying around.
#
# Each row is `package<TAB>test<TAB>artifact<TAB>text`: the row names the TEST
# that refused and the ARTIFACT it refused (the tested binary the sentence is
# about), because "a refusal happened" is not something a reader can act on —
# where the artifact came from and which test would not verify is.
#
# A stream whose rows were normalised with the tab separator collapsed still
# matches: the text is reassembled from field 4 on (the reader normalises tab to
# space while unescaping, so the text never contains the separator itself).
gopi_stream_freshness_rows() {
    local stream="${1:?gopi_stream_freshness_rows needs a stream path}"
    awk -F"$stream_col_sep" -v kind="$stream_kind_test_output" -v sig="$stream_freshness_signature" -v sep="$stream_col_sep" '
        FNR == NR { failing[$1 sep $2] = 1; next }
        $1 == kind {
            t = $4
            for (i = 5; i <= NF; i++) t = t sep $i
            if (index(t, sig) > 0 && (($2 sep $3) in failing)) print $2 sep $3 sep artifact_of(t) sep t
        }
        # artifact_of reads the artifact a refusal sentence is about: its FIRST
        # word, after the test log'"'"'s own `file.go:NN: ` prefix is dropped. Every
        # refusal arm composes "%s …" with the tested binary in that position
        # (gopibin_identity_guard_test.go), so this is the same value the guard
        # printed rather than a second reading of the stream.
        function artifact_of(t,   s) {
            s = t
            sub(/^[[:space:]]+/, "", s)
            sub(/^[^[:space:]]+\.go:[0-9]+:[[:space:]]*/, "", s)
            sub(/[[:space:]].*$/, "", s)
            return s
        }
    ' <(gopi_stream_failures "$stream") <(gopi_stream_failure_events "$stream")
}

# gopi_stream_freshness_refusals STREAM — the distinct refusal SENTENCES, as
# `package<TAB>artifact<TAB>text`, de-duplicated. Two tests refused by the same
# build carry the same sentence only when they name the same artifact and the same
# files; the measured run's twelve refusals collapsed to the two distinct
# sentences that mattered. The artifact is kept IN the row (not dropped) because a
# reader who is told "a refusal happened" still has to know WHICH artifact the
# tests would not verify.
gopi_stream_freshness_refusals() {
    local stream="${1:?gopi_stream_freshness_refusals needs a stream path}"
    local pkg test artifact text
    gopi_stream_freshness_rows "$stream" |
        while IFS="$stream_col_sep" read -r pkg test artifact text; do
            printf '%s%s%s%s%s\n' "$pkg" "$stream_col_sep" "$artifact" "$stream_col_sep" "$text"
        done | LC_ALL=C sort -u
}

# gopi_stream_freshness_tests STREAM — the (package, test, artifact) triples that
# refused, as `package<TAB>test<TAB>artifact`. It is the triple list the verdict
# reader needs: the signature sits on ONE row of a test's output and the guard's
# VERDICT line on another, so the refusal has to be read as a BODY, not as a
# single row — and the artifact is what makes the row name the thing refused.
gopi_stream_freshness_tests() {
    local stream="${1:?gopi_stream_freshness_tests needs a stream path}"
    local pkg test artifact text
    gopi_stream_freshness_rows "$stream" |
        while IFS="$stream_col_sep" read -r pkg test artifact text; do
            printf '%s%s%s%s%s\n' "$pkg" "$stream_col_sep" "$test" "$stream_col_sep" "$artifact"
        done | LC_ALL=C sort -u
}

# gopi_stream_freshness_artifacts STREAM — the distinct artifacts the run's
# refusals name, one per line. The report prints them so a reader sees WHICH
# binary the run refused to verify (a stale tested binary being the runner's own
# one-build design meeting a tree that moved under it).
gopi_stream_freshness_artifacts() {
    local stream="${1:?gopi_stream_freshness_artifacts needs a stream path}"
    gopi_stream_freshness_refusals "$stream" | cut -f2 | LC_ALL=C sort -u
}

# gopi_stream_freshness_refusal_count STREAM — how many DISTINCT refusals the
# stream carries. 0 means this run's failures are something else, which the
# summary must be able to state without guessing.
gopi_stream_freshness_refusal_count() {
    local stream="${1:?gopi_stream_freshness_refusal_count needs a stream path}"
    local n
    n="$(gopi_stream_freshness_refusals "$stream" | grep -c . || true)"
    printf '%s\n' "${n:-0}"
}

# gopi_stream_freshness_criteria STREAM — the criterion the guard's ONE refusal
# composer named, de-duplicated, read from its own prefix line. The runner RELAYS
# the guard's decision (Rule 14): "which criterion decided" is the guard's value,
# and a second classifier in the summary would be a second definition of it.
gopi_stream_freshness_criteria() {
    local stream="${1:?gopi_stream_freshness_criteria needs a stream path}"
    local pkg test artifact
    while IFS="$stream_col_sep" read -r pkg test artifact; do
        [ -n "$test" ] || continue
        gopi_stream_failure_bodies "$stream" "$pkg" "$test"
    done < <(gopi_stream_freshness_tests "$stream") |
        sed -n "s/.*$stream_freshness_criterion_prefix\([a-z ]*\).*/\1/p" |
        LC_ALL=C sort -u
}

# gopi_stream_freshness_verdicts STREAM — the guard's own VERDICT lines from every
# refusal in the stream, de-duplicated: the sentences that say WHICH of the two
# causes this run hit ("the tree MOVED while the suite ran" vs "the binary
# PREDATES this source"). The runner RELAYS the guard's decision instead of
# re-deriving it (Rule 14): a second classifier in the summary would be a second
# definition of a value the guard owns, and the two would drift.
gopi_stream_freshness_verdicts() {
    local stream="${1:?gopi_stream_freshness_verdicts needs a stream path}"
    local pkg test artifact
    while IFS="$stream_col_sep" read -r pkg test artifact; do
        [ -n "$test" ] || continue
        gopi_stream_failure_bodies "$stream" "$pkg" "$test"
    done < <(gopi_stream_freshness_tests "$stream") |
        sed -n 's/.*VERDICT: //p' |
        sed 's/  Rebuild it.*$//' |
        LC_ALL=C sort -u
}
