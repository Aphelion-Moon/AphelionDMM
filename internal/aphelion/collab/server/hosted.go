package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
)

const desktopAuthHandoffTTL = 5 * time.Minute

type HostedSessionResponse struct {
	SessionID  string           `json:"session_id"`
	DocumentID model.DocumentID `json:"document_id"`
	Revision   model.Revision   `json:"revision"`
	MapHash    string           `json:"map_hash"`
}

func (service *Service) handleHostedAuthBegin(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if service.config.HostedLogin == nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "hosted authentication is unavailable")
		return
	}
	if !service.joinLimiter.Allow("auth-begin:"+remoteIP(request), service.config.Now()) {
		writeError(writer, http.StatusTooManyRequests, "rate_limited", "hosted authentication rate limit exceeded")
		return
	}
	result, err := service.config.HostedLogin.Begin(request.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "hosted authentication could not start")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"authorization_url": result.AuthorizationURL})
}

func (service *Service) handleHostedDesktopAuthBegin(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if service.config.HostedLogin == nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "hosted authentication is unavailable")
		return
	}
	if !service.joinLimiter.Allow("desktop-auth-begin:"+remoteIP(request), service.config.Now()) {
		writeError(writer, http.StatusTooManyRequests, "rate_limited", "hosted authentication rate limit exceeded")
		return
	}
	service.mutex.Lock()
	service.cleanupDesktopAuthHandoffsLocked(service.config.Now())
	atCapacity := len(service.desktopHandoffs) >= service.limits.RateEntries
	service.mutex.Unlock()
	if atCapacity {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "desktop authentication capacity is exhausted")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, service.limits.MaxHTTPBodyBytes)
	var body struct {
		VerifierChallenge string `json:"verifier_challenge"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeHostedDecodeError(writer, err, "invalid desktop authentication request")
		return
	}
	challenge, err := base64.RawURLEncoding.DecodeString(body.VerifierChallenge)
	if err != nil || len(challenge) != sha256.Size {
		writeError(writer, http.StatusBadRequest, "invalid_request", "desktop verifier challenge is invalid")
		return
	}
	result, err := service.config.HostedLogin.Begin(request.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "hosted authentication could not start")
		return
	}
	handoffID, err := randomToken()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal", "desktop authentication handoff could not start")
		return
	}
	handoffHash := sha256.Sum256([]byte(handoffID))
	stateHash := sha256.Sum256([]byte(result.State))
	var challengeHash [sha256.Size]byte
	copy(challengeHash[:], challenge)
	service.mutex.Lock()
	service.cleanupDesktopAuthHandoffsLocked(service.config.Now())
	service.desktopHandoffs[handoffHash] = desktopAuthHandoff{challenge: challengeHash, stateHash: stateHash, expiresAt: service.config.Now().Add(desktopAuthHandoffTTL)}
	service.desktopAuthStates[stateHash] = handoffHash
	service.mutex.Unlock()
	writeJSON(writer, http.StatusOK, map[string]string{"authorization_url": result.AuthorizationURL, "handoff_id": handoffID})
}

func (service *Service) handleHostedAuthComplete(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if service.config.HostedLogin == nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "hosted authentication is unavailable")
		return
	}
	state := request.URL.Query().Get("state")
	code := request.URL.Query().Get("code")
	if len(state) > 512 || len(code) > 4096 {
		writeError(writer, http.StatusBadRequest, "invalid_request", "OIDC callback is invalid")
		return
	}
	stateHash := sha256.Sum256([]byte(state))
	service.mutex.Lock()
	handoffHash, desktop := service.desktopAuthStates[stateHash]
	handoff, handoffExists := service.desktopHandoffs[handoffHash]
	desktopExpired := desktop && (!handoffExists || !service.config.Now().Before(handoff.expiresAt))
	if desktopExpired {
		delete(service.desktopAuthStates, stateHash)
		delete(service.desktopHandoffs, handoffHash)
	}
	service.mutex.Unlock()
	if desktopExpired {
		writeError(writer, http.StatusUnauthorized, "unauthorized", "desktop authentication handoff is invalid or expired")
		return
	}
	session, err := service.config.HostedLogin.Complete(request.Context(), state, code)
	if err != nil {
		if desktop {
			service.mutex.Lock()
			delete(service.desktopAuthStates, stateHash)
			delete(service.desktopHandoffs, handoffHash)
			service.mutex.Unlock()
		}
		writeError(writer, http.StatusUnauthorized, "unauthorized", "OIDC callback was rejected")
		return
	}
	service.mutex.Lock()
	if desktop {
		delete(service.desktopAuthStates, stateHash)
		handoff, exists := service.desktopHandoffs[handoffHash]
		if exists && service.config.Now().Before(handoff.expiresAt) {
			completed := session
			handoff.session = &completed
			service.desktopHandoffs[handoffHash] = handoff
			service.mutex.Unlock()
			writeJSON(writer, http.StatusOK, map[string]string{"status": "complete"})
			return
		}
	}
	service.mutex.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{"token": session.Token, "actor_id": session.ActorID, "display_name": session.DisplayName, "expires_at": session.ExpiresAt})
}

func (service *Service) handleHostedDesktopAuthExchange(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	request.Body = http.MaxBytesReader(writer, request.Body, service.limits.MaxHTTPBodyBytes)
	var body struct {
		HandoffID string `json:"handoff_id"`
		Verifier  string `json:"verifier"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.HandoffID == "" || body.Verifier == "" || len(body.HandoffID) > 128 || len(body.Verifier) > 256 {
		writeHostedDecodeError(writer, err, "invalid desktop authentication exchange")
		return
	}
	handoffHash := sha256.Sum256([]byte(body.HandoffID))
	verifierHash := sha256.Sum256([]byte(body.Verifier))
	service.mutex.Lock()
	service.cleanupDesktopAuthHandoffsLocked(service.config.Now())
	handoff, exists := service.desktopHandoffs[handoffHash]
	if !exists || subtle.ConstantTimeCompare(verifierHash[:], handoff.challenge[:]) != 1 {
		service.mutex.Unlock()
		writeError(writer, http.StatusUnauthorized, "unauthorized", "desktop authentication handoff is invalid or expired")
		return
	}
	if handoff.session == nil {
		service.mutex.Unlock()
		writeJSON(writer, http.StatusAccepted, map[string]string{"status": "pending"})
		return
	}
	session := *handoff.session
	delete(service.desktopHandoffs, handoffHash)
	service.mutex.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{"token": session.Token, "actor_id": session.ActorID, "display_name": session.DisplayName, "expires_at": session.ExpiresAt})
}

func (service *Service) cleanupDesktopAuthHandoffsLocked(now time.Time) {
	for handoffHash, handoff := range service.desktopHandoffs {
		if !now.Before(handoff.expiresAt) {
			delete(service.desktopHandoffs, handoffHash)
			delete(service.desktopAuthStates, handoff.stateHash)
		}
	}
}

func (service *Service) handleHostedAuthLogout(writer http.ResponseWriter, request *http.Request) {
	if service.config.HostedLogin == nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "hosted authentication is unavailable")
		return
	}
	service.config.HostedLogin.Logout(bearerToken(request))
	writer.WriteHeader(http.StatusNoContent)
}

func (service *Service) handleCreateHostedSession(writer http.ResponseWriter, request *http.Request) {
	identity, ok := service.authorizeHostedIdentity(writer, request)
	if !ok {
		return
	}
	if service.config.HostedRegistry == nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "hosted collaboration is unavailable")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, service.limits.MaxSnapshotBodyBytes)
	var body struct {
		Snapshot  model.Snapshot `json:"snapshot"`
		BulkEdits bool           `json:"bulk_edits,omitempty"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeHostedDecodeError(writer, err, "invalid hosted session request")
		return
	}
	if body.Snapshot.DocumentID == "" {
		writeError(writer, http.StatusBadRequest, "invalid_snapshot", "snapshot document id is required")
		return
	}
	sessionID := string(body.Snapshot.DocumentID)
	ownerMember := hostedMember(sessionID, identity, collabstore.HostedRoleOwner)
	principal, err := principalFromHostedMember(ownerMember)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal", "identity initialization failed")
		return
	}
	documentConfig := service.documentConfig
	documentConfig.BulkEdits = body.BulkEdits || documentConfig.BulkEdits
	owner, err := startOrRecoverDocument(service.context, body.Snapshot, service.store, documentConfig)
	if err != nil {
		if writeTransactionUpgradeError(writer, err) {
			return
		}
		var recoveryError *RecoveryError
		if errors.As(err, &recoveryError) {
			service.setDocumentRecoveryError(recoveryError.DocumentID, recoveryError)
		}
		writeError(writer, http.StatusBadRequest, "invalid_snapshot", "snapshot is not valid")
		return
	}
	current, err := owner.Snapshot(request.Context())
	if err != nil {
		_ = owner.Close(request.Context())
		writeError(writer, http.StatusInternalServerError, "internal", "session recovery failed")
		return
	}
	mapHash, err := current.Hash()
	if err != nil {
		_ = owner.Close(request.Context())
		writeError(writer, http.StatusInternalServerError, "internal", "session recovery hash failed")
		return
	}
	created := collabstore.HostedSession{SessionID: sessionID, DocumentID: current.DocumentID, CreatedAt: service.config.Now()}
	if err := service.config.HostedRegistry.CreateHostedSession(request.Context(), created, ownerMember); err != nil {
		_ = owner.Close(request.Context())
		if errors.Is(err, collabstore.ErrHostedSessionExists) {
			writeError(writer, http.StatusConflict, "session_exists", "hosted session already exists")
			return
		}
		writeError(writer, http.StatusInternalServerError, "internal", "hosted session registration failed")
		return
	}
	if err := service.hub.Create(sessionID, owner, principal); err != nil {
		_ = owner.Close(request.Context())
		writeError(writer, http.StatusInternalServerError, "internal", "session registration failed")
		return
	}
	service.mutex.Lock()
	service.sessions[sessionID] = sessionRecord{owner: owner}
	service.mutex.Unlock()
	service.setDocumentRecoveryError(body.Snapshot.DocumentID, nil)
	writeJSON(writer, http.StatusCreated, HostedSessionResponse{SessionID: sessionID, DocumentID: current.DocumentID, Revision: current.Revision, MapHash: mapHash})
}

func (service *Service) handleCreateHostedInvitation(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if service.config.HostedRegistry == nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "hosted collaboration is unavailable")
		return
	}
	sessionID := request.PathValue("session_id")
	authorized, _, ok := service.authorizeRecord(request, sessionID)
	if !ok || !authorized.hosted || !authorized.principal.CanAdminister() {
		writeError(writer, http.StatusForbidden, "forbidden", "hosted owner role is required")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, service.limits.MaxHTTPBodyBytes)
	var body struct {
		Role collabstore.HostedRole `json:"role"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeHostedDecodeError(writer, err, "invalid hosted invitation request")
		return
	}
	if body.Role != collabstore.HostedRoleViewer && body.Role != collabstore.HostedRoleEditor {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invitation role must be viewer or editor")
		return
	}
	token, err := randomToken()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal", "invitation generation failed")
		return
	}
	expiresAt := service.config.Now().Add(service.config.JoinTokenTTL)
	invitation := collabstore.HostedInvitation{
		TokenHash: sha256.Sum256([]byte(token)), SessionID: sessionID, Role: body.Role,
		CreatedByActorID: authorized.principal.ActorID(), ExpiresAt: expiresAt,
	}
	if err := service.config.HostedRegistry.CreateHostedInvitation(request.Context(), invitation); err != nil {
		writeError(writer, http.StatusInternalServerError, "internal", "invitation registration failed")
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"token": token, "role": body.Role, "expires_at": expiresAt})
}

func (service *Service) handleRedeemHostedInvitation(writer http.ResponseWriter, request *http.Request) {
	identity, ok := service.authorizeHostedIdentity(writer, request)
	if !ok {
		return
	}
	if service.config.HostedRegistry == nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "hosted collaboration is unavailable")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, service.limits.MaxHTTPBodyBytes)
	var body struct {
		Token string `json:"token"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.Token == "" {
		writeHostedDecodeError(writer, err, "invalid hosted invitation redemption")
		return
	}
	member, err := service.config.HostedRegistry.RedeemHostedInvitation(
		request.Context(), request.PathValue("session_id"), sha256.Sum256([]byte(body.Token)),
		collabstore.HostedIdentity{Issuer: identity.Issuer, Subject: identity.Subject, ActorID: identity.ActorID, DisplayName: identity.DisplayName},
		service.config.Now(),
	)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, "unauthorized", "hosted invitation is invalid, expired, or redeemed")
		return
	}
	principal, err := principalFromHostedMember(member)
	if err != nil || service.hub.Join(member.SessionID, principal) != nil {
		writeError(writer, http.StatusInternalServerError, "internal", "hosted membership initialization failed")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"session_id": member.SessionID, "actor_id": member.ActorID, "role": member.Role})
}

func (service *Service) authorizeHostedIdentity(writer http.ResponseWriter, request *http.Request) (auth.Session, bool) {
	if service.config.HostedAuth == nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "hosted authentication is unavailable")
		return auth.Session{}, false
	}
	identity, err := service.config.HostedAuth.Authorize(request.Context(), bearerToken(request))
	if err != nil || !service.config.Now().Before(identity.ExpiresAt) {
		writeError(writer, http.StatusUnauthorized, "unauthorized", "hosted authentication session is invalid")
		return auth.Session{}, false
	}
	return identity, true
}

func writeHostedDecodeError(writer http.ResponseWriter, err error, message string) {
	status := http.StatusBadRequest
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		status = http.StatusRequestEntityTooLarge
	}
	writeError(writer, status, "invalid_request", message)
}

func hostedMember(sessionID string, identity auth.Session, role collabstore.HostedRole) collabstore.HostedMember {
	return collabstore.HostedMember{SessionID: sessionID, Issuer: identity.Issuer, Subject: identity.Subject, ActorID: identity.ActorID, DisplayName: identity.DisplayName, Role: role}
}

func principalFromHostedMember(member collabstore.HostedMember) (Principal, error) {
	identityHash := sha256.Sum256([]byte(member.Issuer + "\x00" + member.Subject))
	return NewPrincipal(fmt.Sprintf("oidc-%x", identityHash), member.ActorID, member.DisplayName, Role(member.Role))
}

func (service *Service) RecoverHostedSessions(ctx context.Context) error {
	if service.config.HostedRegistry == nil {
		return fmt.Errorf("hosted collaboration registry is unavailable")
	}
	sessions, err := service.config.HostedRegistry.ListHostedSessions(ctx)
	if err != nil {
		return fmt.Errorf("list hosted sessions: %w", err)
	}
	for _, hostedSession := range sessions {
		if err := service.recoverHostedSession(ctx, hostedSession); err != nil {
			service.setDocumentRecoveryError(hostedSession.DocumentID, err)
			return err
		}
	}
	return nil
}

func (service *Service) recoverHostedSession(ctx context.Context, hostedSession collabstore.HostedSession) error {
	members, err := service.config.HostedRegistry.ListHostedMembers(ctx, hostedSession.SessionID)
	if err != nil {
		return fmt.Errorf("list members for hosted session %q: %w", hostedSession.SessionID, err)
	}
	var ownerMember *collabstore.HostedMember
	for index := range members {
		if !members[index].Disabled && members[index].Role == collabstore.HostedRoleOwner {
			if ownerMember != nil {
				return fmt.Errorf("hosted session %q has multiple enabled owners", hostedSession.SessionID)
			}
			ownerMember = &members[index]
		}
	}
	if ownerMember == nil {
		return fmt.Errorf("hosted session %q has no enabled owner", hostedSession.SessionID)
	}
	owner, err := RecoverDocumentWithConfig(ctx, hostedSession.DocumentID, service.store, service.documentConfig)
	if err != nil {
		return &RecoveryError{DocumentID: hostedSession.DocumentID, Err: err}
	}
	ownerPrincipal, err := principalFromHostedMember(*ownerMember)
	if err != nil {
		_ = owner.Close(ctx)
		return err
	}
	if err := service.hub.Create(hostedSession.SessionID, owner, ownerPrincipal); err != nil {
		_ = owner.Close(ctx)
		return fmt.Errorf("register hosted session %q: %w", hostedSession.SessionID, err)
	}
	for _, member := range members {
		if member.Disabled || member.ActorID == ownerMember.ActorID {
			continue
		}
		principal, err := principalFromHostedMember(member)
		if err != nil {
			_ = owner.Close(ctx)
			return err
		}
		if err := service.hub.Join(hostedSession.SessionID, principal); err != nil {
			_ = owner.Close(ctx)
			return fmt.Errorf("join hosted session member: %w", err)
		}
	}
	service.mutex.Lock()
	service.sessions[hostedSession.SessionID] = sessionRecord{owner: owner}
	service.mutex.Unlock()
	service.setDocumentRecoveryError(hostedSession.DocumentID, nil)
	return nil
}
