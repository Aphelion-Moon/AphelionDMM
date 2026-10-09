package updatepaths

import (
	"reflect"
	"strings"
	"testing"
)

func mustRules(t *testing.T, src string) []Rule {
	t.Helper()
	script, errs := Parse("t.txt", []byte(src))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	return script.Rules
}

func TestApplyRuleVariableSemantics(t *testing.T) {
	tests := []struct {
		name string
		rule string
		in   Instance
		want []Instance
	}{
		{"no @OLD discards vars", "/obj/a : /obj/b", Instance{"/obj/a", map[string]string{"dir": "4"}}, []Instance{{"/obj/b", map[string]string{}}}},
		{"@OLD keeps vars", "/obj/a : /obj/b{@OLD}", Instance{"/obj/a", map[string]string{"dir": "4"}}, []Instance{{"/obj/b", map[string]string{"dir": "4"}}}},
		{"@OLD then override and skip", "/obj/a : /obj/b{@OLD; dir = 8; name = @SKIP}", Instance{"/obj/a", map[string]string{"dir": "4", "name": `"x"`}}, []Instance{{"/obj/b", map[string]string{"dir": "8"}}}},
		{"literal before @OLD is reset", "/obj/a : /obj/b{dir = 8; @OLD}", Instance{"/obj/a", map[string]string{"dir": "4"}}, []Instance{{"/obj/b", map[string]string{"dir": "4"}}}},
		{"named @OLD", "/obj/a : /obj/b{dir = @OLD}", Instance{"/obj/a", map[string]string{"dir": "4", "name": `"x"`}}, []Instance{{"/obj/b", map[string]string{"dir": "4"}}}},
		{"@OLD:other", "/obj/a : /obj/b{desc = @OLD:name}", Instance{"/obj/a", map[string]string{"name": `"x"`}}, []Instance{{"/obj/b", map[string]string{"desc": `"x"`}}}},
		{"missing @OLD source is omitted", "/obj/a : /obj/b{desc = @OLD:name}", Instance{"/obj/a", nil}, []Instance{{"/obj/b", map[string]string{}}}},
		{"delete", "/obj/a : @DELETE", Instance{"/obj/a", nil}, nil},
		{"keep path", "/obj/a : @OLD{dir = 1}", Instance{"/obj/a", map[string]string{"dir": "2"}}, []Instance{{"/obj/a", map[string]string{"dir": "1"}}}},
		{"subtypes include base", "/obj/a/@SUBTYPES : /obj/z/@SUBTYPES", Instance{"/obj/a", nil}, []Instance{{"/obj/z", map[string]string{}}}},
		{"subtypes keep suffix", "/obj/a/@SUBTYPES : /obj/z/@SUBTYPES{@OLD}", Instance{"/obj/a/b/c", map[string]string{"x": "1"}}, []Instance{{"/obj/z/b/c", map[string]string{"x": "1"}}}},
		{"subtypes output without subtypes match", "/obj/a : /obj/z/@SUBTYPES{@OLD}", Instance{"/obj/a", map[string]string{"x": "1"}}, []Instance{{"/obj/z", map[string]string{"x": "1"}}}},
		{"multiple outputs", "/obj/a : /obj/b{@OLD}, /obj/c", Instance{"/obj/a", map[string]string{"x": "1"}}, []Instance{{"/obj/b", map[string]string{"x": "1"}}, {"/obj/c", map[string]string{}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := mustRules(t, test.rule)[0]
			got, matched := rule.Apply(test.in)
			if !matched {
				t.Fatal("rule did not match")
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %#v want %#v", got, test.want)
			}
		})
	}
}

func TestApplyRuleMatching(t *testing.T) {
	tests := []struct {
		rule  string
		in    Instance
		match bool
	}{
		{"/obj/a : /obj/b", Instance{"/obj/a/sub", nil}, false},
		{"/obj/a : /obj/b", Instance{"/obj/ab", nil}, false},
		{"/obj/a/@SUBTYPES : /obj/b", Instance{"/obj/ab", nil}, false},
		{"/obj/a{dir = 2} : /obj/b", Instance{"/obj/a", map[string]string{"dir": "2"}}, true},
		{"/obj/a{dir = 2} : /obj/b", Instance{"/obj/a", map[string]string{"dir": "4"}}, false},
		{"/obj/a{dir = 2} : /obj/b", Instance{"/obj/a", nil}, false},
		{"/obj/a{dir = @ANY} : /obj/b", Instance{"/obj/a", map[string]string{"dir": "4"}}, true},
		{"/obj/a{dir = @ANY} : /obj/b", Instance{"/obj/a", nil}, false},
		{"/obj/a{dir = @UNSET} : /obj/b", Instance{"/obj/a", nil}, true},
		{"/obj/a{dir = @UNSET} : /obj/b", Instance{"/obj/a", map[string]string{"dir": "4"}}, false},
		{"/obj/a{@UNSET} : /obj/b", Instance{"/obj/a", nil}, true},
		{"/obj/a{@UNSET} : /obj/b", Instance{"/obj/a", map[string]string{"x": "1"}}, false},
	}
	for _, test := range tests {
		rule := mustRules(t, test.rule)[0]
		if _, matched := rule.Apply(test.in); matched != test.match {
			t.Errorf("%s on %v: matched=%v want %v", test.rule, test.in, matched, test.match)
		}
	}
}

func TestSetAppliesSequentiallyAcrossScriptsInOrder(t *testing.T) {
	later, _ := Parse("200_LATER.txt", []byte("/obj/b : /obj/c{@OLD}\n"))
	earlier, _ := Parse("100_EARLIER.txt", []byte("/obj/a : /obj/b{@OLD}\n/obj/b : /obj/b2{@OLD}\n"))
	set := NewSet(later, earlier)
	got, trace := set.Apply(Instance{"/obj/a", map[string]string{"x": "1"}}, nil)
	// Line 2 of the earlier script sees line 1's output; the later script then
	// sees nothing to rename because /obj/b no longer exists.
	want := []Instance{{"/obj/b2", map[string]string{"x": "1"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
	if len(trace) != 2 || trace[0].File != "100_EARLIER.txt" || trace[0].Line != 1 || trace[1].Line != 2 {
		t.Fatalf("trace = %#v", trace)
	}
}

func TestSetApplyStopsAtKnownPath(t *testing.T) {
	script, _ := Parse("1_A.txt", []byte("/obj/a : /obj/b{@OLD}\n/obj/b : /obj/c{@OLD}\n"))
	set := NewSet(script)
	known := func(path string) bool { return path == "/obj/b" || path == "/obj/c" }
	got, _ := set.Apply(Instance{"/obj/a", nil}, known)
	if len(got) != 1 || got[0].Path != "/obj/b" {
		t.Fatalf("got %#v", got)
	}
	full, _ := set.Apply(Instance{"/obj/a", nil}, nil)
	if len(full) != 1 || full[0].Path != "/obj/c" {
		t.Fatalf("full %#v", full)
	}
}

func TestResolveChain(t *testing.T) {
	rules := mustRules(t, "/obj/b : /obj/c{@OLD}\n/obj/a : /obj/b{@OLD}\n")
	known := func(path string) bool { return path == "/obj/c" }
	got, trace, err := ResolveChain(rules, Instance{"/obj/a", map[string]string{"x": "1"}}, known)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "/obj/c" || got[0].Vars["x"] != "1" || len(trace) != 2 {
		t.Fatalf("got %#v trace %#v", got, trace)
	}
	if got, _, err := ResolveChain(rules, Instance{"/obj/z", nil}, known); err != nil || got != nil {
		t.Fatalf("unmatched = %#v %v", got, err)
	}
	cycle := mustRules(t, "/obj/a : /obj/b{@OLD}\n/obj/b : /obj/a{@OLD}\n")
	if _, _, err := ResolveChain(cycle, Instance{"/obj/a", nil}, known); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle err = %v", err)
	}
}
