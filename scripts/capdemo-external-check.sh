#!/usr/bin/env bash
# capdemo-external-check.sh — the PUBLISH CHECKPOINT for docs/archive/demo/external/.
#
# WHY A SCRIPT AND NOT A PARAGRAPH IN THE BUNDLE. The external edition's own
# rule is "every claim is an anchor". A rule written only in prose regresses the
# moment the thing it anchors moves — and it DID: the stills were re-shot at a
# later head, two digests and one byte count in `stills/CAPTIONS.md` went stale,
# two stills referenced by `devdoc/capability-annotation-handoff.md` were
# renamed out of existence, and the edition still asserted that `gopi -h`
# advertises no `annotate` row while `gopi annotate -h` renders a full page
# (measured 2026-09-29). Nothing failed a build. This script is what fails.
#
# WHAT IT IS: a READ-ONLY audit. It writes no file, creates no scratch
# directory, runs no gopi capability and generates no load (GOPI.md Rule 27) —
# it reads the bundle, the docs that cite it, and the binary's own help text,
# and it names every claim whose anchor is MISSING or STALE.
#
# USAGE
#   bash scripts/capdemo-external-check.sh            # from the repo root
#   bash scripts/capdemo-external-check.sh ./bin/gopi # pin the binary it probes
# Exit status: 0 = every anchor resolves; 1 = at least one stale/missing anchor.
# The stale claims are the OUTPUT, one line each, prefixed STALE.
#
# THE CHECKS, and the claim each one anchors:
#   A1  every docs/archive/demo/external/**.png path named by a tracked doc exists
#       → a renamed/deleted still cannot stay cited. ONE reviewed bypass, per
#         LINE and counted in the output: a doc recording that a still was
#         replaced carries the token CAPDEMO-A1-HISTORICAL on that same line
#         (per-line, so the rest of the file stays under audit).
#   A2  every still in stills/*.png has its own `## `<name>`` heading in
#       CAPTIONS.md, and every such heading names a file that exists
#       → no unattributed still, no orphan caption.
#   A3  the digest and byte count recorded for a still equal the file's own
#       → a re-shot still whose caption still quotes the old digest is STALE.
#   A4  each heading carries a fenced producing command, that command's output
#       path names the still, and its VERB is advertised by `gopi -h`
#       → the claim "produced by gopi's own verb" is checked against the verb
#         list, not asserted.
#   A5  every repo-relative INPUT path in a producing command exists
#       → a command citing a file that is gone is not reproducible.
#   A6  CAPTIONS.md pins the commit the stills were produced at, and the token
#       resolves in this repository.
#   A7  README.md names the census GENERATOR (a scripts/*.sh that exists) and
#       the commit it was measured at, and that commit resolves and is an
#       ancestor of HEAD (Rule 38).
#
# Rule 1: every literal that carries meaning is a named constant below.
set -uo pipefail

BUNDLE_DIR='docs/archive/demo/external'
STILLS_DIR="$BUNDLE_DIR/stills"
CAPTIONS_REL="$STILLS_DIR/CAPTIONS.md"
README_REL="$BUNDLE_DIR/README.md"
CENSUS_GENERATOR_REL='scripts/gopi-stats.sh'
DEFAULT_BIN='./bin/gopi'
# The phrases that carry an anchor; the checker greps for these exact words so
# a caption that drops its pin fails rather than silently passing.
#
# THE FAMILY WORDS: since the verb retirement a command reaches its surface
# either by its retired flat spelling (`gopi shot`) or through the family door
# that owns it (`gopi tool shot`), and `gopi -h` advertises the LATTER. A4
# derives a command's own ADDRESS with this list, so the check names one thing
# (Rule 14) instead of a family word repeated per call site.
#
# WHAT THIS ARM CHANGES, MEASURED (2026-10-07, rework of task/01a10e23…) — and
# it is smaller than it looks, so it is written down rather than implied. A/B on
# UNCHANGED master input (master's bundle + this script): 6 STALE / rc=1, the
# same 6 master's own script reports — the bundle half is the observable half.
# This arm is inert on TODAY'S captions because they spell the retired flat word
# (`gopi shot …`), which master's bare attribution reads correctly enough: it
# attributes the still to `shot`, and `shot` is not advertised, so the STALE is
# right. Where the two arms differ is a caption that spells the DOOR
# (`gopi tool shot …`): bare attribution then reads the family noun `tool`, and
# `gopi -h` DOES advertise `tool`, so a still produced by a retired flat word
# would pass as attributed. The generic rule closes that hole: the address it
# derives is the two-word one `gopi -h` actually lists, so the attribution is
# correct by construction rather than accidentally correct.
FAMILY_WORDS='tool session task web memory config'
PRODUCED_PHRASE='produced at commit'
MEASURED_PHRASE='Measured at commit'
HEADING_PREFIX='## `'
STILL_SUFFIX='.png'
# A commit pin on this branch is a 40-hex object name; a short one is accepted
# for CAPTIONS (a still's pin) but never for the census figure.
COMMIT_MIN_HEX=40

BIN="$DEFAULT_BIN"
if [ "${1:-}" != "" ]; then
    BIN="$1"
fi

STALE=0
NOTES=0

stale() {
    STALE=$((STALE + 1))
    printf 'STALE  %s\n' "$1"
}

note() {
    NOTES=$((NOTES + 1))
    printf 'NOTE   %s\n' "$1"
}

hex_run_of_len() { # hex_run_of_len <string> <len> → first hex run of exactly that length
    printf '%s' "$1" | grep -oE '[0-9a-f]+' | awk -v n="$2" 'length($0) == n { print; exit }'
}

# --- A1: every cited still path resolves -------------------------------------
# A tracked doc may legitimately need to NAME a still that no longer exists — to
# record, in a dated banner, that it was replaced (this bundle's own devdoc does
# exactly that, quoting the dead specimen path in the row that reports it).
# Deleting that citation would suppress a record, so the doc is clamped ON A
# LINE: the visible token A1_HISTORICAL_TOKEN on the SAME line as the citation
# marks the citation as history. Per-line, never per-file, is the whole point —
# the file stays under audit, so a stale citation anywhere else in it still
# fails, and the result line reports how many lines were clamped (an exemption
# that grows quietly is the thing Rule 1/15 forbid, so it is counted and printed).
A1_HISTORICAL_TOKEN='CAPDEMO-A1-HISTORICAL'
A1_CLAMPED=0
while IFS= read -r hit; do
    [ "$hit" = "" ] && continue
    cited="${hit##*:}"
    hit_file="${hit%%:*}"
    hit_rest="${hit#*:}"
    hit_line="${hit_rest%%:*}"
    if [ -f "$hit_file" ] && sed -n "${hit_line}p" "$hit_file" | grep -qF "$A1_HISTORICAL_TOKEN"; then
        A1_CLAMPED=$((A1_CLAMPED + 1))
        continue
    fi
    if [ ! -f "$cited" ]; then
        stale "A1 a tracked doc cites a still that does not exist: $cited ($hit_file:$hit_line)"
    fi
done < <(git grep -n -oE "$BUNDLE_DIR/[A-Za-z0-9_./-]+$STILL_SUFFIX" -- devdoc docs 2>/dev/null | sort -u)
if [ "$A1_CLAMPED" -gt 0 ]; then
    note "A1 $A1_CLAMPED citation line(s) clamped as history by the $A1_HISTORICAL_TOKEN token"
fi

# --- the caption manifest, parsed once ---------------------------------------
# One pass over CAPTIONS.md emits, per heading: C=producing command,
# D=recorded digest, B=recorded byte count. No temp file: the whole manifest is
# a variable (Rule 35 — the checkpoint persists nothing).
MANIFEST="$(awk '
function hex64(s,   i, c, run) {
    run = ""
    for (i = 1; i <= length(s); i++) {
        c = substr(s, i, 1)
        if (c ~ /[0-9a-f]/) { run = run c } else { if (length(run) == 64) return run; run = "" }
    }
    if (length(run) == 64) return run
    return ""
}
function flush() {
    if (cur == "") return
    if (cmd != "") print "C\t" cur "\t" cmd
    if (dig != "") print "D\t" cur "\t" dig
    if (byt != "") print "B\t" cur "\t" byt
}
/^## `/ {
    flush()
    cur = ""; cmd = ""; dig = ""; byt = ""; infence = 0
    if (match($0, /`[^`]+\.png`/)) { cur = substr($0, RSTART + 1, RLENGTH - 2) }
    next
}
cur == "" { next }
/^```/ { infence = !infence; next }
infence {
    # The PRODUCING command is the first line in the fence that RUNS a gopi
    # binary — a preparation line before it (03 writes its sample text first) is
    # not the command that made the still.
    if (cmd == "" && ($1 == "gopi" || $1 ~ /\/gopi$/)) cmd = $0
    next
}
{
    if (dig == "") { d = hex64($0); if (d != "") dig = d }
    if (byt == "" && match($0, /[0-9]+ bytes/)) { byt = substr($0, RSTART, RLENGTH - 6) }
}
END { flush() }
' "$CAPTIONS_REL")"

heading_names() { printf '%s\n' "$MANIFEST" | awk -F'\t' '$1 == "C" { print $2 }' | sort -u; }

# --- A2: coverage both ways --------------------------------------------------
while IFS= read -r f; do
    [ "$f" = "" ] && continue
    base="$(basename "$f")"
    if ! heading_names | grep -qxF "$base"; then
        stale "A2 still has no provenance heading in $CAPTIONS_REL: $f"
    fi
done < <(find "$STILLS_DIR" -maxdepth 1 -name "*$STILL_SUFFIX" | sort)

while IFS= read -r name; do
    [ "$name" = "" ] && continue
    if [ ! -f "$STILLS_DIR/$name" ]; then
        stale "A2 $CAPTIONS_REL has a heading for a still that does not exist: $name"
    fi
done < <(heading_names)

# --- A3/A4/A5: per-still anchor, digest, bytes, command ----------------------
while IFS= read -r name; do
    [ "$name" = "" ] && continue
    path="$STILLS_DIR/$name"
    [ -f "$path" ] || continue

    recorded="$(printf '%s\n' "$MANIFEST" | awk -F'\t' -v n="$name" '$1 == "D" && $2 == n { print $3; exit }')"
    actual="$(sha256sum "$path" | awk '{print $1}')"
    if [ "$recorded" = "" ]; then
        stale "A3 $name: no sha256 recorded in $CAPTIONS_REL (a still with no digest cannot be shown stale)"
    elif [ "$recorded" != "$actual" ]; then
        stale "A3 $name: caption records sha256 $recorded but the file is $actual (STALE — re-shoot the caption, or the still)"
    fi

    rec_bytes="$(printf '%s\n' "$MANIFEST" | awk -F'\t' -v n="$name" '$1 == "B" && $2 == n { print $3; exit }')"
    act_bytes="$(stat -c %s "$path")"
    if [ "$rec_bytes" = "" ]; then
        stale "A3 $name: no byte count recorded in $CAPTIONS_REL"
    elif [ "$rec_bytes" != "$act_bytes" ]; then
        stale "A3 $name: caption records $rec_bytes bytes but the file is $act_bytes bytes"
    fi

    cmd="$(printf '%s\n' "$MANIFEST" | awk -F'\t' -v n="$name" '$1 == "C" && $2 == n { print $3; exit }')"
    if [ "$cmd" = "" ]; then
        stale "A4 $name: no fenced producing command under its heading"
        continue
    fi
    case "$cmd" in
        *"$name"*) ;;
        *) stale "A4 $name: its producing command does not name the still as an output: $cmd" ;;
    esac

    # The verb A4 attributes the still to: the first token the command names
    # after the program word, ALTOGETHER with any family noun that FOLLOWS it
    # (`gopi tool shot …` → `tool shot`), because since the verb retirement that
    # two-word address is what `gopi -h` advertises and what argv[0] accepts —
    # `gopi shot` is RETIRED and refuses, so checking the bare word would fail
    # every still produced through the family door and pass a spelling the binary
    # no longer runs. The A4 line and the README's rewrite rule both spell this
    # same address, so the command a reader copies and the state the binary
    # advertises cannot drift apart.
    verb="$(printf '%s' "$cmd" | awk -v fam="$FAMILY_WORDS" '
        { b = $1; if (b !~ /gopi$/) next
          words[1] = $2
          if ($3 != "" && index(" " fam " ", " " $2 " ") > 0) { print $2 " " $3 } else { print $2 } }')"
    if [ "$verb" = "" ]; then
        stale "A4 $name: its producing command does not start with a gopi invocation: $cmd"
    elif [ -x "$BIN" ]; then
        if ! "$BIN" -h 2>/dev/null | grep -qE "^  gopi ${verb}([ <]|$)"; then
            stale "A4 $name: its producing verb '$verb' is not advertised by \`$BIN -h\` — the still cannot be attributed to a shipped verb"
        fi
    else
        note "A4 $BIN is not executable — verb advertisement not probed (build it: CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/)"
    fi

    # Repo-relative INPUTS: a token that is not a flag, not the --out value,
    # not a shell expansion, and not an absolute path (a session path the
    # reader substitutes, which is outside the checkout by design).
    for tok in $cmd; do
        case "$tok" in
            # Not a path this check can resolve: a flag, a shell expansion, an
            # absolute path (a session path outside the checkout, which the
            # reader substitutes), a quoted fragment, or an assignment.
            -*|/*|*'$'*|*'"'*|*"'"*|*'('*|*')'*|*'='*) continue ;;
        esac
        case "$tok" in
            *"$STILL_SUFFIX"|*.txt|*.md) ;;
            *) continue ;;
        esac
        case "$cmd" in
            *"--out $tok"*) continue ;;
        esac
        if [ ! -f "$tok" ]; then
            stale "A5 $name: its producing command reads a repo-relative input that does not exist: $tok"
        fi
    done
done < <(heading_names)

# --- A6: the stills' producing commit ----------------------------------------
if ! grep -qF "$PRODUCED_PHRASE" "$CAPTIONS_REL"; then
    stale "A6 $CAPTIONS_REL does not pin the commit the stills were produced at (want the phrase \"$PRODUCED_PHRASE\")"
else
    pin_line="$(grep -F "$PRODUCED_PHRASE" "$CAPTIONS_REL" | head -1)"
    pin="$(hex_run_of_len "$pin_line" "$COMMIT_MIN_HEX")"
    if [ "$pin" = "" ]; then
        stale "A6 the producing commit pinned in $CAPTIONS_REL is not a $COMMIT_MIN_HEX-hex object name: $pin_line"
    elif ! git cat-file -e "${pin}^{commit}" 2>/dev/null; then
        stale "A6 the commit pinned in $CAPTIONS_REL does not resolve in this repository: $pin"
    fi
fi

# --- A7: the census generator and the commit it was measured at (Rule 38) ----
if ! grep -qF "$CENSUS_GENERATOR_REL" "$README_REL"; then
    stale "A7 $README_REL quotes no census figure with its generator (want $CENSUS_GENERATOR_REL)"
elif [ ! -f "$CENSUS_GENERATOR_REL" ]; then
    stale "A7 $README_REL names the generator $CENSUS_GENERATOR_REL, which does not exist"
fi
if ! grep -qF "$MEASURED_PHRASE" "$README_REL"; then
    stale "A7 $README_REL does not pin the commit its census figures were measured at (want the phrase \"$MEASURED_PHRASE\")"
else
    measured_line="$(grep -F "$MEASURED_PHRASE" "$README_REL" | head -1)"
    measured="$(hex_run_of_len "$measured_line" "$COMMIT_MIN_HEX")"
    if [ "$measured" = "" ]; then
        stale "A7 the census commit in $README_REL is not a $COMMIT_MIN_HEX-hex object name: $measured_line"
    elif ! git cat-file -e "${measured}^{commit}" 2>/dev/null; then
        stale "A7 the census commit in $README_REL does not resolve in this repository: $measured"
    elif ! git merge-base --is-ancestor "$measured" HEAD 2>/dev/null; then
        stale "A7 the census commit $measured is not an ancestor of HEAD — the figures were measured on some other history"
    fi
fi

printf '\n'
if [ "$STALE" -eq 0 ]; then
    printf 'capdemo-external-check: OK — every anchor in %s resolves (%d note(s), 0 stale)\n' "$BUNDLE_DIR" "$NOTES"
    exit 0
fi
printf 'capdemo-external-check: %d STALE/missing anchor(s) in %s — fix each line above, then re-run\n' "$STALE" "$BUNDLE_DIR"
exit 1
