package config

import (
	"os"

	"github.com/nickhuang99/gopi/internal/boolvocab"
)

// resume_ground.go — the ONE explicit escape hatch out of the resume ground
// refusal.
//
// WHY IT IS AN ENVIRONMENT VARIABLE AND NOT A FLAG. The resume face has two
// addresses (`gopi session resume <target>` and the no-verb `gopi --session
// <id> "<prompt>"`) that both funnel into one core; a flag would have to be
// parsed, threaded and documented twice, and the second address has no flag
// slot at all (its argv tail is the prompt). The variable reaches both through
// ONE reader below.
//
// WHY IT IS *NOT* IN THE settings.go CATALOG. The catalog is the operator's
// knob table (`gopi --help-env`): every row there is a supported, documented
// setting. This one is a deliberate FOOTGUN — it re-enables the exact
// degradation the refusal exists to prevent (a resume that runs in the CALLER's
// cwd, which from a main checkout means commits with no owner, no branch and no
// rollback point — PROJECT.md Rule 37). Advertising it beside ordinary knobs
// would invite its use; naming it in the refusal's own repair line (which is
// where an operator meets it) does not.
//
// It is still a REAL variable with a real reader (the Rule 21 shape): the gate
// that requires every env-name constant to be consumed is satisfied by
// ResumeAllowMissingGround below, and the refusal's repair line spells it.
const ResumeAllowMissingGroundEnv = "GOPI_RESUME_ALLOW_MISSING_GROUND"

// ResumeAllowMissingGround reports whether an operator EXPLICITLY asked the
// resume face to degrade to the caller's cwd when the row's recorded ground is
// unusable. Anything other than a truthy token (boolvocab: 1/true/yes/on) is
// "not asked" — a typo must never silently re-arm the degradation, which is the
// one direction this switch may fail in.
func ResumeAllowMissingGround() bool {
	return boolvocab.IsTrue(os.Getenv(ResumeAllowMissingGroundEnv))
}
