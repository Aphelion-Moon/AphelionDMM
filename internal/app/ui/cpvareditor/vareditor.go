package cpvareditor

import (
	"sdmm/internal/app/ui/component"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"

	"sdmm/internal/app/config"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util/slice"
	"strings"

	"github.com/rs/zerolog/log"
)

type App interface {
	DoSelectPrefab(prefab *dmmprefab.Prefab)
	CurrentEditor() *editor.Editor
	LoadedEnvironment() *dmenv.Dme

	ConfigRegister(config.Config)
	ConfigFind(name string) config.Config
}

type editMode int

func (e editMode) String() string {
	switch e {
	case emInstance:
		return "editModeInstance"
	case emPrefab:
		return "editModePrefab"
	}
	return ""
}

const (
	emInstance editMode = iota
	emPrefab
)

type VarEditor struct {
	component.Component

	app App

	shortcuts shortcut.Shortcuts

	instance *dmminstance.Instance
	prefab   *dmmprefab.Prefab

	variablesNames []string

	variablesPaths        []string
	variablesNamesByPaths map[string][]string

	sessionEditMode editMode
	sessionPrefabId uint64

	filterVarName  string
	filterTypeName string
}

func (v *VarEditor) Init(app App) {
	v.app = app

	v.addShortcuts()
	v.loadConfig()

	v.AddOnFocused(func(focused bool) {
		v.shortcuts.SetVisible(focused)
	})
}

func (v *VarEditor) Free() {
	v.resetSession()
	log.Print("vareditor free")
}

// Sync does the check if we edit an instance which is exists.
// If the instance doesn't exist, then the editor will switch its mode to the prefab editing.
func (v *VarEditor) Sync() {
	if v.prefab == nil {
		return
	}
	e := v.app.CurrentEditor()
	if e == nil || (v.instance != nil && !e.Dmm().IsInstanceExist(v.instance.Id())) {
		v.instance = nil
		v.sessionEditMode = emPrefab
	}
	log.Print("synced, editMode:", v.sessionEditMode)
}

func (v *VarEditor) EditInstance(instance *dmminstance.Instance) {
	v.resetSession()
	v.sessionEditMode = emInstance
	v.instance = instance
	v.setup(instance.Prefab())
}

func (v *VarEditor) EditPrefab(prefab *dmmprefab.Prefab) {
	v.resetSession()
	v.sessionEditMode = emPrefab
	v.setup(prefab)
}

func (v *VarEditor) EditedInstance() (*dmminstance.Instance, bool) {
	if v.instance != nil && v.sessionEditMode == emInstance {
		return v.instance, true
	}
	return nil, false
}

func (v *VarEditor) setup(prefab *dmmprefab.Prefab) {
	v.prefab = prefab
	v.variablesNames = collectVariablesNames(prefab.Vars())
	// APHELION EDIT ADDITION START - COLLABORATION
	if v.environmentObject() != nil {
		v.variablesPaths = collectVariablesPaths(v.app.LoadedEnvironment().Objects[v.prefab.Path()])
		v.variablesNamesByPaths = collectVariablesNamesByPaths(v.app.LoadedEnvironment(), v.variablesPaths)
	} else {
		v.variablesPaths = []string{prefab.Path()}
		v.variablesNamesByPaths = map[string][]string{prefab.Path(): append([]string(nil), v.variablesNames...)}
	}
	// APHELION EDIT ADDITION END

	// Clear pinned variables from the common list, since they are showed separately.
	for _, pinnedVarName := range v.config().PinnedVarNames {
		v.variablesNames = slice.StrRemove(v.variablesNames, pinnedVarName)
	}

	log.Printf("setup finished: [%s], variablesNames: [%d], variablesPaths: [%d], pinnedVarNames: [%d]",
		prefab.Path(), len(v.variablesNames), len(v.variablesPaths), len(v.config().PinnedVarNames))
}

func (v *VarEditor) setInstanceVariable(varName, varValue string) {
	// APHELION EDIT ADDITION START - COLLABORATION
	if v.environmentObject() == nil {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PASTE PLACEMENT
	if e := v.app.CurrentEditor(); e != nil && e.HasPastePlacement() {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PROPERTY CAPTURE
	currentEditor := v.app.CurrentEditor()
	if currentEditor == nil {
		return
	}
	// Capture before changing either the instance or its session prefab cache.
	// APHELION EDIT CHANGE - COMPOSITION ANCHORS - ORIGINAL: if !currentEditor.TryBeginTileChange(v.instance.Coord()) {
	if !currentEditor.TryBeginInstanceChange(v.instance) {
		currentEditor.CommitOperation("Edit Variable")
		return
	}
	// APHELION EDIT ADDITION END
	if len(varValue) == 0 {
		varValue = dmvars.NullValue
	}

	//correct any issues related to unenclosed stuff
	varValue = v.correctVarIssue(varValue)

	origPrefab := v.instance.Prefab()

	var newVars *dmvars.Variables
	if v.initialVarValue(varName) == varValue {
		newVars = dmvars.Delete(origPrefab.Vars(), varName)
	} else {
		newVars = dmvars.Set(origPrefab.Vars(), varName, varValue)
	}

	newPrefab, isNew := dmmap.PrefabStorage.GetV(origPrefab.Path(), newVars)

	// Newly created prefabs are sort of temporal objects, which need to exist only during the edit session.
	// So if we modified a variable of the instance and that creates a new prefab, the previous one will be deleted.
	if isNew {
		if origPrefab.Id() == v.sessionPrefabId {
			dmmap.PrefabStorage.Delete(origPrefab)
		}
		v.sessionPrefabId = newPrefab.Id()
	}

	v.instance.SetPrefab(newPrefab)
	// APHELION EDIT CHANGE - PROPERTY CAPTURE - ORIGINAL: v.app.CurrentEditor().CommitOperation("Edit Variable")
	currentEditor.CommitOperation("Edit Variable")
	v.app.DoSelectPrefab(newPrefab)

	v.prefab = newPrefab

	log.Printf("instance [%d] variable set; name: [%s], value: [%s]", v.instance.Id(), varName, varValue)
}

func (v *VarEditor) setPrefabVariable(varName, varValue string) {
	// APHELION EDIT ADDITION START - COLLABORATION
	if v.environmentObject() == nil {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PASTE PLACEMENT
	if e := v.app.CurrentEditor(); e != nil && e.HasPastePlacement() {
		return
	}
	// APHELION EDIT ADDITION END
	if len(varValue) == 0 {
		varValue = dmvars.NullValue
	}

	//correct any issues related to unenclosed stuff
	varValue = v.correctVarIssue(varValue)

	var newVars *dmvars.Variables
	if v.initialVarValue(varName) == varValue {
		newVars = dmvars.Delete(v.prefab.Vars(), varName)
	} else {
		newVars = dmvars.Set(v.prefab.Vars(), varName, varValue)
	}

	newPrefab := dmmap.PrefabStorage.Get(v.prefab.Path(), newVars)

	v.app.CurrentEditor().ReplacePrefab(v.prefab, newPrefab)
	v.app.CurrentEditor().CommitOperation("Replace Prefab")

	v.app.DoSelectPrefab(newPrefab)

	v.prefab = newPrefab

	log.Printf("prefab [%d] variable set; name: [%s], value: [%s]", v.prefab.Id(), varName, varValue)
}

func (v *VarEditor) resetSession() {
	v.variablesNames = nil
	v.variablesPaths = nil
	v.variablesNamesByPaths = nil
	v.sessionPrefabId = dmmprefab.IdNone
	v.sessionEditMode = emPrefab
	v.instance = nil
	v.prefab = nil
	log.Print("session reset")
}

func (v *VarEditor) initialVarValue(varName string) string {
	// APHELION EDIT ADDITION START - COLLABORATION
	if v.environmentObject() == nil {
		return v.currentVars().ValueV(varName, dmvars.NullValue)
	}
	// APHELION EDIT ADDITION END
	return v.app.LoadedEnvironment().Objects[v.prefab.Path()].Vars.ValueV(varName, dmvars.NullValue)
}

func (v *VarEditor) isReadOnly(varName string) bool {
	// APHELION EDIT ADDITION START - COLLABORATION
	if v.environmentObject() == nil {
		return true
	}
	// APHELION EDIT ADDITION END
	return v.app.LoadedEnvironment().Objects[v.prefab.Path()].Flags(varName).ReadOnly()
}

func (v *VarEditor) isCurrentVarInitial(varName string) bool {
	// APHELION EDIT ADDITION START - COLLABORATION
	if v.environmentObject() == nil {
		return false
	}
	// APHELION EDIT ADDITION END
	return v.currentVars().ValueV(varName, dmvars.NullValue) == v.initialVarValue(varName)
}

// APHELION EDIT ADDITION START - COLLABORATION
func (v *VarEditor) environmentObject() *dmenv.Object {
	environment := v.app.LoadedEnvironment()
	if environment == nil || v.prefab == nil {
		return nil
	}
	return environment.Objects[v.prefab.Path()]
}

// APHELION EDIT ADDITION END

func (v *VarEditor) correctVarIssue(varValue string) string {
	isList, isString := strings.HasPrefix(varValue, "list("), strings.HasPrefix(varValue, `"`)
	enclosingCharacter := ""
	hasIssue := false
	if isString {
		//we start and end with a quotation mark so we probably shouldnt break
		enclosingCharacter = `"`
		hasIssue = !strings.HasSuffix(varValue, enclosingCharacter)
	} else if isList {
		//if list is enclosed
		enclosingCharacter = `)`
		hasIssue = !strings.HasSuffix(varValue, enclosingCharacter)
	}

	if hasIssue {
		varValue = varValue + enclosingCharacter
	}

	return varValue
}

func (v *VarEditor) doToggleShowModified() {
	cfg := v.config()
	cfg.ShowModified = !cfg.ShowModified
	log.Print("toggle 'showModified':", cfg.ShowModified)
}

func (v *VarEditor) doToggleShowByType() {
	cfg := v.config()
	cfg.ShowByType = !cfg.ShowByType
	log.Print("toggle 'showByType':", cfg.ShowByType)
}

func (v *VarEditor) doToggleShowPins() {
	cfg := v.config()
	cfg.ShowPins = !cfg.ShowPins
	log.Print("toggle 'showPins':", cfg.ShowPins)
}

func (v *VarEditor) doToggleShowTmp() {
	cfg := v.config()
	cfg.ShowTmp = !cfg.ShowTmp
	log.Print("toggle 'showTmp':", cfg.ShowTmp)
}
