package ui

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
)

var ErrHostedSignInPending = errors.New("hosted sign-in is pending in the browser")

type HostedSignIn struct {
	AuthorizationURL string
	baseURL          string
	handoffID        string
	verifier         string
}

func (client *SessionClient) BeginHostedSignIn(ctx context.Context, baseURL string) (HostedSignIn, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if err := validateHostedEndpoint(baseURL); err != nil {
		return HostedSignIn{}, err
	}
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return HostedSignIn{}, fmt.Errorf("generate hosted sign-in verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	challengeHash := sha256.Sum256([]byte(verifier))
	body, err := json.Marshal(map[string]string{"verifier_challenge": base64.RawURLEncoding.EncodeToString(challengeHash[:])})
	if err != nil {
		return HostedSignIn{}, err
	}
	request, err := client.request(ctx, http.MethodPost, baseURL+"/v1/auth/desktop/begin", "", bytes.NewReader(body))
	if err != nil {
		return HostedSignIn{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return HostedSignIn{}, fmt.Errorf("begin hosted sign-in: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return HostedSignIn{}, fmt.Errorf("begin hosted sign-in returned HTTP %d", response.StatusCode)
	}
	var started struct {
		AuthorizationURL string `json:"authorization_url"`
		HandoffID        string `json:"handoff_id"`
	}
	if err := decodeLimited(response.Body, &started); err != nil {
		return HostedSignIn{}, fmt.Errorf("decode hosted sign-in: %w", err)
	}
	if started.AuthorizationURL == "" || started.HandoffID == "" || len(started.HandoffID) > 128 {
		return HostedSignIn{}, fmt.Errorf("hosted sign-in response is invalid")
	}
	return HostedSignIn{AuthorizationURL: started.AuthorizationURL, baseURL: baseURL, handoffID: started.HandoffID, verifier: verifier}, nil
}

func (client *SessionClient) CompleteHostedSignIn(ctx context.Context, signIn HostedSignIn) error {
	if signIn.baseURL == "" || signIn.handoffID == "" || signIn.verifier == "" {
		return fmt.Errorf("hosted sign-in handoff is incomplete")
	}
	body, err := json.Marshal(map[string]string{"handoff_id": signIn.handoffID, "verifier": signIn.verifier})
	if err != nil {
		return err
	}
	request, err := client.request(ctx, http.MethodPost, signIn.baseURL+"/v1/auth/desktop/exchange", "", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("complete hosted sign-in: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusAccepted {
		return ErrHostedSignInPending
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("complete hosted sign-in returned HTTP %d", response.StatusCode)
	}
	var authenticated struct {
		Token       string    `json:"token"`
		DisplayName string    `json:"display_name"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	if err := decodeLimited(response.Body, &authenticated); err != nil {
		return fmt.Errorf("decode hosted sign-in completion: %w", err)
	}
	if authenticated.Token == "" || authenticated.DisplayName == "" || !client.config.Now().Before(authenticated.ExpiresAt) {
		return fmt.Errorf("hosted sign-in completion is invalid")
	}
	client.mutex.Lock()
	client.hostedCredential = authenticated.Token
	client.hostedCredentialExpires = authenticated.ExpiresAt
	client.hostedDisplayName = authenticated.DisplayName
	client.hostedBaseURL = signIn.baseURL
	client.mutex.Unlock()
	return nil
}

func (client *SessionClient) HostedSignedIn() bool {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.hostedCredential != "" && client.config.Now().Before(client.hostedCredentialExpires)
}

func (client *SessionClient) SignOutHosted(ctx context.Context) error {
	client.mutex.Lock()
	if client.hostedSession {
		client.mutex.Unlock()
		return fmt.Errorf("leave the hosted collaboration session before signing out")
	}
	baseURL := client.hostedBaseURL
	credential := client.hostedCredential
	client.hostedCredential = ""
	client.hostedCredentialExpires = time.Time{}
	client.hostedDisplayName = ""
	client.hostedBaseURL = ""
	client.mutex.Unlock()
	if baseURL == "" || credential == "" {
		return nil
	}
	request, err := client.request(ctx, http.MethodPost, baseURL+"/v1/auth/logout", credential, nil)
	if err != nil {
		return err
	}
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("sign out hosted collaboration: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("sign out hosted collaboration returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (client *SessionClient) CreateHosted(ctx context.Context, snapshot model.Snapshot) (Invitation, error) {
	baseURL, credential, expiresAt, err := client.hostedAuthentication()
	if err != nil {
		return Invitation{}, err
	}
	body, err := json.Marshal(map[string]any{"snapshot": snapshot, "bulk_edits": true})
	if err != nil {
		return Invitation{}, err
	}
	request, err := client.request(ctx, http.MethodPost, baseURL+"/v1/hosted/sessions", credential, bytes.NewReader(body))
	if err != nil {
		return Invitation{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return Invitation{}, fmt.Errorf("create hosted collaboration session: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		return Invitation{}, fmt.Errorf("create hosted collaboration session returned HTTP %d", response.StatusCode)
	}
	var created struct {
		SessionID  string           `json:"session_id"`
		DocumentID model.DocumentID `json:"document_id"`
		Revision   model.Revision   `json:"revision"`
		MapHash    string           `json:"map_hash"`
	}
	if err := decodeLimited(response.Body, &created); err != nil {
		return Invitation{}, fmt.Errorf("decode hosted collaboration session: %w", err)
	}
	if created.SessionID == "" || created.DocumentID != snapshot.DocumentID {
		return Invitation{}, fmt.Errorf("hosted collaboration session response is incompatible with the requested document")
	}
	return Invitation{BaseURL: baseURL, Origin: baseURL, SessionID: created.SessionID, Token: credential, TokenExpiresAt: expiresAt, Hosted: true}, nil
}

func (client *SessionClient) RedeemHostedInvitation(ctx context.Context, invitation Invitation) (Invitation, error) {
	if !invitation.Hosted {
		return Invitation{}, fmt.Errorf("collaboration invitation is not hosted")
	}
	if err := invitation.validate(); err != nil {
		return Invitation{}, err
	}
	baseURL, credential, expiresAt, err := client.hostedAuthentication()
	if err != nil {
		return Invitation{}, err
	}
	if baseURL != strings.TrimRight(invitation.BaseURL, "/") {
		return Invitation{}, fmt.Errorf("hosted sign-in service does not match the invitation origin")
	}
	body, err := json.Marshal(map[string]string{"token": invitation.Token})
	if err != nil {
		return Invitation{}, err
	}
	path := baseURL + "/v1/sessions/" + url.PathEscape(invitation.SessionID) + "/hosted-invitations/redeem"
	request, err := client.request(ctx, http.MethodPost, path, credential, bytes.NewReader(body))
	if err != nil {
		return Invitation{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return Invitation{}, fmt.Errorf("redeem hosted collaboration invitation: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return Invitation{}, fmt.Errorf("redeem hosted collaboration invitation returned HTTP %d", response.StatusCode)
	}
	var redeemed struct {
		SessionID string `json:"session_id"`
		Role      string `json:"role"`
	}
	if err := decodeLimited(response.Body, &redeemed); err != nil {
		return Invitation{}, fmt.Errorf("decode hosted collaboration invitation redemption: %w", err)
	}
	if redeemed.SessionID != invitation.SessionID || (redeemed.Role != "viewer" && redeemed.Role != "editor") {
		return Invitation{}, fmt.Errorf("hosted collaboration invitation redemption is invalid")
	}
	return Invitation{BaseURL: baseURL, Origin: baseURL, SessionID: redeemed.SessionID, Token: credential, TokenExpiresAt: expiresAt, Hosted: true}, nil
}

func (client *SessionClient) hostedAuthentication() (string, string, time.Time, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.hostedBaseURL == "" || client.hostedCredential == "" || !client.config.Now().Before(client.hostedCredentialExpires) {
		return "", "", time.Time{}, fmt.Errorf("hosted sign-in is required")
	}
	return client.hostedBaseURL, client.hostedCredential, client.hostedCredentialExpires, nil
}

func (client *SessionClient) createHostedInvitation(ctx context.Context, role InvitationRole, machine *collabclient.StateMachine, sessionID string) (Invitation, error) {
	baseURL, credential, _, err := client.hostedAuthentication()
	if err != nil {
		return Invitation{}, err
	}
	body, err := json.Marshal(map[string]string{"role": string(role)})
	if err != nil {
		return Invitation{}, err
	}
	path := baseURL + "/v1/sessions/" + url.PathEscape(sessionID) + "/hosted-invitations"
	request, err := client.request(ctx, http.MethodPost, path, credential, bytes.NewReader(body))
	if err != nil {
		return Invitation{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return Invitation{}, fmt.Errorf("create hosted collaboration invitation: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		return Invitation{}, fmt.Errorf("create hosted collaboration invitation returned HTTP %d", response.StatusCode)
	}
	var created struct {
		Token     string    `json:"token"`
		Role      string    `json:"role"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := decodeLimited(response.Body, &created); err != nil {
		return Invitation{}, fmt.Errorf("decode hosted collaboration invitation: %w", err)
	}
	if created.Token == "" || created.Role != string(role) || !client.config.Now().Before(created.ExpiresAt) {
		return Invitation{}, fmt.Errorf("hosted collaboration invitation response is invalid")
	}
	client.mutex.Lock()
	stillCurrent := client.machine == machine && client.transport != nil && client.sessionID == sessionID && client.hostedSession && client.hostedCredential == credential
	client.mutex.Unlock()
	if !stillCurrent {
		return Invitation{}, ErrSessionChanged
	}
	invitation := Invitation{BaseURL: baseURL, Origin: baseURL, SessionID: sessionID, Token: created.Token, TokenExpiresAt: created.ExpiresAt, Hosted: true}
	if err := invitation.validate(); err != nil {
		return Invitation{}, err
	}
	return invitation, nil
}
