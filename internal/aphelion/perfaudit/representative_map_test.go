package perfaudit

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
)

// Private representative maps stay outside the repository. Retained output, if
// requested, always goes into a new directory; the source map is never saved.
func TestRepresentativeMapRoundTrip(t *testing.T) {
	mapPath, dmePath := os.Getenv("APHELION_AUDIT_MAP"), os.Getenv("APHELION_AUDIT_DME")
	if mapPath == "" && dmePath == "" {
		t.Skip("set APHELION_AUDIT_MAP and APHELION_AUDIT_DME for a representative fixture")
	}
	if mapPath == "" || dmePath == "" {
		t.Fatal("both APHELION_AUDIT_MAP and APHELION_AUDIT_DME are required")
	}
	mapHash, dmeHash := auditFileHash(t, mapPath), auditFileHash(t, dmePath)
	defer func() {
		if auditFileHash(t, mapPath) != mapHash || auditFileHash(t, dmePath) != dmeHash {
			t.Error("source map or DME changed during the audit")
		}
	}()
	t.Logf("map_sha256=%s dme_sha256=%s", mapHash, dmeHash)
	started := time.Now()
	data, err := dmmdata.New(mapPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("map_parse_ms=%.3f", float64(time.Since(started).Microseconds())/1000)
	paths := make(map[string]bool)
	usedKeys := make(map[dmmdata.Key]bool)
	levels := make(map[int]int)
	instances, overrides, maxInstances := 0, 0, 0
	for coord, key := range data.Grid {
		prefabs, exists := data.Dictionary[key]
		if !exists {
			t.Fatalf("missing dictionary key at %v", coord)
		}
		usedKeys[key] = true
		levels[coord.Z]++
		instances += len(prefabs)
		maxInstances = max(maxInstances, len(prefabs))
		for _, prefab := range prefabs {
			paths[prefab.Path()] = true
			overrides += prefab.Vars().Len()
		}
	}
	variants, variableNames := make(map[string]bool), make(map[string]bool)
	for key := range usedKeys {
		for _, prefab := range data.Dictionary[key] {
			vars := make(map[string]string)
			for _, name := range prefab.Vars().Iterate() {
				variableNames[name] = true
				vars[name] = prefab.Vars().ValueV(name, "")
			}
			encoded, err := json.Marshal(struct {
				Path string
				Vars map[string]string
			}{prefab.Path(), vars})
			if err != nil {
				t.Fatal(err)
			}
			variants[string(encoded)] = true
		}
	}
	t.Logf("dimensions=%dx%dx%d cells=%d levels=%v dictionary=%d used_dictionary=%d types=%d prefab_variants=%d variable_names=%d placed_prefabs=%d explicit_var_occurrences=%d max_prefabs_per_cell=%d TGM=%t",
		data.MaxX, data.MaxY, data.MaxZ, len(data.Grid), levels, len(data.Dictionary), len(usedKeys), len(paths), len(variants), len(variableNames), instances, overrides, maxInstances, data.IsTgm)
	started = time.Now()
	environment, err := dmenv.New(dmePath)
	if err != nil {
		t.Fatalf("native DME parse: %v", err)
	}
	t.Logf("native_dme_parse_ms=%.3f environment_types=%d", float64(time.Since(started).Microseconds())/1000, len(environment.Objects))
	environmentHash, err := mapadapter.EnvironmentHash(environment)
	if err != nil {
		t.Fatal(err)
	}
	var unknown []string
	for path := range paths {
		if _, exists := environment.Objects[path]; !exists {
			unknown = append(unknown, path)
		}
	}
	sort.Strings(unknown)
	t.Logf("environment_hash=%s unknown_types=%d paths=%q", environmentHash, len(unknown), unknown)
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	started = time.Now()
	source, _ := dmmap.New(environment, data, "")
	// Reproducible fixture identities, independent of process-local instance IDs.
	serial := 0
	for _, tile := range source.Tiles {
		for _, instance := range tile.Instances() {
			serial++
			instance.SetStableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", serial))
		}
	}
	snapshot, err := mapadapter.Import(source, "01890f3e-7b5c-7abc-8def-0123456789ab", environmentHash)
	if err != nil {
		t.Fatal(err)
	}
	snapshotHash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("editor_model_import_ms=%.3f snapshot_hash=%s", float64(time.Since(started).Microseconds())/1000, snapshotHash)
	outputDir := t.TempDir()
	if root := os.Getenv("APHELION_AUDIT_OUTPUT"); root != "" {
		outputDir, err = os.MkdirTemp(root, "map-")
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, tgm := range []bool{false, true} {
		path := filepath.Join(outputDir, fmt.Sprintf("roundtrip-tgm-%t.dmm", tgm))
		started = time.Now()
		output, err := mapadapter.Export(snapshot, path, tgm, data.LineBreak)
		if err != nil {
			t.Fatal(err)
		}
		if err := output.Save(); err != nil {
			t.Fatal(err)
		}
		t.Logf("TGM=%t export_atomic_save_ms=%.3f", tgm, float64(time.Since(started).Microseconds())/1000)
		reparsed, err := dmmdata.New(path)
		if err != nil {
			t.Fatal(err)
		}
		assertRepresentativeMapValues(t, data, reparsed)
		target, _ := dmmap.New(environment, reparsed, "")
		roundtrip, err := mapadapter.Reimport(target, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := roundtrip.Hash()
		if err != nil || hash != snapshotHash {
			t.Fatalf("TGM=%t collaboration round trip changed canonical state: %v", tgm, err)
		}
		t.Logf("TGM=%t output=%s sha256=%s exact_values_and_snapshot=true", tgm, path, auditFileHash(t, path))
	}
}

func auditFileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func assertRepresentativeMapValues(t *testing.T, source, target *dmmdata.DmmData) {
	t.Helper()
	if source.MaxX != target.MaxX || source.MaxY != target.MaxY || source.MaxZ != target.MaxZ || len(source.Grid) != len(target.Grid) {
		t.Fatal("saved map dimensions or cell count changed")
	}
	for coord, key := range source.Grid {
		targetKey, exists := target.Grid[coord]
		want, got := source.Dictionary[key], target.Dictionary[targetKey]
		if !exists || len(want) != len(got) {
			t.Fatalf("saved prefab count changed at %v", coord)
		}
		for index, prefab := range want {
			other := got[index]
			if prefab.Path() != other.Path() || prefab.Vars().Len() != other.Vars().Len() {
				t.Fatalf("saved prefab path or variable count changed at %v index %d", coord, index)
			}
			for _, name := range prefab.Vars().Iterate() {
				value, exists := other.Vars().Value(name)
				if !exists || value != prefab.Vars().ValueV(name, "") {
					t.Fatalf("saved variable %q changed at %v index %d", name, coord, index)
				}
			}
		}
	}
}
