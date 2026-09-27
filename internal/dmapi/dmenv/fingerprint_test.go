package dmenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsedFingerprintBelongsToLoadedGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generation.dme")
	if err := os.WriteFile(path, []byte("/obj/example\n\tvar/value = 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	one, err := first.EnvironmentHash()
	if err != nil || one == "" || first.fingerprint != one {
		t.Fatal("parsed generation did not retain fingerprint", err)
	}
	if err := os.WriteFile(path, []byte("/obj/example\n\tvar/value = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	two, err := second.EnvironmentHash()
	if err != nil || two == one {
		t.Fatal("same-path reload reused old fingerprint", err)
	}
	still, err := first.EnvironmentHash()
	if err != nil || still != one {
		t.Fatal("new generation changed existing consumer fingerprint", err)
	}
}

func TestParsedFingerprintIgnoresCheckoutLocationAndLineEndings(t *testing.T) {
	var baseline string
	var previous *Dme
	for _, ending := range []string{"\n", "\r\n"} {
		root := t.TempDir()
		path := filepath.Join(root, "project.dme")
		if err := os.WriteFile(path, []byte("#include \"types.dm\""+ending), 0600); err != nil {
			t.Fatal(err)
		}
		source := "/obj/example\n\tname = \"Example\"\n\ticon = 'icons/example.dmi'\n\tdesc = {\"First line\nSecond line\"}\n\tvar/values = list(\"one\" = 1, \"two\" = 2)\n\tvar/escaped = \"First line\\r\\nSecond line\"\n"
		if err := os.WriteFile(filepath.Join(root, "types.dm"), []byte(strings.ReplaceAll(source, "\n", ending)), 0600); err != nil {
			t.Fatal(err)
		}
		environment, err := New(path)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := environment.EnvironmentHash()
		if err != nil {
			t.Fatal(err)
		}
		escaped, _ := environment.Objects["/obj/example"].Vars.Value("escaped")
		if !strings.Contains(escaped, `\r\n`) {
			t.Fatalf("literal DM escape sequences changed: %q", escaped)
		}
		if baseline == "" {
			baseline = hash
			previous = environment
		} else if hash != baseline {
			for path, object := range environment.Objects {
				for _, name := range object.Vars.Iterate() {
					before, _ := previous.Objects[path].Vars.Value(name)
					after, _ := object.Vars.Value(name)
					if before != after {
						t.Logf("%s.%s: %q != %q", path, name, before, after)
					}
				}
			}
			t.Fatalf("identical environment changed fingerprint across checkout location or line endings: %s != %s", baseline, hash)
		}
	}
}
