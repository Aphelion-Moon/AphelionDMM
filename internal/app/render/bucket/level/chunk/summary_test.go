package chunk

import (
	"reflect"
	"testing"
	"time"
)

type emitted struct {
	key    string
	span   time.Duration
	counts map[string]uint64
}

type summaryClock struct{ t time.Time }

func (c *summaryClock) now() time.Time { return c.t }

func newTestSummary(clock *summaryClock) (*Summary, *[]emitted) {
	var got []emitted
	s := NewSummary("test", time.Second, clock.now)
	s.emit = func(_, key string, span time.Duration, counts map[string]uint64) {
		got = append(got, emitted{key: key, span: span, counts: counts})
	}
	return s, &got
}

func TestSummaryEmitsAtMostOncePerIntervalPerKey(t *testing.T) {
	clock := &summaryClock{t: time.Unix(1000, 0)}
	s, got := newTestSummary(clock)

	// A burst within one interval emits nothing, however many events arrive.
	for i := 0; i < 1000; i++ {
		s.Add("map-a", "chunks_updated", 1)
	}
	if len(*got) != 0 {
		t.Fatalf("emitted %d records inside the interval, want 0", len(*got))
	}

	// The first Add after the interval elapses emits one record with every count.
	clock.t = clock.t.Add(time.Second)
	s.Add("map-a", "chunks_updated", 1)
	want := []emitted{{key: "map-a", span: time.Second, counts: map[string]uint64{"chunks_updated": 1001}}}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("emitted %+v, want %+v", *got, want)
	}

	// Another burst inside the new window again emits nothing.
	for i := 0; i < 500; i++ {
		s.Add("map-a", "chunks_updated", 1)
	}
	if len(*got) != 1 {
		t.Fatalf("emitted %d records after second burst, want 1", len(*got))
	}
}

func TestSummaryKeepsMapsIndependent(t *testing.T) {
	clock := &summaryClock{t: time.Unix(2000, 0)}
	s, got := newTestSummary(clock)

	s.Add("map-a", "bucket_updates", 2)
	s.Add("map-b", "bucket_updates", 5)
	clock.t = clock.t.Add(500 * time.Millisecond)
	s.Add("map-a", "bucket_updates", 1)
	clock.t = clock.t.Add(500 * time.Millisecond)

	// map-a's window started at the first Add, so it has elapsed; map-b's window started at its own first Add.
	s.Add("map-a", "bucket_updates", 1)
	if len(*got) != 1 || (*got)[0].key != "map-a" || (*got)[0].counts["bucket_updates"] != 4 {
		t.Fatalf("map-a emission = %+v, want one record with 4 updates", *got)
	}

	clock.t = clock.t.Add(500 * time.Millisecond)
	s.Add("map-b", "bucket_updates", 1)
	if len(*got) != 2 || (*got)[1].key != "map-b" || (*got)[1].counts["bucket_updates"] != 6 {
		t.Fatalf("map-b emission = %+v, want second record with 6 updates", *got)
	}
}

func TestSummaryNilClockUsesWallClock(t *testing.T) {
	s := NewSummary("test", time.Hour, nil)
	s.emit = func(string, string, time.Duration, map[string]uint64) {
		t.Fatal("record emitted before the hour-long interval elapsed")
	}
	s.Add("map-a", "chunks_updated", 1)
}
