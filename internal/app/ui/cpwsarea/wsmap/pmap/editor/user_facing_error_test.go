package editor

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/client"
)

func TestRateLimitedRefusalsReportOnce(t *testing.T) {
	e := largeBulkEditor(t)
	app := &noopReportingApp{editorTestApp: e.app.(*editorTestApp)}
	e.app = app
	limited := fmt.Errorf("refused: %w", client.ErrRateLimited)
	for range 5 {
		e.reportCollaborationError("Unable to apply map change", limited)
	}
	if len(app.errors) != 1 {
		t.Fatalf("rate-limited refusals reported %d times, want once", len(app.errors))
	}
	e.reportCollaborationError("Unable to apply map change", errors.New("disk full"))
	e.reportCollaborationError("Unable to apply map change", limited)
	if len(app.errors) != 3 {
		t.Fatalf("a new rate-limit episode was not reported, got %d reports", len(app.errors))
	}
}

func TestUserFacingCollaborationErrorHidesPreconditionText(t *testing.T) {
	cause := fmt.Errorf("apply speculative operation: precondition failed at (1,1,1)")
	got := userFacingCollaborationError(cause)
	if strings.Contains(got.Error(), "precondition") || !strings.Contains(got.Error(), "rejected") {
		t.Fatalf("message not user facing: %v", got)
	}
	if !errors.Is(got, cause) {
		t.Fatal("original cause was lost")
	}
	other := errors.New("disk full")
	if userFacingCollaborationError(other) != other {
		t.Fatal("unrelated errors must pass through")
	}
}
