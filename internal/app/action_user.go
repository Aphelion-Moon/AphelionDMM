package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	collabui "sdmm/internal/aphelion/collab/ui"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/render"
	"sdmm/internal/app/ui/cpwsarea/workspace"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/ui/layout/lnode"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/env"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/platform"
	"sdmm/internal/util"
	"sdmm/internal/util/slice"

	dial "sdmm/internal/app/ui/dialog"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
	"github.com/skratchdot/open-golang/open"
	"github.com/sqweek/dialog"
)

const (
	collaborationActionTimeout         = 15 * time.Second
	hostedCollaborationActionTimeout   = 2 * time.Minute
	hostedCollaborationSignInTimeout   = 5 * time.Minute
	hostedCollaborationSignInPollDelay = time.Second
)

/*
	File similar to action.go, but contains methods triggered by user. (ex. when button clicked)
	Such methods have a "Do" prefix, and they are logged excessively.
*/

func (a *app) DoNewWorkspace() {
	log.Print("new workspace...")
	a.layout.WsArea.AddEmptyWorkspace()
}

// DoOpen opens environment, which user need to select in file dialog.
func (a *app) DoOpen() {
	a.DoOpenV(nil)
}

// DoOpenV opens environment, which user need to select in file dialog.
// Verbose version which handle opening in a specific workspace. Helpful to open maps.
func (a *app) DoOpenV(ws *workspace.Workspace) {
	log.Print("selecting resource to load...")

	startDir := ""
	if a.HasLoadedEnvironment() {
		startDir = a.loadedEnvironment.RootDir
	}

	if file, err := dialog.
		File().
		Title("Open").
		Filter("Resource", "dme", "dmm").
		SetStartDir(startDir).
		Load(); err == nil {
		log.Print("resource to load selected:", file)

		if a.HasLoadedEnvironment() {
			a.loadMap(file, ws)
		} else {
			a.DoLoadResourceV(file, ws)
		}
	}
}

// DoLoadResource opens environment by provided path.
func (a *app) DoLoadResource(path string) {
	a.DoLoadResourceV(path, nil)
}

// DoLoadResourceV opens environment by provided path.
// Verbose version which handle opening in a specific workspace. Helpful to open maps.
func (a *app) DoLoadResourceV(path string, ws *workspace.Workspace) {
	log.Print("load resource by path:", path)
	a.loadResourceV(path, ws)
}

// DoClearRecentEnvironments clears recently opened environments.
func (a *app) DoClearRecentEnvironments() {
	log.Print("clear recent environments")
	a.projectConfig().ClearProjects()
}

// DoCloseEnvironment closes currently opened environment.
func (a *app) DoCloseEnvironment() {
	log.Print("closing environment")
	a.closeEnvironment(func(closed bool) {
		if closed {
			// APHELION EDIT ADDITION START - LOADING RESPONSIVENESS
			a.environmentLoadRequest++
			if a.environmentLoadCancel != nil {
				a.environmentLoadCancel()
			}
			if a.environmentLoadDialog != nil {
				dial.Close(a.environmentLoadDialog)
				a.environmentLoadDialog = nil
			}
			a.environmentLoadFinish = nil
			// APHELION EDIT ADDITION END
			a.freeEnvironmentResources()
			a.layout.WsArea.AddEmptyWorkspaceIfNone()
		}
	})
}

// DoNewMap opens dialog window to create a new map file.
func (a *app) DoNewMap() {
	log.Print("opening create map...")
	a.layout.WsArea.OpenCreateMap()
}

// DoClearRecentMaps clears recently opened maps.
func (a *app) DoClearRecentMaps() {
	log.Print("clear recent maps")
	a.projectConfig().ClearMaps()
}

// DoRemoveRecentMaps removes specific recent maps.
func (a *app) DoRemoveRecentMaps(recentMaps []string) {
	log.Print("do remove recent maps:", recentMaps)
	recentMaps = append(make([]string, 0, len(recentMaps)), recentMaps...)
	for _, recentMap := range recentMaps {
		a.projectConfig().RemoveMap(recentMap)
	}
}

// DoRemoveRecentEnvironment removes specific recent environment.
func (a *app) DoRemoveRecentEnvironment(envPath string) {
	log.Print("remove recent environment:", envPath)
	a.projectConfig().RemoveEnvironment(envPath)
}

// DoRemoveRecentMap removes specific recent map.
func (a *app) DoRemoveRecentMap(mapPath string) {
	log.Print("remove recent map:", mapPath)
	a.projectConfig().RemoveMap(mapPath)
}

// DoClose closes currently active workspace.
func (a *app) DoClose() {
	// APHELION EDIT ADDITION START - COLLABORATION
	if a.collaborationEditor != nil && a.CurrentEditor() == a.collaborationEditor {
		guard, err := a.collaborationProjectReplacementGuard()
		if err != nil {
			log.Error().Err(err).Msg("unable to prepare collaborative workspace close")
			util.ShowErrorDialog("Unable to close workspace: " + err.Error())
			return
		}
		a.layout.WsArea.CloseGuarded(guard, nil)
		return
	}
	// APHELION EDIT ADDITION END
	a.layout.WsArea.Close()
}

// DoCloseAll closes all opened workspaces.
func (a *app) DoCloseAll() {
	// APHELION EDIT ADDITION START - COLLABORATION
	if a.HasActiveCollaboration() {
		guard, err := a.collaborationProjectReplacementGuard()
		if err != nil {
			log.Error().Err(err).Msg("unable to prepare collaborative workspace close")
			util.ShowErrorDialog("Unable to close workspaces: " + err.Error())
			return
		}
		a.layout.WsArea.CloseAllGuarded(guard, nil)
		return
	}
	// APHELION EDIT ADDITION END
	a.layout.WsArea.CloseAll()
}

// APHELION EDIT ADDITION START - COLLABORATION

func (a *app) DoCreateLocalCollaborationSession() {
	selectedEditor := a.CurrentEditor()
	if selectedEditor == nil {
		util.ShowErrorDialog("Unable to start collaboration: no map is active")
		return
	}
	ownerName := "Owner"
	dial.Open(dial.TypeCustom{
		Title:       "Start Local Collaboration Session",
		CloseButton: true,
		Layout: w.Layout{
			w.Text("Choose the name other participants will see."),
			w.InputTextWithHint("##collaboration-owner-name", "Display name", &ownerName).Width(-1),
			w.Button("Start Session", func() {
				displayName := strings.TrimSpace(ownerName)
				if displayName == "" {
					util.ShowErrorDialog("Unable to start collaboration: display name is required")
					return
				}
				if a.CurrentEditor() != selectedEditor || a.HasActiveCollaboration() {
					util.ShowErrorDialog("Unable to start collaboration: the active map or session changed")
					return
				}
				imgui.CloseCurrentPopup()
				a.createLocalCollaborationSession(selectedEditor, displayName)
			}),
		},
	})
}

func (a *app) DoSignInHostedCollaboration() {
	if a.collaborationClient == nil || a.HasActiveCollaboration() {
		return
	}
	hostedURL := collabui.DefaultHostedOrigin
	dial.Open(dial.TypeCustom{
		Title:       "Sign In to Hosted Collaboration",
		CloseButton: true,
		Layout: w.Layout{
			w.Text("Enter the HTTPS address of the hosted collaboration service."),
			w.InputTextWithHint("##collaboration-hosted-url", "https://collaboration.example", &hostedURL).Width(-1),
			w.Button("Sign In", func() {
				baseURL := strings.TrimSpace(hostedURL)
				if baseURL == "" {
					util.ShowErrorDialog("Unable to sign in: hosted service address is required")
					return
				}
				if a.HasActiveCollaboration() || a.collaborationClient.HostedSignedIn() {
					util.ShowErrorDialog("Unable to sign in: the collaboration state changed")
					return
				}
				imgui.CloseCurrentPopup()
				go a.signInHostedCollaboration(baseURL)
			}),
		},
	})
}

func (a *app) signInHostedCollaboration(baseURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), hostedCollaborationSignInTimeout)
	defer cancel()
	signIn, err := a.collaborationClient.BeginHostedSignIn(ctx, baseURL)
	if err == nil {
		err = open.Run(signIn.AuthorizationURL)
	}
	for err == nil {
		err = a.collaborationClient.CompleteHostedSignIn(ctx, signIn)
		if !errors.Is(err, collabui.ErrHostedSignInPending) {
			break
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-time.After(hostedCollaborationSignInPollDelay):
			err = nil
		}
	}
	window.RunLater(func() {
		if err != nil {
			log.Error().Err(err).Msg("Unable to sign in to hosted collaboration")
			util.ShowErrorDialog("Unable to sign in to hosted collaboration: " + err.Error())
			return
		}
		log.Info().Msg("signed in to hosted collaboration")
	})
}

func (a *app) DoCreateHostedCollaborationSession() {
	selectedEditor := a.CurrentEditor()
	if selectedEditor == nil || a.collaborationClient == nil || !a.collaborationClient.HostedSignedIn() || a.HasActiveCollaboration() {
		return
	}
	snapshot, err := selectedEditor.CollaborationSnapshot(context.Background())
	if err != nil {
		util.ShowErrorDialog("Unable to start hosted collaboration: " + err.Error())
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), hostedCollaborationActionTimeout)
		defer cancel()
		invitation, prepareErr := a.collaborationClient.CreateHosted(ctx, snapshot)
		var execution executor.Executor
		if prepareErr == nil {
			execution, prepareErr = collabui.PrepareJoinedSession(ctx, a.collaborationController, a.collaborationClient, invitation)
		}
		window.RunLater(func() {
			if prepareErr != nil {
				log.Error().Err(prepareErr).Msg("Unable to start hosted collaboration")
				util.ShowErrorDialog("Unable to start hosted collaboration: " + prepareErr.Error())
				return
			}
			attachErr := collabui.AttachPreparedSession(execution, selectedEditor, a.CurrentEditor() == selectedEditor)
			if attachErr == nil {
				a.collaborationEditor = selectedEditor
				return
			}
			log.Error().Err(attachErr).Msg("Unable to attach hosted collaboration session")
			util.ShowErrorDialog("Unable to attach hosted collaboration session: " + attachErr.Error())
			go a.leaveCollaborationAfterAttachmentFailure()
		})
	}()
}

func (a *app) DoSignOutHostedCollaboration() {
	client := a.collaborationClient
	if client == nil || a.HasActiveCollaboration() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
		defer cancel()
		err := client.SignOutHosted(ctx)
		window.RunLater(func() {
			if err != nil {
				log.Error().Err(err).Msg("Unable to sign out of hosted collaboration")
				util.ShowErrorDialog("Unable to sign out of hosted collaboration: " + err.Error())
			}
		})
	}()
}

func (a *app) createLocalCollaborationSession(selectedEditor *editor.Editor, displayName string) {
	snapshot, err := selectedEditor.CollaborationSnapshot(context.Background())
	if err != nil {
		util.ShowErrorDialog("Unable to start collaboration: " + err.Error())
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
		defer cancel()
		execution, prepareErr := collabui.PrepareNamedLocalSession(ctx, a.collaborationController, a.collaborationClient, snapshot, displayName)
		window.RunLater(func() {
			if prepareErr != nil {
				log.Error().Err(prepareErr).Msg("Unable to start collaboration")
				util.ShowErrorDialog("Unable to start collaboration: " + prepareErr.Error())
				return
			}
			attachErr := collabui.AttachPreparedSession(execution, selectedEditor, a.CurrentEditor() == selectedEditor)
			if attachErr == nil {
				a.collaborationEditor = selectedEditor
				return
			}
			log.Error().Err(attachErr).Msg("Unable to attach collaboration session")
			util.ShowErrorDialog("Unable to attach collaboration session: " + attachErr.Error())
			go a.leaveCollaborationAfterAttachmentFailure()
		})
	}()
}

func (a *app) DoJoinCollaborationSession() {
	selectedEditor := a.CurrentEditor()
	if selectedEditor == nil {
		util.ShowErrorDialog("Unable to join collaboration: no map is active")
		return
	}
	var encodedInvitation string
	dial.Open(dial.TypeCustom{
		Title:       "Join Collaboration Session",
		CloseButton: true,
		Layout: w.Layout{
			w.Text("Paste the invitation shared by the session owner."),
			w.InputTextWithHint("##collaboration-invitation", "Invitation", &encodedInvitation).Width(-1),
			w.Button("Join Session", func() {
				if a.CurrentEditor() != selectedEditor || a.HasActiveCollaboration() {
					util.ShowErrorDialog("Unable to join collaboration: the active map or session changed")
					return
				}
				invitation, err := collabui.ParseInvitation(strings.TrimSpace(encodedInvitation))
				encodedInvitation = ""
				if err != nil {
					util.ShowErrorDialog("Unable to join collaboration: " + err.Error())
					return
				}
				imgui.CloseCurrentPopup()
				go a.joinCollaborationSession(invitation, selectedEditor)
			}),
		},
	})
}

func (a *app) joinCollaborationSession(invitation collabui.Invitation, selectedEditor *editor.Editor) {
	timeout := collaborationActionTimeout
	if invitation.Hosted {
		timeout = hostedCollaborationActionTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var prepareErr error
	if invitation.Hosted {
		invitation, prepareErr = a.collaborationClient.RedeemHostedInvitation(ctx, invitation)
	}
	var execution executor.Executor
	if prepareErr == nil {
		execution, prepareErr = collabui.PrepareJoinedSession(ctx, a.collaborationController, a.collaborationClient, invitation)
	}
	window.RunLater(func() {
		if prepareErr != nil {
			log.Error().Err(prepareErr).Msg("Unable to join collaboration")
			util.ShowErrorDialog("Unable to join collaboration: " + prepareErr.Error())
			return
		}
		attachErr := collabui.AttachPreparedSession(execution, selectedEditor, a.CurrentEditor() == selectedEditor)
		if attachErr == nil {
			a.collaborationEditor = selectedEditor
			return
		}
		log.Error().Err(attachErr).Msg("Unable to attach joined collaboration session")
		util.ShowErrorDialog("Unable to attach joined collaboration session: " + attachErr.Error())
		go a.leaveCollaborationAfterAttachmentFailure()
	})
}

func (a *app) DoCopyCollaborationInvitation(role collabui.InvitationRole, displayName string) {
	client := a.collaborationClient
	if client == nil || !a.HasActiveCollaboration() {
		return
	}
	sessionID := client.Status().SessionID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
		defer cancel()
		invitation, err := client.CreateInvitation(ctx, role, displayName)
		if err == nil {
			var encoded string
			encoded, err = collabui.EncodeInvitation(invitation)
			if err == nil {
				window.RunLater(func() {
					if a.collaborationClient != client || !a.HasActiveCollaboration() || client.Status().SessionID != sessionID {
						return
					}
					platform.SetClipboard(encoded)
					log.Info().Str("role", string(role)).Msg("copied short-lived collaboration invitation")
				})
				return
			}
		}
		window.RunLater(func() {
			util.ShowErrorDialog("Unable to create collaboration invitation: " + err.Error())
		})
	}()
}

func (a *app) DoUpdateCollaborationDisplayName(displayName string) {
	client := a.collaborationClient
	if client == nil || !a.HasActiveCollaboration() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
		defer cancel()
		if err := client.UpdateDisplayName(ctx, displayName); err != nil {
			window.RunLater(func() {
				util.ShowErrorDialog("Unable to update collaboration display name: " + err.Error())
			})
		}
	}()
}

func (a *app) DoLeaveCollaborationSession() {
	if a.collaborationController == nil || !a.collaborationController.Active() {
		return
	}
	permit, err := a.collaborationController.BeginProjectReplacement()
	if err != nil {
		util.ShowErrorDialog("Unable to leave collaboration: " + err.Error())
		return
	}
	if a.collaborationEditor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
		err := a.collaborationEditor.DetachCollaborationExecutor(ctx)
		cancel()
		if err != nil {
			util.ShowErrorDialog("Unable to leave collaboration: " + err.Error())
			return
		}
		a.collaborationEditor = nil
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
		defer cancel()
		if err := a.collaborationController.CompleteProjectReplacement(ctx, permit); err != nil {
			log.Error().Err(err).Msg("Unable to leave collaboration")
			window.RunLater(func() {
				util.ShowErrorDialog("Unable to leave collaboration: " + err.Error())
			})
		}
	}()
}

func (a *app) DoRetryCollaborationSession() {
	client := a.collaborationClient
	if client == nil || !a.HasActiveCollaboration() {
		return
	}
	if err := client.RetryReconnect(); err != nil {
		log.Error().Err(err).Msg("Unable to retry collaboration reconnect")
		util.ShowErrorDialog("Unable to retry collaboration reconnect: " + err.Error())
	}
}

func (a *app) DoResolveCollaborationConflict(operationID model.OperationID, action collabui.ConflictAction) {
	client := a.collaborationClient
	editor := a.collaborationEditor
	if client == nil || editor == nil || !a.HasActiveCollaboration() {
		return
	}
	refresh := func() {
		if a.collaborationClient != client || a.collaborationEditor != editor || !a.HasActiveCollaboration() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
		defer cancel()
		if err := editor.RefreshCollaborationSnapshot(ctx); err != nil {
			log.Error().Err(err).Msg("Unable to synchronize conflict resolution")
			util.ShowErrorDialog("Unable to synchronize conflict resolution: " + err.Error())
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
	switch action {
	case collabui.ConflictActionExport:
		cancel()
		path, err := dialog.File().Title("Export Collaboration Draft").Filter("JSON draft", "json").SetStartFile("collaboration-draft.json").Save()
		if errors.Is(err, dialog.ErrCancelled) {
			return
		}
		if err == nil {
			err = client.ExportConflict(operationID, path)
		}
		if err != nil {
			util.ShowErrorDialog("Unable to export collaboration draft: " + err.Error())
		}
	case collabui.ConflictActionRefresh:
		_, err := client.RefreshConflict(ctx, operationID)
		cancel()
		if err != nil {
			util.ShowErrorDialog("Unable to refresh conflict: " + err.Error())
			return
		}
		refresh()
	case collabui.ConflictActionDiscard:
		_, err := client.DiscardConflict(ctx, operationID)
		cancel()
		if err != nil {
			util.ShowErrorDialog("Unable to discard conflict: " + err.Error())
			return
		}
		refresh()
	case collabui.ConflictActionRebuild:
		cancel()
		err := client.RebuildConflict(context.Background(), operationID, func(_ model.AcceptedOperation, rebuildErr error) {
			window.RunLater(func() {
				if rebuildErr != nil {
					log.Error().Err(rebuildErr).Msg("Unable to rebuild conflict")
					util.ShowErrorDialog("Unable to rebuild conflict: " + rebuildErr.Error())
					return
				}
				refresh()
			})
		})
		if err != nil {
			util.ShowErrorDialog("Unable to rebuild conflict: " + err.Error())
		}
	default:
		cancel()
		util.ShowErrorDialog("Unable to resolve conflict: unsupported action")
	}
}

func (a *app) HasActiveCollaboration() bool {
	return a.collaborationController != nil && a.collaborationController.Active()
}

func (a *app) HasHostedCollaborationSignIn() bool {
	return a.collaborationClient != nil && a.collaborationClient.HostedSignedIn()
}

func (a *app) DoOpenCollaborationPanel() {
	a.ShowLayout(lnode.NameCollaboration, true)
}

func (a *app) leaveCollaborationAfterAttachmentFailure() {
	ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
	defer cancel()
	if err := a.collaborationController.Leave(ctx); err != nil {
		log.Error().Err(err).Msg("Unable to clean up unattached collaboration session")
	}
}

// APHELION EDIT ADDITION END

// DoSave saves current active map.
func (a *app) DoSave() {
	log.Print("do save")
	if ws, ok := a.activeWsMap(); ok {
		ws.Save()
	}
}

// DoSaveAll saves all active maps.
func (a *app) DoSaveAll() {
	log.Print("do save all")
	for _, ws := range a.layout.WsArea.MapWorkspaces() {
		ws.Save()
	}
}

// DoOpenPreferences opens preferences tab.
func (a *app) DoOpenPreferences() {
	log.Print("open preferences")
	a.layout.WsArea.OpenPreferences(prefs.Make(a, &a.preferencesConfig().Prefs))
}

// DoSelectPrefab globally selects provided prefab in the app.
func (a *app) DoSelectPrefab(prefab *dmmprefab.Prefab) {
	log.Printf("select prefab: path=[%s], id=[%d]", prefab.Path(), prefab.Id())
	a.layout.Environment.SelectPath(prefab.Path())
	a.layout.Prefabs.Select(prefab)
}

// DoSelectPrefabByPath globally selects a prefab with provided type path.
func (a *app) DoSelectPrefabByPath(path string) {
	log.Print("select prefab by path:", path)
	a.DoSelectPrefab(dmmap.PrefabStorage.Initial(path))
}

// DoEditInstance enables an editing for the provided instance.
func (a *app) DoEditInstance(instance *dmminstance.Instance) {
	log.Print("edit instance:", instance.Id())
	a.layout.VarEditor.EditInstance(instance)
}

// DoEditPrefab enables an editing for the provided prefab.
func (a *app) DoEditPrefab(prefab *dmmprefab.Prefab) {
	log.Print("edit prefab:", prefab.Id())
	a.layout.VarEditor.EditPrefab(prefab)
}

// DoEditPrefabByPath enables an editing for the provided prefab by its path.
func (a *app) DoEditPrefabByPath(path string) {
	log.Print("edit prefab by path:", path)
	a.DoEditPrefab(dmmap.PrefabStorage.Initial(path))
}

// DoSearchPrefab does a search of the provided prefab ID.
func (a *app) DoSearchPrefab(prefabId uint64) {
	log.Print("search prefab id:", prefabId)
	a.layout.Search.Search(prefabId)
}

// DoSearchPrefabByPath does a search of the provided prefab path.
func (a *app) DoSearchPrefabByPath(path string) {
	log.Print("search prefab path:", path)
	a.layout.Search.SearchByPath(path)
}

// DoExit exits the app.
func (a *app) DoExit() {
	log.Print("exit")
	a.tmpShouldClose = true
}

// DoUndo does undo of the latest command.
func (a *app) DoUndo() {
	log.Print("undo")
	// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: a.commandStorage.Undo()
	a.commandStorage.UndoAsync(nil)
}

// DoRedo does redo of the previous command.
func (a *app) DoRedo() {
	log.Print("redo")
	// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: a.commandStorage.Redo()
	a.commandStorage.RedoAsync(nil)
}

// DoResetLayout resets application windows to their initial positions.
func (a *app) DoResetLayout() {
	log.Print("reset layout")
	a.resetLayout()
}

// DoOpenChangelog opens "changelog" workspace.
func (a *app) DoOpenChangelog() {
	log.Print("open changelog")
	a.layout.WsArea.OpenChangelog()
}

// DoOpenAbout opens "about" window.
func (a *app) DoOpenAbout() {
	log.Print("open about")
	a.openAboutWindow()
}

// DoOpenLogs opens the logs folder.
func (a *app) DoOpenLogs() {
	log.Print("open logs dir:", a.logDir)
	if err := open.Run(a.logDir); err != nil {
		log.Print("unable to open log dir:", err)
	}
}

// DoOpenSourceCode opens GitHub with a source code for the editor.
func (a *app) DoOpenSourceCode() {
	log.Print("open source code:", env.GitHub)
	if err := open.Run(env.GitHub); err != nil {
		log.Print("unable to open GitHub:", err)
	}
}

// DoOpenSupport opens support page.
func (a *app) DoOpenSupport() {
	log.Print("open source code:", env.Support)
	if err := open.Run(env.Support); err != nil {
		log.Print("unable to open support page:", err)
	}
}

// DoCopy copies currently selected (hovered) tiles to the global clipboard.
func (a *app) DoCopy() {
	log.Print("do copy")
	if ws, ok := a.activeWsMap(); ok {
		ws.Map().Editor().TileCopySelected()
	}
}

// DoPaste pastes tiles from the global clipboard on the currently hovered tile.
func (a *app) DoPaste() {
	log.Print("do paste")
	if ws, ok := a.activeWsMap(); ok {
		ws.Map().Editor().TilePasteSelected()
		/* APHELION EDIT REMOVAL START - PASTE PLACEMENT
		ws.Map().Editor().CommitOperation("Paste Tile")
		APHELION EDIT REMOVAL END */
	}
}

// DoCut cuts currently selected (hovered) tiles to the global clipboard.
func (a *app) DoCut() {
	log.Print("do cut")
	if ws, ok := a.activeWsMap(); ok {
		ws.Map().Editor().TileCutSelected()
		ws.Map().Editor().CommitOperation("Cut Tile")
	}
}

// DoDelete deletes tiles from the currently selected (hovered) tiles.
func (a *app) DoDelete() {
	log.Print("do delete")
	if ws, ok := a.activeWsMap(); ok {
		ws.Map().Editor().TileDeleteSelected()
		ws.Map().Editor().CommitOperation("Delete Tile")
	}
}

// DoDeselect deselects currently selected area.
func (a *app) DoDeselect() {
	log.Print("do deselect")
	if ws, ok := a.activeWsMap(); ok {
		ws.Map().DoDeselect()
	}
}

// DoSearch searches for a currently selected prefab.
func (a *app) DoSearch() {
	log.Print("do search")
	if prefabId := a.layout.Prefabs.SelectedPrefabId(); prefabId != dmmprefab.IdNone {
		a.DoSearchPrefab(prefabId)
	}
	a.ShowLayout(lnode.NameSearch, true)
}

// DoAreaBorders toggles area borders rendering.
func (a *app) DoAreaBorders() {
	pmap.AreaBordersRendering = !pmap.AreaBordersRendering
	log.Print("do area borders:", pmap.AreaBordersRendering)
}

// DoMultiZRendering toggles multi-z rendering.
func (a *app) DoMultiZRendering() {
	render.MultiZRendering = !render.MultiZRendering
	log.Print("do multiZ rendering:", render.MultiZRendering)
}

// DoMirrorCanvasCamera toggles mode of mirroring canvas camera.
func (a *app) DoMirrorCanvasCamera() {
	pmap.MirrorCanvasCamera = !pmap.MirrorCanvasCamera
	log.Print("do mirror canvas camera:", pmap.MirrorCanvasCamera)
}

// DoSelfUpdate starts the process of a self update.
func (a *app) DoSelfUpdate() {
	log.Print("do self update")
	a.selfUpdate()
}

// DoRestart restarts the application.
func (a *app) DoRestart() {
	log.Print("do restart")
	window.Restart()
}

// DoIgnoreUpdate adds currently available update to to ignore list.
func (a *app) DoIgnoreUpdate() {
	log.Print("do ignore update:", remoteManifest.Version)
	a.config().UpdateIgnore = slice.StrPushUnique(a.config().UpdateIgnore, remoteManifest.Version)
}

// DoCheckForUpdates checks for available update.
func (a *app) DoCheckForUpdates() {
	log.Print("do check for updates")
	a.checkForUpdatesV(true)
}

// DoOpenJumpWindow opens a window where the user can jump to the inputted coordinates.
func (a *app) DoOpenJumpWindow() {
	log.Print("do jump")
	var x, y, z string
	z = strconv.Itoa(a.CurrentEditor().ActiveLevel())
	dial.Open(dial.TypeCustom{
		Title:       "Jump/Go To Coordinate",
		CloseButton: true,
		Layout: w.Layout{
			w.AlignTextToFramePadding(),
			w.InputTextWithHint("##x", "X", &x),
			w.InputTextWithHint("##y", "Y", &y),
			w.InputTextWithHint("##z", "Z", &z),
			w.Button("Jump!", func() {
				intX, errX := strconv.Atoi(x)
				intY, errY := strconv.Atoi(y)
				intZ, errZ := strconv.Atoi(z)
				if errX == nil && errY == nil && errZ == nil {
					e := a.CurrentEditor()
					e.FocusCameraOnPosition(util.Point{X: intX, Y: intY, Z: intZ})
					imgui.CloseCurrentPopup()
				}
			}),
		},
	})
}
