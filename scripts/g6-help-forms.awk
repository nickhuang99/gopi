# g6-help-forms.awk — extract the advertised invocation FORMS from ONE verb's
# help page. Driven by scripts/g6-tools-maintenance-help-surface.sh with
#   -v verb=<the page verb>
# and the page on stdin (2>&1 from the binary).
#
# ONE OUTPUT RECORD PER FORM:
#   <verb> <TAB> <form> <TAB> <sorted flags> <TAB> <flag count>
#
# WHAT COUNTS AS A FORM. Every line whose trimmed text starts with `gopi `.
# That deliberately covers three shapes the pages use, because all three are
# promises an operator types:
#     the synopsis   "  gopi voice_input <audio-file> [--chunk N]"
#     an example     "  gopi voice_input long-interview.wav --chunk 15"
#     a refusal      "  gopi voice_input clip.ogg   refused: ogg is not ..."
#
# THE THREE SCANNER TRAPS (all measured while writing this — the first two are
# the G3 lessons, memory #4824):
#
#   (a) THE DESCRIPTION BLOCK IS NOT A TERMINATOR. `gopi voice_input -h` prints
#       seventeen `gopi voice_input …` forms with PROSE between them. A scanner
#       that stopped at the first prose line lost most of them, so this one
#       decides per LINE and never per block.
#
#   (b) PROSE IS NOT SURFACE, and the cut is the hard half. cut_prose() cuts at
#       the first run of >= 2 spaces whose END column is at or past 16, ignoring
#       the leading block indent. It is total because of how the printer lays
#       its blocks out: a pinned block pads the description to one column
#       ("gopi merge" + 26 spaces + prose) and a right-aligned block pads each
#       row so the runs END on the column (voice_input), while a form whose own
#       tail IS the description column keeps its single spaces and survives.
#       16 is not guessed: it is the floor of the help printer's computed
#       description column (internal/config helpColumn floors at 16 for its
#       narrowest block), so no description on any G6 page opens before it.
#       Two simpler drafts were measured wrong against the live pages — "any run
#       of 2" truncated three real merge forms, "any run of 3" truncated one
#       more ("gopi merge --max-conflicted-files N per-branch conflicted-file
#       ceiling"). When the column floor is ever raised, both the rule and the
#       Go-side derivation (cmd/agent/cli_tools_maintenance_help_surface_basic_test.go,
#       g6CutDescription) must move together — the gate compares their outputs.
#
#   (c) A PAGE TITLE IS NOT A FORM. The first line of every page is
#       "gopi <verb> — <prose>" (em dash) — or, since the verb-retirement epic
#       moved the group's words under a door, "gopi <door> <verb> — <prose>"
#       (measured on `gopi tool text2img -h`, `gopi task archive -h`,
#       `gopi config completion -h`). Either way it names the page, so it is
#       skipped by its own explicit rule rather than by luck: one or two words
#       after the program word, then an em dash.
#
#   (d) THE PAGE'S FLAT SPELLINGS ARE RE-DERIVED, NOT COPIED. The page text is
#       the same document whatever address printed it, and the help printer
#       renders some lines with the verb's BARE word even on a page reached
#       through a door (measured on the family-census page, whose fixture
#       listings are assembled from the page's own verb word). The operator's
#       ruling on that spelling is the one the refusal vocabulary implements:
#       `gopi scratch-gc …` is RETIRED and does not dispatch. A row listing it
#       would hand a reader a copy-pasteable invocation that refuses, so the
#       form is rewritten to the door spelling the reader must type — the same
#       rewrite cmd/agent's g6DoorSpelling performs on the Go side, and the gate
#       compares the two derivations.
#
# A trailing backslash (a folded shell continuation such as
# `gopi voice_input --client --seconds 5 --stdio \`) is stripped: it belongs to
# the page's line breaking, not to the invocation.
{
    line = $0
    if (line ~ /^gopi ([a-zA-Z0-9_-]+ )?[a-zA-Z0-9_-]+ — /) next   # (c) the page title
    # A page whose shape is "Usage: gopi version" (the one-line verbs) names its
    # form on that line. It starts with "Usage:", not "gopi ", so it needs its
    # own rule or the verb contributes NOTHING to the derivation.
    if (line ~ /^Usage: *gopi /) {
        sub(/^Usage: */, "", line)
        if (line ~ /^gopi ([a-zA-Z0-9_-]+ )?[a-zA-Z0-9_-]+ — /) next  # (c) again, for a retitled page
    }
    if (line !~ /^[ \t]*gopi /) next                # (a) not a form line
    form = cut_prose(line)
    form = strip_fold(form)
    form = door_spelling(form)                      # (d) the live address
    if (form == "" || seen[form]++) next
    emit(verb, form)
}

# door_spelling rewrites this page's own flat spelling to the door spelling the
# binary dispatches (`gopi scratch-gc …` -> `gopi tool scratch-gc …`). An empty
# door (a verb reached by its own word: merge, worker, config, version) is a
# no-op, and a line naming ANOTHER verb is left alone — the rewrite is keyed on
# this page's verb word, which is the same rule g6DoorSpelling applies in Go.
function door_spelling(form,   flat) {
    if (door == "") return form
    flat = "gopi " verb
    if (form != flat && index(form, flat " ") != 1) return form
    return "gopi " door " " verb substr(form, length(flat) + 1)
}

# cut_prose removes the trailing description from one advertised form.
function cut_prose(line,   i, run) {
    sub(/^[ \t]+/, "", line)                        # the block indent is not a cut
    run = 0
    for (i = 1; i <= length(line); i++) {
        if (substr(line, i, 1) == " ") { run++; continue }
        if (run >= 2 && i - 1 >= 16) return substr(line, 1, i - 1 - run)
        run = 0
    }
    return line
}

# strip_fold drops the trailing whitespace and a folded line continuation.
function strip_fold(s) {
    sub(/[ \t]+$/, "", s)
    sub(/ \\$/, "", s)
    return s
}

# emit prints one record, with the flags SORTED so successive runs (and the
# Go-side derivation, which also sorts) are diffable.
function emit(v, form,   count, arr, i, j, key, joined, empty) {
    flags = ""
    count = split(form, parts, /--/)
    for (i = 2; i <= count; i++) {
        if (match(parts[i], /^[a-z][a-z-]*/)) {
            f = substr(parts[i], 1, RLENGTH)
            if (index("," flags ",", "," f ",") == 0) flags = (flags == "" ? f : flags "," f)
        }
    }
    count = split(flags, arr, ",")
    for (i = 2; i <= count; i++) {
        key = arr[i]
        for (j = i - 1; j >= 1 && arr[j] > key; j--) arr[j + 1] = arr[j]
        arr[j + 1] = key
    }
    joined = ""
    for (i = 1; i <= count; i++) joined = (i == 1 ? arr[i] : joined "," arr[i])
    empty = (count == 1 && arr[1] == "")
    printf "%s\t%s\t%s\t%d\n", v, form, (empty ? "" : joined), (empty ? 0 : count)
}
