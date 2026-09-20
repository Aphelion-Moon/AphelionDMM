package sqlite

import (
	"reflect"
	"sync"

	"sdmm/internal/aphelion/collab/engine"
)

// Eligibility bounds serialized history, not exact Go heap usage. Keep only one
// document per store; larger histories retain the uncached validation path.
const recoveryCacheBytes = 4 << 20
const recoveryCacheOperations = 1024

type recoveryCache struct {
	mutex sync.Mutex
	entry *validatedRecovery
}

type validatedRecovery struct {
	state    engine.RecoveryState
	document *engine.Document
}

func (cache *recoveryCache) validate(state engine.RecoveryState) error {
	cache.mutex.Lock()
	matched := cache.entry != nil && reflect.DeepEqual(cache.entry.state, state)
	cache.mutex.Unlock()
	// Load has read every durable row. An exact match needs no second engine
	// reconstruction and must not take ownership of the append candidate.
	if matched {
		return nil
	}
	_, err := state.Restore()
	return err
}

func (cache *recoveryCache) restore(state engine.RecoveryState) (*engine.Document, error) {
	cache.mutex.Lock()
	entry := cache.entry
	cache.entry = nil
	cache.mutex.Unlock()
	// The caller has read every durable row in its current transaction. A head
	// revision alone cannot detect an external writer or changed retained data.
	if entry != nil && reflect.DeepEqual(entry.state, state) {
		// Taking the entry transfers exclusive ownership, so a failed append
		// cannot leave a mutated candidate available to a later request.
		return entry.document, nil
	}
	return state.Restore()
}

func (cache *recoveryCache) retain(state engine.RecoveryState, document *engine.Document, encodedBytes int) {
	var entry *validatedRecovery
	if encodedBytes <= recoveryCacheBytes && len(state.Operations) <= recoveryCacheOperations {
		entry = &validatedRecovery{state: state, document: document}
	}
	cache.mutex.Lock()
	cache.entry = entry
	cache.mutex.Unlock()
}

func (cache *recoveryCache) clear() {
	cache.mutex.Lock()
	cache.entry = nil
	cache.mutex.Unlock()
}
