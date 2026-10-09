package maplint

import (
	"errors"
	"strings"
	"testing"
)

// Expectations below are hand-derived from Python re.match semantics (match at
// the start, no implicit end anchor, backtracking, lookahead without a width).
// Python was not run to produce them.
type pyCase struct {
	in   string
	want bool
}

func runPyCases(t *testing.T, pattern string, cases []pyCase) {
	t.Helper()
	re, err := compilePython(pattern)
	if err != nil {
		t.Fatalf("compilePython(%q): %v", pattern, err)
	}
	for _, c := range cases {
		if got := re.MatchString(c.in); got != c.want {
			t.Errorf("re.match(%q, %q) = %v, want %v", pattern, c.in, got, c.want)
		}
	}
}

// door_name_capitalization.yml.
func TestLookaheadDoorNames(t *testing.T) {
	runPyCases(t, `.*\s(?!of|and|to)[a-z].*`, []pyCase{
		{"Airlock of Doom", false},   // "of" blocked, then "Doom" is upper-case
		{"Airlock door", true},       // "door" starts with none of of/and/to
		{"Airlock Door", false},      // upper-case D
		{"door", false},              // no whitespace
		{"Big top", false},           // "to" is a prefix of "top"
		{"Big Top", false},           // upper-case
		{"Big and", false},           // "and"
		{"Big android", false},       // "and" is a prefix of "android"
		{"Big tool Box", false},      // "tool" blocked by "to", "Box" upper-case
		{"Big office", false},        // "of" is a prefix of "office"
		{"a b", true},                // single lower-case word after a space
		{" x", true},                 // leading whitespace, empty .*
		{"A  b", true},               // first space followed by a space, second by "b"
		{"A\tb", true},               // \s matches a tab
		{"A 1", false},               // digit is not [a-z]
		{"Of the Gods", true},        // lookahead is case sensitive; "the" passes
		{"Hall of the Mighty", true}, // first space blocked, second ("the") passes: backtracking
		{"Hall of Mighty and Power", false},
		{"", false},
		{"A\nb", true},    // \s matches the newline itself
		{"A\nB c", false}, // . does not cross \n, so the later space is unreachable
		{"x y", true},
		{"x o", true},      // "o" alone is not "of"
		{"x of", false},    // blocked
		{"x t", true},      // "t" alone is not "to"
		{"x ta", true},     // "ta" is not "to"
		{"x to", false},    // blocked
		{"x an", true},     // "an" is not "and"
		{"x ünï", false},   // non-ASCII is not [a-z]
		{"ü b", true},      // non-ASCII before the space is consumed by .*
		{"Big  of", false}, // two spaces: first followed by " of" (space fails), second by "of" blocked
	})
}

// window_spawner.yml, first pattern.
func TestLookaheadStructurePaths(t *testing.T) {
	runPyCases(t, `^/obj/structure/(?!.*/directional).*$`, []pyCase{
		{"/obj/structure/window", true},
		{"/obj/structure/window/directional", false},
		{"/obj/structure/", true},
		{"/obj/structure", false},
		{"/obj/structurex/y", false},
		{"/obj/structure/directional", true}, // the lookahead needs a slash before "directional"
		{"/obj/structure/a/directional/b", false},
		{"/obj/structure/a/directionalx", false}, // prefix match: no end anchor in the lookahead
		{"/obj/structure/a/directiona", true},
		{"/obj/structure/window/dir", true},
		{"obj/structure/x", false},
		{"/obj/structure/window/reinforced/fulltile", true},
		{"/obj/structure/window/fulltile/directional", false},
		{"/obj/structure/grille", true},
		{"/obj/structure/a/b/c", true},
		{"/obj/structures/a", false},
		{"/obj/machinery/x", false},
		{"", false},
	})
}

// window_spawner.yml, second pattern: two consecutive lookaheads.
func TestLookaheadTwoConsecutive(t *testing.T) {
	runPyCases(t, `^/obj/effect/spawner/structure/(?!.*/hollow)(?!.*/directional).*$`, []pyCase{
		{"/obj/effect/spawner/structure/window", true},
		{"/obj/effect/spawner/structure/window/hollow", false},
		{"/obj/effect/spawner/structure/window/directional", false},
		{"/obj/effect/spawner/structure/window/hollow/directional", false},
		{"/obj/effect/spawner/structure/hollow", true},
		{"/obj/effect/spawner/structure/directional", true},
		{"/obj/effect/spawner/structure/window/reinforced", true},
		{"/obj/effect/spawner/structure/electrified_grille", true},
		{"/obj/effect/spawner/structure", false},
		{"/obj/effect/spawner/structure/a/hollow/tinted", false},
		{"/obj/effect/spawner/structure/a/direction", true},
		{"/obj/effect/spawner/structure/a/hollowx", false},
		{"/obj/effect/spawner/structure/a/hollo", true},
		{"/obj/effect/spawner/other/a", false},
		{"/obj/structure/window", false},
		{"/obj/effect/spawner/structure/", true},
	})
}

func TestLookaheadGeneral(t *testing.T) {
	runPyCases(t, `ab(?!c)|xy`, []pyCase{
		{"abd", true}, {"abc", false}, {"ab", true}, {"xyz", true}, {"x", false}, {"zab", false}, {"abcxy", false},
	})
	runPyCases(t, `(?!a)b`, []pyCase{{"b", true}, {"ab", false}, {"c", false}, {"", false}, {"bb", true}})
	runPyCases(t, `a(?!b)`, []pyCase{{"ab", false}, {"ac", true}, {"a", true}, {"ba", false}, {"aab", true}, {"", false}})
	// Backtracking over split positions: a* may give back characters.
	runPyCases(t, `a*(?!a)b`, []pyCase{{"aab", true}, {"aaab", true}, {"b", true}, {"aac", false}, {"", false}, {"aba", true}})
	runPyCases(t, `\d+(?!\d)x`, []pyCase{{"12x", true}, {"1x", true}, {"12", false}, {"x", false}, {"12y", false}})
	runPyCases(t, `a.*(?!b)c`, []pyCase{{"abc", true}, {"ac", true}, {"ab", false}, {"abbc", true}, {"bc", false}})
	runPyCases(t, `a+(?!a)`, []pyCase{{"aaa", true}, {"b", false}, {"ab", true}})
	// Groups inside the lookahead body.
	runPyCases(t, `x(?!(?:ab|cd)e)..`, []pyCase{
		{"xabe", false}, {"xabf", true}, {"xcde", false}, {"xcdf", true}, {"xab", true}, {"xa", false}, {"yabf", false},
	})
	// `$` inside a lookahead body refers to the real end of the text.
	runPyCases(t, `a(?!$)`, []pyCase{{"a", false}, {"ab", true}, {"ba", false}, {"aa", true}})
	// A single-character class holding "(?!" is not a lookahead.
	runPyCases(t, `[(?!]x(?!y)`, []pyCase{{"(x", true}, {"(xy", false}, {"!xz", true}, {"ax", false}, {"?x", true}})
	// Escaped parenthesis: not a lookahead at all, plain RE2 path.
	runPyCases(t, `a\(?!b`, []pyCase{{"a!b", true}, {"a(!b", true}, {"a!c", false}, {"a((!b", false}})
	// Escaped backslash before a real lookahead.
	runPyCases(t, `a\\(?!b)`, []pyCase{{`a\`, true}, {`a\b`, false}, {`a\c`, true}, {`a`, false}})
	// Alternation inside a group is not a top-level split.
	runPyCases(t, `(?:ab|cd)(?!e)`, []pyCase{{"abe", false}, {"abf", true}, {"cd", true}, {"cde", false}, {"ef", false}})
	// A lookahead-free pattern is unchanged by this matcher.
	runPyCases(t, `^/obj/pipe`, []pyCase{{"/obj/pipe/x", true}, {"x/obj/pipe", false}})
}

func TestLookaheadUnsupportedForms(t *testing.T) {
	for _, pattern := range []string{
		`(?=a)b`,        // positive lookahead
		`a(?=b)`,        // positive lookahead after text
		`(?<=a)b`,       // lookbehind
		`(?<!a)b`,       // negative lookbehind
		`(a(?!b))c`,     // lookahead nested in a group
		`(?:a(?!b))*c`,  // lookahead nested in a repeated group
		`a(?!(?!b)c)d`,  // lookahead nested in a lookahead
		`(a)\1`,         // backreference
		`a(?!b)*`,       // quantified lookahead
		`a(?!b)?c`,      // quantified lookahead
		`a(?!b){2}`,     // quantified lookahead
		`(?i)a(?!b)`,    // global flags with a lookahead
		`a(?!b`,         // unterminated
		`a$(?!b)c`,      // end anchor before a later piece
		`a\b(?!x)`,      // word boundary needs surrounding context
		`a(?!x)b\b`,     // word boundary in a later piece
		`a(?!^b)`,       // start anchor inside a lookahead
		`a(?!x)^b`,      // start anchor in a later piece
		`a(?!\bx)`,      // word boundary inside a lookahead
		`a(?!x)[`,       // invalid class
		`a(?!x))`,       // unbalanced
		`a(?!b)(?P=n)c`, // named backreference
		`a(?!b)(?<!c)d`, // lookbehind after lookahead
		`a(?!b)(?=c)d`,  // positive lookahead after lookahead
		`a(?!(?<=b)c)d`, // lookbehind in lookahead body
		`a(?!x)(?i:b)`,  // scoped flags with a lookahead are not split safely
	} {
		_, err := compilePython(pattern)
		var u unsupportedDetail
		if err == nil || !errors.As(err, &u) {
			t.Errorf("compilePython(%q) = %v, want unsupported", pattern, err)
		}
	}
	// The error names the offending pattern.
	_, err := compilePython(`(?=a)b`)
	if err == nil || !strings.Contains(err.Error(), `(?=a)b`) {
		t.Fatalf("error text = %v", err)
	}
}

func TestLookaheadRulesLoad(t *testing.T) {
	rs, errs := writeRules(t, map[string]string{
		"door.yml": "/obj/machinery/door:\n  banned_variables:\n    name:\n      deny: { pattern: '.*\\s(?!of|and|to)[a-z].*' }\n",
		"spawner.yml": `
/obj/effect/spawner/structure/window:
  banned_neighbors:
    /turf/closed:
    STRUCTURE:
      pattern: ^/obj/structure/(?!.*/directional).*$
    STRUCTURE_SPAWNER:
      pattern: ^/obj/effect/spawner/structure/(?!.*/hollow)(?!.*/directional).*$
`,
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if rs.RuleCount() != 2 {
		t.Fatalf("rules = %d", rs.RuleCount())
	}
	got := rs.CheckTile([]Atom{atom("/obj/machinery/door", "name", `"Airlock door"`)})
	if len(got) != 1 || got[0].Kind != KindBannedVariable {
		t.Fatalf("door violation = %+v", got)
	}
	if got := rs.CheckTile([]Atom{atom("/obj/machinery/door", "name", `"Airlock Door"`)}); len(got) != 0 {
		t.Fatalf("capitalised door flagged: %+v", got)
	}
	spawner := atom("/obj/effect/spawner/structure/window")
	got = rs.CheckTile([]Atom{spawner, atom("/obj/structure/window")})
	if len(got) != 1 || got[0].Kind != KindBannedNeighbor || got[0].Neighbor != "/obj/structure/window" {
		t.Fatalf("spawner/window = %+v", got)
	}
	if got := rs.CheckTile([]Atom{spawner, atom("/obj/structure/window/directional")}); len(got) != 0 {
		t.Fatalf("directional window flagged: %+v", got)
	}
	got = rs.CheckTile([]Atom{spawner, atom("/obj/effect/spawner/structure/window/hollow")})
	// Only the hollow spawner (a window subtype) is flagged, by the plain
	// window; the plain window must not be flagged by the hollow one.
	if len(got) != 1 || got[0].AtomIndex != 1 {
		t.Fatalf("hollow spawner = %+v", got)
	}
	got = rs.CheckTile([]Atom{spawner, atom("/obj/effect/spawner/structure/window/reinforced")})
	if len(got) != 2 { // each window spawner is a subject with the other as neighbor
		t.Fatalf("spawner pair = %+v", got)
	}
}
