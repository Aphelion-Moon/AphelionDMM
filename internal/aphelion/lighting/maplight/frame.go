package maplight

import "sync/atomic"

var frameSeq atomic.Uint64

// rowSpan records which tile rows one published result changed.
type rowSpan struct {
	seq, prev uint64
	y0, y1    int
	full      bool
}

const maxHistory = 16

// Frame is an immutable published lighting result for one level: for every
// tile in row-major order, the final light of its four corners (SW, SE, NW, NE)
// as RGB floats, 12 floats per tile. Consumers must not modify Tiles.
type Frame struct {
	level, w, h int
	tiles       []float32
	seq         uint64
	hist        []rowSpan
}

// Dimensions returns the level number and size in tiles.
func (f *Frame) Dimensions() (level, w, h int) { return f.level, f.w, f.h }

// Tiles returns the shared tile colours. It is never modified after publication.
func (f *Frame) Tiles() []float32 { return f.tiles }

// Seq identifies this publication. Sequence numbers are unique per process.
func (f *Frame) Seq() uint64 { return f.seq }

// RowsSince returns the half-open tile-row range changed since the consumer
// last uploaded sequence seq, or full when the history cannot prove a bound.
func (f *Frame) RowsSince(seq uint64) (y0, y1 int, full bool) {
	if f == nil {
		return 0, 0, true
	}
	if seq == f.seq {
		return 0, 0, false
	}
	y0, y1 = f.h, 0
	for cur := f.seq; cur != seq; {
		var span *rowSpan
		for i := range f.hist {
			if f.hist[i].seq == cur {
				span = &f.hist[i]
				break
			}
		}
		if span == nil || span.full {
			return 0, f.h, true
		}
		y0, y1 = min(y0, span.y0), max(y1, span.y1)
		cur = span.prev
	}
	return y0, y1, false
}

func newFrame(level, w, h int, tiles []float32, prev *Frame, rows rowSpan) *Frame {
	f := &Frame{level: level, w: w, h: h, tiles: tiles, seq: frameSeq.Add(1)}
	rows.seq = f.seq
	if prev != nil && prev.level == level && prev.w == w && prev.h == h && !rows.full {
		rows.prev = prev.seq
		f.hist = append(f.hist, prev.hist...)
		if len(f.hist) >= maxHistory {
			f.hist = f.hist[len(f.hist)-maxHistory+1:]
		}
	} else {
		rows.full = true
	}
	f.hist = append(f.hist, rows)
	return f
}
