package ui

import (
	"fmt"

	w "sdmm/internal/imguiext/widget"

	"github.com/SpaiR/imgui-go"
)

// DraftBulkApp is the part of the application the bulk draft controls drive.
type DraftBulkApp interface {
	DoResolveAllCollaborationConflicts(BulkDraftAction, bool)
	DoRetryCollaborationSession()
}

// DraftPromptReason says why the "Unsent collaboration changes" prompt opened.
type DraftPromptReason string

const (
	DraftPromptDisconnected DraftPromptReason = "disconnected"
	DraftPromptLeave        DraftPromptReason = "leave"
	DraftPromptClose        DraftPromptReason = "close"
)

func (reason DraftPromptReason) text(count int) string {
	drafts := draftCountText(count)
	switch reason {
	case DraftPromptLeave:
		return fmt.Sprintf("%s remain unresolved, so the session cannot be left yet. Resolve them below, or keep them and stay in the session.", drafts)
	case DraftPromptClose:
		return fmt.Sprintf("%s remain unresolved, so the project cannot be closed yet. Resolve them below, or keep them and keep the project open.", drafts)
	default:
		return fmt.Sprintf("The session is disconnected and %s could not be delivered. Nothing has been discarded. Resolve them below, or keep them and retry when the connection returns.", drafts)
	}
}

func draftCountText(count int) string {
	if count == 1 {
		return "1 unsent draft"
	}
	return fmt.Sprintf("%d unsent drafts", count)
}

// draftBulkControls renders the mass actions shared by the panel and the
// prompt. The zero value ticks "export first".
type draftBulkControls struct {
	confirmDiscard bool
	skipExport     bool
}

func (controls *draftBulkControls) render(app DraftBulkApp, view ViewModel, keep func()) {
	if view.DraftCount == 0 {
		controls.confirmDiscard = false
		return
	}
	if controls.confirmDiscard {
		imgui.TextWrapped(fmt.Sprintf("Discard %s? Discarded drafts cannot be recovered.", draftCountText(view.DraftCount)))
		exportFirst := !controls.skipExport
		if imgui.Checkbox("Export first (recommended)##bulk-export-first", &exportFirst) {
			controls.skipExport = !exportFirst
		}
		w.Button(fmt.Sprintf("Discard %d##bulk-discard-confirm", view.DraftCount), func() {
			controls.confirmDiscard = false
			app.DoResolveAllCollaborationConflicts(BulkDiscardAll, exportFirst)
		}).Build()
		imgui.SameLine()
		w.Button("Cancel##bulk-discard-cancel", func() { controls.confirmDiscard = false }).Build()
		return
	}
	w.Button("Export all##bulk-export", func() { app.DoResolveAllCollaborationConflicts(BulkExportAll, false) }).Build()
	imgui.SameLine()
	w.Button("Discard all...##bulk-discard", func() { controls.confirmDiscard = true }).Build()
	imgui.SameLine()
	w.Button("Refresh all##bulk-refresh", func() { app.DoResolveAllCollaborationConflicts(BulkRefreshAll, false) }).Build()
	if view.ReconnectGaveUp {
		w.Disabled(!view.CanReconnect, w.Button("Retry Reconnect##bulk-retry", app.DoRetryCollaborationSession)).Build()
		imgui.SameLine()
	}
	w.Button("Keep for later / retry when reconnected##bulk-keep", keep).Build()
}

// DraftPrompt is the modal "Unsent collaboration changes" dialog. It satisfies
// the application's dialog type without importing it.
type DraftPrompt struct {
	App    DraftBulkApp
	View   func() ViewModel
	Reason DraftPromptReason
	Closed func()

	controls draftBulkControls
}

func (*DraftPrompt) Name() string         { return "Unsent collaboration changes" }
func (*DraftPrompt) HasCloseButton() bool { return true }

func (prompt *DraftPrompt) OnClose() {
	if prompt.Closed != nil {
		prompt.Closed()
	}
}

func (prompt *DraftPrompt) Process() {
	view := prompt.View()
	if view.DraftCount == 0 {
		imgui.CloseCurrentPopup()
		return
	}
	imgui.PushTextWrapPosV(520)
	imgui.TextWrapped(prompt.Reason.text(view.DraftCount))
	if view.StatusBanner != "" {
		imgui.Spacing()
		imgui.TextWrapped(view.StatusBanner)
	}
	imgui.TextWrapped("Export saves a recovery reference for each draft; it does not apply the edit.")
	imgui.PopTextWrapPos()
	imgui.Separator()
	prompt.controls.render(prompt.App, view, imgui.CloseCurrentPopup)
}
