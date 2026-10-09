package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/auth"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/repoinfo"
)

func repositoryTestService(t *testing.T) (*Service, string, model.ActorID) {
	t.Helper()
	now := time.Now().UTC()
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	backend := newFakeHostedBackend(map[string]auth.Session{"owner-token": {Token: "owner-token", ActorID: actorID, Issuer: "https://issuer.example", Subject: "owner", DisplayName: "owner", ExpiresAt: now.Add(time.Hour)}})
	service := NewService(ServiceConfig{HostedAuth: backend, HostedRegistry: backend, Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
	server := httptest.NewServer(service.Handler())
	t.Cleanup(server.Close)
	return service, server.URL, actorID
}

func createRepositorySession(t *testing.T, baseURL string, repository any) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]any{"snapshot": testSnapshot(t, 1), "visibility": "community", "title": "Repo", "repository": repository})
	if err != nil {
		t.Fatal(err)
	}
	return postJSON(t, baseURL+"/v1/hosted/sessions", "owner-token", body)
}

func TestHostedCapabilitiesAdvertiseRepositoryDescriptorOnlyByHeader(t *testing.T) {
	_, baseURL, _ := repositoryTestService(t)
	response := hostedBrowserRequest(t, http.MethodGet, baseURL+"/v1/hosted/capabilities", "", nil)
	defer func() { _ = response.Body.Close() }()
	if response.Header.Get(protocol.RepositoryCapabilityHeader) != protocol.RepositoryCapabilityValue {
		t.Fatalf("capability header = %q", response.Header.Get(protocol.RepositoryCapabilityHeader))
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 2 {
		t.Fatalf("older strict capability decoders require the body to stay unchanged, got %v", body)
	}
}

func TestHostedRepositoryDescriptorIsListedOnlyWhenRequested(t *testing.T) {
	_, baseURL, _ := repositoryTestService(t)
	descriptor := map[string]string{"dme_name": "game.dme", "environment_hash": strings.Repeat("a", 64), "git_branch": "play-test", "git_commit": strings.Repeat("b", 40)}
	response := createRepositorySession(t, baseURL, descriptor)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", response.StatusCode)
	}
	list := func(query string) map[string]any {
		t.Helper()
		response := hostedBrowserRequest(t, http.MethodGet, baseURL+"/v1/hosted/sessions?scope=mine"+query, "owner-token", nil)
		defer func() { _ = response.Body.Close() }()
		var page struct {
			Sessions []map[string]any `json:"sessions"`
		}
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil || len(page.Sessions) != 1 {
			t.Fatalf("listing = %+v, %v", page, err)
		}
		return page.Sessions[0]
	}
	if _, present := list("")["repository"]; present {
		t.Fatal("repository descriptor leaked to a client that did not request it")
	}
	row := list("&include=repository")
	repository, _ := row["repository"].(map[string]any)
	for key, want := range descriptor {
		if repository[key] != want {
			t.Fatalf("repository %s = %v, want %s", key, repository[key], want)
		}
	}
	for key := range repository {
		if _, allowed := descriptor[key]; !allowed {
			t.Fatalf("unexpected descriptor field %q", key)
		}
	}
}

func TestHostedSessionWithoutRepositoryListsUnknown(t *testing.T) {
	_, baseURL, _ := repositoryTestService(t)
	body, _ := json.Marshal(map[string]any{"snapshot": testSnapshot(t, 1)})
	response := postJSON(t, baseURL+"/v1/hosted/sessions", "owner-token", body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("old-client create status = %d", response.StatusCode)
	}
	list := hostedBrowserRequest(t, http.MethodGet, baseURL+"/v1/hosted/sessions?scope=mine&include=repository", "owner-token", nil)
	defer func() { _ = list.Body.Close() }()
	var raw strings.Builder
	buffer := make([]byte, 4096)
	n, _ := list.Body.Read(buffer)
	raw.Write(buffer[:n])
	if strings.Contains(raw.String(), "repository") {
		t.Fatalf("absent descriptor serialized: %s", raw.String())
	}
}

func TestHostedRepositoryDescriptorIsValidatedOnCreate(t *testing.T) {
	_, baseURL, _ := repositoryTestService(t)
	valid := map[string]string{"dme_name": "game.dme", "environment_hash": strings.Repeat("a", 64)}
	for name, mutate := range map[string]func(map[string]string){
		"path": func(d map[string]string) { d["dme_name"] = `C:\work\game.dme` },
		"flag branch": func(d map[string]string) {
			d["git_branch"] = "--upload-pack=x"
			d["git_commit"] = strings.Repeat("b", 40)
		},
		"short commit":  func(d map[string]string) { d["git_commit"] = "abc" },
		"bad hash":      func(d map[string]string) { d["environment_hash"] = "zz" },
		"remote url":    func(d map[string]string) { d["dme_name"] = "https://example.invalid/game.dme" },
		"unknown field": func(d map[string]string) { d["remote_url"] = "https://example.invalid/repo.git" },
	} {
		descriptor := map[string]string{}
		for key, value := range valid {
			descriptor[key] = value
		}
		mutate(descriptor)
		response := createRepositorySession(t, baseURL, descriptor)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, response.StatusCode)
		}
	}
}

func TestHostedCreateWithoutServerSupportStillRejectsUnknownFields(t *testing.T) {
	// Existing strict-decoder behavior stays: only the documented field is new.
	_, baseURL, _ := repositoryTestService(t)
	body, _ := json.Marshal(map[string]any{"snapshot": testSnapshot(t, 1), "git_commit": strings.Repeat("a", 40)})
	response := postJSON(t, baseURL+"/v1/hosted/sessions", "owner-token", body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func repositoryRestartHarness(t *testing.T) (*fakeHostedBackend, *nonClosingSessionStore, func() *httptest.Server) {
	t.Helper()
	now := time.Unix(20_000, 0).UTC()
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	backend := newFakeHostedBackend(map[string]auth.Session{"owner-token": {Token: "owner-token", ActorID: actorID, Issuer: "https://issuer.example", Subject: "owner", DisplayName: "owner", ExpiresAt: now.Add(time.Hour)}})
	durable := &nonClosingSessionStore{SessionStore: NewMemoryStore()}
	start := func() *httptest.Server {
		service := NewService(ServiceConfig{Store: durable, HostedAuth: backend, HostedRegistry: backend, Now: func() time.Time { return now }})
		t.Cleanup(func() { _ = service.Shutdown(context.Background()) })
		if err := service.RecoverHostedSessions(context.Background()); err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(service.Handler())
		t.Cleanup(server.Close)
		return server
	}
	return backend, durable, start
}

func listMineRepository(t *testing.T, baseURL string) map[string]any {
	t.Helper()
	response := hostedBrowserRequest(t, http.MethodGet, baseURL+"/v1/hosted/sessions?scope=mine&include=repository", "owner-token", nil)
	defer func() { _ = response.Body.Close() }()
	var page struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil || len(page.Sessions) != 1 {
		t.Fatalf("listing = %+v, %v", page, err)
	}
	repository, _ := page.Sessions[0]["repository"].(map[string]any)
	return repository
}

func TestHostedRepositoryDescriptorPersistsAcrossRestart(t *testing.T) {
	backend, _, start := repositoryRestartHarness(t)
	first := start()
	descriptor := map[string]string{"dme_name": "game.dme", "environment_hash": strings.Repeat("a", 64), "git_branch": "play-test", "git_commit": strings.Repeat("b", 40)}
	response := createRepositorySession(t, first.URL, descriptor)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", response.StatusCode)
	}
	backend.mutex.Lock()
	var stored *repoinfo.Descriptor
	for _, session := range backend.sessions {
		stored = session.Repository
	}
	backend.mutex.Unlock()
	if stored == nil || stored.GitCommit != descriptor["git_commit"] {
		t.Fatalf("registry did not receive descriptor on create: %#v", stored)
	}
	// A second service over the same registry and store models a restart.
	second := start()
	repository := listMineRepository(t, second.URL)
	for key, want := range descriptor {
		if repository[key] != want {
			t.Fatalf("recovered repository %s = %v, want %s", key, repository[key], want)
		}
	}
}

func TestHostedRecoveredInvalidRepositoryDescriptorIsUnknown(t *testing.T) {
	backend, _, start := repositoryRestartHarness(t)
	first := start()
	response := createRepositorySession(t, first.URL, map[string]string{"dme_name": "game.dme", "environment_hash": strings.Repeat("a", 64)})
	_ = response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", response.StatusCode)
	}
	backend.mutex.Lock()
	for id, session := range backend.sessions {
		session.Repository = &repoinfo.Descriptor{DMEName: `C:\work\game.dme`, EnvironmentHash: "zz"}
		backend.sessions[id] = session
	}
	backend.mutex.Unlock()
	second := start()
	if repository := listMineRepository(t, second.URL); repository != nil {
		t.Fatalf("invalid stored descriptor was listed: %#v", repository)
	}
}
