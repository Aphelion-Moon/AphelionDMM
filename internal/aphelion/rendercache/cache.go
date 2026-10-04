package rendercache

import (
	"container/list"
	"maps"
	"math"
	"sort"

	"sdmm/internal/app/render/brush"
	"sdmm/internal/app/render/bucket/level/chunk"
)

const (
	maxBytes   = 64 << 20
	maxEntries = 512
	// MaxIndexedUnitsPerEntry bounds selection metadata for unusually dense chunk-layers.
	MaxIndexedUnitsPerEntry = 4096
)

type Key struct {
	Chunk *chunk.Chunk
	Layer uint32
}
type Versions struct{ Chunk, Policy, Appearance uint64 }

// Stats counts cache decisions and static GPU submission uploads.
type Stats struct {
	Hits, Misses, Invalidations, Builds uint64
	UploadBytes                         uint64
	Reuses                              uint64
}

type Entry struct {
	Key           Key
	Versions      Versions
	Submission    *brush.Submission
	unitIDs       []uint64
	indexComplete bool
	bytes         int
	dependencies  *Dependencies
}

func (e *Entry) Matches(v Versions) bool { return e != nil && e.Versions == v }

// IntersectsUnitIDs returns true when a selected ID is in this submission. An
// incomplete index conservatively requests the dynamic path for that layer.
// Values are ignored so callers can borrow their highlight map without copying it.
func IntersectsUnitIDs[T any](e *Entry, ids map[uint64]T) bool {
	if e == nil || len(ids) == 0 {
		return false
	}
	if !e.indexComplete {
		return true
	}
	if len(e.unitIDs) < len(ids) {
		for _, id := range e.unitIDs {
			if _, selected := ids[id]; selected {
				return true
			}
		}
		return false
	}
	for id := range ids {
		index := sort.Search(len(e.unitIDs), func(index int) bool { return e.unitIDs[index] >= id })
		if index < len(e.unitIDs) && e.unitIDs[index] == id {
			return true
		}
	}
	return false
}

type Cache struct {
	entries map[Key]*list.Element
	lru     list.List
	bytes   int
	retired []*brush.Submission
	stats   Stats
}

func New() *Cache                   { return &Cache{entries: make(map[Key]*list.Element)} }
func LayerKey(layer float32) uint32 { return math.Float32bits(layer) }
func (c *Cache) Get(key Key, versions Versions) (*Entry, bool) {
	return c.get(key, versions, nil)
}
func (c *Cache) get(key Key, versions Versions, validate func(*Entry) bool) (*Entry, bool) {
	if c == nil || c.entries == nil {
		return nil, false
	}
	element := c.entries[key]
	if element == nil {
		c.stats.Misses++
		return nil, false
	}
	entry := element.Value.(*Entry)
	if !entry.Matches(versions) && validate != nil && validate(entry) {
		entry.Versions = versions
	}
	if !entry.Matches(versions) {
		c.stats.Misses++
		c.stats.Invalidations++
		c.remove(key, element)
		return nil, false
	}
	c.stats.Hits++
	c.lru.MoveToFront(element)
	return entry, true
}
func (c *Cache) Put(key Key, versions Versions, submission *brush.Submission) bool {
	return c.PutWithUnitIDs(key, versions, submission, nil, true)
}

// PutWithUnitIDs retains a sorted selection index alongside one chunk-layer
// submission. The index shares the cache byte budget and has a hard per-entry cap.
func (c *Cache) PutWithUnitIDs(key Key, versions Versions, submission *brush.Submission, unitIDs []uint64, indexComplete bool) bool {
	return c.put(key, versions, submission, unitIDs, indexComplete, nil)
}

func (c *Cache) PutWithDependencies(key Key, versions Versions, submission *brush.Submission, unitIDs []uint64, indexComplete bool, dependencies Dependencies) bool {
	return c.put(key, versions, submission, unitIDs, indexComplete, &dependencies)
}

func (c *Cache) put(key Key, versions Versions, submission *brush.Submission, unitIDs []uint64, indexComplete bool, dependencies *Dependencies) bool {
	if c == nil || len(unitIDs) > MaxIndexedUnitsPerEntry {
		return false
	}
	unitIDs = append([]uint64(nil), unitIDs...)
	sort.Slice(unitIDs, func(i, j int) bool { return unitIDs[i] < unitIDs[j] })
	bytes := len(unitIDs) * 8
	if dependencies != nil {
		if len(dependencies.Icons) > MaxIndexedUnitsPerEntry {
			return false
		}
		dependencies.Icons = maps.Clone(dependencies.Icons)
		bytes += 32
		for icon := range dependencies.Icons {
			bytes += len(icon) + 64
		}
	}
	if submission != nil {
		bytes += submission.ByteSize()
	}
	if bytes < 0 || bytes > maxBytes {
		return false
	}
	if c.entries == nil {
		c.entries = make(map[Key]*list.Element)
	}
	if old := c.entries[key]; old != nil {
		c.remove(key, old)
	}
	entry := &Entry{Key: key, Versions: versions, Submission: submission, unitIDs: unitIDs, indexComplete: indexComplete, bytes: bytes, dependencies: dependencies}
	element := c.lru.PushFront(entry)
	c.entries[key] = element
	c.bytes += bytes
	for len(c.entries) > maxEntries || c.bytes > maxBytes {
		last := c.lru.Back()
		if last == nil {
			break
		}
		c.remove(last.Value.(*Entry).Key, last)
	}
	return c.entries[key] == element
}
func (c *Cache) remove(key Key, element *list.Element) {
	entry := element.Value.(*Entry)
	delete(c.entries, key)
	c.lru.Remove(element)
	c.bytes -= entry.bytes
	if entry.Submission != nil {
		c.retired = append(c.retired, entry.Submission)
	}
}

func (c *Cache) takeRetired() *brush.Submission {
	if c == nil || len(c.retired) == 0 {
		return nil
	}
	last := len(c.retired) - 1
	submission := c.retired[last]
	c.retired[last] = nil
	c.retired = c.retired[:last]
	return submission
}

// CaptureAdmittedSubmission transfers a retired allocation to a replacement on
// the GL owner. It cannot be drawn or disposed by the cache during capture.
func (c *Cache) CaptureAdmittedSubmission(estimate uint64, build func()) (*brush.Submission, error) {
	retired := c.takeRetired()
	submission, err := brush.CaptureAdmittedReplacement(retired, estimate, build)
	if submission != nil && submission == retired {
		c.stats.Reuses++
	}
	return submission, err
}

// DisposeRetiredStep releases one GL allocation on the shared visual scheduler.
func (c *Cache) DisposeRetiredStep() bool {
	submission := c.takeRetired()
	if submission == nil {
		return false
	}
	submission.Dispose()
	return true
}

func (c *Cache) HasRetired() bool { return c != nil && len(c.retired) != 0 }

func (c *Cache) EvictOldest() bool {
	if c == nil {
		return false
	}
	last := c.lru.Back()
	if last == nil {
		return false
	}
	c.remove(last.Value.(*Entry).Key, last)
	return true
}

// InvalidateChunks applies the same retirement rule to one evicted geometry
// set, preserving retained submissions that belong to other levels.
func (c *Cache) InvalidateChunks(chunks map[*chunk.Chunk]struct{}) {
	if c == nil {
		return
	}
	for key, element := range c.entries {
		if _, found := chunks[key.Chunk]; found {
			c.stats.Invalidations++
			c.remove(key, element)
		}
	}
}

// Clear drops logical entries immediately and defers GPU deletion to a GL owner.
func (c *Cache) Clear() {
	if c == nil {
		return
	}
	for element := c.lru.Front(); element != nil; element = element.Next() {
		if entry := element.Value.(*Entry); entry.Submission != nil {
			c.retired = append(c.retired, entry.Submission)
		}
	}
	c.entries = make(map[Key]*list.Element)
	c.lru.Init()
	c.bytes = 0
}
func (c *Cache) DisposeRetired() {
	if c == nil {
		return
	}
	for _, submission := range c.retired {
		submission.Dispose()
	}
	c.retired = nil
}

// RecordBuild records one completed CaptureSubmission and its static GPU upload.
func (c *Cache) RecordBuild(submission *brush.Submission) {
	if c == nil {
		return
	}
	c.stats.Builds++
	if submission != nil && submission.ByteSize() > 0 {
		c.stats.UploadBytes += uint64(submission.ByteSize())
	}
}

func (c *Cache) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	return c.stats
}

func (c *Cache) Len() int {
	if c == nil {
		return 0
	}
	return len(c.entries)
}
func (c *Cache) Bytes() int {
	if c == nil {
		return 0
	}
	return c.bytes
}
