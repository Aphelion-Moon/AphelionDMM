package store

import (
	"testing"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

func TestVerifiedHeadsTakeTransfersOwnershipAndBounds(t *testing.T) {
	var heads VerifiedHeads
	document := &engine.Document{}
	key := HeadKey{Revision: 3, Hash: "h"}
	heads.Put("doc", key, document)
	if got := heads.Take("doc", HeadKey{Revision: 4, Hash: "h"}); got != nil {
		t.Fatal("mismatched head returned a document")
	}
	if got := heads.Take("doc", key); got != nil {
		t.Fatal("a mismatched take must also discard the entry")
	}
	heads.Put("doc", key, document)
	if got := heads.Take("doc", HeadKey{SnapshotRevision: 1, Revision: 3, Hash: "h"}); got != nil {
		t.Fatal("a published snapshot must invalidate the head")
	}
	heads.Put("doc", key, document)
	if got := heads.Take("doc", key); got != document {
		t.Fatal("matching head not returned")
	}
	if got := heads.Take("doc", key); got != nil {
		t.Fatal("take did not transfer ownership")
	}
	for i := 0; i < maxVerifiedHeads+3; i++ {
		heads.Put(model.DocumentID(rune('a'+i)), key, document)
	}
	if heads.Len() > maxVerifiedHeads {
		t.Fatalf("%d cached heads exceed the bound", heads.Len())
	}
}
