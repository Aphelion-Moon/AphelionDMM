// Package configstore persists owned JSON snapshots without truncating the last
// valid configuration. Callers capture live settings on their owning thread.
package configstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

func Write(path string, data []byte) error {
	if !json.Valid(data) {
		return errors.New("invalid configuration JSON")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return replaceFile(f.Name(), path)
}

// Writer coalesces only superseded configuration snapshots, never editor edits.
// At most one pending snapshot per registered path is retained behind the write
// in progress. Close flushes the latest snapshots and joins the worker.
type Writer struct {
	mu      sync.Mutex
	pending map[string][]byte
	closed  bool
	wake    chan struct{}
	done    chan struct{}
	onError func(string, error)
}

func NewWriter(onError func(string, error)) *Writer {
	w := &Writer{pending: make(map[string][]byte), wake: make(chan struct{}, 1), done: make(chan struct{}), onError: onError}
	go w.run()
	return w
}

func (w *Writer) Submit(path string, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("configuration writer is closed")
	}
	w.pending[path] = bytes.Clone(data)
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return nil
}

func (w *Writer) Close() {
	w.mu.Lock()
	w.closed = true
	select {
	case w.wake <- struct{}{}:
	default:
	}
	w.mu.Unlock()
	<-w.done
}

func (w *Writer) run() {
	defer close(w.done)
	last := make(map[string][]byte)
	for range w.wake {
		w.mu.Lock()
		batch, closed := w.pending, w.closed
		w.pending = make(map[string][]byte)
		w.mu.Unlock()
		for path, data := range batch {
			if bytes.Equal(last[path], data) {
				// Another process may share this configuration directory.
				if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
					continue
				}
			}
			if err := Write(path, data); err != nil {
				if w.onError != nil {
					w.onError(path, err)
				}
			} else {
				last[path] = data
			}
		}
		// Once closed, no producer can add work beyond this detached batch.
		if closed {
			return
		}
	}
}
