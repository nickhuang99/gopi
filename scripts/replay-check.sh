#!/usr/bin/env bash
# replay-check.sh — the CLAIM CHECKPOINT for the replay/film family
# (`gopi chat --replay SESSION --film DIR`).
#
# WHY A SCRIPT AND NOT A PARAGRAPH. A devdoc that states "every IHDR is
# 0x2c2 × 0x156 = 706×342 px", "17 frames, 17 distinct md5", "verify.py is 1609
# lines" makes claims a later commit invalidates SILENTLY: nothing fails a build
# and the reader who trusts the number has no instrument. That is the failure
# `scripts/capdemo-external-check.sh` records ("Nothing failed a build. This
# script is what fails."), and this is the same checkpoint applied to the
# replay/film docs.
#
# WHAT MAKES IT AN ANCHOR RATHER THAN A COPY. `recorded` is EXTRACTED FROM THE
# DOC'S OWN LINE, never typed into this script: the script locates the sentence
# that publishes the claim, parses the value out of it, measures the artifact,
# and compares the two. So editing the document is what makes this gate red —
# which is the whole point, and is the difference between an anchor and a second
# copy of the number waiting to drift on its own.
#
# WHAT IT IS: a READ-ONLY audit. It writes no file, creates no scratch
# directory, runs no film, and generates no load (GOPI.md Rule 27). It reads the
# docs, the evidence artifacts those docs name, the repository, and the built
# binary's own help text. It touches the network never. The only processes it
# starts are `git`/`od`/`stat`/`awk`/`md5sum` and the binary under audit with
# `-h`/`--help` or a deliberately refused argument.
#
# USAGE
#   bash scripts/replay-check.sh                       # from the repo root
#   bash scripts/replay-check.sh ./bin/gopi            # pin the binary it probes
#   bash scripts/replay-check.sh ./bin/gopi DIR        # + the evidence root
#   bash scripts/replay-check.sh --list                # the checks and their commands
# Exit status: 0 = every anchor resolves (UNREACHABLE rows do NOT fail it);
#              1 = at least one anchor is STALE, or the object store could not be
#                  read at all (BROKEN).
# Every line is `recorded=… measured=…`, and a STALE line names the file:line the
# number was PUBLISHED at.
#
# THREE OUTCOMES, NOT TWO — a check that cannot be MEASURED is neither a pass nor
# a drift. This is the defect this script paid for in a clone ground (measured
# 2026-10-27): a clone-channel ground fetches `refs/heads/*` ONLY, so every object
# a doc pins by TAG, by a rewritten head, or by an old commit is legitimately
# absent from it — and `git cat-file -e 429ac774` answering `fatal: Not a valid
# object name` was being read as "the doc drifted". Ten rows reported STALE and
# the run exited 1 for a store limitation, while the same script in the main repo
# measured nine of them green. Worse, the absent object was silently folded into
# a MEASUREMENT: `git show <absent>:<path> | wc -l` is 0, so C12a printed
# `OK recorded=0 importers / 0 entries measured=0 importers / 0 entries` about a
# revision it never read, and C7a printed `before=0`. A skipped check must be
# reported, never swallowed (Rule 15).
#   OK           the anchor was measured and holds
#   STALE        the anchor was measured and no longer holds  → exit 1
#   UNREACHABLE  this ground does not hold the object(s) the anchor is pinned to,
#                so the anchor was NOT MEASURED. git's own words are printed. It
#                is NOT drift and does NOT fail the run: the anchor is not wrong,
#                it is unmeasurable HERE. Measure it in a repository that holds
#                the object (the project's main working repo). NOTE: `git fetch
#                --tags` is NOT that remedy in a clone ground — measured
#                2026-10-27, `git ls-remote --tags origin` there prints zero
#                lines, so the pinned object is not advertised at all.
#   BROKEN       the object store answered with something that is neither "have
#                it" nor "do not have it" — an unreadable store is a failure.
# The count of UNREACHABLE rows is PRINTED in the summary, and a run with any
# reports which anchors it did not measure, so "0 stale" can never quietly mean
# "half the anchors were never read".
#
# THE CHECKS:
#   C1   frame geometry (0x2c2 × 0x156; 0x25c × 0x18a) vs the frames on disk
#   C2   the published frame counts and distinct-frame counts
#   C3   the published byte / row / entry counts of the film's artifacts, and
#        the grabs/stale aggregate over the film directories
#   C4   the published beat-type histogram
#   C5   the `-h` discoverability state — a RECORDED OPEN DEFECT owned by task
#        01a10351, REPORTED with its owner, never this gate's red
#   C6   the `gopi chat` usage line a doc quotes verbatim
#   C7   the doc's own help.go verb census (45→44) and its help-index row count
#   C8   every commit the docs pin resolves; the deleted branch is gone and its
#        rework-backup tag names the tip
#   C9   the published diff shortstats
#   C10  the deleted files' line counts, and "go.mod/go.sum are not in the diff"
#   C11  the published chat flag count and help-group row total, and the
#        geometry→pixel table the help page renders
#   C12  the film package's wiring at the doc's own base (§7/B1)
#   C13  the `gopi chat --film` preflight refusal and its exit code
#   C14  every flag a doc's command examples use is accepted, and mentioned
#   C15  the port census figures a sibling devdoc publishes
#   C16  the PIXELS of the declared frames — the tallest full-width same-colour
#        run and the modal colour's share. Every other criterion here reads a
#        NUMBER (geometry, count, md5, bytes, rc), and all of them stay green on
#        a frame whose picture is destroyed: HP measured 32.9% of one film's
#        frames carrying a full-width pure-black band across the face while this
#        script's number anchors were all satisfied. This is the layer that reads
#        a pixel.
#
# BYPASS, per LINE and counted in the output. An anchor whose expectation a doc
# legitimately records as historical (a superseded number, a row reporting an
# older head) carries the token `REPLAY-CHECK-HISTORICAL` ON THAT SAME LINE.
# Per-line, never per-file: the file stays under audit and every other anchor in
# it still fails. The number of clamped lines is PRINTED, because an exemption
# that grows quietly is exactly what Rules 1/15 forbid.
#
# A PITFALL THIS SCRIPT PAID FOR (measured while building it): with `set -o
# pipefail`, `printf '%s' "$x" | grep -q PATTERN` returns 141 (SIGPIPE) the
# moment grep matches and exits, so `|| fallback` fires on a SUCCESSFUL match and
# the value is reported as absent. Every containment test here is a
# `case "$x" in *"$pat"*)` instead, and no value is fed to a short-circuiting
# reader through a pipeline.
#
# Rule 1: every literal that carries meaning is a named constant below.
set -uo pipefail

# --- constants ---------------------------------------------------------------
REPO_ROOT_SENTINEL='scripts/replay-check.sh'
DOC_ACCEPTANCE='devdoc/chat-film-replay-acceptance.md'
DOC_DRIVER='devdoc/chat-film-replay-driver.md'
DOC_ASSEMBLY='devdoc/film-assembly-and-verification.md'
DEFAULT_BIN='./bin/gopi'
TASK_EVIDENCE='01a1003a-7e74-7dee-aec2-51aeb71bff18'
DEFAULT_EVIDENCE="$HOME/.gopi/sessions/$TASK_EVIDENCE/evidence"

# The ONE bypass token (per line).
HISTORICAL_TOKEN='REPLAY-CHECK-HISTORICAL'

# --- reachability (the three-outcome rule; see the header) -------------------
# `git cat-file -e <rev>` exit statuses. 0 = the object is here; 128 = git
# resolved the spelling and reported it does not hold it; anything else is the
# store itself failing (an unreadable repo, a corrupt pack) and is BROKEN.
CAT_FILE_HAVE_RC=0
CAT_FILE_ABSENT_RC=128
UNREACHABLE_MARK='UNREACHABLE'
BROKEN_MARK='BROKEN'
# The remedy each UNREACHABLE row prints: a repository that holds the object is
# the only place the anchor can be judged. NOT `git fetch --tags` — measured
# 2026-10-27 in a clone ground: `git ls-remote --tags origin` prints ZERO lines,
# so the object the doc pins is not advertised there at all and no tag fetch
# reaches it. A remedy that does not work is worse than none.
UNREACHABLE_REMEDY="judge it in a repository that holds the object (the project's main working repo) — this store was not asked to carry it: it fetched refs/heads/* only, and its origin advertises no tag refs (git ls-remote --tags origin prints nothing)"

# The evidence sub-directory and artifact names the docs cite (one spelling each).
EV_FILM='film'
EV_FILM_KEPT='film-kept'
EV_FILM_KEPT_FILM="$EV_FILM_KEPT/film"
EV_FILM_KEPT_SCRATCH="$EV_FILM_KEPT/scratch"
EV_DUMPS='dumps'
EV_FILM_TOOLS='film-tools'
EV_FILM_SAMEFREQ='film-samefreq'
EV_FILM_DUMP='film-dump'
EV_FILM_SHIM='film-shim'
EV_FILM_SHIM2='film-shim2'
EV_ALL_DIRS="$EV_FILM $EV_FILM_DUMP $EV_FILM_KEPT_FILM $EV_FILM_SAMEFREQ $EV_FILM_SHIM $EV_FILM_SHIM2 $EV_FILM_TOOLS"
ART_FRAMES='frames.tsv'
ART_BEATS='beats.tsv'
ART_PRODUCER='producer.log'
ART_TERMINAL='terminal.log'
ART_ACK='ack'
ART_DONE='done.txt'
ART_REDACTIONS='redactions.tsv'
ART_XVFB_LOG='xvfb-63.log'
FRAME_GLOB='frame-*.png'
FIRST_FRAME='frame-0001.png'

# C1 — the geometry anchors, keyed by the hex pair the doc publishes.
GEOM_DEFAULT_HEX='0x2c2 × 0x156'
GEOM_SAMEFREQ_HEX='0x25c × 0x18a'
GEOM_DEFAULT_SPEC='117x26'
GEOM_SAMEFREQ_SPEC='100x30'
IHDR_OFFSET=16
IHDR_BYTES=8
# The extraction regexes (the doc's own spellings).
RE_HEX_PAIR='0x[0-9a-f]+ × 0x[0-9a-f]+'
RE_INT='[0-9]+'
RE_BYTES='[0-9]+ bytes'
RE_INT_SLASH='bytes [0-9]+(/[0-9]+)+'
RE_HEX_TOKENS='0x[0-9a-f]+'
# The `×` the docs use between dimensions, and the `x` this script prints.
DIM_SEP_DOC='×'
DIM_SEP_OUT='x'
PX_SUFFIX='px'

# C2/C3 — the doc's own key phrases, one per claim.
PH_FRAMES_DISTINCT='| distinct md5 |'
PH_FRAMES_DISTINCT_TOOLS='17 distinct md5'
PH_FRAMES_KEPT='6 × frame-000N'
PH_PRODUCER='producer.log` | 16431'
PH_TERMINAL='terminal.log` | 0 bytes'
PH_ACK='ack=2B'
PH_DONE='done.txt=2B'
PH_BEATS_BYTES='beats.tsv=316B'
PH_REDACTIONS='redactions.tsv=135B'
PH_XVFB_LOG='xvfb-63.log=0B'
PH_FRAMES_TABLE='frames.tsv` | 6 rows'
PH_DUMPS='dumps/=6'
PH_AGGREGATE='2/0 × 52 frames'
RE_FRAMES_AT_2='2/0 × [0-9]+ frames'
RE_FRAMES_AT_3='3/1 × [0-9]+ frame'

# C4 — the histogram's key phrase and its per-type shape.
PH_BEATS_HISTO='beats.tsv` | 6 rows'
HISTO_PREFIX='rows: '

# C5 — the open `-h` defect, its exit codes and its owner.
# The FULL row id: the short form resolves to nothing in this ledger.
HELP_DEFECT_OWNER='01a10351-6fae-7905-8255-7a76b5d2c0b0'
# Measured 2026-10-04: that row reads `completed`, while C5/C14 still measure
# the defect — a closed row whose defect outlived it is exactly what this line is
# here to make visible.
HELP_DEFECT_OWNER_STATUS='completed'
REPLAY_HELP_RC=2
CHAT_HELP_RC=0

# C6/C7/C11/C14 — the help surface.
HELP_SOURCE='internal/config/help.go'
HELP_PREIMAGE='971400a7^'
PH_VERB_CENSUS='before=45 after=44'
PH_HELP_INDEX='prints **57**'
RE_VERB_CENSUS='before=[0-9]+ after=[0-9]+'
RE_HELP_INDEX='\*\*[0-9]+\*\*'
HELP_SCRIPT='scripts/g5-chat-web-help-surface.sh'
PH_CHAT_FLAG_ROW='| chat-ideal-geometry, film'
PH_GROUP_TOTAL='with the whole derived group'
RE_GROUP_TOTAL='\*\*[0-9]+\*\*'
# PIXEL_HELP_ARGV is the page that PUBLISHES the two pixel figures C11c/C11d
# compare against: the replay/film family's own scoped page. The session page
# names it and no longer repeats the arithmetic (chat_replay_help_scope.go).
PIXEL_HELP_ARGV='chat --replay -h'
PH_PIXEL_DEFAULT='706×342 px'
PH_PIXEL_SAMEFREQ='604×394 px'
RE_PX_DOC='[0-9]+×[0-9]+ px'
RE_DIMS_DOC='[0-9]+×[0-9]+'
RE_PX_OUT='[0-9]+x[0-9]+px'
DOC_EXAMPLE_FLAGS='--replay --film --scratch --cols --rows --out --chat-ideal-geometry'
UNKNOWN_FLAG_MARKER='unknown flag'
COMMAND_INDEX_PATTERN='^  gopi '
FILM_COMMAND_PATTERN='^  gopi (replay|film)([ <]|$)'

# C8/C9/C10 — the pinned history and its published arithmetic.
BASE_DOC_CLAIM_REV='429ac774'
MOVING_REF='master'
PINNED_COMMITS='971400a7 c8e71550 02c1e523 b191a55d 429ac774 44303fbb 0b516f5f'
REWORK_TAG='refs/tags/rework-backup/task-01a0ffbba7457cb7a1b43db95617ea8e'
REWORK_TAG_SHORT='0b516f5f'
DEAD_BRANCH='refs/heads/task/01a0ffbba7457cb7a1b43db95617ea8e'
DEAD_BRANCH_RC=1
PH_TASK_LINE='**Task** `01a1003a'
STAT_BASE='c8e71550'
STAT_DRIVER_COMMIT='02c1e523'
STAT_DRIVER_MERGED='b191a55d'
STAT_ABLATION_COMMIT='971400a7'
PH_STAT_DRIVER='c8e71550` → `02c1e523'
PH_STAT_MERGED='c8e71550` → `b191a55d'
PH_STAT_MASTER='c8e71550` → `master'
PH_STAT_ABLATION='37 files, +188 / −8684'
RE_SHORTSTAT='[0-9]+ files changed, \+[0-9]+ / −[0-9]+'
RE_SHORTSTAT_TAIL='[0-9]+ files, \+[0-9]+ / −[0-9]+'
PH_PREIMAGE_PROSE='lines counted from the pre-image'
PH_MODULE_FILES='are **not** in the diff'
DEL_REPLAY_GO='internal/replay/replay.go'
DEL_CLI_REPLAY='cmd/agent/cli_replay.go'
DEL_VERIFY_PY='tools/replay/verify.py'
DEL_TERMEMU_PY='tools/termemu/termemu.py'
DEL_REPLAY_SH='scripts/replay/replay.sh'
GOMOD='go.mod'
GOSUM='go.sum'
RE_BOLD_COUNT='\*\*[0-9]+\*\*'
DRIVER_GO='cmd/agent/chat_film.go'
PH_DRIVER_LINES='578 lines'

# C12/C15 — the film package and the port census.
FILM_PKG='internal/film'
FILM_IMPORT_SENTINEL='gopi/internal/film'
PH_FILM_UNWIRED='nothing imports'
PH_FILM_UNWIRED_FALLBACK='does not exist on master'
PH_VERIFY_LINES='(1949 lines, of which the five checks are'
RE_VERIFY_LINES='[0-9]+ lines, of which the five checks are'

# C13 — the film preflight refusal.
UNWRITABLE_DIR='/proc/gopi-no-such-dir'
FILM_REFUSAL_HINT='--replay SESSION --film DIR'
FILM_REFUSAL_RC=2

# C16 — the PIXELS of the declared frames. Every other criterion in this file
# reads a NUMBER (geometry, count, md5, bytes, rc) and a destroyed picture
# satisfies all of them: HP measured 32.9% of one film's frames carrying a
# full-width pure-black band across the face while this script was green.
#
# THE SEPARATOR IS THE PAIR (height, colour identity) — NOT THE HEIGHT. A printed
# frame is ink on a ground, so a full-width GROUND band between blocks of text is
# normal: a healthy film here carries a full-width run of 111 px (the declared
# ground rgb(19,25,38) filling the gap between the banner and the first tip
# block), so a bare height threshold would have flagged healthy films. In the same
# population a full-width run in a colour that is NOT the frame's modal colour
# NEVER EXCEEDED 6 px. The thresholds below are those measurements, not
# preferences (Rule 38 — a census figure names its generator and revision):
#
#   generator: REPLAY_PIXEL=scripts/replay-pixel.py \
#                python3 <session>/evidence/census.py <film-dir>...
#              (a bounded list of directories; the same sweep also runs a bare
#               root, which takes minutes rather than seconds)
#   measured at: master ba55d0b5, 2026-10-04
#   population:  1763 frames in 26 film directories (evidence/c16-census.txt)
#     non-modal full-width run   max  6 px   -> C16_BAND_MIN_ROWS sits 4x above it
#     modal-colour full-width run max 521 px (a WHOLE-frame run: the census also
#       found two 100%-black, single-colour frames in a 91-frame film, i.e. the
#       blank class is measured, not hypothetical)
#     modal share                74.1 .. 93.7% healthy (the declared 52-frame set
#       reads 92.3-93.7%); 100.0% for the black frames above
C16_PIXEL_PY_REL='scripts/replay-pixel.py'
C16_BAND_MIN_ROWS=24   # a band ACROSS the ground. Census: the tallest NON-modal
                       # full-width run over 2606 frames = 6 px; the measured
                       # defect (a 64 px black bar) is 10x above it, this floor
                       # sits 4x above the tallest healthy reading.
                       # WHY THERE IS NO SHARE CONDITION ON THIS LEG (measured, not
                       # preferred): a k-pixel full-width band occupies k/H of the
                       # frame's pixels, so it CAPS the modal colour's share at
                       # 100 - 100k/H — at k = 24 and H = 342 that is 93%, i.e. the
                       # band is always a minority of the frame by construction, and
                       # a share threshold here would be a condition that can never
                       # change a verdict (Rule 1: a constant nothing can act on is
                       # a literal in disguise). The colour-identity leg is what
                       # separates a stripe from the ground, and the blank leg below
                       # is what catches the equal-colour case.
C16_SHARE_BLANK_PCT=97 # the frame IS one colour — any colour, dark included
                       # (near-blank), which is why an all-dark frame is flagged
                       # while a dark ground on its own is not a defect. Census:
                       # the highest modal share ANY healthy frame reached is
                       # 93.7% (film-tools), so the floor sits 3.3 points above
                       # it; a frame this uniform is blank in fact, not in colour.
C16_SHARE_MIN_PCT=50   # the healthy floor: every real film's modal share is
                       # >= 80% (dark ground 87.9%, light 91-94%).
# The declared film's own expected readings, re-measured every run so the margin
# is visible rather than trusted (the doc publishes its frames at DOC_ACCEPTANCE).
C16_DECLARED_DIR="$EV_FILM"
C16_RUN_PX_LO=1
C16_RUN_PX_HI=120
C16_SHARE_PCT_LO=80
C16_SHARE_PCT_HI=99
# The reader's own output shape (TAB separated, defined once in the reader).
C16_ROW_DIR='dir'

# --- argument handling -------------------------------------------------------
BIN="$DEFAULT_BIN"
EVIDENCE="$DEFAULT_EVIDENCE"
LIST_ONLY=0
case "${1:-}" in
    --list|-l) LIST_ONLY=1 ;;
    "") ;;
    *) BIN="$1" ;;
esac
if [ "${2:-}" != "" ]; then
    EVIDENCE="$2"
fi

# --- output helpers ----------------------------------------------------------
STALE=0
NOTES=0
CLAMPED=0
CHECKED=0
UNREACH=0
BROKEN=0

stale() { # stale <check> <claim> <recorded> <measured> <published-file:line>
    STALE=$((STALE + 1))
    printf 'STALE  %s: recorded=%s measured=%s (published in %s)\n' "$2" "$3" "$4" "$5"
}

# --- reachability helpers (see the header's three-outcome rule) --------------
# git_has <rev> → rc 0 this store holds it; 1 it does not; 2 the store failed.
# GIT_HAVE_RAW carries git's OWN words, so an UNREACHABLE row QUOTES the tool
# instead of paraphrasing it (a paraphrase would be a claim about git).
git_has() {
    local out rc
    out="$(git cat-file -e "$1" 2>&1)"
    rc=$?
    GIT_HAVE_RAW="${out:-}"
    GIT_HAVE_RC="$rc"
    if [ "$rc" = "$CAT_FILE_HAVE_RC" ]; then
        return 0
    fi
    if [ "$rc" = "$CAT_FILE_ABSENT_RC" ]; then
        return 1
    fi
    return 2
}

# revs_readable <check> <claim> <revs…> → rc 0 when EVERY rev is in this store.
# Otherwise it prints ONE row and returns 1, and the caller must NOT measure:
# the whole point is that an absent object piped through `git show … | wc -l`
# produces a NUMBER (0), and that number was being judged as if it had been read
# (measured 2026-10-27: C12a printed an OK about a revision this ground lacks).
revs_readable() {
    local name="$1" claim="$2" revs="$3" rev raw="" rc
    for rev in $revs; do
        git_has "$rev"
        rc=$?
        case "$rc" in
            0) ;;
            1) raw="$raw; $rev: ${GIT_HAVE_RAW:-exit $GIT_HAVE_RC}" ;;
            *)
                BROKEN=$((BROKEN + 1))
                printf 'BROKEN %s the object store failed on %s: %s\n' "$name" "$rev" "${GIT_HAVE_RAW:-exit $GIT_HAVE_RC}"
                printf '       %s the store answered neither "have it" nor "do not have it" — this is NOT a doc verdict\n' "$name"
                return 1
                ;;
        esac
    done
    if [ "$raw" != "" ]; then
        UNREACH=$((UNREACH + 1))
        printf 'UNREACHABLE %s %s — this ground does not hold:%s\n' "$name" "$claim" "${raw#;}"
        printf '       %s not measured here, so no verdict; remedy: %s\n' "$name" "$UNREACHABLE_REMEDY"
        return 1
    fi
    return 0
}

note() { NOTES=$((NOTES + 1)); printf 'NOTE   %s\n' "$1"; }

ok() { # ok <check> <recorded> <measured> <published>
    CHECKED=$((CHECKED + 1))
    printf 'OK     %s recorded=%s measured=%s (%s)\n' "$1" "$2" "$3" "$4"
}

skip() { note "$1 not measured: $2"; }

# anchor_file <fixed-string> <doc> → "<doc>:<line>" of its first occurrence.
# -F, not -E: the anchors are literal text (some carry non-ASCII), and a pattern
# that silently failed to match would HIDE an anchor rather than report it.
anchor_file() {
    local pat="$1" f="$2" ln
    ln="$(grep -nF -- "$pat" "$f" 2>/dev/null | head -1 | cut -d: -f1)"
    printf '%s:%s' "$f" "${ln:-0}"
}

# doc_token <file> <file:line> <ERE> → the FIRST match on that line, or "".
# This is the extraction half of every value anchor: what the DOC publishes.
doc_token() {
    local f="$1" ln="${2##*:}" pat="$3"
    [ "$ln" != "0" ] || { printf ''; return 0; }
    sed -n "${ln}p" "$f" 2>/dev/null | grep -oE "$pat" | head -1
}

# doc_ints <file> <file:line> <ERE> → the integers inside the first match,
# space-separated, so a published "6 files changed, +2003 / −2" reads "6 2003 2".
doc_ints() {
    printf '%s' "$(doc_token "$1" "$2" "$3")" | grep -oE "$RE_INT" | paste -sd' '
}

# need <check> <what> <extracted> <loc> → rc 1 (and a STALE) when the doc no
# longer publishes a parseable value. A claim whose number cannot be found is a
# FAILED anchor, never a silent pass (the capdemo A3 precedent).
need() {
    local name="$1" what="$2" val="$3" loc="$4"
    if [ "$val" != "" ]; then
        return 0
    fi
    STALE=$((STALE + 1))
    printf 'STALE  %s the doc no longer publishes a parseable %s (published in %s)\n' "$name" "$what" "$loc"
    return 1
}

# clamped <file> <file:line> → rc 0 when that line carries the bypass token.
clamped() {
    local f="$1" ln="${2##*:}"
    [ "$ln" != "0" ] || return 1
    sed -n "${ln}p" "$f" 2>/dev/null | grep -qF "$HISTORICAL_TOKEN"
}

# judge <check> <claim> <recorded> <measured> <file> <file:line>
# The ONE place that decides between "this anchor is clamped as history" and
# "this anchor is stale", so no check can forget the bypass or forget to count it.
judge() {
    local name="$1" claim="$2" rec="$3" meas="$4" f="$5" spec="$6"
    if [ "$rec" = "$meas" ]; then
        ok "$name" "$rec" "$meas" "$spec"
        return 0
    fi
    if clamped "$f" "$spec"; then
        # A clamped line is still PRINTED, with both values: the token pins a
        # change that cannot be made from here (a figure measured at another
        # base), it does not hide the drift behind a counter.
        CLAMPED=$((CLAMPED + 1))
        printf 'CLAMP  %s: recorded=%s measured=%s (%s carries %s)\n' "$name" "$rec" "$meas" "$spec" "$HISTORICAL_TOKEN"
        return 0
    fi
    stale "$name" "$claim" "$rec" "$meas" "$spec"
}

# hist_canon <"4 × `request`, 1 × `answer`"> → "answer=1 chrome=1 request=4".
# A histogram is a MULTISET of type→count pairs, so the comparison is made on
# the canonical form: the order a doc lists the types in is not part of the claim.
hist_canon() {
    tr ',' '\n' <<<"$1" | tr -d '`' | sed 's/^ *//;s/ *$//' |
        awk -F' × ' '{ if ($2 != "" && $2 != "rows:") printf "%s=%s\n", $2, $1 }' |
        sort | paste -sd' '
}

# hex_pair_to_dims <"0x2c2 × 0x156"> → "706x342".
hex_pair_to_dims() {
    local a="${1%% × *}" b="${1##* × }"
    printf '%d%s%d' "$((a))" "$DIM_SEP_OUT" "$((b))"
}

# px_doc_to_out <"706×342 px"> → "706x342px". The dims are lifted by the SAME
# regex the extraction used, so a change in the doc's spelling shows up as an
# empty value (reported by need), never as a silently mangled number.
px_doc_to_out() {
    local dims
    dims="$(printf '%s' "$1" | grep -oE "$RE_DIMS_DOC" | head -1)"
    [ "$dims" != "" ] || return 0
    # sed, NOT tr: `×` is multibyte and tr would emit one `x` per BYTE (measured:
    # "706×342" → "706xx342").
    printf '%s%s' "$(printf '%s' "$dims" | sed "s/$DIM_SEP_DOC/$DIM_SEP_OUT/")" "$PX_SUFFIX"
}

# px_present answers which figure to report for a recorded one: the recorded value
# itself when the page prints it (identity, insensitive to the page's line order),
# otherwise the first figure the page DOES print, so the STALE line shows the
# reader what the binary derives now.
px_present() {
    local page="$1" want="$2" first
    first="$(printf '%s' "$page" | grep -oE "$RE_PX_OUT" | sed -n 1p)"
    case "$page" in
        *"$want"*) printf '%s' "$want" ;;
        *) printf '%s' "$first" ;;
    esac
}

# --- preflight ---------------------------------------------------------------
if [ ! -f "$REPO_ROOT_SENTINEL" ]; then
    printf 'replay-check: run me from the repository root (no %s here) — nothing measured\n' "$REPO_ROOT_SENTINEL"
    exit 1
fi
for d in "$DOC_ACCEPTANCE" "$DOC_DRIVER" "$DOC_ASSEMBLY"; do
    [ -f "$d" ] || { printf 'replay-check: %s is missing — the audit has no subject\n' "$d"; exit 1; }
done

if [ "$LIST_ONLY" = "1" ]; then
    cat <<'EOF'
replay-check.sh — the checks and the commands each one runs (all read-only):

  C1  frame geometry vs the docs    od -An -tu1 -j16 -N8 <frame> → IHDR W/H
  C2  frame / distinct counts       ls frame-*.png | wc -l ; md5sum | sort -u | wc -l
  C3  byte / row / entry counts     stat -c %s ; wc -l ; ls dumps/ | wc -l
  C4  beat histogram                awk -F'\t' 'NR>1{n[$2]++} END{...}' beats.tsv
  C5  `--replay -h` state + owner   <bin> chat --replay -h    (rc + first stderr line)
  C6  usage line, verbatim          <bin> chat -h | head -1
  C7  verb census + help index      git show <rev>:internal/config/help.go ; <bin> --help
  C8  pinned commits resolve        git rev-parse --verify --quiet <rev>^{commit}
  C9  diff shortstats               git diff --shortstat <a> <b>
  C10 deleted files' line counts    git show 971400a7^:<path> | wc -l ; git show --name-only
  C11 flag count + pixel table      scripts/g5-chat-web-help-surface.sh <bin> ; <bin> chat -h
  C12 film package wiring           git grep -c 'gopi/internal/film' <rev> -- '*.go'
  C13 film preflight refusal        <bin> chat --film /proc/gopi-no-such-dir   (rc + message)
  C14 command-example flags         <bin> chat <flag> <value> -h   (unknown-flag ⇒ refused)
  C15 port census figures           git show 971400a7^:tools/replay/verify.py | wc -l
  C16 frame pixels (band/share)     python3 scripts/replay-pixel.py <film-dir>

`recorded` is parsed out of the doc line, `measured` out of the artifact.
No check writes anything, creates a directory, starts a film, or generates load.
C16 alone reads no doc figure: it measures a frame's own pixels against the
band criterion below, so it is the one row recorded= is a PROPERTY, not a number.
EOF
    exit 0
fi

printf 'replay-check: auditing the replay/film claims (recorded is parsed from the doc)\n'
printf '  docs:     %s\n            %s\n            %s\n' "$DOC_ACCEPTANCE" "$DOC_DRIVER" "$DOC_ASSEMBLY"
printf '  evidence: %s\n' "$EVIDENCE"
printf '  binary:   %s\n\n' "$BIN"

# =============================================================================
# C1 — frame geometry
# =============================================================================
ihdr_of() { # ihdr_of <png> → "WxH" from the IHDR width/height (big-endian uint32)
    od -An -tu1 -j"$IHDR_OFFSET" -N"$IHDR_BYTES" "$1" 2>/dev/null |
        awk '{ w = $1*16777216 + $2*65536 + $3*256 + $4
               h = $5*16777216 + $6*65536 + $7*256 + $8
               printf "%dx%d", w, h }'
}

c1_geometry() { # c1_geometry <check> <spec> <hex-pair-phrase> <dir>
    local name="$1" spec="$2" phrase="$3" dir="$4" anchor rec got
    anchor="$(anchor_file "$phrase" "$DOC_ACCEPTANCE")"
    rec="$(doc_token "$DOC_ACCEPTANCE" "$anchor" "$RE_HEX_PAIR")"
    need "$name" "IHDR hex pair at $spec" "$rec" "$anchor" || return
    rec="$(hex_pair_to_dims "$rec")"
    if [ ! -f "$dir/$FIRST_FRAME" ]; then
        skip "$name" "$dir/$FIRST_FRAME is not on disk (evidence root: $EVIDENCE)"
        return
    fi
    got="$(ihdr_of "$dir/$FIRST_FRAME")"
    judge "$name($spec)" "the frame geometry the doc publishes at $spec" "$rec" "$got" "$DOC_ACCEPTANCE" "$anchor"
}
c1_geometry C1a "$GEOM_DEFAULT_SPEC" "$GEOM_DEFAULT_HEX" "$EVIDENCE/$EV_FILM"
c1_geometry C1b "$GEOM_SAMEFREQ_SPEC" "$GEOM_SAMEFREQ_HEX" "$EVIDENCE/$EV_FILM_SAMEFREQ"

# =============================================================================
# C2 — frame counts and distinctness
# =============================================================================
# C2a: the doc's "**6 of 6**" — both halves, from the doc.
c2_film() {
    local anchor rec got_n got_d
    anchor="$(anchor_file "$PH_FRAMES_DISTINCT" "$DOC_ACCEPTANCE")"
    rec="$(doc_token "$DOC_ACCEPTANCE" "$anchor" '\*\*[0-9]+ of [0-9]+\*\*' | tr -d '*')"
    need C2a 'frame count and distinct-frame count' "$rec" "$anchor" || return
    if [ ! -d "$EVIDENCE/$EV_FILM" ]; then
        skip C2a "$EVIDENCE/$EV_FILM is not on disk"
        return
    fi
    got_n="$(ls "$EVIDENCE/$EV_FILM"/$FRAME_GLOB 2>/dev/null | wc -l)"
    got_d="$(md5sum "$EVIDENCE/$EV_FILM"/$FRAME_GLOB 2>/dev/null | awk '{print $1}' | sort -u | wc -l)"
    judge C2a 'the film frame AND distinct-frame counts' "$rec" "$got_n of $got_d" "$DOC_ACCEPTANCE" "$anchor"
}
c2_film

# C2b: the doc's "17 frames, **17 distinct md5**".
c2_tools() {
    local anchor rec_n rec_d got_n got_d
    anchor="$(anchor_file "$PH_FRAMES_DISTINCT_TOOLS" "$DOC_ACCEPTANCE")"
    rec_n="$(doc_token "$DOC_ACCEPTANCE" "$anchor" '[0-9]+ frames')"
    rec_d="$(doc_token "$DOC_ACCEPTANCE" "$anchor" '\*\*[0-9]+ distinct')"
    need C2b 'tool-film frame count' "$rec_n" "$anchor" || return
    need C2b 'tool-film distinct count' "$rec_d" "$anchor" || return
    if [ ! -d "$EVIDENCE/$EV_FILM_TOOLS" ]; then
        skip C2b "$EVIDENCE/$EV_FILM_TOOLS is not on disk"
        return
    fi
    got_n="$(ls "$EVIDENCE/$EV_FILM_TOOLS"/$FRAME_GLOB 2>/dev/null | wc -l)"
    got_d="$(md5sum "$EVIDENCE/$EV_FILM_TOOLS"/$FRAME_GLOB 2>/dev/null | awk '{print $1}' | sort -u | wc -l)"
    rec_d="$(printf '%s' "$rec_d" | tr -d '*')"
    rec_n="$(printf '%s' "$rec_n" | tr -d '*')"
    judge C2b 'the tool-bearing film frame AND distinct-frame counts' \
        "${rec_n%% *} frames / ${rec_d%% *} distinct" "$got_n frames / $got_d distinct" "$DOC_ACCEPTANCE" "$anchor"
}
c2_tools

# C2c: the --scratch-kept film's count.
c2_kept() {
    local anchor rec got
    anchor="$(anchor_file "$PH_FRAMES_KEPT" "$DOC_ACCEPTANCE")"
    rec="$(doc_token "$DOC_ACCEPTANCE" "$anchor" '[0-9]+ × frame-000N')"
    need C2c 'the --scratch-kept film frame count' "$rec" "$anchor" || return
    if [ ! -d "$EVIDENCE/$EV_FILM_KEPT_FILM" ]; then
        skip C2c "$EVIDENCE/$EV_FILM_KEPT_FILM is not on disk"
        return
    fi
    got="$(ls "$EVIDENCE/$EV_FILM_KEPT_FILM"/$FRAME_GLOB 2>/dev/null | wc -l)"
    judge C2c 'the --scratch-kept film frame count' "${rec%% *}" "$got" "$DOC_ACCEPTANCE" "$anchor"
}
c2_kept

# =============================================================================
# C3 — byte counts, row counts, entry counts (each parsed from its doc line)
# =============================================================================
# c3_bytes <check> <doc-phrase> <file> <claim> <ERE>
c3_bytes() {
    local name="$1" phrase="$2" f="$3" claim="$4" re="$5" anchor rec got
    anchor="$(anchor_file "$phrase" "$DOC_ACCEPTANCE")"
    rec="$(doc_token "$DOC_ACCEPTANCE" "$anchor" "$re")"
    need "$name" "byte count of $(basename "$f")" "$rec" "$anchor" || return
    if [ ! -f "$f" ]; then
        skip "$name" "$f is not on disk"
        return
    fi
    got="$(stat -c %s "$f")"
    judge "$name" "$claim" "${rec%% *} bytes" "$got bytes" "$DOC_ACCEPTANCE" "$anchor"
}
c3_bytes C3a "$PH_PRODUCER" "$EVIDENCE/$EV_FILM/$ART_PRODUCER" 'the producer.log byte count' "$RE_BYTES"
c3_bytes C3b "$PH_TERMINAL" "$EVIDENCE/$EV_FILM/$ART_TERMINAL" 'the terminal.log byte count' "$RE_BYTES"

# c3_suffix <check> <doc-phrase> <file> <claim> — the `<name>=<N>B` spelling.
c3_suffix() {
    local name="$1" phrase="$2" f="$3" claim="$4" anchor rec got
    anchor="$(anchor_file "$phrase" "$DOC_ACCEPTANCE")"
    rec="$(doc_token "$DOC_ACCEPTANCE" "$anchor" "$(basename "$f")=[0-9]+B")"
    need "$name" "${f##*/} byte count" "$rec" "$anchor" || return
    if [ ! -f "$f" ]; then
        skip "$name" "$f is not on disk"
        return
    fi
    got="$(stat -c %s "$f")"
    judge "$name" "$claim" "$rec" "$(basename "$f")=${got}B" "$DOC_ACCEPTANCE" "$anchor"
}
c3_suffix C3c "$PH_ACK" "$EVIDENCE/$EV_FILM_KEPT_SCRATCH/$ART_ACK" 'the ack byte count'
c3_suffix C3d "$PH_DONE" "$EVIDENCE/$EV_FILM_KEPT_SCRATCH/$ART_DONE" 'the done.txt byte count'
c3_suffix C3e "$PH_BEATS_BYTES" "$EVIDENCE/$EV_FILM_KEPT_SCRATCH/$ART_BEATS" 'the kept-scratch beats.tsv byte count'
c3_suffix C3f "$PH_REDACTIONS" "$EVIDENCE/$EV_FILM_KEPT_SCRATCH/$ART_REDACTIONS" 'the redactions.tsv byte count'
c3_suffix C3g "$PH_XVFB_LOG" "$EVIDENCE/$EV_FILM_KEPT_SCRATCH/$ART_XVFB_LOG" 'the xvfb log byte count'

# C3h: the 6-row frames.tsv table and its per-frame byte list.
c3_frames_table() {
    local anchor rec_rec rec_list got_rec got_list rows_list
    anchor="$(anchor_file "$PH_FRAMES_TABLE" "$DOC_ACCEPTANCE")"
    rec_rec="$(doc_token "$DOC_ACCEPTANCE" "$anchor" '[0-9]+ rows')"
    rec_list="$(doc_token "$DOC_ACCEPTANCE" "$anchor" "$RE_INT_SLASH")"
    need C3h 'frames.tsv row count' "$rec_rec" "$anchor" || return
    need C3h 'frames.tsv per-frame byte list' "$rec_list" "$anchor" || return
    rec_list="${rec_list#bytes }"
    f="$EVIDENCE/$EV_FILM/$ART_FRAMES"
    if [ ! -f "$f" ]; then
        skip C3h "$f is not on disk"
        return
    fi
    rows_list="$(tail -n +2 "$f" | wc -l)"
    got_list="$(tail -n +2 "$f" | cut -f5 | paste -sd/)"
    judge C3h 'the frames.tsv row count and per-frame byte list' \
        "$rec_rec, bytes $rec_list" "$rows_list rows, bytes $got_list" "$DOC_ACCEPTANCE" "$anchor"
}
c3_frames_table

# C3i: the kept-scratch dumps/ entry count.
c3_dumps() {
    local anchor rec dir n
    anchor="$(anchor_file "$PH_DUMPS" "$DOC_ACCEPTANCE")"
    rec="$(doc_token "$DOC_ACCEPTANCE" "$anchor" 'dumps/=[0-9]+')"
    need C3i 'dumps/ entry count' "$rec" "$anchor" || return
    dir="$EVIDENCE/$EV_FILM_KEPT_SCRATCH/$EV_DUMPS"
    if [ ! -d "$dir" ]; then
        skip C3i "$dir is not on disk"
        return
    fi
    n="$(ls "$dir" | wc -l)"
    judge C3i 'the kept-scratch dumps/ entry count' "$rec" "dumps/=$n" "$DOC_ACCEPTANCE" "$anchor"
}
c3_dumps

# C3j: the grab/stale aggregate over the film directories.
c3_aggregate() {
    local anchor rec2 rec3 agg2=0 agg3=0 d n m closed=0
    anchor="$(anchor_file "$PH_AGGREGATE" "$DOC_ACCEPTANCE")"
    rec2="$(doc_token "$DOC_ACCEPTANCE" "$anchor" "$RE_FRAMES_AT_2" | grep -oE "$RE_INT frames" | grep -oE "$RE_INT")"
    rec3="$(doc_token "$DOC_ACCEPTANCE" "$anchor" "$RE_FRAMES_AT_3" | grep -oE "$RE_INT frame" | grep -oE "$RE_INT")"
    need C3j 'grabs=2 frame count' "$rec2" "$anchor" || return
    need C3j 'grabs=3 frame count' "$rec3" "$anchor" || return
    for d in $EV_ALL_DIRS; do
        [ -f "$EVIDENCE/$d/$ART_FRAMES" ] || continue
        closed=$((closed + 1))
        n="$(tail -n +2 "$EVIDENCE/$d/$ART_FRAMES" | awk -F'\t' '$2==2 && $3==0' | wc -l)"
        m="$(tail -n +2 "$EVIDENCE/$d/$ART_FRAMES" | awk -F'\t' '$2==3' | wc -l)"
        agg2=$((agg2 + n)); agg3=$((agg3 + m))
    done
    if [ "$closed" = "0" ]; then
        skip C3j "$EVIDENCE holds none of the film directories"
        return
    fi
    judge C3j 'the grabs/stale aggregate over the film directories the doc counts' \
        "$rec2 frames at 2/0, $rec3 frame at 3/1" \
        "$agg2 frames at 2/0, $agg3 frame at 3/1" "$DOC_ACCEPTANCE" "$anchor"
}
c3_aggregate

# =============================================================================
# C4 — the beat histogram of the filmed session
# =============================================================================
c4_beats() {
    local anchor rec_line rec f hist
    anchor="$(anchor_file "$PH_BEATS_HISTO" "$DOC_ACCEPTANCE")"
    rec_line="$(sed -n "${anchor##*:}p" "$DOC_ACCEPTANCE" 2>/dev/null)"
    rec_line="${rec_line##*$HISTO_PREFIX}"
    rec_line="${rec_line%% |*}"
    need C4 'beat histogram' "$rec_line" "$anchor" || return
    f="$EVIDENCE/$EV_FILM/$ART_BEATS"
    if [ ! -f "$f" ]; then
        skip C4 "$f is not on disk"
        return
    fi
    hist="$(awk -F'\t' 'NR>1 {n[$2]++}
        END {for (t in n) printf "%s × %s, ", n[t], t}' "$f" | sed 's/, $//')"
    judge C4 'the beat-type histogram of the filmed session' \
        "$(hist_canon "$rec_line")" "$(hist_canon "$hist")" "$DOC_ACCEPTANCE" "$anchor"
}
c4_beats

# =============================================================================
# C5 — the `-h` discoverability state and its owner (RECORDED OPEN DEFECT)
# =============================================================================
# The claim under this check is NOT "the help page renders". The recorded defect
# is that `gopi chat --replay -h` answers a usage error instead of the page, and
# its owner is task 01a10351 — so this check REPORTS the measured state and names
# the owner; a defect tracked elsewhere is not turned into this gate's red.
c5_help_state() {
    local out rc first
    if [ ! -x "$BIN" ]; then
        skip C5 "$BIN is not executable (build it: CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/)"
        return
    fi
    out="$("$BIN" chat --replay -h 2>&1 >/dev/null)"
    rc=$?
    first="$(head -1 <<<"$out")"
    if [ "$rc" = "$REPLAY_HELP_RC" ]; then
        printf 'DEFECT C5 recorded=rc=%s (a help page) measured=rc=%s: %s\n' "$REPLAY_HELP_RC" "$rc" "$first"
        printf '       C5 owner=task %s (row status: %s, measured 2026-10-04) — the recorded -h discoverability\n' \
            "$HELP_DEFECT_OWNER" "$HELP_DEFECT_OWNER_STATUS"
        printf '       C5 defect, REPORTED here and not this gate'"'"'s claim: the row is closed while the defect is not\n'
        return
    fi
    if [ "$rc" = "$CHAT_HELP_RC" ]; then
        printf 'CLOSED C5 recorded=rc=%s measured=rc=%s (the help page rendered) — task %s is fixed, clear that row\n' \
            "$REPLAY_HELP_RC" "$rc" "$HELP_DEFECT_OWNER"
        return
    fi
    printf 'NOTE   C5 recorded=rc=%s measured=rc=%s — neither a help page nor the recorded state: %s\n' \
        "$REPLAY_HELP_RC" "$rc" "$first"
}
c5_help_state

# =============================================================================
# C6 — the usage line a doc quotes verbatim
# =============================================================================
c6_usage() {
    local anchor got rec
    anchor="$(anchor_file 'Usage: gopi chat [' "$DOC_ACCEPTANCE")"
    rec="$(sed -n "${anchor##*:}p" "$DOC_ACCEPTANCE" 2>/dev/null)"
    need C6 'quoted usage line' "$rec" "$anchor" || return
    if [ ! -x "$BIN" ]; then
        skip C6 "$BIN is not executable"
        return
    fi
    got="$("$BIN" chat -h 2>&1 | grep -m1 '^Usage: gopi chat \[')"
    judge C6 'the usage line the doc quotes verbatim' "$rec" "$got" "$DOC_ACCEPTANCE" "$anchor"
}
c6_usage

# =============================================================================
# C7 — the verb census and the help index
# =============================================================================
c7_verbs() {
    local anchor rec before after
    anchor="$(anchor_file "$PH_VERB_CENSUS" "$DOC_ACCEPTANCE")"
    rec="$(doc_token "$DOC_ACCEPTANCE" "$anchor" "$RE_VERB_CENSUS")"
    need C7a 'help.go verb census' "$rec" "$anchor" || return
    # `before` is read from a PINNED PREIMAGE, so it is unmeasurable in a store
    # that does not hold it — the trap that printed `before=0` and made a real
    # census look like drift (measured 2026-10-27).
    revs_readable C7a "the census's before/after preimages" "$HELP_PREIMAGE HEAD" || return
    before="$(git show "$HELP_PREIMAGE:$HELP_SOURCE" 2>/dev/null |
        grep -o '{"gopi [a-z_-]*' | sed 's/{"gopi //' | sort -u | wc -l)"
    after="$(git show "HEAD:$HELP_SOURCE" 2>/dev/null |
        grep -o '{"gopi [a-z_-]*' | sed 's/{"gopi //' | sort -u | wc -l)"
    judge C7a "the doc's own $HELP_SOURCE verb census" "$rec" "before=$before after=$after" "$DOC_ACCEPTANCE" "$anchor"
}
c7_verbs

c7_index() {
    local anchor rec rows cmds
    anchor="$(anchor_file "$PH_HELP_INDEX" "$DOC_ACCEPTANCE")"
    rec="$(doc_token "$DOC_ACCEPTANCE" "$anchor" "$RE_HELP_INDEX" | tr -d '*')"
    need C7b 'help index row count' "$rec" "$anchor" || return
    if [ ! -x "$BIN" ]; then
        skip C7b "$BIN is not executable"
        return
    fi
    rows="$("$BIN" --help 2>&1 | grep -cE "$COMMAND_INDEX_PATTERN")"
    cmds="$("$BIN" --help 2>&1 | grep -cE "$FILM_COMMAND_PATTERN")"
    # The doc's own §6 pitfall measures the PRE-ablation head, whose `replay` verb
    # made one row more; both readings agree that no replay/film COMMAND is
    # advertised, which is the claim the sentence actually makes.
    if [ "$cmds" = "0" ] && [ "$rows" != "$rec" ]; then
        printf 'MEASURED C7b the doc publishes %s index rows; this head prints %s %s, and neither\n' \
            "$rec" "$rows" "$COMMAND_INDEX_PATTERN"
        printf '       C7b advertises a replay/film command row (%s) — the sentence'"'"'s claim holds; its figure is head-dependent\n' "$cmds"
        CHECKED=$((CHECKED + 1))
        return
    fi
    judge C7b 'the help index row count and the "replay/film are not command words" claim' \
        "$rec index rows, 0 replay/film command rows" "$rows index rows, $cmds replay/film command rows" \
        "$DOC_ACCEPTANCE" "$anchor"
}
c7_index

# =============================================================================
# C8 — every commit the docs pin is either measured here or reported as absent.
# A PIN is a claim about OBJECT EXISTENCE, so the only store that can judge it is
# one that holds the object. Absence is therefore NOT drift: it is UNREACHABLE,
# carrying git's own words (measured 2026-10-27: all seven pins were absent in a
# clone ground and the row reported SEVEN STALE for what was one fetch-policy
# fact). What this row still measures wherever the objects exist: every pin must
# resolve TO A COMMIT — a pin that resolves to something else (a tree, a blob, a
# misspelled ref) is a real drift and stays STALE.
c8_commits() {
    local anchor rev bad="" absent="" held=0
    anchor="$(anchor_file "$PH_TASK_LINE" "$DOC_ACCEPTANCE")"
    for rev in $PINNED_COMMITS; do
        git_has "$rev"
        case "$?" in
            0)
                held=$((held + 1))
                git rev-parse --verify --quiet "${rev}^{commit}" >/dev/null 2>&1 || bad="$bad $rev"
                ;;
            1) absent="$absent; $rev: ${GIT_HAVE_RAW:-exit $GIT_HAVE_RC}" ;;
            *)
                BROKEN=$((BROKEN + 1))
                printf 'BROKEN C8 the object store failed on %s: %s\n' "$rev" "${GIT_HAVE_RAW:-exit $GIT_HAVE_RC}"
                return
                ;;
        esac
    done
    if [ "$bad" != "" ]; then
        stale C8 'a pinned commit resolves to a commit' \
            "all of: $PINNED_COMMITS are commits" "not a commit:$bad" "$anchor"
    fi
    if [ "$absent" != "" ]; then
        UNREACH=$((UNREACH + 1))
        printf 'UNREACHABLE C8 the commits the docs pin — this ground holds %s of %s and does not hold:%s\n' \
            "$held" "$(printf '%s' "$PINNED_COMMITS" | wc -w)" "${absent#;}"
        printf '       C8 not measured here, so no verdict; remedy: %s\n' "$UNREACHABLE_REMEDY"
        # A store that holds SOME pins and not others is the shape in which an
        # absent pin is worth a second look: it is unjudgeable HERE, but it is not
        # the plain "this ground fetched no tags" case either.
        if [ "$held" -gt 0 ]; then
            printf '       C8 NOTE this store holds %s of the pins, so the absence of the rest is not a fetch-policy blanket — judge it in a store that has them\n' "$held"
        fi
        return
    fi
    if [ "$bad" = "" ]; then
        ok C8 'every commit the docs pin resolves' "all of: $PINNED_COMMITS" "all resolve" "$anchor"
    fi
}
c8_commits

c8_dead_branch() {
    local anchor rec br_rc tag_short
    anchor="$(anchor_file 'no longer exists** — `git rev-parse' "$DOC_ACCEPTANCE")"
    # The `→ exit **1**` sits on the line AFTER the anchor sentence, so the two
    # lines are read together — the claim spans them.
    rec="$(sed -n "$(( ${anchor##*:} )),$(( ${anchor##*:} + 2 ))p" "$DOC_ACCEPTANCE" |
        grep -oE 'exit \*\*[0-9]+\*\*' | head -1)"
    need C8b 'branch exit status' "$rec" "$anchor" || return
    git rev-parse --verify --quiet "$DEAD_BRANCH" >/dev/null 2>&1
    br_rc=$?
    # The tag half is an OBJECT-EXISTENCE claim again: `git rev-parse --short` on
    # a tag this store never fetched prints nothing at all, and the empty string
    # used to be compared against the published short hash as if it were a
    # reading. Report it as not measured instead (measured 2026-10-27).
    if ! git_has "$REWORK_TAG"; then
        UNREACH=$((UNREACH + 1))
        printf 'UNREACHABLE C8b the rework-backup tag names the tip — this ground does not hold %s: %s\n' \
            "$REWORK_TAG" "${GIT_HAVE_RAW:-exit $GIT_HAVE_RC}"
        printf '       C8b the branch half WAS measured (exit %s); the tag half is not measured here; remedy: %s\n' \
            "$br_rc" "$UNREACHABLE_REMEDY"
        return
    fi
    tag_short="$(git rev-parse --short "$REWORK_TAG" 2>/dev/null)"
    judge C8b 'the deleted branch is gone and its rework-backup tag names the tip' \
        "$(printf '%s' "$rec" | tr -d '*') / tag $REWORK_TAG_SHORT" "exit $br_rc / tag $tag_short" \
        "$DOC_ACCEPTANCE" "$anchor"
}
c8_dead_branch

# =============================================================================
# C9 — the diff shortstats
# =============================================================================
# c9_range <check> <doc-phrase> <a> <b> <claim>
c9_range() {
    local name="$1" phrase="$2" a="$3" b="$4" claim="$5" anchor rec got
    anchor="$(anchor_file "$phrase" "$DOC_ACCEPTANCE")"
    rec="$(doc_ints "$DOC_ACCEPTANCE" "$anchor" "$RE_SHORTSTAT")"
    need "$name" 'diff shortstat' "$rec" "$anchor" || return
    # BOTH ends must be in this store: `git diff <absent> <b>` prints nothing, so
    # the empty measured value below used to be read as drift (2026-10-27).
    revs_readable "$name" "the $a..$b range" "$a $b" || return
    got="$(git diff --shortstat "$a" "$b" 2>/dev/null | awk '{print $1, $4, $6}')"
    judge "$name" "$claim" "$rec" "$got" "$DOC_ACCEPTANCE" "$anchor"
}
c9_range C9a "$PH_STAT_DRIVER" "$STAT_BASE" "$STAT_DRIVER_COMMIT" \
    "the $STAT_BASE → $STAT_DRIVER_COMMIT shortstat"
c9_range C9b "$PH_STAT_MERGED" "$STAT_BASE" "$STAT_DRIVER_MERGED" \
    "the $STAT_BASE → $STAT_DRIVER_MERGED shortstat"
# The ablation commit's own totals are published in the DELETIONS prose, not in
# the range table, so the anchor is that sentence and the value is the whole
# shortstat it states.
c9_ablation() {
    local anchor rec got
    anchor="$(anchor_file "$PH_STAT_ABLATION" "$DOC_ACCEPTANCE")"
    rec="$(doc_ints "$DOC_ACCEPTANCE" "$anchor" "$RE_SHORTSTAT_TAIL")"
    need C9d 'ablation commit shortstat' "$rec" "$anchor" || return
    revs_readable C9d "the ablation commit's own range" "$STAT_ABLATION_COMMIT" || return
    got="$(git diff --shortstat "$STAT_ABLATION_COMMIT^" "$STAT_ABLATION_COMMIT" 2>/dev/null | awk '{print $1, $4, $6}')"
    judge C9d "the ablation commit's own shortstat" "$rec" "$got" "$DOC_ACCEPTANCE" "$anchor"
}
c9_ablation

# The row whose range ends at the MOVING ref. The doc's §6b table publishes the
# figure measured at the head that row was taken at, and the head is not named in
# the table — so what is judged is REPRODUCIBILITY (the figure the doc publishes
# must be the one some head of this history yields), and what is reported is the
# head and the value the moving ref gives today (Rule 38).
c9_master() {
    local anchor rec head mine live best=""
    anchor="$(anchor_file "$PH_STAT_MASTER" "$DOC_ACCEPTANCE")"
    rec="$(doc_ints "$DOC_ACCEPTANCE" "$anchor" "$RE_SHORTSTAT")"
    need C9c 'master-range shortstat' "$rec" "$anchor" || return
    # REPRODUCIBILITY is judged by scanning heads of the moving ref for a range
    # that yields the figure — and every one of those ranges starts at STAT_BASE.
    # Without that end in the store the scan compares "" against the recorded
    # value at every head and concludes "no head yields it", which is a statement
    # about the STORE being read as a statement about the doc (2026-10-27).
    revs_readable C9c "the $STAT_BASE → $MOVING_REF reproducibility scan" "$STAT_BASE $MOVING_REF" || return
    for head in $(git rev-list --max-count=400 "$MOVING_REF" 2>/dev/null); do
        mine="$(git diff --shortstat "$STAT_BASE" "$head" 2>/dev/null | awk '{print $1, $4, $6}')"
        if [ "$mine" = "$rec" ]; then best="$head"; break; fi
    done
    if [ "$best" != "" ]; then
        ok C9c "the doc's $STAT_BASE → $MOVING_REF figure" "$rec" "reproduced at $(git rev-parse --short "$best")" "$anchor"
    else
        stale C9c "the $STAT_BASE → $MOVING_REF shortstat the doc publishes" \
            "$rec" "no head of $MOVING_REF in the last 400 yields it" "$DOC_ACCEPTANCE" "$anchor"
    fi
    head="$(git rev-parse --short "$MOVING_REF")"
    live="$(git diff --shortstat "$STAT_BASE" "$MOVING_REF" 2>/dev/null | awk '{print $1, $4, $6}')"
    printf 'MEASURED C9c the same range at %s today: %s (a census figure — re-pin it, Rule 38)\n' "$head" "$live"
}
c9_master

# =============================================================================
# C10 — the deleted files' line counts, and go.mod/go.sum
# =============================================================================
# The five counts are published as a prose block ("`path` **1176**, `path` **51**, …"),
# so the block is read once and each file's own bold count is parsed out of it.
c10_deletions() {
    local anchor block f want got bad="" rec=""
    anchor="$(anchor_file "$PH_PREIMAGE_PROSE" "$DOC_ACCEPTANCE")"
    block="$(sed -n "$(( ${anchor##*:} )),$(( ${anchor##*:} + 5 ))p" "$DOC_ACCEPTANCE" 2>/dev/null)"
    # Every count is read out of the PINNED PREIMAGE, so an absent preimage makes
    # each `git show … | wc -l` print 0 — five zeros reported as five mismatches
    # (2026-10-27). If the preimage is not here, none of the five is measured.
    revs_readable C10a "the deleted files at $HELP_PREIMAGE" "$HELP_PREIMAGE" || return
    for f in "$DEL_REPLAY_GO" "$DEL_CLI_REPLAY" "$DEL_VERIFY_PY" "$DEL_TERMEMU_PY" "$DEL_REPLAY_SH"; do
        # Matched on the FULL path the doc writes, never the basename: `replay.go`
        # also matches `scripts/replay/replay.sh`, and a basename match would read
        # the wrong file's number (measured while building this).
        want="$(printf '%s\n' "$block" | grep -oE "\`$f\` $RE_BOLD_COUNT" | grep -oE "$RE_BOLD_COUNT" | head -1 | tr -d '*')"
        need "C10a($f)" "published line count of $f" "$want" "$anchor" || return
        got="$(git show "$HELP_PREIMAGE:$f" 2>/dev/null | wc -l)"
        rec="$rec$(basename "$f")=$want/"
        [ "$got" = "$want" ] || bad="$bad $f(recorded=$want measured=$got)"
    done
    if [ "$bad" = "" ]; then
        ok C10a 'the deleted files line counts' "${rec%/}" "all five match $HELP_PREIMAGE" "$anchor"
        return
    fi
    stale C10a 'a deleted file line count the doc publishes' "${rec%/}" "mismatched:$bad" "$anchor"
}
c10_deletions

c10_module_files() {
    local anchor n
    anchor="$(anchor_file "$PH_MODULE_FILES" "$DOC_ACCEPTANCE")"
    # `git show --name-only <absent commit>` prints NOTHING, and `grep -c` then
    # answers 0 — so the row printed `OK recorded=0 go.mod/go.sum files
    # measured=0` about a commit it never listed. Zero files is the PUBLISHED
    # value here, which is exactly the case where a silent substitution is
    # invisible: the gate agreed with a reading it never took (2026-10-27).
    revs_readable C10b "the ablation commit's changed-file list" "$STAT_ABLATION_COMMIT" || return
    n="$(git show --name-only --format= "$STAT_ABLATION_COMMIT" 2>/dev/null | grep -cE "^($GOMOD|$GOSUM)$")"
    judge C10b "the $STAT_ABLATION_COMMIT diff does not touch the module files" \
        "0 $GOMOD/$GOSUM files" "$n $GOMOD/$GOSUM files" "$DOC_ACCEPTANCE" "$anchor"
}
c10_module_files

# =============================================================================
# C11 — the published flag count, group total, and pixel table
# =============================================================================
c11_flags() {
    local anchor rec_n rec_rows n rows
    anchor="$(anchor_file "$PH_CHAT_FLAG_ROW" "$DOC_ACCEPTANCE")"
    rec_n="$(sed -n "${anchor##*:}p" "$DOC_ACCEPTANCE" 2>/dev/null | grep -oE "$RE_INT" | tail -1)"
    need C11a 'chat flag count' "$rec_n" "$anchor" || return
    if [ ! -x "$BIN" ] || [ ! -f "$HELP_SCRIPT" ]; then
        skip C11a "$BIN or $HELP_SCRIPT is unusable"
        return
    fi
    n="$(bash "$HELP_SCRIPT" "$BIN" 2>/dev/null | awk -F'\t' '$1=="chat"{print $NF}')"
    judge C11a "the chat flag count $HELP_SCRIPT derives" "$rec_n flags" "$n flags" "$DOC_ACCEPTANCE" "$anchor"

    anchor="$(anchor_file "$PH_GROUP_TOTAL" "$DOC_ACCEPTANCE")"
    rec_rows="$(doc_token "$DOC_ACCEPTANCE" "$anchor" "$RE_GROUP_TOTAL" | tr -d '*')"
    need C11b 'help group row total' "$rec_rows" "$anchor" || return
    rows="$(bash "$HELP_SCRIPT" "$BIN" 2>/dev/null | awk -F'\t' 'NF>=2{s+=$NF} END{print s}')"
    judge C11b "the group total the doc reads off $HELP_SCRIPT" "$rec_rows rows" "$rows rows" "$DOC_ACCEPTANCE" "$anchor"
}
c11_flags

c11_pixels() {
    local anchor_a anchor_b rec_a rec_b got_a got_b page
    anchor_a="$(anchor_file "$PH_PIXEL_DEFAULT" "$DOC_ACCEPTANCE")"
    anchor_b="$(anchor_file "$PH_PIXEL_SAMEFREQ" "$DOC_ACCEPTANCE")"
    rec_a="$(doc_token "$DOC_ACCEPTANCE" "$anchor_a" "$RE_PX_DOC")"
    rec_b="$(doc_token "$DOC_ACCEPTANCE" "$anchor_b" "$RE_PX_DOC")"
    need C11c 'default geometry pixel figure' "$rec_a" "$anchor_a" || return
    need C11c 'same-frequency pixel figure' "$rec_b" "$anchor_b" || return
    if [ ! -x "$BIN" ]; then
        skip C11c "$BIN is not executable"
        return
    fi
    # THE PIXEL FIGURES LIVE ON THE SCOPED PAGE (2026-10-19): the replay/film
    # family's contracts — including the cell-size arithmetic and both pixel
    # figures — moved to `gopi chat --replay -h` (chat_replay_help_scope.go), and
    # the session page names that page instead of repeating them. Reading them off
    # `gopi chat -h` would report STALE for a figure that is still published, which
    # is the failure a check must not have: it would say "the doc invents this
    # number" about a number the binary still derives.
    page="$("$BIN" $PIXEL_HELP_ARGV 2>&1)"
    # WHICH FIGURE IS WHICH IS THE DOC'S ANCHOR, NOT A LINE NUMBER. The page prints
    # the worked example (`--cols 100 --rows 30` → 604x394) BEFORE the default
    # geometry's figure (706x342), while the doc anchors them the other way round;
    # reading them as "match 1" and "match 2" therefore compared the wrong pair and
    # reported both as STALE while both were present (measured 2026-10-19). The
    # reading is by IDENTITY: each recorded figure must be ON the page, and the
    # report names the first figure the page prints when it is not — so a dropped
    # or re-derived number still turns this red.
    got_a="$(px_present "$page" "$(px_doc_to_out "$rec_a")")"
    got_b="$(px_present "$page" "$(px_doc_to_out "$rec_b")")"
    judge C11c 'the pixel figure the doc publishes vs the help page it says derives it' \
        "$(px_doc_to_out "$rec_a")" "$got_a" "$DOC_ACCEPTANCE" "$anchor_a"
    judge C11d 'the same-frequency pixel figure vs the help page' \
        "$(px_doc_to_out "$rec_b")" "$got_b" "$DOC_ACCEPTANCE" "$anchor_b"
}
c11_pixels

# =============================================================================
# C12 — the film package's wiring at the doc's base, and the state today
# =============================================================================
c12_pkg() {
    local anchor importers entries today_importers today_entries
    anchor="$(anchor_file "$PH_FILM_UNWIRED" "$DOC_ACCEPTANCE")"
    if [ "${anchor##*:}" = "0" ]; then
        anchor="$(anchor_file "$PH_FILM_UNWIRED_FALLBACK" "$DOC_ACCEPTANCE")"
    fi
    # C12a is the row that measured GREEN in a clone ground while it never read
    # the revision: `git grep -c <absent>` prints nothing (wc -l → 0) and
    # `git ls-tree <absent>` prints nothing (wc -l → 0), so "0 importers / 0
    # entries" — the PUBLISHED value — was reproduced by two failed reads
    # (measured 2026-10-27). A missing object must never look like a clean zero.
    if revs_readable C12a "the §7/B1 boundary at the doc's base $BASE_DOC_CLAIM_REV" "$BASE_DOC_CLAIM_REV"; then
        importers="$(git grep -c "$FILM_IMPORT_SENTINEL" "$BASE_DOC_CLAIM_REV" -- '*.go' 2>/dev/null | wc -l)"
        entries="$(git ls-tree -r --name-only "$BASE_DOC_CLAIM_REV" "$FILM_PKG" 2>/dev/null | wc -l)"
        judge C12a "the §7/B1 boundary at the doc's base: $FILM_PKG is absent and nothing imports it" \
            "0 importers / 0 entries" "$importers importers / $entries entries" "$DOC_ACCEPTANCE" "$anchor"
    fi

    # The "today" half reads the MOVING ref, which every ground has by definition
    # (it is the branch the ground was cut at) — but it is checked the same way,
    # because a store that cannot answer here cannot answer "today" either.
    if ! revs_readable C12b "the $MOVING_REF state today" "$MOVING_REF"; then
        return
    fi
    today_importers="$(git grep -c "$FILM_IMPORT_SENTINEL" "$MOVING_REF" -- '*.go' 2>/dev/null | wc -l)"
    today_entries="$(git ls-tree -r --name-only "$MOVING_REF" "$FILM_PKG" 2>/dev/null | wc -l)"
    printf 'MEASURED C12b on %s today: %s file(s) name %s, %s holds %s file(s)\n' \
        "$MOVING_REF" "$today_importers" "$FILM_IMPORT_SENTINEL" "$FILM_PKG" "$today_entries"
    note "C12b the §7/B1 'is not on master' wording is HISTORICAL — the package landed after the doc's base"
}
c12_pkg

# =============================================================================
# C13 — the --film preflight refusal
# =============================================================================
c13_refusal() {
    local anchor rec out rc hint
    anchor="$(anchor_file "$FILM_REFUSAL_HINT" "$DOC_ACCEPTANCE")"
    rec="$(doc_token "$DOC_ACCEPTANCE" "$anchor" 'rc=[0-9]+')"
    if [ ! -x "$BIN" ]; then
        skip C13 "$BIN is not executable"
        return
    fi
    out="$("$BIN" chat --film "$UNWRITABLE_DIR" 2>&1 >/dev/null)"
    rc=$?
    hint="present"
    case "$out" in *"$FILM_REFUSAL_HINT"*) ;; *) hint="absent" ;; esac
    if [ "$rec" != "" ]; then
        judge C13 'the declared preflight refusal and its exit code' \
            "$rec, hint present" "rc=$rc, hint $hint" "$DOC_ACCEPTANCE" "$anchor"
    else
        judge C13 'the declared preflight refusal and its exit code' \
            "rc=$FILM_REFUSAL_RC, hint present" "rc=$rc, hint $hint" "$DOC_ACCEPTANCE" "$anchor"
    fi
    printf '       C13 the refusal text: %s\n' "$(head -1 <<<"$out")"
}
c13_refusal

# =============================================================================
# C14 — every flag a doc's command example uses is accepted, and mentioned
# =============================================================================
c14_flags() {
    local anchor bad="" unadvertised="" f out page
    anchor="$(anchor_file '**The flags live on `chat`**' "$DOC_ACCEPTANCE")"
    if [ ! -x "$BIN" ]; then
        skip C14 "$BIN is not executable"
        return
    fi
    # The WHOLE page, not just its usage line: a flag documented anywhere in
    # `gopi chat -h` is discoverable; one the page never mentions at all is not.
    page="$("$BIN" chat -h 2>&1)"
    for f in $DOC_EXAMPLE_FLAGS; do
        out="$("$BIN" chat "$f" "$UNWRITABLE_DIR" -h 2>&1 >/dev/null | head -1)"
        case "$out" in *"$UNKNOWN_FLAG_MARKER"*) bad="$bad $f" ;; esac
        case "$page" in *"$f"*) ;; *) unadvertised="$unadvertised $f" ;; esac
    done
    if [ "$bad" = "" ] && [ "$unadvertised" = "" ]; then
        ok C14 'every flag the docs command examples use is accepted AND mentioned' \
            "all of: $DOC_EXAMPLE_FLAGS" "all accepted and mentioned" "$anchor"
        return
    fi
    if [ "$bad" != "" ]; then
        stale C14 'a flag the docs command examples use is refused as unknown' \
            "all of: $DOC_EXAMPLE_FLAGS" "refused as unknown:$bad" "$DOC_ACCEPTANCE" "$anchor"
    fi
    # A flag the parser ACCEPTS but `gopi chat -h` never mentions is not
    # discoverable: the same defect family the doc's own §2 reports, and its owner
    # is task 01a10351 — REPORTED with the owner, not this gate's red.
    printf 'MEASURED C14 the chat help page never mentions:%s of the flags the docs examples use (%s)\n' \
        "${unadvertised:- none}" "$DOC_EXAMPLE_FLAGS"
    if [ "$unadvertised" != "" ]; then
        printf '       C14 owner=task %s — a flag the parser accepts and the page never mentions is the -h\n' "$HELP_DEFECT_OWNER"
        printf '       C14 discoverability defect (the doc'"'"'s own §2), REPORTED here, not this gate'"'"'s claim\n'
    fi
}
c14_flags

# =============================================================================
# C15 — the port census figures a sibling devdoc publishes
# =============================================================================
c15_port() {
    local anchor rec n
    anchor="$(anchor_file "$PH_VERIFY_LINES" "$DOC_ASSEMBLY")"
    rec="$(doc_token "$DOC_ASSEMBLY" "$anchor" "$RE_VERIFY_LINES")"
    need C15 "the $DEL_VERIFY_PY line count" "$rec" "$anchor" || return
    # An absent preimage makes `wc -l` answer 0 — and that 0 was then CLAMPED by
    # the line's own HISTORICAL token, so the row looked like a handled
    # divergence rather than a reading never taken (measured 2026-10-27).
    revs_readable C15 "the $DEL_VERIFY_PY line count at $HELP_PREIMAGE" "$HELP_PREIMAGE" || return
    n="$(git show "$HELP_PREIMAGE:$DEL_VERIFY_PY" 2>/dev/null | wc -l)"
    judge C15 "the $DEL_VERIFY_PY line count $DOC_ASSEMBLY publishes" "${rec%% lines*}" "$n" "$DOC_ASSEMBLY" "$anchor"
}
c15_port

c15_driver() {
    local anchor rec n
    anchor="$(anchor_file "$PH_DRIVER_LINES" "$DOC_ACCEPTANCE")"
    rec="$(doc_token "$DOC_ACCEPTANCE" "$anchor" '[0-9]+ lines')"
    need C15c "the $DRIVER_GO line count" "$rec" "$anchor" || return
    revs_readable C15c "the $DRIVER_GO line count at $BASE_DOC_CLAIM_REV" "$BASE_DOC_CLAIM_REV" || return
    n="$(git show "$BASE_DOC_CLAIM_REV:$DRIVER_GO" 2>/dev/null | wc -l)"
    judge C15c "the $DRIVER_GO line count the doc publishes" "${rec%% *} lines at $BASE_DOC_CLAIM_REV" \
        "$n lines at $BASE_DOC_CLAIM_REV" "$DOC_ACCEPTANCE" "$anchor"
}
c15_driver

# =============================================================================
# C16 — the PIXELS of the declared frames
#
# C1-C15 all read a NUMBER. A film whose whole face is a black bar satisfies
# every one of them, which is the gap HP measured (32.9% of one film's frames
# full-width pure black, this script green). C16 reads the pixels: it runs
# scripts/replay-pixel.py over each film directory the doc names and judges the
# tallest FULL-WIDTH same-colour run and the modal colour's share against the
# named constants above.
#
# WHY THE SHARE IS PART OF THE CRITERION (and not just the run height): a healthy
# film measured 111 px of full-width ground — 32.5% of the frame's HEIGHT, which a
# height-only threshold would flag. The same stripe read as PIXELS is 74.7% of the
# frame; and the defect HP measured is 18.7% of the height but ALSO 74.7% of the
# pixels. Height alone cannot separate them; the pair cannot be satisfied by a
# healthy ground stripe, because a stripe in the ground's own colour is the
# MODAL colour and the band leg is restricted to non-modal colours.
# =============================================================================
c16_measure_dir() { # → "<rc><TAB><the reader's dir row, or empty>"
    local dir="$1" out rc
    out="$(python3 "$C16_PIXEL_PY_REL" "$dir" 2>/dev/null)"
    rc=$?
    printf '%s\t%s\n' "$rc" "$(printf '%s\n' "$out" | awk -F'\t' -v t="$C16_ROW_DIR" '$1==t')"
}

c16_pixels() {
    local dirs="" d rc row nframes wxh maxpx maxcol maxframe nmpx nmcol nmframe sharep shareframe
    local readings="" fail_bits="" height out_of_band="" declared_row=""
    local band_claim blank_claim thin_claim
    for d in $EV_ALL_DIRS; do
        [ -d "$EVIDENCE/$d" ] || continue
        dirs="$dirs $d"
    done
    if [ -z "$dirs" ]; then
        skip C16 "$EVIDENCE holds none of the film directories ($EV_ALL_DIRS) — no frame to read"
        return
    fi
    for d in $dirs; do
        IFS=$'\t' read -r rc row < <(c16_measure_dir "$EVIDENCE/$d")
        if [ -z "$row" ]; then
            if [ "$rc" = "3" ]; then
                # The reader could not read pixels AT ALL (no PIL). That is not a
                # frame defect and not a pass — it is reported as its own thing.
                fail_bits="$fail_bits noreader($d: exit 3, the reader could not read pixels)"
            else
                fail_bits="$fail_bits norow($d: the reader produced no $C16_ROW_DIR row, rc=$rc)"
            fi
            continue
        fi
        nframes="$(printf '%s' "$row" | cut -f2)"
        wxh="$(printf '%s' "$row" | cut -f3)"
        maxpx="$(printf '%s' "$row" | cut -f4)"
        maxcol="$(printf '%s' "$row" | cut -f5)"
        maxframe="$(printf '%s' "$row" | cut -f6)"
        nmpx="$(printf '%s' "$row" | cut -f7)"
        nmcol="$(printf '%s' "$row" | cut -f8)"
        nmframe="$(printf '%s' "$row" | cut -f9)"
        sharep="$(printf '%s' "$row" | cut -f10)"
        shareframe="$(printf '%s' "$row" | cut -f11)"
        if [ "$nframes" = "0" ]; then
            note "C16 $EVIDENCE/$d holds no $FRAME_GLOB — nothing to read there (reported, not skipped silently)"
            continue
        fi
        height="${wxh#*x}"
        [ "$d" = "$C16_DECLARED_DIR" ] && declared_row="$row"
        readings="$readings $d{max-run=${maxpx}px/${height} ${maxcol}(${maxframe}) non-modal-run=${nmpx}px ${nmcol}(${nmframe}) modal-share=${sharep}%(${shareframe})}"
        # (a) a band ACROSS the ground: a tall full-width run in a colour the
        #     frame is not mostly made of. No share condition here — see the
        #     constant's note: a full-width band CANNOT be a majority of pixels,
        #     so a share test would never change a verdict.
        if [ "$nmpx" -ge "$C16_BAND_MIN_ROWS" ]; then
            fail_bits="$fail_bits band[$d: ${nmpx}px full-width ${nmcol} at ${nmframe} = $(printf '%.1f' "$(echo "$nmpx $height" | awk '{print 100*$1/$2}')")% of the frame height]"
        fi
        # (b) the frame IS one colour — near-blank, whatever the colour (dark
        #     included; this is why an all-dark frame fails while a dark ground
        #     does not).
        if [ "$(printf '%.0f' "$sharep")" -ge "$C16_SHARE_BLANK_PCT" ]; then
            fail_bits="$fail_bits blank[$d: one colour ${maxcol} fills ${sharep}% of the frame at ${shareframe}]"
        fi
        # (c) below the healthy floor: too little of any one colour to be a
        #     printed surface at all.
        if [ "$(printf '%.0f' "$sharep")" -lt "$C16_SHARE_MIN_PCT" ]; then
            fail_bits="$fail_bits thin[$d: modal share only ${sharep}%, floor ${C16_SHARE_MIN_PCT}%]"
        fi
    done

    # The declared film, re-measured against the doc's own numbers. The doc
    # publishes these frames as its evidence; a frame that no longer reads like a
    # printed surface is the thing this whole row exists to notice. Its row was
    # already taken in the loop above — one directory, one read.
    if [ -n "$declared_row" ]; then
        maxpx="$(printf '%s' "$declared_row" | cut -f4)"
        sharep="$(printf '%s' "$declared_row" | cut -f10)"
        if [ "$maxpx" -lt "$C16_RUN_PX_LO" ] || [ "$maxpx" -gt "$C16_RUN_PX_HI" ] ||
            [ "$(printf '%.0f' "$sharep")" -lt "$C16_SHARE_PCT_LO" ] ||
            [ "$(printf '%.0f' "$sharep")" -gt "$C16_SHARE_PCT_HI" ]; then
            fail_bits="$fail_bits declared[$C16_DECLARED_DIR: max-run ${maxpx}px and modal share ${sharep}% fall outside the range this gate pins (${C16_RUN_PX_LO}..${C16_RUN_PX_HI}px, ${C16_SHARE_PCT_LO}..${C16_SHARE_PCT_HI}%)]"
        fi
    fi

    out_of_band="max full-width run <= ${C16_RUN_PX_HI}px and modal share ${C16_SHARE_PCT_LO}..${C16_SHARE_PCT_HI}% (declared films); no non-modal-colour full-width run >= ${C16_BAND_MIN_ROWS}px; modal share < ${C16_SHARE_BLANK_PCT}%; readable modal share >= ${C16_SHARE_MIN_PCT}%"
    if [ -n "$fail_bits" ]; then
        stale C16 "C16 the frame pixels measured:$fail_bits" "$out_of_band" "the frames of:$readings" \
            "the C16 constant block in this script (C16 has no published figure: the criterion reads the frame's own pixels)"
        return
    fi
    ok C16 "$out_of_band" "the frames of:$readings" \
        "the C16 constant block in this script (C16 has no published figure: the criterion reads the frame's own pixels)"
}
c16_pixels

# =============================================================================
printf '\n'
printf 'replay-check: %d check row(s) measured, %d clamped line(s) by %s, %d note(s)\n' \
    "$CHECKED" "$CLAMPED" "$HISTORICAL_TOKEN" "$NOTES"
if [ "$CLAMPED" -gt 0 ]; then
    printf 'replay-check: ^ each CLAMP line above is a published figure that no longer holds — re-take it or re-word it\n'
fi
# The three outcomes are reported SEPARATELY and always: a run can be green on
# every anchor it could read and still have read only half of them, and the
# summary must say so (Rule 15). "0 stale" alone is a claim about the anchors this
# STORE could show, not about the docs.
if [ "$UNREACH" -gt 0 ]; then
    printf 'replay-check: %d anchor group(s) UNREACHABLE — this ground does not hold the pinned object(s); they were NOT\n' "$UNREACH"
    printf '               judged, are NOT drift, and do not fail this run. Remedy: %s\n' "$UNREACHABLE_REMEDY"
fi
if [ "$BROKEN" -gt 0 ]; then
    printf 'replay-check: %d object-store failure(s) (BROKEN) — the store could not answer, so those anchors are unverified\n' "$BROKEN"
fi
if [ "$STALE" -eq 0 ] && [ "$BROKEN" -eq 0 ]; then
    printf 'replay-check: OK — every replay/film anchor this ground could measure resolves (0 stale'
    if [ "$UNREACH" -gt 0 ]; then
        printf ', %d unreachable-and-not-judged' "$UNREACH"
    fi
    printf ')\n'
    exit 0
fi
if [ "$STALE" -gt 0 ]; then
    printf 'replay-check: %d STALE anchor(s) — fix each line above, or clamp it with %s on that line\n' \
        "$STALE" "$HISTORICAL_TOKEN"
fi
exit 1
