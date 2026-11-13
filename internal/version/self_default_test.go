// self_default_test.go — the pin under the version constant.
//
// self.go's doc comment says "a test pins that this default satisfies it — a
// malformed default is the one failure nobody would notice until a release".
// This is that test: it reads DefaultVersion (and Version, which a plain build
// must also carry) through Validate, so a bump that mistypes the value fails
// here — at `make test-budget ./internal/version/` — instead of in a shipped
// binary whose `gopi version` top line is junk.
//
// WHY THE BUMP ITSELF IS NOT ASSERTED: the value moves manually, in the commit
// that earns it (devdoc/version-identity.md § the bump log). A test pinning a
// LITERAL "v0.2.0" would turn every future bump into a two-file dance whose
// forgetting half is invisible; the FORMAT is the invariant, the value is the
// author's decision.
package version

import "testing"

func TestDefaultVersionIsWellFormed(t *testing.T) {
	if DefaultVersion == "" {
		t.Fatal("DefaultVersion is empty; a binary built with no ldflags would print a blank top line")
	}
	if err := Validate(DefaultVersion); err != nil {
		t.Fatalf("DefaultVersion = %q is malformed: %v", DefaultVersion, err)
	}
	if Version != DefaultVersion {
		t.Fatalf("Version = %q but DefaultVersion = %q: a build with no ldflags must carry the tree's default",
			Version, DefaultVersion)
	}
}
