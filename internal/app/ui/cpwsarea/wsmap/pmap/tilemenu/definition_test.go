package tilemenu

import (
	"testing"

	"sdmm/internal/app/prefs"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type definitionApp struct {
	App
	env   *dmenv.Dme
	prefs prefs.Prefs
}

func (a *definitionApp) LoadedEnvironment() *dmenv.Dme { return a.env }
func (a *definitionApp) Prefs() prefs.Prefs            { return a.prefs }

func definitionInstance(path string) *dmminstance.Instance {
	return dmminstance.New(util.Point{X: 1, Y: 1, Z: 1}, dmmprefab.New(dmmprefab.IdNone, path, (&dmvars.MutableVariables{}).ToImmutable()))
}

func TestGoToDefinitionUsesEnvironmentLauncherForKnownTypes(t *testing.T) {
	env := &dmenv.Dme{RootDir: t.TempDir(), Objects: map[string]*dmenv.Object{
		"/obj/structure/table": {Path: "/obj/structure/table"},
	}}
	app := &definitionApp{env: env}
	app.prefs.Editor.CodeEditor = prefs.CodeEditorNPP
	m := &TileMenu{app: app}

	type call struct {
		env    *dmenv.Dme
		editor string
		path   string
	}
	var calls []call
	previous := openDefinition
	openDefinition = func(env *dmenv.Dme, editor, path string) error {
		calls = append(calls, call{env, editor, path})
		return nil
	}
	defer func() { openDefinition = previous }()

	table := definitionInstance("/obj/structure/table")
	if !m.hasDefinition(table) {
		t.Fatal("a type the environment defines should offer Go to Definition")
	}
	m.doGoToDefinition(table)()
	if len(calls) != 1 || calls[0] != (call{env, prefs.CodeEditorNPP, "/obj/structure/table"}) {
		t.Fatalf("launcher calls = %+v", calls)
	}

	if m.hasDefinition(definitionInstance("/area/engine/break_room")) {
		t.Fatal("a type the environment does not define must disable Go to Definition")
	}
	if (&TileMenu{app: &definitionApp{}}).hasDefinition(table) {
		t.Fatal("without a loaded environment nothing has a definition")
	}
}
