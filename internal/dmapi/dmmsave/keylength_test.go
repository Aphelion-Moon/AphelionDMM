package dmmsave

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

const keyLengthTargetContent = "original target"

// keyLengthMap builds a one-row map whose tiles all hold distinct contents and
// whose backup (the "original file") uses one-letter keys.
func keyLengthMap(t *testing.T, unique int) (*dmmap.Dmm, string) {
	t.Helper()
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup.dmm")
	if err := os.WriteFile(backup, []byte("\"a\"=(/obj/one)\n(1,1,1)={\"\na\n\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target.dmm")
	if err := os.WriteFile(target, []byte(keyLengthTargetContent), 0o600); err != nil {
		t.Fatal(err)
	}
	dmm := &dmmap.Dmm{MaxX: unique, MaxY: 1, MaxZ: 1, Backup: backup}
	for x := 1; x <= unique; x++ {
		tile := &dmmap.Tile{Coord: util.Point{X: x, Y: 1, Z: 1}}
		prefab := dmmprefab.New(dmmprefab.IdNone, fmt.Sprintf("/obj/t%d", x), (&dmvars.MutableVariables{}).ToImmutable())
		tile.InstancesSet(dmmdata.Prefabs{prefab})
		dmm.Tiles = append(dmm.Tiles, tile)
	}
	return dmm, target
}

func TestKeyLengthConfirmationBoundaries(t *testing.T) {
	cases := []struct {
		unique   int
		prompted bool
	}{{51, false}, {52, false}, {53, true}}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.unique), func(t *testing.T) {
			dmm, target := keyLengthMap(t, tc.unique)
			var got []KeyLengthChange
			cfg := Config{Format: FormatDM, ConfirmKeyLengthChange: func(change KeyLengthChange) bool {
				got = append(got, change)
				return true
			}}
			if err := SaveV(&dmenv.Dme{}, dmm, target, cfg); err != nil {
				t.Fatal(err)
			}
			if tc.prompted != (len(got) == 1) || len(got) > 1 {
				t.Fatalf("confirmation calls = %d, want prompted=%t", len(got), tc.prompted)
			}
			if tc.prompted {
				plan := got[0].Plan
				if got[0].Path != target || plan.Current != 1 || plan.Required != 2 || plan.Unique != tc.unique || plan.CurrentCapacity != 52 {
					t.Fatalf("unexpected change %+v", got[0])
				}
			}
			saved, err := dmmdata.New(target)
			if err != nil {
				t.Fatal(err)
			}
			wantLength := 1
			if tc.prompted {
				wantLength = 2
			}
			if saved.KeyLength != wantLength || len(saved.Dictionary) != tc.unique {
				t.Fatalf("saved key length %d with %d contents", saved.KeyLength, len(saved.Dictionary))
			}
		})
	}
}

func TestKeyLengthDeclineLeavesFileUntouched(t *testing.T) {
	dmm, target := keyLengthMap(t, 60)
	cfg := Config{Format: FormatDM, ConfirmKeyLengthChange: func(KeyLengthChange) bool { return false }}
	err := SaveV(&dmenv.Dme{}, dmm, target, cfg)
	if !errors.Is(err, ErrKeyLengthChangeDeclined) {
		t.Fatalf("err = %v, want ErrKeyLengthChangeDeclined", err)
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil || string(got) != keyLengthTargetContent {
		t.Fatalf("declined save changed target: %q %v", got, readErr)
	}
	if stages, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".*.tmp-*")); len(stages) != 0 {
		t.Fatalf("staging files remain: %v", stages)
	}
}
