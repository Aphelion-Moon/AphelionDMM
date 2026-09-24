package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/bulktransport"
	"sdmm/internal/aphelion/collab/compat"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	collabstore "sdmm/internal/aphelion/collab/store"
	collabtelemetry "sdmm/internal/aphelion/collab/telemetry"
)

const (
	MaxHTTPBodyBytes     = 2 << 20
	MaxSnapshotBodyBytes = 256 << 20
	WebSocketSubprotocol = "apheliondmm.collaboration.v1"
)

type ServiceConfig struct {
	BulkSpoolBytes                int64
	BulkWorkingBytes              int64
	BulkSpoolDirectory            string
	Store                         SessionStore
	Document                      DocumentConfig
	Limits                        Limits
	Telemetry                     *collabtelemetry.Telemetry
	AllowedOrigins                []string
	Build                         string
	Revision                      string
	OnWebSocketError              func(error)
	LaunchTokenTTL                time.Duration
	JoinTokenTTL                  time.Duration
	ResumptionTokenTTL            time.Duration
	PresenceTimeout               time.Duration
	PresenceInterval              time.Duration
	Now                           func() time.Time
	Compatibility                 compat.Matrix
	HostedAuth                    HostedSessionAuthorizer
	HostedLogin                   HostedLoginManager
	HostedRegistry                collabstore.HostedRegistry
	HostedReauthorizationInterval time.Duration
}

type HostedSessionAuthorizer interface {
	Authorize(context.Context, string) (auth.Session, error)
	AuthorizeSession(context.Context, string, string) (auth.Session, error)
}

type HostedLoginManager interface {
	Begin(context.Context) (auth.BeginResult, error)
	Complete(context.Context, string, string) (auth.Session, error)
	Logout(string)
}

type CreateSessionResponse struct {
	SessionID           string           `json:"session_id"`
	DocumentID          model.DocumentID `json:"document_id"`
	Revision            model.Revision   `json:"revision"`
	MapHash             string           `json:"map_hash"`
	OwnerToken          string           `json:"owner_token"`
	OwnerTokenExpiresAt time.Time        `json:"owner_token_expires_at"`
}

type tokenRecord struct {
	launch             bool
	resumption         bool
	webSocketRedeemed  bool
	sessionID          string
	principal          Principal
	expiresAt          time.Time
	expectedDocumentID model.DocumentID
	expectedMapHash    string
	hosted             bool
	hostedCredential   string
}

type sessionRecord struct {
	owner *DocumentOwner
}

type desktopAuthHandoff struct {
	challenge [sha256.Size]byte
	stateHash [sha256.Size]byte
	expiresAt time.Time
	session   *auth.Session
}

type Service struct {
	bulk              *bulktransport.Codec
	config            ServiceConfig
	context           context.Context
	cancel            context.CancelFunc
	hub               *Hub
	store             SessionStore
	documentConfig    DocumentConfig
	limits            Limits
	mutex             sync.RWMutex
	tokens            map[string]tokenRecord
	sessions          map[string]sessionRecord
	recoveryErrors    map[model.DocumentID]error
	desktopHandoffs   map[[sha256.Size]byte]desktopAuthHandoff
	desktopAuthStates map[[sha256.Size]byte][sha256.Size]byte
	activeConnections int
	joinLimiter       *rateLimiter
	durableLimiter    *rateLimiter
	presenceLimiter   *rateLimiter
	telemetry         *collabtelemetry.Telemetry
	compatibility     compat.Matrix
	server            *http.ServeMux
}

func NewService(config ServiceConfig) *Service {
	if config.BulkSpoolBytes <= 0 {
		config.BulkSpoolBytes = bulktransport.DefaultSpoolBytes
	}
	if config.BulkWorkingBytes <= 0 {
		config.BulkWorkingBytes = bulktransport.DefaultWorkingBytes
	}
	if config.LaunchTokenTTL <= 0 {
		config.LaunchTokenTTL = 2 * time.Minute
	}
	if config.JoinTokenTTL <= 0 {
		config.JoinTokenTTL = 15 * time.Minute
	}
	if config.ResumptionTokenTTL <= 0 {
		config.ResumptionTokenTTL = 8 * time.Hour
	}
	if config.PresenceTimeout <= 0 {
		config.PresenceTimeout = time.Minute
	}
	minimumPresenceInterval := time.Duration(protocol.MinPresenceIntervalMS) * time.Millisecond
	maximumPresenceInterval := time.Duration(protocol.MaxPresenceIntervalMS) * time.Millisecond
	if config.PresenceInterval < minimumPresenceInterval || config.PresenceInterval > maximumPresenceInterval {
		config.PresenceInterval = 100 * time.Millisecond
	}
	if config.HostedReauthorizationInterval <= 0 {
		config.HostedReauthorizationInterval = 30 * time.Second
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if len(config.Compatibility.Releases) == 0 {
		config.Compatibility = compat.DefaultMatrix()
	}
	serviceContext, cancel := context.WithCancel(context.Background())
	store := config.Store
	if store == nil {
		store = NewMemoryStore()
	}
	limits := config.Limits.withDefaults()
	if config.Document.Telemetry == nil {
		config.Document.Telemetry = config.Telemetry
	}
	service := &Service{
		bulk:              bulktransport.NewWithWorkingBudget(config.BulkSpoolDirectory, config.BulkSpoolBytes, config.BulkWorkingBytes),
		config:            config,
		context:           serviceContext,
		cancel:            cancel,
		hub:               NewHubWithTelemetry(config.PresenceTimeout, config.Telemetry),
		store:             store,
		documentConfig:    config.Document,
		limits:            limits,
		joinLimiter:       newRateLimiter(limits.JoinRate, limits.RateEntries),
		durableLimiter:    newRateLimiter(limits.DurableRate, limits.RateEntries),
		presenceLimiter:   newRateLimiter(limits.PresenceRate, limits.RateEntries),
		telemetry:         config.Telemetry,
		compatibility:     config.Compatibility,
		tokens:            make(map[string]tokenRecord),
		sessions:          make(map[string]sessionRecord),
		recoveryErrors:    make(map[model.DocumentID]error),
		desktopHandoffs:   make(map[[sha256.Size]byte]desktopAuthHandoff),
		desktopAuthStates: make(map[[sha256.Size]byte][sha256.Size]byte),
		server:            http.NewServeMux(),
	}
	service.routes()
	go service.expirePresence(config.PresenceTimeout / 2)
	return service
}

func (service *Service) expirePresence(interval time.Duration) {
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-service.context.Done():
			return
		case now := <-ticker.C:
			service.hub.ExpirePresence(now)
		}
	}
}

func (service *Service) Handler() http.Handler {
	return service.server
}

func (service *Service) Shutdown(ctx context.Context) error {
	service.cancel()
	service.mutex.RLock()
	owners := make([]*DocumentOwner, 0, len(service.sessions))
	for _, session := range service.sessions {
		owners = append(owners, session.owner)
	}
	service.mutex.RUnlock()
	for _, owner := range owners {
		if err := owner.Close(ctx); err != nil {
			return err
		}
	}
	return service.store.Close()
}

func (service *Service) NewLaunchToken() (string, error) {
	return service.newLaunchToken(model.Snapshot{})
}

func (service *Service) NewLaunchTokenForSnapshot(snapshot model.Snapshot) (string, error) {
	if _, err := snapshot.Hash(); err != nil {
		return "", err
	}
	return service.newLaunchToken(snapshot)
}

func (service *Service) newLaunchToken(snapshot model.Snapshot) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	service.mutex.Lock()
	record := tokenRecord{launch: true, expiresAt: service.config.Now().Add(service.config.LaunchTokenTTL)}
	if snapshot.DocumentID != "" {
		record.expectedDocumentID = snapshot.DocumentID
		record.expectedMapHash, err = snapshot.Hash()
		if err != nil {
			return "", err
		}
	}
	service.tokens[token] = record
	service.mutex.Unlock()
	return token, nil
}

func ValidateListenAddress(address string, allowNetwork bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse listen address: %w", err)
	}
	if allowNetwork || strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("listen address %q is not loopback", address)
	}
	return nil
}

func (service *Service) routes() {
	service.server.HandleFunc("GET /v1/health/live", service.handleLive)
	service.server.HandleFunc("GET /v1/health/ready", service.handleReady)
	service.server.HandleFunc("GET /v1/version", service.handleVersion)
	service.server.HandleFunc("POST /v1/auth/begin", service.handleHostedAuthBegin)
	service.server.HandleFunc("POST /v1/auth/desktop/begin", service.handleHostedDesktopAuthBegin)
	service.server.HandleFunc("POST /v1/auth/desktop/exchange", service.handleHostedDesktopAuthExchange)
	service.server.HandleFunc("GET /v1/auth/complete", service.handleHostedAuthComplete)
	service.server.HandleFunc("POST /v1/auth/logout", service.handleHostedAuthLogout)
	service.server.HandleFunc("POST /v1/sessions", service.handleCreateSession)
	service.server.HandleFunc("POST /v1/hosted/sessions", service.handleCreateHostedSession)
	service.server.HandleFunc("GET /v1/sessions/{session_id}", service.handleGetSession)
	service.server.HandleFunc("GET /v1/sessions/{session_id}/snapshot", service.handleGetSnapshot)
	service.server.HandleFunc("POST /v1/sessions/{session_id}/join-tokens", service.handleCreateJoinToken)
	service.server.HandleFunc("POST /v1/sessions/{session_id}/hosted-invitations", service.handleCreateHostedInvitation)
	service.server.HandleFunc("POST /v1/sessions/{session_id}/hosted-invitations/redeem", service.handleRedeemHostedInvitation)
	service.server.HandleFunc("POST /v1/sessions/{session_id}/exports", service.handleExport)
	service.server.HandleFunc("GET /v1/collaboration", service.handleWebSocket)
}

func (service *Service) handleLive(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (service *Service) handleReady(writer http.ResponseWriter, request *http.Request) {
	service.mutex.RLock()
	unavailable := len(service.recoveryErrors)
	service.mutex.RUnlock()
	if unavailable > 0 {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "unrecoverable_documents": unavailable})
		return
	}
	if readiness, ok := service.store.(interface{ Ready(context.Context) error }); ok {
		if err := readiness.Ready(request.Context()); err != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (service *Service) setDocumentRecoveryError(documentID model.DocumentID, err error) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	if err == nil {
		delete(service.recoveryErrors, documentID)
		return
	}
	service.recoveryErrors[documentID] = err
}

func (service *Service) handleVersion(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"build": service.config.Build, "revision": service.config.Revision,
		"protocol_versions": []uint16{model.ProtocolVersion}, "schema_versions": []uint16{model.SchemaVersion}, "compatibility": service.compatibility,
	})
}

func (service *Service) handleCreateSession(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	token := bearerToken(request)
	launch, exists := service.launchToken(token)
	if !exists {
		writeError(writer, http.StatusUnauthorized, "unauthorized", "invalid or redeemed launch token")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, service.limits.MaxSnapshotBodyBytes)
	var body struct {
		Snapshot    model.Snapshot `json:"snapshot"`
		DisplayName string         `json:"display_name,omitempty"`
		BulkEdits   bool           `json:"bulk_edits,omitempty"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		status := http.StatusBadRequest
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(writer, status, "invalid_request", "invalid session request")
		return
	}
	displayName := "Owner"
	if body.DisplayName != "" {
		displayName = strings.TrimSpace(body.DisplayName)
		if displayName == "" || len(displayName) > protocol.MaxDisplayNameBytes {
			writeError(writer, http.StatusBadRequest, "invalid_request", "owner display name is invalid")
			return
		}
	}
	mapHash, err := body.Snapshot.Hash()
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_snapshot", "snapshot hash failed")
		return
	}
	if launch.expectedDocumentID != "" && (body.Snapshot.DocumentID != launch.expectedDocumentID || mapHash != launch.expectedMapHash) {
		writeError(writer, http.StatusForbidden, "snapshot_mismatch", "snapshot does not match the embedded launch document")
		return
	}
	if !service.redeemLaunchToken(token) {
		writeError(writer, http.StatusUnauthorized, "unauthorized", "invalid or redeemed launch token")
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
	service.setDocumentRecoveryError(body.Snapshot.DocumentID, nil)
	current, err := owner.Snapshot(request.Context())
	if err != nil {
		_ = owner.Close(request.Context())
		writeError(writer, http.StatusInternalServerError, "internal", "session recovery failed")
		return
	}
	mapHash, err = current.Hash()
	if err != nil {
		_ = owner.Close(request.Context())
		writeError(writer, http.StatusInternalServerError, "internal", "session recovery hash failed")
		return
	}
	actorID, err := model.NewActorID()
	if err != nil {
		_ = owner.Close(request.Context())
		writeError(writer, http.StatusInternalServerError, "internal", "identity generation failed")
		return
	}
	principal, err := NewPrincipal("embedded-owner", actorID, displayName, RoleOwner)
	if err != nil {
		_ = owner.Close(request.Context())
		writeError(writer, http.StatusInternalServerError, "internal", "identity initialization failed")
		return
	}
	sessionID, err := randomToken()
	if err != nil {
		_ = owner.Close(request.Context())
		writeError(writer, http.StatusInternalServerError, "internal", "session generation failed")
		return
	}
	ownerToken, ownerTokenExpiresAt, err := service.issueToken(sessionID, principal)
	if err != nil {
		_ = owner.Close(request.Context())
		writeError(writer, http.StatusInternalServerError, "internal", "token generation failed")
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
	writeJSON(writer, http.StatusCreated, CreateSessionResponse{
		SessionID: sessionID, DocumentID: current.DocumentID, Revision: current.Revision, MapHash: mapHash, OwnerToken: ownerToken, OwnerTokenExpiresAt: ownerTokenExpiresAt,
	})
}

func (service *Service) launchToken(token string) (tokenRecord, bool) {
	service.mutex.RLock()
	defer service.mutex.RUnlock()
	record, exists := service.tokens[token]
	return record, exists && record.launch && service.config.Now().Before(record.expiresAt)
}

func (service *Service) handleGetSession(writer http.ResponseWriter, request *http.Request) {
	sessionID := request.PathValue("session_id")
	_, record, ok := service.authorize(request, sessionID)
	if !ok {
		writeError(writer, http.StatusUnauthorized, "unauthorized", "session token is invalid")
		return
	}
	snapshot, err := record.owner.Snapshot(request.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "session is unavailable")
		return
	}
	hash, _ := snapshot.Hash()
	writeJSON(writer, http.StatusOK, map[string]any{"session_id": sessionID, "document_id": snapshot.DocumentID, "protocol_version": snapshot.ProtocolVersion, "schema_version": snapshot.SchemaVersion, "revision": snapshot.Revision, "map_hash": hash})
}

func (service *Service) handleGetSnapshot(writer http.ResponseWriter, request *http.Request) {
	sessionID := request.PathValue("session_id")
	_, record, ok := service.authorize(request, sessionID)
	if !ok {
		writeError(writer, http.StatusUnauthorized, "unauthorized", "session token is invalid")
		return
	}
	snapshot, err := record.owner.Snapshot(request.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "session is unavailable")
		return
	}
	hash, _ := snapshot.Hash()
	writer.Header().Set("ETag", `"`+hash+`"`)
	writeJSON(writer, http.StatusOK, snapshot)
}

func (service *Service) handleCreateJoinToken(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	sessionID := request.PathValue("session_id")
	authToken, _, ok := service.authorizeRecord(request, sessionID)
	if !ok || authToken.hosted || authToken.resumption || !authToken.principal.CanAdminister() {
		writeError(writer, http.StatusForbidden, "forbidden", "owner role is required")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, service.limits.MaxHTTPBodyBytes)
	var body struct {
		Role        Role   `json:"role"`
		DisplayName string `json:"display_name"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid join-token request")
		return
	}
	actorID, err := model.NewActorID()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal", "identity generation failed")
		return
	}
	joined, err := NewPrincipal("join-"+string(actorID), actorID, body.DisplayName, body.Role)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid join identity")
		return
	}
	if err := service.hub.Join(sessionID, joined); err != nil {
		writeError(writer, http.StatusBadRequest, "join_failed", "could not authorize join")
		return
	}
	token, expiresAt, err := service.issueToken(sessionID, joined)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal", "token generation failed")
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"token": token, "actor_id": actorID, "role": body.Role, "expires_at": expiresAt})
}

func (service *Service) handleExport(writer http.ResponseWriter, request *http.Request) {
	sessionID := request.PathValue("session_id")
	token, session, ok := service.authorizeRecord(request, sessionID)
	if !ok || !token.principal.CanAdminister() {
		writeError(writer, http.StatusForbidden, "forbidden", "owner role is required")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, service.limits.MaxHTTPBodyBytes)
	var body struct {
		Revision       model.Revision `json:"revision"`
		MapHash        string         `json:"map_hash"`
		IdempotencyKey string         `json:"idempotency_key"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeError(writer, http.StatusRequestEntityTooLarge, "invalid_request", "export checkpoint request is too large")
			return
		}
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid export checkpoint request")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid export checkpoint request")
		return
	}
	if strings.TrimSpace(body.IdempotencyKey) == "" || len(body.IdempotencyKey) > model.MaxCheckpointTextBytes || model.ValidateSHA256("map hash", body.MapHash) != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid export checkpoint request")
		return
	}
	snapshot, err := session.owner.Snapshot(request.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "session is unavailable")
		return
	}
	mapHash, err := snapshot.Hash()
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "unavailable", "session snapshot is invalid")
		return
	}
	if body.Revision != snapshot.Revision || body.MapHash != mapHash {
		writeJSON(writer, http.StatusConflict, map[string]any{
			"code": "stale_checkpoint", "message": "export checkpoint precondition does not match the authoritative document",
			"current_revision": snapshot.Revision, "current_map_hash": mapHash,
		})
		return
	}
	checkpointID, err := model.NewCheckpointID()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal", "checkpoint id generation failed")
		return
	}
	checkpoint := model.ExportCheckpoint{
		CheckpointID:   checkpointID,
		IdempotencyKey: body.IdempotencyKey,
		DocumentID:     snapshot.DocumentID,
		SessionID:      sessionID,
		Revision:       snapshot.Revision,
		MapHash:        mapHash,
		RequestedBy:    token.principal.ActorID(),
		CreatedAt:      service.config.Now().UTC(),
		Status:         model.ExportCheckpointPending,
	}
	stored, _, err := service.store.CreateExportCheckpoint(request.Context(), checkpoint)
	if err != nil {
		switch {
		case errors.Is(err, collabstore.ErrCheckpointConflict):
			writeError(writer, http.StatusConflict, "idempotency_conflict", "idempotency key conflicts with an existing checkpoint")
		case errors.Is(err, collabstore.ErrSessionMissing), errors.Is(err, collabstore.ErrStoreClosed):
			writeError(writer, http.StatusServiceUnavailable, "unavailable", "checkpoint persistence is unavailable")
		default:
			writeError(writer, http.StatusInternalServerError, "internal", "checkpoint persistence failed")
		}
		return
	}
	writeJSON(writer, http.StatusAccepted, stored)
}

func (service *Service) authorize(request *http.Request, sessionID string) (Principal, sessionRecord, bool) {
	token, session, ok := service.authorizeRecord(request, sessionID)
	return token.principal, session, ok
}

func (service *Service) authorizeRecord(request *http.Request, sessionID string) (tokenRecord, sessionRecord, bool) {
	service.mutex.RLock()
	tokenValue := bearerToken(request)
	token, tokenExists := service.tokens[tokenValue]
	session, sessionExists := service.sessions[sessionID]
	service.mutex.RUnlock()
	if tokenExists && !token.launch && token.sessionID == sessionID && sessionExists && service.config.Now().Before(token.expiresAt) {
		return token, session, true
	}
	if !sessionExists || service.config.HostedAuth == nil {
		return tokenRecord{}, sessionRecord{}, false
	}
	hosted, err := service.reauthorizeHosted(request.Context(), tokenValue, sessionID)
	if err != nil {
		return tokenRecord{}, sessionRecord{}, false
	}
	return hosted, session, true
}

func (service *Service) reauthorizeHosted(ctx context.Context, credential, sessionID string) (tokenRecord, error) {
	if credential == "" || service.config.HostedAuth == nil {
		return tokenRecord{}, fmt.Errorf("hosted authentication is unavailable")
	}
	session, err := service.config.HostedAuth.AuthorizeSession(ctx, credential, sessionID)
	if err != nil {
		return tokenRecord{}, err
	}
	if !service.config.Now().Before(session.ExpiresAt) {
		return tokenRecord{}, fmt.Errorf("hosted authentication session is expired")
	}
	principal, err := principalFromHosted(session)
	if err != nil {
		return tokenRecord{}, err
	}
	if err := service.hub.Join(sessionID, principal); err != nil {
		return tokenRecord{}, err
	}
	return tokenRecord{hosted: true, hostedCredential: credential, sessionID: sessionID, principal: principal, expiresAt: session.ExpiresAt}, nil
}

func principalFromHosted(session auth.Session) (Principal, error) {
	identityHash := sha256.Sum256([]byte(session.Issuer + "\x00" + session.Subject))
	return NewPrincipal("oidc-"+hex.EncodeToString(identityHash[:]), session.ActorID, session.DisplayName, Role(session.Role))
}

func (service *Service) redeemLaunchToken(token string) bool {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	record, exists := service.tokens[token]
	if !exists || !record.launch || !service.config.Now().Before(record.expiresAt) {
		return false
	}
	delete(service.tokens, token)
	return true
}

func (service *Service) issueToken(sessionID string, principal Principal) (string, time.Time, error) {
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := service.config.Now().Add(service.config.JoinTokenTTL)
	service.mutex.Lock()
	service.tokens[token] = tokenRecord{sessionID: sessionID, principal: principal, expiresAt: expiresAt}
	service.mutex.Unlock()
	return token, expiresAt, nil
}

func (service *Service) rotateResumptionToken(token, sessionID string, principal Principal) (string, time.Time, error) {
	rotated, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := service.config.Now().Add(service.config.ResumptionTokenTTL)
	service.mutex.Lock()
	defer service.mutex.Unlock()
	record, exists := service.tokens[token]
	if !exists || record.launch || record.webSocketRedeemed || record.sessionID != sessionID || record.principal.ActorID() != principal.ActorID() || !service.config.Now().Before(record.expiresAt) {
		return "", time.Time{}, fmt.Errorf("session credential is invalid or already redeemed")
	}
	if record.resumption {
		delete(service.tokens, token)
	} else {
		record.webSocketRedeemed = true
		service.tokens[token] = record
	}
	service.tokens[rotated] = tokenRecord{resumption: true, sessionID: sessionID, principal: principal, expiresAt: expiresAt}
	return rotated, expiresAt, nil
}

func randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func bearerToken(request *http.Request) string {
	value := request.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(value, "Bearer ")
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]string{"code": code, "message": message})
}
