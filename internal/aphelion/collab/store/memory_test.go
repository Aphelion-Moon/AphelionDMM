package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func TestMemoryStoreConformance(t *testing.T) {
	t.Parallel()

	fixture, err := NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyConformance(context.Background(), func() (SessionStore, error) {
		return NewMemoryStore(), nil
	}, fixture); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryTransactionVersionIsExplicitAndMonotonic(t *testing.T) {
	fixture, err := NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value := NewMemoryStore()
	if err := value.Create(context.Background(), fixture.Initial); err != nil {
		t.Fatal(err)
	}
	version, err := value.TransactionVersion(context.Background(), fixture.Initial.DocumentID)
	if err != nil || version != LegacyTransactionVersion {
		t.Fatalf("default transaction version = %d, %v; want %d", version, err, LegacyTransactionVersion)
	}
	if err := value.Append(context.Background(), fixture.First); err != nil {
		t.Fatal(err)
	}
	if err := value.ConfigureTransactions(context.Background(), fixture.Initial.DocumentID, BulkTransactionVersion); err != nil {
		t.Fatal(err)
	}
	version, err = value.TransactionVersion(context.Background(), fixture.Initial.DocumentID)
	if err != nil || version != BulkTransactionVersion {
		t.Fatalf("upgraded transaction version = %d, %v; want %d", version, err, BulkTransactionVersion)
	}
	if err := value.ConfigureTransactions(context.Background(), fixture.Initial.DocumentID, LegacyTransactionVersion); err == nil {
		t.Fatal("transaction version downgrade succeeded")
	}
	loaded, operations, err := value.Load(context.Background(), fixture.Initial.DocumentID)
	loadedHash, loadedHashErr := loaded.Hash()
	initialHash, initialHashErr := fixture.Initial.Hash()
	if err != nil || loadedHashErr != nil || initialHashErr != nil || loadedHash != initialHash || len(operations) != 1 || !reflect.DeepEqual(operations[0], fixture.First) {
		t.Fatalf("version upgrade changed existing data: snapshot hash=%q want=%q operations=%#v wantOperation=%#v errors=%v/%v/%v", loadedHash, initialHash, operations, fixture.First, err, loadedHashErr, initialHashErr)
	}
	if _, err := value.TransactionVersion(context.Background(), model.DocumentID("missing")); !errors.Is(err, ErrSessionMissing) {
		t.Fatalf("missing transaction version error = %v, want %v", err, ErrSessionMissing)
	}
}
