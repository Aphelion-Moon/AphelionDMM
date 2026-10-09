package maplight

import (
	"math/rand"
	"testing"
	"time"

	"sdmm/internal/aphelion/lighting"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

// stationMap builds a station-like synthetic level: a wall every ~6th tile,
// a fixture roughly every 8 tiles, a space border, and two objects per tile.
func stationMap(n int) (*synthetic, *dmmap.Dmm) {
	s := newSynthetic()
	d := s.mapOf(n, n)
	fixture := prefab("/obj/machinery/light", typ(s.root, "dir", "1"))
	table := prefab("/obj/structure/table", typ(s.root))
	r := rand.New(rand.NewSource(11))
	for y := 1; y <= n; y++ {
		for x := 1; x <= n; x++ {
			switch {
			case x <= 4 || y <= 4 || x > n-4 || y > n-4:
				s.set(d, x, y, s.space, s.spaceArea)
			case r.Intn(6) == 0:
				s.set(d, x, y, s.wall, s.station)
			case (x*7+y*13)%61 == 0:
				s.set(d, x, y, s.floor, fixture, table, s.station)
			default:
				s.set(d, x, y, s.floor, table, s.station)
			}
		}
	}
	return s, d
}

func BenchmarkCaptureLevel130(b *testing.B)     { benchCapture(b, 130) }
func BenchmarkCaptureLevel255(b *testing.B)     { benchCapture(b, 255) }
func BenchmarkSnapshotLevel255(b *testing.B)    { benchSnapshot(b, 255) }
func BenchmarkWorkerFull130(b *testing.B)       { benchWorker(b, 130, true) }
func BenchmarkWorkerFull255(b *testing.B)       { benchWorker(b, 255, true) }
func BenchmarkWorkerIncremental255(b *testing.B) { benchWorker(b, 255, false) }

func benchCapture(b *testing.B, n int) {
	_, d := stationMap(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cls := NewClassifier(DefaultSettings())
		st := newLevelState(n, n)
		newBuilder(d, 1, cls, st).Step(0)
	}
}

func benchSnapshot(b *testing.B, n int) {
	_, d := stationMap(n)
	cls := NewClassifier(DefaultSettings())
	st := newLevelState(n, n)
	newBuilder(d, 1, cls, st).Step(0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = st.snapshot()
	}
}

func benchWorker(b *testing.B, n int, full bool) {
	_, d := stationMap(n)
	cls := NewClassifier(DefaultSettings())
	st := newLevelState(n, n)
	newBuilder(d, 1, cls, st).Step(0)
	opts := DefaultSettings().options()
	base := runJob(&job{level: st.snapshot(), opts: opts, full: true, z: 1})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := &job{level: st.snapshot(), opts: opts, full: full, prev: base, z: 1}
		if !full {
			j.dirty = lighting.RectAround(n/2, n/2)
		}
		_ = runJob(j)
	}
}

// TestRecordEndToEndTimings prints the end-to-end costs for the report. It
// asserts only generous ceilings so a slow CI machine does not flake.
func TestRecordEndToEndTimings(t *testing.T) {
	if testing.Short() {
		t.Skip("timing record")
	}
	for _, n := range []int{130, 255} {
		s, d := stationMap(n)
		c := NewController()
		in := input(d, &env{}, 1)
		start := time.Now()
		var captureTicks int
		var maxTick time.Duration
		in.Budget = 2 * time.Millisecond
		for {
			t0 := time.Now()
			c.Tick(in)
			if dt := time.Since(t0); dt > maxTick {
				maxTick = dt
			}
			captureTicks++
			if c.builder == nil {
				break
			}
		}
		captured := time.Since(start)
		f := settle(t, c, in)
		total := time.Since(start)
		sources := c.Report().Sources
		// One-tile wall edit: observed dirty region, incremental recompute.
		s.set(d, n/2, n/2, s.wall, s.station)
		c.MarkDirty(1, []util.Point{{X: n / 2, Y: n / 2, Z: 1}})
		t1 := time.Now()
		_ = settle(t, c, in)
		edit := time.Since(t1)
		c.Close()
		t.Logf("%dx%d (%d tiles, %d sources, frame %d floats): bounded capture %v over %d ticks (max single tick %v); capture+first frame %v; one-tile edit -> new frame %v",
			n, n, n*n, sources, len(f.Tiles()), captured, captureTicks, maxTick, total, edit)
		if total > 30*time.Second {
			t.Fatal("unreasonably slow")
		}
	}
}
