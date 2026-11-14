#!/usr/bin/env bash
# OPERATOR-ONLY. This script launches workers, and a worker is the OPERATOR'S
# process: hermes / PA / any agent must NEVER run it (the canonical rule:
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md). `gopi worker`
# never returns, it stalls the caller, and killing the caller's process group
# takes the workers with it (the 2026-09-12 04:12 incident). An agent's verb is
# a submission (`gopi task submit`); the queue is the handoff.

# gopi-worker-up.sh — launch N detached gopi workers on THIS host, fully
# detached from the caller's session/terminal.
#
# WHY (unchanged, from the 2026-09-10/11 incident): workers started via a
# Hermes background session die when that session is torn down (observed three
# times — each time the worker's child received SIGTERM mid-task, left an
# in_progress row with a dead holder PID, and wasted the whole run). `setsid`
# puts the worker in its own session so no terminal hangup or tool-session
# cleanup can reach it.
#
# WHERE A TASK'S OUTPUT GOES (2026-09-12, epic worker-log-session-attribution;
# re-pointed 2026-09-17, epic child-stream-task-output — THIS script's
# contract changed again, and shrank):
#
# The launcher never named a per-task file and now it names no root either.
# Two earlier designs were retired on the way here:
#
#   1. `worker-<ts>-<i>.log`, in a flat public directory — a name carrying a
#      TIMESTAMP, not a TASK ID, so given a failed task there was no path from
#      the id to its output (the 04:12 incident killed 6 workers inside one
#      ~70 s window; their timestamps overlap).
#   2. GOPI_WORKER_LOG_ROOT / `--worker-log-root DIR` — the launcher told the
#      worker where to attribute, and the worker re-pointed its own
#      stdout/stderr at <root>/<task-id>/output.log the moment it CLAIMED a
#      row.
#
# Design 2 worked but put the destination on the wrong side. The destination is
# now owned by the party that knows the ROW: the executor-child spawn seam in
# cmd/agent/cli_tasks.go points the child's stdout/stderr at
# <gopi-dir>/sessions/<task-id>/output.log (cmd/agent/cli_tasks_exec_output.go)
# — the SAME tree the session JSONL lives in, decided identically for a manual
# `gopi task run`, a worker round and a serve tick. So this launcher passes NO
# root and NO flag, and the worker never re-points its own streams.
#
# What the launcher still does: it holds the stream it launched the worker with
# and appends everything the worker itself prints — the loop banner, the idle
# lines, and each round's grep-able facts (claim / attribution / workdir) —
# to ONE fixed file, <gopi-dir>/logs/worker-startup.log, instead of one file
# per launch. That file is the record of the WORKER; the task's own text is in
# the task's session directory. The `logs/` directory itself survives — it is
# where the host components record (setup/scratch-gc-watchdog.sh writes
# scratch-gc-watchdog.log there).
#
# Usage:  gopi-worker-up.sh [N] [--dry-run] [--count] [--check] [--install]
#         N              number of workers (default 2)
# Options:
#   --dry-run  print the worker commands + WHERE a task's output goes, exit 0;
#              launches NOTHING (this is the verification seam: the path
#              contract is checkable without spawning a real worker)
#   --count    print the live-process breakdown and exit 0; launches NOTHING
#              (the counting seam: the number is checkable in isolation, so a
#              test can drive it without starting a worker)
#   --check    audit the INSTALLED copy against THIS file and exit 0 only when
#              they are byte-identical; launches NOTHING. The 2026-09-12
#              drift: the operator's shell runs ~/.local/bin/<name> (via
#              ~/bin/<name>), NOT the repo's script — and that installed copy
#              was still an older build, starting workers whose output contract
#              no longer matched the repo's, and said nothing about it. A silent
#              degradation of the START path is the defect; this option is the
#              loud answer, and it is the same measure the incident was
#              diagnosed with (sha256 on both sides).
#   --install  copy this script to ~/.local/bin/<its own name> and point
#              ~/bin/<its own name> at that stable copy (the
#              install-scratch-gc-watchdog.sh precedent: the installed launcher
#              must not depend on a worktree, which comes and goes)
#
#
# WHY THE LIVE-COUNT LINE CHANGED (2026-09-12): this script used to end with
# `pgrep -x gopi | wc -l` and reported "live gopi workers: 13" while `ps`
# showed 5 workers. `pgrep -x gopi` counts every process NAMED as the gopi
# binary — the task-serve daemons, the merger, and one-shot `task …` children —
# so the number an operator reads when deciding "do I need more workers?" was
# inflated by other roles, not by workers. The count now reads
# <proc-root>/<pid>/comm (the name) AND <proc-root>/<pid>/cmdline (the argv
# shape), counting a worker only when comm == "$PROC_NAME" and argv[1] ==
# "$WORKER_VERB", and printing the worker PIDs plus the non-worker process
# count so a wrong number is self-evident. See devdoc/worker-count.md for the
# root cause and the self-match proof. The launch semantics (setsid + nohup +
# /dev/null stdin, the 1s cadence, the log path contract, --dry-run/--install)
# are unchanged, and there is NO pgrep/pkill left anywhere in this script.
#
# Env knobs (the defaults below are the single definition site of these names
# on the script side; the Go side reads internal/procname, which is the
# definition site for Go, and internal/testutil/procname_gate_test.go fails the
# build if the two disagree):
#   GOPI_PROC_NAME=gopi       the process NAME prefix that identifies a gopi
#                             process's IMAGE (bin/gopi-fix counts: the image is
#                             what runs this program, comm is only its file name)
#   GOPI_PROC_IMAGE_PREFIX=gopi  the same prefix, spelled for the caller that
#                             wants to say it once (GOPI_PROC_NAME's alias)
#   GOPI_WORKER_VERB=worker   the top-level verb that makes it a worker
#   GOPI_TASK_VERB=task       the verb whose `run` form re-execs this binary
#                             (a one-shot task child: counted as "other")
#   GOPI_PROC_ROOT=/proc      the procfs root to scan (tests point this at a
#                             synthetic <pid>/comm + <pid>/cmdline tree)
#
# THE COUNTER'S OUTPUT IS FOUR LINES AND THE ORDER IS ITS INTERFACE (a shell
# function cannot return an array): 1) worker count, 2) other count, 3) worker
# "pid:starttime" identities, 4) the THIRD STATE's identities — the processes
# whose image channel could not be read at all. The fourth line is printed even
# when it is empty; a caller that takes only the first three silently drops a
# live entry, which is the defect this line exists to end.
set -euo pipefail

# --- identity of the thing we are counting -------------------------------
# Rule 1b/13/14: no bare literal for a process/verb name. The three values are
# defined once in internal/procname (the leaf constant package) and reach this
# script through the environment; the defaults below are the definition site on
# the SCRIPT side (a shell script cannot import Go), and
# internal/testutil/procname_gate_test.go asserts every default below equals its
# Go constant — so a rename on either side turns the build red.
PROC_NAME="${GOPI_PROC_NAME:-gopi}"        # image-name PREFIX of a gopi process (bin/gopi-fix counts)
# The same prefix under its own name: the constant the Go side calls
# ImageGopiPrefix is aliased here, so the shell surface that a caller exports
# cannot be a second, silently-diverging spelling of one value (Rule 14). The
# default is written once — this line reads PROC_NAME, it does not restate "gopi".
IMAGE_PREFIX="${GOPI_PROC_IMAGE_PREFIX:-$PROC_NAME}"
WORKER_VERB="${GOPI_WORKER_VERB:-worker}"  # top-level verb of the worker loop
TASK_VERB="${GOPI_TASK_VERB:-task}"        # `task run` re-execs this same binary
PROC_ROOT="${GOPI_PROC_ROOT:-/proc}"       # procfs root (the test seam)
STAT_FIELD="${GOPI_PROC_STAT_FIELD:-22}"  # /proc/<pid>/stat field holding starttime (ticks since boot)

# The procfs FILE NAMES the scan reads (<root>/<pid>/<name>). They are the same
# contract as the knobs above — internal/procname declares the file names of a
# procfs record and this is the shell-side site — and they are named constants
# for the same reason (Rule 1b/13): a bare "exe" in the readlink call is a
# literal two readers must agree on with nothing to catch a drift.
# They are NOT operator knobs: there is no env spelling for them, because a
# caller repointing them could only make the scan read another file. The gate
# asserts each one equals its procname constant (ExeName / StatName).
readonly EXE_NAME=exe    # <root>/<pid>/exe — the kernel's answer for the IMAGE
readonly STAT_NAME=stat  # <root>/<pid>/stat — carries starttime (field $STAT_FIELD)

# The launcher names itself, so no message or installed path below repeats a
# filename literal: the script IS the definition of its own name.
SELF="${0##*/}"

N="${GOPI_WORKER_N:-2}"
REPO="${GOPI_WORKER_REPO:-$HOME/work/GoPi}"
# bin is the launcher's ONE definition of where the gopi binary lives (Rule
# 1b/13: the bin/<name> path is spelled once, here, and the check, its message
# and the launch below all read this variable). It is DERIVED from REPO on
# purpose — there is no GOPI_WORKER_BIN_DIR knob, so a caller can only be
# pointed at a checkout, never at a binary outside it (the Rule 14 gate in
# internal/testutil/procname_gate_test.go asserts that this launcher declares
# no env contract of its own).
bin="$REPO/bin/$PROC_NAME"
DRY=0
INSTALL=0
COUNT=0
CHECK=0

usage() {
  # No fixed line range: the header is read to its first `set -euo pipefail`
  # (the end of the comment block), so editing the prose above can never
  # truncate --help. The range-free form is master's fix, kept while the
  # [--count] option and the counting-seam prose of HEAD stay in place.
  sed -n '2,/^set -euo pipefail$/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

# gopi_dir resolves the gopi data root exactly like internal/config.GopiDir():
# $GOPI_HOME/.gopi when GOPI_HOME is set, else ~/.gopi. One knob, one place —
# the same env var the Go side honours, so a sandbox never writes the profile.
gopi_dir() {
  if [[ -n "${GOPI_HOME:-}" ]]; then
    printf '%s/.gopi\n' "${GOPI_HOME%/}"
  else
    printf '%s/.gopi\n' "$HOME"
  fi
}

# RETIRED_OUTPUT_OPTION / TASK_OUTPUT_SHAPE are the refusal's two facts, named
# once (Rule 1b: the spellings live in constants, never inline in a message
# that appears in three places).
RETIRED_OUTPUT_OPTION="--worker-log-root"
TASK_OUTPUT_SHAPE="<gopi-dir>/sessions/<task-id>/output.log"

positional=0
while (($#)); do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --dry-run) DRY=1; shift ;;
    --install) INSTALL=1; shift ;;
    --count) COUNT=1; shift ;;
    --check) CHECK=1; shift ;;
    --) shift; break ;;
    # The retired output-root option, and its `=` spelling. Named explicitly
    # (before the generic unknown-flag arm) so a launcher still spelled against
    # the old contract is told WHAT replaced it, not just that it is unknown
    # (Rule 15: a silently ignored option is the degradation this refusal
    # exists for).
    --worker-log-root|--worker-log-root=*)
      echo "$SELF: unexpected argument '$1' — $RETIRED_OUTPUT_OPTION is retired; a task's output goes to $TASK_OUTPUT_SHAPE (its own session directory, decided by the spawn seam, not by the launcher)" >&2
      exit 2 ;;
    -*) echo "$SELF: unknown flag: $1" >&2; usage >&2; exit 2 ;;
    *)
      if ((positional == 0)) && [[ "$1" =~ ^[0-9]+$ ]]; then
        N="$1"; positional=1
      else
        # No positional is accepted: a caller passing a directory here is
        # naming a retired log root (the per-worker public dir, or the
        # --worker-log-root tree), and refusing is the honest answer — the
        # destination now belongs to the row's own session directory and there
        # is nothing for a launcher to point it at.
        echo "$SELF: unexpected argument '$1' — the worker output root is retired; a task's output goes to $TASK_OUTPUT_SHAPE" >&2
        exit 2
      fi
      shift ;;
  esac
done

GOPI_DIR="$(gopi_dir)"
# worker-startup.log is the ONE fixed file for pre-claim worker output (the
# loop banner and idle lines that belong to no task). It never grows a file
# per launch.
STARTUP_LOG="$GOPI_DIR/logs/worker-startup.log"

# count_gopi_processes walks the procfs root and splits the processes running
# the gopi PROGRAM on the host into WORKERS and OTHERS. It is the replacement for
# the old `pgrep -x <name> | wc -l`, which answered "how many processes are
# named that" — a question whose answer (13 on the day of the report) says
# nothing about how many WORKERS are alive (5). Counting by name is not a
# counting bug that a better pattern fixes; the NAME is simply not the
# predicate the caller wants, so the scan reads the argv SHAPE instead:
#
#   worker  ⇔ the process runs the gopi program AND argv[1] == "$WORKER_VERB"
#   other   ⇔ the process runs the gopi program AND argv[1] != "$WORKER_VERB"
#
# THE PROGRAM IS IDENTIFIED BY ITS IMAGE, NOT BY ITS comm (bug 01a131fcb8337c1b,
# measured 2026-10-13). comm is the basename of the executable FILE, so a gopi
# binary built under another file name (bin/gopi-fix, a per-task build) made its
# worker INVISIBLE here: the scan compared comm to $PROC_NAME and reported "live
# gopi workers: 0" while the worker was alive. The real measurement is the
# process's image — <pid>/exe (the kernel's answer, minus the " (deleted)"
# marker), falling back to the process's OWN argv[0] — tested against
# $IMAGE_PREFIX. A prefix, not equality: an image named gopi-<anything> is still
# this program.
#
# WHAT IS STILL MISSING FROM THAT PREDICATE (the identity triple, 2/3 → 3/3):
# comm + the argv shape describe a process that LOOKS like a worker, and a pid is
# a number the kernel RECYCLES. So each reported pid is also stamped with its
# STARTTIME — <pid>/stat field $STAT_FIELD, clock ticks since boot — printed as
# "pid:ticks". starttime is the incarnation: the kernel never reuses a (pid,
# starttime) pair, so a caller that recorded an incarnation (or that re-reads
# these ticks a second later) can tell THIS worker from a stranger that inherited
# its number. Two `$PROC_NAME $WORKER_VERB` processes with the same argv are two
# different incarnations; only the ticks separate them. See internal/procname for
# the three-factor verdict (alive / recycled / unknown) and its documentation of
# what each factor defends against.
#
# WHY IT CANNOT MATCH ITS OWN INVOCATION (the failure class that has bitten
# this repo repeatedly — `pkill -f`/`pgrep -f` matching the very command line
# doing the matching):
#  1. NOTHING here matches a COMMAND LINE. The predicate is the process NAME
#     field (<root>/<pid>/comm, one line, never a command line) plus one
#     NUL-delimited field of <root>/<pid>/cmdline. This shell, the `bash -c` a
#     test wraps it in, and the caller's own command line are all named `bash`
#     (or whatever they are), so they are filtered out by the FIRST test —
#     however much their command line mentions the worker command. A caller
#     that writes that text in its argv can only be counted if it was EXECUTED
#     as a binary named "$PROC_NAME" with "$WORKER_VERB" first — i.e. if it
#     really is a worker.
#  2. NOTHING here shells out to pgrep/pkill/grep, so there is no helper
#     process carrying the pattern in its argv to find.
#  3. The task-run exclusion is POSITIVE, not a substring filter: argv[1] must
#     EQUAL the worker verb, so "$PROC_NAME $TASK_VERB run <id>" (a one-shot
#     task child re-exec'ing the same binary) is excluded by an exact
#     comparison and reported under its own number instead.
#
# THE THIRD STATE (bug 01a132a7a4ef743b, measured 2026-10-13): the image test
# above is a FILTER, and a filter that answers "no" for a process it could not
# MEASURE is the same fabricated absence as counting by comm — one channel
# further out. The contract stated at the top of this function (:277) is that "an
# unreadable factor degrades to unknown, it never silently removes a live entry",
# and the image test violated it: for a process whose /proc/<pid>/exe is
# unreadable AND whose cmdline is empty (a Z-state zombie is exactly this shape,
# and so is a restricted entry), the image is "" — the test omitted the entry
# from BOTH counters, and `other $PROC_NAME processes: N` simply read one lower
# than the old comm judge's, with nothing on screen to say so. Measured on one
# frozen snapshot: old (comm) others=6, new (image) others=5, the difference
# being a live entry no line mentions.
#
# An entry is therefore UNCONFIRMED — its own third state, printed as its own
# line with its own pid list — exactly when BOTH image channels failed (exe
# absent/unreadable AND an empty argv[0]) while comm still names the process
# $PROC_NAME. It is neither counted as a worker (the argv shape cannot even be
# read — the verb test has nothing to compare) nor as an other (the image channel
# that defines "other" never answered). It is REPORTED, so the arithmetic closes
# on screen: others_old = others_new + unconfirmed, per pid.
#
# The comm channel is the right evidence to require here, and it is the WEAKEST
# channel on purpose: unconfirmed claims nothing about WHICH program runs, only
# that the process is NAMED $PROC_NAME and cannot be measured further. Reading
# comm follows the process's own name; it never adds an entry the comm judge
# would miss, so no new false POSITIVE enters the count.
#
# Reads procfs directly (Linux) — no external tool, no pattern, no self-match,
# and no dependency on pgrep's -x semantics differing from ps's comm column.
#
# Prints FIVE lines, in this fixed order (a script cannot return an array, so the
# order IS the interface: every caller reads it by sed line number):
#   1  worker count
#   2  other-process count
#   3  unconfirmed count  — the third state's own number, never folded into
#      either of the two above
#   4  worker identities — "pid:starttime" words, space separated with a
#      leading space so every field is word-separated for `read`
#   5  unconfirmed identities — the same "pid:starttime" words for the third
#      state (empty when there are none), never omitted: a line a reader can
#      always read is what makes "zero" distinguishable from "not reported"
#      (Rule 15a).
# read_incarnation reads a process's STARTTIME — field $STAT_FIELD of
# <root>/<pid>/stat, clock ticks since boot — and echoes it, or "-" when the
# record cannot be read or does not reach that field. It is factored out (ONE
# definition) because BOTH pid lists are stamped with it: the worker list and the
# third state's list. A pid is a NUMBER the kernel RECYCLES, so a list of bare
# numbers cannot be checked against a recorded incarnation; "pid:ticks" can, and
# an unreadable stat degrades to "-" rather than removing the entry (Rule 15: the
# stamp is extra evidence, never a filter).
#
# A stat line is "<pid> (<comm>) <state> …" and comm may contain SPACES and
# PARENTHESES, so the field index is counted from the LAST ')' — a whitespace
# split would read the wrong column whenever a process name holds a space.
read_incarnation() {
  local d="$1" start="-" statline=""
  if [[ -r "$d/$STAT_NAME" ]]; then
    IFS= read -r statline < "$d/$STAT_NAME" 2>/dev/null || true
    # Everything after the LAST ')' is the positional part of the record (the
    # first token there is field 3), so ONE pass can both drop it and split the
    # rest into fields: read assigns each remaining word to a name, and `rest`
    # soaks up everything past the LAST name in the list. That is how field
    # $STAT_FIELD is reached without an intermediate array — bash has no portable
    # word-splitting form that survives `set -u` with an empty tail.
    local w1 w2 w3 w4 w5 w6 w7 w8 w9 w10 w11 w12 w13 w14 w15 w16 w17 w18 w19 rest
    # shellcheck disable=SC2162  # the record is a single line; -r keeps it verbatim
    read -r w1 w2 w3 w4 w5 w6 w7 w8 w9 w10 w11 w12 w13 w14 w15 w16 w17 w18 w19 rest <<< "${statline#*)}"
    # The words are named w1…w19 by their POSITION AFTER the ')' — measured, not
    # assumed: the first word after ')' is procfs field 3, so word N holds field
    # N+2 (checked against this host's /proc/<pid>/stat: word 11 = the wchan
    # field 13, word 20 = starttime field 22). The mapping lives here once, so
    # the shell and internal/procname cannot drift apart.
    case "$STAT_FIELD" in
      20) start="$w18" ;;   # field 20 = word 18
      21) start="$w19" ;;   # field 21 = word 19
      22) start="${rest%% *}" ;;  # field 22 = word 20 = the first word soaked into rest
      23) start="${rest#* }"; start="${start%% *}" ;;  # field 23 = the second
      *)  start="-" ;;
    esac
    [[ -n "$start" ]] || start="-"
  fi
  printf '%s' "$start"
}

count_gopi_processes() {
  local workers=0 others=0 unconfirmed=0 pids="" unpids=""
  local d pid verb start name="" imeasured=0
  local -i seen=0
  for d in "$PROC_ROOT"/[0-9]*; do
    [[ -d "$d" ]] || continue
    seen+=1
    pid="${d##*/}"
    # comm + cmdline are the SHAPE; stat carries the incarnation (starttime).
    # A process that died mid-walk has no stat to read, and is reported with
    # "-" for its incarnation rather than dropped: the count is about shapes,
    # the stamp is extra evidence, never a filter (Rule 15: an unreadable
    # factor degrades to "unknown", it never silently removes a live entry).
    [[ -r "$d/comm" && -r "$d/cmdline" ]] || continue
    # `|| true`: the kernel always appends a newline to comm, but a file without
    # one makes `read` return non-zero at EOF — the value is still read, and
    # treating that as "not a process" would silently drop live entries.
    # comm is read for ONE purpose here: it is the evidence the third state is
    # decided on (see THE THIRD STATE above). It is never the image.
    name=""
    IFS= read -r name < "$d/comm" 2>/dev/null || true
    name="${name%$'\n'}"
    # The IMAGE is what identifies the program (bug 01a131fcb8337c1b): comm is
    # the executable FILE's basename, so a gopi binary built under another name
    # would be invisible to a comm comparison. exe is the kernel's answer;
    # argv[0] is the process's own record, used when exe cannot be read (a
    # deleted image, a restricted entry). ExeName — never a bare "exe" literal
    # (Rule 1b/13/14: internal/procname declares the file names of the procfs
    # record, and the procname gate fails the build when the two disagree).
    # `imeasured` records whether EITHER channel answered: a disagreement is a
    # verdict ("not this program"), an empty pair is a MISSING MEASUREMENT, and
    # the two must not share a branch (Rule 15a).
    local image="" exe=""
    imeasured=0
    exe="$(readlink "$d/$EXE_NAME" 2>/dev/null || true)"
    if [[ -n "$exe" ]]; then
      # The kernel appends " (deleted)" when the image file was unlinked: the
      # process still IS the gopi program.
      exe="${exe% (deleted)}"
      image="${exe##*/}"
      imeasured=1
    fi
    # mapfile -d '' is a NUL-delimited read: argv fields are exact, so an
    # argument that merely CONTAINS the verb cannot pass as the verb.
    local -a argv=()
    mapfile -d '' -t argv < "$d/cmdline" 2>/dev/null || true
    if [[ -z "$image" ]]; then
      local argv0="${argv[0]:-}"
      if [[ -n "$argv0" ]]; then
        image="${argv0##*/}"
        imeasured=1
      fi
    fi
    if ((imeasured == 0)); then
      # NEITHER image channel answered. If comm names this process $PROC_NAME,
      # the entry is REPORTED — the third state, never a silent removal:
      #   not a worker (argv is empty, so the verb test has nothing to compare)
      #   not other   (the image channel that DEFINES "other" never answered)
      # It is stamped with its incarnation like any other entry, so the pid list
      # is as checkable as the worker list is.
      if [[ -n "$name" && "$name" == "$PROC_NAME" ]]; then
        unconfirmed=$((unconfirmed + 1))
        unpids+=" $pid:$(read_incarnation "$d")"
      fi
      continue
    fi
    [[ "$image" == "$IMAGE_PREFIX"* ]] || continue
    verb="${argv[1]:-}"
    if [[ -z "$verb" || "$verb" == "$TASK_VERB" || "$verb" != "$WORKER_VERB" ]]; then
      others=$((others + 1))
      continue
    fi
    start="$(read_incarnation "$d")"
    workers=$((workers + 1))
    pids+=" $pid:$start"
  done
  # An empty scan is a broken procfs root, not a host with no processes: the
  # launch path must never print "live ... workers: 0" as if it were a fact.
  if ((seen == 0)); then
    echo "$SELF: no process entries under $PROC_ROOT" >&2
    return 3
  fi
  printf '%s\n%s\n%s\n%s\n%s\n' "$workers" "$others" "$unconfirmed" "$pids" "$unpids"
}

# The operator-facing VALUES, at their single definition site (Rule 1b/13/14).
# Every W_ line below is a key=value pair on ONE line, and the "third state"
# line is printed unconditionally — including at zero — because a state that is
# only printed when it is non-zero cannot be told from a state nothing measured
# (Rule 15a). The Chinese annotations make the line auditable WITHOUT the script:
# the channel that answered is named ("neither exe nor argv[0]"), so a reader can
# re-run the measurement by hand.
readonly W_WORKERS_LABEL="live ${PROC_NAME} workers"
readonly W_OTHERS_LABEL="other ${PROC_NAME} processes"
readonly W_UNCONFIRMED_LABEL="unconfirmed ${PROC_NAME} processes"
readonly W_OTHERS_NOTE="not workers — daemons, the merger, one-shot run children"
readonly W_UNCONFIRMED_NOTE="(exe and argv[0] both unreadable — 镜像通道未确认，既非 worker 也非 other，不静默丢弃)"

# report_counts prints the operator-facing LINES: the worker PIDs, the non-worker
# count, then the third state (Rule 15: an entry the image channel could not
# measure is REPORTED, never dropped). It runs the counter ONCE and caches the
# breakdown in the WORKERS/OTHERS/UNCONFIRMED/WORKER_PIDS/UNCONFIRMED_PIDS
# globals.
#
# ANY OTHER CALLER THAT READS count_gopi_processes MUST READ line 4 TOO — a
# caller that takes only the first three lines re-creates the silent drop one
# level up, and the grep gate on this script (internal/testutil/procname_gate_test.go)
# fails such a caller by name. Use these globals instead of re-slicing the output.
report_counts() {
  local counts
  counts="$(count_gopi_processes)"
  WORKERS=$(sed -n '1p' <<<"$counts")
  OTHERS=$(sed -n '2p' <<<"$counts")
  UNCONFIRMED=$(sed -n '3p' <<<"$counts")
  WORKER_PIDS=$(sed -n '4p' <<<"$counts")
  UNCONFIRMED_PIDS=$(sed -n '5p' <<<"$counts")
  # The pid list is printed as "pid:starttime" — an IDENTITY, not a number.
  # The line keeps the word "pids:" because callers and tests read that
  # parenthetical; what changed is the value: a number alone cannot be checked
  # for recycling, a (pid, starttime) pair can.
  echo "$W_WORKERS_LABEL: $WORKERS (pids:${WORKER_PIDS:- none})"
  echo "$W_OTHERS_LABEL: $OTHERS ($W_OTHERS_NOTE)"
  echo "$W_UNCONFIRMED_LABEL: $UNCONFIRMED (pids:${UNCONFIRMED_PIDS:- none}) ($W_UNCONFIRMED_NOTE)"
}
# NOTE for any OTHER caller that parses this report: the two count LINES above
# keep their existing label and shape, so a caller that reads one of them by
# regex (`^live <name> workers: N` / `^other <name> processes: N`) is unaffected
# by the third line — scripts/peer-worker/peer-launcher-autostart.sh reads the worker line
# exactly that way. What a parser must NOT do is assume the report ENDS after
# the two lines it knows: the third state is part of the measurement, and a
# reader that stops early is the silent drop this branch removed.

# verify_launched_workers announces the launch's OUTCOME (epic
# worker-spawn-doors; decision record devdoc/worker-display-ownership.md,
# "Decision 2"). It is the launcher's half of Rule 15 (no silent failure).
#
# THE DEFECT IT CLOSES (measured 2026-09-14, this host): the launcher loops
# N times, prints "started worker $i: pid $pid" for each iteration, and then
# reports whatever the host-wide count happens to be. A child that REFUSED to
# start (the agent-spawn guard exits 3 from an agent's environment) still
# gets its "started" line — the pid printed is the short-lived wrapper's — so
# a launcher that started ZERO workers prints exactly what a launcher that
# started N workers prints, exits 0, and leaves the operator believing the
# fleet is up. Nothing in the output distinguished the two cases.
#
# WHY IT REUSES THE COUNT INSTEAD OF MEASURING THE PIDS IT PRINTED (measured on
# this host: `setsid nohup sleep 30 & echo $!` → 1591838, `ps -o comm` = bash,
# and the real child 1591840 is one level below in its own session): `$!` is
# the PID of the `setsid` WRAPPER the shell forked — setsid forks its own child
# into the new session and waits for it — so `kill -0 <printed pid>` (and any
# `pgrep -f`, which self-matches) answers a question about the wrong process.
# The count uses the
# one predicate that answers the real question — a process named $PROC_NAME
# whose argv[1] is the worker verb (count_gopi_processes) — which is also why
# this needs NO new tool and NO new env knob (the procname gate forbids a knob
# the Go side does not declare: Rule 1b/13/14/21).
#
# WHEN IT IS SILENT: an agent that ran this script did not create workers
# (its children refuse), while the operator's fleet may be untouched — so this
# VERDICT is deliberately worded as evidence about THIS LAUNCH, never as an
# accusation. The frozen-host path (recorded incident) is handled by the
# caller: read the post-launch count, and when it is unchanged and not moving,
# say so and stop — do not launch a second supervisor to "fix" it.
#
# Returns 0 when the launch produced evidence of at least one live worker,
# 1 otherwise (after printing the diagnosis).
verify_launched_workers() {
  report_counts
  if ((WORKERS > 0)); then
    echo "launch verdict: $started start request(s) issued, $WORKERS live $PROC_NAME worker(s) (pids:${WORKER_PIDS:- none})"
    return 0
  fi
  echo "$SELF: launch verdict: UNCONFIRMED — $started start request(s) were issued but NO live $PROC_NAME worker is running" >&2
  echo "$SELF:   the usual cause is the agent-spawn guard: an agent's environment is refused (exit 3) while the launcher still prints its pid line. Either this launch was refused, or the host is frozen; in BOTH cases do NOT start a second supervisor." >&2
  echo "$SELF:   recovery: submit the work as a task (the queue is the handoff), or re-run this script from the OPERATOR's own shell (WORKER-OWNERSHIP: OPERATOR-ONLY, devdoc/worker-ownership.md)." >&2
  return 1
}

# installed_copy_matches reports whether the INSTALLED copy of this script
# carries this file's exact bytes — the measure the 2026-09-12 drift was
# diagnosed with (sha256 on both sides). Echoes a one-line verdict; returns
# 0 identical / 1 differs / 2 no installed copy.
#
# WHY THIS EXISTS AT ALL: the operator's shell runs <HOME>/.local/bin/<name>
# (reached through the <HOME>/bin/<name> symlink), not the repo's script. So a
# merge that changes this file changes nothing about how workers are actually
# started until --install runs — exactly how the host kept launching workers
# whose output contract had drifted from the repo's while every test of the
# repo copy stayed green. `--install` is the seam; this is the seam's alarm.
installed_copy_matches() {
  local stable="$HOME/.local/bin/$SELF" link="$HOME/bin/$SELF"
  if [[ ! -e "$stable" ]]; then
    echo "no installed copy at $stable (nothing to audit; run --install to create it)"
    return 2
  fi
  local want got
  want="$(sha256sum "${BASH_SOURCE[0]}" | awk '{print $1}')"
  got="$(sha256sum "$stable" | awk '{print $1}')"
  if [[ "$want" == "$got" ]]; then
    echo "identical: $stable = ${got:0:16}… (repo copy ${want:0:16}…)"
    if [[ -L "$link" ]]; then
      echo "linked:    $link -> $(readlink "$link")"
    else
      echo "NOT linked: $link is not a symlink to the stable copy (an operator's PATH may resolve elsewhere)"
    fi
    return 0
  fi
  echo "STALE: $stable is NOT this script's bytes"
  echo "  installed sha256 ${got:0:16}…"
  echo "  repo      sha256 ${want:0:16}…"
  echo "  fix: ${BASH_SOURCE[0]} --install"
  return 1
}

if ((INSTALL)); then
  stable_dir="$HOME/.local/bin"
  stable="$stable_dir/$SELF"
  mkdir -p "$stable_dir"
  install -m 0755 "${BASH_SOURCE[0]}" "$stable"
  echo "installed: $stable"
  if [[ -d "$HOME/bin" ]]; then
    ln -sfn "$stable" "$HOME/bin/$SELF"
    echo "linked:    $HOME/bin/$SELF -> $stable"
  fi
  exit 0
fi

# The --check seam audits the INSTALLED copy and launches NOTHING. It is
# deliberately ordered with --install (both are about the stable copy) and
# before the launch prerequisites: neither intent starts a worker.
if ((CHECK)); then
  installed_copy_matches
  exit $?
fi

# The --count seam is interactive-only: it reports the live breakdown and
# launches NOTHING, so it must run BEFORE the launch prerequisites (the binary
# check below), which a counting caller has no reason to satisfy.
if ((COUNT)); then
  report_counts
  exit 0
fi

if [[ ! -x "$bin" ]]; then
  echo "$SELF: $bin is missing or not executable (build with: make build)" >&2
  [[ $DRY -eq 1 ]] || exit 1
fi

if ((DRY)); then
  echo "dry-run: N=$N repo=$REPO startup_log=$STARTUP_LOG"
  echo "dry-run: task output = $GOPI_DIR/sessions/<task-id>/output.log (opened by the executor-child spawn seam, same for every spawn path)"
  for i in $(seq 1 "$N"); do
    echo "dry-run: worker $i -> $bin worker (no output flag: the destination belongs to the row)"
  done
  exit 0
fi

mkdir -p "$(dirname "$STARTUP_LOG")"

cd "$REPO" || { echo "gopi-worker-up: cannot cd $REPO" >&2; exit 1; }

started=0
for i in $(seq 1 "$N"); do
  # setsid + nohup + stdin from /dev/null: no controlling terminal, no SIGHUP,
  # no accidental stdin consumption. NO output flag and NO output root: the
  # worker's own stream (this redirect) is the pre-claim record, and a task's
  # text is written by the executor child into the row's session directory
  # (see the header) whatever launched it. The fixed pre-claim file is
  # $STARTUP_LOG.
  setsid nohup "$bin" worker \
    >> "$STARTUP_LOG" 2>&1 < /dev/null &
  pid=$!
  started=$((started + 1))
  # "requested", not "started": the pid is the setsid child the shell forked,
  # and whether a WORKER came of it is decided below by the argv-shape count —
  # a refused child (agent-spawn guard, exit 3) used to be reported here as a
  # started worker, which is the silent failure verify_launched_workers closes.
  echo "requested worker $i: pid $pid (task output under $GOPI_DIR/sessions/<task-id>/output.log)"
  sleep 1
done

sleep 3
# The line the operator reads before deciding to launch more workers, plus the
# verdict on whether THIS launch actually produced a live worker. Only workers
# are gated on; the breakdown is informational, so an unreadable procfs entry
# never fails the launch — but "no worker at all" does (a refusal must never
# exit 0: Rule 15).
verify_launched_workers
