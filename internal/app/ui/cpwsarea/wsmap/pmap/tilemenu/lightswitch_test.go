package tilemenu

import (
	"testing"

	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type lightApp struct {
	App
	env *dmenv.Dme
}

func (a *lightApp) LoadedEnvironment() *dmenv.Dme { return a.env }

type lightEditor struct {
	editor
	switched []*dmminstance.Instance
	commits  []string
	locked   bool
}

func (e *lightEditor) TryBeginTileChange(...util.Point) bool { return !e.locked }
func (e *lightEditor) InstanceSwitchLight(i *dmminstance.Instance) (bool, error) {
	e.switched = append(e.switched, i)
	return false, nil
}
func (e *lightEditor) CommitOperation(label string) { e.commits = append(e.commits, label) }

func lightInstance(env *dmenv.Dme, path string, edits map[string]string) *dmminstance.Instance {
	vars := &dmvars.MutableVariables{}
	for k, v := range edits {
		vars.Put(k, v)
	}
	immutable := vars.ToImmutable()
	immutable.LinkParent(env.Objects[path].Vars)
	return dmminstance.New(util.Point{X: 1, Y: 1, Z: 1}, dmmprefab.New(dmmprefab.IdNone, path, immutable))
}

func TestLightSwitchMenuLabelsAndCommits(t *testing.T) {
	typeVars := func(kv ...string) *dmvars.Variables {
		vars := &dmvars.MutableVariables{}
		for n := 0; n < len(kv); n += 2 {
			vars.Put(kv[n], kv[n+1])
		}
		return vars.ToImmutable()
	}
	env := &dmenv.Dme{Objects: map[string]*dmenv.Object{
		"/obj/lamp":            {Path: "/obj/lamp", Vars: typeVars("light_range", "3", "light_on", "1")},
		"/obj/machinery/light": {Path: "/obj/machinery/light", Vars: typeVars("status", "0", "base_state", `"tube"`)},
		"/obj/structure/table": {Path: "/obj/structure/table", Vars: typeVars()},
	}}
	ed := &lightEditor{}
	m := &TileMenu{app: &lightApp{env: env}, editor: ed}

	if label, _ := m.lightSwitchLabel(lightInstance(env, "/obj/lamp", nil)); label != "Turn Light Off" {
		t.Fatalf("lamp label = %q", label)
	}
	if label, _ := m.lightSwitchLabel(lightInstance(env, "/obj/lamp", map[string]string{"light_on": "FALSE"})); label != "Turn Light On" {
		t.Fatalf("switched-off lamp label = %q", label)
	}
	if label, tip := m.lightSwitchLabel(lightInstance(env, "/obj/machinery/light", nil)); label != "Turn Light Off" || tip == "" {
		t.Fatalf("fixture label = %q, %q", label, tip)
	}
	if label, _ := m.lightSwitchLabel(lightInstance(env, "/obj/structure/table", nil)); label != "" {
		t.Fatalf("table label = %q; a table is not a light", label)
	}

	lamp := lightInstance(env, "/obj/lamp", nil)
	m.switchLight(lamp, "Turn Light Off")
	if len(ed.switched) != 1 || ed.switched[0] != lamp || len(ed.commits) != 1 || ed.commits[0] != "Turn Light Off" {
		t.Fatalf("switched = %d, commits = %v", len(ed.switched), ed.commits)
	}
	ed.locked = true
	m.switchLight(lamp, "Turn Light Off")
	if len(ed.switched) != 1 || len(ed.commits) != 1 {
		t.Fatal("a refused tile change still switched the light")
	}
}
