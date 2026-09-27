package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/server"
)

func TestControllerCreateLocalRedeemsTokenAndLeaves(t *testing.T) {
	t.Parallel()

	snapshot := controllerSnapshot(t)
	service := &fakeEmbeddedService{endpoint: "http://127.0.0.1:1234", token: "launch-secret"}
	client := &fakeCollaborationClient{}
	controller := NewController(func(context.Context, model.Snapshot) (EmbeddedService, error) { return service, nil }, client)
	if err := controller.CreateLocal(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if service.token != "" || client.createToken != "launch-secret" {
		t.Fatalf("launch token was not redeemed exactly once: service=%q client=%q", service.token, client.createToken)
	}
	if client.joined.Token != "owner-secret" || controller.Invitation().Token != "" {
		t.Fatalf("join token retention = client %q controller %q", client.joined.Token, controller.Invitation().Token)
	}
	if err := controller.Leave(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.leaveCalls != 1 || service.shutdownCalls != 1 || controller.Active() {
		t.Fatalf("leave calls = %d shutdown calls = %d active = %t", client.leaveCalls, service.shutdownCalls, controller.Active())
	}
}

func TestControllerCreateLocalNamedPassesOwnerDisplayName(t *testing.T) {
	t.Parallel()
	snapshot := controllerSnapshot(t)
	service := &fakeEmbeddedService{endpoint: "http://127.0.0.1:1234", token: "launch-secret"}
	client := &fakeCollaborationClient{}
	controller := NewController(func(context.Context, model.Snapshot) (EmbeddedService, error) { return service, nil }, client)
	if err := controller.CreateLocalNamed(context.Background(), snapshot, "Test Owner"); err != nil {
		t.Fatal(err)
	}
	if client.createdName != "Test Owner" {
		t.Fatalf("owner display name = %q, want Test Owner", client.createdName)
	}
}

func TestControllerJoinDoesNotExposeToken(t *testing.T) {
	t.Parallel()

	client := &fakeCollaborationClient{}
	controller := NewController(nil, client)
	invitation := Invitation{BaseURL: "https://example.invalid", Origin: "https://example.invalid", SessionID: "session", Token: "join-secret"}
	if err := controller.Join(context.Background(), invitation); err != nil {
		t.Fatal(err)
	}
	if client.joined.Token != "join-secret" {
		t.Fatal("client did not receive join token")
	}
	if strings.Contains(invitation.String(), "join-secret") || strings.Contains(controller.Invitation().String(), "join-secret") {
		t.Fatal("invitation string exposed token")
	}
}

func TestControllerBeginProjectReplacementRefusesPendingOperations(t *testing.T) {
	t.Parallel()

	client := &fakeCollaborationClient{pending: true}
	controller := NewController(nil, client)
	if err := controller.Join(context.Background(), Invitation{BaseURL: "https://example.invalid", Origin: "https://example.invalid", SessionID: "session", Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.BeginProjectReplacement(); !errors.Is(err, ErrUnacknowledgedOperations) {
		t.Fatalf("begin replacement error = %v, want unacknowledged operations", err)
	}
	if !controller.Active() || client.leaveCalls != 0 {
		t.Fatalf("blocked replacement changed session: active=%t leaves=%d", controller.Active(), client.leaveCalls)
	}
}

func TestControllerCompleteProjectReplacementRevalidatesSession(t *testing.T) {
	t.Parallel()

	client := &fakeCollaborationClient{}
	controller := NewController(nil, client)
	if err := controller.Join(context.Background(), Invitation{BaseURL: "https://example.invalid", Origin: "https://example.invalid", SessionID: "session", Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	permit, err := controller.BeginProjectReplacement()
	if err != nil {
		t.Fatal(err)
	}
	controller.generation++
	if err := controller.CompleteProjectReplacement(context.Background(), permit); !errors.Is(err, ErrSessionChanged) {
		t.Fatalf("complete replacement error = %v, want session changed", err)
	}
	if client.leaveCalls != 0 {
		t.Fatalf("changed session leave calls = %d, want 0", client.leaveCalls)
	}
}

func TestControllerCompleteProjectReplacementLeavesPermittedSession(t *testing.T) {
	t.Parallel()

	client := &fakeCollaborationClient{}
	controller := NewController(nil, client)
	if err := controller.Join(context.Background(), Invitation{BaseURL: "https://example.invalid", Origin: "https://example.invalid", SessionID: "session", Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	permit, err := controller.BeginProjectReplacement()
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.CompleteProjectReplacement(context.Background(), permit); err != nil {
		t.Fatal(err)
	}
	if controller.Active() || client.leaveCalls != 1 {
		t.Fatalf("completed replacement state: active=%t leaves=%d", controller.Active(), client.leaveCalls)
	}
}

func TestControllerCompleteProjectReplacementRechecksPendingOperations(t *testing.T) {
	t.Parallel()

	client := &fakeCollaborationClient{}
	controller := NewController(nil, client)
	if err := controller.Join(context.Background(), Invitation{BaseURL: "https://example.invalid", Origin: "https://example.invalid", SessionID: "session", Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	permit, err := controller.BeginProjectReplacement()
	if err != nil {
		t.Fatal(err)
	}
	client.pending = true
	if err := controller.CompleteProjectReplacement(context.Background(), permit); !errors.Is(err, ErrUnacknowledgedOperations) {
		t.Fatalf("complete replacement error = %v, want unacknowledged operations", err)
	}
	if !controller.Active() || client.leaveCalls != 0 {
		t.Fatalf("pending replacement changed session: active=%t leaves=%d", controller.Active(), client.leaveCalls)
	}
}

func TestControllerCreateLocalWithRealService(t *testing.T) {
	t.Parallel()

	client := NewSessionClient(SessionClientConfig{HTTPTimeout: time.Second})
	controller := NewController(func(ctx context.Context, snapshot model.Snapshot) (EmbeddedService, error) {
		return server.StartEmbedded(ctx, snapshot)
	}, client)
	if err := controller.CreateLocal(context.Background(), controllerSnapshot(t)); err != nil {
		t.Fatal(err)
	}
	if !controller.Active() || client.NetworkExecutor() == nil {
		t.Fatal("real local collaboration session did not become active")
	}
	status := client.Status()
	if status.State != collabclient.StateCaughtUp || status.Role != "owner" || status.SessionID == "" {
		t.Fatalf("real local collaboration status = %#v", status)
	}
	if err := controller.Leave(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestControllerLeaveSupersedesInFlightCreate(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	service := &fakeEmbeddedService{endpoint: "http://127.0.0.1:1234", token: "launch-secret"}
	client := &fakeCollaborationClient{}
	controller := NewController(func(context.Context, model.Snapshot) (EmbeddedService, error) {
		close(started)
		<-release
		return service, nil
	}, client)
	snapshot := controllerSnapshot(t)
	createResult := make(chan error, 1)
	go func() {
		createResult <- controller.CreateLocal(context.Background(), snapshot)
	}()
	<-started
	if err := controller.Leave(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !controller.Active() {
		t.Fatal("canceled creation was advertised as idle before its worker finished")
	}
	close(release)
	if err := <-createResult; !errors.Is(err, ErrSessionChanged) {
		t.Fatalf("in-flight create result = %v, want session changed", err)
	}
	if controller.Active() || service.shutdownCalls != 1 {
		t.Fatalf("active = %t, service shutdown calls = %d", controller.Active(), service.shutdownCalls)
	}
	if client.createToken != "" || client.joined.SessionID != "" {
		t.Fatal("canceled startup continued creating or joining a session")
	}
}

func TestControllerLeaveCancelsSetup(t *testing.T) {
	for _, stage := range []string{"startup", "join", "hosted"} {
		t.Run(stage, func(t *testing.T) {
			entered := make(chan context.Context, 1)
			block := func(ctx context.Context) error {
				entered <- ctx
				<-ctx.Done()
				return context.Cause(ctx)
			}
			client := &cancelSetupClient{block: block}
			controller := NewController(func(ctx context.Context, _ model.Snapshot) (EmbeddedService, error) {
				return nil, block(ctx)
			}, client)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			snapshot := controllerSnapshot(t)
			go func() {
				switch stage {
				case "startup":
					result <- controller.CreateLocal(ctx, snapshot)
				case "join":
					result <- controller.Join(ctx, Invitation{BaseURL: "http://localhost", Origin: "http://localhost", SessionID: "session", Token: "secret"})
				case "hosted":
					result <- controller.JoinHosted(ctx, HostedConnection{})
				}
			}()
			setupContext := <-entered
			if err := controller.Leave(context.Background()); err != nil {
				t.Fatal(err)
			}
			select {
			case <-setupContext.Done():
			case <-time.After(time.Second):
				t.Fatal("leaving did not cancel setup work")
			}
			if err := <-result; !errors.Is(err, ErrSessionChanged) {
				t.Fatalf("setup error = %v, want session changed", err)
			}
			if controller.Active() {
				t.Fatal("canceled setup prevented retry")
			}
		})
	}
}

type cancelSetupClient struct {
	fakeCollaborationClient
	block func(context.Context) error
}

func (client *cancelSetupClient) Join(ctx context.Context, _ Invitation) error {
	return client.block(ctx)
}

func (client *cancelSetupClient) JoinHosted(ctx context.Context, _ HostedConnection) error {
	return client.block(ctx)
}

func TestControllerReservesSessionUntilLeaveCompletes(t *testing.T) {
	for _, stage := range []string{"client", "service"} {
		for _, fails := range []bool{false, true} {
			t.Run(stage+map[bool]string{false: "/success", true: "/failure"}[fails], func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				defer close(release)
				var cause error
				if fails {
					cause = errors.New("cleanup failed")
				}
				gate := func() error { close(entered); <-release; return cause }
				service := &fakeEmbeddedService{endpoint: "http://127.0.0.1:1234", token: "launch-secret"}
				client := &fakeCollaborationClient{}
				if stage == "client" {
					client.leaveHook = gate
				} else {
					service.shutdownHook = gate
				}
				controller := NewController(func(context.Context, model.Snapshot) (EmbeddedService, error) { return service, nil }, client)
				if err := controller.CreateLocal(context.Background(), controllerSnapshot(t)); err != nil {
					t.Fatal(err)
				}
				left := make(chan error, 1)
				go func() { left <- controller.Leave(context.Background()) }()
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("leave did not reach cleanup")
				}
				invitation := Invitation{BaseURL: "http://127.0.0.1:1234", Origin: "http://127.0.0.1:1234", SessionID: "new-session", Token: "new-secret"}
				if err := controller.Join(context.Background(), invitation); !errors.Is(err, ErrSessionActive) {
					t.Fatalf("join entered unfinished %s cleanup: %v", stage, err)
				}
				if !controller.Active() {
					t.Fatal("UI considered unfinished cleanup ready for another session")
				}
				release <- struct{}{}
				if err := <-left; !errors.Is(err, cause) {
					t.Fatalf("cleanup error = %v, want %v", err, cause)
				}
				if controller.Active() {
					t.Fatal("completed cleanup retained its reservation")
				}
				client.leaveHook, service.shutdownHook = nil, nil
				if err := controller.Join(context.Background(), invitation); err != nil {
					t.Fatalf("could not join after cleanup: %v", err)
				}
				if client.joined.SessionID != "new-session" {
					t.Fatal("new session was not installed")
				}
				if err := controller.Leave(context.Background()); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func controllerSnapshot(t *testing.T) model.Snapshot {
	t.Helper()
	documentID, err := model.NewDocumentID()
	if err != nil {
		t.Fatal(err)
	}
	return model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: documentID, EnvironmentHash: strings.Repeat("a", 64), MaxX: 1, MaxY: 1, MaxZ: 1}
}

type fakeEmbeddedService struct {
	endpoint      string
	token         string
	shutdownCalls int
	shutdownHook  func() error
}

func (service *fakeEmbeddedService) Endpoint() string { return service.endpoint }

func (service *fakeEmbeddedService) TakeLaunchToken() string {
	token := service.token
	service.token = ""
	return token
}

func (service *fakeEmbeddedService) Shutdown(context.Context) error {
	service.shutdownCalls++
	if service.shutdownHook != nil {
		return service.shutdownHook()
	}
	return nil
}

type fakeCollaborationClient struct {
	createToken string
	createdName string
	joined      Invitation
	leaveCalls  int
	pending     bool
	leaveHook   func() error
}

func (client *fakeCollaborationClient) CreateNamed(_ context.Context, baseURL, launchToken string, _ model.Snapshot, displayName string) (Invitation, error) {
	client.createToken = launchToken
	client.createdName = displayName
	return Invitation{BaseURL: baseURL, Origin: baseURL, SessionID: "session", Token: "owner-secret"}, nil
}

func (client *fakeCollaborationClient) Create(_ context.Context, baseURL, launchToken string, _ model.Snapshot) (Invitation, error) {
	client.createToken = launchToken
	return Invitation{BaseURL: baseURL, Origin: baseURL, SessionID: "session", Token: "owner-secret"}, nil
}

func (client *fakeCollaborationClient) Join(_ context.Context, invitation Invitation) error {
	client.joined = invitation
	return nil
}

func (client *fakeCollaborationClient) Leave(context.Context) error {
	client.leaveCalls++
	if client.leaveHook != nil {
		return client.leaveHook()
	}
	return nil
}

func (client *fakeCollaborationClient) HasUnacknowledgedOperations() bool { return client.pending }

func (client *fakeCollaborationClient) HasRetainedDrafts() bool { return false }
