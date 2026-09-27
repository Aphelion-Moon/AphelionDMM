package server

import (
	"context"
	"sdmm/internal/aphelion/collab/model"
	"testing"
	"time"
)

func TestHostedActivityTracksUniqueLiveConnections(t *testing.T) {
	now := time.Now()
	service := NewService(ServiceConfig{Now: func() time.Time { return now }})
	defer func() { _ = service.Shutdown(context.Background()) }()
	actor, _ := model.NewActorID()
	first := service.trackHostedConnection("session", actor, now.Add(time.Hour))
	second := service.trackHostedConnection("session", actor, now.Add(time.Hour))
	if service.hostedParticipantCount("session") != 1 {
		t.Fatal("duplicate actor counted twice")
	}
	first()
	first()
	if service.hostedParticipantCount("session") != 1 {
		t.Fatal("old connection cleared new connection")
	}
	now = now.Add(2 * time.Hour)
	if service.hostedParticipantCount("session") != 0 {
		t.Fatal("expired connection advertised")
	}
	second()
	if len(service.hostedConnections) != 0 {
		t.Fatal("connection cleanup leaked")
	}
}
