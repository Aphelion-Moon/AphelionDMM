// APHELION EDIT ADDITION START - COLLABORATION CURSOR COLOR
package app

import (
	"context"

	"github.com/rs/zerolog/log"

	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/app/prefs"
)

// normalizeCollaborationPrefs drops palette indices this build cannot render.
func normalizeCollaborationPrefs(collaboration *prefs.Collaboration) {
	if collaboration.CursorColor != nil && !protocol.ValidCursorColor(*collaboration.CursorColor) {
		collaboration.CursorColor = nil
	}
}

// CollaborationCursorColor returns the saved cursor palette index, or nil for the
// automatic default. The returned value is a copy.
func (a *app) CollaborationCursorColor() *int {
	color := a.preferencesConfig().Collaboration.CursorColor
	if color == nil {
		return nil
	}
	index := *color
	return &index
}

// DoSetCollaborationCursorColor saves the choice locally and, when a session is
// active, shares it as ephemeral presence through a profile update.
func (a *app) DoSetCollaborationCursorColor(index int) {
	if !protocol.ValidCursorColor(index) {
		return
	}
	cfg := a.preferencesConfig()
	cfg.Collaboration.CursorColor = &index
	a.configSaveV(cfg)
	client := a.collaborationClient
	if client == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
		defer cancel()
		if err := client.UpdateCursorColor(ctx, index); err != nil {
			log.Debug().Err(err).Msg("unable to publish collaboration cursor color")
		}
	}()
}

// seedCollaborationCursorColor hands the saved choice to the session client so
// it is published after the next join.
func (a *app) seedCollaborationCursorColor() {
	if a.collaborationClient == nil {
		return
	}
	if color := a.CollaborationCursorColor(); color != nil {
		_ = a.collaborationClient.UpdateCursorColor(context.Background(), *color)
	}
}

// APHELION EDIT ADDITION END
