// APHELION EDIT ADDITION START - OWNED MAP OPEN
package editor

import (
	"context"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmsnap"
)

// PreparedOpen owns an unpublished document. No widget, command storage, global
// prefab cache or graphics operation is accessed during its preparation.
type PreparedOpen struct {
	editor        *Editor
	Compatibility *dmmsnap.DmmSnap
	Hash          string
}

func PrepareOpen(ctx context.Context, environment *dmenv.Dme, dmm *dmmap.Dmm) (*PreparedOpen, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	environmentHash, err := mapadapter.EnvironmentHash(environment)
	if err != nil {
		return nil, err
	}
	documentID, err := model.NewDocumentID()
	if err != nil {
		return nil, err
	}
	actorID, err := model.NewActorID()
	if err != nil {
		return nil, err
	}
	snapshot, err := mapadapter.Import(dmm, documentID, environmentHash)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	document, err := engine.NewUnsharedDocument(snapshot)
	if err != nil {
		return nil, err
	}
	local, err := executor.NewLocal(document, actorID)
	if err != nil {
		return nil, err
	}
	e := &Editor{dmm: dmm, documentID: documentID, actorID: actorID, executor: local, workBudget: resources.DefaultBudget()}
	e.setAuthoritative(snapshot)
	e.updateAreasZones()
	hash, err := snapshot.Hash()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &PreparedOpen{editor: e, Compatibility: dmmsnap.New(dmm), Hash: hash}, nil
}
func (p *PreparedOpen) Dmm() *dmmap.Dmm          { return p.editor.dmm }
func (p *PreparedOpen) Revision() model.Revision { return p.editor.authoritative.Revision }
func NewPrepared(app app, attachedMap attachedMap, p *PreparedOpen) *Editor {
	e := p.editor
	p.editor = nil // Single ownership transfer; a prepared document cannot be installed twice.
	e.app = app
	e.pMap = attachedMap
	e.history = app.CommandStorage().Bind(e.dmm.Path.Absolute)
	e.resetAttachment()
	return e
}

// APHELION EDIT ADDITION END
