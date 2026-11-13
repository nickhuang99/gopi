# shellcheck shell=bash
# budget-overlap.sh — the SHARED predicate behind "another test-budget run on
# THIS host is alive and asked for a package I also asked for".
#
# WHY IT IS A LIBRARY AND NOT FIVE LINES INSIDE test-budget.sh
# ------------------------------------------------------------
# The defect (measured 2026-10-16, bug row 01a142e4): two `scripts/test-budget.sh`
# runs ran concurrently on this host — pid 2885273 asked for four packages, a
# neighbour session for two, and the two sets intersected in two packages. Both
# wrote their start records, both started a `go test -json`, and NEITHER said a
# word about it, so every red inside the overlap window was quotable as one run's
# own verdict. This repository's own rule text already names the shape an observed
# failure ("Two go test sets in one run") — the failure was known, and the run
# that suffered from it was not told.
#
# A reporter that waits for a real concurrency accident to be exercised is not a
# guard — the same reasoning scripts/lib/stream-scope.sh records for the mingled
# stream. So the predicate lives here and is called by the gate with SYNTHETIC
# records: the overlap, the no-overlap and the dead-pid cases are ASSERTED
# instead of hoped for.
#
# THE INPUT IS THE EXISTING START RECORD, and nothing new is created (Rules 13/14,
# Rule 35): `run-<epoch>-<pid>-start.txt` is already written by every run before
# `go test` starts, already documented, and already carries exactly the facts this
# check needs — `pid=`, `start_iso=` and `pkg=`. No lock, no daemon, no registry,
# no new file format, no new artifact.
#
# LIVENESS IS THE PROCESS TABLE, NOT THE PPID (Rule 24): the question is "is that
# run still on this machine", and `/proc/<pid>` is the only instrument for it.
# Pids are RECYCLED, so the number alone attributes nothing: the record CLAIMS a
# pid, and a readable command line that does not mention the entry point is a
# stranger wearing that number, not a peer.
#
# AN EMPTY COMMAND LINE IS NOT AN UNREADABLE ONE — the two read outcomes are
# different facts and Rule 15 covers only the first:
#   read < 0   the read FAILED (the /proc entry is gone). That is an honest
#              unknown, and a neighbour neither confirmed nor excluded is not
#              dropped silently.
#   read == 0  the read SUCCEEDED and the file holds zero bytes, i.e. this
#              process has no argv. That is a MEASUREMENT, not a failure to
#              measure: a process with no argv can never BE the entry point, so
#              the answer is known and it is "not a peer". `index("", needle)`
#              already returns the right answer for exactly this reason.
# Measured why the distinction matters (2026-10-17 records, found 2026-10-28 in
# bug row 01a18288): pid 2 (kthreadd) is a kernel thread — an empty command line
# and a process that never exits. `read == 0` used to mean "keep it", so two
# start records claiming pid 2 read as live peers FOR EVER: every budget run
# printed a false CONCURRENT BUDGET RUN OVERLAPS block, and the remedy that block
# prints ("wait for those pids to exit") could never be satisfied, because the
# pid it names is immortal by design. The start records are never collected
# (5927 of them on this host), so the fix may not lean on the records being tidy.
#
# `pkg=` is compared through the SAME predicate the run's own scope check uses
# (gopi_scope_packages, Rule 14), so `./x/`, `./x` and a bare `x` are one package
# and neither side has to resolve paths itself.
#
# Usage (source, do not execute):
#     . "$(dirname "$0")/lib/budget-overlap.sh"
#     gopi_budget_overlapping_runs MODULE SCOPE_FILE SELF_PID RECORD…  # one row per LIVE peer

# The start record's own field names, in ONE place (Rule 1b/14): the writer is
# `write_start_record` in scripts/test-budget.sh, and every reader — this library,
# and the gate that holds both ends against each other — spells them from here.
# The trailing `=` is part of the name because that is the whole shape of the
# match: a flat `key=value` line.
gopi_budget_record_pid_field="pid="
gopi_budget_record_start_field="start_iso="
gopi_budget_record_pkgs_field="pkg="
# One peer row's field separator: a TAB, because the package field is a
# space-separated list, so a space would make the row unparsable. Both sides (the
# awk that decides liveness and the shell that formats the row) read it here.
gopi_budget_overlap_sep=$'\t'
# The entry point's own file name: the ONE thing a peer's command line has to
# mention for a live pid to be attributed to it.
gopi_budget_entry_point_name="test-budget.sh"

# gopi_budget_live_peer_records SELF_PID RECORD… — one TAB-separated row per
# record whose pid is live AND attributable to the entry point:
#
#     pid<TAB>start_iso<TAB>requested packages<TAB>record path
#
# ONE awk pass over every record, and that is a decision rather than a style: the
# host's queue holds thousands of these records (measured 2026-10-16: 1620) and
# they are never collected, so a per-record subshell would put seconds of process
# churn in front of every budget run — a reporter that slows the run it observes
# is its own defect. awk reads /proc/<pid>/cmdline with `getline < file` (no
# fork); a MISSING process makes that read return -1, which is the liveness test.
#
# SELF_PID is excluded: `$$` is reused, so a stale record of a run that happened
# to carry this pid, read inside this pid's own lifetime, would otherwise present
# THIS run as its own neighbour — the one overlap that is always false.
gopi_budget_live_peer_records() {
    local self_pid="${1:?gopi_budget_live_peer_records needs the calling run pid}"
    shift
    if [ "$#" -eq 0 ]; then
        return 0
    fi
    awk -v self="$self_pid" \
        -v pid_field="$gopi_budget_record_pid_field" \
        -v start_field="$gopi_budget_record_start_field" \
        -v pkgs_field="$gopi_budget_record_pkgs_field" \
        -v needle="$gopi_budget_entry_point_name" \
        -v sep="$gopi_budget_overlap_sep" '
        function field(line, name) { return substr(line, length(name) + 1) }
        # live_peer PID — 1 when PID is a live process whose command line names
        # the entry point. TWO read outcomes, two different facts (Rule 15 covers
        # only the first one):
        #   read < 0   the read FAILED: the /proc entry is gone, so the process is
        #              dead. An honest unknown is KEPT only when the process may
        #              still exist; here it demonstrably does not.
        #   read == 0  the read SUCCEEDED on a zero-length file: this process has
        #              no argv (a kernel thread, a zombie). That is a MEASUREMENT,
        #              not a failure to measure, and the measured answer is "it can
        #              never be the entry point" — so it is NOT a peer. Falling
        #              through to the comparison below gives the same verdict,
        #              because index of the needle in the empty string is 0.
        # The kernel-thread case is why this matters: pid 2 (kthreadd) never exits,
        # so treating a zero-length command line as unknown kept its stale start
        # records as peers for ever (bug row 01a18288).
        function live_peer(pid,    probe, read, cmd) {
            if (pid == "" || pid ~ /[^0-9]/ || pid + 0 == 0) return 0
            probe = "/proc/" pid "/cmdline"
            read = (getline cmd < probe)
            close(probe)
            if (read < 0) return 0
            gsub(/\0/, " ", cmd)
            return (index(cmd, needle) > 0)
        }
        function emit() {
            if (path != "" && pid != "" && pid != self && live_peer(pid)) {
                print pid sep start sep pkgs sep path
            }
        }
        FNR == 1 {
            emit()
            path = FILENAME; pid = ""; start = ""; pkgs = ""
        }
        index($0, pid_field) == 1 { pid = field($0, pid_field) }
        index($0, start_field) == 1 { start = field($0, start_field) }
        index($0, pkgs_field) == 1 { pkgs = field($0, pkgs_field) }
        END { emit() }
    ' "$@"
}

# gopi_budget_overlapping_runs MODULE SCOPE_FILE SELF_PID RECORD… — one
# TAB-separated row per LIVE peer run:
#
#     pid<TAB>start_iso<TAB>its requested packages<TAB>the intersection
#
# Rows for a LIVE peer whose packages do NOT intersect are emitted too, with an
# EMPTY fourth field: one stream then carries both facts — how many runs were
# alive, and which of them collide — so a run that looked and found itself alone
# is distinguishable from a run that never looked, without a second artifact.
#
# The REQUESTED packages (never the resolved import paths) are what is printed: an
# operator recognises `./internal/tools` as the string they typed, and the two
# sides must be spelled the same way for the comparison to be a comparison. The
# intersection itself is reported in RESOLVED import paths, which is what names
# the overlapping packages without ambiguity.
gopi_budget_overlapping_runs() {
    local module="${1:?gopi_budget_overlapping_runs needs the module import path}"
    local scope_file="${2:?gopi_budget_overlapping_runs needs the calling run scope file}"
    local self_pid="${3:?gopi_budget_overlapping_runs needs the calling run pid}"
    shift 3
    if [ ! -s "$scope_file" ]; then
        return 0
    fi
    local peer_pid peer_start peer_pkgs peer_scope peer_overlap
    local -a peer_pkg_args
    while IFS="$gopi_budget_overlap_sep" read -r peer_pid peer_start peer_pkgs _record; do
        if [ -z "$peer_pid" ]; then
            continue
        fi
        # Each live peer's packages come back through the SAME predicate this
        # run's own scope came from (Rule 14), so the two sides of the
        # intersection are both resolved import paths. The list is split into an
        # array and quoted, never expanded bare, so a pattern is never
        # glob-expanded against the caller's working directory.
        peer_scope="$scope_file.$peer_pid"
        peer_pkg_args=()
        read -r -a peer_pkg_args <<<"$peer_pkgs"
        if [ "${#peer_pkg_args[@]}" -gt 0 ]; then
            gopi_scope_packages "$module" "${peer_pkg_args[@]}" >"$peer_scope" 2>/dev/null
        else
            : >"$peer_scope"
        fi
        peer_overlap="$(comm -12 "$scope_file" "$peer_scope" | tr '\n' ' ')"
        rm -f "$peer_scope"
        printf '%s%s%s%s%s%s%s\n' \
            "$peer_pid" "$gopi_budget_overlap_sep" "${peer_start:-<unknown>}" \
            "$gopi_budget_overlap_sep" "${peer_pkgs:-<none recorded>}" \
            "$gopi_budget_overlap_sep" "${peer_overlap% }"
    done < <(gopi_budget_live_peer_records "$self_pid" "$@")
}
