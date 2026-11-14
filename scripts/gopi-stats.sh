#!/usr/bin/env bash
# gopi-stats.sh — reproducible statistics snapshot for the GoPi project.
#
# WHY: numbers quoted inside docs drift the moment they are typed (see
# devdoc/gopi-statistics-20260920.md "stale-stat trap"). This script measures
# them from the repository so a snapshot can be RE-RUN at any commit instead
# of trusted.
#
# USAGE:
#   scripts/gopi-stats.sh                  # measure the current worktree
#   scripts/gopi-stats.sh --delta [REF]    # add a growth table against REF
#                                          # (default: the 2026-09-20 snapshot)
#
# CONTRACT: the script is READ-ONLY over the repo. It reports the commit it
# measured and warns (never fails) when the worktree is dirty, because a dirty
# tree means the numbers do not correspond to any commit.
#
# The growth table reads BOTH sides out of the OBJECT STORE (`git grep -c ''` at
# each rev) and never checks a second tree out — a foreign checkout is another
# run's worktree (GOPI.md Rule 29).
#
# Rule 1: every literal that carries meaning is a named constant below.
# Rule 27: static counts only — this script never generates device load.
set -uo pipefail
# C locale: the counts are machine-read and several pipelines break on a line
# whose length grows past `sort`'s internal buffer ("broken pipe" noise on
# stderr); a byte-oriented locale keeps the ranks stable across hosts.
export LC_ALL=C

# --- named constants (Rule 1) ------------------------------------------------
readonly TOOLNAMES_DIR="internal/toolnames"
readonly TOOLNAMES_FILE="names.go"
readonly DOC_GLOB="*.md"
readonly DEVDOC_DIR="devdoc"
readonly LEDGER_DB_DEFAULT="$HOME/.gopi/memory.db"
readonly TMPROOT_DEFAULT="$HOME/workspace/_tmp"
readonly CROSS_GOARCH="amd64"
readonly CROSS_GOOS_LIST="linux darwin windows"
readonly AGENT_PKG="./cmd/agent/"
readonly BIN_NAME="gopi"
readonly BUILD_TIMEOUT_SEC=600
readonly PLATFORM_SPECIFIC_PKGS="internal/scratch internal/procstat internal/cgroupscope internal/daemonlifetime internal/lineedit internal/shot internal/borrowedfd"
# The baseline of the previous snapshot (devdoc/gopi-statistics-20260920.md),
# so "growth" is a measurement against a named commit, not against a memory.
readonly DELTA_REF_DEFAULT="7e34258976e2f768f87df9b30b32f559bf9b6205"
readonly HELP_ENV_FLAG="--help-env"
# The `--help-env` table's own shape: a line that starts two spaces and an
# upper-case name, then the settings key, then the flag.
readonly ENV_NAME_RE='^  [A-Z][A-Z0-9_]+'
readonly DEVDOC_INDEX_FILE="devdoc/DOC-INDEX.tsv"
readonly HEAVY_INVENTORY_FILE="testdata/heavy-tests.tsv"
readonly STANDARDS_GLOB="internal/standards/*_test.go"
readonly CI_WORKFLOWS_GLOB=".github/workflows/*"
readonly SWEEP_BIN_TIMEOUT_SEC=300
readonly RELATIVE_COMMITS="40"
readonly THIRD_PARTY_PREFIX_RE='^[[:space:]]*"(github\.com|golang\.org|modernc\.org|charmbracelet|mattn)[^"]*"'
readonly CADENCE_DAYS="7 30"
# A key-shaped string is only a leak when nothing identifier-like precedes it —
# `…archive/task-01a0d5548d…` contains the letters `sk-` by accident. The
# boundary is what keeps the scan honest: the naive form reported a false
# positive in tasks/01a0d5d2-rework-report.md (a `git tag …-$(date …)` line).
readonly SECRETS_ERE='(^|[^A-Za-z0-9])sk-[A-Za-z0-9]{16,}|AKIA[0-9A-Z]{16}'
# --- capability census (the section further down) ----------------------------
# Every literal the census uses is named here once (Rule 1). The EREs are the
# census's CLASSIFIER: they decide what a row means, and they live nowhere else.
readonly CLI_DIR="cmd/agent"
readonly MAIN_GO="cmd/agent/main.go"
readonly TOOL_SCHEMA_FILE="internal/agent/loop.go"
readonly TOOL_SCHEMA_ERE='Name:[[:space:]]+toolnames\.Tool[A-Za-z]+'
readonly DISPATCH_PRED_ERE='is[A-Za-z]*Subcommand\(dispatchArgs\)'
readonly ARG0_CONST_ERE='args\[0\] *[!=]= *[A-Za-z0-9_.]+'
readonly ARG1_WORD_ERE='args\[1\] *[!=]= *"[a-z_]+"|args\[1\] *[!=]= *[A-Za-z0-9_.]+'
readonly CASE_WORD_ERE='case [A-Za-z0-9_.]+(, [A-Za-z0-9_.]+)*:'
readonly CASE_WORD_FIRST_ERE='case [A-Za-z0-9_.]+'
readonly HELPER_HOP_ERE='return [A-Za-z_]+\(args\)'
readonly VERB_CONST_ERE='CliVerb[A-Za-z]+|toolnames\.Tool[A-Za-z]+'
readonly CONST_ASSIGN_ERE=' *=+ *"[a-z_-]+"'
readonly ADVERTISED_WORD_ERE='^  gopi [a-z][a-z-]+'
readonly HELP_FLAG="-h"
readonly HELP_PROBE_TIMEOUT_SEC=20
readonly HELP_HEAD_BYTES=48
# The first line of the TOP-LEVEL usage page: a word whose `-h` prints it is
# not a dispatched command at all (main.go answers a usage query first, P0-1),
# it is prompt text that the query path intercepted.
readonly HEAD_FALLTHROUGH="GoPi — a tool-using AI coding agent"
readonly HELP_VERDICT_RENDERS="renders"
readonly HELP_VERDICT_FALLTHROUGH="fallthrough-to-toplevel"
readonly HELP_VERDICT_REFUSED="refused-by-design"
readonly HELP_VERDICT_EMPTY="empty"
# The group verbs whose sub-verbs are probed. This is a PROBE SET, declared as
# such: a sub-verb absent from it is not measured (and is not claimed).
readonly SUBVERB_PROBES="task:view,commit,review,reframe,next,list,show,submit,update memory:search,store,update,feedback"
# The eyes: a verb is an EYE when its own implementation names a precondition.
# These EREs are that test, and the verdict below prints the CHANNEL it found.
readonly EYES_PRE_X11_ERE='DISPLAY|X11|_NET_ACTIVE_WINDOW'
readonly EYES_PRE_CHROME_ERE='chrome|Chrome'
readonly EYES_PRE_TTY_ERE='isatty|IsTerminal|/dev/tty'
readonly EYES_PRE_MODEL_ERE='\.gguf'
readonly EYES_PRE_HTTP_ERE='https?://'
readonly EYES_PRE_SOCKET_ERE='net\.Listen|ListenAndServe|ListenTCP'
readonly EYES_CHROME_PATH_NAMES="chromium-browser chromium google-chrome google-chrome-stable"
readonly EYES_CHROME_CACHE_REL=".cache/ms-playwright"
readonly FONT_SRC="internal/text2img/render.go"
readonly FONT_NAME_ERE='"truetype/[A-Za-z0-9._/-]+\.tt[cf]"'
# The renderer's search roots are read from the PRODUCER's own function — the
# whole `return []string{…}` block of fontDirs(), one quoted path per line — so
# the census never re-types a directory (Rule 13: the definition site decides).
# The block runs from the line that opens the returned literal to its closing
# brace, which is what the two anchors below select.
readonly FONT_DIR_FN="fontDirs"
readonly FONT_DIR_BLOCK_OPEN_STR="return []string{"
readonly FONT_DIR_BLOCK_CLOSE_RE='^[[:space:]]*\}[[:space:]]*$'
readonly VOICE_ROOT_CONST_ERE='voiceDefaultModelRoot *=+ *"[^"]+"'
readonly REFUSE_ERE='retired|Retired|deprecated|Deprecated'
# The chrome override's NAME and the voice model root's source file are read from
# their definition sites (Rule 13: the definition site is the source of truth),
# never re-typed here. They are declared ABOVE the eyes section that uses them,
# because a shell reads top to bottom and `set -u` (line 24) makes a
# use-before-declaration a HARD FAILURE rather than an empty value: declared below
# their first use, the eyes section's `chrome` channel silently disappears for
# every verb that declares it. That regression, and the run that caught it, is
# recorded in devdoc/capability-demo-external.md.
readonly CHROME_ENV_SRC="internal/shot/chrome.go"
readonly CHROME_ENV_CONST_ERE='chromeEnvVar *=+ *"[A-Z_]+"'
readonly VOICE_SRC="cmd/agent/cli_voice.go"
# The CJK classifier is a LEAD-BYTE class, built by printf's OWN `\xHH` — the
# escape is expanded only inside the FORMAT string. It must NOT be built by
# passing hex through `%s` (`printf '[\x%s-\x%s]' E4 E9`): that emits a LITERAL
# backslash-x-E-4, whose bracket expression then reads as the character class
# {\,x,E,4,-,E,9} and matches ASCII 'A' but not '中'. MEASURED in this session:
# the `%s` form made the class match 2195 of 2195 markdown files, and the
# self-test below caught it as (CJK-char=0, ascii-char=1). The class is 0xE4..0xE9
# — the lead bytes of 3-byte UTF-8 sequences, which is where CJK lives without a
# Unicode table in this zero-dependency repo. `[^ -~]` (POSIX, always supported)
# is the WIDER number printed beside it; the two differ by the 2-, 4- and 5-byte
# planes, and that difference is itself a fact about the corpus.
readonly CJK_LEAD_BYTE_ERE=$(printf '[\xE4-\xE9]')
readonly CJK_LEAD_ERE_DESC="(the lead bytes of 3-byte UTF-8 sequences — where CJK lives without a Unicode table)"
readonly NON_ASCII_ERE='[^ -~]'
readonly CJK_SAMPLE_CHAR="中"
readonly CJK_SAMPLE_ASCII="A"
# CJK classifier self-test: the class is run against the two one-character
# controls it must separate, and the verdict is PRINTED. A classifier that has
# not been run against a positive and a negative control is not a measurement
# (Rule 31's principle, applied to the instrument itself) — and this one needed
# it: the first spelling tried matched every file in the repo.
cjk_classifier_selftest() {
  local pos neg
  pos=$(printf '%s\n' "$CJK_SAMPLE_CHAR" | grep -cE "$CJK_LEAD_BYTE_ERE" 2>/dev/null)
  neg=$(printf '%s\n' "$CJK_SAMPLE_ASCII" | grep -cE "$CJK_LEAD_BYTE_ERE" 2>/dev/null)
  if [[ "$pos" == 1 && "$neg" == 0 ]]; then
    printf 'ok (cjk-class: CJK-char=%s ascii-char=%s, want 1/0)' "$pos" "$neg"
  else
    printf 'BROKEN (cjk-class: CJK-char=%s ascii-char=%s, want 1/0)' "$pos" "$neg"
  fi
}
# The renderer's summary is Chinese, so the markers the census greps for are the
# PRODUCER's own words (internal/tools/text2img.go's Sprintf): the measured ink
# count and the font chain. Reading them here is what keeps a "✅" over a blank
# page impossible to hide.
readonly INK_MARKER="墨迹"
readonly CHAIN_MARKER="字体链"
readonly TOP_PACKAGES=10
readonly TOP_FILES=8
readonly TOOL_LEADERBOARD=12
readonly ADOPTION_SESSIONS=400
readonly ADOPTION_TOP=14
readonly KINDS_DIR_REL=".gopi/sessions"
readonly RAW_DIR_REL=".gopi/raw"
readonly RULES_FILE="GOPI.md"
readonly CONFIG_FILE="CONFIG.md"

# --- arguments ---------------------------------------------------------------
DELTA_REF=""
if [[ "${1:-}" == "--delta" ]]; then
  DELTA_REF="${2:-$DELTA_REF_DEFAULT}"
fi

cd "$(dirname "$0")/.." || exit 1

# --- helpers -----------------------------------------------------------------
hdr() { printf '\n== %s ==\n' "$1"; }
n()   { wc -l < /dev/null >/dev/null 2>&1; }          # noop guard for shellcheck
loc() { # total lines of the files listed on stdin
  xargs -r wc -l 2>/dev/null | tail -1 | awk '{print $1}'
}
cnt() { wc -l | tr -d ' '; }

# --- hygiene instrument (Rule 1: the rule below is the ONE definition) -------
# The old section printed ONE number — `git grep -l "$HOME"` over every tracked
# file — and that number is a sum of three unrelated populations, which is how
# the 2026-09-27 snapshot came to claim 52 "production .go files carrying the
# operator path": 48 of those files carry it ONLY in prose.
#
# THE CLASSIFICATION RULE, stated once and applied by the code below:
#   * a matching line whose first non-blank characters OPEN A COMMENT (`//`,
#     `/*`) is PROSE — an incident record, an example command in a usage block.
#     It is KEPT on purpose (its specificity is the evidence) and is NOT a
#     portability defect;
#   * every other matching line is CODE — a string literal, a struct tag, a
#     `#cgo` directive. That is the population the operator path is actually
#     copied into, and it is the only one worth a trend line.
# The split is LINE-WISE and by construction, never by eyeballing: the classifier
# is `HOME_COMMENT_ERE` below and nothing else. (A continuation line inside a
# block comment is out of this instrument's scope — it is counted as code, so
# the CODE number can only ever OVER-state, never hide a literal.)
readonly HOME_COMMENT_ERE='^[[:space:]]*(//|/\*)'
# The voice-zerodep-poc POC is a MEASUREMENT (it links whisper.cpp behind CGO and
# is not part of the shipped program), so it is excluded from EVERY count here
# and printed in its own bucket — mixing a proof-of-concept into "production
# code" is the same category error as mixing prose into code.
readonly HYGIENE_POC_DIR="devdoc/voice-zerodep-poc"

# hygiene_code_files REV PATHSPEC... — the files among the revision's matches
# that carry $HOME on at least one CODE line (comment lines dropped by
# HOME_COMMENT_ERE). One git invocation per candidate file; the candidate set is
# the grep's own answer, so this scales with the matches, not the tree.
hygiene_code_files() { # REV PATHSPEC...
  local rev="$1"; shift
  git grep -l "$HOME" "$rev" -- "$@" 2>/dev/null | while IFS= read -r f; do
    # `git grep -l <rev>` prints `<rev>:<path>` (the bare-path form only appears
    # when no rev is given); without this strip the file cannot be read back,
    # every candidate silently fails the read, and the count is a FALSE ZERO.
    f="${f#"$rev":}"
    if git show "$rev:$f" 2>/dev/null | grep -E "$HOME" | grep -vE "$HOME_COMMENT_ERE" | grep -q .; then
      printf '%s\n' "$f"
    fi
  done | cnt
}

# hygiene_code_lines REV PATHSPEC... — the same rule counted in LINES (the
# per-line number the file count above cannot show).
hygiene_code_lines() { # REV PATHSPEC...
  local rev="$1"; shift
  git grep -l "$HOME" "$rev" -- "$@" 2>/dev/null | while IFS= read -r f; do
    f="${f#"$rev":}"
    git show "$rev:$f" 2>/dev/null | grep -E "$HOME" | grep -vE "$HOME_COMMENT_ERE"
  done | cnt
}

# hygiene_poc_files REV — the POC's own bucket, never added to the numbers above.
hygiene_poc_files() { # REV
  git grep -l "$HOME" "$1" -- "$HYGIENE_POC_DIR" 2>/dev/null | cnt
}

# --- identity ----------------------------------------------------------------
BASE_COMMIT=$(git rev-parse HEAD 2>/dev/null || echo unknown)
BASE_DATE=$(git log -1 --format='%ci' 2>/dev/null || echo unknown)
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
DIRTY=$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ')

hdr "identity"
echo "repo:        $(pwd)"
echo "branch:      $BRANCH"
echo "base commit: $BASE_COMMIT"
echo "base date:   $BASE_DATE"
echo "dirty files: $DIRTY $([ "$DIRTY" != 0 ] && echo '(numbers do NOT correspond to a commit)')"

# --- code --------------------------------------------------------------------
hdr "code (go)"
PROD_GO=$(git ls-files '*.go' | grep -v '_test\.go$')
TEST_GO=$(git ls-files '*_test.go')
PROD_FILES=$(printf '%s\n' "$PROD_GO" | grep -c . )
TEST_FILES=$(printf '%s\n' "$TEST_GO" | grep -c . )
PROD_LOC=$(printf '%s\n' "$PROD_GO" | loc)
TEST_LOC=$(printf '%s\n' "$TEST_GO" | loc)
PROD_FUNCS=$(printf '%s\n' "$PROD_GO" | xargs -r grep -h '^func ' | cnt)
TEST_FUNCS=$(printf '%s\n' "$TEST_GO" | xargs -r grep -h '^func Test' | cnt)
BENCH_FUNCS=$(printf '%s\n' "$TEST_GO" | xargs -r grep -h '^func Benchmark' | cnt)
PKG_COUNT=$(go list ./... 2>/dev/null | cnt)
CMD_PKGS=$(go list ./cmd/... 2>/dev/null | cnt)
INTERNAL_PKGS=$(go list ./internal/... 2>/dev/null | cnt)
echo "production files:   $PROD_FILES"
echo "production LOC:     $PROD_LOC"
echo "production funcs:   $PROD_FUNCS"
echo "test files:         $TEST_FILES"
echo "test LOC:           $TEST_LOC"
echo "test funcs:         $TEST_FUNCS"
echo "benchmark funcs:    $BENCH_FUNCS"
echo "packages:           $PKG_COUNT  (cmd=$CMD_PKGS internal=$INTERNAL_PKGS)"
echo "test:prod LOC:      $(awk -v t="$TEST_LOC" -v p="$PROD_LOC" 'BEGIN{printf "%.2f", t/p}')"

# --- architecture ------------------------------------------------------------
hdr "architecture (production code by area)"
# Aggregate production LOC by the two leading path components (a package dir
# inside internal/, or cmd/<verb>/): the shape a newcomer has to learn.
printf '%s\n' "$PROD_GO" | xargs -r wc -l 2>/dev/null | grep -v ' total$' | \
  awk -v top="$TOP_PACKAGES" '{p=$2; n=split(p,a,"/"); key=(n>=3)?a[1]"/"a[2]:a[1]; loc[key]+=$1}
       END{for (k in loc) printf "%8d %s\n", loc[k], k}' | sort -rn | head -"$TOP_PACKAGES"
echo "--- largest production files ---"
printf '%s\n' "$PROD_GO" | xargs -r wc -l 2>/dev/null | grep -v ' total$' | sort -rn | head -"$TOP_FILES" | \
  awk '{printf "%8d %s\n", $1, $2}'
echo "--- top-level trees (tracked files) ---"
git ls-files | awk -F/ 'NF>1 {print $1} NF==1 {print "(root files)"}' | sort | uniq -c | sort -rn | head -"$TOP_PACKAGES" | \
  awk '{printf "  %-16s %s\n", $2, $1}'

# --- other source ------------------------------------------------------------
hdr "code (other)"
for ext in sh py js ts html css sql; do
  f=$(git ls-files "*.$ext"); c=$(printf '%s\n' "$f" | grep -c . ); l=$(printf '%s\n' "$f" | loc)
  printf '%-5s files: %-5s LOC: %s\n' "$ext" "$c" "$l"
done

# --- docs --------------------------------------------------------------------
hdr "docs"
MD_FILES=$(git ls-files "$DOC_GLOB" | cnt)
MD_LOC=$(git ls-files "$DOC_GLOB" | loc)
DEVDOC_FILES=$(git ls-files "$DEVDOC_DIR/$DOC_GLOB" | cnt)
DEVDOC_LOC=$(git ls-files "$DEVDOC_DIR/$DOC_GLOB" | loc)
echo "markdown files:   $MD_FILES"
echo "markdown LOC:     $MD_LOC"
echo "devdoc files:     $DEVDOC_FILES"
echo "devdoc LOC:       $DEVDOC_LOC"
echo "doc:code LOC:     $(awk -v d="$MD_LOC" -v p="$PROD_LOC" 'BEGIN{printf "%.2f", d/p}')"
echo "--- documents by area (tracked .md) ---"
git ls-files '*.md' | awk -F/ 'NF>1 {print $1} NF==1 {print "(root)"}' | sort | uniq -c | sort -rn | head -"$TOP_PACKAGES" | \
  awk '{printf "  %-16s %s\n", $2, $1}'
echo "rules doc:        $(grep -c '^## Rule ' "$RULES_FILE" 2>/dev/null) rules in $RULES_FILE ($(wc -l < "$RULES_FILE" 2>/dev/null) lines)"
echo "config reference: $(wc -l < "$CONFIG_FILE" 2>/dev/null) lines in $CONFIG_FILE"

# --- growth delta ------------------------------------------------------------
# Measured from the OBJECT STORE at both revs (no second checkout — a foreign
# tree is another run's worktree, GOPI.md Rule 29). `git grep -c ''` counts the
# lines of every matching blob, which is the cheapest commit-pinned census.
if [[ -n "$DELTA_REF" ]] && git rev-parse --verify --quiet "$DELTA_REF^{commit}" >/dev/null; then
  hdr "growth vs $DELTA_REF ($(git log -1 --format=%cs "$DELTA_REF"))"
  rev_census() { # REV PATHSPEC... -> "<files> <loc>"
    git grep -c '' "$1" -- "${@:2}" 2>/dev/null | awk -F: '{s+=$NF; n++} END{printf "%d %d", n, s+0}'
  }
  row() { # LABEL REV_A_SPLIT REV_B_SPLIT
    local label="$1" a="$2" b="$3" af al bf bl
    af=${a%% *}; al=${a##* }
    bf=${b%% *}; bl=${b##* }
    printf '%-22s %6s -> %-6s (+%s)   %8s -> %-8s (+%s)   x%s\n' \
      "$label" "$af" "$bf" "$((bf - af))" "$al" "$bl" "$((bl - al))" \
      "$(awk -v x="$al" -v y="$bl" 'BEGIN{printf "%.2f", (x>0)?y/x:0}')"
  }
  row "production go" "$(rev_census "$DELTA_REF" '*.go' ':(exclude)*_test.go')" "$(rev_census HEAD '*.go' ':(exclude)*_test.go')"
  row "test go"       "$(rev_census "$DELTA_REF" '*_test.go')"                  "$(rev_census HEAD '*_test.go')"
  row "markdown"      "$(rev_census "$DELTA_REF" '*.md')"                       "$(rev_census HEAD '*.md')"
  row "shell"         "$(rev_census "$DELTA_REF" '*.sh')"                       "$(rev_census HEAD '*.sh')"
  row "python"        "$(rev_census "$DELTA_REF" '*.py')"                       "$(rev_census HEAD '*.py')"
  echo "(columns: files old -> new (delta)   LOC old -> new (delta)   factor)"
  echo "commits:              $(git rev-list --count "$DELTA_REF") -> $(git rev-list --count HEAD) (+$(( $(git rev-list --count HEAD) - $(git rev-list --count "$DELTA_REF") )))"
  echo "rule count:           $(git show "$DELTA_REF:$RULES_FILE" 2>/dev/null | grep -c '^## Rule ') -> $(grep -c '^## Rule ' "$RULES_FILE") rules"
  echo "tool names:           $(git show "$DELTA_REF:$TOOLNAMES_DIR/$TOOLNAMES_FILE" 2>/dev/null | grep -oE 'Tool[A-Za-z]+ *= *"[a-z_]+"' | sed 's/.*"\(.*\)"/\1/' | sort -u | cnt) -> $(grep -oE 'Tool[A-Za-z]+ *= *"[a-z_]+"' "$TOOLNAMES_DIR/$TOOLNAMES_FILE" 2>/dev/null | sed 's/.*"\(.*\)"/\1/' | sort -u | cnt)"
  echo "hardcoded $HOME:    $(hygiene_code_files "$DELTA_REF" '*.go' ':(exclude)*_test.go' ":(exclude)$HYGIENE_POC_DIR/*") -> $(hygiene_code_files HEAD '*.go' ':(exclude)*_test.go' ":(exclude)$HYGIENE_POC_DIR/*") production .go CODE files ($(hygiene_code_lines "$DELTA_REF" '*.go' ':(exclude)*_test.go' ":(exclude)$HYGIENE_POC_DIR/*") -> $(hygiene_code_lines HEAD '*.go' ':(exclude)*_test.go' ":(exclude)$HYGIENE_POC_DIR/*") CODE lines) — all tracked: $(git grep -l "$HOME" "$DELTA_REF" 2>/dev/null | cnt) -> $(git grep -l "$HOME" 2>/dev/null | cnt) files"
else
  hdr "growth delta"
  echo "(skipped — no REF; run with: scripts/gopi-stats.sh --delta [REF])"
fi

# --- feature surface ---------------------------------------------------------
hdr "feature surface"
TOOL_NAMES_FILE="$TOOLNAMES_DIR/$TOOLNAMES_FILE"
if [[ -f "$TOOL_NAMES_FILE" ]]; then
  TOOLS=$(grep -oE 'Tool[A-Za-z]+ *= *"[a-z_]+"' "$TOOL_NAMES_FILE" | sed 's/.*"\(.*\)"/\1/' | sort -u)
  echo "LLM tools:        $(printf '%s\n' "$TOOLS" | cnt)"
  echo "tool names:       $(printf '%s\n' "$TOOLS" | tr '\n' ' ')"
fi
# A fresh worktree has no bin/ (it is gitignored) — build a query binary into
# the scratch root so the CLI surface is always measurable, never skipped.
export GOTMPDIR="${GOTMPDIR:-$TMPROOT_DEFAULT}"
mkdir -p "$GOTMPDIR" 2>/dev/null
# Two names the census needs are READ FROM THEIR DEFINITION SITES (Rule 13/14:
# a constant exists to be referenced): the voice model root and the chrome
# override env name. Both are commit-pinned reads, never re-typed here.
EYES_VOICE_ROOT=$(git show "HEAD:$VOICE_SRC" 2>/dev/null | grep -oE "$VOICE_ROOT_CONST_ERE" | head -1 | sed 's/.*"\(.*\)"/\1/')
CENSUS_CHROME_ENV_NAME=$(git show "HEAD:$CHROME_ENV_SRC" 2>/dev/null | grep -oE "$CHROME_ENV_CONST_ERE" | head -1 | sed 's/.*"\(.*\)"/\1/')
QUERY_BIN=""
if [[ -x "bin/$BIN_NAME" ]]; then
  QUERY_BIN="./bin/$BIN_NAME"
elif command -v go >/dev/null && timeout "$BUILD_TIMEOUT_SEC" go build -o "$GOTMPDIR/$BIN_NAME-query-$$" "$AGENT_PKG" 2>/dev/null; then
  QUERY_BIN="$GOTMPDIR/$BIN_NAME-query-$$"
fi
if [[ -n "$QUERY_BIN" ]]; then
  VERBS=$("$QUERY_BIN" --help 2>&1 | grep -oE '^  gopi [a-z][a-z-]+' | awk '{print $2}' | sort -u)
  echo "CLI verbs:        $(printf '%s\n' "$VERBS" | cnt)"
  echo "CLI verbs list:   $(printf '%s\n' "$VERBS" | tr '\n' ' ')"
  TASK_SUB=$("$QUERY_BIN" task -h 2>&1 | grep -oE '^  gopi task [a-z-]+' | awk '{print $3}' | sort -u)
  echo "task subcommands: $(printf '%s\n' "$TASK_SUB" | cnt)"
  # The knobs are part of the public surface too: `gopi --help-env` renders the
  # catalog from internal/config, so counting its rows counts the configuration
  # surface a newcomer can actually reach.
  ENV_VARS=$("$QUERY_BIN" "$HELP_ENV_FLAG" 2>&1 | grep -oE "$ENV_NAME_RE" | tr -d ' ' | sort -u)
  SETTINGS_KEYS=$("$QUERY_BIN" "$HELP_ENV_FLAG" 2>&1 | grep -E "$ENV_NAME_RE " | awk '{print $2}' | sort -u)
  echo "env vars:         $(printf '%s\n' "$ENV_VARS" | cnt)"
  echo "settings keys:    $(printf '%s\n' "$SETTINGS_KEYS" | cnt)"
else
  echo "CLI verbs:        (no query binary could be built — skipped)"
fi

# --- capability census -------------------------------------------------------
# THE DEMO'S FACTS, MEASURED — and the WHY, because a reader who finds this
# section must not have to guess why a stats script grew four new dimensions.
#
# A capability demo makes claims about a SURFACE ("every verb has help", "the
# eyes exist", "the renderer draws CJK", "some verbs refuse on purpose"). A
# claim of that shape is a census claim (Rule 38): it must be re-runnable at
# any commit and it must be DERIVED, never a frozen hand-typed list — a list
# goes stale the moment a verb is added, and nobody notices. So each dimension
# here answers ONE question with a VERDICT and the evidence behind it:
#
#   (a) help-surface reachability — for every dispatched verb and every
#       declared LLM tool: does its help render? The verb set comes from
#       main.go's dispatch chain (the DISPATCH_PRED_ERE line), the WORD from
#       the constant the predicate compares argv[0] against — so a new verb is
#       inventoried by the next run with no edit here. The probe reproduces
#       main.go's own order: a `-h` query is answered BEFORE dispatch (P0-1),
#       so a word whose `-h` prints the TOP-LEVEL page is not a command at all
#       — it is prompt text the query path intercepted, and the census names
#       that verdict rather than calling it "help works".
#
#   (b) the eyes — which verbs READ or RENDER, and each one's PRECONDITION as a
#       machine-readable verdict (X11 / Chrome / TTY / model file / socket),
#       plus the same predicate applied to THIS HOST, so "the eyes exist" and
#       "the eyes work here" are two different numbers.
#
#   (c) the non-ASCII/CJK axis — the renderer's candidate font chain, the faces
#       present on this host, and how much of the (human-facing) corpus carries
#       non-ASCII. The CJK classifier is a LEAD-BYTE test in LC_ALL=C, which is
#       exactly what the script already pins: a PCRE \x{4e00} table is NOT used
#       because GNU grep 3.7 refuses it ("character value in \x{} too large",
#       measured in this session) — a classifier that errors is not one.
#
#   (d) the deferred/degraded list — the verbs that REFUSE by design. It is
#       measured, not transcribed: a verb whose help probe exits non-zero is
#       refusing, and the first line of the refusal is printed with it (the
#       `merger` daemon, and the `task serve`/`task merger` aliases its
#       retirement closed). The lesson is the one that matters for a demo: a
#       surface that does NOT answer must be in the census, or the demo shows a
#       capability nobody can reach.
#
# Rule 27: every number here is a STATIC count or one bounded `-h`/render probe
# of a CLI (no device load, no load generator, no saturation). Rule 35: no new
# artifact — this is the same script, one more section; it writes nothing but
# its own stdout and its temp files under $GOTMPDIR.
# The census's own working files (verb table, renderer face lists) live under
# $GOTMPDIR and are removed before the snapshot ends.
readonly CENSUS_BODY_LINES=13
# The renderer's CJK specimen: a fixed string, so the render probe is the same
# bytes on every host and every run (the file lives under $GOTMPDIR and is
# removed again — the script leaves no artifact).
readonly CENSUS_CJK_SAMPLE="知知知知知知知知知知知知知知知知知知知知"
readonly CENSUS_CJK_SAMPLE_FILE="census-cjk-sample.txt"
# The aliases whose retirement closed them: probed WITHOUT the top-level
# fallthrough, because a two-word verb never reaches the fallthrough path.
readonly DEFERRED_PROBES="task:serve task:merger"
export GOTMPDIR="${GOTMPDIR:-$TMPROOT_DEFAULT}"
mkdir -p "$GOTMPDIR" 2>/dev/null
CENSUS_TMP="$GOTMPDIR/census-$$.txt"

# census_defloc FUNC -> "path line": the FIRST definition of FUNC under the CLI
# dir at HEAD (git grep, so the answer is commit-pinned, not worktree-pinned).
census_defloc() { # FUNC
  local hit
  hit=$(git grep -nE "^func $1\(" HEAD -- "$CLI_DIR" 2>/dev/null | head -1)
  [[ -z "$hit" ]] && return 1
  hit="${hit#HEAD:}"
  printf '%s %s\n' "${hit%%:*}" "$(printf '%s' "${hit#*:}" | cut -d: -f1)"
}
census_body() { # PATH LINE — the declaration's first CENSUS_BODY_LINES lines
  git show "HEAD:$1" | sed -n "$2,$(( $2 + CENSUS_BODY_LINES - 1 ))p"
}
# census_word_of_const CONSTEXPR -> "word kind": the STRING the constant holds,
# resolved from its single definition site (Rule 13/14 — read it, never re-type
# it). Two spellings occur and both are handled: a literal assignment, and an
# ALIAS to another package's constant (`CliVerbMemory = tools.CliVerbMemoryGroup`,
# `CliVerbMerge = cliverbs.Merge`) which needs one more hop. A word that still
# cannot be resolved prints as `unresolved:<const>` — a printed verdict, never a
# silent blank (Rule 15).
census_word_of_const() { # CONSTEXPR
  local name="$1" v id
  if [[ "$name" == toolnames.* ]]; then
    v=$(grep -oE "${name#toolnames.} *= *\"[a-z_]+\"" "$TOOLNAMES_DIR/$TOOLNAMES_FILE" 2>/dev/null | head -1 | sed 's/.*"\(.*\)"/\1/')
    [[ -n "$v" ]] && { printf '%s const-tool' "$v"; return 0; }
    printf 'unresolved:%s unresolved' "$name"; return 0
  fi
  v=$(git grep -hoE "^[[:space:]]*(const )?$name *= *\"[a-z_-]+\"" HEAD -- "$CLI_DIR" 2>/dev/null | head -1 | sed 's/.*"\(.*\)"/\1/')
  if [[ -z "$v" ]]; then
    id=$(git grep -hoE "^[[:space:]]*(const )?$name *=+ *[a-zA-Z_][a-zA-Z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*" HEAD -- "$CLI_DIR" 2>/dev/null | head -1 | sed 's/.*\.//')
    if [[ -n "$id" ]]; then
      v=$(git grep -hoE "^[[:space:]]*(const )?$id *=+ *\"[a-z_-]+\"" HEAD -- 'internal/*' 'cmd/*' 2>/dev/null | head -1 | sed 's/.*"\(.*\)"/\1/')
      [[ -n "$v" ]] && { printf '%s const-alias:%s' "$v" "$id"; return 0; }
    fi
  fi
  if [[ -n "$v" ]]; then printf '%s const-cli' "$v"; return 0; fi
  printf 'unresolved:%s unresolved' "$name"
}
# census_pred_word PREDICATE -> "word definition kind": the word comes from the
# predicate's own body; when the body delegates (isBrewSubcommand -> the
# brewSubcommand helper) the helper is resolved too, because the mapping is not
# name-derived (measured: isTermSubcommand -> CliVerbCLI, isTasksSubcommand ->
# CliVerbTask). Three body shapes occur and all three are read:
#   * `args[0] == CliVerbX`                 -> one word
#   * `switch args[0] { case CliVerbX: … }` -> that case's word
#   * `args[0] == CliVerbX && args[1] == Y` -> a TWO-WORD verb (verb + sub)
# A predicate that compares argv[0] against no word at all is not a command: it
# is the REFUSAL path (`cli_undispatched_tool_words.go`, which turns a
# tool-named word into a named refusal), and it prints as kind `refusal-path`.
census_pred_word() { # PREDICATE
  local p="$1" f l body c h sub
  read -r f l < <(census_defloc "$p") || { printf 'missing-definition - -'; return 0; }
  body=$(census_body "$f" "$l")
  c=$(printf '%s\n' "$body" | grep -oE "$ARG0_CONST_ERE" | head -1 | sed 's/.*[!=]= *//')
  [[ -z "$c" ]] && c=$(printf '%s\n' "$body" | grep -oE "$CASE_WORD_ERE" | head -1 | grep -oE "$CASE_WORD_FIRST_ERE" | sed 's/case //')
  if [[ -z "$c" ]]; then
    h=$(printf '%s\n' "$body" | grep -oE "$HELPER_HOP_ERE" | head -1 | sed 's/return //;s/(args)//')
    if [[ -n "$h" ]]; then
      read -r f l < <(census_defloc "$h") || { printf 'missing-helper - -'; return 0; }
      body=$(census_body "$f" "$l")
      c=$(printf '%s\n' "$body" | grep -oE "$ARG0_CONST_ERE" | head -1 | sed 's/.*[!=]= *//')
      [[ -z "$c" ]] && c=$(printf '%s\n' "$body" | grep -oE "$CASE_WORD_ERE" | head -1 | grep -oE "$CASE_WORD_FIRST_ERE" | sed 's/case //')
    fi
  fi
  [[ -z "$c" ]] && { printf 'refusal-path %s:%s refusal-path' "$f" "$l"; return 0; }
  read -r w k < <(census_word_of_const "$c")
  sub=$(printf '%s\n' "$body" | grep -oE "$ARG1_WORD_ERE" | head -1 | sed 's/.*== *//;s/"//g')
  if [[ -n "$sub" ]]; then
    local s
    s=$(census_word_of_const "$sub")
    printf '%s %s:%s %s two-word:%s' "${w:-unresolved:$c}" "$f" "$l" "${k:-unresolved}" "${s%% *}"
    return 0
  fi
  printf '%s %s:%s %s' "${w:-unresolved:$c}" "$f" "$l" "${k:-unresolved}"
}
# census_chrome_bin -> the chrome binary THIS HOST would use, resolved through
# the same chain the shot tool uses (env override -> playwright cache -> PATH),
# with the override's NAME read from its definition site. Empty = none.
census_chrome_bin() { # [ENVNAME] — the var's VALUE is read from the caller's env
  local envname="${1:-}" envval hit
  if [[ -n "$envname" ]]; then
    # ${!name} is a FATAL expansion under `set -u` when the named variable is
    # unset — and "the operator did not set the override" is the ORDINARY case,
    # not an error. With the bare form the whole resolver died there, the
    # playwright-cache and PATH fallbacks below never ran, and every `chrome`
    # channel printed `absent` on a host that has chrome (Rule 15: a silent
    # failure that reads as a correct answer). The `-` default is what makes the
    # unset case an empty value instead.
    envval="${!envname-}"
    if [[ -n "$envval" && -x "$envval" ]]; then printf '%s' "$envval"; return; fi
  fi
  hit=$(ls -1 "$HOME/$EYES_CHROME_CACHE_REL"/chromium-*/chrome-linux*/chrome 2>/dev/null | sort -V | tail -1)
  if [[ -n "$hit" && -x "$hit" ]]; then printf '%s' "$hit"; return; fi
  for c in $EYES_CHROME_PATH_NAMES; do
    hit=$(command -v "$c" 2>/dev/null) && { printf '%s' "$hit"; return; }
  done
  return 1
}
census_chrome_present() { [[ -n "$(census_chrome_bin "$CENSUS_CHROME_ENV_NAME")" ]]; }

# census_help_probe BIN ARGS... -> "rc bytes head": the caller's argv verbatim,
# both streams captured, bounded by HELP_PROBE_TIMEOUT_SEC.
census_help_probe() { # BIN ARGS...
  local bin="$1"; shift
  local out rc
  out=$(timeout "$HELP_PROBE_TIMEOUT_SEC" "$bin" "$@" 2>&1); rc=$?
  printf '%s %s %s' "$rc" "${#out}" "$(printf '%s' "$out" | head -1 | cut -c1-"$HELP_HEAD_BYTES")"
}
readonly REFUSE_MARKER_ERE='is retired|was retired|retired —|deprecated alias'
# census_help_verdict BIN ARGS... -> one of the HELP_VERDICT_* words. This is
# the machine-readable half: a demo claim reads the word, not the prose.
#
# ORDER MATTERS, and the middle case is why: a RETIRED verb can answer its
# refusal with exit 0 (measured: `gopi task serve -h` prints "is a deprecated
# alias …" and exits 0), so classifying on the exit status alone would file a
# dead verb as `renders`. The refusal is checked by CONTENT first, then status,
# then emptiness, then the top-level-head fallthrough.
census_help_verdict() { # BIN ARGS...
  local r rc bytes head rest
  r=$(census_help_probe "$@")
  rc=${r%% *}; rest=${r#* }; bytes=${rest%% *}; head=${rest#* }
  if printf '%s' "$head" | grep -qE "$REFUSE_MARKER_ERE"; then printf '%s' "$HELP_VERDICT_REFUSED"; return; fi
  if (( rc != 0 )); then printf '%s' "$HELP_VERDICT_REFUSED"; return; fi
  if (( bytes == 0 )); then printf '%s' "$HELP_VERDICT_EMPTY"; return; fi
  case "$head" in
    "$HEAD_FALLTHROUGH"*) printf '%s' "$HELP_VERDICT_FALLTHROUGH" ;;
    *) printf '%s' "$HELP_VERDICT_RENDERS" ;;
  esac
}

EYES_HOST_PRESENT="present"
EYES_HOST_ABSENT="absent"
CENSUS_PSEUDO_KIND="refusal-path"
CENSUS_NONWORD_ERE='^(missing-|no-word|unresolved|refusal-path)'
hdr "capability census"
# The dispatched verb set, derived from main.go's dispatch chain at HEAD. The
# predicate LIST is derived here; the WORD each one dispatches comes from the
# constant in its own body (census_pred_word).
DISPATCH_PREDS=$(git show "HEAD:$MAIN_GO" | grep -oE "$DISPATCH_PRED_ERE" | sed 's/(dispatchArgs)//' | sort -u)
echo "dispatch predicates: $(printf '%s\n' "$DISPATCH_PREDS" | cnt)  (source: git show HEAD:$MAIN_GO | grep -oE '$DISPATCH_PRED_ERE')"
echo "--- (a) help-surface reachability ---"
: > "$CENSUS_TMP"
for p in $DISPATCH_PREDS; do
  read -r w d k < <(census_pred_word "$p")
  printf '%s\t%s\t%s\t%s\n' "$w" "$p" "$d" "$k" >> "$CENSUS_TMP"
done
sort -o "$CENSUS_TMP" "$CENSUS_TMP"
CENSUS_VERBS=$(cut -f1 "$CENSUS_TMP" | grep -vE "$CENSUS_NONWORD_ERE" | sort -u)
echo "verbs declared:     $(printf '%s\n' "$CENSUS_VERBS" | cnt)  (word resolved from the const the predicate compares argv[0] against)"
echo "verbs unresolved:   "$(awk -F'\t' '$1 ~ /^unresolved/ {printf "%s(%s) ", $1, $2}' "$CENSUS_TMP" | grep . || echo "(none — every predicate's constant resolved to its string)")
echo "two-word verbs:     "$(awk -F'\t' '$4 ~ /^two-word:/ {printf "%s+%s(%s) ", $1, $4, $2}' "$CENSUS_TMP" | grep . || echo "(none)")
echo "refusal-path predicates (not commands): "$(awk -F'\t' '$4 == "'"$CENSUS_PSEUDO_KIND"'" {printf "%s(%s) ", $1, $2}' "$CENSUS_TMP" | grep . || echo "(none)")
if [[ -n "$QUERY_BIN" ]]; then
  CENSUS_RENDERS=0; CENSUS_FALLTHRU=0; CENSUS_REFUSED=0; CENSUS_EMPTY=0
  : > "$CENSUS_TMP.verdicts"
  while IFS= read -r v; do
    [[ -z "$v" ]] && continue
    vd=$(census_help_verdict "$QUERY_BIN" "$v" "$HELP_FLAG")
    printf '%s\t%s\n' "$v" "$vd" >> "$CENSUS_TMP.verdicts"
    case "$vd" in
      "$HELP_VERDICT_RENDERS") CENSUS_RENDERS=$((CENSUS_RENDERS + 1)) ;;
      "$HELP_VERDICT_FALLTHROUGH") CENSUS_FALLTHRU=$((CENSUS_FALLTHRU + 1)) ;;
      "$HELP_VERDICT_REFUSED") CENSUS_REFUSED=$((CENSUS_REFUSED + 1)) ;;
      *) CENSUS_EMPTY=$((CENSUS_EMPTY + 1)) ;;
    esac
  done <<< "$CENSUS_VERBS"
  echo "verb -h verdicts:   renders=$CENSUS_RENDERS $HELP_VERDICT_FALLTHROUGH=$CENSUS_FALLTHRU $HELP_VERDICT_REFUSED=$CENSUS_REFUSED $HELP_VERDICT_EMPTY=$CENSUS_EMPTY  (probe: '<verb> $HELP_FLAG' against the query binary built from $AGENT_PKG, timeout ${HELP_PROBE_TIMEOUT_SEC}s, first line compared against the top-level head)"
  echo "--- verb -> probe verdict ---"
  sort "$CENSUS_TMP.verdicts" 2>/dev/null | awk -F'\t' '{printf "  %-16s %s\n", $1, $2}'
  command rm -f "$CENSUS_TMP.verdicts"
  # The group verbs: a group page is not a sub-verb page, so sub-verbs are
  # probed separately (the demo reads per-command reachability, not per-group).
  echo "--- group sub-verbs probed (SUBVERB_PROBES, a declared probe set) ---"
  for grp in $SUBVERB_PROBES; do
    g="${grp%%:*}"; subs="${grp#*:}"
    for s in ${subs//,/ }; do
      printf '  %-16s %s\n' "$g $s" "$(census_help_verdict "$QUERY_BIN" "$g" "$s" "$HELP_FLAG")"
    done
  done
  # The LLM tools: 17 of the 29 declared names are tools; a tool has no CLI
  # page, so the reachability that IS measurable is the SCHEMA entry (the
  # description the model reads) — counted from its single source.
  CENSUS_SCHEMA=$(git show "HEAD:$TOOL_SCHEMA_FILE" | grep -cE "$TOOL_SCHEMA_ERE")
  echo "LLM tools with a schema entry: $CENSUS_SCHEMA  (source: git show HEAD:$TOOL_SCHEMA_FILE | grep -cE '$TOOL_SCHEMA_ERE') — a tool has no CLI help page; its reachable surface is the schema the model reads"
else
  echo "verb -h verdicts:   (not measured — no query binary; the declaration counts above are still commit-pinned)"
fi

echo "--- (b) the eyes: verbs that READ or RENDER, and their precondition ---"
if [[ -n "$QUERY_BIN" ]]; then
  printf '  %-15s %-42s %s\n' "verb" "precondition channel (from its own file)" "channel present on THIS host"
  while IFS=$'\t' read -r w p d k; do
    [[ "$w" == *" "* ]] && continue
    f="${d%%:*}"
    [[ -f "$f" ]] || continue
    chans=""
    # ONE read per verb, and a failed read is LOUD: the previous shape grepped
    # `git show HEAD:$f 2>/dev/null` once per channel, so a transient git failure
    # produced an empty body, `grep -q` failed, and the channel was silently
    # dropped from the row — a wrong capability map that looks exactly like a
    # correct one (measured: two consecutive runs disagreed on one verb's
    # `chrome` channel). stderr is no longer discarded, and a body we cannot read
    # says so and skips the verb rather than reporting a channel set we did not
    # measure.
    body=$(git show "HEAD:$f") || { echo "  (unreadable at HEAD: $f — verb skipped)" >&2; continue; }
    grep -qE "$EYES_PRE_X11_ERE" <<<"$body" && chans="$chans X11"
    grep -qE "$EYES_PRE_CHROME_ERE" <<<"$body" && chans="$chans chrome"
    grep -qE "$EYES_PRE_TTY_ERE" <<<"$body" && chans="$chans TTY"
    grep -qE "$EYES_PRE_MODEL_ERE" <<<"$body" && chans="$chans model-file"
    grep -qE "$EYES_PRE_SOCKET_ERE" <<<"$body" && chans="$chans socket"
    grep -qE "$EYES_PRE_HTTP_ERE" <<<"$body" && chans="$chans network"
    [[ -z "$chans" ]] && continue
    present=""
    for c in $chans; do
      case "$c" in
        X11) if [[ -n "${DISPLAY:-}" ]]; then present="$present X11=$EYES_HOST_PRESENT"; else present="$present X11=$EYES_HOST_ABSENT"; fi ;;
        chrome) if census_chrome_present "$CENSUS_CHROME_ENV_NAME"; then present="$present chrome=$EYES_HOST_PRESENT"; else present="$present chrome=$EYES_HOST_ABSENT"; fi ;;
        TTY) if [[ -t 0 ]]; then present="$present TTY=$EYES_HOST_PRESENT"; else present="$present TTY=$EYES_HOST_ABSENT"; fi ;;
        model-file) if [[ -d "$HOME/$EYES_VOICE_ROOT" ]]; then present="$present model-file=$EYES_HOST_PRESENT"; else present="$present model-file=$EYES_HOST_ABSENT"; fi ;;
        socket) present="$present socket=declared" ;;
        network) present="$present network=declared" ;;
      esac
    done
    printf '  %-15s %-42s %s\n' "$w" "$(printf '%s' "$chans" | sed 's/^ //')" "$(printf '%s' "$present" | sed 's/^ //')"
  done < <(awk -F'\t' '$3 ~ /:/ {print}' "$CENSUS_TMP" | sort)
  echo "  present here: DISPLAY=${DISPLAY:-<unset>}  chrome=$(census_chrome_bin "$CENSUS_CHROME_ENV_NAME" || echo '<none>')  tty=$([[ -t 0 ]] && echo yes || echo no)  stdout_isatty=$([[ -t 1 ]] && echo yes || echo no)"
else
  echo "  (not measured — no query binary)"
fi

echo "--- (c) the non-ASCII / CJK axis ---"
CENSUS_NA_MD=$(git grep -lI "$NON_ASCII_ERE" -- '*.md' 2>/dev/null | cnt)
CENSUS_CJK_MD=$(git grep -lI "$CJK_LEAD_BYTE_ERE" -- '*.md' 2>/dev/null | cnt)
CENSUS_NA_GO=$(git grep -lI "$NON_ASCII_ERE" -- '*.go' 2>/dev/null | cnt)
CENSUS_CJK_GO=$(git grep -lI "$CJK_LEAD_BYTE_ERE" -- '*.go' 2>/dev/null | cnt)
CENSUS_NA_HELP=$([[ -n "$QUERY_BIN" ]] && "$QUERY_BIN" "$HELP_FLAG" 2>&1 | grep -cE "$NON_ASCII_ERE" || echo 0)
CENSUS_CJK_SELFTEST=$(cjk_classifier_selftest)
echo "classifier self-test: $CENSUS_CJK_SELFTEST"
if [[ "$CENSUS_CJK_SELFTEST" != ok* ]]; then
  # Rule 15: a broken classifier fails LOUDLY. Reporting a count from a class
  # that matches every file would be a number with no measurement behind it.
  printf '%s\n' "census: the CJK classifier is BROKEN — refusing to report CJK figures (see the self-test above)" >&2
  CENSUS_CLASSIFIER_BROKEN=1
fi
echo "classifier:         non-ASCII = ERE '[^ -~]' (any byte outside the printable ASCII range); CJK = a raw-byte class spanning 0xE4..0xE9 $CJK_LEAD_ERE_DESC — git grep -lIE at HEAD (the class's own bytes are not echoed: printing them puts control-range bytes in the transcript)"
if [[ -n "${CENSUS_CLASSIFIER_BROKEN:-}" ]]; then
  echo "markdown files carrying non-ASCII: (withheld — classifier broken)"
  echo "go files carrying non-ASCII:       (withheld — classifier broken)"
else
  echo "markdown files carrying non-ASCII: $CENSUS_NA_MD of $(git ls-files "$DOC_GLOB" | cnt)   of which CJK-lead: $CENSUS_CJK_MD"
  echo "go files carrying non-ASCII:       $CENSUS_NA_GO   of which CJK-lead: $CENSUS_CJK_GO"
fi
echo "top-level help lines carrying non-ASCII: $CENSUS_NA_HELP  (the CLI's own help DOES carry CJK: the tool descriptions are Chinese where the tool is)"
git show "HEAD:$FONT_SRC" | grep -oE "$FONT_NAME_ERE" | tr -d '"' | sort -u > "$CENSUS_TMP.fonts"
echo "renderer candidate faces (from $FONT_SRC): $(cnt < "$CENSUS_TMP.fonts")"
echo "  $(tr '\n' ' ' < "$CENSUS_TMP.fonts")"
# The search roots come from the PRODUCER's fontDirs() body, read as a BLOCK
# (function header → the returned literal's closing brace) so no path is ever
# re-typed here.
FONT_DIR_BLOCK=$(git show "HEAD:$FONT_SRC" | awk -v fn="$FONT_DIR_FN" -v open="$FONT_DIR_BLOCK_OPEN_STR" '
  index($0, "func " fn "(") > 0 {infn=1}
  infn && index($0, open) > 0 {inblk=1; next}
  inblk && $0 ~ /^[ \t]*}[ \t]*$/ {exit}
  inblk {print}
')
printf '%s\n' "$FONT_DIR_BLOCK" | grep -oE '"[^"]+"' | tr -d '"' | sort -u > "$CENSUS_TMP.dirs"
echo "renderer search roots (read from ${FONT_DIR_FN}() in $FONT_SRC): $(cnt < "$CENSUS_TMP.dirs")  ($(tr '\n' ' ' < "$CENSUS_TMP.dirs"))"
while IFS= read -r d; do
  printf '  %-28s %s\n' "$d" "$([[ -d "$d" ]] && echo "present" || echo "absent")"
done < "$CENSUS_TMP.dirs"
CENSUS_FACES=0
while IFS= read -r d; do
  while IFS= read -r n; do
    [[ -f "$d/$n" ]] && { printf '  face present: %s\n' "$d/$n"; CENSUS_FACES=$((CENSUS_FACES + 1)); }
  done < "$CENSUS_TMP.fonts"
done < "$CENSUS_TMP.dirs"
echo "faces present on this host: $CENSUS_FACES of $(cnt < "$CENSUS_TMP.fonts") candidates"
if [[ -n "$QUERY_BIN" ]]; then
  CENSUS_SAMPLE="$GOTMPDIR/$CENSUS_CJK_SAMPLE_FILE"
  CENSUS_PNG="$GOTMPDIR/$CENSUS_CJK_SAMPLE_FILE.png"
  printf '%s\n' "$CENSUS_CJK_SAMPLE" > "$CENSUS_SAMPLE"
  CENSUS_RENDER_OUT=$(timeout "$HELP_PROBE_TIMEOUT_SEC" "$QUERY_BIN" text2img "$CENSUS_SAMPLE" "$CENSUS_PNG" 2>&1); CENSUS_RENDER_RC=$?
  echo "  render probe: text2img $CENSUS_CJK_SAMPLE_FILE (${#CENSUS_CJK_SAMPLE} CJK chars) -> rc=$CENSUS_RENDER_RC, png=$(stat -c %s "$CENSUS_PNG" 2>/dev/null || echo 0) bytes"
  printf '  render summary: %s\n' "$(printf '%s' "$CENSUS_RENDER_OUT" | head -1 | cut -c1-120)"
  echo "  summary names measured ink: $([[ "$CENSUS_RENDER_OUT" == *"$INK_MARKER"* ]] && echo yes || echo no)   names the font chain: $([[ "$CENSUS_RENDER_OUT" == *"$CHAIN_MARKER"* ]] && echo yes || echo no)  (markers are the producer's own words in internal/tools/text2img.go)"
  command rm -f "$CENSUS_SAMPLE" "$CENSUS_PNG"
fi

echo "--- (d) deferred / degraded capabilities (a verb that refuses ON PURPOSE) ---"
if [[ -n "$QUERY_BIN" ]]; then
  while IFS= read -r v; do
    [[ -z "$v" ]] && continue
    pr=$(census_help_probe "$QUERY_BIN" "$v" "$HELP_FLAG")
    rc=${pr%% *}; rest=${pr#* }; bytes=${rest%% *}; head=${rest#* }
    (( rc == 0 )) && continue
    printf '  %-15s rc=%s  %s\n' "$v" "$rc" "$(printf '%s' "$head" | cut -c1-110)"
  done <<< "$CENSUS_VERBS"
  for gs in $DEFERRED_PROBES; do
    g="${gs%%:*}"; s="${gs#*:}"
    pr=$(census_help_probe "$QUERY_BIN" "$g" "$s" "$HELP_FLAG")
    rc=${pr%% *}; rest=${pr#* }; head=${rest#* }
    printf '  %-15s rc=%s  %s\n' "$g $s" "$rc" "$(printf '%s' "$head" | cut -c1-110)"
  done
  echo "  non-refusing verbs above are the WHOLE live surface: every other declared verb answers its own page"
fi
echo "--- NOT printed by this census (declared, so nothing is inferred from its absence) ---"
echo "  per-verb flag inventories, per-tool schema prose, and any behaviour that needs a display, a"
echo "  browser or a model file at CALL time: this instrument measures what is PRINTED and what is"
echo "  DECLARED at a commit. It never runs a capability to see whether it works (Rule 27)."
command rm -f "$CENSUS_TMP" "$CENSUS_TMP.fonts" "$CENSUS_TMP.dirs"

# --- binary ------------------------------------------------------------------
hdr "binary"
if command -v go >/dev/null; then
  export GOTMPDIR="${GOTMPDIR:-$TMPROOT_DEFAULT}"
  mkdir -p "$GOTMPDIR" 2>/dev/null
  OUT="$GOTMPDIR/$BIN_NAME-stats-$$"
  if timeout "$BUILD_TIMEOUT_SEC" go build -o "$OUT" "$AGENT_PKG" 2>/dev/null; then
    echo "fresh build size: $(stat -c %s "$OUT" 2>/dev/null) bytes ($(du -h "$OUT" 2>/dev/null | cut -f1))"
    rm -f "$OUT"
  else
    echo "fresh build:      FAILED (see go build output)"
  fi
fi
[[ -x "bin/$BIN_NAME" ]] && echo "shipped bin/$BIN_NAME: $(stat -c %s "bin/$BIN_NAME") bytes ($(date -r "bin/$BIN_NAME" +%Y-%m-%d 2>/dev/null))"

# --- repo size ---------------------------------------------------------------
hdr "size"
echo "tracked files:    $(git ls-files | cnt)"
echo "tracked bytes:    $(git ls-files -z | xargs -0 -r wc -c 2>/dev/null | tail -1 | awk '{print $1}')"
# In a linked worktree `.git` is a pointer FILE; the real object store is the
# common dir (the main repo's .git) — measure that, not the pointer.
GIT_COMMON=$(git rev-parse --git-common-dir 2>/dev/null)
echo "git dir:          $GIT_COMMON"
echo "git dir bytes:    $(du -sb "$GIT_COMMON" 2>/dev/null | cut -f1)"
echo "worktree bytes:   $(du -sb --exclude=.git . 2>/dev/null | cut -f1)"

# --- history -----------------------------------------------------------------
hdr "history"
echo "first commit:     $(git log --reverse --format='%h %ci' 2>/dev/null | head -1)"
echo "commits:          $(git rev-list --count HEAD 2>/dev/null)"
echo "merge commits:    $(git log --merges --oneline 2>/dev/null | cnt)"
echo "branches local:   $(git branch --format='%(refname:short)' | cnt)"
echo "branches remote:  $(git branch -r --format='%(refname:short)' | cnt)"
echo "worktrees:        $(git worktree list | cnt)"
echo "tags total:       $(git tag | cnt)"
echo "tags archive/*:   $(git tag -l 'archive/*' | cnt)"
echo "contributors:     $(git log --format='%an' | sort -u | cnt)"
echo "lines added:      $(git log --numstat --format='' | awk '{a+=$1} END{print a+0}')"
echo "lines deleted:    $(git log --numstat --format='' | awk '{d+=$2} END{print d+0}')"
echo "files touched:    $(git log --numstat --format='' | cnt)"
echo "--- commits per month ---"
git log --format='%ad' --date=format:'%Y-%m' | sort | uniq -c | awk '{printf "  %s %s\n",$2,$1}'
echo "--- commit type prefixes ---"
git log --format='%s' | grep -oE '^[a-z]+(\([^)]*\))?:' | sed 's/(.*//;s/://' | sort | uniq -c | sort -rn | head -10 | awk '{printf "  %-10s %s\n",$2,$1}'

# --- ledger ------------------------------------------------------------------
hdr "ledger & knowledge base (~/.gopi)"
DB="${GOPI_MEMORY_DB:-$LEDGER_DB_DEFAULT}"
if [[ -f "$DB" ]] && command -v sqlite3 >/dev/null; then
  q() { sqlite3 "file:$DB?mode=ro" "$1" 2>/dev/null; }
  echo "db:               $DB ($(stat -c %s "$DB") bytes ($(du -h "$DB" | cut -f1)))"
  for t in tasks distilled_qa procedural memory_feedback task_events chats session_conclusions; do
    printf 'rows %-18s %s\n' "$t" "$(q "select count(*) from $t;" || echo '(no table)')"
  done
  echo "--- tasks by status ---"
  q "select status,count(*) from tasks group by status order by 2 desc;" | awk -F'|' '{printf "  %-12s %s\n",$1,$2}'
  echo "--- tasks by source ---"
  q "select source,count(*) from tasks group by source order by 2 desc;" | awk -F'|' '{printf "  %-12s %s\n",$1,$2}'
  echo "--- tasks by driving model ---"
  q "select case when model is null or model='' then '(none)' else model end,count(*) from tasks group by 1 order by 2 desc limit 5;" | awk -F'|' '{printf "  %-22s %s\n",$1,$2}'
  echo "--- task ledger cadence ---"
  for d in $CADENCE_DAYS; do
    printf '  tasks created, last %2s days: %s\n' "$d" "$(q "select count(*) from tasks where created_at > strftime('%s','now')-$d*86400;")"
  done
  printf '  tasks with verify_evidence:  %s\n' "$(q "select count(*) from tasks where verify_evidence is not null and verify_evidence<>'';")"
  # failed-needs-reissue — the census behind internal/tools/taskreissue.go
  # (the ONE mechanism: `gopi task list` and the resident task-lifecycle
  # sweep both render this count). The predicate is the mechanism's own, in
  # SQL: a `failed` row (terminal by design) with NO successor row and no
  # `resolved` tag is a debt nothing in the system will ever mention again.
  # Stated here so the figure is RE-DERIVABLE (GOPI.md Rule 38) rather than
  # typed — the tag is matched as a whole comma-delimited token, exactly as
  # isTaskResolved does in Go.
  printf '  failed-needs-reissue:        %s\n' "$(q "select count(*) from tasks t where t.status='failed' and not exists(select 1 from tasks c where c.parent_task_id=t.task_id) and (','||replace(coalesce(t.tags,''),' ','')||',') not like '%,resolved,%';")"
  echo "--- knowledge base ---"
  echo "  distilled_qa by tier:  $(q "select group_concat(tier||'='||c,' ') from (select tier,count(*) c from distilled_qa group by tier);")"
  echo "  feedback by rating:    $(q "select group_concat(rating||'='||c,' ') from (select rating,count(*) c from memory_feedback group by rating);")"
  printf '  procedural workflows:  %s\n' "$(q "select count(*) from procedural;")"
  printf '  retrieval count total: %s\n' "$(q "select coalesce(sum(retrieval_count),0) from distilled_qa;")"
else
  echo "db:               (not available)"
fi

# --- session corpus & real tool adoption -------------------------------------
hdr "sessions & tool adoption"
SESS_DIR="$HOME/$KINDS_DIR_REL"
if [[ -d "$SESS_DIR" ]]; then
  echo "session dirs:     $(ls -d "$SESS_DIR"/*/ 2>/dev/null | cnt)"
  echo "session logs:     $(find "$SESS_DIR" -name '*.jsonl' 2>/dev/null | cnt) jsonl files"
  echo "corpus size:      $(du -sh "$SESS_DIR" 2>/dev/null | cut -f1) (+ $(du -sh "$HOME/$RAW_DIR_REL" 2>/dev/null | cut -f1) of raw request/response logs)"
  # Adoption: the NEWEST ADOPTION_SESSIONS logs by mtime, counting `"name":"…"`
  # tool_use records. This measures what the LLM actually calls, which is the
  # only honest test of a declared tool surface (GOPI.md Rule 11).
  mapfile -t NEWEST < <(find "$SESS_DIR" -name '*.jsonl' -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -"$ADOPTION_SESSIONS" | cut -d' ' -f2-)
  echo "adoption sample:  ${#NEWEST[@]} newest session logs ($(printf '%s\n' "${NEWEST[@]}" | xargs -r du -cb 2>/dev/null | tail -1 | awk '{printf "%.1f", $1/1048576}') MiB)"
  if [[ ${#NEWEST[@]} -gt 0 ]]; then
    printf '%s\n' "${NEWEST[@]}" | xargs -r grep -ho '"name":"[a-z_]*"' 2>/dev/null | sed 's/.*"\(.*\)"/\1/' | sort | uniq -c | sort -rn > "$GOTMPDIR/adoption-$$.txt"
    TOTAL_CALLS=$(awk '{s+=$1} END{print s+0}' "$GOTMPDIR/adoption-$$.txt")
    echo "tool calls:       $TOTAL_CALLS"
    echo "--- top $ADOPTION_TOP tools (share of all calls) ---"
    head -"$ADOPTION_TOP" "$GOTMPDIR/adoption-$$.txt" | awk -v t="$TOTAL_CALLS" '{printf "  %-16s %7s  %5.1f%%\n", $2, $1, ($1*100)/t}'
    echo "--- declared-but-rare (bottom of the ranking) ---"
    tail -6 "$GOTMPDIR/adoption-$$.txt" | awk -v t="$TOTAL_CALLS" '{printf "  %-16s %7s  %5.2f%%\n", $2, $1, ($1*100)/t}'
    rm -f "$GOTMPDIR/adoption-$$.txt"
  fi
else
  echo "session corpus:   (none at $SESS_DIR)"
fi

# --- portability -------------------------------------------------------------
hdr "portability (cross-compile)"
echo "cgo files (import \"C\"): $(grep -rl 'import "C"' --include='*.go' . 2>/dev/null | grep -v _test | cnt)"
echo "platform-specific packages present:"
for p in $PLATFORM_SPECIFIC_PKGS; do
  [[ -d "$p" ]] && printf '  %s (%s files)\n' "$p" "$(ls "$p"/*.go 2>/dev/null | cnt)"
done
if command -v go >/dev/null; then
  export GOTMPDIR="${GOTMPDIR:-$TMPROOT_DEFAULT}"
  for os in $CROSS_GOOS_LIST; do
    out=$(GOOS="$os" GOARCH="$CROSS_GOARCH" go build ./... 2>&1)
    rc=$?
    if [[ $rc -eq 0 ]]; then
      printf 'GOOS=%-8s BUILD OK\n' "$os"
    else
      pkgs=$(printf '%s\n' "$out" | grep '^#' | sed 's/^# //' | sort -u | tr '\n' ' ')
      errs=$(printf '%s\n' "$out" | grep -c 'undefined:\|cannot use\|has no field')
      printf 'GOOS=%-8s BUILD FAIL — %s errors in: %s\n' "$os" "$errs" "$pkgs"
    fi
  done
fi

# --- hygiene -----------------------------------------------------------------
hdr "open-source hygiene"
for f in LICENSE LICENSE.md COPYING COPYING.md; do
  [[ -f "$f" ]] && echo "license file:     $f" && break
done
[[ -f LICENSE || -f LICENSE.md || -f COPYING ]] || echo "license file:     NONE"
echo "readme:           $(git ls-files 'README*' | head -1 | grep . || echo 'NONE')"
echo "tracked images:   $(git ls-files '*.png' '*.jpg' '*.gif' | cnt)"
echo "tracked audio:    $(git ls-files '*.wav' | cnt)"
echo "tracked logs:     $(git ls-files '*.log' '*.out' '*.err' | cnt)"
echo "go.mod module:    $(grep -m1 '^module' go.mod)"
# The THREE populations, printed as three numbers and NEVER summed (the rule
# each one applies is stated in the classifier block above the helpers).
echo "hardcoded /home/<user> — three separate populations (never summed):"
echo "  ① all tracked files carrying it:        $(git grep -l "$HOME" -- . 2>/dev/null | cnt)"
echo "  ② production .go CODE occurrences:      $(hygiene_code_files HEAD '*.go' ':(exclude)*_test.go' ":(exclude)$HYGIENE_POC_DIR/*") files / $(hygiene_code_lines HEAD '*.go' ':(exclude)*_test.go' ":(exclude)$HYGIENE_POC_DIR/*") lines   [instrument: hygiene_code_files/hygiene_code_lines, comment lines dropped by $HOME_COMMENT_ERE]"
echo "     ...production .go, ALL lines:        $(git grep -l "$HOME" -- '*.go' ':(exclude)*_test.go' ":(exclude)$HYGIENE_POC_DIR/*" 2>/dev/null | cnt) files — the difference is the kept PROSE"
echo "  ③ tracked non-.go residue:              $(git grep -l "$HOME" -- ':(exclude)*.go' ":(exclude)$HYGIENE_POC_DIR/*" 2>/dev/null | cnt) files (scripts / reports / fixtures, NO comment syntax to classify)"
echo "     POC bucket ($HYGIENE_POC_DIR, NOT counted in ①-③ above): $(hygiene_poc_files HEAD) files carry it"
echo "CI workflows:     $(git ls-files "$CI_WORKFLOWS_GLOB" | cnt)"
echo "secrets scan:     $(git grep -lE "$SECRETS_ERE" 2>/dev/null | cnt) files with a key-shaped string"
echo "--- governance ---"
# The corpus gap is a SET comparison, and the shell's own line readings of the
# two sides disagree unless both are normalised to PATH-ORDER first: an empty
# `comm` in BOTH directions is the only honest "no gap". Two questions are
# asked separately, because they have different denominators — a .md file with
# no row (unclassified) and a row naming no path on disk (stale).
DOC_TMP="${GOTMPDIR:-$TMPROOT_DEFAULT}"
mkdir -p "$DOC_TMP" 2>/dev/null
DOC_FILES_LIST="$DOC_TMP/docfiles-$$.txt"
DOC_ROWS_LIST="$DOC_TMP/docrows-$$.txt"
DOC_ALL_LIST="$DOC_TMP/docall-$$.txt"
git ls-files "$DEVDOC_DIR/$DOC_GLOB" | sort > "$DOC_FILES_LIST"
git ls-files "$DEVDOC_DIR" | sort > "$DOC_ALL_LIST"
# An untracked devdoc (one just written, not yet `git add`ed) is on disk and IS
# part of the corpus, so it counts here — otherwise a snapshot run inside a
# worktree reports its own new document as "a row naming no file on disk".
git ls-files --others --exclude-standard "$DEVDOC_DIR" | sort >> "$DOC_ALL_LIST"
sort -u -o "$DOC_ALL_LIST" "$DOC_ALL_LIST"
awk -F'\t' '!/^#/ && $1 ~ /^devdoc\/.*\.md$/{print $1}' "$DEVDOC_INDEX_FILE" | sort > "$DOC_ROWS_LIST"
printf 'devdoc truth:     %s rows classify %s of %s markdown files (%s unclassified, %s rows name no file on disk)\n' \
  "$(grep -vc '^#' "$DEVDOC_INDEX_FILE")" \
  "$(comm -12 "$DOC_FILES_LIST" "$DOC_ROWS_LIST" | cnt)" \
  "$(git ls-files "$DEVDOC_DIR/$DOC_GLOB" | cnt)" \
  "$(comm -23 "$DOC_FILES_LIST" "$DOC_ROWS_LIST" | cnt)" \
  "$(comm -13 "$DOC_ALL_LIST" <(awk -F'\t' '!/^#/ && $1 ~ /^devdoc\//{print $1}' "$DEVDOC_INDEX_FILE" | sort) | cnt)"
command rm -f "$DOC_FILES_LIST" "$DOC_ROWS_LIST" "$DOC_ALL_LIST"
echo "standards gate files:    $(git ls-files "$STANDARDS_GLOB" | cnt)"
echo "heavy-test inventory:    $(grep -vc '^#' "$HEAVY_INVENTORY_FILE") rows"

# --- public-facing summary ---------------------------------------------------
hdr "summary (for a reader meeting the project)"
# FIRST require block = the direct deps; the rest are pinned transitively for
# offline builds. A bare `grep -c` over the whole file counts all 28 (the
# earlier snapshot's "2" was right for its base; this counts by block).
DIRECT_DEPS=$(awk '/^require \(/{inb=1;next} /^\)/{inb=0} inb && /^[[:space:]]+[a-z]/ && $0 !~ /indirect/ {print $1}' go.mod)
echo "what it is:        one Go binary, zero CGO, $(printf '%s\n' "$DIRECT_DEPS" | cnt) direct deps ($(grep -cE '^\s+[a-z0-9./-]+ v' go.mod | tr -d ' ') pinned in go.mod)"
echo "size:              $PROD_FILES production files / $PROD_LOC LOC / $PROD_FUNCS functions in $PKG_COUNT packages"
echo "verification:      $TEST_FILES test files / $TEST_LOC LOC (ratio $(awk -v t="$TEST_LOC" -v p="$PROD_LOC" 'BEGIN{printf "%.2f", t/p}')), $(git ls-files "$STANDARDS_GLOB" | cnt) standards gate files"
echo "documentation:     $MD_FILES markdown files / $MD_LOC LOC (ratio $(awk -v d="$MD_LOC" -v p="$PROD_LOC" 'BEGIN{printf "%.2f", d/p}'))"
echo "history:           $(git rev-list --count HEAD) commits since $(git log --reverse --format=%cs | head -1), $(git log --merges --oneline | cnt) merges"
echo "surface:           $(printf '%s\n' "$TOOLS" | cnt) LLM tool names, $(printf '%s\n' "$VERBS" | cnt) CLI verbs, $(printf '%s\n' "$TASK_SUB" | cnt) task subcommands, $(printf '%s\n' "$ENV_VARS" | cnt) env vars"
echo "direct deps:       $(printf '%s\n' "$DIRECT_DEPS" | tr '\n' ' ')"
# What the code actually imports, which is the number an outsider can compare:
# count DISTINCT third-party roots across production files. The module path
# (github.com/nickhuang99/gopi) is this repo, not a dependency, so it is dropped.
THIRD_PARTY=$(git grep -hE "$THIRD_PARTY_PREFIX_RE" -- '*.go' ':(exclude)*_test.go' 2>/dev/null | tr -d ' \t"' | sed 's/".*//' | grep -v '^github.com/nickhuang99/gopi' | sed 's#\(^[^/]*/[^/]*\).*#\1#' | sort -u)
echo "imported 3rd-party roots (production): $(printf '%s\n' "$THIRD_PARTY" | cnt) — $(printf '%s\n' "$THIRD_PARTY" | tr '\n' ' ')"

# --- working tree & recent activity -------------------------------------------
# The snapshot runs INSIDE a linked worktree, so relative revisions are measured
# against the tip the worktree carries; the sweep is bounded by a timeout so a
# build-hook sweep can never hang the generator.
hdr "working tree & recent activity"
echo "git status --porcelain: $(git status --porcelain | cnt) entries"
if [[ -x "bin/$BIN_NAME" ]]; then
  echo "worktree sweep:   $(timeout "$SWEEP_BIN_TIMEOUT_SEC" "./bin/$BIN_NAME" task sweep-worktrees 2>&1 | tail -3 | tr '\n' ' ')"
else
  echo "worktree sweep:   (skipped — no ./bin/$BIN_NAME in this worktree; run: CGO_ENABLED=0 go build -o bin/$BIN_NAME $AGENT_PKG)"
fi
echo "commits, last $RELATIVE_COMMITS:"
git log -"$RELATIVE_COMMITS" --format='  %h %cs %s' | cut -c1-96

echo
echo "(end of snapshot — every number above was measured, none typed)"
