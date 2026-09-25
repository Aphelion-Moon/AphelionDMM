package mapping

import (
	"sync"
)

const (
	maxReusableSources     = 32
	maxReusableSourceBytes = 256 << 20
	maxReusableConfigs     = 128
	maxReusableConfigBytes = 8 << 20
)

// ReuseCache retains immutable, fully validated disk parses across catalogues.
// Catalogues borrow sources; closing the cache fences future reuse immediately
// and lets outstanding catalogues release their own references normally.
type ReuseCache struct {
	mu sync.Mutex

	closed      bool
	clock       uint64
	sources     map[reuseSourceKey]*reuseSourceEntry
	sourceBytes uint64
	configs     map[reuseConfigKey]*reuseConfigEntry
	configBytes uint64

	// Package-local counters support focused reuse/invalidation regressions.
	sourceHits, sourceMisses int
	configHits, configMisses int
}

type reuseSourceKey struct {
	path, environmentHash string
}

type reuseSourceEntry struct {
	source   *Source
	refs     int
	retained bool
	bytes    uint64
	lastUsed uint64
}

type reuseSourceLease struct {
	cache *ReuseCache
	entry *reuseSourceEntry
	once  sync.Once
}

type reuseConfigKey struct {
	path, environmentHash string
}

type reuseConfigData struct {
	hash      string
	Directory string
	Rooms     map[string]struct{ Modules []string }
}

type reuseConfigEntry struct {
	data     *reuseConfigData
	bytes    uint64
	lastUsed uint64
}

// NewReuseCache creates a bounded cache owned by one inspector/session.
func NewReuseCache() *ReuseCache {
	return &ReuseCache{
		sources: make(map[reuseSourceKey]*reuseSourceEntry),
		configs: make(map[reuseConfigKey]*reuseConfigEntry),
	}
}

// Close releases cache-owned reservations without waiting for in-flight
// parsing or catalogues that still borrow a source.
func (cache *ReuseCache) Close() {
	if cache == nil {
		return
	}
	var closeSources []*Source
	cache.mu.Lock()
	if cache.closed {
		cache.mu.Unlock()
		return
	}
	cache.closed = true
	for key, entry := range cache.sources {
		delete(cache.sources, key)
		entry.retained = false
		if entry.refs == 0 {
			closeSources = append(closeSources, entry.source)
		}
	}
	cache.sourceBytes = 0
	cache.configs = nil
	cache.configBytes = 0
	cache.mu.Unlock()
	for _, source := range closeSources {
		source.Close()
	}
}

func (cache *ReuseCache) acquireSource(path, environmentHash string) (*Source, *reuseSourceLease) {
	if cache == nil {
		return nil, nil
	}
	key := reuseSourceKey{path: sourceKey(path), environmentHash: environmentHash}
	cache.mu.Lock()
	entry := cache.sources[key]
	if cache.closed || entry == nil {
		if !cache.closed {
			cache.sourceMisses++
		}
		cache.mu.Unlock()
		return nil, nil
	}
	cache.clock++
	entry.lastUsed = cache.clock
	entry.refs++
	lease := &reuseSourceLease{cache: cache, entry: entry}
	cache.mu.Unlock()

	// Check performs a full content hash and filesystem identity check. No
	// metadata-only freshness shortcut is allowed for a reusable source.
	if err := entry.source.CheckFresh(); err != nil {
		cache.invalidateSource(key, entry)
		lease.release()
		cache.mu.Lock()
		cache.sourceMisses++
		cache.mu.Unlock()
		return nil, nil
	}
	cache.mu.Lock()
	cache.sourceHits++
	cache.mu.Unlock()
	return entry.source, lease
}

func (cache *ReuseCache) publishSource(source *Source) (*Source, *reuseSourceLease) {
	if cache == nil || source == nil || source.Identity.DocumentID != "" {
		return nil, nil
	}
	bytes := uint64(0)
	if source.lease != nil {
		bytes = source.lease.Bytes()
	}
	if bytes == 0 || bytes > maxReusableSourceBytes || source.CheckFresh() != nil {
		return nil, nil
	}
	key := reuseSourceKey{path: sourceKey(source.Identity.Path), environmentHash: source.Identity.EnvironmentHash}
	for {
		cache.mu.Lock()
		if cache.closed {
			cache.mu.Unlock()
			return nil, nil
		}
		if existing := cache.sources[key]; existing != nil {
			cache.clock++
			existing.lastUsed = cache.clock
			existing.refs++
			lease := &reuseSourceLease{cache: cache, entry: existing}
			cache.mu.Unlock()
			if existing.source.CheckFresh() == nil {
				source.Close()
				cache.mu.Lock()
				cache.sourceHits++
				cache.mu.Unlock()
				return existing.source, lease
			}
			cache.invalidateSource(key, existing)
			lease.release()
			continue
		}
		cache.clock++
		entry := &reuseSourceEntry{source: source, refs: 1, retained: true, bytes: bytes, lastUsed: cache.clock}
		cache.sources[key] = entry
		cache.sourceBytes += bytes
		lease := &reuseSourceLease{cache: cache, entry: entry}
		toClose := cache.trimSourcesLocked()
		cache.mu.Unlock()
		for _, evicted := range toClose {
			evicted.Close()
		}
		return source, lease
	}
}

func (cache *ReuseCache) invalidateSource(key reuseSourceKey, entry *reuseSourceEntry) {
	var closeSource *Source
	cache.mu.Lock()
	if cache.sources[key] == entry {
		delete(cache.sources, key)
		entry.retained = false
		if entry.bytes <= cache.sourceBytes {
			cache.sourceBytes -= entry.bytes
		} else {
			cache.sourceBytes = 0
		}
	}
	if !entry.retained && entry.refs == 0 {
		closeSource = entry.source
	}
	cache.mu.Unlock()
	if closeSource != nil {
		closeSource.Close()
	}
}

func (lease *reuseSourceLease) release() {
	if lease == nil || lease.cache == nil || lease.entry == nil {
		return
	}
	var closeSource *Source
	lease.once.Do(func() {
		cache := lease.cache
		cache.mu.Lock()
		if lease.entry.refs > 0 {
			lease.entry.refs--
		}
		if !lease.entry.retained && lease.entry.refs == 0 {
			closeSource = lease.entry.source
		}
		cache.mu.Unlock()
	})
	if closeSource != nil {
		closeSource.Close()
	}
}

func (cache *ReuseCache) trimSourcesLocked() []*Source {
	var closeSources []*Source
	for len(cache.sources) > maxReusableSources || cache.sourceBytes > maxReusableSourceBytes {
		var victimKey reuseSourceKey
		var victim *reuseSourceEntry
		for key, entry := range cache.sources {
			if victim == nil || entry.lastUsed < victim.lastUsed || (entry.lastUsed == victim.lastUsed && entry.refs == 0 && victim.refs != 0) {
				victimKey, victim = key, entry
			}
		}
		if victim == nil {
			break
		}
		delete(cache.sources, victimKey)
		victim.retained = false
		if victim.bytes <= cache.sourceBytes {
			cache.sourceBytes -= victim.bytes
		} else {
			cache.sourceBytes = 0
		}
		if victim.refs == 0 {
			closeSources = append(closeSources, victim.source)
		}
	}
	return closeSources
}

func (cache *ReuseCache) cachedConfig(path, environmentHash, contentHash string) *reuseConfigData {
	if cache == nil {
		return nil
	}
	key := reuseConfigKey{path: sourceKey(path), environmentHash: environmentHash}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.closed {
		return nil
	}
	entry := cache.configs[key]
	if entry == nil || entry.data.hash != contentHash {
		cache.configMisses++
		return nil
	}
	cache.clock++
	entry.lastUsed = cache.clock
	cache.configHits++
	return entry.data
}

func (cache *ReuseCache) publishConfig(path, environmentHash string, data *reuseConfigData, bytes uint64) *reuseConfigData {
	if cache == nil || data == nil || bytes > maxReusableConfigBytes {
		return data
	}
	key := reuseConfigKey{path: sourceKey(path), environmentHash: environmentHash}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.closed {
		return data
	}
	if existing := cache.configs[key]; existing != nil {
		if existing.data.hash == data.hash {
			cache.clock++
			existing.lastUsed = cache.clock
			return existing.data
		}
		if existing.bytes <= cache.configBytes {
			cache.configBytes -= existing.bytes
		} else {
			cache.configBytes = 0
		}
		delete(cache.configs, key)
	}
	cache.clock++
	cache.configs[key] = &reuseConfigEntry{data: data, bytes: bytes, lastUsed: cache.clock}
	cache.configBytes += bytes
	for len(cache.configs) > maxReusableConfigs || cache.configBytes > maxReusableConfigBytes {
		var victimKey reuseConfigKey
		var victim *reuseConfigEntry
		for candidateKey, entry := range cache.configs {
			if victim == nil || entry.lastUsed < victim.lastUsed {
				victimKey, victim = candidateKey, entry
			}
		}
		if victim == nil {
			break
		}
		delete(cache.configs, victimKey)
		if victim.bytes <= cache.configBytes {
			cache.configBytes -= victim.bytes
		} else {
			cache.configBytes = 0
		}
	}
	return data
}
