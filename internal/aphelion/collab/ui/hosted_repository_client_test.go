package ui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

type repositoryService struct {
	mutex      sync.Mutex
	advertise  bool
	createBody map[string]json.RawMessage
	listQuery  string
	listed     *protocol.RepositoryDescriptor
}

func (s *repositoryService) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		switch {
		case r.URL.Path == "/v1/hosted/capabilities":
			if s.advertise {
				w.Header().Set(protocol.RepositoryCapabilityHeader, protocol.RepositoryCapabilityValue)
			}
			_ = json.NewEncoder(w).Encode(protocol.HostedCapabilities{Provider: "oidc", SessionBrowser: true})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/hosted/sessions":
			body, _ := io.ReadAll(r.Body)
			s.createBody = nil
			_ = json.Unmarshal(body, &s.createBody)
			var snapshot struct {
				Snapshot model.Snapshot `json:"snapshot"`
			}
			_ = json.Unmarshal(body, &snapshot)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"session_id": snapshot.Snapshot.DocumentID, "document_id": snapshot.Snapshot.DocumentID, "revision": 0, "map_hash": strings.Repeat("0", 64)})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/hosted/sessions":
			s.listQuery = r.URL.RawQuery
			summary := protocol.HostedSessionSummary{SessionID: "one", Available: true}
			if r.URL.Query().Get("include") == protocol.IncludeRepositoryQuery {
				summary.Repository = s.listed
			}
			_ = json.NewEncoder(w).Encode(protocol.HostedSessionsPage{Sessions: []protocol.HostedSessionSummary{summary}})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newRepositoryClient(t *testing.T, service *repositoryService) *SessionClient {
	t.Helper()
	server := httptest.NewServer(service.handler(t))
	t.Cleanup(server.Close)
	client := NewSessionClient(SessionClientConfig{})
	client.hostedBaseURL = server.URL
	client.hostedCredential = "secret"
	client.hostedCredentialExpires = time.Now().Add(time.Hour)
	return client
}

func validDescriptor() *protocol.RepositoryDescriptor {
	return &protocol.RepositoryDescriptor{DMEName: "game.dme", EnvironmentHash: strings.Repeat("a", 64), GitBranch: "main", GitCommit: strings.Repeat("c", 40)}
}

func TestCreateSendsRepositoryOnlyToAdvertisingService(t *testing.T) {
	for _, advertise := range []bool{false, true} {
		service := &repositoryService{advertise: advertise}
		client := newRepositoryClient(t, service)
		snapshot := model.Snapshot{DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab"}
		_, err := client.CreateHostedWithRepository(context.Background(), client.HostedAccount(), snapshot, protocol.HostedSessionMetadata{Visibility: "private", Title: "t"}, validDescriptor())
		if err != nil {
			t.Fatal(err)
		}
		_, sent := service.createBody["repository"]
		if sent != advertise {
			t.Fatalf("advertise=%v but repository sent=%v; older services reject unknown fields", advertise, sent)
		}
	}
}

func TestCreateDropsInvalidLocalDescriptor(t *testing.T) {
	service := &repositoryService{advertise: true}
	client := newRepositoryClient(t, service)
	bad := validDescriptor()
	bad.GitBranch = "--detach"
	snapshot := model.Snapshot{DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab"}
	if _, err := client.CreateHostedWithRepository(context.Background(), client.HostedAccount(), snapshot, protocol.HostedSessionMetadata{Visibility: "private"}, bad); err != nil {
		t.Fatal(err)
	}
	if _, sent := service.createBody["repository"]; sent {
		t.Fatal("invalid descriptor was published")
	}
}

func TestListRequestsRepositoryOnlyAfterNegotiationAndRevalidates(t *testing.T) {
	service := &repositoryService{advertise: false, listed: validDescriptor()}
	client := newRepositoryClient(t, service)
	if _, err := client.HostedCapabilities(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListHostedSessions(context.Background(), "mine", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(service.listQuery, "include") {
		t.Fatalf("older service queried with %q", service.listQuery)
	}

	service.advertise = true
	if _, err := client.HostedCapabilities(context.Background()); err != nil {
		t.Fatal(err)
	}
	page, err := client.ListHostedSessions(context.Background(), "mine", "")
	if err != nil || !strings.Contains(service.listQuery, "include=repository") {
		t.Fatalf("query = %q, err = %v", service.listQuery, err)
	}
	if page.Sessions[0].Repository == nil || page.Sessions[0].Repository.GitCommit != strings.Repeat("c", 40) {
		t.Fatalf("descriptor missing: %+v", page.Sessions[0])
	}

	// A hostile service cannot smuggle a command or path through the descriptor.
	service.listed = validDescriptor()
	service.listed.GitBranch = "x;whoami"
	page, err = client.ListHostedSessions(context.Background(), "mine", "")
	if err != nil || page.Sessions[0].Repository != nil {
		t.Fatalf("invalid descriptor survived receipt validation: %+v %v", page.Sessions[0].Repository, err)
	}
}

func TestRepositoryNegotiationResetsWhenServiceRollsBack(t *testing.T) {
	service := &repositoryService{advertise: true}
	client := newRepositoryClient(t, service)
	if _, err := client.HostedCapabilities(context.Background()); err != nil || !client.RepositoryDescriptorSupported() {
		t.Fatalf("support not detected: %v", err)
	}
	service.advertise = false
	if _, err := client.HostedCapabilities(context.Background()); err != nil || client.RepositoryDescriptorSupported() {
		t.Fatalf("stale support after rollback: %v", err)
	}
}
