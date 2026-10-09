// APHELION EDIT ADDITION START - JOIN INTO NEW DOCUMENT
package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"

	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	collabui "sdmm/internal/aphelion/collab/ui"
	"sdmm/internal/aphelion/repoinfo"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/platform"
	"sdmm/internal/util"
)

// untitledDirectory only gives untitled documents a unique identity beside the
// environment. Nothing is ever read from or written to it.
const untitledDirectory = ".aphelion-untitled"

// collaborationJoinNewTabBlocker explains why a session cannot open as a new
// untitled document. No map has to be open, but the environment must be loaded.
func (a *app) collaborationJoinNewTabBlocker() string {
	if a.HasActiveCollaboration() {
		return "Leave the current session before opening another."
	}
	if !a.HasLoadedEnvironment() {
		return "Load the same environment as the host before opening this session."
	}
	return ""
}

// collaborationLocalInspection runs on the UI thread and binds the loaded
// environment. The returned inspection performs all git work off the UI thread
// through the fixed repoinfo adapter; it exposes only a file name and hashes.
func (a *app) collaborationLocalInspection() func(context.Context) (collabui.LocalEnvironment, error) {
	environment := a.LoadedEnvironment()
	return func(ctx context.Context) (collabui.LocalEnvironment, error) {
		if environment == nil {
			return collabui.LocalEnvironment{}, fmt.Errorf("no environment is loaded")
		}
		hash, err := mapadapter.EnvironmentHash(environment)
		if err != nil {
			return collabui.LocalEnvironment{}, err
		}
		local, err := repoinfo.Default().Inspect(ctx, environment.RootDir)
		return collabui.LocalEnvironment{DMEName: filepath.Base(environment.RootFile), EnvironmentHash: hash, Repository: local}, err
	}
}

// hostRepositoryDescriptor builds the optional descriptor for hosting. Any
// failure leaves it unknown instead of blocking the session.
func hostRepositoryDescriptor(ctx context.Context, environment *dmenv.Dme, environmentHash string) *protocol.RepositoryDescriptor {
	if environment == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	descriptor, err := repoinfo.Default().Describe(ctx, environment.RootFile, environmentHash)
	if err != nil {
		log.Warn().Err(err).Msg("repository descriptor unavailable; hosting without it")
		return nil
	}
	return &descriptor
}

func (a *app) copyAlignmentCommands(text string) {
	platform.SetClipboard(text)
	log.Info().Msg("copied git alignment commands")
}

// joinBrowsedHostedSessionNewTab joins a browsed hosted session into a new
// untitled document. The editor never runs the suggested git commands.
func (a *app) joinBrowsedHostedSessionNewTab(ctx context.Context, id string, host *protocol.RepositoryDescriptor, done func(error)) {
	if reason := a.collaborationJoinNewTabBlocker(); reason != "" {
		done(fmt.Errorf("%s", reason))
		return
	}
	environment := a.LoadedEnvironment()
	account := a.collaborationClient.HostedAccount()
	go func() {
		target, err := a.collaborationClient.AdmitHostedSession(ctx, account, id)
		var execution executor.Executor
		if err == nil {
			execution, err = collabui.PrepareHostedSession(ctx, a.collaborationController, a.collaborationClient, target)
		}
		var snapshot model.Snapshot
		if err == nil {
			snapshot, err = execution.Snapshot(ctx)
			if err != nil {
				go a.leaveCollaborationAfterAttachmentFailure()
			}
		}
		window.RunLater(func() {
			if err != nil {
				done(err)
				return
			}
			current := ctx.Err() == nil && a.LoadedEnvironment() == environment && a.collaborationClient.HostedAccountCurrent(account)
			done(a.finishJoinIntoNewDocument(current, environment, execution, snapshot, host))
		})
	}()
}

// joinCollaborationInvitationNewDocument is the invitation equivalent used
// when no map is open. Invitations carry no repository descriptor.
func (a *app) joinCollaborationInvitationNewDocument(invitation collabui.Invitation) {
	timeout := collaborationActionTimeout
	if invitation.Hosted {
		timeout = hostedCollaborationActionTimeout
	}
	environment := a.LoadedEnvironment()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var err error
	if invitation.Hosted {
		invitation, err = a.collaborationClient.RedeemHostedInvitation(ctx, invitation)
	}
	var execution executor.Executor
	if err == nil {
		execution, err = collabui.PrepareJoinedSession(ctx, a.collaborationController, a.collaborationClient, invitation)
	}
	var snapshot model.Snapshot
	if err == nil {
		snapshot, err = execution.Snapshot(ctx)
		if err != nil {
			go a.leaveCollaborationAfterAttachmentFailure()
		}
	}
	window.RunLater(func() {
		if err == nil {
			err = a.finishJoinIntoNewDocument(a.LoadedEnvironment() == environment, environment, execution, snapshot, nil)
		}
		if err != nil {
			log.Error().Err(err).Msg("Unable to join collaboration into a new document")
			util.ShowErrorDialog("Unable to join collaboration: " + err.Error())
		}
	})
}

// finishJoinIntoNewDocument runs on the UI thread. A session that cannot be
// installed is left so no hidden connection outlives the failed attempt.
func (a *app) finishJoinIntoNewDocument(current bool, environment *dmenv.Dme, execution executor.Executor, snapshot model.Snapshot, host *protocol.RepositoryDescriptor) error {
	err := collabui.ErrAttachmentTargetChanged
	if current {
		err = a.installJoinedDocument(environment, execution, snapshot, host)
	}
	if err != nil {
		go a.leaveCollaborationAfterAttachmentFailure()
		var mismatch *collabui.EnvironmentMismatchError
		if errors.As(err, &mismatch) {
			util.ShowErrorDialog(err.Error())
		}
	}
	return err
}

// installJoinedDocument builds a fresh untitled, session-owned workspace from
// the received snapshot. It reuses the owned map-open installation path, so the
// document is an ordinary workspace whose executor is then the synchronized one.
// Save routes to Save As; the destination is always chosen by the local user.
func (a *app) installJoinedDocument(environment *dmenv.Dme, execution executor.Executor, snapshot model.Snapshot, host *protocol.RepositoryDescriptor) error {
	localHash, err := mapadapter.EnvironmentHash(environment)
	if err != nil {
		return err
	}
	if err := collabui.CheckJoinedEnvironment(snapshot, localHash, filepath.Base(environment.RootFile), host); err != nil {
		return err
	}
	name := collabui.UntitledName(snapshot.DocumentID)
	identity := filepath.Join(environment.RootDir, untitledDirectory, name)
	if a.mappingWorkspace(identity) != nil {
		return fmt.Errorf("this session is already open in %s", name)
	}
	// The save pipeline reads an initial-state file for key allocation; stage
	// one from the snapshot beside the other map backups.
	backup := filepath.Join(a.backupDir, a.environmentName(), name, time.Now().Format(util.TimeFormat)+".dmm")
	if err := mapadapter.ExportTGMFile(snapshot, backup); err != nil {
		return fmt.Errorf("stage the joined map: %w", err)
	}
	document := &dmmap.Dmm{Name: name, Path: dmmap.DmmPath{Readable: name, Absolute: identity}, Backup: backup}
	if err := mapadapter.ApplyWithEnvironment(document, snapshot, environment); err != nil {
		return err
	}
	prepared, err := editor.PrepareOpen(context.Background(), environment, document)
	if err != nil {
		return err
	}
	a.installOpenMap(identity, nil, document, nil, prepared)
	opened := a.mappingWorkspace(identity)
	if opened == nil {
		return fmt.Errorf("the joined map could not be opened")
	}
	opened.MarkUntitled()
	if err := opened.Map().Editor().AttachCollaborationExecutor(execution); err != nil {
		return err
	}
	a.collaborationEditor = opened.Map().Editor()
	return nil
}

// APHELION EDIT ADDITION END
