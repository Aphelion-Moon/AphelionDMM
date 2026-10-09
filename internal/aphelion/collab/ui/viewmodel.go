package ui

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

const maxVisibleConflicts = 20

type SessionStatus struct {
	SessionID        string
	Role             string
	State            client.State
	Revision         model.Revision
	Participants     []protocol.ParticipantPresence
	ConflictPreviews []client.ConflictPreview
	ConflictCount    int
	ConflictPage     int
	InviteReady      bool
	ReconnectReady   bool
	Err              error
	SensitiveValues  []string
}

type ParticipantView struct {
	ActorID model.ActorID
	Label   string
	Status  string
}

type ViewModel struct {
	SessionLabel        string
	RoleLabel           string
	RevisionLabel       string
	SyncLabel           string
	Participants        []ParticipantView
	ConflictSummaries   []string
	Conflicts           []ConflictView
	HiddenConflictCount int
	ConflictCount       int
	ConflictPage        int
	ConflictPageCount   int
	CanEdit             bool
	CanAdminister       bool
	CanCopyInvite       bool
	ShowReconnect       bool
	CanReconnect        bool
	CanLeave            bool
	ErrorText           string
	// DraftCount is the number of retained drafts across all pages.
	DraftCount int
	// RateLimited is set while the session is suspended by a server rate limit.
	RateLimited bool
	// ReconnectGaveUp is set when automatic reconnect stopped and only a manual retry remains.
	ReconnectGaveUp bool
	// DraftsInterrupted is set when drafts remain and the session cannot currently deliver them.
	DraftsInterrupted bool
	StatusBanner      string
}

func BuildViewModel(status SessionStatus) ViewModel {
	participants := make([]ParticipantView, len(status.Participants))
	for index, participant := range status.Participants {
		label := participant.DisplayName
		if label == "" {
			label = string(participant.ActorID)
		}
		participants[index] = ParticipantView{ActorID: participant.ActorID, Label: label, Status: participant.Status}
	}
	sort.Slice(participants, func(left, right int) bool {
		leftNamed := participants[left].Label != string(participants[left].ActorID)
		rightNamed := participants[right].Label != string(participants[right].ActorID)
		if leftNamed != rightNamed {
			return leftNamed
		}
		if participants[left].Label != participants[right].Label {
			return participants[left].Label < participants[right].Label
		}
		return participants[left].ActorID < participants[right].ActorID
	})
	previews := status.ConflictPreviews
	totalConflicts := max(status.ConflictCount, len(previews))
	pageCount := 0
	if totalConflicts != 0 {
		pageCount = (totalConflicts-1)/maxVisibleConflicts + 1
	}
	visibleConflicts := len(previews)
	if visibleConflicts > maxVisibleConflicts {
		visibleConflicts = maxVisibleConflicts
	}
	conflicts := make([]string, visibleConflicts)
	actionableConflicts := make([]ConflictView, visibleConflicts)
	for index := 0; index < visibleConflicts; index++ {
		conflict := previews[index]
		conflicts[index] = redactSensitive(fmt.Sprintf("%s: %s", conflict.Code, conflict.Message), status.SensitiveValues)
		actionableConflicts[index] = buildConflictPreviewView(conflict)
		actionableConflicts[index].Code = redactSensitive(actionableConflicts[index].Code, status.SensitiveValues)
		actionableConflicts[index].Message = redactSensitive(actionableConflicts[index].Message, status.SensitiveValues)
		for _, values := range [][]AuthoritativeTileView{actionableConflicts[index].Values, actionableConflicts[index].DraftBefore, actionableConflicts[index].DraftAfter} {
			for tileIndex := range values {
				for prefabIndex := range values[tileIndex].Prefabs {
					prefab := &values[tileIndex].Prefabs[prefabIndex]
					prefab.Path = redactSensitive(prefab.Path, status.SensitiveValues)
					for variableIndex := range prefab.Variables {
						prefab.Variables[variableIndex].Name = redactSensitive(prefab.Variables[variableIndex].Name, status.SensitiveValues)
						prefab.Variables[variableIndex].Value = redactSensitive(prefab.Variables[variableIndex].Value, status.SensitiveValues)
					}
				}
			}
		}
	}
	role := strings.ToLower(status.Role)
	active := status.State != client.StateDisconnected && status.State != client.StateClosed
	view := ViewModel{
		SessionLabel:        status.SessionID,
		RoleLabel:           roleLabel(role),
		RevisionLabel:       "Revision " + strconv.FormatUint(uint64(status.Revision), 10),
		SyncLabel:           stateLabel(status.State),
		Participants:        participants,
		ConflictSummaries:   conflicts,
		Conflicts:           actionableConflicts,
		HiddenConflictCount: totalConflicts - visibleConflicts,
		ConflictCount:       totalConflicts,
		ConflictPage:        status.ConflictPage,
		ConflictPageCount:   pageCount,
		CanEdit:             role == "owner" || role == "editor",
		CanAdminister:       role == "owner",
		CanCopyInvite:       role == "owner" && status.InviteReady,
		ShowReconnect:       status.State == client.StateReconnecting || (status.State == client.StateDisconnected && status.ReconnectReady),
		CanReconnect:        status.ReconnectReady && (status.State == client.StateDisconnected || status.State == client.StateReconnecting),
		CanLeave:            (active || status.SessionID != "") && status.State != client.StateClosed && totalConflicts == 0,
	}
	view.DraftCount = totalConflicts
	suspended := status.State == client.StateReconnecting || status.State == client.StateDisconnected
	view.RateLimited = suspended && errors.Is(status.Err, client.ErrRateLimited)
	view.ReconnectGaveUp = suspended && status.ReconnectReady
	switch {
	case view.RateLimited:
		view.StatusBanner = "Reconnecting (rate limited by server)"
	case view.ReconnectGaveUp:
		view.StatusBanner = "Reconnect attempts exhausted. Use Retry Reconnect."
	}
	terminal := status.SessionID != "" && (status.State == client.StateClosed || status.State == client.StateDisconnected)
	view.DraftsInterrupted = totalConflicts != 0 && (view.RateLimited || view.ReconnectGaveUp || terminal)
	if status.Err != nil {
		view.ErrorText = redactSensitive(status.Err.Error(), status.SensitiveValues)
	}
	return view
}

func stateLabel(state client.State) string {
	switch state {
	case client.StateDisconnected:
		return "Disconnected"
	case client.StateConnecting:
		return "Connecting"
	case client.StateSynchronizing:
		return "Synchronizing"
	case client.StateCaughtUp:
		return "Caught up"
	case client.StateReconnecting:
		return "Reconnecting"
	case client.StateReadOnly:
		return "Read only"
	case client.StateConflict:
		return "Conflict"
	case client.StateClosed:
		return "Closed"
	default:
		return "Unknown"
	}
}

func roleLabel(role string) string {
	switch role {
	case "owner":
		return "Owner"
	case "editor":
		return "Editor"
	case "viewer":
		return "Viewer"
	default:
		return "Unknown"
	}
}

func redactSensitive(value string, sensitive []string) string {
	for _, secret := range sensitive {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return value
}
