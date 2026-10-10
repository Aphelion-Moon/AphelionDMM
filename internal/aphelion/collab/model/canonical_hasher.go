package model

import (
	"crypto/sha256"
	"slices"
	"sort"
)

// CanonicalHasher recomputes Snapshot.Hash for successive revisions of one
// document. It keeps every tile digest and row digest, so an edit rehashes
// only its tiles, their rows and the map root: the cost of one edit does not
// depend on map size beyond the row table.
//
// A hasher is immutable once returned: Update builds a new one that shares the
// unchanged rows, so documents and branches may share a hasher freely.
type CanonicalHasher struct {
	maxX, maxY, maxZ int
	tiles            int
	rows             []*hashRow     // sorted by (z, y); never mutated
	positions        map[rowKey]int // index in rows; never mutated
	digest           string
}

type rowKey struct{ z, y int }

func (key rowKey) less(other rowKey) bool {
	return key.z < other.z || key.z == other.z && key.y < other.y
}

// hashRow is one (z, y) row: its tiles' x coordinates in ascending order, their
// digests and the row digest. Never mutated once shared.
type hashRow struct {
	key    rowKey
	xs     []int
	tiles  []digest
	digest digest
}

// NewCanonicalHasher validates snapshot and hashes it in full.
func NewCanonicalHasher(snapshot Snapshot) (*CanonicalHasher, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	return newCanonicalHasher(snapshot), nil
}

// Digest is the canonical hash of the snapshot this hasher describes.
func (hasher *CanonicalHasher) Digest() string {
	if hasher == nil {
		return ""
	}
	return hasher.digest
}

// Update hashes snapshot, which must equal the snapshot this hasher describes
// except at the changed coordinates. indexes maps every coordinate present in
// snapshot.Tiles to its position there. The caller must already have proven the
// edit keeps the snapshot valid (bounds, unique coordinates and stable IDs), as
// operation validation does; Update rehashes but does not revalidate. A nil
// hasher validates and hashes in full.
func (hasher *CanonicalHasher) Update(snapshot Snapshot, indexes map[Coord]int, changed []Coord) (*CanonicalHasher, string, error) {
	if hasher == nil {
		next, err := NewCanonicalHasher(snapshot)
		if err != nil {
			return nil, "", err
		}
		return next, next.digest, nil
	}
	if snapshot.MaxX != hasher.maxX || snapshot.MaxY != hasher.maxY || snapshot.MaxZ != hasher.maxZ {
		next := newCanonicalHasher(snapshot)
		return next, next.digest, nil
	}
	byRow := make(map[rowKey][]Coord)
	for _, coord := range changed {
		key := rowKey{coord.Z, coord.Y}
		if !slices.Contains(byRow[key], coord) {
			byRow[key] = append(byRow[key], coord)
		}
	}
	next := &CanonicalHasher{maxX: hasher.maxX, maxY: hasher.maxY, maxZ: hasher.maxZ, tiles: hasher.tiles, rows: slices.Clone(hasher.rows), positions: hasher.positions}
	tileHasher := newTileHasher()
	reshaped := false
	for key, coords := range byRow {
		position, existed := hasher.positions[key]
		row := &hashRow{key: key}
		if existed {
			old := hasher.rows[position]
			row.xs, row.tiles = slices.Clone(old.xs), slices.Clone(old.tiles)
		}
		for _, coord := range coords {
			at, present := slices.BinarySearch(row.xs, coord.X)
			index, found := indexes[coord]
			if found && (index < 0 || index >= len(snapshot.Tiles) || snapshot.Tiles[index].Coord != coord) {
				// The index does not describe this snapshot; never guess.
				full := newCanonicalHasher(snapshot)
				return full, full.digest, nil
			}
			switch {
			case found && present:
				row.tiles[at] = tileHasher.digest(snapshot.Tiles[index])
			case found:
				row.xs = slices.Insert(row.xs, at, coord.X)
				row.tiles = slices.Insert(row.tiles, at, tileHasher.digest(snapshot.Tiles[index]))
				next.tiles++
			case present:
				row.xs = slices.Delete(row.xs, at, at+1)
				row.tiles = slices.Delete(row.tiles, at, at+1)
				next.tiles--
			}
		}
		switch {
		case len(row.xs) == 0 && existed:
			next.rows[position] = nil
			reshaped = true
		case len(row.xs) == 0:
		case existed:
			row.digest = rowDigest(row)
			next.rows[position] = row
		default:
			row.digest = rowDigest(row)
			next.rows = append(next.rows, row)
			reshaped = true
		}
	}
	if next.tiles != len(snapshot.Tiles) {
		// The changed set missed an insertion or removal; never guess.
		full := newCanonicalHasher(snapshot)
		return full, full.digest, nil
	}
	if reshaped {
		next.rows = slices.DeleteFunc(next.rows, func(row *hashRow) bool { return row == nil })
		sort.Slice(next.rows, func(i, j int) bool { return next.rows[i].key.less(next.rows[j].key) })
		next.positions = rowPositions(next.rows)
	}
	next.digest = next.mapDigest()
	return next, next.digest, nil
}

// newCanonicalHasher hashes snapshot in full without validating it.
func newCanonicalHasher(snapshot Snapshot) *CanonicalHasher {
	tiles := canonicalTiles(snapshot)
	digests := tileDigests(tiles)
	hasher := &CanonicalHasher{maxX: snapshot.MaxX, maxY: snapshot.MaxY, maxZ: snapshot.MaxZ, tiles: len(tiles)}
	for start := 0; start < len(tiles); {
		key := rowKey{tiles[start].Coord.Z, tiles[start].Coord.Y}
		end := start
		for end < len(tiles) && tiles[end].Coord.Z == key.z && tiles[end].Coord.Y == key.y {
			end++
		}
		row := &hashRow{key: key, xs: make([]int, end-start), tiles: digests[start:end:end]}
		for i := start; i < end; i++ {
			row.xs[i-start] = tiles[i].Coord.X
		}
		row.digest = rowDigest(row)
		hasher.rows = append(hasher.rows, row)
		start = end
	}
	hasher.positions = rowPositions(hasher.rows)
	hasher.digest = hasher.mapDigest()
	return hasher
}

func rowPositions(rows []*hashRow) map[rowKey]int {
	positions := make(map[rowKey]int, len(rows))
	for i, row := range rows {
		positions[row.key] = i
	}
	return positions
}

func rowDigest(row *hashRow) digest {
	canonical := canonicalEncoder{digest: sha256.New()}
	writeString(&canonical, canonicalRowDomain)
	writeUint64(&canonical, uint64(row.key.z))
	writeUint64(&canonical, uint64(row.key.y))
	writeUint64(&canonical, uint64(len(row.tiles)))
	for _, tile := range row.tiles {
		canonical.writeDigest(tile)
	}
	canonical.flush()
	var sum digest
	canonical.digest.Sum(sum[:0])
	return sum
}

func (hasher *CanonicalHasher) mapDigest() string {
	canonical := canonicalEncoder{digest: sha256.New()}
	writeString(&canonical, canonicalMapDomain)
	writeUint64(&canonical, uint64(hasher.maxX))
	writeUint64(&canonical, uint64(hasher.maxY))
	writeUint64(&canonical, uint64(hasher.maxZ))
	writeUint64(&canonical, uint64(hasher.tiles))
	writeUint64(&canonical, uint64(len(hasher.rows)))
	for _, row := range hasher.rows {
		writeUint64(&canonical, uint64(row.key.z))
		writeUint64(&canonical, uint64(row.key.y))
		canonical.writeDigest(row.digest)
	}
	return canonical.sum()
}
