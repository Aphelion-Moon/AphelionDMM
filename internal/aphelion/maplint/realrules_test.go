package maplint

import (
	"os"
	"testing"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

const realLintDir = `C:\Users\Zoe\.codex\worktrees\18e1\Meridian-Rift\tools\maplint\lints`

// Loads the real repository rules when present and logs exactly which rules
// are unsupported. Load errors (as opposed to unsupported rules) fail the test.
func TestRealRepositoryRules(t *testing.T) {
	dir := realLintDir
	if env := os.Getenv("APHELION_MAPLINT_LINTS"); env != "" {
		dir = env
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Skip("real lints directory not present")
	}
	rs, errs := Load(dir)
	for _, e := range errs {
		if _, ok := e.(*UnsupportedError); ok {
			t.Logf("UNSUPPORTED: %v", e)
			continue
		}
		t.Errorf("load error: %v", e)
	}
	t.Logf("loaded %d files, %d rules, %d unsupported", len(rs.Files()), rs.RuleCount(), len(rs.Unsupported()))
	// The regex lookahead subset is implemented, so every shipped rule loads\n	// (52 loaded before lookahead support, plus the 2 that used it).
	if rs.RuleCount() != 54 || len(rs.Unsupported()) != 0 {
		t.Errorf("loaded %d rules with %d unsupported; want 54 rules and none unsupported", rs.RuleCount(), len(rs.Unsupported()))
	}

	// Shipped-rule smoke checks.
	got := rs.CheckTile([]Atom{atom("/turf/open/floor"), atom("/turf/open/floor/plating")})
	if len(got) == 0 || got[0].RuleFile != "multiple_turf.yml" {
		t.Errorf("multiple_turf did not fire: %+v", got)
	}
	got = rs.CheckPlacement([]Atom{atom("/obj/structure/window", "dir", "2")}, atom("/obj/structure/window", "dir", "2"))
	if len(got) == 0 {
		t.Errorf("multiple_windows did not fire")
	}
	if got := rs.CheckTile([]Atom{atom("/obj/structure/cable", "d1", "1")}); len(got) == 0 {
		t.Errorf("cable_varedits did not fire")
	}
}

func TestAdapterFromPrefab(t *testing.T) {
	var mv dmvars.MutableVariables
	mv.Put("dir", "2")
	mv.Put("name", `"x"`)
	p := dmmprefab.New(dmmprefab.IdNone, "/obj/structure/window", mv.ToImmutable())
	a := AtomFromPrefab(p)
	if a.Path != "/obj/structure/window" || a.Vars["dir"] != "2" || a.Vars["name"] != `"x"` || len(a.Vars) != 2 {
		t.Fatalf("bad atom %+v", a)
	}
	if b := AtomFromPrefab(dmmprefab.New(dmmprefab.IdNone, "/turf", nil)); b.Path != "/turf" || b.Vars != nil {
		t.Fatalf("nil vars: %+v", b)
	}
	if got := AtomsFromPrefabs([]*dmmprefab.Prefab{p, nil}); len(got) != 2 || got[1].Path != "" {
		t.Fatalf("slice: %+v", got)
	}
}
