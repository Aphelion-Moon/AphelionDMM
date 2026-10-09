package maplight

import (
	"sync"
	"testing"
	"time"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

type env struct{ id int }

func input(d *dmmap.Dmm, e *env, version uint64) Input {
	s := DefaultSettings()
	s.Enabled = true
	return Input{Dmm: d, Level: 1, Env: e, Settings: s, ViewVersion: version}
}

// settle ticks until the controller has no capture, job or dirty region left.
func settle(t *testing.T, c *Controller, in Input) *Frame {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		f := c.Tick(in)
		if f != nil && !c.Busy() {
			return f
		}
		if time.Now().After(deadline) {
			t.Fatal("controller never settled")
		}
		time.Sleep(time.Millisecond)
	}
}

func tileColor(f *Frame, x, y, corner int) [3]float32 {
	_, w, _ := f.Dimensions()
	i := (y*w+x)*12 + corner*3
	return [3]float32{f.Tiles()[i], f.Tiles()[i+1], f.Tiles()[i+2]}
}

func TestLitTileIsBrighterThanUnlitTileAndFloorIsKept(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(12, 3)
	s.set(d, 2, 2, s.floor, s.lamp, s.station)
	c := NewController()
	defer c.Close()
	f := settle(t, c, input(d, &env{}, 1))
	_, w, h := f.Dimensions()
	if w != 12 || h != 3 || len(f.Tiles()) != 12*3*12 {
		t.Fatalf("frame %dx%d with %d floats", w, h, len(f.Tiles()))
	}
	lit, far := tileColor(f, 1, 1, 0), tileColor(f, 11, 1, 0)
	if lit[0] < 0.5 || lit[1] != 0.10 || lit[2] != 0.10 {
		t.Fatalf("red lamp tile %v: want strong red over the 0.10 floor", lit)
	}
	if far != ([3]float32{0.10, 0.10, 0.10}) {
		t.Fatalf("unlit tile %v must sit on the 0.10 floor", far)
	}
	if rep := c.Report(); rep.Sources != 1 {
		t.Fatalf("report %+v", rep)
	}
}

func TestIncrementalUpdateIsBitIdenticalToFreshCompute(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(30, 150)
	s.set(d, 10, 10, s.floor, s.lamp, s.station)
	s.set(d, 20, 6, s.floor, s.lamp, s.station)
	e := &env{}
	c := NewController()
	defer c.Close()
	first := settle(t, c, input(d, e, 1))

	s.set(d, 12, 10, s.wall, s.station)
	s.set(d, 15, 12, s.floor, s.lamp, s.station)
	s.set(d, 20, 6, s.floor, s.station)
	c.MarkDirty(1, []util.Point{{X: 12, Y: 10, Z: 1}, {X: 15, Y: 12, Z: 1}, {X: 20, Y: 6, Z: 1}})
	f := settle(t, c, input(d, e, 2))
	if st := c.Stats(); st.Incremental != 1 || st.Full != 1 {
		t.Fatalf("an observed edit must recompute incrementally, got %+v", st)
	}

	fresh := NewController()
	defer fresh.Close()
	want := settle(t, fresh, input(d, e, 1))
	if len(f.Tiles()) != len(want.Tiles()) {
		t.Fatal("size")
	}
	for i := range want.Tiles() {
		if f.Tiles()[i] != want.Tiles()[i] {
			t.Fatalf("float %d: incremental %v != full %v", i, f.Tiles()[i], want.Tiles()[i])
		}
	}
	y0, y1, full := f.RowsSince(first.Seq())
	if full || y0 < 0 || y1 > 100 || y0 >= y1 {
		t.Fatalf("an incremental frame must report a bounded dirty row range, got %d..%d full=%v", y0, y1, full)
	}
}

func TestViewVersionChangeWithoutDirtyRegionRecomputesFully(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(10, 10)
	s.set(d, 5, 5, s.floor, s.lamp, s.station)
	e := &env{}
	c := NewController()
	defer c.Close()
	settle(t, c, input(d, e, 1))
	s.set(d, 6, 5, s.wall, s.station) // an unobserved edit
	f := settle(t, c, input(d, e, 2))
	if st := c.Stats(); st.Full != 2 {
		t.Fatalf("stats %+v", st)
	}
	if got := tileColor(f, 5, 4, 0); got[0] < 0.5 {
		t.Fatalf("lamp tile %v", got)
	}
}

type blockedRunner struct {
	mu      sync.Mutex
	started chan *job
	release chan struct{}
	calls   int
}

func newBlockedRunner() *blockedRunner {
	return &blockedRunner{started: make(chan *job, 16), release: make(chan struct{}, 16)}
}

func (b *blockedRunner) run(j *job) *result {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	b.started <- j
	<-b.release
	return runJob(j)
}

func waitJob(t *testing.T, b *blockedRunner) {
	t.Helper()
	select {
	case <-b.started:
	case <-time.After(10 * time.Second):
		t.Fatal("job never started")
	}
}

func TestResultsForAnotherLevelOrEnvironmentAreDropped(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(in *Input)
	}{
		{"level", func(in *Input) { in.Level = 2 }},
		{"environment", func(in *Input) { in.Env = &env{id: 2} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSynthetic()
			d := s.mapOf(8, 8)
			d.MaxZ = 2
			d.Tiles = append(d.Tiles, make([]*dmmap.Tile, 64)...)
			for i := 64; i < 128; i++ {
				d.Tiles[i] = &dmmap.Tile{Coord: util.Point{X: (i-64)%8 + 1, Y: (i-64)/8 + 1, Z: 2}}
			}
			s.set(d, 4, 4, s.floor, s.lamp, s.station)
			runner := newBlockedRunner()
			c := NewController()
			c.run = runner.run
			defer c.Close()
			in := input(d, &env{id: 1}, 1)
			deadline := time.Now().Add(10 * time.Second)
			for len(runner.started) == 0 {
				c.Tick(in)
				time.Sleep(time.Millisecond)
				if time.Now().After(deadline) {
					t.Fatal("no job")
				}
			}
			waitJob(t, runner)
			tc.change(&in)
			runner.release <- struct{}{} // the old result completes after the change
			// The new key restarts the capture; the old result must never be shown.
			for i := 0; i < 50; i++ {
				if f := c.Tick(in); f != nil {
					t.Fatalf("stale frame leaked")
				}
				time.Sleep(time.Millisecond)
			}
			if c.Stats().Dropped < 1 {
				t.Fatalf("stale completion was not dropped: %+v", c.Stats())
			}
			close(runner.release)
		})
	}
}

func TestBurstOfEditsCoalescesToOneInFlightAndOnePending(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(16, 16)
	s.set(d, 8, 8, s.floor, s.lamp, s.station)
	runner := newBlockedRunner()
	c := NewController()
	c.run = runner.run
	defer c.Close()
	e := &env{}
	deadline := time.Now().Add(10 * time.Second)
	for len(runner.started) == 0 && time.Now().Before(deadline) {
		c.Tick(input(d, e, 1))
		time.Sleep(time.Millisecond)
	}
	waitJob(t, runner)
	for i := 0; i < 40; i++ {
		c.MarkDirty(1, []util.Point{{X: 1 + i%16, Y: 3, Z: 1}})
		c.Tick(input(d, e, uint64(2+i)))
	}
	if st := c.Stats(); st.InFlightMax != 1 {
		t.Fatalf("more than one job in flight: %+v", st)
	}
	runner.release <- struct{}{} // first job completes
	for len(runner.started) == 0 && time.Now().Before(deadline) {
		c.Tick(input(d, e, 50))
		time.Sleep(time.Millisecond)
	}
	waitJob(t, runner) // exactly one follow-up job covering the whole burst
	runner.release <- struct{}{}
	for c.Busy() && time.Now().Before(deadline) {
		c.Tick(input(d, e, 50))
		time.Sleep(time.Millisecond)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.calls != 2 {
		t.Fatalf("40 edits during one job must produce exactly one follow-up, got %d jobs", runner.calls)
	}
}

func TestDisabledPreviewDoesNoWorkAndReleasesState(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(8, 8)
	s.set(d, 4, 4, s.floor, s.lamp, s.station)
	e := &env{}
	c := NewController()
	defer c.Close()
	settle(t, c, input(d, e, 1))
	off := input(d, e, 1)
	off.Settings.Enabled = false
	if c.Tick(off) != nil || c.Busy() || c.Report().Sources != 0 {
		t.Fatal("a disabled preview must show nothing and hold no state")
	}
	before := c.Stats()
	c.MarkDirty(1, []util.Point{{X: 1, Y: 1, Z: 1}})
	c.Tick(off)
	if c.Stats() != before {
		t.Fatal("disabled preview scheduled work")
	}
}

func TestPreviewNeverMutatesTheMap(t *testing.T) {
	s := newSynthetic()
	d := s.mapOf(8, 8)
	s.set(d, 4, 4, s.floor, s.lamp, s.station)
	var before []*dmmap.Tile
	before = append(before, d.Tiles...)
	ids := map[uint64]bool{}
	for _, tile := range d.Tiles {
		for _, in := range tile.Instances() {
			ids[in.Id()] = true
		}
	}
	c := NewController()
	defer c.Close()
	settle(t, c, input(d, &env{}, 1))
	for i, tile := range d.Tiles {
		if tile != before[i] {
			t.Fatal("tile pointer replaced")
		}
		for _, in := range tile.Instances() {
			if !ids[in.Id()] {
				t.Fatal("instance added")
			}
		}
	}
}

func TestFrameRowsSinceReportsUnionOrFull(t *testing.T) {
	f := &Frame{level: 1, w: 4, h: 10, seq: 5, hist: []rowSpan{{seq: 3, prev: 2, y0: 2, y1: 4}, {seq: 4, prev: 3, y0: 6, y1: 7}, {seq: 5, prev: 4, y0: 3, y1: 5}}}
	if y0, y1, full := f.RowsSince(5); full || y0 != y1 {
		t.Fatalf("nothing to upload when current: %d %d %v", y0, y1, full)
	}
	if y0, y1, full := f.RowsSince(3); full || y0 != 3 || y1 != 7 {
		t.Fatalf("union of spans after 3: got %d..%d full=%v", y0, y1, full)
	}
	if _, _, full := f.RowsSince(1); !full {
		t.Fatal("history gap must request a full upload")
	}
	f.hist[0].full = true
	if _, _, full := f.RowsSince(2); !full {
		t.Fatal("full span must propagate")
	}
}
