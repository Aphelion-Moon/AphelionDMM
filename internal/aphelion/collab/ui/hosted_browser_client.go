package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

var ErrHostedBrowserUnsupported = errors.New("this service does not support session browsing; use an invitation")

var ErrHostedSignInRequired = errors.New("hosted login expired or was lost when the service restarted; sign in again")

// HostedConnection identifies an admitted target; it contains no invitation or login secret.
type HostedConnection struct {
	BaseURL    string
	SessionID  string
	generation uint64
}

type HostedAccount struct {
	Origin, DisplayName string
	ActorID             model.ActorID
	Generation          uint64
	SignedIn            bool
	Attached            bool
}

func (client *SessionClient) HostedAccount() HostedAccount {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return HostedAccount{Origin: client.hostedBaseURL, DisplayName: client.hostedDisplayName, ActorID: client.hostedActorID, Generation: client.hostedGeneration, SignedIn: client.hostedCredential != "" && client.config.Now().Before(client.hostedCredentialExpires), Attached: client.hostedSession}
}

func (client *SessionClient) HostedAccountCurrent(account HostedAccount) bool {
	current := client.HostedAccount()
	return current.Origin == account.Origin && current.Generation == account.Generation
}

func (client *SessionClient) CancelHostedSignIn() {
	client.mutex.Lock()
	client.hostedGeneration++
	client.mutex.Unlock()
}

func (client *SessionClient) discardHostedLogin(origin, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := client.request(ctx, http.MethodPost, origin+"/v1/auth/logout", token, nil)
	if err == nil {
		if response, err := client.http.Do(request); err == nil {
			_ = response.Body.Close()
		}
	}
}

func (client *SessionClient) HostedCapabilities(ctx context.Context) (protocol.HostedCapabilities, error) {
	account := client.HostedAccount()
	client.mutex.Lock()
	if account.Generation == client.hostedGeneration && account.Origin == client.hostedBaseURL {
		client.hostedSnapshotGzip = false
		client.hostedRepository = false // APHELION EDIT ADDITION - REPOSITORY ALIGNMENT
	}
	client.mutex.Unlock()
	if account.Origin == "" {
		return protocol.HostedCapabilities{}, fmt.Errorf("sign in to a hosted service first")
	}
	request, err := client.request(ctx, "GET", account.Origin+"/v1/hosted/capabilities", "", nil)
	if err != nil {
		return protocol.HostedCapabilities{}, err
	}
	response, err := client.http.Do(request)
	if err != nil {
		return protocol.HostedCapabilities{}, err
	}
	defer func() { _ = response.Body.Close() }()
	// Only the old Go ServeMux's plain unsupported-route response counts as absent support.
	if (response.StatusCode == 404 || response.StatusCode == 405) && strings.HasPrefix(response.Header.Get("Content-Type"), "text/plain") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 256))
		if string(body) == "404 page not found\n" || string(body) == "Method Not Allowed\n" {
			return protocol.HostedCapabilities{}, ErrHostedBrowserUnsupported
		}
	}
	if response.StatusCode != 200 {
		return protocol.HostedCapabilities{}, fmt.Errorf("service capabilities returned HTTP %d", response.StatusCode)
	}
	var result protocol.HostedCapabilities
	err = decodeLimited(response.Body, &result)
	if !client.HostedAccountCurrent(account) {
		return result, ErrSessionChanged
	}
	client.mutex.Lock()
	if account.Generation == client.hostedGeneration && account.Origin == client.hostedBaseURL {
		client.hostedSnapshotGzip = err == nil && response.Header.Get("Accept-Encoding") == "gzip"
		// APHELION EDIT ADDITION - REPOSITORY ALIGNMENT
		client.hostedRepository = err == nil && response.Header.Get(protocol.RepositoryCapabilityHeader) == protocol.RepositoryCapabilityValue
	}
	client.mutex.Unlock()
	if err == nil && !result.SessionBrowser {
		err = ErrHostedBrowserUnsupported
	}
	return result, err
}

func (client *SessionClient) hostedBrowserRequest(ctx context.Context, account HostedAccount, method, path string, query url.Values, body any, result any) error {
	client.mutex.Lock()
	if account.Generation != client.hostedGeneration || account.Origin != client.hostedBaseURL {
		client.mutex.Unlock()
		return ErrSessionChanged
	}
	origin, credential := client.hostedBaseURL, client.hostedCredential
	compressSnapshot := client.hostedSnapshotGzip && method == "POST" && path == "/v1/hosted/sessions"
	valid := credential != "" && client.config.Now().Before(client.hostedCredentialExpires)
	client.mutex.Unlock()
	if !valid {
		return ErrHostedSignInRequired
	}
	var err error
	var encoded []byte
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	if compressSnapshot {
		encoded, err = compressSnapshotRequest(encoded)
		if err != nil {
			return err
		}
	}
	request, err := client.request(ctx, method, origin+path, credential, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Content-Type", "application/json")
	if compressSnapshot {
		request.Header.Set("Content-Encoding", "gzip")
	}
	var response *http.Response
	if method == "POST" && path == "/v1/hosted/sessions" {
		response, err = client.doSnapshotRequest(request)
	} else {
		response, err = client.http.Do(request)
	}
	if err != nil {
		return fmt.Errorf("hosted service request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if !client.HostedAccountCurrent(account) || ctx.Err() != nil {
		return ErrSessionChanged
	}
	if response.StatusCode == 401 {
		client.mutex.Lock()
		if client.hostedGeneration == account.Generation && client.hostedBaseURL == account.Origin {
			client.hostedCredentialExpires = time.Time{}
		}
		client.mutex.Unlock()
		return ErrHostedSignInRequired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusConflict {
			var conflict struct {
				Code string `json:"code"`
			}
			if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&conflict) == nil {
				switch conflict.Code {
				case "transaction_upgrade_required":
					return fmt.Errorf("the hosted service needs an operator to upgrade transaction storage before this editor can start a session")
				case "session_exists":
					return fmt.Errorf("this map's session identity is already stored; choose My sessions to rejoin an active session, or reopen the saved map to start a new one")
				}
			}
		}
		return fmt.Errorf("hosted service returned HTTP %d; refresh and check your access", response.StatusCode)
	}
	if result != nil {
		if err = decodeLimited(response.Body, result); err != nil {
			return err
		}
	}
	if !client.HostedAccountCurrent(account) || ctx.Err() != nil {
		return ErrSessionChanged
	}
	return nil
}

func (client *SessionClient) ListHostedSessions(ctx context.Context, scope, cursor string) (protocol.HostedSessionsPage, error) {
	var page protocol.HostedSessionsPage
	query := url.Values{"scope": {scope}, "cursor": {cursor}, "limit": {"50"}}
	// APHELION EDIT ADDITION START - REPOSITORY ALIGNMENT
	if client.RepositoryDescriptorSupported() {
		query.Set("include", protocol.IncludeRepositoryQuery)
	}
	// APHELION EDIT ADDITION END
	err := client.hostedBrowserRequest(ctx, client.HostedAccount(), "GET", "/v1/hosted/sessions", query, nil, &page)
	if err == nil && len(page.Sessions) > 100 {
		return protocol.HostedSessionsPage{}, fmt.Errorf("session list exceeds page limit")
	}
	// APHELION EDIT ADDITION START - REPOSITORY ALIGNMENT
	for i := range page.Sessions {
		// Hostile until validated: an invalid descriptor is treated as unknown.
		if repository := page.Sessions[i].Repository; repository != nil && repository.Validate() != nil {
			page.Sessions[i].Repository = nil
		}
	}
	// APHELION EDIT ADDITION END
	return page, err
}

func (client *SessionClient) AdmitHostedSession(ctx context.Context, account HostedAccount, id string) (HostedConnection, error) {
	var joined struct {
		SessionID string        `json:"session_id"`
		ActorID   model.ActorID `json:"actor_id"`
		Role      string        `json:"role"`
	}
	err := client.hostedBrowserRequest(ctx, account, "POST", "/v1/hosted/sessions/"+url.PathEscape(id)+"/join", nil, nil, &joined)
	if err != nil {
		return HostedConnection{}, err
	}
	if joined.SessionID != id || (joined.Role != "owner" && joined.Role != "editor" && joined.Role != "viewer") || (account.ActorID != "" && joined.ActorID != account.ActorID) || !client.HostedAccountCurrent(account) {
		return HostedConnection{}, ErrSessionChanged
	}
	return HostedConnection{BaseURL: account.Origin, SessionID: id, generation: account.Generation}, nil
}

func (client *SessionClient) UpdateHostedSession(ctx context.Context, account HostedAccount, id string, metadata protocol.HostedSessionMetadata) error {
	return client.hostedBrowserRequest(ctx, account, "PATCH", "/v1/hosted/sessions/"+url.PathEscape(id), nil, metadata, nil)
}

func (client *SessionClient) CreateHostedWithMetadata(ctx context.Context, account HostedAccount, snapshot model.Snapshot, metadata protocol.HostedSessionMetadata) (HostedConnection, error) {
	return client.CreateHostedWithRepository(ctx, account, snapshot, metadata, nil)
}

// CreateHostedWithRepository publishes the optional repository descriptor only
// when the service advertised support, because older services reject unknown fields.
func (client *SessionClient) CreateHostedWithRepository(ctx context.Context, account HostedAccount, snapshot model.Snapshot, metadata protocol.HostedSessionMetadata, repository *protocol.RepositoryDescriptor) (HostedConnection, error) {
	if !client.HostedAccountCurrent(account) {
		return HostedConnection{}, ErrSessionChanged
	}
	legacy := false
	if _, err := client.HostedCapabilities(ctx); err != nil {
		if !errors.Is(err, ErrHostedBrowserUnsupported) {
			return HostedConnection{}, err
		}
		if metadata.Visibility == "community" || metadata.Title != "" || metadata.MapLabel != "" || metadata.EnvironmentLabel != "" {
			return HostedConnection{}, fmt.Errorf("this older service supports only unnamed Private sessions; clear the title and Community option")
		}
		legacy = true
	}
	var result struct {
		SessionID  string           `json:"session_id"`
		DocumentID model.DocumentID `json:"document_id"`
		Revision   model.Revision   `json:"revision"`
		MapHash    string           `json:"map_hash"`
	}
	// APHELION EDIT ADDITION START - REPOSITORY ALIGNMENT
	if repository != nil && (!client.RepositoryDescriptorSupported() || repository.Validate() != nil) {
		repository = nil
	}
	// APHELION EDIT ADDITION END
	var body any = struct {
		Snapshot  model.Snapshot `json:"snapshot"`
		BulkEdits bool           `json:"bulk_edits"`
		protocol.HostedSessionMetadata
		Repository *protocol.RepositoryDescriptor `json:"repository,omitempty"` // APHELION EDIT ADDITION - REPOSITORY ALIGNMENT
	}{snapshot, true, metadata, repository}
	if legacy {
		body = map[string]any{"snapshot": snapshot, "bulk_edits": true}
	}
	err := client.hostedBrowserRequest(ctx, account, "POST", "/v1/hosted/sessions", nil, body, &result)
	if err != nil {
		return HostedConnection{}, err
	}
	if result.DocumentID != snapshot.DocumentID || result.SessionID == "" || !client.HostedAccountCurrent(account) {
		return HostedConnection{}, ErrSessionChanged
	}
	return HostedConnection{BaseURL: account.Origin, SessionID: result.SessionID, generation: account.Generation}, nil
}

// RepositoryDescriptorSupported reports the most recent capability probe's result.
// APHELION EDIT ADDITION - REPOSITORY ALIGNMENT
func (client *SessionClient) RepositoryDescriptorSupported() bool {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.hostedRepository
}
