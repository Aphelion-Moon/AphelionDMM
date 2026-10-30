package varclip

import (
	"reflect"
	"testing"
)

func TestClipboardSetDeduplicatesAndSorts(t *testing.T) {
	var clip Clipboard
	clip.Set("/obj/door", []Entry{{"b", "1"}, {"a", "2"}, {"b", "3"}})
	want := []Entry{{"a", "2"}, {"b", "3"}}
	if got := clip.Entries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	if value, ok := clip.Value("b"); !ok || value != "3" {
		t.Fatalf("Value(b) = %q, %t", value, ok)
	}
	if clip.Source() != "/obj/door" || clip.Empty() {
		t.Fatalf("source/empty wrong: %q %t", clip.Source(), clip.Empty())
	}
	clip.Clear()
	if !clip.Empty() || clip.Source() != "" {
		t.Fatal("clear did not empty the clipboard")
	}
}

func TestEntriesIsACopy(t *testing.T) {
	var clip Clipboard
	clip.Set("/obj", []Entry{{"a", "1"}})
	entries := clip.Entries()
	entries[0].Value = "changed"
	if value, _ := clip.Value("a"); value != "1" {
		t.Fatalf("clipboard mutated through Entries: %q", value)
	}
}

func TestModifiedListsOnlyChangedVariables(t *testing.T) {
	current := map[string]string{"name": `"Glass door"`, "req_access": `list("medical")`, "dir": "2"}
	initial := map[string]string{"name": `"airlock"`, "req_access": "null", "dir": "2"}
	got := Modified([]string{"req_access", "dir", "name"},
		func(n string) string { return current[n] },
		func(n string) string { return initial[n] })
	want := []Entry{{"name", `"Glass door"`}, {"req_access", `list("medical")`}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Modified = %v, want %v", got, want)
	}
}

func TestPlanSkipsUndefinedReadOnlyAndUnchanged(t *testing.T) {
	defined := map[string]bool{"name": true, "req_access": true, "type": true, "dir": true}
	current := map[string]string{"name": `"x"`, "req_access": "null", "dir": "4"}
	apply, skipped := Plan(
		[]Entry{{"dir", "4"}, {"glass_only", "1"}, {"name", `"y"`}, {"req_access", `list("a")`}, {"type", "/obj"}},
		Target{
			Defined:  func(n string) bool { return defined[n] },
			ReadOnly: func(n string) bool { return n == "type" },
			Current:  func(n string) string { return current[n] },
		})
	if want := []Entry{{"name", `"y"`}, {"req_access", `list("a")`}}; !reflect.DeepEqual(apply, want) {
		t.Fatalf("apply = %v, want %v", apply, want)
	}
	if want := []string{"glass_only", "type"}; !reflect.DeepEqual(skipped, want) {
		t.Fatalf("skipped = %v, want %v", skipped, want)
	}
}
