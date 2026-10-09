package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/sqweek/dialog"

	"sdmm/internal/aphelion/collab/model"
	collabui "sdmm/internal/aphelion/collab/ui"
	dial "sdmm/internal/app/ui/dialog"
	"sdmm/internal/util"
)

// openCollaborationDraftPrompt shows the modal "Unsent collaboration changes"
// dialog. A forced open (leave, close) and the automatic disconnect prompt
// share one dialog; a second request while it is open is ignored.
func (a *app) openCollaborationDraftPrompt(reason collabui.DraftPromptReason) {
	if a.collaborationClient == nil || a.collaborationDraftPrompt != nil {
		return
	}
	prompt := &collabui.DraftPrompt{
		App:    a,
		View:   a.collaborationClient.DraftPromptView,
		Reason: reason,
		Closed: func() {
			a.collaborationDraftPrompt = nil
			// Closing the prompt keeps the drafts; it must not return this episode.
			a.collaborationDraftGate.Dismiss()
		},
	}
	a.collaborationDraftPrompt = prompt
	a.collaborationDraftGate.Dismiss()
	dial.Open(prompt)
}

// showCollaborationGuardError reports why leaving or closing was refused.
// Retained drafts open the shared prompt instead of a plain error dialog.
func (a *app) showCollaborationGuardError(prefix string, err error, leaving bool) {
	if errors.Is(err, collabui.ErrRetainedDrafts) {
		reason := collabui.DraftPromptClose
		if leaving {
			reason = collabui.DraftPromptLeave
		}
		a.openCollaborationDraftPrompt(reason)
		return
	}
	util.ShowErrorDialog(prefix + err.Error())
}

// processCollaborationDraftPrompt runs every frame. It opens the prompt once
// per disconnect episode when drafts remain and the session cannot deliver them.
func (a *app) processCollaborationDraftPrompt() {
	if a.collaborationClient == nil || !a.HasActiveCollaboration() {
		a.collaborationDraftGate.Reset()
		return
	}
	if a.collaborationDraftGate.Observe(a.collaborationClient.DraftPromptView()) {
		a.openCollaborationDraftPrompt(collabui.DraftPromptDisconnected)
	}
}

// DoResolveAllCollaborationConflicts applies one per-draft resolver to every
// retained draft, not only the visible page, and stops at the first failure.
func (a *app) DoResolveAllCollaborationConflicts(action collabui.BulkDraftAction, exportFirst bool) {
	client := a.collaborationClient
	editor := a.collaborationEditor
	if client == nil || !a.HasActiveCollaboration() {
		return
	}
	ids := client.RetainedDraftIDs()
	if len(ids) == 0 {
		return
	}
	exportDir := ""
	if action == collabui.BulkExportAll || (action == collabui.BulkDiscardAll && exportFirst) {
		chosen, err := dialog.Directory().Title("Export Collaboration Drafts").Browse()
		if errors.Is(err, dialog.ErrCancelled) {
			return
		}
		if err == nil {
			// A fresh folder keeps earlier recovery files from being replaced.
			exportDir = filepath.Join(chosen, "collaboration-drafts-"+time.Now().Format("20060102-150405"))
			err = os.MkdirAll(exportDir, 0o755)
		}
		if err != nil {
			util.ShowErrorDialog("Unable to export collaboration drafts: " + err.Error())
			return
		}
	}
	position := make(map[model.OperationID]int, len(ids))
	for index, id := range ids {
		position[id] = index
	}
	resolve := func(id model.OperationID, step collabui.ConflictAction) error {
		ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
		defer cancel()
		switch step {
		case collabui.ConflictActionExport:
			return client.ExportConflict(id, filepath.Join(exportDir, collabui.DraftExportFileName(position[id], id)))
		case collabui.ConflictActionRefresh:
			_, err := client.RefreshConflict(ctx, id)
			return err
		case collabui.ConflictActionDiscard:
			_, err := client.DiscardConflict(ctx, id)
			return err
		default:
			return errors.New("unsupported draft action")
		}
	}
	outcome := collabui.RunBulkDraftAction(action, exportFirst, ids, resolve)
	if action != collabui.BulkExportAll && outcome.Completed != 0 && editor != nil && a.collaborationClient == client && a.collaborationEditor == editor && a.HasActiveCollaboration() {
		ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
		if err := editor.RefreshCollaborationSnapshot(ctx); err != nil {
			log.Error().Err(err).Msg("Unable to synchronize bulk conflict resolution")
			util.ShowErrorDialog("Unable to synchronize conflict resolution: " + err.Error())
		}
		cancel()
	}
	if message := outcome.Message(); message != "" {
		log.Warn().Str("action", string(action)).Int("completed", outcome.Completed).Int("remaining", outcome.Remaining).Msg("bulk collaboration draft action stopped")
		util.ShowErrorDialog(message)
	}
}
