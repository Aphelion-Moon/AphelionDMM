package store

import (
	"strings"
	"testing"
)

func TestNormalizeHostedSessionMetadataDefaultsAndBoundsLabels(t *testing.T) {
	metadata, err := NormalizeHostedSessionMetadata(HostedSessionMetadata{Title: "  " + strings.Repeat("a", 128) + "  ", MapLabel: "map.dmm", EnvironmentLabel: "station.dme"})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Visibility != HostedVisibilityPrivate || metadata.Title != strings.Repeat("a", 128) {
		t.Fatalf("normalized metadata = %#v", metadata)
	}

	for name, invalid := range map[string]HostedSessionMetadata{
		"unknown visibility":   {Visibility: HostedVisibility("public")},
		"too many UTF-8 bytes": {Title: strings.Repeat("界", 43)},
		"control character":    {Title: "safe\nunsafe"},
		"map path":             {MapLabel: `maps\lobby.dmm`},
		"URL":                  {EnvironmentLabel: "https://example.invalid/station.dme"},
		"invalid UTF-8":        {Title: string([]byte{0xff})},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeHostedSessionMetadata(invalid); err == nil {
				t.Fatal("invalid hosted metadata was accepted")
			}
		})
	}
}
