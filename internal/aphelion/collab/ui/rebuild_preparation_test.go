package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

type pausedRecoveryContext struct {
	context.Context
	entered chan struct{}
	release chan struct{}
}

func (ctx *pausedRecoveryContext) Err() error {
	close(ctx.entered)
	<-ctx.release
	return ctx.Context.Err()
}

func TestSessionConflictRebuildPreparationDoesNotBlockCaller(t *testing.T) {
	client := NewSessionClient(SessionClientConfig{})
	network, err := collabclient.NewNetworkExecutor(&channelSessionTransport{sent: make(chan protocol.ClientEnvelope, 1)}, controllerSnapshot(t), "01890f3e-7b5c-7abc-8def-0123456789ac", "session")
	if err != nil {
		t.Fatal(err)
	}
	client.network = network
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := &pausedRecoveryContext{Context: canceled, entered: make(chan struct{}), release: make(chan struct{})}
	defer close(ctx.release)
	returned, completed := make(chan error, 1), make(chan error, 1)
	go func() {
		returned <- client.RebuildConflict(ctx, "draft", func(_ model.AcceptedOperation, err error) { completed <- err })
	}()
	select {
	case <-ctx.entered:
	case <-time.After(time.Second):
		t.Fatal("preparation did not begin")
	}
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("rebuild preparation blocked its caller")
	}
	ctx.release <- struct{}{}
	select {
	case err := <-completed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("completion error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("preparation failure did not complete")
	}
	if network.HasUnacknowledgedOperations() {
		t.Fatal("canceled preparation submitted work")
	}
}
