package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	collabstore "sdmm/internal/aphelion/collab/store"
)

func (backend *fakeHostedBackend) ListCommunityHostedSessions(_ context.Context, actorID model.ActorID, activeIDs []string, request collabstore.HostedSessionPageRequest) (collabstore.HostedSessionPage, error) {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	active := make(map[string]bool, len(activeIDs))
	for _, id := range activeIDs {
		active[id] = true
	}
	var sessions []collabstore.HostedSession
	for id, session := range backend.sessions {
		if !active[id] || session.Visibility != collabstore.HostedVisibilityCommunity {
			continue
		}
		if hostedMember, found := hostedMemberByActor(backend.members[id], actorID); found && hostedMember.Disabled {
			continue
		}
		session.OwnerDisplayName = hostedOwnerDisplayName(backend.members[id])
		sessions = append(sessions, session)
	}
	return fakeHostedSessionPage(sessions, request), nil
}

func (backend *fakeHostedBackend) ListMyHostedSessions(_ context.Context, actorID model.ActorID, request collabstore.HostedSessionPageRequest) (collabstore.HostedSessionPage, error) {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	var sessions []collabstore.HostedSession
	for id, session := range backend.sessions {
		member, found := hostedMemberByActor(backend.members[id], actorID)
		if !found || member.Disabled {
			continue
		}
		session.OwnerDisplayName = hostedOwnerDisplayName(backend.members[id])
		sessions = append(sessions, session)
	}
	return fakeHostedSessionPage(sessions, request), nil
}

func (backend *fakeHostedBackend) UpdateHostedSessionMetadata(_ context.Context, sessionID string, actorID model.ActorID, metadata collabstore.HostedSessionMetadata) (collabstore.HostedSession, error) {
	metadata, err := collabstore.NormalizeHostedSessionMetadata(metadata)
	if err != nil {
		return collabstore.HostedSession{}, err
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	session, found := backend.sessions[sessionID]
	if !found {
		return collabstore.HostedSession{}, collabstore.ErrHostedSessionMissing
	}
	owner, found := hostedMemberByActor(backend.members[sessionID], actorID)
	if !found || owner.Disabled || owner.Role != collabstore.HostedRoleOwner {
		return collabstore.HostedSession{}, collabstore.ErrHostedSessionMissing
	}
	session.Visibility = metadata.Visibility
	session.Title = metadata.Title
	session.MapLabel = metadata.MapLabel
	session.EnvironmentLabel = metadata.EnvironmentLabel
	backend.sessions[sessionID] = session
	return session, nil
}

func (backend *fakeHostedBackend) JoinHostedSession(_ context.Context, sessionID string, identity collabstore.HostedIdentity) (collabstore.HostedMember, bool, error) {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	session, found := backend.sessions[sessionID]
	if !found {
		return collabstore.HostedMember{}, false, collabstore.ErrHostedSessionMissing
	}
	key := identity.Issuer + "\x00" + identity.Subject
	if member, exists := backend.members[sessionID][key]; exists {
		if member.ActorID != identity.ActorID {
			return collabstore.HostedMember{}, false, collabstore.ErrHostedIdentityMismatch
		}
		if member.Disabled {
			return collabstore.HostedMember{}, false, collabstore.ErrHostedMemberDisabled
		}
		return member, false, nil
	}
	if session.Visibility != collabstore.HostedVisibilityCommunity {
		return collabstore.HostedMember{}, false, collabstore.ErrHostedSessionNotCommunity
	}
	member := collabstore.HostedMember{SessionID: sessionID, Issuer: identity.Issuer, Subject: identity.Subject, ActorID: identity.ActorID, DisplayName: identity.DisplayName, Role: collabstore.HostedRoleEditor}
	backend.members[sessionID][key] = member
	return member, true, nil
}

func TestHostedBrowserHTTPHandlersRespectVisibilityMembershipAndOwnerSettings(t *testing.T) {
	now := time.Now().UTC()
	actors := make(map[string]model.ActorID)
	for _, name := range []string{"owner", "viewer", "disabled", "new"} {
		actorID, err := model.NewActorID()
		if err != nil {
			t.Fatal(err)
		}
		actors[name] = actorID
	}
	authentication := make(map[string]auth.Session, len(actors))
	for name, actorID := range actors {
		authentication[name+"-token"] = auth.Session{Token: name + "-token", ActorID: actorID, Issuer: "https://issuer.example", Subject: name, DisplayName: name, ExpiresAt: now.Add(time.Hour)}
	}
	backend := newFakeHostedBackend(authentication)
	service := NewService(ServiceConfig{HostedAuth: backend, HostedRegistry: backend, Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	testServer := httptest.NewServer(service.Handler())
	t.Cleanup(testServer.Close)

	community := createHostedBrowserTestSession(t, testServer.URL, "owner-token", testSnapshot(t, 1), collabstore.HostedSessionMetadata{Visibility: collabstore.HostedVisibilityCommunity, Title: "Engineering", MapLabel: "station.dmm", EnvironmentLabel: "station.dme"})
	private := createHostedBrowserTestSession(t, testServer.URL, "owner-token", testSnapshot(t, 1), collabstore.HostedSessionMetadata{})
	service.trackHostedConnection(community.SessionID, actors["owner"], now.Add(time.Hour))
	service.trackHostedConnection(private.SessionID, actors["owner"], now.Add(time.Hour))

	for _, name := range []string{"viewer", "disabled"} {
		role := collabstore.HostedRoleViewer
		member := collabstore.HostedMember{SessionID: community.SessionID, Issuer: "https://issuer.example", Subject: name, ActorID: actors[name], DisplayName: name, Role: role, Disabled: name == "disabled"}
		backend.mutex.Lock()
		backend.members[community.SessionID][member.Issuer+"\x00"+member.Subject] = member
		backend.mutex.Unlock()
	}

	response := hostedBrowserRequest(t, http.MethodGet, testServer.URL+"/v1/hosted/sessions?scope=community", "owner-token", nil)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("community listing response = %d, Cache-Control %q", response.StatusCode, response.Header.Get("Cache-Control"))
	}
	var communityPage protocol.HostedSessionsPage
	if err := json.NewDecoder(response.Body).Decode(&communityPage); err != nil {
		t.Fatal(err)
	}
	if len(communityPage.Sessions) != 1 || communityPage.Sessions[0].SessionID != community.SessionID || communityPage.Sessions[0].Participants != 1 || !communityPage.Sessions[0].Available || communityPage.Sessions[0].OwnerDisplayName != "owner" || communityPage.Sessions[0].Title != "Engineering" {
		t.Fatalf("community listing = %#v; Private or inactive sessions must be absent", communityPage)
	}

	joined := hostedBrowserRequest(t, http.MethodPost, testServer.URL+"/v1/hosted/sessions/"+community.SessionID+"/join", "new-token", []byte(`{}`))
	defer func() { _ = joined.Body.Close() }()
	if joined.StatusCode != http.StatusOK {
		t.Fatalf("new Community admission status = %d", joined.StatusCode)
	}
	var joinedResponse struct {
		Role collabstore.HostedRole `json:"role"`
	}
	if err := json.NewDecoder(joined.Body).Decode(&joinedResponse); err != nil {
		t.Fatal(err)
	}
	if joinedResponse.Role != collabstore.HostedRoleEditor {
		t.Fatalf("new member role = %q, want editor", joinedResponse.Role)
	}

	viewerJoin := hostedBrowserRequest(t, http.MethodPost, testServer.URL+"/v1/hosted/sessions/"+community.SessionID+"/join", "viewer-token", []byte(`{}`))
	defer func() { _ = viewerJoin.Body.Close() }()
	var viewerResponse struct {
		Role collabstore.HostedRole `json:"role"`
	}
	if viewerJoin.StatusCode != http.StatusOK || json.NewDecoder(viewerJoin.Body).Decode(&viewerResponse) != nil || viewerResponse.Role != collabstore.HostedRoleViewer {
		t.Fatalf("existing viewer admission status=%d role=%q", viewerJoin.StatusCode, viewerResponse.Role)
	}

	for name, sessionID := range map[string]string{"disabled member": community.SessionID, "new Private member": private.SessionID} {
		token := "disabled-token"
		if name == "new Private member" {
			token = "new-token"
		}
		response := hostedBrowserRequest(t, http.MethodPost, testServer.URL+"/v1/hosted/sessions/"+sessionID+"/join", token, []byte(`{}`))
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("%s admission status = %d, want unavailable", name, response.StatusCode)
		}
	}

	disabledList := hostedBrowserRequest(t, http.MethodGet, testServer.URL+"/v1/hosted/sessions?scope=community", "disabled-token", nil)
	defer func() { _ = disabledList.Body.Close() }()
	var disabledPage protocol.HostedSessionsPage
	if disabledList.StatusCode != http.StatusOK || json.NewDecoder(disabledList.Body).Decode(&disabledPage) != nil || len(disabledPage.Sessions) != 0 {
		t.Fatalf("disabled member listing status=%d sessions=%#v", disabledList.StatusCode, disabledPage.Sessions)
	}

	settings := []byte(`{"visibility":"private","title":"Private engineering","map_label":"station.dmm","environment_label":"station.dme"}`)
	deniedSettings := hostedBrowserRequest(t, http.MethodPatch, testServer.URL+"/v1/hosted/sessions/"+community.SessionID, "viewer-token", settings)
	_ = deniedSettings.Body.Close()
	if deniedSettings.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-owner metadata update status = %d", deniedSettings.StatusCode)
	}
	ownerSettings := hostedBrowserRequest(t, http.MethodPatch, testServer.URL+"/v1/hosted/sessions/"+community.SessionID, "owner-token", settings)
	_ = ownerSettings.Body.Close()
	if ownerSettings.StatusCode != http.StatusNoContent {
		t.Fatalf("owner metadata update status = %d", ownerSettings.StatusCode)
	}
	communityAfterPrivate := hostedBrowserRequest(t, http.MethodGet, testServer.URL+"/v1/hosted/sessions?scope=community", "owner-token", nil)
	defer func() { _ = communityAfterPrivate.Body.Close() }()
	var emptyCommunity protocol.HostedSessionsPage
	if communityAfterPrivate.StatusCode != http.StatusOK || json.NewDecoder(communityAfterPrivate.Body).Decode(&emptyCommunity) != nil || len(emptyCommunity.Sessions) != 0 {
		t.Fatalf("Private session remained listed: %#v", emptyCommunity)
	}
	viewerMySessions := hostedBrowserRequest(t, http.MethodGet, testServer.URL+"/v1/hosted/sessions?scope=mine", "viewer-token", nil)
	defer func() { _ = viewerMySessions.Body.Close() }()
	var myPage protocol.HostedSessionsPage
	if viewerMySessions.StatusCode != http.StatusOK || json.NewDecoder(viewerMySessions.Body).Decode(&myPage) != nil || len(myPage.Sessions) != 1 || myPage.Sessions[0].Visibility != string(collabstore.HostedVisibilityPrivate) {
		t.Fatalf("member recovery listing = %#v", myPage)
	}
}

func createHostedBrowserTestSession(t *testing.T, baseURL, token string, snapshot model.Snapshot, metadata collabstore.HostedSessionMetadata) HostedSessionResponse {
	t.Helper()
	body, err := json.Marshal(struct {
		Snapshot model.Snapshot `json:"snapshot"`
		collabstore.HostedSessionMetadata
	}{Snapshot: snapshot, HostedSessionMetadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	response := postJSON(t, baseURL+"/v1/hosted/sessions", token, body)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create hosted browser session status = %d", response.StatusCode)
	}
	var created HostedSessionResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return created
}

func hostedBrowserRequest(t *testing.T, method, url, token string, body []byte) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func hostedMemberByActor(members map[string]collabstore.HostedMember, actorID model.ActorID) (collabstore.HostedMember, bool) {
	for _, member := range members {
		if member.ActorID == actorID {
			return member, true
		}
	}
	return collabstore.HostedMember{}, false
}

func hostedOwnerDisplayName(members map[string]collabstore.HostedMember) string {
	for _, member := range members {
		if member.Role == collabstore.HostedRoleOwner {
			return member.DisplayName
		}
	}
	return ""
}

func fakeHostedSessionPage(sessions []collabstore.HostedSession, request collabstore.HostedSessionPageRequest) collabstore.HostedSessionPage {
	sort.Slice(sessions, func(left, right int) bool { return sessions[left].SessionID < sessions[right].SessionID })
	limit := request.Limit
	if limit <= 0 || limit > collabstore.HostedPageMaxLimit {
		limit = collabstore.HostedPageDefaultLimit
	}
	page := collabstore.HostedSessionPage{}
	for _, session := range sessions {
		if request.Cursor != "" && session.SessionID <= request.Cursor {
			continue
		}
		if len(page.Sessions) == limit {
			page.NextCursor = page.Sessions[limit-1].SessionID
			break
		}
		page.Sessions = append(page.Sessions, session)
	}
	return page
}

var _ collabstore.HostedSessionBrowserStore = (*fakeHostedBackend)(nil)
