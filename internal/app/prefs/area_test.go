package prefs

import (
	"testing"

	"sdmm/internal/app/render"
)

func TestAreaPolicyFromEditorClampsAndMaps(t *testing.T) {
	p := AreaPolicyFromEditor(Editor{AreaOverlayPercent: 35, HideBaseArea: true})
	if p.Alpha != 0.35 || !p.HideBase {
		t.Fatalf("got %+v", p)
	}
	if p := AreaPolicyFromEditor(Editor{AreaOverlayPercent: 500}); p.Alpha != 1 {
		t.Fatalf("alpha %v want 1", p.Alpha)
	}
	if p := AreaPolicyFromEditor(Editor{AreaOverlayPercent: -3}); p.Alpha != 0 {
		t.Fatalf("alpha %v want 0", p.Alpha)
	}
}

func TestApplyAreaPolicyInstallsAndInvalidates(t *testing.T) {
	defer render.SetAreaPolicy(render.DefaultAreaPolicy())
	ApplyAreaPolicy(Editor{AreaOverlayPercent: 35, HideBaseArea: true})
	rev := render.AreaPolicyRevision()
	ApplyAreaPolicy(Editor{AreaOverlayPercent: 60, HideBaseArea: true})
	if render.AreaPolicyRevision() == rev {
		t.Fatal("changing the preference must bump the policy revision")
	}
	if got := render.CurrentAreaPolicy().Alpha; got != 0.6 {
		t.Fatalf("alpha %v want 0.6", got)
	}
}

func TestPreferencesExposeAreaOptions(t *testing.T) {
	e := Editor{AreaOverlayPercent: 35, HideBaseArea: true}
	p := Make(nopApp{}, &Prefs{Editor: e})
	_ = p
}

type nopApp struct{}

func (nopApp) UpdateScale() {}
