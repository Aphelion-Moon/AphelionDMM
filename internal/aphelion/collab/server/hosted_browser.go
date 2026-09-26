package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"sdmm/internal/aphelion/collab/protocol"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func (service *Service) handleHostedCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	_, supported := service.config.HostedRegistry.(collabstore.HostedSessionBrowserStore)
	provider := service.config.HostedProvider
	if provider == "" {
		provider = "oidc"
	}
	writeJSON(w, 200, protocol.HostedCapabilities{Provider: provider, SessionBrowser: supported && service.config.HostedAuth != nil})
}

func (service *Service) handleListHostedSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity, ok := service.authorizeHostedIdentity(w, r)
	if !ok {
		return
	}
	browser, ok := service.config.HostedRegistry.(collabstore.HostedSessionBrowserStore)
	if !ok {
		writeError(w, 501, "unsupported", "Session browsing is unavailable.")
		return
	}
	if !service.joinLimiter.Allow("browser:"+string(identity.ActorID), service.config.Now()) {
		writeError(w, 429, "rate_limited", "Please wait before refreshing.")
		return
	}
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			writeError(w, 400, "invalid_request", "Page limit must be 1 through 100.")
			return
		}
	}
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 128 {
		writeError(w, 400, "invalid_request", "Invalid page cursor.")
		return
	}
	pageRequest := collabstore.HostedSessionPageRequest{Limit: limit, Cursor: cursor}
	counts := service.hostedActivity()
	var page collabstore.HostedSessionPage
	var err error
	switch r.URL.Query().Get("scope") {
	case "community":
		ids := make([]string, 0, len(counts))
		for id, n := range counts {
			if n > 0 && service.hostedDocumentAvailable(id) {
				ids = append(ids, id)
			}
		}
		page, err = browser.ListCommunityHostedSessions(r.Context(), identity.ActorID, ids, pageRequest)
	case "mine":
		page, err = browser.ListMyHostedSessions(r.Context(), identity.ActorID, pageRequest)
	default:
		writeError(w, 400, "invalid_request", "Choose community or mine.")
		return
	}
	if err != nil {
		writeError(w, 503, "unavailable", "Sessions could not be listed.")
		return
	}
	result := protocol.HostedSessionsPage{Sessions: make([]protocol.HostedSessionSummary, 0, len(page.Sessions)), NextCursor: page.NextCursor}
	for _, session := range page.Sessions {
		result.Sessions = append(result.Sessions, protocol.HostedSessionSummary{SessionID: session.SessionID, HostedSessionMetadata: protocol.HostedSessionMetadata{Visibility: string(session.Visibility), Title: session.Title, MapLabel: session.MapLabel, EnvironmentLabel: session.EnvironmentLabel}, OwnerDisplayName: session.OwnerDisplayName, Participants: counts[session.SessionID], Available: service.hostedDocumentAvailable(session.SessionID)})
	}
	writeJSON(w, 200, result)
}

func (service *Service) handleJoinHostedSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity, ok := service.authorizeHostedIdentity(w, r)
	if !ok {
		return
	}
	browser, ok := service.config.HostedRegistry.(collabstore.HostedSessionBrowserStore)
	if !ok {
		writeError(w, 501, "unsupported", "Session browsing is unavailable.")
		return
	}
	if !service.joinLimiter.Allow("hosted-join:"+string(identity.ActorID), service.config.Now()) {
		writeError(w, 429, "rate_limited", "Please wait before joining.")
		return
	}
	id := r.PathValue("session_id")
	if id == "" || len(id) > 128 {
		writeError(w, 404, "unavailable", "Session is unavailable.")
		return
	}
	existing, found, err := service.config.HostedRegistry.ResolveHostedMember(r.Context(), id, identity.Issuer, identity.Subject)
	if err != nil || (found && (existing.Disabled || existing.ActorID != identity.ActorID)) {
		writeError(w, 404, "unavailable", "Session is unavailable.")
		return
	}
	if !service.hostedDocumentAvailable(id) || (!found && service.hostedParticipantCount(id) == 0) {
		writeError(w, 404, "unavailable", "Session is unavailable. Refresh the browser.")
		return
	}
	member, _, err := browser.JoinHostedSession(r.Context(), id, collabstore.HostedIdentity{Issuer: identity.Issuer, Subject: identity.Subject, ActorID: identity.ActorID, DisplayName: identity.DisplayName})
	if err != nil {
		writeError(w, 404, "unavailable", "Session is unavailable. Refresh the browser.")
		return
	}
	if err := service.registerHostedMember(member); err != nil {
		writeError(w, 503, "unavailable", "Membership is saved; retry to connect.")
		return
	}
	writeJSON(w, 200, map[string]any{"session_id": id, "actor_id": member.ActorID, "role": member.Role})
}

func (service *Service) handleUpdateHostedSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity, ok := service.authorizeHostedIdentity(w, r)
	if !ok {
		return
	}
	browser, ok := service.config.HostedRegistry.(collabstore.HostedSessionBrowserStore)
	if !ok {
		writeError(w, 501, "unsupported", "Session settings are unavailable.")
		return
	}
	if !service.joinLimiter.Allow("hosted-settings:"+string(identity.ActorID), service.config.Now()) {
		writeError(w, 429, "rate_limited", "Please wait before changing settings.")
		return
	}
	var metadata collabstore.HostedSessionMetadata
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		writeHostedDecodeError(w, err, "Invalid session metadata.")
		return
	}
	normalized, err := collabstore.NormalizeHostedSessionMetadata(metadata)
	if err != nil {
		writeHostedDecodeError(w, err, "Invalid session metadata.")
		return
	}
	if _, err := browser.UpdateHostedSessionMetadata(r.Context(), r.PathValue("session_id"), identity.ActorID, normalized); err != nil {
		writeError(w, 400, "invalid_request", "Session settings are invalid or unavailable to this account.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (service *Service) registerHostedMember(member collabstore.HostedMember) error {
	principal, err := principalFromHostedMember(member)
	if err != nil {
		return err
	}
	return service.hub.Join(member.SessionID, principal)
}
