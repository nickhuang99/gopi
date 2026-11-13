#!/bin/bash
# The prose-only offender: EVERY line below names the helper and NOT ONE of them
# routes to it (a comment, an echo, a `[ -r … ]` probe). The old predicate
# (`strings.Contains(body, "tmp-ban.sh")` over ALL lines, comments included)
# called this compliant while the script built its own scratch with mktemp.
# scripts/lib/tmp-ban.sh is the ONE definition of the scratch root.
echo "see scripts/lib/tmp-ban.sh for the scratch contract"
helper="scripts/lib/tmp-ban.sh"
[ -r "$helper" ] || echo "helper not readable" >&2
SCRATCH_PARENT="${TMPDIR:-$HOME/.cache}"
SB=$(mktemp -d -p "$SCRATCH_PARENT" prose-probe.XXXXXX) || exit 1
echo "$SB"
