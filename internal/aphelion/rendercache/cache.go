package rendercache

import (
	"container/list"
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
}

type Entry struct {
	Key           Key
	Versions      Versions
	Submission    *brush.Submission
	unitIDs       []uint64
	indexComplete bool
	bytes         int
}

func (e *Entry) Matches(v Versions) bool { return e != nil && e.Versions == v }

// IntersectsUnitIDs returns true when a selected ID is in this submission. An
// incomplete index conservatively requests the dynamic path for that layer.
func (e *Entry) IntersectsUnitIDs(ids map[uint64]struct{}) bool {
	if e == nil || len(ids) == 0 {
		return false
	}
	if !e.indexComplete {
		return true
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
	if c == nil || c.entries == nil {
		return nil, false
	}
	element := c.entries[key]
	if element == nil {
		c.stats.Misses++
		return nil, false
	}
	entry := element.Value.(*Entry)
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
	if c == nil || len(unitIDs) > MaxIndexedUnitsPerEntry {
		return false
	}
	unitIDs = append([]uint64(nil), unitIDs...)
	sort.Slice(unitIDs, func(i, j int) bool { return unitIDs[i] < unitIDs[j] })
	bytes := len(unitIDs) * 8
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
	entry := &Entry{Key: key, Versions: versions, Submission: submission, unitIDs: unitIDs, indexComplete: indexComplete, bytes: bytes}
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
		entry.Submission.Dispose()
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
