package cpenvironment

import (
	"errors"
	"testing"

	"sdmm/internal/aphelion/definition"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/third_party/sdmmparser"
)

// Every rejection below happens before an executable is chosen or started.
func TestOpenDefinitionRefusesUnknownTypesAndEscapingSources(t *testing.T) {
	env := &dmenv.Dme{RootDir: t.TempDir(), Objects: map[string]*dmenv.Object{
		"/obj/escape": {Path: "/obj/escape", Location: sdmmparser.Location{File: "../outside.dm", Line: 1}},
		"/obj/table":  {Path: "/obj/table", Location: sdmmparser.Location{File: "code/table.dm", Line: 3}},
	}}

	if HasDefinition(nil, "/obj/table") || HasDefinition(env, "/area/engine/break_room") || !HasDefinition(env, "/obj/table") {
		t.Fatal("HasDefinition must report only types the loaded environment defines")
	}
	if err := OpenDefinition(env, "Default App", "/area/engine/break_room"); !errors.Is(err, ErrNoDefinition) {
		t.Fatalf("unknown type error = %v", err)
	}
	if err := OpenDefinition(nil, "Default App", "/obj/table"); !errors.Is(err, ErrNoDefinition) {
		t.Fatalf("missing environment error = %v", err)
	}
	if err := OpenDefinition(env, "Default App", "/obj/escape"); !errors.Is(err, definition.ErrOutsideRoot) {
		t.Fatalf("escaping source error = %v", err)
	}
	if err := OpenDefinition(env, "not an editor", "/obj/table"); err == nil {
		t.Fatal("an unknown editor preference must be refused instead of starting a nil command")
	}
}
