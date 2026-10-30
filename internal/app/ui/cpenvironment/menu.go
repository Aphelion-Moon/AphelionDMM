package cpenvironment

import (
	// APHELION EDIT ADDITION START - MAP GO TO DEFINITION
	"errors"
	// APHELION EDIT ADDITION END
	"fmt"
	"os/exec"
	"path/filepath"

	// APHELION EDIT ADDITION START - MAP GO TO DEFINITION
	"sdmm/internal/aphelion/definition"
	"sdmm/internal/dmapi/dmenv"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/ui/layout/lnode"
	// APHELION EDIT ADDITION START - ICON RECOVERY
	"sdmm/internal/dmapi/dmicon"
	// APHELION EDIT ADDITION END
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/imguiext/icon"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/platform"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
	"github.com/skratchdot/open-golang/open"
)

func (e *Environment) showNodeMenu(n *treeNode) {
	if imgui.BeginPopupContextItemV(fmt.Sprint("environment_node_menu_", n.orig.Path), imgui.PopupFlagsMouseButtonRight) {
		w.Layout{
			// APHELION EDIT ADDITION START - ICON RECOVERY
			w.MenuItem("Retry Icon", e.doRetryIcon(n)).
				Icon(icon.Repeat).
				Enabled(n.icon != "" && n.load.State == dmicon.SpriteFailed),
			// APHELION EDIT ADDITION END
			w.MenuItem("Find on Map", e.doFindOnMap(n)).
				Icon(icon.Search).
				Enabled(e.app.HasActiveMap()),
			w.MenuItem("Copy Type", e.doCopyType(n)).
				Icon(icon.ContentCopy),
			w.MenuItem("Go to Definition", e.doGoToDefinition(n)).
				Icon(icon.FolderOpen),
		}.Build()
		imgui.EndPopup()
	}
}

// APHELION EDIT ADDITION START - ICON RECOVERY
func (e *Environment) doRetryIcon(n *treeNode) func() {
	return func() {
		if n.icon != "" {
			dmicon.Cache.RetryIcon(n.icon)
		}
	}
}

// APHELION EDIT ADDITION END

func (e *Environment) doFindOnMap(n *treeNode) func() {
	return func() {
		prefab := dmmap.PrefabStorage.Initial(n.orig.Path)
		log.Print("do find object on map:", prefab.Path())
		e.app.ShowLayout(lnode.NameSearch, true)
		e.app.DoSearchPrefabByPath(prefab.Path())
	}
}

func (e *Environment) doCopyType(n *treeNode) func() {
	return func() {
		log.Print("do copy type:", n.orig.Path)
		platform.SetClipboard(n.orig.Path)
	}
}

func (e *Environment) doGoToDefinition(n *treeNode) func() {
	return func() {
		prefab := dmmap.PrefabStorage.Initial(n.orig.Path)
		log.Print("do go to definition:", prefab.Path())

		// APHELION EDIT ADDITION START - MAP GO TO DEFINITION
		// The tile menu shares this launcher; see OpenDefinition.
		if err := OpenDefinition(e.app.LoadedEnvironment(), e.app.Prefs().Editor.CodeEditor, n.orig.Path); err != nil {
			log.Print("unable to open definition file: ", err)
		}
	}
}

// ErrNoDefinition reports a type the loaded environment does not define.
var ErrNoDefinition = errors.New("type is not defined by the loaded environment")

// HasDefinition reports whether OpenDefinition can locate typePath's source.
func HasDefinition(env *dmenv.Dme, typePath string) bool {
	return env != nil && env.Objects[typePath] != nil
}

// OpenDefinition opens typePath's DM source definition in the configured code
// editor. The executable comes only from the fixed preference table below and
// no shell is invoked; the source file must resolve inside the environment root.
func OpenDefinition(env *dmenv.Dme, codeEditor string, typePath string) error {
	if !HasDefinition(env, typePath) {
		return fmt.Errorf("%w: %s", ErrNoDefinition, typePath)
	}
	location := env.Objects[typePath].Location
	path, err := definition.SourcePath(filepath.FromSlash(env.RootDir), location.File)
	if err != nil {
		return err
	}
	var command *exec.Cmd

	switch codeEditor {
	case prefs.CodeEditorVSC:
		root := filepath.FromSlash(env.RootDir)
		argument := path + ":" + fmt.Sprint(location.Line) + ":" + fmt.Sprint(location.Column)
		command = exec.Command(prefs.CodeEditorVSCActual, root, "-g", argument)
	case prefs.CodeEditorVSCodium:
		root := filepath.FromSlash(env.RootDir)
		argument := path + ":" + fmt.Sprint(location.Line) + ":" + fmt.Sprint(location.Column)
		command = exec.Command(prefs.CodeEditorVSCodiumActual, root, "-g", argument)
	case prefs.CodeEditorDM:
		// No line/col support until https://www.byond.com/forum/post/2970625
		command = exec.Command(prefs.CodeEditorDMActual, path)
	case prefs.CodeEditorNPP:
		lineArg := "-n" + fmt.Sprint(location.Line)
		colArg := "-c" + fmt.Sprint(location.Column)
		command = exec.Command(prefs.CodeEditorNPPActual, path, lineArg, colArg)
	case prefs.CodeEditorDefault:
		return open.Start(path)
	default:
		return fmt.Errorf("unsupported code editor preference %q", codeEditor)
	}
	return command.Start()
}

// APHELION EDIT ADDITION END

/* APHELION EDIT REMOVAL START - MAP GO TO DEFINITION
		location := n.orig.Location
		path := filepath.FromSlash(e.app.LoadedEnvironment().RootDir + "/" + location.File)
		editorPrefs := e.app.Prefs().Editor
		var command *exec.Cmd

		switch editorPrefs.CodeEditor {
		case prefs.CodeEditorVSC:
			root := filepath.FromSlash(e.app.LoadedEnvironment().RootDir)
			argument := path + ":" + fmt.Sprint(location.Line) + ":" + fmt.Sprint(location.Column)
			command = exec.Command(prefs.CodeEditorVSCActual, root, "-g", argument)
		case prefs.CodeEditorVSCodium:
			root := filepath.FromSlash(e.app.LoadedEnvironment().RootDir)
			argument := path + ":" + fmt.Sprint(location.Line) + ":" + fmt.Sprint(location.Column)
			command = exec.Command(prefs.CodeEditorVSCodiumActual, root, "-g", argument)
		case prefs.CodeEditorDM:
			// No line/col support until https://www.byond.com/forum/post/2970625
			command = exec.Command(prefs.CodeEditorDMActual, path)
		case prefs.CodeEditorNPP:
			lineArg := "-n" + fmt.Sprint(location.Line)
			colArg := "-c" + fmt.Sprint(location.Column)
			command = exec.Command(prefs.CodeEditorNPPActual, path, lineArg, colArg)
		case prefs.CodeEditorDefault:
			if err := open.Start(path); err != nil {
				log.Print("unable to open definition file: ", err)
			}
			return
		}

		if err := command.Start(); err != nil {
			log.Print("unable to open definition file: ", err)
		}
	}
}
APHELION EDIT REMOVAL END */
