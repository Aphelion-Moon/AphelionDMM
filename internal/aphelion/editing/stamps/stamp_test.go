package stamps

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func stampFixture(t *testing.T) *Stamp {
	t.Helper()
	vars := &dmvars.MutableVariables{}
	vars.Put("unknown", `"preserved"`)
	tile := &dmmap.Tile{Coord: util.Point{X: 1, Y: 1, Z: 1}}
	tile.InstancesAdd(dmmprefab.New(0, "/obj/unknown", vars.ToImmutable()))
	s, err := Capture("User name", strings.Repeat("a", 64), &dmmap.Dmm{MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []*dmmap.Tile{tile}}, []util.Point{tile.Coord}, dm.NewPathsFilterEmpty())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestStampRejectsMalformedFile(t *testing.T) {
	for _, kind := range []string{"version", "unknown field", "duplicate tile", "outside tile", "inconsistent dimensions", "empty name", "missing vars", "invalid path", "invalid environment", "trailing", "invalid UTF-8"} {
		t.Run(kind, func(t *testing.T) {
			s := stampFixture(t)
			switch kind {
			case "version":
				s.data.Version++
			case "duplicate tile":
				s.data.Tiles = append(s.data.Tiles, s.data.Tiles[0])
			case "outside tile":
				s.data.Tiles[0].X = 2
			case "inconsistent dimensions":
				s.data.Width = 2
			case "empty name":
				s.data.Name = " "
			case "missing vars":
				s.data.Tiles[0].Prefabs[0].Vars = nil
			case "invalid path":
				s.data.Tiles[0].Prefabs[0].Path = ""
			case "invalid environment":
				s.data.EnvironmentHash = "not a hash"
			}
			data, err := json.Marshal(s.data)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "unknown field" {
				data = append([]byte(`{"unexpected":true,`), data[1:]...)
			}
			if kind == "trailing" {
				data = append(data, []byte(` {}`)...)
			}
			if kind == "invalid UTF-8" {
				data = []byte("{\"format\":\"aphelion-selection-stamp\",\"version\":1,\"name\":\"\xff\"}")
			}
			path := filepath.Join(t.TempDir(), "invalid.admmstamp")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("invalid stamp accepted")
			}
		})
	}
}

func TestStampSaveAndTemplateAreIndependent(t *testing.T) {
	s := stampFixture(t)
	filter := dm.NewPathsFilterEmpty()
	filter.TogglePath("/area/hidden")
	data := s.PasteData(filter, nil)
	data.Buffer[0].Instances()[0].SetPrefab(dmmprefab.New(0, "/changed", &dmvars.Variables{}))
	data.Filter.TogglePath("/area/hidden")
	path := filepath.Join(t.TempDir(), "selection.admmstamp")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(loaded.Close)
	if got := loaded.PasteData(filter, nil); got.Buffer[0].Instances()[0].Prefab().Path() != "/obj/unknown" || got.Filter.IsVisiblePath("/area/hidden") {
		t.Fatal("template or filter aliased caller changes")
	}
	s.data.Name = ""
	if err := s.Save(path); err == nil {
		t.Fatal("invalid stamp replaced existing file")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed save changed existing stamp")
	}
}

func TestStampLoadAdmissionReportsActualEstimate(t *testing.T) {
	s := stampFixture(t)
	path := filepath.Join(t.TempDir(), "selection.admmstamp")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWithBudget(path, resources.NewFixedBudget(1<<20)); err == nil {
		t.Fatal("stamp load exceeded the configured byte budget")
	} else {
		var admission *resources.AdmissionError
		if !errors.As(err, &admission) || admission.Needed <= admission.Available || !strings.Contains(err.Error(), "currently configured admission") {
			t.Fatalf("load error did not report needed and available bytes: %v", err)
		}
	}
}

func TestStampReadLeaseKeepsAdmissionUntilReturnedTemplateIsReleased(t *testing.T) {
	budget := resources.NewFixedBudget(16 << 20)
	vars := &dmvars.MutableVariables{}
	vars.Put("unknown", "preserved")
	tile := &dmmap.Tile{Coord: util.Point{X: 1, Y: 1, Z: 1}}
	tile.InstancesAdd(dmmprefab.New(0, "/obj/unknown", vars.ToImmutable()))
	stamp, err := CaptureWithBudget("leased", strings.Repeat("c", 64), &dmmap.Dmm{MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []*dmmap.Tile{tile}}, []util.Point{tile.Coord}, dm.NewPathsFilterEmpty(), budget)
	if err != nil {
		t.Fatal(err)
	}
	baseBytes := budget.Used()
	lease, err := stamp.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if lease.TileCount() != 1 {
		t.Fatal("live lease did not retain the captured tile count")
	}
	_, templateReservation, err := lease.PasteData(context.Background(), nil, nil, budget)
	if err != nil {
		t.Fatal(err)
	}
	if budget.Used() <= baseBytes {
		t.Fatal("materialized paste source was not admitted")
	}
	stamp.Close()
	if budget.Used() <= templateReservation.Bytes() {
		t.Fatal("closing the owner released stamp memory while a read lease was live")
	}
	lease.Release()
	if got := budget.Used(); got != templateReservation.Bytes() {
		t.Fatalf("releasing the read lease left %d bytes; expected only template reservation %d", got, templateReservation.Bytes())
	}
	templateReservation.Release()
	if got := budget.Used(); got != 0 {
		t.Fatalf("releasing both owners retained %d bytes", got)
	}
}

func TestStampStreamsFilesLargerThanFormerLimit(t *testing.T) {
	largeValue := strings.Repeat("x", 8<<20+1)
	s := &Stamp{data: document{
		Format: "aphelion-selection-stamp", Version: 1, Name: "large",
		EnvironmentHash: strings.Repeat("b", 64), Width: 1, Height: 1,
		Tiles: []tile{{X: 1, Y: 1, Prefabs: []prefab{{Path: "/obj/unknown", Vars: map[string]string{"value": largeValue}}}}},
	}}
	path := filepath.Join(t.TempDir(), "large.admmstamp")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 8<<20 {
		t.Fatalf("streamed test file did not exceed prior limit: size=%d err=%v", info.Size(), err)
	}
	loaded, err := LoadWithBudget(path, resources.NewFixedBudget(128<<20))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(loaded.Close)
	if got := loaded.data.Tiles[0].Prefabs[0].Vars["value"]; got != largeValue {
		t.Fatal("streamed load changed large stamp value")
	}
}
