package rsc

import _ "embed"

var (
	//go:embed font/Inter-Medium.ttf
	fontTTF []byte
	//go:embed font/icons/icomoon.ttf
	fontIconsTTF []byte
)

func FontTTF() []byte {
	return fontTTF
}

func FontIconsTTF() []byte {
	return fontIconsTTF
}

// APHELION EDIT ADDITION START - MERIDIAN THEME

// IBM Plex Mono, SIL OFL 1.1; provenance in font/plex/README.md.
//
//go:embed font/plex/IBMPlexMono-Regular.ttf
var fontMonoTTF []byte

// FontMonoTTF is the monospace face for paths and values.
func FontMonoTTF() []byte { return fontMonoTTF }

// APHELION EDIT ADDITION END
