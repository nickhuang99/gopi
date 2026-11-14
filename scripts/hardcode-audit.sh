#!/usr/bin/env bash
# hardcode-audit.sh — measure HARDCODED ELEMENTS in a tracked source tree.
#
# A "hardcoded element" is a literal that binds the software to ONE operator or
# ONE machine instead of deriving the value from configuration: an absolute
# personal path (/home/<user>), a personal/machine identifier (a machine name such as nick-<initials>, hostb,
# nick@), a private LAN address, or an embedded secret. This script finds them,
# and — that is the point — says what each hit IS:
#
#   FILE CLASS   PROD  shipped code (a .go/.sh/.py/Makefile a user runs)
#                TEST  a test, a test fixture or a test-data tree
#                DOC   prose: markdown, devdoc briefings, evidence dumps
#   LINE KIND    CODE  the value is on an executable line — the shipped
#                      software can consume it
#                COMMENT  it sits in a comment (`//`, `#`, `/* … */`) — prose
#                      that happens to live in a source file
#                PROSE    it sits in a DOC-class file
#
# Only PROD+CODE is a DEFECT by default: a doc-comment mention is not a defect,
# and a test fixture is allowed to name a machine. Everything else is reported
# and counted so the review can adjudicate, never silently dropped.
#
# EXIT CODES (the gate):
#   0  every PROD+CODE hit is a REVIEWED one (listed in the baseline file)
#   1  at least one UNREVIEWED PROD+CODE hit — a new hardcode shipped
#   2  usage / environment error (not a git tree, bad flag, missing baseline)
#
# DETERMINISM: output is sorted (LC_ALL=C), carries no timestamps, no absolute
# paths of the invoking shell, and no counts that depend on the checkout
# location. Two runs on the same commit print byte-identical output.
#
# ZERO DEPENDENCY: bash + git + awk + sort. No Go, no python, no ripgrep.
#
# Usage:
#   scripts/hardcode-audit.sh                  # table + hits + exit code
#   scripts/hardcode-audit.sh --table          # the per-class count table only
#   scripts/hardcode-audit.sh --prod-code      # only PROD+CODE hits (the gate)
#   scripts/hardcode-audit.sh --all-hits       # every hit, every class
#   scripts/hardcode-audit.sh --baseline FILE  # reviewed-hit list (default below)
#   scripts/hardcode-audit.sh --self-check     # assert the classifier, then exit
set -u
set -o pipefail
export LC_ALL=C

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
DEFAULT_BASELINE="scripts/hardcode-audit-baseline.tsv"

# --- the scratch contract -------------------------------------------------
# This script builds throwaway working directories (a self-check fixture, the
# hit/table files of a scan). They MUST come from the shared helper rather than
# `mktemp`/`$TMPDIR`: on this host that honour puts them on the `/` partition the
# 2026-09-11 exhaustion filled (see scripts/lib/tmp-ban.sh, and the gate that
# measured it — internal/testutil/scripts_gate_test.go). Sourcing the helper is
# also what registers the directory for trap-time cleanup below.
# shellcheck source=lib/tmp-ban.sh
. "$ROOT/scripts/lib/tmp-ban.sh" || exit 2

# --- what counts as a neutral home directory ------------------------------
# A path under one of these users says nothing about a person: it is the
# convention a container/CI publishes. Overridable, because a repo whose CI
# user really is `runner` must still be able to flag a REAL personal path.
NEUTRAL_HOME_USERS="${HARDCODE_NEUTRAL_HOME_USERS:-runner user app ci ubuntu vagrant gopher docker node test example nobody}"

# --- the classes ----------------------------------------------------------
# ONE class per line, then a parallel PATS list: the two are ZIP'd by index in
# the scanner, so adding a class means adding its pattern at the SAME position.
# Patterns are POSIX ERE with every `.` written as [.] — awk's -v processing
# eats a backslash, so a regex passed that way must not depend on one.
#
# The SECRET patterns deliberately demand a LONG, CONTIGUOUS payload: `sk-`
# followed by 16 alphanumerics is a key, while this repo's own CLI verb
# prefixes (`sk-ledger-archive`, `sk-self-reclaim`) are hyphen-separated short
# words and must NOT be reported — a detector that cries wolf on its own
# vocabulary gets switched off, which is worse than no detector.
CLASSES="PERSONAL_PATH OPERATOR_IDENT PRIVATE_IP SECRET SECRET_INLINE"
PATS="\
[/]home[/][A-Za-z0-9_.-]+|[/]Users[/][A-Za-z0-9_.-]+\
@@nick-[A-Z][A-Za-z]|nick@|peerbox\
@@(192[.]168|10[.][0-9]+|172[.](1[6-9]|2[0-9]|3[01]))[.][0-9]+[.][0-9]+\
@@sk-[A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9][A-Za-z0-9]\
|AKIA[0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z][0-9A-Z]\
|ghp_[A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_][A-Za-z0-9_]\
|xox[baprs]-[A-Za-z0-9-][A-Za-z0-9-][A-Za-z0-9-][A-Za-z0-9-][A-Za-z0-9-][A-Za-z0-9-][A-Za-z0-9-][A-Za-z0-9-][A-Za-z0-9-][A-Za-z0-9-]\
|BEGIN [A-Z ]*PRIVATE KEY\
@@(api[_-]?key|apikey|secret|token|password|passwd)[[:space:]]*=[[:space:]]*\""

# The list separator: a class name contains no "@@", and no pattern above
# contains one either. A raw control character does not survive awk -v.
SEPOP="@@"

MODE=report
BASELINE="$DEFAULT_BASELINE"
want_usage=0
while [ $# -gt 0 ]; do
	case "$1" in
	--table) MODE=table ;;
	--prod-code) MODE=prodcode ;;
	--all-hits) MODE=report ;;
	--baseline)
		shift
		[ $# -gt 0 ] || {
			echo "hardcode-audit: --baseline needs a path" >&2
			exit 2
		}
		BASELINE="$1"
		;;
	--self-check) MODE=selfcheck ;;
	-h | --help)
		want_usage=1
		MODE=usage
		;;
	*)
		echo "hardcode-audit: unknown argument: $1" >&2
		exit 2
		;;
	esac
	shift
done

case "$MODE" in
usage)
	sed -n '2,45p' "$HERE/hardcode-audit.sh" | sed 's/^# \{0,1\}//'
	exit 0
	;;
esac

cd "$ROOT" || exit 2
command -v git >/dev/null 2>&1 || {
	echo "hardcode-audit: git is required (this measures TRACKED files)" >&2
	exit 2
}
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
	echo "hardcode-audit: $ROOT is not a git working tree" >&2
	exit 2
}

# --- file classification --------------------------------------------------
# DOC: prose trees and prose formats. TEST: tests, fixtures, test data.
# Everything else is PROD — the default is the dangerous one on purpose, so a
# new file type is REPORTED rather than assumed harmless.
classify_file() {
	local f="$1"
	case "$f" in
	"$DEFAULT_BASELINE" | "$BASELINE" | "$SELF_PATH") echo SKIP ;;
	*.png | *.gz | *.wav | *.gif | *.jpg | *.jpeg | *.webp | *.zip | *.tgz | *.out | *.log) echo SKIP ;;
	*_test.go | */tests/* | */test_*.py | */tests_*.go | testsuite/* | testdata/* | */testdata/*) echo TEST ;;
	*_scenarios.js | *_harness.js) echo TEST ;;
	devdoc/* | briefings/* | reports/* | bugs/* | archive/* | verify-evidence/* | .evid/* | man/*) echo DOC ;;
	*.md | *.markdown | *.rst | *.txt | *.tsv | *.csv) echo DOC ;;
	*) echo PROD ;;
	esac
}

# --- the scanner ----------------------------------------------------------
# ONE awk process reads every candidate file. A per-file awk child costs ~25ms
# of interpreter start on this tree's 3k candidates — the whole census must
# stay well inside the 2-minute net, so the loop lives in awk, not in bash.
# File class is computed INSIDE awk (class_of) from the same rules bash uses,
# so the two cannot drift: classify_file is the single statement of those
# rules for bash callers, class_of mirrors it for the scan.
SELF_PATH="scripts/hardcode-audit.sh"
ONE_SCAN='
function class_of(f,   base) {
	if (f in CLS) return CLS[f]
	base = f; sub(/.*[/]/, "", base)
	if (f == baseline || f == self) { CLS[f] = "SKIP"; return "SKIP" }
	if (f ~ /[.](png|gz|wav|gif|jpg|jpeg|webp|zip|tgz|out|log)$/) { CLS[f] = "SKIP"; return "SKIP" }
	if (base ~ /_test[.]go$/ || f ~ /(^|[/])tests[/]/ || base ~ /^test_/ || base ~ /^tests_/ ||
	    f ~ /^testsuite[/]/ || f ~ /(^|[/])testdata[/]/ ||
	    base ~ /_(scenarios|harness)[.]js$/) { CLS[f] = "TEST"; return "TEST" }
	if (f ~ /^(devdoc|briefings|reports|bugs|archive|verify-evidence|man|evid)[/]/ ||
	    f ~ /^[.]evid[0-9]*[/]/) { CLS[f] = "DOC"; return "DOC" }
	if (f ~ /[.](md|markdown|rst|txt|tsv|csv)$/) { CLS[f] = "DOC"; return "DOC" }
	CLS[f] = "PROD"; return "PROD"
}
function init(   i, nc, np, cl, pt, n, nl) {
	nc = split(classes, cl, " ")
	np = split(pats, pt, sep)
	if (nc != np) { print "hardcode-audit: " nc " classes vs " np " patterns" > "/dev/stderr"; exit 2 }
	for (i = 1; i <= nc; i++) { CN[i] = cl[i]; PT[i] = pt[i] }
	NC = nc
	split(neutral, nl, " ")
	for (i in nl) if (nl[i] != "") NEUTRAL[nl[i]] = 1
}
# firstprose returns the column where prose starts on this line (0 = none),
# tracking string literals so a "//" inside "http://x" is not prose, and the
# global block state bs across lines.
function firstprose(s, upto,   i, n, ch, nch, instr, q) {
	n = length(s); instr = 0; q = ""
	for (i = upto; i <= n; i++) {
		ch = substr(s, i, 1); nch = substr(s, i + 1, 1)
		if (bs == 1) { if (ch == "*" && nch == "/") { bs = 0; i++ } ; continue }
		if (instr) { if (ch == "\\") { i++ } else if (ch == q) instr = 0; continue }
		if (ch == "\"" || ch == "\047" || ch == "`") { instr = 1; q = ch; continue }
		if (ch == "#" && LMARK == "#") return i
		if (ch == "/" && nch == "/") return i
		if (ch == "/" && nch == "*") { bs = 1; i++ }
	}
	return 0
}
# advancestate carries the block-comment state across lines. The precise scan
# is expensive, so it only runs on a line that can move the state at all: a
# line with no "/*" and no "*/" leaves bs exactly as it was.
function advancestate(s) {
	if (index(s, "/*") == 0 && index(s, "*/") == 0) return
	firstprose(s, 1)
}
# kind_at answers CODE or COMMENT from the state UP TO the hit column: a
# trailing "x = 1 // see /home/u" is a COMMENT hit, not a code one.
function kind_at(s, pos,   i, n, ch, nch, instr, q, st) {
	if (fclass == "DOC") return "PROSE"
	st = bs_start; instr = 0; q = ""; n = length(s)
	for (i = 1; i < pos; i++) {
		ch = substr(s, i, 1); nch = substr(s, i + 1, 1)
		if (st == 1) return "COMMENT"
		if (instr) { if (ch == "\\") i++; else if (ch == q) instr = 0; continue }
		if (ch == "\"" || ch == "\047" || ch == "`") { instr = 1; q = ch; continue }
		if (ch == "#" && LMARK == "#") return "COMMENT"
		if (ch == "/" && nch == "/") return "COMMENT"
		if (ch == "/" && nch == "*") { st = 1; i++ }
	}
	return (st == 1) ? "COMMENT" : "CODE"
}
function home_user(tok,   t) {
	t = tok
	sub(/^[/](home|Users)[/]/, "", t)
	sub(/[/].*$/, "", t)
	return t
}
BEGIN {
	init()
	fname = ""
	bs = 0
	# PRE is a cheap union of the literal core of every class. A line matching
	# none of it CANNOT produce a hit, so the per-class match loops are skipped,
	# which is what keeps the census at seconds, not minutes.
	PRE = "[/]home[/]|[/]Users[/]|nick-[A-Z][A-Za-z]|nick@|peerbox|192[.]168|10[.]|172[.]|sk-|AKIA|ghp_|xox|PRIVATE KEY|key|apikey|secret|token|password|passwd"
}
FNR == 1 {
	if (FILENAME != fname) {
		fname = FILENAME
		fclass = class_of(fname)
		if (fclass == "SKIP") nextfile
		LMARK = "//"
		if (fname ~ /[.](sh|py|mk)$/ || fname ~ /(^|[/])Makefile$/) LMARK = "#"
		bs = 0
	}
}
{
	bs_start = bs
	line = $0
	if (line !~ PRE) { advancestate(line); next }
	for (i = 1; i <= NC; i++) {
		s = line
		base = 0
		while (match(s, PT[i])) {
			col = base + RSTART
			tok = substr(s, RSTART, RLENGTH)
			base = col + RLENGTH - 1
			skip = 0
			if (CN[i] == "PERSONAL_PATH") {
				u = home_user(tok)
				if (u != "" && (u in NEUTRAL)) skip = 1
				# "/home" is only ABSOLUTE at a token boundary. `$D/home/x`
				# and `"$WORK/home/x"` are a scratch tree own subtree
				# spelled relative to a variable, not one operator home.
				if (col > 1) {
					pc = substr(line, col - 1, 1)
					if (index("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_}$)/]", pc)) skip = 1
				}
			}
			if (CN[i] == "PRIVATE_IP" && substr(tok, 1, 4) == "127.") skip = 1
			# a SECRET prefix is TOKEN-INITIAL: "ta" + "sk-01a0ff…" in an id
			# (`rework-backup/task-01a0ff…`) is a task id, not an API key.
			if (CN[i] == "SECRET" && col > 1) {
				pc = substr(line, col - 1, 1)
				if (index("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_/.-", pc)) skip = 1
			}
			# an inline assignment is a SECRET only when its value is long
			# enough to BE one — token = "" is a knob, not a credential.
			if (CN[i] == "SECRET_INLINE" && !skip) {
				if (col > 1 && substr(line, col - 1, 1) ~ /[A-Za-z0-9_]/) skip = 1
				else {
					vl = 0
					for (k = base + 1; k <= length(line); k++) {
						if (substr(line, k, 1) == "\"") break
						vl++
					}
					if (vl < 16) skip = 1
				}
			}
			if (!skip) printf "%s\t%d\t%s\t%s\t%s\t%s\n", fname, FNR, fclass, CN[i], kind_at(line, col), tok
			s = substr(s, RSTART + RLENGTH)
		}
	}
	advancestate(line)
}
'

# scan_text scans ONE file's text through the same classifier — used by
# --self-check on a throwaway fixture, so the proof and the census share code.
scan_text() {
	local file="$1"
	awk -v classes="$CLASSES" -v pats="$PATS" -v neutral="$NEUTRAL_HOME_USERS" \
		-v sep="$SEPOP" -v baseline="$DEFAULT_BASELINE" -v self="$SELF_PATH" "$ONE_SCAN" "$file"
}

# --- self-check -----------------------------------------------------------
# A classifier is only worth its table if it CLASSIFIES. This runs on a
# throwaway fixture (never the tree), so it is fast and machine-independent.
if [ "$MODE" = selfcheck ]; then
	work="$(gopi_scratch_mkdir hardcode-audit-selfcheck)" || exit 2
	trap gopi_scratch_cleanup EXIT
	fix="$work/fix"
	mkdir -p "$fix/internal/x" "$fix/devdoc" || exit 2
	printf '%s\n' 'package x' '' 'const p = "/home/someone/.cfg"' >"$fix/internal/x/a.go"
	printf '%s\n' 'package x' '' '// see /home/someone/.cfg' >"$fix/internal/x/b.go"
	printf '%s\n' 'package x' '' 'const a = "127.0.0.1:80"' '' 'const b = "192.168.7.9"' >"$fix/internal/x/c.go"
	printf '%s\n' 'package x' '' '/* /home/someone */' >"$fix/internal/x/d.go"
	printf '%s\n' 'package x' '' 'var q = "/home/runner/.cfg"' >"$fix/internal/x/e.go"
	printf '%s\n' 'package x' '' 'const u = "v"' '' 'const w = 1 // tail: /home/someone/x' >"$fix/internal/x/f.go"
	printf '%s\n' 'prose /home/someone/.cfg' >"$fix/devdoc/n.md"
	printf '%s\n' 'package x' '' 'var t = "/home/someone/.cfg"' >"$fix/internal/x/a_test.go"
	ok=0
	bad=0
	want() { # <file> <class> <expected substring> <label>
		local got
		got="$(cd "$fix" && scan_text "$1" | awk -F'\t' '{printf "%s/%s ", $5, $4}')"
		printf '  %-22s %-5s %s\n' "$1" "$2" "${got:-<none>}"
		case "$got" in
		*"$3"*) ok=$((ok + 1)) ;;
		*)
			echo "self-check: $4 — want $3, got [${got:-<none>}]" >&2
			bad=$((bad + 1))
			;;
		esac
	}
	notwant() { # <file> <class> <forbidden substring> <label>
		if (cd "$fix" && scan_text "$1") | grep -q -F "$3"; then
			echo "self-check: $4 — $3 must NOT be reported" >&2
			bad=$((bad + 1))
		else ok=$((ok + 1)); fi
	}
	want internal/x/a.go PROD "CODE/PERSONAL_PATH" "a literal in code is PROD+CODE"
	want internal/x/b.go PROD "COMMENT/PERSONAL_PATH" "a whole-line comment is COMMENT"
	want internal/x/d.go PROD "COMMENT/PERSONAL_PATH" "a block comment is COMMENT"
	want internal/x/f.go PROD "COMMENT/PERSONAL_PATH" "a TAIL comment is COMMENT"
	want internal/x/a_test.go TEST "CODE/PERSONAL_PATH" "a _test.go file is TEST"
	want devdoc/n.md DOC "PROSE/PERSONAL_PATH" "a doc file is DOC+PROSE"
	want internal/x/c.go PROD "CODE/PRIVATE_IP" "a private LAN address is PRIVATE_IP"
	notwant internal/x/c.go PROD "127.0.0.1" "loopback is CORRECT, not a defect"
	notwant internal/x/e.go PROD "PERSONAL_PATH" "a neutral CI home is not personal"
	for pair in internal/x/a.go:PROD internal/x/a_test.go:TEST devdoc/n.md:DOC; do
		f="${pair%%:*}"
		c="${pair##*:}"
		if [ "$(cd "$fix" && classify_file "$f")" = "$c" ]; then ok=$((ok + 1)); else
			echo "self-check: classify_file $f != $c" >&2
			bad=$((bad + 1))
		fi
	done
	if [ "$bad" -gt 0 ]; then
		echo "self-check: FAILED ($bad wrong, $ok right)" >&2
		exit 1
	fi
	echo "self-check: OK ($ok checks) — PROD/TEST/DOC and CODE/COMMENT/PROSE are distinguished;"
	echo "           loopback 127.0.0.1 and a neutral CI home are NOT reported as defects"
	exit 0
fi

# --- candidate files ------------------------------------------------------
# git grep -l cuts the scan to files that can possibly hit; the awk pass then
# re-reads the WHOLE file, because comment state needs every preceding line.
candidates="$(
	git grep -l -I -E '/home/[A-Za-z0-9_.]|/Users/[A-Za-z0-9_.]|nick-[A-Z][A-Za-z]|nick@|peerbox|\b19[0-9]\.|\b10\.[0-9]|sk-[A-Za-z0-9]|AKIA|ghp_|gh[osu]_|xox[baprs]-|PRIVATE KEY|api[_-]?key[[:space:]]*=[[:space:]]*"' \
		-- . 2>/dev/null | grep -v -F -x "$DEFAULT_BASELINE"
)" || true

work="$(gopi_scratch_mkdir hardcode-audit)"
[ -n "$work" ] && [ -d "$work" ] || {
	echo "hardcode-audit: cannot create a scratch directory" >&2
	exit 2
}
trap gopi_scratch_cleanup EXIT
hits="$work/hits.tsv"
# ONE awk process over the whole candidate list (see ONE_SCAN above): a
# per-file child would cost minutes on this tree.
printf '%s\n' "$candidates" | grep -v '^$' >"$work/cand.txt"
awk -v classes="$CLASSES" -v pats="$PATS" -v neutral="$NEUTRAL_HOME_USERS" \
	-v sep="$SEPOP" -v baseline="$DEFAULT_BASELINE" -v self="$SELF_PATH" "$ONE_SCAN" \
	$(cat "$work/cand.txt") >"$hits" 2>"$work/scan.err" || {
	echo "hardcode-audit: the file scan failed" >&2
	sed -n '1,5p' "$work/scan.err" >&2
	exit 2
}
sort -t$'\t' -k1,1 -k2,2n -k4,4 -k5,5 "$hits" -o "$hits"

# --- the class table ------------------------------------------------------
table="$work/table.tsv"
# The header is PREPENDED, never sorted with the data: the rows used to be
# emitted header-first into the sort, and `head -1` of a reverse-sorted stream
# returns a data row (TEST sorts last ascending, so it landed on line 5) — the
# table shipped its header in the middle. Emit rows only, sort, then prepend.
awk -F'\t' '
	{ n[$3"\t"$4"\t"$5]++ }
	END { for (k in n) print k "\t" n[k] }' "$hits" \
	| sort -t$'\t' -k1,1 -k2,2 -k3,3 >"$table.rows"
{ printf 'FILE-CLASS\tLINE-KIND\tCOUNT\n'; cat "$table.rows"; } >"$table"

# --- the baseline (reviewed PROD+CODE hits) -------------------------------
# Format: <path>\t<class>\t<token>\t<reason>. A hit is REVIEWED when its path,
# class and token all appear. The line NUMBER is deliberately not part of the
# key: an unrelated edit above the literal must not make a reviewed hit look
# new, and a NEW literal of the same value in the same file is still reviewed —
# the reason text, not the line number, is the review.
base="$work/base.tsv"
: >"$base"
if [ -f "$BASELINE" ]; then
	awk -F'\t' '$1 !~ /^#/ && NF >= 3 { print $1 "\t" $2 "\t" $3 }' "$BASELINE" | sort -u >"$base"
elif [ "$BASELINE" = "$DEFAULT_BASELINE" ]; then
	: # absent default baseline = nothing reviewed yet; that is a valid state
else
	echo "hardcode-audit: --baseline $BASELINE does not exist" >&2
	exit 2
fi

unreviewed="$work/unreviewed.tsv"
awk -F'\t' '
	FILENAME == ARGV[1] { reviewed[$1 "\t" $2 "\t" $3] = 1; next }
	$3 == "PROD" && $5 == "CODE" {
		k = $1 "\t" $4 "\t" $6
		if (!(k in reviewed)) print $0 "\tUNREVIEWED"
		else print $0 "\treviewed"
	}' "$base" "$hits" | sort -t$'\t' -k1,1 -k2,2n >"$unreviewed"

n_unreviewed="$(awk -F'\t' '$7 == "UNREVIEWED"' "$unreviewed" | wc -l | tr -d ' ')"
n_prodcode="$(awk -F'\t' '$3 == "PROD" && $5 == "CODE"' "$hits" | wc -l | tr -d ' ')"
n_total="$(wc -l <"$hits" | tr -d ' ')"

emit_hits() {
	local filter="$1"
	awk -F'\t' "$filter" "$hits" | awk -F'\t' '{ printf "%s:%s\t%s\t%s\t%s\n", $1, $2, $3, $5, $6 }'
}

case "$MODE" in
table)
	cat "$table"
	exit 0
	;;
prodcode)
	emit_hits '$3 == "PROD" && $5 == "CODE"'
	cat "$table"
	[ "$n_unreviewed" -eq 0 ] && exit 0
	exit 1
	;;
esac

# --- report ---------------------------------------------------------------
echo "# hardcode-audit — $(git rev-parse --short HEAD)"
echo "# files scanned: $(printf '%s\n' "$candidates" | grep -c . || true) candidate(s) of $(git ls-files | wc -l | tr -d ' ') tracked"
echo
echo "## per-class count table (hits, not files)"
cat "$table"
echo
echo "## totals"
printf 'PROD+CODE (the gate): %s   reviewed: %s   UNREVIEWED: %s\n' \
	"$n_prodcode" "$((n_prodcode - n_unreviewed))" "$n_unreviewed"
printf 'all hits: %s\n' "$n_total"
echo
if [ "$n_unreviewed" -gt 0 ]; then
	echo "## UNREVIEWED PROD+CODE hits (each one is a shipped hardcode)"
	emit_hits '$3 == "PROD" && $5 == "CODE"'
	if [ -s "$unreviewed" ]; then
		awk -F'\t' '$7 == "UNREVIEWED" { printf "%s:%s\t%s\t%s\n", $1, $2, $4, $6 }' "$unreviewed"
	fi
	echo
fi
echo "## PROD+CODE hits by file (all, reviewed marked in baseline)"
emit_hits '$3 == "PROD" && $5 == "CODE"' | cut -f1 | sort | uniq -c | sort -rn
echo
echo "## every hit (PROD/TEST/DOC x CODE/COMMENT/PROSE)"
emit_hits '1'
echo
if [ "$n_unreviewed" -eq 0 ]; then
	echo "VERDICT: clean — every PROD+CODE hardcode is a reviewed one"
	exit 0
fi
echo "VERDICT: $n_unreviewed unreviewed PROD+CODE hardcode(s)"
exit 1
