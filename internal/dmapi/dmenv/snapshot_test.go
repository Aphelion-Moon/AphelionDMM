package dmenv

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"sdmm/internal/aphelion/envcache"
	"sdmm/internal/aphelion/envsnapshot"
	"sdmm/third_party/sdmmparser"
)

type exportedObject struct {
	Path, Parent string
	Location     sdmmparser.Location
	Children     []string
	Vars         map[string]string
	Flags        map[string]VarFlags
}

func TestRepresentativeEnvironmentSnapshot(t *testing.T) {
	path := os.Getenv("APHELION_MAPPING_DME")
	if path == "" {
		t.Skip("set APHELION_MAPPING_DME for the read-only representative workload")
	}
	root, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(root, "aphelion-snapshot-measure-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	}()
	var expected [32]byte
	for pass := range 4 {
		start := time.Now()
		d, err := NewWithOptions(context.Background(), path, envsnapshot.Options{Enabled: pass != 0, Directory: directory})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("pass=%d elapsed=%s status=%s types=%d", pass, elapsed, d.CacheStatus, len(d.Objects))
		exported := exportEnvironment(d)
		exported.Status = ""
		data, err := json.Marshal(exported)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if pass == 0 {
			expected = digest
		} else if digest != expected {
			t.Fatal("representative restoration export differs from uncached parse")
		}
		if pass == 1 {
			deadline := time.Now().Add(45 * time.Second)
			for {
				entry, e := envcache.New(directory).Load(context.Background(), path, envsnapshot.ParserIdentity)
				if e == nil {
					entry.Close()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("representative cache was not persisted", e, d.CacheStatus)
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		if pass > 1 && d.CacheStatus != "validated cache hit" {
			t.Fatal("representative warm load missed", d.CacheStatus)
		}
	}
}

type exportedEnvironment struct {
	Status, Hash string
	Objects      map[string]exportedObject
}

func exportEnvironment(d *Dme) exportedEnvironment {
	result := exportedEnvironment{Status: d.CacheStatus, Objects: map[string]exportedObject{}}
	result.Hash, _ = d.EnvironmentHash()
	for path, object := range d.Objects {
		entry := exportedObject{Path: path, Location: object.Location, Children: object.DirectChildren, Flags: object.VarFlags, Vars: map[string]string{}}
		if object.Parent() != nil {
			entry.Parent = object.Parent().Path
		}
		for _, name := range object.Vars.Iterate() {
			entry.Vars[name], _ = object.Vars.ExplicitValue(name)
		}
		result.Objects[path] = entry
	}
	return result
}

func TestSnapshotFreshProcessRestoreMatchesEveryExportedField(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fixture.dme")
	if err := os.WriteFile(path, []byte("/obj/other\n\tvar/const/value = 7\n/obj/namespace\n\tname = null\n/obj/namespace/child\n\tparent_type = /obj/other\n\tvar/tmp/temporary = null\n\tvar/static/shared = 3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	userCache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(userCache, "aphelion-snapshot-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	}()
	uncached, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := exportEnvironment(uncached)
	for pass := range 2 {
		output := filepath.Join(root, "result.json")
		command := exec.Command(os.Args[0], "-test.run=^TestSnapshotHelperProcess$")
		command.Env = append(os.Environ(), "APHELION_SNAPSHOT_SOURCE="+path, "APHELION_SNAPSHOT_CACHE="+directory, "APHELION_SNAPSHOT_OUTPUT="+output)
		if data, err := command.CombinedOutput(); err != nil {
			t.Fatalf("fresh process %d: %v\n%s", pass, err, data)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var actual exportedEnvironment
		if err = json.Unmarshal(data, &actual); err != nil {
			t.Fatal(err)
		}
		if pass == 1 && actual.Status != "validated cache hit" {
			t.Fatal("fresh process did not hit cache", actual.Status)
		}
		if actual.Hash != expected.Hash || !reflect.DeepEqual(actual.Objects, expected.Objects) {
			t.Fatal("restored fields differ from full native parse")
		}
	}
}

func TestSnapshotHelperProcess(t *testing.T) {
	path := os.Getenv("APHELION_SNAPSHOT_SOURCE")
	if path == "" {
		t.Skip("subprocess fixture")
	}
	directory := os.Getenv("APHELION_SNAPSHOT_CACHE")
	d, err := NewWithOptions(context.Background(), path, envsnapshot.Options{Enabled: true, Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	cache := envcache.New(directory)
	deadline := time.Now().Add(10 * time.Second)
	for {
		entry, err := cache.Load(context.Background(), path, envsnapshot.ParserIdentity)
		if err == nil {
			entry.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cache was not persisted", err, d.CacheStatus)
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err := json.Marshal(exportEnvironment(d))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("APHELION_SNAPSHOT_OUTPUT"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRelativeSnapshotPreservesSourceLocationSpelling(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "relative.dme")
	if err := os.WriteFile(abs, []byte("/obj/relative\n\tname = \"relative\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.Rel(cwd, abs)
	if err != nil {
		t.Fatal(err)
	}
	userCache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(userCache, "aphelion-relative-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	}()
	uncached, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := exportEnvironment(uncached)
	// Cold and eventual restored exports must retain the same relative source
	// locations, independently from an absolute invocation's cache record.
	deadline := time.Now().Add(10 * time.Second)
	for {
		d, err := NewWithOptions(context.Background(), path, envsnapshot.Options{Enabled: true, Directory: directory})
		if err != nil {
			t.Fatal(err)
		}
		actual := exportEnvironment(d)
		if actual.Hash != expected.Hash || !reflect.DeepEqual(actual.Objects, expected.Objects) {
			t.Fatal("relative cached export differs from original parser")
		}
		if d.CacheStatus == "validated cache hit" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("relative snapshot never restored", d.CacheStatus)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
