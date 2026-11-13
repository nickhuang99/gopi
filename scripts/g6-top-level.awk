# g6-top-level.awk — extract the top-level `gopi -h` page's advertised surface.
# Driven by scripts/g6-tools-maintenance-help-surface.sh with the page on stdin.
#
# The page is one document with four blocks, and the blocks make DIFFERENT
# claims, so they are derived separately rather than flattened into one list:
#
#   "Usage:"    the invocation forms — `gopi [options] "<prompt>"`, `gopi -h |
#               --help`, `gopi --help-env`, … Each is a promise about how argv
#               reaches a run, so each gets its own row.
#   "Inputs:"   the positional SHAPES — "<prompt>", @PATH, image.png,
#               ./PROJECT.md. They are not `gopi …` lines, so they are rendered as
#               `gopi <shape>` to keep every row a copy-pasteable invocation.
#   "Commands:" the other commands, each a form an operator types.
#   "Options…"  a flag TABLE, not an invocation: its claim is "this spelling is
#               admitted", graded by the flag sweep (one row per flag, so a flag
#               the help prints and the parser rejects is visible by name).
#
# Prose is cut by the SAME rule as the per-verb pages (see g6-help-forms.awk):
# the first run of >= 2 spaces whose END column is at or past 16.
{
    if ($0 ~ /^Usage:/)   { block = "usage";    next }
    if ($0 ~ /^Inputs:/)  { block = "inputs";   next }
    if ($0 ~ /^Commands/) { block = "commands"; next }
    if ($0 ~ /^Options/)  { block = "options";  next }
    if ($0 ~ /^[A-Z][A-Za-z]/) { block = ""; next }   # the next heading ends it
    if (block == "" ) next

    # ONE RULE FOR EVERY BLOCK: an ENTRY opens at exactly two spaces of
    # indent; anything deeper is that entry continuation prose (the Inputs
    # block explains a shape over three or four extra lines, and the Options
    # block wraps a description the same way). Without this, a wrapped line
    # whose text happens to begin "gopi ..." or "exits 1." is read as a new
    # form — measured on the first run of this script.
    if ($0 !~ /^  [^ ]/) next

    line = $0
    sub(/^[ \t]+/, "", line)
    if (line == "") next

    if (block == "usage" || block == "commands") {
        if (line !~ /^gopi /) next
        form = strip_fold(cut_prose(line))
        if (form == "" || seen[form]++) next
        printf "(top-level)\t%s\t\t0\n", form
        next
    }
    if (block == "inputs") {
        form = cut_prose(line)
        if (form == "" || seen_in[form]++) next
        printf "(top-level)\tgopi %s\t\t0\n", form
        next
    }
    # options: `--flag NAME   description`
    if (line !~ /^--[a-z]/) next
    n = split(line, p, /[ \t]+/)
    f = p[1]
    sub(/^--/, "", f)
    if (f == "" || seen_opt[f]++) next
    printf "(top-level flags)\tgopi --%s\t%s\t1\n", f, f
}

function cut_prose(line,   i, run) {
    sub(/^[ \t]+/, "", line)
    run = 0
    for (i = 1; i <= length(line); i++) {
        if (substr(line, i, 1) == " ") { run++; continue }
        if (run >= 2 && i - 1 >= 16) return substr(line, 1, i - 1 - run)
        run = 0
    }
    return line
}
function strip_fold(s) {
    sub(/[ \t]+$/, "", s)
    sub(/ \\$/, "", s)
    return s
}
