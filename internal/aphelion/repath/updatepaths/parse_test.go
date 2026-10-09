package updatepaths

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseRules(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Rule
	}{
		{
			name: "plain",
			line: "/obj/old : /obj/new",
			want: Rule{Match: Pattern{Path: "/obj/old"}, Outputs: []Output{{Path: "/obj/new"}}},
		},
		{
			name: "keep vars",
			line: "/obj/old : /obj/new{@OLD}",
			want: Rule{Match: Pattern{Path: "/obj/old"}, Outputs: []Output{{Path: "/obj/new", Props: []Prop{{Kind: PropAllOld}}}}},
		},
		{
			name: "delete",
			line: "/obj/effect/landmark/start/virologist : @DELETE",
			want: Rule{Match: Pattern{Path: "/obj/effect/landmark/start/virologist"}, Delete: true},
		},
		{
			name: "multiple outputs",
			line: "/turf/a : /obj/effect/turf_decal {@OLD} , /obj/thing {icon_state = @OLD:name; name = \"meme\"}",
			want: Rule{Match: Pattern{Path: "/turf/a"}, Outputs: []Output{
				{Path: "/obj/effect/turf_decal", Props: []Prop{{Kind: PropAllOld}}},
				{Path: "/obj/thing", Props: []Prop{{Name: "icon_state", Kind: PropOldOf, From: "name"}, {Name: "name", Kind: PropLiteral, Value: `"meme"`}}},
			}},
		},
		{
			name: "subtypes",
			line: "/obj/structure/closet/crate/@SUBTYPES : /obj/structure/new_box/@SUBTYPES {@OLD}",
			want: Rule{Match: Pattern{Path: "/obj/structure/closet/crate", Subtypes: true}, Outputs: []Output{{Path: "/obj/structure/new_box", Kind: OutputSubtypes, Props: []Prop{{Kind: PropAllOld}}}}},
		},
		{
			name: "filters",
			line: "/mob/living{resize = @ANY; tag = @UNSET; name=\"Tom\"} : /mob/living{@OLD; resize = @SKIP; dir = @OLD}",
			want: Rule{
				Match:   Pattern{Path: "/mob/living", Filters: []Filter{{Name: "resize", Kind: FilterAny}, {Name: "tag", Kind: FilterUnset}, {Name: "name", Kind: FilterEquals, Value: `"Tom"`}}},
				Outputs: []Output{{Path: "/mob/living", Props: []Prop{{Kind: PropAllOld}, {Name: "resize", Kind: PropSkip}, {Name: "dir", Kind: PropOld}}}},
			},
		},
		{
			name: "no edits filter",
			line: "/obj/structure{@UNSET} : /obj/structure{dir=1}",
			want: Rule{Match: Pattern{Path: "/obj/structure", Filters: []Filter{{Kind: FilterNoEdits}}}, Outputs: []Output{{Path: "/obj/structure", Props: []Prop{{Name: "dir", Value: "1"}}}}},
		},
		{
			name: "keep old path",
			line: "/obj/a{dir = 2} : @OLD{dir = 1}",
			want: Rule{Match: Pattern{Path: "/obj/a", Filters: []Filter{{Name: "dir", Value: "2"}}}, Outputs: []Output{{Kind: OutputKeepPath, Props: []Prop{{Name: "dir", Value: "1"}}}}},
		},
		{
			name: "quoted separators",
			line: `/obj/a{name = "a:b,c;d{e}"} : /obj/b{desc = "x;y", list = list("k" = 1, "j" = 2)}`,
			want: Rule{
				Match:   Pattern{Path: "/obj/a", Filters: []Filter{{Name: "name", Value: `"a:b,c;d{e}"`}}},
				Outputs: []Output{{Path: "/obj/b", Props: []Prop{{Name: "desc", Value: `"x;y", list = list("k" = 1, "j" = 2)`}}}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			script, errs := Parse("test.txt", []byte(test.line+"\n"))
			if len(errs) != 0 {
				t.Fatalf("errors: %v", errs)
			}
			if len(script.Rules) != 1 {
				t.Fatalf("rules = %d", len(script.Rules))
			}
			got := script.Rules[0]
			test.want.Pos = Pos{File: "test.txt", Line: 1}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got  %#v\nwant %#v", got, test.want)
			}
		})
	}
}

func TestParseCommentsBlankLinesAndOrder(t *testing.T) {
	src := "# header\n\n// note\n/obj/a : /obj/b\r\n   \n/obj/c : @DELETE\n"
	script, errs := Parse("12345_RENAME.txt", []byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if script.Order != 12345 || script.Name != "12345_RENAME.txt" {
		t.Fatalf("script = %q order %d", script.Name, script.Order)
	}
	if len(script.Rules) != 2 || script.Rules[0].Pos.Line != 4 || script.Rules[1].Pos.Line != 6 {
		t.Fatalf("rules = %#v", script.Rules)
	}
	if unnumbered, _ := Parse("legacy.txt", nil); unnumbered.Order != -1 {
		t.Fatalf("unnumbered order = %d", unnumbered.Order)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"/obj/a /obj/b", "missing ':'"},
		{"/obj/a :", "missing replacement"},
		{" : /obj/b", "empty path"},
		{"obj/a : /obj/b", "must start with '/'"},
		{"/obj/a : @DELETE, /obj/b", "@DELETE cannot be combined"},
		{"/obj/a{name = @SKIP} : /obj/b", "@SKIP is not a filter"},
		{"/obj/a : /obj/b{name = @ANY}", "@ANY is only valid in a filter"},
		{"/obj/a : /obj/b{name}", "expected name = value"},
		{"/obj/a : /obj/b{name = \"x}", "unterminated string"},
		{"/obj/a : /obj/b{name = list(1}", "unbalanced"},
		{"/obj/a : /obj/b{x = @OLD:}", "@OLD: requires a variable name"},
		{"/obj/a : @MOVE", "unknown directive @MOVE"},
		{"/obj/a{@OLD} : /obj/b", "@OLD is not a filter"},
		{"/obj/a : /obj/b{bad name = 1}", "invalid variable name"},
		{"/obj/a : /obj/b{x = 1} trailing", "unexpected text after"},
	}
	for _, test := range tests {
		t.Run(test.line, func(t *testing.T) {
			_, errs := Parse("bad.txt", []byte("# c\n"+test.line))
			if len(errs) != 1 {
				t.Fatalf("errors = %v", errs)
			}
			if errs[0].Pos.Line != 2 || !strings.Contains(errs[0].Msg, test.want) {
				t.Fatalf("error = %v, want %q at line 2", errs[0], test.want)
			}
		})
	}
}

func TestFormatRoundTrip(t *testing.T) {
	src := strings.Join([]string{
		"/obj/old : /obj/new",
		"/obj/old2 : /obj/new{@OLD}",
		"/obj/gone : @DELETE",
		"/turf/a : /obj/effect/turf_decal{@OLD}, /obj/thing{icon_state = @OLD:name; name = \"meme\"}",
		"/obj/crate/@SUBTYPES : /obj/box/@SUBTYPES{@OLD}",
		"/mob/living{resize = @ANY; tag = @UNSET; name = \"Tom\"} : /mob/living{@OLD; resize = @SKIP; dir = @OLD}",
		"/obj/structure{@UNSET} : /obj/structure{dir = 1}",
		"/obj/a{dir = 2} : @OLD{dir = 1}",
	}, "\n")
	first, errs := Parse("a.txt", []byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	formatted := Format(first.Rules)
	if string(formatted) != src+"\n" {
		t.Fatalf("formatted:\n%s\nwant:\n%s", formatted, src)
	}
	second, errs := Parse("a.txt", formatted)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	for n := range first.Rules {
		first.Rules[n].Pos, second.Rules[n].Pos = Pos{}, Pos{}
	}
	if !reflect.DeepEqual(first.Rules, second.Rules) {
		t.Fatalf("round trip changed rules")
	}
}
