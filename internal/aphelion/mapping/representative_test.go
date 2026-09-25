package mapping

import (
	"context"
	"os"
	"path/filepath"
	"sdmm/internal/dmapi/dmenv"
	"testing"
	"time"
)

func TestRepresentativeTramComposition(t *testing.T) {
	dme := os.Getenv("APHELION_MAPPING_DME")
	if dme == "" {
		t.Skip("set APHELION_MAPPING_DME to the selected Meridian environment for the real TramStation contract")
	}
	started := time.Now()
	environment, err := dmenv.New(dme)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("environment reconstruction/fingerprint: %s", time.Since(started))
	catalog := NewCatalog(environment)
	defer catalog.Close()
	base, err := catalog.Load(context.Background(), filepath.Join(environment.RootDir, "_maps/map_files/tramstation/tramstation.dmm"))
	if err != nil {
		t.Fatal(err)
	}
	roots, diagnostics := catalog.Roots(context.Background(), base, Transform{}, "")
	if len(roots) != 11 || len(diagnostics) != 0 {
		t.Fatalf("TramStation root/config contract: roots=%d diagnostics=%+v", len(roots), diagnostics)
	}
	fixed, bindings, diagnostics := catalog.Fixed(context.Background(), base, "_maps/tramstation.json", nil)
	if len(fixed) != 8 || len(bindings) != 8 {
		t.Fatalf("fixed placement contract: bindings=%+v diagnostics=%+v", bindings, diagnostics)
	}
	for _, binding := range bindings {
		if binding.Error != "" {
			t.Fatal(binding.Error)
		}
	}
	projection, err := catalog.Compose(context.Background(), base, Scenario{}, fixed)
	if err != nil {
		t.Fatal(err)
	}
	defer projection.Close()
	nested := 0
	for _, root := range projection.Roots {
		if root.Parent != "" {
			nested++
		}
	}
	if nested < 2 {
		t.Fatal("configured nested alternatives were not expanded")
	}
	for _, diagnostic := range projection.Diagnostics {
		if diagnostic.Severity == "error" {
			t.Errorf("%s: %s [%s]", diagnostic.Code, diagnostic.Message, diagnostic.Source)
		}
	}
	t.Logf("base_hash=%s roots=%d nested=%d placements=%d fixed=%d diagnostics=%d elapsed=%s", base.Identity.ContentHash, len(roots), nested, len(projection.Placements), len(fixed), len(projection.Diagnostics), time.Since(started))
}
