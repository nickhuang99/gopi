#!/usr/bin/env bash
# gopi-failed-audit.sh — READ-ONLY audit of recent FAILED rows in the gopi task
# ledger. Answers two questions per row:
#
#   1. was the underlying problem solved elsewhere (another row, another branch,
#      a later merge into master)?
#   2. if not — is it a live problem nobody is dispatched to?
#
# WHY A SCRIPT: the operator's standing request ("以后我们要学习类似的脚本解决反复
# 的任务"); a count typed into prose is stale on the next commit (Rule 38 — a
# census figure names its generator and the commit it was measured at).
#
# CONTRACT — READ-ONLY, in the strongest form available:
#   * the ledger is opened `sqlite3 -readonly` and every statement is a SELECT;
#     this script cannot write a row even by accident.
#   * git is read-only: rev-parse / merge-base / rev-list / ls-tree / diff /
#     show / grep. No checkout, no fetch, no branch or tag is created or
#     deleted (GOPI.md Rule 29: a foreign worktree is never touched).
#   * no file is written anywhere; stdout only, so there is no /tmp and no
#     scratch directory to leak (Rule 22).
#
# EVIDENCE HIERARCHY (the briefing's, strongest first):
#   1. per-file blob equality against master — `git rev-parse <tip>:<path>` vs
#      `git rev-parse master:<path>`. Commit subjects lie (they get rewritten at
#      merge); blob hashes do not.
#   2. per-file existence — `git cat-file -e master:<path>`.
#   3. the successor query on the ledger — append-only: a re-issue is a NEW row
#      whose payload/summary carries the old row's FULL id.
#   4. `git log --grep` — WEAKEST. It is deliberately NOT used here: measured on
#      this repo it produces false positives (a fetched-but-unmerged object, a
#      subject carrying the word by accident) and an earlier audit was abandoned
#      for exactly that.
#
# WHAT THIS SCRIPT CANNOT DETERMINE — it says so rather than guessing:
#   * whether a row's *intent* is satisfied by master. It reports the raw
#     per-file facts; only a reader (or the audit report) classifies.
#   * whether the git repo it inspects is the repo the row meant. `--repo` names
#     it and the script prints which one it used.
#   * whether a successor row actually RAN. It prints the successor's status.
#   * whether an object still exists after `gc` — an absent tip object is
#     printed as ABSENT-OBJECT, and it is NOT evidence that the work landed.
#
# USAGE:
#   scripts/gopi-failed-audit.sh                 # 7-day window (the default)
#   scripts/gopi-failed-audit.sh --days 3
#   scripts/gopi-failed-audit.sh --repo /home/nick/work/GoPi --db ~/.gopi/memory.db
#
# Rule 1: every literal that carries meaning is a named constant below.
# Rule 27: static inspection only — this script generates no device load.
set -uo pipefail
export LC_ALL=C  # the counts are machine-read; a byte-oriented locale is stable

# --- named constants (Rule 1) -------------------------------------------------
readonly LEDGER_DB_DEFAULT="$HOME/.gopi/memory.db"
readonly WINDOW_DAYS_DEFAULT=7
readonly WINDOW_DAYS_NARROW=3
readonly STATUS_FAILED='failed'
readonly SOURCE_HUMAN='human'
readonly MASTER_REF='master'
readonly SECONDS_PER_DAY=86400
readonly SQL_BACKEND="sqlite3 -readonly"
readonly GIT_BIN='git'
# The branch shapes a rework row names in its summary/payload. Deliberately not
# exhaustive: an unrecognised shape is printed as "(no branch token)" and the
# row is left for the reader, rather than guessed at.
# The trailing character class excludes '.': prose ends a branch name with a
# sentence period ("…d59f60e4 could not compose"), and a token that swallowed it
# would resolve to no ref and print a false NO-REF-ANYWHERE.
readonly BRANCH_TOKEN_RE='(task|fix|feat|chore|rework|salvage)/[A-Za-z0-9._/-]*[A-Za-z0-9_/-]'
readonly BRANCH_JSON_RE='"branch":"[^"]+"'
readonly SUMMARY_WIDTH=58
readonly MAX_PATHS_PER_TIP=14
readonly MAX_PAYLOAD_TIPS=12
# A row's payload usually names its files; this caps how many are queried so the
# section stays a measurement rather than a scan of the whole tree.
readonly MAX_PATHS_PER_ROW=3
readonly CANNOT_DETERMINE='cannot determine'

# --- argument parsing ---------------------------------------------------------
DB="$LEDGER_DB_DEFAULT"
DAYS="$WINDOW_DAYS_DEFAULT"
REPO=""
while [ $# -gt 0 ]; do
  case "$1" in
    --db)    DB="${2:-}"; shift 2 ;;
    --days)  DAYS="${2:-}"; shift 2 ;;
    --repo)  REPO="${2:-}"; shift 2 ;;
    -h|--help) sed -n '2,50p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
if [ -z "$REPO" ]; then
  REPO="$($GIT_BIN -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null)"
fi
readonly DB DAYS REPO

if [ ! -r "$DB" ]; then
  echo "FATAL: ledger not readable: $DB" >&2
  exit 1
fi
if [ -z "$REPO" ] || [ ! -d "$REPO/.git" ] && ! $GIT_BIN -C "$REPO" rev-parse --git-dir >/dev/null 2>&1; then
  echo "FATAL: not a git repo: '$REPO' (pass --repo PATH)" >&2
  exit 1
fi

# --- helpers ------------------------------------------------------------------
# q <sql> — one read-only query, tab-separated, no header.
q() { $SQL_BACKEND "$DB" -noheader -separator '|' "$1"; }

# run_git <args...> — read-only git against the audited repo.
run_git() { $GIT_BIN -C "$REPO" "$@"; }

hdr() { printf '\n=== %s ===\n' "$1"; }

master_tip() { run_git rev-parse --short "$MASTER_REF" 2>/dev/null || echo "$CANNOT_DETERMINE"; }

# ref_state <branch> — where the branch lives now, if anywhere, and whether its
# tip is an ancestor of master. NONE = the name resolves to no ref at all.
ref_state() {
  local b="$1" t
  t="$(run_git rev-parse --verify --quiet "refs/heads/$b")"
  [ -z "$t" ] && t="$(run_git rev-parse --verify --quiet "refs/tags/$b")"
  [ -z "$t" ] && t="$(run_git rev-parse --verify --quiet "refs/tags/rework-backup/$b")"
  if [ -z "$t" ]; then echo 'NO-REF-ANYWHERE'; return; fi
  if run_git merge-base --is-ancestor "$t" "$MASTER_REF" 2>/dev/null; then
    echo "ANCESTOR-OF-MASTER@${t:0:8}"
  else
    echo "AHEAD=$(run_git rev-list --count "$MASTER_REF..$t" 2>/dev/null)@${t:0:8}"
  fi
}

# branch_tokens <row-json-deliverable> <summary-or-payload>
# Prefers the branch the ROW RECORDED in its deliverable; falls back to the
# branch names its prose carries. Prints one per line, deduped.
branch_tokens() {
  { printf '%s\n' "$1" | grep -oE "$BRANCH_JSON_RE" | sed 's/^"branch":"//; s/"$//'
    printf '%s\n' "$2" | grep -oE "$BRANCH_TOKEN_RE"
  } | grep -v '^$' | sort -u
}

# --- header: what was measured, and at which commit (Rule 38) -----------------
NOW="$(date +%s)"
SINCE=$((NOW - DAYS * SECONDS_PER_DAY))

hdr "gopi failed-row audit — READ-ONLY"
echo "ledger:      $DB"
echo "repo:        $REPO"
echo "master tip:  $(run_git rev-parse --short "$MASTER_REF" 2>/dev/null || echo "$CANNOT_DETERMINE") ($(run_git log -1 --format=%cd --date=short "$MASTER_REF" 2>/dev/null || echo "$CANNOT_DETERMINE"))"
echo "window:      $DAYS days (cutoff $SINCE = $(date -d "@$SINCE" '+%Y-%m-%d %H:%M' 2>/dev/null || echo "$CANNOT_DETERMINE"))"
echo "worktree:    $($GIT_BIN -C "$REPO" status --porcelain | wc -l) dirty entries (0 = the figures correspond to the printed commit)"
echo "NOTE: ledger counts drift while it is live; the numbers below are this instant's."

# --- why the window is what it is: the narrow window, measured ---------------
hdr "window justification — the narrow ${WINDOW_DAYS_NARROW}-day window, per source"
q "select source||'='||count(*) from tasks where status='$STATUS_FAILED' and created_at > $NOW - $WINDOW_DAYS_NARROW*$SECONDS_PER_DAY group by source order by source;"
HUMAN_NARROW="$(q "select count(*) from tasks where status='$STATUS_FAILED' and source='$SOURCE_HUMAN' and created_at > $NOW - $WINDOW_DAYS_NARROW*$SECONDS_PER_DAY;")"
echo "source=$SOURCE_HUMAN rows in the narrow window: $HUMAN_NARROW"
if [ "$HUMAN_NARROW" = "0" ]; then
  echo "=> ZERO $SOURCE_HUMAN rows in $WINDOW_DAYS_NARROW days, which is why the audit"
  echo "   window is widened to $DAYS days. The operator asked for 3; 3 returns no"
  echo "   operator-filed row at all, so it cannot answer his question."
fi

# --- per-source counts in the audit window -----------------------------------
hdr "window used — ${DAYS}-day failed rows, per source"
q "select source||'='||count(*) from tasks where status='$STATUS_FAILED' and created_at > $SINCE group by source order by count(*) desc;"
echo "TOTAL=$(q "select count(*) from tasks where status='$STATUS_FAILED' and created_at > $SINCE;")"
echo "(raw ledger baseline, all time: failed=$(( $(q "select count(*) from tasks where status='$STATUS_FAILED';") )) — the filtered CLI view hides rows carrying the 'resolved' tag; always quote --all or SQL)"

# --- the row table ------------------------------------------------------------
hdr "rows — $SOURCE_HUMAN first, then the rest (full task ids, never abbreviated)"
q "select task_id||'|'||source||'|'||date(created_at,'unixepoch','localtime')||'|'||coalesce(deliverable,'')||'|'||replace(summary,char(10),' ')||'|'||replace(coalesce(payload,''),char(10),' ')
   from tasks where status='$STATUS_FAILED' and created_at > $SINCE
   order by (source='$SOURCE_HUMAN') desc, created_at;" |
while IFS='|' read -r id src day deliv summary payload; do
  succ="$(q "select count(*) from tasks s where s.task_id<>'$id' and (s.payload like '%$id%' or s.summary like '%$id%');")"
  succids="$(q "select coalesce(group_concat(substr(s.task_id,1,36)||':'||s.status||'@'||date(s.created_at,'unixepoch','localtime'),' '),'NONE') from tasks s where s.task_id<>'$id' and (s.payload like '%$id%' or s.summary like '%$id%');")"
  printf '\n%-38s %-7s %s  successors=%s\n' "$id" "$src" "$day" "$succ"
  printf '   summary: %.60s\n' "$summary"
  printf '   successors: %s\n' "$succids"
  # The branch the row names, and where that branch stands now.
  tokens="$(branch_tokens "$deliv" "$payload $summary")"
  if [ -z "$tokens" ]; then
    printf '   branch: (no branch token) — classification must come from the payload\n'
  else
    printf '%s\n' "$tokens" | while read -r b; do
      printf '   branch %-52s %s\n' "$b" "$(ref_state "$b")"
    done
  fi
  # Offer each named branch tip for the blob comparison below.
  printf '%s\n' "$tokens" | while read -r b; do
    t="$(run_git rev-parse --verify --quiet "refs/heads/$b")"
    [ -z "$t" ] && t="$(run_git rev-parse --verify --quiet "refs/tags/rework-backup/$b")"
    [ -n "$t" ] && printf '%s\n' "${t:0:8}"
  done
done

# --- blob comparison: is the branch's content IN master, per file? -----------
# compare_tip <label> <rev> — per-file truth for one candidate tip.
# SAME-AS-MASTER for every changed path is the strongest landing evidence there
# is (commit subjects get rewritten at merge; blob hashes do not).
compare_tip() {
  local label="$1" rev="$2" tip base p a bb n
  tip="$(run_git rev-parse --verify --quiet "${rev}^{commit}" 2>/dev/null)"
  printf '\n--- %s  (tip %s)\n' "$label" "$rev"
  if [ -z "$tip" ]; then
    printf '    ABSENT-OBJECT: does not resolve to a commit here — NOT evidence that the work landed\n'
    return
  fi
  base="$(run_git merge-base "$MASTER_REF" "$tip" 2>/dev/null)"
  if [ -z "$base" ]; then printf '    %s: no merge base with %s\n' "$CANNOT_DETERMINE" "$MASTER_REF"; return; fi
  printf '    ahead-of-master=%s  (%s)\n' "$(run_git rev-list --count "$MASTER_REF..$tip")" "${tip:0:8}"
  n=0
  run_git diff --name-only "$base" "$tip" | while read -r p; do
    [ -z "$p" ] && continue
    a="$(run_git rev-parse --verify --quiet "$tip:$p" 2>/dev/null)"
    bb="$(run_git rev-parse --verify --quiet "$MASTER_REF:$p" 2>/dev/null)"
    if [ -z "$a" ] && [ -z "$bb" ]; then printf '    ABSENT-BOTH    %s\n' "$p"; continue; fi
    if [ -z "$bb" ]; then printf '    ONLY-ON-TIP    %s (absent from master)\n' "$p"; continue; fi
    if [ "$a" = "$bb" ]; then printf '    SAME-AS-MASTER %s\n' "$p"; else printf '    DIFFERS        %s  tip=%s master=%s\n' "$p" "${a:0:10}" "${bb:0:10}"; fi
  done
  printf '    (changed paths shown: up to %s; total %s)\n' "$MAX_PATHS_PER_TIP" "$(run_git diff --name-only "$base" "$tip" | head -"$MAX_PATHS_PER_TIP" | wc -l)"
}

hdr "blob comparison — per-file truth for every branch tip reachable from a row"
TIPS="$(
  q "select coalesce(deliverable,'')||' '||replace(coalesce(payload,''),char(10),' ')||' '||replace(summary,char(10),' ') from tasks where status='$STATUS_FAILED' and created_at > $SINCE;" |
  grep -oE "$BRANCH_TOKEN_RE" | sort -u |
  while read -r b; do
    t="$(run_git rev-parse --verify --quiet "refs/heads/$b")"
    [ -z "$t" ] && t="$(run_git rev-parse --verify --quiet "refs/tags/rework-backup/$b")"
    [ -n "$t" ] && echo "${t:0:8} $b"
  done
)"
if [ -z "$TIPS" ]; then
  echo "no branch named by a failed row still resolves to a REF in this repo."
  echo "=> the ref is gone; the next section tests the tip COMMIT the row's own"
  echo "   payload printed, which survives an unreferenced ref until a gc."
else
  printf '%s\n' "$TIPS" | while read -r t b; do compare_tip "$b" "$t"; done
fi

# --- tips printed in the rows' own payloads --------------------------------
# A rework row records the tip it could not land ("Branch tip is 3676d3bb ...").
# The ref may be gone while the COMMIT object survives, so this is the test that
# still works after a cleanup — and per-file blob equality is the verdict.
hdr "tip commits printed in the rows' payloads (survives a deleted ref, until a gc)"
PAYLOAD_TIPS="$(
  q "select replace(coalesce(payload,''),char(10),' ') from tasks where status='$STATUS_FAILED' and created_at > $SINCE;" |
  grep -oE '(tip is |Branch tip is |tip |\()[0-9a-f]{8}' | grep -oE '[0-9a-f]{8}$' | sort -u | head -"$MAX_PAYLOAD_TIPS"
)"
if [ -z "$PAYLOAD_TIPS" ]; then
  echo "no 8-hex token found in any failed row's payload — ${CANNOT_DETERMINE}"
else
  printf '%s\n' "$PAYLOAD_TIPS" | while read -r t; do
    if run_git rev-parse --verify --quiet "${t}^{commit}" >/dev/null 2>&1; then
      compare_tip "payload tip $t" "$t"
    else
      printf '\n--- payload tip %s: OBJECT-ABSENT — the commit is gone from this repo\n' "$t"
      printf '    %s here: no ref and no object. The only remaining evidence is the\n' "$CANNOT_DETERMINE"
      printf '    archived diff path printed in the row payload (outside the repo).\n'
    fi
  done
fi


hdr "rows whose summary carries no work identity (probe/auto-shaped)"
q "select task_id||' '||date(created_at,'unixepoch','localtime')||' '||substr(replace(summary,char(10),' '),1,$SUMMARY_WIDTH)
   from tasks where status='$STATUS_FAILED' and created_at > $SINCE and summary like '[auto]%' order by created_at;"
echo "(this script does NOT verdict these — '[auto] <verb>' is a fingerprint of a probe run,"
echo " not a statement of work; the audit report classifies them.)"

# --- named-area activity: was the row's own area touched on master AFTER it? --
# The strongest still-needed signal available without judgement: the row names
# files (its payload usually does), and master shows NO commit touching them
# since the row was created. A row whose area nobody has touched is a row nobody
# picked up. This is a MEASUREMENT, not a verdict — the report classifies.
hdr "named-area activity — master commits touching each row's own files, since the row"
q "select task_id||'|'||date(created_at,'unixepoch','localtime')||'|'||replace(coalesce(payload,'')||' '||replace(summary,char(10),' '),char(10),' ')
   from tasks where status='$STATUS_FAILED' and created_at > $SINCE order by created_at;" |
while IFS='|' read -r id day text; do
  paths="$(printf '%s\n' "$text" | grep -oE '(cmd|internal|devdoc|scripts)/[A-Za-z0-9_./-]+\.(go|md|sh|tsv|py)' | sort -u | head -"$MAX_PATHS_PER_ROW")"
  if [ -z "$paths" ]; then
    printf '%-38s %s  %s: the row names no file path\n' "$id" "$day" "$CANNOT_DETERMINE"
    continue
  fi
  n=0
  detail=""
  while read -r p; do
    [ -z "$p" ] && continue
    c="$(run_git log --oneline --since="$day 00:00" "$MASTER_REF" -- "$p" 2>/dev/null | wc -l)"
    last="$(run_git log -1 --format=%cd --date=short "$MASTER_REF" -- "$p" 2>/dev/null)"
    n=$((n + c))
    detail="$detail $p(last=${last:-none},since=${c})"
  done <<EOF
$paths
EOF
  if [ "$n" = "0" ]; then
    printf '%-38s %s  UNTOUCHED SINCE:%s\n' "$id" "$day" "$detail"
  else
    printf '%-38s %s  touched:%s\n' "$id" "$day" "$detail"
  fi
done

hdr "what this script did not determine"
cat <<'EOF'
- the CLASSIFICATION of each row (solved-elsewhere / still-needed / moot / unclear):
  the raw facts are above; the judgement is the audit report's, and a claim there
  must carry the command+output that supports it.
- whether an archived diff OUTSIDE the repo still exists: the script prints the
  row's own sentence; it does not read the operator's workspace.
- whether an absent branch was merged, pruned, or never pushed: ref absence is
  ref absence. Only per-file blob truth answers "is the content in master".
- whether a successor row actually executed its work: only its status is printed.
EOF
