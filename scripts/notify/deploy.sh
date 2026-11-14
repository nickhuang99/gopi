#!/usr/bin/env bash
# deploy.sh — install the task-event sink into a gopi profile, ONCE per machine.
#
# WHAT THIS REPLACES. Bringing up a second machine used to mean hand-copying
# scripts/notify/notify.sh into ~/.gopi/notify/ and hand-editing the copy: the
# file is ~40 KB and its peer/cursor/state names lived as literals inside it, so
# every host carried its own drifting fork and no two hosts could be compared.
# Now the file is deployed BYTE-FOR-BYTE as it lives in the repo (it reads its
# per-host values from the environment — see THE OPERATOR PARAMETER BLOCK in
# notify.sh), and the per-host facts are written into ONE line of settings.json
# (`notify.cmd`) plus ONE settings array (`sync_peers`). An upgrade is: pull,
# re-run this command.
#
# WHAT IT DOES, and every step is IDEMPOTENT — a second run changes nothing:
#
#   1. install <profile>/notify/notify.sh from --source (byte compare first; a
#      file whose content already matches is left alone, mtime included);
#   2. ensure each --peer appears in the profile's settings.json "sync_peers"
#      (the ONE whitelist notify.sh reads; duplicates are not added twice);
#   3. ensure settings.json "notify.cmd" runs that script. A cmd that is EMPTY
#      is filled in; a cmd that already names THIS script is left alone; a cmd
#      that names something ELSE is NEVER overwritten — it is printed, so the
#      operator decides (this script does not own the profile);
#   4. print what a reader needs: where each thing landed, what the sink will
#      record, and how to check it.
#
# NO RESIDENT DEPENDENCY (GOPI.md Rule 35): this is a one-shot an operator runs.
# It schedules nothing, starts nothing, watches nothing. The sink it installs is
# still exec'd only by the existing notify delivery path.
#
# NO NEW gopi KNOB: nothing here is read by Go. The names it writes
# (GOPI_NOTIFY_STATE_DIR / _CURSOR_STALE_SEC / _PEER_LIMIT) are read by the sink
# script alone, and an unset one is the documented default.
#
# SAFETY. Every value that ends up in a command line or in the settings file is
# validated first: a peer alias against the ssh-alias whitelist, a directory
# against "absolute, no newline, no quote, no shell metacharacter". A refused
# value stops the run BY NAME — nothing is written half-way.
set -u

readonly SCRIPT_NAME="notify.sh"
readonly SINK_SUBDIR="notify"
readonly PROFILE_DIR_NAME=".gopi"
readonly SETTINGS_FILE_NAME="settings.json"
readonly SYNC_PEERS_KEY="sync_peers"
readonly NOTIFY_SECTION_KEY="notify"
readonly NOTIFY_CMD_KEY="cmd"
# The sink's own parameter names. They are spelled here ONCE because this file
# WRITES them into notify.cmd; notify.sh reads them from the environment and does
# not import this script (it must run with nothing but bash and python3).
readonly STATE_DIR_ENV="GOPI_NOTIFY_STATE_DIR"
readonly CURSOR_STALE_SEC_ENV="GOPI_NOTIFY_CURSOR_STALE_SEC"
readonly PEER_LIMIT_ENV="GOPI_NOTIFY_PEER_LIMIT"
# ssh alias shape: ssh_config accepts these characters and nothing else needs to.
readonly PEER_ALIAS_RE='^[A-Za-z0-9_][A-Za-z0-9._@-]*$'

usage() {
	cat <<'HELP_TEXT'
deploy.sh — install the task-event sink into a gopi profile (idempotent, one-shot)

USAGE
  bash scripts/notify/deploy.sh [options]

OPTIONS
  --profile DIR         the PROFILE ROOT to install into (…/\.gopi). Default:
                        $GOPI_HOME/.gopi when GOPI_HOME is set, else ~/.gopi
  --peer ALIAS          a peer to pull from; repeatable. Added to the profile's
                        settings.json "sync_peers" (never duplicated). Validated
                        against ^[A-Za-z0-9_][A-Za-z0-9._@-]*$
  --state-dir DIR       where the per-peer CURSORS and HEALTH records live
                        (GOPI_NOTIFY_STATE_DIR). Default: <profile>/notify, i.e.
                        where they have always lived. Absolute path.
  --cursor-stale SEC    how long a peer's cursor may stand still before the
                        health record calls it `stalled` (default 21600 = 6h)
  --peer-limit N        how many peers one run may read (default: the sink's own
                        bound, derived from the child's 30s deadline: 2)
  --source PATH         the notify.sh to install. Default: the copy next to this
                        script (i.e. the one in this checkout)
  --dry-run             print every planned action, write nothing, exit 0
  --print-cmd           print ONLY the notify.cmd line to put in settings.json
                        (for a machine whose cmd is owned by somebody else)
  -h, --help            this text

WHAT IS WRITTEN, and why each is safe to repeat
  <profile>/notify/notify.sh      byte-compared; rewritten only when it differs
  <profile>/settings.json         "sync_peers" gets each --peer once; "notify.cmd"
                                  is filled only when EMPTY or already ours, and
                                  any OTHER cmd is printed, never overwritten

EXIT
  0  installed (or already installed — the same thing)
  1  a validation or write failed; nothing was left half-written
  2  usage error

EXAMPLES
  # the common case: this machine's profile, pulling from one peer
  bash scripts/notify/deploy.sh --peer zpeer

  # a second/third machine, with its own state directory
  bash scripts/notify/deploy.sh --peer zpeer --state-dir ~/.gopi/notify-state

  # what would happen, without touching anything
  bash scripts/notify/deploy.sh --peer zpeer --dry-run

AFTER INSTALLING, check it with the sink's own instruments:
  gopi notify sink            # the resolved notify.cmd and what it appends to
  ls <state-dir>              # after one task event: peer-<alias>-health.txt
HELP_TEXT
}

die() {
	printf 'deploy.sh: %s\n' "$1" >&2
	exit "${2:-1}"
}

note() { printf '%s\n' "$1"; }
say() { printf '  %-8s %s\n' "$1" "$2"; }

# ---- argument parsing (no getopt: one shape, spelled out) -------------------
profile="${GOPI_HOME:+${GOPI_HOME}/${PROFILE_DIR_NAME}}"
[ -n "$profile" ] || profile="${HOME}/${PROFILE_DIR_NAME}"
state_dir=""
cursor_stale=""
peer_limit=""
source_path="$(cd "$(dirname "$0")" && pwd)/${SCRIPT_NAME}"
dry_run=0
print_cmd_only=0
peers=""

while [ $# -gt 0 ]; do
	case "$1" in
	-h | --help)
		usage
		exit 0
		;;
	--profile)
		[ $# -ge 2 ] || die "--profile needs a directory" 2
		profile="$2"
		shift 2
		;;
	--peer)
		[ $# -ge 2 ] || die "--peer needs an alias" 2
		peers="${peers}${2}
"
		shift 2
		;;
	--state-dir)
		[ $# -ge 2 ] || die "--state-dir needs a directory" 2
		state_dir="$2"
		shift 2
		;;
	--cursor-stale)
		[ $# -ge 2 ] || die "--cursor-stale needs a number of seconds" 2
		cursor_stale="$2"
		shift 2
		;;
	--peer-limit)
		[ $# -ge 2 ] || die "--peer-limit needs a number" 2
		peer_limit="$2"
		shift 2
		;;
	--source)
		[ $# -ge 2 ] || die "--source needs a path" 2
		source_path="$2"
		shift 2
		;;
	--dry-run)
		dry_run=1
		shift
		;;
	--print-cmd)
		print_cmd_only=1
		shift
		;;
	*)
		die "unknown argument: $1 (try --help)" 2
		;;
	esac
done

# ---- validation: NOTHING is written until every value has passed ------------
# An absolute path with no newline, no quote and no shell metacharacter. The
# directory is only ever used as a filesystem path by this script, but the SAME
# value is written into notify.cmd (a `sh -c` line the parent executes), so the
# refusal is about the command line it becomes, not about mkdir.
validate_dir() {
	case "$2" in
	'') die "$1 is empty" ;;
	/*) ;;
	*) die "$1 must be an absolute path (got $2)" ;;
	esac
	case "$2" in
	*'
'*) die "$1 must not contain a newline" ;;
	*) ;;
	esac
	case "$2" in
	*\"* | *\'* | *\`* | *\$* | *\;* | *\&* | *\|* | *' '* | *'<'* | *'>'* | *'('* | *')'* | *'*'* | *'?'* | *'['* | *']'*)
		die "$1 contains a character that cannot be written into notify.cmd safely: $2"
		;;
	*) ;;
	esac
}

validate_uint() {
	case "$2" in
	'') die "$1 is empty" ;;
	*[!0-9]*) die "$1 must be a positive integer (got $2)" ;;
	esac
	[ "$2" -gt 0 ] || die "$1 must be greater than zero (got $2)"
}

validate_dir "--profile" "$profile"
[ -n "$state_dir" ] && validate_dir "--state-dir" "$state_dir"
[ -n "$cursor_stale" ] && validate_uint "--cursor-stale" "$cursor_stale"
[ -n "$peer_limit" ] && validate_uint "--peer-limit" "$peer_limit"

_peer_list=""
while IFS= read -r _p; do
	[ -n "$_p" ] || continue
	printf '%s' "$_p" | grep -Eq "$PEER_ALIAS_RE" ||
		die "--peer $_p is not an ssh alias (allowed: ^[A-Za-z0-9_][A-Za-z0-9._@-]*\$)"
	_peer_list="${_peer_list}${_p}
"
done <<EOF
$peers
EOF
peers="$_peer_list"

[ -r "$source_path" ] || die "no readable sink to install at $source_path (--source)"
[ -d "$profile" ] || [ "$dry_run" -eq 1 ] ||
	die "the profile directory $profile does not exist — refusing to invent a profile (create it, or point --profile at one)"
command -v python3 >/dev/null 2>&1 ||
	die "python3 is required (settings.json is JSON, not a line format)"

sink_dir="${profile}/${SINK_SUBDIR}"
sink_path="${sink_dir}/${SCRIPT_NAME}"
settings_path="${profile}/${SETTINGS_FILE_NAME}"

# THE cmd LINE. An env prefix is spelled only when the deployment needs it, so a
# host with every default keeps the plain documented form
# (`bash ~/.gopi/notify/notify.sh`, CONFIG.md §2e).
build_cmd() {
	_cmd=""
	[ -z "$state_dir" ] || _cmd="${_cmd}${STATE_DIR_ENV}=${state_dir} "
	[ -z "$cursor_stale" ] || _cmd="${_cmd}${CURSOR_STALE_SEC_ENV}=${cursor_stale} "
	[ -z "$peer_limit" ] || _cmd="${_cmd}${PEER_LIMIT_ENV}=${peer_limit} "
	case "$_cmd" in
	'') _cmd="bash ${sink_path}" ;;
	*) _cmd="${_cmd}bash ${sink_path}" ;;
	esac
	printf '%s' "$_cmd"
}
new_cmd="$(build_cmd)"

if [ "$print_cmd_only" -eq 1 ]; then
	printf '%s\n' "$new_cmd"
	exit 0
fi

# ---- 1. install the sink (byte compare, so a no-op run touches nothing) -----
if [ "$dry_run" -eq 1 ]; then
	say "plan" "install $source_path -> $sink_path"
else
	if ! mkdir -p "$sink_dir"; then
		die "cannot create $sink_dir"
	fi
fi

installed="unchanged"
if [ -f "$sink_path" ] && cmp -s "$source_path" "$sink_path"; then
	say "sink" "$sink_path already current (byte-identical to $source_path)"
else
	installed="written"
	if [ "$dry_run" -eq 1 ]; then
		say "sink" "would write $sink_path from $source_path"
	else
		# Copy through a sibling and MOVE it into place: a run that dies mid-copy
		# must not leave a truncated script where the profile exec's one.
		_tmp="${sink_dir}/.${SCRIPT_NAME}.deploy.$$"
		if ! cp "$source_path" "$_tmp" || ! chmod 0755 "$_tmp" || ! mv -f "$_tmp" "$sink_path"; then
			rm -f "$_tmp" 2>/dev/null
			die "cannot install $sink_path"
		fi
		say "sink" "installed $sink_path (mode 0755) from $source_path"
	fi
fi

# ---- 2+3. the settings: peers appended once, cmd filled only when free ------
# ONE python3 pass does the read, the merge and the write-if-changed, so the file
# is never rewritten for a run that changed nothing (an mtime bump on somebody
# else's settings file is a change nobody asked for).
new_peers="$peers"
if [ "$dry_run" -eq 1 ]; then
	_settings_report="$(
		NEW_CMD="$new_cmd" NEW_PEERS="$new_peers" SYNC_KEY="$SYNC_PEERS_KEY" \
			NOTIFY_KEY="$NOTIFY_SECTION_KEY" CMD_KEY="$NOTIFY_CMD_KEY" \
			python3 - "$settings_path" <<'PY'
import json, os, sys
path = sys.argv[1]
sync_key = os.environ["SYNC_KEY"]; notify_key = os.environ["NOTIFY_KEY"]; cmd_key = os.environ["CMD_KEY"]
new_cmd = os.environ["NEW_CMD"]
wanted = [p for p in os.environ["NEW_PEERS"].split("\n") if p]
try:
    with open(path) as fh:
        cfg = json.load(fh)
except FileNotFoundError:
    cfg = {}
except Exception as exc:
    print("REFUSED parse: %s" % exc)
    raise SystemExit(0)
cur = cfg.get(sync_key) or []
if not isinstance(cur, list):
    print("REFUSED %s is not a list" % sync_key)
    raise SystemExit(0)
added = [p for p in wanted if p not in cur]
nsec = cfg.get(notify_key)
cur_cmd = (nsec or {}).get(cmd_key, "") if isinstance(nsec, dict) else ""
if added:
    print("peers add: %s" % " ".join(added))
else:
    print("peers unchanged: %d declared" % len(cur))
if not cur_cmd:
    print("cmd write: %s" % new_cmd)
elif cur_cmd == new_cmd:
    print("cmd unchanged: %s" % cur_cmd)
else:
    print("cmd CONFLICT (left alone): %s" % cur_cmd)
    print("cmd needed (put this in settings.json yourself): %s" % new_cmd)
PY
	)"
else
	_settings_report="$(
		NEW_CMD="$new_cmd" NEW_PEERS="$new_peers" SYNC_KEY="$SYNC_PEERS_KEY" \
			NOTIFY_KEY="$NOTIFY_SECTION_KEY" CMD_KEY="$NOTIFY_CMD_KEY" \
			python3 - "$settings_path" <<'PY'
import json, os, sys
path = sys.argv[1]
sync_key = os.environ["SYNC_KEY"]; notify_key = os.environ["NOTIFY_KEY"]; cmd_key = os.environ["CMD_KEY"]
new_cmd = os.environ["NEW_CMD"]
wanted = [p for p in os.environ["NEW_PEERS"].split("\n") if p]
try:
    with open(path) as fh:
        raw = fh.read()
    cfg = json.loads(raw) if raw.strip() else {}
except FileNotFoundError:
    cfg = {}
    raw = None
except Exception as exc:
    print("REFUSED parse: %s" % exc)
    raise SystemExit(0)
if not isinstance(cfg, dict):
    print("REFUSED settings.json is not a JSON object")
    raise SystemExit(0)
cur = cfg.get(sync_key) or []
if not isinstance(cur, list):
    print("REFUSED %s is not a list" % sync_key)
    raise SystemExit(0)
added = [p for p in wanted if p not in cur]
nsec = cfg.get(notify_key)
if nsec is None:
    nsec = {}
if not isinstance(nsec, dict):
    print("REFUSED %s is not an object" % notify_key)
    raise SystemExit(0)
cur_cmd = nsec.get(cmd_key, "")
changed = False
if added:
    cfg[sync_key] = list(cur) + added
    changed = True
if not cur_cmd:
    nsec[cmd_key] = new_cmd
    cfg[notify_key] = nsec
    changed = True
if changed:
    out = json.dumps(cfg, indent=2, ensure_ascii=False) + "\n"
    tmp = path + ".deploy.%d" % os.getpid()
    try:
        # A settings file that is a symlink is a READ-ONLY contract in this
        # project (a sandbox's settings is a link to the real one; POSIX
        # rename(2) would REPLACE the link node and silently detach the
        # profile). So the write happens through os.replace ONLY on a real file,
        # and a symlink or a permission refusal is REPORTED by name.
        if os.path.islink(path):
            print("UNWRITABLE %s: it is a symlink (a read-only contract here) — edit its target instead" % path)
            raise SystemExit(0)
        with open(tmp, "w") as fh:
            fh.write(out)
        os.replace(tmp, path)
    except OSError as exc:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        print("UNWRITABLE %s: %s" % (path, exc))
        raise SystemExit(0)
    print("peers %s: %s" % ("added" if added else "unchanged", " ".join(added) if added else "%d declared" % len(cur)))
    print("cmd %s: %s" % ("written" if not cur_cmd else "unchanged", new_cmd))
else:
    print("peers unchanged: %d declared" % len(cur))
    if cur_cmd == new_cmd:
        print("cmd unchanged: %s" % cur_cmd)
    else:
        print("cmd CONFLICT (left alone): %s" % cur_cmd)
        print("cmd needed (put this in settings.json yourself): %s" % new_cmd)
PY
	)"
	case "$_settings_report" in
	REFUSED*)
		die "the settings file $settings_path was REFUSED, not edited: ${_settings_report#REFUSED }"
		;;
	UNWRITABLE*)
		# NOT a silent skip and not a hard failure of the install: the SINK is in
		# place, and the one line the profile still needs is known exactly. Say
		# both, and exit non-zero because the deployment is incomplete.
		_wu="${_settings_report#UNWRITABLE }"
		printf 'deploy.sh: the sink is installed but the profile settings could not be written: %s\n' "$_wu" >&2
		printf 'deploy.sh: put this line in %s yourself:\n' "$settings_path" >&2
		printf '  "notify": { "cmd": "%s" }\n' "$new_cmd" >&2
		[ -z "$new_peers" ] || printf 'deploy.sh: and this peer list: "sync_peers": [%s]\n' \
			"$(printf '%s' "$new_peers" | tr '\n' ',' | sed 's/,$//' >/dev/null 2>&1; printf '%s' "$new_peers" | sed 's/^/"/;s/$/"/' | tr '\n' ',' | sed 's/,$//')" >&2
		exit 1
		;;
	esac
fi

printf '\n'
printf 'deploy.sh: profile %s\n' "$profile"
printf '%s\n' "$_settings_report" | sed 's/^/  /'
printf '\n'
note "next:"
note "  1) one task event must produce a delivery:  gopi task submit 'deploy probe'  (or wait for the next transition)"
note "  2) check what the profile resolves:          gopi notify sink"
note "  3) read the pull's own state (one file per peer):"
if [ -n "$state_dir" ]; then
	note "       ls $state_dir"
else
	note "       ls ${sink_dir}"
fi
note "     a peer file is  peer-<alias>-health.txt:  state=ok|stalled|unreachable|not-read|refused-cursor"
note "     not one file per failure — a repeat of the same failure rewrites nothing."
if [ "$dry_run" -eq 1 ]; then
	printf '\n'
	note "deploy.sh: DRY RUN — nothing was written"
fi
exit 0
