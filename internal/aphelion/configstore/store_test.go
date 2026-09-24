package configstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicConfigFailurePreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, []byte(`{"value":"old"}`)); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte(`invalid`)); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != `{"value":"old"}` {
		t.Fatal("failed save replaced previous configuration", string(data), err)
	}
	if err := Write(path, []byte(`{"value":"new"}`)); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || !json.Valid(data) || string(data) != `{"value":"new"}` {
		t.Fatal("valid save failed", string(data), err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("staged files leaked")
	}
}

func TestWriterOwnsSnapshotsAndFlushesLatestPerConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	errors := make(chan error, 20)
	w := NewWriter(func(_ string, err error) { errors <- err })
	data := []byte(`{"value":"first"}`)
	if err := w.Submit(path, data); err != nil {
		t.Fatal(err)
	}
	for i := range data {
		data[i] = 'x'
	}
	for i := 0; i < 10; i++ {
		if err := w.Submit(path, []byte(`{"value":"last"}`)); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
	result, err := os.ReadFile(path)
	if err != nil || string(result) != `{"value":"last"}` {
		t.Fatal("shutdown lost latest snapshot", string(result), err)
	}
	if err := w.Submit(path, []byte(`{}`)); err == nil {
		t.Fatal("closed writer accepted work")
	}
}
