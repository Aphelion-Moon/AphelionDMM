package server

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Browser callbacks never return application credentials. The legacy begin route
// accepts the same verifier-protected handoff as the desktop route.
func (service *Service) handleHostedAuthBegin(writer http.ResponseWriter, request *http.Request) {
	service.handleHostedDesktopAuthBegin(writer, request)
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
	startToken, err := randomToken()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal", "browser authentication could not start")
		return
	}
	stateHash := sha256.Sum256([]byte(result.State))
	var challengeHash [sha256.Size]byte
	copy(challengeHash[:], challenge)
	service.mutex.Lock()
	service.cleanupDesktopAuthHandoffsLocked(service.config.Now())
	service.desktopHandoffs[handoffHash] = desktopAuthHandoff{challenge: challengeHash, stateHash: stateHash, startHash: sha256.Sum256([]byte(startToken)), authorizationURL: result.AuthorizationURL, expiresAt: service.config.Now().Add(desktopAuthHandoffTTL)}
	service.desktopAuthStates[stateHash] = handoffHash
	service.mutex.Unlock()
	writeJSON(writer, http.StatusOK, map[string]string{"authorization_url": service.config.HostedPublicOrigin + "/v1/auth/browser/start?handoff=" + handoffID + "&start=" + startToken, "handoff_id": handoffID})
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
	if handoff.terminalError != "" {
		delete(service.desktopHandoffs, handoffHash)
		service.mutex.Unlock()
		writeError(writer, http.StatusUnauthorized, handoff.terminalError, "Browser sign-in failed. Please try signing in again.")
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
	snapshotTransferDeadlines(writer)
	identity, ok := service.authorizeHostedIdentity(writer, request)
	if !ok {
		return
	}
	if service.config.HostedRegistry == nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "hosted collaboration is unavailable")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, service.limits.MaxSnapshotBodyBytes)
	switch request.Header.Get("Content-Encoding") {
	case "", "identity":
	case "gzip":
		compressed, err := gzip.NewReader(request.Body)
		if err != nil {
			writeHostedDecodeError(writer, err, "invalid compressed snapshot")
			return
		}
		defer func() { _ = compressed.Close() }()
		// Enforce the same limit on expanded data, including compression bombs.
		request.Body = http.MaxBytesReader(writer, compressed, service.limits.MaxSnapshotBodyBytes)
	default:
		writeError(writer, http.StatusUnsupportedMediaType, "unsupported_encoding", "snapshot content encoding is unsupported")
		return
	}
	var body struct {
		Snapshot  model.Snapshot `json:"snapshot"`
		BulkEdits bool           `json:"bulk_edits,omitempty"`
		collabstore.HostedSessionMetadata
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeHostedDecodeError(writer, err, "invalid hosted session request")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeHostedDecodeError(writer, err, "invalid trailing snapshot data")
		return
	}
	if body.Snapshot.DocumentID == "" {
		writeError(writer, http.StatusBadRequest, "invalid_snapshot", "snapshot document id is required")
		return
	}
	metadata, err := collabstore.NormalizeHostedSessionMetadata(body.HostedSessionMetadata)
	if err != nil {
		writeHostedDecodeError(writer, err, "Invalid session metadata.")
		return
	}
	body.HostedSessionMetadata = metadata
	sessionID := string(body.Snapshot.DocumentID)
	ownerMember := hostedMember(sessionID, identity, collabstore.HostedRoleOwner)
	principal, err := principalFromHostedMember(ownerMember)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal", "identity initialization failed")
		return
	}
	documentConfig := service.documentConfig
	documentConfig.BulkEdits = body.BulkEdits || documentConfig.BulkEdits
	// A supplied baseline is not authority to adopt retained history. Existing
	// hosted documents are recovered only through their persisted membership.
	owner, err := StartDocumentWithConfig(service.context, body.Snapshot, service.store, documentConfig)
	if err != nil {
		if errors.Is(err, ErrSessionExists) {
			writeError(writer, http.StatusConflict, "session_exists", "document identity is already stored")
			return
		}
		if writeTransactionUpgradeError(writer, err) {
			return
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
	created := collabstore.HostedSession{SessionID: sessionID, DocumentID: current.DocumentID, CreatedAt: service.config.Now(), Visibility: body.Visibility, Title: body.Title, MapLabel: body.MapLabel, EnvironmentLabel: body.EnvironmentLabel}
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
	service.sessions[sessionID] = sessionRecord{owner: owner, hosted: true, emptyDeadline: service.config.Now().Add(hostedInitialJoinGrace)}
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
	if member.Disabled || service.registerHostedMember(member) != nil {
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
	if member.Disabled {
		return Principal{}, collabstore.ErrHostedMemberDisabled
	}
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
	service.sessions[hostedSession.SessionID] = sessionRecord{owner: owner, hosted: true, emptyDeadline: service.config.Now().Add(hostedInitialJoinGrace)}
	service.mutex.Unlock()
	service.setDocumentRecoveryError(hostedSession.DocumentID, nil)
	return nil
}
