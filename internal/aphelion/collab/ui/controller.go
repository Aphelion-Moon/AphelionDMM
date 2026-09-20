package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

var (
	ErrSessionActive            = errors.New("a collaboration session is already active")
	ErrSessionChanged           = errors.New("collaboration session changed while confirmation was open")
	ErrUnacknowledgedOperations = errors.New("collaboration operations are awaiting acknowledgement")
	ErrRetainedDrafts           = errors.New("collaboration drafts remain: inspect or export them in the Collaboration panel, then rebuild or explicitly discard them before leaving")
	ErrReconnectInProgress      = errors.New("collaboration reconnect is already in progress")
)

type ProjectReplacementPermit struct {
	generation uint64
	active     bool
}

type Invitation struct {
	BaseURL        string    `json:"base_url"`
	Origin         string    `json:"origin"`
	SessionID      string    `json:"session_id"`
	Token          string    `json:"-"`
	TokenExpiresAt time.Time `json:"-"`
	Hosted         bool      `json:"hosted,omitempty"`
}

func (invitation Invitation) String() string {
	return fmt.Sprintf("collaboration invitation %s at %s", invitation.SessionID, invitation.BaseURL)
}

func (invitation Invitation) validate() error {
	if invitation.BaseURL == "" || invitation.Origin == "" || invitation.SessionID == "" || invitation.Token == "" {
		return fmt.Errorf("collaboration invitation is incomplete")
	}
	if invitation.Hosted {
		if invitation.Origin != invitation.BaseURL {
			return fmt.Errorf("hosted collaboration invitation origin is invalid")
		}
		if err := validateHostedEndpoint(invitation.BaseURL); err != nil {
			return err
		}
	}
	return nil
}

func validateHostedEndpoint(value string) error {
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return fmt.Errorf("hosted collaboration service address is invalid")
	}
	if endpoint.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(endpoint.Hostname())
	if endpoint.Scheme == "http" && (strings.EqualFold(endpoint.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return fmt.Errorf("hosted collaboration service requires HTTPS")
}

type EmbeddedService interface {
	Endpoint() string
	TakeLaunchToken() string
	Shutdown(context.Context) error
}

type EmbeddedStarter func(context.Context, model.Snapshot) (EmbeddedService, error)

type CollaborationClient interface {
	Create(context.Context, string, string, model.Snapshot) (Invitation, error)
	Join(context.Context, Invitation) error
	Leave(context.Context) error
	HasUnacknowledgedOperations() bool
	HasRetainedDrafts() bool
}

type NamedCollaborationClient interface {
	CreateNamed(context.Context, string, string, model.Snapshot, string) (Invitation, error)
}

type Controller struct {
	mutex      sync.Mutex
	start      EmbeddedStarter
	client     CollaborationClient
	service    EmbeddedService
	invitation Invitation
	active     bool
	inflight   bool
	generation uint64
}

func NewController(start EmbeddedStarter, client CollaborationClient) *Controller {
	return &Controller{start: start, client: client}
}

func (controller *Controller) CreateLocal(ctx context.Context, snapshot model.Snapshot) error {
	return controller.createLocal(ctx, snapshot, "Owner")
}

func (controller *Controller) CreateLocalNamed(ctx context.Context, snapshot model.Snapshot, displayName string) error {
	return controller.createLocal(ctx, snapshot, displayName)
}

func (controller *Controller) createLocal(ctx context.Context, snapshot model.Snapshot, displayName string) error {
	reservation, err := controller.reserve()
	if err != nil {
		return err
	}
	if controller.start == nil {
		controller.releaseReservation(reservation)
		return fmt.Errorf("embedded collaboration starter is unavailable")
	}
	service, err := controller.start(ctx, snapshot)
	if err != nil {
		controller.releaseReservation(reservation)
		return err
	}
	launchToken := service.TakeLaunchToken()
	if launchToken == "" {
		_ = service.Shutdown(context.Background())
		controller.releaseReservation(reservation)
		return fmt.Errorf("embedded collaboration launch token is unavailable")
	}
	var invitation Invitation
	if named, ok := controller.client.(NamedCollaborationClient); ok {
		invitation, err = named.CreateNamed(ctx, service.Endpoint(), launchToken, snapshot, displayName)
	} else {
		invitation, err = controller.client.Create(ctx, service.Endpoint(), launchToken, snapshot)
	}
	if err != nil {
		_ = service.Shutdown(context.Background())
		controller.releaseReservation(reservation)
		return err
	}
	if err := invitation.validate(); err != nil {
		_ = service.Shutdown(context.Background())
		controller.releaseReservation(reservation)
		return err
	}
	if err := controller.client.Join(ctx, invitation); err != nil {
		_ = service.Shutdown(context.Background())
		controller.releaseReservation(reservation)
		return err
	}
	if !controller.activate(reservation, invitation, service) {
		_ = controller.client.Leave(context.Background())
		_ = service.Shutdown(context.Background())
		controller.releaseReservation(reservation)
		return ErrSessionChanged
	}
	return nil
}

func (controller *Controller) Join(ctx context.Context, invitation Invitation) error {
	if err := invitation.validate(); err != nil {
		return err
	}
	reservation, err := controller.reserve()
	if err != nil {
		return err
	}
	if err := controller.client.Join(ctx, invitation); err != nil {
		controller.releaseReservation(reservation)
		return err
	}
	if !controller.activate(reservation, invitation, nil) {
		_ = controller.client.Leave(context.Background())
		controller.releaseReservation(reservation)
		return ErrSessionChanged
	}
	return nil
}

func (controller *Controller) Leave(ctx context.Context) error {
	return controller.leave(ctx, 0, false)
}

func (controller *Controller) BeginProjectReplacement() (ProjectReplacementPermit, error) {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	if !controller.active {
		return ProjectReplacementPermit{}, nil
	}
	if controller.inflight {
		return ProjectReplacementPermit{}, ErrSessionChanged
	}
	if controller.client.HasUnacknowledgedOperations() {
		return ProjectReplacementPermit{}, ErrUnacknowledgedOperations
	}
	if controller.client.HasRetainedDrafts() {
		return ProjectReplacementPermit{}, ErrRetainedDrafts
	}
	return ProjectReplacementPermit{generation: controller.generation, active: true}, nil
}

func (controller *Controller) CompleteProjectReplacement(ctx context.Context, permit ProjectReplacementPermit) error {
	if !permit.active {
		return nil
	}
	return controller.leave(ctx, permit.generation, true)
}

func (controller *Controller) leave(ctx context.Context, generation uint64, revalidate bool) error {
	controller.mutex.Lock()
	if !controller.active {
		controller.mutex.Unlock()
		if revalidate {
			return ErrSessionChanged
		}
		return nil
	}
	if revalidate && (controller.inflight || controller.generation != generation) {
		controller.mutex.Unlock()
		return ErrSessionChanged
	}
	if revalidate && controller.client.HasUnacknowledgedOperations() {
		controller.mutex.Unlock()
		return ErrUnacknowledgedOperations
	}
	if revalidate && controller.client.HasRetainedDrafts() {
		controller.mutex.Unlock()
		return ErrRetainedDrafts
	}
	service := controller.service
	controller.active = false
	controller.service = nil
	controller.invitation = Invitation{}
	controller.generation++
	controller.mutex.Unlock()

	clientErr := controller.client.Leave(ctx)
	var serviceErr error
	if service != nil {
		serviceErr = service.Shutdown(ctx)
	}
	return errors.Join(clientErr, serviceErr)
}

func (controller *Controller) Active() bool {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	return controller.active
}

func (controller *Controller) Invitation() Invitation {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	invitation := controller.invitation
	invitation.Token = ""
	return invitation
}

func (controller *Controller) reserve() (uint64, error) {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	if controller.active || controller.inflight {
		return 0, ErrSessionActive
	}
	controller.active = true
	controller.inflight = true
	controller.generation++
	return controller.generation, nil
}

func (controller *Controller) releaseReservation(reservation uint64) {
	controller.mutex.Lock()
	if controller.generation == reservation {
		controller.active = false
		controller.generation++
	}
	controller.inflight = false
	controller.mutex.Unlock()
}

func (controller *Controller) activate(reservation uint64, invitation Invitation, service EmbeddedService) bool {
	invitation.Token = ""
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	if !controller.active || controller.generation != reservation {
		return false
	}
	controller.invitation = invitation
	controller.service = service
	controller.inflight = false
	return true
}
