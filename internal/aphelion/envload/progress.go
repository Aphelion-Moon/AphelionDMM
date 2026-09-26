package envload

import "sync/atomic"

// Progress publishes only a stage name; no linked environment objects cross the
// worker/UI boundary until preparation has completed.
type Progress struct{ stage atomic.Value }

func (p *Progress) Set(stage string) { p.stage.Store(stage) }
func (p *Progress) Stage() string {
	if stage := p.stage.Load(); stage != nil {
		return stage.(string)
	}
	return "Preparing environment"
}
