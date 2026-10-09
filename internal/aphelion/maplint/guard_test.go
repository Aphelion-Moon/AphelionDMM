package maplint

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const windowRules = "/obj/structure/window:\n  banned_neighbors:\n    /obj/structure/window:\n      identical: true\n"
const tableRules = "help: Only one table per tile.\n/obj/structure/table:\n  banned_neighbors:\n    /obj/structure/table: {}\n"

// repo creates <root>/tools/maplint/lints with the given files and returns the
// path of a fake .dme in root.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "tools", "maplint", "lints")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dme := filepath.Join(root, "game.dme")
	if err := os.WriteFile(dme, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return dme
}

func syncSchedule(f func()) { f() }

func TestGuardInactiveWithoutRulesDirectory(t *testing.T) {
	g := NewGuard()
	g.Begin(filepath.Join(t.TempDir(), "game.dme"), syncSchedule)
	if st := g.Status(); st.Active() || st.Loading || st.RuleCount != 0 {
		t.Fatalf("status = %+v", st)
	}
	v := g.Evaluate("m.dmm", []Atom{atom("/obj/structure/window")}, atom("/obj/structure/window"))
	if len(v.Violations) != 0 || v.Skip {
		t.Fatalf("inactive guard reported %+v", v)
	}
}

func TestGuardLoadsOnceAndPublishesOnSchedule(t *testing.T) {
	dme := repo(t, map[string]string{"multiple_windows.yml": windowRules})
	g := NewGuard()
	queued := make(chan func(), 4)
	g.Begin(dme, func(f func()) { queued <- f })
	if st := g.Status(); !st.Active() || !st.Loading {
		t.Fatalf("before publish: %+v", st)
	}
	var publish func()
	select {
	case publish = <-queued:
	case <-time.After(5 * time.Second):
		t.Fatal("load did not complete")
	}
	if st := g.Status(); !st.Loading {
		t.Fatal("published before the scheduler ran")
	}
	publish()
	st := g.Status()
	if st.Loading || st.RuleCount != 1 || len(st.Files) != 1 || st.Files[0] != "multiple_windows.yml" {
		t.Fatalf("after publish: %+v", st)
	}
}
func TestGuardGenerationFencesStaleLoads(t *testing.T) {
	first := repo(t, map[string]string{"a.yml": windowRules})
	second := repo(t, map[string]string{"b.yml": tableRules, "c.yml": windowRules})
	g := NewGuard()
	var mu sync.Mutex
	var queued []func()
	schedule := func(f func()) { mu.Lock(); queued = append(queued, f); mu.Unlock() }
	g.Begin(first, schedule)
	g.Begin(second, schedule)
	for i := 0; i < 1000; i++ {
		mu.Lock()
		n := len(queued)
		mu.Unlock()
		if n == 2 {
			break
		}
		waitABit()
	}
	mu.Lock()
	jobs := append([]func(){}, queued...)
	mu.Unlock()
	if len(jobs) != 2 {
		t.Fatalf("jobs = %d", len(jobs))
	}
	for _, job := range jobs { // both complete; only the newest generation publishes
		job()
	}
	if st := g.Status(); st.RuleCount != 2 || len(st.Files) != 2 {
		t.Fatalf("stale load published: %+v", st)
	}
	g.Reset()
	if st := g.Status(); st.Active() || st.RuleCount != 0 {
		t.Fatalf("reset left %+v", st)
	}
	g.Begin(first, schedule)
	g.Reset()
	for i := 0; i < 1000; i++ {
		mu.Lock()
		n := len(queued)
		mu.Unlock()
		if n == 3 {
			break
		}
		waitABit()
	}
	mu.Lock()
	last := queued[len(queued)-1]
	mu.Unlock()
	last()
	if st := g.Status(); st.Active() || st.RuleCount != 0 {
		t.Fatalf("load completed after Reset published: %+v", st)
	}
}

func TestGuardReportsUnsupportedAndErrorsWithoutBlocking(t *testing.T) {
	dme := repo(t, map[string]string{
		"ok.yml":      windowRules,
		"weird.yml":   "/obj/x:\n  frobnicate: true\n",
		"broken.yml":  "/obj/x: [unterminated\n",
		"ignored.txt": "x",
	})
	g := NewGuard()
	g.Begin(dme, syncSchedule)
	waitForLoad(t, g)
	st := g.Status()
	if st.RuleCount != 1 || len(st.Unsupported) != 1 || len(st.Errors) != 1 {
		t.Fatalf("status = %+v", st)
	}
	if !strings.Contains(st.Unsupported[0], "frobnicate") || !strings.Contains(st.Errors[0], "broken.yml") {
		t.Fatalf("details = %+v", st)
	}
}

func TestEvaluateIdenticalSkipsAndNonIdenticalReplaces(t *testing.T) {
	dme := repo(t, map[string]string{"w.yml": windowRules, "t.yml": tableRules})
	g := NewGuard()
	g.Begin(dme, syncSchedule)
	waitForLoad(t, g)

	existing := []Atom{atom("/turf/open/floor"), atom("/obj/structure/window", "dir", "4")}
	same := g.Evaluate("m.dmm", existing, atom("/obj/structure/window", "dir", "4"))
	if !same.Skip || len(same.Violations) == 0 {
		t.Fatalf("identical window was not skipped: %+v", same)
	}
	diff := g.Evaluate("m.dmm", existing, atom("/obj/structure/window", "dir", "8"))
	if diff.Skip || len(diff.Violations) != 0 {
		t.Fatalf("different window flagged: %+v", diff)
	}

	tables := []Atom{atom("/obj/structure/table"), atom("/obj/item/pen"), atom("/obj/structure/table/wood")}
	v := g.Evaluate("m.dmm", tables, atom("/obj/structure/table/reinforced"))
	if v.Skip || len(v.Violations) == 0 {
		t.Fatalf("table placement: %+v", v)
	}
	if got := v.Replace; len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("replace = %v", v.Replace)
	}
	if text := v.Summary(); !strings.Contains(text, "Only one table per tile.") || !strings.Contains(text, "t.yml") {
		t.Fatalf("summary = %q", text)
	}
}

func TestMapFileIsRepositoryRelativeWithSlashes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	abs := filepath.Join(root, "_maps", "map_files", "a.dmm")
	if got := MapFile(abs, root); got != "_maps/map_files/a.dmm" {
		t.Fatalf("got %q", got)
	}
	outside := filepath.Join(t.TempDir(), "x.dmm")
	if got := MapFile(outside, root); got != filepath.ToSlash(outside) {
		t.Fatalf("outside = %q", got)
	}
}

func TestViolationExposesIdentical(t *testing.T) {
	rs := mustLoad(t, map[string]string{"w.yml": windowRules})
	got := rs.CheckPlacement([]Atom{atom("/obj/structure/window")}, atom("/obj/structure/window"))
	if len(got) == 0 || !got[0].Identical {
		t.Fatalf("identical flag missing: %+v", got)
	}
}

func waitABit() { time.Sleep(2 * time.Millisecond) }

func waitForLoad(t *testing.T, g *Guard) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if !g.Status().Loading {
			return
		}
		waitABit()
	}
	t.Fatal("rules did not finish loading")
}
