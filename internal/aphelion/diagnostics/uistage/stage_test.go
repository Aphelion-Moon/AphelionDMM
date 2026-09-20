package uistage

import (
	"runtime/trace"
	"testing"
)

func BenchmarkDisabledUIStage(b *testing.B) {
	if trace.IsEnabled() {
		b.Fatal("disabled-stage benchmark requires tracing to be off")
	}
	job := func() {}
	b.ReportAllocs()
	for b.Loop() {
		Begin(CaptureTile).End()
		DeferredBucket(job)()
	}
}
