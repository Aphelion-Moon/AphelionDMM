package prefs

import (
	"math"
	// APHELION EDIT ADDITION START - SELECTION GRID STEP
	"sdmm/internal/aphelion/editing"
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PATH MIGRATION
	"sdmm/internal/aphelion/repath"
	// APHELION EDIT ADDITION END

	"sdmm/internal/app/ui/cpwsarea/wsprefs"
	"sdmm/internal/app/window"
)

type App interface {
	UpdateScale()
}

func Make(app App, prefs *Prefs) wsprefs.Prefs {
	p := wsprefs.MakePrefs()

	var preferencesPrefabs = map[wsprefs.PrefGroup][]prefPrefab{
		wsprefs.GPEditor: {
			optionPrefPrefab{
				name:    "Save Format",
				desc:    "Controls the format used by the editor to save the map.",
				label:   "##save_format",
				value:   &prefs.Editor.SaveFormat,
				options: SaveFormats,
				help:    SaveFormatHelp,
			},
			optionPrefPrefab{
				name:    "Code Editor",
				desc:    "Controls what code editor is opened when using Go to Definition.",
				label:   "##code_editor",
				value:   &prefs.Editor.CodeEditor,
				options: CodeEditors,
				help:    CodeEditorHelp,
			},
			boolPrefPrefab{
				name:  "Sanitize Variables",
				desc:  "Enables sanitizing for variables which are declared on the map, but has the same value as initial.",
				label: "##sanitize_variables",
				value: &prefs.Editor.SanitizeVariables,
			},
			optionPrefPrefab{
				name:    "Nudge Mode",
				desc:    "Controls which variables will be changed during the nudge.",
				label:   "##nudge_mode",
				value:   &prefs.Editor.NudgeMode,
				options: SaveNudgeModes,
			},
			// APHELION EDIT ADDITION START - SELECTION GRID STEP
			intPrefPrefab{
				name:  "Selection Move Step",
				desc:  "Number of tiles moved by Alt+Arrow and the Grab selection move buttons. A move that would leave the map is refused. Set to 1 to restore the default.",
				label: "##selection_move_step",
				min:   1, max: editing.MaxSelectionMoveStep,
				value: &prefs.Editor.SelectionMoveStep,
			},
			// APHELION EDIT ADDITION END
			// APHELION EDIT ADDITION START - AREA PRESENTATION
			intPrefPrefab{
				name:  "Area Overlay Opacity",
				desc:  "Opacity (percent) of areas drawn as an overlay above the map. Render-only; map data is unchanged.",
				label: "%##area_overlay_opacity",
				min:   0, max: 100,
				value: &prefs.Editor.AreaOverlayPercent,
				post:  func(int) { ApplyAreaPolicy(prefs.Editor) },
			},
			boolPrefPrefab{
				name:  "Hide Base Area",
				desc:  "Do not draw the environment's base area (world.area, or /area/space). Render-only; map data is unchanged.",
				label: "##hide_base_area",
				value: &prefs.Editor.HideBaseArea,
				post:  func(bool) { ApplyAreaPolicy(prefs.Editor) },
			},
			// APHELION EDIT ADDITION END
			// APHELION EDIT ADDITION START - LIGHTING PREVIEW
			intPrefPrefab{
				name:  "Lighting Darkness",
				desc:  "Strength (percent) of the lighting preview multiplied over the map. 0 shows the map unchanged; 100 matches the modelled game light. Approximate, display only.",
				label: "%##lighting_darkness",
				min:   0, max: 100,
				value: &prefs.Editor.Lighting.Darkness,
				post:  func(int) { ApplyLighting(prefs.Editor) },
			},
			boolPrefPrefab{
				name:  "Lighting Starlight",
				desc:  "Light tiles next to space with starlight (range 2, power 1, #8589fa) in the lighting preview.",
				label: "##lighting_starlight",
				value: &prefs.Editor.Lighting.Starlight,
				post:  func(bool) { ApplyLighting(prefs.Editor) },
			},
			boolPrefPrefab{
				name:  "Lighting Overlay Lights",
				desc:  "Include overlay-system lights in the lighting preview. They are approximated as corner lights and are labelled approximate.",
				label: "##lighting_overlay_lights",
				value: &prefs.Editor.Lighting.OverlayLights,
				post:  func(bool) { ApplyLighting(prefs.Editor) },
			},
			boolPrefPrefab{
				name:  "Lighting Show Light Sources",
				desc:  "Draw a marker, range ring and cone edges for every emitter used by the lighting preview.",
				label: "##lighting_show_sources",
				value: &prefs.Editor.Lighting.ShowSources,
				post:  func(bool) { ApplyLighting(prefs.Editor) },
			},
			// APHELION EDIT ADDITION END
		},

		wsprefs.GPControls: {
			boolPrefPrefab{
				name:  "Alternative Scroll Behavior",
				desc:  "When enabled, scrolling will do panning. Zoom will be available if a Space key pressed.",
				label: "##alternative_scroll_behavior",
				value: &prefs.Controls.AltScrollBehaviour,
			},
			boolPrefPrefab{
				name:  "Quick Edit: Tile Context Menu",
				desc:  "Controls whether Quick Edit should be shown in the tile context menu.",
				label: "##quick_edit:tile_context_menu",
				value: &prefs.Controls.QuickEditContextMenu,
			},
			boolPrefPrefab{
				name:  "Quick Edit: Map Pane",
				desc:  "Controls whether Quick Edit should be shown on the map pane.",
				label: "##quick_edit:map_pane",
				value: &prefs.Controls.QuickEditMapPane,
			},
		},

		wsprefs.GPInterface: {
			intPrefPrefab{
				name:  "Scale",
				desc:  "Controls the interface scale.",
				label: "%##scale",
				min:   50,
				max:   250,
				value: &prefs.Interface.Scale,
				post: func(int) {
					app.UpdateScale()
				},
			},
			intPrefPrefab{
				name:  "Fps",
				desc:  "Controls the application framerate.",
				label: "##fps",
				min:   30,
				max:   math.MaxInt,
				value: &prefs.Interface.Fps,
				post:  window.SetFps,
			},
		},

		wsprefs.GPApplication: {
			// APHELION EDIT ADDITION START - ENVIRONMENT SNAPSHOT
			boolPrefPrefab{
				name:  "Bypass Environment Cache",
				desc:  "Parse project sources on every open. Existing open maps keep their current environment until an explicit reload.",
				label: "##bypass_environment_cache",
				value: &prefs.Application.BypassEnvironmentCache,
			},
			// APHELION EDIT ADDITION END
			boolPrefPrefab{
				name:  "Check for Updates",
				desc:  "When enabled, the editor will always check for updates on startup.",
				label: "##check_for_updates",
				value: &prefs.Application.CheckForUpdates,
			},
			boolPrefPrefab{
				name:  "Auto Update",
				desc:  "Enables automatic self-update, when a new update is available.",
				label: "##auto_update",
				value: &prefs.Application.AutoUpdate,
			},
		},
	}

	// APHELION EDIT ADDITION START - PATH MIGRATION
	if settings := prefs.PathMigration; settings != nil {
		preferencesPrefabs[wsprefs.GPEditor] = append(preferencesPrefabs[wsprefs.GPEditor],
			boolPrefPrefab{
				name:  "Path Migration: Open on Unknown Types",
				desc:  "Show the Path Migration panel when an opened map uses types the environment does not define.",
				label: "##path_migration_open",
				value: &settings.OpenOnUnknown,
			},
			optionPrefPrefab{
				name:    "Path Migration: Automatic Selection",
				desc:    "Which suggestions are selected without a person. Nothing changes the map until Apply, unless the next option is enabled.",
				label:   "##path_migration_auto",
				value:   &settings.Auto,
				options: repath.AutoModes,
			},
			boolPrefPrefab{
				name:  "Path Migration: Apply Certain Results on Open",
				desc:  "Apply Certain selections (UpdatePaths scripts and remembered decisions) as one undoable edit when a map opens.",
				label: "##path_migration_apply_on_open",
				value: &settings.ApplyCertainOnOpen,
			},
			boolPrefPrefab{
				name:  "Path Migration: Read Codebase Scripts",
				desc:  "Read tools/UpdatePaths/Scripts from the loaded codebase.",
				label: "##path_migration_read_scripts",
				value: &settings.ReadCodebaseScripts,
			},
			boolPrefPrefab{
				name:  "Path Migration: Offer to Remember Decisions",
				desc:  "Show a Remember option that keeps chosen rules for maps of the same environment in editor configuration.",
				label: "##path_migration_remember",
				value: &settings.RememberDecisions,
			},
			boolPrefPrefab{
				name:  "Path Migration: Remember Reference Environment",
				desc:  "Reload the last reference environment when this environment opens.",
				label: "##path_migration_remember_reference",
				value: &settings.RememberReferencePath,
			},
			boolPrefPrefab{
				name:  "Path Migration: Allow Saving into Codebase",
				desc:  "Allow saving exported scripts into the codebase's tools/UpdatePaths/Scripts directory.",
				label: "##path_migration_write_scripts",
				value: &settings.WriteCodebaseScripts,
			},
		)
	}
	// APHELION EDIT ADDITION END

	for group, prefabs := range preferencesPrefabs {
		for _, prefab := range prefabs {
			p.Add(group, prefab.make())
		}
	}

	return p
}


