package dialog

import (
	// APHELION EDIT ADDITION START - DIALOG LIFETIME
	"slices"
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SHORTCUT FOCUS
	"sdmm/internal/app/ui/shortcut"
	// APHELION EDIT ADDITION END
	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
)

type Type interface {
	Name() string
	Process()
	HasCloseButton() bool
}

const popupFlags = imgui.WindowFlagsAlwaysAutoResize | imgui.WindowFlagsNoSavedSettings

var opened []Type

func Process() {
	var closedDialogs []Type
	for _, dialog := range opened {
		if !imgui.IsPopupOpen(dialog.Name()) {
			imgui.OpenPopup(dialog.Name())
		}

		var isOpen bool
		if dialog.HasCloseButton() {
			open := true
			isOpen = imgui.BeginPopupModalV(dialog.Name(), &open, popupFlags)
		} else {
			isOpen = imgui.BeginPopupModalV(dialog.Name(), nil, popupFlags)
		}

		if isOpen {
			dialog.Process()
			imgui.EndPopup()
		}

		if !imgui.IsPopupOpen(dialog.Name()) {
			closedDialogs = append(closedDialogs, dialog)
		}
	}

	for _, dialog := range closedDialogs {
		Close(dialog)
	}
}

// Open opens the application dialog.
func Open(t Type) {
	log.Print("opening dialog:", t.Name())
	opened = append(opened, t)
	// APHELION EDIT ADDITION START - SHORTCUT FOCUS
	shortcut.SetModalOpen(true)
	// APHELION EDIT ADDITION END
}

// Close closed the application dialog.
func Close(dialog Type) {
	log.Print("closing dialog:", dialog.Name())
	for idx, t := range opened {
		if dialog.Name() == t.Name() {
			// APHELION EDIT ADDITION START - CANCELLABLE DIALOG WORK
			if closer, ok := t.(interface{ OnClose() }); ok {
				closer.OnClose()
			}
			// APHELION EDIT ADDITION END
			log.Print("dialog closed:", dialog.Name())
			// Clear the removed interface so closed records and callbacks can be collected.
			// APHELION EDIT CHANGE - DIALOG LIFETIME - ORIGINAL: opened = append(opened[:idx], opened[idx+1:]...)
			opened = slices.Delete(opened, idx, idx+1)
			// APHELION EDIT ADDITION START - SHORTCUT FOCUS
			shortcut.SetModalOpen(len(opened) != 0)
			// APHELION EDIT ADDITION END
			return
		}
	}
}
