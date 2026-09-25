package mapadapter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmvars"
)

func TestEnvironmentHashIsCanonical(t *testing.T) {
	t.Parallel()

	left := testDME([]string{"dir", "icon"})
	right := testDME([]string{"icon", "dir"})
	leftHash, err := EnvironmentHash(left)
	if err != nil {
		t.Fatalf("EnvironmentHash(left) error = %v", err)
	}
	rightHash, err := EnvironmentHash(right)
	if err != nil {
		t.Fatalf("EnvironmentHash(right) error = %v", err)
	}
	if leftHash != rightHash {
		t.Fatalf("environment hashes differ: %q != %q", leftHash, rightHash)
	}

	// Retain the previous byte framing as an independent compatibility oracle.
	var encoded bytes.Buffer
	writeAdapterString(&encoded, "apheliondmm.environment.v1")
	writeAdapterUint64(&encoded, 1)
	writeAdapterString(&encoded, "/obj/foo")
	writeAdapterUint64(&encoded, 2)
	for _, value := range []string{"dir", "2", "icon", "'foo.dmi'"} {
		writeAdapterString(&encoded, value)
	}
	digest := sha256.Sum256(encoded.Bytes())
	if leftHash != hex.EncodeToString(digest[:]) {
		t.Fatal("canonical environment framing changed")
	}
	changedVars := &dmvars.MutableVariables{}
	changedVars.Put("dir", "4")
	changedVars.Put("icon", "'foo.dmi'")
	right.Objects["/obj/foo"].Vars = changedVars.ToImmutable()
	changedHash, err := EnvironmentHash(right)
	if err != nil {
		t.Fatalf("EnvironmentHash(changed) error = %v", err)
	}
	if changedHash == leftHash {
		t.Fatalf("environment value change did not affect hash %q", changedHash)
	}
}

func testDME(variableOrder []string) *dmenv.Dme {
	values := map[string]string{"dir": "2", "icon": "'foo.dmi'"}
	variables := &dmvars.MutableVariables{}
	for _, name := range variableOrder {
		variables.Put(name, values[name])
	}
	return &dmenv.Dme{Objects: map[string]*dmenv.Object{
		"/obj/foo": {Path: "/obj/foo", Vars: variables.ToImmutable()},
	}}
}
