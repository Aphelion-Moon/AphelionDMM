package maplint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRules writes synthetic rule files into a temp dir and loads them.
func writeRules(t *testing.T, files map[string]string) (*RuleSet, []error) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Load(dir)
}

func mustLoad(t *testing.T, files map[string]string) *RuleSet {
	t.Helper()
	rs, errs := writeRules(t, files)
	if len(errs) != 0 {
		t.Fatalf("unexpected load errors: %v", errs)
	}
	return rs
}

func atom(path string, kv ...string) Atom {
	a := Atom{Path: path}
	if len(kv) > 0 {
		a.Vars = map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			a.Vars[kv[i]] = kv[i+1]
		}
	}
	return a
}

func messages(vs []Violation) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.Message)
	}
	return out
}

func expectMsgs(t *testing.T, got []Violation, want ...string) {
	t.Helper()
	g := messages(got)
	if len(g) != len(want) {
		t.Fatalf("got %d violations %q, want %d %q", len(g), g, len(want), want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("violation %d = %q, want %q", i, g[i], want[i])
		}
	}
}

// TypepathExtra.matches_path (lint.py): subtype prefix by segment, "=" exact, "*" wildcard.
func TestTypepathMatching(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"sub.yml":   "/mob/dog:\n  banned: true\n",
		"exact.yml": "=/obj/item:\n  banned: true\n",
		"wild.yml":  "\"*\":\n  banned_variables:\n    - step_x\n",
	})
	cases := []struct {
		path string
		want int
	}{
		{"/mob/dog", 1},
		{"/mob/dog/corgi", 1},
		{"/mob/doggo", 0}, // segments, not string prefix
		{"/mob", 0},       // shorter than rule
		{"/obj/item", 1},
		{"/obj/item/sword", 0}, // exact
	}
	for _, c := range cases {
		got := rs.CheckTile([]Atom{atom(c.path)})
		if len(got) != c.want {
			t.Errorf("%s: got %d violations (%q), want %d", c.path, len(got), messages(got), c.want)
		}
	}
	got := rs.CheckTile([]Atom{atom("/anything/at/all", "step_x", "1")})
	expectMsgs(t, got, "Typepath /anything/at/all has a banned variable (set to 1.0): step_x. This variable is not allowed for this type.")
}

// Rules.run banned (lint.py): "Typepath {path} is banned{when}." plus Lint.help.
func TestBannedAndHelp(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"b.yml": "help: Don't do it,\n  really.\n/mob/dog/pug:\n  banned: TRUE\n",
	})
	got := rs.CheckTile([]Atom{atom("/mob/dog/pug"), atom("/turf/open/floor")})
	expectMsgs(t, got, "Typepath /mob/dog/pug is banned.")
	v := got[0]
	if v.RuleFile != "b.yml" || v.Help != "Don't do it, really." || v.Kind != KindBanned || v.AtomIndex != 0 || v.Subject != "/mob/dog/pug" {
		t.Fatalf("bad violation metadata: %+v", v)
	}
}

// AtomNeighbor.matches / Rules.run banned_neighbors (lint.py).
func TestBannedNeighborsListForm(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"n.yml": "/obj/structure/window:\n  banned_neighbors:\n    - /obj/structure/grille\n    - =/obj/machinery/x\n",
	})
	// Subtypes of a listed neighbor match; "=" is exact.
	got := rs.CheckTile([]Atom{atom("/obj/structure/window"), atom("/obj/structure/grille/electric"), atom("/obj/machinery/x/y"), atom("/obj/machinery/x")})
	expectMsgs(t, got,
		"Typepath /obj/structure/window has a banned path on the same tile: /obj/structure/grille/electric",
		"Typepath /obj/structure/window has a banned path on the same tile: /obj/machinery/x",
	)
	if got[0].Neighbor != "/obj/structure/grille/electric" || got[0].NeighborIndex != 1 {
		t.Fatalf("bad neighbor metadata: %+v", got[0])
	}
}

// Neighbor list excludes the atom itself (contents[:i] + contents[i+1:]), so
// two of the same type are required to trigger "multiple_*" rules.
func TestSelfExcludedFromNeighbors(t *testing.T) {
	rs := mustLoad(t, map[string]string{"m.yml": "/obj/structure/table:\n  banned_neighbors:\n    - /obj/structure/table\n"})
	if got := rs.CheckTile([]Atom{atom("/obj/structure/table")}); len(got) != 0 {
		t.Fatalf("single table flagged: %q", messages(got))
	}
	got := rs.CheckTile([]Atom{atom("/obj/structure/table"), atom("/obj/structure/table/wood")})
	// Both atoms match the rule, each sees the other.
	expectMsgs(t, got,
		"Typepath /obj/structure/table has a banned path on the same tile: /obj/structure/table/wood",
		"Typepath /obj/structure/table/wood has a banned path on the same tile: /obj/structure/table",
	)
}

// AtomNeighbor with identical: true (lint.py): same path and same var_edits; typepath is not consulted.
func TestIdenticalNeighbors(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"i.yml": "/obj/structure/window:\n  banned_neighbors:\n    /obj/structure/window:\n      identical: true\n",
	})
	a := atom("/obj/structure/window", "dir", "4")
	same := atom("/obj/structure/window", "dir", "4.0") // 4 == 4.0 once parsed as float
	diffVar := atom("/obj/structure/window", "dir", "8")
	diffPath := atom("/obj/structure/window/reinforced", "dir", "4")
	noVars := atom("/obj/structure/window")

	if got := rs.CheckTile([]Atom{a, same}); len(got) != 2 {
		t.Fatalf("identical (numeric-normalised) not flagged: %q", messages(got))
	}
	for name, other := range map[string]Atom{"var": diffVar, "path": diffPath, "novars": noVars} {
		if got := rs.CheckTile([]Atom{a, other}); len(got) != 0 {
			t.Errorf("%s: unexpectedly flagged %q", name, messages(got))
		}
	}
	// String vs number are different constants.
	if got := rs.CheckTile([]Atom{atom("/obj/structure/window", "x", "\"4\""), atom("/obj/structure/window", "x", "4")}); len(got) != 0 {
		t.Fatalf("string 4 equated to number 4")
	}
	if got := rs.CheckTile([]Atom{atom("/obj/structure/window", "x", "\"ab\""), atom("/obj/structure/window", "x", "\"ab\"")}); len(got) != 2 {
		t.Fatalf("equal strings not identical")
	}
}

// AtomNeighbor pattern (lint.py): re.match semantics, anchored at the start, on str(path).
// Label keys (all upper case) have no typepath.
func TestPatternNeighbors(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"p.yml": "/obj/structure/grille:\n  banned_neighbors:\n    FULLTILE_WINDOW:\n      pattern: ^/obj/structure/window[/\\w]*?fulltile$\n    PREFIX:\n      pattern: /turf/closed\n",
	})
	got := rs.CheckTile([]Atom{atom("/obj/structure/grille"), atom("/obj/structure/window/reinforced/fulltile"), atom("/obj/structure/window/reinforced")})
	expectMsgs(t, got, "Typepath /obj/structure/grille has a banned path on the same tile: /obj/structure/window/reinforced/fulltile")
	// re.match anchors at start but not at end.
	got = rs.CheckTile([]Atom{atom("/obj/structure/grille"), atom("/turf/closed/wall")})
	expectMsgs(t, got, "Typepath /obj/structure/grille has a banned path on the same tile: /turf/closed/wall")
	// Not anchored-at-end, but anchored at start: pattern must not match mid-string.
	got = rs.CheckTile([]Atom{atom("/obj/structure/grille"), atom("/x/turf/closed/wall")})
	expectMsgs(t, got)
}

// Rules.ignored_neighbors (lint.py): if ANY other atom matches an ignore,
// every banned_neighbors entry of the rule is skipped (upstream behaviour).
func TestIgnoreSkipsAllBannedNeighbors(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"ig.yml": "/turf/wall:\n  banned_neighbors:\n    - /obj/structure\n  ignore:\n    - /obj/structure/sign\n",
	})
	expectMsgs(t, rs.CheckTile([]Atom{atom("/turf/wall"), atom("/obj/structure/chair")}),
		"Typepath /turf/wall has a banned path on the same tile: /obj/structure/chair")
	// A sign present anywhere on the tile disables the whole banned_neighbors check.
	expectMsgs(t, rs.CheckTile([]Atom{atom("/turf/wall"), atom("/obj/structure/chair"), atom("/obj/structure/sign/poster")}))
}

// Rules.required_neighbors (lint.py): one failure per missing entry, message uses to_string().
func TestRequiredNeighbors(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"r.yml": "/obj/machinery/disposal:\n  required_neighbors:\n    /obj/structure/disposalpipe/trunk:\n    PIPE:\n      pattern: ^/obj/pipe\n",
	})
	got := rs.CheckTile([]Atom{atom("/obj/machinery/disposal")})
	expectMsgs(t, got,
		"Typepath /obj/machinery/disposal is missing a required neighbor: /obj/structure/disposalpipe/trunk",
		"Typepath /obj/machinery/disposal is missing a required neighbor: ^/obj/pipe",
	)
	if got[0].Kind != KindRequiredNeighbor {
		t.Fatalf("kind = %v", got[0].Kind)
	}
	got = rs.CheckTile([]Atom{atom("/obj/machinery/disposal"), atom("/obj/structure/disposalpipe/trunk/x"), atom("/obj/pipe/a")})
	expectMsgs(t, got)
}

// Rules.run banned_variables: true (lint.py).
func TestBannedVariablesTrue(t *testing.T) {
	rs := mustLoad(t, map[string]string{"v.yml": "/obj/structure/cable:\n  banned_variables: true\n"})
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/structure/cable")}))
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/structure/cable", "d1", "1")}),
		"Typepath /obj/structure/cable should not have any variable edits.")
}

// BannedVariable.run (lint.py): bare, allow list/pattern, deny list/pattern.
func TestBannedVariables(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"v.yml": `
/obj/machinery/power/apc:
  banned_variables:
    pixel_x:
      allow: [25, -25]
/obj/a:
  banned_variables:
    bare:
    dir_unused:
/obj/c:
  banned_variables:
    - listed
/obj/b:
  banned_variables:
    dir:
      deny: [1, 2, 4, 8]
    name:
      allow: { pattern: "^[A-Z].*$" }
    tag:
      deny: { pattern: "^bad" }
    both:
      allow: ["x"]
      deny: ["x"]
    key:
      allow: ["a", 3]
`,
	})
	// allow list: floats compared numerically; message lists str(float).
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/machinery/power/apc", "pixel_x", "25")}))
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/machinery/power/apc", "pixel_x", "-25")}))
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/machinery/power/apc", "pixel_x", "24")}),
		"Typepath /obj/machinery/power/apc has a banned variable (set to 24.0): pixel_x. Must be one of 25.0, -25.0")
	// Unset variables are never flagged.
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/machinery/power/apc")}))

	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/c", "listed", "1")}),
		"Typepath /obj/c has a banned variable (set to 1.0): listed. This variable is not allowed for this type.")
	// Bare entry.
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/a", "bare", "\"x\"")}),
		"Typepath /obj/a has a banned variable (set to x): bare. This variable is not allowed for this type.")
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/a", "dir_unused", "null")}),
		"Typepath /obj/a has a banned variable (set to null): dir_unused. This variable is not allowed for this type.")

	// deny list.
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/b", "dir", "4")}),
		"Typepath /obj/b has a banned variable (set to 4.0): dir. Must not be one of 1.0, 2.0, 4.0, 8.0")
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/b", "dir", "5")}))
	// allow pattern uses re.match on str(value) (string content without quotes).
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/b", "name", "\"Door\"")}))
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/b", "name", "\"door\"")}),
		"Typepath /obj/b has a banned variable (set to door): name. Must match ^[A-Z].*$")
	// deny pattern.
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/b", "tag", "\"badness\"")}),
		"Typepath /obj/b has a banned variable (set to badness): tag. Must not match ^bad")
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/b", "tag", "\"good\"")}))
	// allow wins over deny: when allow passes, deny is not consulted.
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/b", "both", "\"x\"")}))
	// mixed list: strings compare to strings, numbers to numbers, never across.
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/b", "key", "\"a\"")}))
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/b", "key", "3")}))
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/b", "key", "\"3\"")}),
		"Typepath /obj/b has a banned variable (set to 3): key. Must be one of a, 3.0")
}

// When / WhenCondition / WhenGroup (lint.py).
func TestWhen(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"w.yml": `
/obj/airlock:
  when:
    - req_access is set
  banned_neighbors:
    - /obj/helper
/obj/dog:
  when:
    - any:
        - breed is 'lab'
        - breed is 'pug'
    - owner is not set
  banned: true
/obj/cat:
  when:
    - all:
        - age is '3'
        - name like '[A-Z]x'
        - mood is not 'sad'
  banned: true
`,
	})
	helper := atom("/obj/helper")
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/airlock"), helper}))
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/airlock", "req_access", "\"1\""), helper}),
		"Typepath /obj/airlock has a banned path on the same tile when req_access is set: /obj/helper")

	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/dog", "breed", "\"pug\"")}), "Typepath /obj/dog is banned when (breed is 'lab' or breed is 'pug') and owner is not set.")
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/dog", "breed", "\"corgi\"")}))
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/dog", "breed", "\"lab\"", "owner", "\"bob\"")}))

	// "is '3'" on a float with integral value compares str(int(v)); like uses re.match on str(v).
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/cat", "age", "4", "name", "\"Ax1\"")}))
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/cat", "age", "3.0", "name", "\"Ax1\"", "mood", "\"happy\"")}),
		"Typepath /obj/cat is banned when age is '3' and name like '[A-Z]x' and mood is not 'sad'.")
	expectMsgs(t, rs.CheckTile([]Atom{atom("/obj/cat", "age", "3", "name", "\"Ax1\"", "mood", "\"sad\"")}))
}

func TestWhenIsEqualQuirks(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"w.yml": "/obj/a:\n  when:\n    - n is '2'\n    - f is '2.5'\n  banned: true\n",
	})
	if got := rs.CheckTile([]Atom{atom("/obj/a", "n", "2.0", "f", "2.5")}); len(got) != 1 {
		t.Fatalf("expected violation for n=2.0,f=2.5: %q", messages(got))
	}
	if got := rs.CheckTile([]Atom{atom("/obj/a", "n", "\"2\"", "f", "2.5")}); len(got) != 1 {
		t.Fatalf("string \"2\" should equal '2'")
	}
	if got := rs.CheckTile([]Atom{atom("/obj/a", "f", "2.5")}); len(got) != 0 {
		t.Fatalf("missing var must not match 'is'")
	}
}

// Rules.skip_files (lint.py): substring of the normalised filename, or regex search.
func TestSkipFiles(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"s.yml": "/area/noop:\n  banned: true\n  skip_files:\n    - _maps/templates\n    - pattern: \"^_maps/holo.*\"\n",
	})
	a := []Atom{atom("/area/noop")}
	if got := rs.CheckTileInFile("", a); len(got) != 1 {
		t.Fatal("no file: should check")
	}
	if got := rs.CheckTileInFile("_maps/map_files/x.dmm", a); len(got) != 1 {
		t.Fatal("unrelated file should check")
	}
	if got := rs.CheckTileInFile(`C:\repo\_maps\templates\a.dmm`, a); len(got) != 0 {
		t.Fatal("backslashes normalised, substring match should skip")
	}
	if got := rs.CheckTileInFile("_maps/holodeck/a.dmm", a); len(got) != 0 {
		t.Fatal("regex skip")
	}
	if got := rs.CheckTileInFile("x/_maps/holodeck/a.dmm", a); len(got) != 1 {
		t.Fatal("regex is anchored by its own ^")
	}
}

// Unknown keys, unsupported (positive) lookaheads and malformed rules.
func TestUnsupportedAndErrors(t *testing.T) {
	rs, errs := writeRules(t, map[string]string{
		"good.yml": "/mob/a:\n  banned: true\n",
		"mixed.yml": `
/mob/ok:
  banned: true
/mob/future:
  banned: true
  frobnicate: 3
/mob/look:
  banned_variables:
    name:
      deny: { pattern: '.*\s(?=of|and)[a-z].*' }
`,
		"broken.yml":  "/mob/x: [unterminated\n",
		"empty.yml":   "",
		"badtype.yml": "mob/nopath:\n  banned: true\n",
		"badflag.yml": "/mob/q:\n  banned: maybe\n",
	})
	var unsupported []*UnsupportedError
	var loadErrs []*LoadError
	for _, e := range errs {
		switch x := e.(type) {
		case *UnsupportedError:
			unsupported = append(unsupported, x)
		case *LoadError:
			loadErrs = append(loadErrs, x)
		default:
			t.Fatalf("unexpected error type %T", e)
		}
	}
	if len(unsupported) != 2 {
		t.Fatalf("unsupported = %v", errs)
	}
	for _, u := range unsupported {
		if u.File != "mixed.yml" || (u.Rule != "/mob/future" && u.Rule != "/mob/look") {
			t.Fatalf("unexpected unsupported %+v", u)
		}
	}
	if !strings.Contains(unsupported[0].Error(), "mixed.yml") {
		t.Fatalf("error text lacks file: %v", unsupported[0])
	}
	if len(loadErrs) != 4 {
		t.Fatalf("load errors = %v", loadErrs)
	}
	if len(rs.Unsupported()) != 2 {
		t.Fatalf("RuleSet.Unsupported() = %v", rs.Unsupported())
	}
	// Supported parts still load; unsupported rules are dropped, not guessed.
	expectMsgs(t, rs.CheckTile([]Atom{atom("/mob/a"), atom("/mob/ok")}),
		"Typepath /mob/a is banned.", "Typepath /mob/ok is banned.")
	expectMsgs(t, rs.CheckTile([]Atom{atom("/mob/future")}))
}

func TestLoadMissingDir(t *testing.T) {
	rs, errs := Load(filepath.Join(t.TempDir(), "nope"))
	if len(errs) == 0 || rs == nil {
		t.Fatalf("expected error and non-nil ruleset, got %v %v", rs, errs)
	}
}

// CheckPlacement reports only violations introduced by the placed atom,
// including ones whose subject is an existing atom (e.g. a wall gaining an airlock).
func TestCheckPlacement(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"a.yml": "/obj/structure/window:\n  banned_neighbors:\n    /obj/structure/window:\n      identical: true\n",
		"b.yml": "/turf/closed:\n  banned_neighbors:\n    - /obj/machinery/door/airlock\n",
		"c.yml": "/obj/structure/table:\n  banned_neighbors:\n    - /obj/structure/table\n",
	})
	// Pre-existing violation is not attributed to the new placement.
	existing := []Atom{atom("/obj/structure/table"), atom("/obj/structure/table")}
	expectMsgs(t, rs.CheckPlacement(existing, atom("/obj/item/pen")))

	// New identical window.
	w := atom("/obj/structure/window", "dir", "2")
	got := rs.CheckPlacement([]Atom{w}, w)
	if len(got) != 2 {
		t.Fatalf("got %q", messages(got))
	}
	// Existing subject, placed neighbor.
	got = rs.CheckPlacement([]Atom{atom("/turf/closed/wall")}, atom("/obj/machinery/door/airlock"))
	expectMsgs(t, got, "Typepath /turf/closed/wall has a banned path on the same tile: /obj/machinery/door/airlock")
	if got[0].AtomIndex != 0 || got[0].NeighborIndex != 1 {
		t.Fatalf("indices %+v", got[0])
	}
	// Does not mutate the caller's slice.
	ex := make([]Atom, 1, 8)
	ex[0] = atom("/turf/closed/wall")
	_ = rs.CheckPlacement(ex, atom("/obj/machinery/door/airlock"))
	if len(ex[:2]) == 2 && ex[:2][1].Path != "" {
		t.Fatal("CheckPlacement wrote into caller's backing array")
	}
}

func TestDeterministicOrder(t *testing.T) {
	rs := mustLoad(t, map[string]string{
		"b.yml": "/mob/a:\n  banned: true\n",
		"a.yml": "/mob/a:\n  banned: true\nhelp: first\n",
	})
	for i := 0; i < 20; i++ {
		got := rs.CheckTile([]Atom{atom("/mob/a")})
		if len(got) != 2 || got[0].RuleFile != "a.yml" || got[1].RuleFile != "b.yml" {
			t.Fatalf("order: %+v", got)
		}
	}
}

func TestFindLintDir(t *testing.T) {
	root := t.TempDir()
	dme := filepath.Join(root, "game.dme")
	if _, ok := FindLintDir(dme); ok {
		t.Fatal("found nonexistent")
	}
	lints := filepath.Join(root, "tools", "maplint", "lints")
	if err := os.MkdirAll(lints, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := FindLintDir(dme)
	if !ok || got != lints {
		t.Fatalf("FindLintDir = %q, %v", got, ok)
	}
	if _, ok := FindLintDir(""); ok {
		t.Fatal("empty path")
	}
}

func TestConstantParsing(t *testing.T) {
	cases := []struct {
		in   string
		str  string
		kind constKind
	}{
		{"25", "25.0", constNum},
		{"-0.5", "-0.5", constNum},
		{"1e20", "1e+20", constNum},
		{"\"hi\"", "hi", constStr},
		{"/obj/a", "/obj/a", constPath},
		{"null", "null", constNull},
		{"'icons/a.dmi'", "icons/a.dmi", constFile},
		{"list(1,2)", "['NYI: list']", constList},
		{"weird(", "weird(", constRaw},
	}
	for _, c := range cases {
		k := parseConstant(c.in)
		if k.kind != c.kind || k.pyStr() != c.str {
			t.Errorf("%q => kind %v str %q; want %v %q", c.in, k.kind, k.pyStr(), c.kind, c.str)
		}
	}
}

func BenchmarkCheckPlacement(b *testing.B) {
	dir := b.TempDir()
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("/obj/thing" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ":\n  banned_neighbors:\n    - /obj/other\n")
	}
	sb.WriteString("/obj/structure/window:\n  banned_neighbors:\n    /obj/structure/window:\n      identical: true\n")
	if err := os.WriteFile(filepath.Join(dir, "r.yml"), []byte(sb.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	rs, errs := Load(dir)
	if len(errs) != 0 {
		b.Fatal(errs)
	}
	existing := []Atom{atom("/turf/open/floor"), atom("/obj/structure/window", "dir", "2"), atom("/obj/item/pen")}
	placed := atom("/obj/structure/window", "dir", "2")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = rs.CheckPlacement(existing, placed)
	}
}
