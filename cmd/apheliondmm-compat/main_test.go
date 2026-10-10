package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunNegotiatesClientAndRejectsUnsupportedVersion(t *testing.T) {
	var output bytes.Buffer
	if code := run([]string{"-client-protocol", "2", "-client-schema", "1"}, &output, &output); code != 0 {
		t.Fatalf("run() = %d, output = %s", code, output.String())
	}
	output.Reset()
	if code := run([]string{"-client-protocol", "1", "-client-schema", "1"}, &output, &output); code == 0 {
		t.Fatalf("run() = 0, output = %s", output.String())
	}
}

func TestRunValidatesDeclaredRollingPairFromFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "matrix.json")
	data := []byte(`{"releases":[{"name":"previous","protocol_versions":[2],"schema_versions":[1]},{"name":"current","protocol_versions":[2],"schema_versions":[1]}],"rolling_pairs":[{"from":"previous","to":"current"}]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if code := run([]string{"-matrix", path, "-from", "previous", "-to", "current"}, &output, &output); code != 0 {
		t.Fatalf("run() = %d, output = %s", code, output.String())
	}
}
