package dmenv

import (
	"os"
	"path/filepath"
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
