package envcache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheValidatesConsumedBytesAndNegativeProbes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "project.dme")
	missing := filepath.Join(root, "optional.dm")
	data := []byte("#define VALUE 1\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	trace := Trace{SchemaVersion: 1, Complete: true, WorkingDir: cwd, ConfigMode: "default-no-autodetect", Accesses: []Access{{Kind: "read", Path: path, ResolvedPath: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}}, Probes: []Probe{{Kind: "include_candidate", Path: missing, ResolvedPath: missing, Exists: false}}}
	cache := New(privateTestDirectory(t))
	if err := cache.Put(context.Background(), path, "test-parser", []byte("{ \"path\":\"<area>&\" }"), trace); err != nil {
		t.Fatal(err)
	}
	entry, err := cache.Load(context.Background(), path, "test-parser")
	if err != nil {
		t.Fatal(err)
	}
	entry.Close()
	storedPath := filepath.Join(cache.Directory, key(path, "test-parser", cwd))
	encoded, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	var stored envelope
	if err = json.Unmarshal(encoded, &stored); err != nil {
		t.Fatal(err)
	}
	stored.Trace.Probes = nil
	var modified bytes.Buffer
	encoder := json.NewEncoder(&modified)
	encoder.SetEscapeHTML(false)
	if err = encoder.Encode(stored); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(storedPath, modified.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = cache.Load(context.Background(), path, "test-parser"); err == nil {
		t.Fatal("corrupt dependency manifest accepted")
	}
	if err = os.WriteFile(storedPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Load(context.Background(), path, "changed-parser"); err == nil {
		t.Fatal("parser identity drift accepted")
	}
	if err := os.WriteFile(missing, []byte("present"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Load(context.Background(), path, "test-parser"); err == nil {
		t.Fatal("negative probe drift accepted")
	}
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if err := os.WriteFile(path, []byte("#define VALUE 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Load(context.Background(), path, "test-parser"); err == nil {
		t.Fatal("timestamp-preserving content replacement accepted")
	}
}

func TestCacheCollectsAbandonedStagesAndPreservesActiveStages(t *testing.T) {
	c := New(privateTestDirectory(t))
	old := filepath.Join(c.Directory, ".envcache-123.tmp")
	active := filepath.Join(c.Directory, ".envcache-456.tmp")
	for _, path := range []string{old, active} {
		if err := os.WriteFile(path, []byte("stage"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	yesterday := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(old, yesterday, yesterday); err != nil {
		t.Fatal(err)
	}
	if err := c.prune(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("abandoned stage survived", err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatal("active writer stage removed", err)
	}
	if err := c.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatal("clear disrupted active writer", err)
	}
}

func TestCacheRejectsIncompleteOrEscapingTrace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "project.dme")
	if err := os.WriteFile(path, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	cache := New(privateTestDirectory(t))
	trace := Trace{SchemaVersion: 1, Complete: false, WorkingDir: cwd, ConfigMode: "default-no-autodetect"}
	if err := cache.Put(context.Background(), path, "parser", []byte("{}"), trace); err == nil {
		t.Fatal("incomplete trace stored")
	}
	trace.Complete = true
	trace.Accesses = []Access{{Kind: "read", Path: filepath.Join(t.TempDir(), "outside"), ResolvedPath: filepath.Join(t.TempDir(), "outside"), SHA256: fmt.Sprintf("%x", sha256.Sum256(nil))}}
	if err := cache.Put(context.Background(), path, "parser", []byte("{}"), trace); err == nil {
		t.Fatal("escaping cache read accepted")
	}
}

func privateTestDirectory(t *testing.T) string {
	t.Helper()
	root, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	path, err := os.MkdirTemp(root, "aphelion-cache-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(path); err != nil {
			t.Error(err)
		}
	})
	return path
}

func TestCacheFailureAndWriterContentionPreservePublishedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.dme")
	data := []byte("/obj/cache_fixture\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	trace := Trace{SchemaVersion: 1, Complete: true, WorkingDir: cwd, ConfigMode: "default-no-autodetect", Accesses: []Access{{Kind: "read", Path: path, ResolvedPath: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}}}
	c := New(privateTestDirectory(t))
	put := func(ctx context.Context) error { return c.Put(ctx, path, "test", []byte("{}"), trace) }
	if err := put(context.Background()); err != nil {
		t.Fatal(err)
	}
	published := filepath.Join(c.Directory, key(path, "test", cwd))
	before, err := os.ReadFile(published)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := put(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled publication", err)
	}
	unlock, err := storageLock(filepath.Join(c.Directory, ".writer.lock"))
	if err != nil {
		t.Fatal(err)
	}
	err = put(context.Background())
	unlock()
	if err == nil {
		t.Fatal("concurrent writer lock ignored")
	}
	after, err := os.ReadFile(published)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed writer replaced record", err)
	}
	if err := os.WriteFile(published, before[:len(before)/2], 0600); err != nil {
		t.Fatal(err)
	}
	if entry, err := c.Load(context.Background(), path, "test"); err == nil {
		entry.Close()
		t.Fatal("truncated record restored")
	}
	if err := put(context.Background()); err != nil {
		t.Fatal("could not recover corrupt record", err)
	}
	blocked := New(filepath.Join(published, "not-a-directory"))
	if err := blocked.Put(context.Background(), path, "test", []byte("{}"), trace); err == nil {
		t.Fatal("unavailable storage accepted")
	}
	entry, err := c.Load(context.Background(), path, "test")
	if err != nil {
		t.Fatal("published record unavailable after failed storage attempt", err)
	}
	entry.Close()
}
