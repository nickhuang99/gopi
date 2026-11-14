package config

// delivery_repo_path.go — the PATH half of a declared delivery point, resolved
// from the ONE declaration this host already carries (Rule 14: the answer has
// one source, referenced by every leg; Rule 2: every env read lives here, in
// the config package, not in a consumer).
//
// WHY THIS EXISTS (measured 2026-10-25, filed by the run that hit it): three
// delivery legs — the serve/`gopi task push` pass, the harvest push-back and
// the close-out push — each carried its OWN constant for the peer's checkout
// path, and all three spelled `~/work/GoPi`. The host declares its delivery
// point elsewhere and under another spelling (`dev_repo` = `zpeer:/home/user/work/gopi`,
// `repos` = `/home/user/work/gopi`), and the two paths are DIFFERENT
// REPOSITORIES on the peer (measured: `rev-list --max-parents=0` answers
// disjoint root commits, 12025 vs 359 commits). Consequences measured in the
// same evidence file: the mirror ref took the OTHER repository's tip, every
// tick printed an unrelated-histories verdict, and all three pushes were
// refused non-fast-forward.
//
// The rule this file implements: **the peer's checkout path is a fact of the
// DELIVERY POINT, parsed out of the declaration — never a per-leg literal.** A
// path that is only a fallback says so (see peer_repo_path.go in
// internal/tools, which owns the ordering and the words).

// DeliveryRepoDeclared answers the delivery point this host declares, together
// with the name of the channel that declared it (Rule 15: a resolution says
// WHAT it resolved AND WHERE THAT CAME FROM — a value with no provenance is a
// guess wearing a declaration's clothes).
//
// It is the SAME question DevRepoConfigured answers — that function IS the one
// reader of the env-vs-settings precedence, so this one delegates instead of
// opening a second path to the same key (Rule 14). An empty value means "no
// delivery point is declared", which is not a defect: it is the single-node
// shape, and every caller treats it as the fail-soft arm it always was.
func DeliveryRepoDeclared() (string, string, error) {
	return DevRepoConfigured()
}

// DeliveryReposTable answers the settings.json `repos` table's OWN keys — the
// per-repo merge/build contract's paths (see ResolveMergerRepos, which reads
// the same table for the merger's sweep list). Two readers, one parse: the
// question here is "which repository does this host treat as its system
// repo", never "which repos shall be swept", so it returns the table rather
// than a sorted sweep list.
//
// An unreadable settings file is an ERROR, not an empty table: falling
// through to a fallback because the document could not be parsed is how a
// misconfiguration turns into a silent wrong destination (Rule 15).
func DeliveryReposTable() (map[string]RepoConfig, error) {
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		return nil, err
	}
	return cfg.Repos, nil
}
