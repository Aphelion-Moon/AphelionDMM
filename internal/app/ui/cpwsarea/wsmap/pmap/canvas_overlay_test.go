package pmap

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/canvas"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/overlay"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

func TestFlashExpiryCompactsSurvivors(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	for _, tc := range []struct {
		name string
		ages []float64
		want []int
	}{
		{"empty", nil, nil},
		{"one expired", []float64{1}, nil},
		{"multiple expired after delayed frame", []float64{2, 2, 2}, nil},
		{"mixed", []float64{1, .1, 1, .2}, []int{1, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &editor.Editor{}
			areas := make([]overlay.FlickArea, len(tc.ages))
			instances := make([]overlay.FlickInstance, len(tc.ages))
			for i, age := range tc.ages {
				areas[i] = overlay.FlickArea{Time: imgui.Time() - age, Area: util.Bounds{X1: float32(i)}}
				instances[i] = overlay.FlickInstance{Time: imgui.Time() - age, Instance: &dmminstance.Instance{}}
			}
			e.SetFlickAreas(areas)
			e.SetFlickInstance(instances)
			p := &PaneMap{editor: e, canvasOverlay: canvas.NewOverlay()}
			p.processCanvasOverlayFlick()
			if len(e.FlickAreas()) != len(tc.want) || len(e.FlickInstance()) != len(tc.want) {
				t.Fatalf("survivors = %d areas, %d instances; want %d", len(e.FlickAreas()), len(e.FlickInstance()), len(tc.want))
			}
			for i, original := range tc.want {
				if e.FlickAreas()[i].Area.X1 != float32(original) || e.FlickInstance()[i].Time != imgui.Time()-tc.ages[original] {
					t.Fatal("expiry changed survivor order or identity")
				}
			}
			for _, entry := range instances[len(tc.want):] {
				if entry.Instance != nil {
					t.Fatal("expired instance retained in backing slice")
				}
			}
		})
	}
}
