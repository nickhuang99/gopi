#!/usr/bin/env bash
#
# voice-mic-probe.sh — deterministic, NON-INTERACTIVE audio-capture inventory
# and per-device capability probe for the gopi voice-input client.
#
# What it does, in a fixed order (no prompts, no state mutation):
#   0. privilege     — is passwordless `sudo -n` available? Every capture cell is
#                      measured at BOTH privilege levels when it is
#   1. identity      — hostname, uname, date, kernel ALSA version
#   2. inventory     — /proc/asound/cards, /proc/asound/pcm, arecord -l, arecord -L
#                      UNPRIVILEGED **and** PRIVILEGED. A uid outside group
#                      `audio` with no /dev/snd ACL cannot resolve a card at
#                      all, and ALSA then prints "no soundcards found" — a
#                      PERMISSION artifact that must NEVER be read as "this node
#                      has no capture hardware" (the defect this probe fixes)
#   3. usb           — lsusb (guarded: absent lsusb is recorded, not fatal)
#   4. pulse         — pactl info, pactl list short sources, pactl list short sinks
#   5. permissions   — /dev/snd listing, user groups vs 'audio' group
#   6. hw params     — arecord --dump-hw-params for every capture-capable device
#   7. capture probe — for each candidate device string, N trials of a 1s capture
#                      at (16kHz mono S16_LE) and (native rate/2ch S16_LE), in
#                      BOTH privilege modes (user first, root when sudo -n is
#                      available). A device that only captures as root is
#                      reported SUDO_REQUIRED — never NO_DEVICE/NO_SOUNDCARDS
#
# Output: labelled raw dumps on stdout PLUS a machine-readable TSV table
# between the VOICE_MIC_TABLE_BEGIN/END sentinels. Raw dumps are also written
# to $OUT (default: ./voice-hw-probe-<node>/) so a caller can archive them.
#
# EXIT CODE: 0 whenever the probe itself ran to completion. A node with no
# capture device is a RESULT (outcome=NO_SOUNDCARDS), never an error. The
# script exits non-zero only when it cannot run at all (bad usage) — this is
# what makes it usable on the capture-less node without breaking CI.
#
# Determinism: every external command runs under an explicit timeout with stdin
# from /dev/null, LC_ALL=C, and a stable device/order; the table has a fixed
# column set and stable sort order, so two runs on an unchanged node differ
# only in the timestamp column of the header comment.

set -u
set -o pipefail

# ── Constants (Rule 1: no magic numbers / bare string keywords inline) ────────
readonly PROBE_DURATION_SEC=1          # length of every capture trial
readonly CMD_TIMEOUT_SEC=8             # per-external-command wall clock cap
readonly HW_PARAM_TIMEOUT_SEC=6        # --dump-hw-params is fast but can block
readonly PULSE_TIMEOUT_SEC=5           # pactl against a dead socket must not hang
readonly SUDO_TIMEOUT_SEC=5            # `sudo -n` must never block on a prompt
# SIGKILL escalation for the privileged path. sudoers here carries `use_pty`,
# so sudo runs the child on a new pty and does NOT die on timeout's SIGTERM: a
# privileged arecord blocking on a busy PCM held the probe for minutes (measured
# 3m41s on zpeer 2026-09-13) despite the timeout. -k makes the time cap real.
readonly SUDO_KILL_GRACE_SEC=2
readonly DEFAULT_RATE=16000            # the voice-client contract rate
readonly DEFAULT_CHANNELS=1            # the voice-client contract channel count
readonly NATIVE_RATE=48000             # the rate ALSA reports as card-native here
readonly NATIVE_CHANNELS=2             # ... and its channel count
readonly SAMPLE_FORMAT=S16_LE          # the voice-client contract sample format
readonly DEFAULT_TRIALS=2              # trials per (device,rate,channels) cell
readonly WAV_HEADER_BYTES=44           # arecord writes a RIFF header before PCM
readonly MAX_LIST_LINES=120            # cap on line-oriented dumps in stdout
# timeout(1) exit statuses: 124 = the SIGTERM deadline fired, 137 = its SIGKILL
# escalation (128+SIGKILL) fired — both mean "the wall clock, not the device".
readonly TIMEOUT_EXIT_STATUS=124
readonly TIMEOUT_KILL_EXIT_STATUS=137

readonly DEFAULT_OUT_DIR="voice-hw-probe"
readonly NODE_ENV_VAR="GOPI_VOICE_PROBE_NODE"
readonly TRIALS_ENV_VAR="GOPI_VOICE_PROBE_TRIALS"
readonly OUT_ENV_VAR="GOPI_VOICE_PROBE_OUT"

# Outcome vocabulary — the values a client may branch on (stable strings).
readonly OUTCOME_OK="OK"
readonly OUTCOME_EMPTY="EMPTY"
readonly OUTCOME_BUSY="BUSY"
readonly OUTCOME_NO_DEVICE="NO_DEVICE"
readonly OUTCOME_NO_SOUNDCARDS="NO_SOUNDCARDS"
readonly OUTCOME_PERMISSION="PERMISSION"
readonly OUTCOME_UNSUPPORTED="HW_PARAM_UNSUPPORTED"
readonly OUTCOME_PULSE_REFUSED="PULSE_REFUSED"
readonly OUTCOME_ERROR="ERROR"
# The privilege dimension (the whole point of this probe revision):
#   SUDO_REQUIRED     — this device/rate/channel DID capture, but only privileged
#   SUDO_UNAVAILABLE  — no passwordless sudo here, so the privilege level was NOT
#                       measured (recorded, never silently missing)
readonly OUTCOME_SUDO_REQUIRED="SUDO_REQUIRED"
readonly OUTCOME_SUDO_UNAVAILABLE="SUDO_UNAVAILABLE"
# A cell whose open blocked past the wall clock (the privileged arecord on a
# PCM another process holds). Distinct from ERROR: the device was never
# disproved, the probe simply refused to wait forever.
readonly OUTCOME_TIMEOUT="TIMEOUT"

# Privilege modes — the value carried in the table's `mode` column.
readonly PRIV_MODE_USER="user"
readonly PRIV_MODE_ROOT="root"
readonly SUDO_AVAILABLE_YES="yes"
readonly SUDO_AVAILABLE_NO="no"

# External binaries / flags and ALSA's misleading phrasing, each spelled once.
readonly SUDO_BINARY="sudo"
readonly SUDO_NONINTERACTIVE_FLAG="-n"
readonly GETFACL_BINARY="getfacl"
readonly ARECORD_BINARY="arecord"
readonly ARECORD_LIST_FLAG="-l"
readonly ARECORD_LIST_PCMS_FLAG="-L"
readonly ARECORD_STDOUT_TARGET="-"
readonly ARECORD_WAV_TYPE="wav"
readonly ARECORD_TYPE_FLAG="-t"
readonly ARECORD_NO_SOUNDCARDS_MARKER="no soundcards found"
readonly ARECORD_LIST_ROW_PREFIX="card "

readonly TABLE_BEGIN="VOICE_MIC_TABLE_BEGIN"
readonly TABLE_END="VOICE_MIC_TABLE_END"

# Device strings probed for EVERY capture-capable card / always, in this order:
#   hw:<card>,0     raw ALSA, no conversion (fails when params are unsupported)
#   plughw:<card>,0 raw ALSA + plug conversion (the client's first choice)
#   default         ALSA default (dmix/plug chain; Pulse-routed when configured)
#   pulse           the PulseAudio ALSA plugin (honours pactl source selection)
#   sysdefault      ALSA's "sane first device" alias
#   front            the card's front PCM
readonly -a CARD_DEVICE_PATTERNS=("hw:%d,0" "plughw:%d,0")
readonly -a GLOBAL_DEVICES=("default" "pulse" "sysdefault")
# Card-ID forms (ALSA's stable, index-independent spelling) are built in the
# enumeration step from /proc/asound/cards: plughw:CARD=<id>,DEV=0 and friends.
readonly CARD_ID_PLUGHW_FMT='plughw:CARD=%s,DEV=0'
readonly CARD_ID_FRONT_FMT='front:CARD=%s,DEV=0'
readonly CARD_ID_SYSDEFAULT_FMT='sysdefault:CARD=%s'

# ── Helpers ───────────────────────────────────────────────────────────────────
export LC_ALL=C

OUT_DIR=""
NODE_NAME=""
TRIALS="$DEFAULT_TRIALS"

log() { printf '%s\n' "$*"; }

section() {
    printf '\n===== %s =====\n' "$*"
}

# Run a command non-interactively under a timeout; print stdout, and stderr
# prefixed so a reader can tell the two apart. Never aborts the script.
run_capture() {          # run_capture <timeout_sec> <artifact> <cmd...>
    local tmo="$1"; shift
    local artifact="$1"; shift
    if [ -n "$artifact" ]; then
        timeout "$tmo" "$@" </dev/null >"$artifact" 2>&1
    else
        timeout "$tmo" "$@" </dev/null 2>&1
    fi
}

dump_lines() {           # dump_lines <title> <file>
    local title="$1" file="$2"
    section "$title"
    if [ ! -s "$file" ]; then
        log "(empty)"
        return 0
    fi
    head -n "$MAX_LIST_LINES" "$file"
    local total
    total="$(wc -l <"$file")"
    if [ "$total" -gt "$MAX_LIST_LINES" ]; then
        log "... (${total} lines total; full dump in ${file})"
    fi
}

# Classify a probe attempt from its exit code, captured bytes and stderr text.
# This is the ONLY place the outcome vocabulary is decided.
classify() {             # classify <rc> <data_bytes> <stderr_file> -> stdout
    local rc="$1" bytes="$2" errf="$3"
    if [ "$rc" -eq 0 ]; then
        if [ "$bytes" -gt 0 ]; then
            printf '%s' "$OUTCOME_OK"
        else
            printf '%s' "$OUTCOME_EMPTY"
        fi
        return 0
    fi
    if grep -qi "$ARECORD_NO_SOUNDCARDS_MARKER" "$errf" 2>/dev/null; then
        printf '%s' "$OUTCOME_NO_SOUNDCARDS"; return 0
    fi
    if grep -qi 'permission denied' "$errf" 2>/dev/null; then
        printf '%s' "$OUTCOME_PERMISSION"; return 0
    fi
    # Parameter conversations are checked BEFORE busy: when an audio server holds
    # the raw PCM, arecord reports the open failure AND the parameter complaint,
    # and the parameter verdict is the deterministic, reproducible one (ALSA
    # reports parameters from a fresh configuration); OTOH a pure "Device or
    # resource busy" with no parameter text is genuine contention.
    if grep -qiE 'channels count non available|sample format non available|rate.*non available|unable to install hw params|invalid argument' "$errf" 2>/dev/null; then
        printf '%s' "$OUTCOME_UNSUPPORTED"; return 0
    fi
    if grep -qi 'device or resource busy' "$errf" 2>/dev/null; then
        printf '%s' "$OUTCOME_BUSY"; return 0
    fi
    if grep -qiE 'connection refused|failed to connect|no such entity' "$errf" 2>/dev/null; then
        printf '%s' "$OUTCOME_PULSE_REFUSED"; return 0
    fi
    # The wall clock, not the device: timeout(1) reports 124 on SIGTERM and
    # 137 on its SIGKILL escalation.
    if [ "$rc" -eq "$TIMEOUT_EXIT_STATUS" ] || [ "$rc" -eq "$TIMEOUT_KILL_EXIT_STATUS" ]; then
        printf '%s' "$OUTCOME_TIMEOUT"; return 0
    fi
    if grep -qiE 'no such file or directory|cannot find card|cannot get card index|unknown pcm|unknown device|no such device' "$errf" 2>/dev/null; then
        printf '%s' "$OUTCOME_NO_DEVICE"; return 0
    fi
    printf '%s' "$OUTCOME_ERROR"
}

# Run a privileged command non-interactively, under a REAL wall clock.
#
# Two nesting levels, both needed:
#   outer `timeout -k` bounds sudo itself (a sudo that would prompt must fail
#   fast instead of hanging the probe);
#   inner `timeout -k` runs the command AS ROOT, which is the only way to bound
#   it — sudoers' `use_pty` puts the child on a new pty, so killing sudo (even
#   with SIGKILL) leaves the child alive and holding the PCM. Measured on zpeer
#   2026-09-13: an orphaned privileged `arecord --dump-hw-params` outlived two
#   killed probe runs and made every later privileged cell read BUSY.
run_sudo() {             # run_sudo <timeout_sec> <cmd...>
    local tmo="$1"; shift
    timeout -k "$SUDO_KILL_GRACE_SEC" "$tmo" \
        "$SUDO_BINARY" "$SUDO_NONINTERACTIVE_FLAG" \
        timeout -k "$SUDO_KILL_GRACE_SEC" "$tmo" "$@" </dev/null
}

# ── Capture core ──────────────────────────────────────────────────────────────
# One capture trial cell, ONE privilege level.
#
# Root never writes the artifact itself: arecord hands the WAV to STDOUT and the
# shell (running as the caller) redirects it, so the file stays user-owned and
# removable. `sudo arecord … file` instead would leave a root-owned file in the
# caller's artifact directory (measured: owner root:root).
probe_trial() {          # probe_trial <device> <rate> <channels> <trial_no> <mode>
    local device="$1" rate="$2" channels="$3" trial="$4" mode="$5"
    local slug bytes data_bytes errf artifact rc outcome err1
    slug="$(printf '%s_%s_r%s_c%s_t%s' "$device" "$mode" "$rate" "$channels" "$trial" | tr -c 'A-Za-z0-9_.-' '_')"
    artifact="${OUT_DIR}/capture-${slug}.wav"
    errf="${OUT_DIR}/capture-${slug}.err"
    rm -f "$artifact" "$errf"

    if [ "$mode" = "$PRIV_MODE_USER" ]; then
        timeout "$CMD_TIMEOUT_SEC" "$ARECORD_BINARY" -D "$device" -d "$PROBE_DURATION_SEC" \
            -f "$SAMPLE_FORMAT" -r "$rate" -c "$channels" \
            "$ARECORD_TYPE_FLAG" "$ARECORD_WAV_TYPE" "$ARECORD_STDOUT_TARGET" \
            </dev/null >"$artifact" 2>"$errf"
    else
        run_sudo "$CMD_TIMEOUT_SEC" "$ARECORD_BINARY" -D "$device" -d "$PROBE_DURATION_SEC" \
            -f "$SAMPLE_FORMAT" -r "$rate" -c "$channels" \
            "$ARECORD_TYPE_FLAG" "$ARECORD_WAV_TYPE" "$ARECORD_STDOUT_TARGET" \
            >"$artifact" 2>"$errf"
    fi
    rc=$?
    bytes=0
    [ -f "$artifact" ] && bytes="$(stat -c%s "$artifact" 2>/dev/null || echo 0)"
    data_bytes=$((bytes > WAV_HEADER_BYTES ? bytes - WAV_HEADER_BYTES : 0))
    outcome="$(classify "$rc" "$data_bytes" "$errf")"
    err1="$(tr '\n\t' '  ' <"$errf" 2>/dev/null | cut -c1-160)"
    # TSV row: fixed column set, stable order, `mode` LAST so a parser reading
    # the older ten columns keeps working.
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$NODE_NAME" "$device" "$rate" "$channels" "$SAMPLE_FORMAT" \
        "$trial" "$rc" "$data_bytes" "$outcome" "$err1" "$mode" >>"$TABLE_ROWS_FILE"
}

# Both privilege levels for one device/rate/channels cell. The USER rows come
# first (the harness's own capability today); the ROOT rows are what separate a
# missing device from an access refusal.
probe_cell_both_modes() { # probe_cell_both_modes <device> <rate> <channels> <trial>
    local device="$1" rate="$2" channels="$3" trial="$4"
    local user_outcome root_outcome
    probe_trial "$device" "$rate" "$channels" "$trial" "$PRIV_MODE_USER"
    user_outcome="$(awk -F'\t' -v d="$device" -v r="$rate" -v c="$channels" -v t="$trial" \
        -v m="$PRIV_MODE_USER" '$2==d && $3==r && $4==c && $6==t && $11==m {print $9}' "$TABLE_ROWS_FILE" | tail -1)"

    if [ "$SUDO_STATE" != "$SUDO_AVAILABLE_YES" ]; then
        # The privilege level was NOT measured. Say so explicitly rather than
        # leaving the cell looking like a plain device failure.
        printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
            "$NODE_NAME" "$device" "$rate" "$channels" "$SAMPLE_FORMAT" \
            "$trial" "-" "0" "$OUTCOME_SUDO_UNAVAILABLE" \
            "no passwordless sudo on this node — the root row was not measured" \
            "$PRIV_MODE_ROOT" >>"$TABLE_ROWS_FILE"
        return 0
    fi

    probe_trial "$device" "$rate" "$channels" "$trial" "$PRIV_MODE_ROOT"
    root_outcome="$(awk -F'\t' -v d="$device" -v r="$rate" -v c="$channels" -v t="$trial" \
        -v m="$PRIV_MODE_ROOT" '$2==d && $3==r && $4==c && $6==t && $11==m {print $9}' "$TABLE_ROWS_FILE" | tail -1)"

    # The verdict that motivated this revision: the user is refused, root
    # captures. The cause is an access-control refusal, and it must NOT be
    # reported as "this node has no capture path".
    if [ "$root_outcome" = "$OUTCOME_OK" ] && [ "$user_outcome" != "$OUTCOME_OK" ]; then
        printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
            "$NODE_NAME" "$device" "$rate" "$channels" "$SAMPLE_FORMAT" \
            "$trial" "0" "0" "$OUTCOME_SUDO_REQUIRED" \
            "captures as root; the unprivileged attempt was ${user_outcome} (access control, not absent hardware)" \
            "$PRIV_MODE_USER" >>"$TABLE_ROWS_FILE"
    fi
}

# ── Argument handling (no prompts — everything comes from argv/env) ───────────
while [ $# -gt 0 ]; do
    case "$1" in
        --out) OUT_DIR="$2"; shift 2 ;;
        --node) NODE_NAME="$2"; shift 2 ;;
        --trials) TRIALS="$2"; shift 2 ;;
        -h|--help)
            cat <<'USAGE'
voice-mic-probe.sh [--out DIR] [--node NAME] [--trials N]

Deterministic non-interactive audio-capture inventory + per-device probe.
Always exits 0 when the probe ran; a device-less node is a recorded result.

Every capture cell is probed at BOTH privilege levels (as the calling user, and
as root when passwordless `sudo -n` is available). A device that only captures
privileged is reported SUDO_REQUIRED — an access-control fact, never
"no capture hardware": ALSA renders an access refusal as "no soundcards found".

EXIT CODE: 0 whenever the probe itself ran to completion. A node with no
capture device is a RESULT (outcome=NO_SOUNDCARDS), never an error. The script
exits non-zero only when it cannot run at all (bad usage) — this is
what makes it usable on the capture-less node without breaking CI.

Env: GOPI_VOICE_PROBE_NODE, GOPI_VOICE_PROBE_TRIALS, GOPI_VOICE_PROBE_OUT
USAGE
            exit 0
            ;;
        *) log "voice-mic-probe: unknown argument: $1" >&2; exit 2 ;;
    esac
done

[ -n "$NODE_NAME" ] || NODE_NAME="${!NODE_ENV_VAR:-$(hostname -s 2>/dev/null || hostname)}"
[ -n "$OUT_DIR" ] || OUT_DIR="${!OUT_ENV_VAR:-${DEFAULT_OUT_DIR}-${NODE_NAME}}"
TRIALS_OVERRIDE="${!TRIALS_ENV_VAR:-}"
case "$TRIALS_OVERRIDE" in
    ''|*[!0-9]*) ;;                 # absent or junk: keep the default (Rule 2)
    *) TRIALS="$TRIALS_OVERRIDE" ;;
esac
mkdir -p "$OUT_DIR" || exit 2
TABLE_ROWS_FILE="${OUT_DIR}/table.tsv"
: >"$TABLE_ROWS_FILE"

have() { command -v "$1" >/dev/null 2>&1; }

# ── 0. Privilege layer (the defect this revision fixes) ───────────────────────
# ALSA resolves a card by opening its /dev/snd control node. A uid outside group
# `audio` with no ACL entry is refused, and libasound then reports the refusal as
# "no soundcards found" / "Cannot get card index for N" — phrasing that reads as
# "no hardware" while it is access control. Measured on zpeer 2026-09-13:
# /dev/snd is mode 660 root:audio with an ACL for the display manager only, so
# `arecord -l` is empty for uid 1000 while the ALC3228 capture node is present
# and capturing (the same uid + group audio, or root, records 32000 B/s).
# SUDO_STATE is decided ONCE here and read by the capture loop.
SUDO_STATE="$SUDO_AVAILABLE_NO"
detect_privilege() {
    have "$SUDO_BINARY" || { SUDO_STATE="$SUDO_AVAILABLE_NO"; return 0; }
    # -n = never prompt, and a wall clock so a sudo waiting on a password (or a
    # tty) can never hang the probe; both are what keep this non-interactive.
    # -n = never prompt; the wall clock plus SIGKILL escalation bound a sudo that
    # cannot complete (see SUDO_KILL_GRACE_SEC for the measured `use_pty` case).
    if timeout -k "$SUDO_KILL_GRACE_SEC" "$SUDO_TIMEOUT_SEC" "$SUDO_BINARY" \
        "$SUDO_NONINTERACTIVE_FLAG" true </dev/null >/dev/null 2>&1; then
        SUDO_STATE="$SUDO_AVAILABLE_YES"
    else
        SUDO_STATE="$SUDO_AVAILABLE_NO"
    fi
}
detect_privilege

section "PRIVILEGE"
log "sudo_available=${SUDO_STATE}"
log "effective_uid=$(id -u 2>/dev/null) groups=$(id -Gn 2>/dev/null)"
log "note=ALSA reports an ACCESS refusal as '${ARECORD_NO_SOUNDCARDS_MARKER}' — the capture loop measures both privilege levels"

# ── 1. Identity ───────────────────────────────────────────────────────────────
section "IDENTITY"
log "node=${NODE_NAME}"
log "hostname=$(hostname 2>/dev/null)"
log "uname=$(uname -srm 2>/dev/null)"
log "date=$(date -Is 2>/dev/null)"
log "user=$(id -un 2>/dev/null) uid=$(id -u 2>/dev/null)"
log "out_dir=${OUT_DIR}"
log "trials=${TRIALS}"
log "sudo_available=${SUDO_STATE}"
for tool in arecord aplay pactl parecord lsusb amixer; do
    if have "$tool"; then log "tool:${tool}=$(command -v "$tool")"; else log "tool:${tool}=ABSENT"; fi
done
{ echo "hostname=$(hostname 2>/dev/null)"; echo "uname=$(uname -srm 2>/dev/null)"; echo "date=$(date -Is 2>/dev/null)"; } \
    >"${OUT_DIR}/identity.txt"

# ── 2. Inventory ──────────────────────────────────────────────────────────────
section "PROC_ASOUND_CARDS"
if [ -r /proc/asound/cards ]; then
    cat /proc/asound/cards >"${OUT_DIR}/proc-asound-cards.txt" 2>&1
else
    echo "(no /proc/asound/cards on this node — no ALSA sound card support)" >"${OUT_DIR}/proc-asound-cards.txt"
fi
cat "${OUT_DIR}/proc-asound-cards.txt"

section "PROC_ASOUND_PCM"
if [ -r /proc/asound/pcm ]; then
    cat /proc/asound/pcm >"${OUT_DIR}/proc-asound-pcm.txt" 2>&1
else
    echo "(no /proc/asound/pcm on this node)" >"${OUT_DIR}/proc-asound-pcm.txt"
fi
cat "${OUT_DIR}/proc-asound-pcm.txt"

if have "$ARECORD_BINARY"; then
    "$ARECORD_BINARY" "$ARECORD_LIST_FLAG" </dev/null >"${OUT_DIR}/arecord-l.txt" 2>&1
    dump_lines "ARECORD_L (capture hardware devices)" "${OUT_DIR}/arecord-l.txt"
    # arecord -L can be long (every plugin PCM) — cap in stdout, keep the file.
    timeout "$CMD_TIMEOUT_SEC" "$ARECORD_BINARY" "$ARECORD_LIST_PCMS_FLAG" </dev/null >"${OUT_DIR}/arecord-L.txt" 2>&1
    section "ARECORD_L_PCMS (first ${MAX_LIST_LINES} lines; full: ${OUT_DIR}/arecord-L.txt)"
    head -n "$MAX_LIST_LINES" "${OUT_DIR}/arecord-L.txt"
else
    log "arecord ABSENT — capture probes will be recorded as ${OUTCOME_ERROR}"
    printf 'arecord ABSENT\n' >"${OUT_DIR}/arecord-l.txt"
    printf 'arecord ABSENT\n' >"${OUT_DIR}/arecord-L.txt"
fi

# The SAME two listings privileged. On a node where uid 1000 is refused, the
# unprivileged pair above is EMPTY or misleading ("no soundcards found") while
# these carry the real hardware — the difference has repeatedly been misread as
# "the node has no capture device", which is why both are recorded.
section "PRIVILEGED_INVENTORY"
if [ "$SUDO_STATE" = "$SUDO_AVAILABLE_YES" ] && have "$ARECORD_BINARY"; then
    run_sudo "$CMD_TIMEOUT_SEC" "$ARECORD_BINARY" "$ARECORD_LIST_FLAG" \
        >"${OUT_DIR}/arecord-l-privileged.txt" 2>&1
    dump_lines "ARECORD_L_PRIVILEGED (capture hardware devices, as root)" \
        "${OUT_DIR}/arecord-l-privileged.txt"
    run_sudo "$CMD_TIMEOUT_SEC" "$ARECORD_BINARY" "$ARECORD_LIST_PCMS_FLAG" \
        >"${OUT_DIR}/arecord-L-privileged.txt" 2>&1
    section "ARECORD_L_PCMS_PRIVILEGED (first ${MAX_LIST_LINES} lines; full: ${OUT_DIR}/arecord-L-privileged.txt)"
    head -n "$MAX_LIST_LINES" "${OUT_DIR}/arecord-L-privileged.txt"
else
    log "no passwordless sudo (${SUDO_STATE}) — privileged inventory NOT measured"
    printf 'sudo unavailable — not measured\n' >"${OUT_DIR}/arecord-l-privileged.txt"
    printf 'sudo unavailable — not measured\n' >"${OUT_DIR}/arecord-L-privileged.txt"
fi

# ── 3. USB ────────────────────────────────────────────────────────────────────
if have lsusb; then
    lsusb </dev/null >"${OUT_DIR}/lsusb.txt" 2>&1 || true
    dump_lines "LSUSB" "${OUT_DIR}/lsusb.txt"
else
    printf '(lsusb absent)\n' >"${OUT_DIR}/lsusb.txt"
    section "LSUSB"
    log "(lsusb absent on this node)"
fi

# ── 4. PulseAudio ─────────────────────────────────────────────────────────────
if have pactl; then
    timeout "$PULSE_TIMEOUT_SEC" pactl info </dev/null >"${OUT_DIR}/pactl-info.txt" 2>&1
    dump_lines "PACTL_INFO" "${OUT_DIR}/pactl-info.txt"
    timeout "$PULSE_TIMEOUT_SEC" pactl list short sources </dev/null >"${OUT_DIR}/pactl-sources.txt" 2>&1
    dump_lines "PACTL_SOURCES" "${OUT_DIR}/pactl-sources.txt"
    timeout "$PULSE_TIMEOUT_SEC" pactl list short sinks </dev/null >"${OUT_DIR}/pactl-sinks.txt" 2>&1
    dump_lines "PACTL_SINKS" "${OUT_DIR}/pactl-sinks.txt"
else
    for f in pactl-info pactl-sources pactl-sinks; do printf '(pactl absent)\n' >"${OUT_DIR}/${f}.txt"; done
    section "PACTL"
    log "(pactl absent on this node)"
fi

# ── 5. Permissions ────────────────────────────────────────────────────────────
section "PERMISSIONS"
if [ -d /dev/snd ]; then
    ls -l /dev/snd >"${OUT_DIR}/dev-snd.txt" 2>&1
    cat "${OUT_DIR}/dev-snd.txt"
else
    printf '(no /dev/snd on this node)\n' >"${OUT_DIR}/dev-snd.txt"
    log "(no /dev/snd on this node)"
fi
log "groups=$(id -Gn 2>/dev/null)"
case " $(id -Gn 2>/dev/null) " in
    *" audio "*) log "audio_group=yes" ;;
    *) log "audio_group=no (capture depends on ACLs — see dev-snd.txt)" ;;
esac

# Per-node access check on every /dev/snd node. `test -r/-w` goes through
# access(2), so it honours the POSIX ACLs the desktop session installs — this
# separates "device absent" from "this user is not allowed to open it", which
# is exactly the distinction that matters when /proc/asound advertises a
# capture stream that no client can actually open.
# node_mode_marks prints the ACL grants that decide whether THIS uid may open the
# node — the fact a bare mode string hides. /dev/snd is 660 root:audio with an
# ACL for the display manager on zpeer, which is why a uid outside group `audio`
# is refused the card and ALSA then says there is no sound card. The ACL lines
# are the same for every node in a directory, so a missing getfacl is recorded,
# not fatal.
node_mode_marks() {      # node_mode_marks <path>
    local path="$1" acl
    have "$GETFACL_BINARY" || { printf 'acl=(no %s)' "$GETFACL_BINARY"; return 0; }
    acl="$(timeout "$CMD_TIMEOUT_SEC" "$GETFACL_BINARY" -p "$path" 2>/dev/null |
        sed -n 's/^\(user:[^:]*:...\)$/\1/p' | grep -v '^user::' | paste -sd, -)"
    if [ -n "$acl" ]; then printf 'acl=%s' "$acl"; else printf 'acl=none'; fi
}

section "DEVICE_NODE_ACCESS"
DEV_NODE_TABLE="${OUT_DIR}/dev-node-access.tsv"
printf '# path\taccess\tmode_owner\tmarks\n' >"$DEV_NODE_TABLE"
denied_nodes=0
if [ -d /dev/snd ]; then
    for node in /dev/snd/*; do
        [ -e "$node" ] || continue
        if [ -r "$node" ] && [ -w "$node" ]; then access="rw"
        elif [ -r "$node" ]; then access="r"
        else access="DENIED"; denied_nodes=$((denied_nodes + 1)); fi
        printf '%s\t%s\t%s\t%s\n' "$node" "$access" "$(stat -c '%A %U:%G' "$node" 2>/dev/null)" \
            "$(node_mode_marks "$node")" >>"$DEV_NODE_TABLE"
    done
    cat "$DEV_NODE_TABLE"
    log "capture_nodes=$(ls /dev/snd/pcmC*c 2>/dev/null | wc -l) denied_nodes=${denied_nodes}"
else
    printf '(no /dev/snd)\n' >>"$DEV_NODE_TABLE"
    log "capture_nodes=0 denied_nodes=0"
fi

# ── 6. Capture-capable device enumeration ─────────────────────────────────────
# A card is capture-capable when /proc/asound/pcm lists a 'capture' stream for it.
declare -a CAPTURE_CARDS=()
if [ -r /proc/asound/pcm ]; then
    while read -r card; do
        [ -n "$card" ] || continue
        CAPTURE_CARDS+=("$card")
    done < <(grep -i 'capture' /proc/asound/pcm 2>/dev/null | cut -d: -f1 | cut -d- -f1 | sort -u)
    # The pcm file numbers cards without the 'card' prefix; the cards file has it.
fi

# Fallback: when /proc/asound/pcm yields nothing for this uid (or is absent),
# the privileged `arecord -l` still names the cards — without this, an
# access-denied uid would probe NO card-derived device at all, which is how a
# node with hardware ends up looking empty.
if [ "${#CAPTURE_CARDS[@]}" -eq 0 ] && [ -s "${OUT_DIR}/arecord-l-privileged.txt" ]; then
    while read -r card; do
        [ -n "$card" ] || continue
        CAPTURE_CARDS+=("$card")
    done < <(awk -v p="$ARECORD_LIST_ROW_PREFIX" 'index($0,p)==1 {print $2}' \
        "${OUT_DIR}/arecord-l-privileged.txt" | tr -d ':' | sort -u)
    if [ "${#CAPTURE_CARDS[@]}" -gt 0 ]; then
        log "capture cards taken from the PRIVILEGED arecord -l (unprivileged enumeration found none)"
    fi
fi

section "CAPTURE_CAPABLE_CARDS"
if [ "${#CAPTURE_CARDS[@]}" -eq 0 ]; then
    log "(none — no PCM in /proc/asound/pcm advertises a capture stream)"
else
    for c in "${CAPTURE_CARDS[@]}"; do
        log "card ${c} (from /proc/asound/pcm)"
    done
fi

# Candidate device strings: card-derived patterns first, then global aliases.
declare -a CANDIDATE_DEVICES=()
for c in "${CAPTURE_CARDS[@]}"; do
    for pat in "${CARD_DEVICE_PATTERNS[@]}"; do
        # shellcheck disable=SC2059 # pat is a printf format by design
        CANDIDATE_DEVICES+=("$(printf "$pat" "$c")")
    done
done
for d in "${GLOBAL_DEVICES[@]}"; do CANDIDATE_DEVICES+=("$d"); done

# Card-ID forms: ALSA's stable spelling, independent of the card index that
# moves when cards are added/removed. Extracted from /proc/asound/cards
# (`N [ID ]: driver - name`) for every capture-capable card.
for c in "${CAPTURE_CARDS[@]}"; do
    line="$(awk -v n="$c" '$1==n {print}' /proc/asound/cards 2>/dev/null)"
    card_id="$(printf '%s' "$line" | sed -n 's/.*\[\(.*\)\].*/\1/p' | tr -d ' ')"
    [ -n "$card_id" ] || continue
    CANDIDATE_DEVICES+=("$(printf "$CARD_ID_PLUGHW_FMT" "$card_id")")
    CANDIDATE_DEVICES+=("$(printf "$CARD_ID_FRONT_FMT" "$card_id")")
    CANDIDATE_DEVICES+=("$(printf "$CARD_ID_SYSDEFAULT_FMT" "$card_id")")
done

section "CANDIDATE_DEVICE_STRINGS"
for d in "${CANDIDATE_DEVICES[@]}"; do log "$d"; done

# ── 7. hw params + capture probes (BOTH privilege levels) ─────────────────────
section "HW_PARAMS"
if have "$ARECORD_BINARY" && [ "${#CANDIDATE_DEVICES[@]}" -gt 0 ]; then
    for d in "${CANDIDATE_DEVICES[@]}"; do
        f="${OUT_DIR}/hwparams-$(printf '%s' "$d" | tr -c 'A-Za-z0-9_.-' '_').txt"
        timeout "$HW_PARAM_TIMEOUT_SEC" "$ARECORD_BINARY" -D "$d" --dump-hw-params \
            -f "$SAMPLE_FORMAT" -r "$DEFAULT_RATE" -c "$DEFAULT_CHANNELS" /dev/null \
            </dev/null >"$f" 2>&1
        log "--- ${d} (rc=$?)"
        sed -n '1,12p' "$f" 2>/dev/null
    done
    if [ "$SUDO_STATE" = "$SUDO_AVAILABLE_YES" ]; then
        for d in "${CANDIDATE_DEVICES[@]}"; do
            f="${OUT_DIR}/hwparams-$(printf '%s' "$d" | tr -c 'A-Za-z0-9_.-' '_')-privileged.txt"
            run_sudo "$HW_PARAM_TIMEOUT_SEC" "$ARECORD_BINARY" -D "$d" --dump-hw-params \
                -f "$SAMPLE_FORMAT" -r "$DEFAULT_RATE" -c "$DEFAULT_CHANNELS" /dev/null \
                >"$f" 2>&1
            log "--- ${d} (as root, rc=$?)"
            sed -n '1,12p' "$f" 2>/dev/null
        done
    fi
fi

section "CAPTURE_PROBES (${PROBE_DURATION_SEC}s each, ${TRIALS} trial(s) per cell, mode=user then mode=root)"
if have "$ARECORD_BINARY"; then
    for d in "${CANDIDATE_DEVICES[@]}"; do
        for t in $(seq 1 "$TRIALS"); do
            probe_cell_both_modes "$d" "$DEFAULT_RATE" "$DEFAULT_CHANNELS" "$t"
            probe_cell_both_modes "$d" "$NATIVE_RATE" "$NATIVE_CHANNELS" "$t"
        done
    done
else
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
        "$NODE_NAME" "(none)" "-" "-" "-" "0" "127" "0" "$OUTCOME_ERROR" "arecord absent" "-" \
        >>"$TABLE_ROWS_FILE"
fi

# ── 8. Machine-readable table ─────────────────────────────────────────────────
section "$TABLE_BEGIN"
printf '# node\tdevice\trate\tchannels\tformat\ttrial\trc\tdata_bytes\toutcome\tstderr_first_line\tmode\n'
sort -u "$TABLE_ROWS_FILE" 2>/dev/null || cat "$TABLE_ROWS_FILE"
section "$TABLE_END"

# Per-device verdict. OK = captured as the calling user; SUDO_REQUIRED = captured
# only privileged (an access-control fact, NOT an absent device); UNUSABLE = no
# trial at that node captured. `best_mode` names which level worked, so the
# verdict line alone answers "is this hardware there, and who may use it?".
section "DEVICE_VERDICT (OK = captured as the user; SUDO_REQUIRED = only as root)"
printf '%-42s %-14s %s\n' "DEVICE" "VERDICT" "DETAIL"
while read -r dev; do
    [ -n "$dev" ] || continue
    verdict="$(awk -F'\t' -v d="$dev" \
        '$2==d && $9=="OK" {ok=1} $2==d && $9=="SUDO_REQUIRED" {sr=1}
         END{if(ok) print "OK"; else if(sr) print "SUDO_REQUIRED"; else print "UNUSABLE"}' \
        "$TABLE_ROWS_FILE")"
    best="$(awk -F'\t' -v d="$dev" '$2==d && $9=="OK" {print $11}' "$TABLE_ROWS_FILE" | sort -u | paste -sd, -)"
    [ -n "$best" ] || best="-"
    detail="$(awk -F'\t' -v d="$dev" '$2==d {c[$9]++} END{for (k in c) printf "%s=%d ", k, c[k]}' "$TABLE_ROWS_FILE")"
    printf '%-42s %-14s mode=%s %s\n' "$dev" "$verdict" "$best" "$detail"
done < <(awk -F'\t' 'NR>0 && $2!="" {print $2}' "$TABLE_ROWS_FILE" | sort -u)

# The node's capture truth — the line a reader must not have to derive from 60
# table rows, and the one the earlier revision got wrong for zpeer.
section "CAPTURE_TRUTH"
best_user="$(awk -F'\t' '$9=="OK" && $11=="'"$PRIV_MODE_USER"'" {n++} END{print n+0}' "$TABLE_ROWS_FILE")"
best_root="$(awk -F'\t' '$9=="OK" && $11=="'"$PRIV_MODE_ROOT"'" {n++} END{print n+0}' "$TABLE_ROWS_FILE")"
sudo_required="$(awk -F'\t' '$9=="'"$OUTCOME_SUDO_REQUIRED"'" {n++} END{print n+0}' "$TABLE_ROWS_FILE")"
cards_listed="$(grep -c "^$ARECORD_LIST_ROW_PREFIX" "${OUT_DIR}/arecord-l.txt" 2>/dev/null || true)"
cards_listed_priv="$(grep -c "^$ARECORD_LIST_ROW_PREFIX" "${OUT_DIR}/arecord-l-privileged.txt" 2>/dev/null || true)"
capture_cards="${#CAPTURE_CARDS[@]}"
log "cards_visible_to_user=${cards_listed:-0}"
log "cards_visible_to_root=${cards_listed_priv:-0} sudo_available=${SUDO_STATE}"
log "capture_streams_in_proc_asound=${capture_cards}"
log "ok_cells_user=${best_user} ok_cells_root=${best_root} sudo_required_cells=${sudo_required}"
if [ "$best_user" -gt 0 ]; then
    log "verdict=CAPTURE_OK (the calling user can record)"
elif [ "$best_root" -gt 0 ]; then
    log "verdict=CAPTURE_SUDO_REQUIRED (hardware present and recording; the user is refused by access control — rerun with sudo or add the user to group audio)"
elif [ "$SUDO_STATE" != "$SUDO_AVAILABLE_YES" ] && [ "${capture_cards:-0}" -gt 0 ]; then
    log "verdict=UNMEASURED (no passwordless sudo: a privilege-gated capture path cannot be ruled out)"
elif [ "${capture_cards:-0}" -eq 0 ] && [ "${cards_listed_priv:-0}" -eq 0 ]; then
    log "verdict=NO_CAPTURE_HARDWARE (no capture stream anywhere, at either privilege level)"
else
    log "verdict=NO_USABLE_DEVICE (hardware listed but no probed device string captured)"
fi

log ""
log "raw artifacts: ${OUT_DIR}/"
exit 0
