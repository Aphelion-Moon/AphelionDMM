// Package uistage names editor execution-trace boundaries. It does not start
// tracing, collect map content, or change scheduling. Regions are wall-clock
// intervals (including waits and nested work), not exclusive CPU or GPU time.
package uistage

import (
	"context"
	"runtime/trace"
)

type Stage string

const (
	Workload               Stage = "aphelion.ui.workload"
	CaptureTile            Stage = "aphelion.edit.capture_tile"
	Commit                 Stage = "aphelion.edit.commit"
	Dispatch               Stage = "aphelion.edit.dispatch"
	LocalPrepare           Stage = "aphelion.local.prepare"
	LocalApply             Stage = "aphelion.local.apply"
	LocalRefresh           Stage = "aphelion.local.refresh"
	ProjectionVisible      Stage = "aphelion.projection.visible"
	ProjectionApply        Stage = "aphelion.projection.apply"
	Refresh                Stage = "aphelion.view.refresh"
	BucketBuild            Stage = "aphelion.bucket.build"
	CanvasDraw             Stage = "aphelion.canvas.draw_cpu"
	Frame                  Stage = "aphelion.window.frame"
	Present                Stage = "aphelion.window.swap_buffers"
	EnvironmentParse       Stage = "aphelion.parser.environment_total"
	IconMetadata           Stage = "aphelion.parser.icon_metadata_total"
	IconDecode             Stage = "aphelion.icon.decode"
	TextureUpload          Stage = "aphelion.texture.upload_cpu"
	EnvironmentReconstruct Stage = "aphelion.environment.reconstruct_link"
	EnvironmentFingerprint Stage = "aphelion.environment.fingerprint"
	MapSource              Stage = "aphelion.map.source_parse_backup"
	MapFlush               Stage = "aphelion.map.backup_flush_close"
	MapIntern              Stage = "aphelion.map.intern_slice"
	MapTiles               Stage = "aphelion.map.tiles_instances"
	MapImport              Stage = "aphelion.map.authority_import_validation"
	MapDocument            Stage = "aphelion.map.document_validation_hash"
	MapIndexes             Stage = "aphelion.map.authoritative_indexes"
	MapCompatibility       Stage = "aphelion.map.compatibility"
	MapInstall             Stage = "aphelion.map.install"
	FilterCompile          Stage = "aphelion.visibility.compile"
	FilterPublish          Stage = "aphelion.visibility.publish"
)

// Begin uses Go's disabled-trace fast path. End the region on this goroutine.
func Begin(stage Stage) *trace.Region {
	return trace.StartRegion(context.Background(), string(stage))
}

// DeferredBucket separates time waiting in the frame queue from execution. A
// task without an end event was still queued or executing at the cutoff; it must
// not be treated as a zero-duration sample or a completed rebuild.
func DeferredBucket(job func()) func() {
	if !trace.IsEnabled() {
		return job
	}
	ctx, task := trace.NewTask(context.Background(), "aphelion.bucket.queued")
	return func() {
		defer task.End()
		trace.WithRegion(ctx, "aphelion.bucket.deferred", job)
	}
}
