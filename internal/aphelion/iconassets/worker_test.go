package iconassets

import (
	"context"
	"sdmm/internal/aphelion/resources"
	"sdmm/third_party/sdmmparser"
	"testing"
	"time"
)

func TestCancelledDecodedHandoffReleasesReservation(t *testing.T) {
	budget := resources.NewFixedBudget(1024)
	reservation, err := budget.Reserve(512)
	if err != nil {
		t.Fatal(err)
	}
	decoded := make(chan struct{})
	w := newWorker(func(context.Context, string) (*Decoded, error) {
		close(decoded)
		return &Decoded{Reservation: reservation}, nil
	})
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	if !w.Submit(Request{Context: ctx, Key: "old", Path: "old"}) {
		t.Fatal("not submitted")
	}
	<-decoded
	cancel()
	deadline := time.Now().Add(time.Second)
	for budget.Used() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if budget.Used() != 0 {
		t.Fatal("cancelled handoff retained decoded memory")
	}
}

func TestMetadataCannotReadPastDecodedImage(t *testing.T) {
	for _, metadata := range []*sdmmparser.IconMetadata{
		nil,
		{Width: 32, Height: 32, States: []*sdmmparser.IconState{{Dirs: 8, Frames: 2}}},
		{Width: 32, Height: 32, States: []*sdmmparser.IconState{{Dirs: 3, Frames: 1}}},
	} {
		if validateMetadata(metadata, 32, 32) == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	if err := validateMetadata(&sdmmparser.IconMetadata{Width: 32, Height: 32, States: []*sdmmparser.IconState{{Dirs: 1, Frames: 1}}}, 32, 32); err != nil {
		t.Fatal(err)
	}
}
