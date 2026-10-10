package store

import (
	"sync"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

// maxVerifiedHeads bounds how many documents keep a verified engine head in
// memory. Each is a full engine document, so the bound is small; a document
// without an entry takes the full recovery path.
const maxVerifiedHeads = 8

// HeadKey is the durable head an engine document was verified and committed
// at. Stores read it inside the append transaction, so a match proves no other
// writer appended and no snapshot was published since the store committed it.
type HeadKey struct {
	SnapshotRevision model.Revision
	Revision         model.Revision
	Hash             string
}

// VerifiedHeads lets Append verify one new operation against the document a
// store last committed, instead of reloading and replaying the session's whole
// durable history on every append. Any mismatch, failed append or restart
// falls back to full recovery, which still validates every retained row.
type VerifiedHeads struct {
	mutex   sync.Mutex
	entries map[model.DocumentID]verifiedHead
}

type verifiedHead struct {
	key      HeadKey
	document *engine.Document
}

// Take transfers exclusive ownership of the document verified at key, or
// returns nil. Either way the entry is gone, so a failed append can never
// leave a mutated candidate for a later one.
func (heads *VerifiedHeads) Take(documentID model.DocumentID, key HeadKey) *engine.Document {
	heads.mutex.Lock()
	defer heads.mutex.Unlock()
	entry, found := heads.entries[documentID]
	delete(heads.entries, documentID)
	if !found || entry.key != key {
		return nil
	}
	return entry.document
}

// Put records document as committed at key. The caller gives up the document.
func (heads *VerifiedHeads) Put(documentID model.DocumentID, key HeadKey, document *engine.Document) {
	heads.mutex.Lock()
	defer heads.mutex.Unlock()
	if heads.entries == nil {
		heads.entries = make(map[model.DocumentID]verifiedHead)
	}
	if _, exists := heads.entries[documentID]; !exists && len(heads.entries) >= maxVerifiedHeads {
		for evicted := range heads.entries {
			delete(heads.entries, evicted)
			break
		}
	}
	heads.entries[documentID] = verifiedHead{key: key, document: document}
}

// Rekey moves a cached head from one durable key to another when only the
// published snapshot changed. A snapshot never changes document state or the
// retained revision hashes, so the verified document stays valid.
func (heads *VerifiedHeads) Rekey(documentID model.DocumentID, from, to HeadKey) {
	heads.mutex.Lock()
	defer heads.mutex.Unlock()
	if entry, found := heads.entries[documentID]; found && entry.key == from {
		entry.key = to
		heads.entries[documentID] = entry
	}
}

// Clear drops every cached head.
func (heads *VerifiedHeads) Clear() {
	heads.mutex.Lock()
	defer heads.mutex.Unlock()
	heads.entries = nil
}

// Len reports how many heads are cached.
func (heads *VerifiedHeads) Len() int {
	heads.mutex.Lock()
	defer heads.mutex.Unlock()
	return len(heads.entries)
}
